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
 Repositories [R] │ Merge requests [M] │ Settings [S]
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

Nothing to edit by hand. In **Settings [S]** you add servers (`a`), paste a
token, pick the groups you work with (`space`) and say where they should be
cloned (`d`). GitLab wants a token with the `api` scope; GitHub wants `repo`
**and `read:org`** - without the latter GitHub answers the organisation listing
with an empty array rather than an error, so your orgs simply would not show up.
`v` checks a token against the server before you trust it.

Requirements: Go 1.26+, `git`, and an editor (`nvim` by default).

## Navigating

`R` lists every repository across every server and group you picked. `○ ● ◐ ◉`
say what is on disk, and the path column says where - which matters once
different groups live in different places.

- `C` clones it to disk without opening the editor. `Ctrl-C` is left to end
  unagit, as it ends any program in a terminal.
- `Ctrl-O` opens your editor in it at once, as it is on disk - no fetch, no
  pull. Only a repository not cloned yet is cloned first. The lists say where
  everything stands, and `p` updates it first when you want that. The same
  goes for `Ctrl-O` in Merge requests and Worktrees.
- `b` lists every branch in a searchable modal and switches it **in that same
  clone**, so there is one working copy per repository, not a directory per
  branch.
- `Ctrl-W` gives a branch a worktree of its own instead (`n` for a new
  branch). Nothing opens: unagit moves to Worktrees with the cursor on it, and
  `Ctrl-O` opens it when you want.
- `REMOTE` says where the clone's branch stands against origin: `✓` up to
  date, `↓3` behind, `↑2` commits of yours not pushed, `↑2↓3` both. It is read
  from the refs on disk, so it costs nothing, and read again whenever you
  switch to a list or come back to the terminal from another window - an edit
  made in your editor shows up without asking. `r` fetches every clone in the
  background first, and the header counts the fetches still running. A
  rebase or merge git stopped in the middle of shows there instead, in red.
- `EDITS` counts the files not committed in the clone, and `Enter` lists them
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
says `SELECT 3`, `Esc` clears it); `Ctrl-W` on a selection then asks for:

- **New branch** - made in every repository, from the branch picked for it
  below. Left empty, nothing is created and each repository checks out the
  branch picked for it.
- **Folder** - the name of the folder; it follows the branch until you change it.
- **A branch for each repository** - where the new branch starts, or, without
  one, the branch that repository checks out.

```
<root>/.unagit/groups/<folder>/<repo>     a worktree of each repository
<root>/.unagit/groups/<folder>/.unagit-group.json
```

Git checks a branch out only once, so a branch already checked out - usually
the default one, in the main clone - is marked so in the list and cannot be
picked without a new branch: give the group one, or switch that checkout away.
Every repository is cloned, fetched and checked before anything is made, so a
group is made whole or not at all. Once made, Worktrees shows it with the
cursor on it.

In Worktrees the group is one row marked `◆`, `REPOS` says how many it holds,
and `Enter` lists each repository with its branch, its remote and its state.
`Ctrl-O` opens the folder, `p` updates every repository in it (see below), `P`
pushes every repository that needs it, and `d` deletes the worktrees - the
branches and the main clones stay.

`n` opens a merge request in every repository of the group at once, from the
branch they share. One form takes the title and the description - proposed
from the commits of all of them - and, for each repository, the branch to
merge into: the one its worktree was made from, unless you pick another.
unagit pushes what origin lacks, opens them one by one, and once every address
is known ends each description with a **Related merge requests** list linking
the others. A repository that would need a force push, or has nothing to
merge into its target, is named and left out; the rest go ahead.

Run it again when another repository has caught up: the merge requests
already open stay as they are - their new commits are pushed - only the
missing ones are opened, and every description's list is written afresh,
the earlier merge requests' too. The text above the list is left alone.

`c` commits everything a worktree has not committed - staged or not, new
files and deletions - and in a grouped worktree every repository at once.
One message serves all of them; any repository can be given its own instead.

### Keeping worktrees current

A branch unagit makes - in a grouped worktree, or with `n` in the `Ctrl-W`
picker - remembers the branch it was made from (`git config
branch.<name>.unagitBase`). Until it is pushed, Worktrees measures it against
origin's copy of that base: `REMOTE` says `↓2 behind main`, and `p` rebases it
onto it. Once pushed it has an upstream of its own and follows that instead,
as a clone does in Repositories - a pushed branch is never rebased onto its
base, since that would rewrite what origin has and need a force push, which
unagit does not do. The same rules apply: a fast-forward when nothing local is
in the way, a rebase otherwise, and nothing at all when that would conflict.
`r` fetches first, `Alt-P` updates every worktree.

`Ctrl-R` goes further: it puts the branch - its commits and its uncommitted
edits - on top of its base as it is now, pushed or not, so that it reads as
made from today's base. It is done only when it goes through without a
conflict; otherwise nothing changes. A pushed branch then differs from origin's
copy and its row says **force push required**: `P` asks, then pushes with
`--force-with-lease` set to exactly what origin had before the rebase, so a
commit someone pushed in the meantime makes git refuse instead of being lost.
That is the only force push unagit ever does.

`EDITS` counts the files with uncommitted changes, so work in progress shows
before it is committed; a grouped worktree adds up its repositories.

`Enter` shows what there is to know before deciding anything: what the branch
was made from and how far that has moved, its own commits, what is new on the
base, the files it changes against it, what is not committed, what is not on
origin yet, and the merge request.

