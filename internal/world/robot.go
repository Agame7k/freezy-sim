package world

import (
	"math"
)

const (
	Red  = 0
	Blue = 1

	bumperThickness = 0.085
)

// Archetype describes a robot design. There is no rule limiting how much FUEL a robot can hold, so capacities are big.
// Every design obeys R104/R107: no more than 30 in (0.762 m) tall at any time, so nothing can dump into the 72 in
// opening; FUEL goes in by shooting. Designs at or under 22.25 in (0.565 m) fit under the trench arm.
type Archetype struct {
	Id          int     `json:"id"`
	Name        string  `json:"name"`
	Label       string  `json:"label"`
	Frame       float64 `json:"frame"`
	Height      float64 `json:"height"`
	MaxSpeed    float64 `json:"maxSpeed"`
	MaxAccel    float64 `json:"maxAccel"`
	MaxOmega    float64 `json:"maxOmega"`
	Capacity    int     `json:"capacity"`
	ShootRate   float64 `json:"shootRate"` // FUEL per second into the hub
	PassRate    float64 `json:"passRate"`  // FUEL per second when lobbing to a teammate zone
	ShotSpeedSD float64 `json:"shotSpeedSD"`
	ShotAimSD   float64 `json:"shotAimSD"` // radians
	MaxRange    float64 `json:"maxRange"`
	// Volley is how many FUEL a multi-lane shooter fires side by side at once; LaneSpread is the distance between its
	// outer lanes. A plain shooter has Volley 1. Lanes sit at least a ball width (0.165 m) apart or the FUEL
	// collides in the air.
	Volley     int     `json:"volley"`
	LaneSpread float64 `json:"laneSpread"`
	Turret     bool    `json:"turret"` // aims independently of the drivetrain and leads shots while moving
	ClimbLevel int     `json:"climbLevel"`
	ClimbTime  float64 `json:"climbTime"` // seconds per level
	// IntakeWidth is the width of the ground intake; Mass (kg, with bumpers and battery) and Traction (wheel grip,
	// 1 = typical swerve on carpet) decide pushing matches. Tank drives grip harder sideways than swerve.
	IntakeWidth float64 `json:"intakeWidth"`
	Mass        float64 `json:"mass"`
	Traction    float64 `json:"traction"`
	Tank        bool    `json:"tank"`
	Weight      float64 `json:"weight"` // how common the design is at an event
	// Class is the stock design this one is closest to (game plans are learned per mix of classes); Spec is its parts
	// (design.go) and Model what the 3D view draws.
	Class string    `json:"class"`
	Spec  Build     `json:"spec"`
	Model ModelSpec `json:"model"`
}

// stockModels mirror ARCHETYPES in web/js/scene/robots.js; stockBuilds are the same robots as kit parts (a team
// whose design was picked by hand starts its pit changes from them).
var (
	stockModels = map[string]ModelSpec{
		"scorer": {Height: 0.54, Frame: 0.68, Drive: "swerve", Intake: 0.92, Shooter: "fixed", Lanes: 2, Hopper: 0.78,
			Capacity: 55, Climber: true},
		"tall": {Height: 0.75, Frame: 0.72, Drive: "swerve", Intake: 0.95, Shooter: "fixed", Lanes: 3, Hopper: 0.8,
			Capacity: 70, Climber: true},
		"volley": {Height: 0.55, Frame: 0.78, Drive: "swerve", Intake: 1.0, Shooter: "wide", Lanes: 4, Hopper: 0.82,
			Capacity: 90, Climber: true},
		"defender": {Height: 0.36, Frame: 0.74, Drive: "swerve", Intake: 0.7, Shooter: "small", Lanes: 1, Hopper: 0.5,
			HopperLength: 0.55, Capacity: 12},
		"turret": {Height: 0.55, Frame: 0.7, Drive: "swerve", Intake: 0.95, Shooter: "turret", Lanes: 2, Hopper: 0.78,
			Capacity: 60, Climber: true},
		"bigdumper": {Height: 0.74, Frame: 0.84, Drive: "swerve", Intake: 1.0, Shooter: "wide", Lanes: 4, Hopper: 0.86,
			Bin: true, Capacity: 130, Climber: true},
		"shuttle": {Height: 0.45, Frame: 0.74, Drive: "swerve", Intake: 1.12, Shooter: "fixed", Lanes: 3, Hopper: 0.8,
			Capacity: 80},
		"kitbot": {Height: 0.5, Frame: 0.7, Drive: "swerve", Intake: 0.86, Shooter: "kitbot", Lanes: 1, Hopper: 0.5,
			Capacity: 22, Climber: true},
	}
	stockBuilds = map[string]Build{
		"scorer":    {Drive: "swerve", Frame: 0.68, Height: 0.54, Intake: 0.95, Shooter: "fixed", Lanes: 2, Hopper: "medium", Climber: 2},
		"tall":      {Drive: "swerve", Frame: 0.74, Height: 0.75, Intake: 0.95, Shooter: "fixed", Lanes: 3, Hopper: "medium", Climber: 3},
		"volley":    {Drive: "swerve", Frame: 0.8, Height: 0.54, Intake: 0.95, Shooter: "wide", Lanes: 4, Hopper: "large", Climber: 1},
		"defender":  {Drive: "swerve", Frame: 0.74, Height: 0.36, Intake: 0.8, Shooter: "small", Lanes: 1, Hopper: "small", Climber: 1, Ballast: 10},
		"turret":    {Drive: "swerve", Frame: 0.68, Height: 0.54, Intake: 0.95, Shooter: "turret", Lanes: 2, Hopper: "medium", Climber: 2},
		"bigdumper": {Drive: "swerve", Frame: 0.84, Height: 0.75, Intake: 0.95, Shooter: "wide", Lanes: 4, Hopper: "huge", Climber: 1},
		"shuttle":   {Drive: "swerve", Frame: 0.74, Height: 0.45, Intake: 1.12, Shooter: "fixed", Lanes: 3, Hopper: "large", Climber: 1},
		"kitbot":    {Drive: "swerve", Frame: 0.68, Height: 0.54, Intake: 0.8, Shooter: "kitbot", Lanes: 1, Hopper: "small", Climber: 1},
	}
)

