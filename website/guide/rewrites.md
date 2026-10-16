# Rewriting History

**Every rewrite is written down.** Everything unagit does to a branch's
history is recorded: a squash, an edited message, an undone commit, a rebase
(`p`, `Ctrl-R`, **Rebase onto…**), a base set, a branch deleted.

## Rewrite History

**Rewrite History…** is `H` in a commit log or the branch manager, or among the
actions of a repository or a worktree. It lists the changes for the
repository, newest first.

| Key | Does |
| --- | --- |
| `u` | undo the change, after saying what will happen |
| `Enter` | the commits the branch had before and not after, and the other way round |
| `D` | what the files went through, in Hunk |
| `y` | copy the commit before, for going back by hand |

Undoing puts the branch back where it was, and the later changes of the same
branch go back with it.

- After a squash or an edited message only the history goes back; the files
  are the same.
- After a rebase the files go back too.
- What is not committed stays either way, and when it would collide nothing is
  done.
- A deleted branch is made again where it was, tracking what it tracked. One
  deleted only on origin is made again here, to push back if you want it there.

If origin already has what the branch leaves, the question says a force push
will be needed, and `P` then asks for it with the right lease. The undo is
written down like the rest, so it can be undone in its turn.

## Where it is kept

The record is the repository's own, in its git directory, shared by its
worktrees. Refs under `refs/unagit/rewrites` keep both states so git does not
collect them. It keeps the newest 200 changes and none older than 90 days. The
refs are never pushed.

## Recover from Reflog

Rewrite History knows only what unagit did. **Recover from Reflog…**, `R` in a
commit log or among the actions, is the net under everything else: every place
the branch has been, read from git's reflog, whatever moved it there - a rebase
in a terminal, lazygit, unagit - with what moved it.

`Enter` shows the commits the branch had there and has not now, `D` the files
against now, and `u` puts the branch back there, the files with it. Going back
is written down like any rewrite.

## In the Activity log

The same changes come to the log of the Activity screen as its local history.
`H` there narrows the log to them, and `Enter` on one opens Rewrite History on
it.