## Integrations

Configure integrations in **Settings → Integrations**. Each integration has
its own card: press `e` to enable or disable it and `c` to check installation.

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
worktree, every repository in one review.

`Alt-D` asks first, in a list:

| Choice | What Hunk shows |
| --- | --- |
| Not committed | the same as `D` |
| Since `origin/<base>` | the branch's commits since its base, and what is not committed: a worktree's base, a clone's upstream, a merge request's target |
| a commit | that commit alone - the ones since the base, or the latest without one; a review lists the merge request's |

Hunk reads one repository at a time, so for a grouped worktree unagit puts the
changes of its repositories together into one patch, new files included, and
opens that.

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

When the author answers your comments in new commits, `v` lists the commits
with the cursor on the first one you have not reviewed yet - a rebase does not
fool it - and `Enter` reopens the review with only that commit and what follows
pending. The list comes from the server, so a repository not cloned yet is
cloned only after you pick a commit.

A link pasted from chat or email goes straight there:

```sh
unagit review https://gitlab.example.com/group/app/-/merge_requests/12
unagit open   https://github.com/owner/repo/pull/12     # the branch worktree
```

It asks for the passphrase as usual, opens the editor, and leaves you in the
lists when you close it. The merge request does not have to be in a selected
group.

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
  conversation, `i` replies, `a` approves (it asks first - everyone sees it).
- **Filters that stick**: cloned-only, hidden repositories, sort order,
  repositories grouped by their group or subgroup and merge requests by their
  repository, plus a fuzzy filter on everything.
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

## Editors

Everything that opens a directory - `Ctrl-O`, `Ctrl-R`, `v`, `b` -
opens it in your favourite editor. Hold Alt with the same key (`Alt-O`,
`Alt-R`, ...) and unagit asks which one first. Settings › Integrations ›
Editors lists the ones it found - Neovim, IntelliJ IDEA, VS Code and Zed: a
launcher on `PATH` or in JetBrains Toolbox's scripts folder first, then, on
macOS, the application in `/Applications` - and `f` picks the favourite, or
none. Without a favourite that is installed, every open asks, the way Alt
does. A command of your own goes in Settings › General as the custom editor.

Neovim takes the terminal: unagit steps aside until you quit it. The others
open a window of their own and unagit carries on; `unagit cd` knows about them
until unagit exits. On macOS, Alt needs the terminal to send Option as Meta
(iTerm2: Profiles › Keys › Left Option key › Esc+).

## Follow it into another terminal

Opening an editor does not end unagit: it suspends itself and waits, so for as
long as something is open it knows where. Another window can go there:

```sh
unagit cd            # asks which, when more than one is open
unagit cd calling    # a search narrows it; one match needs no asking
unagit sessions      # what is open, for scripts
```

`unagit cd` starts a shell in that directory and leaving it puts you back,
the way `chezmoi cd` does. `unagit cd --print` writes just the path, for
`ug() { cd "$(unagit cd --print "$@")" || return; }`.

## Keys

| Key | |
| --- | --- |
| `R` `M` `S` | Repositories · Merge requests · Settings |
| `/` `Esc` | fuzzy filter · leave it, clear it, close the detail |
| `Enter` | detail column, and jump into it |
| `e` | in Repositories: set the exact destination before cloning; blank restores inherited roots |
| `C` | clone without opening the editor; a merge request's review worktree |
| `Ctrl-O` | open the editor as it is on disk; clones only what is missing |
| `p` | update: a fast-forward, or a rebase of your work; never a conflict |
| `Alt-O` `Alt-R` … | the same, in an editor you choose |
| `Ctrl-R` | open a merge request for review - the change as pending edits |
| `v` | review from a chosen commit to the head |
| `y` | copy the link, reference, branch or directory |
| `c` `a` | read and write comments · approve |
| `D` `Alt-D` | in Hunk: what is not committed · or since the base, or a commit |
| `p` `Alt-P` | in Repositories: pull or rebase onto origin · every clone at once |
| `space` `Ctrl-W` | in Repositories: select several · one grouped worktree of them |
| `b` `m` `f` | branch picker · merge requests of this repo · limit to a repo |
| `L` `x` `X` `o` `Ctrl-G` | cloned only · hide · hidden list · order · group the list |
| `Ctrl-F` | star or unstar a favourite |
| `Ctrl-T` `f` `F` | in Repositories: tag · show only some tags · every tag again |
| `v` | in Repositories: what the list shows - tags, grouping, favourites first |
| `d` `w` `r` | delete from disk · open in the browser · refresh |
| `?` `q` | help · quit |

On-disk markers: `○` nothing, `●` branch worktree, `◐` review worktree, `◉`
both, `⊘` hidden.

## Where unagit keeps its own things

```
~/.config/unagit/config.yaml      written by the Settings tab
~/.config/unagit/tokens.enc       sealed with your passphrase
~/.config/unagit/index-*.json     the cached lists
~/.config/unagit/sessions/        what is open in an editor right now
```

Working on unagit itself? [AGENTS.md](AGENTS.md) has the internals.

Press `?` in a main view for contextual help: relevant shortcuts appear first
in normal text, and shortcuts for other contexts remain dimmed. Modals and
simple blocks with up to five actions keep dim inline hints at the bottom.
Confirmation dialogs accept the action letter directly (`c` cancels, `d` deletes).
In forms, use `Alt` plus the hinted letter while typing, or the letter alone
when a button has focus.
