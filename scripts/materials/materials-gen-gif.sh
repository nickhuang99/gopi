#!/usr/bin/env bash
# materials-gen-gif.sh — 给一条素材加「动图」表达层：真实 gopi 终端会话 → 单文件 gif
#
# 形态（与 docs/archive/demo/materials/README.md 的静态层并存，不改不删静态层）：
#   <out>/06-gif.README.md   这次的出身、体积、时长、怎么复核
#   <out>/06-gif.gif         产物（单文件、无限循环、>=3s）
#   <out>/06-gif-raw.txt     同一次会话的 stdout+stderr 逐字节副本（复核基准）
#   <tmp>/frames/*.png       关键帧（中间物，落在私有临时目录，不入库）
#
# 路线：Route A（零安装）—— 真实 xterm 跑真 gopi，Xvfb 上抓关键帧，
#   ImageMagick 装帧、gifsicle 优化成一个文件。
#   为什么不用 asciinema+agg（Route B）：那条路存的是**文本流**（.cast 是 JSON），
#   可核对性天然更好；但本机没有这两个工具，装它们 = 新增持久依赖（Rule 35 四问）。
#   本线的可核对性由 tools/gif-verify.py 补上：拆帧 → 逐字符格双射 + 逐像素 IoU。
#
# 用法: bash scripts/materials/materials-gen-gif.sh [OUTDIR]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "materials-gen-gif: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }

# A demo must not depend on the shell that launched it. Measured: an unscrubbed
# run inherited the operator's GOPI_TASK_NOTIFY_CMD, so the row transition drew
# the notify failure into the picture — real output, wrong demo. So re-exec once
# under an environment with every caller-exported GOPI_* dropped; this script
# then re-exports exactly what its own session needs.
if [ -z "${GOPI_GIF_ENV_SCRUBBED:-}" ]; then
  UNSET=()
  while IFS= read -r v; do UNSET+=(-u "$v"); done < <(env | grep -oE '^GOPI_[A-Z_]+' || true)
  exec env "${UNSET[@]}" GOPI_GIF_ENV_SCRUBBED=1 bash "$0" "$@"
fi

OUT="${1:-$ROOT/docs/archive/demo/materials/001-notify-cmd-default-off}"
BIN="${GOPI_MATERIALS_BIN:-$ROOT/bin/gopi}"
[ -x "$BIN" ] || { echo "materials-gen-gif: no gopi binary at $BIN (build it first)" >&2; exit 2; }

# --- named constants (Rule 1) ------------------------------------------------
DISP="${GOPI_GIF_DISPLAY:-:88}"
SCREEN_W=1700; SCREEN_H=1000          # virtual screen — MUST exceed window + border
ROWS=44                               # >= the lines this session prints (measured 36) + margin, so nothing scrolls off
FONT="DejaVu Sans Mono"; FS=11
STEPS=8; INTERVAL=4                   # 8 keyframes x 4 s = 32 s of screen time
DELAY_CS=40                           # 40 cs = 400 ms per keyframe
PRODUCT="$OUT/06-gif.gif"
BASELINE="$OUT/06-gif-raw.txt"

for f in 03-raw.png 04-annotated.png; do
  [ -f "$OUT/$f" ] || { echo "materials-gen-gif: $OUT/$f is absent — this adds a layer, it does not replace one" >&2; exit 2; }
done

WORK="$(gopi_scratch_mkdir materials-gif)" || { echo "materials-gen-gif: cannot create an off-\`/\` scratch dir" >&2; exit 3; }
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$WORK/frames"

# ---- 1. a PRIVATE profile (never the operator's ledger) ---------------------
# Unique per run: the sandbox root is shared, and a REUSED one accumulates rows
# (measured: a second run opened on 3 in_progress rows and the notify error),
# which is real output but a non-deterministic demo. Unique => a clean, stable
# picture, and the sweep reclaims it on age (Rule 34) — no cleanup hook here.
SANDBOX="$("$BIN" tool sandbox create --label "gif-$$-$(date +%s)" --task-id "materials-gif-$$" | tail -1)"
[ -d "$SANDBOX/.gopi" ] || { echo "materials-gen-gif: sandbox init failed" >&2; exit 3; }

# The sandbox carries a COPY of the real settings, so it inherits notify.* — and
# with the notify command's target directory absent the row transition prints a
# '[task-notify] ... Directory nonexistent' block. That is genuine gopi output,
# but this demo is about the ledger transition, not about notify, so pin the
# notify knobs OFF for this session (the tree's own precedent: the 001 entry
# ships a settings-off.json next to it). GOPI_SETTINGS is a value, not a write
# into the profile — the profile's own settings.json is never touched.
SETTINGS_FIX="$WORK/settings-off.json"
python3 - "$SANDBOX/.gopi/settings.json" "$SETTINGS_FIX" <<'PY'
import json, sys
src, dst = sys.argv[1], sys.argv[2]
d = json.load(open(src))
d.pop("notify", None)
d["repos"] = {}          # the demo runs no merge; keep the picture to the ledger
json.dump(d, open(dst, "w"), indent=2)
print(f"settings fixture: {dst} (notify.* removed)")
PY
export GOPI_SETTINGS="$SETTINGS_FIX"

ROW="$("$BIN" task submit --help >/dev/null 2>&1; GOPI_HOME="$SANDBOX" "$BIN" task submit "gif demo row" 2>&1 \
      | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)"
echo "sandbox=$SANDBOX"; echo "row=$ROW"

