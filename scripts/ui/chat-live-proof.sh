#!/usr/bin/env bash
# live-proof.sh (v2) — ONE unbroken run: boot the SHIPPED `gopi chat` binary on a
# real X11 display in a real terminal, drive a REAL model turn through it, and
# capture the ACTIVE WINDOW with the shipped `gopi tool capture --window` BEFORE and
# AFTER, plus the chat's own `--dump-screen` for the status row's exact cells.
#
# v2 fixes two measured failures of v1 (both recorded in the report):
#   1. `xdotool type --window` sends SYNTHETIC events, which xterm DISCARDS
#      (allowSendEvents is false by default): the prompt never appeared and the
#      "turn" never reached the app. v2 types into the FOCUSED window (XTEST
#      events, indistinguishable from a keyboard) and VERIFIES the text landed on
#      the editing line before pressing Enter.
#   2. the child inherited this session's GOPI_TASK_REPORT_PATH/OWNER and refused
#      them by name; v2 removes both from the child's environment.
set -euo pipefail

# Rule 22: the probe scratch comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root, asserted off `/` by DEVICE ID and registered
# for trap-time cleanup. The X11 SOCKET directory is the ONE /tmp path this
# repository keeps (internal/shot/x11.go keeps the same one in Go): a unix
# socket there is created by the X server, never written by us, so it is not
# scratch and the off-/ rule does not apply to it. Both come from the helper,
# so neither is spelled here.
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
X11SOCKDIR="$(gopi_x11_socket_dir)"
PROBEDIR="$(gopi_scratch_mkdir x11-probe)" || exit 1
trap 'gopi_scratch_cleanup' EXIT

bin=""
outdir=""
boot_seconds=6
answer_timeout=150
palette="hermes"
prompt_text="In one short sentence, what colour is a goldfinch?"

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) bin="$2"; shift 2 ;;
    --out) outdir="$2"; shift 2 ;;
    --boot-seconds) boot_seconds="$2"; shift 2 ;;
    --answer-timeout) answer_timeout="$2"; shift 2 ;;
    --prompt) prompt_text="$2"; shift 2 ;;
    --palette) palette="$2"; shift 2 ;;
    *) echo "live-proof: unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$bin" ] || { echo "live-proof: --bin is required" >&2; exit 2; }
[ -n "$outdir" ] || { echo "live-proof: --out is required" >&2; exit 2; }
[ -x "$bin" ] || { echo "live-proof: $bin is not executable" >&2; exit 2; }

repo="$(cd "$(dirname "$bin")/.." && pwd)"
capture="$repo/bin/gopi"

# Rule 22: temporary paths OFF the / partition.
scratch_root="${GOPI_TMPDIR:-${TMPDIR:-}}"
case "$scratch_root" in ""|"/"|"/tmp") scratch_root="$HOME/workspace/_tmp" ;; esac
mkdir -p "$scratch_root"

utf8_locale=""
for cand in C.UTF-8 C.utf8 en_US.UTF-8; do
  if locale -a 2>/dev/null | grep -qx "$cand"; then utf8_locale="$cand"; break; fi
done
[ -n "$utf8_locale" ] || { echo "live-proof: no UTF-8 locale" >&2; exit 1; }

display_num=""
for cand in 99 98 97 96 95 94 93; do
  if ! [ -e ""$X11SOCKDIR"/X$cand" ]; then display_num=":$cand"; break; fi
done
[ -n "$display_num" ] || { echo "live-proof: no free display in :93-:99" >&2; exit 1; }

run="$outdir/run"
rm -rf "$outdir"
mkdir -p "$run/home/.gopi"

settings=""
if [ -f "$HOME/.gopi/settings.json" ]; then
  cp "$HOME/.gopi/settings.json" "$run/home/.gopi/settings.json"
  settings="$run/home/.gopi/settings.json"
  echo "live-proof: settings copied from the operator's own profile (model kept real)"
else
  echo "live-proof: no operator settings.json — built-in defaults" >&2
fi

xvfb_pid=""
xterm_pid=""
cleanup() {
  [ -n "${xterm_pid:-}" ] && kill "$xterm_pid" 2>/dev/null || true
  [ -n "${xvfb_pid:-}" ] && kill "$xvfb_pid" 2>/dev/null || true
}
trap cleanup EXIT

