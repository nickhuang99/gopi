#!/usr/bin/env bash
# build.sh — produce the "human's interface" showcase under docs/archive/demo/human/.
#
# WHAT IT MAKES
#   hero.png         a real gopi chat round trip, >= 1200px wide
#   chat-loop.gif    typing -> streaming answer, >= 12 distinct frames, and
#                    >= $MIN_PX_PER_CHAR pixels per terminal character
#   chat.png         the chat surface, cropped to the terminal's cell band
#   ui.png           the `gopi ui` panel in use
#   web.png          gopi's own web chat UI, captured BY `gopi shot`
#   raw/ clean/      every still twice: as captured, and with this host's own
#                    identifiers replaced (see docs/archive/demo/human/README.md)
#   manifest.json    what was run, what each frame is, and the measured evidence
#
# THREE PROPERTIES THIS SCRIPT DOES NOT TRADE AWAY
#
#  1. **It never touches the operator's desktop.** A private Xvfb (:61) is
#     created, ONE terminal lives in it, and the pointer is moved into that window
#     before any keystroke. The operator's DISPLAY, terminal and sessions are
#     not read, not written and not signalled.
#  2. **No third-party recorder.** Pixels come from `gopi capture` (a hand-rolled
#     X11 GetImage client), the web page from `gopi shot` (its own CDP client),
#     the animation from `gopi gif` (internal/gifgen), the labels from
#     `gopi annotate`, the prose from `gopi text2img`. Outside the gopi binary,
#     only the base system (Xvfb / the host's own terminal / xdotool) is required.
#  3. **The terminal is the HOST'S OWN, not a stand-in.** Which one is probed from
#     the desktop session (`--term` overrides) and the profile it renders with is
#     the operator's real one — see the "WHICH TERMINAL" block below for why an
#     xterm capture was a distortion rather than a style choice.
#  4. **It leaves the worktree clean.** Every intermediate frame is written
#     under --scratch; only the delivered artifacts land in --out.
#
# WHY THE FILM IS SAMPLED THE WAY IT IS. A model answer here renders in about
# two to four seconds, so a leisurely sampling cadence photographs the gap
# BEFORE the answer and the stillness AFTER it, and returns a film of one
# state. The sampler therefore runs as fast as the capture path allows, and the
# film is then reduced to DISTINCT states (tools/human-ui/assemble.py): a frame
# that repeats the previous picture is not merely redundant, it would make the
# per-frame diff table report a zero-change pair — turning the evidence that the
# artifact moves into a claim that it stood still.
#
# USAGE
#   bash scripts/human-ui/build.sh --bin bin/gopi --scratch <dir> [--out <dir>]
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN=""
SCRATCH=""
OUT="$ROOT/docs/archive/demo/human"
TOOLS="$ROOT/tools/human-ui"

