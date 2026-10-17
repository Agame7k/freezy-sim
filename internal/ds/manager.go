package ds

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const publishPeriod = 100 * time.Millisecond

// PacketRecord is one entry in the packet inspector.
type PacketRecord struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"dir"`
	Kind      string    `json:"kind"`
	Station   string    `json:"station"`
	Hex       string    `json:"hex"`
	Length    int       `json:"len"`
}

// Manager owns the six simulated driver stations.
type Manager struct {
	cfg      *config.Config
	hub      *bus.Hub
	arena    *arena.Client
	Stations [6]*Station

	stop chan struct{}

	packetMu sync.Mutex
	packets  []PacketRecord
}

func NewManager(cfg *config.Config, hub *bus.Hub, arenaClient *arena.Client) *Manager {
	manager := &Manager{cfg: cfg, hub: hub, arena: arenaClient, stop: make(chan struct{})}
	for i := range manager.Stations {
		station := newStation(i, cfg.Arena.Host, cfg.DS.BaseUDPPort+i, cfg.DS.Legacy[i], cfg.DS.DefaultTeams[i])
		station.followArena = cfg.DS.FollowArena
		station.settings.DsOn = cfg.DS.AutoConnect
		station.onPacket = manager.recordPacket
		manager.Stations[i] = station
	}
	arenaClient.OnMatchLoad(func(load arena.MatchLoad) {
		manager.applyArenaTeams()
		// Fresh batteries go in between matches, like a real queue line.
		for _, station := range manager.Stations {
			station.mu.Lock()
			if station.robot.SoC < 0.9 {
				station.robot.FreshBattery()
			}
			station.mu.Unlock()
		}
	})
	manager.registerCommands()
	return manager
}

// Run starts every station loop and the state publisher.
func (manager *Manager) Run() {
	manager.applyArenaTeams()
	for _, station := range manager.Stations {
		station := station
		safe.Go(manager.hub, "ds", func() { station.run(manager.stop) })
	}
	safe.Go(manager.hub, "ds", func() {
		ticker := time.NewTicker(publishPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-manager.stop:
				return
			case now := <-ticker.C:
				states := make([]State, len(manager.Stations))
				for i, station := range manager.Stations {
					states[i] = station.snapshot(now)
				}
				manager.hub.Publish("ds.stations", states)
			}
		}
	})
}

// Stop shuts down every station.
func (manager *Manager) Stop() {
	close(manager.stop)
}

// SetNetworkGates installs the field-network gates on every station.
func (manager *Manager) SetNetworkGates(radio func(team int) bool, ethernet func(station int) bool) {
	for _, station := range manager.Stations {
		station.mu.Lock()
		station.radioGate = radio
		station.ethernetGate = ethernet
		station.mu.Unlock()
	}
}

// Station returns the station with the given name ("R1".."B3").
func (manager *Manager) Station(name string) *Station {
	for _, station := range manager.Stations {
		if station.Name == name {
			return station
		}
	}
	return nil
}

func (manager *Manager) applyArenaTeams() {
	teams := manager.arena.Teams()
	for i, station := range manager.Stations {
		station.mu.Lock()
		station.arenaTeam = teams[i]
		station.mu.Unlock()
	}
}

func (manager *Manager) recordPacket(direction, kind, station string, data []byte) {
	record := PacketRecord{
		Time: time.Now(), Direction: direction, Kind: kind, Station: station, Hex: hex.EncodeToString(data),
		Length: len(data),
	}
	if kind != "fms-control" { // (every control packet is noted in readControl)
		inspector.Note("ds", direction, station, kind, data)
	}
	manager.packetMu.Lock()
	manager.packets = append(manager.packets, record)
	if len(manager.packets) > 400 {
		manager.packets = manager.packets[len(manager.packets)-400:]
	}
	manager.packetMu.Unlock()
}

// Packets returns the recent packet log.
func (manager *Manager) Packets() []PacketRecord {
	manager.packetMu.Lock()
	defer manager.packetMu.Unlock()
	return append([]PacketRecord(nil), manager.packets...)
}

type stationRequest struct {
	Station string          `json:"station"`
	Field   string          `json:"field"`
	Value   json.RawMessage `json:"value"`
}

func (manager *Manager) targets(name string) ([]*Station, error) {
	if name == "" || name == "all" {
		return manager.Stations[:], nil
	}
	if station := manager.Station(name); station != nil {
		return []*Station{station}, nil
	}
	return nil, fmt.Errorf("unknown station %q", name)
}

func (manager *Manager) registerCommands() {
	manager.hub.Handle("ds.set", func(args []byte) (any, error) {
		var request stationRequest
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		stations, err := manager.targets(request.Station)
		if err != nil {
			return nil, err
		}
		for _, station := range stations {
			if err := station.set(request.Field, request.Value); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	action := func(name string, fn func(station *Station, now time.Time)) {
		manager.hub.Handle(name, func(args []byte) (any, error) {
			var request stationRequest
			if err := json.Unmarshal(args, &request); err != nil {
				return nil, err
			}
			stations, err := manager.targets(request.Station)
			if err != nil {
				return nil, err
			}
			now := time.Now()
			for _, station := range stations {
				station.mu.Lock()
				fn(station, now)
				station.mu.Unlock()
			}
			return nil, nil
		})
	}
	action("ds.rebootRio", func(station *Station, now time.Time) {
		station.robot.RebootRio(now)
		station.log("warn", "roboRIO rebooting")
	})
	action("ds.rebootRadio", func(station *Station, now time.Time) {
		station.robot.RebootRadio(now)
		station.log("warn", "Radio power-cycling")
	})
	action("ds.restartCode", func(station *Station, now time.Time) {
		station.robot.RestartCode(now)
		station.log("info", "Restarting robot code")
	})
	action("ds.crashCode", func(station *Station, now time.Time) {
		station.robot.Code = false
		station.log("bad", "Robot code crashed")
	})
	action("ds.powerCycle", func(station *Station, now time.Time) {
		station.robot.PowerCycle(now)
		station.log("warn", "Robot power-cycled")
	})
	action("ds.freshBattery", func(station *Station, now time.Time) {
		station.robot.FreshBattery()
		station.log("info", "Fresh battery installed")
	})
	action("ds.clearEStop", func(station *Station, now time.Time) {
		station.robot.EStopLatched = false
	})
	action("ds.resetStats", func(station *Station, now time.Time) {
		station.lostPackets = 0
		station.maxControlGap = 0
		station.controlCount = 0
	})
	manager.hub.Handle("ds.packets", func(_ []byte) (any, error) { return manager.Packets(), nil })
	manager.hub.Handle("ds.follow", func(args []byte) (any, error) {
		var request struct {
			Follow bool `json:"follow"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		for _, station := range manager.Stations {
			station.mu.Lock()
			station.followArena = request.Follow
			station.mu.Unlock()
		}
		return nil, nil
	})
}

