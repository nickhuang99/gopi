<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The blank-line mask: one manifest, two shape verdicts

This doc is classified `normative` by the corpus's own generator: the shared
shape rule and the gate below are standing properties of this tree, not merely
this run's account. The run's own observations are quoted as evidence.

## What was wrong

`devdoc/DOC-INDEX.tsv` carried a blank line at 2369. The manifest had two
readers, and they disagreed about that one line:

```
$ ./bin/gopi tool doccheck
doccheck: malformed-row  — devdoc/DOC-INDEX.tsv line 2369: 1 field(s), want 4 (tab-separated)
doccheck: 1 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv       # exit 1

$ go test ./internal/standards/ ./internal/docindex/ ./cmd/agent/ -run 'DocIndex|Manifest|DocClass|DocCheck'
ok  	github.com/nick/gopi/internal/standards
ok  	github.com/nick/gopi/internal/docindex
ok  	github.com/nick/gopi/cmd/agent
```

Three defects, and they are separate:

1. **The divergence.** `internal/docindex` counted every blank line among the
   data rows (`reindex`: `len(f) != ManifestCols || f[0] == ""`), so it reported
   it as a malformed row; `internal/docclass.ParseManifest` **filtered it**
   (`strings.TrimSpace(text) == "" → continue`), so the whole class-driven gate
   family passed the same bytes. `tools/docclass/classify.py --check` filtered it
   too and printed `0 gap(s)`.
2. **The misleading wording.** `strings.Split("", "\t")` answers ONE element, so
   a line with **zero** fields was reported as `1 field(s), want 4` — a sentence
   that sends the operator hunting for a row that lost a column, when there is no
   row at all.
3. **The line's origin.** The `pre-merge` merge commit `7e7b665da` unioned two
   writers whose sides were joined with `strings.Join(lines, "\n")` over lines
   that each already ended in `\n` — one blank line per join. The manifest is
   data (Rule 36), so nothing had noticed.

## The fix: ONE shape rule, called by both readers

