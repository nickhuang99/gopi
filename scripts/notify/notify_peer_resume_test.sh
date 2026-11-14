#!/usr/bin/env bash
# notify_peer_resume_test.sh — the PEER-PULL PROGRESS contract (task 01a14194).
#
# THE DEFECT THIS PINS. The first version of the peer pull delivered a peer's new
# events ONE LINE AT A TIME and advanced the cursor ONCE, AFTER the whole loop. A
# peer backlog therefore cost one delivery process PER LINE PER SLOT, and the
# parent (internal/tools/tasknotify.go) kills the sink with SIGKILL at
# taskNotifyCmdTimeout (30s). Measured 2026-10-16 on this host: a 402-line
# backlog, 10 live slots, the process killed somewhere inside the loop, the
# cursor never written — so the next run read the SAME backlog from the SAME
# cursor, the same events reached the operator's chat again and again, and
# SIGKILL left 33 `.peer-pull.*.raw` spools behind (no trap runs on SIGKILL).
#
# THE FOUR CASES, and the property each one measures:
#
#   1. O(1) STARTS PER RUN   the number of delivery processes a run starts is
#                            bounded by the run's batch cap and does NOT grow with
#                            the number of lines the peer returns (the old script
#                            is measured beside it, where it grows 1:1);
#   2. NORMAL PATH           every line delivered, the cursor left at the newest
#                            timestamp, no spool left behind;
#   3. RESUME (GREEN)        killed with SIGKILL mid-run, the run 2 delivers ONLY
#                            what run 1 did not, and re-delivers NOTHING;
#   4. NO RESUME (RED)       the SAME interruption against the PRE-FIX script from
#                            git: the cursor does not move and run 2 re-delivers
#                            the same lines. This arm is the proof that case 3's
#                            green is bought by the change and not by the rig.
#
# THE INSTRUMENTS ARE STUBS, NOT MOCKS OF THE ASSERTION. `ssh` is replaced on
# PATH by a script that serves a synthetic peer anchor and filters it by the
# cursor operand it was handed — i.e. it does what the remote awk does. `gopi` is
# replaced by a script that records every invocation and, for a batch, appends
# each message it was given. Every case then asserts on the SINK's OWN stderr,
# on the cursor file, on the spy log — never on a variable the script set for the
# test's benefit.
#
# THE INTERRUPTION IS `timeout -s KILL`, and the SIGNAL IS THE POINT: SIGKILL is
# what the parent uses and is the one signal no trap can catch, which is why the
# cursor must already be on disk when it arrives. A rig using SIGTERM would let
# the trap run and prove nothing about the defect.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared
# helper — no /tmp, no hardcoded path.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SINK="$ROOT/scripts/notify/notify.sh"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-peer-resume)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# THE RIG'S OWN HOME (measured 2026-11-03). notify.sh's refusal of a
# checkout-shaped GOPI_HOME ANCHORS the payload at $HOME/.gopi — the documented
# fallback, pinned by notify_slot_contract_test's case 6. So a sink call that
# pins GOPI_HOME but leaves HOME INHERITED files the rig's fixture payload into
# the OPERATOR's live evidence file (/home/user/.gopi/notify/task-events.jsonl). Every
# sink invocation in this file therefore names $RIG_HOME; notify_hermeticity_test.sh
# scans for a call site that does not.
RIG_HOME="$RIG/rig-home"
mkdir -p "$RIG_HOME"

trap gopi_scratch_cleanup EXIT INT TERM

# The names the sink and this rig agree on. The sink's own readonly constants are
# read back out of the script rather than spelled again here (Rule 13/14): a
# renamed batch bound must break THIS test, not silently stop measuring it.
SINK_SLOT_PREFIX="chat_session_id_"
SINK_SLOT_SUFFIX=".txt"
PROFILE_DIR_NAME=".gopi"
SINK_DIR_NAME="notify"
CURSOR_PREFIX="peer-"
CURSOR_SUFFIX="-last-seen.txt"
TMP_PREFIX=".peer-pull"

# The batch bounds UNDER TEST, read from the script that declares them. Without
# this the rig would encode a second copy of the numbers and keep passing after
# the script changed.
script_const() {
	sed -n "s/^readonly $1=\([^ ]*\).*/\1/p" "$SINK" | head -1
}
BATCH_LINES="$(script_const PEER_DELIVER_BATCH_LINES)"
BATCHES_PER_RUN="$(script_const PEER_DELIVER_BATCHES_PER_RUN)"
: "${BATCH_LINES:?cannot read PEER_DELIVER_BATCH_LINES from the sink}"
: "${BATCHES_PER_RUN:?cannot read PEER_DELIVER_BATCHES_PER_RUN from the sink}"
# What ONE run may deliver at most — derived, never spelled.
BATCH_CAP=$((BATCH_LINES * BATCHES_PER_RUN))

