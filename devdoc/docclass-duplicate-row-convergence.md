<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# devdoc: a duplicated manifest row is CONVERGED by the writer, never a veto over every write

**Status:** standing — the rule this document states, the gate that pins it and the
measurement that produced it. Task `01a14858-e238-7672-9509-233b27b79dea`, branch
`task/01a14858e23876729509233b27b79dea`, cut at `4961ca7`.

## The defect, measured

`devdoc/DOC-INDEX.tsv` carried **three** rows for one path — `1290 normative`,
`2630 normative` (byte-identical to 1290) and `2631 historical` — the residue of a
merge that unioned two writers. `tools/docclass/classify.py`'s `splice_rows`
validated the WHOLE manifest before writing: **any** duplicated path raised
`SystemExit` and nothing was written. So one historical duplicate refused **every**
write in the corpus:

```console
$ python3 <pre-fix copy, git show 4961ca7:tools/docclass/classify.py> . \
        --only devdoc/merge-compile-only-policy.md
classify.py: refusing to write — duplicate row for devdoc/merge-compile-only-policy.md
      (a manifest with a duplicated path fails the coverage gate)
exit=1                      # and zero bytes written
```

That is not a corner: `--only PATH` is the ONLY incremental writer Rule 36
prescribes, and every NEW devdoc's first row is spliced into the same file. One
duplicated path therefore forced every devdoc-adding run into whole-corpus mode,
which re-classifies other writers' rows as a side effect. Meanwhile `--check`
reported the same bytes as a `duplicate-row` gap — the question and the repair
disagreed about one tree.

## The fix — one converged reading, at the one writer seam

- **`duplicate_convergence(old_rows, old_order)`** — the ONE definition of which
  row survives (Rule 13/14). It keeps the **LAST** row the file carries for a
  duplicated path and drops the earlier copies, returning `(kept, dropped, counts)`.
- **`splice_rows` calls it first**, before merging. It is the one function that
  turns the file's rows into what gets written, so BOTH writer modes (whole-corpus
  and incremental) inherit the convergence and neither can acquire the defect.
- **`existing_rows` returns ONE ENTRY PER OCCURRENCE** in `order` (it used to
  collapse to distinct paths, which made a duplicate unmeasurable and let a
  re-sorted write rewrite copies nobody asked about). `rows` stays path-keyed, and
  the last copy's bytes win there too — the same rule, so the two readings cannot
  disagree.
- **The old guard is not deleted, it is scoped to what the call PRODUCES**: the
  emitted rows carry each path at most once, and a violation is now an *internal
  error* — the defect would be in the splice, not in the file the run was asked to
  write. The pre-fix guard's mistake was policing rows it merely carried over.
- **Announced, never silent (Rule 15)**: the incremental writer prints
  `DUPLICATE_CONVERGED_NOTICE`, naming each converged path and its row count, and
  its JSON summary carries `"converged": N`. Both are computed from
  `duplicate_convergence()` itself, so the sentence cannot disagree with the bytes.

## Why the LAST row — and it is not this script's opinion

`internal/docindex`'s `dropDuplicates` keeps the last row:

> `// dropDuplicates removes every row for a path but the LAST one, which is the one`
> `// the row appended by hand is the repair of the row that was already there`

Two faces of ONE corpus answer ONE way (Rule 14). The corpus's own case confirms
the rule rather than merely obeying it: `devdoc/merge-compile-only-policy.md`
opens with `<!-- doc-class: historical -->` on **line 1** — its own banner — and
the last of its three rows was the `historical` one. The two `normative` copies
were the stale unioned half (`grep -n` on the manifest: 1290, 2630 normative;
2631 historical).

## Acceptance, as measured this run

| check | result |
|---|---|
| `--only devdoc/merge-compile-only-policy.md` on the duplicated manifest | **exit 0**, `{"updated": 1, "converged": 2, "rows": 2620}`, notice naming the path (3 rows) |
| `git diff --numstat devdoc/DOC-INDEX.tsv` | `0 2` — two deletions, no other row touched |
| survivor | `devdoc/merge-compile-only-policy.md  historical` (the doc's own line-1 banner) |
| `awk -F'\t' '{print $1}' devdoc/DOC-INDEX.tsv \| sort \| uniq -d` | no output |
| idempotency | manifest copied → same `--only` rerun → `cmp` byte-identical |
| `python3 tools/docclass/classify.py . --check` | `0 gap(s)` for `duplicate-row` |
| `classify_test.py` (whole suite) | `Ran 52 tests … OK` |
| red proof | `TestADuplicateRowIsConvergedNotVetoed` (7 arms) against the pre-fix copy via `DOCCLASS_CLASSIFY=` → `FAILED (failures=6)`; tree writer → `OK` |
| `docclassSuiteMinTests` | 45 → **52** in the same commit (the floor ratchets with the arms) |

## Two rows this run had to write for other runs' documents

`--check` after the convergence reported exactly one remaining gap, and it was NOT
the duplicate class: `file-with-no-row devdoc/sigcgt-pty-probe-decode.md`. That doc
is TRACKED at the cut `4961ca7` (`git cat-file -e 4961ca7:devdoc/…` → present,
`git log` → `4a2687a6`) but had no row at that rev either — an inherited gap, the
exact shape that made the old guard's corpus-wide refusal so expensive. It was
cleared with the same writer:

```console
$ python3 tools/docclass/classify.py . --only devdoc/sigcgt-pty-probe-decode.md
{ "added": 1, "updated": 0, "converged": 0, "rows": 2622 }  exit=0
$ python3 tools/docclass/banner.py        # the class it derived needs its banner
banner applied to 1 docs, removed from 0
```

And this document's own row, same way (`{"added": 1, "updated": 0}`, exit 0). No
TSV was hand-edited anywhere in this run (Rule 36).

## The fixture, and why it is shaped that way

`DuplicateRowFixture` builds a two-document corpus, then APPENDS a copy of one
document's row as the file's **last** data line **with a different class**. Both
halves are load-bearing: while the copies are byte-identical, "kept the last" is
unobservable (the rule would be true of either copy); and the second document
exists so the run has something UNRELATED to write — the pre-fix defect was
precisely a duplicate it never touched vetoing that write.

The red proof uses the tested file's own env override of the generator path
(`DOCCLASS_CLASSIFY`), never a copy placed over the tree: A/B on ONE tree, one
variable, no risk of committing the old writer.

## A neighbouring question, deliberately NOT re-decided here

The refusal that remains possible is the SCOPE refusal (`gopi`-shaped: "the scope
does not cover the manifest's whole state"), which is about attribution rather than
duplication. This change narrows the duplicate guard only; it does not widen what
an incremental run is allowed to write.

## Check

```bash
python3 tools/docclass/classify_test.py                                   # 52 tests
go test ./internal/docindex/ -run 'DocClass'                              # the floor, 52
go test ./internal/standards/ -run 'DocManifestCoversEveryDevdocFile|DocClassBannerMatchesManifest'
python3 tools/docclass/classify.py . --check                              # 0 gap(s); ~2m, run it as a job
```
