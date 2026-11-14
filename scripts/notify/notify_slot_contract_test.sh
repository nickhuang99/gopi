#!/usr/bin/env bash
# notify_slot_contract_test.sh — the SHELL half of task 01a12d05's contract,
# driven end to end against the shipped sink (scripts/notify/notify.sh).
#
# WHY A SHELL TEST AND NOT ONLY A GO TEST. The Go tests pin the REGISTRY
# (cmd/agent/chat_session_slots_test.go) and the two languages' shared constants
# (cmd/agent/chat_session_slots_script_test.go), but neither of them ever RUNS
# the script: what the sink PRINTS when no chat received an event, and which
# DIRECTORY it scans, are properties of that file's control flow. A Go test that
# re-implemented the loop would be a second implementation asserting on itself —
# the same drift the cross-language pin exists to prevent.
#
# THE STUB IS THE INSTRUMENT, NOT A MOCK OF THE ASSERTION. `gopi` is replaced on
# PATH by a three-line script that records the session it was asked to deliver
# to and exits 0. That is exactly what the real verb does on success and it is
# the only fact these cases need; every case below then asserts on the sink's OWN
# stdout/stderr, on the anchor file, and on the stub's log — never on a variable
# the script set for the test's benefit.
#
# THE FIVE CASES, and the defect each one is aimed at:
#
#   1. no slot registered at all        → the event reached nobody and the sink
#                                          SAYS SO (before: one `delivered=0
#                                          refused=0` line, indistinguishable
#                                          from a healthy machine);
#   2. every slot dead (dir gone)       → same report, with the dead count;
#   3. one live chat, two dead slots    → delivered=1, dead=2, and NO "NO CHAT
#                                          RECEIVED" line (the report must not
#                                          fire when somebody did receive it);
#   4. a RELOCATED run's own tree       → the sink scans the tree named by
#     (GOPI_SESSIONS_ROOT set)            GOPI_SESSIONS_ROOT, delivers there, and
#                                          does NOT read the profile's registry;
#   5. the control arm (nothing set)    → the profile registry, byte for byte
#                                          the pre-change behaviour.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared
# helper — no /tmp, no hardcoded path.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK="$ROOT/scripts/notify/notify.sh"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-slot-contract)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
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
SLOT_LIMIT=10
PAYLOAD='{"event":"completed","task_id":"01a12d05","status":"completed","summary":"slot contract case"}'

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

# ---- the stub `gopi` --------------------------------------------------------
STUB_DIR="$RIG/bin"
mkdir -p "$STUB_DIR"
cat >"$STUB_DIR/gopi" <<'STUB'
#!/usr/bin/env bash
# Records the delivery target and succeeds — the shape of a live chat taking a
# message. STUBLOG is set by the test, one file per case.
# argv: notify --session <DIR> --message <TEXT> — the operand is the THIRD word,
# and recording exactly that is what lets a case assert WHICH tree was reached.
printf 'notify --session %s\n' "$3" >>"${STUBLOG:?STUBLOG unset}"
# The MESSAGE is the fifth word (notify --session <DIR> --message <TEXT>) and it
# is what the operator actually reads. Recording it is what lets a case assert
# on the rendered line rather than on a variable the test set for itself.
printf 'message %s\n' "$5" >>"${STUBLOG:?STUBLOG unset}"
# The REFUSAL half of the real verb, and it is not decoration: a session
# directory that does not exist is the "unknown session" refusal production
# answers with, so a case whose slots are all dead must see refused>0 rather
# than a success the real command would never have printed.
if [ ! -d "$3" ]; then
	printf 'gopi notify: unknown session "%s" — no session directory there\n' "$3" >&2
	exit 1
fi
printf 'delivered to %s\n' "$3"
exit 0
STUB
chmod 0755 "$STUB_DIR/gopi"

