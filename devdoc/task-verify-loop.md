<!-- doc-class: normative -->
# Task Report: pa-q-p25-verify-loop — Task State Machine verified/reopened + `gopi task verify` CLI + Bug-Flow Adoption Tally

Branch: `feat/task-verify-loop` (worktree `~/workspace/gopi-p25-verify-loop`, base fa8368d)

## Completed

### ① verified / reopened in the state machine (internal/tools/tasks.go)
- `taskStatusVerified` / `taskStatusReopened` constants + exported aliases;
  `taskStatusSet` now 8 values → the SQL CHECK literal and the rebuild
  migration extend automatically (single source of truth, Rule 2).
- Transition table:
  - `completed|failed → verified|reopened` — the verifier judges DELIVERED
    artifacts only.
  - `verified → in_progress|reopened` (overturn), `reopened → in_progress`
    (rework picks the row up).
  - `pending|in_progress → verified` deliberately ILLEGAL (tested).
- `terminalTaskStatus` now includes `verified` (stamps ended_at);
  `reopened` does not (the re-enqueued row carries the timeline).
- Legacy migration: `tasksNeedsRebuild` also triggers when the live CHECK
  lacks `verified` — pre-P2.5 DBs are healed in place by the existing
  rename-recreate-copy-drop rebuild (data intact; tested).

### ② `gopi task verify <task_id> [--timeout N] [--db PATH]`
- Parser (verify verb: positional task_id, `--timeout N` non-negative int,
  strict per-verb flag whitelist), dispatch, usage text (tasksUsage +
  internal/config/help.go) — the full CLI family pattern.
- Engine `TaskVerify(ctx, TaskVerifyOptions{TaskID, Timeout})`: builds a
  black-box briefing (task id, summary, payload/acceptance criteria, exact
  writeback commands), runs a FRESH gopi child (`os.Executable`, PATH
  prepended with the build dir — see E2E finding), then re-reads the row:
  the child's verdict lands in the SAME append-only ledger. Missing task /
  bad timeout → `task verify: invalid: …`; child crash → FAIL prefix with
  stderr tail (400B cap); no verdict → explicit error.
- Verdict writeback (bugfix-task-p1, 2026-09-02 — was `--summary`-era, dead
  since M4.5): the briefing tells the child
  `gopi task update <id> --status verified --result "evidence: <cmd> + tail"`
  (PASS) or `--status failed --result "reason: <what failed>"` (FAIL). The
  `--result` digest rides the append-only task_events trail via
  TaskUpdateWithResult — `task show` renders it as the `result:` line; the
  summary column stays immutable (bug056). A FAIL verdict on a COMPLETED row
  is a legal terminal-tier peer move (completed↔failed, M4.20-B rank 3
  平级) — the judged row stays terminal and rework is reissued.
- **RETIRED (2026-10-30): both the `TaskVerifyToReopen` wrapper and the
  `gopi task reopen` verb are DELETED.** What replaces them, so a reader knows
  where the capability went: a failed or judged row is CONTINUED with
  `gopi session resume <id>` (same row, same session, same directory — LEVEL 1)
  or COPIED into a clean room with `gopi task clone <id>` (`<id>-c1`, new
  session, new directory — LEVEL 2). The copy mechanics the verb owned — the
  repo axis, the carried attachments manifest, the WIP-pointer in the payload —
  are `taskCloneDeliverable` / `taskSuccessorCarryAttachments` /
  `taskSuccessorWIPAxis`, reached by the clone lane. History, for the reader who
  finds an old report: the verb existed, was deleted by 83039f1, RESTORED on
  2026-10-21 with the attachments fix (71ea4f7), and then removed for good
  because a name shaped like "continue the failure" that minted a whole new
  ground sent operators to the wrong door.
- `TaskShow` now renders `verify_evidence` (the column existed in the schema
  and SELECT but was never rendered — completing the intended surface).

### ③ Bug-flow adoption check (Rule 11)
- Every bug-tagged `task_submit` (tool AND `gopi task submit --tags bug` —
  same engine) echoes a one-line running tally:
  `[bug-flow] tasks with tag bug: N (verified V, reopened R, other O)`.
- `hasBugTag` exact comma-word match ("debug"/"bugflow" do NOT trigger).
- task_submit tool description gains the tally sentence (routing guidance);
  the tally line is measurable in the session JSONL → adoption-rate check.

