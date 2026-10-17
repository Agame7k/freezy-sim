package world

import (
	"math"
)

// Between matches, as at an event: once the field is safe the drive teams cart their robots off through the gates,
// the field reset crew walks the field collecting FUEL into buckets and restages it (depots, chutes, the
// neutral-zone box), and the next match's drive teams cart their robots in to their starting positions. The arena
// clock does not wait, so the crew works at a realistic but brisk pace (about a minute for a full reset).

// Robot cart states.
const (
	CartNone = iota // on the field
	CartOut         // being wheeled off to a gate
	CartOff         // off the field (in queue / the pits)
	CartIn          // being wheeled in to its starting position
)

// BallCrew is FUEL in a field reset volunteer's bucket.
const BallCrew = 6

const (
	crewSize      = 6
	crewSpeed     = 1.5  // m/s walking the field
	crewPickRate  = 6.0  // FUEL per second scooped into a bucket
	crewPlaceRate = 8.0  // FUEL per second set out on the staging marks
	crewBucket    = 28   // FUEL per bucket
	crewReach     = 0.75 // m from the volunteer to FUEL they can scoop
	cartSpeed     = 0.7  // m/s pushing a robot cart
	cartInAfter   = 25.0 // s into a reset the next teams roll in even if the crew is still working
	cartHeight    = 0.32 // robots ride on carts this high
	crewStartSec  = 5.0  // the crew steps on once the robots are moving off
)

// Volunteer is one field reset crew member.
type Volunteer struct {
	Pos    Vec2
	Mode   int // crew modes below
	Bucket int
	goal   Vec2
	work   float64
}

const (
	crewEntering = iota
	crewCollecting
	crewPlacing
	crewLeaving
)

type fieldReset struct {
	started   float64
	crewIn    bool
	targets   []Vec2 // staging marks (depots and the neutral box) not yet filled
	pending   map[*Ball]bool
	preloads  int
	volunteer []*Volunteer
	done      bool
}

// The between-match sequence follows the head referee's field signals, as at an event:
//   - purple, FIELD CLEANUP (FieldCleanup): the reset crew comes on and restages FUEL; robots stay where they stopped.
//   - green, FIELD SAFE (FieldSafe): drive teams come on and take their robots off.
//   - the next match loads (FieldReset): the next teams bring their robots on. Whatever the signals did not start
//     (nobody signalled, or a match was loaded straight away) starts here.
// The AI benchmark and tests use the instant Reset.

// FieldCleanup is the purple field-cleanup signal: the reset crew comes on and restages FUEL.
func (world *World) FieldCleanup() {
	world.mu.Lock()
	defer world.mu.Unlock()
	if world.playing() {
		return
	}
	world.startCrewLocked()
}

// FieldSafe is the green field-safe signal: drive teams take their robots off (the crew starts too, if it has not).
// The robots wait off the field until the next match is loaded.
func (world *World) FieldSafe() {
	world.mu.Lock()
	defer world.mu.Unlock()
	if world.playing() {
		return
	}
	world.startCrewLocked()
	if world.cartOffLocked() {
		world.awaitingMatch = true
	}
}

// FieldReset is a match load: the next teams bring their robots on, after anything the signals did not start.
func (world *World) FieldReset() {
	world.mu.Lock()
	defer world.mu.Unlock()
	present := false
	for _, robot := range world.robots {
		present = present || robot.Present
	}
	if !present && world.crew == nil {
		world.resetLocked() // nothing on the field yet (startup): just stage it
		return
	}
	world.startCrewLocked()
	world.cartOffLocked()
	world.awaitingMatch = false
	// Game plans for the teams now in the stations (the crew may have started before this match was loaded).
	world.planAlliance(Red)
	world.planAlliance(Blue)
}

// playing reports whether a match is running (any robot enabled).
func (world *World) playing() bool {
	for _, robot := range world.robots {
		if robot.Enabled {
			return true
		}
	}
	return false
}

