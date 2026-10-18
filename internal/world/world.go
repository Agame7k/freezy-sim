package world

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/ds"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const (
	stepHz        = 120
	snapshotEvery = 2  // 60 Hz to the 3D view
	agentEvery    = 4  // 30 Hz decisions
	stateEvery    = 12 // 10 Hz JSON summary
	manualTimeout = 600 * time.Millisecond
)

// StationLink is the robot's connection to its driver station.
type StationLink interface {
	Command() ds.Command
	SetLoad(amps float64)
}

// FieldLink is the field I/O the world drives (hub sensors) and reads (hub motors and lights).
type FieldLink interface {
	ScoreFuel(alliance, sensor int)
	HubMotorsOn() (red, blue bool)
	HubLightsOn() (red, blue bool)
}

// Controller decides what a robot does (AI drivers implement this).
type Controller interface {
	Update(robot *Robot, world *World, dt float64)
}

// Static is a solid field element.
type Static struct {
	Name        string
	Box         AABB
	Robots      bool
	Fuel        bool
	ClearHeight float64 // robots at or below this height pass underneath (trench arms)
}

// Stats are cumulative counters since the last reset.
type Stats struct {
	Shots      [2]int `json:"shots"`
	Entered    [2]int `json:"entered"`
	OutOfField int    `json:"outOfField"`
	OutBy      [2]int `json:"outBy"` // launched out of the field, by the alliance that shot it
	Resets     int    `json:"resets"`
}

type World struct {
	layout     *Layout
	bus        *bus.Hub
	ballRadius float64

	mu              sync.Mutex
	robots          [6]*Robot
	balls           []*Ball
	hubs            [2]*Hub
	outposts        [2]*Outpost
	statics         []Static
	bumps           []AABB
	clearanceNeeded map[int]float64 // belly clearance to cross a bump, per module spacing (mm)
	grid            [][]int32
	stations        [6]StationLink
	field           FieldLink
	controllers     [6]Controller
	manualAt        [6]time.Time
	plans           [6]RobotPlan
	teamArch        map[int]Archetype
	teamSkill       map[int]float64
	teamDesigns     bool // teams build their own robots (design.go)
	tipping         bool // hard hits can tip tall robots over (an AI setting)
	// Between matches (fieldcrew.go): the crew has been called and the robots carted off since the last match, and
	// robots carted off on the field-safe signal wait for the next match load to come back on.
	crewCalled, cartedOff, awaitingMatch bool
	overrides                            [6]RobotPlan
	planNames                            [2]string
	time                                 float64
	steps                                int64
	paused                               bool
	stats                                Stats
	frame                                uint32
	statePath                            string
	saveTimer                            *time.Timer
	quiet                                bool                // no event-log output (AI benchmark)
	perfectInfo                          bool                // AI sees every ball instead of only what its cameras and drivers can see
	difficulty                           string              // AI difficulty preset
	personalities                        map[int]Personality // per-team overrides
	learning                             Learning
	coaches                              [2]*coach
	scoreboard                           [2]int                 // points the drive teams believe each alliance has (FUEL counted while scoring)
	matchClock                           func() (float64, bool) // true match time, when the FMS provides it
	navGrids                             map[navKey]*navGrid
	navScratch                           navScratch
	fuelCountsAt                         int64
	fuelCountsCache                      []int16
	teleopSeen                           bool // a teleop period is under way (for spotting the end of a real match)
	lastRemaining                        int
	rng                                  *rand.Rand // all world randomness (seedable for the AI benchmark); used under mu
	officials                            *officials
	crew                                 *fieldReset // the between-match field reset in progress, if any
	autoPreparedAt                       [2]float64  // world time each alliance last ran its pre-match auto selection
	autoChoices                          [6]*autoChoice
	truth                                MatchTruth // what really happened this match (truth.go)
	seed                                 int64      // this match's random seed
	replaySeed                           int64      // a seed to use for the next match (0 = random)

	subsMu sync.Mutex
	subs   map[chan []byte]struct{}
}

var stationNames = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

