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

**An integration**, in Settings › Integrations: unibox on or off
(unset, on when installed), and its block:

- the environments repository, picked from the list of repositories and
  handed to unibox as a path (`unibox config set environments <path>`;
  see where they live in unibox's docs/design.md). A repository not yet
  laid out is laid out with `unibox init --dir <path>`;
- the runtimes unibox has, each on or off, and stopping idle sandboxes -
  `unibox config set runtimes.<runtime>.on|stop_idle`;
- the unibox found and the profile format it reads (`unibox --version`),
  and a refusal of an `api_version` unagit does not know;
- in herdr's block, unibox's herdr plugin.

unagit's config.yaml keeps only what it must to be portable: whether
unibox is on, and the environments repository as a repository of the
list. The rest is unibox's.

**A Sandboxes tab**, `6`, after Settings (`5` stays, as the messages that
point at `Settings [5]` do). Three parts, each a list made the way the
other screens' are (`pane.go`, its actions as `uiAction`s), filled from
unibox's `--json`:

| part | rows | actions |
| --- | --- | --- |
| **Profiles** | name, image state per runtime, agents, last commit to its directory, an older format; a pane with its instructions as the agent gets them, and what changed in it since unibox last saw it | Open in Editor…, Edit Instructions…, Build…, Network… (`unibox network`), Services… (`unibox service`), Sign In… (`unibox agent login`, `unibox secret set`), Migrate… (`unibox init <profile>`), Open in Sandboxed Agent… |
| **Sandboxes** | runtime, profile, state, memory, tasks | Shell… (`unibox shell` in a place), Stop, Recreate, Remove (a question naming its tasks) |
| **Tasks** | in the sandbox under the cursor: worktree, agent, its state | Open Worktree, Resume… (`unibox resume <task> --agent`, any of the profile's agents), Remove Task |

Without unibox installed or an environments repository chosen, the tab
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
- **A worktree whose branch an agent works on in a VM** (unibox's tasks
  name the host directory) wears a mark in the lists, and the commit
  dialog on it warns before a commit: "an agent works on this branch in
  <vm> - pull first". Nothing is forbidden; two commits apart on one
  branch only end in a conflict at the pull.
- Deleting a worktree or a group with a task in a VM offers to remove the
  task (`unibox rm-task <task>`) and, for a VM of its own, the VM. The default
  keeps the VM.
- What unagit learns from use - the order of the profile and sandbox
  pickers - goes to its `state.json`, as the place picker's does.
- **The commit author**: unagit passes `--author` only when the user
  picked one for the run. unibox otherwise takes the author from a git
  service of the profile that names one, then from the worktree's git
  configuration - and an explicit `--author` beats both, so passing it
  always would hide the profile's choice.
- Help rows for the tab and the new keys; the README explains the
  feature, unibox, the environments repository and the forge-side
  protection.

Later, not in the first version: following the branch on the forge
(`internal/watch`) to say when the agent pushed, and pulling on request
from that news.

How unagit hands the sandboxes the forge tokens they need - a socket, a
`unagit credential get` helper, grants - is planned in
[sandbox-credentials.md](sandbox-credentials.md).

## What unagit reads

The contract is unibox's command line (its docs/design.md, "The command
line" and "The boundary"); what unagit relies on in it:

- Every `--json` object carries `api_version`. unagit decodes into types
  of its own and refuses a version it does not know, saying to update
  unagit or unibox.
- `unibox check --json` answers `needs`, each with a `code`; unagit maps
  codes to its questions and shows a need it does not know by its
  `message`, so a newer unibox does not stop it.
- A failure under `--json` is `{"api_version", "code", "message"}`; exit
  status 3 is an image to build, 4 a refusal, and after `run` or `resume`
  the agent's own status.
- unagit reads no YAML. A profile of an older format does not load; it is
  migrated with `unibox init <profile>`, whose output unagit shows (what
  could not be carried over is named there).
- Values never pass through unagit: Sign In… opens a terminal running
  `unibox agent login` or `unibox secret set`, which take the token there.

## Built

- `internal/unibox`: the client - the binary run with `--json`, decoded into
  unagit's own types, an `api_version` other than 1 refused; its stand-in
  for tests in `internal/unibox/uniboxtest`.
- The Sandboxes tab (`sandboxes.go`, `sandboxes_actions.go`): one tree of
  profiles, sandboxes and tasks rather than three lists, the detail beside
  it read from `agent ls`, `service ls`, `network ls`, `network port ls`
  and `script ls`. The tab is on the bar only while unibox is on, so `5`
  stays Settings.
- Resume (any of the profile's agents, every place), shells, New Task…
  with `task check` and a push only on request, stop, recreate, remove,
  ports, scripts, network rules, services, build, migrate, sign-in in this
  terminal, the context, the environments repository, prune.
- Open in Sandbox… on worktrees, groups and clones.
- Settings › Integrations: unibox on or off, and what was found.
- A session record names its sandbox and task, so Activity lists the agent.

Not yet: the integration block's runtimes and herdr plugin, the commit
warning and the mark on a worktree an agent works on in a VM, removal with
a worktree, pushing the branch from the host before `prepare`, and clone
URLs from the forge's API (unibox takes them from `origin` for now).

## Order of work

1. unibox's side of the contract first (unibox's docs/design.md, "Order
   of work"): `profiles --json` without `approved` and with a profile's
   format, `api_version` 2. The command line's arguments are settled:
   what a command works on is its argument, `--sandbox` names a sandbox
   there or one to make from `--profile`, `rm-task <task>`, `sandboxes`.
2. Settings: the integration block, the configuration handed over.
3. Open in Sandboxed Agent… for one repository's worktree in This
   Terminal: the pickers, `check` and its questions, the push, `prepare`,
   `run`; a stand-in `unibox` on its own PATH logging its argv, checking
   the order check → (build, sign in) → push → prepare → run, the clone
   URLs from the forge, and no token in any argv or environment.
4. Groups, then every place (Zellij, Ghostty, herdr).
5. The Sandboxes tab and its actions, at several terminal sizes
   (`assertLegible`, `TestEveryActionIsNamedAndExplained`,
   `TestNoTwoActionsShareAKey`); removal with a worktree; the commit
   warning; Activity.
6. The herdr plugin's install from Integrations.
7. Credentials from unagit's vault ([sandbox-credentials.md](sandbox-credentials.md)).
