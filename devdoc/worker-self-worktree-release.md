<!-- doc-class: historical -->
# Worker-side (producer) worktree release — a finished run frees its OWN directory

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/worker-self-release` (linked worktree `~/workspace/gopi-wt-release`)

## The defect (live row 01a095b2, 2026-09-12)

A worker that finished its own development did **not** release its own linked
worktree. The release existed only in *other people's* sweeps, and both were
blind to a row whose worker was still running:

| arm | what it needs | why it missed row 01a095b2 |
|---|---|---|
| `serveFinishCleanupSweep` (merger tick) | a row found by its capped windows, passing the ownership / live-owner gates | the row's chain reported `Skipped`; capped scan windows |
| `ReclaimDeadHolderWorktrees` | the claiming PID to be **provably dead** | the claim stamp is the long-lived `gopi worker` PID (still alive) |

Consequence: `git worktree list` kept naming `/home/user/workspace/gopi-voice-help`
(clean, `feat/voice-help` `65979b74`), `scripts/safe-merge.sh` gate [c] kept
refusing the branch, the merger's protected arm dropped it — **and nothing
printed a line anywhere saying why** (Rule 15 hole: a refusal was silent).

Live proof before the fix: 4 of the repo's 6 occupied worktrees belonged to rows
that were already terminal; the dead-holder repair audit had already had to free
17 by hand.

## The fix — the producer publishes its own release

`internal/tools/task_reclaim_self.go` (new):

* `ReclaimOwnFinishedWorktrees()` — scans rows **claimed by THIS process**
  (the exact `host:pid` string the claim ladder itself renders, via
  `TaskClaimOwner`), statuses `in_progress|completed|failed`, deliverable naming
  a worktree, and frees each through the **existing** guarded predicate:

  1. ownership + life-over (the only candidate set — a live peer's ground can
     never be in it, so nothing alive is ever a candidate),
  2. the directory must be **clean**,
  3. **no live process working inside it** (`/proc/<pid>/cwd` scan, which
     already excludes this process's own self-chain), and the checkout must
     still be on the row's branch.

  Removal is the ordinary `git worktree remove` — never `--force`, never
  `-D` on the branch. **The branch is the deliverable and is never touched.**
* `in_progress` **is** in the set, deliberately: the worker writes the terminal
  state *after* its release pass and the wrapper writes `completed` only after
  the child is gone, so at close-out time `in_progress` is the honest status.
  The ownership axis is what makes that safe — the claim pid *is* this process.
* A vanished-but-still-registered directory is **repaired** (pruned) too: the
  registration alone is what gate [c] greps.
* Every refusal is a **named** line
  (`own worktree release refused: <branch> (<id>) — <guard's sentence>`), and a
  registration that is already clean is silent (no per-pass burst).
* `GOPI_SELF_WORKTREE_RELEASE` is a report-only kill switch: it turns the
  removal off without turning the disclosure off. It deliberately does **not**
  reuse `GOPI_DEAD_HOLDER_RECLAIM` — one knob must not silently cover both
  "stop the sweep clearing other people's ground" and "stop my own hygiene".
* A wall budget (`ReclaimOwnFinishedWorktreesWithin`) means a hung git can never
  hold a close-out open; the overrun is named, not swallowed.

### Callers

* `cmd/agent/cli_tasks_worker.go` — **deferred** at the round's close-out
  (`defer workerRoundOwnWorktrees()`), so it runs on every exit path of a round
  (fall-through, `worker: run failed (continuing)`, panic) while the round's own
  claim stamp is installed, and *before* the output streams are detached so the
  release lands in the **task's own output log**.
* `cmd/agent/cli_tasks.go` — at the wrapper's `runTaskRunOnce` close-out, next
  to the existing finish-cleanup chain, and a `Skipped` chain is now reported
  **by name** instead of printing nothing.
