<!-- doc-class: historical -->
# task 多节点同步的方向收缩：payload-hash 去重判为失败尝试

**裁决**：payload-hash 内容去重**是失败的尝试，移除**；多节点协同**暂缓**（时机未到，非能力否决）；
方向 = **UUID 唯一键 + 命令行推送**（重复任务合法；同步层不做内容去重、不做冲突检测）。
`01a0fea1` 的 285 行长文已按实测收敛到本 ≤80 行版（见 §三④）；`01a0fe75` 的提问见 §四。**只出决策与方案，不改代码。**

## 一、四张清单

### 放弃（失败尝试）
| 项 | 失败证据 |
|---|---|
| `syncTaskWorkHash`=sha256(summary+NUL+payload) 作**身份判据** | 相似任务难区分 ⇒ 唯一可改源 payload 被迫冻结 ⇒ 只能取消/重提 |
| `syncTaskCanonHash`（剥路径/时间戳）+trigram twin 闸 | `:487/491`：仍把两份相似工作判成一份 |
| **live twin 命中即 `continue`（丢行）** | `:689,698-703`（`RowsDuped++; continue`）静默丢行；`syncTaskLiveDedupHitTx` `:920/955-961` 同判 |
| `harvestSameContent` 的 `r.Payload != local.payload` 项 | `:2212`：拿 payload 判「是不是同一份工作」 |
### 保留（必须留）
| 项 | 位置 | 理由 |
|---|---|---|
| `SyncTaskExportDelta`/`SyncTaskImportDelta` | `memory_sync_delta.go:187/:431` | 三通道唯一合并权威；单行推送也走它（`memory_sync_row.go:337,401`） |
| claim-lock / session / reconcile | `claim.go:407,996,1013`、`session.go:94,144,217,251`、`reconcile.go:254` | 红线①，本次一行不动 |
| **task_id 分支** | `memory_sync_task.go:674-680` 逐字 "identity is the task_id" | 正是目标方向，已在树里 |
| **内容分歧=WARNING（不再打 conflict tag）** | `memory_sync_delta.go:617-631`（`d3f8120d`） | 半个方向**已落地**，是 P2 的既有基准 |
| `sameWorkIdentity`（只按 summary）+ `gopi task dedup-scan` | `:2197`；`cli_memsync.go:394`（"moves nothing"） | 前者是收窄判据的仓内先例；后者是 hash 唯一合法去处（只报告不移动） |
### 移除（终态，按阶段）
| 项 | 位置 | 阶段 |
|---|---|---|
| 导入端 hash 闸的 `continue`（skip） | `:689`、`:698-703` | P1 |
| `importTaskExistingTx` 的 hash 身份判据 | `:1135`、`:1238` | P2 |
| `harvestSameContent` 的 payload 项 | `:2212` | P2 |
| 三闸函数 + `RowsDuped` | `:447/:491/:920/:430` + 6 个 `_test.go` | P3 |

> **不违反操作者口径**：id 碰撞路径 `:1149` 把冲突行 **relabel 成 `-r1` 子行**（"重复任务是合法的"）。**skip 才是缺陷**——hash 只该出警告，不该丢行。
### 绝不动（红线）
1. **不推倒 `SyncTask*` 一族**——claim-lock/session/reconcile 依赖它。
2. **单机模式（`sync_peers=[]`）零变化**：总闸 `config.HasSyncPeers()`（`internal/config/config.go:763`），无 peer 时导入路径不执行。
3. **hash 不作身份**——`task_id`（UUID）是唯一键；**不删 dispatch case**，退休带指针拒绝（`cli_memsync.go:125`）。
4. **`:605-608` 的 bug053/M4.16 护栏不降级**（payload 非空的真 ticket 不得被 flatten 成 completed）——与"内容分歧降级"是两件事。

## 二、分阶段方案（每阶段独立测试 / 独立回退 / 不破单机）

| 阶段 | 内容 | 独立测试命令 | 单机不破坏论证 | 回退 | 影响面 |
|---|---|---|---|---|---|
| **P1** | hash 命中**不再丢行**：`:698-703` 的 `continue` → 计数+warn，行走 id 分支 | `go test ./internal/tools/ -run 'Import\|Dedup\|Bidir\|Reflow'` | 导入路径仅 `HasSyncPeers()` 时进入，无 peer 零执行 | `git revert` 单 commit | 1 文件；`memory_sync_task_test.go`/`_task_dedup_test.go` 的 "duped⇒丢行" 断言 |
| **P2** | 判据收窄：`:2212` 去 payload 项（对齐 `:2197`）；`:1135/:1238` 换 task_id+状态判身份 | `go test ./internal/tools/ -run 'Import\|Harvest\|Migration\|Terminal'` | 同上 | `git revert` 单 commit | 1–2 文件；terminal 分支须保留**响亮拒绝**（Rule 15），只换判据不换拒绝 |
| **P3** | 删三闸 + `RowsDuped` + `dedup-scan` 的 gate 词汇 | `go test ./internal/tools/ -run 'Dedup\|Import'`；`go test ./cmd/agent/ -run DedupScan` | 无 peer 不受影响 | `git revert` 单 commit | 6 测试文件 + 1 CLI 文案；**须操作者确认**（动对外词汇） |
| **P4** | 多节点协同重启评估 | 不实施 | — | — | 暂缓，触发条件见 `01a0fea1` §3.6 |

