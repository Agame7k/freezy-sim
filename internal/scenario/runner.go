// Package scenario runs automated FMS acceptance tests: each scenario drives the real arena through the simulated
// field and asserts on what the arena and the emulated hardware actually did. Results are published live and written
// as HTML and JUnit XML reports (for headless/CI runs).
package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/ds"
	"github.com/Agame7k/freezy-sim/internal/fieldio"
	"github.com/Agame7k/freezy-sim/internal/world"
)

// Deps are the simulator subsystems scenarios can observe and drive.
type Deps struct {
	Hub     *bus.Hub
	Arena   *arena.Client
	DS      *ds.Manager
	FieldIO *fieldio.Manager
	World   *world.World
	// NetworkEnabled reports whether the managed field network is being simulated.
	NetworkEnabled func() bool
}

// Scenario is one acceptance test.
type Scenario struct {
	Id          string
	Name        string
	Description string
	Run         func(t *T)
}

// Result is the outcome of one scenario.
type Result struct {
	Id        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // pending, running, pass, fail, skip
	Duration  float64   `json:"duration"`
	Failures  []string  `json:"failures"`
	Log       []string  `json:"log"`
	StartedAt time.Time `json:"startedAt"`
}

type failNow struct{}
type skipNow struct{ reason string }

// T is the scenario context.
type T struct {
	deps   Deps
	result *Result
	runner *Runner
}

func (t *T) Logf(format string, args ...any) {
	line := fmt.Sprintf("%6.1fs  ", time.Since(t.result.StartedAt).Seconds()) + fmt.Sprintf(format, args...)
	t.runner.mu.Lock()
	t.result.Log = append(t.result.Log, line)
	t.runner.mu.Unlock()
	t.runner.publish()
}

// Errorf records a failure and continues.
func (t *T) Errorf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	t.runner.mu.Lock()
	t.result.Failures = append(t.result.Failures, message)
	t.runner.mu.Unlock()
	t.Logf("FAIL: %s", message)
}

// Fatalf records a failure and stops the scenario.
func (t *T) Fatalf(format string, args ...any) {
	t.Errorf(format, args...)
	panic(failNow{})
}

// Skip stops the scenario without failing it.
func (t *T) Skip(reason string) {
	panic(skipNow{reason})
}

// Check records a failure if the condition is false.
func (t *T) Check(ok bool, format string, args ...any) bool {
	if !ok {
		t.Errorf(format, args...)
	} else {
		t.Logf("ok: "+format, args...)
	}
	return ok
}

// Cmd runs a simulator command (anything the GUI can do).
func (t *T) Cmd(command string, args any) {
	data, _ := json.Marshal(args)
	if _, err := t.deps.Hub.Dispatch(command, data); err != nil {
		t.Fatalf("command %s failed: %v", command, err)
	}
}

