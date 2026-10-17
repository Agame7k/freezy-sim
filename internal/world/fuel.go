package world

import "math"

// Ball states.
const (
	BallFree   = iota // on the field or in flight
	BallHeld          // inside a robot's hopper
	BallHub           // inside a hub (falling through sensors or waiting at an exit)
	BallChute         // staged in an outpost chute
	BallCorral        // in an outpost corral (human-player side)
	BallOut           // left the field over a guardrail
)

const (
	gravity         = 9.81
	dragK           = 0.022 // quadratic air drag (1/m) for a 0.2 kg, 15 cm foam ball
	groundBounce    = 0.35
	wallBounce      = 0.4
	ballBounce      = 0.3
	rollingFriction = 2.4
	sleepSpeed      = 0.04
	sleepFrames     = 24
	fieldSlop       = 0.008 // m two FUEL squeeze into each other before pushing back hard (foam)
	gridCell        = 0.16

	// Spin. FUEL is a 0.21 kg, 15 cm foam ball. Magnus lift is about 0.5·ρ·A·C_L·v² with C_L ≈ 0.5·rω/v at the spin
	// rates shooters give it, i.e. magnusK·|ω×v| per unit mass; air slows the spin over a few seconds. The carpet grips
	// the foam hard.
	magnusK            = 0.5 * 1.2 * math.Pi * 0.075 * 0.075 * 0.5 * 0.075 / 0.21
	spinDecay          = 3.0 // s
	ballCarpetFriction = 0.6
	// Foam gets deader the harder it is hit: the bounce coefficients above hold at bounceRefSpeed and fall off above it.
	bounceRefSpeed = 3.0
	bounceSoftness = 5.0 // m/s
)

// foamBounce is the coefficient of restitution of a foam impact at speed (m/s), from its value at bounceRefSpeed.
func foamBounce(nominal, speed float64) float64 {
	return math.Min(0.75, nominal*(1+bounceRefSpeed/bounceSoftness)/(1+speed/bounceSoftness))
}

// ballGroundFriction applies the carpet's grip at the contact point under a ball (normalImpulse per unit mass):
// friction opposes the slip of the contact point, up to μ·N, and turns the ball's speed and spin toward rolling.
// The ball is a solid foam sphere (I = 2/5·m·r²).
func ballGroundFriction(ball *Ball, r, normalImpulse float64) {
	// Contact point velocity: v + ω × (0, 0, −r).
	slipX := ball.V[0] - r*ball.W[1]
	slipY := ball.V[1] + r*ball.W[0]
	slip := math.Hypot(slipX, slipY)
	if slip < 1e-6 {
		return
	}
	// The impulse that would stop the slip outright (the contact point of a solid sphere responds 7/2 as fast), capped
	// by friction.
	j := math.Min(slip/3.5, ballCarpetFriction*normalImpulse)
	jx, jy := -slipX/slip*j, -slipY/slip*j
	ball.V[0] += jx
	ball.V[1] += jy
	ball.W[0] += jy / (0.4 * r)
	ball.W[1] -= jx / (0.4 * r)
}

type Ball struct {
	P        [3]float64
	V        [3]float64
	W        [3]float64 // spin (rad/s) while free
	State    int
	Owner    int
	sleep    int
	shooter  int
	sensor   int
	timing   int  // early/late shot tag
	autoShot bool // launched in AUTO (credited to the shooter's auto routine)
	thrower  int  // alliance+1 of the human player who threw it
	// fromHub marks FUEL just released by a hub exit; until it touches the carpet or other FUEL, a robot that takes
	// control of it is catching (G408).
	fromHub bool
	// Inside a hub (hubflow.go): 0 not counted yet, 1 counted by a lane's beam break, 2 got past the beam uncounted.
	hubPhase int
	outAt    float64 // world time it left the field (field staff put it back)
	// Being pulled into a robot: progress (1 = in the hopper) and the robot-frame point it was picked up at. In the
	// hopper it is simulated in the robot's frame (x forward, y left, z up): position and velocity (hopper.go).
	intakeT    float64
	intakeFrom [3]float64
	hopP, hopV [3]float64
}

