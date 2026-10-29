package world

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Team-built robots. Every team builds its robot from the same kit of parts real 2026 teams pick from: a drive base,
// a frame size, a height, an intake, a hopper, a shooter and a climber. The parts decide the robot's numbers (speed,
// capacity, shot rate, accuracy, weight, climb). A team picks the parts it thinks win at the event: what it can
// afford and build, the job it wants, and what the other robots at the event (its likely alliance partners) already
// do. Between matches a team may rework one part in the pits when the robot keeps underperforming, or when the event
// turns out to need something else; the user can lock a team's design to stop that.

// Build is the parts a team chose.
type Build struct {
	Drive   string  `json:"drive"`   // "swerve", "tank" (West Coast) or "kop" (kit chassis)
	Frame   float64 `json:"frame"`   // frame side (m)
	Height  float64 `json:"height"`  // m; at most 0.762 (R107); 0.565 or lower fits under the trench arm
	Intake  float64 `json:"intake"`  // intake width as a share of the frame
	Shooter string  `json:"shooter"` // "fixed" (side-plate flywheels), "wide" (4-lane volley), "turret", "small", "kitbot"
	Lanes   int     `json:"lanes"`
	Hopper  string  `json:"hopper"`  // "small", "medium", "large", "huge"
	Climber int     `json:"climber"` // highest tower level it is built for (0 = no climber)
	Ballast float64 `json:"ballast"` // kg of weight added to push harder
	Tune    int     `json:"tune"`    // shooter tuning sessions in the pits (each tightens the shots)
}

// ModelSpec is what the 3D view draws (the same fields as ARCHETYPES in web/js/scene/robots.js).
type ModelSpec struct {
	Label        string  `json:"label"`
	Height       float64 `json:"height"`
	Frame        float64 `json:"frame"`
	Drive        string  `json:"drive"`
	Intake       float64 `json:"intake"`
	Shooter      string  `json:"shooter"`
	Lanes        int     `json:"lanes"`
	Hopper       float64 `json:"hopper"`
	HopperLength float64 `json:"hopperLength,omitempty"` // share of the room between intake and back (0 = all)
	Bin          bool    `json:"bin,omitempty"`
	Capacity     int     `json:"capacity"`
	Climber      bool    `json:"climber,omitempty"`
}

const (
	trenchHeight  = 0.565 // tallest robot that fits under the trench arm
	maxRobotMass  = 65.5  // kg with bumpers and battery (R103: 115 lb without them)
	bumperBattery = 13.0  // kg of bumpers and battery
	buildPrefix   = "build:"
)

var (
	// Every team runs swerve: COTS modules made it the norm. (The other drives stay in the tables for old saves.)
	buildDrives   = []string{"swerve"}
	buildFrames   = []float64{0.68, 0.74, 0.8, 0.84}
	buildHeights  = []float64{0.36, 0.45, 0.54, 0.75}
	buildIntakes  = []float64{0.8, 0.95, 1.12}
	buildHoppers  = []string{"small", "medium", "large", "huge"}
	buildShooters = []struct {
		kind  string
		lanes int
	}{{"fixed", 1}, {"fixed", 2}, {"fixed", 3}, {"wide", 4}, {"turret", 2}, {"small", 1}, {"kitbot", 1}}
	// A hopper size is how much of the frame it takes: its width (share of the frame) and length (share of the room
	// between the intake and the back). Its capacity is what that volume holds (hopper.go).
	hopperShare  = map[string]float64{"small": 0.5, "medium": 0.72, "large": 0.82, "huge": 0.86}
	hopperLength = map[string]float64{"small": 0.55, "medium": 0.85, "large": 1, "huge": 1}
	hopperMass   = map[string]float64{"small": 1.5, "medium": 3.5, "large": 4.5, "huge": 6}
	hopperCost   = map[string]float64{"small": 0, "medium": 0.5, "large": 1, "huge": 2}
)