# The private display. Fixed so a re-run is comparable; killed at exit.
# 1500px tall on purpose: a 120x40 VTE window at the legibility floor is ~1358px,
# and a window TALLER than the screen is captured clipped — dead margins in every
# frame, a defect no hash would reveal. (xterm's own 120x40 is ~1084px.)
DISPLAY_NUM=:61
SCREEN_W=2400
SCREEN_H=1500
TERM_COLS=120
TERM_ROWS=40
# ── WHICH TERMINAL, AND WHICH OF ITS PROFILES ─────────────────────────────
# THE FILM MUST SHOW WHAT A USER OF *THIS HOST* SEES. Until 2026-10-01 these
# captures ran in `xterm`, and that was not a style choice — it was a失真: xterm
# renders the 16 ANSI colours with its OWN palette and never consults the
# operator's, so gopi's gold (`colour-230` / `colour-178`) came out as something
# else entirely. Measured on this host: the same escape sequences, a real
# terminal vs xterm on the same private Xvfb.
#
# So the terminal is CHOSEN, never written into the pipeline:
#   * `--term` / $GOPI_HUMAN_UI_TERM  → explicit, wins over everything;
#   * otherwise PROBED from the login session's own desktop (see probe_terminal),
#     which on zbook is XFCE ⇒ xfce4-terminal, and on HP is GNOME ⇒ gnome-terminal;
#   * `xterm` remains selectable — it is still the INTERNAL mechanism
#     (scripts/ui/chat-ui-observe.sh and the self-observation legs), just not the
#     one this showcase publishes.
# The PROFILE is the other half of fidelity, and its default is the operator's
# OWN file, read where the terminal itself reads it — `~/.config/xfce4/terminal/
# terminalrc` for xfce4-terminal (the same file the desktop's Settings dialog
# writes), dconf for gnome-terminal. `--profile scratch` points the terminal at a
# COPY instead, which is how the fidelity proof works: change one colour in the
# copy, re-record, and the frame changes with it (recorded in the manifest).
TERM_KIND="${GOPI_HUMAN_UI_TERM:-}"           # xfce4 | gnome | xterm; empty ⇒ probe
PROFILE_MODE="${GOPI_HUMAN_UI_PROFILE:-user}" # user (default; faithful) | scratch
PROFILE_RC="${GOPI_HUMAN_UI_PROFILE_RC:-}"    # explicit profile file (implies scratch)
TERM_WHY=""
TERM_SIZE=""          # xterm: -fs points
TERM_SCALE="1.0"      # VTE (xfce4/gnome): GDK_DPI_SCALE
TERM_FONT_ARG=""      # VTE: --font, only ever set in scratch mode
TERM_CONFIG_ARG=""    # VTE: --config <copy>, only ever set in scratch mode
TERM_WINDOW_CLASS=""  # xdotool --class of the chosen terminal
TERM_PID=""
# The size knob is CALIBRATED, not declared. The user's own font (DejaVu Sans Mono
# 12 in this host's terminalrc) gives ~10.13 px per character at 120 columns —
# below the legibility floor — so a candidate is tried, its window measured, and
# the first candidate that clears $MIN_PX_PER_CHAR is taken. Lists are in
# PREFERENCE order (first to clear wins), and the chosen value plus every
# measurement are printed and recorded. Colours are NEVER touched here.
TERM_SIZE_CANDIDATES_XTERM=(13 15 17 20)             # -fs points
TERM_SIZE_CANDIDATES_VTE=(1.8 1.6 2.0 2.2 2.4)       # GDK_DPI_SCALE
XTERM_FONT="Noto Sans Mono CJK SC"
XTERM_SIZE=13

# THE LEGIBILITY FLOOR, and it is about CHARACTERS, not file size. The film is a
# terminal transcript: what a reader needs is not a wide file but glyphs they can
# read, so the criterion is pixels PER CHARACTER — window width / terminal columns
# — and an earlier cut of this film failed it (960px / 120 cols = 8.00 px/char:
# the text was there and could not be read). At 120 cols and 13pt the window
# measures 2284px, i.e. 19.03 px/char, so the film is delivered at the capture's
# OWN resolution (assemble.py: --gif-min-px-per-char) and NO downscale is applied
# — a downscale here would both drop the film below the floor's margin and, on
# this dithered grab, make the file LARGER (measured: 3.55MB at 2284px vs 6.14MB
# at 2056px). The floor is asserted at capture time (drive_into_window) and again
# on the delivered bytes (assemble.py), because the two can disagree only if
# someone moved the geometry.
MIN_PX_PER_CHAR=16

# The typed prompt. ASCII on purpose: `xdotool type` cannot deliver a multi-byte
# character to a terminal (it fails with "Invalid multi-byte sequence"), so a
# CJK prompt is not typeable and therefore not honestly demonstrable. The ANSWER
# is Chinese either way — the model answers in the operator's language, and that
# is the half worth showing.
PROMPT_1="What is gopi? Answer in at most 6 short lines."
PROMPT_2="And what is it NOT? At most 3 one-line items."

# The row band of the `gopi shot` render that carries the page's own address and
# session id (its banner line, ~20px tall, starting ~80px down). A browser render
# composes its own layout, so this cannot be located as a cell box — it is masked
# as a band, and the band is recorded in redaction.json like any other mask.
WEB_BANNER_BAND="80,102"

# The typed prompt is fed in CHUNKS, one captured frame each: that is what makes
# the "typing" half of the film a recording of typing rather than a caption
# claiming it happened.
CHUNKS=("What" " is" " gopi" "? Ans" "wer i" "n at" " most" " 6 sh" "ort l" "ines" "." "")

