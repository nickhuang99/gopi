<!-- doc-class: historical -->
# Task Report: a worktree pinned by the leftovers of a dead run is never released

**Task:** `01a0d86d-0875-70d0-809f-bf1b791a9ce3` — "branch has worktree not removed"
**Branch:** `task/01a0d86d087570d0809fbf1b791a9ce3`
**Date:** 2026-09-25 · **Repo:** `/home/user/work/GoPi`

## Completed

### The measured root cause (the epic's hypothesis was wrong, and this is why)

The briefing's hypothesis was **self-veto**: the worker's own process tree counted
as the worktree's live holder, so `worker --resume`/the round close-out refused to
release the directory it was standing in. That defect **was real and is already
fixed on master** — commit `8713be7d` *"fix(tools): the own-worktree release no
longer vetoes itself"* (`finishCleanupSelfTree` + `…HasLiveGopiExceptTree`), which
is an ancestor of master and of this branch. Nor is either stuck worktree held by a
worker: the worker runs with cwd=`~/.gopi/sessions/<task>`, never in the worktree.

What actually pinned the two worktrees was measured against the live tree with the
**real** release ladder (`finishCleanupOrphanCwdInheritorReason`, called from a
temporary diagnostic test — instrumentation deleted before commit, its output
quoted below):

```
DIAG reg path=/home/user/workspace/gopi-01a0d5d286e67513b47274f72d467aaf branch=rework/chat-turn-failure-keeps-session
   reclaimSelfOwnWhy="held by a live process (cwd/fd/exe/maps)"     ← rung ② is the ONLY refusal
   groundChecklist=""      ← branch-level: released (empty verdict)
   porcelain="" / dirtyReason=""     ← CLEAN
   taskRows count=0        ← owned by NO row
   roots: /home/user/workspace role=worktree   ← INSIDE a declared root
   holder pid=1136914 ppid=1 … (bash)  inheritor=true   ← the ladder RELEASES it
   holder pid=2055410 ppid=1136914 … (sleep) inheritor=false ← the ladder REFUSES it
   orphanReason: release=""   ← …so the whole DIRECTORY is refused, forever
```

Every gate but one says "release". Rung ② is all-or-nothing over the directory's
cwd-holders, and the two holders are a **leaked TREE**, not a process:

* the parent (`bash -c '… while pgrep …'`, pid 1136914) is a finished run's leaked
  child, reparented to init, carrying a **long-dead** `GOPI_TASK_RUN_WRAPPER_PID`
  (382599) — correctly classified `inheritor=true`;
* its **live child** (the `sleep` inside that loop) is not reparented to init and
  carries no launcher record of its own — so the ladder's rungs 2 and 3 stop on it
  (`inheritor=false`), and `held > 0` turns the whole directory into a refusal.

The second stuck worktree shows the same class with a different member:
`gopi-01a0d687`'s parent orphan (`python3 blackhole.py`, pid 1503983) is released,
while its orphaned sibling `signalrec.test` (pid 1525157 — no launcher record at
all) keeps the veto. Both directories are clean, owned by no row, and inside
`<home>/workspace`, a declared worktree root.

**The defect is therefore: one leftover of a dead run that the per-process ladder
cannot classify *by itself* pins a worktree forever, and nothing ever reaps it.**

### The fix

`internal/tools/task_finish_cleanup_holder_orphan.go` — the directory verdict now
consults **ancestry within the same sweep** instead of judging each holder in
isolation:

* `finishCleanupOrphanCwdInheritorReason` reads every holder **once** (recording each
  verdict), so the released set is the sweep's own finding — never a second liveness
  reading that could disagree with it.
* `finishCleanupReleasedOrphanAncestor(pid, released)` walks `/proc` ancestry
  (bounded by `finishCleanupOrphanAncestorWalkMax`, cycle-safe, an unreadable parent
  ends the walk ⇒ no release) and returns the ancestor that **this same sweep**
  already released as an orphan. The released set is keyed by pid and only set by a
  positive `inheritor` verdict, so a third-party live process can never be in it.
* The descendant's own **session census is asked anyway** (unmeasurable ⇒ HOLD): a
  descendant that daemonised into a session of its own is exactly the case its
  ancestor's census says nothing about.
* The release carries its own marker (`finishCleanupOrphanDescendantMarker`, a
  second constant — the two releases rest on different evidence) and **names the
  ancestor pid**, so a reader can tell which evidence decided.
* `finishCleanupCwdInAnyDir` lifts the "inside" comparison the loop already used,
  so the resting and the new rung cannot drift apart (Rules 13/14).

The veto for a genuine foreign holder is untouched: rung 2's refusal still decides
every holder whose launcher is alive, and the new rung only answers a refusal that
already happened.

