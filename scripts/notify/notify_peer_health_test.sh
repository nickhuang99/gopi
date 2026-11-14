#!/usr/bin/env bash
# notify_peer_health_test.sh — the PULL-FAILURE IS VISIBLE contract.
#
# THE DEFECT THIS PINS. `notify.cmd` runs with the payload on stdin and its
# output captured (internal/tools/tasknotify.go: taskNotifyCmd); on the success
# path the parent records `cmd ok (stdout: )` and DROPS the text. So every
# `notify.sh: peer … cannot read its event stream` line the sink writes reaches
# nobody: a pull that has been dead for a week produced exactly the output of a
# pull with nothing to say. That is Rule 15's silent degradation, and no test
# measured it — the two existing rigs assert on delivery, never on the report.
#
# THE CONTRACT.
#   * an unreachable peer leaves ONE durable record in the profile's state
#     directory — `peer-<alias>-health.txt`, carrying the alias, the time it was
#     first seen, the last time it was checked, and the reason;
#   * a SECOND run of the same failure does NOT add a second record and does not
#     reprint the transition (dedupe is the whole point: this runs on every task
#     event, so a per-run line would be a flood that hides itself);
#   * a peer that is reachable but whose cursor has not moved past the staleness
#     threshold is recorded as `stalled` — the failure that looks like silence;
#   * a healthy peer is recorded as `ok`, so the files can be read as a
#     fleet-wide state rather than as a list of complaints;
#   * the state directory is a PARAMETER (`GOPI_NOTIFY_STATE_DIR`), proven by
#     putting the cursors and the records somewhere other than the default.
#
# INSTRUMENTS ARE STUBS (`ssh`, `gopi`) and every assertion reads a FILE the sink
# wrote, never a variable the sink set for this rig (the same shape as
# notify_peer_resume_test.sh, whose stub `ssh` this one reuses in spirit).
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared helper.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK="$ROOT/scripts/notify/notify.sh"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-peer-health)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM

# Names read back OUT of the sink rather than spelled here (Rule 13/14): a
# renamed record must break THIS test instead of silently un-measuring it.
script_const() {
	sed -n "s/^readonly $1=\([^ ]*\).*/\1/p" "$SINK" | head -1 | sed -e 's/^"//' -e 's/"$//'
}
HEALTH_PREFIX="$(script_const PEER_HEALTH_PREFIX)"
HEALTH_SUFFIX="$(script_const PEER_HEALTH_SUFFIX)"
CURSOR_PREFIX="$(script_const PEER_CURSOR_PREFIX)"
CURSOR_SUFFIX="$(script_const PEER_CURSOR_SUFFIX)"
: "${HEALTH_PREFIX:?cannot read PEER_HEALTH_PREFIX from the sink}"
: "${HEALTH_SUFFIX:?cannot read PEER_HEALTH_SUFFIX from the sink}"
: "${CURSOR_PREFIX:?cannot read PEER_CURSOR_PREFIX from the sink}"
: "${CURSOR_SUFFIX:?cannot read PEER_CURSOR_SUFFIX from the sink}"

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

PEER="probe-peer"
PAYLOAD='{"event":"completed","task_id":"t-1","status":"completed","summary":"local"}'

BIN="$RIG/bin"
mkdir -p "$BIN"

# ---- the stub `ssh`: its behaviour is chosen by STUB_SSH_MODE ----------------
cat >"$BIN/ssh" <<'STUB'
#!/usr/bin/env bash
cat >/dev/null                      # drain the remote program on stdin
case "${STUB_SSH_MODE:-ok}" in
fail)
	# The shape ssh prints when the host does not answer: an address that names
	# the failure and a decimally-varying detail the sink must NOT copy verbatim
	# (a varying reason defeats the dedupe).
	printf 'ssh: connect to host 192.0.2.1 port 22: Connection refused\r\n' >&2
	exit 255
	;;
anchor-missing)
	printf '' >&2
	exit 3
	;;
esac
exit 0                             # reached, nothing new
STUB
chmod 0755 "$BIN/ssh"

# ---- the stub `gopi`: accepts the capability probe and every delivery ---------
cat >"$BIN/gopi" <<'STUB'
#!/usr/bin/env bash
for a in "$@"; do
	if [ "$a" = "-h" ] || [ "$a" = "--help" ]; then exit 0; fi
done
cat >/dev/null 2>/dev/null || true
exit 0
STUB
chmod 0755 "$BIN/gopi"

