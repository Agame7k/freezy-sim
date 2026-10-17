package world

import (
	"fmt"
	"math"
	"sort"
)

// Pre-programmed autonomous routines. Real AUTO is not a driver: it is a routine the team wrote before the event
// (PathPlanner/Choreo style): a list of timed paths and actions, selected on the driver station chooser before the
// match, run blind on the robot's own pose estimate. It does not see FUEL or other robots and does not replan; a robot
// that gets bumped or blocked keeps following its trajectory clock and ends wherever it ends. Paths are drawn on the
// empty field in the blue frame and rotated onto the red half when the team plays red.
//
// Each team keeps a library of routines (one per routine and starting spot) and tunes them between matches from how
// they went (autotune.go). Drive teams make real mistakes at the chooser — the wrong routine, the routine for another
// starting spot, nothing at all — and alliances check at queueing that their paths will not collide (when they
// remember to).

// Auto step kinds.
const (
	StepPath  = "path"  // follow a timed trajectory through Points
	StepShoot = "shoot" // turn to the hub off the pose estimate and empty the hopper
	StepWait  = "wait"
	StepFeed  = "feed"  // hold at the outpost chute while the human player releases it
	StepPass  = "pass"  // lob FUEL to Target
	StepClimb = "climb" // climb the tower to Level
)

// AutoStep is one command of a routine. Coordinates are blue-frame metres.
type AutoStep struct {
	Kind    string       `json:"kind"`
	Label   string       `json:"label,omitempty"`
	Points  [][2]float64 `json:"points,omitempty"`  // path waypoints, the first is where the path starts
	Heading *float64     `json:"heading,omitempty"` // holonomic end heading (nil: face the direction of travel)
	Speed   float64      `json:"speed,omitempty"`   // path speed limit (m/s)
	Accel   float64      `json:"accel,omitempty"`   // path acceleration limit (m/s²)
	Intake  bool         `json:"intake,omitempty"`
	Reverse bool         `json:"reverse,omitempty"` // tank drive: drive this path backwards
	Seconds float64      `json:"seconds,omitempty"` // time limit for shoot/feed/pass/climb, duration for wait
	Level   int          `json:"level,omitempty"`
	Slot    int          `json:"slot"` // climb: which side of the tower (-1, 0, +1)
	Target  [2]float64   `json:"target,omitempty"`
}

// AutoProgram is one routine in a team's library.
type AutoProgram struct {
	Key       string     `json:"key"` // routine@start
	Name      string     `json:"name"`
	Routine   string     `json:"routine"`
	Start     string     `json:"start"`
	Design    string     `json:"design"`    // the robot design it was written for
	StartPose [3]float64 `json:"startPose"` // blue frame x, y, heading
	Delay     float64    `json:"delay,omitempty"`
	Steps     []AutoStep `json:"steps"`
	Revision  int        `json:"revision"`
	Runs      int        `json:"runs"`
	AvgPoints float64    `json:"avgPoints"`       // running average of the auto points it produced
	Notes     []string   `json:"notes,omitempty"` // the latest tuning changes, newest last
}

var routineLabels = map[string]string{
	AutoPreload: "Preload + leave", AutoCenter: "Center sweep", AutoDepot: "Depot", AutoOutpost: "Outpost",
	AutoClimb: "Preload + L1 climb", AutoShuttle: "Center shuttle",
}

var startLabels = map[string]string{
	"outpost-trench": "outpost trench", "outpost-bump": "outpost bump", "hub": "hub",
	"depot-bump": "depot bump", "depot-trench": "depot trench",
}

func autoKey(routine, start string) string { return routine + "@" + start }

func headingPtr(h float64) *float64 { return &h }

// ---- Writing routines ----------------------------------------------------------------------------------------------

// autoBuilder writes a routine for one robot design, step by step, tracking where the robot should be.
type autoBuilder struct {
	world   *World
	robot   *Robot
	at      Vec2
	heading float64
	steps   []AutoStep
}

func (b *autoBuilder) speed() float64 { return b.robot.Arch.MaxSpeed * 0.8 }
func (b *autoBuilder) accel() float64 { return b.robot.Arch.MaxAccel * 0.55 }

// path drives to a point along a path planned on the empty field.
func (b *autoBuilder) path(to Vec2, heading *float64, intake bool, speed float64, label string) {
	points := []Vec2{b.at}
	if planned := b.world.planStaticPath(b.robot, b.at, to); len(planned) > 0 {
		points = append(points, planned...)
		points[len(points)-1] = to
	} else {
		points = append(points, to)
	}
	step := AutoStep{Kind: StepPath, Label: label, Heading: heading, Intake: intake, Speed: speed, Accel: b.accel()}
	if step.Speed <= 0 {
		step.Speed = b.speed()
	}
	for _, p := range points {
		step.Points = append(step.Points, [2]float64{round3(p.X), round3(p.Y)})
	}
	first := points[1].Sub(points[0])
	last := points[len(points)-1].Sub(points[len(points)-2])
	if b.robot.Arch.Tank && !intake {
		if heading != nil {
			// It must arrive facing a way (the shooter at the hub, the hooks at the rung): back in if that is how.
			step.Reverse = last.Len() > 1e-6 && last.Norm().Dot(Vec2{math.Cos(*heading), math.Sin(*heading)}) < 0
		} else if first.Len() > 1e-6 && first.Norm().Dot(Vec2{math.Cos(b.heading), math.Sin(b.heading)}) < -0.2 {
			step.Reverse = true // a tank drive backs up rather than turning around
		}
	}
	switch {
	case heading != nil && !b.robot.Arch.Tank:
		b.heading = *heading
	case last.Len() > 1e-6:
		b.heading = math.Atan2(last.Y, last.X)
		if step.Reverse {
			b.heading = wrapAngle(b.heading + math.Pi)
		}
	}
	b.steps = append(b.steps, step)
	b.at = to
}

