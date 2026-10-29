# Make Your Own Theme

In this tutorial you fork a built-in theme, change its accent and a glyph, and
watch unagit change as you save.

## 1. Try the themes

From any screen press `:`, type `theme` and choose **Switch Theme…**. Move
through the list: the screen behind is drawn in each theme as the cursor comes
to it. `Esc` puts back the one that was on; `Enter` keeps the one under the
cursor.

Pick one close to what you want, say **gruvbox-dark**.

## 2. Fork it

Go to Settings (`S`) › Theme, put the cursor on the theme and press `f`. unagit
writes it to

```
~/.config/unagit/themes/<name>.json
```

with every colour and glyph spelled out, and puts it on.

## 3. Open the file beside unagit

Open the file in your editor, in another terminal or a split, with unagit
still visible.

## 4. Change the accent

Find `text`, and change `accent`:

```json
"text": { "accent": "#7aa2f7" }
```

Save. unagit puts the change on at once.

## 5. Change a glyph

Hearts for favourites instead of stars:

```json
"glyphs": { "favourite": "♥" }
```

A glyph is one character. Save, and every favourite is a heart.

## 6. Break it on purpose

Write `"accent": "blue-ish"` and save. unagit keeps the last good version on and
says which key is wrong. Put it back.

## 7. Keep only what you changed

A forked file names everything. A theme you share is easier to read when it
names only what it changes, and takes the rest from the one it `extends`:

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

Save it as `midnight.json` beside the other, and `r` in Settings › Theme reads
the folder again.

## 8. Colour one thing

Anything drawn has a role under `colours`, which falls back on a more general
one when left out. To colour the author column of Merge requests alone:

```json
"colours": { "merge_requests.author": "#e0af68" }
```

Every role and its fallback is in
[`internal/ui/themeroles.go`](https://github.com/bitbyone/unagit/blob/main/internal/ui/themeroles.go).

## What you learned

- `f` forks a theme into a file, and every save shows at once.
- A theme names only what it changes, over the one it `extends`.
- Roles fall back on base colours, so a few changes carry through everything.

More in [Themes](/guide/themes).