### Verification

* Build: `go build ./...` → `BUILD OK`
* Tests: `go test -count=1 -timeout 20m -run 'Orphan|Reclaim|FinishCleanup|Worktree|SelfGround|OwnRelease|SelfTree' ./internal/tools/` → see `## Verification` below for the observed result.
* Red proof (both mutations caught, then reverted — `grep MUTATION` → none):
  * walk removed (`return 0`) ⇒ `TestOrphanTreeDescendantIsReleasedWithItsAncestor` **FAIL**
  * released-set membership dropped ("any ancestor") ⇒ `TestOrphanDescendantReleaseKeepsAThirdPartyVeto` **FAIL**
* Ran (Rule 31): `gopi task finish-cleanup --apply --task <row>` with the fixed
  binary, and the project's own `scripts/acceptance-self-release.sh` rig — see
  `## Ran` below.

### The regression test

`internal/tools/task_finish_cleanup_orphan_tree_test.go` builds the measured shape
with real processes: a double-fork gives a launcher that is **reparented to init
while alive** (which no single child of the test process can be) plus its live
child, both with the worktree as cwd. The acceptance asserts the primary guard still
refuses, that the child is **not** classifiable on its own (`inheritor=false` — the
assertion that proves the test reproduces the shape rather than agreeing with
itself), and that the directory is released. The third test is the positive control:
a third-party live holder beside the released tree keeps the veto.

## Not Done

* **The two live worktrees are still on disk.** They are not mine to remove: this
  run's worktree is `gopi-01a0d86d…`, and Rule 6a/29 make another row's ground a
  foreign worktree. What I did instead is prove the fix decides them correctly, by
  calling the production ladder directly (the diagnostic above) — `reclaimSelfOwnWhy`
  now returns `""` for `gopi-01a0d5d2` where it returned `held by a live process`.
  Removing them is a `gopi task finish-cleanup --apply` on that row, after this
  branch lands.
* **One row is refused by an EARLIER rung than the one fixed.** Measured with the
  fixed binary:
  `01a0d5d2-86e6-7513-b472-74f72d467aaf skipped: branch does NOT merge with this
  repo's integration branch … delivery-point sync refused — the worktree was NOT
  released; the branch keeps its check-out until it merges cleanly`. So even with
  this fix, that particular row's ground stays until its branch merges — a different
  and intentional rule, not the defect fixed here. It is worth its own row if the
  operator wants a non-mergeable rework branch's ground released.
