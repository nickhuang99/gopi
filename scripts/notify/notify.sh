#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# notify.sh — the SAMPLE `notify.cmd` sink (settings.json "notify"."cmd").
#
# WHAT IT IS. gopi hardcodes NO notification mechanism: it reads the shell
# command out of the configuration and execs it, handing the lifecycle event to
# the command on STDIN. Everything about where that event goes is therefore the
# COMMAND's business, not gopi's — and this file is the sample that shows the
# intended destination: the operator's chat console.
#
# THREE STEPS, and the order is the safety property:
#
#   1. deliver THIS machine's event to every registered chat console — the
#      profile's slot registry, the files `<profile>/chat_session_id_<0..9>.txt`
#      written by `gopi chat` at startup (cmd/agent/chat_session_slots.go). Each
#      slot holds one chat's surface identity, `<pid|self>:<session-directory>`,
#      and the delivery is one
#      `gopi notify --session <session-directory> --message <summary>` — i.e. the
#      message lands in that session's inbox and its console takes it at the next
#      round boundary. A refused delivery is REPORTED and the loop carries on:
#      one dead chat must not silence the other nine.
#
#   2. read PEER EVENTS this machine has not delivered yet — one `ssh` per
#      declared peer (settings.json "sync_peers"), reading the peer's
#      `<profile>/notify/task-events.jsonl` and keeping the lines whose
#      `"timestamp":"<ISO>"` sorts AFTER the one recorded in this machine's
#      cursor `<profile>/notify/peer-<peer>-last-seen.txt`. Those events go
#      through THE SAME slot loop as step 1 (`deliver_all`, called a second
#      time — there is no second delivery loop) tagged `[peer <alias>] ` so the
#      reader can tell where a message came from. A peer that cannot be reached
#      is REPORTED on stderr and contributes nothing: it never delays and never
#      aborts step 1 or step 3.
#
#   3. ALWAYS, last: append the RAW payload to
#      `<profile>/notify/task-events.jsonl`. This is the fallback ANCHOR — the
#      subscribers that read the stream (`gopi notify events`, the zombie-audit
#      recipes, anything that greps the file) keep working unchanged, even on a
#      machine where no chat is running at all and even when every slot above
#      refused. ONLY the local payload is anchored here: a peer's events already
#      exist in the peer's own anchor, and this machine's file is the anchor of
#      what this machine did. (So a peer's event is NOT copied into this file —
#      a reader of it sees exactly what it saw before the peer pull existed.)
#
# STDIN CONTENTION — the one subtlety of this shape. The payload arrives on
# stdin and a pipe is NOT seekable: it can be read exactly once. Anything this
# script spawns (the nine `gopi notify` children above) would otherwise inherit
# that same pipe, and a child that reads it — or merely closes it early — silently
# eats the bytes the anchor still needs. So the payload is captured into a shell
# variable FIRST, before any child is started, and every child is given
# `</dev/null` so none can touch the stream. Measured 2026-10-10: with the
# capture the anchor receives the complete payload; with a per-child `cat` the
# second reader gets nothing.
#
# NO RESIDENT DEPENDENCY (GOPI.md Rule 35). Nothing here ticks, schedules,
# watches or cleans: the script is exec'd by the already-existing notify
# delivery path, it does its work and exits. The slot files and the peer cursor
# files are the only state, the slot count is a fixed 10, and a stale slot costs
# one refused delivery in the loop below — there is no reaper and none is
# needed. The peer read is a ONE-SHOT `ssh` executed by an already-running
# delivery, never a poller, a timer or a daemon: with no notify there is no
# peer read, and the cursor is just a single-line text file.
#
# THE BOUNDED SETS. Slots: the number 10 and the name shape
# `chat_session_id_<n>.txt`, both defined by `gopi chat`
# (cmd/agent/chat_session_slots.go: ChatSessionSlotLimit /
# ChatSessionSlotPrefix+ChatSessionSlotSuffix). Peers: PEER_LIMIT below, derived
# from the child's own hard timeout, because this script is exec'd with a
# deadline it does not own (internal/tools/tasknotify.go: taskNotifyCmdTimeout).
#
# SAYING NOTHING WHEN NOTHING ARRIVED (task 01a12d05). Until this change the
# loop reported per-slot refusals and stopped there: a machine where EVERY
# registered chat refused — or where no chat was registered at all — produced the
# same shape of output as a machine where nine of ten were served, and the
# operator learned that their notifications had been lost eight hours later, from
# a task ledger that moved without them (Rule 15: 静默降级必须出声). Step 3b below
# is the line that makes the aggregate outcome audible, and step 3c counts the
# DEAD slots rather than leaving a reader to infer them from N refused lines.
set -u

readonly SLOT_LIMIT=10
readonly SLOT_PREFIX="chat_session_id_"
readonly SLOT_SUFFIX=".txt"
readonly PROFILE_DIR_NAME=".gopi"
readonly SINK_DIR_NAME="notify"
readonly SINK_FILE_NAME="task-events.jsonl"
readonly MESSAGE_SUMMARY_MAX=300
# The marker appended AFTER the cut point (never counted inside it — the byte
# budget is the cut, and the line was always at most MAX + this marker's 3 bytes).
readonly MESSAGE_TRUNCATION_MARK="…"
# UTF-8 LEAD-BYTE RANGES — the table `utf8_truncate` below walks, named here
# because each number is a rule from RFC 3629, not a value someone tuned:
# 0x00–0x7F is one byte on its own; a lead byte states how many bytes its
# character occupies; 0x80–0xBF is a CONTINUATION byte and never starts a
# character (0xC0/0xC1 are refused by the standard — they would encode an
# overlong form).
readonly UTF8_ONE_BYTE_MAX=127
readonly UTF8_TWO_BYTE_MIN=194
readonly UTF8_TWO_BYTE_MAX=223
readonly UTF8_THREE_BYTE_MIN=224
readonly UTF8_THREE_BYTE_MAX=239
readonly UTF8_FOUR_BYTE_MIN=240
readonly UTF8_FOUR_BYTE_MAX=244
# THE PARSE-FAILURE WORDING (task 01a1a640, defect B). A payload the field
# extractor can read NOTHING out of used to render as
# `task event: event=? task=? status=?` — three question marks that name no
# event, no task and no status, and that a reader cannot tell apart from a
# message whose fields merely happen to be empty. It is now spelled as a
# PARSE FAILURE plus the RAW line, because the one thing such an event still
# carries is its own bytes, and those are what a reader needs to diagnose it
# (Rule 15: a bad event must SAY SO, never be dropped and never be laundered
# into a placeholder that reads like data).
readonly MESSAGE_PARSE_FAILURE="PARSE FAILURE: no readable "
# The one-line message's own prefix, and the word a payload with NO bytes at all
# is reported with — `raw line: ` followed by nothing reads like a truncation of
# the report itself, so the empty case says so in words (Rule 15).
readonly MESSAGE_EVENT_PREFIX="task event: "
readonly MESSAGE_EMPTY_PAYLOAD="(empty — the producer sent no bytes at all)"
readonly MESSAGE_WHITESPACE_PAYLOAD="(blank — bytes arrived, but they carry no characters)"
# The checkout marker (internal/config's CheckoutMarkerName — the Go half of the
# same predicate) and this script's own name in its diagnostics.
readonly CHECKOUT_MARKER_NAME=".git"
readonly NOTIFY_SCRIPT_NAME="notify.sh"

# ---- settings-file constants ----------------------------------------------
# Resolved exactly as gopi resolves them (internal/config/config.go:
# ResolveSettingsPath = $GOPI_SETTINGS, else DefaultSettingsPath =
# <GopiDir>/settings.json; SyncPeersSettingsKey = "sync_peers").
readonly SETTINGS_PATH_ENV="GOPI_SETTINGS"
readonly SETTINGS_FILE_NAME="settings.json"
readonly SYNC_PEERS_KEY="sync_peers"

# ---- peer-pull constants ---------------------------------------------------
# ssh: BatchMode=yes (never prompt — nobody is there to answer, and a prompt
# would hang the delivery), ConnectTimeout bounds the CONNECT phase, -T forbids
# pty allocation. All three are spelled AT THE CALL SITE (peer_new_lines), each
# with its own shellcheck escape beside it, rather than through a shared options
# variable: a variable holding flags splits into bare words at the call.
#
# 10s is the smallest value with real headroom over the measured cost of this
# exact call on a healthy link (handshake + full remote scan 0.36-0.82s, see
# devdoc/notify-peer-pull.md), and it matches the precedent already in this repo
# (internal/sync: syncSSHConnectTimeout = 10).
readonly PEER_SSH_TIMEOUT=10
# How many peers one run may read. The child that execs this script is killed
# with its process group at 30s (taskNotifyCmdTimeout), and a peer with a
# blackholed address costs the whole ConnectTimeout: 2 * 10s leaves 10s for the
# local delivery, which measures ~0.13s. Beyond the limit the remaining peers are
# NAMED as skipped on stderr — a bound that is enforced and reported, never a
# silent truncation.
readonly PEER_LIMIT=2
readonly PEER_CURSOR_PREFIX="peer-"
readonly PEER_CURSOR_SUFFIX="-last-seen.txt"
readonly PEER_TMP_PREFIX=".peer-pull"

# ---- THE OPERATOR PARAMETER BLOCK (see also STATE_DIR below) --------------
# The three names a SECOND machine used to have to hand-edit in this file. They
# are read from the ENVIRONMENT first, so the deployment
# (`scripts/notify/deploy.sh`) writes them into ONE line of the profile's
# settings.json (`notify.cmd`) and the SAME script bytes serve every machine —
# an upgrade of this file then never has to be re-edited per host.
#
# These are NOT gopi configuration knobs: nothing in Go reads them, CONFIG.md is
# unchanged, and an UNSET variable is the documented default, so a copy of this
# file that no deployment has touched behaves exactly as it did before
# (NONE_VAR_COUNT gate: every name below is spelled once, here, and read at the
# single site named beside it).
#
#   GOPI_NOTIFY_STATE_DIR         where the per-peer cursor files and the health
#                                 records live — read ONCE, at STATE_DIR below.
#                                 Default: this profile's notify directory, i.e.
#                                 where they have always lived.
#   GOPI_NOTIFY_CURSOR_STALE_SEC  how long a peer cursor may stand still before
#                                 the health record calls it STALLED — read ONCE,
#                                 at cursor_stale_sec below.
#   GOPI_NOTIFY_PEER_LIMIT        how many peers one run may read — read ONCE, at
#                                 peer_limit below. The default stays PEER_LIMIT,
#                                 which is derived from the child's 30s deadline.
readonly STATE_DIR_ENV="GOPI_NOTIFY_STATE_DIR"
readonly CURSOR_STALE_SEC_ENV="GOPI_NOTIFY_CURSOR_STALE_SEC"
readonly PEER_LIMIT_ENV="GOPI_NOTIFY_PEER_LIMIT"
# 6 hours: long enough that a quiet night is not a stall, short enough that a
# broken pull is visible the same working day.
readonly CURSOR_STALE_SEC_DEFAULT=21600
# The reader that accepts a batch: `gopi notify --deliver-multi`, whose stdin is
# a JSON array of message strings (one `gopi notify` process per batch instead
# of one per line). The spelling lives HERE, once, and is pinned against the Go
# constant by cmd/agent/chat_session_slots_script_test.go — a renamed flag that
# drifted would turn every batch into an argument error.
readonly NOTIFY_DELIVER_MULTI_FLAG="--deliver-multi"