// New creates the world from the shared layout.
func New(layout *Layout, hub *bus.Hub) *World {
	world := &World{
		layout: layout, bus: hub, ballRadius: layout.Fuel.Diameter / 2, subs: map[chan []byte]struct{}{},
		teamArch: map[int]Archetype{}, teamSkill: map[int]float64{}, teamDesigns: true, tipping: true,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		difficulty: "regional", personalities: map[int]Personality{}, learning: newLearning(),
		coaches:   [2]*coach{newCoach(Red), newCoach(Blue)},
		officials: newOfficials(),
	}
	for i := range world.robots {
		alliance := Blue
		if i < 3 {
			alliance = Red
		}
		arch := Archetypes[0]
		if i%3 == 1 {
			arch = Archetypes[1]
		}
		world.robots[i] = &Robot{Index: i, Station: stationNames[i], Alliance: alliance, Arch: arch, Skill: 1}
	}
	world.hubs = [2]*Hub{newHub(layout, Red), newHub(layout, Blue)}
	world.outposts = [2]*Outpost{newOutpost(layout, Red), newOutpost(layout, Blue)}
	world.buildStatics()
	total := layout.Fuel.Total
	world.balls = make([]*Ball, total)
	for i := range world.balls {
		world.balls[i] = &Ball{}
	}
	world.resetLocked()
	world.registerCommands()
	world.registerAICommands()
	return world
}

// Attach wires the world to the driver stations and field I/O.
func (world *World) Attach(stations [6]StationLink, field FieldLink) {
	world.mu.Lock()
	world.stations = stations
	world.field = field
	world.mu.Unlock()
}

// SetController installs an AI driver for a station's robot.
func (world *World) SetController(index int, controller Controller) {
	world.mu.Lock()
	world.controllers[index] = controller
	world.mu.Unlock()
}

func (world *World) buildStatics() {
	l := world.layout
	for _, alliance := range []int{Red, Blue} {
		hub := world.hubs[alliance]
		half := l.Hub.Size / 2
		world.statics = append(world.statics, Static{
			Name: "hub", Robots: true, Fuel: true,
			Box: AABB{hub.Center.X - half, hub.Center.Y - half, hub.Center.X + half, hub.Center.Y + half, 0, l.Hub.RoofHeight},
		})
		// Trenches: a thin frame on the hub line. The legs block everything; the panel over the opening blocks robots
		// taller than the clearance and FUEL at its height. The opening runs from the guardrail inward (the outer leg
		// stands behind the rail).
		t := l.Trench
		hubX := l.Hub.Positions["blue"][0]
		leg := (t.Width - t.ClearWidth) / 2
		for _, center := range []float64{t.OpeningCenterY, l.Field.Width - t.OpeningCenterY} {
			open0, open1 := center-t.ClearWidth/2, center+t.ClearWidth/2
			for _, span := range [][2]float64{{open0 - leg, open0}, {open1, open1 + leg}} {
				x0, ya := l.onAlliance(alliance, hubX-t.Thickness/2, span[0])
				x1, yb := l.onAlliance(alliance, hubX+t.Thickness/2, span[1])
				world.statics = append(world.statics, Static{Name: "trench-leg", Robots: true, Fuel: true,
					Box: AABB{math.Min(x0, x1), math.Min(ya, yb), math.Max(x0, x1), math.Max(ya, yb), 0, t.Height}})
			}
			x0, ya := l.onAlliance(alliance, hubX-t.Thickness/2, open0-leg)
			x1, yb := l.onAlliance(alliance, hubX+t.Thickness/2, open1+leg)
			world.statics = append(world.statics, Static{Name: "trench-arm", Robots: true, Fuel: true, ClearHeight: t.ClearHeight,
				Box: AABB{math.Min(x0, x1), math.Min(ya, yb), math.Max(x0, x1), math.Max(ya, yb), t.ClearHeight, t.Height}})
		}
		// Bumps (traversable height field).
		b := l.Bump
		offset := l.Hub.Size/2 + b.Width/2
		for _, sign := range []float64{-1, 1} {
			centerY := l.Hub.Positions["blue"][1] + sign*offset
			x0, ya := l.onAlliance(alliance, hubX-b.Depth/2, centerY-b.Width/2)
			x1, yb := l.onAlliance(alliance, hubX+b.Depth/2, centerY+b.Width/2)
			world.bumps = append(world.bumps, AABB{math.Min(x0, x1), math.Min(ya, yb), math.Max(x0, x1), math.Max(ya, yb), 0, b.Height})
		}
		// Tower pocket: the space between the alliance wall and the uprights is too narrow for any robot (the uprights
		// are 32.25 in apart), so it is solid to robots; FUEL can still roll in.
		tw := l.Tower
		{
			pocketHalf := tw.UprightSpacing/2 + 0.06
			x0, ya := l.onAlliance(alliance, 0, tw.CenterY["blue"]-pocketHalf)
			x1, yb := l.onAlliance(alliance, tw.UprightSetback+tw.UprightDepth/2, tw.CenterY["blue"]+pocketHalf)
			// FUEL bounces off the tower base too, so the pocket does not become a sink no robot can empty.
			world.statics = append(world.statics, Static{Name: "tower-pocket", Robots: true, Fuel: true,
				Box: AABB{math.Min(x0, x1), math.Min(ya, yb), math.Max(x0, x1), math.Max(ya, yb), 0, tw.UprightHeight}})
		}
		// Tower uprights.
		for _, sign := range []float64{-1, 1} {
			y := tw.CenterY["blue"] + sign*(tw.UprightSpacing/2+0.02)
			x0, ya := l.onAlliance(alliance, tw.UprightSetback-tw.UprightDepth/2, y-0.02)
			x1, yb := l.onAlliance(alliance, tw.UprightSetback+tw.UprightDepth/2, y+0.02)
			world.statics = append(world.statics, Static{Name: "tower", Robots: true, Fuel: true,
				Box: AABB{math.Min(x0, x1), math.Min(ya, yb), math.Max(x0, x1), math.Max(ya, yb), 0, tw.UprightHeight}})
		}
	}
}

