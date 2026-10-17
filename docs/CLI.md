# Command line

```bash
freezy-sim.exe [flags]
```

With no flags, the sim opens the main window and runs until you close it. Any of `-suite`, `-soak`, `-director` or
`-compare` runs **headless**: no window, do the job, write reports, exit. Run `freezy-sim.exe -help` for the built-in
list.

The sim works from its own folder, the one with `freezy-sim.json` and `run/`. Started from somewhere else (a shortcut,
or a terminal in another folder), it moves there first. The exception is a `-config` given as an absolute path.

## General

| Flag | Default | What it does |
|---|---|---|
| `-config <path>` | `freezy-sim.json` | The config file ([CONFIGURATION.md](CONFIGURATION.md)) |
| `-headless` | off | Run without opening a window. The GUI is still served at `http://127.0.0.1:8090` for a browser. Ctrl+C quits |
| `-no-arena` | off | Don't build or start the sandbox arena. Start it later from the Arena panel |
| `-arena-ref <ref>` | — | Build the sandbox arena from this git ref of the arena folder: a tag, branch, commit or `HEAD~2`. The folder is exported with `git archive` and never checked out or modified. `worktree` means the folder as it is. Works with every mode |
| `-version` | — | Print the version, commit and Go version, then exit |
| `-debug` | off | Enable the WebView2 developer tools (F12, or right-click → Inspect) |
| `-webdir <dir>` | — | Serve the GUI from this folder instead of the copy built into the exe. For front-end development: edit `web/`, reload the window, no rebuild ([DEVELOPMENT.md](DEVELOPMENT.md)) |

Only one windowed sim runs at a time. Starting a second one brings the first window to the front, and the second exits.

## Acceptance suite

| Flag | Default | What it does |
|---|---|---|
| `-suite acceptance` | — | Run the FMS acceptance suite against the sandbox arena, write the reports, and exit |
| `-tests <ids>` | all | Comma-separated test ids to run, for example `-tests grace,leds-shifts,estop-json`. The ids are listed in [TESTING.md](TESTING.md#the-built-in-tests) |
| `-compare <refs>` | — | Comma-separated arena refs, for example `v1.4,HEAD,worktree`. Builds each version, runs the suite (or `-tests`) against it, and writes a side-by-side report. Implies `-headless` and `-no-arena` (it starts each version itself) |

## Soak test and event director

| Flag | Default | What it does |
|---|---|---|
| `-soak <n>` | `0` | Play *n* AI matches back to back with injected faults, checking the arena after each, and exit |
| `-soak-chaos <0-3>` | `2` | How much goes wrong during `-soak`: 0 Off, 1 Occasional (about 1.5 problems a match), 2 Frequent (about 3.5), 3 Mayhem (about 7). Problems are faults in the match (brownouts, radio drops, code crashes, jams, E-stops) and, at any level but Off, pre-match friction (late teams, no-shows, robots left off, E-stops left latched) |
| `-director <type>` | — | Run the event director unattended and exit: `practice`, `qualification`, `selection`, `playoff`, or `event` (remaining quals, then scouting and alliance selection, then the playoffs) |
| `-director-limit <n>` | `0` | Stop after *n* scheduled matches (0 = all of them) |

## AI benchmark

The benchmark plays whole matches inside the world model, with no arena and no network, much faster than real time.
Use it to measure changes to the robots and AI.

| Flag | Default | What it does |
|---|---|---|
| `-suite ai` | — | Run the benchmark and exit |
| `-bench-matches <n>` | `10` | Matches to play |
| `-bench-seed <n>` | `1` | Random seed. The same seed plays the same matches |
| `-bench-workers <n>` | half your CPU cores | Independent events played side by side |
| `-bench-perfect-info` | off | Let the AI see every ball (no vision limits) |
| `-bench-chaos` | off | Inject random robot faults |
| `-bench-save-baseline` | off | Save this run as the baseline that later runs are compared to |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success: the tests passed, the soak or director run was clean, or the window was closed |
| `1` | A test failed, a soak match or director run had an anomaly, a compare found a regression, the arena never became ready, or the sim couldn't start (the reason is the last log line) |

Ctrl+C in a headless run stops the arena and exits.

## What each mode writes

Everything goes under `run/` in the sim folder. Times in file names are local, as `YYYYMMDD-HHMMSS`.

| File | Written by |
|---|---|
| `run/reports/acceptance-<time>.html` and `.xml` (JUnit) | `-suite acceptance`, and suite runs from the GUI |
| `run/reports/compare-<time>.html` and `.json` | `-compare` |
| `run/reports/soak-<time>.html` | `-soak` |
| `run/reports/event-<type>-<time>.html` | `-director`, and the director in the Match panel |
| `run/reports/scouting-<time>.csv`, `picklist-<time>.csv` | Scouting before alliance selection |
| `run/reports/<time>-<match>.json` | The auto-referee's report for each match |
| `run/reports/ai-bench-<time>.html` and `.json`, `ai-baseline.json` | `-suite ai`; the baseline with `-bench-save-baseline` |
| `run/reports/bundle-<time>.zip` | Bug-report bundles, saved on any failure or with the 🐞 button ([RECORDINGS.md](RECORDINGS.md#bug-report-bundles)) |
| `run/recordings/*.fsr` | One recording per match, for the Replay panel |
| `run/logs/freezy-sim-<date>.log`, `crash-<time>.txt` | The sim's log, one file per day, kept for two weeks; the stack of any recovered crash |

## Examples

Run two tests against the arena exactly as it is on disk:

```bash
freezy-sim.exe -suite acceptance -tests estop,grace
```

Check whether the last arena commit broke anything:

```bash
freezy-sim.exe -compare HEAD~1,HEAD
```

Play an unattended qualification block of 6 matches:

```bash
freezy-sim.exe -director qualification -director-limit 6
```

An overnight soak with lots of faults:

```bash
freezy-sim.exe -soak 100 -soak-chaos 3
```

Benchmark the AI over 50 matches and keep the result as the new baseline:

```bash
freezy-sim.exe -suite ai -bench-matches 50 -bench-save-baseline
```
