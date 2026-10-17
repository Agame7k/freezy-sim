package world

import (
	"math"
	"sync"
)

// Shot timing around hub shift changes.
//
// The arena counts a FUEL when the hub's sensors see it, not when it leaves the robot: a shot is in the air for about
// a second, takes sensorTransit inside the hub, and the count reaches the arena a little later. Active periods also keep
// counting for GraceSec after they end. A drive team that knows this can start shooting just before its hub turns on
// ("early shots") and keep emptying through the end of an active period ("late shots") — with some risk, because the
// FMS only tells robots the match time in whole seconds, every 500 ms.

// ShotStats count a robot's shots timed around shift changes. "In" means the ball entered the hub; "Scored" means it
// was counted while the hub was scoring.
type ShotStats struct {
	Early       int `json:"early"`
	EarlyIn     int `json:"earlyIn"`
	EarlyScored int `json:"earlyScored"`
	Late        int `json:"late"`
	LateIn      int `json:"lateIn"`
	LateScored  int `json:"lateScored"`
}

// Shot timing tags carried by a ball.
const (
	timingNone = iota
	timingEarly
	timingLate
)

const countLatency = 0.08 // hub sensor → arena count (PLC/alt-IO polling), seconds

// matchClock is a drive team's estimate of the true match time, built from the whole-second countdown in the FMS
// control packets. The arena sends ceil(remaining) every 500 ms (and at once on a period change), so the moment the
// reported value ticks down is the moment the true remaining time crossed that whole second, seen up to 0.5 s late.
type matchClock struct {
	reported  int
	auto      bool
	enabled   bool
	anchorAt  float64 // world time of the anchor
	anchorRem float64 // true remaining seconds estimated at the anchor
	spread    float64 // ± uncertainty of the anchor
	valid     bool
}

func (clock *matchClock) update(robot *Robot, now float64) {
	periodChanged := robot.Auto != clock.auto || robot.Enabled != clock.enabled
	if !clock.valid || periodChanged {
		// Period changes are sent immediately, so the reported value is the true remaining time right now.
		clock.anchorAt, clock.anchorRem, clock.spread, clock.valid = now, float64(robot.Remaining), 0.05, true
	} else if robot.Remaining != clock.reported {
		if robot.Remaining == clock.reported-1 {
			// A clean tick: the true remaining time crossed this whole second within the last packet period.
			clock.anchorAt, clock.anchorRem, clock.spread = now, float64(robot.Remaining)-0.25, 0.28
		} else {
			clock.anchorAt, clock.anchorRem, clock.spread = now, float64(robot.Remaining)-0.5, 0.5
		}
	}
	clock.reported, clock.auto, clock.enabled = robot.Remaining, robot.Auto, robot.Enabled
}

// matchTime is the estimated match time (seconds since the start of auto) and its uncertainty.
func (clock *matchClock) matchTime(now float64) (float64, float64) {
	remaining := clock.anchorRem - (now - clock.anchorAt)
	// Between ticks the estimate can never drop below the next whole second's lower bound.
	remaining = math.Max(remaining, float64(clock.reported)-1)
	if clock.auto {
		return AutoSec - remaining, clock.spread
	}
	return TeleopEndSec - remaining, clock.spread
}

// wonAuto reads the game data: did this robot's alliance win auto (and therefore sit out shifts 1 and 3)?
func wonAuto(robot *Robot) (bool, bool) {
	switch robot.GameData {
	case "R":
		return robot.Alliance == Red, true
	case "B":
		return robot.Alliance == Blue, true
	}
	return false, false
}

// flight times, cached by (distance, height) since solving the trajectory is expensive.
var (
	flightMu    sync.Mutex
	flightCache = map[[2]int]float64{}
)

// shotFlightTime is how long a FUEL launched from height exitZ takes to drop into the hub from a horizontal distance.
func shotFlightTime(distance, dz float64) float64 {
	key := [2]int{int(math.Round(distance / 0.1)), int(math.Round(dz / 0.05))}
	flightMu.Lock()
	cached, ok := flightCache[key]
	flightMu.Unlock()
	if ok {
		return cached
	}
	d := float64(key[0]) * 0.1
	h := float64(key[1]) * 0.05
	flight := 1.0
	if speed, angle, ok := solveLaunch(d, h); ok {
		vx, vz := speed*math.Cos(angle), speed*math.Sin(angle)
		x, z := 0.0, 0.0
		const step = 0.004
		for t := 0.0; t < 4; t += step {
			v := math.Hypot(vx, vz)
			vx -= dragK * v * vx * step
			vz -= (gravity + dragK*v*vz) * step
			x += vx * step
			z += vz * step
			if x >= d {
				flight = t
				break
			}
		}
	}
	flightMu.Lock()
	flightCache[key] = flight
	flightMu.Unlock()
	return flight
}

