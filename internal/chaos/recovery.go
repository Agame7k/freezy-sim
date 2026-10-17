package chaos

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/ds"
)

// Recovery checks turn chaos into a test of the arena: every fault the FMS can see comes with what a correct FMS must
// show and how fast — a rebooting radio drops the robot's link and the link comes back, a crashed DS laptop is noticed
// and reconnects, an A-stop disables the robot for the rest of AUTO only, an E-stop latches for the match, a foul the
// referee enters shows up in the score. Checks read only the arena's own view of the station.

// Check is one expectation following an injected fault.
type Check struct {
	Fault   string    `json:"fault"`
	Station string    `json:"station"`
	Team    int       `json:"team"`
	At      float64   `json:"at"` // match time of the fault
	Started time.Time `json:"started"`
	Status  string    `json:"status"` // waiting, pass, fail, n/a
	Step    string    `json:"step"`   // what it is waiting for now (or what failed)
	Detail  string    `json:"detail,omitempty"`

	steps    []checkStep
	index    int
	stepAt   time.Time
	baseline int
}

// checkStep waits for until() within a deadline; a hold step instead requires ok() to stay true until until().
type checkStep struct {
	describe string
	within   time.Duration
	until    func() bool
	hold     func() bool
}

const checksKept = 60

func (chaos *Chaos) station(name string) *arena.StationStatus {
	status := chaos.arena.Status()
	if status.AllianceStations == nil || status.AllianceStations[name] == nil {
		return &arena.StationStatus{}
	}
	return status.AllianceStations[name]
}

func (chaos *Chaos) dsConn(name string) arena.DsConn {
	if conn := chaos.station(name).DsConn; conn != nil {
		return *conn
	}
	return arena.DsConn{}
}

// planCheck sets up the expectations for a fault (nil for faults the FMS cannot see).
func (chaos *Chaos) planCheck(kind, station string, team int, at float64) *Check {
	state, _ := chaos.arena.MatchTime()
	conn := chaos.dsConn(station)
	check := &Check{Fault: kind, Station: station, Team: team, At: at, Started: time.Now(), Status: "waiting"}
	linkLost := func() bool { return !chaos.dsConn(station).RobotLinked }
	linkBack := func() bool { return chaos.dsConn(station).RobotLinked }
	enabled := func() bool { return chaos.dsConn(station).Enabled }
	inAuto := func() bool { s, _ := chaos.arena.MatchTime(); return s == arena.AutoPeriod }
	switch kind {
	case "radio":
		check.steps = []checkStep{
			{describe: "the arena shows the robot's link drop", within: 3 * time.Second, until: linkLost},
			{describe: "the robot links again after the radio boots", within: ds.RadioBootTime + 15*time.Second, until: linkBack},
		}
	case "rio":
		check.steps = []checkStep{
			{describe: "the arena shows the robot's link drop", within: 3 * time.Second, until: linkLost},
			{describe: "the robot links again after the roboRIO boots", within: ds.RioBootTime + ds.CodeBootTime + 10*time.Second,
				until: linkBack},
		}
	case "ds":
		check.steps = []checkStep{
			{describe: "the arena notices the DS go away", within: 3 * time.Second, until: func() bool { return !chaos.dsConn(station).DsLinked }},
			{describe: "the DS reconnects to the FMS", within: 30 * time.Second, until: func() bool { return chaos.dsConn(station).DsLinked }},
		}
	case "code":
		// The FMS keeps commanding the station enabled; what it can see is the DS reporting the robot disabled (its
		// TCP log packets, every 500 ms), then enabled again once the code is back.
		if !conn.Enabled || !conn.DsReportedStatusValid {
			return nil
		}
		reported := func() bool { return chaos.dsConn(station).DsReportedEnabled }
		check.steps = []checkStep{
			{describe: "the DS reports the robot disabled", within: 2 * time.Second, until: func() bool { return !reported() }},
			{describe: "the DS reports it enabled again once the code restarts", within: 20 * time.Second,
				until: func() bool { return reported() || !enabled() }},
		}
	case "loss":
		check.baseline = conn.MissedPacketCount
		check.steps = []checkStep{
			{describe: "the arena counts missed packets", within: 8 * time.Second, until: func() bool {
				return chaos.dsConn(station).MissedPacketCount > check.baseline
			}},
		}
	case "astop":
		if state != arena.AutoPeriod || !conn.Enabled {
			return nil
		}
		check.steps = []checkStep{
			{describe: "the A-stop disables the robot", within: 1500 * time.Millisecond, until: func() bool { return !enabled() }},
			{describe: "it stays disabled for the rest of AUTO", hold: func() bool { return !enabled() || !inAuto() },
				until: func() bool { s, _ := chaos.arena.MatchTime(); return s != arena.AutoPeriod }},
			{describe: "it is enabled again in teleop", within: 6 * time.Second, until: func() bool {
				s, _ := chaos.arena.MatchTime()
				return s == arena.TeleopPeriod && enabled()
			}},
		}
	case "estop":
		if !state.InMatch() {
			return nil
		}
		check.steps = []checkStep{
			{describe: "the E-stop disables the robot", within: 1500 * time.Millisecond, until: func() bool { return !enabled() }},
			{describe: "the arena latches the E-stop", within: 2 * time.Second, until: func() bool { return chaos.station(station).EStop }},
			{describe: "the robot stays disabled until the match ends", hold: func() bool { return !enabled() },
				until: func() bool { s, _ := chaos.arena.MatchTime(); return !s.InMatch() }},
		}
	case "foul":
		alliance := 0
		if station[0] == 'B' {
			alliance = 1
		}
		check.baseline = chaos.foulCount(alliance)
		check.steps = []checkStep{
			{describe: "the foul appears in the arena's score", within: 10 * time.Second, until: func() bool {
				return chaos.foulCount(alliance) > check.baseline
			}},
		}
	default:
		return nil // mechanical faults: nothing for the FMS to see
	}
	check.stepAt = time.Now()
	check.Step = check.steps[0].describe
	return check
}

