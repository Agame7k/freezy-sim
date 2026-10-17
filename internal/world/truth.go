package world

import "math/rand"

// Match truth: what really happened on the field this match, recorded by the simulator itself so it can be checked
// against what the arena scored (app/arenadiff.go). Every FUEL the hub sensors counted (with the FMS match time it was
// counted at), each robot's tower level at the end of AUTO and at the end of the match, the fouls the line referees
// called, and the stations the referees awarded LEVEL 3 for a G420 violation.

// MatchTruth is one match's record.
type MatchTruth struct {
	Seed       int64        `json:"seed"`
	Counts     [2][]float64 `json:"counts"` // match time of every counted FUEL, by alliance
	AutoLevels [6]int       `json:"autoLevels"`
	EndLevels  [6]int       `json:"endLevels"`
	AutoTaken  bool         `json:"autoTaken"`
	EndTaken   bool         `json:"endTaken"`
	Fouls      [2]int       `json:"fouls"` // called against each alliance
	AwardL3    [6]bool      `json:"awardL3"`
	Teams      [6]int       `json:"teams"`
}

// noteCountTruth records a FUEL the sensors counted.
func (world *World) noteCountTruth(alliance int) {
	if world.matchClock == nil {
		return
	}
	if t, inMatch := world.matchClock(); inMatch && t >= 0 {
		world.truth.Counts[alliance] = append(world.truth.Counts[alliance], t)
	}
}

// watchTruth takes the tower snapshots the referees score from (end of AUTO, end of the match).
func (world *World) watchTruth() {
	if world.matchClock == nil {
		return
	}
	t, inMatch := world.matchClock()
	if !inMatch {
		return
	}
	for i, robot := range world.robots {
		world.truth.Teams[i] = robot.Team
	}
	if !world.truth.AutoTaken && t >= AutoSec+0.3 {
		world.truth.AutoTaken = true
		world.truth.AutoLevels = world.towerLevelsLocked()
	}
	if !world.truth.EndTaken && t >= TeleopEndSec+0.3 {
		world.truth.EndTaken = true
		world.truth.EndLevels = world.towerLevelsLocked()
	}
}

func (world *World) towerLevelsLocked() [6]int {
	var levels [6]int
	for i, robot := range world.robots {
		if robot.Present {
			levels[i] = robot.ClimbLevel
		}
	}
	return levels
}

// Truth returns this match's record so far.
func (world *World) Truth() MatchTruth {
	world.mu.Lock()
	defer world.mu.Unlock()
	truth := world.truth
	for alliance := range truth.Counts {
		truth.Counts[alliance] = append([]float64(nil), truth.Counts[alliance]...)
	}
	return truth
}

// ---- Match seeds ---------------------------------------------------------------------------------------------------

// NewMatchSeed starts a new match's randomness: the world's random source and every AI drive team's are re-seeded
// from one number, so a match's AI decisions, natural mistakes and field reset can be played again (as far as the
// arena's real-time timing allows). With a pending replay seed it uses that; otherwise a fresh random one. It returns the
// seed (recorded with the match).
func (world *World) NewMatchSeed() int64 {
	world.mu.Lock()
	defer world.mu.Unlock()
	seed := world.replaySeed
	world.replaySeed = 0
	if seed == 0 {
		seed = rand.Int63n(1<<40) + 1
	}
	world.reseedLocked(seed)
	world.truth = MatchTruth{Seed: seed}
	world.applyPitChangesLocked()
	return seed
}

func (world *World) reseedLocked(seed int64) {
	world.seed = seed
	world.rng = rand.New(rand.NewSource(seed))
	for i, controller := range world.controllers {
		if agent, ok := controller.(*Agent); ok {
			agent.rng = rand.New(rand.NewSource(seed*31 + int64(i+1)))
		}
	}
}

// SetReplaySeed makes the next match use a given seed (0 = random again).
func (world *World) SetReplaySeed(seed int64) {
	world.mu.Lock()
	world.replaySeed = seed
	world.mu.Unlock()
}

// Seed is the current match's seed.
func (world *World) Seed() int64 {
	world.mu.Lock()
	defer world.mu.Unlock()
	return world.seed
}
