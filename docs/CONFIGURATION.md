# Configuration

The sim reads `freezy-sim.json` from its folder, or another file given with `-config`. Every key is optional: the file
is layered over the defaults, so it only needs what you change. Start from
[`config.example.json`](../config.example.json). If there is no file at all, the sim starts with the defaults and
tells you to create one, because `arena.sourceDir` has no default.

The file is read once at startup, so restart the sim after editing it. The sim never writes this file.

## Example setups

**Freezy Arena, everything on this PC** (the usual case):

```json
{ "arena": { "sourceDir": "C:/code/freezy-arena" } }
```

**Cheesy Arena.** The default `altio` profile uses Freezy Arena's ESP32 E-stop boxes, so use the Modbus PLC profile:

```json
{
  "arena": { "sourceDir": "C:/code/cheesy-arena" },
  "field": { "profile": "plc" }
}
```

**An arena you run yourself.** The sim attaches to it instead of building a sandbox. It never reconfigures an arena it
didn't start, so set the arena's hardware addresses to the sim's yourself ([PROTOCOLS.md](PROTOCOLS.md)):

```json
{
  "arena": { "mode": "external", "host": "127.0.0.1", "httpPort": 8080, "adminPassword": "your-password" }
}
```

**Open the GUI to other devices on your tailnet:**

```json
{ "gui": { "remote": "tailscale" } }
```

**A lighter field.** Driver stations only, with no PLC, no E-stop boxes and no field network:

```json
{ "field": { "profile": "minimal", "networkSecurity": false } }
```

## `gui`: the simulator window and web server