func init() {
	for i := range Archetypes {
		arch := &Archetypes[i]
		arch.Class, arch.Spec, arch.Model = arch.Name, stockBuilds[arch.Name], stockModels[arch.Name]
		arch.Model.Label = arch.Label
		arch.Capacity = arch.hopperShape().capacity() // what the hopper really holds
		arch.Model.Capacity = arch.Capacity
	}
}

// MaxRobotHeight is the R107 vertical limit (30 in).
const MaxRobotHeight = 0.762

var Archetypes = []Archetype{
	{Id: 0, Name: "scorer", Label: "Low shooter (trench)", Frame: 0.68, Height: 0.54, MaxSpeed: 4.4, MaxAccel: 7, MaxOmega: 7,
		Capacity: 55, ShootRate: 7, PassRate: 8, ShotSpeedSD: 0.022, ShotAimSD: 0.011, MaxRange: 5.5, Volley: 1,
		ClimbLevel: 2, ClimbTime: 2.6, IntakeWidth: 0.62, Mass: 62, Traction: 1, Weight: 24},
	{Id: 1, Name: "tall", Label: "Tall shooter (30 in, bump runner)", Frame: 0.72, Height: 0.75, MaxSpeed: 4.0, MaxAccel: 6,
		MaxOmega: 6, Capacity: 70, ShootRate: 8, PassRate: 8, ShotSpeedSD: 0.018, ShotAimSD: 0.009, MaxRange: 6.5,
		Volley: 1, ClimbLevel: 3, ClimbTime: 2.4, IntakeWidth: 0.66, Mass: 66, Traction: 1, Weight: 14},
	{Id: 2, Name: "volley", Label: "Wide volley shooter (4 lanes, trench)", Frame: 0.78, Height: 0.55, MaxSpeed: 4.2,
		MaxAccel: 6.5, MaxOmega: 6, Capacity: 90, ShootRate: 12, PassRate: 10, ShotSpeedSD: 0.022, ShotAimSD: 0.012,
		MaxRange: 4.6, Volley: 4, LaneSpread: 0.54, ClimbLevel: 1, ClimbTime: 3, IntakeWidth: 0.76, Mass: 64, Traction: 1,
		Weight: 11},
	{Id: 3, Name: "defender", Label: "Defender (heavy tank drive)", Frame: 0.74, Height: 0.36, MaxSpeed: 5.0, MaxAccel: 9,
		MaxOmega: 8, Capacity: 12, ShootRate: 3, PassRate: 4, ShotSpeedSD: 0.05, ShotAimSD: 0.03, MaxRange: 3.5, Volley: 1,
		ClimbLevel: 1, ClimbTime: 3, IntakeWidth: 0.5, Mass: 70, Traction: 1.2, Weight: 5},
	{Id: 4, Name: "turret", Label: "Turret shooter (shoots on the move)", Frame: 0.7, Height: 0.55, MaxSpeed: 4.5,
		MaxAccel: 7, MaxOmega: 7, Capacity: 60, ShootRate: 9, PassRate: 10, ShotSpeedSD: 0.018, ShotAimSD: 0.01,
		MaxRange: 6, Volley: 1, Turret: true, ClimbLevel: 2, ClimbTime: 2.5, IntakeWidth: 0.64, Mass: 63, Traction: 1,
		Weight: 15},
	{Id: 5, Name: "bigdumper", Label: "Big dumper (4-wide shooter, huge bin)", Frame: 0.84, Height: 0.74, MaxSpeed: 3.6,
		MaxAccel: 5.5, MaxOmega: 5, Capacity: 130, ShootRate: 18, PassRate: 9, ShotSpeedSD: 0.026, ShotAimSD: 0.014,
		MaxRange: 3.6, Volley: 4, LaneSpread: 0.54, ClimbLevel: 1, ClimbTime: 3.2, IntakeWidth: 0.82, Mass: 68,
		Traction: 1.1, Weight: 8},
	{Id: 6, Name: "shuttle", Label: "Shuttle bot (wide intake, fast passer)", Frame: 0.74, Height: 0.45, MaxSpeed: 4.8,
		MaxAccel: 8, MaxOmega: 7, Capacity: 80, ShootRate: 5, PassRate: 14, ShotSpeedSD: 0.035, ShotAimSD: 0.022,
		MaxRange: 4, Volley: 1, ClimbLevel: 1, ClimbTime: 3, IntakeWidth: 0.86, Mass: 58, Traction: 0.95, Weight: 11},
	{Id: 7, Name: "kitbot", Label: "Kitbot", Frame: 0.7, Height: 0.5, MaxSpeed: 3.2, MaxAccel: 5, MaxOmega: 5,
		Capacity: 22, ShootRate: 3, PassRate: 3, ShotSpeedSD: 0.05, ShotAimSD: 0.03, MaxRange: 3.5, Volley: 1,
		ClimbLevel: 1, ClimbTime: 3.5, IntakeWidth: 0.5, Mass: 52, Traction: 1, Weight: 12},
}