// Name is the build's design name: it lists every part, so a saved name rebuilds the same robot.
func (b Build) Name() string {
	return fmt.Sprintf("%s%s-f%d-h%d-i%d-%s%d-%s-c%d-b%d-t%d", buildPrefix, b.Drive, cm(b.Frame), cm(b.Height),
		cm(b.Intake), b.Shooter, b.Lanes, b.Hopper, b.Climber, int(math.Round(b.Ballast)), b.Tune)
}

func cm(v float64) int { return int(math.Round(v * 100)) }

// parseBuild reads a design name written by Name.
func parseBuild(name string) (Build, bool) {
	parts := strings.Split(strings.TrimPrefix(name, buildPrefix), "-")
	if !strings.HasPrefix(name, buildPrefix) || len(parts) != 9 {
		return Build{}, false
	}
	var b Build
	var frame, height, intake, ballast int
	b.Drive, b.Hopper = parts[0], parts[5]
	b.Drive = "swerve" // designs saved before every team ran swerve
	shooter := strings.TrimRight(parts[4], "0123456789")
	b.Shooter = shooter
	_, err1 := fmt.Sscanf(parts[1], "f%d", &frame)
	_, err2 := fmt.Sscanf(parts[2], "h%d", &height)
	_, err3 := fmt.Sscanf(parts[3], "i%d", &intake)
	_, err4 := fmt.Sscanf(parts[4][len(shooter):], "%d", &b.Lanes)
	_, err5 := fmt.Sscanf(parts[6], "c%d", &b.Climber)
	_, err6 := fmt.Sscanf(parts[7], "b%d", &ballast)
	_, err7 := fmt.Sscanf(parts[8], "t%d", &b.Tune)
	for _, err := range []error{err1, err2, err3, err4, err5, err6, err7} {
		if err != nil {
			return Build{}, false
		}
	}
	b.Frame, b.Height, b.Intake, b.Ballast = float64(frame)/100, float64(height)/100, float64(intake)/100, float64(ballast)
	return b, true
}

// mass is the robot's weight in kg with bumpers and battery.
func (b Build) mass() float64 {
	m := map[string]float64{"swerve": 25, "tank": 27, "kop": 22}[b.Drive]
	m += (b.Frame-0.7)*30 + (b.Height-0.5)*10 + 2 + 3*b.Intake + hopperMass[b.Hopper]
	switch b.Shooter {
	case "fixed":
		m += 3 + 1.5*float64(b.Lanes)
	case "wide":
		m += 8
	case "turret":
		m += 9
	case "small":
		m += 2.5
	case "kitbot":
		m += 3
	}
	m += []float64{0, 2.5, 4, 5.5}[clampInt(b.Climber, 0, 3)]
	return m + b.Ballast + bumperBattery
}

// cost is what the parts take in money, machining and build season hours (a team's budget limits it).
func (b Build) cost() float64 {
	c := map[string]float64{"swerve": 0, "tank": 0, "kop": 0}[b.Drive] // every team has swerve modules now
	switch b.Shooter {
	case "fixed":
		c += 1 + 0.5*float64(b.Lanes)
	case "wide":
		c += 3
	case "turret":
		c += 4
	case "small":
		c += 0.5
	}
	c += hopperCost[b.Hopper] + []float64{0, 0.5, 1.5, 3}[clampInt(b.Climber, 0, 3)]
	if b.Height > 0.68 {
		c += 0.5
	}
	if b.Intake > 1 {
		c += 0.5
	}
	return c + 0.4*float64(b.Tune)
}

// legal reports whether the build passes inspection and the parts fit together, with the reason when not.
func (b Build) legal() (bool, string) {
	switch {
	case b.Height > MaxRobotHeight:
		return false, "taller than 30 in (R107)"
	case b.mass() > maxRobotMass:
		return false, "over the 115 lb weight limit (R103)"
	case b.Shooter == "turret" && b.Height < 0.5:
		return false, "a turret needs a deck at least 20 in up"
	case b.Shooter == "wide" && b.Frame < 0.76:
		return false, "a 4-lane shooter needs a wider frame"
	case b.Hopper == "huge" && b.Frame < 0.8:
		return false, "a huge bin needs a big frame"
	case b.Shooter == "fixed" && b.Lanes > 3:
		return false, "side-plate shooters top out at 3 lanes"
	case b.Climber >= 3 && b.Height < 0.5:
		return false, "too short to reach the L3 rung"
	case b.Drive == "kop" && (b.Shooter == "turret" || b.Shooter == "wide"):
		return false, "the kit chassis cannot carry that shooter"
	case b.Drive == "kop" && b.Frame > 0.74:
		return false, "the kit chassis only comes in one size"
	case b.Drive == "kop" && (b.Hopper == "huge" || b.Climber > 2):
		return false, "the kit chassis is too light for that"
	}
	return true, ""
}

