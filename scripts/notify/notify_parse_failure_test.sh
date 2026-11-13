#!/usr/bin/env bash
# notify_parse_failure_test.sh — an event whose fields cannot be read must SAY
# SO and carry its raw line, never render as `event=? task=? status=?`.
#
# THE DEFECT THIS PINS (task 01a1a640, called "B" in that row's briefing).
# `build_message` rendered a payload from which `field()` could extract nothing
# as
#
#     task event: event=? task=? status=?
#
# — three question marks naming no event, no task and no status. Measured on
# this host 2026-11-04 against the baseline sink, with a stub `gopi` capturing
# the `--message` operand:
#
#     $ printf '{}' | bash scripts/notify/notify.sh      # valid JSON, no fields
#     task event: event=? task=? status=?
#     $ bash scripts/notify/notify.sh < /dev/null        # no bytes at all
#     task event: event=? task=? status=?
#     $ printf 'this is not json' | bash ...              # not JSON
#     task event: event=? task=? status=?
#
# A reader could not tell "the payload's fields were empty" from "the sink's
# extractor is broken", and the message carried nothing to grep. So the
# question-mark placeholder is not merely ugly: it is a SILENT failure wearing
# the shape of data (Rule 15), and it launders a producer bug into something
# that looks like an ordinary event.
#
# WHAT THE FIXED SINK MUST DO, and what each case below asserts:
#
#   * a payload that names an event renders it, byte for byte as before
#     (cases 1 and 5 — the ordinary path must not regress);
#   * a payload that names NOTHING renders the parse-failure wording, NAMES the
#     fields it could not read, and carries the raw line (cases 2, 3, 4, 6);
#   * `event=? task=? status=?` appears NOWHERE in any of them (all cases);
#   * a payload with no bytes at all adds NO BLANK LINE to the append-only
#     anchor (case 4) — three such lines are already in the live anchor on this
#     host (lines 361, 6043, 6044 of ~/.gopi/notify/task-events.jsonl), which
#     is exactly the fingerprint of this defect, and appending a fourth would
#     add ambiguity to an evidence file that is never rewritten.
#
# WHY A SHELL TEST AND NOT A GO TEST. The defect lives in the sink's control
# flow — which bytes reach `gopi notify --message` — so the only instrument that
# can see it is the sink itself, run end to end with a stub `gopi` that records
# the message operand verbatim. A Go test re-implementing `build_message` would
# assert on its own copy of the rule, which is the drift the cross-language pins
# in this repo exist to prevent (see notify_field_decode_test.sh's header).
#
# THE RED PROOF IS PART OF THE TEST, not a note about a run somebody did once.
# `--red-proof` runs the SAME assertions against the sink as it was at the
# branch's cut point (`git show "$BASE_REV:$SINK_REL"`, default 3259f17) and
# REQUIRES them to fail: a green here would mean the assertions do not actually
# see the defect, which is the "vacuum" failure mode a fix's own test is most
# likely to have. Run without arguments for the green (post-fix) arm.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared
# helper — no /tmp, no hardcoded path.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK_REL="scripts/notify/notify.sh"
SINK="$ROOT/$SINK_REL"
# The baseline the red proof is measured against: this task's cut point, which
# is the tree the defect was reported on.
readonly BASE_REV_DEFAULT="3259f17"
BASE_REV="${BASE_REV:-$BASE_REV_DEFAULT}"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-parse-failure)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# THE RIG'S OWN HOME (measured 2026-11-03). notify.sh's refusal of a
# checkout-shaped GOPI_HOME ANCHORS the payload at $HOME/.gopi — the documented
# fallback, pinned by notify_slot_contract_test's case 6. So a sink call that
# pins GOPI_HOME but leaves HOME INHERITED files the rig's fixture payload into
# the OPERATOR's live evidence file (/home/nick/.gopi/notify/task-events.jsonl). Every
# sink invocation in this file therefore names $RIG_HOME; notify_hermeticity_test.sh
# scans for a call site that does not.
RIG_HOME="$RIG/rig-home"
mkdir -p "$RIG_HOME"


