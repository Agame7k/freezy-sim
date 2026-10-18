# Troubleshooting

Start with the log: the console window, or the newest `run/logs/freezy-sim-<date>.log`. The **Arena** panel shows the
sandbox arena's state and its own log. Most problems are explained there in plain words, with what to do about them.

## Starting up

| Symptom | Fix |
|---|---|
| Red banner "Freezy Sim needs to know where your Freezy Arena (or Cheesy Arena) folder is" | The sim found no arena folder, or more than one. Press **Choose arena folder**, then **Use this one** next to a folder it found, **Browse for the folder…**, or paste the path. Pick the folder with `go.mod` in it ([GETTING_STARTED.md](GETTING_STARTED.md#4-configure)) |
| "no arena folder chosen yet" | As above. Or set `arena.sourceDir` in `freezy-sim.json` |
| "arena.sourceDir is still the example placeholder" | You copied `config.example.json` but didn't change `C:/path/to/freezy-arena`. Choose the folder in the GUI, or edit the file |
| "arena folder … not found" | The path is wrong, or the folder was moved. Choose it again in the Arena panel (**Change arena folder…**) |
| "… is the folder around the arena; choose … instead" | You picked the folder above the arena, such as where a zip was extracted. Pick the folder the message names |
| "does not look like a Freezy Arena or Cheesy Arena folder" | Pick the folder that holds the arena's `go.mod`, not its parent or a subfolder |
| "freezy-sim.json is not valid JSON at line N, column M" | Open the file and look at that line. The message says the likely cause: a single backslash in a Windows path (write `C:/arena` or `C:\\arena`), a missing comma or quote, or an extra comma before `}` |
| "the setting "gui.port" should be a number (no quotes), not a string" | Remove the quotes around the number (`"port": 8090`) |
| Warning "has a setting the simulator does not know, … so it is ignored" | A misspelled key. Check it against [CONFIGURATION.md](CONFIGURATION.md) |
| Warning "Could not read saved robot designs (starting fresh) … kept as run/state.json.corrupt-…" | `run/state.json` was damaged (often by a crash or power cut during a save in an old version). The sim moved it aside and started fresh; the AI relearns over a few matches. Send the `.corrupt-` file with a bug report if it keeps happening |
| An error event "Recovered from a crash: … (stack written to run/logs/panic-….txt)" | A bug in the sim. The affected part kept running or restarted itself. Please open an issue with that file (or press 🐞 **Report** for a bundle). A crash that repeats is reported once every 30 s, with a count |
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
| Viewer mode shows the last match's final score over everything | That is the arena's audience display on its score screen, which is full-screen by design. Change it on the arena's Audience Display controls, or pick **Simple** in the viewer's control strip (move the mouse) |
| Viewer mode shows no score at all | The arena's audience display is blank, or the arena has no audience overlay. Pick **Simple** in the viewer's control strip |
| The field never becomes ready | Hover over **Field Ready** in the top bar, or look at the Match panel's blockers. The FTA in the **Officials** panel lists what it is waiting for. For unattended runs, enable its **auto-bypass** |
| Driver stations don't connect | Check the **Driver Stations** panel: DS on, Ethernet in, radio linked. With network security on, the **Network** panel shows whether the AP is broadcasting that team's SSID. A team that isn't in the loaded match is rejected, as at an event |
| Robots don't move in auto | Check that they are enabled (the DS panel), and that the AI isn't parked: a test may have put robots in manual |
| The score check (arena-diff) fails | Open the bundle the sim saved. `events.log` shows which FUEL or climb the arena credited differently, and the seed reproduces the match ([RECORDINGS.md](RECORDINGS.md)) |
| The sandbox database is in a strange state | Arena panel → **Stop** → **Reset database** → **Start**. Your real arena folder is never touched |
| Matches are slow to cycle | That's the real-time arena clock, plus the field crew resetting the field. A qualification cycle is about 5–7 minutes |
| The 3D view is choppy | Click the graphics button at the top right of the 3D view (it shows the preset, such as **High**) and choose a lower preset, or the simple field model. Close popped-out display windows you aren't using |
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
