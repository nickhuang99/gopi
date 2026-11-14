<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Docclass Manifest Repair — whole-corpus regenerate + banner pass

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Task Report.** Session `01a0b736` · Branch `feat/docclass-manifest-repair` · Branched at master
`21f4cc71`, **rebased onto current master `fa12d97f`** before the final run.
Worktree `/home/user/workspace/gopi-docclass` (linked worktree — never the main repo; Rule 6/23/29).

Master moved while this task ran, and it moved the corpus: `fa12d97f` added
`devdoc/conflict-branch-rename-recovery.md` **without a manifest row**, i.e. master was red for the
coverage gate again on its own. The branch was therefore rebased and the corpus regenerated, so the
manifest covers the **union** of both corpora (master itself carried only a `DOC-INDEX.tsv`
one-line edit in that range, and the rebase was conflict-free).

## Completed

- Whole-corpus regenerate with the checked-in generators, **no hand-edited row**:
  `python3 tools/docclass/classify.py .` then `python3 tools/docclass/banner.py .`.
- `devdoc/DOC-INDEX.tsv` grew to cover **every file under `devdoc/`** — the files the gate named
  (including the `.tsv` / `.csv` / `.py` artifacts that had no row) now each carry a class, a reason
  and an evidence cell. Row-key diff against the base blob: **inserts 37, removes 0** (the generator
  never drops a row); 2055 manifest rows cover 2055 files under `devdoc/` (base: 2018 rows over 2054
  files, **36 uncovered**). `git diff --numstat` on the manifest: `118 81`.
- Banner pass: **28 historical docs received their first-line `<!-- doc-class: historical -->`**;
  one banner was **removed**, `devdoc/merger-loop-restart.md`, because its class legitimately became
  `normative` (see the flips below). `devdoc/daemon-death-pipe-fd-shape.md` — the doc named in the
  epic — now opens with the banner and the manifest gives it `historical`.
- Three class flips, each explained by master's own landed changes (not by the regenerate):
  | doc | old → new | why |
  |---|---|---|
  | `devdoc/merger-loop-restart.md` | historical → normative | newly cited as a landed ruling by `tasks/01a0b01d-report.md:30` |
  | `devdoc/merger-stop-takeover-contract.md` | normative → historical | its citing authority `internal/tools/merger_schedule.go` was deleted from master |
  | `devdoc/scratch-shm-role-split.md` | normative → historical | its citing authority `internal/tools/firstopen_sync_test.go` was deleted from master |
- Both target gates PASS and the whole `internal/standards` package is green.

### Where this report lives, and why (a real constraint of the generator)

This report is deliberately placed at the **`devdoc/` root, not under `tasks/`**. The classifier
builds its citation index from every non-`devdoc/` artifact, so a report under `tasks/` is an
**authority** file: merely naming `devdoc/<x>.md` in it stamps `<x>` as "cited as the live statement
by tasks/…". Measured during this task — the first draft of this report under `tasks/` flipped
`devdoc/daemon-death-pipe-fd-shape.md` (and two siblings) to `normative`, which **stripped the very
banner the epic requires** on the next generator run. Inside `devdoc/` a doc citing a sibling is
corpus-internal and creates no citation. 114 report/audit-class docs already live at the `devdoc/`
root for the same reason; this report follows that precedent.

## Not Done

- The pre-existing red in `internal/agent` (`TestDocAnchorsResolveInNormativeDocs`) is **not fixed**
  here: it is red on clean master as well (A/B below). Out of this epic's scope.
- The two anchor failures the widened scope newly exposes are reported, not repaired:
  `devdoc/fd-finalizer-leak.md` (`GOROOT/src/os/file_unix.go:243` — never resolvable in-tree) and
  `devdoc/requirements/99-summary.md` (a bare anchor drifted onto a blank line of
  `internal/tools/task_finish_cleanup.go`). Both docs had **no manifest row before**, i.e. they were
  ungated; repairing either would mean hand-editing a generated row or a frozen record, which the
  idempotence criterion forbids.

## Verification

