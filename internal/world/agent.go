package world

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
)

// Agent is an AI drive team. It only uses what a real team knows: its driver station's enable/auto state, the match
// time and game data from the FMS, the hub lights it can see, what its cameras and drivers can see on the field, and
// what its alliance's coach tells it. Its role comes from the alliance game plan (plan.go), its moment-to-moment job
// from its own decisions (brain.go) and the coach (coach.go).
type Agent struct {
	strafeLeg   int         // turret strafing leg while shooting on the move
	Reliability float64     // chance a climb reaches the planned level
	Personality Personality // how this drive team plays
	team        int         // the team the personality belongs to
	reaction    float64     // seconds from deciding to the robot acting

	clock matchClock

	// Decisions (brain.go) and the coach's instructions (coach.go).
	job       string
	jobSince  float64
	decideAt  float64
	jobScores []JobScore
	coachBias map[string]float64
	climbPlan int // 0 = undecided, -1 = the coach says skip the climb, n = climb to level n
	markIndex int // the opponent the coach wants guarded (-1 = choose)
	waitSince float64

	climbGoal   int
	autoClimbed bool
	auto        *autoRun // this match's pre-programmed routine (autoprog.go)

	// Experiments and memory (memory.go).
	choices         map[string]string  // this match's experimental choices by category
	Experiment      string             // what the team is trying this match (for the panel)
	shootDistance   float64            // metres from the hub to shoot from (0 = default)
	misread         bool               // misread the game data at the start of teleop
	tippedAt        float64            // world time it was first seen tipped over
	eStopped        bool               // its drive team has pressed the E-stop this match
	mechReliability float64            // how robust the robot is (jams)
	hazards         map[[2]int]float64 // nav cells the team remembers getting stuck in
	wasAuto         bool               // enabled in auto on the previous decision (a new match starts on the rising edge)
	unloading       bool               // latched: unload until empty, then refill until full

	releaseUntil  float64 // backing off after a pin (G-rule: release and retreat before re-engaging)
	contactTarget *Robot  // the robot this one is deliberately in contact with (not avoided)

	// Navigation (nav.go).
	path         []Vec2
	pathAt       float64
	pathGoal     Vec2
	pathFailed   bool
	avoidCells   map[[2]int]float64
	stuckReplans int

	// Collection targeting and progress watching.
	blacklist   map[[2]int]float64
	targetCell  [2]int
	progressAt  float64
	progressHop int
	lastPos     Vec2
	movedAt     float64
	escapeUntil float64
	escapeVel   Vec2
	lastLight   float64
	wobble      Vec2
	wobbleUntil float64

	// Perception (perception.go).
	belief      *belief
	searchIndex int
	searchAt    float64

	// What the AI panel and 3D overlay show.
	Intent    string
	target    Vec2
	hasTarget bool

	rng       *rand.Rand
	shotStats ShotStats

	// Endgame planning (climbing.go).
	teleopStarted    bool
	teleopScoredBase int     // robot.Scored when teleop began
	activeTime       float64 // teleop seconds with our hub lit
	climbOptionsSeen []ClimbOption
}

// NewAgent creates an AI driver for a robot.
func NewAgent(robot *Robot, seed int64) *Agent {
	return &Agent{Reliability: 0.82, rng: rand.New(rand.NewSource(seed)), blacklist: map[[2]int]float64{},
		avoidCells: map[[2]int]float64{}, coachBias: map[string]float64{}, markIndex: -1}
}

// teleopShift infers the hub shift from the FMS match time (teleop is 140 s: 10 transition, 4 × 25, 30 end game).
func teleopShift(remaining int) int {
	switch {
	case remaining > 130:
		return 0
	case remaining > 105:
		return 1
	case remaining > 80:
		return 2
	case remaining > 55:
		return 3
	case remaining > 30:
		return 4
	default:
		return 5
	}
}

// allianceHubActive applies the game-data rule: the auto winner's hub is inactive in shifts 1 and 3.
func allianceHubActive(alliance int, gameData string, remaining int, auto bool) bool {
	if auto {
		return true
	}
	shift := teleopShift(remaining)
	if shift == 0 || shift == 5 || gameData == "" {
		return true
	}
	wonAuto := (gameData == "R" && alliance == Red) || (gameData == "B" && alliance == Blue)
	return (shift == 1 || shift == 3) != wonAuto
}

// hubActive is what this drive team believes about its own hub right now: its clock and the game data, with the
// light as the cue before game data arrives.
func (agent *Agent) hubActive(robot *Robot, world *World) bool {
	if world.hubs[robot.Alliance].LightOn {
		agent.lastLight = world.time
	}
	won, known := agent.readGameData(robot, world)
	if robot.Auto || !known {
		return agent.lightRecently(world) || robot.Auto
	}
	t, _ := agent.clock.matchTime(world.time)
	return ActiveAt(t, won)
}

// opponentHubActive is whether the other alliance's hub is scoring right now.
func (agent *Agent) opponentHubActive(robot *Robot, world *World) bool {
	won, known := agent.readGameData(robot, world)
	if robot.Auto || !known {
		return true
	}
	t, _ := agent.clock.matchTime(world.time)
	return ActiveAt(t, !won)
}

