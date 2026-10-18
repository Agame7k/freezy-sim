// Package chaos injects the things that go wrong at real events: brownouts, radio and roboRIO reboots, code crashes,
// driver station crashes, packet loss, mechanical jams, broken swerve modules, high-centering, tipping, failed climbs,
// spilled hoppers, panicked A-stops/E-stops and fouls. Faults are scheduled randomly per match by a chaos level, or
// injected by hand.
package chaos

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/world"
)

var stations = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

// FaultType describes one kind of problem.
type FaultType struct {
	Id       string  `json:"id"`
	Label    string  `json:"label"`
	Weight   float64 `json:"weight"`
	AutoOnly bool    `json:"autoOnly"`
	Category string  `json:"category"`
}

var Types = []FaultType{
	{"brownout", "Brownout (battery sag)", 1.2, false, "electrical"},
	{"radio", "Radio reboot (comms lost ~20 s)", 0.8, false, "electrical"},
	{"rio", "roboRIO reboot (~12 s)", 0.6, false, "electrical"},
	{"code", "Robot code crash", 0.8, false, "software"},
	{"ds", "Driver station laptop crash", 0.4, false, "software"},
	{"loss", "Packet-loss burst", 1.0, false, "network"},
	{"intake", "Intake jam", 1.2, false, "mechanical"},
	{"shooter", "Shooter jam", 1.0, false, "mechanical"},
	{"module", "Swerve module failure", 0.6, false, "mechanical"},
	{"highcenter", "High-centered on a bump", 0.8, false, "mechanical"},
	{"tip", "Robot tips over", 0.15, false, "mechanical"},
	{"climb", "Climber failure", 0.7, false, "mechanical"},
	{"spill", "Hopper spill", 0.8, false, "mechanical"},
	{"astop", "Drivers hit the A-stop", 0.5, true, "driver"},
	{"estop", "Drivers hit the E-stop", 0.08, false, "driver"},
	{"foul", "Referee foul", 1.0, false, "referee"},
}

// expected faults per match by chaos level.
var levelRates = []float64{0, 1.5, 3.5, 7}

// FoulCaller is the referee (enters fouls through the scoring panels).
type FoulCaller interface {
	AddFoul(alliance int, major bool, reason, station string, team int)
}

type Scheduled struct {
	At      float64 `json:"at"` // match seconds
	Type    string  `json:"type"`
	Station string  `json:"station"`
	Done    bool    `json:"done"`
}

type Record struct {
	Time    time.Time `json:"time"`
	At      float64   `json:"at"`
	Type    string    `json:"type"`
	Station string    `json:"station"`
	Team    int       `json:"team"`
	Text    string    `json:"text"`
}

type Chaos struct {
	hub   *bus.Hub
	arena *arena.Client
	world *world.World
	fouls FoulCaller

	mu       sync.Mutex
	level    int
	reveal   bool
	schedule []Scheduled
	history  []Record
	rng      *rand.Rand
	source   *lockedSource
	checks   []*Check // recovery checks in progress and finished (recovery.go)
	friction friction // pre-match problems for the loaded match (friction.go)
}

// Reseed restarts the fault randomness from a match seed (the same seed draws the same faults).
func (chaos *Chaos) Reseed(seed int64) {
	chaos.source.Seed(seed)
}

func New(hub *bus.Hub, arenaClient *arena.Client, w *world.World, fouls FoulCaller) *Chaos {
	// The fault scheduler, injections from the GUI and delayed recoveries run on different goroutines, so the random
	// source is locked.
	source := &lockedSource{source: rand.NewSource(time.Now().UnixNano())}
	chaos := &Chaos{hub: hub, arena: arenaClient, world: w, fouls: fouls, level: 1, rng: rand.New(source), source: source}
	arenaClient.OnMatchState(func(old, new arena.MatchState) {
		if new == arena.AutoPeriod && old != arena.AutoPeriod {
			chaos.planMatch()
		}
		if new == arena.PostMatch {
			chaos.mu.Lock()
			chaos.schedule = nil
			chaos.mu.Unlock()
			chaos.publish()
			safe.Async(chaos.hub, "chaos", chaos.endFriction)
		}
	})
	arenaClient.OnMatchLoad(func(load arena.MatchLoad) { safe.Async(chaos.hub, "chaos", func() { chaos.onMatchLoad(load) }) })
	chaos.registerCommands()
	return chaos
}

// Run starts the scheduler.
func (chaos *Chaos) Run() {
	safe.Go(chaos.hub, "chaos", func() {
		for range time.Tick(100 * time.Millisecond) {
			chaos.tick()
		}
	})
	chaos.publish()
}

