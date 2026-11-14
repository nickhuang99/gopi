<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The whole-corpus writer preserves the rows it was not asked about, and the drift it hides is named

**Task** `01a13204-a6a0-7444-b615-dac0c03545f3` · **base** `120ddaf` · surface:
`tools/docclass/classify.py` (the corpus's own truth-class generator).

## What was measured

| command | result |
|---|---|
| `python3 tools/docclass/classify.py . --check` | `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, exit 0 |
| `python3 tools/docclass/classify.py .` | **`721 721 devdoc/DOC-INDEX.tsv`** — 721 of 2429 lines rewritten |
| `gopi tool doccheck`, `python3 tools/docclass/banner.py . --check` | byte-identical output before and after that rewrite |

All 721 rows were PAIRS (same 721 paths removed and re-added) — nothing was added
or dropped, so the change was a pure re-rendering. What moves is everything a row
derives from the LIVE TREE:

* a rendered citation line number — `sym internal/tools/tasks.go migrateTasksFTS at
  internal/tools/tasks.go:1075` → `:1321`, `sym internal/gopilog/stats.go
  BuildStatsFromJSONL at …:199` → `:234`;
* a live count in the reason — `standing N / record M`;
* the citation site — `cited by tasks/01a0b8ca-report.md:195`;
* ` stale=N` in the evidence cell.

Of the 721 paired rows, **201 changed CLASS as well** (e.g.
`devdoc/chat-voice-slash-command.md`: normative → historical). That is the
distinction the fix is built on: a re-classification is a DECISION with a review
behind it (201 rows), while the remaining 520 are pure live-tail churn — and
neither is something a caller who merely ran the generator asked for.

Why it was invisible: every consistency face — `--check`, the Go gates, `gopi
doccheck`, `banner.py --check` — asks about the manifest's SHAPE (coverage,
class vocabulary, reason floor, spot-check presence). None of them asks whether a
row's stored text is still what the classifier renders today, and the only
consumer that parses the evidence cells (`internal/standards/doc_evidence_gate.go`)
holds its findings out and explicitly refuses to block a landing.

The cost that made this an epic: in a shared worktree the rewrite lands rows for
paths nobody in the room touched, so it rebases other agents' work — the failure
shape Rule 29 and the incremental-mode history (`03d7447e6` "the incremental mode
is a function of the caller's paths, not of the tree") already name.

## The fix, in two halves

**1. The whole-corpus writer carries, it does not re-derive.** A row already in the
manifest is written back BYTE-FOR-BYTE, its live-derived evidence cell included; a
path with NO row is still classified and inserted at the corpus sort position; a
row whose file is GONE from the tree is DROPPED, and the drop is named on stderr
(every gate calls such a row a gap, so carrying it would preserve a defect; the
drop is a repair, not churn, and 0 rows needed it on this tree). Rule 36's coverage
is a property of the SPLICE, never of how much was re-derived — the same reading
`03d7447e6` reached for the incremental arm, extended to the whole-corpus one.

    $ git checkout -- devdoc/DOC-INDEX.tsv
    $ python3 tools/docclass/classify.py .
    { "normative": 934, "historical": 1473, "index-meta": 1, "roadmap": 12 }
    total 2420 new 0 carried 2420 dropped 0
    $ git diff --numstat devdoc/DOC-INDEX.tsv
    (nothing)

Discovering drift is not hiding it: the summary prints `new` / `carried` /
`dropped`, so a caller can see that 2420 rows were carried rather than re-derived.

**2. `--check` names the drift without failing on it.** A SECOND question on the
same walk, after the gap question: which rows is the classifier rendering
differently today. Each is named under `DOCCLASS STALE`, the count is printed, and
the EXIT STATUS IS UNCHANGED (the gap count's alone):

    $ python3 tools/docclass/classify.py . --check | tail -3
    DOCCLASS STALE devdoc/ws5-stop-red-was-a-fixture-path-bug.md
    DOCCLASS STALE devdoc/zombie-grace-disposal-2026-09-03.md
    721 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal: re-derive ONE with `--only <path>`)
    $ echo $?
    0

Non-fatal is a decision, not timidity: 721 rows is the ordinary state of a manifest
that is not regenerated on every commit, and a check that went red on it would be a
red nobody reads — the same reason `docEvidenceHeldOut` exists rather than a plain
failure. The zero case prints a sentence of its own (`0 row(s) whose stored text is
not what the classifier renders today`) so "measured, found nothing" is
distinguishable from "the measurement never ran" (Rule 15). A path with NO row is
not reported as stale — that is the check's own `file-with-no-row` gap class, and
one fact must not get two verdicts.

## What was deliberately NOT done

* **The 721-row re-derivation is not landed here.** Clearing drift is `--only PATH`
  (per path) or a reviewed census; a generator run is not a review. `git diff HEAD --
  devdoc/DOC-INDEX.tsv` on this branch is empty.
* **`--add-missing` / `--only` behaviour is untouched** — the incremental arm already
  carried foreign rows, and this change does not widen or narrow its scope.
* **The `sym` tier's shape rule is untouched.** `strong_symbol` requires the symbol
  to end in an uppercase run (`migrateTasksFTS`), which is why the unit test's
  fixture names `ProbeAnchorID` and not `ProbeAnchor`: the first name falls through
  to a lower tier and the test would measure nothing. Worth knowing before writing
  the next fixture — it cost one debugging round here.

## Verification pointers

* `python3 tools/docclass/classify_test.py` → `Ran 28 tests … OK` (7 new).
* Red→green, both arms (the recipes live in the artifacts directory named in the
  task report): disabling the carry in the writer reddens
  `test_an_existing_row_is_carried_byte_for_byte_across_an_unrelated_commit` +
  `test_a_new_document_still_gets_a_row_beside_the_carried_ones`; emptying the
  `--check` drift list reddens
  `test_the_drift_this_preservation_hides_is_named_by_check`. The production file
  was restored from a snapshot and the restore verified by blob hash.
* `TestCommandsDocGateRedProofStaleGeneratedBlock` (`internal/standards`) is red on
  this tree and is NOT this change: it fails identically at HEAD with both of this
  branch's files reverted, and four completed ledger rows already carry it
  (`01a12fa9`, `01a12ff8`, `01a13127`, `01a131af`).