// WaitFor polls until cond is true or the timeout expires (fatal on timeout).
func (t *T) WaitFor(description string, timeout time.Duration, cond func() bool) time.Duration {
	start := time.Now()
	for time.Since(start) < timeout {
		if cond() {
			elapsed := time.Since(start)
			t.Logf("%s (after %.2fs)", description, elapsed.Seconds())
			return elapsed
		}
		if t.runner.cancelled() {
			t.Fatalf("cancelled")
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("timed out after %.1fs waiting for: %s", timeout.Seconds(), description)
	return timeout
}

// Poll polls until cond is true or the timeout expires, without failing; it reports whether cond became true.
func (t *T) Poll(timeout time.Duration, cond func() bool) bool {
	start := time.Now()
	for time.Since(start) < timeout {
		if cond() {
			return true
		}
		if t.runner.cancelled() {
			t.Fatalf("cancelled")
		}
		time.Sleep(40 * time.Millisecond)
	}
	return cond()
}

// Status is the arena's current status.
func (t *T) Status() arena.ArenaStatus { return t.deps.Arena.Status() }

// MatchState returns the arena match state and time.
func (t *T) MatchState() (arena.MatchState, float64) { return t.deps.Arena.MatchTime() }

// Station returns a driver station's robot-facing command view.
func (t *T) Station(name string) ds.Command { return t.deps.DS.Station(name).Command() }

// ArenaStation returns the arena's view of a station.
func (t *T) ArenaStation(name string) *arena.StationStatus {
	status := t.Status()
	if status.AllianceStations == nil {
		return &arena.StationStatus{}
	}
	if station := status.AllianceStations[name]; station != nil {
		return station
	}
	return &arena.StationStatus{}
}

// Coils returns the effective field outputs.
func (t *T) Coils() [fieldio.CoilCount]bool { return t.deps.FieldIO.Field.Coils() }

// Topic decodes the latest value of a bus topic.
func (t *T) Topic(name string, target any) bool {
	raw := t.deps.Hub.Get(name)
	if raw == nil {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

// ---- Common setup ----------------------------------------------------------------------------------------------

// ReadyField puts the field back to a clean, startable test match with every robot under AI control.
func (t *T) ReadyField() {
	state, _ := t.MatchState()
	if state != arena.PreMatch {
		if state.InMatch() {
			t.Cmd("match.abort", nil)
			t.WaitFor("match aborted", 5*time.Second, func() bool { s, _ := t.MatchState(); return s == arena.PostMatch })
		}
		t.Cmd("match.discard", nil)
		t.WaitFor("arena back in pre-match", 10*time.Second, func() bool { s, _ := t.MatchState(); return s == arena.PreMatch })
	}
	// Clear any physical stops, faults and overrides left by earlier scenarios.
	for i := 0; i < 6; i++ {
		t.Cmd("field.set", map[string]any{"control": "teamEStop", "index": i, "value": false})
		t.Cmd("field.set", map[string]any{"control": "teamAStop", "index": i, "value": false})
	}
	t.Cmd("field.set", map[string]any{"control": "fieldEStop", "index": 0, "value": false})
	for _, station := range arena.Stations {
		t.Cmd("ds.set", map[string]any{"station": station, "field": "team", "value": 0})
		t.Cmd("ds.set", map[string]any{"station": station, "field": "dsOn", "value": true})
		t.Cmd("ds.set", map[string]any{"station": station, "field": "lossPct", "value": 0})
		t.Cmd("world.robot", map[string]any{"station": station, "manual": false})
	}
	t.Cmd("chaos.config", map[string]any{"level": 0})
	t.Cmd("match.quickTest", nil)
	// The reload makes the arena reconfigure the network (SCC ports cycle, AP reprograms), so wait for readiness to
	// hold steadily rather than trusting the previous status.
	time.Sleep(1500 * time.Millisecond)
	t.WaitReady(60 * time.Second)
}

// WaitReady waits until the arena allows a match start continuously for 2 s; on timeout it reports the blockers.
func (t *T) WaitReady(timeout time.Duration) {
	start := time.Now()
	var readySince time.Time
	for time.Since(start) < timeout {
		if t.Status().CanStartMatch {
			if readySince.IsZero() {
				readySince = time.Now()
			}
			if time.Since(readySince) >= 2*time.Second {
				t.Logf("field ready to start (after %.1fs)", time.Since(start).Seconds())
				return
			}
		} else {
			readySince = time.Time{}
		}
		if t.runner.cancelled() {
			t.Fatalf("cancelled")
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("field never became ready; arena blockers: %s", strings.Join(t.Status().StartMatchConditions, "; "))
}

// StartMatch starts the match and waits for auto and for the arena's match reset pulse to reach the hub counters, so
// FUEL a scenario injects afterwards can never be wiped by a late reset.
func (t *T) StartMatch() {
	resets := t.deps.FieldIO.Field.ResetCount()
	t.Cmd("match.start", nil)
	t.WaitFor("match in auto", 5*time.Second, func() bool { s, _ := t.MatchState(); return s == arena.AutoPeriod })
	if t.Poll(3*time.Second, func() bool { return t.deps.FieldIO.Field.ResetCount() > resets }) {
		t.Logf("hub counters reset by the arena")
	} else {
		t.Logf("NOTE: no match reset pulse seen within 3 s of the start")
	}
}

// WaitMatchTime waits until the arena match clock reaches the given second.
func (t *T) WaitMatchTime(seconds float64, timeout time.Duration) {
	t.WaitFor(fmt.Sprintf("match time %.0fs", seconds), timeout, func() bool {
		state, now := t.MatchState()
		return state.InMatch() && now >= seconds || state == arena.PostMatch
	})
}

// AllStations returns true if pred holds for every station's DS command.
func (t *T) AllStations(pred func(name string, command ds.Command) bool) bool {
	for _, name := range arena.Stations {
		if !pred(name, t.Station(name)) {
			return false
		}
	}
	return true
}

// ---- Runner ----------------------------------------------------------------------------------------------------

// Runner executes scenarios one at a time.
type Runner struct {
	deps      Deps
	scenarios []Scenario

	mu       sync.Mutex
	running  bool
	cancel   bool
	results  []*Result
	started  time.Time
	finished time.Time
	onDone   []func([]*Result)
}

func NewRunner(deps Deps) *Runner {
	runner := &Runner{deps: deps, scenarios: append(Suite(), LoadScenarios(ScenarioDir)...)}
	runner.reset(nil)
	runner.registerCommands()
	return runner
}

func (runner *Runner) reset(ids []string) {
	runner.results = nil
	for _, s := range runner.scenarios {
		status := "pending"
		if len(ids) > 0 && !contains(ids, s.Id) {
			status = "not selected"
		}
		runner.results = append(runner.results, &Result{Id: s.Id, Name: s.Name, Status: status})
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (runner *Runner) cancelled() bool {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.cancel
}

// OnDone registers a callback for when a run finishes.
func (runner *Runner) OnDone(fn func([]*Result)) {
	runner.mu.Lock()
	runner.onDone = append(runner.onDone, fn)
	runner.mu.Unlock()
}

// Start runs the selected scenarios (all if ids is empty) in the background.
func (runner *Runner) Start(ids []string) error {
	runner.mu.Lock()
	if runner.running {
		runner.mu.Unlock()
		return fmt.Errorf("a test run is already in progress")
	}
	runner.running = true
	runner.cancel = false
	runner.started = time.Now()
	runner.reset(ids)
	runner.mu.Unlock()
	runner.publish()
	go func() {
		runner.deps.Hub.Logf("tests", "Acceptance run started")
		for i, scenario := range runner.scenarios {
			result := runner.results[i]
			if result.Status == "not selected" {
				continue
			}
			if runner.cancelled() {
				result.Status = "skip"
				continue
			}
			runner.runOne(scenario, result)
		}
		runner.mu.Lock()
		runner.running = false
		runner.finished = time.Now()
		results := runner.results
		callbacks := append([]func([]*Result){}, runner.onDone...)
		runner.mu.Unlock()
		passed, failed := 0, 0
		for _, r := range results {
			if r.Status == "pass" {
				passed++
			} else if r.Status == "fail" {
				failed++
			}
		}
		runner.deps.Hub.Logf("tests", "Acceptance run finished: %d passed, %d failed", passed, failed)
		runner.publish()
		for _, callback := range callbacks {
			callback(results)
		}
	}()
	return nil
}

func (runner *Runner) runOne(scenario Scenario, result *Result) {
	runner.mu.Lock()
	result.Status = "running"
	result.StartedAt = time.Now()
	result.Failures = nil
	result.Log = nil
	runner.mu.Unlock()
	runner.publish()
	t := &T{deps: runner.deps, result: result, runner: runner}
	status := "pass"
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				switch value := recovered.(type) {
				case failNow:
				case skipNow:
					status = "skip"
					t.Logf("skipped: %s", value.reason)
				default:
					t.Errorf("panic: %v", value)
				}
			}
		}()
		scenario.Run(t)
	}()
	runner.mu.Lock()
	if status != "skip" && len(result.Failures) > 0 {
		status = "fail"
	}
	result.Status = status
	result.Duration = time.Since(result.StartedAt).Seconds()
	runner.mu.Unlock()
	runner.deps.Hub.Logf("tests", "%s: %s (%.0fs)", strings.ToUpper(status), scenario.Name, result.Duration)
	runner.publish()
}

func (runner *Runner) publish() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	descriptions := map[string]string{}
	for _, s := range runner.scenarios {
		descriptions[s.Id] = s.Description
	}
	runner.deps.Hub.Publish("tests.state", map[string]any{
		"running": runner.running, "started": runner.started, "finished": runner.finished, "results": runner.results,
		"descriptions": descriptions,
	})
}

func (runner *Runner) registerCommands() {
	runner.deps.Hub.Handle("tests.run", func(args []byte) (any, error) {
		var request struct {
			Ids []string `json:"ids"`
		}
		if len(args) > 0 {
			_ = json.Unmarshal(args, &request)
		}
		return nil, runner.Start(request.Ids)
	})
	runner.deps.Hub.Handle("tests.reload", func(_ []byte) (any, error) {
		// Re-read the JSON scenarios (after editing a file) without restarting the simulator.
		runner.mu.Lock()
		if runner.running {
			runner.mu.Unlock()
			return nil, fmt.Errorf("a test run is in progress")
		}
		runner.scenarios = append(Suite(), LoadScenarios(ScenarioDir)...)
		runner.reset(nil)
		count := len(runner.scenarios)
		runner.mu.Unlock()
		runner.publish()
		return map[string]int{"scenarios": count}, nil
	})
	runner.deps.Hub.Handle("tests.cancel", func(_ []byte) (any, error) {
		runner.mu.Lock()
		runner.cancel = true
		runner.mu.Unlock()
		return nil, nil
	})
}
