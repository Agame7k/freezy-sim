package world

import (
	"container/heap"
	"math"
)

// Path planning. Each robot plans over a costmap of the field built for its own size and height: field elements are
// inflated by the robot's half-width (blocked), with a soft band around them, the bumps cost extra (robots slow to
// half speed and can high-center) and the trench arms are open only to robots short enough to fit under. Other robots
// add a soft cost so paths flow around traffic. Plans are A* on a 0.15 m grid, shortened by line-of-sight checks, and
// followed with a pure-pursuit lookahead.

const (
	navCell       = 0.15
	navReplanSec  = 0.35
	navLookahead  = 0.9
	navBumpCost   = 2.6
	navNearCost   = 1.8 // soft band next to obstacles
	navRobotCost  = 3.0 // passing close to another robot
	navRobotRange = 1.0
)

type navGrid struct {
	w, h int
	cost []float32 // < 0 blocked; ≥ 1 traversal cost per metre
}

type navKey struct {
	low     bool
	radius  int  // centimetres
	noBumps bool // the belly would ground on a bump crest: the bumps are walls
}

func (grid *navGrid) index(cx, cy int) int { return cy*grid.w + cx }

func (grid *navGrid) cellOf(p Vec2) (int, int) {
	cx := int(p.X / navCell)
	cy := int(p.Y / navCell)
	return clampInt(cx, 0, grid.w-1), clampInt(cy, 0, grid.h-1)
}

func (grid *navGrid) center(cx, cy int) Vec2 {
	return Vec2{(float64(cx) + 0.5) * navCell, (float64(cy) + 0.5) * navCell}
}

func (grid *navGrid) blocked(cx, cy int) bool {
	if cx < 0 || cy < 0 || cx >= grid.w || cy >= grid.h {
		return true
	}
	return grid.cost[grid.index(cx, cy)] < 0
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// navGridFor returns (building once) the costmap for a robot's size and height.
func (world *World) navGridFor(robot *Robot) *navGrid {
	low := robot.Arch.Height <= world.layout.Trench.ClearHeight
	key := navKey{low: low, radius: int(math.Round(robot.half().X * 100)), noBumps: low && !world.crossesBumps(robot)}
	if world.navGrids == nil {
		world.navGrids = map[navKey]*navGrid{}
	}
	if grid, ok := world.navGrids[key]; ok {
		return grid
	}
	l := world.layout
	grid := &navGrid{w: int(math.Ceil(l.Field.Length / navCell)), h: int(math.Ceil(l.Field.Width / navCell))}
	grid.cost = make([]float32, grid.w*grid.h)
	radius := float64(key.radius) / 100
	soft := radius * 1.45
	for cy := 0; cy < grid.h; cy++ {
		for cx := 0; cx < grid.w; cx++ {
			p := grid.center(cx, cy)
			cost := float32(1)
			// Walls and guardrails.
			edge := math.Min(math.Min(p.X, l.Field.Length-p.X), math.Min(p.Y, l.Field.Width-p.Y))
			if edge < radius {
				cost = -1
			} else if edge < soft {
				cost = navNearCost
			}
			for _, static := range world.statics {
				if cost < 0 {
					break
				}
				if !static.Robots || (static.ClearHeight > 0 && robot.Arch.Height <= static.ClearHeight) {
					continue
				}
				d := boxDistance(static.Box, p)
				if d < radius {
					cost = -1
				} else if d < soft && cost < navNearCost {
					cost = navNearCost
				}
			}
			if cost > 0 {
				for _, bump := range world.bumps {
					if key.noBumps && boxDistance(bump, p) < radius {
						cost = -1
						break
					}
					if boxDistance(bump, p) < radius*0.6 {
						cost = float32(math.Max(float64(cost), navBumpCost))
					}
				}
			}
			grid.cost[grid.index(cx, cy)] = cost
		}
	}
	world.navGrids[key] = grid
	return grid
}

// boxDistance is the distance from a point to an axis-aligned box (0 inside).
func boxDistance(box AABB, p Vec2) float64 {
	dx := math.Max(math.Max(box.MinX-p.X, 0), p.X-box.MaxX)
	dy := math.Max(math.Max(box.MinY-p.Y, 0), p.Y-box.MaxY)
	return math.Hypot(dx, dy)
}

// nearestFree moves a point out of blocked cells (to the closest open cell within ~1.5 m).
func (grid *navGrid) nearestFree(p Vec2) (Vec2, bool) {
	cx, cy := grid.cellOf(p)
	if !grid.blocked(cx, cy) {
		return p, true
	}
	for r := 1; r <= 10; r++ {
		best, bestDistance := Vec2{}, math.Inf(1)
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if absInt(dx) != r && absInt(dy) != r {
					continue
				}
				if grid.blocked(cx+dx, cy+dy) {
					continue
				}
				c := grid.center(cx+dx, cy+dy)
				if d := c.Sub(p).Len(); d < bestDistance {
					best, bestDistance = c, d
				}
			}
		}
		if !math.IsInf(bestDistance, 1) {
			return best, true
		}
	}
	return p, false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---- A* ----------------------------------------------------------------------------------------------------------

