package scenario

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Agame7k/freezy-sim/internal/arena"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/ds"
	"github.com/Agame7k/freezy-sim/internal/fieldio"
)

// Match timeline (game/match_timing.go): auto 0–20, pause 20–23, transition 23–33, shifts 1–4 of 25 s each
// (33–58, 58–83, 83–108, 108–133), endgame 133–163. Hub shift indexes follow the arena's game.Shift order.
const (
	shiftAuto = iota
	shiftTransition
	shift1
	shift2
	shift3
	shift4
	shiftEndgame
	shiftPostMatch
)

const (
	shift1Start  = 33.0
	shift2Start  = 58.0
	teleopEnd    = 163.0
	scoringGrace = 3.0  // game.ScoringGracePeriodSec
	motorRunOut  = 10.0 // ScoringGracePeriodSec + MotorsOnExtraPeriodSec
)

func shiftCount(counts []int, shift int) int {
	if shift < len(counts) {
		return counts[shift]
	}
	return -1
}

// hubColor classifies one hub's sACN pixels by their average color: off, red, blue, white or mixed.
func hubColor(pixels string) string {
	raw, err := hex.DecodeString(pixels)
	if err != nil || len(raw) < 3 {
		return "unknown"
	}
	var r, g, b int
	n := len(raw) / 3
	for i := 0; i < n; i++ {
		r += int(raw[3*i])
		g += int(raw[3*i+1])
		b += int(raw[3*i+2])
	}
	r, g, b = r/n, g/n, b/n
	switch {
	case r < 12 && g < 12 && b < 12:
		return "off"
	case r > 40 && g > 40 && b > 40 && max(r, g, b) < 2*min(r, g, b):
		return "white"
	case r > 2*b && r > 2*g:
		return "red"
	case b > 2*r && b > 2*g:
		return "blue"
	}
	return "mixed"
}

func (t *T) hubColors() (red, blue string) {
	var hubs map[string]string
	if !t.Topic("sacn.hubs", &hubs) {
		return "unknown", "unknown"
	}
	return hubColor(hubs["red"]), hubColor(hubs["blue"])
}

// waitPostMatch waits for the match to end on its own and returns when the arena entered post-match.
func (t *T) waitPostMatch(timeout time.Duration) time.Time {
	t.WaitFor("match ends", timeout, func() bool { s, _ := t.MatchState(); return s == arena.PostMatch })
	return time.Now()
}