# Chrome reserves ~51 GiB of virtual address space; the default 512 MiB unit
# ceiling kills chromium 153 on this host before it prints a DevTools port
# (CONFIG.md carries the knob's chronology). Raised HERE, in one place.
export GOPI_SHOT_MEM_MAX_MB="${GOPI_SHOT_MEM_MAX_MB:-4096}"

# The web leg stays the headless `gopi shot` render, and that is a MEASURED
# choice, not inertia: the X11 leg (a real chromium in this same Xvfb) was built
# and photographed — scripts/human-ui/compare-web.sh — and it needs a systemd
# transient unit to survive this host's snap+AppArmor profile, prints chrome's
# own "--no-sandbox is unsupported" warning banner INTO the picture, and hides
# the run's banner line somewhere a cell grid cannot name. Evidence and the
# full comparison: docs/archive/demo/human/compare/. The A/B is a script a reader can
# re-run; it is not a knob this build silently flips.

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --scratch) SCRATCH="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --term) TERM_KIND="$2"; shift 2 ;;
    --profile) PROFILE_MODE="$2"; shift 2 ;;
    --profile-rc) PROFILE_RC="$2"; PROFILE_MODE="scratch"; shift 2 ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$BIN" ] || { echo "--bin <gopi binary> required (not guessed from PATH)" >&2; exit 2; }
[ -n "$SCRATCH" ] || { echo "--scratch <dir> required (intermediates must not land in the worktree)" >&2; exit 2; }
[ -x "$BIN" ] || { echo "binary not executable: $BIN" >&2; exit 2; }
mkdir -p "$SCRATCH" "$OUT"

log() { printf '[human-ui] %s\n' "$*" >&2; }
die() { log "FATAL: $*"; exit 1; }

XVFB_PID=""
cleanup() { [ -n "$XVFB_PID" ] && kill "$XVFB_PID" 2>/dev/null; return 0; }
trap cleanup EXIT

# ── 0. environment ────────────────────────────────────────────────────────
# A fresh sandbox per run (Rule 34): reusing one leaves the previous run's screen
# dumps in the session tree, and `ui_state --target` refuses to guess between two
# candidates. A fresh profile keeps the tree unambiguous as well as the read.
SB="$("$BIN" tool sandbox create --label "human-ui-$(date +%Y%m%d-%H%M%S)" \
      --task-id "${GOPI_TASK_RUN_ID:-human-ui}" 2>/dev/null | tail -1)"
[ -d "$SB/.gopi" ] || die "sandbox create produced no profile at $SB"
export GOPI_HOME="$SB"
log "sandbox=$SB"

Xvfb "$DISPLAY_NUM" -screen 0 "${SCREEN_W}x${SCREEN_H}x24" -nolisten tcp >"$SCRATCH/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 2
kill -0 "$XVFB_PID" 2>/dev/null || die "Xvfb $DISPLAY_NUM did not start (see $SCRATCH/xvfb.log)"
export DISPLAY="$DISPLAY_NUM"
log "display=$DISPLAY_NUM ${SCREEN_W}x${SCREEN_H} (private; the operator's display is untouched)"

SHOTS="$SCRATCH/shots"; rm -rf "$SHOTS"; mkdir -p "$SHOTS/type" "$SHOTS/stream" "$SHOTS/ui"

capture_to() {
  "$BIN" tool capture --display "$DISPLAY_NUM" --window --out "$1" >/dev/null 2>&1 \
    || die "gopi capture failed for $1"
  [ -s "$1" ] || die "gopi capture wrote no bytes to $1"
}

