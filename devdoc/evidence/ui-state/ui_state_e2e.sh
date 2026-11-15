#!/usr/bin/env bash
# ui_state_e2e.sh — drive the REAL `gopi ui` under a pty and read its state with
# the NEW `gopi ui_state` verb (not with `read`/`cat`), WHILE the surface is live,
# and then once more on the SAME dump after the surface is gone.
#
#   usage:  GOPI_UI_SANDBOX=<sandbox dir> ui_state_e2e.sh /path/to/gopi
#
# WHY THE FIRST READ MUST HAPPEN WHILE THE SURFACE RUNS. The verb's whole claim
# is that it reads the CURRENT state of a LIVE surface. A dump read after the TUI
# exits is the last frame the sink installed — and bubbletea wipes its own panel
# on a clean exit — so a run that only observed after the fact would prove
# nothing about the discovery, the identity check or the ambiguity refusal, all
# of which are about a process that exists. This script reads BOTH states, in
# order, from one run: live first, then exited on the same file.
#
# NO --dump-screen PATH: the dump must land where the PRODUCT decides (the run's
# own session directory under the profile), because that layout is exactly what
# the verb's discovery exists for. Handing the surface an explicit path would
# test the reader against a location it deliberately does not scan.
#
# The sandbox comes from `gopi sandbox create` (Rule 34) — never a hand-built
# directory. Everything this run writes lives under $HERE.

set -u

HERE=$(cd "$(dirname "$0")" && pwd)
BIN=${1:?usage: ui_state_e2e.sh /path/to/gopi}
SB=${GOPI_UI_SANDBOX:?set GOPI_UI_SANDBOX to a directory from: gopi sandbox create}

STATE_OUT="$HERE/ui_state.txt"
STATE_JSON="$HERE/ui_state.json"
STATE_LIST="$HERE/ui_state_list.txt"
STATE_EXITED="$HERE/ui_state_exited.txt"
TRANSCRIPT="$HERE/pty-transcript.txt"
RUNLOG="$HERE/ui.log"
SESSIONS="$SB/.gopi/sessions"

rm -rf "$SESSIONS"
rm -f "$STATE_OUT" "$STATE_JSON" "$STATE_LIST" "$STATE_EXITED" "$TRANSCRIPT" "$RUNLOG"

ROWS=30
COLS=100
# READY_TIMEOUT is a CEILING on the wait for the first painted frame, not a
# measurement of it. MEASURED: at 100 s this run's surface had still written no
# frame into the dump (its provenance line read frames=0 dumps=0, the state the
# constructor installs), so the loop gave up and the "live" read that followed
# described a surface that had not painted yet — a diagnosis about the HARNESS,
# not about the reader. The engine's startup is what varies (it loads GOPI.md and
# brings up a server before the TUI draws), so the window has to be generous and
# the wait has to be on the ARTIFACT's own frame counter.
READY_TIMEOUT=240
KILL_AFTER=$((READY_TIMEOUT + 45))

# find_dump prints the dump the product wrote, or nothing yet.
find_dump() {
  find "$SESSIONS" -name ui-screen.dump 2>/dev/null | head -1
}