// Archetype works out a build's numbers.
func (b Build) Archetype() Archetype {
	mass := b.mass()
	arch := Archetype{Name: b.Name(), Label: b.label(), Frame: b.Frame, Height: b.Height,
		Volley: 1, ClimbLevel: b.Climber, IntakeWidth: b.Intake * b.Frame * 0.98, Mass: math.Round(mass*10) / 10,
		Traction: 1, Tank: b.Drive != "swerve", Turret: b.Shooter == "turret"}
	speed := map[string]float64{"swerve": 4.6, "tank": 4.5, "kop": 3.3}[b.Drive]
	arch.MaxSpeed = round2(speed * math.Pow(60/mass, 0.35))
	arch.MaxAccel = round2(map[string]float64{"swerve": 7.5, "tank": 7, "kop": 5}[b.Drive] * 62 / mass)
	arch.MaxOmega = map[string]float64{"swerve": 7, "tank": 6, "kop": 5}[b.Drive]
	if b.Drive == "tank" {
		arch.Traction = 1.2
	}
	tall := b.Height > 0.68
	switch b.Shooter {
	case "fixed":
		arch.ShootRate, arch.PassRate = 3+2*float64(b.Lanes), 4+2*float64(b.Lanes)
		arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 0.022, 0.011, 5.5
		if tall {
			arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 0.018, 0.009, 6.5
		}
	case "wide":
		arch.ShootRate, arch.PassRate, arch.Volley, arch.LaneSpread = 12, 10, 4, 0.54
		arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 0.022, 0.012, 4.6
		if b.Hopper == "huge" {
			arch.ShootRate, arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 18, 0.026, 0.014, 3.6
		}
	case "turret":
		arch.ShootRate, arch.PassRate, arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 9, 10, 0.018, 0.01, 6
	case "small":
		arch.ShootRate, arch.PassRate, arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 3, 4, 0.05, 0.03, 3.5
	default: // kitbot roller
		arch.ShootRate, arch.PassRate, arch.ShotSpeedSD, arch.ShotAimSD, arch.MaxRange = 3, 3, 0.05, 0.03, 3.5
	}
	arch.PassRate = math.Max(3, arch.PassRate+(b.Intake-0.9)*20)
	tune := math.Pow(0.85, float64(clampInt(b.Tune, 0, 3)))
	arch.ShotSpeedSD, arch.ShotAimSD = round4(arch.ShotSpeedSD*tune), round4(arch.ShotAimSD*tune)
	arch.ClimbTime = 3.4 - 0.3*float64(b.Climber)
	if tall {
		arch.ClimbTime -= 0.2
	}
	arch.Class = b.class()
	arch.Id = archetypeByName(arch.Class).Id
	arch.Spec = b
	arch.Model = ModelSpec{Label: arch.Label, Height: b.Height, Frame: b.Frame, Drive: b.Drive, Intake: b.Intake,
		Shooter: b.Shooter, Lanes: b.Lanes, Hopper: hopperShare[b.Hopper], HopperLength: hopperLength[b.Hopper],
		Bin: b.Hopper == "huge", Climber: b.Climber > 0}
	arch.Capacity = arch.hopperShape().capacity()
	arch.Model.Capacity = arch.Capacity
	return arch
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// class is the stock design a build is closest to (plans are learned per mix of classes, the 3D view falls back to it).
func (b Build) class() string {
	switch {
	case b.Shooter == "kitbot":
		return "kitbot"
	case b.Shooter == "small":
		return "defender"
	case b.Shooter == "wide" && b.Hopper == "huge":
		return "bigdumper"
	case b.Shooter == "wide":
		return "volley"
	case b.Shooter == "turret":
		return "turret"
	case b.Height > 0.68:
		return "tall"
	case b.Intake > 1.05 || (b.Lanes >= 3 && b.Climber <= 1):
		return "shuttle"
	}
	return "scorer"
}

func (b Build) label() string {
	drive := map[string]string{"swerve": "Swerve", "tank": "West Coast tank", "kop": "Kit chassis"}[b.Drive]
	height := "low (trench)"
	switch {
	case b.Height > trenchHeight:
		height = "tall"
	case b.Height < 0.42:
		height = "very low"
	}
	shooter := map[string]string{"fixed": fmt.Sprintf("%d-lane shooter", b.Lanes), "wide": "4-lane volley shooter",
		"turret": "turret shooter", "small": "small shooter", "kitbot": "KitBot roller"}[b.Shooter]
	parts := []string{drive, height, shooter, b.Hopper + " hopper"}
	if b.Climber > 0 {
		parts = append(parts, fmt.Sprintf("L%d climber", b.Climber))
	} else {
		parts = append(parts, "no climber")
	}
	if b.Ballast > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kg ballast", b.Ballast))
	}
	if b.Tune > 0 {
		parts = append(parts, fmt.Sprintf("shooter tuned ×%d", b.Tune))
	}
	return strings.Join(parts, " · ")
}