// archetypeForTeam gives every team a consistent robot design (deterministic from the team number).
func archetypeForTeam(team int) Archetype {
	total := 0.0
	for _, archetype := range Archetypes {
		total += archetype.Weight
	}
	hash := uint32(team)*2654435761 ^ 0x9e3779b9
	pick := float64(hash%10000) / 10000 * total
	for _, archetype := range Archetypes {
		if pick -= archetype.Weight; pick <= 0 {
			return archetype
		}
	}
	return Archetypes[0]
}

// skillForTeam gives every team a consistent driver skill between 0.7 and 1.3.
func skillForTeam(team int) float64 {
	hash := uint32(team)*40503 ^ 0x51ed27
	return 0.7 + float64(hash%600)/1000
}

func archetypeByName(name string) Archetype {
	if build, ok := parseBuild(name); ok {
		return build.Archetype()
	}
	for _, archetype := range Archetypes {
		if archetype.Name == name || (name == "dumper" && archetype.Name == "volley") {
			return archetype // "dumper" (the old elevator dumper, not legal under R107) became the volley shooter
		}
	}
	return Archetypes[0]
}

// Control is what a driver (AI or human) asks the robot to do this step.
type Control struct {
	Vel        Vec2    // field-relative velocity request (m/s)
	Omega      float64 // rad/s
	Intake     bool
	Shoot      bool
	Climb      int // requested tower level (0 = none)
	OpenChute  bool
	Pass       bool // lob FUEL to PassTarget instead of shooting at the hub
	PassTarget Vec2
	Timing     int  // shot timing tag (early/late around a shift change) for the statistics
	Eject      bool // push FUEL out of the intake (into the outpost corral)
}

// Faults are mechanical/driver problems the chaos injector (or the user) can give a robot.
type Faults struct {
	IntakeJamUntil  float64 `json:"intakeJamUntil"`
	ShooterJamUntil float64 `json:"shooterJamUntil"`
	SpeedScale      float64 `json:"speedScale"` // 0 = normal (1.0)
	StuckUntil      float64 `json:"stuckUntil"`
	HighCenterNext  bool    `json:"highCenterNext"`
	Tipped          bool    `json:"tipped"`
	ClimbFail       bool    `json:"climbFail"`
}

// Robot is one simulated robot on the field.
type Robot struct {
	Index    int
	Station  string
	Alliance int
	Team     int
	Present  bool
	Arch     Archetype

	Pos       Vec2
	Heading   float64
	Vel       Vec2
	Omega     float64
	Z         float64
	hopperVel Vec2 // velocity at the last hopper step (the FUEL inside feels the robot's acceleration)
	Pitch     float64
	Roll      float64

	// Physics state (drivetrain.go, body.go): the drivetrain behind the design, the last step's acceleration, each
	// swerve module's angle, wheel slip, the chassis on its tires, the belly on a bump, and tipping about an edge.
	drive          Drivetrain
	driveKey       string
	accel          Vec2
	moduleAngle    [4]float64
	Slip           float64
	zRate          float64
	pitchRate      float64
	rollRate       float64
	bellyLoad      float64
	highCentered   bool
	tilt           float64
	tiltRate       float64
	tiltAxisLocal  Vec2
	contactImpulse Vec2    // contact impulses since the last body step (field frame, N·s)
	flywheelSag    float64 // how far the shooter flywheel is below its setpoint (fraction), recovering

	Ctl         Control
	Manual      bool
	GameData    string
	Remaining   int
	FmsLinked   bool
	ClimbSlot   int
	Enabled     bool
	Auto        bool
	Brownout    bool
	Voltage     float64 // battery voltage the DS reports (0 = not known: the AI benchmark)
	Hopper      int
	IntakeOn    bool
	Shooting    bool
	cooldown    float64
	ClimbLevel  int
	ClimbProg   float64
	Climbing    bool
	Load        float64
	Shots       int
	Scored      int
	AutoScored  int // FUEL launched in AUTO that went in
	Passes      int
	Collected   int
	AiState     string
	Skill       float64
	Faults      Faults
	Odo         Odometry // what the robot's code believes about its own pose (odometry.go)
	AutoName    string   // the auto routine selected on the driver station
	Activity    Activity
	MatchType   int     // FMS match type (3 = playoffs)
	AutoTower   int     // tower points earned in auto
	Caught      int     // FUEL caught from a hub exit (G408)
	bumpedUntil float64 // knocked off its aim by a hit until this world time
	// Between matches: on a cart being wheeled on or off (fieldcrew.go), the path, the heading to arrive at, and
	// the team whose robot replaces this one once it is off.
	Cart        int
	cartT       float64
	cartPath    []Vec2
	cartHeading float64
	pendingTeam int
	preloadOwed bool // rolled in before the staff had all its preload FUEL to hand
}

func (robot *Robot) faultActive(now float64) []string {
	var active []string
	f := robot.Faults
	if now < f.IntakeJamUntil {
		active = append(active, "intake jam")
	}
	if now < f.ShooterJamUntil {
		active = append(active, "shooter jam")
	}
	if f.SpeedScale > 0 && f.SpeedScale < 1 {
		active = append(active, "drive module failure")
	}
	if now < f.StuckUntil {
		active = append(active, "high-centered")
	}
	if f.HighCenterNext {
		active = append(active, "will high-center")
	}
	if f.Tipped {
		active = append(active, "TIPPED OVER")
	}
	if f.ClimbFail {
		active = append(active, "climber failure")
	}
	return active
}

func (robot *Robot) half() Vec2 {
	h := robot.Arch.Frame/2 + bumperThickness
	return Vec2{h, h}
}

func (robot *Robot) obb() OBB {
	return OBB{Center: robot.Pos, Half: robot.half(), Angle: robot.Heading}
}

