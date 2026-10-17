package world

import (
	"math"
	"sort"
)

// FUEL inside a hub (GE-26300), simulated like any other FUEL. It drops through the opening into the clear hex
// hopper and bounces down its sloped walls into the throat, falls through the throat into the body, lands on the
// internal ramp and rolls toward the exit slot in the neutral-zone face. Near the face, dividers split the ramp into
// the four lanes. Each lane ends at a motorized exit that stops the FUEL and lets one out at a time while the motors run
// (with them off the lane backs up), and a beam-break sensor across the lane at the exit counts each FUEL as it rolls
// into the exit. A FUEL leaves only after the beam has seen it.
//
// Everything is in the hub's frame: u from the hub center toward the neutral zone (the exit face), v the field's y
// offset from the center, z up. The 3D hub draws the same ramp and lanes (web/js/scene/field.js).

const (
	hubRampRise    = 0.35 // m the ramp rises from the exit slot to the back wall
	hubDividerLen  = 0.42 // m the lane dividers run back from the exit face
	hubWall        = 0.03 // m inside the body panels
	hubDamping     = 0.6  // 1/s of air and foam losses inside
	hubBounce      = 0.35 // FUEL off polycarbonate and the HDPE ramp
	hubContactSlop = 0.008
)

// hubFrame converts between field and hub coordinates.
func (hub *Hub) toHub(p [3]float64) [3]float64 {
	return [3]float64{(p[0] - hub.Center.X) * hub.neutralDir, p[1] - hub.Center.Y, p[2]}
}

func (hub *Hub) toField(q [3]float64) [3]float64 {
	return [3]float64{hub.Center.X + q[0]*hub.neutralDir, hub.Center.Y + q[1], q[2]}
}

func (hub *Hub) face() float64 { return hub.size/2 - hubWall }

// rampFloor is the ramp's surface height at u.
func (hub *Hub) rampFloor(world *World, u float64) float64 {
	back := -hub.size/2 + hubWall
	k := clamp((hub.face()-u)/(hub.face()-back), 0, 1)
	return world.layout.Hub.Exit.Bottom + hubRampRise*k
}

// laneVs are the lanes' v offsets, in lane-index order, and the dividers between neighbours (sorted by v).
func (hub *Hub) laneVs(world *World) ([]float64, []float64) {
	lanes := world.layout.Hub.Exit.Lanes
	vs := make([]float64, len(lanes))
	sorted := make([]float64, len(lanes))
	for k := range lanes {
		vs[k] = hub.laneOffset(world, k)
		sorted[k] = vs[k]
	}
	sort.Float64s(sorted)
	dividers := make([]float64, 0, len(sorted))
	for k := 1; k < len(sorted); k++ {
		dividers = append(dividers, (sorted[k-1]+sorted[k])/2)
	}
	return vs, dividers
}

// laneAt is the lane whose strip a v offset is in.
func (hub *Hub) laneAt(world *World, v float64) int {
	vs, _ := hub.laneVs(world)
	best, bestDistance := 0, math.Inf(1)
	for k, lane := range vs {
		if d := math.Abs(v - lane); d < bestDistance {
			best, bestDistance = k, d
		}
	}
	return best
}

// step moves the FUEL inside the hub, reads the beam breaks and runs the exits.
func (hub *Hub) step(world *World, dt float64) {
	var inside []*Ball
	for _, ball := range world.balls {
		if ball.State == BallHub && ball.Owner == hub.Alliance {
			inside = append(inside, ball)
		}
	}
	lanes := len(world.layout.Hub.Exit.Lanes)
	for k := 0; k < lanes && k < len(hub.exitTimers); k++ {
		hub.exitTimers[k] -= dt
	}
	if len(inside) == 0 {
		hub.beamBlocked = [4]bool{}
		return
	}
	r := world.ballRadius
	damping := math.Max(0, 1-hubDamping*dt)
	for _, ball := range inside {
		ball.V[2] -= gravity * dt
		for axis := 0; axis < 3; axis++ {
			ball.V[axis] *= damping
			ball.P[axis] += ball.V[axis] * dt
		}
	}
	// Contacts: FUEL on FUEL (soft), then the hub's insides. Three passes keep a backed-up lane settled.
	for pass := 0; pass < 3; pass++ {
		hub.collideInside(inside, r)
		for _, ball := range inside {
			hub.contain(world, ball, r, dt, pass == 0)
		}
	}
	hub.readBeams(world, inside, r, dt)
	hub.runExits(world, inside, r)
}

