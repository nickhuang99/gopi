#!/usr/bin/env bash
# memconsult-cost-probe.sh — reproducible cost probe for the memory-consult epic.
#
# Runs the SAME trivial task through the rebuilt bin/gopi in three arms that
# differ ONLY in the enforcement switches, and reports the per-run cost from the
# session log's own `request` events (prompt_tokens / cache_hit / cache_miss per
# API request) — not from a raw-log grep.
#
#   arm on        : both checkpoints armed (task-start + review-time consult)
#   arm consult-off: GOPI_NO_ENFORCE_MEMORY_CONSULT=1 (review-time checkpoint off)
#   arm all-off   : GOPI_NO_ENFORCE_MEMORY=1 (every harness memory checkpoint off)
#
# Why the session log and not the raw SSE log: one `message_start` token appears
# TWICE in the raw log (`event: message_start` line + the `data: {...}` JSON),
# so `grep -c message_start` double-counts requests — the exact measurement
# defect this probe exists to avoid. The session log's `request` event is
# emitted once per API request by the harness itself.
#
# Also reports, per arm, the request-payload index in which the consult
# checkpoint message first appears, so "the checkpoint is paid at which request"
# is measured rather than assumed (the injection fires at the first commit, NOT
# at request 1).
#
# Usage: bash scripts/memconsult-cost-probe.sh
set -u

# shellcheck source=lib/tmp-ban.sh
. "$(dirname "$0")/lib/tmp-ban.sh"

BIN="$(cd "$(dirname "$0")/.." && pwd)/bin/gopi"
SESSIONS="$HOME/.gopi/sessions"
WORKDIR="$(gopi_scratch_mkdir memconsult-cost)" || exit 1
trap gopi_scratch_cleanup EXIT INT TERM

# The one task every arm runs — trivial on purpose: the delta between arms is
# the mechanism's own footprint, not task-dependent work. It MUST commit a
# task-list item, because the review-time checkpoint fires at the FIRST COMMIT:
# a probe prompt with no commit would show "marker events=0" in every arm and
# measure nothing (the defect that produced the report's first cost table).
read -r -d '' PROMPT <<'EOF' || true
Do this two-step bookkeeping task here and maintain a task list with exactly
those two items, committing each with task(action=commit, status=completed,
conclusion=...):
(1) create the file cost-probe.txt containing exactly the single line 'cost probe';
(2) read it back and confirm the content matches.
Keep to at most 6 tool calls, then reply DONE.
EOF

probe() {
	local arm="$1" env_prefix="$2"
	local out="$WORKDIR/$arm.out" err="$WORKDIR/$arm.err"
	( cd "$WORKDIR" && printf '%s\n' "$PROMPT" | env $env_prefix "$BIN" >"$out" 2>"$err" )
	local f sid
	f=$(grep -o "$SESSIONS/[0-9a-f-]*/session.jsonl" "$err" | head -1)
	sid=$(basename "$(dirname "$f")")
	if [ -z "$f" ] || [ ! -f "$f" ]; then
		echo "ARM $arm: no session log (see $err)"
		return
	fi
	python3 - "$arm" "$sid" "$f" <<'PY'
import json, sys
arm, sid, path = sys.argv[1], sys.argv[2], sys.argv[3]
reqs, marker_at, marker_n = [], None, 0
for line in open(path, encoding="utf-8", errors="replace"):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    t = e.get("type")
    if t == "request":
        reqs.append((e.get("prompt_tokens", 0), e.get("cache_hit", 0),
                     e.get("cache_miss", 0), e.get("completion_tokens", 0)))
    elif t == "user":
        c = e["message"].get("content")
        s = c if isinstance(c, str) else json.dumps(c, ensure_ascii=False)
        if "MEMORY-CONSULT CHECKPOINT" in s:
            marker_n += 1
            if marker_at is None:
                marker_at = len(reqs)
# The FULL session id, never a prefix: an 8-character chunk names more than one
# session and cannot be pasted into `gopi log <id>` (which refuses a truncated
# id). This label is how a reader goes from the probe's output to the run.
print(f"ARM {arm} session={sid}")
print(f"  requests={len(reqs)}")
print(f"  prompt_tokens  first={reqs[0][0] if reqs else 0} "
      f"total={sum(r[0] for r in reqs)}")
print(f"  cache_hit first={reqs[0][1] if reqs else 0} total={sum(r[1] for r in reqs)}")
print(f"  completion_tokens total={sum(r[3] for r in reqs)}")
print(f"  consult marker events={marker_n} first_payload_after_request={marker_at}")
PY
}

echo "### memory-consult cost probe (workdir $WORKDIR)"
probe "on"          "GOPI_NO_ENFORCE_MEMORY=0 GOPI_NO_ENFORCE_MEMORY_CONSULT=0"
probe "consult-off" "GOPI_NO_ENFORCE_MEMORY=0 GOPI_NO_ENFORCE_MEMORY_CONSULT=1"
probe "all-off"     "GOPI_NO_ENFORCE_MEMORY=1 GOPI_NO_ENFORCE_MEMORY_CONSULT=0"
echo "### done"
