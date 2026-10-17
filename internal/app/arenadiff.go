package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/world"
)

// Arena-diff: after every match, compare what really happened on the field (the simulator's own record: every FUEL
// the hub sensors counted and when, tower levels, the fouls the referees called) with what the arena scored. A
// difference means the arena (or the field I/O feeding it) lost, double-counted or misattributed something — the
// kind of bug that is otherwise invisible until a team disputes a score.

// DiffLine is one finding.
type DiffLine struct {
	Severity string `json:"severity"` // error, warn, info
	Area     string `json:"area"`     // hub, shift, tower, fouls
	Alliance string `json:"alliance,omitempty"`
	Text     string `json:"text"`
}

// ArenaDiff is one match's comparison.
type ArenaDiff struct {
	Match   string     `json:"match"`
	Seed    int64      `json:"seed"`
	At      time.Time  `json:"at"`
	OK      bool       `json:"ok"`
	Aborted bool       `json:"aborted"`
	Lines   []DiffLine `json:"lines"`
	Checked []string   `json:"checked"` // what was compared
}

// The arena's attribution window around a sensor count: the count reaches the arena after the PLC/alt-IO poll and the
// arena loop (late), and the sim's match clock may lead the arena's by a little (early).
const (
	diffEarly = 0.35
	diffLate  = 0.55
)

var shiftNames = []string{"AUTO", "TRANSITION", "SHIFT 1", "SHIFT 2", "SHIFT 3", "SHIFT 4", "END GAME", "post-match"}

type arenaScoreDoc struct {
	Red, Blue struct {
		Score struct {
			AutoTowerStatuses    [3]int
			EndgameTowerStatuses [3]int
			Hub                  struct {
				WonAuto     bool
				ShiftCounts []int
			}
			Fouls []json.RawMessage
		}
	}
}

type diffState struct {
	mu      sync.Mutex
	last    *ArenaDiff
	history []ArenaDiff
	done    chan struct{}
}

func (app *App) registerArenaDiff() {
	app.diff = &diffState{done: make(chan struct{})}
	app.Referee.OnFinished(func(refereeing, aborted bool) {
		app.runArenaDiff(refereeing, aborted)
	})
	app.Hub.Handle("arena.diffs", func(_ []byte) (any, error) {
		app.diff.mu.Lock()
		defer app.diff.mu.Unlock()
		return app.diff.history, nil
	})
}

// runArenaDiff compares this match's truth with the arena's realtime score and publishes the result.
func (app *App) runArenaDiff(refereeing, aborted bool) ArenaDiff {
	truth := app.World.Truth()
	label := "Match"
	if match := app.Arena.MatchLoad().Match; match != nil && match.ShortName != "" {
		label = match.ShortName
	}
	diff := ArenaDiff{Match: label, Seed: truth.Seed, At: time.Now(), Aborted: aborted}
	read := func() arenaScoreDoc {
		var doc arenaScoreDoc
		if raw := app.Hub.Get("arena.realtimeScore"); raw != nil {
			_ = json.Unmarshal(raw, &doc)
		}
		return doc
	}
	doc := read()
	// Fouls are entered one at a time; give the last ones a moment to land.
	settings := app.Referee.Settings()
	compareFouls := refereeing && settings.CallFouls
	if compareFouls {
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) && (len(doc.Red.Score.Fouls) < truth.Fouls[0] || len(doc.Blue.Score.Fouls) < truth.Fouls[1]) {
			time.Sleep(250 * time.Millisecond)
			doc = read()
		}
	}
	diff.Lines = compareArenaScore(truth, doc, refereeing, compareFouls, aborted)
	return app.publishDiff(diff, label, refereeing, compareFouls)
}

