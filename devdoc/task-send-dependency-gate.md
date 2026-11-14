<!-- doc-class: historical -->
# Task Report: `gopi task send` 的依赖保护（MOVE 判据）— 与 master 的判据合一

Task: `01a11648-e4eb-758f-8b64-13a3fcc79a99`（rework of `01a11599-d096-7b7d-aba5-2bfb64cc223d`）
Branch: `task/01a11648e4eb758f8b6413a3fcc79a99`
Base: master `9ea9d75b8`

## 这次 rework 要解决什么

原分支（`task/01a11599d0967b7daba52bfb64cc223d`，ref 已删，内容只存在于归档 diff）
与 master **各自**写了一份 `internal/tools/task_transfer_eligibility.go`，add/add 冲突：

| | master（epic task-pull ①，`e9e1b2963`） | 原分支 |
|---|---|---|
| 判据签名 | `TaskTransferEligible(t *Task, dep TaskTransferDependency, skipSet map[string]bool)` | `TaskTransferEligible(t *Task, dep TaskDependencyView)` |
| 反向查询 | `TaskTransferLiveChild(ctx, db, id)`（SQL，返回 youngest live child） | `TaskDependencyView.Dependents(parent)` 读接口 |
| 调用者 | `task_transfer.go` 三处（selector / 写点 / pull） | 只有 send 腿 |

两侧都做了「反向半边」，但 **接口不同**：任何一边单独取都会让另一边编译不过
（master 取分支 → `task_transfer.go` 三处调用缺第三个参数且 `TaskDependencyView`
未定义；分支取 master → send 腿的 `syncRowTransferGate` 调 2 参数版）。

**结论（Rule 14：一份定义，两个调用者）**：保留 master 的三参数判据与它的
`TaskTransferLiveChild`（它已经是「一份定义、三个调用者」），**把原分支的 MOVE 腿
折叠到它上面**——send 腿成为**第四个**调用者，而不是第二份判据。

## Completed

- **`internal/tools/memory_sync_row.go`** —— send 腿的第三个前置判据：
  - 新常量 `syncRowTransferGateFormat`（Rule 1：句子只有一处拼写）；
  - `SyncRowSendOptions.CancelSource`（`--cancel-source` 是 MOVE、不是 COPY）；
  - `syncRowTransferGate(taskID, dbPath, moves)` 排在 `syncRowStatusGate` **之后**、
    **任何 peer 被接触之前**（被拒 = 可证明的 no-op）；
  - `SyncRowSendEx` 里调用它，judgment 交给 **master 的** `TaskTransferEligible(row, dep, taskTransferSkipSet(""))`。
- **`cmd/agent/cli_tasks_sync.go`** —— 把 `opts.cancelSource` 传进 `SyncRowSendEx`（一层，不新增开关）。
- **判据只在 MOVE 上生效** —— 由实测得出（见下）：不带 `--cancel-source` 的 `task send`
  是 **COPY**，源行留在本账本、依赖图一字未动，子行带着自己的边先走是 master 已落地的
  设计流程（`memory_sync_parent_chain_test.go`：先送 parent、再送 child，边随行、由接收端的
  `syncParentVerdict` 定价）。对 COPY 施加反向判据 = 拒绝链迁移本身，且拒绝的是**不可能发生**的伤害。
- **回归测试**（随修复提交）：
  - `internal/tools/task_transfer_eligibility_test.go` —— 判据表格（live 孩子按 pending/claimed/
    in_progress 三值各拒一次并点名子行 id + 状态 + Rule 33 释放集；终态孩子放行并带 live 对照臂；
    正向（parented）臂；nil 行与非 pending 状态臂）**加上**两个端到端臂（真 send 腿、两个真账本、
    对端真 handler，不 mock merge）：
    - `TestRowSendRefusesARowWithALiveDependent`：MOVE 被拒（error 含子行 id）→ **对端一次都没被
      接触**（`sent == 0`）→ 对端账本无该行 → 源行仍 pending、子行仍 pending → 子行到 `completed`
      后**同一条 MOVE** 成功、对端持有该行。
    - `TestRowSendCopiesAChainChildWithoutAskingTheMoveCriterion`：**边界的两侧**。同一份账本里
      COPY（parent + child）放行、边随行落到对端；把同一个 parent 改成 MOVE 立刻被拒并点名 child。
      这条测试的作用是让「COPY ↔ MOVE」的边界不可能被悄悄放宽。
    - `TestRowSendRefusesAChildWhoseParentIsUnreleased`：正向臂（parent 本地 live / 本地缺席）。
  - `cmd/agent/cli_tasks_send_dependency_test.go` —— 命令线（真二进制、真账本、真对端 handler）：
    refusal 落 **stderr**、退出码非 0、对端无该行、源行与子行都未动；孩子完成后**同一条命令**成功、
    对端 `task show` 打出该行。
