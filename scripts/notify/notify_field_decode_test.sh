#!/usr/bin/env bash
# notify_field_decode_test.sh — the sink must DECODE a payload field once, not
# hand its JSON ESCAPES to the console as if they were text.
#
# THE DEFECT THIS PINS (task 01a1a61b-9e80, measured on this host 2026-11-04).
# `scripts/notify/notify.sh`'s `field()` lifted a payload field out with
#
#     printf '%s' "$payload" | tr -d '\n' |
#         sed -n 's/.*"KEY":"\([^"]*\)".*/\1/p'
#
# which is a REGEX, not a decode. Go's `encoding/json` writes `>` as `\u003e`
# (HTML escaping is on by default — internal/tools/tasknotify.go's
# deliverTaskNotify → json.Marshal(payload)), so the extractor returned the six
# literal characters `\u003e`; the inbox writer then marshalled THAT as content
# into `\\u003e` on disk, and every reader that decoded once — the model, the
# session record, `grep` — saw `\u003e` where the agent had written `>`.
# Measured on the PA session that reported it: 78 literal `\u003e` and 0 real
# `>` in the notify-delivered records of its inbox.
#
# WHAT THIS TEST ASSERTS, and why it is a SHELL test. The defect lives in the
# sink's control flow — which bytes reach `gopi notify --message` — so the only
# instrument that can see it is the sink itself, run end to end with a stub
# `gopi` that records the message operand verbatim. A Go test re-implementing
# the extractor would assert on its own copy, which is the drift the
# cross-language pins in this repo exist to prevent.
#
# THE PAYLOAD IS THE REAL WIRE SHAPE, not a hand-written string: case 1 uses
# the exact bytes `json.Marshal` produces for a summary carrying `>`, `<`, `&`,
# a `"`, a `\` and CJK. Both decode routes are driven — the python3 fast path
# and the dependency-free in-script decoder (Rule 35: python3 is optional, so
# the fallback must be exercised, and it must be CORRECT, not merely present).
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared
# helper — no /tmp, no hardcoded path.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK="$ROOT/scripts/notify/notify.sh"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-field-decode)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# THE RIG'S OWN HOME (measured 2026-11-03). notify.sh's refusal of a
# checkout-shaped GOPI_HOME ANCHORS the payload at $HOME/.gopi — the documented
# fallback, pinned by notify_slot_contract_test's case 6. So a sink call that
# pins GOPI_HOME but leaves HOME INHERITED files the rig's fixture payload into
# the OPERATOR's live evidence file (/home/user/.gopi/notify/task-events.jsonl). Every
# sink invocation in this file therefore names $RIG_HOME; notify_hermeticity_test.sh
# scans for a call site that does not.
RIG_HOME="$RIG/rig-home"
mkdir -p "$RIG_HOME"


SLOT_PREFIX="chat_session_id_"
SLOT_SUFFIX=".txt"

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

# ---- the stub `gopi` --------------------------------------------------------
# It records the FULL argv (so the message operand is read exactly as the sink
# passed it, one word) and succeeds, which is what the real verb does when a
# live session takes the message.
STUB_DIR="$RIG/bin"
mkdir -p "$STUB_DIR"
cat >"$STUB_DIR/gopi" <<'STUB'
#!/usr/bin/env bash
# APPEND, never truncate: the sink calls this more than once per run (the
# capability probes ask the verb's own help), and a truncating stub would erase
# the delivery it is supposed to record.
case "$1${2:+ $2}" in
	"notify --session")
		printf 'session=%s\n' "$3" >>"${STUBLOG:?STUBLOG unset}"
		# ONE line per delivery, the message operand exactly as it arrived.
		# `%s\n` rather than a bare append so a message with a trailing newline
		# (which this sink never produces) could not silently merge two records.
		printf 'message=%s\n' "$5" >>"${STUBLOG:?STUBLOG unset}"
		;;
esac
# Everything else (--deliver-multi -h, the probes) answers "yes, supported" and
# reads its stdin, which is what a current `gopi` does.
cat >/dev/null 2>&1
exit 0
STUB
chmod 0755 "$STUB_DIR/gopi"

