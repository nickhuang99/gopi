#!/usr/bin/env python3
"""pxdiff.py — per-frame pixel difference table for a GIF frame sequence.

WHY THIS FILE EXISTS. `sha256_16` (or any file hash) answers "are these two
files byte-identical?" — which is NOT the question a film asks. A live screen
repaints: a caret blinks, a clock ticks, a status counter increments, and the
whole PNG re-encodes byte-differently while the PICTURE is the same. The
inverse is worse: two frames that differ in the picture can hash identically
if the encoder is deterministic and the capture is stale. So the evidence that
a GIF is "moving" has to be measured on PIXELS, one adjacent pair at a time.

USAGE
  pxdiff.py --frames a.png,b.png,... [--out diff.tsv] [--tolerance N]

OUTPUT (one row per adjacent pair, TSV)
  pair  from  to  changed  total  pct  bbox  jaccard
`changed` counts pixels whose channel values differ by more than --tolerance
(default 0: any difference counts). A pair with 0 changed pixels means the film
stood still between those two frames, and a film whose every pair is 0 is not
an animation — this script exits non-zero when that is the case, so "it moves"
is a gate and not a claim.
"""
import argparse
import sys

try:
    from PIL import Image
except ImportError:  # pragma: no cover - environment guard, names the channel
    sys.exit("pxdiff: python3-PIL missing (Image.open is the only image reader here)")


def parse_args():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--frames", required=True, help="comma-separated PNG paths, in play order")
    p.add_argument("--out", default="", help="TSV path (default: stdout only)")
    p.add_argument("--tolerance", type=int, default=0, help="per-channel difference tolerated (default 0)")
    return p.parse_args()


def diff_pair(a_path, b_path, tol):
    a = Image.open(a_path).convert("RGB")
    b = Image.open(b_path).convert("RGB")
    if a.size != b.size:
        raise SystemExit(f"pxdiff: frame size mismatch {a_path}({a.size}) vs {b_path}({b.size})")
    pa, pb = a.load(), b.load()
    w, h = a.size
    changed = 0
    minx, miny, maxx, maxy = w, h, -1, -1
    for y in range(h):
        for x in range(w):
            ra, ga, ba = pa[x, y]
            rb, gb, bb = pb[x, y]
            if abs(ra - rb) > tol or abs(ga - gb) > tol or abs(ba - bb) > tol:
                changed += 1
                if x < minx:
                    minx = x
                if y < miny:
                    miny = y
                if x > maxx:
                    maxx = x
                if y > maxy:
                    maxy = y
    return changed, (minx, miny, maxx, maxy), w * h


def main():
    args = parse_args()
    frames = [f for f in args.frames.split(",") if f.strip()]
    if len(frames) < 2:
        sys.exit("pxdiff: need at least two frames")
    rows = ["pair\tfrom\tto\tchanged\ttotal\tpct\tbbox\tjaccard"]
    still_pairs = []
    for i in range(len(frames) - 1):
        changed, bbox, total = diff_pair(frames[i], frames[i + 1], args.tolerance)
        if changed == 0:
            still_pairs.append(i)
            bbox_str = "-"
        else:
            bbox_str = "%dx%d+%d+%d" % (bbox[2] - bbox[0] + 1, bbox[3] - bbox[1] + 1, bbox[0], bbox[1])
        rows.append("%d\t%s\t%s\t%d\t%d\t%.4f%%\t%s\t%.4f" % (
            i + 1, frames[i].rsplit("/", 1)[-1], frames[i + 1].rsplit("/", 1)[-1],
            changed, total, 100.0 * changed / total, bbox_str, changed / total))
    table = "\n".join(rows)
    print(table)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(table + "\n")
    if still_pairs:
        sys.exit("pxdiff: %d adjacent pair(s) show ZERO changed pixels (pairs %s) — "
                 "the film stands still there" % (len(still_pairs), still_pairs))


if __name__ == "__main__":
    main()
