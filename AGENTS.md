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

The one exception is the user's own choice, and it is exactly this: on macOS,
Settings › Security › `k` keeps the passphrase in the login keychain
(`internal/keychain`), created by the unagit binary through the legacy
keychain API, so its access list trusts that binary alone - verified: any
other program, `security` included, gets a macOS dialog. It is off unless the
user turns it on. Do not widen it: no keychain item readable without that
dialog, no Secret Service on Linux (it lets every program of the user's read
it), no cache daemon, and never read the item yourself. Tests use a fake
store; `UNAGIT_KEYCHAIN_TEST=1` runs the one test that touches the real
keychain, with an item of its own.

## Working here

```sh
make build            # go build -o unagit ./cmd/unagit
make test             # go test -race ./...
gofmt -l . && go vet ./...
```

Everything must be gofmt-clean, vet-clean and race-clean before a commit. The
UI package's tests run a real tview application against a simulation screen and
take ~15 s; that is normal.

Commit and push every finished change, in every repository you touched (unagit
and `../incomm`), without waiting to be asked: the user wants nothing left
unpushed. Never force-push. Commit messages are a sentence in the imperative
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

## Repeated UI elements are one component

**Anything that appears in more than one place is built once and reused, so it
looks and behaves the same everywhere.** If you are about to configure a widget
a second time, stop and look for the existing way first - for a form that is
`ui/fields.go`, for styling `ui/theme.go`, for lists `showPicker`, for
confirmations `confirm`/`confirmWith`, for a form in a modal `showFormModal`.

This has gone wrong twice. A select box in the merge request form was made with
plain `AddDropDown`: its open list was an unreadable pale slab, and typed
letters went into tview's hidden search field. Settings already had the right
select (`styleDropDown`), it just was not reused. The rules:

- **Select boxes** come from `addSelect`, checkboxes from `addCheckbox`. Never
  call `AddDropDown`, `NewDropDown`, `AddCheckbox` or `NewCheckbox` outside
  `fields.go`; `TestSharedFieldsAreTheOnlyWayToMakeThem` fails if you do.
- A select ignores typed letters (arrows and Enter open it) and draws its list
  with the same selection band as every list. Every select of a form is as
  wide as the widest (`addSelect` sizes them all; tview would size each to its
  own longest option and the column ends ragged). Esc on an open select closes
  its list and nothing else: tview hands that Esc to the select, which tells
  the form it is finished, and the form cancels - so `bindFormButtons` gives
  it to the open list instead (`TestEscClosesAnOpenSelectAndNotTheDialog`). Do not "improve" one of them on
  its own; change `styleDropDown` and every select follows.
- A list to choose from is `showPicker` (or `showPickerWith` for its options),
  and it opens on the list: j/k move, Enter picks, `/` starts the filter. Do
  not open one on the filter; typed letters then vanish into a search.
- A new kind of field that will be used twice goes into `fields.go` first, with
  a test, and the old call sites move to it in the same change.
- **Look at what you built.** This is a terminal UI and it can be drawn: render
  it on the simulation screen, print `screenText`, and read it before you say
  it works. Every new dialog gets a layout test at several terminal sizes
  (`mrform_layout_test.go` is the model): no field wider than its frame, the
  frame's border intact on every row, every label on screen, and
  `assertLegible` after opening whatever can open. Checking widget rectangles is
  not enough - tview draws an over-wide field past its rectangle.

## The map

| Package | What it owns |
| --- | --- |
| `cmd/unagit` | cobra commands: the TUI, `cd`, `sessions`, `where` |
| `internal/forge` | the neutral vocabulary and the `Provider` interface |
| `internal/gitlab` | REST v4 client answering in forge's words |
| `internal/github` | github.com client, same |
| `internal/config` | `config.yaml`, instances, groups, roots, filters |
| `internal/secret` | the vault, the passphrase prompt |
| `internal/keychain` | the passphrase in the macOS keychain, when chosen |
| `internal/index` | the on-disk JSON caches of the lists |
| `internal/workspace` | clones, worktrees, the review arrangement |
| `internal/gitx` | the git command line, credentials, error hints |
| `internal/session` | what is open in an editor, files named by pid |
| `internal/editors` | which editors are installed, and the command that opens one |
| `internal/fuzzy` | the subsequence matcher behind `/` |
| `internal/md` | markdown → tview markup |
| `internal/ui` | everything on screen |