Xvfb "$display_num" -screen 0 1600x900x24 >"$run/xvfb.log" 2>&1 &
xvfb_pid=$!
for _ in $(seq 1 40); do [ -e ""$X11SOCKDIR"/X${display_num#:}" ] && break; sleep 0.1; done
export DISPLAY="$display_num"

term_bg="#300A24"
term_fg="#FFFFFF"
geometry="120x34"
dump_path="$outdir/chat-screen.dump"
window_title="gopi chat live proof"

envfile="$run/env.sh"
{
  echo "export HOME='$run/home'"
  echo "export GOPI_HOME='$run/home'"
  echo "export GOPI_SETTINGS='$settings'"
  echo "export PATH='$PATH'"
  echo "export GOPI_PALETTE='$palette'"
  echo "export COLORTERM=truecolor"
  echo "export TERM=xterm-256color"
  echo "export LANG='$utf8_locale'"
  echo "export LC_ALL='$utf8_locale'"
  echo "export GOPI_CHAT_DUMP='$dump_path'"
  # The child must NOT inherit this session's task-report contract: gopi refuses
  # a report path belonging to another session, by name.
  echo "unset GOPI_TASK_REPORT_PATH GOPI_TASK_REPORT_OWNER"
} > "$envfile"

LANG="$utf8_locale" LC_ALL="$utf8_locale" DISPLAY="$display_num" \
  xterm -u8 -geometry "$geometry" -fa Monospace -fs 12 \
  -bg "$term_bg" -fg "$term_fg" -title "$window_title" \
  -e sh -c ". '$envfile'; cd '$run'; exec '$bin' chat" &
xterm_pid=$!

sleep "$boot_seconds"
win_id="$(DISPLAY="$display_num" xdotool search --name "$window_title" | head -1)"
echo "live-proof: window=$win_id pid=$xterm_pid display=$display_num geometry=$geometry palette=$palette prompt=$(printf %q "$prompt_text")"
[ -n "$win_id" ] || { echo "live-proof: the chat window never appeared" >&2; exit 1; }

# Focus by REAL pointer motion + click (XTEST): neither carries
# --window, so the events are delivered to whatever is under the pointer, exactly
# as a human's would be. This is what makes typed keys arrive at all.
DISPLAY="$display_num" xdotool mousemove --window "$win_id" 60 20
DISPLAY="$display_num" xdotool click 1
sleep 1.5
focus="$(DISPLAY="$display_num" xdotool getwindowfocus 2>/dev/null || echo 0)"
echo "live-proof: focused window = $focus (expected $win_id)"

# ---- BEFORE -----------------------------------------------------------------
before_json="$(DISPLAY="$display_num" "$capture" capture --window --out "$outdir/before.png")"
echo "live-proof: BEFORE $before_json"

# ---- TYPE (into the FOCUSED window; XTEST, not synthetic) --------------------
DISPLAY="$display_num" xdotool type --delay 45 -- "$prompt_text"
sleep 1.0

# VERIFY the text landed on the editing line before submitting: a turn that was
# never typed is a capture of nothing (v1's failure, made visible).
typed=""
for _ in $(seq 1 20); do
  if [ -s "$dump_path" ] && grep -q "$(printf '%s' "$prompt_text" | cut -c1-20)" "$dump_path"; then typed=yes; break; fi
  sleep 0.25
done
echo "live-proof: editing line carries the prompt = ${typed:-NO}"
[ -n "$typed" ] || { echo "live-proof: the typed prompt never reached the editor — aborting rather than capturing a fake turn" >&2; exit 1; }

submitted_at="$(date -Iseconds)"
DISPLAY="$display_num" xdotool key Return
echo "live-proof: submitted at $submitted_at; polling up to ${answer_timeout}s for the real model's answer"

