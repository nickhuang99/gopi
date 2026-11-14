<!-- doc-class: normative -->
# Task Report: `gopi task pull` 的到达方向依赖 gate（与「边在传输层静默丢失」的根因）

> 本文在 branch `task/01a1165c9a40787ca10f5937e2323b79` 上重做（Rule 23）：
> 原分支 `task/01a11639b2d375d6a2bddf5d34a631bf` 的作者结论保存下来，**代码按 master 的
> 当代版本重新生成**，不是把旧 diff 打上去。

## Completed

- **判据的第二半，落在同一份定义里**：`internal/tools/task_transfer_eligibility.go` 追加导出入口
  `TaskTransferArrivalEligible(row SyncTaskRow, dep TaskDependencyView) (bool, string)` —— 与
  send/MOVE 腿的 `TaskTransferEligible` 同文件、同一定义（Rule 14）。
  它判的是**行自己那条边**能否在本账本被兑现，**复用家族的既有谓词**
  （`syncParentUnresolvable` / `syncParentVerdict` + `taskParentReleaseStatuses`），
  所以判据、apply、rollback 不可能对同一条边给出不同答案。
- **接进 pull 腿**：`internal/tools/memory_sync_row.go` 的 `SyncRowReceive` 里，`fetch` 之后、
  `SyncTaskImportDelta` 之前（declared-peer 门之后）。拒绝用 `syncRowArrivalGateFormat`，
  按名给出挡路的父行 id 与 release 集合。
- **镜像 merge 自己的回填门**：`taskArrivalWritesTheEdge` 照抄 `syncDeltaMergeRow` 的分支
  （本地行不存在 → INSERT 整条写边；远端 `UpdatedAt` 更新或 `statusRank` 更高 → merge 会走到边回填；
  其余形状 merge 根本不写边）。判据只拒「merge 会写而写不成」的边。
- **live parent 不拒**（设计的链式流）：父行 pending/claimed/in_progress 一律放行，边随行落地，
  Rule 33 再把它 `[queue-skip]` 停车。

## 冲突是怎么解掉的（Rule 23：按「代」重生成，不按口味）

两个文件都不是「挑一段」能解决的，必须看两边各自的**代**：

| 文件 | master 侧 | 本分支侧 | 解法 |
|---|---|---|---|
| `internal/tools/task_transfer_eligibility.go` | `e9e1b2963`（push 腿的判据，199 行，且 `TaskTransferEligible` 的**签名带 `skipSet`**，读过 `task_transfer.go` 三个调用点） | 原分支版本是**同一文件的分叉重写**（471 行，签名不同，还自作主张给 `TaskTransferDependency` 加了 `Dependents`/`StatusesByID`） | master 的版本在前，本行**只追加**到达方向的一节（接口 `TaskDependencyView`、生产视图、`sqlPlaceholders`、`taskArrivalWritesTheEdge`、两个 reason 常量、`TaskTransferArrivalEligible`）。**签名一个字没动** |
| `internal/tools/memory_sync_row.go` | `d0de49ba3` 之后：`syncRowRemoteSelect` 已经引用**共享常量** `syncRemoteCoalescedProjection`（含 `COALESCE(parent_task_id,'') AS parent_task_id`），pull 腿与窗口腿共用一份 projection | 原分支版本把那一列**原样写在 SELECT 里**（12 列裸表达式 + 一个 `AS`） | 保留 master 的共享 projection（它是更新的那一代），表头与接收腿部分按本分支语义补上 |

> 一张实测的表：`git merge-base --is-ancestor bb6ee97f9 e9e1b2963` → 真；`e9e1b2963` 是 master 祖先。
> 也就是说**原分支的 base 是 e9e1b2963 的前身** —— 它的 `eligibility.go` 与 master 上的那份是
> 同一条设计线的两个分支点，master 那一支已经落进 `task_transfer.go` 的三个调用点。

### `AS <field>` 那个 wire-key 修复：master 已经先到一步，本行的职责变成了「证明它真的成立」

原报告 §Evidence ④ 抓到的是真 bug：`sqlite3 -json` 用**结果名**给列起 key，裸列保留列名，
**表达式列用整段原文**，于是 `COALESCE(parent_task_id,'')` 回来的 key 是那串 SQL，`encoding/json`
绑不上 `parent_task_id` → **边在传输层静默丢失，pull 照样报 `stored`**。

在本行之前，master 上已经有两次修复：
`e9e1b2963` 引入 `syncRemoteCoalescedProjection`（五个表达式列一律带别名），
`d0de49ba3` 给窗口腿补了**逐列按位置**的门（`TestHarvestRemoteSelectNamesEveryColumnForTheWireStruct`）
并把 `syncColumnAliasKeyword` 提成唯一拼写。