// startPose returns a station's robot starting position (bumpers on the starting line, facing the field).
func (world *World) startPose(index int) (Vec2, float64) {
	l := world.layout
	ys := []float64{6.55, 4.035, 1.55}
	x := l.Zones.AllianceZoneDepth - 0.5
	y := ys[index%3]
	alliance := Blue
	if index < 3 {
		alliance = Red
	}
	x, y = l.onAlliance(alliance, x, y)
	heading := 0.0
	if alliance == Red {
		heading = math.Pi
	}
	return Vec2{x, y}, heading
}

// Reset re-stages the field for a new match.
func (world *World) Reset() {
	world.mu.Lock()
	world.resetLocked()
	world.mu.Unlock()
	world.bus.Logf("world", "Field reset: FUEL staged and robots placed")
}

func (world *World) resetLocked() {
	world.resetMatchState()
	world.crew = nil
	for _, robot := range world.robots {
		robot.Cart, robot.cartT, robot.preloadOwed = CartNone, 0, false
	}
	targets := world.stagingTargets()
	for i, ball := range world.balls {
		*ball = Ball{}
		target := targets[i]
		ball.State, ball.Owner, ball.P = target.state, target.owner, target.p
		if ball.State == BallHeld {
			robot := world.robots[ball.Owner]
			robot.loadHopper(ball, robot.Hopper) // preloaded: already in the hopper
			robot.Hopper++
		}
		if ball.State == BallFree {
			ball.sleep = sleepFrames
		}
	}
}

