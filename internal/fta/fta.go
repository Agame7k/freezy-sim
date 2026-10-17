// Package fta is the AI FIRST Technical Advisor. It watches the arena the way an FTA watches the Field Monitor:
// before each match it runs the connection checklist for every driver station, works problems with the teams (who
// then power-cycle a radio, restart code or swap a battery), bypasses a robot that cannot be made ready in time if
// allowed to, and only then reports the field ready (the PLC's FTA-ready input). During a match it logs robot link
// drops and bad connections, and after the match it releases the field until the next one is checked.
package fta

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const (
	issueGrace   = 10 * time.Second // let a station come up on its own before working the problem with the team
	retryEvery   = 30 * time.Second
	lowBattery   = 12.2
	badTripMs    = 40
	noteHistory  = 120
	teamResponse = 4 * time.Second // how long the team takes to act on the FTA's advice
)

// Settings are the user-controllable FTA options.
type Settings struct {
	Enabled     bool `json:"enabled"`
	ManageReady bool `json:"manageReady"` // hold the field's FTA-ready input until the checklist passes
	HelpTeams   bool `json:"helpTeams"`   // have teams act on the FTA's advice (reboot radio, restart code, ...)
	AutoBypass  bool `json:"autoBypass"`  // bypass a robot that is still not ready this long after the match loads
	BypassAfter int  `json:"bypassAfter"` // seconds
}

// StationCheck is one row of the checklist.
type StationCheck struct {
	Station string  `json:"station"`
	Team    int     `json:"team"`
	Ready   bool    `json:"ready"`
	Bypass  bool    `json:"bypass"`
	Issue   string  `json:"issue,omitempty"`
	Battery float64 `json:"battery"`
	TripMs  int     `json:"tripMs"`
	Missed  int     `json:"missed"`
}

// Note is an FTA log line.
type Note struct {
	Time    string `json:"time"`
	Station string `json:"station,omitempty"`
	Text    string `json:"text"`
}

// State is published on officials.fta.
type State struct {
	Settings Settings       `json:"settings"`
	Phase    string         `json:"phase"`
	Ready    bool           `json:"ready"`
	Blockers []string       `json:"blockers"`
	Stations []StationCheck `json:"stations"`
	Notes    []Note         `json:"notes"`
}

type issueTrack struct {
	issue   string
	since   time.Time
	actedAt time.Time
	actions int
}

// FTA is the advisor.
type FTA struct {
	hub   *bus.Hub
	arena *arena.Client

	mu         sync.Mutex
	settings   Settings
	issues     map[string]*issueTrack
	notes      []Note
	loadedAt   time.Time
	lastState  arena.MatchState
	linked     map[string]bool // robot link during the match, for drop notes
	bypassed   map[string]bool
	ready      bool
	blockers   []string
	checks     []StationCheck
	fieldCheck func() string // the world's between-match work still in progress ("" when the field is clear)
}

// New creates the FTA.
func New(hub *bus.Hub, arenaClient *arena.Client) *FTA {
	fta := &FTA{hub: hub, arena: arenaClient, issues: map[string]*issueTrack{}, linked: map[string]bool{},
		bypassed: map[string]bool{},
		settings: Settings{Enabled: true, ManageReady: true, HelpTeams: true, AutoBypass: false, BypassAfter: 120}}
	arenaClient.OnMatchLoad(func(arena.MatchLoad) {
		fta.mu.Lock()
		fta.loadedAt = time.Now()
		fta.issues = map[string]*issueTrack{}
		fta.bypassed = map[string]bool{}
		fta.mu.Unlock()
	})
	arenaClient.OnMatchState(fta.onMatchState)
	fta.registerCommands()
	return fta
}

// Run starts the FTA's checks.
func (fta *FTA) Run() {
	safe.Go(fta.hub, "fta", func() {
		for range time.Tick(time.Second) {
			fta.check()
		}
	})
}

func (fta *FTA) note(station, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	fta.notes = append(fta.notes, Note{Time: time.Now().Format("15:04:05"), Station: station, Text: text})
	if len(fta.notes) > noteHistory {
		fta.notes = fta.notes[len(fta.notes)-noteHistory:]
	}
	fta.hub.Logf("fta", "%s", text)
}

func (fta *FTA) testsRunning() bool {
	var state struct {
		Running bool `json:"running"`
	}
	_ = json.Unmarshal(fta.hub.Get("tests.state"), &state)
	return state.Running
}

func (fta *FTA) onMatchState(old, new arena.MatchState) {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	fta.lastState = new
	if !fta.settings.Enabled {
		return
	}
	switch {
	case new.InMatch() && !old.InMatch():
		fta.linked = map[string]bool{}
		fta.note("", "Match started; watching connections")
	case new == arena.PostMatch && old.InMatch():
		if fta.settings.ManageReady && !fta.testsRunning() {
			// The field is not ready for the next match until the FTA has checked it again.
			go fta.setReady(false)
			fta.ready = false
		}
	}
}

func (fta *FTA) setReady(ready bool) {
	if _, err := fta.hub.Dispatch("field.set", json.RawMessage(fmt.Sprintf(`{"control":"ftaReady","index":0,"value":%t}`, ready))); err != nil {
		fta.hub.Warnf("fta", "Could not set FTA ready: %v", err)
	}
}

