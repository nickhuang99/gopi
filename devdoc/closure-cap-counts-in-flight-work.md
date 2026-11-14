<!-- doc-class: historical -->
# The closure cap counted HISTORY, not in-flight work (2026-10-22, task 01a1638f)

## The defect, measured

`internal/tools/task_verify_closure.go` bounded its starvation guard with

```sql
SELECT COUNT(*) FROM tasks
 WHERE status = 'completed' AND tags LIKE '%bug%' AND (claimed_by != '' OR run_method != '')
```

— the ledger's HISTORICAL population of completed auto-dispatched bug rows. That
number only grows (measured: 244 rows on hosta, 601 on peerbox) and nothing in
the loop can drain it. The constant's own doc says what the bound was FOR: *"when
this many completed bug rows are already awaiting (or holding) a verdict … at 100
closures each waiting on a full model session"*, and *"a normal working queue NEVER
hits it (measured steady state: a handful of closures in flight at once)"*. The
unit the sentence is written in is **in-flight verification work**, and the query
could not see one such row: `enqueueVerifyClosure` tags its rows with the marker
alone (`verify-closure`, no `bug`) and inserts them `pending`.

Result: `n >= cap` was permanently true, so every new close printed

```
[verify-closure] 602 bug rows are already awaiting a verdict (cap 100) — not enqueuing verification for <id>; drain the verify backlog first
```

and wrote `status=completed` anyway (exit 0). The verify loop had stopped, silently,
for every close after the 100th completed bug row — and could not self-heal, because
the backlog it named has no drain mechanism.

## The fix: the counting unit is in-flight work

```go
func inFlightClosureCount(ctx, db) (int, error) {
	// status IN (pending, claimed, in_progress) AND the marker tag as a TOKEN
	`... WHERE status IN (?, ?, ?) AND (','||replace(COALESCE(tags,''),' ','')||',') GLOB ?`
}
```

Three deliberate choices:

1. **pending + claimed + in_progress**, not just pending: a closure row the queue
   has claimed, or a verifier is judging right now, still occupies a slot. Counting
   only `pending` would let the loop queue a fresh verifier for every row already
   under judgement and re-create the flood the cap exists to prevent.
2. **terminal closure rows free their slot.** `verified` / `failed` / `completed`
   are not in-flight — this is what makes the guard SELF-CLEARING, which is the
   property the pre-fix count lacked (and the reason the defect was permanent).
3. **a comma-anchored GLOB on the marker tag**, never `LIKE '%verify-closure%'`
   (Rule 14): the tag set is comma-split, so `verify-closure-old` must not count.

The message was reworded with the unit: `N verification closures are already in
flight (cap 100) — … the cap clears as those verifiers finish`. The old sentence
named a number an operator could not act on; the new one names a number that
returns to 0 by itself.

## What the guard still does (unchanged)

The cap is kept at 100 and still refuses the enqueue — that half was never broken.
`TestVerifyClosureGateStarvationGuard` now seeds 100 **pending closure rows** (the
in-flight population) and asserts the refusal; `TestClosureCapCountsClaimedAndInProgressClosures`
seeds 99 pending + 1 claimed + 1 in_progress and asserts the refusal too.

## Provenance, and the honest gap

Real CLI, sandbox ledger (`gopi tool sandbox`), two binaries built from the same
tree (pre-fix = `git checkout HEAD --` the file):

| binary | seeded ledger | `task update <bug> --status completed` |
|---|---|---|
| pre-fix | 101 completed-bug, 0 in-flight | `102 bug rows are already awaiting a verdict (cap 100) … drain the verify backlog first`; **0** closure rows |
| fixed | 102 completed-bug, 0 in-flight | no cap line; queued `01a163f2-8458-7b41-a995-ecd9f1efb571` (pending, parent = judged row) |
| fixed | 104 completed-bug, **101 in-flight** | `101 verification closures are already in flight (cap 100) … the cap clears as those verifiers finish`; no row |

**Not done here:** the historical backlog. It is not the cap's input any more, so it
needs no drain for the loop to work — but those rows will never be verified by it
either. `devdoc/bug-verify-closure-20260912.md` §backfill already ruled the triage
shape (bucket by artifacts: branch liveness on each peer × dispatch provenance;
~94% unverifiable in principle). That triage is unchanged and still un-run; the
fix does not create a sweep and deliberately does not.

**Doc drift left standing on purpose:** `devdoc/bug-tag-population-token-match.md`
says "`awaitingVerifyCount` still carries the same coarse `tags LIKE '%bug%'`".
That was true when written and the function no longer exists — the sentence is a
dated record of an unmerged branch, not a claim about today's tree. Editing it
would rewrite a measurement.
