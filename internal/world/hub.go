package world

import (
	"math"
)

const (
	sensorTransit = 0.7  // seconds from the opening to a lane's beam break (measured: TestHubCountsAndReleasesFuel)
	exitPeriod    = 0.18 // seconds between balls per exit while the motors run
)

// Hub is one alliance's scoring element: a sensor array under the opening and four motorized exits.
type Hub struct {
	Alliance   int
	Center     Vec2
	neutralDir float64 // +1 if the neutral zone is toward +x
	size       float64
	bodyHeight float64 // top of the roof (the solid part FUEL bounces off)
	openingZ   float64
	radius     float64

	Stored      int
	lastRelease float64 // world time FUEL last left an exit (still falling for a moment after)
	Sensed      [4]int
	MotorOn     bool
	LightOn     bool
	exitTimers  [4]float64
	// The beam-break sensor in each lane: blocked right now, and since when (a FUEL breaking it is counted).
	beamBlocked [4]bool
	throatZ     float64 // bottom of the clear hopper (the roof), where the throat starts
	baseZ       float64 // top of the vertical side panels
	throatR     float64
}

func newHub(layout *Layout, alliance int) *Hub {
	position := layout.Hub.Positions["blue"]
	x, y := layout.onAlliance(alliance, position[0], position[1])
	direction := 1.0
	if alliance == Red {
		direction = -1
	}
	return &Hub{
		Alliance: alliance, Center: Vec2{x, y}, neutralDir: direction, size: layout.Hub.Size,
		bodyHeight: layout.Hub.RoofHeight, openingZ: layout.Hub.OpeningHeight,
		radius:  layout.Hub.OpeningDiameter / 2,
		throatZ: layout.Hub.RoofHeight, baseZ: layout.Hub.BodyHeight, throatR: layout.Hub.ThroatDiameter / 2,
	}
}

// aimPoint is where shooters aim (the middle of the opening, slightly past its front edge).
func (hub *Hub) aimPoint() Vec2 {
	return hub.Center.Add(Vec2{hub.neutralDir * 0.05, 0})
}

func (world *World) hubOpeningZ() float64 {
	return world.layout.Hub.OpeningHeight
}

