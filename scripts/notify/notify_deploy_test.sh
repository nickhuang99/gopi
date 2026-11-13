#!/usr/bin/env bash
# notify_deploy_test.sh — the ONE-COMMAND, IDEMPOTENT install contract.
#
# THE COST THIS PINS. Bringing up machine N used to mean hand-copying a 40 KB
# script into the profile and hand-editing the copy's literals, so every host
# carried its own fork. `scripts/notify/deploy.sh` replaces that with one
# repeatable command; this rig measures the four properties that make it
# repeatable rather than merely runnable:
#
#   1. INSTALL      a fresh profile gets the sink, byte-identical to the repo's,
#                   mode 0755, and settings.json gains the peer + the cmd line;
#   2. IDEMPOTENT   a SECOND run writes NOTHING — not the sink, not the settings
#                   file (compared by bytes AND mtime), and it does not disturb a
#                   cursor that already exists (a re-deploy is not a reset);
#   3. NEVER OWNS   a settings.json whose notify.cmd names something ELSE is not
#                   overwritten — the wanted line is PRINTED instead;
#   4. REFUSES      a peer alias or a directory that could smuggle text into the
#                   `sh -c` command line stops the run BEFORE anything is written,
#                   and --dry-run writes nothing at all.
#
# Every case reads FILES the command wrote (sink bytes, settings JSON, cursor),
# never a variable the command set for this rig.
#
# Scratch lives OFF the `/` partition (GOPI.md Rule 22) through the shared helper.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEPLOY="$ROOT/scripts/notify/deploy.sh"
SINK="$ROOT/scripts/notify/notify.sh"
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
RIG="$(gopi_scratch_mkdir notify-deploy)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# THE RIG'S OWN HOME (measured 2026-11-03). notify.sh's refusal of a
# checkout-shaped GOPI_HOME ANCHORS the payload at $HOME/.gopi — the documented
# fallback, pinned by notify_slot_contract_test's case 6. So a sink call that
# pins GOPI_HOME but leaves HOME INHERITED files the rig's fixture payload into
# the OPERATOR's live evidence file (/home/nick/.gopi/notify/task-events.jsonl). Every
# sink invocation in this file therefore names $RIG_HOME; notify_hermeticity_test.sh
# scans for a call site that does not.
RIG_HOME="$RIG/rig-home"
mkdir -p "$RIG_HOME"

trap gopi_scratch_cleanup EXIT INT TERM

FAILS=0
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; FAILS=$((FAILS + 1)); }

command -v python3 >/dev/null 2>&1 || { echo "FATAL: python3 is required" >&2; exit 2; }
jget() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2]))' "$1" "$2"; }

echo "notify.sh one-command deploy (rig: $RIG)"

# ---- case 1: --help says what the command needs, and changes nothing --------
HELP="$(bash "$DEPLOY" --help 2>&1)"
rc=$?
if [ "$rc" -eq 0 ]; then pass "case 1: --help exits 0"; else fail "case 1: --help exited $rc"; fi
for want in --profile --peer --state-dir --dry-run; do
	case "$HELP" in
	*"$want"*) pass "case 1: --help documents $want" ;;
	*) fail "case 1: --help does not mention $want" ;;
	esac
done

# ---- case 2: a fresh profile is installed in ONE call ----------------------
H="$RIG/p1"
PROFILE="$H/.gopi"
STATE="$RIG/p1-state"
mkdir -p "$PROFILE"
printf '{\n  "sync_peers": []\n}\n' >"$PROFILE/settings.json"
bash "$DEPLOY" --profile "$PROFILE" --peer synthetic-peer --state-dir "$STATE" >"$RIG/c2.out" 2>&1
rc=$?
if [ "$rc" -eq 0 ]; then pass "case 2: the install exits 0"; else fail "case 2: install exited $rc: $(cat "$RIG/c2.out")"; fi
if [ -x "$PROFILE/notify/notify.sh" ]; then pass "case 2: the sink is installed and executable"; else fail "case 2: no executable sink at $PROFILE/notify/notify.sh"; fi
if cmp -s "$SINK" "$PROFILE/notify/notify.sh"; then
	pass "case 2: the installed sink is BYTE-IDENTICAL to the repo's (one source of truth)"
