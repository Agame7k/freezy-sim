# Releasing

A checklist for publishing a version. Pushing a tag like `v0.2.0` runs the **Release** workflow
(`.github/workflows/release.yml`), which builds the zip and publishes the GitHub release. The version number goes into
the exe at build time (`release.cmd <version>`); the source default in `internal/version/version.go` is the next
version with `-dev` after it.

## 1. Before you build

- [ ] `CHANGELOG.md`: everything in this release is listed under `## [Unreleased]`. You don't need to rename it:
  when the Release workflow finds no `## [0.2.0]` section, it moves the Unreleased notes under
  `## [0.2.0] - <today>`, uses them as the release notes, and commits the changelog back to `main` (see
  [Publish](#6-publish)). To do it yourself instead, for example to pick the date:

  ```bash
  bash tools/changelog.sh promote 0.2.0
  ```

  `bash tools/changelog.sh notes 0.2.0` prints what the release notes will be.
- [ ] Docs: check that the README and `docs/` match what the release does (new flags, config keys, tests).
- [ ] `git status` shows only what you mean to publish. These must **never** be committed, and `.gitignore` covers them:
  - `freezy-sim.json` (local paths, maybe passwords)
  - `run/` (the sandbox arena, logs, reports, recordings and the imported field model)
  - `EBWebView/` (a browser profile with cache and cookies)
  - `.claude/settings.local.json`
  - `*.exe`, `dist/`
  - `field-cad.glb`, `*.step` and `*.FCStd` (FIRST's CAD; see the licensing checks below)
- [ ] Check what git would add, and how big it is:

  ```bash
  git status --short
  ```

  ```bash
  git ls-files --others --exclude-standard
  ```

  For 0.1.0 that was about 270 files and 16 MB. The largest are the two GIFs in `docs/images/` (about 4 MB each),
  `web/models/human.fbx` (2.4 MB) and three.js.

## 2. Licensing checks

- [ ] No FIRST CAD in the tree. This should print nothing:

  ```bash
  git ls-files "*.glb" "*.step" "*.stp" "*.FCStd"
  ```

- [ ] `THIRD_PARTY_NOTICES.md` lists every module in the build. Compare it with:

  ```bash
  go version -m dist/freezy-sim-v0.1.0-windows-amd64/freezy-sim.exe
  ```

  A new dependency needs its license text added. Test-only dependencies don't ship in the exe.
- [ ] If three.js or the people model changed, update their rows and license texts.

## 3. Test

- [ ] `gofmt -l internal cmd web` prints nothing; `go vet ./...` and `go test ./...` pass. `release.cmd` checks these
  too.
- [ ] The acceptance suite passes against the current Freezy Arena, with both field profiles:

  ```bash
  freezy-sim.exe -suite acceptance
  ```

  Then set `"field": { "profile": "plc" }` and run it again.
- [ ] A short soak is clean: `freezy-sim.exe -soak 3 -soak-chaos 3`.
- [ ] If physics or AI changed: `freezy-sim.exe -suite ai -bench-matches 40 -bench-seed 21`, compared with the baseline.
- [ ] If possible, the suite against upstream Cheesy Arena (`plc` profile). Note the result in the release notes.

## 4. Build a local copy to smoke-test

The Release workflow builds the published zip, but build the same thing locally first so you can test it before
tagging:

```bash
release.cmd 0.1.0
```

This checks formatting, vet and tests, then builds a trimmed, stamped exe. It writes
`dist/freezy-sim-v0.1.0-windows-amd64.zip` and a `.sha256` file beside it. The zip holds one folder,
`freezy-sim-v0.1.0-windows-amd64/`, containing:

- `freezy-sim.exe`
- `README.md`, `LICENSE.txt`, `THIRD_PARTY_NOTICES.md`, `CHANGELOG.md`
- `config.example.json`, `scenarios/`, `docs/`
- `tools/cad/export_field.py`

## 5. Smoke-test the zip

- [ ] Unzip it into a new, empty folder outside the repository.
- [ ] `freezy-sim.exe -version` prints the right version.
- [ ] Run it with no config: the log says to create `freezy-sim.json`.
- [ ] Add a config pointing at an arena, run it, and play one test match.
- [ ] Graphics button → Field model shows "The official CAD isn't included…", and importing a `.glb` works.

## 6. Publish

1. Commit and push the branch, and wait for the "Tests" workflow to pass.
2. Tag the release with a lowercase `v` and push the tag:

   ```bash
   git tag v0.2.0
   ```

   ```bash
   git push origin v0.2.0
   ```

3. The **Release** workflow (Actions tab) then:
   - checks the tag looks like `v1.2.3` or `v1.2.3-rc.1`
   - takes the `## [1.2.3]` section of `CHANGELOG.md` as the release notes. If there is none, it moves the
     `## [Unreleased]` notes under `## [1.2.3] - <today>` and uses those. It fails only if both are missing or empty
   - runs `release.cmd` (format, vet, tests, build, zip, checksum)
   - creates the GitHub release titled with the tag, and attaches the zip and the `.sha256` file
   - if it moved the Unreleased notes, commits `CHANGELOG.md` to `main` as "CHANGELOG: release 1.2.3" (by
     github-actions). It does this only if `main` is still at the tagged commit; if `main` has moved on, its
     Unreleased section may already hold notes for the next version, so it leaves a warning on the run instead, and
     you move the notes by hand. Pull afterwards to get the commit.

   Every 0.x version, and any tag with a suffix (`v1.2.0-rc.1`), is published as a pre-release; from 1.0 a plain
   version is a normal release.
4. If the workflow fails, fix the problem, push the fix, then move the tag to it and push the tag again. (Re-running
   the failed run doesn't help: a tag's run uses the workflow and files at the tagged commit.)

   ```bash
   git tag -f v0.2.0
   ```

   ```bash
   git push -f origin v0.2.0
   ```

   If it got as far as creating the release, delete that release on GitHub first.
5. Open the release on GitHub and check the notes and the two files.
6. First release only, in the repository settings:
   - Description: "A simulated 2026 FRC field for testing Freezy Arena / Cheesy Arena without field hardware"
   - Topics: `frc`, `first-robotics`, `fms`, `cheesy-arena`, `simulator`, `testing`
   - Turn on Issues, and **Private vulnerability reporting** (Security → Advisories), which SECURITY.md points to
   - Check that the "Tests" workflow ran on the push and passed

## 7. After

- [ ] Set `internal/version/version.go` to the next version with `-dev`, for example `0.2.0-dev`.
- [ ] `git pull` to get the release's changelog commit, which already starts a new, empty "Unreleased" section.
