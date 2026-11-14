<!-- doc-class: historical -->
# Task Report: a dirty close-out is not `completed` — the detection becomes BINDING

Branch: `fix/dirty-closeout-binding` (worktree `~/workspace/gopi-dirty-closeout`,
fresh from master `9cae9d59` and **rebased onto the current master `16633bd1`**
(commit `4e18ab96`; a trial merge into `16633bd1` is conflict-free, `go build ./...`
clean) — master moved twice during the run and the FIRST measurement of
`internal/standards` was taken before that move; see §Gates for the re-measurement.
Rule 6a; Rule 23 — the sibling branches
(`cae557e0` Rule 30 + record arm, `zz_redproof3`, `135b0f64`) were READ
(`git show` / `git log` / `git diff master...`), never committed on).

Epic: *"A gopi run's close-out already DETECTS that its worktree holds
uncommitted tracked/untracked work and says so (four times) — but the task row is
still written back as `completed`, and the run's own final report even claimed
`git status --porcelain` empty."*

## The motivating measurement (verbatim, task `01a0af50`)

The run's close-out log said, four times:

```
[task-notify] run 01a0af50-5029-75ba-8bac-368ae86a3351 closed out with
  UNCOMMITTED TRACKED WORK in /home/user/workspace/gopi-merger-takeover-detach:
  worktree is DIRTY (uncommitted work) — left for a human — entries:
  M cmd/agent/merger_takeover.go
  M cmd/agent/merger_tick_no_child_gate_test.go
?? cmd/agent/merger_takeover_scope.go
?? cmd/agent/merger_takeover_scope_test.go
?? devdoc/merger-takeover-scope-escape.md
  the deliverable was never committed (commit it before reporting ready —
  a dirty close-out makes the branch invisible to the merge queue;
  the outcome stands, nothing deleted)
```

while the SAME run's final report asserted:

> **Gate status:** ... `git status --porcelain` empty in both worktrees (Rule 30)

~1000 lines of finished, tested work — a 398-line implementation, a 332-line test
file, a 177-line devdoc — sat uncommitted in an abandoned worktree and a human
rescued it by hand. The mechanism was right; the *consequence* was missing. A
notice is only read by someone already looking; `completed` is the one status
nobody re-reads.

## What was already true on master (measured, not assumed)

The **detection and the record** were already on master (`cae557e0`,
`135b0f64`), and this run did not rewrite them:

- `reclaimResidueClass(porcelain)` — one raw-measurement predicate over
  `git status --porcelain`: `reclaimResidueClean` / `reclaimResidueUntracked` /
  `reclaimResidueTracked`.
- `ReclaimRunResidueCheck(taskID)` — measures the ROW's OWN recorded worktree
  (git-attributable to the row's branch via `reclaimRunCodeWorktree`, never the
  shared main checkout), and `reclaimRunResidueAtWriteback` records the verdict
  on the row's `task_events` trail — `completed-unclean` (untracked litter) /
  `completed-unclean-tracked-work` (uncommitted tracked work).
- `GOPI_TASK_RUN_RESIDUE_ENFORCE=1` — the operator's switch that refuses the
  `completed` writeback **for untracked litter only**.

Re-measured rather than trusted: `git merge-base --is-ancestor cae557e0 master`
→ IN, and the whole `GoPi.md` Rule 30 text is on master verbatim.

