# Watched

Built, as below; [Where it stands](#where-it-stands) at the end says where
the code went another way and what is still to do. Before it, a running
pipeline was followed only while it was in front of the user: the lists ask again about the pipelines they show as
running (`cipoll.go`), and an open jobs dialog follows its own
(`followPipeline`). Nothing says "it failed" once the user has looked away,
and nothing follows a pipeline a filter hides. The aim: the user *watches*
something - a merge request, a branch of a repository, a worktree - and
unagit follows it in the background, lists it on a screen of its own, and
says when it changes, with a desktop notification when the terminal is not
in front.

Pipelines are the first thing watched. The screen and the machinery are
general, so that other kinds - a merge request's activity, reviews asked of
the user - become new sections later rather than new screens.

## Decisions

- **One screen, `[5] Watched`, in sections**, after Agents; Settings moves
  to `[6]`. The first and for now only section is Pipelines.
- **"Watch Pipelines" is an action without a key.** It is in the action
  pickers (`Alt-Enter`) of a repository, a merge request, a worktree, a
  repository's block in the worktree view, and of the jobs dialog; on
  something watched it is "Stop Watching Pipelines". `keys: ""`.
- **A watch is on a thing, not on one pipeline.** Watching a merge request
  follows the pipeline of whatever its head is: a push starts a new pipeline
  and the watch moves to it. Watching a branch follows its newest pipeline.
  A worktree is watched as its branch; a grouped worktree asks which
  repository, as `J` does.
- **A watch lasts until it is stopped**, or until what it watches is gone:
  its merge request merged or closed, its branch deleted on origin.
- **The watches are state, not configuration.** `config.yaml` is what one
  keeps in chezmoi and carries between machines; what one happens to be
  waiting for on this machine does not belong there. They go to a file of
  their own (below), and the next start carries on with them.
- **Only while a unagit runs.** Following a pipeline needs the token, and the
  token is only ever in the memory of a running, unlocked unagit (AGENTS.md:
  no cache daemon). There is no background service; the README says so.
- **Several unagits, one poller.** Each instance a user starts would
  otherwise ask the servers about every watch - twice the requests, and
  every notification twice. One instance follows the watches and the others
  read what it found (below).
- **A background change is a toast, not a box.** AGENTS.md says a
  warning comes up in a box that holds the keys until Esc. A failure the user
  is not looking at must not take the keyboard from whatever they are typing
  into, so a watch's news is a toast in the top right corner, coloured by
  its level, that takes no key and goes by itself; the tab's title gains a count of unseen changes
  (`[5] Watched ●2`), and the row stays marked until the screen has been
  opened. This is a deliberate exception, and this is where it is written
  down.

## Where the watches live

```
<config dir>/watch/watches.json    what is watched - written by any instance
<config dir>/watch/state.json      what was last seen - written by the poller
<config dir>/watch/poller.lock     held by the instance that polls
<config dir>/watch/present/<pid>   each running instance: is its terminal focused
```

Under the configuration directory because everything unagit reads goes
through `cfg.Dir()`, which is what keeps tests away from the user's files
(AGENTS.md). chezmoi manages only the files it is told to, so
`config.yaml` can be in it while `watch/` is not. Moving every
machine-local file - the indexes, the sessions, the watches - to
`$XDG_STATE_HOME` is a separate change, for all of them at once.

```go
// internal/watch
type Watch struct {
	Kind     string    `json:"kind"` // "pipeline" for now
	Instance string    `json:"instance"`
	Project  string    `json:"project"`
	IID      int       `json:"iid,omitempty"`    // a merge request's
	Branch   string    `json:"branch,omitempty"` // a branch's
	Since    time.Time `json:"since"`
}
```

`state.json` holds, per watch, the pipeline id, its status, the head, the
first failed job, when it last changed and whether the change has been seen;
and a sequence number that goes up with every write, so a reader can tell
what it has not shown yet.

Every file is written whole to a temporary file and renamed over the old
one, so a reader never sees half of it. `watches.json` is changed under a
short `flock` of its own (read, change, write, unlock), since any instance
may add or remove a watch at the same moment as another.

## One poller among several instances

- **Who polls.** After the passphrase - an instance still waiting at the
  dialog has no token - each instance starts a goroutine that takes an
  exclusive `flock` on `poller.lock`, blocking. One gets it and polls; the
  others wait in that call. When the poller exits, or dies, the kernel
  releases the lock and one of the waiting instances gets it and carries on
  from `state.json`. No election, no heartbeat, nothing to clean up after a
  crash.
- **How the others learn.** They look at `state.json`'s modification time
  every second - one `stat`, no new dependency (there is no file watching in
  the code today) - and read it when it changed. Events are the difference
  between the sequence numbers they last showed and the new one, so every
  instance shows each change once, as its own toast.
- **New watches.** The poller looks at `watches.json` the same way and asks
  about a new watch at once, rather than at its next turn.
- **Seen.** Opening the Watched screen in any instance marks what it shows
  as seen in `state.json` - through the poller, which alone writes that
  file: the instance writes the sequence number it has seen into its
  `present/<pid>` file, and the poller folds it in.
- **Notifications are sent once, by the poller**, unless any instance says
  in its `present/<pid>` file that its terminal has focus and it is not
  suspended for an editor - then the user is looking at a unagit and the
  toast is enough. A `present` file whose pid is gone is ignored and
  removed, as a stale session is.

`unagit go`, `cd` and `sessions` never poll; only the TUI does.

## Asking the forges

No forge pushes pipeline changes to a client like this one:

- GitLab's own web UI gets some updates over ActionCable and GraphQL
  subscriptions, but that is undocumented and built for the browser's
  session, not for a token. Not relied on.
- GitHub sends webhooks, which need an address on the internet that
  GitHub can reach. Not for a program on a laptop.

So it is polling, made cheap:

- a watch whose pipeline runs is asked every 15 s (`ciAskEvery`); one that
  waits, every 20 s, to notice a push that starts a new pipeline;
- one request per watch and turn - `MergeRequestPipeline` or
  `LatestPipeline`, no jobs. The jobs are read only when a pipeline has just
  failed, to name the first failed job, and when the user opens it;
- on GitHub with `If-None-Match`: an unchanged answer is `304`, which does
  not count against the rate limit, so a hundred idle watches cost nothing;
- later, **one GraphQL request per server and turn** for all its watches
  (aliases in one query: GitLab's `project { mergeRequest { headPipeline {
  status } } }`, GitHub's `pullRequest { commits(last: 1) { ... 
  statusCheckRollup { state } } }`). That is a `forge.Provider` method,
  `PipelineStates(ctx, []PipelineRef)`, with a REST fallback in each
  provider - nothing above `forge` learns that GraphQL exists. Not in the
  first change: REST is enough until there are dozens of watches;
- requests run with the bounded fan-out the refresh uses, and a server that
  fails or says it is rate limited is backed off by its headers;
- the periodic reads are not jobs with a spinner - the status line would
  never be still. The Watched header says when the watches were last read;
  `r` / `R` on the screen read now, as a job;
- when the lists' own following (`watchCI`) reads a pipeline that is also
  watched, the poller takes that reading instead of asking again. Only in
  the polling instance; the others' lists read as they do today.

Events for a pipeline: started, succeeded, failed (with the first failed
job), cancelled, waiting for a manual job, and for a merge request a new
head with a new pipeline. The first reading after a start is never news -
only a change from what `state.json` last held.

Each kind of watch is one implementation of

```go
type watchKind interface {
	read(ctx context.Context, client forge.Provider, w Watch) (State, error)
	changes(before, after State) []Event // nothing on the first reading
	running(s State) bool               // ask again soon
}
```

so a new section is a kind, its row and its actions, and the poller is
shared.

## Notifications

`internal/notify`, given a title, a line and whether the terminal is free:

- **In the terminal.** OSC 777 (`ESC ] 777 ; notify ; title ; body BEL`) for
  Ghostty, WezTerm and foot; OSC 9 for iTerm2; OSC 99 for kitty; chosen by
  `TERM_PROGRAM` / `TERM`, the way `nerdfont.go` guesses its terminal.
  Inside tmux the sequence is wrapped for passthrough (`ESC P tmux; ... ESC
  \`), which needs `allow-passthrough on`; the card's `found` line says
  whether tmux has it.
- **From the system** when the terminal cannot, or while an editor has the
  terminal: an escape sequence written then would land in the middle of
  Neovim's output. On macOS `osascript -e 'display notification ...'`, on
  Linux `notify-send`.
- Settings › Integrations › Notifications: automatic (as above), terminal
  only, system only, or off; and a key that sends a test.

A notification says what and what happened - "api-gateway !42 · pipeline
failed · test:unit" - and nothing more.

## The screen

A `pane` like the other lists, with a header per section (the grouping the
lists already have). A row of Pipelines:

```
  WHAT                       CI   PIPELINE     BY      CHANGED
