# Editors that keep running in the background

Implemented for Neovim 0.12 and newer. Before this change a terminal editor
took the terminal and unagit waited for it to exit (`openEditor` in
`internal/ui/editors.go`: `tv.Suspend`, then `cmd.Run`). One editor at a time, and closing it was the only way back.
The aim: put Neovim aside without closing it, open another, and move between
them from a list in unagit.

## What Neovim already does

Neovim is a server with a separate UI, and since 0.12 a UI can let go of
the server with `:detach`. Checked by hand with nvim 0.12.5:

- A plain `nvim --listen <socket>`, started the way unagit starts it now,
  survives `:detach`: the UI exits, the server stays and answers on the
  socket.
- `nvim --server <socket> --remote-ui` attaches again, and what was open is
  still open - a file opened before the detach was the current buffer
  after it.
- `:qa` ends the UI and the server together, and the socket goes with them.
- The UI exits 0 either way. Whether the editor lives on can be told only by
  asking the socket afterwards.

So the first start stays what it is, with `--listen` added; only coming back
is new.

## Decisions

- **Ctrl-Z puts the editor aside.** An editor unagit starts gets
  `nnoremap <C-z> <cmd>detach<cr>` (and the same in insert and visual mode)
  through `-c`, so the user's own configuration is not touched. Ctrl-Z is
  the gesture for "out of the way for a moment", and today it is a trap:
  Neovim's suspend stops its whole process group, unagit with it, and the
  shell comes back with unagit frozen.
- **Quitting unagit leaves the editors running**, quietly - no question.
  The next unagit finds them; so does `unagit attach` from a shell.
- **The list is a picker**, `E` from any main screen and "Running
  Editors…" in `:`. No tab of its own.

## How it goes

**Opening** (Ctrl-O and every other opening action, as now). For Neovim
0.12 or newer unagit runs `nvim --listen <socket> -c <the Ctrl-Z mapping>`
in the directory. The socket lives in `<config>/sessions/`:

- the directory is 0700, and must stay so: whoever can connect to the socket
  can make Neovim run anything;
- the path stays short, since a Unix socket path on macOS is limited to
  about 104 bytes and `$TMPDIR` there is already most of that. Name it by a
  short random id, not by the project.

**Putting it aside.** Ctrl-Z (or `:detach`) ends the UI, the terminal comes
back to unagit, and unagit asks the socket:

- it answers: the editor runs on, the session record stays, and a `note`
  says "nvim aside: acme/api · E lists the running editors";
- it does not: the editor was closed, and the record is removed as now.

**The picker** (`E`, "Running Editors…"). A `showPicker` with `pickTable`
columns: repository, branch or merge request, directory, how long it has
run, and a mark for unsaved changes (`getbufinfo({'bufmodified': 1})`
through the socket). `explain` shows the record's full directory under the
list. Its keys, each with a name and an about as `pickKey` asks:

- Enter attaches: suspend the TUI, run `nvim --server <socket> --remote-ui`,
  and on return the same check as above.
- `x` closes the editor - `:confirm qa` through the socket. With unsaved
  changes it attaches instead, so the question is asked where the changes
  can be seen.

**Opening what is already open.** Ctrl-O on a repository, a merge request
or a worktree whose directory has a running editor attaches to it instead
of starting a second one: two Neovims on one directory fight over swap
files and over the review worktree. Alt-O still chooses an editor, and
choosing another (IDEA, Zed) is allowed.

**The lists** mark a row whose directory has a running editor, aside or
not, with the "open in Neovim" mark of [markers.md](markers.md).

**Quitting unagit** does nothing to them: the servers are not children of
the terminal once detached, and their records do not depend on unagit's
pid any more.

**From a shell**: `unagit attach [query]` picks a running editor the way
`unagit cd` picks a session and `exec`s the remote UI in it; `unagit
sessions` lists the editors in the background too.

## What it makes possible besides

The socket is a channel into the editor, so unagit can tell it things:

- **`:checktime` after a review worktree is reset** - a force push followed,
  or Ctrl-R narrowed it to one commit. Without it the buffers in an editor
  aside are the old files and the gutter is wrong.
- **Open a file at a line** in the editor that is already running - from a
  comment of a merge request, from Incomm.
- **Deleting a worktree** sees an editor running in it and offers to close
  that first, rather than pulling the directory from under it.

These are follow-ups, not part of the first change.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/editors` | tell that an editor is Neovim 0.12 or newer (`:detach` exists from 0.12), once per process; the command to start with `--listen` and the mapping, the one to attach, and a check whether a socket answers. Older Neovim, Vim and custom terminal editors keep today's behaviour. |
| `internal/session` | `Record` gains the socket. A record with a socket is alive while the socket answers, not while the pid that wrote it does; the sweep checks the socket and removes a dead one with its record. A record without a socket keeps today's rule. |
| `internal/ui/editors.go` | after the terminal comes back, aside or closed; an opening action attaches when the directory has a running editor. |
| `internal/ui` | "Running Editors…" with `E` in the list actions of the three main screens and in `globalActions`; the mark in the lists. Help rows for `E` and the mark; a paragraph in the README. |
| `cmd/unagit` | `unagit attach`. |

## Tests

- A fake `nvim` on `PATH` (a small Go program built in the test, or a
  script) that listens on the socket it is given, answers the few requests
  unagit makes, and either exits or stays after its "UI" ends, by an
  environment variable. Tests that put it on `PATH` are serial
  (`t.Setenv`).
- One test against the real Neovim, skipped when `nvim` is missing or older
  than 0.12: start it headless with `--listen`, attach a remote UI under a
  pseudo-terminal, `:detach`, check the server still answers, then `:qa`
  and check the socket is gone.
- `internal/session`: a record with a dead socket is swept, a live one is
  kept although the pid that wrote it is gone, and the socket directory is
  0700.
- The picker gets a layout test at several sizes, as every new dialog does.

## Open details

- How to ask the socket: `nvim --server <socket> --remote-expr` costs a
  process per question; a direct msgpack-RPC connection is faster but a
  dependency. The first version uses a process for these few requests after
  a key, and a direct Unix socket connection to check liveness. A busy editor must
  not lose its record just because it cannot handle an RPC request yet.
- A Neovim left aside for days holds its LSP servers. The picker shows how
  long each has run; nothing closes one on its own.
