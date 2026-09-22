# unagit

A TUI for people who review a lot of merge requests: list every project and
open merge request across the groups you care about - on GitLab and GitHub at
once - fuzzy find the one you want, and land in `nvim` inside a ready checkout.

Pull requests are merge requests here too; one word for one thing.

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

## Setting it up

```sh
make install     # -> ~/.local/bin/unagit
unagit           # asks for a passphrase, then walks you into Settings
```

There is nothing to edit by hand. On the first run unagit asks for a
passphrase (it encrypts your tokens), then opens **Settings [S]**:

1. **GitLab servers** - press `a`, fill in a name, the URL and a personal
   access token with the `api` scope. `v` checks the token against the server.
   Add as many as you like; each keeps its own token.
2. **GitHub accounts** - the same, without a URL: github.com is the only
   address there is (GitHub Enterprise is not supported). A classic token
   needs `repo` **and `read:org`** - without the latter GitHub answers the
   organisation listing with an empty array rather than an error, so your
   organisations simply would not appear. A fine-grained token has to be
   approved by each organisation instead.
   Both sections have a **Clone over** setting: `https` hands the token to git
   through a one-shot credential helper, `ssh` uses your key and keeps the
   token for the API alone. Changing it offers to repoint the repositories
   already on disk.
3. **Groups & roots** - press `r` to load the groups, then `space` on the ones
   you work with. On GitLab `space` cycles *off → this group only → including
   subgroups*; a GitHub organisation is simply on or off. `d` gives a group -
   or a whole server - its own clone directory.
4. Press `p` and `m` to build the project and merge request indexes.

## How it works

* **Three tabs**, switched with `R`, `M` and `S`: *Repositories*, *Merge
  requests*, *Settings*. Merge requests are listed across all selected groups - of every
  server - and can be limited to a single project (`f`), on top of the fuzzy
  filter. With more than one server configured, the lists gain a `SERVER`
  column.
* **Groups are picked with a granularity**: `space` in Settings cycles a group
  between *off*, *this group only* (the projects sitting directly in it) and
  *including subgroups* (the whole tree below it).
* **Where things land is configurable per group.** A project is cloned under
  the first of: its group's own directory, its server's, the global default.
  A relative override is taken from the level above, an absolute one (or
  `~/…`) replaces it - so `acme/platform` can live in `~/work/platform` while
  everything else stays under `~/unagit`.
* **A detail column** slides in on `Enter` and takes the focus, so `j`/`k`
  scroll it. `Esc` goes back to the list, and from there the column **follows
  the cursor**: move through the list and the detail catches up once you stop
  (300 ms), without taking the focus. A closed column asks GitLab for nothing.
  `Esc` again closes it.
* Projects show visibility, statistics, languages, the latest pipeline, the
  most recent commits and their open merge requests. Merge requests are
  **always fetched fresh** from the API: author, reviewers, assignees, labels,
  approvals, pipeline, merge status, description, commits and the newest
  comments.
* **Comments are markdown, and are shown as markdown** - bold is bold, lists
  are lists, code is code. The detail column keeps the three newest; `c` opens
  the whole conversation in its own view, where `i` writes a reply and `a`
  approves.
* **Repositories** are cloned once and reused. The list shows where each one
  is on disk, which is worth seeing once a group or a server has a root of its
  own. `Ctrl-O` fetches, fast-forwards and
  starts the editor. `b` lists every branch in a searchable modal and switches
  the branch **in that same clone**.
* **Merge requests** get their own directory, so you can keep half-finished
  notes and edits in several reviews at the same time without committing
  anything. They are git worktrees of the project's main clone, which means they
  cost a checkout, not a full clone. Each merge request can have two of them:
  a **branch** worktree (`Ctrl-O`) and a **review** worktree (`Ctrl-R`) - see
  below.
* **Indexes are explicit.** Project and merge request lists are cached as JSON
  in the config directory and only refreshed when you ask (`r`, or `p` / `m`
  in settings). Startup is instant and nothing hits the API behind your back.
