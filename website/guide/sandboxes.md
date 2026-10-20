# Sandboxes

[unibox](https://github.com/bitbyone/unibox) runs coding agents in sandboxes:
VMs made from **profiles** kept in an environments repository, each sandbox
holding **tasks** - fresh clones of your repositories on a branch, with an
agent working on them. Nothing of the host is mounted; the work leaves as
pushes to the task's branch.

unagit is one interface over it: the **Sandboxes** tab, `5`. Everything on
it is read from unibox's `--json` and everything done there is a unibox
command: unagit reads no profile's YAML and none of unibox's state, so it
shows what unibox itself would say. The integration is on whenever `unibox`
is on `PATH` (Settings › Integrations, under *Sandboxes*); without it the
tab is grey, and going there says so - `Enter` there sets it up.

## Setting up

**Set Up unibox…** (`I` on the tab, or `Enter` while the tab is grey) lists
what a first sandbox needs, each with how it stands, read afresh from unibox
every time it opens:

| Step | `Enter` |
| --- | --- |
| **unibox** installed | copies `brew install unibox` |
| **Sandboxes tab** on | turns the integration on |
| **Runtime** ready - sbx signed in to Docker, say | runs what unibox suggests, such as `sbx login`, in this terminal |
| **Environments repository**, where the profiles live | picks a clone to lay out with `unibox init --dir`, or **New Repository…** first |
| **Profiles** | adds one: **New unibox Profile…** with its name, agents, CPUs and memory, or **unibox Profile from Template…** |
| **Image** of each profile, for the default runtime | builds it, in a log |
| each profile **ready to start** - its agents signed in, its values set | goes to what is missing: Sign In Agents…, unibox Values…, Services… |

**unibox Profile from Template…** copies a profile from a repository of yours
that holds profiles. It lists your repositories, clones first, and clones the
one picked when it is not. `unibox template inspect` reads it, and its
profiles are listed with their agents, the values each asks for and the
tokens it needs. The one picked is copied with
`unibox init <name> --from <clone> --use <profile>`, under a name of your
choosing when you have one of its already.

A repository of your own becomes the environments repository from
Repositories, too: create it as any other (**New Repository…** in the action
picker), then **Init unibox Here** on it. While another repository holds the
profiles, it asks first; the old one stays as it is.

## The tab

It is laid out as Activity is, in panels; `Tab` goes round them, `h j k l`
past a panel's edge to the one beside it, and each has a key of its own.

| Panel | Key | Shows |
| --- | --- | --- |
| **Sandboxes** | `B` | a card for each sandbox: running or stopped, its profile and runtime, processors, memory, tasks, how many an agent works on; the context marked |
| **Environment** | `E` | the profiles' repository, the context, the runtimes, how many secrets and values are kept |
| **Tasks** | `t` | every sandbox's tasks: the sandbox, ready or **working**, the agent, the repositories, the branch |
| **Profiles** | `P` | each profile's image for the default runtime (**built**, outdated or missing; the other runtimes' in the detail), its agents, how many sandboxes are made from it |
| **Log** | `L` | every command unagit gave unibox, how it went and what unibox said; `Enter` shows it whole, `y` copies the command |

The detail follows the panel with the focus: a profile's images by runtime,
its agents with how each signs in and how its commits credit it, its
services and whether each token is stored, its network rules, the values it
asks for and which are missing; a sandbox's runtime, resources, tasks, ports
with their addresses, scripts; a task's repositories, branches and commit
author; the environment's runtimes and what is chosen for each, the secrets
by name and your values. On a terminal narrower than 120 columns it goes
under the tasks and opens on `Enter`.

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
| `!` | run one of the profile's scripts now, in a log |
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
| `v` | its services: `a` adds one, `t` adds one whose token unagit keeps, `d` takes one out |
| `A` | sign its agents in: `Enter` with a token unagit keeps, `r` makes it again, `u` with unibox's own sign-in |
| `O` | its directory in the editor |
| `u` | make it the context |

## Secrets, values and settings

From any panel:

| Key | |
| --- | --- |
| `K` | **unibox Secrets…**: the tokens kept in the platform's store, by name - `a` stores one, `Enter` replaces it, `d` removes it |
| `V` | **unibox Values…**: your own values for what profiles ask of you - a name, a login - `Enter` sets one, `a` a new one, `d` removes one; a value a profile asks for and lacks is listed as **missing** |
| `T` | **Sandbox Tokens in unagit…**: the tokens unagit keeps for the sandboxes - `Enter` replaces one, `d` removes it, `y` copies the command a profile names |
| `U` | **unibox Settings…**: the runtime new sandboxes get, which runtimes may be used, whether a sandbox stops when its last session ends |

On the Environment panel `e` chooses the environments repository among your
clones, `O` opens it, `c` clears the context. `:` has the rest: read
everything again (`r`), validate the profiles, prune the images unibox no
longer needs.

A token goes one of two ways. With unibox's own sign-in, a service's `a` and
storing a secret, unibox asks for it in this terminal, on a hidden prompt, and
keeps it in the platform's store; unibox lists the secrets by name alone.

Or unagit keeps it. `Enter` on an agent of `A` makes the token first, as
unibox's sign-in does - `claude setup-token` in this terminal, which signs
your subscription in through the browser and prints the token, or the page
where Copilot's or Codex's is made - and then asks for it on a hidden
input; `r` makes a new one for an agent signed in already. That, and `t` in a
profile's services, take a token into unagit's encrypted vault - beside your forge
tokens, never among them - and have unibox write
`unagit credential get <name>` into the profile as where it comes from. The runtime runs that
command on the host whenever it wants the token, and it asks the running
unagit over a socket of its own (`~/.config/unagit/run/credential.sock`):
while unagit is closed or locked there is no socket, and a sandbox starting
then fails and says to start unagit. No passphrase is asked for and none is
kept. Every token handed out, and every name asked for that unagit does not
keep, is in Activity.

Any program of yours can ask while unagit runs unlocked, an agent outside a
sandbox too: keep tokens there narrow, made for the sandboxes alone. A
forge token unagit works with is never handed out.

A value is no secret - it is kept in `~/.config/unibox/vars.toml` - and is
shown.

## From a worktree

**Open in Sandbox…**, in the action picker of a worktree, a grouped worktree
and a clone, does the same as **New Task…** from the other side: the sandbox
(one of a profile's, or **New Sandbox of &lt;profile&gt;…**), the agent, the
place. A grouped worktree goes into one task with all its repositories.

**Sandbox with &lt;agent&gt;…** names the agent at once, and typing finds it
in each place too - **Sandbox with Claude Code in Ghostty Window**. Each
asks which sandbox, the one chosen most often at the top, or a new one of a
profile that offers the agent.

unibox checks the task first. What `unibox run` asks about on the terminal -
a login, local changes - is left to it; a branch the forge does not have is
offered **Push and Start** alone, and anything run would refuse is said
without a start to choose. Should run stop all the same, it waits for Enter,
so what it said can be read.