SLOT_PREFIX="chat_session_id_"
SLOT_SUFFIX=".txt"

# The placeholder the defect printed, and the wording the fix must print
# instead. Both are spelled ONCE here so a case can compare against them
# without typing either.
readonly DEFECT_PLACEHOLDER="event=? task=? status=?"
readonly PARSE_FAILURE_WORD="PARSE FAILURE"

MODE="green"
case "${1:-}" in
	--red-proof) MODE="red" ;;
	"") ;;
	*) printf 'usage: %s [--red-proof]\n' "$0" >&2; exit 2 ;;
esac

if [ "$MODE" = "red" ]; then
	# The defect's version of the sink, materialised OUT of the object store —
	# never a second checkout (Rule 29).
	SINK="$RIG/baseline-notify.sh"
	if ! git -C "$ROOT" show "$BASE_REV:$SINK_REL" >"$SINK" 2>"$RIG/git.err"; then
		printf 'red-proof: cannot read %s:%s out of the object store:\n' "$BASE_REV" "$SINK_REL" >&2
		cat "$RIG/git.err" >&2
		exit 2
	fi
fi

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

# ---- the stub `gopi` --------------------------------------------------------
# It records the MESSAGE operand verbatim and succeeds — the shape of a live
# chat taking a message. APPEND, never truncate: the sink also calls this for
# its two capability probes, and a truncating stub would erase the delivery it
# exists to record.
STUB_DIR="$RIG/bin"
mkdir -p "$STUB_DIR"
cat >"$STUB_DIR/gopi" <<'STUB'
#!/usr/bin/env bash
case "$1${2:+ $2}" in
	"notify --session")
		printf 'message=%s\n' "$5" >>"${STUBLOG:?STUBLOG unset}"
		;;
esac
cat >/dev/null 2>&1
exit 0
STUB
chmod 0755 "$STUB_DIR/gopi"

# run_sink PAYLOAD_FILE — feed one payload, capture the rendered message.
# An empty PAYLOAD_FILE name means "the producer sent no bytes at all": stdin
# is /dev/null, which is the shape a child exec'd with no input has.
RUN_SEQ=0
run_sink() {
	RUN_SEQ=$((RUN_SEQ + 1))
	local stub="$RIG/run${RUN_SEQ}.stub" err="$RIG/run${RUN_SEQ}.err"
	: >"$stub"
	: >"$err"
	# GOPI_SETTINGS pins a rig file with no `sync_peers`: the subject is the
	# message builder, and an ssh leg to a real peer would make the verdict
	# depend on two other machines.
	if [ -n "$1" ]; then
		STUBLOG="$stub" HOME="$HOME_RIG" GOPI_HOME="$HOME_RIG" GOPI_SETTINGS="$RIG/settings.json" \
			PATH="$STUB_DIR:$PATH" bash "$SINK" <"$1" 2>"$err"
	else
		STUBLOG="$stub" HOME="$HOME_RIG" GOPI_HOME="$HOME_RIG" GOPI_SETTINGS="$RIG/settings.json" \
			PATH="$STUB_DIR:$PATH" bash "$SINK" </dev/null 2>"$err"
	fi
	RUN_RC=$?
	RUN_MSG="$(sed -n 's/^message=//p' "$stub" | tail -n 1)"
	RUN_STUB="$stub"
	RUN_ERR="$err"
	RUN_ANCHOR="$HOME_RIG/.gopi/notify/task-events.jsonl"
}

