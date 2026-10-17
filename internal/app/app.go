// Package app wires every simulator subsystem together so the GUI and headless modes share one setup.
package app

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/chaos"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/ds"
	"github.com/Agame7k/freezy-sim/internal/fieldio"
	"github.com/Agame7k/freezy-sim/internal/fta"
	"github.com/Agame7k/freezy-sim/internal/gui"
	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/launcher"
	"github.com/Agame7k/freezy-sim/internal/netsim"
	"github.com/Agame7k/freezy-sim/internal/recorder"
	"github.com/Agame7k/freezy-sim/internal/referee"
	"github.com/Agame7k/freezy-sim/internal/sacn"
	"github.com/Agame7k/freezy-sim/internal/scenario"
	"github.com/Agame7k/freezy-sim/internal/version"
	"github.com/Agame7k/freezy-sim/internal/winui"
	"github.com/Agame7k/freezy-sim/internal/world"
	"github.com/Agame7k/freezy-sim/web"
)

type Options struct {
	Suite      string
	Tests      []string // scenario ids to run (empty = the whole suite)
	Soak       int      // matches to play back to back, then exit
	Director   string   // run the event director headless (qualification, playoff, selection, event), then exit
	DirectorN  int      // -director: at most this many scheduled matches (0 = all)
	SoakChaos  int
	ConfigPath string
	WebDir     string
	Headless   bool
	Debug      bool
	NoArena    bool
}

// App owns every subsystem.
//
// Lock order (a goroutine holding a lock may only take locks further down this list, never back up it):
//
//	world.mu → ds Station.mu (Command/SetLoad) → netsim AP.mu (radio gate)
//	world.mu → fieldio Field.mu (ScoreFuel, hub coils)
//
// Rules that keep it that way: the AP gathers station info (Station.mu, world.mu) without holding AP.mu; the field
// calls its match-reset listeners after releasing Field.mu; world.WithRobot callbacks only touch the robot (never
// dispatch commands, which may take world.mu again); bus.Hub never calls out while holding its own lock except to
// write to subscriber channels without blocking. The stall watchdog writes a goroutine dump if this is ever violated.
type App struct {
	Options  Options
	Config   *config.Config
	Hub      *bus.Hub
	Launcher *launcher.Launcher
	Arena    *arena.Client
	DS       *ds.Manager
	FieldIO  *fieldio.Manager
	SACN     *sacn.Receiver
	World    *world.World
	Network  *netsim.Manager
	Referee  *referee.Referee
	FTA      *fta.FTA
	Chaos    *chaos.Chaos
	Tests    *scenario.Runner
	Recorder *recorder.Recorder
	Replay   *recorder.Player
	GUI      *gui.Server

	listener             net.Listener
	startedAt            time.Time
	settingsContributors []SettingsContributor
	stalls               atomic.Int64 // physics stalls caught by the watchdog
	diff                 *diffState   // arena-diff results (arenadiff.go)
	director             *director

	bundleMu       sync.Mutex // automatic bug-report bundles (bundle.go)
	bundlesAuto    int
	lastAutoBundle time.Time

	testMu        sync.Mutex
	lastTestTeams [6]int    // teams of the last quick test match
	fillingTest   time.Time // when QuickTestMatch last ran (so its own match load is not refilled)
}

