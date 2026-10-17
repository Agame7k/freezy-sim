package app

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/referee"
	"github.com/Agame7k/freezy-sim/internal/scouting"
)

// The second half of an event: scouting the qualification matches, alliance selection on the arena's own selection
// screen, and the playoff bracket with alliance timeouts. Everything goes through the arena's real web pages and
// match-play socket, so it exercises the same paths a scorekeeper and an emcee use.

// playedMatch is a schedule row with its committed result (/api/matches/{type}).
type playedMatch struct {
	scheduledMatch
	PlayoffRedAlliance  int `json:"PlayoffRedAlliance"`
	PlayoffBlueAlliance int `json:"PlayoffBlueAlliance"`
	Result              *struct {
		RedSummary, BlueSummary *struct {
			AutoFuelPoints, AutoTowerPoints, TeleopTowerPoints, MatchPoints int
		}
	} `json:"Result"`
}

// Arena match statuses (game.MatchStatus).
const (
	statusScheduled = 0
	statusRedWon    = 2
	statusBlueWon   = 3
	statusTie       = 4
)

func (match playedMatch) teamsKnown() bool {
	for _, team := range [6]int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
		if team <= 0 {
			return false
		}
	}
	return true
}

// scoutEvent builds the pick list from the committed qualification matches (the arena's alliance scores) and the
// referees' per-robot match reports (what the scouts saw), and writes the scouting sheet as a CSV.
func (app *App) scoutEvent() ([]scouting.TeamRating, string, error) {
	var matches []playedMatch
	if err := app.Arena.GetJSON("/api/matches/qualification", &matches); err != nil {
		return nil, "", err
	}
	var scores []scouting.MatchScore
	names := map[int]string{}
	for _, match := range matches {
		if match.Result == nil || match.Result.RedSummary == nil || match.Result.BlueSummary == nil {
			continue
		}
		red, blue := match.Result.RedSummary, match.Result.BlueSummary
		scores = append(scores, scouting.MatchScore{Name: match.ShortName,
			Red: [3]int{match.Red1, match.Red2, match.Red3}, Blue: [3]int{match.Blue1, match.Blue2, match.Blue3},
			RedPoints: red.MatchPoints, BluePoints: blue.MatchPoints,
			RedAuto: red.AutoFuelPoints + red.AutoTowerPoints, BlueAuto: blue.AutoFuelPoints + blue.AutoTowerPoints,
			RedTower: red.AutoTowerPoints + red.TeleopTowerPoints, BlueTower: blue.AutoTowerPoints + blue.TeleopTowerPoints})
		names[match.Id] = match.ShortName
	}
	observations := scoutReports(filepath.Join("run", "reports"), names)

	ranks := map[int]int{}
	ranked, _ := app.rankedTeams()
	for i, team := range ranked {
		ranks[team] = i + 1
	}
	ratings := scouting.Rate(scores, observations, ranks)
	path, err := writeScoutingCSV(filepath.Join("run", "reports"), observations, ratings)
	if err != nil {
		return ratings, "", err
	}
	app.Hub.Publish("scouting.ratings", ratings)
	app.Hub.Logf("director", "Scouted %d matches (%d robot rows); scouting sheet %s", len(scores), len(observations), path)
	return ratings, path, nil
}

