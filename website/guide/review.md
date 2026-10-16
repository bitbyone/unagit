# The Review Worktree

The review arrangement is the feature the whole tool exists for. Press
`Ctrl-R` on a merge request and unagit builds a worktree where:

```
HEAD = the merge base     index = the merge base     files = the merge request
```

So the whole merge request reads as **one pending, unstaged change**:

```sh
git diff        # the entire merge request, as a single diff
git status      # every file it touches, additions and deletions included
```

`C` builds the same worktree without opening anything, for a review to read
later or in Hunk with `D`.

## Why unstaged

Editors draw their gutter by comparing the file with git's index. So the index
has to be the merge base. Then gitsigns, gitgutter, `]c`, `:Gvdiffsplit` and
`:DiffviewOpen` all work on the merge request as a whole, with nothing to
configure.

An earlier design staged the change instead (index = head), and the gutter,
`git diff` and every plugin had nothing to show.

Files the merge request **adds** are entered as intent-to-add (`git add -N`),
so they read as new files instead of vanishing into untracked.

## The right base

The base is the one GitLab itself diffs against (`diff_refs.base_sha`, the
merge base), not the tip of the target branch. Otherwise a target that moved on
would show its own commits backwards in your diff. What you see matches the
forge's three-dot diff exactly.

## Your notes survive

Notes you type into the files survive reopening. Because the merge request
itself is pending by design, "has the reviewer edited something" is measured
against the head unagit last checked out, never by "is anything unstaged".

A force push on the other side is picked up on the next review. When unagit
cannot tell what is yours, it leaves the worktree alone rather than reset it.

## Review or branch?

| Key | Gives you |
| --- | --- |
| `Ctrl-R` | the review worktree: the change as pending edits, for reading |
| `Ctrl-O` | the branch worktree: the merge request's real branch, for committing and pushing |

`Ctrl-O` opens the branch as it is; `p` brings it to the merge request's head.
A review worktree is never committed: its HEAD is the merge base on purpose.

## Only what is new

When the author answers your comments in new commits, `Ctrl-L` lists the
commits, with the ones you have not reviewed marked `●` and the cursor on the
oldest of them. A rebase does not fool it: "new since the last review" is
measured with `git cherry`, so rewritten commits you saw still count as seen.

`Ctrl-R` there reopens the review with only that commit and what follows
pending: HEAD and the index go to its parent, the files stay the head. Narrowing
refuses rather than skips when you have edits.

`V` marks a merge request as reviewed without opening it, after reading it in
the browser or in Hunk, so `NEW` and the commit log count only what is pushed
after.

## The reviewer's notebook

`.incomm/`, where [Incomm](/guide/incomm-hunk) keeps comments, is the
reviewer's notebook, not part of the change. Usually it is not committed, and
then it is ignored through `info/exclude`, so git status, lazygit and editors do
not list it either. In a repository that commits it, it is set aside while the
worktree follows the merge request, and put back.

## Open it somewhere else

`Alt-R` asks which editor first. **Review with Claude Code…** and the other
agents prepare the same worktree and start the agent there. Inside Zellij or
herdr, **Review in Zellij Tab** and the like open it beside unagit. See
[Coding Agents](/guide/agents) and [Zellij, herdr, Ghostty](/guide/multiplexers).