func opponent(alliance int) int {
	if alliance == Red {
		return Blue
	}
	return Red
}

// Update runs one decision step, then checks the plan against the rules the referees call.
func (agent *Agent) Update(robot *Robot, world *World, dt float64) {
	agent.decide(robot, world, dt)
	if robot.Enabled {
		agent.obeyRules(robot, world)
	}
}

func (agent *Agent) decide(robot *Robot, world *World, dt float64) {
	robot.Ctl = Control{}
	agent.clock.update(robot, world.time)
	if robot.Team != agent.team {
		agent.team = robot.Team
		agent.Personality = world.personalityFor(robot.Team)
	}
	agent.reaction = agent.Personality.Reaction
	plan := world.plans[robot.Index]
	if world.hubs[robot.Alliance].LightOn {
		agent.lastLight = world.time
	}
	// A new match starts when the robot is enabled in auto with the full auto countdown (not on a reconnect mid-auto).
	autoEnabled := robot.Enabled && robot.Auto
	if autoEnabled && !agent.wasAuto && robot.Remaining >= int(AutoSec)-2 {
		agent.startMatch(robot, world, plan)
	}
	agent.wasAuto = autoEnabled
	if !robot.Enabled {
		robot.AiState = "idle"
		agent.Intent = "disabled"
		return
	}
	if robot.Faults.Tipped {
		agent.eStopTipped(robot, world)
		robot.AiState = "tipped"
		agent.Intent = "tipped over: waiting for the E-stop"
		return
	}
	if !robot.Auto && !agent.teleopStarted {
		agent.teleopStarted = true
		agent.teleopScoredBase = robot.Scored
		agent.finishAuto(robot, world)
		if agent.auto != nil {
			agent.auto.result.Tower = TowerPoints(robot.ClimbLevel, true)
		}
		// Now and then a drive team reads the game data the wrong way round (less often once it has done it before).
		if stats, ok := world.learning.Teams[robot.Team]; ok {
			chance := (1 - agent.Personality.Discipline) * 0.04 * math.Pow(0.5, stats.incidentWeight("misread"))
			agent.misread = agent.rng.Float64() < chance
		}
	}
	if !robot.Auto && robot.Enabled {
		agent.naturalMistakes(robot, world, dt)
		agent.recover(robot, world, dt)
	}
	if !robot.Auto && agent.hubActive(robot, world) {
		agent.activeTime += dt
	}
	if !robot.Auto && robot.Enabled && agent.autoClimbed {
		// Drop off the tower after an auto climb to play teleop (the referee already counted the auto tower points).
		// Only once enabled in teleop: a disabled robot hangs there through the pause, which is also when the
		// referees look, so they and the field record agree whichever arrives first (period change or DS packet).
		if robot.ClimbLevel >= 1 {
			robot.AutoTower = TowerPoints(1, true)
		}
		agent.autoClimbed = false
		agent.climbGoal = 0
		robot.ClimbLevel, robot.ClimbProg, robot.Climbing, robot.Z = 0, 0, false, 0
	}
	agent.contactTarget = nil
	agent.hasTarget = false
	if robot.ClimbLevel > 0 && !robot.Climbing {
		robot.AiState = "climb"
		agent.Intent = "hanging on the tower"
		robot.Ctl.Climb = agent.climbGoal
		if robot.Auto {
			agent.autoClimbed = true
		}
		return
	}
	agent.perceive(robot, world)
	if robot.Auto {
		// AUTO is the team's pre-programmed routine: no drivers, no replanning, no getting unstuck.
		agent.runAuto(robot, world)
		return
	}
	agent.maybeWobble(robot, world)
	if agent.unstick(robot, world) {
		return
	}
	agent.runTeleop(robot, world, plan)
}

// startMatch resets everything that belongs to one match.
func (agent *Agent) startMatch(robot *Robot, world *World, plan RobotPlan) {
	agent.climbGoal = 0
	agent.climbPlan = 0
	agent.autoClimbed = false
	agent.unloading = robot.Hopper > 0 && plan.Auto != AutoShuttle // start by emptying the preload
	agent.blacklist = map[[2]int]float64{}
	agent.avoidCells = map[[2]int]float64{}
	agent.coachBias = map[string]float64{}
	agent.markIndex = -1
	agent.job, agent.jobScores = "", nil
	agent.path = nil
	agent.belief = nil
	agent.shotStats = ShotStats{}
	agent.movedAt = world.time
	agent.teleopStarted, agent.activeTime, agent.climbOptionsSeen = false, 0, nil
	agent.misread, agent.tippedAt, agent.eStopped = false, 0, false
	agent.mechReliability = world.teamReliability(robot.Team)
	agent.Reliability = clamp(0.55+0.4*agent.mechReliability, 0.6, 0.97)
	agent.hazards = world.rememberedHazards(robot)
	agent.chooseExperiments(robot, world)
	agent.beginAuto(robot, world)
}

