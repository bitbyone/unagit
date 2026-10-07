# tmux and zellij

A plan, not yet built. A terminal editor takes unagit's terminal: the TUI is
suspended until the editor exits, one editor at a time. Inside tmux or
zellij there is a better place for it - a tab or a split of its own, with
unagit still running beside it.

## Decisions

- **Three actions, nothing else.** "Open in New Tab", "Open in Vertical
  Split" and "Open in Horizontal Split", in the action pickers of a
  repository, a merge request, a worktree and a block of the worktree view.
  No keys (`keys: ""`), no setting: `Ctrl-O` and the other opening keys keep
  suspending unagit as now, and a tab or a split is what one asks for by
  name. Their `when` is "unagit runs inside tmux or zellij", so outside one
  the pickers do not offer them.
- **Only when unagit itself runs inside one.** `$TMUX` says tmux, `$ZELLIJ`
  zellij.
- **The split names follow Vim.** A vertical split puts the editor *beside*
  unagit (a vertical divider, `:vsplit`), a horizontal one *below* it. tmux
  names them the other way round (`split-window -h` is side by side), so the
  mapping is written down once, in `internal/mux`, and the `about` of each
  action says where the editor goes.
- **The editor is the favourite terminal editor.** When the favourite opens a
  window of its own (IDEA, VS Code, Zed) a tab makes no sense; the action
  then asks among the terminal editors, as `withEditor` does when there is
  no favourite.
- **What it opens is what `Ctrl-O` would open.** A merge request opens its
  branch worktree (made first if need be); a repository its clone (cloned
  first). The preparation runs as it does now; only the last step - where
  the editor starts - differs.
- **The tab is named after the row** - `api-gateway !42`, `api-gateway ·
  feat/rate`, the group's folder. A split has no name.

## How it goes

**tmux.**

```
tmux new-window   -P -F '#{pane_id}' -c <dir> -n <name> -- <editor command>
tmux split-window -h -P -F '#{pane_id}' -c <dir> -- <editor command>   # vertical
tmux split-window -v -P -F '#{pane_id}' -c <dir> -- <editor command>   # horizontal
```

each printing the new pane's id. Whether the pane still lives is `tmux
list-panes -a -F '#{pane_id}'`.

**zellij.**

```
zellij action new-tab --cwd <dir> --name <name> --layout <generated layout>
zellij action new-pane --direction right --cwd <dir> -- <editor command>
zellij action new-pane --direction down  --cwd <dir> -- <editor command>
```

the layout a temporary file with `tab { pane command="..." cwd="..." }`.
zellij's actions have changed between 0.40 and 0.43; the exact commands are
checked against the installed version before the code is written. zellij
does not hand back a pane id, so its records are kept as a window editor's
are: until unagit exits.

**Sessions.** The record (`internal/session`) gains the multiplexer and the
pane id. A record with a pane is alive while the pane is, not while the pid
that wrote it is - the same change the [background editors](background-editors.md)
plan makes for a socket, and the two should share it. So `unagit cd` and
`unagit sessions` know an editor in a tmux pane even after the unagit that
started it has quit.

**Zoxide.** A directory opened in a tab or a split is a visit like any other
([zoxide.md](zoxide.md)).

## How it relates to background editors

Inside a multiplexer the background editors plan matters less: the editors
already run side by side, and the multiplexer is the list of them. The
session record with a liveness check of its own is the part both plans want;
whichever is built first adds it.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/mux` (new) | detect tmux or zellij; open a tab, a vertical or a horizontal split in a directory with a command; tell whether a pane lives. One interface, two implementations. |
| `internal/session` | `Record.Mux`, `Record.Pane`; liveness by pane when set. |
| `internal/ui/editors.go` | `openEditor` takes where to open: here (today), a tab, a split. |
| `internal/ui/actions_lists.go`, `wtmodal.go` | the three actions in the four places. |
| README | a paragraph under Integrations. No help rows: the actions have no keys. |

## Tests

- A fake `tmux` on `PATH` (serial, with `TMUX` set) that records its
  arguments and answers `list-panes` from a file the test writes: each
  action runs the right command in the row's directory, the tab named after
  the row; without `TMUX` the pickers do not offer them; a window editor as
  favourite asks among the terminal ones.
- The same for zellij once its commands are settled.
- `internal/session`: a record with a living pane outlives its pid; a dead
  pane is swept.

## Open details

- WezTerm (`wezterm cli spawn`, `wezterm cli split-pane`) and kitty
  (`kitty @ launch --type=tab|window`, which needs remote control on) fit
  the same interface. Not in the first change, but `internal/mux` should not
  assume a multiplexer is tmux or zellij.
- Whether a pane closes with its editor (the default of both) or drops to a
  shell. Closing; a shell would need `<editor>; exec $SHELL`.
