# Changelog

All notable changes to Freezy Sim are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/). Before 1.0, a minor version (0.x) may change the config,
the scenario format or the recording format.

## [Unreleased]

### Added
- A test that draws every robot the simulator can build: each team-built design (on all three drives) and the stock
  robots, about 7,800 in all. It runs in a headless Edge or Chrome, on every push in CI. It fails if a model throws an
  error, or if any part sticks out past the frame and bumpers or above the robot's height while stowed, climbing or
  with a turret turned, or if a deployed intake reaches more than 12 in past the frame. The designs come from the
  simulator itself, so new kit parts are covered without changing the test. See
  [DEVELOPMENT.md](docs/DEVELOPMENT.md#checks).

### Changed
- Robots look more like real ones:
  - The bumpers are the deep red and royal blue cloth teams use, with white team numbers printed on the cloth. Before,
    the numbers sat on a darker box and a plywood strip showed along the top.
  - The frame tube is pre-drilled, with a row of holes every 1/2 in.
  - Battery leads, motor power and the yellow CAN cable run from the battery and power distribution hub to every swerve
    module or gearbox. The battery has its label and strap.
  - The shooter hood is smoked polycarbonate, so the flywheels show through it.
  - Swerve modules look like the COTS modules teams buy: a fixed housing with the motors, pulley cover and absolute
    encoder on top, and a fork round the wheel that steers under it. Before, the whole module turned, motors and all.
  - Teams differ in their shooters: about half drive the flywheels straight from the motors and half belt them up from
    motors low on the side plates, and flywheel wheels come in black, orange, blue or grey.
  - The team number is on both hopper side walls in white vinyl too, where the wall is big enough.

### Fixed
- A stowed intake stuck out in front of the bumpers. It now folds up inside the frame, and lies back over the hopper on
  short robots so it stays under the robot's height.
- An intake wider than the frame (the widest intake option in a team's design) stuck out past the side bumpers even
  when stowed. Its roller now fits between the frame rails, and extensions slide out of the ends when it deploys.
- On tall robots the stowed climber stuck out past the back bumper. It now stays inside the frame.
- On the West Coast and kit drives, the dropped center wheels sank into the carpet. The center wheels now touch the
  carpet and the outer wheels ride just above it.

## [0.1.2] - 2026-10-19

### Added
- Every button in the GUI says what it does when you hover over it. That covers the Match, Driver Stations, Field
  I/O, Robots, Network, Chaos, Match Log, Replay, Inspector, Log, Arena and AI panels and the graphics menu. Toggles
  such as the chute doors, Ethernet cables, E-stops, network devices and bypass also say what a click will change, and
  report their state to screen readers.
- Confirmation before **Discard results**, **Reset sandbox DB**, **Rebuild from source** and removing the imported
  field CAD. Each says what will happen and whether it can be undone.
- A test that fails if a GUI button sends a command that no part of the simulator handles, so a renamed handler or a
  typo is caught before anyone clicks the button. Also tests for the **Follow arena teams** setting and the long
  sandbox path message.

### Changed
- Buttons are greyed out when they can't work, instead of failing when clicked:
  - The match buttons in the top bar and the Match panel while the arena is offline.
  - In the Match panel, the actions the arena would ignore or reject in the current match state. These are Start
    without Field Ready, Commit and Discard before the match ends, field-reset and volunteer signals during a match,
    and Load during a match. The Match panel now follows the same rules as the top bar.
  - The Arena panel's **Start** while the arena runs, **Stop** while it is stopped, and **Reset sandbox DB** until
    the arena is stopped.
  - The event director's **Run** while it is running, and **Stop** while it isn't.
- Clearer toggle labels: **Door open** / **Door closed** (was Open / Closed), **Ethernet plugged in** / **Ethernet
  unplugged** (was "Ethernet ✓" / Unplugged), and **FTA ready: on** / **off**. The foul buttons read **Minor foul on
  red** and **Major foul on red** (was "Major on red").
- The Match panel's schedule loads as soon as the arena connects, and again after it reconnects. Before, it loaded
  once, 1.5 s after the page opened, and kept showing an error if the arena wasn't up yet.
- Opening a saved match report scrolls down to it.
- The docs no longer refer to the 🐞 and 🖥 buttons, which became the **Report** button and a screen icon in 0.1.1.
  The user guide has a new "Buttons" section.

### Fixed
- **Follow arena teams** in the Driver Stations panel always showed as ticked, even when `ds.followArena` was off in
  `freezy-sim.json`. It now shows the real setting.
- **Drive** and **Follow camera** in a popped-out Robots panel did nothing. They now say they need the main window's
  3D view.
- The Inspector's **Clear**, the event director's **Stop** and the Driver Stations panel's **Follow arena teams**
  ignored errors. They now show them.
- The replay play button's screen-reader label always said "Play", even while playing. The −5 s and +5 s buttons no
  longer seek before the start or past the end of the recording.
- **Use this path** in the arena-folder setup with an empty box now asks for a path instead of trying to use "".
- A sim in a deeply nested folder couldn't start the arena, and said only "The directory name is invalid". Windows
  can't start a program in a folder whose path is longer than 258 characters. The Arena panel now says that the path
  is too long, and how to fix it: move the sim somewhere shorter, or set `arena.sandboxDir`.

## [0.1.1] - 2026-10-18

### Added
- First-run setup without editing JSON. The sim looks for the arena folder next to itself and in the usual places
  (home, Documents, Desktop, Downloads, `source`, `code`, GitHub, OneDrive) and uses it if it finds exactly one.
  Otherwise the Arena panel offers the folders it found, a folder picker and a path box. The choice is saved to
  `freezy-sim.json`, and **Change arena folder…** switches it later.
- A banner for an arena that never started, such as a missing arena folder or a failed build, with a button that
  opens the fix. Before, only a lost connection got a banner, so a first run that failed showed nothing.
- Startup warnings for misspelled config keys, which were ignored silently before.
- Fuzz tests for the FMS UDP/TCP packets, Modbus requests, sACN datagrams and recording files. CI fuzzes each for 15 s.
- The app icon in the top bar and as the favicon of the GUI and the mock driver station.
- **Viewer mode**: the match as a spectator sees it. Open it with the **Viewer** button or a `?view=viewer` link.
  - The panels and tools are hidden; the auto-broadcast camera fills the window.
  - The arena's own audience overlay is keyed over the picture, using its real audience display with a transparent
    background, so it always matches the arena.
  - **Simple** switches to the sim's own score bug, with "Up next" and result cards.
  - A control strip that hides itself offers Auto, Wide, Overhead and end cameras, Full screen (**F**) and Exit
    (**Esc**).

### Changed
- Releasing no longer needs the changelog edited by hand. When a pushed version tag has no section of its own in
  `CHANGELOG.md`, the Release workflow moves the "Unreleased" notes under `## [<version>] - <date>` and uses them as
  the release notes. It then commits the changelog back to `main` (only if `main` is still at the tagged commit).
  `tools/changelog.sh` does the same by hand (`promote <version>`) and prints a version's notes (`notes <version>`).
- The auto-broadcast director is smarter:
  - Before a match it shows the venue, then introduces the red and the blue alliance. After the match it shows the
    final score.
  - Wide shots are framed to fit every robot. Before, a low shot of the middle of the field could show only the FUEL.
  - It covers robots battling for position and calls out an alliance on a scoring run ("Red on a 21-point run").
  - It shares airtime: a robot that just had the camera waits its turn, and one nobody has seen for a while gets a
    look. A long shot moves on to the next best play instead of holding on the same robot.
- A visual pass over the GUI:
  - The 14 dock tabs, which wrapped into three ragged rows, are now four groups (**Match**, **Field**, **Simulation**,
    **System**), each with one row of panels. A group remembers its last panel and shows a dot when one of its panels
    has a count to look at (start blockers, problems, fouls, faults).
  - Emoji and symbol characters (🐞 📺 👁 🎮 🎥 ⚙ ▶ ■ ⧉ ✕) are replaced by one set of line icons that looks the same
    on every PC.
  - Buttons are calmer. Only the main action is filled, and destructive ones are tinted rather than bright.
  - Dropdowns, checkboxes, text boxes, scrollbars, focus rings, cards, tables, toasts and the 3D overlay buttons got
    a consistent style.
  - Hint text has more contrast.
- The browser's own `prompt()` and `confirm()` boxes are replaced by in-app dialogs (Report, Rebuild, Reset learning,
  Forget, Delete recording). Each says what will happen and whether it can be undone.
- The footer ticker no longer shows the arena's own console warnings (such as "Arena loop iteration took a long
  time") or field I/O value changes. Both are still in the Log panel.
- Controls inside a card heading no longer inherit its small caps ("QUALIFICATIONS"). The event director's controls
  have their own row, and its match limit is labeled. Wide tables scroll sideways instead of squeezing every cell
  into a column of single words.
- The screenshots in `docs/images` show the new GUI. That covers the venue, the graphics menu and ten panels,
  captured mid-match on the official field CAD.
- Config errors give the line and column. For a single backslash in a Windows path, the error says how to write it.
  A wrong type names the setting and what it should be. A Notepad byte-order mark no longer breaks the file.
- Error messages about the arena folder show paths as typed (no doubled backslashes). They also say when you chose
  the folder around the arena, or left the example placeholder in place.
- A crash that repeats is reported once every 30 s, with a count of the repeats, instead of filling the event log
  120 times a second.

### Fixed
- Team Wi-Fi keys no longer leak into the bus. The arena sends each team's Wi-Fi key in its match-load and status
  messages, and the sim republished them as topics, so the keys reached every GUI client, match recordings, the black
  box and bug bundles.
  - Arena messages are now cleaned before they are published: any field named like a Wi-Fi key, password, token or
    secret is blanked. Bug bundles run the same pass over their topic dump.
  - The network panel's published state no longer carries the salted key hashes. The arena's AP status API still
    returns them.
  - Robots still link with network security on, because the emulated AP gets its keys from the arena's own
    configuration requests.
  - Recordings and bundles made before this fix still contain the keys. The bundle's copy of the sandbox arena
    database (`arena/event.db`) holds them by design.
- The **Audience** camera was inside the crowd, showing the backs of spectators' heads. It is now high above the back
  rows of the stands, in front of the screen behind them, and shows the whole field.
- In a narrow window, such as a popped-out panel, the top bar no longer stretches the page, which pushed panel
  content off the right edge. It scrolls sideways instead, and pop-outs hide the brand text.
- The Inspector's millisecond timestamps no longer wrap their last digit onto a second line under every packet.
- A panic in a background goroutine no longer takes down the sim and the arena with it. This covers the arena client,
  driver-station sockets, the Modbus PLC, network emulators, recorder, replay, launcher, chaos, FTA, referee, director,
  training and the world loop. Loops restart themselves, one-shot work is logged and dropped, and a panicking bus
  listener no longer breaks publishing for everyone else.
- A damaged recording (`.fsr`) could crash the sim on load: a negative entry count panicked, and a huge one could
  exhaust memory. Chunk sizes and counts are now bounded.
- A damaged `run/state.json` was overwritten on the next save, losing every team's learned history. It is now moved
  aside to `state.json.corrupt-<time>` first.
- `freezy-sim.json`, the head referee's ledger, window placements, the AI bench baseline and recordings are saved
  atomically, through a temporary file and a rename. A crash or power cut mid-save no longer leaves a truncated file.
  The rename is retried when antivirus or OneDrive briefly holds the file open.

## [0.1.0] - 2026-10-17

The first public release.

### Emulated field hardware
- Six driver stations speaking both FMS protocols (current and legacy NI DS), with simulated radio, roboRIO, robot code
  and battery, and adjustable loss, latency and jitter.
- Field I/O as a Modbus-TCP PLC (`plc` profile) or as Freezy Arena's ESP32 E-stop boxes plus the PLC websocket
  (`altio` profile): E-stops, A-stops, Ethernet sensing, the FTA switch, hub FUEL sensors, stack lights, hub motors,
  and the winner coils.
- An sACN (E1.31) receiver that paints the arena's hub LED packets onto the 3D hubs.
- The managed field network: a Vivid-style access point (HTTP), a Cisco-style switch (telnet) and two SCCs (SSH), which
  feed back into whether robots link.
- Startup checks for every emulator address, with clear messages for port conflicts.

### The venue and the simulation
- A 3D venue with the 2026 REBUILT field, live LEDs and stack lights, the arena's displays on virtual screens,
  broadcast cameras, and animated referees, drive teams, human players, field crew, FTA and crowd.
- An optional official field model: convert FIRST's field CAD with FreeCAD and import it from the graphics menu.
- Swerve robots built by each team from a kit of parts, with a per-module drivetrain force model, a chassis that rides
  the bumps and can tip, simulated hoppers, and FUEL with spin.
- Physical hubs with beam-break counting at the exit gates.
- AI drive teams with autonomous routines on odometry and vision, strategy that follows hub shifts, learning across an
  event, mistakes, reliability and repairs.
- An AI rules engine, head referee (cards, field signals), FTA (checklist, bypass) and field crew.
- Manual driving with the keyboard or a gamepad.

### Testing
- An FMS acceptance suite of 23 tests, with HTML and JUnit reports (`-suite acceptance`, `-tests`).
- Acceptance tests written in JSON (`scenarios/`), with three examples.
- Soak tests with fault injection, pre-match friction and recovery checks (`-soak`, `-soak-chaos`).
- An arena-diff after every match: the arena's committed score against the simulated field's own record.
- Comparing arena versions by git ref, with regression detection (`-compare`, `-arena-ref`).
- An unattended event director: practice, quals, scouting, alliance selection and double-elimination playoffs
  (`-director`).
- Match recordings, a 10-minute black box, and replay of the whole GUI.
- Bug-report bundles, saved on failures or on demand, with passwords and tokens removed.
- A deterministic offline AI benchmark (`-suite ai`).

### Project
- Remote access over Tailscale or the LAN, with a token. Cross-site requests to the GUI are refused.
- `-version`, and a `release.cmd` that builds the release zip with a checksum. Pushing a `v*` tag runs a GitHub
  Actions workflow that builds the zip and publishes the release.
- Full documentation in `docs/`, the MIT license, and third-party notices.