func (b *autoBuilder) shoot(seconds float64, label string) {
	b.steps = append(b.steps, AutoStep{Kind: StepShoot, Label: label, Seconds: seconds})
	hub := b.hub()
	b.heading = math.Atan2(b.at.Y-hub.Y, b.at.X-hub.X)
}

func (b *autoBuilder) add(step AutoStep) { b.steps = append(b.steps, step) }

func (b *autoBuilder) hub() Vec2 {
	p := b.world.layout.Hub.Positions["blue"]
	return Vec2{p[0], p[1]}
}

// shootSpot is a shooting spot in the alliance zone on the side of the hub nearest y, and the heading that points the
// rear-mounted shooter at the hub.
func (b *autoBuilder) shootSpot(y float64) (Vec2, float64) {
	hub := b.hub()
	distance := math.Min(math.Max(b.robot.Arch.MaxRange*0.6, 2.5), 2.8)
	angle := clamp((y-hub.Y)/3, -0.55, 0.55)
	spot := hub.Add(Vec2{-distance * math.Cos(angle), distance * math.Sin(angle)})
	return spot, math.Atan2(spot.Y-hub.Y, spot.X-hub.X)
}

// laneY is the neutral-zone lane a robot sweeps from a starting spot (partners from different spots use different
// lanes).
func (b *autoBuilder) laneY(startY float64) float64 {
	l := b.world.layout
	box := l.Fuel.NeutralBox
	low, high := box.CenterY-box.WidthY/2+0.5, box.CenterY+box.WidthY/2-0.5
	return clamp(low+(startY-0.85)/(l.Field.Width-1.7)*(high-low), low, high)
}

// sweep drives into the near half of the neutral FUEL box along a lane, slides across and comes back out with the
// intake running, staying behind the center line (G403).
func (b *autoBuilder) sweep(laneY float64) {
	l := b.world.layout
	box := l.Fuel.NeutralBox
	half := b.robot.half().X
	outX := box.CenterX - box.LengthX/2 - half - 0.35
	deepX := l.Field.Length/2 - 0.25
	shift := 0.75
	if laneY > l.Field.Width/2 {
		shift = -0.75
	}
	b.path(Vec2{outX, laneY}, headingPtr(0), false, 0, "to the neutral zone")
	b.path(Vec2{deepX, laneY}, nil, true, 1.8, "sweep in")
	b.path(Vec2{deepX, laneY + shift}, nil, true, 1.2, "sweep across")
	b.path(Vec2{outX + 0.3, laneY + shift}, nil, true, 1.8, "sweep out")
}

