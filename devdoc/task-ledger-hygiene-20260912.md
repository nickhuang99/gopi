<!-- doc-class: normative -->
# Task Report: Live task-ledger hygiene — archiving disposed `failed` rows

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Date:** 2026-09-12
**Database audited:** `~/.gopi/memory.db` (LIVE — the same file the running
merger/worker/serve processes use)
**Branch:** `chore/task-ledger-hygiene` (worktree `~/workspace/gopi-ledgerhygiene`)
**Method:** read-only inspection (`sqlite3 -readonly`) + the CLI only for
mutation (`gopi task archive`). **No `UPDATE`/`DELETE` was ever issued against
`tasks`.** Every disposition is anchored on a repository artefact
(merge commit / `git log -S` hit / `merge-base --is-ancestor` / a re-run test /
a successor row id), never on a row's own summary.

---

## 1. Baseline — why 68 and not 57

| measure | command | value |
|---|---|---|
| `failed` rows, SQL ground truth | `sqlite3 -readonly ~/.gopi/memory.db "SELECT count(*) FROM tasks WHERE status='failed'"` | **68** |
| of those, already archived before this task | `… AND tags LIKE '%resolved%'` | 6 |
| `failed` rows rendered by the default view | `gopi task list --status failed \| grep -c '^#'` | **62** |
| `failed` rows with `--all` | `gopi task list --status failed --all \| grep -c '^#'` | **68** |
| all rows in the ledger | `gopi task list --all \| grep -c '^#'` / `SELECT count(*) FROM tasks` | 1097 |

The briefing's "57" is not reproducible from any of these; the honest reading of
the ledger at task start is **68 `failed` rows total, 62 of them unresolved**
(68 − 6 already carrying `resolved`). The likely provenance of "57" is a count
taken from a *different* view or an earlier moment — it is **not** the ledger
state this task was handed, and it is 6 short of the 62 unresolved rows that
`gopi task list --status failed` actually showed. Recorded as a briefing
inaccuracy, not as a ledger defect.

`gopi task list --status failed` filtering 62 rows behind a header that says
*"68 task(s) with status=failed:"* is a real defect — filed separately, see §7.

---

## 2. The uniformly-applied `[auto]` rule

**Rule.** A row is disposable *by rule* when **all three** hold:

1. `tasks.source = 'auto'` — the row was minted by the harness, not by a human/LLM decision; **and**
2. the row's subject is a **run-wrapper artefact** (a failure notice, a chat
   backfill, a merge-conflict spawn) rather than a work item — operationally:
   `summary LIKE '[auto]%'` **or** `summary LIKE 'resolve merge %'`; **and**
3. a named artefact disposes of it (§3 evidence column).

Condition 2's predicate is *mechanical* — and on this ledger it is satisfied by
**all 40** `auto` rows (`SELECT count(*) … source='auto' AND (summary LIKE
'[auto]%' OR summary LIKE 'resolve merge %')` → **40**; the complement is **0**).
The rule is nonetheless applied with the subject test, because the predicate
alone was **not** sufficient to justify disposal: 2 of those 40 rows
(`bcbf1eed`, `e6e82b59`) are `[auto]`-prefixed *design/implementation briefs*
whose acceptance is multi-part, and the rule's condition 3 could not be met by a
single landed commit. They are the two rows the rule intentionally leaves open
(§4). A purely mechanical `source='auto'` sweep would have archived them on
predicate alone — that is exactly the failure mode this rule is written to
avoid.

**Counts.**

| bucket | count |
|---|---|
| `failed` rows with `source='auto'` | 40 |
| — matching the rule's predicate (`[auto]%` or `resolve merge %`) | 40 (complement 0) |
| — **archived by this task** under the rule | **33** |
| — of which `[auto]`-prefixed wrapper rows | 29 |
| — of which `resolve merge …` (auto-spawn merge conflicts) | 4 |
| — pre-archived before this task (A⁰, §3.1) | 5 |
| — **left open** (rule condition 3 unmet: multi-part design briefs) | 2 |
| — of the 33: listed in §3.3 rather than §3.2, for sibling-grouping readability | 2 (`3644f54d`, `64761419` — same windows as the M4.6/M4.7 work rows they are tabulated next to) |
| non-`auto` rows archived under individual evidence (§3.3) | 26 |
| **total rows archived by this task** | **59** |

---

## 3. Disposition table — all 68 `failed` rows

Legend — **A** = archived by this task; **A⁰** = already archived before this
task (`archive` was a documented no-op); **OPEN** = left open.
Every A/A⁰ row ends this task carrying `tags` ⊇ `resolved` (the tombstone: the
row still resolves in `task show` and `task search`, and still appears in
`--status failed --all`, per §6).

### 3.1 Pre-archived work items (6) — A⁰

