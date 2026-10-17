// Package scouting turns an event's results into what a real event's scouts and strategists produce: a per-robot,
// per-match scouting sheet, OPR-style ratings solved from the official alliance scores, and a pick list. The
// alliance-selection model (selection.go) picks from those numbers the way captains do.
package scouting

import (
	"math"
	"sort"
)

// MatchScore is one played match as the arena recorded it.
type MatchScore struct {
	Name      string `json:"name"`
	Red, Blue [3]int `json:"-"`
	// Points without fouls, so a team is not rated on what its opponents gave away.
	RedPoints, BluePoints int
	RedAuto, BlueAuto     int // auto FUEL + auto tower points
	RedTower, BlueTower   int // auto + endgame tower points
}

// Observation is one robot in one match as a scout saw it (a row of the scouting CSV).
type Observation struct {
	Match     string   `json:"match"`
	Team      int      `json:"team"`
	Station   string   `json:"station"`
	Present   bool     `json:"present"`
	Scored    int      `json:"scored"` // FUEL this robot put in its hub
	Passes    int      `json:"passes"`
	AutoClimb int      `json:"autoClimb"` // tower level at the end of auto
	EndClimb  int      `json:"endClimb"`  // tower level at the end of the match
	Faults    []string `json:"faults,omitempty"`
}

// Broke reports whether the robot had a problem the scouts would note (no-show, fault, dead on the field).
func (observation Observation) Broke() bool {
	return !observation.Present || len(observation.Faults) > 0
}

// TeamRating is one team's line on the pick list.
type TeamRating struct {
	Team   int `json:"team"`
	Rank   int `json:"rank"` // qualification rank (0 = unranked)
	Played int `json:"played"`
	// OPR-style contributions solved from alliance scores (least squares over every match).
	OPR      float64 `json:"opr"`
	AutoOPR  float64 `json:"autoOpr"`
	TowerOPR float64 `json:"towerOpr"`
	// From the scouting sheet.
	Scouted     int     `json:"scouted"` // matches with a scouting row
	AvgScored   float64 `json:"avgScored"`
	AvgPasses   float64 `json:"avgPasses"`
	AvgClimbPts float64 `json:"avgClimbPts"` // endgame tower points per match
	ClimbRate   float64 `json:"climbRate"`   // matches ending on the tower
	Reliability float64 `json:"reliability"` // matches without a breakdown
	// PickValue blends the two views and discounts unreliable robots; captains rank invitations by it.
	PickValue float64 `json:"pickValue"`
}

// EndgamePoints is the tower value of a level at the end of the match.
func EndgamePoints(level int) int {
	switch level {
	case 1:
		return 10
	case 2:
		return 20
	case 3:
		return 30
	}
	return 0
}

// OPR solves each team's average contribution to an alliance value (least squares, lightly regularized so a team
// with few matches does not get a wild number).
func OPR(matches []MatchScore, value func(match MatchScore, red bool) float64) map[int]float64 {
	index := map[int]int{}
	var teams []int
	for _, match := range matches {
		for _, team := range append(match.Red[:], match.Blue[:]...) {
			if _, ok := index[team]; !ok && team > 0 {
				index[team] = len(teams)
				teams = append(teams, team)
			}
		}
	}
	n := len(teams)
	if n == 0 {
		return map[int]float64{}
	}
	// Normal equations (AᵀA + λI) x = Aᵀb.
	ata := make([][]float64, n)
	for i := range ata {
		ata[i] = make([]float64, n)
		ata[i][i] = 0.1
	}
	atb := make([]float64, n)
	add := func(alliance [3]int, score float64) {
		for _, a := range alliance {
			ia, ok := index[a]
			if !ok {
				continue
			}
			atb[ia] += score
			for _, b := range alliance {
				if ib, ok := index[b]; ok {
					ata[ia][ib]++
				}
			}
		}
	}
	for _, match := range matches {
		add(match.Red, value(match, true))
		add(match.Blue, value(match, false))
	}
	x := solve(ata, atb)
	out := make(map[int]float64, n)
	for i, team := range teams {
		out[team] = x[i]
	}
	return out
}