所以本行**不再重复那次修改**，而是把结论变成一条 master 上还不存在的证据：两个已有的门都只读
**查询文本**，谁都证明不了「真 `sqlite3` 出来的 JSON 真的绑上了 edge」。新增
`internal/tools/task_transfer_arrival_wire_test.go` 用真二进制跑真查询，断言
`ParentTaskID` / `Reason` 真的绑进 `harvestRemoteRow`，并带一条**反向控制**：
对端 schema 更宽（没有该列）时，`AS <x>` 会变成把不存在的列改名 → `no such column`，从而
**整个 pull 腿对旧对端失效**。那条控制是**红**的（见 §Evidence ⑥），本行如实记录，没有把它
隐藏成一条更窄的断言。

## Not Done（诚实版）

- **对「比本端更旧的对端」的兼容性没有在本行闭合**（不是新引入的风险）。实测：`COALESCE(parent_task_id,'')`
  在**有别名和没别名两种写法下**，对一张没有该列的 tasks 表都是
  `Error: in prepare, no such column: parent_task_id` —— 也就是说 `AS` 只是**改名**，
  它既不产生这个暴露、也治不了它，而且失败是**响亮的**（prepare 阶段报错，不是静默丢边）。
  范围也被钉住了：该列由账本自己的迁移添加（`internal/tools/tasks.go`：`ADD COLUMN parent_task_id`），
  所以暴露面只限于「从未跑过带该列构建的对端」。要真正闭合它需要自己的任务与验收（例如对端探测或
  按列存在性选 projection），本行**不改它**，只把它量清楚。
- **存量数据不回填**：窗口腿长期丢 `parent_task_id` 意味着**已经拉进来的行可能有边而无边**。
  本行只保证「往后不再丢」，不回填历史 —— 回填需要一条自己的审计任务与验收。
- **`syncDeltaSelectColumns`（库内 delta SELECT）未改**：它走 `db.Query` + 位置 Scan，不是
  `sqlite3 -json`，同一个 bug 不适用。
- **对端 hp 上的两行探针数据留在它的私有 profile 里**（`/home/user/workspace/gopi-sandbox/
  sandbox-arrival-gate-peer-01a1165c/.gopi/memory.db`）—— 那是**本行的探针账本**，不是操作者账本，
  留着可以让任何人复跑；该目录是本行自建（hp 的 `gopi` 早于 `sandbox` verb，没有自动回收）。
  若要清理：删掉那个目录即可（内容全部由本行创建，无别的来源）。

## Verification

- Build: `go build ./...` → 无输出（exit 0）；`go vet ./internal/tools/` → 无输出（exit 0）。
- Tests:
  - `go test ./internal/tools/ -run 'TestTaskTransferArrival|TestArrivalView|TestRowReceive|TestRowRemoteSelect|TestRemoteProjection|TestHarvestRemoteSelect' -count=1` → `ok github.com/nick/gopi/internal/tools 1.616s`
  - `go test ./internal/tools/ -run 'TestRowRemoteSelectBindsThroughTheRealCLI|TestRowReceiveRefusesAnArrivalThatCannotLand|TestRowReceiveLandsAChildOnAQueuedParent|TestArrivalViewReadsTheLedgerTheCallerPinned|TestTaskTransferArrivalEligible' -count=1 -v` → 8/8 PASS
  - `make test-budget PKGS='./internal/tools'`（branch-scoped）→ 见 §Evidence ③（与 **pristine
    master 对照**同机、同 `PKGS`、同一时刻各跑一次，逐条归因）
- Ran（Rule 31）: `~/.gopi/sessions/01a1165c-9a40-787c-a10f-5937e2323b79/artifacts/run-live.sh`，
  逐字输出见同目录 `live.log`。每一步都是真命令：`gopi tool sandbox create`（Rule 34 的唯一
  初始化器，`owner=task=01a1165c…`）、对端真账本（hp 上真 `memory.db`，用真 `sqlite3` 读）、
  真的 `gopi task pull`（ssh + 对端 `sqlite3`）、`task show` / `task list`。本地每个 verb 都同时
  钉了 `GOPI_HOME=$SB` 与 `--db "$LOCALDB"`。
- Result: 拒绝臂 `EXIT=1`，句子**按名**给出父行 id 与 release 集合，且本账本 `task show` 报
  「no task with task_id」；放行臂两次 `stored`，本账本 `parent_task_id` 等于对端父行 id，
  `task list` 打出 `[queue-skip] parent … not released (status=pending; releases on
  completed|superseded|verified)`；对端账本两条行的 **status 与 parent_task_id 均未变**
  （这条腿不 cancel）。
- Commit: `c173ec89b`（本行的语义重做）+ `db4ddee2a`（delivery-point sync：把 master 合进来）
- Merge probe（Rule 30a 第 3 步的验收）: 从 master 拉一个 throwaway worktree，`git merge
  --no-commit --no-ff task/01a1165c…` → `Exit 0`，输出只有
  `Auto-merging devdoc/DOC-INDEX.tsv` / `Auto-merging internal/tools/memory_sync_row.go` /
  `Automatic merge went well; stopped before committing as requested`（逐字见
  `artifacts/merge-probe.txt`）；探针 worktree 已用 `git worktree remove --force` 回收。

## Evidence (self-filed)