// foulCount is the arena's foul list length for an alliance.
func (chaos *Chaos) foulCount(alliance int) int {
	var score struct {
		Red, Blue struct {
			Score struct{ Fouls []any }
		}
	}
	if raw := chaos.hub.Get("arena.realtimeScore"); raw != nil {
		_ = json.Unmarshal(raw, &score)
	}
	if alliance == 0 {
		return len(score.Red.Score.Fouls)
	}
	return len(score.Blue.Score.Fouls)
}

// evaluateChecks advances every waiting check (called from the scheduler tick). The conditions only read the arena
// client and the bus, never the chaos lock, so they run under it.
func (chaos *Chaos) evaluateChecks() {
	state, _ := chaos.arena.MatchTime()
	var finished []Check
	chaos.mu.Lock()
	for _, check := range chaos.checks {
		for check.Status == "waiting" {
			step := check.steps[check.index]
			now := time.Now()
			advance := false
			switch {
			case step.hold != nil:
				if step.until() {
					advance = true
				} else if !step.hold() {
					check.Status, check.Step = "fail", "broke: "+step.describe
				}
			case step.until():
				check.Detail += fmt.Sprintf("%s (%.1f s). ", step.describe, now.Sub(check.stepAt).Seconds())
				advance = true
			case now.Sub(check.stepAt) > step.within:
				if !state.InMatch() && state != arena.PostMatch {
					check.Status, check.Step = "n/a", "the match ended first"
				} else {
					check.Status, check.Step = "fail", fmt.Sprintf("not within %.1f s: %s", step.within.Seconds(), step.describe)
				}
			}
			if advance {
				check.index++
				check.stepAt = now
				if check.index >= len(check.steps) {
					check.Status = "pass"
				} else {
					check.Step = check.steps[check.index].describe
				}
			}
			if check.Status != "waiting" {
				finished = append(finished, *check)
			}
			if !advance {
				break
			}
		}
	}
	chaos.mu.Unlock()
	for _, check := range finished {
		switch check.Status {
		case "fail":
			chaos.hub.Warnf("chaos", "Recovery check FAILED for %s on %s (%d): %s", check.Fault, check.Station, check.Team,
				check.Step)
		case "pass":
			chaos.hub.Logf("chaos", "Recovery check passed for %s on %s: %s", check.Fault, check.Station, check.Detail)
		}
	}
	if len(finished) > 0 {
		chaos.publish()
	}
}

// ChecksSince returns the recovery checks started at or after t (for the soak report).
func (chaos *Chaos) ChecksSince(t time.Time) []Check {
	chaos.mu.Lock()
	defer chaos.mu.Unlock()
	var out []Check
	for _, check := range chaos.checks {
		if !check.Started.Before(t) {
			out = append(out, *check)
		}
	}
	return out
}
