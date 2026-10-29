# Development

How to build, test and change Freezy Sim. Read [ARCHITECTURE.md](ARCHITECTURE.md) first for the map of the code.

## Setup

| | Get it |
|---|---|
| Go 1.27 or newer | [go.dev/dl](https://go.dev/dl/) |
| Git | [git-scm.com/downloads/win](https://git-scm.com/downloads/win) |
| An arena checkout, for anything beyond unit tests | [Freezy Arena](https://github.com/Freezy-Arena/freezy-arena) or [Cheesy Arena](https://github.com/Team254/cheesy-arena) |
| Windows 10/11 with the WebView2 Runtime, for the GUI | [WebView2](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) |

```bash
git clone https://github.com/Agame7k/freezy-sim.git
```

Then make a `freezy-sim.json` as in [GETTING_STARTED.md](GETTING_STARTED.md#4-configure). It is git-ignored, along
with `run/`, the exe and the other local files.

## Build and run

```bash
build.cmd
```

`build.cmd` is the same as `go build -o freezy-sim.exe ./cmd/freezy-sim`, but first moves a running exe aside, because
Windows won't overwrite a running program. Plain `go build` in the root folder fails with "no Go files", because the
program is in `cmd/freezy-sim`.

**Front-end work needs no rebuild.** The exe embeds `web/`, but `-webdir` serves it from disk instead. Edit a file and
reload the window (Ctrl+R):

```bash
freezy-sim.exe -webdir web -debug
```

`-debug` turns on the WebView2 developer tools (F12). You can also open `http://127.0.0.1:8090` in Edge or Chrome and use
their tools. Rebuild before you share an exe, because the embedded copy is what users get.

## Checks

Run all three before you send a change. The release script runs them too.

```bash
gofmt -l internal cmd web
```

```bash
go vet ./...
```

```bash
go test ./...
```

`gofmt -l` must print nothing. The unit tests need no arena. They take about a minute and a half, most of it the
physics tests in `internal/world`; `go test ./internal/gui` and the like run in under a second.

`go test ./web` also draws the 3D model of every robot the simulator can build (each design `world.ModelSpecs` lists,
and the stock robots) in a headless Edge or Chrome, in about 45 seconds. It fails if a model throws, or if any part
sticks out past the frame and bumpers or above the robot's height while stowed, climbing or with a turret turned, or
if a deployed intake reaches more than 12 in past the frame. The check itself is `web/testdata/robotcheck.js`. The test
finds Edge or Chrome in the usual places, or uses the browser `FREEZY_TEST_BROWSER` names. Without either it's skipped,
except in CI, where it fails. `go test -short ./...` skips it.

Everything that reads bytes from outside (the FMS packets, Modbus requests, sACN datagrams, recording files) has a Go
fuzz test. `go test ./...` runs their seed inputs. If you change one of those parsers, fuzz it for a while too, one
target at a time:

```bash
go test ./internal/ds -run '^$' -fuzz '^FuzzDecode$' -fuzztime 60s
```

The targets are `FuzzDecode` and `FuzzTcpPacket` (`internal/ds`), `FuzzModbusHandle` (`internal/fieldio`),
`FuzzDecode` (`internal/sacn`), and `FuzzRecording` and `FuzzChunk` (`internal/recorder`). A crash is saved under the
package's `testdata/fuzz/`; keep that file, since it then runs on every `go test` as a regression test.

Then test against a real arena, matching what you changed:

| You changed | Run |
|---|---|
| A protocol, an emulator, the arena client, match flow | `freezy-sim.exe -suite acceptance`, or the tests that cover it with `-tests` |
| Physics, robots or the AI | `freezy-sim.exe -suite ai -bench-matches 40 -bench-seed 21`, and compare the report with the saved baseline. Then play a match or two in the GUI and watch |
| Faults, recovery, or the director | A short soak: `freezy-sim.exe -soak 3 -soak-chaos 3` |
| The 3D view or panels | Look at it, in the GUI, on Low and High graphics. For robot models, `go test ./web` too |

For a rough front-end syntax check without Node.js, `tools/jscheck.py` (Python 3) checks bracket, string and
template-literal balance, and unfinished imports, in the files you name:

```bash
python tools/jscheck.py web/js/panels/arena.js web/js/scene/cadfield.js
```

A broken module usually shows up straight away as a blank panel and an error in the F12 console.

## Common changes

**Add a GUI panel.** Create `web/js/panels/<name>.js` that calls `registerPanel({ id, title, mount })`, and import it
in `web/js/main.js`. The import order is the tab order. Use `h()` from `core/dom.js` to build elements, and `on(topic,
fn)` and `cmd(name, args)` from `core/store.js` to read state and act.

**Add a command.** In the subsystem that owns the state, call `hub.Handle("area.verb", func(args []byte) (any,
error) {...})`. The GUI, the HTTP API (`POST /api/cmd/area.verb`) and JSON scenarios (`{"do": "cmd"}`) can all call
it then.

**Publish state.** Call `hub.Publish("area.name", value)`. It is sent to every GUI client and recorded. **Never publish
a secret**, such as a password or a token, as a topic or a log event: both go into recordings and bug bundles. Return
it from a command instead. Messages from the arena are passed through `redact.JSON` before they are published, which
blanks any field whose name looks like a secret (`WpaKey`, `*Password`, `*Token`, `*Secret`, `ApiKey`); use it for
any other outside JSON you publish.

**Start a goroutine.** A panic in any goroutine ends the whole program (and the arena with it), so don't use a bare
`go`. Use `safe.Go(hub, "area", fn)` for a loop that must keep running: it is restarted a second after a panic. Use
`safe.Async(hub, "area", fn)` for one-shot work that must not run twice. For a goroutine you start yourself, put
`defer safe.Recover(hub, "area")` first. All three log the panic and write its stack to `run/logs/`, at most once
every 30 s per area. Command handlers and bus listeners are already protected.

**Save a file.** Use `safe.WriteFile` (or `safe.WriteFileFunc` to stream) for anything the sim reads back, such as
state, settings and recordings. It writes a temporary file and renames it over the old one, so a crash or power cut
never leaves a half-written file. It also retries when Windows (antivirus, the indexer, OneDrive) briefly holds the
target open.

**Put it in the GUI.** Give the panel a group in `GROUPS` in `web/js/ui/dock.js` (Match, Field, Simulation or
System). For icons, use `icon(name)` from `core/icons.js`, never emoji or symbol characters, which look different on
every PC; add a Lucide path there if one is missing. For questions, use `ask()` and `confirmAction()` from
`core/dom.js`, never the browser's `prompt()` or `confirm()`, and pass `danger: true` for anything that deletes or
can't be undone. `cmdButton(label, command, args, cls, { title, confirm })` does both for a command button.

Every button gets a `title` that says what a click does. An icon-only button also gets an `aria-label`. A toggle shows
the current state in its label ("Door closed"), sets `aria-pressed`, and says in its `title` what a click will change.
Disable a button when it can't work (the arena is offline, the wrong match state), rather than letting the click fail.
Catch every `cmd()` and `toast()` the error, so a failed click never looks like nothing happened. Something that needs
the 3D view (`window.venue`) must say so when it runs in a popped-out panel, which has no 3D view.

One `primary` (or `go`) button per card at most; the rest stay plain. Put controls under a card's
`h3`, not in it, unless it is a single small control. A table with more than five columns gets the `wide` class, so it
scrolls sideways in a narrow dock instead of squeezing its cells.

**Write an error message.** Write it for someone who skimmed the README: say what is wrong and what to do next, and
show paths as they are typed (`%s` in quotes, not `%q`, which doubles every backslash). If the user can fix it in the
GUI, say where.

**Add an acceptance test.** If it can be written as a timeline, write it in JSON in `scenarios/`
([SCENARIOS.md](SCENARIOS.md)). For anything that needs Go, add it to `internal/scenario/suite_more.go`, following the
tests there.

**Change the snapshot format.** Change `encodeSnapshot` in `internal/world/snapshot.go` and the decoder in
`web/js/scene/worldstream.js` together, and update the format comment at the top of `snapshot.go`. Recordings store
these frames, so keep old frames readable, or say in the changelog that old recordings won't open.

**Keep the twins in sync.** A few things exist in both Go and JS and must match:

- hopper packing: `hopperGeometry` in `internal/world/fuel.go` and `hopperShape` in `web/js/scene/robots.js`
- the field layout: `web/layouts/field-2026.json`, read by both
- the snapshot format, above

**Regenerate the field CAD mesh.** See [FIELD_MODEL.md](FIELD_MODEL.md). The mesh is never committed or embedded.

**Upgrade three.js.** Replace the files in `web/vendor/three/` with the same set from the new release, check that the
venue still renders (the post-processing addons change most often), and update the version in
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## Style

- Go: `gofmt`, standard library first, and few dependencies. A new dependency needs a good reason, and its license
  goes into `THIRD_PARTY_NOTICES.md`.
- The front end has no build step, no npm and no framework. Plain ES modules and three.js only.
- Comments say *why*. Match the density and tone of the code around them.
- User-facing messages say what is wrong and what to do about it.
- Files are UTF-8 without a BOM. On Windows, beware of PowerShell's `Set-Content` and `Out-File`: their defaults can
  add a BOM or change the encoding.

## Releasing

See [RELEASING.md](RELEASING.md).
