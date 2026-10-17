package world

import "math"

// The robot's body out of the carpet plane: riding the bumps on its four wheels (with a little tire and frame give),
// the belly pan touching a bump's crest (high-centering: the wheels lose their load and with it their grip), and
// tipping up about the edge of its wheelbase when acceleration, a slope or a hit pushes its center of gravity past
// that edge. Tipping is physical: a robot goes over only when its center of gravity actually crosses the edge.

const (
	suspensionHz    = 7.0  // natural frequency of the chassis on its tires
	suspensionZeta  = 0.55 // damping ratio
	bellyFriction   = 0.35 // a belly pan sliding on HDPE
	bellyLoadPerM   = 1 / 0.012
	maxBellyLoad    = 0.88
	tiltRestitution = 0.25 // a robot dropping back onto its wheels barely bounces
)

// stepBody updates the robot's height, pitch and roll from the ground under its wheels, the belly contact, and the
// tipping dynamics. It runs after the planar step.
func (world *World) stepBody(robot *Robot, dt float64) {
	drive := world.drivetrain(robot)
	if robot.Faults.Tipped {
		robot.Roll += (1.45 - robot.Roll) * math.Min(1, dt*6)
		robot.Z = 0.05
		robot.tilt, robot.tiltRate, robot.bellyLoad = 0, 0, 0
		return
	}
	if robot.Climbing || robot.ClimbLevel > 0 {
		robot.tilt, robot.tiltRate, robot.bellyLoad = 0, 0, 0
		return
	}
	contact := world.chassisContact(robot.Pos, robot.Heading, drive.Half, drive.Clearance)
	z0, gx, gy, penetration := contact.z, contact.gx, contact.gy, contact.penetration
	z0 += penetration
	robot.bellyLoad = math.Min(maxBellyLoad, penetration*bellyLoadPerM)
	if robot.bellyLoad > 0.5 && !robot.highCentered {
		robot.highCentered = true
		world.noteIncident(robot, "high-centered", true)
		world.logf("warn", "world", "%d (%s) is high-centered on a bump", robot.Team, robot.Station)
	} else if robot.bellyLoad < 0.1 {
		robot.highCentered = false
	}

	// Tipping about an edge of the wheelbase, from the moments about that edge (d'Alembert): the slope's pull and
	// the reaction to the robot's acceleration act at the center of gravity, a contact force acts at bumper height,
	// and the weight holds it down. Wheel forces act at the carpet, so they tip nothing directly; they only matter
	// through the acceleration they cause. A hit at bumper height, below the center of gravity, therefore rocks a
	// robot toward whatever hit it unless its wheels hold, and a hard stop against a wall lifts its far side.
	m := drive.Mass
	_, slope := world.groundHeight(robot.Pos)
	cosSlope := 1 / math.Sqrt(1+slope*slope)
	contactForce := robot.contactImpulse.Scale(1 / dt).Rotate(-robot.Heading)
	accel := robot.accel.Rotate(-robot.Heading).Add(contactForce.Scale(1 / m))
	robot.contactImpulse = Vec2{}
	lean := Vec2{-slope * gravity * cosSlope, 0}.Rotate(-robot.Heading).Sub(accel)
	upright := gravity * cosSlope
	h, d, hb := drive.CGHeight, drive.Half, drive.BumperHeight
	critical := math.Atan2(d, h)
	edgeInertia := m*(h*h+d*d) + m*(math.Pow(2*d, 2)+math.Pow(2*h, 2))/12
	moment := func(e Vec2, phi float64) float64 {
		c, s := math.Cos(phi), math.Sin(phi)
		return m*lean.Dot(e)*(h*c+d*s) + contactForce.Dot(e)*(hb*c+d*s) - m*upright*(d*c-h*s)
	}
	if robot.tilt <= 0 && robot.tiltRate <= 0 {
		// On all four wheels: start tipping over the edge with the largest positive moment, if any.
		for _, e := range [...]Vec2{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if moment(e, 0) > 0 && (robot.tiltRate <= 0 || moment(e, 0) > moment(robot.tiltAxisLocal, 0)) {
				robot.tiltAxisLocal = e
				robot.tiltRate = 1e-6
			}
		}
	}
	if robot.tilt > 0 || robot.tiltRate > 0 {
		robot.tiltRate += moment(robot.tiltAxisLocal, robot.tilt) / edgeInertia * dt
		robot.tilt += robot.tiltRate * dt
		if robot.tilt <= 0 {
			robot.tilt = 0
			robot.tiltRate = -robot.tiltRate * tiltRestitution
			if robot.tiltRate < 0.05 {
				robot.tiltRate = 0
			}
		}
		if !world.tipping && robot.tilt > critical*0.6 {
			robot.tilt, robot.tiltRate = critical*0.6, math.Min(0, robot.tiltRate) // tipping turned off
		}
		if robot.tilt > critical {
			robot.Faults.Tipped = true
			robot.tilt, robot.tiltRate = 0, 0
			world.noteIncident(robot, "tipped", true)
			world.logf("warn", "world", "%d (%s) tipped over", robot.Team, robot.Station)
			return
		}
	}

	// Target pose: the chassis plane, plus the tilt about its edge.
	targetZ := z0
	targetPitch := -math.Atan(gx) // nose down is positive pitch (the slope rising ahead tips the nose up)
	targetRoll := math.Atan(-gy)  // ground rising on the right (−y) rolls the right side up
	if robot.tilt > 0 {
		e := robot.tiltAxisLocal
		targetPitch += robot.tilt * e.X
		targetRoll += robot.tilt * e.Y
		targetZ += d*math.Sin(robot.tilt) + h*(math.Cos(robot.tilt)-1)
	}
	// The chassis follows on its tires: a damped spring, so it settles with a small bounce off a bump.
	omega := 2 * math.Pi * suspensionHz
	spring := func(value, rate *float64, target float64) {
		accel := omega*omega*(target-*value) - 2*suspensionZeta*omega**rate
		*rate += accel * dt
		*value += *rate * dt
	}
	spring(&robot.Z, &robot.zRate, targetZ)
	if robot.tilt > 0 {
		robot.Pitch, robot.Roll = targetPitch, targetRoll // rigid rotation about the edge
	} else {
		spring(&robot.Pitch, &robot.pitchRate, targetPitch)
		spring(&robot.Roll, &robot.rollRate, targetRoll)
	}
}

