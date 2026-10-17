package world

// Personality is how a team's drive team plays, separate from what its robot can do. Every team gets a consistent
// one (derived from its team number), and the user can override it per team.
type Personality struct {
	Aggression  float64 `json:"aggression"`  // 0–1: appetite for defense and contact
	Discipline  float64 `json:"discipline"`  // 0–1: how carefully it follows the rules (pin release, tower contact)
	Risk        float64 `json:"risk"`        // 0–1: how close it cuts early/late shots around shift changes
	Consistency float64 `json:"consistency"` // 0–1: how repeatable its driving is (low = overshoots, fumbles)
	Reaction    float64 `json:"reaction"`    // seconds of driver reaction time
	Curiosity   float64 `json:"curiosity"`   // 0–1: how readily it tries something new (autos, shot range, roles)
}

// Difficulty presets scale every team's skill.
var Difficulties = []string{"rookie", "regional", "championship", "einstein"}

type difficulty struct {
	skill, consistency, reaction float64
}

var difficultyScales = map[string]difficulty{
	"rookie":       {skill: 0.75, consistency: 0.65, reaction: 1.6},
	"regional":     {skill: 1.0, consistency: 1.0, reaction: 1.0},
	"championship": {skill: 1.15, consistency: 1.2, reaction: 0.75},
	"einstein":     {skill: 1.3, consistency: 1.35, reaction: 0.55},
}

// hashUnit maps a team number and a salt to a repeatable value in [0, 1).
func hashUnit(team int, salt uint32) float64 {
	hash := uint32(team)*2246822519 ^ salt
	hash ^= hash >> 15
	hash *= 2654435761
	hash ^= hash >> 13
	return float64(hash%10000) / 10000
}

// personalityForTeam gives every team a consistent drive team.
func personalityForTeam(team int) Personality {
	return Personality{
		Aggression:  0.15 + 0.8*hashUnit(team, 0xa11ce),
		Discipline:  0.35 + 0.65*hashUnit(team, 0xd15c),
		Risk:        0.1 + 0.8*hashUnit(team, 0x415c),
		Consistency: 0.45 + 0.55*hashUnit(team, 0xc0de),
		Reaction:    0.12 + 0.2*hashUnit(team, 0x5eed),
		Curiosity:   0.1 + 0.8*hashUnit(team, 0xc0c0),
	}
}

// scaled applies a difficulty preset.
func (personality Personality) scaled(level string) Personality {
	scale, ok := difficultyScales[level]
	if !ok {
		return personality
	}
	personality.Consistency = clamp(personality.Consistency*scale.consistency, 0.05, 1)
	personality.Reaction *= scale.reaction
	return personality
}
