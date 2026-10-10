# Rebase, base branches and squashing

A plan, agreed with the user on 2026-10-10, for the next part of commit
support: choosing a branch's base, rebasing onto any branch, a commit log
that shows where local history ends and origin's begins - also once the
two have parted - and squashing commits picked in the log. It is written
to be carried out in a fresh session: what is already there, what is to be
built, in which order, and how it is tested.

Read [AGENTS.md](../AGENTS.md) first; every rule there holds. In
particular: actions are data (`uiAction`, `pickKey`), offered only where
they can be done (`when`), named in Title Case with `…` before a dialog;
messages go through `flash`/`note`/`done`/`errorf` and never from the
background into a box; every new dialog gets a layout test at several
sizes and `assertLegible`; colours and glyphs come from the theme.

**Commits.** At first the user committed through unagit and the changes
were left in the working tree; since part 5 the work is committed and
pushed as it lands, as AGENTS.md says. Stage only your own files
(`git add <paths>`, never `-A`): the user edits in IntelliJ beside the
session.

## What is there already

How commits work in unagit (AGENTS.md, "Commits are IntelliJ's, not
git's"): a file is versioned or not, a change is committed as it is on
disk, nothing stages. What touches history today:

| Where | What | Code |
| --- | --- | --- |
| commit log `e` | Edit Commit Message…, commits no remote has | `gitx.RewordCommit` (`internal/gitx/commit.go`): amend for HEAD; for an older one `commit-tree` with the author kept, then `rebase --rebase-merges --autostash --onto <new> <old> <branch>` |
| commit log `u` | Undo Commit, the newest local commit | `gitx.UndoCommit`: `reset --soft HEAD~1`, refuses the first commit and a merge |
| Worktrees `p` | Pull: an unpushed branch with a known base is rebased onto it | `workspace.UpdateBranch` |
| Worktrees `Ctrl-R` | Rebase onto Base, pushed or not | `workspace.RebaseOntoBase` (`internal/workspace/update.go`) → `moveOnto`; notes `branch.<name>.unagitRebasedFrom` (`gitx.SetRebasedFrom`) so `P` force-pushes with exactly that lease |
| `P` everywhere | Push, Force Push offered beside it (`offerPush`, `internal/ui/worktrees.go`), lease = upstream as last fetched (`gitx.UpstreamTip`) | `gitx.PushHead` |

The base of a branch is unagit's own record, `branch.<name>.unagitBase`
(`gitx.SetBranchBase`, `gitx.BranchBases`, key `unagitbase`). Git itself
keeps no record of what a branch was made from: the upstream is the same
branch on origin, and the reflog's "Created from" is local and expires.
unagit writes the base only for branches it makes (a new worktree, a new
branch in the branch manager, a group's members); a branch made outside
unagit has none, and Rebase onto Base / Show Changes Since Base are not
offered for it (`based` in `worktreeActions`, `internal/ui/actions_lists.go`).
`remoteState.Base` (`internal/ui/worktrees.go`) carries it to the lists;
`BaseBehind` is measured only for a branch not pushed yet.

The commit log (`internal/ui/log.go`): `showCommitLog` lists `logCommit`s
in a picker (`showPickerWith`, `internal/ui/modals.go`); `localLog` reads
them with `gitx.History(dir, "HEAD", historyLimit)` and marks each
`Unpushed` (not on the upstream, `gitx.Unpushed`) and `Local` (on no branch
of any remote, `gitx.LocalCommits`). `labelLog` draws the rows; an
unpushed one starts with `glyphAhead`. Keys are `pickKey`s with a `when`
per item; one with no `hint` is left out of the hint and found in the
item's actions (Alt-Enter), as Edit Commit Message and Undo Commit are.
Pickers have no marks today - lists do (`pane.marks`, space, `bandMarked`).

The branch manager (`showBranchManager`, `internal/ui/branches.go`) lists
`branchInfo`: local and on origin, where it is out, its upstream, whether
it is the default, its open merge request.

## 1. Set Base… and Rebase onto…

Small and independent; do these first.

**Set Base…** - choose the branch a branch was made from, for one made
outside unagit or one whose base was wrong.

- Offered on a worktree of one repository (Worktrees list and worktree
  view, `worktreeActions`) and on a clone in Repositories whose branch is
  not the default one. Not on a group (each member has its own; light its
  block in the view).
- A picker of the repository's branches, local and on origin, the
  current base first if there is one, then the default branch, then the
  target of the merge request open from the branch (`openMRFor`), then
  the rest by name; the branch itself is left out. Reuse what the branch
  manager reads (`branchInfo`) rather than reading branches again.
- Enter writes `branch.<name>.unagitBase` (`SetBranchBase`) - in the
  clone's config, which every worktree of it shares - and says `done`.
  Refresh the row so `RMT` and `based` follow.
- Removing a base is not needed now; choosing another replaces it.

**Rebase onto…** - rebase the branch onto any branch, once.

- Offered where Rebase onto Base is (`Ctrl-R`), named "Rebase onto…",
  found among the actions (no key of its own unless one is free in every
  list it is in - check `TestNoTwoActionsShareAKey`).
- The same picker as Set Base…, the base first.
- Runs as `RebaseOntoBase` does, with the chosen branch for the base:
  refactor `RebaseOntoBase(dir, base)` into a `RebaseOnto(dir, onto)` it
  calls, so the busy check, the fetch, the `moveOnto` refusal of a
  conflict (nothing changes then) and the `unagitRebasedFrom` note for a
  pushed branch are shared. The base recorded does not change.
- Under `runTaskNoting` as `rebaseWorktree` (`internal/ui/sync.go`) is,
  through `moveMany`, so Incomm's comments are re-anchored after.

Tests (`internal/workspace` and `internal/ui`, real repositories as
`rebase_test.go` makes them): Set Base… on a branch made with plain git
writes the key and makes Rebase onto Base offered; Rebase onto… a branch
other than the base moves the commits on top of it and leaves the base
alone; a conflict leaves the branch exactly as it was; a pushed branch
rebased is then force-pushed by `P` with the noted lease.

## 2. Where local history ends, in the log

The log shows where origin's copy of the branch stands, and - once the
two have parted - what is only here and what only on origin. Sections,
not two columns: a terminal has no room for two, and a commit is in one
place or the other.

```
  a1b2c3d4  Bill them               (only here)
  e5f6a7b8  Count requests          (only here)
 ── origin/feat/x ───────────────────────────────
  9c0d1e2f  Add the limiter
  3a4b5c6d  Start the gateway
```

When the branch and its upstream have diverged - after a squash, an
amend or a rebase of pushed commits, or a push from elsewhere - three
sections:

```
 ── only here · a force push puts these on origin ──
  f00dbabe  Count and bill requests
 ── only on origin · a force push removes these ──
  a1b2c3d4  Bill them                     (dim)
  e5f6a7b8  Count requests                (dim)
 ── shared ───────────────────────────────────────
  9c0d1e2f  Add the limiter
```

- Read in `localLog`: `git rev-list --left-right @{upstream}...HEAD` (or
  the log of `HEAD...@{upstream}` with `--left-right --boundary`) says
  which side each commit is on; the shared part is `merge-base` down.
  A branch with no upstream: only the first kind of line, at the newest
  commit some remote has (the `Local` boundary). Add what is needed to
  `gitx` with a test of its own, not in the UI.
- The commits only on origin are listed too, between the two separators,
  dimmed (a role of their own, falling back on `text.dim`); Enter, `D`,
  `w` and `y` work on them, nothing that rewrites (`when`).
- A separator is a row of the picker that cannot be chosen: the cursor
  steps over it (check `showPickerWith` for a non-selectable item; add one
  if there is none - `pickItem` with a flag - and keep j/k, g/G and the
  filter right with it). Filtering hides the separators.
- The separators' words are short and in the hint's voice; their colour
  is a role (`log.boundary`, falling back on `text.dim`).
- `glyphAhead` before an unpushed commit stays.

Tests: a branch two commits ahead shows one separator under them, at
`origin/<branch>`; after rewriting a pushed commit with plain git, the
log shows the three sections with the right commits in each; the cursor
never stops on a separator; a filter hides them.

## 3. Marks in the log, and Squash Commits…

**Marks.** Space marks commits in the commit log as it marks rows in every
list: the marks' band (`bandMarked`/`colMarked`), moving on to the next
row, Esc clearing them before it closes the log. A picker has no marks
today, so this is new to `showPickerWith`: add marks as an option of
`pickerOptions` (off for every other picker), drawn with the same band,
and hand the marked items to the keys (`pickKey.run` gets the item under
the cursor; give the keys a way to read the marked ones - a function on
the options, or a second argument - and keep `when` able to see them so
the actions offered follow the marks). The title counts them, as the
Changes dialog's does ("· 2 marked").

**Squash Commits…** - one commit out of several next to each other.

- Offered (`when`) when two or more commits are marked, they are next
  to each other in the history (no unmarked commit between the oldest and
  the newest marked), none is a merge, and all are on the branch's side
  (not in the "only on origin" section).
- A form like Edit Commit Message's (`editCommitMessage`, `log.go`): the
  message starts as the marked commits' messages joined oldest first,
  each separated by a blank line, as git's own squash does; the author is
  the oldest commit's. Save squashes; Cancel goes back to the log with the
  marks kept.
- In `gitx`, `SquashCommits(dir string, oldest, newest string, message
  string) (string, error)`, built as `RewordCommit` is: the tree of the
  newest, the parents of the oldest, `commit-tree` with the oldest's
  author (`GIT_AUTHOR_*`), then `rebase --rebase-merges --autostash
  --onto <new> <newest> <branch>` for the commits after it; on a failure
  `rebase --abort`. Refuse a detached HEAD, an operation in progress, a
  range not on the branch, a merge inside it.
- **On pushed commits** it is allowed, after asking: "3 of these commits
  are on origin. Squashing them rewrites its history: a force push will
  be needed, and anyone who built on them has to rebase." Accept with
  "Squash". After it, note the upstream it moved away from in
  `branch.<name>.unagitRebasedFrom` (`SetRebasedFrom`) as a rebase does,
  so `P` can force-push with exactly that lease; the log then shows the
  diverged sections of part 2.
- The log is read again with the new commit under the cursor and `done`
  said ("squashed 3 commits into f00dbabe").

Tests: `gitx` - squashing the middle three of five keeps the tree, the
oldest's author, the message given, and the two after it with their own
messages; a merge in the range is refused; the uncommitted edits survive.
UI - marking with space shows the band and the count; Squash Commits… is
offered for two marked neighbours and not for one, nor for two with a gap;
the form starts with both messages; on pushed commits the question names
the force push, and afterwards the log shows the three sections; Esc
clears marks before closing; a layout test of the form at several sizes.

## 4. Rewriting what is pushed, elsewhere

Once part 2 shows a diverged history plainly, Edit Commit Message and
Undo Commit can do what Squash does on pushed commits: offered there too,
after the same question, noting `unagitRebasedFrom`. Today both refuse a
commit any remote has (`Local`); change the `when`s and the refusals
together, and keep the question. Do this last, and only once 2 and 3 are
in and look right on screen.

## Order, and when a part is done

1. Set Base… and Rebase onto….
2. The sections in the log.
3. Marks in the log, then Squash Commits….
4. Edit Commit Message and Undo Commit on pushed commits.

Each part is done when its tests pass (`go test ./...`; `-race` before a
commit, see AGENTS.md), `gofmt -l .` and `go vet ./...` are clean, the
README says how to use it (keys and behaviour; the `?` help gets keys only,
one short line each), the new dialogs and the log were drawn on the
simulation screen and looked at, and the user has seen it. Update this
file as parts land: what was built, and where it differs from the plan.

## What was built

All four parts are in, committed and pushed. `go test ./...`, `gofmt -l .` and `go vet ./...` are clean; the
base picker, the log with each kind of line, the marked log, the squash
form and its question were drawn on the simulation screen and looked at.

**1.** `workspace.RebaseOnto(dir, onto)` and `RebaseOntoBase` share
`rebaseOnto`: the busy check, the fetch, `moveOnto` and the
`unagitRebasedFrom` note. The picker is `chooseBase` in
`internal/ui/base.go`, ordered by `baseCandidates`, each row saying why it
is where it is ("the base now · default", "target of !12", "local only").
Differences: without a token origin's branches are left out and the
clone's are listed, rather than nothing. Neither action has a key - both
are found with `Alt-Enter`. Rebase onto… is offered on a worktree of one
repository only, not on a group as Rebase onto Base is: the picker lists
one repository's branches (in the worktree view, light a member's block).

**2.** `gitx.UpstreamOnly` reads the upstream and what only it has;
`sectionLog` orders the commits, `logRules` places the lines, `pickItem.Rule`
is a row the cursor steps over (`offRule`/`pass` in `showPickerWith`, since
tview reports a move before it makes it, so the cursor cannot be corrected
in its changed callback). Roles `log.boundary` and `log.theirs`. Beyond the
plan: a branch only behind its upstream also lists origin's commits, under
"only on origin · a pull brings these", then "shared"; the cursor starts on
the first commit of the branch, not on origin's. The line of a branch with
no upstream reads "on origin". The arrows in pickers no longer wrap from
the last row to the first, as j/k never did.

**3.** `pickerOptions.marks` takes a `*pickMarks` the caller keeps, so the
keys' `when` and `run` read the marks and a log opened again keeps them
(`showMarkedLog`); `bandedList` paints the band after tview's List draws.
The key is `s`, out of the hint, which says `space mark`.
`gitx.SquashCommits` as planned; `gitx.Rewriting` wraps any rewrite and
notes the lease when the branch has moved off its upstream - squash, edit
message and undo all go through it. The worktree row and `P`'s question
for such a branch now say "rewritten here - rebased or squashed" instead
of naming the base. Cancel on the question before squashing pushed
commits closes it without going back to the log: `confirmChoices` has no
cancel callback.

**4.** `e` is offered on every commit of the branch, `u` on HEAD
(`isHead`, since the first row may now be one of origin's), each asking
first when origin has the commit (`onOriginQuestion`).

## 5. Rewrite History, and undoing it

Agreed with the user on 2026-10-10, after parts 1 to 4 were in, and built
the same day.

- **The record** (`internal/gitx/rewrites.go`) is the repository's:
  `<git-common-dir>/unagit/rewrites.json`, written under a lock, each
  rewrite with the branch's tip before and after, its configuration before
  (every `branch.<name>.*` key), and whether the files changed too. Refs
  `refs/unagit/rewrites/<id>/before` and `/after` keep the commits from
  git's collection. The newest 200, none older than 90 days.
- **What is written down**: everything through `gitx.Rewriting` - squash,
  edit message, undo commit, set base, the rebase in `moveOnto` (so `p`,
  `Ctrl-R`, Rebase onto… and a merge request's update) - and a branch
  deleted (`DeleteLocalBranch`, or `RecordDeletion` for one only origin
  had). A rebase that only brought the branch forward rewrote nothing and is
  left out. `Rewriting` also notes the lease, so `rebaseOnto` no longer does.
- **Undo** (`PlanUndo`, `UndoRewrite`) goes back to before the rewrite and
  the later ones of the same branch, as a stack: `reset --soft` when only
  the history changed, `reset --keep` after a rebase, `branch --force` when
  the branch is out nowhere; the configuration goes back too. Commits made
  since are counted in the question; the undo is itself recorded, so
  nothing it sets aside is lost. A deletion is undone by making the branch
  again; a branch of that name made since blocks it.
- **UI** (`internal/ui/rewrites.go`): Rewrite History… - `H` in a commit
  log and in the branch manager, an action of a repository and of a
  worktree - lists the record (`u` undo, `Enter` the commits before and
  after, `D` the files in Hunk, `y` the commit before). Each change is an
  event of the Activity log (`watch.Event.Local`, never unseen, never on
  the tab's count); `H` there narrows the log to them, and `Enter` on one
  opens Rewrite History on it.
