#!/usr/bin/env python3
"""make_sample.py — the fixed byte-string two terminals are made to render.

WHY A SYNTHETIC SAMPLE, AND WHY IT IS THE HONEST CHOICE. The old/new comparison
must vary ONE thing: the terminal. If each leg ran `gopi chat` on its own, the
model's two answers would differ in wording and the picture would no longer
isolate the terminal — a reader could not tell a colour change from a different
answer. So the sample is generated once, hashed, and `cat`-ed into both
terminals: identical bytes, identical escapes, identical layout.

WHAT IT CARRIES: the 16 ANSI colours, the 256-colour cube entries gopi's own
interface uses (178/230 gold, 220/39/208/45 accents), CJK text at several widths,
and a box/block row — the four things whose rendering differs between xterm's
built-in 16-colour table and a modern VTE terminal reading the user's palette.

ROWS ARE SEPARATED BY A BLANK ROW, on purpose. Block glyphs (█) fill their cell
vertically, so two adjacent swatch rows touch and no scanner can tell where the
cell boundary is (measured: the ink of the whole grid merges into one band). The
blank rows are the separator that makes each sample row a distinct band of ink,
which is what lets the measurement locate a cell in a picture that may or may not
have a menu bar or decoration above it.

MARKERS. Alongside the bytes it writes (row, first_col, last_col, label) for every
colour swatch, so the comparison script can MEASURE what colour a cell actually
came out as instead of describing it. One generator, one layout — the sample and
the markers cannot drift apart. Every marker row is EVEN (0-based); odd rows are
blank separators.
"""
import json
import sys
import os
import re
import unicodedata
import unicodedata

SGR = re.compile(r"\033\[[0-9;]*m")

# Wide (double-cell) ranges that matter here: CJK, fullwidth forms, and the box /
# block / geometric shapes a terminal draws double-width only when East Asian
# Ambiguous — the sample keeps to characters whose width is unambiguous.
def char_width(ch):
    if unicodedata.east_asian_width(ch) in ("W", "F"):
        return 2
    return 1


def ink_cells(row_text):
    """The width of a row's INKED cells: its visible text with trailing spaces
    trimmed, counting a double-width glyph as two cells. Trailing spaces matter —
    a row that ends in a space inks nothing there, and a span that counted them
    would overstate the cell count by up to the number of trailing spaces."""
    vis = SGR.sub("", row_text).rstrip()
    return sum(char_width(c) for c in vis)

COLS = 120
ROWS = 13
SWATCH = "█" * 3
CUBE_SWATCH = "█" * 4
ANSI = list(range(16))
CUBE = [178, 230, 220, 39, 208, 45, 250]
# The plain-text row: exactly 51 monospace characters, starting at column 0, so its
# first and last inked pixels give the picture's x origin and its cell width. Kept
# in ONE place because the measuring script divides by its length.
REF_LINE = "no colour on this row: the glyphs are the reference"
REF_LEN = 51


def sgr_cube(n):
    """256-colour SGR for index n (the 16..255 cube and the grey ramp)."""
    return "\033[38;5;%dm" % n


