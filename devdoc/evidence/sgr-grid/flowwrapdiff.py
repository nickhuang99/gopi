#!/usr/bin/env python3
"""flowwrapdiff.py — the verdict on a before/after capture pair.

Why a comparator and not a raw `diff`: the reserved layout's band carries blank
rows, and how many of them a shot begins with depends on where the band's
scroll position happened to sit when the shot was taken — MEASURED, two runs of
the SAME binary differ by exactly one leading blank row (before vs before-rerun
below). A raw diff therefore reports an offset that is instrument noise, not the
change. So the comparison NORMALIZES the leading blank rows and then reports the
one thing the capture claims: the WIDTH, in cells, of the wrapped reasoning row's
segments — several short rows (the layout's own re-cut) versus rows that fill the
band (the producer's wrap).

Usage: flowwrapdiff.py <before.grid.txt> <after.grid.txt> [<before-rerun.grid.txt>]
"""
import sys


def rows(path):
    out = []
    for line in open(path).read().split("\n"):
        out.append(line.split("|", 1)[1] if "|" in line else "")
    while out and not out[0].strip():
        out.pop(0)
    while out and not out[-1].strip():
        out.pop()
    return out


def flow_segments(rs):
    """The contiguous rows carrying the reasoning fill token, with their widths."""
    segs = []
    started = False
    for r in rs:
        if "[thinking]" in r:
            started = True
        if not started:
            continue
        if r.strip() and "flowwrap" in r:
            segs.append(len(r))
        elif r.strip() and segs:
            break
    return segs


def main():
    if len(sys.argv) < 3:
        print(__doc__)
        return 2
    before, after = sys.argv[1], sys.argv[2]
    rb, ra = rows(before), rows(after)
    sb, sa = flow_segments(rb), flow_segments(ra)

    print("=== the reasoning row's segments, in cells ===")
    print("before  (%s): %s" % (before, sb))
    print("after   (%s): %s" % (after, sa))
    cols = max(sb + sa + [0])
    print("band width (widest row seen): %d cells" % cols)
    print("before: full-width segments %d of %d" % (sum(1 for w in sb if w == cols), len(sb)))
    print("after : full-width segments %d of %d" % (sum(1 for w in sa if w == cols), len(sa)))
    print()
    # THE content claim: the FLOW TEXT is the agent's own bytes and must be
    # byte-identical across the pair — a wrap may move where a row breaks, never
    # what the text is. (The chrome's row POSITION is shot timing: the band
    # begins at whatever blank row its scroll position was on, measured to differ
    # between two runs of the SAME binary, so it is not part of this claim and is
    # reported separately.)
    fb = "".join(r for r in rb if "flowwrap" in r)
    fa = "".join(r for r in ra if "flowwrap" in r)
    print("=== the flow text ===")
    print("flow rows: before %d, after %d" % (len(sb) + 0, len(sa) + 0))
    print("flow TEXT byte-identical: %s (%d cells)" % ("YES" if fb == fa else "NO", len(fb)))
    if fb != fa:
        print("  before %r" % fb)
        print("  after  %r" % fa)
    print("chrome rows (editing/status) present in both: %s"
          % ("YES" if ("you>" in "".join(rb) and "you>" in "".join(ra)
                       and "thinking" in "".join(rb) and "thinking" in "".join(ra)) else "NO"))

    if len(sys.argv) > 3:
        rr = rows(sys.argv[3])
        print()
        print("=== instrument control: the SAME binary, two runs ===")
        print("%s vs %s -> identical: %s" % (before, sys.argv[3], "YES" if rb == rr else "NO"))
        if rb != rr:
            print("  (the two runs' row counts: %d vs %d — the offset a raw diff reports is"
                  " the band's blank-row position, i.e. shot timing, not the binary)"
                  % (len(rb), len(rr)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