// chassisFit is how a chassis sits on the ground: the height and gradients (along the robot's x and y) of the plane
// through its wheels, and how far the ground pokes above its belly pan.
type chassisFit struct {
	z, gx, gy, penetration float64
}

// chassisContact fits a chassis with its modules half apart at pos/heading to the ground: a least-squares plane
// through the four contact patches, lifted so that no wheel sinks in (a rigid frame rests on its highest wheels),
// then the ground under the frame sampled against the belly pan clearance above that plane.
func (world *World) chassisContact(pos Vec2, heading, half, clearance float64) chassisFit {
	forward := Vec2{math.Cos(heading), math.Sin(heading)}
	left := Vec2{-forward.Y, forward.X}
	toWorld := func(r Vec2) Vec2 { return pos.Add(forward.Scale(r.X)).Add(left.Scale(r.Y)) }
	offsets := [4]Vec2{{half, half}, {half, -half}, {-half, half}, {-half, -half}}
	var heights [4]float64
	var fit chassisFit
	for i, r := range offsets {
		heights[i], _ = world.groundHeight(toWorld(r))
		fit.z += heights[i] / 4
		fit.gx += heights[i] * r.X
		fit.gy += heights[i] * r.Y
	}
	d2 := 4 * half * half
	fit.gx, fit.gy = fit.gx/d2, fit.gy/d2
	plane := func(r Vec2) float64 { return fit.z + fit.gx*r.X + fit.gy*r.Y }
	lift := 0.0
	for i, r := range offsets {
		lift = math.Max(lift, heights[i]-plane(r))
	}
	fit.z += lift
	for _, fx := range [...]float64{-0.8, -0.6, -0.4, -0.2, 0, 0.2, 0.4, 0.6, 0.8} {
		for _, fy := range [...]float64{-0.6, 0, 0.6} {
			r := Vec2{fx * half, fy * half}
			ground, _ := world.groundHeight(toWorld(r))
			fit.penetration = math.Max(fit.penetration, ground-(plane(r)+clearance))
		}
	}
	return fit
}

// bumpClearanceNeeded is the belly-pan clearance a chassis with its modules half apart needs to drive over a bump
// at any angle without the crest touching its belly (found by sliding it across the bump; cached per size).
func (world *World) bumpClearanceNeeded(half float64) float64 {
	key := int(math.Round(half * 1000))
	if need, ok := world.clearanceNeeded[key]; ok {
		return need
	}
	need := 0.0
	if len(world.bumps) > 0 {
		bump := world.bumps[0]
		center := bump.Center()
		for angle := 0.0; angle <= math.Pi/4+1e-9; angle += math.Pi / 36 {
			for x := bump.MinX - half*1.5; x <= bump.MaxX+half*1.5; x += 0.005 {
				fit := world.chassisContact(Vec2{x, center.Y}, angle, half, 0)
				need = math.Max(need, fit.penetration)
			}
		}
	}
	if world.clearanceNeeded == nil {
		world.clearanceNeeded = map[int]float64{}
	}
	world.clearanceNeeded[key] = need
	return need
}