// ---- Teams --------------------------------------------------------------------------------------------------------

// TeamTraits are what a team brings to build season.
type TeamTraits struct {
	Budget float64 `json:"budget"` // how many parts' worth it can buy and build
	Role   string  `json:"role"`   // the job it would like: "score", "pass", "defend" or "climb"
}

func teamTraits(team int) TeamTraits {
	resources := hashUnit(team, 0xb0d6e7)*0.6 + (skillForTeam(team)-0.7)/0.6*0.4 // better drivers, richer teams
	roles := []string{"score", "score", "score", "pass", "climb", "defend"}
	return TeamTraits{Budget: round2(0.7 + 11*math.Pow(resources, 1.2)), Role: roles[int(hashUnit(team, 0x201e)*6)%6]}
}

// needs are how much a team values each job (scoring, passing, defense, climbing) for its alliances.
type needs struct {
	score, pass, defend, climb float64
	short                      string // the job the event is most short of (for the reason shown), if any
}

// eventNeeds weighs the jobs for a team: the job it wants, then what the robots it will be paired with already do.
// When most of the event builds scorers, alliances are short on passers and defenders, so those jobs are worth more.
func eventNeeds(traits TeamTraits, others []Archetype) needs {
	n := needs{score: 1, pass: 0.5, defend: 0.45, climb: 1}
	switch traits.Role {
	case "score":
		n.score += 0.4
	case "pass":
		n.pass += 0.6
	case "defend":
		n.defend += 0.7
	case "climb":
		n.climb += 0.6
	}
	if len(others) >= 4 {
		var scorers, passers, defenders, climbers float64
		for _, arch := range others {
			switch {
			case arch.Class == "shuttle" || arch.PassRate >= 11:
				passers++
			case arch.Class == "defender" || arch.Mass*arch.Traction >= 72:
				defenders++
			case arch.Class == "kitbot":
			default:
				scorers++
			}
			if arch.ClimbLevel >= 2 {
				climbers++
			}
		}
		total := float64(len(others))
		// Each alliance wants about two scorers, a passer or defender, and two good climbers.
		boosts := map[string]float64{
			"passers":  clamp(1.0/3-passers/total, -0.3, 0.3) * 2.5,
			"defense":  clamp(0.15-defenders/total, -0.15, 0.15) * 3,
			"climbers": clamp(0.4-climbers/total, -0.4, 0.4),
		}
		best := 0.1 // only a real shortage is worth mentioning
		for _, job := range []string{"passers", "defense", "climbers"} {
			if boosts[job] > best {
				n.short, best = job, boosts[job]
			}
		}
		n.pass *= 1 + boosts["passers"]
		n.defend *= 1 + boosts["defense"]
		n.score *= 1 + clamp(2.0/3-scorers/total, -0.3, 0.3)
		n.climb *= 1 + boosts["climbers"]
	}
	return n
}

