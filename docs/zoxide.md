# Zoxide

A plan, not yet built. [zoxide](https://github.com/ajeetdsouza/zoxide)
remembers the directories a shell goes to and ranks them by frecency, so
`z gateway` lands in the one meant. unagit opens directories all day - clones,
review worktrees, branch worktrees, groups - but it starts the editor in them
directly, so no shell ever `cd`s there and zoxide never hears of them. The
aim is both ways round: unagit tells zoxide where it went, and reads back
where the user goes.

## Decisions

- **A directory goes to zoxide when it is opened, not when it is made.**
  `Alt-P` over forty clones, or `C` down a list of merge requests, would
  otherwise put a crowd of directories into zoxide with the same score, and
  frecency would stop meaning anything. What is opened in an editor, in Yazi
  (see [yazi.md](yazi.md)) or in a multiplexer window
  ([multiplexers.md](multiplexers.md)) is a visit; a clone is not.
- **A directory unagit deletes is taken out of zoxide.** zoxide drops a
  missing directory by itself, but only lazily, and review worktrees come and
  go often - `R` removes them without asking once their merge request is
  merged. A `z review` that offers a deleted worktree is worse than none.
- **On whenever `zoxide` is found**, as Hunk and chezmoi are: a card in
  Settings › Integrations, `e` turns it off, and the choice is
  `integrations.zoxide` in `config.yaml` (a `*bool`, unset meaning "on when
  installed").
- **zoxide's own rules apply.** `zoxide add` honours `_ZO_EXCLUDE_DIRS`, so
  a user who keeps worktrees out of zoxide can; unagit adds nothing of its
  own on top.

## How it goes

**Adding.** `openEditor` (`internal/ui/editors.go`) is where every opening
action ends, terminal and window editors alike, and it already knows the
directory (`what.Dir`). It calls `a.zoxideAdd(dir)` before handing over -
a `zoxide add <dir>` started and not waited on beyond a short timeout, its
failure ignored: a visit not recorded is no reason to stop an editor
opening. The same call goes into the Yazi and multiplexer paths when they
exist, and into `unagit go` and `unagit cd` (`cmd/unagit`), which take a
shell to a directory exactly the way `z` would.

**Removing.** Directories leave the disk in several places - `RemoveProject`,
`RemoveMR`, `RemoveWorktreeDir`, `RemoveGroupMember`, a group deleted whole,
and the tidying after `R` (`cleanup.go`). Rather than chase each call site,
`workspace.Options` gets an `OnRemoved func(dir string)` that the manager
calls after a directory is gone; the UI sets it to `zoxide remove <dir>` when
the integration is on. The workspace package still knows nothing about
zoxide.

**Reading frecency back.** `zoxide query --list --score` prints a score and
a path per line. Read once when a list is drawn after a switch of tab (it
is a local file, a few milliseconds) and kept until the next switch:

- **Sort by frecency** - a new order for Repositories (a clone's score is the
  highest of its directory and every worktree under its `.unagit/`) and for
  Worktrees, beside the orders each list already has (`sorting.go`). Rows
  zoxide does not know come after, by activity, as ties do now.
- **`unagit go`** lists by frecency when zoxide is on, so the directory one
  goes to most is where the cursor starts.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/zoxide` (new) | `Add`, `Remove`, `Scores() (map[string]float64, error)`, each a short-lived `zoxide` process; the binary looked up once. |
| `internal/config` | `Integrations.Zoxide *bool`. |
| `internal/workspace` | `Options.OnRemoved`, called after every removal of a directory. |
| `internal/ui/editors.go` | `zoxideAdd` before an editor starts. |
| `internal/ui/integrations.go` | the card, with `found` saying how many directories zoxide knows under the roots. |
| `internal/ui/sorting.go` | "frecency" in Repositories and Worktrees. |
| `cmd/unagit` | `go` and `cd` add the directory; `go` sorts by score. |
| README | a paragraph under Integrations; help rows for the new order if it gets a key. |

## Tests

- A fake `zoxide` on `PATH` that appends its arguments to a file and prints
  a fixed `--list --score`. Tests that put it there are serial (`t.Setenv`).
- Opening a clone adds its directory once; cloning without opening adds
  nothing; deleting a review worktree removes it; turning the card off adds
  nothing.
- The frecency order on a fixture: a clone ranked by its busiest worktree,
  unknown rows last in their activity order.
- `internal/zoxide`: parsing of `--score` output, including paths with
  spaces and a missing binary.

## Open details

- Whether to offer a one-off "Add Every Clone to Zoxide" in `:` for a fresh
  machine, with a low score each (`zoxide add` has no score argument; it
  would need `zoxide import`, which takes another tool's database). Probably
  not worth it: a week of use fills zoxide anyway.
