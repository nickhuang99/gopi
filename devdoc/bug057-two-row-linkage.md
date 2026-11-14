<!-- doc-class: normative -->
# Task Report: bug057 — 双行结构无关联标注 (two-row linkage)

## Problem

`gopi task submit` + `gopi task run` produces TWO ledger rows for ONE execution:

- **human wrapper row** (`t-1788277288`, source=human) — the enqueued ticket
- **auto mirror row** (session_id as task_id, source=auto) — the child run's
  auto-registration (taskregister.go)

Before this fix the two rows sat side by side with no visible relation —
"chaos or duplicated?" (the operator, 2026-09-02). M3 时代双行结构 (101=wrapper +
102=child) 已知，M4.6 chat 表分流时未处理显示关联。

## Fix (A+B+C)

### A. auto mirror row → linked_to: wrapper task

- **Seam**: `runTaskRun` (cmd/agent/cli_tasks.go) exports
  `GOPI_TASK_WRAPPER_ID=<human-task-id>` on the child's env **BEFORE spawn**
  (`cmd.Env`). Deliberately pre-spawn: the pre-existing
  `GOPI_TASK_CHILD_SESSION` is set POST-exit (writeback seam) — too late for
  registration-time tagging. Rule 2: one writer (runTaskRun), one reader
  (maybeRegisterSessionTask).
- **Hook**: `maybeRegisterSessionTask` (internal/agent/taskregister.go) reads
  the env after `TaskNew` and tags the mirror row `wrapper:<task-id>` via new
  `tools.TaskTagWrapper` (idempotent, best-effort — missing env = plain
  session, skip).
- **Display**: `TaskShow` renders `linked_to: <wrapper> (wrapper task that
  dispatched this run — same execution, two rows)`.

### B. human wrapper row → executor: auto mirror

`taskExecutorTaskID` (internal/tools/tasks.go) resolves the mirror:
- **Primary**: DB reverse lookup — the `source='auto'` row whose tags carry
  `wrapper:<this-task-id>` (comma-boundary patterns, prefix-collision guard:
  `wrapper:t-1788277288` never matches `wrapper:t-17882772881`).
- **Fallback** (pre-bug057 rows, tagged nothing): a `source=human` row's own
  `session_id` (bug054 stamps the child session id into CLI rows; that id IS
  the mirror's task_id). An **llm row is NEVER fallback-resolved** — its
  session_id is the SUBMITTER's session; a false executor is worse than none
  (caught by TestTaskLinkageNoFalseLinkage during dev).

`TaskShow` renders `executor: <mirror> (auto mirror row — same execution, two
rows)` only when a mirror actually exists.

### C. task list [dup-of t-*] marker

`TaskList` renders `[dup-of <wrapper>]` on any row carrying the
`wrapper:<task-id>` tag — the mirror row announces its wrapper at a glance;
rows without the tag stay unmarked.

## Files

| File | Change |
|---|---|
| internal/tools/tasks.go | `TaskWrapperEnv` const, `taskTagWrapperPrefix`, `TaskTagWrapper`, `taskWrapperTagValue`, `taskExecutorTaskID`, TaskShow `linked_to:`/`executor:`, TaskList `[dup-of]` |
| internal/agent/taskregister.go | read env → tag mirror row (best-effort) |
| cmd/agent/cli_tasks.go | pre-spawn env export in runTaskRun |
| internal/tools/tasks_test.go | 4 new tests |
| internal/agent/taskregister_test.go | 1 new test |
| cmd/agent/cli_tasks_test.go | 1 new E2E test |

## Tests (D)

1. **TestTaskLinkageShowBothDirections** — seeds the two-row shape, asserts
   `linked_to:` on the mirror + `executor:` on the wrapper.
2. **TestTaskLinkageNoFalseLinkage** — plain auto session links nothing; an
   llm row with submitter session_id + no mirror fabricates no executor.
3. **TestTaskLinkageListDupOfMarker** — list renders `[dup-of t-*]`.
4. **TestTaskLinkageExecutorReverseLookup** — reverse lookup wins over the
   session_id fallback even with a later untagged auto row present.
5. **TestAutoTaskRegisterWrapperEnvTag** — env set → mirror tagged; env
   absent → nothing.
6. **TestTaskRunTwoRowLinkageE2E** — real binary + mock LLM in sandboxed
   GOPI_HOME/GOPI_MEMORY_DB: submit → run → show BOTH rows assert mutual
   linkage + list marker. This is requirement D exactly.

## Live-fire (manual, real API)

```
$ gopi task show t-e2e-057          # human wrapper row
executor:       998b8258-f301-4bdf-9bd7-f9f7e95701c1 (auto mirror row — same execution, two rows)

$ gopi task show 998b8258-...       # auto mirror row
tags:           wrapper:t-e2e-057
linked_to:      t-e2e-057 (wrapper task that dispatched this run — same execution, two rows)

$ gopi task list
#998b8258-...  [in_progress]  [auto] do the bug057 linkage work  ... [dup-of t-e2e-057]
```

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- gofmt: clean (all 6 files)
- go vet: clean (affected packages)
- Targeted: `TestTaskLinkage*` 4/4, `TestAutoTaskRegisterWrapperEnvTag` 1/1,
  `TestTaskRunTwoRowLinkageE2E` 1/1 — all PASS
- Full suites: `go test ./internal/agent/ ./cmd/agent/` (job-1) — see output

## Not Done

- 远程 dispatch (serve → ssh → remote `task run`) 未单独 E2E — remote runs
  through the SAME runTaskRun code path, behavior identical.
- worker 循环 (runTaskWorker → runTaskRun) shares the path automatically.
- `task verify`'s verifier-child is a self-built short session — no wrapper
  row exists by design; no linkage needed.
- 新列方案 (taskregister 直接写 wrapper 列) 未采用 — tags 零 schema 变更,
  FTS 可搜索, 与 resumed:/resolved: 同一机制.

## Commit

- Branch: feat/bug057-two-row-linkage
- Commit: 8ad11ef (feat(task): two-row linkage annotation — wrapper/executor/dup-of)
