package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/fieldio"
	"github.com/Agame7k/freezy-sim/internal/world"
)

// JSON scenarios: acceptance tests written as timelines instead of Go code. Each file in scenarios/ holds one
// scenario (or an array of them):
//
//	{
//	  "id": "astop-json", "name": "A-stop during auto", "description": "...",
//	  "setup": "readyField",                        // default; "none" skips it
//	  "steps": [
//	    {"do": "startMatch"},
//	    {"at": "auto+5", "do": "press", "control": "teamAStop", "index": 0},
//	    {"expect": "ds.R1.enabled == false", "within": 0.6},
//	    {"expect": "ds.R2.enabled", "hold": 1},
//	    {"at": "teleop+1", "expect": "ds.R1.enabled", "within": 1.5},
//	    {"do": "abort"}
//	  ]
//	}
//
// "at" waits for a match time: "auto+5" (5 s into auto), "teleop+1" (1 s into teleop), "match+30" (30 s after the
// start) or "+2" (2 s after the previous step). Actions ("do"): startMatch, readyField, abort, press/release (a field
// control), set (field.set with value), ds (ds.set station/field/value), cmd (any simulator command with args),
// manualAll, wait (seconds), log (message). An expectation is "path", "!path" or "path op value" (op: == != < <= > >=)
// checked now, polled up to "within" seconds, or required to stay true for "hold" seconds; "fatal": true stops the
// scenario when it fails. Paths:
//
//	ds.<station>.<field>          the robot-facing DS command (enabled, auto, estop, astop, gameData, remainingSeconds…)
//	arena.<field>                 the arena status (canStartMatch, isFtaReady, plcIsHealthy…), arena.state, arena.time
//	arena.station.<station>.<f>   the arena's view of a station (bypass, estop, astop, dsConn.robotLinked…)
//	coil.<name or index>          a field I/O coil (heartbeat, buzzer, redHubMotor, stackRed…)
//	score.<red|blue>.<field>      the realtime score summary (score, autoFuelPoints, teleopTowerPoints…)
//	world.<station>.<field>       the simulated robot (hopper, scored, climbLevel, aiState, autoName…)
//	topic:<bus topic>/<a>/<b>     anything on the simulator bus, by JSON path
//
// Field names match case-insensitively.

// FileScenario is a scenario as written in JSON.
type FileScenario struct {
	Id          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Setup       string     `json:"setup"`
	Steps       []FileStep `json:"steps"`
}

// FileStep is one timeline entry.
type FileStep struct {
	At      string          `json:"at,omitempty"`
	Do      string          `json:"do,omitempty"`
	Expect  string          `json:"expect,omitempty"`
	Within  float64         `json:"within,omitempty"`
	Hold    float64         `json:"hold,omitempty"`
	Fatal   bool            `json:"fatal,omitempty"`
	Control string          `json:"control,omitempty"`
	Index   int             `json:"index,omitempty"`
	Station string          `json:"station,omitempty"`
	Field   string          `json:"field,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Command string          `json:"command,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Seconds float64         `json:"seconds,omitempty"`
	Message string          `json:"message,omitempty"`
}

// ScenarioDir is where JSON scenarios are loaded from (relative to the simulator's folder).
const ScenarioDir = "scenarios"

// LoadScenarios reads every JSON scenario in dir. A file that does not parse becomes a scenario that fails with the
// parse error, so a typo shows up in the results instead of silently dropping the test.
func LoadScenarios(dir string) []Scenario {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(paths)
	var scenarios []Scenario
	for _, path := range paths {
		data, err := os.ReadFile(path)
		var files []FileScenario
		if err == nil {
			trimmed := strings.TrimSpace(string(data))
			if strings.HasPrefix(trimmed, "[") {
				err = json.Unmarshal(data, &files)
			} else {
				var one FileScenario
				err = json.Unmarshal(data, &one)
				files = []FileScenario{one}
			}
		}
		base := strings.TrimSuffix(filepath.Base(path), ".json")
		if err != nil {
			message := fmt.Sprintf("%s: %v", path, err)
			scenarios = append(scenarios, Scenario{Id: "file-" + base, Name: base + " (JSON)", Description: message,
				Run: func(t *T) { t.Fatalf("%s", message) }})
			continue
		}
		for k, file := range files {
			file := file
			if file.Id == "" {
				file.Id = base
				if len(files) > 1 {
					file.Id = fmt.Sprintf("%s-%d", base, k+1)
				}
			}
			if file.Name == "" {
				file.Name = file.Id
			}
			scenarios = append(scenarios, Scenario{Id: file.Id, Name: file.Name + " (JSON)",
				Description: file.Description, Run: func(t *T) { runFileScenario(t, file) }})
		}
	}
	return scenarios
}

func runFileScenario(t *T, file FileScenario) {
	switch strings.ToLower(file.Setup) {
	case "", "readyfield":
		t.ReadyField()
	case "none":
	default:
		t.Fatalf("unknown setup %q", file.Setup)
	}
	last := time.Now()
	for k, step := range file.Steps {
		if step.At != "" {
			t.waitAt(step.At, last)
		}
		if step.Do != "" {
			t.doStep(step)
		}
		if step.Expect != "" {
			t.expectStep(k, step)
		}
		last = time.Now()
	}
}