# ---- a PATH with NO python3, for the fallback arm ---------------------------
# Rule 35 says python3 may only be an OPTIONAL path, so the fallback has to be
# driven for real. Shadowing it with a broken shim is not enough (`command -v`
# would find the real one further along), so this arm builds a PATH of the
# host's own tools MINUS anything python-shaped: the sink then takes its
# dependency-free route exactly as it would on a machine without python3.
NOPY="$RIG/nopy"
mkdir -p "$NOPY"
for _d in /usr/bin /bin /usr/local/bin; do
	[ -d "$_d" ] || continue
	for _f in "$_d"/*; do
		_b="${_f##*/}"
		case "$_b" in
			python*|python3*|pip*) continue ;;
		esac
		[ -e "$NOPY/$_b" ] && continue
		ln -s "$_f" "$NOPY/$_b" 2>/dev/null || true
	done
done

# live_session PROFILE NAME — a session DIRECTORY the stub accepts, and the
# identity that names it in the slot registry.
live_session() {
	local dir="$1/.gopi/sessions/$2"
	mkdir -p "$dir"
	printf '%s' "$dir"
}

# run_sink HOME PAYLOAD_FILE PATHVALUE — feed one payload, capture the message.
RUN_SEQ=0
run_sink() {
	RUN_SEQ=$((RUN_SEQ + 1))
	RUN_STUB="$RIG/run${RUN_SEQ}.stub"
	RUN_ERR="$RIG/run${RUN_SEQ}.err"
	: >"$RUN_STUB"
	: >"$RUN_ERR"
	# GOPI_SETTINGS: the sink reads its peer list from here. Pinning it at a rig
	# file with no `sync_peers` is what keeps this test OFFLINE and FAST — the
	# subject is the field decoder, and an ssh leg to a real peer would make the
	# verdict depend on two other machines.
	STUBLOG="$RUN_STUB" HOME="$RIG_HOME" GOPI_HOME="$1" GOPI_SETTINGS="$RIG/settings.json" \
		PATH="$STUB_DIR:$3" bash "$SINK" <"$2" 2>"$RUN_ERR"
	RUN_RC=$?
	RUN_MSG="$(sed -n 's/^message=//p' "$RUN_STUB" | tail -n 1)"
}

# ---- 1. THE REPRO: the real `json.Marshal` wire shape ----------------------
# The summary is the one an operator actually hit: `x >= 3`-shaped technical
# text, plus `<`, `&`, a quote and a backslash so the string scan is exercised
# too. Anything the extractor leaves escaped is visible in the message.
P1="$RIG/p1"
mkdir -p "$P1"
SLOT1="$(live_session "$P1" 55555555-0000-4000-8000-000000000001)"
mkdir -p "$P1/.gopi"
printf '424250:%s\n' "$SLOT1" >"$P1/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"

# The payload is BUILT BY GO, not typed: the defect is about what encoding/json
# emits, so a hand-written fixture could drift from it and the test would pass
# while production broke (measured: the escape list is exactly > < & and the
# quote/backslash forms; non-ASCII is NOT escaped by encoding/json).
PAYLOAD1="$RIG/payload1.json"
GOMARSHAL="$RIG/gomarshal"
mkdir -p "$GOMARSHAL"
cat >"$GOMARSHAL/main.go" <<'GOFIXTURE'
package main

// Emits the exact bytes internal/tools' deliverTaskNotify puts on the wire for
// one lifecycle event, with a summary that carries every character class this
// test cares about.

import (
	"encoding/json"
	"fmt"
	"os"
)

type payload struct {
	Event      string `json:"event"`
	Previous   string `json:"previous_status"`
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Reason     string `json:"reason,omitempty"`
	Timestamp  string `json:"timestamp"`
	ElapsedSec int    `json:"elapsed_seconds"`
}

