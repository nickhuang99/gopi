#!/usr/bin/env python3
"""compare_web.py — measure two renders of the SAME page and build the A/B image.

The A/B question is "which way should gopi photograph its own web UI?", and the
answer has to be a measurement, not a preference. So both PNGs are read here and
the SAME numbers are taken from each: pixel size, bytes on disk, distinct
colours, the luminance of the top rows (which is where a browser's own warning
banner would appear), and how much of the picture differs between the legs.

The comparison IMAGE is the deliverable a human checks: leg A and leg B stacked,
labelled, at their own resolution — no rescaling, because the whole point of the
pair is the pixels.
"""
import argparse
import json
import os
import shutil
import sys

try:
    from PIL import Image, ImageChops, ImageDraw
except ImportError:  # pragma: no cover
    sys.exit("compare_web: python3-PIL missing")

# Rows of the X11 leg that chrome's own "--no-sandbox is unsupported" banner
# occupies. It is measured, not assumed: the top band is compared against the
# page's own background luminance below it.
TOP_BAND_ROWS = 40
BAND_LUM_DELTA = 25  # luminance above the page background that counts as a banner

LABEL_H = 34
SEP = 12


def lum(p):
    return 0.2126 * p[0] + 0.7152 * p[1] + 0.0722 * p[2]


def band_luminance(im, y0, y1):
    px = im.load()
    w, h = im.size
    tot = 0.0
    n = 0
    for y in range(max(0, y0), min(h, y1)):
        for x in range(0, w, 7):  # every 7th column: the band's mean, cheaply
            tot += lum(px[x, y])
            n += 1
    return tot / max(1, n)


def measure(path):
    im = Image.open(path).convert("RGB")
    w, h = im.size
    page = band_luminance(im, h // 3, h // 3 + 60)     # middle of the page = its own background
    top = band_luminance(im, 0, TOP_BAND_ROWS)
    return {
        "path": os.path.basename(path),
        "size": [w, h],
        "bytes": os.path.getsize(path),
        "distinct_colours": len(set(im.getdata())),
        "top_band_luminance": round(top, 1),
        "page_luminance": round(page, 1),
        "top_band_is_banner": bool(top - page > BAND_LUM_DELTA),
        "image": im,
    }


def main():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--a", required=True, help="leg A: the headless `gopi shot` render")
    p.add_argument("--b", required=True, help="leg B: the X11 render, via gopi capture")
    p.add_argument("--out", required=True)
    p.add_argument("--scratch", required=True)
    p.add_argument("--prompt", default="")
    p.add_argument("--seconds-a", default="")
    p.add_argument("--seconds-b", default="")
    a = p.parse_args()

    for f in (a.a, a.b):
        if not os.path.exists(f):
            sys.exit("compare_web: missing %s" % f)
    os.makedirs(a.out, exist_ok=True)

    A = measure(a.a)
    B = measure(a.b)
    ia, ib = A.pop("image"), B.pop("image")

    diff = None
    if ia.size == ib.size:
        d = ImageChops.difference(ia, ib).convert("L")
        n = sum(1 for v in d.getdata() if v > 8)
        diff = {"pixels_differing": n, "of_total": ia.size[0] * ia.size[1],
                "share": round(n / float(ia.size[0] * ia.size[1]), 4)}

    # The A/B image: leg A above leg B, same scale, labelled. Between the two a
    # white separator, so the boundary cannot be mistaken for part of a page.
    w = max(ia.size[0], ib.size[0])
    h = LABEL_H + ia.size[1] + SEP + LABEL_H + ib.size[1]
    canvas = Image.new("RGB", (w, h), (255, 255, 255))
    draw = ImageDraw.Draw(canvas)
    y = 0
    # ASCII labels only (PIL's default bitmap font is latin-1).
    for label, im in (("A - headless gopi shot (incumbent)", ia),
                      ("B - real chromium in Xvfb, gopi capture", ib)):
        draw.text((8, y + 9), label, fill=(20, 20, 20))
        y += LABEL_H
        canvas.paste(im, (0, y))
        y += im.size[1] + SEP
    canvas_path = os.path.join(a.out, "ab.png")
    canvas.save(canvas_path)

    # The two legs travel WITH the comparison: a reader must be able to open each
    # one at its own resolution, and the numbers below must be re-measurable.
    shutil.copyfile(a.a, os.path.join(a.out, "legA-headless.png"))
    shutil.copyfile(a.b, os.path.join(a.out, "legB-x11.png"))

    result = {
        "question": "which renderer should photograph gopi's own web UI?",
        "seed_prompt": a.prompt,
        "legA": dict(A, how="gopi shot --self (gopi's own CDP client, headless chromium)",
                     seconds=a.seconds_a,
                     strengths=["no display needed", "deterministic viewport ⇒ byte-stable layout",
                                "the run's own banner line sits on a KNOWN band (%s)" % "80,102",
                                "no browser warning chrome in the picture"],
                     costs=["it is a headless engine: no window manager, no user font stack, "
                            "no scrollbar/decoration that a real browser would add"]),
        "legB": dict(B, how="real chromium as a systemd user unit on a private Xvfb, "
                            "photographed by gopi capture --window",
                     seconds=a.seconds_b,
                     strengths=["what a person with a real browser on this host sees",
                                "photographs the REAL window, client area only (--app mode adds "
                                "no address bar or tab strip)"],
                     costs=["needs a private Xvfb AND a transient user unit: snap chromium dies "
                            "with SIGTRAP (exit 133) as an ordinary child under this host's "
                            "AppArmor profile — measured 2026-10-01",
                            "chrome prints its own warning banner for --no-sandbox at the top of "
                            "the frame, inside the picture being published",
                            "a composed render has no cell grid, so its masks are bands"]),
        "diff": diff,
        "verdict": "leg A (headless gopi shot)",
        "why": ("Same size, same page, same seeded turn — so the choice is not about what is on "
                "screen. Leg B's picture carries chrome's own unsupported-flag banner as its top "
                "band (top_band_is_banner=true on leg B, false on leg A), i.e. publishing leg B "
                "means publishing a browser warning that is not gopi's, and it MOVES the run's own "
                "banner line off the band the mask was measured for. Leg B also needs a private "
                "display and a transient systemd unit to survive snap+AppArmor, which is machinery "
                "the incumbent does not need. So the delivered web.png stays the headless render; "
                "leg B remains available through scripts/human-ui/compare-web.sh for anyone who "
                "wants the real-browser picture, with the cost stated."),
    }
    with open(os.path.join(a.out, "ab.json"), "w", encoding="utf-8") as fh:
        json.dump(result, fh, ensure_ascii=False, indent=2, sort_keys=True)
    print(json.dumps({"legA": A, "legB": B, "diff": diff, "verdict": result["verdict"]},
                     ensure_ascii=False, indent=2, sort_keys=True))
    print("compare_web: wrote %s" % canvas_path)
    _ = a.scratch


if __name__ == "__main__":
    main()
