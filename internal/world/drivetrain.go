package world

import "math"

// Swerve drivetrain dynamics. Each of the four modules is a brushless motor (Kraken X60 class) through a fixed gear
// ratio to a 4 in tread wheel. The robot's code runs a chassis velocity loop; each module then gives the force it
// can: limited by the motor (torque falls with speed as back-EMF rises, the stator current limit caps it at low
// speed, and a sagging battery lowers both) and by its tread's grip, which depends on how much of the robot's weight
// is on that wheel at the moment (weight shifts toward the wheels the robot accelerates away from). Acceleration, top
// speed, pushing matches, wheel slip and current draw all come out of this rather than being set directly.
//
// The design numbers an Archetype carries (MaxSpeed, MaxAccel) are what its gearing and current limits are chosen
// to achieve on a full battery, so the strategic planner's estimates stay right.

const (
	motorFreeSpeed    = 6000 * 2 * math.Pi / 60 // rad/s at 12 V (Kraken X60)
	motorStallTorque  = 7.09                    // N·m at 12 V
	motorStallCurrent = 366.0                   // A
	motorFreeCurrent  = 2.0                     // A
	motorKt           = motorStallTorque / motorStallCurrent
	wheelRadius       = 0.0508 // 4 in wheel
	driveEfficiency   = 0.9
	carpetMu          = 1.1  // static grip of a typical tread on field carpet
	carpetMuKinetic   = 0.85 // sliding (a pushed or skidding wheel)
	rollingResistance = 0.015
	velocityLoopTau   = 0.06 // s: how hard the robot code's velocity loop asks for the remaining error
	moduleInset       = 0.055
)

// Drivetrain is the physical drivetrain behind an Archetype.
type Drivetrain struct {
	Mass         float64 // kg with bumpers and battery
	Inertia      float64 // kg·m² about the vertical axis
	Gear         float64 // motor turns per wheel turn
	CurrentLimit float64 // A stator, per drive motor
	Mu, MuK      float64 // tread grip (static, sliding)
	Half         float64 // module distance from the center along each axis (m)
	CGHeight     float64 // m above the carpet
	Clearance    float64 // belly pan height above the carpet (m)
	BumperHeight float64 // height of the bumper's middle (where hits land)
	TopSpeed     float64 // m/s free speed at 12 V (with losses)
}

// drivetrainFor derives the physical drivetrain from a design.
func drivetrainFor(arch Archetype) Drivetrain {
	mass := arch.Mass
	if mass <= 0 {
		mass = 60
	}
	traction := arch.Traction
	if traction <= 0 {
		traction = 1
	}
	frame := arch.Frame
	if frame <= 0 {
		frame = 0.7
	}
	outer := frame + 2*bumperThickness
	dt := Drivetrain{Mass: mass, Mu: carpetMu * traction, MuK: carpetMuKinetic * traction, Half: frame/2 - moduleInset}
	// A square plate with the heavy parts (battery, modules) spread toward the corners.
	dt.Inertia = mass * outer * outer / 6 * 0.9
	// Gear for the design's top speed: the robot code's max speed is about 90 % of free speed.
	topSpeed := arch.MaxSpeed
	if topSpeed <= 0 {
		topSpeed = 4
	}
	dt.TopSpeed = topSpeed / 0.9
	dt.Gear = motorFreeSpeed * wheelRadius / dt.TopSpeed
	// Current limit for the design's launch acceleration (the motor is nowhere near stall at that current).
	accel := arch.MaxAccel
	if accel <= 0 {
		accel = 6
	}
	force := mass * accel / 4 // per module
	torque := force * wheelRadius / (dt.Gear * driveEfficiency)
	dt.CurrentLimit = clamp(torque/motorKt, 30, 120)
	// The heavy parts sit low: drivetrain, battery and bumpers; tall mechanisms raise it.
	height := arch.Height
	if height <= 0 {
		height = 0.5
	}
	dt.CGHeight = 0.11 + 0.26*height
	dt.BumperHeight = 0.11
	return dt
}

