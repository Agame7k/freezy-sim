package world

import (
	"fmt"
	"math"
)

// Between matches every team reviews its AUTO the way real programmers do in the pits: did each path end where it
// should, did the shots go in, did it run out of time, did it hit a partner? Then it changes one thing: recalibrate the
// wheels, slow a path that slipped, add a settle pause before shooting, delay the start to let a partner through,
// speed up a routine that ran out of time. Routines get better over an event; the notes say what changed and why.

const (
	autoTuneMissTol  = 0.35 // metres: a path that ends further off than this needs work
	autoNotesKept    = 6
	autoMaxDelay     = 4.5
	autoMinPathSpeed = 1.0
)

// autoResult is a robot's AUTO for the match summary (nil if it never ran a routine).
func (agent *Agent) autoResult(robot *Robot, world *World) *AutoResult {
	if agent.auto == nil {
		return nil
	}
	agent.finishAuto(robot, world)
	result := agent.auto.result
	result.Fuel = robot.AutoScored
	result.Steps = append([]AutoStepResult(nil), result.Steps...)
	return &result
}

// value is what a routine produced, in points: FUEL and tower points now, plus FUEL lobbed home (worth about half a
// point each: a teammate still has to score it in teleop).
func (result AutoResult) value() float64 {
	return float64(result.Fuel+result.Tower) + 0.5*float64(result.Passes)
}

// reviewAuto learns from one robot's AUTO.
func (world *World) reviewAuto(team int, result AutoResult) {
	stats := world.teamStats(team)
	points := result.value()
	stats.AutoRuns++
	stats.AutoPoints += (points - stats.AutoPoints) / float64(min(stats.AutoRuns, 8))
	if result.Mistake != "" {
		stats.AutoMistakes++
		world.logf("info", "ai", "%d adds a pre-match checklist after a chooser mistake (%s)", team, result.Mistake)
	}
	program := world.learning.Autos[team][result.Key]
	if program == nil {
		return
	}
	program.Runs++
	program.AvgPoints = round2(program.AvgPoints + (points-program.AvgPoints)/float64(min(program.Runs, 5)))
	if result.Mistake != "" || program.Revision != result.Revision {
		return // the routine was not what went wrong (or it already changed since)
	}
	if note := world.tuneAuto(team, program, result, stats); note != "" {
		program.Revision++
		program.Notes = append(program.Notes, fmt.Sprintf("r%d: %s", program.Revision, note))
		if len(program.Notes) > autoNotesKept {
			program.Notes = program.Notes[len(program.Notes)-autoNotesKept:]
		}
		world.logf("info", "ai", "%d tuned %s: %s", team, program.Name, note)
	}
	world.markDirtyLocked()
}

// tuneAuto picks one change to make to a routine (or none), applies it and describes it.
func (world *World) tuneAuto(team int, program *AutoProgram, result AutoResult, stats *TeamStats) string {
	personality := world.personalityFor(team)
	// Not every team works on its auto after every match.
	if world.rng.Float64() > 0.45+0.45*personality.Discipline {
		return ""
	}
	// Step results are in program order, one per step run, so result k is program step k.
	// 1. Hit a partner on a path: give the partner room at the start.
	for _, step := range result.Steps {
		if step.Kind == StepPath && step.Contact == "partner" && step.EndError > autoTuneMissTol && program.Delay < autoMaxDelay {
			program.Delay = math.Min(autoMaxDelay, program.Delay+1)
			return fmt.Sprintf("hit a partner during %q, now waits %.1f s at the start", step.Label, program.Delay)
		}
	}
	// 2. A path ended well off its end without being hit: wheel slip and calibration.
	for k, step := range result.Steps {
		if step.Kind != StepPath || step.Contact != "" || step.EndError <= autoTuneMissTol {
			continue
		}
		if !result.Vision && stats.OdoCal < 0.6 {
			stats.OdoCal = math.Min(0.9, stats.OdoCal+0.3)
			return fmt.Sprintf("ended %.2f m off after %q: recalibrated the wheel diameter", step.EndError, step.Label)
		}
		index := k
		if index < len(program.Steps) && program.Steps[index].Kind == StepPath {
			path := &program.Steps[index]
			if path.Speed > autoMinPathSpeed {
				path.Speed = round2(math.Max(autoMinPathSpeed, path.Speed*0.85))
				path.Accel = round2(path.Accel * 0.85)
				return fmt.Sprintf("ended %.2f m off after %q: slowed it to %.1f m/s", step.EndError, step.Label, path.Speed)
			}
		}
	}
	// 3. Shots went out but few went in: the pose estimate (or a moving robot) spoiled the aim.
	fired := 0
	for _, step := range result.Steps {
		if step.Kind == StepShoot {
			fired += step.Fired
		}
	}
	if fired >= 4 && float64(result.Fuel) < 0.5*float64(fired) {
		if !result.Vision && stats.OdoCal < 0.9 {
			stats.OdoCal = math.Min(0.9, stats.OdoCal+0.25)
			return fmt.Sprintf("only %d of %d auto shots went in: re-zeroed odometry and recalibrated", result.Fuel, fired)
		}
		for index, step := range program.Steps {
			if step.Kind == StepShoot && (index == 0 || program.Steps[index-1].Kind != StepWait) {
				program.Steps = append(program.Steps[:index], append([]AutoStep{{Kind: StepWait, Label: "settle",
					Seconds: 0.3}}, program.Steps[index:]...)...)
				return fmt.Sprintf("only %d of %d auto shots went in: added a settle pause before shooting", result.Fuel, fired)
			}
		}
	}
	// 4. The tower climb did not catch the rung: line up more slowly.
	for k, step := range result.Steps {
		if step.Kind == StepClimb && !step.Completed && k > 0 {
			index := k - 1
			if index < len(program.Steps) && program.Steps[index].Kind == StepPath && program.Steps[index].Speed > 0.5 {
				path := &program.Steps[index]
				path.Speed = round2(math.Max(0.5, path.Speed*0.75))
				return fmt.Sprintf("missed the rung: slowed the line-up to %.1f m/s", path.Speed)
			}
		}
	}
	// 5. Ran out of AUTO with steps left and the paths were fine: speed the paths up.
	if len(result.Steps) < len(program.Steps) || (len(result.Steps) > 0 && !result.Steps[len(result.Steps)-1].Completed) {
		clean := true
		for _, step := range result.Steps {
			if step.Kind == StepPath && step.EndError > autoTuneMissTol {
				clean = false
			}
		}
		if clean {
			robotSpeed := archetypeByName(program.Design).MaxSpeed
			changed := false
			for index := range program.Steps {
				path := &program.Steps[index]
				if path.Kind == StepPath && !path.Intake && path.Speed < robotSpeed*0.95 {
					path.Speed = round2(math.Min(robotSpeed*0.95, path.Speed*1.1))
					changed = true
				}
			}
			if program.Delay > 0 {
				program.Delay = math.Max(0, program.Delay-0.5)
				changed = true
			}
			if changed {
				return fmt.Sprintf("ran out of time at step %d of %d: sped up the paths", len(result.Steps), len(program.Steps))
			}
		}
	}
	return ""
}
