<!-- doc-class: normative -->
# The delivery-retry census arms: TRIGGERS, and the producer omission they exposed

> **RETIRED MECHANISM, as of 2026-10-30 (task 01a18799, slice 2/4).** The
> `gopi task retry-delivery` VERB was retired and `cli_tasks_retry_delivery.go` (deleted with the verb)
> was deleted with it. The CENSUS ARMS this page is about are still live
> (`DeliveryRetryTaskID` / `DeliveryRetryLines` / `DeliveryRetryJSON`, exported by
> `internal/tools/task_delivery_retry.go`, which finish-cleanup's delivery-retry leg
> reads), so the gate's obligation survives; what moved is the PRODUCER the
> classification proof names — `cwdReleaseDeliveryRetryProducer` is now
> `cmd/agent/cli_tasks.go`, whose call sites are `:4607` / `:4610` and which carries
> the release at `:3697` and `:6448`. Read every `cli_tasks_retry_delivery.go`
> reference below as the historical producer, not the current one.

**Task**: 01a12f00-0522-7093-8257-fe2d5a0c4dd0 (rework of 01a12ea0-4087-e548-65a2-df0004-80cd4, reconciling sibling 01a12ebe-6d3b-739c-aa65-122cf48d3592) · **Branch**: `task/01a12f00052270938257fe2d5a0c4dd0`
**Gate**: `internal/standards/cwd_release_gate_test.go`

## The red

```
--- FAIL: TestGateCwdReleaseCensusArmsAreListed (0.31s)
    cwd_release_gate_test.go:816: census derived 3 removal arm(s) that this gate
    neither triggers on nor records as deliberately exempt: [DeliveryRetryJSON
    DeliveryRetryLines DeliveryRetryTaskID] — a caller of one removes a linked
    worktree with no release obligation attached.
```

*(Method note: this output was captured at the commit that added
`TestGateCwdReleaseCensusArmsAreListed`, and **`TestGateCwdReleaseCensusArmsAreClassified`
— named later on this page as the old read proof — was RETIRED 2026-10-31
(`4ab33ce8`) and does not exist at HEAD**; do not `-run` it. The live read proof
is `TestGateCwdReleaseDeliveryRetryRetirement`. The gate's current runnable legs
are `TestGateCwdReleaseFindsTheRealTree` (`internal/standards/cwd_release_gate_test.go:825`),
`TestGateCwdReleaseCensusArmsAreListed` (`:1037`),
`TestGateCwdReleaseCensusFindsTheRealArms` (`:1108`) and
`TestGateCwdReleaseDeliveryRetryRetirement` (`:1214`) — each measured with
`grep -c '^=== RUN'` ≥ 1.)*

## How the gate got here (the mechanism, not the symptom)

The census (`cwdReleaseCensus`) walks every production file of `internal/tools`,
indexes each function's CALLS, seeds from the functions whose bodies carry the
removal verb (`cwdReleaseRemovalVerb`: the `git worktree remove` verb pair, or
the `reclaimGitRemove(` helper), and then **closes over CALLERS** — a function
that calls a remover IS a remover. Exported names of that closure are the arms.

`internal/tools/task_delivery_retry.go` reaches a worktree removal **only
transitively**, through `TaskFinishCleanupSessionID` — the close-out chain:

```
DeliveryRetryTaskID  → deliveryRetryOne → TaskFinishCleanupSessionID → … → reclaimGitRemove(
DeliveryRetryLines   → deliveryRetryRunRows → DeliveryRetryTaskID
DeliveryRetryJSON    → deliveryRetryRunRows → DeliveryRetryTaskID
```

`TaskFinishCleanupSessionID` was **already a trigger**, so no scanned-tree file
could reach a removal *unseen*. What was new was three exported names in the
closure with no entry — the census reporting a **gap in its own classification
table**, not a live violation. That distinction decided the answer (below).

## The decision, per arm

**All three are TRIGGERS** — `cwdReleaseDeliveryRetryArms`, folded into
`cwdReleaseRemovalTriggers`. Not one is recorded in `cwdReleaseArmsNotListed`.