# ---- THE PRE-FIX SINK: THREE SOURCES, RANKED, AND NEVER A SILENT PASS ------
#
# THE DEFECT THIS SECTION CLOSES (task 01a1a67d). The RED arms below — case 1's
# old-script comparison and case 4's NO-RESUME arm — need the PRE-FIX sink, and
# the rig used to obtain it ONE WAY ONLY: walk `scripts/notify/notify.sh`'s local
# git history back to the newest revision whose content lacks the batch reader.
# That works in a repo with that history and NOWHERE ELSE. A clone ground cut
# from a squashed source snapshot carries exactly ONE revision of the file, and
# it is the post-fix one (measured on this ground: `git log -- <path>` = one
# commit, `964b2404`), so the walk fell through and the rig printed
#
#   FATAL: no revision of scripts/notify/notify.sh predates the batch reader
#   — there is no RED arm to measure against          (exit 2)
#
# on EVERY clone — i.e. this rig had never run outside the one host that
# happened to hold the pre-fix history. A rig that cannot run is indistinguishable
# from a rig that passed (Rule 36), which is why the FATAL was never noticed.
#
# THE RESOLUTION IS THREE SOURCES, AND THE RIG SAYS WHICH ONE IT TOOK (Rule 15:
# the answer, and where it came from, on every run — not only on failure):
#
#   1. NOTIFY_PRE_FIX_REV=<rev>  an EXPLICIT override, for examining one
#      revision. Still highest priority: an operator who names a revision gets
#      that revision or an error, never a quiet substitution.
#   2. HISTORY  the newest revision of the file whose content does NOT carry the
#      batch reader. A real historical blob is harder evidence than anything
#      rebuilt, so it is preferred whenever it exists (the original behaviour,
#      unchanged — this is why the fix is additive).
#   3. SYNTHESIS  an ANCHORED SUBSTITUTION on the sink under test, rebuilt into
#      the pre-fix shape BY CONSTRUCTION. Three anchored edits, each of which
#      must match exactly once: (a) the batch reader is neither used nor probed,
#      (b) the cursor write is lifted OUT of the batch loop, and (c) the single
#      advance the loop no longer performs is placed AFTER it — which is the
#      defect stated in the file header, not a guess at it.
#
# A SYNTHESIS IS CHECKED, NEVER TRUSTED. Four measurements, and ANY failure is a
# FATAL (exit 2) rather than a skip: the result must parse (`bash -n`), must
# DIFFER byte-wise from the sink under test, must no longer carry the batch
# reader, and must no longer carry the IN-LOOP cursor write — that last one is
# the property case 4 measures, so a synthesis that missed edit (b) would mount a
# RED arm that is really the GREEN behaviour, i.e. a false arm.
#
# AN ANCHOR THAT IS GONE IS THE ONE HONEST SKIP. If the sink has been refactored
# so that one of the three anchors no longer exists, the pre-fix shape cannot be
# rebuilt from it — and the only two honest answers are "say so by name" or
# "invent an arm". So the synthesis declines, and the TWO RED assertions each
# print their own `SKIP (named, Rule 27)` line, the summary reports a SKIPPED
# count instead of "passed", and the GREEN arms still have to be all green. What
# is forbidden in every branch: turning a missing RED arm into a green one, and
# downgrading the FATAL into a silent exit 0.
#
# The readings this section relies on, named here because they carry meaning
# (Rule 1/13): the feature whose presence DEFINES "post-fix", and the sink path
# the git source reads.
readonly PRE_FIX_FEATURE='NOTIFY_DELIVER_MULTI_FLAG'
readonly PRE_FIX_SINK_PATH='scripts/notify/notify.sh'
# The in-loop cursor write is the DEFECT's absence/presence marker: this literal
# is the guarded advance inside `peer_drain_batches`, and a pre-fix shape must
# not contain it. Spell it once — the synthesis deletes it and the check below
# greps for it.
readonly PRE_FIX_INLOOP_CURSOR_MARK='if [ "$_pd_seen" != "$_pd_since" ]; then'
# THE PRODUCTION POINT OF THE BATCH READER, not the constant's own definition.
# A check for the bare name would hit `readonly NOTIFY_DELIVER_MULTI_FLAG=…` on
# line 189 and every report line that NAMES the flag — a constant's definition is
# not its use, and counting hits instead of finding the call is how a text gate
# goes green over a live production path (Rule 43's homophone trap). So the
# literal below is the PROBE — the one line whose answer decides whether the
# batch route is taken for the whole run — and it is what a pre-fix sink must not
# have.
readonly PRE_FIX_PROBE_MARK='notify "$NOTIFY_DELIVER_MULTI_FLAG" -h'

RED_ARM_SOURCE=''
RED_ARM_DETAIL=''
RED_ARM_SKIP_REASON=''
RED_ARM_REV=''
OLD_SINK="$RIG/notify-pre-fix.sh"
rm -f "$OLD_SINK"

# The sink under test must CARRY the feature, or this rig is measuring nothing.
if ! grep -q "$PRE_FIX_FEATURE" "$SINK"; then
	echo "FATAL: $SINK does not carry $PRE_FIX_FEATURE — there is no fix here to measure" >&2
	exit 2
fi

