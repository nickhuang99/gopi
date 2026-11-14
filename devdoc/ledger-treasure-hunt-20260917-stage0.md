<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 0 (worklist + bulk classes)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-17 · Branch: `feat/ledger-treasure-hunt-stage0` · Epic: `01a0ab1c-e5c1-75ac-8992-ea71c58c1287`

Read-only audit of every `failed` (126) + `cancelled` (193) row in `~/.gopi/memory.db`.
**Zero ledger mutation**: every query in this report ran under `sqlite3 -readonly`.
No `UPDATE`/`DELETE`/`INSERT` was issued; the archive/merge tools were never invoked.

## Completed

- The per-row index of all **319** rows — `devdoc/ledger-treasure-hunt-20260917-rows-master.csv`
  (320 lines = header + 319 rows; columns: task_id, status, source, class, sub_class,
  created_at, resolved, evidence_available, summary).
- The class partition, reproduced twice from two different precedence orders (below).
- Git forensics on the three bulk classes (sync-yielded, wrapper:t-*, auto-prefixed),
  including a full `git cherry` sweep of the archive tags they name.
- Six measured ledger-mechanism defects (D1–D6) with file:line / commit evidence.
- Five reusable disposition rules (R1–R5) for the remaining batches.
- Three confirmed recoveries submitted as new tasks (this stage's STILL-NEEDED subset).

## Not Done (honest)

- Stages 1–6 (`01a0ab3e-cb61/d17f/d3b1/d55c/d6f6/d926`) hold the per-row verdicts for
  319 − 17 = **302 rows**. This stage produces verdicts for the 17 wrapper rows and
  the two sync-yielded real rows, and class-level dispositions (with the mechanical
  rule stated) for the rest. Per-row verdicts for the four curated/HP/merge batches
  are NOT in this document.
- `hp/feat/*`, `hp/master` branch names named by merge-backlog rows resolve to **no
  ref on this host** — those verdicts are deferred to Stage 4 with the caveat recorded.

## Statistics

### Raw figures

| quantity | count | command |
|---|---|---|
| `status='failed'` | 126 | `select status,count(*) from tasks where status in ('failed','cancelled') group by status` |
| `status='cancelled'` | 193 | (same) |
| total | **319** | (same) |

### The filtered CLI view, reconciled exactly

`gopi task list --status failed` **hides rows whose tags contain `resolved`** (the help
text says so: *"A `--status failed` view shows only UNRESOLVED failures — rows already
archived (tags contain "resolved") are hidden; `--all` includes them"*). Measured on
this host, so the epic's demand to reconcile against the filtered view is closed
exactly, not approximately:

| view | command | rows |
|---|---|---|
| failed, filtered | `gopi task list --status failed` | **61** |
| failed, unfiltered | `gopi task list --status failed --all` | **126** ✓ = raw |
| cancelled, filtered | `gopi task list --status cancelled` | **191** |
| cancelled, unfiltered | `gopi task list --status cancelled --all` | **193** ✓ = raw |
| resolved-tagged, failed | `tags LIKE '%resolved%' AND status='failed'` | **65** |
| resolved-tagged, cancelled | `tags LIKE '%resolved%' AND status='cancelled'` | **2** |

Both gaps are the same quantity: 126 − 65 = **61** ✓ and 193 − 2 = **191** ✓. So the
filtered view is not a second, disagreeing count of the backlog — it is the same 319
rows minus the 67 already-disposed ones, and **any audit that reads the CLI alone
under-counts by 67**. Stage 5 audits exactly those 67 (dedup by `task_id`: 39 of them
are `auto-prefixed`, 17 `wrapper:t-*`, 6 `HP-origin`, 4 `merge-lane`, 0 `sync-yielded`
— the tag overlaps the mechanical classes, so it is nowhere near disjoint and cannot be
subtracted as a class).

### The two partitions (both sum to 319, both purely rule-driven)

The epic's briefing and this stage measured **different** class sizes from the same
319 rows. The divergence is entirely **predicate precedence**, and reproducing the
epic's exact numbers pinned the mechanism (D6).

**The epic's seven figures reproduce — but only as precedence-ordered RESIDUALS.**
Measured, each against the rule stated:

| epic class | epic n | measured predicate | measured n |
|---|---|---|---|
| auto-prefixed | 89 | `substr(summary,1,6)='[auto]'` **minus** wrapper-tagged | **89** |
| sync-yielded | 18 | `tags LIKE '%sync-yielded%'` | **18** |
| wrapper:t-* | 17 | (class total, incl. `resolved`) | **17** |
| merge-lane | 30 | — **no single predicate matches 30**; it is the *residual* left after the other six are removed | 30 |
| HP-origin | 29 | HP-tagged **minus** `[auto]`-prefixed (i.e. what `auto` left behind) | **29** |
| curated failed | 51 | what is left in `status='failed'` | 51 |
| curated cancelled | 85 | what is left in `status='cancelled'` | 85 |

Two facts fall out, and they must be stated because they govern every child batch:

- `[auto]`-prefixed **overlaps HP** (21 rows) and **wrapper** (17 rows): the 106
  `[auto]` rows are 89 (no wrapper tag) and 21 of those carry an HP tag. "89" is the
  non-wrapper count, "29" is the non-`[auto]` HP count — the two predicates are not
  independent, so **the epic's 89 and 29 are order-dependent, not disjoint definitions.**
- `merge-lane 30` has **no predicate**. The union of every merge tag shape is **17**
  (`merge-verify|branch-rework|merge-conflict|auto-spawn`), 33 with `merge-backlog`
  summaries added, 41 with `summary LIKE 'merge%'`, 70 with `tags LIKE '%merge%'`.
  Since 89+18+17+29+51+85 = 289 = 319 − 30, the epic's "merge-lane 30" is the
  **leftover**. A child batch told "audit the 30 merge-lane rows" cannot compute its
  own worklist from a rule.

**This stage's partition (B)** — every class has an explicit predicate, HP-first,
and the remainder is stated, so the batches are disjoint and each is computable:

| class | n | failed | cancelled | predicate |
|---|---|---|---|---|
| curated-cancelled | 93 | 0 | 93 | remainder, `status='cancelled'` |
| auto-prefixed | 71 | 31 | 40 | `substr(summary,1,6)='[auto]'`, not taken above |
| curated-failed | 56 | 56 | 0 | remainder, `status='failed'` |
| HP-origin | 50 | 18 | 32 | HP tags (incl. `worker:hostb-Laptop`) |
| sync-yielded | 18 | 0 | 18 | `tags LIKE '%sync-yielded%'` |
| merge-lane | 17 | 13 | 4 | merge tags ∪ `summary LIKE 'resolve merge conflict%'` |
| wrapper:t-* | 14 | 8 | 6 | `tags LIKE '%wrapper:t-%'` |
| **total** | **319** | 126 | 193 | |

Note `substr` is used rather than `LIKE '[auto]%'`: SQLite `LIKE` has **no bracket
expression**, so `summary LIKE '[auto]%'` returns **0** (and `'[[]auto]%'` returns 0
too — verify with `select 'x' like '[auto]%'`). Any audit or gate counting this class
by `LIKE` silently measures nothing. Recorded as D5.

### Prior disposition (must not be double-counted)

`tags LIKE '%resolved%'` overlaps the mechanical classes heavily (measured):
39 auto, 17 wrapper, 6 HP, 4 merge, 0 sync-yielded — 67 rows total (65 failed + 2 cancelled).
Stage 5 is a **confirmation pass deduped by task_id**, never a count of the tag.

## Per-class forensics

### sync-yielded (18) — 16 phantom merge-backlog rows + 2 real rows

16 of the 18 are `merge-backlog: <branch> unmerged — merge/archive decision needed`.
They exist because the serve branch-scan **re-armed** every tick: fix commit `7162092b`
("terminal rows mask") records the mechanism — a decision row reaching a terminal state
without the branch being merged re-armed the branch on the very next tick, and because
the summary is a pure function of the branch name each tick minted a **byte-identical
row** (591 rows over 10 branches in the 2026-09-10 audit). That is defect **D1**.

The 8 distinct branches they name, resolved as far as this host allows
(`git rev-list --count master..<ref>`):

| ref named | resolves to | master..ref | verdict | evidence |
|---|---|---|---|---|
| `feat/claim-broadcast` | tag `archive/feat-claim-broadcast-20260901` | **0** | SUPERSEDED | master carries `53fb4a5d merge: feat/claim-broadcast — claim broadcast M4.18` |
| `feat/merge-backlog` | tag `archive/feat-merge-backlog-20260901` | **0** | SUPERSEDED | master carries `e2c5f402 adoption: merge feat/merge-backlog-single-source` |
| `fix/memsync-import-atomic` | tag `archive/fix-memsync-import-atomic-20260901` | **5** | **STILL-NEEDED** | the 5 commits (482 insertions) are absent from master; see below |
| `hp/feat/claim-broadcast` | no ref on this host | — | UNCLEAR | HP-side ref; Stage 4 |
| `hp/feat/merge-backlog` | no ref on this host | — | UNCLEAR | HP-side ref; Stage 4 |
| `hp/fix/memsync-import-atomic` | no ref on this host | — | UNCLEAR | HP-side ref; Stage 4 |
| `hp/feat/serve-tick-git-sync` | no ref on this host | — | UNCLEAR | HP-side ref; Stage 4 |
| `hp/feat/task-id-collision` | no ref on this host | — | UNCLEAR | HP-side ref; Stage 4 |
| `hp/master` | local ref `hp/master` | **0** | SUPERSEDED | parked at `version/v20260916-1304-69975030` |

**The two non-phantom sync-yielded rows:**

- `t-1788147916` — `gopi run completed — run: elapsed=23m35s` (claim-broadcast work).
  Master carries the merge `53fb4a5d` → **ALREADY-DONE**
  (`git log --oneline master --grep='claim-broadcast'`).
- `t-1788210015` — "Fix 3 TestBidir2* failures: all-zero import stats on same-host
  bidir sim". The four `TestBidir2*` functions are live on master
  (`git grep -n 'func TestBidir2' internal/tools/memory_sync_bidir2_e2e_test.go` →
  ImportSymmetry / LiveConflictNeverClobbered / DivergentTerminalsConverge /
  ServeTickAutoSync), and two successors closed: `c7ce2aaf` [completed] (round-5 tools
  red-list naming the same 4 tests) and `t-1788266431` [completed] (reflow gate contract
  drift) → **ALREADY-DONE**.

#### Recovered: the memsync import-atomicity fix

`archive/fix/memsync-import-atomic-20260901` holds 5 commits master does not:

```
4b05426a fix(memsync): import-tasks single-transaction atomicity + busy_timeout 15s
68b89d5c docs(memsync): import-atomic task report
88a10f23 test(tools): pin GOPI_SYNC_PEERS in server-resolve test
9924831e test(memsync): concurrent same-patch loser may fail BUSY whole-batch
67264535 docs(memsync): full-suite green + concurrent-test loser-BUSY contract
  6 files changed, 482 insertions(+), 116 deletions(-)
```

master's `SyncTaskImport` has **no transaction** — verified by
`git show master:internal/tools/memory_sync_task.go` (the function body from line 545
contains no `BeginTx`, no `Rollback`), and
`git grep -n 'importTaskTxDB|isImportRefusal|importTaskInsertTx' master -- internal/tools/`
returns nothing. The archived version's own doc comment states the subject:
*"a mid-patch SQLITE_BUSY or crash leaves ZERO rows written — the epic's
'import 半途失败 = 部分行入库?' answer is now 'impossible by design'."*
The originating row (`5a69162b` / `9440de12`, both [completed]) asked exactly that
question and the fix that answers it never landed → **STILL-NEEDED**, submitted.

### wrapper:t-* (17) — run identities, not work identities

11 carry `resolved`, 6 do not. A wrapper row is the **run** the child was spawned
from (`GOPI_TASK_RUN_ID`; `internal/tools/tasks.go:5468` — *"'completed' when the child
was launched, 'failed' when the wrapper never launched it"*). Its status therefore
records the **wrapper process's** fate, not the work's. The wrapper rows are
NO-VALUE **as work items**; the work must be judged from the child/successor row or
from git. Sample where the work is checkable and checkable-done:

| row | status | work named | git evidence | verdict |
|---|---|---|---|---|
| `075d366b` | failed+resolved | `gopi worker -h` runs the worker loop | master `9e24d228 fix(cmd/agent): service help is capability-first, not a devdoc (worker/merger -h)`, `9e6dfba7 fix(worker): refuse unknown argv words before the loop starts` | ALREADY-DONE |
| `91ad8a5c` | failed+resolved | stuck-detection toolStreak false-positives | `git log master --grep='idle-streak|stuck-detection'` → `ba3ce543 fix(worker): idle-exit after N drained rounds`, `8eb0207b fix(thinking)` | ALREADY-DONE |
| `4896b1f0` | failed+resolved | serve-tick full-counter contract | `git log master --grep='serve-tick'` → `6e6a7d81 Merge branch 'feat/serve-tick-git-sync'`, `f70dfacf fix(serve): merged-branch cleanup pass` | ALREADY-DONE |
| `63aed300` | cancelled | session log path in `task show` | `7085c12f feat(tasks): session log path in show + task_id/session_id consolidation` | ALREADY-DONE |
| `1d51eba4`, `997292f2`, `t-1788343430`, `t-1788343459` | cancelled/failed | `prompt test`, `t` | no work identity in summary; no payload | NO-VALUE |

The `t-1788343430`/`t-1788343459` ("t") and `1d51eba4`/`997292f2` ("prompt test") rows are
**smoke prompts promoted to ledger rows** — same family as the `[auto]` probes below.

### auto-prefixed — a heterogeneous class, NOT uniformly void

The class is 106 rows by `substr(summary,1,6)='[auto]'`; **71** of them are outside every
higher-precedence class (partition B) and 89 carry no wrapper tag (the epic's figure).
It is heterogeneous and splits by measured summary shape:

| sub-shape | n | disposition |
|---|---|---|
| `— chat: backfilled` (interactive chat turns backfilled into the ledger) | 25 | NO-VALUE — the work identity is the operator's probe, not a task |
| test fixtures (`Run the failing deploy task`, `failing chat turn`) | 11 | NO-VALUE — deliberately-failing fixtures |
| summary ≤ 25 chars (`config schema`, `status`, `-z`, `ELF`, `version`, `work`) | 14 | NO-VALUE — CLI probes with no work content |
| real bug-fix / architecture work (`BUG FIX`, `bug056`, `ARCHITECTURE FIX`) | 5 | per-row git check (Stage 0 flags; Stage 1/2 verdict) |
| real feature briefs (`背景（nick…`, `特性设计`, `预见性审查`) | 11 | per-row git check — these are nick-authored briefs, the **highest-signal** rows in the class |
| remainder (mixed) | ~14 | per-row git check |

The uniform rule for this class is therefore **shape-first, not tag-first**: a row whose
summary is a chat turn, a fixture, or a ≤25-char token has no work identity and is
NO-VALUE by rule; anything carrying a brief or a defect statement gets a git check.
Classifying the whole 89/93 as void would bury 16 real briefs — measured, not assumed.

### merge-lane (17 by the unions-rule; oracle 30) and the archive-tag family

Only 17 rows carry a merge tag or a `resolve merge conflict` summary. The **archive
tags** are the larger instrument: 95 exist. `git cherry -v master <tag>` proves the
patches are absent for the sampled tags (`+` = not applied):

```
archive/feat/chat-m2-status-wake           + ff9927a0 wip(chat-m2-status-wake): salvage by PA
                                           + d8596127 fix(chat): one terminal writer — status refresh is a publish, not a draw
archive/feat/cwd-release-inventory-verified + 455a7751 test(standards): verify the cwd-release producer inventory by census
archive/feat/worker-run-entry2              + 0aa03689 wip(worker-run-entry2): salvage by PA
archive/fix/hp-positional-param-submit      + 54c3ba25 fix(tasks): isTasksSubcommand misroutes config-flag pairs as ssh lanes
archive/log-stats-raw-gopi                  + 65571649 feat(logstats): gopi stats + gopi raw subcommands
```

Ancestry sampling (12 tags, uniform sample): **11 of 12 are NOT ancestors of master**,
and their patches are unapplied by `git cherry`. `archive/log-stats-raw-gopi` is the
one whose content *is* superseded (`cli_stats.go` / `cli_raw.go` are on master via
`cbe564ae` and `2b6586e7`). So an archive tag records *"we stopped carrying this"*,
**not** *"this was done"* — the reverse of what the epic's premise assumed for 22/29.
Pending queue coverage: 30 of the 95 tags have a `resolve merge conflict … parked at
archive/<name>` row queued; **65 tags have no repair row at all.**

## Child-task contract — Stages 1–6 MUST read this before classifying

The reframe roadmap's per-stage briefings were written from the epic's own seven
figures, and Stage 0 **disproved those figures as predicates** (D6). A child that
computes its worklist from "the 30 merge-lane rows" or "the 51 curated-failed rows"
will not find that many rows, and may double-count rows another stage owns. The
binding worklist is the CSV, not the briefing's number:

1. **Worklist**: `devdoc/ledger-treasure-hunt-20260917-rows-master.csv` (all 319 rows,
   generated by the single statement under *Reproduction*) on branch
   `feat/ledger-treasure-hunt-stage0`. Until that branch is merged, reach it without
   checking anything out (Rule 29 — never borrow another task's worktree):

   ```bash
   git -C /home/user/work/GoPi show feat/ledger-treasure-hunt-stage0:devdoc/ledger-treasure-hunt-20260917-rows-master.csv \
     | awk -F, 'NR==1 || $4=="merge-lane"'      # ← your stage's class, exactly
   ```

   A byte-identical copy also sits in the epic's session dir, readable by every child on
   this host without any git call (md5 `ff312577ccb7eb0406b520cce31b15cb`, 319 rows) — a
   child may slice either one, they are the same bytes:
   `/home/user/.gopi/sessions/01a0ab1c-e5c1-75ac-8992-ea71c58c1287/rows-master.csv`
   (with `rows-stage0-canonical.csv` for the three bulk classes).
   Each child's own worktree is short-lived; the session dir is not — prefer it when
   your worktree has been recycled mid-task.
2. **Your class is the `class` column** of partition B (`HP-origin` / `wrapper:t-*` /
   `sync-yielded` / `merge-lane` / `auto-prefixed` / `curated-failed` /
   `curated-cancelled`). The epic's numbers map onto it as **residuals**, never as
   predicates: epic "merge-lane 30" = this CSV's `merge-lane` **17** plus rows the
   higher-precedence classes took; epic "curated failed 51" = `curated-failed` **56**
   once the HP/wrapper/auto rows are lifted out first. Do not re-derive a class count
   with a second query — the ledger is live and the count drifts (D7); slice the CSV.
3. **Rows already disposed of elsewhere**: the 67 `resolved`-tagged rows are Stage 5's
   confirmation pass and belong to no other stage even when they sit in your class
   (the CSV's `sub_class` column marks them `failed+resolved`); Stage 5 dedups by
   `task_id`.
4. **Emit to your own shard**, one line per row:
   `devdoc/ledger-treasure-hunt-20260917-rows-<batch>.csv`, header
   `task_id,verdict,evidence_cmd,evidence_result` — Stage 6 concatenates the shards.
5. **A verdict with no command is not a verdict.** Every line carries the command that
   produced it *and* its output (R1/R2/R3). `git merge-base --is-ancestor` is NOT an
   acceptable merge proof — it false-positives on a ref parked at the master tip. Use
   `git rev-parse --verify <ref>` then `git rev-list --count master..<ref> == 0`; a ref
   that does not resolve is **UNCLEAR, never merged**.

## Stage 1 — merge-lane (17/17 classified)

Batch shard: `devdoc/ledger-treasure-hunt-20260917-rows-stage1-merge-lane.csv`
(`task_id,verdict,evidence_cmd,evidence_result` · 17 rows · coverage proved by
`diff` against the master CSV's `merge-lane` slice → **exact match, no extras**).

| verdict | n | rows |
|---|---|---|
| ALREADY-DONE | **14** | t-1788991014, t-1788994920, 01a089a7, 01a0967d, 01a09694, 01a096b5, 01a096c9, 01a0998c, 01a09a03, 01a0a203, 01a0a4a4, 01a0a59a, 01a0a7e9, 01a0a9e0 |
| SUPERSEDED | **3** | t-1789013949, t-1789015454, 01a0a211 |
| STILL-NEEDED / PARTIALLY-DONE | **0** | — |
| UNCLEAR | **0** | — |
| **total** | **17** | |

**Every one of the 17 is resolved: zero rows in this lane hold unlanded work.** That is
the lane-level answer to the epic's premise, and it is the opposite of what the ledger
said (17/17 terminal-failed-or-cancelled, 0 of 17 actually outstanding).

### How each verdict was reached (evidence, never the summary)

The three dispositions are mechanically distinguishable, and the distinction is the
value of this batch:

**(a) ALREADY-DONE — the branch landed and was then DELETED, which the row read as
failure.** For 11 of the 14, the proof is a merge commit on master naming the branch:

```bash
git log master --oneline --grep=<branch>       # 5452a3e0, 0e172c07, d75818e8,
                                               # 3ae65866, cbbe9cc4, 833cc2b6,
                                               # f178840f, 3c579375, f7fbaa4b, …
```

`git rev-parse --verify refs/heads/<branch>` returns **exit 1 for all 15 named branches**
— there is no live branch, and for 12 of them no `archive/*` tag either. Branch absence
is the *signature of a successful merge* (the merger deletes the branch after landing
it), yet three rows recorded it as the failure reason verbatim.

**(b) SUPERSEDED — master carries the same feature by a different organisation.** The
three rows (both `hippocampus-host` rows collapse into one finding) are proven by
content comparison, not by tag:

```bash
diff <(git show master:internal/hippocampus/brief.go) \
     <(git show archive/hippocampus-host-superseded-20260910:internal/hippocampus/brief.go)
→ IDENTICAL           # brief.go is byte-identical on master
git diff --stat master archive/fix/cwd-and-lifetime -- internal/worktreerelease/
→ (empty)             # master's worktreerelease == the tag's copy
git ls-tree master internal/procname/ → procname.go only, but master carries the
                                        functionality in internal/testutil/
                                        {daemonlifetime,daemonlifetestsupport,lifetimeprobe}.go
                                        + procname_triple_redproof_test.go
```

The tag holds 7 commits master lacks in each case — but the *feature* is on master
under a better-organised form (master's `host.go` names the live binding
`internal/agent/hippocampus_agent.go`; the tag names a stale
`hippocampus_binding.go`). SUPERSEDED is the correct verdict, **not** STILL-NEEDED: the
capability exists; only the earlier file layout does not.

**(c) Two rows were repaired by a DIFFERENT lane (the RC3 shape).**
`01a09694` (rework of `fix/task-show-run-cmd-verbatim`) and `01a0a203`
(`fix/daemoncwd-scratch-pin`) both name work that master repaired elsewhere:

```bash
git log master --oneline --grep=fix/task-show-run-cmd-verbatim
→ cbbe9cc4 adoption: merge fix/task-show-run-cmd-verbatim (99c8e9e5)   # landed anyway
→ b2e2407b Revert "Merge branch 'fix/task-show-run-cmd-verbatim'"      # …after a revert
→ 6e8e1220 rework(task-show): re-apply the run_cmd verbatim fix on current master
git merge-base --is-ancestor 44928156 master → ANCESTOR   # fix/stale-merge-cleanup
git grep -l TestAtMostOneTestMainPerSuite master
→ master:internal/daemoncwd/testmain_scratchroot_test.go
→ master:internal/testutil/scripts_gate_test.go
```

`01a0a203`'s own reason names the sibling fix (`fix/stale-merge-cleanup`) that fixed the
compile break — and that merge commit **is** an ancestor of master, plus the row's
stated remainder (the `TestMain` gate) is on master. A rework row filed for a red master
is worthless once another lane greens master — **this is RC3 measured on real rows, twice.**

### Two NEW ledger-mechanism defects, discovered in this batch

| id | defect | evidence |
|---|---|---|
| **D8** | **the merge-verify staleness guard reads BRANCH ABSENCE as merge failure.** A successful merge deletes its branch; the guard then runs `git rev-parse --verify --quiet refs/heads/<branch>` → exit 1 → **cancels** the row with reason `[merge-verify staleness guard] … no forward rework: branch-not-found`. Effect: the guard *institutionalises* the exact false-failure the epic opened with | rows `01a0a59a` (`feat/web-lan-access`) and `01a0a9e0` (`feat/merger-status-log`), both carrying the reason verbatim; both branches have merge commits on master (`51edd8d9`/`b16663b9`, `e49d1760`/`15060a5c`). Structural sibling of D2 (terminal state on an unmerged branch) — D2 is "verified while unmerged", D8 is "cancelled *because* merged" |
| **D9** | **a transient infrastructure death is recorded as a permanent work verdict.** The resolver child was killed by its own 20-minute timeout, the merger restarted, or the owner process vanished — and the row lands `failed` with `reason='task failed'` or `'child timed out after 20m0s'`, indistinguishable from a real failure | `t-1788991014` (reason: timed out; work landed `5452a3e0`), `01a089a7` (child signal: killed; work landed `d75818e8`), `01a0a7e9` (owner process gone; work landed `566cf91c`), `01a09694`/`01a096b5`/`01a096c9`/`01a0998c` (`reason='task failed'` only, all four landed) |

**D8 is the higher-value find**: it is a *code* defect that manufactures false failures
on every sweep, not a bookkeeping slip. It is a distinct mechanism from D2 and is
reported as a submitted task (below), because the same guard may cancel the row while
the work is safe — an operator reading `cancelled` has no way to know the merge
succeeded.

**D8's root cause was corrected mid-audit, and the correction is itself a finding.**
The first statement of D8 ("the guard reads branch absence as merge failure") was
*almost* right and would have sent a fixer to the wrong code: the guard does look at
master, it just looks through a 40-commit window with a one-spelling token parser. The
audit caught this by opening `internal/tools/task_staleness_guard.go` and *reading* the
fallback path instead of stopping at the reason string. Both the imprecise task and the
corrected one are in the queue (`01a0ab4d…`, superseded by `01a0ab4e…`) — a wrong root
cause left standing is worse than a missing fix, because it is actionable and wrong.

### Disposition rules, sharpened by this batch

- **R1 addendum — branch absence is EVIDENCE OF SUCCESS, not failure.** Test the branch
  first (`git rev-parse --verify --quiet refs/heads/<b>`); if it is gone, the merge
  commit is the proof (`git log master --oneline --grep=<branch>`) and the row is
  ALREADY-DONE. A missing branch never downgrades a verdict to UNCLEAR by itself.
- **R6 (new) — a terminal row that a LATER lane repaired is ALREADY-DONE.** `git log
  master --oneline --grep=<branch>` finds the adoption merge even when a revert
  intervened (`b2e2407b` then `cbbe9cc4`). The row's own `reason` may name a sibling
  lane that fixed it (`01a0a203` → `fix/stale-merge-cleanup`) — check whether *that*
  merge landed.
- **R7 (new) — an archived branch whose FEATURE is on master under another layout is
  SUPERSEDED, not STILL-NEEDED.** Prove by content, not by file list:
  `diff` the surviving files and `git diff --stat master <tag> -- <the package>`; if the
  package is identical or master's version is strictly better (a live reference vs a
  stale one), the remaining commits are an earlier organisation, not missing work.

### Recovered work — submitted task from this batch

| original ledger row | why | new task |
|---|---|---|
| `01a0a59a` (cancelled) + `01a0a9e0` (cancelled) — i.e. **defect D8** | the guard's landing witness misses the real merge subjects and is bounded to a 40-commit window (measured landing: 363 deep); it cancels a row while the work is on master | **`01a0ab4d-19fb-72c3-82ee-e2e61c005dae`** (superseded by) **`01a0ab4e-f127-78d1-a126-9b80b9a0d335`** |

## Ledger-mechanism defects

| id | defect | evidence |
|---|---|---|
| **D1** | merge-backlog decision rows re-arm every tick after reaching a terminal state without a merge, minting byte-identical rows | `7162092b` commit message: *"591 rows over 10 branches"*; explains 16/18 sync-yielded rows |
| **D2** | **a merge-backlog decision row reaches terminal `verified` while the branch stays unmerged** — verify closes the ROW, not the MERGE | `t-1788218295` is `[verified]`; `git rev-list --count master..archive/fix-memsync-import-atomic-20260901` = **5**. The atomicity fix was dropped this way |
| **D3** | an archive tag is treated as evidence of completion; it only records abandonment | 11/12 sampled tags not master ancestors; `git cherry` shows the patches unapplied |
| **D4** | `resolved` overlap makes tag counts non-additive across classes | 39 auto + 11 wrapper + 6 HP + 4 merge = 60 of the 67 resolved rows also sit in a mechanical class |
| **D5** | SQLite `LIKE` has no bracket expression, so `LIKE '[auto]%'` returns **0** — any gate or audit counting this class by `LIKE` silently measures nothing | `select count(*) … where summary like '[[]auto]%'` → 0; `substr(summary,1,6)='[auto]'` → 106 |
| **D6** | class sizes are **precedence- and residual-dependent**; the epic's own seven figures have no shared predicate set — `merge-lane 30` matches **no** predicate, and `auto 89` / `HP 29` are each other's complement | measured table above: union of merge shapes = 17, with `merge-backlog` = 33, with `tags LIKE '%merge%'` = 70; 89+18+17+29+51+85 = 319−30 |
| **D7** | the ledger is a **live** target: `select count(*)` for the same class returned 79 then 71 across the audit (the daemon's sync/sweep rewrites rows between queries). An audit that re-derives its counts per query reports its own drift as findings | same query, repeated: `auto-prefixed` 79 → 71; total stable at 319/126/193 over 4 samples 15s apart |
| **D8** | **the merge-verify staleness guard's "did it land?" witness is unreliable, so it cancels rows whose work DID land.** The ref-gone path falls back to scanning master for a landing witness, and that scan fails two ways: (i) it is bounded to `-n 40` commits (`mergeVerifyStaleLogWindow = 40`), while the measured landing sits **363 commits deep** — the verdict therefore depends on the daemon's tick rate, not on the fact; (ii) its token parser accepts **only** the subject `adoption: merge <branch> (<sha>)`, while the merges actually on master read `Merge branch 'feat/web-lan-access'` and `resolve merge feat/merger-status-log into master` — invisible even inside the window. The guard then writes a terminal `cancelled` with reason `no forward rework: branch-not-found`, which an operator reads as "the work failed" | rows `01a0a59a` (`feat/web-lan-access`) and `01a0a9e0` (`feat/merger-status-log`), both cancelled, both merged on master (`51edd8d9`/`b16663b9`, `e49d1760`/`15060a5c`). Proof: `git log master -n 40 --format=%H \| grep -c 51edd8d9` → **0**; `git log master --format=%H \| grep -n 51edd8d9` → **363**; `rev-list --count master` → 3719; `git log master --format=%s --grep=feat/web-lan-access` → `Merge branch 'feat/web-lan-access'`. Fixed by tasks `01a0ab4d-19fb-72c3-82ee-e2e61c005dae` + **`01a0ab4e-f127-78d1-a126-9b80b9a0d335` (the corrected root cause — act on this one)** |
| **D9** | a **transient infrastructure death is recorded as a permanent work verdict** — resolver timeout, merger restart, owner process gone — landing `failed` with `reason='task failed'`, indistinguishable from a real failure | `t-1788991014` (timed out 20m; work landed `5452a3e0`), `01a089a7` (child killed; landed `d75818e8`), `01a0a7e9` (owner gone; landed `566cf91c`), `01a09694`/`01a096b5`/`01a096c9`/`01a0998c` (`reason='task failed'` only, all four landed) |

D2 is the defect that actually lost work: a row can be `verified` and the branch still
carry unlanded commits. It is reported separately as a submitted task because it is a
mechanism fix, not a row disposition.

## Reusable disposition rules

**R1 — Merge proof is a resolvable ref plus a zero count.** `git rev-parse --verify <ref>`
first, then `git rev-list --count master..<ref> == 0`. `git merge-base --is-ancestor`
false-positives on a ref parked at the master tip; a ref that does not resolve is
UNCLEAR, never merged. Print the resolved sha next to the count.

**R2 — An archive tag is evidence of abandonment, never of completion.**
`git cherry -v master <tag>` — a `+` line means the patch is not in master. Only
`git grep <symbol>` on master, or a matching merge commit, proves the content landed.

**R3 — A row's status is not evidence about its work.** The row's terminal state and
the work's state are independent observables. Check the branch named in the summary or
the symbol/behaviour named in the payload against master; the summary text alone is
never the verdict.

**R4 — Rows with no work identity are NO-VALUE by shape, before any git call.**
Wrapper rows (`GOPI_TASK_RUN_ID`), `— chat: backfilled` turns, deliberately-failing
fixtures, and ≤25-char CLI probes. Judge the *child* row for wrapper rows.

**R5 — Pin the precedence order before classifying.** `HP-origin > wrapper:t-* >
sync-yielded > merge-lane > auto > curated-failed > curated-cancelled`, each predicate
mutually exclusive and the remainder explicit. Without this the class sizes shift and
the batches double-count (measured: D6).

## Recovered work list — submitted tasks

| original ledger row(s) | why the work is real | new task |
|---|---|---|
| `t-1788218295` [verified] + `t-1788213995` `t-1788211772` `t-1788211810` `t-1788214035` [cancelled] (merge-backlog `memsync-import-atomic`) | the atomicity fix is absent from master (5 commits, 482 insertions); the archived branch is its only holder | **`01a0ab46-f1e2-7a6d-8b3c-67bee7d04425`** |
| `t-1788218295` as a mechanism defect (D2) | terminal `verified` on an unmerged branch — this row type can go green while nothing merged | **`01a0ab46-f3b0-70f8-aefd-480af2a5a14c`** |
| 66 `archive/*` tags with no repair row (D3) | 11/12 of a uniform sample carry unapplied patches; 66 of 95 tags were never audited | **`01a0ab47-28c7-7dbf-99e0-40ce8712610b`** |
| `01a0a59a` + `01a0a9e0` [cancelled] — defect **D8** (Stage 1 batch) | the staleness guard cancels the row of a branch that already merged, because a successful merge deletes the branch | **`01a0ab4d-19fb-72c3-82ee-e2e61c005dae`** |

All four were submitted with self-contained payloads naming the original failure, the
git evidence proving it, and the acceptance criteria.

### Verdict tally across delivered batches (2 of 7 classes closed)

| batch | rows in class | per-row verdicts delivered | ALREADY-DONE | SUPERSEDED | STILL-NEEDED | NO-VALUE | UNCLEAR |
|---|---|---|---|---|---|---|---|
| Stage 1 — merge-lane | 17 | **17 (complete)** | 14 | 3 | 0 | 0 | 0 |
| Stage 0 — wrapper:t-* | 14 | 8 sampled + class rule | 3 | 0 | 0 | 4 | 0 |
| Stage 0 — sync-yielded | 18 | 2 real rows (16 phantom, class rule) | 2 | 6 | 1 | 0 | 5 (HP refs) |
| Stage 2..6 — not yet classified | 270 | 0 | | | | | |

**Remaining after this batch: 302 − 17 = 285 rows** across `curated-cancelled` (93),
`auto-prefixed` (71), `curated-failed` (56), `HP-origin` (50) and the class-rule
remainders of `sync-yielded` / `wrapper:t-*`. Named, never a silent gap.

**The 17/17 result settles one lane's premise: merge-lane rows are not lost work — 100 %
are done or superseded, and 0 hold recoverable patches.** Three of the 17 were
mis-disposed by a *mechanism* (D8) rather than by a mistake, which is why this lane
produced a code fix instead of a recovery task.

## Reproduction

```sql
-- all 319 rows, class + sub-class (partition B — the canonical statement; the
-- master CSV is generated from exactly this)
with c as (
  select task_id,status,source,tags,summary,created_at,session_id,deliverable,
    case
      when tags like '%from-hostb-Laptop%' or tags like '%node:hp%' or tags like '%from-hp%'
        or tags like '%worker:hostb-Laptop%' then 'HP-origin'
      when tags like '%wrapper:t-%' then 'wrapper:t-*'
      when tags like '%sync-yielded%' then 'sync-yielded'
      when tags like '%merge-verify%' or tags like '%branch-rework%' or tags like '%merge-conflict%'
        or tags like '%auto-spawn%' or summary like 'resolve merge conflict%' then 'merge-lane'
      when substr(summary,1,6)='[auto]' then 'auto-prefixed'
      when status='failed' then 'curated-failed'
      else 'curated-cancelled' end as cls
  from tasks where status in ('failed','cancelled'))
select cls, count(*), sum(status='failed'), sum(status='cancelled') from c group by cls;
```

All queries ran as `sqlite3 -readonly ~/.gopi/memory.db`, with a bounded retry (the live
ledger returns `database is locked` under the running daemon; a single attempt yields
false empties — retry up to ~15× with a 2-3s sleep before believing a zero). Generate
the per-row CSV with **one** statement (class + projection in the same query, as the
master CSV is): re-deriving a class count in a second query against this live ledger is
how D7's 79-vs-71 drift is produced.


## Verification

- Build: not run in this stage (docs-only change; no `.go` file touched).
- Tests: not run in this stage (docs-only change).
- Ledger: **0 rows mutated** — every statement was a `SELECT` under `sqlite3 -readonly`.
- Main tree: untouched (`git status` in `/home/user/work/GoPi` shows no file from this work).
- Deliverable: this file + `ledger-treasure-hunt-20260917-rows-master.csv` on
  `feat/ledger-treasure-hunt-stage0`.

---

# Stage 1 (2/7) — merge-lane forensics: the full oracle worklist

**Batch**: 83 rows (`merge-verify` / `branch-rework` / `merge-conflict` / `auto-spawn`
tags, or a summary matching `resolve merge conflict`) · **shard**:
`devdoc/ledger-treasure-hunt-20260917-rows-stage1-merge-lane.csv` · **branch**:
`feat/ledger-treasure-hunt-stage1` (cut from master `5cdae25e`).

The briefing's oracle figure was **30** rows (18 failed + 12 cancelled). The live
predicate returns **83**, and Stage 0's own contract (D6/D7) already said why: the
briefing's numbers are *residuals*, and the ledger drifts. The worklist was therefore
re-derived from the query, not from the number:

```sql
select task_id, status, tags, summary, deliverable, reason
  from tasks
 where status in ('failed','cancelled')
   and (tags like '%merge-verify%' or tags like '%branch-rework%'
     or tags like '%merge-conflict%' or tags like '%auto-spawn%'
     or summary like '%resolve merge conflict%');        -- 83 rows: 70 cancelled + 13 failed
```

Coverage: the CSV's `merge-lane` slice is **17/17 inside** these 83 (Stage 0's shard,
re-verified per row and not re-derived), and **66 rows are new** — every one a
`merge-conflict,auto-dispatch` row minted *after* Stage 0's CSV was generated. The
shard's `task_id` set equals the query's set exactly (no extras, none missing), proved
by set equality, not by counting.

## Statistics

| verdict | n | of 83 |
|---|---|---|
| ALREADY-DONE | **73** | 88% |
| SUPERSEDED | **7** | 8.4% |
| PARTIALLY-DONE | **2** | 2.4% |
| STILL-NEEDED | **1** | 1.2% |
| NO-VALUE | 0 | — |
| UNCLEAR | **0** | — |
| **total** | **83** | every row carries a command and its observed output |

**The lane-level answer: 80 of 83 rows (96%) hold no outstanding work** — 73 already in
master, 7 superseded by master's better organisation. **Three rows hold real unlanded
work**, and none of the three failed for a merge reason at all.

### The one fact that decided 63 of the 83 rows

Not git — the **reason column**. 63 rows carry, verbatim:

> `superseded: the park/resolve-conflict mechanism is retired — merger becomes a
> cronjob with gopi-driven judgment (2026-…). Do not re-file.`

The park/resolve lane that minted these rows was **deleted from master** this same day
(`devdoc/merger-remove-park.md`, branch `feat/merger-remove-park`, cut from `5e6f0062`;
`cmd/agent/cli_merger_daemon.go`, `cli_merger_stop.go`, `merge_resolve_child.go` are
ABSENT from master). So the rows are void **as rows**. But a retired mechanism does not
retire the work it was carrying, and that is exactly what this batch had to separate:

- **44 of the 63** point at a branch whose archive tag holds **0 commits master lacks**
  (`git rev-list --count master..archive/<branch>` = 0) → ALREADY-DONE, the resolve row
  simply outlived its subject.
- **19 of the 63** point at branches the retired lane itself owned (park worktree
  release, merger daemon resume/stop, grandchild reconciliation) → **SUPERSEDED**: a
  daemon that no longer exists needs no daemon resume, and `devdoc/merger-remove-park.md`
  is the ruling. `git diff --name-only --diff-filter=A master archive/feat/worker-run-entry2`
  shows all 12 of its new files ABSENT from master and *every one* in the retired set
  (`cli_merger_daemon.go`, `merger_graceful.go`, `internal/tools/repo_sweep_park_test.go`)
  — that is the signature of the lane's removal, not of missing work.
- **3 of the 63** carried work the lane's retirement did **not** touch → the three
  findings below.

## The RC modes, measured — not asserted

| RC | what the briefing claims | measured in this batch | rows |
|---|---|---|---|
| **RC0** — rebuild version-probe false mismatch | "rebuild status unknown" | **NOT PRESENT.** No merge-lane row carries a rebuild/version-probe reason; the 4 rows matching `%rebuild%` at all are `cmd/agent` rebuild-gate tasks in other classes. RC0 is not a merge-lane mechanism | 0 |
| **RC2** — resolve lane marks failed after the work landed | resolve landed, re-merge did not | **PRESENT, 4 rows** — reason exactly `resolve child failed to resolve the conflict: resolve landed but re-merge did not (error)` ×3 and `(deleted)` ×1; all four branches are in master (`0e172c07`, `3ae65866`, `f7fbaa4b` …) | 4 |
| **RC3** — verify gate files rework for a red master another lane greened | rework row filed against a red master | **PRESENT, and the highest-value discovery.** 6 rows carry `merge-verify,branch-rework`; two of them (`01a09694`, `01a0a203`) name work a *different* lane landed. **And the mechanism is still live at tip**: `go test ./internal/stylegate/` is **RED on master 5cdae25e** (3 `test-sleep` + 1 `test-skip` vs *ceiling 0*, `FAIL … 19.8s`) while `./internal/standards/` and `./internal/heavytest/` are **ok**. A rework row filed for a red master is void the moment another lane greens it — which is why 2 of the 6 needed no repair | 6 |

### A fourth mode, found in the data rather than the briefing

**RC4 — the fix landed in a merged sibling worktree and the branch was abandoned.**
The lane's `resolve` child merges master into the branch (to resolve), commits that
merge, and is then killed by its own timeout. The re-merge never runs, the branch is
archived, and the **work is inside the branch's merge commit — reached only through the
mainline, not visible as an ancestor of the branch tip.** Two rows show it:

- `fix/merger-status-branch-account` — the branch's own commits are *not* in master, yet
  master carries the identical change as `69fe2e8c fix(merger-status): reconcile every
  local head into one named bucket` (an ancestor; `git merge-base --is-ancestor
  69fe2e8c master` → true). 96.3% of the branch's added lines are already on master.
- `fix/merger-tick-branch-account` — subject `9ffe128d the tick log line reconciles every
  local head (bug 01a0aa74, daemon face)` is exactly `69fe2e8c`'s change; 63.8% of added
  lines on master.

This is the trap the briefings warned about in the other direction: a row can look
unlanded **by branch ancestry** and be fully landed **by content**. The instrument that
settles it is not `rev-list` — it is *subject matching plus added-line presence*:

```bash
# per unlanded commit: is its subject already a master subject?
git log --format=%s master > /tmp/m.txt
git log --format=%s master..<tag> | grep -vxF -f /tmp/m.txt      # → the genuinely new subjects
# per branch: how much of its added text is already on master?
git show --format= --unified=0 <commit> | grep '^+' | grep -v '^+++' ...
git grep -F -h -f <that-file> master -- '*.go'
```

Two instruments were tried and **rejected** before this one, and the report keeps them
so the next agent does not re-walk them:

1. **`git merge-base --is-ancestor`** (already banned by the contract) — false-positives
   on a ref parked at the master tip.
2. **Two-dot tree diffs / introduced-symbol diffs** — a branch 650 commits behind master
   diffs the *world*: `feat/web-m1-b4b7` reports 158 "missing" test functions that are
   the whole of `cmd/agent`'s interim history, and `fix/cwd-and-lifetime` reports
   `internal/procname/identity.go` absent while master carries `internal/procname/`.
   With such a branch, `--diff-filter=A` lists master's *own newer* files as branch
   additions. **Only exact-subject matching and capability probes survive a 650-commit
   gap.**

## The unlanded work — 3 rows, each with its commit list

### STILL-NEEDED — `01a0ab60` · `feat/conflict-branch-rename` · tag `archive/feat/conflict-branch-rename` (`abb4a062`)

```
ab22b4a0..abb4a062  (12 commits master lacks, 697 files differ vs master)
  … conflict/ naming helper: ConflictBranchPrefix, IsConflictBranch, ConflictName
git grep -ln 'ConflictBranchPrefix\|ConflictName' master -- '*.go'   → EMPTY
git log --format=%s master..<tag> minus master's subjects             → 12/12 new
```
The spec IS on master (`briefings/brief-conflict-branch-rename.md`); the implementation
is not. → task **`01a0b4aa-6874-710f-9380-e39910a2d8f4`**.

### PARTIALLY-DONE — `01a0ab1e` (`feat/archive-why-gate`) and `01a0ab7c` (`feat/hand-archive-gate`)

```
git ls-tree master briefings/ --name-only | grep archive → briefings/brief-archive-gate.md   (spec LANDED)
git cat-file -e master:internal/tools/archive_why.go     → ABSENT
git grep -ln 'ArchiveDecide\|ArchiveRecord\|ArchiveBacklog' master -- '*.go' → EMPTY
git grep -ln 'hand-archive\|HandArchive' master -- '*.go'                    → EMPTY
git rev-list --count master..archive/feat/archive-why-gate    → 4   (tip eac3b475)
git rev-list --count master..archive/feat/hand-archive-gate   → 8   (tip c7569bcc)
```
Two rows, one missing capability: master requires no reason to hand-archive a branch.
Both submitted as **one** task (the same gate, two faces) —
**`01a0b4aa-69af-739c-a381-92e2a031578f`**. The production delta is small (13 and 27
touched files; 3 and 0 byte-identical on master) — recovery, not re-implementation.

### SUPERSEDED (7) — kept separate from the above, because they are *decisions*, not gaps

| row | branch/tag | why superseded (what master has instead) |
|---|---|---|
| `t-1789013949`, `t-1789015454` | `archive/hippocampus-host-superseded-20260910` | Stage 0's finding, re-verified: `brief.go` byte-identical on master, master's `host.go` strictly better |
| `01a0a211` | `archive/fix/cwd-and-lifetime` | master carries `internal/worktreerelease/release.go` + `internal/procname/` + `internal/testutil/{daemonlifetime,lifetimeprobe}.go`; the tag's absent files are the retired lane |
| `01a0aabc` | `archive/feat/worker-run-entry2` | all 12 new files belong to the removed merger-daemon/park lane |
| `01a0ab12` | `archive/fix/merger-park-worktree-release` | implements the park lane master deleted (10 commits) |
| `01a0ab9e` | `archive/fix/merge-verify-landing-witness` | master `7bfdc6a5` repaired the same guard *better*: a full-history search over master, "never a window" (`task_staleness_guard.go:45–67`), vs the branch's 40-commit-window fix (`c327fd52`) |
| `01a0abe8` | `archive/wip/cwd-release-resolve` | `rev-list --count master..tag` = 0 |

## Ledger-mechanism defects added by this batch

| id | defect | evidence |
|---|---|---|
| **D10** | **a retired mechanism's rows are void as rows but are read as void as work.** One `reason` string ("the park/resolve-conflict mechanism is retired") terminates 63 rows, and the only way to know whether each row's *work* survived is to go to git per row. 44 had landed; **3 had not, and would have been lost silently** — the reason text says "do not re-file", which is precisely the instruction that buries them | the 63-row census; the 3 findings above |
| **D11** | **the resolve lane's own merge makes unlanded work look landed — and landed work look unlanded.** The resolver merges master *into* the branch before being killed, so (a) added-line/symbol diffs against such a branch measure master's own history (false "missing"), and (b) master can already carry the change via a sibling merge commit while the branch shows as unlanded (false "not landed") | RC4 rows `01a0aaea`, `01a0aabc` vs master `69fe2e8c` (an ancestor) |
| **D12** | **`rev-list --count` alone is not a completeness proof.** Zero unlanded commits proves the *tag* holds nothing master lacks; it does not prove the work exists — and the reverse (a non-zero count) does not prove the work is absent | 45 rows decided by `count = 0`; 18 with non-zero counts of which 11 proved landed-by-content |
| **D13** | the RC3 mechanism is **live at tip**: master's own gate suite is red (`internal/stylegate`: 3 `test-sleep` + 1 `test-skip`, ceiling 0, `FAIL`) while the row-level repair queue is where such reds get filed — so any rework row filed today for that red is already void | `go test ./internal/stylegate/ ./internal/standards/ ./internal/heavytest/ -count=1` → `FAIL github.com/nick/gopi/internal/stylegate 19.787s`; `ok internal/standards 25.337s`; `ok internal/heavytest 4.193s` |

**Note on D13 — already owned, so NOT re-submitted.** The red is tracked: ledger row
`01a0b3d6` ("internal/stylegate TestStyleGate is RED on master: 5 test-sleep + 1 …",
`completed`) and `01a0b47e` (`pending`, "rework: fix/stylegate-ceilings-ratchet").
Verified by query, not by assumption:

```bash
sqlite3 -readonly ~/.gopi/memory.db "select task_id,status,substr(summary,1,70) from tasks
  where summary like '%stylegate%' order by created_at desc limit 8;"
```

## Reusable rules, sharpened by this batch

- **R8 (new) — a `superseded:` reason retires the ROW's mechanism, never the WORK.** Open
  the reason's own claim and test it against git: `git rev-list --count master..archive/<b>`
  (0 → ALREADY-DONE) then, if non-zero, per-commit subject matching. 3 of 63 rows were
  carrying live work behind that sentence.
- **R9 (new) — a branch that merged master into itself reports the WHOLE mainline as its
  own diff.** Never read a two-dot tree/symbol diff as "unlanded" across a large gap;
  require exact-subject matching (branch subjects ∖ master subjects) or a capability probe
  (`git grep -ln '<symbol>' master -- '*.go'`).
- **R10 (new) — a capability that master replaced by a better implementation is
  SUPERSEDED even when the branch's commits are absent** (`merge-verify-landing-witness`:
  master's full-history witness vs the branch's windowed one). Compare *what master does
  now*, not *which commit did it*.
- **R11 (new) — a retired lane's branches need a lane-level ruling, not a row verdict.**
  When master has a devdoc that removes the mechanism, every branch touching only that
  mechanism's files is SUPERSEDED by that ruling — name the devdoc in the evidence.

## Recovered work — tasks filed by this batch

| original row(s) | verdict | new task |
|---|---|---|
| `01a0ab60` `feat/conflict-branch-rename` | STILL-NEEDED | **`01a0b4aa-6874-710f-9380-e39910a2d8f4`** — land the conflict-branch naming helper (12 unlanded commits) |
| `01a0ab1e` + `01a0ab7c` archive-why / hand-archive gate | PARTIALLY-DONE ×2 | **`01a0b4aa-69af-739c-a381-92e2a031578f`** — land the archive reason gate (12 unlanded commits across the two tags) |
| `01a0ab47` (pre-existing, pending) | — | covers the 66 new archive tags minted by the park-lane retirement — **no new task filed**, to avoid duplicating it |

Every branch with commits-not-in-master that still exists is also reported by ref, so a
recoverer needs no query:

```
archive/feat/conflict-branch-rename          abb4a062  12 commits
archive/feat/archive-why-gate                eac3b475   4 commits
archive/feat/hand-archive-gate               c7569bcc   8 commits
archive/feat/worker-run-entry2               0aa03689   1 commit   (SUPERSEDED — retired lane)
archive/fix/merger-park-worktree-release     d1a4c708  10 commits  (SUPERSEDED — retired lane)
archive/fix/merge-verify-landing-witness     5db36b11  10 commits  (SUPERSEDED — better fix on master)
archive/fix/merger-status-branch-account     24eded5c   3 commits  (landed as master 69fe2e8c)
archive/feat/merger-tick-branch-account      e0932678   2 commits  (landed as master 69fe2e8c)
archive/fix/cwd-and-lifetime                 b37b776f   7 commits  (SUPERSEDED)
archive/feat/scan-tip-at-mint-045423         4534cd12   5 commits  (same tip as 045325 → landed)
archive/feat/scan-tip-at-mint-045640         4534cd12   5 commits  (same tip as 045325 → landed)
archive/feat/scan-tip-at-mint-050535         4534cd12   5 commits  (same tip as 045325 → landed)
```

## Reproduction

```bash
cd ~/work/GoPi && git worktree add ~/workspace/gopi-ledger-stage1 -b feat/ledger-treasure-hunt-stage1
# the 83-row worklist (read-only; the live DB is locked by the server — copy it first)
cp ~/.gopi/memory.db ~/workspace/_tmp/ledger-stage1/memory.db
sqlite3 -readonly ~/workspace/_tmp/ledger-stage1/memory.db "…the union query above…"
for b in $(…unique branches…); do git rev-parse --verify refs/heads/$b; git tag -l "archive/$b"; done
# per branch: unlanded commits, subjects already on master, added lines already on master
git rev-list --count master..archive/<b>; git log --format=%s master..archive/<b>
```

## Verification

- `git rev-list --count master..<tag>` for all 83 rows' branches; 45 returned **0**.
- Exact-subject matching (`git log --format=%s`) separated stale branches from unlanded work.
- Added-line presence (`git grep -F -h -f`) ordered the ambiguity: ≥76% on master ⇒ landed.
- `go test ./internal/stylegate/ ./internal/standards/ ./internal/heavytest/ -count=1`
  — RC3's liveness test: `FAIL internal/stylegate` (3 sleep + 1 skip vs ceiling 0),
  `ok internal/standards` (25.3s), `ok internal/heavytest` (4.2s).
- Shard coverage: the 83 `task_id`s of the CSV are **set-equal** to the query's 83; the
  17 Stage-0 rows are all present (re-verified, not re-derived).

## Not Done (honest)

- **RC0 was not observed in this lane** (0 rows). It is reported absent rather than
  "tested": no merge-lane row carries a rebuild or version-probe reason. Whether RC0
  exists in the *rebuild* lane is Stage 3/4's question, not this batch's.
- **The three RC4/CAP verdicts rest on content evidence, not on a landed commit**:
  `feat/web-m1-b4b7` / `feat/web-m2-b5b8b9b12` are ALREADY-DONE because master carries
  the capabilities and the canonical tests (with the branch copies parked in master's
  `.ref/`), and 80–89% of the branch added lines are on master. A reviewer who wants a
  per-increment proof should run the tests named in `.ref/web-m1-b4b7.md` — this batch
  read their presence on master, not their pass state.
- **`01a0af55` and the four `scan-tip-at-mint-*` rows** were decided on their tags'
  identity (identical tips `4534cd12`) and master's independent capability; the
  per-suffix branches' *own* deltas (if any) were not diffed individually.