**顺序硬约束**：P1→P2→P3，一阶段一 commit，禁跳阶（P2 依赖 P1 让行存活）。**执行者**：`01a0fe88`（持有"payload 解锁"验收）。

## 三、事实核实（三条，均带 file:line 实证）

**① payload 不可改的强制点**——**单机侧没有墙，同步侧才是墙**：
- 生产代码唯一 `SET payload = ?` 是**正当写入** `internal/tools/tasks.go:4097`（同事务写 `PayloadRevisionEventName` 审计行）。
- **`--payload` 已进 `task update`**：`cmd/agent/cli_tasks.go:473`（synopsis）、`:486-499`（语义），提交 `0a8ba8a8`。隔离 profile 实测：`gopi task update <id> --payload "REVISED briefing" --reason "…"` → `Task payload updated: <id> (payload=16 bytes, was 17)`；缺 `--reason` 被点名拒绝。
- **墙在**：`:1135` / `:1238` / `:2212` 用 `syncTaskWorkHash(summary,payload)` 判身份。

**② `SyncTaskImportDelta` 中的 hash 去重判定在哪？**（`memory_sync_delta.go:431` 是唯一入口，session/harvest/row 共用）
```
memory_sync_delta.go:431        func SyncTaskImportDelta(...)   ← 入口
memory_sync_delta.go:546-631    syncDeltaMergeRow               ← 旧行合并：hash 出现 0 次(awk 实测)
memory_sync_task.go:689/691     hash := syncTaskWorkHash(...) / seenCanon[...]  ← 新行闸①②
memory_sync_task.go:698,700-703 syncTaskLiveDedupHitTx → RowsDuped++; continue  ← 命中即丢行
memory_sync_task.go:920,955-961 同一闸对 live DB 行
```
⇒ **`SyncTaskImportDelta` 自身不做 hash 判定**：新行交 `insertTaskTx` 的 hash 闸，旧行交 `syncDeltaMergeRow`（纯状态/标签合并）。

**③ `task update` 无 `--payload`：设计还是缺口？** ——**是缺口，且已修复**（`0a8ba8a8`，在 master 上）。`cli_tasks.go:483` 现文案 "The SUMMARY is immutable — cancel and…"，payload 已是一等、带审计的修订通道。记忆 #7540 引的 `:1099` "task content is immutable" 是**过期快照**。⇒ **`01a0fe88` 验收标准 1「payload 可改」已满足。**

**④ 对 `01a0fea1` 长文的更正**：其 §2 四条事实基于 master `f3347151`，而 `d3f8120d`（内容冲突降级为警告）与 `0a8ba8a8`（`--payload`）**都不是它的祖先**⇒ 其"事实①payload 差异升级为 conflict tag""事实④没有 `--payload`"**在今天 master 上已不成立**，其 Phase 1/Phase 3 **已由他人落地**。引用以本文件为准。

## 四、回答姊妹任务 `01a0fe75`（按 task_id 判 / 按 hash 判 / 两者？）

**答：两者都存在。**
- **新行（INSERT）按 hash 判** ⇒ 违背"重复任务合法"，**P1/P2 移除**（命中转警告，行照常入队）。
- **既有行（MERGE）按 task_id 判** ⇒ **符合口径，保留**；内容分歧今日已降级为 warning（`memory_sync_delta.go:617-631`）。
- **终局**：hash 只活在 `gopi task dedup-scan`（只报告、不移动）。同步层**不做**内容去重、**不做**冲突检测。

**复核（可重跑）**：`grep -n "syncTaskWorkHash\|RowsDuped++; continue" internal/tools/memory_sync_task.go`；
`awk 'NR>=430&&NR<=700&&/hash/' internal/tools/memory_sync_delta.go`（期望空）；`grep -rn "SET payload" internal/tools/`（期望仅 tasks.go:4097）；`gopi task update -h | grep -- --payload`；`gopi doccheck`。
