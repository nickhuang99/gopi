#!/usr/bin/env bash
# compare-terminals.sh — xterm vs THIS HOST'S real terminal, on identical bytes.
#
# Produces, under --out:
#   sample.ansi                 the bytes both terminals render (fixed, hashed)
#   leg-<term>.png              one capture per terminal, same 120-column geometry
#   terminals-compare.png       the side-by-side (script writes it)
#   terminals-compare.json      every swatch's measured colour, per terminal
#
# And with `--scratch-rc <file>` it adds a THIRD leg: the same real terminal
# pointed at a COPY of the user's profile with one colour changed — the fidelity
# proof ("change a colour, the picture follows"), which is only meaningful because
# the two real-terminal legs otherwise receive identical flags.
#
# USAGE
#   bash scripts/human-ui/compare-terminals.sh --bin bin/gopi --scratch <dir> \
#        --out docs/archive/demo/human/compare [--scratch-rc <terminalrc copy>]
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN=""; SCRATCH=""; OUT="$ROOT/docs/archive/demo/human/compare"; SCRATCH_RC=""
while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --scratch) SCRATCH="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --scratch-rc) SCRATCH_RC="$2"; shift 2 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$BIN" ] || { echo "--bin <gopi binary> required" >&2; exit 2; }
[ -n "$SCRATCH" ] || { echo "--scratch <dir> required" >&2; exit 2; }
[ -x "$BIN" ] || { echo "binary not executable: $BIN" >&2; exit 2; }

DISPLAY_NUM="${GOPI_TERMCMP_DISPLAY:-:64}"
SCREEN_W=2400; SCREEN_H=1500
COLS=120
ROWS=""   # from the sample itself (make_sample.py), never re-typed here
MIN_PX_PER_CHAR=16
# The glyph scale each terminal needs to clear the floor at 120 columns, on THIS
# host, MEASURED (see docs/archive/demo/human/compare/README.md for the table):
#   xterm  -fs 13                     → 2284px / 120 = 19.03 px per char
#   xfce4  GDK_DPI_SCALE=1.7          → 1936px / 120 = 16.13 px per char
# Colours are NEVER set by flags on either leg: the real terminal reads the
# user's profile, and that is the point of the comparison.
XTERM_FS=13
XTERM_FONT="Noto Sans Mono CJK SC"
VTE_SCALE=1.7

log() { printf '[term-cmp] %s\n' "$*" >&2; }
die() { log "FATAL: $*"; exit 1; }

mkdir -p "$SCRATCH" "$OUT"
XVFB_PID=""
TERM_PIDS=()
cleanup() {
  for p in "${TERM_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  pkill -x xterm 2>/dev/null
  pkill -x xfce4-terminal 2>/dev/null
  [ -n "$XVFB_PID" ] && kill "$XVFB_PID" 2>/dev/null
  return 0
}
trap cleanup EXIT

python3 "$ROOT/tools/human-ui/make_sample.py" "$SCRATCH/sample.ansi" "$SCRATCH/sample.json" >&2
# The terminal geometry comes FROM THE SAMPLE. A hard-coded row count here was a
# real defect for one run: the sample grew to 13 rows while the terminals were
# still launched 8 rows tall, so the top rows scrolled away and the measurement
# could not find the sample's bands (it refused — the guard worked, the geometry
# was wrong).
ROWS="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["rows"])' "$SCRATCH/sample.json")"
[ -n "$ROWS" ] || die "could not read the sample's row count"
log "geometry ${COLS}x${ROWS} (from the sample)"

Xvfb "$DISPLAY_NUM" -screen 0 "${SCREEN_W}x${SCREEN_H}x24" -nolisten tcp >"$SCRATCH/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 2
kill -0 "$XVFB_PID" 2>/dev/null || die "Xvfb $DISPLAY_NUM did not start"
export DISPLAY="$DISPLAY_NUM"
log "display=$DISPLAY_NUM (private)"
sha="$(sha256sum "$SCRATCH/sample.ansi" | cut -c1-16)"
log "sample sha256_16=$sha ($(stat -c %s "$SCRATCH/sample.ansi") bytes) — BOTH terminals get these exact bytes via cat"

biggest_window() {  # $1 = xdotool class
  local wid best="" bestarea=0 area
  for wid in $(xdotool search --class "$1" 2>/dev/null); do
    eval "$(xdotool getwindowgeometry --shell "$wid" 2>/dev/null)" || continue
    area=$((WIDTH * HEIGHT))
    [ "$area" -gt "$bestarea" ] && { bestarea=$area; best="$wid"; }
  done
  printf '%s' "$best"
}

