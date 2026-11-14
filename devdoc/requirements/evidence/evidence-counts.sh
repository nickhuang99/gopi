#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Evidence-class counters for the reverse-requirement-analysis series.
#
# Prints the machine-derived counts that devdoc/requirements/evidence/
# evidence-index.md quotes. Run it and compare: every number in the index is a
# measurement, and a stale number is a visible diff rather than a silent claim.
#
# Usage:  bash devdoc/requirements/evidence/evidence-counts.sh
# Env:    GOPI_LEDGER_DB  ledger path (default /home/user/.gopi/memory.db)
#         GOPI_REPO      repo root   (default: the worktree this script is in)
#
# Dependencies (Rule 10.7 preflight): sqlite3, grep, git, bash. Nothing else.
# The ledger is opened READ-ONLY (mode=ro); nothing is written anywhere (Rule 22).
# ---------------------------------------------------------------------------
set -euo pipefail

DB_PATH="${GOPI_LEDGER_DB:-/home/user/.gopi/memory.db}"
REPO_ROOT="${GOPI_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}"
DB="file:${DB_PATH}?mode=ro"
WHERE="source='human'"

cd "$REPO_ROOT"

# sqlite3 -list one value, no header — the shape a shell wants.
sql() { sqlite3 "$DB" "$1"; }

echo "== E1  source=human ledger rows =="
sql "select printf('ledger rows total          : %d', count(*)) from tasks;"
sql "select printf('source=human               : %d', count(*)) from tasks where $WHERE;"
sql "select printf('  of those telemetry (gopi run): %d', count(*)) from tasks where $WHERE and summary like 'gopi run%';"
sql "select printf('  substantive (non-telemetry)  : %d', count(*)) from tasks where $WHERE and summary not like 'gopi run%';"
sql "select printf('  with payload >200 chars      : %d', count(*)) from tasks where $WHERE and length(payload)>200;"
sql "select printf('  summary-only (payload<=200)  : %d', count(*)) from tasks where $WHERE and summary not like 'gopi run%' and (length(payload)<=200 or payload='');"
sql "select printf('  operator-quote marker hit    : %d', count(*)) from tasks where $WHERE and (payload like '%nick 原话%' or payload like '%nick 终审%' or payload like '%nick 拍板%' or payload like '%nick 明确要求%' or payload like '%nick 同意%' or payload like '%nick 裁决%' or payload like '%不采用%' or payload like '%我们不%' or payload like '%I want%');"
sql "select printf('  id shape t-<unix>            : %d', count(*)) from tasks where $WHERE and task_id like 't-%';"
sql "select printf('  id shape <uuid>              : %d', count(*)) from tasks where $WHERE and task_id not like 't-%';"
sql "select printf('  snapshot max(created_at) IS  : %s', datetime(max(created_at),'unixepoch')) from tasks where $WHERE;"
echo "snapshot max(created_at) epoch     : $(sql "select max(created_at) from tasks where $WHERE;")"

echo
echo "== E2  GOPI.md (intent-stating rules) =="
echo "rule headings '## Rule N:'          : $(grep -c '^## Rule [0-9]' GOPI.md)"
echo "gate headings '### Rule N —'        : $(grep -c '^### Rule [0-9]' GOPI.md)"
echo "rules carrying a '**Why**' block    : $(grep -c '^\*\*Why\*\*' GOPI.md)"
echo "file lines                          : $(wc -l < GOPI.md)"

echo
echo "== E3  devdoc rationale/decision docs (tree, not history) =="
echo "devdoc *.md in the worktree         : $(ls devdoc/*.md 2>/dev/null | wc -l)"
echo "devdoc *.md on master (git ls-tree) : $(git ls-tree -r --name-only master -- devdoc | grep -c '\.md$')"
echo "name matches decision|ruling|why    : $(ls devdoc/ 2>/dev/null | grep -icE 'decision|ruling|why')"
echo "docs carrying a rationale phrase    : $(grep -lriE 'because|so that|instead of|root cause' devdoc/*.md 2>/dev/null | wc -l)"

echo
echo "== E4  commit messages explaining a why =="
echo "commits reachable (--all)           : $(git log --all --format=%H | wc -l)"
echo "subject/body rationale grep         : $(git log --all -i -E --grep='because|so that|instead of|to avoid|root cause|rather than|the reason|the real' --format=%H | wc -l)"
echo "commit body quoting the operator    : $(git log --all -i -E --grep='operator ruling|operator.s rule|nick .*终审|原话' --format=%H | wc -l)"

echo
echo "== E5  intent-stating comments in production Go =="
echo "matching lines (internal cmd)       : $(grep -rn --include=*.go '^[[:space:]]*// \(Why\|Because\|So that\|Instead of\|Deliberately\|NOTE:\)' internal cmd | grep -v '_test.go' | wc -l)"
echo "matching files                      : $(grep -rln --include=*.go '^[[:space:]]*// \(Why\|Because\|So that\|Instead of\|Deliberately\|NOTE:\)' internal cmd | grep -v '_test.go' | wc -l)"
echo "production .go files                : $(find internal cmd -name '*.go' ! -name '*_test.go' | wc -l)"

echo
echo "== what is NOT recorded =="
sql "select printf('human rows with EMPTY deliverable      : %d', count(*)) from tasks where $WHERE and trim(deliverable)='';"
sql "select printf('human rows with EMPTY verify_evidence  : %d', count(*)) from tasks where $WHERE and trim(verify_evidence)='';"
sql "select printf('cancelled rows, reason EMPTY           : %d', count(*)) from tasks where $WHERE and status='cancelled' and trim(reason)='';"
sql "select printf('failed rows, reason EMPTY              : %d', count(*)) from tasks where $WHERE and status='failed' and trim(reason)='';"
sql "select printf('human rows with ZERO task_events rows  : %d', count(*)) from tasks t where t.source='human' and not exists(select 1 from task_events e where e.task_id=t.task_id);"
sql "select printf('duplicate-summary groups (one ask, N rows): %d', count(*)) from (select summary from tasks where $WHERE group by summary having count(*)>1);"
sql "select printf('  extra rows those groups account for  : %d', coalesce(sum(n),0)) from (select count(*)-1 n from tasks where $WHERE group by summary having count(*)>1);"
echo "session dirs present, chats table rows : $(ls ~/.gopi/sessions 2>/dev/null | wc -l) / $(sql 'select count(*) from chats;')"
echo "distilled_qa rows (no human-need field): $(sql 'select count(*) from distilled_qa;')"