func (robot *Robot) forward() Vec2 {
	return Vec2{math.Cos(robot.Heading), math.Sin(robot.Heading)}
}

// shooterExit is where launched FUEL leaves the robot: the rear-mounted hood, or the turret at the center. Nothing
// may stick up past the 30 in limit (R107), so FUEL leaves just under the robot's top.
func (robot *Robot) shooterExit() (Vec2, float64) {
	z := robot.Z + math.Min(robot.Arch.Height, MaxRobotHeight) - 0.03
	if robot.Arch.Turret {
		return robot.Pos, z
	}
	back := robot.forward().Scale(-robot.Arch.Frame * 0.28)
	return robot.Pos.Add(back), z
}

// stepRobot integrates one robot's motion and mechanisms.
func (world *World) stepRobot(robot *Robot, dt float64) {
	arch := robot.Arch
	ctl := robot.Ctl
	if !robot.Enabled || robot.Brownout {
		ctl = Control{}
	}
	if robot.Climbing || robot.ClimbLevel > 0 {
		// Hanging on the tower: no driving.
		ctl.Vel = Vec2{}
		ctl.Omega = 0
	}

	faults := &robot.Faults
	if faults.Tipped || world.time < faults.StuckUntil {
		ctl.Vel = Vec2{}
		ctl.Omega = 0
	}
	if faults.Tipped {
		ctl = Control{}
	}
	if world.time < faults.IntakeJamUntil {
		ctl.Intake = false
	}
	if world.time < faults.ShooterJamUntil {
		ctl.Shoot = false
		ctl.Pass = false
	}
	// The robot code's speed limit: the design's max speed (a fault may lower it); drivers ease off over a bump.
	// The motors, gearing, battery and grip decide what the robot really does (drivetrain.go).
	maxSpeed := arch.MaxSpeed
	if faults.SpeedScale > 0 {
		maxSpeed *= faults.SpeedScale
	}
	bumpZ, _ := world.groundHeight(robot.Pos)
	if bumpZ > 0.01 {
		maxSpeed *= 0.7
		if faults.HighCenterNext && bumpZ > world.layout.Bump.Height*0.8 {
			// A crooked approach: the robot stops square on the crest with its belly on it.
			faults.HighCenterNext = false
			faults.StuckUntil = world.time + 4 + world.rng.Float64()*4
			robot.Vel = Vec2{}
			world.logf("warn", "world", "%d (%s) is high-centered on a bump", robot.Team, robot.Station)
		}
	}
	target := ctl.Vel
	if speed := target.Len(); speed > maxSpeed {
		target = target.Scale(maxSpeed / speed)
	}
	omegaTarget := clamp(ctl.Omega, -arch.MaxOmega, arch.MaxOmega)
	enabled := robot.Enabled && !robot.Brownout && !faults.Tipped && robot.ClimbLevel == 0 && !robot.Climbing
	var driveCurrent float64
	if robot.ClimbLevel == 0 && !robot.Climbing && !faults.Tipped {
		driveCurrent, robot.Slip = world.stepDrive(robot, target, omegaTarget, enabled, dt)
	} else {
		robot.Vel, robot.Omega, robot.accel, robot.Slip = Vec2{}, 0, Vec2{}, 0
	}
	world.stepOdometry(robot, dt, bumpZ > 0.01, robot.Slip)
	robot.Pos = robot.Pos.Add(robot.Vel.Scale(dt))
	robot.Heading = wrapAngle(robot.Heading + robot.Omega*dt)

	// Ride over bumps, high-center, tip (body.go).
	world.stepBody(robot, dt)

	world.collideRobotStatics(robot)

	// Mechanisms.
	if robot.flywheelSag > 0 {
		// The flywheel spins back up: slower on a sagging battery.
		volts := robot.Voltage
		if volts <= 1 {
			volts = 12.5 // no battery model (the AI benchmark)
		}
		tau := flywheelRecovery * 12.5 / math.Max(8, volts)
		robot.flywheelSag *= math.Exp(-dt / tau)
	}
	robot.IntakeOn = ctl.Intake && robot.Hopper < arch.Capacity
	robot.Shooting = false
	robot.cooldown -= dt
	if ctl.Shoot && robot.Hopper > 0 && robot.cooldown <= 0 {
		if world.launch(robot, ctl.Timing) {
			robot.cooldown = float64(max(1, arch.Volley)) / arch.ShootRate
			robot.Shooting = true
		}
	} else if ctl.Pass && robot.Hopper > 0 && robot.cooldown <= 0 {
		if world.lob(robot, ctl.PassTarget) {
			robot.cooldown = 1 / math.Max(1, arch.PassRate)
			robot.Shooting = true
		}
	} else if ctl.Eject && robot.Hopper > 0 && robot.cooldown <= 0 {
		if world.eject(robot) {
			robot.cooldown = 0.12
		}
	}
	if ctl.Climb > 0 {
		world.stepClimb(robot, ctl.Climb, dt)
	} else if robot.Climbing {
		robot.Climbing = false
	}

	// Electrical load reported to the battery model: the drive motors' real current plus the mechanisms.
	load := 4 + driveCurrent
	if robot.IntakeOn {
		load += 12
	}
	if ctl.Shoot {
		load += 18
	}
	if robot.Climbing {
		load += 70
	}
	robot.Load = load
}

// Contacts are rigid-body impulses: a hit shares momentum by mass (bumper foam makes it nearly inelastic), the
// bumpers' fabric drags sideways, and an off-center hit spins the robots. What happens next, who wins a pushing
// match, is the drivetrains' doing (drivetrain.go): each robot's wheels push up to their grip.
const (
	bumperRestitution = 0.08 // foam pool noodles under fabric are very dead
	bumperFriction    = 0.3  // Cordura on Cordura, sliding
	wallRestitution   = 0.04
	wallFriction      = 0.12 // Cordura on polycarbonate and HDPE
)

