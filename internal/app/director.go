package app

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/scouting"
)

// The event director runs the sandbox arena's schedule like a real event's scorekeeper and field crew would: load the
// next unplayed match, let the robots cart on and the field come ready, start it, let the AI play and the referees
// score it, commit, move on. It reports what an event cares about: cycle time (start to start), how long the field took
// to come ready after each load, start delays, match lengths, and anything that went wrong.

// DirectorMatch is one match the director ran.
type DirectorMatch struct {
	Id         int       `json:"id"`
	Name       string    `json:"name"`
	Teams      [6]int    `json:"teams"`
	LoadedAt   time.Time `json:"loadedAt"`
	StartedAt  time.Time `json:"startedAt"`
	ReadySecs  float64   `json:"readySecs"`  // load → field ready
	StartDelay float64   `json:"startDelay"` // ready → start
	Duration   float64   `json:"duration"`   // start → post-match
	CycleSecs  float64   `json:"cycleSecs"`  // previous start → this start
	Red        int       `json:"red"`
	Blue       int       `json:"blue"`
	Warnings   int       `json:"warnings"`
	Errors     []string  `json:"errors,omitempty"`
	Problem    string    `json:"problem,omitempty"`
	ScoreOK    bool      `json:"scoreOk"`              // the arena's score matched the field (arenadiff.go)
	ScoreDiffs []string  `json:"scoreDiffs,omitempty"` // where it did not
}

// DirectorState is published on director.state.
type DirectorState struct {
	Running   bool            `json:"running"`
	Type      string          `json:"type"`
	Total     int             `json:"total"`
	Current   string          `json:"current"`
	Phase     string          `json:"phase"`
	Matches   []DirectorMatch `json:"matches"`
	AvgCycle  float64         `json:"avgCycle"`
	AvgReady  float64         `json:"avgReady"`
	Anomalies []string        `json:"anomalies"`
	Report    string          `json:"report,omitempty"`
	// Alliance selection and playoffs (event.go).
	PickList  []scouting.TeamRating `json:"pickList,omitempty"`
	Selection []scouting.Step       `json:"selection,omitempty"`
	Alliances [][]int               `json:"alliances,omitempty"`
	Timeouts  []string              `json:"timeouts,omitempty"`
	Winner    string                `json:"winner,omitempty"`
	Scouting  string                `json:"scouting,omitempty"` // scouting CSV path
}

type director struct {
	app       *App
	mu        sync.Mutex
	state     DirectorState
	stop      bool
	events    []bus.Event
	lastStart time.Time
	options   directorOptions
}

// directorOptions tune the event director's playoff and selection behavior.
type directorOptions struct {
	PickSeconds    float64 `json:"pickSeconds"`    // pause between alliance-selection picks (so screens can be watched)
	TimeoutSeconds int     `json:"timeoutSeconds"` // playoff alliance timeout length (real events: several minutes)
	TimeoutChance  float64 `json:"timeoutChance"`  // chance an alliance with its timeout left calls it before a match
	DeclineChance  float64 `json:"declineChance"`
}

func (app *App) registerDirector() {
	d := &director{app: app}
	app.director = d
	app.Hub.OnEvent(func(event bus.Event) {
		d.mu.Lock()
		if d.state.Running && event.Source != "director" && (event.Level == "warn" || event.Level == "error") {
			d.events = append(d.events, event)
		}
		d.mu.Unlock()
	})
	app.Hub.Handle("director.start", func(args []byte) (any, error) {
		request := struct {
			Type  string `json:"type"`
			Limit int    `json:"limit"`
			directorOptions
		}{directorOptions: directorOptions{PickSeconds: 4, TimeoutSeconds: 60, TimeoutChance: 0.2, DeclineChance: 0.35}}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &request); err != nil {
				return nil, err
			}
		}
		if request.Type == "" {
			request.Type = "qualification"
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.state.Running {
			return nil, fmt.Errorf("the event director is already running")
		}
		d.state = DirectorState{Running: true, Type: request.Type, Phase: "starting"}
		d.stop = false
		d.lastStart = time.Time{}
		d.options = request.directorOptions
		matchType, limit := request.Type, request.Limit
		safe.Async(app.Hub, "director", func() { d.run(matchType, limit) })
		return nil, nil
	})
	app.Hub.Handle("director.stop", func(_ []byte) (any, error) {
		d.mu.Lock()
		d.stop = true
		d.mu.Unlock()
		return nil, nil
	})
	d.publish()
}

