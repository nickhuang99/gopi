<!-- doc-class: historical -->
# Task Report: worktree-cwd release coverage (branch `fix/cwd-release-coverage`)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: *The worktree-release cwd action exists and is correct but has exactly ONE
caller; every other process path that can stand inside a task worktree never
releases its cwd.*

## Completed

### 1. Exhaustive inventory (commands + output)

Two greps over the whole production tree (`grep -rn … cmd internal | grep -v _test.go`),
full output archived at `~/workspace/cwd-demo/inventory.txt`:

* `os.Chdir` / `worktreerelease.` / `ProcessRootPath` → 8 production chdir sites
  (below).
* `"worktree", "remove"` / `reclaimGitRemove` / `ReclaimOwnFinishedWorktrees*`
  / `TaskFinishCleanupSessionID` → the worktree-removing seams.

**The chdir sites, classified** (this classification IS the deliverable's
justification for which paths had to be wired):

| site | process | verdict |
|---|---|---|
| `internal/worktreerelease/release.go:61` | any | **THE** implementation (the only `os.Chdir` a producer uses) |
| `cmd/agent/cli_tasks.go:1858` | `task run` wrapper | the pre-existing caller, re-pointed at the shared action |
| `cmd/agent/cli_tasks_worker.go:165` | worker round loop | **was uncovered** → wired |
| `cmd/agent/cli_tasks.go:2384` | merger pass | **was uncovered** → wired |
| `cmd/agent/merge_resolve_child.go:142` | merger resolve teardown (removes a worktree) | **was uncovered** → wired |
| `cmd/agent/merger_verify_child.go:563` | merger verify/baseline teardown (removes a worktree) | **was uncovered** → wired |
| `internal/agent/taskregister.go:405` | executor child close-out | **was uncovered** → wired |
| `internal/agent/loop.go:2771` | executor child exit funnel | **was uncovered** → wired |
| `cmd/agent/cli_resume.go:433,443` | resumed session (both exit paths) | **was uncovered** → wired |
| `cmd/agent/cli_resume.go:475` | resume **enters** a worktree | deliberate entry, not a release — out of scope |
| `cmd/agent/task_session_cwd.go:193` | workdir restore (round undo) | a *different* obligation (restore the round origin); a chdir, but not a release — see Not Done |
| `internal/tools/worker_output.go:189` | library arm that moves the **caller** | deliberately not wired (see below) |

**The worktree-removing producers** (what the gate keys on):
`cmd/agent/cli_tasks.go` (finish-cleanup + own-reclaim),
`cmd/agent/cli_tasks_worker.go` (own-reclaim),
`cmd/agent/merge_resolve_child.go` (`git worktree remove --force`),
`cmd/agent/merger_verify_child.go` (`git worktree remove --force`),
`internal/agent/taskregister.go` (finish-cleanup chain),
and in `internal/tools`: `task_reclaim.go:216`, `worktree_sweep.go:985`,
`task_reclaim_self.go:162` — all three are **library** removals that execute in
the *caller's* process over **other rows** (the ownership contract explicitly
forbids chdir'ing the caller there), so the gate excludes that tree by design
and says so in its header.

### 2. One shared release action, called on every producer path

New leaf package **`internal/worktreerelease`** (zero project imports — the
`internal/toolnames` precedent, Rules 13/14): `Release()`, `ReleaseReporting(w, prog)`,
`ProcessRootPath = "/"`.

* It could not live in `internal/tools`: `internal/agent` imports `tools`, so a
  release function there could never be called by the executor — that import
  direction is precisely why the duty had been duplicated per call site before.
* `tools.ProcessRootPath` became an **alias** — and this is where the first
  version of this change went WRONG (see §3b): the name spelled TWO facts at
  once, so the alias is now split into `tools.ProcRootPath = procRootName`
  (procfs, read by `cli_worker_status.go`) and
  `tools.CwdReleasePath = worktreerelease.ProcessRootPath` (the release
  destination).
* The wrapper's former private `disownProcessCwd()` was **deleted** — its body
  is now the shared action.

