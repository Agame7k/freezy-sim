package world

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestDiagnoseStuck plays benchmark matches and reports where and in which AI state robots were stuck or idle. It is
// a tuning aid, run with FREEZY_DIAG=1.
func TestDiagnoseStuck(t *testing.T) {
	if os.Getenv("FREEZY_DIAG") == "" {
		t.Skip("set FREEZY_DIAG=1 to run")
	}
	layout, err := LoadLayout(os.DirFS("../../web"))
	if err != nil {
		t.Fatal(err)
	}
	episodes := map[string]int{}
	diagHook = func(world *World, robot *Robot, kind string) {
		near := ""
		for _, other := range world.robots {
			if other != robot && other.Present && other.Pos.Sub(robot.Pos).Len() < 1.1 {
				near += other.Station + "(" + other.AiState + ") "
			}
		}
		key := fmt.Sprintf("%s state=%-14s at (%4.1f,%4.1f) near=%s", kind, robot.AiState, robot.Pos.X, robot.Pos.Y, near)
		episodes[key]++
	}
	defer func() { diagHook = nil }()
	report := RunBench(layout, BenchOptions{Matches: 4, Seed: 3}, nil)
	keys := make([]string, 0, len(episodes))
	for key := range episodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Logf("%3d × %s", episodes[key], key)
	}
	t.Logf("idle %.2f s/robot, stuck %.2f s/robot", report.Summary.IdlePerRobot, report.Summary.StuckPerRobot)
}

// TestDiagnoseStates reports how long robots spend in each AI state (FREEZY_DIAG=1).
func TestDiagnoseStates(t *testing.T) {
	if os.Getenv("FREEZY_DIAG") == "" {
		t.Skip("set FREEZY_DIAG=1 to run")
	}
	layout, err := LoadLayout(os.DirFS("../../web"))
	if err != nil {
		t.Fatal(err)
	}
	times := map[string]float64{}
	diagState = func(robot *Robot, dt float64) {
		state := robot.AiState
		if i := strings.IndexAny(state, "+"); i > 0 {
			state = state[:i]
		}
		times[state] += dt
	}
	defer func() { diagState = nil }()
	report := RunBench(layout, BenchOptions{Matches: 4, Seed: 1}, nil)
	keys := make([]string, 0, len(times))
	for key := range times {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return times[keys[i]] > times[keys[j]] })
	for _, key := range keys {
		t.Logf("%-18s %6.1f s per robot-match", key, times[key]/24)
	}
	t.Logf("score %.0f, fuel %.0f", report.Summary.AvgScore, report.Summary.AvgFuel)
}