// buildAutoProgram writes one routine for a robot's design from a starting spot.
func (world *World) buildAutoProgram(robot *Robot, routine, start string) *AutoProgram {
	l := world.layout
	startY, ok := startSpots[start]
	if !ok {
		start, startY = "hub", startSpots["hub"]
	}
	if routine == AutoClimb && robot.Arch.ClimbLevel < 1 {
		routine = AutoPreload
	}
	if _, ok := routineLabels[routine]; !ok {
		routine = AutoPreload
	}
	b := &autoBuilder{world: world, robot: robot, at: Vec2{l.Zones.AllianceZoneDepth - robot.half().X - 0.06, startY}}
	program := &AutoProgram{Key: autoKey(routine, start), Routine: routine, Start: start, Design: robot.Arch.Name,
		Name: routineLabels[routine] + " (" + startLabels[start] + ")", StartPose: [3]float64{round3(b.at.X), round3(b.at.Y), 0}}
	spot, shootHeading := b.shootSpot(startY)
	half := robot.half().X
	switch routine {
	case AutoDepot:
		depotY := l.Depot.CenterY["blue"]
		spot, shootHeading = b.shootSpot(depotY)
		b.path(spot, headingPtr(shootHeading), false, 0, "to the shooting spot")
		b.shoot(3, "shoot the preload")
		from := Vec2{half + 0.06, depotY - l.Depot.Width/2 - 0.45}
		to := Vec2{half + 0.06, math.Min(depotY+l.Depot.Width/2+0.4, l.Field.Width-half-0.06)}
		b.path(from, headingPtr(math.Pi/2), false, 0, "to the depot")
		b.path(to, nil, true, 1.5, "sweep the depot")
		b.path(spot, headingPtr(shootHeading), false, 0, "back to shoot")
		b.shoot(6, "shoot the depot FUEL")
	case AutoOutpost:
		outpost := world.outposts[Blue]
		spot, shootHeading = b.shootSpot(outpost.chuteY)
		b.path(spot, headingPtr(shootHeading), false, 0, "to the shooting spot")
		b.shoot(3, "shoot the preload")
		feedSpot := Vec2{outpost.wallX + half + 0.12, outpost.chuteY}
		b.path(feedSpot.Add(Vec2{0.6, 0}), headingPtr(math.Pi), false, 0, "to the outpost")
		b.path(feedSpot, headingPtr(math.Pi), false, 0.9, "line up on the chute")
		b.add(AutoStep{Kind: StepFeed, Label: "take the chute", Seconds: 4.5})
		b.path(spot, headingPtr(shootHeading), false, 0, "back to shoot")
		b.shoot(6, "shoot the chute FUEL")
	case AutoCenter:
		b.path(spot, headingPtr(shootHeading), false, 0, "to the shooting spot")
		b.shoot(3, "shoot the preload")
		b.sweep(b.laneY(startY))
		spot, shootHeading = b.shootSpot(b.at.Y)
		b.path(spot, headingPtr(shootHeading), false, 0, "back to shoot")
		b.shoot(8, "shoot the neutral FUEL")
	case AutoShuttle:
		lane := b.laneY(startY)
		b.sweep(lane)
		hub := b.hub()
		passY := b.at.Y
		if math.Abs(passY-hub.Y) < l.Hub.Size/2+0.5 {
			passY = hub.Y + math.Copysign(l.Hub.Size/2+0.7, passY-hub.Y+1e-3)
		}
		passSpot := Vec2{hub.X + l.Hub.Size/2 + 1.3, passY}
		target := Vec2{hub.X - 1.7, passY}
		b.path(passSpot, headingPtr(0), false, 0, "to the passing lane")
		b.add(AutoStep{Kind: StepPass, Label: "lob FUEL home", Seconds: 8, Target: [2]float64{round3(target.X), round3(target.Y)}})
	case AutoClimb:
		b.path(spot, headingPtr(shootHeading), false, 0, "to the shooting spot")
		b.shoot(3.5, "shoot the preload")
		slot := 0
		switch {
		case startY < 3:
			slot = -1
		case startY > 5:
			slot = 1
		}
		zone, heading := world.climbZone(Blue, slot)
		approach := zone.Sub(Vec2{math.Cos(heading), math.Sin(heading)}.Scale(0.7))
		b.path(approach, headingPtr(heading), false, 0, "to the tower")
		b.path(zone, headingPtr(heading), false, 0.8, "line up on the rung")
		b.add(AutoStep{Kind: StepClimb, Label: "climb L1", Level: 1, Slot: slot, Seconds: robot.Arch.ClimbTime*1.4 + 1.5})
	default: // preload
		b.path(spot, headingPtr(shootHeading), false, 0, "to the shooting spot")
		b.shoot(4, "shoot the preload")
	}
	program.Steps = b.steps
	return program
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// autoLibrary returns a team's routine for a routine and starting spot, writing it if the team does not have one yet
// (or if the team's robot design changed).
func (world *World) autoLibrary(robot *Robot, routine, start string) *AutoProgram {
	if world.learning.Autos == nil {
		world.learning.Autos = map[int]map[string]*AutoProgram{}
	}
	library := world.learning.Autos[robot.Team]
	if library == nil {
		library = map[string]*AutoProgram{}
		world.learning.Autos[robot.Team] = library
	}
	key := autoKey(routine, start)
	if program, ok := library[key]; ok && program.Design == robot.Arch.Name {
		return program
	}
	program := world.buildAutoProgram(robot, routine, start)
	library[program.Key] = program // (a climb routine for a robot without a climber is stored as its preload routine)
	if program.Key != key {
		library[key] = program
	}
	world.markDirtyLocked()
	return program
}

// ---- The chooser and queueing --------------------------------------------------------------------------------------

// autoChoice is what a drive team selected for this match.
type autoChoice struct {
	program    *AutoProgram // a copy for this match; nil = nothing selected
	key        string       // library key (for tuning)
	intended   string       // the routine the alliance planned
	spot       string       // where the robot actually stands
	mistake    string
	note       string // agreed at queueing
	routine    string // what the team chose to run (before any mistake)
	experiment bool
	samples    []Vec2 // nominal positions every autoSample seconds (blue frame), for the collision check
}

const autoSample = 0.1

// nearestStartSpot is the starting spot a robot is standing on (blue-frame y).
func (world *World) nearestStartSpot(robot *Robot) string {
	_, y := world.layout.onAlliance(robot.Alliance, robot.Pos.X, robot.Pos.Y)
	best, bestDistance := "hub", math.Inf(1)
	for _, spot := range StartSpots {
		if d := math.Abs(startSpots[spot] - y); d < bestDistance {
			best, bestDistance = spot, d
		}
	}
	return best
}

// prepareAutos runs the alliance's pre-match: each drive team selects its routine on the DS, then the alliance checks
// its paths against each other. Called once per match (by the first robot of the alliance to start auto).
func (world *World) prepareAutos(alliance int) {
	if world.time-world.autoPreparedAt[alliance] < 5 && world.autoPreparedAt[alliance] > 0 {
		return
	}
	world.autoPreparedAt[alliance] = world.time
	var members []*Robot
	for i, robot := range world.robots {
		world.autoChoices[i] = nil
		if robot.Alliance != alliance || !robot.Present || robot.Manual {
			continue
		}
		agent, ok := world.controllers[i].(*Agent)
		if !ok {
			continue
		}
		world.autoChoices[i] = world.chooseAuto(robot, agent)
		members = append(members, robot)
	}
	world.coordinateAutos(members)
	for _, robot := range members {
		choice := world.autoChoices[robot.Index]
		robot.AutoName = "Do Nothing"
		if choice.program != nil {
			robot.AutoName = choice.program.Name
		}
	}
}

// chooseAuto is the drive team at the DS chooser.
func (world *World) chooseAuto(robot *Robot, agent *Agent) *autoChoice {
	plan := world.plans[robot.Index]
	routine := plan.Auto
	if routine == "" {
		routine = AutoPreload
	}
	spot := world.nearestStartSpot(robot)
	choice := &autoChoice{intended: routine, spot: spot}
	stats := world.teamStats(robot.Team)
	// The team picks from its own record: the alliance's planned routine has a head start, but a routine that keeps
	// working gets run, and curious teams try the ones they have not run much (memory.go).
	var options []string
	for _, option := range AutoRoutines {
		if option != AutoClimb || robot.Arch.ClimbLevel > 0 {
			options = append(options, option)
		}
	}
	if world.overrides[robot.Index].Auto == "" {
		routine, choice.experiment = world.pickChoice(stats, ChoiceAuto, options, routine, practiceCuriosity(robot, agent.Personality.Curiosity),
			agent.rng.Float64)
	}
	choice.routine = routine
	program := world.autoLibrary(robot, routine, spot)
	// Mistakes at the chooser: less likely for disciplined drive teams, and every mistake a team makes teaches it
	// to double-check (a pre-match checklist).
	chance := 0.07 * (1 - agent.Personality.Discipline) * math.Pow(0.6, float64(stats.AutoMistakes))
	roll := agent.rng.Float64()
	switch {
	case roll < chance*0.25:
		choice.mistake = "no auto selected"
		program = nil
	case roll < chance*0.6:
		others := []string{}
		for _, other := range StartSpots {
			if other != spot {
				others = append(others, other)
			}
		}
		wrong := others[agent.rng.Intn(len(others))]
		program = world.autoLibrary(robot, routine, wrong)
		choice.mistake = "selected the " + startLabels[wrong] + " routine from the " + startLabels[spot] + " spot"
	case roll < chance:
		var others []string
		for _, other := range AutoRoutines {
			if other != routine && (other != AutoClimb || robot.Arch.ClimbLevel > 0) {
				others = append(others, other)
			}
		}
		wrong := others[agent.rng.Intn(len(others))]
		program = world.autoLibrary(robot, wrong, spot)
		choice.mistake = "selected " + routineLabels[wrong] + " instead of " + routineLabels[routine]
	}
	if choice.mistake != "" {
		world.logf("warn", "ai", "%d (%s) at the chooser: %s", robot.Team, robot.Station, choice.mistake)
	}
	if program != nil {
		copied := *program
		choice.program = &copied
		choice.key = program.Key
		choice.samples = world.nominalPath(robot, choice.program)
	}
	return choice
}

// coordinateAutos is the alliance at queueing: partners whose routines would collide change one of them (another
// routine from the same spot, or a delayed start). Not every alliance remembers to check.
func (world *World) coordinateAutos(members []*Robot) {
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			a, b := members[i], members[j]
			ca, cb := world.autoChoices[a.Index], world.autoChoices[b.Index]
			if ca == nil || cb == nil || ca.program == nil || cb.program == nil {
				continue
			}
			if !autoConflict(ca.samples, cb.samples, a.half().X+b.half().X+0.1) {
				continue
			}
			agentA, _ := world.controllers[a.Index].(*Agent)
			agentB, _ := world.controllers[b.Index].(*Agent)
			discipline := (agentA.Personality.Discipline + agentB.Personality.Discipline) / 2
			if agentB.rng.Float64() > 0.35+0.6*discipline {
				world.logf("info", "ai", "%d and %d did not compare autos at queueing (their paths cross)", a.Team, b.Team)
				continue
			}
			if !world.resolveConflict(b, cb, ca, a) {
				world.resolveConflict(a, ca, cb, b)
			}
		}
	}
}

