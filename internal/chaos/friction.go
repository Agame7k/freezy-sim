package chaos

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
)

// Match-flow friction: the problems that hold a real field up before a match even starts. A team is still walking
// over from queueing, a team never shows up (the FTA bypasses it), a robot was carried on but never switched on, or
// last match's E-stop is still latched down. These exercise the arena's start blockers and bypass flow the way real
// events do. They are drawn when a match loads, at the chaos level's rate, and undone when the match ends.

// expected pre-match problems per match by chaos level.
var frictionRates = []float64{0, 0.25, 0.6, 1.2}

// Absent is published on field.absent: stations whose drive team is not at the field, and why. The FTA reads it so
// it waits on the team (or bypasses) instead of asking an empty station to restart its Driver Station.
type Absent map[string]string

type friction struct {
	generation int
	undo       []func()
	absent     Absent
}

func (chaos *Chaos) onMatchLoad(load arena.MatchLoad) {
	chaos.mu.Lock()
	chaos.friction.generation++
	generation := chaos.friction.generation
	undo := chaos.friction.undo
	chaos.friction.undo = nil
	chaos.friction.absent = Absent{}
	level := chaos.level
	chaos.mu.Unlock()
	for _, fn := range undo {
		fn()
	}
	chaos.publishAbsent()
	if level == 0 || load.Match == nil {
		return
	}
	chaos.mu.Lock()
	rate := frictionRates[level]
	count := 0
	for limit, p := math.Exp(-rate), chaos.rng.Float64(); p > limit; p *= chaos.rng.Float64() {
		count++
	}
	type pick struct {
		kind    string
		station string
	}
	var picks []pick
	used := map[string]bool{}
	for i := 0; i < count; i++ {
		station := stations[chaos.rng.Intn(6)]
		if used[station] {
			continue
		}
		used[station] = true
		kinds := []string{"late", "late", "late", "poweroff", "poweroff", "estopheld", "noshow"}
		picks = append(picks, pick{kinds[chaos.rng.Intn(len(kinds))], station})
	}
	chaos.mu.Unlock()
	for _, p := range picks {
		chaos.applyFriction(generation, p.kind, p.station, load)
	}
}

// applyFriction starts one pre-match problem; its fix runs later unless another match has loaded since.
func (chaos *Chaos) applyFriction(generation int, kind, station string, load arena.MatchLoad) {
	team := 0
	if t := load.Teams[station]; t != nil {
		team = t.Id
	}
	if team == 0 {
		return
	}
	label := fmt.Sprintf("%s %d", station, team)
	set := func(field string, value any) {
		_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": field, "value": value})
	}
	index := 0
	for i, name := range stations {
		if name == station {
			index = i
		}
	}
	// fixLater runs fix after a delay, only if this match is still the loaded one; undo always restores at the end.
	fixLater := func(delay time.Duration, fix func(), note string) {
		time.AfterFunc(delay, func() {
			chaos.mu.Lock()
			current := chaos.friction.generation == generation
			chaos.mu.Unlock()
			if !current {
				return
			}
			if state, _ := chaos.arena.MatchTime(); state != arena.PreMatch {
				return // the match went ahead without them (bypassed); they rejoin after it
			}
			fix()
			chaos.hub.Logf("chaos", "%s", note)
		})
	}
	var text string
	var undo func()
	switch kind {
	case "noshow":
		if !chaos.ftaBypasses() {
			kind = "late" // without an FTA allowed to bypass, a no-show would hold the field forever
		} else {
			set("dsOn", false)
			chaos.markAbsent(station, "no-show")
			text = label + " has not shown up for its match (the FTA will have to bypass it)"
			undo = func() { set("dsOn", true) }
			break
		}
		fallthrough
	case "late":
		kind = "late"
		delay := time.Duration(chaos.seconds(25, 110) * float64(time.Second))
		set("dsOn", false)
		chaos.markAbsent(station, "still coming from queueing")
		text = fmt.Sprintf("%s is late from queueing (about %.0f s)", label, delay.Seconds())
		undo = func() { set("dsOn", true) }
		fixLater(delay, func() {
			chaos.markAbsent(station, "")
			set("dsOn", true)
		}, label+" arrived at the field and plugged in its Driver Station")
	case "poweroff":
		delay := time.Duration(chaos.seconds(20, 70) * float64(time.Second))
		set("robotPower", false)
		text = label + " carried its robot on without switching it on"
		undo = func() { set("robotPower", true) }
		fixLater(delay, func() { set("robotPower", true) }, label+" noticed and switched the robot on")
	case "estopheld":
		delay := time.Duration(chaos.seconds(12, 35) * float64(time.Second))
		_ = chaos.dispatch("field.set", map[string]any{"control": "teamEStop", "index": index, "value": true})
		text = label + "'s E-stop is still latched down from the last match"
		release := func() {
			_ = chaos.dispatch("field.set", map[string]any{"control": "teamEStop", "index": index, "value": false})
		}
		undo = release
		fixLater(delay, release, label+" reset its E-stop")
	default:
		return
	}
	chaos.mu.Lock()
	if chaos.friction.generation == generation {
		chaos.friction.undo = append(chaos.friction.undo, func() { chaos.markAbsent(station, ""); undo() })
	} else {
		go undo() // another match loaded while this was being set up
	}
	chaos.history = append(chaos.history, Record{Time: time.Now(), At: -1, Type: kind, Station: station, Team: team, Text: text})
	chaos.mu.Unlock()
	chaos.hub.Warnf("chaos", "%s", text)
	chaos.publish()
}

// endFriction undoes the pre-match problems once the match is over, so the next match starts clean.
func (chaos *Chaos) endFriction() {
	chaos.mu.Lock()
	chaos.friction.generation++
	undo := chaos.friction.undo
	chaos.friction.undo = nil
	chaos.mu.Unlock()
	for _, fn := range undo {
		fn()
	}
}

func (chaos *Chaos) markAbsent(station, reason string) {
	chaos.mu.Lock()
	if chaos.friction.absent == nil {
		chaos.friction.absent = Absent{}
	}
	if reason == "" {
		delete(chaos.friction.absent, station)
	} else {
		chaos.friction.absent[station] = reason
	}
	chaos.mu.Unlock()
	chaos.publishAbsent()
}

func (chaos *Chaos) publishAbsent() {
	chaos.mu.Lock()
	absent := Absent{}
	for station, reason := range chaos.friction.absent {
		absent[station] = reason
	}
	chaos.mu.Unlock()
	chaos.hub.Publish("field.absent", absent)
}

// ftaBypasses reports whether the AI FTA is allowed to bypass robots that are not ready.
func (chaos *Chaos) ftaBypasses() bool {
	var state struct {
		Settings struct {
			Enabled    bool `json:"enabled"`
			AutoBypass bool `json:"autoBypass"`
		} `json:"settings"`
	}
	if json.Unmarshal(chaos.hub.Get("officials.fta"), &state) != nil {
		return false
	}
	return state.Settings.Enabled && state.Settings.AutoBypass
}
