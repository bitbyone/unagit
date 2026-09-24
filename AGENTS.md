# AGENTS.md

Notes for whoever - person or model - works on unagit next. The README says
what it is for; this says how it is built and which mistakes have already been
made.

The name expands to *Universal Navigator Around GIT*, and that is the wider
job: reviewing merge requests is the sharpest part of it, but the tool is a
navigator over every repository the user works with - what exists, where on
disk it goes, and getting inside it. Keep both halves in mind when you add
something.

## The one hard rule

**The tokens are deliberately out of your reach.** They live encrypted in
`~/.config/unagit/tokens.enc` (Argon2id → AES-256-GCM) and the passphrase is
typed by the user into a dialog at startup. That design exists precisely so an
agent working in this repository cannot read them.

So: never print a token, never ask the user to paste one into the conversation,
never add a way to pass one through a flag, an environment variable, a file or
a pipe, and never `cat` the vault. If you need to know whether a key exists,
read its name, not its value (`cut -d= -f1`). Never weaken the passphrase
prompt "for testing".

## Working here

```sh
make build            # go build -o unagit ./cmd/unagit
make test             # go test -race ./...
gofmt -l . && go vet ./...
```

Everything must be gofmt-clean, vet-clean and race-clean before a commit. The
UI package's tests run a real tview application against a simulation screen and
take ~15 s; that is normal.

Commit only when asked. Commit messages are a sentence in the imperative
("Make a review worktree read the way a gutter reads"), then a paragraph about
*why*, not a list of touched files.

### Style

Plain English comments that explain the reason, not the mechanics - the file
you are editing shows the voice. Comments earn their place by saying something
the code cannot. No emoji, no exclamation marks, ASCII arrows in code comments
(`→` is fine in user-facing strings). Errors are lower case and say what to do
about it.

### A trap when editing

`gofmt` realigns struct tags and re-wraps nothing else, but it *does* move
things - and several times a scripted replacement silently matched nothing
because the anchor text had been reformatted since it was last read. If a
replacement reports zero matches, re-read the file rather than retrying.

## The map

| Package | What it owns |
| --- | --- |
| `cmd/unagit` | cobra commands: the TUI, `cd`, `sessions`, `where` |
| `internal/forge` | the neutral vocabulary and the `Provider` interface |
| `internal/gitlab` | REST v4 client answering in forge's words |
| `internal/github` | github.com client, same |
| `internal/config` | `config.yaml`, instances, groups, roots, filters |
| `internal/secret` | the vault, the passphrase prompt |
| `internal/index` | the on-disk JSON caches of the lists |
| `internal/workspace` | clones, worktrees, the review arrangement |
| `internal/gitx` | the git command line, credentials, error hints |
| `internal/session` | what is open in an editor, one file per pid |
| `internal/fuzzy` | the subsequence matcher behind `/` |
| `internal/md` | markdown → tview markup |
| `internal/ui` | everything on screen |

In `internal/ui`: `app.go` holds the `App` and the refresh fan-out, `pane.go`
the table+filter+detail widget both lists are made of, `projects.go` / `mrs.go`
their contents, `detail.go` the right-hand column, `settings.go` the whole
configuration UI, `comments.go` the conversation, `filters.go` the shared
filters, `modals.go` the overlay machinery, `theme.go` the palette, `help.go`
the `?` screen as data.

Main views expose shortcuts through `?`: help keeps actions for the opening
context in normal text and dims the rest. Keep inline hints in modals and in
simple blocks with up to five actions, such as integrations. Dialog buttons
use local action letters; forms use Alt plus the letter while editing, and
plain letters when a button has focus. Inline hints stay below their context
and wrap when the terminal narrows. Keep `? help` in the global status line
below all panels, including Settings; do not repeat it in panel footers.

## Decisions worth knowing before changing them

**One vocabulary.** Nothing above `internal/forge` knows whether it is talking
to GitLab or GitHub. A GitHub organisation is a group; a pull request is a
merge request. If a forge lacks something (GitHub has no subgroups, no group
wide merge request listing, no merge base), the provider makes it up out of
what the API does have - it does not leak the difference upwards.

**Explicit indexes.** The lists are JSON caches refreshed only on `r`/`p`/`m`,
so startup is instant. `index.Version` goes up whenever a new field is added
that an old cache cannot have, and the UI then says a refresh would bring
something new instead of leaving a column quietly empty (that was the `COM`
column bug).

**Refresh fans out** over groups (and, on GitHub, over repositories) with a
bounded worker count and first-error cancellation - half an index is worse
than none.

