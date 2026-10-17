package app

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
)

// SoakMatch is one match in a soak run.
type SoakMatch struct {
	Number      int            `json:"number"`
	Teams       [6]int         `json:"teams"`
	Started     time.Time      `json:"started"`
	Duration    float64        `json:"duration"` // seconds from start to post-match
	RedScore    int            `json:"redScore"`
	BlueScore   int            `json:"blueScore"`
	Stalls      int64          `json:"stalls"`
	Goroutines  int            `json:"goroutines"`
	HeapMB      float64        `json:"heapMB"`
	Warnings    map[string]int `json:"warnings"` // by source
	Errors      []string       `json:"errors"`
	Problem     string         `json:"problem,omitempty"` // why the match could not run
	Reports     int            `json:"reports"`           // referee match reports written so far
	ChaosFaults int            `json:"chaosFaults"`
	// The arena's score against the field (arenadiff.go), and how the arena handled each chaos fault (recovery checks).
	ScoreOK       bool     `json:"scoreOk"`
	ScoreDiffs    []string `json:"scoreDiffs,omitempty"`
	RecoveryPass  int      `json:"recoveryPass"`
	RecoveryFails []string `json:"recoveryFails,omitempty"`
}

// RunSoak plays back-to-back AI matches through the real arena (with chaos faults and the auto-referee) and records
// stability numbers for each: stall-watchdog trips, goroutine count and heap growth, warnings/errors by subsystem, and
// match durations. It writes run/reports/soak-*.html and returns whether every match ran cleanly.
func (app *App) RunSoak(matches, chaosLevel int) bool {
	app.Hub.Logf("soak", "Soak test: %d matches at chaos level %d", matches, chaosLevel)
	var mu sync.Mutex
	warnings := map[string]int{}
	var errors []string
	recording := false
	app.Hub.OnEvent(func(event bus.Event) {
		mu.Lock()
		defer mu.Unlock()
		if !recording || event.Source == "soak" {
			return
		}
		switch event.Level {
		case "warn":
			warnings[event.Source]++
		case "error":
			if len(errors) < 50 {
				errors = append(errors, event.Source+": "+event.Msg)
			}
		}
	})
	dispatch := func(command string, args any) error {
		data, _ := json.Marshal(args)
		_, err := app.Hub.Dispatch(command, data)
		return err
	}
	matchState := func() arena.MatchState { state, _ := app.Arena.MatchTime(); return state }
	waitFor := func(timeout time.Duration, cond func() bool) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if cond() {
				return true
			}
			time.Sleep(200 * time.Millisecond)
		}
		return cond()
	}

	// Rotate the team pool so later matches bring new robot designs and drive teams onto the field.
	pool := append(append([]int{}, app.Config.DS.DefaultTeams[:]...), app.Config.DS.TeamPool...)
	teamsFor := func(n int) [6]int {
		var teams [6]int
		for i := range teams {
			teams[i] = pool[(n*6+i)%len(pool)]
		}
		return teams
	}
	_ = dispatch("chaos.config", map[string]any{"level": chaosLevel})

	var results []SoakMatch
	startGoroutines := runtime.NumGoroutine()
	var startHeap float64
	allClean := true
	for n := 0; n < matches; n++ {
		result := SoakMatch{Number: n + 1, Teams: teamsFor(n), Warnings: map[string]int{}}
		// Field reset: clear the previous match and load the next teams.
		if state := matchState(); state != arena.PreMatch {
			if state.InMatch() {
				_ = dispatch("match.abort", nil)
			}
			waitFor(10*time.Second, func() bool { return matchState() == arena.PostMatch })
			_ = dispatch("match.discard", nil)
			waitFor(10*time.Second, func() bool { return matchState() == arena.PreMatch })
		}
		if err := app.QuickTestMatch(result.Teams); err != nil {
			result.Problem = "could not load the match: " + err.Error()
		}
		time.Sleep(1500 * time.Millisecond)
		if result.Problem == "" && !waitFor(200*time.Second, // pre-match friction: a team can be ~2 min late
			func() bool { return app.Arena.Status().CanStartMatch }) {
			result.Problem = "field never became ready: " + strings.Join(app.Arena.Status().StartMatchConditions, "; ")
		}
		mu.Lock()
		recording, warnings, errors = true, map[string]int{}, nil
		mu.Unlock()
		stallsBefore := app.stalls.Load()
		if result.Problem == "" {
			result.Started = time.Now()
			if err := dispatch("match.start", nil); err != nil {
				result.Problem = "start refused: " + err.Error()
			} else if !waitFor(200*time.Second, func() bool { return matchState() == arena.PostMatch }) {
				result.Problem = "match did not finish within 200 s"
			}
			result.Duration = time.Since(result.Started).Seconds()
			// Let the auto-referee finish its end-of-match assessment and commit the scoring panels; the arena's score
			// is then checked against the field.
			if diff := app.waitArenaDiff(app.World.Seed(), 20*time.Second); diff != nil {
				result.ScoreOK = diff.OK
				for _, line := range diff.Lines {
					if line.Severity == "error" {
						result.ScoreDiffs = append(result.ScoreDiffs, strings.ToUpper(line.Alliance)+": "+line.Text)
					}
				}
			} else {
				time.Sleep(4 * time.Second)
				result.ScoreDiffs = []string{"the score check did not run"}
			}
			// Chaos recovery checks still running get a moment to finish (a reboot can outlast the match).
			waitFor(25*time.Second, func() bool {
				for _, check := range app.Chaos.ChecksSince(result.Started) {
					if check.Status == "waiting" {
						return false
					}
				}
				return true
			})
			for _, check := range app.Chaos.ChecksSince(result.Started) {
				switch check.Status {
				case "pass":
					result.RecoveryPass++
				case "fail":
					result.RecoveryFails = append(result.RecoveryFails, fmt.Sprintf("%s on %s: %s", check.Fault, check.Station, check.Step))
				}
			}
			var score struct {
				Red, Blue struct{ ScoreSummary struct{ Score int } }
			}
			if raw := app.Hub.Get("arena.realtimeScore"); raw != nil {
				_ = json.Unmarshal(raw, &score)
			}
			result.RedScore, result.BlueScore = score.Red.ScoreSummary.Score, score.Blue.ScoreSummary.Score
			if matchState() == arena.PostMatch {
				if err := dispatch("match.commit", nil); err != nil {
					app.Hub.Warnf("soak", "Commit failed: %v", err)
				}
			}
		}
		mu.Lock()
		recording = false
		for source, count := range warnings {
			result.Warnings[source] = count
		}
		result.Errors = append([]string{}, errors...)
		mu.Unlock()
		result.Stalls = app.stalls.Load() - stallsBefore
		runtime.GC()
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		result.Goroutines = runtime.NumGoroutine()
		result.HeapMB = float64(memory.HeapAlloc) / (1 << 20)
		if n == 0 {
			startHeap = result.HeapMB
		}
		result.Reports = len(app.Referee.Reports())
		if result.Problem != "" || result.Stalls > 0 || len(result.RecoveryFails) > 0 || (result.Problem == "" && !result.ScoreOK) {
			allClean = false
			app.autoBundle(fmt.Sprintf("soak match %d: %s", n+1, soakProblemText(result)))
		}
		app.Hub.Logf("soak", "Match %d/%d: %s, %.0f s, score %d-%d, %d goroutines, %.1f MB heap, %d stalls",
			n+1, matches, map[bool]string{true: "OK", false: "PROBLEM " + result.Problem}[result.Problem == ""],
			result.Duration, result.RedScore, result.BlueScore, result.Goroutines, result.HeapMB, result.Stalls)
		results = append(results, result)
	}
	_ = dispatch("chaos.config", map[string]any{"level": 0})

	// Goroutine or heap growth across the run points to a leak.
	if len(results) >= 3 {
		last := results[len(results)-1]
		if last.Goroutines > startGoroutines+60 {
			allClean = false
			app.Hub.Errorf("soak", "Goroutines grew from %d to %d over the soak run", startGoroutines, last.Goroutines)
		}
		if last.HeapMB > startHeap*2+50 {
			allClean = false
			app.Hub.Errorf("soak", "Heap grew from %.1f MB to %.1f MB over the soak run", startHeap, last.HeapMB)
		}
	}
	path, err := writeSoakReport(filepath.Join("run", "reports"), results, chaosLevel, startGoroutines, allClean)
	if err != nil {
		app.Hub.Errorf("soak", "Could not write the soak report: %v", err)
	} else {
		app.Hub.Logf("soak", "Soak report written to %s", path)
		app.Hub.Publish("soak.reportPath", path)
	}
	return allClean
}

