<!-- doc-class: historical -->
# Task Report: a live row's standing session is never swept away (2026-09-17 incident, task 01a0af50)

Branch `fix/live-row-session-veto` (worktree `/home/user/workspace/gopi-live-row-session-veto`).

## The incident, pinned by evidence

On 2026-09-17 the linked worktree `~/workspace/gopi-merger-takeover-seam` (branch
`fix/merger-takeover-reuse-scope-ladder` @ `b87c9008`) — a LIVE session's ground —
was removed mid-run, and the branch then became reachable from no ref. Forensic
facts, all re-measured on this host:

- The row is `01a0af50-5029-75ba-8bac-368ae86a3351`; its `deliverable.worktree`
  records `/home/user/workspace/gopi-merger-takeover-detach` (branch
  `fix/merger-takeover-scope-escape`) — the OTHER session's worktree. **Two
  sessions ran that one row concurrently while the ledger held one registration.**
- The tip is safe today only by rescue: merged as `4d054ae2` (21:01:33), tag
  `preserved/merger-takeover-ladder-20260917`. `git merge-base --is-ancestor
  b87c9008 master` → IN-MASTER.
- The removing arm is **not falsified by the logs**: `~/.gopi/logs/{merger,worker}.log`
  carry no `worktree remove` / branch-delete line naming either the seam path or the
  branch, and `~/.gopi/notify/task-events.jsonl` has exactly ONE 2026-09-17 line.
  The incident session's own trail records the observation but no culprit
  (`…the worktree vanished… likely by the merger's dead-holder reclaim … or by
  scratch GC`, `.raw.log` lines 4413-4443). **Honest answer: the arm is
  unidentified** — the logs for that window are gone, so this fix does not rest on
  naming it. It rests on the class of removal that COULD have done it.

## Root cause (5-why)

Symptom: a live worktree and then its branch disappeared under a running session.
Mechanism: every protection is keyed on the ROW's recorded fields — the recorded
worktree path, the recorded branch, the recorded runner identity (`proc_pid` +
start ticks). A row carries ONE of each. So a row running in two sessions is
protected only through the session the ledger happens to record: when that
runner exits, the recorded-runner veto goes quiet while the other session is still
standing in the directory. Compounding it, `scratch.HeldByProcesses` — the holder
veto — reads `cwd`/`fd`/`exe`/`maps`, and for a FOREIGN session those are blank
(measured: `readlink /proc/<foreign pid>/fd/*` → permission denied; note the
`cwd` READLINK itself is readable, which is why this fix uses it).
Norm: "removal needs positive evidence that nobody is standing here", but the
evidence set had no cross-session channel at all.

## Completed

1. **Cross-session liveness arm** (`internal/tools/worktree_sweep.go`) —
   `worktreeSweepSessionLiveEvidence` is the ONE implementation of "does this row
   have a run standing right now?" (Rule 14), over two channels readable across
   sessions: (①) the recorded runner is *positively* alive
   (`finishCleanupProcessHolderLive`), (②) a live process's working directory IS
   the row's own session container `~/.gopi/sessions/<task id>`
   (`processInsideSessionDir`, one bounded `/proc` scan). Wired into the sweep as
   rule `live-session` in BOTH removal rungs (①D dead registration, ③ stale
   registration), and into `worktreeSweepRowHoldsRegistration` so the deliverable
   ties now NAME the live session they are backed by.
2. **Reclaim Guard ⓪** (`internal/tools/task_reclaim.go`) — `reclaimLiveSessionRefusal`
   runs BEFORE the repo/dir probes on the directory-deleting arm
   (`reclaimDisposition`), refusing with the row id, the evidence and the session
   directory named. Same predicate as (1), so the two removal paths cannot drift.
3. **Orphan-ref guard** (`internal/tools/branch_delete_guard.go`) — every FORCE
   delete asks `branchDeleteOrphanRefusal(repo, branch)` first: if no ref other
   than the one being deleted has the tip on its ancestry, the delete is refused
   and named (with the archive recipe). Reachability, not equality — a merged or
   carried tip still reaps. Wired into the finish-cleanup chain's `-D` fallback
   and the adoption arm's force-prune.
4. **Regression tests** (`internal/tools/live_row_session_veto_test.go`) — three
   tests, each with a POSITIVE and a NEGATIVE pole, no device load (Rule 27).

## Not Done (honest)

- **The removing arm is NOT identified.** The logs for the 2026-09-17 window do
  not name it (see above). This fix closes the CLASS (a live row's second session
  is now visible to the removal arms); it does not claim to have caught the
  original caller.
- **`scratch.HeldByProcesses`' foreign-process blindness is not fixed** — this
  branch adds a second, cross-session-readable channel beside it rather than
  rewriting the holder scanner (that scanner also feeds scratch GC; rewriting it
  is its own task).
- **The recovery half (the incident's actual data loss) is untouched**: a tip that
  already became unreachable is still recovered by hand/tag only.
- `gofmt -l` reports three files the diff does NOT touch
  (`task_finish_premerge.go`, `task_staleness_guard_test.go`, `taskledger_cost_test.go`)
  — pre-existing on master, left alone deliberately.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0, no output).
- Tests: `go test ./internal/tools/ -run 'TestSweep…|TestReclaim…|TestBranchDeleteGuard…' -count=1` → **ok**
  (full-package run `go test ./internal/tools/ -count=1` → ok 48s for the sweep/reclaim/branch subset).
