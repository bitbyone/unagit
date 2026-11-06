# Actions and Keys

**Nothing has to be remembered.** Every list, view and dialog describes what
it can do as a list of actions: a name, a sentence about it, and its key. The
same list answers the keys and fills the pickers, so a key cannot do one thing
while the picker says another.

## The two action pickers

| Key | Lists |
| --- | --- |
| `.` or `Alt-Enter` | everything that can be done with the row under the cursor, the marked rows, the lit block of a worktree's view, or the section of Settings |
| `:` | everything the screen itself can do |

`Alt-Enter` is kept for those used to it; a Mac's terminal and Windows
Terminal keep it for themselves, so `.` is the one that always arrives.

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

## The mouse

The mouse does what the keys do, nothing of its own:

| Do | And |
| --- | --- |
| click | the cursor goes to the row, the section of Settings or the field; on the bar, to that tab |
| double click | `Enter` |
| right click | the actions of what it is on, as `.` lists them |
| Ctrl-click, Alt-click | mark or unmark the row, as space does |
| Shift-click | mark from the cursor to the row |
| drag over rows | mark every row it passes over |
| `×` in the detail's corner | close the detail column |
| wheel | scroll |

A click beside a dialog closes it, as `Esc` does, typing or not, and
reaches nothing beneath it. While unagit has the
mouse, the terminal selects text with Shift-drag (Option-drag in iTerm);
Settings › General › **Mouse** gives the mouse back to the terminal.

## Your own keys

The keys unagit ships keep out of the way of the terminals and multiplexers:
none needs Alt, which a Mac's terminal sends as a character, and Ctrl is only
`Ctrl-E`, `Ctrl-L`, `Ctrl-R` and `Ctrl-W`, which Zellij, tmux, GNU screen,
Windows Terminal and the editors' terminals leave alone. Navigation comes
first; then the letter is the action's own, small for what stays local and
capital for the bigger form of the same verb (`p` pull, `P` push). Yours may
still clash with something of your own: `keybinds.toml` in unagit's
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

The file is not there until you make it. To see everything there is,
Settings › Keys › `w` (or `unagit keys --update`) writes it with every
action, a line saying what it does and its key, commented out: uncomment a
line and change the key. `e` there opens the file in the editor, and `r`
reads it again and puts its keys on with no restart.

A release that adds actions or changes a key leaves the file as it is, and
Settings › Keys says what it would bring. `u` (or `unagit keys --update`
again) brings it: the commented lines are unagit's, so they take the new
keys and descriptions, the new actions are added where they belong, and the
ones gone are dropped. A line you uncommented is yours and stays, with any
note of yours beside it; a line whose action is gone is said at start, as
any other slip. A comment right above an action that starts with its name
is unagit's description; a note of your own goes under it or on the line.

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
