package world

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/ds"
)

type fakeField struct {
	scored [2][4]int
	motor  [2]bool
	light  [2]bool
}

func (f *fakeField) ScoreFuel(alliance, sensor int) { f.scored[alliance][sensor]++ }
func (f *fakeField) HubMotorsOn() (bool, bool)      { return f.motor[0], f.motor[1] }
func (f *fakeField) HubLightsOn() (bool, bool)      { return f.light[0], f.light[1] }
func (f *fakeField) total(alliance int) (count int) {
	for _, n := range f.scored[alliance] {
		count += n
	}
	return
}

type fakeStation struct{ command ds.Command }

func (s *fakeStation) Command() ds.Command { return s.command }
func (s *fakeStation) SetLoad(float64)     {}

func newTestWorld(t *testing.T) (*World, *fakeField, [6]*fakeStation) {
	t.Helper()
	layout, err := LoadLayout(os.DirFS("../../web"))
	if err != nil {
		t.Fatal(err)
	}
	world := New(layout, bus.New())
	world.reseedLocked(1) // repeatable tests
	field := &fakeField{}
	var stations [6]*fakeStation
	var links [6]StationLink
	for i := range stations {
		stations[i] = &fakeStation{command: ds.Command{Team: 100 + i, RobotLinked: true, FmsLinked: true}}
		links[i] = stations[i]
	}
	world.Attach(links, field)
	world.step(1.0 / stepHz) // robots become present
	world.resetLocked()
	return world, field, stations
}

func run(world *World, seconds float64) {
	for t := 0.0; t < seconds; t += 1.0 / stepHz {
		world.step(1.0 / stepHz)
	}
}

func TestStagingMatchesManual(t *testing.T) {
	world, _, _ := newTestWorld(t)
	counts := map[int]int{}
	for _, ball := range world.balls {
		counts[ball.State]++
	}
	if len(world.balls) != 504 {
		t.Fatalf("expected 504 FUEL, got %d", len(world.balls))
	}
	if counts[BallHeld] != 48 || counts[BallChute] != 48 || counts[BallFree] != 408 {
		t.Fatalf("staging wrong: %v (want 48 preloaded, 48 in chutes, 360 neutral + 48 depot)", counts)
	}
}

func TestShotScoresThroughSensor(t *testing.T) {
	world, field, _ := newTestWorld(t)
	robot := world.Robot("B2")
	robot.Arch = archetypeByName("turret") // aims without turning the robot
	robot.Enabled = true
	hub := world.hubs[Blue]
	robot.Pos = hub.Center.Add(Vec2{-2.6, 0.2})
	scoredBefore := field.total(Blue)
	// With the exit motors off (before the arena starts them) the FUEL drops in and waits at the exits: only the
	// FUEL sitting in front of a lane's beam is counted.
	for shot := 0; shot < 8; shot++ {
		if !world.launch(robot, timingNone) {
			t.Fatalf("no firing solution from %.1f m", robot.Pos.Sub(hub.Center).Len())
		}
		run(world, 2.0)
	}
	entered := world.stats.Entered[Blue]
	if entered < 6 {
		t.Fatalf("expected most shots from 2.6 m to go in, %d/8 did", entered)
	}
	inside := 0
	for _, ball := range world.balls {
		if ball.State == BallHub && ball.Owner == Blue {
			inside++
		}
	}
	if inside != entered || field.total(Blue)-scoredBefore > len(world.layout.Hub.Exit.Lanes) {
		t.Fatalf("with the motors off the hub should hold all %d (holds %d), counting one per lane (counted %d)",
			entered, inside, field.total(Blue)-scoredBefore)
	}
	// The motors start: the queues roll through the beams one at a time, every FUEL is counted and leaves.
	field.motor = [2]bool{true, true}
	run(world, 4)
	if made := field.total(Blue) - scoredBefore; made != entered || hub.Stored != 0 {
		t.Fatalf("with the motors on every FUEL in should be counted (%d of %d) and leave (%d still counted inside)",
			made, entered, hub.Stored)
	}
}

func TestBackNetBlocksNeutralZoneShots(t *testing.T) {
	world, field, _ := newTestWorld(t)
	robot := world.Robot("B1")
	hub := world.hubs[Blue]
	robot.Pos = hub.Center.Add(Vec2{2.6, 0}) // neutral-zone side
	before := field.total(Blue)
	for shot := 0; shot < 5; shot++ {
		world.launch(robot, timingNone)
		run(world, 2.0)
	}
	if got := field.total(Blue) - before; got > 1 {
		t.Fatalf("back net should block shots from the neutral zone, %d scored", got)
	}
}

func TestTrenchClearance(t *testing.T) {
	world, _, stations := newTestWorld(t)
	for i := 0; i < 5; i++ {
		stations[i].command.Team = 0 // only B3 on the field
	}
	world.step(1.0 / stepHz)
	l := world.layout
	hubX := l.Hub.Positions["blue"][0]
	for _, tc := range []struct {
		arch   string
		passes bool
	}{{"scorer", true}, {"tall", false}} {
		robot := world.Robot("B3")
		robot.Arch = archetypeByName(tc.arch)
		robot.Manual = true
		stations[5].command.Enabled = true
		robot.Pos = Vec2{hubX - 1.4, l.Trench.OpeningCenterY}
		robot.Heading = 0
		robot.Vel = Vec2{}
		for step := 0; step < stepHz*3; step++ {
			robot.Ctl = Control{Vel: Vec2{2, 0}}
			world.manualAt[robot.Index] = timeNow()
			world.step(1.0 / stepHz)
		}
		crossed := robot.Pos.X > hubX+0.8
		if crossed != tc.passes {
			t.Errorf("%s robot crossed trench = %v (x=%.2f), want %v", tc.arch, crossed, robot.Pos.X, tc.passes)
		}
	}
}

func TestLaunchSolverDescends(t *testing.T) {
	for _, distance := range []float64{1.5, 2.5, 4, 5.5} {
		speed, angle, ok := solveLaunch(distance, 1.3)
		if !ok || speed <= 0 || math.IsNaN(speed) {
			t.Errorf("distance %.1f: no descending solution (speed %.2f angle %.2f)", distance, speed, angle)
		}
	}
}

func timeNow() time.Time { return time.Now() }
