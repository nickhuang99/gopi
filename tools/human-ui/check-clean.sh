#!/usr/bin/env bash
# check-clean.sh — prove the clean twin withholds what it claims to withhold,
# and that it changed NOTHING ELSE.
#
# Three checks, and the second one is the one that matters:
#
#   A. POSITIVE CONTROL. The identifiers must be findable in the RAW material.
#      Without this the whole check is vacuous: "no uuid in the clean images"
#      is trivially true of a run that never had one, and a vacuous pass is
#      worse than a failure because it reads as evidence.
#   B. CONTAINMENT. Every pixel that differs between raw/<x>.png and
#      clean/<x>.png must lie inside a recorded mask box. This is what makes
#      the twins "the same picture, one fact withheld" a measured claim rather
#      than a promise: a redaction that reflowed, rescanned or re-rendered the
#      frame would spill outside the boxes and fail here.
#   C. NO SHAPES LEFT. No delivered text artifact (the manifest, this README)
#      may carry a uuid / 32-hex id / home path.
#   D. THE BOX SAT ON ITS TARGET. Every recorded mask box must contain INK in the
#      raw picture — the thing it substitutes for. This is the check that closes
#      the leak class B cannot see, and it is here because that leak happened:
#      when the terminal's own chrome (a menu bar) shifted the cell grid, the
#      boxes were placed one row too high, landed on an EMPTY row, and painted
#      substitute ink THERE. Containment stayed green (every changed pixel was
#      inside a recorded box) while the session UUID sat unmasked one row above —
#      published, and invisible to every check, because the checks all asked the
#      dump (whose grid was right) and none asked the pixels.
#
# USAGE
#   bash tools/human-ui/check-clean.sh [--out docs/archive/demo/human] [--raw-scratch DIR]
# Exit 0 = all three green; non-zero names which one failed and on what.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OUT="$ROOT/docs/archive/demo/human"
SCRATCH=""

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --raw-scratch) SCRATCH="$2"; shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

fail=0
say() { printf '[check-clean] %s\n' "$*"; }

# The shapes that identify this host or this run. Written as SHAPES so the check
# never has to store a secret to look for one — which is also why it catches an
# identifier that arrived through a path the masker did not know about.
SHAPES='[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{32}|/home/[A-Za-z0-9_.-]+|sandbox-[A-Za-z0-9-]+-[0-9a-f]{8}'

# ── C first: the text artifacts are greppable, so this one is literal ─────
say "C. grepping the delivered TEXT artifacts for identifying shapes"
hits="$(grep -REn "$SHAPES" "$OUT" --include='*.md' --include='*.json' --include='*.sh' 2>/dev/null || true)"
if [ -n "$hits" ]; then
  say "FAIL C: identifying shapes found in delivered text:"
  printf '%s\n' "$hits" | head -20
  fail=1
else
  say "OK   C: no identifying shape in any delivered .md/.json/.sh"
fi

# ── A: positive control, on the raw material ─────────────────────────────
if [ -n "$SCRATCH" ] && [ -d "$SCRATCH" ]; then
  say "A. positive control — the identifiers must exist in the RAW material"
  raw_hits="$(grep -REh "$SHAPES" "$SCRATCH" 2>/dev/null | sort -u | head -20 || true)"
  if [ -z "$raw_hits" ]; then
    say "FAIL A: no identifying shape in $SCRATCH — the mask proves nothing about a run that had none"
    fail=1
  else
    say "OK   A: $(printf '%s\n' "$raw_hits" | wc -l) distinct identifying string(s) present in the raw material"
  fi
else
  say "SKIP A: no --raw-scratch given (the positive control is PART of the check; pass it to run it)"
fi

# ── B: containment, pixel by pixel ────────────────────────────────────────
say "B. containment — every changed pixel must lie inside a recorded mask box"
if [ -f "$OUT/redaction.json" ]; then
  python3 - "$OUT" <<'PY'
import json, os, sys
out = sys.argv[1]
spec = json.load(open(os.path.join(out, "redaction.json"), encoding="utf-8"))
try:
    from PIL import Image
except ImportError:
    sys.exit("FAIL B: python3-PIL missing (containment is a pixel measurement)")