**Why not an exemption**, though the arms' own bodies remove nothing:
`cwdReleaseArmsNotListed` is matched on the **derived name**, so an entry there
exempts the whole **function**, not the one call inside it. These functions call
the close-out chain, which removes the **row's own linked worktree** — the
directory a producer may be standing in. That is the exact category the gate's
own history forbids exempting (the 2026-09-26 close-out-seam entry on
`cwdReleaseRemovalTriggers` carries the same reasoning), so an exemption would
be a wider silence than the reason could justify.

**Why a trigger is not merely conservative — it is correct here**: arms are not
what this gate reports, **files** are. `TestGateCwdReleaseFindsTheRealTree`
demands a release of a scanned-tree *file* that has a trigger and no
`cwdReleaseMarker`. Adding the arms to the trigger list made the scan below light
up, and it lit up on a **real omission**.

## The omission the classification exposed (the reason it is a fix, not a tidy-up)

`cli_tasks_retry_delivery.go` — the `gopi task retry-delivery` face, added by the same
commit that added the arms and deleted with the verb it implemented on 2026-10-30 —
carried **no `worktreerelease` call**. That verb exists to re-run the close-out chain on rows whose delivery a
gate refused — the chain that removes the row's worktree. Run from inside the
worktree it retries (which is an ordinary operator invocation), the process
keeping its cwd there **pins the removed directory's inode**, and finish-cleanup
guard ③ then refuses that branch forever.

The chain does not release on the caller's behalf, and *cannot*: the chdir
obligation is the **producer's** — `internal/worktreerelease` is the caller's
own chdir, because Linux has no syscall to move another process's cwd. So the fix
lands at the producer:

```go
// cli_tasks_retry_delivery.go (deleted with the verb), runTaskRetryDelivery
worktreerelease.ReleaseReporting(os.Stderr, retryDeliveryProg)
```

This is the same shape as the other faces on the list (`cli_tasks.go:3174`,
`cli_resume.go:680/:786`) and adds no new dependency (Rule 35): one import of the
existing leaf package and one call.

## Read proof — the classification WAS measured, and its subject is now retired

> **Retired mechanism (2026-10-31, deleted by `4ab33ce8`).**
> `TestGateCwdReleaseCensusArmsAreClassified` **no longer exists**. Running it
> matches nothing — this is the vacuum green, not a passing gate:
>
> ```
> $ go test ./internal/standards/ -run '^TestGateCwdReleaseCensusArmsAreClassified$' -count=1 -v | grep -c '^=== RUN'
> 0
> $ go test ./internal/standards/ -run '^TestGateCwdReleaseCensusArmsAreClassified$' -count=1 -v | tail -2
> testing: warning: no tests to run
> PASS
> ```
>
> The arms it classified stopped being removers (the close-out
> chain ask was retired one slice earlier), so all four legs below are false by
> construction. **The live read proof today is
> `TestGateCwdReleaseDeliveryRetryRetirement`** (defined at
> `internal/standards/cwd_release_gate_test.go:1214`):
>
> ```
> $ go test ./internal/standards/ -run '^TestGateCwdReleaseDeliveryRetryRetirement$' -count=1 -v | grep -c '^=== RUN'
> 1
> ```
>
> Its four legs (still a live face / no production caller / in neither list /
> note stays attributable) are tabulated in the retirement section at the end of
> this page. The four legs listed below are **the historical claim of the page**,
> kept because the reasoning they were written for is what the retirement had to
> answer — read them as past tense, never as something to run.

`TestGateCwdReleaseCensusArmsAreClassified` (new at the time; **RETIRED 2026-10-31,
replaced by `TestGateCwdReleaseDeliveryRetryRetirement`**) walked
`cwdReleaseDeliveryRetryArms` and failed on each way the decision could rot:

1. **Still derived** — an arm the census stops deriving makes its trigger entry
   dead weight in a green run.
2. **Not also exempt** — a name in both lists reads as coverage while
   suppressing it.
3. **Still reached** — a scanned-tree file must still CALL one of the arms in
   code (comments stripped, the same code-only rule the release marker uses), so
   the claim "this is a producer obligation" cannot outlive its producer.
4. **The producer releases** — `cwdReleaseViolation` on the named producer, i.e.
   the leg that would have caught the original omission.

The three arm names are spelled **once** (`cwdReleaseDeliveryRetryArms`, split by
the existing `cwdReleaseTriggerSep`), so the trigger list and the read proof
cannot drift into two spellings (Rule 1b) — a zero-byte separator cannot be
forged by splitting across entries.

