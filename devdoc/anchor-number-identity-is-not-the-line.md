<!-- doc-class: historical -->
# Anchor-number identity: the line is an accelerator, not the claim

**Task** `01a12f66-badd-7b50-b520-df7b1303718c` · **base** `79b1388c5` ·
**branch** `task/01a12f66badd7b50b520df7b1303718c`

## The defect this repairs

A frozen permit in `internal/docclass/testdata/anchor-number-baseline.tsv` is an
assertion about MEANING — "this anchor row already drifted before the freeze, and
here is why". It was keyed on four fields:

```
doc | verdict | anchor-cell ("path:NNN") | recorded text
```

Two of those four are arithmetic. The **line number** is the cell the anchor
records, and the **verdict** (`number-drifted` vs `recorded-line-blank`) is a
function of *what sits at* that number. So a semantics-free edit — inserting
lines above a cited site — re-keys permits that did not change, and the check
reds a tree in which nothing happened.

**Reproduced** (base `79b1388c5`, 3 comment lines inserted at the top of
`internal/tools/tasks.go`):

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check
  NOT PERMITTED (drifts, absent from the table): devdoc/capability-demo-closure.md
      recorded-line-blank internal/tools/tasks.go:5789  var taskParentReleaseStatuses = ...
  NOT PERMITTED (drifts, absent from the table): devdoc/merge-backlog-terminal-guard.md
  NOT PERMITTED (drifts, absent from the table): devdoc/task-render-pct-extra-determination.md
  STALE PERMIT (in the table, no longer drifts): devdoc/capability-demo-closure.md
      number-drifted internal/tools/tasks.go:5789  var taskParentReleaseStatuses = ...
  3 unpermitted, 1 stale permit(s)
exit status 1
```

Every one of those rows cites **the same text at the same recorded number**; only
the number's arithmetic moved. `capability-demo-closure` is the sharpest case: it
re-paired as `number-drifted` → `recorded-line-blank` because `5789` now lands on
a BLANK line, so the **verdict** moved too — same finding, only the arithmetic.
Evidence: `~/.gopi/sessions/01a12f66-evidence/{A-base,B-inserted}.txt`.

## The decision

**A permit's identity is the pair the claim already gates on — (document, file,
recorded text) — and nothing else.**

```go
func (f AnchorFinding) Key() string {
    return strings.Join([]string{f.Doc, f.Path, f.Text}, AnchorNumberBaselineFieldSep)
}
```

Stored column by column, the rule reads: *a permit is keyed on the doc that cites,
the file it cites, and the text it records.* The `:NNN` tail of the recorded cell
and the class it produces are **reported** — they are on `AnchorFinding`, they are
in the table's verdict column, `AnchorNumberIncoherent` still counts them, and
`gopi tool doccheck` still prints the drift — but they are not an identity.

The author's own comment in `anchor_number.go` already mandated this one level up:
the key must be "a function of the doc and the ROW's own recorded cells, never of
a line number". The manifest's own row line was excluded for exactly this reason;
the cited line number is the same kind of arithmetic.

## Why this is not "the key ignores everything"

The removed fields are the ones a **pure insertion** moves; the kept fields are
the ones a **re-point** moves. That is the whole distinction, and it is tested
from both sides:

| edit | key | arms |
|---|---|---|
| lines inserted above the cited site | unchanged | no unpermitted, no stale |
| the manifest is re-pointed to other text | changed | old permit stale + new finding unpermitted |

The discriminating test is
`TestAnchorNumberNumberChangeOverTheRealFrozenPopulation` (over the **real**
shipped freeze — 154 permits — and the **real** tree, shifting every recorded
number and flipping every class): it asserts every permit still covers its row,
and then re-runs the same material with one recorded TEXT changed to assert the
permit no longer covers it. Its sibling
`TestAnchorNumberArithmeticOnlyClassMoveKeepsThePermit` states the defect as a
fixture, and `TestAnchorNumberGenuineTextChangeStillPairsBothArms` is the
re-point control that must keep pairing.

**Red-then-green, measured** (the pre-fix `Key()` restored, one test run):

```
--- FAIL: TestAnchorNumberArithmeticOnlyClassMoveKeepsThePermit
    a permit written before an arithmetic-only move no longer covers the row:
    unpermitted=[... blankmove.md:1 ... recorded-line-blank] stale=[... src.go:0 ...]
--- FAIL: TestAnchorNumberNumberChangeOverTheRealFrozenPopulation
    a pure number shift re-keyed a frozen row:
    "devdoc/cov-shot-hp.md\tnumber-drifted\tinternal/shot/shot.go:124\tfunc Capture(...)" →
    "devdoc/cov-shot-hp.md\trecorded-line-blank\tinternal/shot/shot.go:141\tfunc Capture(...)"