- **既有 master 测试全绿**（见 `## Evidence`）：`TestRowSendCarriesTheDependencyEdge`、
  `TestParentEdgeRefusedWhenThePeerLacksTheParent`、`TestParentEdgeRefusedOnATerminalParent`、
  `TestTransferEligibilityFilterArms`、`TestRowSendCancelSource*`、`TestRowSendWithoutCancelSource*`。

## Not Done / 被弃（诚实）

- **原分支的 `TaskDependencyView` 读接口与 2 参数判据没有保留** —— 这正是 Rule 14 要求去掉的
  第二份定义。它的**能力**没有丢：反向查询由 master 的 `TaskTransferLiveChild` 承担，只有
  **live** 孩子阻塞（`taskTransferLiveChildStatuses` = pending/claimed/in_progress，即
  `taskLiveStatusSQLList()`，不是第二份列表）；句子由 master 的 `taskTransferWhyLiveChildFmt`
  渲染，点名子行 id + 状态 + 释放集。原分支那套「最多 8 个 id + and N more」的多子行渲染因此
  没有移植——master 的句子只点名**一个**（youngest live child，按 claim 序），并把「释放集」
  从 `taskParentReleaseSetLabel()` 现算（Rule 33 的 {completed, superseded, verified}，**不重新列举**）。
  取一份定义的代价是取它的句子形态；两套句子并存才是缺陷。
- **短 id 接受（`taskIDResolve`）未移植**：`taskTransferRowByID` 按 `task_id = ?` 精确匹配。
  **实测过**（本行沙箱，父 `…1a` + 子 `…1b` 都在本账本）：
  ```
  $ gopi task send --task-id 01adepgate00 --peer peerbox --remote-home <peer> --cancel-source
  send peerbox: row send: task 01adepgate00 not found in this ledger
  cancel-source: 01adepgate00 LEFT PENDING here — no peer persisted the row (refused by peer peerbox:
  row send: task 01adepgate00 not found in this ledger); send is a push, not a move, so the source is unchanged
  EXIT=0        ← 源行未动（pending），对端无该行 —— 但**它没有听见依赖判据**
  ```
  即：短 id 下 `syncRowStatusGate` 读到的是「本账本没有这个 id」，与
  `syncRowCancelSource` 那条（拿到完整 id 后照样取消）**互相矛盾** —— 这是
  `taskTransferRowByID` 精确匹配与「短 id 解析」没被统一的表现，
  属独立缺陷。**不完整的修复按 Rule 26 立案再派发**（不在本行顺手改），
  立案行 **`01a117082d5473c7a8ed247ec357795d`**（tags=bug，parent=本行，
  payload 里带着上面这条逐字输出、约束与验收）。

  > **这一条的后续（2026-10-08，行 `01a117082d5473c7a8ed247ec357795d`）**：
  > 第一句里的 `taskIDResolve` **在整个仓库、整个历史里都不存在**
  > （`git log --all -S taskIDResolve` 空，`code_search` / `grep` 全空）——
  > 它是当时那条 brief 的转述错，**不是**一处未统一的第二个解析器。
  > 真因是「操作数里没有 `-` 就被当成完整 id」的**隐式形似判据**：`gopi task update
  > <前缀>` 报 `no task with task_id=…`（不是取消成功），而 MOVE 腿在读不到行时
  > 按设计把「本账本没有此行」让给 export 去说 —— 于是判据被跳过、exit 0。
  > 修法据此改为：**短 id 一律按项目唯一规则拒绝**（`internal/fullid`，与
  > `gopi log <前缀>` 同一条判据），不新增解析器、不引入前缀解析。
  > 详见上线后的 `devdoc/task-send-dependency-gate.md` 补记与
  > `internal/tools/memory_sync_row_id_resolve_test.go`。