// collideRobotStatics keeps a robot inside the field and out of solid field elements.
func (world *World) collideRobotStatics(robot *Robot) {
	if robot.Climbing || robot.ClimbLevel > 0 {
		return // hanging on the tower
	}
	box := robot.obb()
	extentX := box.project(Vec2{1, 0})
	extentY := box.project(Vec2{0, 1})
	L := world.layout.Field.Length
	W := world.layout.Field.Width
	if robot.Pos.X < extentX {
		robot.Pos.X = extentX
		world.wallImpulse(robot, Vec2{1, 0})
	}
	if robot.Pos.X > L-extentX {
		robot.Pos.X = L - extentX
		world.wallImpulse(robot, Vec2{-1, 0})
	}
	if robot.Pos.Y < extentY {
		robot.Pos.Y = extentY
		world.wallImpulse(robot, Vec2{0, 1})
	}
	if robot.Pos.Y > W-extentY {
		robot.Pos.Y = W - extentY
		world.wallImpulse(robot, Vec2{0, -1})
	}
	for _, static := range world.statics {
		if !static.Robots {
			continue
		}
		if static.ClearHeight > 0 && robot.Arch.Height <= static.ClearHeight {
			continue // fits under the trench arm
		}
		push, hit := SAT(robot.obb(), aabbAsOBB(static.Box))
		if !hit {
			continue
		}
		robot.Pos = robot.Pos.Add(push)
		world.wallImpulse(robot, push.Norm())
	}
}

// wallImpulse stops a robot's motion into a fixed surface whose normal points back at the robot: a small bounce
// off the bumper foam and a sideways drag along the wall.
func (world *World) wallImpulse(robot *Robot, normal Vec2) {
	into := robot.Vel.Dot(normal)
	if into >= 0 {
		return
	}
	m := world.drivetrain(robot).Mass
	jn := -(1 + wallRestitution) * into * m
	tangent := Vec2{-normal.Y, normal.X}
	slide := robot.Vel.Dot(tangent)
	jt := clamp(-slide*m, -wallFriction*jn, wallFriction*jn)
	world.applyContact(robot, normal.Scale(jn).Add(tangent.Scale(jt)))
}

// applyContact changes a robot's velocity by a contact impulse (N·s, field frame) and remembers it for the body
// dynamics: an impulse at bumper height, below the center of gravity, rocks the robot (body.go).
func (world *World) applyContact(robot *Robot, impulse Vec2) {
	m := world.drivetrain(robot).Mass
	robot.Vel = robot.Vel.Add(impulse.Scale(1 / m))
	robot.contactImpulse = robot.contactImpulse.Add(impulse)
}

// collideRobots resolves robot-robot contacts.
func (world *World) collideRobots() {
	for i := 0; i < len(world.robots); i++ {
		a := world.robots[i]
		if !a.onField() {
			continue
		}
		for j := i + 1; j < len(world.robots); j++ {
			b := world.robots[j]
			if !b.onField() {
				continue
			}
			push, hit := SAT(a.obb(), b.obb())
			if !hit {
				continue
			}
			aFixed := a.ClimbLevel > 0 || a.Climbing
			bFixed := b.ClimbLevel > 0 || b.Climbing
			if aFixed && bFixed {
				continue
			}
			da, db := world.drivetrain(a), world.drivetrain(b)
			invA, invB := 1/da.Mass, 1/db.Mass
			invIA, invIB := 1/da.Inertia, 1/db.Inertia
			if aFixed {
				invA, invIA = 0, 0
			}
			if bFixed {
				invB, invIB = 0, 0
			}
			normal := push.Norm() // from b toward a
			// Separate them in proportion to their masses.
			a.Pos = a.Pos.Add(push.Scale(invA / (invA + invB)))
			b.Pos = b.Pos.Sub(push.Scale(invB / (invA + invB)))
			// The contact point: midway between each bumper's point nearest the other robot.
			contact := closestPointOBB(a.obb(), b.Pos).Add(closestPointOBB(b.obb(), a.Pos)).Scale(0.5)
			ra, rb := contact.Sub(a.Pos), contact.Sub(b.Pos)
			pointVel := func(r *Robot, arm Vec2) Vec2 { return r.Vel.Add(Vec2{-arm.Y, arm.X}.Scale(r.Omega)) }
			rel := pointVel(a, ra).Sub(pointVel(b, rb))
			closing := rel.Dot(normal)
			if closing >= 0 {
				continue
			}
			effective := func(dir Vec2) float64 {
				rna, rnb := ra.Cross(dir), rb.Cross(dir)
				return invA + invB + rna*rna*invIA + rnb*rnb*invIB
			}
			jn := -(1 + bumperRestitution) * closing / effective(normal)
			tangent := Vec2{-normal.Y, normal.X}
			jt := clamp(-rel.Dot(tangent)/effective(tangent), -bumperFriction*jn, bumperFriction*jn)
			impulse := normal.Scale(jn).Add(tangent.Scale(jt))
			if !aFixed {
				world.applyContact(a, impulse)
				a.Omega += ra.Cross(impulse) * invIA
			}
			if !bFixed {
				world.applyContact(b, impulse.Scale(-1))
				b.Omega -= rb.Cross(impulse) * invIB
			}
			// A hard hit knocks a shooter off its aim for a moment and jolts the gyros.
			if hit := -closing; hit > 0.8 {
				a.bumpedUntil = math.Max(a.bumpedUntil, world.time+0.25+0.1*hit)
				b.bumpedUntil = math.Max(b.bumpedUntil, world.time+0.25+0.1*hit)
				world.gyroJolt(a, hit)
				world.gyroJolt(b, hit)
			}
		}
	}
}