// interact handles a free ball near the hub. Returns true if the hub consumed the ball.
func (hub *Hub) interact(world *World, ball *Ball, r float64) bool {
	dx := ball.P[0] - hub.Center.X
	dy := ball.P[1] - hub.Center.Y
	if math.Abs(dx) > hub.size || math.Abs(dy) > hub.size {
		return false
	}
	radial := math.Hypot(dx, dy)
	z := ball.P[2]
	// In through the opening: from here on the FUEL falls through the hub for real (hubflow.go) and is counted when it
	// breaks a lane's beam.
	// The opening's rim is a thin edge: a FUEL that clips it bounces off it like any edge (in or out).
	if d := math.Hypot(radial-hub.radius, z-hub.openingZ); d < r && d > 1e-9 && radial > 1e-9 {
		nr, nz := (radial-hub.radius)/d, (z-hub.openingZ)/d
		ball.P[0] += dx / radial * nr * (r - d)
		ball.P[1] += dy / radial * nr * (r - d)
		ball.P[2] += nz * (r - d)
		vr := (ball.V[0]*dx + ball.V[1]*dy) / radial
		if into := vr*nr + ball.V[2]*nz; into < 0 {
			ball.V[0] -= (1 + wallBounce) * into * nr * dx / radial
			ball.V[1] -= (1 + wallBounce) * into * nr * dy / radial
			ball.V[2] -= (1 + wallBounce) * into * nz
		}
		dx, dy = ball.P[0]-hub.Center.X, ball.P[1]-hub.Center.Y
		radial, z = math.Hypot(dx, dy), ball.P[2]
	}
	if radial < hub.radius-0.01 && z > hub.throatZ && z < hub.openingZ && ball.V[2] < 0 {
		ball.State = BallHub
		ball.Owner = hub.Alliance
		ball.hubPhase = 0 // not counted yet
		ball.sensor = -1
		world.stats.Entered[hub.Alliance]++
		if ball.shooter > 0 && ball.shooter <= len(world.robots) {
			world.robots[ball.shooter-1].Scored++
			if ball.autoShot {
				world.robots[ball.shooter-1].AutoScored++
			}
		}
		if ball.thrower > 0 {
			world.outposts[ball.thrower-1].Made++
			ball.thrower = 0
		}
		return true
	}
	// The outside of the clear hopper: a funnel shell widening from the throat at the roof up to the opening. A miss
	// that hits it bounces off and rolls down onto the roof.
	if z > hub.throatZ && z < hub.openingZ && radial >= hub.radius-0.01 {
		k := (z - hub.throatZ) / (hub.openingZ - hub.throatZ)
		shell := hub.throatR + (hub.radius-hub.throatR)*k
		if radial < shell+r && radial > 1e-9 {
			nx, ny := dx/radial, dy/radial
			ball.P[0] = hub.Center.X + nx*(shell+r)
			ball.P[1] = hub.Center.Y + ny*(shell+r)
			if into := ball.V[0]*nx + ball.V[1]*ny; into < 0 {
				ball.V[0] -= (1 + wallBounce) * into * nx
				ball.V[1] -= (1 + wallBounce) * into * ny
			}
		}
		return false
	}
	// Back net on the neutral-zone side: blocks shots from the wrong side. It rises from the roof to 120 in and leans
	// out to 10.26 in past the face at the top (GE-26300 sheet 4).
	net := world.layout.Hub.Net
	netX := hub.Center.X + hub.neutralDir*hub.size/2
	if z > net.MeshBottom {
		netX += hub.neutralDir * net.Overhang * math.Min(1, (z-net.MeshBottom)/(net.Top-net.MeshBottom))
	}
	if math.Abs(dy) < net.Width/2 && z > hub.bodyHeight && z < net.Top {
		side := (ball.P[0] - netX) * hub.neutralDir
		if side > -r && side < r {
			if ball.V[0]*hub.neutralDir < 0 {
				ball.P[0] = netX + hub.neutralDir*r
				ball.V[0] = -ball.V[0] * 0.15
			} else {
				ball.P[0] = netX - hub.neutralDir*r
				ball.V[0] = -ball.V[0] * 0.15
			}
			ball.V[1] *= 0.3
		}
	}
	return false
}

// laneOffset is a lane's lateral offset from the hub center in field coordinates.
func (hub *Hub) laneOffset(world *World, lane int) float64 {
	lanes := world.layout.Hub.Exit.Lanes
	if lane < 0 || lane >= len(lanes) {
		return 0
	}
	return lanes[lane] * hub.neutralDir
}

// Outpost is an alliance's chute (human-player fed) and corral (where robots drop FUEL for the human player).
type Outpost struct {
	Alliance  int
	wallX     float64
	inward    float64 // +1 if the field is toward +x
	chuteY    float64
	floorHalf float64
	floorTop  float64
	chuteZ    float64

	DoorOpen      bool
	holdOpenUntil float64
	Corral        int
	HumanMode     string  // "auto" (decide), "feed" (load chute from corral), "score" (throw at hub), "idle"
	Accuracy      float64 // human player throwing skill, 0–1
	releaseTime   float64
	hpTimer       float64
	robotComing   float64 // world time a robot last headed for (or sat at) the chute
	Throws        int     // human player shots at the hub this match
	Made          int     // ... that went in
	lastThrow     float64 // world time of the last throw (for the 3D throw animation)
}

const (
	humanReleaseZ = 2.25 // overhand release above the alliance wall
	// CorralCapacity is about how much FUEL the 35.8 x 37.6 in corral holds; with the chute also full the human
	// player must enter the excess onto the field right away (G427).
	CorralCapacity    = 40
	corralStockTarget = 24   // robots keep the human player stocked up to about here
	humanStandBack    = 0.3  // metres behind the wall
	humanFlight       = 1.35 // seconds a throw takes to drop into the hub (for timing)
	humanThrowTime    = 0.75 // fastest time between throws (grab, wind up, release)
	humanFeedTime     = 0.45 // fastest time to drop one corral FUEL into the chute
	humanChuteReady   = 3    // FUEL the human player keeps in the chute before throwing
)

