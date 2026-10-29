# Hand a Review to an Agent

An agent can read a merge request the way you do: in a checkout where the whole
change is pending. In this tutorial you start Claude Code in a review worktree,
beside unagit, and follow it from the Activity screen.

You need `claude` on your `PATH`. Codex, Copilot CLI, opencode and Antigravity
work the same way. Running unagit inside [Zellij](https://zellij.dev) or
[herdr](https://herdr.dev) lets the agent sit beside it; without one, it gets
unagit's terminal until it ends.

## 1. Check the integration

In Settings (`S`) › Integrations, the Claude Code card says **enabled**. If it
says **not installed**, `c` checks again once `claude` is on your `PATH`.

## 2. Pick the merge request

In Merge requests (`2`), put the cursor on the merge request and press
`Alt-Enter`.

## 3. Review with Claude Code

Type `cc rev`. The picker narrows to the review actions for Claude Code. Choose
**Review with Claude Code…**, and unagit asks where:

- **This Terminal**: unagit is suspended until the agent ends
- a tab or split of the Zellij or herdr you are in
- a tab of herdr's **Unagit Agents** workspace
- a Ghostty window, tab or split, on a Mac

Choose a vertical split. Next time, typing `cc rev split` in the picker finds
that place directly.

unagit makes the review worktree as `Ctrl-R` would, with the change unstaged,
and starts Claude Code there. Ask it to review the change; to it, `git diff` is
the whole merge request.

## 4. Follow it

Back in unagit, press `4`. The agent is listed with what it works on. In herdr,
Activity also says what it is doing: **working** under *under way*, **waiting**
for an answer under *needs you*. The number waiting is on the tab from every
screen.

`Enter` on its row goes to the agent's pane. `Alt-A` lists the agents from any
screen.

## 5. Read what it found yourself

The worktree is the same one `Ctrl-R` opens. Press `Ctrl-R` on the merge request
and your editor opens there too, on the same pending change.

## 6. Finish

`x` on the agent in Activity closes it, after asking, when it runs in herdr.
Elsewhere, end it as you would end the agent anywhere.

## What you learned

- **Review with &lt;agent&gt;…** prepares the review and starts the agent in it.
- Short words find the action: `cc`, `rev`, `split`.
- Activity follows the agents unagit started.

More in [Coding Agents](/guide/agents).
