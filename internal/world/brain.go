package world

import (
	"math"
	"sort"
)

// Teleop decisions. Every quarter second a drive team scores the jobs it could be doing — by its role in the game plan,
// the state of the match (its hub lit or not and for how long, what FUEL it knows about, how threatening the opponents
// are) and what its coach asked for — and keeps doing the best one, with some stickiness so it does not dither.

// JobScore is one candidate job and how attractive it looked (shown in the AI panel).
type JobScore struct {
	Job   string  `json:"job"`
	Score float64 `json:"score"`
}

// Teleop jobs.
const (
	JobScoring  = "score"   // collect and shoot into the hub
	JobStage    = "stage"   // hub off: gather near home and be in place the moment it lights
	JobDefend   = "defend"  // guard the opponents' scorers while their hub is lit
	JobSteal    = "steal"   // raid the opponent zone
	JobShuttle  = "shuttle" // lob neutral FUEL home
	JobReverse  = "reverse" // pull FUEL off the opponent's side of the neutral zone
	JobFeed     = "feed"    // load from the outpost chute
	JobDeliver  = "deliver" // drop FUEL in the outpost corral for the human player
	JobHerd     = "herd"    // push FUEL piles toward home with the bumpers
	decideEvery = 0.25
	commitTime  = 1.5
)

var Jobs = []string{JobScoring, JobStage, JobDefend, JobSteal, JobShuttle, JobReverse, JobFeed, JobDeliver, JobHerd}

// teleopContext is what a drive team knows about the match right now.
type teleopContext struct {
	activeNow   bool    // our hub scores now
	toActive    float64 // seconds until it scores again (0 if now)
	activeLeft  float64 // seconds until it stops being lit (0 if not lit)
	oppActive   bool
	timeToCount float64 // trigger to arena count for a shot from our spot
	travelSpot  float64 // seconds to drive to our shooting spot
	fullness    float64 // hopper / capacity
}

// canScoreSoon reports whether a shot fired after driving to the spot would land in a scoring window.
func (ctx teleopContext) canScoreSoon(extra float64) bool {
	return ctx.activeNow || ctx.toActive <= ctx.travelSpot+ctx.timeToCount+extra
}

func (agent *Agent) context(robot *Robot, world *World) teleopContext {
	ctx := teleopContext{
		activeNow:   agent.hubActive(robot, world),
		oppActive:   agent.opponentHubActive(robot, world),
		timeToCount: agent.timeToCount(robot, world),
		travelSpot:  agent.travelTime(robot, agent.shootSpot(robot, world)),
		fullness:    float64(robot.Hopper) / math.Max(1, float64(robot.Arch.Capacity)),
	}
	if !ctx.activeNow {
		ctx.toActive = agent.secondsToActive(robot, world)
	} else {
		ctx.activeLeft = agent.secondsActiveLeft(robot, world)
	}
	return ctx
}

func (agent *Agent) runTeleop(robot *Robot, world *World, plan RobotPlan) {
	if agent.endgame(robot, world, plan) {
		agent.job = "climb"
		return
	}
	ctx := agent.context(robot, world)
	if world.time >= agent.decideAt || agent.job == "" || agent.job == "climb" {
		agent.decideAt = world.time + decideEvery
		agent.chooseJob(robot, world, plan, ctx)
	}
	switch agent.job {
	case JobStage:
		agent.stage(robot, world, ctx)
	case JobDefend:
		agent.defend(robot, world)
	case JobSteal:
		agent.steal(robot, world, ctx)
	case JobShuttle:
		agent.shuttle(robot, world, "neutral", ctx)
	case JobReverse:
		agent.shuttle(robot, world, "deny", ctx)
	case JobFeed:
		if world.outposts[robot.Alliance].chuteCount(world) > 0 && robot.Hopper < robot.Arch.Capacity*3/4 {
			agent.goFeed(robot, world)
		} else {
			agent.scoreCycle(robot, world, ctx)
		}
	case JobDeliver:
		agent.goDeliver(robot, world, ctx)
	case JobHerd:
		agent.herd(robot, world)
	default:
		agent.scoreCycle(robot, world, ctx)
	}
	// Waiting at the spot for the hub is only fine briefly (unless it is about to light: then this is exactly where to
	// be); if it drags on, think again right away.
	if robot.AiState == "shoot-wait" {
		if agent.waitSince == 0 {
			agent.waitSince = world.time
		} else if world.time-agent.waitSince > 1.5 && (ctx.activeNow || ctx.toActive > stageLead(agent.Personality)+ctx.timeToCount+1) {
			agent.decideAt = 0
			agent.jobSince = -10
		}
	} else {
		agent.waitSince = 0
	}
}

