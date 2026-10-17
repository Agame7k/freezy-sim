package world

import (
	"math"
	"sync"
)

// FUEL inside robots. A hopper is a box in the robot's frame (x forward, y left, z up) running from just behind the
// intake to the back of the frame, under the shooter at the back (or the turret deck). Every FUEL in it is simulated:
// it falls onto the indexer floor and the FUEL below, squeezes a little against its neighbours (FUEL is foam), and
// sloshes when the robot speeds up, brakes or turns. The shooter feeds the FUEL nearest its feeder, the intake run
// backwards ejects the one nearest the front. A design's capacity is what its hopper volume really holds.
//
// The 3D model draws the same box (web/js/scene/robots.js hopperShape) and the FUEL at the positions streamed here.

// hopperShape is a hopper's walls in the robot frame (not ball centers).
type hopperShape struct {
	back, front, halfWidth float64
	floor, ceiling         float64
	// The shooter sits over the back of the hopper: behind notchX the ceiling is notchTop.
	notchX, notchTop float64
}

const (
	fuelDiameter   = 0.150 // 5.91 in
	hopperSlop     = 0.012 // how far two FUEL may squeeze into each other before they push back hard
	hopperDamping  = 3.0   // 1/s: foam on foam and on polycarbonate soaks up motion
	hopperBounce   = 0.15
	maxSloshAccel  = 14.0 // m/s²: a hit is not a 10 g kick to the FUEL
	hopperFeedRoom = 0.2  // the shooter can only feed FUEL within this of its feeder (indexer belts bring the rest)
)

// hopperShape mirrors hopperShape in web/js/scene/robots.js.
func (arch Archetype) hopperShape() hopperShape {
	f := arch.Frame
	model := arch.Model
	top := math.Min(arch.Height, MaxRobotHeight)
	share := model.Hopper
	if share == 0 {
		share = 0.75
	}
	if model.Shooter == "kitbot" {
		// The KitBot's sheet-metal bin between its side panels, ahead of the shooter roller.
		return hopperShape{back: -0.36 * f, front: 0.26 * f, halfWidth: 0.27 * f, floor: 0.17, ceiling: top - 0.04,
			notchX: -1, notchTop: top}
	}
	shape := hopperShape{back: -f/2 + 0.04, front: f/2 - 0.1, halfWidth: f * share / 2, floor: 0.1, ceiling: top - 0.03,
		notchX: -1, notchTop: top}
	if model.HopperLength > 0 {
		shape.front = shape.back + (shape.front-shape.back)*model.HopperLength
	}
	if model.Bin {
		shape.halfWidth += 0.04
		shape.floor = 0.07
	}
	switch model.Shooter {
	case "turret":
		shape.ceiling = top - 0.17 // under the turntable deck
	case "small":
		shape.notchX, shape.notchTop = -f/2+0.32*0.7, top-0.26*0.7-0.01
	default:
		shape.notchX, shape.notchTop = -f/2+0.32, top-0.27
	}
	shape.notchTop = math.Max(shape.notchTop, shape.floor)
	return shape
}

// capacity is how many FUEL a hopper really holds, found the way a team finds out: fill it through the intake until
// the next FUEL no longer fits without squashing the pile (foam gives a centimeter or two, not more). Hopper shapes
// repeat across designs, so each is filled once.
func (shape hopperShape) capacity() int {
	key := [7]int{cm(shape.back), cm(shape.front), cm(shape.halfWidth), cm(shape.floor), cm(shape.ceiling),
		cm(shape.notchX), cm(shape.notchTop)}
	capacityMu.Lock()
	defer capacityMu.Unlock()
	if n, ok := capacityCache[key]; ok {
		return n
	}
	n := shape.fill()
	capacityCache[key] = n
	return n
}

var (
	capacityMu    sync.Mutex
	capacityCache = map[[7]int]int{}
)

const (
	maxHopperFuel = 150   // more than any legal robot holds (the 3D view draws up to 160)
	fullSqueeze   = 0.026 // m: past this the pile is crushing FUEL, so the last one did not fit
)