# ---- the DELIVERY BUDGET: why this script must finish inside a deadline -----
#
# THE DEFECT THIS BOUNDS (task 01a14194, measured 2026-10-16). The first version
# delivered a peer's new events ONE LINE AT A TIME and advanced the cursor once,
# AFTER the whole loop — so a peer that had been unreachable for two days handed
# back a 402-line backlog, 402 lines x 10 live slots = 4020 delivery processes,
# and the child was SIGKILLed at taskNotifyCmdTimeout (30s) somewhere inside the
# loop. The cursor write never ran, so the next run read the SAME backlog from
# the SAME stale cursor and repeated it (measured: two 295,389-byte spools, byte
# for byte identical, minutes apart), and SIGKILL gave the EXIT trap no chance to
# remove the spool (measured: 13 `.peer-pull.*.raw` litter files, 3.7 MB).
#
# THE INVARIANT THIS ESTABLISHES. Work is delivered in BATCHES, and the cursor is
# written BETWEEN batches (inside the loop, after each `deliver_all`), so the
# progress record always describes what has actually been delivered. From that
# one change: an interruption costs at most ONE batch of re-delivery, the NEXT
# run resumes instead of restarting, and the number of delivery processes per run
# is bounded by `batches x slots` — a CONSTANT that does not grow with the number
# of lines the peer returns (O(n) starts -> O(1) starts per run).
#
# AND THE DEADLINE IS NOW ENFORCED HERE, not only by the parent's SIGKILL. The
# parent's 30s kill is a SIGKILL: no trap, no unwinding, no last words. Every
# bound below exists so this script stops ITSELF, at a batch boundary, with its
# cursor already written — the difference between "deferred to the next run" and
# "lost until someone notices".
readonly TASK_NOTIFY_CMD_TIMEOUT_SEC=30 # internal/tools/tasknotify.go: taskNotifyCmdTimeout
# The budget this script allows ITSELF, DERIVED from the parent's deadline minus
# a margin rather than spelled a second time: the two numbers can then never
# disagree, and raising the parent's timeout moves this one with it. The margin
# (5s) is what the rest of the run needs — step 3's anchor append, the EXIT trap,
# process teardown — so stopping here is a graceful stop and not a race with the
# parent's SIGKILL. All local work (both peers' ssh legs AND their deliveries)
# stops at this budget: ONE budget for the whole run, not one per peer, because
# two peers each allowed their own could overrun the parent's kill together.
readonly NOTIFY_BUDGET_SAFETY_SEC=5
readonly NOTIFY_RUN_BUDGET_SEC=$((TASK_NOTIFY_CMD_TIMEOUT_SEC - NOTIFY_BUDGET_SAFETY_SEC))
# Hard cap on ONE peer's ssh leg, enforced with timeout(1) rather than by
# ConnectTimeout alone: ConnectTimeout bounds the CONNECT phase, so a peer that
# accepts the connection and then stalls while scanning its anchor would hold the
# leg for as long as it likes. The leg is also clipped to whatever is LEFT of the
# run budget, whichever is smaller.
readonly PEER_REMOTE_MAX_SEC=15
# How many peer lines go into ONE delivery message. Two reasons for a bound at
# all: the round trip per line is what made this O(n) in the first place, and a
# message approaching the session inbox's own line capacity
# (internal/lineedit: InputLineRuneBound, 64 KiB) is SPILLED to a paste file
# instead of being delivered as text — a shape this script should not depend on.
# 40 lines is roughly 28 KB at the measured ~700 bytes/line: comfortably inside
# the bound, and large enough that the per-line overhead (field extraction) is
# paid 120 times per run rather than 4020.
readonly PEER_DELIVER_BATCH_LINES=40
# How many batches one peer may deliver per run. With the two above this caps a
# run at 120 lines/peer — enough to drain the measured 402-line backlog in four
# notify events, and small enough that even 10 live slots cost ~120 delivery
# processes against a budget measured in seconds (~30ms per delivery).
readonly PEER_DELIVER_BATCHES_PER_RUN=3

# ---- 0. REFUSE A PROFILE THAT LIVES INSIDE A CODE CHECKOUT ------------------
# THE MEASURED DEFECT this closes (2026-10-15). GOPI_HOME is documented as a
# HOME, and the profile root is `<GOPI_HOME>/.gopi`. A run standing in its own
# git worktree can hand that worktree down as GOPI_HOME (a launcher's export, a
# script setting it to `$PWD`, a checkout that IS the sandbox), so this script
# resolved `PROFILE=<worktree>/.gopi` and appended the anchor into it: the event
# was written into a THROWAWAY profile that nothing sweeps, dispatches from or
# reads back, the delivery into it vanished silently, and the checkout went
# DIRTY — which parks the merge queue on "tracked files modified". Nothing here
# refused that shape before; this is the missing half.
#
# THE TEST IS THE MARKER, NOT THE PATH SHAPE: `$GOPI_HOME/.git` existing is a
# FACT about the value, so a SANDBOX home (`GOPI_HOME=$SB`, a real directory
# holding `.gopi`) carries no marker and keeps working exactly as before — that
# documented path must never be refused. The Go half of the same predicate is
# internal/config's GopiHomeNamesCodeCheckout (CheckoutMarkerName); keep the two
# spellings one (Rule 13/14: the constant in Go, the readonly here).
#
# WHERE TO STILL WRITE THE EVENT. Refusing to anchor it at all would lose the
# delivery — the very failure this guard exists to stop. So the payload is
# ANCHORED where the value should have pointed: the operator's own profile
# ($HOME/.gopi), and the refusal says so, naming the value, the reason and the
# destination (Rule 15: never silent). Nothing is created inside the checkout.
refuse_checkout_home() {
	_rc_value="$1"
	_rc_fallback_profile="${HOME:-}/${PROFILE_DIR_NAME}"
	printf '%s: REFUSING %s=%s — it names a CODE CHECKOUT (it carries %s), so the profile root would be %s/%s: a throwaway profile inside the checkout that nothing sweeps, nothing dispatches from and nothing reads back. The event is anchored in %s/%s/%s instead, and NOT inside the checkout. Point %s at a real home (e.g. %s) or at a sandbox.\n' \
		"$NOTIFY_SCRIPT_NAME" "$GOPI_HOME_ENV_NAME" "$_rc_value" "$CHECKOUT_MARKER_NAME" \
		"$_rc_value" "$PROFILE_DIR_NAME" \
		"$_rc_fallback_profile" "$SINK_DIR_NAME" "$SINK_FILE_NAME" \
		"$GOPI_HOME_ENV_NAME" "${HOME:-}" >&2
}

# THE PROFILE ROOT, resolved the way every gopi path resolves it
# (internal/config/gopihome.go: GopiDir = $GOPI_HOME/.gopi when GOPI_HOME is set,
# else $HOME/.gopi).
GOPI_HOME_ENV_NAME="GOPI_HOME"
if [ -n "${GOPI_HOME:-}" ] && [ -e "${GOPI_HOME}/${CHECKOUT_MARKER_NAME}" ]; then
	refuse_checkout_home "$GOPI_HOME"
	PROFILE="${HOME:-}/${PROFILE_DIR_NAME}"
elif [ -n "${GOPI_HOME:-}" ]; then
	PROFILE="${GOPI_HOME}/${PROFILE_DIR_NAME}"
else
	PROFILE="${HOME}/${PROFILE_DIR_NAME}"
fi
# A PROFILE THAT IS A GIT WORKTREE IS REFUSED, BY NAME (measured 2026-10-17 on
# this host). `GOPI_HOME` is inherited by every child a gopi process starts, and
# a worker/merger run exports its OWN ground — so a child exec'd from inside such
# a run resolves a $HOME that is a checkout. Delivery would then land in
# `<worktree>/.gopi/`, whose slot registry is empty: the notification is lost
# with no error anywhere, and the worktree gets a `.gopi/` it never asked for
# (GOPI.md Rule 30 — a run leaves its worktree clean).
#
# THE PREDICATE IS THE `.git` ENTRY, not the directory's mode: an operator's real
# profile was measured at mode 775 on this host, so a permission test would
# refuse the very profile the sink exists to serve. A `.git` node at the profile
# root or at GOPI_HOME is what a checkout has and a profile never does.
profile_usable=1
if [ -n "${GOPI_HOME:-}" ]; then
	if [ -e "${GOPI_HOME}/.git" ] || [ -e "${PROFILE}/.git" ]; then
		printf 'notify.sh: GOPI_HOME=%s is a git worktree, not a profile — this is a run-exported home leaked into a child. Slot/peer work is SKIPPED (nothing is written into %s); the raw payload is still anchored at %s\n' \
			"$GOPI_HOME" "$PROFILE" "${HOME}/${PROFILE_DIR_NAME}/${SINK_DIR_NAME}" >&2
		profile_usable=0
	fi
fi
SINK_DIR="${PROFILE}/${SINK_DIR_NAME}"
SINK_FILE="${SINK_DIR}/${SINK_FILE_NAME}"
# THE STATE (cursor files + health records) lives in SINK_DIR unless the
# deployment says otherwise — the parameter block above explains why it is an
# env-overridable name rather than a second hardcoded path. Expanded EXACTLY
# ONCE, here; nothing below reads the variable again.
STATE_DIR="${!STATE_DIR_ENV:-}"
[ -n "$STATE_DIR" ] || STATE_DIR="$SINK_DIR"

# THE SETTINGS FILE, same two channels and the same precedence gopi uses.
settings_path="${!SETTINGS_PATH_ENV:-}"
[ -n "$settings_path" ] || settings_path="${PROFILE}/${SETTINGS_FILE_NAME}"

