<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# `--only-file` selects a WRITER, not just a scope — one scope, one predicate, one writer

## The defect: the writer was selected by ONE of the scope's two spellings

`tools/docclass/classify.py` accepts a scope in two spellings — `--only PATH`
(repeatable) and `--only-file FILE` (a path list, `-` for stdin). Both are
documented as the same thing, and the write hook's own shape is the file form.

The writer, however, was selected by:

```python
if not (add_missing or only):        # `only` is the flags list ONLY
        ... WHOLE-CORPUS WRITER ...
```

`--only-file` contributed to `explicit` (the scope) and to nothing else, so a
caller who named its scope in a FILE fell through to the whole-corpus writer —
which prints every existing row **byte-for-byte**. The measured result, on the
real corpus:

```
$ python3 tools/docclass/classify.py . --only-file <821 paths>   # pre-fix
  "updated": 0                                    (a census, not a write)
  git status --porcelain devdoc/DOC-INDEX.tsv     -> nothing (byte-identical)
```

Exit 0, a summary that looks like a completed run, and **not one word on stderr**
to say the scope had been ignored. That is the whole cost of this class of bug: a
no-op wearing the shape of a passing check (Rule 15/41), where the caller's
evidence for "the row is now correct" is a command that by construction cannot
change it.

## The mechanism, once named

Three facts are one fact:

1. **The writer is chosen by ONE predicate over the scope** — which is why two
   spellings of the scope must be ONE value before the selection, not two
   predicates that have to be kept in step by hand (Rule 13/14).
2. **A scope file whose paths are ALL outside the corpus is not a scope.** The
   pre-fix code silently dropped every non-corpus path, so a list of typos, a
   list of paths from another tree, or an empty list produced the same
   silent no-op as a legitimate run.
3. **The whole-corpus `--dry-run` is the WRITE, not a re-derivation.** Because a
   carried row is printed as STORED, that output *cannot* contain the drift the
   classifier would render — and a caller who reads it as a preview concludes
   "nothing changes". Measured: this output was read as a 0-impact measurement on
   a corpus whose own `--check` named **786** DOCCLASS STALE rows.

## The fix

- `if not (add_missing or only or scope_files)` — the scope-file spelling now
  reaches the SAME incremental writer, and the file's paths join `explicit`
  before any writer is chosen. One scope, one predicate, one writer.
- A scope file that yields **no path at all**, and one whose paths are **all
  outside the corpus**, are each refused BY NAME (`exit 2`, nothing written).
- A path named but outside the corpus is **recorded on stderr** and left alone —
  the same shape `--only PATH` has always had.
- The whole-corpus `--dry-run` now names, on stderr, how many of the rows it
  carries are ones the classifier would render differently, with `--check` as the
  pointer — measured by `stale_rows`, the SAME function `--check` reports with,
  so the two faces cannot disagree about what drifted. Its USAGE paragraph says
  the same thing, because that is where a caller reads a flag's meaning.

## What it cost to see, and how it is pinned

`scripts/test_docclass_scope.py` carries the regression: scope-file ⇄ flag
**byte parity**, the drift re-derived through the scope file, both refusals, and
the dry-run's announcement. Red→green is a property of the extraction, not of a
claim:

```
$ git show 8a2c5199f:tools/docclass/classify.py > <session>/artifacts/pre-fix-classify.py
$ DOCCLASS_CLASSIFY=<…>/pre-fix-classify.py python3 scripts/test_docclass_scope.py
  FAILED 22/77     # the pre-fix script — every scope-file check above is red
$ python3 scripts/test_docclass_scope.py
  PASS 77/77       # the fixed one
```

**The trap this file exists to record:** the byte-parity arm is the one that
would have caught it years earlier and costs one `cmp` — two spellings of one
scope must be *diffed against each other*, not each tested against a description
of itself. A test that asserts "the row was re-derived" on the flag spelling and
"exit 0" on the file spelling would have been green through the whole defect.

**Second trap, for the reader of `--dry-run`:** under a scope it prints the rows
the run would RE-DERIVE (what it sounds like); as the whole-corpus write it prints
the rows a writing run would put ON DISK. Same flag, two meanings, and the only
thing that distinguishes them is the notice this fix added.
