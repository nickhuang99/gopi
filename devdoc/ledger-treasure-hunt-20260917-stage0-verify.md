<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 0 VERIFICATION ADDENDUM

Date: 2026-09-18 · Branch: `feat/ledger-treasure-hunt-stage0-verify` · Epic: `01a0ab1c-e5c1-75ac-8992-ea71c58c1287`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

This run was dispatched as "[1/7] Stage 0 — build the worklist and the shared harness,
then flush the quick-confirm bulk classes". **Stage 0 was already delivered and already
merged.** This addendum records that finding, verifies the four acceptance criteria
against the merged artifacts, and closes the one criterion that was genuinely
incomplete — criterion (b)'s "residual exceptions named individually", which the
merged document answered only by shape counts.

**No ledger row was read for classification in this run**: every class, count and row
identity below comes from the frozen 2026-09-17 CSV (the authoritative worklist —
defect D7, below, shows why a fresh query cannot be one).

## Completed

1. **Verified the Stage 0 deliverable is merged, not lost.** The branch
   `feat/ledger-treasure-hunt-stage0` no longer exists (deleted after merge, as the
   merger does); its content is on `master` via merge `1602c1a1`.
2. **Checked all four acceptance criteria** — (a) CSV, (c) statistics/reconciliation,
   (d) zero-mutation proof: satisfied by the merged document and re-measured here.
   (b) class partition + rule: satisfied; (b) *residual exceptions named individually*:
   was **incomplete** (shape counts only) and is **closed by this addendum**.
3. **Named every residual exception of the two mechanical classes individually**:
   all 71 `auto-prefixed` rows (30 real-content + 19 backfilled + 11 fixture + 11 probe)
   and all 14 `wrapper:t-*` rows — §A and §B.
4. **Re-measured the live ledger and recorded the drift** (defect D7, second
   observation), so no future batch mistakes today's numbers for the snapshot's.
5. **Explained the one apparent count discrepancy** (epic "wrapper:t-* 17" vs CSV 14)
   as precedence, with the query that proves it — not as a missing row.

## Not Done (honest)

- **No per-row verdict is issued here.** This stage owns the worklist and the bulk
  classes, not the 319 dispositions. The 30 real-content `auto-prefixed` rows are
  *flagged*, not adjudicated — Stages 1–6 own their verds.
- The 3 `wrapper:t-`-tagged rows that `HP-origin` took by precedence
  (`tags like '%from-hostb-Laptop%'` / `%node:hp%` / `%from-hp%` /
  `%worker:hostb-Laptop%`) are counted and explained but not itemised — they are
  Stage 4's rows under partition B.
- `hp/*` refs named by `sync-yielded` rows still do not resolve on this host; those
  verdicts remain Stage 4's, as the merged document already recorded.

## Verification of the merged Stage 0 deliverable

### Criteria (a), (c), (d) — satisfied by the merged artifacts

```
$ git merge-base --is-ancestor 1602c1a1 master && echo YES-ANCESTOR
YES-ANCESTOR
$ git log --format='%H %ci %s' -1 1602c1a1
1602c1a1e6e81e4a277b93803aec22c83827399b 2026-09-17 05:52:13 +0800 Merge branch 'feat/ledger-treasure-hunt-stage0'
$ git ls-tree -r --name-only master -- devdoc/ | grep -i ledger-treasure
devdoc/ledger-treasure-hunt-20260917-rows-master.csv
devdoc/ledger-treasure-hunt-20260917-rows-stage0.csv
devdoc/ledger-treasure-hunt-20260917-rows-stage1-merge-lane.csv
devdoc/ledger-treasure-hunt-20260917-stage0.md
$ wc -l devdoc/ledger-treasure-hunt-20260917-rows-master.csv
320 devdoc/ledger-treasure-hunt-20260917-rows-master.csv      # header + 319 rows ✓ (a)
$ awk -F, 'NR>1{print $4}' devdoc/ledger-treasure-hunt-20260917-rows-master.csv | sort | uniq -c
     93 curated-cancelled
     71 auto-prefixed
     56 curated-failed
     50 HP-origin
     18 sync-yielded
     17 merge-lane
     14 wrapper:t-*
```