func (fta *FTA) teamDoes(command, station string) {
	time.AfterFunc(teamResponse, func() {
		if command == "ds.set:dsOn" {
			_, _ = fta.hub.Dispatch("ds.set", json.RawMessage(fmt.Sprintf(`{"station":%q,"field":"dsOn","value":true}`, station)))
			return
		}
		if command == "ds.rebootRadio" {
			// A robot that was never switched on gets switched on (which boots the radio anyway).
			var stations []struct {
				Station string `json:"station"`
				Robot   struct {
					Powered bool `json:"powered"`
				} `json:"robot"`
			}
			_ = json.Unmarshal(fta.hub.Get("ds.stations"), &stations)
			for _, s := range stations {
				if s.Station == station && !s.Robot.Powered {
					_, _ = fta.hub.Dispatch("ds.set", json.RawMessage(fmt.Sprintf(`{"station":%q,"field":"robotPower","value":true}`, station)))
					return
				}
			}
		}
		_, _ = fta.hub.Dispatch(command, json.RawMessage(fmt.Sprintf(`{"station":%q}`, station)))
	})
}

// diagnose names the first thing wrong with a station, as the Field Monitor shows it.
func diagnose(status *arena.StationStatus) string {
	conn := status.DsConn
	switch {
	case status.EStop:
		return "E-STOP pressed"
	case status.AStop:
		return "A-STOP pressed"
	case !status.Ethernet:
		return "driver station Ethernet unplugged"
	case conn == nil || !conn.DsLinked:
		return "driver station not connected"
	case conn.WrongStation != "":
		return "driver station plugged into the wrong station (" + conn.WrongStation + ")"
	case !conn.RadioLinked:
		return "robot radio not linked"
	case !conn.RioLinked:
		return "roboRIO not reachable"
	case !conn.RobotLinked:
		return "no robot code"
	case conn.BatteryVoltage > 0 && conn.BatteryVoltage < lowBattery:
		return fmt.Sprintf("battery low (%.1f V)", conn.BatteryVoltage)
	}
	return ""
}

// remedy is what the FTA asks the team to do about an issue, and the simulator command for the team doing it.
func remedy(issue string) (advice, command string) {
	switch {
	case issue == "driver station not connected":
		return "restart the Driver Station and check the Ethernet cable", "ds.set:dsOn"
	case issue == "robot radio not linked":
		return "check the robot is switched on and power-cycle the radio", "ds.rebootRadio"
	case issue == "roboRIO not reachable":
		return "reboot the roboRIO", "ds.rebootRio"
	case issue == "no robot code":
		return "restart robot code", "ds.restartCode"
	case strings.HasPrefix(issue, "battery low"):
		return "swap in a fresh battery", "ds.freshBattery"
	}
	return "", ""
}

func (fta *FTA) check() {
	status := fta.arena.Status()
	load := fta.arena.MatchLoad()
	state, _ := fta.arena.MatchTime()
	fta.mu.Lock()
	defer fta.mu.Unlock()
	settings := fta.settings
	now := time.Now()
	var checks []StationCheck
	allReady := true
	for _, station := range arena.Stations {
		row := StationCheck{Station: station}
		if team := load.Teams[station]; team != nil {
			row.Team = team.Id
		}
		st := status.AllianceStations[station]
		if st == nil || row.Team == 0 {
			row.Ready = true
			checks = append(checks, row)
			continue
		}
		row.Bypass = st.Bypass
		if st.DsConn != nil {
			row.Battery, row.TripMs, row.Missed = st.DsConn.BatteryVoltage, st.DsConn.DsRobotTripTimeMs, st.DsConn.MissedPacketCount
		}
		row.Issue = diagnose(st)
		row.Ready = row.Bypass || row.Issue == "" || strings.HasPrefix(row.Issue, "battery")
		if !row.Ready {
			allReady = false
		}
		checks = append(checks, row)
		if !settings.Enabled {
			continue
		}
		if state.InMatch() {
			fta.watchMatch(station, row, st)
			continue
		}
		if state != arena.PreMatch || row.Bypass {
			continue
		}
		fta.workIssue(station, row, now, settings)
	}
	fta.checks = checks
	var blockers []string
	if status.FieldEStop {
		blockers = append(blockers, "field E-stop pressed")
	}
	// Field-level blockers the arena itself reports (PLC, ArmorBlocks, ...), except the robots (checked per station
	// above) and the FTA-ready input (ours to set).
	for _, condition := range status.StartMatchConditions {
		lower := strings.ToLower(condition)
		if !strings.Contains(lower, "robot") && !strings.Contains(lower, "fta") {
			blockers = append(blockers, condition)
		}
	}
	for _, row := range checks {
		if !row.Ready {
			blockers = append(blockers, fmt.Sprintf("%s %d: %s", row.Station, row.Team, row.Issue))
		}
	}
	if fta.fieldCheck != nil {
		if busy := fta.fieldCheck(); busy != "" {
			blockers = append(blockers, busy)
		}
	}
	sort.Strings(blockers)
	fta.blockers = blockers
	ready := allReady && len(blockers) == 0 && load.Match != nil
	if settings.Enabled && settings.ManageReady && state == arena.PreMatch && !fta.testsRunning() {
		if ready != status.IsFtaReady {
			if ready {
				fta.note("", "Checklist passed: all robots linked or bypassed, field clear. FTA READY.")
			} else if status.IsFtaReady && fta.ready {
				fta.note("", "Holding the field: %s", firstOr(blockers, "match not loaded"))
			}
			go fta.setReady(ready)
		}
		fta.ready = ready
	}
	fta.publishLocked()
}

