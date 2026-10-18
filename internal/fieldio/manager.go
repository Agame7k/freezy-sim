package fieldio

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const (
	loopPeriod        = 50 * time.Millisecond
	boxRefreshPeriod  = time.Second
	altRegisterResync = 2 * time.Second
)

// EthernetSource reports whether the DS in a station (0-5, R1..B3) is plugged into the field.
type EthernetSource func(station int) bool

// Manager owns the field I/O model and whichever transport the active profile uses.
type Manager struct {
	cfg   *config.Config
	hub   *bus.Hub
	arena *arena.Client
	Field *Field

	Modbus *ModbusServer
	Boxes  []*Esp32Box

	mu              sync.Mutex
	profile         string
	feederRate      [2]float64
	feederCarry     [2]float64
	altLastRegs     [RegisterCount]uint16
	altRegsAt       time.Time
	altLastEthernet [6]bool
	altEthernetSent bool
	altFtaSent      bool
	altLastFta      bool
	altSendSensors  bool
	altLastSensors  [8]bool
	altErr          string
	altInputsSent   int
	altRegsSent     int
	lastBoxRefresh  time.Time
	stop            chan struct{}
}

func NewManager(cfg *config.Config, hub *bus.Hub, arenaClient *arena.Client, ethernet EthernetSource) *Manager {
	field := newField()
	field.ethernet = ethernet
	manager := &Manager{
		cfg: cfg, hub: hub, arena: arenaClient, Field: field, profile: cfg.Field.Profile, stop: make(chan struct{}),
	}
	manager.Modbus = NewModbusServer(field, cfg.PLC.Bind, cfg.PLC.Port)
	manager.Boxes = []*Esp32Box{
		newBox("scoreTable", cfg.AltIO.ScoreTableIP, cfg.AltIO.HealthPort, []int{InFieldEStop}),
		newBox("red", cfg.AltIO.RedIP, cfg.AltIO.HealthPort,
			[]int{InRed1EStop, InRed1AStop, InRed2EStop, InRed2AStop, InRed3EStop, InRed3AStop}),
		newBox("blue", cfg.AltIO.BlueIP, cfg.AltIO.HealthPort,
			[]int{InBlue1EStop, InBlue1AStop, InBlue2EStop, InBlue2AStop, InBlue3EStop, InBlue3AStop}),
	}
	field.OnMatchReset(func() { hub.Logf("field", "PLC match reset pulse: hub counters cleared") })
	arenaClient.OnMatchState(func(old, new arena.MatchState) {
		switch {
		case new == arena.StartMatch || new == arena.AutoPeriod:
			field.MatchStarted(true)
		case new == arena.PreMatch:
			field.MatchStarted(false)
		}
	})
	manager.registerCommands()
	return manager
}

// Profile returns the active field I/O profile.
func (manager *Manager) Profile() string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.profile
}

// ContributeSettings tells the arena where to find the active profile's devices.
func (manager *Manager) ContributeSettings(settings map[string]string) {
	cfg := manager.cfg
	switch manager.Profile() {
	case config.ProfilePLC:
		settings["plcAddress"] = cfg.PLC.Bind
		settings["alternateIOEnabled"] = ""
		settings["ScoreTableEstopAddress"] = ""
		settings["RedAllianceStationEstopAddress"] = ""
		settings["BlueAllianceStationEstopAddress"] = ""
	case config.ProfileAltIO:
		settings["plcAddress"] = ""
		settings["alternateIOEnabled"] = "on"
		settings["ScoreTableEstopAddress"] = cfg.AltIO.ScoreTableIP
		settings["RedAllianceStationEstopAddress"] = cfg.AltIO.RedIP
		settings["BlueAllianceStationEstopAddress"] = cfg.AltIO.BlueIP
	default:
		settings["plcAddress"] = ""
		settings["alternateIOEnabled"] = ""
		settings["ScoreTableEstopAddress"] = ""
		settings["RedAllianceStationEstopAddress"] = ""
		settings["BlueAllianceStationEstopAddress"] = ""
	}
}

// Run starts the I/O loop and publisher.
func (manager *Manager) Run() {
	manager.Modbus.Start()
	manager.applyProfile()
	safe.Go(manager.hub, "fieldio", manager.loop)
}

// Stop shuts down every transport.
func (manager *Manager) Stop() {
	close(manager.stop)
	manager.Modbus.Stop()
	for _, box := range manager.Boxes {
		box.ensureListening(false)
	}
}

func (manager *Manager) applyProfile() {
	profile := manager.Profile()
	manager.Modbus.SetOffline(profile != config.ProfilePLC)
	for _, box := range manager.Boxes {
		box.ensureListening(profile == config.ProfileAltIO)
	}
	manager.mu.Lock()
	manager.altEthernetSent = false
	manager.altRegsAt = time.Time{}
	manager.mu.Unlock()
}