// Throw scatter (fraction of speed / radians) for a human player of a given accuracy: ~80 % of throws go in at the
// default 0.8, every throw at 1.0, ~40 % at 0.6.
const (
	humanSpreadBase  = 0.004
	humanSpreadScale = 0.15
)

func newOutpost(layout *Layout, alliance int) *Outpost {
	o := layout.Outpost
	rangeY := o.Range["blue"]
	center := (rangeY[0] + rangeY[1]) / 2
	chuteY := center + 0.25
	if o.OpeningY > 0 {
		chuteY = o.OpeningY
	}
	wallX := 0.0
	inward := 1.0
	if alliance == Red {
		_, chuteY = layout.mirror(0, chuteY)
		wallX = layout.Field.Length
		inward = -1
	}
	return &Outpost{
		Alliance: alliance, wallX: wallX, inward: inward, chuteY: chuteY, floorHalf: o.FloorOpening.Width / 2,
		floorTop: o.FloorOpening.Bottom + o.FloorOpening.Height, chuteZ: o.ChuteOpening.Bottom + o.ChuteOpening.Height/2,
		HumanMode: "auto", Accuracy: 0.8, robotComing: -100,
	}
}

// floorOpeningHit reports whether a ball at the wall passes through the outpost floor opening.
func (outpost *Outpost) floorOpeningHit(ball *Ball, r float64) bool {
	if math.Abs(ball.P[1]-outpost.chuteY) > outpost.floorHalf-r*0.4 {
		return false
	}
	return ball.P[2] < outpost.floorTop
}

func (outpost *Outpost) chuteCount(world *World) int {
	count := 0
	for _, ball := range world.balls {
		if ball.State == BallChute && ball.Owner == outpost.Alliance {
			count++
		}
	}
	return count
}

// doorOpen reports whether the chute door is open (held by the user or by a robot waiting at the chute).
func (outpost *Outpost) doorOpen(world *World) bool {
	return outpost.DoorOpen || world.time < outpost.holdOpenUntil
}

// releasePoint is where the human player lets go of a throw.
func (outpost *Outpost) releasePoint() Vec2 {
	return Vec2{outpost.wallX - outpost.inward*humanStandBack, outpost.chuteY}
}

// step runs the chute door and the human player.
func (outpost *Outpost) step(world *World, dt float64) {
	outpost.releaseTime -= dt
	if outpost.doorOpen(world) && outpost.releaseTime <= 0 {
		for _, ball := range world.balls {
			if ball.State != BallChute || ball.Owner != outpost.Alliance {
				continue
			}
			ball.State = BallFree
			ball.P = [3]float64{outpost.wallX + outpost.inward*(world.ballRadius+0.02),
				outpost.chuteY + (world.rng.Float64()-0.5)*0.4, outpost.chuteZ}
			ball.V = [3]float64{outpost.inward * (1.4 + world.rng.Float64()*0.5), (world.rng.Float64() - 0.5) * 0.4, -0.2}
			ball.sleep = 0
			outpost.releaseTime = 0.22
			break
		}
	}

	outpost.hpTimer -= dt
	if outpost.hpTimer > 0 || outpost.Corral == 0 || !outpost.humanActive(world) {
		return
	}
	capacity := world.layout.Outpost.ChuteCapacity
	chute := outpost.chuteCount(world)
	mode := outpost.HumanMode
	if mode == "auto" {
		robotComing := world.time-outpost.robotComing < 3
		switch {
		case robotComing && chute < capacity:
			mode = "feed" // a teammate is on the way: keep the chute stocked
		case chute < humanChuteReady:
			mode = "feed" // always keep a few ready
		case outpost.throwWouldCount(world):
			mode = "score"
		case chute < capacity:
			mode = "feed"
		case outpost.Corral >= CorralCapacity:
			mode = "dump" // G427: chute and corral full, the excess goes over the wall now
		default:
			mode = "idle"
		}
	}
	switch mode {
	case "feed":
		if chute < capacity {
			if ball := outpost.takeCorralBall(world); ball != nil {
				ball.State = BallChute
				outpost.hpTimer = humanFeedTime + world.rng.Float64()*0.25
			}
		}
	case "score":
		if outpost.HumanMode == "score" || outpost.throwWouldCount(world) {
			outpost.throw(world)
		}
	case "dump":
		outpost.throw(world)
	}
}

