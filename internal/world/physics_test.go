package world

import (
	"math"
	"testing"
)

// physicsWorld is a test world with only the named robots on the field, the rest parked off it.
func physicsWorld(t *testing.T, stations ...string) (*World, []*Robot) {
	t.Helper()
	world, _, _ := newTestWorld(t)
	var robots []*Robot
	for _, robot := range world.robots {
		keep := false
		for _, station := range stations {
			keep = keep || robot.Station == station
		}
		if !keep {
			robot.Present = false
			continue
		}
		robot.Enabled = true
		robot.Voltage = 12.5
		robot.Vel, robot.Omega, robot.Heading = Vec2{}, 0, 0
		robot.Z, robot.Pitch, robot.Roll = 0, 0, 0
		robots = append(robots, robot)
	}
	return world, robots
}

// physicsStep runs just the robot physics (no AI, no FUEL).
func physicsStep(world *World, robots []*Robot, seconds float64) {
	dt := 1.0 / stepHz
	for t := 0.0; t < seconds; t += dt {
		world.time += dt
		for _, robot := range robots {
			world.stepRobot(robot, dt)
		}
		world.collideRobots()
		for _, robot := range robots {
			world.collideRobotStatics(robot)
		}
	}
}

func TestDrivetrainAcceleration(t *testing.T) {
	world, robots := physicsWorld(t, "R1")
	world.statics = nil // an open runway
	robot := robots[0]
	robot.Arch = archetypeByName("scorer")
	robot.Pos = Vec2{0.8, 1.2}
	robot.Ctl.Vel = Vec2{10, 0}
	var to3 float64
	for t := 0.0; t < 2.4; t += 1.0 / stepHz {
		physicsStep(world, robots, 1.0/stepHz)
		if to3 == 0 && robot.Vel.X >= 3 {
			to3 = t
		}
	}
	if to3 < 0.35 || to3 > 1.0 {
		t.Errorf("0 to 3 m/s took %.2f s, want a swerve robot's 0.35-1.0 s", to3)
	}
	if top := robot.Vel.X; math.Abs(top-robot.Arch.MaxSpeed) > 0.15 {
		t.Errorf("top speed %.2f m/s, want the design's %.2f", top, robot.Arch.MaxSpeed)
	}
	if robot.Load < 10 || robot.Load > 120 {
		t.Errorf("cruising current %.0f A is implausible", robot.Load)
	}
	// From rest, the launch pulls far more current than cruising.
	robot.Vel = Vec2{}
	robot.Pos = Vec2{8.27, 1.2}
	physicsStep(world, robots, 0.05)
	if robot.Load < 120 {
		t.Errorf("launch current %.0f A, want well over 120 A", robot.Load)
	}

	// A sagging battery is a slower robot (back-EMF reaches the lower voltage sooner).
	robot.Voltage = 9
	robot.Vel = Vec2{}
	robot.Pos = Vec2{0.8, 1.2}
	physicsStep(world, robots, 2.4)
	if sag := robot.Vel.X; sag > robot.Arch.MaxSpeed*0.92 || sag < robot.Arch.MaxSpeed*0.55 {
		t.Errorf("at 9 V the robot reached %.2f m/s (full battery %.2f)", sag, robot.Arch.MaxSpeed)
	}
}