```

and green with the fix in place.

## Why the line number is nonetheless still in the table

It is the **accelerator**, and that is a different job from identity:

- `AnchorFinding.Number` / `TextAt` are what `String()` prints and what a reader of
  a reading needs ("the recorded text is at :2222" is how a human re-points a row).
- The census still reports `number-drifted` / `recorded-line-blank` as real
  findings — the gate still fails on drift NOT permitted; only *permit
  regeneration and staling* are suppressed for arithmetic.
- The anchor CELL stays spelled `path:line` in the artifact, so the table reads
  the way a person wrote it.

## The measured basis for the key being unambiguous

A key that drops the number has to be collision-free, or a permit covers the wrong
row. Measured at the base over **both** populations
(`~/.gopi/sessions/01a12f66-evidence/key_collisions.py`):

| key | frozen rows (154) | live anchor rows (237) |
|---|---|---|
| `(doc, path, text)` — **chosen** | 0 dup | 0 dup |
| `(doc, text)` | 0 dup | 0 dup |
| `(doc, anchor-cell, text)` — pre-fix spelling | 0 dup | 0 dup |
| text alone (no doc) | 8 non-unique groups | 11 |

The ambiguity the epic feared lives **only in a key that keeps the NUMBER**: 17 of
the 156 frozen rows carry a text that occurs more than once in its own file (7 of
them record a bare `}`, and `}` alone is 1030 lines of `internal/tools/tasks.go`),
so *text+number* was the thing pinning those rows apart. With the number
gone, each row's `(doc, path, text)` is still unique because it is the triple a
human would use to point at the claim — and that is exactly why the number can be
demoted without losing discrimination.

## The table the fix lands with, and why it is 156 rows and not 154

Two rows were unpermitted at the base (`merge-backlog-terminal-guard.md`,
`task-render-pct-extra-determination.md`) and two permits carried a
`first-frozen` provenance claim the probe refutes. Both were real: the shipped
table was regenerated from the current tree

```
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv
# 237 anchor row(s) scanned, 156 incoherent (3 new permit(s), 153 pre-existing)
```

and the diff is legible as a re-keying rather than a re-decision: comparing the
old table's rows under the NEW spelling, **2 keys added, 0 removed** —
`merge-backlog-terminal-guard.md|internal/tools/tasks.go|}` and
`task-render-pct-extra-determination.md|internal/tools/tasks.go|fmt.Fprintf(&out, taskListRowFormat,`
were both already drifting at the base and simply had no permit; the other 154
rows carry the same `(doc, path, text)` before and after.

## One source of truth for the two shapes

The table's columns and a key's fields are **not the same shape** (5 vs 3: a key
drops the verdict and collapses the anchor cell to its file half), and conflating
them is a silent, plausible bug — it was made while writing this change: the
provenance probe read the key's field 2 (the **text**) as if it were the anchor
column, split it on `:`, and materialised **0** anchored files, measuring an empty
base population. The fix gives each shape its own named constants
(`AnchorNumberBaselineKey*Column` for the table,
`anchorNumberBaselineKeyPathField` / `anchorNumberBaselineKeyFields` for the key)
and one reader per job (`AnchorNumberAnchorPathIn` for a stored cell,
`AnchorNumberBaselineKeyPathOf` for a key, `AnchorNumberBaselineStoredKey` as the
one bridge between a stored row and a live finding).

## The prose-path-line rung is red on master, and it is not this branch's

`go test ./internal/docclass/` also carries a SECOND rung
(`prose-path-line-baseline.tsv`, prose citations past EOF). It is red — and it
was red **before** this branch merged anything, measured on its own worktree at
the base, and every file of that rung is **byte-identical to `origin/master`**
after the merge:

```
$ git diff origin/master --stat -- internal/docclass/prose_path_line_baseline.go \
      internal/docclass/prose_path_line_gate_test.go \
      internal/docclass/prose_path_line_stability_test.go \
      internal/docclass/testdata/prose-path-line-baseline.tsv tasks/01a12edb-report.md
(empty)
```

So `make test-budget PKGS='./internal/docclass'` reds for a reason this change
cannot cause and does not fix. The rung this change OWNS is green:

```
$ go test ./internal/docclass/ -count=1 -run AnchorNumber
ok  	github.com/nick/gopi/internal/docclass
```

Evidence: `/home/user/.gopi/sessions/01a12f66-evidence/prose-rung-pre-existing-red.md`.

## Gates re-run

- `go build ./...` — OK
- `go test ./internal/docclass/ -run AnchorNumber` — ok
- `make test-budget PKGS='./internal/docclass ./cmd/agent'` — see the task report
- `./bin/gopi tool doccheck` — **exit 0** on this branch, `the devdoc corpus is
  consistent — every file under devdoc/ has a well-formed row`.

## The blank line at DOC-INDEX.tsv:2368 (a pre-existing defect, measured)

`doccheck` also reported `malformed-row — devdoc/DOC-INDEX.tsv line 2368:
1 field(s), want 4`. That line is **empty**, it is **on master**, and it is
present in a detached worktree at the base with none of this branch's changes —
so it is not this change's doing. It is filed as its own bug row
(`01a12fa6-02e2-7c14-ab9a-875fd967f36b`, tags=bug), with the measurement.

**What happened to it here, stated plainly**: writing the manifest through the
tool — `gopi tool doccheck --fix`, which classifies this devdoc and rewrites the
file through the repo's own writer — **normalized the blank line away**. So this
branch's diff carries that one-line deletion, and `doccheck` is green on it. The
defect itself is not this branch's to fix (the empty line needs a writer-side
decision, which is what the bug row asks for), but the tree this branch lands is
green rather than red, and the report says how it got that way instead of
claiming the blank line was never there.
