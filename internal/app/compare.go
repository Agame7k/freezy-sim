package app

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/scenario"
)

// Comparing arena versions: the acceptance suite runs against each arena ref in turn (each built from a `git
// archive` export, launcher/ref.go), and the report lines the results up side by side. A scenario that passes on the
// first ref and fails on a later one is a regression, and the run exits non-zero.

// CompareRun is one arena version's results.
type CompareRun struct {
	Ref     string             `json:"ref"`
	Commit  string             `json:"commit"`
	Problem string             `json:"problem,omitempty"` // the arena could not be built or never came ready
	Results []*scenario.Result `json:"results"`
}

// RunCompare runs the acceptance suite (or the given scenario ids) against each arena ref and writes
// run/reports/compare-*.html and .json. It returns false if anything regressed from the first ref.
func (app *App) RunCompare(refs, tests []string) bool {
	done := make(chan []*scenario.Result, 1)
	app.Tests.OnDone(func(results []*scenario.Result) {
		select {
		case done <- results:
		default:
		}
	})
	connected := make(chan struct{}, 1)
	app.Arena.OnConnect(func() {
		select {
		case connected <- struct{}{}:
		default:
		}
	})
	var runs []CompareRun
	for _, ref := range refs {
		run := CompareRun{Ref: ref}
		app.Hub.Logf("compare", "Arena %s: building and starting", ref)
		_ = app.Launcher.Stop()
		app.Launcher.SetRef(ref)
		select {
		case <-connected: // drop a stale connect from the previous arena
		default:
		}
		app.Launcher.Start()
		status := app.Launcher.Status()
		run.Commit = status.Commit
		reconnected := func() bool {
			select {
			case <-connected:
				return true
			case <-time.After(time.Minute):
				return false
			}
		}
		switch {
		case status.State == "error":
			run.Problem = status.Detail
		case !app.waitLauncherReady(3 * time.Minute):
			run.Problem = "the arena never came up: " + app.Launcher.Status().Detail
		case !reconnected():
			run.Problem = "the simulator never reconnected to the arena"
		case !app.WaitArenaReady():
			run.Problem = "the field never became ready: " + strings.Join(app.Arena.Status().StartMatchConditions, "; ")
		}
		if run.Problem == "" {
			if err := app.Tests.Start(tests); err != nil {
				run.Problem = err.Error()
			} else {
				for _, result := range <-done {
					if result.Status != "not selected" {
						run.Results = append(run.Results, result)
					}
				}
			}
		}
		if run.Problem != "" {
			app.Hub.Errorf("compare", "Arena %s: %s", ref, run.Problem)
		}
		runs = append(runs, run)
	}
	regressions := compareRegressions(runs)
	path, err := writeCompareReport(filepath.Join("run", "reports"), runs, regressions)
	if err != nil {
		app.Hub.Errorf("compare", "Could not write the comparison report: %v", err)
	} else {
		app.Hub.Logf("compare", "Comparison report written to %s", path)
	}
	for _, line := range regressions {
		app.Hub.Errorf("compare", "REGRESSION: %s", line)
	}
	return len(regressions) == 0 && runs[0].Problem == ""
}

// waitLauncherReady waits for the sandbox arena to be serving (the build can take a while for a new export).
func (app *App) waitLauncherReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := app.Launcher.Status()
		if status.Ready {
			return true
		}
		if status.State == "error" || status.State == "exited" {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// compareRegressions lists every scenario that passed against the first ref but not against a later one, and every
// later ref that would not even run.
func compareRegressions(runs []CompareRun) []string {
	if len(runs) < 2 {
		return nil
	}
	base := map[string]string{}
	for _, result := range runs[0].Results {
		base[result.Id] = result.Status
	}
	var out []string
	for _, run := range runs[1:] {
		if run.Problem != "" && runs[0].Problem == "" {
			out = append(out, fmt.Sprintf("%s: %s", run.Ref, run.Problem))
			continue
		}
		for _, result := range run.Results {
			if base[result.Id] == "pass" && result.Status == "fail" {
				first := ""
				if len(result.Failures) > 0 {
					first = ": " + result.Failures[0]
				}
				out = append(out, fmt.Sprintf("%s fails on %s (passes on %s)%s", result.Id, run.Ref, runs[0].Ref, first))
			}
		}
	}
	return out
}

func writeCompareReport(dir string, runs []CompareRun, regressions []string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	data, _ := json.MarshalIndent(map[string]any{"runs": runs, "regressions": regressions}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "compare-"+stamp+".json"), data, 0o644); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Arena comparison</title><style>
body{font:14px system-ui;background:#0e1116;color:#dfe5ee;margin:24px}table{border-collapse:collapse}
td,th{border-bottom:1px solid #2a313c;padding:6px 10px;text-align:left;vertical-align:top}th{color:#8b95a5}
.pass{color:#4cd38a}.fail{color:#ff6b6b}.skip{color:#8b95a5}.small{font-size:12px;color:#8b95a5}.reg{background:#3a1b1f}
</style></head><body><h1>Arena comparison</h1>`)
	fmt.Fprintf(&b, "<p class=\"small\">%s</p>", time.Now().Format("2006-01-02 15:04"))
	if len(regressions) > 0 {
		b.WriteString(`<h2 class="fail">Regressions</h2><ul>`)
		for _, line := range regressions {
			fmt.Fprintf(&b, `<li class="fail">%s</li>`, html.EscapeString(line))
		}
		b.WriteString("</ul>")
	} else if len(runs) > 1 {
		b.WriteString(`<p class="pass">No regressions.</p>`)
	}
	b.WriteString("<table><tr><th>Scenario</th>")
	for _, run := range runs {
		commit := run.Commit
		if len(commit) > 10 {
			commit = commit[:10]
		}
		fmt.Fprintf(&b, `<th>%s<br><span class="small">%s</span></th>`, html.EscapeString(run.Ref), commit)
	}
	b.WriteString("</tr>")
	var ids []string
	names := map[string]string{}
	byRun := make([]map[string]*scenario.Result, len(runs))
	for i, run := range runs {
		byRun[i] = map[string]*scenario.Result{}
		for _, result := range run.Results {
			if _, seen := names[result.Id]; !seen {
				ids = append(ids, result.Id)
				names[result.Id] = result.Name
			}
			byRun[i][result.Id] = result
		}
	}
	for _, id := range ids {
		regressed := false
		if base := byRun[0][id]; base != nil && base.Status == "pass" {
			for _, results := range byRun[1:] {
				if r := results[id]; r != nil && r.Status == "fail" {
					regressed = true
				}
			}
		}
		class := ""
		if regressed {
			class = ` class="reg"`
		}
		fmt.Fprintf(&b, `<tr%s><td>%s<br><span class="small">%s</span></td>`, class, html.EscapeString(names[id]), id)
		for i := range runs {
			result := byRun[i][id]
			if result == nil {
				b.WriteString(`<td class="skip">—</td>`)
				continue
			}
			detail := ""
			if len(result.Failures) > 0 {
				detail = `<br><span class="small">` + html.EscapeString(result.Failures[0]) + "</span>"
			}
			fmt.Fprintf(&b, `<td class="%s">%s (%.0f s)%s</td>`, result.Status, result.Status, result.Duration, detail)
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
	for _, run := range runs {
		if run.Problem != "" {
			fmt.Fprintf(&b, `<p class="fail">%s: %s</p>`, html.EscapeString(run.Ref), html.EscapeString(run.Problem))
		}
	}
	b.WriteString("</body></html>")
	path := filepath.Join(dir, "compare-"+stamp+".html")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}
