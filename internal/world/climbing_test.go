package world

import "testing"

// bestOption picks a robot's best endgame option on its own (no coach).
func bestOption(options []ClimbOption) ClimbOption {
	best := ClimbOption{Level: -1}
	for _, option := range options {
		if (option.Feasible || option.Level == 0) && (best.Level < 0 || option.Total() > best.Total()) {
			best = option
		}
	}
	return best
}

func endgameRobot(t *testing.T, arch string, scored int, activeTime float64) (*World, *Robot, *Agent) {
	world, _, _ := newTestWorld(t)
	robot := world.robots[0]
	robot.Arch = archetypeByName(arch)
	robot.Skill = 1
	robot.Scored, robot.Shots = scored, scored*3/2+1
	robot.Hopper = 0
	agent := NewAgent(robot, 1)
	agent.Personality = Personality{Risk: 0.5, Discipline: 0.8, Consistency: 0.9, Reaction: 0.2}
	agent.activeTime = activeTime
	world.controllers[0] = agent
	zone, _ := world.climbZone(robot.Alliance, robot.Index%3-1)
	robot.Pos = zone.Add(Vec2{-3, 0})
	if robot.Alliance == Red {
		robot.Pos = zone.Add(Vec2{3, 0})
	}
	return world, robot, agent
}

func TestStrongScorerSkipsCheapClimb(t *testing.T) {
	// A dumper that has been scoring ~2 FUEL/s can only reach level 1 (10 points): it keeps scoring.
	world, robot, agent := endgameRobot(t, "dumper", 140, 70)
	best := bestOption(agent.climbOptions(robot, world, 30))
	if best.Level != 0 {
		t.Errorf("a 2 FUEL/s scorer should keep scoring rather than climb for 10 points, chose level %d (%+v)",
			best.Level, agent.climbOptions(robot, world, 30))
	}
}

func TestWeakScorerClimbsHigh(t *testing.T) {
	// A tall robot that has scored little this match climbs to level 3 (30 points).
	world, robot, agent := endgameRobot(t, "tall", 6, 70)
	best := bestOption(agent.climbOptions(robot, world, 30))
	if best.Level != 3 {
		t.Errorf("a slow scorer that can reach level 3 should climb, chose level %d (%+v)", best.Level,
			agent.climbOptions(robot, world, 30))
	}
}

func TestNoClimbWithoutTime(t *testing.T) {
	world, robot, agent := endgameRobot(t, "tall", 6, 70)
	for _, option := range agent.climbOptions(robot, world, 4) {
		if option.Level > 1 && option.Feasible {
			t.Errorf("level %d should not be feasible with 4 s left", option.Level)
		}
	}
}

// planAlliance runs the coach's endgame decision for three level-3 robots that score fast enough that, on points alone,
// shooting edges out climbing. It returns how many it sends up the tower.
func planAllianceClimbs(t *testing.T, matchType int) (int, []ClimbOption) {
	world, _, _ := newTestWorld(t)
	var robots []*Robot
	var agents []*Agent
	for i := 0; i < 3; i++ {
		robot := world.robots[i]
		robot.Arch = archetypeByName("tall")
		robot.Skill, robot.MatchType, robot.Enabled = 1, matchType, true
		robot.Scored, robot.Shots = 240, 300
		agent := NewAgent(robot, int64(i+1))
		agent.Personality = Personality{Risk: 0.5, Consistency: 0.9, Reaction: 0.2}
		agent.activeTime = 70
		world.controllers[i] = agent
		zone, _ := world.climbZone(robot.Alliance, robot.Index%3-1)
		robot.Pos = zone.Add(Vec2{3, 0})
		robots, agents = append(robots, robot), append(agents, agent)
	}
	world.coaches[Red].planClimbs(world, robots, agents, 32, TeleopEndSec-32)
	climbers := 0
	for _, agent := range agents {
		if agent.climbPlan > 0 {
			climbers++
		}
	}
	return climbers, agents[0].climbOptionsSeen
}

func TestCoachGoesForTraversalBonus(t *testing.T) {
	quals, options := planAllianceClimbs(t, 2)
	if options[0].Total() <= options[3].Total() {
		t.Fatalf("test setup: shooting should beat a level 3 climb on points alone (%+v)", options)
	}
	if quals < 2 {
		t.Errorf("in quals the alliance should climb for the Traversal bonus (50 tower points); sent %d robots up", quals)
	}
	if playoffs, _ := planAllianceClimbs(t, 3); playoffs != 0 {
		t.Errorf("in playoffs ranking points do not matter, so these fast scorers should shoot; sent %d robots up", playoffs)
	}
}