Wired paths (7 seams, 9 call sites): task-run close-out, worker round close-out
(*after* the own-worktree reclaim, which needs a usable cwd), merger pass
boundary, merger resolve teardown (before the removal), merger verify/baseline
teardown (before the removal), executor child close-out (right after the
finish-cleanup chain that may have removed the directory), executor exit funnel,
resume success + failure. Every site is best-effort and reported (Rule 15): the
release never alters control flow or a task outcome.

### 3. The mechanical gate

`internal/standards/cwd_release_gate_test.go` (the existing gate machinery's
shape: one pure predicate, a red-proof test, an exhaustive scan, a read floor, a
reviewed-exception marker `// cwd-release-ok:`):

* **Predicate** `cwdReleaseViolation(src)` = a production file in a
  process-owning tree (`cmd/agent`, `internal/agent`) that carries a
  worktree-removal trigger **and** never calls `worktreerelease.Release*`.
* **Read floor** (`cwdReleaseReadFloor`, measured at the gate's landing: 5;
  **4** since master 69975030 — see §"Inventory re-established" below): if the
  trigger set stops matching the tree the gate fails loudly instead of reading
  green over an empty set.
* **Measured precision fix #1 (during this task):** the first marker was the
  package qualifier `worktreerelease.`, and deleting the release *call* from
  `merger_verify_child.go` left the gate **GREEN** — the file's own `import`
  line still carried the qualifier. An unused import is not a release. The
  marker is now `worktreerelease.Release` (the call), and the import-only shape
  became a negative probe in the red-proof test. Re-measured afterwards: RED.
* **Measured precision fix #2 (same session, same method):** with the marker
  being the call, a producer's *explanatory comment* could still satisfy it —
  `internal/agent/taskregister.go`'s own comment spells
  `worktreerelease.ReleaseReporting(a.taskLogWriter(), agentProgName)` in prose.
  The predicate now matches the release call in **code only**: comments are
  blanked by a real `go/scanner` pass. A hand-rolled `//`/`/*` stripper was
  tried first and is kept as a *negative probe* in the red-proof test — it read
  the `feat/*` inside a STRING literal in `cli_tasks.go` as a block-comment
  opener and ate the rest of the file, turning a compliant producer into a
  FALSE POSITIVE. Both failure modes are now pinned by probes (import-only,
  comment-only, prose-`feat/*`).

### 4. Two defects found by RUNNING the branch's own suite (red proof, then fix)

Wiring the release into the producer seams turned the branch's own suite red, and
both causes are recorded because both were invisible to the unit tests:

1. **The release hijacked the TEST HARNESS's cwd.** Production close-out seams
   are exercised *in-process* by `go test`, so the new calls chdir'd the whole
   `cmd/agent` suite to `/` — **30 failures**, all cwd-relative
   (`read fixture ../../tests/fixtures/log-stats-raw/…: no such file or
   directory`). This is the 5-why the fix rests on: the release's contract is
   *"this process is DONE with the directory and may die"*; a test binary is
   neither — its cwd is the harness's shared working context, and every sibling
   test in the run resolves relative paths against it. That is not a release, it
   is a hijack. **Fix:** `ReleaseReporting` skips inside a compiled Go test
   binary (executable suffix `TestProcessExecutableSuffix`); `Release()` stays
   unconditional so the acceptance rig (a *re-exec'd real holder*) still measures
   the release. Both directions are unit-pinned.
2. **A duplicate release call drifted a line-anchored doc.** The extra call in
   `internal/agent/loop.go` was redundant — `loop.go:2747` already calls
   `finishSessionTaskRecorded`, which releases — and adding 7 lines there moved
   every following line number, so `docanchor_test` caught
   `devdoc/agent-five-organs-ontology.md`'s anchors pointing at blank lines.
   **Fix:** the duplicate call and the `agentProgName` constant are gone from
   `loop.go`; the agent package releases at ONE seam, in `taskregister.go`.

This is the case for running the project's own gate rather than the new tests
alone: the new tests were green while the suite was 30 tests red.

### 4b. The third defect — an alias between two DIFFERENT facts (found only by
running the suite against the untouched master baseline)

