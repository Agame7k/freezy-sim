package world

import (
	"sort"
)

// Teleop strategies.
const (
	StratScorer  = "scorer"  // collect and shoot into the hub, stockpile while the hub is inactive
	StratShuttle = "shuttle" // collect in the neutral zone and lob FUEL into the alliance zone for teammates
	StratReverse = "reverse" // reverse shuttle: pull FUEL off the opponent's side of the neutral zone toward ours
	StratDefense = "defense" // guard opponent scorers while their hub is active, deny FUEL otherwise
	StratFeeder  = "feeder"  // work the outpost chute with the human player, then score
	StratSteal   = "steal"   // raid the opponent alliance zone for their stockpiled FUEL and bring it home
)

// What a robot does while its own hub is inactive.
const (
	InactiveHold    = "hold"    // fill up, then wait at the shooting spot
	InactiveDefend  = "defend"  // play defense on the opponent (whose hub is active)
	InactiveSteal   = "steal"   // raid the opponent zone for FUEL
	InactiveShuttle = "shuttle" // stockpile: lob neutral-zone FUEL into our own zone
)

var InactiveRoles = []string{InactiveHold, InactiveDefend, InactiveSteal, InactiveShuttle}

// Auto routines.
const (
	AutoPreload = "preload" // shoot the preload, then pick up nearby FUEL
	AutoCenter  = "center"  // shoot the preload, rush the neutral zone, come back and shoot
	AutoDepot   = "depot"   // shoot the preload, empty the depot, shoot
	AutoOutpost = "outpost" // shoot the preload, take the outpost chute, shoot
	AutoClimb   = "climb"   // shoot the preload, then climb tower level 1
	AutoShuttle = "shuttle" // rush the neutral zone and lob FUEL home for teleop
)

// Starting spots along the alliance starting line (blue-frame y).
var startSpots = map[string]float64{
	"outpost-trench": 0.85, "outpost-bump": 2.45, "hub": 4.035, "depot-bump": 5.6, "depot-trench": 7.2,
}

var Strategies = []string{StratScorer, StratShuttle, StratReverse, StratDefense, StratFeeder, StratSteal}
var AutoRoutines = []string{AutoPreload, AutoCenter, AutoDepot, AutoOutpost, AutoClimb, AutoShuttle}
var StartSpots = []string{"outpost-trench", "outpost-bump", "hub", "depot-bump", "depot-trench"}

// RobotPlan is one robot's role in its alliance's game plan.
type RobotPlan struct {
	Strategy string `json:"strategy"`
	Auto     string `json:"auto"`
	Start    string `json:"start"`
	Inactive string `json:"inactive"`
}

type planTemplate struct {
	name  string
	slots [3]RobotPlan
}

var planTemplates = []planTemplate{
	{"Balanced", [3]RobotPlan{{StratScorer, AutoCenter, "", InactiveDefend}, {StratScorer, AutoDepot, "", InactiveHold},
		{StratShuttle, AutoClimb, "", ""}}},
	{"Triple scorer", [3]RobotPlan{{StratScorer, AutoCenter, "", InactiveShuttle}, {StratScorer, AutoOutpost, "", InactiveHold},
		{StratScorer, AutoDepot, "", InactiveSteal}}},
	{"Shuttle line", [3]RobotPlan{{StratShuttle, AutoShuttle, "", ""}, {StratScorer, AutoPreload, "", InactiveHold},
		{StratScorer, AutoDepot, "", InactiveDefend}}},
	{"Lockdown defense", [3]RobotPlan{{StratScorer, AutoCenter, "", InactiveHold}, {StratScorer, AutoDepot, "", InactiveDefend},
		{StratDefense, AutoPreload, "", ""}}},
	{"Deny", [3]RobotPlan{{StratReverse, AutoCenter, "", ""}, {StratScorer, AutoDepot, "", InactiveSteal},
		{StratScorer, AutoClimb, "", InactiveHold}}},
	{"Outpost feed", [3]RobotPlan{{StratFeeder, AutoOutpost, "", InactiveHold}, {StratScorer, AutoCenter, "", InactiveDefend},
		{StratShuttle, AutoPreload, "", ""}}},
	{"Double shuttle", [3]RobotPlan{{StratShuttle, AutoShuttle, "", ""}, {StratReverse, AutoCenter, "", ""},
		{StratScorer, AutoDepot, "", InactiveHold}}},
	{"Raiders", [3]RobotPlan{{StratSteal, AutoCenter, "", ""}, {StratScorer, AutoDepot, "", InactiveSteal},
		{StratScorer, AutoClimb, "", InactiveDefend}}},
}

