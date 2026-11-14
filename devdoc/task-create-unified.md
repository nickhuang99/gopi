<!-- doc-class: historical -->
# task-create-unified — 统一创建代码路径（单一 INSERT 全列对齐）

> Epic 2026-09-05（nick 定稿 + doubao 备忘）。分支 `feat/task-create-unified`。

## 目标

所有 task 创建（auto 登记 / 裸 prompt / submit / resolve-conflict）走单一 INSERT，
schema 全列对齐——"all task creation process must using same code path for easy
maintainment"。字段差异用默认值表达（零值 = schema 默认列），不存在"某条路径
少插一列、事后要另写 UPDATE 补齐"的双维护。

## 统一路径设计

```
                        ┌──────────────────────────────┐
                        │  taskCreateFields (全列表面)  │
                        │  13 个 operator-visible 列     │
                        └──────────────┬───────────────┘
                                       │
                       ┌───────────────▼────────────────┐
                       │ insertTaskRow(ctx, db, f)      │
                       │  ONE INSERT, 14 列 (含 reason)  │
                       │  task_id 生成/唯一性 / status    │
                       │  /source 默认 /tags 规范化      │
                       │  priority 零值→taskTagPriority  │
                       └───────────────┬────────────────┘
        ┌──────────────┬───────────────┼─────────────────┐
        ▼              ▼               ▼                 ▼
  TaskNewOID      TaskSubmit     TaskVerifyToReopen   (未来 resolve 登记)
  (auto 登记,     (队列 pending,   (reissue pending,   走同一面——
   in_progress)    完整 opts)        parent+payload)    零新路径)
```

关键点：

1. **扩展入口 TaskNewOID(opts)** — `TaskNewReturningID(summary,taskID,
   sessionID,model)` 4-string 签名保留为兼容薄封装。想带 payload/tags/source/
   deliverable/reason/priority/parent_task_id 的 caller（裸 prompt auto 登记、
   未来的 resolve-conflict 登记）直接用 `TaskNewOptions` 同 INSERT 落地——不再
   需要"先 7 列 INSERT 再 UPDATE 补列"。
2. **priority 零值推导** — helper 内 `taskPriorityForZero` 走与 ensureSchema
   NULL backfill 同一条 taskTagPriority（bug→1 / docs→8 / 默认 5），杜绝
   "priority=0 悬空排最前" 与 backfill 双源漂移。
3. **错误前缀逐路径保留** — `task:` / `task_submit: insert:` / `task reopen:
   insert:` 原样，错误信息形状不变（测试钉住 does-not-exist / already-exists）。
4. **行为零变化约束** — TaskNew 传 status=in_progress + source 默认 auto；
   TaskSubmit 传 status=pending + 完整 opts；reopen 传 status=pending +
   parent/tags/payload。改动只收敛 INSERT 代码，不改任何行的语义。

## 与 doubao 备忘的实现映射

doubao 备忘 `~/work/claude/nick/merger_resolve_conflict_note.txt` 提出的
resolve-conflict 登记模型（merger spawn child 前 DB 写 in_progress 行 +
waitpid + crash 恢复扫描 + worktree 二检 → aborted）**已由现行已提交架构取代**
（见下）。备忘内容逐条映射如下——供 PA 决策是否在新协调设计下复活 row-per-resolve：

| doubao 备忘条目 | 现行架构（0a3449b, 2026-09-05, merger-single-owner-architecture） | 裁决 |
|---|---|---|
| Auto-spawn Task DB 登记 in_progress（持久化/审计/追踪） | resolve 生命周期由 merger 单点拥有；审计 = resolve child 的 session JSONL；崩溃隔离 = 进程组 kill（child 死 = failed resolve → 分支原样 → 下 tick 重试） | **不再需要 DB 行** — 设计要求明文 "task queue gets zero new merge-conflict rows" |
| pwait/waitpid 监控子进程 | mergerResolveConflict 阻塞等待 child.Wait()，超时 kill 进程组 | 已实现（单 repo tick 内阻塞，不卡全局轮询主循环） |
| 崩溃重启恢复（扫 in_progress、pid 死→failed） | 无需——无 DB 行；重启后重新扫分支遇仍冲突者重试（git 状态无损） | 不再需要 |
| 子进程失败 → Task failed + 留存现场 | child 失败 → merger 返回 error → 分支 untouched + session JSONL 现场 | 已满足（载体不同） |
| worktree 二检（resolve 期间重新 checkout → aborted 不 merge） | 每次 tick 重扫：worktree 保护仍在 ① 步拦截（分支带 worktree = 保护跳过） | 已满足（无 aborted 状态 — taskStatusSet 无 'aborted'，需 cancelled） |
| 成功 → merge + 删分支 + Task success | child 成功推进分支 tip → taskMergeOne 重合并落地 + 删分支 | 已实现（merger 单点所有权——child 不做 merge） |

**结论**：E2-E4 的意图（可追踪、崩溃恢复、worktree 保护优先）全部由
self-resolve + 进程组 kill + git-arbiter 机制以更简单的形态满足。row-per-resolve
是备忘时代的中间产物，与 "merger never submits" 终局架构互斥。若未来要恢复
DB 行追踪，唯一不破坏架构的落点：resolve 行作为 merger 自己的审计载体
（source=auto、tags=merge-conflict,auto-spawn、status=completed/failed/cancelled），
且 merger 写、merger 终——child 仍不碰队列。统一 helper 已为此预留
`insertTaskRow`（本 epic item 4 若复活直接调用，无需新 INSERT）。

## 不做的事（诚实清单）

- resolve-conflict 登记（item 4/E2-E4 的 DB 行部分）：见上，与已提交架构冲突。
- auto 行 payload 填充（item 3 的 payload 半侧）：renderer 注释 + 测试钉住
  auto 行 briefing 从 session JSONL 取（payload 镜像 = 双份存储每条 prompt）。
  schema 对齐已结构性完成（同 INSERT 同列），payload 空是语义选择非缺列。
- sync 层 INSERT 并入 helper：远端复制路径（verbatim remote row），非本 epic 范围。

## 验证

- `go build ./...` OK
- `go test ./internal/tools/ -run 'TestInsertTaskRowWritesFullColumnSurface|TestTaskNewOIDStampsOptionalFieldsInSameInsert|TestAllNewRowPathsConvergeOnInsertTaskRow'` 3/3 PASS
- `go test ./internal/agent/ -run 'TestAutoTask|TestChatTaskRegistration'` PASS（含 WrapperChildNoAutoRow）
- `go test ./internal/tools/ ./internal/agent/` 全量 PASS（tools 344.9s / agent cached）
- `go test ./cmd/...` 全量 PASS（agent 96.3s — 含 merge_resolve_e2e、cli_tasks）
- Commit: 见 git log（feat/task-create-unified）
