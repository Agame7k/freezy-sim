# Freezy Sim: Full Implementation Plan (v2)

> **Historical document.** This is the design plan the simulator was built from, kept for its research notes (the
> 2026 field, what an arena expects from field hardware) and the reasoning behind the architecture. Some details
> changed during the build: for example, the official field CAD is now imported by each user rather than shipped, and
> M10 (the WPILib bridge) has not been built. For how things work today, see [ARCHITECTURE.md](../ARCHITECTURE.md) and
> the rest of [docs/](../README.md).

**Goal:** `freezy-sim.exe`, a native Windows app with a full 3D, near-mock **2026 FRC REBUILT**
field and venue, that is a complete stand-in for real field hardware. You run Freezy Arena against
it and see everything a real event would show: robots driving and scoring 504 FUEL, hub lights
driven by real DMX packets, stack lights and E-stops wired through a simulated PLC, team signs and
timers, every arena display on virtual screens in the venue, and the full set of FTA / scorekeeper /
network tools. It also runs **headless** as an automated acceptance-test rig for Freezy Arena.

---

## 0. Ground rules

1. **All code lives in `freezy-sim/`.** Freezy Arena is read-only and serves only as a reference.
   Nothing is ever written into the Freezy Arena folder.
2. The sim talks to the arena **only over the wire** (DS TCP/UDP, Modbus, sACN, HTTP, websockets,
   telnet, SSH), exactly as real hardware does. No arena code is imported, patched, or linked.
3. The arena writes `event.db` and `static/logs/*.csv` into its working directory, so the sim never runs it from its own folder.
   The **launcher** builds it with `go build -o` into `freezy-sim/run/arena/`, copies `static/`, `templates/`,
   `schedules/` and `font/` beside it, and runs it there with `-dev`. All settings and automation touch only that
   sandboxed copy.

---

## 1. Research summary: the 2026 REBUILT field

Sources: 2026 Game Manual §5 (Arena) and §6 (Game Details), and WPILib `2026-rebuilt-welded.json`
AprilTag layout. All numbers are nominal. The official Field Dimension Drawings govern, and the sim's
layout file is editable so anything can be corrected.

**Coordinates:** WPILib field frame. Origin is the blue-alliance-wall corner, +X runs toward red, +Y across, +Z up.
The field is **16.541 m × 8.069 m** (651.2 in × 317.7 in).

