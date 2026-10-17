package world

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Experiments, mistakes and memory. Real teams try things — a different auto, a longer shot, defense while their hub
// is off, a higher climb — keep what works and drop what does not. They also make mistakes: drivers high-center on a
// bump, get tipped by a hard hit, misread the game data, rack up pin fouls; mechanisms jam. And they remember: a team
// that got stuck on the same bump twice drives around it, a team with pin fouls plays more carefully, a climb that
// keeps failing gets scaled back, the pit crew fixes what broke. Memories fade, so a team that fixed its problem slowly
// regains confidence.

const (
	incidentCell      = 0.5  // metres: incidents are remembered at this resolution
	incidentDecay     = 0.8  // per match
	incidentForget    = 0.25 // below this a memory is gone
	incidentsKept     = 24   // per team
	reliabilityRepair = 0.015
	maxRepairs        = 0.2
)

// Incident is something that went wrong for a team, where (blue frame; X = Y = 0 for anywhere), and how strongly it is
// remembered.
type Incident struct {
	Kind  string  `json:"kind"`
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Count float64 `json:"count"`
	Last  int     `json:"last"` // the team's match count when it last happened
}

// Choice categories a team experiments with.
const (
	ChoiceAuto     = "auto"     // which auto routine (from the spot it stands on)
	ChoiceRange    = "range"    // shooting distance from the hub
	ChoiceInactive = "inactive" // what to do while its hub is off
	ChoiceClimb    = "climb"    // tower climb success by level (a record, not a free choice)
)

var shootRanges = []float64{2.3, 2.7, 3.1}

// noteIncident remembers a mistake for the robot's team (at the robot's position when located).
func (world *World) noteIncident(robot *Robot, kind string, located bool) {
	if robot.Team <= 0 {
		return
	}
	stats := world.teamStats(robot.Team)
	x, y := 0.0, 0.0
	if located {
		bx, by := world.layout.onAlliance(robot.Alliance, robot.Pos.X, robot.Pos.Y) // (the rotation is its own inverse)
		x = (math.Floor(bx/incidentCell) + 0.5) * incidentCell
		y = (math.Floor(by/incidentCell) + 0.5) * incidentCell
	}
	for _, incident := range stats.Incidents {
		if incident.Kind == kind && math.Abs(incident.X-x) < 1e-6 && math.Abs(incident.Y-y) < 1e-6 {
			incident.Count++
			incident.Last = stats.Matches
			world.markDirtyLocked()
			return
		}
	}
	stats.Incidents = append(stats.Incidents, &Incident{Kind: kind, X: x, Y: y, Count: 1, Last: stats.Matches})
	if len(stats.Incidents) > incidentsKept {
		sort.Slice(stats.Incidents, func(i, j int) bool { return stats.Incidents[i].Count > stats.Incidents[j].Count })
		stats.Incidents = stats.Incidents[:incidentsKept]
	}
	world.markDirtyLocked()
}

// fadeMemories ages a team's incidents after a match.
func fadeMemories(stats *TeamStats) {
	kept := stats.Incidents[:0]
	for _, incident := range stats.Incidents {
		incident.Count *= incidentDecay
		if incident.Count >= incidentForget {
			kept = append(kept, incident)
		}
	}
	stats.Incidents = kept
}

// incidentWeight sums a team's remembered incidents whose kind starts with prefix.
func (stats *TeamStats) incidentWeight(prefix string) float64 {
	total := 0.0
	for _, incident := range stats.Incidents {
		if strings.HasPrefix(incident.Kind, prefix) {
			total += incident.Count
		}
	}
	return total
}

// rememberedHazards are nav cells a drive team now avoids (where it got stuck, high-centered or tipped), with a cost.
func (world *World) rememberedHazards(robot *Robot) map[[2]int]float64 {
	stats, ok := world.learning.Teams[robot.Team]
	if !ok {
		return nil
	}
	grid := world.navGridFor(robot)
	cells := map[[2]int]float64{}
	for _, incident := range stats.Incidents {
		if incident.Count < 0.5 || (incident.X == 0 && incident.Y == 0) {
			continue
		}
		if incident.Kind != "high-centered" && incident.Kind != "stuck" && incident.Kind != "tipped" {
			continue
		}
		x, y := world.layout.onAlliance(robot.Alliance, incident.X, incident.Y)
		cx, cy := grid.cellOf(Vec2{x, y})
		reach := int(math.Ceil(0.45 / navCell))
		cost := 6 * math.Min(3, incident.Count)
		for dy := -reach; dy <= reach; dy++ {
			for dx := -reach; dx <= reach; dx++ {
				key := [2]int{cx + dx, cy + dy}
				cells[key] = math.Max(cells[key], cost)
			}
		}
	}
	return cells
}

