# Sandboxes

[unibox](https://github.com/bitbyone/unibox) runs coding agents in Docker
Sandboxes (sbx). A team describes its sandboxes in its own repository, as
sbx's env files (`*.sbxenv.yaml`) and kits; an **env** names those files,
the **sandbox** is the VM sbx creates from it, and each sandbox holds
**tasks** - your checkouts mounted into it, or fresh clones from the forge,
with an agent working on them.

unagit is one interface over it: the **Sandboxes** tab, `5`. Everything on
it is read from unibox's `--json` and everything done there is a unibox
command: unagit reads no sbxenv file and none of unibox's state, so it
shows what unibox itself would say. The integration is on whenever `unibox`
is on `PATH` (Settings › Integrations, under *Sandboxes*); without it the
tab is grey, and going there says so - `Enter` there sets it up.

## The context

unibox works in a **context** - a sandbox, or an env whose sandbox is to be
made - when a command is not told which. unagit shows it on the tab bar,
after **Sandboxes [5]**, in a colour of its own (`●web`), on every screen,
so you know where **Sandbox with…** goes before you start an agent. `u` on a
sandbox or an env makes it the context, `c` on the Environment panel clears
it.

## Setting up

**Set Up unibox…** (`I` on the tab, or `Enter` while the tab is grey) lists
what a first sandbox needs, each with how it stands, read afresh from unibox
every time it opens:

| Step | `Enter` |
| --- | --- |
| **unibox** installed | copies `brew install unibox` |
| **Sandboxes tab** on | turns the integration on |
| **Envs** | picks your team's repository of sbxenv files - a clone, or a git address - and walks `unibox init` through it in this terminal: sbx, the envs, your values, tokens, sandboxes |
| **Values** of each env | lists what it asks of you, the missing first |
| **Tokens** of each env | lists the tokens its kits use and whether sbx has each |
| **Sandbox** of each env | creates it in this terminal, where sbx asks to approve the env |
| **unibox doctor** | runs it, and `Enter` on a check runs the command that fixes it |

**Set Up unibox from Here**, in the action picker of a cloned repository,
runs `unibox init` on it directly.

## The tab

It is laid out as Activity is, in panels; `Tab` goes round them, `h j k l`
past a panel's edge to the one beside it, and each has a key of its own.

| Panel | Key | Shows |
| --- | --- | --- |
| **Sandboxes** | `B` | a card for each sandbox: running or stopped, its agents, its tasks and how many an agent works on; the context marked |
| **Environment** | `E` | the context, the default env, unibox's switches, the tokens sbx keeps |
| **Tasks** | `t` | every sandbox's tasks: the sandbox, ready or **working**, the agent, the repositories, the branch |
| **Envs** | `N` | each env, its sandbox, what stands between it and a start (a value missing) or its sandbox's state, its agent, its tasks |
| **Log** | `L` | every command unagit gave unibox, how it went and what unibox said; `Enter` shows it whole, `y` copies the command |

The detail follows the panel with the focus: an env's files, kits, your
values, the tokens it needs and its repository index; a sandbox's agents,
resources, tasks, ports with their addresses, network rules and the hosts it
was refused lately; a task's repositories - mounted from where, or cloned -
branches and commit author; the context, unibox's settings and the tokens
sbx keeps. On a terminal narrower than 120 columns it goes under the tasks
and opens on `Enter`.

## Tasks

| Key | |
| --- | --- |
| `o` | **Resume…** its own agent on its last conversation - here or in a tab, split or window; an agent at work is gone to instead |
| `a` | **Resume with Agent…**: another agent the sandbox has, then where |
| `s` | a shell in the task's directory, in the sandbox |
| `i` | the instructions its agent reads when a session starts, file by file |
| `+` | add a repository of yours to it, even while its agent runs |
| `O` | open the checkout mounted into it, on the host |
| `e` | rename it; an empty name takes the alias away |
| `y` | copy `unibox resume <task>` |
| `w` | the forge's page of its repository |
| `d` | remove it; a mounted checkout is unmounted, never deleted; unibox refuses while it has a session or work not pushed |

**Start Afresh…** in the picker runs the agent on a new conversation. The
places are those of [Coding Agents](/guide/agents): **This Terminal**,
Zellij and herdr tabs and splits, herdr's Unagit Agents workspace, Ghostty
windows, tabs and splits. Typing in the action picker finds every agent in
every place at once: **Resume with Claude Code in Zellij Vertical Split**,
`cc split`. An agent started here is listed in **Activity**, with its
sandbox and task, under the checkout it works on when that is mounted.

## Sandboxes

| Key | |
| --- | --- |
| `n` | **New Task…**: a clone or a worktree of yours, then the agent, then where |
| `s` | a shell in the sandbox |
| `p` | its ports: `Enter` publishes one or takes it back while it runs, `w` opens it, `t` says whether it is on at each start |
| `f` | its network: `a` allows a host, `x` denies one, `d` takes a rule of yours back, `Enter` allows a host it was refused |
| `M` | its own CPUs and memory, over the env's, applied at the next recreate |
| `x` | stop it; its disk and tasks stay |
| `C` | recreate it from its env as it is now, keeping its tasks, in this terminal |
| `d` | remove it with its tasks, after a question naming them; **Force Remove** for one with work not pushed or made with sbx |
| `u` | make it the context |

A new task is checked before it starts (`unibox task check`): what unibox
names is shown, and a branch the forge does not have yet is pushed only when
you choose **Push and Start**.

## Envs

| Key | |
| --- | --- |
| `n` | a new task in its sandbox, made on the way when it is not yet |
| `c` | create its sandbox now, in this terminal |
| `V` | **Values…**: what it asks of you - `Enter` sets one for this env, its repository's envs or every env, `a` a new one, `d` removes yours |
| `K` | **Tokens…**: the tokens its kits use and whether sbx has each - `Enter` has unibox ask for one in this terminal, `t` keeps it in unagit, `a` asks for every missing one, `d` has sbx forget one |
| `i` | **Repository Index…**: the repositories its tasks are given by name, the team's and yours - `a` names a clone of yours, `d` forgets one, `Enter` says whether a new task takes it |
| `O` | the team's repository its files are in, in the editor |
| `u` | make its sandbox the context |

**Make Default Env**, **Update Env Files** and **Forget Env…** are in the
picker.

## Settings and the rest

From any panel:

| Key | |
| --- | --- |
| `U` | **unibox Settings…**: the default env, whether a sandbox stops when its last session ends, the SSH agent, editors over SSH, and sbx's switches |
| `D` | **Run unibox Doctor**: every check, the failed first; `Enter` runs the unibox command that fixes one, in this terminal |
| `T` | **Sandbox Tokens in unagit…**: the tokens unagit keeps for the sandboxes - `Enter` replaces one, `d` removes it, `y` copies its command |

`:` has the rest: read everything again (`r`), update the envs' files,
prune, install or uninstall unibox's herdr plugin.

## Tokens

A token goes one of two ways. unibox asks for it in this terminal, on a
hidden prompt, offering the page where it is made, and hands it to sbx,
which keeps it; nothing lists its value.

Or unagit keeps it. `t` on a token of an env makes it first where that is
how it is made - `claude setup-token` in this terminal for Claude's
subscription, or the page the env names - then asks for it on a hidden
input, keeps it in unagit's encrypted vault - beside your forge tokens,
never among them - and has `unibox secret set --command` give sbx
`unagit credential get <service>` as where it comes from. sbx runs that
command on the host whenever it wants the token, and it asks the running
unagit over a socket of its own (`~/.config/unagit/run/credential.sock`):
while unagit is closed or locked there is no socket, and sbx is told to
start unagit. No passphrase is asked for and none is kept. Every token
handed out, and every name asked for that unagit does not keep, is in
Activity.

Any program of yours can ask while unagit runs unlocked, an agent outside a
sandbox too: keep tokens there narrow, made for the sandboxes alone. A
forge token unagit works with is never handed out.

A value is no secret - it is kept in `~/.config/unibox/vars.toml` - and is
shown.

## From anywhere in unagit

The action picker of a repository, a worktree, a grouped worktree and a
merge request starts agents in sandboxes:

- **Sandbox with &lt;agent&gt; in &lt;context&gt;…** - the context's sandbox,
  when it has the agent: only where is asked.
- **Sandbox with &lt;agent&gt;…** - asks which sandbox only when more than one
  runs the agent, the context's first, then the one chosen most often; an
  env whose sandbox is not made yet is among them, and its sandbox is made
  on the way. Typing finds it in each place too - **Sandbox with Claude
  Code in Ghostty Window**.
- **Open in Sandbox…** asks everything: the sandbox, the agent, the place.

What goes in depends on what it is. A clone is **mounted**: the agent works
on your files and your `.git` as they are. A worktree cannot be mounted, so
it - or each repository of a grouped worktree, in one task - is **cloned**
in the sandbox from the forge at its branch. A merge request is cloned on
its source branch and a repository not cloned yet on its default one,
whatever is on disk.

**Add to unibox Index…** on a repository names it in unibox's index, for one
env's tasks or every env's, and says whether a new task takes it.

unibox checks the task first. What `unibox run` asks about on the terminal -
a value missing, local changes - is left to it; a branch the forge does not
have is offered **Push and Start** alone, and anything run would refuse is
said without a start to choose. Should run stop all the same, it waits for
Enter, so what it said can be read.