# run_case NAME HOME SESSIONS_ROOT
# Feeds $PAYLOAD on stdin, captures the sink's stderr in $RIG/<name>.err and the
# stub's deliveries in $RIG/<name>.stub. An empty SESSIONS_ROOT is the
# UNRELOCATED control arm: the variable is present and empty, which is the same
# fact as unset for both the shell sink and config.SessionsRoot.
run_case() {
	local name="$1" home="$2" sessions_root="$3"
	local profile="$home/.gopi"
	CASE_ERR="$RIG/$name.err"
	CASE_STUB="$RIG/$name.stub"
	: >"$CASE_ERR"
	: >"$CASE_STUB"
	printf '%s' "$PAYLOAD" | GOPI_HOME="$home" GOPI_SESSIONS_ROOT="$sessions_root" \
		STUBLOG="$CASE_STUB" HOME="$RIG_HOME" PATH="$STUB_DIR:$PATH" bash "$SINK" 2>"$CASE_ERR"
	CASE_RC=$?
}

# slot REGISTRY_DIR INDEX IDENTITY — write one slot file, the way `gopi chat`
# does: into the registry, which is the PROFILE ROOT (<home>/.gopi), the parent
# of the sessions tree — the sink's own derivation, spelled the same way here.
slot() {
	local dir="$1"
	mkdir -p "$dir"
	printf '%s\n' "$3" >"$dir/${SLOT_PREFIX}$2${SLOT_SUFFIX}"
	chmod 0600 "$dir/${SLOT_PREFIX}$2${SLOT_SUFFIX}"
}

# live_session PROFILE NAME — a session DIRECTORY that exists (the stub's only
# requirement for a successful delivery) and the identity that names it.
live_session() {
	local dir="$1/.gopi/sessions/$2"
	mkdir -p "$dir"
	printf '%s' "$dir"
}

assert_has() {
	if grep -qF -- "$2" "$1"; then pass "$3"; else fail "$3 — no line containing: $2"; fi
}
assert_lacks() {
	if grep -qF -- "$2" "$1"; then fail "$3 — it DID say: $2"; else pass "$3"; fi
}

printf '%s\n' "notify.sh slot contract (rig: $RIG)"

# ---- 1. NO SLOT AT ALL ------------------------------------------------------
P1="$RIG/p1"
mkdir -p "$P1"
run_case "empty" "$P1" ""
if [ "$CASE_RC" -ne 0 ]; then fail "case 1: sink exited $CASE_RC"; fi
assert_has "$CASE_ERR" "delivered=0" "case 1: the outcome line is printed"
assert_has "$CASE_ERR" "NO CHAT RECEIVED THIS EVENT" "case 1: an event that reached nobody says so"
assert_has "$CASE_ERR" "no chat is registered" "case 1: and says WHY (no registry entry)"
assert_has "$P1/.gopi/notify/task-events.jsonl" "01a12d05" "case 1: the anchor still holds the raw payload"

# ---- 2. EVERY SLOT DEAD (session directory gone) ----------------------------
P2="$RIG/p2"
mkdir -p "$P2"
for i in 0 1 2; do slot "$P2/.gopi" "$i" "424242:$RIG/gone/session-$i"; done
run_case "alldead" "$P2" ""
assert_has "$CASE_ERR" "delivered=0 refused=3 dead=3 registered=3" "case 2: the aggregate counts the dead registrations"
assert_has "$CASE_ERR" "NO CHAT RECEIVED THIS EVENT" "case 2: every target dead is reported, not just refused per-slot"
assert_has "$CASE_ERR" "3 of them dead" "case 2: and the dead count is in the report"

# ---- 3. ONE LIVE CHAT, TWO DEAD SLOTS (the control for case 2) --------------
P3="$RIG/p3"
mkdir -p "$P3"
live3="$(live_session "$P3" 11111111-2222-4333-8444-555555555555)"
slot "$P3/.gopi" 0 "424242:$RIG/gone/session-a"
slot "$P3/.gopi" 1 "424243:$live3"
slot "$P3/.gopi" 2 "424244:$RIG/gone/session-b"
run_case "onlive" "$P3" ""
assert_has "$CASE_ERR" "delivered=1 refused=2 dead=2 registered=3" "case 3: one delivery, two dead, three registered"
assert_lacks "$CASE_ERR" "NO CHAT RECEIVED THIS EVENT" "case 3: a delivery that happened is NOT reported as a loss"
assert_has "$CASE_STUB" "notify --session $live3" "case 3: the live session is the one delivered to"

