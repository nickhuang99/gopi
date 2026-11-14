<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# Task Report: the SECOND pass on the doc-anchor drift — `TestDocAnchorsResolveInNormativeDocs` GREEN at master `6936c787`

**Read `devdoc/docanchor-drift-repair-20260927.md` first** — it is the FIRST repair
(base master `c183aab6`, commits `259619e2`/`4a3664d8`) and this document is its
successor, not a replacement. This pass is what closed the drift that accumulated
AFTER that repair landed.

**Task:** `01a0e2ea-91f3-7e91-85e4-f99317484247` — *master red: internal/agent
TestDocAnchorsResolveInNormativeDocs fails (15 new + 4 stale permits)*
**Branch:** `task/01a0e2ea91f37e9185e4f99317484247`
**Worktree:** `/home/user/workspace/gopi-01a0e2ea91f37e9185e4f99317484247` (base
master `6936c787`)
**Date of measurement:** 2026-09-27 (every number below was produced by this run's
own invocations, quoted verbatim)

## Completed

The gate was red for two independent reasons, and both were repaired by the
mechanism the gate's own doctrine names — **re-key by SYMBOL**, never by arithmetic
shift and never by adding a bare-lineno permit.

**1. Ten anchors re-keyed to the symbol their own table row names (15 textual
occurrences — the same problem appears more than once in one doc, and one permit
covers both).** Each target line was located by the symbol, not by line arithmetic,
and each was verified non-blank before the edit:

| doc | was | now | symbol the row names |
|---|---|---|---|
| `devdoc/hippocampus.md` | `internal/config/config.go:944` | `:964` | `taskStallSecEnvLegacy` (the old `GOPI_TASK_STALE_SEC` spelling) |
| `devdoc/hp-push-diagnosis-20260916.md` | `cli_tasks_worker.go:233` | `cmd/agent/cli_tasks_worker.go:347` | `runTaskWorkerLoop` — the worker loop's own lifecycle passes |
| `devdoc/reclaim-deterministic-checklist.md` | `tasks.go:6662` | `internal/tools/tasks.go:7463` | the `verify-ground` removal arm (`removalaudit.ArmVerifyGround` call site) |
| `devdoc/requirements/04-sync-and-nodes.md` (row 319) | `internal/config/config.go:322`, `:344` | `:362`, `:404` | `SyncPeers`, `MemoryPeers` |
| `devdoc/requirements/04-sync-and-nodes.md` (row S6) | `internal/config/config.go:322`, `:344` | `:362`, `:404` | same two whitelist keys |
| `devdoc/requirements/07-tools-and-routing.md` | `internal/tools/websearch.go:235`, `:363` | `:226`, `:382` | the two `exec.CommandContext(runCtx, "python3", …)` sites |
| `devdoc/requirements/cli.md` (3 rows) | `cmd/agent/main.go:809` | `:554` | the ONE dispatch-chain `if` (the predicate chain) |
| `devdoc/requirements/cli.md` (3 rows) | `cmd/agent/cli_contracts_test.go:620` | `:739` | `TestCLIContractLegacySpellingsStillDispatch` |
| `devdoc/why-worktree-residue.md` (2 rows) | `cli_tasks_worker.go:638` | `:992` | `defer workerRoundOwnWorktrees()` |

One re-key went **beyond** what the gate demanded, and is reported rather than
hidden: `:363` was not itself a gate failure (that line is not blank), but the doc's
claim is "*executed by the Go tool via `python3` (`:235` and `:363`)*" — and line 363
was `defer os.RemoveAll(tmp)`, i.e. not a python3 exec site at all. It was re-pointed
to `:382`, the merge-script python3 site the claim actually describes. A latent
mis-citation, found and fixed while re-keying its sibling.

