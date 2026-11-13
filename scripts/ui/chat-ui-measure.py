#!/usr/bin/env python3
"""chat-ui-measure.py — measure a captured terminal band, per text line.

Reads a PNG produced by `gopi capture` and reports, for each detected text band:
the row background, the brightest text pixel, the median text pixel, the WCAG
contrast of each against the background, and the CORE PIXEL FRACTION (text
pixels whose contrast against the bg is >= 90% of the band's maximum).

Why core fraction: contrast is computed from the BRIGHTEST pixel, which for an
anti-aliased glyph is the stroke centre. A thin or dim face has few such pixels,
so its strokes read as "faint" even when the maximum contrast passes WCAG. The
fraction is the measurable half of that complaint.

Output is a table plus a JSON blob (--json).
"""
import argparse
import collections
import json
import sys

from PIL import Image

# A pixel counts as TEXT when it differs from the band's modal background by more
# than TEXT_DELTA on some channel. 25/255 is above the terminal's own subtle
# background gradient and below any real glyph edge.
TEXT_DELTA = 25
# A row counts as TEXT when it holds more than MIN_TEXT_PX such pixels.
MIN_TEXT_PX = 3
# A text pixel is a CORE pixel when its contrast is at least this fraction of the
# band's maximum contrast.
CORE_FRACTION = 0.9
# Inside one band, a run of blank columns this wide or wider separates two
# FIELDS (the status row's label fields and its live hint). The status row's own
# separator is three cells wide, so the gap must be wider than that or the run
# around a separator would be reported as a field of its own.
FIELD_GAP = 6


def lum(c):
    def f(v):
        v = v / 255.0
        return v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4
    r, g, b = (f(x) for x in c)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def ratio(a, b):
    la, lb = lum(a), lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)


def interior(im, pad=4):
    """Crop to the TERMINAL's text area inside the captured window.

    A terminal window is a text interior inside a frame, with a scrollbar and a
    title bar. Those are CONSTANT down every row, so they read as a full-height
    "text band" and drown the measurement. The interior is found by the terminal
    background: the most common colour among the image's non-black pixels (the
    Xvfb root is black). A capture with no such region is returned unchanged.
    """
    w, h = im.size
    px = im.load()
    counts = collections.Counter(
        c for c in im.getdata() if c != (0, 0, 0))
    if not counts:
        return im
    bg = counts.most_common(1)[0][0]
    def is_bg(c):
        return max(abs(c[i] - bg[i]) for i in range(3)) <= 6
    minx, miny, maxx, maxy = w, h, -1, -1
    for y in range(h):
        for x in range(w):
            if is_bg(px[x, y]):
                if x < minx: minx = x
                if x > maxx: maxx = x
                if y < miny: miny = y
                if y > maxy: maxy = y
    if maxx < 0:
        return im
    box = (max(0, minx + pad), max(0, miny + pad),
           min(w, maxx + 1 - pad), min(h, maxy + 1 - pad))
    if box[2] <= box[0] or box[3] <= box[1]:
        return im
    return im.crop(box)


def bands(im):
    """Return [(y0, y1, bg)] — contiguous runs of rows holding text."""
    w, h = im.size
    px = im.load()
    out = []
    cur = None
    for y in range(h):
        cnt = collections.Counter(px[x, y] for x in range(w))
        bg, _ = cnt.most_common(1)[0]
        n = 0
        for x in range(w):
            c = px[x, y]
            if max(abs(c[i] - bg[i]) for i in range(3)) > TEXT_DELTA:
                n += 1
        if n > MIN_TEXT_PX:
            if cur is None:
                cur = [y, y, bg]
            cur[1] = y
        else:
            if cur is not None:
                out.append(tuple(cur))
                cur = None
    if cur is not None:
        out.append(tuple(cur))
    return out


