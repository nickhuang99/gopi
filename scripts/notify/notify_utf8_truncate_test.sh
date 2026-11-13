#!/usr/bin/env bash
# notify_utf8_truncate_test.sh — the sink's one-line message must be cut at a
# CHARACTER boundary, not in the middle of one.
#
# THE DEFECT THIS PINS (task 01a1a63d, measured on this host 2026-11-04).
# `scripts/notify/notify.sh`'s `build_message()` cut the one-line summary with
# TWO byte operations:
#
#     if [ "${#_msg}" -gt "$MESSAGE_SUMMARY_MAX" ]; then
#         _msg="${_msg:0:$MESSAGE_SUMMARY_MAX}…"
#     fi
#
# Under `LANG=C` — the locale this script runs in, and the one its own `field()`
# decoder depends on — `${#s}` measures BYTES and `${s:i:n}` slices BYTES, so a
# 3-byte character that straddles the 300-byte boundary was cut in half. The
# half character went onto the wire as an ILLEGAL UTF-8 sequence, `json.Marshal`
# carried those bytes into `messages/inbox.jsonl` verbatim, and every reader that
# decoded ONCE saw U+FFFD where the summary's last character should have been.
#
# WHAT THIS TEST ASSERTS. It runs the REAL sink end to end — the payload arrives
# on stdin exactly as `internal/tools/tasknotify.go` delivers it, and a stub
# `gopi` records the `--message` operand VERBATIM (argv, byte for byte, no
# re-encoding) — and then judges the recorded bytes:
#
#   (a) they are LEGAL UTF-8 (this is what turns red pre-fix), and
#   (b) their character count is at least the character count of the original
#       cut at the last COMPLETE character inside 300 bytes (so the fix did not
#       "solve" the problem by dropping characters it could have kept), and
#   (c) the 300-byte budget is still the upper bound, and
#   (d) the JSON round trip the real inbox performs — marshalled, then decoded
#       ONCE — gives back a message field with no U+FFFD in it.
#
# BOTH DECODE ROUTES ARE DRIVEN (with python3 and with a PATH that has none),
# because the sink's dependency-free path must be correct, not merely present
# (GOPI.md Rule 35).
#
# THE PRE-FIX VERSION RUNS THE SAME RIG: `--sink <path>` points the identical
# harness at another copy of the script, which is how the red-for-green proof is
# taken (acceptance: the same rig must be red on the unfixed sink). No second
# rig exists to drift from this one.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared
# helper — no /tmp, no hardcoded path.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK="$ROOT/scripts/notify/notify.sh"
while [ "$#" -gt 0 ]; do
	case "$1" in
		--sink)
			SINK="$2"
			shift 2
			;;
		-h|--help)
			printf 'usage: %s [--sink PATH]   (default: %s)\n' "${0##*/}" "$ROOT/scripts/notify/notify.sh"
			exit 0
			;;
		*)
			printf '%s: unknown argument %s\n' "${0##*/}" "$1" >&2
			exit 2
			;;
	esac
done
[ -f "$SINK" ] || { printf 'FATAL: sink not found: %s\n' "$SINK" >&2; exit 2; }

# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-utf8-truncate)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
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

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

# ---- the Go judge -----------------------------------------------------------
# The judgement is a property of BYTES, so it is made in a language that reads
# bytes and runes without a locale: `go`. It is the same instrument the JSON
# round trip needs (json.Marshal is what the real inbox writer uses), so one
# program answers (a)–(d) and prints the numbers it decided on.
JUDGE_SRC="$RIG/judge"
mkdir -p "$JUDGE_SRC"
cat >"$JUDGE_SRC/main.go" <<'GOJUDGE'
package main