// scoutReports turns the referees' saved match reports into scouting rows for the given matches (newest finished
// report per match). Scouts are human: FUEL counts are off by a ball or two now and then.
func scoutReports(dir string, matchNames map[int]string) []scouting.Observation {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	latest := map[int]referee.Report{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var report referee.Report
		if json.Unmarshal(data, &report) != nil || report.Aborted || !report.Finished {
			continue
		}
		if _, ok := matchNames[report.MatchId]; !ok {
			continue
		}
		if previous, ok := latest[report.MatchId]; !ok || report.StartedAt.After(previous.StartedAt) {
			latest[report.MatchId] = report
		}
	}
	ids := make([]int, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var out []scouting.Observation
	for _, id := range ids {
		report := latest[id]
		rng := rand.New(rand.NewSource(int64(id)))
		for i, robot := range report.Robots {
			if i >= 6 {
				break
			}
			scored := robot.Scored
			if scored > 0 {
				scored = max(0, scored+int(math.Round(rng.NormFloat64()*0.08*float64(scored))))
			}
			out = append(out, scouting.Observation{Match: matchNames[id], Team: report.Teams[i], Station: robot.Station,
				Present: robot.Present, Scored: scored, Passes: robot.Passes, AutoClimb: report.AutoTower[i],
				EndClimb: report.EndTower[i], Faults: robot.Faults})
		}
	}
	return out
}

func writeScoutingCSV(dir string, observations []scouting.Observation, ratings []scouting.TeamRating) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(dir, "scouting-"+stamp+".csv")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	writer := csv.NewWriter(file)
	_ = writer.Write([]string{"match", "team", "station", "present", "fuelScored", "passes", "autoTower", "endTower", "faults"})
	for _, o := range observations {
		_ = writer.Write([]string{o.Match, strconv.Itoa(o.Team), o.Station, strconv.FormatBool(o.Present),
			strconv.Itoa(o.Scored), strconv.Itoa(o.Passes), strconv.Itoa(o.AutoClimb), strconv.Itoa(o.EndClimb),
			strings.Join(o.Faults, "; ")})
	}
	writer.Flush()
	if err := file.Close(); err != nil {
		return "", err
	}
	// The pick list sits next to it.
	pickPath := filepath.Join(dir, "picklist-"+stamp+".csv")
	file, err = os.Create(pickPath)
	if err != nil {
		return path, err
	}
	writer = csv.NewWriter(file)
	_ = writer.Write([]string{"pick", "team", "rank", "played", "opr", "autoOpr", "towerOpr", "avgFuel", "avgClimbPts",
		"climbRate", "reliability", "pickValue"})
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	for i, r := range ratings {
		_ = writer.Write([]string{strconv.Itoa(i + 1), strconv.Itoa(r.Team), strconv.Itoa(r.Rank), strconv.Itoa(r.Played),
			f(r.OPR), f(r.AutoOPR), f(r.TowerOPR), f(r.AvgScored), f(r.AvgClimbPts), f(r.ClimbRate), f(r.Reliability), f(r.PickValue)})
	}
	writer.Flush()
	return path, file.Close()
}

// runSelection scouts the event, then runs alliance selection on the arena's selection page, one pick at a time
// (shown on the audience display), and finalizes it to create the playoff bracket.
func (d *director) runSelection() {
	app := d.app
	var alliances []json.RawMessage
	if err := app.Arena.GetJSON("/api/alliances", &alliances); err == nil && len(alliances) > 0 {
		app.Hub.Logf("director", "Alliance selection is already finalized (%d alliances); skipping it", len(alliances))
		return
	}
	d.setPhase("Alliance selection", "scouting the qualification matches")
	ratings, csvPath, err := app.scoutEvent()
	if err != nil {
		d.anomaly("scouting: %v", err)
		if ratings == nil {
			return
		}
	}
	byTeam := map[int]scouting.TeamRating{}
	for _, rating := range ratings {
		byTeam[rating.Team] = rating
	}
	d.mu.Lock()
	d.state.PickList, d.state.Scouting = ratings, csvPath
	d.mu.Unlock()

	settings, err := app.Arena.ReadSettings()
	if err != nil {
		d.anomaly("alliance selection: could not read the event settings: %v", err)
		return
	}
	numAlliances, _ := strconv.Atoi(settings.Get("numPlayoffAlliances"))
	cfg := scouting.SelectionConfig{Alliances: numAlliances, Round2Order: settings.Get("selectionRound2Order"),
		Round3Order: settings.Get("selectionRound3Order"), DeclineChance: d.options.DeclineChance}
	ranked, err := app.rankedTeams()
	if err != nil || len(ranked) == 0 {
		d.anomaly("alliance selection: no rankings (play the qualification matches first): %v", err)
		return
	}
	// A small event runs fewer alliances (the bracket takes 4 to 8).
	perAlliance := 3
	if cfg.Round3Order != "" {
		perAlliance = 4
	}
	if fit := min(8, len(ranked)/perAlliance); fit < cfg.Alliances && fit >= 4 {
		if _, err := app.Arena.ApplySettings(map[string]string{"numPlayoffAlliances": strconv.Itoa(fit)}); err != nil {
			d.anomaly("alliance selection: could not reduce the alliances to %d for %d teams: %v", fit, len(ranked), err)
			return
		}
		app.Hub.Logf("director", "Only %d ranked teams: running %d playoff alliances instead of %d", len(ranked), fit, cfg.Alliances)
		cfg.Alliances = fit
	}
	selection, err := scouting.Select(ranked, byTeam, cfg, rand.New(rand.NewSource(time.Now().UnixNano())))
	if err != nil {
		d.anomaly("alliance selection: %v", err)
		return
	}

	d.setPhase("Alliance selection", "on stage")
	if err := app.Arena.PostForm("/alliance_selection/start", nil); err != nil {
		d.anomaly("alliance selection: the arena refused to start it: %v", err)
		return
	}
	_ = d.dispatch("match.audienceDisplay", map[string]string{"mode": "allianceSelection"})
	grid := make([][]int, cfg.Alliances)
	for i := range grid {
		grid[i] = make([]int, len(selection.Alliances[i]))
	}
	pause := time.Duration(d.options.PickSeconds * float64(time.Second))
	for _, step := range selection.Steps {
		if d.stopped() {
			return
		}
		time.Sleep(pause)
		app.Hub.Logf("director", "%s", step.Text)
		d.mu.Lock()
		d.state.Selection = append(d.state.Selection, step)
		d.mu.Unlock()
		d.publish()
		if step.Kind == "invite-declined" {
			if err := app.Arena.PostForm("/alliance_selection/decline", url.Values{"teamId": {strconv.Itoa(step.Team)}}); err != nil {
				d.anomaly("alliance selection: recording %d's decline failed: %v", step.Team, err)
			}
			continue
		}
		grid[step.Alliance-1][step.Slot] = step.Team
		form := url.Values{}
		for i, members := range grid {
			for j, team := range members {
				value := ""
				if team > 0 {
					value = strconv.Itoa(team)
				}
				form.Set(fmt.Sprintf("selection%d_%d", i, j), value)
			}
		}
		if err := app.Arena.PostForm("/alliance_selection", form); err != nil {
			d.anomaly("alliance selection: the arena rejected %q: %v", step.Text, err)
			return
		}
	}
	time.Sleep(pause)
	start := time.Now().Add(10 * time.Minute).Format("2006-01-02 03:04:05 PM")
	if err := app.Arena.PostForm("/alliance_selection/finalize", url.Values{"startTime": {start}}); err != nil {
		d.anomaly("alliance selection: finalizing failed: %v", err)
		return
	}
	// The arena answers a refused finalize with the selection page, not an error status; check it took.
	if err := app.Arena.GetJSON("/api/alliances", &alliances); err != nil || len(alliances) != cfg.Alliances {
		d.anomaly("alliance selection: finalize did not create %d alliances (got %d)", cfg.Alliances, len(alliances))
		return
	}
	d.mu.Lock()
	d.state.Alliances = selection.Alliances
	d.mu.Unlock()
	d.publish()
	_ = d.dispatch("match.audienceDisplay", map[string]string{"mode": "bracket"})
	app.Hub.Logf("director", "Alliance selection finalized: %v", selection.Alliances)
}