type navNode struct {
	index int
	f     float32
}

type navHeap []navNode

func (h navHeap) Len() int            { return len(h) }
func (h navHeap) Less(i, j int) bool  { return h[i].f < h[j].f }
func (h navHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *navHeap) Push(x interface{}) { *h = append(*h, x.(navNode)) }
func (h *navHeap) Pop() interface{} {
	old := *h
	node := old[len(old)-1]
	*h = old[:len(old)-1]
	return node
}

var navSteps = [8][3]float64{{1, 0, 1}, {-1, 0, 1}, {0, 1, 1}, {0, -1, 1}, {1, 1, math.Sqrt2}, {1, -1, math.Sqrt2},
	{-1, 1, math.Sqrt2}, {-1, -1, math.Sqrt2}}

// planPath finds a path from start to goal for a robot (nil if there is none). Other robots and the agent's own
// recently-blocked cells add cost.
func (world *World) planPath(robot *Robot, agent *Agent, start, goal Vec2) []Vec2 {
	return world.planPathWith(robot, agent, start, goal, true)
}

// planStaticPath plans on the empty field (no traffic), the way a team draws an auto path before the match.
func (world *World) planStaticPath(robot *Robot, start, goal Vec2) []Vec2 {
	return world.planPathWith(robot, nil, start, goal, false)
}