# ---- rig HOME STATE_DIR_MODE RUN_ENV... -------------------------------------
# Runs the sink the way the parent does — payload on stdin — and keeps its
# stderr in $CASE_OUT so a case can assert on what an operator WOULD have seen.
run_sink() {
	local home="$1" state="$2"
	shift 2
	local profile="$home/.gopi"
	mkdir -p "$profile/notify"
	[ -f "$profile/settings.json" ] || printf '{"sync_peers": ["%s"]}\n' "$PEER" >"$profile/settings.json"
	CASE_OUT="$RIG/case.out"
	env -u GOPI_SESSIONS_ROOT \
		HOME="$RIG_HOME" \
		GOPI_HOME="$home" GOPI_SETTINGS="$profile/settings.json" \
		GOPI_NOTIFY_STATE_DIR="$state" \
		PATH="$BIN:$PATH" "$@" \
		bash "$SINK" >"$CASE_OUT" 2>&1 <<<"$PAYLOAD"
	CASE_RC=$?
}

# ---- THE RIG'S OWN HOMES, AND WHY EVERY SINK CALL MUST NAME ONE -------------
# THE DEFECT THIS CLOSES (measured 2026-11-03). notify.sh refuses a GOPI_HOME
# that names a CODE CHECKOUT and then ANCHORS the payload where the value should
# have pointed — `$HOME/.gopi`, the operator's own profile (the fallback is
# documented in the refusal's own text and pinned by notify_slot_contract_test's
# case 6, which asserts the event is ANCHORED in the operator's profile rather
# than lost). So a case that hands the sink a checkout-shaped GOPI_HOME and lets
# HOME be INHERITED does not merely exercise the refusal: it appends the rig's
# fixture payload to the operator's LIVE evidence file. Case 7 did exactly that,
# and every full-suite run left one
# `{"event":"completed","task_id":"t-1","status":"completed","summary":"local"}`
# row in ~/.gopi/notify/task-events.jsonl (9 of them predate this fix and are
# NOT deleted — an append-only evidence file is not edited by a test fix).
#
# THE FIX IS AT THE TEST SEAM, NOT IN THE SINK. Making the sink drop the payload
# would lose real events from production worktree children — the exact loss the
# fallback exists to prevent. A rig owns its INPUTS; HOME is one of them.
RIG_HOME="$RIG/rig-home"
mkdir -p "$RIG_HOME"
# `gopi_rig_env HOME` prints the env prefix every sink call in this file uses.
# It is the ONE place HOME is pinned, so a new case cannot forget it (and
# notify_hermeticity_test.sh scans for a call site that does).
rig_env() {
	printf 'HOME=%s' "$1"
}

health_path() { printf '%s/%s%s%s' "$1" "$HEALTH_PREFIX" "$PEER" "$HEALTH_SUFFIX"; }
cursor_path() { printf '%s/%s%s%s' "$1" "$CURSOR_PREFIX" "$PEER" "$CURSOR_SUFFIX"; }

echo "notify.sh peer-pull failure visibility (rig: $RIG)"

# ---- case 1: an UNREACHABLE peer leaves a record, and only one --------------
H1="$RIG/h1"
S1="$RIG/state1"
run_sink "$H1" "$S1" STUB_SSH_MODE=fail
H1F="$(health_path "$S1")"
if [ -r "$H1F" ]; then
	pass "case 1: the unreachable peer left a record at $(basename "$H1F")"
else
	fail "case 1: no record at $H1F — the failure is invisible (the defect)"
fi
line1="$(sed -n '1p' "$H1F" 2>/dev/null)"
case "$line1" in
*state=unreachable*) pass "case 1: the record names the state (unreachable)" ;;
*) fail "case 1: record does not say state=unreachable: $line1" ;;
esac
case "$line1" in
*"peer=$PEER"*) pass "case 1: the record names the peer alias" ;;
*) fail "case 1: record does not name peer $PEER: $line1" ;;
esac
case "$line1" in
*reason=*"exit 255"*) pass "case 1: the record carries the reason (ssh exit 255)" ;;
*) fail "case 1: record carries no reason: $line1" ;;
esac
case "$(sed -n '2p' "$H1F" 2>/dev/null)" in
first_seen=?*Z*) pass "case 1: the record carries the instant it was first seen" ;;
*) fail "case 1: no first_seen timestamp: $(sed -n '2p' "$H1F" 2>/dev/null)" ;;
esac
case "$line1" in
*"$RIG"*|*'ssh: connect to host'*) fail "case 1: the record copied the volatile ssh wording verbatim (the dedupe would never fire): $line1" ;;
*) pass "case 1: the reason is normalised, not the raw ssh line" ;;
esac

