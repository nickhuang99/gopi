<!-- doc-class: normative -->
# The empty-gate-package edge is an ERROR class, and a committing path refuses on it

**Task:** `01a13ae1-2477-7490-b79b-24b8b8b2b705`
**Branch:** `task/01a13ae124777490b79b24b8b8b2b705`
**Files:** `internal/gendoc/edge.go`, `internal/gendoc/edge_test.go`,
`internal/tools/merge_anchor_gate.go`, `internal/tools/task_finish_premerge_smoke.go`,
`internal/tools/mergeset_gendoc.go`, `internal/tools/mergeset_gendoc_base.go`,
`internal/tools/branch_check_anchor_test.go`

## The defect

`internal/gendoc` derives, for every document carrying a generated region, the
GATE PACKAGE — the package whose TESTS block on that document, i.e. the thing
that would catch a line moved under one of the document's published anchors.

`GatePackages` walks `BaseDirs` (`internal`, `cmd`). A ROOT document whose
generator lives under `testdata/` (`COMMANDS.md`, rendered by
`internal/standards/testdata/commands-census.go`) therefore comes back with an
EMPTY gate-package set: the only place the document's own generator is named is
itself, and the Go tool builds no test binary for a directory under `testdata/`.
The edge KNEW this and said so — as a plain FINDING, at the same level as "a
refusal arm the corpus never got around to writing". Every committing path that
refuses on a broken edge went on merging: a line moved under one of `COMMANDS.md`'s
anchors would reach the integration branch with nothing anywhere able to catch
it.

## The measurement — two facts, one empty set

The fix turns on a distinction the finding could not carry, and the distinction
is measured, not asserted. `Dep.GateUnderivable` is set by
`gatePackageUnderivable(generator)` — the generator's directory is under
`testdata/` — and it is asked only when the gate set came back EMPTY and the walk
was COMPLETE:

| | empty set, derivation possible | empty set, derivation impossible |
|---|---|---|
| what it means | nobody has written the gate yet | no package CAN gate it |
| remedy | write the test | write the gate package, or retire the class in writing |
| severity | FINDING (unchanged: the sentence this edge always carried) | **ERROR** (`EmptyGateDerivedFormat`) |