// collideInside resolves FUEL-on-FUEL contacts inside the hub.
func (hub *Hub) collideInside(balls []*Ball, r float64) {
	diameter := 2 * r
	for i, a := range balls {
		for j := i + 1; j < len(balls); j++ {
			b := balls[j]
			dx, dy, dz := b.P[0]-a.P[0], b.P[1]-a.P[1], b.P[2]-a.P[2]
			if math.Abs(dx) >= diameter || math.Abs(dy) >= diameter || math.Abs(dz) >= diameter {
				continue
			}
			d2 := dx*dx + dy*dy + dz*dz
			if d2 >= diameter*diameter {
				continue
			}
			d := math.Sqrt(d2)
			n := [3]float64{0, 0, 1}
			if d > 1e-6 {
				n = [3]float64{dx / d, dy / d, dz / d}
			}
			overlap := diameter - d
			push := 0.5 * (math.Max(0, overlap-hubContactSlop) + 0.3*math.Min(overlap, hubContactSlop))
			relative := 0.0
			for axis := 0; axis < 3; axis++ {
				a.P[axis] -= n[axis] * push
				b.P[axis] += n[axis] * push
				relative += (b.V[axis] - a.V[axis]) * n[axis]
			}
			if relative < 0 {
				impulse := -(1 + ballBounce) * relative / 2
				for axis := 0; axis < 3; axis++ {
					a.V[axis] -= impulse * n[axis]
					b.V[axis] += impulse * n[axis]
				}
			}
		}
	}
}

// contain keeps a FUEL inside the hub: the clear hopper's sloped walls, the throat, the body panels, the ramp, the lane
// dividers and the closed exit gates. roll applies the ramp's pull (once per step).
func (hub *Hub) contain(world *World, ball *Ball, r float64, dt float64, roll bool) {
	q := hub.toHub(ball.P)
	w := hub.toHub([3]float64{ball.V[0] + hub.Center.X, ball.V[1] + hub.Center.Y, ball.V[2]}) // velocity in hub frame
	defer func() {
		ball.P = hub.toField(q)
		ball.V = [3]float64{w[0] * hub.neutralDir, w[1], w[2]}
	}()
	// Bounced back up out of the opening: it is a free ball again.
	if q[2] > hub.openingZ+0.25 {
		ball.State = BallFree
		return
	}
	reflect := func(axis int, sign float64) { // keep the velocity from pointing into a wall
		if w[axis]*sign < 0 {
			w[axis] = -w[axis] * hubBounce
		}
	}
	switch {
	case q[2] > hub.throatZ:
		// The clear hopper: a funnel from the opening down to the throat (its axis leans 5 cm toward the stations).
		k := clamp((q[2]-hub.throatZ)/(hub.openingZ-hub.throatZ), 0, 1)
		center := -0.05 * k
		radius := hub.throatR + (hub.radius-hub.throatR)*k
		hub.keepInCircle(&q, &w, center, radius-r)
	case q[2] > hub.baseZ-0.02:
		hub.keepInCircle(&q, &w, 0, hub.throatR-r) // the throat
	default:
		// The body: panels all round, the ramp under it, and the roof over it (except up the throat).
		back, face := -hub.size/2+hubWall, hub.face()
		side := hub.size/2 - hubWall - 0.01
		if q[0] < back+r {
			q[0] = back + r
			reflect(0, 1)
		}
		if q[1] < -side+r {
			q[1] = -side + r
			reflect(1, 1)
		} else if q[1] > side-r {
			q[1] = side - r
			reflect(1, -1)
		}
		if math.Hypot(q[0], q[1]) > hub.throatR-r && q[2] > hub.baseZ-0.03-r {
			q[2] = hub.baseZ - 0.03 - r
			reflect(2, -1)
		}
		// The ramp: its surface tilts down toward the exit face. Against its normal a FUEL bounces only off a real
		// impact; otherwise it stays on the surface and gravity carries it down the slope.
		floor := hub.rampFloor(world, q[0]) + r
		if q[2] < floor+0.002 {
			if q[2] < floor {
				q[2] = floor
			}
			slope := hubRampRise / (face - back)
			norm := math.Sqrt(1 + slope*slope)
			n := [3]float64{slope / norm, 0, 1 / norm}
			if into := w[0]*n[0] + w[2]*n[2]; into < 0 {
				k := 1.0
				if into < -0.3 {
					k = 1 + hubBounce
				}
				w[0] -= k * into * n[0]
				w[2] -= k * into * n[2]
			}
			if roll {
				friction := math.Max(0, 1-0.3*dt) // rolling resistance on HDPE
				w[0] *= friction
				w[1] *= friction
			}
		}
		// Lane dividers: thin plates from the face back hubDividerLen; FUEL meeting a plate's back edge is turned
		// into one lane or the other.
		_, dividers := hub.laneVs(world)
		start := face - hubDividerLen
		for _, d := range dividers {
			nearestU := clamp(q[0], start, face)
			du, dv := q[0]-nearestU, q[1]-d
			dist := math.Hypot(du, dv)
			if dist < r && q[2]-r < hub.rampFloor(world, nearestU)+0.2 {
				if dist < 1e-6 {
					du, dv, dist = 0, 1, 1
					if (q[1]-d) < 0 || (q[1] == d && w[1] < 0) {
						dv = -1
					}
				}
				q[0] += du / dist * (r - dist)
				q[1] += dv / dist * (r - dist)
				if into := w[0]*du/dist + w[1]*dv/dist; into < 0 {
					w[0] -= (1 + hubBounce) * into * du / dist
					w[1] -= (1 + hubBounce) * into * dv / dist
				}
			}
		}
		// The exit gate: a wall until the exit lets this FUEL out (runExits).
		if q[0] > face-r {
			q[0] = face - r
			reflect(0, -1)
		}
	}
}

