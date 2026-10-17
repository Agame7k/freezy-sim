package world

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// What the AI panel and the 3D AI overlay show (published as "world.ai" at 4 Hz), and the AI settings commands.

const aiEvery = 30 // physics steps between AI summaries (4 Hz)

// AIRobot is one drive team's current thinking.
type AIRobot struct {
	Station     string        `json:"station"`
	Team        int           `json:"team"`
	Job         string        `json:"job"`
	Intent      string        `json:"intent"`
	State       string        `json:"state"`
	Options     []JobScore    `json:"options"` // best first
	Target      *[2]float64   `json:"target,omitempty"`
	Path        [][2]float64  `json:"path,omitempty"`
	Mark        string        `json:"mark,omitempty"` // opponent it is guarding
	Personality Personality   `json:"personality"`
	Shots       ShotStats     `json:"shots"`
	Climb       []ClimbOption `json:"climb,omitempty"`
	ClimbPlan   int           `json:"climbPlan"` // -1 keep scoring, 0 undecided, n level
	StillReason string        `json:"stillReason,omitempty"`
	ClockError  float64       `json:"clockError"` // how far its match clock estimate is off (when the FMS time is known)
	Auto        *AutoView     `json:"auto,omitempty"`
	Experiment  string        `json:"experiment,omitempty"` // what the team is trying this match
	Reliability float64       `json:"reliability"`          // how robust its robot is
	// The robot's pose estimate (odometry + vision) and how far off it is.
	Est      [3]float64 `json:"est"`
	OdoError float64    `json:"odoError"`
	Vision   bool       `json:"vision"`
	TagsSeen int        `json:"tagsSeen"`
}

// AISummary is the published AI state.
type AISummary struct {
	Robots      []AIRobot          `json:"robots"`
	Coach       [2][]CoachCall     `json:"coach"`
	Scoreboard  [2]int             `json:"scoreboard"`
	Difficulty  string             `json:"difficulty"`
	PerfectInfo bool               `json:"perfectInfo"`
	TeamDesigns bool               `json:"teamDesigns"` // teams build their own robots
	Tipping     bool               `json:"tipping"`     // hard hits can tip tall robots over
	Learned     []LearnedTeam      `json:"learned"`
	Human       [2]HumanSummary    `json:"human"`
	Autos       []AutoLibraryEntry `json:"autos"`      // the routines of the teams on the field
	Seed        int64              `json:"seed"`       // this match's seed
	ReplaySeed  int64              `json:"replaySeed"` // the seed the next match will use (0 = random)
}

// LearnedTeam is a team's learned statistics, for the panel.
type LearnedTeam struct {
	Team int `json:"team"`
	TeamStats
	Reliability float64      `json:"reliability"`
	Records     []ChoiceView `json:"records"` // experiment records
	Discipline  float64      `json:"learnedDiscipline"`
}