# The input side is SELF-SYNCHRONIZING and OWNED: it waits for THIS run's dump,
# then for THIS run's live read, and only then quits the TUI with its own quit
# key (`q`, a printable byte the textarea receives through the normal path — NOT
# Ctrl+C, which aborts and erases the panel; see the note at the send). The
# ownership marker is what keeps a poller from a run whose surface already ended
# out of a later run's files — measured: without it a stale poller copied one
# run's dump into another run's evidence.
{
  # STAGE 1 — KICK. MEASURED DEADLOCK this stage exists to break: bubbletea paints
  # its first frame when the program has a reason to (an event), and the engine
  # banner the transcript shows is printed BEFORE the TUI takes the terminal. A
  # poller that waits for frames>0 while the input side waits for frames>0
  # deadlocks — it did, for the full READY_TIMEOUT, twice, leaving `frames=0` for
  # a surface that was alive and had already drawn. One harmless byte breaks it:
  # the textarea receives it and the renderer fires.
  KICK_AFTER=20
  kick_waited=0
  while [ "$kick_waited" -lt "$KICK_AFTER" ]; do
    D=$(find_dump)
    if [ -n "$D" ]; then
      F=$(head -1 "$D" | sed -n 's/.*[^0-9]frames=\([0-9][0-9]*\).*/\1/p')
      [ -n "$F" ] && [ "$F" -gt 0 ] && break
    fi
    sleep 1
    kick_waited=$((kick_waited + 1))
  done
  if [ "$kick_waited" -ge "$KICK_AFTER" ]; then
    # >&2: the poller's OWN narration must not travel down the pipe into the
    # pty — measured: printing it on stdout made the terminal echo it and the
    # surface's textarea display it, so the painted grid's prompt row carried
    # the harness's words. Only the KEY BYTES belong on stdout.
    echo "poller: no frame after ${KICK_AFTER}s — sending one key to make the renderer fire" >&2
    printf ' '
  fi

  # STAGE 2 — QUIT. The quit key is sent only once the surface has PAINTED
  # (frames>0) and the live read exists, so the read above it observed a real
  # screen. Waiting merely on the dump FILE would be waiting on a frames=0
  # snapshot: the sink installs its first, empty state at construction.
  READY=0
  waited=0
  while [ "$waited" -lt "$KICK_AFTER" ]; do
    D=$(find_dump)
    if [ -n "$D" ]; then
      F=$(head -1 "$D" | sed -n 's/.*[^0-9]frames=\([0-9][0-9]*\).*/\1/p')
      if [ -n "$F" ] && [ "$F" -gt 0 ] && [ -s "$STATE_OUT" ]; then READY=1; break; fi
    fi
    sleep 1
    waited=$((waited + 1))
  done
  echo "poller: ready=$READY" >&2
  sleep 1
  # `q` is the TUI's OWN quit key. Ctrl+C must NOT be used here: bubbletea
  # treats it as an abort and ERASES the panel as it unwinds, so the sink's
  # final write is a blank grid — measured on this very script: with `\003`
  # the run ended `frames=0` and every row read empty, which is a diagnosis
  # about the quit path, not about the reader. `q` leaves through the normal
  # path the surface already paints.
  printf 'q'
  sleep 5
} | timeout "$KILL_AFTER" env \
  GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" \
  GOPI_UI_SELFTEST=1 TERM=xterm-256color \
  script -q -c "stty rows $ROWS cols $COLS; GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json GOPI_UI_SELFTEST=1 $BIN ui --addr 127.0.0.1:0 --dump-screen" /dev/null \
  >"$TRANSCRIPT" 2>&1 &
BG=$!

waited=0
frames=""
DUMP=""
while [ "$waited" -lt "$READY_TIMEOUT" ]; do
  DUMP=$(find_dump)
  if [ -n "$DUMP" ]; then
    frames=$(head -1 "$DUMP" | sed -n 's/.*[^0-9]frames=\([0-9][0-9]*\).*/\1/p')
    if [ -n "$frames" ] && [ "$frames" -gt 0 ]; then
      break
    fi
  fi
  sleep 1
  waited=$((waited + 1))
done
echo "painted after ${waited}s (frames=${frames:-unknown})"
echo "dump written by the product at: ${DUMP:-none}"
head -1 "$DUMP"

# The pid the dump itself names — never `pgrep -f`, which self-matches (Rule
# 24's sibling trap).
PID=$(head -1 "$DUMP" | sed -n 's/.*[^0-9]pid=\([0-9][0-9]*\).*/\1/p')
echo "surface pid: ${PID:-unknown}"

# The port the surface bound — read from the PRODUCT's own line, never guessed,
# because the layout assertion below quotes the header row byte-exactly.
PORT=$(head -1 "$DUMP" | sed -n 's#.*target=http://127\.0\.0\.1:\([0-9][0-9]*\).*#\1#p')
echo "surface port: ${PORT:-unknown}"