else
	fail "case 2: the installed sink differs from $SINK — a per-host fork is back"
fi
if [ "$(jget "$PROFILE/settings.json" 'd["sync_peers"]')" = "['synthetic-peer']" ]; then
	pass "case 2: the peer landed in sync_peers"
else
	fail "case 2: sync_peers is $(jget "$PROFILE/settings.json" 'd["sync_peers"]')"
fi
cmd="$(jget "$PROFILE/settings.json" 'd["notify"]["cmd"]')"
case "$cmd" in
*"bash $PROFILE/notify/notify.sh") pass "case 2: notify.cmd runs the installed sink" ;;
*) fail "case 2: notify.cmd is $cmd" ;;
esac
case "$cmd" in
*"GOPI_NOTIFY_STATE_DIR=$STATE"*) pass "case 2: notify.cmd carries the state directory the operator asked for" ;;
*) fail "case 2: notify.cmd does not carry the state dir: $cmd" ;;
esac

# ---- case 3: a SECOND run changes nothing, and spares the state -------------
mkdir -p "$STATE"
printf '2026-10-17T00:00:00+08:00\n' >"$STATE/peer-synthetic-peer-last-seen.txt"
before_settings="$(cat "$PROFILE/settings.json")"
before_settings_mtime="$(stat -c %Y "$PROFILE/settings.json")"
before_sink_mtime="$(stat -c %Y "$PROFILE/notify/notify.sh")"
before_cursor="$(cat "$STATE/peer-synthetic-peer-last-seen.txt")"
sleep 1
bash "$DEPLOY" --profile "$PROFILE" --peer synthetic-peer --state-dir "$STATE" >"$RIG/c3.out" 2>&1
rc=$?
if [ "$rc" -eq 0 ]; then pass "case 3: the second run exits 0"; else fail "case 3: second run exited $rc"; fi
if [ "$before_settings" = "$(cat "$PROFILE/settings.json")" ]; then
	pass "case 3: settings.json is byte-identical after the second run"
else
	fail "case 3: the second run rewrote settings.json"
fi
if [ "$before_settings_mtime" = "$(stat -c %Y "$PROFILE/settings.json")" ]; then
	pass "case 3: settings.json was not even touched (mtime unchanged)"
else
	fail "case 3: settings.json mtime moved on a no-op run"
fi
if [ "$before_sink_mtime" = "$(stat -c %Y "$PROFILE/notify/notify.sh")" ]; then
	pass "case 3: the sink file was left alone (no pointless rewrite)"
else
	fail "case 3: the sink was rewritten although it was already current"
fi
if [ "$before_cursor" = "$(cat "$STATE/peer-synthetic-peer-last-seen.txt" 2>/dev/null)" ]; then
	pass "case 3: an existing CURSOR survived the re-deploy (a deploy is not a reset)"
else
	fail "case 3: the re-deploy destroyed the cursor"
fi
if grep -q 'peers unchanged' "$RIG/c3.out" && grep -q 'cmd unchanged' "$RIG/c3.out"; then
	pass "case 3: the run SAYS it changed nothing (an operator can tell the two runs apart)"
else
	fail "case 3: the second run did not report itself as a no-op: $(cat "$RIG/c3.out")"
fi

# ---- case 4: a peer is added once, never twice ------------------------------
bash "$DEPLOY" --profile "$PROFILE" --peer second-peer --state-dir "$STATE" >/dev/null 2>&1
bash "$DEPLOY" --profile "$PROFILE" --peer second-peer --state-dir "$STATE" >/dev/null 2>&1
peers="$(jget "$PROFILE/settings.json" 'd["sync_peers"]')"
if [ "$peers" = "['synthetic-peer', 'second-peer']" ]; then
	pass "case 4: a repeated --peer is added exactly once"
else
	fail "case 4: sync_peers is $peers (expected each peer once)"
fi