func (ball *Ball) asleep() bool { return ball.sleep >= sleepFrames }
func (ball *Ball) wake()        { ball.sleep = 0 }

// groundHeight returns the floor height (bumps) and its slope along x at a point.
func (world *World) groundHeight(p Vec2) (float64, float64) {
	b := world.layout.Bump
	for _, bump := range world.bumps {
		if p.Y < bump.MinY || p.Y > bump.MaxY || p.X < bump.MinX || p.X > bump.MaxX {
			continue
		}
		// The BUMP (GE-26100, measured from the field CAD): HDPE ramps at 15° meeting in a sharp ridge 6.5 in up the
		// middle, with a lip of about half an inch where they meet the carpet.
		center := (bump.MinX + bump.MaxX) / 2
		offset := p.X - center
		grade := math.Tan(b.RampDegrees * math.Pi / 180)
		if grade <= 0 {
			grade = b.Height / (b.Depth / 2)
		}
		height := b.Height - grade*math.Abs(offset)
		slope := -math.Copysign(grade, offset)
		return math.Max(0, height), slope
	}
	return 0, 0
}

// stepBalls advances every free ball.
func (world *World) stepBalls(dt float64) {
	r := world.ballRadius
	L := world.layout.Field.Length
	W := world.layout.Field.Width
	rail := world.layout.Guardrail.Height

	for _, ball := range world.balls {
		if ball.State != BallFree {
			ball.W = [3]float64{} // held, in a hub or a chute: whatever spin it had is gone
			continue
		}
		if ball.asleep() {
			continue
		}
		// Gravity, quadratic drag and the Magnus lift of its spin (backspin off a hooded shooter holds a shot up).
		ball.V[2] -= gravity * dt
		speed := math.Sqrt(ball.V[0]*ball.V[0] + ball.V[1]*ball.V[1] + ball.V[2]*ball.V[2])
		drag := 1 - dragK*speed*dt
		ball.V[0] *= drag
		ball.V[1] *= drag
		ball.V[2] *= drag
		if ball.W != [3]float64{} {
			w, v := ball.W, ball.V
			ball.V[0] += magnusK * (w[1]*v[2] - w[2]*v[1]) * dt
			ball.V[1] += magnusK * (w[2]*v[0] - w[0]*v[2]) * dt
			ball.V[2] += magnusK * (w[0]*v[1] - w[1]*v[0]) * dt
			decay := 1 - dt/spinDecay
			ball.W[0] *= decay
			ball.W[1] *= decay
			ball.W[2] *= decay
		}
		ball.P[0] += ball.V[0] * dt
		ball.P[1] += ball.V[1] * dt
		ball.P[2] += ball.V[2] * dt

		// Floor (with bumps).
		ground, slope := world.groundHeight(Vec2{ball.P[0], ball.P[1]})
		onGround := false
		if ball.P[2]-r <= ground {
			ball.P[2] = ground + r
			normalImpulse := gravity * dt // resting contact (per unit mass)
			if ball.V[2] < 0 {
				into := -ball.V[2]
				ball.V[2] = into * foamBounce(groundBounce, into)
				normalImpulse += into + ball.V[2]
				if ball.V[2] < 0.15 {
					ball.V[2] = 0
				}
			}
			onGround = true
			// The carpet grips the ball where it touches: a skidding or spinning ball trades spin for speed until it
			// rolls (a backspin shot checks up where it lands).
			ballGroundFriction(ball, r, normalImpulse)
			friction := math.Max(0, 1-rollingFriction*dt)
			ball.V[0] *= friction
			ball.V[1] *= friction
			ball.V[0] += gravity * slope * (5.0 / 7) * dt // a rolling sphere: 5/7 of g along the slope
		}

		// Alliance walls (x) — with outpost floor openings into the corrals.
		if ball.P[0] < r || ball.P[0] > L-r {
			side := Blue
			if ball.P[0] > L/2 {
				side = Red
			}
			if world.outposts[side].floorOpeningHit(ball, r) {
				ball.State = BallCorral
				ball.Owner = side
				world.outposts[side].Corral++
				continue
			}
			if ball.P[2] < 2.0 {
				if ball.P[0] < r {
					ball.P[0] = r
					ball.V[0] = math.Abs(ball.V[0]) * foamBounce(wallBounce, math.Abs(ball.V[0]))
				} else {
					ball.P[0] = L - r
					ball.V[0] = -math.Abs(ball.V[0]) * foamBounce(wallBounce, math.Abs(ball.V[0]))
				}
			}
		}
		// Guardrails (y) are only 20 in tall; a high ball can leave the field.
		if ball.P[1] < r || ball.P[1] > W-r {
			if ball.P[2]-r < rail {
				if ball.P[1] < r {
					ball.P[1] = r
					ball.V[1] = math.Abs(ball.V[1]) * foamBounce(wallBounce, math.Abs(ball.V[1]))
				} else {
					ball.P[1] = W - r
					ball.V[1] = -math.Abs(ball.V[1]) * foamBounce(wallBounce, math.Abs(ball.V[1]))
				}
			}
		}
		// Over a guardrail or the alliance wall: out of the field. It keeps flying and lands outside (offField).
		// (FUEL coming in from outside, a human player's throw or a staff toss, is still on its way in.)
		leaving := (ball.P[0] < -r && ball.V[0] <= 0) || (ball.P[0] > L+r && ball.V[0] >= 0) ||
			(ball.P[1] < -r && ball.V[1] <= 0) || (ball.P[1] > W+r && ball.V[1] >= 0)
		if leaving || ((ball.P[1] < -r || ball.P[1] > W+r || ball.P[0] < -r || ball.P[0] > L+r) && ball.P[2] < rail) {
			ball.State = BallOut
			ball.outAt = world.time
			ball.sleep = 0
			world.stats.OutOfField++
			if ball.shooter > 0 && ball.shooter <= len(world.robots) {
				world.stats.OutBy[world.robots[ball.shooter-1].Alliance]++
			}
			continue
		}

		// Solid field elements.
		for _, static := range world.statics {
			if !static.Fuel {
				continue
			}
			world.collideBallBox(ball, static.Box, r)
		}

		// Hubs: capture through the opening, bounce off the rim and back net.
		for _, hub := range world.hubs {
			if hub.interact(world, ball, r) {
				break
			}
		}
		if ball.State != BallFree {
			continue
		}

		if onGround {
			ball.shooter = 0
			ball.fromHub = false
		}
		if onGround && speed < sleepSpeed {
			ball.sleep++
		} else {
			ball.sleep = 0
		}
	}
}

