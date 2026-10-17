// Package referee automates the human officials: it reads tower climbs off the simulated field and enters them through
// the arena's real scoring panels, calls pinning fouls, optionally commits/posts matches like a scorekeeper, and writes a
// report for every match.
package referee

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/world"
)

const (
	assessmentDelay = 3 * time.Second // the arena turns the hub lights white for scoring assessment, then off
)

var stations = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

// Settings are the user-controllable referee options.
type Settings struct {
	Enabled    bool `json:"enabled"`
	AutoCommit bool `json:"autoCommit"` // commit the scoring panels after the end-of-match assessment
	AutoPost   bool `json:"autoPost"`   // also commit & post the match (scorekeeper)
	CallFouls  bool `json:"callFouls"`
	// FieldSignals has the head referee turn the field purple (safe for field staff) then green after each match.
	FieldSignals bool `json:"fieldSignals"`
}

// Event is one line in a match report.
type Event struct {
	MatchTime float64 `json:"matchTime"`
	Wall      string  `json:"wall"`
	Kind      string  `json:"kind"`
	Station   string  `json:"station,omitempty"`
	Team      int     `json:"team,omitempty"`
	Text      string  `json:"text"`
}

// Report is the record of one match.
type Report struct {
	File      string               `json:"file"`
	Match     string               `json:"match"`
	MatchId   int                  `json:"matchId"`
	StartedAt time.Time            `json:"startedAt"`
	Teams     [6]int               `json:"teams"`
	Plans     [2]string            `json:"plans"`
	Robots    []world.RobotSummary `json:"robots"`
	AutoTower [6]int               `json:"autoTower"`
	EndTower  [6]int               `json:"endTower"`
	Score     json.RawMessage      `json:"score"`
	RedScore  int                  `json:"redScore"`
	BlueScore int                  `json:"blueScore"`
	Aborted   bool                 `json:"aborted"`
	Events    []Event              `json:"events"`
	Finished  bool                 `json:"finished"`
}

// ReportSummary is a row in the report list.
type ReportSummary struct {
	File      string    `json:"file"`
	Match     string    `json:"match"`
	StartedAt time.Time `json:"startedAt"`
	RedScore  int       `json:"redScore"`
	BlueScore int       `json:"blueScore"`
	Events    int       `json:"events"`
	Aborted   bool      `json:"aborted"`
}

type Referee struct {
	hub   *bus.Hub
	arena *arena.Client
	world *world.World
	dir   string

	panels   [2]*arena.Socket // red, blue scoring panels
	refPanel *arena.Socket    // head referee panel (fouls with team and rule, cards, field signals)
	Head     *HeadRef
	fouls    chan world.Infraction

	mu         sync.Mutex
	settings   Settings
	report     *Report
	reports    []ReportSummary
	lastState  arena.MatchState
	onFinished []func(refereeing, aborted bool)
}

// OnFinished registers a callback for when the referees have finished scoring a match (towers entered, fouls in,
// scoring panels committed), before anyone commits the match.
func (ref *Referee) OnFinished(fn func(refereeing, aborted bool)) {
	ref.mu.Lock()
	ref.onFinished = append(ref.onFinished, fn)
	ref.mu.Unlock()
}

// Settings returns the referee options.
func (ref *Referee) Settings() Settings {
	ref.mu.Lock()
	defer ref.mu.Unlock()
	return ref.settings
}

func New(hub *bus.Hub, arenaClient *arena.Client, w *world.World, reportDir string) *Referee {
	ref := &Referee{
		hub: hub, arena: arenaClient, world: w, dir: reportDir,
		settings: Settings{Enabled: true, AutoCommit: true, CallFouls: true, FieldSignals: true},
		fouls:    make(chan world.Infraction, 64),
	}
	ref.panels[0] = arenaClient.NewSocket("redScoring", "/panels/scoring/red/websocket", nil)
	ref.panels[1] = arenaClient.NewSocket("blueScoring", "/panels/scoring/blue/websocket", nil)
	ref.refPanel = arenaClient.NewSocket("refereePanel", "/panels/referee/websocket", nil)
	ref.Head = newHeadRef(hub, filepath.Dir(reportDir))
	arenaClient.OnMatchState(ref.onMatchState)
	hub.OnEvent(ref.onEvent)
	ref.loadReportList()
	ref.registerCommands()
	return ref
}

