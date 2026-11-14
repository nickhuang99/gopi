<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# devdoc: the convergence notice must NAME the class that survived, not just the path

**Status:** standing — the residual defect measured at `2c56271`, the arm that
pins it, and the decision the briefing asked for. Task
`01a147b6-f73d-7484-be9d-4955cf106d20`, branch
`task/01a147b6f73d7484be9d4955cf106d20`, cut at `2c56271fb`.

## First: the briefing's premise is 44 commits stale, and the re-measurement says SO

The briefing reports `tools/docclass/classify.py`'s global duplicate guard raising
`SystemExit` for EVERY invocation, so the repair the tooling prescribed could not
run. It names base `942b3ac6`. Measured at THIS tree's revision (`2c56271fb`):

- `git log --oneline -1 -- tools/docclass/classify.py` → `2ba5bda23 fix(docclass):
  a duplicated manifest row is CONVERGED by the writer, never a veto`. The
  convergence is **already on this line**, and `devdoc/docclass-duplicate-row-
  convergence.md` already records the decision.
- The pre-fix writer is reachable for the red proof and nothing else:
  `git show 942b3ac6:tools/docclass/classify.py` → line 1385
  `raise SystemExit("classify.py: refusing to write — duplicate row for %s …")`.

So this run is a **re-measurement plus one narrow residual**, not a repair. The
briefing's own freshness clause is what produced that order, and it held: the
defect it describes is gone, but the sentence the repair prints is not yet the one
a reader needs.

## What is behaviourally deliberate TODAY (read from the code, then run)

One seam decides: `splice_rows` is the ONE function that turns the manifest's rows
into what gets written, so BOTH writer modes inherit the behaviour and neither can
re-acquire the defect. It calls `duplicate_convergence(old_rows, old_order)` first,
which keeps the **LAST** row the file carries and drops the earlier copies — the
same rule `internal/docindex.dropDuplicates` states ("the row appended by hand is
the repair of the row that was already there"), so the two faces of one corpus
cannot answer differently (Rule 14).

Measured on a **throwaway detached worktree** carrying a real duplicate — an
uncommitted copy of `devdoc/merge-compile-only-policy.md`'s own row appended with a
different class, i.e. the shape a merge union leaves behind (its two standing rows
are `historical`, the appended copy `normative`):

| case | invocation | result |
|---|---|---|
| duplicate **OUTSIDE** scope, real write | `--only devdoc/voice-input-default-local.md` | **exit 0**; `{"updated": 1, "converged": 1, "rows": 2634}`; stderr names the path and its row count |
| duplicate **INSIDE** scope, real write | `--only devdoc/merge-compile-only-policy.md` | **exit 0**; converged to **one** row, survivor class `historical`; summary `{"converged": 1}` |
| **`--dry-run`** on the duplicated path | `--only … --dry-run` | **exit 0**, prints the row it would write, and the manifest is **BYTE-IDENTICAL** after (`cmp`) — the dry run is the ORACLE the repair names, so it must not refuse |
| `--check` on a duplicated manifest | `--check` | report-only: names the path as a `duplicate-row` GAP and writes nothing (the check MEASURES, the writer REPAIRS) |

The pre-fix writer on the same fixture fails **six** of the seven arms of
`TestADuplicateRowIsConvergedNotVetoed`, each with
`refusing to write — duplicate row for devdoc/keep.md`; the tree writer passes all
seven (`Ran 52 tests … OK`). Both directions are quoted in the task report.

**The decision the briefing asked for is therefore already taken, and this run
endorses it:** the refusal is NOT kept as a global abort. A duplicated path is a
**fact the check reports** and a **repair the writer performs**, announced on
stderr — never a veto over rows the run was not asked about. The scope refusal that
does remain (`add-missing`'s ambient form, exit 2, "the scope does not cover the
manifest's whole state") is about **attribution**, a different question, and this
run deliberately did not widen it.

## The residual defect — the notice names the path, never the surviving class

`DUPLICATE_CONVERGED_NOTICE` says which paths were converged and how many rows each
carried. It does **not** say **which row survived**. The two copies in the real
shape carry DIFFERENT classes (`historical` vs `normative`), so the convergence is a
classification decision made on the operator's behalf — and the only account of it
is a count. Measured: the run above prints

```console
classify.py: converged 1 duplicated path(s) in devdoc/DOC-INDEX.tsv — kept the LAST row for
each … dropped the earlier copy/copies: devdoc/merge-compile-only-policy.md (2 rows)
```

and then, one line later, the survivor is only discoverable by
`grep -P '^<path>\t' devdoc/DOC-INDEX.tsv`. **Rule 15's subject is the account, not
the act**: a repair that silently picks between two classifications is the defect
the guard used to avoid by refusing — the fix moved the decision into the writer
without moving the sentence that reports it.

The class is not invented at the notice: the survivor IS `old_rows[path]`, whose
second cell is the class, and `duplicate_convergence` already returns `kept`. So the
repair is a one-line widening of the sentence at its ONE definition (Rule 13/14),
plus the arm that pins it.

## Clearing a duplicate — the command a reader runs

```bash
# 1. Does the manifest carry one? (report-only, writes NOTHING; ~2 min → run it as a job)
python3 tools/docclass/classify.py . --check            # a `duplicate-row` gap names the path

# 2. Clear it with the WRITER — never by editing the TSV (Rule 36: it is generated data)
python3 tools/docclass/classify.py . --only <the duplicated path>

# 3. The file half, if the surviving class is a bannered one (historical)
python3 tools/docclass/banner.py . --only <the same path>
```

Step 2 prints `converged N duplicated path(s) …` and (after this change) the class
each one kept; step 3 is the OTHER half of one classification, and skipping it is
how a convergence lands green on the row side and red on the file side.

## A second, independent finding — the write hook can land half a classification

`internal/docindex`'s `EnsureDoc` is the OTHER writer of this corpus and it does
write the banner (`case ok:` prepends it, `docindex.go:496`). But the
`tools/docclass/classify.py` path — the one `internal/docindex`'s own
`BannerRepairCommand` names — writes the ROW only; the banner is a second
invocation. At this commit the corpus is red on exactly that shape:
`devdoc/finish-cleanup-bounded-lock-wait.md` carries its row (`historical`, written
by `7eb334c31`) and **no banner**, so
`TestDocClassBannerMatchesManifest` fails on it — a pre-existing red at `2c56271`,
not caused by this run (measured at the cut with an otherwise clean worktree). The
file was repaired here with the prescribed `banner.py --only` (`+1 −0`), which is
the half the row writer leaves to the operator.

## Check

```bash
python3 tools/docclass/classify_test.py                                           # 52 arms
go test ./internal/docindex/ -run 'DocClass|Manifest|Duplicate' -count=1
go test ./internal/standards/ -run 'DocManifestCoversEveryDevdocFile|DocClassBannerMatchesManifest'
python3 tools/docclass/classify.py . --check                                      # run as a job
```