// runPlayoffs plays the bracket as it unfolds: the arena fills later matches as earlier ones are committed, so the
// schedule is re-read after every match. Before a match, an alliance that still has its timeout may call it; the
// next match is then loaded during the timeout, as a scorekeeper would.
func (d *director) runPlayoffs() {
	app := d.app
	timeoutUsed := map[int]bool{}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	stuck := 0
	for !d.stopped() {
		var schedule []playedMatch
		if err := app.Arena.GetJSON("/api/matches/playoff", &schedule); err != nil {
			d.anomaly("could not read the playoff schedule: %v", err)
			return
		}
		if len(schedule) == 0 {
			d.anomaly("no playoff matches (finalize alliance selection first)")
			return
		}
		sort.Slice(schedule, func(i, j int) bool { return schedule[i].TypeOrder < schedule[j].TypeOrder })
		var next *playedMatch
		pending := 0
		for i := range schedule {
			if schedule[i].Status != statusScheduled {
				continue
			}
			pending++
			if next == nil && schedule[i].teamsKnown() {
				next = &schedule[i]
			}
		}
		if pending == 0 {
			d.finishPlayoffs(schedule)
			return
		}
		if next == nil {
			// Matches remain but none has both alliances yet: the bracket did not advance.
			if stuck++; stuck > 3 {
				d.anomaly("playoff bracket stuck: %d matches scheduled but none has both alliances", pending)
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}
		stuck = 0
		d.mu.Lock()
		d.state.Total = len(d.state.Matches) + pending
		d.mu.Unlock()

		var beforeLoad func()
		if d.options.TimeoutSeconds > 0 {
			for _, alliance := range []int{next.PlayoffRedAlliance, next.PlayoffBlueAlliance} {
				if alliance > 0 && !timeoutUsed[alliance] && rng.Float64() < d.options.TimeoutChance {
					timeoutUsed[alliance] = true
					match, caller := *next, alliance
					beforeLoad = func() { d.callTimeout(match.ShortName, caller) }
					break
				}
			}
		}
		result := d.playMatch(next.scheduledMatch, beforeLoad)
		if result.Problem != "" {
			return // playMatch logged it; replaying the match blindly could commit it twice
		}
		// The arena records the result and advances the bracket after the commit; reading the schedule before it
		// does would play the same match again.
		committed := d.waitFor(15*time.Second, func() bool {
			var now []playedMatch
			if app.Arena.GetJSON("/api/matches/playoff", &now) != nil {
				return false
			}
			for _, match := range now {
				if match.Id == next.Id {
					return match.Status != statusScheduled
				}
			}
			return true // hidden now (e.g. an unneeded final)
		})
		if !committed {
			d.anomaly("%s: the arena did not record the committed result within 15 s", next.ShortName)
			return
		}
	}
}

// callTimeout starts an alliance timeout on the arena; playMatch then loads the next match while it runs and waits
// for it to end before starting.
func (d *director) callTimeout(nextMatch string, alliance int) {
	text := fmt.Sprintf("Alliance %d calls its timeout before %s (%d s)", alliance, nextMatch, d.options.TimeoutSeconds)
	d.app.Hub.Logf("director", "%s", text)
	d.mu.Lock()
	d.state.Timeouts = append(d.state.Timeouts, text)
	d.mu.Unlock()
	d.setPhase(nextMatch, fmt.Sprintf("alliance %d timeout", alliance))
	err := d.app.Arena.SendMatchPlay("startTimeout", map[string]any{
		"Description": fmt.Sprintf("Alliance %d Timeout", alliance), "NextMatchName": nextMatch,
		"DurationSec": d.options.TimeoutSeconds})
	if err != nil {
		d.anomaly("alliance %d timeout: %v", alliance, err)
		return
	}
	if !d.waitFor(5*time.Second, func() bool { return d.matchState() == arena.TimeoutActive }) {
		d.anomaly("alliance %d timeout: the arena never entered the timeout", alliance)
		return
	}
	time.Sleep(3 * time.Second) // the field crew starts resetting; the scorekeeper loads the next match mid-timeout
}

// finishPlayoffs names the winner: the alliance that won the last match played (the deciding final).
func (d *director) finishPlayoffs(schedule []playedMatch) {
	var last *playedMatch
	for i := range schedule {
		if schedule[i].Status == statusRedWon || schedule[i].Status == statusBlueWon {
			last = &schedule[i]
		}
	}
	if last == nil {
		d.anomaly("playoffs finished without a decided match")
		return
	}
	alliance := last.PlayoffRedAlliance
	if last.Status == statusBlueWon {
		alliance = last.PlayoffBlueAlliance
	}
	d.mu.Lock()
	known := len(d.state.Alliances) > 0
	d.mu.Unlock()
	if !known {
		// A playoff-only run: the alliances were picked earlier, so read them from the arena.
		var alliances []struct {
			Id      int   `json:"Id"`
			TeamIds []int `json:"TeamIds"`
		}
		if d.app.Arena.GetJSON("/api/alliances", &alliances) == nil {
			sort.Slice(alliances, func(i, j int) bool { return alliances[i].Id < alliances[j].Id })
			d.mu.Lock()
			for _, a := range alliances {
				d.state.Alliances = append(d.state.Alliances, a.TeamIds)
			}
			d.mu.Unlock()
		}
	}
	d.mu.Lock()
	var teams []int
	if alliance > 0 && alliance <= len(d.state.Alliances) {
		teams = d.state.Alliances[alliance-1]
	}
	d.state.Winner = fmt.Sprintf("Alliance %d %v (won %s)", alliance, teams, last.ShortName)
	winner := d.state.Winner
	d.mu.Unlock()
	d.app.Hub.Logf("director", "Event winner: %s", winner)
	_ = d.dispatch("match.audienceDisplay", map[string]string{"mode": "bracket"})
}

// rankedTeams returns the qualification ranking, best first.
func (app *App) rankedTeams() ([]int, error) {
	var response struct {
		Rankings []struct {
			TeamId int `json:"TeamId"`
			Rank   int `json:"Rank"`
		} `json:"Rankings"`
	}
	if err := app.Arena.GetJSON("/api/rankings", &response); err != nil {
		return nil, err
	}
	rankings := response.Rankings
	sort.Slice(rankings, func(i, j int) bool { return rankings[i].Rank < rankings[j].Rank })
	ranked := make([]int, len(rankings))
	for i, ranking := range rankings {
		ranked[i] = ranking.TeamId
	}
	return ranked, nil
}