// maybeWobble gives inconsistent drivers the occasional overshoot.
func (agent *Agent) maybeWobble(robot *Robot, world *World) {
	if world.time < agent.wobbleUntil {
		return
	}
	chance := (1 - agent.Personality.Consistency) * 0.012 // per decision (30 Hz)
	if agent.rng.Float64() < chance {
		angle := agent.rng.Float64() * 2 * math.Pi
		agent.wobble = Vec2{math.Cos(angle), math.Sin(angle)}.Scale(0.8 + agent.rng.Float64()*0.8)
		agent.wobbleUntil = world.time + 0.25 + agent.rng.Float64()*0.35
	}
}

// busyStill reports whether the robot's current state is one where standing still is the job.
func busyStill(robot *Robot, world *World) bool {
	state := strings.TrimPrefix(robot.AiState, "auto:")
	for _, prefix := range []string{"shoot", "pass", "feed", "release", "guard", "deliver", "climb"} {
		if strings.HasPrefix(state, prefix) {
			return true
		}
	}
	return robot.Climbing || robot.ClimbLevel > 0 || robot.Faults.Tipped || world.time < robot.Faults.StuckUntil
}

// unstick notices when a robot stops making progress. It first re-plans around the spot (marking it as blocked for a
// few seconds); if that keeps failing it backs out toward open field.
func (agent *Agent) unstick(robot *Robot, world *World) bool {
	if robot.Pos.Sub(agent.lastPos).Len() > 0.25 || busyStill(robot, world) {
		if robot.Pos.Sub(agent.lastPos).Len() > 0.6 {
			agent.stuckReplans = 0
		}
		agent.lastPos = robot.Pos
		agent.movedAt = world.time
	} else if world.time-agent.movedAt > 1.4 && world.time > agent.escapeUntil+1 {
		agent.movedAt = world.time
		agent.stuckReplans++
		if agent.stuckReplans <= 2 {
			// Mark the cells just ahead (where it is trying to go) as blocked and plan around them.
			grid := world.navGridFor(robot)
			ahead := robot.Ctl.Vel
			if ahead.Len() < 0.1 {
				ahead = robot.forward()
			}
			probe := robot.Pos.Add(ahead.Norm().Scale(robot.half().X + 0.3))
			cx, cy := grid.cellOf(probe)
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					agent.avoidCells[[2]int{cx + dx, cy + dy}] = world.time + 4
				}
			}
			agent.path = nil
			return false
		}
		// Back out toward open field (the field center), with some randomness so robots do not repeat the same move.
		agent.stuckReplans = 0
		center := Vec2{world.layout.Field.Length / 2, world.layout.Field.Width / 2}
		toCenter := center.Sub(robot.Pos).Norm()
		angle := math.Atan2(toCenter.Y, toCenter.X) + (agent.rng.Float64()-0.5)*2.0
		agent.escapeVel = Vec2{math.Cos(angle), math.Sin(angle)}.Scale(2.2)
		agent.escapeUntil = world.time + 0.8
	}
	if world.time < agent.escapeUntil {
		robot.AiState = "unstick"
		agent.Intent = "backing out of a jam"
		robot.Ctl.Vel = agent.escapeVel
		return true
	}
	return false
}

// ---- Behaviors -------------------------------------------------------------------------------------------------

// inArea reports whether a floor point is somewhere this behavior should collect from.
func (agent *Agent) inArea(robot *Robot, world *World, area string, p Vec2) bool {
	l := world.layout
	hub := world.hubs[robot.Alliance]
	toNeutral := hub.neutralDir
	ownLine := hub.Center.X
	oppLine := world.hubs[opponent(robot.Alliance)].Center.X
	inOwnZone := (p.X-ownLine)*toNeutral < -0.6
	inOppZone := (p.X-oppLine)*toNeutral > 0.6
	inNeutral := !inOwnZone && !inOppZone
	switch area {
	case "depot":
		depotX, depotY := l.onAlliance(robot.Alliance, l.Depot.Depth/2, l.Depot.CenterY["blue"])
		return Vec2{depotX, depotY}.Sub(p).Len() < 1.2
	case "own":
		return inOwnZone
	case "home":
		// Our zone plus the near edge of the neutral zone around our hub (where our hub spits FUEL back out).
		return inOwnZone || (inNeutral && (p.X-ownLine)*toNeutral < 2.5)
	case "neutral":
		return inNeutral
	case "deny":
		// The opponent's half of the neutral zone, where their hub spits FUEL back out.
		return inNeutral && (p.X-l.Field.Length/2)*toNeutral > -0.3
	case "steal":
		// The opponent alliance zone (their stockpiles), staying clear of the area right around their hub.
		oppHub := world.hubs[opponent(robot.Alliance)]
		return inOppZone && p.Sub(oppHub.Center).Len() > 1.6
	default: // "play": anywhere except the opponent's alliance zone
		return !inOppZone
	}
}

