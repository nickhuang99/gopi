<!-- doc-class: historical -->
# Ledger Treasure Hunt — FINAL consolidation (Stage 6 of 7)

**Epic**: `ledger-treasure-hunt-20260917` · **Branch**: `feat/ledger-treasure-hunt-stage6`
**Date**: 2026-09-18 · **Master base**: `0317d09a` (2026-09-18 21:59:12 +08:00)
**Frozen worklist**: `devdoc/ledger-treasure-hunt-20260917-rows-master.csv`, md5
`ff312577ccb7eb0406b520cce31b15cb`, 319 rows + header.

This is the ONE report the epic asks for. It does not re-do the per-row forensics —
those live in the five batch shards and their sections (§1). What it does is what no
single batch could: **account for all 319 rows in one table whose counts add up**, state
the reusable disposition rules, name the ledger-mechanism defects with a code site, and
list every task this audit submitted.

---

## 1. What was consolidated (the per-batch sections)

| stage | section (committed) | shard (committed) | rows | on master |
|---|---|---|---|---|
| 0 | `ledger-treasure-hunt-20260917-stage0.md` | `…-rows-stage0.csv` | 103 | ✅ |
| 0-verify | `ledger-treasure-hunt-20260917-stage0-verify.md` | (names the 71 `auto-prefixed` rows in §A1/§A2) | — | ✅ |
| 0-residual | (this stage — see below) | `…-rows-stage0-residual.csv` | 68 | **written by this stage** |
| 1 | §Stage 1 in `…stage0.md` (`c9f4e4b8`) | `…-rows-stage1-merge-lane.csv` | 83 | ✅ |
| 2 | `ledger-treasure-hunt-20260917-stage2.md` | `…-stage2-verdicts.csv` | 56 | ✅ |
| 3 | `ledger-treasure-hunt-20260917-stage3.md` | `…-rows-stage3-cancelled.csv` | 121 | ✅ |
| 4 | `ledger-treasure-hunt-20260917-stage4.md` | `…-rows-stage4-hp.csv` | 29 | ✅ |
| 5 | `ledger-treasure-hunt-20260917-stage5.md` | `…-rows-stage5-resolved.csv` | 52 | **recovered by this stage** |

**Stage 5 was the gap this consolidation had to close first.** Its deliverables existed
only as `/tmp/s5.csv` + `/tmp/s5.md`; `feat/ledger-treasure-hunt-stage5` was not in
`git worktree list` and its report was uncommitted on the branch. Recovered verbatim
(`git checkout feat/ledger-treasure-hunt-stage5 -- …`) at `9c7611e6`; the recovery is
the only content change in this branch so far. Without it the final table would have
had 52 unattributed rows.

**Stage 0 had the same defect one level down, and this stage closed it.** Stage 0's CSV
has **no `verdict` column** — it is the class partition, not a disposition — so the 68
rows no later stage claimed had their verdicts only in a session scratch map
(`/tmp/a1map.json`). The final table would have had 68 `UNCLASSIFIED` rows. This stage
promoted them into a committed shard,
`devdoc/ledger-treasure-hunt-20260917-rows-stage0-residual.csv` (68 rows, emitted by
`…-rows-stage0-residual.py`), and — because a scratch map is not evidence — **re-checked
all 29 real-content rows against master** and recorded the deciding ref in each row's
`evidence_result` cell. The 39 void-by-shape rows carry the R4 shape rule (19 chat
backfills / 10 CLI probes / 10 fixtures). Result: the table is reproducible from
committed artifacts alone, with no console-scrolling step left.