func (manager *Manager) loop() {
	ticker := time.NewTicker(loopPeriod)
	defer ticker.Stop()
	publishCounter := 0
	last := time.Now()
	for {
		select {
		case <-manager.stop:
			return
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			manager.runFeeder(dt)
			switch manager.Profile() {
			case config.ProfileAltIO:
				manager.syncAltIO(now)
			case config.ProfilePLC:
				// Coils arrive through Modbus writes; nothing to push.
			default:
				manager.syncCoilsFromArena()
			}
			publishCounter++
			if publishCounter%2 == 0 {
				manager.publish(now)
			}
		}
	}
}

func (manager *Manager) runFeeder(dt float64) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for alliance := 0; alliance < 2; alliance++ {
		manager.feederCarry[alliance] += manager.feederRate[alliance] * dt
		for manager.feederCarry[alliance] >= 1 {
			manager.feederCarry[alliance]--
			manager.Field.ScoreFuel(alliance, rand.Intn(4))
		}
	}
}

func (manager *Manager) syncCoilsFromArena() {
	plcIo := manager.arena.PlcIo()
	if len(plcIo.Coils) >= LegacyCoilCount { // older arenas have no winner coils; those stay off
		var coils [CoilCount]bool
		copy(coils[:], plcIo.Coils)
		manager.Field.SetCoils(coils, "arena")
		manager.Field.setHeartbeat(true)
	}
}

// syncAltIO mirrors the field into the arena over the Freezy alternate-IO interfaces.
func (manager *Manager) syncAltIO(now time.Time) {
	manager.syncCoilsFromArena()
	if !manager.arena.Connected() {
		return
	}
	inputs := manager.Field.Inputs(now)

	// E-stop boxes post their buttons over HTTP.
	forceBoxes := now.Sub(manager.lastBoxRefresh) >= boxRefreshPeriod
	if forceBoxes {
		manager.lastBoxRefresh = now
	}
	post := func(payload []stopState) error { return manager.arena.PostJSON("/api/freezy/eStopState", payload) }
	for _, box := range manager.Boxes {
		box.sync(inputs, post, forceBoxes)
	}

	// Registers (hub counters) and the remaining inputs go over the arena's PLC websocket, like plc_sym.py.
	registers := manager.Field.Registers()
	manager.mu.Lock()
	sendRegs := registers != manager.altLastRegs || now.Sub(manager.altRegsAt) >= altRegisterResync
	var ethernet [6]bool
	copy(ethernet[:], inputs[InRedConnected1:InRedConnected1+6])
	sendEthernet := !manager.altEthernetSent || ethernet != manager.altLastEthernet
	sendFta := manager.altFtaSent == false || inputs[InFtaReady] != manager.altLastFta
	var sensors [8]bool
	copy(sensors[:], inputs[InRedHubSensor1:InRedHubSensor1+8])
	sendSensors := manager.altSendSensors && sensors != manager.altLastSensors
	manager.mu.Unlock()

	var errText string
	if sendRegs {
		payload := make([]map[string]int, 0, RegisterCount-1)
		for register := RegRedHubTotal; register < RegisterCount; register++ {
			payload = append(payload, map[string]int{"register": register, "cValue": int(registers[register])})
		}
		err := manager.arena.Plc.Send("setRegisters", payload)
		inspector.Note("altio", "out", "plc websocket", fmt.Sprintf("setRegisters hub totals %d/%d", registers[RegRedHubTotal],
			registers[RegBlueHubTotal]), nil)
		if err != nil {
			errText = err.Error()
		} else {
			manager.mu.Lock()
			manager.altLastRegs = registers
			manager.altRegsAt = now
			manager.altRegsSent++
			manager.mu.Unlock()
		}
	}
	var changes []map[string]any
	if sendEthernet {
		for i := 0; i < 6; i++ {
			changes = append(changes, map[string]any{"channel": InRedConnected1 + i, "state": ethernet[i]})
		}
	}
	if sendFta {
		changes = append(changes, map[string]any{"channel": InFtaReady, "state": inputs[InFtaReady]})
	}
	if sendSensors {
		for i := 0; i < 8; i++ {
			changes = append(changes, map[string]any{"channel": InRedHubSensor1 + i, "state": sensors[i]})
		}
	}
	if len(changes) > 0 {
		err := manager.arena.Plc.Send("setInput", changes)
		inspector.Note("altio", "out", "plc websocket", fmt.Sprintf("setInput %v", changes), nil)
		if err != nil {
			errText = err.Error()
		} else {
			manager.mu.Lock()
			manager.altEthernetSent = true
			manager.altLastEthernet = ethernet
			manager.altFtaSent = true
			manager.altLastFta = inputs[InFtaReady]
			manager.altLastSensors = sensors
			manager.altInputsSent++
			manager.mu.Unlock()
		}
	}
	manager.mu.Lock()
	manager.altErr = errText
	manager.mu.Unlock()
}

// Transport is the published status of the active transport.
type Transport struct {
	Profile    string      `json:"profile"`
	Modbus     ModbusStats `json:"modbus"`
	Boxes      []BoxStatus `json:"boxes"`
	AltError   string      `json:"altError"`
	AltInputs  int         `json:"altInputsSent"`
	AltRegs    int         `json:"altRegistersSent"`
	AltSensors bool        `json:"altSendSensors"`
	FeederRate [2]float64  `json:"feederRate"`
}

