#!/usr/bin/env python3
"""compare_terms.py — the xterm-vs-real-terminal comparison, as a MEASUREMENT.

WHAT IT COMPARES. Two (or more) PNGs of the SAME sample bytes — one per terminal
— at the same 120-column geometry. For every colour swatch the sample declares
(a row/col marker from make_sample.py) it reads the pixels of that cell and
reports the COLOUR that came out. That is the whole claim: not "it looks
better", but "cell (2, 13..16) is #b7b7b7 in the user's terminal and #aaaaaa in
xterm" — a table a reader can check, and re-derive by re-running this script.

THE DISTINCT-COLOUR COUNT is the second figure: across one frame, how many
distinct colours the two terminals actually produce. It is the crude but honest
proxy for "16-colour table vs the real palette", and it is printed, not asserted.

THE SIDE-BY-SIDE IMAGE stacks the terminals vertically at their own resolution,
labels each by the term name given on the command line, and draws a thin frame so
the boundary between two similar-looking terminals is unambiguous.
"""
import argparse
import json
import os
import sys

try:
    from PIL import Image, ImageDraw
except ImportError:  # pragma: no cover
    sys.exit("compare_terms: python3-PIL missing")

# The label strip above each terminal in the side-by-side image.
LABEL_H = 30
SEP = 10

# The sample's own geometry is DERIVED FROM THE PICTURE, never from the window
# size: the real terminal on this host draws a MENU BAR (a user-visible band) above
# the cell grid, and a window may also have no decoration at all — so "row R of the
# window" is not "row R of the sample". Bands of ink are detected instead, and the
# sample's rows are taken from the LAST N of them (terminal chrome, if any, sits
# above the grid).
# The sample's own row order and its glyph-reference length come FROM THE SAMPLE
# JSON (make_sample.py is their one definition) — never re-typed here.
INK_DELTA = 60        # manhattan distance from the background that counts as ink
# A terminal cell is between these widths in pixels, for any font/hi-dpi pairing
# this pipeline uses (measured: 10.13 at the user's own size, 16.13 at the size
# the legibility floor forces). Outside the range the pair is not a candidate for
# "this band is that row".
MIN_CELL_W = 6.0
MAX_CELL_W = 34.0
WIDTH_TOL = 0.05
# How far a detected band's width may sit from the width its own cell count
# implies, before the picture is refused rather than measured. 5% is comfortably
# outside the antialiasing/rounding noise (measured across both terminals: worst
# case 1.2%) and comfortably inside the gap between two neighbouring rows' implied
# widths (the sample's rows are 30..72 cells wide, so a one-row mix-up is >4% on
# the narrow rows and 10-40% on the rest).
# The right-edge strip reserved for a terminal's OWN scrollbar. It spans every row,
# so scanning the full width merges every band into one (measured: the VTE leg came
# back as a single 0..H-1 band, and the swatch table would have been fiction). The
# strip is derived from the picture's width, and the sample's own text never
# reaches it (120 columns of the reference row start at column 0).
SCROLLBAR_GUARD_DIV = 32
SCROLLBAR_GUARD_MIN = 24


def _count(im):
    from collections import Counter
    return Counter(im.getdata())


