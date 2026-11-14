<!-- doc-class: historical -->
# A header figure that nothing re-derives is not a measurement — it is a snapshot of the last write

*Task `01a134c5e41b7b4da917e9bac66a323d` (epic reframe, verdict DOABLE).*

## The defect, measured

`internal/docclass/testdata/anchor-number-baseline.tsv` carried, on master tip
`82f8b9529`:

```
# measured 245 anchor row(s) scanned; 163 incoherent (frozen below)
```

The same tree scanned **248** rows, and

```bash
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check   # exit 0
```

exited **0**. The figure was not wrong when it was written — it was a snapshot of
the tree *at the moment of that write*, and it rotted with the next manifest
commit. Nothing could notice, because:

- every machine reader of this table **skips comments** — the header is prose to
  the parser, so the number is not read by anything; and
- the one surface that re-derives the population (`--check`) **printed** its own
  count and never **compared** it to the header's.

So the artifact asserted a measurement nobody took. That is the exact shape Rule
38 names: a census figure must name its generator and the commit it was measured
at, or it is a rumour with a number attached.

## What replaced it

**One line, whose every figure the check arm re-derives and compares.**

```
# figures: frozen=164 pre-existing=152 first-frozen=12
```

Three counts, each a property of artifact-or-tree rather than of a moment:

| key | what it counts | re-derived by |
|---|---|---|
| `frozen` | the artifact's own data rows | counting the table's rows (`--check` reads the file it was handed) |
| `pre-existing` | permits whose key is incoherent in the freeze commit's own tree | `AnchorNumberProvenanceProbe` over the freeze commit |
| `first-frozen` | permits this table revision is where the key first appeared | the same probe |

The live scan count («N anchor rows scanned») is **not lost — it is no longer
stored**: `--check` reports it (`scanned 248 anchor row(s), 164 incoherent;
table carries 164 row(s)`), and `TestAnchorNumberHeaderFiguresMatchTheProbe`
exercises the tree half in the suite. A number whose whole point is that it
moves belongs on a *report*, not in a stored claim.

**The comparison, not an echo.** `compare()` now parses the header, re-derives
the three figures, and fails by name:

```
HEADER FIGURE IS NOT THE MEASUREMENT: the header declares frozen=163 while this
run re-derived frozen=164 — the figure is not the measurement; regenerate the
table with internal/docclass/testdata/anchor-number-census.go (never hand-edit
it, Rule 36/38)
```

**The second door, and it is the one the old line came through.** A checkable
figures line is not enough if a hand-written count sentence beside it can say
anything — so `ParseAnchorNumberHeaderFigures` refuses a count sentence ANYWHERE
in the header except the figures line:

```
anchor-number-census: <table>: line 32 states a count nothing re-derives
("245 anchor" in "# measured 245 anchor row(s) scanned") — a figure in this
header must live on the ONE "# figures:" line, where the check arm recomputes it
from the artifact and the tree
```

The detector is deliberately narrow (`anchor` … `scanned` adjacency), because the
header's own prose legitimately carries numbers that measure nothing — a commit
(`0b3c1de29`), a task id (`01a12d1e`), a `path.go:NNN` reference. Refusing those
would make the header unmaintainable, and an unmaintainable header gets its
figures hand-written again.

## One spelling, two readers (Rule 14)

`internal/docclass/anchor_number_baseline_header.go` owns the vocabulary and the
rendering:

- `AnchorNumberBaselineHeaderFiguresLead` (`# figures:`) — the ONE anchor the
  checker keys on (not the word `figures`: a renamed line is refused, not
  silently unchecked);
- `AnchorNumberBaselineHeaderFiguresLine` — the writer;
- `ParseAnchorNumberHeaderFigures` + `AnchorNumberHeaderFigureVerdict` — the
  reader.

The generator's `# figures:` line is rendered by calling the same function
`--check` parses back, and `TestAnchorNumberHeaderFiguresLeadIsTheOneSpelling`
asserts the round trip in the ONE key order. An unknown key or a missing key is
refused, never ignored — a figure the reader does not know is a figure nobody
re-derives.

## Why not "just compare the scan count"

That was the obvious alternative and it is the one the epic's constraint forbids:
making the header's live scan count a hard gate turns **every unrelated devdoc
add/remove** into a red build (adding one anchored row moves the scan count by
one), which is precisely what the freeze table exists to avoid and what would get
the gate switched off. A re-derivable figure must be a function of the artifact
and the freeze commit — both of which change only when somebody *means* it.

## Arms and red proofs (all observed in this run)