func (agent *Agent) goCollect(robot *Robot, world *World, area string) {
	robot.AiState = "collect"
	l := world.layout
	cellOf := func(p Vec2) [2]int { return [2]int{int(p.X / 0.3), int(p.Y / 0.3)} }
	reach := robot.half().X + 0.05
	best := Vec2{}
	bestScore := math.Inf(1)
	// Teammates' current targets (the coach keeps teammates off the same pile).
	var claimed []Vec2
	for i, other := range world.robots {
		if other == robot || other.Alliance != robot.Alliance || !other.Present {
			continue
		}
		if mate, ok := world.controllers[i].(*Agent); ok && mate.hasTarget {
			claimed = append(claimed, mate.target)
		}
	}
	// Density map (0.5 m cells) so robots prefer piles over lone balls.
	density := map[[2]int]int{}
	agent.knownBalls(robot, world, func(p Vec2, weight int) { density[[2]int{int(p.X / 0.5), int(p.Y / 0.5)}] += weight })
	agent.knownBalls(robot, world, func(p Vec2, weight int) {
		if until, ok := agent.blacklist[cellOf(p)]; ok && world.time < until {
			return
		}
		nearX := p.X < reach || p.X > l.Field.Length-reach
		nearY := p.Y < reach || p.Y > l.Field.Width-reach
		if nearX && nearY {
			return // wedged in a corner
		}
		if !world.Reachable(p, robot.Arch.Height) || world.nearHangingRobot(p) {
			return // inside the tower pocket, against a trench leg, under a hanging robot, ...
		}
		if !agent.inArea(robot, world, area, p) {
			return
		}
		if world.inDropZone(p, 0) || (robot.Auto && !world.autoReach(robot, p)) {
			return // under a releasing hub exit (G408), or past the center line in AUTO (G403)
		}
		score := p.Sub(robot.Pos).Len()
		score -= math.Min(2.5, float64(density[[2]int{int(p.X / 0.5), int(p.Y / 0.5)}])*0.25)
		for _, other := range world.robots {
			if other != robot && other.Present && other.Pos.Sub(p).Len() < 0.9 {
				score += 2
			}
		}
		for _, c := range claimed {
			if c.Sub(p).Len() < 1.0 {
				score += 1.5
			}
		}
		// Teammates spread across the field's width by station.
		lane := float64(robot.Index%3-1) * l.Field.Width / 3
		score += math.Abs(p.Y-(l.Field.Width/2+lane)) * 0.3
		if score < bestScore {
			bestScore = score
			best = p
		}
	})
	if math.IsInf(bestScore, 1) {
		if area != "play" && area != "home" {
			agent.goCollect(robot, world, "play")
			return
		}
		// Nothing known to pick up here: go and look (never stand around waiting).
		agent.search(robot, world, area)
		return
	}
	cell := cellOf(best)
	if cell != agent.targetCell || robot.Hopper != agent.progressHop {
		agent.targetCell = cell
		agent.progressHop = robot.Hopper
		agent.progressAt = world.time
	} else if world.time-agent.progressAt > 2.5 {
		agent.blacklist[cell] = world.time + 15
		agent.progressAt = world.time
	}
	agent.target, agent.hasTarget = best, true
	agent.Intent = "collecting FUEL"
	robot.Ctl.Intake = true
	agent.driveTo(robot, world, best, 0, false)
}

// goShoot drives to the shooting spot and fires when a shot from there would count (including timed early and late
// shots around shift changes).
func (agent *Agent) goShoot(robot *Robot, world *World) {
	if robot.Arch.Turret {
		agent.turretRun(robot, world)
		return
	}
	if !agent.goShootSpot(robot, world) {
		robot.AiState = "to-shoot"
		agent.Intent = "driving to the shooting spot"
		return
	}
	fire, timing := agent.shotTiming(robot, world)
	if !fire {
		robot.AiState = "shoot-wait"
		agent.Intent = "lined up, waiting for the hub"
		return
	}
	robot.Ctl.Shoot = true
	robot.Ctl.Timing = timing
	robot.AiState = "shoot"
	switch timing {
	case timingEarly:
		robot.AiState = "shoot-early"
		agent.Intent = "shooting early: the hub lights up as these land"
	case timingLate:
		robot.AiState = "shoot-late"
		agent.Intent = "shooting late: counted inside the grace window"
	default:
		agent.Intent = "shooting"
	}
}

// shootSpot returns a spot in this alliance's zone facing the hub, spread out per robot.
func (agent *Agent) shootSpot(robot *Robot, world *World) Vec2 {
	hub := world.hubs[robot.Alliance]
	toAlliance := -hub.neutralDir
	distance := math.Min(math.Max(robot.Arch.MaxRange*0.6, 2.5), 2.8)
	if agent.shootDistance > 0 {
		distance = agent.shootDistance // the range this team is shooting from this match (memory.go)
	}
	angle := float64(robot.Index%3-1) * 0.55
	return hub.Center.Add(Vec2{toAlliance * distance * math.Cos(angle), distance * math.Sin(angle)})
}

// travelTime estimates how long driving to a point takes.
func (agent *Agent) travelTime(robot *Robot, p Vec2) float64 {
	return robot.Pos.Sub(p).Len()/(robot.Arch.MaxSpeed*0.65) + 0.4
}

// goShootSpot drives to the shooting spot; returns true once there.
func (agent *Agent) goShootSpot(robot *Robot, world *World) bool {
	spot := agent.shootSpot(robot, world)
	hub := world.hubs[robot.Alliance]
	agent.target, agent.hasTarget = spot, true
	if robot.Pos.Sub(spot).Len() < 0.35 {
		away := robot.Pos.Sub(hub.Center)
		agent.faceAngle(robot, math.Atan2(away.Y, away.X)) // rear-mounted shooter toward the hub
		return true
	}
	agent.driveTo(robot, world, spot, 0, false)
	return false
}