* `disownProcessCwd()` — after the release the wrapper re-parents its CWD to
  `/proc` (`tools.ProcRootPath`; the release destination is a separate value,
  `tools.CwdReleasePath` — one name must not spell two facts. Rule 14: one home
  for each value, and it is the package that already owns procfs knowledge). Without it a process standing
  in the directory `git worktree remove` just deleted keeps that inode — and
  every file under it — alive until the process exits; with a long-lived caller
  that is the whole worker lifetime. The chdir is best-effort and its failure is
  reported, never swallowed.

## Verification

* Repro suite `internal/tools/task_reclaim_self_test.go` (6 tests): own directed
  release; refuse a live process inside; refuse a dirty worktree; ignore another
  holder's row; repair a vanished-but-registered directory; kill switch =
  report-only.
* **Red-proof**: against a stub arm (the exported symbol present, doing nothing —
  the pre-fix reality) 5 of the 6 fail; the 6th (peer isolation) is vacuously
  green without the arm and is honest about it. With the fix: 6/6 PASS.
* Acceptance pair kept green (`cmd/agent`): `TestAcceptance_DeadHolderReclaimUnblocksSafeMerge`
  ✓, `TestAcceptance_LiveHolderKeepsSafeMergeRefused` ✓ (the cwd guard still
  refuses, by name).
* Sandbox end-to-end (`scripts/acceptance-self-release.sh`, committed on this branch): real
  `bin/gopi worker --once`, sandboxed `HOME`/ledger, real repo + two worktrees —
  gate [c] refused before, the finished run's directory gone afterwards, branch
  tip byte-identical, gate [c] passing, and a live peer's worktree surviving.

## Not done / honest limits

* The **orphaned `in_progress` row** hole (`01a09573`, filed as `01a095b5`) is
  out of scope: a row whose claim stamp names a *dead* peer is not this
  process's and is not touched here. The dead-holder repair arm covers it.
* The pass trusts the ledger's `claimed_by` stamp. A row whose stamp was
  overwritten by another process is not released by the original holder — the
  conservative direction (nothing is freed on a guess).
* Measured on the installed binary of the day: `0244f3d1d6af-dirty` at master
  `9bfb390e`, and the worktree build `d79e1982-dirty` while master was
  `d79e1982`.

## Round 2 — the verification rig (what the first pass missed)

The first pass shipped the fix with the sandbox script only in `/tmp` and left
the cmd/agent rig's coverage of the producer arm undiagnosed. Round 2 corrects
both — and, per the Round-2 audit, it also corrects a CORRECTION: the first
version of this section described a `cmd/agent` rig mechanism that never
existed. What is claimed below is what is on the branch.

* **The sandbox is committed** as `scripts/acceptance-self-release.sh` (not a
  `/tmp` name the devdoc cites but nobody can run). ⚠️ Do **not** run it from
  inside a gopi session that shares its `HOME`/ledger: step C re-claims rows and
  self-recurses into nested workers. Use the sandbox env
  (`GOPI_ACCEPT_ALLOW_TMP_SANDBOX=1 GOPI_TEST_TMPDIR=/dev/shm/gopi-scratch-bug`).

