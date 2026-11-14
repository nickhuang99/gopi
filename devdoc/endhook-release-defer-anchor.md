<!-- doc-class: historical -->
# Task Report: the end hook the guard names is the end hook that runs (cwd-release defer)

Branch `feat/endhook-release-defer`, worktree
`/home/user/workspace/gopi-endhook-defer`, base master `cacb6e35`.

## Completed

### The defect, measured

`TestTaskRunReleaseIsDeferredInTheEndHook` (`cmd/agent/task_run_release_coverage_test.go`)
was RED on master `cacb6e35`:

```
--- FAIL: TestTaskRunReleaseIsDeferredInTheEndHook (0.00s)
    task_run_release_coverage_test.go:181: finishSessionTaskRecorded must DEFER
    worktreerelease.ReleaseReporting — a direct call is skipped by the hook's
    early returns, which is how a child stays standing in its worktree and
    vetoes its removal
```

Root cause (`git log -L 372,380:internal/agent/taskregister.go`): commit
**`572100d5`** ("fix(task ledger): a run the engine cut short is not a completed
run") split the end-hook BODY into a new function
`finishSessionTaskRecordedWithReason` — which owns the `defer
worktreerelease.ReleaseReporting(...)` at its top, before the early-return
gates — and left `finishSessionTaskRecorded` a one-line forwarder passing
`runend.Finished`.

The *defer itself was never wrong*: the release still covers every exit path of
the function that runs. What broke is the **name**: the function carrying the
end hook became one with **zero production callers**, while `loop.go:3199` (the
only real caller), `finishSessionTask` (the panic-recover entry point) and the
three cases in `internal/agent/truncated_run_test.go` all call the new name —
and the guard anchors on `finishSessionTaskRecorded` because that was the
function that owned the hook before the split. Every doc reference
(`devdoc/resume-writes-terminal-status.md`, `devdoc/truncated-run-is-not-completed.md`,
`cmd/agent/cli_tasks.go:2297`, `internal/agent/agencyhook.go:18`) still pointed
at the old name too.

### The repair — production code, not the assertion

Merged the two functions back into ONE end hook that keeps the anchored name:

- `internal/agent/taskregister.go`: `finishSessionTaskRecorded` is now the
  single end hook and takes the end reason as its **parameter**
  (`truncation runend.Reason`). The forwarder and
  `finishSessionTaskRecordedWithReason` are gone; the `defer
  worktreerelease.ReleaseReporting(a.taskLogWriter(), agentProgName)` sits
  where it always did, at the top of the body before the early-return gates, so
  coverage of "no auto row registered / no session log" and "failed ledger
  close" is unchanged. The doc comment records WHY the shape is one function.
- `finishSessionTask` (the 5-arg entry the panic-recover round uses) now calls
  it with `runend.Finished`, as before — same semantics, one name.
- `internal/agent/loop.go:3199`: the real caller now names the anchored
  function (`a.finishSessionTaskRecorded(..., endReason)`).
- `internal/agent/truncated_run_test.go`: three call sites renamed.

The guard was **not weakened or re-pointed**: it still finds
`finishSessionTaskRecorded`, still requires the deferred call, still forbids a
second/direct spelling. Behaviour is identical because the defer and the body
are byte-identical — only the naming (one function instead of two) changed.

### Stale doc references repaired

- `devdoc/truncated-run-is-not-completed.md` — its "the old
  `finishSessionTaskRecorded` is kept as a thin wrapper" sentence described the
  split shape; rewritten to the one-hook shape with the re-merge noted.
- `devdoc/resume-writes-terminal-status.md` — two references to
  `finishSessionTaskRecordedWithReason` re-pointed at the one function.

## Not Done

- **`internal/agent/truncated_run_test.go` keeps a stale doc sentence.** Its
  trailing comment block reads "// TestRunEndReasonFor_ReadsWhatTheRunRecorded
  pins the second door to the same" twice (the first line duplicated, a
  pre-existing cosmetic defect on master, unrelated to this change). Left
  alone: not this task's scope, and touching it adds diff noise to a rename
  commit.
