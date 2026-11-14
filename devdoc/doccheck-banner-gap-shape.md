<!-- doc-class: historical -->
# `gopi tool doccheck --fix` and the banner half it claimed but could not reach

**Measured 2026-10-18, task `01a14abe7ded743293f94e627d0b7817`, branch
`task/01a14abe7ded743293f94e627d0b7817`, baseline `2c56271`.**

## The defect, in one paragraph

`gopi tool doccheck --fix` printed (and its help text claimed) that it writes
"the row, and the class banner where the class requires one". For a document
whose `devdoc/DOC-INDEX.tsv` row EXISTS and whose line 1 is not that class's
banner, it printed

```
doccheck: nothing to repair in the GAP class (0 gaps between devdoc/ and devdoc/DOC-INDEX.tsv) …
```

and exited 0 — while `go test ./internal/standards -run
TestDocClassBannerMatchesManifest` was RED on the same bytes. Two readers of one
criterion, giving opposite verdicts about one corpus.

## Why it could not reach the writer (the mechanism, from the code)

Two independent breaks, and both had to be closed:

1. **The vocabulary had no shape.** `CheckCorpus`'s closed gap set
   (`GapFileWithNoRow`, `GapRowWithNoFile`, `GapDuplicateRow`,
   `GapMalformedRow`, `GapReasonTooShort`, `GapNormativeNoCheck`,
   `GapUnknownEvidence`) encodes only ROW defects. "The row is there and the
   banner is not" was expressible nowhere, so it was reported nowhere —
   `RepairableGaps` could never yield `ScopeGaps.Unbannered`.
2. **`RepairGap` dropped the banner arm anyway.** Even with a gap routed into
   `ScopeGaps.Unbannered`, `RepairGap` (the verb's writer) rebuilt its
   `remaining` struct from `Malformed`/`Duplicated`/`BlankLines` only — a silent
   drop of the one field the fix was for (Rule 15: a swallow, not a decision).

So the banner arm (`Repair` → `EnsureDoc`) existed, worked, and was reachable
ONLY through the branch-scoped `ScopeCheck` the close-out chain calls with the
branch's ADDED paths. From the corpus-wide verb it was dead code behind a live
claim.

## The fix

### 1. Two gap shapes, split by REMEDY rather than by spelling

`internal/docindex/docindex.go`:

| shape | meaning | repairable? |
|---|---|---|
| `banner-missing` | the row's class carries a banner; the file declares none | **yes** — routed into `ScopeGaps.Unbannered`, written by `EnsureDoc` from the row (the row already decided the class, so this is not a classification) |
| `banner-contradicts` | the file DECLARES a class that contradicts its row — a different class, an unknown word, or a declaration where the class carries none — on line 1 or anywhere in the body outside a fence | **no** — reported, and its `Detail` names both routes (`banner.py --only` repairs the file, since the manifest is the authority and banner.py is its follower; `classify.py --only` re-derives the row when the document is right) |

The criterion is NOT re-implemented here. `bannerGapDefects` calls
**`docclass.BannerDefects`** — the exact function `TestDocClassBannerMatchesManifest`
runs — and only decides which of the two shapes each sentence is. That decision
is taken from `docclass.BannerDeclarations` / `BannerDeclaredClass`, i.e. the
package's own scan, never a second reading of what a banner looks like.

**The line-1 rule matters and was measured.** `ParseBanner` alone would call
`<!-- doc-class: reference -->` on line 1 "no banner", so a reader built on it
would classify that file as `banner-missing` and the repair would PREPEND a
second banner over a declaration the document already makes. `BannerDefects`
sees it (ParseBanner rejects the word → "does not open with its banner"), so it
is reported as `banner-contradicts` and never written. The shape table in the
test pins exactly this arm.

`IsRepairable(shape)` is the ONE answer to "can the verb write this shape";
`RepairableGaps` routes by it and the verb's closing line counts by it, so the
two cannot disagree.

### 2. The row arm no longer visits the banner paths

`Repair`'s row loop visited `unionPaths(Malformed ∪ {manifest}, Missing,
Unbannered)`. A path in `Unbannered` already HAS its row — that is how the class
is known — so visiting it rewrote a correct, generator-authored row with this
writer's generic `WritePathReason`. That is a re-classification by the back door
and it was a real byte diff, not a theoretical one (measured: the row's reason
text was replaced). The loop now visits `Malformed ∪ {manifest} ∪ Missing`; the
banner is written afterwards, from the row already on disk.

### 3. `RepairGap` carries `Unbannered` instead of dropping it

### 4. The verb's two false claims

`cmd/agent/cli_doccheck.go`:

- the help text now states the TWO banner shapes and which one is written;
- the `--check` bullet lists both among the GAP question's shapes;
- the closing summary said **"none of these can be written by this command"**
  over gaps this arm had just written. It now reports the count of gaps it left
  unwritten (`docCheckUnwrittenGaps`, counted through `docindex.IsRepairable`).

## What was NOT changed (the abstention survives)

`ScopeCheck` and `RepairWithScope` are untouched: the branch-scoped check still
reads the banner from line 1 for the documents a branch ADDS, and still says
nothing about an inherited document. A corpus-wide shape must not turn the
close-out chain into a whole-tree re-decider — that is the 2026-09-26 defect
(the close-out re-decided an INHERITED doc's row while its authoring run was
still executing). `TestScopeCheckBannerArmIsUnchanged` pins it: the same
document the corpus check reports is NOT reported by `ScopeCheck` when it is not
in the branch's added set.

## Verification

- Build: `go build ./...` → OK
- Tests: `go test ./internal/docindex/ ./internal/docclass/ ./cmd/agent/ -run
  'Banner|Doccheck|DocCheck|Repair'` → PASS (see `## Evidence`)
- Ran: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` then
  `./bin/gopi tool doccheck --repo .docarm --fix` on the briefing's fixture
  (a `.docarm` corpus inside the worktree: `go.mod`, `tools/docclass/{classify,banner}.py`,
  a two-row manifest, and `devdoc/record.md` whose line 1 is `# a past run`)
- Result: baseline binary → `nothing to repair in the GAP class`, line 1 still
  `# a past run`. Fixed binary → `doccheck: repaired 1 path(s): devdoc/record.md`
  and line 1 is `<!-- doc-class: historical -->` (head -1 verbatim below).
- `go test ./internal/standards/ -run TestDocClassBannerMatchesManifest -count=1`
  → OK on this tree.

## Not Done (honest)

- **A declaration on line 1 whose payload is an UNKNOWN word is repaired by
  nobody through this verb** — by design (it is a contradiction, not a missing
  banner), and `banner.py --only` is the named route. Nothing here makes that
  route automatic, and that is deliberate: choosing which half is stale is a
  class decision.
- **`--fix` still exits 1 on an unrelated class.** On a corpus with a stored-cell
  drift the exit status is the report arm's verdict on the WHOLE corpus, so the
  fixture run exits 1 for a reason that predates this change. The verb-level
  test asserts the WRITE (bytes + gap vocabulary) rather than the status, and
  says so in its own comment.
- **The `banner.py` / `classify.py` help text does not mirror the two new shape
  names.** `classify.py --check` reports ROW gaps only (the banners are
  `banner.py`'s question, by that file's own header), so adding them there would
  be a second face for the same criterion — the Rule 14 defect. Recorded here
  as the reasoning, not left as an omission.