// resetMatchState clears everything about the last match except where the FUEL is.
func (world *World) resetMatchState() {
	world.planAlliance(Red)
	world.planAlliance(Blue)
	for _, robot := range world.robots {
		robot.Pos, robot.Heading = world.startPoseFor(robot)
		robot.Vel, robot.Omega = Vec2{}, 0
		robot.Z, robot.Pitch, robot.Roll = 0, 0, 0
		robot.Hopper, robot.ClimbLevel, robot.ClimbProg, robot.Climbing = 0, 0, 0, false
		robot.Shots, robot.Collected, robot.Scored, robot.Passes, robot.AutoScored = 0, 0, 0, 0, 0
		robot.Odo.Est, robot.Odo.EstHeading = robot.Pos, robot.Heading
		robot.AutoName = ""
		robot.Faults = Faults{}
		robot.Activity = Activity{}
		robot.AutoTower = 0
	}
	for _, hub := range world.hubs {
		hub.Stored, hub.Sensed, hub.exitTimers, hub.beamBlocked = 0, [4]int{}, [4]float64{}, [4]bool{}
	}
	for _, outpost := range world.outposts {
		outpost.Corral, outpost.DoorOpen = 0, false
		outpost.Throws, outpost.Made, outpost.holdOpenUntil, outpost.robotComing = 0, 0, 0, -100
	}
	world.stats = Stats{Resets: world.stats.Resets + 1}
	world.scoreboard = [2]int{}
	for _, c := range world.coaches {
		c.reset()
	}
}

// ballTarget is where one FUEL is staged for the start of a match.
type ballTarget struct {
	state int
	owner int
	p     [3]float64
}

// stagingTargets lays out all FUEL for a match start (Game Manual 6.3.4): up to 8 preloaded per robot, 24 in each
// chute, 24 in each depot, and the rest in the neutral-zone box. Index i is FUEL i.
func (world *World) stagingTargets() []ballTarget {
	l := world.layout
	r := world.ballRadius
	targets := make([]ballTarget, 0, len(world.balls))
	for _, robot := range world.robots {
		if !robot.Present {
			continue
		}
		for k := 0; k < l.Fuel.PreloadPerRobot; k++ {
			targets = append(targets, ballTarget{state: BallHeld, owner: robot.Index})
		}
	}
	for _, outpost := range world.outposts {
		for k := 0; k < l.Outpost.ChuteCapacity; k++ {
			targets = append(targets, ballTarget{state: BallChute, owner: outpost.Alliance})
		}
	}
	// Depots: 24 each in a 4 x 6 grid against the wall.
	for _, alliance := range []int{Red, Blue} {
		for i := 0; i < 4; i++ {
			for j := 0; j < 6; j++ {
				x := 0.09 + float64(i)*0.165
				y := l.Depot.CenterY["blue"] - l.Depot.Width/2 + 0.09 + float64(j)*0.177
				x, y = l.onAlliance(alliance, x, y)
				targets = append(targets, ballTarget{state: BallFree, p: [3]float64{x, y, r}})
			}
		}
	}
	// Everything else goes in the neutral zone box.
	remaining := len(world.balls) - len(targets)
	box := l.Fuel.NeutralBox
	columns := 12
	rows := int(math.Ceil(float64(remaining) / float64(columns)))
	for k := 0; k < remaining; k++ {
		i := k % columns
		j := k / columns
		x := box.CenterX - box.LengthX/2 + (float64(i)+0.5)*(box.LengthX/float64(columns))
		y := box.CenterY - box.WidthY/2 + (float64(j)+0.5)*(box.WidthY/float64(rows))
		targets = append(targets, ballTarget{state: BallFree, p: [3]float64{x, y, r}})
	}
	return targets
}

// Run starts the fixed-rate simulation loop.
func (world *World) Run() {
	// tick recovers its own panics; safe.Go restarts the loop if broadcasting or publishing ever panics.
	safe.Go(world.bus, "world", func() {
		period := time.Second / stepHz
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		dt := 1.0 / stepHz
		for range ticker.C {
			frame, state, ai := world.tick(dt)
			if frame != nil {
				world.broadcast(frame)
			}
			if state != nil {
				world.bus.Publish("world.state", state)
			}
			if ai != nil {
				world.bus.Publish("world.ai", ai)
			}
		}
	})
}