// resolveConflict changes mover's routine so it no longer collides with other's: first another routine from the same
// spot, then a delayed start. It reports success.
func (world *World) resolveConflict(mover *Robot, choice, other *autoChoice, otherRobot *Robot) bool {
	clearance := mover.half().X + otherRobot.half().X + 0.1
	for _, routine := range []string{AutoPreload, AutoDepot, AutoOutpost, AutoClimb, AutoCenter} {
		if routine == choice.program.Routine || (routine == AutoClimb && mover.Arch.ClimbLevel < 1) {
			continue
		}
		program := world.autoLibrary(mover, routine, choice.spot)
		copied := *program
		samples := world.nominalPath(mover, &copied)
		if !autoConflict(samples, other.samples, clearance) {
			choice.program, choice.key, choice.samples, choice.routine = &copied, program.Key, samples, program.Routine
			choice.note = fmt.Sprintf("switched to %s so its path stays clear of %d", program.Name, otherRobot.Team)
			world.logf("info", "ai", "%d %s", mover.Team, choice.note)
			return true
		}
	}
	for _, delay := range []float64{1.5, 3, 4.5} {
		copied := *choice.program
		copied.Delay = delay
		samples := world.nominalPath(mover, &copied)
		if !autoConflict(samples, other.samples, clearance) {
			choice.program, choice.samples = &copied, samples
			choice.note = fmt.Sprintf("waits %.1f s at the start to let %d through", delay, otherRobot.Team)
			world.logf("info", "ai", "%d %s", mover.Team, choice.note)
			return true
		}
	}
	return false
}

// autoConflict reports whether two nominal paths come within clearance of each other at the same time (after the
// first half second, when both are still on the starting line).
func autoConflict(a, b []Vec2, clearance float64) bool {
	for k := int(0.5 / autoSample); k < len(a) && k < len(b); k++ {
		if a[k].Sub(b[k]).Len() < clearance {
			return true
		}
	}
	return false
}

