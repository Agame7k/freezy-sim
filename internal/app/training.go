package app

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/world"
	"github.com/Agame7k/freezy-sim/web"
)

// Pre-season training: the event's teams play a block of practice matches offline (the AI benchmark, much faster than
// real time) starting from everything they know, and bring what they learned back — tuned autos, shot ranges, which
// roles work, the mistakes to avoid. A fresh event then starts with teams that look like week-3 teams, not week-0.

// TrainingState is published on ai.training.
type TrainingState struct {
	Running bool                `json:"running"`
	Done    int                 `json:"done"`
	Total   int                 `json:"total"`
	Teams   int                 `json:"teams"`
	Started time.Time           `json:"started"`
	Summary *world.BenchSummary `json:"summary,omitempty"`
	Error   string              `json:"error,omitempty"`
}

func (app *App) registerTraining() {
	var mu sync.Mutex
	state := TrainingState{}
	publish := func() {
		mu.Lock()
		copied := state
		mu.Unlock()
		app.Hub.Publish("ai.training", copied)
	}
	app.Hub.Handle("world.ai.train", func(args []byte) (any, error) {
		var request struct {
			Matches int   `json:"matches"`
			Teams   []int `json:"teams"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &request); err != nil {
				return nil, err
			}
		}
		if request.Matches <= 0 {
			request.Matches = 30
		}
		mu.Lock()
		if state.Running {
			mu.Unlock()
			return nil, fmt.Errorf("training is already running")
		}
		teams := request.Teams
		if len(teams) == 0 {
			// The event's teams (registered in the arena), or the simulator's own pool.
			if registered, err := app.Arena.ListTeams(); err == nil && len(registered) >= 6 {
				teams = registered
			} else {
				teams = append(append([]int{}, app.Config.DS.DefaultTeams[:]...), app.Config.DS.TeamPool...)
			}
		}
		if len(teams) < 6 {
			mu.Unlock()
			return nil, fmt.Errorf("need at least 6 teams to train (have %d)", len(teams))
		}
		state = TrainingState{Running: true, Total: request.Matches, Teams: len(teams), Started: time.Now()}
		mu.Unlock()
		publish()
		go func() {
			defer safe.Recover(app.Hub, "training")
			layout, err := world.LoadLayout(web.Files)
			if err != nil {
				mu.Lock()
				state.Running, state.Error = false, err.Error()
				mu.Unlock()
				publish()
				return
			}
			saved := app.World.ExportState()
			// Shuffle so every team plays with and against everyone.
			pool := append([]int{}, teams...)
			rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			app.Hub.Logf("ai", "Pre-season training: %d teams, %d practice matches", len(pool), request.Matches)
			report := world.RunBench(layout, world.BenchOptions{Matches: request.Matches, Seed: rand.Int63(), Teams: pool,
				State: &saved}, func(world.BenchMatch) {
				mu.Lock()
				state.Done++
				mu.Unlock()
				publish()
			})
			if report.Learning != nil {
				app.World.MergeLearning(*report.Learning, teams)
			}
			mu.Lock()
			state.Running = false
			state.Summary = &report.Summary
			mu.Unlock()
			publish()
			app.Hub.Logf("ai", "Pre-season training done in %.0f s: average score %.0f, %.0f AUTO FUEL per alliance",
				time.Since(state.Started).Seconds(), report.Summary.AvgScore, report.Summary.AutoFuel)
		}()
		return nil, nil
	})
	publish()
}
