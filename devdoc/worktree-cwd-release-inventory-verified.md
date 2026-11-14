<!-- doc-class: normative -->
# Task Report: verify the cwd-release producer inventory by CENSUS (branch `feat/cwd-release-inventory-verified`)

Epic: *`TestGateCwdReleaseFindsTheRealTree` was red because its floor (5) exceeded
the producers the trigger set matched (4). A branch already set the floor to 4,
arguing the two vanished producers were DELETED WITH their machinery by refactor
`126fad74`, not relocated. Verify that claim independently and either confirm the
acceptance on master or find the detection hole the epic warns about.*

Base: master `16ec3437` (where `go test ./internal/standards/ -count=1 -run TestGateCwdReleaseFindsTheRealTree` is GREEN — verified, 1.68s).

## Completed

### 1. The deletion claim is TRUE — verified by reachability, not by re-reading

The interesting question was never "were the two files deleted" (they are absent
from the tree); it was **"does a producer still exist that removes a worktree
through a helper the trigger set cannot see"** — i.e. was the obligation
RELOCATED into an unrecognised spelling rather than removed with its machinery.

Method (read-only, run over master `16ec3437`): a removal-reachability closure
over every production `.go` file of `internal/tools` — seed = the functions that
carry a `git worktree remove` verb pair (`finishCleanupWorktreeVerb,
finishCleanupWorktreeRemoveVerb`) or call the `reclaimGitRemove` helper; close
over **callers** ("a function that calls a remover is a remover"), then intersect
with every `tools.*` call the scanned trees make.

Result — the derived exported removal arms and the chain from a scanned caller:

| arm (derived) | chain into `internal/tools` | called from |
|---|---|---|
| `TaskFinishCleanupSessionID` | → `taskFinishCleanup` → `finishCleanupServerRecycle` (removes) | `cmd/agent/cli_resume.go`, `cmd/agent/cli_tasks.go`, `internal/agent/taskregister.go` |
| `ReclaimOwnFinishedWorktrees{,Within}` | → `reclaimSelfRelease` → `reclaimGitRemove` | `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go` |
| `ReclaimSelfOwnRegisteredWorktrees{,Within}` | → `reclaimSelfOwnRepo` → `reclaimGitRemove` | `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go` |
| `ReclaimDeadHolderWorktrees` | → `reclaimGitRemove` (**seed**) | `cmd/agent/cli_tasks.go` (merger dead-holder leg) |
| `MergedCleanupSweep` | **seed** (removes) | `cmd/agent/cli_tasks.go` |
| `MergerRepoSweep` | → `MergedCleanupSweep` | `cmd/agent/cli_tasks.go` (merger pass) |
| `WorktreeSweepRun` | → `worktreeSweepRun` → `worktreeSweepRunWith` (removes) | `cmd/agent/cli_tasks.go` (app + resident reap) |
| `TaskMerge` | → `taskMergeOne` → `mergeBuildGate` → `mergeBuildGateBuild` (**private scratch**, `-C` + `Dir`) | `cmd/agent/cli_tasks.go`, `cli_tasks_merge.go` |
| `TaskVerify` | → `taskVerifyGround` (**private scratch**) | `cmd/agent/cli_tasks.go` |
| `FinishCleanupPlan`, `TaskFinishCleanupPlan` | → `taskFinishCleanup` → `finishCleanupServerRecycle` (**planOnly**: removes nothing) | `cmd/agent/cli_tasks.go` |

No call in either scanned tree reaches the deleted files' machinery, and the
merger child spawn is gone (`mergerResolveProg` / `mergerVerifyProg` /
`resolveChild` / `verifyChild`: **0 production hits**). **Conclusion: a genuine
removal, not a relocation — the floor of 4 is honest** (re-measured, see §3).

### 2. The epic's "detection hole" warning was right — found, and closed

The trigger set was a **hand-written phrase list** (`"worktree", "remove"`,
`ReclaimOwnFinishedWorktrees`, `TaskFinishCleanupSessionID`, `reclaimGitRemove`)
that **no machine checked against the library's actual removal arms**. Three arms
were being CALLED from the scanned trees and were on that list nowhere:

* `internal/tools.WorktreeSweepRun` (`gopi task sweep-worktrees` **and** the
  resident reap leg the lifecycle tick fires),
