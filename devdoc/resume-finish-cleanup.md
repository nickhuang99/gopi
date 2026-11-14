<!-- doc-class: historical -->
# `gopi resume` must run the finish-cleanup chain (epic `01a0a57c`)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a0a57c` (dispatched by the night-shift session from
`devdoc/nightshift-cgroup-worktree-resume-20260915.md` §三)
**Branch**: `feat/resume-finish-cleanup`
**Worktree**: `/home/user/workspace/gopi-resume-cleanup`
**Commits**: `d6d8e00b` (fix + one-shot verb), `b58dcb49` (red-proven acceptance)

---

## 1. The defect, exactly

The close-out chain (`tools.TaskFinishCleanupSessionID`) had exactly **one**
in-process trigger: the executor child's end hook, `finishSessionTask` in
`internal/agent/taskregister.go`. That hook only ever reaches the chain for a
row it LATCHED ONTO, and the latch is **wrapper identity**:

```go
// internal/agent/taskregister.go (maybeRegisterSessionTask)
if wrapperID := strings.TrimSpace(os.Getenv(tools.TaskWrapperEnv)); wrapperID != "" {
    a.autoTaskRowID = wrapperID          // ← the only path that reaches the chain
    ...
    return
}
```

`tools.TaskWrapperEnv` is `GOPI_TASK_WRAPPER_ID`, exported pre-spawn by
`gopi task run` — and by nothing else. **`gopi resume` sets nothing of the
kind** (its `--task` linkage is a bare ledger tag, `tools.TaskTagResumed`), so
`a.autoTaskRowID` stayed empty, `finishSessionTask` returned at its
`!a.autoTaskRegistered` guard, and the chain never ran for the row resume had
just continued.

The affected function carried this comment, which is *true of a `task run`
child and false of a resume*:

```go
// cmd/agent/cli_resume.go (before this change)
// Producer obligation, success half: the resumed session may have
// COMPLETED the task whose worktree this process is standing in — its own
// end hook runs the finish-cleanup chain, which can remove that very
// directory.
worktreerelease.ReleaseReporting(os.Stderr, resumeProgName)
fmt.Print(result)
return 0
```

(the same sentence is quoted in `devdoc/nightshift-cgroup-worktree-resume-20260915.md:130`)

**Observable cost** (measured by the night-shift session, re-measured here):
`gopi resume` on a row whose deliverable named a worktree left that worktree
registered and on disk forever, and the ledger row was never touched by the
resume. The operator's examples were `gopi-textguard`, `gopi-brew-fix`,
`gopi-web-ip` (all three already gone by the time of this task — the
night-shift session committed their work and removed the directories by hand).

**Second, structural half of the defect**: every other trigger of the chain is
a LONG-RUNNING process (`serveFinishCleanupSweep` = the worker/merger lifecycle
tick). This host runs **no** serve and **no** merger, so the rows a killed run
left behind were re-visited by nobody — the worktree pile-up had no repair
path at all without starting a second resident service.

---

## 2. The fix

### 2a. The missing call site (`cmd/agent/cli_resume.go`)

One call to the existing chain, for the row the resume continued, on the
SUCCESS path — after the CWD release, never before it:

```go
func resumeFinishTail(result, taskID string) {
    resumeWorktreeRelease(os.Stderr, resumeProgName)  // ① release the cwd
    runResumeFinishCleanup(taskID)                    // ② close out the row
    fmt.Print(result)                                 // ③ the session's payload
}
```

* **One implementation, no second mechanism** — it is
  `tools.TaskFinishCleanupSessionID`, the very call the end hook makes.
  No new tool (PROJECT.md Rule 3).