// nominalPath is where a routine expects the robot to be (blue frame) every autoSample seconds through AUTO.
func (world *World) nominalPath(robot *Robot, program *AutoProgram) []Vec2 {
	samples := make([]Vec2, 0, int(AutoSec/autoSample)+1)
	at := Vec2{program.StartPose[0], program.StartPose[1]}
	t := 0.0
	push := func(until float64, position func(float64) Vec2) {
		for float64(len(samples))*autoSample < until && float64(len(samples))*autoSample <= AutoSec {
			samples = append(samples, position(float64(len(samples))*autoSample))
		}
	}
	stay := func(float64) Vec2 { return at }
	t += program.Delay
	push(t, stay)
	hopper := world.layout.Fuel.PreloadPerRobot
	for _, step := range program.Steps {
		duration := 0.0
		switch step.Kind {
		case StepPath:
			trajectory := newTrajectory(stepPoints(step, Blue, world.layout), step.Speed, step.Accel)
			begin := t
			duration = trajectory.total
			push(t+duration, func(s float64) Vec2 { p, _ := trajectory.sample(s - begin); return p })
			at = trajectory.end()
			if step.Intake {
				hopper += 10
			}
		case StepShoot:
			duration = math.Min(step.Seconds, 0.6+float64(hopper)/math.Max(1, robot.Arch.ShootRate))
			hopper = 0
		case StepPass:
			duration = math.Min(step.Seconds, 0.5+float64(hopper)/math.Max(1, robot.Arch.PassRate))
			hopper = 0
		case StepFeed:
			duration = step.Seconds
			hopper += 20
		default:
			duration = step.Seconds
		}
		t += duration
		push(t, stay)
		if t > AutoSec {
			break
		}
	}
	push(AutoSec+autoSample, stay)
	return samples
}

// stepPoints converts a path's blue-frame waypoints onto an alliance's half.
func stepPoints(step AutoStep, alliance int, layout *Layout) []Vec2 {
	points := make([]Vec2, len(step.Points))
	for i, p := range step.Points {
		x, y := layout.onAlliance(alliance, p[0], p[1])
		points[i] = Vec2{x, y}
	}
	return points
}

// allianceHeading converts a blue-frame heading onto an alliance's half.
func allianceHeading(alliance int, heading float64) float64 {
	if alliance == Red {
		return wrapAngle(heading + math.Pi)
	}
	return heading
}

// ---- Trajectories ----------------------------------------------------------------------------------------------------

// trajectory is a path with a trapezoidal speed profile (starting and ending at rest).
type trajectory struct {
	points      []Vec2
	cumulative  []float64
	length      float64
	vmax, amax  float64
	accelTime   float64
	cruiseSpeed float64
	total       float64
}

func newTrajectory(points []Vec2, vmax, amax float64) *trajectory {
	if vmax <= 0 {
		vmax = 3
	}
	if amax <= 0 {
		amax = 3
	}
	t := &trajectory{points: points, vmax: vmax, amax: amax}
	t.cumulative = make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		t.cumulative[i] = t.cumulative[i-1] + points[i].Sub(points[i-1]).Len()
	}
	if len(points) > 0 {
		t.length = t.cumulative[len(points)-1]
	}
	t.cruiseSpeed = vmax
	t.accelTime = vmax / amax
	if accelDistance := 0.5 * amax * t.accelTime * t.accelTime; 2*accelDistance > t.length {
		t.cruiseSpeed = math.Sqrt(t.length * amax)
		t.accelTime = t.cruiseSpeed / amax
		t.total = 2 * t.accelTime
	} else {
		t.total = 2*t.accelTime + (t.length-2*accelDistance)/vmax
	}
	return t
}

// progress is the distance along the path and the speed at time s.
func (t *trajectory) progress(s float64) (float64, float64) {
	switch {
	case s <= 0:
		return 0, 0
	case s >= t.total:
		return t.length, 0
	case s < t.accelTime:
		return 0.5 * t.amax * s * s, t.amax * s
	case s > t.total-t.accelTime:
		left := t.total - s
		return t.length - 0.5*t.amax*left*left, t.amax * left
	}
	accelDistance := 0.5 * t.amax * t.accelTime * t.accelTime
	return accelDistance + (s-t.accelTime)*t.cruiseSpeed, t.cruiseSpeed
}

// sample is the setpoint position and velocity at time s.
func (t *trajectory) sample(s float64) (Vec2, Vec2) {
	if len(t.points) == 0 {
		return Vec2{}, Vec2{}
	}
	distance, speed := t.progress(s)
	for i := 1; i < len(t.points); i++ {
		if distance <= t.cumulative[i] || i == len(t.points)-1 {
			segment := t.points[i].Sub(t.points[i-1])
			length := segment.Len()
			if length < 1e-9 {
				continue
			}
			f := clamp((distance-t.cumulative[i-1])/length, 0, 1)
			return t.points[i-1].Add(segment.Scale(f)), segment.Scale(speed / length)
		}
	}
	return t.points[len(t.points)-1], Vec2{}
}

func (t *trajectory) end() Vec2 {
	if len(t.points) == 0 {
		return Vec2{}
	}
	return t.points[len(t.points)-1]
}

// ---- Running a routine -----------------------------------------------------------------------------------------------

// AutoStepResult is how one step went.
type AutoStepResult struct {
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	Completed bool    `json:"completed"` // finished normally (not cut off by its time limit or the end of AUTO)
	EndError  float64 `json:"endError"`  // paths: how far the robot really ended from the path's end
	EstError  float64 `json:"estError"`  // odometry error when the step ended
	Contact   string  `json:"contact,omitempty"`
	Seconds   float64 `json:"seconds"`
	Fired     int     `json:"fired,omitempty"`
	Collected int     `json:"collected,omitempty"`
}