def build():
    markers = []
    rows = {}
    # row 0 — a title in the terminal's BOLD colour
    rows[0] = "\033[1mgopi — the SAME bytes, two terminals\033[0m"
    # row 2 — the 16 ANSI colours, 3 cells each
    buf, col = [], 0
    prefix = "16 ANSI  "
    buf.append(prefix)
    col = len(prefix)
    for n in ANSI:
        # FOREGROUND SGR (30-37 / 90-97), not background: the swatch is the block
        # glyph █, which paints in the FOREGROUND colour — with a background SGR the
        # block comes out in the terminal's default foreground in BOTH terminals,
        # and the row would measure "identical" while carrying no colour at all
        # (measured: every ANSI swatch read #b7b7b7 on VTE and #000000 on xterm,
        # i.e. the two terminals' DEFAULT FOREGROUNDS, not their palettes).
        seq = "\033[3%dm" % n if n < 8 else "\033[9%dm" % (n - 8)
        buf.append("%s%s\033[0m " % (seq, SWATCH))
        markers.append({"row": 2, "from": col, "to": col + 2, "label": "ansi%d" % n})
        col += 4
    rows[2] = "".join(buf)
    # row 4 — the 256-colour cube entries gopi's UI uses
    buf, col = [], 0
    prefix = "256 cube  "
    buf.append(prefix)
    col = len(prefix)
    for n in CUBE:
        buf.append("%s%s\033[0m " % (sgr_cube(n), CUBE_SWATCH))
        markers.append({"row": 4, "from": col, "to": col + 3, "label": "cube%d" % n})
        col += 5
    rows[4] = "".join(buf)
    # row 6 — CJK, the half a terminal's font has to cover
    rows[6] = "中文：账本 / 工作树 / 任务队列 / 显示宽度"
    # row 8 — box drawing + the block ramp a progress bar is made of
    rows[8] = "┌──┬──┐ ▏▎▍▌▋▊▉█ └──┴──┘ é ①②③"
    # row 10 — plain text, the glyph reference the cell width is measured on
    assert len(REF_LINE) == REF_LEN, "REF_LINE length must equal REF_LEN"
    rows[10] = REF_LINE
    # row 12 — empty, and the BACKGROUND probe: its colour is the terminal's own
    # background, measured far from the blinking cursor (which sits on row 0 of the
    # first free line, i.e. right after this sample) and far from any glyph
    # Row 12 is left empty on purpose and carries NO marker: it keeps the block
    # cursor (which sits on the first free line, i.e. right here) away from the ink
    # bands, and the terminal's BACKGROUND needs no marker at all — the most
    # frequent colour of the whole picture IS the background, which is what
    # compare_terms.py measures.
    rows[12] = ""

    # Hide the cursor (DECTCEM off) for the whole sample: a block cursor paints a
    # filled cell on the first free line, i.e. an EXTRA ink band right below the
    # sample, and it blinks — two reasons it must not be in a measured picture.
    # The terminal is killed after the capture, so nothing is left hidden.
    text = "\033[?25l"
    # NO trailing newline: a last "\r\n" puts the cursor on a row BELOW the screen
    # and the terminal scrolls the top row away — measured, the sample's title row
    # scrolled off and every band shifted up by one, which the width-identification
    # below then had to fail on. The sample ends on its last row.
    text += "\r\n".join(rows.get(r, "") for r in range(ROWS))
    return text, markers, rows


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: make_sample.py <out.ansi> <out.json>")
    text, markers, rows = build()
    with open(sys.argv[1], "w", encoding="utf-8") as fh:
        fh.write(text)
    with open(sys.argv[2], "w", encoding="utf-8") as fh:
        # INK ROWS are the rows that actually draw glyphs — one ink band each in a
        # capture, and the only rows a scanner can locate. Every marker lives on one
        # of them (a marker on a blank row would name a band that does not exist).
        ink_rows = sorted({0, 2, 4, 6, 8, 10})
        for m in markers:
            assert m["row"] in ink_rows, "marker on a blank row: %r" % m
        # INK CELLS: how many cells each ink row actually paints. The scanner uses
        # this to IDENTIFY a band — a terminal's own menu bar and its block cursor
        # are extra bands in the picture, and matching each band to the row whose
        # width it has is what keeps them from being read as sample rows (measured:
        # taking "the last N bands" put the cursor where the glyph-reference row
        # was, and every swatch box landed one row off).
        cells = {str(r): ink_cells(rows[r]) for r in ink_rows}
        json.dump({"cols": COLS, "rows": ROWS, "ref_len": REF_LEN,
                   "ink_rows": ink_rows, "ink_cells": cells, "markers": markers},
                  fh, ensure_ascii=False, indent=2)
    print("make_sample: %d rows (%d with ink, blank separators between), %d markers, %d bytes, "
          "ref_len=%d" % (ROWS, len(ink_rows), len(markers), os.path.getsize(sys.argv[1]), REF_LEN))


if __name__ == "__main__":
    main()
