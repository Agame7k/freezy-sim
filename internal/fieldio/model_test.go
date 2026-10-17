package fieldio

import "testing"

// A match-reset pulse that arrives after the match has started must only discard pre-match FUEL.
func TestLateResetKeepsInMatchFuel(t *testing.T) {
	field := newField()
	for i := 0; i < 10; i++ {
		field.ScoreFuel(0, 1) // pre-match practice shots
	}
	field.MatchStarted(true)
	field.ScoreFuel(0, 2) // scored in the first instant of auto
	field.ScoreFuel(1, 0)
	field.SetCoils([CoilCount]bool{CoilMatchReset: true}, "test")
	registers := field.Registers()
	if registers[RegRedHubTotal] != 1 || registers[RegBlueHubTotal] != 1 {
		t.Fatalf("late reset should keep the 2 in-match FUEL, got red=%d blue=%d", registers[RegRedHubTotal], registers[RegBlueHubTotal])
	}
}

// A reset pulse before the match (match load) clears everything.
func TestPreMatchResetClears(t *testing.T) {
	field := newField()
	field.ScoreFuel(0, 0)
	field.SetCoils([CoilCount]bool{CoilMatchReset: true}, "test")
	if field.Registers()[RegRedHubTotal] != 0 {
		t.Fatal("pre-match reset should clear the counters")
	}
}