# ---- 2. the session: every non-'$' line is VERBATIM gopi output -------------
SESSION="$WORK/session.sh"
cat > "$SESSION" <<'EOS'
set -u
SB="$1"; BIN="$2"; ROW="$3"; cd "$(dirname "$BIN")/.." || exit 1
printf '$ export GOPI_HOME=%s\n' "$SB"; export GOPI_HOME="$SB"; sleep 2
printf '$ ./bin/gopi task list --status pending\n'
./bin/gopi task list --status pending 2>&1; printf 'exit=%s\n' "$?"; sleep 5
printf '$ ./bin/gopi task update %s --status in_progress\n' "$ROW"
./bin/gopi task update "$ROW" --status in_progress 2>&1; printf 'exit=%s\n' "$?"; sleep 5
printf '$ ./bin/gopi task list --status in_progress\n'
./bin/gopi task list --status in_progress 2>&1; printf 'exit=%s\n' "$?"; sleep 5
printf '$ ./bin/gopi task show %s\n' "$ROW"
./bin/gopi task show "$ROW" 2>&1 | head -14; printf 'exit=%s\n' "$?"; sleep 5
printf '$ \n'
EOS
echo "SESSION_NAME=$SESSION" >/dev/null

# ---- 3. Xvfb + a real xterm, sized to FIT ----------------------------------
pkill -x Xvfb 2>/dev/null || true
Xvfb "$DISP" -screen 0 "${SCREEN_W}x${SCREEN_H}x24" -nolisten tcp > "$WORK/xvfb.log" 2>&1 &
sleep 2
export DISPLAY="$DISP"
export LANG=C.UTF-8 LC_ALL=C.UTF-8    # the output carries — and →; without UTF-8 they mojibake
xdpyinfo >/dev/null || { echo "materials-gen-gif: no display on $DISP" >&2; exit 3; }
ROOTW=$(xdpyinfo | awk '/dimensions:/{print $2}' | cut -dx -f1)
ROOTH=$(xdpyinfo | awk '/dimensions:/{print $2}' | cut -dx -f2)

xterm -u8 -T probewin -fa "$FONT" -fs "$FS" -geometry 80x24 -bg black -fg white \
      -e /bin/sh -c 'sleep 60' & PROBE=$!
sleep 2
PW=$(xdotool search --name probewin | head -1)   # xdotool matches the WINDOW TITLE (-T)
eval "$(xdotool getwindowgeometry --shell "$PW")"
CW=$(( (WIDTH-4) / 80 )); CH=$(( (HEIGHT-4) / 24 ))
kill "$PROBE" 2>/dev/null || true; sleep 1
COLS=$(( (ROOTW - 24) / CW ))         # as many columns as the screen holds
echo "cell=${CW}x${CH}px  root=${ROOTW}x${ROOTH}  -> window ${COLS}x${ROWS} cells"

( cd "$ROOT" && xterm -u8 -T demowin -fa "$FONT" -fs "$FS" -geometry "${COLS}x${ROWS}" \
    -bg black -fg white -e /bin/sh -c \
    "sh $SESSION '$SANDBOX' '$BIN' '$ROW' 2>&1 | tee '$BASELINE'; sleep 600" ) &
sleep 3
WID=$(xdotool search --name demowin | head -1)
eval "$(xdotool getwindowgeometry --shell "$WID")"
IX=$((X+2)); IY=$((Y+2))
[ $((X+WIDTH)) -le "$ROOTW" ] && [ $((Y+HEIGHT)) -le "$ROOTH" ] || {
  pkill -x Xvfb || true
  echo "materials-gen-gif: REFUSED — the window runs off the root; import -window root" >&2
  echo "  would wrap the crop past the screen edge into gray garbage (measured defect)." >&2
  exit 3; }

# ---- 4. keyframes ----------------------------------------------------------
i=1
while [ "$i" -le "$STEPS" ]; do
  import -window root -crop "${WIDTH}x${HEIGHT}+${IX}+${IY}" +repage "$WORK/frames/f$(printf '%02d' "$i").png"
  echo "frame $i at +$(( (i-1)*INTERVAL ))s"
  i=$((i+1)); [ "$i" -le "$STEPS" ] && sleep "$INTERVAL"
done
pkill -x Xvfb 2>/dev/null || true; pkill -x xterm 2>/dev/null || true; sleep 1

# ---- 5. one file out -------------------------------------------------------
convert -delay "$DELAY_CS" -loop 0 "$WORK/frames"/f0*.png "$WORK/A-raw.gif"
gifsicle --optimize=3 --colors 64 --loop "$WORK/A-raw.gif" -o "$PRODUCT"

# ---- 6. the self-check: refuse to hand over an unverifiable product --------
python3 "$ROOT/tools/gif-verify.py" "$PRODUCT" "$BASELINE" "$COLS" "$CW" "$CH" \
  > "$OUT/06-gif-verify.txt" 2>&1 || {
    echo "materials-gen-gif: the gif FAILED verification — see $OUT/06-gif-verify.txt" >&2
    sed -n '1,20p' "$OUT/06-gif-verify.txt" >&2; exit 4; }

DUR=$(python3 - "$PRODUCT" <<'PY'
import sys
from PIL import Image
im = Image.open(sys.argv[1]); tot = n = 0
try:
    while True:
        n += 1; tot += im.info.get("duration", 0); im.seek(im.tell() + 1)
except EOFError: pass
print(f"{n} frames {tot} ms")
PY
)
echo "product : $PRODUCT"
file "$PRODUCT"
echo "bytes   : $(stat -c %s "$PRODUCT")"
echo "duration: $DUR"