// AutoResult is how a robot's AUTO went, for the review between matches.
type AutoResult struct {
	Program    string           `json:"program"`
	Routine    string           `json:"routine,omitempty"`    // the routine the team chose (credited to its record)
	Experiment bool             `json:"experiment,omitempty"` // trying something other than its usual
	Key        string           `json:"key"`
	Revision   int              `json:"revision"`
	Mistake    string           `json:"mistake,omitempty"`
	Note       string           `json:"note,omitempty"`
	Steps      []AutoStepResult `json:"steps"`
	Fuel       int              `json:"fuel"`
	Passes     int              `json:"passes,omitempty"` // FUEL lobbed home for teleop
	Tower      int              `json:"tower"`
	Vision     bool             `json:"vision"`
	EndError   float64          `json:"endError"` // odometry error at the end of AUTO
}

// autoRun is a routine executing on one robot.
type autoRun struct {
	program     *AutoProgram
	result      AutoResult
	step        int
	stepAt      float64 // world time the current step began (< 0: not begun)
	trajectory  *trajectory
	fromHeading float64
	lastHeading float64
	delayUntil  float64
	shotsAt     int
	collectedAt int
	contact     string
	finished    bool
}

const (
	autoPathKP      = 2.2  // 1/s position feedback on the trajectory setpoint
	autoHeadingKP   = 5.0  // 1/s
	autoPathSettle  = 0.6  // seconds a finished trajectory waits to settle on its end point
	autoPathTol     = 0.07 // metres
	autoAimTol      = 0.05 // radians
	autoRotateSpeed = 0.8  // fraction of max turn rate while lining up a shot
)

// beginAuto runs the alliance's pre-match (once), zeroes the robot's odometry where its routine starts, and arms the
// routine.
func (agent *Agent) beginAuto(robot *Robot, world *World) {
	world.prepareAutos(robot.Alliance)
	choice := world.autoChoices[robot.Index]
	calibration := world.teamStats(robot.Team).OdoCal
	// The drive team sets the robot on its spot by eye: a few centimetres and a degree or so off.
	sloppy := 1.6 - agent.Personality.Consistency
	robot.Pos = robot.Pos.Add(Vec2{agent.rng.NormFloat64(), agent.rng.NormFloat64()}.Scale(placementSigma * sloppy))
	robot.Heading = wrapAngle(robot.Heading + agent.rng.NormFloat64()*placementYawSD*sloppy)
	run := &autoRun{stepAt: -1}
	agent.auto = run
	if choice == nil || choice.program == nil {
		world.resetOdometry(robot, robot.Pos, robot.Heading, calibration)
		if choice != nil {
			run.result.Mistake = choice.mistake
		}
		run.result.Program = "Do Nothing"
		run.result.Vision = robot.Odo.Vision
		return
	}
	program := choice.program
	run.program = program
	run.result = AutoResult{Program: program.Name, Key: choice.key, Revision: program.Revision, Mistake: choice.mistake,
		Note: choice.note, Routine: choice.routine, Experiment: choice.experiment}
	x, y := world.layout.onAlliance(robot.Alliance, program.StartPose[0], program.StartPose[1])
	world.resetOdometry(robot, Vec2{x, y}, allianceHeading(robot.Alliance, program.StartPose[2]), calibration)
	run.result.Vision = robot.Odo.Vision
	run.delayUntil = world.time + program.Delay
	run.lastHeading = robot.Odo.EstHeading
}

// runAuto executes the selected routine (blind: only the pose estimate, no sight of FUEL or robots).
func (agent *Agent) runAuto(robot *Robot, world *World) {
	run := agent.auto
	if run == nil || run.program == nil {
		robot.AiState = "auto:idle"
		agent.Intent = "auto: no routine selected, sitting still"
		if run != nil && run.result.Mistake != "" {
			agent.Intent = "auto: " + run.result.Mistake
		}
		return
	}
	steps := run.program.Steps
	if world.time < run.delayUntil {
		robot.AiState = "auto:wait"
		agent.Intent = fmt.Sprintf("auto (%s): waiting %.1f s for a partner", run.program.Name, run.delayUntil-world.time)
		return
	}
	if run.step >= len(steps) {
		robot.AiState = "auto:done"
		agent.Intent = "auto (" + run.program.Name + "): routine finished"
		return
	}
	step := steps[run.step]
	if run.stepAt < 0 {
		agent.startStep(robot, world, step)
	}
	agent.noteContact(robot, world)
	elapsed := world.time - run.stepAt
	done, completed := false, false
	switch step.Kind {
	case StepPath:
		done, completed = agent.autoPath(robot, world, step, elapsed)
	case StepShoot:
		done, completed = agent.autoShoot(robot, world, step, elapsed)
	case StepFeed:
		done, completed = agent.autoFeed(robot, world, step, elapsed)
	case StepPass:
		done, completed = agent.autoPass(robot, world, step, elapsed)
	case StepClimb:
		robot.ClimbSlot = step.Slot
		if !robot.Climbing {
			// Keep squaring up on the rung (off the pose estimate) until the hooks catch.
			zone, heading := world.climbZone(robot.Alliance, step.Slot)
			agent.holdAt(robot, zone, heading, 0.6)
		}
		robot.Ctl.Climb = step.Level
		done = elapsed > step.Seconds
		completed = robot.ClimbLevel >= step.Level
	default: // wait
		done, completed = elapsed >= step.Seconds, true
	}
	robot.AiState = fmt.Sprintf("auto:%s", step.Kind)
	label := step.Label
	if label == "" {
		label = step.Kind
	}
	agent.Intent = fmt.Sprintf("auto (%s) %d/%d: %s", run.program.Name, run.step+1, len(steps), label)
	if done {
		agent.endStep(robot, world, step, completed)
	}
}

