// Package bench runs the AI benchmark (whole matches simulated inside the world, no arena) and writes a report that
// compares the run with a saved baseline, so changes to the AI can be measured.
package bench

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/world"
)

// BaselineFile is where a run can be saved as the reference for later runs.
const BaselineFile = "ai-baseline.json"

// Run plays the benchmark and writes run/reports/ai-bench-*.html and .json. With saveBaseline the run also becomes the
// new baseline.
func Run(layout *world.Layout, options world.BenchOptions, dir string, saveBaseline bool,
	progress func(world.BenchMatch)) (world.BenchReport, string, error) {
	report := world.RunBench(layout, options, progress)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return report, "", err
	}
	stamp := time.Now().Format("20060102-150405")
	data, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "ai-bench-"+stamp+".json"), data, 0o644); err != nil {
		return report, "", err
	}
	var baseline *world.BenchReport
	if raw, err := os.ReadFile(filepath.Join(dir, BaselineFile)); err == nil {
		var saved world.BenchReport
		if json.Unmarshal(raw, &saved) == nil {
			baseline = &saved
		}
	}
	if saveBaseline {
		if err := safe.WriteFile(filepath.Join(dir, BaselineFile), data, 0o644); err != nil {
			return report, "", err
		}
	}
	path := filepath.Join(dir, "ai-bench-"+stamp+".html")
	return report, path, os.WriteFile(path, []byte(renderHTML(report, baseline)), 0o644)
}

type metric struct {
	label  string
	value  func(world.BenchSummary) float64
	format string
	better int // +1 higher is better, -1 lower is better, 0 neutral
}

var metrics = []metric{
	{"Average alliance score", func(s world.BenchSummary) float64 { return s.AvgScore }, "%.1f", 1},
	{"FUEL scored per alliance", func(s world.BenchSummary) float64 { return s.AvgFuel }, "%.1f", 1},
	{"FUEL wasted (counted while inactive)", func(s world.BenchSummary) float64 { return s.AvgWastedFuel }, "%.1f", -1},
	{"Robot shot accuracy", func(s world.BenchSummary) float64 { return s.ShotAccuracy * 100 }, "%.0f%%", 1},
	{"Idle seconds per robot", func(s world.BenchSummary) float64 { return s.IdlePerRobot }, "%.1f", -1},
	{"Stuck seconds per robot", func(s world.BenchSummary) float64 { return s.StuckPerRobot }, "%.1f", -1},
	{"Fouls per match", func(s world.BenchSummary) float64 { return s.FoulsPerMatch }, "%.2f", 0},
	{"Robots on the tower at the end", func(s world.BenchSummary) float64 { return s.ClimbRate * 100 }, "%.0f%%", 1},
	{"Tower points per alliance", func(s world.BenchSummary) float64 { return s.AvgClimbPoints }, "%.1f", 1},
	{"Lead changes per match", func(s world.BenchSummary) float64 { return s.LeadSwaps }, "%.1f", 1},
	{"Early shots per match", func(s world.BenchSummary) float64 { return s.EarlyShots }, "%.1f", 0},
	{"Early shots that counted", func(s world.BenchSummary) float64 { return s.EarlyCountedRate * 100 }, "%.0f%%", 1},
	{"Late shots per match", func(s world.BenchSummary) float64 { return s.LateShots }, "%.1f", 0},
	{"Late shots that counted", func(s world.BenchSummary) float64 { return s.LateCountedRate * 100 }, "%.0f%%", 1},
	{"Bonus ranking points per alliance", func(s world.BenchSummary) float64 { return s.BonusRP }, "%.2f", 1},
	{"Climbs skipped to keep scoring (per match)", func(s world.BenchSummary) float64 { return s.ClimbSkips }, "%.1f", 0},
	{"Human player throws per match", func(s world.BenchSummary) float64 { return s.HumanShots }, "%.1f", 0},
	{"Human player accuracy", func(s world.BenchSummary) float64 { return s.HumanAccuracy * 100 }, "%.0f%%", 1},
	{"AUTO FUEL per alliance", func(s world.BenchSummary) float64 { return s.AutoFuel }, "%.1f", 1},
	{"AUTO routine steps finished", func(s world.BenchSummary) float64 { return s.AutoStepsDone * 100 }, "%.0f%%", 1},
	{"AUTO chooser mistakes per match", func(s world.BenchSummary) float64 { return s.AutoMistakes }, "%.2f", -1},
	{"AUTO steps with robot contact (per match)", func(s world.BenchSummary) float64 { return s.AutoContacts }, "%.1f", -1},
	{"Odometry error at the end of AUTO (cm)", func(s world.BenchSummary) float64 { return s.AutoOdoError * 100 }, "%.0f", -1},
}

