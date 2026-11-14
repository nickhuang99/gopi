<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Devdoc: refreshing the anchor-NUMBER freeze table (the ratchet moves DOWN, never up)

Task `01a13338992d78539e9beabb5127283e`, branch
`task/01a13338992d78539e9beabb5127283e`, base `7d0f7848b`. This is the REWORK of
the archived merge run for task `01a132a8` (`task/01a132a83e64705ab3e12a9909fd6f7a`,
base `128fad4c8`): that branch could not be merged, because its one regenerated
artifact — the anchor-NUMBER freeze table — conflicted with a regeneration of the
same table on the other side. This run re-applies the SEMANTIC INTENT on the
current tree instead.

## The intent, and what this tree measures instead

The archived run's symptom: `./bin/gopi tool doccheck` exited 1 with ONE
finding — `devdoc/merge-resolve-peak-hour-guard.md`, anchor
`cmd/agent/main.go` (recorded line 982) at `}`, reported as **number-drifted**
(the recorded text is still in the file, but on line 80). Its repair was to
regenerate the table so that row becomes a **permit**.

On THIS tree that row is not in the drift population at all, and it is not in the
freeze table either. It is reported under a DIFFERENT class, and the class is the
whole story:

```
doccheck:   TEXT AMBIGUOUS (matched by more than one line — the number is not
            checked and the row is NOT frozen):
            devdoc/merge-resolve-peak-hour-guard.md#1268:
            anchor cmd/agent/main.go:982 at } — text-ambiguous (recorded text is at :80)
```

The recorded text is a bare `}`. `cmd/agent/main.go` contains 104 lines that a
`^[[:space:]]*}$` match satisfies (`grep -c '^[[:space:]]*}$' cmd/agent/main.go`),
so the text does not identify a line, the number is not checked for that row, and
the row can never be frozen: a permit covers **only** rows whose recorded text is
unique, and the table's own header states the consequence ("a frozen row whose
text later becomes ambiguous loses its permit and goes stale"). 38 of this tree's
253 scanned anchor rows are text-ambiguous.

So the second half of the archived intent — "add the one missing permit row" —
has no re-application here, and that is a MEASUREMENT, not a judgement call about
effort: the generator cannot emit that permit (it emits `incoherentOnly` rows,
and a text-ambiguous row is by definition not one), and a hand-written row would
have to invent the provenance claim the generator decides by probing the freeze
commit's tree. A permit invented for an ambiguous row is a phantom — a permission
that covers a verdict the tree never produced. The table therefore keeps its
shape, and the deliverable of that half is this measurement.

## What the generator measures at this tree

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv
anchor-number-census: 253 anchor row(s) scanned, 139 incoherent
  (15 first-frozen permit(s), 124 pre-existing claim(s));
  the write's own delta: 0 permit(s) added, 0 permit(s) removed → wrote …/anchor-number-baseline.tsv
```

and the check arm re-derives the artifact rather than trusting it:

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 253 anchor row(s), 139 incoherent; table carries 139 row(s)
anchor-number-census: 38 of 253 scanned row(s) are text-ambiguous (matched by more than one line —
  the recorded number is not checked and such a row is never frozen)
anchor-number-census: header figures re-derived and equal (frozen=139 pre-existing=124
  first-frozen=15 ambiguous=38)
anchor-number-census: table matches the tree — the freeze is exact        (exit 0)
```

The table is DATA (Rules 38/39): it is REGENERATED, never hand-edited, and the
regeneration at this tree is byte-identical to the committed artifact — the only
byte a re-run moves is the output PATH echoed back inside the generator's own help
comment, and an in-place run (the documented recipe) does not even move that. The
write's own delta is 0 added / 0 removed, which is the honest answer to the
question a reviewer of a freeze table actually has.

## Why the archived figures are not this tree's figures

The archived report quotes `243 anchor row(s) scanned, 160 incoherent (8 new
permit(s), 152 pre-existing claim(s))`, a table that grew 159 → 160 rows. NONE of
those five numbers is this tree's: the manifest and the tree both moved under it
(this tree: 253 / 139 / 15 / 124, table 139 rows). This is the exact hazard the
rework exists for — a census figure is a statement about ONE tree, and copying the
archived one forward would have made this document false on the day it landed
(Rule 38). The freeze commit is unchanged (`0b3c1de29`), which is what keeps the
two runs comparable at all.

