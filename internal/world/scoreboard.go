package world

// The drive teams' view of the score (what a coach reads off the audience display), and the end of a real match so
// teams can learn from it.

// SetMatchClock gives the world the FMS's match time (seconds since the start of auto, and whether a match is running).
func (world *World) SetMatchClock(clock func() (float64, bool)) {
	world.mu.Lock()
	world.matchClock = clock
	world.mu.Unlock()
}

// allianceWonAuto reads the game data any of the alliance's robots received.
func (world *World) allianceWonAuto(alliance int) (bool, bool) {
	for _, robot := range world.robots {
		if robot.Present && robot.Alliance == alliance {
			if won, known := wonAuto(robot); known {
				return won, true
			}
		}
	}
	return false, false
}

// recordCount adds a counted FUEL to the scoreboard if it scored.
func (world *World) recordCount(alliance int) {
	world.noteCountTruth(alliance)
	if world.matchClock != nil {
		t, inMatch := world.matchClock()
		if !inMatch {
			return
		}
		won, _ := world.allianceWonAuto(alliance)
		if FuelScores(t+countLatency, won) {
			world.scoreboard[alliance]++
		}
		return
	}
	// Without the FMS clock, the hub light is the best guide.
	if world.hubs[alliance].LightOn {
		world.scoreboard[alliance]++
	}
}

// watchMatchEnd notices a real match finishing (teleop running down to zero and the robots disabled) and learns from
// it. Aborted matches teach nothing. The AI benchmark reports its matches itself.
func (world *World) watchMatchEnd() {
	if world.quiet {
		return
	}
	enabled, teleop, remaining := false, false, 0
	for _, robot := range world.robots {
		if robot.Present {
			if robot.Enabled {
				enabled = true
				teleop = teleop || !robot.Auto
			}
			remaining = robot.Remaining
		}
	}
	if enabled && teleop {
		world.teleopSeen = true
		world.lastRemaining = remaining
		return
	}
	if world.teleopSeen && !enabled {
		world.teleopSeen = false
		if world.lastRemaining <= 1 {
			world.learnFromLocked(world.liveMatchSummary())
		}
	}
}

// liveMatchSummary describes the match that just ended, from what the world saw.
func (world *World) liveMatchSummary() BenchMatch {
	var match BenchMatch
	for alliance := 0; alliance < 2; alliance++ {
		match.Alliances[alliance].Plan = world.planNames[alliance]
		match.Mixes[alliance] = world.allianceMix(alliance)
		score := world.scoreboard[alliance]
		for _, robot := range world.robots {
			if robot.Present && robot.Alliance == alliance {
				score += TowerPoints(robot.ClimbLevel, false)
			}
		}
		match.Alliances[alliance].Score = score
	}
	for i, robot := range world.robots {
		if !robot.Present {
			continue
		}
		entry := BenchRobot{Station: robot.Station, Team: robot.Team, Archetype: robot.Arch.Name, Shots: robot.Shots,
			Scored: robot.Scored, ClimbLevel: robot.ClimbLevel}
		if agent, ok := world.controllers[i].(*Agent); ok {
			entry.ClimbAttempted = agent.climbGoal > 0
			entry.EarlyShots, entry.EarlyIn, entry.EarlyScored = agent.shotStats.Early, agent.shotStats.EarlyIn, agent.shotStats.EarlyScored
			entry.LateShots, entry.LateIn, entry.LateScored = agent.shotStats.Late, agent.shotStats.LateIn, agent.shotStats.LateScored
			entry.Auto = agent.autoResult(robot, world)
			entry.ClimbGoal, entry.Choices = agent.climbGoal, agent.choices
			entry.Shots, entry.Scored = robot.Shots, robot.Scored
		}
		match.Robots = append(match.Robots, entry)
	}
	return match
}
