// Package fieldio models the field's physical I/O (E-stops, A-stops, FTA switch, Ethernet sensing, hub fuel sensors,
// stack lights, hub motors and lights) and exposes it to the arena through either a Modbus-TCP PLC emulator or the
// Freezy alternate-IO path (ESP32 E-stop boxes + the arena's PLC websocket).
package fieldio

import (
	"fmt"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
)

// Input indexes, identical to the arena's plc/plc.go enum.
const (
	InFieldEStop = iota
	InRed1EStop
	InRed1AStop
	InRed2EStop
	InRed2AStop
	InRed3EStop
	InRed3AStop
	InBlue1EStop
	InBlue1AStop
	InBlue2EStop
	InBlue2AStop
	InBlue3EStop
	InBlue3AStop
	InRedConnected1
	InRedConnected2
	InRedConnected3
	InBlueConnected1
	InBlueConnected2
	InBlueConnected3
	InFtaReady
	InRedHubSensor1
	InRedHubSensor2
	InRedHubSensor3
	InRedHubSensor4
	InBlueHubSensor1
	InBlueHubSensor2
	InBlueHubSensor3
	InBlueHubSensor4
	InputCount
)

// Register indexes.
const (
	RegFieldIoConnection = iota
	RegRedHubTotal
	RegBlueHubTotal
	RegRedHubCount1
	RegRedHubCount2
	RegRedHubCount3
	RegRedHubCount4
	RegBlueHubCount1
	RegBlueHubCount2
	RegBlueHubCount3
	RegBlueHubCount4
	RegisterCount
)

// Coil indexes.
const (
	CoilHeartbeat = iota
	CoilMatchReset
	CoilStackGreen
	CoilStackOrange
	CoilStackRed
	CoilStackBlue
	CoilStackBuzzer
	CoilFieldResetLight
	CoilAwardsModeLight
	CoilRedHubMotor
	CoilBlueHubMotor
	CoilRedHubLight
	CoilBlueHubLight
	CoilRedWins  // pulsed 10 s when the audience display shows a red (or tie) result; the PLC runs the light show
	CoilBlueWins // same for blue
	CoilCount
)

// LegacyCoilCount is the coil map before the winner coils (arena builds before 2026-10-11 write 13 coils).
const LegacyCoilCount = CoilBlueHubLight + 1

var InputNames = [InputCount]string{
	"fieldEStop", "red1EStop", "red1AStop", "red2EStop", "red2AStop", "red3EStop", "red3AStop", "blue1EStop",
	"blue1AStop", "blue2EStop", "blue2AStop", "blue3EStop", "blue3AStop", "redConnected1", "redConnected2",
	"redConnected3", "blueConnected1", "blueConnected2", "blueConnected3", "ftaReady", "redHubSensor1",
	"redHubSensor2", "redHubSensor3", "redHubSensor4", "blueHubSensor1", "blueHubSensor2", "blueHubSensor3",
	"blueHubSensor4",
}

var RegisterNames = [RegisterCount]string{
	"fieldIoConnection", "redHubTotal", "blueHubTotal", "redHubCount1", "redHubCount2", "redHubCount3",
	"redHubCount4", "blueHubCount1", "blueHubCount2", "blueHubCount3", "blueHubCount4",
}

var CoilNames = [CoilCount]string{
	"heartbeat", "matchReset", "stackLightGreen", "stackLightOrange", "stackLightRed", "stackLightBlue",
	"stackLightBuzzer", "fieldResetLight", "awardsModeLight", "redHubMotor", "blueHubMotor", "redHubLight",
	"blueHubLight", "redWins", "blueWins",
}

// ArmorBlock bit names (fieldIoConnection register).
var ArmorBlockNames = [4]string{"redDs", "blueDs", "redIoLink", "blueIoLink"}

const sensorPulse = 60 * time.Millisecond

// Field is the physical state of the field's I/O devices.
type Field struct {
	mu sync.Mutex

	FieldEStop  bool
	TeamEStop   [6]bool
	TeamAStop   [6]bool
	FtaReady    bool
	ArmorBlocks [4]bool
	HubCounts   [2][4]int // [red, blue][sensor]

	ethernet     func(station int) bool
	sensorUntil  [2][4]time.Time
	coils        [CoilCount]bool
	coilSource   string
	coilsAt      time.Time
	heartbeatOk  bool
	resetCount   int
	lastReset    bool
	onMatchReset []func()

	// The winner light show the PLC runs on its own once the arena pulses redWins/blueWins.
	lastWins    [2]bool
	winnerShow  WinnerShow
	revealDelay time.Duration

	// Match-start bookkeeping so a late-arriving reset pulse never erases FUEL scored after the match started.
	inMatch   bool
	resetDone bool      // the reset pulse for this match has been applied
	baseline  [2][4]int // counts at the moment the match start was observed
}