// designValue is a team's estimate of the points per match a robot is worth to its alliance, given its needs.
func designValue(arch Archetype, n needs) (float64, map[string]float64) {
	speed := arch.MaxSpeed
	collect := float64(arch.Capacity) / (arch.IntakeWidth * speed * 2.2) // FUEL is thick on the floor
	travel := 2 * 6 / speed
	if arch.Height <= trenchHeight {
		travel *= 0.8 // under the trench instead of over the bump
	}
	// A big frame carries more but gets blocked, turns wide and squeezes past fewer robots in traffic.
	travel *= 1 + 2.5*(arch.Frame-0.68)
	if !arch.Tank {
		travel *= 0.85 // swerve strafes around traffic and lines up its shots without turning
		collect *= 0.9
	}
	collect *= 1 + 1.5*(arch.Frame-0.68)
	shoot := float64(arch.Capacity) / arch.ShootRate
	if arch.Turret {
		shoot *= 0.5 // shoots on the way back
	}
	hit := 1 / (1 + 12*arch.ShotSpeedSD + 25*arch.ShotAimSD)
	hit *= 0.85 + 0.15*math.Min(1, arch.MaxRange/6)
	score := float64(arch.Capacity) * hit / (collect + travel + shoot) * 32
	pass := float64(arch.Capacity) / (collect + float64(arch.Capacity)/arch.PassRate + 3) * 12
	// Defense is weight and grip (and getting under the other robots' bumpers helps a little).
	push := arch.Mass * arch.Traction / 62
	defend := 25 * push * push * push
	if arch.Height < 0.45 {
		defend *= 1.1
	}
	// Climbing: the endgame points, an L1 climb in AUTO, and its share of the tower ranking point.
	climb := float64(TowerPoints(arch.ClimbLevel, false)) * (1 - 0.06*arch.ClimbTime)
	if arch.ClimbLevel >= 1 {
		climb += 0.5*float64(TowerPoints(1, true)) + 8
	}
	parts := map[string]float64{"score": score * n.score, "pass": pass * n.pass, "defend": defend * n.defend,
		"climb": climb * n.climb}
	// A robot does one job at a time in teleop: its best job counts in full, the others as a fallback. Climbing
	// comes on top at the end.
	main := math.Max(parts["score"], math.Max(parts["pass"], parts["defend"]))
	rest := parts["score"] + parts["pass"] + parts["defend"] - main
	return main + 0.25*rest + parts["climb"], parts
}

// allBuilds lists every combination of parts in the kit that passes inspection.
func allBuilds() []Build {
	var builds []Build
	for _, drive := range buildDrives {
		for _, frame := range buildFrames {
			for _, height := range buildHeights {
				for _, intake := range buildIntakes {
					for _, shooter := range buildShooters {
						for _, hopper := range buildHoppers {
							for climber := 0; climber <= 3; climber++ {
								b := Build{Drive: drive, Frame: frame, Height: height, Intake: intake,
									Shooter: shooter.kind, Lanes: shooter.lanes, Hopper: hopper, Climber: climber}
								if ok, _ := b.legal(); ok {
									builds = append(builds, b)
								}
							}
						}
					}
				}
			}
		}
	}
	return builds
}

var kitBuilds = allBuilds()

// ModelSpecs lists every robot the 3D view may have to draw: the model of each build in the kit, and the same builds
// on the West Coast and kit drives that older saves used (where those pass inspection). The web package's robot
// model test draws them all.
func ModelSpecs() []ModelSpec {
	var specs []ModelSpec
	seen := map[ModelSpec]bool{}
	for _, b := range kitBuilds {
		for _, drive := range []string{b.Drive, "tank", "kop"} {
			b.Drive = drive
			if ok, _ := b.legal(); !ok {
				continue
			}
			spec := b.Archetype().Model
			if !seen[spec] {
				seen[spec] = true
				specs = append(specs, spec)
			}
		}
	}
	return specs
}