- **pull 腿**（`gopi task pull`）不属本行：master 已落地它的 gate（本判据的第三、四个调用者），
  本行只补 send 腿。
- **未新增任何开关/env/settings key**（Rule 35）：保护不可选，绕过它的方式是把孩子处理完，
  或整条链一起走。

## Verification

- Build: `go build ./...` → OK（exit 0，无输出）；`go vet ./internal/tools/ ./cmd/agent/` → 无输出
- Tests:
  - `go test ./internal/tools/ -run 'TestTaskTransferEligible|TestRowSend|TestParentEdge|TestRowSendCancelSource|TestRowSendWithoutCancelSource|TestTransferEligibilityFilterArms' -count=1` → **ok**
  - `go test ./cmd/agent/ -run 'TestTaskSendRefusesWhileALiveChildDependsOnTheRow|TestTaskSendRefusesAChildWhoseParentIsUnreleasedAtTheCommandLine|TestTaskSendCancel|TestRowSendCancelSource|TestTaskSendWithoutCancelSource' -count=1` → **ok**
  - `make test-budget PKGS='./internal/tools ./cmd/agent'` → 见 `## Evidence` ③（失败清单与归因）
- **计数措辞（2026-10-08 修正，行 `01a1176f0d3c764e8599628bb853770d`）**：凡写
  `--- PASS N 条`，必须说明是**顶层**还是**含子测试** —— 一个只写"N 条"的句子会被读成
  "跑到的测试总数"。实测（`devdoc` 姊妹报告里那条 `-run 'RowSend|RowCancel|…'` 命令，
  在 `06b28d7b6` 抽取树与本次 HEAD 上各跑一次）：**31 顶层 PASS + 16 子测试 PASS，0 FAIL**
  （`ok github.com/nick/gopi/internal/tools 1.562s` / `4.401s`）；
  而复核给出的 `22 顶层 + 7 子测试` 这个读法**本行未能复现** —— 记为**未复现**，
  **不记为"31 是假的"**（顶层 31 本身两次逐条相同）。同一命令在 fix 的父提交
  `419ebf54fe4c896e1ae1d748baefa618dbe72da0`（= `06b28d7b6^`）上是 **4 FAIL / 1 PASS**（见上）。
