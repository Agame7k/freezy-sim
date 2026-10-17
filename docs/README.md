# Freezy Sim documentation

New here? Start with [Getting started](GETTING_STARTED.md). It takes you from nothing to a match playing on a
simulated field.

## Using the sim

| Guide | What's in it |
|---|---|
| [Getting started](GETTING_STARTED.md) | What to install (with download links), first run, first match, where files are kept |
| [User guide](USER_GUIDE.md) | The main window, the 3D venue, every panel, driving a robot, the event director |
| [Configuration](CONFIGURATION.md) | Every key in `freezy-sim.json`, example setups, remote access |
| [Command line](CLI.md) | Every flag, exit codes, and the files each mode writes |
| [Official field model](FIELD_MODEL.md) | Converting FIRST's field CAD with FreeCAD and importing it |
| [Troubleshooting](TROUBLESHOOTING.md) | Symptoms and fixes, and an FAQ |

## Testing an arena

| Guide | What's in it |
|---|---|
| [Testing](TESTING.md) | The acceptance suite and its tests, soak tests, comparing arena versions, running in CI |
| [Writing test scenarios](SCENARIOS.md) | The JSON scenario format, with every action, control and path |
| [Recordings and replay](RECORDINGS.md) | Match recordings, the black box, bug-report bundles, the `.fsr` format |

## How it works

| Guide | What's in it |
|---|---|
| [Protocols and ports](PROTOCOLS.md) | Each emulated device's protocol, ports and firewall, how the sim drives the arena, arena compatibility |
| [The simulation](SIMULATION.md) | The field, robots, AI, officials, determinism, and how far to trust it |
| [Architecture](ARCHITECTURE.md) | The packages, the bus, the world loop, the front end, startup |

## Working on the sim

| Guide | What's in it |
|---|---|
| [Development](DEVELOPMENT.md) | Building, checks, common changes, style |
| [Releasing](RELEASING.md) | The release checklist |
| [Contributing](../CONTRIBUTING.md) | How to report bugs and send changes |
| [Original design plan](design/original-plan.md) | The research and plan the sim was built from (historical) |
