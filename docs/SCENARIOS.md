# Writing test scenarios

An acceptance test is a **timeline**: do something at a point in the match, then check that the arena reacted. You
write one as a JSON file in the `scenarios/` folder. The suite picks it up next to the built-in tests. You don't need
to touch Go or rebuild anything.

## A first scenario

Save this as `scenarios/my-astop.json`:

```json
{
  "id": "my-astop",
  "name": "A-stop disables only during auto",
  "description": "R1's A-stop disables just R1 for the rest of auto; it is enabled again in teleop.",
  "steps": [
    { "do": "startMatch" },
    { "at": "auto+3", "do": "press", "control": "teamAStop", "index": 0 },
    { "expect": "ds.R1.enabled == false", "within": 1, "fatal": true },
    { "expect": "ds.R2.enabled" },
    { "do": "release", "control": "teamAStop", "index": 0 },
    { "expect": "!ds.R1.enabled", "hold": 1.5 },
    { "at": "teleop+0.3", "expect": "ds.R1.enabled", "within": 1.5 },
    { "do": "abort" }
  ]
}
```

Run it on its own:

```bash
freezy-sim.exe -suite acceptance -tests my-astop
```

Here is what happens, step by step:

1. Before the first step, the field is made ready (`setup`, below): a match is loaded, every DS is connected and the
   arena can start.
2. `startMatch` starts the match.
3. At 3 s into auto, R1's A-stop is pressed. R1 is station index 0.
4. Within 1 s, R1's driver station must report disabled. If not, the test fails and stops there (`fatal`).
5. R2 must still be enabled, checked once.
6. The A-stop is released. R1 must stay disabled for 1.5 s (`hold`), because an A-stop lasts the rest of auto.
7. 0.3 s into teleop, R1 must be enabled again within 1.5 s.
8. The match is aborted, so the next test starts clean.

## File format

A file holds one scenario, or an array of them (see [`scenarios/estop-and-hub.json`](../scenarios/estop-and-hub.json)).

| Key | Required | Meaning |
|---|---|---|
| `id` | yes | Unique id, used with `-tests` and in reports. Use lowercase and dashes |
| `name` | yes | One-line title shown in reports |
| `description` | no | What the test proves |
| `setup` | no | `"readyField"` (the default) gets a match loaded and the field ready before the first step. `"none"` starts from whatever state the arena is in |
| `steps` | yes | The timeline, run in order |

A step can wait (`at`), act (`do`), check (`expect`), or wait and then act or check in one step.

## Waiting: `at`

| Value | Waits until |
|---|---|
| `"auto+5"` | 5 s into auto |
| `"match+30"` | 30 s after the match start (the same clock as `auto`) |
| `"teleop+1"` | 1 s into teleop |
| `"+2"` | 2 s after the previous step finished |

Match times follow the arena's match clock, so they line up with what the arena does. If a step's time has already
passed, it runs at once.

## Acting: `do`

| Action | Extra keys | What it does |
|---|---|---|
| `startMatch` | — | Starts the loaded match |
| `readyField` | — | Loads a test match if needed and waits until the arena can start it |
| `abort` | — | Aborts the match |
| `press`, `release` | `control`, `index` | Presses or releases a field control (see below) |
| `set` | `control`, `index`, `value` | Sets a field control to `value` |
| `ds` | `station`, `field`, `value` | Changes a driver station or its robot (see below) |
| `manualAll` | — | Parks every robot: the AI stops playing, so only your steps change the score |
| `cmd` | `command`, `args` | Runs any simulator command (the same ones the GUI uses), for example `{"do": "cmd", "command": "field.fuel", "args": {"alliance": 0, "sensor": -1, "count": 5}}` |
| `wait` | `seconds` | Waits that long |
| `log` | `message` | Writes a line to the test log |

**Field controls** for `press`, `release` and `set`:

| `control` | `index` | |
|---|---|---|
| `fieldEStop` | `0` | The scoring table's field E-stop |
| `teamEStop` | `0`–`5` | A station's E-stop (R1, R2, R3, B1, B2, B3) |
| `teamAStop` | `0`–`5` | A station's A-stop |
| `ftaReady` | `0` | The FTA ready switch |
| `armorBlock` | `0`–`3` | Whether an ArmorBlock I/O module is connected (`plc` profile) |

