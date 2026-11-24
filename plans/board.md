# Board

A plan, not built yet. Agreed with the user on 2026-11-24; the phases at
the end are the order it is built in.

unagit already knows everything a piece of work touches: the repositories,
the merge requests, the worktrees, the sandboxes and their tasks, the
editors and agents open, the pipelines. What it does not know is *why*:
which of those belong together, as one thing the user is doing. The Board
is that: a kanban of the user's own tasks, each card a piece of work with
what it is linked to, alive - the merge request's state, the pipeline, the
editor open in its worktree - and one key away from carrying on with it.
The place the user comes to first, and goes on from.

It is not a task manager. Other tools do that well, and unagit must not
copy them: no due dates, no priorities, no labels of its own, no subtasks,
no people assigned, no estimates. A card is a title, a description, a
state, a colour if the user wants one, links, and the user's own
comments on it. What makes it worth having is what the links do, and
that only unagit can do.

## Decisions

- **A screen of its own, `Board`**, a main tab like Repositories or
  Activity, `[6]` after Sandboxes - the numbers the user knows stay as
  they are. Settings › General › **Start on** chooses the tab unagit opens
  on (Repositories by default; Board for whoever uses it as the start).
- **Three states: TODO, IN PROGRESS, DONE**, the three columns. Fixed:
  no states of the user's own. A card moves one column at a time.
- **Spaces.** The user's tasks are scoped as their work is: per customer,
  per employer, per project. A space is a board of its own, named by the
  user; every task is in exactly one. There is always at least one, named
  **Task Board** at first; it can be renamed, not deleted while it is the
  last. Spaces are switched on the Board itself, and the one open is
  remembered.
- **DONE goes away by itself.** A task done for longer than Settings ›
  Board › **Hide done after** (1 day by default; minutes, hours or days;
  "never") leaves the board. Nothing is deleted: **History** shows the
  tasks done, newest first, searchable, and a task there can be opened
  again (back to TODO or IN PROGRESS).
- **A link outlives what it links to.** A worktree removed, a merge request
  merged, a sandbox deleted: the link stays on the card, dimmed, saying
  what became of it and when ("worktree removed · 3d ago"). Only the user
  removes a link. The card is a record of what the work was, as well as a
  way back into it.
- **A card can have a colour**, the user's way of making it stand out -
  red for what must not wait, say. It means nothing to unagit: no
  priority, no sorting by it, no filter, no rule. A handful of the
  theme's colours (red, orange, yellow, green, blue, purple) or none, the
  default. The order in a column stays what says what comes first.
- **A task has comments, the user's own.** Notes as the work goes on -
  what was tried, what is left, what someone said. They are the
  conversation of a merge request, reused: the same cards, the same
  dialog, the same keys. Flat, no threads: they are notes to oneself, not
  a discussion.
- **Links are to what unagit knows**: a repository, a merge request, a
  branch, a worktree (a grouped one too), a sandbox, a sandbox's task, and
  a URL for anything else. No issues for now; a merge request linked shows
  what the list knows of it (below).
- **Editors and agents are not linked, they are found.** A session record
  (`internal/session`) has its directory; an editor or agent open in a
  directory a task links to - its worktree, its repository's clone - shows
  on the card by itself. Nothing is stored for it, so nothing goes stale.
- **Linking happens where the user stands.** "Add to Task…" is an action of
  a repository, a merge request, a worktree, a sandbox and a sandbox task,
  wherever they are listed: a picker of the open space's tasks, with "New
  Task…" first. The Board's own "Link…" is the other way round, for when
  the task is in front.
- **State, not configuration.** Tasks are what the user is doing on this
  machine, like the watches: not in `config.yaml`, which is carried between
  machines in dotfiles. They live under the configuration directory, in a
  layout made to be synced later by git (below), though nothing syncs yet.
- **No server, no account, no AI** in what is built now. The AI is future
  work, at the end.

## The screen

```
 Board · Customer A                                         3 todo · 2 doing
╭ TODO ───────────────────╮╭ IN PROGRESS ────────────╮╭ DONE ──────────────────╮
│╭ Rate limiting ───────╮ ││╭ Fix login redirect ──╮ ││╭ Bump Go to 1.26 ────╮ │
││ ⎇ gateway  !42 ● ✓   │ │││ ⎇ auth-wt ✎ nvim     │ │││ !40 merged          │ │
│╰──────────────────────╯ │││ !51 draft ● running  │ ││╰─────────────────────╯ │
│╭ Audit log DLQ ───────╮ ││╰──────────────────────╯ ││                        │
││ ↗ jira · ↗ docs      │ ││                          ││                        │
│╰──────────────────────╯ ││                          ││                        │
╰─────────────────────────╯╰──────────────────────────╯╰────────────────────────╯
 h l column · j k card · H L move · n new · Enter open · o carry on · w browser
```