# ---- case 5: a FOREIGN notify.cmd is never overwritten ---------------------
H5="$RIG/p5"
P5="$H5/.gopi"
mkdir -p "$P5"
printf '{\n  "sync_peers": ["synthetic-peer"],\n  "notify": {\n    "cmd": "cat >> %s/other.log"\n  }\n}\n' "$P5" >"$P5/settings.json"
bash "$DEPLOY" --profile "$P5" >"$RIG/c5.out" 2>&1
cmd5="$(jget "$P5/settings.json" 'd["notify"]["cmd"]')"
if [ "$cmd5" = "cat >> $P5/other.log" ]; then
	pass "case 5: somebody else's notify.cmd was left EXACTLY as it was"
else
	fail "case 5: deploy overwrote a foreign notify.cmd with: $cmd5"
fi
if grep -q 'cmd CONFLICT' "$RIG/c5.out" && grep -q 'cmd needed' "$RIG/c5.out"; then
	pass "case 5: it PRINTED the line the operator must install instead"
else
	fail "case 5: no conflict line printed: $(cat "$RIG/c5.out")"
fi

# ---- case 6: --print-cmd is the whole answer, and writes nothing -----------
H6="$RIG/p6"
P6="$H6/.gopi"
mkdir -p "$P6"
OUT6="$(bash "$DEPLOY" --profile "$P6" --peer synthetic-peer --print-cmd 2>&1)"
rc=$?
if [ "$rc" -eq 0 ] && [ "$(printf '%s\n' "$OUT6" | wc -l | tr -d ' ')" = "1" ]; then
	pass "case 6: --print-cmd prints exactly one line and exits 0"
else
	fail "case 6: --print-cmd printed $(printf '%s\n' "$OUT6" | wc -l | tr -d ' ') line(s), exit $rc"
fi
case "$OUT6" in
*"bash $P6/notify/notify.sh") pass "case 6: the printed line names the sink path for THAT profile" ;;
*) fail "case 6: the printed line is $OUT6" ;;
esac
if [ -e "$P6/notify/notify.sh" ] || [ -e "$P6/settings.json" ]; then
	fail "case 6: --print-cmd wrote to the profile"
else
	pass "case 6: --print-cmd wrote nothing"
fi

# ---- case 7: --dry-run plans, writes nothing -------------------------------
H7="$RIG/p7"
P7="$H7/.gopi"
mkdir -p "$P7"
printf '{\n  "sync_peers": []\n}\n' >"$P7/settings.json"
before7="$(cat "$P7/settings.json")"
bash "$DEPLOY" --profile "$P7" --peer synthetic-peer --dry-run >"$RIG/c7.out" 2>&1
if [ -e "$P7/notify/notify.sh" ]; then fail "case 7: --dry-run installed the sink"; else pass "case 7: --dry-run installed nothing"; fi
if [ "$before7" = "$(cat "$P7/settings.json")" ]; then pass "case 7: --dry-run left settings.json alone"; else fail "case 7: --dry-run rewrote settings.json"; fi
if grep -q 'would write' "$RIG/c7.out"; then pass "case 7: the plan says what it WOULD do"; else fail "case 7: no plan output"; fi

# ---- case 8: REFUSALS — nothing half-written -------------------------------
refuse() {
	local label="$1"
	shift
	if bash "$DEPLOY" "$@" >"$RIG/refuse.out" 2>&1; then
		fail "case 8: $label was ACCEPTED"
	else
		pass "case 8: $label is refused"
	fi
}
H8="$RIG/p8"
P8="$H8/.gopi"
mkdir -p "$P8"
printf '{\n  "sync_peers": []\n}\n' >"$P8/settings.json"
refuse "a peer alias carrying a shell metacharacter" --profile "$P8" --peer 'evil;id'
refuse "a peer alias carrying a quote" --profile "$P8" --peer "evil'alias"
refuse "a relative state directory" --profile "$P8" --state-dir 'relative/dir'
refuse "a state directory carrying a command substitution" --profile "$P8" --state-dir '/abs/$(id)'
refuse "a state directory carrying a space" --profile "$P8" --state-dir '/abs/dir with space'
refuse "a non-numeric cursor-stale" --profile "$P8" --cursor-stale 'ten'
refuse "a zero cursor-stale" --profile "$P8" --cursor-stale '0'
refuse "an unknown argument" --profile "$P8" --nonsense
refuse "a missing source" --profile "$P8" --source "$RIG/does-not-exist.sh"
if [ ! -e "$P8/notify/notify.sh" ] && [ "$(cat "$P8/settings.json")" = '{
  "sync_peers": []
}' ]; then
	pass "case 8: after every refusal, the profile is untouched"