# assert_named WHAT MESSAGE — the value gives the operator something to act on.
assert_named() {
	case "$RUN_MSG" in
		*"$1"*) pass "$2" ;;
		*) fail "$2 — the message does not name \"$1\": $RUN_MSG" ;;
	esac
}
# assert_never_placeholder CASE — the ONE assertion every case carries.
assert_never_placeholder() {
	case "$RUN_MSG" in
		*"$DEFECT_PLACEHOLDER"*)
			fail "$1: the defect's placeholder is back verbatim ($DEFECT_PLACEHOLDER): $RUN_MSG" ;;
		*'=?'*)
			fail "$1: a field rendered as a bare '?' placeholder: $RUN_MSG" ;;
		*) pass "$1: no '?' placeholder in any field" ;;
	esac
}

# A profile whose slot registry holds one live session directory, so every
# payload below is actually DELIVERED (a refused/absent slot would leave
# RUN_MSG empty and every assertion would pass for the wrong reason).
HOME_RIG="$RIG/home"
LIVE_SESSION="$HOME_RIG/.gopi/sessions/66666666-0000-4000-8000-000000000001"
mkdir -p "$LIVE_SESSION" "$HOME_RIG/.gopi"
printf '434343:%s\n' "$LIVE_SESSION" >"$HOME_RIG/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
printf '{}' >"$RIG/settings.json"

# ---- 1. THE REPORTED EVENT: t-1 must name its task and its event ----------
# The exact bytes the operator's notification was about. This is the CONTROL
# arm: the fix must not change a payload that was always readable.
T1="$RIG/t1.json"
printf '%s' '{"event":"completed","task_id":"t-1","status":"completed","summary":"local"}' >"$T1"
run_sink "$T1"
if [ "$RUN_RC" -ne 0 ]; then fail "case 1: sink exited $RUN_RC"; fi
assert_named "task=t-1" "case 1: the t-1 event names its task id"
assert_named "event=completed" "case 1: the t-1 event names its event"
assert_named "status=completed" "case 1: the t-1 event names its status"
assert_never_placeholder "case 1"

# ---- 2. VALID JSON, NO READABLE FIELD: {} ---------------------------------
# The hardest shape for the old code and the reason the placeholder was wrong:
# the payload PARSES. Nothing is malformed about `{}`; it simply names nothing,
# and a message built out of it says nothing either. The fixed sink states the
# failure and carries the raw line, which is `{}` — the only fact there is.
BRACE="$RIG/brace.json"
printf '{}' >"$BRACE"
run_sink "$BRACE"
assert_named "$PARSE_FAILURE_WORD" "case 2: {} is reported as a parse failure, not as an event"
assert_named "raw line: {}" "case 2: the raw line rides the message verbatim"
assert_named "task_id" "case 2: the message NAMES a field it could not read"
assert_never_placeholder "case 2"

# ---- 3. NOT JSON AT ALL ---------------------------------------------------
# The producer never writes this, so it is by definition a torn or foreign
# write. It must be visible AS such, with its bytes, rather than be swallowed.
BAD="$RIG/bad.json"
printf 'this is not json' >"$BAD"
run_sink "$BAD"
assert_named "$PARSE_FAILURE_WORD" "case 3: non-JSON is reported as a parse failure"
assert_named "raw line: this is not json" "case 3: and its raw bytes ride the message"
assert_never_placeholder "case 3"

# ---- 4. NO BYTES AT ALL: the sink says so, and the ANCHOR gains no blank ---
# Two assertions, and the second is the data-integrity half: an append-only
# evidence file must not collect a row that carries no fact.
ANCHOR_BEFORE=0
[ -f "$RUN_ANCHOR" ] && ANCHOR_BEFORE="$(wc -l <"$RUN_ANCHOR")"
run_sink ""
if [ "$RUN_RC" -ne 0 ]; then fail "case 4: sink exited $RUN_RC on empty stdin"; fi
assert_named "$PARSE_FAILURE_WORD" "case 4: an empty payload is reported as a parse failure"
assert_named "empty" "case 4: and says in words that no bytes arrived"
assert_never_placeholder "case 4"
ANCHOR_AFTER=0
[ -f "$RUN_ANCHOR" ] && ANCHOR_AFTER="$(wc -l <"$RUN_ANCHOR")"
if [ "$ANCHOR_AFTER" -eq "$(($ANCHOR_BEFORE + 1))" ]; then
	# The row may legitimately have grown by one IF that one row is not blank.
	LAST_ROW="$(tail -n 1 "$RUN_ANCHOR")"
	if [ -n "$LAST_ROW" ]; then
		fail "case 4: an empty payload was anchored as a row: $LAST_ROW"
	else
		fail "case 4: an empty payload was anchored as a BLANK line (the defect's fingerprint)"
	fi