- **Cards are the cards unagit has**: the comments' bubbles
  (`ui/bubble.go`), the title on the frame, the body filled with the card
  background. The frame takes the card's colour when it has one, else
  the state's (new roles, `board.todo`, `board.doing`, `board.done`) -
  the column already says the state, so the frame is free for the
  colour. The title on the frame takes it too, and the fill a faint tint
  of it, so a red card is seen from across the board in both zooms. The
  selected card is framed as a focused box is, and the column with the
  cursor is lit as a focused panel. A card in DONE keeps its colour,
  quieter.
- **Two zooms.** *Compact*: the title and one row of the links as icons
  with their state's colour - `⎇` a worktree, `!42` a merge request,
  `●` its pipeline, `✎` an editor open, `⚙` an agent at work, `↗` a URL.
  *Full*: the first lines of the description, then each link on a row of
  its own with its state in words. The zoom is the Board's, kept in state,
  not per card.
- **The detail is a dialog**, as the conversation of a merge request is:
  the title, the description rendered from markdown, the links each with
  its actions, then below them the task's comments and its story,
  together, oldest first (below). Read with a cursor, selected and copied
  as a comment is.
- **A comment count on the card**, as an icon with the number, in both
  zooms; the full zoom shows the newest comment's first line too.
- **Narrow terminals**: below three columns' width, one column at a time,
  `h` and `l` changing it, its name and the others' counts in the header.
- **Empty**: a space with no task says how to make one and how "Add to
  Task…" works, as the other screens' empty states do.

### Keys

By the rule every action follows (navigation first, then the action's own
letter, lower case for the small form and upper case for the big one, no
Alt, Ctrl only where the letter is navigation's). Proposed, to be checked
against `keymap` and the help when built:

| key | does |
|---|---|
| `h` `l`, `j` `k` | column, card |
| `H` `L` | move the card to the column on its left or right |
| `K` `J` | move the card up or down its column (the order is the priority) |
| `n` | new task, in the column the cursor is in |
| `Enter` | the task's detail |
| `o` | carry on (below) |
| `e` | edit the title and the description |
| `c` | comment: the task's comments, a new one written at once (below) |
| `#` | the card's colour: a picker of the colours and none |
| `w` | open the link in the browser; a picker when there are several |
| `i` | link something to the task (`Link…`) |
| `x` | done (to DONE); in History, open again |
| `d` | delete the task, after asking |
| `v` | the zoom |
| `p` | the spaces: switch, new, rename, delete, move a task to another |
| `[` `]` | the space before and after |
| `y` | History |
| `/` | find a task in the space (History too, when it is shown) |
| `.` | everything the card can do |

The mouse does what the keys do, as everywhere: a click chooses, a double
click is `Enter`, the right button lists the actions, a drag of a card to
another column moves it there.

### Comments

The task's comments are the merge request conversation's machinery
(`ui/comments_dialog.go`, `ui/bubble.go`), fed from the task instead of a
forge:

- `c` on a card opens the detail at its comments with a new comment being
  written - what `c` does on a merge request. `Enter` and then the
  comments are the way to read them without writing.
- Each comment is a card with its time on the frame, its body markdown.
  The author is the user and is not drawn: every comment is theirs.
- The keys are the conversation's: `j` `k` a comment at a time, `c` a new
  one, `e` edit, `d` delete after asking, `v` a cursor in a long one, `y`
  and `Y` copy; a long comment is clipped and read through as there.
- **The story between them.** The task's events - moved to IN PROGRESS,
  a merge request linked, a worktree gone, its merge request merged -
  stand between the comments as quiet one-line notes, as GitLab's system
  notes do in a merge request, so the comments read in the order things
  happened. `h` hides the notes, as it hides the code in a conversation.
- Nothing is published anywhere: a task's comment stays in the task. (A
  comment meant for a merge request is written on the merge request.)

### Carry on

`o` is the reason for the Board: it takes the user back into the work. On
a card it goes, in this order of preference:

1. to an editor or agent already open in the task's worktree or
   repository - attaching to the Neovim, bringing the pane forward in
   Zellij, herdr, Ghostty or Rex, as Activity's cards do;
2. else it opens the task's worktree (or clone) the way Open does, with
   the editor the user has set;
3. else, with only a merge request, it starts its review;
4. else, with only a URL, it opens it.

With more than one candidate at the same level - two worktrees, an editor
and an agent - it asks, with a picker, as Open with does. The task moves
from TODO to IN PROGRESS on carrying on, unless Settings › Board says not
to.

## Links

A link stores what it points to and what it last knew of it:

