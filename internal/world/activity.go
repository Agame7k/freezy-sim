package world

import (
	"math"
	"strings"
)

// Activity tracks how much of a match a robot spent not doing anything useful. A healthy AI robot should never sit
// still unless it is doing a deliberately stationary job or something is wrong with it.
type Activity struct {
	IdleSeconds  float64 `json:"idleSeconds"`  // still, not asked to move, and no reason to be still
	StuckSeconds float64 `json:"stuckSeconds"` // asked to move but not moving (blocked, wedged)
	StillReason  string  `json:"stillReason"`  // why it is still right now, if it is

	idleStreak  float64
	stuckStreak float64
	loggedIdle  bool
}

const (
	stillSpeed     = 0.15 // m/s
	idleGrace      = 1.0  // seconds a robot may pause before it counts as idle
	stuckGrace     = 1.2
	idleLogMinimum = 2.5 // log an idle episode once it lasts this long
)

// stationaryStates are AI states where standing still is the job.
var stationaryStates = []string{"shoot", "pass", "feed", "climb", "deliver", "intake-chute"}

// stillReason explains why a robot may legitimately be stationary ("" if it should be moving).
func (world *World) stillReason(robot *Robot) string {
	switch {
	case !robot.Enabled:
		return "disabled"
	case robot.Brownout:
		return "brownout"
	case robot.Faults.Tipped:
		return "tipped over"
	case world.time < robot.Faults.StuckUntil:
		return "high-centered"
	case robot.Climbing || robot.ClimbLevel > 0:
		return "on the tower"
	case robot.Manual:
		return "manual control"
	case robot.Shooting:
		return "shooting"
	case robot.AiState == "auto:done":
		return "auto routine finished"
	case robot.AiState == "auto:idle":
		return "no auto selected"
	case robot.AiState == "auto:wait":
		return "auto: waiting for a partner"
	}
	state := strings.TrimPrefix(robot.AiState, "auto:")
	for _, s := range stationaryStates {
		if strings.HasPrefix(state, s) {
			return s
		}
	}
	// Held still by an opponent (pinned): the robot is trying but cannot move.
	for _, other := range world.robots {
		if other != robot && other.Present && other.Alliance != robot.Alliance && other.Pos.Sub(robot.Pos).Len() < 1.0 {
			return "held by " + other.Station
		}
	}
	return ""
}

// trackActivity updates a robot's idle/stuck accounting after a physics step.
func (world *World) trackActivity(robot *Robot, dt float64) {
	if diagState != nil && robot.Present && robot.Enabled {
		diagState(robot, dt)
	}
	activity := &robot.Activity
	if !robot.Present || !robot.Enabled || robot.Manual {
		activity.idleStreak, activity.stuckStreak, activity.loggedIdle, activity.StillReason = 0, 0, false, ""
		return
	}
	moving := robot.Vel.Len() > stillSpeed || math.Abs(robot.Omega) > 0.6
	if moving {
		activity.idleStreak, activity.stuckStreak, activity.loggedIdle, activity.StillReason = 0, 0, false, ""
		return
	}
	reason := world.stillReason(robot)
	activity.StillReason = reason
	if reason != "" {
		activity.idleStreak, activity.stuckStreak = 0, 0
		return
	}
	if robot.Ctl.Vel.Len() > 0.4 {
		// Trying to drive but going nowhere.
		activity.idleStreak = 0
		activity.stuckStreak += dt
		if activity.stuckStreak > stuckGrace {
			if activity.StuckSeconds == 0 || activity.stuckStreak-dt <= stuckGrace {
				if diagHook != nil {
					diagHook(world, robot, "stuck")
				}
			}
			activity.StuckSeconds += dt
			activity.StillReason = "blocked"
			if activity.stuckStreak-dt <= 3 && activity.stuckStreak > 3 && !robot.Auto {
				world.noteIncident(robot, "stuck", true) // wedged for a while: the team will avoid this spot
			}
		}
		return
	}
	activity.stuckStreak = 0
	activity.idleStreak += dt
	if activity.idleStreak > idleGrace {
		activity.IdleSeconds += dt
		activity.StillReason = "idle"
		if activity.idleStreak-dt <= idleGrace && diagHook != nil {
			diagHook(world, robot, "idle")
		}
		if !activity.loggedIdle && activity.idleStreak > idleLogMinimum {
			activity.loggedIdle = true
			if world.bus != nil && !world.quiet {
				world.bus.Warnf("ai", "AI idle: %d (%s) standing still in state %q with no reason", robot.Team,
					robot.Station, robot.AiState)
			}
		}
	}
}

// diagHook (tests only) is told about every new stuck or idle episode.
var diagHook func(world *World, robot *Robot, kind string)

// diagState (tests only) is called every step for every enabled robot.
var diagState func(robot *Robot, dt float64)