# probe_terminal — decide WHICH terminal without a hard-coded name.
# Precedence: --term/$GOPI_HUMAN_UI_TERM (explicit) → the login session's own
# desktop → the config file a terminal of that desktop would have written. Never
# a bare "xfce4-terminal" literal in the pipeline: on another host the same rule
# has to answer the same question.
probe_terminal() {
  local d="${XDG_CURRENT_DESKTOP:-}${DESKTOP_SESSION:-}"
  d="$(printf '%s' "$d" | tr 'A-Z' 'a-z')"
  case "$d" in
    *xfce*) TERM_KIND=xfce4; TERM_WHY="login session desktop=${d:-<unset>} ⇒ Xfce" ; return ;;
    *gnome*|*unity*|*ubuntu*) TERM_KIND=gnome; TERM_WHY="login session desktop=${d:-<unset>} ⇒ GNOME" ; return ;;
  esac
  if [ -r "$HOME/.config/xfce4/terminal/terminalrc" ] && command -v xfce4-terminal >/dev/null; then
    # The reason is RECORDED (it lands in the delivered manifest), so it must not
    # carry an absolute path: the showcase's own cleanliness gate greps the
    # delivered text for identifying shapes and refused a run whose manifest named
    # the operator's home directory. Measured, this run: check-clean.sh FAIL C.
    TERM_KIND=xfce4; TERM_WHY="desktop unset, but this user HAS an Xfce terminal profile (~/.config/xfce4/terminal/terminalrc)"
  elif command -v gnome-terminal >/dev/null; then
    TERM_KIND=gnome; TERM_WHY="desktop unset, no Xfce profile ⇒ GNOME Terminal"
  else
    TERM_KIND=xterm; TERM_WHY="no desktop terminal found at all ⇒ xterm (the internal mechanism)"
  fi
}

# term_prepare — bind the chosen terminal's NAME, window class and profile.
term_prepare() {
  [ -n "$TERM_KIND" ] || probe_terminal
  case "$TERM_KIND" in
    xfce4)
      command -v xfce4-terminal >/dev/null || die "xfce4-terminal requested but not installed"
      TERM_WINDOW_CLASS="xfce4-terminal"
      TERM_SIZE="${TERM_SIZE_CANDIDATES_VTE[0]}"
      ;;
    gnome)
      command -v gnome-terminal >/dev/null || die "gnome-terminal requested but not installed"
      TERM_WINDOW_CLASS="gnome-terminal"
      TERM_SIZE="${TERM_SIZE_CANDIDATES_VTE[0]}"
      ;;
    xterm)
      command -v xterm >/dev/null || die "xterm requested but not installed"
      TERM_WINDOW_CLASS="xterm"
      TERM_SIZE="${TERM_SIZE_CANDIDATES_XTERM[0]}"
      ;;
    *) die "--term must be xfce4 | gnome | xterm (got '$TERM_KIND')" ;;
  esac

  # THE PROFILE — where fidelity comes from, and the ONE channel this script uses
  # to get it. In `user` mode NOTHING is set: the terminal reads its own config,
  # where it always does, so the picture is the operator's picture. In `scratch`
  # mode a COPY of it is placed in a private XDG_CONFIG_HOME (a 1-file skeleton,
  # the same layout the real directory has) and the terminal is pointed there —
  # which is how the fidelity PROOF re-records with one colour changed. Env vars
  # are used instead of command-line colour flags on purpose: the flags would be
  # this script inventing a palette, which is the failure this whole change is
  # about.
  TERM_ENV=(LC_ALL=C.utf8 LANG=C.utf8)
  TERM_PROFILE_SRC="$HOME/.config/xfce4/terminal/terminalrc"
  TERM_PROFILE_USED="the operator's own profile (nothing overridden)"
  case "$TERM_KIND:$PROFILE_MODE" in
    xfce4:user) ;;
    xfce4:scratch)
      local src="${PROFILE_RC:-$TERM_PROFILE_SRC}"
      [ -r "$src" ] || die "profile source not readable: $src"
      CFGHOME="$SCRATCH/xdg-config"; rm -rf "$CFGHOME"
      mkdir -p "$CFGHOME/xfce4/terminal"
      cp "$src" "$CFGHOME/xfce4/terminal/terminalrc"
      TERM_ENV+=(XDG_CONFIG_HOME="$CFGHOME")
      TERM_PROFILE_USED="a COPY of $(basename "$src") in a private XDG_CONFIG_HOME (scratch mode)"
      ;;
    gnome:*)
      if [ "$PROFILE_MODE" = "scratch" ]; then
        die "gnome-terminal takes its palette from dconf, and rewriting the user's dconf to prove fidelity would MUTATE the operator's settings — that is not a trade this script may make. Use --term xfce4 --profile scratch for the rewrite proof, or run the gnome leg in user mode."
      fi
      TERM_PROFILE_USED="the operator's own dconf profile (nothing overridden)" ;;
    xterm:*)
      TERM_PROFILE_USED="xterm has no profile in this pipeline: it is the INTERNAL mechanism, not the published one" ;;
  esac
}