`internal/docclass/shape.go` (the corpus's own leaf package, Rule 14) gained the
single definition of what a manifest line IS. The rule lives with the manifest's
other shape constants (`ManifestCols`, `ManifestSep`, `MinReasonLen`) because
that file already exists for exactly this reason — a second spelling of a
manifest shape is the drift class it was created to remove.

```go
docclass.ManifestLineShapeOf(line) ManifestLineShape   // Row | Blank | Fields
docclass.ManifestLineProblem(lineNo, line)  string      // the ONE sentence
docclass.ManifestBlankLine                              // "blank line"
```

The predicate is checked **before** any split: a line whose `TrimSpace` is empty
is `ManifestLineBlank`, so it can never be handed a field count. The refusal's
sentence is rendered by `ManifestLineProblem`, and both readers print it verbatim
— the check's `CorpusGap.Detail` and the gate's `ParseManifest` error differ only
in their file prefix.

Callers that changed:

| reader | before | after |
|---|---|---|
| `docclass.ParseManifest` | skipped blank lines | refuses them via the rule |
| `docclass.ParseManifestLines` (merge lane) | skipped blank lines | refuses them via the rule |
| `docindex.(*Manifest).reindex` | counted a blank as a 1-field row | records `MalformedRow{BlankLine: true}` |
| `docindex.CheckCorpus` | `"line %d: %d field(s), want %d"` | the shared sentence, keyed on the LINE |
| `docindex.RepairGap` | (no path) | deletes the line (see below) |
| `classify.py --check` | filtered blanks (`0 gap(s)`) | reports them |
| `classify.py manifest_data_lines` | returned lines, no numbers | returns `(line_no, line)`, blanks included |

**Blank lines above the column header are still tolerated** by both readers: the
manifest's own prose block is separated from the header by blank lines in some
writers, and that region is not data.

## The one shape whose remedy is a DELETION

A short row is a row that lost a column, and `Repair` rewrites it **in place,
keeping the class its author wrote**. A blank line is not that: the format defines
no blank separator between rows, so there is nothing to rewrite and nothing to
preserve — the remedy is to delete the line. Hence `ScopeGaps.BlankLines []int`
(line numbers, not paths: a blank line keys on nothing else), `dropBlankLines`,
and deletion in **descending** line order so a removal cannot shift a line still
to come. `RepairGap` routes the shape to that arm before the `ManifestRel` skip
that would otherwise drop it.

On this branch the deletion was performed by the corpus's own surface, not by
hand (Rule 36):

```
$ ./bin/gopi tool doccheck --fix
doccheck: repaired 1 path(s): devdoc/DOC-INDEX.tsv
doccheck: nothing to repair in the GAP class (0 gaps between devdoc/ and devdoc/DOC-INDEX.tsv)
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   156 of 156 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv …

$ git diff --stat devdoc/DOC-INDEX.tsv
 devdoc/DOC-INDEX.tsv | 1 -
 1 file changed, 1 deletion(-)
```

Exactly one line removed, no row rewritten.

## The gate that keeps the shape from returning

`internal/standards/docindex_blank_line_gate_test.go` — six arms, on the smallest
sample that reproduces the incident (a manifest whose column line is followed by
a row, a blank line, and a second row, in a throwaway git repository):

- `TestManifestShapeRuleRefusesABlankLine` — the rule's own verdict, including
  the assertion that a blank line's sentence carries **no** field count.
- `TestBothManifestReadersRefuseTheBlankLine` — **the epic's acceptance**: both
  readers refuse the same fixture, name the same line, and print the same
  sentence.
- `TestManifestShapeRuleIsTheOneBothReadersCall` — the whitespace-only twin of
  the shape: a reader that splits before it trims reports this as `1 field(s)`,
  which is the divergence returning.
- `TestBlankLineRepairTurnsTheSameTreeGreen` — the repair deletes the line, loses
  no row, and both readers accept the tree afterwards.
- `TestCorpusFacesRefuseTheBlankLineTogether` — the repo's **own** Python checker,
  run for real, refuses the same line (this is the arm that was red on the
  pre-fix script).
- `TestGuardedTreeCarriesNoBlankLine` — the standing gate on **this repo's** own
  manifest, with a row-count floor so a fragmentary read cannot pass quietly.

`tools/docclass/classify_test.py` gained the same regression from the Python side
(`test_check_reports_a_blank_line_among_the_rows`), so the corpus's own test
suite carries it too.

### RED, measured

The test file references the new API, so an arm cannot exist on the pre-fix
sources at all; the red is therefore taken where each half actually behaves
differently from the other, on the pre-fix code.

Pre-fix `classify.py`, post-fix Go readers, the fixed manifest, the new test:

```
--- FAIL: TestCorpusFacesRefuseTheBlankLineTogether (0.16s)
    docindex_blank_line_gate_test.go:264: the corpus's own checker reported a consistent corpus for a
    manifest carrying a blank line while both Go readers refuse it — the three faces cannot print
    opposite verdicts about one file (the defect this shape rule exists to remove):
        0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
FAIL	github.com/nick/gopi/internal/standards	1.268s
```

Pre-fix sources, pre-fix manifest: the Go standards gates were green while the
check exited 1 — the divergence itself, quoted in "What was wrong" above. The
python regression's own red is the same sentence: pre-fix `manifest_data_lines`
filtered the blank line, so the fixture's `--check` printed `0 gap(s)` and
`rc=0` where the test demands `rc=1`.

GREEN, same surfaces, fixed tree: quoted under "Verification".

## Not done (honest)

- **`classify.py`'s blank-line rule is a second implementation of the Go
  predicate, not a call into it.** The corpus's rule of record is the Go shape
  rule (`internal/docclass`), and the Python generator is the corpus's writer
  policy — the same split the class vocabulary already lives under (the Go half
  is a leaf constant, the generator's half is data; see `ArchiveDir`'s comment in
  `internal/docclass/docclass.go`). The two are held equal by a test on each side
  (the fixture asserts the shared sentence text), not by one implementation.
  Nothing in the build runs Python, so this cannot be resolved by a dependency —
  it is the same shape the corpus already accepts elsewhere.
- **`cmd/agent/merge_union.go`'s union still joins with `strings.Join(out, "\n")`.**
  I traced that path and it does NOT produce the defect: `out` holds the row
  lines WITHOUT their newlines, and `mergeUnionRenderRow` renders a row with no
  trailing separator, so the join is correct there. The blank line came from the
  `pre-merge` commit's own text-level union of two whole file bodies. I did not
  add a gate over the merge lane's byte handling beyond what the readers now do —
  a union that produces a blank line is caught by both readers at the next read,
  which is the surface this epic asked for.
- **The two `internal/standards` files `gofmt -l` lists (`export_test.go`,
  `generated_doc_deps_test.go`) were already unformatted before this branch** and
  are untouched here.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go build ./...` → OK
- Tests: `go test ./internal/standards/ ./internal/docindex/ ./cmd/agent/ -run
  'DocIndex|Manifest|DocClass|DocCheck'` → `ok` ×3;
  `go test ./internal/standards/ -run 'BlankLine|ShapeRule|BothManifestReaders|CorpusFacesRefuse|GuardedTreeCarriesNo'`
  → `ok`; `python3 tools/docclass/classify_test.py` → `Ran 21 tests … OK`
- Ran (Rule 31): `./bin/gopi tool doccheck` (exit 0, "the devdoc corpus is
  consistent"), `./bin/gopi tool doccheck --fix` (exit 0, "repaired 1 path(s)"),
  `python3 tools/docclass/classify.py . --check` (exit 0, "0 gap(s)")
- Result: quoted in the sections above.