func (shape hopperShape) fill() int {
	r := fuelDiameter / 2
	if 2*shape.halfWidth < fuelDiameter || shape.ceiling-shape.floor < fuelDiameter*0.95 {
		return 0
	}
	var balls []*Ball
	for len(balls) < maxHopperFuel {
		ball := &Ball{}
		// In across the intake width, as the intake delivers it.
		across := (float64(len(balls)%5)/4 - 0.5) * 1.6 * shape.halfWidth
		ball.hopP = shape.entry(across)
		ball.hopV = [3]float64{-0.9, 0, -0.3}
		balls = append(balls, ball)
		const dt = 1.0 / stepHz
		for step := 0; step < stepHz/3; step++ {
			for _, b := range balls {
				b.hopV[2] -= gravity * dt
				for axis := 0; axis < 3; axis++ {
					b.hopV[axis] *= 1 - hopperDamping*dt
					b.hopP[axis] += b.hopV[axis] * dt
				}
			}
			shape.relax(balls, r)
		}
		if shape.worstSqueeze(balls, r) > fullSqueeze {
			return len(balls) - 1
		}
	}
	return maxHopperFuel
}

func (shape hopperShape) worstSqueeze(balls []*Ball, r float64) float64 {
	worst := 0.0
	for i, a := range balls {
		for _, b := range balls[i+1:] {
			dx, dy, dz := a.hopP[0]-b.hopP[0], a.hopP[1]-b.hopP[1], a.hopP[2]-b.hopP[2]
			worst = math.Max(worst, 2*r-math.Sqrt(dx*dx+dy*dy+dz*dz))
		}
	}
	return worst
}

// ceilingAt is the hopper's ceiling over a point.
func (shape hopperShape) ceilingAt(x float64) float64 {
	if x < shape.notchX {
		return shape.notchTop
	}
	return shape.ceiling
}

// hopperEntry is where FUEL drops in off the intake conveyor: over the front wall, high up.
func (shape hopperShape) entry(across float64) [3]float64 {
	r := fuelDiameter / 2
	return [3]float64{shape.front - r - 0.01, clamp(across*0.6, -shape.halfWidth+r, shape.halfWidth-r),
		shape.ceiling - r}
}

// feeder is where the shooter takes FUEL from: the back of the indexer floor (the middle for a turret, which feeds
// up through its turntable; the KitBot rolls FUEL back to its roller).
func (shape hopperShape) feeder(arch Archetype) [3]float64 {
	r := fuelDiameter / 2
	if arch.Turret {
		return [3]float64{-arch.Frame * 0.18, 0, shape.floor + r}
	}
	return [3]float64{shape.back + r, 0, shape.floor + r}
}

// stepHoppers moves the FUEL inside every robot.
func (world *World) stepHoppers(dt float64) {
	var held [6][]*Ball
	for _, ball := range world.balls {
		if ball.State == BallHeld && ball.intakeT >= 1 && ball.Owner >= 0 && ball.Owner < len(held) {
			held[ball.Owner] = append(held[ball.Owner], ball)
		}
	}
	for i, robot := range world.robots {
		balls := held[i]
		accel := robot.Vel.Sub(robot.hopperVel).Scale(1 / dt)
		robot.hopperVel = robot.Vel
		if len(balls) == 0 {
			continue
		}
		world.stepHopper(robot, balls, accel, dt)
	}
}

