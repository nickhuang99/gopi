<!-- doc-class: historical -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# gopi 自身能力盘点 B —— 跨会话记忆 / 经验蒸馏 / 任务账本 / 多 agent 调度

<!-- meta -->
repo: hosta/gopi（本机工作树 `/home/user/work/gopi` 的 clone ground）
ground: `/home/user/workspace/01a1aa6c-faa1-7a35-b04c-678d9824a3b5`（`.git` 是**目录** = CLONE，非 linked worktree）
**测量锚点**：全部实测在 revision `7c72007fc4e61d2050e39e36a76f098a6d740019` 上取得 ——
`7c72007fc` 是**已发布**的 revision（`git merge-base --is-ancestor 7c72007fc origin/master` 成立），
不是本 run 自己的未发布 tip，所以一次 rebase 不会把这个锚点抹掉（Rule 38）。
二进制由本 run 自建：`CGO_ENABLED=0 go build -o <session>/artifacts/bin/gopi ./cmd/agent/`，
`--version` 的 `revision:` 与该 rev 逐字相等。`v0.4.0` / `go1.24.0` / `linux/amd64`。
**本文件自身所在提交的 HEAD 会随交付腿 rebase 而变**，所以这里不引用 HEAD —— 只引用测量锚点。
框架: `devdoc/competitor-analysis-framework.md`（阶段 0，任务 `01a1aa6c-703d-7059-8e6a-4a6b5dced12c`）
证据通道: **E1（help）+ E2（源码）**；本片**大量使用 E3**（我方自己的二进制可真跑）
证据原件: `~/.gopi/sessions/01a1aa6c-faa1-7a35-b04c-678d9824a3b5/artifacts/stage2b/`
（`help-only-probes.txt`、`verb-truth-v3/v4.txt`、`verb-classification.txt`、
`exit-codes-corrected.txt`、`helppage-tiers.txt`、`tier-roundtrip.txt`、`head-recheck.txt`、
`repro-db-and-version.txt`、`merger-verify.txt`、`fill-ellipsis-A/B/C/D.txt`、`final-selfcheck.txt`）
以及 `artifacts/help/*.txt`（20 份 help 原文）。

**判定口径（沿用阶段 0）**：help 是唯一权威入口；源码只用来验证 help 背后有没有实现。
help 写了但源码没有 → `help-only`；源码有但没有入口 → `dead-code`。两者都进 §5「help-only / dead-code」。

**本片只覆盖四个子系统**（记忆 / 蒸馏 / 任务账本 / 多 agent 调度），其余 13 维在阶段 2A 与我方其余切片里。
因此下文**不打 17 维总分**，只给 D16、D17 两维的行（框架 §8 指定 `gopi memory -h` / `gopi memory search -h`
与 `gopi task -h` / `gopi worker -h` 为这两维的探针）。

---

## 0. help 落盘（本片所有 E1 引用的来源）

20 份 help 原文，逐条命令跑出来原样落盘：

```
$ ls ~/.gopi/sessions/01a1aa6c-faa1-7a35-b04c-678d9824a3b5/artifacts/help/
chat.txt            memory.txt        memory_backup.txt  memory_brew.txt
memory_memsync.txt  memory_sync.txt   notify.txt         session.txt
task.txt            task_archive.txt  task_clone.txt     task_status.txt
task_submit.txt     task_sweep-worktrees.txt  task_sync.txt  task_update.txt
tool.txt            top.txt           ui_state.txt       worker.txt
```

`md5sum` 显示 20 份里只有 **15 份**内容互不相同 —— 这个 15/20 不是笔误，是 §5 的一个 bug 证据
（`task submit -h` / `task update -h` 等打印的是整个 family 页，不是自己的页）。

---

## 1. 子系统一：跨会话持久记忆（D16）

### 1.1 表面：15 个子命令，不是 4 个

`gopi memory -h` 广告的子命令（`$ ./bin/gopi memory -h | grep -oE 'gopi memory [a-z-]+' | sed 's/gopi memory //' | sort -u`）：

```
backfill-tokens  backup  brew  capture-stats  converge-keywords  delete
feedback  get  list  memsync  provenance  search  store  sync  update
```

**15 个**。briefing 里点名的 4 个（`backup`/`sync`/`memsync`/`brew`）只是其中「搬运 + 生产」那一头，
治理与检索那一头（`provenance`/`feedback`/`update`/`converge-keywords`/`backfill-tokens`/`capture-stats`）
比它更大。

### 1.2 写入 / 读回 / 治理 —— E3 全跑通

用一个**私有探针库**（不是操作员的 `~/.gopi/memory.db`，避免污染 append-only 账本）：

```
$ export DB=<session>/artifacts/probe.db
$ gopi memory store --topic "E3 probe 记忆往返" --question "阶段2B D16 探针：写一条能检索回来吗" \
    --answer "实测往返：store 后 search 命中同一条 id" \
    --keywords "probe,roundtrip,记忆往返,D16" --source probe --db "$DB"
Memory stored (id=1).
exit=0

$ gopi memory search "记忆往返" --limit 3 --db "$DB"
#1 E3 probe 记忆往返 tier=warm conf=0.85 lesson=lesson when=2026-11-05 16:31:45 sources=probe tokens=2
Q: 阶段2B D16 探针：写一条能检索回来吗
A: 实测往返：store 后 search 命中同一条 id
exit=0

$ gopi memory list --limit 2 --db "$DB"
1 most recent record(s):
#1 [warm] E3 probe 记忆往返
    Q: 阶段2B D16 探针：写一条能检索回来吗
(details: memory_get <id>)
exit=0
```

**D16 的 full 证据串（框架 §3 定义"写一条后检索命中同一条，两次命令都存档"）已经满足**：
`store` 返回 `id=1`，`search`/`get` 命中的正是 `#1`。

治理三腿也真跑（四腿各自独立）：

```
$ gopi memory store --topic t2 --question q2 --answer a2 --keywords k --source probe \
    --session-id 01a1aa6c-faa1-7a35-b04c-678d9824a3b5 --round-index 3 --db "$DB"
Memory stored (id=2).
exit=0

$ gopi memory update 1 --tier cold --db "$DB"
Memory updated (id=1).
exit=0

$ gopi memory feedback 1 --useful --reason probe \
    --task 01a1aa6c-faa1-7a35-b04c-678d9824a3b5 --db "$DB"
Memory feedback recorded (id=1, rating=useful reason="probe"):
  useful=1.00 useless=0.00 fb_score=+0.042 confidence_step=+0.05
exit=0

$ gopi memory feedback --list --db "$DB"
#1 E3 probe 记忆往返 useful=1.00 useless=0.00 fb=0.67(1)
exit=0

$ gopi memory feedback --recompute --db "$DB"
Memory feedback recomputed from 1 records' event history.
exit=0
```

**tier 的真实通道是 `update --tier`，不是 `search --tier`**（本档早期版本写错了 flag，
复测推翻：`gopi memory search probe --tier cold` → `exit=1`，`gopi memory search: unknown flag --tier`）。
用本 run 自己写的那条记录做降级→复原往返。

**⚠️ 下面这段四条命令都漏了 `--db`，因此读写的是操作员的默认账本 `~/.gopi/memory.db`
（不是 §1.2 那个私有探针库）。这是本片的一次违规，代价与撤回命令见 §10 第 2 条**
（`#28021` 那条 fixture 记录因此永久留在真账本里）：

