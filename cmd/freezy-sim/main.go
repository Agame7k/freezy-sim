// Freezy Sim: a full field simulator for Freezy Arena.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Agame7k/freezy-sim/internal/app"
	"github.com/Agame7k/freezy-sim/internal/bench"
	"github.com/Agame7k/freezy-sim/internal/logfile"
	"github.com/Agame7k/freezy-sim/internal/version"
	"github.com/Agame7k/freezy-sim/internal/winui"
	"github.com/Agame7k/freezy-sim/internal/world"
	"github.com/Agame7k/freezy-sim/web"
)

func init() {
	// WebView2 windows must be created and pumped on the main OS thread.
	runtime.LockOSThread()
}

func main() {
	var options app.Options
	flag.StringVar(&options.ConfigPath, "config", "freezy-sim.json", "Path to the simulator config file")
	flag.StringVar(&options.WebDir, "webdir", "", "Serve the GUI from this directory instead of the embedded copy")
	flag.BoolVar(&options.Headless, "headless", false, "Run without opening a window")
	flag.BoolVar(&options.Debug, "debug", false, "Enable WebView2 developer tools")
	flag.BoolVar(&options.NoArena, "no-arena", false, "Do not launch the sandboxed arena automatically")
	flag.StringVar(&options.Suite, "suite", "", "Run a test suite (\"acceptance\") headless, write reports to run/reports, and exit")
	benchMatches := flag.Int("bench-matches", 10, "Matches for -suite ai")
	benchSeed := flag.Int64("bench-seed", 1, "Random seed for -suite ai (same seed, same matches)")
	benchPerfect := flag.Bool("bench-perfect-info", false, "-suite ai: let the AI see every ball")
	benchChaos := flag.Bool("bench-chaos", false, "-suite ai: inject random robot faults")
	benchBaseline := flag.Bool("bench-save-baseline", false, "-suite ai: save this run as the baseline later runs compare to")
	benchWorkers := flag.Int("bench-workers", runtime.NumCPU()/2, "-suite ai: independent events to play side by side")
	flag.IntVar(&options.Soak, "soak", 0, "Play this many AI matches back to back headless, write run/reports/soak-*.html, and exit")
	flag.IntVar(&options.SoakChaos, "soak-chaos", 2, "Chaos level for -soak (0-3)")
	tests := flag.String("tests", "", "-suite acceptance: comma-separated scenario ids to run (default: all)")
	flag.StringVar(&options.Director, "director", "", "Run the event director headless and exit: qualification, practice, "+
		"selection, playoff or event (quals, alliance selection, playoffs); exits 1 on any anomaly")
	flag.IntVar(&options.DirectorN, "director-limit", 0, "-director: at most this many scheduled matches (0 = all)")
	arenaRef := flag.String("arena-ref", "", "Build the sandbox arena from this git ref of the arena folder (exported with "+
		"git archive; the arena folder is only read). \"worktree\" = the folder as it is")
	compare := flag.String("compare", "", "Comma-separated arena refs (e.g. v1.4,HEAD,worktree): run the acceptance suite "+
		"(or -tests) against each, write run/reports/compare-*.html, exit 1 if a scenario regressed from the first ref")
	showVersion := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Describe())
		return
	}
	var compareRefs []string
	for _, ref := range strings.Split(*compare, ",") {
		if ref = strings.TrimSpace(ref); ref != "" {
			compareRefs = append(compareRefs, ref)
		}
	}
	if len(compareRefs) > 0 {
		options.Headless = true
		options.NoArena = true // RunCompare builds and starts each version itself
	}
	for _, id := range strings.Split(*tests, ",") {
		if id = strings.TrimSpace(id); id != "" {
			options.Tests = append(options.Tests, id)
		}
	}
	if options.Suite != "" || options.Soak > 0 || options.Director != "" {
		options.Headless = true
	}
	findHome(options.ConfigPath)
	gui := !options.Headless && winui.Available
	if gui && !winui.SingleInstance(mainTitle) {
		return // already running: its window was brought to the front
	}
	if logs, err := logfile.Setup(filepath.Join("run", "logs"), true); err != nil {
		log.Printf("Could not open a log file: %v", err)
	} else {
		defer logs.Close()
		log.Printf("%s starting; logging to %s", version.Describe(), logs.Path())
	}

	if options.Suite == "ai" {
		// The AI benchmark runs entirely inside the world model: no arena, no network ports.
		os.Exit(runAIBench(world.BenchOptions{Matches: *benchMatches, Seed: *benchSeed, PerfectInfo: *benchPerfect,
			Chaos: *benchChaos, Workers: *benchWorkers}, *benchBaseline))
	}

	sim, err := app.New(options)
	if err != nil {
		if gui {
			winui.Alert("Freezy Sim could not start", err.Error())
		}
		log.Fatalf("Startup failed: %v", err)
	}
	if *arenaRef != "" {
		sim.Launcher.SetRef(*arenaRef)
	}
	sim.Start()

	if len(compareRefs) > 0 {
		clean := sim.RunCompare(compareRefs, options.Tests)
		sim.Stop()
		if !clean {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if options.Soak > 0 {
		if !sim.WaitArenaReady() {
			log.Printf("The arena never became ready; not running the soak test")
			sim.Stop()
			os.Exit(1)
		}
		clean := sim.RunSoak(options.Soak, options.SoakChaos)
		sim.Stop()
		if !clean {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if options.Director != "" {
		if !sim.WaitArenaReady() {
			log.Printf("The arena never became ready; not running the event director")
			sim.Stop()
			os.Exit(1)
		}
		clean := sim.RunDirector(options.Director, options.DirectorN)
		sim.Stop()
		if !clean {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if options.Suite != "" {
		passed := sim.RunSuite()
		sim.Stop()
		if !passed {
			os.Exit(1)
		}
		os.Exit(0)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	if !options.Headless && winui.Available {
		go func() {
			<-signals
			sim.Stop()
			os.Exit(0)
		}()
		cfg := sim.Config
		if !winui.RunMain(mainTitle, cfg.GUIURL()+"/", cfg.GUI.Width, cfg.GUI.Height,
			options.Debug) {
			log.Printf("WebView2 is unavailable (install the runtime from "+
				"https://developer.microsoft.com/microsoft-edge/webview2/); meanwhile open %s in a browser. "+
				"Press Ctrl+C to quit.", cfg.GUIURL())
			<-signals
		}
	} else {
		log.Printf("Running headless; GUI available at %s. Press Ctrl+C to quit.", sim.Config.GUIURL())
		<-signals
	}
	sim.Stop()
}

func runAIBench(options world.BenchOptions, saveBaseline bool) int {
	layout, err := world.LoadLayout(web.Files)
	if err != nil {
		log.Printf("Could not load the field layout: %v", err)
		return 1
	}
	log.Printf("AI benchmark: %d matches, seed %d, %d events side by side", options.Matches, options.Seed,
		max(1, options.Workers))
	started := time.Now()
	report, path, err := bench.Run(layout, options, filepath.Join("run", "reports"), saveBaseline, func(m world.BenchMatch) {
		log.Printf("  match %d: %s %d - %d %s", m.Number, m.Alliances[0].Plan, m.Alliances[0].Score,
			m.Alliances[1].Score, m.Alliances[1].Plan)
	})
	if err != nil {
		log.Printf("Could not write the benchmark report: %v", err)
		return 1
	}
	log.Printf("%s", bench.SummaryText(report))
	log.Printf("Report written to %s (%.1f s)", path, time.Since(started).Seconds())
	return 0
}

const mainTitle = "Freezy Sim — 2026 Field Simulator"

// findHome moves into the simulator's folder (where freezy-sim.json, web/ and run/ live) when started from elsewhere,
// e.g. a double-clicked bin/freezy-sim.exe or a shortcut.
func findHome(configPath string) {
	if filepath.IsAbs(configPath) {
		return
	}
	if _, err := os.Stat(configPath); err == nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	for _, candidate := range []string{dir, filepath.Dir(dir)} {
		if _, err := os.Stat(filepath.Join(candidate, configPath)); err == nil {
			_ = os.Chdir(candidate)
			return
		}
	}
	_ = os.Chdir(dir) // first run: keep run/ and the config next to the exe
}