func newField() *Field {
	return &Field{ArmorBlocks: [4]bool{true, true, true, true}, heartbeatOk: true, revealDelay: DefaultRevealDelay}
}

// Inputs returns the 28 discrete inputs exactly as wired (E-stops are normally closed: true = not pressed).
func (field *Field) Inputs(now time.Time) [InputCount]bool {
	field.mu.Lock()
	defer field.mu.Unlock()
	return field.inputsLocked(now)
}

func (field *Field) inputsLocked(now time.Time) [InputCount]bool {
	var inputs [InputCount]bool
	inputs[InFieldEStop] = !field.FieldEStop
	for i := 0; i < 6; i++ {
		inputs[InRed1EStop+2*i] = !field.TeamEStop[i]
		inputs[InRed1AStop+2*i] = !field.TeamAStop[i]
		if field.ethernet != nil {
			inputs[InRedConnected1+i] = field.ethernet(i)
		}
	}
	inputs[InFtaReady] = field.FtaReady
	for alliance := 0; alliance < 2; alliance++ {
		for sensor := 0; sensor < 4; sensor++ {
			inputs[InRedHubSensor1+4*alliance+sensor] = now.Before(field.sensorUntil[alliance][sensor])
		}
	}
	return inputs
}

// Registers returns the 11 holding registers.
func (field *Field) Registers() [RegisterCount]uint16 {
	field.mu.Lock()
	defer field.mu.Unlock()
	return field.registersLocked()
}

func (field *Field) registersLocked() [RegisterCount]uint16 {
	var registers [RegisterCount]uint16
	for i, connected := range field.ArmorBlocks {
		if connected {
			registers[RegFieldIoConnection] |= 1 << i
		}
	}
	for alliance := 0; alliance < 2; alliance++ {
		total := 0
		for sensor := 0; sensor < 4; sensor++ {
			count := field.HubCounts[alliance][sensor]
			registers[RegRedHubCount1+4*alliance+sensor] = uint16(count)
			total += count
		}
		registers[RegRedHubTotal+alliance] = uint16(total)
	}
	return registers
}

// ScoreFuel records a FUEL passing one of a hub's sensors. alliance 0 = red, 1 = blue; sensor 0-3.
func (field *Field) ScoreFuel(alliance, sensor int) {
	if alliance < 0 || alliance > 1 || sensor < 0 || sensor > 3 {
		return
	}
	field.mu.Lock()
	field.HubCounts[alliance][sensor]++
	field.sensorUntil[alliance][sensor] = time.Now().Add(sensorPulse)
	field.mu.Unlock()
}

// ResetHubCounts zeroes the hub counters (what the PLC does on a match reset pulse).
func (field *Field) ResetHubCounts() {
	field.mu.Lock()
	field.HubCounts = [2][4]int{}
	field.mu.Unlock()
}

// SetCoils records the outputs driven by the arena, and detects the match reset pulse.
func (field *Field) SetCoils(coils [CoilCount]bool, source string) {
	field.mu.Lock()
	field.coils = coils
	field.coilSource = source
	field.coilsAt = time.Now()
	rising := coils[CoilMatchReset] && !field.lastReset
	field.lastReset = coils[CoilMatchReset]
	field.watchWinnerCoilsLocked(coils, field.coilsAt)
	var listeners []func()
	if rising {
		if field.inMatch && !field.resetDone {
			// The pulse arrived after the match had already started: only discard FUEL counted before the start.
			for alliance := 0; alliance < 2; alliance++ {
				for sensor := 0; sensor < 4; sensor++ {
					field.HubCounts[alliance][sensor] = max(0, field.HubCounts[alliance][sensor]-field.baseline[alliance][sensor])
				}
			}
		} else if !field.inMatch {
			field.HubCounts = [2][4]int{}
		}
		field.resetDone = field.inMatch
		field.baseline = [2][4]int{}
		field.resetCount++
		listeners = append(listeners, field.onMatchReset...)
	}
	field.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
}