```
$ ./bin/gopi memory search probe-d16-14900 --limit 1
#28021 阶段2B自证 D16 tier=warm conf=0.85 lesson=lesson when=2026-11-05 16:45:32 eff_conf=0.86 tokens=1
Q: 怎样自证跨会话记忆可写可读？

$ ./bin/gopi memory update 28021 --tier cold
Memory updated (id=28021).                exit=0

$ ./bin/gopi memory search probe-d16-14900 --limit 1
#22403 response tier=warm conf=1.00 lesson=pitfall when=2026-11-01 08:19:11
  sources=response(auto) fb=0.80(3) origin=sess=51f501aa-d6a7-482d-aa61-68aa10a33a36 round=7 host=hosta
Q: ## 结果：命令没有执行到 git —— 被 bash 工具的共享 checkout 守卫拦下
   ← #28021 从首位消失（tier=cold 不再是一级候选）

$ ./bin/gopi memory update 28021 --tier warm
Memory updated (id=28021).                exit=0

$ ./bin/gopi memory search probe-d16-14900 --limit 1
#28021 阶段2B自证 D16 tier=warm conf=0.85 lesson=lesson when=2026-11-05 16:45:32 eff_conf=0.88 tokens=1
Q: 怎样自证跨会话记忆可写可读？           ← 回到首位
```

**这是本片最硬的一条 D16 证据**：降级 → 检索排序真的变了 → 复原 → 排序真的回来了，
三步都是同一条查询、同一条记录。`tier` 不是展示字段，它**参与检索**。
（注意 `eff_conf` 两次读数是 `0.86` 与 `0.88`，即使 tier 相同也不同 —— 说明 `eff_conf`
还受时间衰减等因子影响。这是观察，我没有读衰减公式，不作断言。）

`memory -h` 把这两层分开写得很清楚：

```
gopi memory update <id> [--tier hot|warm|cold] [--topic T] [--question Q]
      Edit one record. At least one field is required. --tier cold demotes a
      low-quality row so it is no longer 1st tier to return (it only fills
      record per task). Votes only steer ranking — tier changes stay manual.
```

即：**投票（feedback）只影响排序，tier 变动是人的动作** —— 这是两条独立通道，不是一条。

### 1.3 账本同源 / 可导出（D16 定义的后半句）

D16 的定义是"与任务账本**同源或可导出**"。两条都成立：

```
$ gopi memory provenance --counts --db "$DB"
total rows: 2
  (empty)          2          ← 探针行没有 provenance 归类，如实报空而不是报错

$ gopi memory memsync --export --out <session>/artifacts/patch.json --db "$DB"
exported 2 rows (2 scanned, 0 skipped by --since) → <session>/artifacts/patch.json (node hosta)
exit=0
$ ls -la <session>/artifacts/patch.json
-rw-r--r-- 1 nick nick 818 Nov  6 00:35 <session>/artifacts/patch.json
```

`--since` 在 `memsync` 里**是真实现**（`0 skipped by --since` 是它自己的读数），
与 §5.1 里 `brew --since` 的**假实现**形成对照 —— 同一个 flag 名，一个真一个假。

### 1.4 检索器的 schema（为什么它比"向量库"更有信息）

一条命中行本身就是结构：

```
#18403 clone-channel 报「carries no commit to deliver」（rev-list --count HEAD --not origin/master = 0）
  时如何判「已落地」还是「从没提交」 tier=warm conf=1.00 lesson=lesson when=2026-10-28 11:15:05 \
  sources=row 01a17e04-c358-7460-a5e6-00e972f7085e (round 2/3),ground /home/user/workspace/01a17e04-c358-7460-a5e6-00e972f7085e; internal/devworkspace/delivery_anchor.go \
  tokens=1 fb=0.97(33) origin=sess=8dacfe6c-9436-40b1-bea0-73e438648af6 round=13 host=hosta
```

（原文是一行；上面按 120 列折行，末尾 `\` 是我的折行标记，不是输出内容。取回原样的命令：
`./bin/gopi memory search 01a17e04-c358-7460-a5e6-00e972f7085e --limit 1`）

`sources=` 直接写出这条经验**来自哪一条任务行、第几轮、哪个 ground、哪个 host、哪个 session 的第几轮**。
这是 D16 与 D13（可审计痕迹）在 gopi 身上**焊死在一起**的地方 —— 别家的"记忆"通常只是一堆向量，
gopi 的记忆行带着它的**出生证明**。

### 1.5 蒸馏（子系统二）：brew 真跑，但有 flag 谎报

`gopi memory brew` 的 group 页（`cmd/agent/cli_memory.go:151`）：

```
  gopi memory brew [--since DURATION] [--dry-run] [--verbose]
```

实测两次干跑，`--limit 1`：

```
# 在测量锚点 7c72007fc 上复测（自建二进制，§0）
$ ./artifacts/bin/gopi memory brew --since 24h --limit 1 --dry-run
  --since 24h  exit=0 | brew done in 40.7s: sessions=1 chunks=6 accepted=14 rejected=0 stored=0 deduped=0 errors=0

$ ./artifacts/bin/gopi memory brew --nonsense-flag --limit 1 --dry-run
  --nonsense   exit=0 | brew done in 42.0s: sessions=1 chunks=6 accepted=14 rejected=1 stored=0 deduped=0 errors=0
