# Pipelines

The `CI` column is the newest pipeline: of the clone's branch in Repositories,
of the head in Merge requests, of the worktree's branch in Worktrees.

| Mark | Means |
| --- | --- |
| full circle, green | passed |
| full circle, red | failed |
| turning | running |
| empty circle, blue | waiting its turn |
| grey | skipped |
| `?` | the server did not answer |

It is read on `R` (`r` for one row) and kept for the next start. A pipeline
still running is asked about again every 10 seconds until it ends, in every
list. A repository not cloned has none and costs no request.

## The jobs

`J` shows the pipeline's jobs, the first that failed under the cursor. It works
on a merge request, a repository (the clone's branch, or the default branch
before it is cloned), a worktree, and a grouped worktree, where it asks which
repository first.

| Key | Does |
| --- | --- |
| `Enter` | read a job's log, its end, in the colours the job printed it in |
| `R` | run the job again, or start a manual (`▶`) or delayed (`◷`) one |
| `w` | open the job in the browser |
| `W` | open the pipeline in the browser |
| `P` | the earlier pipelines of the same merge request or branch |

In a log, `Ctrl-D`/`Ctrl-U` move by half a page, `Ctrl-F`/`Ctrl-B` by a whole
one. A job run again keeps its earlier attempts under it (`↺`), whose logs can
still be read. On a trigger job (`↳`), `Enter` lists the pipeline it started.

While a pipeline runs, its jobs are read again every few seconds and change in
place, and a running job's log grows at its end, as `tail -f` does, unless you
scrolled up.

::: info On GitHub
The jobs are the check runs, and the log is GitHub Actions', which GitHub gives
only once the job has finished.
:::

## Not waiting for it

To be told when a pipeline ends without keeping it on screen, watch it. See
[Watching and Activity](/guide/watching) and the
[Waiting for a Pipeline](/tutorials/watch-a-pipeline) tutorial.