* `internal/tools.MergedCleanupSweep` (the merged-cleanup pass),
* `internal/tools.ReclaimDeadHolderWorktrees` (the merger's dead-holder leg).

A fourth trigger did not even exist in code: `reclaimGitRemove` is a
**lowercase private helper**, so that entry matched only a comment.

Closed by making the arms **derived** instead of recalled:

1. `TestGateCwdReleaseCensusArmsAreListed` — walks `internal/tools`
   (`cwdReleaseToolsTree`) to a fixpoint and asserts every derived exported
   removal arm is either a trigger or recorded in `cwdReleaseArmsNotListed` with
   its reason (a new arm in neither = RED). It also fails a trigger that matches
   no derived arm (a stale trigger reading as coverage).
2. `TestGateCwdReleaseCensusFindsTheRealArms` — the read proof over the real
   tree (derives the arms the gate depends on; asserts `ReclaimHeldBranches`, a
   branch-ref-only helper, is NOT derived).
3. `cwdReleaseArmFloor = 10` (measured **13** walls of 94 files) — the census's
   own non-vacuity tripwire.
4. `cwdReleaseHasTrigger` now matches the **comments-stripped** source. The
   widened list made `cmd/agent/task_lifecycle_tick.go` (which *names*
   `serveMergedCleanupSweep` in prose and removes nothing) a producer — a trigger
   firing on prose demands a chdir of a file that cannot owe one. Measured red
   then green.

### 3. Acceptance evidence (all commands run on this branch)

| check | result |
|---|---|
| `go build ./... && go vet ./internal/standards/ ./cmd/agent/ ./internal/tools/` | **BUILD+VET OK** |
| `go test ./internal/standards/ -count=1` | **ok** (all 6 machine gates + this one) |
| `go test ./internal/standards/ -count=1 -run TestGateCwdRelease -v` | 4/4 **PASS** (`_RedProof`, `FindsTheRealTree`, `CensusArmsAreListed`, `CensusFindsTheRealArms`) |
| **producer count re-measured** (temporary `cwdReleaseReadFloor = 99`) | `found only **4** worktree-removing producer(s) across [cmd/agent internal/agent] (floor 99)` — the floor 4 is the true count |
| **arm count re-measured** (`cwdReleaseArmFloor = 99`) | `census derived only **13** exported removal arm(s) over 94 file(s) of internal/tools` |
| **delete-a-producer probe** (moved `cmd/agent/cli_tasks_worker.go` out of the tree) | **RED**: `found only 3 … (floor 4)` — the floor is not decoration; restored → green |
| **new-arm probe** (temp `tools.CwdReleaseProbeRemove` + a caller in `cmd/agent`) | census **RED** (`neither triggers on nor records as deliberately exempt`); after listing the arm as a trigger the gate reported the caller **RED**; after the caller called `worktreerelease.ReleaseReporting` → **green**. Probes deleted. |

The 4 producers (hand census, each with the trigger it matches and the release it
makes — the flag-text census is §3's `found only 4`):

| producer | trigger matched | release call |
|---|---|---|
| `cmd/agent/cli_resume.go` | `TaskFinishCleanupSessionID` (via `resumeFinishCleanup`) | `worktreerelease.ReleaseReporting` via `resumeWorktreeRelease` (:527) |
| `cmd/agent/cli_tasks.go` | `TaskFinishCleanupSessionID` + `ReclaimOwnFinishedWorktrees` + `ReclaimSelfOwnRegisteredWorktrees` + `MergedCleanupSweep` + `MergerRepoSweep` + `ReclaimDeadHolderWorktrees` + `WorktreeSweepRun` | `worktreerelease.ReleaseReporting` at :1878 (`taskRunReleaseOwnWorktrees`) and :2466 (`mergerPassOnce` pass boundary) |
| `cmd/agent/cli_tasks_worker.go` | `ReclaimOwnFinishedWorktrees` + `ReclaimSelfOwnRegisteredWorktrees` | `:183` `workerRoundOwnWorktrees` |
| `internal/agent/taskregister.go` | `TaskFinishCleanupSessionID` | `:364` `defer … ReleaseReporting` (every return path) |

## Not Done (honest)

* **The four producers' release calls were not runtime-verified** (no process was
  made to stand in a worktree and then be checked for the released inode). This
  task's acceptance is the gate's inventory/coverage, and the gate is a SOURCE
  gate by design; the runtime behaviour was the earlier epics' subject.
* `internal/agent/loop.go` still carries no release call — its historical
  chdir-at-exit funnel is gone, and the release moved to the END hook
  (`taskregister.go:364`, `defer` = every return path), which is a strictly
  larger surface. Noted, not changed here.
* `"worktree", "remove"` as a *source* trigger remains approximate: a producer
  calling `git` through a differently spelled invocation (e.g. a `-C`-split argv
  built elsewhere) would still not match. The **arm** half is now machine-derived
  (that is the hole this branch closed); the raw-verb half stays heuristic, and
  the census reports it as such.
* The `stylegate`/`standards` baseline was not touched (no new violations: the
  whole `internal/standards` package is green).

## Verification (Rule 5)

- Build: `go build ./...` + `go vet` → **OK**
- Tests: `go test ./internal/standards/ -count=1` → **ok**; `-run TestGateCwdRelease -v` → **4/4 PASS**; floors and probes as tabulated in §3
- Commit: branch tip of `feat/cwd-release-inventory-verified` (worktree branch, shared with the main repo; subject: `test(standards): verify the cwd-release producer inventory by census`)
- Recovery: `git show feat/cwd-release-inventory-verified:devdoc/worktree-cwd-release-inventory-verified.md`
