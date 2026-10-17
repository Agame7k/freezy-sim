package world

// REBUILT match timing and FUEL scoring windows, mirroring Freezy Arena (game/match_timing.go, game/hub.go). Times are
// match seconds since the start of auto.
const (
	AutoSec        = 20.0
	PauseSec       = 3.0
	TransitionSec  = 10.0
	ShiftSec       = 25.0
	EndgameSec     = 30.0
	GraceSec       = 3.0 // FUEL counted this long after an active period ends still scores for it
	TeleopStartSec = AutoSec + PauseSec
	TeleopSec      = TransitionSec + 4*ShiftSec + EndgameSec
	TeleopEndSec   = TeleopStartSec + TeleopSec
	LightWarnSec   = 3.0 // the hub light blinks for the last 3 s of an active period
)

// Arena shifts, in order.
const (
	ShiftAuto = iota
	ShiftTransition
	Shift1
	Shift2
	Shift3
	Shift4
	ShiftEndgame
	ShiftPost
)

// ShiftActive reports whether an alliance's hub counts FUEL in a shift (the auto winner sits out shifts 1 and 3).
func ShiftActive(shift int, wonAuto bool) bool {
	switch shift {
	case ShiftAuto, ShiftTransition, ShiftEndgame, ShiftPost:
		return true
	case Shift1, Shift3:
		return !wonAuto
	case Shift2, Shift4:
		return wonAuto
	}
	return false
}

// shiftEnd is when a shift's own clock time ends (before any grace).
func shiftEnd(shift int) float64 {
	switch shift {
	case ShiftAuto:
		return AutoSec
	case ShiftTransition:
		return TeleopStartSec + TransitionSec
	case Shift1, Shift2, Shift3, Shift4:
		return TeleopStartSec + TransitionSec + float64(shift-ShiftTransition)*ShiftSec
	case ShiftEndgame:
		return TeleopEndSec
	}
	return TeleopEndSec + GraceSec
}

// ScoringShift is the shift FUEL counted at match time t is credited to: an active shift keeps counting for GraceSec
// after it ends, an inactive one does not.
func ScoringShift(t float64, wonAuto bool) (int, bool) {
	if t < 0 {
		return 0, false
	}
	for shift := ShiftAuto; shift <= ShiftEndgame; shift++ {
		end := shiftEnd(shift)
		if shift != ShiftEndgame && ShiftActive(shift, wonAuto) {
			end += GraceSec
		}
		if t < end {
			return shift, true
		}
	}
	if t < TeleopEndSec+GraceSec {
		return ShiftPost, true
	}
	return 0, false
}

// FuelScores reports whether FUEL counted at match time t earns points for an alliance.
func FuelScores(t float64, wonAuto bool) bool {
	shift, ok := ScoringShift(t, wonAuto)
	return ok && ShiftActive(shift, wonAuto)
}

// ActiveAt reports whether an alliance's hub is active (lit) at match time t, ignoring the grace period.
func ActiveAt(t float64, wonAuto bool) bool {
	switch {
	case t < AutoSec:
		return true
	case t < TeleopStartSec:
		return false // pause
	case t >= TeleopEndSec:
		return false
	}
	for shift := ShiftTransition; shift <= ShiftEndgame; shift++ {
		if t < shiftEnd(shift) {
			return ShiftActive(shift, wonAuto)
		}
	}
	return false
}

// TowerPoints are the points for a robot's tower level in auto (level 1 only) or at the end of the match.
func TowerPoints(level int, auto bool) int {
	if auto {
		if level >= 1 {
			return 15
		}
		return 0
	}
	switch {
	case level >= 3:
		return 30
	case level == 2:
		return 20
	case level == 1:
		return 10
	}
	return 0
}
