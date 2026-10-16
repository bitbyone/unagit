# Branches

`b` on a repository manages its branches. Each says:

- whether it is in the clone, on origin or both, and how far apart (`↑2 ↓1`);
- whether it is checked out, and where;
- whether it is the default or protected;
- whether a merge request is open from it (`!12 open`).

## Switching

`Enter` switches **that same clone** to the branch, so there is one working
copy per repository, not a directory per branch. It is a plain checkout:
nothing is fetched and no editor starts (`p` pulls, `Ctrl-O` opens), and the
list closes with the row showing the branch now out.

## Creating

`n` makes a new branch in the clone, starting from the one under the cursor or
another you choose. It appears in the list with the cursor on it, and `Enter`
switches to it when you want.

## A worktree for a branch

`Ctrl-W` gives a branch a worktree of its own instead (`n` for a new branch).
A branch git will not check out again, out in the main clone or in another
worktree, is listed last, dimmed, with where it is out. Nothing opens: unagit
moves to Worktrees with the cursor on it, and `Ctrl-O` opens it when you want.

## A merge request from a branch

`m` on a branch without a merge request opens one from it, pushing it first if
origin lacks it.

## Deleting

| Key | Deletes |
| --- | --- |
| `d` | in the clone; origin keeps it |
| `D` | in the clone and on origin |
| `Alt-D` | on origin only; the clone keeps it, no longer tracking |

The default branch and protected ones are never deleted, and one with an open
merge request is not deleted on origin. What would be lost, commits and
uncommitted edits, is counted in the question first. A deleted branch can be
made again from [Rewrite History](/guide/rewrites).

## Its base

A branch unagit makes remembers the branch it was made from, in
`branch.<name>.unagitBase`. That is what `p` rebases it onto until it is
pushed. See [Keeping Worktrees Current](/guide/keeping-current).
