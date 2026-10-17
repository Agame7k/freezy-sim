package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/config"
)

// SettingsContributor adds arena settings needed by one simulated device.
type SettingsContributor func(settings map[string]string)

// AddSettingsContributor registers a device whose address the arena must be pointed at.
func (app *App) AddSettingsContributor(contributor SettingsContributor) {
	app.settingsContributors = append(app.settingsContributors, contributor)
}

// DesiredArenaSettings is the set of arena settings the simulator needs for the active profile.
func (app *App) DesiredArenaSettings() map[string]string {
	settings := map[string]string{
		// Never reach out to the internet from the sandbox.
		"tbaDownloadEnabled":    "",
		"tbaPublishingEnabled":  "",
		"nexusEnabled":          "",
		"nexusAutoQueueEnabled": "",
		"useLiteUdpPort":        "",
	}
	for _, contributor := range app.settingsContributors {
		contributor(settings)
	}
	return settings
}

// ConfigureArena points the arena's hardware settings at the simulator's emulated devices.
func (app *App) ConfigureArena() ([]string, error) {
	changed, err := app.Arena.ApplySettings(app.DesiredArenaSettings())
	if err != nil {
		app.Hub.Errorf("setup", "Configuring arena settings failed: %v", err)
		return nil, err
	}
	if len(changed) > 0 {
		app.Hub.Logf("setup", "Updated arena settings: %s", strings.Join(changed, "; "))
	} else {
		app.Hub.Logf("setup", "Arena settings already match the simulator")
	}
	return changed, nil
}

// SeedTeams registers the simulator's team pool in the arena.
func (app *App) SeedTeams() ([]int, error) {
	pool := append([]int{}, app.Config.DS.DefaultTeams[:]...)
	pool = append(pool, app.Config.DS.TeamPool...)
	added, err := app.Arena.AddTeams(pool)
	if err != nil {
		app.Hub.Errorf("setup", "Seeding teams failed: %v", err)
		return nil, err
	}
	if len(added) > 0 {
		app.Hub.Logf("setup", "Registered %d teams in the arena", len(added))
	}
	return added, nil
}

// QuickTestMatch loads the arena's test match and fills it with six teams (the last teams used when none are given).
func (app *App) QuickTestMatch(teams [6]int) error {
	app.testMu.Lock()
	if teams == [6]int{} {
		teams = app.lastTestTeams
	}
	if teams == [6]int{} {
		teams = app.Config.DS.DefaultTeams
	}
	app.fillingTest = time.Now()
	app.testMu.Unlock()
	if _, err := app.Arena.AddTeams(teams[:]); err != nil {
		return err
	}
	// The arena refuses new teams once an event has a qualification schedule; use teams it already has instead.
	if existing, err := app.Arena.ListTeams(); err == nil && len(existing) >= 6 {
		have := map[int]bool{}
		for _, team := range existing {
			have[team] = true
		}
		next := 0
		for i, team := range teams {
			if have[team] {
				continue
			}
			for next < len(existing) && slices.Contains(teams[:], existing[next]) {
				next++
			}
			if next < len(existing) {
				app.Hub.Warnf("setup", "Team %d is not at this event (its schedule is set); using %d instead", team, existing[next])
				teams[i] = existing[next]
				next++
			}
		}
	}
	app.testMu.Lock()
	app.lastTestTeams = teams
	app.testMu.Unlock()
	if err := app.Arena.SendMatchPlay("loadMatch", map[string]int{"MatchId": 0}); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	if err := app.Arena.SubstituteTeams(teams); err != nil {
		return err
	}
	app.Hub.Logf("setup", "Loaded test match with teams %v", teams)
	return nil
}

func (app *App) autoSetup() {
	if !app.Config.Arena.AutoConfigure || app.Config.Arena.Mode != config.ArenaSandbox {
		return
	}
	if state := app.Launcher.Status().State; state == "external" {
		// Never silently reconfigure an arena the simulator did not launch.
		app.Hub.Logf("setup", "Skipping auto-configuration of an externally started arena")
		return
	}
	if _, err := app.ConfigureArena(); err != nil {
		return
	}
	if _, err := app.SeedTeams(); err != nil {
		return
	}
	if teams := app.Arena.Teams(); teams == [6]int{} {
		if err := app.QuickTestMatch([6]int{}); err != nil {
			app.Hub.Warnf("setup", "Could not load the default test match: %v", err)
		}
	}
}

// refillTestMatch puts teams back into an empty test match (the arena loads one after a test match is committed or
// discarded), so the field is always ready to play another match.
func (app *App) refillTestMatch(load arena.MatchLoad) {
	if !app.Config.Arena.AutoConfigure || load.Match == nil || load.Match.Type != 0 || app.Arena.Teams() != [6]int{} {
		return
	}
	if state := app.Launcher.Status().State; state == "external" {
		return // never fill matches on an arena the simulator did not launch
	}
	app.testMu.Lock()
	recent := time.Since(app.fillingTest) < 5*time.Second
	app.testMu.Unlock()
	if recent {
		return // this load is our own QuickTestMatch on its way to substituting the teams
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		app.testMu.Lock()
		busy := time.Since(app.fillingTest) < 5*time.Second
		app.testMu.Unlock()
		if busy || app.Arena.Teams() != [6]int{} {
			return // someone else (a scenario, the GUI) is already loading a test match
		}
		if err := app.QuickTestMatch([6]int{}); err != nil {
			app.Hub.Warnf("setup", "Could not refill the test match: %v", err)
		}
	}()
}

func (app *App) registerSetupCommands() {
	app.Arena.OnConnect(app.autoSetup)
	app.Arena.OnMatchLoad(app.refillTestMatch)
	app.Hub.Handle("arena.configure", func(_ []byte) (any, error) { return app.ConfigureArena() })
	app.Hub.Handle("arena.desiredSettings", func(_ []byte) (any, error) { return app.DesiredArenaSettings(), nil })
	app.Hub.Handle("arena.readSettings", func(_ []byte) (any, error) { return app.Arena.ReadSettings() })
	app.Hub.Handle("arena.seedTeams", func(_ []byte) (any, error) { return app.SeedTeams() })
	app.Hub.Handle("arena.teams", func(_ []byte) (any, error) { return app.Arena.ListTeams() })
	app.Hub.Handle("match.quickTest", func(args []byte) (any, error) {
		var request struct {
			Teams [6]int `json:"teams"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &request); err != nil {
				return nil, err
			}
		}
		return nil, app.QuickTestMatch(request.Teams)
	})
	app.Hub.Handle("match.substitute", func(args []byte) (any, error) {
		var request struct {
			Teams [6]int `json:"teams"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if _, err := app.Arena.AddTeams(request.Teams[:]); err != nil {
			return nil, err
		}
		return nil, app.Arena.SubstituteTeams(request.Teams)
	})
	app.Hub.Handle("arena.applySettings", func(args []byte) (any, error) {
		var changes map[string]string
		if err := json.Unmarshal(args, &changes); err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			return nil, fmt.Errorf("no settings given")
		}
		return app.Arena.ApplySettings(changes)
	})
}
