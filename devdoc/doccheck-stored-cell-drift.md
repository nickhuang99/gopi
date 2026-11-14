<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# devdoc: the third question — stored-cell drift (doccheck)

**Status:** landed on `task/01a136c51e8c7867bc3e35b72b4a9eb3`, class `record`.
**Tree it was written at:** `46f2ba7141a07e35e6c4d04d0ec1ba61ef8b9b3c` (worktree HEAD of that branch).

## The finding this exists for

One run, one tree, two commands, and they disagreed about the same file:

```
$ python3 tools/docclass/classify.py . --check | tail -1
858 row(s) whose stored text is not what the classifier renders today

$ gopi tool doccheck
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

Both exited **0**. Neither sentence is false: doccheck was answering a
*coverage* question ("does every file have a row?") and printing it as a verdict
about the manifest, while a third of the corpus carried a class or a
live-derived tail the repo's own classifier would render differently *today*.
Nothing on the doccheck side counted it, mentioned it, or alluded to it — the
drift surfaced only when a merge-union or an anchor leg happened to touch one of
those rows.

The verb had two legs before this change (gaps, anchor NUMBER) and both are
honest about their own question. The defect was never that a leg lied; it was
that the CLEAN LINE spoke for the corpus while resting on one leg's answer.

## What was built

`internal/docindex/docclass_drift.go` — one CAPABILITY added to the verb
doccheck already owns (Rule 3: no new tool, no new script, no new dependency):

- `DocclassDriftRows(manifest []byte, rendered map[string]string)` — THE
  predicate, pure, red-provable. Rows are matched **by path**, and each of the
  three text columns (class, reason, evidence) is compared through `Cell`, the
  same normaliser the whole-corpus write path uses (Rule 14), so a cell copied
  byte-for-byte out of a manifest is EQUAL and a normalised difference is not a
  false positive.
- `DocclassDrift(root)` — runs the corpus's own classifier ONCE in its write-free
  rendering mode and compares. The **classifier is asked first**: a tool that
  read the manifest and then discovered it could not render it would have to
  report a number it does not have.
- `cmd/agent/cli_doccheck.go` — the third section, in **both** arms (report and
  `--fix`), plus the clean line's qualifier.

```
doccheck: stored-cell drift (the manifest's stored text vs what tools/docclass/classify.py renders today):
  doccheck: stored-cell drift: 858 of 2497 rendered row(s) whose stored text is not what the classifier renders today
  doccheck: stored-cell drift: devdoc/DOC-INDEX.tsv (manifest line 2) reason: stored "…" rendered "…"
  … (bounded to 3 rows)
doccheck: re-derive a drifted row with its OWN generator (never hand-edit a cell):
  python3 tools/docclass/classify.py <repo> --only <path>
```

## The measurement that changed the design, and it is the reason to read this file

**The first version of this leg reported `0 of 2497` on a tree with 858.** The
channel it used was `--dry-run --only-file LIST` — the scope-file flag — on the
reasoning that a path list on stdin is the cheapest way to render the whole
corpus. It is not: classify.py enters its **whole-corpus** branch whenever no
`--only` was given, and that branch **echoes the stored rows byte for byte**
(`would write 2497 row(s): 0 new, 2497 carried`, on the generator's own stderr).
The leg was comparing the manifest with itself and calling the result a verdict —
the purest possible form of the defect it was written to catch.

The fix is one flag: **`--only PATH`, repeated, in ONE process**. That is the
only channel that re-renders a path which already has a row. The proof is in
`TestDocclassDriftRenderArgs_CarriesTheWriteFreeAndRenderingFlags`, which asserts
the scope-file flag is **absent** from the argv — that assertion is a measured
defect written down, not a style preference.

Two consequences worth keeping:

- **Cost of a corpus-wide question.** One process renders all 2498 paths in
  ~30–60 s (the corpus index build dominates; a single `--only` invocation costs
  the same index build, so 2498 separate calls would be ~40 hours). A per-path
  loop is not an option, and that is what makes one-argv-for-everything the
  design rather than an optimisation.
- **Two streams, not one.** `--dry-run` writes its summary to **stderr**
  (`would write N row(s): …`). A reader that folded stderr into stdout tried to
  parse that sentence as a manifest row and refused the whole tree. stdout is the
  rows; stderr is the tool's own words, echoed only in a refusal.

## The abstention, and why it is a FAILURE

`NOT MEASURED` is a distinct class with distinct repair advice, and it exits 1:

```
doccheck: stored-cell drift: NOT MEASURED — the corpus's own classifier did not run: …
doccheck: this run exits 1 on stored-cell drift NOT MEASURED (…), independently of the 0 gap(s)
```

Reasons it can happen: no `python3`, no `tools/docclass/classify.py` in the tree
(`--repo PATH` pointing at a tree without the corpus), a classifier that exits
non-zero, output that is not the manifest's row shape, or a **partial** render
(fewer rows than the corpus has paths — a narrowed comparison that would silently
shrink the count).

It is a failure and not a pass for one reason: the operator asked a question. In a
tree where nobody can answer it, printing the clean line would certify a corpus
nobody compared — the silent pass Rule 15 forbids, and exactly the shape this
whole task exists to remove. The class is the **abstention's own**, so the corpus
is never accused of drifting when nothing was compared.

## The manifest's own row is excluded, and that is a decision

`devdoc/DOC-INDEX.tsv` has TWO renderers by design. The write path normalises its
own row to `internal/docindex`'s definition (`selfReason` — the row is a fact about
the index, not a classification decision), while the generator, left to itself,
renders its own older wording. Both spellings are in the tree the moment a fixture
(or the real corpus) is built by the write path.

Comparing that row would therefore report ONE permanent row of drift that **no
re-derivation can ever clear**: re-deriving it rewrites exactly the spelling the
write path exists to normalise. A check whose population contains a row that cannot
be fixed is a check nobody can turn green, and the three existing doccheck fixtures
said so by failing (`TestDocCheckCleanCorpusKeepsBothArmsAtZero`,
`TestDocCheckReportIsReadOnlyAndAgreesWithTheCorpus`,
`TestDocCheckFixWritesTheGapAndOnlyTheGap` — all three red until this exclusion
landed). The row is still compared by the COVERAGE and ANCHOR legs like every other
one; only this question steps aside, and only for this path.

## The defect this leg reproduced inside itself, and where its proof had to live

After the leg was implemented and its predicate tests were green, the verb on the
real tree printed:

```
  doccheck: stored-cell drift: 858 of 2498 rendered row(s) …
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
exit=0
```

The ARM had printed a MEASURED drift count and then returned the clean value — the
exact silent pass the leg was written to remove. Every `internal/docindex` proof
passed while this was true, and that is the transferable lesson: **a predicate test
cannot catch a defect in the arm that consumes it**, because the arm's verdict (the
exit code and the lines it prints) is the only thing an operator ever sees.

The fix is `return docCheckStoredDriftVerdictClassDrifted` with its own exit-1 class
(distinct from the abstention's: "858 rows disagree" and "nothing was compared" are
different facts about different trees). The proof lives in `cmd/agent`, drives the
REAL arm function, and stubs the measurement through one seam
(`docCheckStoredDriftReport = docindex.DocclassDrift`, asserted by a test to still be
that binding):

```
$ go test ./cmd/agent/ -run TestDoccheckStoredDriftArm_FailsOnMeasuredDrift   # with the defect restored
--- FAIL: TestDoccheckStoredDriftArm_FailsOnMeasuredDrift
    a MEASURED drift exited 0 — the arm printed the count and then returned the clean value …
$ # restore (sha256 identical), then:
ok  	github.com/nick/gopi/cmd/agent
```

**A test file named `*_arm_test.go` is IGNORED by the Go toolchain** — `arm` is a
GOARCH, so the name is read as a platform constraint and `go test` reported
`[no tests to run]` while `go vet` stayed silent. The proofs were moved to
`doccheck_stored_drift_verdict_test.go`. (`go list -f '{{.IgnoredGoFiles}}'` is how
it was found.)

## The remainder that read as a corpus count (2026-10-14)

The leg above was implemented to make drift VISIBLE, and it did — but it left one
line under-specified, and a later reader paid for it. Task `01a139c0` was filed with
this finding: the Go face reports **696** stored-cell drifted rows while the Python
face reports **698** for the same tree at the same commit (`f36a8174b`). Two faces,
one question, two populations — and the briefing that filed it said, honestly, that
it could not explain the 2.

There is no second population. The verb's own output, on that tree, reads:

```
  doccheck: stored-cell drift: 698 of 2521 rendered row(s) whose stored text is not what the classifier renders today
doccheck: stored-cell drift: devdoc/FEATURE-VERIFY-MATRIX.md (manifest line 13) class: …
… two more example rows, three fields each …
doccheck: stored-cell drift: ... and 695 more drifted row(s)
```

`698 of 2521` is the FIRST number on screen, and it is the corpus count —
`len(rep.Drift)`, the same set `classify.py . --check` enumerates (698, measured on
that tree, all 698 rendered rows byte-identical to the Python face's set). The number
a reader carried away as "the Go face's count" was `695`: the remainder of the **echo
budget** (`docCheckStoredDriftExamples = 3`), which says how many rows were *not
printed*, not how many drifted. `695` and `698` answer different questions, and
nothing on the screen said which was which — so a reader did the only arithmetic the
screen offered, and arrived at 696.

**Two hypotheses in the filing, both now measured and both false**, and they are
worth recording because either one would have sent the next run somewhere useless:
the Go leg's enumeration (`docclass.Walk`) and the generator's (`all_files()`) do NOT
differ — both return **2521** identical paths on that tree, zero set difference in
either direction; and the Go comparator does NOT skip Cell-normalised rows that Python
would report — rendering all 698 Python-stale paths and comparing cell by cell gives
**0** rows that Go accepts and Python rejects. The populations were equal all along.

**The fix is a sentence, not a number** (Rule 15): the closer now reads

```
doccheck: stored-cell drift: ... and 695 more drifted row(s) NOT echoed — 698 counted, 3 echoed above
```

so the screen carries the corpus count, the echo budget and their arithmetic, and a
reader can reconcile this face with the Python face without reading Go or Python. The
help text says the same thing, because the count line is the corpus count.

The proof is `TestDoccheckStoredDriftArm_TheRemainderReconcilesWithTheCount`
(`cmd/agent/doccheck_stored_drift_verdict_test.go`): it drives the REAL arm through
the same stub seam the other proofs use, counts the echoed paths **off the screen**
(never from the arm's own arithmetic), and asserts `counted == echoed + remainder`.
It carries its own red proof — the replaced wording is scanned with the new format and
the scan must FAIL, so the test cannot pass by measuring nothing:

```
$ go test ./cmd/agent/ -run TestDoccheckStoredDriftArm_TheRemainderReconcilesWithTheCount   # old wording restored
--- FAIL: TestDoccheckStoredDriftArm_TheRemainderReconcilesWithTheCount
    the closer does not carry the arithmetic in a readable shape (too many operands):
        doccheck: stored-cell drift: ... and 2 more drifted row(s)
```

## Deliberately NOT done (the 2026-10-14 repair)

- **No new flag.** The filing noted that no flag prints the drifted paths, and that a
  count a reader cannot expand is hard to reconcile. That is a fair observation and it
  is NOT the defect that was filed: the count line always carried the population, and
  the repair above makes the two numbers on screen reconcile without a listing. A
  `--list-drift` flag is a separate proposal with its own question (what bounds its
  output on a 698-row corpus) and belongs to a task that asks for it.
- **No change to `internal/docindex`.** The comparator and the coverage/enumeration
  legs were measured correct on the reported tree; the fix is entirely in what the
  verb PRINTS. Changing them would have been a fix to code that was not wrong.

## Deliberately NOT done

- **No repair of the 858.** `DOC-INDEX.tsv` is DATA and its single writer is the
  generator (Rule 36). Re-deriving the whole corpus is a reviewable census with
  its own task; the adjacent one (`01a136a8-8f88`) flipped 199 class rows, which
  is the size of decision this leg exists to make *visible* rather than to make
  *silently*.
- **No new flag.** The verb's existing `--repo` / `--check` / `--fix` shape is
  enough; the leg runs in both arms, because a `--fix` run that stayed silent
  about an unmeasured question would be claiming a consistency it never measured.
- **No re-implementation of the classifier in Go.** That would be the second
  source of truth the corpus spent several tasks removing.

## Anchors

- `internal/docindex/docclass_drift.go`, `internal/docindex/docclass_drift_test.go`,
  `cmd/agent/cli_doccheck.go`.
- Invocation, verbatim: `./bin/gopi tool doccheck --repo .` (exit 1 on this tree,
  because the anchor NUMBER leg has one unpermitted drift).
