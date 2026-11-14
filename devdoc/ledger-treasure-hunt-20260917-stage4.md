<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 4: the 29 HP-origin rows

Part of the epic `ledger-treasure-hunt-20260917` (Stage 4 of 7). Prior stages:
Stage 0 (partition + defect list, `devdoc/ledger-treasure-hunt-20260917-stage0.md`),
Stage 1 (merge lane), Stage 3 (cancelled non-HP rows,
`feat/ledger-treasure-hunt-stage3`). Canonical rows:
`devdoc/ledger-treasure-hunt-20260917-rows-master.csv`.

Row CSV for this stage: `devdoc/ledger-treasure-hunt-20260917-rows-stage4-hp.csv`
(columns `task_id,status,verdict,host,evidence_cmd,evidence_result,hp_recheck`).

## Scope — the 29-row worklist, reproduced (not re-derived)

The briefing's predicate is "tags `from-hostb-Laptop` / `node:hp`, plus
`from-hp`, `conflict`". Stage 0's partition B defines the class more precisely:
`HP-origin` = HP tags (including `worker:hostb-Laptop`, which the briefing's
list omits) **minus** the `[auto]`-prefixed notification residue. Reproduced
exactly from the canonical CSV:

| measure | value | how |
|---|---|---|
| `class=HP-origin` in the canonical CSV | 50 | `csv.DictReader` over the master CSV |
| of those, `[auto]`-prefixed | 21 | summary prefix |
| **worklist** | **29** | 50 − 21, ids listed below |

Integrity checks run: 29/29 ids exist in the live `~/.gopi/memory.db`; 0 missing;
0 ids outside the worklist (set difference both ways); the live DB now carries
**one additional** HP-tagged row created after the 2026-09-17 snapshot
(`01a0abfc-3e3f-7923-9355-1010f117d1c7`, `from-hp,bug,merger,worktree,data-loss`,
cancelled) — **excluded**, because the epic's population is the frozen snapshot.
Every one of the 29 gets exactly one verdict below; none was skipped, none invented.

## Evidence base — what is reachable on THIS host (measured, per row)

| source | state on peerbox | consequence |
|---|---|---|
| `~/.gopi/memory.db` | 112 MB, live, read-only queries | the HP rows' **ids, status, payload, reason** are all readable here |
| HP run logs (`~/.gopi/sessions/*.jsonl`) | **absent** for HP sessions | HP's *process* claims are not re-checkable |
| HP artifacts on disk (`~/.gopi/peerbox-mirror/`, `memory-glm-audit.db`, `legacy-audit-log-*.md`, `brief-glm-night-legacy-audit.md`) | **absent** (measured with `ls`) | the legacy-audit family's *effect* is unverifiable here |
| `refs/remotes/hp/*` | **39 refs present** — incl. `hp/feat/cov-*-hp` (5), `hp/fix/read-binary-sample-straddle`, `hp/hp-local-docs-67500ca`, `hp/master` | HP-side branch evidence *is* partly reachable |
| `master` | source of truth for "did the work land" | blob/ancestor checks below |

Correction to an inherited Stage-0 line: Stage 0 recorded five `hp/*` names
(`hp/feat/claim-broadcast`, `hp/feat/merge-backlog`,
`hp/fix/memsync-import-atomic`, `hp/feat/serve-tick-git-sync`,
`hp/feat/task-id-collision`) as "no ref on this host → UNCLEAR; Stage 4". They
are still absent **as refs**, and each of those five subjects is **already done
on master** (claim-lock `601c50ff` + `feat/claim-broadcast`; merge-backlog
`c88a2052`/`4e9cf9c3` + `e85b8509`; serve-tick/node-sync family; task-id
collision). So the missing refs are *not* unresolved work — the underlying rows
here (`t-1788329788`, the batch family) resolve on sibling evidence.

## Per-row verdicts — 29/29

`host` column: **peerbox** = the verdict is settled on this host from git/sibling
facts; **HP** = the decisive evidence (HP-side artifact, run log, HP ledger) is
NOT on this host and the verdict here rests on partial evidence.

### A. Real-work rows (11)

