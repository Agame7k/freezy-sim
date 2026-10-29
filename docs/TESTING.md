# Testing an arena

Freezy Sim tests an FMS the way a field test at an event does: real driver stations connecting, real E-stops pressed,
real FUEL counted by the hub sensors. The difference is that it is automated, repeatable and needs no hardware. There
are four tools:

| Tool | Use it for |
|---|---|
| [The acceptance suite](#the-acceptance-suite) | "Does the arena still do everything a field needs?" A pass/fail answer in about 15 minutes |
| [Soak tests](#soak-tests) | "Does it survive hours of matches with things going wrong?" |
| [Comparing arena versions](#comparing-arena-versions) | "Did this commit break something that used to work?" |
| [The event director](USER_GUIDE.md#the-event-director) | "Can it run a whole event: quals, alliance selection, playoffs?" |

When any of them finds a problem, the sim saves a [bug-report bundle](RECORDINGS.md#bug-report-bundles) with a replay
of what happened.

## The acceptance suite

```bash
freezy-sim.exe -suite acceptance
```

The suite runs every test against the live sandbox arena in real time, because the arena's match clock is wall time. A
full run takes about 15 minutes. It writes:

- `run/reports/acceptance-<time>.html`, with every test's steps, timings and failures
- `run/reports/acceptance-<time>.xml`, in JUnit format for CI systems

![An acceptance report](images/acceptance-report.png)

It exits with code 1 if any test fails. Run only some tests with `-tests`:

```bash
freezy-sim.exe -suite acceptance -tests estop,fieldestop,grace
```

To watch tests play out in 3D, start them in a running sim through its HTTP command API. There is no button for this
yet. An empty body runs the whole suite:

```bash
curl -X POST http://127.0.0.1:8090/api/cmd/tests.run -d "{\"ids\":[\"estop\",\"grace\"]}"
```

`tests.cancel` stops a run, and `tests.reload` re-reads `scenarios/` after you edit a JSON test, without a restart. The
report is written the same way as from the command line.

### The built-in tests

| Id | Checks |
|---|---|
| `blockers` | A team E-stop and a missing DS each block the match start; bypassing the station clears it |
| `timeline` | Robots enable and disable at every period boundary |
| `astop` | An A-stop disables a robot only during auto |
| `estop` | A team E-stop latches for the rest of the match |
| `fieldestop` | The field E-stop aborts the match and stops the hub motors |
| `gamedata` | Game data names the auto winner |
| `tiegamedata` | A tied auto picks one winner, and every DS gets the same one |
| `shifts` | FUEL is credited to the right hub shift |
| `grace` | 3 s of grace after an active shift; none after an inactive one |
| `endofmatch` | A full match: disable at 2:43, the post-match grace, the 10 s hub-motor run-out, white hub LEDs |
| `leds` | Hub LEDs arrive over sACN |
| `leds-shifts` | The lit hub follows the active shift |
| `reconnect` | A DS that reconnects mid-match is re-enabled |
| `radio` | A robot radio loss and recovery |
| `legacyds` | The legacy NI DS protocol works end to end |
| `wrongteam` | The arena rejects a team that isn't in the match |
| `stacklight` | The field stack light and buzzer |
| `network` | AP SSIDs and switch VLANs are configured for each match (needs `field.networkSecurity`) |
| `tower` | The auto-referee scores an auto climb |
| `plchealth` | Losing the PLC blocks the start (`plc` profile only) |
| `armorblocks` | Each ArmorBlock dropping out blocks the start (`plc` profile only) |
| `estopbox` | An E-stop box going offline is reported (`altio` profile only) |
| `winnerlights` | Showing a committed score pulses the winner coil for 10 s and starts the PLC winner light show (Freezy Arena only) |

The `scenarios/` folder adds JSON tests in the same suite: `astop-json`, `estop-json` and `fieldestop-json`. These are
written in the [scenario format](SCENARIOS.md) and are a good starting point for your own.

Tests that need a particular field profile skip themselves under the other one. To cover everything, run the suite once
with `field.profile` set to `"plc"` and once with `"altio"`.

### Reading a failure

In the HTML report, each failed test is already expanded. It shows every failed expectation (what was expected and what
the sim saw), then the test's step-by-step log.

A bundle is saved beside the report. Open its `blackbox.fsr` in the **Replay** panel to watch the failure in 3D, with
every DS, coil and LED state at each moment ([RECORDINGS.md](RECORDINGS.md)).

Some tests depend on timing, such as "disabled within 0.65 s". On a heavily loaded PC an occasional timing failure can
be the PC, not the arena. Re-run just that test with `-tests <id>` before you start digging.

## Soak tests

```bash
freezy-sim.exe -soak 20 -soak-chaos 2
```

A soak test plays AI matches back to back, with faults injected during each match and friction before it. After every
match, it:

- compares the arena's committed score with what really happened on the field (the arena-diff)
- runs a recovery check for each injected fault, confirming the arena noticed it and recovered
- watches for physics stalls, and for goroutine or heap growth in the sim

It writes `run/reports/soak-<time>.html` and exits 1 if any match was not clean. The chaos levels are 0 (Off),
1 (Occasional, about 1.5 problems a match), 2 (Frequent, about 3.5) and 3 (Mayhem, about 7). The faults are:

- **In the match:** brownouts, radio and roboRIO reboots, code crashes, jams, tipping, E-stops and fouls.
- **Before the match:** teams late from queueing, no-shows, robots left switched off, E-stops left latched.

The same faults can be injected by hand from the **Chaos** panel, which also shows each recovery check as it runs:

![The Chaos panel with a passed recovery check](images/panel-chaos.png)

## Comparing arena versions

```bash
freezy-sim.exe -compare v1.4,HEAD,worktree -tests endofmatch,grace
```

For each ref, the sim:

1. exports that commit of the arena folder with `git archive` into `run/arena-src/<commit>/`. The arena repo is only
   read, and nothing is checked out.
2. builds it into the sandbox, refreshing the sandbox's web assets to match.
3. runs the suite, or the `-tests` subset.

`run/reports/compare-<time>.html` lines the results up side by side. A test that passes on the first ref and fails on a
later one is a **regression**, and the run exits 1. `worktree` means the arena folder as it is, including uncommitted
changes. This needs [Git](https://git-scm.com/downloads/win) on the PATH.

Notes:

- A ref that fails to build is reported as a problem. It never falls back to another binary.
- All versions share the sandbox `event.db`. If a version changes the database schema, reset the database in the Arena
  panel between runs.
- To run the GUI or any other mode against one particular version, use `-arena-ref <ref>`.

## Running the suite in CI

The suite is a normal command-line program with a JUnit report, so it fits any CI system that has a Windows runner. Here
is an example GitHub Actions job for an **arena** repository. It hasn't been run by the project yet, so adjust it to
your setup:

```yaml
# .github/workflows/field-test.yml in your arena repository
name: Field acceptance test
on: [push, pull_request]
jobs:
  acceptance:
    runs-on: windows-latest
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v4
        with: { path: arena }
      - uses: actions/setup-go@v5
        with: { go-version-file: arena/go.mod }
      - name: Download Freezy Sim
        shell: pwsh
        run: |
          gh release download v0.1.0 --repo Agame7k/freezy-sim --pattern "*.zip" --dir sim-zip
          Expand-Archive sim-zip/*.zip -DestinationPath sim
        env: { GH_TOKEN: "${{ github.token }}" }
      - name: Run the acceptance suite
        shell: pwsh
        working-directory: sim
        run: |
          '{ "arena": { "sourceDir": "${{ github.workspace }}/arena" }, "field": { "profile": "plc" } }' |
            Set-Content -Encoding utf8 freezy-sim.json
          ./freezy-sim.exe -suite acceptance
      - uses: actions/upload-artifact@v4
        if: always()
        with: { name: field-test-reports, path: sim/run/reports/ }
```

Use the `plc` profile for Cheesy Arena, or `altio` for Freezy Arena's E-stop boxes. If CI time matters, pick a subset
with `-tests`.

## Testing the sim itself

The sim's own unit tests are plain Go tests and need no arena:

```bash
go test ./...
```

They include a check that draws the 3D model of every robot design in a headless Edge or Chrome and makes sure each
one fits inside its frame, bumpers and height. The packet and file parsers also have fuzz tests, and CI fuzzes each
one briefly on every push. See
[DEVELOPMENT.md](DEVELOPMENT.md#checks) for those and the rest.