# ---- 4. A RELOCATED RUN SCANS ITS OWN TREE ---------------------------------
# The profile has a live chat of its own; the relocated tree has a different one.
# A sink that read the profile's registry would deliver to the WRONG session —
# which is the whole defect (a scratch run occupying the operator's registry).
P4="$RIG/p4"
mkdir -p "$P4"
profile_live="$(live_session "$P4" 99999999-0000-4000-8000-000000000001)"
slot "$P4/.gopi" 0 "424245:$profile_live"
# The relocated run's tree: its registry is the directory the caller named
# (ChatSlotsDir = the parent of GOPI_SESSIONS_ROOT) and its sessions live under
# it — the layout cmd/agent/chat_sessions_root.go publishes.
RELOC="$RIG/relocated"
mkdir -p "$RELOC/sessions"
reloc_live="$RELOC/sessions/77777777-0000-4000-8000-000000000002"
mkdir -p "$reloc_live"
slot "$RELOC" 0 "424246:$reloc_live"
run_case "relocated" "$P4" "$RELOC/sessions"
assert_has "$CASE_ERR" "slots_dir=$RELOC" "case 4: the sink scanned the relocated tree, and names it"
assert_has "$CASE_STUB" "notify --session $reloc_live" "case 4: delivery went to the relocated run's own chat"
assert_lacks "$CASE_STUB" "$profile_live" "case 4: the profile's chat was NOT a target (no cross-tree delivery)"
assert_has "$P4/.gopi/notify/task-events.jsonl" "01a12d05" "case 4: the anchor is still the profile's own stream"

# ---- 5. THE CONTROL ARM: nothing relocated is byte-for-byte the old shape ---
P5="$RIG/p5"
mkdir -p "$P5"
live5="$(live_session "$P5" 33333333-0000-4000-8000-000000000003)"
slot "$P5/.gopi" 0 "424247:$live5"
run_case "profile" "$P5" ""
assert_has "$CASE_ERR" "slots_dir=$P5/.gopi" "case 5: an unrelocated sink scans the profile root"
assert_has "$CASE_STUB" "notify --session $live5" "case 5: and delivers to the profile's own chat"
assert_lacks "$CASE_ERR" "NO CHAT RECEIVED THIS EVENT" "case 5: no loss report when a chat was served"

printf '\n'

# ---- 6. A GOPI_HOME THAT NAMES A CODE CHECKOUT ------------------------------
# THE DEFECT (epic: a child must never resolve a throwaway profile). GOPI_HOME is
# documented as a HOME and the profile root is `<GOPI_HOME>/.gopi`, so a value
# naming a GIT WORK TREE made this sink anchor the event inside the checkout:
# written into a profile nothing sweeps or reads back, the delivery vanished
# silently, and the worktree went dirty. THIS CASE IS THE MISSING HALF of that
# guard on the consumer side: the sink must REFUSE the shape, name the value and
# the reason, and still not lose the event.
#
# THE KEEP ARM IS CASE 1-5 ABOVE: every one of them runs with GOPI_HOME=<a real
# directory holding .gopi> and no checkout marker, so a sandbox-shaped home must
# keep resolving through GOPI_HOME — this case is the only one that refuses.
WT="$RIG/p6-worktree"
mkdir -p "$WT/.gopi"            # a profile-shaped directory INSIDE the checkout…
git init -q "$WT" 2>/dev/null || mkdir -p "$WT/.git"   # …and the marker that makes it one
P6_HOME="$RIG/p6-home"
mkdir -p "$P6_HOME"
: >"$RIG/p6.err"
: >"$RIG/p6.stub"
printf '%s' "$PAYLOAD" | GOPI_HOME="$WT" STUBLOG="$RIG/p6.stub" PATH="$STUB_DIR:$PATH" \
	HOME="$P6_HOME" bash "$SINK" 2>"$RIG/p6.err"
