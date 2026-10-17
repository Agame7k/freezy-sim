package world

import (
	"fmt"
	"math"
)

// The officials' rules engine: it watches the authoritative world (contacts, zones, FUEL released by the hubs) and
// calls the 2026 REBUILT game rules the way the referees would (Game Manual section 7, TU22). Calls are queued for
// the referee, which enters them through the arena's panels, and the live state (pin counts, flags) drives the
// referees in the 3D view.

// Arena rule ids (Freezy Arena game/rule.go) for the rules the engine calls.
const (
	ruleG403      = 6
	ruleG405Minor = 8
	ruleG405Major = 9
	ruleG407      = 11
	ruleG408Minor = 12
	ruleG408Major = 13
	ruleG418Minor = 20
	ruleG418Major = 21
	ruleG419      = 22
	ruleG420      = 23
)

const (
	officialsEvery   = 12   // steps between rule checks (10 Hz)
	pinCountSec      = 3.0  // G418: a PIN may last 3 s; each further 3 s is a MAJOR FOUL
	pinReleaseDist   = 1.83 // G418 A/B: 72 in of separation or movement for more than 3 s ends the count
	pinEgregiousSec  = 15.0 // G211 E: a single PIN over 15 s is a card
	pinStartSec      = 0.8  // contact must clearly hold the opponent this long before a referee counts it
	catchGapSec      = 1.0  // G408: FUEL flow into a robot stopping for this long ends an instance
	catchCampSec     = 2.0  // sitting under a hub exit this long before catching makes it strategic (G408 C/D)
	catchCampDist    = 1.4
	momentarySec     = 3.0 // "MOMENTARY" is fewer than ~3 s
	towerProtectSec  = 30.0
	recentCallWindow = 12.0
)

// Infraction is one rule violation the officials saw.
type Infraction struct {
	At        float64 `json:"at"`        // world time
	MatchTime float64 `json:"matchTime"` // seconds since the start of the match
	Rule      string  `json:"rule"`
	RuleId    int     `json:"ruleId"`
	Major     bool    `json:"major"`
	Alliance  int     `json:"alliance"` // the alliance that committed it
	Station   string  `json:"station,omitempty"`
	Team      int     `json:"team,omitempty"`
	Reason    string  `json:"reason"`
	// Strategic marks a deliberate violation (G408 C/D), which the head referee escalates across the event.
	Strategic bool `json:"strategic,omitempty"`
	// Egregious asks the head referee to consider a card (G211).
	Egregious bool `json:"egregious,omitempty"`
	// AwardLevel3 names the station whose robot is awarded LEVEL 3 TOWER points (G420).
	AwardLevel3 string `json:"awardLevel3,omitempty"`
	// Victim is the station the violation was against, for the referees' signals.
	Victim string `json:"victim,omitempty"`
}

// PinState is a live PIN count, for the referees' visible count.
type PinState struct {
	Pinner  string  `json:"pinner"`
	Pinned  string  `json:"pinned"`
	Seconds float64 `json:"seconds"`
	Paused  bool    `json:"paused"`
	Fouls   int     `json:"fouls"`
}

// OfficialsState is published with the world summary.
type OfficialsState struct {
	Pins   []PinState   `json:"pins"`
	Recent []Infraction `json:"recent"` // calls in the last few seconds (flags in the air)
	Totals [2]int       `json:"totals"` // fouls committed this match (minor + major), by alliance
}

type pinTrack struct {
	active       bool
	holdFor      float64 // how long the hold has looked like a PIN before the count starts
	count        float64
	startA       Vec2
	startB       Vec2
	separatedFor float64
	movedFor     float64
	fouls        int
	egregious    bool
}

type robotWatch struct {
	acrossCenter bool         // G403: bumpers completely across the center line in auto
	contacts     map[int]bool // G403 / G420 contact instances in progress
	outsideShots float64      // G407: world time of the last launch from outside the alliance zone
	slowNearExit float64      // seconds sitting slowly near a hub exit
	catchOpen    bool
	catchCount   int
	catchLast    float64
	catchCamped  bool
}

type officials struct {
	enabled   bool
	pins      map[[2]int]*pinTrack
	robots    [6]robotWatch
	outSeen   [2]int
	outCallAt [2]float64
	outCalls  [2]int
	block     [2]float64 // G419 blocking time, by alliance
	blockCall [2]float64
	pending   []Infraction
	recent    []Infraction
	totals    [2]int
	inMatch   bool
}

func newOfficials() *officials {
	o := &officials{enabled: true}
	o.reset(nil)
	return o
}

