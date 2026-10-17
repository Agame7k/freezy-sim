package world

import (
	"math"
	"math/rand"
	"sync"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/ds"
)

// The AI benchmark plays whole matches inside the world, with a stand-in FMS that follows Freezy Arena's rules
// (timing, 500 ms DS packets with whole-second match time, game data, hub lights and motors, shift scoring with the
// grace period, tower points, and the auto-referee's fouls). It needs no arena, so it runs much faster than real time
// and is repeatable from a seed. It measures how well the AI plays and how often it looks broken.

// BenchOptions configure a benchmark run.
type BenchOptions struct {
	Matches     int
	Seed        int64
	Teams       []int // team pool; six per match, rotating
	PerfectInfo bool  // AI sees every ball (no limited sight)
	Chaos       bool  // random robot faults
	// Workers plays that many independent events side by side (each its own field and seed, matches split between
	// them): several times faster on a multi-core machine. Teams then learn within each event only.
	Workers int
	// State starts the teams from saved designs, personalities and learning (pre-season training of live teams).
	State *SavedState `json:"-"`
}

// BenchAlliance is one alliance's result in one match.
type BenchAlliance struct {
	Plan        string `json:"plan"`
	AutoFuel    int    `json:"autoFuel"`
	TeleopFuel  int    `json:"teleopFuel"`
	WastedFuel  int    `json:"wastedFuel"` // counted while inactive
	AutoTower   int    `json:"autoTower"`
	TeleopTower int    `json:"teleopTower"`
	FoulPoints  int    `json:"foulPoints"` // awarded to this alliance for the opponent's fouls
	Score       int    `json:"score"`
	Fouls       int    `json:"fouls"` // committed
	WonAuto     bool   `json:"wonAuto"`
	BonusRP     int    `json:"bonusRP"` // Energized + Supercharged + Traversal
}

// BenchRobot is one robot's result in one match.
type BenchRobot struct {
	Station        string            `json:"station"`
	Team           int               `json:"team"`
	Archetype      string            `json:"archetype"`
	Strategy       string            `json:"strategy"`
	Shots          int               `json:"shots"`
	Scored         int               `json:"scored"`
	Passes         int               `json:"passes"`
	IdleSeconds    float64           `json:"idleSeconds"`
	StuckSeconds   float64           `json:"stuckSeconds"`
	ClimbLevel     int               `json:"climbLevel"`
	ClimbAttempted bool              `json:"climbAttempted"`
	SkippedClimb   bool              `json:"skippedClimb"` // chose to keep scoring FUEL
	EarlyShots     int               `json:"earlyShots"`
	EarlyIn        int               `json:"earlyIn"`
	EarlyScored    int               `json:"earlyScored"`
	LateShots      int               `json:"lateShots"`
	LateIn         int               `json:"lateIn"`
	LateScored     int               `json:"lateScored"`
	Auto           *AutoResult       `json:"auto,omitempty"`
	ClimbGoal      int               `json:"climbGoal,omitempty"`
	Choices        map[string]string `json:"choices,omitempty"` // the team's experimental choices this match
}

// BenchMatch is one benchmark match.
type BenchMatch struct {
	Number     int              `json:"number"`
	Alliances  [2]BenchAlliance `json:"alliances"`
	Robots     []BenchRobot     `json:"robots"`
	LeadSwaps  int              `json:"leadSwaps"` // how often the lead changed hands (entertainment)
	FoulCalls  []string         `json:"foulCalls"`
	HumanShots [2]int           `json:"humanShots"`
	HumanMade  [2]int           `json:"humanMade"`
	Mixes      [2]string        `json:"mixes"` // each alliance's robot designs
}