// drivetrain returns the robot's physical drivetrain, rebuilt whenever its design changes.
func (world *World) drivetrain(robot *Robot) *Drivetrain {
	key := robot.Arch.Name + "|" + robot.Arch.Label
	if robot.driveKey != key || robot.drive.Mass != robot.Arch.Mass {
		robot.drive = drivetrainFor(robot.Arch)
		// Teams raise the belly pan enough to cross a bump (square-on is the worst case: a skewed robot rides on the
		// wheels nearest the crest). A kit chassis sits low and cannot.
		robot.drive.Clearance = world.bumpClearanceNeeded(robot.drive.Half) + 0.01
		if robot.Arch.Class == "kitbot" || robot.Arch.Name == "kitbot" {
			robot.drive.Clearance = 0.03
		}
		robot.driveKey = key
	}
	return &robot.drive
}

// motorForce is the most force one module's motor can put on the carpet along a direction in which the wheel is
// moving at speed u (m/s, positive = with the force), at battery voltage volts.
func (dt *Drivetrain) motorForce(u, volts float64) float64 {
	motorSpeed := u / wheelRadius * dt.Gear // rad/s, signed
	vFrac := volts / 12
	// DC motor: torque = stall·(V/12 − ω/ωfree), capped by the stator current limit.
	torque := motorStallTorque * (vFrac - motorSpeed/motorFreeSpeed)
	limit := dt.CurrentLimit * motorKt
	torque = clamp(torque, -limit, limit)
	return torque * dt.Gear / wheelRadius * driveEfficiency
}

// moduleOffsets are the four modules in the robot frame (x forward, y left).
func (dt *Drivetrain) moduleOffsets() [4]Vec2 {
	h := dt.Half
	return [4]Vec2{{h, h}, {h, -h}, {-h, h}, {-h, -h}}
}