`go test ./...` on the branch was compared, failure-for-failure, with the same
command on a pristine master checkout (`~/workspace/gopi-cwd-baseline`,
`10f96bc0`). That comparison — *not* the branch's own red — is what exposed it:

```
--- FAIL: TestWorkerDaemon_StatusReportsTheLiveDaemon (0.99s)
    status must name the live daemon's state record …/worker-state-2061539 for
    pid 2061539, got: gopi worker: no workers running …
    worker-state-2061539: pid 2061539: cannot attest (no readable comm entry)
--- FAIL: TestWorkerStatusRecycledPidIsResidueNotAWorker (0.00s)
    residue entry must say the process is not gopi: [… cannot attest …]
```

Both FAIL on the branch and PASS on the baseline, and in isolation on the
branch too (order/state, not a hard break — which is why my first grouped run
missed them and the full-suite A/B caught them).

**5-why.** (1) `gopi worker --status` reports a live daemon as unattestable.
(2) The attestation reads `<procfs>/<pid>/comm` and got an error → "cannot
attest". (3) `<procfs>` resolved to `/`, so the path was `/<pid>/comm`. (4) The
reader is `procnameRoot() → tools.ProcessRootPath`. (5) **`tools.ProcessRootPath`
had been defined as `procRootName = "/proc"` — it was PROCFS knowledge — and the
aliasing step pointed that same name at the RELEASE destination `"/"`.** The
name spelled two unrelated facts, so the alias (correct between two names for
ONE thing, Rules 13/14) silently re-pointed every procfs read in the tree.

**Root-cause fix, not a parameter tweak:** split the conflation into two names,
each meaning exactly one thing —
`tools.ProcRootPath = procRootName` (`/proc`, procfs readers) and
`tools.CwdReleasePath = worktreerelease.ProcessRootPath` (`/`, the release
destination); readers updated (`cli_worker_status.go:596`, its test's procfs
skip). Guard: `internal/tools/procroot_release_path_test.go` asserts the procfs
root really carries a live `<pid>/comm` (by USE, not by comparing two spellings),
that the release destination is the leaf package's single value, and that the
two are **not** the same path — so a future consolidation cannot re-merge them.
RED-proofed: re-aliasing `ProcRootPath` to the release value turns the new guard
red **and** reproduces the exact two failures above.

### 5. Real verification (see the Verification block)

A production-shaped holder → worktree removal, RED (pinned) vs GREEN (free),
plus the acceptance rig and the gate red-proof.

## Files

```
internal/worktreerelease/release.go                    (new — the one implementation)
internal/standards/cwd_release_gate_test.go            (new — the mechanical gate)
cmd/agent/cwd_release_rig_test.go                      (new — acceptance rig, RED+GREEN)
cmd/agent/cli_tasks.go                                 (wrapper + merger pass)
cmd/agent/cli_tasks_worker.go                          (worker round)
cmd/agent/cli_resume.go                                (resume, both exits)
cmd/agent/merge_resolve_child.go                       (resolve teardown)
cmd/agent/merger_verify_child.go                       (verify teardown)
internal/agent/loop.go                                 (executor exit funnel)
internal/agent/taskregister.go                         (executor close-out)
internal/tools/task_reclaim_self.go                    (ProcRootPath / CwdReleasePath — the split)
internal/tools/procroot_release_path_test.go           (new — the two facts stay distinct)
internal/worktreerelease/release_test.go               (new — the action's own contract)
devdoc/worktree-cwd-release-coverage.md                (this report)
devdoc/worktree-cwd-release-coverage/run.sh            (the RED/GREEN demo, replayable)
devdoc/worktree-cwd-release-coverage/run.log           (its recorded output)
devdoc/worktree-cwd-release-coverage/inventory.txt     (the raw grep inventory)
```

## Verification

