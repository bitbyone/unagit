# Yazi

A plan, not yet built. [Yazi](https://yazi-rs.github.io) is a terminal file
manager. Two halves: unagit opens a directory in Yazi the way it opens one
in Neovim, and Yazi gets a plugin that jumps to anything unagit has on disk.

## Does Yazi feed zoxide by itself?

Not by default. Yazi's built-in `zoxide` plugin is for jumping *from* zoxide;
adding the directories Yazi visits is its `update_db` option
(`require("zoxide"):setup { update_db = true }` in `init.lua`), off unless the
user sets it. To be checked against the installed Yazi before building -
the option has moved between releases.

So unagit does its own part regardless (see [zoxide.md](zoxide.md)): the
directory it opens Yazi in is added like any other opened directory, and so
is the directory Yazi ends in, which unagit learns from `--cwd-file`. What
the user walks through in between is Yazi's to record, and the README points
at `update_db` for it.

## Part one: Yazi from unagit

**An action, not an editor.** Yazi could be listed in `internal/editors` as
a terminal editor and get `Alt-O` and the sessions for free, but it would
then be a candidate favourite editor, which it is not. It is an action of
its own, "Browse Files", on a repository, a merge request (its worktree,
review first when there is one), a worktree and a block of the worktree
view. Proposed key `Ctrl-Y`, free on every list. Too rare for a key? Then
`keys: ""` and it lives in the pickers alone.

**Running it** reuses what `openEditor` does for a terminal editor -
`sessions.Open`, `tv.Suspend`, run, refresh the disk afterwards - so that
part of `openEditor` is pulled out into `runInTerminal(dir, what, cmd)` and
both call it. The command:

```
yazi --cwd-file <tmp>/cwd --chooser-file <tmp>/chosen <dir>
```

the two files in a fresh 0700 temporary directory, removed afterwards.

**After it exits:**

- `cwd`: the directory Yazi was in last. When it differs from `<dir>`, it
  goes to zoxide (when on).
- `chosen`: Enter on a file in Yazi, with `--chooser-file`, writes the path
  and quits instead of opening it. unagit then opens the favourite editor
  **on that file** in the same directory - so Yazi becomes the way to start
  a review on one particular file. This needs `editors.Editor.CommandAt(dir,
  file)`: `nvim <file>` with the directory as working directory, `code
  <dir> <file>`, `zed <dir> <file>`, `idea <dir> --line 1 <file>` (to be
  checked for each). Without a chosen file nothing more happens.
- The session record is the directory Yazi was opened in, for as long as
  it runs, as for Neovim.

**Card.** Settings › Integrations › Yazi, on when found, `e` off. Its
`found` line says whether the zoxide plugin's `update_db` is set - read
from `~/.config/yazi/init.lua` by a plain search, a hint and nothing more.

## Part two: unagit from Yazi

A plugin, `unagit.yazi`, kept in `contrib/yazi/unagit.yazi/` and installable
with `ya pkg add` from the repository. It does what Yazi's own `z` does for
zoxide: hide Yazi, let unagit's picker choose, go there.

```lua
-- main.lua, sketch; the API names follow Yazi 25.x and need checking.
return {
  entry = function()
    local permit = ui.hide()
    local out, err = Command("unagit"):arg({ "go", "--print" })
      :stdout(Command.PIPED):stderr(Command.INHERIT):output()
    permit:drop()
    if not out or not out.status.success then return end
    local dir = out.stdout:gsub("%s+$", "")
    if dir ~= "" then ya.emit("cd", { dir }) end
  end,
}
```

`unagit go --print` already draws its picker on `/dev/tty` and prints only
the path, so nothing in unagit changes for this. A second entry,
`unagit.yazi sessions`, does the same with `unagit cd --print` - only what is
open in an editor right now.

The README gives the keymap line (`{ on = ["g", "u"], run = "plugin unagit" }`).

### Later

A Yazi linemode or fetcher that marks a directory unagit made - `◐ !42` for a
review worktree, read from `git config --worktree unagit.mr.iid` and
`unagit.mr.mode`. It costs a git process per directory shown, so it would
need Yazi's fetcher caching; not part of the first change.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/ui/editors.go` | `runInTerminal`, shared by terminal editors and Yazi. |
| `internal/ui/yazi.go` (new) | the action, the temporary files, what happens after. |
| `internal/editors` | `CommandAt(dir, file)` for each known editor and the custom one (the custom editor gets the file as a last argument). |
| `internal/config` | `Integrations.Yazi *bool`. |
| `internal/ui/actions_lists.go`, `wtmodal.go` | "Browse Files" in the four places. |
| `internal/ui/integrations.go` | the card. |
| `contrib/yazi/unagit.yazi` | the plugin and its README. |
| README, `help.go` | the key and the plugin. |

## Tests

- A fake `yazi` on `PATH` (serial) that writes given paths into its
  `--cwd-file` and `--chooser-file` and exits: a chosen file opens the
  favourite (a fake terminal editor) on that file; a different cwd reaches
  the fake zoxide; nothing chosen opens nothing.
- `CommandAt` for each editor.
- The plugin is checked by hand; there is no Yazi in the test run.
