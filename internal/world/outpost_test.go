package world

import (
	"math"
	"testing"
)

// throwRate throws n corral FUEL from an outpost and returns the share that entered the hub.
func throwRate(t *testing.T, accuracy float64, n int) float64 {
	world, _, _ := newTestWorld(t)
	for _, robot := range world.robots {
		robot.Present = false // an empty field: only the throws
	}
	outpost := world.outposts[Blue]
	outpost.Accuracy = accuracy
	made := 0
	for k := 0; k < n; k++ {
		// Put one ball in the corral and throw it.
		for _, ball := range world.balls {
			if ball.State == BallFree {
				ball.State, ball.Owner = BallCorral, Blue
				outpost.Corral++
				break
			}
		}
		before := outpost.Made
		outpost.throw(world)
		for step := 0; step < stepHz*3 && outpost.Made == before; step++ {
			world.stepBalls(1.0 / stepHz)
			for _, ball := range world.balls {
				if ball.State == BallFree && ball.thrower > 0 {
					world.hubs[Blue].interact(world, ball, world.ballRadius)
				}
			}
		}
		if outpost.Made > before {
			made++
		}
	}
	return float64(made) / float64(n)
}

func TestHumanPlayerThrows(t *testing.T) {
	if rate := throwRate(t, 1, 60); rate < 0.98 {
		t.Errorf("a perfect human player should sink every throw, made %.0f%%", rate*100)
	}
	rate := throwRate(t, 0.8, 300)
	t.Logf("default human player makes %.0f%% of throws", rate*100)
	if math.Abs(rate-0.8) > 0.1 {
		t.Errorf("default human player should make about 80%% of throws, made %.0f%%", rate*100)
	}
}

func TestBallThroughFloorOpeningReachesCorral(t *testing.T) {
	world, _, _ := newTestWorld(t)
	outpost := world.outposts[Blue]
	ball := world.balls[len(world.balls)-1]
	*ball = Ball{State: BallFree, P: [3]float64{0.6, outpost.chuteY, world.ballRadius}, V: [3]float64{-2, 0, 0}}
	for step := 0; step < stepHz && ball.State == BallFree; step++ {
		world.stepBalls(1.0 / stepHz)
	}
	if ball.State != BallCorral || outpost.Corral != 1 {
		t.Fatalf("ball rolled into the floor opening should reach the corral (state %d, corral %d)", ball.State, outpost.Corral)
	}
	// The human player loads it into an empty chute.
	for _, b := range world.balls {
		if b.State == BallChute && b.Owner == Blue {
			b.State = BallOut
		}
	}
	outpost.HumanMode = "feed"
	before := outpost.chuteCount(world)
	outpost.step(world, 0.1)
	if outpost.chuteCount(world) != before+1 {
		t.Errorf("human player should load the corral FUEL into the chute")
	}
}

// stockCorral moves n free FUEL into an alliance's corral.
func stockCorral(world *World, alliance, n int) {
	for _, ball := range world.balls {
		if n == 0 {
			return
		}
		if ball.State == BallFree {
			ball.State, ball.Owner = BallCorral, alliance
			world.outposts[alliance].Corral++
			n--
		}
	}
}

func TestHumanPlayerPace(t *testing.T) {
	world, _, _ := newTestWorld(t)
	outpost := world.outposts[Blue]
	outpost.HumanMode = "score"
	stockCorral(world, Blue, 30)
	world.matchClock = func() (float64, bool) { return 40, true }
	for step := 0; step < stepHz*20; step++ {
		outpost.step(world, 1.0/stepHz)
	}
	if outpost.Throws < 15 {
		t.Errorf("a default human player should throw about once a second, threw %d in 20 s", outpost.Throws)
	}
}

func TestHumanPlayerStopsAtBuzzer(t *testing.T) {
	world, _, _ := newTestWorld(t)
	outpost := world.outposts[Blue]
	stockCorral(world, Blue, CorralCapacity)
	for _, mode := range []string{"auto", "score", "feed"} {
		outpost.HumanMode = mode
		for _, clock := range []float64{TeleopEndSec + 0.5, TeleopEndSec + 30} {
			world.matchClock = func() (float64, bool) { return clock, true } // PostMatch still reports a match
			throws, chute := outpost.Throws, outpost.chuteCount(world)
			for step := 0; step < stepHz*5; step++ {
				outpost.step(world, 1.0/stepHz)
			}
			if outpost.Throws != throws || outpost.chuteCount(world) > chute {
				t.Errorf("%s human player kept playing at t=%.1f after the match", mode, clock)
			}
		}
	}
}

// A robot bulldozing loose FUEL into its outpost wall drives it through the floor opening into the corral.
func TestPushFuelIntoOutpost(t *testing.T) {
	world, _, stations := newTestWorld(t)
	robot := world.Robot("B1")
	for _, other := range world.robots {
		if other != robot {
			other.Present = false
		}
	}
	outpost := world.outposts[Blue]
	r := world.ballRadius
	pushed := 0
	for _, ball := range world.balls {
		if ball.State == BallFree && pushed < 5 {
			*ball = Ball{State: BallFree, P: [3]float64{0.45 + float64(pushed%2)*0.17, outpost.chuteY + float64(pushed/2-1)*0.12, r}}
			pushed++
		}
	}
	robot.Manual = true
	stations[3].command.Enabled = true
	robot.Heading = math.Pi // facing the wall
	robot.Pos = Vec2{0.45 + 0.4 + robot.half().X, outpost.chuteY}
	before := outpost.Corral
	for step := 0; step < stepHz*4; step++ {
		robot.Ctl = Control{Vel: Vec2{-1.0, 0}}
		world.manualAt[robot.Index] = timeNow()
		world.step(1.0 / stepHz)
	}
	if got := outpost.Corral - before; got < 4 {
		t.Errorf("pushing 5 FUEL into the outpost wall should put most through the floor opening, %d went in", got)
	}
}