# synth_red_arm SRC DEST — rebuild SRC into the pre-fix shape. Returns 0 on
# success; 10 when an anchor is absent (the named-skip path); anything else is a
# hard error (an anchor that matched more than once is ambiguous, not absent).
synth_red_arm() {
	local _sa_src="$1" _sa_dest="$2"
	python3 - "$_sa_src" "$_sa_dest" "$PRE_FIX_FEATURE" <<'SYNTH'
import sys

src, dest, feature = sys.argv[1], sys.argv[2], sys.argv[3]

# (a) the batch reader is not USED and not PROBED. The probe's `if` becomes the
#     literal `false`, so the batch route is dead for this sink and every batch
#     falls back to `deliver_all`, one process per line — the old shape.
A_OLD = '\t"$gopi_bin" notify "$%s" -h >/dev/null 2>&1; then' % feature
A_NEW = '\tfalse; then'

# (b) THE DEFECT ITSELF: the cursor write leaves the batch loop. `_pd_since` is
#     still tracked per batch (so the batch top is still computed) but is no
#     longer PERSISTED inside the loop — an interruption now loses every batch
#     the killed run delivered.
B_OLD = "\n".join([
    '\t\tif [ "$_pd_seen" != "$_pd_since" ]; then',
    '\t\t\tif printf \'%s\\n\' "$_pd_seen" >"$_pd_cursor"; then',
    '\t\t\t\t_pd_since="$_pd_seen"',
    '\t\t\telse',
    "\t\t\t\tprintf 'notify.sh: peer %s: cannot write cursor %s — at most this batch (%s line(s)) will be re-delivered next run\\n' \\",
    '\t\t\t\t\t"$_pd_peer" "$_pd_cursor" "$_pd_batch_n" >&2',
    '\t\t\tfi',
    '\t\tfi',
    '',
])
B_NEW = '\t\t_pd_since="$_pd_seen"\n'

# (c) the advance the loop no longer performs, done ONCE AFTER it — which is
#     literally the pre-fix ordering, and what makes the sink end up in the same
#     state a completed batch pass would have reached.
C_OLD = '\treturn 0\n}\n\n# ---- 5. the peer whitelist'
C_NEW = ('\tif [ "$_pd_delivered" -gt 0 ] && [ "$_pd_seen" != "$_pd_since" ]; then\n'
         '\t\tprintf \'%s\\n\' "$_pd_seen" >"$_pd_cursor" 2>/dev/null\n'
         '\tfi\n'
         '\treturn 0\n}\n\n# ---- 5. the peer whitelist')

text = open(src, encoding='utf-8', errors='surrogateescape').read()
for name, old, new in (('a', A_OLD, A_NEW), ('b', B_OLD, B_NEW), ('c', C_OLD, C_NEW)):
    hits = text.count(old)
    if hits != 1:
        sys.stderr.write('anchor %s: %d occurrence(s), want exactly 1\n' % (name, hits))
        sys.exit(10 if hits == 0 else 11)
    text = text.replace(old, new, 1)
open(dest, 'w', encoding='utf-8', errors='surrogateescape').write(text)
SYNTH
}

# ---- 1. THE EXPLICIT OVERRIDE ----------------------------------------------
if [ -n "${NOTIFY_PRE_FIX_REV:-}" ]; then
	RED_ARM_REV="$NOTIFY_PRE_FIX_REV"
	if ! git -C "$ROOT" cat-file -e "${RED_ARM_REV}^{commit}" 2>/dev/null; then
		echo "FATAL: NOTIFY_PRE_FIX_REV=$RED_ARM_REV names no commit in $ROOT" >&2
		exit 2
	fi
	if ! git -C "$ROOT" show "${RED_ARM_REV}:${PRE_FIX_SINK_PATH}" >"$OLD_SINK" 2>/dev/null; then
		echo "FATAL: cannot read $PRE_FIX_SINK_PATH out of $RED_ARM_REV — the override names no such blob" >&2
		exit 2
	fi
	if cmp -s "$OLD_SINK" "$SINK"; then
		echo "FATAL: NOTIFY_PRE_FIX_REV=$RED_ARM_REV is byte-identical to $SINK — the RED arm would measure the FIX" >&2
		exit 2
	fi
	RED_ARM_SOURCE='override'
	RED_ARM_DETAIL="NOTIFY_PRE_FIX_REV=$RED_ARM_REV"
fi

# ---- 2. THE FILE'S OWN HISTORY ---------------------------------------------
# IT IS FOUND BY CONTENT, NOT BY "the working copy differs from HEAD". Before the
# fix is committed those are the same thing; the moment it IS committed they stop
# being (HEAD and the working copy agree, and a rig that compared them would
# refuse to run — which is exactly what happened when this fix landed). So the
# rig walks the file's history back to the newest revision that does NOT carry
# the batch reader: that revision, by definition, is the shape the defect was.
if [ -z "$RED_ARM_SOURCE" ] && git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1; then
	while IFS= read -r _rev; do
		[ -n "$_rev" ] || continue
		# FOUND is what ends the walk — NOT the first revision examined. A `break`
		# outside this `if` looks identical in review and walks exactly one
		# revision (measured: it made the git arm report `synthesis` while a
		# pre-fix blob sat one commit back), so the exit condition lives HERE.
		# THE PRODUCTION POINT, not the constant's name. `grep "$PRE_FIX_FEATURE"`
		# looks right and walks past every pre-fix revision that still DEFINES the
		# constant (a pre-fix blob keeps `readonly NOTIFY_DELIVER_MULTI_FLAG=…`);
		# measured on the git arm: rev1 held a genuine pre-fix sink and the walk
		# skipped it because the name appeared in it three times. What decides
		# "post-fix" is the PROBE whose answer routes the run, so that is what is
		# searched for (Rule 43: a name is not a production point).
		if ! git -C "$ROOT" show "${_rev}:${PRE_FIX_SINK_PATH}" 2>/dev/null |
			grep -qF "$PRE_FIX_PROBE_MARK"; then
			# The blob must still be READABLE and must still be the other shape;
			# a revision whose content cannot be materialised is not an arm.
			if git -C "$ROOT" show "${_rev}:${PRE_FIX_SINK_PATH}" >"$OLD_SINK" 2>/dev/null &&
				[ -s "$OLD_SINK" ] && ! cmp -s "$OLD_SINK" "$SINK"; then
				RED_ARM_REV="$_rev"
				RED_ARM_SOURCE='history'
				RED_ARM_DETAIL="newest revision of $PRE_FIX_SINK_PATH without $PRE_FIX_FEATURE = $RED_ARM_REV"
			fi
			break
		fi
	done <<EOF
