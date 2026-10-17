package world

import (
	"encoding/json"
	"fmt"
	"time"
)

type robotRequest struct {
	Station   string  `json:"station"`
	Archetype string  `json:"archetype"`
	Skill     float64 `json:"skill"`
	Manual    *bool   `json:"manual"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Heading   float64 `json:"heading"`
}

// DriveRequest is a manual control frame from the GUI (keyboard or gamepad).
type DriveRequest struct {
	Station string  `json:"station"`
	Vx      float64 `json:"vx"` // field-relative, m/s
	Vy      float64 `json:"vy"`
	Omega   float64 `json:"omega"`
	Intake  bool    `json:"intake"`
	Shoot   bool    `json:"shoot"`
	Climb   int     `json:"climb"`
}

func (world *World) registerCommands() {
	world.bus.Handle("world.reset", func(_ []byte) (any, error) {
		world.Reset()
		return nil, nil
	})
	world.bus.Handle("world.pause", func(args []byte) (any, error) {
		var request struct {
			Paused bool `json:"paused"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		world.paused = request.Paused
		world.mu.Unlock()
		return nil, nil
	})
	world.bus.Handle("world.archetypes", func(_ []byte) (any, error) { return Archetypes, nil })
	world.bus.Handle("world.fuelReport", func(_ []byte) (any, error) {
		// Where the free FUEL is, and how much of it no robot can reach (tuning aid for "balls get trapped" issues).
		world.mu.Lock()
		defer world.mu.Unlock()
		l := world.layout
		report := map[string]int{}
		for _, ball := range world.balls {
			if ball.State != BallFree {
				continue
			}
			p := Vec2{ball.P[0], ball.P[1]}
			blueLine := l.Hub.Positions["blue"][0]
			redLine := l.Field.Length - blueLine
			switch {
			case p.X < blueLine-0.6:
				report["blueZone"]++
			case p.X > redLine+0.6:
				report["redZone"]++
			default:
				report["neutral"]++
			}
			if p.X < 0.3 || p.X > l.Field.Length-0.3 || p.Y < 0.3 || p.Y > l.Field.Width-0.3 {
				report["againstWalls"]++
			}
			if !world.Reachable(p, 0.5) {
				report["unreachable"]++
			}
			if (p.X < 0.5 || p.X > l.Field.Length-0.5) && (p.Y < 0.5 || p.Y > l.Field.Width-0.5) {
				report["corners"]++
			}
			if ball.P[2] > world.ballRadius*2 {
				report["airborne"]++
			}
		}
		return report, nil
	})
	world.bus.Handle("world.options", func(_ []byte) (any, error) {
		return map[string]any{"archetypes": Archetypes, "strategies": Strategies, "autos": AutoRoutines,
			"starts": StartSpots, "inactive": InactiveRoles}, nil
	})
	world.bus.Handle("world.plan", func(args []byte) (any, error) {
		// Override a robot's strategy/auto/start ("" = let the alliance game plan decide). Applies at the next reset.
		var request struct {
			Station  string  `json:"station"`
			Strategy *string `json:"strategy"`
			Auto     *string `json:"auto"`
			Start    *string `json:"start"`
			Inactive *string `json:"inactive"`
			Apply    bool    `json:"apply"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		robot := world.Robot(request.Station)
		if robot == nil {
			world.mu.Unlock()
			return nil, fmt.Errorf("unknown station %q", request.Station)
		}
		override := &world.overrides[robot.Index]
		plan := &world.plans[robot.Index]
		if request.Strategy != nil {
			override.Strategy = *request.Strategy
			if *request.Strategy != "" {
				plan.Strategy = *request.Strategy
			}
		}
		if request.Auto != nil {
			override.Auto = *request.Auto
			if *request.Auto != "" {
				plan.Auto = *request.Auto
			}
		}
		if request.Start != nil {
			override.Start = *request.Start
		}
		if request.Inactive != nil {
			override.Inactive = *request.Inactive
			if *request.Inactive != "" {
				plan.Inactive = *request.Inactive
			}
		}
		world.markDirtyLocked()
		world.mu.Unlock()
		if request.Apply {
			world.Reset()
		}
		return nil, nil
	})
	world.bus.Handle("world.robot", func(args []byte) (any, error) {
		var request robotRequest
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		robot := world.Robot(request.Station)
		if robot == nil {
			return nil, fmt.Errorf("unknown station %q", request.Station)
		}
		// Designs and skill belong to the team, so they follow it into later matches.
		// "team" hands the design back to the team (it builds its own again); anything else is set by hand and locked.
		switch {
		case request.Archetype == "team" && robot.Team > 0:
			world.setTeamDesignLocked(robot.Team, "", false)
			robot.Arch = world.designForLocked(robot.Team)
		case request.Archetype == "team":
		case request.Archetype != "":
			robot.Arch = archetypeByName(request.Archetype)
			if robot.Team > 0 {
				world.setTeamDesignLocked(robot.Team, robot.Arch.Name, true)
			}
		}
		if request.Skill > 0 {
			robot.Skill = request.Skill
			if robot.Team > 0 {
				world.teamSkill[robot.Team] = request.Skill
			}
		}
		if request.Archetype != "" || request.Skill > 0 {
			world.markDirtyLocked()
		}
		if request.Manual != nil {
			robot.Manual = *request.Manual
			robot.Ctl = Control{}
		}
		return nil, nil
	})
	world.bus.Handle("world.teleport", func(args []byte) (any, error) {
		var request robotRequest
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		robot := world.Robot(request.Station)
		if robot == nil {
			return nil, fmt.Errorf("unknown station %q", request.Station)
		}
		robot.Pos = Vec2{request.X, request.Y}
		robot.Heading = request.Heading
		robot.Vel = Vec2{}
		robot.ClimbLevel, robot.ClimbProg, robot.Climbing = 0, 0, false
		return nil, nil
	})
	world.bus.Handle("world.drive", func(args []byte) (any, error) {
		var request DriveRequest
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		robot := world.Robot(request.Station)
		if robot == nil {
			return nil, fmt.Errorf("unknown station %q", request.Station)
		}
		robot.Manual = true
		robot.Ctl = Control{Vel: Vec2{request.Vx, request.Vy}, Omega: request.Omega, Intake: request.Intake,
			Shoot: request.Shoot, Climb: request.Climb}
		world.manualAt[robot.Index] = time.Now()
		return nil, nil
	})
	world.bus.Handle("world.chute", func(args []byte) (any, error) {
		var request struct {
			Alliance string `json:"alliance"`
			Open     bool   `json:"open"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		world.outposts[allianceIndex(request.Alliance)].DoorOpen = request.Open
		return nil, nil
	})
	world.bus.Handle("world.humanPlayer", func(args []byte) (any, error) {
		var request struct {
			Alliance string   `json:"alliance"`
			Mode     string   `json:"mode"`
			Accuracy *float64 `json:"accuracy"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		switch request.Mode {
		case "", "auto", "feed", "score", "idle":
		default:
			return nil, fmt.Errorf("unknown human player mode %q", request.Mode)
		}
		world.mu.Lock()
		defer world.mu.Unlock()
		outpost := world.outposts[allianceIndex(request.Alliance)]
		if request.Mode != "" {
			outpost.HumanMode = request.Mode
		}
		if request.Accuracy != nil {
			outpost.Accuracy = clamp(*request.Accuracy, 0, 1)
		}
		world.markDirtyLocked()
		return nil, nil
	})
}

func allianceIndex(name string) int {
	if name == "red" || name == "R" {
		return Red
	}
	return Blue
}
