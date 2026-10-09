# Activity

The Watched and the Agents tabs become one screen, Activity: what runs in
the background and what is waiting for the user, in one place to look at
when a notification comes or after time away. Designed with the user on
2026-10-09; not built yet. It builds on the watches of
[watched.md](watched.md).

What it answers, top to bottom:

- what is open - the editors, as cards: context, never news;
- what needs me now, and what is under way - watches and agents in one
  list, by how much they want the user;
- what happened to the thing I chose - its state now and its history;
- what happened in all - a log of everything, with what came since the
  last visit set apart.

## The layout

```
 [1] Repos │ [2] MRs │ [3] Worktrees │ [4] Activity ●3 │ [5] Settings
╭ Open ─────────────────────────────────────────────────────────────────────╮╭ Watching ──────────────╮
│ ┌──────────────────────┐ ┌──────────────────────┐ ┌──────────────────────┐ ││  3 merge requests     │
│ │  nvim  unagit       │ │  nvim  incomm       │ │  idea  acme/api     │ ││  2 branches' pipelines│
│ │ wt-ci · fix/ci    3h │ │ main              1d │ │ review !341      20m │ ││                        │
│ └──────────────────────┘ └──────────────────────┘ └──────────────────────┘ ││ Enter all of them…     │
╰───────────────────────────────────────────────────────────────────────────╯╰────────────────────────╯
╭ Activity ──────────────────────────────────────┬ !334 · Rate limiting for the public API ─────────╮
│ NEEDS YOU                                      │ acme/gateway · Jane Doe → main · watched 2d      │
│●✗ !334    acme/gateway   Pipeline failed    2m │ CI   ✗ #8121 failed in test:unit · 12m run       │
│●◆ claude  unagit/wt-ci   waits for answer   5m │ MR   ✓ 1/2 approvals · 5 comments · 3 behind     │
│ ▶ main    acme/web       manual job         1h │ HEAD 9f8e7d6c John Smith "Refill the bucket…"    │
│                                                │ ── since your last visit ─────────────────────── │
│ UNDER WAY                                      │●14:02 ✗ Pipeline failed · test:unit              │
│ ◐ !341    acme/api       pipeline           4m │●13:58 ● New commit by John Smith "Refill…"       │
│ ✦ codex   incomm/rev-12  working           20m │ 11:20 ✓ Approved by msmith                       │
│ QUIET · 4                                      │ Oct 8 ● 3 new comments                           │
╰────────────────────────────────────────────────┴──────────────────────────────────────────────────╯
╭ Log ──────────────────────────────────────────────────────────────────────────────────────────────╮
│●14:02 ✗ !334 acme/gateway    Pipeline failed · test:unit                                          │
│●13:58 ● !334 acme/gateway    New commit by John Smith "Refill the bucket lazily…"                 │
│●13:41 ◆ claude unagit/wt-ci  waits for an answer                                                  │
│ ── since 14:20 yesterday ──────────────────────────────────────────────────────────────────────── │
│ yday  ✓ feat/x acme/web      Pipeline passed                                                      │
╰───────────────────────────────────────────────────────────────────────────────────────────────────╯
```

### Open: the editors, as cards

An editor is never news and never waits for anything: it says where
things are open. So it is not a row of the list but a band of cards above
it.

- A card is filled with a background a step off the screen's (a role of
  its own, `activity.card`, falling back on `surface.raised`), with a thin
  border a shade stronger, and a gap of the screen's own between cards.
- Its first line: the editor's icon and name, the repository. Its second:
  the worktree and the branch or the merge request, and at the right how
  long it has been open.
- Cards are of one width (about 24); as many as fit are drawn, the rest
  are a card `+N`.
- Tab goes into the band, h/l between cards; the chosen card takes the
  selection's colour. Enter goes to the editor; the actions are those of
  an open editor today.