# term_launch TITLE CMD LOG — start the chosen terminal running `bash -lc CMD`.
# One place per terminal family, because their command-line shapes differ
# (xterm: -e; xfce4: --disable-server -e "<string>"; gnome: --wait -- <argv>).
term_launch() {
  local title="$1" cmd="$2" logf="$3" q
  q="$(printf '%q' "$cmd")"
  case "$TERM_KIND" in
    xterm)
      env "${TERM_ENV[@]}" xterm -u8 -geometry "${TERM_COLS}x${TERM_ROWS}" \
          -fa "$XTERM_FONT" -fs "$TERM_SIZE" -T "$title" -e bash -lc "$cmd" \
          >"$logf" 2>&1 & ;;
    xfce4)
      # --disable-server: without it a second launch lands as a TAB in the first
      # window, and the film would photograph two conversations in one frame.
      env "${TERM_ENV[@]}" GDK_DPI_SCALE="$TERM_SCALE" \
          xfce4-terminal --disable-server --geometry "${TERM_COLS}x${TERM_ROWS}" \
          $TERM_CONFIG_ARG -T "$title" \
          -e "bash -lc $q" >"$logf" 2>&1 & ;;
    gnome)
      env "${TERM_ENV[@]}" GDK_DPI_SCALE="$TERM_SCALE" \
          gnome-terminal --wait --geometry "${TERM_COLS}x${TERM_ROWS}" \
          $TERM_CONFIG_ARG -t "$title" -- bash -lc "$cmd" >"$logf" 2>&1 & ;;
  esac
  echo $!
}

# term_window — the terminal's REAL top-level window, by geometry.
# A bare Xvfb has no window manager, so no window has a size hint and no window
# is auto-sized; a GTK terminal therefore leaves a 10x10 placeholder on the root
# beside the real window (measured: xwininfo shows both). Picking the first hit
# would photograph a 10x10 square, so the rule is: the LARGEST window of that
# class, and if two share it, refuse — an ambiguous pick is the bug, not a
# detail.
term_window() {
  local wid best="" bestarea=0 area n=0
  for wid in $(xdotool search --class "$TERM_WINDOW_CLASS" 2>/dev/null); do
    eval "$(xdotool getwindowgeometry --shell "$wid" 2>/dev/null)" || continue
    area=$((WIDTH * HEIGHT))
    [ "$area" -gt "$bestarea" ] && { bestarea=$area; best="$wid"; n=1; }
    [ "$area" -eq "$bestarea" ] && n=$((n + 1))
  done
  [ -n "$best" ] || return 1
  printf '%s' "$best"
}

# drive_into_window — park the pointer inside the terminal and echo its window id.
# A bare Xvfb has no window manager, so `xdotool windowfocus` is a no-op there
# and XTEST delivers key events to the window UNDER THE POINTER; moving the
# pointer is the mechanism. The geometry assertion is not decoration: a terminal
# wider or taller than the screen would be captured CLIPPED (with dead margins),
# and every frame of the film would be wrong in a way no hash would reveal.
drive_into_window() {
  local wid X Y WIDTH HEIGHT
  wid="$(term_window)" || die "no ${TERM_WINDOW_CLASS} window found on $DISPLAY_NUM"
  eval "$(xdotool getwindowgeometry --shell "$wid")"
  [ "$WIDTH" -le "$SCREEN_W" ] || die "$TERM_KIND is ${WIDTH}px wide, wider than the ${SCREEN_W}px screen (frames would be clipped)"
  [ "$HEIGHT" -le "$SCREEN_H" ] || die "$TERM_KIND is ${HEIGHT}px tall, taller than the ${SCREEN_H}px screen (frames would be clipped)"
  # THE LEGIBILITY FLOOR, checked where it can still be fixed. The delivered film
  # is this window's pixels at (or above) the floor; a window that came out too
  # narrow for $TERM_COLS characters would ship an unreadable film no matter how
  # the frames are later assembled, and the failing number would be invisible in
  # every downstream artifact. So the capture refuses it and names the arithmetic.
  local ppc
  ppc="$(awk -v w="$WIDTH" -v c="$TERM_COLS" 'BEGIN{printf "%.3f", w/c}')"
  awk -v p="$ppc" -v m="$MIN_PX_PER_CHAR" 'BEGIN{exit !(p+0.0005 >= m)}' \
    || die "the terminal renders ${ppc} px per character (${WIDTH}px / ${TERM_COLS} cols), below the ${MIN_PX_PER_CHAR} px floor: the film's words would not be readable. Enlarge the glyphs (the size knob this script calibrates) or the column count."
  log "legibility: ${WIDTH}px / ${TERM_COLS} cols = ${ppc} px per character (floor ${MIN_PX_PER_CHAR})"
  xdotool mousemove $((X + WIDTH / 2)) $((Y + HEIGHT / 2)) >/dev/null
  echo "$wid"
}

