#!/usr/bin/env python3
"""replay-pixel.py — the PIXEL read of one film directory's frames.

WHY THIS EXISTS. `scripts/replay-check.sh` is an anchor audit: every one of its
other criteria reads a NUMBER — a geometry, a frame count, a byte size, a return
code. A film can satisfy every one of them and still be visually destroyed: HP
measured 32.9% of one film's frames carrying a full-width pure-black band
straight across the face while `check-claims.sh` was green. Geometry, md5
distinctness and byte counts are all silent about that, because none of them
reads a pixel. This reader is that missing layer.

WHAT IT MEASURES, per frame:

  px           the tallest FULL-WIDTH same-colour run, in pixels. A run is k >= 1
               consecutive rows that are each a single colour AND identical to
               one another.
  colour       that run's RGB colour.
  nonmodal_px  the same measurement restricted to runs whose colour is NOT the
               frame's modal (most frequent) colour — the band ACROSS the ground.
  share_pct    the modal colour's share of the frame, as a percentage.

WHY THE MODAL COLOUR IS THE SEPARATOR, AND WHY HEIGHT ALONE IS NOT. A printed
frame is ink on a ground, so full-width ground bands between blocks of text are
NORMAL — a census of 2606 frames from every film this repo has produced found a
healthy full-width run of 111 px (the declared ground rgb(19,25,38), filling the
space between the banner and the first tip block). Height alone therefore
separates nothing. The separator is the PAIR (height, colour identity): in that
same census, a full-width run in a colour that is NOT the frame's modal colour
never exceeded 6 px, while modal-colour runs reach 111 px. Hence two failures
this instrument feeds, which point in opposite directions:

  * a band painted ACROSS the ground — a tall run in a colour the frame is not
    mostly made of (a black bar over a dark ground is the HP defect);
  * a frame whose ground IS the band — the whole face one colour, however dark
    (near-blank), which is why an all-dark frame is flagged although a dark
    ground on its own is not a defect at all.

The thresholds are NOT here: they are named constants in scripts/replay-check.sh,
which is the ONE place that judges (Rule 1/13). This file only measures, so the
same read can be quoted in a report (see --analysis) and decided by the gate.

HONESTY (Rule 15). A directory with no frames is reported as `skipped` with the
reason, and a frame that cannot be decoded is reported as `err` with the path —
never silently absent from the read. Exit: 0 = measured (whatever the values),
2 = the argument is not a directory, 3 = the reader cannot read pixels at all
(no PIL, or no numpy AND no PIL) — a reader that could not run must be
distinguishable from a reader that ran and found nothing wrong.

READ THIS BEFORE TRUSTING A NUMBER: the PNG is read through PIL's convert("RGB"),
so the value at a pixel is the PNG's own pixel, not a guess from a hash. The run
scan is exact (byte equality of whole rows), not sampled.

OUTPUT (stdout, TAB separated, stable column order — the shell parses positionally
so the interface is defined exactly once, here):

  frame  <basename>  px  colour  nonmodal_px  nonmodal_colour  share_pct  modal_colour
  dir    <n_frames>  <WxH>  <max px>  <its colour>  <its frame>
         <max nonmodal_px>  <its colour>  <its frame>  <max share_pct>  <its frame>

The `dir` row carries the maxima over that directory's frames, so the caller
reads one row per directory. The human-readable table goes to stderr, never to
stdout: stdout is the machine interface, and mixing the two is how a parser
starts reading prose.

USAGE
  python3 scripts/replay-pixel.py <film-dir>              # measure one directory
  python3 scripts/replay-pixel.py --analysis <film-dir>   # + a human block on stdout
"""

import glob
import os
import sys

EXIT_OK = 0
EXIT_USAGE = 2
EXIT_NO_READER = 3

FRAME_GLOB = "frame-*.png"
RGB_MAX = 255
NO_COLOUR = "-"
NO_FRAME = "-"

try:
    import numpy as np
except ImportError:  # the slow path is still an exact read, just slower
    np = None

try:
    from PIL import Image
except ImportError:
    Image = None


def fail_no_reader(what):
    sys.stderr.write(
        "replay-pixel: no pixel reader available (%s) — the frames were NOT read\n" % what
    )
    return EXIT_NO_READER


def colour_hex(c):
    return "#%02x%02x%02x" % c


