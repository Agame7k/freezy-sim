# Contributing to Freezy Sim

Thanks for helping. Bug reports, new test scenarios, fixes and documentation are all welcome. This project follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting a bug

1. Check the [troubleshooting guide](docs/TROUBLESHOOTING.md) and the existing issues.
2. Save a **bug-report bundle**: the 🐞 button in the top bar, or the `run/reports/bundle-*.zip` a failed headless run
   left behind. It holds a replay of what happened, every live state value, the logs, the arena's database and your
   config, with passwords and tokens removed ([what's inside](docs/RECORDINGS.md#bug-report-bundles)).
3. Open an issue with the bug report form, and attach the bundle.

Is it the sim or the arena? If the arena did something wrong (scored FUEL in the wrong shift, didn't disable a robot),
report it to [Freezy Arena](https://github.com/Freezy-Arena/freezy-arena/issues) or
[Cheesy Arena](https://github.com/Team254/cheesy-arena/issues), with the sim's report or bundle as evidence. If the sim
did something wrong (a robot drove through a wall, a test failed against a correct arena), report it here.

**Security problems:** please don't open an issue. See [SECURITY.md](SECURITY.md).

## Suggesting a feature

Open an issue with the feature request form. Say what you are trying to test or see, not only the feature: there may
already be a way to do it.

## Contributing a test scenario

New acceptance tests are among the most useful contributions, and they need no Go. Write the scenario in JSON
([docs/SCENARIOS.md](docs/SCENARIOS.md)), check that it passes against a correct arena and fails when the behavior is
broken, and send it as a pull request in `scenarios/`.

## Changing the code

1. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).
2. For anything bigger than a small fix, open an issue first to agree on the approach.
3. Keep changes focused: one fix or feature per pull request.
4. Before you send it, run:

   ```bash
   gofmt -l internal cmd web
   ```

   ```bash
   go vet ./...
   ```

   ```bash
   go test ./...
   ```

   Also run the acceptance tests, the AI benchmark or a short soak that cover what you changed (the table in
   DEVELOPMENT.md says which).
5. Update the docs and `CHANGELOG.md` (under "Unreleased") if users will notice the change.

Some ground rules for the code:

- **Over the wire only.** Never import, patch or link arena code. The sim must talk to the arena the way real hardware
  does.
- **The arena folder is read-only.** Nothing may write to it.
- **No secrets in topics or the event log.** Both are recorded into bundles that people share publicly.
- **No new dependencies without a reason.** The front end has no build step; keep it that way. A new Go dependency
  needs a permissive license, which goes into `THIRD_PARTY_NOTICES.md`.
- **Nothing from FIRST's CAD in the repository.** The field model is converted by each user
  ([docs/FIELD_MODEL.md](docs/FIELD_MODEL.md)).

## License

By contributing, you agree that your contribution is licensed under the project's [MIT License](LICENSE). Only
contribute work you have the right to share. Don't include content copied from FIRST's game manual or CAD, or code from
projects under licenses that don't allow it. Cheesy Arena's license, for example, doesn't allow redistributing its code
outside contributions to that project.
