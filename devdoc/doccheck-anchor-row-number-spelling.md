<!-- doc-class: historical -->
# The anchor diagnostic's leading token is a ROW, not a citation

**Task** `01a13426-e78a-7db8-b317-3dc99a0c34ad` · **base** `b79ce73` ·
**branch** `task/01a13426e78a7db8b3173dc99a0c34ad`
**Decision** option (a) — re-spell the diagnostic. Recorded here and on the row.

## The observation, and the cost it was charging

`gopi tool doccheck` printed an unpermitted anchor finding as

```
devdoc/<doc>.md:717: anchor internal/docclass/anchor_number.go:12 at <text> — number-drifted (recorded text is at :30)
```

and the first token of that line, `devdoc/<doc>.md:717`, is spelled in the
repository's own `<path>:<line>` citation grammar — the one the prose-path-line
rung (`internal/docclass/prose_path_line.go`) reads out of every report under
`tasks/`. A landed report copied the token into its prose, the rung read the copy
as a claim about `devdoc/<doc>.md`, and **three rows were frozen as `past-eof`
citations pointing at nothing** — three permits added to a table whose direction
is DOWN (Rule 39).

The rung was behaving correctly: it cannot distinguish a mis-spelled transcript
from a citation, and it must not start guessing (its header states that boundary).
**The defect was on the producer side.**

## Why the number was ambiguous at all

An `AnchorFinding` mixes **three** files, which no other finding in the package
does:

| field | what it is |
|---|---|
| `Doc` | the citing document (`devdoc/<doc>.md`) |
| `RowLine` | the line of **`devdoc/DOC-INDEX.tsv`** that carries the row |
| `Path:Number` | the anchored claim the row records |

So `Doc` + `RowLine` is not a path and a line of that path — it is two different
files, and the colon said otherwise.

## The decision

**Option (a): re-spell.** `Doc` and `RowLine` are now joined by
`AnchorFindingNumberSep` — `#`, the fragment marker — so the row reads

```
devdoc/<doc>.md#717: anchor internal/docclass/anchor_number.go:12 at <text> — number-drifted (recorded text is at :30)
```

`#` is the language's own "a position inside this document" marker, it appears in
no `path:line` citation grammar the repository reads, and it is visible rather than
subtle — a reader who copies the token copies `#717`, which no rung resolves.

**Why not option (b) "by design".** The ambiguity is not a tolerance the tree
needs; it is a cost the tree pays — three permits that will never cover a real
citation, and a spelling that re-charges the same cost every time a report quotes
the diagnostic (which reports do, by design: quoting it is the whole point of
having it). Recording "by design" would have preserved a producer that manufactures
false citations.

**Why the sibling rungs keep their colons, deliberately.**
`ProseAnchorFinding.String()` and `ProsePathLineFinding.String()` also print
`doc:rowline`, and that spelling is **TRUE**: their `RowLine` is the line of the
citing report the mention sits on, so `tasks/<report>.md:<line>` is a citation of
a real line of a real file. Only this rung's spelling lied. A future reader must
not "make them consistent" — the difference between them is the fix.

## What was NOT touched (the epic's constraints, each as a test or a measurement)

- The citation rung's predicate and floors: untouched. `prose_path_line.go` and
  `prose_path_line_gate_test.go` are byte-identical to the base.
- The permit table: **not shrunk, not grown.** `git status --porcelain` lists
  exactly two files, neither of them a baseline; both `census --check` arms exit 0
  on the branch, and `gopi tool doccheck` still reports `163 of 163 frozen`.
- `AnchorFinding.Key()` and every stored column: untouched, so no permit's identity
  moved. This was a **display** change, and the census proves it (the anchor census
  reports the freeze exact, unchanged, after the change).

## The gate

`TestAnchorFindingSpellingIsNotReadAsACitation` (`internal/docclass/anchor_number_gate_test.go`)
drives the **real** reader, not a restatement of its regexp, over a fixture corpus
whose report quotes the diagnostic verbatim. Three arms:

1. the shipped spelling produces **zero** citations of `devdoc/x.md` — and the
   message visibly carries the ROW label;
2. the pre-fix colon spelling (spelled in the test as the red arm's own material)
   produces **exactly one** false citation, `devdoc/x.md:717 is past EOF` — i.e.
   the arm is discriminating, and the fix is the separator rather than a reader
   that stopped reading;
3. a genuine citation of the same document past its end is **still reported** — the
   control that keeps arms 1 and 2 from being vacuous.
