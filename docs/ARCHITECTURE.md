# Architecture

Freezy Sim is one Go program with an embedded web front end. The Go side emulates the hardware, runs the physics and
AI, and drives the arena. The front end (three.js and plain ES modules, with no build step) shows it all in a native
WebView2 window. The two talk over websockets.

```text
                   ┌──────────── freezy-sim.exe ─────────────────────────────────────────────┐
  Freezy / Cheesy  │  ds  fieldio  sacn  netsim     ← emulated hardware (wire protocols)     │
  Arena (sandbox)  │  arena (client)  launcher      ← drives / supervises the arena          │
       ▲  ▲        │  world  referee  fta  chaos    ← physics, AI, officials, faults         │
       │  │        │  scenario  scouting  app/*     ← tests, event director, soak, bundles   │
       │  │        │                 │                                                       │
       │  │        │                bus  ← topics (latest state) · events (log) · commands   │
       │  │        │                 │                                                       │
       │  │        │  recorder (black box, recordings)   inspector (packet log)              │
       │  │        │                 │                                                       │
       │  │        │  gui: HTTP + /ws (JSON) + /ws/world (binary 60 Hz) ──▶ WebView2 window   │
       │  │        └─────────────────────────────────────────────────────────────────────────┘
       │  └── TCP/UDP/Modbus/sACN/HTTP/telnet/SSH, all on loopback
       └───── HTTP + websockets (match play, scoring panels, setup pages)
```

## Ground rules

1. **Over the wire only.** No arena code is imported, linked or patched. Everything the sim knows about the arena
   comes from its network protocols and web interface. A finding is therefore real arena behavior.
2. **The arena folder is read-only.** The launcher builds it into `run/arena/` and runs it there with `-dev`.
3. **The world is authoritative.** What happened on the field is decided once, in `internal/world`. Sensors, referees
   and the score check all read from it.
4. **Everything goes through the bus.** Subsystems publish state and register commands. The GUI, the recorder, the
   tests and the HTTP API all use the same topics and commands.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/freezy-sim` | Flags, finding the sim's home folder, logging, choosing GUI or headless, the AI benchmark entry point |
| `internal/app` | Wires every subsystem together; the event director, soak runner, version compare, arena-diff, bug bundles, pre-season training, arena setup, port preflight |
| `internal/bus` | The central hub: latest value per topic, the event log, command handlers, and subscribers |
| `internal/config` | `freezy-sim.json` and its defaults |
| `internal/version` | The version string stamped into release builds |
| `internal/launcher` | Exports, builds and syncs the arena into the sandbox; starts and supervises it (a kill-on-close job on Windows) |
| `internal/arena` | The arena client: mirrors its websockets into topics; sends match commands; reads and writes settings; scoring panel and referee sessions |
| `internal/ds` | Six driver stations: both FMS protocols, the robot's radio, roboRIO, code and battery, link impairments |
| `internal/fieldio` | The field's physical I/O model, exposed as a Modbus-TCP PLC or as Freezy's ESP32 boxes plus the PLC websocket |
| `internal/sacn` | The E1.31 receiver and the hub LED fixture decoding |
| `internal/netsim` | The access point (HTTP), field switch (telnet) and SCCs (SSH), and their effect on robot links |
| `internal/world` | The physics and AI: field layout, robots, swerve drivetrain, chassis, FUEL, hoppers, hubs, autos, strategy, learning, officials' rules engine, field crew, snapshots, benchmark |
| `internal/referee` | The auto-referee: enters climbs and fouls through the arena's panels; the head referee (cards, field signals) |
| `internal/fta` | The AI FTA: pre-match checklist, helping teams, bypass, field-ready |
| `internal/chaos` | Fault injection, pre-match friction, and recovery checks |
| `internal/scenario` | The acceptance test runner, the built-in suite, the JSON scenario format, HTML and JUnit reports |
| `internal/scouting` | Scouting data, OPR, pick lists, and the alliance selection model |
| `internal/recorder` | The black box, match recordings, the `.fsr` format, and replay into the bus |
| `internal/inspector` | A ring buffer of protocol traffic for the Inspector panel |
| `internal/gui` | The HTTP server, websockets, command API, remote access guard, and field model import |
| `internal/winui` | Native WebView2 windows on Windows (each on its own locked OS thread); a stub elsewhere |
| `internal/logfile`, `internal/safe` | Daily log files, and panic recovery that keeps long-running loops alive |
| `web` | The embedded front end (see below) |

## The bus

`internal/bus` is the spine. It has three kinds of traffic:

- **Topics**: the latest value of a named piece of state, such as `ds.stations`, `field.io`, `world.state`, `score` or
  `arena.<message>`. Publishing replaces the value, and every subscriber gets it as JSON.
- **Events**: timestamped log lines with a level and a source, shown in the Log panel and saved in bundles.
- **Commands**: named handlers such as `match.start`, `field.set`, `ds.set` or `tests.run`, called with JSON arguments.
  They are reachable from the GUI websocket and from `POST /api/cmd/<command>`.

The recorder subscribes to everything, which is why a replay can drive the whole GUI. Topics and events are recorded,
so secrets must never be published as either. Command replies are not recorded.

## The world loop

`internal/world` runs a fixed-rate loop (`World.Run`). Each tick it steps the physics and the AI, and every few ticks
it encodes a binary snapshot (the format is documented at the top of `snapshot.go`), sent at 60 Hz on `/ws/world`. It
also publishes the `world.state` and `world.ai` topics. A panic in a tick is recovered and costs one frame. The AI
benchmark runs the same world without the loop, the arena or the clock.

## The front end

```text
web/
  index.html, ds.html     the main window; the mock driver station screen
  assets.go               go:embed of everything the exe serves
  css/                    styles
  layouts/field-2026.json the field layout, shared with the Go physics
  models/human.fbx        the people (CC0); the field CAD is imported at run time, not embedded
  vendor/three/           three.js r170 and the addons used (MIT)
  js/main.js              boots the store, the dock, the venue and the panels
  js/core/                store.js (websocket client, topics, commands), dom.js (tiny h() helper), match clock,
                          display URLs, pop-out windows
  js/ui/                  the dock, the top bar, the ticker
  js/panels/              one module per panel, each calling registerPanel()
  js/scene/               the three.js venue: field, robots, FUEL, LEDs, devices, people, cameras, quality, replay
```

There is no bundler and no npm. The browser loads the ES modules directly, and an import map points `three` at
`vendor/three`. Run with `-webdir web` to serve the files from disk while you edit them ([DEVELOPMENT.md](DEVELOPMENT.md)).

Some geometry exists on both sides and must match. Hopper packing is in `internal/world/fuel.go` (`hopperGeometry`) and
in `web/js/scene/robots.js` (`hopperShape`). The field layout JSON is read by both.

## Startup

1. `main` parses the flags, moves into the sim's folder, and starts the log file.
2. `app.New` loads the config and creates the bus, world, arena client, launcher, emulators, officials, recorder,
   test runner and GUI server, and binds the GUI port.
3. `app.Start` checks every emulator address, starts the emulators and the world loop, and the launcher builds and
   starts the sandbox arena.
4. When the arena connects, `autoSetup` points its settings at the emulators and seeds teams and a test match.
5. Then either `winui.RunMain` opens the window, or a headless mode runs its job and exits.
