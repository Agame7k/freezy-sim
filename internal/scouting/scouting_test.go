package scouting

import (
	"math"
	"math/rand"
	"testing"
)

// With noiseless scores that are exact sums of team contributions, OPR recovers each contribution.
func TestOPRRecoversContributions(t *testing.T) {
	truth := map[int]float64{}
	teams := []int{}
	for i := 1; i <= 12; i++ {
		teams = append(teams, i*10)
		truth[i*10] = float64(5 * i)
	}
	rng := rand.New(rand.NewSource(3))
	var matches []MatchScore
	for m := 0; m < 40; m++ {
		order := rng.Perm(len(teams))
		var match MatchScore
		for k := 0; k < 3; k++ {
			match.Red[k], match.Blue[k] = teams[order[k]], teams[order[k+3]]
			match.RedPoints += int(truth[match.Red[k]])
			match.BluePoints += int(truth[match.Blue[k]])
		}
		matches = append(matches, match)
	}
	opr := OPR(matches, func(m MatchScore, red bool) float64 {
		if red {
			return float64(m.RedPoints)
		}
		return float64(m.BluePoints)
	})
	for team, want := range truth {
		if math.Abs(opr[team]-want) > 0.5 {
			t.Errorf("team %d: OPR %.2f, want %.0f", team, opr[team], want)
		}
	}
}

// Unreliable robots drop down the pick list even with the same scoring.
func TestPickValueDiscountsBreakdowns(t *testing.T) {
	var observations []Observation
	for m := 0; m < 8; m++ {
		observations = append(observations,
			Observation{Team: 1, Present: true, Scored: 30},
			Observation{Team: 2, Present: true, Scored: 30, Faults: map[bool][]string{true: {"brownout"}}[m%2 == 0]})
	}
	ratings := Rate(nil, observations, nil)
	if ratings[0].Team != 1 || ratings[1].Reliability != 0.5 {
		t.Fatalf("pick list %+v", ratings)
	}
}

func testField(n int) ([]int, map[int]TeamRating) {
	ranked := make([]int, n)
	ratings := map[int]TeamRating{}
	for i := range ranked {
		ranked[i] = 100 + i
		ratings[ranked[i]] = TeamRating{Team: ranked[i], PickValue: float64(n - i), AvgClimbPts: float64(i % 3 * 10)}
	}
	return ranked, ratings
}

// Every alliance is full, nobody is on two alliances, and a team that declined is never a pick (only a captain).
func TestSelectionIsLegal(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		ranked, ratings := testField(40)
		for _, round3 := range []string{"", "L"} {
			cfg := SelectionConfig{Alliances: 8, Round2Order: "L", Round3Order: round3, DeclineChance: 0.9}
			selection, err := Select(ranked, ratings, cfg, rand.New(rand.NewSource(seed)))
			if err != nil {
				t.Fatal(err)
			}
			size := 3
			if round3 != "" {
				size = 4
			}
			seen := map[int]bool{}
			declined := map[int]bool{}
			for _, team := range selection.Declined {
				declined[team] = true
			}
			for i, alliance := range selection.Alliances {
				if len(alliance) != size {
					t.Fatalf("seed %d: alliance %d has %v", seed, i+1, alliance)
				}
				for slot, team := range alliance {
					if seen[team] {
						t.Fatalf("seed %d: team %d on two alliances", seed, team)
					}
					seen[team] = true
					if slot > 0 && declined[team] {
						t.Fatalf("seed %d: team %d declined but was picked by alliance %d", seed, team, i+1)
					}
				}
			}
			if selection.Alliances[0][0] != ranked[0] {
				t.Fatalf("seed %d: the top seed is not the captain of alliance 1", seed)
			}
		}
	}
}

// With a high decline chance, declines happen, and only to teams that went on to captain.
func TestDeclinersBecomeCaptains(t *testing.T) {
	ranked, ratings := testField(40)
	declines := 0
	for seed := int64(1); seed <= 30; seed++ {
		selection, err := Select(ranked, ratings, SelectionConfig{Alliances: 8, Round2Order: "L", DeclineChance: 1},
			rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatal(err)
		}
		captains := map[int]bool{}
		for _, alliance := range selection.Alliances {
			captains[alliance[0]] = true
		}
		for _, team := range selection.Declined {
			declines++
			if !captains[team] {
				t.Fatalf("seed %d: team %d declined but never captained", seed, team)
			}
		}
	}
	if declines == 0 {
		t.Fatal("no team ever declined")
	}
}
