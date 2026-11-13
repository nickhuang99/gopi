#!/usr/bin/env bash
# pre-merge-safety-check: which un-finished task's branch is unprotected
#       right now, and what `gopi merge` will do with it?
#
#
# RETIRED MECHANISM (merge verb removal, 2026-11-01). Every `gopi merge` below is
# the RECORD of the surface this script was written against; that top-level verb
# was removed and answers one retirement line with exit 1 today (measured), and
# the fence it describes lives on under `gopi task merge`. Kept verbatim as the
# script's own history — the READ side here (ledger + settings "repos" table) is
# still the resolution both spellings use, so the check itself is not void.
# WHY THIS SCRIPT STILL EXISTS, and why its verdict changed (2026-09-22).
#
# It was written for the exposure that epic merge-candidate-live-owner-fence
# fixed: `gopi merge` had no decision-time question about the TASK, so a branch
# belonging to a not-yet-finished task was a BARE candidate — nothing but three
# repository-side protections (the integration branch, an archive/<name> tag, a
# worktree holding it) stood between an unfinished run's partial work and master.
# The script's job was to enumerate the bare ones so an operator could tag them
# out before running the command.
#
# The fence (cmd/agent/merge_run.go, mergeLiveOwnerParks) now asks that question
# itself, one ledger read per candidate, BEFORE the branch reaches any lane that
# could measure, hand over or land it. So a "bare" branch — unfinished task, no
# worktree, no archive tag — is no longer UNSAFE: it is exactly what the fence
# parks, by design and with the row id in its output. The list below is now a
# PREDICTION of what the command will report, not a list of branches to protect
# by hand, and the verdict says so.
#
# What remains worth running by hand: the fence reads the ledger the process it
# runs in can reach (GOPI_MEMORY_DB / the profile's memory.db). This script reads
# the SAME database through the same resolution, so a mismatch between "what I
# see" and "what the command sees" is visible here rather than at merge time.
#
# Usage: bash scripts/pre-merge-safety-check.sh
#   GOPI_MEMORY_DB=/path/to/memory.db bash scripts/pre-merge-safety-check.sh
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

python3 - <<'PY'
import sqlite3, subprocess, json, sys, os

# Resolve the ledger the SAME way the tools layer does: an explicit
# GOPI_MEMORY_DB wins, otherwise the profile's memory.db. Reporting a different
# database than the command reads would make this check lie about the fence.
DB = os.environ.get("GOPI_MEMORY_DB") or os.path.expanduser("~/.gopi/memory.db")
try:
    db = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
except Exception as e:
    sys.exit(f"cannot read ledger {DB}: {e}")

# LIVE only — the fence's own predicate (isLiveTaskStatus: pending/claimed/
# in_progress). completed/verified/failed/cancelled are deliberately absent: a
# terminal row never parks a branch, so listing one here would predict a park
# that will not happen.
rows = db.execute(
    "SELECT task_id,status,deliverable,summary FROM tasks "
    "WHERE status IN ('pending','in_progress','claimed') AND deliverable LIKE '%branch%'"
).fetchall()

def sh(cmd):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True).stdout.strip()

# WHICH REPOS THE COMMAND SWEEPS. `gopi merge` takes its repo set from the
# settings table (config.ResolveMergerRepos → settings.json "repos"), and the
# fence applies its repo test only to repos in that set — a row naming a repo
# the command never sweeps is not a park at all. Read through the same
# precedence the config layer uses: GOPI_SETTINGS, else <gopi dir>/settings.json.
SETTINGS = os.environ.get("GOPI_SETTINGS") or os.path.join(
    os.environ.get("GOPI_HOME") or os.path.expanduser("~/.gopi"), "settings.json")
swept = []
try:
    with open(SETTINGS) as fh:
        for p in sorted(json.load(fh).get("repos") or {}):
            if p.strip():
                swept.append(p)
except FileNotFoundError:
    print(f"note: no settings table at {SETTINGS} — the command would sweep no repo at all")
except Exception as e:
    print(f"note: cannot read {SETTINGS}: {e}")

# The branch/ref questions are asked IN THE REPO THEY CONCERN (`git -C <repo>`),
# never in this script's own checkout: a branch that protects a row in repo A
# does not exist in repo B, and asking the wrong tree would report every such
# row as "branch already gone" (the silent all-clear this check must not give).
def repo_of(row_repo):
    """The repos a row's branch must be looked for in, as (path, is_row_repo)."""
    if row_repo:
        return [(row_repo, True)]
    return [(p, False) for p in swept]

parked, foreign, gone = [], [], []
for tid, st, deliv, summ in rows:
    try:
        d = json.loads(deliv or "{}")
    except Exception:
        continue
    b = (d.get("branch") or "").strip()
    if not b:
        continue
    rowrepo = (d.get("repo") or "").strip()
    if not rowrepo and not swept:
        continue
    for repo, is_row_repo in repo_of(rowrepo):
        # A row naming a repo the command does not sweep is NOT parked by the
        # fence (mergeOwnerLookupForRepo keeps rows naming this repo, or none) —
        # because the same branch name legitimately exists in two repos and a
        # foreign live row must not freeze this one. Reported separately, never
        # counted as a park this check predicted.
        if is_row_repo and swept and not any(sh(f"git -C {repo!r} rev-parse --git-common-dir")
                                             == sh(f"git -C {p!r} rev-parse --git-common-dir") for p in swept):
            foreign.append((b, tid, repo))
            continue
        if not sh(f"git -C {repo!r} rev-parse --verify --quiet refs/heads/{b}"):
            gone.append((b, tid, repo))
            continue
        held = bool(sh(f"git -C {repo!r} worktree list | grep -F \"[{b}]\""))
        tagged = bool(sh(f"git -C {repo!r} tag -l \"archive/{b}\""))
        if held or tagged:
            continue                               # an older fence already covers it
        parked.append((b, tid, st, repo, summ[:60]))

print(f"ledger:   {DB}")
print(f"settings: {SETTINGS} — {len(swept)} repo(s) swept")
print()
print("THE FENCE PARKED SET — `gopi merge` skips each of these, naming the row and its status:")
if not parked:
    print("  none")
for b, tid, st, repo, summ in parked:
    print(f"  {b}  (in {repo})")
    print(f"      task {tid}  status={st}  worktree=no  archive-tag=no")
    print(f"      {summ}")
print()
if gone:
    print("NOT a candidate — the branch no longer exists (nothing left to protect):")
    for b, tid, repo in gone:
        print(f"  {b}  task {tid}  in {repo}")
    print()
if foreign:
    print("NOT parked — the row records a repo the command does not sweep:")
    for b, tid, repo in foreign:
        print(f"  {b}  task {tid}  repo={repo}")
    print()
print("VERDICT:",
      "nothing for the operator to do — the fence parks every branch above (no manual tagging needed)"
      if parked or foreign
      else "SAFE — no unfinished task has an unprotected branch in a swept repo")
PY
