package world

import (
	"math"
	"testing"
)

// fillHopper loads n FUEL into a robot the way the intake does (through the entry), letting each settle.
func fillHopper(world *World, robot *Robot, n int) {
	for k := 0; k < n; k++ {
		for _, ball := range world.balls {
			if ball.State == BallFree {
				ball.State, ball.Owner = BallHeld, robot.Index
				ball.intakeFrom = [3]float64{0.4, (float64(k%5)/4 - 0.5) * 0.5, 0.1} // across the intake
				ball.intakeT = 1
				robot.enterHopper(ball)
				robot.Hopper++
				break
			}
		}
		for step := 0; step < stepHz/4; step++ {
			world.stepHoppers(1.0 / stepHz)
		}
	}
	for step := 0; step < stepHz*2; step++ {
		world.stepHoppers(1.0 / stepHz)
	}
}

func heldBalls(world *World, robot *Robot) []*Ball {
	var held []*Ball
	for _, ball := range world.balls {
		if ball.State == BallHeld && ball.Owner == robot.Index {
			held = append(held, ball)
		}
	}
	return held
}

func TestHopperHoldsItsCapacity(t *testing.T) {
	for _, name := range []string{"scorer", "tall", "bigdumper", "kitbot", "turret"} {
		world, _, _ := newTestWorld(t)
		robot := world.Robot("B1")
		robot.Arch = archetypeByName(name)
		for _, ball := range heldBalls(world, robot) {
			ball.State = BallFree // empty it
		}
		robot.Hopper = 0
		fillHopper(world, robot, robot.Arch.Capacity)
		shape := robot.Arch.hopperShape()
		r := world.ballRadius
		worst := 0.0
		held := heldBalls(world, robot)
		for i, a := range held {
			p := a.hopP
			if p[0] < shape.back+r-0.01 || p[0] > shape.front-r+0.01 || math.Abs(p[1]) > shape.halfWidth-r+0.01 ||
				p[2] < shape.floor+r-0.01 || p[2] > shape.ceilingAt(p[0])-r+0.01 {
				t.Fatalf("%s: FUEL %d outside its hopper at %v (%+v)", name, i, p, shape)
			}
			for _, b := range held[i+1:] {
				dx, dy, dz := a.hopP[0]-b.hopP[0], a.hopP[1]-b.hopP[1], a.hopP[2]-b.hopP[2]
				worst = math.Max(worst, 2*r-math.Sqrt(dx*dx+dy*dy+dz*dz))
			}
		}
		t.Logf("%-9s holds %3d, worst squeeze %.1f cm", name, robot.Arch.Capacity, worst*100)
		if worst > 0.035 {
			t.Errorf("%s: a full hopper squeezes FUEL %.1f cm into each other (foam gives ~1-2 cm)", name, worst*100)
		}
	}
}

func TestHopperSloshes(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B1")
	robot.Arch = archetypeByName("scorer")
	for _, ball := range heldBalls(world, robot) {
		ball.State = BallFree
	}
	robot.Hopper = 0
	fillHopper(world, robot, 4)
	meanX := func() float64 {
		sum := 0.0
		held := heldBalls(world, robot)
		for _, ball := range held {
			sum += ball.hopP[0]
		}
		return sum / float64(len(held))
	}
	// Driving at 4 m/s, then a hard stop: the FUEL slides forward. Then flooring it: back it goes.
	robot.Heading = 0
	drive := func(from, to, rate float64) {
		robot.Vel = Vec2{from, 0}
		robot.hopperVel = robot.Vel
		for step := 0; step < stepHz; step++ {
			if from > to {
				robot.Vel = Vec2{math.Max(to, robot.Vel.X-rate/stepHz), 0}
			} else {
				robot.Vel = Vec2{math.Min(to, robot.Vel.X+rate/stepHz), 0}
			}
			world.stepHoppers(1.0 / stepHz)
		}
	}
	start := meanX()
	drive(4, 0, 12)
	braked := meanX()
	drive(0, 4, 10)
	accelerated := meanX()
	t.Logf("mean FUEL x: %.3f, after braking %.3f, after accelerating %.3f", start, braked, accelerated)
	if braked < start+0.05 || accelerated > braked-0.05 {
		t.Errorf("FUEL should slide forward when braking and back when accelerating")
	}
}

func TestFuelOutOfFieldComesBack(t *testing.T) {
	world, _, _ := newTestWorld(t)
	for _, robot := range world.robots {
		robot.Present = false
	}
	ball := world.balls[len(world.balls)-1]
	*ball = Ball{State: BallFree, P: [3]float64{3, 0.4, 1.2}, V: [3]float64{0, -3, 1}}
	sawOut, maxJump := false, 0.0
	for step := 0; step < stepHz*20; step++ {
		before := ball.P
		run(world, 1.0/stepHz)
		if ball.State == BallOut {
			sawOut = true
		}
		dx, dy := ball.P[0]-before[0], ball.P[1]-before[1]
		maxJump = math.Max(maxJump, math.Hypot(dx, dy))
	}
	if !sawOut {
		t.Fatal("a ball over the guardrail should leave the field")
	}
	if ball.State != BallFree || ball.P[1] < 0 {
		t.Fatalf("the field staff should have tossed it back in (state %d at %v)", ball.State, ball.P)
	}
	if maxJump > 0.2 {
		t.Errorf("the ball teleported %.2f m in one step (it should fly back in)", maxJump)
	}
}

func TestPinnedFuelSqueezesOut(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B1")
	for _, other := range world.robots {
		if other != robot {
			other.Present = false
		}
	}
	// The robot drives sideways into the guardrail with a ball between them.
	robot.Heading = 0
	robot.Pos = Vec2{4, robot.half().Y + 0.2}
	ball := world.balls[len(world.balls)-1]
	*ball = Ball{State: BallFree, P: [3]float64{4, world.ballRadius + 0.01, world.ballRadius}}
	for step := 0; step < stepHz; step++ {
		robot.Pos.Y = math.Max(robot.half().Y+0.01, robot.Pos.Y-0.5/stepHz)
		world.interactRobotsBalls()
		world.stepBalls(1.0 / stepHz)
	}
	box := robot.obb()
	if _, _, inside := circleOBB(Vec2{ball.P[0], ball.P[1]}, world.ballRadius*0.6, box); inside {
		t.Errorf("FUEL pinned against the guardrail ended up inside the robot at %v", ball.P)
	}
}