# THE PEER'S OWN ANCHOR, spelled from the same names this machine uses: a peer's
# profile root is its own business, but the stream it writes is this contract.
readonly PEER_ANCHOR_REL="${PROFILE_DIR_NAME}/${SINK_DIR_NAME}/${SINK_FILE_NAME}"

# WHERE THE SLOT REGISTRY IS — the SAME derivation the WRITER uses
# (internal/config/sessions_root.go: ChatSlotsDir is the directory the session
# tree lives in, which is the profile root unless the run relocated its own tree
# with GOPI_SESSIONS_ROOT). Reading $GOPI_SESSIONS_ROOT here is what keeps the
# two ends of this channel pointed at ONE tree: a run that filed its slot into
# its own scratch (a replay/film run — cmd/agent/chat_sessions_root.go) is
# reachable by a sink exec'd from that same process tree, and an ordinary sink
# (the variable unset, which is every production worker and merger) scans the
# profile exactly as before. Nothing is created here and nothing is required:
# an unset or empty value is the documented default, and an exported one that
# does not exist simply yields no slots, which step 3b now reports.
if [ -n "${GOPI_SESSIONS_ROOT:-}" ]; then
	SLOT_DIR="$(dirname "$GOPI_SESSIONS_ROOT")"
else
	SLOT_DIR="$PROFILE"
fi



payload="$(cat)"

