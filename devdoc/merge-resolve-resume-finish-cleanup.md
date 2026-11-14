<!-- doc-class: normative -->
# Merge resolution: feat/resume-finish-cleanup into master

Resolver session (merger conflict resolver). Worktree:
`/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-2470012/gopi-merge-resolve-398739425/wt`
Branch `feat/resume-finish-cleanup` (tip before merge `a3637876`) ← master `1dccad8e`.

## Conflicted files (reproduced with `git merge master --no-commit --no-ff`)

4 UU files, 9 hunks:

| file | hunks | class |
|---|---|---|
| internal/tools/task_finish_cleanup.go | 5 | sentinel/reader refactor (ours) vs. two-state dirty naming (master) |
| internal/tools/task_merge_executor.go | 1 | two spellings of the dirty gate |
| internal/tools/task_merged_cleanup.go | 1 | clean sentinel name |
| internal/tools/worktree_sweep.go | 1 | clean sentinel name |

The branch (ours) had renamed the clean sentinel to `finishCleanupNoOutput`,
added `finishCleanupProbeClean` (exact comparison — a *substring* test over
git's output let a file named `no output` read as a clean tree and defeat the
live-bond guard), added `finishCleanupFirstLine`/`finishCleanupDiagnosticLine`
(a failed probe must NAME its cause, never print the clean sentinel), and
renamed the sentinel **literal** out of the other production files (its own
literal-gate test enforces that). Master had introduced
`finishCleanupNoOutputSentinel` + `finishCleanupWorktreeDirtyReason`, which
splits "dirty" into *tracked changes* vs *untracked-only residue* and records
the residue as a durable step.

## Resolution — one helper, union of both intents

* **One sentinel constant**: `finishCleanupNoOutput` (ours). It is the name the
  merged tree's literal-gate test (`TestFinishCleanupNoOutputSentinelLiteralGate`)
  pins, and 4 other production files reference it. Master's
  `finishCleanupNoOutputSentinel` constant is retired (same value, one spelling —
  Rules 1b/13/14); `worktree_sweep.go` / `task_merged_cleanup.go` return
  `finishCleanupNoOutput`.
* **Master's reader wins the dirty gate** (3 call sites in
  `task_finish_cleanup.go`, 1 in `task_merge_executor.go`): it is the call the
  accepted test `TestFinishCleanupWorktreeDirtyReasonMatchesReclaim` requires,
  and it is the richer behaviour (two NAMED refusal states + the residue step
  record). Its internals keep **our** exact-comparison discipline: the
  clean-probe test is `trimmed == finishCleanupNoOutput`, not
  `strings.Contains(...)` — master's substring test is exactly the defect our
  side measured (a file named `no output` read as clean). So the union keeps
  master's naming and ours' guard.
* **Our `rep.Skipped = true` restored in all three call sites** (master's hunk
  had dropped it; the `Skipped` flag is what `FinishCleanupReportSettled` /
  the operator's refusal count read — the omission made a refusal render as a
  completed report). See *pre-existing break* below.
* **`task_merge_executor.go`**: ours' `finishCleanupProbeClean` call (empty
  porcelain → sentinel → clean, anything else is dirt, untracked included)
  with master's reason text and master's `MergeExecStatusSkipped` constant
  (this side's `mergeExecOutcomeSkipped` had no definition left in the merged
  tree). Master's inline `strings.Contains(dirty, "no output")` is the same
  substring defeat, so it is not carried over.
* `finishCleanupProbeClean` and `finishCleanupDiagnosticLine` are kept: the
  latter is called from **non-conflicted** lines of `worktree_sweep.go:873` and
  `task_merged_cleanup.go:875/992`, so dropping the block would not compile.

## Test adjustments forced by the union (marked in-file)

* `task_finish_cleanup_test.go` `TestFinishCleanupDirtyFileNamedNoOutput`:
  asserted the literal substring `"worktree dirty"`; the union names the state
  `worktree carries ONLY UNTRACKED files …`. The test's *property* (a file
  named after the sentinel is refused and the worktree is kept) is unchanged —
  only the expectation string moved. Annotated with a `merge-note:` comment.
* `worktree_refusal_arms_test.go`: `finishCleanupNoOutputSentinel` →
  `finishCleanupNoOutput` (sentinel reference, same single constant).

### Follow-up (same session, after the bounded test run)

The bounded `go test ./internal/tools/ -run 'TestFinishCleanup|TestReclaim|TestMergeExec|TestWorktreeSweep|TestServerRecycle'`
still reported 4 dirty-gate failures. Two were inherited from MASTER's own
baseline, not from my resolution, and are repaired here because the union
adopts master's naming:

1. `finishCleanupDirtyUntrackedPrefix` (master's constant) carried no `dirty`
   token, while the assertions in BOTH sides' test files
   (`TestFinishCleanupChainDirtyWorktree`, `TestServerRecycleDirtyWorktreeSkipped`,
   `TestFinishCleanupPlanKeepsEveryGuard`) require the refusal to *name*
   dirtiness. Master alone fails this (proven: `git archive master` into a
   scratch tree → `dirty worktree must skip, got Skipped:false`). The prefix now
   reads `worktree dirty (live bond protected) — carries ONLY UNTRACKED files …`:
   master's two-state naming is kept, the `dirty` token the refusal vocabulary
   is anchored on is restored, and the reuse of the protected opener is shared
   with the tracked prefix.
2. `len(rep.Steps) != 0` on a dirty skip (an assertion only the HEAD side
   carried) contradicts master's deliberate `finishCleanupUntrackedResidueNote`
   step record. Narrowed to `onlyUntrackedResidueNotes(rep.Steps)` — the
   property kept is "no step RAN (`OK:true`); a note/prediction is not an
   action", which is what the assertion was for.

Verification: the same bounded selection then reports `ok
github.com/the operator/gopi/internal/tools 8.496s`; `go build ./...` is clean.

## Pre-existing break proven on the master baseline (not introduced here)

Master alone does **not** pass its own dirty-gate tests: extracting
`git archive master` into a scratch tree and running
`go test ./internal/tools/ -run 'TestFinishCleanupChainDirtyWorktree|TestFinishCleanupPlanKeepsEveryGuard|TestFinishCleanupDirtyFileNamedNoOutput|TestProbeCleanIsNotASubstringMatch'`
reproduces `TestFinishCleanupChainDirtyWorktree` failing with `Skipped:false` —
master's own `finishCleanupWorktreeDirtyReason` call site omits
`rep.Skipped = true`. The resolution *repairs* that omission.

## Verification (bounded)

* `git merge master --no-commit --no-ff` reproduced; 0 conflict markers left
  (`grep -rn '^<<<<<<<\|^=======$\|^>>>>>>>' --include=*.go internal/ cmd/` empty).
* `go build ./...` → OK.
* `gofmt -l internal/tools/` → only `task_deliverable_worktree.go`, which is
  also unformatted at master's tip (pre-existing).
* `go test ./internal/tools/ -run 'TestFinishCleanup|TestProbeClean|TestReclaim|TestMergeExec|TestWorktreeSweep' -count=1`
  (with the inherited daemon env sanitised: `env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD`).
* No `go test ./...` (the repo's full suite has a known hang); `git add` per
  file, never `-A`.