● acme/api-gateway !42       ✗    failed 4m    jane    2m ago
  acme/billing · main        ◐    running 1m   ci      now
  acme/web !17               ✓    passed 12m   john    1h ago
```

`●` marks a change not seen yet. The columns go through `layoutColumns` like
every list's. Its actions, as `uiAction`s:

- `Enter` - the pipeline, in the jobs dialog that exists (`showPipeline`
  with the watch's `ciTarget`);
- `x` - stop watching; space marks rows, and `x` then stops every
  marked one (see [markers.md](markers.md));
- `w` - the pipeline in the browser; `m` - go to the merge request or the
  repository in its list;
- `r` / `R` - read one / every watch now.

The header also says which instance polls, when it is another one ("followed
by unagit 41231"), so a user with several open knows why `r` here asks
that one rather than the server.

A watched row in the other lists wears the coloured "watched" mark of
[markers.md](markers.md).

## Later sections

Each a `watchKind`, its row and its actions:

- **Merge request activity** - new commits, new threads or replies,
  approvals, merged or closed.
- **Asked to review** - a watch on a query rather than a thing: a merge
  request where the user becomes a reviewer.
- **A branch behind its base** - read from the disk after a fetch, no
  request.
- **Releases or tags** of a repository.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/watch` (new) | `Watch`, `State`, `Event`; the files under `watch/`, written by rename, `watches.json` under its own lock; the poller lock; the `present` files. No UI, no forge calls. |
| `internal/config` | `WatchDir()` beside `IndexPath`, from `Dir()`. |
| `internal/notify` (new) | the escape sequences, the tmux wrapping, the system notifiers, the choice between them. |
| `internal/forge`, providers | `If-None-Match` on GitHub; later `PipelineStates`. |
| `internal/ui/watch.go` (new) | the kinds, the poller, following `state.json` in the others; folds in what `cipoll.go` can share. |
| `internal/ui/watched.go` (new) | the screen, its columns, its actions. |
| `internal/ui/tabs.go`, `app.go` | `[4] Watched`, Settings on `[5]`, the unseen count in the title. |
| `internal/ui/actions_lists.go`, `wtmodal.go`, `pipeline.go` | "Watch Pipelines" in the four places and the jobs dialog. |
| `internal/ui/integrations.go` | the Notifications card. |
| `internal/ui/screen.go` | focus and suspension, for the `present` file. |
| themes | the unseen mark; the watched mark is markers.md's. |
| README, `help.go` | the screen, the tab number, the screen's keys. |