// planMatch draws this match's faults.
func (chaos *Chaos) planMatch() {
	chaos.mu.Lock()
	defer chaos.mu.Unlock()
	chaos.schedule = nil
	rate := levelRates[chaos.level]
	if rate == 0 {
		return
	}
	// Poisson-distributed fault count.
	count := 0
	for limit, p := math.Exp(-rate), chaos.rng.Float64(); p > limit; p *= chaos.rng.Float64() {
		count++
	}
	total := 0.0
	for _, t := range Types {
		total += t.Weight
	}
	for i := 0; i < count; i++ {
		pick := chaos.rng.Float64() * total
		var chosen FaultType
		for _, t := range Types {
			if pick -= t.Weight; pick <= 0 {
				chosen = t
				break
			}
		}
		at := 2 + chaos.rng.Float64()*150
		if chosen.AutoOnly {
			at = 3 + chaos.rng.Float64()*14
		}
		chaos.schedule = append(chaos.schedule, Scheduled{At: at, Type: chosen.Id, Station: stations[chaos.rng.Intn(6)]})
	}
	sort.Slice(chaos.schedule, func(i, j int) bool { return chaos.schedule[i].At < chaos.schedule[j].At })
	chaos.hub.Logf("chaos", "Chaos level %d: %d problem(s) will happen this match", chaos.level, len(chaos.schedule))
	safe.Async(chaos.hub, "chaos", chaos.publish)
}

func (chaos *Chaos) tick() {
	chaos.evaluateChecks()
	state, matchTime := chaos.arena.MatchTime()
	if !state.InMatch() {
		return
	}
	chaos.mu.Lock()
	var due []Scheduled
	for i := range chaos.schedule {
		if !chaos.schedule[i].Done && matchTime >= chaos.schedule[i].At {
			chaos.schedule[i].Done = true
			due = append(due, chaos.schedule[i])
		}
	}
	chaos.mu.Unlock()
	for _, item := range due {
		if err := chaos.Inject(item.Type, item.Station); err != nil {
			chaos.hub.Warnf("chaos", "Could not inject %s on %s: %v", item.Type, item.Station, err)
		}
	}
}

func (chaos *Chaos) dispatch(command string, args any) error {
	data, _ := json.Marshal(args)
	_, err := chaos.hub.Dispatch(command, data)
	return err
}

func (chaos *Chaos) later(delay time.Duration, fn func()) {
	go func() {
		defer safe.Recover(chaos.hub, "chaos")
		time.Sleep(delay)
		fn()
	}()
}

func (chaos *Chaos) seconds(lo, hi float64) float64 {
	return lo + chaos.rng.Float64()*(hi-lo)
}

