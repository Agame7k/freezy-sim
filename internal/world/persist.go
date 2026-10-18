package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agame7k/freezy-sim/internal/safe"
)

// SavedState is what the world remembers between sessions (run/state.json): each team's robot design and drive-team
// skill, the per-station plan overrides, and the human player modes.
type SavedState struct {
	Version        int                 `json:"version"`
	TeamArchetypes map[int]string      `json:"teamArchetypes"`
	TeamSkill      map[int]float64     `json:"teamSkill"`
	Overrides      [6]RobotPlan        `json:"overrides"`
	HumanModes     [2]string           `json:"humanModes"`
	HumanAccuracy  [2]float64          `json:"humanAccuracy"`
	Difficulty     string              `json:"difficulty"`
	PerfectInfo    bool                `json:"perfectInfo"`
	StockDesigns   bool                `json:"stockDesigns"` // team-built robots turned off
	NoTipping      bool                `json:"noTipping"`    // robots never tip over
	Personalities  map[int]Personality `json:"personalities"`
	Learning       *Learning           `json:"learning"`
}

const savedStateVersion = 1

// LoadState restores saved state (if the file exists) and saves future changes to the same file.
func (world *World) LoadState(path string) error {
	world.mu.Lock()
	world.statePath = path
	world.mu.Unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state SavedState
	if err := json.Unmarshal(data, &state); err != nil {
		// Keep the damaged file: the next save would otherwise overwrite every team's learned history for good.
		backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
		if renameErr := os.Rename(path, backup); renameErr == nil {
			return fmt.Errorf("%w (the damaged file was kept as %s)", err, backup)
		}
		return err
	}
	world.mu.Lock()
	for team, name := range state.TeamArchetypes {
		world.teamArch[team] = archetypeByName(name)
	}
	for team, skill := range state.TeamSkill {
		if skill > 0 {
			world.teamSkill[team] = skill
		}
	}
	world.overrides = state.Overrides
	for i, mode := range state.HumanModes {
		if mode != "" {
			world.outposts[i].HumanMode = mode
		}
		if state.HumanAccuracy[i] > 0 {
			world.outposts[i].Accuracy = state.HumanAccuracy[i]
		}
	}
	if state.Difficulty != "" {
		world.difficulty = state.Difficulty
	}
	world.perfectInfo = state.PerfectInfo
	world.teamDesigns = !state.StockDesigns
	world.tipping = !state.NoTipping
	for team, personality := range state.Personalities {
		world.personalities[team] = personality
	}
	if state.Learning != nil && state.Learning.Teams != nil {
		world.learning = *state.Learning
		if world.learning.Plans == nil {
			world.learning.Plans = map[string]map[string]*PlanRecord{}
		}
		if world.learning.Autos == nil {
			world.learning.Autos = map[int]map[string]*AutoProgram{}
		}
	}
	// Teams already on the field pick up their saved designs.
	for _, robot := range world.robots {
		if arch, ok := world.teamArch[robot.Team]; ok && robot.Team > 0 {
			robot.Arch = arch
		}
		if skill, ok := world.teamSkill[robot.Team]; ok && robot.Team > 0 {
			robot.Skill = skill
		}
	}
	world.mu.Unlock()
	world.bus.Logf("world", "Restored %d team designs and plan overrides from %s", len(state.TeamArchetypes), path)
	return nil
}

// markDirtyLocked schedules a save of the remembered state (debounced; the world lock must be held).
func (world *World) markDirtyLocked() {
	if world.statePath == "" || world.saveTimer != nil {
		return
	}
	world.saveTimer = time.AfterFunc(time.Second, world.saveState)
}

// savedStateLocked builds the state to remember (the world lock must be held).
func (world *World) savedStateLocked() SavedState {
	learning := world.learning
	state := SavedState{Version: savedStateVersion, TeamArchetypes: map[int]string{}, TeamSkill: map[int]float64{},
		Overrides: world.overrides, Difficulty: world.difficulty, PerfectInfo: world.perfectInfo, StockDesigns: !world.teamDesigns, NoTipping: !world.tipping,
		Personalities: map[int]Personality{}, Learning: &learning}
	for team, personality := range world.personalities {
		state.Personalities[team] = personality
	}
	for team, arch := range world.teamArch {
		state.TeamArchetypes[team] = arch.Name
	}
	for team, skill := range world.teamSkill {
		state.TeamSkill[team] = skill
	}
	for i, outpost := range world.outposts {
		state.HumanModes[i] = outpost.HumanMode
		state.HumanAccuracy[i] = outpost.Accuracy
	}
	return state
}

// ExportState is a deep copy of everything the world remembers (for training a copy of the teams offline).
func (world *World) ExportState() SavedState {
	world.mu.Lock()
	data, _ := json.Marshal(world.savedStateLocked())
	world.mu.Unlock()
	var state SavedState
	_ = json.Unmarshal(data, &state)
	return state
}

// applyState loads designs, skills, personalities, difficulty and learning (not the per-station overrides).
func (world *World) applyState(state SavedState) {
	world.mu.Lock()
	defer world.mu.Unlock()
	world.teamDesigns = !state.StockDesigns
	world.tipping = !state.NoTipping
	for team, name := range state.TeamArchetypes {
		world.teamArch[team] = archetypeByName(name)
	}
	for team, skill := range state.TeamSkill {
		world.teamSkill[team] = skill
	}
	for team, personality := range state.Personalities {
		world.personalities[team] = personality
	}
	if state.Difficulty != "" {
		world.difficulty = state.Difficulty
	}
	if state.Learning != nil {
		world.learning = *state.Learning
		if world.learning.Teams == nil {
			world.learning = newLearning()
		}
		if world.learning.Plans == nil {
			world.learning.Plans = map[string]map[string]*PlanRecord{}
		}
		if world.learning.Autos == nil {
			world.learning.Autos = map[int]map[string]*AutoProgram{}
		}
	}
}

// MergeLearning takes what the given teams learned elsewhere (offline training) and the alliance plan records,
// keeping everything else the world knows.
func (world *World) MergeLearning(learned Learning, teams []int) {
	world.mu.Lock()
	defer world.mu.Unlock()
	for _, team := range teams {
		if stats, ok := learned.Teams[team]; ok {
			world.learning.Teams[team] = stats
		}
		if library, ok := learned.Autos[team]; ok {
			world.learning.Autos[team] = library
		}
	}
	for mix, records := range learned.Plans {
		world.learning.Plans[mix] = records
	}
	world.refreshDriversLocked()
	world.markDirtyLocked()
}

func (world *World) saveState() {
	world.mu.Lock()
	world.saveTimer = nil
	path := world.statePath
	data, _ := json.MarshalIndent(world.savedStateLocked(), "", "  ") // marshal under the lock: the learning maps are live
	world.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		world.bus.Warnf("world", "Could not save robot designs: %v", err)
		return
	}
	if err := safe.WriteFile(path, data, 0o644); err != nil {
		world.bus.Warnf("world", "Could not save robot designs: %v", err)
	}
}