// tick advances the world one step. A panic is recovered (after the lock is released) so a physics or AI bug costs
// one frame rather than the whole simulator.
func (world *World) tick(dt float64) (frame []byte, state any, ai any) {
	world.mu.Lock()
	defer world.mu.Unlock()
	defer safe.Recover(world.bus, "world")
	world.steps++
	if !world.paused {
		world.step(dt)
	}
	if world.steps%snapshotEvery == 0 {
		frame = world.encodeSnapshot()
	}
	if world.steps%stateEvery == 0 {
		state = world.stateSummary()
	}
	if world.steps%aiEvery == 0 {
		ai = world.aiSummary()
	}
	return frame, state, ai
}

// updateController runs one AI driver; a panic disables just that robot's decisions for this tick.
func (world *World) updateController(i int, robot *Robot, dt float64) {
	defer safe.Recover(world.bus, "ai")
	world.controllers[i].Update(robot, world, dt)
}

func (world *World) step(dt float64) {
	world.time += dt
	now := time.Now()
	// Sync robots with their driver stations: only what the DS reports reaches the robot.
	for i, robot := range world.robots {
		link := world.stations[i]
		if link == nil {
			continue
		}
		command := link.Command()
		present := command.Team > 0
		if present != robot.Present {
			robot.Present = present
			if present {
				robot.Pos, robot.Heading = world.startPoseFor(robot)
				if world.crew != nil {
					world.startCart(robot, CartIn) // arriving during a field reset: wheeled in on its cart
				}
			}
		}
		switch {
		case command.Team == robot.Team || command.Team == 0:
		case robot.Cart == CartOut:
			robot.pendingTeam = command.Team // the old robot is still being wheeled off
		default:
			world.assignTeam(robot, command.Team)
		}
		if command.Team == 0 {
			robot.Team = 0
		}
		robot.Enabled = command.Enabled && command.RobotLinked
		robot.Auto = command.Auto
		robot.Brownout = command.Brownout
		robot.Voltage = command.Voltage
		robot.GameData = command.GameData
		robot.MatchType = command.MatchType
		robot.Remaining = command.RemainingSeconds
		robot.FmsLinked = command.FmsLinked
		if robot.Manual && now.Sub(world.manualAt[i]) > manualTimeout {
			robot.Ctl.Vel, robot.Ctl.Omega = Vec2{}, 0
		}
	}
	// Hub motors and lights come from the PLC coils.
	if world.field != nil {
		redMotor, blueMotor := world.field.HubMotorsOn()
		redLight, blueLight := world.field.HubLightsOn()
		world.hubs[Red].MotorOn, world.hubs[Blue].MotorOn = redMotor, blueMotor
		world.hubs[Red].LightOn, world.hubs[Blue].LightOn = redLight, blueLight
	}
	if world.steps%agentEvery == 0 {
		for i, robot := range world.robots {
			if robot.onField() && !robot.Manual && world.controllers[i] != nil {
				world.updateController(i, robot, dt*agentEvery)
			}
		}
		world.runCoaches()
		world.watchMatchEnd()
		world.watchTruth()
	}
	for _, robot := range world.robots {
		if robot.onField() {
			world.stepRobot(robot, dt)
		}
	}
	world.collideRobots()
	for _, robot := range world.robots {
		if robot.onField() {
			world.collideRobotStatics(robot)
		}
	}
	world.interactRobotsBalls()
	world.stepFieldReset(dt)
	world.stepIntakes(dt)
	world.stepHoppers(dt)
	world.stepBalls(dt)
	world.stepOffField(dt)
	world.collideBalls()
	for _, hub := range world.hubs {
		hub.step(world, dt)
	}
	for _, outpost := range world.outposts {
		outpost.step(world, dt)
	}
	if world.steps%officialsEvery == 0 {
		world.runOfficials(dt * officialsEvery)
		if world.steps%(officialsEvery*5) == 0 {
			world.fieldStaff()
		}
	}
	for i, robot := range world.robots {
		if world.stations[i] != nil && robot.Present {
			world.stations[i].SetLoad(robot.Load)
		}
		world.trackActivity(robot, dt)
	}
}