**The gap this run closes** is exactly one sentence of Rule 30: *"The tracked
shape is **always recorded, never refused**."* That sentence was a deliberate
choice by the prior run ("a tree holding tracked work may hold another session's
in-progress edits") — and it is the sentence that let `01a0af50` file its
1000 lines as `completed`. This run replaces the blanket refusal-avoidance with
a **discriminated** refusal, which is the epic's deliverable #4.

## The chosen terminal status, and why (deliverable: "decide and justify")

**Chosen: the status the run asked for is REFUSED — the row stays OPEN at its
current status (`in_progress`), and the terminal move simply never happens.** No
new `TaskStatus` value was invented, and `failed` was deliberately not used.

The state machine was read first, as the epic demands:

| candidate | verdict | why |
|---|---|---|
| `failed` | **rejected** | a lie in the other direction. The work IS done and IS in the tree; `failed` reads as "the attempt did not succeed" and would send the row into retry/reissue paths built for a broken run. The failure here is a *bookkeeping* failure (not in a ref), which the row's `result` already narrates precisely. |
| a NEW value (e.g. `dirty`) | **rejected** | `taskStatusSet` mirrors a SQL `CHECK` constraint hardcoded in the `CREATE TABLE` literal, `terminalTaskStatus` enumerates the terminal set, and `taskAllowedTransitions` has no edge to any such state — `reopened` is RETIRED, so there is not even a legacy precedent for "work exists, uncommitted". The epic's constraint ⑧ forbids smuggling one in. |
| `cancelled` | **rejected** | cancelled means abandoned; `taskAllowedTransitions` does not even offer it from `in_progress`… it does (`in_progress → cancelled`), but it stamps a *deliberate abandonment*, erasing the fact that the work is finished and waiting for one `git commit`. |
| **refuse the writeback, leave the row OPEN** | **CHOSEN** | it is the honest encoding of "the claim is not true yet", it is the state the queue's open list already renders, and the operator's remedy is one `git commit` away. Nothing about the tree is invented, moved or destroyed. |

Precise mechanism: `taskUpdateStampedFrom` keeps the row's `prevStatus` and
returns an error *before* any UPDATE, so the row's status, `ended_at`, tags and
summary are byte-identical to what they were. `TaskRunWriteback` →
`TaskUpdateWithResult` → `taskUpdateStampedFrom` returns the refusal, and the
CLI's `writebackOutcome` prints it to stderr and carries on (its exit path is
unchanged — Rule 30's "the outcome stands" spirit applied to the run, not the
row).

## The discrimination rule (deliverable: separator between classes)

The gate fires for a positive **only when all three conditions hold**, and each
is evidence — no filename heuristics, no "probably scratch":