// suitability scores how well a robot fits a plan slot (higher is better).
func suitability(robot *Robot, plan RobotPlan, layout *Layout) float64 {
	score := 0.0
	low := robot.Arch.Height <= layout.Trench.ClearHeight
	switch robot.Arch.Name {
	case "shuttle":
		if plan.Strategy == StratShuttle || plan.Strategy == StratReverse {
			score += 4
		}
	case "bigdumper", "volley":
		if plan.Strategy == StratScorer || plan.Strategy == StratFeeder {
			score += 2
		}
	case "turret":
		if plan.Strategy == StratScorer {
			score += 1.5
		}
	case "kitbot":
		if plan.Auto == AutoPreload || plan.Auto == AutoClimb {
			score += 1.5
		}
	}
	switch plan.Strategy {
	case StratDefense, StratSteal:
		if robot.Arch.Name == "defender" {
			score += 5
		}
		if plan.Strategy == StratSteal && robot.Arch.Capacity >= 50 {
			score += 1
		}
	case StratShuttle, StratReverse:
		if low {
			score += 1
		}
	case StratScorer, StratFeeder:
		if robot.Arch.Name == "defender" {
			score -= 3
		}
		if robot.Arch.Name == "tall" {
			score += 1
		}
	}
	switch plan.Auto {
	case AutoCenter, AutoShuttle:
		if low {
			score += 1.5 // can take the trench
		}
	case AutoClimb:
		if robot.Arch.ClimbLevel < 1 {
			score -= 5
		}
	}
	return score
}

// chooseStart picks a starting spot that suits the auto routine.
func chooseStart(robot *Robot, plan RobotPlan, layout *Layout, used map[string]bool) string {
	low := robot.Arch.Height <= layout.Trench.ClearHeight
	var preferences []string
	switch plan.Auto {
	case AutoDepot:
		preferences = []string{"depot-bump", "depot-trench", "hub"}
	case AutoOutpost:
		preferences = []string{"outpost-bump", "outpost-trench", "hub"}
	case AutoCenter, AutoShuttle:
		if low {
			preferences = []string{"outpost-trench", "depot-trench", "outpost-bump", "depot-bump"}
		} else {
			preferences = []string{"outpost-bump", "depot-bump", "hub"}
		}
	case AutoClimb:
		preferences = []string{"hub", "depot-bump", "outpost-bump"}
	default:
		preferences = []string{"hub", "outpost-bump", "depot-bump"}
	}
	for _, spot := range append(preferences, StartSpots...) {
		if !used[spot] {
			used[spot] = true
			return spot
		}
	}
	return "hub"
}

// planAlliance picks a game plan for one alliance and assigns roles and starting spots to its robots.
func (world *World) planAlliance(alliance int) {
	var members []*Robot
	for _, robot := range world.robots {
		if robot.Alliance == alliance {
			members = append(members, robot)
		}
	}
	template := world.choosePlan(alliance)
	world.planNames[alliance] = template.name

	// Assign slots greedily to the best-suited robots.
	type option struct {
		robot *Robot
		slot  int
		score float64
	}
	// In practice the alliance hardly coordinates: roles go out almost at random.
	noise := 0.3
	if world.practiceMatch() {
		noise = 1.5
	}
	var options []option
	for _, robot := range members {
		for slot, plan := range template.slots {
			options = append(options, option{robot, slot, suitability(robot, plan, world.layout) + world.rng.Float64()*noise})
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i].score > options[j].score })
	assigned := map[*Robot]bool{}
	taken := map[int]bool{}
	for _, o := range options {
		if assigned[o.robot] || taken[o.slot] {
			continue
		}
		assigned[o.robot] = true
		taken[o.slot] = true
		world.plans[o.robot.Index] = template.slots[o.slot]
	}
	// ...and teams try a strategy of their own now and then (not defense: that waits for qualifications).
	if world.practiceMatch() {
		tryable := []string{StratScorer, StratShuttle, StratReverse, StratFeeder}
		for _, robot := range members {
			if world.rng.Float64() < 0.35 {
				world.plans[robot.Index].Strategy = tryable[world.rng.Intn(len(tryable))]
			}
		}
	}
	// Apply per-robot overrides, then choose starting spots (overridden spots first).
	used := map[string]bool{}
	for _, robot := range members {
		override := world.overrides[robot.Index]
		plan := &world.plans[robot.Index]
		if override.Strategy != "" {
			plan.Strategy = override.Strategy
		}
		if override.Auto != "" {
			plan.Auto = override.Auto
		}
		if override.Inactive != "" {
			plan.Inactive = override.Inactive
		}
		if plan.Inactive == "" {
			plan.Inactive = InactiveHold
		}
		if override.Start != "" {
			plan.Start = override.Start
			used[plan.Start] = true
		} else {
			plan.Start = ""
		}
	}
	for _, robot := range members {
		plan := &world.plans[robot.Index]
		if plan.Start == "" {
			plan.Start = chooseStart(robot, *plan, world.layout, used)
		}
	}
}

// startPoseFor returns the starting pose for a robot's planned spot.
func (world *World) startPoseFor(robot *Robot) (Vec2, float64) {
	l := world.layout
	y, ok := startSpots[world.plans[robot.Index].Start]
	if !ok {
		return world.startPose(robot.Index)
	}
	x := l.Zones.AllianceZoneDepth - robot.half().X - 0.06
	x, y = l.onAlliance(robot.Alliance, x, y)
	heading := 0.0
	if robot.Alliance == Red {
		heading = 3.141592653589793
	}
	return Vec2{x, y}, heading
}