// startCrewLocked sends the reset crew on, once per break between matches.
func (world *World) startCrewLocked() {
	if world.crew != nil || world.crewCalled {
		return
	}
	world.crewCalled = true
	world.resetMatchStateKeepPoses()
	world.crew = &fieldReset{started: world.time, pending: map[*Ball]bool{}}
	world.logf("info", "world", "Field cleanup: the crew is restaging FUEL")
}

// cartOffLocked has the drive teams take their robots off, once per break; it reports whether any went.
func (world *World) cartOffLocked() bool {
	if world.cartedOff {
		return false
	}
	world.cartedOff = true
	any := false
	for _, robot := range world.robots {
		if robot.Present && robot.Cart == CartNone {
			world.startCart(robot, CartOut)
			any = true
		}
	}
	if any {
		world.logf("info", "world", "Field safe: drive teams are carting their robots off")
	}
	return any
}

// resetMatchStateKeepPoses clears match state but leaves robots where they are (they are carted off).
func (world *World) resetMatchStateKeepPoses() {
	poses := make([][2]any, len(world.robots))
	for i, robot := range world.robots {
		poses[i] = [2]any{robot.Pos, robot.Heading}
	}
	world.resetMatchState()
	for i, robot := range world.robots {
		robot.Pos, robot.Heading = poses[i][0].(Vec2), poses[i][1].(float64)
	}
}

// gatePath is the route from a point on the field out through this robot's nearest gate (blue frame gates at
// x = 2.5 m and 14.04 m on both long sides).
func (world *World) gatePath(robot *Robot, from Vec2) []Vec2 {
	l := world.layout
	gateX := 2.5
	if robot.Alliance == Red {
		gateX = l.Field.Length - 2.5
	}
	inside, outside := 0.75, -1.8
	if from.Y > l.Field.Width/2 {
		inside, outside = l.Field.Width-0.75, l.Field.Width+1.8
	}
	return []Vec2{from, {gateX, inside}, {gateX, outside}}
}

func (world *World) startCart(robot *Robot, mode int) {
	robot.Cart, robot.cartT = mode, 0
	robot.Vel, robot.Omega = Vec2{}, 0
	if mode == CartOut {
		robot.cartPath = world.gatePath(robot, robot.Pos)
	} else {
		start, heading := world.startPoseFor(robot)
		path := world.gatePath(robot, start)
		robot.cartPath = []Vec2{path[2], path[1], path[0]}
		robot.cartHeading = heading
		robot.Pos = path[2]
	}
}

// pathPoint walks distance d along a polyline; done reports reaching the end.
func pathPoint(path []Vec2, d float64) (Vec2, float64, bool) {
	d = math.Max(0, d)
	for i := 0; i+1 < len(path); i++ {
		segment := path[i+1].Sub(path[i])
		length := segment.Len()
		if d <= length {
			return path[i].Add(segment.Scale(d / math.Max(length, 1e-6))), math.Atan2(segment.Y, segment.X), false
		}
		d -= length
	}
	last := path[len(path)-1]
	heading := 0.0
	if len(path) > 1 {
		segment := last.Sub(path[len(path)-2])
		heading = math.Atan2(segment.Y, segment.X)
	}
	return last, heading, true
}