// judge — read the message the sink actually passed to `gopi notify`, and say
// whether it is a legal, maximally-long UTF-8 truncation of the expected line.
//
// argv: <expectedFile> <capturedFile> <maxBytes>
// Exit 0 only when every criterion holds; each criterion prints its numbers so
// a failure is a measurement, not an opinion.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const truncationMarker = "…"

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: judge <expectedFile> <capturedFile> <maxBytes>")
		os.Exit(2)
	}
	expected, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "judge: reading expected: %v\n", err)
		os.Exit(2)
	}
	captured, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "judge: reading captured: %v\n", err)
		os.Exit(2)
	}
	maxBytes, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "judge: maxBytes: %v\n", err)
		os.Exit(2)
	}

	bad := 0
	report := func(ok bool, format string, args ...any) {
		if ok {
			fmt.Printf("  ok   %s\n", fmt.Sprintf(format, args...))
			return
		}
		bad++
		fmt.Printf("  FAIL %s\n", fmt.Sprintf(format, args...))
	}

	// (a) THE WIRE BYTES ARE LEGAL UTF-8. This is the defect: a cut inside a
	// character leaves an incomplete sequence, and this is the reading that
	// turns red on the unfixed sink.
	valid := utf8.Valid(captured)
	report(valid, "captured message is legal UTF-8 (%d bytes, %d chars; valid=%v)",
		len(captured), utf8.RuneCount(captured), valid)
	if !valid {
		// Name the exact offset of the first illegal byte: a report that says
		// only "invalid" makes the reader re-derive where the cut landed.
		for i := 0; i < len(captured); {
			r, size := utf8.DecodeRune(captured[i:])
			if r == utf8.RuneError && size == 1 {
				fmt.Printf("       first illegal byte at offset %d: 0x%02x (next: 0x%02x)\n",
					i, captured[i], byteAt(captured, i+1))
				break
			}
			i += size
		}
	}

	// (b) THE CUT IS AT THE LAST COMPLETE CHARACTER INSIDE THE BUDGET — not
	// earlier (that would be a fix that silently drops characters it could have
	// kept). The cut point is computed from the EXPECTED line, independently of
	// what the sink produced.
	cut := lastCompleteBoundary(expected, maxBytes)
	wantChars := utf8.RuneCount(expected[:cut])
	body := captured
	truncated := bytes.HasSuffix(captured, []byte(truncationMarker))
	if truncated {
		body = captured[:len(captured)-len(truncationMarker)]
	}
	gotChars := utf8.RuneCount(body)
	report(truncated, "the message is MARKED as truncated (a cut happened)")
	report(bytes.HasPrefix(expected, body),
		"the kept text is a byte-prefix of the original line (%d of %d bytes)", len(body), len(expected))
	report(valid && gotChars >= wantChars,
		"kept %d chars, expected at least %d (the last complete character inside %d bytes ends at byte %d)",
		gotChars, wantChars, maxBytes, cut)

	// (c) THE BYTE BUDGET IS STILL THE UPPER BOUND. The marker rides AFTER the
	// cut, exactly as it always did, so the ceiling is maxBytes + the marker.
	budget := maxBytes + len(truncationMarker)
	report(len(captured) <= budget,
		"the message is %d bytes, inside the %d-byte ceiling (cut %d + marker %d)",
		len(captured), budget, maxBytes, len(truncationMarker))

	// (d) THE INBOX ROUND TRIP: marshal the way `gopi notify` does, then decode
	// ONCE. The decoded field must be byte-identical to what the sink sent and
	// must carry no replacement character — U+FFFD here is the reader-visible
	// symptom the whole task is about.
	record, err := json.Marshal(map[string]string{"message": string(captured)})
	if err != nil {
		report(false, "json.Marshal of the inbox record: %v", err)
		return
	}
	var decoded map[string]string
	decErr := json.Unmarshal(record, &decoded)
	if decErr != nil {
		report(false, "json.Unmarshal of the inbox record (one decode): %v", decErr)
		return
	}
	msg := decoded["message"]
	report(!strings.ContainsRune(msg, utf8.RuneError),
		"the decoded message field carries no U+FFFD (%d chars)", utf8.RuneCountInString(msg))
	report(msg == string(captured),
		"the decoded message field is byte-identical to what the sink sent")

	if bad != 0 {
		os.Exit(1)
	}
}

func byteAt(b []byte, i int) byte {
	if i < 0 || i >= len(b) {
		return 0
	}
	return b[i]
}

// lastCompleteBoundary returns the largest index <= maxBytes that does not split
// a UTF-8 character.
func lastCompleteBoundary(b []byte, maxBytes int) int {
	if len(b) <= maxBytes {
		return len(b)
	}
	cut := 0
	for i := 0; i < len(b); {
		_, size := utf8.DecodeRune(b[i:])
		if i+size > maxBytes {
			break
		}
		cut = i + size
		i += size
	}
	return cut
}
GOJUDGE

JUDGE="$RIG/judgebin"
if ! (cd "$JUDGE_SRC" && GO111MODULE=off go build -o "$JUDGE" main.go) 2>"$RIG/judge.build.err"; then
	printf 'FATAL: cannot build the judge (see %s)\n' "$RIG/judge.build.err" >&2
	cat "$RIG/judge.build.err" >&2
	exit 2