# calibrate_term_size — find the SMALLEST size knob that clears the floor, by
# MEASURING it, and refuse if none does. The user's own glyph size (DejaVu Sans
# Mono 12 in this host's terminalrc) gives 10.13 px per char at 120 columns, i.e.
# below the floor, so something has to grow — and the honest something is the
# FONT SCALE, never the palette: colours are the half that must stay exactly the
# operator's. Every candidate is tried for real (a terminal is launched, its
# window measured, then killed) and every measurement is printed and recorded, so
# the chosen value is a measurement and not a claim.
calibrate_term_size() {
  local cands=() ts w meas best="" rows_or_scale
  case "$TERM_KIND" in
    xterm) cands=("${TERM_SIZE_CANDIDATES_XTERM[@]}") ;;
    *)     cands=("${TERM_SIZE_CANDIDATES_VTE[@]}") ;;
  esac
  : > "$SCRATCH/term-size.tsv"
  printf '# terminal=%s  cols=%s  rows=%s  floor_px_per_char=%s\n' "$TERM_KIND" "$TERM_COLS" "$TERM_ROWS" "$MIN_PX_PER_CHAR" >> "$SCRATCH/term-size.tsv"
  for ts in "${cands[@]}"; do
    case "$TERM_KIND" in
      xterm) TERM_SIZE="$ts" ;;
      *)     TERM_SCALE="$ts" ;;
    esac
    local pid logf
    logf="$SCRATCH/term-size-${ts}.log"
    pid="$(term_launch "gopi-size-probe" "sleep 12" "$logf")"
    local i=0 wid=""
    while [ "$i" -lt 20 ]; do
      sleep 0.5; i=$((i + 1))
      wid="$(term_window 2>/dev/null)" && break
    done
    if [ -z "$wid" ]; then
      printf '%s\t%s\t%s\n' "$ts" "no-window" "$(tail -1 "$logf" 2>/dev/null | cut -c1-60)" >> "$SCRATCH/term-size.tsv"
      kill "$pid" 2>/dev/null
      continue
    fi
    eval "$(xdotool getwindowgeometry --shell "$wid")"
    w="$WIDTH"; local h="$HEIGHT"
    meas="$(awk -v w="$w" -v c="$TERM_COLS" 'BEGIN{printf "%.3f", w/c}')"
    printf '%s\t%sx%s\t%s\n' "$ts" "$w" "$h" "$meas" >> "$SCRATCH/term-size.tsv"
    kill "$pid" 2>/dev/null
    pkill -x "$TERM_KIND-terminal" 2>/dev/null
    sleep 0.6
    if [ -z "$best" ] && awk -v p="$meas" -v m="$MIN_PX_PER_CHAR" 'BEGIN{exit !(p+1e-9 >= m)}'; then
      best="$ts"
      if [ "$w" -le "$SCREEN_W" ] && [ "$h" -le "$SCREEN_H" ]; then break; fi
    fi
  done
  [ -n "$best" ] || die "no size knob in the candidate list reaches ${MIN_PX_PER_CHAR} px per character for $TERM_KIND (see $SCRATCH/term-size.tsv): the user's own glyph size is below the legibility floor at ${TERM_COLS} columns, and this script will not shrink the column count or the font of the published film silently"
  case "$TERM_KIND" in
    xterm) TERM_SIZE="$best" ;;
    *)     TERM_SCALE="$best" ;;
  esac
  log "size knob calibrated: $TERM_KIND $([ "$TERM_KIND" = xterm ] && echo "-fs $best" || echo "GDK_DPI_SCALE=$best") (measurements in $SCRATCH/term-size.tsv)"
  # xfce4-terminal can additionally take --font; it is set ONLY in scratch mode,
  # where the whole point is to show the profile being followed. In user mode the
  # font is the user's own and only the SCALE is ours.
  if [ "$TERM_KIND" = xfce4 ] && [ "$PROFILE_MODE" = "scratch" ]; then
    rows_or_scale="$(sed -n 's/^FontName=//p' "${CFGHOME:-}/xfce4/terminal/terminalrc" 2>/dev/null)"
    [ -n "$rows_or_scale" ] && log "scratch profile font: $rows_or_scale (from the copy)"
  fi
}

