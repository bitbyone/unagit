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

- `Ctrl-O` clones it if it is not there, fetches and fast-forwards it if it is,
  then opens your editor in it.
- `b` lists every branch in a searchable modal and switches it **in that same
  clone**, so there is one working copy per repository, not a directory per
  branch.
- `m` jumps to that repository's merge requests, `w` opens it in the browser,
  `d` deletes it from disk - after warning about uncommitted or unpushed work.
- `/` fuzzy-finds, `C` hides everything you have not cloned, `x` hides a
  repository you never want to see.

**Where things land is yours to decide.** A repository is cloned under the
first of: its group's own directory, its server's, the global default. So
`acme/platform` can live in `~/work/platform` while everything else stays under
`~/unagit`, and a subgroup can override its parent. It is one keystroke (`d`)
in Settings, and it applies to everything cloned afterwards.

```
<root>/<group>/<repo>                        the clone you switch branches in
<root>/<group>/<repo>.mrs/<iid>-<branch>     a branch worktree for one MR
<root>/<group>/<repo>.reviews/<iid>-<branch> a review worktree for one MR
```

## Reviewing: the part worth stealing

Press `Ctrl-R` on a merge request and unagit builds a worktree where

```
HEAD = the merge base     index = the merge base     files = the merge request
```

so the whole merge request reads as **one pending, unstaged change**:

```sh
git diff        # the entire merge request, as a single diff
git status      # every file it touches, additions and deletions included
```

Unstaged is the point. Editors draw their gutter by comparing the file against
the index, so the index has to be the merge base - then gitsigns, gitgutter,
`]c`, `:Gvdiffsplit` and `:DiffviewOpen` all work on the merge request as a
whole, with nothing to configure. Files the merge request adds go in as
intent-to-add, so they read as new files instead of vanishing into untracked.

The base is the one GitLab itself diffs against (`diff_refs.base_sha`), not the
tip of the target branch - otherwise a target that moved on would show its own
commits backwards in your diff.

Notes you type into the files survive reopening, and a force push on the other
side is picked up on the next open. `Ctrl-O` gives you the ordinary branch
checkout instead, when you mean to commit and push.

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
- **Filters that stick**: cloned-only, hidden repositories, sort order, group
  by repository, plus a fuzzy filter on everything.
- **Instant startup.** The lists are cached on disk and only refreshed when you
  ask; nothing hits the API behind your back.
- **Your tokens are encrypted** with Argon2id + AES-256-GCM and exist in
  plaintext only in memory, for as long as unagit runs. The passphrase is asked
  for in a dialog on every start and is never read from a flag, an environment
  variable or a pipe. git gets the token through a one-shot credential helper,
  so it never reaches `.git/config` or a remote URL - or you can clone over SSH
  and keep the token for the API alone.

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
| `Ctrl-O` | clone or update, then open the editor |
| `Ctrl-R` | open a merge request for review - the change as pending edits |
| `c` `a` | read and write comments · approve |
| `b` `m` `f` | branch picker · merge requests of this repo · limit to a repo |
| `C` `x` `X` `o` `Ctrl-G` | cloned only · hide · hidden list · order · group by repo |
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
