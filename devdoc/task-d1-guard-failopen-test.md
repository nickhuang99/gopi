<!-- doc-class: historical -->
# Task Report: memory-aware guard DB fail-open unit test (D1 gap)

## Completed
- Added `TestT2_Guard_DBFailure_FailsOpen` to `internal/agent/taskarea_takeover_test.go`
  (same file/style as the existing TestT2_ series — no new test file).
- Added `corruptSessionConclusionsTable` helper: renames the healthy
  `session_conclusions` table and recreates it WITHOUT the `created_at` /
  `analysis` columns.
- Test asserts the fail-open invariant at two levels:
  1. **Guard unit**: `pendingTaskAreaConclusionGuard()` returns false (no
     interception) when `SessionConclusions` errors, zero nudges injected.
  2. **Full loop**: a no-tool all-done round under the same DB fault ends
     normally in ONE API call with the real result — no nudge, no wedge,
     no infinite loop.

## Injection-point rationale (why schema corruption, not a bad DB path)
The guard's `err != nil` branch (loop.go:3940) is reachable ONLY when
`openTasksDB` + `tableExists` succeed but the read SELECT fails. An
unopenable DB path never reaches that branch: `SessionConclusions` swallows
open failure / missing table to `nil, nil` (writetaskarea.go:162-173) — the
guard sees an empty conclusion set (err==nil) and passes vacuously.
Corrupting the schema keeps open + tableExists green while making the
SELECT `... ORDER BY created_at` error — a REAL error surfaces through the
guard's err branch. A precondition assertion fails loudly if the corruption
ever stops erroring (silent nil,nil would make the pass vacuous).

## Not Done
- No behavioral changes (test-only). No real bug found → nothing to
  task_submit. The `taskAreaActive` write-only dead field and other review
  P2s were out of scope.

## Verification
- Build: `go build ./...` OK
- Vet: `go vet ./...` OK
- Tests: `go test ./internal/agent/ -run TestT2_` PASS (9 tests incl. new);
  `go test ./internal/agent/` full package PASS (28s);
  `go test ./internal/tools/ -run TestSessionConclusions` PASS
- Mutation check: flipping loop.go:3940 to `return true` makes the new test
  FAIL (guard must fail open) — test genuinely bites, not vacuous.
- Commit: e833079 `test(agent): guard DB fail-open unit test (D1 gap)`
  on branch `feat/test-guard-db-failopen` (worktree, shared repo).

## Deliverable declaration (Rule 6d)
- Branch: `feat/test-guard-db-failopen` (in main repo ~/work/GoPi)
- Recovery: `git show feat/test-guard-db-failopen:internal/agent/taskarea_takeover_test.go`
- Worktree ~/workspace/gopi-guard-failopen left in place for PA review/merge.
