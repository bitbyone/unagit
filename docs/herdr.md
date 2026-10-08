# Herdr: panes, agents, and agents in a container

## Where it stands

Built (2026-10-08): herdr as a multiplexer level with Zellij; Ghostty
windows, tabs and splits through AppleScript; Claude Code, Codex, Copilot
CLI, opencode and Antigravity as integrations, each with Open in
<agent>… and a choice of place. Agents opened in herdr from a place of
their own go to one workspace, Unagit Agents, a tab each. herdr and
Ghostty run the command through `unagit launch` (`internal/mux`), which
was checked against both with `UNAGIT_HERDR_TEST=1` and
`UNAGIT_GHOSTTY_TEST=1`. Found on the way: `herdr pane run` prints
nothing when it succeeds; Ghostty keeps a terminal whose program ended
open until a key, whatever `wait after command` says, so the launcher
closes it; `line` cannot name an AppleScript variable; Ghostty refuses
`new tab` when tabs are turned off in its configuration.

Not yet: the agents' state in the lists, a picker of the running agents,
bringing herdr's window forward, and everything about containers below.

## The plan

Herdr is a terminal multiplexer made for AI coding
agents: workspaces, tabs and panes like Zellij's, and on top of them it
knows which agent runs in a pane and whether it is working, idle or waiting
for an answer. unagit should treat it in two ways:

- **As a multiplexer**, level with Zellij: while unagit runs inside herdr,
  Open in New Tab and the splits open there, exactly as they do in Zellij.
- **As the place agents live**: Start Agent… on a repository, a merge
  request or a worktree starts one in herdr, from a unagit inside herdr or
  outside it, and the lists show what each agent is doing. Optionally the
  agent runs in a container that sees only its worktree.

People use herdr differently. Some run everything in it, unagit included;
some keep it for agents alone (a quick terminal on a key of their own) and
navigate in a plain terminal. Both have to work, and unagit cannot know the
key or the window that brings herdr forward - nothing it says may name one.

## What was checked

By hand, with herdr 0.9.1, Ghostty 1.3.1, Docker 29.4.0 under OrbStack and
Claude Code 2.1.293, on 2026-10-08:

- **The herdr CLI works from outside herdr.** With an empty environment (no
  `HERDR_*` variables) `herdr status` and `herdr agent list` reached the
  running server and returned the agents with their states. A unagit in an
  ordinary terminal can create workspaces, start and focus agents, and read
  their states.
- **Herdr tells which agent runs by the foreground process, and its state
  by the screen.** Each agent has a manifest
  (`~/.local/state/herdr/agent-detection/remote/<agent>.toml`) with
  `aliases` for the process name and rules over regions of the screen.
  - `docker run -it … claude` in a pane was not recognised: the foreground
    process is `docker` (named `docker-tools`). The same screen from a
    local `claude` was.
  - `herdr pane report-agent` can name the agent from outside, but then
    the state is whatever was reported last: after reporting `working`
    and `release-agent`, the pane stayed `working` while the screen
    changed. Reporting would need hooks inside the container and a way out
    to the host's herdr socket.
  - **What works:** a small launcher named `claude` that runs `docker run`
    as its child (not `exec`). The pane's foreground process group then
    holds a process named `claude`; herdr recognised the agent and took
    its state from the screen - an "esc to interrupt" line printed inside
    the container read as `working` by the rule `live_turn_working`, as it
    does for a local Claude.
- **A git worktree works in a container** when it and the main clone's
  `.git` are mounted at the same absolute paths as on the host (the
  worktree's `.git` file points at `.git/worktrees/<name>` by absolute
  path). `git status`, the `unagit.mr.*` worktree config and a commit
  inside the container all worked, and the commit was the host's at once;
  OrbStack mapped the container's user to the host's files without any
  `safe.directory`.
- **What the host's git would run can be locked.** `.git/config`,
  `.git/hooks` and `.git/worktrees/<name>/config.worktree` mounted
  read-only over the writable `.git`: `git config core.fsmonitor …`, the
  same with `--worktree`, and writing a hook all failed inside, while a
  commit still went through. Without this, an agent could plant a hook or
  an `fsmonitor` that the host's git - unagit runs it all the time - would
  execute outside the sandbox.