- Ran: **隔离沙箱**（Rule 34，**未碰** `/home/user/.gopi/memory.db`）
  - 发送端 `SB=/home/user/workspace/gopi-sandbox/sandbox-depgate-send-a8d3b5e3`
    （`GOPI_HOME=$SB`、`GOPI_SETTINGS=$SB/settings-send.json`、`$SB/bin/gopi` = **本次 diff 构建**，
    sha256 `ca812d48b0c1…`）
  - 对端 = **peerbox 自己**（`--remote-home` 指向
    `/home/user/workspace/gopi-sandbox/sandbox-depgate-peer-1022f9b0`，该沙箱 `bin/gopi` 是同一份二进制），
    读对端账本用对端自己的二进制 + 对端自己的 `GOPI_HOME`
  - 沙箱根 `/home/user/workspace`（dev `2052`）≠ `/`（dev `2051`，Rule 22）

  **① 被拒那次（逐字）** —— E=`…0e`（父，pending）、F=`…0f`（子，parent=E，pending）：

  ```
  $ gopi task send --task-id 01adepgate0000000000000000000e --peer peerbox \
        --remote-home <peer sandbox> --cancel-source
  EXIT=1
  [stderr] gopi task sync: row send: task 01adepgate0000000000000000000e may not leave
  this ledger yet: row 01adepgate0000000000000000000f (pending) still depends on it HERE —
  moving it away would leave that child pointing at a parent this ledger no longer holds,
  which Rule 33 parks permanently (releases only on completed|superseded|verified);
  finish or cancel the child first
  ```

  同伴断言：
  ```
  $ gopi task show 01adepgate0000000000000000000e   → status: pending   ← 源行未被 cancel
  $ GOPI_HOME=<peer> <peer>/bin/gopi task show …0e  → (absent)          ← 对端账本没有 E
  ```

  **② 放行那次（逐字）** —— F 走本账本的合法迁移 `pending → in_progress → completed`，
  然后**同一条命令、同样参数**：

  ```
  $ gopi task send --task-id 01adepgate0000000000000000000e --peer peerbox \
        --remote-home <peer sandbox> --cancel-source
  send peerbox: stored (row 01adepgate0000000000000000000e, status=pending)
  cancel-source: 01adepgate0000000000000000000e cancelled here (peer peerbox holds it: stored)
  EXIT=0
  ```
  ```
  $ gopi task show 01adepgate0000000000000000000e                  → status: cancelled
  $ GOPI_HOME=<peer> <peer>/bin/gopi task show …0e
  task_id: 01adepgate0000000000000000000e
  status:  pending                                                 ← 对端自己的账本
  parent_task_id: —
  ```

  **③ 边界对照（COPY 仍放行，MOVE 仍被拒）** —— C=`…0c`（父）、D=`…0d`（子，parent=C）：
  ```
  $ gopi task send --task-id 01adepgate0000000000000000000c --peer peerbox --remote-home <peer>
  send peerbox: already current (row 01adepgate0000000000000000000c, status=pending)   EXIT=0
  $ gopi task send --task-id 01adepgate0000000000000000000d --peer peerbox --remote-home <peer>
  send peerbox: stored (row 01adepgate0000000000000000000d, status=pending)            EXIT=0
  $ gopi task send --task-id 01adepgate0000000000000000000c --peer peerbox --remote-home <peer> --cancel-source
  EXIT=1
  [stderr] … row 01adepgate0000000000000000000d (pending) still depends on it HERE …
  ```
- Result: 被拒那次 **exit 1**、stderr 含挡路子行 id `…0f`、本地 E 仍 `pending`、对端**无**该行；
  放行那次 **exit 0**、`stored`、本地 E `cancelled`、对端账本打出该行 `status: pending`。
  COPY 的父子两行都成功过账（边随行），同一个父的 MOVE 立刻被拒 —— 与 epic 的目标逐条一致。
- Commit: 见 report (`$GOPI_TASK_REPORT_PATH`)

## Evidence (self-filed)

**⓪b `make test-budget PKGS='./internal/tools ./cmd/agent'` —— 本行与基线（detached `9ea9d75b8`）逐条对齐**

| | 本行（含 delivery-point sync `6433ea117`） | 基线 `9ea9d75b8` |
|---|---|---|
| 失败**包** | `internal/tools` | `internal/tools` |
| 失败**测试** | 同 3 条（下表） | 同 3 条 |
| `cmd/agent` | 绿（0 失败） | 绿（0 失败） |
| 流文件 | `run-1791389282-1036047.json`（852s wall） | `run-1791387760-829906.json`（1392s wall） |

失败集完全相同 → **red-set delta = 0**，这 3 条是 master 自己的，不是本行的：

```
--- FAIL: TestTaskRunGroundCutsTheBaseFromThePeerTip
--- FAIL: TestTaskRunGroundDegradesExplicitlyWhenThePeerIsUnreachable
--- FAIL: TestTaskRunGroundSkipsThePeerFetchOnTheServerRole
```
逐字原因（两侧同文）：
```
TaskRunGroundFor: cannot resolve the ground for task t-ground-base-peer in repo
/dev/shm/suite-tools-run-437/…/client: the task id is not a full uuid, so no ground name
can be derived from it (a ground directory is gopi- + the row's uuid with its dashes
removed, 32 hex characters …); resubmit the row under a uuid id, or name the ground
explicitly with gopi task update <task_id> --repo <path-to-live-work-tree>
```
归因（读代码 + blame + 祖先关系，非推断）：
- `internal/tools/task_run_ground.go:179` 起要求 uuid 形状的 id 才能派生 ground 名
  （提交 `19caf9de9` "feat(ground): the worktree/branch name must be derived from the row's full uuid"，
  **在 master 里**），并且**同一棵树上已有**它的正向测试
  `internal/tools/task_ground_name_uuid_test.go`（"is not a full uuid gets a REFUSAL"）。