else
	fail "case 8: a refused run left something behind in $P8"
fi

# ---- case 9: a profile whose settings cannot be written is REPORTED ---------
# The shape measured on this host: a SANDBOX's settings.json is a read-only
# symlink to the real one, and POSIX rename(2) would replace the LINK NODE and
# silently detach the profile (Rule 34). So the write must refuse and SAY so,
# while still installing the sink and naming the line the operator must add.
H9="$RIG/p9"
P9="$H9/.gopi"
mkdir -p "$P9" "$RIG/p9-target"
printf '{\n  "sync_peers": []\n}\n' >"$RIG/p9-target/settings.json"
ln -s "$RIG/p9-target/settings.json" "$P9/settings.json"
bash "$DEPLOY" --profile "$P9" --peer synthetic-peer >"$RIG/c9.out" 2>&1
rc=$?
if [ "$rc" -ne 0 ]; then pass "case 9: an unwritable settings file makes the deployment exit non-zero"; else fail "case 9: exit 0 although the settings were not written"; fi
if [ -L "$P9/settings.json" ]; then pass "case 9: the symlink SURVIVED (the link node was not replaced)"; else fail "case 9: the symlink was replaced by a regular file"; fi
if grep -q 'could not be written' "$RIG/c9.out" && grep -q 'put this line in' "$RIG/c9.out"; then
	pass "case 9: it names the failure AND the line to add by hand"
else
	fail "case 9: the refusal is not actionable: $(cat "$RIG/c9.out")"
fi
if [ -x "$P9/notify/notify.sh" ]; then pass "case 9: the sink was still installed (the report is not the only outcome)"; else fail "case 9: the sink was not installed"; fi

# ---- case 10: the installed sink is EXECUTABLE and runs --------------------
# The whole point of the install is a script the parent can exec. Run it the way
# `notify.cmd` does — `sh -c`, payload on stdin — against an isolated profile.
H10="$RIG/p10"
P10="$H10/.gopi"
mkdir -p "$P10"
printf '{\n  "sync_peers": []\n}\n' >"$P10/settings.json"
bash "$DEPLOY" --profile "$P10" --peer unreachable.invalid --state-dir "$RIG/p10-state" >/dev/null 2>&1
cmd10="$(jget "$P10/settings.json" 'd["notify"]["cmd"]')"
# The sink resolves its SETTINGS from $GOPI_SETTINGS first (gopi's own
# precedence, internal/config). The operator's variable must be removed here or
# this case would read the OPERATOR's profile instead of the fixture's — that is
# the rig's own contamination, not a property under test.
printf '%s\n' '{"event":"completed","task_id":"t-probe","status":"completed","summary":"probe"}' |
	env -u GOPI_SETTINGS -u GOPI_SESSIONS_ROOT \
		HOME="$RIG_HOME" GOPI_HOME="$H10" PATH=/usr/bin:/bin sh -c "$cmd10" >>"$RIG/c10.out" 2>&1
rc=$?
if [ "$rc" -eq 0 ]; then pass "case 10: the installed notify.cmd runs with the payload on stdin"; else fail "case 10: notify.cmd exited $rc"; fi
if grep -q '"task_id":"t-probe"' "$P10/notify/task-events.jsonl" 2>/dev/null; then
	pass "case 10: the event was anchored by the installed script"
else
	fail "case 10: the payload was not anchored"
fi
if [ -r "$RIG/p10-state/peer-unreachable.invalid-health.txt" ]; then
	pass "case 10: the failing peer left a health record where --state-dir said it would"
else
	fail "case 10: no health record in $RIG/p10-state"
fi

if [ "$FAILS" -ne 0 ]; then
	printf '\nnotify_deploy_test: %d assertion(s) FAILED\n' "$FAILS" >&2
	exit 1
fi
printf '\nnotify_deploy_test: all assertions passed\n'
