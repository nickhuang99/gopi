#!/usr/bin/env bash
# night-throughput.sh — "what did the night produce?" over a time window.
#
# WHY THIS EXISTS (epic: overnight worker throughput, 2026-09-17). Four gopi
# workers stayed alive ~5.5h on 2026-09-16/17 and only a handful of branch
# merges landed in master. The question "what did the night produce" had no
# answer that could be computed: the numbers live in three places that were
# never joined — the task ledger (claims/completions), git (what actually
# landed), and the signal-provenance records (what was killed). This script is
# that join, read-only, over an arbitrary window.
#
# It does NOT define "good". It reports claimed vs landed and the termination
# census, and leaves the judgement to the reader — an instrument that flatters
# is worse than no instrument.
#
# Usage:
#   scripts/night-throughput.sh [--since 'YYYY-MM-DD HH:MM'] [--until '...'] [--repo PATH]
# Defaults: since 22:00 yesterday .. until 05:00 today is NOT derivable, so the
# window MUST be given (see the constants below for the form).
#
# Zero dependencies beyond bash, sqlite3, git and python3 (stdlib only).
set -uo pipefail

# --- constants (Rule 1: no inline magic values) --------------------------
readonly LEDGER_NAME="memory.db"
readonly SESSIONS_NAME="sessions"
readonly PROVENANCE_FILE="signal-provenance.jsonl"
readonly DEFAULT_REPO="$HOME/work/GoPi"
# The window is a PAIR of timestamps; a bare time is not a window.
readonly DEFAULT_SINCE=""
readonly DEFAULT_UNTIL=""
# Presentation only.
readonly WIDTH_RULE="────────────────────────────────────────────────────────────────"

# gopi home: GOPI_HOME wins, else $HOME/.gopi (the launcher's own resolution).
gopi_dir() {
  if [ -n "${GOPI_HOME:-}" ]; then printf '%s/.gopi\n' "${GOPI_HOME%/}"; else printf '%s/.gopi\n' "$HOME"; fi
}

SINCE="$DEFAULT_SINCE"
UNTIL="$DEFAULT_UNTIL"
REPO="$DEFAULT_REPO"
while [ $# -gt 0 ]; do
  case "$1" in
    --since) SINCE="${2:-}"; shift 2 ;;
    --until) UNTIL="${2:-}"; shift 2 ;;
    --repo)  REPO="${2:-}";  shift 2 ;;
    -h|--help)
      sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "night-throughput: unknown argument '$1'" >&2; exit 2 ;;
  esac
done

if [ -z "$SINCE" ] || [ -z "$UNTIL" ]; then
  echo "night-throughput: --since and --until are both required (e.g. --since '2026-09-16 22:00' --until '2026-09-17 05:00')" >&2
  exit 2
fi

GOPI_DIR="$(gopi_dir)"
LEDGER="$GOPI_DIR/$LEDGER_NAME"
SESSIONS="$GOPI_DIR/$SESSIONS_NAME"

echo "$WIDTH_RULE"
echo "gopi night throughput — window: $SINCE .. $UNTIL"
echo "host: $(hostname)   gopi-dir: $GOPI_DIR   repo: $REPO"
echo "$WIDTH_RULE"

# --- 1. the ledger: what was queued, claimed, finished -------------------
if [ ! -f "$LEDGER" ]; then
  echo "ledger: MISSING ($LEDGER) — no ledger numbers can be reported"