func (agent *Agent) startStep(robot *Robot, world *World, step AutoStep) {
	run := agent.auto
	run.stepAt = world.time
	run.contact = ""
	run.shotsAt = robot.Shots + robot.Passes
	run.collectedAt = robot.Collected
	run.fromHeading = robot.Odo.EstHeading
	run.trajectory = nil
	if step.Kind == StepPath {
		run.trajectory = newTrajectory(stepPoints(step, robot.Alliance, world.layout), step.Speed, step.Accel)
	}
}

func (agent *Agent) endStep(robot *Robot, world *World, step AutoStep, completed bool) {
	run := agent.auto
	result := AutoStepResult{Kind: step.Kind, Label: step.Label, Completed: completed, Contact: run.contact,
		Seconds: round2(world.time - run.stepAt), EstError: round2(robot.odoError()),
		Fired: robot.Shots + robot.Passes - run.shotsAt, Collected: robot.Collected - run.collectedAt}
	if run.trajectory != nil {
		result.EndError = round2(robot.Pos.Sub(run.trajectory.end()).Len())
	}
	run.result.Steps = append(run.result.Steps, result)
	run.step++
	run.stepAt = -1
}

// finishAuto records the step that AUTO cut off, once teleop begins.
func (agent *Agent) finishAuto(robot *Robot, world *World) {
	run := agent.auto
	if run == nil || run.finished {
		return
	}
	run.finished = true
	if run.program != nil && run.step < len(run.program.Steps) && run.stepAt >= 0 {
		step := run.program.Steps[run.step]
		agent.endStep(robot, world, step, step.Kind == StepClimb && robot.ClimbLevel >= step.Level)
	}
	run.result.EndError = round2(robot.odoError())
	run.result.Passes = robot.Passes
}

// noteContact remembers bumping into another robot during the current step.
func (agent *Agent) noteContact(robot *Robot, world *World) {
	for _, other := range world.robots {
		if other == robot || !other.onField() || !touching(robot, other) {
			continue
		}
		if other.Alliance == robot.Alliance {
			agent.auto.contact = "partner"
		} else if agent.auto.contact == "" {
			agent.auto.contact = "opponent"
		}
	}
}

// drive commands a field-relative velocity and heading as the robot code computes them: in the frame its gyro
// believes, so heading error rotates the motion. A tank drive can only drive along its heading.
func (agent *Agent) drive(robot *Robot, velocity Vec2, heading float64, turnScale float64) {
	if speed := velocity.Len(); speed > robot.Arch.MaxSpeed {
		velocity = velocity.Scale(robot.Arch.MaxSpeed / speed)
	}
	estHeading := robot.Odo.EstHeading
	headingError := wrapAngle(heading - estHeading)
	if robot.Arch.Tank {
		forward := Vec2{math.Cos(estHeading), math.Sin(estHeading)}
		along := velocity.Dot(forward) * math.Max(0, math.Cos(headingError))
		velocity = forward.Scale(along)
	}
	robot.Ctl.Vel = velocity.Rotate(-robot.odoHeadingError())
	limit := robot.Arch.MaxOmega * turnScale
	robot.Ctl.Omega = clamp(headingError*autoHeadingKP, -limit, limit)
}

// autoPath follows the step's trajectory clock with feedback on the pose estimate.
func (agent *Agent) autoPath(robot *Robot, world *World, step AutoStep, elapsed float64) (bool, bool) {
	run := agent.auto
	trajectory := run.trajectory
	setpoint, feedforward := trajectory.sample(elapsed)
	estimate := robot.Odo.Est
	velocity := feedforward.Add(setpoint.Sub(estimate).Scale(autoPathKP))
	// Heading: a holonomic robot turns toward the end heading over most of the path (or faces its travel); a tank
	// drive points along its velocity (backwards on reversed paths).
	heading := run.lastHeading
	switch {
	case robot.Arch.Tank:
		if velocity.Len() > 0.15 {
			heading = math.Atan2(velocity.Y, velocity.X)
			if step.Reverse {
				heading = wrapAngle(heading + math.Pi)
			}
		}
	case step.Heading != nil:
		end := allianceHeading(robot.Alliance, *step.Heading)
		f := clamp(elapsed/math.Max(0.3, trajectory.total*0.75), 0, 1)
		heading = wrapAngle(run.fromHeading + wrapAngle(end-run.fromHeading)*f)
	case feedforward.Len() > 0.2:
		heading = math.Atan2(feedforward.Y, feedforward.X)
	}
	run.lastHeading = heading
	agent.target, agent.hasTarget = trajectory.end(), true
	robot.Ctl.Intake = step.Intake
	agent.drive(robot, velocity, heading, 1)
	if elapsed < trajectory.total {
		return false, false
	}
	settled := estimate.Sub(trajectory.end()).Len() < autoPathTol
	return settled || elapsed > trajectory.total+autoPathSettle, true
}