* **Merge requests can be grouped by project** (`Ctrl-G`): each project gets a
  heading with how many it has, and the merge requests inside keep the shared
  order. The `COM` column counts the comments - GitLab reports that on the
  listing, GitHub only on a single merge request.
* **Three filters, shared by both lists**, reachable and changeable from
  either: *cloned only* (`C`), *hidden projects* (`x` on a row, `X` for the
  list) and the *order* (`o`, by activity or by name). Hiding a project takes
  its merge requests with it, and the header under each list says what is
  being left out. They are remembered in the configuration.
* **Refreshing fans out.** Every selected group is asked in parallel, a few at
  a time, across all servers at once. GitHub has no group wide merge request
  listing, so each of its repositories is asked separately - also in parallel.
  The first failure stops the rest rather than leaving half an index behind.

Layout under the configured root directory:

```
<root>/<group>/<project>                           main clone, branch switching
<root>/<group>/<project>.mrs/<iid>-<branch>        branch worktree per merge request
<root>/<group>/<project>.reviews/<iid>-<branch>    review worktree per merge request
```

## Following it into another terminal

Opening an editor does not end unagit: it suspends the interface and waits for
the editor to exit, so for exactly as long as something is open it knows where.
It writes that down in its configuration directory, one file per running
process, and another terminal can follow it:

```sh
cd "$(unagit cd)"          # asks which, when more than one is open
cd "$(unagit cd calling)"  # a search narrows it; one match needs no asking
unagit sessions            # what is open, for scripts
```

Worth keeping in your shell:

```sh
ug() { cd "$(unagit cd "$@")" || return; }
```

The chooser draws on the terminal itself, not on standard output, which is what
leaves the directory usable in a command substitution. Records whose process is
gone are swept up on the next read, so a crash leaves nothing behind.

## Reading and answering

`c` on a merge request opens the conversation with the markdown rendered.
Comments are grouped into the threads they belong to - replies indented under
what they answer, marked with `↳` - and the threads run oldest first. On
GitLab that comes from the discussions endpoint, on GitHub from a review
comment's `in_reply_to_id`. From there `i` writes a comment (`Ctrl-S` sends it) and `a`
approves - approving asks for confirmation first, because everyone on the
merge request sees it. Both work straight from the list too.

## Reviewing a merge request

A branch worktree (`Ctrl-O`) is an ordinary checkout of the merge request
branch: real commits, and you can commit and push. The catch when reviewing is
that everything is already committed, so a diff view has nothing pending to
show you, and the change is spread over however many commits the author made.

A review worktree (`Ctrl-R`) turns that around. `HEAD` sits on the commit the merge
request branched from, while the index and the working tree hold the merge
request head. The whole change is therefore **pending**, exactly as if you had
just typed it:

```sh
git diff --staged        # the entire merge request, as one diff
git status               # every file it touches, including additions and deletions
```

Gutter signs, `]c`, `:Gvdiffsplit`, `:DiffviewOpen` - anything that works on
uncommitted changes now works on the merge request as a whole. Your own edits
on top survive reopening it, and a rebase or a force push on the other side is
picked up on the next open.

The base is the one GitLab itself uses (`diff_refs.base_sha`, the merge base),
not the tip of the target branch - otherwise a target that has moved on would
show its own commits backwards in your diff. GitHub does not publish a merge
base, so there unagit works it out from the repository.

Both kinds of worktree record what they are in their own git configuration, so
an editor can pick it up:

```sh
git config unagit.mr.base    # the commit GitLab diffs against
git config unagit.mr.head    # the merge request head
git config unagit.mr.iid     # …and .project, .source, .target, .url, .mode
```

In a branch worktree that is what you need for the same view over the commits:

```vim
:DiffviewOpen <C-r>=system('git config unagit.mr.base')<CR>...HEAD
```

## The token

The GitLab token is stored **encrypted** in `~/.config/unagit/token.enc`:
Argon2id derives a key from a passphrase, AES-256-GCM seals the token. The
passphrase is asked for in a dialog inside the TUI on every start — so it looks
the same whether you run unagit from a shell or from inside nvim — and the
decrypted token only ever lives in process memory.

