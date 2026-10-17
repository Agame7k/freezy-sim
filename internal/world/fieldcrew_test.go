package world

import (
	"testing"
)

// After a "match" scatters FUEL everywhere, the between-match reset carts the robots off and on and the crew
// restages every FUEL, in a realistic time, without losing any.
func TestFieldResetRestagesEverything(t *testing.T) {
	world, _, stations := newTestWorld(t)
	// Scatter: everything free on the field lands somewhere random, some in a hub and a corral, robots hold some.
	for i, ball := range world.balls {
		switch {
		case i%17 == 0:
			ball.State, ball.Owner = BallHub, i%2
			ball.hubPhase = 2
		case i%23 == 0:
			ball.State, ball.Owner = BallCorral, i%2
		case ball.State == BallFree:
			ball.P = [3]float64{1 + world.rng.Float64()*14.5, 0.5 + world.rng.Float64()*7, world.ballRadius}
		}
	}
	// The next match brings different teams.
	for i, station := range stations {
		station.command.Team = 900 + i
	}
	world.FieldReset()
	start := world.time
	for world.time-start < 240 {
		run(world, 1)
		carting, crew := false, world.crew != nil
		for _, robot := range world.robots {
			carting = carting || robot.Cart != CartNone
		}
		if !carting && !crew {
			break
		}
	}
	took := world.time - start
	if world.crew != nil {
		t.Fatalf("crew still working after %.0f s", took)
	}
	if took < 20 || took > 150 {
		t.Errorf("field reset took %.0f s; expected a realistic 20–150 s", took)
	}
	counts := map[int]int{}
	for _, ball := range world.balls {
		counts[ball.State]++
	}
	if counts[BallCrew] != 0 || counts[BallOut] != 0 || counts[BallHub] != 0 {
		t.Fatalf("FUEL left behind: %v", counts)
	}
	if counts[BallChute] != 48 {
		t.Errorf("chutes hold %d, want 48 (%v)", counts[BallChute], counts)
	}
	for _, robot := range world.robots {
		if robot.Team != 900+robot.Index {
			t.Errorf("%s still has team %d after the swap", robot.Station, robot.Team)
		}
		if robot.Hopper != 8 {
			t.Errorf("%s preloaded %d FUEL, want 8", robot.Station, robot.Hopper)
		}
		start, _ := world.startPoseFor(robot)
		if robot.Pos.Sub(start).Len() > 0.05 || robot.Z != 0 {
			t.Errorf("%s not at its start pose: %v vs %v (z %.2f)", robot.Station, robot.Pos, start, robot.Z)
		}
	}
	if total := counts[BallFree] + counts[BallChute] + counts[BallHeld]; total != 504 {
		t.Errorf("accounted for %d FUEL, want 504 (%v)", total, counts)
	}
	t.Logf("reset took %.0f s: %v", took, counts)
}

// The field signals drive the break: purple brings the crew on (robots stay), green takes the robots off (and they
// wait off the field), and the next match load brings the next robots on.
func TestFieldSignalsDriveTheReset(t *testing.T) {
	world, _, stations := newTestWorld(t)
	// A match was played.
	for _, station := range stations {
		station.command.Enabled = true
	}
	run(world, 0.5)
	for _, station := range stations {
		station.command.Enabled = false
	}
	run(world, 0.5)

	world.FieldCleanup()
	run(world, 5)
	for _, robot := range world.robots {
		if robot.Cart != CartNone {
			t.Fatalf("field cleanup (purple) should not move robots; %s is carting", robot.Station)
		}
	}
	if world.crew == nil {
		t.Fatal("field cleanup (purple) should bring the crew on")
	}

	world.FieldSafe()
	run(world, 60)
	for _, robot := range world.robots {
		if robot.Present && robot.Cart != CartOff {
			t.Fatalf("after field safe (green) robots should be off and waiting for the next match; %s cart %d", robot.Station, robot.Cart)
		}
	}

	for i, station := range stations {
		station.command.Team = 700 + i
	}
	world.FieldReset() // the next match loads
	run(world, 40)
	for _, robot := range world.robots {
		if robot.Present && (robot.Cart != CartNone || robot.Team < 700) {
			t.Fatalf("after the next match loads its robots should be on the field; %s team %d cart %d", robot.Station, robot.Team, robot.Cart)
		}
	}
}