def scan_width(w):
    """The width the scanner reads: everything left of the scrollbar strip."""
    return w - max(SCROLLBAR_GUARD_MIN, w // SCROLLBAR_GUARD_DIV)


def ink_mask(im, bg):
    """Rows that contain ink, measured against the picture's own background."""
    w, h = im.size
    x_max = scan_width(w)
    px = im.load()
    out = []
    for y in range(h):
        hit = False
        for x in range(0, x_max, 2):
            p = px[x, y]
            if abs(p[0] - bg[0]) + abs(p[1] - bg[1]) + abs(p[2] - bg[2]) > INK_DELTA:
                hit = True
                break
        out.append(hit)
    return out


def bands(mask):
    runs, s = [], None
    for y, v in enumerate(mask):
        if v and s is None:
            s = y
        if not v and s is not None:
            runs.append((s, y - 1))
            s = None
    if s is not None:
        runs.append((s, len(mask) - 1))
    return runs


def band_extent(im, bg, y0, y1):
    """First and last inked column of a row band, inside the scan width."""
    w = scan_width(im.size[0])
    px = im.load()
    cols = [x for x in range(w)
            if any(abs(px[x, y][0] - bg[0]) + abs(px[x, y][1] - bg[1]) + abs(px[x, y][2] - bg[2]) > INK_DELTA
                   for y in range(y0, y1 + 1))]
    return (cols[0], cols[-1]) if cols else (0, 0)


def grid_geometry(im, bg, ink_rows, ink_cells, ref_row):
    """(x0, cell_w, {sample_row: (y0,y1)}) for one terminal picture.

    The picture is not the sample and only the sample: a real terminal draws a MENU
    BAR above the grid (measured: a 35px band of ink above the first sample row on
    this host), or a scrollbar down the side, or nothing at all. So the sample's
    rows are taken as the LAST len(ink_rows) ink bands — anything the terminal draws
    of its own sits ABOVE the grid — and then VERIFIED against the sample: every
    band's width must equal that row's own cell count times ONE cell width, within
    5%. A picture that does not verify is REFUSED, never guessed at: a silently
    misplaced cell would turn the swatch table into fiction, which is worse than a
    failed comparison.

    The cell width is taken from the glyph-reference row (row 10 of the sample: 51
    monospace characters starting at column 0), and the row's own first inked column
    is the grid's x origin. The verification then either confirms that origin or the
    picture is refused.
    """
    b = bands(ink_mask(im, bg))
    if len(b) < len(ink_rows):
        return None
    b = b[-len(ink_rows):]
    spans = {}
    for band in b:
        x0, x1 = band_extent(im, bg, *band)
        spans[band] = (x0, x1, x1 - x0 + 1)
    ref_band = b[ink_rows.index(ref_row)]
    x0, _, ref_span = spans[ref_band]
    cell_w = ref_span / float(ink_cells[str(ref_row)])
    if not (MIN_CELL_W <= cell_w <= MAX_CELL_W):
        return None
    matched = {}
    for r, band in zip(ink_rows, b):
        want = ink_cells[str(r)] * cell_w
        got = spans[band][2]
        if abs(got - want) > WIDTH_TOL * want:
            return None
        matched[r] = band
    return x0, cell_w, matched


def cell_colour(im, geom, band, c0, c1):
    """The colour a swatch came out as: the most frequent pixel of its cell box.

    The MODE, not a corner pixel: a glyph is antialiased at its edges, so a single
    pixel can land on a blend. The most frequent colour of the cell's own pixels is
    what a human sees when they look at that block, and for a 3-4 cell swatch of a
    solid block glyph it is the swatch colour itself. The box is inset by one pixel
    and by a quarter of the band vertically, so a neighbouring cell's edge cannot
    vote.
    """
    x0, cell_w = geom[0], geom[1]
    y0, y1 = band
    w, h = im.size
    xa = max(0, int(round(x0 + c0 * cell_w)) + 1)
    xb = min(w, max(xa + 1, int(round(x0 + (c1 + 1) * cell_w)) - 1))
    ya = max(0, y0 + (y1 - y0 + 1) // 4)
    yb = min(h, max(ya + 1, y1 - (y1 - y0 + 1) // 4))
    px = im.load()
    hist = {}
    for y in range(ya, yb):
        for x in range(xa, xb):
            p = px[x, y]
            hist[p] = hist.get(p, 0) + 1
    if not hist:
        return (0, 0, 0), [xa, ya, xb, yb]
    return max(hist.items(), key=lambda kv: kv[1])[0], [xa, ya, xb, yb]


def hexs(p):
    return "#%02x%02x%02x" % p


def main():
    ap = argparse.ArgumentParser(add_help=True)
    ap.add_argument("--sample-json", required=True, help="marker table from make_sample.py")
    ap.add_argument("--out", required=True)
    ap.add_argument("--term", action="append", required=True,
                    help="NAME=PATH (repeatable, in display order: oldest first)")
    args = ap.parse_args()

    with open(args.sample_json, encoding="utf-8") as fh:
        sample = json.load(fh)
    cols, rows, marks = sample["cols"], sample["rows"], sample["markers"]
    ink_rows = sample["ink_rows"]           # the sample's ink rows, in screen order
    ink_cells = sample["ink_cells"]         # each of them: how many cells it inks
    ref_len = sample["ref_len"]             # the glyph-reference row's cell count
    ref_row = max(ink_rows)                 # that row (starts at column 0)

    terms = []
    for spec in args.term:
        if "=" not in spec:
            sys.exit("compare_terms: --term wants NAME=PATH, got %r" % spec)
        name, path = spec.split("=", 1)
        if not os.path.exists(path):
            sys.exit("compare_terms: missing %s" % path)
        terms.append((name, path))

    report = {"cols": cols, "rows": rows, "sample_markers": len(marks), "terminals": {}}
    images = []
    for name, path in terms:
        im = Image.open(path).convert("RGB")
        bg = _count(im).most_common(1)[0][0]
        geom = grid_geometry(im, bg, ink_rows, ink_cells, ref_row)
        if geom is None:
            sys.exit("compare_terms: %s: could not match the sample's %d ink rows to the picture's "
                     "bands by their CELL WIDTHS - the picture does not carry the sample"
                     % (path, len(ink_rows)))
        gx0, cell_w, bnds = geom
        entry = {"path": os.path.basename(path), "size": list(im.size), "background": hexs(bg),
                 "distinct_colours": len(set(im.getdata())), "swatches": {},
                 "geometry": {"x0": round(gx0, 2), "cell_w": round(cell_w, 3),
                              "row_bands": {str(r): [bnds[r][0], bnds[r][1]] for r in ink_rows},
                              "ink_rows": ink_rows, "ink_cells": ink_cells}}
        for m in marks:
            col, box = cell_colour(im, geom, bnds[m["row"]], m["from"], m["to"])
            entry["swatches"][m["label"]] = {"hex": hexs(col), "rgb": list(col), "box": box,
                                             "sample_row": m["row"]}
        report["terminals"][name] = entry
        images.append((name, im))

    # Per-swatch verdicts between the FIRST term (the reference) and each other.
    ref_name = terms[0][0]
    ref = report["terminals"][ref_name]["swatches"]
    report["deltas"] = {}
    for name, _ in terms[1:]:
        d = {}
        for label, sw in report["terminals"][name]["swatches"].items():
            r = ref.get(label)
            if not r:
                continue
            dist = sum(abs(a - b) for a, b in zip(r["rgb"], sw["rgb"]))
            d[label] = {"reference": r["hex"], "other": sw["hex"], "manhattan": dist,
                        "same": dist == 0}
        report["deltas"][name] = d
        same = sum(1 for v in d.values() if v["same"])
        report["terminals"][name]["swatches_matching_reference"] = same
        report["terminals"][name]["swatches_total"] = len(d)

    # The side-by-side canvas: every terminal, its own resolution, labelled.
    w = max(im.size[0] for _, im in images)
    h = sum(LABEL_H + im.size[1] + SEP for _, im in images)
    canvas = Image.new("RGB", (w, h), (245, 245, 245))
    draw = ImageDraw.Draw(canvas)
    y = 0
    n = report["terminals"]
    for name, im in images:
        t = n[name]
        # ASCII labels only: PIL's default bitmap font is latin-1, so an em dash
        # or any CJK glyph here raises instead of drawing (measured).
        draw.text((8, y + 8), "%s - %dx%d, %d distinct colours, %d/%d swatches match %s" %
                  (name, t["size"][0], t["size"][1], t["distinct_colours"],
                   t.get("swatches_matching_reference", len(t["swatches"])), len(t["swatches"]), ref_name),
                  fill=(20, 20, 20))
        y += LABEL_H
        canvas.paste(im, (0, y))
        y += im.size[1] + SEP
    os.makedirs(args.out, exist_ok=True)
    canvas_path = os.path.join(args.out, "terminals-compare.png")
    canvas.save(canvas_path)
    with open(os.path.join(args.out, "terminals-compare.json"), "w", encoding="utf-8") as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2, sort_keys=True)

    print(json.dumps({k: {"size": v["size"], "distinct_colours": v["distinct_colours"],
                          "matches": v.get("swatches_matching_reference")}
                      for k, v in report["terminals"].items()}, indent=2, sort_keys=True))
    print("compare_terms: wrote %s" % canvas_path)
    for name, d in report["deltas"].items():
        diff = [(k, v["reference"], v["other"]) for k, v in d.items() if not v["same"]]
        print("compare_terms: %s vs %s — %d swatches differ%s" %
              (ref_name, name, len(diff),
               ("" if not diff else " (first: %s %s→%s)" % diff[0])))


if __name__ == "__main__":
    main()