| arm | input | result |
|---|---|---|
| green | shipped table | `--check` exit 0; prints `header figures re-derived and equal (frozen=164 pre-existing=152 first-frozen=12)` |
| red 1 | copy with `frozen=163` | exit 1, `HEADER FIGURE IS NOT THE MEASUREMENT …` |
| red 2 | copy with the retired line re-added | exit 1, `line 32 states a count nothing re-derives ("245 anchor" …)` |
| red 3 | `TestAnchorNumberHeaderFigureNamesTheMismatch` (`245` vs `248`) | red on the forged figure, names both halves |
| red 4 | `TestAnchorNumberShippedHeaderCarriesNoLiveScanFigure` | red if the shipped header or the generator carries a `scanned` figure again |

Idempotence: three consecutive generator runs leave
`sha256 88415562a26c8af4…` unchanged.

## Follow-up (task 01a13a82): the SAME defect returned with five keys — a figure's CLASS is now explicit

**Measured on master 969ef17ae (2026-10-14):** `--check` exited 1 with

	HEADER FIGURE IS NOT THE MEASUREMENT: the header declares ambiguous=38 while
	this run re-derived ambiguous=37

while the SAME run printed `0 unpermitted, 0 stale permit(s), 0 unreproducible,
0 unreadable and 0 untrue` — the permits were EXACT and only the header had moved.
The commit that WROTE that line (6bc71ab67) exited 0 against its own tree; master
went red because an ordinary devdoc merge moved the manifest. That is the exact
shape this table exists to prevent (the first instance is the section above).

**Root cause, and it is a distinction the header did not make:** of the five
figures the line carried, three are a function of the ARTIFACT plus a FROZEN
REFERENCE — `frozen` (the table's own data-row count), `pre-existing` /
`first-frozen` (each permit's own reason, re-derived by the provenance probe
against the freeze commit 0b3c1de29). Those cannot move without the TABLE
changing, so each is a stored CLAIM and a claim belongs where a later run can
contradict it. The other two — `ambiguous` (rows the uniqueness criterion could
not decide) and `numbered` (rows still carrying the legacy `:NNN` shape) — count
the tree THIS RUN scans, so any manifest commit moves them. Comparing a moving
number makes every devdoc edit a red build whose repair is "regenerate the
table", i.e. the gate degenerating into red-repair (Rule 38: a moving number
belongs on a report).

**The fix — two named classes, ONE classifier:**

- `AnchorNumberBaselineHeaderFigureCompared` / `...Reported`, and
  `anchorNumberHeaderFigureClassOf(key)` as the single classifier. The rendered
  key set, the parser's required key set and the verdict's compared key set are
  all derived from it (`AnchorNumberHeaderFigureKeysOfClass`), so no second list
  exists to drift and a key cannot be added without being classified (Rule 13/14).
- The figures line now carries ONLY the compared class. Storing a reported key is
  REFUSED BY NAME with the surface that reports it.
- The check arm PRINTS the reported class on every run, green or red
  (`AnchorNumberBaselineHeaderReportedLine`) — a demotion, not a loss.
- `AnchorNumberHeaderFigureKeysInOrder` stays the whole VOCABULARY (every key the
  parser knows), and it is no longer the list of keys the line carries.

**The core regression proof (the point of the change), measured on a COPY tree:**
one devdoc document + one manifest anchor row added (254 scanned vs 253):

	pre-change code, churned tree: exit 1 — "declares numbered=250 while this run
	    re-derived numbered=251", with 0 unpermitted / 0 stale permit(s)
	post-change code, churned tree: exit 0 — "table matches the tree — the freeze is exact"

and the red direction is intact: forging `frozen` / `pre-existing` /
`first-frozen` each exits 1 naming both halves, and storing `ambiguous` on the
figures line is refused by name.

**What did NOT change:** permit pairing, the ambiguity/read floors, the empty-table
criterion, the provenance arm and the count-sentence refusal are untouched — this
change narrows what the header may STORE, never what the tree must satisfy.

## Files

- `internal/docclass/testdata/anchor-number-census.go` — the writer (header
  template + the header check arm); the ONLY writer of the table (Rule 36/38).
- `internal/docclass/anchor_number_baseline_header.go` — the vocabulary, the one
  line's rendering, the parser, the verdict.
- `internal/docclass/anchor_number_header_test.go` — the regression arms.
- `internal/docclass/testdata/anchor-number-baseline.tsv` — regenerated (DATA,
  never hand-edited).

## Not done (honest)

- The retired line's **shape** is refused by a narrow adjacency detector
  (`anchor` … `scanned`). A hand-written count that avoids both words — `# this
  run covered 249 rows` — is refused by the *count-sentence* arm only if it
  carries a count word plus a noun phrase the detector knows; today it does not.
  The figures line itself is fully checked, so this is a gap in the *prose*
  guard, not in the measurement.
- No attempt was made to make `# provenance:` machine-checked beyond what the
  probe already re-derives (`pre-existing` / `first-frozen` per permit); the
  freeze commit is a named constant, and a header naming a *different* commit is
  not detected (the probe would simply re-derive against its own constant).
