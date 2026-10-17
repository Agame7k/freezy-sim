package arena

import "time"

// The arena's audience display (static/js/audience_display.js) does not show a result straight away: switching to
// "score" first runs the screen transitions from the previous screen, waits 1 s, then plays the winner video
// (RedWins/BlueWins/Tie.mp4, 6.0 s) and only then fades the score in. The PLC's winner coils rise the moment the mode
// changes, so a light show timed to the coil would fire seconds before the audience sees who won; the arena leaves
// that delay-on to the PLC. These numbers mirror the display's transition code and were measured from the videos.
const (
	// winnerVideoStart is the logo → score transition's wait before the video, plus the time a local 11 MB video
	// takes to load and start playing (measured: the video played 2.83 s after a blank → score switch).
	winnerVideoStart = 1000*time.Millisecond + 100*time.Millisecond
	// WinnerVideoReveal is when the winner first shows in the video: the ice cube cracks open on a red or blue robot
	// (white for a tie) at 3.2 s and the "... WINS" caption follows at 3.5 s.
	WinnerVideoReveal = 3250 * time.Millisecond
	// WinnerVideoLength is the video's length; the display holds the last frame 0.5 s, waits 0.5 s, then fades the
	// final score in over 5 s.
	WinnerVideoLength = 6016 * time.Millisecond
	// queueGap is the display's pause between two queued transitions.
	queueGap = 100 * time.Millisecond
	// blankToScore is blank → logo (blinds close 1 s, 0.2 s, logo flip 0.5 s) plus 50 ms before logo → score.
	blankToScore = 1750 * time.Millisecond
)

// toScore is how long each screen takes to reach the start of the logo → score transition. Screens without a direct
// transition to "score" go through "blank" first.
var toScore = map[string]time.Duration{
	"logo":              0,
	"blank":             blankToScore,
	"logoLuma":          1000 * time.Millisecond, // blinds close, then logo → score
	"bracket":           1000 * time.Millisecond, // bracket fades out while the logo moves up
	"sponsor":           1750 * time.Millisecond, // sponsor → logo flip
	"match":             1800*time.Millisecond + queueGap + blankToScore,
	"intro":             2000*time.Millisecond + queueGap + blankToScore,
	"timeout":           2000*time.Millisecond + queueGap + blankToScore,
	"allianceSelection": 500*time.Millisecond + queueGap + blankToScore,
}

// ScoreRevealDelay is how long after the display switches to "score" from the given screen the audience sees who won.
// After a match the display is normally still on "match", which is the default.
func ScoreRevealDelay(previous string) time.Duration {
	before, ok := toScore[previous]
	if !ok {
		before = toScore["match"]
	}
	return before + winnerVideoStart + WinnerVideoReveal
}

// AudienceDisplay is the arena's audience display mode and the screen it was showing before.
type AudienceDisplay struct {
	Mode     string `json:"mode"`
	Previous string `json:"previous"`
}
