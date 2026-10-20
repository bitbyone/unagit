# The Shelf

As in IntelliJ, changes can be put aside to come back to.

**Shelve Changes…** is `s` in the [Changes dialog](/guide/changes) for the
files marked there, or every file when none is, and among the actions of a
repository or a worktree for all of them. It asks for a name and puts what is
not committed aside, unversioned files too, leaving the files as the last
commit has them.

**Shelf…** lists what is there:

| Key | Does |
| --- | --- |
| `Enter` | the files |
| `D` | the changes, in Hunk |
| `u` | unshelve into the checkout it was opened from, and take it off the shelf |
| `a` | apply it and keep it on the shelf |
| `d` | delete it; Rewrite History can undo that |

**Move Changes to New Worktree…**, among a repository's actions, uses the
shelf to carry work begun in the clone elsewhere: it asks for a branch name,
makes that branch in a worktree at the commit the clone has out, and moves
what is not committed there, unversioned files too. The clone is left clean
and Worktrees comes up with the cursor on the new one. A step that fails
leaves the changes on the shelf.

An unshelve that would collide, a file not committed here too or a change the
branch has since made to the same lines, is refused before anything moves.

The shelf is git's stash, read as a list. It is the repository's, shared by its
worktrees, and what lazygit or `git stash` put there is on it too. A review
worktree is never shelved: its changes are the merge request's.
