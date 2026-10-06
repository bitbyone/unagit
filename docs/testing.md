# Testing unagit

`make test` runs every package with the race detector. `make test-fast` runs
the same tests without it while iterating. A finished change still needs the
race run, `go vet ./...`, a build and clean `gofmt` output before committing.

## What takes time

The UI suite at c7704eb had 284 top-level tests. Most started a real tview event
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
spinners run while work is under way. Data polling, the theme watcher and input
debounce keep their real clocks. Ordinary fixtures control only job and task
spinner ticks; the tests of animation still exercise their frames. Production
uses the same real tickers and intervals as before.

## First pass measurements

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

The first pass's whole race test phase was about 37% shorter, or 1.58 times as
fast.
Earlier successful optimized full runs took 123.1 and 123.6 s in UI; the table
uses the final verification after fixing the remaining timing assumptions.
A standalone UI run with `-parallel 8` took 126.1 s, within the range of the
four-test runs, so four remains the default. These are samples on this machine,
not a timing requirement for CI.

An uncached full-package run without race also passed; its UI package took
40.4 s. This is the mode offered by `make test-fast`.

All original test cases remain. The first pass added two regression tests for
ordered Unicode key delivery and independent Git fixture histories; the second
adds two for matching tcell's rendered cells and preserving graphemes while
dimming. Theme and Settings checks still walk every built-in theme; focusing a
Settings field now explicitly draws it before checking its legibility.

## Second pass measurements

The final full run used the same uncached race command and four parallel tests
as the first pass. A separate paired comparison ran only UI, sequentially:
the preserved race test binary from 747d3b7, without CPU profiling, took
124.0 s; the new suite took 78.2 s with the same test settings. It includes
the two additional regression tests.

| Test phase | First pass | Second pass |
| --- | ---: | ---: |
| Every package, race | 130.9 s | 87.8 s |
| UI package in the full run, race | 130.8 s | 85.5 s |
| Standalone UI, paired race runs | 124.0 s | 78.2 s |
| Every theme is legible, full race run | 22.46 s | 10.04 s |
| Worktree view fits its frame, full race run | 4.59 s | 0.35 s |

The whole race test phase is another 33% shorter; the paired UI runs are 37%
shorter. From the original 206.9 s, the whole test phase is about 58% shorter.
It remains above 60 s. The final race run passed all 571 top-level tests,
including all 288 in UI. Animation, resizing, Unicode rendering and Git
fixture isolation checks also passed five consecutive race runs.

The final uncached run of every package without race passed in 34.6 s of test
phase; UI took 33.7 s. This is the same suite used by `make test-fast`.

A trial limiting Go to four processors gave 137.1 s against 140.9 s at an
intermediate stage, which was not a convincing improvement. Neither the
processor default nor the four-test parallel limit has changed.

## How the fixtures work

- `observedScreen` embeds the real tcell simulation screen. `waitFor` and
  `waitGone` subscribe to the next rendered frame; text is converted once per
  inspected frame. Reads and frame bookkeeping stay on the event loop. Its
  older `SetContent` API reuses strings for single-byte characters through
  tcell's `Put`; the same real buffer, grapheme handling and locks are used.
- Production modal dimming uses tcell's grapheme strings directly instead of
  converting them to runes and back. The colors and attributes stay the same;
  a regression test covers combining marks, wide characters and emoji joiners.
- Workflow fixtures start at 120 by 34, about 42% fewer cells to redraw than
  before. Tests reading long rows or deep detail, and every theme walk, choose
  the original 160 by 44. Layout tests still cover their original sizes.
  `resizeApp` explicitly draws the new size before reading its cells; a word
  already present on the previous frame cannot acknowledge a resize.
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
  files are independent; the shared seed lasts until `TestMain` exits. An empty
  Git template keeps unused sample hooks out of the copies.
- The commit form layout test supplies the rows and counts directly to the
  form. Commit integration tests still create and commit real groups. The
  worktree layout and theme checks give the production view prepared rows,
  remote states and commit facts. Workflow tests still create real groups and
  check that the view waits for every repository's Git facts.
- Each theme builds fresh widgets once, then uses the same app for its dialog,
  Settings and worktree walks. The preceding dialogs are closed with Esc. Every
  member block is brought into view and checked, including its commit text.
- Unlock layout fixtures copy a valid empty encrypted vault made once with the
  usual Argon2id parameters. Unlocking, wrong-passphrase, rekey and keychain
  tests still create and open real vaults.
- Job and task animations use a private ticker factory. Ordinary fixtures
  provide a channel they close at cleanup, and do not send decorative ticks.
  The background-refresh test uses a real ticker; the task-log test sends a
  tick and checks the changed frame and completion. Progress, job completion,
  API polling and debounce still run normally.
- Hunk tests supply an executable lookup to their own app, then execute the
  real test script at that path. They run in parallel without changing `PATH`.
  Production still uses `exec.LookPath`, including Settings' integration checks.
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
  -run '^(TestGroupingKeepsTheCursorInView|TestAThemeOfEachKindIsLegible)$' \
  -cpuprofile /tmp/unagit-ui.cpu -o /tmp/unagit-ui.test ./internal/ui
go tool pprof -top /tmp/unagit-ui.test /tmp/unagit-ui.cpu
```

Run comparisons one at a time. Starting another suite or profile in parallel
changes the CPU, process and race-runtime contention being measured. Keep
`-count=1`, the package selection, race setting and parallel limit the same
on both versions; a cached `ok` line is not a performance measurement.
