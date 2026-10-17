package world

import (
	"fmt"
	"math"
)

// Endgame: climb, or keep scoring FUEL?
//
// Climbing takes a robot out of the scoring for the drive to the tower and the climb itself, and a climb can fall short
// of the level it aims for. Each drive team prices its own options — every tower level it can reach in the time left,
// against scoring FUEL right to the buzzer (balls still in the air count for a few seconds after it) — from what it
// has actually been scoring this match, what is in its hopper and how much FUEL it can see. The alliance's coach then
// picks the best combination for the three robots, which can also earn bonus ranking points in qualification matches:
// Traversal (50 tower points), Energized (100 FUEL) and Supercharged (360 FUEL), as Freezy Arena scores them.

// Bonus ranking point thresholds (Freezy Arena game/score.go).
const (
	EnergizedFuel     = 100
	SuperchargedFuel  = 360
	TraversalTower    = 50
	rankingPointWorth = 20.0 // how many match points a bonus ranking point is worth to a drive team in quals
	climbDecideAt     = 45.0 // seconds before the end the alliance starts weighing its endgame
)

// ClimbOption is one endgame choice for one robot, in expected points.
type ClimbOption struct {
	Level       int     `json:"level"` // 0 = no climb: score FUEL to the buzzer
	TowerPoints float64 `json:"towerPoints"`
	FuelPoints  float64 `json:"fuelPoints"`
	Fuel        float64 `json:"fuel"` // FUEL counted (for the FUEL bonus ranking points)
	Feasible    bool    `json:"feasible"`
}

// Total is the option's expected match points.
func (option ClimbOption) Total() float64 { return option.TowerPoints + option.FuelPoints }

// fuelRate estimates how many FUEL per second this robot scores while its hub is lit: what it has really been doing
// this match, blended with what its design should manage given the FUEL it can see.
func (agent *Agent) fuelRate(robot *Robot, world *World) float64 {
	arch := robot.Arch
	accuracy := 0.65
	if robot.Shots >= 15 {
		accuracy = clamp(float64(robot.Scored)/float64(robot.Shots), 0.2, 1)
	}
	known := 0
	agent.knownBalls(robot, world, func(p Vec2, weight int) {
		if agent.inArea(robot, world, "play", p) {
			known += weight
		}
	})
	availability := clamp(float64(known)/80, 0.25, 1)
	intakeRate := 2.2 * (arch.IntakeWidth / 0.62) * availability * math.Min(1.1, robot.Skill)
	load := float64(arch.Capacity) * 0.8
	cycle := load/arch.ShootRate + load/intakeRate + 3.5 // shoot, collect, drive between
	prior := load * accuracy / cycle
	scored := float64(robot.Scored - agent.teleopScoredBase)
	if agent.activeTime < 15 || scored <= 0 {
		return prior
	}
	observed := scored / agent.activeTime
	return 0.6*observed + 0.4*prior
}

// climbOptions prices every endgame choice for this robot, with remaining seconds of teleop left.
func (agent *Agent) climbOptions(robot *Robot, world *World, remaining float64) []ClimbOption {
	rate := agent.fuelRate(robot, world)
	accuracy := 0.65
	if robot.Shots >= 15 {
		accuracy = clamp(float64(robot.Scored)/float64(robot.Shots), 0.2, 1)
	}
	// FUEL scored in t seconds of shooting/cycling: the hopper first (fast), then cycles.
	fuelIn := func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		hopperTime := float64(robot.Hopper)/robot.Arch.ShootRate + agent.travelTime(robot, agent.shootSpot(robot, world))
		if t <= hopperTime {
			return float64(robot.Hopper) * accuracy * t / hopperTime
		}
		return float64(robot.Hopper)*accuracy + rate*(t-hopperTime)
	}
	maxLevel := robot.Arch.ClimbLevel
	if robot.Faults.ClimbFail {
		maxLevel = min(maxLevel, 1)
	}
	// Buzzer beaters: shots in the air at the end still count for the grace period.
	shootTime := remaining + GraceSec - agent.timeToCount(robot, world)
	options := []ClimbOption{{Level: 0, Feasible: true, Fuel: fuelIn(shootTime), FuelPoints: fuelIn(shootTime)}}
	zone, _ := world.climbZone(robot.Alliance, robot.Index%3-1)
	reliability := agent.Reliability * math.Min(1.2, robot.Skill)
	// Careful teams leave more time to spare.
	spare := 1.0 + (1-agent.Personality.Risk)*1.5
	for level := 1; level <= maxLevel; level++ {
		travel := agent.travelTime(robot, zone) + 0.8
		climb := robot.Arch.ClimbTime * float64(level)
		fuelTime := remaining - travel - climb - spare
		option := ClimbOption{Level: level, Feasible: fuelTime > -spare*0.5}
		// What this team's climbs to this level have actually done (a level that keeps failing gets scaled back).
		reliability := world.climbReliability(robot.Team, level, reliability)
		// A climb that falls short usually ends a level or two lower.
		short := float64(TowerPoints(max(0, level-1), false)+TowerPoints(max(0, level-2), false)) / 2
		option.TowerPoints = reliability*float64(TowerPoints(level, false)) + (1-reliability)*short
		option.Fuel = fuelIn(math.Max(0, fuelTime))
		option.FuelPoints = option.Fuel
		options = append(options, option)
	}
	return options
}

