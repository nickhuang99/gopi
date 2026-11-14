<!-- doc-class: normative -->
# 终态冲突裁决：completed 赢（db-sync 双节点终态冲突）

Epic 2026-08-31 · commit 5c68437 (master)

## 问题

db-sync 模式下同一任务可能在两节点各自到达终态：

- **HP 本地跑完** → `completed`（有工作成果）
- **PA 在 peerbox `task cancel` 掉它** → `cancelled`（意图放弃）

memsync 前向迁移规则（M4 五机制）未定义终态间冲突：

- `importTaskExisting` 里 forwardable patch（completed/failed/verified）遇
  local 非 pending 行 → 直接报错 refused（"live execution outranks…"）
- cancelled patch 行被 `syncTaskForwardable` 挡掉 → silent already，无任何
  意图留痕

即：双侧终态后 sync 永不收敛，且 cancel 意图随行丢失。

## 裁决原则

**completed 赢**（工作成果不可丢——nick 的 DO NOT drop 数据约束推论）；
**cancelled 让位但留痕**（意图不丢失，落到行 tags：`cancel-was-intent`）。

## 冲突矩阵（实现于 importTaskExisting + terminalConflictRuling）

| patch（远端） | local（本地） | 裁决 | 报告计数 | tags |
|---|---|---|---|---|
| completed/failed/verified | cancelled | **迁移** cancelled→patch status | migrated | +cancel-was-intent |
| cancelled | completed/failed/verified | 行不动（终态不回退） | already | +cancel-was-intent |
| cancelled | pending | 行不动（本地调度器拥有 cancel） | already | +cancel-was-intent |
| cancelled | cancelled | 幂等（tags 相等时） | already | — |
| completed/… | claimed/in_progress | 拒绝（本地执行不被远端终态覆盖） | refused | — |
| completed/… | pending | 迁移（原有 sync-back） | migrated | — |
| completed/… | 同状态 | 幂等 | already | — |

## 实现落点

- `internal/tools/memory_sync_task.go`
  - `importTaskExisting`：非 forwardable 分支改走 `terminalConflictRuling`；
    非 pending 分支加 cancelled 特例（迁移 + 打 tag）
  - `terminalConflictRuling`：cancelled patch 的裁决矩阵，tag 落在任意
    非 pending 行 + pending 行（provenance 便宜且不会错）
  - `syncTaskMigrate` 拆出 `syncTaskMigrateFrom(taskID, fromStatus,
    status)`：同一 CAS `WHERE status=<from>` 纪律，completed 赢迁移用
    `fromStatus=cancelled`
  - `syncTaskCancelIntentTag = "cancel-was-intent"`
- 设计点：迁移仍走 CAS（不绕过状态机守卫），只是源状态从 pending 扩到
  cancelled——与 M4 的 sync-back 同构（远端合法走完=分布式真相补齐）。

## 验证

- 单测 8 用例全绿：`go test ./internal/tools/ -run TestTaskSync`
  - `TestTaskSyncCompletedBeatsCancelled`（裁决正向）
  - `TestTaskSyncCancelledLosesToTerminalWork`（裁决反向）
  - `TestTaskSyncCancelConflictMatrix` 6 子测（failed/verified patch、
    双 cancel 幂等、pending+cancelled-patch 只打 tag、live 行拒绝、
    同状态幂等）
- E2E 双节点同机模拟（双 GOPI_HOME + GOPI_SYNC_NODE）：
  1. A 提交 pending → export live 行
  2. B import（queued=1，同 task_id）
  3. 分叉：A walk 到 completed；B `task update --status cancelled`
  4. A export completed sync-back → B import：
     **`queued=0 duped=0 already=0 migrated=1 tagged=0 refused=0`**
  5. B 行终态：`status: completed`，`tags: from-peerbox,e2e-ruling,cancel-was-intent`
  6. 重跑同 patch：`already=1`（幂等收敛）

## 意图审计入口

`gopi task show <task_id>` 的 tags 行带 `cancel-was-intent` 即"有人曾想
取消它但 completed 赢了"——审计时按 tag 查即可，无单独表。
