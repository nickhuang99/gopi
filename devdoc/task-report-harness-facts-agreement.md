<!-- doc-class: normative -->
# devdoc: the harness skeleton's facts now agree with the ledger

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date**: 2026-10-12 · **Task**: `01a12e0dd700705397bf98b9cc261824` (host-b)
**Motivating row**: `01a12de6-c052-7346-9efc-bbd885387489` (completed, delivery
merged, branch `task/01a12de6c05273469efcbbd885387489` @ `b8c566ea2`)

## 1. The defect, measured

Row `01a12de6-c052` finished a real delivery: branch
`task/01a12de6c05273469efcbbd885387489` at `b8c566ea2`, 12 evidence files under
its session `artifacts/`, a full narrative at the tail of `output.log`. Its
session `task-report.md` was the harness **skeleton** (1140 bytes, 4× `not
recorded`), and its own `## Harness facts` block contradicted the ledger:

```
$ sed -n '/## Harness facts/,$p' /home/user/.gopi/sessions/01a12de6-c052-.../task-report.md
- task_id: 01a12de6-c052-7346-9efc-bbd885387489
- status: in_progress          ← the row reads completed
- session_id: f58a7e0f-1fc9-4bc1-8177-979872483535
- branch: (unknown)            ← the row records task/01a12de6c05273469efcbbd885387489
- commit: (unknown)            ← the ref was reachable the whole time
- created_at: 2026-10-12T02:17:39Z

$ gopi task show 01a12de6-c052-7346-9efc-bbd885387489   # (ledger, same moment)
status:         completed
deliverable.branch: task/01a12de6c05273469efcbbd885387489
```

A census of the host's session tree (the same day) shows this is not one row:

```
$ cd /home/user/.gopi/sessions && for f in */task-report.md; do \
    grep -q "not recorded — the model wrote no report" "$f" && echo "$f"; done | wc -l
16                      # 16 skeletons among 151 reports
$ # of the 14 post-seam skeletons, every single one carried:
- status: in_progress - branch: (unknown) - commit: (unknown)   # 14/14
```

(The two `t-acc-*` skeletons are the harness's own acceptance fixtures and did
resolve a branch/commit — they are the shape that proved the *fallback* channel
works when the child-session env is present.)

## 2. Root cause — two causes, hidden behind one symptom

Both are structural: neither depends on the model, the task, or the host.

### ② the seal runs before the outcome is written (status)

`cmd/agent/cli_tasks.go`:

```
3499  func writebackOutcome(taskID, status, resultSummary, reportTo, repo string) error {
...
3531      if line, sealErr := tools.SealTaskReport(taskID); sealErr != nil {   ← the seal
...
3562      _, wbErr = tools.TaskRunWriteback(taskID, status, resultSummary)       ← the write that lands `status`
```

The seal is the FIRST thing `writebackOutcome` does and the terminal write is
the LAST, so `taskRowFacts` could only ever read the row the writeback had **not
yet written**: `in_progress`, always. The harness's fallback report was
guaranteed to state a status its own ledger would replace one call later.

The seal does not need the row for this: it is invoked *by* the close-out that
knows the outcome — `status` is its caller's own argument.

### ③ branch/commit resolved from the child-session ENV alone (branch, commit)

`taskReportFacts` called `taskWritebackBranch()` and `taskReportHeadCommit()`,
both of which begin at `os.Getenv(TaskChildSessionEnv)` /
`TaskExecutorSessionEnv` and return `""` when neither is set:

```
$ grep -n "func taskReportHeadCommit" -A 12 internal/tools/task_report.go   # pre-fix
	sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv))
	if sid == "" { sid = strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv)) }
	if sid == "" { return "" }            ← "(unknown)", whatever the row says
```

The row, meanwhile, was holding both facts: the claim-time stamp and the
completion-time backfill put `branch`/`worktree`/`repo` in
`deliverable` **before** the seal runs. Measured (this task's own baseline
worktree, `internal/tools/zz_redproof_reportfacts_test.go` copied onto
`dca292f88`, no env set, row's deliverable stamped with
`feat/redproof-report-facts` @ its tip):