// BenchSummary averages a run.
type BenchSummary struct {
	Matches          int     `json:"matches"`
	AvgScore         float64 `json:"avgScore"`
	AvgFuel          float64 `json:"avgFuel"`
	AvgWastedFuel    float64 `json:"avgWastedFuel"`
	ShotAccuracy     float64 `json:"shotAccuracy"`
	IdlePerRobot     float64 `json:"idlePerRobot"`  // seconds per robot per match
	StuckPerRobot    float64 `json:"stuckPerRobot"` // seconds per robot per match
	FoulsPerMatch    float64 `json:"foulsPerMatch"`
	ClimbRate        float64 `json:"climbRate"` // share of robots on the tower at the end
	AvgClimbPoints   float64 `json:"avgClimbPoints"`
	LeadSwaps        float64 `json:"leadSwaps"`
	EarlyShots       float64 `json:"earlyShots"` // per match
	EarlyCountedRate float64 `json:"earlyCountedRate"`
	LateShots        float64 `json:"lateShots"`
	LateCountedRate  float64 `json:"lateCountedRate"`
	HumanAccuracy    float64 `json:"humanAccuracy"`
	HumanShots       float64 `json:"humanShots"` // per match
	BonusRP          float64 `json:"bonusRP"`    // per alliance per match
	ClimbSkips       float64 `json:"climbSkips"` // robots per match that chose FUEL over a climb they could make
	// AUTO routines.
	AutoFuel      float64 `json:"autoFuel"`      // per alliance
	AutoStepsDone float64 `json:"autoStepsDone"` // share of routine steps that finished normally
	AutoMistakes  float64 `json:"autoMistakes"`  // chooser mistakes per match
	AutoOdoError  float64 `json:"autoOdoError"`  // metres off at the end of AUTO, per robot
	AutoContacts  float64 `json:"autoContacts"`  // routine steps that hit another robot, per match
}

// BenchReport is a whole run.
type BenchReport struct {
	Options BenchOptions       `json:"options"`
	Summary BenchSummary       `json:"summary"`
	Matches []BenchMatch       `json:"matches"`
	Autos   []AutoLibraryEntry `json:"autos"` // every team's auto routines at the end of the run (with tuning notes)
	// Learning is what the teams knew at the end (the first event's, with several workers).
	Learning *Learning `json:"-"`
}

type benchStation struct{ command ds.Command }

func (station *benchStation) Command() ds.Command { return station.command }
func (station *benchStation) SetLoad(float64)     {}

// benchField is the FMS side of the field I/O: it timestamps every hub count with the match clock.
type benchField struct {
	matchTime float64
	counts    [2][]float64
	motors    bool
	lights    [2]bool
}

const benchIOLatency = 0.08 // seconds from the hub sensor to the arena's count (PLC/alt-IO polling)

func (field *benchField) ScoreFuel(alliance, sensor int) {
	field.counts[alliance] = append(field.counts[alliance], field.matchTime+benchIOLatency)
}
func (field *benchField) HubMotorsOn() (bool, bool) { return field.motors, field.motors }
func (field *benchField) HubLightsOn() (bool, bool) { return field.lights[Red], field.lights[Blue] }

// RunBench plays the benchmark. progress (optional) is called after each match (from several goroutines at once with
// more than one worker).
func RunBench(layout *Layout, options BenchOptions, progress func(BenchMatch)) BenchReport {
	if options.Matches <= 0 {
		options.Matches = 10
	}
	if len(options.Teams) < 6 {
		options.Teams = []int{254, 1678, 4414, 971, 118, 2056, 1323, 6328, 3476, 2910, 148, 1690}
	}
	workers := min(max(1, options.Workers), options.Matches)
	if workers == 1 {
		return runBenchEvent(layout, options, progress)
	}
	// Independent events side by side; their matches are reported in order, event after event.
	reports := make([]BenchReport, workers)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	for k := 0; k < workers; k++ {
		event := options
		event.Workers = 1
		event.Seed = options.Seed + int64(k)*1000003
		event.Matches = options.Matches / workers
		if k < options.Matches%workers {
			event.Matches++
		}
		wg.Add(1)
		go func(k int, event BenchOptions) {
			defer wg.Done()
			reports[k] = runBenchEvent(layout, event, func(match BenchMatch) {
				if progress != nil {
					progressMu.Lock()
					progress(match)
					progressMu.Unlock()
				}
			})
		}(k, event)
	}
	wg.Wait()
	report := BenchReport{Options: options, Learning: reports[0].Learning}
	for _, event := range reports {
		for _, match := range event.Matches {
			match.Number = len(report.Matches) + 1
			report.Matches = append(report.Matches, match)
		}
		report.Autos = append(report.Autos, event.Autos...)
	}
	report.Summary = summarizeBench(report.Matches)
	return report
}

