# Finishing a Review

The rest of a merge request's life is a key away too, on GitLab and GitHub
alike. After each, the row is read again, so the list shows what the server now
has.

## Comments

`c` opens the conversation: each comment a card, its author, age and line on
its frame, the markdown rendered, threaded the way they were written. A
thread on code is one block: the lines it is about - three before, four
after, from the version of the diff the comment was written on - then its
comments, then GitLab's notes that a later version changed the line. `j`
and `k` choose a comment, `i` replies to it in its thread, `c` writes a
new one. `Enter` shows the whole diff of its file with the commented lines
marked, or on a change note the change itself; `e` shows the whole file,
asking first which version when the comment is on an older one. `x`
resolves the chosen thread, or opens it again.

What is said of the merge request as a whole comes first, the threads on
its code after. `s` hides the code, to read the comments through, and
shows it again, staying on the comment chosen. In the whole file (`e`) a mark and a count stand beside
each line with comments; `s` shows the comments under their lines, and
`j` and `k` go over them as over the lines - `i` answers one, `x`
resolves its thread, and `v` switches to another version of the file.

In the whole file of the latest version, `c` writes a comment on the
cursor's line. **Publish** starts a thread on the forge at
once; **Save** keeps it in unagit until `P` publishes it with the rest -
it counts in `PUB` meanwhile, and `d` in the conversation deletes it. With
Incomm on, opening or syncing a worktree of the merge request moves a
saved comment into that worktree's Incomm store, and from then on it is
edited in the editor like any comment written there. With [Incomm](/guide/incomm-hunk) the comments are in
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
