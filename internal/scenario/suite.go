package scenario

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/ds"
	"github.com/Agame7k/freezy-sim/internal/fieldio"
	"github.com/Agame7k/freezy-sim/internal/world"
)

type scoreSummary struct {
	AutoFuelPoints    int
	AutoTowerPoints   int
	TeleopFuelPoints  int
	TeleopTowerPoints int
	Score             int
}

type allianceScore struct {
	Score struct {
		Hub struct {
			WonAuto     bool
			ShiftCounts []int
		}
	}
	ScoreSummary scoreSummary
}

type realtimeScore struct {
	Red  allianceScore
	Blue allianceScore
}

func (t *T) score() realtimeScore {
	var score realtimeScore
	t.Topic("arena.realtimeScore", &score)
	return score
}

func (t *T) dsStates() map[string]ds.State {
	var states []ds.State
	t.Topic("ds.stations", &states)
	byName := map[string]ds.State{}
	for _, state := range states {
		byName[state.Station] = state
	}
	return byName
}

// manualAll parks every robot (no AI) so scenarios control exactly what is scored.
func (t *T) manualAll() {
	for _, station := range arena.Stations {
		t.Cmd("world.robot", map[string]any{"station": station, "manual": true})
	}
}

func hasCondition(status arena.ArenaStatus, fragment string) bool {
	for _, condition := range status.StartMatchConditions {
		if strings.Contains(condition, fragment) {
			return true
		}
	}
	return false
}

