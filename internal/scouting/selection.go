package scouting

import (
	"fmt"
	"math/rand"
	"sort"
)

// SelectionConfig mirrors the arena's playoff settings.
type SelectionConfig struct {
	Alliances   int    // number of playoff alliances
	Round2Order string // "L": the last alliance picks first in round 2 (serpentine); "F": alliance 1 first
	Round3Order string // "" = no third round (3-team alliances)
	// DeclineChance is how likely a team that would otherwise be a captain declines the last alliance's invitation;
	// it scales down to zero for alliance 1 (nobody turns down the top seed).
	DeclineChance float64
}

// Step is one thing that happens on the alliance-selection stage, in order.
type Step struct {
	Kind     string `json:"kind"` // captain, invite-declined, pick
	Alliance int    `json:"alliance"`
	Slot     int    `json:"slot"` // 0 = captain, then picks in order
	Team     int    `json:"team"`
	Text     string `json:"text"`
}

// Selection is the result: each alliance's teams (captain first) and the steps that produced it.
type Selection struct {
	Alliances [][]int `json:"alliances"`
	Steps     []Step  `json:"steps"`
	Declined  []int   `json:"declined,omitempty"`
}

// Select runs alliance selection. ranked is the qualification ranking (best first); ratings feed each captain's
// pick list. Each captain judges with its own scouting noise, so two captains do not share one list, and values
// tower help more once its alliance already has scoring.
func Select(ranked []int, ratings map[int]TeamRating, cfg SelectionConfig, rng *rand.Rand) (Selection, error) {
	if cfg.Alliances <= 0 {
		return Selection{}, fmt.Errorf("no playoff alliances configured")
	}
	rounds := 2
	if cfg.Round3Order != "" {
		rounds = 3
	}
	if len(ranked) < cfg.Alliances*(rounds+1) {
		return Selection{}, fmt.Errorf("%d ranked teams is not enough for %d alliances of %d", len(ranked), cfg.Alliances, rounds+1)
	}
	rankOf := map[int]int{}
	for i, team := range ranked {
		rankOf[team] = i
	}
	selection := Selection{Alliances: make([][]int, cfg.Alliances)}
	taken := map[int]bool{}
	declined := map[int]bool{}
	// Each captain's private noise on every team, fixed for the whole selection.
	opinion := make([]map[int]float64, cfg.Alliances)
	for i := range opinion {
		opinion[i] = map[int]float64{}
		for _, team := range ranked {
			value := ratings[team].PickValue
			opinion[i][team] = value * (1 + 0.12*rng.NormFloat64())
		}
	}

	// fillCaptain makes the highest-ranked team still available the alliance's captain (declined teams may still
	// captain; that is why they declined).
	fillCaptain := func(alliance int) {
		if len(selection.Alliances[alliance]) > 0 {
			return
		}
		for _, team := range ranked {
			if !taken[team] {
				taken[team] = true
				selection.Alliances[alliance] = []int{team}
				selection.Steps = append(selection.Steps, Step{Kind: "captain", Alliance: alliance + 1, Team: team,
					Text: fmt.Sprintf("Team %d (rank %d) is the captain of alliance %d", team, rankOf[team]+1, alliance+1)})
				return
			}
		}
	}
	// wouldCaptain reports whether team would still become a captain if it declined now.
	wouldCaptain := func(team int) bool {
		openSeats := 0
		for _, members := range selection.Alliances {
			if len(members) == 0 {
				openSeats++
			}
		}
		ahead := 0
		for _, other := range ranked {
			if other == team {
				return ahead < openSeats
			}
			if !taken[other] {
				ahead++
			}
		}
		return false
	}
	climbPoints := func(alliance int) float64 {
		total := 0.0
		for _, team := range selection.Alliances[alliance] {
			total += ratings[team].AvgClimbPts
		}
		return total
	}

	order := func(round int) []int {
		forward := true
		switch round {
		case 2:
			forward = cfg.Round2Order == "F"
		case 3:
			forward = cfg.Round3Order == "F"
		}
		seq := make([]int, cfg.Alliances)
		for i := range seq {
			if forward {
				seq[i] = i
			} else {
				seq[i] = cfg.Alliances - 1 - i
			}
		}
		return seq
	}

	for round := 1; round <= rounds; round++ {
		for _, alliance := range order(round) {
			fillCaptain(alliance)
			for {
				candidates := []int{}
				for _, team := range ranked {
					if !taken[team] && !declined[team] {
						candidates = append(candidates, team)
					}
				}
				if len(candidates) == 0 {
					return selection, fmt.Errorf("alliance %d has nobody left to pick", alliance+1)
				}
				// A captain wants FUEL first; once the alliance scores, a strong climber is worth more to it.
				needClimb := climbPoints(alliance) < 15*float64(len(selection.Alliances[alliance]))
				score := func(team int) float64 {
					value := opinion[alliance][team]
					if needClimb {
						value += 0.3 * ratings[team].AvgClimbPts
					}
					return value
				}
				sort.SliceStable(candidates, func(i, j int) bool { return score(candidates[i]) > score(candidates[j]) })
				choice := candidates[0]
				if round == 1 && wouldCaptain(choice) && cfg.DeclineChance > 0 {
					chance := cfg.DeclineChance * float64(alliance) / float64(max(1, cfg.Alliances-1))
					if rng.Float64() < chance {
						declined[choice] = true
						selection.Declined = append(selection.Declined, choice)
						selection.Steps = append(selection.Steps, Step{Kind: "invite-declined", Alliance: alliance + 1,
							Team: choice, Text: fmt.Sprintf("Team %d respectfully declines alliance %d's invitation", choice, alliance+1)})
						continue
					}
				}
				taken[choice] = true
				selection.Alliances[alliance] = append(selection.Alliances[alliance], choice)
				selection.Steps = append(selection.Steps, Step{Kind: "pick", Alliance: alliance + 1,
					Slot: len(selection.Alliances[alliance]) - 1, Team: choice,
					Text: fmt.Sprintf("Alliance %d picks team %d (rank %d)", alliance+1, choice, rankOf[choice]+1)})
				break
			}
		}
	}
	return selection, nil
}