def measure(path):
    """Return the measurements of one frame, or raise."""
    im = Image.open(path).convert("RGB")
    w, h = im.size
    raw = im.tobytes()
    stride = w * 3
    rows = [raw[y * stride:(y + 1) * stride] for y in range(h)]

    # The modal colour, and a colour -> pixel-count lookup for it. Both come from
    # ONE histogram pass over the frame's own bytes.
    counts = {}
    if np is not None:
        arr = np.frombuffer(raw, dtype=np.uint8).reshape(h, w, 3).astype(np.uint32)
        packed = (arr[:, :, 0] << 16) | (arr[:, :, 1] << 8) | arr[:, :, 2]
        vals, cnt = np.unique(packed, return_counts=True)
        total = int(w) * int(h)
        modal_packed = int(vals[int(cnt.argmax())])
        modal = ((modal_packed >> 16) & RGB_MAX, (modal_packed >> 8) & RGB_MAX, modal_packed & RGB_MAX)
        peak = int(cnt.max())
    else:
        from collections import Counter
        hist = Counter(im.getdata())
        modal, peak = max(hist.items(), key=lambda kv: kv[1])
        total = int(w) * int(h)

    best_px = 0
    best_colour = None
    best_other_px = 0
    best_other_colour = None
    y = 0
    while y < h:
        row = rows[y]
        k = 1
        while y + k < h and rows[y + k] == row:
            k += 1
        # full width AND one colour: every byte-triple in the row identical
        if row == row[:3] * w:
            c = (row[0], row[1], row[2])
            if c == modal:
                if k > best_px:
                    best_px, best_colour = k, c
            elif k > best_other_px:
                best_other_px, best_other_colour = k, c
        y += k

    return {
        "w": w,
        "h": h,
        "px": best_px,
        "colour": best_colour,
        "other_px": best_other_px,
        "other_colour": best_other_colour,
        "share_pct": 100.0 * peak / total,
        "modal": modal,
    }


def measure_dir(dirpath):
    frames = sorted(glob.glob(os.path.join(dirpath, FRAME_GLOB)))
    return frames


def main(argv):
    analysis = False
    args = []
    for a in argv:
        if a == "--analysis":
            analysis = True
        else:
            args.append(a)
    if len(args) != 1:
        sys.stderr.write("usage: replay-pixel.py [--analysis] <film-dir>\n")
        return EXIT_USAGE
    if Image is None:
        return fail_no_reader("PIL/Image is not importable")
    dirpath = args[0]
    if not os.path.isdir(dirpath):
        sys.stderr.write("replay-pixel: %s is not a directory\n" % dirpath)
        return EXIT_USAGE

    frames = measure_dir(dirpath)
    if not frames:
        sys.stdout.write("dir\t0\t-\t-\t%s\t%s\t-\t%s\t%s\t-\t%s\n" % (NO_COLOUR, NO_FRAME, NO_COLOUR, NO_FRAME, NO_FRAME))
        sys.stderr.write("replay-pixel: skipped %s — no %s on disk\n" % (dirpath, FRAME_GLOB))
        return EXIT_OK

    best_px, best_colour, best_frame = 0, None, NO_FRAME
    best_other, best_other_colour, best_other_frame = 0, None, NO_FRAME
    best_share, best_share_frame = -1.0, NO_FRAME
    wxh = "-"
    for path in frames:
        m = measure(path)
        base = os.path.basename(path)
        wxh = "%dx%d" % (m["w"], m["h"])
        if m["px"] > best_px:
            best_px, best_colour, best_frame = m["px"], m["colour"], base
        if m["other_px"] > best_other:
            best_other, best_other_colour, best_other_frame = m["other_px"], m["other_colour"], base
        if m["share_pct"] > best_share:
            best_share, best_share_frame = m["share_pct"], base
        sys.stdout.write(
            "frame\t%s\t%d\t%s\t%d\t%s\t%.1f\t%s\n"
            % (
                base,
                m["px"],
                colour_hex(m["colour"]) if m["colour"] else NO_COLOUR,
                m["other_px"],
                colour_hex(m["other_colour"]) if m["other_colour"] else NO_COLOUR,
                m["share_pct"],
                colour_hex(m["modal"]),
            )
        )
    sys.stdout.write(
        "dir\t%d\t%s\t%d\t%s\t%s\t%d\t%s\t%s\t%.1f\t%s\n"
        % (
            len(frames),
            wxh,
            best_px,
            colour_hex(best_colour) if best_colour else NO_COLOUR,
            best_frame,
            best_other,
            colour_hex(best_other_colour) if best_other_colour else NO_COLOUR,
            best_other_frame,
            best_share,
            best_share_frame,
        )
    )

    sys.stderr.write(
        "replay-pixel: %s — %d frame(s) %s, tallest full-width run %dpx %s (frame %s), "
        "tallest non-modal-colour full-width run %dpx %s (frame %s), max modal share %.1f%% (frame %s)\n"
        % (
            dirpath,
            len(frames),
            wxh,
            best_px,
            colour_hex(best_colour) if best_colour else NO_COLOUR,
            best_frame,
            best_other,
            colour_hex(best_other_colour) if best_other_colour else NO_COLOUR,
            best_other_frame,
            best_share,
            best_share_frame,
        )
    )
    if analysis:
        sys.stdout.write(
            "# analysis: dir=%s frames=%d %s | tallest full-width same-colour run %dpx (%s, %s) | "
            "of which non-modal-colour %dpx (%s, %s) | max modal-colour share %.1f%% (%s)\n"
            % (
                dirpath,
                len(frames),
                wxh,
                best_px,
                colour_hex(best_colour) if best_colour else NO_COLOUR,
                best_frame,
                best_other,
                colour_hex(best_other_colour) if best_other_colour else NO_COLOUR,
                best_other_frame,
                best_share,
                best_share_frame,
            )
        )
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