// runBenchEvent plays one event's matches on one field.
func runBenchEvent(layout *Layout, options BenchOptions, progress func(BenchMatch)) BenchReport {
	world := New(layout, bus.New())
	if options.State != nil {
		world.applyState(*options.State)
	}
	world.quiet = true
	world.rng = rand.New(rand.NewSource(options.Seed))
	world.perfectInfo = options.PerfectInfo
	// New planned the alliances with a clock-seeded generator: plan again from the seed, or the robots' first
	// starting spots (and everything after) would differ from run to run.
	world.planAlliance(Red)
	world.planAlliance(Blue)
	field := &benchField{}
	world.matchClock = func() (float64, bool) { return field.matchTime, field.matchTime <= TeleopEndSec+GraceSec }
	var stations [6]*benchStation
	var links [6]StationLink
	for i := range stations {
		stations[i] = &benchStation{}
		links[i] = stations[i]
	}
	world.Attach(links, field)
	for i, robot := range world.robots {
		world.SetController(i, NewAgent(robot, options.Seed*31+int64(i+1)))
	}
	chaosRng := rand.New(rand.NewSource(options.Seed + 7))

	report := BenchReport{Options: options}
	for n := 0; n < options.Matches; n++ {
		match := world.benchMatch(n, options, stations, field, chaosRng)
		report.Matches = append(report.Matches, match)
		if progress != nil {
			progress(match)
		}
	}
	report.Summary = summarizeBench(report.Matches)
	world.mu.Lock()
	report.Autos = world.autoLibraryView(nil)
	learning := world.learning
	report.Learning = &learning
	world.mu.Unlock()
	return report
}