func (world *World) planPathWith(robot *Robot, agent *Agent, start, goal Vec2, withTraffic bool) []Vec2 {
	grid := world.navGridFor(robot)
	goal, ok := grid.nearestFree(goal)
	if !ok {
		return nil
	}
	start, _ = grid.nearestFree(start)
	sx, sy := grid.cellOf(start)
	gx, gy := grid.cellOf(goal)
	startIndex, goalIndex := grid.index(sx, sy), grid.index(gx, gy)
	if startIndex == goalIndex {
		return []Vec2{goal}
	}
	// Soft costs for traffic, computed once per plan.
	type obstacle struct {
		p    Vec2
		cost float64
	}
	var traffic []obstacle
	for _, other := range world.robots {
		if other == robot || !other.Present || !withTraffic {
			continue
		}
		cost := navRobotCost
		if other.Alliance == robot.Alliance {
			cost *= 0.7
		}
		if other.ClimbLevel > 0 || other.Climbing || !other.Enabled {
			cost *= 2 // parked robots are obstacles
		}
		traffic = append(traffic, obstacle{other.Pos, cost})
	}
	extra := func(p Vec2) float64 {
		total := 0.0
		for _, o := range traffic {
			if d := p.Sub(o.p).Len(); d < navRobotRange {
				total += o.cost * (1 - d/navRobotRange)
			}
		}
		if agent != nil {
			cx, cy := grid.cellOf(p)
			if until, ok := agent.avoidCells[[2]int{cx, cy}]; ok && world.time < until {
				total += 25
			}
			total += agent.hazards[[2]int{cx, cy}] // where this team remembers getting stuck

		}
		return total
	}

	n := grid.w * grid.h
	scratch := &world.navScratch
	if len(scratch.g) != n {
		scratch.g = make([]float32, n)
		scratch.parent = make([]int32, n)
		scratch.seen = make([]uint32, n)
		scratch.closed = make([]uint32, n)
	}
	scratch.epoch++
	epoch := scratch.epoch
	heuristic := func(cx, cy int) float32 {
		dx, dy := math.Abs(float64(cx-gx)), math.Abs(float64(cy-gy))
		return float32((math.Max(dx, dy) + (math.Sqrt2-1)*math.Min(dx, dy)) * navCell)
	}
	open := &navHeap{}
	scratch.g[startIndex] = 0
	scratch.parent[startIndex] = -1
	scratch.seen[startIndex] = epoch
	heap.Push(open, navNode{startIndex, heuristic(sx, sy)})
	found := false
	for open.Len() > 0 {
		node := heap.Pop(open).(navNode)
		if scratch.closed[node.index] == epoch {
			continue
		}
		scratch.closed[node.index] = epoch
		if node.index == goalIndex {
			found = true
			break
		}
		cx, cy := node.index%grid.w, node.index/grid.w
		for _, step := range navSteps {
			nx, ny := cx+int(step[0]), cy+int(step[1])
			if grid.blocked(nx, ny) {
				continue
			}
			if step[2] > 1 && (grid.blocked(cx+int(step[0]), cy) || grid.blocked(cx, cy+int(step[1]))) {
				continue // no corner cutting
			}
			ni := grid.index(nx, ny)
			if scratch.closed[ni] == epoch {
				continue
			}
			cost := float64(grid.cost[ni])
			g := scratch.g[node.index] + float32(step[2]*navCell*cost+extra(grid.center(nx, ny))*navCell)
			if scratch.seen[ni] == epoch && g >= scratch.g[ni] {
				continue
			}
			scratch.g[ni] = g
			scratch.parent[ni] = int32(node.index)
			scratch.seen[ni] = epoch
			heap.Push(open, navNode{ni, g + heuristic(nx, ny)})
		}
	}
	if !found {
		return nil
	}
	var cells []int
	for i := goalIndex; i >= 0; i = int(scratch.parent[i]) {
		cells = append(cells, i)
		if i == startIndex {
			break
		}
	}
	// Reverse, then keep only the corners a straight line cannot skip.
	path := make([]Vec2, 0, len(cells))
	for i := len(cells) - 1; i >= 0; i-- {
		path = append(path, grid.center(cells[i]%grid.w, cells[i]/grid.w))
	}
	path[len(path)-1] = goal
	var avoid func(Vec2) bool
	if agent != nil && len(agent.hazards) > 0 {
		// Shortcuts must not cut back through spots the team remembers getting stuck in.
		avoid = func(p Vec2) bool {
			cx, cy := grid.cellOf(p)
			return agent.hazards[[2]int{cx, cy}] > 0
		}
	}
	return grid.simplify(start, path, avoid)
}

// simplify drops waypoints that are in direct, equally cheap line of sight (and, with avoid, clear of those points).
func (grid *navGrid) simplify(start Vec2, path []Vec2, avoid func(Vec2) bool) []Vec2 {
	var out []Vec2
	from := start
	i := 0
	for i < len(path) {
		next := i
		for j := len(path) - 1; j > i; j-- {
			if grid.clearLine(from, path[j]) && (avoid == nil || !lineTouches(from, path[j], avoid)) {
				next = j
				break
			}
		}
		out = append(out, path[next])
		from = path[next]
		i = next + 1
	}
	return out
}

// clearLine reports whether a straight drive stays on open, normally-priced ground (bumps are only crossed where the
// planner chose to).
func (grid *navGrid) clearLine(a, b Vec2) bool {
	distance := b.Sub(a).Len()
	steps := int(distance/(navCell*0.5)) + 1
	startCost := grid.costAt(a)
	for k := 1; k <= steps; k++ {
		p := a.Add(b.Sub(a).Scale(float64(k) / float64(steps)))
		cx, cy := grid.cellOf(p)
		if grid.blocked(cx, cy) {
			return false
		}
		if c := grid.cost[grid.index(cx, cy)]; c > startCost && c >= navBumpCost {
			return false
		}
	}
	return true
}