```
# build
$ go build ./...                                → BUILD OK (exit 0)

# the release action is THE only chdir a producer performs
$ grep -rn "os.Chdir" --include=*.go cmd internal | grep -v _test.go
internal/worktreerelease/release.go:61:  os.Chdir(ProcessRootPath)      ← the ONE release
cmd/agent/task_session_cwd.go:193                                  ← round-origin RESTORE (different duty)
cmd/agent/cli_resume.go:475                                        ← ENTERS a worktree (not a release)
internal/tools/worker_output.go:189                                ← library arm moving its CALLER

# gate — red proof of the unfixed shape
$ go test -run 'TestGateCwdRelease' -v ./internal/standards/
--- PASS: TestGateCwdRelease_RedProof
--- PASS: TestGateCwdReleaseFindsTheRealTree

# gate demonstrably RED on the unfixed shape (release call deleted, import kept)
$ python3 -c "…delete the worktreerelease.ReleaseReporting line in merger_verify_child.go…"
$ go test -run 'TestGateCwdReleaseFindsTheRealTree' ./internal/standards/
--- FAIL: TestGateCwdReleaseFindsTheRealTree
    cwd_release_gate_test.go:271: cmd/agent/merger_verify_child.go removes a linked worktree
    but never releases this process's cwd — it will pin the removed directory's inode …
FAIL  github.com/nick/gopi/internal/standards  2.814s
$ cp <backup> cmd/agent/merger_verify_child.go   # restored
$ go test -run 'TestGateCwdRelease' ./internal/standards/   → ok

# acceptance rig (real linked worktree, real `git worktree remove`, re-exec'd holder)
$ go test -run 'TestCwdReleaseRig' -v ./cmd/agent/
--- PASS: TestCwdReleaseRig
    --- PASS: red_unreleased_holder_pins_the_removed_tree   (holder 1778957 cwd: "…/wt-cwd-release (deleted)")
    --- PASS: green_released_holder_lets_the_tree_go        (holder 1779050 cwd: "/")

# REAL COMMANDS, end to end — replayable IN-TREE (script + recorded log committed):
$ bash devdoc/worktree-cwd-release-coverage/run.sh
scratch root device=2052   / device=2051        ← the scratch tree is OFF the / partition (Rule 22)
=============== CASE RED: a holder that NEVER releases its cwd ===============
holder pid         : 1878427
cwd before removal : /home/user/workspace/cwd-demo/scratch/red/wt-demo
worktree removal   : `git worktree remove` — exit 0
git worktree list  : 1 entry/-ies (the registration is gone)
worktree on disk   : removed
cwd  after removal : /home/user/workspace/cwd-demo/scratch/red/wt-demo (deleted)
=> still PINNED (the ' (deleted)' suffix) — finish-cleanup guard 3 keeps refusing
   this branch, the merger never merges it
=============== CASE GREEN: the holder calls the PRODUCTION release ===============
holder start cwd: /home/user/workspace/cwd-demo/scratch/green/wt-demo
released-by-holder: cwd is now /
cwd before removal : /
worktree removal   : `git worktree remove` — exit 0
worktree on disk   : removed
cwd  after removal : /
=> PINNED? no — the released tree's inode is free while the holder is STILL ALIVE
deliverable safe   : 888269b the deliverable
git worktree list — no demo registration survives in either repo:
/home/user/workspace/cwd-demo/scratch/red/repo    ac273ad [master]
/home/user/workspace/cwd-demo/scratch/green/repo  404d0a9 [master]

# the demo holder calls the PRODUCTION implementation (no second chdir spelling):
#   run.sh builds ./.cwdholder-demo, whose main.go imports
#   github.com/nick/gopi/internal/worktreerelease and calls worktreerelease.ReleaseReporting(),
#   the SAME seam every producer close-out calls (the test-binary skip does not
#   apply to it: it is an ordinary executable). It is built INSIDE the module
#   because internal/ packages are import-restricted,
#   then removed again — `git status` shows no leftover.

# guard 3 was NOT weakened
$ git diff --stat   (internal/tools/task_reclaim_self.go: comment + alias only — no guard edit)
$ go test -run 'TestReclaimOwnFinishedWorktrees' ./internal/tools/
ok  github.com/nick/gopi/internal/tools  9.118s   (6/6 PASS, incl. RefusesALiveProcessInside)
```

**Full suite.** `go test ./...` was run. Two categories of pre-existing failure
appear, both **reproduced on the untouched baseline** (`~/workspace/gopi-cwd-baseline`,
master `10f96bc0`), so neither is attributable to this change.