// Run connects the scoring panels and starts the foul watcher.
func (ref *Referee) Run() {
	go ref.panels[0].Run()
	go ref.panels[1].Run()
	go ref.refPanel.Run()
	safe.Go(ref.hub, "referee", ref.enterFouls)
	safe.Go(ref.hub, "referee", ref.watchFouls)
	ref.publish()
}

// Panel returns the alliance's scoring panel socket (0 red, 1 blue).
func (ref *Referee) send(alliance int, command string, data any) error {
	return ref.panels[alliance].Send(command, data)
}

// AddFoul enters a foul committed by the given alliance (chaos injection and manual calls; no rule attached).
func (ref *Referee) AddFoul(alliance int, major bool, reason, station string, team int) {
	ref.queueFoul(world.Infraction{Alliance: alliance, Major: major, Reason: reason, Station: station, Team: team})
}

func (ref *Referee) queueFoul(call world.Infraction) {
	select {
	case ref.fouls <- call:
	default:
		ref.hub.Warnf("referee", "Foul queue full; dropped: %s", call.Reason)
	}
}

// enterFouls enters queued fouls one at a time: the foul through the head referee panel (falling back to the
// alliance's scoring panel), then, once the arena lists it, the offending team and the rule.
func (ref *Referee) enterFouls() {
	for call := range ref.fouls {
		name := []string{"red", "blue"}[call.Alliance]
		before := ref.foulCount(call.Alliance)
		args := map[string]any{"Alliance": name, "IsMajor": call.Major}
		viaReferee := ref.refPanel.Connected()
		var err error
		if viaReferee {
			err = ref.refPanel.Send("addFoul", args)
		} else {
			err = ref.send(call.Alliance, "addFoul", args)
		}
		if err != nil {
			ref.hub.Warnf("referee", "Could not enter foul: %v", err)
			continue
		}
		kind := "minor"
		if call.Major {
			kind = "MAJOR"
		}
		rule := ""
		if call.Rule != "" {
			rule = call.Rule + " "
		}
		ref.record("foul", call.Station, call.Team, fmt.Sprintf("%s%s foul on %s: %s", rule, kind, strings.ToUpper(name),
			call.Reason))
		if !viaReferee || (call.Team == 0 && call.RuleId == 0) {
			continue
		}
		index := -1
		for wait := 0; wait < 30 && index < 0; wait++ {
			if count := ref.foulCount(call.Alliance); count > before {
				index = count - 1
			} else {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if index < 0 {
			continue
		}
		if call.Team > 0 {
			_ = ref.refPanel.Send("updateFoulTeam", map[string]any{"Alliance": name, "Index": index, "TeamId": call.Team})
		}
		if call.RuleId > 0 {
			_ = ref.refPanel.Send("updateFoulRule", map[string]any{"Alliance": name, "Index": index, "RuleId": call.RuleId})
		}
	}
}

// foulCount is how many fouls the arena lists for an alliance in the realtime score.
func (ref *Referee) foulCount(alliance int) int {
	var score struct {
		Red struct {
			Score struct{ Fouls []json.RawMessage }
		}
		Blue struct {
			Score struct{ Fouls []json.RawMessage }
		}
	}
	_ = json.Unmarshal(ref.hub.Get("arena.realtimeScore"), &score)
	if alliance == 0 {
		return len(score.Red.Score.Fouls)
	}
	return len(score.Blue.Score.Fouls)
}

func (ref *Referee) onMatchState(old, new arena.MatchState) {
	ref.mu.Lock()
	enabled := ref.settings.Enabled
	ref.lastState = new
	ref.mu.Unlock()
	switch {
	case new == arena.AutoPeriod && old != arena.AutoPeriod:
		ref.startReport()
	case old == arena.AutoPeriod && new == arena.PausePeriod:
		if enabled {
			go func() { defer safe.Recover(ref.hub, "referee"); ref.scoreAuto() }()
		}
	case new == arena.PostMatch && (old == arena.TeleopPeriod || old == arena.PausePeriod || old == arena.AutoPeriod):
		aborted := old != arena.TeleopPeriod
		go func() { defer safe.Recover(ref.hub, "referee"); ref.finishMatch(enabled, aborted) }()
	}
}

func (ref *Referee) scoreAuto() {
	levels := ref.world.ClimbLevels()
	teams := ref.arena.Teams()
	var autoTower [6]int
	for i, level := range levels {
		status := 0
		if level >= 1 {
			status = 1 // only level 1 counts in auto
		}
		autoTower[i] = status
		alliance := i / 3
		if err := ref.send(alliance, "autoTower", map[string]int{"TeamPosition": i%3 + 1, "AutoTowerStatus": status}); err != nil {
			ref.hub.Warnf("referee", "Could not enter auto tower for %s: %v", stations[i], err)
		}
		if status > 0 {
			ref.record("climb", stations[i], teams[i], fmt.Sprintf("AUTO tower level 1 (%s %d)", stations[i], teams[i]))
		}
	}
	ref.mu.Lock()
	if ref.report != nil {
		ref.report.AutoTower = autoTower
	}
	ref.mu.Unlock()
}

func (ref *Referee) finishMatch(enabled, aborted bool) {
	time.Sleep(assessmentDelay)
	teams := ref.arena.Teams()
	var endTower [6]int
	if enabled && !aborted {
		levels := ref.world.ClimbLevels()
		for i, level := range levels {
			if ref.Head.Awarded(stations[i]) && level < 3 {
				level = 3
				ref.record("climb", stations[i], teams[i], fmt.Sprintf("G420: %s %d awarded LEVEL 3", stations[i], teams[i]))
			}
			endTower[i] = level
			alliance := i / 3
			if err := ref.send(alliance, "endgame", map[string]int{"TeamPosition": i%3 + 1, "EndgameTowerStatus": level}); err != nil {
				ref.hub.Warnf("referee", "Could not enter endgame tower for %s: %v", stations[i], err)
			}
			if level > 0 {
				points := []int{0, 10, 20, 30}[level]
				ref.record("climb", stations[i], teams[i], fmt.Sprintf("END GAME tower level %d (+%d) — %s %d", level,
					points, stations[i], teams[i]))
			}
		}
	}
	ref.mu.Lock()
	settings := ref.settings
	ref.mu.Unlock()
	if enabled {
		ref.presentCards(teams)
	}
	if enabled && settings.AutoCommit {
		time.Sleep(500 * time.Millisecond)
		for alliance := 0; alliance < 2; alliance++ {
			if err := ref.send(alliance, "commitMatch", nil); err != nil {
				ref.hub.Warnf("referee", "Could not commit %s scores: %v", []string{"red", "blue"}[alliance], err)
			}
		}
		ref.hub.Logf("referee", "Referees committed tower scores")
	}
	time.Sleep(time.Second)
	ref.closeReport(endTower, aborted)
	ref.mu.Lock()
	hooks := append([]func(bool, bool){}, ref.onFinished...)
	ref.mu.Unlock()
	for _, hook := range hooks {
		hook(enabled, aborted)
	}
	if enabled && settings.FieldSignals {
		go ref.fieldSignals()
	}
	if enabled && settings.AutoPost {
		time.Sleep(2 * time.Second)
		if err := ref.arena.SendMatchPlay("commitAndPost", nil); err != nil {
			ref.hub.Warnf("referee", "Auto-post failed: %v", err)
		} else {
			ref.hub.Logf("referee", "Scorekeeper committed and posted the match")
		}
	}
}

// watchFouls passes the line referees' calls (the world's rules engine: G403, G405, G407, G408, G418, G419, G420)
// to the head referee for review and enters them in the arena.
func (ref *Referee) watchFouls() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		calls := ref.world.DrainInfractions()
		ref.mu.Lock()
		on := ref.settings.Enabled && ref.settings.CallFouls
		ref.mu.Unlock()
		if !on || len(calls) == 0 {
			continue
		}
		for _, call := range calls {
			ref.Head.Review(call)
			ref.queueFoul(call)
		}
		ref.Head.publish()
	}
}

