package world

import (
	"fmt"
	"math"
)

// The alliance coach watches the match every couple of seconds and adjusts the plan: all-in when behind late, protect a
// lead with defense, cover for a broken teammate, put a defender on the opponent that is doing the damage, add a
// shuttle when FUEL piles up in the neutral zone, and decide who climbs how high in the endgame. Its calls show up in
// the event log and the AI panel.

const coachEvery = 2.0

// CoachCall is one decision the coach made.
type CoachCall struct {
	Time     float64 `json:"time"` // match time
	Alliance int     `json:"alliance"`
	Text     string  `json:"text"`
}

type coach struct {
	alliance  int
	nextAt    float64
	calls     []CoachCall
	situation map[string]string // last call per topic, so a situation is announced once
	climbCall map[int]int       // last endgame decision per robot (level, -1 = keep scoring)
	threat    int               // opponent being marked (-1 none)
	shuttle   int               // robot told to shuttle (-1 none)
}

func newCoach(alliance int) *coach {
	return &coach{alliance: alliance, situation: map[string]string{}, climbCall: map[int]int{}, threat: -1, shuttle: -1}
}

func (c *coach) reset() {
	c.nextAt, c.calls, c.situation, c.climbCall, c.threat, c.shuttle = 0, nil, map[string]string{}, map[int]int{}, -1, -1
}

// runCoaches is called with the world locked after the drive teams have decided.
func (world *World) runCoaches() {
	for _, c := range world.coaches {
		if world.time >= c.nextAt {
			c.nextAt = world.time + coachEvery
			if world.practiceMatch() {
				c.nextAt += coachEvery * 3 // practice: each team works on its own game; the coaches mostly watch
			}
			c.evaluate(world)
		}
	}
}

// members are the alliance's AI-driven robots.
func (c *coach) members(world *World) ([]*Robot, []*Agent) {
	var robots []*Robot
	var agents []*Agent
	for i, robot := range world.robots {
		if robot.Present && robot.Alliance == c.alliance && !robot.Manual {
			if agent, ok := world.controllers[i].(*Agent); ok {
				robots = append(robots, robot)
				agents = append(agents, agent)
			}
		}
	}
	return robots, agents
}

// announce logs a coach call once per decision: key identifies the decision for a topic ("" = the situation is over),
// so a call is not repeated while only the numbers in it change.
func (c *coach) announce(world *World, topic, key, text string, t float64) {
	if c.situation[topic] == key {
		return
	}
	c.situation[topic] = key
	if key == "" {
		return
	}
	name := []string{"Red", "Blue"}[c.alliance]
	c.calls = append(c.calls, CoachCall{Time: math.Round(t*10) / 10, Alliance: c.alliance, Text: text})
	if len(c.calls) > 30 {
		c.calls = c.calls[len(c.calls)-30:]
	}
	world.logf("info", "coach", "%s coach: %s", name, text)
}