```go
type Link struct {
	Kind  string    // repository, mr, branch, worktree, sandbox, sandbox-task, url
	Key   string    // the stable key: instance, project, iid; a path; a sandbox's name; the URL
	Label string    // as it was shown when linked, for when it is gone
	Path  string    // on disk, for a worktree or a clone
	URL   string    // its page, for w
	Added time.Time
	// What it last was: kept, so the card draws at once without the
	// network, and says what it was once it is gone.
	Seen  LinkState
}

type LinkState struct {
	State string    // open, draft, merged, closed; present, removed; running, stopped
	Since time.Time // when it became that
	Gone  bool      // no longer there: the link stays, dimmed, without actions that need it
	// A merge request's: its title, labels, author, target branch,
	// approvals, conflicts, pipeline status, comments.
	MR *MRSnapshot `json:",omitempty"`
}
```

- **Keys are the ones the watches use** (`watch.Watch.Key`), so a merge
  request is the same merge request in both, and the Board can show a
  watch's news on the card.
- **Alive from what unagit already reads.** Nothing new is asked of the
  servers for the Board: the merge requests' list, the watches, the
  worktrees on disk, unibox's sandboxes, the session records. A link is
  refreshed from them when they are read, and `Seen` written when it
  changes. A merge request no longer listed (merged, closed) is asked once,
  then kept as it was.
- **A merge request on a card** shows, compact: its state (draft, open,
  merged, closed), its pipeline, approvals and conflicts as icons. Full and
  in the detail: its title, labels, author, target branch and comments, as
  the list's detail column has them.
- **Gone** is decided by what owns the thing: a worktree whose directory is
  not there and not in `git worktree list`, a merge request merged or
  closed, a sandbox unibox no longer lists. The card keeps the link,
  dimmed, "merged · 2h ago". `w` still works for anything with a URL.
- **Done and tidying up.** Moving a task to DONE offers, in one dialog with
  everything ticked that is safe, what can now go: finish the review of its
  merge request, remove its worktrees (none with uncommitted work), stop
  its sandbox. Nothing is done without the dialog, and the dialog can be
  turned off. The other way round, a merge request linked being merged
  marks its card ("merged - done?"), never moves it.

```go
type Comment struct {
	ID      string
	Body    string // markdown
	Written time.Time
	Edited  time.Time // zero unless edited
}
```

## Spaces

```go
type Space struct {
	ID    string // a slug made once, never renamed with the space
	Name  string
	Order int
}
```

- `p` lists the spaces: Enter switches, and the keys there make a new one,
  rename, delete, and move the task under the cursor to the space chosen.
  `[` and `]` go to the space before and after, without the picker.
- The space open is in the Board's header and in the tab title
  (`Unagit · Board · Customer A`), and kept in `state.json`.
- Deleting a space with tasks in it asks where they go: another space, or
  History.
- "Add to Task…" lists the open space's tasks first, then the other
  spaces' under their names.

### Next step: a space bound to a place

A space can be bound to a part of the groups tree in Settings: a server, a
group, a folder - where the user already keeps customers and employers
apart. Then:

- "Add to Task…" on a repository, merge request, worktree or sandbox from
  there offers that space's tasks first, and a new task goes there;
- the Board can open on the space of the repository the user came from;
- a link from outside the space's place is allowed, and marked.