# Observation 1: the LIVE read, through the NEW verb only.
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state >"$STATE_OUT" 2>"$RUNLOG"
echo "ui_state exit: $?"
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state --json >"$STATE_JSON" 2>>"$RUNLOG"
echo "ui_state --json bytes: $(wc -c <"$STATE_JSON")"
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state --list >"$STATE_LIST" 2>>"$RUNLOG"
echo "ui_state --list exit: $?"
# The substitution guard, exercised while the surface is live: an explicit pid
# that matches nothing must refuse rather than hand back the dump it did find.
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state --pid 999991 >>"$RUNLOG" 2>&1
echo "ui_state --pid 999991 exit: $? (1 = refused, never substituted)"

# The LIVE artifact, kept beside the reads (the dump the verb just read).
cp "$DUMP" "$HERE/ui-screen.dump.live"
cp "$DUMP.json" "$HERE/ui-screen.dump.live.json"
echo "live dump kept: $(wc -c <"$HERE/ui-screen.dump.live") bytes"

wait "$BG"
echo "surface ended"

# Observation 2: the SAME dump after its writer is gone — the exited diagnosis.
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state >"$STATE_EXITED" 2>>"$RUNLOG"
echo "ui_state (after exit) exit: $?"
head -3 "$STATE_EXITED"

# The comparison that gives the LIVE grid its meaning. An empty live grid is
# ambiguous ON ITS OWN — "the surface was blank" and "the panel was erased as it
# quit" look identical — so the run lands the answer and then asks the same verb
# about the SAME file after the writer is gone: if the after-exit grid is
# byte-identical to the live one, the live read observed nothing the quit path
# does not also produce, and the run must say so instead of reporting a screen.
LIVE_BODY=$(sed -n '/^lines_begin/,$p' "$STATE_OUT" | cksum | cut -d' ' -f1)
EXIT_BODY=$(sed -n '/^lines_begin/,$p' "$STATE_EXITED" | cksum | cut -d' ' -f1)
if [ -n "$LIVE_BODY" ] && [ "$LIVE_BODY" = "$EXIT_BODY" ]; then
  echo "VERDICT: NO OBSERVATION — the live grid and the after-exit grid are identical (cksum=$LIVE_BODY); this run proves discovery/identity/refusal, NOT a painted layout"
else
  echo "VERDICT: PAINTED — the live grid differs from the after-exit grid (live=$LIVE_BODY after-exit=$EXIT_BODY): the live read caught the surface mid-flight"
fi
# The frame counter the artifact itself carries, so a report never has to infer
# paint state from the grid's emptiness. Read from the KEPT copy (the artifact
# the reads above were made against), not from the live session file: the session
# file is the surface's to rewrite, and a report must quote what it observed.
KEPT_FRAMES=$(head -1 "$HERE/ui-screen.dump.live" | sed -n 's/.*[^0-9]frames=\([0-9][0-9]*\).*/\1/p')
echo "NOTE: the kept live dump records frames=${KEPT_FRAMES:-unparsed} dumps=$(head -1 "$HERE/ui-screen.dump.live" | sed -n 's/.*[^0-9]dumps=\([0-9][0-9]*\).*/\1/p')"
if [ "${KEPT_FRAMES:-0}" -eq 0 ]; then
  echo "VERDICT: the live read ran against a dump the sink never painted (frames=0) — this run proves DISCOVERY, IDENTITY and REFUSAL, and says nothing about a painted grid"
fi

# LAYOUT ASSERTIONS, derived from the tool's OWN TEXT (the file this verb
# printed), on the artifact this run measured. Each names what a PIXEL path
# cannot say: pixels are geometry of glyphs, these are CELL indices and code
# points. Assertions run only on a painted dump — an all-blank grid would make
# every one of them pass vacuously, which is the failure this guard prevents.
if [ "${KEPT_FRAMES:-0}" -eq 0 ]; then
  echo "ASSERTIONS: SKIPPED — no painted grid to assert on (nothing is claimed about layout)"
