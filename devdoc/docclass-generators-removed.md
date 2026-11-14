<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The doc-class generators are gone — what decided a class, and what decides it now

Task `01a161fe-313c-77db-9fe6-78624f30924f` (document-pipeline teardown, the
generator slice). The write path's truth-class hook was REWRITTEN in Go by
`5a35f61`/`2608a09` and no longer re-decides a declared class — but the
generators it replaced were still tracked, still executable, and 73 devdocs still
printed the command that reproduces the old overwrite. A reader in any clone
could run `python3 tools/docclass/classify.py . --only <path>` and then
`banner.py --only <path>` and get back the exact defect the Go hook removed: the
manifest row decides, line 1 is rewritten from it, and a document that declared
`normative` comes out `historical`.

This page is the account of deleting that mechanism.

## What was deleted

`tools/docclass/` in full, four files, removed with `git rm`:

| file | what it did |
|---|---|
| `classify.py` | derived each document's class by HEURISTIC (title/name/citation shape) and wrote the row into `devdoc/DOC-INDEX.tsv` |
| `banner.py` | rewrote line 1 of the document FROM that row — the overwriting half |
| `classify_test.py` | the generator's own unit tests (52 arms) |
| `arm6_census.py` | the measurement helper for one arm of the classifier |

**Measured: no live caller.** The only references in Go are COMMENTS
(`internal/tools/write.go`, `header_hook.go`, `internal/taskreport/driftclaim.go`,
`internal/standards/testdata/repo-hygiene-census.go`) and two test fixtures whose
subject is shell-list PARSING, not classification
(`internal/tools/task_briefing_path_set_test.go`,
`internal/taskreport/driftclaim_test.go` — both keep the string as sample prose).
`Makefile` names neither the directory nor a script. Evidence:
`evidence/01-no-callers.txt` in this run's session directory.

**`devdoc/DOC-INDEX.tsv` itself survives on purpose.** The merge path still reads
it (`internal/devdoc.ManifestName`, and `merge_union_rows.go`'s MANIFEST-DUP
lane), so only its GENERATORS went. Removing the manifest is slice 4/5's job —
doing it here would have broken the merge path in the same commit.

## What replaced the prose

73 devdocs printed a `python3 tools/docclass/...` command line (a 74th, this
page, grew one while describing the removal). They are records of past runs, so
the history stays: each one now carries — immediately after its own line-1 banner
and before any other content — the same note, which says the directory is
deleted, that a class now comes from the file header, and that the quoted commands
are a record and cannot be re-run. `devdoc/evidence/repro-inherited-row-rewrite.py`
is the one exception: a python EVIDENCE script, whose docstring quotes the
deleted classifier's defect and carries an interpreter shebang, not a fenced
recipe.

The files, named individually because a sweep that does not list its members
cannot be reviewed (`git diff --name-only` for the same set):

- `acceptance-count-clause-contradiction-20261014.md`
- `anchor-cell-owner-and-empty-freeze-table.md`
- `anchor-excerpt-fixed-point.md`
- `anchor-number-baseline-ratchet-20261013.md`
- `anchor-number-producer-checker-arm.md`
- `assert-the-bug-audit-anchor-review-20261005.md`
- `blocked-delivery-redelivery-e2e-proof.md`
- `briefing-pinned-path-set-staleness-at-dispatch.md`
- `chat-input-self-drive-acceptance.md`
- `chat-wrap-tty-width-salvage.md`
- `cli-help-user-audit-class-report.md`
- `cli-reorg-e2e-verification.md`
- `cli-reorg-red-baseline-20261007.md`
- `cli-tool-group-door-closure.md`
- `derived-manifest-merge.md`
- `design-audit-modules.md`
- `doc-evidence-freshness-gate.md`
- `doc-truth-classification-report.md`
- `docanchor-repair-shape-20261002.md`
- `doccheck-clean-path-count-line.md`
- `doccheck-corpus-consistency.md`
- `doccheck-fix-single-corpus-render.md`
- `doccheck-per-path-phase-silence.md`
- `doccheck-permit-not-reproduced-20261016.md`
- `doccheck-stored-cell-drift-12-rows-reground.md`
- `doccheck-stored-cell-drift-cleared-by-generator.md`
- `doccheck-stored-cell-drift.md`
- `docclass-anchor-tier-determination-20261012.md`
- `docclass-banner-body-scan.md`
- `docclass-cli-help-audit-truth-class.md`
- `docclass-duplicate-row-convergence.md`
- `docclass-duplicate-row-notice-states-the-surviving-class.md`
- `docclass-incremental-mode.md`
- `docclass-manifest-repair-20260919.md`
- `docclass-scope-file-selects-the-writer.md`
- `docgate-manifest-and-anchor-repair.md`
- `docindex-blank-line-shape.md`
- `docindex-duplicate-row-unblock.md`
- `docindex-generation-preserves-stored-rows.md`
- `docindex-inherited-row-rewrite.md`
- `docindex-stored-cell-drift-stock.md`
- `docindex-write-path-row-ownership.md`
- `docindex-write-path-row-refresh.md`
- `durable-anchor-cells.md`
- `help-slimming.md`
- `materials-competitor-comparison-round.md`
- `merge-resolution-voice-input-rig-conflict.md`
- `merge-union-lane.md`
- `merge-union-row-check-anchor-drift-determination.md`
- `o2-peer-channel-remeasurement-20261003.md`
- `orphan-landing-01a142f5-bookkeeping-20261017.md`
- `orphan-rework-residual-measurement.md`
- `other-citing-trees-authority.md`
- `python-suite-on-the-gate-path.md`
- `replay-dependency-preflight.md`
- `repo-history-rewrite-plan.md`
- `retired-verb-refusal-probe-raw.md`
- `rework-bound-notice-count-reconciliation.md`
- `rule18-new-violation-premise-is-ref-relative.md`
- `run-record-is-not-authority.md`
- `scratch-base-suite-root-leak.md`
- `scratch-gate-root-widening.md`
- `sessions-tree-siblings-census-report.md`
- `standards-gate-reds-already-fixed.md`
- `status-row-context-repartition.md`
- `task-01a13141-gocache-gib-forms-dedup-report.md`
- `task-01a133ee-brief-correction.md`
- `task-ground-lost-diagnosis.md`
- `task-reopen-verb-restored-merge-artifact.md`
- `testtree-typecheck-gate.md`
- `truth-class-repair-retired-verb-docs.md`
- `undispatched-tool-words-gate-844d1f3d.md`
- `write-hook-outside-the-write-deadline.md`