* **`cmd/agent/worktree_release_accept_test.go`** (new) — the CLI-level rig for
  the producer arm, driving the REAL `cmd/agent` round-end entry point
  `workerRoundOwnWorktrees()` over a sandboxed ledger:
  * `TestWorkerReleaseOwnWorktree_Acceptance` — PASS: after the pass, the
    finished run's OWN directory is GONE, its **branch is PRESERVED**
    (byte-identical tip + the deliverable still on it), `git worktree list` no
    longer occupies it (so `scripts/safe-merge.sh` gate [c] — run for real —
    flips from refuse to exit 0), while a LIVE SIBLING's directory,
    registration and branch are untouched (and gate [c] still refuses the
    sibling's branch).
  * `TestOwnRelease_RefusesLiveSibling` — the red-proof half: the sibling's row
    is terminalised (its deliverable names the sibling's own directory), so a
    release keyed on "the row looks finished" instead of "the row is MINE
    (this process's `host:pid`)" would delete a live peer's ground. It FAILS on
    an arm that selects candidates without the ownership stamp — the intended
    reason — and passes with the shipped `claimed_by = <host:pid>` query.
  * **The claim stamp is the LIVE `host:pid`, and there is no worker claim
    segment anywhere in this rig.** The row is walked up the ledger's own
    ladder (`TaskSubmit` → `TaskStart`) and its `claimed_by` is what
    `tools.TaskClaimOwner("")` renders for the RUNNING TEST PROCESS — asserted
    alive via `/proc/<pid>/status` before and after the release. That is the
    arm's real precondition: the holder is provably NOT dead, so the
    dead-holder sweep cannot be what freed the directory.
  * The row's `deliverable.worktree` is stamped through the PRODUCTION seam
    (`tools.TaskDeliverableWorktreeBash`, the executor's post-bash-round
    backfill) with the wrapper env set, so the deliverable shape the pass reads
    is the shape a live run leaves behind.
  * Hermetic: every path is a `t.TempDir()` under the suite's pinned scratch
    root, env is saved/restored with `testutil.SnapshotEnv`, and fixture git
    runs with inherited `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE` DROPPED and
    `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` pinned to `/dev/null`.

### Retracted: the `TestMain` "sed" story

The Round-2 text as first written claimed that `cmd/agent`'s `TestMain` "seds
the worker claim segment (`<task>.worker`) to a stale absolute path under
`/tmp`", that rig tasks therefore died at `worker start failed: segment start:`,
and that the rig now writes that segment with a live `host:pid` via
`test taskOwnerSegmentEnvelope`. **None of that is true and none of it ever
was:** `git log --all -S'.worker'` is empty, no `.worker` claim-segment
mechanism exists in the tree, and `cmd/agent`'s `TestMain`
(`cmd/agent/testmain_test.go`) only calls `testutil.PinSuiteForTestMain`
(scratch-root + notify hermeticity) and `testutil.FinishSuiteForTestMain`. There
is no sed, and nothing in the suite blocks a live `host:pid` claim. The
conclusion drawn from that fiction ("the cmd/agent red was a TEST-ENV flaw, not
the fix") was therefore an unsupported diagnosis, and it is retracted here.
What remains true is the coverage statement in the next section: the producer
arm IS reachable from `cmd/agent` and is now exercised by the rig above.

### Coverage of the producer arm, stated honestly

| layer | what exists | status |
|---|---|---|
| producer arm, unit | `internal/tools/task_reclaim_self_test.go` — 6 tests (`FreeItsOwnDirectory`, `RefusesALiveProcessInside`, `RefusesADirtyWorktree`, `IgnoresAnotherRowsDirectory`, `RepairsAVanishedDirectory`, `KillSwitchIsReportOnly`) | PASS (`go test ./internal/tools/`) |
| producer arm, CLI end-to-end | `cmd/agent/worktree_release_accept_test.go` — the two tests above, driving `workerRoundOwnWorktrees()` and the real `scripts/safe-merge.sh` | see the table below |
| dead-holder (sweep) arm, CLI end-to-end | `cmd/agent/cli_tasks_dead_holder_accept_test.go` — 3 tests | PASS |
| end-to-end sandbox (real `bin/gopi worker`) | `scripts/acceptance-self-release.sh` | not run in-round (see the warning above) |

### Round-2 verification commands and results

Commands below ran under `GOPI_ACCEPT_ALLOW_TMP_SANDBOX=1
GOPI_TEST_TMPDIR=/dev/shm/gopi-scratch-bug`. `GOMODCACHE=/home/user/go/pkg/mod
GOPROXY=off` is required on this host: `GOPI_TEST_TMPDIR`/`GOPI_HOME` isolation
hides the ambient module cache, so the tree will not compile without it.

| check | result |
|---|---|
| `go vet ./internal/tools/ ./cmd/agent/` | clean |
| `go test -run 'TestWorkerReleaseOwnWorktree_Acceptance\|TestOwnRelease_RefusesLiveSibling' -v ./cmd/agent/` | PASS (both; own dir gone, own branch kept, live sibling untouched, gate [c] flips/re-refuses) |
| `go test -run 'TestReclaimOwnFinishedWorktrees' ./internal/tools/` | PASS (6/6) |
| `go test -run 'Acceptance_DeadHolder\|Acceptance_LiveHolder\|Acceptance_Vanished' ./cmd/agent/` | PASS (3/3) |
| red-proof (measured) | neutralise the ownership clause in a throwaway worktree (`WHERE claimed_by = ?` → `WHERE (claimed_by = ? OR 1=1)`, `internal/tools/task_reclaim_self.go`) → `TestOwnRelease_RefusesLiveSibling` FAILS with `the LIVE SIBLING's worktree is gone — the release took a peer's ground`, the pass naming the freed peer row (`own worktree released: feat/sibling-release (rig-sibling-live-peer) freed for merge; branch kept`). The test cannot pass vacuously; the throwaway worktree was removed with a plain `git worktree remove` (no `--force`), the branch untouched. |


## Round 3 — the end-to-end PASS, captured as a before/after pair (23:35–23:40)

Round 2 could only *cite* a PASS from an earlier session. This round reproduced
it as a captured pair on the fixed binary, so the claim no longer rests on a
remembered log line.

**Pre-state** (occupied worktree, before the producing run finishes):
`bash scripts/safe-merge.sh feat/self-release-e2e` → `gate_c_exit=1`,
`WT_PRESENT=yes`, `WT2_PRESENT=yes`, `TIP_KEPT=yes` — gate [c]:
`⚠️ 分支仍被 worktree 占用（未清理）` → `❌ 拒绝`.

**Post-state** (the same sandbox, row `01a09615`, after the round's own
close-out — recorded by a detached observer that waits for the worker pid to
vanish, because the release fires *after* the agent session exits):

```
task 01a09615: finish-cleanup: 01a09615 branch=feat/self-release-e2e done=[worktree]
task 01a09615 completed (elapsed 6m0s)
-- gate c --  ✅ [c] worktree 占用: 无 worktree 挂着 'feat/self-release-e2e'
✅ safe-merge: 全部通过 —— 可安全 merge 'feat/self-release-e2e'
gate_c_exit=0
WT_PRESENT=no
WT2_PRESENT=yes
SWEEP_DONE
```

That is the whole epic in four lines: the finished run's directory is **gone**,
its **branch** is intact, gate [c] went `❌ → ✅`, and a **peer's** worktree
survived in the same repo. Evidence files: `/tmp/self-release-sweep.out`
(observer), `scripts/acceptance-self-release.sh` (reproduction).

A second, freshly built observer (`/tmp/selfrel-5H70-observer.sh`) was armed
against the round-3 sandbox for the same before/after pattern; its pre-state was
recorded (`gate_c_post_exit=1`, `WT_PRESENT=yes`) and its post-state lands in
`/tmp/selfrel-5H70-observer.out` after that round exits.

A second, independent observer (`/tmp/selfrel-5H70-observer.sh`, a different
sandbox on the same branch/binary) completed the same pattern, duplicating the
result rather than reusing it:

```
== producer pid 3904906 gone ==
gate_c_post_exit=0
WT_RELEASED=yes
PEER_WT2_SURVIVES=yes
WT_DIR_PRESENT=no
BRANCH_TIP_KEPT=yes
OBSERVER_DONE
```

Two independent sandboxes, same branch and binary, pre-state refused and
post-state released with the peer intact: a corroborated end-to-end PASS.

### Retraction

An intermediate note in this file's history asserted that
`cmd/agent/worktree_release_accept_test.go` "exists on no branch". That is
**false as of `ea095c80`** — the rig was landed on this branch by a sibling
session at 23:32 and both of its tests PASS here:

```
--- PASS: TestWorkerReleaseOwnWorktree_Acceptance (2.42s)
--- PASS: TestOwnRelease_RefusesLiveSibling (1.21s)
```

Round-3 gates on tip `ea095c80`: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
→ BUILD_OK; `go vet ./internal/tools/ ./cmd/agent/` → clean;
`TestClaimStampFreshness` (the dead-holder/liveness pinning test in
`internal/tools/claim_holder_freshness_test.go`) → PASS;
`Acceptance_DeadHolder|Acceptance_LiveHolder` (`cmd/agent`) → PASS.
