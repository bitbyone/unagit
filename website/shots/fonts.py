#!/usr/bin/env python3
"""Make unagit mono, the font the website shows unagit's terminal in - the
home page's player, the screenshots, the video: JetBrains Mono with Nerd
Font's icons (JetBrainsMonoNL Nerd Font Mono), cut down to what unagit
draws, under a name of its own as its licence (OFL) asks of a changed font.

The box lines and a pill's round ends are drawn anew. A terminal draws
them itself, edge to edge of the cell; set in a font, their lines stopped
short of the player's 1.333 em rows and the frames came out dashed. Here
each line runs a little past the cell's edges, on JetBrains Mono's grid
(600 units wide, 1020 above the baseline and 300 below).

    pip install fonttools brotli
    python3 website/shots/fonts.py [JetBrainsMono.tar.xz]

It writes website/shots/fonts/*.ttf (the screenshots and the video draw
with them) and website/public/fonts/*.woff2 (the player loads them), with
the licence beside each. Run it again when unagit draws a new icon.
"""
import json
import math
import pathlib
import re
import shutil
import sys
import tarfile
import tempfile
import urllib.request

from fontTools import subset
from fontTools.pens.recordingPen import DecomposingRecordingPen
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import TTFont

UPM, WIDTH, ASCENT, DESCENT = 1000, 600, 1020, 300
# The player's rows are 1.333 em; the lines run that far and a little past
# it, so neighbouring rows overlap rather than leave a seam.
ROW = 1333
TOP = (ASCENT - DESCENT) / 2 + ROW / 2 + 12
BOTTOM = (ASCENT - DESCENT) / 2 - ROW / 2 - 12
LEFT, RIGHT = -12, WIDTH + 12
CX, CY = WIDTH / 2, (ASCENT - DESCENT) / 2
LIGHT, HEAVY = 80, 150


def rect(x0, y0, x1, y1):
    return [(x0, y0), (x0, y1), (x1, y1), (x1, y0)]


def arm(direction, t):
    """One arm of a line from the cell's centre to an edge."""
    h = t / 2
    return {
        "l": rect(LEFT, CY - h, CX + h, CY + h),
        "r": rect(CX - h, CY - h, RIGHT, CY + h),
        "u": rect(CX - h, CY - h, CX + h, TOP),
        "d": rect(CX - h, BOTTOM, CX + h, CY + h),
    }[direction]


def corner(h, v, t):
    """A round corner joining the arm to side h (+1 right, -1 left) and the
    arm to side v (+1 up, -1 down), as the terminal's own are: a quarter
    circle as wide as half the cell."""
    r = WIDTH / 2
    ox, oy = CX + h * r, CY + v * r
    a0 = math.atan2(-v, 0)
    a1 = math.atan2(0, -h)
    if a1 - a0 > math.pi:
        a1 -= 2 * math.pi
    if a0 - a1 > math.pi:
        a1 += 2 * math.pi
    steps = 24
    outer = [(ox + (r + t / 2) * math.cos(a0 + (a1 - a0) * i / steps),
              oy + (r + t / 2) * math.sin(a0 + (a1 - a0) * i / steps)) for i in range(steps + 1)]
    inner = [(ox + (r - t / 2) * math.cos(a0 + (a1 - a0) * i / steps),
              oy + (r - t / 2) * math.sin(a0 + (a1 - a0) * i / steps)) for i in range(steps + 1)]
    arc = outer + inner[::-1]
    # The arms beyond the arc, to the edges.
    hx = rect(min(ox, RIGHT if h > 0 else LEFT), CY - t / 2, max(ox, RIGHT if h > 0 else LEFT), CY + t / 2)
    vy = rect(CX - t / 2, min(oy, TOP if v > 0 else BOTTOM), CX + t / 2, max(oy, TOP if v > 0 else BOTTOM))
    return [arc, hx, vy]


