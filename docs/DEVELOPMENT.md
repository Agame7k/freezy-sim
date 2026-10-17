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

Then test against a real arena, matching what you changed:

| You changed | Run |
|---|---|
| A protocol, an emulator, the arena client, match flow | `freezy-sim.exe -suite acceptance`, or the tests that cover it with `-tests` |
| Physics, robots or the AI | `freezy-sim.exe -suite ai -bench-matches 40 -bench-seed 21`, and compare the report with the saved baseline. Then play a match or two in the GUI and watch |
| Faults, recovery, or the director | A short soak: `freezy-sim.exe -soak 3 -soak-chaos 3` |
| The 3D view or panels | Look at it, in the GUI, on Low and High graphics |

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
it from a command instead.

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