The seven classes sum to **319** = 126 failed + 193 cancelled: the CSV is a per-row
projection (not a re-derived count), so it is the worklist of record.

### Criterion (b) — partition + rule satisfied; residual naming CLOSED here

The merged document states partition B with a predicate per class and a stated
precedence order (R5: `HP-origin > wrapper:t-* > sync-yielded > merge-lane > auto >
curated-failed > curated-cancelled`). What it did **not** do is name the `auto-prefixed`
residual exceptions individually — it gave shape counts (25 backfilled / 11 fixtures /
14 short probes / 5 bug-fix / 11 briefs). Re-derived from the CSV, the 71 rows of this
class split **30 real-content / 19 backfilled-chat-turn / 11 short-cli-probe /
11 test-fixture**, and every one of the 71 is named in §A: the split is now a list, not
a histogram.

**Exact overlap identity (corrects the merged document's arithmetic).** The merged
report derived its figures as `106 [auto] rows − 17 wrapper-tagged = 89`, "21 of those
carry an HP tag". Measured from the CSV, the identity is:

```
all [auto]-prefixed rows in the 319 CSV : 106
  ├─ class auto-prefixed (partition B)  :  71   ← this class
  ├─ class HP-origin  (precedence 1)    :  21
  └─ class wrapper:t-* (precedence 2)   :  14
                                    106  ✓
```

106 = 71 + 21 + 14 exactly. The `89 = 106 − 17` step subtracts the **17 wrapper-tagged**
rows, but 3 of those are also HP-tagged and are taken by `HP-origin` first — so the HP
rows *inside* the 89 number **18, not 21** (89 = 71 + 21 − 3 = 89, and 89 − 18 = 71 ✓).
The merged document's 89/21 pair therefore over-states the HP share by 3 rows. The
partition itself (71 / 21 / 14, all disjoint) is unaffected and remains binding —
only the derivation sentence is corrected here.

```
$ awk -F, 'NR>1 && $9 ~ /^"\[auto\]/{print $4}' devdoc/ledger-treasure-hunt-20260917-rows-master.csv \
    | sort | uniq -c
     21 HP-origin
     71 auto-prefixed
     14 wrapper:t-*
```

### Criterion (c) — reconciliation, re-measured today

The merged document reconciles exactly (126/193 raw; 61/191 filtered; gap = the 67
`resolved`-tagged rows). Re-measured 2026-09-18, the *gap quantity* is stable while the
*total* is not:

```
$ sqlite3 -readonly /home/user/.gopi/memory.db "select sum(tags like '%resolved%' and status='failed'),
                                             sum(tags like '%resolved%' and status='cancelled')
                                        from tasks where status in ('failed','cancelled')"
65|2                                        # identical to the 2026-09-17 snapshot ✓
```

The `resolved` overlap is unchanged (65 failed + 2 cancelled = 67). The raw totals have
grown (measured this run: `failed` up, `cancelled` up, total 428) because the daemon
kept running — see D7. **The reconciliation identity holds; only its inputs are live.**

### Criterion (d) — zero ledger mutation

Every statement in both runs was issued as `sqlite3 -readonly /home/user/.gopi/memory.db`. No
`UPDATE`/`DELETE`/`INSERT` was issued, by this run or the run that produced the merged
document; the archive/merge tools were never invoked. All SQL in this addendum is
`SELECT` over `tasks`, plus CSV-side projection. (Read-only access still bumps
`memory.db-shm`/`-wal` timestamps via SQLite's WAL reader, which is not a mutation of
any row.)

### The apparent 17-vs-14 wrapper count — precedence, proven

```
$ sqlite3 -readonly /home/user/.gopi/memory.db "select count(*) from tasks
     where status in ('failed','cancelled') and tags like '%wrapper:t-%'"
17
$ sqlite3 -readonly /home/user/.gopi/memory.db "select count(*) from tasks
     where status in ('failed','cancelled') and tags like '%wrapper:t-%'
       and (tags like '%from-hostb-Laptop%' or tags like '%node:hp%'
            or tags like '%from-hp%' or tags like '%worker:hostb-Laptop%')"
3
```

17 − 3 = **14** ✓. The epic's figure is the tag predicate; partition B's 14 is the
residual after HP-origin takes its 3 first. Documented in the merged report as D6 —
restated here because a child batch that re-derives "17 wrapper rows" will not find them.

### Defect D7 re-observed (the ledger is live — the CSV, not a query, is the worklist)

| quantity | 2026-09-17 snapshot | measured 2026-09-18 (this run) |
|---|---|---|
| `status='failed'` | 126 | **155** |
| `status='cancelled'` | 193 | **273** |
| total | **319** | **428** |
| `resolved`-tagged failed/cancelled | 65 / 2 | 65 / 2 (stable) |
| `LIKE '%wrapper:t-%'` (precedence-blind) | 17 | 17 (stable) |
| `substr(summary,1,6)='[auto]'` | 106 | 141 |

```
$ ./bin/gopi task list --status failed --all | head -1
155 task(s) with status=failed:
$ ./bin/gopi task list --status failed --all | wc -l
157                                          # 155 rows + banner + blank
$ ./bin/gopi task list --status failed | wc -l
92                                           # filtered (unresolved only)
$ ./bin/gopi task list --status cancelled --all | wc -l
275
$ ./bin/gopi task list --status cancelled | wc -l
273
```

So the class totals **drift with the daemon's tick** while the *partitions* hold. Any
batch that computes its own worklist with a fresh query will both miss its rows and
double-count another batch's. **Slice `devdoc/ledger-treasure-hunt-20260917-rows-master.csv`
by its `class` column.** A byte-identical copy lives at
`/home/user/.gopi/sessions/01a0ab1c-e5c1-75ac-8992-ea71c58c1287/rows-master.csv` for use when a
worktree has been recycled mid-task.

## Reusable disposition rules — unchanged

R1 merge proof = resolvable ref + `rev-list --count master..<ref> == 0` ·
R2 an archive tag proves abandonment, never completion ·
R3 a row's status is not evidence about its work ·
R4 rows with no work identity are NO-VALUE by shape, before any git call ·
R5 pin the precedence order before classifying ·
R6 a terminal row a LATER lane repaired is ALREADY-DONE ·
R7 an archived branch whose FEATURE is on master under another layout is SUPERSEDED.

New in this addendum: **R8 — the worklist is a frozen artifact, not a query.** For any
live/append-only ledger, generate the per-row index once, commit it, and slice the
artifact thereafter; re-deriving a count from the live store reports the store's drift
as findings (D7).

## Reproduction

```sql
-- the CSV's class column, restated (partition B). NOT to be used as a fresh worklist —
-- the ledger is live; slice the committed CSV instead (R8).
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

All queries ran as `sqlite3 -readonly /home/user/.gopi/memory.db`, with a bounded retry (the live
ledger returns `database is locked` under the running daemon; a single attempt yields
false empties — retry before believing a zero).


### A1 — `auto-prefixed` real-content rows (30) — named individually

These are the rows criterion (b) was missing: NOT void by shape. Each carries a work identity.
A `failed`/`cancelled` label here says nothing about the work (rule R3) — Stage 1/2/3 own the verdict.

| task_id | status | sub_class | disposition (shape) | summary (truncated) |
|---|---|---|---|---|

| `459a4263-862d-423c-a2aa-ec6ebf13c94c` | failed | failed+resolved | real-content → per-row git check | [auto] # Task: symlink-aware read/write/edit tools for gopi ## 背景与问题 gopi 的文件工具 read/wri… |
| `7d0507eb-cbf5-4714-b513-b74dcc9d7c1a` | failed | failed+resolved | real-content → per-row git check | [auto] current gopi read/write/edit has no knowledge of file is a symlink, this can crea… |
| `b4b6b43c-cb43-46e8-87ea-b30eb95ebf8b` | failed | failed+resolved | real-content → per-row git check | [auto] 抓取 https://bigmodel.cn/pricing 和 https://docs.bigmodel.cn/cn/pricing 的价格表。重点 glm-… |
| `8ef3003b-3db5-4a40-b596-872ed4a5dbcd` | failed | failed+resolved | real-content → per-row git check | [auto] M3 实施（M1 检索地基 cfa8d5c + M2 生命周期 621a60e 已 merge）。范围：①brew 管道：session 结束/定期触发，从 ~/… |
| `f14ae1bd-aa6c-456b-afb4-c2780ef57469` | cancelled | plain | real-content → per-row git check | [auto] M3（feat/memroadmap-m3 分支 cfe590a，worktree ~/workspace/gopi-m3）收尾验收：①devdoc/memroa… |
| `3644f54d-9c9d-48ed-be0d-8bf1954291f1` | failed | failed+resolved | real-content → per-row git check | [auto] 总单：M4.6 chat 表 + M4.7 remote dispatch 连做（单分支 feat/chat-remote-dispatch 防 taskregi… |
| `764751dd-42c8-46ed-b085-c91bd9b44614` | cancelled | plain | real-content → per-row git check | [auto] websearch sqlite bidirectional sync two nodes cr-sqlite litestream rqlite 2025 20… |
| `42cfe7ab-098b-454c-af6c-3d6aa05ff282` | failed | failed+resolved | real-content → per-row git check | [auto] 背景（nick：simple script=sync timestamp-after-records，SyncML 协议核心简化，**nick 确认 willin… |
| `e6e82b59-0106-4a4c-a5d8-3b39fb369893` | failed | plain | real-content → per-row git check | [auto] 背景（nick：harvest 时是否也 push HP 分支回 peerbox？代码 harvest 回 peerbox 后分支该清理；otherwise only D… |
| `6c723b40-e2f3-4ad5-a0dc-6b38c953e162` | cancelled | plain | real-content → per-row git check | [auto] 特性设计+实现：gopi shot 子命令——对 gopi 自身的 web chat UI（cmd/agent/cli_web.go 内嵌 webChatPage… |
| `aab1b66a-4017-49d6-bc86-cc7d185a0305` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick 布置）：HP/peerbox=两个独立开发环境各自积累资产——①HP 本地执行完成的 task 终态 ②HP 本地 LLM 自投的新 bugfix 单… |
| `af1bb7d5-abe4-432b-96f6-ff07f2701311` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick 授权：1214 两例实锤——长会话 1.5h+ 首轮合法中途流式 1214，compact/上下文折叠后的 messages 形态嫌疑最大；**c… |
| `fcaff097-b868-4990-a227-5972a8dcac76` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick 确认 submit：push 自动[bug038 已落地]→干净 merge 自动→冲突自动派 LLM→收编自动——两开发者 merge 闭环的最… |
| `fc652652-647a-4c59-a98c-80fdf84d7652` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick：HP gopi 也应建 worktree 干活，但收尾=check if client→push branch to server→delete … |
| `59d49895-6021-436b-9b92-b213dc6d546e` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick 模型 aligned：领取即所有权——claim 后立即 db-sync 推 pending→in_progress 到对端，对端 TaskNex… |
| `137026e9-44a6-4ea3-afe3-0a2465d0fe8a` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick 补充 M4.18 失败语义：if db-sync failure, it should just update local db with tas… |
| `f8f351f8-ff77-4556-a3b4-c14c1e685b9e` | cancelled | plain | real-content → per-row git check | [auto] 背景（nick：so many new features and new bugfixing, need full code review and full te… |
| `6aa98260-8f28-47b7-9c76-d5ac82251a1a` | cancelled | plain | real-content → per-row git check | [auto] at HP: first PULL all new records to local, then PUSH all new records to peerbox |
| `cbe57cd6-c251-4fe1-8c25-7259e988e121` | failed | failed+resolved | real-content → per-row git check | [auto] [bug056] writeback 覆盖 summary 复发（bug051 虚假关账后症状再现）——审计线索丢失 证据链： 1. bug051（t-17882… |
| `t-1788513902` | failed | failed+resolved | real-content → per-row git check | [auto] Reply with exactly: GOPI-DO-SMOKE-OK |
| `01a0882c-954d-7368-8b42-9ae0caa4c4e9` | cancelled | plain | real-content → per-row git check | [auto] You are the merger's conflict resolver for branch 'feat/job-family-t3' merging in… |
| `01a0898a-88f8-7e4b-bea9-5736e8a2b2c0` | cancelled | plain | real-content → per-row git check | [auto] You are the merger's conflict resolver for branch 'feat/hippocampus-host' merging… |
| `01a089a1-7e3d-7169-81dc-d1851f2e0942` | cancelled | plain | real-content → per-row git check | [auto] You are the merger's conflict resolver for branch 'feat/hippocampus-host' merging… |
| `01a089a7-26cb-744f-8e74-a9180f4e5503` | cancelled | plain | real-content → per-row git check | [auto] You are the merger's conflict resolver for branch 'feat/safe-merge-guard-answer' … |
| `01a08c34-52b7-7a12-83d4-a45957d988a7` | cancelled | plain | real-content → per-row git check | [auto] Capture the live desktop to /tmp/llm-shot.png and tell me the exact pixel dimensi… |
| `01a08e7f-ccaa-7e1c-a2e0-d35ada3c18fd` | cancelled | plain | real-content → per-row git check | [auto] Create the file cost-probe.txt containing exactly the single line 'cost probe', r… |
| `01a08e9b-4573-7026-a6fc-d08e59f0553e` | cancelled | plain | real-content → per-row git check | [auto] Create the file cost-probe.txt containing exactly the single line 'cost probe'. T… |
| `01a09990-9282-7b17-8c82-e45bcb3fa0b4` | failed | plain | real-content → per-row git check | [auto] Do EXACTLY this and nothing else: (1) run the bash command: git worktree add /dev… |
| `01a0a044-166a-75ed-90f6-a546f024d345` | failed | plain | real-content → per-row git check | [auto] You are an independent verification instance for ledger task 01a09d07-73be-775b-a… |
| `01a0a1c0-c422-7757-ae01-80286a9579f5` | failed | plain | real-content → per-row git check | [auto] in the last hour I have to manually merge many stale branches of gopi and maybe s… |

### A2 — `auto-prefixed` NO-VALUE residuals — named individually (41)

Void **by shape** under the uniform rule: no work identity to check against git (rule R4).

**interactive chat turn backfilled into the ledger (`— chat: backfilled`) — 19 rows**

| task_id | status | sub_class | disposition (shape) | summary (truncated) |
|---|---|---|---|---|

| `663d74a7-e813-4495-9cfe-1c137f76cbd6` | cancelled | plain | NO-VALUE (shape) | [auto] 搜索验证任务：台账自动登记 FTS5 全文检索 demo — elapsed=14.1s iter=1 tokens=0 — … |
| `5666322d-4354-4662-bc01-23218e79ead8` | cancelled | plain | NO-VALUE (shape) | [auto] 搜索验证任务：台账自动登记 FTS5 全文检索 demo — elapsed=13.8s iter=1 tokens=0 — … |
| `87663cb5-7ab7-46f5-99c5-24f934ba2201` | cancelled | plain | NO-VALUE (shape) | [auto] let us do a web search : we know ai agent has toolcall like rea… |
| `6874f922-ed2c-4e67-9b07-b782718f0daf` | cancelled | plain | NO-VALUE (shape) | [auto] 你好，请用一句话介绍你自己。 — chat: backfilled |
| `b1fddd7e-5006-4522-a44e-62f02373e198` | failed | failed+resolved | NO-VALUE (shape) | [auto] 图中写了什么字？请原样输出。 — elapsed=0.4s iter=1 tokens=0 — chat: backfille… |
| `85a3c295-d83e-4546-adac-ac15baa39f3a` | cancelled | plain | NO-VALUE (shape) | [auto] 图中写了什么字？请原样输出。 — chat: backfilled |
| `7a48b3c1-20ad-4030-8866-3644d289a9b2` | cancelled | plain | NO-VALUE (shape) | [auto] fetch https://bigmodel.cn/pricing 的完整价格表数据。重点：glm-5.3-flash 和 g… |
| `aa1b288d-f720-42d3-af23-8fb60a50e496` | cancelled | plain | NO-VALUE (shape) | [auto] webfetch https://example.com/ — chat: backfilled |
| `28f3d941-6d6a-4e98-b8bc-4840dffed6fe` | cancelled | plain | NO-VALUE (shape) | [auto] webfetch https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-f… |
| `58256cf1-24fd-4549-a149-e73e71a26544` | cancelled | plain | NO-VALUE (shape) | [auto] t-1788012586 — chat: backfilled |
| `1f944cf2-3f71-4ee8-b2ee-6897f9229613` | cancelled | plain | NO-VALUE (shape) | [auto] now I am thinking to use this for a task dispatcher. can you te… |
| `28cdf1cd-ac2e-4492-83f6-387f1c908fc2` | cancelled | plain | NO-VALUE (shape) | [auto] pa-q-p25-verify-loop — chat: backfilled |
| `871c3ccb-1fe9-4682-9aa5-14b29d605cad` | cancelled | plain | NO-VALUE (shape) | [auto] pa-q-p3-wf-contract — chat: backfilled |
| `702ebd2a-7f04-4e2e-ac19-19dc37aa1259` | cancelled | plain | NO-VALUE (shape) | [auto] You are an independent verification instance for ledger task t-… |
| `5f8ebe4d-e3ee-4ddb-a8b5-b02611f65c08` | cancelled | plain | NO-VALUE (shape) | [auto] You are an independent verification instance for ledger task t-… |
| `14617361-f8b5-4b40-8cd4-399e528230be` | cancelled | plain | NO-VALUE (shape) | [auto] turn 4 — chat: backfilled |
| `5c986760-97bb-43d8-a544-cfad5a52f483` | cancelled | plain | NO-VALUE (shape) | [auto] turn 4 — chat: backfilled |
| `b1476151-e657-4ab4-982f-138efc4a84a1` | cancelled | plain | NO-VALUE (shape) | [auto] turn 4 — chat: backfilled |
| `f92a3523-4e66-46b1-8ca7-a494fd4d1967` | cancelled | plain | NO-VALUE (shape) | [auto] memory search 迭代时 — chat: backfilled |

**≤25-char CLI probe — 11 rows**

| task_id | status | sub_class | disposition (shape) | summary (truncated) |
|---|---|---|---|---|

| `82a5d2ff-e245-4577-a16b-66f292871af1` | cancelled | plain | NO-VALUE (shape) | [auto] config schema |
| `ee8c365b-6e3a-4850-a327-64227e844380` | cancelled | plain | NO-VALUE (shape) | [auto] reply with just: ok |
| `78d40ad1-2150-4b00-9a78-a7bd31933583` | cancelled | plain | NO-VALUE (shape) | [auto] tash run t-1788304050 |
| `01a089f2-c6bf-73e7-a455-320dd7a41eec` | failed | failed+resolved | NO-VALUE (shape) | [auto] -z |
| `01a09573-328f-756d-b895-0292fc7cf60e` | failed | plain | NO-VALUE (shape) | [auto] config dir |
| `01a097c6-3125-7d36-b28f-cb4cf698ced1` | failed | plain | NO-VALUE (shape) | [auto] status |
| `01a0996c-fb44-73d2-85b2-87cb5f1707e9` | failed | plain | NO-VALUE (shape) | [auto] Reply with exactly: OK |
| `01a09a53-b747-76c5-9b2e-427911801592` | failed | plain | NO-VALUE (shape) | [auto] Reply with just: OK |
| `01a09f6d-7753-7321-ba33-8b32b09213e7` | failed | plain | NO-VALUE (shape) | [auto] ELF |
| `01a0a0b7-b774-7909-846a-89f18ad4c13b` | failed | plain | NO-VALUE (shape) | [auto] help env |
| `01a0a1af-adae-7430-a018-80e28f4e3bf7` | cancelled | plain | NO-VALUE (shape) | [auto] worktree list |

**deliberately-failing test fixture — 11 rows**

| task_id | status | sub_class | disposition (shape) | summary (truncated) |
|---|---|---|---|---|

| `6fa09727-3c2b-4c97-97c9-c7e6bed29b7a` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `d268ed28-3493-491a-ad61-2a227f256706` | failed | failed+resolved | NO-VALUE (shape) | [auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 \| FAILED — r… |
| `813e4557-cc36-4b4b-8dec-77a40025caac` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `0a34e9a9-3771-4130-8e99-113c2ddfbc56` | failed | failed+resolved | NO-VALUE (shape) | [auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 \| FAILED — r… |
| `946d7023-c3e3-45df-b127-a02dd51aa0df` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `830509e2-35d4-4c41-a7a8-9c67fbd93961` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `45170459-8f50-4b23-b810-0825104f04a5` | failed | failed+resolved | NO-VALUE (shape) | [auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 \| FAILED — r… |
| `13b13d0a-52a7-4780-a1df-619a044130a4` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `907082b2-0525-4e78-8681-f43804c3d2e5` | failed | failed+resolved | NO-VALUE (shape) | [auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 \| FAILED — r… |
| `0841524a-9b1e-4f45-8aa2-316325d763ba` | failed | failed+resolved | NO-VALUE (shape) | [auto] Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 \|… |
| `f74719f0-6782-4516-9ac0-68cfa6b492f8` | failed | failed+resolved | NO-VALUE (shape) | [auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 \| FAILED — r… |
### B1 — `wrapper:t-*` rows (14) — named individually

A wrapper row is the **run** the child was spawned from (`GOPI_TASK_RUN_ID`); its status records the
wrapper process's fate, not the work's. 3 further `wrapper:t-`-tagged rows are taken by `HP-origin`, so the
epic's 17 = this 14 + those 3 (precedence — defect D6, not a discrepancy).

| task_id | status | sub_class | disposition (shape) | summary (truncated) |
|---|---|---|---|---|

| `63aed300-5665-4edd-b313-9258f2887dbc` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] current gopi task sessionid missing session log path, and it would be con… |
| `4d410346-248e-46ae-ac7f-be26b67897b8` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] EPIC CONTEXT: gopi source is at risk of collapse from accumulated feature… |
| `c7750810-8f11-47f7-89ed-196917efa818` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] DEBUG TASK: t-1788305178 failed with 'signal: broken pipe' after 18s — ro… |
| `1d51eba4-1a2c-4d21-9406-97aef5c51bc1` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] prompt test |
| `997292f2-3fe7-4e05-89e9-08520396b074` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] t |
| `694d28e9-2f3d-4bd0-bb00-172e9c4467fa` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] EPIC CONTEXT: gopi source is at risk of collapse from accumulated feature… |
| `075d366b-1adc-4536-9e59-5e1e77cfe7a4` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] BUG FIX: `gopi worker -h` runs the worker loop (live side-effect on -h) —… |
| `4896b1f0-3fb9-48ec-a8ce-2571bdc24f42` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] BUG FIX: task-sync domain P1 batch — serve-tick full-counter contract, ss… |
| `91ad8a5c-496b-419a-823f-7d2cca46497a` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] BUG FIX: stuck-detection (toolStreak, bug055) false-positives on polling/… |
| `t-1788330680` | cancelled | plain | NO-VALUE as a work item — judge the child row / git | [auto] this seems to be a bug: nick@peerbox:~/work/GoPi$ gopi task sync --peer hp … |
| `t-1788343430` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] t |
| `t-1788343459` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] t |
| `t-1788468254` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] ARCHITECTURE FIX (nick's directive, 2026-09-03 — from the HOMM3 multi-rep… |
| `t-1788551884` | failed | failed+resolved | NO-VALUE as a work item — judge the child row / git | [auto] [Task summary] DO provider doc-review test (verify-write-task-area doc) [… |
## Verification (this run)

- Build: not run — docs-only change, no `.go` file touched.
- Tests: not run — docs-only change.
- Ledger: **0 rows mutated** — every statement `SELECT` under `sqlite3 -readonly`.
- Deliverable proof: `git merge-base --is-ancestor 1602c1a1 master` → exit 0.
- Worktree clean: `git status --porcelain` prints nothing (Rule 30).

## Deliverable declaration

- **Branch**: `feat/ledger-treasure-hunt-stage0-verify` (this run's own worktree
  `/home/user/workspace/gopi-ledger-verify`; worktree shares the main repo's refs, so
  the commit is already on the branch — no push needed).
- **Recovery**: `git -C /home/user/work/GoPi show feat/ledger-treasure-hunt-stage0-verify:devdoc/ledger-treasure-hunt-20260917-stage0-verify.md`
- The verified Stage 0 deliverable itself is already on **master**:
  `git -C /home/user/work/GoPi show master:devdoc/ledger-treasure-hunt-20260917-stage0.md`
