<!-- doc-class: historical -->
# memsync 的 task 通道从「广告面」剥离（epic memsync-task-detach 的第一刀）

**任务**：`01a0fe75-3b12-7f41-ade6-91bee7ebae56`
**分支**：本 run 的 worktree 分支（见报告）
**状态**：广告面剥离已交付；**单行 push/pull 动词腿未实现**，已按 Rule 26 转交（见报告 `## Not Done`）。

## 一、为什么做这一刀，而不是整件事

操作者的口径是：`gopi memsync` 是**记忆的 push/pull**，不该把 task 混进去；账本行按
`<task_id>` 的粒度走自己的通道。侦察（本 run 第一半）发现这条口径落地时撞上一件事实：

**`gopi task push` 这个名字已经被占用了，而且是完全不同的语义。**
`cmd/agent/cli_tasks_sync.go` 的 usage head（实测原文）：

> `gopi task push` — The git sync channel's manual face … client — push local `feat/*` branches
> back to the server repo (idempotent) + pull master `--ff-only`; server — fetch the peers'
> branches, clean-merge + delete them.

而 briefing 明写「**不要擅自改语义，先报告事实与判断**」。⇒ 命名必须先裁决；在裁决之前
写不出一个诚实的 `--help`。这一刀因此只做**能验证、且不需要裁决**的那一半。

## 二、这一刀改了什么（可验证）

| 表面 | 改前 | 改后 |
|---|---|---|
| `memsyncFlagNames`（广告词汇，也是裸调拒绝行打印的东西） | `--export / --import / --export-tasks / --import-tasks / --export-proc / --import-proc` | `--export / --import / --export-proc / --import-proc` |
| `gopi memsync -h` 的 task 通道 synopsis | 6 行（`--export-tasks --out …` / `--import-tasks …` 各一段，含 5 条导入语义） | **移除**；改为一句话说明 task 通道已离开本页 + 指向账本行的一行面 |
| `gopi memory sync -h` 的自述 | 「also the only way to move **task-ledger rows** (--export-tasks/--import-tasks) and procedural rows」 | task 通道一句删除（Rule 15：文案不得失真） |
| 同页「何时用 memsync 而不是 memory sync」 | 「when you sync **task-ledger rows** or procedural rows」 | task 通道一句删除 |
| 「Maintenance」段的界定 | 无说明 | 补一句「**NOT part of the sync transfer** — it moves no rows」（同一段里的 `task dedup-scan` 也属此类） |

**接受的集合是广告集合的严格超集，不是被删了。** `--export-tasks` / `--import-tasks`
仍然 PARSE、仍然 DISPATCH 到同一批 `tools.SyncTask*` 原语（Rule 14：不写第二套实现），
老脚本照旧能 round-trip 一个 patch 文件；它们只是**不再被广告**。先例是本仓既有的
`--dedup-scan` 再安置（re-home）：**永不删除一个 dispatch case**，要拒绝就带着指针拒绝。
两个拼写在代码里只有一处定义（`memsyncTaskExportFlag` / `memsyncTaskImportFlag`），
usage、dispatch、fatal 文案与测试全部引用它（Rule 13）。

## 三、★ 侦察实测到的产品事实：`--push-only --task-id` 是 claim 广播，不是落库

在隔离 profile 里对 HP 真跑（`GOPI_SYNC_PEERS=hp`）：

```
$ gopi task sync --push-only --task-id 01a0feaf-6647-78b3-9bba-c8ae3c2a0453 --peer hp
push hp: accepted (1 row(s))
$ ssh hp 'gopi task show 01a0feaf-6647-78b3-9bba-c8ae3c2a0453'
gopi task error: no task with task_id="01a0feaf-…"
```

⇒ **对端「accepted」的是裁决，不是这条行**。`SyncTaskPushOnly` 走 claim 通道
（`claimSSHExchange` → 对端 `--claim-remote`），对端只回答「能不能领」；**它不会把行放进对端账本**。

**这条必须先说清，否则这一刀会撒一个谎**：如果按原方案把 `--help` 指向
`gopi task push/pull <task_id> --peer`（当时还不存在的动词），那就是 Rule 15 的静默失真——
一个被广告、跑不通的入口。所以本期的文案指向的是**今天真实存在、且真的能跑**的那一面
（`gopi task sync --push-only --task-id …`），并在同一段里写明它**是公告不是落库**。

## 四、新增的两条守卫（Rule 10.5/13）

- `TestMemsyncTaskChannelLeftTheAdvertisedSurface`：`-h` 里不得再出现 task 通道的 synopsis；
  `memsyncFlagNames` 不得含退休拼写；**但页面必须仍然点名** `memsyncRetiredFlagNames`（移除要自我解释）；
  并且必须指向 `taskSyncRowFaceProg`（指向的拼法来自 parser 自己的常量，不会广告一个 sync 页拒绝的拼法）。
- `TestMemsyncRetiredTaskFlagsStillParse`：真二进制驱动 `memsync --export-tasks`（不给 `--out`），
  断言它**走到了自己的 dispatch 臂**（该臂的拒绝对话里点名 `export-tasks`）——证明「没被删掉」。

## 五、Not Done（诚实）

单行 push/pull 的**动词腿**不在本报告里，已转交新任务。真跑取证证明了三件事：
①今天的一行面是 claim 广播（不落库）；②pull 腿没有现成的单行实现（`SyncTaskExportDelta` 是本地导出、
无 task_id 过滤；对端取数唯一入口 `harvestRemoteTasks(peer, remoteDB, anchor)` 是锚点粒度）；
③命名撞名须先裁决。三件事都已写进转交任务的 briefing。
