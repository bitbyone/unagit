# Watching and Activity

What you are waiting for need not be on screen.

## Watching something

- **Watch Merge Request**, in the actions (`Alt-Enter`) of a merge request or
  of its pipeline's jobs, follows it in the background: its pipeline whatever
  its head ran, a push moving the watch on to the new one, its commits,
  comments and approvals.
- **Watch Branch**, in the actions of a repository, a worktree or a block of
  the worktree view, follows a branch: its newest pipeline, how far behind its
  base it is, whether it is still on origin.

The row wears `◎` in every list, and on something watched the same action is
**Stop Watching**. A merged or closed merge request's watch goes by itself, and
so does a branch's that was deleted on origin.

## The news

Every change is a **toast** in the bottom right corner: a pipeline that began,
passed, failed, was cancelled or waits for a manual job, a new commit, a merge
request merged or closed. A toast is coloured by how it went, never takes the
keys from what you are typing, and goes by itself, after five seconds of your
looking at it (Settings › Notifications sets how long).

When no unagit is in front, a **desktop notification** goes out, through
Notification Centre or `notify-send`. Settings › Integrations › Notifications
chooses the system, the terminal, or neither, and `t` sends a test.

## The Activity screen

**Activity** (`4`) is what runs in the background and what waits for you:

| Panel | Holds |
| --- | --- |
| **Open** | the editors open, as cards; `e` goes there |
| **Watching** | how many merge requests and branches are watched; `W` lists every watch |
| **The list** | the watches and the agents, under *needs you*, *under way* and *quiet* |
| **The detail** | how the row under the cursor stands now, and its story, newest first |
| **The log** | everything in time; `L` goes into it, `z` brings it to the front |

In the list, a bar at a row's edge is a change since your last visit. `Enter`
opens a pipeline's jobs or goes to an agent, `x` stops watching, `w` opens the
pipeline, `m` goes to the merge request, `r` and `R` read one or everything
now.

The tab counts what is new from every screen: `Activity [4] ●2`.

## How it asks

Watching runs only while unagit does: the token is only ever in a running
unagit's memory, and there is no background service.

- A running pipeline is asked about every 10 seconds, a finished one every two
  minutes, to notice the next push.
- The servers are asked once a turn about every watch at once, with GraphQL,
  and a watch is read in full only when something about it moved.
- A server short of its rate limit is left alone until it resets.
- With several unagits open, one asks the servers and the others read what it
  found, so each change is said once.

What is watched is kept under `~/.config/unagit/watch/`, beside the
configuration but not in it. Each thing's story is kept for a month or 200
events.

## Trying it out

`unagit --debug` adds Settings › Debug, which fires every kind of news by hand,
toasts and desktop notifications, so you can see how they look without waiting
for a pipeline to fail.