// launch fires one shot at the hub: a single FUEL, or a side-by-side volley from a multi-lane shooter. It returns
// false when there is no ballistic solution or the shooter is not lined up.
func (world *World) launch(robot *Robot, timing int) bool {
	arch := robot.Arch
	hub := world.hubs[robot.Alliance]
	exit, exitZ := robot.shooterExit()
	target := hub.aimPoint()
	if !arch.Turret && !robot.poseAim() {
		// A fixed rear hood only fires when the robot's back points at the hub (a robot aiming off its pose estimate
		// fires when it believes it is lined up, and misses by however wrong the estimate is).
		back := robot.forward().Scale(-1)
		if back.Dot(target.Sub(exit).Norm()) < math.Cos(0.35) {
			return false
		}
	}
	skill := robot.Skill
	if skill <= 0 {
		skill = 1
	}
	// One pointing error for the whole shooter, plus a little per lane (wheel wear, compression differences).
	speedErr := world.rng.NormFloat64() * arch.ShotSpeedSD / skill
	yawErr := world.rng.NormFloat64() * arch.ShotAimSD / skill
	angleErr := world.rng.NormFloat64() * arch.ShotAimSD / skill
	if robot.Voltage > 1 && robot.Voltage < 10.8 {
		// The flywheel's velocity loop runs out of headroom on a low battery: shots come off slow and fall short.
		speedErr -= (10.8 - robot.Voltage) * 0.03
	}
	if robot.Odo.Vision && robot.Odo.TagsSeen > 0 && !robot.Auto {
		// Vision auto-aim: with hub tags in view the shooter locks on, and the driver's aim error mostly goes away.
		yawErr *= 0.6
		speedErr *= 0.8
	}
	if world.time < robot.bumpedUntil {
		// Just took a hit: the whole shooter is knocked off line.
		yawErr += world.rng.NormFloat64() * 0.07
		angleErr += world.rng.NormFloat64() * 0.05
	}
	// The feeder waits for the flywheel to come back up to speed after the last shot; what sag is left when it
	// fires costs some exit speed (the ball takes about half the wheel's surface speed deficit).
	if robot.flywheelSag > flywheelReady {
		return false
	}
	speedErr -= robot.flywheelSag * 0.5
	lanes := min(max(1, arch.Volley), robot.Hopper)
	left := Vec2{-math.Sin(robot.Heading), math.Cos(robot.Heading)}
	if arch.Turret {
		toTarget := target.Sub(exit).Norm()
		left = Vec2{-toTarget.Y, toTarget.X}
	}
	fired := 0
	for lane := 0; lane < lanes; lane++ {
		offset := 0.0
		if lanes > 1 {
			offset = arch.LaneSpread * (float64(lane)/float64(lanes-1) - 0.5)
		}
		// The lanes fire parallel, each at its own spot across the opening (converging lanes would collide in flight).
		laneExit := exit.Add(left.Scale(offset))
		laneTarget := target.Add(left.Scale(offset))
		laneNoise := 0.4 / skill
		velocity, spin, ok := world.shotVelocity(robot, laneExit, exitZ, laneTarget,
			speedErr+world.rng.NormFloat64()*arch.ShotSpeedSD*laneNoise,
			yawErr+world.rng.NormFloat64()*arch.ShotAimSD*laneNoise,
			angleErr+world.rng.NormFloat64()*arch.ShotAimSD*laneNoise)
		if !ok {
			if fired == 0 {
				return false
			}
			continue
		}
		ball := world.takeHeldBall(robot)
		if ball == nil {
			break
		}
		ball.State = BallFree
		ball.P = [3]float64{laneExit.X, laneExit.Y, exitZ}
		ball.V = velocity
		ball.W = spin
		ball.sleep = 0
		ball.shooter = robot.Index + 1
		ball.timing = timing
		ball.autoShot = robot.Auto
		robot.Hopper--
		robot.Shots++
		world.stats.Shots[robot.Alliance]++
		fired++
		if agent, ok := world.controllers[robot.Index].(*Agent); ok && timing != timingNone {
			if timing == timingEarly {
				agent.shotStats.Early++
			} else {
				agent.shotStats.Late++
			}
		}
	}
	robot.flywheelSag += flywheelDip * float64(fired)
	if fired > 0 {
		world.noteLaunch(robot)
	}
	return fired > 0
}

// shotVelocity solves the launch for one FUEL from exit to target (with drag). FUEL keeps the robot's own velocity,
// so a fixed shooter that fires on the move misses by that drift; a turret leads its shot instead: it aims where the
// target will be relative to the moving robot when the FUEL arrives.
func (world *World) shotVelocity(robot *Robot, exit Vec2, exitZ float64, target Vec2,
	speedErr, yawErr, angleErr float64) ([3]float64, [3]float64, bool) {
	arch := robot.Arch
	dz := world.hubOpeningZ() + 0.12 - exitZ
	aim := target
	if arch.Turret {
		for i := 0; i < 2; i++ {
			flight := shotFlightTime(aim.Sub(exit).Len(), dz)
			aim = target.Sub(robot.Vel.Scale(flight))
		}
	}
	toAim := aim.Sub(exit)
	yawBias := 0.0
	if robot.poseAim() {
		// The shooter is set up for where the robot thinks it is: the distance and direction come from the pose
		// estimate, and the gyro error turns the whole shot.
		toAim = aim.Sub(exit.Add(robot.Odo.Est.Sub(robot.Pos)))
		yawBias = -robot.odoHeadingError()
	}
	distance := toAim.Len()
	if distance > arch.MaxRange {
		return [3]float64{}, [3]float64{}, false
	}
	spin := shotSpin(arch)
	speed, angle, ok := solveLaunchSpin(distance, dz, spin)
	if !ok {
		return [3]float64{}, [3]float64{}, false
	}
	speed *= 1 + speedErr
	yaw := math.Atan2(toAim.Y, toAim.X) + yawErr + yawBias
	angle += angleErr
	return [3]float64{
		speed*math.Cos(angle)*math.Cos(yaw) + robot.Vel.X,
		speed*math.Cos(angle)*math.Sin(yaw) + robot.Vel.Y,
		speed * math.Sin(angle),
	}, backspin(speed, yaw, spin, world.ballRadius), true
}

