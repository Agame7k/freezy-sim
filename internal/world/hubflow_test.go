package world

import (
	"math"
	"testing"
)

// dropIntoHub drops n FUEL into the blue hub's opening, gap seconds apart, and runs until they are all out (or
// timeout). It returns the transit times to the beam break, how many were counted, and how many came back out.
func dropIntoHub(t *testing.T, n int, gap float64) (transits []float64, counted, out int) {
	world, field, stations := newTestWorld(t)
	for _, station := range stations {
		station.command.Team = 0 // an empty field
	}
	for _, robot := range world.robots {
		robot.Present = false
	}
	hub := world.hubs[Blue]
	field.motor = [2]bool{true, true} // the PLC runs the exits
	var dropped []*Ball
	dropAt := map[*Ball]float64{}
	for _, ball := range world.balls {
		if len(dropped) == n {
			break
		}
		if ball.State == BallFree {
			dropped = append(dropped, ball)
			ball.State = BallOut // parked until dropped
			ball.outAt = world.time + 1e6
		}
	}
	next := 0
	sensed := 0
	seen := map[*Ball]bool{}
	for step := 0; step < stepHz*int(float64(n)*gap+15); step++ {
		if next < len(dropped) && world.time >= float64(next)*gap {
			ball := dropped[next]
			angle := float64(next) * 2.4
			*ball = Ball{State: BallFree, P: [3]float64{hub.Center.X + 0.25*math.Cos(angle), hub.Center.Y + 0.25*math.Sin(angle),
				hub.openingZ + 0.3}, V: [3]float64{0, 0, -2}}
			dropAt[ball] = world.time
			next++
		}
		before := hub.Sensed[0] + hub.Sensed[1] + hub.Sensed[2] + hub.Sensed[3]
		run(world, 1.0/stepHz)
		if after := hub.Sensed[0] + hub.Sensed[1] + hub.Sensed[2] + hub.Sensed[3]; after > before {
			sensed += after - before
		}
		for _, ball := range dropped {
			if ball.State == BallHub && ball.hubPhase == 1 && !seen[ball] {
				seen[ball] = true
				transits = append(transits, world.time-dropAt[ball])
			}
		}
		if next == len(dropped) {
			done := true
			for _, ball := range dropped {
				if ball.State == BallHub || (ball.State == BallFree && dropAt[ball] == 0 && ball.P[2] > 1) {
					done = false
				}
			}
			if done && world.time > float64(n)*gap+1 {
				break
			}
		}
	}
	for _, ball := range dropped {
		if ball.State != BallHub {
			out++
		} else {
			t.Logf("still in the hub at %v (phase %d)", hub.toHub(ball.P), ball.hubPhase)
		}
	}
	return transits, sensed, out
}

func TestHubCountsAndReleasesFuel(t *testing.T) {
	transits, counted, out := dropIntoHub(t, 30, 0.6)
	mean := 0.0
	for _, transit := range transits {
		mean += transit
	}
	mean /= math.Max(1, float64(len(transits)))
	t.Logf("spaced FUEL: %d of 30 counted, %d out, mean time from the opening to the beam %.2f s", counted, out, mean)
	if counted < 29 || out != 30 {
		t.Errorf("spaced-out FUEL should all be counted and leave the hub (counted %d, out %d)", counted, out)
	}
	if math.Abs(mean-sensorTransit) > 0.15 {
		t.Errorf("sensorTransit (%.2f s) should match the measured transit (%.2f s)", sensorTransit, mean)
	}
	// A volley: FUEL arrives together and queues at the exits, but each is still seen by its lane's beam on its own.
	_, counted, out = dropIntoHub(t, 40, 0.05)
	t.Logf("volley: %d of 40 counted, %d out", counted, out)
	if counted != 40 || out != 40 {
		t.Errorf("every FUEL in a volley should be counted once and leave the hub (counted %d, out %d)", counted, out)
	}
}
