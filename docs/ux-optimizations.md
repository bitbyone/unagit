# UX optimisations

A plan, agreed with the user on 2026-10-08, not yet built. It comes from
using herdr, Zellij, Ghostty, Neovim put aside and the coding agents for a
while. What already works well and is to be kept as it is:

- Neovim put aside with Ctrl-Z, and brought back.
- Tabs and splits offered only for the multiplexer unagit runs in - herdr
  or Zellij - and none outside; Ghostty offered always, with its windows,
  tabs and splits to choose from.
- Attaching from `E` to a Neovim whose pane is open goes to that pane.

## 1. Bring a Neovim put aside back where you choose (done)

Today Enter in `E` attaches in unagit's own terminal only: a Neovim whose
herdr tab closed when it was put aside can come back nowhere else.
`attachInPane` already does the rest; `E` does not offer it.

Enter in `E` asks where - this terminal, a tab or split of the
multiplexer unagit runs in, a Ghostty window, tab or split - the cursor on
the place most often chosen, so Enter Enter is as quick as one Enter is
now. The same picker of places as the agents' (`agentPlaces`), without the
herdr workspace: editors go to herdr only from inside it (see 2).

## 2. Opening: three tiers of actions, named by a grammar

Opening is three questions: **what** (the clone, a merge request's branch
worktree, its review worktree), **with** what (an editor, an agent) and
**where** (this terminal, a tab, a split, a window). Every combination as
an action would be dozens. Instead:

**Tier 1, always at the top of the action picker:**

- **Open** (Ctrl-O, today "Open in Editor"): the favourite editor, here.
- **Review** (Ctrl-R, merge requests only): the review worktree, the
  favourite editor, here.
- **Open…** (`O`): a small form - Mode (Branch / Review, merge requests
  only), With (editors and agents that are on, the favourite first), Where
  (the places that tool can go: a window editor has none) - filled in as it
  was last used for that kind of row. Enter opens. Built from
  `showFormModalSized` and `addSelect`, with its NORMAL and INSERT modes.
- **Open With…** (Alt-O, today "Open in Editor…"): choose the editor, here.

**Tier 2, at the bottom of the picker, found by typing:**

- **Open with \<agent\>…** for each agent that is on, and **Review with
  \<agent\>…** on merge requests: the review worktree made as Ctrl-R makes
  it - the change unstaged - and the agent started in it. Each asks where,
  the places ordered by how often each was chosen (herdr first, for the
  user).

**Tier 3, shown only while filtering, run at once:**

- Places for the favourite editor: "Open in Zellij Vertical Split",
  "Review in herdr Tab", "Open in Ghostty Window"…
- Agents with a place: "Open with Claude Code in Zellij Vertical Split",
  "Review with Claude Code in herdr", "Open with Codex in herdr" - the last
  is a tab of the Unagit Agents workspace; no more herdr choice is needed
  for now.

The grammar: **with** names the tool, **in** names the place, and `…` ends
a name exactly when another choice follows. A name without `…` runs.

What keeps tier 3 in check:

- Only tools that are on and places that are there: tabs and splits of the
  multiplexer unagit runs in, Ghostty when it is on, and for agents herdr
  always - herdr runs as a service, reachable from a plain Ghostty window.
  Editors go to herdr only from inside it, so herdr stays the agents'.
- With an editor, only the favourite. Another editor in a split is Open…
  or Open With…; otherwise the editors alone would make hundreds.
- `uiAction` gets a flag for actions shown only while filtering.

**Finding by aliases.** An action has aliases beside its name, and
filtering matches them: Claude Code `cc` `claude`, Codex `cx` `codex`,
vertical split `vsplit` `vs` `split`, horizontal split `hsplit` `hs`,
`tab`, review `rev`. The ranking: every word of the query matching an
alias or the start of a word of the name, then the fuzzy match, then a
preference among equals - a vertical split before a horizontal one, being
the more usual. `cc split` puts "Open with Claude Code in herdr Vertical
Split" first. The explanation under the picker shows an action's aliases,
so they can be learnt.

## 3. Integration cards scroll, and keep their background inside the frame

- A card at the edge of the scrolled area disappears whole, because tview
  draws a widget past its rectangle and the card would cover the panel's
  frame. Instead a card is drawn through a screen that drops the cells
  outside the viewport, so it slides under the edge. (done)
- An unfocused card's background spills a cell past its right frame and a
  row below its bottom one (Herdr, Codex, opencode in the screenshot): the
  card gets its grid row's height, the frame is drawn for its own. The
  background must end with the frame. (Not reproduced on the simulation
  screen, in any built-in theme, at any size, with or without Nerd Font
  icons: every card cell lies inside its rectangle. Waiting for the
  terminal, theme and size it was seen in.)

## 4. h and l between the sidebar and the content, everywhere in Settings

Integrations already does it: `h` from the first column goes back to the
sections, `l` from a section goes in. Every section is to work so, by one
rule: `h` goes back to the sections whenever the content has no use for it.