# ---- 2. extract one string field from a payload ----------------------------
# The delivery target is a chat CONSOLE, so what it needs is the fact, not the
# whole record: the event, the task, the status and the task's own summary. The
# raw payload is never lost — it is what step 5 appends. `field` takes the
# payload as an ARGUMENT (not from a captured global) so the same extractor
# serves the local payload and every peer line.
#
# THE VALUE IS DECODED ONCE, AND THAT IS THE WHOLE CONTRACT (task
# 01a1a61b-9e80, the `\u003e` defect). The payload arrived as JSON and the
# field's VALUE inside it is a JSON STRING: `>` is legal, but encoding/json
# writes it as the escape `\u003e` (HTML escaping is on by default), and the
# same is true of `<`, `&`, `"` and every non-ASCII character. The extractor
# used to hand that ESCAPED body back as if it were the text — a `sed` regex
# cannot decode — so the literal six bytes `\u003e` travelled on as content,
# got marshalled by the inbox writer into `\\u003e` on disk, and every reader
# that decoded once saw `\u003e` instead of `>`. Measured: 78 such literals in
# one PA session and 0 real `>` in the notify-delivered records of its inbox.
#
# WHAT CHANGED, AND WHAT DID NOT. The extraction is now a real scan of the JSON
# string (escaped quotes no longer truncate the value, which the old
# `\([^"]*\)` did), followed by exactly ONE JSON decode — so the value that
# leaves here is the text the producer meant. The caller's contract is
# unchanged: a field that is absent still yields the empty string, never an
# error, because a payload without a `ledger` is the ordinary case.
#
# ONE DECODE, NOT A DISPLAY FIX. Nothing downstream patches this up: the inbox
# writer and the session record are the same encoders they were, and the
# reader decodes what any JSON reader decodes. A value that arrived correct
# stays correct through the whole chain; a `sed` that re-escaped at read time
# would only move the lie to the reader (the same refusal 01a1a5f1 made).
#
# json_string_body PAYLOAD KEY — the RAW (still escaped) body of the LAST
# `"KEY":"…"` in the payload, i.e. exactly the bytes that sit between the JSON
# string's quotes. Last, not first: a log line that carries a whole payload
# inside a string field would otherwise answer with the inner copy. The scan
# tracks backslash runs so a `\"` inside the value does not end it.
json_string_body() {
	_jsb_payload="$1"
	_jsb_key="$2"
	case "$_jsb_payload" in
		*'"'"$_jsb_key"'":"'*) ;;
		*) return 0 ;;
	esac
	_jsb_rest="${_jsb_payload##*'"'"$_jsb_key"'":"'}"
	_jsb_out=""
	_jsb_pos=0
	# The scan advances or breaks on EVERY branch below, so the cap can never
	# truncate a real value — it exists so that a future edit which breaks that
	# invariant terminates instead of spinning forever (the unbounded-loop ban,
	# the 2026-09-11 exhaustion shape; scripts/testutil/scripts_gate_test.go).
	_jsb_cap=${#_jsb_rest}
	_jsb_iter=0
	while :; do
		_jsb_iter=$((_jsb_iter + 1))
		if [ "$_jsb_iter" -gt "$_jsb_cap" ]; then
			# Unreachable while the invariant holds: carry the remainder, loudly.
			_jsb_out="$_jsb_out${_jsb_rest:_jsb_pos}"
			break
		fi
		_jsb_ch="${_jsb_rest:_jsb_pos:1}"
		if [ -z "$_jsb_ch" ]; then
			# An unterminated string: carry the remainder rather than lose it.
			_jsb_out="$_jsb_out${_jsb_rest:_jsb_pos}"
			break
		fi
		if [ "$_jsb_ch" = '"' ]; then
			break
		fi
		if [ "$_jsb_ch" = '\' ]; then
			_jsb_out="$_jsb_out${_jsb_rest:_jsb_pos:2}"
			_jsb_pos=$((_jsb_pos + 2))
			continue
		fi
		_jsb_run="${_jsb_rest:_jsb_pos}"
		_jsb_run="${_jsb_run%%[\\\"]*}"
		_jsb_out="$_jsb_out$_jsb_run"
		_jsb_pos=$((_jsb_pos + ${#_jsb_run}))
	done
	printf '%s' "$_jsb_out"
}

# json_unescape ESCAPED — ONE JSON string decode of an already-extracted body.
#
# RULE 35 (zero dependency): python3 is the FAST path, the in-script
# `json_unescape_bash` is the CORRECT path when it is absent, and this script
# already treats python3 that way for the peer list and the batch encoder. The
# fallback is not a degradation of correctness — the two produce the same bytes
# for every escape the format defines (the `"`, `\`, `/`, `\b\f\n\r\t`, and
# `\uXXXX` forms, surrogate pairs included) — it is only a few milliseconds per
# field slower. What IS refused is silence: a python3 that is missing, or one
# that fails on a particular body, is REPORTED once per run on stderr (Rule 15),
# never quietly downgraded.
#
# A TRAILING NEWLINE IS NOT PRESERVED, and that is the historic shape of this
# script rather than a choice made here: every caller captures the value with
# `$(...)`, which strips trailing newlines, and the delivery trims space at the
# inbox seam. Nothing that reaches a console is affected — build_message joins
# one line and forbids newlines inside it.
json_decode_route="" # "python3" | "bash" — decided once per run, below.
json_unescape() {
	_ju_body="$1"
	case "$_ju_body" in
		*\\*) ;;
		*)
			# No escape at all: nothing to decode, and nothing to report.
			printf '%s' "$_ju_body"
			return 0
			;;
	esac
	if [ -z "$json_decode_route" ]; then
		if command -v python3 >/dev/null 2>&1; then
			json_decode_route="python3"
		else
			json_decode_route="bash"
			printf '%s: no python3 — decoding notify payload fields with the in-script JSON string decoder (correct, and slower per field)\n' \
				"$NOTIFY_SCRIPT_NAME" >&2
		fi
	fi
	if [ "$json_decode_route" = "python3" ]; then
		_ju_out="$(printf '%s' "$_ju_body" | python3 -c '
import json, sys
sys.stdout.write(json.loads("\"" + sys.stdin.read() + "\""))
' 2>/dev/null)"
		_ju_rc=$?
		if [ "$_ju_rc" -eq 0 ]; then
			printf '%s' "$_ju_out"
			return 0
		fi
		# A python3 that is present but cannot decode THIS body is the one case
		# the fallback must be announced for, and once is enough for a run: the
		# cause (a broken interpreter, a body that is not a JSON string) does
		# not change from field to field.
		json_decode_route="bash"
		printf '%s: python3 could not decode a payload field (exit %s) — FALLING BACK to the in-script JSON string decoder for the rest of this run\n' \
			"$NOTIFY_SCRIPT_NAME" "$_ju_rc" >&2
	fi
	json_unescape_bash "$_ju_body"
}

# json_unescape_bash ESCAPED — the dependency-free JSON string decoder. It
# walks the body one escape at a time: the two-character forms become their
# bytes through printf's own `\b \f \n \r \t`, and `\uXXXX` is turned into its
# UTF-8 BYTES (1..4 of them, surrogate pairs combined per RFC 8259 §7) and then
# materialised with `printf '%b'`. Building the bytes rather than echoing the
# code point is what keeps this independent of the locale: this host runs with
# LANG=C, where a `\uXXXX` is six literal characters to every other tool.
#
# An escape the format does not define (`\x`, a truncated `\u12`) is passed
# through VERBATIM, which is both the serde-style leniency and the only branch
# that cannot lose text: a decoder that rejected the body would cost the whole
# message, and dropping the backslash would invent content.
json_unescape_bash() {
	_jub_s="$1"
	_jub_out=""
	_jub_one=""
	_jub_esc=""
	_jub_hex=""
	_jub_seq=""
	_jub_cp=0
	_jub_lo=0
	while :; do
		case "$_jub_s" in
			*\\*) ;;
			*)
				_jub_out="$_jub_out$_jub_s"
				break
				;;
		esac
		_jub_out="$_jub_out${_jub_s%%\\*}"
		_jub_s="${_jub_s#*\\}"
		_jub_esc="${_jub_s:0:1}"
		_jub_s="${_jub_s:1}"
		case "$_jub_esc" in
			'"') printf -v _jub_one '%s' '"' ;;
			'\') printf -v _jub_one '%s' '\' ;;
			'/') printf -v _jub_one '%s' '/' ;;
			b) printf -v _jub_one '\b' ;;
			f) printf -v _jub_one '\f' ;;
			n) printf -v _jub_one '\n' ;;
			r) printf -v _jub_one '\r' ;;
			t) printf -v _jub_one '\t' ;;
			u)
				_jub_hex="${_jub_s:0:4}"
				_jub_s="${_jub_s:4}"
				case "$_jub_hex" in
					[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]) ;;
					*)
						# Not a \u escape the format defines: keep the text.
						_jub_out="$_jub_out\\u$_jub_hex"
						continue
						;;
				esac
				_jub_cp=$((16#$_jub_hex))
				if [ "$_jub_cp" -ge 55296 ] && [ "$_jub_cp" -le 56319 ]; then
					# A high surrogate: a low one must follow, or it is not a
					# pair and the escape stands alone (passed through below).
					if [ "${_jub_s:0:2}" = '\u' ]; then
						case "${_jub_s:2:4}" in
							[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F])
								_jub_lo=$((16#${_jub_s:2:4}))
								if [ "$_jub_lo" -ge 56320 ] && [ "$_jub_lo" -le 57343 ]; then
									_jub_s="${_jub_s:6}"
									_jub_cp=$((65536 + (_jub_cp - 55296) * 1024 + (_jub_lo - 56320)))
								fi
								;;
						esac
					fi
				fi
				if [ "$_jub_cp" -lt 128 ]; then
					printf -v _jub_seq '\\x%02x' "$_jub_cp"
				elif [ "$_jub_cp" -lt 2048 ]; then
					printf -v _jub_seq '\\x%02x\\x%02x' \
						$((192 | _jub_cp >> 6)) $((128 | _jub_cp & 63))
				elif [ "$_jub_cp" -lt 65536 ]; then
					printf -v _jub_seq '\\x%02x\\x%02x\\x%02x' \
						$((224 | _jub_cp >> 12)) $((128 | _jub_cp >> 6 & 63)) $((128 | _jub_cp & 63))
				else
					printf -v _jub_seq '\\x%02x\\x%02x\\x%02x\\x%02x' \
						$((240 | _jub_cp >> 18)) $((128 | _jub_cp >> 12 & 63)) \
						$((128 | _jub_cp >> 6 & 63)) $((128 | _jub_cp & 63))
				fi
				printf -v _jub_one '%b' "$_jub_seq"
				;;
			*)
				# Unknown escape: verbatim, backslash included.
				_jub_out="$_jub_out\\$_jub_esc"
				continue
				;;
		esac
		_jub_out="$_jub_out$_jub_one"
	done
	printf '%s' "$_jub_out"
}

# field PAYLOAD KEY — the DECODED value of KEY, "" when the payload has none.
field() {
	# `tr -d '\n'` stays: a payload is one JSON object and a caller may hand
	# this a multi-line capture, while the scan above is line-agnostic only by
	# accident (a newline is not special to JSON, but it IS to `read` in every
	# other consumer of these values).
	json_unescape "$(json_string_body "$(printf '%s' "$1" | tr -d '\n')" "$2")"
}

# ---- 2c. cutting a message at a BYTE budget WITHOUT splitting a character ---
# THE DEFECT THIS CLOSES (task 01a1a63d, measured on this host 2026-11-04).
# The one-line message used to be cut with
#
#     [ "${#_msg}" -gt "$MESSAGE_SUMMARY_MAX" ] && _msg="${_msg:0:$MESSAGE_SUMMARY_MAX}…"
#
# which is TWO byte operations. Under `LANG=C` — the locale this script runs in
# on the notify path, and the one its own `field()` decoding depends on — bash
# measures `${#s}` in BYTES and `${s:i:n}` slices BYTES, so a 3-byte character
# straddling the 300-byte boundary was cut in half: the tail of the message was
# an ILLEGAL UTF-8 sequence, `json.Marshal` of the inbox record encoded those
# bytes as they were, and every reader that decoded once (the model, the session
# record, `grep`) saw U+FFFD where the summary's last character should have been.
#
# WHY THE CUT IS DONE HERE, BY HAND, AND NOT BY SWITCHING LOCALES. The obvious
# repair — run the truncation under a UTF-8 locale so `${s:i:n}` becomes
# rune-aware — is not available: this script is executed with `LANG=C` (its head
# comment and the whole `field()` / `json_unescape` path are written for it), and
# a locale flip would change how EVERY other string operation in the same shell
# behaves — `${s:0:2}` on `\u` escapes in `json_unescape_bash`, `${#}` on a
# partially-decoded buffer. It would also be a dependency on the host having a
# UTF-8 locale at all. So the byte budget is kept exactly as it was (300 BYTES,
# still the upper bound: the line is at most MAX + the marker) and only the CUT
# POINT moves, backwards, to the last character boundary at or before it.
#
# The walk is over bytes, which is what the budget is measured in: a lead byte
# says how many bytes its character occupies (the UTF8_* ranges at the top), a
# character is kept only when ALL of its bytes fit inside the budget, and a byte
# that starts no character the standard defines (a stray continuation byte, an
# overlong 0xC0/0xC1) is treated as ONE byte and copied through untouched —
# verbatim preservation is the contract here, this is a truncator and not a
# validator/repairer.
#
# `printf '%d' "'$c"` is bash's own byte-to-ordinal conversion (no `od`, no
# `iconv`, no `python3`: Rule 35 — this must be correct with nothing but bash),
# and it reports the CODE POINT under a UTF-8 locale, which is why the whole walk
# runs inside a `LC_ALL=C` SUBSHELL: the locale is forced for this arithmetic
# only, and the caller's locale — and every other string operation in the run —
# is left exactly as it was.
utf8_truncate() {
	_ut_str="$1"
	_ut_max="$2"
	(
		LC_ALL=C
		export LC_ALL
		_ut_n="${#_ut_str}"
		if [ "$_ut_n" -le "$_ut_max" ]; then
			printf '%s' "$_ut_str"
			exit 0
		fi
		_ut_i=0
		_ut_keep=0
		while [ "$_ut_i" -lt "$_ut_n" ]; do
			_ut_c="${_ut_str:_ut_i:1}"
			if ! printf -v _ut_b '%d' "'$_ut_c" 2>/dev/null; then
				# Not a byte this shell can read as a number: treat it as a
				# single byte and let it through (never silently drop content
				# and never guess a length — Rule 15).
				_ut_b="$UTF8_ONE_BYTE_MAX"
			fi
			if [ "$_ut_b" -le "$UTF8_ONE_BYTE_MAX" ]; then
				_ut_len=1
			elif [ "$_ut_b" -ge "$UTF8_TWO_BYTE_MIN" ] && [ "$_ut_b" -le "$UTF8_TWO_BYTE_MAX" ]; then
				_ut_len=2
			elif [ "$_ut_b" -ge "$UTF8_THREE_BYTE_MIN" ] && [ "$_ut_b" -le "$UTF8_THREE_BYTE_MAX" ]; then
				_ut_len=3
			elif [ "$_ut_b" -ge "$UTF8_FOUR_BYTE_MIN" ] && [ "$_ut_b" -le "$UTF8_FOUR_BYTE_MAX" ]; then
				_ut_len=4
			else
				_ut_len=1
			fi
			# Every byte position is visited in increasing order, so the first
			# character that does not fit also means NO later one can: the keep
			# point is final and the walk stops.
			if [ $((_ut_i + _ut_len)) -gt "$_ut_max" ]; then
				break
			fi
			_ut_keep=$((_ut_i + _ut_len))
			_ut_i=$((_ut_i + _ut_len))
		done
		printf '%s%s' "${_ut_str:0:_ut_keep}" "$MESSAGE_TRUNCATION_MARK"
	)
}

# ---- 3. build a one-line summary for a payload -----------------------------
# `prefix` is empty for this machine's own event — the local message text is
# byte-identical to what it was before the peer pull existed — and
# `[peer <alias>] ` for an event read from a peer, which is what makes the
# source visible in the chat.
build_message() {
	_msg_payload="$1"
	_msg_prefix="$2"
	_msg_event="$(field "$_msg_payload" event)"
	_msg_task_id="$(field "$_msg_payload" task_id)"
	_msg_status="$(field "$_msg_payload" status)"
	_msg_summary="$(field "$_msg_payload" summary)"
	# THE ORIGIN LEDGER — present only when the event came out of a ledger that
	# is NOT the profile's own (internal/tools/tasknotify.go
	# taskNotifyOriginLedger). Without it an event from a probe's / `--db`'s
	# private ledger is INDISTINGUISHABLE in this one-line message from one the
	# console can look up: two such events (01a16515-…, probe-dead-holder) sent
	# the operator hunting a row that exists in no reachable ledger. The path is
	# the answer to "where did this come from", so it rides the message rather
	# than only the raw anchor.
	_msg_ledger="$(field "$_msg_payload" ledger)"
	# AN EVENT THAT NAMES NOTHING IS A PARSE FAILURE, NOT A MESSAGE (task
	# 01a1a640, defect B). Every lifecycle payload the producer writes
	# (internal/tools/tasknotify.go's taskNotifyPayload) carries `event`,
	# `task_id` and `status` WITHOUT omitempty, so a payload missing any of the
	# three was not produced by that path at all: it is a blank line, a
	# truncated write, a hand-typed line, or a non-JSON body. Rendering those as
	# `event=? task=? status=?` invented a message out of nothing and named no
	# function of the failure — the operator saw three question marks and had
	# nothing to open, nothing to grep, and no way to tell "the fields were
	# empty" from "the sink is broken". The wording now STATES the failure,
	# NAMES the fields it could not read, and carries the raw line — the only
	# thing such an event still has (Rule 15: a bad event must say so rather
	# than be dropped or laundered into a placeholder that reads like data).
	_msg_missing=""
	[ -n "$_msg_event" ] || _msg_missing="event"
	[ -n "$_msg_task_id" ] || _msg_missing="${_msg_missing:+${_msg_missing},}task_id"
	[ -n "$_msg_status" ] || _msg_missing="${_msg_missing:+${_msg_missing},}status"
	if [ -n "$_msg_missing" ]; then
		# The raw line, flattened (a message is ONE line — `build_message`'s
		# contract) and explicit when there is nothing at all to show: an empty
		# operand after `raw line: ` reads like a truncation of the report.
		_msg_raw="$(printf '%s' "$_msg_payload" | tr -d '\n\r')"
		if [ -z "$_msg_raw" ]; then
			if [ -z "$_msg_payload" ]; then
				_msg_raw="$MESSAGE_EMPTY_PAYLOAD"
			else
				# Bytes arrived but every one of them was whitespace (a lone
				# newline is the shape a torn heredoc leaves). Saying "empty —
				# no bytes" here would be a false statement about the producer.
				_msg_raw="$MESSAGE_WHITESPACE_PAYLOAD"
			fi
		fi
		_msg="${_msg_prefix}${MESSAGE_EVENT_PREFIX}${MESSAGE_PARSE_FAILURE}${_msg_missing}; raw line: ${_msg_raw}"
	else
		_msg="${_msg_prefix}${MESSAGE_EVENT_PREFIX}event=${_msg_event} task=${_msg_task_id} status=${_msg_status}"
		if [ -n "$_msg_ledger" ]; then
			_msg="${_msg} ledger=${_msg_ledger}"
		fi
		if [ -n "$_msg_summary" ]; then
			_msg="${_msg} summary=${_msg_summary}"
		fi
	fi
	# THE CUT is made unconditionally, and the call decides whether one is
	# needed: the guard that used to sit here (`[ "${#_msg}" -gt MAX ]`) was
	# ITSELF locale-dependent — under a UTF-8 locale `${#_msg}` counts
	# CHARACTERS, so a 300-character line of CJK (900 bytes) passed the guard
	# untouched and the byte ceiling was never enforced. utf8_truncate measures
	# and cuts in bytes inside its own `LC_ALL=C` subshell, so the 300-byte
	# bound holds whatever locale the caller happens to run in, and a message
	# that already fits comes back byte-identical with no marker.
	_msg="$(utf8_truncate "$_msg" "$MESSAGE_SUMMARY_MAX")"
	printf '%s' "$_msg"
}

# ---- 3b. THE RUN DEADLINE ------------------------------------------------
# The budget is spent by the WHOLE run (both peers), so it is a wall-clock
# instant computed once, at the top, and every check below is a comparison
# against it. EPOCHREALTIME is a bash builtin (no `date` process per check) and
# carries microseconds, which is why the comparison is on the digits with the
# decimal point removed rather than on a float.
#
# A shell that has no EPOCHREALTIME (bash < 5) leaves the deadline EMPTY, and an
# empty deadline means "no local deadline": the script then behaves exactly as
# before this change and keeps only the parent's kill as its bound. That
# degradation is REPORTED at the one place it changes a decision (the per-peer
# note below), never silent.
if [ -n "${EPOCHREALTIME:-}" ]; then
	_notify_started_us="${EPOCHREALTIME/./}"
	DEADLINE_US=$((_notify_started_us + NOTIFY_RUN_BUDGET_SEC * 1000000))
else
	DEADLINE_US=""
fi

# deadline_expired — 0 when the run has spent its budget, 1 otherwise.
# An empty DEADLINE_US is "never expired": the fallback above.
deadline_expired() {
	[ -n "$DEADLINE_US" ] || return 1
	[ "${EPOCHREALTIME/./}" -ge "$DEADLINE_US" ]
}

# seconds_left — whole seconds remaining before the deadline, floored at
# DEADLINE_MIN_FLOOR_SEC, or empty when no deadline is in force. This is what
# bounds one ssh leg: neither a number that would make `timeout` reject the
# operand (0 or negative) nor one that would let the leg outlive the budget.
readonly DEADLINE_MIN_FLOOR_SEC=1
seconds_left() {
	[ -n "$DEADLINE_US" ] || return 0
	_sl_now="${EPOCHREALTIME/./}"
	_sl_left=$(( (DEADLINE_US - _sl_now) / 1000000 ))
	[ "$_sl_left" -ge "$DEADLINE_MIN_FLOOR_SEC" ] || _sl_left="$DEADLINE_MIN_FLOOR_SEC"
	printf '%s' "$_sl_left"
}

# ---- 4. the ONE per-slot delivery loop -------------------------------------
# Every message — this machine's own and every peer event — goes through THIS
# loop. It is called, never duplicated: two copies of a delivery rule are two
# places for the rule to drift.
deliver_all() {
	_da_message="$1"
	_da_delivered=0
	_da_refused=0
	_da_dead=0
	_da_registered=0
	_da_i=0
	while [ "$_da_i" -lt "$SLOT_LIMIT" ]; do
		_da_slot="${SLOT_DIR}/${SLOT_PREFIX}${_da_i}${SLOT_SUFFIX}"
		_da_i=$((_da_i + 1))
		[ -s "$_da_slot" ] || continue
		_da_registered=$((_da_registered + 1))
		_da_identity="$(tr -d '\n\r' <"$_da_slot")"
		# The value is `<pid|self>:<session-directory>`; the directory after the
		# first ':' is the operand `gopi notify --session` resolves.
		_da_session="${_da_identity#*:}"
		if [ -z "$_da_session" ] || [ "$_da_session" = "$_da_identity" ]; then
			printf 'notify.sh: slot %s holds %q, which is not <pid|self>:<session-dir> — skipped\n' "$_da_slot" "$_da_identity" >&2
			_da_dead=$((_da_dead + 1))
			continue
		fi
		# A slot whose session DIRECTORY no longer exists is a DEAD registration —
		# a swept scratch tree, a deleted profile, a run that died before minting a
		# session. It is counted separately from a refusal because the two say
		# different things: a refusal means a chat was there and would not take the
		# message, an absent directory means nobody is there at all.
		if [ ! -d "$_da_session" ]; then
			_da_dead=$((_da_dead + 1))
		fi
		if [ -z "$gopi_bin" ]; then
			continue
		fi
		# `</dev/null`: the child must not see the payload pipe (see the header).
		if _da_out="$("$gopi_bin" notify --session "$_da_session" --message "$_da_message" </dev/null 2>&1)"; then
			_da_delivered=$((_da_delivered + 1))
		else
			_da_refused=$((_da_refused + 1))
			printf 'notify.sh: slot %s (%s): delivery refused: %s\n' "$_da_slot" "$_da_session" "$_da_out" >&2
		fi
	done
	printf 'notify.sh: delivered=%d refused=%d dead=%d registered=%d slots_dir=%s\n' \
		"$_da_delivered" "$_da_refused" "$_da_dead" "$_da_registered" "$SLOT_DIR" >&2

	# ---- the AGGREGATE outcome, which the per-slot lines cannot state --------
	# The per-slot lines above describe each attempt; none of them answers the
	# question an operator actually has, which is "did THIS event reach anybody?".
	# Silence is the answer "yes" only when it is indistinguishable from the
	# answer "no", and it was not: a machine with no delivery target at all
	# printed exactly the same shape as a machine with one working chat. So the
	# two failing aggregates get their own line, and they are the loudest thing
	# this script writes (task 01a12d05, the eight hours of events that reached
	# nobody).
	#
	# The condition is deliberately about DELIVERY, not about refusal counts: what
	# was lost is the message, and it was lost whenever `_da_delivered` is zero —
	# whether because no slot was registered, because every registered one was
	# dead, or because every live one refused.
	#
	# RE-PORT NOTE (task 01a12d61, 2026-10-12): master restructured the delivery
	# into THIS single function (one loop shared by this machine's event and every
	# peer event, so the rule cannot drift between two copies). The predecessor
	# wrote these two aggregate lines in main, around its own inline copy of the
	# loop. The union keeps master's single loop and moves the report inside it,
	# which is strictly wider: a peer event that reached nobody is now just as
	# audible as a local one.
	if [ "$_da_delivered" -eq 0 ]; then
		if [ "$_da_registered" -eq 0 ]; then
			printf 'notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — no chat is registered in %s (slots scanned=0). The raw event is still anchored in %s; every chat console is blind to it until a `gopi chat` writes a slot there.\n' \
				"$SLOT_DIR" "$SINK_FILE" >&2
		else
			printf 'notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — %d slot(s) registered in %s, %d of them dead (their session directory no longer exists), %d refused. The raw event is still anchored in %s.\n' \
				"$_da_registered" "$SLOT_DIR" "$_da_dead" "$_da_refused" "$SINK_FILE" >&2
		fi
	fi
}

# ---- 4b. the BATCH delivery: ONE process for a whole batch ----------------
# `deliver_all` above is right for ONE message and is unchanged. A peer backlog
# is not one message: it is hundreds, and handing each one its own process per
# slot is the O(n) that stalled this script (see THE DELIVERY BUDGET). This is
# the same delivery with the round trip amortised — the messages are handed to
# ONE child, which delivers each of them to every live slot.
#
# IT IS CONDITIONAL, AND THE CONDITION IS THE SCRIPT'S OWN. The batch path needs
# the `--deliver-multi` reader on the `gopi notify` verb; a `gopi` that predates
# it would answer the unknown flag with an argument error, and the failure mode
# of guessing wrong is a LOST NOTIFICATION, not a slow one. So the flag is
# probed ONCE per run (`gopi notify --deliver-multi -h` — the verb's own help
# exits 0 when the flag parses) and the answer decides the route for the whole
# run. When the reader is absent every batch falls back to `deliver_all`, line by
# line: exactly the old behaviour, which is correct (it is what shipped) and only
# slower — and it is REPORTED once, so a deployment running an old binary is not
# silently the slow path either.
#
# THE BATCH IS PASSED AS JSON ON STDIN, built by python3 (already required for
# parsing settings.json): a message carries `"` characters and newlines are
# forbidden, and a format that cannot survive either is not a transport.
deliver_batch() {
	_db_peer="$1"
	_db_file="$2"
	_db_total="$3"
	if [ "$db_multi_ok" -ne 1 ]; then
		_db_i=0
		while IFS= read -r _db_line; do
			[ -n "$_db_line" ] || continue
			deliver_all "$(build_message "$_db_line" "[peer ${_db_peer}] ")"
		done <"$_db_file"
		return 0
	fi
	if ! _db_json="$(python3 -c '
import json, sys
prefix = sys.argv[1]
sys.stdout.write(json.dumps([prefix + l.rstrip("\n") for l in open(sys.argv[2])]))
' "[peer ${_db_peer}] " "$_db_file")"; then
		printf 'notify.sh: peer %s: cannot encode a %s-line batch as JSON — falling back to per-line delivery for this batch\n' \
			"$_db_peer" "$_db_total" >&2
		_db_i=0
		while IFS= read -r _db_line; do
			[ -n "$_db_line" ] || continue
			deliver_all "$(build_message "$_db_line" "[peer ${_db_peer}] ")"
		done <"$_db_file"
		return 0
	fi
	# `_db_out` as the LAST command substitution in this function: the caller
	# reads the child's refusal text back to name it in its own report.
	#
	# NO `</dev/null` HERE, unlike every other child this script starts: for THIS
	# one the batch IS its stdin, and redirecting it away would hand the reader an
	# empty array. The payload was captured into a variable at the top, so this
	# script's own stdin is not needed by anything below.
	if _db_out="$(printf '%s' "$_db_json" | "$gopi_bin" notify "$NOTIFY_DELIVER_MULTI_FLAG" 2>&1)"; then
		printf 'notify.sh: peer %s: batch of %s line(s) delivered in ONE process\n' \
			"$_db_peer" "$_db_total" >&2
		return 0
	fi
	# A batch the reader REFUSED is not a delivery: report it and let the caller
	# decide. It must NOT advance the cursor over lines nobody received.
	printf 'notify.sh: peer %s: batch of %s line(s) REFUSED: %s\n' \
		"$_db_peer" "$_db_total" "$_db_out" >&2
	return 1
}

# ---- 4c. ONE BATCH, DRAINED — deliver, then advance the cursor ------------
# This is the unit the whole fix is built on, and the ORDER is the invariant:
# deliver the batch, and only then move the cursor past it. The cursor therefore
# never claims more than has been delivered, and an interruption costs at most
# this ONE batch in re-delivery (never the whole backlog).
#
# The batch is materialised as a SLICE of the spool (`tail -n +N | head -n B`):
# no second copy of the peer's lines exists, and the batch boundary is a line
# number rather than a buffer.
#
# peer_drain_batches PEER RAWLINE POS — deliver up to PEER_DELIVER_BATCHES_PER_RUN
# batches of PEER_DELIVER_BATCH_LINES lines starting at 1-based line POS,
# advancing the cursor after EACH one. Prints nothing but its own report.
#
# RETURNS 1 as soon as a batch is refused (the caller must not advance over
# lines nobody received), and stops — without an error — when the run budget is
# spent, which is the ordinary "the rest is next run's work" exit.
peer_drain_batches() {
	_pd_peer="$1"
	_pd_rawline="$2"
	_pd_pos="$3"
	# The peer's cursor — its path and the value already recorded in it — comes
	# from the caller (pull_one_peer owns it for the run); this function only
	# advances it, and only AFTER a batch has been delivered.
	_pd_cursor="$_po_cursor"
	_pd_since="$_po_since"
	_pd_delivered=0
	_pd_batch=0
	while [ "$_pd_batch" -lt "$PEER_DELIVER_BATCHES_PER_RUN" ]; do
		_pd_batch=$((_pd_batch + 1))
		if ! tail -n "+$((_pd_pos + 1))" "$_pd_rawline" 2>/dev/null |
			head -n "$PEER_DELIVER_BATCH_LINES" >"$_pd_batchf"; then
			return 0
		fi
		[ -s "$_pd_batchf" ] || return 0 # nothing left on the peer
		_pd_batch_n="$(wc -l <"$_pd_batchf" | tr -d ' ')"
		# The newest timestamp IN THIS BATCH is what the cursor advances to. The
		# spool arrives sorted by the peer's anchor order, so the batch's last
		# line is its newest — and using the batch's own last line (rather than a
		# running maximum over the whole slice) is what makes the advance exact
		# at every boundary.
		_pd_batch_top="$(field "$(tail -n 1 "$_pd_batchf")" timestamp)"
		[ -n "$_pd_batch_top" ] || _pd_batch_top="$_pd_seen"
		if ! deliver_batch "$_pd_peer" "$_pd_batchf" "$_pd_batch_n"; then
			return 1
		fi
		# THE ADVANCE, after the delivery and never before it.
		_pd_pos=$((_pd_pos + _pd_batch_n))
		_pd_delivered=$((_pd_delivered + _pd_batch_n))
		_pd_seen="$_pd_batch_top"
		if [ "$_pd_seen" != "$_pd_since" ]; then
			if printf '%s\n' "$_pd_seen" >"$_pd_cursor"; then
				_pd_since="$_pd_seen"
			else
				printf 'notify.sh: peer %s: cannot write cursor %s — at most this batch (%s line(s)) will be re-delivered next run\n' \
					"$_pd_peer" "$_pd_cursor" "$_pd_batch_n" >&2
			fi
		fi
		if deadline_expired; then
			printf 'notify.sh: peer %s: %s new event(s) delivered — the rest is the next run'"'"'s work (the %ss run budget is spent)\n' \
				"$_pd_peer" "$_pd_delivered" "$NOTIFY_RUN_BUDGET_SEC" >&2
			return 0
		fi
	done
	if [ "$_pd_delivered" -gt 0 ]; then
		# Not an error and not a loss: a backlog is drained in batches across
		# runs, and this line is what tells an operator that a large backlog is
		# being worked through instead of being re-read forever.
		printf 'notify.sh: peer %s: %s new event(s) delivered this run (at most %s batches of %s lines per run)\n' \
			"$_pd_peer" "$_pd_delivered" "$PEER_DELIVER_BATCHES_PER_RUN" "$PEER_DELIVER_BATCH_LINES" >&2
	fi
	return 0
}

# ---- 5. the peer whitelist, read from settings.json ------------------------
# ONE source of truth: the same `sync_peers` array the task-ledger transport
# uses (internal/config: SyncPeersSettingsKey). No second, hardcoded list lives
# here — a peer added to settings.json is pulled from on the next run. Parsed
# with python3 when it is available (a JSON array is not a line-oriented
# format); without python3 the pull is SKIPPED and said so, never silently
# guessed at with a fragile regex. A token is stripped of surrounding quotes so
# a config value can never smuggle shell metacharacters into the ssh operand.
peer_list() {
	if [ ! -r "$settings_path" ]; then
		printf 'notify.sh: no readable settings file at %s — peer pull skipped\n' "$settings_path" >&2
		return 0
	fi
	if ! command -v python3 >/dev/null 2>&1; then
		printf 'notify.sh: no python3 to parse %s — peer pull skipped\n' "$settings_path" >&2
		return 0
	fi
	python3 -c '
import json, sys
try:
    with open(sys.argv[1]) as fh:
        cfg = json.load(fh)
except Exception as exc:
    sys.stderr.write("notify.sh: cannot parse %s: %s — peer pull skipped\n" % (sys.argv[1], exc))
    sys.exit(0)
peers = cfg.get(sys.argv[2]) or []
if not isinstance(peers, list):
    sys.stderr.write("notify.sh: %s in %s is not a list — peer pull skipped\n" % (sys.argv[2], sys.argv[1]))
    sys.exit(0)
for p in peers:
    p = str(p).strip().strip("\"")
    if p:
        print(p)
' "$settings_path" "$SYNC_PEERS_KEY"
}

# ---- 6. read a peer's NEW events, filtered ON THE PEER ---------------------
# The remote side does the filtering: the anchor is ~53 MB / 4k lines, so
# shipping it whole would be the expensive half of this call. The peer keeps the
# lines whose `"timestamp":"<ISO>"` sorts AFTER the cursor — both hosts stamp
# `+08:00`, so those strings compare correctly as text. The cursor travels as an
# OPERAND (`sh -c <program> sh <cursor>`), never interpolated into the program,
# and the program itself is a constant: no settings-derived text ever reaches a
# shell on the far side.
PEER_REMOTE_PROGRAM="$(cat <<REMOTE
f="\$HOME/${PEER_ANCHOR_REL}"
[ -r "\$f" ] || exit 3
if [ -z "\${1:-}" ]; then
	grep -F '"timestamp":"' "\$f" | tail -n 1
else
	grep -F '"timestamp":"' "\$f" |
		awk -v since="\$1" '{ if (match(\$0, /"timestamp":"[^"]*"/)) { ts=substr(\$0, RSTART+13, RLENGTH-14); if (ts > since) print } }'
fi
REMOTE
)"
readonly PEER_REMOTE_PROGRAM