func TestPushingMatch(t *testing.T) {
	world, robots := physicsWorld(t, "R1", "B1")
	pusher, other := robots[0], robots[1]
	pusher.Arch, other.Arch = archetypeByName("scorer"), archetypeByName("scorer")
	frame := pusher.half().X + other.half().X
	pusher.Pos, other.Pos = Vec2{7, 2}, Vec2{7 + frame + 0.01, 2}
	pusher.Ctl.Vel = Vec2{4, 0}
	// A disabled robot (brake mode) gets shoved, but well below free speed.
	other.Enabled = false
	physicsStep(world, robots, 2)
	if other.Pos.X-7-frame < 0.3 || other.Vel.X > 2.5 {
		t.Errorf("a disabled robot should be shoved slowly: moved %.2f m, at %.2f m/s", other.Pos.X-7-frame, other.Vel.X)
	}
	// The same robot driving back at full power holds a stalemate.
	other.Enabled = true
	other.Ctl.Vel = Vec2{-4, 0}
	other.Vel, pusher.Vel = Vec2{}, Vec2{}
	start := other.Pos.X
	physicsStep(world, robots, 2)
	if moved := math.Abs(other.Pos.X - start); moved > 0.25 {
		t.Errorf("equal robots pushing each other should stall, moved %.2f m", moved)
	}
	// A heavy, grippy defender wins.
	other.Arch = archetypeByName("defender")
	physicsStep(world, robots, 2)
	if other.Pos.X > start-0.3 {
		t.Errorf("a heavier, grippier defender should push the scorer back (moved %.2f m)", start-other.Pos.X)
	}
}

func TestHitsRockButDoNotTip(t *testing.T) {
	world, robots := physicsWorld(t, "R1", "B1")
	tall, hitter := robots[0], robots[1]
	tall.Arch, hitter.Arch = archetypeByName("tall"), archetypeByName("defender")
	tall.Heading = math.Pi / 2 // side on
	tall.Pos = Vec2{8, 4}
	hitter.Pos = Vec2{8 - tall.half().Y - hitter.half().X - 0.3, 4}
	hitter.Vel = Vec2{5, 0} // T-bone at 5 m/s
	hitter.Ctl.Vel = Vec2{5, 0}
	maxTilt := 0.0
	for k := 0; k < 120; k++ {
		physicsStep(world, robots, 1.0/stepHz)
		maxTilt = math.Max(maxTilt, tall.tilt)
	}
	if tall.Faults.Tipped {
		t.Errorf("a 5 m/s T-bone should not tip a robot built low and heavy")
	}
	if maxTilt <= 0.005 {
		t.Errorf("a hard T-bone should rock the robot a little (max tilt %.3f rad)", maxTilt)
	}

	// Full speed into the alliance wall: the far side lifts, the robot does not go over.
	world, robots = physicsWorld(t, "R1")
	robot := robots[0]
	robot.Arch = archetypeByName("tall")
	robot.Pos = Vec2{world.layout.Field.Length - 1.5, 4}
	robot.Ctl.Vel = Vec2{10, 0}
	maxTilt = 0
	for k := 0; k < 360; k++ {
		physicsStep(world, robots, 1.0/stepHz)
		maxTilt = math.Max(maxTilt, robot.tilt)
	}
	if robot.Faults.Tipped || maxTilt <= 0.005 {
		t.Errorf("a wall slam should lift the robot a little without tipping it (max tilt %.3f, tipped %v)", maxTilt,
			robot.Faults.Tipped)
	}
}

func TestTippingIsPhysicalAndCanBeTurnedOff(t *testing.T) {
	for _, tipping := range []bool{true, false} {
		world, robots := physicsWorld(t, "R1")
		world.tipping = tipping
		robot := robots[0]
		robot.Arch = archetypeByName("tall")
		robot.Pos = Vec2{8, 4}
		// A blow far harder than any robot can deliver (a robot's whole momentum several times over).
		robot.contactImpulse = Vec2{-1500, 0}
		robot.Vel = Vec2{-1500 / robot.Arch.Mass, 0}
		robot.Enabled = false
		physicsStep(world, robots, 2)
		if robot.Faults.Tipped != tipping {
			t.Errorf("tipping %v: tipped = %v", tipping, robot.Faults.Tipped)
		}
	}
}

