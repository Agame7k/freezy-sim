package world

import (
	"math"
	"sort"
	"strings"
)

// Learning across matches. Every team keeps statistics from the matches it plays (shooting, climbing, how its early and
// late shots worked out) and adjusts: drive teams whose timed shots keep counting get bolder, ones that keep missing
// the window get more careful, and drivers get a little better with practice over an event. Alliances also learn which
// game plans work for their mix of robot designs: plans are chosen by Thompson sampling over each plan's record.
// Everything is saved with the rest of the world state, and the AI panel can reset it.

// TeamStats is what the simulator has learned about one team.
type TeamStats struct {
	Matches     int     `json:"matches"`
	Shots       int     `json:"shots"`
	Scored      int     `json:"scored"`
	ClimbTried  int     `json:"climbTried"`
	ClimbMade   int     `json:"climbMade"`
	TimedIn     int     `json:"timedIn"`     // early/late shots that reached the hub
	TimedScored int     `json:"timedScored"` // ... and counted
	RiskAdjust  float64 `json:"riskAdjust"`  // learned shift of the risk appetite
	Practice    float64 `json:"practice"`    // skill gained over the event
	// AUTO: chooser mistakes made (each one makes the drive team more careful), and how much of its wheel calibration
	// error the team has tuned out (0..0.9).
	AutoMistakes int     `json:"autoMistakes"`
	OdoCal       float64 `json:"odoCal"`
	AutoRuns     int     `json:"autoRuns"`
	AutoPoints   float64 `json:"autoPoints"` // running average
	// Experiments (choice → record), remembered mistakes, and the pit crew's repairs (memory.go).
	Choices   map[string]*PlanRecord `json:"choices,omitempty"`
	Incidents []*Incident            `json:"incidents,omitempty"`
	Repairs   float64                `json:"repairs"`
	// The robot the team built and its pit changes (design.go).
	Design *DesignRecord `json:"design,omitempty"`
}

// PlanRecord is one game plan's results for one mix of robot designs.
type PlanRecord struct {
	Wins   float64 `json:"wins"`
	Losses float64 `json:"losses"`
}

// Learning is everything learned so far.
type Learning struct {
	Teams map[int]*TeamStats                `json:"teams"`
	Plans map[string]map[string]*PlanRecord `json:"plans"` // alliance mix → plan name → record
	Autos map[int]map[string]*AutoProgram   `json:"autos"` // team → routine@start → its auto routine
}

func newLearning() Learning {
	return Learning{Teams: map[int]*TeamStats{}, Plans: map[string]map[string]*PlanRecord{},
		Autos: map[int]map[string]*AutoProgram{}}
}

func (world *World) teamStats(team int) *TeamStats {
	stats, ok := world.learning.Teams[team]
	if !ok {
		stats = &TeamStats{}
		world.learning.Teams[team] = stats
	}
	return stats
}

