# Changelog

What each release brings, newest first. Breaking changes come first in a
release and say what to do. Every release's archives are on
[GitHub](https://github.com/bitbyone/unagit/releases); `brew upgrade
unagit` or `scoop update unagit` takes the latest. Nightly builds
(`unagit-nightly`) bring the next release's changes early and are not
listed here.

## 0.5 <Badge type="tip" text="2026-11-24" />

Merge request comments read, written and answered in unagit, beside their
code, and unagit in Rex.

### New

- **Comments beside their code.** `c` on a merge request opens the
  conversation: every comment a card with its author on the frame, a
  thread on code under the lines of the diff it was written on. `j` / `k`
  go a comment at a time, `r` replies in the thread, `x` resolves it or
  opens it again, `h` hides the code. A GitHub comment outside a thread is
  answered by quoting it. [Finishing a Review](/guide/finishing#comments)
- **The whole file of a comment.** `Enter` on a comment expands its file at
  the commented line, with every comment under its line and a line cursor:
  `c` there writes a new comment, published at once or saved in unagit and
  published later with the rest by `P`. With Incomm on, a saved comment
  goes into the worktree's Incomm store.
- **Long comments read through and copied.** A comment longer than 15 rows
  is clipped; `v` puts a cursor in it to read it to the end, select with
  `v` / `V` or the mouse, and copy with `y` (`Y` copies it whole).
- **Rex.** Open with, Review with and a sandbox task's Resume offer Rex:
  a tab of unagit's own session, or from a Rex terminal a tab or split
  beside unagit. A task's agent is found in Rex whoever started it, and a
  task offers its own agent first when resumed.
  [Zellij, herdr, Ghostty, Rex](/guide/multiplexers#rex)
- **Watch Local Changes.** A clone or worktree can be watched on disk: new
  commits, branches and tags an agent makes there show on Activity,
  quietly (no toast), and never unagit's own. [Watching and Activity](/guide/watching)
- **Only the clones out of step.** View Options keeps only the clones with
  commits to push, or only those behind origin; Toggle Out of Sync Only
  turns both on at once. [The Lists](/guide/navigating)
- What other tools report to a pipeline (Atlantis's plan and apply on
  GitLab, a commit's statuses on GitHub) is listed with its jobs.

### Improvements

- Moving through a long file or conversation is instant: unagit sends the
  terminal only what changed, not the whole screen, so held `j` / `k` keep
  up even in a large terminal.
- A file opens with its comment in the middle of the view, the code above
  it in sight; `j` / `k` scroll a row at a time past the edge, `Ctrl-D` /
  `Ctrl-U` a page.
- The terminal's tab is named after where unagit is: the screen, the
  worktree open, or standalone's repository and branch.
- The right button on a screen's tab opens its actions, a click opens a
  dialog's tab, and a double click on a panel of text is its `Enter`.
- `k` leaves a panel with nothing in it.
- The theme previews have a pane of their own, and themes can be tried over
  an open dialog.
- YAML and JSON in a file have their keys and values coloured.
- The sandbox's actions work on marked repositories.

### Fixes

- The first unlock on a fresh install no longer fails with "no such file
  or directory".
- The spinner turns only for what you started, and new commits are counted
  only when a branch's head really moved.

## 0.4 <Badge type="info" text="2026-11-09" />

Windows, keys of your own, the mouse, and who commits per repository.

### Breaking changes

- **Many default keys changed**, so that every action has a key the
  terminals leave alone: no Alt (a Mac's terminal never sends it), and no
  Ctrl key that Zellij, tmux or VS Code keep. The selection's actions are
  `.`, open is `o`, the log `L`, changes `s`, tags `t`, star `*`, grouping
  `z`, since the base `B`, pull everything `U`; in the log `d` diffs a
  commit and `D` from it to now. `Ctrl-O` and `Alt-Enter` still work. The
  help (`?`) lists the keys, and `keybinds.toml` gives any action another.
  [Keys](/reference/keys)

### New

- **Windows.** unagit builds and runs on Windows, with archives for amd64
  and arm64 and a Scoop bucket. [Getting Started](/guide/#scoop-windows)
- **Your own keys.** `keybinds.toml` gives any action another key;
  Settings › Keys writes it with every action commented out, and a newer
  release brings an older one up to date. The help and every message name
  the keys you bound. [Actions and Keys](/guide/actions)
- **The mouse.** A click does what the keys do: it moves the cursor,
  chooses a card, goes to a panel or a tab, and closes a dialog when it
  lands beside it; the wheel scrolls lists and pickers.
  [Actions and Keys](/guide/actions#the-mouse)
- **Who commits.** A git name and email in Settings, on a server, a group
  or a repository, each inherited from the nearest level that sets it, is
  written into every clone's own configuration, so editors and agents in
  it commit as that person. [First Setup](/guide/setup#who-commits)
- **Nightly builds.** `unagit-nightly` from Homebrew or Scoop brings what
  comes next early. [Getting Started](/guide/#nightly)
- **Your Unibox configuration** in the Sandboxes tab, a tab per kind, and
  Unibox Init sets unibox up from a repository.
  [Sandboxes](/guide/sandboxes)
- A sandbox task starts with as many repositories as it needs, from
  anywhere, and the tab says what the sandboxes lack before a run finds
  out.

### Improvements

- A GitHub branch's pipelines are its workflow runs, with their history,
  and a list of pipelines follows one under way.
- A sandbox's ports are coloured by whether they are on.
- A task's agent comes before its name, its repositories at the right edge.
- `.` and `:` typed into a field are text, not the action pickers.
- What unagit opens in Ghostty gets unagit's PATH.

### Fixes

- A renamed file in a review is pending as the addition it is.
- A GitHub branch stays pending while a workflow run of its head waits.
- The cursor stays at the top of a list until you move it.
- A failed read of the watches or of zoxide's scores is tried again.

## 0.3 <Badge type="info" text="2026-11-02" />

Sandboxes from unibox, one repository alone, and this website.

### New

- **Sandboxes.** A tab of their own for unibox 2: envs, sandboxes and their
  tasks, with tokens kept by unagit and handed out while it is unlocked.
  An agent starts in a sandbox on a merge request or a repository not even
  cloned, and unagit sets unibox up step by step from nothing.
  [Sandboxes](/guide/sandboxes)
- **One repository alone.** `unagit -s [DIR]` opens one repository with
  git and nothing else: no server, no passphrase.
  [One Repository Alone](/guide/standalone)
- **Smart Checkout.** Switching a branch leaves uncommitted changes to git,
  as git does; when the branch's own version of a file is in the way,
  Smart Checkout shelves, switches and unshelves.
  [Branches](/guide/branches)
- **Push up to a commit.** `P` on a commit not pushed yet in the log sends
  it and what is below it, leaving the commits above it here.
  [The Commit Log](/guide/commit-log)
- **Move Changes to New Worktree** takes what is not committed in the clone
  to a new worktree in one step.
- A merge request opens from the clone's branch, as from a worktree's.
- The website, with the guide, the tutorials and this reference.

### Improvements

- Without a base set, origin's default branch stands in for it.
- A slow Neovim is asked its version again rather than taken as absent,
  and a detached Neovim keeps off the terminal it was started in.

### Fixes

- A push is asked about from where the branch stands now, not from the last
  read.
- A dialog keeps what it was told when two of its waits overlap.

## 0.2 <Badge type="info" text="2026-10-16" />

### Fixes

- The EDITS column counts the files of a new directory, as the Changes
  dialog does.

## 0.1 <Badge type="info" text="2026-10-16" />

The first release.

- Every repository of GitLab and GitHub in one list, its merge requests
  beside it, cloned where your layout says. [The Lists](/guide/navigating)
- A review worktree with the merge request's change sitting there as pending
  edits, the commit log, history rewrites and the shelf.
  [The Review Worktree](/guide/review)
- Grouped worktrees across repositories, kept current with their base.
  [Grouped Worktrees](/guide/grouped-worktrees)
- Pipelines, watches and the Activity screen.
  [Watching and Activity](/guide/watching)
- Editors, coding agents, Zellij, herdr, Ghostty, Incomm, Hunk, chezmoi,
  Yazi and zoxide. [Integrations](/guide/integrations)
- Themes. [Themes](/guide/themes)