// RobotSummary is one robot's published state.
type RobotSummary struct {
	Station    string    `json:"station"`
	Team       int       `json:"team"`
	Present    bool      `json:"present"`
	Archetype  string    `json:"archetype"`
	Model      ModelSpec `json:"model"`
	X          float64   `json:"x"`
	Y          float64   `json:"y"`
	Heading    float64   `json:"heading"`
	Speed      float64   `json:"speed"`
	Enabled    bool      `json:"enabled"`
	Manual     bool      `json:"manual"`
	Hopper     int       `json:"hopper"`
	Capacity   int       `json:"capacity"`
	ClimbLevel int       `json:"climbLevel"`
	Climbing   bool      `json:"climbing"`
	Shots      int       `json:"shots"`
	Collected  int       `json:"collected"`
	AiState    string    `json:"aiState"`
	Load       float64   `json:"load"`
	Skill      float64   `json:"skill"`
	Scored     int       `json:"scored"`
	Passes     int       `json:"passes"`
	Plan       RobotPlan `json:"plan"`
	Override   RobotPlan `json:"override"`
	Faults     []string  `json:"faults"`
	Activity   Activity  `json:"activity"`
	AutoName   string    `json:"autoName"`
	OdoError   float64   `json:"odoError"`
}

// Summary is the published world state (10 Hz).
type Summary struct {
	Time      float64        `json:"time"`
	Paused    bool           `json:"paused"`
	Robots    []RobotSummary `json:"robots"`
	Hubs      [2]HubSummary  `json:"hubs"`
	Outposts  [2]OutpostInfo `json:"outposts"`
	Fuel      map[string]int `json:"fuel"`
	Stats     Stats          `json:"stats"`
	Plans     [2]string      `json:"plans"`
	Officials OfficialsState `json:"officials"`
	Seed      int64          `json:"seed"` // this match's random seed (enter it to replay the AI's match)
}

type HubSummary struct {
	Stored  int    `json:"stored"`
	Sensed  [4]int `json:"sensed"`
	MotorOn bool   `json:"motorOn"`
	LightOn bool   `json:"lightOn"`
}

type OutpostInfo struct {
	Chute     int    `json:"chute"`
	Corral    int    `json:"corral"`
	DoorOpen  bool   `json:"doorOpen"`
	HumanMode string `json:"humanMode"`
}

func (world *World) stateSummary() Summary {
	summary := Summary{Time: world.time, Paused: world.paused, Stats: world.stats, Fuel: map[string]int{},
		Plans: world.planNames, Officials: world.officialsState(), Seed: world.seed}
	for _, robot := range world.robots {
		summary.Robots = append(summary.Robots, RobotSummary{
			Station: robot.Station, Team: robot.Team, Present: robot.Present, Archetype: robot.Arch.Name, Model: robot.Arch.Model,
			X: robot.Pos.X, Y: robot.Pos.Y, Heading: robot.Heading, Speed: robot.Vel.Len(), Enabled: robot.Enabled,
			Manual: robot.Manual, Hopper: robot.Hopper, Capacity: robot.Arch.Capacity, ClimbLevel: robot.ClimbLevel,
			Climbing: robot.Climbing, Shots: robot.Shots, Collected: robot.Collected, AiState: robot.AiState,
			Load: robot.Load, Skill: robot.Skill, Scored: robot.Scored, Passes: robot.Passes,
			Plan: world.plans[robot.Index], Override: world.overrides[robot.Index], Faults: robot.faultActive(world.time),
			Activity: robot.Activity, AutoName: robot.AutoName, OdoError: round2(robot.odoError()),
		})
	}
	for i, hub := range world.hubs {
		summary.Hubs[i] = HubSummary{Stored: hub.Stored, Sensed: hub.Sensed, MotorOn: hub.MotorOn, LightOn: hub.LightOn}
	}
	for i, outpost := range world.outposts {
		summary.Outposts[i] = OutpostInfo{Chute: outpost.chuteCount(world), Corral: outpost.Corral,
			DoorOpen: outpost.DoorOpen, HumanMode: outpost.HumanMode}
	}
	names := []string{"field", "held", "hub", "chute", "corral", "out", "crew"}
	for _, ball := range world.balls {
		summary.Fuel[names[ball.State]]++
	}
	return summary
}