- `internal/tools/task_ground_baseline_test.go` 的夹具仍用 slug id：
  `git blame -L 161,161` → 提交 `460dbd958` "refactor+feat(git): one peer fetch, and the ground starts
  at the peer's tip"，而且 **`git merge-base --is-ancestor 460dbd958 9ea9d75b8` 为真** ——
  这行在我这条分支的**基点之前**就已经存在，是 master 自己的测试漂移（peerbox 上同一条红可见）。
- **本行不碰** `task_run_ground*` 的任何文件：`git diff master...HEAD --name-only` 只有 6 个文件
  （见 `## Files`），全程 `git status --porcelain` 干净。
- **已按 Rule 26 立案**：`01a1171f-50d2-7940-933d-878bb44f3ce9`（tags=bug，parent=短 id 那一行），
  payload 里带着逐字红、blame/祖先取证、约束与验收；修法是把那三条 fixture 的 slug id 换成 uuid，
  **不放宽断言**。

**⓪ 干净合并（Acceptance 第 1 条）** —— 在 `master` 当时的 tip `20715a69f` 的 detached
worktree 里试合并：
```
$ git worktree add --detach /home/user/workspace/gopi-mergecheck-01a11648 20715a69f
$ cd /home/user/workspace/gopi-mergecheck-01a11648
$ git merge --no-commit --no-ff task/01a11648e4eb758f8b6413a3fcc79a99
Automatic merge went well; stopped before committing as requested
MERGE_EXIT=0
$ git status --porcelain | grep -E '^(UU|AA|DU|UD|AU|UA|DD)'   → (none)
$ git diff --cached --name-only
cmd/agent/cli_tasks_send_dependency_test.go
cmd/agent/cli_tasks_sync.go
devdoc/DOC-INDEX.tsv
devdoc/task-send-dependency-gate.md
internal/tools/memory_sync_row.go
internal/tools/task_transfer_eligibility_test.go
$ go build ./...                                              → exit 0（合并后的树上）
$ go test ./internal/tools/ -run 'TestTaskTransferEligible|TestRowSendRefusesARowWithALiveDependent|TestRowSendCopies|TestRowSendRefusesAChildWhoseParentIsUnreleased|TestTransferEligibilityFilterArms' -count=1
ok  	github.com/nick/gopi/internal/tools	1.969s
```
（`git grep -c '<<<<<<<'` 在合并树里有 3 个命中，全在 `briefings/`、`cmd/agent/merge_prompt.md`、
`devdoc/abnormal-child-retry-merge.md` —— **是 master 自己的文档在讲冲突标记这件事**，
`git status --porcelain -- <那三个文件>` 为空 = 本次合并没碰它们。）

**① 既存 master 测试（判据 + 边）在本行全绿**：
```
$ go test ./internal/tools/ -run 'TestRowSend|TestParentEdge|TestTaskTransfer|TestRowCancelSource|TestRowSendCancelSource|TestRowSendWithoutCancelSource' -count=1 -v | grep -E '^--- (PASS|FAIL)'
--- PASS: TestRowSendCarriesTheDependencyEdge (0.08s)
--- PASS: TestParentEdgeRefusedWhenThePeerLacksTheParent (0.08s)
--- PASS: TestParentEdgeRefusedOnATerminalParent (0.08s)
--- PASS: TestParentEdgeLandsWhateverThePatchOrder (0.05s)
--- PASS: TestParentEdgeSurvivesTheHarvestLane (0.05s)
--- PASS: TestRowSendRefusesEveryNonPendingStatus (0.11s)
--- PASS: TestRowSendCancelSourceCancelsOnlyOnAPersistedReceipt (0.22s)
--- PASS: TestRowCancelSourceReportsTheLocalFailureWindow (0.06s)
--- PASS: TestRowCancelSourceRereadsTheSourceBeforeWriting (0.06s)
--- PASS: TestRowSendWithoutCancelSourceLeavesTheSourceAlone (0.11s)
--- PASS: TestRowSendGateUsesTheLedgerConstants (0.00s)
--- PASS: TestRowSendCancelSourceHonoursTheNamedLedger (0.10s)
--- PASS: TestRowSendStoresTheRowOnThePeer (0.11s)
--- PASS: TestRowSendCancelSourceStampsTheHandoffMarkerOnItsOwnLedger (0.07s)
ok  	github.com/nick/gopi/internal/tools	2.333s
```

