# unagit

A GitLab TUI for people who review a lot of merge requests: list every project
and open merge request across the groups you care about, fuzzy find the one you
want, and land in `nvim` inside a ready checkout.

```
 NORMAL  120/318 projects  indexed 4m ago  root: ~/unagit
/ api
┌ Projects ────────────────────────────────────────────────────────────────────┐
│   PROJECT                    BRANCH        MR  ACTIVITY                      │
│ ● acme/platform/api-gateway  feature/rate   2  3h ago                        │
│ ○ acme/platform/api-docs     main              2d ago                        │
└──────────────────────────────────────────────────────────────────────────────┘
 PROJECTS 318  (mrs: 84)  | ? help  | q quit
```

## How it works

* **Two lists.** `Tab` switches between *Projects* and *Merge requests*. Merge
  requests are listed across all selected groups and can be limited to a single
  project (`p`), on top of the fuzzy filter.
* **Projects** are cloned once and reused. Opening one fetches and
  fast-forwards, then starts the editor. `b` switches the branch **in that same
  clone**.
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
passphrase is asked for on every start and the decrypted token only ever lives
in process memory.

Both the token and the passphrase are read straight from `/dev/tty` with echo
off — they cannot be supplied through arguments, environment variables or a
pipe, so nothing that inspects your shell history, your process list or your
files can pick them up.

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
unagit                # start; press 's' to pick groups, then 'p' and 'm'
```

Requires Go 1.24+, `git`, and a GitLab personal access token with the `api`
scope.

## Keys

| Key | Action |
| --- | --- |
| `Tab` | switch Projects / Merge requests |
| `/` | filter mode (fuzzy, space separated terms) |
| `Esc` | leave filter mode; again to clear the filter |
| `j` `k` `g` `G` | move |
| `Enter` | clone or update, then open the editor |
| `b` | pick a branch (projects only) |
| `m` | merge requests of the selected project |
| `p` / `P` | limit merge requests to a project / clear that limit |
| `d` | delete from disk, with a warning about uncommitted or unpushed work |
| `w` | open in the browser |
| `r` | refresh the current index from GitLab |
| `s` | settings (group selection, index refresh) |
| `?` | help |
| `q` | quit |

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
```

Selecting a group includes its subgroups. Alongside it live `token.enc` and the
`index-*.json` caches.

## Commands

| Command | Purpose |
| --- | --- |
| `unagit` | start the TUI |
| `unagit init` | create the config and store the encrypted token |
| `unagit token` | replace the stored token |
| `unagit passphrase` | change the passphrase |
| `unagit where` | print the config and index paths |
