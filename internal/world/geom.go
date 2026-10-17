package world

import "math"

type Vec2 struct{ X, Y float64 }

func (a Vec2) Add(b Vec2) Vec2      { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2      { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Scale(s float64) Vec2 { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float64   { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Cross(b Vec2) float64 { return a.X*b.Y - a.Y*b.X }
func (a Vec2) Len() float64         { return math.Hypot(a.X, a.Y) }
func (a Vec2) Rotate(t float64) Vec2 {
	c, s := math.Cos(t), math.Sin(t)
	return Vec2{a.X*c - a.Y*s, a.X*s + a.Y*c}
}
func (a Vec2) Norm() Vec2 {
	length := a.Len()
	if length < 1e-9 {
		return Vec2{}
	}
	return a.Scale(1 / length)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func wrapAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// AABB is an axis-aligned rectangle in the field plane with a vertical extent.
type AABB struct {
	MinX, MinY, MaxX, MaxY float64
	ZMin, ZMax             float64
}

func (box AABB) Center() Vec2 { return Vec2{(box.MinX + box.MaxX) / 2, (box.MinY + box.MaxY) / 2} }
func (box AABB) Half() Vec2   { return Vec2{(box.MaxX - box.MinX) / 2, (box.MaxY - box.MinY) / 2} }
func (box AABB) Contains(p Vec2) bool {
	return p.X >= box.MinX && p.X <= box.MaxX && p.Y >= box.MinY && p.Y <= box.MaxY
}

// OBB is an oriented rectangle (a robot footprint).
type OBB struct {
	Center Vec2
	Half   Vec2
	Angle  float64
}

func (obb OBB) axes() [2]Vec2 {
	c, s := math.Cos(obb.Angle), math.Sin(obb.Angle)
	return [2]Vec2{{c, s}, {-s, c}}
}

// project returns the half-extent of the box projected onto a unit axis.
func (obb OBB) project(axis Vec2) float64 {
	axes := obb.axes()
	return obb.Half.X*math.Abs(axes[0].Dot(axis)) + obb.Half.Y*math.Abs(axes[1].Dot(axis))
}

func aabbAsOBB(box AABB) OBB {
	return OBB{Center: box.Center(), Half: box.Half()}
}

// SAT returns the minimum translation vector that pushes a out of b (zero vector and false if not overlapping).
func SAT(a, b OBB) (Vec2, bool) {
	axesA := a.axes()
	axesB := b.axes()
	delta := a.Center.Sub(b.Center)
	best := math.Inf(1)
	var bestAxis Vec2
	for _, axis := range [4]Vec2{axesA[0], axesA[1], axesB[0], axesB[1]} {
		distance := delta.Dot(axis)
		overlap := a.project(axis) + b.project(axis) - math.Abs(distance)
		if overlap <= 0 {
			return Vec2{}, false
		}
		if overlap < best {
			best = overlap
			if distance < 0 {
				bestAxis = axis.Scale(-1)
			} else {
				bestAxis = axis
			}
		}
	}
	return bestAxis.Scale(best), true
}

// closestPointOBB returns the closest point on (or in) the box to p.
func closestPointOBB(box OBB, p Vec2) Vec2 {
	axes := box.axes()
	d := p.Sub(box.Center)
	u := clamp(d.Dot(axes[0]), -box.Half.X, box.Half.X)
	v := clamp(d.Dot(axes[1]), -box.Half.Y, box.Half.Y)
	return box.Center.Add(axes[0].Scale(u)).Add(axes[1].Scale(v))
}

// circleOBB returns the push-out vector for a circle overlapping a box, and the contact normal.
func circleOBB(center Vec2, radius float64, box OBB) (Vec2, Vec2, bool) {
	closest := closestPointOBB(box, center)
	d := center.Sub(closest)
	distance := d.Len()
	if distance > radius {
		return Vec2{}, Vec2{}, false
	}
	if distance < 1e-9 {
		// Center is inside the box: push out along the shallowest local axis.
		axes := box.axes()
		local := center.Sub(box.Center)
		u := local.Dot(axes[0])
		v := local.Dot(axes[1])
		pu := box.Half.X - math.Abs(u)
		pv := box.Half.Y - math.Abs(v)
		if pu < pv {
			n := axes[0].Scale(math.Copysign(1, u))
			return n.Scale(pu + radius), n, true
		}
		n := axes[1].Scale(math.Copysign(1, v))
		return n.Scale(pv + radius), n, true
	}
	normal := d.Scale(1 / distance)
	return normal.Scale(radius - distance), normal, true
}
