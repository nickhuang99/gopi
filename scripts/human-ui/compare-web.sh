#!/usr/bin/env bash
# compare-web.sh — the web-capture A/B, as a script a reader can re-run.
#
# THE QUESTION. `gopi shot` photographs gopi's web UI with a HEADLESS chromium
# (its own CDP client, no display at all). The alternative is to render the SAME
# page in a real browser on this build's private Xvfb and photograph it with
# `gopi capture` — "what a person with a browser actually sees". Both legs are
# produced here, from ONE seeded turn against ONE running `gopi web`, so the two
# pictures differ only in how they were rendered.
#
# IT IS NOT A FLAG ON build.sh, on purpose: an A/B that a build can silently flip
# is a choice nobody re-checks. This script is the measurement; build.sh reads
# the verdict recorded next to it (docs/archive/demo/human/compare/) and keeps the
# headless leg.
#
# WHAT THE MEASUREMENT FOUND (this host, 2026-10-01; full table in ab.json):
#   * the X11 leg cannot run as an ordinary child process — snap chromium dies
#     with SIGTRAP (exit 133) under this host's AppArmor profile. It RUNS when
#     started as a transient systemd USER unit (`systemd-run --user`), which is
#     how `gopi shot` already launches headless chrome here, so no new dependency
#     and no privilege is involved (Rules 24/25).
#   * the X11 leg's picture carries chrome's OWN warning banner ("You are using an
#     unsupported command-line flag: --no-sandbox") burned into the top of the
#     frame at y≈0..35, i.e. it MOVED the run's own banner line off the band the
#     headless render puts it on — a browser window that ignores viewport size
#     composes its own layout, which is exactly why a composed render is masked
#     by band rather than by cell.
#   * the headless leg is deterministic (a fixed viewport ⇒ byte-stable layout),
#     needs no display, and keeps the mask's geometry stable across hosts.
#
# USAGE
#   bash scripts/human-ui/compare-web.sh --bin bin/gopi --scratch <dir> [--out <dir>]
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN=""; SCRATCH=""; OUT="$ROOT/docs/archive/demo/human/compare"
while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --scratch) SCRATCH="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$BIN" ] || { echo "--bin <gopi binary> required" >&2; exit 2; }
[ -n "$SCRATCH" ] || { echo "--scratch <dir> required (Rule 22/30: intermediates stay out of the worktree)" >&2; exit 2; }
[ -x "$BIN" ] || { echo "binary not executable: $BIN" >&2; exit 2; }

# A private display, a private number, never the operator's. Nothing here reads
# $DISPLAY from outside; the script SETS it for its own children only.
DISPLAY_NUM="${GOPI_WEBAB_DISPLAY:-:63}"
SCREEN_W=2400; SCREEN_H=1500
VIEW_W=1440; VIEW_H=1400
PORT="${GOPI_WEBAB_PORT:-18101}"
CHROME_UNIT="gopi-webab-chrome-$$"     # transient user unit; --collect ⇒ no residue

log() { printf '[web-ab] %s\n' "$*" >&2; }
die() { log "FATAL: $*"; exit 1; }

mkdir -p "$SCRATCH" "$OUT"
XVFB_PID=""
cleanup() {
  systemctl --user stop "$CHROME_UNIT" >/dev/null 2>&1
  [ -n "${WEB_PID:-}" ] && kill "$WEB_PID" 2>/dev/null
  [ -n "$XVFB_PID" ] && kill "$XVFB_PID" 2>/dev/null
  return 0
}
trap cleanup EXIT

Xvfb "$DISPLAY_NUM" -screen 0 "${SCREEN_W}x${SCREEN_H}x24" -nolisten tcp >"$SCRATCH/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 2
kill -0 "$XVFB_PID" 2>/dev/null || die "Xvfb $DISPLAY_NUM did not start"
export DISPLAY="$DISPLAY_NUM"
log "display=$DISPLAY_NUM (private; the operator's display is untouched)"

SB="$("$BIN" tool sandbox create --label "webab-$(date +%Y%m%d-%H%M%S)" --task-id "${GOPI_TASK_RUN_ID:-human-ui}" 2>/dev/null | tail -1)"
[ -d "$SB/.gopi" ] || die "sandbox create produced no profile"
log "sandbox=$SB"

