# Changes and Commits

**Commits are IntelliJ's, not git's.** A file is *versioned* or
*unversioned*. A change to a versioned file is committed as it is on disk,
staged or not, and an unversioned file goes in only when you choose it. Nothing
in unagit stages, and git's index is never something to think about.

## The edits column

`✎` counts the files not committed. `12/3` is twelve versioned files with
changes, in blue, and three unversioned files git does not track yet, in
red-brown. `Enter` lists them with the branch, HEAD and the commits origin does
not have yet.

## Committing

`c` commits the versioned files as they are on disk. Unversioned files stay
out. The dialog asks only for the message:

| Button | Does |
| --- | --- |
| **Commit** | commit |
| **Commit and Push** (`p`) | commit, then push it with every earlier commit not pushed yet |
| **Commit and Force Push** (`f`) | replace origin's copy, but only what was last fetched of it |

`P` pushes without committing. Every push asks first, and its question lists
the commits that go. A branch origin has moved past is offered Force Push
alone, or pull first to keep both.

## The Changes dialog

`Ctrl-K` opens **Changes**, IntelliJ's commit window:

- On the left, the files not committed in two groups: **Changes**, the
  versioned ones, and **Unversioned Files**. Each name has the colour of what
  happened to it: changed blue, added green, deleted grey, unversioned
  red-brown, with how many lines were added and deleted (`+12 −3`).
- On the right, the diff of the file under the cursor: only the places that
  changed, with old and new line numbers, the code coloured by its language.

Two things are kept apart, as IntelliJ keeps its checkboxes apart from its
selection:

- **The box** (`■`/`□`) says whether the file goes into the commit. `x` ticks
  or unticks it, a whole group on its heading, and `a` ticks all or none. The
  versioned files start ticked, the unversioned not. Only `c` reads the boxes.
- **The marks** (`space`) choose the rows every other action takes, or the row
  under the cursor when none is marked.

| Key | Does |
| --- | --- |
| `c` | commit the ticked files; an unversioned one is added on the way |
| `R` | roll changes back to the last commit |
| `A` | add unversioned files to git: they join the changes as added, ticked |
| `d` | delete unversioned files, after asking |
| `I` | write unversioned files into `.gitignore` |
| `s` | shelve the changes. See [The Shelf](/guide/shelf). |
| `y` | copy paths, folders, or the changes as a patch for `git apply` |
| `D` | show them in Hunk |
| `Ctrl-O` | open the working tree in your editor, the file under the cursor in front |
| `Tab`, `l` | go into the diff to scroll it; `h` comes back |

A rolled back file that was added stays on disk, unversioned. An unversioned
file is never rolled back, only deleted, and `d` never deletes what is merely
ticked.

The same dialog opens on a worktree, a repository of a group's view and a merge
request's branch worktree. Never on a review: its changes are the merge
request's.

## A detached HEAD

A detached HEAD is a checkout with no branch. You can commit there, but the
commits belong to no branch and unagit will not push them: make a branch with
`n` first.