**① 冲突的「代」是量出来的，不是猜的。**
`git log -1 --format='%ci %h' master -- internal/tools/task_transfer_eligibility.go` → `2026-10-07 19:52:18 e9e1b2963`；
同文件在本分支 tip 上 → `2026-10-07 20:24:58 1dc5c13c0`（更晚，但**base 更早**：
`git merge-base --is-ancestor bb6ee97f9 e9e1b2963` 为真）。`memory_sync_row.go` 同理：
master 侧最后一笔是 `d0de49ba3`（2026-10-07 23:22:07），晚于本分支的 `1dc5c13c0`。
两个文件都取 master 的结构，只叠加本分支的语义。

**② 判据本身的红绿对照（端到端，真 pull 路径 + 真账本）。**
`TestRowReceiveRefusesAnArrivalThatCannotLand` 的三段：① 拒绝且点名父行；② 本地**边为空、状态未动**；
③ 放行父行后同一条 pull 写出边（`stats.RowsParented != 0`）。中间那段 CONTROL 直接调
`SyncTaskImportDelta`（无判据在前），复现的是本判据要防的**旧行为**：merge 拒绝该边并整条回滚。

**③ `make test-budget PKGS='./internal/tools'`（branch-scoped）**：命令与逐字尾部输出记录了
本轮真跑的流文件；红项与 **pristine master 对照**（同一 `PKGS`、同机、同一时刻）逐条归因 ——
凡 baseline 也红的不算本行引入，本行新增/触达的测试名在 §Verification 的 `-run` 下全绿。

**④ 传输层丢边的取证（原报告的实测，本行复核）**：`sqlite3 -json` 的 key 规则 —— 裸列取列名，
表达式取整段原文。本行的动态门把它变成了可执行断言（`TestRowRemoteSelectBindsThroughTheRealCLI`）。

**⑤ 红绿证明（本行新门自己的）**：把 `syncRowRemoteSelect` 的共享 projection 换成**未加别名**的
裸表达式（即 bug 出货时的形状），再跑新门 →
`FAIL … the dependency edge did not bind through the real 'sqlite3 -json' (ParentTaskID="", want "t-arrival-wire-parent")`；
还原后同一条命令 → `ok`。这一步**当场证明这条门不是装饰**（Rule 10 §4）。

**⑥ 未闭合项的量（诚实）**：`AS` 是**改名**不是「有则取」——
`sqlite3 -json /tmp/wide.db "<本腿的真 SELECT>"` 对一张没有 `parent_task_id` 的 tasks 表返回
`Error: in prepare, no such column: parent_task_id (1)`；同一张表上，**没加别名**的写法返回同一条错。
所以 (a) 这条暴露**不是**别名改动引入的，(b) 它**失败得很响**（prepare 报错，不是静默丢边），
(c) 该列由账本迁移添加（`internal/tools/tasks.go`：`ADD COLUMN parent_task_id`），范围因此有界。
本行把它量清楚并留给自己的任务，没有把它藏进一条更窄的断言里。

**⑥′ 三条 key 规则的对照（同一台机器、同一时刻，实测输出）**：
`SELECT a FROM t` → `[{"a":"x"}]`；`SELECT COALESCE(parent_task_id,'') FROM t` →
`[{"COALESCE(parent_task_id,'')":"P1"}]`（**这就是原 bug**）；
`SELECT COALESCE(parent_task_id,'') AS parent_task_id FROM t` → `[{"parent_task_id":"P1"}]`（修复形状）。
这三行就是新铁门所依赖的机制本身。

**⑦ 操作者账本的越界与归还（必须记录）**：第一次真跑时 `task list` 步骤漏了 `--db`，在
`GOPI_HOME=$SB` 之下读了操作者账本 `/home/user/.gopi/memory.db`，两个探针行
（`01a11735-2a10-…` / `01a11735-2a29-…`）因此落进那里。发现后按**原始 DB 格式**
（`.backup`，非 SQL dump）备份到
`~/.gopi/sessions/01a1165c-9a40-787c-a10f-5937e2323b79/artifacts/operator-memory-before-undo.db`，
执行 `DELETE`（同一目录 `undo-operator-ledger.sql`，可重放），并核对
`tasks` / `tasks_fts` / `tasks_tri` 三处计数归零、`pragma integrity_check` = `ok`。
脚本随后改为**每个本地 verb 同时钉 `GOPI_HOME` 与 `--db "$LOCALDB"`**，重跑后全程未再触达。

**⑧ `git status --porcelain`（close-out 前）** → 打印为空。

## Files

- `internal/tools/task_transfer_eligibility.go`（追加到达方向一节：视图、两个 reason 常量、
  `taskArrivalWritesTheEdge`、`sqlPlaceholders`、`TaskTransferArrivalEligible`）
- `internal/tools/task_transfer_arrival_test.go`（新：判据表 + pull 腿端到端 + 视图钉账本）
- `internal/tools/task_transfer_arrival_wire_test.go`（新：真 `sqlite3 -json` 动态绑定门）
- `internal/tools/memory_sync_row.go`（`SyncRowReceive` 的到达前置判据 + 拒绝前缀常量）
- `devdoc/task-pull-dependency-gate.md`（本文）