// MatchStarted is called when the arena's match starts (or ends). The counts at the start are remembered so the
// arena's reset pulse, whenever it arrives, removes exactly the pre-match FUEL.
func (field *Field) MatchStarted(started bool) {
	field.mu.Lock()
	defer field.mu.Unlock()
	if started && !field.inMatch {
		field.inMatch = true
		field.winnerShow.Started = time.Time{} // house lights back to normal for the match
		field.resetDone = false
		field.baseline = field.HubCounts
	} else if !started {
		field.inMatch = false
		field.resetDone = false
	}
}

// ResetCount is the number of match reset pulses seen from the arena.
func (field *Field) ResetCount() int {
	field.mu.Lock()
	defer field.mu.Unlock()
	return field.resetCount
}

// OnMatchReset registers a callback for the PLC match reset pulse.
func (field *Field) OnMatchReset(listener func()) {
	field.mu.Lock()
	field.onMatchReset = append(field.onMatchReset, listener)
	field.mu.Unlock()
}

// Coils returns the effective outputs. When the PLC heartbeat watchdog has tripped, every output is off.
func (field *Field) Coils() [CoilCount]bool {
	field.mu.Lock()
	defer field.mu.Unlock()
	if !field.heartbeatOk {
		return [CoilCount]bool{}
	}
	return field.coils
}

// HubMotorsOn reports whether each hub's exhaust motors are running.
func (field *Field) HubMotorsOn() (red, blue bool) {
	coils := field.Coils()
	return coils[CoilRedHubMotor], coils[CoilBlueHubMotor]
}

// HubLightsOn reports whether each hub's light coil is on.
func (field *Field) HubLightsOn() (red, blue bool) {
	coils := field.Coils()
	return coils[CoilRedHubLight], coils[CoilBlueHubLight]
}

func (field *Field) setHeartbeat(ok bool) {
	field.mu.Lock()
	field.heartbeatOk = ok
	field.mu.Unlock()
}

// Set changes one physical control by name.
func (field *Field) Set(control string, index int, value bool) error {
	field.mu.Lock()
	defer field.mu.Unlock()
	switch control {
	case "fieldEStop":
		field.FieldEStop = value
	case "teamEStop":
		if index < 0 || index > 5 {
			return fmt.Errorf("bad station index %d", index)
		}
		field.TeamEStop[index] = value
	case "teamAStop":
		if index < 0 || index > 5 {
			return fmt.Errorf("bad station index %d", index)
		}
		field.TeamAStop[index] = value
	case "ftaReady":
		field.FtaReady = value
	case "armorBlock":
		if index < 0 || index > 3 {
			return fmt.Errorf("bad ArmorBlock index %d", index)
		}
		field.ArmorBlocks[index] = value
	default:
		return fmt.Errorf("unknown field control %q", control)
	}
	return nil
}

// SetHubCount overwrites one sensor counter (manual testing).
func (field *Field) SetHubCount(alliance, sensor, value int) {
	if alliance < 0 || alliance > 1 || sensor < 0 || sensor > 3 {
		return
	}
	field.mu.Lock()
	field.HubCounts[alliance][sensor] = value
	field.mu.Unlock()
}

// Snapshot is the published state of the physical field I/O.
type Snapshot struct {
	FieldEStop  bool                  `json:"fieldEStop"`
	TeamEStop   [6]bool               `json:"teamEStop"`
	TeamAStop   [6]bool               `json:"teamAStop"`
	FtaReady    bool                  `json:"ftaReady"`
	ArmorBlocks [4]bool               `json:"armorBlocks"`
	HubCounts   [2][4]int             `json:"hubCounts"`
	Inputs      [InputCount]bool      `json:"inputs"`
	Registers   [RegisterCount]uint16 `json:"registers"`
	Coils       [CoilCount]bool       `json:"coils"`
	RawCoils    [CoilCount]bool       `json:"rawCoils"`
	CoilSource  string                `json:"coilSource"`
	CoilsAgeMs  int64                 `json:"coilsAgeMs"`
	HeartbeatOk bool                  `json:"heartbeatOk"`
	ResetCount  int                   `json:"resetCount"`
	InputNames  [InputCount]string    `json:"inputNames"`
	RegNames    [RegisterCount]string `json:"registerNames"`
	CoilNames   [CoilCount]string     `json:"coilNames"`
	WinnerShow  *WinnerShowState      `json:"winnerShow,omitempty"`
}