*(a) environment-dependent, cwd-relative reads (`cmd/agent`):*

```
--- FAIL: TestTopLevelHelpCoversEveryDispatchedSubcommand
    usage_test.go:518: read main.go: open main.go: no such file or directory
--- FAIL: TestTaskListReadPathE2E / TestSweepZombiesCLI_*     (baseline: the same 3)
```

They read files relative to the **test binary's cwd**, and pass when the package
is run as `go test ./cmd/agent/` (verified on this branch). Identical 3 FAILs in
the baseline log `~/workspace/cwd-demo/baseline-cmdagent-full.log`.

*(b) package gates the baseline also fails (`internal/testutil`):*
`TestPinCoverageTable` and `TestEveryTempUsingSuiteIsPinned` report
`internal/daemoncwd` NO-PIN, and `TestWorkerOwnershipPointerPresent` lists the
same pre-existing devdoc/briefing files — verbatim identical on the baseline
(`~/workspace/cwd-demo/baseline-testutil-gates.log`). The package THIS change
adds reads **PIN**, and its devdoc carries the pointer line, so the change
neither causes nor hides those rows.

No gate was loosened; the six standards gates are untouched and green.

**And the branch's own suite caught two real defects before this (see §4) — 30
cwd-relative failures — which is exactly why the full suite, not just the new
tests, is the gate that decides.**

**Pre-existing flakes, measured not assumed** (a failure in a daemon E2E proves
nothing by itself; only an A/B on the SAME selection separates "my regression"
from "the suite was already like this"). Branch tip `205ba5cf` vs pristine
master `10f96bc0` (worktree `~/workspace/gopi-cwd-baseline`), SIX runs of the
same selection in `cmd/agent`:

```
$ for i in 1..6; do go test -count=1 -run '<the daemon/status/release selection>' ./cmd/agent/; done
branch   1..6: fails=0 0 0 0 0 0   → 0/6
baseline 1..6: fails=0 1 0 0 1 1   → 3/6   (raw logs: ~/workspace/cwd-demo/flake-*-*.log)
```

Both baseline failures are the same pair of pre-existing daemon-E2E patterns,
and both are **timing/race shaped, not cwd shaped** — neither touches this
change's surface:

