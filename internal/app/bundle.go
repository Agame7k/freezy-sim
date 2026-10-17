package app

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/recorder"
)

// A bug-report bundle is one zip with everything needed to understand (and replay) a problem the sim found: the
// black box, the match seed, every live topic, the event log with the arena's own log lines, the sandbox arena's
// database and CSV logs, the latest reports and the config. It is written to run/reports/bundle-*.zip.

const bundleDir = "run/reports"

// BundleManifest is manifest.json inside a bundle.
type BundleManifest struct {
	Created    time.Time         `json:"created"`
	Reason     string            `json:"reason"`
	Version    string            `json:"version"`
	Seed       int64             `json:"seed"`
	MatchState string            `json:"matchState"`
	MatchTime  float64           `json:"matchTime"`
	Teams      [6]int            `json:"teams"`
	Files      map[string]string `json:"files"` // name in the zip → where it came from
	Problems   []string          `json:"problems,omitempty"`
}

// WriteBundle writes a bug-report bundle and returns its path. blackBox is how much of the black box to include
// (0 = the last 3 minutes).
func (app *App) WriteBundle(reason string, blackBox time.Duration) (string, error) {
	if blackBox <= 0 {
		blackBox = 3 * time.Minute
	}
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return "", err
	}
	now := time.Now()
	path := filepath.Join(bundleDir, "bundle-"+now.Format("20060102-150405")+".zip")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	archive := zip.NewWriter(file)
	state, matchTime := app.Arena.MatchTime()
	manifest := BundleManifest{Created: now, Reason: reason, Version: Version, Seed: app.World.Seed(),
		MatchState: state.String(), MatchTime: matchTime, Teams: app.Arena.Teams(), Files: map[string]string{}}
	problem := func(format string, args ...any) {
		manifest.Problems = append(manifest.Problems, fmt.Sprintf(format, args...))
	}

	add := func(name, source string, write func(io.Writer) error) {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err == nil {
			err = write(entry)
		}
		if err != nil {
			problem("%s: %v", name, err)
			return
		}
		manifest.Files[name] = source
	}
	addJSON := func(name, source string, value any) {
		add(name, source, func(w io.Writer) error {
			data, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, scrubSecrets(string(data))+"\n")
			return err
		})
	}
	// addText copies a text file with any access tokens scrubbed (logs written before tokens were kept out of them).
	addText := func(name, source string) {
		add(name, source, func(w io.Writer) error {
			data, err := os.ReadFile(source)
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, scrubSecrets(string(data)))
			return err
		})
	}
	addFile := func(name, source string) {
		add(name, source, func(w io.Writer) error {
			// The arena keeps its database open; share-read it rather than failing on a lock.
			in, err := os.Open(source)
			if err != nil {
				return err
			}
			defer in.Close()
			_, err = io.Copy(w, in)
			return err
		})
	}

	addJSON("topics.json", "every live bus topic", app.Hub.Topics())
	add("events.log", "sim + arena event log", func(w io.Writer) error {
		for _, event := range app.Hub.Events() {
			fmt.Fprintf(w, "%s %-5s %-10s %s\n", event.Time.Format("15:04:05.000"), event.Level, event.Source,
				scrubSecrets(event.Msg))
		}
		return nil
	})
	addJSON("config.json", app.Config.Path()+" (passwords and tokens removed)", redactedConfig(app.Config))
	if recording, err := app.Recorder.BlackBox(blackBox); err != nil {
		problem("black box: %v", err)
	} else {
		add("blackbox.fsr", fmt.Sprintf("black box, last %.0f s", recording.Meta.Duration()), func(w io.Writer) error {
			return recorder.Write(w, recording)
		})
	}

	sandbox := app.Config.Arena.SandboxDir
	addFile("arena/event.db", filepath.Join(sandbox, "event.db"))
	for _, source := range newestFiles(filepath.Join(sandbox, "static", "logs"), "*.csv", 6) {
		addFile("arena/logs/"+filepath.Base(source), source)
	}
	for _, source := range newestFiles(filepath.Join("run", "logs"), "*", 2) {
		addText("sim-logs/"+filepath.Base(source), source)
	}
	for _, pattern := range []string{"*.html", "*.xml", "*.json", "*.csv"} {
		for _, source := range newestFiles(bundleDir, pattern, 3) {
			addFile("reports/"+filepath.Base(source), source)
		}
	}

	addJSON("manifest.json", "this bundle", manifest)
	if err := archive.Close(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	app.Hub.Logf("bundle", "Bug-report bundle written: %s (%s)", path, reason)
	app.Hub.Publish("bundle.last", map[string]any{"path": path, "reason": reason, "created": now})
	return path, nil
}

// newestFiles returns up to n regular files in dir matching pattern, newest first.
func newestFiles(dir, pattern string, n int) []string {
	paths, _ := filepath.Glob(filepath.Join(dir, pattern))
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || strings.HasPrefix(filepath.Base(path), "bundle-") {
			continue
		}
		items = append(items, item{path, info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	var out []string
	for i := 0; i < len(items) && i < n; i++ {
		out = append(out, items[i].path)
	}
	return out
}

func (app *App) registerBundle() {
	app.Hub.Handle("bundle.write", func(args []byte) (any, error) {
		var request struct {
			Reason  string  `json:"reason"`
			Minutes float64 `json:"minutes"`
		}
		_ = json.Unmarshal(args, &request)
		if request.Reason == "" {
			request.Reason = "saved from the GUI"
		}
		path, err := app.WriteBundle(request.Reason, time.Duration(request.Minutes*float64(time.Minute)))
		if err != nil {
			return nil, err
		}
		absolute, _ := filepath.Abs(path)
		return map[string]string{"path": absolute}, nil
	})
}

// autoBundle writes a bundle when the sim itself finds a problem (a failed acceptance run, a soak match that went
// wrong, an event-director anomaly). It is rate limited so a run that goes bad repeatedly does not fill the disk.
func (app *App) autoBundle(reason string) {
	app.bundleMu.Lock()
	if app.bundlesAuto >= 10 || time.Since(app.lastAutoBundle) < 2*time.Minute {
		app.bundleMu.Unlock()
		return
	}
	app.bundlesAuto++
	app.lastAutoBundle = time.Now()
	app.bundleMu.Unlock()
	if _, err := app.WriteBundle(reason, 0); err != nil {
		app.Hub.Errorf("bundle", "Could not write a bug-report bundle: %v", err)
	}
}

// secretPattern finds an access token in a URL or a log line ("?token=..." or "token=...").
var secretPattern = regexp.MustCompile(`(?i)(\btoken=)[^&\s"'<>]+`)

// scrubSecrets removes access tokens from text that goes into a bundle.
func scrubSecrets(text string) string {
	return secretPattern.ReplaceAllString(text, "${1}(removed)")
}

// redactedConfig returns the config as plain JSON values with every password, token and secret blanked, so a
// bundle can be shared.
func redactedConfig(cfg any) any {
	data, _ := json.Marshal(cfg)
	var value any
	_ = json.Unmarshal(data, &value)
	var walk func(any)
	walk = func(node any) {
		switch node := node.(type) {
		case map[string]any:
			for key, child := range node {
				lower := strings.ToLower(key)
				if strings.Contains(lower, "password") || strings.Contains(lower, "token") ||
					strings.Contains(lower, "secret") {
					if text, ok := child.(string); ok && text == "" {
						continue // an empty value says "not set", which helps a bug report
					}
					node[key] = "(removed)"
				} else {
					walk(child)
				}
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(value)
	return value
}