// collideBallBox resolves a sphere against an axis-aligned box in 3D.
func (world *World) collideBallBox(ball *Ball, box AABB, r float64) {
	cx := clamp(ball.P[0], box.MinX, box.MaxX)
	cy := clamp(ball.P[1], box.MinY, box.MaxY)
	cz := clamp(ball.P[2], box.ZMin, box.ZMax)
	dx, dy, dz := ball.P[0]-cx, ball.P[1]-cy, ball.P[2]-cz
	distanceSquared := dx*dx + dy*dy + dz*dz
	if distanceSquared > r*r {
		return
	}
	var nx, ny, nz, depth float64
	if distanceSquared > 1e-12 {
		distance := math.Sqrt(distanceSquared)
		nx, ny, nz = dx/distance, dy/distance, dz/distance
		depth = r - distance
	} else {
		// Center inside the box: exit through the nearest face.
		candidates := [6][4]float64{
			{-1, 0, 0, ball.P[0] - box.MinX}, {1, 0, 0, box.MaxX - ball.P[0]},
			{0, -1, 0, ball.P[1] - box.MinY}, {0, 1, 0, box.MaxY - ball.P[1]},
			{0, 0, -1, ball.P[2] - box.ZMin}, {0, 0, 1, box.ZMax - ball.P[2]},
		}
		best := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate[3] < best[3] {
				best = candidate
			}
		}
		nx, ny, nz, depth = best[0], best[1], best[2], best[3]+r
	}
	ball.P[0] += nx * depth
	ball.P[1] += ny * depth
	ball.P[2] += nz * depth
	into := ball.V[0]*nx + ball.V[1]*ny + ball.V[2]*nz
	if into < 0 {
		bounce := foamBounce(wallBounce, -into)
		ball.V[0] -= (1 + bounce) * into * nx
		ball.V[1] -= (1 + bounce) * into * ny
		ball.V[2] -= (1 + bounce) * into * nz
	}
	ball.wake()
}

