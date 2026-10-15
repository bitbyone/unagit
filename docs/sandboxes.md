# Sandboxed agents

The sandboxing - profiles, sandboxes, tasks, the agents' logins and network
rules - is **unibox**, a project of its own beside this one (`../unibox`).
Its design, the reasons behind it and what was verified about Docker
Sandboxes (`sbx`) are in unibox's `docs/design.md`, which this document
used to be. unibox imports nothing of unagit's and unagit nothing of
unibox's: unagit is meant to be one UI over it, driving its CLI and its
versioned `--json` output. What follows is that UI's part of the plan, kept
here because it is unagit's to build.

## In unagit

**An integration**, in Settings › Integrations: agentbox on or off
(unset, on when installed), and its block:

- the environments repository, picked from the list of repositories and
  handed to agentbox as a path (see where they live (unibox's docs/design.md));
- the runtimes agentbox has, each on or off, and stopping idle VMs -
  written to agentbox's configuration with `agentbox config set`;
- in herdr's block, agentbox's herdr plugin.

unagit's config.yaml keeps only what it must to be portable: whether
agentbox is on, and the environments repository as a repository of the
list. The rest is agentbox's.

**A Sandboxes tab**, `6`, after Settings (`5` stays, as the messages that
point at `Settings [5]` do). Three parts, each a list made the way the
other screens' are (`pane.go`, its actions as `uiAction`s), filled from
agentbox's `--json`:

| part | rows | actions |
| --- | --- | --- |
| **Profiles** | name, image state per runtime, agents, last commit to its directory; a pane with its instructions as the agent gets them, and what changed in it since unibox last saw it | Open in Editor…, Edit Instructions…, Build…, Network… (`unibox network`), Services… (`unibox service`), Sign In… (`unibox agent login`), Open in Sandboxed Agent… |
| **Sandboxes** | VM: runtime, profile, state, memory, tasks | Shell… (`agentbox shell` in a place), Stop, Remove (a question naming its tasks) |
| **Tasks** | in the VM under the cursor: worktree, agent, its state | Open Worktree, Remove Task |

Without agentbox installed or an environments repository chosen, the tab
says what is missing and where to set it. Activity still lists every
agent, sandboxed ones with their VM: Activity is what runs, the Sandboxes
tab is what it runs in.

Elsewhere:

- **Open in Sandboxed Agent…** on repositories with a worktree, worktrees,
  groups, merge requests with a branch worktree. Alt picks again what use
  would skip (the profile and the sandbox, even when one).
- **Pull** of a worktree or a group is what brings the agent's work; it
  exists already for branches with an upstream, check it covers a group in
  one go.
- **A worktree whose branch an agent works on in a VM** (agentbox's tasks
  name the host directory) wears a mark in the lists, and the commit
  dialog on it warns before a commit: "an agent works on this branch in
  <vm> - pull first". Nothing is forbidden; two commits apart on one
  branch only end in a conflict at the pull.
- Deleting a worktree or a group with a task in a VM offers to remove the
  task (`agentbox rm-task`) and, for a VM of its own, the VM. The default
  keeps the VM.
- What unagit learns from use - the order of the profile and sandbox
  pickers - goes to its `state.json`, as the place picker's does.
- **The commit author**: unagit passes `--author` only when the user
  picked one for the run. unibox otherwise takes the author from a git
  service of the profile that names one, then from the worktree's git
  configuration - and an explicit `--author` beats both, so passing it
  always would hide the profile's choice.
- Help rows for the tab and the new keys; the README explains the
  feature, agentbox, the environments repository and the forge-side
  protection.

Later, not in the first version: following the branch on the forge
(`internal/watch`) to say when the agent pushed, and pulling on request
from that news.

How unagit hands the sandboxes the forge tokens they need - a socket, a
`unagit credential get` helper, grants - is planned in
[sandbox-credentials.md](sandbox-credentials.md).