// planClimbs is the coach's endgame decision: the combination of choices for its robots with the most expected
// points (plus bonus ranking points in qualification matches). Robots already committed to a climb keep it.
func (c *coach) planClimbs(world *World, robots []*Robot, agents []*Agent, remaining, t float64) {
	if remaining > climbDecideAt {
		return
	}
	type choices struct {
		options []ClimbOption
		fixed   int // -1 = free to choose
	}
	all := make([]choices, len(robots))
	for i, robot := range robots {
		agent := agents[i]
		all[i].fixed = -1
		switch {
		case robot.Climbing || robot.ClimbLevel > 0 || agent.climbGoal > 0:
			all[i].fixed = max(agent.climbGoal, robot.ClimbLevel) // committed
		case robot.Faults.Tipped || !robot.Enabled:
			all[i].fixed = 0
		}
		all[i].options = agent.climbOptions(robot, world, remaining)
	}
	// Bonus ranking points only matter in qualification (and practice/test) matches.
	rpWorth := rankingPointWorth
	if robots[0].MatchType == 3 {
		rpWorth = 0
	}
	autoTower := 0
	for _, robot := range robots {
		autoTower += robot.AutoTower
	}
	fuelSoFar := float64(world.scoreboard[c.alliance])
	bestValue := math.Inf(-1)
	best := make([]int, len(robots))
	pick := make([]int, len(robots))
	var search func(i int)
	search = func(i int) {
		if i == len(robots) {
			points, tower, fuel := 0.0, float64(autoTower), fuelSoFar
			for k, choice := range pick {
				option := all[k].options[choice]
				points += option.Total()
				tower += option.TowerPoints
				fuel += option.Fuel
			}
			value := points
			if rpWorth > 0 {
				value += rpWorth * (reachChance(tower, TraversalTower, 8) + reachChance(fuel, EnergizedFuel, 0.12*fuel+6) +
					reachChance(fuel, SuperchargedFuel, 0.12*fuel+6))
			}
			if value > bestValue {
				bestValue = value
				copy(best, pick)
			}
			return
		}
		for choice, option := range all[i].options {
			if all[i].fixed >= 0 {
				if choice != min(all[i].fixed, len(all[i].options)-1) {
					continue
				}
			} else if !option.Feasible && choice != 0 {
				continue
			}
			pick[i] = choice
			search(i + 1)
		}
	}
	search(0)
	for i, robot := range robots {
		agent := agents[i]
		choice := all[i].options[best[i]]
		agent.climbOptionsSeen = all[i].options
		if agent.climbGoal > 0 || robot.Climbing || robot.ClimbLevel > 0 {
			continue
		}
		level := choice.Level
		decision := level
		if level == 0 {
			decision = -1
		}
		previous, decided := c.climbCall[robot.Index]
		c.climbCall[robot.Index] = decision
		announce := !decided || previous != decision
		if level == 0 {
			agent.climbPlan = -1
			stay := all[i].options[0]
			var bestClimb ClimbOption
			for _, option := range all[i].options[1:] {
				if option.Feasible && option.Total() > bestClimb.Total() {
					bestClimb = option
				}
			}
			reason := "no time left to climb"
			if bestClimb.Level > 0 {
				reason = fmt.Sprintf("~%.0f points shooting vs ~%.0f climbing to level %d", stay.Total(), bestClimb.Total(),
					bestClimb.Level)
			}
			if announce {
				c.announce(world, fmt.Sprintf("climb%d", robot.Index), fmt.Sprint(decision), fmt.Sprintf("%d keeps scoring FUEL instead of climbing (%s)",
					robot.Team, reason), t)
			}
			continue
		}
		agent.climbPlan = level
		if announce {
			why := ""
			if choice.Total() < all[i].options[0].Total() {
				why = " — for the Traversal ranking point"
			}
			c.announce(world, fmt.Sprintf("climb%d", robot.Index), fmt.Sprint(decision), fmt.Sprintf("%d will climb to level %d (~%.0f points vs ~%.0f "+
				"shooting)%s", robot.Team, level, choice.Total(), all[i].options[0].Total(), why), t)
		}
	}
}

// reachChance is the chance an expected total reaches a threshold, with the given uncertainty (a logistic curve).
func reachChance(expected, threshold, spread float64) float64 {
	return 1 / (1 + math.Exp(-(expected-threshold)/math.Max(1, spread)*1.7))
}