func firstOr(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// workIssue handles a pre-match problem: wait a moment, then advise the team; bypass if it drags on.
func (fta *FTA) workIssue(station string, row StationCheck, now time.Time, settings Settings) {
	track := fta.issues[station]
	if row.Issue == "" {
		if track != nil && track.actions > 0 {
			fta.note(station, "%s %d is good now", station, row.Team)
		}
		delete(fta.issues, station)
		return
	}
	if track == nil || track.issue != row.Issue {
		track = &issueTrack{issue: row.Issue, since: now}
		fta.issues[station] = track
	}
	if now.Sub(track.since) < issueGrace {
		return
	}
	if row.Issue == "E-STOP pressed" || row.Issue == "A-STOP pressed" {
		if track.actions == 0 {
			track.actions++
			fta.note(station, "%s %d has its %s; asking the team to reset it", station, row.Team, row.Issue)
		}
		return
	}
	advice, command := remedy(row.Issue)
	if reason := fta.absent(station); reason != "" {
		// Nobody at the station to talk to: wait for the team (or bypass below).
		advice, command = "", ""
		if track.actions == 0 {
			track.actions++
			fta.note(station, "%s %d is not at the field (%s); waiting on the team", station, row.Team, reason)
		}
	}
	if advice != "" && now.Sub(track.actedAt) > retryEvery {
		track.actedAt = now
		track.actions++
		fta.note(station, "%s %d: %s. Asked the team to %s.", station, row.Team, row.Issue, advice)
		if settings.HelpTeams && command != "" {
			fta.teamDoes(command, station)
		}
	}
	if settings.AutoBypass && !fta.bypassed[station] && !fta.loadedAt.IsZero() &&
		now.Sub(fta.loadedAt) > time.Duration(settings.BypassAfter)*time.Second {
		fta.bypassed[station] = true
		fta.note(station, "%s %d still not ready after %d s (%s): BYPASSING so the match can start", station, row.Team,
			settings.BypassAfter, row.Issue)
		go func() {
			_, _ = fta.hub.Dispatch("match.bypass", json.RawMessage(fmt.Sprintf(`{"station":%q}`, station)))
		}()
	}
}

// watchMatch logs link drops and bad connections during a match (the FTA does not touch robots mid-match).
func (fta *FTA) watchMatch(station string, row StationCheck, st *arena.StationStatus) {
	linked := st.DsConn != nil && st.DsConn.RobotLinked
	was, seen := fta.linked[station]
	if seen && was && !linked {
		_, matchTime := fta.arena.MatchTime()
		fta.note(station, "%s %d lost robot link at %.0f s: %s", station, row.Team, matchTime, firstOr([]string{diagnose(st)}, "unknown"))
	}
	if seen && !was && linked {
		fta.note(station, "%s %d robot link back", station, row.Team)
	}
	fta.linked[station] = linked
	if linked && row.TripMs > badTripMs {
		key := station + "-trip"
		if fta.issues[key] == nil {
			fta.issues[key] = &issueTrack{issue: "trip"}
			fta.note(station, "%s %d trip time %d ms: likely Wi-Fi interference or a busy radio", station, row.Team, row.TripMs)
		}
	}
}

func (fta *FTA) publishLocked() {
	notes := append([]Note(nil), fta.notes...)
	phase := fta.lastState.String()
	fta.hub.Publish("officials.fta", State{Settings: fta.settings, Phase: phase, Ready: fta.ready, Blockers: fta.blockers,
		Stations: fta.checks, Notes: notes})
}

func (fta *FTA) registerCommands() {
	fta.hub.Handle("fta.settings", func(args []byte) (any, error) {
		fta.mu.Lock()
		defer fta.mu.Unlock()
		if err := json.Unmarshal(args, &fta.settings); err != nil {
			return nil, err
		}
		if fta.settings.BypassAfter < 15 {
			fta.settings.BypassAfter = 15
		}
		fta.publishLocked()
		return nil, nil
	})
}

// SetFieldCheck tells the FTA how to ask whether the field is still being reset (robots on carts, the crew out).
func (fta *FTA) SetFieldCheck(check func() string) {
	fta.mu.Lock()
	fta.fieldCheck = check
	fta.mu.Unlock()
}

// absent returns why a station's drive team is not at the field ("" if it is), as the chaos friction reports it.
func (fta *FTA) absent(station string) string {
	var absent map[string]string
	_ = json.Unmarshal(fta.hub.Get("field.absent"), &absent)
	return absent[station]
}