// New builds the simulator and binds its GUI port.
func New(options Options) (*App, error) {
	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	hub := bus.New()
	app := &App{Options: options, Config: cfg, Hub: hub, startedAt: time.Now()}

	app.Launcher = launcher.New(cfg, hub)
	app.Arena = arena.NewClient(cfg, hub)
	app.DS = ds.NewManager(cfg, hub, app.Arena)
	app.FieldIO = fieldio.NewManager(cfg, hub, app.Arena, func(station int) bool {
		return app.DS.Stations[station].EthernetConnected()
	})
	app.AddSettingsContributor(app.FieldIO.ContributeSettings)
	app.SACN = sacn.New(cfg, hub)
	app.AddSettingsContributor(app.SACN.ContributeSettings)
	inspector.Register(hub)
	layout, err := world.LoadLayout(web.Files)
	if err != nil {
		return nil, fmt.Errorf("loading field layout: %w", err)
	}
	app.World = world.New(layout, hub)
	if err := app.World.LoadState(filepath.Join("run", "state.json")); err != nil {
		hub.Warnf("world", "Could not read saved robot designs (starting fresh): %v", err)
	}
	var links [6]world.StationLink
	for i, station := range app.DS.Stations {
		links[i] = station
	}
	app.World.Attach(links, app.FieldIO.Field)
	for i, robot := range app.World.Robots() {
		app.World.SetController(i, world.NewAgent(robot, int64(i+1)))
	}
	// The world times shots and keeps its scoreboard with the arena's own match clock.
	app.World.SetMatchClock(func() (float64, bool) {
		state, seconds := app.Arena.MatchTime()
		return seconds, state.InMatch() || state == arena.PostMatch
	})
	app.Arena.OnMatchLoad(func(arena.MatchLoad) {
		// A new match: one seed drives its AI, natural mistakes, field reset and chaos faults (enter it under the AI
		// settings to play the same match again).
		seed := app.World.NewMatchSeed()
		if app.Chaos != nil {
			app.Chaos.Reseed(seed + 7)
		}
		hub.Logf("world", "Match seed %d", seed)
		// The next teams bring their robots on (and whatever the field signals did not start, starts now).
		go func() {
			time.Sleep(600 * time.Millisecond)
			app.World.FieldReset()
		}()
	})
	// The head referee's field signals, from the arena's hub LED modes: purple (field cleanup) sends the reset crew
	// on to restage FUEL; green (field safe) lets the drive teams take their robots off.
	hub.OnTopic(func(topic string, data json.RawMessage) {
		if topic != "arena.ledModes" {
			return
		}
		var modes arena.LedModes
		if json.Unmarshal(data, &modes) != nil {
			return
		}
		switch {
		case modes.RedMode == arena.LedFieldCleanup && modes.BlueMode == arena.LedFieldCleanup:
			go app.World.FieldCleanup() // (not on the bus goroutine: the world publishes under its own lock)
		case modes.RedMode == arena.LedFieldSafe && modes.BlueMode == arena.LedFieldSafe:
			go app.World.FieldSafe()
		}
	})
	// The PLC's winner light show waits for the audience display's winner video to reveal the result; how long that
	// takes depends on the screen the display leaves.
	hub.OnTopic(func(topic string, data json.RawMessage) {
		if topic != "arena.audienceDisplay" {
			return
		}
		var display arena.AudienceDisplay
		if json.Unmarshal(data, &display) == nil && display.Mode == "score" {
			delay := arena.ScoreRevealDelay(display.Previous)
			app.FieldIO.Field.SetRevealDelay(delay)
			hub.Logf("field", "Audience display %s → score: winner revealed in %.1f s (PLC light show delay-on)",
				display.Previous, delay.Seconds())
		}
	})
	app.Network = netsim.NewManager(cfg, hub, func(index int) netsim.StationInfo {
		station := app.DS.Stations[index]
		x, y, enabled := app.World.RobotPose(index)
		return netsim.StationInfo{Team: station.TeamNumber(), RadioUp: station.RadioPowered(), X: x, Y: y, Enabled: enabled}
	})
	app.DS.SetNetworkGates(app.Network.RadioAllowed, app.Network.EthernetUp)
	app.AddSettingsContributor(app.Network.ContributeSettings)
	app.Referee = referee.New(hub, app.Arena, app.World, filepath.Join("run", "reports"))
	app.Chaos = chaos.New(hub, app.Arena, app.World, app.Referee)
	app.FTA = fta.New(hub, app.Arena)
	app.FTA.SetFieldCheck(func() string {
		switch carting, crew := app.World.FieldResetBusy(); {
		case carting:
			return "robots still being carted on or off"
		case crew:
			return "field reset crew still restaging FUEL"
		}
		return ""
	})
	app.Tests = scenario.NewRunner(scenario.Deps{
		Hub: hub, Arena: app.Arena, DS: app.DS, FieldIO: app.FieldIO, World: app.World, NetworkEnabled: app.Network.Enabled,
	})
	app.Tests.OnDone(func(results []*scenario.Result) {
		htmlPath, xmlPath, err := scenario.WriteReports(filepath.Join("run", "reports"), results)
		if err != nil {
			hub.Errorf("tests", "Could not write test reports: %v", err)
			return
		}
		hub.Logf("tests", "Reports written: %s and %s", htmlPath, xmlPath)
		hub.Publish("tests.reportPath", htmlPath)
		var failed []string
		for _, result := range results {
			if result.Status == "fail" {
				failed = append(failed, result.Id)
			}
		}
		if len(failed) > 0 {
			app.autoBundle("acceptance failures: " + strings.Join(failed, ", "))
		}
	})
	// The black box records the live world; the GUI watches a stream the replay player can take over.
	app.Recorder = recorder.New(hub, filepath.Join("run", "recordings"))
	app.Recorder.RegisterCommands()
	worldView := recorder.NewStream(app.World)
	app.Replay = recorder.NewPlayer(hub, worldView)
	app.Replay.RegisterCommands(app.Recorder)
	app.Arena.OnMatchState(func(old, new arena.MatchState) {
		switch {
		case new.InMatch() && !old.InMatch():
			label := "Match"
			if match := app.Arena.MatchLoad().Match; match != nil && match.LongName != "" {
				label = match.LongName
			}
			app.Recorder.MatchStarted(fmt.Sprintf("%s (seed %d)", label, app.World.Seed()), app.Arena.Teams())
		case new == arena.PostMatch && old.InMatch():
			app.Recorder.MatchEnded()
		}
	})
	app.GUI = gui.New(cfg, hub, web.Files, options.WebDir)
	app.GUI.SetWorldStream(worldView)

	app.listener, err = app.GUI.Listen()
	if err != nil {
		return nil, fmt.Errorf("GUI port %d is unavailable (is another freezy-sim running?): %w", cfg.GUI.Port, err)
	}
	app.registerCommands()
	app.registerSetupCommands()
	app.registerDirector()
	app.registerBundle()
	app.registerArenaDiff()
	app.registerTraining()
	return app, nil
}

