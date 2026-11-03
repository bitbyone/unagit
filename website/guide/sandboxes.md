# Sandboxes

[unibox](https://bitbyone.github.io/unibox/) runs coding agents in Docker
Sandboxes. A team describes a sandbox in its own repository as sbx files; an
**env** names them, the **sandbox** is what sbx makes from it, and a
**task** is a set of your repositories in it with an agent at work.

unagit is a UI over unibox: the **Sandboxes** tab, `5`, and **Sandbox
with…** wherever there is something to work on. Everything shown is
unibox's `--json`, and everything done is a unibox command. The tab is on
while `unibox` is on `PATH`; without it, it is grey and `Enter` there sets
unibox up.

![The Sandboxes tab: the sandboxes, the tasks with the detail of one, the envs](/shots/sandboxes.svg)

## From anywhere

On a repository, a worktree, a grouped worktree or a merge request:

- **Sandbox with Claude Code in platform…** - the sandbox of unibox's
  context, asking only where the agent runs.
- **Sandbox with Claude Code…** - asks which sandbox only when more than
  one has the agent; one not made yet is made on the way.
- **Open in Sandbox…** - asks for everything.

A clone is **mounted**: the agent works on your files as they are. A
worktree, and each repository of a grouped one, is **cloned** in the
sandbox at its branch; so is a merge request, on its source branch, with
nothing needed on disk. Typing finds each agent in each place:
**Sandbox with Codex in Zellij Tab**.

**Add to Unibox Index…** on a repository names it for unibox's tasks; on
marked repositories it names them all, asking which index and when once.

## Setting up from a repository

**Unibox Init…** on your team's repository of sbxenv files lists every
`*.sbxenv.yaml` in it, each marked **new** or with the env unibox already
made of it. `Enter` sets up the file under the cursor; `i` every file, or
only the marked ones (`space`). What follows is `unibox init` itself in
this terminal - sbx's sign-in, your values, the tokens, the sandboxes - so
the questions are always unibox's own; afterwards the step-by-step setup
(`I`) says what is still missing. A repository that is not cloned yet is
cloned first: by unagit where it keeps its clones, or by unibox under its
own data directory, which `unibox env update` keeps current.

## What is missing

The tab counts, from every screen, what stands between the sandboxes and a
start: `Sandboxes [5] !2` - an env that does not load or wants a value of
yours, a sandbox older than its env. That last one only unibox's doctor
can tell, so unagit asks it when you open the doctor (`D`), and right
after a pull moves the team's repository an env is read from: the env
then reads **env changed**, and unagit offers to recreate its sandbox.

A clone that a task mounts is the agent's work as well: **Delete from
Disk…** names the task in its question, first when a session of its agent
is open.

## The context

The sandbox unibox works in when not told is on the tab bar, inside
**Sandboxes [5]** and quieter than its name, on every screen. unibox is
read again when the tab or the terminal comes to the front; `R` reads it
at once. `u` on a
sandbox or an env makes it the context; `c` on the Environment panel clears
it.

## The tab

| Panel | Key | |
| --- | --- | --- |
| **Sandboxes** | `B` | a card each: running or stopped, its agents, its tasks |
| **Environment** | `e` | the context, the default env, unibox's switches, the tokens sbx keeps |
| **Tasks** | `t` | every sandbox's tasks, **active** while an agent has a session in it |
| **Envs** | `N` | each env, its sandbox, what it still lacks |
| **Log** | `L` | every command given to unibox, and what it said |

On a task `o` resumes its agent, `a` with another, `s` opens a shell, `i`
shows what the agent is told. On a sandbox `n` starts a task - `Ctrl-S` from any screen, in the
context's sandbox: Space marks its repositories, then a form takes its
name, branch, checkouts, push and agent. `p` `f` `M`
are its ports, network and resources, `C` recreates it. On an env `V`, `K`
and `p` are your values, its tokens and its ports; among ports `a` names
one of yours and `d` forgets it. `U` is unibox's settings, `D` its doctor,
`I` setting up step by step. `?` has every key.

## Your configuration

`Y` lists everything you said to unibox beside the teams' files, a tab for
each kind: **Settings** (and your instructions), **Values**, **Ports** and
**Repositories** - each for every env, an env repository's envs or one
env - the **Tokens** sbx keeps and the sandboxes' **Network** rules. `Tab`
or a tab's number goes to another; `Enter` opens the dialog where one is
managed, `a` adds one, `d` forgets it where it was said.

What sbx or unibox asks for - approving an env, a token - is asked in this
terminal; unagit never sees a token unless you keep it in its vault.

## Tokens kept by unagit

`t` on a token of an env keeps it in unagit's encrypted vault instead of
sbx's store, and has sbx get it with `unagit credential get <service>`,
answered by the running, unlocked unagit over a socket of its own. Closed
or locked, there is no socket. Every handout is in Activity; a forge token
of unagit's is never one. Keep tokens there narrow: any program of yours
can ask while unagit runs.
