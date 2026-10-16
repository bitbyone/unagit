# Incomm and Hunk

Two tools for reading a change: one for the comments, one for the diff.

## Incomm

[Incomm](https://github.com/bitbyone/incomm) shows merge request comments in
your editor, anchored to the lines they are about. Install `incomm` on `PATH`,
then enable its integration.

When you open a review with `Ctrl-R`, unagit imports the comments before
starting the editor:

- Imported comments keep the reviewer's name and a link to the merge request.
- Replies use their thread's file location, and unchanged comments are not
  duplicated when you reopen the review.
- General comments without a file location are skipped.
- Comments on deleted lines or missing files are marked as orphaned and remain
  visible in Incomm's explorer.

If the import fails, unagit shows the error before opening the editor.

What you write in Incomm goes back to the server with `P` on the merge request,
and `COM` counts it as waiting until then. In Worktrees, `R` brings the
comments of every worktree's merge request.

Incomm keeps its store in `.incomm/`, which unagit keeps out of git's way in a
review. See [The reviewer's notebook](/guide/review#the-reviewer-s-notebook).

## Hunk

[Hunk](https://github.com/modem-dev/hunk) is a terminal diff viewer made for
reviewing a whole changeset. Install `hunk` on `PATH`; the integration is on as
soon as it is found, and `e` in its card turns it off.

| Key | Shows |
| --- | --- |
| `D` | what the row's working tree has not committed: staged, unstaged and new files |
| `Alt-D` | everything since the branch's base: its commits and what is not committed |
| `D` in a commit log | one commit |

In a review worktree, `D` is the whole merge request. A merge request with
nothing on disk gets its review made first, the repository cloned if need be,
so `D` down the list never stops to ask for `C`.

Hunk reads one repository at a time, so for a grouped worktree unagit puts the
changes of its repositories together into one patch, new files included, and
opens that.