In `internal/ui`: `app.go` holds the `App` and the refresh fan-out, `pane.go`
the table+filter+detail widget both lists are made of, `projects.go` / `mrs.go`
their contents, `detail.go` the right-hand column, `settings.go` the whole
configuration UI, `comments.go` the conversation, `filters.go` the shared
filters, `modals.go` the overlay machinery, `theme.go` the palette, `help.go`
the `?` screen as data.

**What a screen can do is data.** Every list, the worktree view and Settings
describe their actions as `uiAction`s (`palette.go`, the lists in
`actions_lists.go`, Settings in `settings_actions.go`): a name, the key, a
rank by how often it is wanted, when it can be done, and what it does. The
same list answers the keys and fills the two action pickers - `Alt-Enter`
(`Ctrl-A`) for the selection, `:` for the screen - so a key cannot do one
thing while the picker says another. A new action goes into that list, not
into a `switch` on runes; one too rare for a key gets `keys: ""` and lives in
the picker alone (creating a repository does). A key whose action
cannot be done now is still run, so that it says why; `when` only keeps the
pickers to what can be done. In Settings an action presses its own key in
the section, which keeps the behaviour where it was. `TestNoTwoActionsShareAKey`
guards the lists.

Main views expose shortcuts through `?`: help keeps actions for the opening
context in normal text and dims the rest.

**The help is keys, not prose.** This has gone wrong twice: paragraphs of
explanation were added to `helpRows`, and the help became a wall of text with
empty key columns in which the keys could not be found. Every row of the help
is a key (or a column name, a marker, a path kind) on the left and one short
line on the right - nothing else. A new feature adds its keys there and its
explanation to the README. If something will not fit in one line, it is not
help text. `TestHelpIsKeysNotProse` enforces it: no row without a key, a key at
most 15 wide, a description at most 52 long; there is no `note()` any more, so
do not bring it back. Open `?` on the tab you changed and look at it. Keep inline hints in modals and in
simple blocks with up to five actions, such as integrations. Dialog buttons
use local action letters, lit in their labels (`markKey`; coloured, not
bracketed - brackets widen the row past small dialogs). Every form has a
NORMAL and an INSERT mode (`formmode.go`, wired by `bindFormButtons` through
`showFormModalSized`): NORMAL moves with j/k/Tab and presses a button by its
letter, i/Enter types, Esc stops typing. A button has its lit letter and no
other key - no Alt variant - and the hint names exactly those letters (the
user asked for that). A form whose first item is a text field opens in INSERT. h j k l i are
no button's. The focused field is painted after the draw (`markFocusedField`),
since tview re-colours every field on each draw. Find a button by
`buttonName`, never by its label. Inline hints stay below their context
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

**Grouped worktrees** put several repositories in one folder,
`<root>/.unagit/groups/<folder>/<repo>`, each a worktree of its own main clone,
with `.unagit-group.json` saying which repository each directory is (a name
cannot tell two servers apart). Space marks rows in a pane (`pane.marks`, for a
list that sets `markable`); Ctrl-W on marks makes the group. It is made whole
or not at all: every repository is cloned and fetched, then checked
(`CheckGroupMember`), and only then are worktrees added; a failure takes back
what was made. A new group always makes a branch of its own; the selects in
its form are bases, not checkouts (this was once misread and "fixed" into a
checkout filter - do not). They offer every branch but one a worktree has
out, starting on the main clone's branch. Deleting a group keeps its branches.
Incomm in a group: the editor's store is the group folder's, one file for every
repository, its paths under the members' folders. `incomm.Place` (a dir and a
prefix) is how a merge request's comments are read from it - `mrPlaces` adds the
group's place to the worktrees' - and `ImportAt` writes them back under the
prefix. Thread.Dir stays the group's, so a published comment's source is
recorded where it was read.
A branch unagit makes records its base in `branch.<name>.unagitBase`;
`UpdateBranch` rebases a branch onto that base only while it has no upstream -
once pushed, rebasing would need a force push. `Ctrl-R` (`RebaseOntoBase`)
does it anyway on request and notes the upstream it moved away from in
`branch.<name>.unagitRebasedFrom`; `P` force-pushes only with that as the
lease, so nothing pushed since can be overwritten. No other path forces.

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
- `v` narrows the same worktree to one commit onwards: HEAD and the index go
  to that commit's parent, the working tree stays the head, and
  `unagit.mr.base` follows HEAD while `unagit.mr.from` names the commit (unset
  again by a whole review). "New since the last review" is `git cherry` against
  the head last checked out, so commits a rebase rewrote still count as seen.
  Narrowing refuses rather than skips when the reviewer has edits.