func (d *director) publish() {
	d.mu.Lock()
	state := d.state
	state.Matches = append([]DirectorMatch(nil), d.state.Matches...)
	state.Anomalies = append([]string(nil), d.state.Anomalies...)
	d.mu.Unlock()
	d.app.Hub.Publish("director.state", state)
}

func (d *director) stopped() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stop
}

func (d *director) setPhase(current, phase string) {
	d.mu.Lock()
	d.state.Current, d.state.Phase = current, phase
	d.mu.Unlock()
	d.publish()
}

func (d *director) anomaly(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	d.mu.Lock()
	d.state.Anomalies = append(d.state.Anomalies, text)
	d.mu.Unlock()
	d.app.Hub.Warnf("director", "%s", text)
	d.app.autoBundle("event director: " + text)
}

type scheduledMatch struct {
	Id        int    `json:"Id"`
	TypeOrder int    `json:"TypeOrder"`
	ShortName string `json:"ShortName"`
	Status    int    `json:"Status"`
	Red1      int    `json:"Red1"`
	Red2      int    `json:"Red2"`
	Red3      int    `json:"Red3"`
	Blue1     int    `json:"Blue1"`
	Blue2     int    `json:"Blue2"`
	Blue3     int    `json:"Blue3"`
}

func (d *director) run(matchType string, limit int) {
	defer func() {
		d.mu.Lock()
		d.state.Running, d.state.Phase, d.state.Current = false, "done", ""
		d.mu.Unlock()
		d.publish()
	}()
	switch matchType {
	case "event":
		// A whole event: qualifications, scouting and alliance selection, then the playoff bracket.
		d.runSchedule("qualification", limit)
		if !d.stopped() {
			d.runSelection()
		}
		if !d.stopped() {
			d.runPlayoffs()
		}
	case "selection":
		d.runSelection()
	case "playoff":
		d.runPlayoffs()
	default:
		d.runSchedule(matchType, limit)
	}
	d.writeReport(matchType)
}

func (d *director) dispatch(command string, args any) error {
	data, _ := json.Marshal(args)
	_, err := d.app.Hub.Dispatch(command, data)
	return err
}

func (d *director) matchState() arena.MatchState { state, _ := d.app.Arena.MatchTime(); return state }

func (d *director) waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && !d.stopped() {
		if cond() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return cond()
}

// runSchedule plays a fixed schedule (practice or qualification) in order.
func (d *director) runSchedule(matchType string, limit int) {
	var schedule []scheduledMatch
	if err := d.app.Arena.GetJSON("/api/matches/"+matchType, &schedule); err != nil {
		d.anomaly("could not read the %s schedule: %v", matchType, err)
		return
	}
	var todo []scheduledMatch
	for _, match := range schedule {
		if match.Status == 0 { // not played yet
			todo = append(todo, match)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].TypeOrder < todo[j].TypeOrder })
	if limit > 0 && len(todo) > limit {
		todo = todo[:limit]
	}
	d.mu.Lock()
	d.state.Total += len(todo)
	d.mu.Unlock()
	if len(todo) == 0 {
		d.app.Hub.Logf("director", "No unplayed %s matches in the schedule", matchType)
		return
	}
	d.app.Hub.Logf("director", "Event director: %d %s matches", len(todo), matchType)
	for _, match := range todo {
		if d.stopped() {
			break
		}
		d.playMatch(match, nil)
	}
}