| Key | Default | Meaning |
|---|---|---|
| `gui.bind` | `"127.0.0.1"` | Address the GUI web server listens on. Leave it on loopback and use `gui.remote` to open it to other devices |
| `gui.port` | `8090` | GUI web server port. The native window and any browser use `http://127.0.0.1:8090` |
| `gui.width`, `gui.height` | `1600`, `940` | Size of the main window the first time it opens. After that the sim remembers where you put each window |
| `gui.remote` | `""` | Who else may open the GUI: `""` (this PC only), `"tailscale"` or `"lan"`. See [Remote access](#remote-access) |
| `gui.token` | `""` | The access token for `"lan"` mode. If empty, a random one is generated and printed at each start |

## `arena`: which arena, and how it is run

| Key | Default | Meaning |
|---|---|---|
| `arena.sourceDir` | *(none)* | Your Freezy Arena or Cheesy Arena folder, the one with its `go.mod`. **Only ever read.** Required in `sandbox` mode |
| `arena.mode` | `"sandbox"` | `"sandbox"`: build and run a private copy under `sandboxDir`, and supervise it. `"external"`: attach to an arena that is already running at `host:httpPort` |
| `arena.sandboxDir` | `"run/arena"` | Where the sandbox lives. It must not be inside `sourceDir` |
| `arena.host` | `"127.0.0.1"` | The arena's address. The simulated driver stations connect here too |
| `arena.httpPort` | `8080` | The arena's web port |
| `arena.adminPassword` | `""` | The arena's admin password, if it has one. The sandbox arena starts without one |
| `arena.autoStart` | `true` | Build and start the sandbox arena when the sim starts. With `false`, start it from the Arena panel |
| `arena.autoConfigure` | `true` | On first connect to a sandbox arena: point its hardware settings at the emulated devices; turn off TBA, Nexus and the "lite" DS port; add the team pool to the event; and load a test match. The sim never does this to an external arena |
| `arena.preferBuild` | `true` | Build the arena from source with Go. With `false`, or if the build fails, it copies a prebuilt `cheesy-arena.exe` from `sourceDir` instead |
| `arena.extraArgs` | `[]` | Extra command-line arguments for the arena (it always gets `-dev`) |

If something is already serving the arena's port when the sim starts, it attaches to that instead of launching its own,
and treats it as external.

## `field`: the field hardware profile

| Key | Default | Meaning |
|---|---|---|
| `field.profile` | `"altio"` | `"altio"`: Freezy Arena's alternate IO, with three ESP32 E-stop boxes plus the arena's PLC websocket. `"plc"`: a Modbus-TCP PLC, like an official field and like Cheesy Arena expects. `"minimal"`: driver stations only, no field I/O |
| `field.variant` | `"welded"` | Which field build you are simulating, `"welded"` or `"andymark"`. It is shown in the sim's info; the 3D field and physics are the same for both today |
| `field.networkSecurity` | `true` | Emulate the managed field network: the AP, the field switch and the two SCCs. Robots then only link when the AP broadcasts their SSID, and a DS only reaches the FMS while its SCC port is up. Can also be toggled in the Network panel |

Some acceptance tests need a particular profile (`plchealth` and `armorblocks` need `plc`; `estopbox` needs `altio`)
and skip themselves on the other. To cover everything, run the suite once with each profile.

## `ds`: the six driver stations

| Key | Default | Meaning |
|---|---|---|
| `ds.defaultTeams` | `[254, 1678, 971, 118, 148, 2056]` | Team numbers for R1, R2, R3, B1, B2, B3 when the arena has no match loaded, and for test matches |
| `ds.teamPool` | 36 team numbers | More teams for soak runs, training and the event the sim seeds into the arena |
| `ds.followArena` | `true` | Each DS takes its team from the match the arena loads, as a real team would. With `false`, it always uses its default team, and you can test wrong-team handling by hand |
| `ds.autoConnect` | `true` | Driver stations start powered on and connect by themselves |
| `ds.legacy` | `[false × 6]` | Per station, use the legacy NI DS protocol instead of the current one |
| `ds.baseUdpPort` | `11210` | Station *i* receives FMS control packets on UDP `baseUdpPort + i`. A legacy DS uses 1121, as the real one does |

## `plc`, `altio`, `sacn`: field I/O and LEDs

| Key | Default | Meaning |
|---|---|---|
| `plc.bind`, `plc.port` | `"127.0.0.1"`, `502` | The Modbus-TCP PLC emulator (`plc` profile). The arena's PLC address is set to `plc.bind` |
| `altio.scoreTableIp` | `"127.0.0.5"` | The scoring table E-stop box (`altio` profile) |
| `altio.redIp`, `altio.blueIp` | `"127.0.0.6"`, `"127.0.0.7"` | The red and blue alliance E-stop boxes |
| `altio.healthPort` | `80` | The HTTP port each box answers the arena's health probe on |
| `sacn.bind`, `sacn.port` | `"127.0.0.1"`, `5568` | Where the sim receives the arena's E1.31 (sACN) hub LED packets |

The arena always dials the standard ports (502 for the PLC, 5568 for sACN, 23 for the switch, 22 for the SCCs), so
change the **bind addresses** to move a device, and keep the ports as they are. The ports are configurable only for
running the emulators against other tools.

## `ap`, `switch`, `scc`: the field network

These only run with `field.networkSecurity` on. The passwords are for the simulated devices on loopback, and the sim
configures the arena with the same values. The AP's address includes its port; the switch and SCCs are always reached
on 23 and 22.

| Key | Default | Meaning |
|---|---|---|
| `ap.bind`, `ap.port` | `"127.0.0.1"`, `8081` | The access point's HTTP API |
| `ap.password` | `"simpass"` | The AP admin password |
| `switch.bind`, `switch.port` | `"127.0.0.2"`, `23` | The field switch's telnet CLI |
| `switch.password` | `"simpass"` | The switch password |
| `scc.redBind`, `scc.blueBind` | `"127.0.0.3"`, `"127.0.0.4"` | The red and blue SCC switches |
| `scc.port` | `22` | SSH port for both SCCs |
| `scc.username`, `scc.password` | `"admin"`, `"simpass"` | SSH login for the SCCs |

Port 22 and port 23 are often taken by an installed SSH server or telnet service. The emulated devices bind to their
own loopback addresses (127.0.0.2 and up) to avoid that, but a server that listens on *all* addresses still collides.
The sim checks every address at startup and says which program to stop or which key to change.

## Remote access

By default only this PC can open the simulator. Set `gui.remote` and restart to use it from a laptop, tablet or phone.

- **`"tailscale"`** also listens on this PC's Tailscale addresses. Only devices on your tailnet can connect, since
  Tailscale already authenticates them. If Tailscale connects after the sim starts, it is picked up within 15 s. Get
  Tailscale from [tailscale.com/download](https://tailscale.com/download).
- **`"lan"`** listens on every network interface. Anyone on the network could then drive the sim and the arena, so
  other devices need an access token. Open one of the links printed in the console at startup, or shown in the Arena
  panel (they end in `?token=...`), once. After that a cookie remembers the device for 30 days. Set `gui.token` to keep
  the same token across restarts. The token is kept out of the sim's log files, recordings and bug bundles, so those
  are safe to share.

The reachable addresses are logged at startup and shown in the **Arena** panel's "Remote access" card. On a remote
device:

- The arena screens in the 3D venue and the Displays panel load from the same host, on the arena's port (8080).
- The "open in a window" buttons open a browser tab on that device, not a window on the simulator PC.

If a device can't connect, Windows Firewall is the usual cause. Allow `freezy-sim.exe` (port 8090) and
`cheesy-arena.exe` (port 8080) on the network that device uses. The security side of this is in
[SECURITY.md](../SECURITY.md).