// roleWeights is how much a role values each job.
func roleWeights(plan RobotPlan, personality Personality) map[string]float64 {
	weights := map[string]float64{JobScoring: 0.6, JobStage: 0.35, JobDefend: 0.1 + 0.25*personality.Aggression,
		JobSteal: 0.1, JobShuttle: 0.15, JobReverse: 0.1, JobFeed: 0.15, JobDeliver: 0.2, JobHerd: 0.25}
	switch plan.Strategy {
	case StratShuttle:
		weights[JobShuttle], weights[JobScoring] = 1.0, 0.35
	case StratReverse:
		weights[JobReverse], weights[JobShuttle], weights[JobScoring] = 1.0, 0.4, 0.3
	case StratDefense:
		weights[JobDefend], weights[JobReverse], weights[JobScoring] = 1.25, 0.7, 0.3
	case StratFeeder:
		weights[JobFeed], weights[JobScoring], weights[JobDeliver] = 1.1, 0.9, 0.4
	case StratSteal:
		weights[JobSteal], weights[JobScoring] = 1.0, 0.45
	default: // scorer
		weights[JobScoring] = 1.0
		switch plan.Inactive {
		case InactiveDefend:
			weights[JobDefend] = 1.1
		case InactiveSteal:
			weights[JobSteal] = 1.1
		case InactiveShuttle:
			weights[JobShuttle] = 1.1
		default:
			weights[JobStage] = 1.0
		}
	}
	return weights
}

// chooseJob scores every job and switches to the best one (with stickiness).
func (agent *Agent) chooseJob(robot *Robot, world *World, plan RobotPlan, ctx teleopContext) {
	weights := roleWeights(plan, agent.Personality)
	if practiceMatch(robot) {
		// Practice and test matches are for working on the robot and the drive team's own cycles, not for beating up
		// on the other alliance: defense and stealing mostly wait for qualifications.
		weights[JobDefend] *= 0.15
		weights[JobSteal] *= 0.3
	}
	outpost := world.outposts[robot.Alliance]
	opportunity := map[string]float64{}

	// Scoring pays while the hub is lit or will be by the time a load is ready.
	switch {
	case ctx.activeNow || ctx.canScoreSoon(0.5):
		opportunity[JobScoring] = 1
	case ctx.fullness < 0.85 && ctx.toActive < 14:
		opportunity[JobScoring] = 0.8 // fill up for the next active period
	default:
		opportunity[JobScoring] = 0.1
	}
	if !ctx.activeNow {
		opportunity[JobStage] = 0.95
	}
	// Defense: worth it while the opponents' hub is lit, more so against loaded shooters.
	if ctx.oppActive {
		endgame := robot.Remaining <= 30
		threat := 0.0
		for _, other := range world.robots {
			if !other.Present || other.Alliance == robot.Alliance || !other.Enabled || other.Faults.Tipped || other.onTower() {
				continue
			}
			if endgame && world.nearOwnTower(other, 2.5) {
				continue
			}
			threat = math.Max(threat, math.Min(1.2, float64(other.Hopper)/30+0.3))
		}
		if endgame {
			threat *= 0.3 // endgame defense risks tower fouls (G420); most teams go score or climb instead
		}
		opportunity[JobDefend] = threat
	}
	// Collect-and-move jobs scale with the FUEL known to be there.
	count := func(area string) float64 {
		n := 0
		agent.knownBalls(robot, world, func(p Vec2, weight int) {
			if agent.inArea(robot, world, area, p) {
				n += weight
			}
		})
		return float64(n)
	}
	neutral := count("neutral")
	opportunity[JobShuttle] = math.Min(1, neutral/40)
	opportunity[JobReverse] = math.Min(1, count("deny")/30)
	opportunity[JobSteal] = math.Min(1, count("steal")/20)
	if outpost.chuteCount(world) > 0 && ctx.fullness < 0.75 {
		opportunity[JobFeed] = 1
	}
	// Get FUEL to the human player only when there is a pile of loose FUEL in front of the outpost to bulldoze into
	// the floor opening (robots do not spit FUEL out of their intakes), and the corral is low. Only some teams make a
	// habit of it; once pushing, a robot finishes the pile.
	if _, pile := agent.outpostPile(robot, world); pile && outpost.Corral < corralStockTarget {
		opportunity[JobDeliver] = 0.6 * outpostHabit(robot.Team)
		if agent.job == JobDeliver {
			opportunity[JobDeliver] = 1.2
		}
	}
	if !ctx.activeNow && ctx.fullness >= 0.9 && neutral >= 6 {
		opportunity[JobHerd] = 0.45
	}

	var scores []JobScore
	best, bestScore := JobScoring, -1.0
	for _, job := range Jobs {
		score := weights[job] * opportunity[job]
		if bias, ok := agent.coachBias[job]; ok {
			score *= bias
		}
		if job == agent.job {
			score *= 1.2 // stickiness
		}
		scores = append(scores, JobScore{Job: job, Score: math.Round(score*100) / 100})
		if score > bestScore {
			best, bestScore = job, score
		}
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })
	agent.jobScores = scores
	// Commit to a job for a moment unless it has become worthless.
	if agent.job != "" && agent.job != "climb" && best != agent.job && world.time-agent.jobSince < commitTime {
		for _, s := range scores {
			if s.Job == agent.job && s.Score > 0.05 {
				return
			}
		}
	}
	if best != agent.job {
		agent.job = best
		agent.jobSince = world.time
	}
}

