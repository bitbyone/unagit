# Sandboxes

[unibox](https://github.com/bitbyone/unibox) runs coding agents in sandboxes:
VMs made from **profiles** kept in an environments repository, each sandbox
holding **tasks** - fresh clones of your repositories on a branch, with an
agent working on them. Nothing of the host is mounted; the work leaves as
pushes to the task's branch.

unagit is one interface over it. The integration is on whenever `unibox` is
on `PATH` (Settings › Integrations, under *Sandboxes*), and it adds the
**Sandboxes** tab, `6`. Everything on the tab is read from unibox's `--json`
and everything done there is a unibox command: unagit reads no profile's
YAML and none of unibox's state, so it shows what unibox itself would say.

## The tab

One tree: each profile, the sandboxes made from it, the tasks in each.

| Column | Says |
| --- | --- |
| NAME | the profile, the sandbox under it, the task under that |
| STATE | a profile's images (**built**, or which runtime's is outdated or missing), a sandbox running or stopped, a task ready or its agent **working** |
| AGENT | a profile's agents, those at work in a sandbox, a task's own |
| RUNTIME | the runtime a sandbox runs in, `sbx` or `msb` |
| REPOS, BRANCH | a task's repositories and its branch |
| MEM | a sandbox's memory |

A dot before a sandbox or a profile marks unibox's context - what its
commands work in when they are not told. The detail beside the tree says the
rest: a profile's images by runtime, its agents with how each signs in and
how its commits credit it, its services and whether each token is stored,
its network rules; a sandbox's runtime, resources, tasks, ports with their
addresses, scripts; a task's repositories, branches and commit author. On a
terminal narrower than 140 columns it goes under the tree and opens on
`Enter`.

## Tasks

| Key | |
| --- | --- |
| `o` | **Resume…** its own agent - here or in a tab, split or window; an agent at work is gone to instead |
| `a` | **Resume with Agent…**: another of the profile's agents, then where |
| `s` | a shell in the task's directory, in the sandbox |
| `e` | rename it; an empty name takes the alias away |
| `y` | copy `unibox resume <task>` |
| `w` | the forge's page of its repository |
| `d` | remove it; unibox refuses while it has a session or work not pushed |

The places are those of [Coding Agents](/guide/agents): **This Terminal**,
Zellij and herdr tabs and splits, herdr's Unagit Agents workspace, Ghostty
windows, tabs and splits. Typing in the action picker finds every agent in
every place at once: **Resume with Claude Code in Zellij Vertical Split**,
`cc split`. An agent started here is listed in **Activity**, with its
sandbox and task.

## Sandboxes

| Key | |
| --- | --- |
| `n` | **New Task…**: a worktree or a clone of yours, then the agent, then where |
| `s` | a shell in the sandbox, in `/work` |
| `p` | its ports: `Enter` publishes one or takes it back while it runs, `w` opens it |
| `S` | run one of the profile's scripts now, in a log |
| `i` | the instructions its agents get |
| `x` | stop it; its disk, caches and tasks stay |
| `C` | recreate it from the profile as it is now, or rebuild the image first |
| `d` | remove it with its tasks, after a question naming them |
| `u` | make it the context |

A new task is checked before it starts (`unibox task check`): what unibox
names is shown, and a branch the forge does not have yet is pushed only when
you choose **Push and Start**.

## Profiles

| Key | |
| --- | --- |
| `n` | a new task in one of its sandboxes, or a new sandbox of it |
| `c` | a new sandbox, named by you |
| `b` | build its image; **Rebuild Image** in the picker builds it from scratch |
| `m` | migrate a profile of an older format (`unibox init <profile>`) |
| `D` | what changed since the last build |
| `i` | the instructions its agents get |
| `N` | its network rules: `a` allows a host, `x` denies one, `d` takes a rule back |
| `v` | its services: `a` adds one, `d` takes one out |
| `l` | sign its agents in |
| `O` | its directory in the editor |
| `u` | make it the context |

Tokens never pass through unagit. Signing in and adding a service run unibox
in this terminal, where it asks for the token itself and keeps it in the
platform's store.

`:` has what the tab itself can do: read everything again (`r`), choose the
environments repository among your clones, open it, validate the profiles,
clear the context, prune the images unibox no longer needs.

## From a worktree

**Open in Sandbox…**, in the action picker of a worktree, a grouped worktree
and a clone, does the same as **New Task…** from the other side: the sandbox
(one of a profile's, or **New Sandbox of &lt;profile&gt;…**), the agent, the
place. A grouped worktree goes into one task with all its repositories.