func (manager *Manager) publish(now time.Time) {
	manager.hub.Publish("field.io", manager.Field.snapshot(now))
	boxes := make([]BoxStatus, len(manager.Boxes))
	for i, box := range manager.Boxes {
		boxes[i] = box.status()
	}
	manager.mu.Lock()
	transport := Transport{
		Profile: manager.profile, Modbus: manager.Modbus.Stats(), Boxes: boxes, AltError: manager.altErr,
		AltInputs: manager.altInputsSent, AltRegs: manager.altRegsSent, AltSensors: manager.altSendSensors,
		FeederRate: manager.feederRate,
	}
	manager.mu.Unlock()
	manager.hub.Publish("field.transport", transport)
}

// SetProfile switches the field I/O profile at runtime.
func (manager *Manager) SetProfile(profile string) error {
	switch profile {
	case config.ProfileAltIO, config.ProfilePLC, config.ProfileMinimal:
	default:
		return fmt.Errorf("unknown profile %q", profile)
	}
	manager.mu.Lock()
	manager.profile = profile
	manager.mu.Unlock()
	manager.cfg.Field.Profile = profile
	manager.applyProfile()
	manager.hub.Logf("field", "Field I/O profile set to %s", profile)
	return nil
}

func (manager *Manager) registerCommands() {
	manager.hub.Handle("field.set", func(args []byte) (any, error) {
		var request struct {
			Control string `json:"control"`
			Index   int    `json:"index"`
			Value   bool   `json:"value"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if err := manager.Field.Set(request.Control, request.Index, request.Value); err != nil {
			return nil, err
		}
		manager.hub.Logf("field", "%s[%d] = %v", request.Control, request.Index, request.Value)
		return nil, nil
	})
	manager.hub.Handle("field.pulse", func(args []byte) (any, error) {
		// Momentary press: on now, released after the given duration.
		var request struct {
			Control string `json:"control"`
			Index   int    `json:"index"`
			Ms      int    `json:"ms"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Ms <= 0 {
			request.Ms = 800
		}
		if err := manager.Field.Set(request.Control, request.Index, true); err != nil {
			return nil, err
		}
		go func() {
			defer safe.Recover(manager.hub, "fieldio")
			time.Sleep(time.Duration(request.Ms) * time.Millisecond)
			_ = manager.Field.Set(request.Control, request.Index, false)
		}()
		return nil, nil
	})
	manager.hub.Handle("field.fuel", func(args []byte) (any, error) {
		var request struct {
			Alliance int `json:"alliance"`
			Sensor   int `json:"sensor"`
			Count    int `json:"count"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Count <= 0 {
			request.Count = 1
		}
		for i := 0; i < request.Count; i++ {
			sensor := request.Sensor
			if sensor < 0 {
				sensor = rand.Intn(4)
			}
			manager.Field.ScoreFuel(request.Alliance, sensor)
		}
		return nil, nil
	})
	manager.hub.Handle("field.feeder", func(args []byte) (any, error) {
		var request struct {
			Alliance int     `json:"alliance"`
			Rate     float64 `json:"rate"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Alliance < 0 || request.Alliance > 1 {
			return nil, fmt.Errorf("bad alliance")
		}
		manager.mu.Lock()
		manager.feederRate[request.Alliance] = request.Rate
		manager.mu.Unlock()
		return nil, nil
	})
	manager.hub.Handle("field.resetHubs", func(_ []byte) (any, error) {
		manager.Field.ResetHubCounts()
		return nil, nil
	})
	manager.hub.Handle("field.profile", func(args []byte) (any, error) {
		var request struct {
			Profile string `json:"profile"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return nil, manager.SetProfile(request.Profile)
	})
	manager.hub.Handle("field.box", func(args []byte) (any, error) {
		var request struct {
			Name   string `json:"name"`
			Online bool   `json:"online"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		for _, box := range manager.Boxes {
			if box.Name == request.Name {
				box.setOnline(request.Online)
				box.ensureListening(manager.Profile() == config.ProfileAltIO)
				manager.hub.Logf("field", "E-stop box %s %s", box.Name,
					map[bool]string{true: "online", false: "offline"}[request.Online])
				return nil, nil
			}
		}
		return nil, fmt.Errorf("unknown box %q", request.Name)
	})
	manager.hub.Handle("field.plcOffline", func(args []byte) (any, error) {
		var request struct {
			Offline bool `json:"offline"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		manager.Modbus.SetOffline(request.Offline || manager.Profile() != config.ProfilePLC)
		manager.hub.Logf("field", "PLC emulator %s", map[bool]string{true: "offline", false: "online"}[request.Offline])
		return nil, nil
	})
	manager.hub.Handle("field.plcLag", func(args []byte) (any, error) {
		var request struct {
			Ms int `json:"ms"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		manager.Modbus.SetResponseLag(request.Ms)
		return nil, nil
	})
	manager.hub.Handle("field.altSensors", func(args []byte) (any, error) {
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		manager.mu.Lock()
		manager.altSendSensors = request.Enabled
		manager.mu.Unlock()
		return nil, nil
	})
}