// scoreCycle collects until the hopper is worth unloading, then unloads into the hub.
func (agent *Agent) scoreCycle(robot *Robot, world *World, ctx teleopContext) {
	capacity := robot.Arch.Capacity
	if robot.Hopper == 0 {
		agent.unloading = false
	}
	if !agent.unloading && robot.Hopper > 0 {
		unloadTime := float64(robot.Hopper)/robot.Arch.ShootRate + ctx.travelSpot
		switch {
		case robot.Hopper >= capacity*9/10:
			agent.unloading = true // full: go empty it
		case agent.nearestBallDistance(robot, world, "play") > 4.5:
			agent.unloading = true // nothing worth chasing nearby
		case ctx.activeNow && robot.Hopper >= capacity/4 && ctx.activeLeft+GraceSec-ctx.timeToCount < unloadTime+2:
			agent.unloading = true // the hub is about to stop counting: score what we have
		}
	}
	if agent.unloading {
		agent.unload(robot, world, ctx)
		return
	}
	agent.goCollect(robot, world, "play")
	agent.turretFire(robot, world)
}

// stageLead is how many seconds before it needs to be there a drive team heads to its spot for the next active period.
func stageLead(personality Personality) float64 {
	return 1.6 + (1-personality.Risk)*1.2
}

// unload shoots the hopper empty when that would count, and otherwise keeps busy until it will.
func (agent *Agent) unload(robot *Robot, world *World, ctx teleopContext) {
	if ctx.canScoreSoon(stageLead(agent.Personality)) {
		agent.goShoot(robot, world)
		return
	}
	agent.stage(robot, world, ctx)
}

// stage is the inactive-hub job: gather FUEL near home and be at the shooting spot just as the hub lights. A full robot
// with time to spare delivers to the human player or herds FUEL instead of parking.
func (agent *Agent) stage(robot *Robot, world *World, ctx teleopContext) {
	if ctx.activeNow {
		agent.scoreCycle(robot, world, ctx)
		return
	}
	// Be on the spot, lined up and on the trigger before the hub lights (with a margin for traffic: careful drive
	// teams get there earlier), so the first early shots are already in the air as it turns on.
	if robot.Hopper > 0 && ctx.toActive <= ctx.travelSpot+ctx.timeToCount+stageLead(agent.Personality) {
		agent.goShoot(robot, world)
		if robot.AiState == "to-shoot" {
			robot.AiState = "pre-position"
			agent.Intent = "getting in place for the next active period"
		}
		return
	}
	if robot.Hopper < robot.Arch.Capacity {
		agent.goCollect(robot, world, "home")
		if robot.AiState == "collect" {
			robot.AiState = "stage"
			agent.Intent = "gathering FUEL near our hub while it is off"
		}
		return
	}
	outpost := world.outposts[robot.Alliance]
	if _, pile := agent.outpostPile(robot, world); pile && outpost.Corral < corralStockTarget && outpostHabit(robot.Team) == 1 {
		agent.pushToOutpost(robot, world)
		return
	}
	agent.herd(robot, world)
}

// practiceMatch reports whether the field says this is a practice or test match (FMS match type 1, or 0 for a test
// match or no FMS).
func practiceMatch(robot *Robot) bool { return robot.MatchType <= 1 }

// practiceMatch reports whether the robots on the field are playing a practice or test match.
func (world *World) practiceMatch() bool {
	for _, robot := range world.robots {
		if robot.Present {
			return practiceMatch(robot)
		}
	}
	return false
}

// practiceCuriosity is how readily a team tries something new: in practice and test matches, much more (that is
// what they are for).
func practiceCuriosity(robot *Robot, curiosity float64) float64 {
	if practiceMatch(robot) {
		return math.Min(1, curiosity*3+0.3)
	}
	return curiosity
}