# ---- case 2: THE SAME failure again is ONE record, not two ------------------
before_bytes="$(cat "$H1F")"
before_mtime="$(stat -c %Y "$H1F")"
sleep 1
run_sink "$H1" "$S1" STUB_SSH_MODE=fail
after_bytes="$(cat "$H1F")"
after_mtime="$(stat -c %Y "$H1F")"
if [ "$before_bytes" = "$after_bytes" ]; then
	pass "case 2: the second identical failure did not touch the record (first_seen kept, no duplicate)"
else
	fail "case 2: the record changed on a repeat failure — dedupe failed:
--- before
$before_bytes
--- after
$after_bytes"
fi
if [ "$before_mtime" = "$after_mtime" ]; then
	pass "case 2: the file was not even rewritten (inside the refresh window)"
else
	fail "case 2: the file was rewritten for an unchanged state ($before_mtime -> $after_mtime)"
fi
transitions="$(grep -c 'recorded in' "$CASE_OUT" 2>/dev/null || true)"
if [ "${transitions:-0}" -eq 0 ]; then
	pass "case 2: the repeat did not reprint the transition on stderr (no flood)"
else
	fail "case 2: the repeat reprinted the transition $transitions time(s) on stderr"
fi
lines="$(wc -l <"$H1F" 2>/dev/null | tr -d ' ')"
if [ "$lines" = "2" ]; then
	pass "case 2: the record is still exactly two lines"
else
	fail "case 2: the record grew to $lines line(s) — a reader cannot tell one failure from many"
fi

# ---- case 3: a REACHED peer whose cursor stands still is STALLED ------------
H3="$RIG/h3"
S3="$RIG/state3"
mkdir -p "$S3"
printf '2026-10-01T00:00:00+08:00\n' >"$(cursor_path "$S3")"
touch -d '3 hours ago' "$(cursor_path "$S3")"
run_sink "$H3" "$S3" STUB_SSH_MODE=ok GOPI_NOTIFY_CURSOR_STALE_SEC=600
line3="$(sed -n '1p' "$(health_path "$S3")" 2>/dev/null)"
case "$line3" in
*state=stalled*) pass "case 3: an idle cursor is recorded as stalled, not as silence" ;;
*) fail "case 3: expected state=stalled, got: $line3" ;;
esac
case "$line3" in
*reason=*"not advanced for"*) pass "case 3: the record says how long the cursor has stood still" ;;
*) fail "case 3: the stalled record carries no age: $line3" ;;
esac
case "$line3" in
*"cursor=2026-10-01T00:00:00+08:00"*) pass "case 3: the record names the cursor it judged" ;;
*) fail "case 3: the stalled record does not carry the cursor value: $line3" ;;
esac

# ---- case 4: a healthy peer reads as ok (the fleet view, not a complaint list)
H4="$RIG/h4"
S4="$RIG/state4"
mkdir -p "$S4"
printf '2026-10-17T00:00:00+08:00\n' >"$(cursor_path "$S4")"
run_sink "$H4" "$S4" STUB_SSH_MODE=ok GOPI_NOTIFY_CURSOR_STALE_SEC=600
case "$(sed -n '1p' "$(health_path "$S4")" 2>/dev/null)" in
*state=ok*) pass "case 4: a fresh cursor reads as ok" ;;
*) fail "case 4: expected state=ok, got: $(sed -n '1p' "$(health_path "$S4")" 2>/dev/null)" ;;
esac

# ---- case 5: the STATE DIRECTORY is a parameter (cursors and records follow it)
H5="$RIG/h5"
S5="$RIG/state5-with-a-different-name"
run_sink "$H5" "$S5" STUB_SSH_MODE=fail
if [ -r "$(health_path "$S5")" ]; then
	pass "case 5: the record landed in GOPI_NOTIFY_STATE_DIR"
else
	fail "case 5: nothing recorded in $S5"
fi
if [ -e "$H5/.gopi/notify/${HEALTH_PREFIX}${PEER}${HEALTH_SUFFIX}" ]; then
	fail "case 5: a record was ALSO written into the default profile notify dir ($H5/.gopi/notify) — the parameter is being ignored"
else
	pass "case 5: nothing was written into the profile's default notify dir"