- `go test ./internal/standards/ -count=1` → **ok 27.751s** (Rule 16-21 gates green on this branch).
- Red proof (guard disabled, tests untouched) → all three FAIL:
  `TestSweepKeepsGroundOfALiveRowsSecondSession` → `reclaim (no task row owns it…)`;
  `TestReclaimRefusesADirectoryALiveRowsSessionStandsIn` → `did not refuse`;
  `TestBranchDeleteGuardRefusesAnOrphaningDelete` → `NOT refused`.
  Restored → all three PASS.
- Commits: `feaa8c6b` (the fix) + `7eb0525c` (the fixture's cwd flip → `t.Chdir`).

### `internal/stylegate` is RED on master, not on this branch (measured, not assumed)

`go test ./internal/stylegate/ -count=1` exits 1 **on master too**, with the same
rule counts — 5 rules over their ceilings (`test-skip` 1/0, `errorf-verb` 8/7,
`ignored-error` 11/0 production and 163-ceiling test, `test-sleep` 5/0). Measured
by running that package in a **detached worktree of master**
(`git worktree add --detach … master`; `194` test ignored-error hits vs `195` on
this branch before the second commit, `194` after it) and diffing the hit lists
file-by-file: **the only difference this branch ever contributed was its own
fixture's `_ = os.Chdir(prev)`**, which `7eb0525c` removed by using `t.Chdir`.
The two `worktree_sweep.go` `%v`-wrap hits appear at different line numbers on
each side (1711 on master, 1880 here) — same single site, shifted by this
branch's insertions, carrying its `// no-wrap:` reason on both. A live ledger row
`01a0b165` ("internal/standards Rule 16 gate is RED at base: 2 fmt.Errorf sites…",
`in_progress` at 13:07:07 on 2026-09-18) and the sweep's own ceiling comment already
own master's redness; it is not this branch's to fix, and this branch leaves it no
worse.

### Three more master-side FAILs, all reproduced on the same detached master worktree

- `internal/tools` `TestMergeBuildGate_GoCacheLeavesNoScratchCache` — FAILS on
  master too (same assertion: "the gate's cache … is inside the scratch tree").
- `cmd/agent` — `TestTaskRunSingleRowE2E`, `TestTaskRunSurvivesClosedStdoutPipe`,
  `TestTaskRunChildReceivesSummaryAndPayloadE2E`, `TestTaskRunNormalExitSingleRowE2E`,
  `TestTaskRunChildNonZeroExitClosesWrapperRowE2E` all FAIL on master too
  (`task_submit: repo "…/suite-cmd-agent-run-…/gopi-repo-ctx-…" is not a git work
  tree`), and they FAIL in **0.01–0.10s** — an environment assertion, not a
  timeout. They are the reason the branch-scoped smoke exits 3 at all.
- The smoke run for this diff **timed out** (`exit=3 outcome=timeout`, 25m budget,
  elapsed 1523s, load_start 14.88 / load_end 17.64 on 4 cores) rather than
  reporting a failing test; the stream's own slowest entries are the ledger's
  device cost (`TestManyFreshDBsNoHang` 62.0s, `TestMergeUnionGate_RefusesTheIncidentShape`
  47.6s). The relevant half — the sweep/reclaim/branch-delete tests — was then run
  directly and is green: `go test ./internal/tools/ -count=1 -run
  'WorktreeSweep|SweepKeeps|…|Reclaim|BranchDelete|MergedCleanupOrphan|DeadHolder|LiveClaim'`
  → `ok 324.480s` (WALL 332.07s).

### One FAIL was THIS branch's, and the gate caught it

`TestWorktreeSweepFindingRootGate` — "the ladder's arm set changed (12 cited + 2
post-judge != 15 defined) — cite the new arm above". That is the arm-citation
tripwire working exactly as designed: `worktreeRuleSessionLive` was added to the
ladder without a citing case. Fixed in `42068860`: a new case drives the
concurrent-session shape with a REAL live process (the mock-daemon stand-in,
reaped by its own reaper) plus a cwd into the row's session container, so the
liveness half cannot pass vacuously.

## Evidence (self-filed)

- `/proc` channel, measured on THIS host:
  `readlink /proc/786339/cwd` → `/home/user/workspace/gopi-liveworktree-veto`…
  (foreign same-uid worker, readlink SUCCEEDS);
  `readlink /proc/786339/fd/0` → permission denied for the same pid
  (`kernel.yama.ptrace_scope=1`). The guard is built on the surviving channel.
- Incident row type: `sqlite3 ~/.gopi/memory.db "select task_id,status,claimed_by,
  deliverable from tasks where deliverable like '%takeover-seam%'…"` → one row,
  `01a0af50-5029-75ba-8bac-368ae86a3351`, `completed`, `peerbox:3083931`,
  `deliverable.worktree=/home/user/workspace/gopi-merger-takeover-detach`.
- Reachability of the rescued tip: `git merge-base --is-ancestor b87c9008 master`
  → exit 0 (IN-MASTER); `git log --oneline -1 4d054ae2` → `Merge branch
  'fix/merger-takeover-reuse-scope-ladder'`.
- Guard oracle (before the namespace fix, temp test, since deleted):
  `for-each-ref: "refs/heads/fix/orphan\nrefs/heads/master"`, `refusal: ""` — i.e.
  the guard could not refuse because `refs/heads/` had been derived from the
  porcelain `"branch "` prefix; after deriving it from `refsHeadsPrefix` the same
  probe printed
  `its tip is reachable from no other ref (4217bb35bf7c) — … archive it (git tag archive/fix/orphan fix/orphan) …`.