**Worktrees, not clones.** One main clone per repository; each merge request
gets `<parent>/.unagit/<repo>/<iid>-<branch>` (a real branch, for committing) and/or
`<parent>/.unagit/<repo>/review-<iid>-<branch>` (see below). Existing `.mrs` and
`.reviews` worktrees are still discovered and reused at their old paths. They share the object store, so a
second review costs a checkout. Per-worktree git config
(`extensions.worktreeConfig`) carries `unagit.mr.base/.head/.iid/.project/
.source/.target/.url/.mode`, which is how an editor - or a later unagit - knows
what a directory is.

**The review arrangement** is the feature the whole tool exists for, and it is
easy to get subtly wrong:

```
HEAD = merge base     index = merge base     working tree = merge request head
```

- The change must be **unstaged**, not staged. Editors draw their gutter by
  comparing the buffer to the *index*; an earlier version staged the change
  (index = head) and gitsigns, gitgutter and plain `git diff` all had nothing
  to show. `pendChange` does `read-tree -u -m <head>` then `reset` the index
  back to HEAD.
- Files the merge request **adds** are entered with `git add -N`, or they would
  be untracked and `git diff` would pass over them.
- The base is GitLab's `diff_refs.base_sha` (the merge base), never the tip of
  the target branch; with a target that has moved on, its own commits would
  otherwise appear reversed in the diff. Verified empirically, not assumed.
- Because the merge request itself is pending by design, "has the reviewer
  edited something" cannot mean "is anything unstaged". It is
  `git diff --name-only <the head we last checked out>`, and when that cannot
  be determined the worktree is left alone rather than reset.

**Credentials never touch disk.** `gitx` passes a one-shot credential helper on
the command line that reads user and token from the child process environment,
so `.git/config` and remote URLs stay clean (there is a test for that). SSH is
the other option, and git runs with `BatchMode=yes` - a passphrase-protected
key with no agent therefore fails immediately instead of hanging; `hint()`
turns that, and a few other git failures, into an instruction.

**Sessions.** Opening an editor suspends the TUI and waits for the child, so
the process knows the directory the whole time. It writes one JSON file per pid
under `<config>/sessions/`, removed when the editor exits and swept up later if
the process died. `unagit cd` `exec`s a shell there (a process cannot change
its parent's directory); `--print` writes the path for a command substitution,
which is why the picker draws on `/dev/tty` and never on stdout.

**The theme is one pair of colours.** tview builds every interactive widget out
of `PrimaryTextColor` / `ContrastBackgroundColor` used both ways round: at rest
the dark one is the background, when active it is the ink. Both halves must
therefore be real colours - leaving one as the terminal default is what made
buttons and drop-downs invisible. Fix colour problems in `theme.go`, never by
styling one widget.

## What the tests are actually guarding

- **Legibility** (`ui/legibility_test.go`): no rendered glyph may be drawn in
  its own background colour, nor left at the terminal's default foreground on a
  background we chose. It walks the dialogs with the keyboard and focuses each
  form item in turn, because the *focused* state is the one that breaks.
- **The review worktree** matches GitLab's three-dot diff exactly, is entirely
  unstaged, keeps the reviewer's edits, and follows a force push.
- **The token** appears in no file anywhere under the clone root after a clone
  and a worktree - the test greps every byte on disk, not just `.git/config`.
- **`unagit cd`** is exercised from a second process, with a real shell, and
  must print the directory and nothing else in `--print` mode.

### Driving the UI in tests

`newTestApp` starts the app on a `tcell.SimulationScreen` against a fake API
server. Rules learned the hard way:

- Read app or widget state only through `onLoop`, which hops onto tview's event
  loop; touching it directly is a race.
- `QueueUpdate` can overtake a key event that has not been handled yet, so
  assertions poll (`waitFor`, `waitSelected`) instead of assuming.
- `Form.SetFocus` on a non-focusable `TextView` re-enters that item's own lock
  and deadlocks tview; the legibility test skips such items, and so should you.
- Fixture merge requests need distinct `id`s (the dedupe is by id) and
  `Version: index.Version`, or the list arrives empty or stale.

### tview quirks already paid for

- A masked `InputField` leaves residue after `SetText("")`; toggle the mask off
  and on (`clearMasked`).
- `Box.DrawForSubclass` fills its rectangle with spaces, so a centring Flex
  erases the background you were about to dim. `modalBox` draws nothing of its
  own and `dimArea` darkens what is beneath.
- A draw function is handed the **outer** rectangle and must return the
  **inner** one; returning it unchanged makes tables overflow their border.
- The `u` (underline) style flag is broken in this version: setting attributes
  afterwards loses the bit but keeps tcell's underline, so everything after a
  link came out underlined. Links use colour only.

## State of things

The code is complete and green as of the last commit. What is left is the
user's to do, not the repository's: load the SSH key into the agent
(`ssh-add --apple-use-keychain ~/.ssh/id_rsa`) if cloning over SSH, and issue a
GitHub token with `read:org` for organisations to appear.
