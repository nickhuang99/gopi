<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# The clean path of `gopi tool doccheck` now prints its own `0 gap(s)` line

**Task**: `01a1416f-c182-7857-b1bb-5e82846c58b3` (epic item `[3/3]`).
**Change**: `cmd/agent/cli_doccheck.go`, `docCheckReport`'s clean branch.

## The defect, measured

Two faces of ONE question — is the devdoc corpus consistent? — rendered the
answer differently on a clean tree:

```
$ python3 tools/docclass/classify.py . --check        # the repo's own policy source
0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv

$ gopi tool doccheck                                  # the Go verb (before this change)
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

`docCheckReport` returned at the clean branch **before** reaching
`fmt.Printf(docCheckSummaryFormat …)`, so on the GREEN path the counter a script
greps for existed in `classify.py`'s output (`GAP_SUMMARY`, printed
unconditionally at `tools/docclass/classify.py:1808`) and in no line of the
verb. A reader who gated on `0 gap(s)` got a value from one face and silence
from the other; Rule 14 forbids exactly that — one policy, and a counter whose
spelling (or presence) each face chooses for itself is two policies.

## The remedy, and why this arm of the choice

Two arms were on the table: print the zero count on the clean path, or file the
divergence as a bug row. The count line is the smaller and the *convergent*
change — the verb is a thin face over the corpus's own reader, and the Python
face already prints this line on every run, green or red. Making the Go face
agree removes a difference rather than recording one.

The clean sentence is **kept** (Rule 15): "the check looked and found nothing"
must stay an explicit VALUE, not something a reader infers from an empty screen.
The two lines now stand together:

```
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

The format is the **same named constant** (`docCheckSummaryFormat`) the failing
path already used — one spelling for one counter (Rule 1: no second literal;
Rule 14: no second policy). The order — counter first, clean sentence second —
matches "the count the exit status encodes" being the skimmable line the
constant's own comment already described, with the sentence closing the run.

## What did not change

- The exit status, on every path (green stays `DocclassExitClean`).
- The failing path's output, byte for byte — it printed the summary line before
  this change and still does.
- The clean sentence's wording.

## Regression guard

`cmd/agent/doccheck_clean_count_line_test.go` asserts, on a fixture corpus built
by the repo's own generators (so `0 gaps` is a measurement, not a stub):

1. the clean run prints `fmt.Sprintf(docCheckSummaryFormat, 0, …)` — spelled
   through the verb's own constant, so the test cannot pass while the verb and
   its constant drift;
2. the clean sentence is still printed (Rule 15's VALUE is not replaced);
3. on a corpus with a real gap, the failing path prints its own count through
   the SAME constant and never the clean sentence — one spelling, two paths.
