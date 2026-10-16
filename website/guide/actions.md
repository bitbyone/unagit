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
