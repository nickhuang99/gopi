<!-- doc-class: historical -->
# The close-out header's count and its rows are ONE population

**Epic** 01a12bc0-d6a2-7cf0-b33b-cf94342470d2 · **Branch** `task/01a12bc0d6a27cf0b33bcf94342470d2`

## The defect (measured, not inferred)

`gopi task list --status <st>` printed a close-out header whose number came from a
**different population** than the rows under it.

`taskListCloseoutSignals` (`internal/tools/task_list_closeout_signal.go`) read the
ledger with a single query taken before the row cursor opened — `task_events`, newest
row per `task_id`, filtered to one event name — and **never joined `tasks`**. The row
walk below it applied `WHERE status = ?`. So the number in the sentence counted the
whole ledger while the list showed one status.

Measured on `hosta`, 2026-10-12, master binary (`/home/user/work/GoPi/bin/gopi`):

| view | header | marker lines on screen |
|---|---|---|
| `--status failed` | `close-out refused on 54 row(s)` | **0** |
| `--status cancelled` | `close-out refused on 54 row(s)` | **1** |
| `--status pending` | `close-out refused on 54 row(s)` | **0** |
| `--status in_progress` | `close-out refused on 54 row(s)` | **0** |
| `--all` | `close-out refused on 54 row(s)` | 54 |

Worse than a wrong number: the sentence carried **no scope clause at all**. "close-out
refused on 54 row(s) — … each affected row is marked [closeout-refused] with its
reason:" reads as *"of the rows below"*, and on a filtered view that reading is false
for every row on screen. The operator has to open 54 rows (`task show`) to discover the
list is not the population the header counted. The number being ledger-wide is a
defensible design; the surface not saying so is not.

## The fix — two halves, one domain

1. **The read is scoped like the walk** (`taskListCloseoutSignals(ctx, db, statusFilter)`).
   A non-empty filter appends `closeoutSignalsViewScopeClause`
   (`AND e.task_id IN (SELECT task_id FROM tasks WHERE status = ?)`) — the tasks-table
   predicate the row walk itself applies, spelled beside it on purpose so a divergence
   is visible. An unfiltered render keeps the ledger-global query: there is no filter
   to share and every row is about to be walked.
2. **The count is what was PRINTED.** `TaskList` increments `markedCloseout` in its own
   row loop and renders `taskListCloseoutNote(markedCloseout, scope)`. The header number
   is now the number of marker lines on screen under it — the arithmetic a reader can
   redo by eye — and `closeoutScope` names the population in words
   (`taskListCloseoutScopeLedger` = "across the ledger", `taskListCloseoutScopeView` =
   "in this view").

Both scope phrases and both SQL fragments are named constants (Rule 1b/13). The read
still runs **before the row cursor opens** — the single-connection constraint
(`internal/tools/memory.go` `SetMaxOpenConns(1)`) is unchanged, and nothing queries
per row.

## The consequence clause removed in the same line (Rule 41)

The header also asserted **which half of the close-out chain had stopped**: "the
delivery chain stopped before the push, so the branch was NOT removed". That claim is
not derivable from what this reader holds. `taskListCloseoutSignals` keys on ONE event
name, `finishCleanupRemovalRefusedEvent`, written by **both** the removal-refusal arm
and the removal-not-reached arm (`internal/tools/task_finish_cleanup.go`) — so the
render knows exactly one thing: the row's newest recorded verdict is a refusal. On this
host 49/49 marked rows carried the *same* reason, `"removal skipped (delivery already
attempted): the recorded worktree directory is not there"` — i.e. the push had already
run and the removal was an idempotent no-op, the opposite of what the header said for
every row under it.

The replacement states the population and points at the per-row reason:
`"close-out refused on %d row(s) %s — each affected row is marked %s with its reason:"`.
Which half stopped, and what survived it, remain in the row's reason text, where each
refusing arm's own remedy already lives.

**The sibling epic (01a12bad) had already landed this wording — on the PEER's master,
and my branch could not see it.** This is worth recording because two readings of it
were wrong before the third was right (commands + raw output in the report's
`## Evidence`):