// stepDrive advances the robot's planar motion one step. target and omegaTarget are what the robot code asks for
// (field frame); slope is the bump's height gradient along x under the robot; extra is any outside force (N).
// It returns the drive motors' total current and how much the wheels slipped (0-1).
func (world *World) stepDrive(robot *Robot, target Vec2, omegaTarget float64, enabled bool, dt float64) (current, slip float64) {
	drive := world.drivetrain(robot)
	m, inertia := drive.Mass, drive.Inertia
	volts := 12.5
	if robot.Voltage > 1 {
		volts = robot.Voltage
	}
	heading := robot.Heading
	offsets := drive.moduleOffsets()

	// Wheel loads: the robot's weight, shifted by the last step's acceleration and the slope it is standing on, and
	// taken off the wheels when the belly rests on a bump or the robot is tipping up on two wheels.
	_, slope := world.groundHeight(robot.Pos)
	cosSlope := 1 / math.Sqrt(1+slope*slope)
	weight := m * gravity * cosSlope * (1 - robot.bellyLoad)
	accelLocal := robot.accel.Rotate(-heading)
	gravityLocal := Vec2{-slope * gravity * cosSlope, 0}.Rotate(-heading) // the slope pulls the robot downhill
	var loads [4]float64
	d2 := drive.Half * drive.Half
	for i, r := range offsets {
		// Σ N·r = −m·h·a: weight moves toward the wheels opposite the acceleration.
		shift := -m * drive.CGHeight * accelLocal.Sub(gravityLocal).Dot(r) / (4 * d2)
		loads[i] = math.Max(0, weight/4+shift)
	}
	if lift := robot.tilt; lift > 0 {
		// Tipping up: the wheels on the rising side carry nothing.
		for i, r := range offsets {
			if r.Dot(robot.tiltAxisLocal) > 0 {
				loads[i] = 0
			}
		}
	}

	// What the robot code asks of each module: the force that closes the velocity error over the loop's time
	// constant, split so the modules also produce the yaw torque.
	var wantForce Vec2
	var wantTorque float64
	if enabled {
		wantForce = target.Sub(robot.Vel).Scale(m / velocityLoopTau).Rotate(-heading)
		wantTorque = (omegaTarget - robot.Omega) * inertia / velocityLoopTau
	}
	var force Vec2
	var torque float64
	var demand, delivered float64
	for i, r := range offsets {
		// The module's wheel velocity over the carpet (robot frame).
		wheelVel := robot.Vel.Rotate(-heading).Add(Vec2{-r.Y, r.X}.Scale(robot.Omega))
		var f Vec2
		grip := drive.Mu * loads[i]
		if enabled {
			f = wantForce.Scale(0.25).Add(Vec2{-r.Y, r.X}.Scale(wantTorque / (4 * 2 * d2)))
			if mag := f.Len(); mag > 1e-9 {
				dir := f.Scale(1 / mag)
				// The motor's limit at the wheel's speed along the force.
				available := drive.motorForce(wheelVel.Dot(dir), volts)
				if mag > available {
					mag = math.Max(0, available)
				}
				demand += mag // (what the motor can give; the tread may not hold it)
				if mag > grip {
					mag = grip // the tread lets go
				}
				delivered += mag
				f = dir.Scale(mag)
			}
		} else {
			// Disabled: the motors are in brake mode and the modules hold their angle, so the wheels resist rolling
			// through the motor's back-EMF and skid sideways on the tread.
			roll := robot.moduleAngle[i]
			along := Vec2{math.Cos(roll), math.Sin(roll)}
			u := wheelVel.Dot(along)
			brake := math.Min(math.Abs(drive.motorForce(-u, 0)), grip)
			f = along.Scale(-math.Copysign(brake, u))
			side := Vec2{-along.Y, along.X}
			if s := wheelVel.Dot(side); math.Abs(s) > 1e-4 {
				f = f.Add(side.Scale(-math.Copysign(math.Min(drive.MuK*loads[i], math.Abs(s)*m/4/dt), s)))
			}
		}
		// Steering follows the force the module makes (the robot code points each wheel along its command).
		if f.Len() > 1 {
			robot.moduleAngle[i] = math.Atan2(f.Y, f.X)
		}
		force = force.Add(f)
		torque += r.Cross(f)
		// Current: the torque the motor is making, plus what it draws spinning freely.
		wheelTorque := f.Len() * wheelRadius / (drive.Gear * driveEfficiency)
		current += wheelTorque/motorKt + motorFreeCurrent*math.Min(1, wheelVel.Len()/drive.TopSpeed)
	}
	if demand > 1e-6 {
		slip = clamp(1-delivered/demand, 0, 1)
	}

	// Rolling resistance and the slope.
	net := force.Rotate(heading)
	if speed := robot.Vel.Len(); speed > 1e-3 {
		net = net.Sub(robot.Vel.Scale(rollingResistance * m * gravity / speed))
	}
	net = net.Add(Vec2{-slope * gravity * cosSlope * m, 0})
	accel := net.Scale(1 / m)
	alpha := torque / inertia
	robot.Vel = robot.Vel.Add(accel.Scale(dt))
	robot.Omega += alpha * dt
	if !enabled && robot.Vel.Len() < 0.02 && math.Abs(robot.Omega) < 0.05 && slope == 0 {
		robot.Vel, robot.Omega = Vec2{}, 0 // static friction holds a stopped robot
	}
	robot.accel = accel
	return current, slip
}

// crossesBumps reports whether the robot's belly clears a bump crest (if not, it goes under the trench instead).
func (world *World) crossesBumps(robot *Robot) bool {
	drive := world.drivetrain(robot)
	return drive.Clearance >= world.bumpClearanceNeeded(drive.Half)
}

// The shooter flywheel: each FUEL it launches takes a bite of its speed, and its velocity loop spins it back up.
const (
	flywheelDip      = 0.025 // speed lost per FUEL (fraction)
	flywheelReady    = 0.04  // the feeder holds the next FUEL until the wheel is this close to its setpoint
	flywheelRecovery = 0.12  // s, time constant of the spin-up on a full battery
)
