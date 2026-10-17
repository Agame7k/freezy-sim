package scenario

import (
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/version"
)

// WriteReports saves an HTML report and a JUnit XML file for a finished run; it returns their paths.
func WriteReports(dir string, results []*Result) (string, string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	stamp := time.Now().Format("20060102-150405")
	htmlPath := filepath.Join(dir, "acceptance-"+stamp+".html")
	xmlPath := filepath.Join(dir, "acceptance-"+stamp+".xml")

	passed, failed, skipped := 0, 0, 0
	total := 0.0
	for _, r := range results {
		total += r.Duration
		switch r.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		default:
			skipped++
		}
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Freezy Sim acceptance report</title><style>
body{font:14px system-ui,sans-serif;background:#0b0e13;color:#e7ecf2;margin:24px}h1{font-size:20px}
.s{display:inline-block;padding:1px 8px;border-radius:10px;font-weight:600}.pass{background:#173b2a;color:#9ff0c6}
.fail{background:#4a1f22;color:#ffb1b3}.skip,.notselected{background:#262e3a;color:#8a96a8}
details{background:#151a22;border:1px solid #262e3a;border-radius:8px;margin:8px 0;padding:8px 12px}
summary{cursor:pointer}pre{white-space:pre-wrap;color:#c4ccd8;font:12px Consolas,monospace}.f{color:#ff8e8e}</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Freezy Sim — FMS acceptance report</h1><p>%s · %d passed · %d failed · %d skipped · %.0f s · %s</p>",
		time.Now().Format("2006-01-02 15:04:05"), passed, failed, skipped, total, html.EscapeString(version.Describe()))
	for _, r := range results {
		class := strings.ReplaceAll(r.Status, " ", "")
		fmt.Fprintf(&b, `<details%s><summary><span class="s %s">%s</span> %s <small>(%.1f s)</small></summary>`,
			map[bool]string{true: " open", false: ""}[r.Status == "fail"], class, html.EscapeString(strings.ToUpper(r.Status)),
			html.EscapeString(r.Name), r.Duration)
		for _, f := range r.Failures {
			fmt.Fprintf(&b, `<div class="f">✗ %s</div>`, html.EscapeString(f))
		}
		fmt.Fprintf(&b, "<pre>%s</pre></details>", html.EscapeString(strings.Join(r.Log, "\n")))
	}
	b.WriteString("</body></html>")
	if err := os.WriteFile(htmlPath, []byte(b.String()), 0644); err != nil {
		return "", "", err
	}

	type failure struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	}
	type testcase struct {
		Name      string    `xml:"name,attr"`
		Classname string    `xml:"classname,attr"`
		Time      float64   `xml:"time,attr"`
		Failure   *failure  `xml:"failure,omitempty"`
		Skipped   *struct{} `xml:"skipped,omitempty"`
		SystemOut string    `xml:"system-out,omitempty"`
	}
	type suite struct {
		XMLName  xml.Name   `xml:"testsuite"`
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Skipped  int        `xml:"skipped,attr"`
		Time     float64    `xml:"time,attr"`
		Cases    []testcase `xml:"testcase"`
	}
	out := suite{Name: "freezy-sim.acceptance", Tests: len(results), Failures: failed, Skipped: skipped, Time: total}
	for _, r := range results {
		c := testcase{Name: r.Name, Classname: "freezy-sim." + r.Id, Time: r.Duration, SystemOut: strings.Join(r.Log, "\n")}
		if r.Status == "fail" {
			c.Failure = &failure{Message: strings.Join(r.Failures, "; "), Text: strings.Join(r.Failures, "\n")}
		} else if r.Status != "pass" {
			c.Skipped = &struct{}{}
		}
		out.Cases = append(out.Cases, c)
	}
	data, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(xmlPath, append([]byte(xml.Header), data...), 0644); err != nil {
		return "", "", err
	}
	return htmlPath, xmlPath, nil
}