// lob passes one FUEL to a spot on the floor (shuttling into the alliance zone).
func (world *World) lob(robot *Robot, target Vec2) bool {
	exit, exitZ := robot.shooterExit()
	delta := target.Sub(exit)
	distance := delta.Len()
	if distance < 1 || distance > robot.Arch.MaxRange+2.5 {
		return false
	}
	angle := 0.9
	spin := shotSpin(robot.Arch)
	speed, _ := solveLaunchSpeedSpin(distance, world.ballRadius-exitZ, angle, spin)
	if speed <= 0 {
		return false
	}
	ball := world.takeHeldBall(robot)
	if ball == nil {
		return false
	}
	speed *= 1 + world.rng.NormFloat64()*0.05
	yaw := math.Atan2(delta.Y, delta.X) + world.rng.NormFloat64()*0.06
	ball.State = BallFree
	ball.P = [3]float64{exit.X, exit.Y, exitZ}
	ball.V = [3]float64{speed * math.Cos(angle) * math.Cos(yaw), speed * math.Cos(angle) * math.Sin(yaw),
		speed * math.Sin(angle)}
	ball.W = backspin(speed, yaw, spin, world.ballRadius)
	ball.sleep = 0
	ball.shooter = robot.Index + 1
	robot.Hopper--
	robot.Passes++
	return true
}

// SpillHopper dumps some of a robot's FUEL onto the floor around it.
func (world *World) SpillHopper(robot *Robot, count int) int {
	spilled := 0
	for _, ball := range world.balls {
		if spilled >= count || robot.Hopper == 0 {
			break
		}
		if ball.State != BallHeld || ball.Owner != robot.Index {
			continue
		}
		angle := world.rng.Float64() * 2 * math.Pi
		radius := robot.half().X + world.ballRadius + 0.05
		ball.State = BallFree
		ball.P = [3]float64{robot.Pos.X + math.Cos(angle)*radius, robot.Pos.Y + math.Sin(angle)*radius, 0.3}
		ball.V = [3]float64{math.Cos(angle) * 1.2, math.Sin(angle) * 1.2, 0.5}
		ball.sleep = 0
		robot.Hopper--
		spilled++
	}
	return spilled
}

// rimClearance is how far before the aim point (the hub center) a shot passes the near edge of the clear hopper; the
// FUEL must already be above the aim height there, or a close-range shot that is still rising clips the hopper wall.
const rimClearance = 0.62

// Shooter spin. A hooded flywheel shooter rolls the FUEL along its hood and sends it off with backspin: the spin
// ratio is the ball's surface speed from spin over its launch speed. The robot code's shot tables were tuned on the
// real ball, so its solver flies the same physics (drag and Magnus lift) the field does.
const defaultShotSpin = 0.6

// shotSpin is a design's backspin ratio.
func shotSpin(arch Archetype) float64 {
	shooter := arch.Spec.Shooter
	if shooter == "" {
		shooter = arch.Model.Shooter
	}
	switch shooter {
	case "kitbot":
		return 0.25 // a roller launcher barely spins it
	case "small":
		return 0.4
	case "turret":
		return 0.55
	}
	return defaultShotSpin
}

// flightStep advances a ball in the vertical plane of its flight by step seconds: gravity, drag, and the lift of a
// backspin w (rad/s).
func flightStep(vx, vz, w, step float64) (float64, float64, float64) {
	v := math.Hypot(vx, vz)
	ax := -dragK*v*vx - magnusK*w*vz
	az := -gravity - dragK*v*vz + magnusK*w*vx
	return vx + ax*step, vz + az*step, w * (1 - step/spinDecay)
}

// solveLaunch picks the shallowest launch angle (from ~58°) whose trajectory drops into the target on the way down
// and clears the near rim of the opening, for the typical shooter's backspin.
func solveLaunch(distance, dz float64) (float64, float64, bool) {
	return solveLaunchSpin(distance, dz, defaultShotSpin)
}

// solveLaunchSpin is solveLaunch for a shooter with the given spin ratio.
func solveLaunchSpin(distance, dz, spin float64) (float64, float64, bool) {
	for angle := 1.02; angle < 1.52; angle += 0.04 {
		speed, descending := solveLaunchSpeedSpin(distance, dz, angle, spin)
		if speed <= 0 || !descending {
			continue
		}
		if distance > rimClearance+0.3 && trajectoryHeight(speed, angle, spin, distance-rimClearance) < dz+0.03 {
			continue
		}
		return speed, angle, true
	}
	return 0, 0, false
}

