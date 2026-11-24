#!/usr/bin/env bash
# Regenerate this example's GIF from the ASCII frame sources beside this script.
# Run from the example's own directory:  bash frames/make.sh
# Pure Go: gopi tool text2img renders each frame, gopi tool gif composes them.
set -euo pipefail
cd "$(dirname "$0")/.."

# The font is NOT a style choice here. These frames are BILINGUAL on purpose, and
# the CJK face below is the one the committed PNGs were rendered with. Measured:
# re-rendering f1.txt with DejaVuSansMono (a Latin-only face) gives 740 px wide and
# 2.5% ink instead of 410 px / 4.9% -- the Chinese lines become missing-glyph boxes
# and the width blows past the 500 px ceiling. The GIF still builds, so this failure
# is silent. Override FONT only with another face that actually carries CJK.
FONT=${FONT:-/usr/share/fonts/truetype/wqy/wqy-microhei.ttc}
SIZE=${SIZE:-22}; COLS=${COLS:-200}; LH=${LH:-26}; LINES=${LINES:-20}
GIF=$(basename "$PWD").gif

# 1. render each frame from its ASCII source
frames=()
for f in frames/f[0-9].txt; do
  out="frames/$(basename "${f%.txt}").png"
  gopi tool text2img "$f" "$out" --size "$SIZE" --cols "$COLS" --lh "$LH" \
      --lines "$LINES" --font "$FONT" >/dev/null
  frames+=("$out")
done

# 2. Every frame MUST have identical pixel dimensions, or `gopi tool gif` refuses.
#    A bilingual frame is NOT width-identical to another even with the same display
#    column count: the face is proportional (measured on wqy-microhei: ASCII step
#    4 px, CJK step 13 px -- ratio 3.25, where a true monospace face would be 2.00).
#    So each frame is padded to the widest one instead of being assumed equal.
python3 - "${frames[@]}" <<'PY'
import struct, sys
paths = sys.argv[1:]
dims = []
for p in paths:
    d = open(p, 'rb').read(33)
    wh = struct.unpack('>II', d[16:24])
    dims.append(wh)
    print('%-28s %dx%d' % (p, *wh))
W = max(d[0] for d in dims); H = max(d[1] for d in dims)
if len(set(dims)) != 1:
    from PIL import Image
    for p in paths:
        im = Image.open(p).convert('RGB')
        if im.size != (W, H):
            c = Image.new('RGB', (W, H), (255, 255, 255))
            c.paste(im, (0, 0)); c.save(p)
            print('padded %-24s %dx%d -> %dx%d' % (p, *im.size, W, H))
print('all frames agree:', W, 'x', H)
assert W <= 500, 'over the 500 px phone ceiling: %d' % W
PY

# 3. compose
gopi tool gif --frames "$(IFS=,; echo "${frames[*]}")" --out "$GIF" --delay 250

# 4. verify the artifact itself -- `gopi tool gif` WARNS above 500 px but still
#    writes the file, so a too-wide GIF looks exactly like a success.
python3 - "$GIF" <<'PY'
import struct, sys
from PIL import Image
p = sys.argv[1]
b = open(p, 'rb').read()
w, h = struct.unpack('<HH', b[6:10])
print('%s: %dx%d, %d B' % (p, w, h, len(b)))
assert b[:6] == b'GIF89a', 'not a GIF'
assert w <= 500, 'over the 500 px phone ceiling: %d' % w
# ink check: a frame that rendered no glyphs (wrong font, missing CJK) would read near 0
im = Image.open(p).convert('L')
px = list(im.getdata())
ink = sum(1 for v in px if v < 128)
pct = 100.0 * ink / len(px)
print('ink: %.1f%% of pixels' % pct)
assert pct > 1.0, 'almost no ink (%.2f%%) -- the font probably has no CJK glyphs' % pct
print('OK: %d frames, %dx%d, %.1f%% ink' % (getattr(Image.open(p), 'n_frames', 1), w, h, pct))
PY