func main() {
	p := payload{
		Event:      "completed",
		Previous:   "in_progress",
		TaskID:     "01a1a61b-9e80-75ae-9849-e17fe4a86193",
		Status:     "completed",
		Summary:    os.Args[1],
		Timestamp:  "2026-11-04T19:00:00+08:00",
		ElapsedSec: 7,
	}
	b, err := json.Marshal(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s", b)
}
GOFIXTURE
SUMMARY1='slack 13 > margin 12; dial tcp <edge IP>:443 & "quoted" \ back; 中文 x >= 3'
if ! (cd "$GOMARSHAL" && GO111MODULE=off go run main.go "$SUMMARY1" >"$PAYLOAD1"); then
	fail "case 1: cannot build the Go wire fixture (encoding/json round trip)"
	printf '' >"$PAYLOAD1"
fi

printf '{}\n' >"$RIG/settings.json"
printf '%s\n' "notify.sh field decode (rig: $RIG)"

run_sink "$P1" "$PAYLOAD1" "$PATH"
if [ "$RUN_RC" -ne 0 ]; then fail "case 1: sink exited $RUN_RC"; fi
case "$RUN_MSG" in
	*"summary=$SUMMARY1"*)
		pass "case 1: the message's summary is BYTE-IDENTICAL to the summary the producer wrote" ;;
	*)
		fail "case 1: the summary did not survive the chain. wanted: summary=$SUMMARY1 ; got: $RUN_MSG" ;;
esac
case "$RUN_MSG" in
	*'\u003e'*|*'\u003c'*|*'\u0026'*)
		fail "case 1: the message still carries a JSON escape literally: $RUN_MSG" ;;
	*) pass "case 1: no \\u003e / \\u003c / \\u0026 literal reached --message" ;;
esac
case "$RUN_MSG" in
	*'x >= 3'*) pass "case 1: the operator's own '>=' arrives as '>='" ;;
	*) fail "case 1: '>=' did not arrive as text: $RUN_MSG" ;;
esac
# THE TRUNCATION ARM: `\([^"]*\)` stopped at the first `\"` inside the value, so
# a summary with a quote in it was cut in half. The scan must carry it.
case "$RUN_MSG" in
	*'"quoted"'*) pass "case 1: an escaped quote inside the value does not truncate it" ;;
	*) fail "case 1: the value was truncated at the escaped quote: $RUN_MSG" ;;
esac
# …and the peer keys stay intact, because a regex that over-reads would swallow
# them (the whole-object capture the old expression relied on).
case "$RUN_MSG" in
	*task=01a1a61b-9e80-75ae-9849-e17fe4a86193*) pass "case 1: the neighbouring field is intact" ;;
	*) fail "case 1: the task id did not survive: $RUN_MSG" ;;
esac

# ---- 2. THE SAME PAYLOAD WITH NO python3: the fallback must be CORRECT -----
# Rule 35: python3 is an optional path, never a requirement — so the route
# without it has to produce the SAME bytes, not merely "something".
run_sink "$P1" "$PAYLOAD1" "$NOPY"
if [ "$RUN_RC" -ne 0 ]; then fail "case 2: sink exited $RUN_RC with no python3"; fi
if [ "$RUN_MSG" = "task event: event=completed task=01a1a61b-9e80-75ae-9849-e17fe4a86193 status=completed summary=$SUMMARY1" ]; then
	pass "case 2: without python3 the message is byte-identical to the python3 route"
else
	fail "case 2: the no-python3 route produced a different message: $RUN_MSG"
fi
if grep -qF "no python3" "$RUN_ERR"; then
	pass "case 2: the fallback is ANNOUNCED on stderr, not silently taken"
else
	fail "case 2: the fallback route said nothing — a degradation nobody sees is Rule 15's defect"
fi

# ---- 3. THE UNICODE FORMS, byte for byte -----------------------------------
# `\uXXXX` is where a hand-rolled decoder goes wrong, and this host runs with
# LANG=C, so a decoder that echoes the code point instead of building its UTF-8
# BYTES produces six literal characters. Both a BMP character and a surrogate
# PAIR (non-BMP) are driven through the fallback route.
P3="$RIG/p3"
mkdir -p "$P3"
SLOT3="$(live_session "$P3" 55555555-0000-4000-8000-000000000003)"
mkdir -p "$P3/.gopi"
printf '424252:%s\n' "$SLOT3" >"$P3/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
PAYLOAD3="$RIG/payload3.json"
printf '%s' '{"event":"claimed","task_id":"t3","status":"claimed","summary":"\u4e2d\u6587 and \ud83d\ude00 and a\/b"}' >"$PAYLOAD3"
EXPECT3='summary=中文 and 😀 and a/b'
run_sink "$P3" "$PAYLOAD3" "$NOPY"
case "$RUN_MSG" in
	*"$EXPECT3"*)
		pass "case 3: \\uXXXX (BMP + surrogate pair) decodes to the real UTF-8 bytes without python3" ;;
	*)
		fail "case 3: unicode escapes did not decode. wanted: $EXPECT3 ; got: $RUN_MSG" ;;