peer_new_lines() {
	# The program travels on STDIN (`sh -s`), never as an argv element: `ssh`
	# joins its command arguments into ONE string that the remote login shell
	# parses again, so a quoted program passed as an argument loses its quotes
	# and its own quotes stop pairing (measured: the remote side read the file
	# as unreadable and exited 3). The cursor is the only argv element, and it is
	# passed as a quoted OPERAND — it is whitelisted by the caller before it gets
	# here, so nothing else can reach the far side's parser.
	#
	# THE LEG IS CLIPPED TO THE RUN BUDGET (see THE DELIVERY BUDGET). ConnectTimeout
	# bounds only the CONNECT phase, so a peer that answers and then stalls while
	# scanning its anchor would otherwise hold this leg for as long as it likes
	# and eat the parent's kill — the exact shape of the stall this task fixes.
	# `timeout` is the bound; when it is unavailable the leg is left to
	# ConnectTimeout and THAT IS REPORTED (below), never silently.
	#
	# shellcheck disable=SC2029  # the cursor MUST expand here, on the client
	_pnl_left="$(seconds_left)"
	if [ -n "$_pnl_left" ] && [ "$_pnl_left" -gt "$PEER_REMOTE_MAX_SEC" ]; then
		_pnl_left="$PEER_REMOTE_MAX_SEC"
	fi
	if [ -n "$_pnl_left" ] && [ "$timeout_ok" -eq 1 ]; then
		printf '%s\n' "$PEER_REMOTE_PROGRAM" |
			timeout "$_pnl_left" ssh -T -o BatchMode=yes -o "ConnectTimeout=${PEER_SSH_TIMEOUT}" "$1" "sh -s -- '${2}'"
		return $?
	fi
	printf '%s\n' "$PEER_REMOTE_PROGRAM" |
		ssh -T -o BatchMode=yes -o "ConnectTimeout=${PEER_SSH_TIMEOUT}" "$1" "sh -s -- '${2}'"
}

