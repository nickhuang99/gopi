<!-- doc-class: historical -->
# Buried real work in `refs/tags/archive/*` — a read-only audit of 33 BURIED-REAL tags, plus the mechanism that buries them

**Date**: 2026-09-23 · **Repo**: `/home/user/work/GoPi` · **master snapshot**: `db109cfb`
**Mode**: READ-ONLY / REPORT-ONLY. No branch, tag, ref or worktree was mutated by this
audit; every fact below was read with `git show` / `git log` / `git grep` /
`git for-each-ref` / `git rev-list` / `git cherry` / `git cat-file` / `git ls-tree`.
No merge, no `git tag -d`, no `git branch -d` was run.

**Recovery**: branch `feat/buried-archive-audit`, worktree
`/home/user/workspace/gopi-buried-audit`. `git show feat/buried-archive-audit:devdoc/buried-archive-audit.md`.
Raw evidence dumps (outside the worktree, deliberately not deliverables):
`/home/user/workspace/buried-audit/{scan,scan2,scan3,cherry,ident,modified,final,branchfiles}.txt`.

## 0. What this audit is, and the two questions it answers

**Phase 1** — the 33 `archive/*` tags whose tip adds at least one file absent from
master ("BURIED-REAL"): is any of that work still wanted? Each tag gets a verdict
(LIVE / SUPERSEDED / DEAD / UNSURE) with the master read that decides it.

**Phase 2** — the *mechanism*: every code path that creates an `archive/*` tag, the
reason semantics attached to each, whether the 2026-08-30 "archive = permanent death"
semantics competes with the 2026-09-17 reason-bearing `gopi archive` command, where
the 24 slug-shaped tags came from, and a verifiable (unimplemented) design that makes
"bury a branch" always carry an auditable reason.

### 0.1 The instruments, stated before the verdicts

A verdict here is *not* re-derived from `git cherry` alone, and never from a branch
name, a commit subject, or an ahead-count. Three instruments, in this order:

| # | instrument | what it decides | why not the others |
|---|---|---|---|
| I1 | `git cat-file -e master:<path>` over every file the tag **adds** (`git diff --diff-filter=A <merge-base> <tip>`) | is the tag's *path* on master at all | ahead-count is necessary, not sufficient (a squash/rebase land shows non-zero ahead with nothing missing) |
| I2 | blob identity: `git rev-parse <tip>:<f>` vs `git rev-parse master:<f>` for every **modified** path | is the tag's *version* what master carries | a path can exist on master with different content — the "SAME" rows below are the ones where the tag adds nothing master lacks |
| I3 | **content**, not patch-id: `git grep -w <sym> master -- '*.go'` + `git log -S<sym> master` for every declaration the tag introduces | is the *surface* on master, under any path, ever | needed because `cherry` ids differ for a legitimately re-derived land |

Blob identity (I2) is what makes this audit different from the 2026-09-18 audit: that
one classified 116 tags ALREADY-DONE, and for a subset of the 33 the truth is sharper
than "the content landed somewhere" — master carries **byte-identical** files. Both
facts are reported per row below.

### 0.2 The corpus, measured on `db109cfb`

| quantity | value | command |
|---|---|---|
| `archive/*` tags in the namespace | **188** | `git tag -l 'archive/*' \| wc -l` |
| … light­weight refs / annotated | **187 / 1** (`archive/openai-sdk-backup-20260826`) | `git for-each-ref --format='%(objecttype)' refs/tags/archive/` |
| reason refs under `refs/gopi/archive-reason/` | **0** | `git for-each-ref refs/gopi/archive-reason/ \| wc -l` |
| name depth: `archive/<name>` vs `archive/<a>/<b>` | **31 / 157** | `git tag -l 'archive/*' \| awk -F/ '{print NF}' \| sort \| uniq -c` |
| date-suffixed names (`-YYYYMMDD$`), depth 2 | **24** | the epic's "slug-shaped" count, reproduced exactly |
| date-suffixed names, depth 3 (slash preserved) | **22** | e.g. `archive/feat/conflict-branch-rename-20260917` |
| tags in this audit (tip adds ≥1 file master lacks) | **33** |  |