// waitAt waits for a step's time anchor.
func (t *T) waitAt(at string, previous time.Time) {
	at = strings.ReplaceAll(strings.ToLower(at), " ", "")
	if strings.HasPrefix(at, "+") {
		seconds, err := strconv.ParseFloat(at[1:], 64)
		if err != nil {
			t.Fatalf("bad time %q", at)
		}
		if remaining := time.Until(previous.Add(time.Duration(seconds * float64(time.Second)))); remaining > 0 {
			time.Sleep(remaining)
		}
		return
	}
	base, offset, found := strings.Cut(at, "+")
	if !found {
		base, offset = at, "0"
	}
	seconds, err := strconv.ParseFloat(offset, 64)
	if err != nil {
		t.Fatalf("bad time %q", at)
	}
	switch base {
	case "auto", "match":
	case "teleop":
		seconds += world.TeleopStartSec
	default:
		t.Fatalf("bad time anchor %q (auto, teleop, match or +N)", at)
	}
	t.WaitMatchTime(seconds, time.Duration(seconds+60)*time.Second)
}

func (t *T) doStep(step FileStep) {
	value := func() any {
		var v any = true
		if len(step.Value) > 0 {
			_ = json.Unmarshal(step.Value, &v)
		}
		return v
	}
	switch strings.ToLower(step.Do) {
	case "startmatch":
		t.StartMatch()
	case "readyfield":
		t.ReadyField()
	case "abort":
		t.Cmd("match.abort", nil)
	case "press", "release":
		t.Cmd("field.set", map[string]any{"control": step.Control, "index": step.Index,
			"value": strings.EqualFold(step.Do, "press")})
	case "set":
		t.Cmd("field.set", map[string]any{"control": step.Control, "index": step.Index, "value": value()})
	case "ds":
		t.Cmd("ds.set", map[string]any{"station": step.Station, "field": step.Field, "value": value()})
	case "cmd":
		var args any
		if len(step.Args) > 0 {
			_ = json.Unmarshal(step.Args, &args)
		}
		t.Cmd(step.Command, args)
	case "manualall":
		t.manualAll()
	case "wait":
		time.Sleep(time.Duration(step.Seconds * float64(time.Second)))
	case "log":
		t.Logf("%s", step.Message)
	default:
		t.Fatalf("unknown action %q", step.Do)
	}
}