// stepFieldReset runs the carts and the crew (called every physics step).
func (world *World) stepFieldReset(dt float64) {
	if world.playing() {
		world.crewCalled, world.cartedOff, world.awaitingMatch = false, false, false // a match: the next break starts fresh
	}
	// A match starting mid-reset (somebody pressed Start): finish everything instantly.
	for _, robot := range world.robots {
		if robot.Enabled && (robot.Cart != CartNone || world.crew != nil) {
			world.finishFieldReset()
			return
		}
	}
	allOff := true
	for _, robot := range world.robots {
		switch robot.Cart {
		case CartOut, CartIn:
			robot.cartT += dt * cartSpeed
			p, heading, done := pathPoint(robot.cartPath, robot.cartT)
			robot.Pos = p
			robot.Z = cartHeight
			if robot.Cart == CartOut {
				robot.Heading = heading
			} else {
				robot.Heading += wrapAngle(robot.cartHeading-robot.Heading) * math.Min(1, dt*2)
			}
			if done {
				if robot.Cart == CartOut {
					robot.Cart = CartOff
					world.dumpHopper(robot)
				} else {
					robot.Cart, robot.Z, robot.Heading = CartNone, 0, robot.cartHeading
					world.preload(robot)
				}
			}
		}
		if robot.Cart == CartOut {
			allOff = false
		}
	}
	// Robots leave before the next teams bring theirs on, once the crew is done (or has had a fair head start); the
	// teams arrive a few seconds apart.
	ready := (world.crew == nil || world.time-world.crew.started > cartInAfter) && !world.awaitingMatch
	if allOff && ready {
		for _, robot := range world.robots {
			if robot.Cart == CartOff {
				world.applyPendingTeam(robot)
				if robot.Present {
					world.startCart(robot, CartIn)
					robot.cartT = -world.rng.Float64() * 6 * cartSpeed // waiting their turn at the gate
				}
			}
		}
	}
	// Robots that rolled in before the staff had FUEL to hand get the rest of their preload as soon as there is some.
	if world.steps%30 == 0 {
		for _, robot := range world.robots {
			if robot.preloadOwed && robot.onField() && !robot.Enabled {
				world.preload(robot)
			}
		}
	}
	crew := world.crew
	if crew == nil {
		// Carts done after the crew left: whatever FUEL the teams handed over goes back in the neutral box (once every
		// robot has its preload).
		carting := false
		for _, robot := range world.robots {
			carting = carting || (robot.Present && robot.Cart != CartNone) || (robot.preloadOwed && robot.onField())
		}
		if !carting && world.steps%120 == 0 {
			for _, ball := range world.balls {
				if ball.State == BallOut && ball.outAt > world.time+1000 {
					world.restageLoose(ball)
				}
			}
		}
		return
	}
	if !crew.crewIn && world.time-crew.started > crewStartSec {
		world.crewEnters(crew)
	}
	if crew.crewIn {
		world.stepCrew(crew, dt)
	}
}

// crewEnters plans the restage: the human players top up the chutes from their corrals and the hub, robots'
// FUEL went into buckets with them, and every loose FUEL on the carpet is collected and set out on the marks.
func (world *World) crewEnters(crew *fieldReset) {
	crew.crewIn = true
	for _, target := range world.stagingTargets() {
		if target.state == BallFree {
			crew.targets = append(crew.targets, Vec2{target.p[0], target.p[1]})
		}
	}
	// FUEL already sitting on a mark stays.
	for _, ball := range world.balls {
		if ball.State != BallFree {
			continue
		}
		// FUEL already inside a staging area (the neutral box or a depot) counts toward it: it uses up the
		// nearest empty mark there instead of being picked up and put back down.
		p := Vec2{ball.P[0], ball.P[1]}
		bestIndex, best := -1, math.Inf(1)
		for i, mark := range crew.targets {
			if d := mark.Sub(p).Len(); d < best {
				bestIndex, best = i, d
			}
		}
		if bestIndex >= 0 && (best < 0.1 || world.inStagingArea(p)) {
			crew.targets = append(crew.targets[:bestIndex], crew.targets[bestIndex+1:]...)
			continue
		}
		crew.pending[ball] = true
	}
	// Chutes: the human players reload 24 each from what is off the carpet (corrals, the hubs).
	for _, outpost := range world.outposts {
		need := world.layout.Outpost.ChuteCapacity - outpost.chuteCount(world)
		for _, ball := range world.balls {
			if need <= 0 {
				break
			}
			if ball.State == BallCorral || ball.State == BallHub || ball.State == BallOut {
				ball.State, ball.Owner, ball.hubPhase = BallChute, outpost.Alliance, 0
				need--
			}
		}
		outpost.Corral = 0
	}
	// Whatever else is off the carpet goes to the crew's buckets.
	gates := []Vec2{{2.5, -1.2}, {world.layout.Field.Length - 2.5, -1.2}, {2.5, world.layout.Field.Width + 1.2},
		{world.layout.Field.Length - 2.5, world.layout.Field.Width + 1.2}}
	for i := 0; i < crewSize; i++ {
		crew.volunteer = append(crew.volunteer, &Volunteer{Pos: gates[i%len(gates)], Mode: crewCollecting})
	}
	k := 0
	for _, ball := range world.balls {
		if ball.State == BallCorral || ball.State == BallHub || ball.State == BallOut {
			v := crew.volunteer[k%crewSize]
			ball.State, ball.Owner = BallCrew, k%crewSize
			v.Bucket++
			k++
		}
	}
	for _, hub := range world.hubs {
		hub.Stored = 0
	}
}

