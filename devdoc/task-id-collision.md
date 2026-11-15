<!-- doc-class: normative -->
# task-id 碰撞修复：同秒跨节点 id 撞车 → 静默丢 bug 单 (t-1788077827)

## 碰撞机理

- id 生成位点：`TaskNew` / `TaskSubmit` (internal/tools/tasks.go)，auto id = `t-<unix 秒>`，
  仅在**本地**库内查重（taskIDExists 循环 + `-1` 后缀）。
- sync 通道保留原 id（mechanic ② importTaskFresh / harvestInsert）。
- HP 本地 LLM 自投单与 peer1 投单同步过来：两节点**同秒**生成 → `t-<同秒>`。
- import 落到已存在的 task_id：
  - live patch row → `importTaskExisting` 走 tags-union（RowsTagged / RowsAlready），**不比对 payload**；
  - payload hash 门（mechanic ①）只挡"全库 live 行同 work"——本地行是 **pending** 且同 summary+payload 时
    会先被 hash 门 duped；**summary/payload 不同**的行落到 tags-union 分支，
    HP 的新 bug 单被记成 `RowsTagged`（"已存在"），**执行内容静默丢失**。
- harvest 通道同病：同 id + remote 行落在 harvestReconcile，按 status 阶梯合并，
  同 summary 但 payload 不同的两行可能 Conflict/duped，同样无"改号重排"出路。

## 方案权衡（Epic 给出的两条路）

| 方案 | 做法 | 优点 | 缺点 |
|---|---|---|---|
| ① 节点号嵌入 id | 生成本地 id 时带节点号 `t-hp-<sec>` | 根治：两节点 id 空间天然不相交；零比对逻辑 | 历史存量 t-* 仍可撞；改 id 形态，CLI/人工引用习惯变；跨节点 traceability 依赖前缀约定 |
| ② import 撞 id 比对 payload hash | 撞 id 时 sha256(summary+NUL+payload) 与本地行不同 → **强制改号重排**（`t-<sec>-n<node>`），显式报告 | 兜住所有来源（存量 id、手工 id）；不改正常 id 形态 | 只修 sync 入口，本地双进程投单仍靠 taskIDExists 本地查重（本来就有） |
| **采纳：② 为主 + ① 的思想做重排后缀** | | | |

裁决理由：
- 方案 ① 改的是"生成端"，但**碰撞发生在"接收端"**——已生成的存量 id、跨节点 patch 里带来的
  旧 id 都能在接收端撞车，生成端前缀治不了存量。接收端比对是唯一全覆盖位点。
- 重排后缀带节点号（`-n<node>`），与 ① 的 provenance 思想一致：改号后 id 仍可溯源到来源节点。
- hash 门（mechanic ①）已有 syncTaskWorkHash 基建，② 是在撞 id 分支**复用**它，最小改动。

## 实现（internal/tools/memory_sync_task.go）

1. `SyncTaskImport` 撞 id 分支（existing != nil 且 live patch row）：
   - 现逻辑：tags-union / already。
   - 新逻辑：先比对 `syncTaskWorkHash(r.Summary, r.Payload)` vs 本地行
     `syncTaskWorkHash(existing.Summary, existing.Payload)`：
     - **相同** → 原语义不变（tags-union / already，幂等重导入）。
     - **不同** → 改号重排：新 id = `r.TaskID + "-n" + nodeSuffix(node)`（node 非法字符清洗，
       `-` 内嵌安全），再走 taskIDExists 查重循环（撞了再 `-1`），`importTaskFresh` 写入，
       stats.RowsRelabeled++ 且 Errors 里带显式报告行（`relabeled <old> → <new> ...`）。
   - terminal patch row 撞 id 且 payload 不同：仍走原 refused（不重排——sync-back 的
     状态迁移语义里，"同 id 不同 payload"意味着本地行有自己的生命周期，重排会
     制造一份幽灵 completed，宁可显式 refused）。
2. `harvestApplyTx` 同路径：probe 到本地行、remote 行非 terminal、payload hash 不同、
   本地行 pending → 改号 insert（复用同一 helper），counts.Relabeled++。
   其余（terminal/local live/同 hash）走原阶梯。
3. `SyncTaskImportStats` / `HarvestCounts` 加 `Relabeled int`（JSON: relabeled）。
4. id 生成端（TaskNew/TaskSubmit auto 分支）**不动**：本地查重已保证单节点唯一；
   跨节点唯一性由接收端重排保证——这是本 devdoc 的核心裁决。

### helper

```go
// taskIDRelabel dedup-proofs a colliding import id: <id>-n<node>, then -1..-9.
// node 非法字符清洗（非 [A-Za-z0-9] → '_'，空 → "node"），返回 (newID, changed).
func taskIDRelabel(collidingID, node string, exists func(string) (bool, error)) (string, bool, error)
```

## 测试（internal/tools/memory_sync_task_test.go 追加）

- `TestTaskSyncImportIdCollisionRelabelsDistinctWork`：
  本地 pending `t-9001` (summary A/payload B)，patch 带 live row `t-9001` (summary A'/payload B')、
  同 summary 同 payload 的 row `t-9002`、terminal 撞 id row `t-9003`：
  - `t-9001` 行 → Relabeled=1，新 id 形如 `t-9001-n<node>`，原行不动，
    Errors/输出含显式 relabel 报告；
  - 同 hash 撞 id → 原语义（tagged/already，不重排）；
  - terminal 撞 id → refused（不重排）。
  - 幂等：同一 patch 二次 import → 新 id 行 hash 命中 duped，**不再**二次重排。
- `TestHarvestCollisionRelabelsDistinctWork`（Fetch 注入）：同 id 不同 payload，
  counts.Relabeled=1，双行并存。

## 验收

`go build ./... && go test ./internal/tools -run 'TestTaskSync|TestHarvest' -count=1` 全绿；
全量 `go test ./...` 无回归。