// holdAt keeps the robot on a point (off its pose estimate) while it does something else.
func (agent *Agent) holdAt(robot *Robot, point Vec2, heading float64, turnScale float64) {
	agent.drive(robot, point.Sub(robot.Odo.Est).Scale(autoPathKP), heading, turnScale)
}

// autoHoldPoint is where the current step should be standing: the end of the previous path (or the start pose).
func (agent *Agent) autoHoldPoint(robot *Robot, world *World) Vec2 {
	run := agent.auto
	for k := run.step - 1; k >= 0; k-- {
		if step := run.program.Steps[k]; step.Kind == StepPath && len(step.Points) > 0 {
			points := stepPoints(step, robot.Alliance, world.layout)
			return points[len(points)-1]
		}
	}
	x, y := world.layout.onAlliance(robot.Alliance, run.program.StartPose[0], run.program.StartPose[1])
	return Vec2{x, y}
}

// autoShoot turns the rear shooter to where the robot believes the hub is and empties the hopper.
func (agent *Agent) autoShoot(robot *Robot, world *World, step AutoStep, elapsed float64) (bool, bool) {
	hub := world.hubs[robot.Alliance]
	away := robot.Odo.Est.Sub(hub.Center)
	heading := math.Atan2(away.Y, away.X)
	agent.holdAt(robot, agent.autoHoldPoint(robot, world), heading, autoRotateSpeed)
	agent.target, agent.hasTarget = hub.Center, true
	if robot.Arch.Turret || math.Abs(wrapAngle(heading-robot.Odo.EstHeading)) < autoAimTol {
		robot.Ctl.Shoot = true
	}
	if robot.Hopper == 0 {
		return true, true
	}
	return elapsed > step.Seconds, false
}

// autoFeed holds at the outpost chute with the intake running while the human player releases it.
func (agent *Agent) autoFeed(robot *Robot, world *World, step AutoStep, elapsed float64) (bool, bool) {
	outpost := world.outposts[robot.Alliance]
	heading := math.Pi
	if outpost.inward < 0 {
		heading = 0
	}
	agent.holdAt(robot, agent.autoHoldPoint(robot, world), heading, 1)
	robot.Ctl.Intake = true
	outpost.robotComing = world.time
	outpost.holdOpenUntil = world.time + 0.5
	if robot.Hopper >= robot.Arch.Capacity*9/10 {
		return true, true
	}
	return elapsed > step.Seconds, true
}

// autoPass lobs the hopper toward a spot in the alliance zone.
func (agent *Agent) autoPass(robot *Robot, world *World, step AutoStep, elapsed float64) (bool, bool) {
	x, y := world.layout.onAlliance(robot.Alliance, step.Target[0], step.Target[1])
	target := Vec2{x, y}
	away := robot.Odo.Est.Sub(target)
	heading := math.Atan2(away.Y, away.X)
	agent.holdAt(robot, agent.autoHoldPoint(robot, world), heading, autoRotateSpeed)
	agent.target, agent.hasTarget = target, true
	if math.Abs(wrapAngle(heading-robot.Odo.EstHeading)) < 0.15 {
		robot.Ctl.Pass = true
		robot.Ctl.PassTarget = target
	}
	if robot.Hopper == 0 {
		return true, true
	}
	return elapsed > step.Seconds, false
}

// ---- What the panels show ------------------------------------------------------------------------------------------

// AutoView is a robot's routine for the AI panel and the 3D overlay.
type AutoView struct {
	Program  string           `json:"program"`
	Step     int              `json:"step"`
	Steps    int              `json:"steps"`
	Label    string           `json:"label,omitempty"`
	Mistake  string           `json:"mistake,omitempty"`
	Note     string           `json:"note,omitempty"`
	Revision int              `json:"revision"`
	Path     [][2]float64     `json:"path,omitempty"` // the whole routine's path on the field
	Results  []AutoStepResult `json:"results,omitempty"`
}

func (world *World) autoView(robot *Robot, agent *Agent) *AutoView {
	run := agent.auto
	if run == nil {
		return nil
	}
	view := &AutoView{Program: run.result.Program, Mistake: run.result.Mistake, Note: run.result.Note,
		Revision: run.result.Revision, Results: run.result.Steps}
	if run.program == nil {
		return view
	}
	view.Step, view.Steps = run.step, len(run.program.Steps)
	if run.step < len(run.program.Steps) {
		view.Label = run.program.Steps[run.step].Label
	}
	for _, step := range run.program.Steps {
		if step.Kind != StepPath {
			continue
		}
		points := stepPoints(step, robot.Alliance, world.layout)
		for _, p := range points {
			view.Path = append(view.Path, [2]float64{round2(p.X), round2(p.Y)})
		}
	}
	return view
}

// AutoLibraryEntry is one routine in a team's library, for the AI panel.
type AutoLibraryEntry struct {
	Team int `json:"team"`
	AutoProgram
}

// autoLibraryView lists every team's routines (sorted by team, then routine).
func (world *World) autoLibraryView(teams map[int]bool) []AutoLibraryEntry {
	var entries []AutoLibraryEntry
	for team, library := range world.learning.Autos {
		if teams != nil && !teams[team] {
			continue
		}
		seen := map[*AutoProgram]bool{}
		for _, program := range library {
			if seen[program] || program.Runs == 0 {
				continue
			}
			seen[program] = true
			entry := AutoLibraryEntry{Team: team, AutoProgram: *program}
			entry.Steps = nil
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Team != entries[j].Team {
			return entries[i].Team < entries[j].Team
		}
		return entries[i].Key < entries[j].Key
	})
	return entries
}