- **The `finishSessionTask`/`finishSessionTaskRecorded` pair is still two
  functions.** They are distinct duties (the panic-recover collector vs the
  close-out), so this was not merged; only the third, empty function was
  removed. If a future refactor wants one entry point, that is a separate
  judgement about `panicCollectorPeek`.
- **Rule 11 (LLM adoption proof) does not apply** — no tool, prompt, or routing
  change; this is a Go source-shape repair.

## Verification

- Build:  `go build ./...` → no output (OK); `gofmt -l internal/agent/` → empty
- Vet:    `go vet ./internal/agent/ ./cmd/agent/` → no output (OK)
- Tests:  `go test -count=1 -run 'TestTaskRunReleaseIsDeferredInTheEndHook|TestTaskRunFailedChildReleasesOwnWorktree' ./cmd/agent/`
          → `ok github.com/nick/gopi/cmd/agent 3.138s`
- Tests:  `go test -count=1 ./internal/agent/ ./internal/worktreerelease/ ./internal/standards/ -run 'Release|CwdRelease|Gate'`
          → `ok internal/agent 2.048s`, `ok internal/worktreerelease 2.316s`,
            `ok internal/standards 18.617s` (the cwd-release gate still sees the
            call on the tree)
- Ran:    `go test -count=1 -run TestTaskRunReleaseIsDeferredInTheEndHook ./cmd/agent/`
- Result: BEFORE (master `cacb6e35`): `--- FAIL: TestTaskRunReleaseIsDeferredInTheEndHook`
          with the "must DEFER" line quoted above.
          AFTER: exit 0 (`ok … 3.138s`) — see Evidence.
- Tests:  `go test -count=1 -run TestTaskRunReleaseIsDeferredInTheEndHook ./cmd/agent/`
          re-run at close-out (after the package job was killed) → `ok
          github.com/nick/gopi/cmd/agent 0.733s`
- Ran:    `go test -count=1 ./cmd/agent/` (the whole package) — run TWICE, in
          two contexts, and **NEITHER run is a valid package verdict.** Both are
          recorded here because the second one is a measurement worth keeping.
- Result: **run 1 (from the worktree, detached job, killed at t+10m):** the
          process was still alive with an EMPTY stdout file after 10 minutes,
          so it was killed — no verdict was produced and none is claimed. The
          stdout stayed empty because the command ended in `| tail -60`, which
          buffers until the pipe closes; the "empty log" was the reporting
          shape, not a stuck suite.
- Result: **run 2 (from a CLONE, `-timeout 15m`, no `tail`):** completed in
          `902.749s` (15m02s) — so run 1 was not stuck either, only budgeted
          short and observed through a buffering pipe. **It reported `FAIL` for
          the package, and its `--- FAIL: TestTaskRunReleaseIsDeferredInTheEndHook`
          line is NOT a verdict on this repair**: the suite's own repo-context
          hazard cases move the process cwd and free the repo ground mid-run
          (`getwd: no such file or directory`), which is exactly the class
          memory record #3379 documents ("run the suite from a CLONE — the
          suite's own repo-context-hazard cases FREE worktrees of the cwd repo
          and delete your own working directory mid-run (47 bogus `getwd: no
          such file or directory` failures, measured twice)"). Measured here:
          run 1 was from a WORKTREE and destroyed its own worktree mid-run
          (`/home/user/workspace/gopi-endhook-defer` no longer exists; 44
          `getwd: no such file or directory` hits, 70 `--- FAIL` lines, most of
          them that cascade). Run 2 was from a clone and the clone SURVIVED
          (`ls -d` on it succeeds — see Evidence), but its failures are still
          the clone-context family, not this repair's: the `bin/gopi` staleness
          guard (`… bin/gopi … is OLDER than … internal/agent/loop.go`), the
          help-surface family (`TestG5InventoryMatchesTheHelp`,
          `TestTopLevelUsageDelegatesEnvVarsToHelpEnv`,
          `TestCLINamingExceptionsDocumentedInTopLevelHelp`, …), and a
          `test timed out after 15m0s` panic parked in
          `TestEditToolAnnotatesTeachingDocsInAuditedTrees`. **The one test this
          task repairs PASSES in every context measured** — alone in the
          worktree (`ok … 1.637s`), alone in the clone after a real rebuild
          (`ok … 0.792s`), and on current master (`ok … 0.792s`).
          The package-level verdict this task does NOT produce, and does not
          claim: a green `go test ./cmd/agent/`. It is the pre-existing
          package-budget defect Rule 27's own epic names, not a regression from
          this commit — the guard file is byte-identical to master
          (`git diff cacb6e35 -- cmd/agent/task_run_release_coverage_test.go`
          prints nothing).