- `TestWorkerDaemon_SelfExitsOnIdleQueue` (31.6s, 31.5s) — the daemon log never
  carried the idle self-exit line; it had reached `queue drained (1/2 idle
  rounds)` and was killed by the test's own wait budget before the second idle
  round. A slow second tick under a loaded machine (the baseline runs shared the
  host with this branch's builds), not a missing release.
- `TestWorkerDaemon_DetachesAndKeepsRunning` (0.61s) — `ps -o sid -p <pid>: exit
  status 1`: the launched daemon was already gone when the very first `ps`
  probe ran (`wdProcessField` fatals on that), i.e. the pid had exited before
  being inspected. Same class: the daemon E2E's own liveness probe racing
  process startup/exit.

Consequence for the branch's claim: the cwd-release change is **not** the cause
of either, and on the identical selection the branch was *more* stable than
master. Recorded here because "it failed once" and "it is my regression" are
different claims and only the A/B separates them.

## Not Done (honest)

1. **`cmd/agent/task_session_cwd.go:193` (`taskWorkdirRestore`) is a chdir but
   NOT a release, and it was deliberately left alone.** It undoes a *round's*
   move back to its captured origin. Its origin check (`liveWorkdir`) already
   refuses a removed origin and leaves the process where it is — so if a round's
   origin was the removed worktree, the process **stays inside the deleted
   directory**, which is the same pinning the epic exists to stop. Whether the
   right repair is "fall back to the release root when the origin is gone" is a
   behavioural change to the round-origin contract (and to `TaskWorkdirEnv`, the
   child's start workdir), so it is **not** in this branch. A follow-up task is
   warranted; the gate does not cover this file (it carries no removal trigger),
   so nothing here pretends the case is closed.
2. **The merger's per-tick sweep seams (`serveMergedCleanupSweep`,
   `serveWorktreeReapSweep`, `serveFinishCleanupSweep`) are covered transitively,
   not individually.** All three live in `cmd/agent/cli_tasks.go` and are reached
   from passes whose boundary now releases (`mergerPassOnce`, `runTaskLifecycleTick`
   → the worker loop's `workerRoundOwnWorktrees`); the gate keys on the FILE, so
   it cannot distinguish "this file releases somewhere" from "this specific seam
   releases". A file-level gate is what keeps its false-positive count at zero —
   the finer, seam-level gate is not implemented.
3. **No e2e test drives a real `gopi worker`/`gopi task merger` daemon to
   completion and then removes its worktree.** The acceptance proof uses the
   production release function called from a real holder process plus the
   in-process rig; the daemon round trip is unproven end-to-end (cost, and the
   daemon's own harness dependencies).
4. **The gate has no baseline.** It is EMPTY-era: the producers all comply
   today (four since master 69975030), so any new one fails immediately. That is
   intended, but it means the gate has never seen a real legacy permit and the
   `// cwd-release-ok:` escape path is exercised only by the synthetic probe.

## Inventory re-established (2026-09-16) — floor 5 → 4, and why the number moved

`TestGateCwdReleaseFindsTheRealTree` was RED on master tip `69975030`
(`found only 4 worktree-removing producer(s) … floor 5`), which made
`go test ./...` — and therefore the merge/verify path — red for every worker.
It was the ONLY failure in `./internal/standards/`.

**Root cause, not a count to be nudged.** `126fad74`
("refactor(merger): ff-only or park-and-file — no child, no wait, no state",
2026-09-16 18:24) deleted the merger's two spawned-child faces — and with them
the two producers that were this gate's original inventory members 3 and 4:

| deleted producer | what its removal was for |
|---|---|
| `cmd/agent/merge_resolve_child.go` | `git … worktree remove --force` after the merger's spawned RESOLVER child |
| `cmd/agent/merger_verify_child.go` | `git … worktree remove --force` after the merger's spawned VERIFY child |

Both removals cleaned up after children the SAME commit removed, so the
producers went away **with** the machinery — they were deleted, not relocated.
Verified rather than assumed: the removal verb those files carried has no
successor anywhere in the scanned trees (`grep -rn '"worktree", "remove"'
cmd/agent internal/agent` matches no production file today), and a tick is now
pure git (`merge --ff-only`, or a park of the branch ref) that spawns no child,
so the merger never stands in a worktree. Conflict resolution moved to the
ordinary worker path, whose own close-out already releases.

**Direction taken: (b)** — a producer was legitimately removed, so the floor and
the inventory moved together, with the cause recorded in the constant's doc
comment. The hand-counted inventory is four files, all still calling the
release:

* `cmd/agent/cli_resume.go` — `TaskFinishCleanupSessionID` (via var)
* `cmd/agent/cli_tasks.go` — `TaskFinishCleanupSessionID` + `ReclaimOwnFinishedWorktreesWithin` + `ReclaimSelfOwnRegisteredWorktreesWithin` + `ReclaimDeadHolderWorktrees`
* `cmd/agent/cli_tasks_worker.go` — `ReclaimOwnFinishedWorktreesWithin` + `ReclaimSelfOwnRegisteredWorktreesWithin`
* `internal/agent/taskregister.go` — `TaskFinishCleanupSessionID`

**Trigger scope re-read at the same time.** The four arms listed in
`cwdReleaseRemovalTriggers` are the ones actually CALLED from the scanned trees.
`ReclaimHeldBranches` (branch-only — orphan refs, never a worktree
registration) and `ReclaimRunResidue*` (ownership-by-other: its contract says
the caller must NOT be the holder) are deliberately absent, with the reason now
recorded on the constant instead of left to the reader.

**Falsified, not asserted** (both halves of the gate still bite on the new
floor): deleting the release CALL from `cli_tasks_worker.go` → the gate reports
that producer (`removes a linked worktree but never releases this process's cwd`),
the exhaustive half working as designed; additionally deleting its reclaim-arm
call → the gate fails with `found only 3 … (floor 4)`, so the floor's
non-vacuity tripwire is live at the new value and not merely written down.