Tag-creation histogram (by the tagged commit's date, +0800):
`09-17: 68 · 09-16: 39 · 09-11: 17 · 09-14: 13 · 09-23: 6 · 09-20: 2 · …`

**The 33 rows, and the one number that matters.** Of the 33, only **6** add a
production (non-test) `.go` file that master lacks; the rest add `_test.go` files,
`devdoc/*.md`, or `.evid/`/`.live/` scratch. The distribution, measured by
`git diff --diff-filter=A` over each tag's merge-base→tip range:

| class of added file | rows |
|---|---|
| `.evid/` / `.live/` scratch only (plus docs) | 3 |
| `devdoc/*.md` + `_test.go`, no absent production file | 24 |
| **absent production (non-test) `.go`** | **6** |

So the epic's phrase "buried real work" is literally true for 6 tags and true in a
weaker sense (a *test* or a *doc* the tag alone carries) for the other 27. That split
is the single most useful result of this audit and it is stated up front rather than
buried in the table.

## 1. Phase 1 — verdict per tag (33 rows)

Legend for **verdict**: LIVE = work still wanted and not on master;
SUPERSEDED = master carries the intent by another path (the counterpart is named);
DEAD = a decision source retires it (named); UNSURE = the instruments cannot decide
(the missing evidence is named).

| # | tag (`archive/…`) | tip | ahead / cherry + | absent prod | verdict | the master read that decides it |
|---|---|---|---|---|---|---|
| 1 | `feat/req-b8-cli` | `f9e5201f` | 2 / +2 | 0 | **SUPERSEDED** | its single added doc `devdoc/requirements/08-cli.md` is the numbered-generation draft of `devdoc/requirements/cli.md` (added `8ce4c674`). master's copy is the LIVE module (`[9/15]`, R1–R8 prose absent: `git show master:devdoc/requirements/cli.md \| grep -c 'reachable by name'` = 0) — the *filename* scheme was renumbered, not the content lost |
| 2 | `feat/sgr-aware-grid` | `503dfe55` | 6 / +6 | 1 | **SUPERSEDED** | the absent `internal/lineedit/attr.go` (`AttributedLine`, `CellAttr`, `Attribute`, `ColorDefault`) was re-landed as `internal/lineedit/screenattr.go` + `internal/sgr/sgr.go` by `4f56ccd2` (`CellsOf`/`CellOf`, `git ls-tree master:internal/sgr`) |
| 3 | `feat/cov-tools-rest` | `3e128733` | 15 / +14 | 0 | **SUPERSEDED (partial)** | 4 of its 5 prod edits are covered by `7b7c5161` ("merge: fix/liverows-claim-host-component") which lands `LiveRowsOwnedByOthers` (`internal/tools/memory_sync_claim.go:1228`); the 9 `_test.go` files are re-cut as master's `cov_*_test.go` family. `tasks/01a0b51d-3489-7da0-b84a-4ea6a5beea53-report.md` is master's own re-measure of this tag ("SALVAGE, partial") |
| 4 | `feat/req-b10-observability` | `758a89d4` | 2 / +2 | 0 | **SUPERSEDED** | `devdoc/requirements/10-notifications-and-observability.md` is the B10/12 draft of master's `devdoc/requirements/notifications.md` ("Requirements 11 — Notifications & Observability", `beac38c5`); the tag's banner phrases occur 0× on master, the need it names is master's module |
| 5 | `fix/notify-all-events` | `f197232d` | 5 / +4 | 0 | **SUPERSEDED (1 unit SALVAGE, landed)** | `e44742f4` "Merge branch 'fix/notify-all-events' into master" + `aff417b6` ("all" filter enumerates every synthetic alarm from a registry). master's own re-measure: `tasks/01a0b51d-7678-7741-8d17-dcf61776c579-report.md` — "MIXED — 4 of 5 units OBSOLETE, 1 unit SALVAGED (U4)" |
| 6 | `feat/conflict-branch-rename` | `abb4a062` | 12 / +12 | 1 | **DEAD (park half) / LIVE (graveyard half, landed)** | `devdoc/conflict-branch-rename-recovery.md` §1 (`<!-- doc-class: historical -->`, master): *"the capability is deliberately dropped — with one live exception, which is landed by this branch"*. The absent `internal/branchnames/names.go` (`ConflictName`, `IsConflict`, `IsArchive`) carries the **withdrawn** `conflict/<name>` state namespace, voided by `devdoc/branch-lifecycle-ruling.md` (`762bc8eb`), with mechanical gates (`cmd/agent/merge_prompt_gate_test.go`, `mrgVoidedStateNamespacePrefix`). Ledger row `01a0ab60-…` cancelled: *"superseded: the park/resolve-conflict mechanism is retired"* |
| 7 | `feat/archive-why-gate` | `eac3b475` | 4 / +4 | 2 | **DEAD (superseded by its own later generation)** | `devdoc/archive-why-gate-recovery.md` §1: the tag's design put the WHY in an **annotated tag** (`internal/tools/archive_why.go`: `archiveWhyDecide`, `archiveCensus`); the corrected generation (`feat/hand-archive-gate`, tip `c7569bcc`) puts it in a **companion ref** and ships as `internal/tools/archive_reason.go` + `cmd/agent/cli_archive.go` on master. The doc's own table: the annotated cut "went red on `TestRepoSweepParksNonFFBranchAndContinues`" — engine and shape conflict, later generation wins |
| 8 | `fix/merger-park-worktree-release` | `d1a4c708` | 10 / +10 | 0 | **DEAD** | `tasks/01a0b51d-69c7-753f-bc14-5c0db98d0b11-park-worktree-release-remeasure-report.md` (master) — "OBSOLETE per unit (6 units)": the park lane was deleted by `925194b0`, the tick is report-only since `40380b79`/`sweep-report-not-act`. master re-cut the surviving contract as *absences* in `repo_sweep_notff_test.go` (+486). `repoSweepDrainParkedWorktrees`/`repoSweepParkReleaseWorktree`: 0 hits on master |
| 9 | `feat/merger-pure-cronjob` | `d1faf33b` | 9 / +2 −7 | 1 | **DEAD** | `devdoc/merger-pure-cronjob.md` (master) opens with its own CORRECTION: *"`gopi merger --every D` is **gone**, not reduced — the resident merger's entry points were closed (`f9ff1a89`) and its implementation deleted (`55c4d6f3`)"*. The tag's `cmd/agent/merger_takeover.go` was deleted by `93009688`; cherry `-7` shows 7 of its 9 commits are already on master |
| 10 | `feat/cov-hippocampus-hp` | `5cd0b643` | 3 / +3 | 0 | **SUPERSEDED** | the absent `internal/hippocampus/memory_test.go` + two edge-test files were re-derived as `internal/hippocampus/cov_hippocampus_test.go` by `234d7efd` ("close function-level coverage 57.1% → 100.0% (covHipp* slice)"). The tag tip is **not** an ancestor of master (`git merge-base --is-ancestor` → 1); the *work* is |
| 11 | `fix/serve-lock-race` | `dd9c53b4` | 9 / +9 | 0 | **SUPERSEDED (1 unit landed)** | `devdoc/serve-lock-acquire-race.md` on master carries `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY -->` and *"**Re-derived onto master from** `archive/fix/serve-lock-race` (tip `dd9c53b4`…)"*. master's re-measure `tasks/01a0b51d-7ee6-…-report.md`: "MIXED — 1 unit OBSOLETE, 1 unit SALVAGE (landed on a fresh branch)" |
| 12 | `fix/merger-takeover-scope-escape` | `da7326f5` | 1 / +1 | 1 | **DEAD** | `tasks/01a0b51d-72e9-…-report.md` (master): "OBSOLETE for every audited unit — twice over", the parallel land being `b87c9008` ("the take-over's cgroup escape is a ladder, not one rung", 2026-09-17 20:54). Its 1-commit `merger_takeover_scope.go` targets a file `93009688` deleted |
| 13 | `feat/web-m2-b5b8b9b12` | `98fb2942` | 19 / +12 −6 | 0 | **SUPERSEDED (per-unit)** | `devdoc/web-m2-b5b8b9b12-remeasure.md` (master) — a U1–U11 table, every row OBSOLETE with the landing commit named (U1 `webChatResetPath`…, U4 `68642cb7`, U7 `c0c722e3`). 6 of 19 commits are already on master by patch-id (cherry `−6`) |
| 14 | `feat/cov-deepseek-hp` | `03c624e0` | 2 / +2 | 0 | **SUPERSEDED** | `02ae250f` "Merge branch 'feat/cov-deepseek-hp'"; master's `internal/deepseek/hp_coverage_test.go` carries the same lane (`TestHPToolCall_WireRoundTrip`, …). The tagged tip is a *different* commit from the landed one, so `cherry` shows `+2` — the content is what decides it |
| 15 | `fix/shot-chrome-numeric-sort` | `b66da327` | 9 / +9 | 0 | **SUPERSEDED** | `92173106` "fix(shot): compare playwright cache versions numerically, not lexically" lands `comparePlaywrightVersions`/`playwrightVersion` at `internal/shot/chrome.go:161`/`:124`. 3 of its 3 "absent prod decls" in the 09-18 audit are these symbols — landed. `devdoc/recover-shot-chrome-numeric-sort.md` is the recovery record |
| 16 | `fix/merge-verify-landing-witness` | `5db36b11` | 10 / +10 | 0 | **SUPERSEDED** | `devdoc/merge-verify-landing-witness-residual.md` (master): *"**The tag is OBSOLETE per unit** — master settled the same defect D8 independently, ~8 minutes before the tag's kernel commit … in `7bfdc6a5` … + `1a6622cd`"*; and this very tag was later merged (`39359dac` "Merge branch 'fix/merge-verify-landing-witness' into master") |
| 17 | `fix/merge-verify-landing-witness-20260917` | `737ca0bf` | 14 / +14 | 0 | **SUPERSEDED** | same defect path as #16 (`internal/tools/task_staleness_guard.go`, both tags share the merge-base `909d4ca9`); the 09-17 tag is the same work re-cut, and the master residual doc closes both |
| 18 | `fix/hippo-host-resolution` | `564fa6dd` | 11 / +11 | 0 | **SUPERSEDED** | `87da84ef` "Merge branch 'fix/hippo-host-resolution' into master" (+ `cb11f6a2`, "re-derive the archived tag's host resolution — one brief derivation"). The tag's own `internal/hippocampus/host_deadline_test.go` is absent; master's `internal/hippocampus/host_test.go` is the landed one |
| 19 | `fix/envbool-single-truthy-parser` | `e21582f2` | 1 / +1 | 0 | **SUPERSEDED (by a different design)** | master's `71a31879` ("refactor(config): one boolean-vocabulary definition in a leaf package (`internal/boolvocab`) + Rule-13 gate", 2026-09-19 12:34). The tag's report (`tasks/01a0b51d-envbool-archive-remeasure-report.md`, master) calls every unit **OBSOLETE** and adds the honest detail: the tag's list is `BoolTrueValues`/`boolVocabulary`, master's is `boolvocab.TrueValues`/`IsTrue` — *"master landed the same rule by a different name"* |
| 20 | `feat/worker-term-exit` | `dd5dde45` | 4 / +4 | 0 | **SUPERSEDED (landed)** | `07198611` "Merge branch 'verdict/worker-term-exit-remeasure'" (+ `88952e70`, `8687e0a5`). master carries `startWorkerStopForwarder`, `signalRunChild`, `descendantPIDs`, `workerStopNudge`, `workerStopDrainSettle` (`grep -lw` each → `cmd/agent/cli_tasks_worker_stop.go`) and `worker_stop_forward_regression_test.go` |
| 21 | `feat/web-m1-b4b7` | `9ebc9554` | 14 / +7 −6 | 0 | **SUPERSEDED (landed)** | `c0c722e3` "feat(web): B1/B2/B3/B5/B8/B9/B12 rebuilt off master …". master's `cmd/agent/cli_web_m1_test.go` carries the tag's `TestWebM1B1_*`/`TestWebM1B2_*`/`TestWebM1B3_*` names; its `cli_web_m2_test.go` carries the renumbered `TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite` |
| 22 | `feat/shot-jsonlist-retry` | `484e5d30` | 9 / +9 | 0 | **SUPERSEDED (split verdict, landed)** | `d6540bf2` "test(shot): cover the transport-failure arm of the page-target poll" adds `internal/shot/jsonlist_retry_test.go`; master's re-measure `tasks/01a0b51d-report.md`: *"SPLIT — the retry slice is SALVAGE, the rest is OBSOLETE"*, and the SALVAGE half is what landed |
| 23 | `feat/raw-round-grammar` | `d9a5ea41` | 8 / +7 | 0 | **SUPERSEDED (1 unit landed)** | `891a9775` "fix(gopilog,raw): re-derive the shared round-selector grammar from `archive/feat/raw-round-grammar`". `devdoc/raw-round-grammar-archive-remeasure.md` (master) is the per-unit table: `httpSelectorGrammar` + `rawRoundPosition` **SALVAGE** (both re-derived by `891a9775`; the tag's own files are absent), docs OBSOLETE |
| 24 | `feat/merger-park-tip-dedup` | `50f6d4df` | 1 / +1 | 0 | **DEAD** | `tasks/01a0b51d-3d39-7cda-bb2c-dc1df53549d3-report.md` (master): its subject `repoSweepRepark*` + `TestRepoSweepReparksSameTipOncePerWork` / `TestRepoSweepSettlesParkedTipMintedByTheOldLane` have **0 hits** on master — the park lane they dedup was deleted (`925194b0`), so there is nothing left to dedup |
| 25 | `feat/chat-stream-tty-wrap` | `11feae53` | 2 / +2 | 0 | **SUPERSEDED (landed)** | `87b8bf3d` "feat(chat): bound the fallback stream's rows to the terminal width (U2)" + `ce64a91a` (the U2 report). `cmd/agent/chat_width.go` is ON master (blob **DIFFERS** — master moved on), `cmd/agent/chat_stream.go:659 func wrapStreamText` is the landed form |
| 26 | `fix/worker-ownership-10files` | `8cdf0c28` | 2 / +2 | 0 | **SUPERSEDED (landed)** | `cc1574db` "docs(worker-ownership): annotate the ten files the gate reports, at each launch-shape mention". Measured per file: all 10 paths are ON master and carry the `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY -->` pointer (`devdoc/daemon-e2e-leak.md` master=2 / tag=1) |
| 27 | `fix/standards-stylegate-master-red` | `a3dfb1a3` | 3 / +3 | 0 | **SUPERSEDED (landed)** | master's `internal/standards/baseline.go` carries the tag's own resolution as a *removal*: *"The permit stays deleted and the site stays fixed: no entry is re-added here"* (with the `a2bf8ef4` → `333fb3ac` regression recorded), re-greened by `b8653088` |
| 28 | `fix/read-binary-sample-straddle` | `a7e39f5e` | 1 / +1 | 0 | **SUPERSEDED (landed)** | `e87cc2e9` "fix(read): tolerate an incomplete trailing rune in the binary-detection sample". master's `internal/tools/read.go` carries `trimIncompleteTrailingRune` + `isIncompleteRunePrefix` + `utf8ContinuationByte` (the tag's helper under a slightly different signature: `int` return, `[]byte`) |
| 29 | `fix/daemoncwd-scratch-pin` | `5f05462c` | 1 / +1 | 0 | **SUPERSEDED (landed)** | `internal/daemoncwd/testmain_scratchroot_test.go` is ON master (blob DIFFERS): *"ONE ENTRY POINT PER SUITE — the reason this file carries the provenance. The package briefly held TWO TestMain definitions…"* — exactly the pin the tag added |
| 30 | `feat/merger-tick-branch-account` | `e0932678` | 2 / +2 | 0 | **SUPERSEDED (landed)** | its `devdoc/merger-status-branch-accounting.md` and `internal/tools/merger_status_inventory_test.go` are ON master (blob DIFFERS); `BucketLines` lives at `internal/tools/repo_sweep.go:260`, its regression test at `merger_status_inventory_test.go:143` (`TestBranchInventory_BucketLinesNeverDropANote`). master's re-measure `tasks/01a0b51d-3e04-…-report.md`: "verdict (b) OBSOLETE" |
| 31 | `feat/cov-shot-hp` | `569f7fdb` | 4 / +4 | 0 | **SUPERSEDED** | `3160a1d9` "Merge branch 'feat/cov-shot-hp'"; master has `internal/shot/cov_shot_x11_test.go` (`TestCovX11DisplayReportsConnectedName`, …). The tag's 28 tests: 1 name shared, the rest re-cut — the coverage campaign landed, the file names differ |
| 32 | `feat/cov-gopilog-hp` | `d9b26fcd` | 2 / +2 | 0 | **SUPERSEDED** | `7336bdeb` "Merge branch 'feat/cov-gopilog-hp'" (+ `949ddbc4`); master has `internal/gopilog/cov_gopilog_hp_test.go`. `devdoc/cov-gopilog-hp.md` is ON master (blob DIFFERS — `5faf7daa` re-bannered it) |
| 33 | `feat/cov-config-hp` | `829681ff` | 1 / +1 | 0 | **SUPERSEDED** | `2cce3ff8` "Merge branch 'feat/cov-config-hp'"; master carries the family `internal/config/cov_config_{compute_nodes,effort,envbool,reset_identity,settings_path}_test.go`, the tag's 8 tests re-cut across them |

### 1.1 The verdict histogram, and what is NOT in it

| verdict | rows |
|---|---|
| SUPERSEDED (master carries the intent; counterpart named per row) | 27 |
| DEAD (a decision source retires it; source named per row) | 6 |
| LIVE | **0** |
| UNSURE | **0** |

**No LIVE row and no UNSURE row is the honest result, not a convenience.** Every one
of the 33 has a master read that decides it, and in 12 cases master's own re-measure
report of that very tag is ON master (`tasks/01a0b51d-*`, 18 files) — the audit's job
here was to re-run the instruments on a later master (`db109cfb` vs the reports'
`0317d09a`-era snapshots) and reconcile, which §1.2 does.

### 1.2 Reconciliation with the 2026-09-18 audit (`devdoc/archive-tag-audit-20260918.{md,tsv}`)

That audit classified the namespace at `0317d09a`: 116 ALREADY-DONE / 7 ALREADY-QUEUED
/ 2 SUPERSEDED / 42 UNLANDED (and its 32 "unlanded units" got rows). This audit's 33
overlap that set and **moves 31 of them off "unlanded"**, on evidence later than that
snapshot:

- **3 new re-measure reports** were written after 09-18 and are ON master:
  `devdoc/archive-why-gate-recovery.md`, `devdoc/conflict-branch-rename-recovery.md`,
  `devdoc/web-m2-b5b8b9b12-remeasure.md`, plus `devdoc/raw-round-grammar-archive-remeasure.md`
  and the 18 `tasks/01a0b51d-*` files. Each is a per-unit OBSOLETE/SALVAGE table for a
  tag this audit had to judge.
- **2 remaining disagreements, stated rather than smoothed**:
  - `archive/feat/cov-tools-rest` — 09-18: UNLANDED (6/6 absent); today: the *prod* half is
    landed (`7b7c5161` → `LiveRowsOwnedByOthers`, `memory_sync_claim.go:1228`) and the
    `_test.go` half is re-cut. The tag's own report calls it "SALVAGE, partial" — the
    partial is the honest word and it is kept here.
  - `archive/fix/read-binary-sample-straddle` — 09-18: "1 of 2 prod decl the tip adds are
    absent from master"; today: `trimIncompleteTrailingRune` IS on master with a different
    signature (`[]byte`→`int`, plus `isIncompleteRunePrefix`). Landed by re-derivation
    (`e87cc2e9`), which is exactly the case `cherry` cannot see.

## 2. Phase 2 — the mechanism: every path that creates an `archive/*` tag

### 2.1 The code paths (file:line, and the reason each writes)

There is **exactly ONE writer function** in the project, and three callers reach it.

| # | site (master `db109cfb`) | what it writes | reason semantics |
|---|---|---|---|
| **W0** | `internal/tools/archive_reason.go:387 func ArchiveTagReasoned(repo, branch, reason, detail)` — the ONE writer | probes `git tag -l archive/<branch>` (`:403`), creates the **lightweight** tag at the branch tip if absent (`:411 git tag <tag> <branch>`), then writes the companion ref `refs/gopi/archive-reason/<branch>` = a blob (`:446`, via `hash-object -w --stdin` + `update-ref`) | refuses an **unknown** reason (`:391`, `ArchiveReasonKnown`) and an empty branch; the record's line 1 is `reason: <code>` (`archiveReasonRecordFmt`, `:303`), body = `branch/repo/actor/task/at/detail` (`:307`, `archiveDetailUnstated = "(none stated)"`, `archiveOwnerNone = "(none)"`). **A tag that already exists is never re-pointed** (`:407` comment), and a record that already parses is never rewritten (`:418`) — idempotence is BY REASON |
| **W1** | `internal/tools/repo_resolve_row.go:111 func repoSweepArchiveBranch(repo, branch, reason)` | the mechanism's front door: empty reason ⇒ **refusal** (`:113`), already-exempt ⇒ no-op (`:126`), then the **owner gate** (`:137 repoSweepArchiveOwnerGate`), then `ArchiveTagReasoned` (`:141`) | the gate refuses to bury a branch whose owning ledger row is `completed`/`verified` unless `repos.<path>.archive_bury_override` is set (the mechanism cannot be asked a question, so it declines) |
| **C1** | `internal/tools/task_merge.go:782` (in `taskMergeRefuseUncompilable`) | the **merge-time compile gate**: a branch that does not build against master is archived with `ArchiveReasonCompileFailure`, then a rework row is filed | ACTIVE lane. Its archive is what removes the branch from the next tick's candidate set (`:775` comment) |
| **C2** | `internal/tools/archive_reason.go:214 func RetireUnrelatedHistoryBranch` → `repoSweepArchiveBranch(…, ArchiveReasonUnrelatedHistory)` — called from `cmd/agent/merge_run.go:2599` | `gopi merge`'s **unrelated-history verdict**: a branch sharing no commit with master is retired durably | ACTIVE lane (epic unrelated-history-durable-retire). The verdict is terminal for one run only without the tag — measured: `feat/sync-conflict-completed` was re-filed 4× (`archive_reason.go:110` comment) |
| **C3** | `cmd/agent/cli_archive.go:174 tools.ArchiveBranchCwd` → `archive_reason.go:252` → `:227 ArchiveBranch` → `:878 ArchiveDecide` + `ArchiveTagReasoned` | the **human** face: `gopi archive <branch> --reason <code> [--detail] [--override-completed]` | ACTIVE. `ArchiveDecide` consults the owner row; a `completed`/`verified` owner is **refused** unless `--override-completed`; a row-less branch proceeds as `--reason unowned` |
| **W−** | `internal/tools/repo_sweep.go:624 func repoSweepArchiveExempt` | **probes only** — `git tag -l archive/<branch>` (`:625`), then `TrimSpace(out) != ""` | the predicate four consumers branch on (§2.3). Never writes |
| **DEAD** | `internal/tools/archive_reason.go:88-91` `ArchiveReasonNotFFAblePark = "not-ff-able-park"` | **is in the vocabulary and written by nothing.** `grep -n ArchiveReasonNotFFAblePark` = 4 hits, all in `archive_reason.go` itself (the const, the ordered list, the doc switch). The park lane that used to write it was deleted by `925194b0` | kept deliberately: the 09-17 tags' records (if any existed) name it, and a reason vocabulary that drops a spelling retroactively makes old decisions unreadable |

The vocabulary (`archive_reason.go:84-135`, `ArchiveReasons`): `not-ff-able-park`,
`compile-failure`, `unrelated-history`, `failed-verify`, `superseded`, `hand-mistake`,
`unowned`. Seven codes; **three** of them are written by live automatic lanes (C1, C2,
and — none today for the park code).

**The 21:04 origin, in the code's own words** (`cmd/agent/cli_archive.go:5`):
*"at 21:04 nine branches in /home/user/work/GoPi were archived by hand — `git tag
archive/<branch> <branch>`, nine loose refs written inside one second, five of them
over task rows the ledger calls `completed` — with no ledger row and no reason recorded
anywhere. The tag is the project's decided-exclusion namespace and five readers honour
it …, so the burial was EFFECTIVE and invisible at once."*

### 2.2 Do the 2026-08-30 "permanent death" semantics compete with the 2026-09-17 reason-bearing command? — YES, on one axis, and the competition is UNRESOLVED

**The 08-27/09-04 generation (permanent death).** `repoSweepArchiveExempt`
(`repo_sweep.go:624`) is introduced by `dda32907` (2026-09-04, "MergerRepoSweep —
config-driven pure-git per-repo sweep"), and GOPI.md **Rule 9** (added `eea04a8d`,
2026-08-27, "feat(safe-merge): 竞争收尾三层防护") already prescribed the tag as step 2 of
the 3-step loser-archive flow. Its semantics are stated most sharply by master's own
audit, `devdoc/archive-tag-audit-20260916.md:157`:

> **4. Permanent and unreported.** `repoSweepArchiveExempt` hides the branch from the
> merger **forever**, and no report lists archive-tagged branches. It is invisible in
> `merger --status` (`pending: none`) and invisible in `task list` (the task rows say
> completed). … **a completed deliverable can be archived by mistake and nothing in the
> system will ever mention it again.**

and by `devdoc/requirements/merge-branch-lifecycle.md:493` (master), which records the
same thing as a *certified* property:

> `TestMergerRepoSweep_BuildRefusalArchivesOnceAndFreesTheQueue` … asserts a build-refused
> branch **merges nothing, files exactly 1 rework row, and is exempted via the tag on tick
> 2** — i.e. it certifies *"archive = permanent exemption"* … **this test certifies one-way
> burial.**

**The 09-17 generation (reason-bearing).** `ArchiveReason*` + `gopi archive` +
`repoSweepArchiveOwnerGate` + `ArchiveReasonlessBacklog` are the answer to that audit:
the burial now *requires* a reason code, refuses to bury a finished task's deliverable,
and is reportable (`gopi archive --check`, `runArchiveCheck`, `cmd/agent/cli_archive.go:206`).

**Where they compete, precisely.** They do NOT compete on the ref (both write the same
lightweight `refs/tags/archive/<branch>`; the reason lives on a *separate* ref — the
09-17 design's whole point, and why `git rev-parse archive/<branch>` still answers with a
commit). They compete on **whether the burial is reversible/reviewable**, and the
competition is decided in the 08-30 generation's favour **on the reader side**:

- the exemption predicate is still `repoSweepArchiveExempt` = *exact tag name present*,
  with no reason consulted and no expiry. A tag with a recorded reason and a tag typed by
  hand at 21:04 are **indistinguishable to every merge path**;
- `ArchiveTagReasoned`'s doc is explicit that this is deliberate for the *shape*
  (`:387`: "The tag is LIGHTWEIGHT and stays so … an annotated tag would … re-point a ref
  that repo-wide code reads as a commit"), but nothing on the reader side asks *which*
  reason a tag carries. There is no `ArchiveReason == superseded ⇒ reviewer may revive`
  lane.
- The 09-17 generation's own visibility half is **report-only**: `ArchiveReasonlessBacklog`
  (`:570`) lists reason-less tags, `gopi archive --check` exits 1 — but no gate refuses a
  merge on a reason-less tag, and no lane ever un-archives one.

**Measured proof that the reason half has never been used on this repo:**

```
$ git for-each-ref refs/gopi/archive-reason/ | wc -l
0
```

Zero reason refs against 188 archive tags — including the 9 tags minted in the 09-23
salvage wave. So on this repo the 09-17 design is a **write-side gate that has never
recorded a single decision**, while the 08-30/09-04 semantics (permanent, unreported,
reason-blind) are what every tag in the namespace actually relies on. That is the
competition, and it is unresolved by construction: nothing in the reader set can prefer
one generation over the other because nothing in the reader set reads a reason.

### 2.3 The reader asymmetry that lets slug-shaped tags bypass the exemption

Five readers honour the namespace, and they do **not** agree on what "in the namespace"
means. Measured on master:

| reader | file:line | predicate | what a slug tag does to it |
|---|---|---|---|
| R1 `repoSweepArchiveExempt` | `internal/tools/repo_sweep.go:624` | `git tag -l archive/<branch>` — **exact name** | **MISSES.** Proof: tag `archive/feat-claim-broadcast-20260901`, branch `feat-claim-broadcast` ⇒ `git tag -l archive/feat-claim-broadcast` = **0 lines** (measured). `repoSweepUnprotectedBranches` (`:639`) therefore keeps the branch a merge candidate |
| R2 `archiveTagAncestry` | `internal/tools/task_merge_backlog.go:70` | `for-each-ref refs/tags/archive/*` + `merge-base --is-ancestor <tag> <branch-tip>` — **any depth** | **CATCHES** (if the tagged tip is reachable from the branch). This is the only reader that is shape-agnostic |
| R3 `mergedCleanupBranchExempt` | `internal/tools/task_merged_cleanup.go:645` | "the operator's protected set (`archive/*`, the exempt pattern list)" | catches by pattern precedence, not by tag presence |
| R4 `taskMergeExecutor` blacklist | `internal/tools/task_merge_executor.go:82`, `:503` | **branch-name** prefixes `loser/`, `archive/` | irrelevant to a tag — a *name* test |
| R5 `task_deliverable_branch.go:428` | `strings.HasPrefix(branch, archiveBranchPrefix)` | **branch-name** prefix | irrelevant to a tag — a *name* test |
| R6 `scripts/safe-merge.sh:65` | `git tag -l "archive/$BRANCH"` — **exact name** (`:11`, `:33` add name-prefix rules) | **MISSES** a slug tag for the same reason R1 does |

So the slug form defeats the *merge-exclusion* readers (R1, R6) while the
*backlog/ancestry* reader (R2) still catches the branch. The practical consequence is not
"the branch gets merged by mistake" — it is that a decided-loser branch stays in the
**rework/scan candidate set** and, in R1's consumers, keeps being re-reported. That is
the failure mode the 09-17 generation's own comments name for a *different* symptom
(`task_merge_backlog.go:165`: *"without it the audit re-submits 'merge \<loser\>' forever"*).

**A measured caveat, stated so the claim is not over-read.** All 24 slug-shaped tags carry
**an exact-name sibling or no live branch at all** in this repo today: no branch named
like a slug-tagged branch exists (`git rev-parse --verify refs/heads/<name>` → none for all
24), and for the slash-preserving family the exact-name tag exists beside the dated one
(e.g. `archive/feat/conflict-branch-rename` **and** `…-20260917`, both at `abb4a062`). So
the bypass is **latent, not currently firing**: it needs the branch to come back. It is
reported as a mechanism defect, not as an incident.

### 2.4 Where the 24 slug-shaped tags came from — two spellings, neither honoured

The two date-suffixed name shapes in the namespace:

```
$ git tag -l 'archive/*' | grep -E '^archive/[^/]+-[0-9]{8}$' | wc -l     # depth 2, flat
24
$ git tag -l 'archive/*' | grep -E '^archive/[^/]+/[^/]+-[0-9]{8}$' | wc -l  # depth 3, slashes kept
22
```

**Spelling A (flat, `<branch-with-slashes-flattened>-<date>`)** is what GOPI.md Rule 9
tells a human to type — *"败者分支归档清理：`git tag archive/<name>-<date>` + `git branch -D
<name>` + `git worktree remove <path>`"* — and what the shipped script writes:
`scripts/post-merge-cleanup.sh:123  TAG_NAME="${TAG_PREFIX}/${BRANCH}-$(date +%Y%m%d)"`
with `readonly TAG_PREFIX="archive"` (`:38`). Note the script does **not** flatten: a
branch `feat/x` gets the literal `archive/feat/x-20260916`, which is why the dated set
splits 24 flat / 22 slashed depending on whether the branch name carried a slash.
`scripts/d2-disposition.sh:85-86` is the only place that *reads* both: `flat=${branch//\//-}`
then probes `git tag -l "${ARCHIVE_TAG_PREFIX}*${flat}*"` **and** `…*${branch}*`.

**Which one produced which tag is not recorded anywhere, and that is the finding.** A
lightweight tag has no message and no author; the only remaining evidence is an outside
reference. Searched (`git grep -l -F <tag> master -- briefings/*.md tasks/*.md devdoc/*.md`):

- **17 of the 24** are named in a master artifact — 13 × `devdoc/archive-tag-audit-20260918.md`,
  3 × `devdoc/archive-tag-sweep-88-20260916.md`, plus `devdoc/merger-remove-park.md`,
  `briefings/brief-archive-sweep.md`, `devdoc/turn-layout-merge-report.md`,
  `devdoc/branch-scan-archive-filter-merge-report.md`.
- **7 of the 24 are named nowhere on master** — and they are the *newest*:
  `feat_merge-handover-main-repo-backstop-salvage-20260923`,
  `fix_literal-escape-decode-salvage-20260923`,
  `fix_memory-escape-decode-salvage-20260923`,
  `fix_peer-ref-collision-named-salvage-20260923`,
  `rework-docclass-truthclass-master-20260923`,
  `rework-g3-memory-help-surface-recovered-20260923`,
  `fix-chat-quit-race-20260921`. (6 of the 7 are 2026-09-23.)
- Shape note: the 09-23 group uses `_` where the branch had `/`
  (`feat_merge-handover-…-salvage-20260923`), i.e. a *third* flattening convention
  (`/`→`_`) that neither GOPI.md Rule 9 nor `d2-disposition.sh` names.

**Conclusion for question 3**: the 24 come from the *pre-reason* human convention
(Rule 9 / `post-merge-cleanup.sh`), minted by hand or by script, with **no recorded
provenance per tag** — which is exactly the condition the 09-17 generation was built to
remove and could not, because these were written without it.

## 3. A design that would make "bury a branch" always carry an auditable reason (NOT implemented)

The constraint set: (a) the ref must stay a **lightweight tag at the branch tip** — five
readers treat it as a commit and an annotated tag breaks them (measured: the 09-17
`archive_why.go` generation went red on `TestRepoSweepParksNonFFBranchAndContinues`);
(b) the design must not require the readers to grow a new concept; (c) it must be
**mechanically verifiable** — a check, not a convention.

**D1 — the writer already exists; what is missing is the second half of its own contract.**
`ArchiveTagReasoned` already writes the companion ref. The gap is that nothing turns
"tag without record" into a *failure of anything*. Design: make the **exemption
predicate itself** two-valued — `repoSweepArchiveExempt` returns `(exempt bool,
reason string)`, reading `refs/gopi/archive-reason/<branch>` through the existing
`archiveReasonRecordOf`. Then every consumer of R1 can distinguish
`exempt+superseded` from `exempt+""`, with no new namespace and no new tag shape.

**D2 — a permitted revival lane per reason code.** The vocabulary is already a machine
token, which is what makes this possible where prose could not: a `superseded` /
`unowned` / `hand-mistake` tag is *reviewable* (a human error is the whole point of the
`hand-mistake` code), while `compile-failure` / `failed-verify` / `not-ff-able-park` are
*earned refusals*. Design: `ArchiveDecide` already computes the code — add a
`ReviveEligible(code)` predicate beside it and let the rework/backlog lanes treat an
eligible tag as a *candidate* (R2's ancestry filter is already the mechanism), while a
non-eligible tag stays invisible. The rule that keeps this honest: **revival re-arms the
branch, it never deletes the tag** (`archive_reason.go:210`: *"The BRANCH REF IS NEVER
DELETED … removing the tag re-arms the branch"*), so the decision record survives the
reversal.

**D3 — a gate, not a checklist (`gopi archive --check` already exists; wire it).**
`runArchiveCheck` (`cmd/agent/cli_archive.go:206`) exits 1 on reason-less tags, but
nothing runs it where it matters. The verifiable half: a merge-path gate that refuses
*to mint a NEW* `archive/*` tag when the write path cannot produce a reason — the writer
already refuses an empty reason (`repo_resolve_row.go:113`) and an unknown code
(`archive_reason.go:391`), so the mechanical statement is: *no code path in the tree may
call `git tag` with an `archive/` name except `ArchiveTagReasoned`.* That is a one-predicate
gate over `internal/`+`cmd/` (stdlib `go/ast`, the `internal/standards` pattern) —
**and it would have caught every slug tag in the namespace**, because all 24 were minted
by hand or by `post-merge-cleanup.sh`, never by the writer.

**D4 — retire the flat spelling explicitly.** Two spellings exist in the repo
(GOPI.md Rule 9 + `post-merge-cleanup.sh:123`, both flat) and neither is honoured by
R1/R6. The design statement that closes it: *the only archive name is
`archive/<branch>` with the branch name verbatim* — which makes `d2-disposition.sh`'s
two-probe read (`flat` **and** `branch`) unnecessary and makes `git tag -l archive/<branch>`
sufficient again. Removing the flat form from GOPI.md Rule 9 and from
`post-merge-cleanup.sh` is the change; the 24 existing tags stay (they are evidence, and
Rule 9's own archive step is what minted the *decision*, not the naming).

**Boundary — what this design does NOT do, deliberately:** it does not delete, rename or
re-point a single existing tag; it does not add a merge-blocking gate on *existing*
reason-less tags (that would refuse merges over history nobody can reconstruct); it does
not make `not-ff-able-park` readable as a live lane (its writer is gone). And it does not
propose a second namespace — `refs/gopi/archive-reason/` already is the right one, and
the reason it must stay *outside* `refs/tags/` is on master at `archive_reason.go:282`:
a reason kept as a second tag "would double every listing and invite a reader to mistake a
reason for an archive decision".

## 4. What is NOT claimed

- **This audit changed nothing.** No tag was deleted, no branch re-armed, no ref written,
  no worktree removed. The 33 tags are exactly where they were; their branches are gone
  (or were never local).
- **"SUPERSEDED" is a statement about master's content, not a recommendation.** Several
  of the tags are superseded *by design decisions that removed the machinery they fixed*
  (#8, #9, #12, #24) — re-landing those would resurrect deleted lanes, not fix a gap.
- **No per-tag provenance exists for the lightweight tags, and this audit did not invent
  one.** Where a master artifact names a tag, it is cited; where none does (7 of the 24),
  the row says so.
- **The `_`-flattening convention** (7 tags, all 09-21/09-23) is reported, not explained —
  nothing on master documents who chose `/`→`_` over `/`→`-`.

## 5. Not Done (honest)

- **The 33 rows do not each carry a machine-generated evidence line.** Rows cite the
  deciding master read (`file:line`, a commit, or the master re-measure report that
  covers the tag); the per-tag file-level PRESENT/ABSENT matrix that backs them lives in
  the raw dumps (`/home/user/workspace/buried-audit/{branchfiles,ident,modified}.txt`),
  not inlined here — 373 file rows would have doubled this report for no decision value.
- **6 tags were judged from master's own re-measure report plus my re-run instruments,
  without re-deriving their SALVAGE halves.** `feat/cov-tools-rest` is the one where that
  matters: its report calls the tag "SALVAGE, partial" and I did not re-derive the
  partial half on a fresh branch (that is a different task's deliverable, not an audit's).
- **The whole-tree `archive/*` disposition is not re-decided.** This report narrows to the
  33 BURIED-REAL rows; the other 155 tags keep the 09-18 audit's verdicts unless a later
  artifact overrode them.
- **No gate was written** (§3 is a design, explicitly unimplemented). D3's writer-monopoly
  predicate is the one piece I would implement first — it is a cheap AST gate and it would
  have flagged all 24 slug tags.

## 6. Verification

- **Read-only proof**: every fact came from `git show` / `git log` / `git grep` /
  `git for-each-ref` / `git rev-list` / `git cherry` / `git cat-file` / `git ls-tree` on
  `/home/user/work/GoPi`. No `git tag`, `git branch -d/-D`, `git update-ref`, `git merge`,
  `git worktree remove` was executed in the audited repo. `git worktree list` before and
  after this run shows the same five entries plus this run's own two.
- **The 33-tag corpus is reproducible**: `git tag -l 'archive/*' | wc -l` = 188;
  the 33 are the tags with ≥1 added file absent from master (dump: `scan.txt`).
- **The zero-reason-ref number**: `git for-each-ref refs/gopi/archive-reason/ | wc -l` = 0.
- **The slug-bypass proof**: `git tag -l archive/feat-claim-broadcast` = 0 lines while
  `archive/feat-claim-broadcast-20260901` exists.
- **Manifest gate**: `go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile`
  — run in the report's worktree (§7).
- **Raw dumps**: `/home/user/workspace/buried-audit/` — `scan.txt` (the 33 tags: tip, ahead,
  merge-base, per-file PRESENT/ABSENT), `scan2.txt`/`scan3.txt`/`scan5.txt` (per-branch
  symbol/ledger probes), `cherry.txt` (cherry +/-), `ident.txt` + `modified.txt` (blob
  identity), `final.txt` (the per-tag class counters used by §0.2), `branchfiles.txt`
  (373 added/modified file rows), `slugs.txt` (the 24).

## 7. DOC-INDEX row

```
devdoc/buried-archive-audit.md	historical	historical: a read-only audit of 33 archive/* tags at master db109cfb — a past run's record, frozen by nature	
```

(Appended to `devdoc/DOC-INDEX.tsv` in the same commit; class `historical` because the
document is a record of one run against one snapshot, and the file opens with the
`<!-- doc-class: historical -->` banner the gate requires.)
