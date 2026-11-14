<!-- doc-class: normative -->
# Task Report: P1 bug fix — verify briefing dead flag + serve daemon crash (bugfix-task-p1)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-02 · Branch: `feat/fix-task-p1` · Source review: `devdoc/review-cli-task-core.md` P1 items 2/3

## Completed

### P1#2 — verify briefing instructed a dead flag; evidence lost

**Root cause** (probe-verified): the verify child's briefing
(`internal/tools/tasks.go` TaskVerify) told the child
`gopi task update <id> --status X --summary "evidence: ..."` — but `--summary`
was removed by M4.5 (bug056: summary immutable); `taskFlagMember("update")`
whitelisted only `db|status`. The child burned tool calls discovering the
unknown flag, then wrote `verified` with NO evidence. Even without the flag
error, plain `TaskUpdate` drops the digest — the evidence chain was dead on
master.

A SECOND latent break surfaced during root-cause (probe-proven): the FAIL
verdict `completed → failed` was REFUSED by the local transition gate
("illegal status transition completed → failed") while the M4.20-B rank
design (terminal tier-3 peer 后到覆盖) and the sync layer already allow it —
the child's FAIL writeback could never land at all.

**Fix shipped** (the review's "better" option):
1. `gopi task update <id> --status X --result "evidence|reason: ..."` — the
   `--result` flag routes to `TaskUpdateWithResult`: the digest rides the
   append-only task_events trail (`task show` renders it as the `result:`
   line), the summary column stays immutable.
   - `taskFlagMember("update")` now admits `result` (whitelist);
   - `parseTasksArgs` maps `--result` → `opts.result`;
   - `runTasksSubcommand` `update` case dispatches to TaskUpdateWithResult
     when `--result` is present, else the status-only TaskUpdate;
   - usage text updated (cmd/agent/cli_tasks.go + internal/config/help.go).
2. Transition gate now admits terminal-tier PEER moves `completed↔failed`
   (`taskAllowedTransitions` in internal/tools/tasks.go) with a doc comment
   tying it to the M4.20-B rank-3 平级 design the sync layer already
   implements. Cancelled stays excluded (an abandoned row never becomes a
   verdict).
3. Verify briefing rewritten: PASS →
   `gopi task update <id> --status verified --result "evidence: <cmd> + tail"`,
   FAIL → `--status failed --result "reason: <what failed>"`.
4. Stale docs corrected: `devdoc/task-verify-loop.md` §② (was `--summary`-era)
   and the review report's P1 sections carry RESOLVED notes.

### P1#3 — serve daemon dies on the first failed run

**Root cause**: `runTaskServe`'s loop called `runTaskRun(opts)` which called
`os.Exit(1)` on EVERY error path (claim failure, start failure, payload
refusal, child failure) — the first bad row killed the whole dispatcher.

**Fix shipped**: `runTaskRun` split into
- `runTaskRunOnce(opts) error` — returns an error on every failure path,
  never exits the process; the child-failure path writes the row `failed`
  via the existing writebackOutcome then returns the error;
- `runTaskRun(opts)` — the standalone wrapper: logs the error + os.Exit(1)
  (the plain `gopi task run` CLI contract: a failing run IS an error to the
  invoking shell).

The serve daemon loop now calls `runTaskRunOnce` and on error logs
`[serve] run failed (continuing): ...` then moves to the next tick — the row
lands `failed` and the daemon survives. The worker loop path (runTaskRun in
workerRound) keeps its exit-on-error contract unchanged.

### Regression tests shipped
> **FILE NAMES RETIRED 2026-10-30 (task 01a18799, slice 2/4).** The `gopi task verify`
> VERB is gone. `internal/tools/task_ledger_state_test.go` is now
> `internal/tools/task_ledger_state_test.go` and `cmd/agent/cli_tasks_update_test.go` is now
> `cmd/agent/cli_tasks_update_test.go` — the arms below that survive are the state-machine and
> `--result` ones, which belong to the ledger and `update`, not to the retired verb.

- `internal/tools/task_ledger_state_test.go` (was `task_verify_test.go`):
  - `TestTaskVerifyFailVerdictLandsOnCompleted` — FAIL verdict writes a
    completed row failed; the reason rides the result: (task_events) line;
    the immutable summary is untouched.
  - `TestTaskVerifyPeerMoveFailedToCompleted` — failed→completed peer move
    legal (rank-3 平级).
- `cmd/agent/cli_tasks_update_test.go` (was `cli_tasks_verify_test.go`):
  - `TestTaskFlagMemberUpdateResult` — `--result` is an update member;
    `--summary`/`--payload`/`--timeout`/`--tags` are not.
  - `TestParseTasksUpdateResult` — `--result` parses with `--status`;
    `--result` without `--status` refused; `--summary` stays rejected.
- `cmd/agent/cli_tasks_test.go` — the M4.5 `--summary`-rejection test gained
  the `--result` acceptance assertion.
- `cmd/agent/cli_serve_test.go`:
  - `TestRunTaskServe_SurvivesFailedRun` — hermetic child failure (new
    `taskChildFailTestEnv` escape = `GOPI_TASK_CHILD_FAIL_TEST`, the
    GOPI_SKIP_CLAIM_BROADCAST-style test seam): the serve --once pass claims
    the row, the child "fails", the row is written `failed` with the reason
    in result:, and runTaskServe RETURNS (under the old code os.Exit(1)
    killed the whole test binary = hard failure).

## Not Done
- No CLI E2E with a REAL model child (the child would need an API key / live
  model call — deliberately not hermetic; the taskChildFailTestEnv seam
  drives the identical writeback+continue code path).
- P2 review items (stale `list --status failed` count after archive
  filtering, etc.) are out of scope for this P1 fix — untouched.
- `devdoc/m420-rank-model.md` line "verify FAIL 语义 → --status failed" was
  already correct; no edit needed.

## Verification
- Build: `go build ./...` — OK; `go vet ./cmd/agent/ ./internal/tools/ ./internal/config/` — clean.
- Tests (targeted, all PASS):
  - `go test ./internal/tools/ -run 'TestTaskVerify|TestTaskUpdateLifecycle|TestStatusRankTable|TestRankApply|TestBilateral|TestLegacy|TestTaskDispatch|TestTaskBugAdoption'` — ok (27.6s)
  - `go test ./cmd/agent/ -run 'TestTaskFlagMember|TestParseTasksUpdate|TestRunTaskServe|TestWorkerHelp|TestWorkerWriteback|TestTaskDispatch|TestParseTasks'` — ok (11.6s)
  - `go test ./cmd/agent/ -run TestRunTaskServe_SurvivesFailedRun -v` — PASS, exercising: claim → `[serve] run failed (continuing): ... synthetic child failure` → row failed → --once returns
- Probe (removed after run): `completed→failed` previously REFUSED with
  "illegal status transition completed → failed"; after the gate fix the
  FAIL verdict lands.
- Full `go test ./internal/tools/ ./cmd/agent/`: cmd/agent full suite PASS (179.6s); internal/tools full suite hit the KNOWN 600s box-load ceiling (memory #1238: the package takes >20min on this machine — not a regression); the gate-sensitive family rerun `go test ./internal/tools/ -timeout 30m -run 'TestTaskVerify|TestTaskUpdate|TestTaskDispatch|TestStatusRank|TestRankApply|TestBilateral|TestLegacy|TestTaskSubmit|...'` PASS (79.3s). `go test ./internal/agent/ -run 'TestTask|TestVerify|TestResumeTask'` PASS (17.8s).
- Commit: `0d759d0` on `feat/fix-task-p1` (in the main repo via worktree = shared object store).
