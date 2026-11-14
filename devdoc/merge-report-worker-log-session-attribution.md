<!-- doc-class: historical -->
# Merge Report: fix/worker-log-session-attribution → master

## Completed
- Reproduced the merge in the branch's linked worktree: `git merge master --no-commit --no-ff`
  (branch tip e550c068, master 06a400bf) — conflicts as expected.
- Resolved every conflicted hunk, combining both sides' intent:
  - `scripts/gopi-worker-up.sh` (5 hunks) — union of the branch's session-attribution
    contract (`--output-root`, per-task `<session-id>/output.log`, hard output-root
    prerequisite, one fixed `worker-startup.log`) with master's `--count` counting
    seam (flag + dispatch + usage line) and master's range-free `usage()`
    (`sed -n '2,/^set -euo pipefail$/p'`). Master's retired `$LOGDIR`/`$LOG`
    per-worker filename path was dropped — that retirement IS the branch's purpose.
  - `internal/config/help.go`, `devdoc/*` — carried both sides' text.
- Semantic conflict from an auto-merged region: master's new gate whitelist
  `procNameIgnoredKnobs` (`internal/testutil/procname_gate_test.go`) named the
  retired `GOPI_WORKER_LOG_DIR`; the merged script's knob is the branch's
  `GOPI_WORKER_LOG_ROOT`. Whitelist updated to the merged truth (with a merge note),
  so the gate now describes the script that actually exists.
- Removed all conflict markers; `git add`; committed the merge
  (`45b8801b`, two parents e550c068 + 06a400bf).

## Not Done
- No full-suite run (`go test ./...`) — out of scope per the epic (known pre-existing hang).
- Failing `TestScriptsAreBoundedAndOffTmp` was NOT fixed: it is a pre-existing master
  defect (fails identically on the master parent alone —
  `devdoc/wip-worktree-guard-e2e.sh` does not source `scripts/lib/tmp-ban.sh`,
  `scripts_gate_test.go:155`), untouched by this merge.

## Verification
- Merge reproduced: yes (`git diff --diff-filter=U` empty; 0 conflict markers in the tree).
- Commit: `45b8801b resolve merge fix/worker-log-session-attribution into master` (2 parents, MERGE_HEAD consumed).
- Build: `go build ./...` → exit 0.
- Tests (touched packages, bounded):
  - `go test ./internal/testutil/ -run 'TestWorkerUpScriptNamesItsValues'` → PASS
  - `go test ./cmd/agent/ -run 'WorkerOutput|WorkerUp'` → ok
  - `go test ./internal/tools/` (worker-output/worker-up selection) → ok
  - `go test ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp` → FAIL (pre-existing on master, see Not Done)

## Deliverable location
- Branch `fix/worker-log-session-attribution`, merge commit `45b8801b` in `/home/user/work/GoPi`.
- Recovery: `git show fix/worker-log-session-attribution:devdoc/merge-report-worker-log-session-attribution.md`