// collideBalls resolves ball-ball contacts with a uniform spatial hash.
func (world *World) collideBalls() {
	r := world.ballRadius
	diameter := 2 * r
	cols := int(world.layout.Field.Length/gridCell) + 3
	rows := int(world.layout.Field.Width/gridCell) + 3
	if len(world.grid) != cols*rows {
		world.grid = make([][]int32, cols*rows)
	}
	for i := range world.grid {
		world.grid[i] = world.grid[i][:0]
	}
	cell := func(x, y float64) (int, int) {
		cx := int(x/gridCell) + 1
		cy := int(y/gridCell) + 1
		if cx < 0 {
			cx = 0
		}
		if cy < 0 {
			cy = 0
		}
		if cx >= cols {
			cx = cols - 1
		}
		if cy >= rows {
			cy = rows - 1
		}
		return cx, cy
	}
	for i, ball := range world.balls {
		if ball.State != BallFree {
			continue
		}
		cx, cy := cell(ball.P[0], ball.P[1])
		world.grid[cy*cols+cx] = append(world.grid[cy*cols+cx], int32(i))
	}
	for i, a := range world.balls {
		if a.State != BallFree || a.asleep() {
			continue
		}
		cx, cy := cell(a.P[0], a.P[1])
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				x, y := cx+ox, cy+oy
				if x < 0 || y < 0 || x >= cols || y >= rows {
					continue
				}
				for _, j := range world.grid[y*cols+x] {
					if int(j) == i {
						continue
					}
					b := world.balls[j]
					if !b.asleep() && int(j) < i {
						continue // pair handled from the other side
					}
					dx := b.P[0] - a.P[0]
					dy := b.P[1] - a.P[1]
					dz := b.P[2] - a.P[2]
					distanceSquared := dx*dx + dy*dy + dz*dz
					if distanceSquared >= diameter*diameter || distanceSquared < 1e-12 {
						continue
					}
					distance := math.Sqrt(distanceSquared)
					nx, ny, nz := dx/distance, dy/distance, dz/distance
					overlap := diameter - distance
					a.fromHub, b.fromHub = false, false
					// FUEL is foam: a light touch squeezes it a little (pushed apart gently), a hard one pushes back
					// fully. A resting ball is woken and shoved like any other, so piles give way instead of acting
					// as fixed anchors.
					if b.asleep() && overlap > fieldSlop {
						b.wake()
					}
					push := 0.5 * (math.Max(0, overlap-fieldSlop) + 0.3*math.Min(overlap, fieldSlop))
					wa, wb := push, push
					if b.asleep() {
						wa, wb = 2*push, 0 // too light a touch to move a resting ball
					}
					a.P[0] -= nx * wa
					a.P[1] -= ny * wa
					a.P[2] -= nz * wa
					b.P[0] += nx * wb
					b.P[1] += ny * wb
					b.P[2] += nz * wb
					relative := (b.V[0]-a.V[0])*nx + (b.V[1]-a.V[1])*ny + (b.V[2]-a.V[2])*nz
					if relative < 0 {
						impulse := -(1 + foamBounce(ballBounce, -relative)) * relative / 2
						a.V[0] -= impulse * nx
						a.V[1] -= impulse * ny
						a.V[2] -= impulse * nz
						b.V[0] += impulse * nx
						b.V[1] += impulse * ny
						b.V[2] += impulse * nz
						if math.Abs(relative) > 0.03 {
							b.wake()
						}
					}
				}
			}
		}
	}
}

