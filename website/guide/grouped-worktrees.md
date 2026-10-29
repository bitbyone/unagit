# Grouped Worktrees

Work that spans several repositories wants them in one folder: a change to an
API and its two clients, say, or a task for an AI agent that has to see all of
them.

```
<root>/.unagit/groups/<folder>/<repo>               a worktree of each repository
<root>/.unagit/groups/<folder>/.unagit-group.json   which repository each directory is
```

## Making one

In Repositories, `space` marks a repository and moves on. The header says
`SELECT 3`, and the marked rows have a band of their own; `Esc` clears them.
`Ctrl-W` on the marks then asks for:

- **New branch**, required: the group always works on a branch of its own, made
  in every repository.
- **Folder**: its name. It follows the branch until you change it.
- **A base branch for each repository**: where its new branch starts, and what
  `p` rebases it onto until it is pushed. It starts on the branch the main
  clone has out, or the default branch for a repository not cloned yet. Any
  branch is offered except one a worktree already has out.

It is made whole or not at all: every repository is cloned and fetched, then
checked, and only then are the worktrees added. A failure takes back what was
made.

## In Worktrees

The group is one row marked `◆`, and `REPOS` says how many it holds.

| Key | Does |
| --- | --- |
| `Enter` | open it as a view of its own |
| `Ctrl-O` | open the folder in your editor |
| `p` | update every repository in it |
| `P` | push every repository that has commits origin lacks |
| `c` | commit every repository at once, with one message or one each |
| `a` | add a repository |
| `x` | take one out: its worktree goes, its branch stays |
| `n` | open a merge request in every repository at once |
| `d` | delete the worktrees; the branches and the main clones stay |

A branch with nothing of its own yet stays local when you push, rather than
leaving an empty branch on origin.

## The view

`Enter` opens the group in blocks: one for the whole group, one for every
repository. `j`/`k` light a block, and the keys act on the one lit. On the
group, `p`, `C`, `n` and `a` do it for all of them; on a repository the same
keys do it for that one alone, and `w` opens its merge request, `c` its
conversation, `Ctrl-L` the commits of its branch.

Each block lists what the branch was made from and how far that has moved,
where it stands against origin, what is not committed, its merge request and
its comments.

## Linked merge requests

`n` opens a merge request in every repository at once, from the branch they
share. One form takes the title and the description, proposed from the commits
of all of them, and for each repository the branch to merge into, or
`(no merge request)` to leave it out of this round.

unagit pushes what origin lacks, opens them one by one, and ends each
description with a **Related merge requests** list linking the others. Run it
again when another repository has caught up: only the missing ones are opened,
and every list is written afresh.

## Comments across the group

An editor or an agent opened on the group's folder keeps one
[Incomm](/guide/incomm-hunk) store there, for all its repositories: a comment
on `gateway/src/x.go` is a comment on `src/x.go` in the gateway repository.
`P` on a merge request publishes the comments of its repository, with their
paths as the repository names them.

The [A Change Across Repositories](/tutorials/cross-repo-change) tutorial goes
through it end to end.
