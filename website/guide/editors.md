# Editors

Everything that opens a directory, `Ctrl-O` and `Ctrl-R`, opens it in your
default editor. Hold Alt with the same key (`Alt-O`, `Alt-R`) and unagit asks
which one first.

Settings › Integrations has a card for each editor, saying whether it was
found: Neovim, IntelliJ IDEA, VS Code, Zed and a custom one. A launcher on
`PATH` or in JetBrains Toolbox's scripts folder is looked for first, then, on
macOS, the application in `/Applications`.

| Key | In an editor's card |
| --- | --- |
| `e` | turn the editor off; it is then offered nowhere |
| `d` | make it the default, marked `(default)`; `d` on the default leaves none |
| `o` | on the Custom card: its command, its arguments and whether it opens a window |

Without a default that is installed and on, every open asks, the way Alt does.
unagit never falls back quietly to another editor.

## Terminal editors and window editors

A **terminal editor** (Neovim, or a custom one) gets the terminal: unagit
suspends itself and waits for it. A **window editor** (IntelliJ IDEA, VS Code,
Zed) is only started, because its launcher returns at once, and unagit carries
on.

## Neovim put aside

With Neovim 0.12 or newer, **`Ctrl-Z`** (or `:detach`) puts it aside and
returns to unagit, keeping the buffers and unsaved changes.

**`E`** on a main list, or **Running Editors…** in `:`, lists these editors:

| Key | Does |
| --- | --- |
| `Enter` | attach in this terminal |
| `a` | **Attach In…** a tab, split or window of Zellij, herdr or Ghostty |
| `x` | close it; with unsaved changes it attaches first and asks in Neovim |

The list shows the repository, branch or merge request, directory, age and
unsaved changes. In the lists, `▣` at the start of a row marks its directory
open in Neovim, and `✦` each coding agent unagit started there.

Opening the same directory in Neovim again attaches to it. Before it is shown,
Neovim reads again whatever changed on disk while it was aside, a review reset
for a force push or a branch pulled, so its buffers and gutter are the files as
they are.

Quitting unagit leaves the editors aside running. The next instance finds
them, and `unagit attach [query]` returns to one from a shell. Nothing closes
an editor because of its age.

::: tip A long configuration path
The sessions directory holds the editors' sockets. A very long configuration
path needs a shorter `UNAGIT_CONFIG_DIR` to fit macOS's Unix socket limit.
:::

## A command of your own

The Custom card takes any command: `o` sets the command, its arguments and
whether it opens a window. Say whether it does, so unagit knows whether to give
it the terminal.

## Alt on macOS

Alt needs the terminal to send Option as Meta. In iTerm2: Profiles › Keys ›
Left Option key › Esc+.