func (o *officials) reset(world *World) {
	o.pins = map[[2]int]*pinTrack{}
	for i := range o.robots {
		o.robots[i] = robotWatch{contacts: map[int]bool{}, outsideShots: -100}
	}
	o.outCallAt = [2]float64{-100, -100}
	o.outCalls = [2]int{}
	o.block, o.blockCall = [2]float64{}, [2]float64{}
	o.recent = nil
	o.totals = [2]int{}
	if world != nil {
		o.outSeen = world.stats.OutBy
	}
}

// matchPhase returns the match time and whether a match is being played (robots can be enabled).
func (world *World) matchPhase() (t float64, inMatch, auto bool) {
	enabled := false
	for _, robot := range world.robots {
		if robot.Present && robot.Enabled {
			enabled = true
			auto = auto || robot.Auto
		}
	}
	if world.matchClock != nil {
		var ok bool
		t, ok = world.matchClock()
		inMatch = ok && t >= 0 && t <= TeleopEndSec+GraceSec
		if inMatch {
			auto = t < AutoSec
		}
		return t, inMatch && (enabled || t > TeleopEndSec), auto
	}
	return 0, enabled, auto
}

func (world *World) call(infraction Infraction) {
	o := world.officials
	infraction.At = world.time
	if infraction.Station != "" {
		for _, robot := range world.robots {
			if robot.Station == infraction.Station {
				infraction.Team = robot.Team
				world.noteIncident(robot, "foul "+infraction.Rule, false) // the drive team remembers (memory.go)
			}
		}
	}
	o.pending = append(o.pending, infraction)
	o.recent = append(o.recent, infraction)
	world.truth.Fouls[infraction.Alliance]++
	for i, station := range stationNames {
		if station == infraction.AwardLevel3 {
			world.truth.AwardL3[i] = true
		}
	}
	o.totals[infraction.Alliance]++
	kind := "MINOR"
	if infraction.Major {
		kind = "MAJOR"
	}
	world.logf("info", "officials", "%s %s FOUL on %s: %s", infraction.Rule, kind, allianceName(infraction.Alliance), infraction.Reason)
}

func allianceName(alliance int) string {
	if alliance == Red {
		return "red"
	}
	return "blue"
}

// DrainInfractions returns (and clears) the calls made since the last drain.
func (world *World) DrainInfractions() []Infraction {
	world.mu.Lock()
	defer world.mu.Unlock()
	calls := world.officials.pending
	world.officials.pending = nil
	return calls
}

// SetOfficials turns the in-world rule calls on or off.
func (world *World) SetOfficials(enabled bool) {
	world.mu.Lock()
	world.officials.enabled = enabled
	world.mu.Unlock()
}

// noteCatch records a robot taking in FUEL that a hub released before it touched anything else (G408).
func (world *World) noteCatch(robot *Robot) {
	watch := &world.officials.robots[robot.Index]
	if !watch.catchOpen {
		watch.catchOpen = true
		watch.catchCount = 0
		watch.catchCamped = watch.slowNearExit >= catchCampSec
	}
	watch.catchCount++
	watch.catchLast = world.time
	robot.Caught++
}

// noteLaunch records a shot at the hub for G407.
func (world *World) noteLaunch(robot *Robot) {
	if world.inAllianceZone(robot) {
		return
	}
	watch := &world.officials.robots[robot.Index]
	t, inMatch, _ := world.matchPhase()
	if inMatch && world.time-watch.outsideShots > 2.5 {
		world.call(Infraction{MatchTime: t, Rule: "G407", RuleId: ruleG407, Major: true, Alliance: robot.Alliance,
			Station: robot.Station, Reason: fmt.Sprintf("%s launched FUEL into its HUB from outside its ALLIANCE ZONE", robot.Station)})
	}
	watch.outsideShots = world.time
}

// xExtent is how far the robot's bumpers reach along x from its center.
func (robot *Robot) xExtent() float64 {
	half := robot.half()
	return math.Abs(math.Cos(robot.Heading))*half.X + math.Abs(math.Sin(robot.Heading))*half.Y
}

// inAllianceZone reports whether the robot's bumpers are partially or fully in its alliance zone.
func (world *World) inAllianceZone(robot *Robot) bool {
	depth := world.layout.Zones.AllianceZoneDepth
	extent := robot.xExtent()
	if robot.Alliance == Blue {
		return robot.Pos.X-extent < depth
	}
	return robot.Pos.X+extent > world.layout.Field.Length-depth
}

