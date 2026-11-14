<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The doc-index write hook owns its own path — a write may not re-decide a row it did not write

Task `01a0dc29-2e6d-779f-80de-25e07ffe0064`. Follow-up to the bug task filed by
session `01a0dbec` ("the manifest's rows are being injected by a process that
cannot classify them"). The incident, the row evidence and the commit below are
that session's; the attribution and the fix are this one's.

## 1. The observation (session 01a0dbec, 2026-09-26 ~13:17)

Worktree `/home/user/workspace/gopi-01a0dbecd85f7984bc36f2d35d8b6c02`, branch
`task/01a0dbecd85f7984bc36f2d35d8b6c02`. The author created
`devdoc/main-checkout-deposit-repo-door.md`; seconds later
`devdoc/DOC-INDEX.tsv` carried a row for it — class `historical`, the
generator's generic template reason ("the doc's own title/name declares a past
run's record, and no current artifact cites it as authority", `2 standing / 3
record`), inserted at a line (1351) that is NOT the manifest's alphabetical
position for `main-checkout-deposit-…` (1412–1413). `pgrep -af
"docclass|classify"` showed no classifier process. The worktree's `md5sum`
changed between two commands seconds apart:

```
679852743b66cbdb3b14337b2c1a5dd2   (observed with the row present)
8789d454131d13bfeadf378db9c95f06   (observed after the author removed it)
```

The two rows in commit `051a1ab3`
(`feat(tasks): the main-checkout arm gets a second door`) are the whole
evidence, and they are ONE hunk:

```
-devdoc/main-checkout-deposit-arm.md	normative	normative: standing claim about master that names code that resolves (path) — the close-out arm's own file; …	path internal/tools/task_main_checkout_deposit.go
+devdoc/main-checkout-deposit-arm.md	historical	historical (0 standing / 3 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority	
+devdoc/main-checkout-deposit-repo-door.md	historical	historical (2 standing / 3 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority	
```

Row 1413 is the new document. Row 1412 is the tell: a **previously classified
document's row, REPLACED**, its class flipped `normative` → `historical` and its
**evidence cell emptied** in the same write. `051a1ab3` also prepends
`<!-- doc-class: historical -->` to `main-checkout-deposit-arm.md` — a document
whose manifest class is `normative`, which carries no banner. That pair is what
`internal/standards/docclass_gate_test.go` reports as *"carries the
\"historical\" banner but … classes it \"normative\""*.

## 2. The writer: the agent's own write tool, one call away — NOT a hidden process

**The culprit pathway, with `file:line`:**

| # | site |
|---|---|
| ① | `internal/tools/write.go:80` — the `write` tool, after `atomicWriteFile` |
| ② | `internal/tools/edit.go:118` — the `edit` tool, same hook |
| ③ | `internal/docindex/generator.go:120 ClassifyWrittenFile` |
| ④ | `internal/docindex/generator.go:157 → ClassifyWithGenerator` (`:91`) → `classify.py --only <rel>`, then `banner.py --only <rel>` |
| ⑤ | `tools/docclass/classify.py:513` and `:534` — the **incremental mode**: `--only` replaces the row for that path in place, through `splice_rows` (`:426`) |

So an ordinary `write` to ANY path under a `devdoc/` corpus runs the
content-aware classifier on that path, in-process, invisibly to the agent. When
the path **already has a row**, `--only` does not skip it: it **re-derives** the
class from the document's text and rewrites the row, dropping what the row
recorded. Two things then contradict each other in the same file pair:

- the manifest row loses a deliberate classification and its citation;
- `banner.py` prepends the banner for the class the generator just derived,
  while other rows (and the classification someone wrote by hand) say otherwise.

**Reproducing its write** (run in any gopi checkout; it is exactly the pair
`ClassifyWithGenerator` executes):

```bash
python3 tools/docclass/classify.py . --only devdoc/main-checkout-deposit-arm.md
python3 tools/docclass/banner.py   . --only devdoc/main-checkout-deposit-arm.md
# ⇒ {"added": 0, "updated": 1, "rows": 2422}   banner applied to 1 docs
# ⇒ git diff --numstat devdoc/  →  1  1  devdoc/DOC-INDEX.tsv
#                                   1  0  devdoc/main-checkout-deposit-arm.md
```

Measured on this branch's base (2026-09-26): the manifest's claim for
`main-checkout-deposit-arm.md` flips `normative` (with
`path internal/tools/task_main_checkout_deposit.go`) → `historical (0 standing / 3
record)` with an empty evidence cell, and the doc gains the banner. Restored
immediately with `git checkout -- devdoc/`; the manifest was byte-identical
before and after (`d7f5009b14f726cdd39938e6970fa0ea`).

**What was ruled out, and how.** The incident's own signature ruled it out:

- *no hidden process was needed* — the write hook needs no timer, no tick and no
  PID: it runs inside the tool call the author makes;
- `git blame` landing on the **previous** commit (`d01a952d`) is not a rewrite of
  history: the hook edits the file on disk, so the line is uncommitted and blame
  reports the last commit that touched the file — which is why the author saw a
  row they never staged, and why the manifest's md5 moved between two commands;
- the *near-sorted* insert position needs no sorter either: that is what the
  incremental `--only` writer does (it splices at the generator's sort position,
  measured as the `marker-window`/`max-…` misplacement in the incident);
- the class assigned **without reading the file's banner** is exactly
  `--only`'s design: `classify.py` derives the class from the document's
  text/name (a record-y title or name ⇒ `historical`), and **never consults the
  file's existing `<!-- doc-class: -->` banner**;
- the second, "correct" `normative` row the author saw later came from their own
  hand classification — the row they met first was the hook's. A **duplicate
  row** is thus the ordinary outcome of "the hook wrote one, the author wrote
  one", which is why the task could not tell the concurrent write from its own
  edit.

## 3. The fix: ownership, stated as one rule

`ClassifyWrittenFile` now checks **ownership before any generator runs**:

- **A path that already carries a well-formed row is ALREADY CLASSIFIED.** The
  hook does not re-run the classifier on it. Its class is taken from the row it
  carries, and the hook's job shrinks to the half the row DOES own: the row's
  class is **materialised in the file** as its banner (added when the class is
  bannered, removed when it is not), so the two halves of a classified document
  cannot disagree. It writes nothing when the file already says what the row
  says.
- **Creating a row (none present) and repairing an unreadable one (malformed)
  stay the hook's job.** Those are an omission and a readability defect, not a
  classification — and they are exactly what the earlier work in this epic
  (`internal/docindex`, the close-out's branch-scoped check) relies on.
- **The fallback leg (a corpus with no generator) obeys the same rule**, so it
  is one rule and not one leg's behaviour.

**Why this way, and not "refuse to touch a worktree whose HEAD it does not
own".** The manifest is one file shared by every branch, and its row for a path
is the corpus's recorded truth about that path. A writer that re-derives a class
on every write makes the class a function of *which door the document came
through and when* — the second source of truth this epic exists to remove. The
ownership rule is the smaller, checkable statement: **an existing classification
is carried over byte-for-byte; the write path may add what is missing and repair
what is unreadable, and may not re-adjudicate.** The banner follows the row,
never the row the banner.

The direction of that last sentence is load-bearing, and it is also what the
banner gate wants: `internal/standards`' rule is *"fix one, and the manifest is
the source of truth"* — so the row wins and the file is made to agree.

Files: `internal/docindex/generator.go` (the guard, `existingRowClass`,
`materialiseRowClass`, `rowOwnsClassNote`), `internal/docindex/docindex.go`
(`Fixup.RowOwned`).

## 4. Red-first proof (the reproduction is at the owning layer)

`internal/docindex/generator_ownership_test.go`:
`TestClassifyWrittenFileDoesNotReclassifyARowedDocument` ships **this repo's real
`classify.py` + `banner.py`** into the fixture and drives the real
`ClassifyWrittenFile`, then asserts the property: *a write may not re-decide an
existing classification* — the manifest is byte-identical, the document keeps its
title as line 1, and the row keeps its class and its citation.

- **RED, on master's code** (`git restore --source=HEAD` of the two source files,
  reproducer kept): the hook rewrote the fixture's row
  `devdoc/landed-rule.md  normative … path internal/docindex/docindex.go` into
  `devdoc/landed-rule.md  historical  historical (5 standing / 5 record): … `
  with an **empty evidence cell** — the incident's signature, reproduced in a
  fixture. `exit=1`.
- **GREEN with the fix**: the same test passes, and
  `go test ./internal/standards/ ./internal/docclass/ ./internal/docindex/` is
  green (`ok internal/standards 30.6s`, `ok internal/docclass 5.4s`,
  `ok internal/docindex`).

`internal/docindex/generator_ownership_fixup_test.go` carries the fixup's own
half (the `RowOwned` report) and the fallback leg's; it is a separate file so
that the behavioural reproducer above stays runnable — and red — on master's API.

And the incident's own surface, `internal/tools/write_docindex_e2e_test.go`,
drives the real `Write` tool (`internal/tools`) against the same corpus:

- `TestWriteToolDoesNotReclassifyARowedDocument` — **RED on master's code**: the
  tool rewrote `devdoc/DOC-INDEX.tsv` for a document it merely overwrote, turning
  `devdoc/landed-rule.md  normative  …  path internal/docindex/docindex.go` into
  `devdoc/landed-rule.md  historical  historical (5 standing / 5 record): …` with
  an empty evidence cell — the incident's own row flip, one tool call deep.
- `TestWriteToolStillClassifiesANewDocument` — the **positive control**: a
  document with NO row still gets its row through the same door, and the row that
  was already there comes out byte-identical. Without it, a hook that had simply
  been switched off would pass the test above while the corpus silently lost its
  write-path classification.

**Measured in the live tree too.** Writing *this* document through the same tool
(the hook runs on it) produced a **pure insertion**: `git diff --numstat` on the
manifest was `1  0  devdoc/DOC-INDEX.tsv`, one added line, no existing row
touched — the hook classified the new path and left the 2422 rows it did not own
alone, which is the whole point.

## 5. Not done (honest)

- **The generator itself still replaces a row under `--only`.** That is its
  documented contract (the incremental mode), and it is the right primitive;
  what changed is that the *write hook* no longer reaches it for an
  already-classified path. A human running the command by hand still gets the
  documented replace — deliberately, and that is the one door where a
  content-aware re-classification is the point.
- **The two other callers of the generator leg are untouched**: the close-out's
  branch-scoped repair (`SyncBranch`/`Repair`, deliberately scoped to what the
  branch ADDS) and `EnsureDoc`'s fallback. Neither re-classifies an existing row
  (`EnsureDoc` looks the class up and keeps it).
- **`internal/standards`' own findings are not re-tested here** beyond running
  the suite: this change does not touch the gates.
- **The incident's remaining open question — whether a *second* mechanism also
  appends rows — is answered "no mechanism needed"**, not "rule out all others
  forever": the hook's write fully accounts for both observed rows, and no
  hidden process is required to explain the md5 movement or the blame landing on
  the previous commit.