// solve does Gaussian elimination with partial pivoting (the system is small and positive definite).
func solve(a [][]float64, b []float64) []float64 {
	n := len(b)
	for col := 0; col < n; col++ {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(a[row][col]) > math.Abs(a[pivot][col]) {
				pivot = row
			}
		}
		a[col], a[pivot] = a[pivot], a[col]
		b[col], b[pivot] = b[pivot], b[col]
		if math.Abs(a[col][col]) < 1e-12 {
			continue
		}
		for row := col + 1; row < n; row++ {
			factor := a[row][col] / a[col][col]
			if factor == 0 {
				continue
			}
			for k := col; k < n; k++ {
				a[row][k] -= factor * a[col][k]
			}
			b[row] -= factor * b[col]
		}
	}
	x := make([]float64, n)
	for row := n - 1; row >= 0; row-- {
		sum := b[row]
		for k := row + 1; k < n; k++ {
			sum -= a[row][k] * x[k]
		}
		if math.Abs(a[row][row]) > 1e-12 {
			x[row] = sum / a[row][row]
		}
	}
	return x
}

// Rate builds the pick list: every team in ranks, matches or observations, best PickValue first.
func Rate(matches []MatchScore, observations []Observation, ranks map[int]int) []TeamRating {
	opr := OPR(matches, func(m MatchScore, red bool) float64 {
		if red {
			return float64(m.RedPoints)
		}
		return float64(m.BluePoints)
	})
	auto := OPR(matches, func(m MatchScore, red bool) float64 {
		if red {
			return float64(m.RedAuto)
		}
		return float64(m.BlueAuto)
	})
	tower := OPR(matches, func(m MatchScore, red bool) float64 {
		if red {
			return float64(m.RedTower)
		}
		return float64(m.BlueTower)
	})
	ratings := map[int]*TeamRating{}
	get := func(team int) *TeamRating {
		if ratings[team] == nil {
			ratings[team] = &TeamRating{Team: team, Rank: ranks[team], Reliability: 1}
		}
		return ratings[team]
	}
	for team := range ranks {
		get(team)
	}
	for _, match := range matches {
		for _, team := range append(match.Red[:], match.Blue[:]...) {
			if team > 0 {
				get(team).Played++
			}
		}
	}
	type sums struct{ scored, passes, climbPts, climbs, broke float64 }
	totals := map[int]*sums{}
	for _, observation := range observations {
		if observation.Team <= 0 {
			continue
		}
		rating := get(observation.Team)
		rating.Scouted++
		s := totals[observation.Team]
		if s == nil {
			s = &sums{}
			totals[observation.Team] = s
		}
		s.scored += float64(observation.Scored)
		s.passes += float64(observation.Passes)
		s.climbPts += float64(EndgamePoints(observation.EndClimb))
		if observation.EndClimb > 0 {
			s.climbs++
		}
		if observation.Broke() {
			s.broke++
		}
	}
	var out []TeamRating
	for team, rating := range ratings {
		rating.OPR, rating.AutoOPR, rating.TowerOPR = opr[team], auto[team], tower[team]
		if s := totals[team]; s != nil && rating.Scouted > 0 {
			n := float64(rating.Scouted)
			rating.AvgScored, rating.AvgPasses = s.scored/n, s.passes/n
			rating.AvgClimbPts, rating.ClimbRate = s.climbPts/n, s.climbs/n
			rating.Reliability = 1 - s.broke/n
		}
		rating.PickValue = pickValue(*rating)
		out = append(out, *rating)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PickValue != out[j].PickValue {
			return out[i].PickValue > out[j].PickValue
		}
		return out[i].Team < out[j].Team
	})
	return out
}

// pickValue is what a strategist would rank on: the OPR and the scouted contribution (FUEL roughly scores a point
// each; about half of it lands while the hub is active) blended by how much data there is, discounted for breakdowns.
func pickValue(rating TeamRating) float64 {
	scouted := 0.5*rating.AvgScored + rating.AvgClimbPts
	value := scouted
	switch {
	case rating.Scouted == 0:
		value = rating.OPR
	case rating.Played > 0:
		weight := math.Min(1, float64(rating.Played)/6) * 0.5
		value = weight*rating.OPR + (1-weight)*scouted
	}
	return value * (0.4 + 0.6*rating.Reliability)
}
