# Themes

Everything unagit draws with is a theme: the colours of text, borders, fields,
the selection and every state, the screen's background, and the glyphs that say
what something is (`○ ● ◐ ◉`, `◆`, `★`, `✓ ✗`, `↑ ↓`, the borders).

## Choosing one

Settings › Theme lists them with a strip of their colours. `Enter` puts one on
at once and remembers it.

From any screen, `:` › **Switch Theme…** lists them too. Over a main screen it
tries each theme as the cursor comes to it, drawing the screen behind in it;
only `Enter` keeps it, and `Esc` puts back the one that was on.

## Built in

**unagit**, the muted default, keeps the terminal's own background so it sits
quietly beside an editor. Twenty-eight more paint a background of their own:

| Kind | Themes |
| --- | --- |
| After an IntelliJ theme | retro-block |
| Dark | ayu-mirage, carbonfox, catppuccin-mocha, cyberdream, dracula, everforest-dark, github-dark-dimmed, gruvbox-dark, gruvbox-material-dark, kanagawa-dragon, kanagawa-wave, monokai-pro, moonfly, nightfly, nord, onedark, rose-pine, solarized-dark, sonokai, tokyonight-night |
| Light | catppuccin-latte, everforest-light, github-light, gruvbox-light, rose-pine-dawn, solarized-light, tokyonight-day |

## In Settings › Theme

| Key | Does |
| --- | --- |
| `Enter` | put the theme on |
| `f` | fork it into a file of your own, and put it on |
| `r` | read the themes folder again |
| `b` | keep the terminal's own background under every theme |
| `n` | Nerd Font icons on, off, or back to guessing |

`b` lets a translucent or blurred terminal show through. Every other colour
still comes from the theme. It is kept in `config.yaml` as
`terminal_background`.

## Nerd Fonts

Some glyphs have an icon from a [Nerd Font](https://www.nerdfonts.com) as well:
a theme gives them under `nerd_glyphs`, with a plain character standing in
under `glyphs`. They are drawn when the terminal can. Ghostty, WezTerm and kitty
bring the icons with them, and iTerm2 and Alacritty are asked for their font.
`n` overrides the guess.

A few are icons only: one before each column's heading, the server's before a
repository's name, a draft's icon in place of the word, and a muted icon before
each action in the pickers.

## A theme of your own

Your own go in `~/.config/unagit/themes/*.json`. A theme names only what it
changes and takes the rest from the one it `extends`, the default when it says
nothing:

```json
{
  "name": "midnight",
  "description": "gruvbox with a blue accent and hearts for favourites",
  "extends": "gruvbox-dark",
  "background": "#101418",
  "text": { "accent": "#7aa2f7" },
  "glyphs": { "favourite": "♥" }
}
```

A colour is `"default"` (the terminal's own), `"#rrggbb"`, a number of the
256-colour palette (`"109"`) or a colour name.

The top-level keys are `text`, `state`, `border`, `tabs`, `surface`,
`selection`, `backdrop` (the dimming behind a dialog), `markdown`, `chezmoi`,
`tags`, `glyphs` and `borders`. Every key, with what it is for, is in
[`internal/ui/themes/unagit.json`](https://github.com/bitbyone/unagit/blob/main/internal/ui/themes/unagit.json).

### Roles

Anything else drawn has a colour of its own under `colours`, by name:
`repositories.mr`, `worktrees.size`, `merge_requests.author`,
`diff.added_fill`, `syntax.keyword`. Each falls back on a more general one when
the theme leaves it out: a list's column on the column of every list
(`column.mr`), that on a base colour (`text.accent`). So a theme changes a few
base colours and everything follows. Every name and its fallback is in
[`internal/ui/themeroles.go`](https://github.com/bitbyone/unagit/blob/main/internal/ui/themeroles.go).

A theme with a background of its own does not have to colour the tags' pills:
those it leaves out are worked out of its colours, so all sixteen look alike in
weight and belong to the theme.

### Editing live

Edit the file with unagit open beside it: each save puts the change on at once.
A save that breaks the file keeps the last good version on and says which key
is wrong. A file that cannot be used is listed in the section with what is
wrong, and the others still load.

The [Make Your Own Theme](/tutorials/own-theme) tutorial goes step by step.