// ---- Reports ---------------------------------------------------------------------------------------------------

func (ref *Referee) startReport() {
	load := ref.arena.MatchLoad()
	name := "Match"
	id := 0
	if load.Match != nil {
		name = load.Match.LongName
		id = load.Match.Id
	}
	var plans [2]string
	if raw := ref.hub.Get("world.state"); raw != nil {
		var state struct {
			Plans [2]string `json:"plans"`
		}
		_ = json.Unmarshal(raw, &state)
		plans = state.Plans
	}
	matchType := 0
	if load.Match != nil {
		matchType = load.Match.Type
	}
	ref.Head.StartMatch(name, matchType)
	ref.mu.Lock()
	ref.report = &Report{Match: name, MatchId: id, StartedAt: time.Now(), Teams: ref.arena.Teams(), Plans: plans}
	ref.mu.Unlock()
	ref.record("match", "", 0, fmt.Sprintf("%s started — red plan %q, blue plan %q", name, plans[0], plans[1]))
	ref.publish()
}

// record adds an event to the current match report and the event log.
func (ref *Referee) record(kind, station string, team int, text string) {
	_, matchTime := ref.arena.MatchTime()
	ref.mu.Lock()
	if ref.report != nil && !ref.report.Finished {
		ref.report.Events = append(ref.report.Events, Event{MatchTime: matchTime, Wall: time.Now().Format("15:04:05"),
			Kind: kind, Station: station, Team: team, Text: text})
	}
	ref.mu.Unlock()
	if kind != "match" {
		ref.hub.Logf("referee", "%s", text)
	}
	ref.publish()
}

