<!-- doc-class: historical -->
# The main-checkout arm's second door — `deliverable.repo` IS the shared checkout, and the fence gets a destination

**Class:** normative (a standing claim about master: two resolvable seams in
`internal/tools/task_main_checkout_deposit.go` and `internal/tools/task_run_ground.go`,
named in the manifest's anchor column).
**Measured shape:** row `01a0dbe4-ac93-74f6-8afe-e740669c9532` on the HP main checkout
`/home/user/work/GoPi`, 2026-09-26 — `deliverable.repo` = the shared checkout, its
briefing silent about it, and `scratch/` (91 porcelain entries / 161 files / 135 MB)
left in the operator's tree.

## The gap

The arm's original precondition was the row's own BRIEFING TEXT naming the shared
checkout (`TaskBriefingNamesDir`, epic `01a0db25`). The shape that actually leaked had
that predicate **false by construction**: the row's briefing named the repo's work,
never the operator's checkout, while its `deliverable.repo` was the checkout itself. So
the arm measured nothing, and no other arm could — `reclaimRunUnattributableGround`
excludes the main checkout on purpose (it is shared).

## Door 2: the DELIVERABLE, not the prose

`reclaimMainCheckoutDoor` decides in this order, and each silent case is a shape the arm
must not speak for:

| # | condition | outcome |
|---|---|---|
| 0 | the row's recorded ground (`deliverable.worktree`) IS the shared checkout | silent — that is the spawn-path refusal's shape (`TaskGroundMainCheckout`), not a deposit into somebody else's tree |
| 1 | the briefing NAMES the checkout (`TaskBriefingNamesDir`) | **door 1**, basis = the briefing; keeps the path proof per entry (the original behaviour, unchanged) |
| 2 | `deliverable.repo` IS the checkout (per `SameDir`) | **door 2**, basis = the repo + the run's own launch directory + the window |
| 3 | otherwise | silent |

Door 2's proof is **lineage, not time**: `reclaimRunLaunchedIn` reads the run's own
session (`session_start.workdir`) through the existing readers — the row's `session_id`,
`taskFindSessionJSONLForRow`, `taskWorkdirFromSession`/`taskLaunchRepo` — and requires it
to be the shared checkout. Only then does `reclaimWindowDeposits` attribute the untracked
entries inside the row's window (`in_progress_at → claimed_at → created_at`).

**Why lineage was required, measured during this work.** The first draft attributed by
window alone. The epic's own negative control — a row whose run launched in its OWN
worktree, with the operator's `operator-note.txt` appearing in the shared checkout inside
this row's window — **failed**: the arm filed the operator's file against an unrelated
row. A timestamp cannot tell "this run deposited" from "somebody else wrote while this
row ran"; that is precisely the misattribution the shared-tree design forbids. The
lineage witness is the fact that separates them, and it is a SECOND measurement rather
than a derivation (the deliverable is a claim about the TARGET; the session's own
`workdir` is where the process actually stood).

## What door 2 does NOT do

- Never attributes a **tracked** change: the untracked split (`reclaimUntrackedDirtyPaths`)
  is untouched, and the uncommitted-work guards are not referenced or weakened.
- Records **nothing** for a clean or tracked-only checkout (returns before any
  attribution), for a foreign row, or for a row whose run did not launch there.
- **Never mutates**: no delete, no move, no git write, no chmod, no reaper, no
  `.gitignore` entry. The regression asserts the checkout's `git status --porcelain` is
  **byte-identical** across the close-out, and that the deposit survives.
- The record states its basis in one sentence (`taskMainCheckoutDepositNote`), quotes the
  entries by their porcelain spelling, and adds the `scratch/` clause only when a
  measured entry names that root (`reclaimMainCheckoutScratch`, constant
  `reclaimMainCheckoutScratchName`).

## The fence's door: a legal evidence destination

The announcement (`TaskGroundAnnouncementClause`) tells a run that its worktree is the
ONLY tree it writes and says nothing about where evidence goes. Rule 22 forbids the `/`
partition, `.gitignore` entries and reapers were ruled out, and no directory in that run's
text was legal — so it invented `<repo>/scratch/`. A fence with no door is a fence that
gets routed around.

`TaskGroundEvidenceClause(taskID, ground)` renders the destination, and **invents
nothing**: it is the run's OWN session-artifacts directory, resolved through the same
resolver the report contract uses (`TaskReportPath` → `taskSessionDir`), i.e. the
directory `GOPI_TASK_REPORT_PATH` (`taskreport.PathEnv`) already points into. Off every
git work tree (no `git status` / `git add` / merge path sees it), per-task, and already
created by `SealTaskReport`. Appended in `cmd/agent/cli_tasks.go` **after** the authority
clause, so the run's own payload text is earlier. Exported label:
`tools.TaskGroundEvidenceLabel`.

## Verification

- Red-first, one file, pre-fix API only:
  `internal/tools/zz_redproof2_main_checkout_repo_door_test.go` — `FAIL` on master
  (detached worktree from the scratch queue, registration returned afterwards) and
  `PASS` on the branch; its invariant half (the foreign row records nothing) passes on
  BOTH trees, which is what forbids "measure everything".
- `internal/tools/task_main_checkout_deposit_test.go` — the door-2 regression plus the
  foreign-row negative control.
- `internal/tools/task_ground_announcement_test.go` — the destination clause.
- `cmd/agent/task_ground_announcement_e2e_test.go` — the clause reaches the child's FIRST
  model request, spelled from the CHILD's profile (`gopiHome`), not the ambient one.
- Live: `gopi task update t-livedep --status completed` against the built binary, event
  `completed-unclean-main-checkout-deposit` in the ledger, porcelain byte-identical,
  deposit intact; control row → 0 records.

## References

- `internal/tools/task_main_checkout_deposit.go` — the arm (both doors)
- `internal/tools/task_run_ground.go` — `TaskGroundAnnouncementClause` / `TaskGroundEvidenceClause`
- `cmd/agent/cli_tasks.go` — where both clauses are appended (announcement → authority → evidence)
- `devdoc/main-checkout-deposit-arm.md` — the first door's own record (epic `01a0db25`)