func moreScenarios() []Scenario {
	return []Scenario{
		{
			Id: "grace", Name: "Scoring grace periods at shift boundaries",
			Description: "FUEL landing within 3 s after an active shift counts for that shift; an inactive shift gets no grace.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				// Red wins auto, so red is inactive in shift 1 and active in shift 2; blue is the reverse.
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 9})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 3})
				// 1.5 s into shift 1: both still count in the transition shift's grace (transition is active for both).
				t.WaitMatchTime(shift1Start+1.5, 45*time.Second)
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 2})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 2})
				// 1.5 s into shift 2: blue's shift-1 grace (scores); red had no grace for its inactive shift 1, so its
				// FUEL lands in shift 2 (active, scores).
				t.WaitMatchTime(shift2Start+1.5, 30*time.Second)
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 3})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 3})
				// After blue's grace runs out it is inactive: shift-2 FUEL is counted but scores nothing.
				t.WaitMatchTime(shift2Start+scoringGrace+1.5, 10*time.Second)
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 4})
				time.Sleep(1500 * time.Millisecond)

				score := t.score()
				red, blue := score.Red.Score.Hub.ShiftCounts, score.Blue.Score.Hub.ShiftCounts
				t.Check(score.Red.Score.Hub.WonAuto, "red won auto (9 vs 3)")
				t.Check(shiftCount(red, shiftTransition) == 2, "red FUEL 1.5 s into shift 1 counted in the transition grace (counts %v)", red)
				t.Check(shiftCount(blue, shiftTransition) == 2, "blue FUEL 1.5 s into shift 1 counted in the transition grace (counts %v)", blue)
				t.Check(shiftCount(red, shift1) == 0 && shiftCount(red, shift2) == 3,
					"red (inactive in shift 1) gets no grace: its early shift-2 FUEL is shift 2 (counts %v)", red)
				t.Check(shiftCount(blue, shift1) == 3, "blue's FUEL 1.5 s into shift 2 counted in its shift-1 grace (counts %v)", blue)
				t.Check(shiftCount(blue, shift2) == 4, "blue's FUEL after the grace counted in shift 2 (counts %v)", blue)
				t.Check(score.Red.ScoreSummary.TeleopFuelPoints == 5, "red teleop FUEL points 2 + 3 = 5, got %d", score.Red.ScoreSummary.TeleopFuelPoints)
				t.Check(score.Blue.ScoreSummary.TeleopFuelPoints == 5, "blue teleop FUEL points 2 + 3 = 5 (inactive-shift 4 score nothing), got %d",
					score.Blue.ScoreSummary.TeleopFuelPoints)
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "endofmatch", Name: "End of match: disable, post-match grace, motor run-out, LEDs",
			Description: "A full match: robots disable at 2:43, FUEL within 3 s still scores, later FUEL does not, hub motors " +
				"run 10 s to exhaust balls, and the hubs go white for the scoring assessment.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.WaitMatchTime(teleopEnd-10, 180*time.Second)
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 4})
				t.Cmd("field.fuel", map[string]any{"alliance": 1, "sensor": -1, "count": 4})
				ended := t.waitPostMatch(20 * time.Second)
				latency := t.WaitFor("every robot disabled at the end of the match", 2*time.Second, func() bool {
					return t.AllStations(func(_ string, c ds.Command) bool { return !c.Enabled })
				})
				t.Check(latency <= 650*time.Millisecond, "robots disabled within one control period of the end (%d ms)", latency.Milliseconds())
				t.WaitFor("hubs white for the scoring assessment", 2*time.Second, func() bool {
					red, blue := t.hubColors()
					return red == "white" && blue == "white"
				})

				time.Sleep(time.Until(ended.Add(1500 * time.Millisecond)))
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 2})
				time.Sleep(time.Until(ended.Add(time.Duration((scoringGrace + 1.5) * float64(time.Second)))))
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 5})

				time.Sleep(time.Until(ended.Add(time.Duration((motorRunOut - 2) * float64(time.Second)))))
				coils := t.Coils()
				t.Check(coils[fieldio.CoilRedHubMotor] && coils[fieldio.CoilBlueHubMotor], "hub motors still running %.0f s after the match", motorRunOut-2)
				t.WaitFor("hub motors stop after the run-out", 4*time.Second, func() bool {
					coils := t.Coils()
					return !coils[fieldio.CoilRedHubMotor] && !coils[fieldio.CoilBlueHubMotor]
				})

				score := t.score()
				red := score.Red.Score.Hub.ShiftCounts
				t.Check(shiftCount(red, shiftEndgame) == 4, "endgame FUEL counted (counts %v)", red)
				t.Check(shiftCount(red, shiftPostMatch) == 2, "FUEL 1.5 s after the end counted in the post-match grace (counts %v)", red)
				t.Check(score.Red.ScoreSummary.TeleopFuelPoints == 6,
					"red teleop FUEL 4 + 2 = 6; FUEL after the grace scores nothing (got %d)", score.Red.ScoreSummary.TeleopFuelPoints)
			},
		},
		{
			Id: "leds-shifts", Name: "Hub LEDs follow the active shift",
			Description: "With red winning auto: mid shift 1 only blue's hub is lit, mid shift 2 only red's.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 6})
				t.WaitMatchTime(shift1Start+10, 50*time.Second)
				t.WaitFor("shift 1: red hub dark, blue hub blue", 2*time.Second, func() bool {
					red, blue := t.hubColors()
					return red == "off" && blue == "blue"
				})
				t.WaitMatchTime(shift2Start+10, 30*time.Second)
				t.WaitFor("shift 2: red hub red, blue hub dark", 2*time.Second, func() bool {
					red, blue := t.hubColors()
					return red == "red" && blue == "off"
				})
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "tiegamedata", Name: "Auto tie picks one winner for everyone",
			Description: "With no FUEL in auto the arena picks the winner at random; every station gets the same game " +
				"data and it matches the arena's hub state.",
			Run: func(t *T) {
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.WaitMatchTime(23.2, 25*time.Second)
				var data string
				t.WaitFor("every station has the same game data", 3*time.Second, func() bool {
					data = t.Station("R1").GameData
					return data != "" && t.AllStations(func(_ string, c ds.Command) bool { return c.GameData == data })
				})
				score := t.score()
				t.Check(score.Red.Score.Hub.WonAuto != score.Blue.Score.Hub.WonAuto, "exactly one alliance won auto")
				t.Check((data == "R") == score.Red.Score.Hub.WonAuto, "game data %q matches the arena's winner (red won %v)",
					data, score.Red.Score.Hub.WonAuto)
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "legacyds", Name: "Legacy driver station protocol",
			Description: "R2 switches to the legacy DS protocol, relinks, and is enabled and disabled like the others.",
			Run: func(t *T) {
				legacy := t.dsStates()["R2"].Settings.Legacy
				defer t.Cmd("ds.set", map[string]any{"station": "R2", "field": "legacy", "value": legacy})
				t.ReadyField()
				t.Cmd("ds.set", map[string]any{"station": "R2", "field": "legacy", "value": !legacy})
				t.Logf("R2 now on the %s protocol", map[bool]string{true: "legacy", false: "current"}[!legacy])
				t.WaitReady(30 * time.Second)
				t.StartMatch()
				t.WaitFor("R2 enabled in auto", 1500*time.Millisecond, func() bool { c := t.Station("R2"); return c.Enabled && c.Auto })
				t.WaitMatchTime(20.2, 25*time.Second)
				t.WaitFor("R2 disabled at the end of auto", 1500*time.Millisecond, func() bool { return !t.Station("R2").Enabled })
				t.Cmd("match.abort", nil)
			},
		},
		{
			Id: "plchealth", Name: "PLC loss blocks the start",
			Description: "The PLC stops answering: the arena reports it unhealthy and refuses to start; it recovers when the PLC returns.",
			Run: func(t *T) {
				if t.deps.FieldIO.Profile() != config.ProfilePLC {
					t.Skip("field profile is not the Modbus PLC")
				}
				defer t.Cmd("field.plcOffline", map[string]any{"offline": false})
				t.ReadyField()
				t.Cmd("field.plcOffline", map[string]any{"offline": true})
				t.WaitFor("arena reports the PLC unhealthy and blocks the start", 6*time.Second, func() bool {
					return hasCondition(t.Status(), "PLC is not healthy") && !t.Status().CanStartMatch
				})
				t.Cmd("field.plcOffline", map[string]any{"offline": false})
				t.WaitFor("PLC healthy again", 10*time.Second, func() bool { return t.Status().PlcIsHealthy })
				t.WaitReady(20 * time.Second)
			},
		},
		{
			Id: "armorblocks", Name: "Each ArmorBlock drop blocks the start",
			Description: "Each of the four field IO ArmorBlocks is disconnected in turn; the arena names it as a start blocker.",
			Run: func(t *T) {
				if t.deps.FieldIO.Profile() != config.ProfilePLC {
					t.Skip("field profile is not the Modbus PLC")
				}
				defer func() {
					for i := range fieldio.ArmorBlockNames {
						t.Cmd("field.set", map[string]any{"control": "armorBlock", "index": i, "value": true})
					}
				}()
				t.ReadyField()
				for i, name := range fieldio.ArmorBlockNames {
					t.Cmd("field.set", map[string]any{"control": "armorBlock", "index": i, "value": false})
					fragment := fmt.Sprintf("ArmorBlock %q", string(name[0]-'a'+'A')+name[1:])
					t.WaitFor("arena names "+name+" as a blocker", 4*time.Second, func() bool {
						return hasCondition(t.Status(), fragment) && !t.Status().CanStartMatch
					})
					t.Cmd("field.set", map[string]any{"control": "armorBlock", "index": i, "value": true})
					t.WaitFor(name+" blocker clears", 4*time.Second, func() bool { return !hasCondition(t.Status(), fragment) })
				}
				t.WaitReady(20 * time.Second)
			},
		},
		{
			Id: "estopbox", Name: "E-stop box health is reported",
			Description: "The red alliance's ESP32 E-stop box goes offline: the arena shows it unhealthy, then healthy when it returns.",
			Run: func(t *T) {
				if t.deps.FieldIO.Profile() != config.ProfileAltIO {
					t.Skip("field profile is not alternate IO")
				}
				defer t.Cmd("field.box", map[string]any{"name": "red", "online": true})
				t.ReadyField()
				if !t.Status().RedEstopsEnabled {
					t.Skip("the arena has no red E-stop box configured")
				}
				t.Cmd("field.box", map[string]any{"name": "red", "online": false})
				t.WaitFor("arena shows the red E-stop box unhealthy", 10*time.Second, func() bool { return !t.Status().RedEstopsIsHealthy })
				if t.Status().CanStartMatch {
					t.Logf("NOTE: the arena still allows a match start with the red alliance E-stops unreachable")
				}
				t.Cmd("field.box", map[string]any{"name": "red", "online": true})
				t.WaitFor("red E-stop box healthy again", 10*time.Second, func() bool { return t.Status().RedEstopsIsHealthy })
			},
		},
		{
			Id: "winnerlights", Name: "Winner coils pulse when the score is shown",
			Description: "A committed result is shown on the audience display: the winner's coil (redWins/blueWins, both " +
				"for a tie) pulses for 10 s, the loser's stays off, and the PLC's winner light show runs on past the pulse.",
			Run: func(t *T) {
				if coils := t.deps.Arena.PlcIo().Coils; len(coils) > 0 && len(coils) < fieldio.CoilCount {
					t.Skip("this arena build has no winner coils")
				}
				t.ReadyField()
				t.manualAll()
				t.StartMatch()
				t.Cmd("field.fuel", map[string]any{"alliance": 0, "sensor": -1, "count": 30})
				t.WaitMatchTime(6, 15*time.Second)
				t.Cmd("match.abort", nil)
				t.waitPostMatch(10 * time.Second)
				time.Sleep(time.Second)
				// The referees may have called fouls, so the winner is whatever the arena scored, not the FUEL alone.
				score := t.score()
				red, blue := score.Red.ScoreSummary.Score, score.Blue.ScoreSummary.Score
				wantRed, wantBlue := red >= blue, blue >= red
				t.Logf("final score red %d, blue %d", red, blue)
				t.Cmd("match.commit", nil)
				time.Sleep(time.Second)
				// Mode changes only act on a change, so leave "score" first (commit already shows it).
				t.Cmd("match.audienceDisplay", map[string]string{"mode": "blank"})
				time.Sleep(300 * time.Millisecond)
				t.Cmd("match.audienceDisplay", map[string]string{"mode": "score"})
				defer t.Cmd("match.audienceDisplay", map[string]string{"mode": "blank"})
				lit := func() bool { coils := t.Coils(); return coils[fieldio.CoilRedWins] || coils[fieldio.CoilBlueWins] }
				if !t.Poll(3*time.Second, lit) {
					arenaCoils := t.deps.Arena.PlcIo().Coils
					t.Fatalf("no winner coil came on (the arena's own PLC view has %d coils: %v)", len(arenaCoils), arenaCoils)
				}
				on := time.Now()
				time.Sleep(200 * time.Millisecond) // both coils of a tie arrive in one write, but give it a cycle
				coils := t.Coils()
				t.Check(coils[fieldio.CoilRedWins] == wantRed && coils[fieldio.CoilBlueWins] == wantBlue,
					"winner coils match the result (red %d, blue %d): redWins=%v blueWins=%v",
					red, blue, coils[fieldio.CoilRedWins], coils[fieldio.CoilBlueWins])
				show := t.deps.FieldIO.Field.WinnerShow()
				t.Check(show != nil && show.Red == wantRed && show.Blue == wantBlue, "the PLC starts the winner show (got %+v)", show)
				// The display left "blank", so its winner video reveals the result after blank → logo → score.
				reveal := arena.ScoreRevealDelay("blank")
				t.Check(show != nil && show.RevealMs == reveal.Milliseconds(),
					"the show's delay-on waits for the video's reveal %.2f s after the coil (got %+v)", reveal.Seconds(), show)
				t.WaitFor("winner coils drop after their pulse", 13*time.Second, func() bool { return !lit() })
				pulse := time.Since(on)
				t.Check(pulse > 9*time.Second && pulse < 11500*time.Millisecond, "the coil pulse lasts about 10 s (%.1f s)", pulse.Seconds())
				t.Check(t.deps.FieldIO.Field.WinnerShow() != nil, "the PLC's light show keeps running after the pulse")
			},
		},
	}
}