// HumanSummary is a human player's state.
type HumanSummary struct {
	Mode     string  `json:"mode"`
	Accuracy float64 `json:"accuracy"`
	Throws   int     `json:"throws"`
	Made     int     `json:"made"`
	Corral   int     `json:"corral"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func (world *World) aiSummary() AISummary {
	summary := AISummary{Coach: world.coachCalls(), Scoreboard: world.scoreboard, Difficulty: world.difficulty, TeamDesigns: world.teamDesigns, Tipping: world.tipping,
		PerfectInfo: world.perfectInfo, Seed: world.seed, ReplaySeed: world.replaySeed}
	var trueTime float64
	haveTime := false
	if world.matchClock != nil {
		trueTime, haveTime = world.matchClock()
	}
	for i, robot := range world.robots {
		entry := AIRobot{Station: robot.Station, Team: robot.Team, State: robot.AiState,
			StillReason: robot.Activity.StillReason,
			Est:         [3]float64{round2(robot.Odo.Est.X), round2(robot.Odo.Est.Y), round2(robot.Odo.EstHeading)},
			OdoError:    round2(robot.odoError()), Vision: robot.Odo.Vision, TagsSeen: robot.Odo.TagsSeen}
		if agent, ok := world.controllers[i].(*Agent); ok && robot.Present && !robot.Manual {
			entry.Auto = world.autoView(robot, agent)
			entry.Experiment, entry.Reliability = agent.Experiment, round2(agent.mechReliability)
			if entry.Auto != nil && agent.auto != nil && agent.auto.result.Experiment {
				trying := "trying " + agent.auto.result.Program
				if entry.Experiment != "" {
					trying += "; " + entry.Experiment
				}
				entry.Experiment = trying
			}
			entry.Job, entry.Intent, entry.Personality = agent.job, agent.Intent, agent.Personality
			entry.Shots, entry.ClimbPlan = agent.shotStats, agent.climbPlan
			for k, option := range agent.jobScores {
				if k == 3 {
					break
				}
				entry.Options = append(entry.Options, option)
			}
			if agent.hasTarget {
				entry.Target = &[2]float64{round2(agent.target.X), round2(agent.target.Y)}
			}
			// The planned path (thinned to at most 12 points).
			step := max(1, len(agent.path)/12)
			for k := 0; k < len(agent.path); k += step {
				entry.Path = append(entry.Path, [2]float64{round2(agent.path[k].X), round2(agent.path[k].Y)})
			}
			if agent.contactTarget != nil {
				entry.Mark = agent.contactTarget.Station
			}
			for _, option := range agent.climbOptionsSeen {
				option.TowerPoints, option.FuelPoints, option.Fuel = round2(option.TowerPoints), round2(option.FuelPoints), round2(option.Fuel)
				entry.Climb = append(entry.Climb, option)
			}
			if haveTime && robot.Enabled {
				estimate, _ := agent.clock.matchTime(world.time)
				entry.ClockError = round2(estimate - trueTime)
			}
		}
		summary.Robots = append(summary.Robots, entry)
	}
	for team, stats := range world.learning.Teams {
		entry := LearnedTeam{Team: team, TeamStats: *stats, Reliability: round2(world.teamReliability(team)),
			Records: stats.choiceViews(), Discipline: round2(stats.learnedDiscipline())}
		entry.Choices = nil // (sent as Records)
		incidents := make([]*Incident, 0, len(stats.Incidents))
		for _, incident := range stats.Incidents {
			copied := *incident
			copied.Count = round2(copied.Count)
			incidents = append(incidents, &copied)
		}
		sort.Slice(incidents, func(i, j int) bool { return incidents[i].Count > incidents[j].Count })
		entry.Incidents = incidents
		summary.Learned = append(summary.Learned, entry)
	}
	sort.Slice(summary.Learned, func(i, j int) bool { return summary.Learned[i].Team < summary.Learned[j].Team })
	onField := map[int]bool{}
	for _, robot := range world.robots {
		if robot.Present && robot.Team > 0 {
			onField[robot.Team] = true
		}
	}
	summary.Autos = world.autoLibraryView(onField)
	for i, outpost := range world.outposts {
		summary.Human[i] = HumanSummary{Mode: outpost.HumanMode, Accuracy: outpost.Accuracy, Throws: outpost.Throws,
			Made: outpost.Made, Corral: outpost.Corral}
	}
	return summary
}

func (world *World) registerAICommands() {
	world.bus.Handle("world.ai.settings", func(args []byte) (any, error) {
		var request struct {
			Difficulty  *string `json:"difficulty"`
			PerfectInfo *bool   `json:"perfectInfo"`
			ReplaySeed  *int64  `json:"replaySeed"` // the next match uses this seed (0 = random)
			TeamDesigns *bool   `json:"teamDesigns"`
			Tipping     *bool   `json:"tipping"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		if request.Difficulty != nil {
			if _, ok := difficultyScales[*request.Difficulty]; !ok {
				return nil, fmt.Errorf("unknown difficulty %q", *request.Difficulty)
			}
			world.difficulty = *request.Difficulty
			world.refreshDriversLocked()
		}
		if request.PerfectInfo != nil {
			world.perfectInfo = *request.PerfectInfo
		}
		if request.ReplaySeed != nil {
			world.replaySeed = *request.ReplaySeed
		}
		if request.TeamDesigns != nil {
			world.teamDesigns = *request.TeamDesigns
		}
		if request.Tipping != nil {
			world.tipping = *request.Tipping
		}
		world.markDirtyLocked()
		return nil, nil
	})
	world.bus.Handle("world.ai.personality", func(args []byte) (any, error) {
		// Override a team's drive-team personality (reset = back to its own).
		var request struct {
			Team        int          `json:"team"`
			Personality *Personality `json:"personality"`
			Reset       bool         `json:"reset"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Team <= 0 {
			return nil, fmt.Errorf("team number required")
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		if request.Reset || request.Personality == nil {
			delete(world.personalities, request.Team)
		} else {
			p := *request.Personality
			p.Aggression, p.Discipline = clamp(p.Aggression, 0, 1), clamp(p.Discipline, 0, 1)
			p.Risk, p.Consistency = clamp(p.Risk, 0, 1), clamp(p.Consistency, 0.05, 1)
			p.Reaction = clamp(p.Reaction, 0.05, 1)
			p.Curiosity = clamp(p.Curiosity, 0, 1)
			world.personalities[request.Team] = p
		}
		world.refreshDriversLocked()
		world.markDirtyLocked()
		return world.personalityFor(request.Team), nil
	})
	world.bus.Handle("world.ai.teamPersonality", func(args []byte) (any, error) {
		var request struct {
			Team int `json:"team"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		_, overridden := world.personalities[request.Team]
		base := personalityForTeam(request.Team)
		if overridden {
			base = world.personalities[request.Team]
		}
		return map[string]any{"personality": base, "effective": world.personalityFor(request.Team), "overridden": overridden}, nil
	})
	world.bus.Handle("world.ai.forgetTeam", func(args []byte) (any, error) {
		// One team forgets everything it learned (records, mistakes, auto routines, repairs).
		var request struct {
			Team int `json:"team"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		delete(world.learning.Teams, request.Team)
		delete(world.learning.Autos, request.Team)
		world.refreshDriversLocked()
		world.markDirtyLocked()
		world.logf("info", "ai", "%d forgot what it had learned", request.Team)
		return nil, nil
	})
	world.bus.Handle("world.ai.redesign", func(args []byte) (any, error) {
		var request struct {
			Team int `json:"team"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Team <= 0 {
			return nil, fmt.Errorf("no team")
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		if stats, ok := world.learning.Teams[request.Team]; ok {
			stats.Design = nil // a fresh build season, not just an unlock
		}
		world.setTeamDesignLocked(request.Team, "", false)
		world.applyPitChangesLocked()
		world.markDirtyLocked()
		return nil, nil
	})
	world.bus.Handle("world.ai.lockDesign", func(args []byte) (any, error) {
		var request struct {
			Team int  `json:"team"`
			Lock bool `json:"lock"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		stats, ok := world.learning.Teams[request.Team]
		if !ok || stats.Design == nil {
			return nil, fmt.Errorf("team %d has not built a robot yet", request.Team)
		}
		stats.Design.Locked = request.Lock
		world.markDirtyLocked()
		return nil, nil
	})
	world.bus.Handle("world.ai.resetLearning", func(_ []byte) (any, error) {
		world.mu.Lock()
		defer world.mu.Unlock()
		world.learning = newLearning()
		world.refreshDriversLocked()
		world.markDirtyLocked()
		world.logf("info", "ai", "Learning reset: every team starts fresh")
		return nil, nil
	})
}

// refreshDriversLocked re-applies personalities and the difficulty to the drive teams on the field.
func (world *World) refreshDriversLocked() {
	for i, robot := range world.robots {
		if agent, ok := world.controllers[i].(*Agent); ok && robot.Team > 0 {
			agent.Personality = world.personalityFor(robot.Team)
		}
		if robot.Team > 0 {
			if _, custom := world.teamSkill[robot.Team]; !custom {
				robot.Skill = world.effectiveSkill(robot.Team, skillForTeam(robot.Team))
			}
		}
	}
}
