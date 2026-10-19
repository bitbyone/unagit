# Integrations

unagit opens into the tools you already use. Each is configured in
**Settings › Integrations**, a card each, with its state at the right of its
title: **enabled**, **disabled** or **not installed**.

| Key | In a card |
| --- | --- |
| `e` | enable or disable it |
| `c` | check the installation again |

Most are on as soon as their program is found on `PATH`.

## Editors

| | |
| --- | --- |
| [Neovim](/guide/editors) | the default; takes the terminal, can be put aside with `Ctrl-Z` |
| [IntelliJ IDEA, VS Code, Zed](/guide/editors) | open a window of their own |
| [Custom](/guide/editors#a-command-of-your-own) | any command you give it |

## Review

| | |
| --- | --- |
| [Incomm](/guide/incomm-hunk#incomm) | merge request comments in your editor |
| [Hunk](/guide/incomm-hunk#hunk) | a terminal diff viewer for a whole changeset |

## Terminals

| | |
| --- | --- |
| [Zellij](/guide/multiplexers#zellij) | tabs and splits beside unagit |
| [herdr](/guide/multiplexers#herdr) | a multiplexer for coding agents |
| [Ghostty](/guide/multiplexers#ghostty) | windows, tabs and splits on a Mac |

## AI agents

[Claude Code, Codex, Copilot CLI, opencode, Antigravity](/guide/agents): open a
repository, a worktree or a review in an agent.

## Sandboxes

[unibox](/guide/sandboxes): the sandboxes agents run in, their profiles and
tasks, on a tab of their own.

## Files and navigation

| | |
| --- | --- |
| [chezmoi](/guide/chezmoi) | your dotfiles checkout is the main clone |
| [Yazi](/guide/yazi-zoxide#yazi) | browse a repository's files |
| [zoxide](/guide/yazi-zoxide#zoxide) | visits recorded, and an order by frecency |

## Where things open

Opening is three questions: **what** (the clone, a merge request's branch or
its review, a worktree), **with what**, and **where**. The action pickers answer
them in three tiers:

1. At the top, **Open** (`Ctrl-O`) and **Review** (`Ctrl-R`) in the default
   editor here, **Open With…** (`Alt-O`) in an editor you choose, and
   **Open…** (`O`), a form of all three questions, filled in as it was last
   used for that kind of row.
2. At the bottom, **Open with &lt;agent&gt;…** and **Review with &lt;agent&gt;…**,
   which ask where.
3. Found only by typing, and run at once: every place for the default editor,
   **Open in Zellij Vertical Split**, **Review in herdr Tab**,
   **Open in Ghostty Window**, and every agent in every place.

**with** names the tool and **in** the place, and `…` ends a name exactly when
another choice follows.
