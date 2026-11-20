# Finishing a Review

The rest of a merge request's life is a key away too, on GitLab and GitHub
alike. After each, the row is read again, so the list shows what the server now
has.

## Comments

`c` opens the conversation at once: each comment a card, its author, age
and line on its frame, the markdown rendered, threaded the way they were
written. What is said of the merge request as a whole comes first. A
thread on code is one block: the lines it is about - three before, four
after, from the version of the diff the comment was written on - then its
comments, then GitLab's notes that a later version changed the line; the
code of a thread on an older version comes in behind the rest.

`j` and `k` go over the comments and the code of each thread. `r` replies
in the thread, `c` writes a new comment, `e` and `d` edit and delete one of
your own, `x` resolves the thread or opens it again, and `h` hides the
code or shows it again, staying on the comment chosen. `Enter` on a note of a later change
compares the change; on a comment it expands the file. `.` lists
everything that can be done, here and in an expanded file. The mouse does
what the keys do: a click chooses, a double click is `Enter`, the right
button lists the actions, and the wheel moves as `j` and `k` do.

`j` and `k` go a whole comment at a time, its start shown. A comment taller
than the view - a plan from Atlantis - is read to its end, or some of it
taken, with a cursor in it: `v` puts one in the comment chosen, and `j`
`k` `h` `l` `0` `$` `G` move it, `v` again selects from there, `V` selects
whole lines, and the mouse selects by dragging; `y` copies the selection,
`Y` the whole comment as it was written, and `Esc` drops the selection,
then goes back to the comments.

The expanded file is the version the comment was written on - or the
latest, asked when they differ - with the cursor on the comment, a mark and
a count beside each line with comments, and the comments under their lines
(`h` hides them and shows them again). The cursor goes over the comments as over the lines,
with the same keys, and `v` switches version.

In the latest version, `c` writes a comment on the cursor's line.
**Publish** starts a thread on the forge at once; **Save** keeps it in
unagit, drawn at once, until `P` publishes it with the rest - it counts in
`PUB` meanwhile. With Incomm on, opening or syncing a worktree of the merge
request moves a saved comment into that worktree's Incomm store, and from
then on it is edited in the editor like any comment written there. With [Incomm](/guide/incomm-hunk) the comments are in
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
| `.` › **Mark as Draft** | mark a merge request as a draft; **Mark Ready** takes it back |
| `a` | **Add/Change Assignee…** |
| `s` | **Add/Change Reviewer…** |
| `t` | the labels |
| `V` | mark it reviewed without opening it |
| `.` › **Close Merge Request…** | close it without merging; its branch stays |

The people pickers put who is assigned at the top, above a line. Below come
whom you assign most often, then who is about the merge requests on screen,
then everyone else. `space` assigns, `x` unassigns, and `Esc` saves. How often
you give merge requests to whom is counted in `state.json`, never in
`config.yaml`.

Labels work the same way. They stand as pills in the `LABELS` column, in their
own hue worked out in the theme's colours, and `/` finds a merge request by
them.