// SummaryText is a plain-text table for the console.
func SummaryText(report world.BenchReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "AI benchmark: %d matches (seed %d, perfect info %v)\n", report.Summary.Matches,
		report.Options.Seed, report.Options.PerfectInfo)
	for _, m := range metrics {
		fmt.Fprintf(&b, "  %-40s "+m.format+"\n", m.label, m.value(report.Summary))
	}
	return b.String()
}

func renderHTML(report world.BenchReport, baseline *world.BenchReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Freezy Sim AI benchmark</title><style>
body{font:14px system-ui;background:#0e1116;color:#dfe5ee;margin:24px}table{border-collapse:collapse;margin-bottom:24px}
td,th{border-bottom:1px solid #2a313c;padding:5px 10px;text-align:left}th{color:#8b95a5;font-weight:600}
.good{color:#4cd38a}.bad{color:#ff6b6b}.muted{color:#8b95a5}.num{text-align:right;font-variant-numeric:tabular-nums}
</style></head><body><h1>AI benchmark</h1>`)
	fmt.Fprintf(&b, `<p class="muted">%d matches · seed %d · perfect info %v · chaos %v · %s</p>`, report.Summary.Matches,
		report.Options.Seed, report.Options.PerfectInfo, report.Options.Chaos, time.Now().Format("2006-01-02 15:04"))
	b.WriteString(`<table><tr><th>Metric</th><th class="num">This run</th>`)
	if baseline != nil {
		b.WriteString(`<th class="num">Baseline</th><th class="num">Change</th>`)
	}
	b.WriteString(`</tr>`)
	for _, m := range metrics {
		value := m.value(report.Summary)
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">`+m.format+`</td>`, m.label, value)
		if baseline != nil {
			old := m.value(baseline.Summary)
			delta := value - old
			class := "muted"
			if m.better != 0 && math.Abs(delta) > 1e-9 {
				if delta*float64(m.better) > 0 {
					class = "good"
				} else {
					class = "bad"
				}
			}
			fmt.Fprintf(&b, `<td class="num muted">`+m.format+`</td><td class="num %s">%+.1f</td>`, old, class, delta)
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</table><h2>Matches</h2><table><tr><th>#</th><th>Red plan</th><th class="num">Red</th>` +
		`<th class="num">Blue</th><th>Blue plan</th><th class="num">Lead changes</th><th>Fouls</th></tr>`)
	for _, match := range report.Matches {
		red, blue := match.Alliances[0], match.Alliances[1]
		fmt.Fprintf(&b, `<tr><td>%d</td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td>%s</td>`+
			`<td class="num">%d</td><td class="muted">%s</td></tr>`, match.Number, html.EscapeString(red.Plan), red.Score,
			blue.Score, html.EscapeString(blue.Plan), match.LeadSwaps, html.EscapeString(strings.Join(match.FoulCalls, "; ")))
	}
	b.WriteString(`</table><h2>Robots (last match)</h2><table><tr><th>Station</th><th>Team</th><th>Design</th>` +
		`<th>Strategy</th><th class="num">Shots</th><th class="num">Scored</th><th class="num">Idle s</th>` +
		`<th class="num">Stuck s</th><th class="num">Tower</th></tr>`)
	if n := len(report.Matches); n > 0 {
		for _, r := range report.Matches[n-1].Robots {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td><td class="num">%d</td><td class="num">%d</td>`+
				`<td class="num">%.1f</td><td class="num">%.1f</td><td class="num">%d</td></tr>`, r.Station, r.Team,
				r.Archetype, r.Strategy, r.Shots, r.Scored, r.IdleSeconds, r.StuckSeconds, r.ClimbLevel)
		}
	}
	b.WriteString(`</table><h2>AUTO routines (after the run)</h2><table><tr><th>Team</th><th>Routine</th>` +
		`<th class="num">Rev</th><th class="num">Runs</th><th class="num">Avg pts</th><th>Tuning</th></tr>`)
	for _, p := range report.Autos {
		fmt.Fprintf(&b, `<tr><td>%d</td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%.1f</td>`+
			`<td class="muted">%s</td></tr>`, p.Team, html.EscapeString(p.Name), p.Revision, p.Runs, p.AvgPoints,
			html.EscapeString(strings.Join(p.Notes, "; ")))
	}
	b.WriteString("</table></body></html>")
	return b.String()
}
