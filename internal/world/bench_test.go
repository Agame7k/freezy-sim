package world

import (
	"encoding/json"
	"testing"
)

// The same seed plays the same event: every match, every robot, every routine.
func TestBenchIsDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole matches")
	}
	layout := testLayout(t)
	run := func() string {
		report := RunBench(layout, BenchOptions{Matches: 3, Seed: 42}, nil)
		data, _ := json.Marshal(report.Matches)
		return string(data)
	}
	first, second := run(), run()
	if first != second {
		// Find the first difference to make the failure useful.
		k := 0
		for k < len(first) && k < len(second) && first[k] == second[k] {
			k++
		}
		from := max(0, k-160)
		t.Fatalf("two runs with seed 42 differ at byte %d:\n%s\n---\n%s", k, first[from:min(len(first), k+80)],
			second[from:min(len(second), k+80)])
	}
}

// Several workers play the requested number of matches between them, numbered in order.
func TestBenchWorkers(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole matches")
	}
	report := RunBench(testLayout(t), BenchOptions{Matches: 5, Seed: 3, Workers: 3}, nil)
	if len(report.Matches) != 5 {
		t.Fatalf("%d matches, want 5", len(report.Matches))
	}
	for i, match := range report.Matches {
		if match.Number != i+1 {
			t.Fatalf("match %d numbered %d", i+1, match.Number)
		}
	}
	if report.Learning == nil || len(report.Learning.Teams) == 0 {
		t.Fatal("no learning came back")
	}
}

// Training starts from a saved state and the learning comes back for the live teams.
func TestBenchStartsFromState(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole matches")
	}
	state := SavedState{TeamArchetypes: map[int]string{254: "kitbot"}, Learning: &Learning{
		Teams: map[int]*TeamStats{254: {Matches: 40, AutoMistakes: 3}}}}
	report := RunBench(testLayout(t), BenchOptions{Matches: 2, Seed: 5, State: &state}, nil)
	if stats := report.Learning.Teams[254]; stats == nil || stats.Matches < 41 {
		t.Fatalf("254's record did not carry over: %+v", stats)
	}
	for _, robot := range report.Matches[0].Robots {
		if robot.Team == 254 && robot.Archetype != "kitbot" {
			t.Fatalf("254 played as %s, not its saved design", robot.Archetype)
		}
	}
}
