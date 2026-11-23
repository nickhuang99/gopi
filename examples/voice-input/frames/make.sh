#!/usr/bin/env bash
# Regenerate this example's GIF from the ASCII frame sources beside this script.
# Run from the example's own directory:  bash frames/make.sh
# Pure Go: gopi tool text2img renders each frame, gopi tool gif composes them.
set -euo pipefail
cd "$(dirname "$0")/.."

FONT=${FONT:-/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf}
SIZE=${SIZE:-13}; COLS=${COLS:-60}; LH=${LH:-20}; LINES=${LINES:-14}
GIF=$(basename "$PWD").gif

# 1. render each frame from its ASCII source
frames=()
for f in frames/f[0-9]*.txt; do
  out="frames/$(basename "${f%.txt}").png"
  gopi tool text2img "$f" "$out" --size "$SIZE" --cols "$COLS" --lh "$LH" \
      --lines "$LINES" --font "$FONT" >/dev/null
  frames+=("$out")
done

# 2. every frame MUST have identical pixel dimensions, or gopi tool gif refuses.
python3 - "${frames[@]}" <<'PY'
import struct, sys
dims = set()
for p in sys.argv[1:]:
    d = open(p, 'rb').read(33)
    dims.add(struct.unpack('>II', d[16:24]))
    print('%-28s %dx%d' % (p, *struct.unpack('>II', d[16:24])))
assert len(dims) == 1, 'frames disagree on size: %s' % dims
print('all frames agree:', dims.pop())
PY

# 3. compose. Keep the width under 500 px: gopi tool gif WARNS above the ceiling
#    but still writes the file, so a too-wide GIF looks like success -- verify.
gopi tool gif --frames "$(IFS=,; echo "${frames[*]}")" --out "$GIF" --delay 250

python3 - "$GIF" <<'PY'
import struct, sys
p = sys.argv[1]; d = open(p, 'rb').read(16)
w, h = struct.unpack('<HH', d[6:10])
print('%s: %dx%d, %d B' % (p, w, h, len(open(p,'rb').read())))
assert d[:6] == b'GIF89a', 'not a GIF'
assert w <= 500, 'over the 500 px phone ceiling: %d' % w
PY