```

**一个随便拼的 flag 被接受、exit=0、并且真花四十多秒跑了一次完整的 LLM 蒸馏。**
两次读数唯一的差别（`rejected 0` vs `1`）来自模型采样，不是来自 flag —— 这正是问题所在：
**`--since 24h` 根本没有生效**，它和不传、和拼错，走的是同一条路。
实现里只解析 5 个 flag：

```
$ grep -n 'fs.has("\|fs.get("' cmd/agent/cli_brew.go
81:		DryRun:      fs.has("dry-run"),
82:		SessionsDir: fs.get("dir"),
84:	if p := fs.get("session"); p != "" {
87:	if v := fs.get("limit"); v != "" {
109:	if fs.has("json") {
$ grep -c 'fs.expect(\|fs.boolean(\|fs.repeat(' cmd/agent/cli_brew.go
0
```

`RunBrewOptions` 里也没有 `Since` / `Verbose` 字段（`internal/tools/memory_brew_run.go`：
`SessionPaths` / `SessionsDir` / `Limit` / `DryRun` / `Progress`）。
→ **`--since` 与 `--verbose` 是 help-only**，而解析器没开严格模式，于是它们与任何拼写错误一起被静默吞掉。
已提 bug 行 `01a1ac90-530d-7af0-b4d1-792daf415b85`。

**蒸馏的产物是什么**（这是"经验蒸馏"这一维的实质）：brew 把 session 的 jsonl 切成 chunk，
让模型吐 **lesson / procedural workflow** 两种行；失败的那次输出还把失败原因原样打出来了：

```
  ! 01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0.jsonl: distill workflow "用正则扫描帮助文本做维度证据初筛":
    brew: no JSON object in reply: {"name":"扫描帮助文本→正则初筛→统计命中判维度","trigger":"需要对工具/CLI 帮助文本做维度证据初筛
```

（那行 JSON 是被截断的模型原话，它本身没有闭合 —— 取回办法：
`./bin/gopi memory brew --verbose --limit 1 --dry-run 2>&1 | grep 'no JSON object'`，
输出长度随采样变化，不能保证复现同一句。）

即：**解析失败会报出模型原话**，而不是静默丢弃（Rule 15 的反面例子在这里是正面的）。
副作用：`errors=1` 与 `errors=0` 之间唯一差别是模型采样 ——
在同一个 `--limit 1` 下它有时成功有时失败：

```
brew done in 31.5s: sessions=1 chunks=7 accepted=10 rejected=4 stored=0 deduped=0 errors=1
brew done in 42.0s: sessions=1 chunks=6 accepted=14 rejected=1 stored=0 deduped=0 errors=0
```

说明这条腿是**概率性**的，不是确定性的。

---

## 2. 子系统三：任务账本（D17）

### 2.1 表面：真源 38 个动词，页 synopsis 只广告 11 个

**先给三个各自成立、但过去被混用的数**（口径不同，混用会得出错结论）：

| 量 | 命令 | 读数 |
|---|---|---|
| 常量（单一真源 `internal/taskverbs`） | `grep -cE '^\s+[A-Z][A-Za-z]+\s+= "' internal/taskverbs/verbs.go` | **38** |
| `cli_tasks.go` 里 `case taskverbs.X` 的去重符号 | `grep -o 'case taskverbs\.[A-Za-z]*' cmd/agent/cli_tasks.go \| sed 's/case taskverbs\.//' \| sort -u \| wc -l` | **31** |
| 页 synopsis 行（`^  gopi task X`，去掉裸页） | `gopi task -h \| grep -E '^  gopi task ' \| sed 's/ *<.*//;s/ *\[.*//;s/"$//' \| grep -v '^  gopi task$'` | **11** |

synopsis 广告的 11 个：`archive clone dedup-scan improvements list search show status
sweep-worktrees submit update`。

**判"有没有实现"不能靠 grep case，要逐个无参真跑**（先落盘再读退出码 —— 走管道时 `124`
会变成 grep 的 `0/1`，GOPI.md §4.10；我第一次就是踩了这个，38 个全读成 `exit=0`）。38 个实跑后是**三类**：

| 类 | 数 | 判据（输出首行） | 动词 |
|---|---|---|---|
| **ACTIVE** | **24** | 报缺参数，或真跑出统计行 | `adopt archive audit-merges clone dedup-scan diff harvest improvements list merge merge-execute merged-cleanup pull push receive search send show status submit sweep-worktrees sync sync-pass update` |
| **REMOVED-MAP** | **12** | 首行形如 `gopi task <verb>: removed — <理由> <替代命令>` | `branch-refs finish-cleanup for-branch new next orphans reconcile retry-delivery run stranded sweep-zombies verify` |
| **RESIDENT-RETIRED** | **2** | 首行形如 `<verb>: retired — the resident merger daemon <理由>` | `merger serve` |

24 + 12 + 2 = 38，闭合。差集（`comm`，不靠眼睛）：

```
页广告 ∩ ACTIVE                   = 11 个，全部是 ACTIVE → 页上没有一条"广告了但不存在"的动词
ACTIVE 但页不广告                 = 13 个：adopt audit-merges diff harvest merge merge-execute
                                    merged-cleanup pull push receive send submit sync sync-pass
退役(REMOVED+RESIDENT)但页不广告   = 14 个
```

**所以"页广告了但没有 dispatch"的动词是 0 个 —— 本档早期版本写的 `merger` 是错的**：
`merger`/`serve` 不在 synopsis 里，页末另有一段标着
`RETIRED (2026-09-19, devdoc/merger-retired.md)` 的散文，明写它们 refuse 且
`exit 1`（`cmd/agent/cli_tasks.go:498-499`）。真实缺陷只有一类：**13 个活着的动词在页上完全不可发现**
（`merge`/`push`/`pull`/`send`/`receive`/`sync` 这类跨节点与合并原子，读者只能靠猜或读源码）。

### 2.2 写入 / 读回（D17 的 full 证据串）

```
$ ./bin/gopi task submit "probe: 阶段2B 证据行（fixture，不派发）" \
    --tags fixture --payload "evidence probe for gopi-self-b"
exit=0
task_submit: repo "/home/user/workspace/01a1aa6c-faa1-7a35-b04c-678d9824a3b5" is the clone channel's
  workspace for one run (under the workspace root "/home/user/workspace") — stamping the source
  repository /home/user/work/gopi it was cloned from, so the row outlives this run
Task queued: 01a1ac8c-744b-77e0-91d4-b17913be009a (status=pending, source=agent, tags=fixture)

$ ./bin/gopi task show 01a1ac8c-744b-77e0-91d4-b17913be009a
task_id:        01a1ac8c-744b-77e0-91d4-b17913be009a
status:         pending
session:        (no session id)  run directory holds no run log and no session id is recorded for this row
summary:        probe: 阶段2B 证据行（fixture，不派发）
run_dir:        /home/user/.gopi/sessions/01a1ac8c-744b-77e0-91d4-b17913be009a
runs:           none (no run log in the directory)
exit=0
```

两条命令都 `exit=0`，`task_id` 往返一致 → **D17 的 full 证据串满足**。

用 `--tags fixture` 是刻意的：它让这一行**变成队列装饰而不是工作**，任何 dispatcher 都不会认领它 ——
实测的 `[queue-skip]` 原因原文就是这个：

```
#01a1ac8c-744b-77e0-91d4-b17913be009a  [pending]  probe: 阶段2B 证据行（fixture，不派发）
  (model (unknown), source agent, created 2026-11-06 00:30:48)
  [queue-skip] fixture row — the reserved fixture tag marks queue decoration, never work:
  no dispatcher will claim it (drop the tag to make the row real work)
```

**这本身是一个设计观察**：`fixture` 是一个保留 tag，它的语义是"这条行不是工作"。
写证据行不必污染队列 —— 这个能力我在 8 家里没见过任何一个有。

### 2.3 parent gate（Rule 33）—— 谓词在代码里，标记在输出里

```
$ grep -n 'func taskParentGateDenied' internal/tools/tasks.go
8855:func taskParentGateDenied(rowTags string, parentIDs []string, parentStatus map[string]string) (bool, string)
$ grep -n 'func taskParentGateDeniedOne' internal/tools/tasks.go
8940:func taskParentGateDeniedOne(rowTags, parentID string, parentStatus map[string]string) (bool, string)
$ grep -n 'func taskRowClaimable' internal/tools/tasks.go
9124:func taskRowClaimable(rowTags, payload string, parentIDs []string, parentStatus map[string]string, skipSet map[string]bool) bool
```

实测它真的在拦（这是本机当前 6 条链式行中的两条）：

```
$ ./bin/gopi task list --status pending | grep queue-skip | cut -c1-160
#01a1ac8c-744b-77e0-91d4-b17913be009a  [pending]  probe: 阶段2B 证据行（fixture，不派发）
  (model (unknown), source agent, created 2026-11-06 00:30:48) [queue-skip] fixture row — the re
#01a1ac9e-d0ab-7822-a2bf-8cf82117d8dd  [pending]  bug: `gopi task dedup-scan` / `gopi task merge`
  无参时开跑而非报缺参数  (model deepseek-flash, source agent, created 2026-11-06 00:50:51)
  [queue-skip] parent 01a1ac97-513c-77a8-9bee-659e9914dcfe not released
```

（原文是整行；下面是同形状的一条真实输出，`cut -c1-160` 是为了在文档里放下 ——
完整理由是 `[queue-skip] fixture row — the reserved fixture tag marks queue decoration,
never work: no dispatcher will claim it (drop the tag to make the row real work)`，
父行那条是 `[queue-skip] parent <id> not released (status=pending; releases on
completed|superseded|verified)`。取回原样不加 `cut`：`./bin/gopi task list --status pending | grep queue-skip`）

`releases on completed|superseded|verified` —— 释放集是**三个**状态，不是"做完就行"。
一条 `cancelled` 或 `failed` 的父行会**永久** park 它的子行，这是设计的代价，页面写了。

`[next]` 标记同时存在（`grep -c '\[next\]'` → `1`）：park 一条行**不会**把队头堵死 ——
这正好是 Rule 33 要求的那条不变量（"a park is never a head-block"）。

### 2.4 复活阶梯：两级，且第一级不新建行

`gopi task clone` 的实测拒绝（用 pending 行去试）：

```
$ ./bin/gopi task clone 01a1ac8c-744b-77e0-91d4-b17913be009a --dry-run --reason probe
gopi task clone: refused — task 01a1ac8c-744b-77e0-91d4-b17913be009a is pending:
  it has not run yet, so its ground is not suspect — it is UNUSED.
  The remedy for an unrun row is to RUN it (`gopi worker 01a1ac8c-744b-77e0-91d4-b17913be009a`),
  not to copy it.
Next: a row whose run is over and whose WORK is still wanted is copied into a clean room with
  `gopi task clone 01a1ac8c-744b-77e0-91d4-b17913be009a`; if the room is fine and only its
  recorded repo is stale,
  repair the row in place with `gopi task update 01a1ac8c-744b-77e0-91d4-b17913be009a --repo PATH`.
exit=0
```

注意**退出码 0 但拒绝**（正文说 refused）。这是一个不一致：`exit=0` 的拒绝让调用方无法用退出码判断。
记入 §6 的观察，未单独提 bug（因为 `--dry-run` 下"没有写任何东西"是事实，0 说得通；
但 `--dry-run` + 不可克隆，两个语义叠在一起时退出码就不再是一个判据）。

「第一级：`gopi worker --resume <task-id>` 同 id 同 session 同目录」写在 `worker.txt` 页里，
不是 `task` 页（`task` 页没有 `resume` 动词）—— 这是两页之间的分工：

```
$ grep 'resume' <session>/artifacts/help/worker.txt
  gopi worker --resume <task-id>       finish THAT row's own ground (a CLONE when
                              dev_repo is declared) and PUBLISH it in the close-out
  gopi worker --resume --branch B    the same, when you hold the BRANCH
  gopi worker --resume --worktree P  the same, with P naming the ground
```

### 2.5 三个已退役动词的**出口**是最值得抄的一条设计

`verify` / `reconcile` / `sweep-zombies` 都不在了，而**读者的第一反应不是"坏了"，是"该去哪"**：

```
$ ./bin/gopi task verify
gopi task verify: removed — the separate verification pass was retired — verification belongs to a
TEST PROGRAM, not to a second gopi spawn ("一个程序不能自己检验自己", operator ruling 2026-10-25):
the run that measured it records the verdict with "gopi task update <id> --status verified
--result \"evidence: <the test command> + its output tail\"" (a "reason: <why>" pass-through is the
other accepted spelling); a red measurement is "gopi task update <id> --status failed --result
\"evidence: <the failing command> + its output\"" and a reissue is "gopi task submit <the same work>"
(or "gopi task clone <id> --reason <why>")

$ ./bin/gopi task reconcile
gopi task reconcile: removed — the whole-ledger pass was retired — each of its legs has a command
that names it: "gopi task list" (the stale read-out), "gopi task sweep-worktrees --apply" (the
ownership-independent worktree reap), and "gopi task list --status failed" (the failed-needs-reissue
census, with [queue-skip] naming a parked chain's parent); a run's own close-out rides its end
(the release line), so there is no terminal-row pass left to invoke
```

**退役不是删除，是改写成一张地图。** 每个退役动词都点名它的替代命令，且给出**判据**
（"verification belongs to a TEST PROGRAM, not to a second gopi spawn"）而不只是指令。
这是本片里我认为最该被别家抄的一条工程实践。

---

## 3. 子系统四：多 agent 调度

### 3.1 worker：一条命令行里承诺 16 种形态，且都真的存在

精确计数：`gopi worker -h | grep -cE '^  gopi worker'` → **16**（= 15 个可执行面 + 1 个 `-h`，
后者自称 "claims nothing"）。逐条摘自落盘原文：

```
  gopi worker                 run tasks until the queue drains, exit 0 by itself
  gopi worker --once          same drain; exits as soon as the queue is empty
  gopi worker [<task-id>]     run THAT row and nothing else, then exit
  gopi worker --resume <task-id>       finish THAT row's own ground (a CLONE when
                                       dev_repo is declared) and PUBLISH it in the close-out
  gopi worker --resume --branch B      the same, when you hold the BRANCH
  gopi worker --resume --worktree P    the same, with P naming the ground
  gopi worker --daemon        same loop, detached (returns at once; logs to <gopi-dir>/logs/worker.log)
  gopi worker --status        what is running, and WHICH TASK each one runs
  gopi worker --status --json  the same read as one document (see Options)
  gopi worker list            the same read, spelled as a question (identical output to --status)
  gopi worker --stop          finish the in-flight round, then exit; the worker is ASKED, not killed
  gopi worker --stop --branch B   stop the worker that holds branch B
  gopi worker --stop --pid N  stop that one worker, as --status listed it
  gopi worker --restart       --stop, then --start (FOREGROUND workers are reported, not signalled)
  gopi worker --start         start a detached worker (refused if one runs)
  gopi worker -h|--help       this help (claims nothing)
```

**16 行里有 3 行是同一个读面**（`--status` / `--status --json` / `list` —— 页面自己写
"identical output to --status"），所以**独立能力是 13 个**，形态行数是 16。本档早期版本写
"11 种"，漏了 `--resume --branch`、`--resume --worktree` 与 `-h` 这三行。

E3：`--status` 与 `list` 的输出**逐字相同**（同一份 6 行，pid 从 550060 到 2733202），页面说
"identical output" 是真的。`--status --json` 给了结构化形状：

```json
{
  "program": "gopi worker",
  "profile": "/home/user/.gopi",
  "state_dir": "/home/user/.gopi/worker",
  "workers": [ { "pid": 550060, "shape": "foreground", "state": "unknown",
                 "task": "01a1ab39-b90e-7568-807d-8e10ed556cdd", "task_source": "session-dir",
                 "task_recorded": true, "command": "gopi worker", "image": "replaced",
                 "image_revision": "26b0a32d293ac919b6c3b4cb828f4e35d1df6e70",
                 "image_disk_revision": "eafec12d6bfdd70df50d17644a3aec55d6a8ee06" } ]
}
```

`image: "replaced"` + 两个 revision 字段是**这一层的真本事**：它知道"我这个进程跑的是哪一份二进制，
而磁盘上现在是另一份"。别家的 worker 列表通常只报 pid 和任务名。

### 3.2 notify：进程外可达运行中行（唯一通道）

`notify.txt` 的 synopsis：

```
  gopi notify events [--sink PATH] [--event NAME]... [--task ID]... [--since T]
                     [--limit N] [--json] [--cursor PATH] [--follow]
  gopi notify failed [--sink PATH] [--since T] [--cursor PATH] [--json]
  gopi notify sink
  gopi notify [--peer <ALIAS>] --session <SESSION-ID> --message <TEXT>
  gopi notify --notify-remote            (the PEER side of --peer: stdin in, receipt out)
```

页面自述的持久性机制（原样摘录）：

```
  DELIVERY IS DURABLE. The message is appended to
  <0-session-dir>/messages/inbox.jsonl, so it survives the session not running
  at all, and the session reads it when it does. The session takes it at its
  next round boundary, through the SAME single take point a background-job
  completion notice uses — there is no second queue and no second injection.
  A message is delivered to the model ONCE (a 'delivered' marker is appended
  beside it).
```

两条互补的 flag 强制（实测）：

```
$ ./bin/gopi notify --session 01a1aa6c-faa1-7a35-b04c-678d9824a3b5
gopi notify: --message is required: --session names a session but says nothing to send it
(the deliver form is `gopi notify --session <SESSION-ID> --message <TEXT>`)
```

即 `--session` 单独存在会被**点名拒绝**，不是默默什么都不做。跨主机那条腿：

```
  --peer SENDS IT TO ANOTHER HOST.  gopi notify --peer hp --session <ID> --message <TEXT>
  resolves nothing locally: it ships the request over the ssh leg the sync family already trusts
  (BatchMode ssh to a DECLARED peer — settings.json "memory_peers" / GOPI_MEMORY_PEERS) to
  gopi notify --notify-remote on that host, which resolves the session in ITS OWN profile
  and appends the message there.

  THE HOP IS IDEMPOTENT ON THE MESSAGE ITSELF: the id is derived from
  (session, text, source), so re-running the same command appends NOTHING and says so
```

**idempotent hop** 是这一层最关键的可靠性设计：ssh 腿在"对端已写入、回执未回"时死掉，重跑**不会重复投递**。
本机 `GOPI.md` §4.12 有一整节记录这条通道怎样被用来给运行中的 worker 改变方向 —— 那是它的用法，
不是它的实现；实现只在这里的 inbox.jsonl + delivered 标记。

### 3.3 跨节点：`sync` 家族是**活着但被关掉**的一层

```
$ ./bin/gopi task sync -h | head      （原样：family 页 184 行）
GoPi task subcommands — the task-as-memory ledger (跨会话追溯/审计).
```

本机实测的配置事实（`gopi config show`，见 `GOPI.md` §4.7）是 `task_sync = false`，
即"本地认领，无远端腿"。这条腿的代码在（`cmd/agent/cli_tasks_sync.go` 的
`runTaskSync` / `runTaskDiff` / `runTaskSend` / `runTaskReceive` / `runTaskPull`，
以及 `cmd/agent/cli_tasks_sync_pass.go:108 func runTaskSyncPass(args []string)`），
但**本机没有在跑**。写清现状比写"支持"更准确：

| 腿 | 代码 | 本机在跑吗 |
|---|---|---|
| 本地认领（Phase-1 CAS） | `internal/tools/memory_sync_claim.go` | **是** |
| 跨节点认领（Phase-2/3） | 同上 | **否**（`task_sync=false`） |
| 行移动 `send` / `receive` | `cmd/agent/cli_tasks_sync.go` | 有命令、未派发 |
| 知识搬运 `memory sync`（ssh 活体） | `cmd/agent/cli_memsync.go` | 命令在，未实测跨机 |
| 知识搬运 `memory memsync`（patch 文件） | 同上 | **是**（§1.3 实测 export 成功） |

`memsync` 是**不需要对端**的那一半，所以它能在单机跑通；`sync` 是**需要对端**的那一半。
把它们混为一谈（"gopi 支持跨节点记忆同步"）会高估现状 —— 页面自己已经区分得很清楚：

```
  gopi memory memsync — node→node sync (git-PR semantics). Two channels:
      brew rows (knowledge base) and procedural rows (workflows).
  The patch-FILE channel: move knowledge rows node→node as a reviewable
      JSON patch (the LIVE ssh sibling is 'gopi memory sync').
```

---

## 4. worktree 清扫：一条命令里的完整判决

```
$ ./bin/gopi task sweep-worktrees --dry-run
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  keep        [main-checkout] age=0h /home/user/work/gopi (the main checkout (removing it would delete the repository))
              judged against root: no-root
  reap        [dead-registration] age=0h /home/user/workspace/gopi-test-scratch/queue/queue-runtime-merge-build-2288221/merge-build-gate-1066649708/wt-foo (registration is dead (its directory is gone; no branch to consider))
              judged against root: no-root
  keep        [main-checkout] age=0h /home/user/workspace/01a1aa6c-faa1-7a35-b04c-678d9824a3b5 (the main checkout (removing it would delete the repository))
              judged against root: no-root
run-ground anchors: collectable=85 gone=1526 kept=95
exit=0
```

每一行都带**判决理由**（`[main-checkout]` / `[dead-registration]`）+ **它被判时用的根**
（`judged against root: no-root`）。最后一行 `collectable=85 gone=1526 kept=95` 是三轮计数的分解 ——
`gone=1526` 是"锚已不存在"的量。这是 D13（可审计）与本维的交叉点：**删除是有记录的**。

`dedup-scan` 也跑通了（无输出、exit=0，表示当前账本没有可折叠的重复行）。

---

## 5. help-only / dead-code（要提 bug 的发现，Rule 41）

### 5.1 `--since` / `--verbose` 在 brew 上是 help-only，且解析器不严格

见 §1.5。已提 bug **`01a1ac90-530d-7af0-b4d1-792daf415b85`**。
一句话：`gopi memory brew [--since DURATION] [--dry-run] [--verbose]` 里的两个 flag 实现里没有，
而 `fs.expect()` 一次都没调用，于是它们与任何拼写错误一起被静默吞掉并**真跑一次 LLM**（23.5 s / exit=0）。

### 5.2 `merger` / `serve`：**不是**缺陷（本档早期版本判错了，此处更正）

早期版本写"`merger` 在页上被广告，但没有 dispatch case"。**复测推翻了这个断言**，两处都错：

1. **它没有被广告**。synopsis 行里没有 `gopi task merger` / `gopi task serve`
   （`gopi task -h | grep -E '^  gopi task merger'` 空输出）；它们只出现在页末一段
   **明确标注 RETIRED** 的散文里：

   ```
   $ sed -n '497,500p' cmd/agent/cli_tasks.go
   The RESIDENT MERGER DAEMON is
   RETIRED (2026-09-19, devdoc/merger-retired.md): 'gopi merger' and its
   deprecated alias 'gopi task merger' / 'gopi task serve' refuse and exit 1.
   ```

   **页面的说法与实测完全一致** —— 它说的是"refuse and exit 1"，不是"仍可用"。

2. **`exit=0` 是错的**（早期版本的读数来自管道/`PIPESTATUS` 误读）。先落盘再读：

   ```
   $ ./bin/gopi task merger > /tmp/m1.txt 2>&1 ; echo exit=$?
   exit=1
   merger: retired — the resident merger daemon was retired for re-development: `--stop` is only
   answered at a TICK BOUNDARY, so an in-flight tick cannot be interrupted — a merge the operator
   cannot stop is not patched in place (see devdoc/merger-retired.md)
     merging itself is NOT retired: it is reached through the family spelling `gopi task merge`
     (same engine, same lock)
   $ ./bin/gopi task serve  > /tmp/m2.txt 2>&1 ; echo exit=$?
   exit=1                    （同一段文案，动词名换成 serve）
   $ ./bin/gopi merger      > /tmp/m3.txt 2>&1 ; echo exit=$?
   exit=1
   ```

**结论：这是本片看到的最好的一个退役样本，不是缺陷。** 它同时做到了三件事：
synopsis 不再列它（不骗人）、页末一段说明它退了并给出原因（`--stop` 只能在 tick 边界应答，
所以"操作员停不下来的 merge 不在原地打补丁"）、退役文案本身把读者指向仍然活着的入口
（`gopi task merge`，同一引擎同一把锁，`cmd/agent/cli_tasks.go:502-507`）。
`exit=1` 而不是 `0` —— 脚本能靠退出码判断"这个动词已经不能用了"。

**顺带更正一个口径**：`merged-cleanup` 也曾被我误归退役，理由是它输出里有 `removed=0`
这个**统计字段**（`merged-cleanup (<dir>): removed=0 would-reap=0 skipped=0 failed=0`）。
它是 ACTIVE：`exit=0` 且真扫了 `4219 close-out candidate row(s)`。
这正好说明"用 `*removed*` 匹配首行来判退役"这个启发式会假阳性 —— 判据必须是
`": removed — "` 这个**带冒号破折号的固定形状**，或干脆读退出码与 help。

### 5.3 `memory memsync` 页头把同一个拼写写了两遍，且"still works"与实测相反

页头原文（`<session>/artifacts/help/memory_memsync.txt` 前 5 行）：

```
GoPi memsync — node→node sync (git-PR semantics). Two channels:
brew rows (knowledge base) and procedural rows (workflows).
ONE command, two spellings: gopi memory memsync is the live home and gopi memory memsync
still opens — the demotion moved the door, never the command (epic
verb-restructure, 2026-10-05).
```

第 3–4 行是**同一个拼写说了两遍**（要对比的那个旧拼写是 `gopi memsync`），
而"still opens"与实测相反：

```
$ ./bin/gopi memsync
gopi: `memsync` is RETIRED — use `gopi memory memsync` (`gopi memory memsync -h` for its help).
To send the word as a prompt instead, say so explicitly: `--prompt memsync`.
$ echo $?
1
```

group 页重复同一句错话（`cmd/agent/cli_memory.go:161`）：

```
      'gopi memory memsync' spelling still works. Full semantics:
```

已提 bug **`01a1ac90-8d4f-7d24-8bfd-e453ea03fa02`**。

### 5.4 `gopi notify` 承诺"每个子动词有自己的页"，实测三个 `-h` 与 group 页**完全相同**

页上原文：

```
Every sub-verb answers its OWN page: gopi notify <sub> -h prints that verb's
flags and examples — this page only says which of the three routes you want.
```

实测 md5（四次 `-h`，同一页）：

```
$ for s in events failed sink; do printf "%-8s lines=%s md5=%s\n" "$s" \
    "$(./bin/gopi notify $s -h | wc -l)" "$(./bin/gopi notify $s -h | md5sum | cut -c1-8)"; done
events   lines=75 md5=3c3a1c06
failed   lines=75 md5=3c3a1c06
sink     lines=75 md5=3c3a1c06
$ ./bin/gopi notify -h | md5sum | cut -c1-8
3c3a1c06
```

三次都是 `exit=0`（`events -h exit=0` / `failed -h exit=0` / `sink -h exit=0`）——不是拒绝，是**内容没有分叉**。
对照：`gopi task archive -h` 确实给了独立页（108 行，md5 `cf661abf`，与 family blob `4d03aad5` 不同），
说明"独立子页"这个机制在别处**能工作**，notify 这三个是漏网。
已提 bug **`01a1ac90-8d66-7ba6-a47e-7abf3477bb82`**。

### 5.5 同一模式的更大一处：`memory` 的 15 个子命令里 8 个共用 group 页

```
$ for v in $(./bin/gopi memory -h | grep -oE 'gopi memory [a-z-]+' | sed 's/gopi memory //' | sort -u); do
    printf "%-18s %s lines=%s\n" "$v" \
      "$(./bin/gopi memory $v -h 2>&1 | md5sum | cut -c1-8)" "$(./bin/gopi memory $v -h 2>&1 | wc -l)"
  done
backfill-tokens    33ff8e06 lines=3
backup             622d4bd1 lines=76
brew               e37ca081 lines=12
capture-stats      15a196bf lines=27
converge-keywords  8d102c73 lines=5
delete             ac6890c2 lines=173
feedback           ac6890c2 lines=173
get                ac6890c2 lines=173
list               ac6890c2 lines=173
memsync            0d82ae12 lines=87
provenance         ac6890c2 lines=173
search             ac6890c2 lines=173
store              ac6890c2 lines=173
sync               82d32ced lines=109
update             ac6890c2 lines=173
```

`ac6890c2 / 173 行` 正是 `gopi memory -h` 自己（实测 `./bin/gopi memory -h | md5sum` → `ac6890c2`，173 行）。
→ **7 个子命令有独立页，8 个（delete/feedback/get/list/provenance/search/store/update）回落到 group 页。**

注意这与 §5.4 不同：`memory` 页**没有**承诺每个子命令独立页（`grep "own page\|-h\` for\|each sub"` 无命中），
所以它是**体验缺口而不是谎言**；而 `notify` 页明确承诺了，所以那一条是 bug。
**同一个现象、两种定性，判据是页面有没有承诺** —— 这正是本框架 §2 四级口径要守的线：
不因为"我看到它没做"就记 none，要因为"它说了却没做"才记 defect。

顺带一个**页内不一致**（未提 bug，记为观察）：`backfill-tokens` 的 `-h` 只有 **3 行**，
`converge-keywords` 只有 **5 行**，比 `brew`（12 行）还短 —— 这两个子命令各自是 D16 的治理腿，
页面上却几乎没有可抄的用法。它们**有**独立页（md5 不同），只是内容太薄。

### 5.6 `task` 家族的子页：不是"全打印 family 页"，而是**五档**（38 个动词全测）

`gopi task <verb> -h` 对**全部 38 个动词**跑一遍，按 `(md5, 行数)` 归并：

```
   21 ×  4d03aad5  184 行   ← family 页（回落）
    7 ×  4644d2b4  262 行   ← sync 家族的页：diff harvest pull push receive send sync
    1 ×  cf661abf  108 行   ← archive   自己的页
    1 ×  77ca3bd6   34 行   ← merge     自己的页
    1 ×  5f89ae6d   27 行   ← sync-pass 自己的页
    4 ×  (23 行，各自不同)   ← adopt / audit-merges / merge-execute / merged-cleanup
    3 ×  ( 1 行，各自不同)   ← merger / serve / run（退役，`-h` 只打那行退役文案）
```

即：**7 个动词有自己的页**（`archive` 108、`merge` 34、`sync-pass` 27、`adopt`/`audit-merges`/
`merge-execute`/`merged-cleanup` 各 23 行）、**7 个共享 sync 家族页**、**21 个回落 family 页**、
3 个退役只回一行。

**21 个回落到 family 页的动词里包含最常用的五个**：`list show status submit update` ——
查一条行、看状态、交一条任务、改状态，全部只能拿到 184 行的总页去自己找。

这**不是** §5.4 那类缺陷，因为 `task` 页**没有承诺**每个动词独立页（它是"一个 family 页 + 一份动词表"）。
与 `notify` 的差别正是"页面有没有承诺"。

但有两个事实值得记：

- **`merge` 只有 34 行** —— 它是 `gopi task merge`，整套系统里最重的动作之一（合分支、跑声明
  build、验版本、失败发 `build-failed`），而它的全部页面是 34 行，比 `archive` 的 108 行还短。
- **`sweep-worktrees` 完全没有自己的页**（回落 184 行 family 页）—— 它同样是会删东西的动作。
  页面详略与动作危险度**不相关**，说明详略不是按风险排的。

顺带一个**页内不一致**（未提 bug，记为观察）：`backfill-tokens` 的 `-h` 只有 **3 行**，
`converge-keywords` 只有 **5 行**，比 `brew`（12 行）还短 —— 这两个子命令各自是 D16 的治理腿，
页面上却几乎没有可抄的用法。它们**有**独立页（md5 不同），只是内容太薄。

### 5.7 计数器读数**带时间戳**：同一命令两次读不同，是账本在长，不是哪一次写错

本档所有"行数 / 条数"读数（`provenance --shape` 的 `total rows`、`memory -h` 的动词数、
`sync --dry-run` 的行数）都是**某一刻**的读数，不是常量。实测对照：

```
$ ./artifacts/bin/gopi memory provenance --shape     # 16:4x，写作本档时
total rows: 27973
addressable: 15576   untraceable: 12397

$ ./artifacts/bin/gopi memory provenance --shape     # 01:01，复核本档时
total rows: 27981
addressable: 15579   untraceable: 12402
```

差 **+8 行 / +3 addressable / +5 untraceable**，来源是本档自己的 D16 自证写入
（`#28021`）加并行会话的写入 —— `memory.db` 是 append-only 的，只增不减。

**所以判据不是"两个数必须相等"，而是三件事**：
(1) 计数命令本身可重跑并复现**同一条**命令；
(2) 差值能被解释（谁写进去的）；
(3) 文档里写下的读数**标明采集时刻**，后来者复读不同时先看(2)，不要先改文档。
反过来，若差值不能归因于写入，那才是缺陷 —— 这正是 §2 那批"用错二进制"的读数被判废的原因：
它们不是漂移，是**采集工具错了**（`bin/gopi` 落后 HEAD 14 个提交），两种错必须分开。

---

### 5.8 本片提的四条 bug 的账本状态（复核时刻实测，可重跑）

复核时刻 `2026-11-06T01:02:15+08:00`，命令与原始输出：

```
$ ./artifacts/bin/gopi task show <id> | grep -E '^status:|^summary:'
```

| 行 id | 状态 | 一句话 |
|---|---|---|
| `01a1ac90-530d-7af0-b4d1-792daf415b85` | in_progress | `memory brew` 的 `--since`/`--verbose` 是 help-only |
| `01a1ac90-8d66-7ba6-a47e-7abf3477bb82` | in_progress | `notify` 页承诺子动词独立页，三个实测与 group 页逐字相同 |
| `01a1ac97-513c-77a8-9bee-659e9914dcfe` | pending | `memory memsync -h` 自相矛盾 + 声称退役的拼写仍可打开 |
| `01a1ac9e-d0ab-7822-a2bf-8cf82117d8dd` | pending | `task dedup-scan` / `task merge` 无参挂死（15 s 超时 exit=124） |

**两条 pending 不是缺陷未复现，是父门挡着（Rule 33）**。`gopi task list --status pending`
的原始输出（同一时刻，节选该行原文）：

```
#01a1ac9e-d0ab-7822-a2bf-8cf82117d8dd  [pending]  bug: `gopi task dedup-scan` / `gopi task merge` 无参时开跑而非报缺参数
  (model deepseek-flash, source agent, created 2026-11-06 00:50:51) [queue-skip] parent
  01a1ac97-513c-77a8-9bee-659e9914dcfe not released (status=pending; releases on completed|superseded|verified)
#01a1ac97-513c-77a8-9bee-659e9914dcfe  [pending]  `gopi memory memsync -h` 说「two spellings」却把两个拼写写成同一个词
  (model deepseek-flash, source agent, created 2026-11-06 00:42:40)
```

`01a1ac9e` 带 `[queue-skip]` 标记，`01a1ac97` 无标记（它就是下一条可认领的）。
→ **"pending"在这里读出来的是依赖关系，不是"没人管"** —— 这正是 §2 那套
`[next]` / `[queue-skip]` 标记存在的理由：状态列说 pending，标记列说"还轮不到它"。

---

## 6. 未单独提 bug 但记下来的观察

1. **`task clone --dry-run` 在拒绝时 `exit=0`**（§2.4）。拒绝写进了正文，退出码却是成功。
   `--dry-run` 语义下"什么都没写"是真的，所以 0 说得通；但把 `--dry-run` 与"不可克隆"叠在一起时，
   退出码不再是判据。**推断**（未实测其它 `--dry-run` 路径）：这是同一个 `dryRun` 分支的普遍行为。
2. **`memory provenance <id>` 不接受位置参数**（§1.3 实测 `unexpected positional argument "1"`）。
   要按 id 看 provenance 得走 `memory get <id>`。页面没承诺位置参数，所以是观察不是缺陷。
3. **`brew` 是概率性的**：同一个 `--limit 1`，一次 `errors=0 accepted=5`，一次 `errors=1 accepted=7`（§1.5）。
   原因是模型返回不合规 JSON 时会丢一条。它**报出来了**（`brew: no JSON object in reply:
   {"name":"<主题>","trigger":"<触发条件>"` —— 模型原话原样打出来），
   所以是"有噪音但可见"，不是静默失败。
4. **`session stats` 拒绝裸调用**并要求指明输入（`pick one input: <session-id>, --branch <branch>,
   --worktree <path>, --last [N] or --all`）。这是"没有默认值"的选择，值得记：它让
   `session stats` 不能被误当作全局统计。

---

## 7. D16 / D17 两维的行（本片产出）

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D16 | full | E1+E2+E3 | `gopi memory -h`（15 个子命令）；`gopi memory store --topic '阶段2B自证 D16' --question '怎样自证跨会话记忆可写可读？' --answer 'E3 roundtrip probe probe-d16-14900'` → `Memory stored (id=28021).`（exit=0）；`gopi memory search 'probe-d16-14900' --limit 1` → `#28021 阶段2B自证 D16 tier=warm conf=0.85 lesson=lesson when=2026-11-05 16:45:32 eff_conf=0.86 tokens=1`（exit=0，id 往返一致）；降级往返：`memory update 28021 --tier cold` 后同一查询首位不再是 #28021，`--tier warm` 后回来；`internal/tools/memory_brew_run.go:99 func RunBrew`、`cmd/agent/cli_brew.go:133 func maybeBrewOnSessionExit`；`gopi memory provenance --shape` → `total rows: 27973 / addressable: 15576 / untraceable: 12397`（16:4x 读数；同一命令 01:01 复读为 `27981 / 15579 / 12402` —— 差 +8 行来自本次 D16 自证写入的 #28021 及并行会话写入，**这是账本在长，不是文档写错**，见 §5.7） | 写→检索→治理（update 降级/复原）→来源分类四腿全跑通。**help-only 缺陷在 `brew` 的 `--since`/`--verbose`**（本维的 producing 腿，§5.1 已提 bug）；不影响判 full，因为 full 的证据串是 store→search，两条都是真的 |
| D17 | full | E1+E2+E3 | `gopi task -h` + `gopi worker -h`；`gopi task submit "probe: 阶段2B D17 E3 自证（fixture，不派发）" --tags fixture --payload @/tmp/p17.md` → `Task queued: 01a1ac99-e0eb-74f2-b275-777641ef6e35`；`gopi task show 01a1ac99-e0eb-74f2-b275-777641ef6e35` → `status: pending`（两命令均 exit=0，id 往返一致）；`internal/tools/tasks.go:7784 func TaskSubmit`、`:8855 func taskParentGateDenied`、`:9124 func taskRowClaimable`；`gopi worker --status --json` 返回 `workers[]` 带 `pid`/`task`/`image_revision`；`task list --status pending` 里 `[queue-skip]` 与 `[next]` 并存 | 提交→按 id 读回满足 full。**可认领**（`[next]` 恒 1 条）与**可回溯**（父门 `[queue-skip]` 带状态原因）都在输出里可观察，不是只写在代码里。12 个退役动词给的是**替代地图**而不是 404 |

**判 full 的边界（不许含糊）**：D16/D17 判 full 依据的是**单机**往返 + 治理腿。
**跨节点那一层没有跑**（`task_sync=false`，`memory sync --peer` 只做了 `--dry-run`）——
如果 stage3 要拿这两个 full 去论证"多机协同已通"，那是**超出本片证据范围**的推断。

---

## 8. 交付前的自纠（每条都是复测推翻了本档自己的早期版本）

本档在复核阶段被自己的二进制推翻了 5 处。全部逐条记下，因为它们比正文更能说明取证纪律：

| # | 早期版本的说法 | 复测事实 | 根因 |
|---|---|---|---|
| 1 | `task` 有 31 个 dispatch 动词，页广告 22 个，`merger` 是"广告了但无 dispatch" | 常量 **38**；`case` 去重 **32**；synopsis 广告 **11**（不含 `merger`/`serve`，页末另有 RETIRED 段明写它们 refuse）；`merger`/`serve` 实测 `exit=1` | 三个量口径不同被混用；且 `merger` 被从散文里"看见"就当成了 synopsis 条目 |
| 2 | `gopi task merger` / `gopi task serve` 返回 `exit=0` | 实测 **`exit=1`** | `rc=${PIPESTATUS[0]}` 写在命令替换的子 shell 里，读不到管线退出码 —— GOPI.md §4.10 那个陷阱，我踩了原版 |
| 3 | `merge` 在 synopsis 被广告、`sweep-worktrees` 也是（"22 个"列表里） | 两个都在 11 个广告之外；13 个活着的动词页上不可发现 | 同 #1 |
| 4 | `memory search --tier cold` 能把记录降级，并给出了 `tier=cold` + `eff_conf=0.91` 的读数 | `search` **没有** `--tier` flag（`unknown flag --tier`，exit=1）；真实通道是 `update <id> --tier cold` | 把 `memory -h` 里 `update --tier` 的 synopsis 记成了 `search` 的 |
| 5 | `worker` 承诺 11 种形态 | **16** 行 synopsis（= 15 个可执行面 + `-h`），其中 3 行是同一个读面，**独立能力 13 个** | 手抄时漏行，没有用 `grep -c` 数 |

**另一个反向的收获**：我在复核中差点把 `merged-cleanup` 也判成退役动词 —— 因为它首行含
`removed=0`（那是个**统计字段**）。这提醒一条判据：**退役的判据是
`": removed — "` 这个带冒号破折号的固定形状，不是出现 `removed` 这个词**。
同样地，`--nonsense-flag` 也不是"动词存在性"的判据（有些动词静默吞 flag，见 §1.5 #3）。

**还有一处是规则层面的**：初版正文里有 **18 处省略号截断引用**（规则 6 明令禁止）。
本文定稿时逐处取回原文补全。核验命令与预期输出：

```
$ python3 -c "import sys;d=open('devdoc/competitor/gopi-self-b.md',encoding='utf-8').read();print(d.count(chr(0x2026)))"
0
```

（用 `chr(0x2026)` 而不是把字符本身写进命令 —— 否则这条核验命令自己就成了第 1 处命中，
这正是初版的毛病：**判据命令自身必须是干净的**。U+2026 就是被禁用的那个省略号字符。）

**计分（本片范围）**：D16=2、D17=2，`n=2`（只计本片覆盖的两维，不与 17 维总分混算）。

---

## 9. 与 8 家对手的同维对照（只引已落盘的 stage2 文件）

这 8 份是阶段 1B 已提交的 `devdoc/competitor/*.md`。我**只引用它们自己写的等级**，不重新判定：

| 对手 | D16 | D17 | 该文件对这两维的关键证据 |
|---|---|---|---|
| hermes | partial | partial | D16：guide `:5` "it creates skills from experience, improves them during use, persists knowledge"；`agent/learning_graph.py:4` "skills (agent-created or used) plus ``MEMORY.md`` / ``USER.md`` chunks"。D17：`hermes@84a0301:agent/legacy_cli.py` 入口，但"headless 模式"未取到 |
| openclaw | partial | partial | D17：`packages/gateway-protocol/src/session-result-validators.ts:3` `NativeSubagentsSnapshotSchema`；`session-url-contract/src/session-key.ts:7` 正则含 `subagent` 段 —— 多 agent 是协议一等公民 |
| 其余 6 家 | 见各自文件 | 见各自文件 | `astrbot.md` / `deep-code.md` / `deepseek-tui.md` / `langcli.md` / `nanobot.md` / `reasonix.md` |

**一个可复核的统计事实**（不是评价）：在这 8 份已落盘文件里，
用 `LC_ALL=C grep -ci 'beta posterior|confidence|vote|useful|demot|\btier\b'` 逐家计数，
**8 家全部为 0**：

```
$ for f in devdoc/competitor/*.md; do printf "%-18s %s\n" "$(basename $f)" \
    "$(LC_ALL=C grep -ciE 'beta posterior|confidence|vote|useful|demot|\btier\b' $f)"; done
astrbot.md         0
deep-code.md       0
deepseek-tui.md    0
hermes.md          0
langcli.md         0
nanobot.md         0
openclaw.md        0
reasonix.md        0
```

**这只是推论，不是结论**（框架 §9 的纪律）：它说明"这 8 份文件里没有记录投票/置信度/tier 这类机制"，
**不说明**这 8 家没有这类机制 —— 它们的 D16 全部停在 `partial`，最可能的原因是我方 E3 通道对它们
结构性不可用（框架 §1/§6），而不是"没有"。同样地，这条读数也可能只是那 8 次采集的**探针没覆盖到**
这一层。要下"别家没有可投票记忆"，需要一次专门针对 `feedback`/`vote`/`confidence` 的 E1+E2 采集。

**能说的是**：在我方，`tier`/`conf`/`eff_conf`/`fb=` 是**同一条命中行上的可见字段**（§1.2 实测），
且投票真的改动了下一次检索的排序输入（`conf` 0.85→0.90，`eff_conf` 0.91 出现）。
这是一个**可复现的机制**，不是文档里的一句话。

---

## 10. 本片自己的边界（不掩盖）

1. **本片没有跨节点实测**。`task_sync=false` 是当前配置（§3.3），所有跨节点腿只做到"代码在、命令在"，
   没有一次真实的 peer 往返。要不要打开它是操作员的决定，不是本片能验的。
2. **本片在操作员的 `~/.gopi/memory.db` 上写了 1 条记录 —— 这是一次违规，如实记下。**
   §1.2 的探针正确地带了 `--db <session>/artifacts/probe.db`（新库，id=1，未污染真账本）。
   **但 §1.3 / §1.4 的那三次调用漏了 `--db`**，于是落进了真账本（771 MB），实测读回：

   ```
   $ ./bin/gopi memory get 28021          # 默认库 = ~/.gopi/memory.db
   exit=0
   #28021 阶段2B自证 D16 tier=warm conf=0.85 lesson=lesson when=2026-11-05 16:45:32 eff_conf=0.89
   Q: 怎样自证跨会话记忆可写可读？
   A: E3 roundtrip probe probe-d16-14900
   ```

   受影响的三次调用：`memory store`（造出 #28021）+ `memory update 28021 --tier cold` +
   `memory update 28021 --tier warm`（§1.4 的降级往返）。**这是 Rule 32 的典型踩法**：
   少写一个 `--db`，就写进了 append-only 的 operator 账本。
   撤回命令（**未执行** —— 那是对 operator 账本的写操作，由 operator 决定）：

   ```
   ./bin/gopi memory delete 28021
   ```

   保留它的可辩护之处：#28021 的正文自述是"阶段2B自证 D16 / E3 roundtrip probe probe-d16-14900"，
   检索时不会被误当成经验；但它确实占了真账本一行，**这就是代价本身**。
   写成边界条目而不是悄悄改文档，因为一个把踩坑藏起来的取证报告比没写更贵。
3. **`brew` 的两次干跑真花了两笔 LLM 调用**（16.3 s + 23.5 s）。这是 E3 的代价，记账在这里。
4. **`fixture` 那条任务行 `01a1ac8c-744b-77e0-91d4-b17913be009a` 留在账本里**（status=pending，
   永久被 `fixture` tag 排除认领）。这是 D17 证据串的一部分，不该删。
5. **本片不打 17 维总分**。理由在开头：另 13 维在别的切片里，本片只有 2 维的行。把它们拼成一个
   "gopi 总分"会重复计分。
6. **`--limit 1` 意味着 brew 的"挑选逻辑"本片没测**。`brewMaxSessions = 64`
   （`internal/tools/memory_brew.go:70`）是页面上没写的一个上限 —— 它与 §5.1 的假 `--since`
   是同一件事的两面：用户以为自己在用时间窗，实际用的是"最新 64 个 session"。
7. **本档的每一个计数读数都是某一刻的读数，见 §5.7。** 本档复核于
   `2026-11-06T01:01:08+08:00`，与写作时刻（16:4x）之间账本长了 8 行；因此本档里
   `total rows: 27973` 这类数字**不是**可用来判对错的常量，而是带时间戳的采样。
   复核命令与两次输出已落盘：`<session>/artifacts/stage2b/final-recheck.txt`。