| # | task_id | status | verdict | evidence | host |
|---|---|---|---|---|---|
| 1 | `t-1788018876` | cancelled | **STILL-NEEDED** | `sed -n '88,98p' ~/work/tasks/internal/scheduler/cli/submit.go` → `else if fs.NArg() == 1 { sa.Description = sa.Title // use title as description if not provided }`; `gopi.go:113` `stdinContent := t.Description`; `Submit` validates only Type/Title, never Description | peerbox |
| 2 | `t-1788036172` | cancelled | **ALREADY-DONE** | M4 memsync landed: `devdoc/memroadmap-m1..m4.md`; `e85b8509 Merge branch 'feat/memsync-import-atomic'`; `devdoc/memroadmap-m4.md` carries a measured end-to-end acceptance (§"端到端实测（epic 验收线）": export→scp→import `stored=6 deduped=1 refused=0`, replay idempotent, search hits #1136/#1137) | peerbox (product); HP residue → re-check |
| 3 | `t-1788041891` | cancelled | **PARTIALLY-DONE** | `devdoc/glm-5.3-pricing.md` (53 lines) + `devdoc/assets/bm-pricing-20260830.{png,txt}` cover method + cross-validation + provenance; the payload demanded the **全脚本全文附录** (`/tmp/bmshot/bm-shot.js`) — `grep -n 'playwright\|goto(\|chromium\|fullPage'` finds only prose, no code block | peerbox (deliverable on master); script itself HP-resident |
| 4 | `t-1788053642` | cancelled | **ALREADY-DONE** | `internal/config/config.go:514` `type ComputeNode struct` with `MaxConcurrent` (LV counting), `ReportTo`, `Repo`; `ParseComputeNodesJSON` / `ResolveComputeNodes`; `cmd/agent/dispatch.go`; HP-side proof `devdoc/m47-e2e-report.md` (peerbox→HP→peerbox, real push-back, master unpolluted) | peerbox (product); HP record → re-check |
| 5 | `t-1788305210` | cancelled | **NO-VALUE** | reason (ledger): *stale claim (9/2 sync residue, claimed_by empty, no holder) — superseded by the current coverage campaign*; 12 of its 14 sibling rows completed, the 2 cancelled ones are the naming/classification entries superseded by that campaign | peerbox |
| 6 | `t-1788305224` | cancelled | **NO-VALUE** | same reason string as #5 | peerbox |
| 7 | `t-1788329788` | failed | **NO-VALUE** | subject is *claim task `t-1788329099` failed* — a claim-failure artifact, not a work row. The underlying defect is closed: `601c50ff fix(claim): claim-lock round wait must exceed ssh-leg worst case — 13min blowout fix (t-1788329099)`, `git merge-base --is-ancestor 601c50ff master` → yes | peerbox |
| 8 | `t-1788511058-3` | failed | **ALREADY-DONE** | `devdoc/night-delivery-20260905.md:42`: "small packages: test results ALL GREEN (config/deepseek/checkpoint/gopilog/proxy/shot FAIL=0) — task marked failed only due to an execution-layer artifact" (the ledger recorded the harness, not the work) | peerbox |
| 9 | `t-1788548312` | failed | **NO-VALUE** | payload is a pasted `git pull` transcript; 3 sibling rows in the same minute; the pull lane is covered by later production work (`devdoc/hp-push-diagnosis-20260916.md` links the HP→peerbox client-leg fix, `devdoc/hp-peerbox-divergence-audit-20260907.md`) | peerbox |
| 10 | `t-1788721024` | failed | **SUPERSEDED** | same subject created 5 minutes earlier and COMPLETED: `t-1788720718` (2026-09-06 18:51:58, completed, "LLM plays full HOMM3 game vs AI (M1) — RUN ON HP"), plus `t-1788720726` / `t-1788720910` ([auto] HP执行版, completed) | peerbox for the duplicate verdict; the game result itself is HP-side |
| 11 | `t-1788767581` | cancelled | **NO-VALUE** | summary `x`, payload `p` — a typing artifact with a 1-char briefing | peerbox |

### B. Legacy-audit family (11) — the batch work ran twice; the v2 campaign is the landing

| # | task_id | status | verdict | evidence | host |
|---|---|---|---|---|---|
| 12 | `01a085cd-bc06` (`test-diag-2`) | cancelled | **SUPERSEDED** | its payload is the **batch-01** audit briefing (id/summary mismatch — registration artifact); batch-01 landed in v2 | peerbox |
| 13–20 | `01a085cd-d22c` … `01a085ce-10b7` (v1 batch-01 … batch-08) | cancelled | **SUPERSEDED** | v1 payloads 411–426 chars, never run; the v2 family completed 01–07: `01a085d2-71ff`, `-7aee`, `-83e0`, `-8cb7`, `-9595`, `-9e88`, `-a772` (all `completed`, 2026-09-09, same briefings) | peerbox |
| 21 | `01a085d2-b051` (v2 **batch-08**) | failed | **STILL-NEEDED** | no completed successor exists: `select … where summary like '%batch-08%'` → only this row (failed) and the v1 row (cancelled). `/tmp/legacy-audit-batches/batch-08.tsv` still exists (161 lines) but the audit DB lives on HP (absent here) | peerbox (gap is settled); execution needs HP |
| 22 | `01a087c1-05fc` (v2 batch-02 增补) | failed | **SUPERSEDED** | v2 batch-02 `01a085d2-7aee` is `completed` (same day, earlier), and it was exactly the *v2-of-v1* re-registration whose own summary says "同 batch-01 配方" | peerbox |

### C. Code-review campaign (4) — deliverables on master; two rows mis-registered

| # | task_id | status | verdict | evidence | host |
|---|---|---|---|---|---|
| 23 | `01a08bc6-4a2a` (gopilog 100%) | failed | **ALREADY-DONE** | `51668406 test(gopilog): close function-level coverage 91.4% -> 100% (5 exported funcs)`, ancestor of master → yes; `python3 scripts/func_coverage.py --pkg internal/gopilog` → 59/59 **100.0%** | peerbox |
| 24 | `01a08bc9-dd9c` (shot) | cancelled | **ALREADY-DONE** | `ae79d8d1 test(shot): function-level coverage of the X11/XTEST verbs + fake X server`, ancestor of master → yes; today 12/13 (92.3%); the residual `ProbeDisplay` did not exist at that commit (`git grep -c 'func ProbeDisplay' ae79d8d1` → 0; present on master → added later by other work) | peerbox |
| 25 | `01a08bc9-ddbd` (deepseek) | cancelled | **ALREADY-DONE** | `6e29c21e test(deepseek): close function-level coverage gaps in internal/deepseek`, ancestor of master → yes; residual `EffectiveThinkingBudget` added later (`git grep … 6e29c21e` → 0; present on master) | peerbox |
| 26 | `01a08bcf-6dd0` (config 6 gaps) | cancelled | **ALREADY-DONE** | `f1904541 test(config): close function-level coverage 81.2% -> 100% (6 exported funcs)`, ancestor of master → yes; every function the row names now has a same-package test reference (`EnvBool`, `ResolveSettingsPath`, `ResolveReasoningEffort`, `ResolveComputeNodes`, `ParseComputeNodesJSON`, `ResetNodeIdentityForTest` → tests:1 each). Today's 75.5% is **later-added** surface (`env-reader`/`tmpdir` family, incl. `ResolveTmpDirConfigured` which does not exist at `f1904541`) | peerbox |
| — | `t-1788305210`, `t-1788305224` | cancelled | (see #5, #6) | the campaign's manifest (`devdoc/cli-review-epic-manifest.md` rows 5 & 7) names `devdoc/review-cli-task-merge.md` / `review-cli-resume-checkpoint.md` — **neither exists**; the campaign's 12 delivered reports (`review-cli-config-options.md`, `-task-core`, `-task-sync`, `-log`, `-chat-web`, `-brew-memsync`, `-proxy-version`, `-search`, `-vision`, …) cover every other surface | peerbox |

### D. Smoke / probe residue (3)

| # | task_id | status | verdict | evidence | host |
|---|---|---|---|---|---|
| 27 | `01a0957d-e6eb` ("HP deployment smoke: confirm the refreshed checkout") | failed | **NO-VALUE** | payload empty; reason `task failed`; no branch/commit/deliverable name anywhere in the row | peerbox |
| 28 | `01a0958c-0fd3` ("verification smoke: HP refresh attribution probe (verifier session)") | failed | **NO-VALUE** | payload empty; a smoke of the HP refresh path, whose production counterpart is on master (`f785f963 docs(no-privilege): report`, merged at `5554780e`; worker-ownership pointer + gate live on master: `devdoc/worker-ownership.md` present, `ee0f363f`/`699c6bf9` ancestors → yes) | peerbox |
| 29 | `t-live-accept` | failed | **NO-VALUE** | non-uuid id, summary `live acceptance row`, payload `live acceptance row` — a probe written to adopt another row's live claim (the reclaim path exercises this id) | peerbox |

*(Post-snapshot `01a0ab10-cd48`, `from-hp`, payload empty, summary `work`, is excluded from the 29 — noted in Scope.)*

## Statistics

| verdict | count | share |
|---|---|---|
| ALREADY-DONE | 7 | 24% |
| SUPERSEDED | 11 | 38% |
| NO-VALUE | 8 | 28% |
| STILL-NEEDED | 2 | 7% |
| PARTIALLY-DONE | 1 | 3% |
| UNCLEAR | 0 | 0% |
| **total** | **29** | 100% |

Host split: **29/29 verdicts were reached on this host**; **7 rows** carry a
verdict that is complete for the *ledger* question but whose *product* claim
still wants HP-side confirmation (list below). 0 rows needed HP to decide
what to do with them — the HP ledger is the authority for **process**, this
ledger (row ids are uniform across hosts since M4) for **existence and status**.

Recovery yield: **2 rows filed** (1 STILL-NEEDED + 1 PARTIALLY-DONE re-file):

| new task | from row | what |
|---|---|---|
| `01a0b4af-ff33-7f09-a89b-997505f40ed5` | `t-1788018876` | `~/work/tasks` dispatcher: a task with no Description must not dispatch as a summary-only briefing |
| `01a0b4ba-173e-70a6-9f21-72721927fe4f` | `t-1788041891` | bm-pricing devdoc: append the HP capture-script full text (or a labelled reconstruction) |
| `01a0b4ba-a43e-78c8-9b96-9ac0eeac5959` | `01a085d2-b051` | legacy-audit batch-08: the only batch with no completed run |

## Pattern analysis

1. **HP-org rows are 42% notification residue.** 21 of the 50 HP-tagged rows are
   `[auto]`-prefixed (`reply with just: ok`, `version`, pasted terminal output) —
   HP's notify channel writes a row per operator message. The 29 "real" ones are
   really **~12 distinct pieces of work** (the legacy-audit family alone is 11).
2. **Double registration is the dominant loss mode here (11/29 = 38%).** The whole
   legacy-audit batch family exists twice — v1 (cancelled, never run) and v2
   (completed 01–07) — with no link field between them. Resolving "did batch-03
   run?" requires matching prose across two generations of ids.
3. **Ledger `failed` is not evidence of absence.** 7 of the 29 are ALREADY-DONE or
   STILL-SUPERSEDED with the work merged on master; the clearest case is
   `t-1788511058-3`, whose own completion report says tests were ALL GREEN and the
   row was failed by the harness. This is Stage 0's D-list confirmed on the HP
   population.
4. **HP-origin rows are NOT HP-ledger-only.** Every id in this batch resolves in
   *this* database (`~/.gopi/memory.db`), because the HP rows were synced into it;
   what is missing is HP's disk (sessions, audit DB, `/tmp` artifacts), not HP's rows.
5. **The cancelled rows' reason strings are the only provenance** for the two
   code-review entries (`stale claim … superseded by the current coverage
   campaign`) — accurate, and it is why both resolve to NO-VALUE rather than UNCLEAR.

## Ledger-mechanism defects (stage-4 observations, additive to Stage 0's D-list)

- **D-S4-1 — a claim failure is written as a task row.** `t-1788329788`'s subject
  is another row's id ("claim task `t-1788329099` failed"); its `[auto]` sibling
  `t-1788329799` (same minute, excluded from the 29 by the class rule) does the same.
  Failures of the *machinery* occupy the same id space as failures of *work*, so
  any audit counting "failed rows" counts harness noise — 1 of the 29 is such a row,
  plus 1 in the `[auto]` residue.
- **D-S4-2 — `failed+resolved` contradicts itself.** 2 of the 29 carry
  `sub_class=failed+resolved` (`t-1788329788`, `t-1788511058-3`): resolution was recorded without moving the status
  off `failed`. Any query filtering `status='failed'` re-reports work that is done.
- **D-S4-3 — a row can be registered with no payload, or a 1-char payload.**
  `01a0957d`, `01a0958c`, `t-live-accept`, `01a0ab10` (`work`), `t-1788767581`
  (`x`/`p`). Nothing in the schema distinguishes these from a real briefing; they
  inflate every audit population.
- **D-S4-4 — a cancelled row's payload can belong to a DIFFERENT task.**
  `01a085cd-bc06` has summary `test-diag-2` and the batch-01 audit payload: the id
  and the content disagree. Registration writes summary and payload from different
  sources.
- **D-S4-5 — no supersedes edge.** Two generations of the same campaign share no
  field (v2 says "同 batch-01 配方" in prose). "Which row actually did it" is
  archaeology; the ledger cannot answer it.
- **D-S4-6 — sync inserts rows for work that is already superseded.**
  `t-1788305210`/`t-1788305224` were cancelled as *"stale claim (9/2 sync residue,
  claimed_by empty, no holder)"* — the sync created claimable rows on peerbox for
  work already completed elsewhere, i.e. the ledger manufactured two DOA rows.
- **D-S4-7 — the `hp/` remote namespace mixes a parked master copy with branches.**
  `hp/master` == `hp/feat/worker-run-entry2` == `69975030` (same tip). Content,
  not the ref name, decides whether an `hp/*` ref carries work.

## Rows that must be re-checked on HP (7)

| row | what to check there | why it cannot be closed here |
|---|---|---|
| `t-1788036172` | whether the M4 memsync **design-round** artifact (the design-comparison doc the payload demands *before* implementation) exists on HP, and whether any HP memory rows were later refused on import | HP run log absent; `~/.gopi/peerbox-mirror/` absent |
| `t-1788041891` | `/tmp/bmshot/bm-shot.js` (the demanded script appendix) and `~/.gopi/sessions/6d57ea58…` | the script lived in HP `/tmp`; only its *result* was scp'd to peerbox |
| `t-1788053642` | the M4.7 dispatch runs' HP-side report-back records (HP ledger writeback, `feat/m47-e2e-hp`) | the delivering report exists here (`devdoc/m47-e2e-report.md`); HP's own accounting is on HP |
| `t-1788721024` (+ siblings `t-1788720718`, `t-1788720726`, `t-1788720910`) | the HoMM3 run output/recording — did the completed sibling actually play a full game? | product evidence (game log/screenshot) is HP-side; only the duplicate verdict is settable here |
| `t-1788305210` | nothing needed for the verdict — check only that no `review-cli-task-merge.md` was left unmerged on an HP branch | covers an HP-side report path |
| `t-1788305224` | same, for `review-cli-resume-checkpoint.md` | same |
| `t-1788511058-3` | the NIGHT-TEST run log for the small packages (confirm FAIL=0 independently of the delivery doc) | the run happened on HP |

## Host-boundary statement (what Stage 4 could NOT settle)

- HP **run logs** (`~/.gopi/sessions/*.jsonl`), the **HP ledger**, the **audit DB**
  (`~/.gopi/backups/memory-glm-audit-20260909-184030.db` exists here but the live
  `~/.gopi/memory-glm-audit.db` does not), `legacy-audit-log-*.md`, and
  `~/.gopi/peerbox-mirror/` are **all absent on peerbox** (`ls` exit ≠ 0, recorded in
  `## Evidence (self-filed)`).
- Therefore: HP's *process* claims are not re-verifiable here, and the
  batch-08 verdict is "no successor completed it", not "the audit was never done".
- **No HP-side state was modified by this stage** — every HP interaction was a
  read of this host's synced copy or of `refs/remotes/hp/*`.

## Not Done (honest)

- One PARTIALLY-DONE row (`t-1788041891`) got its verdict but its **re-filed
  task is a payload-appendix job whose source artifact lives on HP `/tmp`**; the
  briefing for it names the HP-side check explicitly, so the new task may close as
  NO-VALUE if the script is gone — that is the executor's call, not this stage's.
- Rows `t-1788305210` / `t-1788305224` could be upgraded from NO-VALUE to
  ALREADY-DONE on HP if a leftover review branch turns up; recorded in the re-check
  table rather than guessed at here.
- The 21 `[auto]`-prefixed HP notification rows are **out of this stage's scope**
  (the briefing's predicate excludes them, matching Stage 0's class rule) and were
  not individually classified.