- **Ghostty can be scripted.** Its AppleScript dictionary has `focus` (a
  terminal, "bringing its window to the front"), `activate window` and
  `perform action`. The quick terminal is listed as a terminal that belongs
  to no window. Whether `focus` on it slides it out was not tried: it would
  have moved the user's window under them.

- **What herdr's hooks do depends on the agent.** `herdr integration
  install claude` (integration version 10, tried in a throwaway `HOME`)
  adds one `SessionStart` hook that reports the session's id and
  transcript path (`pane.report_agent_session`) - for resuming it, not for
  its state. Claude's state is the manifest's: rules over the screen and
  over the terminal title Claude sets (a spinner while it works), kept up
  to date by herdr per Claude version - "Do you want to proceed?" with
  numbered options is `blocked`, the prompt box back with no spinner is
  `idle`. Other integrations report the state itself: opencode's plugin
  calls `pane.report_agent` with working, idle and blocked. Every hook
  needs `HERDR_ENV`, `HERDR_PANE_ID` and `HERDR_SOCKET_PATH`, and quietly
  does nothing without them.
- **The host's herdr socket does not reach a container.** Under OrbStack a
  Unix socket on the host, bind-mounted into a container (the file or its
  directory), answered on the host and refused the connection inside.

So the launcher gives a Claude in a container what a Claude on the host
gets without the integration: the same identification and the same screen
rules. What it loses is the session's identity, and for an agent whose
state comes from its hooks it would leave only the screen. It also leans
on which foreground process herdr picks, which herdr does not document.
The two ways that keep the hooks:

- **A relay**: the container's `HERDR_SOCKET_PATH` is a socket inside it,
  served by a small forwarder that passes only `pane.report_agent`,
  `pane.report_agent_session` and `pane.release_agent`, with the pane
  fixed to the container's own, over TCP to unagit on the host
  (`host.docker.internal`), with a token per container. Never the socket
  itself: `send-keys` into another pane would be a way out of the sandbox.
- **Herdr's own remote**: a herdr server inside the container as an SSH
  machine (`herdr machine add`, `ProxyCommand docker exec -i … sshd -i`).
  Supported by herdr and complete - the hooks talk to a server next to
  them - but every container is a machine in herdr's sidebar.

Not tried: herdr's SSH machines (`herdr machine add`) with a container as
the machine. The launcher makes them unnecessary for a container on this
computer.

## Herdr as a multiplexer

`internal/mux` gets a second backend beside Zellij, chosen by the
environment unagit runs in: `HERDR_ENV=1` with `HERDR_PANE_ID` and
`HERDR_SOCKET_PATH`. The same three actions, the same sessions and markers,
the same one-Neovim-per-directory rule:

| Zellij | Herdr |
| --- | --- |
| `new-tab --cwd --name --layout-string` | `tab create --workspace $HERDR_WORKSPACE_ID --cwd --label`, then run the editor in its pane |
| `new-pane --direction right` / `down` near the current pane | `pane split $HERDR_PANE_ID --direction right` / `down --cwd` |
| `list-panes --json` for liveness | `pane list` (all workspaces) |
| `focus-pane-id` | `pane` → its tab: `tab focus`, then the pane; `agent focus` for agents |

One difference matters: a herdr pane starts a shell, and the command is
typed into it (`pane run`), where Zellij takes the command's arguments in a
layout. unagit must not assemble a shell command - the user's shell may be
fish, and a path may hold anything. So the pane runs a constant line,
`exec <unagit> launch`, and the command itself goes in an environment
variable of the pane (`--env UNAGIT_LAUNCH=<the argv, encoded>`); `unagit
launch` decodes it and `exec`s the editor in the directory. The pane then
closes when the editor exits, as Zellij's `close_on_exit` does - to be
checked against herdr.

A unagit outside herdr does not open editors in it: herdr's tabs and splits
are offered only from inside, as Zellij's are.

## Agents

**Start Agent…** on a repository, a merge request (its branch worktree, or
the review worktree with a first prompt) or a worktree. It prepares the
directory as Ctrl-O does, then:

- inside herdr: in a new tab or a split beside unagit, chosen like the
  editor placements;
- outside herdr: in a new herdr workspace labelled after the row (`api
  !42`), focused in herdr, so that whatever brings herdr forward lands on
  it. unagit says only that the agent started in herdr.

The agent is started with `herdr agent start <name> --kind <kind> --pane
<pane>` where it runs on the host, which waits until herdr sees it ready;
in a container with the launcher (below). The name is unique and readable
(`api-42`), and the record of it goes beside the editors' in
`<config>/sessions/`, alive while herdr lists the agent.

**What they are doing.** unagit asks `herdr agent list` while some agent of
its own is recorded - the same rhythm as the panes' liveness - and the
rows of the three lists wear the agent's state: working, idle, and above
all **blocked**, an agent waiting for an answer. A new glyph per state, as
themes require.

**Running Agents…** with a key of its own and in `:`: a `showPicker` table
of the agents - repository, branch or merge request, state, how long - and
Enter focuses the agent in herdr. Inside herdr that is the whole job;
outside it the agent is focused and herdr may still be hidden, which is
what the next section is for.

**Bringing herdr forward**, outside herdr, is a setting with three values:
nothing (the default - the user has their own key), Ghostty, or a command
of the user's. Ghostty uses AppleScript to `focus` the terminal herdr's
client runs in; macOS asks once for permission to control Ghostty. Finding
that terminal is still open: a quick terminal is the one terminal in no
window, which is enough when herdr lives there, and a title or working
directory would be needed otherwise.

## Agents in a container

Opt-in per start (Start Agent in Container…) and per repository default.

- **The launcher.** unagit installs a symlink to itself named after the
  agent, `<config>/agents/claude`, and the pane runs `exec
  <config>/agents/claude` with the container's description in an
  environment variable. Started under that name, unagit runs `docker run`
  as its child, forwards signals and the window size, and exits with it.
  Herdr sees a process named `claude` and does the rest from the screen.
- **What is mounted**, each at its host path: the worktree, read-write; the
  main clone's `.git`, read-write, with `config`, `hooks`, the worktree's
  `config.worktree` and any `config.worktree` of the main clone read-only
  over it. The main clone's own files are not mounted.
- **No forge tokens.** The agent commits; pushing is unagit's, from the
  host, with `P`. The agent's own login lives in a named volume for its
  home (`unagit-agent-claude`), filled once by logging in inside the
  container - unagit never reads or passes it.
- **The image** is the repository's `devcontainer.json` image when it has
  one (its tools for building and testing), otherwise a default unagit
  documents. The agent must be installed in it.
- **Network**: open by default; a restricted mode with an allowlist (the
  agent's API, the package registries) after Anthropic's reference
  devcontainer firewall.
- **One container per agent**, named after the row, removed when the agent
  ends (`--rm`). Deleting a worktree with a running agent offers to stop it
  first.

What the sandbox does not cover: the agent writes code you will run - tests,
build scripts, an `.envrc`, a Neovim `exrc`. It protects the machine while
the agent works, not from what it leaves in the worktree. Refs of other
branches are writable through `.git` as well; what moved shows in the lists.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/mux` | a herdr backend: detection, tab and splits, liveness, focus; the argv in an environment variable |
| `cmd/unagit` | `unagit launch` (decode, `exec`), and the launcher role when started as an agent's name |
| `internal/agents` (new) | the agent kinds, `herdr agent` calls, the container's mounts and arguments |
| `internal/session` | records of agents beside the editors', alive while herdr lists them |
| `internal/ui` | Start Agent…, Start Agent in Container…, Running Agents…, the state marks, the herdr settings in Integrations; help rows and the README |

## Tests

- A fake `herdr` on `PATH`, as `fake_zellij.go` is for Zellij: workspaces,
  tabs, panes, `pane run`, `agent start`, `agent list` with states.
- An opt-in test against a real herdr server in a session of its own
  (`--session`), like `UNAGIT_ZELLIJ_TEST`: open a pane, run a command
  through `unagit launch` with awkward arguments, check where it ran and
  that the pane closes with it.
- An opt-in container test (`UNAGIT_DOCKER_TEST`): the mounts, a commit
  inside, and the locked files refusing writes.
- The launcher: started under an agent's name it runs its child and passes
  the exit status; herdr's process list shows the name.

## Open questions

- Does a herdr pane close when its shell `exec`s an editor that then
  exits?
- Ghostty: does `focus` on the quick terminal slide it out, and how is
  herdr's terminal told apart when it is not in the quick terminal?
- Which agent kinds come first. Claude is checked; each other kind needs
  its process name and that it runs in the default image.
