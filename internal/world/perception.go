package world

import "math"

// Limited sight. A drive team only knows about FUEL it can see: through the robot's intake camera (a cone ahead of the
// robot plus whatever is right around the bumpers) and, in teleop, through the drivers' own eyes from the alliance
// wall — reliable nearby, only big piles far away, and nothing hidden behind a hub. What it saw is remembered, and the
// memory fades. With the world's perfectInfo switch on, every drive team sees everything.

const (
	beliefCell      = 0.5
	beliefRefresh   = 0.2  // seconds between sight updates
	beliefMemory    = 30.0 // seconds a remembered pile is trusted
	cameraRange     = 5.5
	cameraHalfAngle = 62 * math.Pi / 180
	bumperSense     = 1.3
	driverClear     = 9.0  // the drivers see single balls this far from the wall
	driverPiles     = 14.0 // ...and piles of 3+ this far
	driverEyeHeight = 1.7
)

type belief struct {
	w, h      int
	count     []int16
	seen      []float64
	visible   []bool
	updatedAt float64
}

func newBelief(world *World) *belief {
	w := int(math.Ceil(world.layout.Field.Length / beliefCell))
	h := int(math.Ceil(world.layout.Field.Width / beliefCell))
	b := &belief{w: w, h: h, count: make([]int16, w*h), seen: make([]float64, w*h), visible: make([]bool, w*h),
		updatedAt: -100}
	for i := range b.seen {
		b.seen[i] = -100
	}
	return b
}

func (b *belief) cell(p Vec2) int {
	cx := clampInt(int(p.X/beliefCell), 0, b.w-1)
	cy := clampInt(int(p.Y/beliefCell), 0, b.h-1)
	return cy*b.w + cx
}

func (b *belief) center(i int) Vec2 {
	return Vec2{(float64(i%b.w) + 0.5) * beliefCell, (float64(i/b.w) + 0.5) * beliefCell}
}

// fuelCounts is the true number of free floor FUEL per belief cell (computed once per physics step).
func (world *World) fuelCounts() []int16 {
	if world.fuelCountsAt == world.steps && world.fuelCountsCache != nil {
		return world.fuelCountsCache
	}
	w := int(math.Ceil(world.layout.Field.Length / beliefCell))
	h := int(math.Ceil(world.layout.Field.Width / beliefCell))
	if len(world.fuelCountsCache) != w*h {
		world.fuelCountsCache = make([]int16, w*h)
	}
	counts := world.fuelCountsCache
	for i := range counts {
		counts[i] = 0
	}
	world.FreeBalls(func(p Vec2, z float64) {
		cx := clampInt(int(p.X/beliefCell), 0, w-1)
		cy := clampInt(int(p.Y/beliefCell), 0, h-1)
		counts[cy*w+cx]++
	})
	world.fuelCountsAt = world.steps
	return counts
}

// perceive refreshes what this drive team knows.
func (agent *Agent) perceive(robot *Robot, world *World) {
	if world.perfectInfo {
		return
	}
	if agent.belief == nil {
		agent.belief = newBelief(world)
	}
	b := agent.belief
	if world.time-b.updatedAt < beliefRefresh {
		return
	}
	b.updatedAt = world.time
	truth := world.fuelCounts()
	driver, driverOK := agent.driverPosition(robot, world)
	forward := robot.forward()
	for i := range b.count {
		p := b.center(i)
		offset := p.Sub(robot.Pos)
		distance := offset.Len()
		visible := distance < bumperSense
		if !visible && distance < cameraRange {
			angle := math.Acos(clamp(offset.Dot(forward)/math.Max(distance, 1e-6), -1, 1))
			visible = angle < cameraHalfAngle && !world.viewBlocked(robot.Pos, p)
		}
		if !visible && driverOK {
			d := p.Sub(driver).Len()
			if d < driverClear || (d < driverPiles && truth[i] >= 3) {
				visible = !world.viewBlocked(driver, p)
			}
		}
		b.visible[i] = visible
		if visible {
			b.count[i] = truth[i]
			b.seen[i] = world.time
		}
	}
}

// driverPosition is where this team's drivers stand (teleop only; in auto the robot is on its own).
func (agent *Agent) driverPosition(robot *Robot, world *World) (Vec2, bool) {
	if robot.Auto {
		return Vec2{}, false
	}
	l := world.layout
	ys := []float64{6.0, 4.035, 2.1}
	x, y := l.onAlliance(robot.Alliance, -0.6, ys[robot.Index%3])
	return Vec2{x, y}, true
}

// viewBlocked reports whether a hub stands between an eye and a floor point.
func (world *World) viewBlocked(eye, p Vec2) bool {
	for _, hub := range world.hubs {
		half := world.layout.Hub.Size / 2
		box := AABB{hub.Center.X - half, hub.Center.Y - half, hub.Center.X + half, hub.Center.Y + half, 0, 0}
		if box.Contains(p) {
			continue // FUEL at the hub itself is visible
		}
		if segmentHitsBox(eye, p, box) {
			return true
		}
	}
	return false
}

// segmentHitsBox is a slab test for a 2-D segment against an axis-aligned box.
func segmentHitsBox(a, b Vec2, box AABB) bool {
	tMin, tMax := 0.0, 1.0
	d := b.Sub(a)
	for axis := 0; axis < 2; axis++ {
		origin, delta, lo, hi := a.X, d.X, box.MinX, box.MaxX
		if axis == 1 {
			origin, delta, lo, hi = a.Y, d.Y, box.MinY, box.MaxY
		}
		if math.Abs(delta) < 1e-9 {
			if origin < lo || origin > hi {
				return false
			}
			continue
		}
		t1, t2 := (lo-origin)/delta, (hi-origin)/delta
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		tMin, tMax = math.Max(tMin, t1), math.Min(tMax, t2)
		if tMin > tMax {
			return false
		}
	}
	return true
}

// knownBalls calls fn for the FUEL this drive team knows about: exact positions for what it sees right now, and the
// remembered piles (at their cell centres, weighted by how many it saw) elsewhere.
func (agent *Agent) knownBalls(robot *Robot, world *World, fn func(p Vec2, weight int)) {
	if world.perfectInfo || agent.belief == nil {
		world.FreeBalls(func(p Vec2, z float64) { fn(p, 1) })
		return
	}
	b := agent.belief
	world.FreeBalls(func(p Vec2, z float64) {
		if b.visible[b.cell(p)] {
			fn(p, 1)
		}
	})
	for i, count := range b.count {
		if b.visible[i] || count <= 0 || world.time-b.seen[i] > beliefMemory {
			continue
		}
		fn(b.center(i), int(count))
	}
}
