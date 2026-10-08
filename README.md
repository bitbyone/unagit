# unagit

<img src="docs/unagit.png" alt="UNAGIT" width="520">

### **U**niversal **Nav**igator **A**round **GIT**

*(Or, if you happen to watch Friends - any resemblance is purely coincidental.)*

**One place to see every repository you work with, decide where each one lands
on disk, and get into it.** Clone it, switch its branch, open it in your
editor - and when it is a merge request you are after, land in a checkout where
the whole change is already sitting there as pending edits.

GitLab and GitHub at the same time, in one list. Pull requests are merge
requests here too - one word for one thing.

```
 [1] Repositories │ [2] Merge requests │ [3] Worktrees │ [4] Agents │ [5] Settings
 /
╭ Merge requests ─────────────────────────────╮╭ acme/api-gateway !42 ─────────╮
│   REPO              MR  TITLE           COM ││ !42  Fix login rate limiting  │
│ ● acme/api-gateway  !42 Fix login rat…    7 ││ acme/api-gateway              │
│ ○ acme/billing      !17 Invoice roundi…     ││ feat/rate → main              │
│                                             ││                               │
│                                             ││ MERGE REQUEST                 │
│                                             ││ Author       jane (Jane Doe)  │
│                                             ││ Created      3d ago           │
│                                             ││ Reviewers    john             │
│                                             ││ Approvals    1 of 2 · john    │
│                                             ││ Pipeline     ● running        │
╰─────────────────────────────────────────────╯╰───────────────────────────────╯
 DETAIL  84/84 merge requests · indexed 4m ago · scope all projects
 ? help · q quit
```

## Why

Work spread over a few dozen repositories and two forges turns into a mess of
its own: half of them cloned, half not, nobody remembers where, and the ones
you cloned last year sit in whatever directory you happened to be in at the
time. Add a review and it is a browser tab for the diff, a terminal for the
code, and a ritual of `fetch`, `checkout`, `stash` every time you switch.

unagit is the navigator over that: one list of everything, a layout on disk
that you decide instead of inherit, and one keystroke to be inside any of it.

## Install

```sh
make install     # -> ~/.local/bin/unagit
unagit           # asks for a passphrase, then walks you into Settings
```

Nothing to edit by hand. In **[5] Settings** you add servers (`a`), paste a
token, pick the groups you work with (`space`) and say where they should be
cloned (`d`). GitLab wants a token with the `api` scope; GitHub wants `repo`
**and `read:org`** - without the latter GitHub answers the organisation listing
with an empty array rather than an error, so your orgs simply would not show up.
`v` checks a token against the server before you trust it.

Requirements: Go 1.26+, `git`, and an editor (`nvim` by default).

## Navigating

`1` lists every repository across every server and group you picked. `○ ● ◐ ◉`
say what is on disk, and the path column says where - which matters once
different groups live in different places.

**Nothing has to be remembered.** `Alt-Enter` (or `Ctrl-A`, for a terminal
that keeps Alt-Enter for itself) lists everything that can be done with the
row under the cursor - or with the selected rows, or the lit block of a
worktree's view, or the section of Settings - and `:` everything the screen
itself can do. Each action has its key beside it, so the list teaches the keys
as it is used; what one usually comes for is at the top and what destroys
something at the bottom. Actions are named in a word or two, and a pane at
the bottom says in a sentence what the one under the cursor does. It opens
typing into its filter, so an action is found by its name or, failing that,
by what it does; `Enter` does it, the arrows move, and `Esc` goes to the list
(`j`/`k`) - or, with nothing typed, closes it. Some actions are rare enough to have no key at
all and live only there.

- **Create a new repository** (`:` in Repositories) asks where - one of the
  groups picked in Settings, or a subgroup of one taken with its subgroups -
  and for its name, description, visibility, first branch, a README, a license
  and a `.gitignore`. It is created on the server, cloned at once and listed
  with the others, with the cursor on it. GitLab gets the license and the
  `.gitignore` from its templates in a commit right after; on GitHub either
  of them brings a README along, as GitHub writes them only into a repository
  it initialises.

- `C` clones it to disk without opening the editor. `Ctrl-C` is left to end
  unagit, as it ends any program in a terminal.
- `Ctrl-O` opens your editor in it at once, as it is on disk - no fetch, no
  pull. Only a repository not cloned yet is cloned first. The lists say where
  everything stands, and `p` updates it first when you want that. The same
  goes for `Ctrl-O` in Merge requests and Worktrees.