## Tests

- `internal/watch`: two goroutines with their own handles on one lock file -
  one polls, the other takes over when the first releases; `watches.json`
  changed by two writers at once loses neither change; a reader never sees
  half a file.
- Two Apps on one configuration directory against the fake API server, with
  a short `ciAskEvery`: only one asks the server; a pipeline going running →
  failed is shown once in each and notified once; stopping the polling App
  makes the other poll and carry on without a second notification of the
  same change.
- A first reading gives no event; a merged merge request drops its watch; a
  new head moves the watch to its pipeline.
- `internal/notify`: the exact bytes for each terminal, the tmux wrapping;
  a fake `osascript` / `notify-send` on `PATH` (serial) while no instance
  has focus or one is suspended; nothing while one has focus.
- A background failure never opens a message box and never takes a key.
- The screen: a layout test at several sizes, `assertLegible`, the help
  rows, and the tests that move with the tab numbers.

## Open details

- The second-long `stat` loop costs nothing measurable, but a test that
  waits on it waits up to a second; the interval is a field of the App like
  `ciAskEvery`, short in tests.
- Twenty running pipelines at 15 s are under 5,000 requests an hour -
  GitHub's limit for a token - but the lists' own refresh shares it.
  GraphQL batching is what removes the ceiling.

## Where it stands

Built in `internal/watch` (the files), `internal/notify` (the sequences and
the system notifiers), `internal/forge/transport.go` (the rate limit, the
ETags), `internal/ui/watch.go` (the follower, the poller),
`internal/ui/watchread.go` (the reads and the events) and
`internal/ui/watched.go` (the screen). Where it went
another way than above:

