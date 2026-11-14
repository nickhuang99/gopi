<!-- doc-class: historical -->
# Task Report: bounded automatic retry of an abnormally-exited wrapped executor child

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: `wrapped-executor-child` (4/5). Branch: `feat/abnormal-child-retry`.
Epic restatement (the reframe the reviews anchor on) is in the commit's task
area; the short form: the `gopi task run` wrapper applies a bounded, named,
restart-survivable retry when its executor child dies ABNORMALLY, and never
when the child reported an outcome.

## Completed

- **The discriminator (the epic's non-goal, made explicit).** The wrapper reads
  the row's STATUS at the child's exit INSTANT — before its own writeback lands
  — via the new `tools.TaskRowTerminalAt`. A child that JUDGED its task closes
  the row itself, so an already-terminal row is a VERDICT: not retried. A child
  that died abnormally leaves the row open, and the wrapper's writeback is the
  only reason it will ever read `failed`. This needs no new marker in the
  ledger: it is the difference between "the row was closed before I looked" and
  "I am the one closing it". (`cmd/agent/cli_tasks.go`, the `runErr != nil`
  return.)
- **The bounded ladder** `taskRunAbnormalChildRetry`: refuses with a NAMED
  reason (judged failure / unreadable row / cap reached / holder verdict
  refuses), otherwise resumes the SAME session through the EXISTING recovery
  entry point — `gopi resume <session-or-task-id> --task <task-id>`, launched
  DETACHED, the same implementation the merger's dead-holder arm uses. The
  wrapper does not babysit the child: it starts it and returns.
- **The ledger half** (`internal/tools/task_run_retry.go`): `TaskRunRetryCap`
  (3), the append-only ordinal tag counter `run-retry:<n>` (the
  `dead-holder-resume:<n>` predecessor — never a mutable column, so a wrapper
  that CRASHES between two spawns cannot outrun its cap by forgetting), the
  named handover reason `TaskRunRetryCapReason()`, `TaskRunRetryNote`,
  `TaskRunRetryCounts` (the HIGHEST ordinal wins — a duplicated or hand-edited
  trail never buys an extra retry), and the pre-spawn double-run question
  `TaskRunChildResumable(taskID, selfPID)`.
- **`tools.SpawnTaskResume` is the EXPORTED single spawn implementation**
  (Rule 14 — the wrapper must not grow a second resume mechanism), and it
  performs the ownership handoff (`TaskStampHolder`) the moment the child
  exists, so the ledger never names a dead pid while a recovery run is live.
- **Cap handover**: at the cap the arm spends nothing, records the named reason
  on the row (`TaskUpdateReason`), and prints where the ladder ended. A failed
  handover RECORD is reported, never swallowed (Rule 15b).
- **The pre-existing hermetic escape is untouched.** `taskChildFailTestEnv`
  returns BEFORE `cmd.Wait()`, so it is the failed-WRITEBACK seam; the retry arm
  is deliberately not reachable from it (a resume spawned there would resolve
  its launcher to the TEST BINARY and recurse into the suite). That exclusion is
  asserted, not commented.

## Not Done

- **No live end-to-end spawn test.** The retry arm hangs off `cmd.Wait()`, and
  the only hermetic child-failure seam in the suite returns before it. A live
  proof would need a child that really exits non-zero with no model call —
  either the test binary launched as `gopi` (a fork hazard the wiring gate
  exists to prevent) or a rewrite of the 13 existing escape users. The two
  halves are therefore pinned separately: the DECISION in-process against a real
  ledger, the CALL SITE by parsing the production source. Honest gap: a
  production-side regression that keeps the call site but breaks the spawn
  arguments would not be caught by these tests.
- **The resume child's own exit is not observed.** The epic scope ends at
  "start the recovery run bounded"; the next cycle's attempt is counted when the
  NEXT wrapper reaches its own failed return. Nothing else was attempted.
- **`internal/tools` package test budget.** The package exceeds Go's 10m default
  timeout on this host at `TestPostMergeVetGate_ReportsThroughTheCloseLoop`
  (pre-existing, measured on the UNMODIFIED master base — see Verification).

## Verification

- Build: `go build ./...` → exit 0; `go vet ./cmd/agent/` → exit 0;
  `gofmt -l` on every touched file → clean.
- New tests (all green):
  - `go test -count=1 -run 'TestAbnormalChildRetry|TestJudgedChildFailure|TestRetryCounterSurvives|TestRetryRefuses' ./cmd/agent/` → ok 4.3s
    - bounded ladder: exactly `cap` spawns, each recorded on the trail, then ONE
      named handover with NO further spawn (idempotent at the cap);
    - judged failure: zero spawns, zero attempts spent;
    - counter survives a restart: a fresh fixture reads back 2 spent attempts,
      spends the last one, hands over — and does NOT outrun the cap;
    - refusals: an unreadable row and a spawn that started nothing both refuse
      with a reason and spend no attempt.
  - `go test -count=1 -run 'TestTaskRunRetry' ./internal/tools/` → ok 2.1s
    (high-water-mark read, ordinal append against a real ledger, the reason
    naming the cap).
  - `go test -count=1 -run 'TestRetryArm|TestRetryWiring' ./cmd/agent/` → ok 2.4s
    (the arm is called on the abnormal-child return, AFTER the writeback and
    AFTER the pre-writeback status read; the escape path does not call it; the
    gate's file is re-read with a size floor).
- Pre-existing baseline (measured before any edit, on the same worktree):
  `go test ./cmd/agent/ ./internal/tools/` → `cmd/agent` ok; **`internal/tools`
  FAILs on the 10m package budget** with the stack parked in
  `vetGateExecute`/`TestPostMergeVetGate_ReportsThroughTheCloseLoop`
  (`rebuild_vet_gate_test.go:226`) — unrelated to this change (it spawns
  `go vet` subprocesses). The targeted re-run above is green.
- Commit: `e71654e5 feat(task-run): bounded automatic retry of an abnormally-exited executor child`

## Files

- `cmd/agent/cli_tasks.go` — the discriminator read + the retry arm + its
  operator lines and named constants.
- `internal/tools/task_run_retry.go` — the ledger half (new).
- `internal/tools/task_holder_resume.go` — `SpawnTaskResume` exported, with the
  ownership handoff.
- `cmd/agent/cli_tasks_child_retry_test.go` — the three acceptance tests plus
  the two refusal tests (new).
- `cmd/agent/cli_tasks_child_retry_wiring_test.go` — the source gate pinning the
  production call site (new).
- `internal/tools/task_run_retry_test.go` — the ledger-half tests (new).
