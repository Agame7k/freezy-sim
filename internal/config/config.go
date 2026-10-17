// Package config holds all user-tunable settings for the simulator.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Field I/O profiles.
const (
	ProfileAltIO   = "altio"   // Freezy alternate IO: ESP32 e-stop boxes + arena PLC websocket.
	ProfilePLC     = "plc"     // Modbus-TCP PLC (official-field style).
	ProfileMinimal = "minimal" // Driver stations only; no field I/O.
)

// Arena run modes.
const (
	ArenaSandbox  = "sandbox"  // Build/copy the arena into run/arena and launch it there.
	ArenaExternal = "external" // Attach to an arena that is already running.
)

type Config struct {
	GUI    GUIConfig    `json:"gui"`
	Arena  ArenaConfig  `json:"arena"`
	Field  FieldConfig  `json:"field"`
	DS     DSConfig     `json:"ds"`
	PLC    PLCConfig    `json:"plc"`
	AltIO  AltIOConfig  `json:"altio"`
	SACN   SACNConfig   `json:"sacn"`
	AP     APConfig     `json:"ap"`
	Switch SwitchConfig `json:"switch"`
	SCC    SCCConfig    `json:"scc"`

	path string
}

type GUIConfig struct {
	Bind   string `json:"bind"`
	Port   int    `json:"port"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// Remote opens the GUI to other devices: "" (this PC only), "tailscale" (also this PC's Tailscale addresses) or
	// "lan" (every interface; other devices need Token, or a random token printed at startup).
	Remote string `json:"remote"`
	Token  string `json:"token"`
}

type ArenaConfig struct {
	Mode          string   `json:"mode"`
	SourceDir     string   `json:"sourceDir"`
	SandboxDir    string   `json:"sandboxDir"`
	Host          string   `json:"host"`
	HTTPPort      int      `json:"httpPort"`
	AdminPassword string   `json:"adminPassword"`
	AutoStart     bool     `json:"autoStart"`
	AutoConfigure bool     `json:"autoConfigure"`
	PreferBuild   bool     `json:"preferBuild"`
	ExtraArgs     []string `json:"extraArgs"`
}

type FieldConfig struct {
	Profile         string `json:"profile"`
	Variant         string `json:"variant"`         // "welded" or "andymark"
	NetworkSecurity bool   `json:"networkSecurity"` // simulate the managed field network (AP, switch, SCCs)
}

type DSConfig struct {
	BaseUDPPort  int     `json:"baseUdpPort"`
	Legacy       [6]bool `json:"legacy"`
	StatusHz     int     `json:"statusHz"`
	FollowArena  bool    `json:"followArena"`
	AutoConnect  bool    `json:"autoConnect"`
	DefaultTeams [6]int  `json:"defaultTeams"`
	TeamPool     []int   `json:"teamPool"`
}

type PLCConfig struct {
	Bind string `json:"bind"`
	Port int    `json:"port"`
}

type AltIOConfig struct {
	ScoreTableIP string `json:"scoreTableIp"`
	RedIP        string `json:"redIp"`
	BlueIP       string `json:"blueIp"`
	HealthPort   int    `json:"healthPort"`
}

type SACNConfig struct {
	Bind string `json:"bind"`
	Port int    `json:"port"`
}

type APConfig struct {
	Bind     string `json:"bind"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

type SwitchConfig struct {
	Bind     string `json:"bind"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

type SCCConfig struct {
	RedBind  string `json:"redBind"`
	BlueBind string `json:"blueBind"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Default returns the configuration used when no file is present.
func Default() *Config {
	return &Config{
		GUI: GUIConfig{Bind: "127.0.0.1", Port: 8090, Width: 1600, Height: 940},
		Arena: ArenaConfig{
			Mode:          ArenaSandbox,
			SandboxDir:    filepath.Join("run", "arena"),
			Host:          "127.0.0.1",
			HTTPPort:      8080,
			AutoStart:     true,
			AutoConfigure: true,
			PreferBuild:   true,
		},
		Field: FieldConfig{Profile: ProfileAltIO, Variant: "welded", NetworkSecurity: true},
		DS: DSConfig{
			BaseUDPPort:  11210,
			StatusHz:     50,
			FollowArena:  true,
			AutoConnect:  true,
			DefaultTeams: [6]int{254, 1678, 971, 118, 148, 2056},
			TeamPool: []int{
				254, 1678, 971, 118, 148, 2056, 1323, 4414, 3476, 1690, 6328, 2910, 1114, 330, 604, 846, 1072,
				2471, 3005, 3130, 4607, 5940, 8033, 195, 4613, 2169, 2052, 2177, 2228, 2502, 2501, 3058, 3082,
				3184, 3277, 4009,
			},
		},
		PLC:    PLCConfig{Bind: "127.0.0.1", Port: 502},
		AltIO:  AltIOConfig{ScoreTableIP: "127.0.0.5", RedIP: "127.0.0.6", BlueIP: "127.0.0.7", HealthPort: 80},
		SACN:   SACNConfig{Bind: "127.0.0.1", Port: 5568},
		AP:     APConfig{Bind: "127.0.0.1", Port: 8081, Password: "simpass"},
		Switch: SwitchConfig{Bind: "127.0.0.2", Port: 23, Password: "simpass"},
		SCC: SCCConfig{
			RedBind: "127.0.0.3", BlueBind: "127.0.0.4", Port: 22, Username: "admin", Password: "simpass",
		},
	}
}

// Load reads the config file at path, layering it over the defaults. A missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.path = path
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

// Save writes the config back to the file it was loaded from.
func (cfg *Config) Save() error {
	if cfg.path == "" {
		return fmt.Errorf("config has no file path")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfg.path, data, 0644)
}

// Path returns the file the config was loaded from.
func (cfg *Config) Path() string {
	return cfg.path
}

// ArenaURL returns the base HTTP URL of the arena web server.
func (cfg *Config) ArenaURL() string {
	return fmt.Sprintf("http://%s:%d", cfg.Arena.Host, cfg.Arena.HTTPPort)
}

// GUIURL returns the base HTTP URL of the simulator's GUI server.
func (cfg *Config) GUIURL() string {
	return fmt.Sprintf("http://%s:%d", cfg.GUI.Bind, cfg.GUI.Port)
}
