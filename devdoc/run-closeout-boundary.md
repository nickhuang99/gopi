<!-- doc-class: normative -->
# The run close-out boundary: the child records lineage, the wrapper writes the row

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task 01a16f7c, 2026-10-25. This page is the one window on the boundary the task
drew: which half of the old "worker-writeback" seam was deleted, which half was
kept, and **why the kept half is not the same thing wearing a new name**. A later
reader who finds `GOPI_TASK_RUN_WRAPPER_PID` in a spawn seam will otherwise
conclude the deleted pid-liveness check is still there, or — worse — reintroduce it.

## What was deleted, and the reason it could not have helped

`internal/agent.closeOutRunRow` (with `runCloseoutEnabled`, `runCloseoutEnv =
GOPI_TASK_RUN_CLOSEOUT`, `autoTaskRunCloseoutStatsFormat`) and the tools seam it
called, `tools.TaskRunCloseoutStaleRun` (with `RunCloseoutRequest`,
`RunCloseoutOwnedBy`, `runCloseoutDigestDefault`, `runCloseoutClaimEvidenceHolds`).

It ran in the CHILD's END hook and tried to reconcile the WRAPPER's row by asking
**"is the pid my wrapper published still alive?"**. That question has no
informative answer:

- The wrapper is the process that `Wait`s for the child. If the wrapper is gone,
  it is gone — asking `/proc` about it after the fact cannot change the row, and
  the case is already the ledger's stale/zombie disposition (claim holder dead ⇒
  the row is reclaimable by the ordinary machinery).
- If the wrapper is alive, its own claim is live, and the seam's own gate refused
  every write ("a live wrapper owns the row: not our close to make").
- On the healthy path the row was **already terminal** by the time the child's
  END hook ran, so the guarded `UPDATE` matched nothing.

So the net effect on every healthy run: one `/proc/<pid>` read plus one ledger
read, in exchange for a decision that could only ever be "do nothing". The defect
class it was built for (a wrapper killed inside its post-`Wait` window) is not
reached by it either, because a wrapper killed there leaves a dead claim — which
the stale/zombie machinery already handles.

## What was kept, and why it is a DIFFERENT question

Two readers survive, and both are **lineage** readers, not liveness readers:

| reader | question it asks |
|---|---|
| `internal/tools/run_closeout_reap.go` (`RunCloseoutReapRunChildren`, `runCloseoutLineageNamesRun`) | "is this leftover process a descendant of the run I just finished?" — before killing it |
| `internal/tools/task_finish_cleanup_holder_orphan.go` (`finishCleanupHolderEnvWrapperPID`) | "does the process pinning this worktree belong to a run, or to a third party?" |

Both read the exec-time environment block (`/proc/<pid>/environ`), which the child
inherits into everything it spawns and which the kernel keeps after reparenting.
That is evidence of **provenance**, and it stays true regardless of whether any
pid is alive. Deleting the writer of those variables (`cmd/agent/cli_tasks.go`)
would break both readers, so the spawn seam still stamps
`TaskRunIDEnv` / `TaskRunOutcomeEnv` / `TaskRunWrapperPIDEnv`.

`tools.RunCloseoutOwnedBy` was the one reader whose answer was a **liveness
verdict** ("is the pid that published this request still the run's"). It has no
caller now, and it is gone with the seam.

## The one survivor on the child side: the truncation note

`internal/agent.recordRunTruncationOnWrapperRow` is what remains of the END-hook
call, and it is deliberately narrow:

- A `task run` / `worker` child registers **no auto row** (single-row exec), so on
  a truncated run its only ledger contact is the wrapper's row.
- Every engine-forced stop (wallclock cap, stuck net, iteration budget) exits
  **zero**, so the wrapper's post-`Wait` writeback would stamp `completed`.
- The note is the child's **testimony of WHY the run stopped**, written as the
  append-only runend record (`tools.RecordRunEnd`) where the surviving writer
  reads it (`tools.RunEndReasonFor`).