Not in the first phases; the data model keeps room for it (`Space.Places`,
a list of the tree's keys).

## Storage

Under the configuration directory, made to be synced by git later
without conflicts between machines:

```
~/.config/unagit/board/
  spaces.json            the spaces and their order
  board.lock             taken by whoever writes, as watches.lock is
  <space>/
    <task-id>.json       one file per task
```

- **One file per task**, so two machines changing different tasks never
  touch the same file; a git sync later is a commit and a pull. The order
  in a column is a sortable key in the task (a fractional rank), not a
  list in one file, for the same reason.
- **Every instance writes**, under `board.lock`, and reads what changed by
  the files' times, as the watches do (`watch.Store`). Two unagit open
  each see the other's moves within a look.
- **Paths are this machine's.** A synced task will carry links to
  worktrees that are not here; they show as gone ("not on this machine"),
  which the link model already covers.
- A task is written whole, to a temporary file and renamed, as the vault
  is.

```go
type Task struct {
	ID      string
	Space   string
	Title   string
	Body    string // markdown
	State   string // todo, doing, done
	Rank    string // its place in the column
	Colour  string // "", or one of the card colours: red, orange, yellow, green, blue, purple
	Created time.Time
	Moved   time.Time // into its state
	Done    time.Time // zero unless done
	Links    []Link
	Comments []Comment
	Story    []Event  // created, moved, linked, link gone, edited
}
```

## Where it touches the rest

- **`internal/board`** (new): the store, the model, ranks, the done
  cut-off, History. No UI, tested on its own.
- **`ui/board*.go`** (new): the screen, the cards, the detail dialog, the
  spaces picker, carry on.
- **`ui/bubble.go`**: cards, as they are; a compact form may need a flag.
- **`ui/comments_dialog.go`**: the conversation, made to take its
  comments and notes from a task as well as from a forge - a source
  behind an interface, not a copy of the dialog.
- **Actions of the lists** (`ui/actions_lists.go`, `wtmodal.go`,
  `sandboxes_actions.go`): "Add to Task…".
- **`internal/session`**: read to find the editors and agents of a task.
- **`internal/watch`**: its keys, and its news shown on cards.
- **Settings › General**: Start on. **Settings › Board** (new section):
  hide done after, tidy up on done, move to IN PROGRESS on carry on.
- **Themes**: the new roles - the three states, and the card colours
  (`board.card.red` and the rest, each a frame and a tint) - in every
  shipped theme and the theme reference, so a red card is the theme's
  red and readable on its background.
- **Help, keymap, `keybinds.toml`**: the Board's scope and keys.
- **Website**: a guide page (`guide/board.md`), Settings, Keys, the
  changelog.

## Phases

1. **The board.** Spaces, tasks, the three columns, moving, the two zooms,
   the detail dialog, comments, card colours, URL links with `w`, done going to History after the
   time set, History. Start on in Settings. Usable alone: a scratchpad of
   what is being done, per space.
2. **Links to what unagit knows**, alive: repositories, merge requests
   with their state, worktrees, sandboxes, sandbox tasks; gone links kept;
   carry on (`o`); tidying up on done.
3. **Linking from everywhere and what is found**: "Add to Task…" on every
   list, editors and agents shown on the cards, watches' news on them.
4. **Spaces bound to places** in the groups tree (above).

Each phase ships on its own, with its tests, its help and its website
page.

## Future work: an agent of the user's own

Out of scope now, and nothing in the phases above depends on it; written
down so the model does not close the door on it.

The idea: the user says what the task is in a sentence - typed, or pasted
from a chat - and an AI makes the card from it: a clear title, a
description, the links it can find (the merge request named, the
repository, the worktree on that branch, the sandbox), in the right space.
And while the work goes on, it keeps the card's story: what happened, from
the events unagit already sees.

### Bring your own agent

unagit has no AI of its own and must not get one: no API keys of its own,
no model chosen for the user, no tokens sent anywhere the user did not
send them already. The user has an agent - Claude Code, Codex, Copilot
CLI, opencode - and has signed it in. unagit calls *that*, in its
headless mode, in the background:

- **Found, not configured.** Integrations already finds the agents on the
  machine (`internal/agents`, the Integrations screen). Each agent there
  gains, besides how to start it in a terminal, how to run it headless and
  read a structured answer: Claude Code's print mode with JSON output,
  Codex's exec mode, and so on - per agent, in `internal/agents`, behind
  one interface. Settings › Board › **Agent** picks one of those found,
  "none" by default.
- **Asked through a contract, not a chat.** unagit sends a prompt with the
  user's sentence and what it may link to - the open space, the
  repositories, the merge requests the list has, the worktrees, the
  sandboxes, as a compact JSON - and asks for a JSON answer in a schema
  of unagit's: title, body, links by the keys given, space. The answer is
  checked against what exists; a key unagit did not give is dropped.
- **Shown before it is kept.** The card the agent proposes opens in the
  editor of the task, links ticked, for the user to change and save, or
  throw away. Nothing is created behind the user's back.
- **No tools for the agent.** It is asked to rephrase and to choose from a
  list, not to run commands or read the disk: headless, with its tools
  off where the agent allows it, in a temporary directory, with a time
  limit. The forge tokens never reach it - the vault stays out of its
  reach, as it is out of every agent's (AGENTS.md, the one hard rule).

### The story

Every task has a story (`Task.Story`) from phase 1: created, moved,
linked, a link gone. With links alive (phase 2) it can also take what
unagit sees of them: commits in its worktree, the merge request's
pipeline, approvals, merged. That much needs no AI - it is the Activity
log, kept per task.

The user's comments are part of it, and the best part: what was tried
and why is in them, not in the events. What an agent could add is the
summary: "Monday: started the branch,
pipeline failed on lint; Tuesday: review from Jane, two threads, fixed;
merged Wednesday." Written on demand (a key in the detail), or when the
task goes to DONE, from the story's events, by the same headless call.
Kept in the task as text the user can edit.

### Open questions for then

- Which agents have a headless mode with structured output good enough,
  and how each turns its tools off.
- How long a call may take before the dialog says so, and whether to run
  it while the user types the rest.
- Whether the sentence may come from outside unagit - `unagit task new
  "..."` on the command line, for an agent working in a terminal to file
  what it found.
- What of the story may be sent at all: commit messages and comments are
  the user's company's, and the user decides whether their agent sees
  them, per space.