// interactRobotsBalls handles intakes and robots pushing FUEL around.
func (world *World) interactRobotsBalls() {
	r := world.ballRadius
	for _, robot := range world.robots {
		if !robot.onField() {
			continue
		}
		box := robot.obb()
		reach := box.Half.X + r + 0.25
		forward := robot.forward()
		left := Vec2{-forward.Y, forward.X}
		top := robot.Z + robot.Arch.Height
		for _, ball := range world.balls {
			if ball.State != BallFree {
				continue
			}
			center := Vec2{ball.P[0], ball.P[1]}
			offset := center.Sub(robot.Pos)
			if math.Abs(offset.X) > reach || math.Abs(offset.Y) > reach {
				continue
			}
			if ball.P[2]-r > top || (ball.shooter == robot.Index+1 && ball.V[2] > 0) {
				continue
			}
			// Coming down onto the robot from above: into its open hopper, or onto its top to roll off.
			if ball.P[2] > top-0.06 && ball.V[2] <= 0 && world.landOnRobot(robot, ball, offset, forward, left, top) {
				continue
			}
			// Intake: a ball low in front of the bumper gets pulled in.
			if robot.IntakeOn && ball.P[2] < robot.Z+0.32 {
				along := offset.Dot(forward)
				across := offset.Dot(left)
				// The rollers reach slightly over the bumper, so balls pinned against a wall can still be picked up.
				if along > box.Half.X-0.13 && along < box.Half.X+0.24 && math.Abs(across) < robot.Arch.IntakeWidth/2 {
					if ball.fromHub {
						world.noteCatch(robot)
						ball.fromHub = false
					}
					ball.State = BallHeld
					ball.Owner = robot.Index
					ball.thrower = 0
					ball.intakeT = 0 // ride the rollers and conveyor into the hopper (see stepIntakes)
					ball.intakeFrom = [3]float64{along, across, ball.P[2] - robot.Z}
					ball.V = [3]float64{}
					robot.Hopper++
					robot.Collected++
					if robot.Hopper >= robot.Arch.Capacity {
						robot.IntakeOn = false
					}
					continue
				}
			}
			push, normal, hit := circleOBB(center, r, box)
			if !hit {
				continue
			}
			ball.P[0] += push.X
			ball.P[1] += push.Y
			// Pinned between the robot and a wall or field element: a foam ball squeezes and pops out sideways
			// along the bumper instead of ending up inside the robot.
			if world.ballPinned(ball, r) {
				tangent := Vec2{-normal.Y, normal.X}
				if offset.Dot(tangent) < 0 || (offset.Dot(tangent) == 0 && world.rng.Float64() < 0.5) {
					tangent = tangent.Scale(-1)
				}
				ball.P[0] -= push.X
				ball.P[1] -= push.Y
				slide := tangent.Scale(push.Len() + 0.02)
				ball.P[0] += slide.X
				ball.P[1] += slide.Y
				ball.V[0], ball.V[1] = robot.Vel.X*0.3+tangent.X*1.2, robot.Vel.Y*0.3+tangent.Y*1.2
				ball.V[2] = math.Max(ball.V[2], 0.4) // squirts up a little
				ball.wake()
				continue
			}
			relative := Vec2{ball.V[0], ball.V[1]}.Sub(robot.Vel).Dot(normal)
			if relative < 0 {
				ball.V[0] -= (1 + 0.2) * relative * normal.X
				ball.V[1] -= (1 + 0.2) * relative * normal.Y
			}
			ball.wake()
		}
	}
}

// fieldStaff does what the field staff do (Game Manual 6.8): FUEL that left the field (other than into an outpost)
// goes back in near where it left at the next safe moment. It also restages any FUEL the physics lost track of
// (numerical blow-ups, tunnelling under the carpet), so a glitch never takes FUEL out of play.
func (world *World) fieldStaff() {
	r := world.ballRadius
	L, W := world.layout.Field.Length, world.layout.Field.Width
	inside := func(ball *Ball) {
		x := clamp(ball.P[0], 0.35, L-0.35)
		y := clamp(ball.P[1], 0.35, W-0.35)
		if math.IsNaN(x) || math.IsNaN(y) {
			x, y = L/2+(world.rng.Float64()-0.5)*4, W/2+(world.rng.Float64()-0.5)*4
		}
		ball.State = BallFree
		ball.P = [3]float64{x, y, r}
		ball.V = [3]float64{}
		ball.shooter, ball.thrower, ball.fromHub = 0, 0, false
		ball.sleep = 0
	}
	for _, ball := range world.balls {
		switch ball.State {
		case BallOut:
			p := ball.P
			if ball.offField(world.time) && (math.IsNaN(p[0]+p[1]+p[2]) || math.IsInf(p[0]+p[1]+p[2], 0)) {
				inside(ball)
				continue
			}
			// Once it has stopped somewhere (or after a while), a staff member picks it up and tosses it back in.
			if ball.offField(world.time) && world.time-ball.outAt > staffWaitMin+world.rng.Float64()*3 &&
				(ball.asleep() || world.time-ball.outAt > 10) {
				world.staffReturn(ball)
			}
		case BallFree:
			p := ball.P
			if math.IsNaN(p[0]+p[1]+p[2]) || math.IsInf(p[0]+p[1]+p[2], 0) || p[2] < -0.2 || p[2] > 12 ||
				p[0] < -venueMargin-1 || p[0] > L+venueMargin+1 || p[1] < -venueMargin-1 || p[1] > W+venueMargin+1 {
				inside(ball)
			}
		}
	}
}

