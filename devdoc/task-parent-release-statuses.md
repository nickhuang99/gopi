<!-- doc-class: normative -->
# The parent gate's release set, and the `superseded` terminal status

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- census-evidence-ok: the figures in §1 are a live-ledger snapshot with the exact SQL and the
     measurement date stated; they count ROWS ON ONE HOST's live ledger at a moment in time, not the
     project's tree, so there is no commit-reproducible generator for them (Rule 38's NARRATIVE
     boundary: evidence of one measurement, not a project census). -->

Task `01a113a3-4aa6-73cf-9efc-250f738d57e0`, 2026-10-07, branch
`task/01a113a34aa673cf9efc250f738d57e0`.

## 1. What was wrong (measured, not asserted)

Rule 33's dependency gate releases a parked child only when the parent row's
status is in `taskParentReleaseStatuses`. Until this change that set was
`{completed, verified}` — and `cancelled` is TERMINAL (no status write leaves
it), so a child of a cancelled parent is parked **forever**.

`cancelled` was carrying TWO meanings. Measured on peerbox's live ledger,
2026-10-07 (read-only `sqlite3 -readonly "file:$HOME/.gopi/memory.db?mode=ro"`):

| figure | value | query |
|---|---|---|
| cancelled rows | **426** | `SELECT COUNT(*) FROM tasks WHERE status='cancelled'` |
| …of which park a pending child | **1** | `tasks p JOIN tasks c ON c.parent_task_id=p.task_id WHERE p.status='cancelled' AND c.status='pending'` |
| failed rows parking a pending child | 0 | same join with `p.status='failed'` |
| pending rows carrying a parent | 6 | `WHERE status='pending' AND parent_task_id != ''` |
| cancelled rows naming a migration in `reason` | 23 | `lower(reason) LIKE '%migrat%'` |
| cancelled rows naming a supersession in `reason` | 139 | `lower(reason) LIKE '%supersed%'` |

The one parked child's parent
(`01a10efc-760d-79e2-aadd-cf5681406c37`) carries the three-step migration
recipe verbatim in its `reason`: *"迁移到 HP：本行在 peerbox 侧取消，改由 HP 的
ledger 执行（新行 01a10f9c-…）… 不要在 peerbox 侧再执行本行。"* Its work IS done —
on HP's ledger, by a completed row — but the gate reads `cancelled` as
"dependency unmet", so the child never runs and the queue looks clean. That is
the Rule 15 silent failure: nothing errors, nothing warns, the row just waits.

So the defect is **small but systematically caused**: 1 row today, and a
standing trap for every future migration or supersession (162 cancelled rows
already describe one of the two).

## 2. Three candidate fixes, and why this one

**(a) Relax the release set to include `cancelled`.** One line, and wrong: it
releases children of a parent that was ABANDONED — a child built on a premise
that never held. The whole point of the gate is that an unsatisfied dependency
is never assumed satisfied. Rejected.

**(b) Release on a reason/tag heuristic** (e.g. `tags LIKE '%migrated%'`).
Rejected on two counts: it needs a SECOND judgement site — a predicate that
re-derives the release decision from row prose, drifting from
`taskParentGateDenied` (Rule 14) — and it makes a release depend on free text
a writer may phrase any way. `reason` is prose for a human; `status` is the
field the ledger's state machine owns.

**(c) A `superseded` terminal status in the release set. ← chosen.** It makes
the decision the ledger already encodes on every one of these rows into a NAME
the gate can read, with:

- ONE landing point (`taskParentReleaseStatuses`), ONE predicate
  (`taskParentGateDenied`) — nothing new to keep in step;
- the `[queue-skip]` prose re-rendered automatically by
  `taskParentReleaseSetLabel()`, which DERIVES its text from the map, so the
  sentence an operator reads cannot describe a different set than the gate
  enforces;
- `superseded` is ALREADY the ledger's word for "a later branch/commit carries
  the intent" (`archive_reason.ArchiveReasonSuperseded`, and
  `dirty_holder_salvage` already tells an agent to close a row "as superseded
  with the one-line reason"). The vocabulary existed; the STATUS did not.

The name is the decision ("this row's work is carried elsewhere"), deliberately
not the mechanism ("migration"), because a migration is supersession only
because a successor row exists — see the risk below.

## 3. The risk this opens, named rather than hidden

`cancelled` is the ledger's only status for "retire this row". A writer that
cancels a row whose work was NOT done and stamps `superseded` anyway now
releases that row's children onto nothing. Three things bound the damage:

1. **Nothing changes automatically.** No sweep, hook or daemon writes
   `superseded`; a row reaches it only by an explicit `gopi task update <id>
   --status superseded`, whose default reason ("work carried by a successor
   row — this row is retired as superseded") records the claim.
2. **The abandoned arm is untouched and pinned.** `failed` and plain
   `cancelled` still park their children, by a test that drives the real
   funnel (`TestParentGateCancelledParentParksChildForever`) — the relaxation
   this rule refuses is now a regression test, not a memory.
3. **The status is not the whole decision.** The successor row is the
   evidence: `gopi task send --cancel-source` already writes a reason naming
   the peer and the receipt, and a supersession names the successor. An
   operator reading `gopi task show` sees `superseded` *plus* the reason; a
   bare `superseded` with no named successor is a mis-stamp, and the reason
   column is where that shows.

Rejected alternative on the other side: minting the status but NOT putting it
in the release set. That fixes nothing — it would be a bookkeeping label.

## 4. Every consumer of the status vocabulary (the inventory)

`superseded` is terminal and behaves exactly as `cancelled` does everywhere
EXCEPT the release set. The list below is every non-test site that names a
terminal status or reads the terminal/live predicates, with its verdict. Sites
that were already correct by DELEGATION (`terminalTaskStatus`,
`isLiveTaskStatus`, `taskTerminalTier`, `taskStatusRank`, `validTaskStatus`,
`taskStatusWritable`) are therefore fixed by one edit each and are listed to
show that nothing was left implicit.

### 4a. The vocabulary itself — `internal/tools/tasks.go`

| site | change |
|---|---|
| `taskStatusSuperseded` const | **NEW** — the only new literal |
| `taskStatusSet` | **+ member** → renders the SQL `CHECK` (new tables) |
| `TaskStatusSuperseded` exported alias | **NEW** (Rule 1/13: consumers never type the literal) |
| `taskAllowedTransitions` | **+ key** (`superseded: {}`), and `pending`/`claimed`/`in_progress` each gain it as a target — a migrated row is QUEUED when it is retired |
| `taskStatusRank` | **+ `superseded: 3`** (terminal tier — sync convergence) |
| `terminalTaskStatus` | **+ member** (stamps `ended_at`; `taskTerminalTier` follows from the rank) |
| `taskTerminalStatusSQLList` | **+ member** (derive-from-constants list) |
| `taskDefaultReason` | **+ arm** (a lazy caller still gets a real reason) |
| `taskParentReleaseStatuses` | **+ member** — THE landing point |
| `taskParentReleaseSetLabel` | unchanged — it derives, so it re-renders itself |
| `taskStatusWritable` / `taskStatusListForCLI` / `validTaskStatus` | unchanged — all derived from `taskStatusSet` |
| `tasksNeedsRebuild` | **reverted, deliberately** — see §4c |

### 4b. Delegating consumers (fixed by the predicates above)

- `internal/tools/task_finish_cleanup.go` — `taskTerminalStatus` (close-out
  triggers: completed/failed only) and `taskCleanupEligibleStatus`, the set that
  additionally admits `cancelled`. The second is an EXPLICIT `|| status ==
  taskStatusCancelled` conjunct rather than a delegation, so `superseded` is
  added there by hand (below, 4d).
- `internal/tools/task_for_branch.go` — `Live: isLiveTaskStatus(status)` and
  `Terminal: taskTerminalStatus(status)`.
- `internal/tools/task_ground_reconcile.go` — same two.
- `internal/tools/branch_attribution.go` — `row.terminal()`.
- `internal/tools/worktree_sweep.go` — `row.terminal()`.
- `internal/tools/task_reclaim_self.go` — `taskCleanupEligibleStatus`.
- `internal/tools/merged_cleanup` (via `task_merged_cleanup`'s own switch —
  see 4d).
- `internal/tools/memory_sync_delta.go` — `taskTerminalTier(status) || verified`.
- `internal/tools/task_merge_backlog.go` — `mergeBacklogLiveStatus` (live set
  only; `superseded` is correctly NOT live).
- `internal/tools/ledger_read_guard.go` — `mergeLiveOwnerParks` reads `owner.Live` (the ONE
  live list), so a superseded owner parks nothing — correct by delegation. *(Measured
  2026-11-01: the function was cited at `cmd/agent/merge_run.go`, a file deleted with
  the top-level merge verb on that date; it lives in the file named above.)*

### 4c. `tasksNeedsRebuild` — why it was reverted

The first cut added a probe for `'superseded'` so a pre-change ledger would be
rebuilt into the wider `CHECK`. That was wrong for a reason worth recording:
**`testutil.NewTestDBPath` pins `GOPI_MEMORY_DB` at a fresh path, and a fresh
`memory.db` carries `memory`'s schema, not `tasks`** — so a probe that answers
"rebuild" for a normal DB turns every test's first ledger use into a rebuild of
a `tasks` table that does not exist yet, and the rebuild path is not built for
that. `tasksNeedsRebuild` already guards `ddl == "" → no rebuild`; the probe
would have made its answer depend on WHEN the table was created rather than on
its shape, and a genuine pre-`superseded` ledger simply keeps its narrower
`CHECK` (SQLite refuses only the NEW value, and nothing writes `superseded` to
an old ledger except an explicit operator command — which then fails loudly,
which is the correct outcome, not a silent one). The `CHECK` widens on the
same path it always has: a table created from `tasksSchemaTable()`.

### 4d. Explicit per-status edits (the sites that list statuses by hand)

| file | list | change |
|---|---|---|
| `internal/tools/memory_sync_row.go` (`syncRowWhyNotPending`) | completed/verified → "finished"; failed/cancelled → "terminal" | **+ superseded in the terminal arm** (a row send refuses it by name with a reason) |
| `internal/tools/memory_sync_claim.go` | 2 arms: terminal insert, terminal claim verb | **+ superseded to both** (a terminal verb is a history delivery) |
| `internal/tools/memory_sync_task.go` (`syncTaskForwardable`) | completed/failed/verified | **+ superseded** (it may forward-migrate a pending local row — the migration case) |
| `internal/tools/memory_sync_task.go` (`harvestUpdate` guard) | `status NOT IN (completed, failed, verified)` | **+ superseded** (the local-row-terminal guard) |
| `internal/tools/task_run_retry.go` (`TaskRowTerminalAt`) | `taskTerminalStatus(st) || st == cancelled` | **+ superseded** (a superseded row's exit code is not a retryable failure) |
| `internal/tools/task_finish_cleanup.go` (`taskCleanupEligibleStatus`) | `taskTerminalStatus || cancelled` | **+ superseded** (its recorded worktree must be recyclable — the stranded-row shape) |
| `internal/tools/task_reclaim.go` (`reclaimDeadHolderRowsFrom`, `status IN (?,?,?,?)`) | 4 params | **+ 5th param** (reclaim a superseded row's ground) |
| `internal/tools/worktree_sweep.go` (`tasksRowForSweep.terminal()`) | 4 cases | **+ superseded** |
| `internal/tools/task_merged_cleanup.go` (count query) | 3-status SQL list | **+ superseded** (the pass must be able to describe the row it will clean) |
| `internal/tools/repo_resolve_row.go` (`archiveOwnerArchivable`) | failed/cancelled may be archived without an override | **+ superseded** (the row already decided AGAINST keeping the branch) |
| `cmd/agent/cli_tasks.go` (`serveFinishCleanupQuery`, `serveFinishCleanupWorktreeQuery`) | 3-status SQL lists | **+ superseded** (the close-out sweep must reach it, or its worktree is stranded exactly as `cancelled`'s was before 2026-09-12) |
| `internal/agencyscan/agencyscan.go` (`terminalStatuses`) | mirrored map (leaf package, cannot import `internal/tools`) | **+ superseded** — the MIRROR is named here rather than hidden; the package imports nothing from `internal/tools` by design, so this is a deliberate second spelling, documented at the site |
| `cmd/agent/cli_tasks.go` (`tasksUsage`) | "terminal (completed / failed / cancelled)" | **+ superseded**, with a pointer to this doc |

### 4e. Sites deliberately NOT changed (and why)

- **`internal/tools/task_merge_backlog_terminal_guard.go`
  (`mergeBacklogSuccessTerminal`)** — governs only SUCCESS terminals
  (completed/verified) forcing a merge check. A superseded row is an
  abandonment of the branch, exactly like cancelled/failed, and the guard's own
  comment says abandoned rows "are left alone".
- **`internal/tools/archive_reason.go` (`archiveOwnerDecided`)** — completed/
  verified only: the statuses whose branch must never be archived without a
  reason. `superseded` means the opposite (the branch is spent).
- **`internal/tools/tasks.go` `taskTerminalClaimStatuses`** (completed,
  verified) — the statuses that CLAIM a run delivered into a ref. A superseded
  row claims nothing about this branch.
- **`internal/tools/tasks.go` `TaskArchive`** — it gates on
  `terminalTaskStatus`, so `superseded` is archivable as resolved by
  delegation. Its error text still enumerates the old three; that text is
  illustrative prose in a refusal, and the predicate is the gate.
- **`internal/tools/taskreissue.go`** — the `failed-needs-reissue` line. It
  reads `status='failed'` only and is out of scope by the briefing (Rule 26:
  narrow, never wrong).
- **`internal/agent/tasklist.go` / `internal/gopilog/formatter.go`** — the
  in-session TODO-list vocabulary (`pending`/`progressing`/`completed`), a
  different thing from the ledger's status set; sharing a word, not a value.
- **`internal/agent/taskregister.go`, `cmd/agent/cli_tasks.go` writeback
  paths** — they WRITE `failed`/`completed` for a run's outcome. `superseded`
  is never a run outcome; no write site changed.
- **`task_adoption.go`, `notifyfeed/unhandled.go`, `taskreport/*`** — their
  `completed`/`failed`/`verified` literals are their own vocabularies
  (adoption pairing, notification buckets, report verdicts), not the ledger's
  terminal set.

## 5. Verification

- `go build ./...` — green.
- `go test ./internal/tools/ -run 'ParentGate|TaskStatus|Claim' -count=1` —
  green, including the four new tests:
  `TestParentGateSupersededParentReleasesChild`,
  `TestParentGateSupersededParentPendingRowReleasesChild`,
  `TestParentGateSupersededParentListingFollowsTheMap`, and the exhausted
  release-set assertion inside `TestParentGateDeniedIsTheOnlyDecisionPoint`
  (which now walks EVERY member of `taskStatusSet`, so a future status must be
  decided rather than defaulting into "parks forever" unnoticed).
- `make test-budget PKGS='./internal/tools/'` — the branch-scoped smoke of the
  packages the diff touched.
- The Rule 31 run — a REAL binary (`go build -o … ./cmd/agent/`) against a
  real ledger in a sandbox (`gopi tool sandbox create --label parentrelease`),
  with the three directions exercised through the CLI, not through a test:

  | parent | written how | result |
  |---|---|---|
  | `p-release` | `task update --status superseded` (pending → superseded: the migration shape) | `c-release` **RELEASED** — the pending view drops its marker and the worker claim takes it |
  | `p-cancel` | `task update --status cancelled` | `c-cancel` **PARKED** — `[queue-skip] parent p-cancel not released (status=cancelled; releases on completed\|superseded\|verified)` |
  | `p-fail` | claimed → in_progress → failed | `c-fail` **PARKED** — same marker, `status=failed` |

  Before the writes, all three children were parked (`status=pending` in the
  marker) because all three parents were still pending — the fixture's own red.
  The claim itself was observed, not inferred: `gopi worker --once` printed
  `worker: claimed c-release` and the sandbox ledger afterwards read
  `c-release | failed | peerbox:3734842` (the child then failed as a *task*, in
  its own ground, which is its own business — the GATE released it, which is
  what this change is about), while `c-cancel` and `c-fail` stayed
  `pending / claimed_by=NULL`.

  The derived label is visible in the operator-facing text above:
  `releases on completed|superseded|verified` — the map, re-rendered, with no
  prose edit anywhere.

  A fourth direction, because "released" must not become a blanket licence:
  a row that already reached a verdict refuses the new status BY NAME —
  `gopi task update p-fail --status superseded` →
  `task: illegal status transition failed → superseded (allowed from failed:
  verified|completed)`. And a typo is refused with the whole vocabulary, so the
  new value is discoverable from the error:
  `task: invalid status "supersededx" (want one of
  pending|claimed|in_progress|completed|failed|cancelled|verified|superseded)`.

  A note on the run's side effects, kept honest: the worker created the ground
  `/home/user/workspace/gopi-crelease` on branch `task/crelease` (the row's own
  worktree) and then failed the child there; the worktree was removed and the
  branch deleted after the measurement, so nothing of the run is left in the
  repo or in the main working area. The sandbox is reclaimed by
  `gopi scratch-gc sandboxes` — never by a hand-written cleanup hook (Rule 34).

## 6. The rule this leaves behind

**A status set is read by more places than the file that defines it.** The
defect was not that `cancelled` had the wrong meaning; it was that a MEANING
`suspended` across two facts (denied / parked) needed a NAME before any gate
could read it. When a status acquires a second meaning, mint the name in the
same change — and then walk every consumer, because the compiler will not.