# The spool files the ssh output is landed in. Fixed names, named after this
# process, removed by an EXIT trap so an interrupted run leaves no litter.
peer_tmp_cleanup() {
	rm -f "${SINK_DIR}/${PEER_TMP_PREFIX}.$$.raw" "${SINK_DIR}/${PEER_TMP_PREFIX}.$$.err" \
		"${SINK_DIR}/${PEER_TMP_PREFIX}.$$.batch"
}
trap peer_tmp_cleanup EXIT

# ---- 6b. the spools an INTERRUPTED run leaves behind -----------------------
# The parent kills this script with SIGKILL at taskNotifyCmdTimeout, and a
# SIGKILL runs no trap: the spool of the run that died stays on disk. Measured
# 2026-10-16 (the stall this task fixes): 33 such files, ~10 MB, each a full
# copy of a peer's backlog that had already been re-read from the same stale
# cursor many times over.
#
# THE SWEEP IS BY AGE, NEVER BY "it looks abandoned". A live sibling notify run
# is ordinary (events arrive in bursts and each one is its own process), so
# removing a spool that is merely not OURS would delete a running peer's input.
# Age is the property that makes it safe: a spool goes only once it is older than
# NOTIFY_SPOOL_STALE_SEC — far beyond the 30s a live run can possibly hold one —
# and only when it is not this process's own file.
#
# WHAT WAS REMOVED IS REPORTED, never silent: these files are the residue of
# runs that died BEFORE their cursor write, so their presence is evidence about
# this machine, and an operator should be able to see it (Rule 15).
readonly NOTIFY_SPOOL_STALE_SEC=300
sweep_stale_spools() {
	[ -d "$SINK_DIR" ] || return 0
	_ss_removed=0
	for _ss_file in "$SINK_DIR"/"${PEER_TMP_PREFIX}".*; do
		[ -f "$_ss_file" ] || continue
		case "$_ss_file" in
		*"${PEER_TMP_PREFIX}.$$."*) continue ;;
		esac
		# -mmin is whole minutes, so the threshold is expressed in the same unit
		# (a rounded-UP conversion, so the sweep can only ever be LATER than the
		# constant promises — never earlier, which would be the unsafe direction).
		if ! find "$_ss_file" -maxdepth 0 -mmin "+$(( (NOTIFY_SPOOL_STALE_SEC + 59) / 60 ))" 2>/dev/null |
			grep -q .; then
			continue
		fi
		if rm -f "$_ss_file" 2>/dev/null; then
			_ss_removed=$((_ss_removed + 1))
		fi
	done
	if [ "$_ss_removed" -gt 0 ]; then
		printf 'notify.sh: removed %s spool file(s) left by earlier runs that were killed before their EXIT trap could run (untouched for over %ss, in %s)\n' \
			"$_ss_removed" "$NOTIFY_SPOOL_STALE_SEC" "$SINK_DIR" >&2
	fi
}