elif [ "$ANCHOR_AFTER" -eq "$ANCHOR_BEFORE" ]; then
	pass "case 4: nothing was appended to the anchor for an empty payload"
else
	fail "case 4: the anchor grew by $((ANCHOR_AFTER - ANCHOR_BEFORE)) lines for one empty payload"
fi
if grep -qF "payload on stdin was EMPTY" "$RUN_ERR" 2>/dev/null; then
	pass "case 4: the refusal to anchor is REPORTED on stderr, not silent (Rule 15)"
else
	fail "case 4: the sink declined to anchor and said nothing about it"
fi

# ---- 5. A REAL LIFECYCLE EVENT STILL RENDERS ------------------------------
# A genuine producer payload — the shape internal/tools/tasknotify.go writes:
# every field present, HTML-escaped characters in the summary. The fix is
# about what happens when fields are ABSENT, so a full payload must be
# untouched, escapes decoded exactly once (notify_field_decode_test.sh pins
# the decoder; this case pins that the fix did not wrap it in a failure path).
REAL="$RIG/real.json"
printf '%s' '{"event":"failed","previous_status":"in_progress","task_id":"01a1a640-1c0e-707e-a4d2-73df25654559","status":"failed","summary":"x >= 3 && y < 4","timestamp":"2026-11-04T20:00:00+08:00"}' >"$REAL"
run_sink "$REAL"
assert_named "task=01a1a640-1c0e-707e-a4d2-73df25654559" "case 5: a real 01a1… event renders its task id"
assert_named "event=failed" "case 5: and its event name"
assert_named "x >= 3 && y < 4" "case 5: and its summary, decoded once"
assert_never_placeholder "case 5"

# ---- 6. PARTIAL: a payload that names SOME fields -------------------------
# The wording must name ONLY what is missing — a message that cried "parse
# failure" over a payload carrying a perfectly good event would be its own kind
# of lie, and would make the operator distrust the whole channel.
PARTIAL="$RIG/partial.json"
printf '%s' '{"event":"claimed"}' >"$PARTIAL"
run_sink "$PARTIAL"
assert_named "$PARSE_FAILURE_WORD" "case 6: a half-named payload is reported"
assert_named "task_id" "case 6: the missing task_id is named"
assert_named "status" "case 6: the missing status is named"
case "$RUN_MSG" in
	*"no readable event"*) fail "case 6: it names 'event' as missing, but the payload HAS one: $RUN_MSG" ;;
	*) pass "case 6: the field that IS present is not reported missing" ;;
esac
assert_named "raw line: {\"event\":\"claimed\"}" "case 6: and the raw line rides it"
assert_never_placeholder "case 6"

# ---- verdict ---------------------------------------------------------------
printf '\n'
if [ "$MODE" = "red" ]; then
	# The RED arm inverts the verdict: the baseline MUST fail these assertions.
	if [ "$FAILS" -eq 0 ]; then
		printf 'red-proof: the BASELINE (%s) passed every assertion — this test cannot see the defect it exists for\n' \
			"$BASE_REV" >&2
		exit 1
	fi
	printf 'red-proof: the baseline (%s) fails %d assertion(s) — the test observes the defect\n' "$BASE_REV" "$FAILS"
	exit 0
fi
if [ "$FAILS" -ne 0 ]; then
	printf 'notify_parse_failure_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf 'notify_parse_failure_test: all assertions passed (6 cases)\n'
exit 0