// compareArenaScore lists where the arena's score disagrees with the field's record.
func compareArenaScore(truth world.MatchTruth, doc arenaScoreDoc, refereeing, compareFouls, aborted bool) []DiffLine {
	var lines []DiffLine
	add := func(severity, area, alliance, format string, args ...any) {
		lines = append(lines, DiffLine{Severity: severity, Area: area, Alliance: alliance,
			Text: fmt.Sprintf(format, args...)})
	}
	for alliance, name := range []string{"red", "blue"} {
		score := doc.Red.Score
		if alliance == 1 {
			score = doc.Blue.Score
		}
		won := score.Hub.WonAuto
		// Hub: every count inside the scoring window, total and per shift.
		var counts []float64
		for _, t := range truth.Counts[alliance] {
			if t < world.TeleopEndSec+world.GraceSec+diffLate {
				counts = append(counts, t)
			}
		}
		arenaTotal := 0
		for _, n := range score.Hub.ShiftCounts {
			arenaTotal += n
		}
		lateCounts := 0
		for _, t := range counts {
			if t > world.TeleopEndSec+world.GraceSec-diffEarly {
				lateCounts++
			}
		}
		if delta := arenaTotal - len(counts); delta != 0 {
			severity := "error"
			if abs(delta) <= lateCounts {
				severity = "warn" // FUEL counted right at the end of the scoring window: either side of the line
			}
			add(severity, "hub", name, "the hub sensors counted %d FUEL; the arena has %d", len(counts), arenaTotal)
		}
		low := make([]int, len(shiftNames))
		high := make([]int, len(shiftNames))
		for _, t := range counts {
			early, okEarly := world.ScoringShift(t-diffEarly, won)
			late, okLate := world.ScoringShift(t+diffLate, won)
			switch {
			case okEarly && okLate && early == late:
				low[early]++
				high[early]++
			default:
				if okEarly {
					high[early]++
				}
				if okLate {
					high[late]++
				}
			}
		}
		for shift := range shiftNames {
			arena := 0
			if shift < len(score.Hub.ShiftCounts) {
				arena = score.Hub.ShiftCounts[shift]
			}
			if arena < low[shift] || arena > high[shift] {
				expect := fmt.Sprint(low[shift])
				if high[shift] != low[shift] {
					expect = fmt.Sprintf("%d–%d", low[shift], high[shift])
				}
				add("error", "shift", name, "%s: the arena credited %d FUEL, the field saw %s", shiftNames[shift], arena, expect)
			}
		}
		// Tower: what the referees saw at the end of AUTO and of the match.
		if refereeing && truth.AutoTaken {
			for i := 0; i < 3; i++ {
				station := alliance*3 + i
				want := truth.AutoLevels[station] > 0
				if got := score.AutoTowerStatuses[i] > 0; got != want {
					add("error", "tower", name, "%d (station %d): AUTO tower %v on the field, %v in the arena",
						truth.Teams[station], i+1, onOff(want), onOff(got))
				}
			}
		}
		if refereeing && truth.EndTaken && !aborted {
			for i := 0; i < 3; i++ {
				station := alliance*3 + i
				want := truth.EndLevels[station]
				if truth.AwardL3[station] {
					want = 3
				}
				if got := score.EndgameTowerStatuses[i]; got != want {
					add("error", "tower", name, "%d (station %d): END GAME level %d on the field, %d in the arena",
						truth.Teams[station], i+1, want, got)
				}
			}
		}
		// Fouls the line referees called (the arena may list more: chaos and manual entries).
		if compareFouls {
			arena := len(score.Fouls)
			switch {
			case arena < truth.Fouls[alliance]:
				add("error", "fouls", name, "the referees called %d fouls; the arena lists %d", truth.Fouls[alliance], arena)
			case arena > truth.Fouls[alliance]:
				add("info", "fouls", name, "the arena lists %d fouls, %d called by the line referees (the rest entered by hand or by chaos)",
					arena, truth.Fouls[alliance])
			}
		}
	}
	return lines
}

// publishDiff logs, stores and publishes a comparison.
func (app *App) publishDiff(diff ArenaDiff, label string, refereeing, compareFouls bool) ArenaDiff {
	diff.Checked = []string{"hub totals", "FUEL per shift"}
	if refereeing {
		diff.Checked = append(diff.Checked, "tower levels")
	}
	if compareFouls {
		diff.Checked = append(diff.Checked, "fouls")
	}
	diff.OK = true
	for _, line := range diff.Lines {
		if line.Severity == "error" {
			diff.OK = false
		}
	}
	if diff.OK {
		app.Hub.Logf("diff", "%s: the arena's score matches the field (%s)", label, strings.Join(diff.Checked, ", "))
	} else {
		for _, line := range diff.Lines {
			if line.Severity == "error" {
				app.Hub.Warnf("diff", "%s %s: %s", label, strings.ToUpper(line.Alliance), line.Text)
			}
		}
	}
	app.diff.mu.Lock()
	app.diff.last = &diff
	app.diff.history = append(app.diff.history, diff)
	if len(app.diff.history) > 40 {
		app.diff.history = app.diff.history[len(app.diff.history)-40:]
	}
	close(app.diff.done)
	app.diff.done = make(chan struct{})
	app.diff.mu.Unlock()
	app.Hub.Publish("arena.diff", diff)
	return diff
}

// waitArenaDiff waits for the comparison of the match with the given seed (the referees finish first).
func (app *App) waitArenaDiff(seed int64, timeout time.Duration) *ArenaDiff {
	deadline := time.After(timeout)
	for {
		app.diff.mu.Lock()
		last, done := app.diff.last, app.diff.done
		app.diff.mu.Unlock()
		if last != nil && last.Seed == seed {
			return last
		}
		select {
		case <-done:
		case <-deadline:
			return nil
		}
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