# ---- 6c. THE OTHER TWO PARAMETERS, and the FAILURE RECORD ------------------
# Both values are resolved ONCE, here, from the parameter block. A value that is
# not a positive integer is REPORTED and replaced by the default — a typo must
# never disable the bound it was meant to tighten (Rule 15: no silent fallback).
positive_int_or_default() {
	case "$1" in
	'')
		printf '%s' "$2"
		return 0
		;;
	*[!0-9]*)
		printf 'notify.sh: %s=%s is not a positive integer — using %s\n' "$3" "$1" "$2" >&2
		printf '%s' "$2"
		return 0
		;;
	esac
	if [ "$1" -gt 0 ]; then
		printf '%s' "$1"
		return 0
	fi
	printf 'notify.sh: %s=%s is not positive — using %s\n' "$3" "$1" "$2" >&2
	printf '%s' "$2"
}
peer_limit="$(positive_int_or_default "${!PEER_LIMIT_ENV:-}" "$PEER_LIMIT" "$PEER_LIMIT_ENV")"
cursor_stale_sec="$(positive_int_or_default "${!CURSOR_STALE_SEC_ENV:-}" "$CURSOR_STALE_SEC_DEFAULT" "$CURSOR_STALE_SEC_ENV")"

# THE FAILURE RECORD — this is the deliverable that makes a broken pull visible.
#
# WHY A FILE AND NOT STDERR. `notify.cmd` runs with the payload on stdin and its
# stdout/stderr captured (`internal/tools/tasknotify.go: taskNotifyCmd`); the
# success path records `cmd ok (stdout: )` and DROPS the text. So every
# `notify.sh: peer …` line this script writes is invisible unless somebody is
# watching a failing terminal. A pull that has been dead for a week therefore
# looked EXACTLY like a pull with nothing to say (Rule 15).
#
# THE SHAPE: ONE file per peer, `${STATE_DIR}/peer-<alias>-health.txt`, two
# lines. The first carries only what a HUMAN needs and what a comparison can
# reason about — state, peer, the cursor it was judged at, the reason — the
# second carries the machine timestamps. The first line is the DEDUPE KEY: it is
# rewritten only when the key CHANGES (a new state, a new cursor, a different
# failure), so a peer that is broken for a week leaves ONE record that keeps
# saying so, not one per event. The second line is refreshed at most once per
# PEER_HEALTH_REFRESH_SEC so a reader can still tell "still broken now" from
# "broken once, months ago" — bounded, never a per-run write.
readonly PEER_HEALTH_PREFIX="peer-"
readonly PEER_HEALTH_SUFFIX="-health.txt"
readonly PEER_HEALTH_REFRESH_SEC=3600
readonly PEER_HEALTH_TMP_PREFIX=".peer-health"
readonly PEER_HEALTH_REASON_MAX=200
peer_health_write() {
	_ph_peer="$1"
	_ph_state="$2"
	_ph_reason="$3"
	_ph_cursor="$4"
	_ph_file="${STATE_DIR}/${PEER_HEALTH_PREFIX}${_ph_peer}${PEER_HEALTH_SUFFIX}"
	# One line, one write: a reason newline would forge a second record.
	_ph_reason="$(printf '%s' "$_ph_reason" | tr '\n\r\t' '   ')"
	if [ "${#_ph_reason}" -gt "$PEER_HEALTH_REASON_MAX" ]; then
		_ph_reason="${_ph_reason:0:$PEER_HEALTH_REASON_MAX}…"
	fi
	_ph_key="state=${_ph_state} peer=${_ph_peer} cursor=${_ph_cursor:-none} reason=${_ph_reason}"
	_ph_seen=""
	_ph_since=""
	_ph_checked=0
	if [ -r "$_ph_file" ]; then
		_ph_seen="$(sed -n '1p' "$_ph_file")"
		_ph_since="$(sed -n 's/^first_seen=\([^ ]*\).*/\1/p' "$_ph_file")"
		_ph_checked="$(sed -n 's/.*last_checked_epoch=\([0-9]*\).*/\1/p' "$_ph_file")"
		case "$_ph_checked" in
		'' | *[!0-9]*) _ph_checked=0 ;;
		esac
	fi
	_ph_now="$(date +%s 2>/dev/null || printf '0')"
	case "$_ph_now" in
	'' | *[!0-9]*) _ph_now=0 ;;
	esac
	_ph_iso="$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || printf 'unknown')"
	if [ "$_ph_seen" = "$_ph_key" ]; then
		# Unchanged: keep the original first_seen, and write only when the last
		# refresh is old enough to be worth stating.
		if [ "$_ph_now" -gt 0 ] && [ "$_ph_checked" -gt 0 ] &&
			[ "$((_ph_now - _ph_checked))" -lt "$PEER_HEALTH_REFRESH_SEC" ]; then
			return 0
		fi
	else
		_ph_since="$_ph_iso"
	fi
	[ -n "$_ph_since" ] || _ph_since="$_ph_iso"
	_ph_tmp="${STATE_DIR}/${PEER_HEALTH_TMP_PREFIX}.$$.${_ph_peer}"
	# Written to a sibling and MOVED into place: a reader either sees the old
	# record or the new one, never half of either (this is the record a human
	# consults when they do not trust the notifications).
	if ! printf '%s\nfirst_seen=%s last_checked=%s(last_checked_epoch=%s)\n' \
		"$_ph_key" "$_ph_since" "$_ph_iso" "$_ph_now" >"$_ph_tmp" 2>/dev/null ||
		! mv -f "$_ph_tmp" "$_ph_file" 2>/dev/null; then
		rm -f "$_ph_tmp" 2>/dev/null
		printf 'notify.sh: cannot write the health record %s — the failure state of peer %s stays unreported\n' \
			"$_ph_file" "$_ph_peer" >&2
		return 0
	fi
	# The record is written; ONE stderr line rides along so a live operator sees
	# the transition too, and only the transition (the file is the dedupe).
	printf 'notify.sh: peer %s: state=%s — recorded in %s\n' \
		"$_ph_peer" "$_ph_state" "$_ph_file" >&2
}

# cursor_age_sec FILE — whole seconds since FILE's last write, or empty.
# The peer's cursor is rewritten on every batch that advanced, so its mtime IS
# the instant the pull last made progress; an unchanged cursor over hours means
# the pull is not working (unreachable peer, refused auth, a peer whose anchor
# stopped growing) — the condition this record exists to surface.
cursor_age_sec() {
	[ -e "$1" ] || return 0
	_ca_mtime="$(stat -c %Y "$1" 2>/dev/null || printf '')"
	case "$_ca_mtime" in
	'' | *[!0-9]*) return 0 ;;
	esac
	_ca_now="$(date +%s 2>/dev/null || printf '')"
	case "$_ca_now" in
	'' | *[!0-9]*) return 0 ;;
	esac
	[ "$_ca_now" -ge "$_ca_mtime" ] || return 0
	printf '%s' "$((_ca_now - _ca_mtime))"
}