// timeToCount estimates how long after the trigger is pulled a shot from here would be counted by the arena.
func (agent *Agent) timeToCount(robot *Robot, world *World) float64 {
	hub := world.hubs[robot.Alliance]
	exit, exitZ := robot.shooterExit()
	flight := shotFlightTime(hub.aimPoint().Sub(exit).Len(), world.hubOpeningZ()+0.12-exitZ)
	return agent.reaction + flight + sensorTransit + countLatency
}

// shotTiming decides whether a shot fired now would score, judged with this drive team's own clock and nerve.
// It returns whether to fire and the timing tag for the shot.
func (agent *Agent) shotTiming(robot *Robot, world *World) (bool, int) {
	if robot.Auto {
		return true, timingNone // auto counts until GraceSec after it ends
	}
	won, known := agent.readGameData(robot, world)
	t, spread := agent.clock.matchTime(world.time)
	countAt := t + agent.timeToCount(robot, world)
	if !known {
		// Before game data arrives (the transition shift is active for everyone) just use the light.
		return world.hubs[robot.Alliance].LightOn || agent.lightRecently(world), timingNone
	}
	// The whole uncertainty band must land in a scoring window; daring drive teams shave the safety margin.
	margin := (spread + 0.12) * (1.15 - agent.Personality.Risk)
	if margin < 0.05 {
		margin = 0.05
	}
	early, late := FuelScores(countAt-margin, won), FuelScores(countAt+margin, won)
	if !early || !late {
		return false, timingNone
	}
	activeNow := ActiveAt(t, won)
	switch {
	case !activeNow:
		// The hub is not lit yet (or any more): this ball lands just after it turns on, or inside the grace window.
		if ActiveAt(countAt, won) {
			return true, timingEarly
		}
		return true, timingLate
	case !ActiveAt(countAt, won):
		return true, timingLate // fired while lit, counted in the grace window
	}
	return true, timingNone
}

// lightRecently reports whether this team's hub light was on within the last moment (it blinks near the end of an
// active period).
func (agent *Agent) lightRecently(world *World) bool {
	return world.time-agent.lastLight < 0.7
}

// secondsToActive is this team's estimate of how long until its hub starts scoring again (0 if it scores now), and
// secondsActiveLeft how long until it stops scoring (counting the grace window), using the drive team's clock.
func (agent *Agent) secondsToActive(robot *Robot, world *World) float64 {
	won, known := agent.readGameData(robot, world)
	if robot.Auto || !known {
		return 0
	}
	t, _ := agent.clock.matchTime(world.time)
	if ActiveAt(t, won) {
		return 0
	}
	for probe := t; probe < TeleopEndSec; probe += 0.1 {
		if ActiveAt(probe, won) {
			return probe - t
		}
	}
	return math.Inf(1)
}

func (agent *Agent) secondsActiveLeft(robot *Robot, world *World) float64 {
	won, known := agent.readGameData(robot, world)
	t, _ := agent.clock.matchTime(world.time)
	if robot.Auto {
		return AutoSec - t
	}
	if !known {
		return TeleopStartSec + TransitionSec - t
	}
	if !ActiveAt(t, won) {
		return 0
	}
	probe := t
	for probe < TeleopEndSec && ActiveAt(probe+0.1, won) {
		probe += 0.1
	}
	return probe - t
}

// recordTimedCount credits an early/late shot that reached the hub sensors to its shooter's statistics.
func (world *World) recordTimedCount(ball *Ball) {
	if ball.timing == timingNone || ball.shooter <= 0 || ball.shooter > len(world.robots) {
		return
	}
	agent, ok := world.controllers[ball.shooter-1].(*Agent)
	if !ok || world.matchClock == nil {
		return
	}
	t, inMatch := world.matchClock()
	if !inMatch {
		return
	}
	robot := world.robots[ball.shooter-1]
	won, _ := wonAuto(robot)
	scored := FuelScores(t+countLatency, won)
	if ball.timing == timingEarly {
		agent.shotStats.EarlyIn++
		if scored {
			agent.shotStats.EarlyScored++
		}
	} else {
		agent.shotStats.LateIn++
		if scored {
			agent.shotStats.LateScored++
		}
	}
}