// playMatch runs one match end to end: clear the field, load, wait for the field crew and readiness, play, let the
// referees score it, check the arena's score against the field, commit. beforeLoad (optional) runs between clearing
// the field and loading, for things like a playoff timeout.
func (d *director) playMatch(match scheduledMatch, beforeLoad func()) DirectorMatch {
	app := d.app
	result := DirectorMatch{Id: match.Id, Name: match.ShortName,
		Teams: [6]int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3}}
	// Clear whatever is on the field first.
	if state := d.matchState(); state != arena.PreMatch {
		if state.InMatch() {
			_ = d.dispatch("match.abort", nil)
		}
		d.waitFor(10*time.Second, func() bool { return d.matchState() == arena.PostMatch })
		_ = d.dispatch("match.discard", nil)
		d.waitFor(10*time.Second, func() bool { return d.matchState() == arena.PreMatch })
	}
	d.mu.Lock()
	d.events = nil
	d.mu.Unlock()
	if beforeLoad != nil {
		beforeLoad()
	}
	d.setPhase(match.ShortName, "loading")
	result.LoadedAt = time.Now()
	if err := d.dispatch("match.load", map[string]int{"matchId": match.Id}); err != nil {
		result.Problem = "load failed: " + err.Error()
	}
	d.setPhase(match.ShortName, "field reset, waiting for the field to be ready")
	time.Sleep(1500 * time.Millisecond)
	// A match loaded during a timeout can only start once the timeout is over. That includes the playoff bracket's
	// scheduled field and awards breaks, which the arena starts by itself (up to 15 minutes).
	// The arena starts those a few seconds after it preloads the match, so watch for one the whole time.
	inBreak := func() bool { s := d.matchState(); return s == arena.TimeoutActive || s == arena.PostTimeout }
	waitBreak := func() {
		d.setPhase(match.ShortName, "waiting out the timeout / scheduled break")
		d.mu.Lock()
		called := len(d.state.Timeouts) > 0 && strings.Contains(d.state.Timeouts[len(d.state.Timeouts)-1], match.ShortName)
		if !called {
			d.state.Timeouts = append(d.state.Timeouts, "The arena ran a scheduled break before "+match.ShortName)
		}
		d.mu.Unlock()
		if !d.waitFor(20*time.Minute, func() bool { return !inBreak() }) {
			result.Problem = "the timeout before the match never ended"
		}
		result.LoadedAt = time.Now() // time to field-ready counts from the end of the break
		d.setPhase(match.ShortName, "field reset, waiting for the field to be ready")
	}
	for result.Problem == "" {
		if inBreak() {
			waitBreak()
			continue
		}
		if d.waitFor(240*time.Second-time.Since(result.LoadedAt), func() bool {
			return inBreak() || d.matchState() == arena.PreMatch && app.Arena.Status().CanStartMatch
		}) && !inBreak() {
			break
		}
		if !inBreak() {
			result.Problem = "field never became ready: " + strings.Join(app.Arena.Status().StartMatchConditions, "; ")
		}
	}
	ready := time.Now()
	result.ReadySecs = ready.Sub(result.LoadedAt).Seconds()
	if result.Problem == "" && !d.stopped() {
		d.setPhase(match.ShortName, "in the match")
		result.StartedAt = time.Now()
		result.StartDelay = result.StartedAt.Sub(ready).Seconds()
		d.mu.Lock()
		if !d.lastStart.IsZero() {
			result.CycleSecs = result.StartedAt.Sub(d.lastStart).Seconds()
		}
		d.lastStart = result.StartedAt
		d.mu.Unlock()
		if err := d.dispatch("match.start", nil); err != nil {
			result.Problem = "start refused: " + err.Error()
		} else if !d.waitFor(220*time.Second, func() bool { return d.matchState() == arena.PostMatch }) {
			result.Problem = "match did not finish within 220 s"
		}
		result.Duration = time.Since(result.StartedAt).Seconds()
		d.setPhase(match.ShortName, "scoring and committing")
		// The referees finish their review and the scoring panels commit; then the arena's score is checked
		// against the field before the match is committed.
		if diff := app.waitArenaDiff(app.World.Seed(), 20*time.Second); diff != nil {
			result.ScoreOK = diff.OK
			for _, line := range diff.Lines {
				if line.Severity == "error" {
					result.ScoreDiffs = append(result.ScoreDiffs, strings.ToUpper(line.Alliance)+": "+line.Text)
				}
			}
		} else {
			result.ScoreDiffs = append(result.ScoreDiffs, "the score check did not run (referees disabled?)")
			time.Sleep(5 * time.Second)
		}
		var score struct {
			Red, Blue struct{ ScoreSummary struct{ Score int } }
		}
		if raw := app.Hub.Get("arena.realtimeScore"); raw != nil {
			_ = json.Unmarshal(raw, &score)
		}
		result.Red, result.Blue = score.Red.ScoreSummary.Score, score.Blue.ScoreSummary.Score
		if d.matchState() == arena.PostMatch {
			if err := d.dispatch("match.commit", nil); err != nil {
				result.Problem = "commit failed: " + err.Error()
			}
		}
	}
	d.mu.Lock()
	for _, event := range d.events {
		if event.Level == "error" && len(result.Errors) < 10 {
			result.Errors = append(result.Errors, event.Source+": "+event.Msg)
		} else if event.Level == "warn" {
			result.Warnings++
		}
	}
	d.state.Matches = append(d.state.Matches, result)
	d.recomputeLocked()
	d.mu.Unlock()
	switch {
	case result.Problem != "":
		d.anomaly("%s: %s", match.ShortName, result.Problem)
	case result.ReadySecs > 150:
		d.anomaly("%s: the field took %.0f s to come ready after the load", match.ShortName, result.ReadySecs)
	case result.Duration > 175:
		d.anomaly("%s: the match ran %.0f s (start to post-match)", match.ShortName, result.Duration)
	case len(result.ScoreDiffs) > 0:
		d.anomaly("%s: the arena's score differs from the field: %s", match.ShortName, strings.Join(result.ScoreDiffs, "; "))
	case len(result.Errors) > 0:
		d.anomaly("%s: %d errors logged (%s)", match.ShortName, len(result.Errors), result.Errors[0])
	}
	app.Hub.Logf("director", "%s done: %d-%d, ready in %.0f s, cycle %.0f s", match.ShortName, result.Red,
		result.Blue, result.ReadySecs, result.CycleSecs)
	d.publish()
	return result
}