// trajectoryHeight is the height (relative to launch) of a ball with drag and spin at horizontal distance x.
func trajectoryHeight(speed, angle, spin, x float64) float64 {
	vx, vz := speed*math.Cos(angle), speed*math.Sin(angle)
	w := spin * speed / 0.075
	px, pz := 0.0, 0.0
	const step = 0.004
	for t := 0.0; t < 4 && px < x; t += step {
		vx, vz, w = flightStep(vx, vz, w, step)
		px += vx * step
		pz += vz * step
	}
	return pz
}

// solveLaunchSpeed finds the launch speed that carries a spinless ball (a human player's throw) to the target
// distance at height dz, arriving on the way down.
func solveLaunchSpeed(distance, dz, angle float64) (float64, bool) {
	return solveLaunchSpeedSpin(distance, dz, angle, 0)
}

// solveLaunchSpeedSpin is solveLaunchSpeed for a ball launched with backspin (spin ratio).
func solveLaunchSpeedSpin(distance, dz, angle, spin float64) (float64, bool) {
	heightAt := func(speed float64) (float64, bool) {
		vx := speed * math.Cos(angle)
		vz := speed * math.Sin(angle)
		w := spin * speed / 0.075
		x, z := 0.0, 0.0
		const step = 0.004
		for t := 0.0; t < 4; t += step {
			vx, vz, w = flightStep(vx, vz, w, step)
			x += vx * step
			z += vz * step
			if x >= distance {
				return z, vz < 0
			}
		}
		return -100, false
	}
	low, high := 1.0, 25.0
	if z, _ := heightAt(high); z < dz {
		return 0, false
	}
	for i := 0; i < 40; i++ {
		mid := (low + high) / 2
		if z, _ := heightAt(mid); z < dz {
			low = mid
		} else {
			high = mid
		}
	}
	_, descending := heightAt(high)
	return high, descending
}

// backspin is the spin vector (rad/s) of a ball launched at speed toward horizontal direction yaw with the given spin
// ratio: about the horizontal axis across the shot, top surface moving back.
func backspin(speed, yaw, spin, radius float64) [3]float64 {
	w := spin * speed / radius
	return [3]float64{w * math.Sin(yaw), -w * math.Cos(yaw), 0}
}

// stepClimb advances a climb if the robot is lined up on its tower.
func (world *World) stepClimb(robot *Robot, level int, dt float64) {
	if level > robot.Arch.ClimbLevel {
		level = robot.Arch.ClimbLevel
	}
	if robot.Faults.ClimbFail && level > 1 {
		level = 1
	}
	if robot.ClimbLevel >= level {
		robot.Climbing = false
		return
	}
	zone, heading := world.climbZone(robot.Alliance, robot.ClimbSlot)
	if !robot.Climbing {
		if zone.Sub(robot.Pos).Len() > 0.3 || math.Abs(wrapAngle(robot.Heading-heading)) > 0.4 {
			return
		}
		robot.Climbing = true
		robot.Pos = zone
		robot.Vel = Vec2{}
		robot.Heading = heading
	}
	climbTime := robot.Arch.ClimbTime
	if robot.Odo.Vision {
		climbTime *= 0.9 // vision auto-align on the tower: less time squaring up between rungs
	}
	robot.ClimbProg += dt / climbTime
	if robot.ClimbProg >= float64(robot.ClimbLevel+1) {
		robot.ClimbLevel++
		world.logf("info", "world", "%d (%s) reached tower level %d", robot.Team, robot.Station, robot.ClimbLevel)
	}
	// Lift the robot as it climbs toward the rung.
	targetZ := []float64{0, 0.12, 0.58, 1.04}[min(robot.ClimbLevel+1, 3)]
	progress := robot.ClimbProg - float64(robot.ClimbLevel)
	base := []float64{0, 0.12, 0.58, 1.04}[robot.ClimbLevel]
	robot.Z = base + (targetZ-base)*clamp(progress, 0, 1)
	robot.Pitch = -0.12
}

// climbZone returns where a robot must sit (and face) to climb its alliance's tower. Slot 0 faces the front of the
// tower; slots -1 and +1 hang from the rung overhangs on either side, facing in, so three robots fit without touching.
func (world *World) climbZone(alliance, slot int) (Vec2, float64) {
	t := world.layout.Tower
	var x, y, heading float64
	if slot == 0 {
		x = t.UprightSetback + t.UprightDepth/2 + 0.42
		y = t.CenterY["blue"]
		heading = math.Pi
	} else {
		const halfWidth = 0.47 // a big robot's half-width over its bumpers
		x = t.UprightSetback - 0.05
		y = t.CenterY["blue"] + float64(slot)*(t.UprightSpacing/2+t.RungOverhang+halfWidth-0.08)
		heading = -float64(slot) * math.Pi / 2
	}
	x, y = world.layout.onAlliance(alliance, x, y)
	if alliance == Red {
		heading = wrapAngle(heading + math.Pi)
	}
	return Vec2{x, y}, heading
}

// eject rolls one FUEL out over the front bumper (through the intake, run backwards).
func (world *World) eject(robot *Robot) bool {
	shape := robot.Arch.hopperShape()
	ball, _ := world.heldBallNear(robot, [3]float64{shape.front, 0, shape.floor})
	if ball == nil {
		return false
	}
	forward := robot.forward()
	side := Vec2{-forward.Y, forward.X}.Scale((world.rng.Float64() - 0.5) * 0.3)
	at := robot.Pos.Add(forward.Scale(robot.half().X + world.ballRadius + 0.04)).Add(side)
	ball.State = BallFree
	ball.P = [3]float64{at.X, at.Y, world.ballRadius + 0.01}
	ball.V = [3]float64{forward.X * 1.6, forward.Y * 1.6, 0}
	ball.sleep = 0
	ball.shooter = 0
	robot.Hopper--
	return true
}
