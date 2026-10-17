package world

import "math"

// Rule awareness: what a drive team does to stay legal, applied after every decision (the referees in
// officials.go call what slips through).

// obeyRules adjusts the robot's commands so it does not draw an avoidable foul.
func (agent *Agent) obeyRules(robot *Robot, world *World) {
	// G407: only launch into the hub with the bumpers at least partly in the alliance zone.
	if robot.Ctl.Shoot && !world.inAllianceZone(robot) {
		robot.Ctl.Shoot = false
	}
	// G408: keep the intake off under a hub exit while that hub is releasing FUEL (driving through is fine:
	// bulldozing and deflecting are not CONTROL).
	intakeFront := robot.Pos.Add(robot.forward().Scale(robot.half().X + 0.2))
	if robot.Ctl.Intake && (world.inDropZone(robot.Pos, 0.25) || world.inDropZone(intakeFront, 0.2)) {
		robot.Ctl.Intake = false
	}
	// G418: a robot that finds itself holding an opponent (traffic, a shove that went on too long) eases off. The
	// deliberate defender manages its own count in defend().
	if robot.AiState != "guard" && robot.AiState != "release" {
		if victim, count := world.officials.pinningAnyone(robot.Index); victim >= 0 && count > 0.4 {
			away := robot.Pos.Sub(world.robots[victim].Pos).Norm()
			robot.Ctl.Vel = robot.Ctl.Vel.Scale(0.3).Add(away.Scale(2.2))
		}
	}
	// G420: in the endgame, keep clear of opponents on or lining up at their tower (contact awards them LEVEL 3).
	if !robot.Auto && robot.Remaining <= 30 {
		for _, other := range world.robots {
			if !other.Present || other.Alliance == robot.Alliance || !world.nearOwnTower(other, 1.2) {
				continue
			}
			if away := robot.Pos.Sub(other.Pos); gap(robot, other) < 0.7 && robot.Ctl.Vel.Dot(away) < 0 {
				robot.Ctl.Vel = robot.Ctl.Vel.Sub(away.Norm().Scale(robot.Ctl.Vel.Dot(away.Norm()))).Add(away.Norm().Scale(1.2))
			}
		}
	}
	// G403: in AUTO the bumpers may not end up completely across the center line.
	if robot.Auto {
		center := world.layout.Field.Length / 2
		extent := robot.xExtent()
		toward := 1.0 // the direction of the center line from this alliance's side
		if robot.Alliance == Red {
			toward = -1
		}
		// How far the trailing bumper edge is from the center line (positive: still on our side).
		margin := (center-robot.Pos.X)*toward + extent
		if margin < 0.25 && robot.Ctl.Vel.X*toward > 0 {
			robot.Ctl.Vel.X = -toward * 0.6
		}
	}
}

// inDropZone reports whether a floor point is in front of a hub exit that is releasing FUEL (the hub holds FUEL and
// its motors run).
func (world *World) inDropZone(p Vec2, margin float64) bool {
	for _, hub := range world.hubs {
		releasing := hub.Stored > 0 && hub.MotorOn
		if !releasing && world.time-hub.lastRelease > 1.5 {
			continue
		}
		out := (p.X - hub.Center.X) * hub.neutralDir
		if out > hub.size/2-margin && out < hub.size/2+1.1+margin && math.Abs(p.Y-hub.Center.Y) < hub.size/2+0.2+margin {
			return true
		}
	}
	return false
}

// autoReach reports whether FUEL at p can be collected in AUTO without the bumpers completely crossing the center
// line (an intake reaching a little past it is fine).
func (world *World) autoReach(robot *Robot, p Vec2) bool {
	center := world.layout.Field.Length / 2
	if robot.Alliance == Red {
		return p.X > center-robot.half().X*0.6
	}
	return p.X < center+robot.half().X*0.6
}

// atOutpost reports whether the robot is lined up at its outpost floor opening (delivering).
func (agent *Agent) atOutpost(robot *Robot, world *World) bool {
	outpost := world.outposts[robot.Alliance]
	return robot.Pos.Sub(Vec2{outpost.wallX, outpost.chuteY}).Len() < robot.half().X+0.6
}