| Element | Spec used for the model | Position (from AprilTags / manual) |
|---|---|---|
| Carpet & tape | Low-pile carpet, 2 in gaffer tape lines | Full field |
| Guardrails | 20 in (0.508 m) polycarbonate, extrusion top & bottom, 4 gates (38 in opening) | Long sides |
| Zones | Alliance zone 158.6 in (4.03 m) deep; neutral zone 283 in (7.19 m); white center line; alliance-colored robot starting line at the alliance-zone edge | — |
| **HUB** ×2 | 47 × 47 in (1.194 m) prism, hexagonal top opening 41.7 in (1.06 m), front edge at 72 in (1.83 m), net on back, **4 FUEL exits at base facing neutral zone**, DMX light bars on top angles, 2 AprilTags per face at 44.25 in | Blue center (4.625, 4.035), red center (11.915, 4.035) |
| **BUMP** ×4 | 73.0 × 44.4 × 6.513 in, 15° alliance-colored HDPE ramps | Either side of each hub |
| **TRENCH** ×4 | 65.65 × 47.0 × 40.25 in; clearance under arm **50.34 in wide × 22.25 in tall**; AprilTags at 35 in | Guardrail to bump, in line with hubs (x≈4.6 / 11.9) |
| **TOWER** ×2 | 49.25 × 45 × 78.25 in; uprights 72.1 in, 32.25 in apart; rungs (1.66 in OD) **LOW 27 in, MID 45 in, HIGH 63 in** | In the alliance wall between DS2 and DS3 (blue y≈3.75–4.18, red y≈3.89–4.32) |
| **OUTPOST** ×2 | CHUTE (15° ramp, holds ~25, door rotates ~90°), upper opening 31.8 × 7 in at 28.1 in; floor opening 32 × 7 in; CORRAL 35.8 × 37.6 in with 8.13 in walls | Blue at the low-Y end of the blue wall; red at the high-Y end of the red wall (rotational symmetry) |
| **DEPOT** ×2 | 42 × 27 in against the alliance wall, steel barriers 3 × 1 in | Along the alliance wall (exact spot from drawings) |
| **Driver station** ×6 | 36.8 in diamond plate + 42 in polycarbonate; shelf 69 × 12.25 in; **E-stop left, A-stop right**; team sign on top (field side shows the team # in alliance color); **LED stack: 2 alliance-colored robot LEDs over 1 amber stop LED**; **timer on DS2 only** | 3 per wall plus outpost and tower |
| **FUEL** | 5.91 in (0.150 m) foam ball, 0.45–0.50 lb. **504 per match**: 24 per depot, 24 per chute, ≤8 preload per robot, the rest (360–408) in a 206 × 72 in neutral-zone box | — |
| AprilTags | 36h11, IDs 1–32, 8.125 in tag on a 10.5 in panel. Exact poses come from the WPILib welded JSON (AndyMark variant selectable) | 16 hub, 8 trench, 4 tower, 4 outpost |
| Venue orientation | The arena's LED test modes label hub faces "Facing Scoring Table" and "Facing Audience", so the **scoring table and audience are on opposite long sides** | Configurable |

**Match:** AUTO 20 s, 3 s pause, TRANSITION 10 s, SHIFTS 1–4 at 25 s each, END GAME 30 s. This matches
the arena's `MatchTiming {20,3,10,25,30}`. Both hubs are active in AUTO, TRANSITION and END GAME. In
SHIFTS 1–4 only one is active, alternating. **The AUTO winner's hub is inactive in SHIFT 1.** Ties are
broken randomly by FMS, and the result is sent to all DSs as game data. FUEL scores for 3 s after deactivation
or the end of the match.
**Points:** FUEL is 1 point in an active hub, 0 in an inactive one. TOWER: L1 is 15 in AUTO (max 2 robots) and 10 in TELEOP; L2 is 20; L3 is 30.
**Ranking points:** Energized at 100 FUEL, Supercharged at 360 FUEL, Traversal at 50 tower points.
**Hub lighting:** off pre-match. Alliance color at 100% means active, and pulsing warns of deactivation.
Post-match purple means safe for staff, and green means safe for all.
**Audio cues:** match start, AUTO end, TELEOP start, each SHIFT start, END GAME, match end, match stopped
(foghorn).

---

## 2. What the arena expects from a field (hardware emulators)

| Real device | Arena code | Wire protocol | Sim implementation |
|---|---|---|---|
| 6× Driver Station + robot | `field/driver_station_connection.go` | TCP **1750** handshake (legacy tag 24 / new DS tag 30 with its own UDP port), UDP **1160** status to the arena, control packets from the arena every 500 ms (tag 32 game data), TCP types 20/25/28/29/22/31 | DS client ×6, both protocols |
| Field PLC | `plc/plc.go` | Arena is a Modbus-TCP client to **:502**, slave 0xFF, 100 ms loop: FC15 15 coils (13 before 2026-10-11), FC2 28 inputs, FC3 11 registers | Modbus-TCP **server** + field IO model |
| Freezy alternate IO | `web/alternateIO.go` | `ws /api/plc/websocket` (`setInput`, `setRegisters`), `POST /api/freezy/eStopState`, `/register_values` | Alt-IO client |
| ESP32 E-stop boxes | `plc/esp32IO.go` | Arena checks **TCP :80** on each box every 1 s; boxes POST stops | 3 box emulators on loopback IPs |
| Hub LEDs | `led/*` | **E1.31 sACN UDP 5568**, 16 fixtures × 8 px, 1 or 2 universes | sACN receiver mapped to the 3D light bars |
| Access point | `network/access_point.go` | HTTP `GET /status` (1 s poll), `POST /configuration`, Bearer auth | Fake AP with an RF link model |
| Field switch | `network/switch.go` | Telnet **:23**, per-team VLAN/DHCP config | Fake IOS-style CLI that parses the config |
| Red/Blue SCC | `network/sccswitch.go` | SSH **:22** up/down command sequences | Fake SSH server (`x/crypto/ssh`) |
| Team signs + timers | `field/team_sign.go` | UDP 10011 to hard-coded `10.0.100.x` | Derived from arena state by default; real packet decode if you add loopback IPs |
| Team stack lights | `/api/freezy/team_stack_light` | HTTP JSON | Polled, so the sim renders the arena's own logic |

**PLC map** (must match exactly):
- **Inputs 0–27:** fieldEStop, red1–3 E/A, blue1–3 E/A, red/blueConnected1–3, **ftaReady (19)**, red/blueHubSensor1–4.
  These are **normally closed**, so `true` means *not pressed*.
- **Registers 0–10:** fieldIoConnection (ArmorBlock bits: redDs, blueDs, redIoLink, blueIoLink), red/blueHubTotal,
  red/blueHubCount1–4.
- **Coils 0–14:** heartbeat, matchReset, stack light G/O/R/B, buzzer, fieldResetLight, awardsModeLight, red/blue hub
  motor, red/blue hub light, redWins/blueWins (pulsed 10 s when the score is shown; the PLC runs the winner light
  show itself).

**Arena behaviors the sim must honor:**
- A DS is rejected unless its team is in the loaded match.
- A Modbus PLC overwrites alternate-IO inputs every 100 ms, so the sim runs one mode at a time.
- Hub totals must only go up, and the arena puts each increase into the current shift.
- `matchReset` is a short pulse.
- Start blockers: robots linked or bypassed, no E-stop, A-stops reset, PLC healthy, field E-stop clear, FTA ready, all 4 ArmorBlocks connected.

---

## 3. Architecture

```
┌──────────────────────────────── freezy-sim.exe (Go, no CGO) ────────────────────────────────┐
│                                                                                              │
│  Native windows (WebView2)          Embedded web server :8090 (+ any browser can connect)    │
│  ┌─────────────────────┐            ┌──────────────────────────────────────────────────┐    │
│  │ Main: 3D venue +    │◄──ws────► │ GUI hub: JSON state ws + binary world-snapshot ws │    │
│  │ dockable panels     │            │ reverse proxy /arena/* → arena (same-origin)      │    │
│  │ Pop-outs: displays, │            └───────────────┬──────────────────────────────────┘    │
│  │ mock DS screens ... │                            │ event bus                             │
│  └─────────────────────┘   ┌────────────────────────┼─────────────────────────────────┐     │
│                            │ SIM CORE (authoritative, fixed 120 Hz)                   │     │
│                            │ world physics · robots · fuel · hub mechanism · agents   │     │
│                            │ match model (derived from DS packets) · recorder         │     │
│                            └──┬──────┬──────┬──────┬──────┬──────┬──────┬──────┬──────┘     │
│                               DS×6   PLC   AltIO  sACN   AP/SW/  Signs  Arena   Scenario    │
│                                            ESP32         SCC           client  engine       │
└───────────────────────────────┼──────┼──────┼──────┼──────┼──────┼──────┼───────────────────┘
                     TCP1750/UDP1160  :502  ws/HTTP :5568  HTTP/23/22 10011  HTTP/ws :8080
                                ▼                                            ▼
                  ┌─────────────────── Freezy Arena (sandbox in run/arena, -dev) ─────────────┐
                  └────────────────────────────────────────────────────────────────────────────┘
```

**Why this stack:**
- **Go**, the arena's own language: single static exe, great at networking.
- **WebView2** native windows through `go-webview2`, which is pure Go. The runtime ships with Windows 11.
- **Three.js** for 3D (WebGL2).
- **CSS3DRenderer** puts the arena's real display pages *inside* the 3D venue as live screens.
- No Node, no CGO, no build step: ES modules plus an import map, all `go:embed`-ed.

**The core is authoritative.** Physics, robots and devices all run in Go, so the exact same sim runs
headless for automated tests. The 3D client only interpolates snapshots (robots and 504 FUEL as
quantized binary at 60 Hz) and sends user input.

### Folder layout
```
freezy-sim/
  cmd/freezy-sim/main.go        GUI by default; -headless, -scenario, -event, -config
  internal/
    app/        wiring, config, profiles, lifecycle
    bus/        typed event bus + state store (single source of truth for UI)
    layout/     2026 field layout (AprilTags + element geometry), welded/AndyMark
    world/      physics: robot rigid bodies, fuel spheres (spatial hash), static colliders,
                bump height-field, trench clearance, hub funnel/sensors/motors/exits,
                outpost chute/corral, depot, tower rungs, field reset/staging
    agents/     robot AI (auto routines, teleop roles, shift awareness, endgame climb),
                human players, drive teams (E/A-stop presses), skill profiles
    ds/         DS emulator (new + legacy), comms chain, battery/brownout, mock-DS model
    plc/        Modbus server, IO model, heartbeat watchdog, ArmorBlocks
    altio/      alternate IO client + ESP32 box emulators
    sacn/       E1.31 receiver, fixture maps, pixel buffers
    netsim/     AP (HTTP + RF model), switch (telnet CLI), SCC (SSH), topology model
    signs/      team sign + timer model (derived / packet decode)
    arena/      arena client: observers, match-play / scoring / referee / alliance-selection
                websockets, setup HTTP, settings configurator, event director
    launcher/   sandbox build/copy/run/supervise, log capture
    scenario/   timeline DSL, assertions, runner, built-in acceptance suite, HTML/JUnit report
    recorder/   record/replay, black-box ring buffer
    gui/        http/ws server, snapshot codec, reverse proxy, WebView2 window manager
  web/          index.html, importmap, app/, scene/, ui/, vendor/three/
  layouts/      field-2026-rebuilt.json, venue.json
  profiles/     field profiles (Modbus PLC / Freezy alt-IO / minimal), team & robot profiles
  scenarios/    built-in acceptance suite + examples
  docs/         PLAN.md, PROTOCOLS.md, USER_GUIDE.md
  run/          (gitignored) arena sandbox, recordings, reports
  build.ps1
```

---

## 4. The 3D venue (near-mock field)

Procedurally built from `layouts/field-2026-rebuilt.json`, so dimensions are data, not code.

- **Field:** carpet, all tape lines and zones, guardrails with gates, center line, starting lines,
  outpost areas.
- **Hubs:** prism body, hex top opening, back net, 4 base exits, and **DMX light bars driven by the
  real sACN packets** the arena sends. Visible motor rollers (PLC coil) and per-sensor counters.
  If the motors are off, FUEL backs up inside the hub, so the arena's motor logic visibly matters.
- **Bumps** (15° ramps that robots pitch over), **trenches** (arm, legs, clearance; tall robots can't
  pass), **towers** with three rungs, **outposts** with an animated chute door and corral, and
  **depots**.
- **32 AprilTags** at their exact poses with real 36h11 patterns, generated procedurally.
- **Alliance walls:** diamond plate, polycarbonate, shelves, **E-stop / A-stop buttons you can
  click**, **team signs** (front: team # in alliance color; rear: driver text), **DS2 timers**,
  **LED stacks** (2 alliance + 1 amber, driven by the arena's own stack-light logic), and DS laptops
  showing a **mock FRC Driver Station screen** for each team.
- **Venue:**
  - Scoring table: scorekeeper laptop running **Match Play**, FTA laptop with the **Field Monitor**, head-ref touchscreen with the **Referee Panel**, the **field stack light** tower with buzzer, and a network rack with blinking link LEDs.
  - Audience side: stands and a big projection screen running the **Audience Display**.
  - Queueing screen, announcer desk, pit rankings screen.
- **504 FUEL** as an instanced mesh, plus **6 robots** with bumpers, team numbers, intake, shooter
  and climber animations.
- **Cameras:** free orbit, overhead orthographic (doubles as the 2D map), audience, each of the 6
  driver-station POVs (what drivers see through the glass), robot follow / first person, and a
  **broadcast cam** with the live audience overlay composited on top.
- **Interaction:** click any screen to focus or pop it out, click a robot for its inspector, click a
  device (hub, stack light, E-stop) for its panel.

---

## 5. Every display a real field uses

**A. Arena web displays.** These are live pages shown on virtual screens in the 3D venue, on the display wall, and in
pop-out native windows. Each gets a fixed `displayId=sim-*` so it registers cleanly in the arena's Display Setup.

| Where it lives in the venue | Arena page |
|---|---|
| Audience big screen + broadcast overlay | Audience Display (`background`/`overlayLocation` params) |
| Above each driver station (×6) | Alliance Station Display (`station=R1..B3`) |
| FTA laptop | Field Monitor, FMS Field Monitor |
| Scorekeeper laptop | Match Play, Match Review, Alliance Selection |
| Head-ref touchscreen | Referee Panel |
| Ref tablets (red/blue) | Scoring Panels |
| Alliance E-stop control (Freezy) | E-stop Control Panels (red/blue) |
| Announcer desk | Announcer Display |
| Queueing area | Queueing Display |
| Pits / lobby | Rankings, Wall, Bracket, Logo, Unpicked, Twitch, Webpage, Placeholder |
| Display manager | Setup → Displays, Lower Thirds |

**B. Physical field devices.** The sim renders these itself from protocol data:
- Hub DMX bars (sACN)
- Hub light and motor coils, field stack light and buzzer, field reset light, awards-mode light (PLC)
- Team LED stacks (arena stack-light API)
- Team signs and DS2 timers
- E/A-stop buttons
- Mock DS screens: team, battery, comms/code/joystick LEDs, mode, FMS status, game data, match time, messages
- Network rack link LEDs

**C. Venue modes:**
- **Single-window**: 3D venue plus docked panels.
- **Display wall**: a grid of every display.
- **Multi-monitor event mode**: each display in its own native window, assigned to monitors, with a saved layout, like setting up a real event.

---

## 6. Simulation model

**World physics (Go, fixed 120 Hz):**
- **Robots:** oriented-box rigid bodies with holonomic (swerve) or tank dynamics, accel and speed limits,
  collisions against field geometry and each other, bump height-field with pitch and a speed penalty,
  trench height check.
- **FUEL:** 504 spheres with gravity, foam restitution, rolling friction, a spatial hash for
  ball–ball contact, and collisions with walls, elements and robots.
- **Hub mechanism:** a ball entering the hex opening goes down the funnel and past one of 4 sensors.
  That pulses the matching PLC input and increments its register, and the total is the sum. Then the ball
  waits for the motor coil to eject it through its exit into the neutral zone. The PLC clears counts on the
  `matchReset` edge and watches the heartbeat (outputs drop if the arena stops writing).
- **Outpost:** chute releases on door actuation, corral collects, and human players can throw into the hub.
- **Tower:** robot aligns, climbs to L1/L2/L3 at the real rung heights, and holds.
- **Field reset:** restages all 504 FUEL to official staging (depots, chutes, preloads, neutral box)
  with a "field crew" animation. Runs automatically between matches in automation mode.

**Robots and agents:**
- **Archetypes:** low-profile trench runner, tall bump-crosser, shooter, dumper, defender, climber
  (L1/L2/L3 capability). Each has size, speed, hopper capacity, shot accuracy, cycle time and reliability.
- **Robots act only on what a real robot would know:** the enable/auto bits, match time and **game data** from
  their DS packets, plus the hub lights they "see". If the arena sends wrong data, the robots visibly
  behave wrong.
- **Auto routine library:** shoot preloads, depot run, neutral sweep, L1 climb. **Teleop roles:**
  scorer (holds FUEL while its hub is inactive), feeder, defender. Endgame climb timing.
- **Fault models:** radio drop, RIO reboot, code crash ("No Robot Code"), brownout under load,
  DS laptop crash, Ethernet unplug, packet loss and latency, bandwidth spikes.
- **Drive it yourself:** Xbox controller or keyboard controls any robot (field-oriented), with
  intake/shoot/climb buttons. It only moves while FMS enables that station.
- **Team profiles:** per-team skill so mock events produce realistic, varied rankings.

---

## 7. Tools and panels (docked or popped out)

| Panel | Contents |
|---|---|
| **Match bar** (always on top) | Arena connection, match name/state/period/time, hub shift and countdown, scores, auto winner and game data, readiness light |
| **Field readiness** | Recomputes the arena's start blockers on the sim side and compares them with what the arena reports. Mismatches are flagged. One-click fixes. |
| **Driver stations** | 6 cards: DS/radio/RIO/code/ethernet toggles, protocol, team override, battery, loss, latency, bandwidth, command bits received, DS-reported mode, packet timing chart |
| **Robots** | Pose, hopper, state, AI role and skill, faults, take-control |
| **PLC / field IO** | Server status and poll period, every input (toggle), register (edit), coil (lamp), ArmorBlocks, watchdog, matchReset, mode switch (Modbus or alt-IO) |
| **E-stop boxes** | ESP32 box states, health port, POST log |
| **Lights** | sACN universe viewer, fixture map, current LED mode, coil-driven lights |
| **Network** | Live topology (FMS, switch, AP, SCCs, DS laptops, radios, RIOs), VLANs parsed from the switch config, SSIDs/WPA keys from the AP config, RF quality by robot position, bandwidth vs limit, telnet/SSH transcripts |
| **Team signs** | All 8 signs (front/rear) and timers |
| **Displays** | Display wall, pop-outs, venue-mode monitor layouts |
| **Arena** | Sandbox launcher (build/start/stop/restart), live arena log with warning highlighting (e.g. "Arena loop iteration took a long time"), "configure arena for sim" (PLC/LED/AP/switch/SCC addresses, alt-IO, TBA off) |
| **Automation** | Auto-scorekeeper, auto-referee (tower levels and fouls through the real scoring/referee panels), event director |
| **Scenarios / tests** | Run, step or record scenarios; acceptance-suite results |
| **Inspector** | Packet log for every protocol with filters, timing charts, export |
| **Replay** | Timeline scrubber for recordings and the black box |

---

## 8. Automation and testing

- **Scenario DSL** (JSON timelines with match-relative times and assertions on *both* sim truth and arena
  state). Example: `auto+5s pressAStop R1 → assert ds.R1.enabled==false within 600ms → assert ds.R2.enabled →
  teleop+1s assert ds.R1.enabled`.
- **Built-in FMS acceptance suite (~30 scenarios):**
  - start blockers, each one individually
  - field E-stop abort; team E-stop latching; A-stop only in AUTO plus its reset rule
  - DS reconnect mid-match (status preserved); radio loss and recovery; brownout
  - wrong team / not-in-match rejection; legacy and new DS; game data R/B including tie randomness
  - hub shift scoring; grace periods; inactive-hub FUEL = 0; motor run-out after the match and E-stop motor kill
  - PLC disconnect and heartbeat loss; ArmorBlock drop; LED modes per period
  - AP/switch/SCC configuration per match; stack light and buzzer states; field reset / volunteers signals
- **Headless mode:** `freezy-sim.exe -headless -suite acceptance` launches the sandbox arena, runs everything,
  and writes an HTML + JUnit report. Use it as a regression check after changing Freezy Arena.
- **Event director:** creates a full mock event in the sandbox arena (N teams, schedule, all quals with AI robots
  and auto-scoring, alliance selection, double-elim playoffs, awards). It reports cycle times, start delays and anomalies.
  This runs in real time because the arena's clock is wall time.
- **Recorder:** every run is recorded (world, devices, packets, arena notifications) and can be replayed in 3D.
  A black-box buffer keeps the last 10 minutes.

---

## 9. Milestones

Each milestone ends with verification against the sandboxed arena, and with `go vet` and `go test ./...` passing.

| # | Milestone | Exit criteria |
|---|---|---|
| **M0** | Foundation: module, config/profiles, event bus, GUI server, WebView2 main window, Three.js shell, arena launcher + sandbox + log console, arena observer, reverse proxy | `freezy-sim.exe` opens a native window, starts the sandbox arena, and shows live match state |
| **M1** | Driver stations: both protocols, comms chain, battery, faults, follows match loads, DS panel, mock DS screens | Arena shows 6 green, test match runs, enable/disable at every boundary, game data R/B received |
| **M2** | Field IO: Modbus PLC server, IO model, watchdog, ArmorBlocks, alt-IO + ESP32 boxes, readiness panel | Every start blocker togglable; E/A-stop rules verified; stack lights and buzzer follow the arena |
| **M3** | 3D venue (static): full 2026 field from the layout, AprilTags, walls, DS, venue, cameras | Field matches the manual and AprilTag poses; all camera modes work |
| **M4** | Live field devices in 3D: sACN hub bars, PLC coils, LED stacks, team signs and timers, clickable E/A-stops, DS laptop screens, live CSS3D display screens | Every physical device and display reacts live to the arena |
| **M5** | World physics: robots, 504 FUEL, hub mechanism wired to PLC sensors/registers, outpost/depot, field reset, binary snapshot streaming | Shooting FUEL scores in the arena's realtime score with correct shift attribution |
| **M6** | Robots and agents: archetypes, AI auto/teleop/endgame, shift awareness from game data, human players, gamepad driving | A full match plays out believably; you can drive a robot |
| **M7** | Network: AP with RF model, switch telnet CLI, SCC SSH, topology view, lights panel | Arena reports AP/switch/SCC healthy with network security on; topology is live |
| **M8** | Displays: display wall, multi-window venue mode, broadcast cam overlay, all panels registered | Every arena display is visible and usable from the sim |
| **M9** | Automation and testing: auto-scorekeeper/referee, scenario engine, acceptance suite, headless + reports, event director, recorder/replay, inspector | `-headless -suite acceptance` passes; a mock event runs unattended |
| **M10** | Extras and polish: WPILib robot-sim bridge (real robot code driven by Freezy Arena via `halsim_ds_socket`, pose via NT4), team-sign packet decode with optional loopback IPs, wrong-station simulation, performance pass, user guide, `build.ps1` | Docs complete; optional features behind flags |

---

## 10. Dependencies and environment

- Go 1.27, the WebView2 runtime, `gorilla/websocket` and `golang.org/x/crypto`.
- `github.com/jchv/go-webview2`, pure-Go WebView2 bindings, plus its small deps, via proxy.golang.org.
- `three` (Three.js core and the OrbitControls / CSS3DRenderer / postprocessing addons, about 1.5 MB) from jsDelivr.
  It's vendored into `web/vendor/` so the exe works fully offline.

**Data:** the 2026 AprilTag poses are transcribed into `layouts/field-2026-rebuilt.json`.

## 11. Risks and limitations

- **Fixed ports** (502, 23, 22, 80) are bound on separate loopback IPs (`127.0.0.2+`). Conflicts with an installed
  SSH server or IIS are detected and reported.
- **Firewall:** the arena in `-dev` binds all interfaces, so Windows may prompt once.
- **Team signs and wrong-station detection** need real `10.x` addresses. They're derived from arena state by default,
  and real-packet mode is optional (you'd add loopback IPs yourself).
- **Field details not in the public text** (exact depot spot, DS numbering direction, hub facet shape) are approximated
  in the layout file and can be corrected from the Field Dimension Drawings.
- **CSS3D live screens** are heavy at scale. Screens out of view are paused or shown as placeholders.
- **Real-time arena clock:** a full mock event takes as long as a real one minus field-reset time.
- **Scope:** this is a large build, roughly 15–25k lines across 11 milestones. Each milestone is usable on its own.