func (t *T) expectStep(k int, step FileStep) {
	fail := func(format string, args ...any) {
		if step.Fatal {
			t.Fatalf(format, args...)
		}
		t.Errorf(format, args...)
	}
	check := func() (bool, string) {
		ok, actual, err := t.evaluate(step.Expect)
		if err != nil {
			t.Fatalf("step %d: %v", k+1, err)
		}
		return ok, actual
	}
	switch {
	case step.Hold > 0:
		deadline := time.Now().Add(time.Duration(step.Hold * float64(time.Second)))
		for time.Now().Before(deadline) {
			if ok, actual := check(); !ok {
				fail("%s did not hold for %.1fs (was %s)", step.Expect, step.Hold, actual)
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Logf("ok: %s held for %.1fs", step.Expect, step.Hold)
	case step.Within > 0:
		start := time.Now()
		if t.Poll(time.Duration(step.Within*float64(time.Second)), func() bool { ok, _ := check(); return ok }) {
			t.Logf("ok: %s (after %.2fs)", step.Expect, time.Since(start).Seconds())
			return
		}
		_, actual := check()
		fail("%s not true within %.1fs (is %s)", step.Expect, step.Within, actual)
	default:
		if ok, actual := check(); ok {
			t.Logf("ok: %s", step.Expect)
		} else {
			fail("%s (is %s)", step.Expect, actual)
		}
	}
}

// evaluate checks an expectation, returning the outcome and the value it saw.
func (t *T) evaluate(expression string) (bool, string, error) {
	fields := strings.Fields(expression)
	switch len(fields) {
	case 1:
		path, negate := fields[0], false
		if strings.HasPrefix(path, "!") {
			path, negate = path[1:], true
		}
		value, err := t.resolve(path)
		if err != nil {
			return false, "", err
		}
		return truthy(value) != negate, fmt.Sprint(value), nil
	case 3:
		value, err := t.resolve(fields[0])
		if err != nil {
			return false, "", err
		}
		ok, err := compare(value, fields[1], strings.Trim(fields[2], `"'`))
		return ok, fmt.Sprint(value), err
	}
	return false, "", fmt.Errorf("cannot read %q (use \"path\", \"!path\" or \"path op value\" with spaces)", expression)
}

var coilNames = []string{"heartbeat", "matchReset", "stackGreen", "stackOrange", "stackRed", "stackBlue", "buzzer",
	"fieldResetLight", "awardsModeLight", "redHubMotor", "blueHubMotor", "redHubLight", "blueHubLight", "redWins", "blueWins"}

// resolve reads a path's current value.
func (t *T) resolve(path string) (any, error) {
	if strings.HasPrefix(path, "topic:") {
		name, rest, _ := strings.Cut(strings.TrimPrefix(path, "topic:"), "/")
		raw := t.deps.Hub.Get(name)
		if raw == nil {
			return nil, nil
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, err
		}
		return walk(document, splitNonEmpty(rest, "/"))
	}
	parts := strings.Split(path, ".")
	switch strings.ToLower(parts[0]) {
	case "ds":
		if len(parts) < 3 {
			return nil, fmt.Errorf("%s: use ds.<station>.<field>", path)
		}
		return walk(asDocument(t.Station(strings.ToUpper(parts[1]))), parts[2:])
	case "arena":
		if len(parts) < 2 {
			return nil, fmt.Errorf("%s: use arena.<field>", path)
		}
		state, seconds := t.MatchState()
		switch strings.ToLower(parts[1]) {
		case "state":
			return state.String(), nil
		case "time":
			return seconds, nil
		case "station":
			if len(parts) < 4 {
				return nil, fmt.Errorf("%s: use arena.station.<station>.<field>", path)
			}
			return walk(asDocument(t.ArenaStation(strings.ToUpper(parts[2]))), parts[3:])
		}
		return walk(asDocument(t.Status()), parts[1:])
	case "coil":
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s: use coil.<name or index>", path)
		}
		coils := t.Coils()
		index, err := strconv.Atoi(parts[1])
		if err != nil {
			index = -1
			for i, name := range coilNames {
				if strings.EqualFold(name, parts[1]) || strings.EqualFold(fieldio.CoilNames[i], parts[1]) {
					index = i // (short names, or the arena's own names such as stackLightRed)
				}
			}
		}
		if index < 0 || index >= len(coils) {
			return nil, fmt.Errorf("%s: no such coil", path)
		}
		return coils[index], nil
	case "score":
		if len(parts) < 3 {
			return nil, fmt.Errorf("%s: use score.<red|blue>.<field>", path)
		}
		score := t.score()
		summary := score.Red.ScoreSummary
		if strings.EqualFold(parts[1], "blue") {
			summary = score.Blue.ScoreSummary
		}
		return walk(asDocument(summary), parts[2:])
	case "world":
		if len(parts) < 3 || t.deps.World == nil {
			return nil, fmt.Errorf("%s: use world.<station>.<field>", path)
		}
		for _, robot := range t.deps.World.RobotStats() {
			if strings.EqualFold(robot.Station, parts[1]) {
				return walk(asDocument(robot), parts[2:])
			}
		}
		return nil, fmt.Errorf("%s: no such station", path)
	}
	return nil, fmt.Errorf("%s: unknown path (ds, arena, coil, score, world or topic:)", path)
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// asDocument turns a struct into generic JSON values (so paths can walk it by field name).
func asDocument(value any) any {
	data, _ := json.Marshal(value)
	var document any
	_ = json.Unmarshal(data, &document)
	return document
}

// walk follows a path through generic JSON values, matching object keys case-insensitively.
func walk(document any, path []string) (any, error) {
	current := document
	for _, key := range path {
		switch node := current.(type) {
		case map[string]any:
			found := false
			for name, value := range node {
				if strings.EqualFold(name, key) {
					current, found = value, true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("no field %q (have %s)", key, strings.Join(keys(node), ", "))
			}
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(node) {
				return nil, fmt.Errorf("bad index %q", key)
			}
			current = node[index]
		default:
			return nil, fmt.Errorf("cannot look up %q in %v", key, current)
		}
	}
	return current, nil
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case int:
		return v != 0
	case string:
		return v != "" && v != "false" && v != "0"
	}
	return true
}

func compare(value any, op, literal string) (bool, error) {
	number := func(v any) (float64, bool) {
		switch n := v.(type) {
		case float64:
			return n, true
		case int:
			return float64(n), true
		case bool:
			if n {
				return 1, true
			}
			return 0, true
		case string:
			f, err := strconv.ParseFloat(n, 64)
			return f, err == nil
		}
		return 0, false
	}
	expected := any(literal)
	switch literal {
	case "true":
		expected = true
	case "false":
		expected = false
	}
	if b, ok := expected.(bool); ok {
		switch op {
		case "==":
			return truthy(value) == b, nil
		case "!=":
			return truthy(value) != b, nil
		}
		return false, fmt.Errorf("%s does not apply to true/false", op)
	}
	left, leftOK := number(value)
	right, rightOK := number(literal)
	if leftOK && rightOK {
		switch op {
		case "==":
			return left == right, nil
		case "!=":
			return left != right, nil
		case "<":
			return left < right, nil
		case "<=":
			return left <= right, nil
		case ">":
			return left > right, nil
		case ">=":
			return left >= right, nil
		}
		return false, fmt.Errorf("unknown operator %q", op)
	}
	text := fmt.Sprint(value)
	switch op {
	case "==":
		return strings.EqualFold(text, literal), nil
	case "!=":
		return !strings.EqualFold(text, literal), nil
	}
	return false, fmt.Errorf("cannot compare %q %s %q", text, op, literal)
}