- `b` manages the branches. Each says whether it is in the clone, on origin
  or both and how far apart (`↑2 ↓1`), whether it is checked out and where,
  and whether it is the default or protected. `Enter` switches **that same
  clone** to it, so there is one working copy per repository, not a directory
  per branch. It is a plain checkout: nothing is fetched and no editor
  starts - `p` pulls, `Ctrl-O` opens - and the list closes, the row showing
  the branch now out. `n` makes a new branch in the clone, starting from the one under
  the cursor (or another you choose); it appears in the list with the cursor
  on it, and `Enter` switches to it when you want. A branch with a merge
  request open says `!12 open`; `m` on one without opens a merge request
  from it, pushing it first if origin lacks it. `d` deletes it in the clone (origin keeps it), `D` in the clone
  and on origin, `Alt-D` on origin only (the clone keeps it, no longer
  tracking). The default branch and protected ones are never deleted, and
  one with an open merge request is not deleted on origin. One out in the
  main clone cannot be deleted there; one out in a worktree - unagit's or
  another tool's, at the directory git lists - is deleted with that worktree
  (a grouped one's member is taken out of its group in Worktrees). What
  would be lost - commits, uncommitted edits - is counted in the question
  first. `Ctrl-W` gives the branch under the cursor a worktree, as in the
  list; one out somewhere already says where. In a worktree's view, `b` on a repository's
  block opens the same list, without `Enter`: a worktree's branch is not
  switched there.
- `Ctrl-W` gives a branch a worktree of its own instead (`n` for a new
  branch). A branch git will not check out again - out in the main clone or
  in another worktree - is listed last, dimmed, with where it is out. Nothing opens: unagit moves to Worktrees with the cursor on it, and
  `Ctrl-O` opens it when you want.
- `RMT` (remote) says where the clone's branch stands against origin: `✓` up to
  date, `↓3` behind, `↑2` commits of yours not pushed, `↑2↓3` both. It is read
  from the refs on disk, so it costs nothing, and read again whenever you
  switch to a list or come back to the terminal from another window - an edit
  made in your editor shows up without asking. `R` fetches every clone in the
  background first - `r` only the one under the cursor - and the header counts the fetches still running. A
  rebase or merge git stopped in the middle of shows there instead, in red.
- `CI` is the newest pipeline of the clone's branch, drawn as in Merge
  requests; a repository not cloned has none, and costs no request. It is
  read on `R` (`r` for one row) and kept for the next start; one still
  running is asked about again until it ends.
- the edits column, headed `✎` (a pencil with a Nerd Font), counts the
  files not committed in the clone, and `Enter` lists them
  with the clone's branch, HEAD and the commits origin does not have yet.
- `p` updates the clone: a fast-forward when nothing of yours is in the way, a
  rebase of your commits and uncommitted edits onto origin when there is. If
  that would conflict - your edits touch a file origin changed, or your commits
  clash with origin's - it refuses and leaves the clone exactly as it was, for
  you to sort out by hand. `Alt-P` does the same for every clone at once,
  passing over those without an upstream and naming the ones it left alone.
- `m` jumps to that repository's merge requests, `w` opens it in the browser,
  `d` deletes it from disk - after warning about uncommitted or unpushed work.
- `/` fuzzy-finds, `L` hides everything you have not cloned (also in `v`), `x` hides a
  repository you never want to see.
- `v` also lists the columns in the order they stand, each with an eye: space
  hides one or shows it again, in Merge requests and Worktrees too. What you
  hide is kept in `config.yaml` under `filters.hidden_columns`; the column a
  row is - the repository, a merge request's title - always stays.
- The `NEW` column counts the commits pushed to a merge request since your
  review last checked out its head (`●` when they are not on disk to count),
  `APPR` its approvals - `✓` when you approved, `1/2` of those asked for -
  and `CI` its pipeline: a full circle green when it passed, red when it
  failed, a turning one while it runs, an empty one in blue while it waits
  its turn and grey when it was skipped. `COM` counts
  the threads still to resolve, in amber, where GitLab says; on GitHub it
  counts who has said something. All of it is read on `R`, and the
  refresh sums up what it found: new merge requests, ones with commits
  since your review, failed pipelines, worktrees tidied away. A column
  nothing has anything in stays out of the way.
- `R` runs behind the list, not in a dialog: the header turns a spinner,
  and the lists stay there to move in and read.
  The pipeline, approvals and threads are asked only of the merge requests
  the list shows - a hidden author's, a hidden repository's, the drafts you
  hide keep what was last known - so hiding what you never look at makes
  the refresh faster. The header's end, before `? help`, says how far it
  has got, as it does for `r` on one row, for fetches and for whatever
  else runs behind the lists, and what was just done is said there too;
  a warning or an error comes up in a box of its own until `Esc`. When the
  terminal is too narrow for both, that end takes a line of its own just
  above the status line.
- Space marks rows in Repositories and Merge requests, and the keys then
  act on every marked row at once: `x` and `H` hide, `r` refreshes, `p`
  pulls, `C` clones, `Ctrl-T` tags, `Ctrl-F` stars, `y` copies one of each
  a line, `V` marks merge requests reviewed, `Ctrl-W` makes one grouped
  worktree of repositories. Alt-Enter lists what the marks can do. `:` in
  Repositories also pulls every favourite at once.
- Repositories' `SIZE` is what a clone takes on disk with all its
  worktrees - its merge requests', reviews', branches', grouped ones' -
  measured behind the list at the start and again as the last step of `R`,
  or on its own by `:` › Measure Sizes Again. With `SIZE` hidden and the
  list not ordered by size, nothing is measured until it is shown again.
- With a GitHub account, `:` in Repositories › View Starred Repositories…
  lists what you starred, with its language and stars: `Enter` reads the
  README, drawn from its markdown (`O` opens it in your editor), `w` the
  page in the browser, `C` clones it. A repository cloned from there joins
  Repositories, though none of your groups holds it, with a star badge
  saying where it came from (`index-starred.json` keeps it there).
- Authors and reviewers are shown by name. GitLab's lists carry the names;
  GitHub's carry only logins, so `R` asks for the names it does not know
  and keeps them in `index-users.json` for a month.
- `V` marks a merge request as reviewed without opening it - read in the
  browser, or in Hunk with `D` - so `NEW` and the commit log count only what
  is pushed after.
- `J` shows the head's pipeline: its jobs, the first that failed under the
  cursor. The same `J` works on a repository (the clone's branch, or the
  default branch before it is cloned), on a worktree (its merge request's
  pipeline while one is open, else the branch's), and on a grouped worktree,
  where it asks which repository first. The jobs still to run are there
  too: a manual one (`▶`), which `R` starts, a delayed one (`◷`), which `R`
  starts at once, and a trigger job (`↳`), on which `Enter` lists the
  pipeline it started and `Esc` comes back. `Enter` reads a job's log - its end,
  in the colours the job printed it in, `Ctrl-D`/`Ctrl-U` by half a page,
  `Ctrl-F`/`Ctrl-B` by a whole one - `R` runs it again, `w` opens it, `W`
  the pipeline, and the jobs stay open behind the browser. A job run again
  keeps its earlier attempts under it (`↺`), whose logs can still be read,
  and `P` lists the earlier pipelines of the same merge request or branch;
  `J` in a commit log lists the pipelines of the commit under the cursor.
  A commit's detail lists the files it changed with the lines gained and
  lost, from git or, when the commit is not on disk, from the forge. While a pipeline
  runs, its jobs are read again every few seconds and change in place
  (the title says "following"), and a running job's log keeps growing at
  its end, as `tail -f` does, unless you have scrolled up; neither forge
  pushes changes, so this is asking again, and it stops when nothing
  moves any more. On GitHub the jobs are the check runs, and the log is
  GitHub Actions', which GitHub gives only once the job has finished.
- `v` in Merge requests narrows them to yours, to those you review or are
  assigned to, or both, and can leave the drafts out.
- When `R` finds a merge request merged or closed, its worktrees are removed
  without asking - unless they hold work of yours: uncommitted changes,
  unpushed commits, your own edits in a review, comments not yet published.
  Those are kept, and named.
- `H` on a merge request hides its author's merge requests - Renovate's, a
  CI bot's - and the header counts the hidden authors. `x` there does the
  same for the repository's merge requests, the repository itself still
  listed in Repositories (`H` there does it too); hiding the repository
  everywhere is Repositories' own `x`. `v` in Merge requests
  turns the filter off for a while, or, space on an author, shows that one
  again for good.

**Where things land is yours to decide.** A repository is cloned under the
first of: its exact repository destination (`e` in Repositories), its group's
own directory, its server's, the global default. So
`acme/platform` can live in `~/work/platform` while everything else stays under
`~/unagit`, and a subgroup can override its parent. It is one keystroke (`d`)
in Settings, and it applies to everything cloned afterwards.

```
<root>/<group>/<repo>                        the clone you switch branches in
<root>/<group>/.unagit/<repo>/<iid>-<branch>     a branch worktree for one MR
<root>/<group>/.unagit/<repo>/review-<iid>-<branch> a review worktree for one MR
```

The repository PATH column shows planned destinations in a dim colour until cloned.
Repository directory overrides bypass all root and group rules. Worktrees live in
`.unagit/<directory-name>` beside that exact destination. Existing `.mrs` and
`.reviews` worktrees remain available in their original locations.

## Grouped worktrees

Work that spans several repositories - a change to an API and its two clients,
say, or a task for an AI agent that has to see all of them - wants them in one
folder. In Repositories, `space` selects a repository and moves on (the header
says `SELECT 3`, and the selected rows have a band of their own; `Esc` clears
it); `Ctrl-W` on a selection then asks for:

- **New branch** - required: the group always works on a branch of its own,
  made in every repository.
- **Folder** - the name of the folder; it follows the branch until you change it.
- **A base branch for each repository** - where its new branch starts, and
  what `p` rebases it onto until it is pushed. It starts on the branch the
  main clone has checked out, or the default branch for a repository not
  cloned yet (it is cloned on the way). Any branch is offered except one a
  worktree has out - a worktree is not built on another worktree's branch.

```
<root>/.unagit/groups/<folder>/<repo>     a worktree of each repository
<root>/.unagit/groups/<folder>/.unagit-group.json
```

In Worktrees the group is one row marked `◆`, `REPOS` says how many it holds,
and `Enter` opens it as a view of its own (see below).
`Ctrl-O` opens the folder, `p` updates every repository in it (see below), `P`
pushes every repository that has commits origin lacks - a branch with nothing
of its own yet stays local, rather than leaving an empty branch on origin -
and `d` deletes the worktrees - the branches and the main clones stay. A
branch pushed too early is taken off origin again from a repository's
branches (`b` on its block in the view, `Alt-D` on the branch).

A group grows and shrinks a repository at a time. `a` lists the repositories
not in it yet, filtered as you type; the one picked gets the group's branch,
made from the branch you choose, or checked out when it has it already - a
group without a branch of its own asks which branch to check out. `x` takes
one out: its worktree goes, its branch stays in the repository, and the last
one stays, since `d` deletes the group.

`n` opens a merge request in every repository of the group at once, from the
branch they share. One form takes the title and the description - proposed
from the commits of all of them - and, for each repository, the branch to
merge into: the one its worktree was made from, unless you pick another, or
`(no merge request)` to leave that repository out of this round.
unagit pushes what origin lacks, opens them one by one, and once every address
is known ends each description with a **Related merge requests** list linking
the others. A repository that would need a force push, or has nothing to
merge into its target, is named and left out; the rest go ahead.

A merge request closed or merged on the forge is let go on `R` in Worktrees:
the worktree stays as it is, it just has no merge request any more, and `n`
can open a new one.

Run it again when another repository has caught up: the merge requests
already open stay as they are - their new commits are pushed - only the
missing ones are opened, and every description's list is written afresh,
the earlier merge requests' too. The text above the list is left alone.

**Incomm in a grouped worktree.** An editor or an agent opened on the group's
folder keeps one Incomm store there, for all its repositories: a comment on
`gateway/src/x.go` is a comment on `src/x.go` in the gateway repository.
unagit gives the folder that store when it makes the group, so Incomm does not
settle in some folder above it, and reads it repository by repository: `P` on
a merge request in Merge requests publishes the comments on its repository's
folder, with their paths as the repository names them, and the list counts
them as waiting. In Worktrees, `R` also brings the comments of the merge
requests open from every worktree - `r` those of the one under the cursor - a group's into its one store - and `COM`
counts what Incomm holds, amber while some of it waits to be published.
Opening never waits for comments: on your own work they come second.

`c` commits everything a worktree has not committed - staged or not, new
files and deletions - and in a grouped worktree every repository at once.
One message serves all of them; any repository can be given its own instead.

### How old, how big

Worktrees also says how old each one is and what it takes: `CREATED` is when
it was made, `SIZE` what its files take on disk - a group's summed - with
whatever was built in it, but not the object store it shares with its clone.
Sizes are measured in the background the first time a worktree is seen, and
again on `R`. Nothing is removed for being old or big; that stays your call,
with `d`.

### Keeping worktrees current

A branch unagit makes - in a grouped worktree, or with `n` in the `Ctrl-W`
picker - remembers the branch it was made from (`git config
branch.<name>.unagitBase`). Until it is pushed, Worktrees measures it against
origin's copy of that base: `RMT` says `↓2 behind main`, and `p` rebases it
onto it. Once pushed it has an upstream of its own and follows that instead,
as a clone does in Repositories - a pushed branch is never rebased onto its
base, since that would rewrite what origin has and need a force push, which
unagit does not do. The same rules apply: a fast-forward when nothing local is
in the way, a rebase otherwise, and nothing at all when that would conflict.
`R` fetches first, `Alt-P` updates every worktree.

`Ctrl-R` goes further: it puts the branch - its commits and its uncommitted
edits - on top of its base as it is now, pushed or not, so that it reads as
made from today's base. It is done only when it goes through without a
conflict; otherwise nothing changes. A pushed branch then differs from origin's
copy and its row says **force push required**: `P` asks, then pushes with
`--force-with-lease` set to exactly what origin had before the rebase, so a
commit someone pushed in the meantime makes git refuse instead of being lost.
That is the only force push unagit ever does.

The edits column (`✎`) counts the files with uncommitted changes, so work in progress shows
before it is committed; a grouped worktree adds up its repositories.

`CI` is the newest pipeline of the worktree's branch itself, read on `R` and
`r` as in Repositories, and `J` lists its jobs. A merge request open from the
branch has its own pipeline in the merge request list, and `m` goes there,
with the cursor on it. A grouped worktree has a branch in each repository and
leaves `CI` empty; `J` and `m` ask which one.

`Enter` opens a worktree as a view of its own, in blocks: a block for every
repository - one for a worktree of its own - and, for a grouped one, a block
for the whole group above them. `j`/`k` light a block, and the keys act on the
one lit: on the group, `p` updates every repository, `C` commits everything,
`n` opens the merge requests of all of them, `a` adds a repository; on a
repository, the same keys do it for that one alone, and `w` opens its merge
request in the browser, `c` its conversation, `l` the commits of its branch
(Enter shows one in Hunk), `x` takes it out of the group. Each block is framed,
the lit one in the bright border, and lists under a label each what the branch
was made from and how far that has moved, where it stands against origin,
what is not committed, its merge request and its comments. Git is asked about
every repository at once and the view is drawn when all of them have
answered; until then it says it is reading.

## Integrations

Configure integrations in **Settings → Integrations**. Each integration has
its own card - Zellij included - with its state at the right of its title:
**enabled**, **disabled** or **not installed**. Press `e` to enable or
disable it and `c` to check installation. The cards stand under the kind of
integration they are - Editors, Review, Terminals, AI Agents, Files &
Navigation - three across on a large screen, two on an ordinary one, one on
a narrow one; `h` `j` `k` `l` and the arrows move between them as they are
drawn, `Tab` through all of them in order, and `h` from the first column
goes back to the sections.

### [Incomm](https://github.com/bitbyone/incomm)

Incomm displays merge request comments in your editor. Install `incomm` on
PATH, then enable its integration. When you open a review with `Ctrl-R`,
unagit imports comments before starting the editor.

Imported comments keep the reviewer name and a link to the merge request.
Replies use their thread's file location, and unchanged comments are not
duplicated when you reopen the review. General comments without a file
location are skipped. Comments on deleted lines or missing files are marked
as orphaned and remain visible in Incomm's explorer.

If the import fails, unagit shows the error before opening the editor.

### [Hunk](https://github.com/modem-dev/hunk)

Hunk is a terminal diff viewer made for reviewing a whole changeset. Install
`hunk` on PATH - the integration is on as soon as it is found, and `e` in its
card turns it off. `D` hands it the terminal the way an editor gets it, always
on what the row's working tree has not committed - staged, unstaged and new
files. In a review worktree that is the whole merge request; in a grouped
worktree, every repository in one review. A merge request with nothing on
disk gets its review made first - the repository cloned if need be - so `D`
down the list never stops to ask for `C`.

`Alt-D` in Repositories and Worktrees shows everything since the branch's
base instead: its commits and what is not committed, measured from a
worktree's base or a clone's upstream. One commit at a time is the commit
log's - `Ctrl-L`, then `D` on the commit.

Hunk reads one repository at a time, so for a grouped worktree unagit puts the
changes of its repositories together into one patch, new files included, and
opens that.

### [chezmoi](https://www.chezmoi.io)

chezmoi clones your dotfiles repository into a directory of its own and
applies it from there, so a second clone of it under the root would only
drift apart from the first. With `chezmoi` on PATH the integration is on, and
at every start unagit asks chezmoi for its working tree and origin and pairs
that with a repository of the list.

That repository wears a **↗ Managed by Chezmoi** badge, first in the tags
column. It is not a tag: you cannot put it on or take it off, it is never
written to the configuration, and hiding the tags keeps it. Opening the
repository opens chezmoi's checkout, which counts as cloned. Its branch and
review worktrees and its merge requests work as usual, under the root where
unagit would otherwise have cloned it, not beside chezmoi's directory.

chezmoi's checkout stays chezmoi's: deleting the repository deletes only its
worktrees, switching a server's clone protocol leaves its remote alone, and
`e` cannot move it. Turn the integration off with `e` in its card to clone
the repository like any other.

### [Zellij](https://zellij.dev)

Inside Zellij, the selection action picker (`Alt-Enter` or `Ctrl-A`) offers
**Open in New Tab**, **Open in Vertical Split** (beside unagit), and
**Open in Horizontal Split** (below it). They use the favourite terminal
editor, or ask among terminal editors when the favourite opens a window.
Existing opening keys keep using unagit's own terminal.

A repository is cloned if needed; a merge request gets its branch worktree,
as with `Ctrl-O`. A worktree or the lit block of its view opens its own
directory, and a group opens its folder. Tabs are named after the repository,
merge request, branch or group. Unagit stays usable while the editor runs.
The pane closes when its editor exits.

`unagit sessions` and `unagit cd` include these panes, and Neovim wears the
usual open-editor marker. Records follow the pane in its original Zellij
session, including after unagit exits; a later instance finds them again.
Neovim in a pane is the same Neovim as in unagit's own terminal: it runs
until `:qa`, and **Ctrl-Z** puts it aside - the pane closes, the editor
runs on with its buffers and unsaved changes, and `E` lists it. Opening its
directory again brings it back where you ask: `Ctrl-O` or Enter in `E` in
unagit's terminal, a tab or split in a pane of its own. While it still has
its pane, opening the directory goes to that pane instead of starting a
second Neovim. From a unagit outside that Zellij session the pane cannot be
brought forward, so unagit asks: **Attach Here Too** opens the same editor
in this terminal as well - both windows show the same files and cursor,
sized to the smaller - and **Take Over** puts the pane's window aside, as
Ctrl-Z there would, and opens it here. Taking over is offered only while
the pane is its one window. Closing a pane by hand, rather than with
Ctrl-Z, ends the Neovim in it. Each opening also records a
zoxide visit when that integration is enabled.

Requires Zellij 0.45.1 or newer. No setting is needed; the actions appear
only inside Zellij. tmux is not implemented yet.

### [herdr](https://herdr.dev)

herdr is a terminal multiplexer made for coding agents. Inside it, unagit
treats it as it treats Zellij: **Open in New Tab** and the two splits open
the editor in herdr's tabs and splits, beside unagit, and the records, the
open-editor marker, `E` and Ctrl-Z all work the same.

From anywhere - herdr or a plain terminal - an agent can be started in herdr
(**Open in Claude Code…** and the other agents, below): every one goes to a
workspace called **Unagit Agents**, a tab each, named after the repository,
merge request or branch, and herdr is switched to it. Bringing herdr's
window forward is yours: unagit does not know which key or window shows it.
herdr itself starts the agent, so it follows what the agent is doing.

The integration is on whenever `herdr` is on PATH; `e` in its card turns it
off. Requires herdr 0.9 or newer.

### [Ghostty](https://ghostty.org)

On a Mac, **Open in Ghostty…** opens the favourite terminal editor in a new
Ghostty window or tab, or - when unagit runs in Ghostty itself, not in a
multiplexer inside it - in a split beside unagit; agents can be opened in
the same places. Ghostty is scripted through AppleScript: macOS asks once
whether unagit may control it. Ghostty keeps a terminal whose program has
ended open until a key; unagit closes it instead. A tab cannot be opened
when tabs are turned off in Ghostty's configuration - use a window.

### Coding agents

Claude Code, Codex, Copilot CLI, opencode and Antigravity are integrations of
their own, each on whenever its command (`claude`, `codex`, `copilot`,
`opencode`, `agy`) is on PATH. Each adds **Open in <agent>…** to the actions
of a repository, a merge request and a worktree: the directory is prepared
as `Ctrl-O` prepares it - cloned, the branch worktree made - and then the
agent starts there, where you choose: **This Terminal** (unagit is
suspended until the agent ends), a tab or split of the Zellij or herdr
unagit runs in, a tab of herdr's Unagit Agents workspace, or a Ghostty
window, tab or split. The place chosen last is offered first next time.

**[4] Agents** lists the agents started from unagit - never the others herdr
runs - with what each works on, where it runs, and, for those in herdr, what
it is doing: **waiting** for an answer first, then **working**, **idle**, and
**ended** when the agent left only its shell. The number waiting is on the
tab from every screen. `Enter` goes to the agent: its herdr tab, its Zellij
pane or its Ghostty terminal; with Ghostty on and unagit outside herdr, the
Ghostty terminal herdr runs in comes forward too - a quick terminal included
- found by the title herdr gives it, `<host>: <workspace>`. `d` closes an
agent in herdr after asking, `Ctrl-O` opens its directory in the editor, and
`Alt-A` lists the agents over any screen. In herdr the agent's icon is drawn
from the Nerd Font where the terminal has one.

### [Yazi](https://yazi-rs.github.io)

With `yazi` on PATH, **Browse Files** appears in the selection action picker
(`Alt-Enter` or `Ctrl-A`) on repositories, merge requests, worktrees and the
lit block of the worktree view. Settings › Integrations › Yazi `e` turns it
off. A repository is cloned if needed; a merge request opens its review
worktree first, its branch worktree otherwise, or prepares a review when
neither exists. Groups open their folder.

Yazi gets the terminal until you leave. Choosing a file opens it in the
favourite editor with the original repository as its working directory;
without a usable favourite, unagit asks which editor to use. An existing
Neovim server opens the file in a new tab, keeping unsaved buffers. Quitting
without choosing a file returns to unagit. While browsing, the directory is
listed by `unagit sessions` and `unagit cd`.

With unagit's zoxide integration on, both the starting directory and a
different final directory count as visits. Intermediate visits are Yazi's:
put `require("zoxide"):setup { update_db = true }` in Yazi's `init.lua` to
record them. The Yazi card shows a hint from that file, without running Lua
or changing your configuration.

The [unagit.yazi plugin](contrib/yazi/unagit.yazi/README.md) goes the other
way: jump from Yazi to any unagit directory, or only to open sessions.
Install with `ya pkg add bitbyone/unagit:unagit`, then add
`{ on = ["g", "u"], run = "plugin unagit" }` to `mgr.prepend_keymap` in
`keymap.toml`. `plugin unagit -- sessions` chooses from open sessions.
The plugin requires Yazi 26.9.1 or newer.

### [zoxide](https://github.com/ajeetdsouza/zoxide)

With `zoxide` on PATH the integration is on automatically; `e` in its card
turns it off. Opening an editor, returning to a running editor, or entering
a directory with `unagit go` or `unagit cd` records a visit, including with
`--print`; `unagit attach` records a return to its editor too. Making a clone or worktree alone records nothing. Removing one
forgets its directory, and deleting a group forgets its members and folder.
Zoxide's own `_ZO_EXCLUDE_DIRS` rules apply. A missing or failing zoxide never
prevents an editor or shell from opening.

Choose **by frecency** in `o`, the sort picker, in Repositories or Worktrees
for frequent and recent visits first. A repository takes the highest score
of its clone and worktrees, including the older layout and grouped members.
Unknown directories follow known ones; equal scores and unknown rows use
activity. While a list is in that order, the scores are read on each tab
switch and kept until the next one; otherwise zoxide is not asked. `unagit go` offers the most visited directories first
when the integration is on. The card shows how many directories zoxide
knows under your configured roots; `c` checks installation and reads it again.

## Reviewing merge requests

Press `Ctrl-R` on a merge request and unagit builds a worktree where

```
HEAD = the merge base     index = the merge base     files = the merge request
```

so the whole merge request reads as **one pending, unstaged change**:

```sh
git diff        # the entire merge request, as a single diff
git status      # every file it touches, additions and deletions included
```

`C` builds the same worktree without opening anything, for a review to read
later, or in Hunk with `D`.

Unstaged is the point. Editors draw their gutter by comparing the file against
the index, so the index has to be the merge base - then gitsigns, gitgutter,
`]c`, `:Gvdiffsplit` and `:DiffviewOpen` all work on the merge request as a
whole, with nothing to configure. Files the merge request adds go in as
intent-to-add, so they read as new files instead of vanishing into untracked.

The base is the one GitLab itself diffs against (`diff_refs.base_sha`), not the
tip of the target branch - otherwise a target that moved on would show its own
commits backwards in your diff.

Notes you type into the files survive reopening, and a force push on the other
side is picked up on the next review. `Ctrl-O` gives you the ordinary branch
checkout instead, when you mean to commit and push; it opens as it is, and
`p` brings it to the merge request's head.

When the author answers your comments in new commits, `Ctrl-L` lists the
commits with the ones you have not reviewed marked `●` and the cursor on the
oldest of them - a rebase does not fool it - and `Ctrl-R` there reopens the
review with only that commit and what follows pending. The list comes from
the server, so a repository not cloned yet is cloned only after you pick a
commit.

The same commit log opens on a repository and a worktree too, and what it
offers follows from where it was opened. `Enter` shows a commit's detail -
author, date, branches and tags pointing at it, the whole message, the files
it changed - and `Esc` comes back to the list. `D` shows the commit in Hunk,
`Alt-D` everything from it to the working tree; a commit not on disk yet is
brought first - the repository cloned, the merge request fetched - so a log
can be paged through without stopping to clone. In a clone or a worktree `C`
checks the commit out with a detached HEAD: the branch column then shows
`@<commit>` and the RMT column how far behind the branch it left it is,
and `B` - or `b` and a branch - goes back. `n` starts a branch at the commit,
`Ctrl-W` a worktree of its own, for an old state without moving the clone.
`w` opens the commit on the server, and `y` copies its id, its link, a
markdown link, or a link with text for a chat: repository, branch, commit
and subject followed by the link. `y` on a merge request or a repository
offers the same kind of line.

A detached HEAD is a checkout with no branch: you can commit there, but the
commits belong to no branch, and unagit will not push them - make a branch
with `n` first.

A link pasted from chat or email goes straight there:

```sh
unagit review https://gitlab.example.com/group/app/-/merge_requests/12
unagit open   https://github.com/owner/repo/pull/12     # the branch worktree
```

It asks for the passphrase as usual, opens the editor, and leaves you in the
lists when you close it. The merge request does not have to be in a selected
group.

### Finishing a review

The rest of a merge request's life is a key away too, on GitLab and GitHub
alike:

- `A` approves, after asking - everyone on it will see it.
- `M` merges. The dialog lists first what the list knows to stand in the way
  - a draft, a pipeline failed or still running, approvals missing, threads
  not resolved - and then asks whether to squash and whether to delete the
  source branch. While the pipeline runs it offers to wait for it instead
  (auto-merge on GitHub, where the repository has to allow it). The merge is
  of the head the list has seen: a push since then is refused rather than
  merged unread, and unagit says to look first.
- `Ctrl-D` marks a merge request as a draft, or a draft as ready.
- `a` lists who can be asked to review - the repository's members on GitLab,
  those with push access on GitHub - with the reviewers asked marked;
  `space` asks or withdraws, and `Esc` saves the choice.
- **Close Merge Request…** (in `Alt-Enter`, no key of its own) closes it
  without merging, after asking. Its branch stays.

After each the row is asked about again, so the list shows what the server
now has.

## The rest of it

- **One list, several servers.** Any number of GitLab instances plus GitHub,
  each with its own token, refreshed in parallel. Groups are picked with a
  granularity: a group's own repositories, or the whole tree below it.
- **A worktree per merge request**, so three half-finished reviews can sit on
  disk at once without committing anything - and they cost a checkout, not a
  clone, because they share the repository's object store.
- **Detail column** on `Enter` that follows the cursor as you move, with
  everything the API knows: reviewers, approvals, pipeline, labels, commits,
  how far behind the target it is.
- **Comments as markdown**, threaded the way they were written. `c` opens the
  conversation, `i` replies, `A` approves (it asks first - everyone sees it).
- **Filters that stick**: cloned-only, hidden repositories, sort order,
  repositories grouped by their group or subgroup and merge requests by their
  repository, plus a fuzzy filter on everything.
- **An order for each list.** `o` sorts the list on screen, and each list
  keeps its own: every one by activity or by name; Repositories also by
  edits, by size, or by remote - the furthest behind origin first, then any
  other clone out of step with it (diverged, unpushed, no upstream, a failed
  fetch), then the rest by activity; Merge requests by new commits since your
  last review, or by comments - open threads first, then all of them;
  Worktrees by edits. Repositories and Worktrees also by frecency, using
  zoxide visits. Rows that tie stay in the order of their activity.
- **Tags of your own.** Settings › Tags holds them - `oss`, `personal`, `work`,
  `private`, `fork`, `hobby` and `tooling` to start with - each a light ink on
  a deep fill, in one of sixteen colours. `Ctrl-T` in Repositories puts them on
  a repository; `t` in Settings › Groups & roots puts them on a whole server
  or on a group, and everything below wears them too unless it takes one off.
  They have a column of their own after the names. `f` shows only the
  repositories wearing all of the tags you pick, so each tag more narrows the
  list further; `F` shows every repository again, and `v` hides the column. The pills end rounded, which needs a Nerd Font; `s` in Settings ›
  Tags switches to half circles or square ends.
- **Favourites.** `Ctrl-F` stars a repository or a merge request. A flat list
  shows the favourites first, in the usual order, above a line; a grouped one
  only marks them with `★`. Favourites first can be turned off in the order
  picker (`o`). A starred merge request is forgotten once a refresh no longer
  lists it, merged or closed.
- **Instant startup.** The lists are cached on disk and only refreshed when you
  ask; nothing hits the API behind your back.
- **Your tokens are encrypted** with Argon2id + AES-256-GCM and exist in
  plaintext only in memory, for as long as unagit runs. The passphrase is asked
  for in a dialog on every start and is never read from a flag, an environment
  variable or a pipe. git gets the token through a one-shot credential helper,
  so it never reaches `.git/config` or a remote URL - or you can clone over SSH
  and keep the token for the API alone. On macOS, Settings › Security › `k`
  can remember the passphrase in the Keychain instead, where only the unagit
  binary may read it: unagit then opens without asking, and anything else
  that wants it gets a macOS dialog. After a rebuild macOS asks once whether
  the new binary may.

## Themes

Everything unagit draws with is a theme: the colours of text, borders,
fields, the selection and every state, the screen's background, and the
glyphs that say what something is (`○ ● ◐ ◉`, `◆`, `★`, `✓ ✗`, `↑ ↓`, the
borders). Settings › Theme lists them with a strip of their colours; `Enter`
puts one on at once and remembers it. From any screen, `:` › Switch Theme…
lists them too, the one on under the cursor - over a dialog as well, where
`:` offers what can be done from anywhere and `Alt-Enter` on a list what
can be done with its item. Over a main screen that list tries each theme as
the cursor comes to it: the screen behind is drawn in it, undimmed, the list
on a darker background of its own, and only `Enter` keeps it - `Esc` puts
back the one that was on.

`b` in Settings › Theme leaves the terminal's own background under every
theme, so a translucent or blurred terminal shows through; every other
colour - text, borders, fields, the selection - still comes from the theme,
which then reads best on a terminal background close to its own. It is kept
in `config.yaml` as `terminal_background`.

Some glyphs have an icon from a [Nerd Font](https://www.nerdfonts.com) as
well - a theme gives them under `nerd_glyphs`, a plain character standing
in under `glyphs` - and they are drawn when the terminal can: Ghostty,
WezTerm and kitty bring the icons with them, and iTerm2 and Alacritty are
asked for their font. `n` in Settings › Theme turns them on or off when
that guess is wrong, and back to guessing. A few are icons only, with nothing in their
place without a Nerd Font: the server's before a repository's name in the
lists, and the one after a worktree's count of repositories, a draft's icon in place
of the word - and of the "Draft:" its title starts with - and an icon,
muted, before each action in the action pickers, where a theme gives one
under `action_icons` by the action's name. They are drawn, never copied:
`y` and `/` see the name alone.

unagit comes with **unagit**, the muted default, which keeps the terminal's
own background so it sits quietly beside an editor, and twenty-eight that
paint a background of their own: **retro-block**, after the
[Retro Block](https://github.com/bitbyone/retro-block-theme) IntelliJ
theme, and the popular editor colour schemes - dark **ayu-mirage**,
**carbonfox**, **catppuccin-mocha**, **cyberdream**, **dracula**,
**everforest-dark**, **github-dark-dimmed**, **gruvbox-dark**,
**gruvbox-material-dark**, **kanagawa-dragon**, **kanagawa-wave**,
**monokai-pro**, **moonfly**, **nightfly**, **nord**, **onedark**,
**rose-pine**, **solarized-dark**, **sonokai** and **tokyonight-night**;
light **catppuccin-latte**, **everforest-light**, **github-light**,
**gruvbox-light**, **rose-pine-dawn**, **solarized-light** and
**tokyonight-day**.

A theme with a background of its own does not have to colour the tags'
pills: those it leaves out under `tags` are worked out of its colours -
the fill a step off its background and tinted by it, the ink as
colourful and as light as its accents, held back a little so the pills sit
in the background rather than on it, and far enough apart to read (4:1) -
so all sixteen look alike in weight and belong to the theme.

To tune one, `f` forks it: the theme under the cursor is written to
`~/.config/unagit/themes/<name>.json` with every colour and glyph spelled
out, and put on. Edit the file with unagit open beside it - each save puts
the change on at once. A save that breaks the file keeps the last good
version on and says which key is wrong; `r` reads the folder again by hand.

Your own go in `~/.config/unagit/themes/*.json`. A theme names only what it
changes and takes the rest from the one it `extends` - the default when it
says nothing:

```json
{
  "name": "midnight",
  "description": "gruvbox with a blue accent and hearts for favourites",
  "extends": "gruvbox-dark",
  "background": "#101418",
  "text": { "accent": "#7aa2f7" },
  "glyphs": { "favourite": "♥" }
}
```

A colour is `"default"` (the terminal's own), `"#rrggbb"`, a number of the
256-colour palette (`"109"`) or a colour name. Every key there is, with what
it is for, is in [`internal/ui/themes/unagit.json`](internal/ui/themes/unagit.json)
and described in [`internal/ui/themes.go`](internal/ui/themes.go): `text`,
`state`, `border`, `tabs`, `surface`, `selection`, `backdrop` (the dimming
behind a dialog), `markdown`, `chezmoi`, `tags` (each tag colour's ink and
fill), `glyphs` and `borders`. A file that cannot be used - a colour that is
not one, a glyph of two characters, a theme that extends itself - is listed
in the section with what is wrong, and the others still load.

Anything else drawn has a colour of its own under `colours`, by name -
`repositories.mr`, `worktrees.size`, `merge_requests.author` - and each
falls back on a more general one when the theme leaves it out: a list's
column on the column of every list (`column.mr`, `column.age`), that on a
base colour (`text.accent`, `text.muted`). So a theme changes a few base
colours and everything follows, and names only what it wants set apart.
Every name and what it falls back on is in
[`internal/ui/themeroles.go`](internal/ui/themeroles.go). `heat` is two
colours, the coldest and the hottest, which sizes are drawn between - from
the least a repository or a worktree takes to the most, on a logarithmic
scale - in sixteen shades worked out between them, as faded as the ends.

## Editors

Everything that opens a directory - `Ctrl-O`, `Ctrl-R` -
opens it in your favourite editor. Hold Alt with the same key (`Alt-O`,
`Alt-R`, ...) and unagit asks which one first. Settings › Integrations has
a card for each editor - Neovim, IntelliJ IDEA, VS Code, Zed and a custom
one - saying whether it was found: a launcher on `PATH` or in JetBrains
Toolbox's scripts folder first, then, on macOS, the application in
`/Applications`. `e` turns an editor off, and it is then offered nowhere;
`f` makes it the favourite, starred in its title, and `f` on the favourite
leaves none. Without a favourite that is installed and on, every open asks,
the way Alt does. A command of your own is the Custom card's: `o` sets its
command, its arguments and whether it opens a window.

Neovim takes the terminal. With Neovim 0.12 or newer, **Ctrl-Z** (or
`:detach`) puts it aside and returns to unagit, keeping the buffers and
unsaved changes. **E** on a main list, or **Running Editors…** in `:`, lists
these editors: Enter attaches, `x` closes. Inside Zellij or herdr, or with
Ghostty on, Enter first asks where - this terminal, or a tab, split or
window - starting on the place chosen most often, so Enter Enter goes
there again. With unsaved changes, closing
attaches first and asks about saving in Neovim. The list shows the repository,
branch or merge request, directory, age and unsaved changes (`?` when the
editor is too busy to answer). `▣` beside a row marks its directory open in
Neovim; Nerd Fonts use an icon, and `v` can hide the marks column.
Opening the same directory in Neovim again attaches to it. Before it is
shown, Neovim reads again whatever changed on disk while it was aside - a
review reset for a force push, a branch pulled - so its buffers and gutter
are the files as they are. Alt-O still lets you choose a different editor.

Quitting unagit leaves the editors aside running. The next instance finds
them, and `unagit attach [query]` returns to one from a shell without opening
the main interface. `unagit sessions` and `unagit cd` include them too. Nothing
closes an editor because of its age. The sessions directory is private
(0700), since its sockets give access to the editor. A very long config path
needs a shorter `UNAGIT_CONFIG_DIR` to fit macOS's Unix socket limit.

Older Neovim and custom terminal editors keep the terminal until they quit.
Window editors open their own window and unagit carries on; `unagit cd` knows
about them until unagit exits. On macOS, Alt needs the terminal to send Option
as Meta (iTerm2: Profiles › Keys › Left Option key › Esc+).

## Follow it into another terminal

Opening an editor does not end unagit: it suspends itself and waits, so for as
long as something is open it knows where. Another window can go there:

```sh
unagit cd            # asks which, when more than one is open
unagit cd calling    # a search narrows it; one match needs no asking
unagit sessions      # what is open, including editors left aside
unagit attach calling # return to a Neovim left aside
```

`unagit cd` starts a shell in that directory and leaving it puts you back,
the way `chezmoi cd` does. `unagit cd --print` writes just the path, for
`ug() { cd "$(unagit cd --print "$@")" || return; }`.

`unagit go` does the same for anything on disk, open or not: a clone, a
merge request's review or branch worktree, another worktree, a grouped
worktree. It needs no passphrase - it reads unagit's index and the disk.

```sh
unagit go                  # everything on disk; / narrows the list
unagit go gateway review   # every word has to match; one match needs no asking
unagit go --print '!42'    # the path, for ugo() { cd "$(unagit go --print "$@")"; }
```

## Keys

Every form works like the lists: in NORMAL, `j`/`k` (or Tab) move from field
to field and over the buttons, the field with the focus lit in an accent, and
a button is pressed by its letter, lit in its label; `i` or Enter types into a
field and `Esc` stops typing. A form that starts with a text field opens
typing into it; while typing, `Esc` and then the letter press a button.

| Key | |
| --- | --- |
| `Alt-Enter` `Ctrl-A` | every action on the row, the selection or the lit block, with its key |
| `:` | every action of the screen, with its key |
| `1` `2` `3` `4` `5` | Repositories · Merge requests · Worktrees · Agents · Settings |
| `Alt-A` | running agents: Enter goes to one |
| `/` `Esc` | fuzzy filter · leave it, clear it, close the detail |
| `Enter` | detail column, and jump into it |
| `e` | in Repositories: set the exact destination before cloning; blank restores inherited roots |
| `C` | clone without opening the editor; a merge request's review worktree |
| `Ctrl-O` | open the editor as it is on disk; clones only what is missing |
| `E` | running Neovims: Enter attaches (asking where), x closes |
| `Ctrl-Z` in Neovim | put it aside and return to unagit (0.12+) |
| `p` | update: a fast-forward, or a rebase of your work; never a conflict |
| `Alt-O` `Alt-R` … | the same, in an editor you choose |
| `Ctrl-R` | open a merge request for review - the change as pending edits |
| `y` | copy the link, reference, branch or directory |
| `Ctrl-L` | commit log: the clone's branch, a merge request's commits, a worktree's branch; `Enter` details |
| `D` `Alt-D` `C` `n` `Ctrl-W` | in the log: diff · diff since · check out · branch · worktree at the commit |
| `Ctrl-R` | in a merge request's log: review from the commit to the head |
| `B` | back to the branch a commit was checked out from |
| `c` `A` | read and write comments · approve |
| `M` `Ctrl-D` `a` | in Merge requests: merge · draft or ready · reviewers |
| `D` `Alt-D` | in Hunk: what is not committed (a review: the whole merge request) · since the base |
| `p` `Alt-P` | in Repositories: pull or rebase onto origin · every clone at once |
| `space` `Ctrl-W` | in Repositories: select several · one grouped worktree of them |
| `b` `m` `f` | branches · merge requests of this repo · limit to a repo |
| `n` | in branches: a new branch from the one under the cursor |
| `d` `D` `Alt-D` | in branches: delete in the clone · everywhere · on origin |
| `L` `x` `X` `o` `Ctrl-G` | cloned only · hide · hidden list · order · group the list |
| `H` `v` | in Merge requests: hide the author's merge requests · view options, the hidden authors |
| `Ctrl-F` | star or unstar a favourite |
| `Ctrl-T` `f` `F` | in Repositories: tag · show only some tags · every tag again |
| `v` | what the list shows - grouping, favourites first, and in every list which columns |
| `d` `w` | delete from disk · open in the browser |
| `r` `R` | refresh the row under the cursor · the whole list |
| `?` `q` | help · quit |

On-disk markers: `○` nothing, `●` branch worktree, `◐` review worktree, `◉`
both, `⊘` hidden.

## Where unagit keeps its own things

```
~/.config/unagit/config.yaml      written by the Settings tab
~/.config/unagit/tokens.enc       sealed with your passphrase
~/.config/unagit/index-*.json     the cached lists
~/.config/unagit/sessions/        what is open in an editor right now
~/.config/unagit/themes/*.json    themes of your own
```

Working on unagit itself? [AGENTS.md](AGENTS.md) has the internals.
The test commands and measured costs are in [docs/testing.md](docs/testing.md).

Press `?` in a main view for contextual help: relevant shortcuts appear first
in normal text, and shortcuts for other contexts remain dimmed. Modals and
simple blocks with up to five actions keep dim inline hints at the bottom.
Confirmation dialogs accept the action letter directly (`c` cancels, `d` deletes).
In forms, use `Alt` plus the hinted letter while typing, or the letter alone
when a button has focus.