def half_disc(side):
    """The round end of a pill: half an ellipse filling the cell's height,
    its flat side on the cell's edge toward the pill (side +1 right)."""
    edge = WIDTH if side > 0 else 0
    rx, ry = WIDTH, (ROW / 2) - 40
    steps = 32
    pts = [(edge - side * rx * math.sin(math.pi * i / steps), CY + ry * math.cos(math.pi * i / steps)) for i in range(steps + 1)]
    return [pts]


def lines(arms, t):
    return [arm(a, t) for a in arms]


GLYPHS = {
    0x2500: lines("lr", LIGHT), 0x2502: lines("ud", LIGHT),
    0x2501: lines("lr", HEAVY), 0x2503: lines("ud", HEAVY),
    0x250C: lines("rd", LIGHT), 0x2510: lines("ld", LIGHT), 0x2514: lines("ru", LIGHT), 0x2518: lines("lu", LIGHT),
    0x251C: lines("udr", LIGHT), 0x2524: lines("udl", LIGHT), 0x252C: lines("lrd", LIGHT), 0x2534: lines("lru", LIGHT),
    0x253C: lines("lrud", LIGHT),
    0x256D: corner(1, -1, LIGHT), 0x256E: corner(-1, -1, LIGHT), 0x2570: corner(1, 1, LIGHT), 0x256F: corner(-1, 1, LIGHT),
    0xE0B6: half_disc(1), 0xE0B4: half_disc(-1),
}


def clockwise(pts):
    area = sum(x0 * y1 - x1 * y0 for (x0, y0), (x1, y1) in zip(pts, pts[1:] + pts[:1]))
    return pts if area < 0 else pts[::-1]


def glyph(contours):
    pen = TTGlyphPen(None)
    for c in contours:
        c = clockwise([(round(x), round(y)) for x, y in c])
        pen.moveTo(c[0])
        for p in c[1:]:
            pen.lineTo(p)
        pen.closePath()
    return pen.glyph()


# NERD_FONTS is the release the icons and JetBrains Mono come from.
NERD_FONTS = "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.5.1/JetBrainsMono.tar.xz"
STYLES = {"regular": "JetBrainsMonoNLNerdFontMono-Regular.ttf", "bold": "JetBrainsMonoNLNerdFontMono-Bold.ttf"}
DEJAVU = {"regular": "DejaVuSansMono.ttf", "bold": "DejaVuSansMono-Bold.ttf"}
# Text: Latin, its punctuation, and the box lines; the other symbols are
# those unagit's code and themes name (icons).
TEXT = [(0x20, 0x7E), (0xA0, 0x17F), (0x2000, 0x206F), (0x2500, 0x257F)]


def icons(repo):
    """Every icon and symbol unagit's code and themes name: a character past
    Latin, in a theme's strings, or in the code as itself or escaped."""
    private = lambda c: ord(c) >= 0x2000
    found = set()

    def strings(v):
        if isinstance(v, str):
            yield v
        elif isinstance(v, dict):
            for x in v.values():
                yield from strings(x)
        elif isinstance(v, list):
            for x in v:
                yield from strings(x)

    for path in repo.glob("internal/**/*.json"):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except ValueError:
            continue
        found |= {ord(c) for s in strings(data) for c in s if private(c)}
    for path in repo.glob("internal/**/*.go"):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        found |= {ord(c) for c in text if private(c)}
        found |= {int(h, 16) for h in re.findall(r"\\u([eEfF][0-9a-fA-F]{3})", text)}
        found |= {int(h, 16) for h in re.findall(r"\\U000([fF][0-9a-fA-F]{4})", text)}
    return found


def sources(given):
    """The Nerd Fonts release, as given or downloaded."""
    if given:
        archive = pathlib.Path(given)
    else:
        archive = pathlib.Path(tempfile.gettempdir()) / "JetBrainsMono.tar.xz"
        if not archive.exists():
            print("downloading", NERD_FONTS)
            urllib.request.urlretrieve(NERD_FONTS, archive)
    out = pathlib.Path(tempfile.mkdtemp())
    with tarfile.open(archive) as tar:
        tar.extractall(out, members=[m for m in tar.getmembers() if m.name in [*STYLES.values(), "OFL.txt"]], filter="data")
    return out