// Steps returns how many physics ticks have run (used by the stall watchdog).
func (world *World) Steps() int64 {
	world.mu.Lock()
	defer world.mu.Unlock()
	return world.steps
}

// Time returns the simulation clock.
func (world *World) Time() float64 { return world.time }

// WithRobot runs fn with the world locked (for fault injection and referees).
func (world *World) WithRobot(station string, fn func(robot *Robot, world *World)) bool {
	world.mu.Lock()
	defer world.mu.Unlock()
	robot := world.Robot(station)
	if robot == nil {
		return false
	}
	fn(robot, world)
	return true
}

// ClimbLevels returns each station's current tower level (what a referee would see).
func (world *World) ClimbLevels() [6]int {
	world.mu.Lock()
	defer world.mu.Unlock()
	var levels [6]int
	for i, robot := range world.robots {
		if robot.Present && !robot.Climbing {
			levels[i] = robot.ClimbLevel
		} else if robot.Present && robot.Climbing {
			levels[i] = robot.ClimbLevel // only completed levels count
		}
	}
	return levels
}

// RobotPose returns a station's robot position (for the RF model).
func (world *World) RobotPose(index int) (float64, float64, bool) {
	world.mu.Lock()
	defer world.mu.Unlock()
	robot := world.robots[index]
	return robot.Pos.X, robot.Pos.Y, robot.Enabled
}

// FieldStats returns the cumulative world counters.
func (world *World) FieldStats() Stats {
	world.mu.Lock()
	defer world.mu.Unlock()
	return world.stats
}

// RobotStats returns per-station shots/scored/passes for reports.
func (world *World) RobotStats() []RobotSummary {
	world.mu.Lock()
	defer world.mu.Unlock()
	return world.stateSummary().Robots
}

// Robot returns a robot by station name (for controllers and tests).
func (world *World) Robot(station string) *Robot {
	for _, robot := range world.robots {
		if robot.Station == station {
			return robot
		}
	}
	return nil
}

// Layout exposes the field layout.
func (world *World) Layout() *Layout { return world.layout }

// Hubs exposes the hubs (read-only use by controllers inside the world lock).
func (world *World) Hub(alliance int) *Hub { return world.hubs[alliance] }

// Robots exposes the robots (read-only use by controllers inside the world lock).
func (world *World) Robots() [6]*Robot { return world.robots }

// FreeBalls calls fn for each free ball on the floor (controllers use this to find FUEL).
func (world *World) FreeBalls(fn func(p Vec2, z float64)) {
	for _, ball := range world.balls {
		if ball.State == BallFree && ball.P[2] < world.ballRadius*2.5 {
			fn(Vec2{ball.P[0], ball.P[1]}, ball.P[2])
		}
	}
}

// Reachable reports whether a robot of the given height could get its intake to a floor point: points inside (or
// pressed against) any element the robot cannot enter are unreachable.
func (world *World) Reachable(p Vec2, robotHeight float64) bool {
	for _, static := range world.statics {
		if !static.Robots || (static.ClearHeight > 0 && robotHeight <= static.ClearHeight) {
			continue
		}
		box := static.Box
		margin := 0.06
		if p.X > box.MinX-margin && p.X < box.MaxX+margin && p.Y > box.MinY-margin && p.Y < box.MaxY+margin {
			return false
		}
	}
	return true
}

// Outpost exposes an alliance outpost.
func (world *World) Outpost(alliance int) *Outpost { return world.outposts[alliance] }

// ClimbZone exposes the tower climb position for an alliance and slot.
func (world *World) ClimbZone(alliance, slot int) (Vec2, float64) {
	return world.climbZone(alliance, slot)
}

// logf writes to the event log unless the world is running quietly (the AI benchmark).
func (world *World) logf(level, source, format string, args ...any) {
	if world.quiet || world.bus == nil {
		return
	}
	world.bus.Emit(level, source, fmt.Sprintf(format, args...))
}
