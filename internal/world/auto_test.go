package world

import (
	"math"
	"os"
	"strings"
	"testing"
)

// clearField parks every robot except keep in the far corners of the opposite half, out of the way.
func clearField(world *World, keep *Robot) {
	for i, robot := range world.robots {
		if robot != keep {
			robot.Pos = Vec2{world.layout.Field.Length - 1.5, 1 + float64(i)*1.2}
		}
	}
}

func testLayout(t *testing.T) *Layout {
	t.Helper()
	layout, err := LoadLayout(os.DirFS("../../web"))
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

// Every routine for every design and starting spot is written: it starts on the starting line, its paths are
// connected, and it stays behind the center line (G403).
func TestAutoProgramsAreWellFormed(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B1")
	center := world.layout.Field.Length / 2
	for _, arch := range Archetypes {
		robot.Arch = arch
		for _, routine := range AutoRoutines {
			for _, spot := range StartSpots {
				program := world.buildAutoProgram(robot, routine, spot)
				if len(program.Steps) == 0 {
					t.Fatalf("%s %s: no steps", arch.Name, program.Key)
				}
				at := Vec2{program.StartPose[0], program.StartPose[1]}
				for k, step := range program.Steps {
					if step.Kind != StepPath {
						continue
					}
					first := Vec2{step.Points[0][0], step.Points[0][1]}
					if first.Sub(at).Len() > 1e-6 {
						t.Errorf("%s %s step %d starts at %v, previous ended at %v", arch.Name, program.Key, k, first, at)
					}
					for _, p := range step.Points {
						if p[0] > center {
							t.Errorf("%s %s step %d crosses the center line at x=%.2f", arch.Name, program.Key, k, p[0])
						}
					}
					last := step.Points[len(step.Points)-1]
					at = Vec2{last[0], last[1]}
				}
				if routine == AutoClimb && arch.ClimbLevel > 0 && program.Steps[len(program.Steps)-1].Kind != StepClimb {
					t.Errorf("%s %s does not end with a climb", arch.Name, program.Key)
				}
			}
		}
	}
}

// Red runs the blue-frame routine rotated onto its half of the field.
func TestAutoMirrorsForRed(t *testing.T) {
	layout := testLayout(t)
	step := AutoStep{Kind: StepPath, Points: [][2]float64{{1, 2}, {3.5, 6}}}
	blue := stepPoints(step, Blue, layout)
	red := stepPoints(step, Red, layout)
	for i := range blue {
		if math.Abs(red[i].X-(layout.Field.Length-blue[i].X)) > 1e-9 || math.Abs(red[i].Y-(layout.Field.Width-blue[i].Y)) > 1e-9 {
			t.Fatalf("point %d: red %v is not blue %v rotated", i, red[i], blue[i])
		}
	}
	if h := allianceHeading(Red, 0.3); math.Abs(wrapAngle(h-(0.3+math.Pi))) > 1e-9 {
		t.Fatalf("red heading %.3f", h)
	}
}

// The trapezoidal profile covers the path exactly and respects its limits.
func TestTrajectoryProfile(t *testing.T) {
	trajectory := newTrajectory([]Vec2{{0, 0}, {3, 0}, {3, 4}}, 3, 4)
	if math.Abs(trajectory.length-7) > 1e-9 {
		t.Fatalf("length %.3f", trajectory.length)
	}
	end, v := trajectory.sample(trajectory.total)
	if end.Sub(Vec2{3, 4}).Len() > 1e-6 || v.Len() > 1e-6 {
		t.Fatalf("ends at %v moving %v", end, v)
	}
	for s := 0.0; s < trajectory.total; s += 0.05 {
		if _, v := trajectory.sample(s); v.Len() > 3+1e-6 {
			t.Fatalf("speed %.2f over the limit at %.2f s", v.Len(), s)
		}
	}
}

// A robot with perfect odometry ends its auto paths where they end; one that is shoved off its path does not know it.
func TestAutoPathFollowing(t *testing.T) {
	world, _, stations := newTestWorld(t)
	robot := world.Robot("B2")
	agent := NewAgent(robot, 5)
	world.SetController(robot.Index, agent)
	robot.Arch = archetypeByName("scorer")
	program := world.buildAutoProgram(robot, AutoPreload, "hub")
	start := Vec2{program.StartPose[0], program.StartPose[1]}
	robot.Pos, robot.Heading, robot.Vel = start, 0, Vec2{}
	world.resetOdometry(robot, start, 0, 0.9)
	robot.Odo.scale, robot.Odo.gyroBias, robot.Odo.Vision = 0, 0, false
	clearField(world, robot)
	agent.auto = &autoRun{program: program, stepAt: -1}
	stations[robot.Index].command.Enabled, stations[robot.Index].command.Auto = true, true
	agent.wasAuto = true // do not start a new match (keeps this hand-made run)
	path := program.Steps[0]
	end := Vec2{path.Points[len(path.Points)-1][0], path.Points[len(path.Points)-1][1]}
	for k := 0; k < stepHz*6 && agent.auto.step == 0; k++ {
		world.step(1.0 / stepHz)
	}
	if agent.auto.step == 0 {
		t.Fatal("the path never finished")
	}
	if miss := robot.Pos.Sub(end).Len(); miss > 0.15 {
		t.Fatalf("ended %.2f m from the path end (odometry error %.2f)", miss, robot.odoError())
	}
	// Shove the robot sideways: the estimate does not move with it.
	before := robot.Odo.Est
	robot.Pos = robot.Pos.Add(Vec2{0, 0.5})
	world.step(1.0 / stepHz)
	if robot.Odo.Est.Sub(before).Len() > 0.05 || robot.odoError() < 0.4 {
		t.Fatalf("odometry noticed a shove: error %.2f", robot.odoError())
	}
}

// Vision pulls a drifted estimate back toward the truth when tags are in view.
func TestVisionCorrectsDrift(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B2")
	robot.Team = 254
	hub := world.hubs[Blue]
	// Two metres behind the blue hub, facing it: front camera on the hub's alliance-side tags.
	robot.Pos, robot.Heading, robot.Vel, robot.Omega = hub.Center.Add(Vec2{-2.2, 0}), 0, Vec2{}, 0
	robot.Odo = Odometry{Est: robot.Pos.Add(Vec2{0.6, -0.4}), EstHeading: 0.1, Vision: true}
	start := robot.odoError()
	for k := 0; k < 30; k++ {
		world.time += visionEvery
		world.visionUpdate(robot)
	}
	if robot.Odo.TagsSeen == 0 {
		t.Fatal("no tags in view")
	}
	if robot.odoError() > start*0.25 {
		t.Fatalf("vision only took the error from %.2f to %.2f m", start, robot.odoError())
	}
	// No vision: no correction.
	robot.Odo = Odometry{Est: robot.Pos.Add(Vec2{0.6, -0.4})}
	world.visionUpdate(robot)
	if robot.odoError() < start-1e-9 {
		t.Fatal("a robot without cameras corrected its pose")
	}
}

// Wheel slip on the bumps grows the odometry error.
func TestOdometryDriftsOnBumps(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B2")
	robot.Odo = Odometry{Est: robot.Pos}
	robot.Vel = Vec2{3, 0}
	for k := 0; k < stepHz; k++ {
		world.stepOdometry(robot, 1.0/stepHz, false, 0)
		robot.Pos = robot.Pos.Add(robot.Vel.Scale(1.0 / stepHz))
	}
	flat := robot.odoError()
	robot.Odo.Est = robot.Pos
	for k := 0; k < stepHz; k++ {
		world.stepOdometry(robot, 1.0/stepHz, true, 0)
		robot.Pos = robot.Pos.Add(robot.Vel.Scale(1.0 / stepHz))
	}
	if bumpy := robot.odoError(); bumpy <= flat {
		t.Fatalf("bump drift %.3f not above flat drift %.3f", bumpy, flat)
	}
}

// A blocked path does not wait forever: its trajectory clock runs out and the routine moves on.
func TestBlockedAutoStepTimesOut(t *testing.T) {
	world, _, stations := newTestWorld(t)
	robot := world.Robot("B2")
	agent := NewAgent(robot, 9)
	world.SetController(robot.Index, agent)
	program := world.buildAutoProgram(robot, AutoPreload, "hub")
	start := Vec2{program.StartPose[0], program.StartPose[1]}
	robot.Pos, robot.Heading = start, 0
	world.resetOdometry(robot, start, 0, 0.9)
	clearField(world, robot)
	agent.auto = &autoRun{program: program, stepAt: -1}
	stations[robot.Index].command.Enabled, stations[robot.Index].command.Auto = true, true
	agent.wasAuto = true
	total := newTrajectory(stepPoints(program.Steps[0], Blue, world.layout), program.Steps[0].Speed, program.Steps[0].Accel).total
	for k := 0; k < int((total+autoPathSettle+1)*stepHz) && agent.auto.step == 0; k++ {
		robot.Faults.StuckUntil = world.time + 1 // pinned in place
		world.step(1.0 / stepHz)
	}
	if agent.auto.step == 0 {
		t.Fatal("the blocked path never ended")
	}
	if result := agent.auto.result.Steps[0]; result.EndError < 0.5 {
		t.Fatalf("blocked path reported ending %.2f m off", result.EndError)
	}
}

// Every design with a climber gets onto the low rung from every starting spot, swerve and tank alike.
func TestAutoClimbFromEverySpot(t *testing.T) {
	for _, arch := range Archetypes {
		if arch.ClimbLevel < 1 {
			continue
		}
		for _, spot := range StartSpots {
			world, _, stations := newTestWorld(t)
			robot := world.Robot("B2")
			agent := NewAgent(robot, 5)
			world.SetController(robot.Index, agent)
			robot.Arch = arch
			program := world.buildAutoProgram(robot, AutoClimb, spot)
			start := Vec2{program.StartPose[0], program.StartPose[1]}
			robot.Pos, robot.Heading, robot.Vel = start, 0, Vec2{}
			world.resetOdometry(robot, start, 0, 0.9)
			robot.Odo.scale, robot.Odo.gyroBias, robot.Odo.Vision = 0, 0, false
			clearField(world, robot)
			agent.auto = &autoRun{program: program, stepAt: -1}
			stations[robot.Index].command.Enabled, stations[robot.Index].command.Auto = true, true
			agent.wasAuto = true
			for k := 0; k < int(AutoSec*stepHz); k++ {
				world.step(1.0 / stepHz)
			}
			if robot.ClimbLevel < 1 {
				t.Errorf("%s from %s did not climb (at %v heading %.2f)", arch.Name, spot, robot.Pos, robot.Heading)
			}
		}
	}
}

// A bench run plays the routines; teams tune them over an event.
func TestAutoBench(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole matches")
	}
	layout := testLayout(t)
	report := RunBench(layout, BenchOptions{Matches: 12, Seed: 11}, nil)
	ran, completed, steps, mistakes, fuel := 0, 0, 0, 0, 0
	for _, match := range report.Matches {
		for _, r := range match.Robots {
			if r.Auto == nil {
				continue
			}
			ran++
			fuel += r.Auto.Fuel
			if r.Auto.Mistake != "" {
				mistakes++
			}
			for _, step := range r.Auto.Steps {
				steps++
				if step.Completed {
					completed++
				}
			}
		}
	}
	if ran < 60 {
		t.Fatalf("only %d robot-matches ran an auto routine", ran)
	}
	t.Logf("%d autos, %.0f%% of steps completed, %.1f auto FUEL per robot, %d chooser mistakes, avg auto fuel/alliance %.1f",
		ran, 100*float64(completed)/float64(max(1, steps)), float64(fuel)/float64(ran), mistakes,
		func() float64 {
			total := 0
			for _, m := range report.Matches {
				total += m.Alliances[0].AutoFuel + m.Alliances[1].AutoFuel
			}
			return float64(total) / float64(2*len(report.Matches))
		}())
	if fuel == 0 {
		t.Fatal("no auto routine scored any FUEL")
	}
	tuned := 0
	for _, entry := range report.Autos {
		if len(entry.Notes) > 0 {
			tuned++
			t.Logf("%d %s (r%d, %d runs, %.1f pts): %s", entry.Team, entry.Name, entry.Revision, entry.Runs, entry.AvgPoints,
				strings.Join(entry.Notes, "; "))
		}
	}
	if tuned == 0 {
		t.Error("no team tuned an auto routine over the event")
	}
}