func (d *director) writeReport(matchType string) {
	d.mu.Lock()
	matches := append([]DirectorMatch(nil), d.state.Matches...)
	state := d.state
	d.mu.Unlock()
	if len(matches) == 0 && len(state.Alliances) == 0 {
		return
	}
	if path, err := writeEventReport(filepath.Join("run", "reports"), matchType, matches, state); err == nil {
		d.mu.Lock()
		d.state.Report = path
		d.mu.Unlock()
		d.app.Hub.Logf("director", "Event report written to %s", path)
	}
}

func (d *director) recomputeLocked() {
	cycles, readies, n, m := 0.0, 0.0, 0, 0
	for _, match := range d.state.Matches {
		if match.CycleSecs > 0 {
			cycles += match.CycleSecs
			n++
		}
		if match.Problem == "" {
			readies += match.ReadySecs
			m++
		}
	}
	if n > 0 {
		d.state.AvgCycle = cycles / float64(n)
	}
	if m > 0 {
		d.state.AvgReady = readies / float64(m)
	}
}

func writeEventReport(dir, matchType string, matches []DirectorMatch, state DirectorState) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html><head><meta charset="utf-8"><title>Freezy Sim event report</title><style>
body{font:14px system-ui;background:#0e1116;color:#dfe5ee;margin:24px}table{border-collapse:collapse;width:100%%}
td,th{border-bottom:1px solid #2a313c;padding:6px 8px;text-align:left;vertical-align:top}th{color:#8b95a5}
.bad{color:#ff6b6b}.small{font-size:12px;color:#8b95a5}.num{text-align:right;font-variant-numeric:tabular-nums}
</style></head><body><h1>Event director: %s matches</h1><p>%d matches · average cycle %.0f s (%.1f min) · field ready %.0f s after load on average · %s</p>`,
		html.EscapeString(matchType), len(matches), state.AvgCycle, state.AvgCycle/60, state.AvgReady,
		time.Now().Format("2006-01-02 15:04"))
	if len(state.Anomalies) > 0 {
		b.WriteString("<h2>Anomalies</h2><ul>")
		for _, a := range state.Anomalies {
			fmt.Fprintf(&b, `<li class="bad">%s</li>`, html.EscapeString(a))
		}
		b.WriteString("</ul>")
	}
	if state.Winner != "" {
		fmt.Fprintf(&b, "<h2>Winner: %s</h2>", html.EscapeString(state.Winner))
	}
	if len(state.Alliances) > 0 {
		b.WriteString(`<h2>Playoff alliances</h2><table><tr><th>Alliance</th><th>Captain</th><th>Picks</th></tr>`)
		for i, teams := range state.Alliances {
			fmt.Fprintf(&b, `<tr><td>%d</td><td>%d</td><td>%v</td></tr>`, i+1, teams[0], teams[1:])
		}
		b.WriteString("</table>")
	}
	if len(state.Selection) > 0 {
		b.WriteString(`<h2>Alliance selection</h2><ol class="small">`)
		for _, step := range state.Selection {
			fmt.Fprintf(&b, "<li>%s</li>", html.EscapeString(step.Text))
		}
		b.WriteString("</ol>")
	}
	for _, timeout := range state.Timeouts {
		fmt.Fprintf(&b, `<p class="small">%s</p>`, html.EscapeString(timeout))
	}
	if len(state.PickList) > 0 {
		fmt.Fprintf(&b, `<h2>Pick list</h2><p class="small">Scouting sheet: %s</p><table><tr><th>#</th><th>Team</th>`+
			`<th class="num">Rank</th><th class="num">OPR</th><th class="num">Auto OPR</th><th class="num">Tower OPR</th>`+
			`<th class="num">FUEL/match</th><th class="num">Climb pts</th><th class="num">Reliability</th><th class="num">Pick value</th></tr>`,
			html.EscapeString(state.Scouting))
		for i, r := range state.PickList {
			if i >= 30 {
				break
			}
			fmt.Fprintf(&b, `<tr><td>%d</td><td>%d</td><td class="num">%d</td><td class="num">%.1f</td><td class="num">%.1f</td>`+
				`<td class="num">%.1f</td><td class="num">%.1f</td><td class="num">%.1f</td><td class="num">%.0f%%</td><td class="num">%.1f</td></tr>`,
				i+1, r.Team, r.Rank, r.OPR, r.AutoOPR, r.TowerOPR, r.AvgScored, r.AvgClimbPts, r.Reliability*100, r.PickValue)
		}
		b.WriteString("</table>")
	}
	b.WriteString(`<h2>Matches</h2><table><tr><th>Match</th><th>Teams</th><th class="num">Ready</th><th class="num">Start delay</th>` +
		`<th class="num">Length</th><th class="num">Cycle</th><th>Score</th><th>Score check</th><th class="num">Warnings</th>` +
		`<th>Problem / errors</th></tr>`)
	for _, m := range matches {
		problem := html.EscapeString(m.Problem)
		if len(m.Errors) > 0 {
			problem += "<br>" + html.EscapeString(strings.Join(m.Errors, "; "))
		}
		check := `<span style="color:#4cd38a">matches the field</span>`
		if !m.ScoreOK {
			check = `<span class="bad small">` + html.EscapeString(strings.Join(m.ScoreDiffs, "; ")) + `</span>`
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="small">%v</td><td class="num">%.0f s</td><td class="num">%.1f s</td>`+
			`<td class="num">%.0f s</td><td class="num">%.0f s</td><td>%d – %d</td><td>%s</td><td class="num">%d</td><td class="bad small">%s</td></tr>`,
			html.EscapeString(m.Name), m.Teams, m.ReadySecs, m.StartDelay, m.Duration, m.CycleSecs, m.Red, m.Blue,
			check, m.Warnings, problem)
	}
	b.WriteString("</table></body></html>")
	path := filepath.Join(dir, fmt.Sprintf("event-%s-%s.html", matchType, time.Now().Format("20060102-150405")))
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// RunDirector runs the event director headless until it finishes and reports whether it ran without anomalies.
func (app *App) RunDirector(matchType string, limit int) bool {
	data, _ := json.Marshal(map[string]any{"type": matchType, "limit": limit})
	if _, err := app.Hub.Dispatch("director.start", data); err != nil {
		app.Hub.Errorf("director", "%v", err)
		return false
	}
	for {
		time.Sleep(time.Second)
		app.director.mu.Lock()
		running, anomalies := app.director.state.Running, len(app.director.state.Anomalies)
		app.director.mu.Unlock()
		if !running {
			return anomalies == 0
		}
	}
}