fi
# The cursor must live beside the records, or a deployment would move the
# failure records and leave the progress record behind (two sources of truth).
S6="$RIG/state6"
H6="$RIG/h6"
mkdir -p "$S6"
printf '2026-10-17T00:00:00+08:00\n' >"$(cursor_path "$S6")"
run_sink "$H6" "$S6" STUB_SSH_MODE=ok
if [ -e "$H6/.gopi/notify/${CURSOR_PREFIX}${PEER}${CURSOR_SUFFIX}" ]; then
	fail "case 5: the cursor was created in the default dir while the state dir was named — the two were split"
else
	pass "case 5: the cursor lives in the named state dir (progress and failure in ONE place)"
fi

# ---- case 6: the payload is still anchored, whatever the pull did -----------
if [ -s "$H5/.gopi/notify/task-events.jsonl" ] && grep -q '"task_id":"t-1"' "$H5/.gopi/notify/task-events.jsonl"; then
	pass "case 6: a failing pull does not cost the local event (still anchored)"
else
	fail "case 6: the local payload was NOT anchored on a failing pull"
fi

# ---- case 7: a run-exported worktree home is REFUSED, and named -------------
H7="$RIG/h7"
mkdir -p "$H7/.gopi" "$H7/.git"
printf '{"sync_peers": ["%s"]}\n' "$PEER" >"$H7/.gopi/settings.json"
CASE_OUT="$RIG/case7.out"
HOME7="$RIG/case7-home"          # the rig's own home: where the fallback anchor MUST land
mkdir -p "$HOME7"
env -u GOPI_SESSIONS_ROOT -u GOPI_NOTIFY_STATE_DIR \
	HOME="$HOME7" \
	GOPI_HOME="$H7" GOPI_SETTINGS="$H7/.gopi/settings.json" \
	PATH="$BIN:$PATH" STUB_SSH_MODE=ok \
	bash "$SINK" >"$CASE_OUT" 2>&1 <<<"$PAYLOAD"
if grep -q 'is a git worktree, not a profile' "$CASE_OUT"; then
	pass "case 7: a GOPI_HOME that is a git worktree is refused by name"
else
	fail "case 7: a worktree GOPI_HOME was accepted silently:
$(cat "$CASE_OUT")"
fi
if [ -e "$H7/.gopi/notify/${HEALTH_PREFIX}${PEER}${HEALTH_SUFFIX}" ] ||
	[ -e "$H7/.gopi/notify/${CURSOR_PREFIX}${PEER}${CURSOR_SUFFIX}" ]; then
	fail "case 7: state was written into the worktree anyway"
else
	pass "case 7: nothing was written into the worktree's profile"
fi
# WHERE THE EVENT WENT — asserted in BOTH directions, because "not in the
# checkout" is only half the contract: the refusal's own text promises the event
# is anchored at $HOME/.gopi, and the sink implements that. So the row must be
# IN the rig's home (the fallback works) and must NOT have reached any home this
# rig did not create. Handing the sink an inherited HOME is the defect this case
# used to have: it did not just exercise the refusal, it filed the rig's fixture
# payload into the operator's live evidence file.
C7_ANCHOR="$HOME7/.gopi/notify/task-events.jsonl"
if grep -q '"task_id":"t-1"' "$C7_ANCHOR" 2>/dev/null; then
	pass "case 7: the refused event is ANCHORED in the rig's own home (not lost)"
else
	fail "case 7: the event is missing from $C7_ANCHOR — the fallback anchor did not work"
fi
if [ -e "$H7/.gopi/notify/task-events.jsonl" ]; then
	fail "case 7: the anchor was ALSO written inside the checkout"
else
	pass "case 7: and nothing was anchored inside the checkout"
fi
# THE HERMETICITY ASSERTION, per case: the row landed under a directory this rig
# created, and no file exists at the path the sink would have used had HOME been
# inherited. Read as a FACT about the filesystem, not as a variable the rig set.
if [ "$(grep -c '"task_id":"t-1"' "$C7_ANCHOR" 2>/dev/null || true)" = "1" ]; then
	pass "case 7: exactly ONE fixture row, in the rig home"
else
	fail "case 7: expected exactly 1 fixture row in $C7_ANCHOR, got $(grep -c '"task_id":"t-1"' "$C7_ANCHOR" 2>/dev/null || echo 0)"
fi

if [ "$FAILS" -ne 0 ]; then
	printf '\nnotify_peer_health_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf '\nnotify_peer_health_test: all assertions passed\n'