### Root-cause bug fix found by the E2E run (Rule 5 regression guard)
- `scanTaskRow` (task show / update / claim paths) scanned 15 columns while
  `taskSelectColumns` selects 16 (`verify_evidence` was added to schema+list
  path on master but not here) → EVERY `gopi task show|update` on a rebuilt
  DB failed with `expected 16 destination arguments in Scan, not 15`.
  Latent on master since 3612bfa (verify_evidence columns), exposed the
  moment my P2.5 tests touched the row readers. Fixed scan order, rendered
  verify_evidence in TaskShow; all row readers now agree.

## Not Done (honest)
- `task verify -` (claim-oldest-pending and verify) — verify requires an
  explicit task id; queue-addressing mode deferred (dispatcher naming is
  still settling).
- `reopened` rows are NOT auto-claimed by `task next` (they are judgment
  terminals; rework flows through the re-enqueued `-r1` pending row).
- verify_evidence column backfill for pre-existing rows: not needed
  (append-only rows keep '' → rendered as absent).

## Verification
> **FILE NAMES RETIRED 2026-10-30 (task 01a18799, slice 2/4).** The `gopi task verify`
> VERB was retired, and two files named below moved with it: `internal/tools/task_ledger_state_test.go`
> became `internal/tools/task_ledger_state_test.go` (the state-machine arms it carries are
> still live) and `cmd/agent/cli_tasks_update_test.go` became
> `cmd/agent/cli_tasks_update_test.go` (the `--payload`/flag arms it carries belong to
> `update`). The verb's own arms — the LLM child spawn, `ErrNoVerdict`, the 0/4/5 exit
> mapping — are GONE with it; the verdict is now written by the run that measured it
> through `gopi task update --status verified --result "evidence: …"`. Read every
> `task verify` reference below as the historical surface.

- Build: `go build ./...` OK; `go vet` clean on touched packages.
- Tests (new, all PASS):
  - `internal/tools/task_ledger_state_test.go`: TestTaskVerifyLoopStateMachine,
    TestTaskBugAdoptionStatsAndTallyEcho, TestTaskVerifyToReopenEnqueuesReworkRow,
    TestEnsureTasksSchemaRebuildsPreP25CHECK.
  - `cmd/agent/cli_tasks_update_test.go`: TestParseTasksArgsVerify,
    TestTaskFlagMemberVerify.
  - Updated `TestTaskStateMachineRejectsIllegalTransitions` (pending→verified
    now illegal-by-table instead of invalid-enum).
  - Pre-existing agent-package failures (TestAutoTaskRegisterFailedStatus,
    TestChatTaskRegistration_WrapperFailedStatus) FAIL identically with my
    changes stashed on the base tree — pre-existing, not from this branch.
- E2E (Rule 11), demo task t-p25-demo in /tmp/p25-demo.db over a real
  sandbox module (/tmp/p25-sandbox, two seeded regressions + their tests):
  1. `./bin/gopi task verify t-p25-demo` → fresh gopi child session; child
     ran `cd /tmp/p25-sandbox && go test -count=1 -v ./...` (both PASS) and
     attempted the verified writeback. Run 1 EXPOSED a real integration bug:
     bare `gopi` in the child's bash resolved to the stale system binary,
     which rejected `--status verified` and the child fell back to
     `completed` → root-caused (child PATH) → fixed (prepend exe dir to
     child PATH) → run 2: row status=verified, evidence stamped:
     `evidence: cd /tmp/p25-sandbox && go test -count=1 -v ./... → --- PASS:
     TestAdd / --- PASS: TestAvgEmpty / ok p25sandbox 0.002s`.
     Child session JSONL: ~/.gopi/sessions/75416118-… (writeback cmds) and
     608e3c09-… (run-1 fallback).
  2. `./bin/gopi task reopen t-p25-demo --reason "E2E: …"` →  *(historic run: that verb was deleted 2026-10-21)*
     `Task re-enqueued: t-p25-demo-r1 (status=pending, parent=t-p25-demo,
     tags=bug)` — the loop closes: judged row keeps verified, rework row is
     pending with the reason in its payload.
  3. Tally echo on the real binary: `gopi task submit --tags bug …` →
     `[bug-flow] tasks with tag bug: 1 (verified 0, reopened 0, other 1)`.
- gofmt: only pre-existing flags remain (internal/agent/loop.go, edit.go,
  write.go, tasks_test.go, config*.go — all flagged on the untouched base).

## Deliverable declaration (Rule 6d)
- Branch: `feat/task-verify-loop` (worktree-shared with the main repo —
  commits land on the branch directly, no push needed).
- Recovery: `git -C ~/work/GoPi show feat/task-verify-loop:devdoc/task-verify-loop.md`
  once committed; worktree `~/workspace/gopi-p25-verify-loop` until PA merges.
