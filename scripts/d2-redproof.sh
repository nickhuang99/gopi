#!/usr/bin/env bash
# d2-redproof.sh — reproduce the D2 terminal guard's RED proof, reproducibly.
#
# WHY: the acceptance for D2 (a merge-backlog row must not reach a success
# terminal while its branch is provably unmerged) is only credible if the
# regression test is shown to go RED when the guard is reverted.  A transcript
# in a devdoc cannot be re-run by a reviewer; this script can.
#
# WHAT IT DOES: neutralizes the guard's SINGLE decision predicate
# (mergeBacklogTerminalVerdictFor) with an early `Allow: true` — the exact
# revert shape — runs the three guard tests that must then fail, and restores
# the file from a backup.
#
# SAFETY: the restore is a `trap` on EXIT/INT/TERM/HUP, and the script ends by
# verifying the file is byte-identical to the backup (cmp).  A crash mid-run
# therefore cannot leave a neutralized guard behind unnoticed.
#
# Rule 22: no /tmp — the backup scratch lives under the worktree's own
# .d2proof/ directory.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
guard="$repo/internal/tools/task_merge_backlog_terminal_guard.go"

# Rule 30: the proof's own scratch must NOT be left inside the worktree — a run
# ends with `git status --porcelain` printing nothing, and `.d2proof/` was
# untracked litter. Rule 22: it must not be an ambient /tmp path either, so the
# project's scratch helper supplies a measured path off the / partition (it
# asserts the device id). `.d2proof/` stays as the fallback for a checkout that
# predates the helper.
proof="$repo/.d2proof"
scratch_cleanup() { :; }
if [ -f "$repo/scripts/lib/tmp-ban.sh" ]; then
  # shellcheck source=lib/tmp-ban.sh
  . "$repo/scripts/lib/tmp-ban.sh"
  if proof="$(gopi_scratch_mkdir d2-redproof 2>/dev/null)"; then
    scratch_cleanup() { gopi_scratch_cleanup; }
  else
    proof="$repo/.d2proof"
  fi
fi
backup="$proof/guard.bak"

mkdir -p "$proof"
cp "$guard" "$backup"
restore() { cp "$backup" "$guard"; }
# The EXIT trap additionally drops the scratch — but the script's own
# mid-run restore below must NOT, or it would delete the backup before the
# cmp check that proves the restore was byte-exact.
cleanup() { restore; scratch_cleanup; }
trap cleanup EXIT INT TERM HUP

# The revert: one early allow inside the single predicate both doors call.
awk '
  /^func mergeBacklogTerminalVerdictFor\(/ { inp = 1; print; next }
  inp { print "\treturn mergeBacklogTerminalVerdict{Governed: true, Allow: true, Note: \"D2 REDPROOF: guard reverted\"}"; inp = 0 }
  { print }
' "$guard" > "$guard.redproof" && mv "$guard.redproof" "$guard"

if ! grep -q "D2 REDPROOF" "$guard"; then
  echo "FAILED to neutralize the guard predicate — the anchor moved; aborting" >&2
  exit 2
fi

cd "$repo"
set +e
go test ./internal/tools/ -count=1 -timeout 20m \
  -run 'TestMergeBacklogTerminalGuard_(RefusesUnmergedBranch|RefusalIsRecorded|D2ExactShape|SyncDoorRecordsInsteadOfRefusing)' \
  2>&1 | tee "$proof/red-run.txt"
red=${PIPESTATUS[0]}
set -e

restore
if ! cmp -s "$guard" "$backup"; then
  echo "RESTORE FAILED — $guard differs from $backup" >&2
  exit 3
fi

echo "RED_EXIT=$red  (non-zero = the guard's absence is detected: the proof holds)"
if [ "$red" -eq 0 ]; then
  echo "the reverted guard did NOT go red — the regression test does not guard D2" >&2
  exit 1
fi
