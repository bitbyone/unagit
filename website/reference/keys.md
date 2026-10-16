# Keys

`?` in any main view shows the keys of where you are. `Alt-Enter` and `:` list
every action with its key, so this page is for reading ahead, not for learning
by heart.

## Everywhere

| Key | Does |
| --- | --- |
| `Alt-Enter`, `Ctrl-A` | every action on the row, the marks or the lit block, with its key |
| `:` | every action of the screen |
| `+` | find any repository of yours, in any group, and list it |
| `1` `2` `3` `4` `5` | Repositories · Merge requests · Worktrees · Activity · Settings |
| `Alt-A` | running agents; `Enter` goes to one |
| `E` | running Neovims: `Enter` attaches, `a` asks where, `x` closes |
| `W` | every watch |
| `/`, `Esc` | fuzzy filter · leave it, clear it, close the detail |
| `Enter` | the detail column, and into it |
| `?`, `q` | help · quit |

## Opening

| Key | Does |
| --- | --- |
| `Ctrl-O` | open the editor as it is on disk; clones only what is missing |
| `Ctrl-R` | open a merge request for review: the change as pending edits |
| `Alt-O`, `Alt-R` | the same, in an editor you choose |
| `O` | **Open…**: what, with what and where |
| `C` | clone without opening; a merge request's review worktree |
| `w` | open in the browser |
| `y` | copy the link, reference, branch or directory |
| `Ctrl-Z` in Neovim | put it aside and return to unagit (0.12+) |

## Lists

| Key | Does |
| --- | --- |
| `r`, `R` | refresh the row under the cursor · the whole list |
| `space` | mark rows |
| `L` | cloned only |
| `x`, `X` | hide · the hidden list |
| `o` | order |
| `Ctrl-G` | group the list |
| `v` | what the list shows: grouping, favourites first, columns |
| `Ctrl-F` | star or unstar a favourite |
| `d` | delete from disk |

## Repositories and worktrees

| Key | Does |
| --- | --- |
| `p`, `Alt-P` | pull or rebase onto origin · every clone at once |
| `c` | commit the versioned files, push with it if you like |
| `P` | push, asking first |
| `Ctrl-K` | the changes: files not committed and their diffs |
| `b` | branches |
| `m` | merge requests of this repository |
| `J` | the pipeline's jobs |
| `D`, `Alt-D` | in Hunk: what is not committed · since the base |
| `Ctrl-W` | a worktree for a branch; on marks, one grouped worktree |
| `e` | the exact destination before cloning; blank restores the inherited one |
| `Ctrl-T`, `f`, `F` | tag · show only some tags · every tag again |

## Merge requests

| Key | Does |
| --- | --- |
| `c` | the conversation |
| `A` | approve |
| `M` | merge |
| `Ctrl-D` | draft, or ready |
| `a`, `s`, `t` | assignee · reviewer · labels |
| `V` | mark reviewed |
| `H` | hide the author's merge requests |
| `Ctrl-L` | the commits; `Ctrl-R` there reviews from one onwards |

## Branches

| Key | Does |
| --- | --- |
| `Enter` | switch the clone to it |
| `n` | a new branch from the one under the cursor |
| `m` | open a merge request from it |
| `d`, `D`, `Alt-D` | delete in the clone · everywhere · on origin |
| `Ctrl-W` | a worktree for it |
| `H` | Rewrite History |

## Commit log

| Key | Does |
| --- | --- |
| `Enter` | the commit's detail |
| `D`, `Alt-D` | diff · diff since, in Hunk |
| `C`, `B` | check out · back to the branch |
| `n`, `Ctrl-W` | a branch · a worktree at the commit |
| `e`, `u` | edit the message · undo the newest commit |
| `space`, `s` | mark · squash |
| `t`, `T` | tag the commit · its tags |
| `J` | the commit's pipelines |
| `H`, `R` | Rewrite History · Recover from Reflog |

## The Changes dialog

| Key | Does |
| --- | --- |
| `x`, `a` | tick for the commit · all or none |
| `c` | commit the ticked |
| `space` | mark |
| `R` | roll back |
| `A` | add to git |
| `d` | delete unversioned |
| `I` | add to `.gitignore` |
| `s` | shelve |
| `y` | copy paths or a patch |
| `Tab`, `l`, `h` | into the diff · back |

## Forms

| Key | Does |
| --- | --- |
| `j`, `k`, `Tab` | move between fields and buttons (NORMAL) |
| `i`, `Enter` | type into a field (INSERT) |
| `Esc` | stop typing |
| a lit letter | press that button |

## Markers

| Marker | Means |
| --- | --- |
| `○` | nothing on disk |
| `●` | a branch worktree |
| `◐` | a review worktree |
| `◉` | both |
| `⊘` | hidden |
| `◆` | a grouped worktree |
| `◎` | watched |
| `★` | favourite |
| `▣` | open in Neovim |
| `✦` | a coding agent at work there |