fi

# ---- the payload generator --------------------------------------------------
# THE PAYLOAD IS THE REAL WIRE SHAPE, built by `encoding/json` rather than typed:
# the defect is about which BYTES reach the sink's cut, and a hand-written
# fixture could drift from what `internal/tools/tasknotify.go` emits (it is the
# same reason the neighbouring notify_field_decode_test.sh builds its payload
# with Go). Non-ASCII is NOT escaped by encoding/json, so the CJK rides as raw
# UTF-8 — the shape that straddles a byte boundary.
PAYLOADGEN_SRC="$RIG/payloadgen"
mkdir -p "$PAYLOADGEN_SRC"
cat >"$PAYLOADGEN_SRC/main.go" <<'GOPAYLOAD'
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type payload struct {
	Event     string `json:"event"`
	Previous  string `json:"previous_status"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	Timestamp string `json:"timestamp"`
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: payloadgen <event> <task_id> <status> <summary>")
		os.Exit(2)
	}
	b, err := json.Marshal(payload{
		Event:     os.Args[1],
		Previous:  "in_progress",
		TaskID:    os.Args[2],
		Status:    os.Args[3],
		Summary:   os.Args[4],
		Timestamp: "2026-11-04T19:00:00+08:00",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(b)
}
GOPAYLOAD
PAYLOADGEN="$RIG/payloadgenbin"
if ! (cd "$PAYLOADGEN_SRC" && GO111MODULE=off go build -o "$PAYLOADGEN" main.go) 2>"$RIG/payloadgen.build.err"; then
	printf 'FATAL: cannot build the payload generator (see %s)\n' "$RIG/payloadgen.build.err" >&2
	cat "$RIG/payloadgen.build.err" >&2
	exit 2
fi

# ---- the stub `gopi` --------------------------------------------------------
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
		# The message operand EXACTLY as it arrived — argv, byte for byte, with
		# a trailing newline of our own so a message carrying one could not
		# silently merge two records. Nothing is re-encoded here: a `printf %s`
		# through an encoder is how a test would repair the very defect it
		# claims to find.
		printf 'message=%s\n' "$5" >>"${STUBLOG:?STUBLOG unset}"
		;;
esac
cat >/dev/null 2>&1
exit 0
STUB
chmod 0755 "$STUB_DIR/gopi"

# ---- a PATH with NO python3, for the fallback arm ---------------------------
# Rule 35 says python3 may only be an OPTIONAL path, so the route without it is
# driven for real: the host's own tools MINUS anything python-shaped.
NOPY="$RIG/nopy"
mkdir -p "$NOPY"
for _d in /usr/bin /bin /usr/local/bin; do
	[ -d "$_d" ] || continue
	for _f in "$_d"/*; do
		_b="${_f##*/}"
		case "$_b" in
			python*|pip*) continue ;;
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
# `RUN_LOCALE` is the LOCALE ALPHABET the sink is started with, as a word list
# (deliberately unquoted at the one `env` call: it is two assignments, not one
# value). Default is the notify path's own `LANG=C LC_ALL=C`; the UTF-8 arm
# changes it to prove the 300-byte bound no longer depends on the caller's
# locale — the cut is measured in bytes inside the sink whatever it was started
# with.
RUN_LOCALE="LANG=C LC_ALL=C"
RUN_SEQ=0
run_sink() {
	RUN_SEQ=$((RUN_SEQ + 1))
	RUN_STUB="$RIG/run${RUN_SEQ}.stub"
	RUN_ERR="$RIG/run${RUN_SEQ}.err"
	RUN_CAP="$RIG/run${RUN_SEQ}.captured"
	: >"$RUN_STUB"
	: >"$RUN_ERR"
	# shellcheck disable=SC2086 # RUN_LOCALE is an assignment list, split on purpose
	env STUBLOG="$RUN_STUB" HOME="$RIG_HOME" GOPI_HOME="$1" GOPI_SETTINGS="$RIG/settings.json" \
		$RUN_LOCALE \
		PATH="$STUB_DIR:$3" bash "$SINK" <"$2" 2>"$RUN_ERR"
	RUN_RC=$?
	# The message operand is the LAST `message=` line, captured BYTE-EXACTLY:
	# the stub wrote it with one terminating newline of its own, and only that
	# byte is removed (`head -c -1` would be fine here too, but subtracting it
	# arithmetically states exactly one byte and guards a zero-length capture).
	sed -n 's/^message=//p' "$RUN_STUB" | tail -n 1 >"$RUN_CAP.raw"
	_cap_size=$(wc -c <"$RUN_CAP.raw")
	if [ "$_cap_size" -gt 0 ]; then
		head -c $((_cap_size - 1)) "$RUN_CAP.raw" >"$RUN_CAP"
	else
		: >"$RUN_CAP"
	fi
	RUN_MSG_BYTES=$(wc -c <"$RUN_CAP")
}

printf 'notify.sh UTF-8 truncation (rig: %s)\n' "$RIG"
printf 'sink under test: %s (%s)\n' "$SINK" "$(md5sum "$SINK" | cut -d' ' -f1)"

printf '{}\n' >"$RIG/settings.json"

# ---- the expected one-line message -----------------------------------------
# The sink builds exactly this from the payload's four ASCII fields (no `ledger`
# key). Keeping the fields here — not hand-typing the joined line twice — is
# what makes "expected" an INDEPENDENT statement of the contract rather than a
# copy of the sink's own output.
EVENT="completed"
TASK_ID="01a1a63d-267b-7ab2-a4ce-9fc007c274a4"
STATUS="completed"
PREFIX="task event: event=${EVENT} task=${TASK_ID} status=${STATUS} summary="
# Byte length of the prefix: every byte is ASCII, so under LANG=C `${#PREFIX}`
# is exactly its size in bytes — which is what the cut point is measured from.
PREFIX_BYTES=${#PREFIX}
MAX_BYTES=300

# pad=0..6 puts the 300-byte boundary at a different offset INSIDE the multi-byte
# run each time (0,1,2 bytes into a 3-byte character; 0,1,2 into the next; and
# pad=3 lands exactly on a boundary — the control that stays green either way).
for pad in 0 1 2 3 4 5 6; do
	ascii_len=$((MAX_BYTES - PREFIX_BYTES - pad))
	[ "$ascii_len" -ge 1 ] || { fail "case pad=$pad: rig arithmetic produced a non-positive pad ($ascii_len)"; continue; }
	pad_ascii="$(printf 'a%.0s' $(seq 1 "$ascii_len"))"
	summary="${pad_ascii}中文测试字符串结尾"
	profile="$RIG/p${pad}"
	mkdir -p "$profile/.gopi"
	slot="$(live_session "$profile" "55555555-0000-4000-8000-00000000010${pad}")"
	printf '424260:%s\n' "$slot" >"$profile/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"

	payload="$RIG/payload${pad}.json"
	"$PAYLOADGEN" "$EVENT" "$TASK_ID" "$STATUS" "$summary" >"$payload" || {
		fail "pad=$pad: payload generator failed"
		continue
	}

	expected="$RIG/expected${pad}.txt"
	printf '%s%s' "$PREFIX" "$summary" >"$expected"

	printf '\n[case pad=%s] the 300-byte boundary lands %s byte(s) into a 3-byte character\n' \
		"$pad" "$((pad % 3))"
	printf '  measured: prefix=%s bytes, whole line=%s bytes\n' \
		"$PREFIX_BYTES" "$(wc -c <"$expected")"

	# ---- route 1: the python3 fast path (the normal machine) ----
	run_sink "$profile" "$payload" "$PATH"
	if [ "$RUN_RC" -ne 0 ]; then fail "pad=$pad route=python3: sink exited $RUN_RC"; fi
	printf '  -- route: python3 (PATH as found), captured %s bytes\n' "$RUN_MSG_BYTES"
	if "$JUDGE" "$expected" "$RUN_CAP" "$MAX_BYTES"; then
		pass "pad=$pad route=python3: legal, maximally long, inside the budget"
	else
		fail "pad=$pad route=python3: see the FAIL lines above"
	fi

	# ---- route 2: no python3 at all (Rule 35: the fallback must be correct) ----
	run_sink "$profile" "$payload" "$NOPY"
	if [ "$RUN_RC" -ne 0 ]; then fail "pad=$pad route=bash: sink exited $RUN_RC"; fi
	printf '  -- route: no python3 (fallback decoder), captured %s bytes\n' "$RUN_MSG_BYTES"
	if "$JUDGE" "$expected" "$RUN_CAP" "$MAX_BYTES"; then
		pass "pad=$pad route=bash: legal, maximally long, inside the budget"
	else
		fail "pad=$pad route=bash: see the FAIL lines above"
	fi
done

# ---- the short-message control ---------------------------------------------
# A message that fits must be delivered COMPLETELY and must carry no truncation
# marker: the fix moves a cut point, it does not truncate everything.
profile="$RIG/pshort"
mkdir -p "$profile/.gopi"
slot="$(live_session "$profile" 55555555-0000-4000-8000-0000000001ff)"
printf '424261:%s\n' "$slot" >"$profile/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
short_summary="短消息 no truncation here"
"$PAYLOADGEN" "$EVENT" "$TASK_ID" "$STATUS" "$short_summary" >"$RIG/payload_short.json" || fail "short case: payload generator failed"
printf '%s%s' "$PREFIX" "$short_summary" >"$RIG/expected_short.txt"
run_sink "$profile" "$RIG/payload_short.json" "$PATH"
if [ "$(cat "$RUN_CAP")" = "$(cat "$RIG/expected_short.txt")" ]; then
	pass "short message: delivered in full, byte for byte, with no marker"
else
	fail "short message: the line changed. wanted: $(cat "$RIG/expected_short.txt") ; got: $(cat "$RUN_CAP")"
fi

# ---- the CALLER-LOCALE control: the byte bound is not the caller's business --
# The pre-fix guard was `[ "${#_msg}" -gt 300 ]`, and under a UTF-8 locale `${#}`
# counts CHARACTERS — so one line of 300 CJK characters (900 bytes) passed the
# guard untouched and the byte ceiling was never enforced at all. That is the
# reason the cut is now measured in bytes inside the sink instead of behind a
# locale-dependent guard, and this arm is what holds it to that: the SAME
# oversized payload is delivered with the sink started under a UTF-8 locale and
# must still come back inside the byte budget.
UTF8_LOCALE=""
for _cand in C.UTF-8 C.utf8 en_US.UTF-8 en_US.utf8; do
	if LC_ALL="$_cand" bash -c 'true' 2>/dev/null; then
		UTF8_LOCALE="$_cand"
		break
	fi
done
if [ -z "$UTF8_LOCALE" ]; then
	# Not a silent skip (Rule 15): the arm states that this host has no UTF-8
	# locale, so the reading is absent rather than green.
	printf '\n  note UTF-8-locale arm not run: this host has no C.UTF-8/en_US.UTF-8 locale\n'
else
	profile="$RIG/putf8"
	mkdir -p "$profile/.gopi"
	slot="$(live_session "$profile" 55555555-0000-4000-8000-0000000001fe)"
	printf '424262:%s\n' "$slot" >"$profile/.gopi/${SLOT_PREFIX}0${SLOT_SUFFIX}"
	utf8_summary=""
	# Built by ITERATION COUNT, never by `${#…}`: the test's own locale decides
	# whether that counts bytes or characters, and a fixture whose size depends
	# on the shell's locale measures nothing about the sink.
	for _i in $(seq 1 300); do utf8_summary="${utf8_summary}中"; done
	"$PAYLOADGEN" "$EVENT" "$TASK_ID" "$STATUS" "$utf8_summary" >"$RIG/payload_utf8.json" || fail "utf8-locale case: payload generator failed"
	printf '%s%s' "$PREFIX" "$utf8_summary" >"$RIG/expected_utf8.txt"
	printf '\n[case caller-locale=%s] a 300-CHARACTER CJK line is 995 bytes — the byte ceiling must still hold\n' "$UTF8_LOCALE"
	RUN_LOCALE="LANG=$UTF8_LOCALE LC_ALL=$UTF8_LOCALE"
	run_sink "$profile" "$RIG/payload_utf8.json" "$PATH"
	RUN_LOCALE="LANG=C LC_ALL=C"
	printf '  -- captured %s bytes under %s (the line itself is 995 bytes)\n' "$RUN_MSG_BYTES" "$UTF8_LOCALE"
	if "$JUDGE" "$RIG/expected_utf8.txt" "$RUN_CAP" "$MAX_BYTES"; then
		pass "caller locale $UTF8_LOCALE: the 300-BYTE bound is enforced for a 995-byte line"
	else
		fail "caller locale $UTF8_LOCALE: the byte bound was not enforced — see the FAIL lines"
	fi
fi

printf '\n'
if [ "$FAILS" -ne 0 ]; then
	printf 'notify_utf8_truncate_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf 'notify_utf8_truncate_test: all assertions passed (7 boundary cases x 2 routes + 1 control)\n'
exit 0