// Start launches all background subsystems.
func (app *App) Start() {
	go func() {
		if err := app.GUI.Serve(app.listener); err != nil {
			app.Hub.Errorf("gui", "GUI server stopped: %v", err)
		}
	}()
	app.preflightPorts()
	app.Arena.Run()
	app.DS.Run()
	app.FieldIO.Run()
	app.SACN.Run()
	app.World.Run()
	app.Network.Run()
	app.Referee.Run()
	app.Chaos.Run()
	app.FTA.Run()
	app.Recorder.Run(app.World)
	if app.Config.Arena.AutoStart && !app.Options.NoArena {
		go app.Launcher.Start()
	}
	app.publishInfo()
	go app.stallWatchdog()
	app.Hub.Logf("sim", "Freezy Sim started; GUI at %s", app.Config.GUIURL())
}

// RunSuite waits for the arena and field to be ready, runs the acceptance suite, and returns whether it passed.
func (app *App) RunSuite() bool {
	app.Hub.Logf("tests", "Waiting for the arena and field to come up before running the suite")
	app.WaitArenaReady()
	done := make(chan []*scenario.Result, 1)
	app.Tests.OnDone(func(results []*scenario.Result) { done <- results })
	if err := app.Tests.Start(app.Options.Tests); err != nil {
		app.Hub.Errorf("tests", "%v", err)
		return false
	}
	results := <-done
	time.Sleep(time.Second) // let the report writer finish
	passed := true
	for _, r := range results {
		if r.Status == "fail" {
			passed = false
		}
	}
	return passed
}

// WaitArenaReady waits (up to 3 minutes) for the arena to connect and the field to be startable.
func (app *App) WaitArenaReady() bool {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if app.Arena.Connected() && app.Arena.Status().CanStartMatch {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// stallWatchdog dumps every goroutine to run/ if the physics loop stops advancing (e.g. a deadlock), so hangs are
// diagnosable instead of looking like stuck robots.
func (app *App) stallWatchdog() {
	steps := make(chan int64, 1)
	var last int64 = -1
	stalledSince := time.Time{}
	dumped := false
	for range time.Tick(time.Second) {
		go func() { steps <- app.World.Steps() }()
		var current int64
		select {
		case current = <-steps:
		case <-time.After(2 * time.Second):
			current = last // could not even take the world lock
		}
		if current != last {
			last = current
			stalledSince = time.Time{}
			dumped = false
			continue
		}
		if stalledSince.IsZero() {
			stalledSince = time.Now()
		}
		if !dumped && time.Since(stalledSince) > 3*time.Second {
			dumped = true
			app.stalls.Add(1)
			path := filepath.Join("run", fmt.Sprintf("stall-%s.txt", time.Now().Format("20060102-150405")))
			if file, err := os.Create(path); err == nil {
				_ = pprof.Lookup("goroutine").WriteTo(file, 2)
				_ = file.Close()
			}
			app.Hub.Errorf("sim", "Physics loop stalled for 3 s; goroutine dump written to %s", path)
		}
	}
}

// Stop shuts down background subsystems (the arena sandbox is terminated).
func (app *App) Stop() {
	app.DS.Stop()
	app.FieldIO.Stop()
	_ = app.Launcher.Stop()
}

func (app *App) publishInfo() {
	hostname, _ := os.Hostname()
	app.Hub.Publish("sim.info", map[string]any{
		"version":    Version,
		"startedAt":  app.startedAt,
		"headless":   app.Options.Headless,
		"native":     winui.Available && !app.Options.Headless,
		"guiUrl":     app.Config.GUIURL(),
		"arenaUrl":   app.Config.ArenaURL(),
		"profile":    app.Config.Field.Profile,
		"variant":    app.Config.Field.Variant,
		"configPath": app.Config.Path(),
		"host":       hostname,
		"goVersion":  runtime.Version(),
	})
}

func (app *App) registerCommands() {
	app.Hub.Handle("window.open", func(args []byte) (any, error) {
		var request struct {
			URL    string `json:"url"`
			Title  string `json:"title"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if app.Options.Headless || !winui.Available {
			return map[string]bool{"native": false}, nil
		}
		if request.Width == 0 {
			request.Width, request.Height = 1280, 720
		}
		winui.Open(request.Title, request.URL, request.Width, request.Height, app.Options.Debug)
		return map[string]bool{"native": true}, nil
	})
	app.Hub.Handle("window.list", func(_ []byte) (any, error) { return winui.List(), nil })
	app.Hub.Handle("window.close", func(args []byte) (any, error) {
		var request struct {
			Id int `json:"id"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		winui.Close(request.Id)
		return nil, nil
	})
	app.Hub.Handle("sim.commands", func(_ []byte) (any, error) { return app.Hub.Commands(), nil })
}

// Version is the simulator version string (set at build time in internal/version).
var Version = version.Version