**Driver station fields** for `ds`. Set `station` to `R1`…`B3`, or `all`:

| `field` | `value` | |
|---|---|---|
| `dsOn` | `true` / `false` | Driver Station software running |
| `ethernet` | `true` / `false` | Field Ethernet cable plugged in |
| `team` | number | Override the team number (0 = follow the arena) |
| `legacy` | `true` / `false` | Use the legacy NI DS protocol (reconnects) |
| `lossPct`, `latencyMs`, `jitterMs` | number | Packet loss and delay between the DS and the FMS |
| `robotPower` | `true` / `false` | Robot main breaker. Turning it on boots the radio and roboRIO |
| `radio`, `rio`, `code` | `true` / `false` | Radio up, roboRIO up, robot code running. Turning one back on reboots it |
| `autoClearEStop` | `true` / `false` | Let the robot clear a latched E-stop by itself once the FMS releases it, as the real DS does (default on) |
| `soc` | `0`–`1` | Battery state of charge |
| `batteryFull`, `resistance` | volts, ohms | The battery's full-charge voltage and its internal resistance |

## Checking: `expect`

An expectation is one of:

- `"path"`: the value must be true (or non-zero, or non-empty)
- `"!path"`: it must be false
- `"path op value"`, with `op` one of `==`, `!=`, `<`, `<=`, `>`, `>=`. For example `"arena.state == PostMatch"` or
  `"score.red.autoFuelPoints >= 3"`

How long it is given:

| Option | Meaning |
|---|---|
| *(none)* | Checked once, right now |
| `"within": s` | Polled until it holds; fails after `s` seconds |
| `"hold": s` | Must stay true the whole `s` seconds |
| `"fatal": true` | If it fails, stop the scenario here. Later steps would only add noise |

### Paths

| Path | What it reads |
|---|---|
| `ds.<station>.<field>` | What the FMS told a DS: `enabled`, `auto`, `estop`, `astop`, `gameData`, `remainingSeconds`… |
| `arena.<field>` | The arena's status: `canStartMatch`, `isFtaReady`, `plcIsHealthy`… |
| `arena.state`, `arena.time` | The match state (`PreMatch`, `StartMatch`, `AutoPeriod`, `PausePeriod`, `TeleopPeriod`, `PostMatch`, `TimeoutActive`, `PostTimeout`) and the match time in seconds |
| `arena.station.<station>.<field>` | The arena's view of a station: `bypass`, `estop`, `astop`, `dsConn.robotLinked`… |
| `coil.<name>` | A field I/O coil by name or number: `heartbeat`, `matchReset`, `stackGreen`, `stackOrange`, `stackRed`, `stackBlue`, `buzzer`, `fieldResetLight`, `awardsModeLight`, `redHubMotor`, `blueHubMotor`, `redHubLight`, `blueHubLight`, `redWins`, `blueWins`. The arena's own names (`stackLightRed`…) work too |
| `score.<red\|blue>.<field>` | The realtime score: `score`, `autoFuelPoints`, `teleopTowerPoints`… |
| `world.<station>.<field>` | The simulated robot: `hopper`, `scored`, `climbLevel`, `aiState`, `autoName`… |
| `topic:<topic>/<a>/<b>` | Anything published on the simulator's bus, by JSON path |

Field names match case-insensitively. The **Inspector** panel and the `GET /api/topic/<topic>` endpoint
(`http://127.0.0.1:8090/api/topic/ds.stations`, for example) show what is available.

## Tips

- **Start from a copy** of one of the files in `scenarios/`.
- **Put `fatal` on the first check** that everything else depends on.
- **Leave timing margins.** The arena sends each driver station a control packet every 0.5 s (sooner when something changes), so give `within` at least 0.65 s.
- **End with `abort`** when you started a match, so the next test starts clean.
- **Use `manualAll`** when you count points, so the AI doesn't score too.
- **Reload without restarting:** if the sim is open, `POST /api/cmd/tests.reload` re-reads `scenarios/`.

A JSON scenario that can't be parsed is reported as a failed test with the parse error, so a typo never passes
silently.
