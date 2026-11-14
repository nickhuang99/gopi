<!-- doc-class: historical -->
# The stop-observable defect was ALREADY FIXED on master — independent re-derivation, plus one measured fact the record did not carry

**Task**: `01a0b400-2270-7e9d-8220-dd02221c8d3a` — *"`TestMergerRestartDoesNotTouchTheTakeoverInFlight` fails on master `9ce8e159` and on branch `fix/merger-loop-exit-bounded`: stop face waits for `serve.lock` that the takeover child holds on inherited fd 3"*.

**Outcome: no production change is needed. The defect is fixed on master.** This
report is the honest deliverable: the evidence that the row's premise
(`9ce8e159` = master) went stale mid-task, that the merged fix holds, and the
one measured fact the merged record does not contain.

## Completed

- Re-derived the defect from source and measurement, independently of the
  branches that had already worked it:
  - `mergerStopLiveLoop` on `9ce8e159` observed the lock going free
    (`mergerWaitLockFree`) as its "the loop has stopped" evidence;
  - a merger tick hands that very descriptor to the take-over it launches
    (`mergerTakeoverExtraFiles`, `ExtraFiles[0]` = `mergerLockInheritedFD` = 3),
    and an `flock` lives on the **open file description** — so the round, not the
    loop, is the holder for the round's whole life.
- Measured the base failure directly (`GOPI_TEST_BIN=$PWD/bin/gopi go test
  ./cmd/agent/ -run TestMergerRestartDoesNotTouchTheTakeoverInFlight -count=1`):
  `--- FAIL (86.72s)`, with `[merger] … stopped — pid 1213858 is gone and
  …/serve.lock is …` followed by the 60s-budget cleanup failure.
- Measured the mechanism in a 20ms-resolution timeline (temporary diagnostic
  `zz_diag_test.go`, deleted before this commit): the signalled loop is a
  **zombie at t+0.27s** while the round is still alive and the lock is still
  held — i.e. the retireable observable is wrong exactly as hypothesised.
