<!-- doc-class: normative -->
# The TARGET-SCOPED devdoc-anchor lane — closing a routing gap, not a detection gap

## The gap, in one sentence

Every REFUSING merge-time lane (the CLI contract, the build, the union compile,
the published anchor) measures the **UNION** of `merge(target, branch)` and blames
only paths the **incoming branch** touched — so a drift the **TARGET** makes *by
itself* is refused by no lane, and the one leg that asks the devdoc question asks
it **report-only** (`internal/tools/mergeset_docclass.go`, "res.OK left alone")
and only while a landing is in flight.

The union scope is not a bug. `merge_anchor_gate.go`'s own header states the rule
and its reason: *"the TARGET is deliberately NOT required to pass … what replaces
it is BLAME"* — refusing a candidate for a defect it did not cause tells the wrong
author to fix the wrong tree. The gap is the consequence nobody routed: a red
target has no owner.

## Measured, at the commit that produced this doc

| revision | `doccheck` | note |
|---|---|---|
| `6ef428c16` (this epic's red fixture) | exits 1, **2** unpermitted anchor rows | the state that started the epic |
| `2f697e5e` (this branch's base) | exits 1, **1** unpermitted row (`devdoc/merge-backlog-terminal-guard.md` → `internal/tools/tasks.go:2825`) | **pre-existing**, carried by the merge preceding this work |

`devdoc/merge-backlog-terminal-guard.md`'s manifest row still anchored
`anchor internal/tools/tasks.go:2825 at }`, a line that has held
`// here come from the SHARED classification …` for some time and recorded `= 5`
periods as stale. Its newer home is `internal/tools/tasks.go:3643` — which
renders `at } else {`, so the anchor text moves with the number and the row was
re-pointed **through the generator** in the same commit (that row is a normalized
`else {`/`}` shape, so the repair is done rather than frozen).

## The arm, and why it is not a new dependency

Asked at the **head** of the gate sequence both merge faces already run:

* the merger's own sweep tick — `repoSweepDecideBranch` (`internal/tools/repo_sweep.go`),
* a manual `gopi task merge` — `taskMergeOne` (`internal/tools/task_merge.go`).

Both faces already run that sequence, so the lane costs the machine nothing new:
no hook, no tool, no script, no resident process, no tick (Rules 3/35). It reuses
the readers that already exist and are already the one definition of each
question (Rule 14):

* `docclass.AnchorNumberDrift` — the very computation `gopi tool doccheck` prints,
  so the lane's verdict and the verb's verdict cannot disagree about one tree;
* `mergeUnionGateExtract` — the archive-pipe materialisation the union lane uses;
* the merged-result step's own `VetFailedEvent` notify and `TaskSubmit` bug-row
  channels, deduped through the same `reworkSummaryLive` probe;
* `scratch.RuntimeScratchDir` — the merge-build runtime class, so scratch stays
  off the `/` partition and inside the class's existing GC bound (Rule 22).

## Why the TREE and not a diff

A drift is a property of a **tree**, not of a path list: the question is "does
this revision's manifest still point at the text it records?". The lane reads
`git rev-parse <target>^{tree}` and extracts it into scratch, so it needs no path
list and cannot be dodged by a landing whose diff happens not to touch a manifest
path — exactly the shape that made the union lanes abstain.

## The policy: it reports before it ever refuses

The drift found here is **by construction the target's own**, so the default verb
is **detect and dispatch, never block**: the defect is filed as a `bug` ROW (a
queue item a later run picks up, deduped by **defect** identity — the gate, the
frozen table and the revision's *role*, never a commit hash, so a drift that
survives several landings is one row) and announced once on the close loop's own
notify channel.

The landing is **not** held by default, and that is a measured decision rather
than caution: holding a candidate would archive a healthy branch for a defect on
the target and stop the queue behind a repair nobody dispatched — the exact
"told the author the wrong thing" failure the blame arms exist to avoid.

The stricter policy is one named value away for the operator who wants the
landing held:

```bash
GOPI_MERGE_TARGET_DOC_GATE=refuse   # hold the landing — archive NOTHING, the branch stays a candidate
GOPI_MERGE_TARGET_DOC_GATE=notify   # the default
GOPI_MERGE_TARGET_DOC_GATE=0        # off entirely (an unforeseen false report is one value away from gone)
```

A refused landing reports action `target-doc-refused`, counted **apart** from
`build-refused`: no branch was found broken — nothing archived, no rework row
filed — and an operator reading a nonzero build-refused tally must not go looking
for a broken branch.

## What an abstention looks like

The lane **names its reason** for every non-verdict, because an abstention that
reads like a clean pass is this family's most-repeated defect (Rule 15): no
scratch, an unresolvable revision, a tree that will not extract, a tree that
carries no corpus, and a corpus recording **no** anchor rows are each printed as
their own sentence; a clean target prints `PASS …` with the population scanned.

## The regression guard

`internal/tools/merge_target_doc_gate_test.go` builds a **two-commit** fixture —
a green target and a drifted descendant of it — and asks about each. Because the
lane reads git **objects** (never a checkout), the red arm needs **no mutated
working tree and no red commit pushed to a peer**: the drift lives in a commit
that stays in the test's own temporary repo.

These siblings keep the same contract:

* `devdoc/standards-gate.md` — "state it, never a silent skip".
* `internal/docclass/anchor_number_baseline.go` — the frozen table, its generator
  (`internal/docclass/testdata/anchor-number-census.go`) and
  `AnchorNumberDriftVerdictClass`, the class name this lane and `gopi tool
  doccheck` now share (Rule 14).
