<!-- doc-class: historical -->
# Task Report: 跨账本行搬运 —— 对端交接（epic task-pull 交付物 ①）

## Completed

**一条 trans 的完整实现：从对端账本把一行"搬"到本机，行 id 不变，全局唯一。**

### 新增文件

- `internal/tools/task_transfer.go` —— 传输腿本体：
  - `TaskTransferEligible`（导出的双向判据入口）、`TaskTransferLiveChild`（反向边查询）
  - `SyncTransferEligible` / `TaskTransferSelectOne` —— 对端只读选择器（`--next-eligible [ID]`）
  - `SyncTransferHandoverFlag` / `TaskTransferHandover` / `syncTransferSignOver` —— 对端交接写（`--handover ID`），
    CAS `pending → cancelled`，CAS 输了**按名报出实际状态**（`SyncTransferCASRefusal`）
  - `SyncTransferTakeOver` —— 发起方：offer → sign → fetch → import → admit
  - `TaskTransferAdmit` —— 落地方准入：只在"确是本交接的副本"时 claim，其余四种形态各按名拒绝
  - `SyncTransferPeerFace` —— 对端侧两个面统一入口（JSON 文档输出，pid/别名都写进文档）
- `internal/tools/task_transfer_eligibility.go` —— 双向判据（正向：非 pending / 有 parent / skip-tag / 无 payload /
  merger-resolve；反向：**本账本里还有活着的孩子**），`why` 每条可打印。
- `internal/tools/task_transfer_test.go`、`internal/tools/memory_sync_remote_projection_test.go`、`cmd/agent/task_takeover_trigger_test.go`
  —— 见下。

### 修改文件

- `internal/tools/memory_sync_row.go` —— `SyncRowReceiveOptions` 增加 `Handover` / `Message` 两个钩子（交接在 fetch/import
  **之前**，本地账本在拒绝时一字未动）；**修了一个真的 bug**：`status` 原先在 `Message` 之前读，导致刚 claim 的行
  仍报 `cancelled`（worker 触发点会据此判定"没拿到行"而丢掉它）——现在 `Message` 先跑、状态后读。
- `internal/tools/memory_sync_task.go` —— 与上面对齐的 SELECT 常量。
- `cmd/agent/cli_tasks.go` + `cli_tasks_sync.go` —— `task receive`（无 `--task-id` 即搬运，或 `--takeover` 显式指名）
  与 `task sync --next-eligible [ID]` / `--handover ID` 两个对端面；三处 help 同步。
- `cmd/agent/cli_tasks_worker.go` —— 触发点：**claim 扫描为空之后**才去问已声明的 peer，`workerRowTakeOver` 走
  与 claim 完全相同的 round body；`--once` 不触发；`task_sync` 关或无 peer 不触发；stop 请求时不新建承诺。

### 顺带修掉的一个**已在生产里静默丢数据**的根因（本任务最重的发现）

对端 SELECT 写的是 `COALESCE(reason,'')` **不带别名**。`sqlite3 -json` 对表达式列用**整段源文本**当 JSON key，
于是 `reason` / `deliverable` / `run_method` / `run_cmd` / `parent_task_id` **五个字段过线即空**——
这正是文档里"五条链落地变成无父"的根因。已抽成单一常量 `syncRemoteCoalescedProjection`（带 `AS`），
row 腿与 window 腿共用一份；`syncRemoteSQLiteBinary` 同样收敛成一处。

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → 无输出，exit 0
- **Tests**: `go test ./internal/tools/ -run 'TestTransfer|TestRemoteProjection|TestRowReceive|TestRowSend' -count=1` → `ok 2.7s`；
  `go test ./cmd/agent/ -run 'TestWorkerTakeOver|TestSyncBarePeersTestCoversEveryChangedFile' -count=1` → `ok 14.1s`
