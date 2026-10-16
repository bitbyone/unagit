# Waiting for a Pipeline

You pushed, and the pipeline takes twenty minutes. In this tutorial you watch
it, get on with something else, and read the failed job when unagit tells you.

## 1. Watch the merge request

In Merge requests (`2`), put the cursor on your merge request and press
`Alt-Enter`. Type `watch` and press `Enter` on **Watch Merge Request**.

The row wears `◎`, and the Activity tab starts counting what runs:
`Activity [4] ●1`.

## 2. Get on with your work

Open something else, switch tabs, review another merge request. unagit asks the
server about the running pipeline every 10 seconds, in the background, while it
runs.

## 3. The news arrives

When the pipeline ends, a toast comes up in the bottom right corner:
**Pipeline failed**, with the merge request and what happened. It takes no key
and goes by itself.

If unagit is not in front, behind another window or with an editor in its
terminal, a desktop notification goes out instead.

::: tip No notification?
Settings › Integrations › Notifications chooses the system, the terminal, or
neither, and `t` there sends a test.
:::

## 4. Read the failure

Press `4` for Activity. The merge request is under *needs you*. Press `Enter`:
the pipeline's jobs open, the first failed one under the cursor. `Enter` again
reads its log, at its end, in the job's own colours.

## 5. Run it again

Fixed something, or think it was flaky? `R` on the job runs it again. The
earlier attempt stays under it (`↺`), its log still readable. The watch follows
the new run.

## 6. A new push

Push a fix. The watch moves on to the new head's pipeline by itself. When it
passes, the toast says so.

## 7. Stop watching

When the merge request is merged, its watch goes by itself. To stop earlier,
`x` on its row in Activity, or **Stop Watching** in its actions. `W` from any
screen lists every watch.

## What you learned

- **Watch Merge Request** and **Watch Branch** follow something in the
  background.
- News comes as toasts, desktop notifications when unagit is not in front, and
  in Activity.
- Watching runs only while unagit does: there is no background service, and
  the token never leaves a running unagit.

More in [Watching and Activity](/guide/watching).
