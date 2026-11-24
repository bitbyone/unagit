# The Board

`6` is your tasks as cards in three columns: **To do**, **In progress** and
**Done**. It is not a task manager - there are no due dates, priorities,
labels or people to assign, which other tools do well. A card is a title, a
description, a colour if you want one, links and your own comments: what
you are doing, where you left it, and what you found on the way.

```
 To do 2                          │  In progress 1                    │  Done 1
╭─ ○ Rate limiting · ◇2 ↗1 ─────╮ │ ╭─ ● Fix login redirect · ◇1 ──╮ │ ╭─ ✓ Bump Go ──── done 1h ago ╮
╰───────────────────────────────╯ │ ╰──────────────────────────────╯ │ ╰─────────────────────────────╯
╭─ ○ Audit log DLQ ─────────────╮ │                                  │
╰───────────────────────────────╯ │                                  │
```

## Cards

| Key | Does |
| --- | --- |
| `h` `l`, `j` `k` | from column to column, from card to card |
| `n` | a new task in the column the cursor is in, at its top |
| `Enter` | the task in full |
| `H` `L` (Shift `h` `l`) | move the card to the column before or after |
| `K` `J` (Shift `k` `j`) | move it up or down its column: the order is what comes first |
| `x` | done; in History, open it again |
| `c` | comment on it |
| `e` | edit its title and description (Markdown) |
| `i`, `w` | link a URL · open the link in the browser, a list when there are several |
| `#` | give the card a colour, or take it away |
| `d` | delete the task, after asking |
| `v` | the cards in full - the description, the links and the latest comment, as they are at first - or compact: the title, and how many comments and links |
| `/` | find a card by its title, description, links or comments |

A colour makes a card stand out - red for what must not wait - and means
nothing else: nothing is sorted or filtered by it. The mouse does what the
keys do: a click chooses a card, a double click opens it, the right button
lists what can be done with it, and the wheel moves the cursor. A card
dragged goes where it is dropped: up or down its column, or to another
column at the line shown between the cards there.

## A task

`Enter` opens the task as a merge request's conversation opens: its state,
colour, description and links at the top, then your comments as cards, with
the lines of its story between them - created, moved, linked, edited - in
the order things happened. The keys are the conversation's: `j` `k` go from
comment to comment, `c` writes one, `e` and `d` change the chosen one, `v`
selects in it and `y` copies, `h` hides the story to read the comments
through. `E` edits the task, `i` links, `w` opens the link, `H` `L` and `x`
move it. Nothing is published anywhere: a task's comments stay in the task.

## Done and History

A task done leaves the board after the time set in **Settings › General ›
Hide done after** - a day unless you say otherwise; never, if you would
rather clear it yourself. Nothing is deleted: `y` shows History, every
task done, the newest first; `x` there opens one again, and `y` or `Esc`
goes back to the board.

## Spaces

Tasks are scoped as your work is: per customer, per employer, per project.
A space is a board of its own, named by you; every task is in one. There is
**Task Board** to begin with.

`p` lists the spaces: `Enter` opens one, `n` makes one, `r` renames it, `d`
deletes it - its tasks moved to another space or deleted with it, as you
choose - and `m` moves the task under the cursor there. `]` and `[` go to the
next space and the one before without the list. The board remembers the
space it was on.

## Where it is kept

The tasks are this machine's, not configuration: they live beside the
indexes, in `~/.config/unagit/board/`, a directory a space and a file a
task. Two unagit open at once see each other's changes within a couple of
seconds. **Settings › General › Start on** opens unagit on the Board, for
whoever uses it as the place to start from.
