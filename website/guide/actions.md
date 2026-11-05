# Actions and Keys

**Nothing has to be remembered.** Every list, view and dialog describes what
it can do as a list of actions: a name, a sentence about it, and its key. The
same list answers the keys and fills the pickers, so a key cannot do one thing
while the picker says another.

## The two action pickers

| Key | Lists |
| --- | --- |
| `Alt-Enter` or `Ctrl-A` | everything that can be done with the row under the cursor, the marked rows, the lit block of a worktree's view, or the section of Settings |
| `:` | everything the screen itself can do |

`Ctrl-A` is there for terminals that keep `Alt-Enter` for themselves.

Each action has its key beside it, so the list teaches the keys as it is used.
What one usually comes for is at the top; what destroys something is at the
bottom. A pane at the bottom says in a sentence what the action under the
cursor does.

The picker opens typing into its filter, so an action is found by its name or,
failing that, by what it does. `Enter` does it, the arrows move, and `Esc` goes
to the list (`j`/`k`), or with nothing typed, closes it.

Some actions are rare enough to have no key at all and live only in the
pickers: creating a repository, closing a merge request without merging.

### Short words

Opening actions can be found by short words shown under the picker:

| Type | Finds |
| --- | --- |
| `cc`, `claude` | Claude Code |
| `cx`, `codex` | Codex |
| `vs`, `vsplit`, `split` | a vertical split |
| `hs`, `hsplit` | a horizontal split |
| `tab` | a tab |
| `rev` | a review |

So `cc split` puts Claude Code in a vertical split first.

## Your own keys

A terminal or a multiplexer can keep a key for itself before unagit sees it:
Zellij takes `Ctrl-O`, `Ctrl-T`, `Ctrl-G`, `Ctrl-S`, `Ctrl-P` and `Ctrl-N`
for its modes, Windows Terminal `Alt-Enter` for full screen, tmux and GNU
screen their prefix (`Ctrl-B`, `Ctrl-A`). `keybinds.toml` in unagit's
configuration directory, beside `config.yaml`, gives any action another key,
several, or none:

```toml
[global]
actions = ["ctrl+space", "alt+enter"]   # the selection's action picker
screen_actions = [":", "f2"]            # the screen's

[repositories]
open = "ctrl+e"
open_with = "alt+e"
toggle_grouping = []                    # no key, the pickers still have it

[merge_requests]
review = ["ctrl+v", "f5"]
```

An action is known by its name in snake case, Open With… being `open_with`,
in the section for where it is done:

| Section | Where |
| --- | --- |
| `global` | everywhere the action is, and the two pickers |
| `repositories`, `merge_requests`, `worktrees`, `activity`, `sandboxes` | the lists |
| `worktree_view` | a worktree's view |
| `changes` | the Changes dialog |
| `log`, `branches` | the commit log and the branches |

A section's own line wins over `[global]`. A key given to an action is taken
from whatever had it there, which then has none. `unagit keys` prints every
action with its key in the shape the file takes, to copy from;
`unagit keys --check` says what is wrong in the file. unagit says that too
when it starts, and a wrong line keeps unagit's own key while the rest of the
file holds.

Keys are written the way Ghostty writes them:

| Write | For |
| --- | --- |
| `ctrl+e`, `ctrl+space`, `ctrl+\` | Ctrl with a letter, space, `\`, `]`, `^` or `_` |
| `alt+o`, `alt+enter`, `alt++` | Alt with any character, or with Enter |
| `f5`, `shift+f5`, `insert`, `delete` | the function keys and the like |
| `alt+pageup`, `ctrl+home` | the keys that move, with a modifier |
| `P`, `+`, `shift+p` | a plain character |

A key a terminal sends as another one is refused with the reason: `ctrl+h` is
Backspace, `ctrl+i` Tab, `ctrl+m` Enter, `ctrl+shift+o` arrives as `ctrl+o`,
and Alt is told apart without case. So are the keys every list keeps for
itself: `j`, `k`, `g`, `G`, `h`, `l`, `q`, `/`, `?`, space, the arrows, Tab,
Esc and the tab numbers.

The pickers, the hints, the messages and the `?` help show the keys as
bound. Settings keeps its keys. `keybinds.toml` is a choice like
`config.yaml`, so it belongs in a dotfiles repository beside it.

## Help

`?` in a main view shows its keys. Those for where you are stand in normal text;
the rest are dimmed. Inline hints at the bottom of dialogs name what can be done
there.

## Forms

Every form works like the lists. In **NORMAL** mode, `j`/`k` (or `Tab`) move
from field to field and over the buttons, and a button is pressed by its
letter, lit in its label. `i` or `Enter` types into a field (**INSERT**), and
`Esc` stops typing. A form that starts with a text field opens typing into it.

A select box opens with the arrows or `Enter`; typed letters do not search it.

## Dialogs are a stack

Whatever you cancel goes back to the dialog it was opened from, and that one to
its own, down to the screen. A picker closes before what it opens and leaves
its cursor, filter and marks as they were, so coming back finds it where you
left it.

## Messages

- A **warning** or an **error** comes up in a small box over whatever is in
  front, and holds the keys until `Esc`.
- A **note** or a **success** is a passing word at the right end of the status
  line, or beside the bottom edge of the dialog in front.
- **News from the background**, a watched pipeline say, is a toast in the
  bottom right corner that takes no key and goes by itself.

The full table is in [Keys](/reference/keys).