func (c *coach) evaluate(world *World) {
	robots, agents := c.members(world)
	if len(robots) == 0 || !robots[0].Enabled || robots[0].Auto {
		return
	}
	t, _ := agents[0].clock.matchTime(world.time)
	if t < TeleopStartSec {
		return
	}
	remaining := TeleopEndSec - t
	margin := world.scoreboard[c.alliance] - world.scoreboard[1-c.alliance]
	for _, agent := range agents {
		agent.coachBias = map[string]float64{}
		agent.markIndex = -1
	}

	// Who is out of action? (A moment's traffic jam is not; being stuck for seconds is.)
	broken := map[int]bool{}
	var brokenTeams []int
	for i, robot := range robots {
		if robot.Faults.Tipped || !robot.Enabled || (robot.Faults.SpeedScale > 0 && robot.Faults.SpeedScale < 0.7) ||
			world.time < robot.Faults.ShooterJamUntil || world.time < robot.Faults.StuckUntil ||
			robot.Activity.stuckStreak > 4 {
			broken[i] = true
			brokenTeams = append(brokenTeams, robot.Team)
		}
	}
	// The healthy robot that scores best carries the load.
	bestScorer, bestScored := -1, -1
	for i, robot := range robots {
		if !broken[i] && robot.Scored > bestScored {
			bestScorer, bestScored = i, robot.Scored
		}
	}
	coverKey := ""
	for i, robot := range robots {
		if !broken[i] {
			continue
		}
		plan := world.plans[robot.Index]
		if plan.Strategy == StratScorer && bestScorer >= 0 && world.plans[robots[bestScorer].Index].Strategy != StratScorer {
			agents[bestScorer].coachBias[JobScoring] = 1.6
			coverKey = fmt.Sprintf("%d>%d", robot.Team, robots[bestScorer].Team)
			c.announce(world, "cover", coverKey, fmt.Sprintf("%d is out of action; %d takes over scoring", robot.Team,
				robots[bestScorer].Team), t)
		} else {
			coverKey = fmt.Sprint(brokenTeams)
			c.announce(world, "cover", coverKey, fmt.Sprintf("%d is out of action; playing on without it", robot.Team), t)
		}
	}
	if coverKey == "" {
		c.announce(world, "cover", "", "", t)
	}

	switch {
	case remaining < 45 && margin < -15:
		// Behind late: everybody scores.
		for _, agent := range agents {
			agent.coachBias[JobScoring] = 1.6
			agent.coachBias[JobDefend] = 0.3
			agent.coachBias[JobShuttle] = 0.6
		}
		c.announce(world, "plan", "all-in", fmt.Sprintf("down %d with %.0f s left — all-in on scoring", -margin, remaining), t)
	case margin > 25 && remaining > 30:
		// Protect the lead: the most aggressive robot goes to defense when the opponents' hub is lit.
		pick, aggression := -1, -1.0
		for i, agent := range agents {
			if !broken[i] && i != bestScorer && agent.Personality.Aggression > aggression {
				pick, aggression = i, agent.Personality.Aggression
			}
		}
		if pick >= 0 {
			agents[pick].coachBias[JobDefend] = 1.8
			c.announce(world, "plan", fmt.Sprintf("protect:%d", robots[pick].Team),
				fmt.Sprintf("up %d — %d protects the lead on defense", margin, robots[pick].Team), t)
		}
	default:
		c.announce(world, "plan", "", "", t)
	}

	// Mark the opponent doing the damage (switching only when another clearly overtakes it).
	var threat *Robot
	for _, other := range world.robots {
		if other.Present && other.Alliance != c.alliance && other.Enabled &&
			(threat == nil || other.Scored > threat.Scored) {
			threat = other
		}
	}
	if previous := c.threat; previous >= 0 && threat != nil && previous != threat.Index {
		if old := world.robots[previous]; old.Present && old.Enabled && threat.Scored < old.Scored+10 {
			threat = old
		}
	}
	if threat != nil && threat.Scored >= 25 {
		c.threat = threat.Index
		for i, robot := range robots {
			if world.plans[robot.Index].Strategy == StratDefense || agents[i].coachBias[JobDefend] > 1 {
				agents[i].markIndex = threat.Index
				c.announce(world, fmt.Sprintf("mark%d", robot.Index), fmt.Sprintf("%d", threat.Team),
					fmt.Sprintf("%d, shadow %d (%d scored)", robot.Team, threat.Team, threat.Scored), t)
			}
		}
	}

	// FUEL piling up in the neutral zone with no one bringing it home: one robot (kept the same while it can) shuttles.
	neutral := 0
	world.FreeBalls(func(p Vec2, z float64) {
		hub := world.hubs[c.alliance]
		opp := world.hubs[1-c.alliance]
		if (p.X-hub.Center.X)*hub.neutralDir > 0.6 && (p.X-opp.Center.X)*hub.neutralDir < -0.6 {
			neutral++
		}
	})
	hasShuttle := false
	for _, robot := range robots {
		if s := world.plans[robot.Index].Strategy; s == StratShuttle || s == StratReverse {
			hasShuttle = true
		}
	}
	if neutral > 70 && !hasShuttle {
		pick := -1
		for i, robot := range robots {
			if robot.Index == c.shuttle && !broken[i] {
				pick = i
			}
		}
		if pick < 0 {
			emptiest := math.Inf(1)
			for i, robot := range robots {
				if !broken[i] && float64(robot.Hopper) < emptiest {
					pick, emptiest = i, float64(robot.Hopper)
				}
			}
		}
		if pick >= 0 {
			c.shuttle = robots[pick].Index
			agents[pick].coachBias[JobShuttle] = 2.5
			c.announce(world, "shuttle", fmt.Sprintf("%d", robots[pick].Team),
				fmt.Sprintf("FUEL is piling up in the neutral zone (%d) — %d shuttles it home", neutral, robots[pick].Team), t)
		}
	} else if neutral < 40 {
		c.shuttle = -1
		c.announce(world, "shuttle", "", "", t)
	}

	// Endgame: climb or keep scoring, for the whole alliance.
	c.planClimbs(world, robots, agents, remaining, t)
}

// Calls returns the coaches' recent decisions (newest last), for the AI panel.
func (world *World) coachCalls() [2][]CoachCall {
	var calls [2][]CoachCall
	for i, c := range world.coaches {
		calls[i] = append([]CoachCall(nil), c.calls...)
	}
	return calls
}