// turretFire lets turret robots shoot while driving whenever they are in range in their own zone and the shot would
// count.
func (agent *Agent) turretFire(robot *Robot, world *World) {
	if !robot.Arch.Turret || robot.Hopper == 0 {
		return
	}
	hub := world.hubs[robot.Alliance]
	offset := robot.Pos.Sub(hub.Center)
	inOwnZone := offset.X*hub.neutralDir < -0.8
	distance := offset.Len()
	if !inOwnZone || distance <= 1.6 || distance >= robot.Arch.MaxRange*0.85 {
		return
	}
	if fire, timing := agent.shotTiming(robot, world); fire {
		robot.Ctl.Shoot = true
		robot.Ctl.Timing = timing
		robot.AiState += "+fire"
	}
}

// passSpot is on the neutral side of this alliance's hub line, away from the hub's back net. Each robot uses its own
// lane so shuttles do not queue for the same spot.
func (agent *Agent) passSpot(robot *Robot, world *World) (Vec2, Vec2) {
	l := world.layout
	hub := world.hubs[robot.Alliance]
	offset := l.Hub.Size/2 + l.Bump.Width/2
	lanes := []float64{hub.Center.Y - offset, hub.Center.Y + offset, l.Trench.OpeningCenterY, l.Field.Width - l.Trench.OpeningCenterY}
	laneY := lanes[robot.Index%3]
	spot := Vec2{hub.Center.X + hub.neutralDir*(1.6+0.4*float64(robot.Index%2)), laneY}
	target := Vec2{hub.Center.X - hub.neutralDir*1.7, laneY + (agent.rng.Float64()-0.5)*0.8}
	return spot, target
}

func (agent *Agent) goPass(robot *Robot, world *World) {
	spot, target := agent.passSpot(robot, world)
	// Anywhere in the neutral zone within lob range and clear of the hub's net works: pass from right here.
	hub := world.hubs[robot.Alliance]
	ahead := (robot.Pos.X - hub.Center.X) * hub.neutralDir
	clearOfNet := math.Abs(robot.Pos.Y-hub.Center.Y) > world.layout.Hub.Size/2+0.35
	if ahead > 1.1 && ahead < robot.Arch.MaxRange+0.5 && clearOfNet {
		target = Vec2{hub.Center.X - hub.neutralDir*1.7, clamp(robot.Pos.Y, 0.6, world.layout.Field.Width-0.6)}
		if math.Abs(target.Y-hub.Center.Y) < world.layout.Hub.Size/2+0.4 {
			target.Y = hub.Center.Y + math.Copysign(world.layout.Hub.Size/2+0.6, target.Y-hub.Center.Y)
		}
		spot = robot.Pos
	}
	agent.target, agent.hasTarget = target, true
	if robot.Pos.Sub(spot).Len() < 0.45 {
		robot.AiState = "pass"
		agent.Intent = "lobbing FUEL home for the scorers"
		away := robot.Pos.Sub(target)
		agent.faceAngle(robot, math.Atan2(away.Y, away.X))
		robot.Ctl.Pass = true
		robot.Ctl.PassTarget = target
		return
	}
	robot.AiState = "to-pass"
	agent.Intent = "carrying FUEL to the passing lane"
	agent.driveTo(robot, world, spot, 0, false)
}

func (agent *Agent) goClimb(robot *Robot, world *World, level int) {
	robot.AiState = "climb"
	agent.Intent = "climbing to level " + string(rune('0'+level))
	slot := robot.Index%3 - 1
	robot.ClimbSlot = slot
	zone, heading := world.climbZone(robot.Alliance, slot)
	agent.target, agent.hasTarget = zone, true
	approach := zone.Sub(Vec2{math.Cos(heading), math.Sin(heading)}.Scale(0.6))
	if robot.Pos.Sub(zone).Len() > 0.28 || math.Abs(wrapAngle(robot.Heading-heading)) > 0.35 {
		robot.AiState = "to-climb"
		if robot.Pos.Sub(approach).Len() > 0.4 && robot.Pos.Sub(zone).Len() > 0.5 {
			agent.driveTo(robot, world, approach, heading, true)
			return
		}
		agent.driveTo(robot, world, zone, heading, true)
		return
	}
	robot.Ctl.Climb = level
}

// goFeed takes FUEL from the outpost chute while the human player releases it.
func (agent *Agent) goFeed(robot *Robot, world *World) {
	robot.AiState = "feed"
	agent.Intent = "loading from the outpost chute"
	outpost := world.outposts[robot.Alliance]
	spot := Vec2{outpost.wallX + outpost.inward*(robot.half().X+0.12), outpost.chuteY}
	heading := math.Pi
	if outpost.inward < 0 {
		heading = 0
	}
	agent.target, agent.hasTarget = spot, true
	robot.Ctl.Intake = true
	outpost.robotComing = world.time
	if robot.Pos.Sub(spot).Len() > 0.3 {
		robot.AiState = "to-outpost"
		agent.driveTo(robot, world, spot, heading, true)
		return
	}
	agent.faceAngle(robot, heading)
	outpost.holdOpenUntil = world.time + 0.5
}