The completeness veto is not decoration; it is the measured failure of the first
draft. A generator under `testdata/` on a tree that HAS a gate package is not a
candidate at all — the set is not empty. But a tree MISSING a base directory
(`internal/standards/generated_doc_deps_test.go`'s fixture has no `cmd/`) leaves
an empty set over a walk that could not look: claiming "none CAN read it" there
turned that fixture's `TestGeneratedDocGatesStatesTheBaseItCouldNotWalk` red on a
fact it never measured. `len(skipped) == 0` is the veto; the same fixture's arm
in `edge_test.go` (d) pins it.

## The separation is load-bearing (measured twice)

`Answer.Errors` is a NEW channel; `Findings` keeps exactly what it carried before,
byte for byte (`EmptyGateFindingFormat` is the old sentence, and
`SeparateErrors` already states the measured reason).

The same measurement then repeated one level up, and it cost a red run to find:
an arm that refuses on the whole `Errors` channel refused on the DECLARATION
invariant too, and the lane that was supposed to run the document's GATE test
refused EARLIER instead.

```
--- FAIL: TestBranchCheckAnchorArmRefusesADroppedRegeneration
    the refusal must quote the ANCHOR arm's failing test "TestCommandsDocInStep", got:
      ... the published-anchor edge reports an ERROR-class defect no package can catch;
      ERROR: COMMANDS.md: the anchor internal/bcaanchor/thing.go:3 ... points at a comment line ...
```

The two classes share a severity and nothing else — one means "regenerate the
document", the other means "there is no gate to run". `gendoc.EmptyGateDerivedErrors`
selects the sub-class by `EmptyGateDerivedMark`, and every arm refuses on that.

## What refuses now

| surface | before | after |
|---|---|---|
| `internal/gendoc` | FINDING only | ERROR in `Answer.Errors` |
| `finishCleanupBranchAnchorCheck` (the branch's own pre-push check) | no arm | refuses, `mergeCheckAnchorErrorClassMark`, quoting the edge |
| `mergeAnchorGate` (the merger's pre-merge union lane) | logged only | refuses, attributed by `Answer.Touched` |
| `mergeset_gendoc` / `_base` (the merged-result REPORT lane) | nothing | the sentence is PRINTED (that lane is report-only by contract) |

`Answer.Touched` is the attribution, and it exists because of a measured
near-miss: the empty-gate finding quotes the document and its generator and
carries NO `file.go:NNN` anchor ref, so `mergeAnchorGateBlamesBranch` — which
reads refs out of a finding — cannot attribute it, and the refusal would have
been logged away on a union that moved exactly the anchor source.

## The escape hatch, and why it is not a bypass

`GeneratedDocAnswerOpts(root, changed, skip)` — one extra entry point, so no
existing caller re-signs — retires the ERROR class on a participating tree and
records the decision in `Answer.Skip`. It is read from
`GOPI_GENDOC_ERROR_CLASS_SKIP` by exactly ONE reader (`gendocErrorClassSkipped`),
and every arm that honours it APPENDS `gendocErrorClassSkipNote()` to its own
sentence, so a tree merged with the class retired is visibly merged with it
retired. It never touches `Findings`.

## When the escape hatch has strength — MEASURED

The gap the class documents is real and closed for a tree that mirrors this
repo's layout. It is NOT closed for the one case the class cannot tell from a
defect: the document's OWN generator is the only thing that reads it, and the
edge cannot name it as a gate package without also naming the program that RENDERS
the document as a gate ON the document. `GOPI_GENDOC_ERROR_CLASS_SKIP` is the
operator's lever for that tree.

**THE LEVER HAD NONE AT FIRST — measured during the originating task's own
close-out (2026-10-14), and repaired on branch
`task/01a13b61e2957c8f96f246e05898b434` (2026-10-15).** Both halves are recorded
here, because the broken measurement is why the repair exists.

*As measured broken.* On the fixture tree the arm refused with
`gendoc.EmptyGateDerivedMark`'s sentence whether the variable was set or not — the
skip empties `Answer.Errors`, so the CLASS arm stands down, and the refusal was
then produced by the `ans.Touched` fallback arm, which returned the SAME
`mergeCheckAnchorErrorClassMark`. The operator could therefore not retire the class
on a tree at all, and no arm printed `gendocErrorClassSkipNote()` while it
happened — so a tree merged with the class "retired" was not visibly retired; it
read exactly like a live refusal.

```
LIVE   : ok=false why="the published-anchor edge reports an ERROR-class defect no package can catch"
SKIPPED: ok=false why="the published-anchor edge reports an ERROR-class defect no package can catch:
        this change moves a published anchor source and the edge names no package to run for it (4 path(s) measured)"
        findings={  0 }
```

*As repaired.* The fallback now reads the decision from the edge's own answer
(`Answer.Skip`, set by `GeneratedDocAnswerOpts` when it emptied `Errors`) rather
than re-reading the environment, and under a retired class it abstains with its own
mark `mergeCheckAnchorErrorClassRetiredMark` — which deliberately does **not**
contain `mergeCheckAnchorErrorClassMark`, since a substring match is how a caller
classifies a refusal. Re-measured on the same fixture through the same arm:

```
LIVE   : ok=false why="the published-anchor edge reports an ERROR-class defect no package can catch"
RETIRED: ok=false why="the published-anchor edge's ERROR class is retired for this run, so this change's
        anchor movement is reported rather than refused (4 path(s) measured) (GOPI_GENDOC_ERROR_CLASS_SKIP
        is set: the published-anchor ERROR class is retired for this run)"
        — no class mark, findings=[]
```

The arm's line numbers moved with this repair; the fallback is at
`internal/tools/task_finish_premerge_smoke.go:431` on the repairing branch (it was
`:396-414` when the broken reading was taken — do not read either number as
current without checking). The live path is unchanged: with the class live the
class arm refuses first, and `Answer.Skip` is false so the fallback takes its
original branch, which the no-weakening control
(`TestBranchCheckAnchorArmSkipDoesNotWeakenTheLiveClass`) observes.

This was a DEFECT IN THIS DOCUMENT'S OWN TASK DELIVERABLE, recorded here rather
than silently repaired, and filed as its own row (Rule 41: 发现异常即取证、即提 bug;
Rule 26: an incomplete fix is repaired by dispatching again). The first draft of
this section said the escape "is the operator's lever for that tree"; the lever had
no strength, and that draft was written from the intent rather than from a run.
Evidence: `~/.gopi/sessions/01a13ae1-.../evidence/skip-does-not-reach-the-committing-arm.md`
(broken), `devdoc/gendoc-error-class-retirement-reaches-the-committing-arm.md`
(repaired).

## Verification

- `go build ./...` — OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- `go test ./internal/gendoc/` — OK (the whole package).
- `scripts/test-budget.sh -run 'TestGeneratedDoc|TestRegionAnchor|TestCommandsDoc|TestConfigDoc|TestGate|TestBranchCheckAnchorArm' ./internal/gendoc/ ./internal/tools/ ./internal/standards/` — exit 0, `outcome=pass`.
  **Corrected 2026-10-15 (this devdoc's first draft stated the opposite, and the
  opposite was not measured).** The draft said the `TestConfigDocNameCarryingRefsDeclareTheirSymbol`
  arm "caught the two `CONFIG.md` line references my edits moved" and that the arm
  was green. Both halves were wrong at the revision the draft described: my edits
  moved `mergeAnchorGateTimeout` to `:1004` and `mergeAnchorGateDisabled` to
  `:1027`, the committed `CONFIG.md` still said `:994` / `:1017`, and replaying the
  budget run on that tree printed
  `--- FAIL: TestConfigDocNameCarryingRefsDeclareTheirSymbol ... merge_anchor_gate.go:1017 does not declare 'mergeAnchorGateDisabled'`
  (`run-1791997062-470079.json`, `exit=1 outcome=fail`). The numbers are
  re-pointed and verified against the file in the amended commit.
- The committing surface, run for real (Rule 31) — see the task report's `Ran:`
  line; the same fixture's control arm does not refuse.