func (world *World) benchMatch(n int, options BenchOptions, stations [6]*benchStation, field *benchField,
	chaosRng *rand.Rand) BenchMatch {
	const dt = 1.0 / stepHz
	pool := options.Teams
	for i, station := range stations {
		station.command = ds.Command{Team: pool[(n*6+i)%len(pool)], RobotLinked: true, FmsLinked: true,
			RemainingSeconds: int(AutoSec), MatchType: 2}
	}
	// Robots roll on, then the field crew stages FUEL and robots.
	world.benchStep(dt)
	world.mu.Lock()
	world.resetLocked()
	world.mu.Unlock()
	*field = benchField{}
	field.matchTime = -1
	match := BenchMatch{Number: n + 1}
	for alliance := 0; alliance < 2; alliance++ {
		match.Alliances[alliance].Plan = world.planNames[alliance]
		match.Mixes[alliance] = world.allianceMix(alliance)
	}
	world.DrainInfractions()
	awarded := map[string]bool{}

	// Chaos: a few faults at random times, like the live chaos injector at level 2.
	type fault struct {
		at    float64
		robot int
		kind  int
	}
	var faults []fault
	if options.Chaos {
		for k := 0; k < 1+chaosRng.Intn(3); k++ {
			faults = append(faults, fault{at: 3 + chaosRng.Float64()*150, robot: chaosRng.Intn(6), kind: chaosRng.Intn(4)})
		}
	}

	wonAuto := [2]bool{}
	gameData := ""
	var autoLevels [6]int
	lastPacket := -1.0
	lastPeriod := ""
	foulTimer := 0.0
	lead := 0
	for t := 0.0; t < TeleopEndSec+GraceSec+0.5; t += dt {
		period := "post"
		switch {
		case t < AutoSec:
			period = "auto"
		case t < TeleopStartSec:
			period = "pause"
		case t < TeleopEndSec:
			period = "teleop"
		}
		if period != lastPeriod {
			if period == "pause" {
				levels := world.ClimbLevelsLocked()
				autoLevels = levels
			}
			if period == "teleop" {
				// The arena picks the auto winner from auto FUEL (random on a tie) and sends it as game data.
				red, blue := field.shiftFuel(Red, ShiftAuto, false), field.shiftFuel(Blue, ShiftAuto, false)
				redWon := red > blue || red == blue && world.rng.Intn(2) == 1
				wonAuto = [2]bool{redWon, !redWon}
				gameData = "B"
				if redWon {
					gameData = "R"
				}
			}
		}
		// DS control packets: every 500 ms, and immediately on a period change.
		if period != lastPeriod || t-lastPacket >= 0.5 {
			lastPacket = t
			remaining := 0
			switch period {
			case "auto":
				remaining = int(AutoSec) - int(t)
			case "pause":
				remaining = int(TeleopSec)
			case "teleop":
				remaining = int(TeleopEndSec) - int(t)
			}
			for _, station := range stations {
				station.command.Enabled = period == "auto" || period == "teleop"
				station.command.Auto = period == "auto"
				station.command.RemainingSeconds = remaining
				station.command.GameData = gameData
			}
		}
		lastPeriod = period
		// Hub lights and motors, as the arena drives them.
		field.matchTime = t
		field.motors = t < TeleopEndSec+GraceSec+7
		for alliance := 0; alliance < 2; alliance++ {
			field.lights[alliance] = benchHubLight(t, wonAuto[alliance], wonAuto[1-alliance])
		}
		for _, f := range faults {
			if t >= f.at && t < f.at+dt {
				world.benchFault(f.robot, f.kind)
			}
		}
		world.benchStep(dt)

		// Referee: fouls at 4 Hz.
		foulTimer += dt
		if foulTimer >= 0.25 {
			foulTimer = 0
			for _, call := range world.DrainInfractions() {
				if call.AwardLevel3 != "" {
					awarded[call.AwardLevel3] = true
				}
				match.Alliances[call.Alliance].Fouls++
				points := 5
				if call.Major {
					points = 15
				}
				match.Alliances[1-call.Alliance].FoulPoints += points
				match.FoulCalls = append(match.FoulCalls, call.Rule+" "+call.Reason)
			}
			// Track lead changes on FUEL points so far.
			red, blue := field.points(Red, wonAuto[Red], t), field.points(Blue, wonAuto[Blue], t)
			current := 0
			if red > blue {
				current = 1
			} else if blue > red {
				current = -1
			}
			if current != 0 && lead != 0 && current != lead {
				match.LeadSwaps++
			}
			if current != 0 {
				lead = current
			}
		}
	}
	endLevels := world.ClimbLevelsLocked()
	for i, station := range stationNames {
		if awarded[station] {
			endLevels[i] = 3 // G420
		}
	}
	for alliance := 0; alliance < 2; alliance++ {
		a := &match.Alliances[alliance]
		a.WonAuto = wonAuto[alliance]
		for _, count := range field.counts[alliance] {
			shift, ok := ScoringShift(count, wonAuto[alliance])
			switch {
			case !ok:
			case shift == ShiftAuto:
				a.AutoFuel++
			case ShiftActive(shift, wonAuto[alliance]):
				a.TeleopFuel++
			default:
				a.WastedFuel++
			}
		}
		for i := alliance * 3; i < alliance*3+3; i++ {
			a.AutoTower += TowerPoints(autoLevels[i], true)
			a.TeleopTower += TowerPoints(endLevels[i], false)
		}
		a.Score = a.AutoFuel + a.TeleopFuel + a.AutoTower + a.TeleopTower + a.FoulPoints
		fuel := a.AutoFuel + a.TeleopFuel
		for _, reached := range []bool{fuel >= EnergizedFuel, fuel >= SuperchargedFuel,
			a.AutoTower+a.TeleopTower >= TraversalTower} {
			if reached {
				a.BonusRP++
			}
		}
	}
	world.mu.Lock()
	for i, robot := range world.robots {
		var stats ShotStats
		if agent, ok := world.controllers[i].(*Agent); ok {
			stats = agent.shotStats
		}
		match.Robots = append(match.Robots, BenchRobot{
			Station: robot.Station, Team: robot.Team, Archetype: robot.Arch.Name, Strategy: world.plans[i].Strategy,
			Shots: robot.Shots, Scored: robot.Scored, Passes: robot.Passes,
			IdleSeconds: robot.Activity.IdleSeconds, StuckSeconds: robot.Activity.StuckSeconds,
			ClimbLevel: endLevels[i], EarlyShots: stats.Early, EarlyIn: stats.EarlyIn, EarlyScored: stats.EarlyScored,
			LateShots: stats.Late, LateIn: stats.LateIn, LateScored: stats.LateScored,
		})
		if agent, ok := world.controllers[i].(*Agent); ok {
			match.Robots[len(match.Robots)-1].ClimbAttempted = agent.climbGoal > 0
			match.Robots[len(match.Robots)-1].SkippedClimb = agent.climbPlan < 0 && robot.Arch.ClimbLevel > 0
			match.Robots[len(match.Robots)-1].Auto = agent.autoResult(robot, world)
			match.Robots[len(match.Robots)-1].ClimbGoal = agent.climbGoal
			match.Robots[len(match.Robots)-1].Choices = agent.choices
		}
	}
	for alliance, outpost := range world.outposts {
		match.HumanShots[alliance], match.HumanMade[alliance] = outpost.Throws, outpost.Made
	}
	world.mu.Unlock()
	world.matchEnded(match)
	return match
}

