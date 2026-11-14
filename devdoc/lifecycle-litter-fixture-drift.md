<!-- doc-class: historical -->
# The lifecycle litter test's row was not shaped like the harness's row

**Task**: `01a12b800b5f7ba789713c3675b83f6a` (branch
`task/01a12b800b5f7ba789713c3675b83f6a`)
**Date**: 2026-10-11  **Host**: hosta  **Base**: master `fda79bf60`
**Epic**: `task-ledger-auto-row-disposal`

## The red

`TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow` (`cmd/agent`) fails on
master from `71577959e` onward. The tick prints its summary and disposes nothing:

```
[worker] [lifecycle] stale reported=2 disposed=0 sink=…
    task_lifecycle_litter_test.go:95: the never-started auto row is still "in_progress"
        after the tick — this is the reported defect:
```

`reported=2` is the STALE-REPORT leg (`serveStaleSweep`) counting the two
seeded rows; `disposed=0` is the DISPOSAL leg (`autoDisposeConfirmedZombies`)
reaching neither. The test is the operator's own question — "the row exists and
the pass has seen it, why is it still `in_progress`?" — so the honest reading is
"seen but not acted on".

## The measurement that settles it

`71577959e` *narrowed* the disposer's candidate set. Before it, the extinction
pass asked `status='in_progress' AND run_method='auto'`; with this row that is
already false — the row has an empty summary and an empty-ish claim, but the
decisive fact is the summary. Measured on the live ledger (read-only, `mode=ro`):

```
OLD predicate (pre-71577959e): status='in_progress' AND run_method='auto'   count = 0
NEW predicate (71577959e):      + run_method IN ('','auto')
                                  AND summary LIKE '[auto] %'               count = 0
rows carrying the harness marker '[auto] ' at ANY status                   = 479
in_progress rows by (run_method, marker):  ('worker','no',4)
```

Three facts, each independently measured:

1. **The class EXISTS live**: 479 rows carry the harness registration marker.
2. **Neither predicate selects an `in_progress` `[auto] ` row** — on this host
   the four `in_progress` rows are all `worker`-stamped, non-marked.
3. **The harness only ever mints MARKED rows**: `agent.autoTaskSummaryPrefix`
   (`"[auto] "`) was introduced 2026-08-28 (`300602608`), five weeks *before*
   the `RunMethod` stamp landed (2026-09-13, `ab9de4723`). So the historical
   explanation "the 737 empty-method rows are rows whose writer never stamped"
   is the right story about the METHOD column and the wrong story about the
   SUMMARY column — those rows were written by the *same* hook, marker and all.
   There has never been a harness writer that mints `[auto] `-less rows.

Point 3 is the operative one: the test seeds a row whose summary is
`"lifecycle fixture t-litter-never-started"` — **no marker** — which is a row
the harness does not mint, on either side of `71577959e`. `71577959e` merely
moved the sweep's identifying question from a column the fixture *did* set
(`run_method='auto'`) onto the column that actually names the writer (the
summary marker), and the fixture had never modelled the latter.

## Verdict: the fixture was stale, not the predicate

The two candidates were "the fixture mints a row the harness never mints" and
"the production predicate is too narrow to see the class it was fixed for".
Measurement picks the first:

* If the predicate were too narrow, the ledger would contain `in_progress`
  `[auto] `-marked rows it fails to reach. It contains none (fact 2) — and,
  decisively, it contains none that the OLD predicate would have reached either
  (the `run_method='auto'` rows are exactly the marked ones, `[auto] `-less
  `auto` rows = 0 at any status).
* The predicate's contract is *conservative by design*: `71577959e`'s own
  argument is that a row that is not verifiably the harness's registration must
  NOT be collected, because the correct negative (an imported row) is
  indistinguishable from the false negative (a registration with no marker).
  Widening it past the marker would require guessing the writer from the
  `session_id` shape or the `created_at` window — that is the "guess wearing a
  name" the same commit refuses one arm over.

So the defect is entirely in the fixture: it was written (`d7e5d39f6`) against
the pre-`71577959e` predicate, which asked only for the method column, and it
kept passing until the predicate changed. `cmd/agent` owns no production
writer; `internal/tools`' `autoRunLitter*` code is correct as it stands.

**The narrow fix**: the fixture now seeds rows the way the harness does — the
production prefix, referenced as `agent.AutoTaskSummaryPrefix` (the constant
was **exported** for this reader; `cmd/agent` may import `internal/agent`, and
only `internal/tools` cannot, because `agent` imports `tools`), with a
precondition read back from the ledger that fails the fixture if the row it
minted is not a harness registration. No production predicate was touched.

### The non-vacuity proof (both directions, one variable)

Same fixed production code, only the test file swapped:

```
# green — the repaired fixture
go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent
ok  	github.com/nick/gopi/cmd/agent	0.886s

# red — the pristine (master) fixture, run against the SAME fixed production code
git show HEAD:cmd/agent/task_lifecycle_litter_test.go > cmd/agent/task_lifecycle_litter_test.go
go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent
--- FAIL: TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow (0.40s)
    [worker] [lifecycle] stale reported=2 disposed=0 …
```

That is the strongest statement available about *where* the defect was: with the
production tree held fixed, the test's own colour is decided by the fixture
alone. It also proves the repaired test is not vacuous — it is sensitive to
exactly the fact it was written to assert.

## Files

* `cmd/agent/task_lifecycle_litter_test.go` — the repaired fixture (the fix).
* `internal/agent/taskregister.go` — `autoTaskSummaryPrefix` → exported
  `AutoTaskSummaryPrefix` (+ the doc comment naming why).
* `internal/agent/taskregister_claimability_test.go` — reference rename.
* `internal/tools/taskzombie_extinct.go`, `taskzombie_extinct_test.go`,
  `internal/tools/chat.go` — comment references to the constant.