capture_leg() {  # NAME  (the terminal must already be running)
  local name="$1" wid
  wid="$(biggest_window "$2")"
  [ -n "$wid" ] || die "no $2 window for leg $name"
  eval "$(xdotool getwindowgeometry --shell "$wid")"
  local ppc
  ppc="$(awk -v w="$WIDTH" -v c="$COLS" 'BEGIN{printf "%.3f", w/c}')"
  awk -v p="$ppc" -v m="$MIN_PX_PER_CHAR" 'BEGIN{exit !(p+1e-9 >= m)}' \
    || die "leg $name renders ${ppc} px per char (${WIDTH}px/${COLS}) — below ${MIN_PX_PER_CHAR}; the comparison would be unreadable"
  log "leg $name: window $wid ${WIDTH}x${HEIGHT} = ${ppc} px/char"
  xdotool mousemove $((WIDTH / 2)) $((HEIGHT / 2))
  sleep 1.5
  "$BIN" tool capture --display "$DISPLAY_NUM" --window --out "$OUT/$name.png" >/dev/null \
    || die "capture failed for $name"
  TERM_PIDS+=("$(xdotool getwindowpid "$wid" 2>/dev/null)")
  # remember the geometry for the report
  printf '%s\t%s\t%s\t%s\n' "$name" "$WIDTH" "$HEIGHT" "$ppc" >> "$SCRATCH/term-geometry.tsv"
}

: > "$SCRATCH/term-geometry.tsv"
printf '# leg\twidth\theight\tpx_per_char\n' >> "$SCRATCH/term-geometry.tsv"

# ── leg 1: xterm, the INTERNAL mechanism (no user palette) ─────────────────
log "leg xterm: -fs $XTERM_FS (no palette override — xterm uses its own 16-colour table)"
env LC_ALL=C.utf8 LANG=C.utf8 \
  xterm -u8 -geometry "${COLS}x${ROWS}" -fa "$XTERM_FONT" -fs "$XTERM_FS" -T termcmp-xterm \
        -e bash -lc "cat $SCRATCH/sample.ansi; sleep 60" >"$SCRATCH/xterm.log" 2>&1 &
sleep 4
capture_leg "leg-xterm" xterm
pkill -x xterm 2>/dev/null
sleep 1

# ── leg 2: the user's OWN terminal + profile (nothing overridden) ──────────
command -v xfce4-terminal >/dev/null || die "this comparison needs the host's own terminal (xfce4-terminal on zbook)"
log "leg user-terminal: xfce4-terminal, GDK_DPI_SCALE=$VTE_SCALE, profile = the operator's own terminalrc (untouched)"
env LC_ALL=C.utf8 LANG=C.utf8 GDK_DPI_SCALE="$VTE_SCALE" \
  xfce4-terminal --disable-server --geometry "${COLS}x${ROWS}" -T termcmp-user \
    -e "bash -lc 'cat $SCRATCH/sample.ansi; sleep 60'" >"$SCRATCH/terminal-user.log" 2>&1 &
sleep 5
capture_leg "leg-user-terminal" xfce4-terminal
pkill -x xfce4-terminal 2>/dev/null
sleep 1

# ── leg 3: same terminal, same scale, ONE colour changed ────────────────────
# The variant is DERIVED here when the caller does not supply one, so the proof is
# reproducible without an external file: a copy of the operator's profile with TWO
# lines changed — the background, and palette entry 1 (red). Palette entry 1 is the
# decisive one: it is exactly what a 16-colour terminal's built-in table (xterm)
# refuses to consult, so if the picture follows it, the picture is reading the
# user's palette.
if [ -z "$SCRATCH_RC" ]; then
  [ -r "$HOME/.config/xfce4/terminal/terminalrc" ] || die "no operator profile to vary"
  SCRATCH_RC="$SCRATCH/rc-variant"
  sed -e 's/^\(ColorPalette=[^;]*\);[^;]*;/\1;#ff0000;/' \
      -e 's/^ColorBackground=.*/ColorBackground=#3a0d0d/' \
      "$HOME/.config/xfce4/terminal/terminalrc" > "$SCRATCH_RC"
  log "variant profile derived: $(grep -c . "$SCRATCH_RC") lines, ColorPalette[1]→#ff0000, ColorBackground→#3a0d0d"
  log "  diff vs the operator's own file:"
  diff "$HOME/.config/xfce4/terminal/terminalrc" "$SCRATCH_RC" | sed 's/^/    /' >&2
