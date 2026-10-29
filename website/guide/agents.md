# Coding Agents

Claude Code, Codex, Copilot CLI, opencode and Antigravity are integrations of
their own, each on whenever its command is on `PATH`:

| Agent | Command | Short words |
| --- | --- | --- |
| Claude Code | `claude` | `cc`, `claude` |
| Codex | `codex` | `cx`, `codex` |
| Copilot CLI | `copilot` | `copilot` |
| opencode | `opencode` | `oc`, `opencode` |
| Antigravity | `agy` | `agy`, `antigravity` |

## Opening in an agent

Each agent adds **Open with &lt;agent&gt;…** to the actions of a repository, a
merge request and a worktree, and **Review with &lt;agent&gt;…** to a merge
request's.

The directory is prepared as `Ctrl-O` prepares it, cloned and the branch
worktree made, or the review as `Ctrl-R` makes it, the change unstaged. Then
the agent starts there, where you choose:

- **This Terminal**: unagit is suspended until the agent ends
- a tab or split of the Zellij or herdr unagit runs in
- a tab of herdr's **Unagit Agents** workspace
- a Ghostty window, tab or split

The places are listed in the order they are usually chosen. Typing finds them
at once: `cc split` puts Claude Code in a vertical split.

## In Activity

**Activity** (`4`) lists the agents started from unagit, never the others herdr
runs. For those in herdr it says what each is doing:

| State | Section |
| --- | --- |
| **waiting** for an answer | *needs you* |
| **working** | *under way* |
| **idle**, **ended** | *quiet* |

The number waiting is on the tab from every screen. `Enter` goes to the agent:
its herdr tab, its Zellij pane or its Ghostty terminal. `x` closes an agent in
herdr after asking, `Ctrl-O` opens its directory in the editor, and `Alt-A`
lists the agents over any screen.

The [Hand a Review to an Agent](/tutorials/agent-review) tutorial puts it
together.