`PROJECT.md` never mentioned the generators, so its Rule 36 section needed no
edit — it already says the header is the record and names the in-process hook.

## The regression test

`internal/tools/header_hook_retired_test.go`, two arms:

- **`TestRetiredDocclassGeneratorsAreNotTracked`** measures the INDEX
  (`git ls-files tools/docclass`) and the worktree: a re-added generator is a file
  every clone inherits and every reader can run, and "not tracked" is not
  satisfied by a file merely staged for deletion while still on disk.
- **`TestEveryDeclaredClassSurvivesBothWritePaths`** walks
  `devdoc.Classes` — the whole closed vocabulary, taken from the package that
  owns it — through BOTH entry points: a `Write` whose bytes already declare the
  class, then an `Edit` that lands only inside the body (so the header is outside
  the match window and the hook must carry it across). The pre-existing arms pin
  `normative` through Write; the original defect was a FLIP, and a single-class
  arm cannot see the other three.

The third arm is the existing cost bound (`headerHookCostBound`, 250 ms): a
`python3` launch cannot fit in it, so a subprocess returning to the write path
fails a test rather than being noticed by a reader.

## Verification performed

- `go build ./...` → exit 0.
- `go test ./internal/tools/ -run 'TestWriteNeverOverridesADeclaredClass|TestEveryDeclaredClassSurvivesBothWritePaths|TestRetiredDocclassGeneratorsAreNotTracked|TestWriteGivesACorpusDocItsClassHeaderAndSpawnsNothing|TestEditKeepsTheCorpusHeader' -v`
  → 7 PASS (4 of them sub-arms), `ok … 2.053s`.
- `go test ./internal/devdoc/ ./internal/standards/ -run 'RepoHygiene|Doc|Class'`
  → both `ok`.
- `go run internal/standards/testdata/repo-hygiene-census.go . internal/standards/testdata/repo-hygiene-baseline.tsv --check`
  → `matches the tracked tree (28 rows)`, exit 0. No permit named a deleted file,
  so no row needed dropping (Rule 39) — measured, not assumed.
- `git ls-files tools/docclass | wc -l` → `0`.
- **Real invocation** (the surface, not its unit test): the built binary was asked
  to create `devdoc/zz-hook-probe.md` whose line 1 is
  `<!-- doc-class: normative -->` →
  `tool_calls=5: … write 1`, and the file on disk read
  `[<!-- doc-class: normative -->]` on line 1. The probe file was removed after
  the reading; the invocation, its run log and the observed bytes are under
  `evidence/02-real-write*.{log,txt}` in this run's session directory.
- Corpus banner sweep: every tracked markdown under `devdoc/` still has a
  well-formed `<!-- doc-class: -->` on line 1 (the whole-tree standards arm this
  project's Rule 36 text names does not exist at this HEAD — that gap is a
  separate finding, not repaired here).

## Known-broken, not from this change

`internal/tools` and `./cmd/agent` carry pre-existing reds measured at this
base's ancestry (bug rows `01a161f3-67e5`, `01a1625b-29a4`, `01a16257-8c58`).
They are untouched by this diff — it adds one test file and deletes four
unreferenced scripts — and are not repaired here.