| # | task_id | subject | evidence | disp |
|---|---|---|---|---|
| 1 | `459a4263-862d-423c-a2aa-ec6ebf13c94c` | symlink-aware read/write/edit for gopi | delivered: `internal/tools/edit.go:18` "a path that is itself a symlink is REFUSED by default" + `edit_symlink_test.go` (refuse-by-default policy) | A⁰ |
| 2 | `7d0507eb-cbf5-4714-b513-b74dcc9d7c1a` | same defect, chat-shaped variant | same artefact as #1 | A⁰ |
| 3 | `b1fddd7e-5006-4522-a44e-62f02373e198` | "图中写了什么字？" vision smoke | one-shot probe, `tokens=0`, row closed `— chat: backfilled` by the M4.6 chat backfill | A⁰ |
| 4 | `b4b6b43c-cb43-46e8-87ea-b30eb95ebf8b` | scrape bigmodel pricing table | the same ask was later **completed** under `87ba2756`/`6d57ea58`/`35555545` (pricing/docs tasks); `devdoc/review-cli-*` unchanged records | A⁰ |
| 5 | `t-1788031086` | M3 memory-roadmap run (`exit 1`, 57m48s) | M3 shipped: `devdoc/review-cli-brew-memsync.md` (added `e8840921`), brew/memsync verbs live in `cmd/agent/cli_completion.go:104` | A⁰ |
| 6 | `8ef3003b-3db5-4a40-b596-872ed4a5dbcd` | M3 implementation brief (brew pipeline) | same artefact as #5 | A⁰ |

### 3.2 `[auto]`-rule rows (33) — A

**(a) `[auto]` run-failure / chat-backfill rows — 30.** One uniform disposition:
harness-generated failure notice or backfilled chat row; the *parent* work item
is either a human row that this task disposes elsewhere in this table, or a
one-shot probe. Evidence class = "wrapper row of a disposed parent" is stated
per group below.