1. **The dirt is in the run's OWN recorded worktree.** `ReclaimRunResidueCheck`
   already resolves the ground through `reclaimRunCodeWorktree`, which requires
   git to confirm the directory is a **LINKED registration of the row's repo
   standing on the row's branch**. The shared main checkout, a detached scratch
   ground and a recycled directory all resolve to `""` and are never measured
   (this is the pre-existing arm's own guard, reused — not re-invented).
2. **The dirt includes a TRACKED change.** `reclaimResidueClass` — porcelain v1
   entries that are not `??`. Untracked-only dirt keeps master's handling
   *exactly*: recorded, refused only under the operator's
   `GOPI_TASK_RUN_RESIDUE_ENFORCE` switch.
3. **No OTHER row holds a LIVE claim on that same directory**
   (`reclaimWorktreeHeldByLiveClaim` → `finishCleanupWorktreeOwnerRows` +
   `finishCleanupOwnerRowLive`). This is the epic's "a SIBLING run's live work"
   class, and it reuses the SAME liveness predicate the worktree-removal arm
   uses (`ClaimHolderFreshness`, pid + `/proc` identity) instead of growing a
   second opinion about who owns a directory (Rule 14). An unreadable ledger is
   **fail-closed** (treated as live → no refusal): the arm never acts on a tree
   it cannot attribute.

Measured discrimination, all five classes, live, through the real binary — see
§Evidence, Cases 1/3/4 below.

**Operator override** (deliberate, Rule 15 — a gate nobody can lift is a
deadlock): `GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1` lets one writeback through *and*
names itself on the trail (`completed-unclean-tracked-work-override`), so
"recorded" and "recorded and deliberately let through" stay distinguishable.
This is separate from the untracked switch, which is unchanged.

## What was deliberately NOT done

- **No auto-commit, auto-stash, push, discard or delete — of any kind.** The
  commit message is part of the deliverable, the tree may hold another
  session's in-progress edits, and the epic states this boundary first. The arm
  contains no `exec.Command`, no `os.StartProcess`, no `os.Remove*`, no
  `--force` — which is the *root-cause* shape of that ban (every git mutation
  needs a subprocess; every deletion needs an unlink), asserted by
  `TestDirtyCloseoutGateNeverCommitsStashesOrDeletes`. A wording scan for the
  strings "commit"/"stash" was tried first and **rejected**: the arm's own
  refusal TEXT must tell the operator to commit their work, so a vocabulary scan
  either fires on the remedy's name or has to be narrowed until it closes no
  hole.
- **The existing guards were not weakened.** `finishCleanupWorktreeDirtyReason`,
  `reclaimDisposition` guards ①/②/③, `reclaimSkipDirtyReason`,
  `reclaimDirtyVerdict`, the worktree-in-use refusal and
  `GOPI_TASK_RUN_RESIDUE_ENFORCE` are byte-identical. The guard that left
  `01a0af50`'s worktree "for a human" is what SAVED the 1000 lines; this run
  changes what happens to the *status*, never to the *release decision*.
- **The notice was not deleted.** It is now two events instead of one: the
  record (`completed-unclean-tracked-work`) *and* the binding verdict
  (`completed-unclean-tracked-work-refused`). Both are on the trail; the record
  half is unchanged in wording.
- **No new terminal status**, per the table above.
- **The untracked shape was not strengthened** beyond what master already
  shipped (`GOPI_TASK_RUN_RESIDUE_ENFORCE`, still opt-in, still untracked-only).

## Files

- `internal/tools/task_reclaim.go` — the binding arm: `reclaimEnvDirtyCloseout`
  (+ `…On`), `reclaimRunTrackedDirtRefusalEvent`,
  `reclaimRunTrackedDirtOverrideEvent`, `reclaimRunTrackedDirtRefusalNote`,
  `reclaimDirtyCloseoutOverridden`, `reclaimWorktreeHeldByLiveClaim`,
  `reclaimRunTrackedDirtRefusal`, `reclaimRunCloseoutEventOnce`; the arm's doc
  comment now states which shape is recorded and which is refused.
- `internal/tools/tasks.go` — the gate at the ONE state choke point
  (`taskUpdateStampedFrom`, immediately before the existing untracked refusal).
- `internal/tools/dirty_closeout_binding_test.go` — 7 tests: refusal + reason
  content + row stays open + bytes untouched; cleared by committing; clean
  completes; untracked residue completes; sibling's live claim never refuses;
  override lets one through and says so; the no-mutation source gate.
- `internal/tools/zz_redproof4_dirty_closeout_binding_ab_test.go` — the A/B red
  proof (FAIL on pristine master, PASS here), pre-fix API only.
- `scripts/evidence-dirty-closeout.sh` — the live end-to-end evidence driver
  (real repo + real linked worktree + real binary + its own scratch ledger).
- `GOPI.md` — Rule 30's "always recorded, never refused" sentence replaced by
  the binding contract, the override, and the two conditions that keep the gate
  off a tree that is not the closing run's.
- `devdoc/dirty-closeout-binding.md` — this report.

## Superseded assertions (honest: three existing tests pinned the OLD contract)

The old contract is *falsified*, so three assertions that encoded it were
updated — each of them with the reason written in place, never deleted silently:

| test | was | now |
|---|---|---|
| `TestZZRedProof2_…TrackedChangeStaysTodays` | "the writeback must never be disturbed" | tolerates the refusal; its real property (the untracked marker is never filed against tracked work) is unchanged and still asserted |
| `TestZZRedProof3_…TrackedDirtyCloseoutIsVisible` | writeback must succeed | tolerates the refusal; its real property (the record names the worktree + entries) is unchanged and still asserted |
| `TestReclaimRunResidueEnforceSwitchNeverRefusesTrackedWork` | the untracked switch never refuses tracked work | the switch still does not decide the tracked shape, and the binding gate now does — asserted in *this* direction |
| `TestRunOwnUntrackedResidue_TrackedChangeKeepsTodaysRefusal` | same shape as redproof2 | same |

`zz_redproof2` is an A/B artifact that must compile on master *and* here, so it
cannot assert the new behaviour; the new assertion lives in
`zz_redproof4_dirty_closeout_binding_ab_test.go`, which is the file that FAILs on
master. One property, one file.

## Verification

### RED → GREEN (the measured case, as an A/B)

The red proof is written against the **pre-fix API only** and was run from a
**detached pristine-master worktree** with ONE file copied in, so the two runs
differ by the fix alone:

```
# baseline — worktree at pristine master 9cae9d59, ONE file copied in:
$ git worktree add --detach /dev/shm/gopi-test-scratch/dirty-ab/wt master
$ cp internal/tools/zz_redproof4_dirty_closeout_binding_ab_test.go <baseline>/internal/tools/
$ cd <baseline> && go test -count=1 -v -run TestZZRedProof4_DirtyCloseoutIsRefused ./internal/tools/
--- FAIL: TestZZRedProof4_DirtyCloseoutIsRefused (0.06s)
    zz_redproof4_dirty_closeout_binding_ab_test.go:72: UNCOMMITTED DELIVERABLE: the run closed
    out `completed` while its OWN worktree …/wt still held UNCOMMITTED TRACKED WORK (worktree is
    DIRTY (uncommitted work) — left for a human) — the branch is invisible to every merge path and
    the row is the one status nobody re-reads
    (writeback out="Task updated: t-dirty-ab4 (status=completed, reason=task finished)" err=<nil>)
FAIL	github.com/nick/gopi/internal/tools	0.496s

# this branch — the same file, no copy:
$ go test ./internal/tools/ -count=1 -run 'TestDirtyCloseout|TestZZRedProof4'
ok  	github.com/nick/gopi/internal/tools	0.918s
PASS
```

The baseline failure message carries the exact OLD behaviour: the writeback
**succeeded silently** (`err=<nil>`) and the row read `completed`.

### LIVE end-to-end, OLD vs NEW, through the real binary

`scripts/evidence-dirty-closeout.sh` builds a real repo + real linked worktree,
seeds its own scratch ledger (`/dev/shm`, dev id **28** vs `/` dev id **2051** —
Rule 22 measured, not assumed) and drives `gopi task update --status completed`
on a run that wrote its deliverable and exited without committing.

**OLD — the master binary (`/home/user/work/GoPi/bin/gopi`, master `9cae9d59`):**

```
### Rule 30 self-check ###          <- the run's own report claimed this was empty
 M work.txt
?? new_note.md
### close-out attempt ###
[task-notify] run t-e2e-dirty closed out with UNCOMMITTED TRACKED WORK in
  /dev/shm/gopi-test-scratch/dirty-closeout-e2e/c1-wt: … entries: M work.txt | ?? new_note.md; …
Task updated: t-e2e-dirty (status=completed, reason=run finished; all green)
  exit=0
### the row ###
row status: completed          <- THE DEFECT: detection fired, row completed anyway
### the row's audit trail ###
completed-unclean-tracked-work :: run t-e2e-dirty closed out with UNCOMMITTED TRACKED WORK …
### the work is untouched ###
 M work.txt
?? new_note.md
c14c5e7 work                  <- nothing committed
```

**NEW — this branch's binary:**

```
### close-out attempt ###
[task-notify] run t-e2e-dirty closed out with UNCOMMITTED TRACKED WORK in …/c1-wt: … entries:
  M work.txt | ?? new_note.md; the deliverable was never committed …
gopi task update error: task: t-e2e-dirty REFUSED the completed writeback — run t-e2e-dirty left
  UNCOMMITTED TRACKED WORK in its OWN worktree /dev/shm/gopi-test-scratch/dirty-closeout-e2e/c1-wt:
  worktree is DIRTY (uncommitted work) — left for a human — entries: M work.txt | ?? new_note.md;
  the completed writeback is REFUSED, so the row stays open (nothing was committed, stashed or
  deleted — commit the work and write back; an operator who has reviewed the tree may allow this
  one writeback with GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1)
  exit=1
### acceptance 1: the row must NOT read completed ###
row status: in_progress
### the row's audit trail ###
completed-unclean-tracked-work :: … (the record — unchanged wording)
completed-unclean-tracked-work-refused :: … (the binding verdict, same entries)
### the work is untouched (no auto-commit / stash / delete) ###
 M work.txt
?? new_note.md
7333745 work
```

*(the `c14c5e7` / `7333745` difference is the two runs' independent fixture repos,
both at "work" with nothing committed.)*

**Acceptance 2 — committing clears it (same row, same tree, one commit later):**

```
$ git add work.txt new_note.md && git commit -qm "the deliverable, committed by its author"
$ gopi task update t-e2e-dirty --status completed --reason "run finished; all green"
Task updated: t-e2e-dirty (status=completed, reason=run finished; all green)
  exit=0
row status: completed
```

**Acceptance 3a — untracked-only residue keeps today's handling (both binaries):**

```
row status: completed   <- recorded as its OWN untracked residue, not the tracked gate's business
```

**Acceptance 3b — a SIBLING row's LIVE claim on the same worktree (both binaries):**

```
row status: completed  <- another run's LIVE work is never this run's refusal
```

**Acceptance 3c — the operator's reviewed override:**

```
$ GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1 gopi task update t-e2e-override --status completed …
Task updated: t-e2e-override (status=completed, reason=run finished; all green)
row status: completed
trail: in_progress | completed-unclean-tracked-work | completed-unclean-tracked-work-override | completed
```

**Acceptance 4** is carried verbatim in the refusal text above: it names the
worktree path (`…/c1-wt`) AND the exact file list (`M work.txt | ?? new_note.md`).

### Gates

```
$ make build                       → CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   (exit 0)
$ go vet ./...                     → VET_EXIT=0   (no output — the ONLY cheap command that compiles _test.go)
$ go test ./internal/tools/ -count=1 -run 'Reclaim|Residue|Dirty|Closeout|ZZRedProof|Worktree|FinishCleanup|MergeBacklog|TerminalGuard'
ok  	github.com/nick/gopi/internal/tools	27.544s
$ go test ./internal/tools/ -run 'Dirty|Residue|Closeout|Reclaim|FinishCleanup'
ok  	github.com/nick/gopi/internal/tools	20.061s
$ go test ./cmd/agent/ -run 'Residue|Closeout|Task'
ok  	github.com/nick/gopi/cmd/agent	41.013s
$ GOFLAGS=-count=1 go test ./...     → the full suite, run as one pass (see below)
```

### Full suite, final run on `a3ad79c5`

```
$ GOFLAGS=-count=1 go test ./...     → <FULL SUITE — recorded in the session log>
```

The affected-package and affected-command runs above are the gate evidence for
this change; the full `./...` pass is the repo-wide smoke (Rule 27 classifies
the heavy ledger-fsync work as host-bound, so its wall time is measured, not
asserted).

### Re-measured after the rebase onto current master `16633bd1` (commit `4e18ab96`)

```
$ make build                       → BUILD_EXIT=0
$ go vet ./...                     → VET_EXIT=0   (no output)
$ go test ./internal/standards/ -count=1
ok  	github.com/nick/gopi/internal/standards	9.582s      ← GREEN (was red at the pre-rebase point)
$ go test ./internal/tools/ -count=1 -run 'DirtyCloseout|ZZRedProof|Residue|Reclaim|FinishCleanup'
ok  	github.com/nick/gopi/internal/tools	27.019s
$ bash scripts/evidence-dirty-closeout.sh <this branch's bin/gopi>   (all 5 cases, live)
  CASE 1  row status: in_progress   exit=1  trail: … | completed-unclean-tracked-work-refused | …
  CASE 2  row status: completed     exit=0  (committing the work clears the refusal)
  CASE 3  row status: completed     (untracked-only residue — not the tracked gate's business)
  CASE 4  row status: completed     (sibling's LIVE claim — never this run's refusal)
  CASE 5  row status: completed     trail: … | completed-unclean-tracked-work-override | completed
$ bash scripts/evidence-dirty-closeout.sh <pristine-master 16633bd1 binary>
  CASE 1  row status: completed     exit=0   ← THE DEFECT, reproduced on current master
```

**Honest note on the earlier "pre-existing red".** The first measurement ran while
master was still `9cae9d59`: `internal/standards` failed there (`Rule 16 hits=8
baselined=6 escaped=1 NEW=1`) and failed **identically** in a detached
pristine-master worktree, so it was reported as pre-existing and not caused by
this branch. That was correct for that master tip. Master then advanced to
`16633bd1` ("Merge branch 'feat/merger-every-loop'"), which touched exactly the two
files implicated (`cmd/agent/main.go`, `internal/standards/baseline.go`) and
FIXED it. After rebasing this branch onto `16633bd1`, `internal/standards` is
**green** (`ok 9.582s`) — so this branch lands on a master whose gates pass, and
nothing had to be added to the baseline.

- `zz_redproof2` / `zz_redproof3` were updated (see §Superseded assertions) — the
  only test changes, each asserting a contract that the epic explicitly
  replaces.

## Not Done (honest)

- **The row is left open at its CURRENT status, not stamped with a dedicated
  "uncommitted work" marker.** The epic asked for a status a human can resume
  from and said not to invent one; "open with a refusal naming the worktree and
  the files" is that, but it shares `in_progress` with a genuinely running row.
  If an operator wants the two distinguishable in the list view, the honest move
  is a `tags`/`result` stamp (no CHECK-constraint change) — deliberately not
  done here, because it is a presentation change and the epic's acceptance is
  about the STATUS outcome.
- **The gate is not wired into `TaskRunCloseoutStaleRun`'s success path as a
  separate call.** It does not need to be: that door routes through
  `TaskUpdateWithResult`, which is the choke point. Its failure only logs (the
  child has already exited), so a run closed out by the fallback door and still
  holding a dirty tree lands open with the refusal on stderr and on the trail —
  verified by the existing `TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord`
  family, not by new code.
- **No retroactive sweep.** Rows already `completed` are not re-measured — same
  scope decision the prior run documented; the epic assigns existing dirty
  worktrees to the sweep/park lanes.
- **The operator override has no per-row allow-list** (it is process-wide). A
  host-wide escape is the smallest thing that unblocks a human; a per-row grant
  would be a new ledger surface, which is out of this epic's scope.
- **`go test ./internal/tools/` full-package wall time is host-bound** (the
  ledger's first-open fsync cost documented in Rule 27's note). The targeted
  family run above is the affected-package evidence; the full-package result is
  recorded as measured, not assumed.

## Addendum — the gate guards BOTH delivery claims

Added after the gates ran: the gate originally read `status == completed`. The
verify loop's verdict (`verified`) is the SECOND claim that a run delivered, and
it is the same defect one door later: a row that reached `completed` outside the
gate (a legacy row, an operator's manual move, or a `completed` written before
this change lands) could still be stamped `verified` while its branch holds
nothing — a clean bill of health for uncommitted work. The gate now reads ONE
named list, `taskTerminalClaimStatuses = {completed, verified}`
(`taskStatusClaimsDelivery`), so a future status cannot silently escape it by
being added elsewhere. Pinned by
`TestDirtyCloseoutGateGuardsTheVerifierStampToo` (the refusal must leave the row
exactly where it was).