// Suite is the built-in FMS acceptance suite.
func Suite() []Scenario {
	return append([]Scenario{
		{
			Id: "blockers", Name: "Start-match blockers and bypass",
			Description: "A pressed team E-stop and a missing driver station each block the start; bypass clears the missing robot.",
			Run: func(t *T) {
				t.ReadyField()
				t.Cmd("field.set", map[string]any{"control": "teamEStop", "index": 0, "value": true})
				t.WaitFor("arena reports the R1 emergency stop as a blocker", 3*time.Second, func() bool {
					return hasCondition(t.Status(), "emergency stop") && !t.Status().CanStartMatch
				})
				t.Cmd("field.set", map[string]any{"control": "teamEStop", "index": 0, "value": false})
				t.WaitFor("E-stop blocker clears once released (pre-match)", 3*time.Second, func() bool {
					return !hasCondition(t.Status(), "emergency stop")
				})
				t.Cmd("ds.set", map[string]any{"station": "B3", "field": "dsOn", "value": false})
				t.WaitFor("arena reports B3 not connected", 4*time.Second, func() bool {
					return hasCondition(t.Status(), "not all robots are connected")
				})
				t.Cmd("match.bypass", map[string]any{"station": "B3"})
				t.WaitFor("bypassing B3 makes the field startable", 4*time.Second, func() bool { return t.Status().CanStartMatch })
				t.Cmd("match.bypass", map[string]any{"station": "B3"})
				t.Cmd("ds.set", map[string]any{"station": "B3", "field": "dsOn", "value": true})
				t.WaitReady(20 * time.Second)
			},
		},
		{
			Id: "timeline", Name: "Enable/disable timeline",
			Description: "Robots enable in auto, disable in the pause, and re-enable in teleop within one control period.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitFor("all six robots enabled in auto", 1500*time.Millisecond, func() bool {
					return t.AllStations(func(_ string, c ds.Command) bool { return c.Enabled && c.Auto })
				})
				t.WaitMatchTime(20.2, 25*time.Second)
				t.WaitFor("all robots disabled at the end of auto", 1500*time.Millisecond, func() bool {
					return t.AllStations(func(_ string, c ds.Command) bool { return !c.Enabled })
				})
				t.WaitMatchTime(23.1, 6*time.Second)
				t.WaitFor("all robots enabled in teleop", 1500*time.Millisecond, func() bool {
					return t.AllStations(func(_ string, c ds.Command) bool { return c.Enabled && !c.Auto })
				})
				t.Check(t.Station("R1").RemainingSeconds > 130, "FMS reports teleop time remaining (%ds)", t.Station("R1").RemainingSeconds)
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "astop", Name: "A-stop disables only during auto",
			Description: "R1's A-stop disables just R1 for the rest of auto (even after release); R1 re-enables in teleop.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitMatchTime(3, 6*time.Second)
				t.Cmd("field.set", map[string]any{"control": "teamAStop", "index": 0, "value": true})
				t.WaitFor("R1 disabled by its A-stop", time.Second, func() bool { return !t.Station("R1").Enabled })
				t.Check(t.Station("R2").Enabled && t.Station("B1").Enabled, "other robots stay enabled")
				t.Cmd("field.set", map[string]any{"control": "teamAStop", "index": 0, "value": false})
				time.Sleep(1500 * time.Millisecond)
				t.Check(!t.Station("R1").Enabled, "R1 stays disabled for the rest of auto after release")
				t.WaitMatchTime(23.3, 25*time.Second)
				t.WaitFor("R1 enabled again in teleop", 1500*time.Millisecond, func() bool { return t.Station("R1").Enabled })
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "estop", Name: "Team E-stop latches for the match",
			Description: "B2's E-stop disables it immediately and it stays disabled after the button is released.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitMatchTime(24, 30*time.Second)
				t.Cmd("field.set", map[string]any{"control": "teamEStop", "index": 4, "value": true})
				t.WaitFor("B2 disabled by its E-stop", time.Second, func() bool { return !t.Station("B2").Enabled })
				t.Cmd("field.set", map[string]any{"control": "teamEStop", "index": 4, "value": false})
				time.Sleep(2 * time.Second)
				t.Check(!t.Station("B2").Enabled && t.ArenaStation("B2").EStop, "B2 stays E-stopped after release")
				t.Check(t.Station("B1").Enabled, "B1 is unaffected")
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "fieldestop", Name: "Field E-stop aborts the match",
			Description: "The scoring-table field E-stop ends the match within a second and stops the hub motors.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitMatchTime(5, 8*time.Second)
				t.Cmd("field.set", map[string]any{"control": "fieldEStop", "index": 0, "value": true})
				t.WaitFor("match aborted by field E-stop", 2*time.Second, func() bool {
					state, _ := t.MatchState()
					return state == arena.PostMatch
				})
				t.WaitFor("hub motors stopped", 2*time.Second, func() bool {
					coils := t.Coils()
					return !coils[fieldio.CoilRedHubMotor] && !coils[fieldio.CoilBlueHubMotor]
				})
				// The arena sends no immediate control packet on an abort, so robots disable on the next periodic
				// packet (up to 500 ms later). Measure it and require at most one control period (+ margin).
				latency := t.WaitFor("every robot disabled", 2*time.Second, func() bool {
					return t.AllStations(func(_ string, c ds.Command) bool { return !c.Enabled })
				})
				t.Check(latency <= 650*time.Millisecond, "robots disabled within one control period of the field E-stop (%.0f ms)",
					float64(latency.Milliseconds()))
				if latency > 100*time.Millisecond {
					t.Logf("NOTE: Freezy Arena waits for its next periodic DS packet after an abort (%.0f ms here); an immediate packet would disable robots sooner",
						float64(latency.Milliseconds()))
				}
				t.Cmd("field.set", map[string]any{"control": "fieldEStop", "index": 0, "value": false})
			},
		},
		{
			Id: "gamedata", Name: "Game data names the auto winner",
			Description: "Red scores more FUEL in auto; every driver station receives game data \"R\" at the start of teleop.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 9})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 3})
				t.Check(t.AllStations(func(_ string, c ds.Command) bool { return c.GameData == "" }), "no game data during auto")
				t.WaitMatchTime(23.2, 25*time.Second)
				allR := func() bool { return t.AllStations(func(_ string, c ds.Command) bool { return c.GameData == "R" }) }
				if t.Poll(3*time.Second, allR) {
					t.Logf("all driver stations show game data R")
				} else {
					score := t.score()
					var seen []string
					for _, station := range arena.Stations {
						seen = append(seen, fmt.Sprintf("%s=%q", station, t.Station(station).GameData))
					}
					t.Errorf("not every driver station shows game data R: %s (arena auto counts red %v blue %v, red won auto %v)",
						strings.Join(seen, " "), score.Red.Score.Hub.ShiftCounts, score.Blue.Score.Hub.ShiftCounts,
						score.Red.Score.Hub.WonAuto)
				}
				t.Check(t.score().Red.Score.Hub.WonAuto, "arena marks red as the auto winner")
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "shifts", Name: "Hub shifts and inactive-hub scoring",
			Description: "FUEL is attributed to auto, transition and shift 1; the auto winner's shift-1 FUEL scores nothing.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 5})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 3})
				t.WaitMatchTime(24.5, 25*time.Second)
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 4})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 4})
				t.WaitMatchTime(38.5, 20*time.Second) // past shift 1 start + the 3 s transition grace
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 6})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 6})
				time.Sleep(1500 * time.Millisecond)
				score := t.score()
				red, blue := score.Red.Score.Hub.ShiftCounts, score.Blue.Score.Hub.ShiftCounts
				t.Check(len(red) > 2 && red[0] == 5 && red[1] == 4 && red[2] == 6, "red per-shift counts auto/transition/shift1 = %v", red)
				t.Check(len(blue) > 2 && blue[0] == 3 && blue[1] == 4 && blue[2] == 6, "blue per-shift counts = %v", blue)
				t.Check(score.Red.ScoreSummary.AutoFuelPoints == 5 && score.Red.ScoreSummary.TeleopFuelPoints == 4,
					"red (auto winner, inactive in shift 1) scores 5 auto + 4 teleop, got %d + %d",
					score.Red.ScoreSummary.AutoFuelPoints, score.Red.ScoreSummary.TeleopFuelPoints)
				t.Check(score.Blue.ScoreSummary.TeleopFuelPoints == 10, "blue (active in shift 1) scores 4 + 6 teleop, got %d",
					score.Blue.ScoreSummary.TeleopFuelPoints)
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "reconnect", Name: "Driver station reconnect mid-match",
			Description: "R3's DS drops out mid-auto and comes back; the arena relinks it and the robot re-enables.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitMatchTime(4, 8*time.Second)
				t.Cmd("ds.set", map[string]any{"station": "R3", "field": "dsOn", "value": false})
				t.WaitFor("arena sees R3's DS drop", 4*time.Second, func() bool {
					c := t.ArenaStation("R3").DsConn
					return c == nil || !c.DsLinked
				})
				t.Cmd("ds.set", map[string]any{"station": "R3", "field": "dsOn", "value": true})
				t.WaitFor("R3 reconnected, linked and enabled", 10*time.Second, func() bool {
					c := t.ArenaStation("R3").DsConn
					return c != nil && c.RobotLinked && t.Station("R3").Enabled
				})
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "radio", Name: "Robot radio loss and recovery",
			Description: "B1's radio reboots: the arena shows radio/robot link lost, then recovers when it boots.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				t.WaitMatchTime(3, 6*time.Second)
				t.Cmd("ds.rebootRadio", map[string]any{"station": "B1"})
				t.WaitFor("arena shows B1 radio down", 3*time.Second, func() bool {
					c := t.ArenaStation("B1").DsConn
					return c != nil && !c.RadioLinked && !c.RobotLinked
				})
				t.Check(!t.Station("B1").Enabled, "B1 is not enabled without comms")
				t.WaitFor("B1 radio back and robot enabled", 35*time.Second, func() bool {
					c := t.ArenaStation("B1").DsConn
					return c != nil && c.RadioLinked && c.RobotLinked && t.Station("B1").Enabled
				})
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "wrongteam", Name: "Team not in the match is rejected",
			Description: "A driver station for a team that is not in the loaded match is refused by the FMS.",
			Run: func(t *T) {
				t.ReadyField()
				t.Cmd("ds.set", map[string]any{"station": "R2", "field": "team", "value": 9999})
				t.WaitFor("FMS rejects team 9999 (not in match)", 6*time.Second, func() bool {
					state := t.dsStates()["R2"]
					return state.AssignStatus == ds.AssignNotInMatch
				})
				t.WaitFor("arena blocks the start for the empty R2", 4*time.Second, func() bool {
					return hasCondition(t.Status(), "not all robots are connected")
				})
				t.Cmd("ds.set", map[string]any{"station": "R2", "field": "team", "value": 0})
				t.WaitFor("R2 rejoins with the right team", 15*time.Second, func() bool { return t.Status().CanStartMatch })
			},
		},
		{
			Id: "stacklight", Name: "Field stack light and buzzer",
			Description: "Ready field: green + buzzer; in match: green, no buzzer; after the match: orange until scores are committed.",
			Run: func(t *T) {
				t.ReadyField()
				t.WaitFor("buzzer sounds when the field is ready", 3*time.Second, func() bool { return t.Coils()[fieldio.CoilStackBuzzer] })
				t.StartMatch()
				t.WaitFor("green on, buzzer off during the match", 2*time.Second, func() bool {
					coils := t.Coils()
					return coils[fieldio.CoilStackGreen] && !coils[fieldio.CoilStackBuzzer]
				})
				t.Cmd("match.abort", nil)
				t.WaitFor("orange (scores pending) after the match", 3*time.Second, func() bool { return t.Coils()[fieldio.CoilStackOrange] })
			},
		},
		{
			Id: "leds", Name: "Hub LEDs over sACN",
			Description: "The arena lights both hubs during auto (startup sequence) through the sACN controller.",
			Run: func(t *T) {
				t.ReadyField()
				t.StartMatch()
				lit := func(alliance string) bool {
					var hubs map[string]string
					if !t.Topic("sacn.hubs", &hubs) {
						return false
					}
					return strings.Trim(hubs[alliance], "0") != ""
				}
				t.WaitFor("both hubs lit during auto", 4*time.Second, func() bool { return lit("red") && lit("blue") })
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "network", Name: "Managed network configured per match",
			Description: "Each station's SSID on the access point and VLAN on the field switch match the loaded teams.",
			Run: func(t *T) {
				if t.deps.NetworkEnabled == nil || !t.deps.NetworkEnabled() {
					t.Skip("network emulation is off")
				}
				t.ReadyField()
				var state struct {
					AP struct {
						Status   string `json:"status"`
						Stations map[string]struct {
							Ssid     string `json:"ssid"`
							IsLinked bool   `json:"isLinked"`
						} `json:"stations"`
					} `json:"ap"`
					Switch struct {
						Vlans map[string]int `json:"vlans"`
					} `json:"switch"`
				}
				teams := t.deps.Arena.Teams()
				// The arena removes and re-adds the team VLANs with pauses in between; wait for it to finish.
				t.WaitFor("switch VLANs configured for the loaded teams", 20*time.Second, func() bool {
					if !t.Topic("network.state", &state) {
						return false
					}
					for i, station := range arena.Stations {
						if state.Switch.Vlans[station] != teams[i] {
							return false
						}
					}
					return state.AP.Status == "ACTIVE"
				})
				apNames := []string{"red1", "red2", "red3", "blue1", "blue2", "blue3"}
				t.Check(state.AP.Status == "ACTIVE", "access point ACTIVE (%s)", state.AP.Status)
				for i, station := range arena.Stations {
					ap := state.AP.Stations[apNames[i]]
					t.Check(ap.Ssid == itoa(teams[i]) && ap.IsLinked, "%s SSID %s linked=%v (team %d)", station, ap.Ssid, ap.IsLinked, teams[i])
					t.Check(state.Switch.Vlans[station] == teams[i], "%s VLAN subnet for team %d", station, teams[i])
				}
			},
		},
		{
			Id: "tower", Name: "Auto-referee scores an auto climb",
			Description: "B3 climbs tower level 1 in auto; the auto-referee enters it and the arena awards 15 points.",
			Run: func(t *T) {
				t.ReadyField()
				// Let the field crew finish wheeling the robots on: a match started mid-reset finishes it instantly and puts every
				// robot back on its start pose, which would undo the teleport.
				t.WaitFor("field reset finished", 40*time.Second, func() bool {
					carting, crew := t.deps.World.FieldResetBusy()
					return !carting && !crew
				})
				t.manualAll()
				// B3 climbs at the spot its game plan gave it (slot 0 is the front of the tower; -1 and +1 the rung ends).
				slot := 0
				t.deps.World.WithRobot("B3", func(r *world.Robot, _ *world.World) { slot = r.ClimbSlot })
				zone, heading := t.deps.World.ClimbZone(world.Blue, slot)
				t.Cmd("world.teleport", map[string]any{"station": "B3", "x": zone.X, "y": zone.Y, "heading": heading})
				t.StartMatch()
				deadline := time.Now().Add(12 * time.Second)
				climbed := false
				linedUp := time.Now()
				for time.Now().Before(deadline) && !climbed {
					// Something can nudge B3 off its line-up (the climb needs it within 0.3 m and 0.4 rad): put it back.
					started := false
					t.deps.World.WithRobot("B3", func(r *world.Robot, _ *world.World) {
						started = r.Climbing || r.ClimbLevel > 0
						if r.ClimbSlot != slot { // (re-planned meanwhile)
							slot = r.ClimbSlot
							zone, heading = t.deps.World.ClimbZone(world.Blue, slot)
						}
					})
					if !started && time.Since(linedUp) > time.Second {
						t.Cmd("world.teleport", map[string]any{"station": "B3", "x": zone.X, "y": zone.Y, "heading": heading})
						linedUp = time.Now()
					}
					t.Cmd("world.drive", map[string]any{"station": "B3", "climb": 1})
					var summary world.Summary
					if t.Topic("world.state", &summary) {
						for _, robot := range summary.Robots {
							if robot.Station == "B3" && robot.ClimbLevel >= 1 {
								climbed = true
							}
						}
					}
					time.Sleep(250 * time.Millisecond)
				}
				if !climbed {
					t.deps.World.WithRobot("B3", func(r *world.Robot, w *world.World) {
						t.Logf("B3 %s (climb level %d, slot %d) at %.2f,%.2f heading %.2f, zone %.2f,%.2f heading %.2f; climbing %v enabled %v faults %+v",
							r.Arch.Name, r.Arch.ClimbLevel, r.ClimbSlot, r.Pos.X, r.Pos.Y, r.Heading, zone.X, zone.Y, heading, r.Climbing, r.Enabled, r.Faults)
					})
				}
				t.Check(climbed, "B3 reached tower level 1 during auto")
				t.WaitMatchTime(21.5, 20*time.Second)
				t.WaitFor("arena awards blue 15 auto tower points", 5*time.Second, func() bool {
					return t.score().Blue.ScoreSummary.AutoTowerPoints == 15
				})
				t.Cmd("match.abort", nil)
			},
		},
	}, moreScenarios()...)
}

func itoa(n int) string {
	if n == 0 {
		return ""
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