**2. One permit ADDED (not a re-baseline): a dated record that must not be
re-keyed.** `devdoc/handoff-20260925-chat-self-observation.md` is a handoff
measured on master `6be7aeff` on 2026-09-25 ("Measured on master `6be7aeff`
(2026-09-25 20:35, clean worktree)"). Its `answer.go:1158` cites the fence/diff guard
`if ls != LinePlain || !hasBoldMarker(line)`, which today sits deeper in the same
file. Re-pointing it would present a 2026-09-25 measurement as today's, so — exactly
like the 222 records already listed — it carries a permit whose reason is the dated
measurement, and the row's own text names the symbol (`rowFor`) as the authority.

**3. Four permits DELETED (the allow-list ratcheted DOWN, 225 → 222 rows).** These
four were stale: three went stale **because of this run's own re-keys** — re-keying a
row removed the parked anchor it cited, turning the permit that excused it into a
permit that matched nothing — and the fourth (`worker-once-vs-task-run-next.md`
`cli_tasks_worker.go:124`) because the line the permit excused was re-keyed to `:143`,
where the named symbol `workerExcludedTags` now really is, so the site is no longer a
problem at all:

- `devdoc/requirements/01-per-module-briefings.md|blank|cmd/agent/cli_verbs.go:7`
- `devdoc/requirements/cli.md|blank|cmd/agent/cli_verbs.go:7`
- `devdoc/review-cli-config-options.md|blank|internal/config/help.go:179`
- `devdoc/worker-once-vs-task-run-next.md|blank|cli_tasks_worker.go:124`

(The two `cli_verbs.go:7` rows and the `cli.md:235` row are three citations of the
SAME parked anchor at `cli_verbs.go:7`, where the `cliVerbs` doc comment now opens at
`:9`. The two that were permits are deleted; the third is inline in a row that a
same-row date kept anchored, so it is listed, not deleted.)

**4. An UNOWNED permit was made owned — the epic's second defect.** The baseline
header named its author as `01a0de1562497081936d1cab2d0946ed`, which resolved to no
ledger row because the ledger spells ids as dashed UUIDs. The header now carries
`01a0de15-6249-7081-936d-1cab2d0946ed` — the same run, and `gopi task show` resolves
it to a `completed` row whose summary is this very gate ("TestDocAnchorsResolveInNormativeDocs
is RED at master 734d8e04"). The header also records this run's id, master tip, and
**what was re-keyed vs what was permitted**, so a reader of the allow-list no longer
has to guess which half of the 225 entries were measured when.

## Files

- `devdoc/hippocampus.md`, `devdoc/hp-push-diagnosis-20260916.md`,
  `devdoc/reclaim-deterministic-checklist.md`, `devdoc/requirements/cli.md`,
  `devdoc/requirements/04-sync-and-nodes.md`,
  `devdoc/requirements/07-tools-and-routing.md`, `devdoc/why-worktree-residue.md`,
  `devdoc/handoff-20260925-chat-self-observation.md`,
  `devdoc/requirements/01-per-module-briefings.md`,
  `devdoc/review-cli-config-options.md`, `devdoc/worker-once-vs-task-run-next.md` —
  anchors re-keyed to their symbol, or a dated record left verbatim.
- `internal/agent/testdata/docanchor-baseline.tsv` — 4 stale permits deleted, 1 date-
  stamped permit added, the author task id corrected to its ledger spelling, and the
  reason for every surviving entry re-measured and stated.
- `devdoc/docanchor-drift-repair-second-pass-20260927.md` — this report. It is the
  SECOND pass: the first (`devdoc/docanchor-drift-repair-20260927.md`, base master
  `c183aab6`) is a committed record of its own run and was left UNTOUCHED.

## An incident this run caused and repaired (reported, not hidden)

While writing this report the first time, I called `write` on
`devdoc/docanchor-drift-repair-20260927.md` — a path I had **assumed was new** — and
mine (170 lines) replaced the FIRST pass's committed report (92 lines, authored by
task `01a0de15-6249-7081-936d-1cab2d0946ed`, commits `259619e2`/`4a3664d8`). Rule 4
("all edits are surgical") and Rule 29's spirit both say a foreign artifact is not
mine to replace, and the file was an existing document this task had never opened.

**Repair, and how it was evidenced:** my text was preserved first
(`git show :<path> > <session-dir>/my-report.md`), then the original was restored
with `git checkout HEAD -- devdoc/docanchor-drift-repair-20260927.md` (verified: 92
lines again, the first pass's header back), and my report was relocated to this new,
non-colliding path. `gopi doccheck` then reported the corpus consistent. The
practical lesson: **a path is not free because it *sounds* like a filename this run
would choose — `git show HEAD:<path>` before `write`, always.**

## Verification

- **Build:** `CGO_ENABLED=0 go build -o /tmp/gopi-docanchor-check ./cmd/agent/` → OK
  (no output, exit 0).
- **Tests:** `go test ./internal/agent/ -count=1 -run 'TestDocAnchor'` → `ok
  github.com/nick/gopi/internal/agent 1.384s` (the gate plus its three sibling
  anti-regression tests). `go test ./internal/standards/ -count=1 -run
  'TestDocManifest|TestDocClass|TestDocBanner'` → `ok github.com/nick/gopi/internal/standards 0.739s`.
  `gopi doccheck` → `doccheck: the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row` (Rule 36, read-only surface).
- **Ran:**
  `go test ./internal/agent/ -count=1 -run TestDocAnchorsResolveInNormativeDocs -v 2>&1 | tail -3`
  — the named gate, verbatim, three times: before any edit (RED), after each batch
  (via an exact-key emitter, see Evidence), and last as the acceptance run.
- **Result:** the acceptance run printed
  `docanchor_test.go:214: doc anchors verified: 2649 across 558 normative docs (2192 explicit + 457 bare continuations; 463 bare out of row/table/heading scope; 222 pre-existing drift entries permitted, 0 new, 0 stale permits)`
  and `--- PASS: TestDocAnchorsResolveInNormativeDocs (1.43s)` / `ok
  github.com/nick/gopi/internal/agent 3.309s` — **0 new, 0 stale permits**, which is
  the acceptance the epic names. The row count agrees: 225 rows at HEAD − 4 deleted +
  1 added = 222 permitted, exactly the tally the gate printed.
- **Commit:** see the branch tip of `task/01a0e2ea91f37e9185e4f99317484247`.

## Evidence (self-filed)

- **Pre-repair RED, verbatim:** `docanchor_test.go:214: doc anchors verified: 2649
  across 558 normative docs (…; 225 pre-existing drift entries permitted, 15 new, 4
  stale permits)` + `--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.90s)`. Full log:
  `/home/user/.gopi/sessions/01a0e2ea-91f3-7e91-85e4-f99317484247/pre-gate.log`.
- **The gate logs full prose for NEW drift but no Key, so the exact key set was
  obtained with a THROWAWAY in-package emitter** (`TestZZEmitDocAnchorKeys`, writing
  `status<TAB>kind<TAB>key<TAB>msg` to `$KEYOUT`), run before / mid / after and
  diffed — memory recipe #5001 STEP 0. Measured: **before** `stale 4, new 15,
  permitted 244`; **after** `0 new, 0 stale`. The two counts differ in KIND, which is
  why they are both quoted: the emitter counts *occurrences* (the same anchor written
  twice in one doc is two lines), while the gate's tally prints `len(permitted)` —
  *distinct keys* — so 245 permitted occurrences over 222 keys is the expected shape,
  and 222 is the number the acceptance line reports. The emitter file was deleted
  before this commit; it is not in the tree.
- **Every re-keyed target verified non-blank by reading the line**, e.g.
  `sed -n '992p' cmd/agent/cli_tasks_worker.go` → `	defer workerRoundOwnWorktrees()`;
  `sed -n '554p' cmd/agent/main.go` → the `if isCompletionSubcommand(dispatchArgs) || …` predicate chain.
- **The two permits deleted because THIS run re-keyed their docs**
  (`01-per-module-briefings.md`, `review-cli-config-options.md`) and the third
  (`devdoc/requirements/cli.md`) were each confirmed stale by the gate's own stale
  report before deletion, and the deletion was confirmed by the acceptance run's `0
  stale permits` — a deleted permit that was NOT stale would have reappeared as new
  drift, and it did not.
- **Ownership of the header's task id measured, not asserted**:
  `sqlite3 /home/user/.gopi/memory.db "select count(*) from tasks where
  task_id='01a0de15-6249-7081-936d-1cab2d0946ed';"` → `1`, and
  `gopi task show 01a0de15-6249-7081-936d-1cab2d0946ed` → `status: completed`,
  summary "TestDocAnchorsResolveInNormativeDocs is RED at master 734d8e04 — 34 doc
  anchors drifted off their symbols (no baseline entry covers them)".

## Not Done (honest)

- **The other 222 surviving permits carry no owner id.** The epic's acceptance is
  "every *surviving* baseline entry traceable to an existing task id with a dated
  measurement reason". What is delivered is: every one of the 222 was **re-measured
  on 2026-09-27 by this run** (the `0 new / 0 stale` tally is the proof that each
  still matches exactly the problem it names), the header now states that provenance
  for the whole list, and **every entry that DOES name an author names an existing
  row** (`01a0de15-6249-7081-936d-1cab2d0946ed`). What is NOT done is stamping an id
  on each of the other 222 — they were inherited from earlier baselines and no ledger
  row is attributable to them without inventing one, which Rule 15 forbids more than
  it forbids an unowned permit. A future pass could stamp them with the row that
  introduced their `kind`, if that can be measured rather than guessed.
- **The four deleted permits were not individually traced to a ledger row** that
  excused them; they were traced to the *site* (the line was fixed or the doc's row
  moved), which is the strictly narrower claim and the one the gate checks. Rule 15's
  "no silent failure" is satisfied by the fact that their removal is stated here with
  the reason per entry, not by a citation I could not produce.
- **No line-shaped permit was added to excuse a file that shrank.** The one permit
  added is for a dated record, and its reason names the date, the master tip it was
  measured on, and the symbol the row cites — not a bare line number.
- **The gate regex, `docAnchorMinChecked` (floor 1200) and the manifest scope were
  NOT touched** — the only non-doc edit in the change is the permit TSV, and the
  anchor census rose (2191 → 2192 explicit) rather than falling.
