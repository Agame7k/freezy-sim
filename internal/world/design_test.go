package world

import (
	"testing"
)

func TestBuildNameRoundTrip(t *testing.T) {
	for _, b := range kitBuilds[:200] {
		b.Ballast, b.Tune = 5, 2
		back, ok := parseBuild(b.Name())
		if !ok || back != b {
			t.Fatalf("%s did not round-trip: %+v", b.Name(), back)
		}
	}
	if arch := archetypeByName("turret"); arch.Class != "turret" || arch.Model.Shooter != "turret" {
		t.Errorf("stock designs should carry their class and model")
	}
}

func TestTeamsBuildLegalRobots(t *testing.T) {
	world, _, _ := newTestWorld(t)
	classes := map[string]int{}
	for team := 1; team <= 120; team++ {
		world.mu.Lock()
		arch := world.designForLocked(team * 37)
		world.mu.Unlock()
		build := arch.Spec
		if ok, why := build.legal(); !ok {
			t.Fatalf("team %d built an illegal robot (%s): %s", team*37, why, arch.Label)
		}
		if build.cost() > teamTraits(team*37).Budget {
			t.Fatalf("team %d spent over its budget", team*37)
		}
		if again, _ := designRobot(team*37, nil); team == 1 && again.Name() == "" {
			t.Fatal("no design")
		}
		classes[arch.Class]++
	}
	t.Logf("classes built: %v", classes)
	if len(classes) < 4 {
		t.Errorf("teams should build a variety of robots, got %v", classes)
	}
	// Same team, same event: same robot.
	first, _ := designRobot(254, nil)
	second, _ := designRobot(254, nil)
	if first != second {
		t.Errorf("design season should be deterministic")
	}
}

func TestPitChangeAfterMissedClimbs(t *testing.T) {
	world, _, _ := newTestWorld(t)
	world.mu.Lock()
	defer world.mu.Unlock()
	team := 1
	for ; team < 500; team++ {
		if arch := world.designForLocked(team); arch.ClimbLevel >= 2 {
			break
		}
	}
	stats := world.teamStats(team)
	level := stats.Design.Build.Climber
	for match := 0; match < 2; match++ {
		stats.Matches++
		world.reviewDesignLocked(BenchRobot{Team: team, ClimbAttempted: true, ClimbGoal: level}, stats)
	}
	if got := world.teamArch[team].ClimbLevel; got != level-1 || len(stats.Design.Changes) != 1 {
		t.Fatalf("a team that keeps missing its climb should build down a level: L%d → L%d", level, got)
	}
	stats.Design.Locked = true
	before := world.teamArch[team].Name
	for match := 0; match < 3; match++ {
		world.reviewDesignLocked(BenchRobot{Team: team, ClimbAttempted: true, ClimbGoal: level}, stats)
	}
	if world.teamArch[team].Name != before {
		t.Errorf("a locked design must not change")
	}
}
