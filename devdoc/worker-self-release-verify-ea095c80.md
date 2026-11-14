<!-- doc-class: historical -->
# Evidence — epic "sandbox: producer releases its own worktree"

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch `fix/worker-self-release`; verification performed in a **frozen detached
worktree** at the branch tip commit **ea095c80** (`/tmp/gopi-sr-tip-4029889`),
because other sessions commit to and edit that branch concurrently.

Binary built from that commit: `/tmp/gopi-sr-tip-4029889/bin/gopi`
(`go build ./cmd/agent/`, exit 0; module cache used offline —
`GOPROXY=off GOMODCACHE=/home/user/go/pkg/mod`).

## 1. Implementation is committed (not in-flight)

| commit | content |
|---|---|
| `83e7c531` | `internal/tools/task_reclaim_self.go` — producer-side release (the epic's arm) |
| `48a2940e` / `be1176e6` | `scripts/acceptance-self-release.sh` + round-2 devdoc |
| `3a202d9f` | `fix(tools)`: malformed `~token` dropped by name, never statted as a cwd path |
| `ea095c80` | `cmd/agent/worktree_release_accept_test.go` — the rig the devdoc claimed |

Arm: `ReclaimOwnFinishedWorktrees` / `…Within`, candidates = rows `claimed_by`
**this** process, disposition decided by the shared `reclaimDisposition`
(clean dir + no live process inside + still on the row's branch), removal =
plain `git worktree remove` (branch untouched). Callers: worker-round defer
(`cmd/agent/cli_tasks_worker.go:483 workerRoundOwnWorktrees`) and the
`runTaskRunOnce` close-out (`cmd/agent/cli_tasks.go`).

## 2. Gates at the frozen tip

| check | command | result |
|---|---|---|
| unit suite | `go test ./internal/tools/ -run 'OwnFinished\|OwnWorktree\|SelfRelease\|ReclaimSelf\|Gate1SelfRelease\|ClaimHolder' -count=1` | **PASS** — 6/6 (`...FreesItsOwnDirectory`, `...RefusesALiveProcessInside`, `...RefusesADirtyWorktree`, `...IgnoresAnotherRowsDirectory`, `...RepairsAVanishedDirectory`, `...KillSwitchIsReportOnly`) |
| vet | `go vet ./internal/tools/ ./cmd/agent/` | exit 0 |
| own-release rig | `go test ./cmd/agent/ -run 'TestWorkerReleaseOwnWorktree_Acceptance\|TestOwnRelease_RefusesLiveSibling'` | **PASS** — real arm exercised: log line `deliverable.worktree backfilled to ".../wt-own" … at creation` |
| existing reclaimer pair | `-run 'Acceptance_DeadHolder\|Acceptance_LiveHolder'` | **PASS** (live holder still refused **by name**) |
| end-to-end (real `gopi worker --once`) | `/tmp/sr-arm-e2e.sh` (equivalent rig, row hand-stamped with `deliverable.worktree`) | **ARM-VERDICT: PASS** |

E2E transcript (`/tmp/sr-arm-e2e.out`, sandbox `/tmp/self-release-arm-BxQG`):

```
R0: gate [c] pre-run exit=1 (expect 1: occupied)
R1: row 01a09647-… deliverable = {"repo": …, "branch": "feat/self-release-e2e", "worktree": "/tmp/…/wt"}
R2: worker exit=0
R2: release lines:
   [worker] [own-release] claimed by this worker: peerbox:4063532
   worker: own-worktree-release: own worktree released: feat/self-release-e2e (01a09647-…) freed for merge; branch kept
R3: post-run worktree list: repo (master) + wt2 (peer) only
R3: released=yes branch-tip-kept=yes peer-survives=yes
R3: gate [c] post-run exit=0 (expect 0)
R3: row = failed claimed_by=peerbox:4063532
ARM-VERDICT: PASS
```

So: the producer's own worktree is removed, the **branch and its byte-identical
tip survive**, a live peer's directory survives, and `safe-merge` gate [c] flips
from refuse (1) to pass (0).

## 3. Defect found in the committed rig (root cause, not a param tweak)

`scripts/acceptance-self-release.sh` (as committed at the tip) **cannot** print
`VERDICT: PASS`: it submits the row through `gopi task submit`, so the row's
deliverable is `{"repo": …}` and the script never writes the `worktree` key
(its comment claims "the run's writeback stamps it exactly like a real task").

- The release candidate set is
  `… AND deliverable LIKE '%"worktree"%'` (`task_reclaim_self.go:234`), so such
  a row is **not a candidate**; observed: no release line, `released=NO`.
- The writeback overwrites the deliverable with `{"repo":"<repo>"}` (observed
  ledger rows: `/tmp/self-release-q5f6`, `/tmp/self-release-VV7e`), i.e. the
  hand-stamp in the script's own variant is erased too.
- Reproductions: `/tmp/sr-quick.out`, `/tmp/sr-clean.out`, `/tmp/sr-tip-sandbox.out`
  (all `released=NO`, `VERDICT: FAIL`) — while the identical flow with the
  `worktree` key in the row (section 2) releases correctly.

Earlier FAILs in this working area had **two** extra, unrelated causes, both
environmental and now identified: (a) the script drives a **full agent round**
(no `GOPI_TASK_CHILD_FAIL_TEST`) that idles on model capacity for many minutes
under the machine's load, and it was repeatedly SIGTERM'd → `worker exit=143`
close-out never ran; (b) ~25 concurrent `gopi worker` processes from other
sessions' copies of the same rig compete for the same claim queue.

**Verdict:** the epic's arm is implemented, committed and verified end-to-end;
the committed script needs one fix (keep the row's `worktree` in the deliverable,
e.g. stamp it *after* submit/writeback or run the child-fail path) before it can
be the standing acceptance gate.
