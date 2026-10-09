# Terminal multiplexers

Zellij is implemented and verified with 0.45.1. tmux remains a future
backend; this change does not install it or offer tmux actions.

## Opening editors in Zellij

Three actions appear in the selection action picker (`Alt-Enter` or
`Ctrl-A`) while unagit runs inside Zellij:

- **Open in New Tab** opens a named tab.
- **Open in Vertical Split** opens beside unagit, with a vertical divider.
- **Open in Horizontal Split** opens below unagit.

They have no keys and no setting. Existing opening keys still give the
editor unagit's own terminal. These actions use the default terminal
editor; if the default is missing or opens a window, the shared picker
asks among installed terminal editors.

A repository opens its clone, cloning first if needed. A merge request
opens its branch worktree, preparing it as `Ctrl-O` does. A worktree opens
its directory. In a grouped worktree's view the action uses the lit block:
the group's folder, or the member's directory. Tabs use the repository name,
`repository !42`, `repository · branch`, or the group folder's name.

## Commands and sessions

`internal/mux` detects `ZELLIJ`, `ZELLIJ_SESSION_NAME` and the installed
launcher. Every command targets that named session. Splits use
`new-pane --direction right` or `down`, `--cwd`, `--close-on-exit` and
`--near-current-pane`, anchored by the caller's `ZELLIJ_PANE_ID`.

A tab uses `new-tab --cwd --name --layout-string` with one pane, the editor's
exact command and arguments and `close_on_exit true`. The generated KDL
keeps arguments separate and escapes quotes, backslashes and control
characters. An explicit layout is necessary: a user's default layout can
otherwise replace the requested editor with its own command. No shell
command is assembled and no layout file is left on disk.

Zellij 0.45.1 returns a pane ID for a split and a tab ID for a tab, contrary
to the earlier plan. The adapter waits for `list-panes --json` to confirm
the editor's terminal pane before recording it, then focuses that pane.
Creation replies can arrive before the pane appears in the list.

Each session record stores `Mux`, `Pane`, `MuxSession` and `MuxLauncher`,
alongside its existing editor and repository metadata. It lives while its
pane lives, even after the writing unagit exits. Readers query each Zellij
session once per snapshot, exclude plugins and exited panes, and sweep
closed pane records. An unavailable or unreadable server reply keeps the
record for a later retry. Missing sessions are swept.

`unagit sessions` and `unagit cd` include these editors. Neovim panes wear
the existing open-editor marker, including in a new unagit instance.

Neovim in a pane is started as in unagit's own terminal: `--listen` and
the Ctrl-Z mapping. Ctrl-Z puts it aside - its pane closes, the server
runs on - and its record, alive while the pane **or** the socket is, then
drops the pane and is an editor aside like any other (`session.records`
writes that down, so later readers do not ask Zellij about it). One Neovim
per directory holds throughout: opening a directory whose Neovim has a
live pane in this session focuses that pane (`focus-pane-id`); one aside
comes back in unagit's terminal (`Ctrl-O`, `E`) or in a new tab or split
running `nvim --server <socket> --remote-ui`, whose pane the record then
names. When the configuration directory leaves no room for a socket path,
the pane opens without one and is still found by its pane.

**A pane out of reach.** From a unagit outside the pane's Zellij session
the pane cannot be brought forward, so a picker asks: Attach Here Too (a
second window on the same server, in this terminal) or Take Over (only
while the pane is its one window). Checked by hand with Neovim 0.12.5:

- `:detach` asked for over RPC (`--remote-expr "execute('detach')"`)
  detaches a window and the server runs on, unsaved changes kept; with two
  windows it detaches the one last in use, which is why taking over waits
  for exactly one.
- `chanclose()` on the window's channel ends the server as well when that
  window is the one Neovim was started with. Not usable.
- The server runs in a session of its own: once detached it survives its
  pane's process group getting SIGHUP and SIGTERM and its terminal closing,
  so a closed pane or an ended Zellij session leaves it running. Attached,
  it ends with its window - which is why closing a pane by hand ends it.

Opening a tab or split records a zoxide visit when that integration is on.
Unagit stays running throughout; opening is shown as a background job.
Whether panes still live is asked every 15 seconds while some Neovim is
open, and at once whenever the sessions directory changes; that read is
not a job, and draws nothing unless something changed. A
failed creation creates no session or visit. If focusing fails after
creation, the editor's session is kept and the failure is reported.

## Validation

Fake-tool tests check detection, argument boundaries, tab names and split
directions, delayed pane visibility, preparation of a missing clone or MR
branch, group and lit-member directories, terminal-only editor selection,
markers in a later unagit, failure handling and session liveness after the
writer dies. Unknown query errors are tested separately from closed panes.
The terminal-editor picker is drawn at several sizes and checked for
legibility.

An opt-in test starts a private Zellij session and configuration, opens
real commands in each placement, checks their working directory and
arguments (quotes, Unicode, newlines and shell metacharacters), focus and
natural pane closure:

```sh
UNAGIT_ZELLIJ_TEST=1 go test ./internal/mux -run TestZellijOpensRealEditors -count=1 -v
```

## Later backends

For tmux, a tab maps to `new-window`, a vertical split to `split-window -h`,
a horizontal split to `split-window -v`. The pane ID comes from
`-P -F '#{pane_id}'`; liveness comes from `list-panes -a`.

WezTerm (`wezterm cli spawn`, `wezterm cli split-pane`) and kitty
(`kitty @ launch --type=tab|window`, with remote control enabled) can use the
same placement and session vocabulary. They are outside this change.
