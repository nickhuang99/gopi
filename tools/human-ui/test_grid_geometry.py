#!/usr/bin/env python3
"""A minimal repro for the mask-geometry defect — the smallest case that fails
on the old assumption and passes on the fix.

The defect (2026-10-01): mask boxes were placed as `row * (img_h / rows)`, i.e.
assuming the terminal's cell grid starts at the window's first pixel. A real
terminal draws its own chrome above the grid, so every box landed about one row
too high — on an EMPTY row — while the secret stayed in the published picture.
Containment (check B) could not see it: the paint WAS inside a recorded box.

This test builds the smallest picture that has the property: a synthetic image of
CHROME + rows*cell_h pixels whose ink sits exactly on the grid rows a dump
describes. It then asserts, with no terminal and no gopi involved:

  1. locate_grid() finds the true origin (chrome, not 0);
  2. the old formula (row * img_h/rows) is OFF the target — i.e. this picture
     REPRODUCES the defect rather than merely exercising the new code;
  3. locate_grid() REFUSES a picture whose grid does not match the dump at all.

Run:  python3 tools/human-ui/test_grid_geometry.py     (exit 0 = pass)
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from PIL import Image  # noqa: E402

import assemble as A  # noqa: E402

COLS, ROWS = 120, 40
CELL_W, CELL_H = 17, 34          # the real capture's own cell size, rounded down
CHROME = 36                      # xfce4-terminal's menu bar, measured
BG = (19, 25, 38)
INK = (230, 230, 230)

# The dump: every 4th row carries text, the rest are blank. This is what makes
# "a row with text has ink, a row without text has none" a testable statement.
GRID = {r: ("x" * 30 if r % 4 == 0 else "") for r in range(ROWS)}


def build(chrome=CHROME, mismatch=False):
    im = Image.new("RGB", (COLS * CELL_W, chrome + ROWS * CELL_H), BG)
    px = im.load()
    for r, text in GRID.items():
        if not text:
            continue
        y0 = chrome + r * CELL_H
        for x in range(0, min(len(text) * CELL_W, im.size[0])):
            for y in range(y0 + CELL_H // 4, y0 + 3 * CELL_H // 4):
                px[x, y] = INK
    if mismatch:
        # A picture that cannot be the dump: ONE text row is missing its ink. No
        # origin can explain that, so a locator that answers anyway is guessing —
        # and a guessed geometry is exactly how the secret leaked.
        px = im.load()
        victim = sorted(r for r, t in GRID.items() if t)[-1]
        y0 = chrome + victim * CELL_H
        for y in range(y0, min(y0 + CELL_H, im.size[1])):
            for x in range(0, im.size[0]):
                px[x, y] = BG
    return im


def main():
    bad = []

    im = build()
    got = A.locate_grid(im, GRID, COLS, ROWS)
    if got is None:
        bad.append("locate_grid refused a picture that DOES carry the dump's rows")
    else:
        y0, cell_h = got
        if abs(y0 - CHROME) > 1:
            bad.append("locate_grid found origin %s, expected ~%d (the chrome)" % (y0, CHROME))
        if abs(cell_h - CELL_H) > 0.5:
            bad.append("locate_grid found cell_h %.2f, expected %.2f" % (cell_h, CELL_H))
        print("1. locate_grid -> y0=%s cell_h=%.3f  (chrome=%d cell_h=%d) OK" %
              (y0, cell_h, CHROME, CELL_H))

        # 2. The defect, reproduced: the old assumption is off the target.
        old_h = im.size[1] / float(ROWS)
        target_rows = [r for r, t in GRID.items() if t]
        off = 0
        for r in target_rows:
            old_y = int(r * old_h)
            new_y = int(round(y0 + r * cell_h))
            old_band = any(im.getpixel((5, y)) != BG for y in range(old_y, min(old_y + int(old_h), im.size[1])))
            new_band = any(im.getpixel((5, y)) != BG for y in range(new_y, min(new_y + int(cell_h), im.size[1])))
            if old_band != new_band or not old_band:
                off += 1
        if off == 0:
            bad.append("the picture does NOT reproduce the defect — the old formula hit "
                       "every target row, so this test proves nothing")
        else:
            print("2. old formula (row*img_h/rows) misses %d/%d target rows, the measured "
                  "geometry hits them — defect reproduced OK" % (off, len(target_rows)))

    # 3. Refusal: a picture that cannot be the dump must be REFUSED, not guessed.
    wrong = A.locate_grid(build(mismatch=True), GRID, COLS, ROWS)
    blank = A.locate_grid(Image.new("RGB", (COLS * CELL_W, CHROME + ROWS * CELL_H), BG),
                          GRID, COLS, ROWS)
    if wrong is not None:
        bad.append("locate_grid ACCEPTED a picture whose ink is shifted off the dump's rows — "
                   "a guessed geometry is how the secret leaked")
    if blank is not None:
        bad.append("locate_grid ACCEPTED a blank picture")
    print("3. locate_grid refused the mismatched picture and the blank one OK")

    if bad:
        for b in bad:
            print("FAIL: " + b, file=sys.stderr)
        return 1
    print("PASS: the grid origin is measured, the old assumption is provably off-target, "
          "and an unverifiable picture is refused")
    return 0


if __name__ == "__main__":
    sys.exit(main())