def borrow(font, other, keep):
    """What JetBrains Mono lacks of what unagit writes - the turning
    pipeline marks, the star, the spinner - from DejaVu Sans Mono, brought
    to this font's grid."""
    cmap, theirs = font.getBestCmap(), other.getBestCmap()
    gs = other.getGlyphSet()
    scale = UPM / other["head"].unitsPerEm
    order = font.getGlyphOrder()
    for cp in sorted(keep):
        if cp in cmap or cp not in theirs:
            continue
        src = gs[theirs[cp]]
        # Its parts made one outline: the parts are of the other font.
        outline = DecomposingRecordingPen(gs)
        src.draw(outline)
        pen = TTGlyphPen(None)
        outline.replay(TransformPen(pen, (scale, 0, 0, scale, (WIDTH - src.width * scale) / 2, 0)))
        g = pen.glyph()
        g.recalcBounds(font["glyf"])
        name = f"uni{cp:04X}"
        order.append(name)
        font["glyf"][name] = g
        font["hmtx"][name] = (WIDTH, getattr(g, "xMin", 0))
        for table in font["cmap"].tables:
            if table.isUnicode() and (cp <= 0xFFFF or table.format == 12):
                table.cmap[cp] = name
    font.setGlyphOrder(order)


def rename(font, style):
    name = font["name"]
    name.names = [n for n in name.names if n.nameID not in (1, 2, 3, 4, 6, 16, 17, 21, 22)]
    label = style.capitalize()
    for nid, text in {1: "unagit mono", 2: label, 3: f"unagit mono {label} 1.0", 4: f"unagit mono {label}",
                      6: f"unagitmono-{label}"}.items():
        name.setName(text, nid, 3, 1, 0x409)
        name.setName(text, nid, 1, 0, 0)


def build(src, dejavu, style, keep, out_ttf, out_woff2):
    font = TTFont(src)
    opts = subset.Options()
    opts.layout_features = []
    opts.hinting = False
    opts.name_IDs = ["*"]
    opts.notdef_outline = True
    sub = subset.Subsetter(opts)
    sub.populate(unicodes=keep)
    sub.subset(font)
    glyf, cmap = font["glyf"], font.getBestCmap()
    for cp, contours in GLYPHS.items():
        if cp in cmap:
            g = glyph(contours)
            g.recalcBounds(glyf)
            glyf[cmap[cp]] = g
            font["hmtx"][cmap[cp]] = (WIDTH, g.xMin)
    borrow(font, TTFont(dejavu), keep)
    rename(font, style)
    font.save(out_ttf)
    font.flavor = "woff2"
    font.save(out_woff2)
    print("wrote", out_ttf, out_woff2, f"({len(font.getBestCmap())} characters)")


def main():
    repo = pathlib.Path(__file__).resolve().parents[2]
    src = sources(sys.argv[1] if len(sys.argv) > 1 else None)
    keep = {cp for lo, hi in TEXT for cp in range(lo, hi + 1)} | icons(repo) | set(GLYPHS)
    ttf, woff2 = repo / "website/shots/fonts", repo / "website/public/fonts"
    for d in (ttf, woff2):
        d.mkdir(parents=True, exist_ok=True)
        shutil.copy(src / "OFL.txt", d / "OFL.txt")
    for style, file in STYLES.items():
        dejavu = repo / "website/shots/node_modules/dejavu-fonts-ttf/ttf" / DEJAVU[style]
        build(src / file, dejavu, style, keep, ttf / f"unagit-mono-{style}.ttf", woff2 / f"unagit-mono-{style}.woff2")


if __name__ == "__main__":
    main()