* **Order is a producer obligation, not a preference**: the chain's job
  includes `git worktree remove` of the directory this process is standing in
  (`resumeChdirToWorktree` chdir'd into it). A process still inside it pins the
  inode — which is exactly what the chain's own liveness guard reports from the
  outside. Releasing first is what lets the guard stay unweakened.
* **Every outcome is reported, none is fatal**: performed, refused-with-reason,
  or errored — the refusal is what an operator needs (`FinishCleanupReportLine`
  is the one renderer both faces share). The resumed session's SUCCESS stays the
  process's verdict; a cleanup failure never rewrites it. The call is also made
  on the *failure* half's neighbourhood: the `resumeWorktreeRelease` seam covers
  both halves, the chain call is success-only (a session that failed mid-run has
  not finished the work the chain would hand off).

### 2b. A daemon-independent one-shot face (`cmd/agent/cli_tasks.go`)

```
gopi task finish-cleanup [--dry-run] [--apply] [--task T] [--json] [--db PATH]
```

**Why a verb and not "start a worker"** (the choice the epic asked to state):
the scan set lives in a lifecycle pass of a long-running process. Requiring the
operator to start a worker to recover ONE stale row makes the recovery depend on
a second resident service, and gopi's standing constraint is that no new daemon
is the answer. The `gopi task sweep-worktrees` precedent is exactly this shape
(one-shot verb, plan-by-default, `--apply` to act), so the same work is exposed
the same way.

**Shared scan — the anti-drift part.** The two scans (newest-N cleanable rows +
the STRANDED rows whose recorded worktree is still on disk) were inline in
`serveFinishCleanupSweep`. They are now ONE helper:

```go
func serveFinishCleanupCandidateIDs(ctx, db) (ids []string, errs []error)
```

called by both the resident pass and the verb — so the daemon and the operator
can never disagree about WHICH rows are candidates (a second copy would drift
exactly where it hurts: the operator could not reproduce what the daemon does).
A source-shape test (`TestFinishCleanupVerbSharesTheSweepScan`) fails if the
verb ever grows its own `db.QueryContext`.

### 2c. Plan mode inside the ONE chain (`internal/tools/task_finish_cleanup.go`)

`TaskFinishCleanupPlan(taskID)` is the SAME chain with `planOnly=true`:
**every guard runs** (terminal status, deliverable, ownership, dirtiness,
liveness, ledger liveness) and **every action is predicted** instead of
performed. Implemented as one function with a mode flag, not a parallel
implementation:

```go
func TaskFinishCleanupSessionID(taskID string) (*FinishCleanupReport, error) { return taskFinishCleanup(taskID, false) }
func TaskFinishCleanupPlan(taskID string) (*FinishCleanupReport, error)      { return taskFinishCleanup(taskID, true)  }
```

Every predicted step carries the prefix `finishCleanupPlanMarker = "would "`, so
a reader can tell a prediction from an execution without knowing which mode
produced the report, and `FinishCleanupReportRemovedWorktree` reuses that
marker to distinguish "the chain ran" from "the chain freed the directory".

---

## 3. Verification

### 3.1 Build

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # OK
$ go vet ./cmd/agent/ ./internal/tools/ ./internal/taskverbs/   # clean
$ gofmt -l (my files)                                  # clean
```

### 3.2 The red proof (the test fails without the fix)

```
# with the call site REMOVED from resumeFinishTail:
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   # rebuilt, call site gone
$ go test -run TestResumeReclaimsTheWorktreeItFinishedIn ./cmd/agent/
--- FAIL: TestResumeReclaimsTheWorktreeItFinishedIn (0.13s)
FAIL

# restored:
$ go test -run 'TestResumeReclaimsTheWorktreeItFinishedIn|TestResumeKeepsADirtyWorktree' ./cmd/agent/
--- PASS: TestResumeReclaimsTheWorktreeItFinishedIn (0.30s)
--- PASS: TestResumeKeepsADirtyWorktree (0.13s)
```

Both runs used `internal/testutil.GopiBin`, i.e. the real `bin/gopi` — the
red run required an explicit rebuild, which is how the proof was validated
(the first attempt passed against a STALE binary; that trap is why the rebuild
is spelled out here).

### 3.3 The operator's experiment, live (real binary, sandboxed ledger)

Fixture built off the `/` partition (`/dev/shm/...`, device 28 vs `/` device
2051 — Rule 22), a failed row whose deliverable names a registered worktree:

```
### BEFORE
$ git -C repo worktree list
/dev/shm/.../live-accept/repo      80f177f [master]
/dev/shm/.../live-accept/wt-stale  7fc0a9b [feat/stale-holder]

### DRY RUN (default posture — the verb with no --apply)
$ gopi task finish-cleanup --task 01a0a6bd-…
  01a0a6bd-… failed  branch=feat/stale-holder worktree=/dev/shm/.../wt-stale
      — would run; worktree: would run: git worktree remove /dev/shm/.../wt-stale
finish-cleanup (dry-run): candidates=1 would-run=1 refused=0 — pass --apply to run the chain
$ git -C repo worktree list          # UNCHANGED — nothing was touched
$ git rev-parse --verify feat/stale-holder   # local branch PRESENT

### APPLY
$ gopi task finish-cleanup --task 01a0a6bd-… --apply
01a0a6bd-… branch=feat/stale-holder done=[worktree]
finish-cleanup (APPLY): candidates=1 removed=1 refused=0

### AFTER
$ git -C repo worktree list
/dev/shm/.../live-accept/repo  80f177f [master]
$ ls -d wt-stale
ls: cannot access 'wt-stale': No such file or directory     ← RECLAIMED
$ git rev-parse --verify --quiet feat/stale-holder && echo PRESENT
local branch PRESENT                                        ← branch preserved (merger input)
```

### 3.4 The refusal (guards unchanged)

```
$ echo uncommitted > wt-dirty/wip.txt
$ gopi task finish-cleanup --task 01a0a6bd-…3791 --apply
01a0a6bd-…3791 skipped: worktree dirty (live bond protected) — ?? wip.txt
finish-cleanup (APPLY): candidates=1 removed=0 refused=1

$ ls -d wt-dirty && cat wt-dirty/wip.txt
wt-dirty
uncommitted                                                 ← untouched, with its reason
```

The `--apply` runs took an isolated `GOPI_HOME` on purpose: the host's live
merger holds `~/.gopi/serve.lock`, and the verb correctly refused to double-serve
without one (`another merge daemon is already running … refusing double-serve`)
— the lock is taken only on the APPLY arm, so a plan run still works on a busy
host.

---

## 4. The live acceptance exposed a SECOND defect (found while verifying, fixed here)

The dry run against the host's REAL ledger is what turned a green-looking report
into a defect: **`refused=10  would-run=0`, and the reason on six of the ten rows
was `git status: (no output)`.** Both halves are wrong, and both came from the
same habit — the empty-output sentinel being treated as data.

**4.1 The sentinel reported as a CAUSE (Rule 15).** `finishCleanupFirstLine`
turns "git printed nothing" into `"(no output)"`, because for a SUCCESSFUL probe
that means *clean* (empty porcelain, empty tag list). But the callers report the
same string as the reason when the probe FAILED:

```go
dirty, err := finishCleanupGit(ctx, wt, "status", "--porcelain")
if err != nil { rep.SkipReason = "git status: " + dirty; ... }   // "…: (no output)"
```

`exec.Cmd` fails at `chdir` when the directory is gone and writes **zero** bytes
to `CombinedOutput`, so the diagnostic the operator needed lived only in `err`
(measured with a 12-line probe: `err="chdir …: no such file or directory"`,
`len(out)=0`). The single line that should say *the worktree directory is gone*
said *no output*.

Fix: `finishCleanupDiagnosticLine(out, err)` — on failure with no output, the
error's own text IS the diagnostic; `(no output)` is now a named constant
(`finishCleanupNoOutput`) and can no longer be mistaken for a finding.

**4.2 The sentinel used as a MATCH PATTERN (a guard defeated by a filename).**
The dirty gate read its own sentinel by substring:

```go
if strings.TrimSpace(dirty) != "" && !strings.Contains(dirty, "no output") { … }
```

`git status --porcelain` prints `?? no output` for an untracked file with that
name — legal on disk — and the gate would have called the worktree CLEAN and
pulled it out from under a live bond. Fix: `finishCleanupProbeClean(probe)`
compares the sentinel EXACTLY and returns the real dirt for the reason string.
Regression: `TestProbeCleanIsNotASubstringMatch` (three sentinel-carrying
probes), `TestFinishCleanupDirtyFileNamedNoOutput` (the same on disk, end to
end).

**4.3 Why "refused" was the wrong word.** With the cause visible, six rows read
`chdir <dir>: no such file or directory` — the directory was already gone. That
is not a refusal: there is **nothing left to do** for that row. Yet the verb
counted every skip as `refused`, so a CLEAN host printed `refused=10
would-run=0` and read as a stuck sweep — exactly the "an operator cannot tell a
no-op sweep from a stuck one" failure the reportable-marker work (2026-09-12)
set out to end. The genuine refusals (an exempt branch, a live holder, a dirty
bond) were buried among harmless ones.

Fix: a third disposition, `Settled` (`FinishCleanupReportSettled`), meaning *the
close-out is already complete*; `would-run=0 settled=6 refused=4` now names the
four rows that really need an operator:

```
$ ./bin/gopi task finish-cleanup --dry-run          # BEFORE 4.1/4.3
finish-cleanup (dry-run): candidates=10 would-run=0 refused=10 — pass --apply …
  01a0a553-… worktree=/home/user/workspace/gopi-deadholder-resume — git status: (no output)

$ ./bin/gopi task finish-cleanup --dry-run          # AFTER
  01a0a553-… worktree=/home/user/workspace/gopi-deadholder-resume — git status: chdir /home/user/workspace/gopi-deadholder-resume: no such file or directory
finish-cleanup (dry-run): candidates=10 would-run=0 settled=6 refused=4 — pass --apply to run the chain
```

The four refused rows are, in the ledger's own words: `branch master is exempt`
(×2), a `live ledger row references the worktree` (a live holder) and
`worktree dirty (live bond protected) — D GOPI.md`. Those are refusals; the six
gone directories are not, and the counts now say so.

`FinishCleanupReportSettled` is deliberately narrow: it is settled only when the
report carries **no worktree step** (nothing was registered — the branch steps
are all that remained) or when a RECORDED worktree's own directory is the one
git could not enter (`chdir`/`cannot change to` + the reason names that path —
a reason naming a *different* directory is a broken a binding and stays a
refusal). A chain that removed a worktree, failed to remove it, or merely
PREDICTED a removal in plan mode is never settled: there was a worktree and
something happened to it. Tests:
`TestFinishCleanupSettledIsNotARefusal`,
`TestFinishCleanupGoneWorktreeIsReportedAsSettled`.

---

## 5. Not done (honest)

* **The operator's three original worktrees** (`gopi-textguard`,
  `gopi-brew-fix`, `gopi-web-ip`) were already gone before this task started:
  the night-shift session had committed their work and removed the directories
  by hand. They are therefore NOT re-verified here; what is verified is the
  mechanism, on a fixture built to the same shape (3.3).
* **The resume call site runs on the SUCCESS path only.** A resume that fails
  (`ag.Resume` returns an error) releases the CWD but does not run the chain —
  the work is unfinished, so there is nothing to hand off. If a future task
  wants "close out even on a failed resume", that is a deliberate change of the
  session's verdict and belongs in its own decision.
* **No automatic schedule for the one-shot verb.** It is operator/script-driven
  by design (a daemon-independent face); wiring it into a cron-like tick was out
  of scope and would be a new resident mechanism.
* **The `settled` disposition is derived, not stored.** Nothing in the ledger
  records "this row was already closed out" — `Settled` is inferred from the
  report (no worktree step, or the recorded directory is gone). An operator
  reading `settled=6` still sees six lines; the counts tell apart "nothing to
  do" from "a guard said no", but a row that is settled today can only be told
  from one that was never linked by reading its reason line.
* **`finishCleanupServerRecycle` returns before the push for a worktree-less row**
  in plan mode (a plan with no worktree short-circuits after the predicted
  push). That mirrors the executed arm's early return; no behaviour change was
  introduced, but the plan/execute symmetry there is thinner than at the other
  steps.

---

## 6. Files

| File | Change |
|---|---|
| `cmd/agent/cli_resume.go` | the missing call site + `resumeFinishTail` (order) + seams |
| `cmd/agent/cli_resume_finish_cleanup_test.go` | call-site regression + ordering test (new) |
| `cmd/agent/cli_resume_cleanup_e2e_test.go` | the operator's experiment, red-proven (new) |
| `cmd/agent/cli_tasks.go` | `gopi task finish-cleanup` verb, shared candidate scan, usage |
| `cmd/agent/cli_tasks_finish_cleanup_test.go` | verb contract tests (new) |
| `internal/tools/task_finish_cleanup.go` | plan mode, `PlanOnly`, `FinishCleanupPlan*`, plan marker consts |
| `internal/tools/task_finish_cleanup_test.go` | plan-mode safety + guard tests; sentinel/probe + settled regressions |
| `internal/tools/task_merge_executor.go`, `task_merged_cleanup.go`, `worktree_sweep.go` | the same fix at every sentinel consumer (4.1/4.2) |
| `cmd/agent/cli_tasks.go` | `settled` disposition in the plan counts and the APPLY tally (4.3) |
| `internal/taskverbs/verbs.go` (+ test) | `FinishCleanup` verb const, ledger group |