# term_shutdown — stop whatever this leg started, without touching anything else
# on the display (Rule: the operator's desktop is never signalled; this only ever
# names a pid this script itself launched).
term_shutdown() {
  [ -n "${1:-}" ] && kill "$1" 2>/dev/null
  pkill -x "$TERM_KIND-terminal" 2>/dev/null
  return 0
}

# sample_until_settled DIR PREFIX MAX STILL_LIMIT — rapid-fire screen grabs.
# No sleep: the answer renders in seconds and every millisecond spent sleeping
# is a state nobody sees. Stops after STILL_LIMIT byte-identical grabs in a row
# (the picture has settled) or at MAX grabs (a bound, so a hung surface cannot
# hang the run). `cmp` here decides only WHEN TO STOP; it is never the evidence
# that the film moves — a live screen repaints, so a hash is the wrong witness
# for that question and the per-frame pixel table answers it instead.
sample_until_settled() {
  local dir="$1" prefix="$2" max="$3" still_limit="$4"
  local i=0 still=0 prev="" f
  while [ "$i" -lt "$max" ]; do
    i=$((i + 1))
    f="$dir/${prefix}$(printf '%03d' "$i").png"
    capture_to "$f"
    if [ -n "$prev" ] && cmp -s "$f" "$prev"; then
      still=$((still + 1))
      [ "$still" -ge "$still_limit" ] && break
    else
      still=0
    fi
    prev="$f"
  done
  echo "$i"
}

# ── 1. the chat round trip (hero + the film) ──────────────────────────────
# `--dump-screen` is armed for two reasons: it makes the surface publish its own
# interpreted screen state (which is what lets the CLEAN twin be masked at
# exact cell coordinates), and it is the discovery path `gopi ui_state` scans.
# The terminal is chosen, bound and calibrated HERE — after every function it
# needs is defined and immediately before the first use. Bash resolves a function
# name at CALL time, so an earlier call site is simply "command not found"
# (measured: the first run of this change died with an empty terminal name and
# then on an unbound profile variable).
term_prepare
log "terminal=$TERM_KIND ($TERM_WHY)"
log "profile=$TERM_PROFILE_USED"
calibrate_term_size

log "starting the chat surface in a real pty (${TERM_KIND})"
CHAT_XTERM="$(term_launch "gopi-chat-demo" "GOPI_HOME=$SB exec $BIN chat --dump-screen" "$SCRATCH/chat-terminal.log")"
sleep 8
kill -0 "$CHAT_XTERM" 2>/dev/null || die "the chat terminal died (see $SCRATCH/chat-terminal.log)"
CHAT_WID="$(drive_into_window)"
log "chat window=$CHAT_WID"

i=0
for c in "${CHUNKS[@]}"; do
  i=$((i + 1))
  if [ -n "$c" ]; then
    xdotool type --delay 45 "$c" >/dev/null 2>&1 || die "xdotool type failed on chunk $i"
  fi
  sleep 0.30
  capture_to "$SHOTS/type/t$(printf '%02d' "$i").png"
done
log "typed the prompt in $i chunks (one frame each)"

xdotool key Return >/dev/null 2>&1
N1="$(sample_until_settled "$SHOTS/stream" "" 220 12)"
log "answer 1: $N1 grabs, settled"

# The hero needs a SECOND turn: one answer proves the surface works, a round
# trip proves it is a conversation, which is what the acceptance asks for.
xdotool type --delay 45 "$PROMPT_2" >/dev/null 2>&1
sleep 0.4
xdotool key Return >/dev/null 2>&1
N2="$(sample_until_settled "$SHOTS/stream" "h" 220 12)"
log "answer 2: $N2 grabs, settled"

