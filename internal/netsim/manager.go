package netsim

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

// Manager runs the AP, field switch and SCC emulators.
type Manager struct {
	cfg *config.Config
	hub *bus.Hub

	AP     *AccessPoint
	Switch *Switch
	SCCs   [2]*SCC // red, blue

	enabled bool
}

func NewManager(cfg *config.Config, hub *bus.Hub, info func(int) StationInfo) *Manager {
	// The AP sits above the scoring table at mid-field.
	apX := 16.541 / 2
	apY := 8.069 + 1.5
	manager := &Manager{
		cfg: cfg, hub: hub, enabled: cfg.Field.NetworkSecurity,
		AP:     NewAccessPoint(cfg.AP.Bind, cfg.AP.Port, cfg.AP.Password, info, apX, apY),
		Switch: NewSwitch(cfg.Switch.Bind, cfg.Switch.Port, cfg.Switch.Password),
		SCCs: [2]*SCC{
			NewSCC("RedSCC", cfg.SCC.RedBind, cfg.SCC.Port, cfg.SCC.Username, cfg.SCC.Password),
			NewSCC("BlueSCC", cfg.SCC.BlueBind, cfg.SCC.Port, cfg.SCC.Username, cfg.SCC.Password),
		},
	}
	manager.registerCommands()
	return manager
}

// Enabled reports whether the managed field network (network security) is simulated.
func (manager *Manager) Enabled() bool { return manager.enabled }

// ContributeSettings points the arena at the emulated network gear when network security is on.
func (manager *Manager) ContributeSettings(settings map[string]string) {
	cfg := manager.cfg
	if !manager.enabled {
		settings["networkSecurityEnabled"] = ""
		settings["sccManagementEnabled"] = ""
		return
	}
	settings["networkSecurityEnabled"] = "on"
	settings["apAddress"] = fmt.Sprintf("%s:%d", cfg.AP.Bind, cfg.AP.Port)
	settings["apPassword"] = cfg.AP.Password
	settings["switchAddress"] = cfg.Switch.Bind
	settings["switchPassword"] = cfg.Switch.Password
	settings["sccManagementEnabled"] = "on"
	settings["redSCCAddress"] = cfg.SCC.RedBind
	settings["blueSCCAddress"] = cfg.SCC.BlueBind
	settings["sccUsername"] = cfg.SCC.Username
	settings["sccPassword"] = cfg.SCC.Password
}

// Run starts the emulators and the publisher.
func (manager *Manager) Run() {
	manager.apply()
	safe.Go(manager.hub, "network", func() {
		for now := range time.Tick(250 * time.Millisecond) {
			manager.AP.tick(now)
			manager.publish()
		}
	})
}

func (manager *Manager) apply() {
	if manager.enabled {
		manager.AP.start()
		manager.Switch.start()
		manager.SCCs[0].start()
		manager.SCCs[1].start()
	} else {
		manager.AP.stop()
		manager.Switch.stop()
		manager.SCCs[0].stop()
		manager.SCCs[1].stop()
	}
}

// RadioAllowed gates a robot radio: with network security on, it can only link to its team's SSID on the field AP.
func (manager *Manager) RadioAllowed(team int) bool {
	if !manager.enabled {
		return true
	}
	return manager.AP.Broadcasting(team)
}

// EthernetUp gates a driver station's wired link through its alliance SCC port (stations 0-5).
func (manager *Manager) EthernetUp(station int) bool {
	if !manager.enabled {
		return true
	}
	scc := manager.SCCs[station/3]
	scc.mu.Lock()
	online := scc.online
	scc.mu.Unlock()
	return online && scc.PortUp(station%3)
}

func (manager *Manager) publish() {
	manager.hub.Publish("network.state", map[string]any{
		"enabled": manager.enabled,
		"ap":      manager.AP.state(),
		"switch":  manager.Switch.state(),
		"scc":     []SCCState{manager.SCCs[0].state(), manager.SCCs[1].state()},
	})
}

func (manager *Manager) registerCommands() {
	device := func(name string) (func(bool), error) {
		switch name {
		case "ap":
			return func(on bool) {
				manager.AP.mu.Lock()
				manager.AP.online = on
				if !on {
					manager.AP.status = "OFFLINE"
				} else {
					manager.AP.status = "ACTIVE"
				}
				manager.AP.mu.Unlock()
				if on && manager.enabled {
					manager.AP.start()
				} else {
					manager.AP.stop()
				}
			}, nil
		case "switch":
			return func(on bool) {
				manager.Switch.mu.Lock()
				manager.Switch.online = on
				manager.Switch.mu.Unlock()
				if on && manager.enabled {
					manager.Switch.start()
				} else {
					manager.Switch.stop()
				}
			}, nil
		case "redScc", "blueScc":
			scc := manager.SCCs[0]
			if name == "blueScc" {
				scc = manager.SCCs[1]
			}
			return func(on bool) {
				scc.mu.Lock()
				scc.online = on
				scc.mu.Unlock()
				if on && manager.enabled {
					scc.start()
				} else {
					scc.stop()
				}
			}, nil
		}
		return nil, fmt.Errorf("unknown network device %q", name)
	}
	manager.hub.Handle("network.device", func(args []byte) (any, error) {
		var request struct {
			Device string `json:"device"`
			Online bool   `json:"online"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		set, err := device(request.Device)
		if err != nil {
			return nil, err
		}
		set(request.Online)
		manager.hub.Logf("network", "%s %s", request.Device, map[bool]string{true: "back online", false: "taken offline"}[request.Online])
		return nil, nil
	})
	manager.hub.Handle("network.interference", func(args []byte) (any, error) {
		var request struct {
			Db float64 `json:"db"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		manager.AP.mu.Lock()
		manager.AP.interference = request.Db
		manager.AP.mu.Unlock()
		return nil, nil
	})
	manager.hub.Handle("network.enable", func(args []byte) (any, error) {
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		manager.enabled = request.Enabled
		manager.cfg.Field.NetworkSecurity = request.Enabled
		manager.apply()
		_, err := manager.hub.Dispatch("arena.configure", nil)
		return nil, err
	})
}