// Inject applies one fault to a station's robot now.
func (chaos *Chaos) Inject(kind, station string) error {
	index := -1
	for i, name := range stations {
		if name == station {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("unknown station %q", station)
	}
	team := chaos.arena.Teams()[index]
	label := fmt.Sprintf("%s %d", station, team)
	_, injectedAt := chaos.arena.MatchTime()
	check := chaos.planCheck(kind, station, team, injectedAt) // (baselines before the fault lands)
	var text string
	switch kind {
	case "brownout":
		_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "resistance", "value": 0.11})
		chaos.later(time.Duration(chaos.seconds(4, 8)*float64(time.Second)), func() {
			_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "resistance", "value": 0.018})
		})
		text = label + " browned out (battery sagging under load)"
	case "radio":
		_ = chaos.dispatch("ds.rebootRadio", map[string]any{"station": station})
		text = label + " radio rebooted — robot lost comms"
	case "rio":
		_ = chaos.dispatch("ds.rebootRio", map[string]any{"station": station})
		text = label + " roboRIO rebooted"
	case "code":
		_ = chaos.dispatch("ds.crashCode", map[string]any{"station": station})
		chaos.later(time.Duration(chaos.seconds(4, 10)*float64(time.Second)), func() {
			_ = chaos.dispatch("ds.restartCode", map[string]any{"station": station})
		})
		text = label + " robot code crashed (drivers restarting it)"
	case "ds":
		_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "dsOn", "value": false})
		chaos.later(time.Duration(chaos.seconds(6, 14)*float64(time.Second)), func() {
			_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "dsOn", "value": true})
		})
		text = label + " driver station laptop crashed"
	case "loss":
		_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "lossPct", "value": 35})
		chaos.later(time.Duration(chaos.seconds(5, 12)*float64(time.Second)), func() {
			_ = chaos.dispatch("ds.set", map[string]any{"station": station, "field": "lossPct", "value": 0})
		})
		text = label + " heavy packet loss (Wi-Fi interference)"
	case "intake", "shooter", "module", "highcenter", "tip", "climb", "spill":
		ok := chaos.world.WithRobot(station, func(robot *world.Robot, w *world.World) {
			now := w.Time()
			switch kind {
			case "intake":
				robot.Faults.IntakeJamUntil = now + chaos.seconds(8, 20)
				text = label + " intake jammed"
			case "shooter":
				robot.Faults.ShooterJamUntil = now + chaos.seconds(6, 15)
				text = label + " shooter jammed"
			case "module":
				robot.Faults.SpeedScale = 0.55
				text = label + " lost a swerve module (slow for the rest of the match)"
			case "highcenter":
				robot.Faults.HighCenterNext = true
				text = label + " will high-center on the next bump"
			case "tip":
				if !w.TippingLocked() {
					text = label + " took a hard hit (tipping is turned off)"
					break
				}
				robot.Faults.Tipped = true
				text = label + " TIPPED OVER"
			case "climb":
				robot.Faults.ClimbFail = true
				text = label + " climber failure (can only reach level 1)"
			case "spill":
				spilled := w.SpillHopper(robot, 6+chaos.rng.Intn(8))
				text = fmt.Sprintf("%s spilled %d FUEL from its hopper", label, spilled)
			}
		})
		if !ok {
			return fmt.Errorf("no robot at %s", station)
		}
	case "astop":
		_ = chaos.dispatch("field.pulse", map[string]any{"control": "teamAStop", "index": index, "ms": 1200})
		text = label + " drivers hit the A-stop (auto went wrong)"
	case "estop":
		_ = chaos.dispatch("field.pulse", map[string]any{"control": "teamEStop", "index": index, "ms": 1500})
		text = label + " drivers hit the E-STOP"
	case "foul":
		if chaos.fouls != nil {
			major := chaos.rng.Float64() < 0.25
			reasons := []string{"contact outside the frame perimeter", "too many FUEL controlled", "zone violation",
				"launching FUEL out of bounds", "inside the opponent's protected zone", "damaging a field element"}
			chaos.fouls.AddFoul(index/3, major, fmt.Sprintf("%s — %s", label, reasons[chaos.rng.Intn(len(reasons))]),
				station, team)
			text = label + " drew a foul"
		}
	default:
		return fmt.Errorf("unknown fault type %q", kind)
	}
	_, matchTime := chaos.arena.MatchTime()
	chaos.mu.Lock()
	chaos.history = append(chaos.history, Record{Time: time.Now(), At: matchTime, Type: kind, Station: station, Team: team,
		Text: text})
	if len(chaos.history) > 200 {
		chaos.history = chaos.history[len(chaos.history)-200:]
	}
	if check != nil {
		chaos.checks = append(chaos.checks, check)
		if len(chaos.checks) > checksKept {
			chaos.checks = chaos.checks[len(chaos.checks)-checksKept:]
		}
	}
	chaos.mu.Unlock()
	chaos.hub.Warnf("chaos", "%s", text)
	chaos.publish()
	return nil
}

func (chaos *Chaos) publish() {
	chaos.mu.Lock()
	defer chaos.mu.Unlock()
	var upcoming []Scheduled
	pending := 0
	for _, item := range chaos.schedule {
		if !item.Done {
			pending++
			if chaos.reveal {
				upcoming = append(upcoming, item)
			}
		}
	}
	checks := make([]Check, 0, len(chaos.checks))
	for _, check := range chaos.checks {
		checks = append(checks, *check)
	}
	chaos.hub.Publish("chaos.state", map[string]any{
		"level": chaos.level, "reveal": chaos.reveal, "pending": pending, "upcoming": upcoming,
		"history": chaos.history, "types": Types, "checks": checks,
	})
}

func (chaos *Chaos) registerCommands() {
	chaos.hub.Handle("chaos.config", func(args []byte) (any, error) {
		var request struct {
			Level  *int  `json:"level"`
			Reveal *bool `json:"reveal"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		chaos.mu.Lock()
		if request.Level != nil {
			chaos.level = max(0, min(*request.Level, len(levelRates)-1))
		}
		if request.Reveal != nil {
			chaos.reveal = *request.Reveal
		}
		chaos.mu.Unlock()
		chaos.publish()
		return nil, nil
	})
	chaos.hub.Handle("chaos.inject", func(args []byte) (any, error) {
		var request struct {
			Type    string `json:"type"`
			Station string `json:"station"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		if request.Station == "" || request.Station == "random" {
			request.Station = stations[chaos.rng.Intn(6)]
		}
		return nil, chaos.Inject(request.Type, request.Station)
	})
	chaos.hub.Handle("chaos.clearHistory", func(_ []byte) (any, error) {
		chaos.mu.Lock()
		chaos.history = nil
		chaos.mu.Unlock()
		chaos.publish()
		return nil, nil
	})
}

// lockedSource is a goroutine-safe rand.Source.
type lockedSource struct {
	mu     sync.Mutex
	source rand.Source
}

func (s *lockedSource) Int63() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source.Int63()
}

func (s *lockedSource) Seed(seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source.Seed(seed)
}