def band_fields(im, y0, y1, bg, pad=1):
    """Split one band into FIELDS by the blank columns between them.

    A status row is several space-separated fields on one line, and the fix has
    to be reported PER FIELD (the label, each separator, the tool digest, the
    live hint), because they can carry different emphasis. Fields are runs of
    columns holding text, split where FIELD_GAP or more blank columns occur.
    """
    w = im.size[0]
    px = im.load()
    cols = []
    for x in range(w):
        n = 0
        for y in range(y0, y1 + 1):
            c = px[x, y]
            if max(abs(c[i] - bg[i]) for i in range(3)) > TEXT_DELTA:
                n += 1
        cols.append(n)
    out = []
    start = None
    gap = 0
    for x, n in enumerate(cols):
        if n > 0:
            if start is None:
                start = x
            gap = 0
            continue
        if start is None:
            continue
        gap += 1
        if gap >= FIELD_GAP:
            out.append((max(0, start - pad), x - gap + pad))
            start = None
            gap = 0
    if start is not None:
        out.append((max(0, start - pad), w - 1))
    return out


def measure(im, y0, y1, bg, x0=None, x1=None):
    w, h = im.size
    px = im.load()
    if x0 is None:
        x0 = 0
    if x1 is None:
        x1 = w - 1
    x1 = min(x1, w - 1)
    text = []
    for y in range(y0, y1 + 1):
        for x in range(x0, x1 + 1):
            c = px[x, y]
            if max(abs(c[i] - bg[i]) for i in range(3)) > TEXT_DELTA:
                text.append(c)
    if not text:
        return None
    br = max(text, key=lum)
    sec = sorted(text, key=lum)[len(text) // 2]
    maxr = ratio(br, bg)
    core = [c for c in text if ratio(c, bg) >= CORE_FRACTION * maxr]
    rows = sorted({c for c in text}, key=lum)
    return {
        "rows": [y0, y1],
        "bg": list(bg),
        "text_px": len(text),
        "brightest": list(br),
        "brightest_ratio": round(maxr, 2),
        "median": list(sec),
        "median_ratio": round(ratio(sec, bg), 2),
        "core_px": len(core),
        "core_fraction": round(len(core) / len(text), 3),
        "distinct": len(rows),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("png")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--max-bands", type=int, default=0)
    ap.add_argument("--fields", action="store_true",
                    help="report each band's FIELDS separately (the status row is "
                         "several fields on one line, each possibly emphasised "
                         "differently)")
    ap.add_argument("--no-crop", action="store_true",
                    help="measure the whole image instead of the window's text "
                         "interior (the default crop drops the terminal's frame "
                         "and scrollbar, which are constant per row and would "
                         "otherwise count as text)")
    args = ap.parse_args()

    im = Image.open(args.png).convert("RGB")
    if not args.no_crop:
        im = interior(im)
    found = bands(im)
    if args.max_bands:
        found = found[-args.max_bands:]
    results = []
    for y0, y1, bg in found:
        m = measure(im, y0, y1, bg)
        if m:
            results.append(m)
    print(f"# {args.png}  {im.size[0]}x{im.size[1]}  bands={len(results)}")
    print(f"{'rows':>10} {'bg':>16} {'brightest':>16} {'ratio':>6} {'median':>16} {'med_r':>6} {'core%':>6}")
    for m in results:
        print("%10s %16s %16s %6.2f %16s %6.2f %6.1f" % (
            "%d-%d" % tuple(m["rows"]),
            str(tuple(m["bg"])),
            str(tuple(m["brightest"])),
            m["brightest_ratio"],
            str(tuple(m["median"])),
            m["median_ratio"],
            100 * m["core_fraction"],
        ))
    if args.fields:
        print("")
        print("# per-field (each run of text columns inside a band, split on "
              f"{FIELD_GAP}+ blank columns)")
        print(f"{'rows':>10} {'x0':>5} {'x1':>5} {'bg':>16} {'brightest':>16} {'ratio':>6} {'median':>16} {'med_r':>6} {'core%':>6}")
        for m in results:
            y0, y1 = m["rows"]
            bg = tuple(m["bg"])
            for x0, x1 in band_fields(im, y0, y1, bg):
                f = measure(im, y0, y1, bg, x0, x1)
                if f is None:
                    continue
                print("%10s %5d %5d %16s %16s %6.2f %16s %6.2f %6.1f" % (
                    "%d-%d" % (y0, y1), x0, x1,
                    str(tuple(f["bg"])), str(tuple(f["brightest"])),
                    f["brightest_ratio"], str(tuple(f["median"])),
                    f["median_ratio"], 100 * f["core_fraction"]))
    if args.json:
        print(json.dumps(results, indent=1))


if __name__ == "__main__":
    sys.exit(main())
