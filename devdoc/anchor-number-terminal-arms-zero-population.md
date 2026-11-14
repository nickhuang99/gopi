<!-- doc-class: historical -->
# The anchor-number arms whose population reached zero — what each floor and each subject means now

Class: historical (the machine banner above carries the same class; the
classifier is the one writer of that cell — Rule 36). This document RECORDS the
state a completed migration left behind and the criterion the arms now apply; it
does not itself govern a corpus.

## The state this documents

`internal/docclass/testdata/anchor-number-baseline.tsv` is a generated DATA table
(Rule 36: its only writer is
`go run internal/docclass/testdata/anchor-number-census.go . <table>`). The
numberless-render migration retired the `anchor <path>:<NNN> at <text>` cell, so
the numbered population is legacy that goes to ZERO; the table therefore reaches
its terminal state **EMPTY**, and the generator certifies that state itself
(`--check`, exit 0, `the freeze table is EMPTY and this scan accounts for it:
271 anchor row(s) scanned, 0 incoherent, 0 of them still record a line number`).
The emptiness criterion is `AnchorNumberEmptyTableVerdict` /
`AnchorNumberEmptyTableOnTree` (`internal/docclass/anchor_number_migration.go`):
empty is GREEN **iff the same scan** finds no incoherent row.

## The defect, measured on this ground

The epic's premise — four arms take their SUBJECT from that table — was measured
by reverting only the test files the fix (commit `b5bd49782`, an ancestor of this
branch, titled *re-decide the anchor-number arms whose subject was the freeze
table*) re-decided, and running them against the empty table at this branch's
tree:

```
$ git checkout 51b5739 -- internal/docclass/anchor_number_{census_predicate,shipped_face,stability,write_summary}_test.go
$ CGO_ENABLED=0 go test -count=1 ./internal/docclass/ -run Anchor
--- FAIL: TestAnchorNumberCensusShippedTableIsTheWritersFixpoint
    the detector measured 0 incoherent row(s) (read floor 100) — it is reading the wrong tree
--- FAIL: TestAnchorNumberCensusShippedFaceFiresOnAStaleTable
    the shipped table carries no data row to delete — this arm has no subject, so the green test
    above is not being held to anything
--- FAIL: TestAnchorNumberNumberChangeOverTheRealFrozenPopulation
panic: runtime error: index out of range [0] with length 0        ← a panic, not a verdict
FAIL    github.com/nick/gopi/internal/docclass
```

That is the epic's red, reproduced verbatim on the empty table. `b5bd49782`
already carries the fix; it is an ancestor of this branch, so this task's job was
the part the fix did NOT do — **the published criterion** — plus the measured
red→green and the verification.

## The criterion this task publishes (deliverable 2)

One rule, stated where the floors are declared
(`internal/docclass/anchor_number_gate_test.go`), so a later reader cannot
re-read a red as "the detector is reading the wrong tree":

> A FLOOR STANDS ON A POPULATION THAT CANNOT LEGITIMATELY VANISH, OR IT STATES IN
> ITS OWN COMMENT WHAT ITS ZERO MEANS.

Applied to every floor in the family:

| floor | population it stands on | its zero |
|---|---|---|
| `anchorNumberCorpusFloor` (200) | the anchor rows the scan RESOLVES (`.Scanned`, ~271) | never legitimate — a broken read; reds |
| `anchorNumberAmbiguityFloor` (20) | rows the uniqueness criterion could not decide (~40) | never legitimate — the criterion stopped asking; reds |
| `anchorNumberMaterialFloor` (1) | rows a fixture's own generator writes | never legitimate — the fixture's writer was just run over real drift |
| `anchorNumberPermitRatioFloor` (0.5) | permits vs the drift **this same run** measured | **legitimate**: 0 permits for 0 incoherent rows IS the terminal state, judged by `AnchorNumberEmptyTableVerdict` |
| ~~`anchorNumberReadFloor` (100)~~ | ~~the live INCOHERENT count~~ | **deleted, not lowered** — it measured the CRITERION (a worn-out accelerator's census), so it reds on the migrated tree whatever the table holds |

The same rule binds a **subject**, not only a floor. Every arm whose subject was
a shipped permit now takes its material from `anchorNumberFaceCorpus`
(`internal/docclass/anchor_number_material_test.go`), which returns REAL permits
on both branches: the shipped table while it carries rows, otherwise a minimal
real corpus whose table the **shipped generator writes** (executed, never
imitated — Rule 31). Arms that still read the shipped artifact — the stale-permit
arm, the provenance-claim arm, the write-summary material decision — each carry a
comment stating what a zero population means **for them** and, where their
question survives elsewhere, name the arm that still holds it (a skip that says
so in the output, auditable, never a silent pass — Rule 27).

## Invariants kept (Rules 39 / 15)

- The read floor and the ambiguity floor are **kept**, exactly as the constraints
  require: "a scan that found nothing must be distinguishable from a scan that
  never ran".
- The ratio floor is **kept**; only the blanket "the table must be non-empty"
  refusals were replaced by the judged criterion.
- The red-proof arms stay RED where they live
  (`TestAnchorNumberGateFiresOnAnUnpermittedDrift`,
  `TestAnchorNumberCensusShippedFaceFiresOnAStaleTable`) while the green arms go
  green: no arm was deleted to buy green.
- The freeze table was **not** regenerated as part of this task, and the
  generator's own `--check` is the authority for its state (Rule 38: a figure
  names its generator).