// ---- Experiments -------------------------------------------------------------------------------------------------------

func (stats *TeamStats) choice(category, option string) *PlanRecord {
	if stats.Choices == nil {
		stats.Choices = map[string]*PlanRecord{}
	}
	key := category + ":" + option
	record := stats.Choices[key]
	if record == nil {
		record = &PlanRecord{}
		stats.Choices[key] = record
	}
	return record
}

// pickChoice picks an option for a category: usually by Thompson sampling over the team's record (prior gives the
// favoured option a head start), but a curious team that has not tried something much yet sometimes just tries it.
// It returns the option and whether it is an experiment (something the team has hardly tried; a choice that has
// earned its place is not).
func (world *World) pickChoice(stats *TeamStats, category string, options []string, favoured string, curiosity float64,
	rng func() float64) (string, bool) {
	if len(options) == 0 {
		return favoured, false
	}
	tries := 0.0
	least, leastTries := options[0], math.Inf(1)
	for _, option := range options {
		record := stats.choice(category, option)
		n := record.Wins + record.Losses
		tries += n
		if n < leastTries {
			least, leastTries = option, n
		}
	}
	if rng() < curiosity*0.2/(1+tries/8) && least != favoured {
		return least, true
	}
	best, bestSample := favoured, -1.0
	for _, option := range options {
		record := stats.choice(category, option)
		// Untried options start sceptical (a team sticks with its plan until something else proves itself).
		wins, losses := record.Wins+1, record.Losses+2
		if option == favoured {
			wins += 3
		}
		if sample := world.betaSample(wins, losses); sample > bestSample {
			best, bestSample = option, sample
		}
	}
	record := stats.choice(category, best)
	return best, best != favoured && record.Wins+record.Losses < 2
}

// creditChoice scores one match's outcome (0..1) for a choice.
func (stats *TeamStats) creditChoice(category, option string, reward float64) {
	if option == "" {
		return
	}
	record := stats.choice(category, option)
	reward = clamp(reward, 0, 1)
	record.Wins += reward
	record.Losses += 1 - reward
	// Old results matter less than recent ones (teams keep changing their robots).
	if total := record.Wins + record.Losses; total > 20 {
		record.Wins *= 20 / total
		record.Losses *= 20 / total
	}
}

