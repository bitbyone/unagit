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

**Add to unibox Index…** on a repository names it for unibox's tasks.

## The context

The sandbox unibox works in when not told is on the tab bar, after
**Sandboxes [5]**, in a colour of its own, on every screen. `u` on a
sandbox or an env makes it the context; `c` on the Environment panel clears
it.

## The tab

| Panel | Key | |
| --- | --- | --- |
| **Sandboxes** | `B` | a card each: running or stopped, its agents, its tasks |
| **Environment** | `E` | the context, the default env, unibox's switches, the tokens sbx keeps |
| **Tasks** | `t` | every sandbox's tasks, **working** while an agent is at it |
| **Envs** | `N` | each env, its sandbox, what it still lacks |
| **Log** | `L` | every command given to unibox, and what it said |

On a task `o` resumes its agent, `a` with another, `s` opens a shell, `i`
shows what the agent is told. On a sandbox `n` starts a task, `p` `f` `M`
are its ports, network and resources, `C` recreates it. On an env `V` and
`K` are your values and its tokens. `U` is unibox's settings, `D` its
doctor, `I` setting up step by step. `?` has every key.

What sbx or unibox asks for - approving an env, a token - is asked in this
terminal; unagit never sees a token unless you keep it in its vault.

## Tokens kept by unagit

`t` on a token of an env keeps it in unagit's encrypted vault instead of
sbx's store, and has sbx get it with `unagit credential get <service>`,
answered by the running, unlocked unagit over a socket of its own. Closed
or locked, there is no socket. Every handout is in Activity; a forge token
of unagit's is never one. Keep tokens there narrow: any program of yours
can ask while unagit runs.