- **A durable lesson for the next run of this package** (recorded to memory):
          running `./cmd/agent/` from a linked worktree of the repo under test
          is destructive — the hazard cases unregister and delete the cwd repo's
          worktrees, `bin/gopi`'s staleness guard needs the binary rebuilt
          AFTER any source edit, and piping the run through `tail` hides all of
          it until the pipe closes. Clone first, rebuild, then run with
          `-timeout 15m` and no pipe.
- Commit: `cd645332` (repair) + `de5cb19a` (this report) — both on
          `feat/endhook-release-defer`, and **already merged into master as
          `f9f58882`**, so `git show master:devdoc/endhook-release-defer-anchor.md`
          recovers this report.

## Evidence (self-filed)

- **The repair is what made it green, and the guard still has teeth.**
  Red-proof against the NEW shape: with `defer` removed from line 406 of
  `internal/agent/taskregister.go` (`sed -i 's|^\tdefer
  worktreerelease.ReleaseReporting|\tworktreerelease.ReleaseReporting|'`),
  `go test -count=1 -run TestTaskRunReleaseIsDeferredInTheEndHook ./cmd/agent/`
  printed BOTH diagnoses —
  `finishSessionTaskRecorded must DEFER worktreerelease.ReleaseReporting …`
  and `… must have exactly ONE release spelling: the deferred one …` — then the
  file was restored from `/tmp/taskregister.bak`. So the guard is anchored on
  the function that actually owns the defer, not passing vacuously.
- **Before/after on the same command** (the exact invocation is the `Ran:` line
  above): `--- FAIL: TestTaskRunReleaseIsDeferredInTheEndHook` on master →
  `ok github.com/nick/gopi/cmd/agent 3.138s` on this branch.
- **`git status --porcelain` in the worktree**: empty at close-out (all changes
  committed).
- **Red-proof re-run on current master `c14546d0`** (the tree that carries the
  merged repair), in a worktree from the branch, restoring the file afterwards:
  with `defer` removed from line 406,
  `go test -count=1 -run TestTaskRunReleaseIsDeferredInTheEndHook ./cmd/agent/`
  printed BOTH diagnostics —
  `task_run_release_coverage_test.go:181: finishSessionTaskRecorded must DEFER
  worktreerelease.ReleaseReporting …` and `:184: … must have exactly ONE
  release spelling: the deferred one …` — then the file was restored from
  `/tmp/taskregister.bak3` and the same command printed
  `ok github.com/nick/gopi/cmd/agent 0.792s`, with `git status --porcelain`
  empty. So the guard has teeth on the shape master now ships.
- **The clone survived run 2** (run 1's context did not survive run 1):
  `ls -d /home/user/workspace/gopi-endhook-clone` → succeeded after run 2,
  while `ls -d /home/user/workspace/gopi-endhook-defer` (run 1's worktree) →
  `No such file or directory`. This is the worktree-vs-clone difference memory
  #3379 names, observed directly in this task.
- **Sibling worktrees freed during run 1 — both were at the same base and no
  commit was lost.** Run 1 was launched from a worktree of the shared repo, so
  its hazard cases were in a position to free sibling registrations of that
  repo. Two worktrees present before run 1
  (`/home/user/workspace/gopi-memory-subtask-beat`, branch
  `feat/memory-subtask-beat`; `/home/user/workspace/gopi-subtask-beat-memory-pointer`,
  branch `feat/subtask-beat-memory-pointer`) were gone after it, and neither
  branch ref resolves any more. Both sat at base `cacb6e35` (recorded earlier in
  this session's `git worktree list` output), so **no work was lost**; the
  audit log carries no `memory-subtask-beat` removal record, so the freeing was
  not a recorded `worktree:remove` and no attribution is claimed either way.
  Recorded here because a freed sibling is a real cost of the worktree-context
  mistake, not to assert a cause the observation does not establish.
