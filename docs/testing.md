# Testing unagit

`make test` runs every package with the race detector. `make test-fast` runs
the same tests without it while iterating. A finished change still needs the
race run, `go vet ./...`, a build and clean `gofmt` output before committing.

## What takes time

The UI suite at c7704eb had 284 top-level tests. Most start a real tview event
loop, a 160 by 44 tcell simulation screen and a local HTTP server. Many also
exercise real Git clones, pushes and worktrees. Their cost includes rendering,
process startup and synchronization, as well as the code being asserted.

The original setup also derived an Argon2id key for every already unlocked UI
fixture: three passes over 64 MiB, with four workers. Tests with several sizes
or themes repeated that work. Keyboard helpers slept 10 ms per character and
100 ms after pressing a form button; screen assertions polled every 20 ms and
converted all the cells to text on every read. The grouping test waited for
five successive unchanged cursor readings even after delivering its keys.

A CPU profile of the original theme and grouping tests lasted 31.65 s.
Native runtime/race code accounted for 66% of its CPU samples; memory
management, Argon2id and rendering also appeared. This is not a profile of
tests hanging on a single operation. The profile does not attribute the
native samples to individual Go functions, so it cannot give an exact share
of time saved by each change.

The timers have narrower purposes: the theme watcher redraws after a custom
theme file changes, the CI watcher starts for running pipelines, and job
spinners run while work is under way. These remain active in tests. The
optimization changes the test setup and synchronization instead of changing
the application's polling or rendering behavior.

## Measurements

Measured on macOS arm64 with Go 1.27.1 on 2026-10-06. Both versions used
`go test -race -count=1 -timeout 10m -json ./...`, with the default four
parallel UI tests. Runs were sequential, with dependencies and build cache
already available. The whole-suite time below is from the first timestamped
JSON event to the final package result; it excludes build setup before those
events. Individual tests' elapsed times overlap when they run in parallel.

| Test phase | Before | After |
| --- | ---: | ---: |
| Every package, race | 206.9 s | 130.9 s |
| UI package, race | 203.6 s | 130.8 s |
| Every theme is legible | 25.32 s | 22.46 s |
| Grouping keeps the cursor visible | 13.51 s | 4.55 s |
| Worktree view fits its frame | 16.25 s | 4.59 s |
| Commit form fits its frame | 15.05 s | 0.42 s |

The final whole race test phase is about 37% shorter, or 1.58 times as fast.
Earlier successful optimized full runs took 123.1 and 123.6 s in UI; the table
uses the final verification after fixing the remaining timing assumptions.
A standalone UI run with `-parallel 8` took 126.1 s, within the range of the
four-test runs, so four remains the default. These are samples on this machine,
not a timing requirement for CI.

An uncached full-package run without race also passed; its UI package took
40.4 s. This is the mode offered by `make test-fast`.

All original test cases remain. The UI suite adds two regression tests for
ordered Unicode key delivery and independent Git fixture histories. Theme
and Settings checks still walk every built-in theme; focusing a Settings
field now explicitly draws it before checking its legibility.

## How the fixtures work

- `observedScreen` embeds the real tcell simulation screen. `waitFor` and
  `waitGone` subscribe to the next rendered frame; text is converted once per
  inspected frame. Reads and frame bookkeeping stay on the event loop.
- `typeRunes` sends its sequence through tcell's event queue and waits until
  its last key has been handled and drawn. `pressButton` does the same for
  Enter. This also fixes the old 100 ms wait returning before a slow save
  finished. Direct `InjectKey` is still asynchronous; follow it with a wait
  for the state the next operation requires.
- Ordinary UI fixtures use a private vault interface and a per-test token
  map. Saving or rekeying the fixture creates a real encrypted vault with the
  usual parameters. Unlock, passphrase and keychain tests use real vaults
  throughout. The public constructor accepts `secret.Vault` and production
  unlocking uses `secret.OpenOrCreate` with the usual derivation.
- The initial Git clone and bare origin are built once per test process.
  `os.CopyFS` copies both into each test's own directories, and the clone's
  origin is changed to its private copy. Objects, refs, indexes and working
  files are independent; the shared seed lasts until `TestMain` exits.
- The commit form layout test supplies the rows and counts directly to the
  form. Commit integration tests still create and commit real groups. The
  worktree layout test creates a real group once and draws it at every size.
- Both locked and unlocked apps use the same startup helper. Cleanup waits
  for `App.Run` to return before its configuration and fixtures are removed.
- The custom theme watcher test observes a real `os.Stat` read before saving
  its edit. The Hunk test waits for the branch's base to be loaded and the
  terminal to resume before sending its next action. These conditions
  replace timing assumptions exposed by the faster helpers.

Use pure tests for formatting and predicates, rendering tests for widget
geometry and colors, and full keyboard/Git integration tests for workflows.
A rendering test needs the production widget and renderer, but usually does
not need to repeat a workflow already exercised by an integration test for
every terminal size. Time-based behavior such as debounce still needs its
own real-time assertions.

## Repeating the analysis

Disable result caching when measuring:

```sh
go test -race -count=1 -timeout 10m -json ./... > /tmp/unagit-tests.json
go test -count=1 ./...
go test -race -count=1 -parallel 8 ./internal/ui
```

For a CPU profile of the two diagnostic scenarios:

```sh
go test -race -count=1 \
  -run '^(TestGroupingKeepsTheCursorInView|TestEveryThemeIsLegible)$' \
  -cpuprofile /tmp/unagit-ui.cpu -o /tmp/unagit-ui.test ./internal/ui
go tool pprof -top /tmp/unagit-ui.test /tmp/unagit-ui.cpu
```

Run comparisons one at a time. Starting another suite or profile in parallel
changes the CPU, process and race-runtime contention being measured. Keep
`-count=1`, the package selection, race setting and parallel limit the same
on both versions; a cached `ok` line is not a performance measurement.