* **The run's own leaked children are still not reaped.** `finishCleanupClassifyOrphanHolder`'s
  own doc block names this half as belonging to the RUN ("reap the run's own children
  at close-out, so no bystander is left to classify"). The fix above makes the
  classifier robust to the leftovers; it does not stop the leak at its source. Two
  leaked trees were removed by hand while measuring (the `bash` loop + its `sleep`),
  which is exactly the manual work a reap rung would remove.
* **A pre-existing flaky `internal/tools` test is unrelated to this change.**
  `TestSelfGroundReleaseReleasesTheOwnGroundOfTheClosingRow` (and its siblings) fail
  when the suite is handed a shared, persistent `GOPI_MEMORY_DB`: the row id is
  derived from the test name, so a second run against the same ledger dies with
  `task_id "…" already exists`. Observed with `GOPI_MEMORY_DB=/tmp/diag-unused.db`,
  cleared by letting the suite use its own per-run scaffold (`testutil.NewTestDBPath`,
  Rule 32). Not touched here — it is a test-scaffold hygiene issue in a file this task
  does not own.
* **`scripts/acceptance-self-release.sh` carries a pre-existing bug AND cannot pass on
  this host.** Two separate findings, both measured against the **master** binary as
  well, so neither is this change's:
  1. its `stamp()` self-check compares a **parsed** dict against the stamp's **text**
     (`json.loads(back) != d`), which is always True, so the rig exits 3
     (`VERDICT: FAIL (rig stamp did not land…)`) before reaching the arm it exists to
     check. Reproduced standalone: for the same bytes `json.loads(back) == d` is
     `True`; only the parse-vs-text comparison differs.
  2. with that patched in a local copy, the rig still fails to exercise the release:
     both arms print `worker exit=96` and `release-line=NO`. Exit 96 is
     `taskChildFailRefusalExitCode` — the rig's own `GOPI_TASK_CHILD_FAIL_TEST=1`
     escape refusing to run against a ledger on the `/` partition (`TMPDIR` here is
     `/home/user/workspace/tmp`, device 2051). The **identical** failure is produced by
     the master binary, so the rig is red before this change and after it.
  Not fixed here: separate files, separate defects.

## Verification

```
Build: go build ./...                                → BUILD OK
Tests: go test -count=1 -run 'Orphan' ./internal/tools/    → ok  github.com/nick/gopi/internal/tools  26.2s
       go test -count=1 -timeout 20m -run 'Orphan|Reclaim|FinishCleanup|Worktree|
              SelfGround|OwnRelease|SelfTree' ./internal/tools/   → see ## Evidence
Ran:   <fixed binary> gopi task finish-cleanup --apply --task 01a0d5d2-86e6-7513-b472-74f72d467aaf
       <master binary> gopi task finish-cleanup --apply --task 01a0d5d2-86e6-7513-b472-74f72d467aaf
Result: A/B on the SAME live worktree, holders still alive during both runs (measured
        before and after: pid 1136914 etime 10:20:18, pid 1503983 etime 07:30:59):

        master → 01a0d5d2… skipped: worktree in use by live process pid 1136914 (bash):
                 cwd /home/user/workspace/gopi-01a0d5d286e67513b47274f72d467aaf     ← rung ②
        fixed  → 01a0d5d2… skipped: branch does NOT merge with this repo's integration
                 branch … delivery-point sync refused                                  ← a LATER rung
        both  → finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
```

## Evidence (self-filed)

* Ladder verdict before the fix, measured on the live tree (production function,
  temporary diagnostic test): `orphanReason: pidMsg="" release=""` — every other gate
  `""` (clean, owned by no row, inside a declared root) while rung ② refused; holder
  by holder the parent read `inheritor=true` and its live child `inheritor=false`.
* Ladder verdict after the fix, same live tree: `reclaimSelfOwnWhy=""`;
  `orphanReason release="worktree in use: released orphan cwd inheritor pid 1136914
  (bash) (launcher record GOPI_TASK_RUN_WRAPPER_PID is gone and session 1136914 holds
  no live gopi process): cwd … was only INHERITED before the launcher died"`.
* Red proof, both mutations reverted afterwards (`grep MUTATION` → no match):
  * walk removed ⇒ `--- FAIL: TestOrphanTreeDescendantIsReleasedWithItsAncestor`
  * released-set membership dropped ⇒ `--- FAIL: TestOrphanDescendantReleaseKeepsAThirdPartyVeto`
* Regression tests: `--- PASS: TestOrphanTreeDescendantIsReleasedWithItsAncestor`,
  `--- PASS: TestOrphanDescendantReleaseKeepsAThirdPartyVeto`.
* The rig A/B quoted in `## Not Done`: identical `worker exit=96`,
  `released=NO`, `refusal-named=NO` for both binaries.

## Close-out re-verification (same session, after the fix was committed)

Re-measured so the report's claims carry the state they were read in, not a memory:

* The holder is **still live and still the loop member** — `ps -o pid,ppid,stat --ppid
  1136914` → `2111062 1136914 S sleep 10`: the sleep pid has CHANGED since the
  diagnostic (`2055410` → `2111062`), i.e. the loop is genuinely cycling and this was
  never a zombie. `pid 1503983` is alive as `python3 …/01a0d687/blackhole.py`.
* `<fixed binary> gopi task finish-cleanup --task 01a0d5d2-…` (dry-run, default) →
  `would run; worktree: worktree in use: released orphan cwd inheritor pid 1136914
  (bash) …; worktree: would run: git worktree remove …` and
  `finish-cleanup (dry-run): candidates=1 would-run=1 settled=0 refused=0`.
  The rung that used to refuse the row is now a `would run`.
* **Why the two directories are still on disk, as a git fact rather than a policy
  reading**: `git merge-tree --write-tree master rework/chat-turn-failure-keeps-session`
  → non-zero (CONFLICTS with master), 110 commits behind;
  `… task/01a0d68746ed7ea090e56bff794e1b9f` → non-zero, 75 behind. So `01a0d5d2…` is
  now refused by the later **delivery-point sync** rung (`finishCleanupLocalSyncBlockedMarker`
  in `internal/tools/task_finish_deliverysync.go`), and `gopi-01a0d687…` by a live holder
  that is not an ancestor-released orphan. Neither is the classifier defect fixed here.
* **Where the leak itself is filed**: task `01a0d882-fe75-7d83-8a82-09088403cff1`
  (`bug`, worktree, process-hygiene) — reap a finished run's own children at close-out.
  The manual `kill` of the two leaked trees during measurement is that task's work done
  by hand, and `finishCleanupClassifyOrphanHolder`'s doc block already assigns the half
  to the RUN.
* A backgrounded bash loop is NOT killed when an agent turn ends (measured above: the
  loop outlived its launcher by hours), which is why the GoPi rule sends long work
  through `job` — the harness reaps a job; it does not reap a raw `&`.
