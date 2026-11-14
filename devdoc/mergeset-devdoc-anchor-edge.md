<!-- doc-class: normative -->
# The devdoc-anchor edge in the merged-result measurement

**Class: a past run's record of a landed change.** This document describes the
edge added to `RebuildAfterMergeMergeset` on 2026-10-12 by task
`01a132da-b44b-7cd2-8275-f99f3ef4bac4`. It is the account of ONE change and of the
measurements behind it; the standing behaviour lives in the code it names
(`internal/tools/mergeset_docclass.go`, `rebuild_vet_mergeset.go`).

## The gap

`devdoc/DOC-INDEX.tsv` classifies the devdoc corpus and records, per row, one
evidence cell — `anchor <path>:<line> at <text>`, `sym <path> <Symbol>`,
`path <repo-path>`, `cite <repo-file>:<line>`. Three gates inside
`internal/docclass` re-verify those cells against the tree (the anchor-NUMBER
gate, the prose-anchor gate, the prose-path-line gate). Every one of them is keyed
on the pair *(a manifest row, a path in the tree)*.

The post-merge verification (`RebuildAfterMergeMergeset`) assembled its run set
from two sources, and the devdoc family fit neither:

1. `mergesetGatePackageDirs` — a FIXED list of three gate directories
   (`internal/standards`, `internal/stylegate`, `internal/testutil`).
   `internal/docclass` is not in it.
2. the published-anchor edge (`internal/gendoc`) — which derives its packages from
   "a test names this document of record". Its candidate documents are the
   root-level pages and `man/` (`CandidateDirs = {"", "man"}`), so a devdoc file
   is not a document that edge would even consider.

**Consequence:** a landing that moves a line in a file some manifest row anchors
into — or edits a gated devdoc document's own anchors — passed the merged-result
measurement green, and the first red waited for the next person who happened to
run `internal/docclass` by hand. Measured on this tree at the time of the change:
`devdoc/merge-resolve-peak-hour-guard.md:1220` carried
`anchor cmd/agent/main.go:982 at }` whose recorded text had moved to `:80`, and
nothing in the landing path had ever looked at it.

## What the edge is, and where it is read from

The rule (Rules 1/13/14 — the edge is derived from the tree, never typed):

- **which paths the family anchors into** — every manifest row's own evidence
  cell, read through docclass's ONE manifest reader (`docclass.Load`) and parsed
  by docclass's ONE cell parser (`docclass.ParseEvidence`). The rows ARE the edge.
- **which package carries the gates** — derived from artifacts those gates ship,
  each an exported constant of the carrying package
  (`docclass.AnchorNumberCensusCmd`, `ProseAnchorCensusCmd`,
  `ProsePathLineCensusCmd`, and the three frozen baselines, all under the
  package's `testdata/`). The carrier directory is TWO directory steps up from an
  artifact's own path (`mergesetDocclassCarrierDirSteps`), so the path is
  COMPUTED. If the artifacts stop agreeing on one carrier, the derivation fails
  loudly and the leg abstains, naming the directories it saw — it never runs a
  package that may gate nothing.

## The trigger, and why it is affordable (Rule 27)

The leg is union-scoped. It joins the run set only when the landing's union — the
SAME change set `mergesetGendocUnion` builds, so the two legs cannot disagree
about it — carries a path that is

- a manifest row's `anchor`/`sym` source (the file whose line numbers the rows
  publish), or
- a manifest row's OWN path (a gated document — the other half of the same pair),
  or
- the manifest itself.

Otherwise it abstains, stating the counts the same read produced
(`%d path(s) measured, %d anchored source(s) known, %d gated document(s) known`),
so "the scan found nothing" is never inferred from silence (Rule 15) and the
docclass suite is not a per-landing cost.

The derivation also checks that the carrier is a directory **in this landing's
tree** before asking the toolchain to test it: a fixture (or a tree) without the
carrier gets a stated abstention rather than a red about the tree's shape.

## Honest limits

- **Deeper history is not covered.** A commit that moved an anchored source
  without touching the document is visible here only when it arrives IN the
  landing or in one of the landing's two parents. A commit further back on master
  is a pre-existing state, owned by the base arm of this same step — the sibling
  `mergesetGendocUnion` documents the identical limit.
- **The leg reports, it does not repair.** It re-points no row and regenerates no
  document: the red travels through the step's own channels (notify + bug row,
  deduped by defect identity), with `res.OK` left alone, exactly like the sibling
  union leg.
- **The `cite` tier is not part of the membership test.** Rows citing a devdoc
  document are covered through the ROW'S OWN PATH arm; a landing that edits a
  *citing* artifact is covered by the gate package that carries the citation's
  test, not by this leg.
- **`devdoc/` is still not a candidate document directory for `internal/gendoc`**
  (`CandidateDirs`). This change deliberately does not widen it: the devdoc family
  does not ship a generated region with a named generator, so it has no
  published-anchor edge, and widening the candidate list would have made the
  published-anchor edge claim a document shape devdoc does not have.

## Measured on the real tree (2026-10-12)

`mergesetDocclassReadScope` on this repo's master: **2433 manifest rows, 235
anchored sources, 2433 gated documents**; the derived carrier is
`internal/docclass`. Across the last 40 merges on master the leg obliged the
carrier on every landing sampled (e.g. `60cf0a4d5`: 10 paths in the union are
anchored by a manifest row — 6 anchored sources, 4 gated documents; `16cb43b86`:
4 gated documents). The abstention arm is demonstrated by the fixture test
`TestMergesetDocclassAbstainsWhenTheUnionNamesNoManifestPath`, whose landing
touches only paths no row names.

Gates (this tree, same day, unchanged by this change):

```
go build ./...                                              OK
go test ./internal/tools/ -run TestMergesetDocclass          PASS
go test ./internal/tools/ -run TestMergesetGendocMergeset    PASS
```

`internal/docclass` is **red on this tree before this change and after it**:
`TestAnchorNumberGateOnTheRealTree` reports
`devdoc/merge-resolve-peak-hour-guard.md:1220: anchor cmd/agent/main.go:982 at }`
— a pre-existing drift of exactly the family this leg now surfaces, and
deliberately NOT repaired here (Rule 36: the manifest is regenerated by its
generator, never hand-edited by a gate).