// touching reports bumper contact between two robots (with a little slack for the 4 in bumpers flexing).
func touching(a, b *Robot) bool {
	boxA, boxB := a.obb(), b.obb()
	boxA.Half = boxA.Half.Add(Vec2{0.03, 0.03})
	_, hit := SAT(boxA, boxB)
	return hit
}

// gap is the clear distance between two robots' bumpers (approximate, from their centers and sizes).
func gap(a, b *Robot) float64 {
	return a.Pos.Sub(b.Pos).Len() - (a.half().X+b.half().X+a.half().Y+b.half().Y)/2
}

func (robot *Robot) onTower() bool { return robot.Climbing || robot.ClimbLevel > 0 }

// runOfficials checks the rules (called from step at 10 Hz).
func (world *World) runOfficials(dt float64) {
	o := world.officials
	t, inMatch, auto := world.matchPhase()
	if !o.enabled || !inMatch {
		if o.inMatch {
			o.inMatch = false
		}
		if !inMatch {
			o.reset(world)
		}
		return
	}
	if !o.inMatch {
		o.inMatch = true
		o.reset(world)
	}
	cutoff := world.time - recentCallWindow
	for len(o.recent) > 0 && o.recent[0].At < cutoff {
		o.recent = o.recent[1:]
	}
	teleop := !auto && t >= TeleopStartSec
	world.watchPins(t, dt)
	world.watchCatches(t, dt)
	world.watchAuto(t, auto)
	world.watchTowers(t, teleop)
	world.watchBlocking(t, dt)
	// G405: FUEL launched out of the field (at most one call per alliance every 5 s; repeated is MAJOR).
	for alliance := 0; alliance < 2; alliance++ {
		out := world.stats.OutBy[alliance]
		if out > o.outSeen[alliance] && world.time-o.outCallAt[alliance] > 5 {
			o.outCallAt[alliance] = world.time
			o.outCalls[alliance]++
			infraction := Infraction{MatchTime: t, Rule: "G405", RuleId: ruleG405Minor, Alliance: alliance,
				Reason: "launched FUEL out of the FIELD"}
			if o.outCalls[alliance] > 1 {
				infraction.Major, infraction.RuleId, infraction.Reason = true, ruleG405Major, "launched FUEL out of the FIELD again"
			}
			world.call(infraction)
		}
		o.outSeen[alliance] = out
	}
}

// watchPins applies G418: a robot may not PIN an opponent for more than 3 s. The count runs from first contact
// until the robots separate by 72 in, or either moves 72 in from where the PIN began, for more than 3 s, or the
// pinning robot is itself pinned.
func (world *World) watchPins(t, dt float64) {
	o := world.officials
	for _, a := range world.robots {
		for _, b := range world.robots {
			if a.Alliance == b.Alliance || !a.Present || !b.Present {
				continue
			}
			key := [2]int{a.Index, b.Index}
			pin := o.pins[key]
			if pin == nil {
				pin = &pinTrack{}
				o.pins[key] = pin
			}
			contact := touching(a, b)
			// Pinning: in contact and holding the opponent still, pushing it or trapping it against something.
			toB := b.Pos.Sub(a.Pos).Norm()
			pushing := a.Vel.Dot(toB) > 0.15 || a.Ctl.Vel.Dot(toB) > 0.4
			held := b.Vel.Len() < 0.35 && !b.onTower()
			if !pin.active {
				// A referee starts the count once a robot is plainly being held, not on a brush past.
				if contact && pushing && held && a.Enabled {
					pin.holdFor += dt
				} else {
					pin.holdFor = 0
				}
				if pin.holdFor >= pinStartSec {
					*pin = pinTrack{active: true, startA: a.Pos, startB: b.Pos, count: pin.holdFor}
				}
				continue
			}
			// The pinning robot getting pinned ends this count.
			if reverse := o.pins[[2]int{b.Index, a.Index}]; reverse != nil && reverse.active && reverse.count > pin.count {
				pin.active = false
				continue
			}
			paused := false
			if gap(a, b) >= pinReleaseDist {
				pin.separatedFor += dt
				paused = true
			} else {
				pin.separatedFor = 0
			}
			if a.Pos.Sub(pin.startA).Len() >= pinReleaseDist || b.Pos.Sub(pin.startB).Len() >= pinReleaseDist {
				pin.movedFor += dt
				paused = true
			} else {
				pin.movedFor = 0
			}
			if pin.separatedFor > momentarySec || pin.movedFor > momentarySec {
				pin.active = false
				continue
			}
			if paused {
				continue
			}
			pin.count += dt
			due := int(pin.count / pinCountSec)
			for pin.fouls < due {
				pin.fouls++
				infraction := Infraction{MatchTime: t, Rule: "G418", RuleId: ruleG418Minor, Alliance: a.Alliance,
					Station: a.Station, Victim: b.Station,
					Reason: fmt.Sprintf("%s pinning %s (%.0f s)", a.Station, b.Station, pin.count)}
				if pin.fouls > 1 {
					infraction.Major, infraction.RuleId = true, ruleG418Major
				}
				if pin.count > pinEgregiousSec && !pin.egregious {
					pin.egregious = true
					infraction.Egregious = true
					infraction.Reason += ": a single PIN longer than 15 s (G211)"
				}
				world.call(infraction)
			}
		}
	}
}