GOPI_HOME="$SB" "$BIN" web --addr "127.0.0.1:$PORT" >"$SCRATCH/web-server.log" 2>&1 &
WEB_PID=$!
sleep 5
kill -0 "$WEB_PID" 2>/dev/null || die "gopi web did not start (see $SCRATCH/web-server.log)"

# ONE seeded turn, so BOTH legs photograph a conversation rather than an empty
# page: an empty page proves the page loads, not that it is used.
PROMPT="In two short lines: what does gopi's ledger keep?"
python3 "$ROOT/tools/human-ui/seed_web.py" --addr "127.0.0.1:$PORT" --prompt "$PROMPT" \
  --json-out "$SCRATCH/web-turn.json" || die "seeding the turn failed"
log "seeded turn: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["turn_seconds"], "s,", json.load(open(sys.argv[1]))["stream_bytes"], "bytes")' "$SCRATCH/web-turn.json" 2>/dev/null)"

# ── LEG A — headless `gopi tool shot --self` (the incumbent) ────────────────────
export GOPI_SHOT_MEM_MAX_MB="${GOPI_SHOT_MEM_MAX_MB:-4096}"
LEG_A="$SCRATCH/legA-headless.png"
T0=$(date +%s.%N)
"$BIN" tool shot --self --addr "127.0.0.1:$PORT" --viewport "${VIEW_W}x${VIEW_H}" \
      --wait-selector "#log" --wait 3 --out "$LEG_A" >"$SCRATCH/shot.json" 2>"$SCRATCH/shot.err" \
  || die "leg A (gopi shot) failed: $(tail -2 "$SCRATCH/shot.err")"
T1=$(date +%s.%N)
log "legA headless: $(cat "$SCRATCH/shot.json")"

# ── LEG B — a REAL browser on the private Xvfb, photographed by gopi tool capture ─
# systemd-run --user: snap chromium dies under this host's AppArmor profile when
# started as an ordinary child (SIGTRAP, exit 133, measured), and a transient
# USER unit is the same mechanism `gopi shot` already uses. Unprivileged.
CHROME_BIN="$(command -v chromium || command -v chromium-browser || true)"
[ -n "$CHROME_BIN" ] || die "no system chromium/chromium-browser on PATH"
T2=$(date +%s.%N)
systemd-run --user --unit="$CHROME_UNIT" --collect --quiet \
  --setenv=DISPLAY="$DISPLAY_NUM" --setenv=HOME="$HOME" \
  -- "$CHROME_BIN" --no-sandbox --disable-gpu \
     --user-data-dir="$SCRATCH/chrome-profile" --no-first-run --no-default-browser-check \
     --window-position=0,0 --window-size="${VIEW_W},${VIEW_H}" \
     --app="http://127.0.0.1:$PORT/" || die "could not start chromium as a user unit"
LEG_B="$SCRATCH/legB-x11.png"
W=""
for _ in $(seq 1 30); do
  sleep 1
  W="$(xdotool search --class chromium 2>/dev/null | tail -1)"
  [ -n "$W" ] && break
done
[ -n "$W" ] || die "no chromium window appeared within 30s (unit: systemctl --user status $CHROME_UNIT)"
eval "$(xdotool getwindowgeometry --shell "$W")"
log "legB chromium window $W ${WIDTH}x${HEIGHT}"
# Park the pointer inside (a bare Xvfb has no WM, so XTEST targets the window
# under the pointer) and give the page the beat it needs to finish painting.
xdotool mousemove $((WIDTH / 2)) $((HEIGHT / 2))
sleep 6
"$BIN" tool capture --display "$DISPLAY_NUM" --window --out "$LEG_B" || die "leg B capture failed"
T3=$(date +%s.%N)
log "legB x11: $(stat -c %s "$LEG_B") bytes"

# ── MEASURE BOTH LEGS, mechanically ────────────────────────────────────────
python3 "$HERE/compare_web.py" --a "$LEG_A" --b "$LEG_B" --out "$OUT" --scratch "$SCRATCH" \
  --prompt "$PROMPT" \
  --seconds-a "$(awk -v a="$T0" -v b="$T1" 'BEGIN{printf "%.2f", b-a}')" \
  --seconds-b "$(awk -v a="$T2" -v b="$T3" 'BEGIN{printf "%.2f", b-a}')" \
  || die "compare_web.py failed"
log "done: $OUT (legA-headless.png, legB-x11.png, ab.json, ab.png)"
