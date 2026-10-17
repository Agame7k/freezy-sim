# Changelog

All notable changes to Freezy Sim are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/). Before 1.0, a minor version (0.x) may change the config,
the scenario format or the recording format.

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