// goDeliver gets FUEL to the human player the way robots really do: by bulldozing a pile of loose FUEL along the
// carpet into the outpost floor opening with the bumper, driving right up to the wall. Robots do not spit FUEL out of
// their intakes. With no pile there it goes back to scoring.
func (agent *Agent) goDeliver(robot *Robot, world *World, ctx teleopContext) {
	if _, ok := agent.outpostPile(robot, world); !ok {
		agent.scoreCycle(robot, world, ctx)
		return
	}
	agent.pushToOutpost(robot, world)
}

// herd pushes a pile of neutral-zone FUEL toward this alliance's zone with the bumpers (for a robot that is full while
// its hub is off, so the scorers have FUEL close by when it lights up).
func (agent *Agent) herd(robot *Robot, world *World) {
	robot.AiState = "herd"
	agent.Intent = "herding FUEL toward our hub"
	hub := world.hubs[robot.Alliance]
	toHome := Vec2{-hub.neutralDir, 0}
	// The densest known neutral pile.
	density := map[[2]int]int{}
	var best [2]int
	bestCount := 0
	agent.knownBalls(robot, world, func(p Vec2, weight int) {
		if !agent.inArea(robot, world, "neutral", p) {
			return
		}
		cell := [2]int{int(p.X), int(p.Y)}
		density[cell] += weight
		if density[cell] > bestCount {
			best, bestCount = cell, density[cell]
		}
	})
	if bestCount < 3 {
		agent.search(robot, world, "neutral")
		return
	}
	pile := Vec2{float64(best[0]) + 0.5, float64(best[1]) + 0.5}
	behind := pile.Sub(toHome.Scale(1.4))
	through := pile.Add(toHome.Scale(1.8))
	// Approach from the far side, then drive through toward home.
	if (robot.Pos.X-pile.X)*hub.neutralDir < 0.4 {
		agent.target, agent.hasTarget = behind, true
		agent.driveTo(robot, world, behind, 0, false)
		return
	}
	agent.target, agent.hasTarget = through, true
	agent.driveTo(robot, world, through, math.Atan2(toHome.Y, toHome.X), true)
}

// search drives to likely FUEL spots when it knows of none (limited sight), cycling through them.
func (agent *Agent) search(robot *Robot, world *World, area string) {
	robot.AiState = "search"
	agent.Intent = "looking for FUEL"
	spots := agent.searchSpots(robot, world, area)
	if len(spots) == 0 {
		return
	}
	spot := spots[agent.searchIndex%len(spots)]
	if robot.Pos.Sub(spot).Len() < 0.8 || world.time-agent.searchAt > 6 {
		agent.searchIndex++
		agent.searchAt = world.time
		spot = spots[agent.searchIndex%len(spots)]
	}
	agent.target, agent.hasTarget = spot, true
	robot.Ctl.Intake = robot.Hopper < robot.Arch.Capacity
	agent.driveTo(robot, world, spot, 0, false)
}

// searchSpots are where FUEL tends to collect, for each area.
func (agent *Agent) searchSpots(robot *Robot, world *World, area string) []Vec2 {
	l := world.layout
	hub := world.hubs[robot.Alliance]
	opp := world.hubs[opponent(robot.Alliance)]
	mid := l.Field.Width / 2
	ownSide := func(dx, y float64) Vec2 { return Vec2{hub.Center.X - hub.neutralDir*dx, y} }
	switch area {
	case "neutral", "deny":
		// Hub exits spray FUEL into the neutral zone; the centre line collects it.
		return []Vec2{
			{hub.Center.X + hub.neutralDir*2, mid}, {l.Field.Length / 2, mid - 2}, {opp.Center.X + opp.neutralDir*2, mid},
			{l.Field.Length / 2, mid + 2}, {l.Field.Length / 2, 1.2}, {l.Field.Length / 2, l.Field.Width - 1.2},
		}
	case "steal":
		return []Vec2{{opp.Center.X - opp.neutralDir*2.5, mid + 2}, {opp.Center.X - opp.neutralDir*2.5, mid - 2}}
	default:
		return []Vec2{ownSide(2.0, mid+1.6), {hub.Center.X + hub.neutralDir*1.8, mid - 1.5}, ownSide(2.5, mid-2.2),
			{hub.Center.X + hub.neutralDir*2.2, mid + 1.8}, {l.Field.Length / 2, mid}}
	}
}

