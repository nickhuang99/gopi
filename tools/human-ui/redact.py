#!/usr/bin/env python3
"""redact.py — the CLEAN twin of one capture: a white box over every occurrence
of an identifying run, with the substitute drawn as FILLED CELLS.

WHY A BOX AND NOT A TEXT REPLACEMENT. The evidence here is a screenshot: the
identifiers are drawn glyphs, so `sed` cannot touch them and the picture itself
is the record. This tool therefore does the narrowest thing available — cover
exactly the rectangle a secret occupies, and draw a stand-in inside it.

WHY THE STAND-IN IS FILLED CELLS AND NEVER A TYPEFACE GLYPH. The first version of
this file drew a run of '█' with `ImageFont.truetype(...).text()`. That is the
wrong instrument, and it was caught by measurement rather than review: a
full-width '█' in NotoSansCJK advances ~1.65x the terminal's own cell (a 19px
cell, a ~31px glyph), so a 36-character run overshot its 685px span to ~1116px
and painted over the remainder of the line. The result was a mask WIDER than the
fact it hid — and, because the overrun fell outside the recorded rectangle, the
containment check reported it as change outside every box. Filled cells cannot
overshoot: the per-cell width is the arithmetic `w // len(text)`, exact by
construction, and independent of which fonts a machine happens to have.

WHY THE FILL IS SAMPLED, NOT ASSUMED WHITE. A white rectangle on a dark interface
is the brightest thing on screen and draws the eye straight to the line that was
redacted — the opposite of the intent. 'auto' samples the box's own neighbourhood,
so the substitution is invisible AS a substitution: the reader sees that a fact is
missing, not which glyphs carried it.

USAGE
  redact.py --in raw.png --out clean.png [--box X,Y,W,H --text "████"]... \
            [--band Y0,Y1] [--bg auto|R,G,B]

`--band` masks a full-width row band, for the one kind of capture that has no
cell grid to locate a narrower box in (a browser render composes its own layout).
"""
import argparse
import sys

try:
    from PIL import Image, ImageDraw
except ImportError:  # pragma: no cover
    sys.exit("redact: python3-PIL missing")

# Sum of channels above which the neighbourhood counts as light, so the mask ink
# goes dark. 255*3/2 — the midpoint of the 0..765 range, i.e. the same threshold
# a reader's eye uses when deciding whether a line is on white or on black.
INK_THRESHOLD = 380
# Columns left unpainted between two adjacent mask cells, so the substitute reads
# as separate cells rather than as one solid bar of unknown length.
CELL_GAP = 2


def resolve_bg(im, box, bg_arg):
    """The colour to paint a mask with, sampled from above the box by default."""
    if bg_arg != "auto":
        try:
            return tuple(int(v) for v in bg_arg.split(","))
        except ValueError:
            sys.exit("redact: bad --bg %r (want auto or R,G,B)" % bg_arg)
    from collections import Counter
    x, y, w, h = box
    samples = Counter()
    for yy in range(max(0, y - 6), max(0, y - 2)):
        for xx in range(0, im.size[0], 9):
            samples[im.getpixel((xx, yy))] += 1
    if not samples:
        return (255, 255, 255)
    return samples.most_common(1)[0][0]


def paint_cells(dr, box, text, bg):
    """Draw the substitute as len(text) filled cells across exactly the box."""
    x, y, w, h = box
    ink = (0, 0, 0) if sum(bg) > INK_THRESHOLD else (255, 255, 255)
    n = max(1, len(text))
    cw = max(1, w // n)
    for i in range(n):
        left = x + i * cw
        right = min(x + w - 1, left + cw - 1 - CELL_GAP)
        if right <= left:
            continue
        dr.rectangle([left, y + 1, right, y + h - 2], fill=ink)


def main():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--in", dest="inp", required=True)
    p.add_argument("--out", dest="outp", required=True)
    p.add_argument("--box", action="append", default=[],
                   help="X,Y,W,H in the INPUT's pixels, grown by the caller with the same "
                        "margin it records (repeatable, paired with --text)")
    p.add_argument("--text", action="append", default=[], help="substitute for the paired --box")
    p.add_argument("--band", default="", help="Y0,Y1 — mask this full-width row band")
    p.add_argument("--bg", default="auto", help="fill: 'auto' samples the box's surround, or R,G,B")
    args = p.parse_args()

    if len(args.box) != len(args.text):
        sys.exit("redact: --box and --text must be given the same number of times (%d vs %d)"
                 % (len(args.box), len(args.text)))
    im = Image.open(args.inp).convert("RGB")
    dr = ImageDraw.Draw(im)
    masked = 0

    if args.band:
        try:
            y0, y1 = (int(v) for v in args.band.split(","))
        except ValueError:
            sys.exit("redact: bad --band %r (want Y0,Y1)" % args.band)
        if not 0 <= y0 < y1 <= im.size[1]:
            sys.exit("redact: band %s falls outside the image %s" % (args.band, im.size))
        dr.rectangle([0, y0, im.size[0] - 1, y1 - 1], fill=resolve_bg(im, [0, y0, im.size[0], y1 - y0], args.bg))
        masked += 1

    for spec, text in zip(args.box, args.text):
        try:
            box = [int(v) for v in spec.split(",")]
        except ValueError:
            sys.exit("redact: bad --box %r (want X,Y,W,H)" % spec)
        x, y, w, h = box
        if x < 0 or y < 0 or x + w > im.size[0] or y + h > im.size[1]:
            # Refused, not clipped: a box that runs off the image means the caller
            # derived it from a geometry that does not match this picture, and a
            # clipped mask would hide the mismatch while hiding less than intended.
            sys.exit("redact: box %s falls outside the image %s" % (spec, im.size))
        bg = resolve_bg(im, box, args.bg)
        dr.rectangle([x, y, x + w - 1, y + h - 1], fill=bg)
        paint_cells(dr, box, text, bg)
        masked += 1

    im.save(args.outp)
    print("redact: %s -> %s  regions=%d  %dx%d" % (args.inp.rsplit("/", 1)[-1], args.outp.rsplit("/", 1)[-1],
                                                   masked, im.size[0], im.size[1]))


if __name__ == "__main__":
    main()