// designRobot is a team's build season: of the builds it can afford, the one it thinks is worth the most, with
// the team's own taste mixed in (teams do not all agree on what wins).
func designRobot(team int, others []Archetype) (Build, string) {
	traits := teamTraits(team)
	n := eventNeeds(traits, others)
	rng := rand.New(rand.NewSource(int64(team)*7919 + 17))
	taste := map[string]float64{}
	for _, part := range []string{"swerve", "tank", "kop", "fixed", "wide", "turret", "small", "kitbot", "tall"} {
		taste[part] = 1 + (rng.Float64()-0.5)*0.5
	}
	best, bestValue := Build{Drive: "swerve", Frame: 0.68, Height: 0.54, Intake: 0.86, Shooter: "kitbot", Lanes: 1,
		Hopper: "small", Climber: 1}, -1.0
	var bestParts map[string]float64
	for _, b := range kitBuilds {
		if b.cost() > traits.Budget {
			continue
		}
		value, parts := designValue(b.Archetype(), n)
		value *= taste[b.Drive] * taste[b.Shooter]
		if b.Height > 0.68 {
			value *= taste["tall"]
		}
		// Complex parts are a risk for a team that has never built one: it may not work by its first event.
		risk := map[string]float64{"turret": 0.4, "wide": 0.15}[b.Shooter] + map[string]float64{"swerve": 0.1}[b.Drive]
		risk += 0.05 * float64(max(0, b.Climber-1))
		value *= 1 - risk*clamp(1-traits.Budget/12, 0, 1)
		if value > bestValue {
			best, bestValue, bestParts = b, value, parts
		}
	}
	if n.defend > 0.5 && best.mass() < maxRobotMass-4 && best.Shooter == "small" {
		best.Ballast = math.Floor(maxRobotMass - 1 - best.mass()) // a pusher runs at the weight limit
	}
	if bestParts == nil {
		_, bestParts = designValue(best.Archetype(), n) // nothing fit the budget: the kit robot
	}
	return best, designReason(traits, n, bestParts)
}

func designReason(traits TeamTraits, n needs, parts map[string]float64) string {
	type job struct {
		name  string
		value float64
	}
	var jobs []job
	for name, value := range parts {
		jobs = append(jobs, job{name, value})
	}
	sort.Slice(jobs, func(i, j int) bool { // (ties by name: map order is random)
		if jobs[i].value != jobs[j].value {
			return jobs[i].value > jobs[j].value
		}
		return jobs[i].name < jobs[j].name
	})
	words := map[string]string{"score": "scoring", "pass": "passing", "defend": "defense", "climb": "climbing"}
	reason := fmt.Sprintf("budget %.1f, wants %s; built for %s", traits.Budget, words[traits.Role], words[jobs[0].name])
	builtFor := map[string]bool{jobs[0].name: true}
	if len(jobs) > 1 && jobs[1].value > 0.4*jobs[0].value {
		reason += " and " + words[jobs[1].name]
		builtFor[jobs[1].name] = true
	}
	// Mention the event's shortage only when the robot was built to fill it.
	if job := map[string]string{"passers": "pass", "defense": "defend", "climbers": "climb"}[n.short]; job != "" && builtFor[job] {
		reason += " (event short on " + n.short + ")"
	}
	return reason
}

// ---- Between matches ----------------------------------------------------------------------------------------------

// DesignRecord is a team's robot and how it has been doing since the last pit change.
type DesignRecord struct {
	Build      Build          `json:"build"`
	Why        string         `json:"why"`
	Locked     bool           `json:"locked"` // the design was set by hand: the team leaves it alone
	Since      int            `json:"since"`  // matches played since the last change
	Shots      int            `json:"shots"`
	Scored     int            `json:"scored"`
	ClimbTried int            `json:"climbTried"`
	ClimbMade  int            `json:"climbMade"`
	Changes    []DesignChange `json:"changes,omitempty"`
}