// defend guards the opponent the coach points at (or the most dangerous one), staying between it and its hub but
// outside the space right in front of the hub.
func (agent *Agent) defend(robot *Robot, world *World) {
	robot.AiState = "guard"
	agent.Intent = "playing defense"
	opp := opponent(robot.Alliance)
	hub := world.hubs[opp]
	var mark *Robot
	bestScore := math.Inf(-1)
	endgame := robot.Remaining <= 30 && !robot.Auto
	for _, other := range world.robots {
		if !other.Present || other.Alliance != opp || other.ClimbLevel > 0 || other.Climbing {
			continue
		}
		if !other.Enabled || other.Faults.Tipped {
			continue // a dead or tipped robot is no threat (and hitting it is just a foul waiting to happen)
		}
		if endgame && world.nearOwnTower(other, 2.5) {
			continue // touching a robot at its tower in the endgame is a foul (G420): leave it alone
		}
		score := float64(other.Hopper) - other.Pos.Sub(robot.Pos).Len()*0.5
		if other.Shooting || strings.HasPrefix(other.AiState, "shoot") || other.AiState == "to-shoot" {
			score += 15 // a shooter about to score is the priority
		}
		if other.Index == agent.markIndex {
			score += 25 // the coach's call
		} else if world.markedByTeammate(robot, other) {
			score -= 12 // a teammate already has this one
		}
		if other.Vel.Len() < 0.2 && !other.Shooting && other.Hopper < 5 {
			score -= 30 // not worth marking: parked with nothing to score
		}
		if score > bestScore {
			bestScore = score
			mark = other
		}
	}
	if mark == nil {
		agent.goCollect(robot, world, "deny")
		return
	}
	agent.contactTarget = mark
	agent.target, agent.hasTarget = mark.Pos, true
	agent.Intent = "guarding " + mark.Station
	// G418: the referee counts a PIN from first contact until the robots are 72 in apart (or either has moved 72 in)
	// for more than 3 s. A disciplined drive team backs off well before 3 s and stays out until the count is over;
	// a sloppy one holds too long or comes back early and draws the foul.
	discipline := agent.Personality.Discipline
	if world.time < agent.releaseUntil || (agent.releaseUntil > 0 && world.officials.pinActive(robot.Index, mark.Index) &&
		discipline > 0.45) {
		robot.AiState = "release"
		agent.Intent = "backing off " + mark.Station + " (pin count)"
		agent.contactTarget = nil
		away := robot.Pos.Sub(mark.Pos)
		if gap(robot, mark) < pinReleaseDist+0.4 {
			robot.Ctl.Vel = away.Norm().Scale(2.8)
		} else {
			// Out of range: shadow from a distance on the hub side, ready to re-engage when the count is over.
			toHub := hub.Center.Sub(mark.Pos).Norm()
			hold := mark.Pos.Add(toHub.Scale(pinReleaseDist + 1.2))
			agent.driveTo(robot, world, hold, 0, false)
		}
		return
	}
	agent.releaseUntil = 0
	limit := 0.9 + (1-discipline)*2.6 // ~1 s for disciplined teams, up to ~3.5 s for sloppy ones
	if count := world.officials.pinCount(robot.Index, mark.Index); count > limit {
		// Stay out at least the 3 s the count needs to end (sloppy teams cut it short).
		agent.releaseUntil = world.time + 1.6 + discipline*2.4
		robot.AiState = "release"
		return
	}
	// A shooter parked on its spot gets shoved sideways off its line to the hub: the hit spoils its aim and it has
	// to set up again. Aggressive drive teams do this; others just body-block.
	parked := (mark.Shooting || strings.HasPrefix(mark.AiState, "shoot")) && mark.Vel.Len() < 0.6
	if parked && agent.Personality.Aggression > 0.35 && robot.Remaining > 30 {
		toHub := hub.Center.Sub(mark.Pos).Norm()
		side := Vec2{-toHub.Y, toHub.X}
		if side.Dot(robot.Pos.Sub(mark.Pos)) < 0 {
			side = side.Scale(-1)
		}
		through := mark.Pos.Sub(side.Scale(0.9))
		robot.AiState = "guard"
		agent.Intent = "shoving " + mark.Station + " off its shot"
		agent.target, agent.hasTarget = through, true
		agent.driveTo(robot, world, through, 0, false)
		return
	}
	// Endgame: stay away from opponents on (or lining up for) their tower.
	if endgame && world.nearOwnTower(mark, 2.8) {
		agent.goCollect(robot, world, "deny")
		return
	}
	toHub := hub.Center.Sub(mark.Pos)
	guard := mark.Pos.Add(toHub.Norm().Scale(math.Min(1.1, toHub.Len()*0.5)))
	if guard.Sub(hub.Center).Len() < 1.6 {
		guard = hub.Center.Add(guard.Sub(hub.Center).Norm().Scale(1.6))
	}
	agent.driveTo(robot, world, guard, 0, false)
}

// steal raids the opponent zone for FUEL until full, then brings it home.
func (agent *Agent) steal(robot *Robot, world *World, ctx teleopContext) {
	if robot.Hopper == 0 {
		agent.unloading = false
	}
	if !agent.unloading && robot.Hopper > 0 &&
		(robot.Hopper >= robot.Arch.Capacity*9/10 || agent.nearestBallDistance(robot, world, "steal") > 6) {
		agent.unloading = true
	}
	if agent.unloading {
		agent.unload(robot, world, ctx)
		return
	}
	agent.goCollect(robot, world, "steal")
	if robot.AiState == "collect" {
		robot.AiState = "steal"
		agent.Intent = "raiding the opponent's stockpile"
	}
}

// shuttle collects FUEL in the neutral zone ("neutral") or off the opponent's half of it ("deny") until full, then
// lobs the whole load into this alliance's zone so the scorers have a stockpile.
func (agent *Agent) shuttle(robot *Robot, world *World, area string, ctx teleopContext) {
	if robot.Hopper == 0 {
		agent.unloading = false
	}
	if !agent.unloading && robot.Hopper > 0 &&
		(robot.Hopper >= robot.Arch.Capacity*9/10 || agent.nearestBallDistance(robot, world, area) > 5) {
		agent.unloading = true
	}
	if agent.unloading {
		agent.goPass(robot, world)
		return
	}
	agent.goCollect(robot, world, area)
}

