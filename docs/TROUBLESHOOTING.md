# Troubleshooting

Start with the log: the console window, or the newest `run/logs/freezy-sim-<date>.log`. The **Arena** panel shows the
sandbox arena's state and its own log. Most problems are explained there in plain words, with what to do about them.

## Starting up

| Symptom | Fix |
|---|---|
| "no freezy-sim.json yet: copy config.example.json…" | Copy `config.example.json` to `freezy-sim.json` in the sim folder and set `arena.sourceDir` ([GETTING_STARTED.md](GETTING_STARTED.md#4-configure)) |
| "arena.sourceDir is not set" | Set `arena.sourceDir` to your arena folder |
| "arena folder … not found" | The path is wrong. Use forward slashes (`C:/code/freezy-arena`) or doubled backslashes (`C:\\code\\freezy-arena`) |
| "does not look like a Freezy Arena or Cheesy Arena folder" | Point `arena.sourceDir` at the folder that holds the arena's `go.mod`, not its parent or a subfolder |
| "Go is not installed or not on PATH" | Install Go from [go.dev/dl](https://go.dev/dl/), then open a new terminal so the PATH updates. Or put a prebuilt `cheesy-arena.exe` in the arena folder |
| The arena build fails | The Arena panel shows the compiler output. The first build downloads the arena's Go modules, so it needs internet once. Check that the arena builds on its own with `go build` in its folder |
| Template errors such as "can't evaluate field … in type" | The sim fell back to an **old prebuilt** `cheesy-arena.exe` in your arena folder that doesn't match the arena's current templates. Fix the build (install Go) or delete the stale exe |
| "Windows protected your PC" | The exe isn't code-signed. Choose **More info → Run anyway**, or build it from source |
| Antivirus quarantines `freezy-sim.exe` | Unsigned Go programs are sometimes flagged by heuristics. Build from source, or add an exception for the sim folder |
| A port conflict is reported (502, 23, 22, 80, 5568, 8081) | Another program holds that address: an SSH server, a telnet service, IIS, a Modbus or sACN tool. The message says which. Stop it, or change that device's bind address ([CONFIGURATION.md](CONFIGURATION.md)) |
| "GUI port 8090 is unavailable (is another freezy-sim running?)" | Another sim (perhaps a headless one), or another program, is on 8090. Close it or set `gui.port` |
| The window doesn't open; the log says "WebView2 is unavailable" | Install the [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/). Meanwhile, open `http://127.0.0.1:8090` in Edge or Chrome |
| Starting the sim only brings an existing window to the front | One sim runs at a time on a PC, because the emulated devices need the same addresses. Close the first one |
| A Windows Firewall prompt for `cheesy-arena.exe` | The arena in `-dev` mode listens on all interfaces. Allowing private networks is enough |

## During matches

| Symptom | Fix |
|---|---|
| The field never becomes ready | Hover over **Field Ready** in the top bar, or look at the Match panel's blockers. The FTA in the **Officials** panel lists what it is waiting for. For unattended runs, enable its **auto-bypass** |
| Driver stations don't connect | Check the **Driver Stations** panel: DS on, Ethernet in, radio linked. With network security on, the **Network** panel shows whether the AP is broadcasting that team's SSID. A team that isn't in the loaded match is rejected, as at an event |
| Robots don't move in auto | Check that they are enabled (the DS panel), and that the AI isn't parked: a test may have put robots in manual |
| The score check (arena-diff) fails | Open the bundle the sim saved. `events.log` shows which FUEL or climb the arena credited differently, and the seed reproduces the match ([RECORDINGS.md](RECORDINGS.md)) |
| The sandbox database is in a strange state | Arena panel → **Stop** → **Reset database** → **Start**. Your real arena folder is never touched |
| Matches are slow to cycle | That's the real-time arena clock, plus the field crew resetting the field. A qualification cycle is about 5–7 minutes |
| The 3D view is choppy | Click **⚙** in the 3D view and choose a lower preset, or the simple field model. Close popped-out display windows you aren't using |
| A gamepad does nothing | Press any button on it once with the window focused: browsers only report a gamepad after it is used |
| The official field model doesn't show | See [FIELD_MODEL.md](FIELD_MODEL.md#troubleshooting) |

## Tests

| Symptom | Fix |
|---|---|
| A timing test fails now and then ("within 0.65 s") | A heavily loaded PC can miss tight timings. Re-run just that test with `-tests <id>`. If it fails every time, it is real |
| A test is "skipped" | It needs another field profile (`plc` or `altio`) or network security, so it skipped itself. The report says why |
| `winnerlights` fails on Cheesy Arena | Expected: it checks Freezy Arena's winner coils |
| `-compare` reports a build problem for a ref | That commit doesn't build. The sim never falls back to another binary for a specific ref |
| `-compare` fails with "git" errors | Install [Git](https://git-scm.com/downloads/win) and make sure the arena folder is a git repository |
| Results differ between versions after a schema change | All versions share the sandbox `event.db`. Reset the database between runs |

## Remote devices

| Symptom | Fix |
|---|---|
| A tablet or laptop can't connect | Check `gui.remote` is set and the sim was restarted. The Arena panel's "Remote access" card lists the addresses. Then check Windows Firewall: allow `freezy-sim.exe` (8090) and `cheesy-arena.exe` (8080) on that network |
| "open the address printed at startup" (LAN mode) | The device needs the token once. Use a link from the console or the Arena panel; they end in `?token=` |
| Tailscale addresses don't appear | Tailscale must be connected on this PC. The sim checks for new addresses every 15 s |

## FAQ

**Does it change my arena folder?**
No. The arena folder is only ever read. The sim builds a private copy into `run/arena/`, and that copy writes its own
database and logs.

**Does it need the internet?**
Only to download things: the arena's Go modules on its first build, and Git clones. The sandbox arena is configured with
The Blue Alliance and Nexus turned off, so simulated results are never published.

**Can I use it with real field hardware or a real Driver Station?**
No. It replaces the hardware, and its devices live on loopback addresses. Mixing real and simulated devices isn't
supported.

**Can it run my team's robot code?**
Not yet. The robots are simulated by the sim's own AI and physics. A bridge to WPILib robot simulation is a possible
future feature.

**Does it work on Linux or macOS?**
The GUI is Windows only, because it uses WebView2. The program compiles for Linux and macOS, and the headless modes may
work there, but they are untested. Reports are welcome.

**Which game?**
2026 REBUILT only. An arena for another season will connect, but the matches won't make sense.

**Where is everything stored, and how do I reset it?**
In `run/` in the sim folder ([GETTING_STARTED.md](GETTING_STARTED.md#where-things-are-kept)). Delete it to start over.

**How do I report a bug?**
Open an issue and attach the bug-report bundle (the 🐞 button saves one). See [CONTRIBUTING.md](../CONTRIBUTING.md).
