package app

import (
	"strings"
	"testing"

	"github.com/Agame7k/freezy-sim/internal/world"
)

// A score where the arena put every count in the right shift.
func matchingScore(truth world.MatchTruth, wonAuto [2]bool) arenaScoreDoc {
	var doc arenaScoreDoc
	for alliance := 0; alliance < 2; alliance++ {
		counts := make([]int, len(shiftNames))
		for _, t := range truth.Counts[alliance] {
			if shift, ok := world.ScoringShift(t+0.1, wonAuto[alliance]); ok {
				counts[shift]++
			}
		}
		score := &doc.Red.Score
		if alliance == 1 {
			score = &doc.Blue.Score
		}
		score.Hub.WonAuto = wonAuto[alliance]
		score.Hub.ShiftCounts = counts
		for i := 0; i < 3; i++ {
			if truth.AutoLevels[alliance*3+i] > 0 {
				score.AutoTowerStatuses[i] = 1
			}
			score.EndgameTowerStatuses[i] = truth.EndLevels[alliance*3+i]
		}
		for k := 0; k < truth.Fouls[alliance]; k++ {
			score.Fouls = append(score.Fouls, []byte("{}"))
		}
	}
	return doc
}

func sampleTruth() world.MatchTruth {
	truth := world.MatchTruth{AutoTaken: true, EndTaken: true, Fouls: [2]int{1, 0}}
	for t := 1.0; t < world.TeleopEndSec; t += 0.7 {
		truth.Counts[0] = append(truth.Counts[0], t)
		truth.Counts[1] = append(truth.Counts[1], t+0.3)
	}
	truth.AutoLevels[1] = 1
	truth.EndLevels = [6]int{2, 0, 1, 3, 0, 0}
	return truth
}

func TestArenaScoreThatMatchesPasses(t *testing.T) {
	truth := sampleTruth()
	lines := compareArenaScore(truth, matchingScore(truth, [2]bool{true, false}), true, true, false)
	for _, line := range lines {
		if line.Severity == "error" {
			t.Fatalf("unexpected difference: %+v", line)
		}
	}
}

func TestArenaScoreDifferencesAreFound(t *testing.T) {
	truth := sampleTruth()
	cases := map[string]func(doc *arenaScoreDoc){
		"lost FUEL": func(doc *arenaScoreDoc) { doc.Red.Score.Hub.ShiftCounts[3] -= 2 },
		"wrong shift": func(doc *arenaScoreDoc) {
			doc.Blue.Score.Hub.ShiftCounts[2] += 4
			doc.Blue.Score.Hub.ShiftCounts[3] -= 4
		},
		"missing climb":      func(doc *arenaScoreDoc) { doc.Blue.Score.EndgameTowerStatuses[0] = 0 },
		"missing auto tower": func(doc *arenaScoreDoc) { doc.Red.Score.AutoTowerStatuses[1] = 0 },
		"missing foul":       func(doc *arenaScoreDoc) { doc.Red.Score.Fouls = nil },
	}
	for name, spoil := range cases {
		doc := matchingScore(truth, [2]bool{true, false})
		spoil(&doc)
		found := false
		for _, line := range compareArenaScore(truth, doc, true, true, false) {
			found = found || line.Severity == "error"
		}
		if !found {
			t.Errorf("%s: not caught", name)
		}
	}
}

// A count right on a shift boundary may land on either side without being called a bug.
func TestShiftBoundaryIsTolerated(t *testing.T) {
	truth := world.MatchTruth{}
	boundary := world.TeleopStartSec + world.TransitionSec + world.GraceSec // transition (active) grace ends
	truth.Counts[0] = []float64{boundary - 0.1}
	for _, shift := range []int{1, 2} { // either the transition or shift 1
		var doc arenaScoreDoc
		doc.Red.Score.Hub.WonAuto = true
		doc.Red.Score.Hub.ShiftCounts = make([]int, len(shiftNames))
		doc.Red.Score.Hub.ShiftCounts[shift] = 1
		doc.Blue.Score.Hub.ShiftCounts = make([]int, len(shiftNames))
		for _, line := range compareArenaScore(truth, doc, false, false, false) {
			if line.Severity == "error" {
				t.Fatalf("boundary count in %s flagged: %s", shiftNames[shift], line.Text)
			}
		}
	}
	// ...but not three shifts away.
	var doc arenaScoreDoc
	doc.Red.Score.Hub.ShiftCounts = make([]int, len(shiftNames))
	doc.Red.Score.Hub.ShiftCounts[5] = 1
	doc.Blue.Score.Hub.ShiftCounts = make([]int, len(shiftNames))
	errors := 0
	for _, line := range compareArenaScore(truth, doc, false, false, false) {
		if line.Severity == "error" && strings.Contains(line.Text, "SHIFT") {
			errors++
		}
	}
	if errors == 0 {
		t.Fatal("a count three shifts off was not flagged")
	}
}