// DesignChange is one pit change.
type DesignChange struct {
	Match int    `json:"match"` // the team's match count when it was made
	Part  string `json:"part"`
	From  string `json:"from"`
	To    string `json:"to"`
	Why   string `json:"why"`
}

// eventDesignsLocked lists the designs of the other teams the world knows (the event's robots).
func (world *World) eventDesignsLocked(skip int) []Archetype {
	teams := make([]int, 0, len(world.teamArch))
	for team := range world.teamArch {
		if team != skip {
			teams = append(teams, team)
		}
	}
	sort.Ints(teams)
	others := make([]Archetype, 0, len(teams))
	for _, team := range teams {
		others = append(others, world.teamArch[team])
	}
	return others
}

// designForLocked gives a team its robot: the one it already has, or a new build season.
func (world *World) designForLocked(team int) Archetype {
	if arch, ok := world.teamArch[team]; ok {
		return arch
	}
	if !world.teamDesigns || team <= 0 {
		return archetypeForTeam(team)
	}
	build, why := designRobot(team, world.eventDesignsLocked(team))
	arch := build.Archetype()
	world.teamArch[team] = arch
	if world.learning.Teams == nil {
		world.learning = newLearning()
	}
	stats := world.teamStats(team)
	if stats.Design == nil || stats.Design.Build.Name() != build.Name() {
		stats.Design = &DesignRecord{Build: build, Why: why}
	}
	world.logf("info", "world", "Team %d built: %s (%s)", team, arch.Label, why)
	world.markDirtyLocked()
	return arch
}

// SetTeamDesigns turns team-built robots on or off (off: every team gets a stock design from its team number).
func (world *World) SetTeamDesigns(on bool) {
	world.mu.Lock()
	world.teamDesigns = on
	world.mu.Unlock()
}