func writeSoakReport(dir string, results []SoakMatch, chaosLevel, startGoroutines int, clean bool) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	verdict := `<span class="pass">PASS</span>`
	if !clean {
		verdict = `<span class="fail">PROBLEMS</span>`
	}
	fmt.Fprintf(&b, `<!doctype html><html><head><meta charset="utf-8"><title>Freezy Sim soak test</title><style>
body{font:14px system-ui;background:#0e1116;color:#dfe5ee;margin:24px}table{border-collapse:collapse;width:100%%}
td,th{border-bottom:1px solid #2a313c;padding:6px 8px;text-align:left;vertical-align:top}th{color:#8b95a5}
.pass{color:#4cd38a;font-weight:700}.fail{color:#ff6b6b;font-weight:700}.small{font-size:12px;color:#8b95a5}
</style></head><body><h1>Soak test %s</h1><p>%d matches at chaos level %d · started with %d goroutines · %s</p><table>
<tr><th>#</th><th>Teams</th><th>Result</th><th>Duration</th><th>Score</th><th>Score check</th><th>Chaos recovery</th><th>Stalls</th><th>Goroutines</th><th>Heap</th><th>Warnings</th><th>Errors</th></tr>`,
		verdict, len(results), chaosLevel, startGoroutines, time.Now().Format("2006-01-02 15:04"))
	for _, r := range results {
		status := `<span class="pass">OK</span>`
		if r.Problem != "" {
			status = `<span class="fail">` + html.EscapeString(r.Problem) + `</span>`
		}
		var sources []string
		for source, count := range r.Warnings {
			sources = append(sources, fmt.Sprintf("%s ×%d", source, count))
		}
		sort.Strings(sources)
		var errs []string
		for _, e := range r.Errors {
			errs = append(errs, html.EscapeString(e))
		}
		check := `<span class="pass">matches</span>`
		if !r.ScoreOK {
			check = `<span class="fail small">` + html.EscapeString(strings.Join(r.ScoreDiffs, "; ")) + `</span>`
		}
		recovery := fmt.Sprintf("%d passed", r.RecoveryPass)
		if len(r.RecoveryFails) > 0 {
			recovery += `<br><span class="fail small">` + html.EscapeString(strings.Join(r.RecoveryFails, "; ")) + `</span>`
		}
		fmt.Fprintf(&b, "<tr><td>%d</td><td class=small>%v</td><td>%s</td><td>%.0f s</td><td>%d – %d</td><td>%s</td><td class=small>%s</td><td>%d</td><td>%d</td><td>%.1f MB</td><td class=small>%s</td><td class=small>%s</td></tr>\n",
			r.Number, r.Teams, status, r.Duration, r.RedScore, r.BlueScore, check, recovery, r.Stalls, r.Goroutines, r.HeapMB,
			strings.Join(sources, "<br>"), strings.Join(errs, "<br>"))
	}
	b.WriteString("</table></body></html>")
	path := filepath.Join(dir, fmt.Sprintf("soak-%s.html", time.Now().Format("20060102-150405")))
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// soakProblemText says in a few words what went wrong in a soak match.
func soakProblemText(result SoakMatch) string {
	var parts []string
	if result.Problem != "" {
		parts = append(parts, result.Problem)
	}
	if result.Stalls > 0 {
		parts = append(parts, fmt.Sprintf("%d physics stalls", result.Stalls))
	}
	if len(result.RecoveryFails) > 0 {
		parts = append(parts, "recovery: "+strings.Join(result.RecoveryFails, "; "))
	}
	if result.Problem == "" && !result.ScoreOK {
		parts = append(parts, "arena score differs from the field: "+strings.Join(result.ScoreDiffs, "; "))
	}
	return strings.Join(parts, "; ")
}