// benchHubLight mirrors the arena's hub light: on while active, blinking for the last 3 s of an active period (and
// for the auto winner during the transition shift), off while inactive.
func benchHubLight(t float64, wonAuto, opponentWonAuto bool) bool {
	blinkOn := math.Mod(t, 0.5) < 0.25
	switch {
	case t < TeleopStartSec:
		return true // auto and pause
	case t >= TeleopEndSec:
		return false
	case t < TeleopStartSec+TransitionSec:
		if wonAuto {
			return blinkOn
		}
		return true
	}
	if !ActiveAt(t, wonAuto) {
		return false
	}
	// Find when this active period ends.
	end := t
	for end < TeleopEndSec && ActiveAt(end+0.05, wonAuto) {
		end += 0.05
	}
	if end-t <= LightWarnSec {
		return blinkOn
	}
	return true
}

// benchStep advances one physics step without publishing anything.
func (world *World) benchStep(dt float64) {
	world.mu.Lock()
	defer world.mu.Unlock()
	world.steps++
	world.step(dt)
}

func (world *World) benchFault(index, kind int) {
	world.mu.Lock()
	defer world.mu.Unlock()
	robot := world.robots[index]
	switch kind {
	case 0:
		robot.Faults.IntakeJamUntil = world.time + 8 + world.rng.Float64()*10
	case 1:
		robot.Faults.ShooterJamUntil = world.time + 6 + world.rng.Float64()*8
	case 2:
		robot.Faults.SpeedScale = 0.55
	case 3:
		robot.Faults.HighCenterNext = true
	}
}

// ClimbLevelsLocked is ClimbLevels for callers that do not hold the lock.
func (world *World) ClimbLevelsLocked() [6]int { return world.ClimbLevels() }

func (field *benchField) shiftFuel(alliance, shift int, wonAuto bool) int {
	count := 0
	for _, t := range field.counts[alliance] {
		if s, ok := ScoringShift(t, wonAuto); ok && s == shift {
			count++
		}
	}
	return count
}