// onEvent captures notable simulator events (faults, climbs, robot problems) into the match report.
func (ref *Referee) onEvent(event bus.Event) {
	if event.Source != "chaos" && event.Source != "world" {
		return
	}
	ref.mu.Lock()
	active := ref.report != nil && !ref.report.Finished
	ref.mu.Unlock()
	if !active {
		return
	}
	_, matchTime := ref.arena.MatchTime()
	ref.mu.Lock()
	if ref.report != nil {
		ref.report.Events = append(ref.report.Events, Event{MatchTime: matchTime, Wall: event.Time.Format("15:04:05"),
			Kind: event.Source, Text: event.Msg})
	}
	ref.mu.Unlock()
}

func (ref *Referee) closeReport(endTower [6]int, aborted bool) {
	ref.mu.Lock()
	report := ref.report
	ref.mu.Unlock()
	if report == nil || report.Finished {
		return
	}
	score := ref.hub.Get("arena.realtimeScore")
	var parsed struct {
		Red  struct{ ScoreSummary struct{ Score int } }
		Blue struct{ ScoreSummary struct{ Score int } }
	}
	_ = json.Unmarshal(score, &parsed)
	robots := ref.world.RobotStats()
	ref.mu.Lock()
	report.EndTower = endTower
	report.Score = score
	report.RedScore = parsed.Red.ScoreSummary.Score
	report.BlueScore = parsed.Blue.ScoreSummary.Score
	report.Robots = robots
	report.Aborted = aborted
	report.Finished = true
	report.Events = append(report.Events, Event{Wall: time.Now().Format("15:04:05"), Kind: "match",
		Text: fmt.Sprintf("Final: RED %d — BLUE %d%s", report.RedScore, report.BlueScore,
			map[bool]string{true: " (ABORTED)", false: ""}[aborted])})
	ref.mu.Unlock()

	if err := os.MkdirAll(ref.dir, 0755); err == nil {
		safe := strings.Map(func(r rune) rune {
			if r == ' ' || r == '/' || r == '\\' || r == ':' {
				return '_'
			}
			return r
		}, report.Match)
		report.File = fmt.Sprintf("%s-%s.json", report.StartedAt.Format("20060102-150405"), safe)
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(ref.dir, report.File), data, 0644); err != nil {
			ref.hub.Warnf("referee", "Could not save match report: %v", err)
		}
	}
	ref.mu.Lock()
	ref.reports = append([]ReportSummary{summarize(report)}, ref.reports...)
	if len(ref.reports) > 100 {
		ref.reports = ref.reports[:100]
	}
	ref.mu.Unlock()
	ref.hub.Logf("referee", "Match report saved: %s (RED %d — BLUE %d)", report.Match, report.RedScore, report.BlueScore)
	ref.publish()
}

func summarize(report *Report) ReportSummary {
	return ReportSummary{File: report.File, Match: report.Match, StartedAt: report.StartedAt, RedScore: report.RedScore,
		BlueScore: report.BlueScore, Events: len(report.Events), Aborted: report.Aborted}
}

// Reports lists the match reports written so far.
func (ref *Referee) Reports() []ReportSummary {
	ref.mu.Lock()
	defer ref.mu.Unlock()
	return append([]ReportSummary(nil), ref.reports...)
}

func (ref *Referee) loadReportList() {
	entries, err := os.ReadDir(ref.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ref.dir, entry.Name()))
		if err != nil {
			continue
		}
		var report Report
		if json.Unmarshal(data, &report) == nil {
			report.File = entry.Name()
			ref.reports = append(ref.reports, summarize(&report))
		}
	}
	sort.Slice(ref.reports, func(i, j int) bool { return ref.reports[i].StartedAt.After(ref.reports[j].StartedAt) })
}

