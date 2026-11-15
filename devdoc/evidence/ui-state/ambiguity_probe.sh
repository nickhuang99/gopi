#!/usr/bin/env bash
# ambiguity_probe.sh — put TWO live `gopi ui` surfaces in ONE profile and ask
# the verb for the screen. The verb's contract is a REFUSAL (exit 2) that LISTS
# both candidates, never a silent pick: two live surfaces are two screens, and
# an agent that got one of them without being told would be reading the wrong
# screen and would not know it.
#
#   usage:  GOPI_UI_SANDBOX=<sandbox dir> ambiguity_probe.sh /path/to/gopi
#
# Why a REAL pair of surfaces: the refusal is about the DISPATCH decision in the
# presence of real dumps and real processes, and a unit test's two Candidates
# are structs, not two running TUIs.
#
# The surfaces are started exactly as the E2E harness starts one (a pty from
# `script`, the product's own --dump-screen, the sandbox profile) and are fed a
# quit key at the end; no Ctrl+C (it aborts and erases the panel).

set -u

HERE=$(cd "$(dirname "$0")" && pwd)
BIN=${1:?usage: ambiguity_probe.sh /path/to/gopi}
SB=${GOPI_UI_SANDBOX:?set GOPI_UI_SANDBOX to a directory from: gopi sandbox create}

OUT="$HERE/ambiguity.out"
REFUSE_TXT="$HERE/ui_state_ambiguous.txt"
REFUSE_JSON="$HERE/ui_state_ambiguous.json"
LIST="$HERE/ui_state_ambiguous_list.txt"

SESSIONS="$SB/.gopi/sessions"
rm -rf "$SESSIONS"
rm -f "$OUT" "$REFUSE_TXT" "$REFUSE_JSON" "$LIST"

ROWS=30
COLS=100
RUN_SECS=70
READY_SECS=45

start_surface() { # start_surface <tag>
  local tag=$1
  sleep "$RUN_SECS" | timeout "$((RUN_SECS + 10))" env \
    GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" \
    GOPI_UI_SELFTEST=1 TERM=xterm-256color \
    script -q -c "stty rows $ROWS cols $COLS; GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json GOPI_UI_SELFTEST=1 $BIN ui --addr 127.0.0.1:0 --dump-screen" /dev/null \
    >"$HERE/ambiguity-$tag.transcript" 2>&1 &
}

start_surface a
start_surface b

# Wait on the ARTIFACTS, not on a fixed sleep: two dumps in the sessions tree is
# the precondition the refusal is about.
waited=0
while [ "$waited" -lt "$READY_SECS" ]; do
  n=$(find "$SESSIONS" -name ui-screen.dump 2>/dev/null | wc -l)
  [ "$n" -ge 2 ] && break
  sleep 1
  waited=$((waited + 1))
done
echo "surfaces with a dump after ${waited}s: $(find "$SESSIONS" -name ui-screen.dump 2>/dev/null | wc -l)" | tee -a "$OUT"

GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state >"$REFUSE_TXT" 2>&1
echo "ui_state (two live surfaces) exit: $?" | tee -a "$OUT"
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state --json >"$REFUSE_JSON" 2>&1
echo "ui_state --json exit: $?" | tee -a "$OUT"
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" "$BIN" ui_state --list >"$LIST" 2>&1
echo "ui_state --list exit: $? (the refusal's own escape hatch: --list must still answer)" | tee -a "$OUT"

head -20 "$REFUSE_TXT" | tee -a "$OUT"

# The refusal must NAME both candidates (the pids the dumps carry) — a refusal
# that says "ambiguous" without saying BETWEEN WHAT is not usable.
PIDS=$(find "$SESSIONS" -name ui-screen.dump -exec head -1 {} \; | sed -n 's/.*[^0-9]pid=\([0-9][0-9]*\).*/\1/p' | sort -u)
echo "dumps' own pids: $(echo "$PIDS" | tr '\n' ' ')" | tee -a "$OUT"
MISSING=0
for p in $PIDS; do
  grep -q "pid=$p\|$p" "$REFUSE_TXT" || { echo "REFUSAL DID NOT NAME pid=$p" | tee -a "$OUT"; MISSING=1; }
done
if [ "$MISSING" = 0 ]; then
  echo "VERDICT: REFUSED-AND-LISTED — exit 2 and both live candidates named" | tee -a "$OUT"
else
  echo "VERDICT: REFUSAL INCOMPLETE — a live candidate went unnamed" | tee -a "$OUT"
fi

wait
echo "surfaces ended" | tee -a "$OUT"