- Nothing open: the band is not drawn, and its rows go to the panels.
- Below 30 rows the cards lose their border and become one line each
  (`[ nvim unagit wt-ci 3h]`), the band two rows instead of six.

### Watching: what is subscribed to

A small panel beside Open counts the watches by kind - merge requests,
branches' pipelines, and later kinds as they come (a query, releases) -
so how much is followed can be seen without counting rows.

- Its action, **Watches…**, opens a dialog with every watch in one list:
  its kind, the thing, its title, since when it is watched, its state
  now. `x` stops watching the row under the cursor (Space marks several,
  `x` then stops them all, after a confirmation), Enter goes to the thing
  in the list, `o` opens it in the browser.
- The action is the panel's own when the panel is focused (Tab reaches
  it, Enter runs it), and global too - from any tab, `:` lists it - since
  "what am I watching" is asked from anywhere. Its key is still to be
  chosen; `W` is free.
- With nothing watched the panel says so and offers how to start
  (`w` on a merge request or a branch).
- When the cards fold to one line below 30 rows, the panel folds with
  them to one line: `watching 3 MR · 2 branches`.

### The list: what wants the user

Watches and agents together, in sections by how much they want the
user; an empty section is not drawn, and within one the newest change is
first.

- **NEEDS YOU** - a pipeline that failed, a manual job waiting, an
  approval withdrawn, a merge request closed by someone else, an agent
  waiting for an answer (`◆`).
- **UNDER WAY** - a pipeline running, an agent working (`✦`).
- **QUIET** - watches with nothing going on, agents idle or done; folded
  into one line (`QUIET · 4`) when there is no room.

`●` before a row: it changed since the last visit (the mark Watched has
today). An agent leaves the list when its terminal is closed, as it leaves
the Agents tab today.

### The detail: the thing chosen

- Three or four lines of its state now: the pipeline, the approvals, the
  comments, the head, how far behind its base. For an agent: where it
  runs (repository, worktree, pane).
- Under them its history, newest first, with a line `since your last
  visit` setting apart what is new. An agent's history is its states
  (working → waiting → working).
- Below 120 columns the detail is not beside the list but opened over it
  with Enter, as the merge requests' detail is, and Esc goes back.

### The log: everything, in time

Every event of every watch and agent, newest first, with the line of the
last visit. It is what happened while the user was away.

- **Its height follows the terminal's**: about 30% of the rows left
  under the cards, never fewer than 3 lines of events. The list and the
  detail keep at least 8 rows; where they cannot, the cards fold to one
  line first and then the log goes down to its 3.
- It scrolls: Tab focuses it, j/k move, g/G go to the newest and the
  oldest.
- **It can be brought to the front**: `z` - or Enter on the log's
  title - opens the log as a dialog over the screen, most of its height
  and width, with the same rows and the same line of the last visit, and
  room for the whole sentence of each event. There it also filters (`/`)
  and narrows to one kind of thing (merge requests, branches, agents);
  Enter on an event closes the dialog and chooses its thing in the list.
  Esc goes back to the screen as it was.
- It follows the list both ways: a row of the log chooses its thing in
  the list and the detail; a thing chosen in the list lights its rows in
  the log.

## What it takes underneath

- **History per thing.** state.json keeps the 50 newest events for all,
  which is no timeline. Each watch gets an append-only log of its own,
  `watch/history/<key>.jsonl`, kept to its newest 200 events or 30 days;
  an agent's changes of state go into the same kind of log, which today
  nobody keeps.
- **The last visit** is a time, kept in `config.State` (state.json,
  never config.yaml), which draws the `since your last visit` line; the
  unseen marks stay as they are.
- **Tabs.** Six become five: Activity at `[4]` where Agents is, Settings
  at `[5]`. Every action of the Agents tab and of the Watched tab stays,
  offered by what the row is.
- **A merge request merged or closed** - still to decide with the user:
  kept in QUIET with its history for a day, or gone at once as today.