// humanActive reports whether the human player is playing: from the start of AUTO until the buzzer. Before and after
// the match they stand at the wall and touch nothing. Without an FMS clock (free play) they always play.
func (outpost *Outpost) humanActive(world *World) bool {
	if world.matchClock == nil {
		return true
	}
	t, inMatch := world.matchClock()
	return inMatch && t >= 0 && t < TeleopEndSec
}

// throwWouldCount is the human player's judgement of whether a throw now lands while the hub is scoring: the same
// timing rules the drive teams use (with a safety margin that shrinks with skill), or the hub light without an FMS
// clock.
func (outpost *Outpost) throwWouldCount(world *World) bool {
	if world.matchClock == nil {
		return world.hubs[outpost.Alliance].LightOn
	}
	t, inMatch := world.matchClock()
	if !inMatch {
		return false
	}
	won, _ := world.allianceWonAuto(outpost.Alliance)
	countAt := t + humanFlight + sensorTransit + countLatency
	margin := 0.15 + 0.3*(1-clamp(outpost.Accuracy, 0, 1))
	return FuelScores(countAt-margin, won) && FuelScores(countAt+margin, won)
}

// throwInterval is the time between a human player's throws: about a second for a good one.
func (outpost *Outpost) throwInterval(world *World) float64 {
	return humanThrowTime + 0.5*(1-clamp(outpost.Accuracy, 0, 1)) + world.rng.Float64()*0.3
}

// throw sends one corral FUEL at the hub over the alliance wall.
func (outpost *Outpost) throw(world *World) {
	ball := outpost.takeCorralBall(world)
	if ball == nil {
		return
	}
	hub := world.hubs[outpost.Alliance]
	// Human error: a good human player's throws scatter well under a percent in speed and direction.
	spread := humanSpreadBase + humanSpreadScale*(1-clamp(outpost.Accuracy, 0, 1))
	world.throwAt(ball, outpost.releasePoint(), humanReleaseZ, hub.aimPoint(), world.hubOpeningZ()+0.12, spread)
	ball.thrower = outpost.Alliance + 1
	outpost.Throws++
	outpost.lastThrow = world.time
	outpost.hpTimer = outpost.throwInterval(world)
}

func (outpost *Outpost) takeCorralBall(world *World) *Ball {
	for _, ball := range world.balls {
		if ball.State == BallCorral && ball.Owner == outpost.Alliance {
			outpost.Corral--
			return ball
		}
	}
	return nil
}

// throwAt launches a ball on a lob that drops into a target (with drag), with some aim error (human player throws).
func (world *World) throwAt(ball *Ball, from Vec2, fromZ float64, to Vec2, toZ float64, errorSD float64) {
	delta := to.Sub(from)
	distance := delta.Len()
	dz := toZ - fromZ
	// The flattest arc that comes down into the opening.
	angle, speed := 1.0, 0.0
	for a := 0.9; a < 1.35; a += 0.03 {
		if s, descending := solveLaunchSpeed(distance, dz, a); s > 0 && descending {
			angle, speed = a, s
			break
		}
	}
	if speed == 0 {
		speed = math.Sqrt(gravity * distance)
	}
	speed *= 1 + world.rng.NormFloat64()*errorSD
	yaw := math.Atan2(delta.Y, delta.X) + world.rng.NormFloat64()*errorSD
	angle += world.rng.NormFloat64() * errorSD * 0.5
	ball.State = BallFree
	ball.P = [3]float64{from.X, from.Y, fromZ}
	ball.V = [3]float64{speed * math.Cos(angle) * math.Cos(yaw), speed * math.Cos(angle) * math.Sin(yaw),
		speed * math.Sin(angle)}
	ball.sleep = 0
	ball.shooter = 0
	ball.timing = timingNone
}