func TestBumpCrossing(t *testing.T) {
	cross := func(name string, heading float64) (maxBelly float64, crossed bool) {
		world, robots := physicsWorld(t, "R1")
		robot := robots[0]
		robot.Arch = archetypeByName(name)
		bump := world.bumps[0]
		center := bump.Center()
		robot.Pos = Vec2{bump.MinX - 0.8, center.Y}
		robot.Heading = heading
		robot.Ctl.Vel = Vec2{2, 0}
		for k := 0; k < 4*stepHz; k++ {
			physicsStep(world, robots, 1.0/stepHz)
			maxBelly = math.Max(maxBelly, robot.bellyLoad)
			if robot.Pos.Y < bump.MinY+0.4 || robot.Pos.Y > bump.MaxY-0.4 {
				robot.Pos.Y = center.Y // stay on the bump
			}
		}
		return maxBelly, robot.Pos.X > bump.MaxX+0.2
	}
	if belly, crossed := cross("scorer", 0); !crossed || belly > 0.1 {
		t.Errorf("a robot crossing square-on should ride over (crossed %v, belly load %.2f)", crossed, belly)
	}
	if belly, crossed := cross("bigdumper", math.Pi/4); !crossed || belly > 0.1 {
		t.Errorf("a skewed robot rides on the wheels nearest the crest (crossed %v, belly load %.2f)", crossed, belly)
	}
	if belly, _ := cross("kitbot", 0); belly < 0.5 {
		t.Errorf("a low kit chassis should ground its belly on the crest (belly load %.2f)", belly)
	}
}

// flyBall launches one free FUEL and runs only ball physics until it comes to rest or time runs out.
func flyBall(world *World, v, w [3]float64, seconds float64) *Ball {
	for _, ball := range world.balls {
		if ball != world.balls[0] {
			ball.State = BallOut // clear the field so nothing is in the way
			ball.outAt = 1e9
		}
	}
	ball := world.balls[0]
	ball.State = BallFree
	ball.P = [3]float64{3, 4, 0.5}
	ball.V, ball.W, ball.sleep = v, w, 0
	dt := 1.0 / stepHz
	for t := 0.0; t < seconds; t += dt {
		world.stepBalls(dt)
	}
	return ball
}

func TestBackspinLiftsAShot(t *testing.T) {
	world, _ := physicsWorld(t)
	world.statics = nil
	speed, angle := 8.0, 0.9
	v := [3]float64{speed * math.Cos(angle), 0, speed * math.Sin(angle)}
	plain := flyBall(world, v, [3]float64{}, 0.9).P
	spun := flyBall(world, v, backspin(speed, 0, defaultShotSpin, world.ballRadius), 0.9).P
	if spun[2] <= plain[2]+0.05 {
		t.Errorf("backspin should hold a shot up (height after 0.9 s: %.2f with spin, %.2f without)", spun[2], plain[2])
	}
	// The robot code's solver flies the same physics, so a shot it solves for lands at its target.
	distance, dz := 4.0, 1.4
	speed, angle, ok := solveLaunchSpin(distance, dz, defaultShotSpin)
	if !ok {
		t.Fatal("no solution for a 4 m shot")
	}
	if h := trajectoryHeight(speed, angle, defaultShotSpin, distance); math.Abs(h-dz) > 0.03 {
		t.Errorf("solved shot reaches %.2f m at the target, want %.2f", h, dz)
	}
}

func TestFoamBounce(t *testing.T) {
	if soft, hard := foamBounce(groundBounce, 1), foamBounce(groundBounce, 9); hard >= soft || hard < 0.1 {
		t.Errorf("foam should be deader the harder it is hit: %.2f at 1 m/s, %.2f at 9 m/s", soft, hard)
	}
	if e := foamBounce(groundBounce, bounceRefSpeed); math.Abs(e-groundBounce) > 1e-9 {
		t.Errorf("at the reference speed the bounce should be the nominal %.2f, got %.2f", groundBounce, e)
	}
	// A ball skidding across the carpet with backspin checks up: it travels less far than one that is already rolling.
	world, _ := physicsWorld(t)
	world.statics = nil
	r := world.ballRadius
	rolling := flyBall(world, [3]float64{3, 0, 0}, [3]float64{0, 3 / r, 0}, 3).P[0]
	backspun := flyBall(world, [3]float64{3, 0, 0}, [3]float64{0, -3 / r, 0}, 3).P[0]
	if backspun >= rolling-0.2 {
		t.Errorf("a backspinning ball should check up (went %.2f m vs %.2f m rolling)", backspun-3, rolling-3)
	}
}
