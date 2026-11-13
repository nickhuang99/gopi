#!/usr/bin/env bash
# Real-binary acceptance rig for the probe-teardown cwd scope.
#
# RETIRED MECHANISM (merge verb removal, 2026-11-01). This generator's subject is
# the TOP-LEVEL `gopi merge` surface, which no longer exists: `gopi merge` now
# answers with one retirement line and exit 1 (measured on this tree), so the
# entry that pins `gopi merge …` output can only reproduce the RECORD, not the
# live page. The entry text is kept verbatim (it is evidence of what the binary
# printed when the campaign ran); the surviving spelling is `gopi task merge`,
# documented by `man/gopi-merge.md` (see `./bin/gopi task merge -h`).
#
# WHAT IT MEASURES: while a `gopi merge` process is alive, does its /proc/<pid>/cwd
# ever become "/"? On master the probe teardown chdir'd the RUN to "/" before the
# in-process hand-over child, so it did; with the fix the run stays where it was
# invoked. The measurement is the kernel's own reading of the live process — not a
# counter, not a log line.
#
# Usage: probe-scope-rig.sh <path-to-gopi-binary>
set -u

# Rule 22: the rig's scratch directory comes from scripts/lib/tmp-ban.sh — the
# ONE reader of the scratch root (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's
# workspace, the tree the Go suite GC sweeps), asserted off `/` by DEVICE ID and
# registered for trap-time cleanup. /tmp shares `/`'s device here, so it is never
# a candidate and there is no fallback to it.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmp-ban.sh"
trap 'gopi_scratch_cleanup' EXIT

BIN="$1"
WORK="$(gopi_scratch_mkdir probe-scope-rig)" || exit 1
repo="$WORK/repo"
mkdir -p "$repo" "$WORK/home"

export GIT_AUTHOR_NAME=rig GIT_AUTHOR_EMAIL=rig@invalid
export GIT_COMMITTER_NAME=rig GIT_COMMITTER_EMAIL=rig@invalid

git -C "$repo" init -q -b master
printf 'one\n' > "$repo/a.txt"
git -C "$repo" add a.txt
git -C "$repo" commit -q -m base

# A branch that CONFLICTS with master: forces the conflict probe (and its teardown).
git -C "$repo" checkout -q -b feat/diverged
printf 'branch side\n' > "$repo/c.txt"
git -C "$repo" add c.txt
git -C "$repo" commit -q -m 'branch side'
git -C "$repo" checkout -q master
printf 'master side\n' > "$repo/c.txt"
git -C "$repo" add c.txt
git -C "$repo" commit -q -m 'master side'

cat > "$WORK/settings.json" <<EOF
{"repos":{"$repo":{}}}
EOF

echo "repo: $repo"
echo "invocation cwd: $WORK"
echo "binary: $BIN"

GOPI_SETTINGS="$WORK/settings.json" \
GOPI_HOME="$WORK/home" \
GOPI_MEMORY_DB="$WORK/home/memory.db" \
TMPDIR="$WORK" \
ANTHROPIC_BASE_URL="http://127.0.0.1:9" \
ANTHROPIC_AUTH_TOKEN="rig-no-network" \
GOPI_TIMEOUT_SEC=20 \
  "$BIN" merge > "$WORK/run.log" 2>&1 &
pid=$!

# Sample the LIVE process's cwd from the kernel for the whole run.
saw_root=0
samples=0
first_root_ts=""
while kill -0 "$pid" 2>/dev/null; do
  cwd="$(readlink "/proc/$pid/cwd" 2>/dev/null)"
  if [ -n "$cwd" ]; then
    samples=$((samples+1))
    ts="$(date +%s.%N)"
    if [ "$cwd" = "/" ]; then
      saw_root=$((saw_root+1))
      [ -z "$first_root_ts" ] && first_root_ts="$ts"
      echo "TS $ts SAMPLE $samples: cwd=/   <-- the run relocated itself"
    else
      echo "TS $ts SAMPLE $samples: cwd=$cwd"
    fi
  fi
  sleep 0.05
done
wait "$pid" 2>/dev/null
echo "--- samples=$samples relocated=$saw_root first_relocation_ts=${first_root_ts:-none} ---"
echo "--- child session dirs (created when the hand-over child starts) ---"
ls -ldt "$WORK/home/sessions"/* 2>/dev/null | head -5
[ -d "$WORK/home/sessions" ] && for d in "$WORK/home/sessions"/*/; do
  stat -c 'SESSIONDIR %n ctime=%Z' "$d" 2>/dev/null
done | head -5
if [ "$saw_root" -gt 0 ]; then
  echo "VERDICT: RELOCATED — the run's cwd became / mid-flight"
else
  echo "VERDICT: STABLE — the run never relocated itself"
fi
echo "--- merge log (first 40 lines) ---"
head -40 "$WORK/run.log"
echo "work dir (kept for inspection): $WORK"