esac

# ---- 4. THE ANCHOR IS UNTOUCHED -------------------------------------------
# The raw payload is the fallback record every reader greps; the fix is at the
# EXTRACTOR, so the anchor must still carry the producer's bytes verbatim.
ANCHOR1="$P1/.gopi/notify/task-events.jsonl"
if [ -s "$ANCHOR1" ] && [ "$(cat "$PAYLOAD1")" = "$(head -n 1 "$ANCHOR1")" ]; then
	pass "case 4: the anchor holds the raw payload, escapes and all (nothing was rewritten)"
else
	fail "case 4: the anchor is not byte-identical to the payload the producer sent"
fi
# and the escapes ARE still in it: the fix is at the extractor, and a change that
# also re-encoded the anchor would break every existing reader of that file.
if grep -qF '\u003e' "$ANCHOR1" 2>/dev/null; then
	pass "case 4: the anchor still carries the producer's JSON escapes"
else
	fail "case 4: the anchor no longer carries the producer's escapes"
fi

# ---- 5. A MISSING FIELD IS STILL THE EMPTY STRING -------------------------
# The contract the callers rely on: `ledger` absent means "the profile's own
# ledger" and must render as nothing at all, never as an error or a stray word.
P5="$RIG/p5"
mkdir -p "$P5"
SLOT5="$(live_session "$P5" 55555555-0000-4000-8000-000000000005)"
mkdir -p "$P5/.gopi"
printf '424254:%s\n' "$SLOT5" >"$P5/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
PAYLOAD5="$RIG/payload5.json"
printf '%s' '{"event":"pending","task_id":"t5","status":"pending","summary":"no ledger here"}' >"$PAYLOAD5"
run_sink "$P5" "$PAYLOAD5" "$PATH"
case "$RUN_MSG" in
	*ledger=*) fail "case 5: an absent field rendered a fragment: $RUN_MSG" ;;
	*) pass "case 5: an absent field renders nothing (the pre-existing contract)" ;;
esac

# ---- 6. A SUMMARY THAT ITSELF CONTAINS THE LITERAL ESCAPE ------------------
# The nastiest shape, and the one a "fix it in the reader" patch would break:
# an agent writing ABOUT the defect types the six characters `\u003e`. The
# producer escapes the backslash, so the wire carries `\\u003e`, and ONE decode
# must give the six characters back — not `>` (that would be decoding twice and
# inventing content), not eight.
P6="$RIG/p6"
mkdir -p "$P6"
SLOT6="$(live_session "$P6" 55555555-0000-4000-8000-000000000006)"
mkdir -p "$P6/.gopi"
printf '424255:%s\n' "$SLOT6" >"$P6/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
PAYLOAD6="$RIG/payload6.json"
printf '%s' '{"event":"completed","task_id":"t6","status":"completed","summary":"the literal \\u003e must stay literal"}' >"$PAYLOAD6"
run_sink "$P6" "$PAYLOAD6" "$NOPY"
case "$RUN_MSG" in
	*'summary=the literal \u003e must stay literal'*)
		pass "case 6: a literal \\u003e in the producer's text survives exactly once" ;;
	*)
		fail "case 6: a literal escape was decoded twice or lost: $RUN_MSG" ;;
esac
run_sink "$P6" "$PAYLOAD6" "$PATH"
case "$RUN_MSG" in
	*'summary=the literal \u003e must stay literal'*)
		pass "case 6: and the python3 route agrees" ;;
	*)
		fail "case 6: the python3 route disagrees on a literal escape: $RUN_MSG" ;;
esac

printf '\n'
if [ "$FAILS" -ne 0 ]; then
	printf 'notify_field_decode_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf 'notify_field_decode_test: all assertions passed (6 cases)\n'
exit 0