// Outside the field. FUEL that leaves the field is not removed: it flies on, lands on the floor around the field
// (the venue floor, between the guardrails and the stands or behind the driver stations) and rolls to a stop. The
// field staff pick it up where it lies and toss it back over the rail (Game Manual 6.8).
const (
	venueMargin   = 3.0 // m of floor between the guardrails and the stands / behind the driver stations
	staffWaitMin  = 3.0 // s before the staff get to a FUEL that left the field
	staffTossBack = 1.4 // m inside the rail the staff aim their toss
)

// offField reports whether a ball is out of the field on the venue floor (not FUEL the crew took between matches).
func (ball *Ball) offField(now float64) bool { return ball.State == BallOut && ball.outAt <= now+1000 }

// stepOffField moves FUEL outside the field: flight, the venue floor, the outside of the guardrails and the stands.
func (world *World) stepOffField(dt float64) {
	r := world.ballRadius
	L, W := world.layout.Field.Length, world.layout.Field.Width
	rail := world.layout.Guardrail.Height
	for _, ball := range world.balls {
		if !ball.offField(world.time) || ball.asleep() {
			continue
		}
		ball.V[2] -= gravity * dt
		speed := math.Sqrt(ball.V[0]*ball.V[0] + ball.V[1]*ball.V[1] + ball.V[2]*ball.V[2])
		drag := 1 - dragK*speed*dt
		for axis := 0; axis < 3; axis++ {
			ball.V[axis] *= drag
			ball.P[axis] += ball.V[axis] * dt
		}
		onGround := ball.P[2] <= r
		if onGround {
			ball.P[2] = r
			if ball.V[2] < 0 {
				ball.V[2] = -ball.V[2] * groundBounce
				if ball.V[2] < 0.15 {
					ball.V[2] = 0
				}
			}
			friction := math.Max(0, 1-rollingFriction*1.5*dt) // concrete with tape and cable covers
			ball.V[0] *= friction
			ball.V[1] *= friction
		}
		// The outside of the field border: guardrails along the sides, the alliance walls at the ends.
		inX, inY := ball.P[0] > -r && ball.P[0] < L+r, ball.P[1] > -r && ball.P[1] < W+r
		if inX && inY {
			wall := rail
			if ball.P[0] < r || ball.P[0] > L-r {
				wall = 2.0
			}
			if ball.P[2]-r < wall {
				// Push it back out through the nearest side.
				distances := [4]float64{ball.P[0] + r, L + r - ball.P[0], ball.P[1] + r, W + r - ball.P[1]}
				side := 0
				for k := 1; k < 4; k++ {
					if distances[k] < distances[side] {
						side = k
					}
				}
				switch side {
				case 0:
					ball.P[0], ball.V[0] = -r, -math.Abs(ball.V[0])*wallBounce
				case 1:
					ball.P[0], ball.V[0] = L+r, math.Abs(ball.V[0])*wallBounce
				case 2:
					ball.P[1], ball.V[1] = -r, -math.Abs(ball.V[1])*wallBounce
				default:
					ball.P[1], ball.V[1] = W+r, math.Abs(ball.V[1])*wallBounce
				}
			} else if ball.V[2] < 0 && ball.P[0] > 0 && ball.P[0] < L && ball.P[1] > 0 && ball.P[1] < W {
				ball.State = BallFree // dropped back into the field over the rail
				continue
			}
		}
		// The stands and the walls behind the driver stations.
		if ball.P[0] < -venueMargin || ball.P[0] > L+venueMargin {
			ball.P[0] = clamp(ball.P[0], -venueMargin, L+venueMargin)
			ball.V[0] = -ball.V[0] * wallBounce
		}
		if ball.P[1] < -venueMargin || ball.P[1] > W+venueMargin {
			ball.P[1] = clamp(ball.P[1], -venueMargin, W+venueMargin)
			ball.V[1] = -ball.V[1] * wallBounce
		}
		if onGround && speed < sleepSpeed {
			ball.sleep++
		} else {
			ball.sleep = 0
		}
	}
}