func (world *World) stepHopper(robot *Robot, balls []*Ball, accel Vec2, dt float64) {
	shape := robot.Arch.hopperShape()
	r := world.ballRadius
	forward := robot.forward()
	left := Vec2{-forward.Y, forward.X}
	// The FUEL feels the robot's acceleration backwards (and its spin outwards).
	if accel.Len() > maxSloshAccel {
		accel = accel.Norm().Scale(maxSloshAccel)
	}
	ax, ay := -accel.Dot(forward), -accel.Dot(left)
	spin := robot.Omega * robot.Omega
	damping := math.Max(0, 1-hopperDamping*dt)
	for _, ball := range balls {
		p, v := &ball.hopP, &ball.hopV
		v[0] += (ax + spin*p[0]) * dt
		v[1] += (ay + spin*p[1]) * dt
		v[2] -= gravity * dt
		for axis := 0; axis < 3; axis++ {
			v[axis] *= damping
			p[axis] += v[axis] * dt
		}
	}
	shape.relax(balls, r)
	// Where the FUEL is in the world (the robot carries it).
	for _, ball := range balls {
		p := robot.Pos.Add(forward.Scale(ball.hopP[0])).Add(left.Scale(ball.hopP[1]))
		ball.P = [3]float64{p.X, p.Y, robot.Z + ball.hopP[2]}
		ball.V = [3]float64{robot.Vel.X, robot.Vel.Y, 0}
	}
}

// relax resolves the contacts in a hopper: FUEL against FUEL (soft: a little squeeze is free, more pushes back),
// then the walls. Four passes keep a full hopper's pile settled.
func (shape hopperShape) relax(balls []*Ball, r float64) {
	diameter := 2 * r
	for pass := 0; pass < 4; pass++ {
		for i := 0; i < len(balls); i++ {
			a := balls[i]
			for j := i + 1; j < len(balls); j++ {
				b := balls[j]
				dx, dy, dz := b.hopP[0]-a.hopP[0], b.hopP[1]-a.hopP[1], b.hopP[2]-a.hopP[2]
				if math.Abs(dx) >= diameter || math.Abs(dy) >= diameter || math.Abs(dz) >= diameter {
					continue
				}
				d2 := dx*dx + dy*dy + dz*dz
				if d2 >= diameter*diameter {
					continue
				}
				d := math.Sqrt(d2)
				var n [3]float64
				if d < 1e-6 {
					// In exactly the same spot: part them sideways (a different way for each pair).
					angle := float64(i*7+j*13) * 0.618 * 2 * math.Pi
					n, d = [3]float64{math.Cos(angle), math.Sin(angle), 0}, 1e-6
				} else {
					n = [3]float64{dx / d, dy / d, dz / d}
				}
				overlap := diameter - d
				push := 0.5 * (math.Max(0, overlap-hopperSlop) + 0.3*math.Min(overlap, hopperSlop))
				relative := 0.0
				for axis := 0; axis < 3; axis++ {
					a.hopP[axis] -= n[axis] * push
					b.hopP[axis] += n[axis] * push
					relative += (b.hopV[axis] - a.hopV[axis]) * n[axis]
				}
				if relative < 0 {
					impulse := -(1 + hopperBounce) * relative / 2
					for axis := 0; axis < 3; axis++ {
						a.hopV[axis] -= impulse * n[axis]
						b.hopV[axis] += impulse * n[axis]
					}
				}
			}
		}
		for _, ball := range balls {
			shape.contain(ball, r)
		}
	}
}

// contain keeps a FUEL inside the hopper walls (it may squeeze a few millimeters into them).
func (shape hopperShape) contain(ball *Ball, r float64) {
	p, v := &ball.hopP, &ball.hopV
	const give = 0.004
	wall := func(axis int, low, high float64) {
		if low > high {
			low, high = (low+high)/2, (low+high)/2
		}
		if p[axis] < low {
			p[axis] = low
			if v[axis] < 0 {
				v[axis] = -v[axis] * hopperBounce
			}
		} else if p[axis] > high {
			p[axis] = high
			if v[axis] > 0 {
				v[axis] = -v[axis] * hopperBounce
			}
		}
	}
	wall(0, shape.back+r-give, shape.front-r+give)
	wall(1, -shape.halfWidth+r-give, shape.halfWidth-r+give)
	wall(2, shape.floor+r-give, shape.ceiling-r+give)
	// The shooter over the back is a solid block (x < notchX, z > notchTop): FUEL rolls around its corner and under
	// it, the way a ball meets a real edge, rather than meeting an invisible wall.
	if shape.notchX <= shape.back {
		return
	}
	reach := r - give
	if p[0] < shape.notchX && p[2] > shape.notchTop {
		// Center inside the block: out the nearer way, down under it or forward out of it.
		if down, forward := p[2]-shape.notchTop+reach, shape.notchX-p[0]+reach; down < forward {
			p[2] -= down
			v[2] = math.Min(v[2], 0)
		} else {
			p[0] += forward
			v[0] = math.Max(v[0], 0)
		}
		return
	}
	cx, cz := math.Min(p[0], shape.notchX), math.Max(p[2], shape.notchTop)
	dx, dz := p[0]-cx, p[2]-cz
	d2 := dx*dx + dz*dz
	if d2 >= reach*reach || d2 < 1e-12 {
		return
	}
	d := math.Sqrt(d2)
	nx, nz := dx/d, dz/d
	p[0] += nx * (reach - d)
	p[2] += nz * (reach - d)
	if into := v[0]*nx + v[2]*nz; into < 0 {
		v[0] -= (1 + hopperBounce) * into * nx
		v[2] -= (1 + hopperBounce) * into * nz
	}
}

