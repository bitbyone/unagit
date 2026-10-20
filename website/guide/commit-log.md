# The Commit Log

`Ctrl-L` opens a commit log: of the clone's branch on a repository, of a merge
request's commits, of a worktree's branch. What it offers follows from where it
was opened. The list comes from the server, so a repository not cloned yet is
cloned only once you pick a commit.

## Reading

| Key | Does |
| --- | --- |
| `Enter` | the commit's detail: author, date, branches and tags at it, the whole message, the files it changed |
| `D` | the commit in Hunk |
| `Alt-D` | everything from it to the working tree, in Hunk |
| `J` | the pipelines of the commit |
| `w` | open the commit on the server |
| `y` | copy its id, its link, a markdown link, or a line for a chat |

A commit not on disk yet is brought first, the repository cloned and the merge
request fetched, so a log can be paged through without stopping.

## Going there

| Key | Does |
| --- | --- |
| `C` | check the commit out with a detached HEAD; the branch column shows `@<commit>` |
| `B` | back to the branch it was checked out from |
| `n` | a branch at the commit |
| `Ctrl-W` | a worktree of its own at the commit, without moving the clone |
| `Ctrl-R` | in a merge request's log: review from this commit to the head |

## Tags

`t` puts a tag on the commit: a name, a message that makes it an annotated tag
(left empty, a lightweight one), and **Push to origin**, ticked unless you
untick it. A name a tag already has is refused: a tag is never moved, since
whoever fetched it takes it to mean one commit.

`T` lists the tags on the commit and which of them origin has: `p` pushes one
origin lacks, `d` deletes one here, `D` deletes it on origin and here.

## Changing commits

| Key | Does |
| --- | --- |
| `e` | edit a commit's message: the newest is amended, an older one written again and the later commits replayed |
| `u` | undo the newest commit: it goes, its changes stay on disk |
| `P` | push this commit and those below it; the newer ones stay here |
| `space`, then `s` | squash marked commits next to each other into one |

Squashing opens a form with their messages one after another, oldest first, to
make one message of. The commits after them are replayed, and what is not
committed stays as it is. A merge among them is refused.

Rewriting a commit origin already has asks first: a force push will be needed,
and anyone who built on it has to rebase. Where origin's copy stood is noted,
so `P` force-pushes with exactly that as the lease. Every change here can be
undone from [Rewrite History](/guide/rewrites).

## Where origin stands

A branch ahead of origin has a line under its own commits, at
`origin/<branch>`. Once the two have parted, after a squash, an amend or a push
from elsewhere, the log is in three parts:

- **only here**: a force push puts these on origin
- **only on origin**, dimmed: a force push removes these
- **shared**

The commits only on origin can be looked at, never rewritten.
