package world

import "math"

// Odometry is what the robot's own code believes about where it is. Real robots integrate their wheel encoders and
// gyro: the estimate starts at the pose the auto routine was written for and drifts — wheels slip when they
// accelerate hard and on the bumps, tank wheels scrub in turns, the wheel diameter is never calibrated exactly, the gyro
// creeps, and nothing on the robot notices being shoved by another robot. Teams with AprilTag cameras pull the estimate
// back toward the truth whenever a tag is in view. Autonomous routines drive on this estimate, and shots aimed from it
// (every auto shot, a turret aiming off its pose in teleop) miss by however wrong it is.

const (
	visionEvery     = 0.1 // seconds between camera pose updates
	visionRange     = 4.8 // metres a tag is read reliably
	visionHalfFOV   = 38 * math.Pi / 180
	visionMaxSpeed  = 3.2 // m/s: faster than this the tag image smears
	visionMaxOmega  = 3.0 // rad/s
	placementSigma  = 0.03
	placementYawSD  = 0.02
	odoGyroBiasMax  = 0.0008 // rad/s for a modern IMU
	odoScaleBaseMax = 0.02   // wheel calibration error before the team tunes it
)

// Odometry is a robot's pose estimate and the hardware that produces it.
type Odometry struct {
	Est        Vec2    `json:"est"`
	EstHeading float64 `json:"estHeading"`
	Vision     bool    `json:"vision"`   // AprilTag cameras front and back
	TagsSeen   int     `json:"tagsSeen"` // tags in view at the last camera update
	LastFix    float64 `json:"lastFix"`  // world time of the last vision correction
	scale      float64 // wheel calibration error (fraction of distance)
	gyroBias   float64 // rad/s
	visionAt   float64
}

// odoError is how far the robot's estimate is from where it really is.
func (robot *Robot) odoError() float64 { return robot.Odo.Est.Sub(robot.Pos).Len() }

// odoHeadingError is estimated minus true heading.
func (robot *Robot) odoHeadingError() float64 { return wrapAngle(robot.Odo.EstHeading - robot.Heading) }

// teamHasVision reports whether a team runs AprilTag pose estimation (most good teams do; few kitbots).
func teamHasVision(team int, arch Archetype) bool {
	chance := 0.75
	switch arch.Class {
	case "kitbot":
		chance = 0.15
	case "defender":
		chance = 0.35
	case "volley", "bigdumper", "shuttle":
		chance = 0.6
	}
	return hashUnit(team, 0x7a65) < chance
}

// resetOdometry is the robot code zeroing its pose at the start of auto: it believes it sits exactly at the pose the
// routine was written for. calibration is how much of the wheel calibration error the team has tuned out (0..1).
func (world *World) resetOdometry(robot *Robot, pose Vec2, heading, calibration float64) {
	odo := &robot.Odo
	odo.Est, odo.EstHeading = pose, heading
	odo.Vision = teamHasVision(robot.Team, robot.Arch)
	sign := 1.0
	if hashUnit(robot.Team, 0x51a) < 0.5 {
		sign = -1
	}
	odo.scale = sign * (0.003 + odoScaleBaseMax*hashUnit(robot.Team, 0x0d0)) * (1 - clamp(calibration, 0, 0.9))
	bias := odoGyroBiasMax
	if robot.Arch.Class == "kitbot" {
		bias *= 3
	}
	odo.gyroBias = (hashUnit(robot.Team, 0x6e0)*2 - 1) * bias
	odo.visionAt, odo.TagsSeen = 0, 0
}

// stepOdometry integrates the wheels and gyro for one physics step. It runs before collisions resolve, so being
// pushed (or pushing a wall) is invisible to it, exactly like real encoders.
func (world *World) stepOdometry(robot *Robot, dt float64, onBump bool, accelFrac float64) {
	if robot.Climbing || robot.ClimbLevel > 0 || robot.Faults.Tipped {
		return
	}
	odo := &robot.Odo
	velocity := robot.Vel
	speed := velocity.Len()
	if speed > 1e-3 || math.Abs(robot.Omega) > 1e-3 {
		slip := 0.012*speed + 0.08*accelFrac*speed
		if onBump {
			slip += 0.25 * speed
		}
		if robot.Arch.Tank {
			slip += 0.04 * math.Abs(robot.Omega) * speed // skid-steer wheels scrub sideways in turns
		}
		measured := velocity.Scale(1 + odo.scale).Add(Vec2{world.rng.NormFloat64(), world.rng.NormFloat64()}.Scale(slip))
		// The robot integrates in the frame its gyro believes it is in.
		odo.Est = odo.Est.Add(measured.Rotate(robot.odoHeadingError()).Scale(dt))
	}
	odo.EstHeading = wrapAngle(odo.EstHeading + (robot.Omega+odo.gyroBias)*dt)
	world.visionUpdate(robot)
}

// gyroJolt is a hard hit shaking the gyro.
func (world *World) gyroJolt(robot *Robot, severity float64) {
	robot.Odo.EstHeading = wrapAngle(robot.Odo.EstHeading + world.rng.NormFloat64()*0.006*severity)
}

// visionUpdate fuses AprilTag sightings into the estimate (front and rear cameras, tags facing the robot, nothing in
// the way, robot not moving so fast that the image smears).
func (world *World) visionUpdate(robot *Robot) {
	odo := &robot.Odo
	if !odo.Vision || world.time < odo.visionAt {
		return
	}
	odo.visionAt = world.time + visionEvery
	odo.TagsSeen = 0
	if robot.Vel.Len() > visionMaxSpeed || math.Abs(robot.Omega) > visionMaxOmega || robot.Pitch*robot.Pitch > 0.01 {
		return
	}
	forward := robot.forward()
	for _, tag := range world.layout.AprilTags {
		at := Vec2{tag.X, tag.Y}
		offset := at.Sub(robot.Pos)
		distance := offset.Len()
		if distance > visionRange || distance < 0.4 {
			continue
		}
		direction := offset.Scale(1 / distance)
		yaw := tag.Yaw()
		if (Vec2{math.Cos(yaw), math.Sin(yaw)}).Dot(direction.Scale(-1)) < 0.3 {
			continue // seen from behind or too edge-on
		}
		if math.Abs(direction.Dot(forward)) < math.Cos(visionHalfFOV) {
			continue // outside both cameras
		}
		if world.viewBlocked(robot.Pos, at) {
			continue
		}
		odo.TagsSeen++
		sigma := 0.015 + 0.012*distance*distance
		measured := robot.Pos.Add(Vec2{world.rng.NormFloat64(), world.rng.NormFloat64()}.Scale(sigma))
		weight := 0.35 / (1 + distance)
		odo.Est = odo.Est.Add(measured.Sub(odo.Est).Scale(weight))
		headingSeen := robot.Heading + world.rng.NormFloat64()*(0.004+0.004*distance)
		odo.EstHeading = wrapAngle(odo.EstHeading + wrapAngle(headingSeen-odo.EstHeading)*weight*0.5)
		odo.LastFix = world.time
	}
}

// poseAim reports whether this shot is aimed from the robot's pose estimate rather than by the drivers' eyes: every
// auto shot, and turrets with vision (they aim off their pose all match).
func (robot *Robot) poseAim() bool {
	return robot.Auto || (robot.Arch.Turret && robot.Odo.Vision)
}
