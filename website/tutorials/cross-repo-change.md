# A Change Across Repositories

A change to an API usually means a change to its clients as well. In this
tutorial you put three repositories in one folder on one branch, make the
change, and open three merge requests that link to each other.

The examples use `acme/api-gateway`, `acme/web` and `acme/mobile`; use three
repositories of your own.

## 1. Mark the repositories

In Repositories (`1`), move to the first repository and press `space`. The
cursor moves on and the header says `SELECT 1`. Mark the other two the same
way. Type `/` to find one faster; the marks stay while you filter.

## 2. Make the grouped worktree

Press `Ctrl-W`. A form asks for:

1. **New branch**: `feat/rate-limits`. Every repository gets it.
2. **Folder**: it follows the branch name; leave it.
3. **A base for each repository**: where each branch starts. It starts on what
   the main clone has out; pick `main` or `develop` where it differs.

Press the button to create it. unagit clones what is not on disk yet, fetches,
checks every repository, and only then makes the worktrees. If any of them
fails, nothing is left half made.

unagit moves to **Worktrees** (`3`), with the cursor on a row marked `◆`.

## 3. Open it

Press `Ctrl-O`. Your editor opens the group's folder:

```
feat-rate-limits/
├── api-gateway/
├── web/
└── mobile/
```

Each directory is a worktree of its own repository, on `feat/rate-limits`.
Make the change across them and save.

## 4. Look at the group

Back in unagit, press `Enter` on the group's row. The view has a block for the
group and one for each repository. `j`/`k` light a block. Each says what is not
committed, where it stands against origin and how far its base has moved.

## 5. Commit

With the group's block lit, press `c`. One message serves all three; give a
repository its own instead if it needs one. **Commit and Push** pushes at the
same time; or push later with `P`.

## 6. Open the merge requests

Press `n` on the group. One form asks for:

- the **title** and **description**, proposed from the commits of all of them;
- for each repository, the branch to merge into, or `(no merge request)` to
  leave it out.

unagit pushes what origin lacks, opens the merge requests one by one, and ends
each description with a **Related merge requests** list linking the others.

## 7. When the base moves on

Someone merges to `main` while you wait for review. In Worktrees, `R` fetches,
and `RMT` says `↓2 behind main`. Before the branch is pushed, `p` rebases onto
it. After, `Ctrl-R` puts it on today's base and `P` force-pushes with a lease,
so nothing anyone pushed meanwhile is lost.

If a rebase would conflict, `RMT` says `· conflicts` before you try, and the
detail names the files.

## 8. Clean up

When the merge requests are merged, `R` in Worktrees lets their merge requests
go. `d` on the group deletes its worktrees; the branches and the main clones
stay.

## What you learned

- `space` marks, `Ctrl-W` makes one folder of the marked.
- `c`, `P` and `n` on a group act on every repository at once.
- The merge requests link to each other, and running `n` again keeps the links
  current.

More in [Grouped Worktrees](/guide/grouped-worktrees).