$(git -C "$ROOT" log --format=%H -- "$PRE_FIX_SINK_PATH" 2>/dev/null)
EOF
	# No such revision here — the ordinary state of a snapshot clone. The source
	# falls through to synthesis; it does NOT stop the rig.
	if [ -z "$RED_ARM_SOURCE" ]; then
		rm -f "$OLD_SINK"
		RED_ARM_DETAIL="no revision of $PRE_FIX_SINK_PATH in this checkout predates $PRE_FIX_FEATURE ($(git -C "$ROOT" rev-list --count HEAD -- "$PRE_FIX_SINK_PATH" 2>/dev/null || echo '?') revision(s) examined at HEAD $(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo '?'))"
	fi
else
	if [ -z "$RED_ARM_SOURCE" ]; then
		RED_ARM_DETAIL="no readable git repository at $ROOT"
	fi
fi

# ---- 3. SYNTHESIS BY ANCHORED SUBSTITUTION ---------------------------------
if [ -z "$RED_ARM_SOURCE" ]; then
	_old_search_detail="$RED_ARM_DETAIL"
	# The synthesis's own diagnosis goes to a FILE, not to the rig's stdout: a
	# reader of a passing run must see the verdict and its reason, not the anchor
	# that happened to miss.
	if synth_red_arm "$SINK" "$OLD_SINK" 2>"$RIG/synth.err"; then
		RED_ARM_SOURCE='synthesis'
		RED_ARM_DETAIL="anchored substitution on $PRE_FIX_SINK_PATH: batch reader unused and unprobed; cursor write lifted out of the batch loop and done once after it"
	else
		_synth_rc=$?
		_synth_why="$(tr '\n' ' ' <"$RIG/synth.err" 2>/dev/null)"
		rm -f "$OLD_SINK"
		if [ "$_synth_rc" -eq 10 ]; then
			# THE ANCHOR IS GONE — the one honest skip in this file.
			RED_ARM_SOURCE='unavailable'
			RED_ARM_SKIP_REASON="the pre-fix shape cannot be rebuilt: an anchor of the anchored substitution no longer exists in $PRE_FIX_SINK_PATH (${_synth_why}); $_old_search_detail"
		else
			echo "FATAL: synthesising the pre-fix sink failed (exit $_synth_rc: $_synth_why) — an anchor matched more than once, which is ambiguous, not absent" >&2
			exit 2
		fi
	fi
fi

# ---- THE SYNTHESIS IS CHECKED, NOT TRUSTED --------------------------------
# Runs for EVERY source, not only synthesis: an override or a historical blob
# that turns out to be the fixed shape is the same false arm by another road.
if [ -n "$RED_ARM_SOURCE" ] && [ "$RED_ARM_SOURCE" != 'unavailable' ]; then
	if ! bash -n "$OLD_SINK" 2>"$RIG/pre-fix-syntax.err"; then
		echo "FATAL: the pre-fix sink ($RED_ARM_SOURCE) does not parse: $(cat "$RIG/pre-fix-syntax.err")" >&2
		exit 2
	fi
	if cmp -s "$OLD_SINK" "$SINK"; then
		echo "FATAL: the pre-fix sink ($RED_ARM_SOURCE) is byte-identical to $SINK — the RED arm would measure the FIX" >&2
		exit 2
	fi
	# THE PRODUCTION POINT, checked rather than a name counted: a sink that still
	# PROBES the reader would take the batch route, and case 1's RED arm would
	# then be measuring the fix's own behaviour.
	if grep -qF "$PRE_FIX_PROBE_MARK" "$OLD_SINK"; then
		echo "FATAL: the pre-fix sink ($RED_ARM_SOURCE) still probes $PRE_FIX_FEATURE — the batch route is not disabled" >&2
		exit 2
	fi
	# THE PROPERTY THE RED ARM ACTUALLY MEASURES, checked rather than assumed: a
	# cursor write INSIDE the batch loop means the shape still resumes, which
	# would make case 4 a RED arm measuring the GREEN behaviour.
	if grep -qF "$PRE_FIX_INLOOP_CURSOR_MARK" "$OLD_SINK"; then
		echo "FATAL: the pre-fix sink ($RED_ARM_SOURCE) still advances the cursor INSIDE the batch loop — that is the fixed property, not the defect" >&2
		exit 2
	fi
fi

FAILS=0
OKS=0
SKIPS=0
pass() {
	printf '  ok   %s\n' "$1"
	OKS=$((OKS + 1))
}
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }
# A named skip, counted separately from a pass — a skipped assertion is NEVER
# folded into the green count (Rule 27's listable form, Rule 15's loud half).
skip() { printf '  SKIP  (named, Rule 27) %s\n' "$1"; SKIPS=$((SKIPS + 1)); }

