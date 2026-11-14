<!-- doc-class: historical -->
# Task Report: merge feat/task-verify-loop (P2.5) → master

## Completed

Merge of P2.5 verify-loop (576862f + 4b06759) into master @ c16fe41 (merge-base
fa8368d). Conflicted files: exactly the 4 the epic predicted. Resolution
principle: **HEAD 侧 (master M4.5/M4.6/M4.7) 演进全保留，P2.5 是增量叠加**.

### internal/tools/tasks.go — 8 conflicts, all additive overlays

| # | Conflict | Resolution |
|---|----------|------------|
| 1 | `Task` struct: master +liveness fields (TasklistIssuedAt/HasTasklistIssue/LastToolcallAt/HasToolcall) vs branch +VerifyEvidence | Both: 16-col base + VerifyEvidence + 4 liveness fields |
| 2 | `taskSelectColumns`: master 17-col (liveness tail) vs branch 16-col (verify_evidence before created_at) | verify_evidence, created_at, updated_at, ended_at, tasklist_issued_at, last_toolcall_at — ONE order, both sides |
| 3/7 | `scanTask` + `scanTaskRow` Scan arg lists | Scan order mirrors #2: claimed_at → **verify_evidence** → created_at → updated_at → ended_at → liveness. (scanTaskRow is the easy-to-miss twin — memory #1116/#1117 warned; both patched) |
| 4 | `tasksP2Columns` ALTER list | Both ALTERs: liveness pair (M4.5) + verify_evidence (P2.5) |
| 5/6 | rebuild copy SELECT column list + colExpr tail | Both columns in the INSERT list; colExpr fallbacks for tasklist_issued_at/last_toolcall_at AND verify_evidence ('') — legacy tables lacking either get defaults |
| 8 | TaskShow: master +submitted_by+briefing comment vs branch +verify_evidence block | Both: submitted_by line, then verify_evidence (when non-empty), then briefing comment |

Auto-merged (no manual touch): taskStatusSet 8-value, taskSchemaStatusCHECK()
(rendered from the slice — CHECK DDL extends automatically),
taskAllowedTransitions (completed/failed→verified|reopened, verified→
in_progress|reopened, reopened→in_progress), terminalTaskStatus +verified,
tasksNeedsRebuild +`'verified'` probe, hasBugTag tally, TaskVerify/
TaskVerifyToReopen engines.

### cmd/agent/cli_tasks.go — 6 conflicts, additive

- imports: +path/filepath (master serve-lock needs it)
- usage text: verify/reopen paragraphs inserted; serve line = master's
  [--nodes JSON] [--nodes-only] version
- tasksOptions: master's skipTags/listen/M4.7 fields + branch's
  verifyTimeout/reason
- parseTasksArgs: master serve(--skip-tag/--nodes/--nodes-only) + ingest case
  KEPT; branch verify/reopen cases ADDED; default verb list = union
  (…archive|ingest|verify|reopen)
- taskFlagMember: master's archive case + branch's verify/reopen cases (new/next stay removed)
- big block (627-768): master's acquireServeLock/serveLockHolder/
  runTaskIngest/taskRemovedAdvice/staleSweep AND branch's
  taskVerifyTimeoutDefault/runTaskVerify/runTaskReopen — both blocks intact
- dispatch switch auto-merged with verify/reopen cases already wired

### internal/config/help.go — 1 conflict

HEAD's full task section (run/serve --skip-tag/ingest/update-immutability)
+ branch's verify paragraph appended; branch's stale next/next-claim/
[--summary "Y"] lines dropped (master removed those verbs/flags in M4.5 ④).

### internal/tools/task_dispatch_test.go — 1 conflict (the real semantic trap)

Branch had updated this test against the OLD 3-arg TaskUpdate(id, status,
summary) signature; master's M4.5 shrank the surface to 2-arg TaskUpdate +
harness-only TaskUpdateWithResult. Resolution: keep master's signature, keep
branch's improved assertion (pending→verified refused as an ILLEGAL
TRANSITION, not as an invalid status — P2.5 made verified valid, so the old
P2-era "invalid status" assertion had to die):

```go
TaskUpdate("t-sm", "verified")  // 2-arg; error contains "illegal status transition pending → verified"
```

Plus `task_verify_test.go` (new file from branch, 319 lines): all 15
status+summary 3-arg calls mapped to `TaskUpdateWithResult` (they carry
verdict summaries — exactly the harness-only writeback semantics), 4
status-only calls stay `TaskUpdate`. Sed'd; grep-verified zero stragglers.

## Not Done

- E2E verify loop (fresh child instance runs a real task) — covered by
  TestTaskVerify* unit/E2E-lite tests in-package; full black-box E2E was
  proven on the branch (devdoc/task-verify-loop.md) and the merge changes no
  engine logic, only integrates surfaces.
- Worktree flow per Rule 6: epic explicitly overrides — conflict resolution
  done in the main repo (merge commit lands on master directly).

## Verification

- Build: `go build ./...` OK (after restoring runTaskReopen's closing brace,
  lost to a sed line-shift — caught by compiler)
- Vet: `go vet ./...` OK
- Tests: `go test ./...` full suite — see below
- CLI faces: `gopi task verify -h` and `gopi task reopen -h` render their
  usage paragraphs (binary /tmp/gopi-p25, CGO_ENABLED=0)
- Commit: see `git log --oneline -1`

## Merge-decision rationale (devdoc Rule 7)

- The 8-value state machine arrived intact via auto-merge because the P2.5
  branch had DESIGNED its additions as slice/map entries
  (taskStatusSet/taskAllowedTransitions) — the single-source-of-truth
  pattern turned a 4-way conflict zone into zero manual edits. The CHECK DDL
  and rebuild probe follow the slice automatically.
- The conflict surface was almost entirely COLUMN-shaped (struct/SELECT/
  Scan/ALTER/copy-SELECT): two features both appending columns to the same
  table. Positional contracts (Scan order) were the only real risk —
  resolved by picking one canonical order (claimed_at, verify_evidence,
  created/updated/ended, liveness) and threading it through all five sites.
- task_dispatch_test.go was the only place where the two sides' semantics
  truly collided (signature shrink vs new-state assertion) — resolved
  master-side signature + branch-side assertion.