// Pins lists the live PIN counts.
func (o *officials) pinStates(world *World) []PinState {
	var states []PinState
	for key, pin := range o.pins {
		if !pin.active {
			continue
		}
		a, b := world.robots[key[0]], world.robots[key[1]]
		states = append(states, PinState{Pinner: a.Station, Pinned: b.Station, Seconds: math.Round(pin.count*10) / 10,
			Paused: pin.separatedFor > 0 || pin.movedFor > 0, Fouls: pin.fouls})
	}
	return states
}

// watchCatches applies G408: no robot may gain control of FUEL released by a hub before that FUEL touches anything
// else. Each uninterrupted flow of FUEL into a robot is one instance; sitting under an exit to collect is strategic.
func (world *World) watchCatches(t, dt float64) {
	o := world.officials
	for _, robot := range world.robots {
		watch := &o.robots[robot.Index]
		near := false
		for _, hub := range world.hubs {
			if robot.Pos.Sub(hub.exitPoint()).Len() < catchCampDist {
				near = true
			}
		}
		if near && robot.Vel.Len() < 0.4 && robot.IntakeOn {
			watch.slowNearExit += dt
		} else {
			watch.slowNearExit = 0
		}
		if !watch.catchOpen || world.time-watch.catchLast < catchGapSec {
			continue
		}
		watch.catchOpen = false
		strategic := watch.catchCamped || watch.catchCount >= 6
		infraction := Infraction{MatchTime: t, Rule: "G408", RuleId: ruleG408Minor, Alliance: robot.Alliance,
			Station: robot.Station, Strategic: strategic,
			Reason: fmt.Sprintf("%s caught %d FUEL released by the HUB", robot.Station, watch.catchCount)}
		if strategic {
			infraction.Major, infraction.RuleId = true, ruleG408Major
			infraction.Reason += " (sitting under the exit: strategic)"
		}
		world.call(infraction)
	}
}

// exitPoint is the middle of the floor where a hub's exit slot drops FUEL.
func (hub *Hub) exitPoint() Vec2 {
	return hub.Center.Add(Vec2{hub.neutralDir * (hub.size/2 + 0.4), 0})
}

// watchAuto applies G403: in AUTO a robot may not completely cross the center line; each contact with an opponent
// while across is another MAJOR FOUL.
func (world *World) watchAuto(t float64, auto bool) {
	o := world.officials
	center := world.layout.Field.Length / 2
	for _, robot := range world.robots {
		watch := &o.robots[robot.Index]
		if !auto || !robot.Present {
			watch.acrossCenter = false
			continue
		}
		extent := robot.xExtent()
		across := robot.Pos.X-extent > center
		if robot.Alliance == Red {
			across = robot.Pos.X+extent < center
		}
		if across && !watch.acrossCenter {
			world.call(Infraction{MatchTime: t, Rule: "G403", RuleId: ruleG403, Major: true, Alliance: robot.Alliance,
				Station: robot.Station, Reason: fmt.Sprintf("%s completely crossed the CENTER LINE in AUTO", robot.Station)})
		}
		watch.acrossCenter = across
		for _, other := range world.robots {
			if other.Alliance == robot.Alliance || !other.Present {
				continue
			}
			contact := across && touching(robot, other)
			if contact && !watch.contacts[other.Index] {
				world.call(Infraction{MatchTime: t, Rule: "G403", RuleId: ruleG403, Major: true, Alliance: robot.Alliance,
					Station: robot.Station, Victim: other.Station,
					Reason: fmt.Sprintf("%s contacted %s across the CENTER LINE in AUTO", robot.Station, other.Station)})
			}
			watch.contacts[other.Index] = contact
		}
	}
}