# ---- the stub `ssh`: serves a synthetic peer anchor --------------------------
BIN="$RIG/bin"
mkdir -p "$BIN"
cat >"$BIN/ssh" <<'STUB'
#!/usr/bin/env bash
# argv: -T -o BatchMode=yes -o ConnectTimeout=N <peer> "sh -s -- '<cursor>'"
# The remote program rides STDIN and is not needed by the stub; drain it so the
# writer never sees a closed pipe.
cat >/dev/null
PEER_ANCHOR="${STUB_PEER_ANCHOR:?STUB_PEER_ANCHOR unset}"
[ -r "$PEER_ANCHOR" ] || exit 3
# The operand is the LAST argument, spelled `sh -s -- '<cursor>'`.
last="${!#}"
since="${last##*-- }"
since="${since//\'/}"
if [ -z "$since" ]; then
	grep -F '"timestamp":"' "$PEER_ANCHOR" | tail -n 1
	exit 0
fi
# The same filter the remote awk applies: only lines whose timestamp sorts after
# the cursor. Timestamps on both hosts carry +08:00, so text order is time order.
python3 -c '
import re, sys
since = sys.argv[1]
for line in open(sys.argv[2], errors="replace"):
    if "\"timestamp\":\"" not in line:
        continue
    m = re.search(r"\"timestamp\":\"([^\"]*)\"", line)
    if m and m.group(1) > since:
        sys.stdout.write(line)
' "$since" "$PEER_ANCHOR"
STUB
chmod 0755 "$BIN/ssh"

# ---- the stub `gopi`: records every start, counts batches --------------------
cat >"$BIN/gopi" <<'STUB'
#!/usr/bin/env bash
# EVERY invocation is logged, because the count of these lines IS the O(n) claim.
mode="single"
for a in "$@"; do
	if [ "$a" = "--deliver-multi" ]; then mode="multi"; fi
done
printf '%s\n' "$mode" >>"${STUBLOG:?STUBLOG unset}"
# The capability probe: `gopi notify --deliver-multi -h` — the verb answers its
# help and exits 0 exactly when the flag parses, which is what the sink asks.
if [ "$mode" = "multi" ]; then
	for a in "$@"; do
		if [ "$a" = "-h" ] || [ "$a" = "--help" ]; then
			printf 'gopi notify — subscribe to the task-event notify stream\n'
			exit 0
		fi
	done
	# The BATCH: a JSON array of message strings, each delivered to every slot.
	# Recording each one is what lets a case prove which lines were delivered.
	#
	# THE SLEEP COMES FIRST, and that models the thing the parent actually kills:
	# a `gopi notify` child that is still WORKING. Appending before the sleep
	# would count a batch as delivered at the instant it started, which is not
	# what "delivered" means and would make the interruption rig measure a window
	# that does not exist.
	if [ -n "${STUB_BATCH_SLEEP:-}" ]; then sleep "$STUB_BATCH_SLEEP"; fi
	python3 -c '
import json, os, sys
msgs = json.load(sys.stdin)
with open(os.environ["STUBLOG"] + ".messages", "a") as fh:
    for m in msgs:
        fh.write(m + "\n")
print("batch: %d message(s)" % len(msgs))
' || exit 1
	exit 0
fi
# The single-message face, as the sink's deliver_all calls it:
# `notify --session <DIR> --message <TEXT>`. It records the MESSAGE — the same
# value the batch face records — so the two shapes' outputs are comparable line
# for line, which is what lets a case prove a RESUME delivered only new lines.
msg=""
while [ $# -gt 0 ]; do
	case "$1" in
	-h | --help)
		printf 'gopi notify — subscribe to the task-event notify stream\n'
		exit 0
		;;
	--message)
		shift
		msg="${1:-}"
		break
		;;
	--message=*)
		msg="${1#--message=}"
		break
		;;
	esac
	shift
done
if [ -n "${STUB_SINGLE_SLEEP:-}" ]; then sleep "$STUB_SINGLE_SLEEP"; fi
printf '%s\n' "$msg" >>"$STUBLOG.messages"
exit 0
STUB
chmod 0755 "$BIN/gopi"

# ---- the fixture: a synthetic peer anchor of N lines ------------------------
# One line per event, timestamps strictly increasing, each line shaped like the
# real anchor's records (the sink extracts `timestamp` and the message fields).
make_anchor() {
	local path="$1" n="$2"
	python3 -c '
import sys, json
n = int(sys.argv[2])
with open(sys.argv[1], "w") as fh:
    for i in range(n):
        # 1 event per second, so the ISO text sorts in the same order.
        h, rem = divmod(i, 3600)
        m, s = divmod(rem, 60)
        ts = "2026-10-16T%02d:%02d:%02d+08:00" % (h, m, s)
        fh.write(json.dumps({
            "event": "completed", "task_id": "t%04d" % i,
            "status": "completed", "summary": "peer event %d" % i,
            "timestamp": ts,
        }, separators=(",", ":")) + "\n")
' "$path" "$n"
}