// reviewDesignLocked is a team's pit crew after a match: note how the robot did and maybe rework one part.
func (world *World) reviewDesignLocked(r BenchRobot, stats *TeamStats) {
	record := stats.Design
	if !world.teamDesigns || record == nil || record.Locked {
		return
	}
	record.Build.Drive = "swerve" // a design saved before every team ran swerve
	arch, ok := world.teamArch[r.Team]
	if !ok || arch.Name != record.Build.Name() {
		return // the user picked a stock design
	}
	record.Since++
	record.Shots += r.Shots
	record.Scored += r.Scored
	if r.ClimbAttempted {
		record.ClimbTried++
		if r.ClimbLevel > 0 && r.ClimbLevel >= min(r.ClimbGoal, record.Build.Climber) {
			record.ClimbMade++
		}
	}
	if record.Since < 2 {
		return // too soon to judge
	}
	rng := rand.New(rand.NewSource(int64(r.Team)*104729 + int64(stats.Matches)))
	b := record.Build
	change := func(part, from, to, why string, next Build) {
		if ok, _ := next.legal(); !ok {
			return
		}
		record.Changes = append(record.Changes, DesignChange{Match: stats.Matches, Part: part, From: from, To: to, Why: why})
		if len(record.Changes) > 8 {
			record.Changes = record.Changes[len(record.Changes)-8:]
		}
		record.Build, record.Since, record.Shots, record.Scored, record.ClimbTried, record.ClimbMade = next, 0, 0, 0, 0, 0
		world.teamArch[r.Team] = next.Archetype()
		world.markDirtyLocked()
		world.logf("info", "world", "Team %d pit change: %s %s → %s (%s)", r.Team, part, from, to, why)
	}
	accuracy := float64(record.Scored) / math.Max(1, float64(record.Shots))
	switch {
	case record.Shots >= 15 && accuracy < 0.55 && b.Tune < 3:
		next := b
		next.Tune++
		change("shooter tune", fmt.Sprint(b.Tune), fmt.Sprint(next.Tune),
			fmt.Sprintf("only %.0f%% of shots scored", accuracy*100), next)
		return
	case record.ClimbTried >= 2 && record.ClimbMade*2 < record.ClimbTried && b.Climber > 1:
		next := b
		next.Climber--
		change("climber", fmt.Sprintf("L%d", b.Climber), fmt.Sprintf("L%d", next.Climber),
			fmt.Sprintf("made %d of %d climbs", record.ClimbMade, record.ClimbTried), next)
		return
	}
	// Otherwise the team looks at one-part swaps it can make in the pits (intake, hopper, climber, shooter lanes,
	// ballast) for what the event now needs, and makes one only if it looks clearly better. Robots that are not
	// scoring like they should lean away from scoring.
	traits := teamTraits(r.Team)
	n := eventNeeds(traits, world.eventDesignsLocked(r.Team))
	if record.Shots >= 10 && accuracy < 0.4 {
		n.score *= 0.7
		n.pass *= 1.3
	}
	current, _ := designValue(b.Archetype(), n)
	type swap struct {
		part, from, to string
		build          Build
		value          float64
	}
	var swaps []swap
	consider := func(part, from, to string, next Build) {
		if next == b || next.cost() > traits.Budget+0.5 {
			return
		}
		if ok, _ := next.legal(); !ok {
			return
		}
		value, _ := designValue(next.Archetype(), n)
		swaps = append(swaps, swap{part, from, to, next, value})
	}
	for _, intake := range buildIntakes {
		next := b
		next.Intake = intake
		consider("intake", fmt.Sprintf("%.0f%%", b.Intake*100), fmt.Sprintf("%.0f%%", intake*100), next)
	}
	for _, hopper := range buildHoppers {
		next := b
		next.Hopper = hopper
		consider("hopper", b.Hopper, hopper, next)
	}
	for climber := 0; climber <= 3; climber++ {
		next := b
		next.Climber = climber
		consider("climber", fmt.Sprintf("L%d", b.Climber), fmt.Sprintf("L%d", climber), next)
	}
	if b.Shooter == "fixed" {
		for lanes := 1; lanes <= 3; lanes++ {
			next := b
			next.Lanes = lanes
			consider("shooter lanes", fmt.Sprint(b.Lanes), fmt.Sprint(lanes), next)
		}
	}
	for _, ballast := range []float64{0, 5, 10} {
		next := b
		next.Ballast = ballast
		consider("ballast", fmt.Sprintf("%.0f kg", b.Ballast), fmt.Sprintf("%.0f kg", ballast), next)
	}
	sort.SliceStable(swaps, func(i, j int) bool { return swaps[i].value > swaps[j].value }) // (stable: same seed, same choice)
	if len(swaps) == 0 || swaps[0].value < current*1.12 || rng.Float64() < 0.4 {
		return // not worth the risk of breaking a working robot (or the pit crew ran out of time)
	}
	best := swaps[0]
	change(best.part, best.from, best.to, fmt.Sprintf("expects %.0f%% more from it", (best.value/current-1)*100), best.build)
}

// applyPitChangesLocked puts reworked robots on the field before a match (only robots that are disabled and whose
// hopper still fits).
func (world *World) applyPitChangesLocked() {
	for _, robot := range world.robots {
		arch, ok := world.teamArch[robot.Team]
		if robot.Team <= 0 || !ok || arch.Name == robot.Arch.Name || robot.Enabled || robot.Hopper > arch.Capacity {
			continue
		}
		robot.Arch = arch
	}
}

// SetTeamDesign sets a team's robot by hand (a stock design name or a build name) and locks it, or with name ""
// unlocks it so the team designs its own again.
func (world *World) setTeamDesignLocked(team int, name string, lock bool) {
	stats := world.teamStats(team)
	if name == "" {
		delete(world.teamArch, team)
		if stats.Design != nil {
			stats.Design.Locked = false
		}
		world.designForLocked(team)
		if stats.Design != nil {
			stats.Design.Locked = false
		}
		return
	}
	arch := archetypeByName(name)
	world.teamArch[team] = arch
	if stats.Design == nil {
		stats.Design = &DesignRecord{}
	}
	stats.Design.Locked = lock
	if arch.Spec.Drive != "" {
		stats.Design.Build = arch.Spec
	}
	stats.Design.Why = "set by hand"
}
