#!/usr/bin/env python3
"""Make the font the home page's player draws the box lines in: a terminal
draws them itself, edge to edge of the cell, but the player sets them in a
font, whose lines stop short of the row's height - the frames came out
dashed. This font has only those characters, each line running past the
cell's edges, and the round ends of a pill, on the grid of JetBrains Mono
(600 units wide, 1020 above the baseline and 300 below) that the player
sets the rest in.

    pip install fonttools brotli
    python3 website/shots/boxfont.py website/public/fonts/unagit-box.woff2
"""
import math
import sys

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

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


def main():
    out = sys.argv[1]
    names = [".notdef"] + [f"uni{cp:04X}" for cp in GLYPHS]
    fb = FontBuilder(UPM, isTTF=True)
    fb.setupGlyphOrder(names)
    fb.setupCharacterMap({cp: f"uni{cp:04X}" for cp in GLYPHS})
    glyphs = {".notdef": TTGlyphPen(None).glyph()}
    glyphs.update({f"uni{cp:04X}": glyph(c) for cp, c in GLYPHS.items()})
    fb.setupGlyf(glyphs)
    fb.setupHorizontalMetrics({n: (WIDTH, 0) for n in names})
    fb.setupHorizontalHeader(ascent=ASCENT, descent=-DESCENT)
    # Every name a parser may want; one without a PostScript name is
    # dropped by some.
    fb.setupNameTable({"familyName": "unagit box", "styleName": "Regular", "uniqueFontIdentifier": "unagit box 1.0",
                       "fullName": "unagit box Regular", "psName": "unagitbox-Regular", "version": "Version 1.000"})
    fb.setupOS2(sTypoAscender=ASCENT, sTypoDescender=-DESCENT, sTypoLineGap=0,
                usWinAscent=ASCENT, usWinDescent=DESCENT, xAvgCharWidth=WIDTH, fsType=0)
    fb.setupPost(isFixedPitch=1)
    fb.font.flavor = "woff2"
    fb.save(out)
    print("wrote", out)


if __name__ == "__main__":
    main()
