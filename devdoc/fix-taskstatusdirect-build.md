<!-- doc-class: historical -->
# Task Report: Fix build — undefined taskStatusDirect

## Completed
- Diagnosed `make` failure: `internal/tools/memory_sync_claim.go:603:19: undefined: taskStatusDirect`.
- Root cause: uncommitted claim-lock edits (raced-writeback honesty fix, incident t-1788329099) in
  `memory_sync_claim.go` + `memory_sync_task.go` referenced helper `taskStatusDirect(taskID)` which
  existed nowhere in the repo (peerbox repo builds fine — likely has the full commit).
- Implemented `taskStatusDirect` in `internal/tools/memory_sync_task.go` (after `taskSyncDB`):
  thin wrapper — opens sync DB, delegates to existing `taskByID` (tasks.go:899).
- Fixed type bug in same hunk: `row != ""` on `*Task` → `row != nil` (memory_sync_claim.go:603).

## Not Done
- Changes left UNCOMMITTED on purpose (they sit on top of the user's in-flight claim-lock edits).
- peerbox-side full commit for these edits not fetched; if the user later pulls a peerbox commit that
  defines `taskStatusDirect`, resolve by keeping one definition only.
- Pre-existing flake `TestJobNoNoticeOnKill` fails in full-suite runs (passes in isolation) —
  unrelated to this fix, not addressed.

## Verification
- Build: `make` → OK (CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)
- Vet: `go vet ./internal/tools/` → clean
- Tests: `go test ./internal/tools/ -run 'TestClaimLock|TestRollbackLocalClaim|TestRunClaimLock'` → PASS
- Full suite: 1 unrelated flake (TestJobNoNoticeOnKill), passes with -run isolation.
