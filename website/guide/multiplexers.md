# Zellij, herdr, Ghostty, Rex

The opening keys use unagit's own terminal. Inside a multiplexer, on a Mac
with Ghostty, or with Rex, the action pickers offer more places.

## Zellij

Inside [Zellij](https://zellij.dev), the selection action picker (`.`)
has:

- **Open in Zellij Tab**
- **Open in Zellij Vertical Split**, beside unagit
- **Open in Zellij Horizontal Split**, below it

and a merge request the same with **Review**. They are found by typing, `tab`,
`split` or `vs`, `hs`, and use the default terminal editor, or ask among
terminal editors when the default opens a window. `O` (**Open…**) chooses the
editor and the place in one form.

A repository is cloned if needed and a merge request gets its branch worktree,
as with `o`. Tabs are named after the repository, merge request, branch or
group. unagit stays usable while the editor runs, and the pane closes when its
editor exits.

Zellij's default keys take `Ctrl-O`, `Ctrl-T`, `Ctrl-G`, `Ctrl-S`, `Ctrl-P`
and `Ctrl-N` for its modes. Unagit's own keys need none of them; one you gave
an action in [`keybinds.toml`](./actions#your-own-keys) may.

Neovim in a pane is the same Neovim as in unagit's own terminal: `Ctrl-Z` puts
it aside, the pane closes, and `E` lists it. Opening its directory again brings
it back where you ask. While it still has its pane, opening the directory goes
to that pane instead of starting a second Neovim.

From a unagit outside that Zellij session the pane cannot be brought forward,
so unagit asks: **Attach Here Too** opens the same editor in this terminal as
well, and **Take Over** puts the pane's window aside and opens it here.

Requires Zellij 0.45.1 or newer. No setting is needed; the actions appear only
inside Zellij. tmux is not implemented yet.

## herdr

[herdr](https://herdr.dev) is a terminal multiplexer made for coding agents.
Inside it, unagit treats it as it treats Zellij: **Open in herdr Tab** and the
two splits open the editor beside unagit, and the records, the open-editor
marker, `E` and `Ctrl-Z` all work the same.

From anywhere, herdr or a plain terminal, an agent can be started in herdr:
every one goes to a workspace called **Unagit Agents**, a tab each, and herdr
is switched to it. herdr itself starts the agent, so it follows what the agent
is doing, and Activity shows it.

On whenever `herdr` is on `PATH`; `e` in its card turns it off. Requires herdr
0.9 or newer.

## Ghostty

On a Mac, **Open in Ghostty Window** and **Open in Ghostty Tab** open the
default terminal editor in a new [Ghostty](https://ghostty.org) window or tab.
When unagit runs in Ghostty itself, not in a multiplexer inside it, the Ghostty
splits open it beside unagit. Agents can be opened in the same places.

Ghostty is scripted through AppleScript: macOS asks once whether unagit may
control it. A tab cannot be opened when tabs are turned off in Ghostty's
configuration; use a window.

## Rex

**Open in Rex Session** opens the default terminal editor in a tab of
[Rex](https://www.superlogical.com/rex)'s **Unagit** session, made the first
time and kept running by Rex's server. It works from any terminal, as long as
`rex` is on `PATH`.

When unagit runs in a Rex terminal itself, not in a multiplexer inside it,
there is more:

- **Open in Rex Tab**, a new tab of the session unagit is in
- **Open in Rex Vertical Split**, beside unagit
- **Open in Rex Horizontal Split**, below unagit

Agents, and a sandbox task resumed with an agent, can be opened in
the same places. A tab is named after what it opens. unagit runs the program
with its own `PATH` and finds it again by its block; while Rex's server cannot
be reached, nothing it runs is taken for closed.

Rex's app keeps its own tab in each session, apart from the server's focus,
so unagit asks the app to show what it opened, and what **Go to Agent** or
**Resume…** of a task at work goes back to. The app takes that from `rex`
only with **Remote Control** on, in Rex's Settings › Server › Client Info.
Without it the tab still opens, and unagit says what to turn on.

## Following them

`unagit sessions` and `unagit cd` include these panes. Records follow the pane
in its original session, including after unagit exits, and a later instance
finds them again. Each opening also records a zoxide visit when that
integration is on.