bad = 0
checked = 0
for name, entry in sorted(spec.items()):
    raw = os.path.join(out, "raw", name)
    clean = os.path.join(out, "clean", name)
    if not (os.path.exists(raw) and os.path.exists(clean)):
        continue
    boxes = entry.get("boxes", [])
    band = entry.get("band", [])
    if band:
        # A band is a box that spans the image: it exists precisely because the
        # image has no cell grid to locate a narrower box in. Normalising it here
        # (rather than skipping it) is what keeps the containment check honest for
        # the browser render, which is the one artifact masked this way.
        with Image.open(raw) as im:
            bw, _bh = im.size
        boxes = list(boxes) + [[0, band[0], bw, band[1] - band[0]]]
    if not boxes:
        # No box means the clean twin must be byte-identical; anything else is
        # an unrecorded edit.
        same = open(raw, "rb").read() == open(clean, "rb").read()
        print("  %-12s no boxes → identical: %s" % (name, "yes" if same else "NO"))
        if not same:
            bad += 1
        checked += 1
        continue
    a = Image.open(raw).convert("RGB")
    b = Image.open(clean).convert("RGB")
    if a.size != b.size:
        print("  %-12s SIZE MISMATCH %s vs %s" % (name, a.size, b.size))
        bad += 1
        continue
    pa, pb = a.load(), b.load()
    w, h = a.size
    outside = 0
    first = None
    for y in range(h):
        for x in range(w):
            if pa[x, y] == pb[x, y]:
                continue
            inside = any(bx <= x < bx + bw and by <= y < by + bh for bx, by, bw, bh in boxes)
            if not inside:
                outside += 1
                if first is None:
                    first = (x, y)
    status = "OK" if outside == 0 else "FAIL"
    if outside:
        bad += 1
    print("  %-12s boxes=%d changed-outside-box=%d %s%s" % (
        name, len(boxes), outside, status,
        "" if first is None else " first=(%d,%d)" % first))
    checked += 1
print("B: %d artifact pair(s) checked, %d failed" % (checked, bad))
sys.exit(1 if bad else 0)
PY
  [ $? -ne 0 ] && fail=1
else
  say "FAIL B: $OUT/redaction.json is missing — containment cannot be checked without the recorded boxes"
  fail=1
fi

# ── D: the box sat on its target ─────────────────────────────────────────
say "D. every recorded mask box must contain the ink it masks"
if [ -f "$OUT/redaction.json" ]; then
  python3 - "$OUT" <<'PYEOF'
import json, os, sys
out = sys.argv[1]
spec = json.load(open(os.path.join(out, "redaction.json"), encoding="utf-8"))
try:
    from PIL import Image
    from collections import Counter
except ImportError:
    sys.exit("FAIL D: python3-PIL missing (this is a pixel measurement)")
INK_DELTA = 60
bad = 0
checked = 0
for name, entry in sorted(spec.items()):
    raw = os.path.join(out, "raw", name)
    if not os.path.exists(raw):
        continue
    boxes = entry.get("boxes", [])
    if not boxes:
        continue
    im = Image.open(raw).convert("RGB")
    bg = Counter(im.getdata()).most_common(1)[0][0]
    px = im.load()
    w, h = im.size
    empty = []
    for (x, y, bw, bh) in boxes:
        x0, y0 = max(0, x), max(0, y)
        x1, y1 = min(w, x + bw), min(h, y + bh)
        ink = 0
        for yy in range(y0, y1):
            for xx in range(x0, x1):
                p = px[xx, yy]
                if abs(p[0] - bg[0]) + abs(p[1] - bg[1]) + abs(p[2] - bg[2]) > INK_DELTA:
                    ink += 1
        if ink == 0:
            empty.append((x, y, bw, bh))
    status = "OK" if not empty else "FAIL"
    if empty:
        bad += 1
    print("  %-12s boxes=%d empty-boxes=%d %s%s" % (
        name, len(boxes), len(empty), status,
        "" if not empty else " first=(%d,%d,%d,%d)" % empty[0]))
    checked += 1
print("D: %d artifact pair(s) checked, %d with an empty mask box" % (checked, bad))
sys.exit(1 if bad else 0)
PYEOF
  [ $? -ne 0 ] && fail=1
fi

[ "$fail" -eq 0 ]
[ "$fail" -eq 0 ] && say "ALL GREEN" || say "NOT CLEAN"
exit "$fail"