// enterHopper puts a FUEL that has come up the intake conveyor into the hopper.
func (robot *Robot) enterHopper(ball *Ball) {
	shape := robot.Arch.hopperShape()
	ball.hopP = shape.entry(ball.intakeFrom[1])
	ball.hopV = [3]float64{-0.9, 0, -0.3} // tossed back off the top roller
}

// heldBallNear returns the FUEL in a robot's hopper nearest a point in the robot frame (FUEL still on its way in only
// if nothing else is held), and whether it is close enough to reach.
func (world *World) heldBallNear(robot *Robot, point [3]float64) (*Ball, bool) {
	var best, moving *Ball
	bestDistance := math.Inf(1)
	for _, ball := range world.balls {
		if ball.State != BallHeld || ball.Owner != robot.Index {
			continue
		}
		if ball.intakeT < 1 {
			moving = ball
			continue
		}
		dx, dy, dz := ball.hopP[0]-point[0], ball.hopP[1]-point[1], ball.hopP[2]-point[2]
		if d := dx*dx + dy*dy + dz*dz; d < bestDistance {
			best, bestDistance = ball, d
		}
	}
	if best == nil {
		return moving, moving != nil
	}
	return best, math.Sqrt(bestDistance) < hopperFeedRoom+fuelDiameter
}

// takeHeldBall returns the FUEL the shooter feeds next: the one nearest its feeder.
func (world *World) takeHeldBall(robot *Robot) *Ball {
	ball, _ := world.heldBallNear(robot, robot.Arch.hopperShape().feeder(robot.Arch))
	return ball
}

const intakeTransit = 0.35 // seconds from the rollers over the front wall into the hopper

// stepIntakes moves FUEL that a robot just picked up: over the intake rollers and up the conveyor, then drops it into
// the hopper. It is part of the robot from the moment it is picked up.
func (world *World) stepIntakes(dt float64) {
	for _, ball := range world.balls {
		if ball.State != BallHeld || ball.intakeT >= 1 {
			continue
		}
		robot := world.robots[ball.Owner]
		ball.intakeT = math.Min(1, ball.intakeT+dt/intakeTransit)
		ball.P = robot.intakePath(ball, world.ballRadius)
		if ball.intakeT >= 1 {
			robot.enterHopper(ball)
		}
	}
}

// intakePath is where a FUEL being pulled in sits at its current progress (world coordinates).
func (robot *Robot) intakePath(ball *Ball, r float64) [3]float64 {
	half := robot.half()
	roller := [3]float64{half.X - 0.02, ball.intakeFrom[1] * 0.6, 0.2}
	target := robot.Arch.hopperShape().entry(ball.intakeFrom[1])
	t := ball.intakeT
	var local [3]float64
	if t < 0.35 {
		k := t / 0.35
		for axis := 0; axis < 3; axis++ {
			local[axis] = ball.intakeFrom[axis] + (roller[axis]-ball.intakeFrom[axis])*k
		}
	} else {
		k := (t - 0.35) / 0.65
		for axis := 0; axis < 3; axis++ {
			local[axis] = roller[axis] + (target[axis]-roller[axis])*k
		}
	}
	forward := robot.forward()
	left := Vec2{-forward.Y, forward.X}
	p := robot.Pos.Add(forward.Scale(local[0])).Add(left.Scale(local[1]))
	return [3]float64{p.X, p.Y, robot.Z + local[2]}
}