// lineTouches reports whether a straight line passes a point for which hit is true.
func lineTouches(a, b Vec2, hit func(Vec2) bool) bool {
	steps := int(b.Sub(a).Len()/(navCell*0.5)) + 1
	for k := 1; k < steps; k++ {
		if hit(a.Add(b.Sub(a).Scale(float64(k) / float64(steps)))) {
			return true
		}
	}
	return false
}

func (grid *navGrid) costAt(p Vec2) float32 {
	cx, cy := grid.cellOf(p)
	return grid.cost[grid.index(cx, cy)]
}

// navScratch is reusable A* memory (the world runs one plan at a time under its lock).
type navScratch struct {
	g      []float32
	parent []int32
	seen   []uint32 // epoch in which g/parent were set
	closed []uint32 // epoch in which the cell was expanded
	epoch  uint32
}

// ---- Following -----------------------------------------------------------------------------------------------------

// driveTo steers toward a point along a planned path. With useHeading the robot turns to the given heading as it
// arrives; otherwise it faces its direction of travel.
func (agent *Agent) driveTo(robot *Robot, world *World, goal Vec2, heading float64, useHeading bool) {
	grid := world.navGridFor(robot)
	direct := goal.Sub(robot.Pos).Len() < 0.9 || grid.clearLine(robot.Pos, goal)
	if direct && len(agent.hazards) > 0 && goal.Sub(robot.Pos).Len() >= 0.9 {
		direct = !lineTouches(robot.Pos, goal, func(p Vec2) bool {
			cx, cy := grid.cellOf(p)
			return agent.hazards[[2]int{cx, cy}] > 0
		})
	}
	waypoint := goal
	if !direct {
		replan := agent.path == nil || world.time-agent.pathAt > navReplanSec || agent.pathGoal.Sub(goal).Len() > 0.4
		if replan {
			agent.path = world.planPath(robot, agent, robot.Pos, goal)
			agent.pathAt, agent.pathGoal = world.time, goal
			agent.pathFailed = agent.path == nil
		}
		if agent.path != nil {
			waypoint = agent.lookahead(robot, grid)
		}
	} else {
		agent.path = nil
	}
	delta := waypoint.Sub(robot.Pos)
	distance := goal.Sub(robot.Pos).Len()
	maxSpeed := robot.Arch.MaxSpeed * 0.92 * math.Min(1.05, robot.Skill)
	// Brake smoothly into the goal (v² = 2·a·d), and slow for sharp corners in the path.
	speed := math.Min(maxSpeed, math.Sqrt(2*robot.Arch.MaxAccel*0.6*distance)+0.15)
	velocity := delta.Norm().Scale(speed)
	// Local avoidance: ease around robots that are about to be hit.
	for _, other := range world.robots {
		if other == robot || !other.Present || other == agent.contactTarget || other.Climbing || other.ClimbLevel > 0 {
			continue
		}
		away := robot.Pos.Sub(other.Pos)
		gap := away.Len()
		if gap < 1.15 && gap > 1e-3 && velocity.Dot(away.Scale(-1/gap)) > 0 {
			velocity = velocity.Add(away.Norm().Scale((1.15 - gap) * 1.4))
		}
	}
	// Inconsistent drivers wobble and overshoot now and then.
	if world.time < agent.wobbleUntil {
		velocity = velocity.Add(agent.wobble)
	}
	robot.Ctl.Vel = velocity
	switch {
	case useHeading && distance < 1.2:
		agent.faceAngle(robot, heading)
	case delta.Len() > 0.15:
		agent.faceAngle(robot, math.Atan2(delta.Y, delta.X))
	}
}

// lookahead returns the pure-pursuit target point on the current path.
func (agent *Agent) lookahead(robot *Robot, grid *navGrid) Vec2 {
	// Drop waypoints already reached.
	for len(agent.path) > 1 && robot.Pos.Sub(agent.path[0]).Len() < 0.35 {
		agent.path = agent.path[1:]
	}
	from := robot.Pos
	remaining := navLookahead
	for _, p := range agent.path {
		segment := p.Sub(from)
		if segment.Len() >= remaining {
			candidate := from.Add(segment.Norm().Scale(remaining))
			if grid.clearLine(robot.Pos, candidate) {
				return candidate
			}
			return agent.path[0]
		}
		remaining -= segment.Len()
		from = p
	}
	return agent.path[len(agent.path)-1]
}