// chooseExperiments sets this match's team choices: the shooting distance and the inactive-hub role (the auto routine
// is chosen at the DS chooser, autoprog.go).
func (agent *Agent) chooseExperiments(robot *Robot, world *World) {
	agent.choices = map[string]string{}
	agent.Experiment = ""
	if robot.Team <= 0 {
		return
	}
	stats := world.teamStats(robot.Team)
	curiosity := practiceCuriosity(robot, agent.Personality.Curiosity)
	rng := agent.rng.Float64
	var ranges []string
	for _, r := range shootRanges {
		if r <= robot.Arch.MaxRange*0.85 {
			ranges = append(ranges, fmt.Sprintf("%.1f", r))
		}
	}
	favoured := fmt.Sprintf("%.1f", shootRanges[1])
	if len(ranges) > 0 && !contains(ranges, favoured) {
		favoured = ranges[len(ranges)-1]
	}
	pick, tried := world.pickChoice(stats, ChoiceRange, ranges, favoured, curiosity, rng)
	agent.choices[ChoiceRange] = pick
	fmt.Sscanf(pick, "%g", &agent.shootDistance)
	var notes []string
	if tried {
		notes = append(notes, "shooting from "+pick+" m")
	}
	plan := &world.plans[robot.Index]
	if plan.Strategy == StratScorer && world.overrides[robot.Index].Inactive == "" {
		options := InactiveRoles
		if robot.Arch.Class == "kitbot" {
			options = []string{InactiveHold, InactiveShuttle}
		}
		role, tried := world.pickChoice(stats, ChoiceInactive, options, plan.Inactive, curiosity, rng)
		agent.choices[ChoiceInactive] = role
		if role != plan.Inactive {
			plan.Inactive = role
		}
		if tried {
			notes = append(notes, "trying "+role+" while the hub is off")
		}
	}
	agent.Experiment = strings.Join(notes, "; ")
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// creditExperiments learns from one robot's match.
func (world *World) creditExperiments(r BenchRobot, allianceResult float64) {
	stats := world.teamStats(r.Team)
	if option := r.Choices[ChoiceRange]; option != "" && r.Shots >= 10 {
		stats.creditChoice(ChoiceRange, option, float64(r.Scored)/float64(r.Shots))
	}
	if option := r.Choices[ChoiceInactive]; option != "" {
		stats.creditChoice(ChoiceInactive, option, allianceResult)
	}
	if r.Auto != nil && r.Auto.Routine != "" && r.Auto.Mistake == "" {
		stats.creditChoice(ChoiceAuto, r.Auto.Routine, r.Auto.value()/45)
	}
	if r.ClimbGoal > 0 {
		reached := 0.0
		if r.ClimbLevel >= r.ClimbGoal {
			reached = 1
		}
		stats.creditChoice(ChoiceClimb, fmt.Sprintf("L%d", r.ClimbGoal), reached)
		if reached == 0 {
			robotIncident := fmt.Sprintf("climb failed L%d", r.ClimbGoal)
			found := false
			for _, incident := range stats.Incidents {
				if incident.Kind == robotIncident {
					incident.Count++
					found = true
				}
			}
			if !found {
				stats.Incidents = append(stats.Incidents, &Incident{Kind: robotIncident, Count: 1, Last: stats.Matches})
			}
		}
	}
}

// climbReliability is how often a team's climbs to a level succeed, from its record (with a prior).
func (world *World) climbReliability(team int, level int, prior float64) float64 {
	stats, ok := world.learning.Teams[team]
	if !ok {
		return prior
	}
	record, ok := stats.Choices[ChoiceClimb+":"+fmt.Sprintf("L%d", level)]
	if !ok {
		return prior
	}
	return (record.Wins + prior*4) / (record.Wins + record.Losses + 4)
}

// ---- Reliability and natural mistakes ----------------------------------------------------------------------------------

// teamReliability is how robust a team's robot is (0.6..0.99): its build quality plus what the pit crew has fixed.
func (world *World) teamReliability(team int) float64 {
	base := 0.65 + 0.3*hashUnit(team, 0x7e11)
	if stats, ok := world.learning.Teams[team]; ok {
		base += stats.Repairs
	}
	return clamp(base, 0.6, 0.99)
}

// naturalMistakes rolls for the mistakes a drive team and its robot make on their own (called each teleop decision).
func (agent *Agent) naturalMistakes(robot *Robot, world *World, dt float64) {
	if robot.Faults.Tipped || robot.Climbing || robot.ClimbLevel > 0 {
		return
	}
	consistency := agent.Personality.Consistency
	if agent.mechReliability == 0 { // joined mid-match (no match start seen)
		agent.mechReliability = world.teamReliability(robot.Team)
	}
	reliability := agent.mechReliability
	// Overdriving a bump: high-centered for a few seconds.
	if z, _ := world.groundHeight(robot.Pos); z > world.layout.Bump.Height*0.6 && world.time >= robot.Faults.StuckUntil {
		speed := robot.Vel.Len()
		if speed > 2.2 && agent.rng.Float64() < (1.1-consistency)*0.012*(speed/3) {
			robot.Faults.StuckUntil = world.time + 2.5 + agent.rng.Float64()*3.5
			robot.Vel = Vec2{}
			world.noteIncident(robot, "high-centered", true)
			world.logf("warn", "world", "%d (%s) overdrove the bump and is high-centered", robot.Team, robot.Station)
		}
	}
	// Mechanisms jam now and then (a ball wedged in the intake, the indexer double-feeding).
	if robot.IntakeOn && world.time >= robot.Faults.IntakeJamUntil && agent.rng.Float64() < (1-reliability)*0.01*dt {
		robot.Faults.IntakeJamUntil = world.time + 3 + agent.rng.Float64()*5
		world.noteIncident(robot, "intake jam", false)
		world.logf("warn", "world", "%d (%s): intake jammed", robot.Team, robot.Station)
	}
	if robot.Shooting && world.time >= robot.Faults.ShooterJamUntil && agent.rng.Float64() < (1-reliability)*0.025*dt {
		robot.Faults.ShooterJamUntil = world.time + 2 + agent.rng.Float64()*4
		world.noteIncident(robot, "shooter jam", false)
		world.logf("warn", "world", "%d (%s): shooter jammed", robot.Team, robot.Station)
	}
}

// recover is the drive team (and its robot code) working its way out of trouble rather than waiting: rocking free of
// a high-center, running the intake backwards to clear a jam, cycling the indexer to clear the shooter.
func (agent *Agent) recover(robot *Robot, world *World, dt float64) {
	skill := clamp(robot.Skill, 0.5, 1.3)
	if world.time < robot.Faults.StuckUntil && robot.Faults.StuckUntil-world.time > 1 && !robot.Faults.Tipped {
		if agent.rng.Float64() < agent.Personality.Consistency*0.5*skill*dt {
			robot.Faults.StuckUntil = world.time
			world.logf("info", "world", "%d (%s) rocked itself free", robot.Team, robot.Station)
		}
	}
	if world.time < robot.Faults.IntakeJamUntil && agent.rng.Float64() < 0.25*skill*dt {
		robot.Faults.IntakeJamUntil = world.time
		world.logf("info", "world", "%d (%s) cleared its intake by running it backwards", robot.Team, robot.Station)
	}
	if world.time < robot.Faults.ShooterJamUntil && agent.rng.Float64() < 0.2*skill*dt {
		robot.Faults.ShooterJamUntil = world.time
		world.logf("info", "world", "%d (%s) cycled the indexer and cleared the shooter", robot.Team, robot.Station)
	}
}

// readGameData is what this drive team believes the game data says: right, unless it misread it at the start of
// teleop, which it notices once the first shift's hub lights disagree.
func (agent *Agent) readGameData(robot *Robot, world *World) (bool, bool) {
	won, known := wonAuto(robot)
	if known && agent.misread {
		t, _ := agent.clock.matchTime(world.time)
		if t < TeleopStartSec+TransitionSec+1.5 {
			return !won, true
		}
		agent.misread = false
		world.noteIncident(robot, "misread game data", false)
		world.logf("warn", "ai", "%d (%s) misread the game data and played the first shift the wrong way round", robot.Team,
			robot.Station)
	}
	return won, known
}

// reviewMatch is the pit-crew part of the review: what broke gets fixed, memories fade.
func (world *World) reviewMatch(team int) {
	stats := world.teamStats(team)
	for _, incident := range stats.Incidents {
		// (stats.Matches already counts the match under review; incidents during it carry the count from before.)
		if incident.Last >= stats.Matches-1 && (incident.Kind == "intake jam" || incident.Kind == "shooter jam" || incident.Kind == "tipped") {
			if stats.Repairs < maxRepairs {
				stats.Repairs = math.Min(maxRepairs, stats.Repairs+reliabilityRepair)
				what := map[string]string{"intake jam": "the intake after it jammed", "shooter jam": "the shooter after it jammed",
					"tipped": "the robot after it tipped over"}[incident.Kind]
				world.logf("info", "ai", "%d's pit crew worked on %s", team, what)
			}
		}
	}
	fadeMemories(stats)
}

// learnedDiscipline is how much more careful a team has become after fouls (added to its discipline).
func (stats *TeamStats) learnedDiscipline() float64 {
	return math.Min(0.3, 0.06*stats.incidentWeight("foul"))
}

// ---- What the panel shows ----------------------------------------------------------------------------------------------

// ChoiceView is one experiment record.
type ChoiceView struct {
	Choice string  `json:"choice"`
	Runs   float64 `json:"runs"`
	Rate   float64 `json:"rate"`
}

func (stats *TeamStats) choiceViews() []ChoiceView {
	var views []ChoiceView
	for key, record := range stats.Choices {
		n := record.Wins + record.Losses
		if n < 0.5 {
			continue
		}
		views = append(views, ChoiceView{Choice: key, Runs: round2(n), Rate: round2(record.Wins / n)})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Choice < views[j].Choice })
	return views
}

// TippingLocked reports whether robots can tip over (the AI setting; the world lock must be held).
func (world *World) TippingLocked() bool { return world.tipping }