pull_one_peer() {
	_po_peer="$1"
	_po_cursor="${STATE_DIR}/${PEER_CURSOR_PREFIX}${_po_peer}${PEER_CURSOR_SUFFIX}"
	_po_since=""
	if [ -r "$_po_cursor" ]; then
		_po_since="$(tr -d '\n\r' <"$_po_cursor")"
	fi
	# The cursor is the ONE value that crosses into the remote command line, so
	# it is whitelisted rather than trusted: an ISO timestamp needs nothing
	# outside this set, and anything else (a hand-edited file, a quote smuggled
	# in through a peer's `timestamp` field) must not reach the far side's
	# parser. A refused cursor SKIPS the peer, loudly — treating it as empty
	# would re-deliver the peer's whole stream.
	case "$_po_since" in
	*[!0-9A-Za-z:+.-]*)
		printf 'notify.sh: peer %s: cursor %s holds characters outside the allowed set [0-9A-Za-z:+.-] — peer skipped, cursor untouched\n' \
			"$_po_peer" "$_po_cursor" >&2
		peer_health_write "$_po_peer" "refused-cursor" \
			"cursor holds characters outside [0-9A-Za-z:+.-]; peer skipped, cursor untouched" ""
		return 0
		;;
	esac
	_po_rawf="${SINK_DIR}/${PEER_TMP_PREFIX}.$$.raw"
	_po_errf="${SINK_DIR}/${PEER_TMP_PREFIX}.$$.err"
	_pd_batchf="${SINK_DIR}/${PEER_TMP_PREFIX}.$$.batch"
	_po_status=0
	peer_new_lines "$_po_peer" "$_po_since" >"$_po_rawf" 2>"$_po_errf" || _po_status=$?
	# The ssh exit status and the LOCAL delivery path share one report, because
	# the parent's deadline kills this script with SIGKILL and the report line is
	# the only thing that survives to say which half was interrupted.
	if [ "$_po_status" -ne 0 ]; then
		_po_why="$(tr '\n' ' ' <"$_po_errf")"
		printf 'notify.sh: peer %s: cannot read its event stream (ssh exit %s): %s\n' \
			"$_po_peer" "$_po_status" "$_po_why" >&2
		# THE REASON IS NORMALISED, not copied verbatim: ssh quotes the address and
		# the port in its own message, so a raw line would differ run to run and
		# defeat the dedupe. The exit status is the stable classifier (255 = the
		# connect/auth leg failed, 3 = the peer answered but has no readable
		# anchor, 124 = our own `timeout` clipped it); the text rides behind it.
		peer_health_write "$_po_peer" "unreachable" \
			"ssh exit ${_po_status}: $(printf '%s' "$_po_why" | sed -e 's/ssh: *//g' -e 's/\.[0-9][0-9]*//g')" \
			"${_po_since:-}"
		return 1
	fi
	# The newest timestamp SEEN on the peer — new or already delivered — is what
	# the cursor records, so a batch that is fully de-duplicated still advances
	# and is never re-read. Nothing seen at all leaves the cursor untouched.
	#
	# A MISSING cursor is not "everything is new". The first run after this
	# feature lands has no cursor, and the peer's anchor holds every event it
	# ever recorded: delivering that batch would push thousands of historical
	# notifications into the operator's chat at once (measured: 447 messages).
	# So the first run ESTABLISHES the cursor from the peer's newest event and
	# delivers nothing — the pull starts at "from now on", which is what a
	# cursor with no history can honestly mean.
	_po_seen="$_po_since"
	_po_count=0
	_po_delivered=0
	if [ -z "$_po_since" ]; then
		while IFS= read -r _po_line; do
			[ -n "$_po_line" ] || continue
			_po_ts="$(field "$_po_line" timestamp)"
			if [ -n "$_po_ts" ] && [ "$_po_ts" \> "$_po_seen" ]; then
				_po_seen="$_po_ts"
			fi
		done <"$_po_rawf"
		if [ "$_po_seen" = "$_po_since" ]; then
			printf 'notify.sh: peer %s: no cursor yet and its stream carries no event — nothing to do\n' "$_po_peer" >&2
			return 0
		fi
		printf 'notify.sh: peer %s: first read (no cursor) — starting at %s, 0 delivered\n' "$_po_peer" "$_po_seen" >&2
		if ! printf '%s\n' "$_po_seen" >"$_po_cursor"; then
			printf 'notify.sh: peer %s: cannot write cursor %s — the peer will not be read until this succeeds\n' \
				"$_po_peer" "$_po_cursor" >&2
		fi
		return 0
	fi
	# THE DELIVERY, in bounded batches, each one advancing the cursor before the
	# next begins. A batch the reader REFUSES stops the loop with the cursor
	# where it is: the refusal is reported and this batch is re-read next run,
	# which is the one case where re-delivery is the CORRECT outcome (a batch
	# that was not delivered must not be marked delivered).
	_po_count="$(wc -l <"$_po_rawf" | tr -d ' ')"
	if [ "$_po_count" = "0" ]; then
		printf 'notify.sh: peer %s: 0 new event(s)\n' "$_po_peer" >&2
		return 0
	fi
	if ! peer_drain_batches "$_po_peer" "$_po_rawf" 0; then
		printf 'notify.sh: peer %s: a batch was REFUSED — the cursor stays at %s, so that batch is re-read next run\n' \
			"$_po_peer" "$_po_since" >&2
	fi
}

# ---- 8. every declared peer, in the order settings.json lists them ---------
pull_peer_events() {
	_pp_peers="$(peer_list)"
	[ -n "$_pp_peers" ] || {
		printf 'notify.sh: no sync peer declared in %s — peer pull skipped\n' "$settings_path" >&2
		return 0
	}
	_pp_i=0
	while IFS= read -r _pp_peer; do
		[ -n "$_pp_peer" ] || continue
		_pp_i=$((_pp_i + 1))
		if [ "$_pp_i" -gt "$peer_limit" ]; then
			printf 'notify.sh: peer %s: skipped (over the %s-peer-per-run limit; the next run reads it)\n' \
				"$_pp_peer" "$peer_limit" >&2
			# A CLIPPED PEER IS RECORDED, because it is the one failure this
			# script causes itself: peers beyond the per-run limit are read only
			# when the earlier ones are cheap, so a host with three peers and two
			# blackholed ones can starve the third forever. That starvation is
			# exactly a silent one unless it is written down.
			peer_health_write "$_pp_peer" "not-read" \
				"beyond the ${peer_limit}-peer-per-run limit (limit: ${PEER_LIMIT_ENV}); later runs read it" ""
			continue
		fi
		# Stop BEFORE starting another peer's ssh leg when the run has spent its
		# budget: a leg started at the deadline would be killed mid-flight by the
		# parent, which is precisely the shape that left a spool behind.
		if deadline_expired; then
			printf 'notify.sh: peer %s: skipped (the %ss run budget is spent; the next run reads it)\n' \
				"$_pp_peer" "$NOTIFY_RUN_BUDGET_SEC" >&2
			peer_health_write "$_pp_peer" "not-read" \
				"the ${NOTIFY_RUN_BUDGET_SEC}s run budget was spent before its turn" ""
			continue
		fi
		if ! pull_one_peer "$_pp_peer"; then
			# pull_one_peer already wrote the record for THIS failure; nothing
			# more to say and nothing else to try.
			continue
		fi
		# A REACHED PEER: the state is the CURSOR's own age. "ok" and "stalled"
		# are the same reading at two thresholds — a pull that keeps connecting
		# while its cursor never moves is the failure mode nobody notices, and it
		# is precisely what a `0 new event(s)` line looks like from outside.
		_pp_age="$(cursor_age_sec "${STATE_DIR}/${PEER_CURSOR_PREFIX}${_pp_peer}${PEER_CURSOR_SUFFIX}")"
		if [ -n "$_pp_age" ] && [ "$_pp_age" -ge "$cursor_stale_sec" ]; then
			peer_health_write "$_pp_peer" "stalled" \
				"cursor has not advanced for ${_pp_age}s (threshold ${cursor_stale_sec}s, ${CURSOR_STALE_SEC_ENV}) — the peer is reachable but nothing new is arriving" \
				"$(tr -d '\n\r' <"${STATE_DIR}/${PEER_CURSOR_PREFIX}${_pp_peer}${PEER_CURSOR_SUFFIX}" 2>/dev/null)"
		else
			peer_health_write "$_pp_peer" "ok" \
				"reached; cursor ${_pp_age:-unknown}s old" \
				"$(tr -d '\n\r' <"${STATE_DIR}/${PEER_CURSOR_PREFIX}${_pp_peer}${PEER_CURSOR_SUFFIX}" 2>/dev/null)"
		fi
	done <<EOF
$_pp_peers
EOF
}

# ---- 9. main: this machine's event, then the peers, then the anchor --------
gopi_bin="$(command -v gopi || true)"
if [ -z "$gopi_bin" ]; then
	printf 'notify.sh: no `gopi` on PATH — skipping chat delivery (the event is still appended to %s)\n' "$SINK_FILE" >&2
fi

deliver_all "$(build_message "$payload" "")"

# The cursor files and the spools live in the anchor directory, so it has to
# exist before the peer pull. A failure here is REPORTED and skips the pull; the
# delivery above and the anchor below behave exactly as they always did.
sink_ready=1
if ! mkdir -p "$SINK_DIR"; then
	printf 'notify.sh: cannot create %s — peer pull skipped\n' "$SINK_DIR" >&2
	sink_ready=0
elif [ "$STATE_DIR" != "$SINK_DIR" ] && ! mkdir -p "$STATE_DIR"; then
	# The state directory is where the cursors AND the health records go; if the
	# deployment named one that cannot be created, the pull must not quietly fall
	# back to the default (that would put the cursors somewhere nobody configured).
	printf 'notify.sh: cannot create the state directory %s (%s) — peer pull skipped\n' \
		"$STATE_DIR" "$STATE_DIR_ENV" >&2
	sink_ready=0
fi
# The profile guard from the top: a run-exported worktree home must not receive
# a single write from here.
if [ "$profile_usable" -ne 1 ]; then
	sink_ready=0
fi

# THE TWO CAPABILITY PROBES, once per run, before any peer work starts.
#
#   db_multi_ok — does this `gopi` have the batch reader (`deliver_batch`)? It is
#   asked as `gopi notify --deliver-multi -h`: the verb prints its help and exits
#   0 exactly when the flag parses, so an old binary answers "unknown flag" and
#   the slow-but-correct per-line route is taken instead. Probing HERE, once, is
#   what keeps the answer from being re-derived per batch.
#
#   timeout_ok — is `timeout(1)` available to bound an ssh leg? (see
#   peer_new_lines). Absent, the leg falls back to ConnectTimeout alone.
db_multi_ok=0
timeout_ok=0
if [ -n "$gopi_bin" ] &&
	"$gopi_bin" notify "$NOTIFY_DELIVER_MULTI_FLAG" -h >/dev/null 2>&1; then
	db_multi_ok=1
fi
if command -v timeout >/dev/null 2>&1; then
	timeout_ok=1
fi
if [ "$sink_ready" -eq 1 ]; then
	if [ "$db_multi_ok" -ne 1 ]; then
		printf 'notify.sh: this `gopi` has no %s reader — peer batches fall back to ONE PROCESS PER LINE (correct, and much slower; the run budget still bounds it)\n' \
			"$NOTIFY_DELIVER_MULTI_FLAG" >&2
	fi
	sweep_stale_spools
	pull_peer_events
fi

if ! mkdir -p "$SINK_DIR"; then
	printf 'notify.sh: cannot create %s — the payload is NOT anchored\n' "$SINK_DIR" >&2
	exit 1
fi
# AN EMPTY PAYLOAD IS NOT AN ANCHOR ROW (task 01a1a640, defect B's second half).
# `printf '%s\n' ""` used to append a BLANK LINE to the anchor, and a blank line
# is indistinguishable from a torn write: the live anchor on this host carries
# three of them (lines 361, 6043, 6044 of ~/.gopi/notify/task-events.jsonl),
# which is exactly the fingerprint of a producer that handed the sink nothing.
# Appending one more would ADD that ambiguity to an append-only evidence file;
# refusing it is a REPORT, not a silence (Rule 15) — the reader learns that an
# event arrived with no bytes, and from which run, without the stream gaining a
# row that says nothing. Existing rows are never touched: nothing here deletes,
# rewrites or truncates, it only declines to write a row that carries no fact.
if [ -z "$payload" ]; then
	printf 'notify.sh: ⚠ the payload on stdin was EMPTY — no bytes arrived, so nothing is appended to %s (the delivery above reported it as a parse failure). This is the producer side to fix, not the anchor.\n' \
		"$SINK_FILE" >&2
	exit 0
fi
printf '%s\n' "$payload" >>"$SINK_FILE" || {
	printf 'notify.sh: cannot append to %s\n' "$SINK_FILE" >&2
	exit 1
}
exit 0
