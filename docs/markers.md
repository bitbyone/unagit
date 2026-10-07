# Watched and open in Neovim: marks in the lists

A plan, not yet built. It belongs with [watched.md](watched.md) and
[background-editors.md](background-editors.md): both make something true of a
row that the lists do not show today. A repository, a merge request or a
worktree that is **watched**, or that is **open in Neovim** - in this
terminal, put aside with `:detach`, or in a multiplexer's tab or split
([multiplexers.md](multiplexers.md)) - wears an icon in its own colour, so
it is seen at a glance and not only on the Watched screen or in `unagit
sessions`.

## Decisions

- **Two marks, each a glyph and a colour role of its own.**

  | Mark | Nerd Font (`nerd_glyphs`) | Plain (`glyphs`) | Role, falls back on |
  | --- | --- | --- | --- |
  | watched | `󱣾` (U+F18FE) | `◎` | `mark.watched` → `text.accent` |
  | open in Neovim | `` (U+EE9B) | `▣` | `mark.editor` → `state.on` |

  The plain characters are not used for anything else (`○ ● ◐ ◉ ◆` are the
  disk and group marks), and both read without a Nerd Font. Each goes into
  `Theme` (`themes.go`), `themes/unagit.json` - written there as the pairs
  above, the way the other `nerd_glyphs` are - and `glyphs()`; each role
  into `themeroles.go`. Coloured, so that they stand out from the muted
  disk marks; a theme that wants them quieter names the role.
- **A column of their own, "marks".** A `listColumn` in Repositories, Merge
  requests and Worktrees, right after the on-disk mark, two cells wide - a
  row can be both watched and open. Like any column it is left out when no
  row of the list has a mark, so a user who neither watches nor keeps
  editors open loses nothing, and `v` can hide it.
- **What a row counts as.**
  - *watched*: a merge request with a watch on it; a repository whose
    clone's branch is watched; a worktree whose branch is watched; a group
    when any of its repositories' branches is.
  - *open in Neovim*: a live session record whose directory is the row's -
    the clone for a repository, the branch or review worktree for a merge
    request, the worktree or group folder for a worktree. A repository is
    not marked for an editor open in one of its worktrees; the worktree's
    own row is.
- **Neovim only, for now.** The session record does not say today which
  editor it is; it gains `Editor` (the id from `internal/editors`). The mark
  is for terminal Neovim - the one that can be aside or in another pane
  without a window to see it in. A window editor has its own window on
  screen, and a record that is only vouched for until unagit exits; marking
  it too is a one-line change if it turns out to be wanted.
- **The marks are drawn, never copied.** `y` and `/` see the row without
  them, as with the server icons.

## Knowing what is open, cheaply

The lists are drawn often; a draw must not read files or ask sockets. The
App keeps `openDirs map[string]session.Record`, refreshed in the background
and never during a draw:

- when the terminal comes back from an editor, on a tab switch and when
  the terminal regains focus (the moments `RMT` is read again today);
- every few seconds while unagit runs, by a `stat` of the `sessions/`
  directory - a record written or removed by another unagit changes its
  modification time. Only then are the records read.
- A record with a socket (background editors) or a pane (multiplexers) is
  alive while that answers; asking is a process, so it is done in the same
  background read, not per row.

The watched set comes from `watches.json`, which the Watched machinery
already follows (see watched.md); the lists read the App's copy.

## Stopping a watch from the Watched screen

The Watched screen lists every watch, and stopping one is done there as well
as from the row it watches:

- `x` on a row stops that watch, after no question - a watch costs nothing
  to start again;
- space marks rows (the screen is `markable`, as Repositories is), and `x`
  then stops every marked one;
- "Stop Watching" is in the row's action picker, "Stop Watching All" in
  `:`, which asks first.

The row disappears at once, the mark in the other lists goes with it, and
any other unagit open sees it within its next look at `watches.json`.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/session` | `Record.Editor`; written by `openEditor`. |
| `internal/ui/themes.go`, `themes/unagit.json`, `themeroles.go` | the two glyphs, Nerd and plain, and the two roles. |
| `internal/ui/columns.go` and the three lists | the marks column. |
| `internal/ui/app.go` | `openDirs` and its background refresh. |
| `internal/ui/watched.go` | `markable`, `x` on marks, the two actions. |
| `internal/ui/help.go` | a row for each mark, as the disk marks have. |
| README | the marks beside the on-disk markers. |

## Tests

- A session record for a review worktree marks its merge request and the
  worktree's row, and not the repository; removing the record removes the
  mark after the next background read.
- A record written by another process (a file put into `sessions/` by the
  test) is picked up without a key being pressed.
- A watch marks its row in each list; stopping it on the Watched screen,
  alone and by marks, clears the mark everywhere.
- The column is absent with no marks and present with one; both marks on
  one row fit; the glyphs come out plain with Nerd Fonts off.
- `TestTheDefaultThemeIsTodaysLook` and `TestAThemeOfEachKindIsLegible`
  with the new keys; the marks drawn on a selected and on a marked row keep
  their colour.

## Open details

- Whether the marked rows of the lists also want a count in the header
  ("3 open"). Probably not: the header is for what the list is, not for
  what is around it.