- Groups & roots: `h` and `l` fold and unfold the tree; `h` goes back from
  a root or a folded node, where it would do nothing.
- Forms (General, Security): in NORMAL mode `h` goes back; in INSERT it
  types.
- Lists (servers, tags, themes): `h` goes back.

The rule goes into AGENTS.md.

## 5. An integration's state as an icon

The cell of colour before the state is too wide. With a Nerd Font it is
`nf-fa-circle_dot` (U+F192) in the state's colour, then the word:
`Zellij ──── ◉ enabled ─╮`. Without one, the cell of colour stays. A key
under `nerd_glyphs` in the theme, like the agents' icons.

## 6. Rows open in an editor or an agent stand out

- The marks column shows the editor's icon and after it the icon of every
  agent unagit started in that directory; both when both. The column takes
  room only when a row has something in it. Repositories, Merge requests
  (branch and review worktrees) and Worktrees.
- An agent's icon takes the colour of its state in herdr: waiting for an
  answer in the warning colour, working in the accent, idle as it is - so
  an agent waiting shows in the lists, not only on the Agents tab.
- Only what unagit can tell is still running counts as open: Neovim and
  terminal editors in a pane, and the agents. A window editor (Zed, IDEA,
  VS Code) does not, since nothing says when its window closes.
- A row with anything open has a background a twentieth lighter than the
  page's. Where the theme leaves the background to the terminal it cannot
  be lightened, so it is a new role, `row.open`, with a fallback the
  default theme sets. The cursor's band wins over a marked row's, and that
  over an open row's. Tag pills on such a row get a `banded` markup on its
  background (`keptTable`), or they lose their fill - this has gone wrong
  twice; `TestTagsOnRepositories` is to check this row too.

## 7. The open editor's own icon

Neovim's mark is `nf-custom-neovim` (U+E6AE). An editor without an icon of
its own, or a terminal without a Nerd Font, keeps today's mark. The font
also has `dev-intellij` (U+E7B5) and `dev-vscode` (U+E8DA), should those
ever count as open; Zed has none.

## 8. Every editor an integration of its own

The Editors card becomes a card per editor - Neovim, IntelliJ IDEA, VS
Code, Zed, Custom - each with its state and `e`, so one can be turned off
and is then offered nowhere: not in "Open with", not in Alt-O, not in
Open….

- `f` on an editor's card makes it the favourite, marked with a star in
  its title; `f` on the favourite takes the star off, and every open asks,
  as "None" does today.
- A favourite turned off counts as none: opening asks, and never falls
  back quietly to another editor (AGENTS.md).
- The custom editor's settings - command, arguments, terminal or window -
  move from General to the Custom card, so an editor is all in one place.
- The category splits in two: **Editors** (the five) and **Review**
  (Incomm, Hunk).
- In the configuration as the agents are: an editor not named is on
  whenever it is installed.

## 9. An agent's action carries the agent's icon

Every action in the action pickers that opens an agent - today's "Open in
\<agent\>…" (`agentActions`), and 2's "Open with \<agent\>…", "Review with
\<agent\>…" and its tier 3 - is drawn with that agent's icon before its
name, the one the Agents tab uses (`agentIcons`). Without a Nerd Font the
name stands alone, aligned with the other actions rather than indented.

## 10. The marks read from the name outwards

Today a row begins with the favourite's star and the state on disk, then
the open editor's mark, then the name: the column of marks that are only
now and then drawn stands between the name and the state every row has,
and leaves a gap there. The order turns round, read from the name
leftwards by how often a mark is there:

```
[editor, agents]  [favourite]  [state on disk]  name
```

The state on disk, drawn on every row, sits against the name; the star to
its left; the open editor's and the agents' icons (6) furthest out, the
column taking room only when a row has one. Repositories, Merge requests
and Worktrees alike, and the kept markup of the cursor's band and marked
rows (`keepEditorMark`, the star's) moves with them.

## 11. Running Editors stays open while editors are closed (done)

`x` in `E` closed the list with the editor: it flashed up and went, and
closing several meant opening it again for each. The list stays in front,
the closed row gone at once, the wait in its bottom edge with the spinner
(`waitInDialog`); the user closes it. One with unsaved changes is still
attached to be closed there, and the list is read again when it is back.

## 12. Sizes only while the SIZE column is shown (done)

Measuring every clone at start costs a small lag while moving in
Repositories, for a column the user may have hidden. Sizes are measured
only while SIZE is shown, and when it is shown again; and they can be
measured again on request, since nothing else brings them up to date.

## Order of work

1, 3, 4, 5 and 9 are small and independent; 2's new agent actions take 9's
icon from the start. 7 and 8 go together (the editors'
identity), then 10 and 6 together, 6 using 7's icons in the column 10
moves. 2 is the largest and last: the
filter-only flag, aliases and ranking in the action picker, then Open…,
then the tiers.
