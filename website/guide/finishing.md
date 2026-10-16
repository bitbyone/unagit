# Finishing a Review

The rest of a merge request's life is a key away too, on GitLab and GitHub
alike. After each, the row is read again, so the list shows what the server now
has.

## Comments

`c` opens the conversation: comments as markdown, threaded the way they were
written. `i` replies. With [Incomm](/guide/incomm-hunk) the comments are in
your editor too, beside the lines they are about, and `P` publishes the ones
you wrote there.

## Approve

`A` approves, after asking: everyone on the merge request will see it.

## Merge

`M` merges. The dialog lists first what stands in the way, as far as the list
knows: a draft, a pipeline failed or still running, approvals missing, threads
not resolved. Then it asks whether to squash and whether to delete the source
branch.

While the pipeline runs, it offers to wait for it instead (auto-merge on
GitHub, where the repository has to allow it).

The merge is of the head the list has seen: a push since then is refused rather
than merged unread, and unagit says to look first.

## Everything else

| Key | Does |
| --- | --- |
| `Ctrl-D` | mark a merge request as a draft, or a draft as ready |
| `a` | **Add/Change Assignee…** |
| `s` | **Add/Change Reviewer…** |
| `t` | the labels |
| `V` | mark it reviewed without opening it |
| `Alt-Enter` › **Close Merge Request…** | close it without merging; its branch stays |

The people pickers put who is assigned at the top, above a line. Below come
whom you assign most often, then who is about the merge requests on screen,
then everyone else. `space` assigns, `x` unassigns, and `Esc` saves. How often
you give merge requests to whom is counted in `state.json`, never in
`config.yaml`.

Labels work the same way. They stand as pills in the `LABELS` column, in their
own hue worked out in the theme's colours, and `/` finds a merge request by
them.
