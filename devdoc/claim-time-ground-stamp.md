<!-- doc-class: normative -->
# Task Report: claim-time ground stamp — an in-flight row names its branch + worktree

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: branch↔task linkage, item **[2/5]** (2026-09-16).
Branch: `feat/claim-time-deliverable-stamp` (worktree `~/workspace/gopi-claim-stamp`).

## The gap

Before this change the pair (branch, worktree) reached a row through exactly
**two** writers, and neither fires while a run is in flight:

| writer | when | gate |
|---|---|---|
| `taskBackfillDeliverableWorktree` (+ the executor's bash-round seam) | when the executor's own bash creates/moves a worktree | needs a git-touching round in a linked worktree |
| `taskBackfillDeliverableBranch` | **completion only** | `t.Status == taskStatusCompleted` (one caller: `tasks.go:5405`) |

So a row claimed and executing in a worktree its own bash never created (the
wrapper / verify / resume shapes) named **no ground at all** for the whole
in-flight window — its deliverable stayed the submit-time
`{"repo":...}` placeholder (`deliverableRepoOnly`). The merger's resume loader
(`deadHolderResumeRows`) scans for the worktree needle and the branch axis
compares `row.Branch == reg.Branch`, so an in-flight row could neither name nor
be found on either axis. (Root cause recorded in memory #3191.)

## Completed

- **`cmd/agent/task_claim_ground.go`** (new): `taskClaimGroundStamp(taskID,
  workdir)` — the wrapper-side seam. It resolves the row's own
  `deliverable.repo` (`tools.TaskDeliverableRepoFor`) and delegates to the ONE
  stamp entry; best-effort by contract (a return value the caller ignores — a
  claim-time ground that cannot be resolved never blocks the run).
- **`cmd/agent/cli_tasks.go`** — one call in `runTaskRunOnce`, immediately after
  `executorChildDir` resolves `childDir` (the directory the executor child
  *actually* starts in, **after** the foreign-worktree escape). Stamping
  `execWorkdir` there would have bound the row to a foreign task's linked
  worktree — the very hijack the creation seam's never-rebind rule exists to
  prevent. The ground reference is the row's own `deliverable.repo`, never the
  caller's cwd (the wrapper has already `taskWorkdirEnter`'d into the task's
  session directory by then).
- **`internal/tools/task_deliverable_worktree.go`**:
  - `taskWorktreeStampClaimer` (the claim-time entry) + exported
    `TaskWorktreeStampClaimerFor` for `cmd/agent`. It resolves the linked
    worktree owning the claim dir (`taskWorktreeOwningDir`), refuses when
    `cmd/agent`'s **ownership guard** cannot prove the worktree unowned, then
    writes through the ONE writer.
  - `taskWorktreeRowOwner` — the occupation test: is there *another* row whose
    deliverable already names this worktree? The `DeliverableWorktreeKey` scan
    is the ownership claim (`reclaimSelfRowsFrom` and the sweep's ownership arm
    keep a named worktree alive), so a claim must not mint a **second** owner
    (the 2026-09-12 foreign-owner hijack). **Fail-closed**: the error is
    returned, not swallowed — "no owner could be proven" is not "there is no
    owner". Bound through the `taskWorktreeOwnerScan` seam so the branch is
    observable in a test (precedent: `taskLiveHolderProbe`).
  - `taskBackfillDeliverableWorktreeAt(taskID, worktree, branch, groundRef
    *string) (bool, error)` — the existing writer, refactored to (a) report
    `(written, err)` instead of only `err` and (b) accept an **explicit ground
    reference**: when non-nil, the blob's repo must still match it, so a
    claim-time stamp can never bind the row to another project's worktree after
    a concurrent re-stamp. `taskBackfillDeliverableWorktree` is now a thin
    wrapper (`groundRef = nil` = the row's own `deliverable.repo`, the creation
    seam's rule) — **one writer, one parser, one key**, no second spelling
    (Rules 13/14).

**Additive by construction** (the briefing's hard constraint): the stamp shares
`taskDeliverableParse` / `taskDeliverableRebuild` and `DeliverableWorktreeKey`
with every other deliverable path, refuses to re-point a row that already names
a worktree, and CASes its `UPDATE` on the raw blob. The completion-time backfill
(`taskBackfillDeliverableBranch`, which resolves the branch at the **child's**
cwd — the memory #2371 trap) is **byte-for-byte unchanged** and stays the
authority for a terminal row: there is no second source of truth, and the child's
cwd is never consulted by the claim-time path.

## Verification

- Build: `go build ./...` → OK; `go vet ./cmd/agent/ ./internal/tools/` → clean
- `go test -count=1 -timeout 25m ./internal/tools/` → **ok (701.25 s)** — the
  package budget is 25 m (`scripts/test-budget.sh`); the default 10 m trips on
  this host without any assertion failing (see Not Done)
- `go test -count=1 -timeout 25m ./cmd/agent/` → **ok** (see below)
- `internal/standards` + `internal/stylegate` are red on **pristine master**
  too (measured on a `69a4fed4` worktree) — not this branch's doing
- A/B on the real tree: `taskClaimGroundStamp(target, execWorkdir)` → the
  structural gate fails with the ownership message; restored → passes

The two halves of the acceptance live in
`internal/tools/task_deliverable_claim_stamp_test.go` (real git fixture + real
temp ledger):

1. **`TestClaimTimeGroundStampNamesGroundInFlight`** — submits a row, claims +
   starts it (asserting `in_progress`), stamps, then asserts the blob carries
   **repo + branch + worktree** with exactly **one** `worktree` key; then drives
   the REAL `TaskRunWriteback(completed)` path with a seeded child session whose
   `session_start.workdir` is the worktree, and asserts the completion-time
   backfill records the **same** branch / worktree / repo (`c == d`) and exactly
   **one** `branch` key — no drift, no double writer.
2. `TestClaimTimeGroundStampLeavesMainCheckoutRowRepoOnly` (branch `master` is
   exempt → the blob is untouched), `TestClaimTimeGroundStampNeverRebindsAnExistingWorktree`
   (a second, different worktree is refused).
3. **`TestClaimTimeGroundStampRefusesWhenOwnershipIsUnproven`** — the fail-closed
   guard, **A/B gradeable**: arm 1 (scan fails) must refuse; arm 2 (the pre-fix
   swallow shape, same fixture, same forced failure) must WRITE — the test FAILS
   if the two are not distinguishable, so it can never pass as a no-op
   assertion. A first attempt staged the failure by corrupting the ledger DB and
   the A/B came out **identical** (both arms `written=false` — a broken ledger
   makes the blob writer refuse too, so the arm proved nothing); that is *why*
   the seam exists.

`cmd/agent/task_claim_ground_test.go` drives the wrapper's seam over a real
ledger at a temp `GOPI_MEMORY_DB` + real git (main checkout + two linked
worktrees): in-flight naming, ownership (main checkout / unrelated dir / taken
worktree refused, own worktree bound), the repo-less no-op — and the
**structural** half, `TestClaimGroundStampCallSiteIsTheSpawnFunnel`: the call
site in `runTaskRunOnce` (exists / once / argument `childDir` / after the
`executorChildDir` assignment) with four synthetic red-proof probes plus a real
tree A/B.

## Not Done (honest)

- **The wrapper SEQUENCE is pinned structurally, not by an in-process run.** The
  claim-time call sits inside `runTaskRunOnce` **after** `childDir` is resolved,
  and the `taskChildFailTestEnv=1` hermetic escape (the only in-process way to
  run the wrapper without spawning a child) returns **before** that point — so
  the claim → stamp sequence cannot be driven end-to-end from a Go test.
  `TestClaimGroundStampCallSiteIsTheSpawnFunnel` (`cmd/agent`) therefore pins
  the call-site over the AST: the call exists in `runTaskRunOnce`, exactly once,
  its argument is `childDir`, and it sits after the `executorChildDir`
  assignment — with a **red proof** (four synthetic probes, each breaking one
  clause, all rejected) and an **A/B on the real tree** (passing `execWorkdir`
  instead reddens the test: measured). This is the repo's precedent for an
  unobservable-in-process compose (`task_run_release_coverage_test.go`'s
  structural half, the `internal/standards` literal gates). A live-run
  observation (a real `gopi task run` + `~/.gopi/sessions/*.jsonl`) remains the
  outside-the-test evidence; it was not produced in this session.
- **`go test ./cmd/agent/` and `./internal/tools/` need more than Go's default
  budget on this host.** Measured here: `internal/tools` 701 s (green) with
  `-timeout 25m`; under the default 10m it trips the wall-clock budget and
  reports `FAIL … 60x.xxxs` with no assertion failure — the documented
  ledger-first-open cost (`devdoc/test-budget-single-source.md`, which declares
  `test_budget="25m"` in `scripts/test-budget.sh`). Run acceptance through
  `make test-budget PKGS=./internal/tools/` (or `scripts/test-budget.sh`), not a
  bare `go test`.
- **Concurrent test binaries on one host corrupt the ledger.** Two `go test`
  runs of these packages at the same time (each with its own tree but the same
  default memory DB) produced six spurious failures in the *unmodified* baseline
  tree — reproduced on pristine `69a4fed4`, so they are environmental, not
  regressions; the same tests pass when the package runs alone.
- **`internal/standards` and `internal/stylegate` are RED ON MASTER, not on this
  branch.** Re-measured on a pristine `69a4fed4` worktree:
  `TestGateCwdReleaseFindsTheRealTree` (inventory floor 5, found 4) and three
  `stylegate` ceilings (test-sleep ×2, test-setenv ×1, test-skip ×1 — including
  `internal/tools/testmain_test.go:65`, untouched by this branch). Verification
  for this item is therefore scoped to `./internal/tools/` + `./cmd/agent/`, and
  the deliverable-vs-baseline comparison is what carries the signal.
- **The `no linked worktree at <dir>` INFO line fires on every ordinary claim.**
  For the Rule-6a solo shape (the executor creates its own worktree mid-run) the
  claim dir is the repo's main checkout, so the stamp logs an INFO line and
  returns `false` — deliberately INFO (not WARN), but it is one notify line per
  claim. Not tuned here.
- The wider epic items (the merger-side loader/comparison repair on the branch
  axis, the `gopi task for-branch <branch>` reader) are **other items** of the
  epic and are untouched by this one.

## Files

- `cmd/agent/task_claim_ground.go` (new)
- `cmd/agent/task_claim_ground_test.go` (new)
- `internal/tools/task_deliverable_claim_stamp_test.go` (new)
- `internal/tools/task_deliverable_worktree.go`
- `cmd/agent/cli_tasks.go`

## Commit

Commit: `feat(task): stamp branch+worktree at claim time (epic branch↔task linkage 2/5)`
(branch head on `feat/claim-time-deliverable-stamp` — resolve with
`git log --oneline -1 feat/claim-time-deliverable-stamp`)

---

# Follow-on: the BRANCH axis (epic 01a0b182, task 01a0b1b1)

The worktree axis above is complete. A second gap on the **same pair**, found
while reading `cmd/agent`'s own `unowned:` output, is closed by
`f42f8cb4` on `feat/claim-ground-registration`.

## The gap, stated as a measurement

Every constructor of the (branch, worktree) pair — claim seam, creation seam,
completion backfill — resolved through **`taskWorktreeOwningDir`**, and that
function matches only a **linked** worktree of the repo: the first
`git worktree list --porcelain` record *is* the main checkout, and it is
excluded (as are exempt branches: `master`, the integration branches). So a run
whose branch is created **in place** — `cd <repo> && git checkout -b feat/x`,
which is what a run standing in the MAIN checkout does — recorded **no ground at
all** for the whole in-flight window; its branch reached the ledger only at
COMPLETION (`taskBackfillDeliverableBranch`, guarded on a terminal status).

That is the producer half of the merger's `unowned:`/gap class:
`branchAttributionRows` loads no row → the classifier returns
`branchAttribUnowned` → the render prints a branch as a gap **while its owner is
still working on it** (the defect the sibling task `01a0b182` reports from the
reader side, and explicitly dispatches here as the writer-side repair).

Measured consequence, this session: `gopi task sweep-worktrees --dry-run` on the
live repo printed 4 `attributed:` heads **and** 2 `unowned:` heads in one run.

## What landed (`f42f8cb4`)

- **ONE resolver** `taskGroundOwningDir(repo, dir)` — the linked worktree (both
  axes) **else** the repo's main checkout when its HEAD is on a non-exempt branch
  (`taskDirInsideRepo` + `taskRepoHeadBranch`, the latter reusing the package's
  ONE git branch reader `taskBranchFromWorkdir` and its ONE exemption rule
  `taskBranchExempt`; no second branch reader exists — Rules 13/14).
- **Both entry points route through it** and write a **branch-only** pair when
  there is no linked worktree; `taskBackfillDeliverableWorktreeAt` accepts that
  shape. The never-rebind guard stays the **worktree** axis's: a branch-only
  stamp has no directory to hijack, and one branch name is one owner by
  construction.
- **One-owner rule extended to the branch axis** — `taskBranchRowOwner`. Branch
  names are per-repository, and a branch-only row records no directory, so the
  discriminator is the candidate rows' own `deliverable.repo` (the column the SQL
  scan already filters on). Its scan error is **fail-closed** (Rule 15), exactly
  as the worktree guard's — "no owner could be proven" is not "there is no
  owner".
- **Rule 15b, the silent miss made loud** — `taskBranchCreationsFromCommand`
  (`git checkout -b|-B|-c|-C`, `git switch -c|-C`, `git worktree add`, scanned
  as an argv-ish token set so `checkout` inside a path is not a hit) feeds
  `taskWorktreeUnstampedLineFmt`: a round that CREATES a branch and resolves no
  ground logs ONE line naming the row, the probed directories and the command.
  Emitted only on a branch-creating command — a signal on every git round is
  noise, and noise is how a real miss gets ignored.
- **The `cd` carry** — `taskBashWorkdirCandidates` now consults the shell's own
  `cd` carry through the package's existing readers (`commandChangeDir` /
  `carriedChangeDir`, `path_policy.go`), not a second parser. Measured live: the
  agent stands in its session directory and reaches the repo by `cd <repo> && git …`
  **inside the command**, which previously produced no candidate at all.
- **Rule 1b/13 backfill** — `DeliverableBranchKey`/`DeliverableRepoKey` exported
  beside `DeliverableWorktreeKey`; the SQL ownership scan and the test fixtures
  that held the bare `"branch"`/`"repo"` literals now reference them.
- **The existing render reused** (`merger_status_candidate.go`) — a merge
  candidate that a live row OWNS prints the owner beside the candidate answer,
  via the SAME `ownerClause` the non-candidate arm prints. No second mapper: the
  merger's duty is exactly the distinction "no row names it → merge it" versus
  "a run owns it and is still working".

## The test that grades the fix

`internal/tools/task_deliverable_inplace_branch_test.go` (8 tests) — real git
fixture, real temp ledger. Its red-proof is by **mutation with the symbols kept**
(so the file still compiles and the failure is an assertion, not a build error):
resolver call sites reverted → 3 FAILs; the `cd` carry removed → FAIL; the
candidate owner clause removed → FAIL. Precedent worth keeping: a red-proof by
deleting the new symbol proves only that the file does not compile.

One measured trap, recorded because it cost a round: `TestInPlaceBranchIsStampedAtCreationTime`
must pass an **unrelated** directory as the round's workdir and reach the repo by
`cd <repo> && git switch -c …` in the COMMAND. A first version passed the repo
itself as the workdir, which is stamped by the workdir candidate alone and
therefore passes with or without the `cd` carry — an A/B that proves nothing.

## Not done here

- The `cmd/agent` package **as a whole** is red at Go's 10 m budget
  (`FAIL … 600.632s`, goroutines parked in `daemon_cgroup_e2e_test.go` /
  `cli_voice_review_test.go` cleanup) — the known `cmd/agent`-budget row, not
  this diff; the touched package and the claim-seam tests are green.
- **No live instance of the closed gap exists on this host**: every one of the 20
  attributed heads in the live render is a *linked-worktree* branch (an axis
  master already stamped), and no row here claims a branch inside a main
  checkout. The in-place clause's proof is therefore the red→green pair, not a
  live row. Stated rather than dressed up.