// pushToOutpost bulldozes a nearby pile of FUEL along the carpet into this alliance's outpost floor opening
// (G425 B lets FUEL leave the field there), so the human player has a stockpile without a robot carrying it.
func (agent *Agent) pushToOutpost(robot *Robot, world *World) {
	robot.AiState = "push-outpost"
	agent.Intent = "slamming loose FUEL into the outpost for the human player"
	outpost := world.outposts[robot.Alliance]
	opening := Vec2{outpost.wallX + outpost.inward*0.05, outpost.chuteY}
	pile, ok := agent.outpostPile(robot, world)
	if !ok {
		agent.goCollect(robot, world, "own")
		return
	}
	toOpening := opening.Sub(pile).Norm()
	heading := math.Atan2(toOpening.Y, toOpening.X)
	behind := pile.Sub(toOpening.Scale(robot.half().X + 0.5))
	// Get behind the pile (facing the opening), then drive it in with the bumper face; the intake stays off so the
	// FUEL is pushed rather than picked up.
	robot.Ctl.Intake = false
	if robot.Pos.Sub(behind).Len() > 0.35 && (robot.Pos.Sub(opening)).Len() > pile.Sub(opening).Len() {
		agent.target, agent.hasTarget = behind, true
		agent.driveTo(robot, world, behind, heading, true)
		return
	}
	// Drive the bumper right into the wall: the FUEL in front has nowhere to go but through the floor opening.
	stop := opening.Sub(toOpening.Scale(robot.half().X + 0.01))
	agent.target, agent.hasTarget = stop, true
	agent.driveTo(robot, world, stop, heading, true)
}

// turretRun unloads a turret robot on the move: it strafes back and forth across its alliance zone at shooting
// range, firing whenever a shot would count (shots lead the robot's own motion), so it never sits still for a
// defender to pin or shove.
func (agent *Agent) turretRun(robot *Robot, world *World) {
	hub := world.hubs[robot.Alliance]
	spot := agent.shootSpot(robot, world)
	lateral := 1.3
	ends := [2]Vec2{
		{spot.X, clamp(hub.Center.Y-lateral, 1.0, world.layout.Field.Width-1.0)},
		{spot.X, clamp(hub.Center.Y+lateral, 1.0, world.layout.Field.Width-1.0)},
	}
	goal := ends[agent.strafeLeg%2]
	if robot.Pos.Sub(goal).Len() < 0.45 {
		agent.strafeLeg++
		goal = ends[agent.strafeLeg%2]
	}
	agent.target, agent.hasTarget = goal, true
	agent.driveTo(robot, world, goal, 0, false)
	// Shooting on the move: slow enough to stay accurate.
	if speed := robot.Ctl.Vel.Len(); speed > 2.4 {
		robot.Ctl.Vel = robot.Ctl.Vel.Scale(2.4 / speed)
	}
	robot.Ctl.Intake = robot.Hopper < robot.Arch.Capacity
	robot.AiState = "shoot-move"
	agent.Intent = "shooting on the move"
	if fire, timing := agent.shotTiming(robot, world); fire && world.inAllianceZone(robot) {
		distance := robot.Pos.Sub(hub.Center).Len()
		if distance > 1.4 && distance < robot.Arch.MaxRange*0.9 {
			robot.Ctl.Shoot = true
			robot.Ctl.Timing = timing
		}
		return
	}
	robot.AiState = "shoot-wait"
	agent.Intent = "strafing, waiting for the hub"
}

// Loose FUEL in front of the outpost worth bulldozing in: at least this many within reach of the floor opening.
const (
	outpostPileMin   = 4
	outpostPileReach = 2.5 // m from the floor opening
)

// outpostPile finds the pile of loose FUEL in front of this alliance's outpost floor opening (the densest 0.6 m cell
// the robot knows of, nearer the opening counting more), if there is one worth pushing in.
func (agent *Agent) outpostPile(robot *Robot, world *World) (Vec2, bool) {
	outpost := world.outposts[robot.Alliance]
	opening := Vec2{outpost.wallX + outpost.inward*0.05, outpost.chuteY}
	density := map[[2]int]int{}
	total := 0
	var best [2]int
	bestScore := 0.0
	agent.knownBalls(robot, world, func(p Vec2, weight int) {
		distance := p.Sub(opening).Len()
		if distance > outpostPileReach || distance < 0.3 {
			return
		}
		total += weight
		cell := [2]int{int(p.X / 0.6), int(p.Y / 0.6)}
		density[cell] += weight
		// (ties broken by cell, so the same seed always picks the same pile)
		score := float64(density[cell]) - distance*0.6
		if score > bestScore || (score == bestScore && (cell[0] < best[0] || (cell[0] == best[0] && cell[1] < best[1]))) {
			best, bestScore = cell, score
		}
	})
	if total < outpostPileMin || bestScore < 1.5 {
		return Vec2{}, false
	}
	return Vec2{(float64(best[0]) + 0.5) * 0.6, (float64(best[1]) + 0.5) * 0.6}, true
}

// outpostHabit is how much a team goes for FUEL at its outpost: only some teams make a habit of it.
func outpostHabit(team int) float64 {
	if hashUnit(team, 0x0b57) < 0.35 {
		return 1
	}
	return 0.25
}