func (world *World) stepCrew(crew *fieldReset, dt float64) {
	busy := false
	for index, v := range crew.volunteer {
		switch v.Mode {
		case crewCollecting:
			busy = true
			// Scoop up to a bucketful, then set it out; when the carpet is clear, empty what is left.
			// (In ball order, not map order: the simulation must play the same way from the same seed.)
			nearest, best := (*Ball)(nil), math.Inf(1)
			for _, ball := range world.balls {
				if !crew.pending[ball] {
					continue
				}
				if d := (Vec2{ball.P[0], ball.P[1]}).Sub(v.Pos).Len(); d < best {
					nearest, best = ball, d
				}
			}
			if nearest == nil || v.Bucket >= crewBucket {
				if v.Bucket > 0 && len(crew.targets) > 0 {
					v.Mode = crewPlacing
				} else if nearest == nil {
					v.Mode = crewLeaving
				}
				break
			}
			if best > crewReach {
				v.Pos = v.Pos.Add(Vec2{nearest.P[0], nearest.P[1]}.Sub(v.Pos).Norm().Scale(math.Min(best, crewSpeed*dt)))
				break
			}
			v.work += dt * crewPickRate
			for v.work >= 1 && v.Bucket < crewBucket {
				v.work--
				picked := false
				for _, ball := range world.balls {
					if !crew.pending[ball] {
						continue
					}
					if (Vec2{ball.P[0], ball.P[1]}).Sub(v.Pos).Len() <= crewReach {
						delete(crew.pending, ball)
						ball.State, ball.Owner, ball.V = BallCrew, index, [3]float64{}
						v.Bucket++
						picked = true
						break
					}
				}
				if !picked {
					break
				}
			}
		case crewPlacing:
			busy = true
			if v.Bucket == 0 || len(crew.targets) == 0 {
				v.Mode = crewCollecting
				break
			}
			// The nearest empty mark.
			bestIndex, best := 0, math.Inf(1)
			for i, mark := range crew.targets {
				if d := mark.Sub(v.Pos).Len(); d < best {
					bestIndex, best = i, d
				}
			}
			mark := crew.targets[bestIndex]
			if best > 0.5 {
				v.Pos = v.Pos.Add(mark.Sub(v.Pos).Norm().Scale(math.Min(best, crewSpeed*dt)))
				break
			}
			v.work += dt * crewPlaceRate
			for v.work >= 1 && v.Bucket > 0 && len(crew.targets) > 0 {
				v.work--
				ball := world.crewBall(index)
				if ball == nil {
					v.Bucket = 0
					break
				}
				// Set out the next mark within arm's reach (marks are placed in tidy rows).
				bestIndex, best = 0, math.Inf(1)
				for i, mark := range crew.targets {
					if d := mark.Sub(v.Pos).Len(); d < best {
						bestIndex, best = i, d
					}
				}
				if best > 0.9 {
					break
				}
				mark := crew.targets[bestIndex]
				crew.targets = append(crew.targets[:bestIndex], crew.targets[bestIndex+1:]...)
				ball.State, ball.Owner = BallFree, 0
				ball.P = [3]float64{mark.X, mark.Y, world.ballRadius}
				ball.V = [3]float64{}
				ball.sleep = sleepFrames
				ball.shooter, ball.thrower, ball.fromHub = 0, 0, false
				v.Bucket--
			}
		case crewLeaving:
			if v.Bucket > 0 && len(crew.targets) > 0 {
				v.Mode = crewPlacing
				busy = true
				break
			}
			exit := Vec2{v.Pos.X, -1.6}
			if v.Pos.Y > world.layout.Field.Width/2 {
				exit.Y = world.layout.Field.Width + 1.6
			}
			if d := exit.Sub(v.Pos).Len(); d > 0.1 {
				v.Pos = v.Pos.Add(exit.Sub(v.Pos).Norm().Scale(math.Min(d, crewSpeed*dt)))
				busy = true
			}
		}
	}
	if !busy {
		// Anything left in a bucket (no marks left) is the preload supply for robots still to arrive (or owed FUEL);
		// the rest goes in the neutral box loose. Then the crew is done.
		reserve := 0
		for _, robot := range world.robots {
			if robot.Present && (robot.Cart != CartNone || robot.preloadOwed) {
				reserve += world.layout.Fuel.PreloadPerRobot - robot.Hopper // (carted robots handed theirs over: 8)
			}
		}
		for _, ball := range world.balls {
			if ball.State == BallCrew || (ball.State == BallOut && ball.outAt > world.time+1000) {
				if reserve > 0 {
					ball.State, ball.Owner, ball.outAt = BallOut, 0, world.time+1e6
					reserve--
					continue
				}
				world.restageLoose(ball)
			}
		}
		world.logf("info", "world", "Field cleanup done in %.0f s: FUEL restaged, crew off the field", world.time-crew.started)
		world.crew = nil
	}
}

