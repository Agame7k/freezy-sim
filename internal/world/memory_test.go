package world

import (
	"math/rand"
	"testing"
)

// Thompson sampling settles on the option that keeps working, even when it is not the favoured one.
func TestChoiceSamplingConverges(t *testing.T) {
	world, _, _ := newTestWorld(t)
	stats := &TeamStats{}
	rng := rand.New(rand.NewSource(3))
	options := []string{"a", "b", "c"}
	payoff := map[string]float64{"a": 0.3, "b": 0.8, "c": 0.4}
	late := map[string]int{}
	for match := 0; match < 120; match++ {
		pick, _ := world.pickChoice(stats, "test", options, "a", 0.8, rng.Float64)
		reward := 0.0
		if rng.Float64() < payoff[pick] {
			reward = 1
		}
		stats.creditChoice("test", pick, reward)
		if match >= 80 {
			late[pick]++
		}
	}
	if late["b"] < 28 {
		t.Fatalf("late picks %v: did not settle on the best option", late)
	}
}

// Memories fade over matches and are eventually forgotten.
func TestIncidentsFade(t *testing.T) {
	stats := &TeamStats{Incidents: []*Incident{{Kind: "stuck", X: 1, Y: 1, Count: 2}}}
	for k := 0; k < 3; k++ {
		fadeMemories(stats)
	}
	if len(stats.Incidents) != 1 || stats.Incidents[0].Count >= 2 {
		t.Fatalf("after 3 matches: %+v", stats.Incidents)
	}
	for k := 0; k < 10; k++ {
		fadeMemories(stats)
	}
	if len(stats.Incidents) != 0 {
		t.Fatalf("never forgotten: %+v", stats.Incidents[0])
	}
}

// A remembered hazard makes the planner route around the spot.
func TestRememberedHazardChangesPath(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B2")
	robot.Team = 4242
	agent := NewAgent(robot, 1)
	clearField(world, robot)
	start, goal := Vec2{2.25, 2}, Vec2{2.25, 6}
	before := world.planPath(robot, agent, start, goal)
	middle := Vec2{2.25, 4.25} // the centre of a 0.5 m incident cell
	robot.Pos = middle
	for k := 0; k < 3; k++ {
		world.noteIncident(robot, "stuck", true)
	}
	agent.hazards = world.rememberedHazards(robot)
	after := world.planPath(robot, agent, start, goal)
	closest := func(path []Vec2) float64 {
		best := 99.0
		from := start
		for _, p := range path {
			for s := 0.0; s <= 1; s += 0.05 {
				q := from.Add(p.Sub(from).Scale(s))
				if d := q.Sub(middle).Len(); d < best {
					best = d
				}
			}
			from = p
		}
		return best
	}
	if closest(before) > 0.3 {
		t.Fatalf("the original path already avoided the spot (%.2f m)", closest(before))
	}
	if closest(after) < 0.4 {
		t.Fatalf("still drives through the remembered spot (%.2f m away)", closest(after))
	}
}

// Sloppier drivers high-center more often.
func TestMistakesScaleWithConsistency(t *testing.T) {
	world, _, _ := newTestWorld(t)
	robot := world.Robot("B2")
	robot.Team = 99
	bump := world.bumps[0]
	count := func(consistency float64) int {
		agent := NewAgent(robot, 7)
		agent.Personality.Consistency = consistency
		agent.mechReliability = 0.99
		n := 0
		for k := 0; k < 4000; k++ {
			robot.Faults = Faults{}
			robot.Pos, robot.Vel = bump.Center(), Vec2{3, 0}
			agent.naturalMistakes(robot, world, 1.0/30)
			if world.time < robot.Faults.StuckUntil {
				n++
			}
		}
		return n
	}
	careful, sloppy := count(0.95), count(0.3)
	if sloppy <= careful*2 {
		t.Fatalf("sloppy %d vs careful %d high-centers", sloppy, careful)
	}
}

// Fouls make a drive team more disciplined.
func TestFoulsTeachDiscipline(t *testing.T) {
	world, _, _ := newTestWorld(t)
	team := 1678
	before := world.personalityFor(team)
	robot := world.Robot("R1")
	robot.Team = team
	for k := 0; k < 3; k++ {
		world.noteIncident(robot, "foul G418", false)
	}
	after := world.personalityFor(team)
	if after.Discipline <= before.Discipline || after.Aggression >= before.Aggression {
		t.Fatalf("discipline %.2f→%.2f aggression %.2f→%.2f", before.Discipline, after.Discipline, before.Aggression,
			after.Aggression)
	}
}
