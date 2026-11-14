<!-- doc-class: historical -->
# Fixture-Gate Live Verification — 2026-10-13 (a rig's proof ledger, not a standing claim)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** measured once, on 2026-10-13, in one sandbox profile
(`GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-fixture-gate-c1cf907a`,
binary md5 `519edda1f30a0cfa0333dc5165c7b183`).
**This is a RECORD of that run, not a standing claim about master.** The
mechanism itself is designed in `devdoc/fixture-row.md` and pinned by
`internal/tools/task_fixture_gate_test.go`; this file records only the LIVE
measurement of it, taken from a real ledger with a real rig-seeded fixture row
beside real work rows.

## What was measured (the rig's question)

A rig seeded one marked fixture row plus normal rows into its own profile and
asked the claim funnel one question: **does a real row still get offered beside a
parked fixture row, and does the fixture row stay decoration?**

Two facts answer it, both read off the SAME ledger, seconds apart:

| row | created | status | tags | ground / run |
|---|---|---|---|---|
| `01a132d6-5ac1-…-a178f` "SEEDED FIXTURE — must never be claimed" | 09:17:51 | pending | `fixture` | none — no run log; parent `—` |
| `01a132ed-bfcb-…-bcc4e` "SECOND REAL WORK — the funnel must offer this row" | 09:43:24 | in_progress (then failed, see §Anomaly) | — | branch `task/01a132edbfcb7190a8d9a030a96bcc4e` @ its worktree, recorded + resolved live |

The fixture row's `created_at` is the OLDEST of the pending rows — i.e. a FIFO
reading of the queue would take it FIRST. The claim funnel took the newest-tier
oldest NORMAL row instead, and the pending view names it:

```
$ gopi task list --status pending
#01a132ed-cce6-…  [pending]  SECOND REAL WORK — …   [next]
#01a132d6-5ac1-…  [pending]  SEEDED FIXTURE — must never be claimed  [queue-skip] fixture row — the reserved fixture tag marks queue decoration, never work: no dispatcher will claim it (drop the tag to make the row real work)
```

Both rendered facts come from ONE predicate, so the marker cannot disagree with
the funnel (`internal/tools/tasks.go`: `taskQueuePositionSuffix` appends
`TaskQueueSkipMarker + taskQueueSkipReason` for exactly the rows
`taskRowClaimable` refuses; the fixture arm is checked FIRST in the reason, so a
fixture with an un-released parent is not named for a repair that would not make
it claimable).

## The rule-33 corroboration (why `[next]` names a row OTHER than this one)

This run's own row was ACCEPTED by the funnel at 09:43:49, when the pending set
was `bfcb(09:43:24) < cce6(09:43:27) < f9b9(09:43:39)`. The queue order is
`priority ASC, created_at ASC` (`taskClaimOrderBy`), so the funnel took the
OLDEST — this run's row — and `[next]` correctly names `cce6`, the oldest row
STILL pending at capture time. The marker is a live read of the funnel's own
order, not a line number in the render.

## The negative half — what the same ledger shows on a binary WITHOUT the arm

`/usr/local/bin/gopi` (at capture time md5 `2ca5ac762313011522b66842962f1ddd`,
`--fixture` absent from its help) on the SAME ledger marks the fixture row
`[next]`:

```
$ /usr/local/bin/gopi task list --status pending
#01a132d6-5ac1-…  [pending]  SEEDED FIXTURE — must never be claimed  [next]
```

Same rows, same ordering, no fixture arm → the fixture row is the row the funnel
would take. That contrast is what makes the sandbox build's output attributable
to the predicate rather than to the row's text or the view's ordering.

**Re-measured after the system binary was rebuilt (evidence addendum E10).** The
`2ca5ac76…` build no longer exists on disk — `/usr/local/bin/gopi` is a symlink to
`/home/user/work/GoPi/bin/gopi`, which was rebuilt at 09:49:38 (i.e. after the E1
reading, before this file was written) to a different build, md5
`c0d79e9ba365d8709429cfa30f78b3d1`. The negative half is therefore re-taken on the
binary that exists NOW, same ledger: the fixture row is rendered with **no
`[queue-skip]` marker at all** (`[queue-skip]` does not occur in its output),
while the sandbox build prints the refusal sentence on the same rows. The
attribution thus does not rest on a vanished hash; the *behaviour* difference is
re-measured, not the hash. (The `2ca5ac76…` hash is kept above only as the value
recorded in E1 at capture time.)

## This run's own claim (the funnel claimed a NORMAL row)

`gopi task show 01a132ed-bfcb-…` at 09:43:58 (4.4 s after the claim) recorded:
`run_method: worker`, `run_cmd: gopi worker --once`, `claimed_by:
hosta:318794`, `deliverable.branch/worktree` recorded and re-resolved live
(`verified live`), and the row's own run dir carrying a session + raw log.
**No second claim was run** — the rig executed `gopi worker --once` exactly once
(09:43:44), so no sibling row's LLM round was spent proving this.

## Anomaly measured in the same window (Rule 41)

This run's OWN ground was removed at 09:46:10 by the arm `task-self-reclaim`,
recording `evidence: "this run's own finished row … disposition reclaimFreable:
"` — with an EMPTY reason, while the row was non-terminal (it moved to `failed`
at 09:46:07, i.e. three seconds earlier, and the holder pid had not written its
completion) and while pid 318794 (the row's live holder, which wrote that very
audit line) was working inside the directory. That is the state
`internal/tools/task_reclaim.go` describes in guard ⓪ (the live-row veto, asked
FIRST and on the ledger alone) and guard ⓪″ (a live-status row's ground is not
that arm's to free) — the arm's own refusal markers
(`reclaimSelfRefusedMarker`) are absent from the line. This run restored the
ground with an idempotent `git worktree add <path> <branch>` and re-verified
`git status --porcelain` empty before closing; the full trace (audit lines,
`worktree list` after the removal, `/proc` readings) is filed with the task
report and a bug row was submitted for the anomaly.

## Reproducing / re-measuring

```
gopi task submit "<row>" --fixture              # seed the decoration
gopi task list --status pending                 # [queue-skip] + its reason; [next] on a real row
gopi task show <fixture-id>                     # tags: fixture, runs: none
```

The three assertions worth re-running after any change to the claim funnel: (1)
the fixture row is `pending` with the `fixture` tag and no run; (2) it carries
`[queue-skip]` plus the FIXTURE sentence, never the parent sentence; (3) a normal
row is still offered (`[next]`) and a dispatcher can claim it.
