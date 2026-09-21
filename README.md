# unagit

A GitLab TUI for people who review a lot of merge requests: list every project
and open merge request across the groups you care about, fuzzy find the one you
want, and land in `nvim` inside a ready checkout.

```
 Projects [P] │ Merge requests [M] │ Settings [S]
 DETAIL  84/84 merge requests  indexed 4m ago  scope: all projects
 /
╭ Merge requests ─────────────────────────────╮╭ acme/api-gateway !42 ─────────╮
│   PROJECT           MR  TITLE               ││ !42  Fix login rate limiting  │
│ ● acme/api-gateway  !42 Fix login rate li…  ││ acme/api-gateway              │
│ ○ acme/billing      !17 Invoice rounding    ││ feat/rate → main              │
│                                             ││                               │
│                                             ││ MERGE REQUEST                 │
│                                             ││ Author       jane (Jane Doe)  │
│                                             ││ Created      3d ago           │
│                                             ││ Reviewers    john             │
│                                             ││ Approvals    1 of 2 · john    │
│                                             ││ Pipeline     ● running        │
╰─────────────────────────────────────────────╯╰───────────────────────────────╯
 84 merge requests  · ? help · q quit
```

## How it works

* **Three tabs**, switched with `P`, `M` and `S`: *Projects*, *Merge requests*,
  *Settings*. Merge requests are listed across all selected groups and can be
  limited to a single project (`f`), on top of the fuzzy filter.
* **Groups are picked with a granularity**: `Space` in Settings cycles a group
  between *off*, *this group only* (the projects sitting directly in it) and
  *including subgroups* (the whole tree below it).
* **A detail column** slides in on `Enter` and takes the focus, so `j`/`k`
  scroll it. Projects show visibility, statistics, languages, the latest
  pipeline, the most recent commits and their open merge requests. Merge
  requests are **always fetched fresh** from the API: author, reviewers,
  assignees, labels, approvals, pipeline, merge status, description, commits
  and the newest comments. `Esc` goes back to the list, `Esc` again closes the
  column.
* **Projects** are cloned once and reused. `Ctrl-O` fetches, fast-forwards and
  starts the editor. `b` lists every branch in a searchable modal and switches
  the branch **in that same clone**.
* **Merge requests** get their own directory, so you can keep half-finished
  notes and edits in several reviews at the same time without committing
  anything. They are git worktrees of the project's main clone, which means they
  cost a checkout, not a full clone.
* **Indexes are explicit.** Project and merge request lists are cached as JSON
  in the config directory and only refreshed when you ask (`r`, or `p` / `m`
  in settings). Startup is instant and nothing hits the API behind your back.

Layout under the configured root directory:

```
<root>/<group>/<project>                       main clone, branch switching happens here
<root>/<group>/<project>.mrs/<iid>-<branch>    one worktree per merge request
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

## Install

```sh
make install          # builds and copies to ~/.local/bin
unagit init           # asks for URL, root dir, editor, token and passphrase
unagit                # start; press 'S' to pick groups, then 'p' and 'm'
```

Requires Go 1.24+, `git`, and a GitLab personal access token with the `api`
scope.

## Keys

| Key | Action |
| --- | --- |
| `P` `M` `S` | switch to Projects / Merge requests / Settings |
| `/` | filter mode (fuzzy, space separated terms) |
| `Esc` | leave filter mode; again clears it; again closes the detail column |
| `j` `k` `g` `G` | move |
| `Enter` | load the detail column and jump into it |
| `Ctrl-O` | clone or update, then open the editor |
| `h` `l` `←` `→` | move between the list and the detail column |
| `b` | pick a branch in a modal (projects only) |
| `m` | merge requests of the selected project |
| `f` / `F` | limit merge requests to a project / clear that limit |
| `d` | delete from disk, with a warning about uncommitted or unpushed work |
| `w` | open in the browser |
| `r` | refresh the current index from GitLab (groups in Settings) |
| `?` | help |
| `q` | quit |

Inside any modal the same two-stage `Esc` applies: the first one leaves the
filter input so `j`/`k` move the selection, the second one closes the modal.

`●` means the project or merge request is on disk, `○` means it is not.

## Configuration

`~/.config/unagit/config.yaml` (override the directory with `UNAGIT_CONFIG_DIR`
or `XDG_CONFIG_HOME`):

```yaml
gitlab_url: https://gitlab.example.com
root_dir: ~/unagit
editor: nvim
editor_args: ["."]
groups:
  - id: 42
    full_path: acme/platform
    name: platform
    scope: subgroups   # or "group" for this group's own projects only
```

Alongside it live `token.enc` and the `index-*.json` caches.

## Commands

| Command | Purpose |
| --- | --- |
| `unagit` | start the TUI |
| `unagit init` | create the config and store the encrypted token |
| `unagit token` | replace the stored token |
| `unagit passphrase` | change the passphrase |
| `unagit where` | print the config and index paths |
