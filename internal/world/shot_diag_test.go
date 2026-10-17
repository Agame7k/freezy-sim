package world

import (
	"math"
	"os"
	"testing"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

// TestDiagnoseShots fires volleys from each design's shooting spot and reports where the FUEL ends up
// (FREEZY_DIAG=1).
func TestDiagnoseShots(t *testing.T) {
	if os.Getenv("FREEZY_DIAG") == "" {
		t.Skip("set FREEZY_DIAG=1 to run")
	}
	layout, err := LoadLayout(os.DirFS("../../web"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scorer", "tall", "volley", "bigdumper", "turret"} {
		world := New(layout, bus.New())
		world.quiet = true
		robot := world.robots[3] // blue
		robot.Present, robot.Enabled, robot.Arch, robot.Skill = true, true, archetypeByName(name), 1
		agent := NewAgent(robot, 1)
		hub := world.hubs[Blue]
		spot := agent.shootSpot(robot, world)
		robot.Pos = spot
		away := robot.Pos.Sub(hub.Center)
		robot.Heading = math.Atan2(away.Y, away.X)
		in, shots, misses := 0, 0, map[string]int{}
		for volley := 0; volley < 40; volley++ {
			robot.Hopper = 0
			for _, ball := range world.balls {
				if ball.State == BallFree && robot.Hopper < robot.Arch.Volley {
					ball.State, ball.Owner, ball.intakeT = BallHeld, robot.Index, 1
					robot.Hopper++
				}
			}
			before := world.stats.Entered[Blue]
			if !world.launch(robot, timingNone) {
				t.Logf("%s: no solution from %.2f m", name, spot.Sub(hub.Center).Len())
				break
			}
			fired := robot.Arch.Volley - robot.Hopper
			shots += fired
			var flying []*Ball
			for _, ball := range world.balls {
				if ball.State == BallFree && ball.shooter == robot.Index+1 {
					flying = append(flying, ball)
				}
			}
			for step := 0; step < 360; step++ {
				world.stepBalls(1.0 / stepHz)
				if os.Getenv("FREEZY_NOCOLLIDE") == "" {
					world.collideBalls()
				}
			}
			in += world.stats.Entered[Blue] - before
			for _, ball := range flying {
				if ball.State != BallFree {
					continue
				}
				dx := (ball.P[0] - hub.Center.X) * hub.neutralDir
				switch {
				case dx > hub.size/2:
					misses["long (past the hub)"]++
				case dx < -hub.size/2:
					misses["short (alliance side)"]++
				default:
					misses["side / rim"]++
				}
				ball.shooter = 0
			}
			// Return everything to the floor away from the hub.
			for _, ball := range world.balls {
				if ball.State == BallHub {
					ball.State = BallFree
					ball.P = [3]float64{1, 1, world.ballRadius}
				}
			}
		}
		t.Logf("%-10s from %.2f m (exit z %.2f): %d/%d in (%.0f%%) misses %v", name, spot.Sub(hub.Center).Len(),
			robot.Z+math.Min(robot.Arch.Height, MaxRobotHeight)-0.03, in, shots, 100*float64(in)/math.Max(1, float64(shots)), misses)
	}
}
