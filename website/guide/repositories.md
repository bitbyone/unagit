# Repositories

Repositories (`1`) lists every repository of every server and group you
picked. These are the keys you will use most.

## Getting in

| Key | Does |
| --- | --- |
| `Ctrl-O` | open your editor in the clone, as it is on disk. Only a repository not cloned yet is cloned first. |
| `Alt-O` | the same, in an editor you choose |
| `C` | clone to disk without opening anything |
| `O` | **Open…**: what, with what and where, in one form |
| `w` | open the repository in the browser |
| `y` | copy its link, a markdown link, its directory |

`Ctrl-O` fetches nothing and pulls nothing: the list already says where
everything stands, and `p` updates it first when you want that. `Ctrl-C` is
left to end unagit, as it ends any program in a terminal.

## Keeping it current

| Key | Does |
| --- | --- |
| `p` | update the clone: a fast-forward when nothing of yours is in the way, a rebase of your commits and edits onto origin when there is |
| `Alt-P` | the same for every clone at once |
| `R` | fetch every clone in the background, and read the pipelines |
| `r` | the same for the row under the cursor |

If an update would conflict, because your edits touch a file origin changed or
your commits clash with origin's, `p` refuses and leaves the clone exactly as
it was, for you to sort out by hand. `Alt-P` passes over clones without an
upstream and names the ones it left alone.

## Working in the clone

| Key | Does |
| --- | --- |
| `b` | the branches. See [Branches](/guide/branches). |
| `Ctrl-K` | **Changes**: the files not committed and their diffs. See [Changes and Commits](/guide/changes). |
| `c` | commit the versioned files as they are on disk |
| `P` | push, after showing what goes |
| `n` | a merge request from the branch the clone has out, pushed first if origin lacks it |
| `Ctrl-L` | the commit log. See [The Commit Log](/guide/commit-log). |
| `J` | the pipeline's jobs. See [Pipelines](/guide/pipelines). |
| `m` | that repository's merge requests |
| `D`, `Alt-D` | the changes in [Hunk](/guide/incomm-hunk): what is not committed, or everything since the base |

## Creating a repository

**Create a New Repository** (`:` in Repositories) asks where, one of your
groups or a subgroup, and for its name, description, visibility, first branch,
a README, a license and a `.gitignore`. It is created on the server, cloned at
once and listed with the cursor on it.

**New Repository from Folder…** (`:` in Repositories) starts from a directory
on disk, with or without git, and creates its remote repository. Existing
history is kept; the current branch is published, with other branches and tags
when selected. Uncommitted and ignored files stay local.

After publishing, **Keep here** remembers the checkout where it is, without
changing `config.yaml`, and **Move** moves the whole folder to the configured
destination, edits, stashes and local history included. A failed push reuses
the remote already made; no retry force-pushes or deletes a remote repository.

## Starred repositories

With a GitHub account, `:` › **View Starred Repositories…** lists what you
starred, with its language and stars. `Enter` reads the README, `C` clones it.
A repository cloned from there joins Repositories with a star badge saying
where it came from.

## Sizes

`SIZE` is what a clone takes on disk with all its worktrees. It is measured
behind the list at the start and as the last step of `R`, or on its own by
`:` › **Measure Sizes Again**. With `SIZE` hidden and the list not ordered by
size, nothing is measured.