During `unagit init` the token and the passphrase are read straight from
`/dev/tty` with echo off. Neither can be supplied through arguments,
environment variables or a pipe, so nothing that inspects your shell history,
your process list or your files can pick them up.

Git also never sees the token on disk: HTTPS pushes and fetches are
authenticated by a one-shot credential helper that reads it from the
environment of that single git process. Your `.git/config` stays clean, and
the remote URL contains no credentials (there is a test that enforces this).
The flip side is that a `git pull` you run yourself, outside unagit, will ask
for credentials — set up a credential helper of your own or use SSH remotes if
you want that.

## Requirements

Go 1.24+, `git`, and a token per server: `api` scope on GitLab, `repo` scope
on GitHub.

## Keys

| Key | Action |
| --- | --- |
| `R` `M` `S` | switch to Repositories / Merge requests / Settings |
| `/` | filter mode (fuzzy, space separated terms) |
| `Esc` | leave filter mode; again clears it; again closes the detail column |
| `j` `k` `g` `G` | move |
| `Enter` | load the detail column and jump into it |
| `Ctrl-O` | clone or update, then open the editor |
| `Ctrl-R` | open a merge request for review: the change as pending edits |
| `c` | read the whole conversation, and write a comment |
| `a` | approve the merge request (it asks first) |
| `h` `l` `←` `→` | move between the list and the detail column |
| `b` | pick a branch in a modal (projects only) |
| `m` | merge requests of the selected project |
| `f` / `F` | limit merge requests to a project / clear that limit |
| `d` | delete from disk, with a warning about uncommitted or unpushed work |
| `w` | open in the browser |
| `r` | refresh the current index from GitLab (groups in Settings) |
| `C` | show only the projects you have cloned |
| `x` | hide the project under the cursor, or bring it back |
| `X` | manage the hidden projects |
| `o` | order: by activity, or by name |
| `Ctrl-G` | gather the merge requests under their project |
| `?` | help |
| `q` | quit |

Inside any modal the same two-stage `Esc` applies: the first one leaves the
filter input so `j`/`k` move the selection, the second one closes the modal.
Modals darken the interface behind them rather than hiding it, so you keep the
context you opened them from.

On-disk markers: `○` nothing, `●` a branch worktree, `◐` a review worktree,
`◉` both. `⊘` marks a project hidden from the lists.

Merge request heads are fetched from `refs/merge-requests/<n>/head` on GitLab
and `refs/pull/<n>/head` on GitHub; everything downstream of that is the same.

The detail column also reports the size of a merge request - how many commits
it adds on top of its target, how many files it touches, and how far behind
the target it has fallen.

## Configuration

Everything below is written by the Settings tab; it is documented because it
is your data, not because you have to touch it.
`~/.config/unagit/config.yaml` (override the directory with `UNAGIT_CONFIG_DIR`
or `XDG_CONFIG_HOME`):

```yaml
root_dir: ~/unagit          # the default; everything else is an override
editor: nvim
editor_args: ["."]
instances:
  - id: gitlab-example-com  # stable key: ties the token and the caches to it
    kind: gitlab
    name: Work
    url: https://gitlab.example.com
    root_dir: ~/work        # optional, for this server
    groups:
      - id: 42
        full_path: acme/platform
        name: platform
        scope: subgroups    # or "group" for this group's own projects only
        root_dir: platform  # optional; relative to ~/work here
  - id: github-com
    kind: github
    name: Personal
    url: https://github.com
    groups:
      - id: 10
        full_path: widgets  # an organisation, or your own account
        scope: group
```

Alongside it live `tokens.enc` and the `index-*.json` caches.

## Commands

| Command | Purpose |
| --- | --- |
| `unagit` | start the TUI - everything is configured inside it |
| `unagit cd [search]` | print the directory of what is open in an editor |
| `unagit sessions` | list what is open in an editor |
| `unagit where` | print the config, vault and index paths |
