<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The Python suite is on the gate path now

**Measured 2026-10-15**, tree `6e84747` (baseline this task was cut at).

## The defect

`tools/docclass/classify_test.py` is a real unittest suite — 42 tests at the
baseline, 45 after the delivery-point sync — that guarded the corpus generators,
and **no suite entry point ran it**. Measured this run:

```
grep -rn "classify_test" --include=*.go --include=*.sh --include=Makefile .   # no runner
python3 tools/docclass/classify_test.py   →  Ran 42 tests in 6.006s … OK      # it works
```

The nearest Go comment (`internal/docindex/docclass_fixpoint_test.go`) RECORDED
the gap and worked around it with its own Go fixture instead of fixing it. The
cost is concrete: `classify.py`'s citation predicate (`refused_mention`) decides
whether a line mentioning a document CITES it or merely quotes/transcribes it,
its arms are per-arm tested ONLY in the Python file, and a regression in one arm
therefore reached `master` unguarded.

## The repair, and why it is shaped this way

ONE Go test — `internal/docindex/docclass_python_suite_test.go`,
`TestTheDocclassPythonSuiteRunsOnEveryGate` — runs the interpreter over the
suite from the repo root and fails the Go suite on a non-zero exit.

- **The existing entry point, not a new runner (Rule 35).** `make test` /
  `make test-budget PKGS='./internal/docindex/'` → `scripts/test-budget.sh` → `go
  test` already exists; the new test inherits the declared per-package budget and
  the branch-scoped smoke path. A shell runner beside it would be a SECOND entry
  point to the same suite with its own defaults for scope, budget and
  environment — the drift Rule 14 exists to prevent. No script, no daemon, no
  state file, no new dependency: `python3` is already required by this package's
  own generator-driven cases (`corpus_faces_agree_test.go`,
  `docclass_fixpoint_test.go`) and by `internal/standards`' blank-line gate, and
  the suite is stdlib-only (`unittest`; no pytest).
- **Which package owns it.** `internal/docindex` is where the docclass face
  lives: it already invokes `tools/docclass/classify.py` (the same
  `GeneratorDirRel` const composes this path) and it already owns the Go-side
  reader of the manifest. `internal/standards` was the alternative and was
  rejected only on locality — its gates read the corpus, this one drives the
  generators.
- **LOUD, never silent (Rule 27, Rule 15).** Missing `classify_test.py` → fail
  naming the path; no `python3` on PATH → fail naming the interpreter and the
  sibling cases that also need it; a run reporting fewer tests than the floor →
  fail. Every arm is `t.Fatalf`; there is no `t.Skip` anywhere in the file. The
  floor (`docclassSuiteMinTests`) is an explicit UPDATE SITE: a run that adds
  arms raises it in the same commit, so a shrunken or partially skipped run
  cannot read as green.
- **The child's temp workspace is pinned off `/` (Rule 22).** The fixture hands
  the child `TMPDIR=<a fresh scratch dir>` (and `GOPI_TEST_SCRATCH_HOME`) and
  MEASURES the directory's device id against `/`'s before running — the rule is
  device identity, not the path's spelling. The suite's own scratch does not even
  consult `TMPDIR` (it takes its directories from
  `scripts/lib/tmp_ban.make_scratch_dir`, which asserts the device id itself);
  the pin covers the grandchildren that are not written that way.

## The red proof (Rule 31)

Arm 5 (the quoted bare path cell) was disabled in `classify.py`, in both forms it
has had on the two trees this run touched:

```python
# before (tree 6e84747 and master 71980381c, spelled the same)
QUOTED_CELL_RE = re.compile(r'^\s*"([^"]*\.(?:md|tsv))"\s*[,;]?\s*$')
# mutation
QUOTED_CELL_RE = re.compile(r'(?!)')   # RED PROOF: arm5 disabled
```

`make test-budget PKGS='-run TestTheDocclassPythonSuiteRunsOnEveryGate ./internal/docindex/'`:

- **red** — `make` exit 2; the run's own stream carries
  `FAIL: test_a_quoted_bare_path_cell_is_not_a_citation (__main__.TestCitationsAreStatementsNotQuotations)`
  with `AssertionError: 'normative' != 'historical'` and
  `Ran 45 tests … FAILED (failures=4)`, and the Go side reports
  `--- FAIL: TestTheDocclassPythonSuiteRunsOnEveryGate`.
- **restore** — `git checkout -- tools/docclass/classify.py`, then content
  identity against an EXPLICIT REVISION (never the index):
  `git hash-object tools/docclass/classify.py` =
  `git rev-parse HEAD:tools/docclass/classify.py` (`same=YES`), and
  `git diff HEAD -- …` prints nothing.
- **green** — the same invocation with the restored file passes: `make` exit 0,
  `outcome=pass`.

A restore seeded from the index would have written back the very bytes the index
holds; that is why the comparison is against `HEAD:<path>` (see
`devdoc/redproof-restore-criterion.md`).

## What a reader should NOT conclude

The Go test does not re-implement any Python assertion — it delegates the whole
verdict to the suite, exactly as `corpus_faces_agree_test.go` delegates the
corpus verdict to `classify.py --check`. There is ONE policy in ONE place; this
file is only the route that puts the suite on the gate path.