// endgame heads for the tower in time to climb — if climbing is worth more than scoring FUEL to the buzzer (decided
// with the alliance's coach, or by the drive team itself when there is no coach decision).
func (agent *Agent) endgame(robot *Robot, world *World, plan RobotPlan) bool {
	if robot.Arch.ClimbLevel == 0 || robot.Remaining <= 0 {
		return false
	}
	if agent.climbGoal == 0 {
		t, _ := agent.clock.matchTime(world.time)
		remaining := TeleopEndSec - t
		if remaining > climbDecideAt {
			return false
		}
		if agent.climbPlan == 0 {
			// Nobody has decided for us: take the option with the most expected points.
			best := ClimbOption{Level: -1}
			for _, option := range agent.climbOptions(robot, world, remaining) {
				if (option.Feasible || option.Level == 0) && (best.Level < 0 || option.Total() > best.Total()) {
					best = option
				}
			}
			agent.climbPlan = best.Level
			if best.Level == 0 {
				agent.climbPlan = -1
			}
		}
		if agent.climbPlan < 0 {
			return false
		}
		zone, _ := world.climbZone(robot.Alliance, robot.Index%3-1)
		level := min(robot.Arch.ClimbLevel, agent.climbPlan)
		spare := 1.0 + (1-agent.Personality.Risk)*1.5
		if remaining > robot.Arch.ClimbTime*float64(level)+agent.travelTime(robot, zone)+0.8+spare {
			return false // not yet: keep scoring until the last moment
		}
	}
	level := robot.Arch.ClimbLevel
	if agent.climbPlan > 0 {
		level = min(level, agent.climbPlan)
	}
	if agent.climbGoal == 0 {
		agent.climbGoal = level
		// The climb itself can fall short of the plan (the decision already priced that in, from the team's record).
		if agent.rng.Float64() > world.climbReliability(robot.Team, level, agent.Reliability*math.Min(1.2, robot.Skill)) {
			agent.climbGoal = max(1, agent.climbGoal-1-agent.rng.Intn(2))
		}
	}
	agent.goClimb(robot, world, agent.climbGoal)
	return true
}

func (agent *Agent) faceAngle(robot *Robot, angle float64) {
	err := wrapAngle(angle - robot.Heading)
	robot.Ctl.Omega = clamp(err*5, -robot.Arch.MaxOmega, robot.Arch.MaxOmega)
}

func (agent *Agent) nearestBallDistance(robot *Robot, world *World, area string) float64 {
	best := math.Inf(1)
	agent.knownBalls(robot, world, func(p Vec2, weight int) {
		if !agent.inArea(robot, world, area, p) {
			return
		}
		if d := p.Sub(robot.Pos).Len(); d < best {
			best = d
		}
	})
	return best
}

// eStopTipped is the drive team hitting its E-stop once it sees its robot on its side, so the drivetrain and mechanisms
// do not keep running (and grinding) on the carpet. It takes them a moment to react.
func (agent *Agent) eStopTipped(robot *Robot, world *World) {
	if agent.tippedAt == 0 {
		agent.tippedAt = world.time
		return
	}
	if agent.eStopped || world.time-agent.tippedAt < 0.4+agent.Personality.Reaction*3 {
		return
	}
	agent.eStopped = true
	world.logf("warn", "ai", "%d (%s) is tipped over: its drive team pressed the E-stop", robot.Team, robot.Station)
	if world.bus != nil && !world.quiet {
		index := robot.Index
		go func() { // (through the field I/O, like a real button: not under the world lock)
			_, _ = world.bus.Dispatch("field.pulse", []byte(fmt.Sprintf(`{"control":"teamEStop","index":%d,"ms":1500}`, index)))
		}()
	}
}

// nearOwnTower reports whether a robot is on, or within distance of any climbing spot of, its own alliance's tower.
func (world *World) nearOwnTower(robot *Robot, distance float64) bool {
	if robot.onTower() {
		return true
	}
	for slot := -1; slot <= 1; slot++ {
		if zone, _ := world.climbZone(robot.Alliance, slot); robot.Pos.Sub(zone).Len() < distance {
			return true
		}
	}
	return false
}

// markedByTeammate reports whether another robot on this alliance is already guarding an opponent.
func (world *World) markedByTeammate(robot, opponent *Robot) bool {
	for i, other := range world.robots {
		if other == robot || other.Alliance != robot.Alliance || !other.Present {
			continue
		}
		if mate, ok := world.controllers[i].(*Agent); ok && mate.contactTarget == opponent {
			return true
		}
	}
	return false
}

// nearHangingRobot reports whether a floor point is tucked against a robot on the tower (unreachable until it drops).
func (world *World) nearHangingRobot(p Vec2) bool {
	for _, robot := range world.robots {
		if robot.Present && (robot.Climbing || robot.ClimbLevel > 0) && robot.Pos.Sub(p).Len() < robot.half().X+0.35 {
			return true
		}
	}
	return false
}