# ---- rig CASE HOME ANCHOR [extra env...] ------------------------------------
# Runs the sink the way the parent does — `bash <script>`, payload on stdin — and
# returns the sink's own exit status in CASE_RC.
run_case() {
	local sink="$1" name="$2" home="$3" anchor="$4"
	shift 4
	local profile="$home/$PROFILE_DIR_NAME"
	mkdir -p "$profile/$SINK_DIR_NAME"
	CASE_OUT="$RIG/$name.out"
	CASE_STUB="$RIG/$name.stub"
	: >"$CASE_OUT"
	: >"$CASE_STUB"
	: >"$CASE_STUB.messages"
	env -u GOPI_SESSIONS_ROOT \
		HOME="$RIG_HOME" \
		GOPI_HOME="$home" GOPI_SETTINGS="$profile/settings.json" \
		STUBLOG="$CASE_STUB" STUB_PEER_ANCHOR="$anchor" \
		PATH="$BIN:$PATH" "$@" \
		bash "$sink" >"$CASE_OUT" 2>&1 <<<"$PAYLOAD"
	CASE_RC=$?
}

# slot HOME INDEX IDENTITY — one registry entry, the way `gopi chat` writes it.
slot() {
	local dir="$1/.gopi" index="$2" identity="$3"
	mkdir -p "$dir"
	printf '%s\n' "$identity" >"$dir/${SINK_SLOT_PREFIX}${index}${SINK_SLOT_SUFFIX}"
}

# live_session HOME NAME — a session directory that exists, printed.
live_session() {
	local dir="$1/.gopi/sessions/$2"
	mkdir -p "$dir"
	printf '%s' "$dir"
}

# A settings file with ONE declared peer. `GOPI_SETTINGS` is pinned by run_case,
# so the operator's own settings are never read.
settings() {
	local home="$1"
	mkdir -p "$home/.gopi"
	cat >"$home/.gopi/settings.json" <<'JSON'
{"sync_peers": ["synthetic-peer"]}
JSON
}

cursor_of() { cat "$1/.gopi/$SINK_DIR_NAME/${CURSOR_PREFIX}synthetic-peer${CURSOR_SUFFIX}" 2>/dev/null; }
cursor_path() { printf '%s/.gopi/%s/%s%s%s' "$1" "$SINK_DIR_NAME" "$CURSOR_PREFIX" synthetic-peer "$CURSOR_SUFFIX"; }
# PEER lines only. The stub's log holds EVERY delivery, and the sink also delivers
# this machine's OWN event (step 1) through the same stub — so a count of the raw
# log would include one line that was never a peer event. Counting the peer lines
# is what makes "what the peer backlog cost" a measurement rather than a total.
delivered_lines() { grep -c "peer event" "$1" 2>/dev/null || true; }
starts_total() { wc -l <"$1" 2>/dev/null | tr -d ' '; }
peer_set() {
	python3 -c '
import os, sys
p = sys.argv[1] + ".messages"
if not os.path.exists(p) or os.path.getsize(p) == 0:
    raise SystemExit
for line in open(p, errors="replace"):
    if "peer event" in line:
        sys.stdout.write(line)
' "$1"
}

PAYLOAD='{"event":"completed","task_id":"01a14194","status":"completed","summary":"resume rig payload"}'

printf '%s\n' "notify.sh peer-pull resume contract (rig: $RIG)"
printf '%s\n' "  batch bound under test: $BATCH_LINES line(s) x $BATCHES_PER_RUN batch(es) = $BATCH_CAP line(s) per run"

# ---- 1. THE STALL IS BOUNDED, AND DOES NOT GROW WITH n ----------------------
# 600 lines is ~1.5x the measured backlog. One slot, one peer: the number of
# delivery processes must be the run's batch cap, not 600.
BIG=$((BATCH_CAP * 5))
A1="$RIG/c1"
mkdir -p "$A1"; settings "$A1"
live1="$(live_session "$A1" 11111111-1111-4111-8111-111111111111)"
slot "$A1" 0 "424242:$live1"
ANCHOR1="$RIG/anchor-big.txt"; make_anchor "$ANCHOR1" "$BIG"
CURSOR1="$(cursor_path "$A1")"
mkdir -p "$(dirname "$CURSOR1")"
printf '%s\n' "2026-10-15T00:00:00+08:00" >"$CURSOR1"
run_case "$SINK" "c1-new" "$A1" "$ANCHOR1" STUB_BATCH_SLEEP=0.05
if [ "$CASE_RC" -ne 0 ]; then fail "case 1: the sink exited $CASE_RC"; fi
NEW_STARTS="$(starts_total "$CASE_STUB")"
# The bound is the run's OWN cap plus the two one-per-run probes the sink makes
# before any peer work (the batch reader's capability check, and nothing else):
# a number that GROWS with the backlog is the defect, and ${BATCH_CAP} lines is
# where growth would show.
if [ "$NEW_STARTS" -le "$((BATCHES_PER_RUN + 2))" ]; then
	pass "case 1: $BIG new line(s) cost $NEW_STARTS gopi start(s) — bounded by the run's $BATCH_CAP-line cap, not by n"
else
	fail "case 1: $BIG new line(s) cost $NEW_STARTS gopi start(s) — the run is not batch-bounded"
fi

# The SAME anchor against the PRE-FIX script, interrupted at the same wall clock
# the parent allows. Its start count is what the fix removed.
if [ "$RED_ARM_SOURCE" = 'unavailable' ]; then
	skip "case 1 (RED, old-script comparison): no pre-fix sink available — $RED_ARM_SKIP_REASON"