else
  A_FAIL=0
  a() { # a <name> <what a pixel path cannot say>
    if [ "$3" = 0 ]; then echo "A$1 PASS   $2"; else echo "A$1 FAIL   $2"; A_FAIL=1; fi
  }

  # A1 — the header is row 0, byte-exact, and its last cell is column 43.
  ROW0=$(sed -n '/^grid_begin/,/^grid_end/p' "$STATE_OUT" | sed -n 's/^000|\(.*\)$/\1/p')
  want="gopi ui → http://127.0.0.1:$PORT  (tools off)"
  [ "$(printf '%s' "$ROW0" | sed 's/ *$//')" = "$want" ]; [ $? -eq 0 ] && r=0 || r=1
  a 1 "header at cell row 0 col 0..43 == \"$want\" — code points, not glyphs; a pixel row cannot name the column index" "$r"

  # A2 — the input box is row 28 and the status line row 29, i.e. the bottom two
  # of a 30-row grid REGARDLESS of transcript length: a fixed layout, which a
  # pixel path can only see as "ink near the bottom".
  R28=$(sed -n '/^grid_begin/,/^grid_end/p' "$STATE_OUT" | sed -n 's/^028|\(.*\)$/\1/p')
  R29=$(sed -n '/^grid_begin/,/^grid_end/p' "$STATE_OUT" | sed -n 's/^029|\(.*\)$/\1/p')
  case "$R28" in "you> "*) R28_OK=1;; *) R28_OK=0;; esac
  case "$R29" in "idle · turns 0"*) R29_OK=1;; *) R29_OK=0;; esac
  [ "$R28_OK" = 1 ] && [ "$R29_OK" = 1 ]; r=$?  
  a 2 "input box at row 28, status at row 29 (grid rows-2 / rows-1), transcript short — a pixel path cannot name the row index, only that ink is near the bottom" "$r"

  # A3 — CELL WIDTH: every grid row is exactly 100 cells (not 100 bytes, not 800
  # pixels). Asserted on the widest rune-bearing row, which is where a byte
  # count and a cell count diverge.
  WIDTHS=$(sed -n '/^grid_begin/,/^grid_end/p' "$STATE_OUT" | sed -n 's/^[0-9][0-9][0-9]|//p' | awk '{print length($0)}' | sort -u | tr '\n' ',')
  [ "$WIDTHS" = "100," ]; r=$?
  a 3 "every grid row is exactly 100 CELLS (observed set: ${WIDTHS%,}) — a cell count a pixel path cannot measure (the glyphs of a CJK row occupy different pixel widths for the same cell count)" "$r"

  # A4 — the geometry/mode fields are reportable at all: cursor row=29 col=0 and
  # scroll_region top=0 bottom=29 and cols=100 rows=30 and mode=ansi. A pixel
  # path has no cursor coordinate and no scroll region.
  GEO=$(grep -c -E '^cursor row=29 col=0$|^scroll_region top=0 bottom=29$' "$STATE_OUT")
  HDR=$(grep -c -E '^#gopi-screen-dump v=1 cols=100 rows=30$' "$STATE_OUT")
  [ "$GEO" -eq 2 ] && [ "$HDR" -eq 1 ]; r=$?
  a 4 "cursor row=29 col=0 + scroll_region 0..29 + cols=100 rows=30 reported as FIELDS — a pixel path can locate a caret by colour and cannot report a scroll region" "$r"

  # A5 — the 8 blind spots ride along IN THE OUTPUT, and the first sentence
  # calls this an interpreted cell grid. A pixel path carries no self-description.
  BS=$(sed -n '/^blind_spots_begin/,/^blind_spots_end/p' "$STATE_OUT" | grep -c '^00[0-7]|')
  NOTICE=$(grep -c 'interpreted cell grid (not bytes, not pixels)' "$STATE_OUT")
  [ "$BS" -eq 8 ] && [ "$NOTICE" -ge 1 ]; r=$?
  a 5 "8 blind spots present in the tool's own output (count=$BS) and the notice names an interpreted CELL GRID — a pixel path cannot state what it is blind to" "$r"

  echo "LAYOUT ASSERTIONS: tested=5 failed=$A_FAIL"
fi