- **Before** — the two gates, on the detached baseline worktree
  `/home/user/workspace/gopi-docclass-base` @ **`fa12d97f`** (current master, the branch's merge-base):
  `go test -count=1 ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile|TestDocClassBannerMatchesManifest'`
  → **FAIL**: `40 unclassified devdoc files (29 listed + "and 11 more")`,
  `devdoc/DOC-INDEX.tsv: 2018 rows cover 2054 files under devdoc/ (classes: map[historical:1601 index-meta:1 normative:404 roadmap:12])`,
  and `[FAIL] TestDocClassBannerMatchesManifest`: `devdoc/daemon-death-pipe-fd-shape.md: class "historical"
  but the file does not open with its banner — first line must be "<!-- doc-class: historical -->"`.
  (Base of record: the same two gates were also red at `21f4cc71` — 35 unclassified, `2017 rows cover 2052 files`;
  that older base is superseded by the `fa12d97f` baseline above, which is current master.)
- **After** — the same two gates (branch, close-out run):
  `go test -count=1 ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile|TestDocClassBannerMatchesManifest'`
  → `ok  github.com/nick/gopi/internal/standards  3.249s`
- **After** — the whole package: `go test -count=1 ./internal/standards/` →
  `ok  github.com/nick/gopi/internal/standards  61.203s` (no doc-class failure)
- **Idempotence** (the stability criterion): consecutive `classify.py` + `banner.py` runs at the
  committed state produce **zero git diff** — measured by hashing the manifest across a re-run:
  `sha256(devdoc/DOC-INDEX.tsv)` = `3bf4cc4e45de6bd136f47785357475d0d9034103f3ed3fd878e0ed5e7d9cc964`
  before and after, `banner.py` prints `banner applied to 0 docs, removed from 0`, and the only
  porcelain entry is this report file itself.
- **2nd-run zero-diff, witnessed on the committed state** (Rule 30 close-out): after the manifest was
  committed, `classify.py` + `banner.py` were run again in the worktree; `git status --porcelain`
  listed nothing but the not-yet-committed report, and `git diff --stat` touched no doc — the generators
  are converged, and hand-editing a row in place would be reverted by the next run.
- **Regression A/B** (`internal/agent` anchor gate): FAILS on **both** the branch and a detached
  master baseline worktree (`/home/user/workspace/gopi-docclass-base`, detached at the branch's own
  merge-base **`fa12d97f`**, i.e. current master), so it is pre-existing. Re-measured at close-out with
  **both worktrees on the same master** (`git worktree add --detach … fa12d97f`), full `-v` output kept
  and compared with the worktree path normalized to `REPO/`:
  base `1950 anchors / 404 normative docs / 79 new / 21 stale permits` vs branch
  `1962 / 406 / 76 new / 21 stale permits`. The pre-existing-drift **set** is identical between the two
  sides — `comm -3` over the normalized drift keys is empty, **80 drift lines over 41 docs on both**,
  and the **21 stale-permit lines are identical key-for-key** (`comm -3` empty). So the branch neither
  adds nor removes a permit and neither adds nor removes a drift site; the ±3 in the counter is the
  regenerate **fixing** three manifest rows' stale `sym`/`path`/`anchor` evidence. (An earlier pass ran
  this A/B against base `21f4cc71`, a *different* master, reading branch `118` vs base `121` keys with
  `18` stale permits; that cross-master reading is superseded by the same-master numbers above.)

## Evidence (self-filed)

- `python3 tools/docclass/classify.py .` → `{"historical": 1636, "index-meta": 1, "normative": 406, "roadmap": 12} total 2055`
  after the rebase onto `fa12d97f` (2052 before it; the base-of-record gate report read
  `map[historical:1600 index-meta:1 normative:404 roadmap:12]` over 2017 rows).
- `python3 tools/docclass/banner.py .` → `banner applied to 28 docs, removed from 1` on the first run;
  `banner applied to 0 docs, removed from 0` on every run after convergence.
- Row-key set diff computed against the base blob (`git show fa12d97f:devdoc/DOC-INDEX.tsv`), field-split,
  not guessed: **inserts=37, removes=0, class flips=3, evidence-only rows=78**; branch 2055 rows over
  2055 tracked devdoc files with **0 uncovered and 0 phantom** (base 2018 rows / 2054 files / 36 uncovered);
  `git diff --numstat fa12d97f -- devdoc/DOC-INDEX.tsv` → `118  81`.
- Manifest classes: branch `{historical:1636, index-meta:1, normative:406, roadmap:12}` vs base
  `{historical:1601, index-meta:1, normative:404, roadmap:12}` — the 3 flips are
  `merger-loop-restart.md` (historical→normative), `merger-stop-takeover-contract.md` and
  `scratch-shm-role-split.md` (normative→historical), each explained in the Completed table above.
- `git status --porcelain | grep -v 'devdoc/'` → 0 lines: the change touches only the manifest and
  devdoc markdown (the two generators are unmodified).
- Idempotence hashes: the same diff captured on two consecutive runs hashes identically
  (`cmp` → ZERO-DIFF); `banner.py` reports 0 applied / 0 removed on the second run.
- Anchor A/B: detached baseline created with `git worktree add --detach ~/workspace/gopi-docclass-base fa12d97f`;
  full `-v` output captured on both sides and normalized (`sed -e 's#…gopi-docclass-base/#REPO/#' -e 's#…gopi-docclass/#REPO/#'`);
  `comm -3` over the drift keys → empty, **80 lines / 41 docs on both sides**, **21 stale-permit lines
  on both sides**, branch summary `1962 anchors / 406 docs / 76 new` vs base `1950 / 404 / 79 new`.
  Branch `git status --porcelain` at close-out: report file only, then empty after its commit.
- Rule 27 honoured: only scoped `-run` gates and the `internal/standards` package were executed — no
  load generator, no device work, no whole-tree suite.