**② 沙箱归属与「没碰操作者账本」**：`.sandbox-owner.json` 两处都记 task id
`01a11648e4eb758f8b6413a3fcc79a99`；操作者账本 `/home/user/.gopi/memory.db` 对探针行的
`task show` 输出**空**（无该行），探针行只在两个沙箱 + 对端沙箱里。
对端账本 mtime 在**被拒**前后未变（`2026-10-07_19:44:30`），证明「被拒 = 对端一字未写」。

**③ 本次 `--cancel-source` 的真实调用与输出**：见 `## Verification` 的 `Ran:`（逐字复制自终端）。
构建产物与两个沙箱里的二进制 sha256 一致：`ca812d48b0c17df71ebfca975995aa85111750fbeab2af1bce12c2651c04e3ff`
（`bin/gopi`、`$SB_S/bin/gopi`、`$SB_P/bin/gopi` 三处同值）。

**③b `internal/stylegate` 的红是 master 自己的，两侧同数**（本行的文件一个都不在其中）：
| 规则 | 本行 | 基线 worktree（detached `9ea9d75b8`） |
|---|---|---|
| `ignored-error` | 8 production | 8 |
| `unchecked-close` | 3 production | 3 |
| `test-sleep` | 4 test | 4 |
| `test-setenv-scope` | 3 test | 3 |
```
$ grep -cE "task_transfer_eligibility_test.go|cli_tasks_send_dependency_test.go|memory_sync_row.go|cli_tasks_sync.go" <stylegate 输出>  → 0
```
点名站点全是 master 的：`cmd/agent/chat_prefetch.go`、`internal/replayplay/{loop,run}.go`、
`internal/tools/task_transfer.go`、`internal/tools/tasks.go`、`cmd/agent/chat_prefetch_turn_test.go`、
`cmd/agent/cli_session_replay_test.go`、`internal/lineedit/probe_pty_test.go`、
`internal/tools/task_ground_in_use_test.go`、`internal/tools/chat_ops_authorization_test.go`。

**④ 已知缺口（Rule 26：立案，不在本行顺手改）**：`syncRowTransferGate` 走
`taskTransferRowByID`（精确 id 匹配），短 id 的解析（`taskIDResolve`）没有进来，与
`SyncRowCancelSource` 的写路径不一致；`taskRowClaimable` 的 skip-set 参数本行传 `nil`
（发送端没有「接收端 dispatcher 的排除集」，这是 EXPORT 的定价面）。两处都在报告里点名，
以便后续行独立修复。

