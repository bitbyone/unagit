# Where Things Land

**Where things land is yours to decide.** A repository is cloned under the
first of:

1. its exact repository destination, `e` in Repositories
2. its group's own directory
3. its server's directory
4. the global default

So `acme/platform` can live in `~/work/platform` while everything else stays
under `~/unagit`, and a subgroup can override its parent. It is one keystroke,
`d`, in Settings, and it applies to everything cloned afterwards. A blank exact
destination restores the inherited one.

## The layout

```
<root>/<group>/<repo>                               the clone you switch branches in
<root>/<group>/.unagit/<repo>/<iid>-<branch>        a branch worktree for one MR
<root>/<group>/.unagit/<repo>/review-<iid>-<branch> a review worktree for one MR
<root>/.unagit/groups/<folder>/<repo>               a grouped worktree's member
```

- **The main clone** is the one working copy of a repository. `Enter` in its
  branch list switches that same clone, rather than making a directory per
  branch.
- **A branch worktree** is a real branch of a merge request, for committing and
  pushing. `Ctrl-O` on a merge request makes it.
- **A review worktree** holds the merge request as pending edits.
  `Ctrl-R` makes it. See [The Review Worktree](/guide/review).
- **A grouped worktree** puts several repositories in one folder. See
  [Grouped Worktrees](/guide/grouped-worktrees).

With an exact repository destination the worktrees live in
`.unagit/<directory-name>` beside it. Worktrees from older versions, under
`.mrs` and `.reviews`, are still found and reused where they are.

## They share one object store

Every worktree is a worktree of the main clone, so a second review costs a
checkout, not a clone.

## What a worktree knows about itself

Each worktree carries its own git configuration (`extensions.worktreeConfig`)
with `unagit.mr.base`, `.head`, `.iid`, `.project`, `.source`, `.target`,
`.url` and `.mode`. That is how an editor, or a later unagit, knows what a
directory is.

## How old, how big

Worktrees says when each one was made (`CREATED`) and what its files take on
disk (`SIZE`), with whatever was built in it, but not the object store it
shares. Nothing is removed for being old or big: that stays your call, with `d`.

## Tidying up

When `R` finds a merge request merged or closed, its worktrees are removed
without asking, unless they hold work of yours: uncommitted changes, unpushed
commits, your own edits in a review, comments not yet published. Those are
kept and named. A kept review stays a row of Worktrees, marked with an eye and
its merge request's number.

`d` on a repository deletes it from disk, after warning about uncommitted or
unpushed work.