- **The poller's lock is tried, not waited for.** Each instance tries
  `poller.lock` with `LOCK_NB` at every look (a second); a blocked `flock`
  cannot be called off when unagit exits, and a look a second takes over
  within a second all the same. `Run` waits for the follower to let the
  lock and its presence file go.
- **One GraphQL query a server and turn decides what is read.** It is not
  `PipelineStates` handing over the pipelines: GitHub's pipeline is what its
  check runs add up to over several REST calls, which GraphQL's rollup does
  not match one for one. So `forge.Fingerprints` asks every watch due at
  once for a fingerprint - a merge request's state, head, comments,
  approvals and head pipeline, a branch's newest pipeline (on GitHub its
  head and checks, null once it is gone) - and only a watch whose
  fingerprint moved is read in full over REST, as before; every one is read
  in full at least every five minutes all the same, and a server whose
  GraphQL fails is read in full only for ten. Asked for by hand (`r`, `R`),
  a watch is read in full.
- **A full read of a merge request** is its detail - merged, closed,
  pushed, its comments - its approvals, its pipeline, and its commits
  counted when the head moved. The jobs are read only when a pipeline has
  newly failed. A branch's is whether it is still on the server, and its
  newest pipeline.
- **Every client's requests go through `forge.Transport`.** It reads the
  rate limit off every answer (GitLab's `RateLimit-*`, GitHub's
  `X-RateLimit-*`); refused (429, or 403 with nothing left) or down to its
  last tenth or fifty, the server is not asked by the watches until
  `Retry-After` or the reset - what the user asks for still goes. The row
  says so. On GitHub it asks again with `If-None-Match`, and a 304 hands
  back the body kept from before.
- **A merge request's activity is news**: new commits (and a head
  rewritten), new comments, an approval given or withdrawn. A watched
  branch that is on disk says how far it is behind its base - the branch
  unagit made it from, or the default branch - by the refs the clone has,
  no request; falling further behind is news. A branch deleted on origin
  ends its watch, as a merge request merged or closed does. The screen's
  LATEST column is what was last said of each.
- **Every change is news**, with a level: a pipeline that began (info),
  passed (success), failed (danger), was cancelled or waits for a manual
  job (warning), a merge request merged or closed. Each is counted on the
  tab, shown as a toast (`toast.go`) in every instance, and notified on the
  desktop when none is in front. An earlier version said only endings,
  on the status line; a start went unnoticed and so did the ending, under
  whatever else the status line said.
- **The lists follow the watches.** `shareWatchStates` puts each watch's
  status into the merge request list and the branches' CI column, and
  `watchCI` turns the marks - on the Watched screen and the tab too - while
  a watched pipeline runs. `followCI` no longer asks about what a watch
  follows; a list read that disagrees with a watch has it read now
  (`watchHeard`).
- **Focus that is never reported** - a terminal without focus events, a
  multiplexer that keeps them - is told from the application in front
  (`lsappinfo` on macOS, every 3 s at most). Zellij and herdr pass no
  notification sequence on, so the system's notifier stands in.
- **The system's notifier by default.** A terminal's sequence is written
  and never answered: Ghostty and iTerm2 drop it for a window in front, and
  without leave to notify it is dropped unseen - a pipeline's start went
  missing so, while its end came. Automatic goes through the system
  wherever it has a notifier, the terminal only when chosen or where it has
  none. What became of each notification is noted in `watch/notify.log`.
  A toast and a notification carry the start of the merge request's title.
- **The rows keep their mark for the visit.** Opening the screen counts the
  changes as seen on the tab at once, and the rows changed since the last
  visit keep their `●` while it is open, so it can be told which they were.
- **Presence files are per instance**, named `<pid>-<n>`, so two Apps in one
  test process are told apart as two processes are.
- **A reading that fails** keeps the last state, says why in the row, and is
  tried again at the idle interval.

Still to do:

- The other sections of "Later sections": a watch on a query (asked to
  review), releases and tags.
- A dashboard: the activity and the pipelines of what is watched read
  better than as one row each. Chosen (2026-10-09): tiles in columns by
  what each wants - FAILED, RUNNING, WAITING (a manual job, a review),
  DONE - each tile the thing, its title, its pipeline and its latest news,
  mixed with a detail of the tile chosen (who, the stages, the activity as
  a timeline). The layout is still to be designed with the user; the
  timeline wants more than the 50 newest events state.json keeps for all.