## Attribution (bisected, with the mechanical evidence)

One commit, and it is the only commit that ever touched the file:

- `git log --follow --oneline -- internal/tools/task_delivery_retry.go` →
  **only** `da2a97063`.
- `git show da2a97063^:internal/tools/task_delivery_retry.go` →
  `fatal: path … exists on disk, but not in 'da2a97063^'` — the file **did not
  exist before** that commit.
- `git ls-tree da2a97063 -- internal/tools/task_delivery_retry.go` → present
  (blob `0b56afdaa4c740739f3f78394b05c7629e443d11`).
- The gate file was last touched **`8b9e14c09`** (2026-09-26), **before**
  `da2a97063` (2026-10-12 11:51:34 +0800) — so the gate had never had the chance
  to list these arms; the census red is a **new-arm** red, not a regression in
  the gate.

## What was deliberately NOT done

- `cwdReleaseRemovalVerb` was **not** widened, and `cwdReleaseArmFloor` (10,
  measured 22) / `cwdReleaseReadFloor` (4, measured 7) were **not** lowered.
  Both floors were probed *upward* (99) to print the true counts — the
  established method for proving a non-vacuity floor is honest rather than
  tuned to fit a red.

## Reconciliation with master (2026-10-12, task/01a12f00-0522)

Both branches that carried this decision — `task/01a12ea040a87e54865a2df000480cd4`
(tip `2dcd8fe008`) and this doc's own `task/01a12ebe6d3b739caa65122cf48d3592`
(tip `7a3f6be70d`) — collided with master on `cli_tasks_retry_delivery.go` (deleted with the verb)
and `internal/standards/cwd_release_gate_test.go`, and master had **already landed
the same body of work** in `6965883fc` ("close master's 4 standing internal/standards
reds") while they were open:

- the three arms are in `cwdReleaseRemovalTriggers` on master (same three names,
  three literals),
- `cli_tasks_retry_delivery.go` (deleted with the verb) **does** carry the release on master
  (`worktreerelease.ReleaseReporting(os.Stderr, retryDeliveryProg)`, one call for
  the whole invocation, in a body that RETURNS its exit code so the release fires
  on both failure legs),
- `COMMANDS.md`'s generated block was regenerated there.

So the replay does not re-add master's semantics. What master did **not** carry is
the sibling's structure and its read proof, and that is what this landing takes:

| piece | master `05ed336b3` | this landing |
|---|---|---|
| the three arm names | three literals in the trigger list | spelled ONCE (`cwdReleaseDeliveryRetryArms`), split by `cwdReleaseTriggerSep`; the trigger list and both proofs read that constant |
| the producer | unnamed | named (`cwdReleaseDeliveryRetryProducer`) and **measured** — exists, calls an arm in code, and references the release |
| mutual exclusion | per-arm loop over two hand-kept names | over every arm the walk DERIVED (`TestGateCwdReleaseCensusArmsAreListed`) |
| the classification itself | prose on the trigger list | `TestGateCwdReleaseCensusArmsAreClassified`, four legs (derived / really triggered / not also exempt / the producer calls an arm *and* releases) — **RETIRED 2026-10-31, see the section at the end of this page**: its subject (a producer reaching these arms) no longer exists, and `TestGateCwdReleaseDeliveryRetryRetirement` is the read proof in its place |

**Not dropped, and not duplicated**: the sibling's `COMMANDS.md` line-number refresh
was superseded, not merged — master's block had drifted a second time
(`cli_doccheck.go:246` → `:297`), so it was regenerated with the repo's own
generator (`internal/standards/testdata/commands-census.go`) rather than replayed
with the branch's stale numbers (the generator names itself; Rule 38).

**The producer's release is the same obligation, not two of them.**
`retryDeliveryProg` (master) and the older branch's `retryDeliveryProgTopLevel` are
one spelling each of the same string — `"gopi task " + taskverbs.RetryDelivery`,
taken from the verb constant (Rule 1/13). Keeping both would have been a second
name for one verb, which is what `Rule 1b` forbids, so the reconciliation keeps
master's name.

## Retired 2026-10-31 (task/01a18ec8): the arms are no longer triggers

**The verdict flipped, and this section says why — the page above is the history
of the decision, not its current state.**

The 2026-10-12 decision classified the three re-delivery arms as TRIGGERS because
a call to one could remove the row's OWN ground: they reached the worktree+branch
close-out chain, and `cmd/agent/cli_tasks.go`'s delivery-retry leg called them
from a scanned tree. Both halves of that reasoning are gone:

1. **The chain ask was retired one slice earlier** (task 01a18799, slice 3/4).
   `internal/tools/task_delivery_retry.go:365-367`, its own note: *"THE
   CLOSE-OUT CHAIN ASK IS RETIRED (2026-10-30, task 01a18799, slice 3/4). The
   retry used to hand the row to the worktree+branch close-out chain and report
   that chain's verdict as the attempt's detail. The chain is gone"*. The file no
   longer reaches a removal verb, so `cwdReleaseCensus` — which seeds from the
   removal verb and closes over callers — does not derive the three names as arms
   any more.
2. **The producer was already gone with the finish-cleanup mechanism** (58b5618b,
   "slice 3/4"): the serve lifecycle pass's delivery-retry leg went with it.
   Measured on the tree:

   ```
   $ git grep -n 'DeliveryRetryTaskID\|DeliveryRetryLines\|DeliveryRetryJSON' -- cmd/agent internal/agent
   (no output)
   ```

   Every remaining reference lives inside `internal/tools` — the arms' own
   definitions, their doc comments, and `_test.go` callers. The file states the
   same thing about itself, as an honest gap (`task_delivery_retry.go:372-376`):
   *"HONEST GAP, named rather than hidden (Rule 26): DeliveryRetryTaskID has no
   production caller today (the `retry-delivery` verb was retired in slice 2/4),
   so this file is a library with tests, not a live leg."*

So the four trigger entries the gate carried (`DeliveryRetryTaskID`,
`DeliveryRetryLines`, `DeliveryRetryJSON`, and `TaskFinishCleanupSessionID`, the
last surviving only as a `_test.go` fixture after 58b5618b) were **stale**: they
named coverage the census could no longer see. They are deleted, and each of the
four is now absent from BOTH `cwdReleaseRemovalTriggers` and
`cwdReleaseArmsNotListed` — a name in both cancels out and reads a false green.

**What did NOT change, deliberately:**

- `cwdReleaseReadFloor` (4) and `cwdReleaseArmFloor` (10) — the gate's
  non-vacuity tripwires. Not moved, not lowered.
- `cwdReleaseRemovalVerb` and the walk — neither widened, and the census still
  skips `_test.go`, which is the reason a `_test.go`-only name stops being
  derivable in the first place.
- The arms themselves. `internal/tools/task_delivery_retry.go` is still exported
  and still tested; retiring it is a decision about the delivery-owed event
  family (`task_delivery_owed_*.go`), not about this gate, and this change does
  not take it.

**What replaced the read proof** — not nothing, and not a deletion to go green.
`TestGateCwdReleaseCensusArmsAreClassified`'s four legs were all claims about the
arms being removers reached by producers, so three of them are now false by
construction. `TestGateCwdReleaseDeliveryRetryRetirement` fires on the same
subjects from the tree:

| leg | what it measures | what turns it red |
|---|---|---|
| ① still a live face | all three `func` definitions are still in `task_delivery_retry.go`, AND that file no longer matches the removal verb | an arm deleted (a different retirement), or a removal path returning to the file (the trigger decision has to be re-taken) |
| ② no production caller | no `.go` file under `cmd/agent` or `internal/agent` (tests skipped, as the census skips them) names any of the three | a new production caller — the obligation comes back and the trigger entries must too |
| ③ in neither list | not in the trigger list, not in the exemption list | either half restored alone, or the wrong verdict (private-scratch exemption) applied to an arm that removes the row's own ground |
| ④ note stays attributable | `cwdReleaseDeliveryRetryProducer` (`cmd/agent/cli_tasks.go`) still exists | the file the retirement note blames disappearing |

Leg ② was verified by mutation on the real tree: adding
`cmd/agent/zz_mutation_probe.go` with one call to
`tools.DeliveryRetryTaskID("x")` turned the gate RED with its own sentence, and
removing the probe restored GREEN. Leg ③ was verified the same way: re-adding
`"DeliveryRetryTaskID"` to the trigger list made BOTH
`TestGateCwdReleaseCensusArmsAreListed` (stale trigger) and
`TestGateCwdReleaseDeliveryRetryRetirement` (the two decisions disagree) fail.
