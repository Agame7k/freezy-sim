package world

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/safe"
)

func TestSavedStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	world, _, _ := newTestWorld(t)
	if err := world.LoadState(path); err != nil {
		t.Fatal(err)
	}
	strategy := StratDefense
	world.mu.Lock()
	world.teamArch[100] = archetypeByName("turret")
	world.teamSkill[100] = 1.25
	world.overrides[0].Strategy = strategy
	world.outposts[Red].HumanMode = "score"
	world.markDirtyLocked()
	world.mu.Unlock()
	time.Sleep(1500 * time.Millisecond) // debounced save

	restored, _, _ := newTestWorld(t)
	if err := restored.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if restored.teamArch[100].Name != "turret" || restored.teamSkill[100] != 1.25 {
		t.Errorf("team design not restored: %s %.2f", restored.teamArch[100].Name, restored.teamSkill[100])
	}
	if restored.overrides[0].Strategy != strategy || restored.outposts[Red].HumanMode != "score" {
		t.Errorf("overrides not restored: %+v %s", restored.overrides[0], restored.outposts[Red].HumanMode)
	}
	// Team 100 is on the field (R1), so it picks up its saved design immediately.
	if robot := restored.robots[0]; robot.Arch.Name != "turret" || robot.Skill != 1.25 {
		t.Errorf("R1 did not take its team's saved design: %s %.2f", robot.Arch.Name, robot.Skill)
	}
}

func TestPanickingControllerDoesNotStopPhysics(t *testing.T) {
	safe.Dir = t.TempDir()
	world, _, _ := newTestWorld(t)
	world.SetController(0, panicController{})
	before := world.time
	for i := 0; i < 20; i++ {
		_, _, _ = world.tick(1.0 / stepHz)
	}
	if world.time <= before {
		t.Fatal("physics stopped advancing after an AI panic")
	}
}

type panicController struct{}

func (panicController) Update(*Robot, *World, float64) { panic("ai bug") }