## The direction that is checked, not assumed

`compare()` refuses a permit whose row no longer drifts ("the allow-list only
ratchets down, so a stale permit is a silent permission"), and a permit whose
provenance claim the freeze tree cannot support. Both arms are exercised by the
generator's own `--check` on the real table, and the anchor/docclass gate suite
runs the same two faces — one rule (`AnchorFinding.Key`: citing doc | anchored
file | recorded text, with the LINE NUMBER and the VERDICT deliberately outside
the identity), so the program and the verb cannot disagree about what a permit
covers.

## The two inherited reds this run had to clear, and why they are in THIS change

The acceptance criteria require the anchor/docclass gates GREEN, and at this
branch's base two of them were already red — inherited, not introduced here,
proven by reverting this run's own two files and re-running them on a pristine
tree (`TestAnchorNumberShippedHeaderFiguresMatchTheProbe` → exit 1;
`TestCommandsDocGeneratedBlockMatchesSource` + `TestGeneratedDocAnchorChange
RequiresItsGate` → exit 1). Their inputs are byte-identical at `7d0f7848b` and at
`master` (measured: `git diff --stat 7d0f7848b master -- <those files>` prints
nothing), so neither is a property of this branch. Both had a mechanical repair,
and both are in this change because "the freeze is exact" is not a deliverable
while the arm that measures it is red:

1. **`internal/docclass/anchor_number_header_test.go`** — the arm re-derived
   three of the four figures and left the fourth (`ambiguous`) at its zero value.
   That is not "unchecked": `AnchorNumberHeaderFigureVerdict` compares EVERY key
   in `AnchorNumberHeaderFigureKeysInOrder`, so the missing half was compared as a
   `0` the run never measured and the shipped `ambiguous=38` failed against it.
   The repair asks the SAME counter the census's own check arm asks
   (`AnchorNumberVerdictCount(findings, AnchorNumberAmbiguous)` over the manifest
   resolved against the tree), so the Go arm and the generator now re-derive the
   figure through one expression (Rules 13/14).

2. **`COMMANDS.md`** — its generated verb-index block published
   `cmd/agent/cli_doccheck.go:297`, a line that is a comment since the declaration
   moved to `:321`. Regenerated with the documented recipe
   (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`,
   one line changed). The second red was the sharper one: the anchor RESOLVES at
   `:297` while pointing at a comment, so a reference-only check cannot see the
   drift.

Both are outside the anchor-NUMBER freeze table's own change (which stays empty),
and both leave the freeze exact: `--check` still prints "the freeze is exact"
(exit 0) after them, and the scoped suites are green.

## Not done here

- **The archived run's one-row delta has no counterpart on this tree.** Nothing
  was added to the table (nothing could be: see above), so this branch's change to
  the freeze table is EMPTY — the artifact is regenerated and proven exact, and
  that is the whole of its change.
- **The wider corpus was not regenerated.** `devdoc/DOC-INDEX.tsv` gained exactly
  one row (this document); the sibling frozen tables (prose-anchor,
  prose-path-line) were not touched.
- **The anchor-NUMBER section of `doccheck` is clean on this tree** (139 of 139
  frozen, 0 unpermitted, 0 stale permits, 0 untrue claims, 0 gaps between `devdoc/`
  and the manifest). The verb still exits 1 on its THIRD leg, the stored-cell drift
  question, and that red is INHERITED, not introduced here: at this tree the leg
  reports `787 of 2511 rendered row(s) whose stored text is not what the classifier
  renders today` (measured on the committed tree). The two counts differ from a run
  taken before this branch by exactly ONE row — this document's own manifest row,
  which the manifest did not carry before — so the leg's population is the corpus's,
  not this change's: the files that decide the leg are byte-identical to master's,
  and this run did not change them. Clearing it is a corpus-wide regeneration
  decision (`python3 tools/docclass/classify.py <repo>`), not a side effect of this
  repair. The three legs are therefore separable and this run's verdict is per-leg:
  gap = 0, anchor-NUMBER = clean, stored-cell = the corpus's own backlog.