> **这一条的后续（2026-10-08，行 `01a117082d5473c7a8ed247ec357795d`），两半分开结账：**
> - **前半（解析面不一致）—— 已闭合**：`syncRowTransferGate` 不再自己走
>   `taskTransferRowByID`（精确匹配），而是先用**腿上唯一那一步**
>   `syncRowResolveTaskID` 把操作数变成行 id（引用项目已有的 `internal/fullid`；
>   不新增解析器、不引入前缀解析），再用**账本自己的拼写** `row.TaskID` 问判据。
>   `SyncRowSendEx` 的 leg 头部也做同一步（COPY 与 MOVE 同一条地址给出同一答案，
>   不再取决于操作者有没有敲 `--cancel-source`）。同族的姊妹门 `--next-eligible <ID>` /
>   `--handover <ID>` 也改走 `taskTransferRowFullID`（引用同一步，非第二份实现）。
>   `taskTransferRowByID` **保留**（候选投影的精确单行读，由「本账本没有此行」路径使用），
>   契约在注释里写明。回归：`internal/tools/memory_sync_row_id_resolve_test.go`（5 条，红→绿）。
> - **后半（`taskRowClaimable` 的 skip-set 传 `nil`）—— 仍未做，仍在本行之外**：
>   那是 EXPORT 的定价面（发送端没有「接收端 dispatcher 的排除集」），本次未动，
>   需要时另立一行。
>
> ### 上面这条"已闭合"的**修正与限定**（2026-10-08，行 `01a1176f0d3c764e8599628bb853770d`）
>
> 上一条写于 `06b28d7b6`，它说的"同族的姊妹门 `--next-eligible <ID>` / `--handover <ID>`
> 也改走 `taskTransferRowFullID`"**只描述了其中一个半边**，本段按实测补齐三处：
>
> **(a) 姊妹门是两扇门，修的时间不同。** 实测：`--handover <ID>` 在 `06b28d7b6` 上**真的可达**
> （`{"signed":true}` 对应真行，`{"signed":false,"observed":"absent from this ledger"}` 对应垃圾输入 ——
> 两个操作数答案**可分辨**＝值被读了）。`--next-eligible <ID>` 在 `06b28d7b6` 上**仍然坏**：
> 当时的 target guard 只认 `transferNextBare`，dispatcher 分支认 `transferNextBare || transferHandover != ""`，
> 于是**带 `--peer` 时那个值被静默丢弃**（真 id 与垃圾输入**输出逐字相同**、都 exit 0），
> **不带 `--peer`** 时按 usage 拒绝（exit 1）。**它的修复是另一笔提交 `388a63c31`**
> （`syncTransferPeerFaceGiven`，判据改成"认那个**值**，不认拼写"），不在 `06b28d7b6` 里
> （`git merge-base --is-ancestor 388a63c31 06b28d7b6` → 非 0）。
> 取值：**这两扇门不是同一次修复的两个名字，是两次修复** —— 引用本段的人不要把
> `--next-eligible` 归给 `06b28d7b6`。
>
> **(b) 这条腿在 `06b28d7b6` 上"没取到真跑证据"的真实原因不是 anchor guard。**
> 报告里曾把这归因给 `anchor guard`。实测：那一臂执行时**更早**的
> `sync_peers` 白名单判据（`peer "peerbox" is not in the sync_peers whitelist … sync topology is declared,
> never discovered`）已经挡住它，**根本没走到 anchor guard**。判据次序错了，归因就错了 ——
> 而这条 ④ 的结论（未取证）恰好是对的，只有**理由**是错的。
>
> **(c) 红树必须点名 commit。** 引用本段的红→绿配方时，红树要指 **fix 的父提交**
> `419ebf54fe4c896e1ae1d748baefa618dbe72da0`（= `06b28d7b6^`），**不能写 `master`**：
> fix 进 master 当天 master 就绿了，配方当轮失效（Rule 38；本行实测同一命令在父提交上
> **4 FAIL**、在 `06b28d7b6`/HEAD 上 5 PASS，`ok … 0.546s`）。红集是 **4** 条：
> `TestRowSendRefusesATruncatedIDAndNamesTheFullOne`、`TestRowSendRefusesAnAmbiguousPrefixByName`、
> `TestRowSendAsksTheMoveCriterionForATruncatedID`、
> `TestRowTransferGateRefusesATruncatedIDWithTheResolverSentence`——最后一条常被漏掉。
>
> **(d) 操作者账本那 4 行 fixture 不是同一种 reason。** 只读回读（`mode=ro`）得到两种：
> `01a117c1-…` / `01a117d2-…` 带的是 `gopi task send --cancel-source` 自己写的**移交说明**
> （它们**真的被 MOVE 走了**，`receipt: stored`）；只有 `01a117e3-…1111` / `01a117e3-…2222`
>（歧义臂的两条 fixture）带"探针 fixture 误入"说明。读"全部如何"要读**内容**，不是数行数。

## Files

- `internal/tools/memory_sync_row.go`（改：`syncRowTransferGateFormat` + `SyncRowSendOptions.CancelSource`
  + `syncRowTransferGate` + `SyncRowSendEx` 调用；判据引用 **master 的** `TaskTransferEligible`）
- `cmd/agent/cli_tasks_sync.go`（改：`opts.cancelSource` → `SyncRowSendEx`）
- `internal/tools/task_transfer_eligibility_test.go`（新：判据表格 + 两个端到端臂 + 正向臂）
- `cmd/agent/cli_tasks_send_dependency_test.go`（新：命令线验收）
- `devdoc/task-send-dependency-gate.md`（本报告）
