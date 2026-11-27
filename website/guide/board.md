# The Board

`6` is your tasks as cards in three columns: **To do**, **In progress** and
**Done**. It is not a task manager - there are no due dates, priorities,
labels or people to assign, which other tools do well. A card is a title, a
description, a colour if you want one, links and your own comments: what
you are doing, where you left it, and what you found on the way.

```
 To do 2                          │  In progress 1                    │  Done 1
──────────────────────────────────┼───────────────────────────────────┼──────────────────────────────────
╭─ ○ Rate limiting · !42● ⎇ ↗ ─╮ │ ╭─ ● Fix login redirect · ⎇ ▣ ╮ │ ╭─ ✓ Bump Go ──── done 1h ago ╮
╰───────────────────────────────╯ │ ╰──────────────────────────────╯ │ ╰─────────────────────────────╯
╭─ ○ Audit log DLQ ─────────────╮ │                                  │
╰───────────────────────────────╯ │                                  │
```

## Cards

| Key | Does |
| --- | --- |
| `h` `l`, `j` `k` | from card to card: across the columns - past one with no card - and down a column |
| `n` | a new task at the top of To do |
| `Enter` | the task in full |
| `o` | carry on: back into the task's work (below) |
| `H` `L` (Shift `h` `l`) | move the card to the column before or after |
| `K` `J` (Shift `k` `j`) | move it up or down its column: the order is what comes first |
| `x` | done; in History, open it again |
| `c` | comment on it |
| `e` | edit its title and description (Markdown) |
| `i` | link something to the task: a repository, a merge request, a worktree, a sandbox, a sandbox's task or an address |
| `I` | the task's links and what each is now: `Enter` goes to one in its list, `w` opens its page, `d` takes it off |
| `w` | open a link's page in the browser, a list when there are several |
| `#` | give the card a colour, or take it away |
| `d` | delete the task, after asking |
| `v` | the cards in full - the description, the latest comment, then each link on a row with what it is now - as they are at first, or compact: the title, the comments counted and the links as marks in their states' colours |
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
through. `E` edits the task, `o` carries on, `i` links, `I` lists the
links, `w` opens one's page, `H` `L` and `x` move it. Nothing is published
anywhere: a task's comments stay in the task.

## Links

A task links to what unagit knows: a repository, a merge request, a
worktree (a grouped one too), one of unibox's sandboxes, a task in a
sandbox, and an address for anything else. Link from either end: `i` on a
card picks what to link, and **Add to Task…** - in the actions of a
repository, a merge request, a worktree, a sandbox and a sandbox's task,
`.` or the right button - picks the task, the open space's first, the
other spaces' after them, and **New Task…** named after it before them all.
It has no key of its own; bind one in `keybinds.toml` (`add_to_task`) if
you use it often.

A link is alive. What it is comes from what unagit has already read for
its lists, never asked of a server for the Board's sake: a merge request's
state, pipeline and approvals from Merge requests, a worktree from the
disk, a sandbox from unibox. A merge request the list no longer has is
asked of its server once. A link outlives what it links to: a worktree
removed, a merge request merged or closed, a sandbox deleted stays on the
card, dimmed, saying what became of it and when, and the change goes into
the task's story. Only you take a link off (`I`, then `d`). A merge
request merged while its task is not done marks the card **merged ·
done?**; the card never moves by itself.

Editors and agents are not linked, they are found: one open in a task's
worktree, its repository's clone or its merge request's worktrees shows
on the card, and so does an agent at work on its sandbox task. A watched
merge request's latest news shows under its links.

## Carry on

`o` takes you back into the work, in this order:

1. to an editor or agent already open in it: Neovim attached, the pane
   brought forward in Zellij, herdr, Ghostty or Rex, as Activity's cards do;
2. else it opens the task's worktree, its merge request's worktree or
   review, or its repository's clone, with your editor; a sandbox task is
   resumed;
3. else, with only a merge request, it starts its review;
4. else it opens the task's address.

With more than one at the same step it asks which. A task to do moves to
In progress as you carry on, unless **Settings › General › Carry on
starts** is off.

## Done and History

A task done leaves the board after the time set in **Settings › General ›
Hide done after** - a day unless you say otherwise; never, if you would
rather clear it yourself. Nothing is deleted: `y` shows History, every
task done, the newest first; `x` there opens one again, and `y` or `Esc`
goes back to the board.

Marking a task done asks what of what it linked can go now: its
worktrees, its merge requests' review and branch worktrees, its sandboxes
running, everything safe ticked. A worktree with work in it - files not
committed, commits not pushed, edits in a review - is named with why, and
not offered. **Settings › General › Tidy up on done** turns the question
off.

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