CASE_RC=$?
if [ "$CASE_RC" -ne 0 ]; then fail "case 6: sink exited $CASE_RC"; fi
assert_has "$RIG/p6.err" "REFUSING GOPI_HOME=$WT" "case 6: the refusal names the offending value"
assert_has "$RIG/p6.err" "names a CODE CHECKOUT" "case 6: and says WHY (the checkout marker)"
assert_has "$RIG/p6.err" "throwaway profile" "case 6: and what the value would have created"
assert_has "$P6_HOME/.gopi/notify/task-events.jsonl" "01a12d05" \
	"case 6: the event is ANCHORED in the operator's own profile, never lost"
if [ -e "$WT/.gopi/notify/task-events.jsonl" ]; then
	fail "case 6: the sink wrote INSIDE the code checkout ($WT/.gopi/notify/task-events.jsonl)"
else
	pass "case 6: nothing was written inside the checkout"
fi
if [ -n "$(ls -A "$WT/.gopi" 2>/dev/null)" ]; then
	fail "case 6: the checkout's profile dir is no longer empty"
else
	pass "case 6: the checkout's profile dir is untouched"
fi

# ---- 7. THE ORIGIN LEDGER RIDES THE MESSAGE --------------------------------
# THE DEFECT this case pins (measured 2026-10-23): two lifecycle events reached
# the operator's console naming task ids that exist in NO ledger he could open
# (`01a16515-…` from a probe's private ledger, `probe-dead-holder` from
# `probe/ledger.db` under session 01a16409). The mechanism is that the LEDGER
# and the NOTIFY TARGET resolve through independent channels, so a run that
# isolates only its ledger still delivers here — and the message gave the
# operator nothing to open. The payload now names the emitting ledger when it is
# not the profile's own, and this case asserts the sink RENDERS it (a field the
# sink drops is a field the console never sees: Rule 15, one language over).
P7="$RIG/p7"
mkdir -p "$P7"
live7="$(live_session "$P7" 44444444-0000-4000-8000-000000000007)"
slot "$P7/.gopi" 0 "424248:$live7"
FOREIGN_LEDGER="$RIG/p7/foreign-ledger.db"
FOREIGN_PAYLOAD="{\"event\":\"completed\",\"task_id\":\"01a16515-dae5-76a2-88c9-c8f5a15a0c37\",\"ledger\":\"$FOREIGN_LEDGER\",\"status\":\"completed\",\"summary\":\"rule31: undelivered ground probe\"}"
: >"$RIG/p7.stub"
: >"$RIG/p7.err"
printf '%s' "$FOREIGN_PAYLOAD" | HOME="$RIG_HOME" GOPI_HOME="$P7" STUBLOG="$RIG/p7.stub" PATH="$STUB_DIR:$PATH" \
	bash "$SINK" 2>"$RIG/p7.err"
CASE_RC=$?
if [ "$CASE_RC" -ne 0 ]; then fail "case 7: sink exited $CASE_RC"; fi
assert_has "$RIG/p7.stub" "ledger=$FOREIGN_LEDGER" "case 7: the message names the ledger that emitted the event"
assert_has "$RIG/p7.stub" "task=01a16515-dae5-76a2-88c9-c8f5a15a0c37" "case 7: and still names the task id"
# THE CONTROL ARM: a payload with no origin ledger renders byte-identically to
# the pre-fix line — the ordinary case must not grow a `ledger=` fragment that
# reads as a missing value.
: >"$RIG/p7.stub"
printf '%s' "$PAYLOAD" | HOME="$RIG_HOME" GOPI_HOME="$P7" STUBLOG="$RIG/p7.stub" PATH="$STUB_DIR:$PATH" \
	bash "$SINK" 2>"$RIG/p7.err"
assert_lacks "$RIG/p7.stub" "ledger=" "case 7: an own-ledger event carries NO ledger fragment (byte-identical to before)"


if [ "$FAILS" -ne 0 ]; then
	printf 'notify_slot_contract_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf 'notify_slot_contract_test: all assertions passed (7 cases)\n'
exit 0