func (world *World) crewBall(volunteer int) *Ball {
	for _, ball := range world.balls {
		if ball.State == BallCrew && ball.Owner == volunteer {
			return ball
		}
	}
	return nil
}

func (world *World) restageLoose(ball *Ball) {
	box := world.layout.Fuel.NeutralBox
	ball.State, ball.Owner = BallFree, 0
	ball.P = [3]float64{box.CenterX + (world.rng.Float64()-0.5)*box.LengthX, box.CenterY + (world.rng.Float64()-0.5)*box.WidthY,
		world.ballRadius}
	ball.V = [3]float64{}
	ball.sleep = 0
}

// dumpHopper hands a carted-off robot's FUEL to the field staff (R204: FUEL stays at the field).
func (world *World) dumpHopper(robot *Robot) {
	k := 0
	for _, ball := range world.balls {
		if ball.State != BallHeld || ball.Owner != robot.Index {
			continue
		}
		if crew := world.crew; crew != nil && crew.crewIn && len(crew.volunteer) > 0 {
			volunteer := k % len(crew.volunteer)
			ball.State, ball.Owner = BallCrew, volunteer // straight into a crew bucket
			crew.volunteer[volunteer].Bucket++
			if crew.volunteer[volunteer].Mode == crewLeaving {
				crew.volunteer[volunteer].Mode = crewPlacing
			}
			k++
			continue
		}
		ball.State, ball.Owner = BallOut, 0
		ball.outAt = world.time + 1e6 // handed over, not lost: the crew restages it
	}
	robot.Hopper = 0
}

// preload loads a robot that just arrived on the field with its 8 FUEL (from the staff's supply). If the staff have
// none to hand yet (it is all still on the carpet), the robot is owed the rest and topped up as the crew collects.
func (world *World) preload(robot *Robot) {
	need := world.layout.Fuel.PreloadPerRobot - robot.Hopper
	defer func() { robot.preloadOwed = robot.Hopper < world.layout.Fuel.PreloadPerRobot }()
	for _, state := range []int{BallOut, BallCrew, BallCorral} {
		for _, ball := range world.balls {
			if need <= 0 {
				return
			}
			if ball.State == state {
				if state == BallCrew {
					if crew := world.crew; crew != nil && ball.Owner < len(crew.volunteer) {
						crew.volunteer[ball.Owner].Bucket--
					}
				}
				ball.State, ball.Owner = BallHeld, robot.Index
				robot.loadHopper(ball, robot.Hopper)
				robot.Hopper++
				need--
			}
		}
	}
}

