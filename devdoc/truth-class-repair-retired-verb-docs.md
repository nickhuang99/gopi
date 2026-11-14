<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the two inherited devdoc rows (retired-verb sweep, phase 1) decided on their merits

## Completed

- **`devdoc/scripts-retired-verb-sweep.md` → `historical`.** The inherited row was
  `normative` with an EMPTY evidence cell (the gate's `normative-no-check`) while the
  file's own first line already carried `<!-- doc-class: normative -->` — a banner on a
  class that claimed nothing. Both rows were re-written **through the corpus's own
  writer**, never by hand (Rule 36):
  `python3 tools/docclass/classify.py . --only devdoc/scripts-retired-verb-sweep.md --only devdoc/verb-retirement-phase1.md`
  → `{"added": 0, "updated": 2, "rows": 2055}`.
- **`devdoc/verb-retirement-phase1.md` → `historical`, with the banner that class
  requires.** The classifier offered no better option and the file's own evidence
  agrees: `code_spotcheck()` resolves nothing for it (`sym`), and the ONLY symbol the
  doc names that resolves is
  `cmd/agent/chat_surface_bound_e2e_test.go TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`
  — a test the doc names as **NOT this phase's** ("it fails intermittently under machine
  load … and it passes alone … against this branch"). A spot-check chosen from that
  sentence would hold the class up on the doc's own disclaimer, which is exactly the
  "asserted, not held" defect the gate exists to catch.
- Banners applied through the writer's own banner arm:
  `python3 tools/docclass/banner.py . --only … --only …` → `banner applied to 2 docs,
  removed from 0`; then `banner.py . --check` → `0 markdown docs whose banner disagrees
  with the manifest`.
- A bug found in that writer while using it is filed as its own row
  (`01a10fbd-17bc-78a3-8baa-844c4f60f507`) — see `## Not Done (honest)`.

## The class decisions, argued (not asserted)

The corpus policy (`tools/docclass/classify.py`, its docstring is the authority) makes
`normative` = a STANDING claim about current master carried by ONE resolvable spot-check
(`anchor|sym|path|cite`), and `historical` the default for a past run's record. Both files
are records of a run that already happened, and neither has a spot-check that stands on
its own:

| doc | why it is a record | why not `normative` |
|---|---|---|
| `scripts-retired-verb-sweep.md` | the doc names `artifacts/scan-old-spellings.sh` as its instrument — **that path does not exist in the tree** (`ls: cannot access 'artifacts/scan-old-spellings.sh': No such file or directory`); its own `## Not done` lists a site still broken (`scripts/materials/env-catalog-audit.sh` and `scripts/materials/` are both gone from the tree) and four red standards tests it does not fix | the only spot-check the classifier can resolve is `path cmd/agent/cli_verb_aliases.go` — a weak, coincidental path (the true `verbAliases` table lives at `cmd/agent/cli_verb_aliases.go:155`, but the doc names the file as the *instrument's* source, a file it then says was replaced); a `path` tier that resolves only says "a file the doc mentions still exists", which is not a standing claim the anchor gate could hold |
| `verb-retirement-phase1.md` | the file says so about itself: `Truth class: **normative** — a standing claim about **this branch's** surface` — a *branch* claim, and that branch is converged; the retirement is stated **complete** elsewhere by `devdoc/verb-retirement-complete.md` (classed `normative`, `grep 'phase1\|phase 1'` → 0 hits, i.e. it does not cite this doc) | the doc's own `## Files`/measurements are a run's account (the 88-column reflow, the two broken gates, the resume bug it found), and its recorded spot-check would have to be the flaky test above — the classifier's `sym` candidate for the file |

Recorded for the next reader: the sibling-docs check is real — `devdoc/verb-retirement-complete.md`
is `normative` and IS cited from `cmd/agent/retired_verb_test_driver_gate_test.go:7`, so the
`sequential` half of this doc's subject is "the live door: a caller is refused by name". What
has no successor is the **phase** — the plan-level word is gone with the two doors defined
(`retired`, `unimplemented`), and `internal/standards`' own gate
(`TestDocClassBannerMatchesManifest`, plus the sibling
`TestCapdemoExternalBundleAnchorsResolve`) was RED on master before this repair. That is why
the `sequential` family counts as a record here: the family is live, this document is an
account of one step it took, so it carries the account banner.

## Not Done (honest)

- **`tools/docclass/banner.py` writes a SECOND, contradicting banner** when the doc already
  carries a `<!-- doc-class: … -->` line that is not its first line: it prepends the new
  banner and leaves the stale one readable below it. Measured here:
  `banner applied to 1 docs, removed from 0` on `devdoc/scripts-retired-verb-sweep.md`
  produced `<!-- doc-class: historical -->` followed by the pre-existing
  `<!-- doc-class: normative -->`. `banner.py . --check` then reports **0 mismatches** —
  the writer's own check cannot see the double banner, and both gates read only the first
  line (`internal/docindex.FirstLine`), so nothing in the suite catches it. Filed as
  `01a10fbd-17bc-78a3-8baa-844c4f60f507` with the exact reproduction; NOT fixed here
  (Rule 26: an incomplete fix is repaired by dispatching again, and this repair's diff was
  asked to stay the class decision + banner only).
- **`TestDocAnchorsResolveInNormativeDocs` (internal/agent) still FAILS**, and this repair
  is what removed one of its reds: at master it reported `648 normative docs … 6 new, 4
  stale permits`, and one of the `6 new` was
  `devdoc/DOC-INDEX.tsv:1438: class "normative" with no recorded spot-check` — the very row
  repaired here. It now reports `647 normative docs … 5 new, 4 stale permits`. The remaining
  five new drifts and four stale permits are pre-existing and in other writers' rows
  (`chat-status-row-partition.md`, `cov-agent-cr.md`, `requirements/04-sync-and-nodes.md`,
  `task-notify-study-20260920.md`, `verb-retirement-complete.md`; stale permits in
  `hippocampus.md`, `requirements/cli.md`). This run did not touch them: deleting a permit
  is another writer's permission and re-keying a normative anchor is a different repair.
  **That gate is NOT green, and nothing here claims it is.**

## Verification

- Build: `CGO_ENABLED=0 go build -o ~/.gopi/sessions/01a10faf-6d83-7ab0-8bec-946fa1a540f9/artifacts/gopi ./cmd/agent/` → `BUILD_OK` (the binary was built BEFORE the repair, to capture the red baseline).
- Tests: `go test ./internal/standards/ -run 'DocIndex|DocClass|Manifest' -count=1` → before `FAIL ... 3.847s` with `docclass_gate_test.go:161 devdoc/DOC-INDEX.tsv:1438: devdoc/scripts-retired-verb-sweep.md is classed "normative" with an EMPTY evidence cell` + `:212 carries the "normative" banner but … classes it "normative"` + `:205 class "historical" but the file does not open with its banner`; after → `ok github.com/nick/gopi/internal/standards 1.297s`.
- Ran: `./artifacts/gopi tool doccheck` (the same binary, in the repaired worktree) → **before**: `doccheck: normative-no-check devdoc/scripts-retired-verb-sweep.md …` + `doccheck: 1 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv` (exit 1); **after**: `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row` (exit **0**).
- Ran: `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, exit 0; `python3 tools/docclass/banner.py . --check` → `0 markdown docs whose banner disagrees with the manifest`, exit 0; `python3 tools/docclass/classify_test.py` → `Ran 6 tests … OK`.
- Result: both writer arms are **idempotent** — a second `classify.py --only …` (printed `{"added": 0, "updated": 2, "rows": 2055}`) plus `banner.py --only …` (`banner applied to 0 docs, removed from 0`) leaves `git diff --numstat` at
  `1 1 devdoc/DOC-INDEX.tsv` / `1 1 devdoc/scripts-retired-verb-sweep.md` / `1 0 devdoc/verb-retirement-phase1.md`, i.e. exactly the class cell, the banner swap and the added banner — no collateral rewrite of the 2054 other rows.
- Ran: `go test ./internal/agent/ -run 'DocAnchorsResolveInNormativeDocs' -count=1` → exit 1 both before and after (pre-existing), with the `comm`-style line diff of the two verdict sets showing the repair REMOVED one line (`devdoc/DOC-INDEX.tsv:1438: class "normative" with no recorded spot-check`) and added none: the gate's red set moved 6 new → 5 new.
- Commit: `51b59ae75 docs(devdoc): classify the retired-verb sweep and phase-1 docs as the records they are` — `devdoc/DOC-INDEX.tsv`, `devdoc/scripts-retired-verb-sweep.md`, `devdoc/verb-retirement-phase1.md`.

## Evidence (self-filed)

- `python3 tools/docclass/classify.py . --dry-run --only devdoc/verb-retirement-phase1.md` → `devdoc/verb-retirement-phase1.md\thistorical\thistorical (5 standing / 2 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority\t` — the manifest's byte-for-byte new row.
- `python3 tools/docclass/classify.py . --dry-run --only devdoc/scripts-retired-verb-sweep.md` → `… historical (0 standing / 2 record): …` (the identical reason shape `banner.py` had already applied at the write path).
- `ls -l cmd/agent/cli_verb_aliases.go artifacts/scan-old-spellings.sh` → `cli_verb_aliases.go` present; `artifacts/scan-old-spellings.sh: No such file or directory` — the instrument this doc's conclusion rests on is gone from the tree, which is the "record, not a standing claim" reading.
- `grep -n 'phase1\|phase 1' devdoc/verb-retirement-complete.md` → 0 hits (the complete-retirement doc does not cite the phase-1 doc).
- `git rev-parse HEAD` (the delivered tip) and the branch: `task/01a10faf6d837ab08bec946fa1a540f9`.