// points is the FUEL points counted so far.
func (field *benchField) points(alliance int, wonAuto bool, now float64) int {
	points := 0
	for _, t := range field.counts[alliance] {
		if t <= now && FuelScores(t, wonAuto) {
			points++
		}
	}
	return points
}

func summarizeBench(matches []BenchMatch) BenchSummary {
	var summary BenchSummary
	summary.Matches = len(matches)
	if len(matches) == 0 {
		return summary
	}
	var shots, scored, robots, climbs, early, earlyIn, earlyScored, late, lateIn, lateScored, humanShots, humanMade int
	var autos, autoSteps, autoDone int
	for _, match := range matches {
		for _, a := range match.Alliances {
			summary.AvgScore += float64(a.Score)
			summary.AvgFuel += float64(a.AutoFuel + a.TeleopFuel)
			summary.AvgWastedFuel += float64(a.WastedFuel)
			summary.FoulsPerMatch += float64(a.Fouls)
			summary.AvgClimbPoints += float64(a.AutoTower + a.TeleopTower)
			summary.BonusRP += float64(a.BonusRP)
		}
		summary.LeadSwaps += float64(match.LeadSwaps)
		for _, a := range match.Alliances {
			summary.AutoFuel += float64(a.AutoFuel)
		}
		for _, r := range match.Robots {
			if r.Auto != nil {
				autos++
				summary.AutoOdoError += r.Auto.EndError
				if r.Auto.Mistake != "" {
					summary.AutoMistakes++
				}
				for _, step := range r.Auto.Steps {
					autoSteps++
					if step.Completed {
						autoDone++
					}
					if step.Contact != "" {
						summary.AutoContacts++
					}
				}
			}
			robots++
			shots += r.Shots
			scored += r.Scored
			summary.IdlePerRobot += r.IdleSeconds
			summary.StuckPerRobot += r.StuckSeconds
			if r.ClimbLevel > 0 {
				climbs++
			}
			if r.SkippedClimb {
				summary.ClimbSkips++
			}
			early += r.EarlyShots
			earlyIn += r.EarlyIn
			earlyScored += r.EarlyScored
			late += r.LateShots
			lateIn += r.LateIn
			lateScored += r.LateScored
		}
		for alliance := 0; alliance < 2; alliance++ {
			humanShots += match.HumanShots[alliance]
			humanMade += match.HumanMade[alliance]
		}
	}
	m := float64(len(matches))
	summary.AvgScore /= 2 * m
	summary.AvgFuel /= 2 * m
	summary.AvgWastedFuel /= 2 * m
	summary.AvgClimbPoints /= 2 * m
	summary.BonusRP /= 2 * m
	summary.ClimbSkips /= m
	summary.AutoFuel /= 2 * m
	summary.AutoMistakes /= m
	summary.AutoContacts /= m
	if autos > 0 {
		summary.AutoOdoError /= float64(autos)
	}
	if autoSteps > 0 {
		summary.AutoStepsDone = float64(autoDone) / float64(autoSteps)
	}
	summary.FoulsPerMatch /= m
	summary.LeadSwaps /= m
	if shots > 0 {
		summary.ShotAccuracy = float64(scored) / float64(shots)
	}
	if robots > 0 {
		summary.IdlePerRobot /= float64(robots)
		summary.StuckPerRobot /= float64(robots)
		summary.ClimbRate = float64(climbs) / float64(robots)
	}
	summary.EarlyShots = float64(early) / m
	summary.LateShots = float64(late) / m
	if earlyIn > 0 {
		summary.EarlyCountedRate = float64(earlyScored) / float64(earlyIn)
	}
	if lateIn > 0 {
		summary.LateCountedRate = float64(lateScored) / float64(lateIn)
	}
	summary.HumanShots = float64(humanShots) / m
	if humanShots > 0 {
		summary.HumanAccuracy = float64(humanMade) / float64(humanShots)
	}
	return summary
}
