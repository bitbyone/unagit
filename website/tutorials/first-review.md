# Your First Review

In this tutorial you install unagit, connect it to GitLab or GitHub, and review
a merge request in your editor, with the whole change sitting in the gutter as
pending edits. It takes about ten minutes.

You need git 2.38 or newer, an editor (Neovim is the default) and a merge
request somebody asked you to review.

## 1. Install

```sh
brew tap bitbyone/unagit https://github.com/bitbyone/unagit
brew install unagit
unagit --version
```

## 2. Make a token

Make a personal access token on your forge:

- **GitLab**: User settings › Access tokens, with the `api` scope.
- **GitHub**: Settings › Developer settings › Personal access tokens, with
  `repo` and `read:org`.

Keep the page open; you will paste the token in a moment.

## 3. Start unagit and choose a passphrase

```sh
unagit
```

unagit asks for a passphrase. It seals the token on disk, so pick one you will
remember: it is asked for at every start. You land in **Settings**.

## 4. Add the server

1. Press `a` to add a server, and choose GitLab or GitHub. For a self-hosted
   GitLab, type its address.
2. Paste the token.
3. Press `v` to check it against the server. A token the server refuses is
   named here, not later.

## 5. Pick your groups

Under the server, move to the group your merge request lives in and press
`space`. Take it with its subgroups if the repository is deeper down.

Then press `d` and say where its repositories should be cloned, for example
`~/work`. Leave it, and they go under `~/unagit`.

## 6. Refresh

Press `1` for Repositories, then `R`. The header turns a spinner while the
servers answer; you can move about the list meanwhile. Then press `2` for
Merge requests and `R` again.

Your merge request is in the list. If there are many, type `/` and a few
letters of its title.

::: tip Not in your groups?
`unagit review <link>` opens a merge request straight from its link, whether
its group is picked or not.
:::

## 7. Read what it is about

With the cursor on the merge request, press `Enter`. The detail column shows
the author, reviewers, approvals, the pipeline, labels and commits, and follows
the cursor as you move. `c` opens the conversation.

## 8. Open the review

Press `Ctrl-R`.

unagit clones the repository if it is not on disk yet, makes a review worktree
beside it, and opens your editor there. In the worktree:

```
HEAD = the merge base     index = the merge base     files = the merge request
```

So your editor sees the merge request as your own uncommitted work. In Neovim
with gitsigns, the gutter marks every changed line, and `]c` jumps from change
to change. In a terminal in the same directory:

```sh
git status   # every file the merge request touches
git diff     # the whole merge request
```

Type notes into the files if it helps you think: they survive reopening the
review.

## 9. Come back

Quit the editor. unagit is where you left it, with the merge request now
marked `◐`: a review worktree is on disk.

## 10. Approve or comment

- `c` opens the conversation, and `i` replies there.
- `A` approves, after asking: everyone on the merge request will see it.

## When the author pushes again

Press `R` in Merge requests some time later. `NEW` counts the commits pushed
since your review. Press `Ctrl-L` on the merge request: the new commits are
marked `●`, with the cursor on the oldest. Press `Ctrl-R` there, and the review
opens again with only those commits pending.

## What you learned

- Settings is the whole configuration: `a` a server, `space` the groups, `d`
  where they go.
- `R` refreshes a list; nothing is asked of the server otherwise.
- `Ctrl-R` opens a merge request as one unstaged change.
- `Ctrl-L` then `Ctrl-R` reviews only what is new.

Next: [The Review Worktree](/guide/review) explains why it is built this way,
and [Finishing a Review](/guide/finishing) covers merging.