// allianceMix is the key plans are learned under: the alliance's robot designs, sorted.
func (world *World) allianceMix(alliance int) string {
	var names []string
	for _, robot := range world.robots {
		if robot.Alliance == alliance && robot.Present {
			names = append(names, robot.Arch.Class)
		}
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

// choosePlan picks a game plan for an alliance by Thompson sampling over the plans' records for its mix.
func (world *World) choosePlan(alliance int) planTemplate {
	records := world.learning.Plans[world.allianceMix(alliance)]
	best, bestSample := planTemplates[0], -1.0
	for _, template := range planTemplates {
		wins, losses := 0.0, 0.0
		if record, ok := records[template.name]; ok {
			wins, losses = record.Wins, record.Losses
		}
		sample := world.betaSample(wins+1, losses+1)
		if sample > bestSample {
			best, bestSample = template, sample
		}
	}
	return best
}

// betaSample draws from Beta(a, b) (as the ratio of two gamma draws).
func (world *World) betaSample(a, b float64) float64 {
	x, y := world.gammaSample(a), world.gammaSample(b)
	return x / (x + y)
}

// gammaSample draws from Gamma(shape, 1) (Marsaglia–Tsang).
func (world *World) gammaSample(shape float64) float64 {
	if shape < 1 {
		return world.gammaSample(shape+1) * math.Pow(world.rng.Float64(), 1/shape)
	}
	d := shape - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		x := world.rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := world.rng.Float64()
		if math.Log(u) < 0.5*x*x+d-d*v+d*math.Log(v) {
			return d * v
		}
	}
}

// matchEnded learns from a finished match (benchmark or real).
func (world *World) matchEnded(match BenchMatch) {
	world.mu.Lock()
	defer world.mu.Unlock()
	world.learnFromLocked(match)
}

func (world *World) learnFromLocked(match BenchMatch) {
	if world.learning.Teams == nil {
		world.learning = newLearning()
	}
	for _, r := range match.Robots {
		if r.Team <= 0 {
			continue
		}
		stats := world.teamStats(r.Team)
		stats.Matches++
		stats.Shots += r.Shots
		stats.Scored += r.Scored
		if r.ClimbAttempted {
			stats.ClimbTried++
			if r.ClimbLevel > 0 {
				stats.ClimbMade++
			}
		}
		timedIn := r.EarlyIn + r.LateIn
		timedScored := r.EarlyScored + r.LateScored
		stats.TimedIn += timedIn
		stats.TimedScored += timedScored
		if timedIn >= 3 {
			rate := float64(timedScored) / float64(timedIn)
			switch {
			case rate > 0.92:
				stats.RiskAdjust += 0.03 // it keeps working: cut it a little finer
			case rate < 0.75:
				stats.RiskAdjust -= 0.06 // it keeps missing the window: be more careful
			}
			stats.RiskAdjust = clamp(stats.RiskAdjust, -0.4, 0.3)
		}
		stats.Practice = math.Min(0.1, stats.Practice+0.01)
		if r.Auto != nil {
			world.reviewAuto(r.Team, *r.Auto)
		}
		result := 0.5
		if alliance := allianceOfStation(r.Station); alliance >= 0 {
			own, other := match.Alliances[alliance].Score, match.Alliances[1-alliance].Score
			if own > other {
				result = 1
			} else if own < other {
				result = 0
			}
		}
		world.creditExperiments(r, result)
		world.reviewMatch(r.Team)
		world.reviewDesignLocked(r, stats)
	}
	// Plan records.
	red, blue := match.Alliances[Red], match.Alliances[Blue]
	result := [2]float64{0.5, 0.5}
	if red.Score > blue.Score {
		result = [2]float64{1, 0}
	} else if blue.Score > red.Score {
		result = [2]float64{0, 1}
	}
	for alliance := 0; alliance < 2; alliance++ {
		mix := match.Mixes[alliance]
		plan := match.Alliances[alliance].Plan
		if mix == "" || plan == "" {
			continue
		}
		if world.learning.Plans[mix] == nil {
			world.learning.Plans[mix] = map[string]*PlanRecord{}
		}
		record := world.learning.Plans[mix][plan]
		if record == nil {
			record = &PlanRecord{}
			world.learning.Plans[mix][plan] = record
		}
		record.Wins += result[alliance]
		record.Losses += 1 - result[alliance]
	}
	world.markDirtyLocked()
}

// personalityFor is a team's drive team: its own personality (or the user's override), what it has learned, and the
// difficulty preset.
func (world *World) personalityFor(team int) Personality {
	personality := personalityForTeam(team)
	if override, ok := world.personalities[team]; ok {
		personality = override
	}
	if stats, ok := world.learning.Teams[team]; ok {
		personality.Risk = clamp(personality.Risk+stats.RiskAdjust, 0, 1)
		// Fouls teach care: more discipline, a little less appetite for contact.
		learned := stats.learnedDiscipline()
		personality.Discipline = clamp(personality.Discipline+learned, 0, 1)
		personality.Aggression = clamp(personality.Aggression-learned*0.6, 0, 1)
	}
	return personality.scaled(world.difficulty)
}

// allianceOfStation is Red for R1–R3, Blue for B1–B3, -1 otherwise.
func allianceOfStation(station string) int {
	switch {
	case len(station) == 2 && station[0] == 'R':
		return Red
	case len(station) == 2 && station[0] == 'B':
		return Blue
	}
	return -1
}

// effectiveSkill is a team's driving skill with practice and the difficulty preset applied.
func (world *World) effectiveSkill(team int, base float64) float64 {
	skill := base
	if stats, ok := world.learning.Teams[team]; ok {
		skill *= 1 + stats.Practice
	}
	if scale, ok := difficultyScales[world.difficulty]; ok {
		skill *= scale.skill
	}
	return skill
}