// staffReturn has the field staff toss a FUEL that left the field back in, from where it lies, over the nearest rail.
func (world *World) staffReturn(ball *Ball) {
	L, W := world.layout.Field.Length, world.layout.Field.Width
	from := Vec2{ball.P[0], ball.P[1]}
	near := Vec2{clamp(from.X, staffTossBack, L-staffTossBack), clamp(from.Y, staffTossBack, W-staffTossBack)}
	if from.X < 0 || from.X > L {
		near.X = clamp(from.X, 2.5, L-2.5) // back over the alliance wall, well clear of it
	}
	// Aim at open carpet: step further in, then along the rail, until nothing solid is near the landing spot.
	inward := Vec2{L / 2, W / 2}.Sub(near).Norm()
	target := near
	for k := 0; k < 24; k++ {
		candidate := near.Add(inward.Scale(float64(k%4) * 0.7)).Add(Vec2{-inward.Y, inward.X}.Scale(float64(k/4) * 0.8 * float64(1-2*(k/4%2))))
		if world.openCarpet(candidate, 0.45) {
			target = candidate
			break
		}
	}
	target = target.Add(Vec2{(world.rng.Float64() - 0.5) * 0.3, (world.rng.Float64() - 0.5) * 0.3})
	world.throwAt(ball, from, 1.3, target, world.ballRadius, 0.05)
	ball.thrower, ball.fromHub = 0, false
}

// ballPinned reports whether a FUEL sits inside a field wall or a solid field element (it has been pushed there).
func (world *World) ballPinned(ball *Ball, r float64) bool {
	L, W := world.layout.Field.Length, world.layout.Field.Width
	const give = 0.02 // it may squeeze this far into a wall
	// An outpost floor opening is a hole in the alliance wall: FUEL pushed into it goes through to the corral.
	for _, outpost := range world.outposts {
		atWall := math.Abs(ball.P[0]-outpost.wallX) < 2*r
		if atWall && outpost.floorOpeningHit(ball, r) {
			return false
		}
	}
	if ball.P[2] < world.layout.Guardrail.Height+r &&
		(ball.P[0] < r-give || ball.P[0] > L-r+give || ball.P[1] < r-give || ball.P[1] > W-r+give) {
		return true
	}
	for _, static := range world.statics {
		if !static.Fuel {
			continue
		}
		box := static.Box
		if ball.P[2]+r < box.ZMin || ball.P[2]-r > box.ZMax {
			continue
		}
		dx := ball.P[0] - clamp(ball.P[0], box.MinX, box.MaxX)
		dy := ball.P[1] - clamp(ball.P[1], box.MinY, box.MaxY)
		if dx*dx+dy*dy < (r-give)*(r-give) {
			return true
		}
	}
	return false
}

// openCarpet reports whether a point on the field is clear of solid field elements (within margin) and walls.
func (world *World) openCarpet(p Vec2, margin float64) bool {
	L, W := world.layout.Field.Length, world.layout.Field.Width
	if p.X < margin || p.X > L-margin || p.Y < margin || p.Y > W-margin {
		return false
	}
	for _, static := range world.statics {
		box := static.Box
		if p.X > box.MinX-margin && p.X < box.MaxX+margin && p.Y > box.MinY-margin && p.Y < box.MaxY+margin {
			return false
		}
	}
	return true
}