// keepInCircle keeps a FUEL's (u, v) within radius of (center, 0).
func (hub *Hub) keepInCircle(q, w *[3]float64, center, radius float64) {
	du, dv := q[0]-center, q[1]
	d := math.Hypot(du, dv)
	if d <= radius || d < 1e-9 {
		return
	}
	nu, nv := du/d, dv/d
	q[0], q[1] = center+nu*radius, nv*radius
	if out := w[0]*nu + w[1]*nv; out > 0 {
		w[0] -= (1 + hubBounce) * out * nu
		w[1] -= (1 + hubBounce) * out * nv
	}
}

// readBeams checks each lane's beam break. It crosses the lane right at the exit gate, where the motorized exit
// stops each FUEL and lets them out one at a time, so the sensor sees every FUEL on its own: the beam clears as one
// leaves, and the next one rolls down into it. A FUEL breaking the beam is counted (once; one bouncing against the
// gate does not count again).
func (hub *Hub) readBeams(world *World, inside []*Ball, r, dt float64) {
	vs, _ := hub.laneVs(world)
	beamU := hub.face() - r
	var blocked [4]bool
	for _, ball := range inside {
		q := hub.toHub(ball.P)
		if q[2] > world.layout.Hub.Exit.Top {
			continue
		}
		lane := hub.laneAt(world, q[1])
		if lane >= len(blocked) || math.Abs(q[0]-beamU) > r*0.5 || math.Abs(q[1]-vs[lane]) > 0.14 {
			continue
		}
		blocked[lane] = true
		if ball.hubPhase == 0 {
			hub.count(world, ball, lane)
		}
	}
	hub.beamBlocked = blocked
}

// count is a beam break's counter ticking.
func (hub *Hub) count(world *World, ball *Ball, lane int) {
	hub.Sensed[lane]++
	hub.Stored++
	if world.field != nil {
		world.field.ScoreFuel(hub.Alliance, lane)
	}
	world.recordCount(hub.Alliance)
	if ball != nil && ball.hubPhase == 0 {
		ball.hubPhase = 1
		ball.sensor = lane
		world.recordTimedCount(ball)
		ball.timing = timingNone
	}
}

// runExits lets FUEL out of the lanes whose motors are running, one at a time per lane.
func (hub *Hub) runExits(world *World, inside []*Ball, r float64) {
	if !hub.MotorOn {
		return
	}
	exitSpec := world.layout.Hub.Exit
	face := hub.face()
	for _, ball := range inside {
		if ball.State != BallHub {
			continue
		}
		q := hub.toHub(ball.P)
		if q[0] < face-r-0.012 || q[2] > exitSpec.Top {
			continue
		}
		lane := hub.laneAt(world, q[1])
		if lane >= len(hub.exitTimers) || hub.exitTimers[lane] > 0 || ball.hubPhase == 0 {
			continue // (a FUEL leaves only after its beam break has seen it)
		}
		hub.exitTimers[lane] = exitPeriod * (0.85 + world.rng.Float64()*0.3)
		// Out over the slot lip under the AprilTag band, onto the carpet. The outer lanes sit next to the
		// deflectors, which turn their FUEL in toward the middle.
		laneV := hub.laneOffset(world, lane)
		lateral := (world.rng.Float64() - 0.5) * 0.5
		if math.Abs(exitSpec.Lanes[lane]) > hub.size/4 {
			lateral = -math.Copysign(0.15+world.rng.Float64()*0.3, laneV)
		}
		out := [3]float64{hub.size/2 + r + 0.01, q[1], exitSpec.Bottom + r + 0.005}
		ball.State = BallFree
		ball.P = hub.toField(out)
		speed := exitSpec.Speed[0] + world.rng.Float64()*(exitSpec.Speed[1]-exitSpec.Speed[0])
		ball.V = [3]float64{hub.neutralDir * speed, lateral, -0.1}
		ball.sleep = 0
		ball.fromHub = true // G408: no robot may catch it until it touches something else
		hub.lastRelease = world.time
		hub.Stored-- // Stored is the counted FUEL still inside
		ball.hubPhase = 0
	}
}