- **Ran（真机、真 ssh、真 sqlite3，逐字）**:
  - 对端（peerbox，私有 profile `/home/user/workspace/gopi-sb-transfer`）：
    `GOPI_HOME=$Z GOPI_HUMAN_TERMINAL=1 $Z/bin/gopi task submit "peer-side row for the transfer probe" --source human`
    → `Task queued: 01a11633-285f-7927-b08e-d31565a9e357 (status=pending, source=human)`
  - 发起方（HP，sandbox profile）：
    `GOPI_HOME=$SB GOPI_SYNC_PEERS=peerbox gopi task receive --peer peerbox --takeover --skip-tag pa-queue --remote-home /home/user/workspace/gopi-sb-transfer --remote-db .../memory.db --db $SB/.gopi/memory.db`
  - 拒绝路径：`bash /home/user/workspace/takeover-probe/hp-refusals.sh`
- **Result**:
  - 搬运成功：`takeover peerbox: 01a11633-285f-7927-b08e-d31565a9e357 (status here: in_progress)` /
    `imported=1 … refused=0` / `transfer: … claimed HERE (the handover's copy) and set in_progress`，exit 0；
    HP `task list` 显示 `#01a11633-…  [in_progress]`，对端 `SELECT task_id,status` → `cancelled`（**同一 id，未重 mint**），
    对端 `task list --status pending` 已看不见它。
  - 抢不到：指名再搬一次 → exit 1，`peer peerbox refused to hand over …: task … is cancelled, not pending — another writer moved it between the offer and the handover …; nothing was changed by this call`；**对端账本此后仍是同一状态，无任何补偿副作用**。
  - 对端空队列：`takeover peerbox: no claimable row`，exit 0。
  - 对端两个面自身：`{"node":"peerbox","eligible":false,"reason":"no claimable row","scanned":0}` exit 0；
    `{"task_id":"…","signed":false,"observed":"cancelled","reason":"…"}` exit 0（**竞争失败是答案而非崩溃**）。
  - 别名不可见时：对端 reason 写成 `handed over to peer (this peer cannot see the alias you reached it by)` —— 不自造名字。

### Evidence (self-filed)

- 别名 bug 红→绿：把 `AS` 去掉后 `go test -run TestRemoteProjection` → `projection item "COALESCE(run_method,'')" carries no AS alias`
  且 wire 断言打印 `"COALESCE(reason,'')":"handed over to peer peerbox"`（JSON key 就是源文本），恢复后绿。
- 状态时序 bug：`TestRowReceiveReportsTheStateTheMessageLeft` 在修复前会读到 `cancelled`（E2E 里先看到的就是这个）。
- README 未列的反向判据 A/B/C/D：`TestTransferLiveChildIsTheReverseHalf` 四条子测试，pending/in_progress 阻塞、completed/cancelled 不阻塞。
- 触发点顺序：`TestWorkerTakeOverTriggerSitsAfterTheEmptyVerdict`（形状门，带 anchor 缺失时的自述失败）。

## Not Done

- **`make test-budget PKGS='./internal/tools ./cmd/agent'` 整体不是绿的**，但失败项与本次改动无关：在**干净的 HEAD**
  上单独跑同样失败（`TestCLINamingExceptionsAreStillDispatchable`、`TestResumeKeepsADirtyWorktree`、
  `TestTaskMergeCanonicalFlagRefusalOnTheRealBinary`、`TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn`、
  `TestSyncTaskIDRefusedOnEverySessionFace` 全 9 子测试）。已用 detached worktree 在 HEAD 上复现确认。
- 对端 alias 未跨 ssh 传（initiator 的 ssh 配置里才有），对端 reason 因此写的是"看不到别名"；未新增传输通道补它。
- worker **自动**触发那条路只跑了单测（`workerTakeOverAttempt` 的四个门 + 走下一个 peer + 不执行没拿到的行），
  没跑一次真 worker 轮次（那会真起子进程并写对端账本）。
- `TaskTransferHandover` 的 skip-tag 集合在"按 id 指名"路径上取的是空集/CLI 传入值，与 worker 的
  `workerExcludedTags` 是否总是同源未做端到端断言。

## 已知残余窗口

存在且**不可消除**（这是设计，不是漏洞）：对端写完 `cancelled` 与 HP 落成 `in_progress` 之间，该行在两端**都不可被 dispatch**
——对端已是终态、本机尚未 claim。窗口内若 HP 崩溃，结果是一行"对端 cancelled、本机 cancelled"的**可见残留**，
带自述 reason（`undone with gopi task update … --status pending` 可还原），**不会静默丢失、不会被两端同时执行**
（后者才是这个顺序要杜绝的）。