// set changes one user setting on the station.
func (station *Station) set(field string, raw json.RawMessage) error {
	station.mu.Lock()
	defer station.mu.Unlock()
	asBool := func() (bool, error) {
		var value bool
		err := json.Unmarshal(raw, &value)
		return value, err
	}
	asFloat := func() (float64, error) {
		var value float64
		err := json.Unmarshal(raw, &value)
		return value, err
	}
	now := time.Now()
	switch field {
	case "dsOn":
		value, err := asBool()
		if err != nil {
			return err
		}
		station.settings.DsOn = value
		station.log("info", "Driver Station %s", map[bool]string{true: "started", false: "closed"}[value])
	case "ethernet":
		value, err := asBool()
		if err != nil {
			return err
		}
		station.settings.Ethernet = value
		station.log("info", "Field Ethernet %s", map[bool]string{true: "plugged in", false: "unplugged"}[value])
	case "team":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.settings.TeamOverride = int(value)
	case "legacy":
		value, err := asBool()
		if err != nil {
			return err
		}
		station.settings.Legacy = value
		station.closeSessionLocked()
	case "lossPct":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.settings.LossPct = value
	case "latencyMs":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.settings.LatencyMs = int(value)
	case "jitterMs":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.settings.JitterMs = int(value)
	case "autoClearEStop":
		value, err := asBool()
		if err != nil {
			return err
		}
		station.settings.AutoClearES = value
	case "robotPower":
		value, err := asBool()
		if err != nil {
			return err
		}
		if value && !station.robot.Powered {
			station.robot.PowerCycle(now)
		}
		station.robot.Powered = value
	case "radio":
		value, err := asBool()
		if err != nil {
			return err
		}
		station.robot.Radio = value
	case "rio":
		value, err := asBool()
		if err != nil {
			return err
		}
		if value && !station.robot.Rio {
			station.robot.RebootRio(now)
		}
		station.robot.Rio = value
	case "code":
		value, err := asBool()
		if err != nil {
			return err
		}
		if value && !station.robot.Code {
			station.robot.RestartCode(now)
		}
		station.robot.Code = value
	case "batteryFull":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.robot.BatteryFull = value
	case "resistance":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.robot.Resistance = value
	case "soc":
		value, err := asFloat()
		if err != nil {
			return err
		}
		station.robot.SoC = value
	default:
		return fmt.Errorf("unknown driver station setting %q", field)
	}
	return nil
}
