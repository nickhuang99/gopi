<!-- doc-class: historical -->
# The Fixture Row — a Seeded Row That Is Queue Decoration, Never Work

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** implemented (`internal/tasktags`, `internal/tools`, `cmd/agent/cli_tasks.go`)
**Incident:** measured 2026-10-13.

## The failure this closes

A rig does not get "a pending row sitting there" when it seeds one into a profile
a worker drains. **It gets a real agent run.** Measured 2026-10-13: a rig seeded
two labelled fixture rows into its own profile and its own foreground workers
claimed **both** — one ran 22 turns / 998k tokens. The rig's `SIGTERM` could not
recall the claim, and that is not a bug in the rig: **a claimed row belongs to the
worker**, and by the time the rig notices, the LLM round is already spent.

A second, quieter reason the old workaround was not enough: `gopi worker` runs a
one-shot — the operator can observe the waste but the ledger has already moved the
row into a terminal state, so "undo" is not available even in principle.

## The two mechanisms

### 1. `--fixture` — a first-class unclaimable row (the new one)

```bash
gopi task submit "SEEDED FIXTURE — must never be claimed" --fixture
```

The flag appends the reserved tag `tasktags.FixtureTag` (`"fixture"`) through
`tools.AppendTaskTag`, and the row then lands in the ledger exactly like every
other row. **Nothing about the row is special except its tags** — there is no
second table, no side channel, no "fixture" column.

What makes it unclaimable is that the **ONE shared claimability predicate** asks
the same question every other consumer asks:

```go
func taskRowClaimable(rowTags, payload, parentID string, parentStatus ...) bool {
    ...
    if resolveRowIsMergerOwned(rowTags) { return false }
    if taskHasTagToken(rowTags, tasktags.FixtureTag) { return false }   // ← the fixture arm
    denied, _ := taskParentGateDenied(parentID, parentStatus)
    return !denied
}
```

The arm lives **inside the predicate**, not at the dispatch sites. That is the
whole design: a door that re-decides eligibility for itself (the claim funnel, the
transfer leg, a future Nth consumer) inherits the refusal for free, and a door that
*does* re-decide is visible as the anomaly it is (Rule 41).

Where the refusal surfaces:

- **`TaskNextFiltered`** — the ONE funnel every dispatcher shares — refuses the
  row, while an unrelated normal pending row is still offered (`a park is never a
  head-block`).
- **`taskQueueSkipReason`** — the pending view's marker renders
  `[queue-skip] fixture row — the reserved fixture tag marks queue decoration,
  never work: no dispatcher will claim it (drop the tag to make the row real
  work)`. The fixture reason is checked **before** the parent reason, so a row that
  carries both is named for the arm that actually cannot be repaired by releasing
  the parent.
- **`taskTransferClaimabilityWhy`** — the transfer leg gets its own sentence. A
  fixture that travelled to a peer would be claimed **there** — the identical LLM
  round, one host over — so the refusal must name itself rather than fall through
  to "no narrower reason".

`TaskSubmitOptions` accepts the tag through the ordinary `Tags` field, so the tool
face (`task_submit`) can mark a fixture the same way without a new parameter: the
tag is a protocol value, not a CLI-only convention.

### 2. The parent recipe — works on any binary, still documented

```bash
gopi task submit "fixture" --parent-task-id <a row that never reaches completed>
```

Rule 33 parks a child whose parent is not in `{completed, verified}` — forever, by
design, with no cascade cancel and no staleness release. A fixture parented to a
row that never completes is therefore unclaimable **on a binary that has no
`--fixture` at all**.

**The new arm does not weaken it.** `TestFixtureParentRecipeStillParksForever`
pins that the parent gate still denies on its own terms, that the two mechanisms
compose (a row carrying both is refused on both counts), and that the normal row
beside them is still offered.

## Why a tag and not a new field / a new tool / a new table

- **A tag already round-trips.** `tags` is persisted, read by every eligibility
  reader, and visible in `task show`. The predicate needs a bit, and a tag is the
  bit the ledger already has.
- **Rule 3:** no new top-level tool. The mechanism rides `task submit` (an
  existing verb, one new flag) and the existing claim funnel.
- **Rule 1/13/14:** the value is ONE exported constant in the leaf package
  `internal/tasktags`, referenced by the predicate, the CLI, the transfer leg and
  the tests — no bare `"fixture"` literal anywhere, and a persisted protocol value
  is pinned by `TestTagValuesAreThePersistedSpellings` beside its siblings.
- **Rule 35 (zero dependency):** the flag creates no file, no unit, no scheduled
  task, no state anywhere. A fixture row is one ledger row carrying one more tag,
  and the ledger row was going to exist anyway.

## Why the reason text is a separate constant

`taskQueueSkipReason` and `taskFixtureRowReason` exist because the pending view's
marker **appends a rendered sentence** to `TaskQueueSkipMarker` (Rule 15: a refusal
names its own cause). Rendering the parent's sentence on a row with no parent would
send a reader looking for a dependency that does not exist — the exact confusion
that makes a wrong marker worse than no marker. The reason is read-only: it names
the cause, it never re-decides the verdict (the verdict is `taskRowClaimable`'s
alone, so marker and funnel cannot disagree).

## The regression test

`internal/tools/task_fixture_gate_test.go` — five tests, all driving **real
surfaces** (`TaskNextFiltered` through `claimNext`, `TaskList`, `TaskSubmit`,
`TaskTransferEligible`), never the predicate in isolation:

| test | pins |
|---|---|
| `TestFixtureRowIsNeverClaimedWhileANormalRowStillIs` | the funnel skips the fixture and offers the normal row; the fixture is never claimed and stays `pending` with no `claimed_by` |
| `TestFixtureMarkerNamesTheReasonAndAgreesWithTheFunnel` | the fixture wears `[queue-skip]` naming the fixture (and *not* the parent); `[next]` sits on the row the funnel actually claims |
| `TestFixtureRowIsRefusedByTransferEligibility` | the sibling door refuses, naming the fixture; an ordinary row still travels |
| `TestNonFixtureRowsAreUntouched` | the arm changes the verdict for exactly one tag: `fixture-ish` (mid-token) stays claimable, `ui,feature` stays claimable, a payload-less row stays unclaimable |
| `TestFixtureParentRecipeStillParksForever` | the Rule 33 recipe the epic keeps as the documented fallback |

The fixture is submitted **first**, so a FIFO reading of the queue would reach it
first — the assertion is about eligibility, not ordering. The ledger is a fresh
per-test database through `testutil.NewTestDBPath` (Rule 32: the scaffold owns the
path and the environment).

**Red proof:** with the fixture arm removed from `taskRowClaimable`, three of the
five fail — the funnel claims the fixture, the marker reads `[next]` on it, and the
transfer leg reports it eligible. The arm was then restored in the same run, and
the restore was verified by an explicit revision — the criterion (Rule 5:
an index-relative diff cannot prove a restore, because a restore seeded from
the index writes back the bytes the index holds):

```
$ test "$(git hash-object internal/tools/tasks.go)" = "$(git rev-parse HEAD:internal/tools/tasks.go)" \
    && echo RESTORE VERIFIED
RESTORE VERIFIED
```

— after which the same test selector was re-run green (`ok … 0.581s`).

## The real run (Rule 31)

Sandbox profile `sandbox-fixture-gate-c1cf907a` (created by `gopi tool sandbox`,
one private `GOPI_HOME`), one fixture row + one normal row submitted through the
**real CLI**, then a **real** `gopi worker --once`:

```
$ GOPI_HOME=$SB $SB/gopi task submit "SEEDED FIXTURE — must never be claimed" --fixture
$ GOPI_HOME=$SB $SB/gopi task submit "REAL WORK — must be claimed" --payload "a real briefing the worker will run"
$ GOPI_HOME=$SB $SB/gopi worker --once

$ GOPI_HOME=$SB $SB/gopi task show 01a132d6-5ac1   # the fixture
status:         pending
runs:           none (no run log in the directory)
tags:           fixture
ended_at:       (not ended)

$ GOPI_HOME=$SB $SB/gopi task show 01a132d6-5ad6   # the normal row
status:         completed
runs:           1 run(s) — 1 completed
run:            5aa1d8d8-…  2026-10-13 09:20:16  completed exit=0  2 turns  0s
claimed_by:     hosta:3942158 (at 2026-10-13 09:20:14)

$ GOPI_HOME=$SB $SB/gopi task list --status pending
#01a132d6-5ac1-…  [pending]  SEEDED FIXTURE — must never be claimed … [queue-skip] fixture row — the reserved fixture tag marks queue decoration, never work: no dispatcher will claim it (drop the tag to make the row real work)
```

The drain round claimed the normal row and **left the fixture untouched**: still
`pending`, no run directory, no claim, `[queue-skip]` naming the fixture. This is
the acceptance the epic asked for, observed on the surface that failed on
2026-10-13.

### The same proof, re-run against a live queue (both markers in ONE view)

A second round submits a normal row **while the fixture is present**, so the pending
view has to place `[next]` and `[queue-skip]` side by side:

```
$ GOPI_HOME=$SB $SB/gopi task list --status pending
#01a132ed-cce6-…  [pending]  SECOND REAL WORK …                       # no marker yet
#01a132ed-bfcb-…  [pending]  SECOND REAL WORK …            [next]     # ← the row the funnel takes
#01a132d6-5ac1-…  [pending]  SEEDED FIXTURE — must never be claimed …
  [queue-skip] fixture row — the reserved fixture tag marks queue decoration, never work: …
```

and a drain over the SAME queue claims the normal row, leaving the fixture pending
with `runs: none` and a run directory containing only `attachments/` + `metadata.json`
(no run log, no report, no session — it never executed).

**The fixture is not a head-block either**: a drain whose ONLY pending row is the
fixture ends

```
worker: loop start --once (claim → run → … ; exit on drained queue)
worker: queue drained — exiting
[worker exit 0]
```

with the fixture still `pending`. The funnel walks past it, and the queue reads
drained — the park is a skip, never a stall.

## Not done

- **No gate on `--fixture` used against a live profile.** A fixture row in an
  operator's profile is harmless (it is refused, not claimed), so nothing here
  needs a guard — but neither is there anything stopping a rig from littering a
  real ledger with decoration rows. Deliberate: a littering rig is a rig bug, and
  a refusal would make the mechanism harder to use for its intended case.
- **No cross-ledger fixture propagation rule.** A fixture is refused at the
  transfer leg, so it never reaches a peer *as a transfer*. If a peer's ledger
  receives the same submit through another route, the peer's own predicate refuses
  it there too — which is why the arm is in the shared predicate rather than in
  the transfer filter.