fi
if [ -n "$SCRATCH_RC" ]; then
  [ -r "$SCRATCH_RC" ] || die "--scratch-rc not readable: $SCRATCH_RC"
  CFG="$SCRATCH/xdg-config-variant"
  rm -rf "$CFG"; mkdir -p "$CFG/xfce4/terminal"
  cp "$SCRATCH_RC" "$CFG/xfce4/terminal/terminalrc"
  log "leg user-terminal-changed: same terminal, same scale, XDG_CONFIG_HOME=$CFG (one colour differs)"
  env LC_ALL=C.utf8 LANG=C.utf8 GDK_DPI_SCALE="$VTE_SCALE" XDG_CONFIG_HOME="$CFG" \
    xfce4-terminal --disable-server --geometry "${COLS}x${ROWS}" -T termcmp-changed \
      -e "bash -lc 'cat $SCRATCH/sample.ansi; sleep 60'" >"$SCRATCH/terminal-changed.log" 2>&1 &
  sleep 5
  capture_leg "leg-user-terminal-changed" xfce4-terminal
  pkill -x xfce4-terminal 2>/dev/null
  sleep 1
fi

# ── the measurement ───────────────────────────────────────────────────────
ARGS=()
# ORDER = reference first: the host's OWN terminal is the reference, xterm is the
# deviation being measured, and the changed-profile leg is the fidelity proof.
for f in leg-user-terminal leg-xterm leg-user-terminal-changed; do
  [ -f "$OUT/$f.png" ] && ARGS+=(--term "$(printf '%s' "$f" | sed 's/^leg-//')=$OUT/$f.png")
done
python3 "$ROOT/tools/human-ui/compare_terms.py" --sample-json "$SCRATCH/sample.json" \
  --out "$OUT" "${ARGS[@]}" || die "compare_terms.py failed"

python3 - "$OUT" "$SCRATCH" <<'PY' || exit 1
import json, os, sys
out, scratch = sys.argv[1], sys.argv[2]
rep = json.load(open(os.path.join(out, "terminals-compare.json"), encoding="utf-8"))
terms = rep["terminals"]
ref = "user-terminal"
if ref not in terms:
    sys.exit("compare: the reference leg (%s) is missing — nothing to compare against" % ref)
bad = []
r = terms[ref]
print("[term-cmp] reference %s: %d distinct colours, %s" % (ref, r["distinct_colours"], r["size"]))
for name, t in terms.items():
    if name == ref:
        continue
    d = rep["deltas"].get(name, {})
    diff = [k for k, v in d.items() if not v["same"]]
    print("[term-cmp] %s: %d distinct colours, %d/%d swatches match the reference (%s)" %
          (name, t["distinct_colours"], len(d) - len(diff), len(d),
           ", ".join(sorted(diff)[:4]) or "all match"))
# The proof that the picture FOLLOWS the user's configuration: the changed leg must
# differ from the reference, and it must differ in the terminal's own BACKGROUND —
# the most frequent colour of the whole picture, which is exactly the property a
# profile's ColorBackground decides (and needs no marker: it is measured, not
# declared).
d = rep["deltas"].get("user-terminal-changed")
if d is not None:
    ref_bg = terms[ref]["background"]
    var = terms.get("user-terminal-changed", {})
    if var.get("background") == ref_bg:
        bad.append("the variant leg's background is IDENTICAL to the reference (%s): the picture "
                   "did NOT follow the profile change" % ref_bg)
    else:
        print("[term-cmp] fidelity proof: background colour %s -> %s after changing ONE line of a "
              "COPY of the operator's profile (reference vs variant leg)" % (ref_bg, var.get("background")))
    # And the PALETTE half, which is the half this whole change is about: a
    # terminal that ignores the user's palette (xterm's built-in 16-colour table)
    # would show the SAME swatch colour for a changed palette entry.
    ansi_changed = sorted(k for k, v in d.items() if not v["same"] and k.startswith("ansi"))
    if not ansi_changed:
        bad.append("the variant leg's ANSI swatches are all identical to the reference: palette "
                   "entry 1 was changed (#ff0000) and no ansi1 swatch moved, so the picture is NOT "
                   "reading the profile's palette — this is the xterm failure mode, in the leg that "
                   "is supposed to prove the opposite")
    else:
        first = ansi_changed[0]
        print("[term-cmp] fidelity proof: palette entry 1 moved ansi1 %s -> %s; %d/%d swatches "
              "changed with the profile" % (d[first]["reference"], d[first]["other"],
                                            len(ansi_changed), len(d)))
if bad:
    for b in bad:
        print("[term-cmp] FAIL: " + b, file=sys.stderr)
    sys.exit(1)
print("[term-cmp] OK")
PY
log "done: $OUT"