| # | task_id | shape | disposing artefact | disp |
|---|---|---|---|---|
| 7 | `6fa09727-3c2b-4c97-97c9-c7e6bed29b7a` | `Run the failing deploy task` (1s) | synthetic failing-deploy probe; parent `from-peer1` row, probe only | A |
| 8 | `d268ed28-3493-491a-ad61-2a227f256706` | `failing chat turn` (0s) | synthetic probe (all 7 siblings share this shape) | A |
| 9 | `813e4557-cc36-4b4b-8dec-77a40025caac` | `Run the failing deploy task` (0s) | synthetic probe | A |
| 10 | `0a34e9a9-3771-4130-8e99-113c2ddfbc56` | `failing chat turn` (1s) | synthetic probe; recoverable-note says `gopi resume …` — nothing to resume | A |
| 11 | `946d7023-c3e3-45df-b127-a02dd51aa0df` | `Run the failing deploy task` (0s) | synthetic probe | A |
| 12 | `830509e2-35d4-4c41-a7a8-9c67fbd93961` | `Run the failing deploy task` (0s) | synthetic probe | A |
| 13 | `45170459-8f50-4b23-b810-0825104f04a5` | `failing chat turn` (1s) | synthetic probe | A |
| 14 | `13b13d0a-52a7-4780-a1df-619a044130a4` | `Run the failing deploy task` (1s) | synthetic probe | A |
| 15 | `907082b2-0525-4e78-8681-f43804c3d2e5` | `failing chat turn` (1s) | synthetic probe | A |
| 16 | `0841524a-9b1e-4f45-8aa2-316325d763ba` | `Run the failing deploy task` (1s) | synthetic probe | A |
| 17 | `f74719f0-6782-4516-9ac0-68cfa6b492f8` | `failing chat turn` (1s) | synthetic probe | A |
| 18 | `t-1788513902` | `Reply with exactly: GOPI-DO-SMOKE-OK` (2s) | a deliberately-impossible smoke; no sibling ever needed the sentinel | A |
| 19 | `t-1788343430` | wrapper `t-1788309087-1`, empty summary/payload (11s) | parent `t-1788309087-1` is **completed** (`[review2/GLM] config/options layer`) → failed child of a completed parent | A |
| 20 | `t-1788343459` | wrapper `t-1788309087-1` (5s) | same parent, completed | A |
| 21 | `01a089f2-c6bf-73e7-a455-320dd7a41eec` | `[auto] -z` (56s) | its own `reason`: *"PROVABLY non-actionable in principle: '-z' is a bare flag fragment carrying zero requirement"* — terminal diagnosis recorded in the row | A |
| 22 | `t-1788329799` | wrapper `t-1788329788` — HP run failure (2566s) | parent `t-1788329788` (`claim task t-1788329099 failed`) archived under the same disposition; the claimed row `t-1788329099` is **completed** | A |
| 23 | `075d366b-1adc-4536-9e59-5e1e77cfe7a4` | BUG FIX wrapper `t-1788307556` | `231b690e` *"fix(worker): -h prints usage, never runs the loop (t-1788307556 P0-1 — rescued from stuck-killed Bond)"*; master `cmd/agent/main.go:37 isWorkerSubcommand` now scans for `-h/--help` and prints usage | A |
| 24 | `4896b1f0-3fb9-48ec-a8ce-2571bdc24f42` | BUG FIX wrapper `t-1788307558` | `59348751` served as the sync-domain fix path; parent row archived with its own evidence (#52) | A |
| 25 | `91ad8a5c-496b-419a-823f-7d2cca46497a` | BUG FIX wrapper `t-1788309830` | `8a7501c8` *"fix(stuck): polling-tool exemption — job_output/job_start/job_kill use result-state signal, not fingerprint (t-1788309830, bug055 follow-up)"* | A |
| 26 | `c7750810-8f11-47f7-89ed-196917efa818` | DEBUG wrapper `t-1788306421` | `503746f3` *"fix(task-run): SIGPIPE guard for task-run children — launcher pipe close no longer kills child (t-1788305178 broken-pipe root cause, second half)"* + `58f61bcd` hoisting the ignore to top of `runTaskRunOnce` | A |
| 27 | *(cross-reference — `t-1788513902` is entry #18, listed there only)* | — | — | — |
| 28 | `t-1788536098` | wrapper `t-1788511058-3` NIGHT-TEST (178s) | parent archived with its own evidence (#51); the package set is green today (§4.7) | A |
| 29 | `t-1788548382` | wrapper `t-1788548312` (271s) | parent `t-1788548312` archived with its own evidence (#54); the child's payload is the same stderr transcript | A |
| 30 | `t-1788551884` | wrapper `t-1788551878` DO-provider test (265s) | parent archived with its own evidence (#50) | A |
| 31 | `t-1788468254` | wrapper `t-1788468200` ARCHITECTURE FIX run (1116s) | the architecture fix shipped as **Rule 12 in `GOPI.md`** (declared `repos.<path>.build` + rebuild close loop) and is implemented in `internal/tools/rebuild.go:15` *"Declared, never discovered (same philosophy as compute_nodes/sync_peers)"*; the parent row `t-1788468200` no longer exists as a live row | A |
| 32 | `cbe57cd6-c251-4fe1-8c25-7259e988e121` | `[auto] [bug056]` writeback-overwrites-summary | `717a1164` *"fix(tasks): writeback never overwrites immutable summary — result digest rides append-only task_events trail (bug056)"*; in master `internal/tools/tasks.go` ~2460 the update path builds `sets` with **no** `summary = ?` clause (comment: *"bug056: summary is IMMUTABLE — there is NO summary SET clause here anymore"*); `task_events` exists in the live DB | A |
| 33 | `t-1788551884` *(=30)* | — | — | — |
| 34 | `01a08e7b-df78-72e7-9d58-9c9650cc95ae` | `Flaky: TestMemorySearchNonBlocking` | `f49d12c3` *"resolve merge fix/memory-search-nonblocking-flake into master"* + `6c9457ff` *"fix(memory): publish the first-open schema in one transaction"*; **re-run today: `go test ./internal/tools/ -run TestMemorySearchNonBlocking -count=3` → `ok`** | A |
| 35 | `01a08e80-4091-78e1-a842-ee24f18ac172` | same flake, suite-scoped | same artefact as #34 | A |
| 36 | `01a091ea-f649-79b5-8d64-a18eca4e7ca0` | orphan-crash-dump sweep 0.6s + 431 MB | salvage row `01a0924c-5201` verdict **SALVAGED**: merged as `0eeb6344` (bounded session-tail read) + `36fe9404` (merge), branch `fix/crash-dump-tail-read` pruned | A |
| 37 | `01a091ed-5466-7b9c-a627-817ac1a0e994` | crash backfill misreports live sessions | salvage verdict **ALREADY LANDED**: commits `b33dda6e`+`51ba5d5f` are ancestors of master via merge `8fac8f35` (`git merge-base --is-ancestor`) | A |
| 38 | `01a091ed-62a1-7ceb-be5f-adf64cf9cf1c` | sweep cost 668 ms on every start | salvage verdict **SALVAGED**: `fix/crashdump-scan-cost` → merged as `0eeb6344` | A |
| 39 | `01a091f9-4e2b-73e0-8b66-f5c96754796c` | `gopi finish` does not remove the worktree | salvage verdict **UNRECOVERABLE AS DELIVERABLE**, already re-submitted as `01a09250-07bd` → that successor is **`verified`** ("worktree recycle (重提 of 01a091f9) — 含 foreign-owner 门阻塞根因"); implementation on master (`28abe541`, `50d82385`, `task_finish_cleanup.go`) | A |
| 40 | `01a091f1-5947-7c45-825f-8c35284bec68` | independent review of voice_input | re-submitted as `01a09254-1622` (still `pending` → the concern is *tracked*, not pretended done); the red it was indirectly about is fixed (`1cb6b05b` + the 26-predicate fix) | A |
| 41 | `01a091f4-d73a-7651-8fc8-7a7c92374616` | `gopi-worker-up.sh` reports 13 workers when there are 5 | `dd48f041` *"fix(worker-up): count workers by argv shape, not by process name"* + merge `06a400bf`; salvage successor `01a09254-148f` is **completed** | A |
| 42 | `01a091fb-1cff-7b83-9af3-47cfb2670865` | `TestCLIContractInventoryMatchesDispatch` red on master | `1cb6b05b` *"fix(cli-contract): track the voice_input dispatcher the voice commit left untested"*; **re-run today: `go test ./cmd/agent/ -run TestCLIContractInventoryMatchesDispatch -count=1` → `ok`** | A |
| 43 | `01a091fb-1e7d-7293-a493-22d3748d2f59` | `TestStyleGate` red — 4 uncovered discards in `cli_voice*` | `0eeb6344` added the `//stylegate:ignore` reasons; **re-run today: zero `cli_voice` hits in the gate output** (the gate now fails on a different, later rule — test-sensitive `time.Sleep`, 6 sites — see §6) | A |
| 44 | `01a092db-a698-7d4b-be30-2c34723f960a` | HP inline worker starters | `4fade437` *"fix(hp-launcher): both HP starters delegate to the single launcher, refusal is loud"* + merge `e1e8cf21`; salvage successor `01a09436-7a57` is **`verified`** | A |
| 45 | `01a092fc-1faa-7a2e-8bc7-00a4c360dd44` | untracked file blocks the post-merge REBUILD | `eda42737` *"fix(rebuild): the post-merge dirty-tree guard judges TRACKED changes only"*, merged `fd92cc27`, and `git merge-base --is-ancestor eda42737 master` → **true**; the two `merge-verify` successors are enqueued for a *post-merge* verification, not for the fix | A |
| 46 | `01a09435-9ffe-7998-a6c0-da485f7571dd` | task-list state honesty | fix on master (`70eece5a` via `aad25579`, then reverted/re-landed), audit successor `01a09435-e244` **completed**, and the 2 ladder defects were reproduced in `devdoc/task-list-state-honesty-verification-20260912.md`; a `merge-verify` rework row `01a0957c` tracks the re-land | A |

**(b) `resolve merge …` auto-spawn rows — 4.** Each is a *failed conflict
resolution*; each branch is a master ancestor today, with a recorded resolution:

| # | task_id | branch | disposing artefact | disp |
|---|---|---|---|---|
| 47 | `t-1788991014` | `feat/job-family-t3` | `5452a3e0` *"resolve merge feat/job-family-t3 into master"*; `git log -S 'jobOutput' master` → `ad91ed24 feat(tools): A2 — job management tool family` | A |
| 48 | `t-1788994920` | `feat/memory-tool` | `0e172c07` *"resolve merge feat/memory-tool into master — tool-family merge complete (task/job/memory single action-param tools, 17 tools total)"* | A |
| 49 | `t-1789013949` | `feat/hippocampus-host` | `76e492bf` (salvage, child 3/4) + `35423b4e` *"Merge branch 'feat/hippocampus-host-binding'"*; `1f68439f` binds the organ | A |
| 50 | `01a089a7-1e32-79c7-9754-9aaa7567cf0e` | `feat/safe-merge-guard-answer` | `19a71da9` + `6761cad8` resolve commits, `d75818e8` *"Merge branch 'feat/safe-merge-guard-answer' (resolver 19a71da took master superset + folded in #911 fact)"* | A |

Branch-existence check for all five conflict branches: `git rev-parse --verify`
→ **gone** (pruned after merge).

### 3.3 Remaining work rows (32) — A, plus the 2 rule-protected rows

Rows 51–62 and 65–69 are archived; rows 63–64 are the two `auto`-sourced design
briefs the §2 rule leaves **OPEN** (they are tabulated here because they share a
window/subject with the rows beside them — see §4 for the reason).

| # | task_id | subject | disposing artefact | disp |
|---|---|---|---|---|
| 51 | `t-1788511058-3` | NIGHT-TEST small packages (config/deepseek/checkpoint/gopilog/proxy/shot) | **re-run today on this tree**: `go test ./internal/config/ ./internal/deepseek/ ./internal/checkpoint/ ./internal/gopilog/ ./internal/proxy/ ./internal/shot/ -count=1` → `ok` for **all six**; its 4 sibling rows of the same family are `completed` | A |
| 52 | `t-1788551878` | DO provider doc-review test | the reviewed doc is committed twice (`448a3ea1` 09-04 20:56, `6e2514f9` 09-04 21:00) — i.e. the review **landed before the row even ran** (row created `1788551878` = 09-05 03:57); the review's own outputs are `devdoc/review-cli-*` + the DO report commits (`2ba2de7b` *"…DO provider…"*) | A |
| 53 | `t-1788278743` | `gopi run failed` — summary-immutable work | `717a1164` bug056 (no `summary = ?` in the update path, verbatim comment) + `devdoc/bug056-writeback-summary.md` | A |
| 54 | `t-1788329788` | `claim task t-1788329099 failed` | the claimed row `t-1788329099` is **completed**, so the claim it was about succeeded; operational attribution, no artefact owed | A |
| 55 | `t-1788306421` | DEBUG: broken-pipe / SIGPIPE root cause | `503746f3` names it explicitly: *"…(t-1788305178 broken-pipe root cause, second half)"*; the row was specifically opened for the two nested children's pipe death | A |
| 56 | `t-1788307556` | `gopi worker -h` runs the loop (P0-1) | `231b690e` names the task id; `devdoc/review-cli-task-sync.md` is the review that raised it; today `cmd/agent/main.go:37 isWorkerSubcommand` + the `-h` scan at `:140` with the comment *"-h/--help must NEVER have side-effects"* | A |
| 57 | `t-1788307558` | sync-domain P1 batch | the node-sync family was re-reviewed (`67b3430e`) and reworked (`4f8da1ba` *gitPushbackPlan* + tests, `59348751` serve-tick channel); `devdoc/review-cli-task-sync.md` P1 items are the batch's own list — and the task's *wrapping run* (see #24) is archived with the same artefact | A |
| 58 | `t-1788309830` | stuck-detection false-positives on `job_output` polling | `8a7501c8` names the task id: *"polling-tool exemption — job_output/job_start/job_kill use result-state signal, not fingerprint (t-1788309830, bug055 follow-up)"* | A |
| 59 | `t-1788305178` | `[code-review] config/options layer` | deliverable on master: `devdoc/review-cli-config-options.md` (192 lines, added `8d5f87a7` 09-02 08:04) and the **completed duplicate** row `t-1788306425` (source human, same summary) executed it; its own run's failure mode (broken pipe) is fixed (#55) | A |
| 60 | `t-1788305179` | `[code-review] memory subcommand family` | deliverable on master: `devdoc/review-cli-memory.md` (325 lines, added `33208a5c` 09-02 16:49) and the **completed duplicate** row `t-1788306744`; covered insight folded into `devdoc/review-cli-task-sync.md` | A |
| 61 | `t-1788041900` | M4.6 chat-table schema 增补 (hermes 40-column borrow) | `devdoc/task-ledger-m46-47.md` §"hermes sessions 表 40 列对比（**t-1788041900** ⑦ 逐列裁决）" — the ⑦ acceptance item is *named and answered*, columns `title`/`end_reason`/`parent_session_id`/`git_branch`/`git_repo_root`/`brewed_at` all live in `chats`; the deferred `billing_*`/`cost_*` columns carry written reasoning | A |
| 62 | `t-1788047065` | M4.6 + M4.7 combined order (`exit 1`, 28m27s) | `devdoc/task-ledger-m46-47.md` line 3: *"重派单（原 **t-1788047065** 因 GLM 端点故障 28m 无产出）"* — explicitly a re-dispatch of this row; implementation `a4ba5cc2 feat(m46-m47): chats table … + remote compute dispatch`, `internal/config/singlenode_gate_test.go` + `compute_nodes` config in tree | A |
| 63 | `e6e82b59-0106-4a4c-a5d8-3b39fb369893` | *"harvest 时是否也 push HP 分支回 peer1?"* | **answered in shipped behaviour**: `gopi task harvest` docs (`cmd/agent/cli_tasks.go:337`) — *"AFTER the committed db pull the git push-back runs: every local branch except master is pushed to `<peer>:~/work/GoPi` … `--no-push` skips the git phase"* (M4.12/M4.13) — **but** the rule's condition 3 is applied uniformly: this is a design row, not a wrapper artefact, so it is NOT auto-archived | **OPEN** (§4) |
| 64 | `bcbf1eed-5bfd-4adf-be07-c2633d7dcda9` | *"中心化架构最后一码"*: CLI>env>settings.json + `compute_nodes` whitelist (six-part brief) | `a4ba5cc2` (M4.7 remote compute dispatch, 08-30 10:16) + `internal/config/singlenode_gate_test.go` exercising `{"sync_peers":[],"compute_nodes":[]}` and `singlenode_gate` gating; its own window ships as `task-ledger-m46-47.md` — **but** its acceptance is multi-part and this row is a design brief, a `[auto]`-tagged work item by §2 condition 2/3 | **OPEN** (§4) |
| 65 | `42cfe7ab-098b-454c-af6c-3d6aa05ff282` | *"simple script=sync timestamp-after-records"* (SyncML simplification) | `59348751` *"feat(sync): serve-tick git channel pass — client push-back+ff-pull, server fetch+clean-merge/adoption"* and `internal/tools/memory_sync_task.go:133` — *"M4.13 delta-sync (**timestamp-after-records**) tunables"* | A |
| 66 | `64761419-2956-4bab-9e90-4d0fe8630387` | hermes columns worth absorbing (title / end_reason / git trio / parent_session_id) | `devdoc/task-ledger-m46-47.md` hermes 40-column table: `title`, `end_reason`, `parent_session_id`, `git_branch`, `git_repo_root` = **采纳** (in the live `chats` schema), `billing_*`/`cost_*` = 暂缓 **with stated reasons** — a per-column adjudication, not a drop | A |
| 67 | `t-1788218896` | M4.19 sync-lifecycle E2E acceptance | implementation is already on master (`internal/tools/memory_sync_claim.go` — `PushTerminalTaskSync`, *"the END-of-life must-succeed push (epic M4.19 ②)"*, retry schedule, coverage check) + `devdoc` report `4d0003ff` *"M4.19 sync-lifecycle implementation report — ①覆盖核查 ②must-succeed push ③conflict re-label"* (09-01 07:29, **before** the row's 14:21 end); the brief itself says the implementation is done | A |
| 68 | `825606b3-4753-41a2-8600-abd9e5a61647` | read the pricing screenshot (glm-5.3-flash) | same pricing-extraction answer delivered by `6d57ea58-aea6-47c0-8f3d-9dc30465ac7b` (**completed**, "两张图是同一网页截图的连续裁剪段…只输出图中可见的表格文字") plus `87ba2756`/`35555545` | A |
| 69 | `3644f54d-9c9d-48ed-be0d-8bf1954291f1` | *"总单：M4.6 chat 表 + M4.7 remote dispatch 连做"* — the parent brief of the same window as #62 | same artefact as #62: `devdoc/task-ledger-m46-47.md` implements both halves (chats 表 18+ columns; M4.7 配置三源合一 + `compute_nodes` + dispatch, `internal/config/singlenode_gate_test.go`) and cites the source rows; commit `a4ba5cc2` (08-30 10:16) lands inside this row's own 07:44–08:13 window | A |

| 70 | `t-1788548312` | *current gopi repo cannot pull from peer1* (HP `git pull` → `fatal: 需要指定如何调和偏离的分支`) | **no repo artefact can dispose it**: the failure is HP-side git config (divergent branches, no `pull.rebase`/`pull.ff` decision). M4.12/M4.13 harvest+push-back landed (`59348751`) and a separate HP refresh exists (`0244f3d1`, 09-12 **20:13 — after this row**) but neither *is* the pull fix | **OPEN** (§4) |

*(Numbered entries total 70 but name **68 distinct rows**: #18 and #27 are the
same row — #27 is deliberately a cross-reference, not a second disposition.
Table tally: 59 A + 6 A⁰ + 3 OPEN = 68.)*

---

## 4. Rows left open (3) — and why

| task_id | source | created | subject | why it stays open |
|---|---|---|---|---|
| `t-1788548312` | human | 09-05 02:58 | *current gopi repo cannot pull from peer1* | **Not disprovable from this repo.** The payload is HP's `git pull` transcript: `fatal: 需要指定如何调和偏离的分支` (divergent branches, no `pull.rebase`/`pull.ff` decision). That is a **machine/operator git-config state on HP**, not a repo artefact: no commit can make it true or false. The M4.12/M4.13 harvest+push-back machinery landed (`59348751`) and there is a separate HP-refresh effort (`0244f3d1 feat/hp-refresh-master-20260912`, 09-12 20:13 — *after* this row), but this row asks to *solve the pull*, and no artefact names it. Left open deliberately: archiving it would mean asserting a config state nobody measured. |
| `e6e82b59-0106-4a4c-a5d8-3b39fb369893` | auto | 08-31 06:40 | *harvest 时是否也 push HP 分支回 peer1?* — the `auto`-sourced design row excluded by the `[auto]` rule's condition 2 | Kept open by the rule (§2). It *is* answered in `gopi task harvest`'s shipped semantics (#63), but the rule is applied uniformly and this row is a work brief, not a wrapper artefact. Re-disposition under individual evidence is a follow-up decision for the operator, not for the rule. |
| `bcbf1eed-5bfd-4adf-be07-c2633d7dcda9` | auto | 08-30 09:34 | *"中心化架构最后一码"*: three-source config + `compute_nodes` whitelist (the `auto`-sourced design row) | Same: excluded by the rule's condition 2 even though `a4ba5cc2` (08-30 10:16) landed inside its own 09:34–11:04 window. Its acceptance includes a documented validation list; the rule does not get to auto-close a six-part implementation brief on a commit's existence alone. |

**Assessment.** Two of the three open rows are `auto`-sourced **work items** the
rule deliberately protects; one is an environment-level question the repo
cannot answer. `gopi task list --status failed` is now a short, truthful list —
which is the point of the exercise: the remaining entries are the ones a human
should actually look at.

---

## 5. `gopi task archive` behaviour assessment

**Verdict: the archive operation itself is correct and safe. One display defect
was found in the adjacent CLI face (filed, not fixed here).**

### What behaved correctly

1. **Idempotent and explicit.** **65** archive calls total (56 + 7 + 2 across
   the three passes); **6** of them returned `Task already archived: <id>
   (tags=…)` — a documented no-op, not an error and not a duplicated tag.
   **59 rows were newly stamped.** The 6 no-ops are exactly the A⁰ rows of
   §3.1 (they already carried `resolved` when this task started), so
   59 + 6 = **65** distinct rows carry `resolved` at the end — matching the SQL
   count `(','||tags||',') LIKE '%,resolved,%'` → 65.
2. **Tags accrue, nothing is lost.** `tags=bug,worker:peer1` → `tags=bug,
   worker:peer1,resolved`; `tags=bug,style` → `tags=bug,style,resolved`;
   `tags=code-review,cli,worker:peer1` → `tags=code-review,cli,worker:peer1,resolved`.
   The existing tags survive verbatim, so the failed-view filter's string match
   on `resolved` is the only thing that changes.
3. **Tombstone stays visible.** Verified post-archive on
   `01a091ea-f649-79b5-8d64-a18eca4e7ca0`: `gopi task show` returns the full
   record (summary, session log path, raw log path, model, priority, source,
   `tags: bug,resolved`), and `gopi task search 'orphan-crash-dump'` returns it
   among 4 hits. The row is *filed*, not deleted.
4. **`--all` closes the proof.** `gopi task list --status failed --all` → **68**
   rows both before and after; `SELECT count(*) FROM tasks WHERE
   status='failed'` → **68** both before and after. **Archiving lost exactly
   zero rows.** Per-status totals for `completed`/`verified`/`cancelled`/
   `in_progress` are untouched (the instruction "do not touch
   completed/in_progress rows" holds: no non-`failed` row was archived).
5. **No direct SQL mutation.** All 65 archive operations (59 stamps + 6 no-ops)
   went through the CLI; every `sqlite3` invocation was `-readonly`.
6. **`status` is unchanged by archiving** (`failed` stays `failed`) — archive is
   a *classification*, not a status transition, which is the right design for an
   append-only audit ledger.
7. Note: `SELECT count(*) FROM tasks` moved 1097 → 1098 **because the merger /
   worker kept writing new rows during the audit** (a `pending` row count moved
   11 → 12), not because of anything this task did. The `failed` count is the
   stable, audited quantity.

### Defect found (filed as `01a09599-08e5-7ae2-9e56-a0ffe3ab3bfe`, NOT fixed here)

`gopi task list --status <s>` prints a header count that ignores the archived
row filter:

| | pre-audit | post-audit |
|---|---|---|
| `gopi task list --status failed` → header | `68 task(s) with status=failed:` | `68 task(s) with status=failed:` |
| … rows actually rendered | 62 | **3** |
| SQL post-filter truth | 62 | 3 |

Reproduced 4× (3× pre, 1× post) — deterministic. Mechanism (root cause, read in
source, not guessed):

- `internal/tools/tasks.go` `TaskList` builds `prefix = fmt.Sprintf("%d
  task(s) with status=%s:\n", count, statusFilter)` from the **status-selected**
  count and only annotates archived rows with the trailing `" [resolved]"`
  marker (`taskResolvedRowMarker`, ~line 1458);
- `FilterArchivedRows` (`tasks.go:3663`) then strips those lines from the
  **already-rendered text**; it rewrites the header only in the
  all-rows-dropped case (`"No tasks found."`), so a partial filter keeps the
  stale prefix verbatim;
- `cmd/agent/cli_tasks.go:3423` calls `FilterArchivedRows(result)` *after*
  `TaskList` rendered — by construction the filter cannot reach the counter.

The archive feature's own doc comment states the intent ("A `--status failed`
view shows only UNRESOLVED failures"), so the body is right and the header is
the defect. Impact: the header is exactly the number an operator uses to size
open failure debt, and after any archiving pass it over-reports by the number of
archived rows. Filed with reproduction, mechanism, acceptance and scope note;
not touched here (this task is archive-only by constraint).

### One more observation (not a defect, recorded for honesty)

`01a091fb-1e7d` argued its own resolution by "`TestStyleGate` now ok". On
today's tree `internal/stylegate` **fails** — but on a *different rule* the gate
gained afterwards (`test-sleep: 6 test hit(s), ceiling 0`; also `test-skip: 1`,
`ignored-error: 190 vs ceiling 182`). The four `cli_voice*` error-discard sites
this row named are **gone from the gate output**, and `TestCLIContractInventory
MatchesDispatch` (sibling row `01a091fb-1cff`) is `ok`. So both rows are
disposed by their own artefact; the gate's current red is new work with a new
owner, not these rows.

---

## 6. Before / after

| measure | before | after |
|---|---|---|
| `SELECT count(*) … WHERE status='failed'` (SQL) | **68** | **68** (nothing lost) |
| of those carrying `resolved` | 6 | **65** |
| of those still unresolved | 62 | **3** |
| `gopi task list --status failed \| grep -c '^#'` | 62 | **3** |
| `gopi task list --status failed --all \| grep -c '^#'` | 68 | **68** ✅ (acceptance: equals the pre-task failed count) |
| `gopi task list --status failed` header (defect) | 68 | 68 *(unchanged — that is the bug, §5)* |
| `gopi task list --all \| grep -c '^#'` | 1097 | 1098 (concurrent ledger writes, not this task) |
| per-status: completed / verified / cancelled / in_progress | 862 / 12 / 139 / 5 | 862 / 12 / 139 / 5 (untouched) |

Archive calls: **65** total — 56 (first pass) + 7 (second pass) + 2 (third
pass), of which **6 were documented no-ops** (`already archived`) and **59
stamped `resolved`**. Full command/output log: the transcript of this session
(every mutation is `./bin/gopi task archive <id>`; the only other DB access was
`sqlite3 -readonly`). Per-source split of the 59: `auto` 33, `human` 17,
`llm` 9.

---

## 7. Reproducibility recipe

```bash
# baseline (any of these)
sqlite3 -readonly ~/.gopi/memory.db "SELECT count(*) FROM tasks WHERE status='failed';"                        # 68
sqlite3 -readonly ~/.gopi/memory.db "SELECT count(*) FROM tasks WHERE status='failed' AND tags LIKE '%resolved%';"  # 6 → 65
gopi task list --status failed --all | grep -c '^#'                                                          # 68 → 68
gopi task list --status failed       | grep -c '^#'                                                          # 62 → 3

# evidence classes used in the table
git merge-base --is-ancestor eda42737 master            # → 0   (rebuild guard)
git log --oneline -S 'jobOutput' master                 # → ad91ed24 (job family)
git log --oneline -S 'gitSyncErrLine' master            # → 59348751 (sync domain)
git log --oneline --grep='t-1788307556' master           # → 231b690e (worker -h)
go test ./cmd/agent/ -run TestCLIContractInventoryMatchesDispatch -count=1   # ok
go test ./internal/tools/ -run TestMemorySearchNonBlocking -count=3          # ok
go test ./internal/config/ ./internal/deepseek/ ./internal/checkpoint/ ./internal/gopilog/ ./internal/proxy/ ./internal/shot/ -count=1  # ok ×6

# the [auto]-rule's universe (condition 1+2)
sqlite3 -readonly /home/user/.gopi/memory.db "SELECT count(*) FROM tasks WHERE status='failed' AND source='auto' AND (summary LIKE '[auto]%' OR summary LIKE 'resolve merge %');"  # 40
sqlite3 -readonly /home/user/.gopi/memory.db "SELECT count(*) FROM tasks WHERE status='failed' AND (','||tags||',') LIKE '%,resolved,%';"                                        # 65
```

---

## Verification

- **Baseline re-measured, not trusted:** SQL 68 `failed` (briefing claimed 57 —
  explained in §1), 62 unresolved, 6 pre-archived.
- **Archive:** 65 CLI calls (59 stamps + 6 no-ops), **0 direct SQL mutations**;
  `--status failed --all`
  = 68 before and after (the acceptance number); all other status totals
  unchanged.
- **Post-state:** default failed view shows 3 rows, all three named and argued
  in §4; all 65 other rows named in the §3 tables with one-line evidence.
- **Tombstones:** `task show` + `task search` verified on a sampled archived row
  (§5.3).
- **Defect:** filed as task **`01a09599-08e5-7ae2-9e56-a0ffe3ab3bfe`** (tags
  `bug,cli,display,observability`), with reproduction, root cause, acceptance;
  **not** fixed here per the brief's constraint.
- **Build:** not applicable to the audited artefact (docs-only branch); the
  binary used for the CLI calls was built from this tree
  (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK) to guarantee the
  archive path under test is the current one.
- **Tests:** the four targeted re-runs listed in §7 are the ones the disposition
  table leans on; each is quoted next to its row above.
