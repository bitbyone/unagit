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
editor unagit's own terminal. These actions use the favourite terminal
editor; if the favourite is missing or opens a window, the shared picker
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

Neovim in a pane is started with `--listen` (without the Ctrl-Z of one put
aside: its pane is where it waits), so unagit can open a file in it, ask
about unsaved changes and close it. One Neovim per directory holds here
too: opening a directory whose Neovim has a live pane - `Ctrl-O`, a split,
a file chosen in Yazi - focuses that pane (`focus-pane-id`) instead of
starting a second one, and from another Zellij session names the session
instead. `E` lists these editors; Enter focuses the pane. A Neovim aside in
unagit's own terminal is not moved into a pane: a split on its directory
says to attach with `E`. When the configuration directory leaves no room
for a socket path, the pane opens without one, and is still found by its
pane.

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