// packedHopper is how many of a robot's FUEL are already in its hopper (not still on their way in).
func (world *World) packedHopper(robot *Robot) int {
	moving := 0
	for _, ball := range world.balls {
		if ball.State == BallHeld && ball.Owner == robot.Index && ball.intakeT < 1 {
			moving++
		}
	}
	return robot.Hopper - moving
}

// hopperContents lists where a robot's packed FUEL sits in its frame (for the 3D view).
func (world *World) hopperContents(robot *Robot) [][3]float64 {
	var out [][3]float64
	for _, ball := range world.balls {
		if ball.State == BallHeld && ball.Owner == robot.Index && ball.intakeT >= 1 {
			out = append(out, ball.hopP)
		}
	}
	return out
}

// loadHopper puts a FUEL straight into a robot's hopper (a preload): the k-th one rests on the floor, then the next
// layer, spread over the hopper so the pile starts settled.
func (robot *Robot) loadHopper(ball *Ball, k int) {
	shape := robot.Arch.hopperShape()
	r := fuelDiameter / 2
	nx := max(1, int((shape.front-shape.back)/fuelDiameter))
	ny := max(1, int(2*shape.halfWidth/fuelDiameter))
	layer, cell := k/(nx*ny), k%(nx*ny)
	ball.hopP = [3]float64{
		shape.front - r - float64(cell%nx)*fuelDiameter - float64(layer%2)*r,
		-shape.halfWidth + r + float64(cell/nx)*fuelDiameter + float64(layer%2)*r*0.5,
		shape.floor + r + float64(layer)*fuelDiameter*0.8,
	}
	ball.hopV = [3]float64{}
	ball.intakeT = 1
	shape.contain(ball, r)
}

// landOnRobot handles a FUEL dropping onto a robot from above. Over the open hopper (with room) it falls in, as
// FUEL does; elsewhere on the robot it rests on top and rolls off. It reports whether the FUEL was over the robot.
func (world *World) landOnRobot(robot *Robot, ball *Ball, offset, forward, left Vec2, top float64) bool {
	r := world.ballRadius
	along, across := offset.Dot(forward), offset.Dot(left)
	half := robot.half()
	if math.Abs(along) > half.X || math.Abs(across) > half.Y {
		return false
	}
	shape := robot.Arch.hopperShape()
	if along > shape.back+r && along < shape.front-r && math.Abs(across) < shape.halfWidth-r &&
		along > shape.notchX && robot.Hopper < robot.Arch.Capacity {
		ball.State, ball.Owner, ball.thrower, ball.fromHub = BallHeld, robot.Index, 0, false
		ball.intakeT = 1
		ball.hopP = [3]float64{along, across, math.Min(shape.ceiling-r, ball.P[2]-robot.Z)}
		vx, vy := ball.V[0]-robot.Vel.X, ball.V[1]-robot.Vel.Y
		ball.hopV = [3]float64{vx*forward.X + vy*forward.Y, vx*left.X + vy*left.Y, ball.V[2]}
		robot.Hopper++
		robot.Collected++
		return true
	}
	// On top of the robot: bounce, then roll off with the robot's motion and a little nudge outward.
	ball.P[2] = top + r
	if ball.V[2] < 0 {
		ball.V[2] = -ball.V[2] * groundBounce
	}
	out := offset.Norm()
	ball.V[0] += (robot.Vel.X-ball.V[0])*0.2 + out.X*0.08
	ball.V[1] += (robot.Vel.Y-ball.V[1])*0.2 + out.Y*0.08
	ball.wake()
	return true
}