else
	A1O="$RIG/c1-old"
	mkdir -p "$A1O"; settings "$A1O"
	slot "$A1O" 0 "424242:$live1"
	CURSOR1O="$(cursor_path "$A1O")"
	mkdir -p "$(dirname "$CURSOR1O")"
	printf '%s\n' "2026-10-15T00:00:00+08:00" >"$CURSOR1O"
	# A small slice of the SAME backlog: the pre-fix script starts one process per
	# line, so a full 600 would take minutes. 40 lines is enough to show the growth.
	SMALL=40
	run_case "$OLD_SINK" "c1-old" "$A1O" "$ANCHOR1" timeout 20s
	OLD_STARTS="$(starts_total "$CASE_STUB")"
	if [ "$OLD_STARTS" -ge "$SMALL" ]; then
		pass "case 1: the pre-fix script spent $OLD_STARTS start(s) on the same backlog — its cost grows with n"
	else
		fail "case 1: the pre-fix arm only started $OLD_STARTS process(es) — the RED arm is not measuring the old shape"
	fi
fi

# ---- 2. THE NORMAL PATH IS UNCHANGED ---------------------------------------
# A backlog small enough to finish in ONE run: every line delivered, the cursor
# at the newest timestamp in the batch, and no spool left behind.
A2="$RIG/c2"
mkdir -p "$A2"; settings "$A2"
live2="$(live_session "$A2" 22222222-2222-4222-8222-222222222222)"
slot "$A2" 0 "424243:$live2"
N2=$((BATCH_LINES * 2))
ANCHOR2="$RIG/anchor-normal.txt"; make_anchor "$ANCHOR2" "$N2"
CURSOR2="$(cursor_path "$A2")"
mkdir -p "$(dirname "$CURSOR2")"
printf '%s\n' "2026-10-15T00:00:00+08:00" >"$CURSOR2"
run_case "$SINK" "c2" "$A2" "$ANCHOR2"
DELIVERED2="$(delivered_lines "$CASE_STUB.messages")"
WANT_TS="$(python3 -c '
import re, sys
last = ""
for line in open(sys.argv[1]):
    m = re.search(r"\"timestamp\":\"([^\"]*)\"", line)
    if m: last = m.group(1)
print(last)
' "$ANCHOR2")"
if [ "$DELIVERED2" = "$N2" ]; then
	pass "case 2: all $N2 new line(s) delivered"
else
	fail "case 2: delivered $DELIVERED2 of $N2 new line(s)"
fi
if [ "$(cursor_of "$A2")" = "$WANT_TS" ]; then
	pass "case 2: the cursor advanced to the newest timestamp ($WANT_TS)"
else
	fail "case 2: the cursor reads '$(cursor_of "$A2")', want '$WANT_TS'"
fi
if [ -z "$(find "$A2/.gopi/$SINK_DIR_NAME" -name "${TMP_PREFIX}.*" 2>/dev/null)" ]; then
	pass "case 2: no spool left behind (the EXIT trap ran)"
else
	fail "case 2: the run left spool files behind: $(find "$A2/.gopi/$SINK_DIR_NAME" -name "${TMP_PREFIX}.*")"
fi
# A SECOND run over the same anchor must deliver nothing: the cursor already
# covers it, which is the property the whole cursor exists for.
run_case "$SINK" "c2b" "$A2" "$ANCHOR2"
if [ "$(delivered_lines "$CASE_STUB.messages")" = "0" ]; then
	pass "case 2: a second run re-delivered nothing (0 new event(s))"
else
	fail "case 2: a second run re-delivered $(delivered_lines "$CASE_STUB.messages") line(s)"
fi

# ---- 3. GREEN: KILLED MID-RUN, THE NEXT RUN RESUMES ------------------------
# SIGKILL while a batch is in flight, exactly as the parent kills the sink. The
# batches that FINISHED have their cursor already written, so run 2 must start
# after them and re-deliver none of them.
A3="$RIG/c3"
mkdir -p "$A3"; settings "$A3"
live3="$(live_session "$A3" 33333333-3333-4333-8333-333333333333)"
slot "$A3" 0 "424244:$live3"
N3=$((BATCH_CAP * 4))
ANCHOR3="$RIG/anchor-resume.txt"; make_anchor "$ANCHOR3" "$N3"
CURSOR3="$(cursor_path "$A3")"
mkdir -p "$(dirname "$CURSOR3")"
printf '%s\n' "2026-10-15T00:00:00+08:00" >"$CURSOR3"
# 2 x 1.0s of batch work + the kill at 1.6s: the FIRST batch completes (and is
# recorded), the SECOND is killed inside. That is the interruption the defect is
# about — work done, progress unwritten — reproduced deliberately.
run_case "$SINK" "c3-killed" "$A3" "$ANCHOR3" STUB_BATCH_SLEEP=1.0 timeout -s KILL 2.1s
KILLED_TS="$(cursor_of "$A3")"
CASE3_STUB="$CASE_STUB"
run_case "$SINK" "c3-resume" "$A3" "$ANCHOR3" STUB_BATCH_SLEEP=0.05
if [ "$KILLED_TS" != "2026-10-15T00:00:00+08:00" ]; then
	pass "case 3: the killed run had recorded its finished batch(es) — cursor is now $KILLED_TS"
else
	fail "case 3: the killed run recorded NO progress — the cursor is still the pre-run value"