// watchTowers applies G420: in the last 30 s no robot may contact an opponent touching its own tower, whoever
// starts it; an opponent that is off the ground is awarded LEVEL 3.
func (world *World) watchTowers(t float64, teleop bool) {
	o := world.officials
	if !teleop || t < TeleopEndSec-towerProtectSec {
		return
	}
	for _, robot := range world.robots {
		watch := &o.robots[robot.Index]
		for _, other := range world.robots {
			if other.Alliance == robot.Alliance || !robot.Present || !other.Present {
				continue
			}
			key := 10 + other.Index
			contact := other.onTower() && touching(robot, other)
			if contact && !watch.contacts[key] {
				infraction := Infraction{MatchTime: t, Rule: "G420", RuleId: ruleG420, Major: true, Alliance: robot.Alliance,
					Station: robot.Station, Victim: other.Station,
					Reason: fmt.Sprintf("%s contacted %s on its TOWER in the last 30 s", robot.Station, other.Station)}
				if other.ClimbLevel > 0 || other.ClimbProg > 0.2 {
					infraction.AwardLevel3 = other.Station
					infraction.Reason += "; LEVEL 3 awarded"
				}
				world.call(infraction)
			}
			watch.contacts[key] = contact
		}
	}
}

// watchBlocking applies G419: two robots of one alliance may not close off both trenches (or both bumps) between
// the same zones. Each 3 s it continues is another MAJOR FOUL.
func (world *World) watchBlocking(t, dt float64) {
	o := world.officials
	for alliance := 0; alliance < 2; alliance++ {
		blocked := false
		for _, hub := range world.hubs {
			var trenches, bumps [2]bool
			for _, robot := range world.robots {
				if robot.Alliance != alliance || !robot.Present || robot.Vel.Len() > 0.5 {
					continue
				}
				dx := math.Abs(robot.Pos.X - hub.Center.X)
				if dx > 1.3 {
					continue
				}
				side := 0
				if robot.Pos.Y > hub.Center.Y {
					side = 1
				}
				lateral := math.Abs(robot.Pos.Y - hub.Center.Y)
				if lateral > hub.size/2+world.layout.Bump.Width {
					trenches[side] = true
				} else if lateral > hub.size/2 {
					bumps[side] = true
				}
			}
			if (trenches[0] && trenches[1]) || (bumps[0] && bumps[1]) {
				blocked = true
			}
		}
		if !blocked {
			o.block[alliance], o.blockCall[alliance] = 0, 0
			continue
		}
		o.block[alliance] += dt
		if o.block[alliance] > momentarySec && o.block[alliance]-o.blockCall[alliance] >= pinCountSec {
			o.blockCall[alliance] = o.block[alliance]
			world.call(Infraction{MatchTime: t, Rule: "G419", RuleId: ruleG419, Major: true, Alliance: alliance,
				Reason: "two ROBOTS closing off both TRENCHES or both BUMPS"})
		}
	}
}

// officialsState is the live state for the referees in the 3D view.
func (world *World) officialsState() OfficialsState {
	o := world.officials
	return OfficialsState{Pins: o.pinStates(world), Recent: append([]Infraction(nil), o.recent...), Totals: o.totals}
}

// pinCount is the live PIN count of robot a on robot b (0 if none).
func (o *officials) pinCount(a, b int) float64 {
	if pin := o.pins[[2]int{a, b}]; pin != nil && pin.active {
		return pin.count
	}
	return 0
}

// pinActive reports whether the referee is still counting a PIN of robot a on robot b.
func (o *officials) pinActive(a, b int) bool {
	pin := o.pins[[2]int{a, b}]
	return pin != nil && pin.active
}

// pinningAnyone returns the opponent this robot is being counted as pinning, if any.
func (o *officials) pinningAnyone(a int) (int, float64) {
	// In station order, not map order: a robot can pin two opponents at once, and the AI must react the same way
	// every time from the same seed.
	for b := 0; b < 6; b++ {
		if pin, ok := o.pins[[2]int{a, b}]; ok && pin.active {
			return b, pin.count
		}
	}
	return -1, 0
}

// RuleIds maps a rule number to the arena's rule ids for its MINOR and MAJOR versions (0 if that kind does not exist).
var RuleIds = map[string][2]int{
	"G403": {0, ruleG403}, "G405": {ruleG405Minor, ruleG405Major}, "G407": {0, ruleG407},
	"G408": {ruleG408Minor, ruleG408Major}, "G418": {ruleG418Minor, ruleG418Major}, "G419": {0, ruleG419},
	"G420": {0, ruleG420},
}