```
- status: pending
- branch: (unknown)          ← the row records feat/redproof-report-facts
- commit: (unknown)          ← the ref resolves to the tip
--- FAIL: TestZZRedProofSkeletonFactsAgreeWithTheLedger
```

So the harness's own fallback report could **never** agree with the ledger it
was describing — not on status (always one write behind), not on branch/commit
(env-only, while the answer sat in the row).

## 3. The fix

`internal/tools/task_report.go`:

- **`SealTaskReportOutcome(taskID, outcome)`** is the door every close-out
  takes; `SealTaskReport(taskID)` remains as its no-outcome form (tests, and any
  caller that genuinely has no terminal outcome to declare);
  `taskReportFacts(taskID, outcome)` lets a declared outcome **win** over the row
  snapshot, falling back to the row's own status when none is declared.
- **`taskReportBranch(taskID)`** reads the row's `deliverable.branch` first and
  the child-session resolution second — both real channels, ledger first,
  because the ledger's answer survives a pruned session directory and a moved
  worktree.
- **`taskReportHeadCommit(taskID, branch)`** walks three directories in order —
  the child session's launch workdir, the row's recorded `deliverable.worktree`,
  the row's `deliverable.repo` — reading `refs/heads/<branch>` when a branch is
  known and the directory's own `HEAD` only when it is not. The shared repo is
  the rung that answers for a delivered-and-cleaned-up run; reading it as a
  **ref** (never as the repo's HEAD, which is usually master) is what keeps the
  answer from being confidently wrong.

`cmd/agent/cli_tasks.go:3531` now calls `SealTaskReportOutcome(taskID, status)`.

**The same defect, third axis (`session_id`)** — found by running the fix live
(row `01a12e32-f80c`, first close-out of the fixed binary): the skeleton still
printed `session_id: (unknown)` because the row's `session_id` is stamped by the
SAME writeback the seal precedes, while `gopi task show` named that session's
JSONL one line later. `taskReportSessionID()` reads the two env seams the commit
resolver already uses; the row remains the first source.

**What did NOT change**: the seal still never clobbers a model-authored report,
still never writes the ledger, and is still best-effort (a seal failure is
logged and the terminal write proceeds).

## 4. Prevention arm — the two report paths the model was told to use

The run `01a12de6-c052` followed PROJECT.md Rule 5 literally — it wrote
`tasks/01a12de6-c052-7346-9efc-bbd885387489-report.md` **into the branch**
(measured in its session JSONL: 6 `write`/`edit` calls against that path) — and
satisfied neither reader: the harness looks at `$GOPI_TASK_REPORT_PATH`, and a
report committed on a branch is invisible to `task show`. GOPI.md's
`Report location:` line now names the env var FIRST and keeps the in-tree
locations as the fallback; the agent prompt (`internal/agent/loop.go`
task rule 5) already names the env var and was not touched.

## 5. Regression guard

- `internal/tools/task_report_facts_test.go` — four permanent tests: the
  terminal outcome wins over the pre-terminal row snapshot; the ledger branch
  and its ref tip are stated; the run's own session id is stated when the row
  does not carry one yet; the commit follows the recorded **branch ref** and
  never the repo's own HEAD (the worktree is removed to force the shared-repo
  rung).
- `internal/tools/zz_redproof_reportfacts_test.go` — the A/B proof, written
  against the pre-fix API only, so the same file compiles on both trees: red on
  the baseline (`branch: (unknown)` beside a recorded branch), green after.
- `cmd/agent/cli_tasks_reportfacts_seam_test.go` — the same property through the
  REAL `gopi worker` close-out (`writebackOutcome` → `SealTaskReportOutcome`),
  asserting the skeleton's `status`/`branch` against the row `task show` renders.