**Stage 1's shard is 83 rows, of which only 17 are in the 319.** The other 66 are the
oracle superset (the epic's Phase-0 branch census) — they are *not* ledger rows and are
excluded by construction: the intersection with the frozen CSV is taken first (§2).

---

## 2. The reconciled table — 319 rows, one verdict each

Method (mechanical, re-runnable — `devdoc/ledger-treasure-hunt-20260918-consolidate.py`
emits the per-row artifact `…-rows-final.csv`, header
`task_id,status,class,resolved_tag,shard,verdict,recovery_task`; run it with `--check`
to verify the committed CSV is not stale):

1. Read the frozen 319-row CSV (the worklist of record — never a live query, rule R8),
   and fail loudly if its md5 is not the pinned one.
2. For each row take the verdict from the **first** shard that has it, in the fixed
   precedence `stage5 > stage4 > stage3 > stage2 > stage1 > stage0`. The precedence is
   fixed *before* classifying (rule R5) so the overlap cannot shift the totals.
3. Apply the two pinned evidence resolutions (§3) — precedence alone must not silently
   pick a winner where the shards disagree.
4. Rows in no shard are the `auto-prefixed` residual; they are all covered by the
   stage-0 residual shard (§1), whose 29 real-content rows carry their own git fact.
5. Set-difference check both ways, plus a per-row assertion that every
   STILL-NEEDED / PARTIALLY-DONE row has a submitted successor (§6): **0 master rows
   uncovered, 0 ids outside the 319** (stage1's 66 oracle ids are the only extras, and
   they are not ledger rows).

### 2.1 Top-line verdicts

| verdict | rows | share |
|---|---:|---:|
| ALREADY-DONE | **151** | 47.3 % |
| NO-VALUE | **83** | 26.0 % |
| SUPERSEDED | **74** | 23.2 % |
| STILL-NEEDED | **10** | 3.1 % |
| PARTIALLY-DONE | **1** | 0.3 % |
| **total** | **319** | **100 %** |

**The counts add up: 151 + 83 + 74 + 10 + 1 = 319.** Zero UNCLEAR — every row reached a
verdict on independent evidence; the only rows whose decisive evidence is off-host (HP)
are the 7 in Stage 4's re-check table, and each of those still got a verdict on the
evidence reachable here, with the missing fact named in its evidence cell.

**Headline**: 225 of 319 rows (70.5 %) label work that is either already in master
(ALREADY-DONE) or deliberately retired (SUPERSEDED). 11 rows carry live work. That is
the epic's premise, measured: *the ledger state is not a statement about the work.*

### 2.2 By class (the partition, each class a predicate)

| class | rows | failed | cancelled | ALREADY-DONE | SUPERSEDED | NO-VALUE | STILL-NEEDED | PARTIALLY |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `curated-cancelled` | 93 | 0 | 93 | 40 | 39 | 10 | 4 | 0 |
| `auto-prefixed` | 71 | 31 | 40 | 24 | 1 | 46 | 0 | 0 |
| `curated-failed` | 56 | 56 | 0 | 49 | 3 | 1 | 3 | 0 |
| `HP-origin` | 50 | 18 | 32 | 17 | 12 | 18 | 2 | 1 |
| `sync-yielded` | 18 | 0 | 18 | 2 | 16 | 0 | 0 | 0 |
| `merge-lane` | 17 | 13 | 4 | 14 | 3 | 0 | 0 | 0 |
| `wrapper:t-*` | 14 | 8 | 6 | 5 | 0 | 8 | 1 | 0 |
| **total** | **319** | **126** | **193** | **151** | **74** | **83** | **10** | **1** |

Column checks: 0+31+56+18+0+13+8 = **126 failed**; 93+40+0+32+18+4+6 = **193 cancelled**;
each verdict column sums to §2.1. The class predicate is Stage 0's partition B
(`HP-origin > wrapper:t-* > sync-yielded > merge-lane > auto > curated-failed >
curated-cancelled`, mutually exclusive, remainder explicit — rule R5).

### 2.3 By contributing shard

| shard | rows used | ALREADY-DONE | SUPERSEDED | NO-VALUE | STILL-NEEDED | PARTIALLY |
|---|---:|---:|---:|---:|---:|---:|
| stage 5 | 51 | 32 | 1 | 16 | 2 | 0 |
| stage 4 | 27 | 6 | 11 | 7 | 2 | 1 |
| stage 3 | 120 | 45 | 56 | 15 | 4 | 0 |
| stage 2 | 41 | 35 | 3 | 1 | 2 | 0 |
| stage 1 | 12 | 10 | 2 | 0 | 0 | 0 |
| stage 0 (residual) | 68 | 23 | 1 | 44 | 0 | 0 |
| **total** | **319** | **151** | **74** | **83** | **10** | **1** |

The two rows whose shard moved (stage 5 → stage 2 for one, stage 2 → stage 5 for the
other) are §3's resolved conflicts: a resolution is not precedence, so the row is
booked to the shard whose evidence decided it.

### 2.4 The `resolved` tag — reconciled

67 of the 319 rows carry the `resolved` tag (65 failed + 2 cancelled; identical to the
2026-09-17 snapshot while the *total* has drifted 319 → 430, defect D7). Their verdicts:
ALREADY-DONE 43, NO-VALUE 20, STILL-NEEDED 2, SUPERSEDED 2. **So 24 of 67 `resolved`
rows are NOT done work** — 20 that never had a work identity, 2 that are actively still
needed, 2 superseded. D4 also holds: the overlap with the mechanical classes is partial
(`curated-failed 25, auto-prefixed 21, HP-origin 8, wrapper:t-* 8, merge-lane 4,
curated-cancelled 1`), which is why tag counts are not additive across classes.

---

## 3. The two verdict conflicts — resolved, not averaged

24 rows got a verdict from more than one shard; 22 agree. Two did not, and the epic's
rule is that a row gets **one** verdict, so each was decided on evidence rather than by
precedence alone:

**Conflict 1 — `t-1788551878` (stage2 NO-VALUE vs stage5 ALREADY-DONE).** Both cited
the same subject doc. The timestamps decide it: the row was created `19:57:58`,
ended `20:02:29`, its session died on a `429 rate limit exceeded` at iteration 11, and
`devdoc/verify-write-task-area-20260904.md` did not exist until `448a3ea1` at
**20:56:46** — 54 minutes *after* the row died. Stage 5 read the doc's existence as the
row's work landing; the doc was authored later, by the run the row was *about*. The
row's deliverable was a reply never produced (defect D12). **NO-VALUE stands.**

**Conflict 2 — `01a091fb-1e7d` (stage2 ALREADY-DONE vs stage5 STILL-NEEDED).**
Stage 2 greened it from `internal/stylegate/gate_test.go:38` (the cli_voice discard
sites were fixed); Stage 5 re-ran the gate at master `0317d09a`. This stage re-ran it as
well (`CGO_ENABLED=0 go test -count=1 -run TestStyleGate ./internal/stylegate/` →
**FAIL**, 6 rules over ceiling: ignored-error production 12/0, test 189/163, errorf-verb
8/7, test-sleep 4/0, test-setenv 1/0, test-skip 1/0, reported at
`internal/stylegate/gate_test.go:174`). Both shards were right about *their* fact: the
cited sites closed, the gate stayed red. The row is about the *gate*.
**STILL-NEEDED stands**, and the row is re-filed (§5).

---

## 4. Reusable disposition rules (11, of which ≥3 are new here)

Carried from the batches — these are the rules that produced the table, not commentary:

- **R1 — merge proof is a resolvable ref plus a zero count.** `git rev-parse --verify`
  then `git rev-list --count master..<ref> == 0`. `git merge-base --is-ancestor` alone
  is not a proof across a rebase (silent false positives).
- **R2 — an archive tag records ABANDONMENT, never completion.** `git cherry -v master
  <tag>` per tag; the tag's subject is the commit it names, not a reason.
- **R3 — a row's status is not evidence about its work.** The verdict requires an
  independent fact (git content, a re-run gate, a successor row read directly).
- **R4 — rows with no work identity are NO-VALUE by shape, before any git call.**
  Wrapper rows, `— chat: backfilled` turns, deliberately-failing fixtures, ≤25-char CLI
  probes. Judge the *child* row for a wrapper.
- **R5 — pin the precedence order before classifying.** Otherwise class sizes shift and
  batches double-count (measured D6).
- **R6 — a terminal row a LATER lane repaired is ALREADY-DONE.**
- **R7 — an archived branch whose FEATURE is on master under another layout is
  SUPERSEDED.**
- **R8 — the worklist is a frozen artifact, not a query.** (new in stage0-verify) For
  any append-only/live ledger, freeze the per-row index once, commit it, slice that.
  A fresh `select count(*)` reports the ledger's drift as findings — D7 measured 319 → 430
  during this epic.
- **R9 — a `superseded:` reason retires the row's MECHANISM, never the WORK.** Open the
  reason's own claim and test it against git. 3 of 63 rows were hiding live work behind
  that sentence.
- **R10 — "owned" is not "covered".** (new in stage 5) Before treating a queued row as
  coverage of a live red, measure the queued row: base distance to master, whether its
  patch touches the file that is red. A reland row whose base is 210 commits behind and
  which carries 0 hunks for the red file cannot close it. **Measure the owner, not the
  queue.**
- **R11 — a timestamp is evidence, and it can reverse a verdict.** (new here) When two
  shards disagree about "did the artifact land", compare the row's `ended_at` against
  the artifact's authoring commit. An artifact authored *after* the row died cannot be
  the row's output (Conflict 1).

---

## 5. Ledger-mechanism defect list — failure classes that should never have been a verdict

Each defect names the **code site that writes the reason** (the Stage 0 lesson: read the
writer, never stop at the reason string) and one measured instance. D-numbers are the
batches' own ids, preserved; `D-S6-x` are the two this consolidation added.

| id | defect (failure class that must never be recorded as a work verdict) | code site | instance / measurement |
|---|---|---|---|
| **D2** | a merge-backlog decision row reaches terminal **`verified`** while its branch is unmerged — verify closes the ROW, not the MERGE | `internal/tools/memory_sync_task.go:1315` (the sync door that adopted it) · guard now at `internal/tools/task_merge_backlog_terminal_guard.go:495,524` | `t-1788218295` `[verified]`, `git rev-list --count master..archive/fix-memsync-import-atomic-20260901` = **5** |
| **D3** | an **archive tag** is treated as evidence of completion | the tag namespace + its writer: `internal/tools/tasks.go:4087` (`archiveBranchPrefix = "archive/"`), read by the sweep's tag logic | 11/12 sampled tags are not master ancestors; `git cherry -v master <tag>` shows patches unapplied |
| **D8** | the **merge-verify staleness guard** cancels rows whose work landed: its landing witness is a 40-commit window with a one-spelling token parser | `internal/tools/task_staleness_guard.go:287` (`MergeVerifyStaleVerdictBranchNotFound`) · `:126` (`mergeVerifyStaleLogWindow = 40`) | `01a0a59a` (`feat/web-lan-access`) and `01a0a9e0` both cancelled, both merged on master; landing 363 commits deep → outside the window |
| **D9** | a **transient infrastructure death is recorded as a permanent work verdict** — the writer cannot tell "my child crashed" from "the work failed" | `cmd/agent/cli_tasks.go:1686` and `:1961` (`writebackOutcome(target, TaskStatusFailed, "gopi run failed: "+…+taskAbnormalChildMarker+…)`) | `01a095af` (session `exit_code: 0`, 16 299 s, 818 iterations, doc delivered → still `failed`), `01a09805` (same shape), `01a089a7` (child killed; landed `d75818e8`) |
| **D9b** | the same class one door further: `TaskRunWriteback` only accepts `completed|failed`, so every infra death lands as the same word | `internal/tools/tasks.go:5576` (`"task writeback: status must be completed\|failed, got %q"` — the status guard) | all 8 D9 rows above |
| **D10** | a merge → revert → re-merge leaves the row terminal-failed with **no re-open path**: `cancelled`/`verified` are terminal with an EMPTY transition set, and `reopened` is retired | `internal/tools/tasks.go:2895` (`taskStatusCancelled: {}`), `:2896` (`taskStatusVerified: {}`), hint at `:3504` | 7 of 38 branch-bearing rows; master carries the final MERGE in every one |
| **D11** | `curated-failed` is a **residual**, so every downstream count inherits the drift (the epic quoted 51 for a class of 56) | the partition itself — publish the predicate or the frozen CSV row-set | Stage 0's derived table; D6 |
| **D12** | a **reply-only deliverable is unpreservable** — the row has no branch and no file, so a killed run leaves nothing to audit | `internal/tools/task_report.go:105` (`SealTaskReport` writes the skeleton *after* the run) | `t-1788551878` (payload: "不需要 commit 任何文件"); `01a0a92d-560e`/`-77a0` session dirs hold **no jsonl at all** |
| **D13** | the **gates that police the ledger are red on master**, so a `resolved` verdict resting on "the gate is green" cannot be trusted when written | `internal/stylegate/gate_test.go:50` (`gateCeilings`), reported at `:174`; `internal/standards` Rule 17 NEW at `cmd/agent/merge_lock_test.go:200` | this run: `go test -run TestStyleGate ./internal/stylegate/` → **FAIL**, 6 rules over ceiling; `go test ./internal/standards/` → **FAIL**, Rule 17 NEW violation |
| **D-S6-1** | **`resolved` is a bookkeeping tag with a work reading, and the default `task list --status failed` view HIDES tagged rows** — a harness failure disappears from the operator's failure list | `internal/tools/tasks.go:4726` (`const taskTagResolved = "resolved"`), `:5053` (`FilterArchivedRows`), applied at `cmd/agent/cli_tasks.go:4456`, documented at `:211-212` | 65 failed rows carry the tag; of the 67 tagged rows in the 319, **24 are not done work** (§2.4) |
| **D-S6-2** | **retiring a status leaves a dangling write path**: `reopened` is retired with no transition into it and nothing leaving it, so a legacy row that must run again has no legal status move | `internal/tools/tasks.go:66-71` (`taskStatusReopened = "reopened"`, LEGACY-ONLY) and the empty sets at `:2895-2896` | the hint text at `:3504` is the only remedy the system offers — "submit a NEW task and leave the judged row terminal" |
| **D7** (live) | the ledger is a moving target: a count re-derived mid-audit reports drift as findings | — (method defect) | identical query 319 → **430** total failed+cancelled during this epic; the `resolved` overlap stayed 65/2 |
| **D5** | SQLite `LIKE` has no bracket expression, so `LIKE '[auto]%'` matches nothing | — (method defect) | `substr(summary,1,6)='[auto]'` → 106; the `LIKE` spelling → 0 |
| **D6** | class sizes are precedence- and residual-dependent; `merge-lane 30` matches **no** predicate | — (method defect) | union of merge tag shapes = 17, with `merge-backlog` = 33, with `tags LIKE '%merge%'` = 70 |

**Where the defects that lost work stand** (verified by direct row read, not by memory):

| row | status | verdict |
|---|---|---|
| `01a0ab46-f1e2-7a6d-8b3c-67bee7d04425` (D2's lost memsync fix) | **completed** | recovered |
| `01a0ab46-f3b0-70f8-aefd-480af2a5a14c` (D2 mechanism) | **completed** | fixed |
| `01a0ab4d-19fb-…` / `01a0ab4e-f127-…` (D8) | **completed** | fixed (the correction supersedes the first) |
| `01a0b4a7-b0b2-…` (D11 scratch stale age) · `01a0b4a7-b427-…` (post-merge gate re-run) | **completed** | fixed |
| `01a0b4af-93cf-…` (remote `task_submit` wrote a node-local ledger) | **completed** | fixed |
| `01a0b47-28c7-…` (66 archive tags, D3) | pending | open |
| `01a0b4af-78da-…` (SPA capture, D12's sibling) · `…-b718-…` (ledger backup) · `01a0b4d6-5131-…` (missing requirement text) · `01a0b4ba-173e-…` · `01a0b4ba-a43e-…` · `01a0b4aa-6874-…` · `01a0b4aa-69af-…` | pending | open |
| `01a0b4dc-771d-…` (D13 stylegate ratchet) | in_progress | open |

---

## 6. Recovery work list — every task this audit submitted

17 tasks. Each was submitted by a batch with a self-contained payload; **none was
submitted by this consolidation** (its job is to account for them, not to add to them).
Statuses are from a read-only query of the live ledger today.

| # | new task_id | from row(s) | asks for | status |
|---|---|---|---|---|
| 1 | `01a0ab46-f1e2-7a6d-8b3c-67bee7d04425` | `t-1788218295` + `t-1788213995`/`t-1788211772`/`t-1788211810`/`t-1788214035` | land the dropped memsync import-atomicity fix (5 commits, 482 insertions) | completed |
| 2 | `01a0ab46-f3b0-70f8-aefd-480af2a5a14c` | `t-1788218295` (D2) | a terminal `verified` on an unmerged branch must not be writable | completed |
| 3 | `01a0ab47-28c7-7dbf-99e0-40ce8712610b` | D3 (66 `archive/*` tags with no repair row) | audit the unaudited archive tags | in_progress |
| 4 | `01a0ab4d-19fb-72c3-82ee-e2e61c005dae` | `01a0a59a` + `01a0a9e0` (D8) | stop the staleness guard reading a swept ref as a failure | completed |
| 5 | `01a0ab4e-f127-78d1-a126-9b80b9a0d335` | D8 (corrected root cause) | the window/token-parser root cause; **supersedes #4** | completed |
| 6 | `01a0b4aa-6874-710f-9380-e39910a2d8f4` | `01a0ab60` (stage 1) | land `feat/conflict-branch-rename` (12 unlanded commits) | pending |
| 7 | `01a0b4aa-69af-739c-a381-92e2a031578f` | `01a0ab1e` + `01a0ab7c` (stage 1) | land the archive-why / hand-archive reason gate | pending |
| 8 | `01a0b4a7-b0b2-7c35-aeef-41db957ae921` | `01a0a4c4-c26a-726f-a32f-72b171194741` | parameterise the scratch GC stale AGE | completed |
| 9 | `01a0b4a7-b427-7e42-897f-3ed84da2c8a4` | `01a0a92d-77a0-7eb0-90a6-8c5f63051177` | re-run the repo's gates on the MERGED result | completed |
| 10 | `01a0b4af-78da-7d9b-b9df-4dc5f317f52e` | `pa-q-spa-screenshot-paradigm` + `pa-q-cny-screenshot-shot` | the CDP/headless capture paradigm (one mechanism, two rows) | pending |
| 11 | `01a0b4af-93cf-788e-a916-ebdc122a8220` | `t-1788065158` | a remote `task_submit` must not write the node-local ledger | completed |
| 12 | `01a0b4af-b718-7766-a6b6-295b2d7aecbd` | `t-1788897004` | back the authoritative ledger up off-host | pending |
| 13 | `01a0b4af-ff33-7f09-a89b-997505f40ed5` | `t-1788018876` (stage 4) | a description-less task must not dispatch as a summary-only briefing | completed |
| 14 | `01a0b4ba-173e-70a6-9f21-72721927fe4f` | `t-1788041891` (PARTIALLY-DONE) | append the HP capture-script text to the bm-pricing doc | pending |
| 15 | `01a0b4ba-a43e-78c8-9b96-9ac0eeac5959` | `01a085d2-b051-…` (stage 4) | complete legacy-audit batch-08 | pending |
| 16 | `01a0b4d6-5131-7a22-bfc1-58d41e289558` | `t-1788468254` (stage 5) | record the missing requirement text | pending |
| 17 | `01a0b4dc-771d-7f1a-a3de-332123331788` | `01a091fb-1e7d` (stage 5, Conflict 2) | ratchet the stylegate ceilings from a fresh master base | in_progress |

**Row → task coverage of the 11 open rows** (10 live rows + 1 PARTIALLY-DONE):
`01a091fb-1e7d`→#17 · `01a0a4c4`→#8 · `01a0a92d`→#9 · `t-1788065158`→#11 ·
`t-1788897004`→#12 · `pa-q-*` (both)→#10 · `t-1788018876`→#13 · `t-1788041891`→#14 ·
`01a085d2`→#15 · `t-1788468254`→#16. Rows #1/#2/#3/#4/#5/#6/#7 are mechanism/archive
recoveries filed from the batches' own defect findings. **Every STILL-NEEDED and
PARTIALLY-DONE row has a submitted successor** — that is this epic's acceptance clause,
satisfied by construction, and the `recovery_task` column of
`ledger-treasure-hunt-20260918-rows-final.csv` carries it per row.

---

## 7. Verification

- **Count identity** — `python3 devdoc/ledger-treasure-hunt-20260918-consolidate.py`:
  `frozen worklist md5 ok: ff312577ccb7eb0406b520cce31b15cb (319 rows)`,
  `verdicts: {'NO-VALUE': 83, 'ALREADY-DONE': 151, 'STILL-NEEDED': 10,
  'PARTIALLY-DONE': 1, 'SUPERSEDED': 74}  sum=319`,
  `status: failed=126 cancelled=193`, `uncovered=0  outside-worklist ids=66`.
- **Class identity** — each class column sums to its row count
  (`93+71+56+50+18+17+14 = 319`; `126 failed + 193 cancelled = 319`); the same command
  prints the class×verdict matrix used in §2.2.
- **Reproducibility** — `…-consolidate.py --check` → *"…-rows-final.csv matches the
  shards (byte-identical)"*; `…-rows-stage0-residual.py --check` → *"…-rows-stage0-
  residual.csv matches (68 rows)"*.
- **Worklist integrity** — `md5sum …rows-master.csv` =
  `ff312577ccb7eb0406b520cce31b15cb`; the consolidator refuses to run (exit 1, "the
  worklist of record moved") if that pin does not match.
- **Ledger untouched** — every query ran as
  `sqlite3 -readonly "$HOME/.gopi/memory.db" "…"` (`mode=ro`); no
  INSERT/UPDATE/DELETE, no archive/merge tool invoked, no row, tag or branch modified.
- **Conflict 1 re-measured** — `git log --format='%h %ad %s' --date=iso --
  devdoc/verify-write-task-area-20260904.md` → `448a3ea1 2026-09-04 20:56:46 +0800`
  (first creation) vs the row's `ended_at 2026-09-04 20:02:29`, and its own payload:
  *"交付：直接在你的最终回复中给出报告（300-500 字）。不需要 commit 任何文件。"*
- **Conflict 2 / D13 re-measured** — `CGO_ENABLED=0 go test -count=1 -run TestStyleGate
  ./internal/stylegate/` → `--- FAIL: TestStyleGate (5.88s)`, `gate_test.go:174:
  stylegate ignored-error: 12 production hit(s), ceiling 0` / `test hit(s) 189, ceiling
  163` / `errorf-verb 8, ceiling 7` / `test-sleep 4, ceiling 0` / `test-setenv 1,
  ceiling 0` / `test-skip 1, ceiling 0`; and `go test -count=1 ./internal/standards/` →
  FAIL, Rule 17 NEW violation at `cmd/agent/merge_lock_test.go:200`.
- **s0 residual re-check (all 29 real-content rows)** — e.g.
  `git log -1 --format='%h %ad %s' --date=short -- internal/tools/read_symlink_test.go`
  → `7f0427eb 2026-08-29 fix(tools): symlink-aware read/write/edit — refuse link paths
  by default`; `git log master --oneline --grep='feat/job-family-t3'` → `5452a3e0
  resolve merge feat/job-family-t3 into master`; `git ls-tree -r --name-only master`
  → `cmd/agent/cli_shot.go`, `internal/shot/`, `devdoc/chat-table-design.md`,
  `devdoc/memroadmap-m3.md`, `devdoc/stale-merge-audit-20260916.md`,
  `tasks/01a09d07-no-privilege-daemon-report.md`. Each row's deciding ref is recorded
  in the shard's `evidence_result` cell.
- **D9 site read** — `sed -n '1955,1963p' cmd/agent/cli_tasks.go` → `:1961
  writebackOutcome(target, tools.TaskStatusFailed, "gopi run failed: "+runErr.Error()
  +taskAbnormalChildMarker+stats, …)`; the sibling writer is at `:1686`.
- **D8/D10/D-S6-1 site reads** — `internal/tools/task_staleness_guard.go:126`
  (`mergeVerifyStaleLogWindow = 40`) and `:287`
  (`MergeVerifyStaleVerdictBranchNotFound = "branch-not-found"`);
  `internal/tools/tasks.go:2895` (`taskStatusCancelled: {}`), `:2896`
  (`taskStatusVerified: {}`), `:4726` (`const taskTagResolved = "resolved"`), `:5053`
  (`func FilterArchivedRows`), `cmd/agent/cli_tasks.go:4456` (the call) and `:211-212`
  (the documented filter).
- **Recovery list** — `sqlite3 -readonly … select task_id,status … from tasks where
  task_id in (…17 ids…)` → 8 `completed`, 7 `pending`, 2 `in_progress`
  (`01a0ab47-28c7…` D3, `01a0b4dc-771d…` D13) — no row dropped, none added since the
  batches filed them.
- **Stage 5 recovery** — `git checkout feat/ledger-treasure-hunt-stage5 --
  devdoc/ledger-treasure-hunt-20260917-rows-stage5-resolved.csv
  devdoc/ledger-treasure-hunt-20260917-stage5.md` → both added, commit `9c7611e6`.
- **Build / tests** — not run: this branch touches only `devdoc/` (three new documents,
  one new shard CSV, two new scripts) and the recovered Stage 5 files; no `.go` file is
  modified, so there is no build or test surface to regress. The two gate runs quoted
  above were executed as *evidence for the defect list*, not as this branch's suite —
  both FAIL on master, which is defect D13, not a regression introduced here.
- **Worktree clean** — `git status --porcelain` → (no output) after the commit.

## 8. Not Done (honest)

- **7 HP-origin rows keep a verdict that rests on partial evidence** (Stage 4's re-check
  table). HP run logs, the HP ledger, `peerbox-mirror/` and the audit DB are absent on
  this host; the verdicts are settable here, the *re-checks* are not. They are named in
  Stage 4 §"Rows that must be re-checked on HP", not silently upgraded.
- **`01a0a044-166a-75ed-90f6-a546f024d345`** (an independent-verification run of
  `01a09d07`, verdict ALREADY-DONE) was verdicted from its *target's* landing
  (`tasks/01a09d07-no-privilege-daemon-report.md` is on master), not from its own
  process trail — its session dir carries no readable run. The verdict is the one a
  re-check would produce, but it is one inference deep, and it is recorded as such.
- **Three stage0-only rows were verdicted on the shape rule without an individual git
  call**: the two `cost-probe.txt` rows and `01a08c34` (`/tmp/llm-shot.png`). The probe
  file and the screenshot path were `/tmp` artifacts on hosts that no longer hold them
  (Rule 22's sweep retired that class of scratch), so there is nothing left to check —
  NO-VALUE by shape (R4), stated rather than implied.
- **The two D12-lineage rows `01a0a92d-560e` / `01a0a92d-77a0`** (no jsonl at all in
  their session dirs) were verdicted with their successor (`01a0a92d-77a0` → #9). The
  *first* of the pair has no successor of its own and is NO-VALUE.
- **This report does not re-litigate the batches' per-row reasoning.** Where a batch's
  evidence cell is terse, the shard CSV remains the authority; §2's table is the
  reconciliation, not a second audit.
- **No ledger row was repaired.** The defects above are reported, not edited — the
  ledger is append-only evidence (Rule: never delete or mutate; read-only SQL only).

## 9. Evidence (self-filed)

§7 is the evidence list for this stage — every gate claim there carries the command and
the output that was seen (the reconciliation run's printed tallies, the two `--check`
runs, the md5 pin, the two conflict re-measurements, the per-row s0 re-check refs, the
defect-site reads, the recovery-status query). It is deliberately in one place: a gate
claim repeated in two sections is a claim that can go stale in one of them.

The two artifacts that carry the per-row substance — not summaries of it — are:

- `devdoc/ledger-treasure-hunt-20260918-rows-final.csv` (319 rows: the verdict, the
  shard that decided it, and the successor task for every live row), regenerated by
  `…-consolidate.py` and verified byte-identical by `--check`.
- `devdoc/ledger-treasure-hunt-20260917-rows-stage0-residual.csv` (68 rows: the 29
  real-content rows each carrying the git ref that decided them, the 39 shape-rule rows
  each carrying their R4 ground).

## 10. Deliverable declaration

- **Branch**: `feat/ledger-treasure-hunt-stage6` (worktree
  `/home/user/workspace/gopi-treasure-stage6`, base `0317d09a`).
- **Recovery**: `git show feat/ledger-treasure-hunt-stage6:devdoc/ledger-treasure-hunt-20260918-final.md`
  and `git show feat/ledger-treasure-hunt-stage6:devdoc/ledger-treasure-hunt-20260918-rows-final.csv`.
- The commit is in the shared object store of the main repo (`~/work/GoPi`) — the
  worktree is a linked worktree, so **no push is needed** (Rule 6c).