// applyPendingTeam swaps in the team whose driver station connected while the old robot was being carted off.
func (world *World) applyPendingTeam(robot *Robot) {
	if robot.pendingTeam == 0 || robot.pendingTeam == robot.Team {
		robot.pendingTeam = 0
		return
	}
	world.assignTeam(robot, robot.pendingTeam)
	robot.pendingTeam = 0
}

// finishFieldReset completes everything at once (a match is starting).
func (world *World) finishFieldReset() {
	for _, robot := range world.robots {
		if robot.Cart != CartNone {
			world.applyPendingTeam(robot)
			robot.Pos, robot.Heading = world.startPoseFor(robot)
			robot.Cart, robot.Z = CartNone, 0
			world.dumpHopper(robot)
		}
	}
	if crew := world.crew; crew != nil {
		marks := crew.targets
		for _, ball := range world.balls {
			if ball.State != BallCrew && !(ball.State == BallFree && crew.pending[ball]) && ball.State != BallOut {
				continue
			}
			if len(marks) > 0 {
				ball.State, ball.Owner = BallFree, 0
				ball.P = [3]float64{marks[0].X, marks[0].Y, world.ballRadius}
				ball.sleep = sleepFrames
				marks = marks[1:]
			} else {
				world.restageLoose(ball)
			}
		}
		world.crew = nil
	}
	for _, robot := range world.robots {
		if robot.Present && robot.Hopper < world.layout.Fuel.PreloadPerRobot {
			world.preload(robot)
		}
	}
	world.logf("warn", "world", "Match started during the field reset: finished it instantly")
}

// FieldResetBusy reports whether robots are still being carted or the crew is still on the field.
func (world *World) FieldResetBusy() (carting, crew bool) {
	world.mu.Lock()
	defer world.mu.Unlock()
	for _, robot := range world.robots {
		if robot.Present && robot.Cart != CartNone {
			carting = true
		}
	}
	return carting, world.crew != nil
}

// Volunteers returns the crew on the field (for the snapshot).
func (world *World) volunteers() []*Volunteer {
	if world.crew == nil || !world.crew.crewIn {
		return nil
	}
	return world.crew.volunteer
}

// assignTeam puts a team's robot (its own design and drive team) in a station.
func (world *World) assignTeam(robot *Robot, team int) {
	robot.Arch = world.designForLocked(team)
	if skill, ok := world.teamSkill[team]; ok {
		robot.Skill = skill
	} else {
		robot.Skill = world.effectiveSkill(team, skillForTeam(team))
	}
	robot.Team = team
}

// onField reports whether the robot is on the carpet and playing (not on a cart between matches).
func (robot *Robot) onField() bool { return robot.Present && robot.Cart == CartNone }

// inStagingArea reports whether a floor point is inside the neutral-zone box or a depot (where FUEL starts).
func (world *World) inStagingArea(p Vec2) bool {
	l := world.layout
	box := l.Fuel.NeutralBox
	if math.Abs(p.X-box.CenterX) <= box.LengthX/2+0.05 && math.Abs(p.Y-box.CenterY) <= box.WidthY/2+0.05 {
		return true
	}
	for _, alliance := range []int{Red, Blue} {
		x, y := l.onAlliance(alliance, l.Depot.Depth/2, l.Depot.CenterY["blue"])
		if math.Abs(p.X-x) <= l.Depot.Depth/2+0.05 && math.Abs(p.Y-y) <= l.Depot.Width/2+0.05 {
			return true
		}
	}
	return false
}