- `.incomm/` is the reviewer's notebook, not part of the change. Usually it
  is not committed, and then it is ignored through `info/exclude` - git
  status, lazygit and editors list untracked files, so "git diff does not
  show it" is not enough. In a repository that commits it, its tracked files
  are skip-worktree, the merge
  request's additions to it are not `add -N`'d, it is never an "own edit",
  and when the worktree follows the merge request the directory is set aside
  for the reset and put back (`setNotesAside`). A reset with skip-worktree
  files still set fails in `read-tree`, so clear the bit before resetting.

**Credentials never touch disk.** `gitx` passes a one-shot credential helper on
the command line that reads user and token from the child process environment,
so `.git/config` and remote URLs stay clean (there is a test for that). SSH is
the other option, and git runs with `BatchMode=yes` - a passphrase-protected
key with no agent therefore fails immediately instead of hanging; `hint()`
turns that, and a few other git failures, into an instruction.

**Editors.** Every opening action takes the editor as a parameter: nil is
the favourite, looked up at the moment it starts; Alt with the same key picks
one first (`withEditor`). Without an installed favourite every open asks -
never fall back quietly to another editor, and never look a missing one up
again by name (a test once started the real Zed that way). Add a new opening action the same way, with its Alt
variant, rather than calling the favourite directly. A terminal editor
(nvim, or a custom one) gets the terminal; a window editor (IDEA, VS Code,
Zed) is only started, because its launcher returns at once.

**Sessions.** Opening a terminal editor suspends the TUI and waits for the child, so
the process knows the directory the whole time. A window editor's record
stays until unagit exits, since nothing tells unagit when the window closes. It writes a JSON file per open directory, named by pid,
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
- A `DropDown` feeds every typed letter into a hidden search field, and a fresh
  one is drawn in the theme's colours, which here paint text in the background's
  own colour. Use `addSelect`, never the bare widget.
- An `InputField` or `TextArea` given a fixed width wider than the room left
  after its label is drawn over the modal's frame (its rectangle is right, the
  drawing is not). Give a field in a modal width `0` so it fills what is left,
  and choose the modal width with `showFormModalSized`.
- **Nothing may be drawn while an editor has the terminal.** tcell empties
  its cells on Suspend but keeps its size, and a Show then loops for ever
  holding the screen lock, so the Resume after the editor never returns and
  unagit hangs on the shell prompt. Background loads redraw at any time, so
  the screen is wrapped (`quietScreen`, `screen.go`) to drop Show and Sync
  between Suspend and Resume. Hand a screen in with `App.SetScreen`, never
  `tv.SetScreen`, or the wrapper is lost.
- **A table paints a cell's background over its text.** The selection band
  and a row's own background (a marked row's) both repaint every cell of the
  row, so a tag's pill on it lost its fill and became plain text between two
  coloured ends - this went wrong twice, first with the cursor, then with
  the marks. Pills, and anything else that must keep its colours on a
  painted row, are drawn again after the table, from `keptTable`: `markup`
  for the cursor's band, `banded` for a row with a background of its own.
  Give a row a background and you must give its pills a `banded` markup on
  that background (`tagsField` does). `TestTagsOnRepositories` checks the
  pill plain, under the cursor, marked, and both.
- The `u` (underline) style flag is broken in this version: setting attributes
  afterwards loses the bit but keeps tcell's underline, so everything after a
  link came out underlined. Links use colour only.

## State of things

The code is complete and green as of the last commit. What is left is the
user's to do, not the repository's: load the SSH key into the agent
(`ssh-add --apple-use-keychain ~/.ssh/id_rsa`) if cloning over SSH, and issue a
GitHub token with `read:org` for organisations to appear.