else
  sqlite3 -header -column "$LEDGER" "
    SELECT
      (SELECT COUNT(*) FROM tasks WHERE created_at BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc')) AS created,
      (SELECT COUNT(*) FROM tasks WHERE claimed_at BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc')) AS claimed,
      (SELECT COUNT(*) FROM tasks WHERE ended_at  BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc') AND status IN ('completed','verified')) AS completed,
      (SELECT COUNT(*) FROM tasks WHERE ended_at  BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc') AND status='failed')      AS failed,
      (SELECT COUNT(*) FROM tasks WHERE ended_at  BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc') AND status='cancelled')   AS cancelled;"
  echo
  echo "claims by worker:"
  sqlite3 -header -column "$LEDGER" "
    SELECT COALESCE(claimed_by,'(none)') AS worker, COUNT(*) AS claims,
           datetime(MIN(claimed_at),'unixepoch','localtime') AS first,
           datetime(MAX(claimed_at),'unixepoch','localtime') AS last
    FROM tasks WHERE claimed_at BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc')
    GROUP BY worker ORDER BY claims DESC;"
  echo
  echo "queue mix (created in window, by class):"
  sqlite3 -header -column "$LEDGER" "
    SELECT CASE
             WHEN tags LIKE '%merge-conflict%' THEN 'merge-conflict'
             WHEN tags LIKE '%bug%'            THEN 'bug'
             WHEN tags LIKE '%requirements%'   THEN 'requirements'
             WHEN tags LIKE '%recovery%'       THEN 'recovery'
             ELSE 'other' END AS class,
           COUNT(*) AS n
    FROM tasks WHERE created_at BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc')
    GROUP BY class ORDER BY n DESC;"
  echo
  echo "re-claims (a task claimed more than once inside the window):"
  sqlite3 -header -column "$LEDGER" "
    WITH w AS (SELECT task_id, COUNT(*) n FROM task_events
               WHERE event='claimed' AND created_at BETWEEN strftime('%s','$SINCE','utc') AND strftime('%s','$UNTIL','utc')
               GROUP BY task_id)
    SELECT COUNT(*) AS tasks_reclaimed, COALESCE(MAX(n),0) AS worst FROM w WHERE n>1;"
fi

# --- 2. git: what actually landed ---------------------------------------
echo
if [ -d "$REPO/.git" ]; then
  echo "landed on master in window:"
  printf '  merge commits : %s\n' "$(git -C "$REPO" rev-list master --since="$SINCE" --until="$UNTIL" --merges 2>/dev/null | wc -l)"
  printf '  all commits   : %s\n' "$(git -C "$REPO" rev-list master --since="$SINCE" --until="$UNTIL" --count 2>/dev/null)"
  git -C "$REPO" log master --since="$SINCE" --until="$UNTIL" --merges --format='  merge %h %ci %s' 2>/dev/null | head -20
else
  echo "repo: no git repo at $REPO — landed count unavailable"
fi

# --- 3. terminations: the signal-provenance census ----------------------
echo
echo "terminations recorded in window (signal-provenance kind=signal):"
if [ ! -d "$SESSIONS" ]; then
  echo "  sessions dir missing ($SESSIONS)"
else
  find "$SESSIONS" -maxdepth 2 -name "$PROVENANCE_FILE" -print0 2>/dev/null \
    | xargs -0 cat 2>/dev/null \
    | python3 -c '
import sys, json, collections
since, until = sys.argv[1], sys.argv[2]
# A record ts is ISO-8601 ("2026-09-16T22:23:13+08:00"); the window is given in
# SQL-ish wallclock ("2026-09-16 22:00"). Comparing the FIRST 16 CHARACTERS
# silently compares only the DATE, because index 10 is "T" vs " " — every record
# of the start day passed and the time-of-day filter did nothing. Normalize BOTH
# sides to the same wallclock shape first (a comparison whose operands are
# spelled differently is not a comparison).
def norm(s):
    return s.replace("T", " ")[:16]
since, until = norm(since), norm(until)
recs = []
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        recs.append(json.loads(line))
    except Exception:
        continue
sig = [r for r in recs if r.get("kind") == "signal" and since <= norm(r["ts"]) <= until]
intent = [r for r in recs if r.get("kind") == "intent" and since <= norm(r["ts"]) <= until]
by_sig = collections.Counter(r.get("signal", "?") for r in sig)
by_role = collections.Counter(r.get("role", "?") for r in sig)
print("  records: %d signal, %d intent (intent = a gopi process announcing its own kill)" % (len(sig), len(intent)))
print("  by signal: " + ", ".join("%s=%d" % kv for kv in by_sig.most_common()))
print("  by role  : " + ", ".join("%s=%d" % kv for kv in by_role.most_common()))
for r in sorted(sig, key=lambda r: r["ts"]):
    print("   %s %-7s pid=%-8s role=%-14s ppid=%s parent=%s" % (
        r["ts"][:19], r.get("signal"), r.get("pid"), str(r.get("role"))[:14],
        str(r.get("ppid")), str(r.get("parent_name")) + "|" + str(r.get("parent_argv", ""))[:36]))
if not sig:
    print("  (none — a quiet window, or the records live on the peer host)")
' "$SINCE" "$UNTIL"
fi

# --- 4. per-run start latency (is a slow start the reason?) -------------
echo
echo "first-tool-call latency (session_start → first tool_use):"
python3 -c '
import sys, os, json, glob, statistics, collections
from datetime import datetime
root = sys.argv[1]
def p(ts): return datetime.strptime(ts[:26], "%Y-%m-%dT%H:%M:%S.%f")
lat = []
for d in glob.glob(os.path.join(root, "*")):
    for f in glob.glob(os.path.join(d, "*.jsonl")):
        start = None
        try:
            with open(f) as fh:
                for i, line in enumerate(fh):
                    if i > 1200: break
                    try: o = json.loads(line)
                    except Exception: continue
                    if o.get("type") == "session_start":
                        start = o.get("timestamp")
                    elif start and o.get("type") == "assistant":
                        c = (o.get("message") or {}).get("content")
                        if isinstance(c, list) and any(isinstance(b, dict) and b.get("type") == "tool_use" for b in c):
                            lat.append((p(o["timestamp"]) - p(start)).total_seconds()); break
        except Exception: continue
if not lat:
    print("  (no session JSONL with a first tool call found)")
else:
    print("  n=%d  median %.1fs  mean %.1fs  min %.1fs  max %.1fs" % (
        len(lat), statistics.median(lat), statistics.mean(lat), min(lat), max(lat)))
    print("  buckets: " + ", ".join("%s=%d" % kv for kv in sorted(
        collections.Counter("<5s" if x < 5 else "5-30s" if x < 30 else "30-120s" if x < 120 else ">120s" for x in lat).items())))
' "$SESSIONS"
echo "$WIDTH_RULE"
echo "NOTE: this is a measurement, not a verdict. Claimed-vs-landed is not a"
echo "      productivity score: a window with merge-conflict work produces fewer"
echo "      landed branches by construction (the conflict is the work)."
