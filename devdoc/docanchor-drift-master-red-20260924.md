<!-- doc-class: historical -->
# Task Report: `TestDocAnchorsResolveInNormativeDocs` repaired — master's 75 new / 11 stale drift permits → 0 / 0

## What was red, and why

On untouched master (`98e8941c`) the manifest-scoped doc-anchor gate fails:

```
docanchor_test.go:214: doc anchors verified: 2494 across 513 normative docs
  (2056 explicit + 438 bare continuations; 455 bare out of row/table/heading scope;
   233 pre-existing drift entries permitted, 75 new, 11 stale permits)
--- FAIL: TestDocAnchorsResolveInNormativeDocs (0.87s)
```

Two independent debts, both red by design (a NEW failure is red, and a permit
that matched nothing is red too — a stale permit is a silent permission):

1. **75 new** — a drift entry (KEY `doc|kind|detail`) with no permit. The mix
   was: standing specs whose line accelerators had moved, `:NNN` anchors whose
   bare form inherited the WRONG file, and ~30 entries in landed run reports
   whose anchors this repair must not rewrite (re-keying a record falsifies it —
   `devdoc/req-1-merge-branch-lifecycle.md:229`).
2. **11 stale** — permits whose site no longer exists (the symbol moved, the
   file was deleted, or the anchor's own row was re-keyed away from it).

## The rule this repair applies

The gate's own doctrine, which the row's **symbol** is the authority for and its
**line number is only an accelerator**:

- **Standing claim about master** (spec, requirements reverse-analysis,
  checklist, study) → **re-key the doc** to the symbol's current line.
- **Landed record** (dated Task Report, run log, diagnosis, re-measure) →
  **keep as a permit with a row-specific reason**; never re-key, never delete.

And the trap that decides most of the residue: **a bare `` `:NNN` `` inherits its
file from the row's backtick context, then the same table block, then the
nearest preceding heading.** Two failure modes follow, both measured here:

- a row with **no `.go` basename** can never resolve — it was rewritten to name
  the symbol (`devdoc/taskid-whitespace-normalize-salvage.md`: two raw trims that
  the doc had recorded as code become `finishCleanupRemovalStateReason`
  (`internal/tools/task_finish_cleanup.go:2713`) and `taskFindSessionJSONLForRow`
  (`internal/tools/tasks.go:2034`));
- the **nearest heading's** backticked `.go` token can silently supply the wrong
  file — that is how `hippocampus.md`'s new dispatch anchors first resolved to
  `clock.go:5369/5378/5434` (the heading above them names `clock.go`), an error
  the emitter caught immediately and the explicit path fixed.

## Method (repeatable, and the reason no entry was guessed)

The gate logs full messages only for NEW drift; permits print without their key.
So the repair used a **throwaway in-package emitter** (deleted before the
commit, never shipped) that calls `docAnchorScan` + `docAnchorEvidence` per gated
doc, reads `docAnchorBaseline`, and writes `status\tclass\tkey\tmsg` to `$KEYOUT`.
The base key set was frozen first; after **every** edit batch the emitter was
re-run and `comm -13` against the frozen set named any regression. That loop
caught and fixed five self-inflicted regressions (the `clock.go` inheritance
above; `loop.go:2673` vs `task_reclaim.go:2673` on one row; a duplicated
`cmd/agent/cmd/agent/...` path; two past-EOF `wip_worktree_repro_test.go`
anchors). Counts walked 75→68→65→46→45→33→19→18 new while never adding a red.

## Disposition (86 entries: 75 new + 11 stale)

| disposition | count | meaning |
|---|---|---|
| **re-keyed in the doc** | **57** | standing claim, symbol located, line accelerator corrected |
| **kept as a permit** (row-specific reason added) | **18** | landed record, deleted file, ambiguous basename, or a never-resolvable path (GOROOT) |
| **stale permit deleted** | **11** | site gone: symbol moved / file deleted / row re-keyed away |
| **still red at close** | **0** | — |

The file-by-file list is the commit's `internal/agent/testdata/docanchor-baseline.tsv`
diff (`-12` rows, `+18` rows; 239 permits, 0 malformed, 0 duplicate keys).

Re-keyed docs (each anchor checked non-blank inside the worktree and re-measured
after the patch): `hippocampus.md` (MemoryHit `:355`→`:467`; NoEnforceMemory
`:1769`/`:2578`→`:1859`/`:2823`; dispatch `:5083`→`:5092` and store `:5106`→
`internal/agent/loop.go:5369`/`:5378`/`:5434`; Interrupt `:2341`→`:2870`; the
session-logger row → `internal/agent/session.go:295`/`:311`/`:324`/`:339`/`:355`/`:370`;
`heartbeat.go` decls `:11`/`:12`/`:42`/`:50`/`:102`/`:138` with field writes
`loop.go:2841`/`:2842`; `:3448`–`:3455`→`:3449`), `agent-five-organs-ontology.md`,
`requirements/04-sync-and-nodes.md` (incl. `config/node_role.go:14-15`→`:18-19`,
`config.go:554`/`:560`→`:311`/`:614`), `requirements/02-merge-and-branches.md`
(`merge_contract_gate_test.go:327`/`:366`→`:324`/`:362`), `look-without-fixing-bound.md`
(`loop.go:254`/`:256`→`:269`, `:301`/`:303`→`:316`, polling `:1978-1990`→`:1981-1987`),
`requirements/test-infrastructure.md` (flake gate `:144`→`:146`),
`task-notify-study-20260920.md` (`config.go:486`/`:514`/`:707`→`:494`/`:543`/`:735`),
`why-worktree-residue.md` (`loop.go:2471-2326`→`:2750`),
`task-report-20260912.md` (`tasks.go:2516`→`:359`),
`audit-ab3-peerbox-2026-09-04.md` (`tasks.go:3113-3141`→`:5899`),
`docanchor-gate-bare-lineno.md`, `coding-standards-checklist.md`,
`review2-cli-config-options-variant-20260903.md` (`loop.go:2204/2059`→`:1668/2739`),
`merger-help-truth.md`, `cli-as-eyes-and-hands-feasibility.md` and
`cli-ui-feasibility-study.md` (bare `render.go`/`render_test.go` → explicit
`internal/lineedit/...`), `taskid-vs-sessionid-verdict.md` (session.go:107→:87;
`taskShowWithDB` `:2600-2645`→`:2688`), `web-m2-b5b8b9b12-remeasure.md`
(`cli_web.go:976`→`:1095`), `hp-push-diagnosis-20260916.md`
(`cli_tasks_worker.go:234`→`:233`), `review2-tasksync.md`
(`taskregister.go:169-211`→`:161-211`), `cov-agent-cr.md` (`:124`→`:161`),
`taskid-whitespace-normalize-salvage.md`, `brew-subcommand-test-hermetic.md`
(`cli_voice_review_test.go:1694`→`:1687`), `style-sweep-20260912.md`,
`assertion-totality-gate-master-red.md`, `nightshift-20260911.md`.

Permit rows added (18), each with its own reason — the families: the six
`wip_worktree_repro_test.go` past-EOF anchors of
`assertion-totality-gate-master-red.md` (the file is 196 lines today; the row
records the pre-split length), the two manifest **evidence cells**
(`look-without-fixing-bound.md` `ev-anchor 254`, `nightshift-20260911.md`
`ev-anchor 1904` — generator-owned cells and, for the nightshift log, the drift
itself is the subject), `GOROOT/src/os/file_unix.go:243` (a Go standard-library
path can never resolve in-repo), the two retired-`merger_takeover.go` anchors of
`merger-help-truth.md`, the deleted `merger_lock_ownership_e2e_test.go`,
`task_finish_premerge.go:820` past EOF (637 lines, and no symbol in the row
resolves), the two ambiguous `session.go` anchors and the ambiguous
`cli_web.go:2007` blank/short-import pair.

## Verification

- **Build**: `go build ./...` → OK.
- **Tests** (branch-scoped smoke, Rule 5/27): `go test -count=1 -run TestDocAnchors ./internal/agent/`
  and `go test -count=1 ./internal/standards/`.
- **Ran** (the exact acceptance command):
  ```
  go test -count=1 -v -run TestDocAnchors ./internal/agent/
  ```
- **Result** (gate, verbatim tail — the FINAL binary, after the report's own
  DOC-INDEX row + `<!-- doc-class: historical -->` banner were added):
  ```
  docanchor_test.go:214: doc anchors verified: 2488 across 513 normative docs
    (2048 explicit + 440 bare continuations; 454 bare out of row/table/heading scope;
     239 pre-existing drift entries permitted, 0 new, 0 stale permits)
  --- PASS: TestDocAnchorsResolveInNormativeDocs (0.80s)
  ok  	github.com/nick/gopi/internal/agent	1.464s
  ```
  (The report's own row is classified **historical** — a past run's record, so it
  carries no anchors and cannot join the normative scope. The manifest run is
  idempotent from here: re-running `classify.py` reproduces this row.)
- **Ran** (A/B attribution — a detached master worktree, path-normalized):
  ```
  git worktree add --detach /tmp/gopi-ab-base master && cd /tmp/gopi-ab-base
  go test -count=1 -v -run TestDocAnchors ./internal/agent/
  ```
- **Result** (base side of the A/B):
  ```
  docanchor_test.go:214: doc anchors verified: 2494 across 513 normative docs
    (... 233 pre-existing drift entries permitted, 75 new, 11 stale permits)
  --- FAIL: TestDocAnchorsResolveInNormativeDocs (0.87s)
  ```
  Same command, same tree layout, only the branch's 23 doc edits + the baseline
  diff between them: **75 new / 11 stale → 0 new / 0 stale**. The 6-anchor
  delta (2494→2488) and the permit-count delta (233→239) are the deleted stale
  rows + the added permit rows, i.e. the intended change only.
- **Ran** (standards, same invocation on both sides):
  ```
  go test -count=1 ./internal/standards/
  ```
- **Result**: **4 failures on BOTH sides — no new failure.** They pre-date this
  branch: `docclass_gate_test.go:205` × 3 (`devdoc/full-id-no-prefix.md`,
  `devdoc/handoff-20260924-night-autonomous.md`, `devdoc/runaway-loop-20260923.md`
  carry class `historical` without the banner line) and
  `docclass_gate_test.go:222` (`TestDocClassBannerMatchesManifest`). This branch
  does not repair them (a different debt, and the second one's own text says it
  is the historical-banner repair's job).

  One nuance, measured: `docclass_gate_test.go:222`'s **counter** moves
  734→735 bannered markdown docs because this report is a new bannered
  (historical) doc. The test's verdict line is `0 mislabelled` on both sides, and
  `--- FAIL` names only `TestDocClassBannerMatchesManifest` on both sides — no
  new failing test. `TestDocManifestCoversEveryDevdocFile` **was** momentarily
  red when this report first landed without its row/banner; the row + banner
  above are the fix, and it is green now (that sequence is why the manifest run
  and the report are in the same commit).

## Not Done (honest)

- **`internal/standards` is red on master, before and after this branch.** The
  acceptance clause "`go test ./internal/standards/` stays green" cannot be met
  by any edit here: the four failures pre-date the branch and are one family
  (three docs whose class is `historical` but whose first line is not
  `<!-- doc-class: historical -->`). **The right owner is the historical-banner
  repair** (`tasks/01a0c621-docclass-historical-banner-report.md` is cited as the
  landed ruling for one of them), not this task. Shown as a same-output diff of
  the two runs, not asserted.
- **Two devdoc claims are now provably FALSE and were NOT rewritten** (they are
  records, so the defect is reported instead of edited):
  1. `devdoc/hippocampus.md`'s heartbeat row said the old state machine is
     **"已死的"/zero-readers**. It is not: `internal/agent/loop.go:1981`/`:1986`
     declare the fields, `:2841`/`:2842` write them, and
     `heartbeat.go:11`/`:12` (`defaultHeartbeatInterval`, `defaultStaleRounds`)
     are read. The correction is landed in the doc's spec table (a standing
     claim), and the reason is recorded in the row.
  2. `devdoc/coding-standards-checklist.md` row 4.4 cited the 334-line function
     as `cli_web.go:806`; that literal now resolves into `cmd/agent/cli_web.go:806`
     as a blank line, and the intended function moved to
     `internal/cli/cli_web.go` (the file is 334 lines there). The anchor was
     repaired; the row's prose still needs a naming edit.
- **The `ev-anchor` manifest cells were left as-is on purpose.** Two rows
  (`look-without-fixing-bound.md`, `nightshift-20260911.md`) carry an evidence
  cell whose cited line is blank. Those cells are **generator-owned**
  (`tools/docclass/classify.py`); regenerating the manifest was measured to
  change scope membership (it adds `devdoc/agent-sandbox-unify.md` and
  `devdoc/cli-bubbletea-carrier.md` ambiguity entries) and was therefore
  rejected as out of scope. Hand-editing a generated cell would drift from the
  generator, so both are permitted with that reason visible.
- **`devdoc/requirements/04-sync-and-nodes.md` §6.1's role-gate claim is
  unlocatable**: the row cites `task_finish_premerge.go:820`, but that file is
  637 lines today and contains **no** `role`/`Role` symbol at all. The line
  reference was marked stale in the doc; the claim itself needs a fresh reading
  by whoever owns that requirement.
- The throwaway emitter test (`internal/agent/zz_docanchor_keys_emit_test.go`)
  is deleted; it is a measuring instrument, never a deliverable.

## Evidence (self-filed)

- Gate, branch: `doc anchors verified: 2488 across 513 normative docs (... 239 pre-existing drift entries permitted, 0 new, 0 stale permits)` + `--- PASS` + `ok github.com/nick/gopi/internal/agent 1.470s` (exit 0).
- Gate, detached master worktree at the same commit the branch forked from: `... 233 pre-existing drift entries permitted, 75 new, 11 stale permits` + `--- FAIL` (exit 1).
- Emitter loop: `/tmp/keys-final.txt` (frozen base: 75 new / 11 stale / 245 permitted) re-diffed after every batch; final pre-baseline-edit set `/tmp/keys-12.txt` = 18 new / 12 stale; the 12 stale keys were deleted and the 18 new keys added as permits, after which the gate reads 0/0.
- Baseline integrity: 239 permit rows, `bad field counts: 0`, `dups: []` (python check on the file, not on the gate's parse).
- Standards: `diff` of the two runs' failure lines after stripping timings → identical (4 lines), i.e. no new failure.
- Worktree state at close: `git status --porcelain` → only the tracked edits below, no untracked file.

## Commit

Branch **`fix/docanchor-drift-master-red`** (worktree
`/home/user/workspace/gopi-docanchor-drift`, forked from master `98e8941c`).
Recovery: `git show fix/docanchor-drift-master-red --stat` and
`git show fix/docanchor-drift-master-red:devdoc/docanchor-drift-master-red-20260924.md`.
