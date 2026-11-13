#!/usr/bin/env bash
# E2E: merge-backlog audit (foresee-review design §2)
# Builds the real binary and drives `gopi task audit-merges` against a real
# temp git repo with a temp ledger. Three assertions:
#   1. Report-only: unmerged branch listed, ledger untouched (0 rows).
#   2. --submit enqueues exactly one pending "merge <branch> into master" row.
#   3. Dedup: a re-audit with --submit submits nothing new (branch+tip dedup).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Scratch root under the shared workspace scratch home (never /tmp — the
# 2026-09-11 exhaustion incident). trap cleanup kills child processes first.
# shellcheck source=lib/tmp-ban.sh
. "$REPO_ROOT/scripts/lib/tmp-ban.sh"
TMP="$(gopi_scratch_mkdir merge-backlog)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
if gopi_dry_run_requested "$@"; then
    gopi_dry_run_report e2e-merge-backlog "$TMP" \
        "build bin/gopi, create a temp git repo (master + unmerged feat/e2e + merged feature), run task audit-merges report/--submit/dedup" \
        git
    exit $?
fi

echo "==> build"
GOPI_BIN="$TMP/gopi"
(cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o "$GOPI_BIN" ./cmd/agent/)

echo "==> fixture repo: master + unmerged feat/e2e + merged feature"
export GIT_AUTHOR_NAME=e2e GIT_AUTHOR_EMAIL=e2e@test GIT_COMMITTER_NAME=e2e GIT_COMMITTER_EMAIL=e2e@test
git init -q -b master "$TMP/repo"
cd "$TMP/repo"
echo base > f.txt; git add f.txt; git commit -qm "base"
git checkout -qb feat/e2e
echo branch > g.txt; git add g.txt; git commit -qm "feat: e2e branch work"
git checkout -q master
git checkout -qb feature/merged
git checkout -q master
git merge -q --no-ff -m "merge feature/merged" feature/merged 2>/dev/null || git merge --no-ff -m "merge feature/merged" feature/merged

export GOPI_MEMORY_DB="$TMP/memory.db"

echo "==> assertion 1: report-only lists the branch, writes nothing"
OUT="$("$GOPI_BIN" task audit-merges --repo "$TMP/repo")"
echo "$OUT"
echo "$OUT" | grep -q "merge-backlog audit (report-only)"
echo "$OUT" | grep -q "feat/e2e"
# feature/merged may appear in the report table (ahead=0 posture), but it
# must never be submitted — verified in assertion 2 via submitted=1 not 2.
if echo "$OUT" | grep -q "feature/merged.*submitted"; then echo "FAIL: merged feature submitted"; exit 1; fi
ROWS=$(sqlite3 "$GOPI_MEMORY_DB" "SELECT COUNT(*) FROM tasks" 2>/dev/null || echo "0")
[ "$ROWS" = "0" ] || { echo "FAIL: report-only wrote $ROWS row(s)"; exit 1; }
echo "PASS 1/3: report-only clean"

echo "==> assertion 2: --submit enqueues one pending merge task"
OUT="$("$GOPI_BIN" task audit-merges --repo "$TMP/repo" --db "$GOPI_MEMORY_DB" --submit)"
echo "$OUT"
echo "$OUT" | grep -q "submitted=1" || { echo "FAIL: expected submitted=1"; exit 1; }
ROW=$(sqlite3 "$GOPI_MEMORY_DB" "SELECT status||'|'||summary||'|'||tags FROM tasks")
echo "row: $ROW"
echo "$ROW" | grep -q "^pending|merge feat/e2e into master|" || { echo "FAIL: row shape wrong: $ROW"; exit 1; }
echo "PASS 2/3: submit enqueues pending merge row"

echo "==> assertion 3: re-audit with --submit dedupes (branch+tip)"
OUT="$("$GOPI_BIN" task audit-merges --repo "$TMP/repo" --db "$GOPI_MEMORY_DB" --submit)"
echo "$OUT"
echo "$OUT" | grep -q "submitted=0 duped=1" || { echo "FAIL: expected duped=1 submitted=0"; exit 1; }
ROWS=$(sqlite3 "$GOPI_MEMORY_DB" "SELECT COUNT(*) FROM tasks")
[ "$ROWS" = "1" ] || { echo "FAIL: ledger grew to $ROWS rows"; exit 1; }
echo "PASS 3/3: re-audit dedupes"

echo "ALL E2E PASS"
