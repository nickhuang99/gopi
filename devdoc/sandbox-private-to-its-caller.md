<!-- doc-class: historical -->
# A Sandbox Belongs to Exactly One Caller — the Label Is Not an Ownership Claim

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a132eac84271b9a5d344024f1bc45f` (hosta) · **Date**: 2026-10-13
**Symptom class**: two concurrent `make test-budget PKGS='./cmd/agent'` runs
produce FAILs that have nothing to do with the branch under test, while every
isolated run passes.

## The measurement that started it

The source report (session `01a13292dfdb`) recorded two concurrent budget runs in
which stream A failed `TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord`
and stream B failed a five-test dirty-worktree/adoption family, with
`load_start 2.6 → load_end 7-10`.

Reproduced here with two simultaneous `make test-budget PKGS='./cmd/agent'` runs:

- stream A (`run-1791854432-3946754.json`) — `TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord/nothing_yet` + `/filed`
- stream B (`run-1791855090-89871.json`) — `TestResumeRescuesUntrackedResidueBeforeRemoval`
  (`exactly one rescue ref must exist for the row, got []`), `TestServeAdoptionDirtyTreeSkip`,
  `TestAdoptionCleanTreeStaysStrict`, `TestTaskShowBranchNoRowAnswersWithLedgerAndGitFacts`
  (`the answer must contain "memory_escape.go"`), `TestWorkerServerDeliveryDirtyWorktreeKeepsBranch`
  (`worktree dir …/wt must survive a dirty skip: … no such file or directory`),
  `TestWebToolsSignalShutdownWritesSessionEnd`, `TestWrapperSilentFalseSuccessOnRefusedWriteback_RED`,
  `TestLifecyclePassEscalatesDirtyOrphanEndToEnd`.

**Every failure is the same shape: a child process's artifact is GONE
(0-byte/absent file, missing ref, removed worktree) or a write is REFUSED.**
Scratch contamination was ruled out by measurement: each run owns a distinct
suite root (`/dev/shm/suite-cmd-agent-run-2091504451` vs `…-2207371589`), and a
6-test isolated control (`-run` over the failed family) passed WHILE both
concurrent suites were running.

## The root cause (measured, not inferred)

`internal/sandbox.Create` asks `reusableSandbox(root, label)` for an existing
directory to adopt, and the predicate matched on the **label alone** — the same
key the CLI documents as its idempotence key. Two shapes of the defect, both
measured:

1. **Two live runs, one directory.** Two concurrent
   `gopi tool sandbox create --label L` calls with *different* task ids were
   handed the SAME path (`probe-root-a132ea/sandbox-l-417ab9c0`); the second
   call's marker overwrote the first's (`task_id` `T1` → `T2`, `created=true
   reused=false` then `created=false reused=true`). The first caller's
   `t.Cleanup(os.RemoveAll(sb))` then deleted the second caller's ground
   mid-flight — which is exactly the observed symptom class.
2. **A dead marker is not an abandoned sandbox** — the shape that survives the
   obvious fix. `gopi tool sandbox create` is a SHORT-LIVED process, so a
   sandbox a caller is *still using* carries a marker whose pid has already
   exited. The next caller read that as "the leftover of a run that is gone"
   and adopted it — handing out, and later deleting, another live run's private
   profile. In the two-suite run, both streams' subtests shared
   `sandbox-signalrecfiled-2c6dd0e5` for exactly this reason (the name is
   `sandbox-<label>-<rand>`; the pid in the marker is the CLI's, not the
   suite's).

The owning test helper is `cmd/agent/signal_record_filed_guard_test.go`'s
`signalRecordFiledSandbox` (fixed label `signalrecfiled` +
`t.Cleanup(RemoveAll)`); the second caller inherits the first's profile, session
dirs and all.

## The fix

`reusableSandbox` adopts a candidate only when the ownership marker proves **this
very process** made it — `Owner.ThisProcess()`: the pid matches `os.Getpid()` AND
the kernel start ticks match, so a recycled pid or an old marker is not "mine".
Everything else — a live foreign sandbox, a dead foreign sandbox, an unreadable
marker — is left alone and the caller gets a fresh directory.

Why this is the right boundary: the initializer's contract is that **a sandbox is
private to its caller**, and only the caller can prove identity. The CLI shape
that legitimately re-adopts is `gopi tool sandbox run`, which creates AND executes
the child from one process, so the label key and the ownership identity are the
same run; a caller that needs to re-adopt across processes names the path.

`isSandboxForLabel` also replaces the bare `HasPrefix` name match, so one label
cannot match another (`build` no longer matches `builder-…`).

## What is NOT claimed

- The sandbox leak is a **necessary** cause in the reproduced failures; no claim
  is made that it is the only shared resource under two concurrent budget runs.
- The deterministic single-run failures seen while investigating
  (`TestPublishedHelpPagesStayWithinTheirUsageBudget` with subtest `worker`,
  `TestToolsMaintenanceHelpSurface*`: `gopi worker --stop --branch B` is
  advertised by `cmd/agent/main.go` and missing from the frozen inventory
  `devdoc/gopi-tools-maintenance-help-surface-inventory.md`) are a separate,
  single-run defect and are filed as their own row (Rule 41).

## Verification (what was actually run)

```
Ran:   two concurrent `go test ./cmd/agent/ -run 'TestSignalProvenanceRecordFiledWhenAClaimingDoorExits|TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord' -count=1`
       BEFORE the fix : A=1 B=0 (A failed `/filed` + `/nothing_yet`; both processes
                        reported one shared `sandbox-signalrecfiled-*` directory)
       AFTER  the fix : A=0 B=0, and the watcher observed two DISTINCT sandbox dirs
                        (`sandbox-signalrecfiled-f6165d92`, `…-752f678e`)
Ran:   go test ./internal/sandbox/ -count=1            → ok
Ran:   red proof — the guard `if !owner.ThisProcess() { continue }` removed,
       rebuild, `go test ./internal/sandbox/ -run TestCreateDoesNotAdoptADeadRunsLeftover`
       → FAIL (Create adopted the dead run's leftover);
       restore verified by content hash
       (`sha256sum internal/sandbox/sandbox.go` == the recorded fixed hash)
```