- **Reading 1 — WRONG.** `git branch -a --contains 74df98f37` printed nothing and
  `git cat-file -e 74df98f37` succeeded, which this run first wrote up as "dangling,
  reachable from no ref".
- **Reading 2 — WRONG.** `git branch -a --contains 74df98f37` listed
  `remotes/origin/task/01a12bada8ac7b778964227c9314ab75` and `git ls-remote` showed the
  peer's branch pointing at the commit, so the claim narrowed to "on a branch, not on
  master" — measured against **local `master`**, which was 83 commits stale.
- **Reading 3 — the measurement.** `git ls-remote origin refs/heads/master` →
  `9bae8416c`; the object is absent locally; `git fetch --no-tags origin master` →
  `52e4c474`, and `git merge-base --is-ancestor 74df98f37 FETCH_HEAD` **exits 0**. The
  peer's master contains it, and its sentence already reads "across the ledger".

The lesson is the claim's own: **"is X on master?" is not answerable from a local
checkout until the local `master` is established to BE the master you mean.** The old
falsified sentence was live in *this branch's base* (`ec7342d72`), not in master — which
is why a rebase onto the current master, rather than this epic, would have erased it.

**The two epics conflicted, openly.** `TestTaskListCloseoutHeaderCountIsLedgerWideNotViewScoped`
(landed with 01a12bad) **pinned the measured defect as correct behaviour**: "the count
comes from one ledger-wide query taken before the row cursor opens, so on a FILTERED view
it can exceed the marked rows on screen — and that is correct, not a bug … so a future
reader does not 'fix' the difference by scoping the query to the filter". That is exactly
what epic 01a12bc0 does. Resolution: this epic's scope fix wins (the operator's goal), the
peer's *wording* is kept and extended with the scope clause, and the peer's wording-guard
test (`TestTaskListCloseoutHeaderClaimsNoHalfOfTheChain` + its banned-claim list) is kept
as-is. The replaced test is named in the merge commit and in the report, so the reversal
is auditable rather than silent.

The correction is carried forward here by rewriting the same line, so the wording now
sits on a branch whose merge is this epic's own delivery. Backward-looking checks only;
no ref was altered here (Rule 23).

## Boundary — what this did NOT change

- **A superseded snapshot elsewhere stays as it is.** `devdoc/silent-degradation-four-cases.md`
  (case ①) quotes the OLD header text as that run's real-run observation, and it is
  classified `historical` in `DOC-INDEX.tsv` — the record of a past run, which is not
  a claim about master and is not rewritten from here (Rule 36). A reader who lands on
  that quotation should read it as what the surface said *then*; the live sentence is
  the constant in `task_list_closeout_signal.go`.
- **No gate.** This file is a reader plus a rendered marker; the close-out chain refuses
  exactly as before.
- **The archived (resolved) filter still runs after the render** (`TaskListVisible` →
  `FilterArchivedRows`), and a hidden row takes its marker with it. The header is
  stamped from what `TaskList` printed, so an `--all` render is the one shape where the
  printed count legitimately exceeds the *visible* rows — the difference belongs to the
  archive filter, which says so on its own `[hidden]` line.
- **A false-positive extraction trap, worth recording**: an unanchored
  `grep 'close-out refused on'` matched a **row summary** that quoted the header phrase
  (this epic's own ledger row), so the evidence recipe anchors the header at line start
  (`grep -oE '^close-out refused on [0-9]+ row'`). Counting markers likewise matches the
  tail's shape (`\[closeout-refused\](: |$)`), never the bare token.

## Verification

- Red proof: the new tests, run against the pre-change sources, fail with the measured
  shape (`header says 3 row(s) while 0 marker line(s) are on screen`); transcript
  `red-proof.txt`.
- Green: `go test ./internal/tools/ -run TestTaskListCloseout` ok.
- Real run, same render, before/after table: `artifacts/after-scope-fix.txt`.