// The arena only pulses the winner coils for 10 s, at the moment the audience display switches to "score", and leaves
// the sequence (and any delay-on) to the PLC. The audience only learns who won when the display's winner video reveals
// it several seconds later, so the PLC holds the show's big moment back by a delay-on timer (the reveal delay) and
// runs it for WinnerShowAfterReveal from there. The show therefore outlives the pulse.
const WinnerShowAfterReveal = 10 * time.Second

// DefaultRevealDelay is the delay-on used before the sim has seen the audience display: the usual post-match path
// (match screen → score).
var DefaultRevealDelay = arena.ScoreRevealDelay("match")

// WinnerShow is the PLC's winner light sequence: which alliance colors it shows, when the coil rose and how long
// after that the winner is revealed.
type WinnerShow struct {
	Id      int
	Red     bool
	Blue    bool
	Started time.Time
	Reveal  time.Duration
}

// WinnerShowState is the published form of a running winner show.
type WinnerShowState struct {
	Id         int   `json:"id"`
	Red        bool  `json:"red"`
	Blue       bool  `json:"blue"`
	ElapsedMs  int64 `json:"elapsedMs"`
	RevealMs   int64 `json:"revealMs"`
	DurationMs int64 `json:"durationMs"`
}

// watchWinnerCoilsLocked starts the winner show on a rising edge of either win coil. Both coils on is a tie, which
// shows both colors; a coil that rises while the other is already on joins the running show.
func (field *Field) watchWinnerCoilsLocked(coils [CoilCount]bool, now time.Time) {
	wins := [2]bool{coils[CoilRedWins], coils[CoilBlueWins]}
	rising := (wins[0] && !field.lastWins[0]) || (wins[1] && !field.lastWins[1])
	field.lastWins = wins
	if !rising {
		return
	}
	show := &field.winnerShow
	if field.winnerShowLocked(now) != nil {
		// The other coil came on during the show (written separately): the same show, now a tie.
		show.Red, show.Blue = show.Red || wins[0], show.Blue || wins[1]
		return
	}
	show.Id++
	show.Red, show.Blue, show.Started, show.Reveal = wins[0], wins[1], now, field.revealDelay
}

// SetRevealDelay sets the winner show's delay-on: how long after the coil rises the audience display reveals the
// winner. The display mode and the coils reach the sim on different links, so a delay that arrives just after the
// coil still retimes the show that coil started.
func (field *Field) SetRevealDelay(delay time.Duration) {
	field.mu.Lock()
	defer field.mu.Unlock()
	field.revealDelay = delay
	if show := &field.winnerShow; !show.Started.IsZero() && time.Since(show.Started) < 2*time.Second {
		show.Reveal = delay
	}
}

// winnerShowLocked returns the running show, or nil. A tripped heartbeat drops every PLC output, lights included.
func (field *Field) winnerShowLocked(now time.Time) *WinnerShowState {
	show := field.winnerShow
	duration := show.Reveal + WinnerShowAfterReveal
	if show.Started.IsZero() || !field.heartbeatOk || now.Sub(show.Started) >= duration {
		return nil
	}
	return &WinnerShowState{Id: show.Id, Red: show.Red, Blue: show.Blue, ElapsedMs: now.Sub(show.Started).Milliseconds(),
		RevealMs: show.Reveal.Milliseconds(), DurationMs: duration.Milliseconds()}
}

// WinnerShow returns the running winner light show, or nil.
func (field *Field) WinnerShow() *WinnerShowState {
	field.mu.Lock()
	defer field.mu.Unlock()
	return field.winnerShowLocked(time.Now())
}

func (field *Field) snapshot(now time.Time) Snapshot {
	field.mu.Lock()
	defer field.mu.Unlock()
	coils := field.coils
	if !field.heartbeatOk {
		coils = [CoilCount]bool{}
	}
	age := int64(-1)
	if !field.coilsAt.IsZero() {
		age = now.Sub(field.coilsAt).Milliseconds()
	}
	return Snapshot{
		FieldEStop: field.FieldEStop, TeamEStop: field.TeamEStop, TeamAStop: field.TeamAStop,
		FtaReady: field.FtaReady, ArmorBlocks: field.ArmorBlocks, HubCounts: field.HubCounts,
		Inputs: field.inputsLocked(now), Registers: field.registersLocked(), Coils: coils, RawCoils: field.coils,
		CoilSource: field.coilSource, CoilsAgeMs: age, HeartbeatOk: field.heartbeatOk,
		ResetCount: field.resetCount, InputNames: InputNames, RegNames: RegisterNames, CoilNames: CoilNames,
		WinnerShow: field.winnerShowLocked(now),
	}
}