# ---- WAIT for the TURN TO COMPLETE (bounded poll, no blind sleep) ------------
# The completion signal is the chat's OWN status row: the live hint returns to
# "ready" with the iteration counter at 1. The first version polled for a word
# from the answer — which the ECHOED PROMPT also contains, so it matched at t+2s
# and captured the turn mid-flight ("thinking…"). Match the state, not the text.
waited=0
saw_request=no
turn_done=no
while [ "$waited" -lt "$answer_timeout" ]; do
  sleep 2
  waited=$((waited + 2))
  if [ "$saw_request" = no ]; then
    if find "$run/home/.gopi" -name "*.raw.log" -size +2k 2>/dev/null | grep -q .; then
      saw_request=yes
      echo "live-proof: t+${waited}s the model request left the process (raw log grew)"
    fi
  fi
  if [ -s "$dump_path" ] && grep -q "iter 1" "$dump_path" && grep -q "ready" "$dump_path"; then
    turn_done=yes
    echo "live-proof: t+${waited}s turn 1 completed (status row reports iter 1 + ready)"
    break
  fi
done
echo "live-proof: poll ended after ${waited}s (raw_log_request=$saw_request turn_done=$turn_done)"
[ "$turn_done" = yes ] || { echo "live-proof: the turn did not complete inside ${answer_timeout}s — the AFTER capture is a mid-flight state and the report must say so" >&2; }
sleep 2

# ---- AFTER ------------------------------------------------------------------
after_json="$(DISPLAY="$display_num" "$capture" capture --window --out "$outdir/after.png")"
echo "live-proof: AFTER  $after_json"

# ---- the untruncated artifact record ----------------------------------------
python3 - "$before_json" "$after_json" "$outdir" "$prompt_text" "$palette" "$display_num" "$geometry" "$submitted_at" "$waited" <<'PY'
import json, sys, os, hashlib
b = json.loads(sys.argv[1]); a = json.loads(sys.argv[2]); outdir = sys.argv[3]
rec = {
    "prompt": sys.argv[4], "palette": sys.argv[5],
    "display": sys.argv[6], "geometry": sys.argv[7],
    "submitted_at": sys.argv[8], "answer_wait_seconds": int(sys.argv[9]),
    "before": {"path": b.get("path"), "sha256_16": b.get("sha256_16"),
               "w": b.get("width"), "h": b.get("height"),
               "window": b.get("window"), "window_route": b.get("window_route"), "bytes": b.get("bytes")},
    "after": {"path": a.get("path"), "sha256_16": a.get("sha256_16"),
              "w": a.get("width"), "h": a.get("height"),
              "window": a.get("window"), "window_route": a.get("window_route"), "bytes": a.get("bytes")},
}
rec["screen_changed"] = rec["before"]["sha256_16"] != rec["after"]["sha256_16"]
rec["same_window"] = rec["before"]["window"] == rec["after"]["window"]
for k in ("before", "after"):
    p = rec[k]["path"]
    if p and os.path.exists(p):
        rec[k]["sha256_file_16"] = hashlib.sha256(open(p, "rb").read()).hexdigest()[:16]
print(json.dumps(rec, indent=2))
json.dump(rec, open(os.path.join(outdir, "live-proof.json"), "w"), indent=2)
PY

# ---- graceful exit, so the dump is FINALIZED (frames>0, full answer) --------
DISPLAY="$display_num" xdotool key ctrl+d
sleep 3
DISPLAY="$display_num" xdotool key ctrl+d 2>/dev/null || true
sleep 2

# ---- the chat's own screen, from its --dump-screen (exact cells, no OCR) ----
echo "--- chat screen, from the chat's own --dump-screen (exact cells) ---"
if [ -s "$dump_path" ]; then
  head -1 "$dump_path"
  awk -F'|' '/^[0-9][0-9][0-9]\|/ { if ($2 ~ /[^ ]/) print }' "$dump_path"
  echo "--- status row (bottom reserved row) ---"
  grep -n "^033|" "$dump_path"
else
  echo "live-proof: NO screen dump at $dump_path (frames=0) — recorded, not hidden" >&2
fi

# ---- pixel measurement of both captures (the gold, per band) ---------------
if [ -f "$repo/scripts/ui/chat-ui-measure.py" ]; then
  echo "--- pixel bands of before.png ---"
  python3 "$repo/scripts/ui/chat-ui-measure.py" "$outdir/before.png" --max-bands 8 2>&1 | head -12
  echo "--- pixel bands of after.png ---"
  python3 "$repo/scripts/ui/chat-ui-measure.py" "$outdir/after.png" --max-bands 8 2>&1 | head -12
fi
echo "live-proof: artifacts under $outdir"