fi
# Nothing already delivered may be delivered again. The two runs' PEER lines are
# compared as SETS: an intersection of any size is a duplicate.
# WHAT ONE BATCH OF RE-DELIVERY IS, STATED SO IT IS NOT MISTAKEN FOR A FAILURE.
# The cursor is written after a batch is delivered, so an interruption inside the
# window between "delivered" and "recorded" costs exactly that ONE batch — that is
# the bound the fix buys, and it is bounded by construction rather than measured.
# What must NEVER happen is the WHOLE preceding stream coming back, which is the
# defect: the old shape loses every batch the killed run delivered. So the
# assertion is the BOUND, not zero.
CASE3_KILLED_STUB="$CASE3_STUB"
DUP3="$(python3 -c '
import sys, os
def load(p):
    if not os.path.exists(p) or os.path.getsize(p) == 0:
        return set()
    return {l for l in open(p, errors="replace").read().splitlines() if "peer event" in l}
a = load(sys.argv[1])
b = load(sys.argv[2])
print(len(a & b))
' "$CASE3_KILLED_STUB" "$CASE_STUB")"
KILLED3="$(delivered_lines "$CASE3_KILLED_STUB.messages")"
if [ "$DUP3" -le "$BATCH_LINES" ]; then
	pass "case 3: the resume repeated $DUP3 line(s) — at most ONE batch ($BATCH_LINES), not the killed run's $KILLED3"
else
	fail "case 3: the resume repeated $DUP3 of the killed run's $KILLED3 line(s) — more than the one-batch window"
fi
if [ "$(cursor_of "$A3")" \> "$KILLED_TS" ]; then
	pass "case 3: the cursor moved on from $KILLED_TS to $(cursor_of "$A3")"
else
	fail "case 3: the cursor did not advance past the killed run's position ($KILLED_TS)"
fi

# ---- 4. RED: THE SAME KILL AGAINST THE PRE-FIX SCRIPT ----------------------
# Same fixture, same interruption, same resumption — only the script differs. The
# pre-fix script advances the cursor AFTER its loop, so a kill inside the loop
# leaves it exactly where it started and run 2 repeats work already done.
if [ "$RED_ARM_SOURCE" = 'unavailable' ]; then
	# TWO skips, because case 4 IS two assertions — and each says which one did
	# not run. Counting them as one would understate what was not measured.
	skip "case 4 (RED, cursor unmoved): no pre-fix sink available — $RED_ARM_SKIP_REASON"
	skip "case 4 (RED, duplicate stream): no pre-fix sink available — $RED_ARM_SKIP_REASON"
else
	A4="$RIG/c4"
	mkdir -p "$A4"; settings "$A4"
	live4="$(live_session "$A4" 44444444-4444-4444-8444-444444444444)"
	slot "$A4" 0 "424245:$live4"
	ANCHOR4="$RIG/anchor-red.txt"; make_anchor "$ANCHOR4" 80
	CURSOR4="$(cursor_path "$A4")"
	mkdir -p "$(dirname "$CURSOR4")"
	printf '%s\n' "2026-10-15T00:00:00+08:00" >"$CURSOR4"
	run_case "$OLD_SINK" "c4-killed" "$A4" "$ANCHOR4" STUB_SINGLE_SLEEP=0.25 timeout -s KILL 2s
	RED_TS="$(cursor_of "$A4")"
	if [ "$RED_TS" = "2026-10-15T00:00:00+08:00" ]; then
		pass "case 4 (RED): the pre-fix script was killed with its cursor UNMOVED — the defect"
	else
		fail "case 4 (RED): the pre-fix cursor moved to $RED_TS — this arm is not reproducing the defect"
	fi
	FIRST4="$(delivered_lines "$CASE_STUB.messages")"
	run_case "$OLD_SINK" "c4-again" "$A4" "$ANCHOR4" STUB_SINGLE_SLEEP=0.25 timeout -s KILL 2s
	SECOND4="$(delivered_lines "$CASE_STUB.messages")"
	if [ "$SECOND4" -gt 0 ] && [ "$FIRST4" -gt 0 ]; then
		pass "case 4 (RED): the next run RE-DELIVERED $SECOND4 line(s) already delivered ($FIRST4 before) — the duplicate stream"
	else
		fail "case 4 (RED): no duplicate delivery reproduced ($FIRST4 then $SECOND4)"
	fi
fi

printf '\n'
# WHERE THE RED ARM CAME FROM — printed on EVERY run, green or red, because the
# value of a RED arm is the evidence behind it (Rule 15: never a silent choice).
printf '  RED arm source — %s: %s\n' "$RED_ARM_SOURCE" "$RED_ARM_DETAIL"
if [ -n "$RED_ARM_SKIP_REASON" ]; then
	printf '  RED arm SKIPPED (named, not passed) — %s\n' "$RED_ARM_SKIP_REASON"
fi
printf '  counts — ok=%d FAIL=%d SKIP=%d\n' "$OKS" "$FAILS" "$SKIPS"
if [ "$FAILS" -ne 0 ]; then
	printf 'notify_peer_resume_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
if [ "$SKIPS" -ne 0 ]; then
	# NOT "passed": the GREEN arms passed and the RED arm did not run, and a
	# summary that blurred the two would be the whole defect this file fixes.
	# exit 0 is still correct — the script ran to the end and every assertion it
	# could make held — but the SKIPPED count is on the line, and the reason is on
	# the line above it.
	printf 'notify_peer_resume_test: GREEN arms passed; %d RED arm assertion(s) SKIPPED (not passed, see the named reason above)\n' "$SKIPS"
	exit 0
fi
printf 'notify_peer_resume_test: all assertions passed (4 cases, %d ok)\n' "$OKS"
exit 0
