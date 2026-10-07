# Watched

A plan, not yet built. Today a running pipeline is followed only while it is
in front of the user: the lists ask again about the pipelines they show as
running (`cipoll.go`), and an open jobs dialog follows its own
(`followPipeline`). Nothing says "it failed" once the user has looked away,
and nothing follows a pipeline a filter hides. The aim: the user *watches*
something - a merge request, a branch of a repository, a worktree - and
unagit follows it in the background for as long as it runs, lists it on a
screen of its own, and says when it changes, with a desktop notification
when the terminal is not in front.

Pipelines are the first thing watched. The screen and the machinery are
general, so that other kinds - a merge request's activity, reviews asked of
the user - become new sections later rather than new screens.

## Decisions

- **One screen, "Watched", in sections.** A tab of its own, with a section
  per kind of watch; the first and for now only section is Pipelines.
  Proposed: `[4] Watched`, Settings moving to `[5]` - Settings is the screen
  visited least, and the lists stay where muscle memory has them.
- **A watch is on a thing, not on one pipeline.** Watching a merge request
  follows the pipeline of whatever its head is: a push starts a new pipeline
  and the watch moves to it. Watching a branch follows its newest pipeline.
- **Two lengths.** *Until it ends* (the default): the watch notifies once the
  current or next pipeline finishes, then stays on the screen marked ended
  until dismissed or for a day. *Always*: it keeps following every pipeline
  until removed, or until its merge request is merged or closed, or its
  branch is gone from origin. The common case is "tell me when this is
  done", and it should not leave a list of stale watches behind.
- **A background change is a passing word, not a box.** AGENTS.md says a
  warning comes up in a box that holds the keys until Esc. A failure the user
  is not looking at must not take the keyboard from whatever they are typing
  into, so a watch's news is a `note` on the status line (`done` for a
  success), the Watched tab's title gains a count of unseen changes
  (`[4] Watched 2`), and the row stays lit until the screen has been opened.
  This is a deliberate exception and the place it is written down.
- **A desktop notification when the user is elsewhere.** unagit already
  knows whether its terminal has focus (`screen.go` enables tcell's focus
  events). With focus, the status line is enough. Without it, or while an
  editor has the terminal, a notification goes out.
- **Only while unagit runs.** Following a pipeline needs the token, and the
  token is only ever in the memory of a running unagit (AGENTS.md: no cache
  daemon). There is no background service, and the README says so. The
  watches themselves are kept, so the next start carries on.

## The model

```go
// internal/config
type Watch struct {
	Kind     string    // "pipeline" for now
	Instance string
	Project  string
	IID      int       // a merge request's, 0 for a branch
	Branch   string    // a branch's, "" for a merge request
	Always   bool      // false: until the current or next pipeline ends
	Since    time.Time
}
```

kept in `config.yaml` under `watches`, beside the favourites and like them
portable - it names things on servers, never a path on this machine. A
worktree is watched as its branch; a grouped worktree asks which repository,
as `J` does.

What was last seen of each watch - the pipeline id, its status, the head,
when it last changed, whether it ended, whether the user has seen it - is
cache, in `index-watched.json`.

Each kind is one implementation of

```go
type watchKind interface {
	// read asks the forge about one watch, cheaply.
	read(ctx context.Context, client forge.Provider, w config.Watch) (watchState, error)
	// changes says what happened between two readings, nothing on the first.
	changes(before, after watchState) []watchEvent
	// running says whether to ask again soon.
	running(s watchState) bool
}
```

so a new section is a kind, its row and its actions, and the poller is
shared.

## The poller

It grows out of `cipoll.go`, which already follows what is running and
stops when nothing is. One goroutine for all watches:

- a watch whose pipeline runs is asked every 15 s (`ciAskEvery`); one that
  waits is asked every 2 minutes, to notice a push that starts a new
  pipeline; an ended "until it ends" watch is not asked at all;
- a read is `MergeRequestPipeline` or `LatestPipeline` - one request, no
  jobs. The jobs are read only when a pipeline has just failed, to name the
  first failed job in the notification, and when the user opens it;
- requests run with the bounded fan-out the refresh uses, and a server that
  fails is backed off rather than asked every 15 s;
- the periodic reads are not jobs with a spinner - the status line would
  never be still. The Watched header says when the watches were last read;
  a read the user asks for (`r` on the screen) is a job as usual;
- when the lists' own following (`watchCI`) reads a pipeline that is also
  watched, the watch takes that reading instead of asking again.

Events for a pipeline: started, succeeded, failed (with the first failed
job), cancelled, waiting for a manual job, and for a merge request a new
head with a new pipeline. The first reading after a start is never news -
only a change from what was last seen is.

## Notifications

`internal/notify`, given a title, a line and the terminal state:

- **In the terminal.** OSC 777 (`ESC ] 777 ; notify ; title ; body BEL`) for
  Ghostty, WezTerm and foot; OSC 9 for iTerm2; OSC 99 for kitty; chosen by
  `TERM_PROGRAM` / `TERM`, the way `nerdfont.go` guesses its terminal.
  Inside tmux the sequence is wrapped for passthrough (`ESC P tmux; ... ESC
  \`), which needs `allow-passthrough on`; the card's `found` line says
  whether tmux has it.
- **From the system** when the terminal cannot, or when an editor has the
  terminal: an escape sequence written then would land in the middle of
  Neovim's output. On macOS `osascript -e 'display notification ...'`, on
  Linux `notify-send`.
- Settings › Integrations › Notifications: automatic (as above), terminal
  only, system only, or off; and a "send a test" key.

A notification says what and how it ended - "api-gateway !42 · pipeline
failed · test:unit" - and nothing more. Clicking it brings the terminal
forward at best; opening the pipeline from it is a later step
(`terminal-notifier -open <url>` could, but it is another dependency).

## The screen

A `pane` like the other three lists, with a header per section (the
grouping the lists already have). A row of Pipelines:

```
  WHAT                       CI   PIPELINE     BY      CHANGED
● acme/api-gateway !42       ✗    failed 4m    jane    2m ago
  acme/billing · main        ◐    running 1m   ci      now
  acme/web !17          ended ✓   passed 12m   john    1h ago
```

`●` marks a change not seen yet. Columns go through `layoutColumns` like
every list. Its actions, as `uiAction`s:

- `Enter` - the pipeline, in the jobs dialog that exists (`showPipeline`
  with the watch's `ciTarget`);
- `x` - stop watching; `a` - switch between "until it ends" and "always";
- `w` - the pipeline in the browser; `m` - go to the merge request or the
  repository in its list;
- `r` / `R` - read one / every watch now;
- `:` › Clear Ended Watches.

## Watching

"Watch Pipeline" on a repository (its clone's branch, or the default branch
before it is cloned), a merge request, a worktree and a repository's block in
the worktree view, and as a `pickKey` in the jobs dialog - one is most often
in front of a running pipeline when it occurs to them to wait for it. The
same key stops watching. A watched row in the lists wears a mark (a new
glyph, `glyphWatched`, with a theme key and a plain fallback).

Key: `W` is free on every list, though in the jobs dialog it opens the
pipeline in the browser; there the watch would need another key. `Ctrl-N`
("notify") is free everywhere. To decide before building.

## Later sections

Each a `watchKind`, its row and its actions:

- **Merge request activity** - new commits, new threads or replies,
  approvals, merged or closed. Most of it the refresh reads already (`NEW`,
  `COM`, `APPR`); a watch asks for it about one merge request often instead
  of every merge request on `R`.
- **Asked to review** - a watch on a query rather than a thing: a merge
  request where the user becomes a reviewer.
- **A branch behind its base** - a worktree's base moved on by more than N
  commits; read from the disk after a fetch, no request.
- **Releases or tags** of a repository.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/config` | `Watch`, `Config.Watches`, add/remove/find. |
| `internal/index` | `index-watched.json`: the last state of each watch. |
| `internal/notify` (new) | the escape sequences, the tmux wrapping, the system notifiers, the choice between them. |
| `internal/ui/watch.go` (new) | the kinds, the poller, the events; folds in what `cipoll.go` can share. |
| `internal/ui/watched.go` (new) | the screen, its columns, its actions. |
| `internal/ui/tabs.go`, `app.go` | the new tab, its page, the unseen count in its title. |
| `internal/ui/actions_lists.go`, `wtmodal.go`, `pipeline.go` | "Watch Pipeline" in the four places and the jobs dialog. |
| `internal/ui/integrations.go` | the Notifications card. |
| `internal/ui/screen.go` | whether the terminal has focus, readable by the notifier. |
| themes | `glyphWatched`, the unseen mark, colours for an ended row. |
| README, `help.go` | the screen, the keys. |

## Tests

- The poller against the fake API server, with a short `ciAskEvery`: a
  pipeline going running → failed gives one event and one notification;
  a first reading gives none; an "until it ends" watch stops being asked
  after it ends; a merge request merged by `R` drops its "always" watch.
- A new head on a watched merge request moves the watch to its pipeline.
- `internal/notify`: the exact bytes for each terminal, the tmux wrapping;
  a fake `osascript` / `notify-send` on `PATH` (serial) while the terminal
  is unfocused or suspended.
- A background failure never opens a message box and never takes a key.
- The screen: a layout test at several sizes, `assertLegible`, the help
  rows, `TestNoTwoActionsShareAKey` with the new key.

## Open details

- The tab number. `[4] Watched` with Settings on `[5]` is proposed; the
  other way keeps Settings where it is and puts Watched on `5`.
- How many watches before the polling matters. Twenty running pipelines at
  15 s are under 5,000 requests an hour - GitHub's limit for a token - but
  the lists' own refresh shares it. The backoff should read the rate-limit
  headers rather than guess.
- Whether a watch should go with a merge request automatically - for
  example "always watch the pipelines of my own merge requests". A rule like
  that is a later section, not a setting on this one.