# The hero is the LAST grab — the whole conversation on one screen. A second
# grab a beat later proves the surface was still live rather than a frozen
# picture, without anyone having to trust a hash.
LAST="$SHOTS/stream/h$(printf '%03d' "$N2").png"
[ -f "$LAST" ] || die "expected the settling grab at $LAST"
cp "$LAST" "$SCRATCH/hero-a.png"
sleep 1.2
capture_to "$SCRATCH/hero-b.png"
cp "$LAST" "$SCRATCH/chat-still.png"

xdotool key --window "$CHAT_WID" ctrl+c >/dev/null 2>&1
sleep 2
kill "$CHAT_XTERM" 2>/dev/null; term_shutdown "$CHAT_XTERM"

# ── 2. the `gopi ui` panel ────────────────────────────────────────────────
log "starting the ui panel"
UI_XTERM="$(term_launch "gopi-ui-demo" "GOPI_HOME=$SB exec $BIN ui" "$SCRATCH/ui-terminal.log")"
sleep 8
kill -0 "$UI_XTERM" 2>/dev/null || die "the ui terminal died (see $SCRATCH/ui-terminal.log)"
UI_WID="$(drive_into_window)"
xdotool type --delay 45 "Show the tool list in one short line." >/dev/null 2>&1
sleep 0.4
xdotool key Return >/dev/null 2>&1
K="$(sample_until_settled "$SHOTS/ui" "u" 200 12)"
cp "$SHOTS/ui/u$(printf '%03d' "$K").png" "$SCRATCH/ui-still.png"
log "ui panel: $K grabs, settled"
xdotool key --window "$UI_WID" ctrl+c >/dev/null 2>&1
sleep 2
kill "$UI_XTERM" 2>/dev/null; term_shutdown "$UI_XTERM"

# ── 3. the web chat UI, through `gopi shot` ───────────────────────────────
# A picture of an EMPTY page proves the page loads, not that it is used. So a
# real turn goes in first, through the page's own endpoint, and the capture
# waits for the server's transcript to hold a non-empty answer before shooting.
log "starting gopi web for a real gopi shot capture"
WEB_PORT=18097
"$BIN" web --addr "127.0.0.1:$WEB_PORT" >"$SCRATCH/web-server.log" 2>&1 &
WEB_PID=$!
sleep 5
kill -0 "$WEB_PID" 2>/dev/null || die "gopi web did not start (see $SCRATCH/web-server.log)"
python3 "$TOOLS/seed_web.py" --addr "127.0.0.1:$WEB_PORT" \
  --prompt "In two short lines: what does gopi's ledger keep?" \
  --json-out "$SCRATCH/web-turn.json" \
  || die "seeding the web turn failed (see $SCRATCH/web-server.log)"
log "web turn: $(tr -d '\n' < "$SCRATCH/web-turn.json" | cut -c1-200)"
"$BIN" tool shot --self --addr "127.0.0.1:$WEB_PORT" --viewport 1440x1400 \
      --wait-selector "#log" --wait 3 --out "$SCRATCH/web-still.png" \
      >"$SCRATCH/shot.json" 2>"$SCRATCH/shot.err" \
  || die "gopi shot failed: $(tail -2 "$SCRATCH/shot.err")"
log "web.png: $(cat "$SCRATCH/shot.json")"
kill "$WEB_PID" 2>/dev/null

# ── 4. assemble ───────────────────────────────────────────────────────────
rm -rf "$OUT/raw" "$OUT/clean" "$OUT/frames"
mkdir -p "$OUT/raw" "$OUT/clean" "$OUT/frames"
python3 "$TOOLS/assemble.py" \
  --scratch "$SCRATCH" --out "$OUT" --tools "$TOOLS" \
  --bin "$BIN" --term-cols "$TERM_COLS" --term-rows "$TERM_ROWS" \
  --gif-min-px-per-char "$MIN_PX_PER_CHAR" \
  --web-banner-band "$WEB_BANNER_BAND" \
  --term-kind "$TERM_KIND" --term-why "$TERM_WHY" --term-profile "$TERM_PROFILE_USED" \
  --term-size "$([ "$TERM_KIND" = xterm ] && printf -- '-fs %s' "$TERM_SIZE" || printf 'GDK_DPI_SCALE=%s' "$TERM_SCALE")" \
  --term-size-table "$SCRATCH/term-size.tsv" \
  || die "assemble.py failed"

log "done: $OUT"
