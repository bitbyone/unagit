# Keeping Worktrees Current

A branch unagit makes, in a grouped worktree or with `n` in the `Ctrl-W`
picker, remembers the branch it was made from:

```sh
git config branch.<name>.unagitBase
```

## Before it is pushed

Until it is pushed, Worktrees measures the branch against origin's copy of that
base. `RMT` says `↓2 behind main`, and `p` rebases it onto it.

## After it is pushed

Once pushed, a branch has an upstream of its own and follows that instead, as a
clone does in Repositories. A pushed branch is never rebased onto its base by
`p`, since that would rewrite what origin has.

The rules are the same as in Repositories: a fast-forward when nothing local is
in the way, a rebase otherwise, and nothing at all when that would conflict.
`R` fetches first, `Alt-P` updates every worktree.

## Conflicts forecast

Whether a rebase would stop on a conflict is worked out before anything is
tried: git merges the two in memory. So `RMT` says `↓2 behind main · conflicts`
ahead of time, the detail names the files, and `p` or `Ctrl-R` refuse, saying
where.

## Rebasing on request

`Ctrl-R` goes further. It puts the branch, its commits and its uncommitted
edits, on top of its base as it is now, pushed or not, so that it reads as made
from today's base. It is done only when it goes through without a conflict.

A pushed branch then differs from origin's copy, and its row says
**force push required**. `P` asks, then pushes with `--force-with-lease` set to
exactly what origin had before the rebase, so a commit someone pushed in the
meantime makes git refuse instead of being lost.

## A branch made elsewhere

A branch made outside unagit has no base, so `Ctrl-R` is not offered for it.

- **Set Base…** (in `Alt-Enter`) picks one from the repository's branches and
  records it as unagit would have.
- **Rebase onto…** rebases the branch onto any branch, once, under the same
  rules as `Ctrl-R`, and leaves the base as it was.

## Pipelines and edits

`✎` counts the files not committed, so work in progress shows before it is
committed. `CI` is the newest pipeline of the branch, and `J` lists its jobs. A
merge request open from the branch has its own pipeline in Merge requests, and
`m` goes there.
