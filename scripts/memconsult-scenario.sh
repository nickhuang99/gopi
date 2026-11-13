#!/usr/bin/env bash
# memconsult-scenario.sh — Rule 11 adoption proof for the memory-consult epic.
#
# Runs >=2 REAL tasks through the rebuilt bin/gopi against the live harness, so
# the two mechanisms of this epic are demonstrated by LLM behaviour rather than
# by unit tests:
#
#   scenario A — the review-time consult checkpoint (D1) must appear in the
#                session JSONL as a logged user event at the first commit.
#   scenario B — the adoption telemetry (D2) must measure a search whose hits
#                were actually CONSUMED (a later memory feedback naming a hit
#                id), so rows_consumed > 0 in the run's own stats line.
#
# Usage: bash scripts/memconsult-scenario.sh [workdir]
# Prints, for each scenario: the session id, the checkpoint event count, and
# the adoption line. Never mutates the repo tree (runs in a scratch workdir).
set -u

# The scratch-root contract: never /tmp on this host (2026-09-11 / exhaustion).
# shellcheck source=lib/tmp-ban.sh
. "$(dirname "$0")/lib/tmp-ban.sh"

BIN="$(cd "$(dirname "$0")/.." && pwd)/bin/gopi"
SESSIONS="$HOME/.gopi/sessions"
# A caller may pass a workdir (already asserted off /); otherwise take one from
# the shared scratch helper, so the default is bounded and GC-covered.
if [ "$#" -ge 1 ]; then
	WORKDIR="$1"
	gopi_assert_off_root_fs "$WORKDIR" || exit 1
else
	WORKDIR="$(gopi_scratch_mkdir memconsult-scenarios)" || exit 1
	trap gopi_scratch_cleanup EXIT INT TERM
fi
# Logs live in their own absolute directory, so the reporting below reads them
# no matter what CWD the script was started from (the runs themselves CWD into
# $WORKDIR — relative log names landed there and the report read nothing).
OUTDIR="$(dirname "$WORKDIR")/$(basename "$WORKDIR")-logs"
mkdir -p "$OUTDIR"

run_scenario() {
	local name="$1" prompt="$2"
	echo "=== scenario $name ==="
	local before out err
	before=$(ls -t "$SESSIONS" | head -1)
	# Absolute output paths: the run happens with CWD=$WORKDIR, so a relative
	# name would drop the logs THERE and the reporting below (which runs in the
	# script's CWD) would read nothing — the defect that made this script print
	# empty session ids.
	out="$OUTDIR/$name.out"
	err="$OUTDIR/$name.err"
	( cd "$WORKDIR" && printf '%s\n' "$prompt" | "$BIN" >"$out" 2>"$err" )
	local sid f
	# The run's own log path is printorder on stderr ("[gopi] session: ...");
	# reading it from there is exact — guessing from the newest UUID dir picks
	# a sibling session when several runs overlap.
	f=$(grep -o "$SESSIONS/[0-9a-f-]*/session.jsonl" "$err" | head -1)
	sid=$(basename "$(dirname "$f")")
	echo "session_id: $sid"
	echo "jsonl: $f"
	echo "consult-checkpoint user events: $(grep -c 'MEMORY-CONSULT CHECKPOINT' "$f" 2>/dev/null)"
	echo "memory tool_use blocks (action + row id): $(jq -c 'select(.type=="assistant")|.message.content[]?|select(.type=="tool_use" and .name=="memory")|(.input.action + (if .input.id then " #"+(.input.id|tostring) else "" end))' "$f" 2>/dev/null | tr '\n' ' ')"
	echo "trajectory stats (stderr):"
	grep -E '^(memory|memory adoption|consult|rounds=)' "$err" 2>/dev/null | head -6
	if [ "$before" != "$sid" ]; then echo "new session: yes"; fi
	echo
}

run_scenario A "Do a two-step bookkeeping task in this directory: (1) create the file note.txt containing exactly the single line 'memory consult scenario A'; (2) read it back and confirm the content matches. Maintain a task list with exactly those two items and commit each item via task(action=commit, status=completed, conclusion=...) as you complete it. Then reply DONE. Keep it to at most 8 tool calls."

run_scenario B "Use memory(action=search) to find our distilled lesson about reading the LIVE memory.db WAL database safely, then follow it: verify with sqlite3 in read-only mode that the distilled_qa table has rows (report the row count), and vote on the search hit you used with memory(action=feedback, rating=useful, id=<the #N of that hit>). Keep it to at most 10 tool calls."

echo "=== done ==="