func (ref *Referee) publish() {
	ref.mu.Lock()
	defer ref.mu.Unlock()
	ref.hub.Publish("referee.settings", ref.settings)
	ref.hub.Publish("reports.list", ref.reports)
	if ref.report != nil {
		ref.hub.Publish("reports.current", ref.report)
	}
	ref.hub.Publish("referee.panels", []bool{ref.panels[0].Connected(), ref.panels[1].Connected()})
}

func (ref *Referee) registerCommands() {
	ref.hub.Handle("referee.settings", func(args []byte) (any, error) {
		ref.mu.Lock()
		err := json.Unmarshal(args, &ref.settings)
		ref.mu.Unlock()
		ref.publish()
		return nil, err
	})
	ref.hub.Handle("referee.foul", func(args []byte) (any, error) {
		var request struct {
			Alliance string `json:"alliance"`
			Major    bool   `json:"major"`
			Rule     string `json:"rule"`    // optional, e.g. "G418"
			Station  string `json:"station"` // optional offending station, e.g. "B2"
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		alliance := 0
		if request.Alliance == "blue" || strings.HasPrefix(request.Station, "B") {
			alliance = 1
		}
		call := world.Infraction{Alliance: alliance, Major: request.Major, Rule: request.Rule, Station: request.Station,
			Reason: "called manually"}
		if ids, ok := world.RuleIds[strings.ToUpper(request.Rule)]; ok {
			call.Rule = strings.ToUpper(request.Rule)
			if call.Major || ids[0] == 0 {
				call.Major, call.RuleId = true, ids[1]
			} else {
				call.RuleId = ids[0]
			}
		}
		if team := ref.arena.TeamFor(request.Station); team != nil {
			call.Team = team.Id
		}
		ref.queueFoul(call)
		return nil, nil
	})
	ref.hub.Handle("reports.get", func(args []byte) (any, error) {
		var request struct {
			File string `json:"file"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if strings.ContainsAny(request.File, `/\`) || !strings.HasSuffix(request.File, ".json") {
			return nil, fmt.Errorf("bad report name")
		}
		data, err := os.ReadFile(filepath.Join(ref.dir, request.File))
		if err != nil {
			return nil, err
		}
		return json.RawMessage(data), nil
	})
}

// presentCards enters the head referee's CARDS for this match through the referee panel.
func (ref *Referee) presentCards(teams [6]int) {
	for team, card := range ref.Head.PresentCards() {
		alliance := -1
		for i, t := range teams {
			if t == team {
				alliance = i / 3
			}
		}
		if alliance < 0 {
			continue
		}
		name := []string{"red", "blue"}[alliance]
		if err := ref.refPanel.Send("card", map[string]any{"Alliance": name, "TeamId": team, "Card": card}); err != nil {
			ref.hub.Warnf("referee", "Could not enter the %s card for %d: %v", card, team, err)
			continue
		}
		ref.record("card", "", team, fmt.Sprintf("Head referee presented a %s CARD to %d", strings.ToUpper(card), team))
	}
	if carrying := ref.Head.CarryingYellow(teams); len(carrying) > 0 {
		ref.hub.Logf("head-ref", "Teams carrying a YELLOW CARD: %v", carrying)
	}
	ref.Head.publish()
}

// fieldSignals runs the head referee's post-match field signals once the robots have come to rest: purple (safe
// for field staff to start clearing FUEL), then green (safe for drive teams to collect their robots).
func (ref *Referee) fieldSignals() {
	defer safe.Recover(ref.hub, "referee")
	for wait := 0; wait < 20; wait++ {
		moving := false
		for _, robot := range ref.world.RobotStats() {
			if robot.Present && robot.Speed > 0.15 {
				moving = true
			}
		}
		if !moving {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if state, _ := ref.arena.MatchTime(); state != arena.PostMatch {
		return
	}
	if err := ref.refPanel.Send("signalVolunteers", nil); err == nil {
		ref.Head.Note("Field is safe for FIELD STAFF (purple): crew clearing FUEL")
	}
	time.Sleep(6 * time.Second)
	if state, _ := ref.arena.MatchTime(); state != arena.PostMatch {
		return
	}
	if err := ref.refPanel.Send("signalReset", nil); err == nil {
		ref.Head.Note("Field is safe for all (green): drive teams may collect their robots")
	}
}