It writes no status, closes no row and reads no process. That distinction is the
whole boundary: **recording a fact about my own run is in scope; deciding who owns
someone else's row is not.**

## What changed around it (comments that would have lied)

Deleting a seam leaves prose pointing at it. The following were rewritten in the
same commit, because a comment that names a deleted function teaches the next
reader a false model: `internal/tools/tasks.go` (the `RunCloseout*` block and two
call-site comments), `internal/tools/task_reclaim.go`
(`reclaimRunResidueUncleanEvent`), `cmd/agent/task_run_closeout_landed.go`,
`cmd/agent/task_run_success_path_ledger_read_test.go`, and the
`cli_tasks_worker_writeback_test.go` header.

## Tests: retargeted, not deleted silently

- `internal/tools/task_run_closeout_test.go` — **deleted**; it was the deleted
  seam's own test file. Two fixtures other tests build on (`deadClaimPid`,
  `hostClaimStamp`) moved to `claim_dead_pid_test.go` unchanged.
- `TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord` — deleted with the door it
  drove; the residue record's other doors are still covered by the tests above it
  in `run_untracked_residue_test.go`.
- `TestFailedReasonClosesTheWholeDoorFamily` — its second door was the deleted
  seam; the door was removed and the remaining doors keep their assertions.
- `TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnCloseoutDoor` →
  `...OwnTerminalWrite`: now drives `tools.TaskUpdateWithResult`, i.e. the choke
  point every terminal write passes. A proof aimed at a door nobody uses proves
  nothing; this one is aimed at the surviving door.
- `TestWriterIdentity_NaiveOwnershipRuleRefusesTheLegitimateCloseout` — deleted
  with the predicate it measured (`naiveOwnershipRefusal`, a test-only function
  standing in for the rule the epic decided not to ship). The decision itself is
  still exercised by the door census beside it.
- `TestCloseOutRunRow*` → `TestRunTruncationNote*` in `internal/agent`: same
  scenario (wrapper-child truncation must reach the row), driven through the
  surviving door; the "must not complete a live wrapper's row" assertion is kept
  as a contract of the note.
- `cmd/agent/cli_runpath_boundary_rp1_test.go` dropped `GOPI_TASK_RUN_CLOSEOUT=off`
  (dead knob, and its presence would have taught the reader the knob exists) and
  pins `GOPI_AUTO_TASK_REGISTER=off` instead — the probe owns its own ledger
  (`--db`) and must leave no row.

## Evidence measured while standing in the seam (Rule 41 evidence, recorded here)

Run 01a16f7c's own process tree, read with `tr '\0' '\n' < /proc/<pid>/environ`:

```
pid=4055211 ppid=4055119 WRAPPER_PID_hits=1 RUN_ID=01a16f7c-…  cmd=bash -c …
pid=4055119 ppid=4053460 WRAPPER_PID_hits=1 RUN_ID=01a16f7c-…  cmd=/home/user/work/gopi/bin/gopi
pid=4053460 ppid=4053459 WRAPPER_PID_hits=0 RUN_ID=none        cmd=./bin/gopi worker 01a16f7c-…
```

Consequences worth keeping:

1. **The wrapper itself does not carry the lineage variables** — only the child
   it spawns does. So a scan that walks the ancestry chain upward from a leaked
   process finds the lineage at the child and NOT at the wrapper; a reader that
   expected to find it at the wrapper would wrongly conclude "not this run's".
2. **`GOPI_TASK_RUN_WRAPPER_PID` names the worker/wrapper pid** (4053460), i.e. the
   process whose death the deleted close-out was trying to detect — which is
   exactly the process that cannot be asked after it is gone.
3. `/proc/<pid>/environ` is the exec-time block, so a fork→exec window yields an
   EMPTY block; a lineage scan must treat "unreadable" as unknown, never as
   "not ours" (that is what `runCloseoutEnvRead.readable` carries).