- Found the premise stale: **master had advanced to `455615b4`**, which carries
  `f8a77e16 Merge branch 'fix/merger-stop-takeover-contract'` — the sibling
  dispatch (task `01a0b3c8`, records #3717/#3719/#3720/#3743) that implemented
  this contract and was merged while this run was working. `9ce8e159` is still
  the merge-base, i.e. the row was dispatched against a master that no longer
  existed.
- Verified the merged fix on that master, with a **separate detached worktree**
  (`~/workspace/gopi-master-verify` @ `455615b4`, never the main repo checkout,
  Rule 29):
  - `TestMergerRestartDoesNotTouchTheTakeoverInFlight` **PASS (2.38s)**;
  - `TestMergerStopContract_LockOutlivesItsCreator` **PASS**, …`_ProbeReportsFreeOnceTheLockIsReleased` **PASS**, …`_TheStopWaitsOnTheLoopNotOnTheLock` **PASS**;
  - repeated on a loaded host (loadavg 8.4): **PASS** again.
- Implemented the equivalent fix myself before discovering the merge
  (`fix/merger-stop-observes-pid`: stop observes the signalled pid, a bounded
  announcement-once ladder, and a diagnostic that distinguishes *"loop gone, a
  descendant holds the inherited lock"* from *"the loop has not reached a
  boundary"*; plus regression case `TestMergerStopObservesThePidNotTheInheritedLock`,
  measured green in 2.28s). **That work is superseded and is NOT the
  deliverable** — the identical diagnosis had already landed in a reviewed,
  merged form with its own devdoc (`devdoc/merger-stop-takeover-contract.md`),
  a broader test family, and a decision (REFUSE a restart while a round is in
  flight) that a second implementation would only duplicate. Its diff is kept
  as evidence at `evidence/duplicate-fix.diff` under the session directory.

## Not Done (honest)

- **No commit on master, no merge, no production diff in this session's
  deliverable.** Nothing needed changing: the failure was already repaired.
  Committing a duplicate fix would be a second cost (rules 23/26 read together)
  for zero product change.
- **`TestMergerRestartDoesNotTouchTheTakeoverInFlight` was rewritten as well as
  fixed** by the sibling branch: it now asserts a REFUSAL while a round is in
  flight (`code != 0`, the flag and the round's pid named, *no* `stop requested`
  line, the loop's membership in the lock preserved, the whole refusal bounded
  by `mreRefusalBound`) instead of asserting that a restart installs a fresh
  loop. That is a contract decision by the branch that owned this area, and this
  run did not re-litigate it — only verified it green.
- **One measured fact did not fit, and is recorded rather than smoothed over.**
  In the temporary diagnostics, the take-over stub was seen receiving **SIGTERM
  ~7.4s after the loop it was launched by had exited**, in a shape the control
  face cannot produce (the same stub survived an identical stop-only sequence
  that reached the same end state, and it survives a raw SIGTERM to the loop).
  `strace -f -e trace=kill,tgkill` over a full suite run shows the sender
  (`si_pid`) is a process **outside the traced tree**, spawned minutes after
  that stub started — i.e. an unrelated concurrent invocation on this host, not
  gopi. The `--stop` control face demonstrably does not signal a round: on the
  merged master, C9 passes in 2.38s *because* nothing is signalled, and the
  round is asserted alive afterwards.

## Verification

- [base] `git diff --stat 9ce8e159 455615b4 -- cmd/agent/merger_restart.go` →
  the stop half rewritten (`mergerWaitLoopGone`, `mergerLiveLockHolder`,
  `mergerRestartRoundInFlightFmt`), i.e. the fix is present on master.
- [fix] `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run
  'TestMergerRestartDoesNotTouchTheTakeoverInFlight|TestMergerStopContract'
  -count=1 -v` on `~/workspace/gopi-master-verify` @ `455615b4` (loadavg 8.43)
  → `--- PASS: TestMergerRestartDoesNotTouchTheTakeoverInFlight (2.38s)`,
  `--- PASS: TestMergerStopContract_LockOutlivesItsCreator (0.19s)`,
  `--- PASS: TestMergerStopContract_ProbeReportsFreeOnceTheLockIsReleased (0.13s)`,
  `--- PASS: TestMergerStopContract_TheStopWaitsOnTheLoopNotOnTheLock (5.58s)`,
  `ok github.com/nick/gopi/cmd/agent 8.642s`.
- [fix] the same case run twice more on a loaded host (loadavg 8.4) → `PASS`,
  `PASS`.
- [mechanism] 20ms timeline on the base: `loop=st=Z ppid=…` at t+270ms with
  `stub=st=S ppid=1` and `lock=[1656939]` still held.
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`.

## Evidence (self-filed)

- Base failure: `--- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight
  (86.72s)`, `merger_restart_e2e_test.go:682: cleanup stop exited 1: …
  pid 1229018 still holds …/serve.lock after 1m0s — the loop has not reached a
  tick boundary yet`.
- Merged fix present: `f8a77e16 Merge branch 'fix/merger-stop-takeover-contract'`
  is an ancestor of `master` = `455615b4`; `mergerStopDeliverTimeout` still exists
  but the stop now concluds via `mergerWaitLoopGone(pid, …)`.
- Rendered diagnostics: session dir `evidence/` — `duplicate-fix.diff`,
  `zzk-stub-sigterm.log`, `zzk-run3.log`, `strace-kill-trace.log`.
- `git status --porcelain` in the run's worktree: empty after the commit (the
  temporary `zz_diag_test.go` was deleted and the duplicate edit reverted first).
- Carried forward: master moved once more to `783186b0` after the verification;
  `git diff --stat 455615b4 783186b0 -- cmd/agent/merger_restart.go
  cmd/agent/merger_restart_e2e_test.go` prints **no rows** — the stop face and the
  acceptance case are byte-identical, so the measured result holds for the current
  tip too.

## Files

- No production file changed (nothing needed changing).
- `devdoc/merger-stop-observable-already-fixed.md` (this report), on branch
  `fix/merger-stop-observes-pid`.

## Rules this touched

- **Rule 6/13**: the pre-existing sibling branch was *read*, never committed onto
  (Rule 23) — the work was done on a fresh branch from `9ce8e159`, and compared
  against the claim branch by `git diff`/`git show` without borrowing a worktree.
- **Rule 29**: the merged master was verified in its own detached worktree
  (`~/workspace/gopi-master-verify`), never in a live task's checkout.
- **Rule 26**: the incomplete-fix family this row belongs to is closed by the
  dispatch already running, not by this run waiting on it — and the row's
  premise being stale is exactly the kind of fact a re-dispatch's briefing has to
  carry.
- **Rule 27**: the loadavg figures are *read* from `/proc/loadavg`, never
  generated.
