<!-- doc-class: normative -->
# M4.20-B: 状态生命周期 Rank 序模型 — Implementation Report

Branch: `feat/worker-loop-m420` (worktree `~/workspace/gopi-m420`, base M4.20-A `27f7eec`)

## 序表（normative rank ladder）

```
pending(0) → claimed(1) → in_progress(2) → [completed|failed|cancelled](3) → verified(4)
legacy reopened = rank 0 (read-only history, 无迁移出/入)
```

## 裁决表（syncDeltaApplyNewer, M4.20-B）

| 情形 | 裁决 | 统计 |
|---|---|---|
| rank(remote) > rank(local) | APPLY（claim mirror / terminal converge / completed→verified 均自然落出） | mirrored（rank≤2）/ migrated（≥3） |
| rank 相等且 = 3（terminal-tier peers） | **后到覆盖**：updated_at 更晚者赢；等时戳 = 幂等 no-op | migrated / already |
| rank 相等且 < 3 | 保留本地（claim mirror 幂等） | already |
| rank(remote) < rank(local) | 拒绝 — 永不倒退 | already |
| 等锚 + 同 rank(<3) + 内容异 | **legacy conflict backstop**（client: conflict tag; server: ruling completed）— M4.18 后正常路径零冲突，触发即 bug 信号 | conflict / resolved |

## 原子更新（核心裁决件）

`UPDATE tasks SET status=? ... WHERE task_id=? AND (CASE rank(local)) < (CASE rank(remote)) OR (rank 相等 AND updated_at < remote_ts)`

- CASE 表达式在 UPDATE 内部重推行的**当前** rank — 守卫看到的是行现状而非读快照。
- 0 行命中 = 并发写已推进该行 → **重读 + 显式报告**（`RowsLostRace++`；行上更新的真相存活，绝不 clobber；bug035 CAS 同源）。
- SQLite 探针验证：等 rank 严格 `<` 守卫命中 0 行（拒绝），加 `OR (rank 等且 ts 更晚)` 后命中 1 行（后到覆盖）。

## 时钟退役

- `syncDeltaMergeRow` 排序从 `remote>local updated_at` 改为 **rank-first**；updated_at 仅存两职：① terminal-tier 平级 tiebreak；② pull 窗口（anchor/AfterSource 不动）。
- `completed→failed / failed→completed` 等旧 equal-anchor 冲突残余消失（rank 3 平级，后到覆盖）。

## reopened 退役（本任务在 sync 侧的部分）

- ladder 无 reopened 出/入；legacy 行 rank=0 只读兼容（open/list/sync 不崩，任何前进态可覆盖它）。
- reissue 语义由 M4.20-C 完成（本 commit 已带 TaskVerifyToReopen 的 doc 与 server ruling 行为）。
- `taskIngestMirrorableSourceStatuses` 去掉 reopened。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` OK；`go vet ./internal/tools/` clean
- 新测试 `internal/tools/memory_sync_rank_test.go`：
  - TestStatusRankTable（序表 9 行全覆盖 + terminal-tier 判定）
  - TestRankApplyMatrix（14 行裁决表：前进 8、平级覆盖/等时戳 no-op、倒退拒绝 4）
  - TestBilateralRankConvergence（双侧前进 + 旧回声不倒退 + 后到覆盖）
  - TestRankGuardAtomicity（并发写注入 → 0 行命中 → 重读 RowsLostRace，真相存活）
  - TestLegacyReopenedRowSyncs（legacy reopened 行收敛到 completed）
- 既有 sync 套件迁移：TwoNodeHandoff / IdempotentRerun / EqualAnchorEcho / ContentConflict / LegacyConflict×2 钉 CLIENT 或按 server-ruling 语义更新；LiveExecutionGuard 更新为 rank 语义（in_progress→completed = 前进收敛，起点原子性由 M4.18 保证）；TaskUpdateLifecycle / TaskVerifyLoopStateMachine / TaskVerifyToReopen 按 reopened 退役更新。
- cmd/agent + internal/agent + internal/config 全绿（85s/104s/0.07s）。
- internal/tools 全量 10m panic = box-load flaky（memory #1183 判定配方），选层套件覆盖改动面。

## M4.20-C: reopened 退役 → reissue（追加在同一分支）

- `reopened` 从所有迁移表出/入移除（legacy 字面量保留在 CHECK 集合，旧行只读兼容）；transition 错误信息带 reissue 提示。
- `TaskVerifyToReopen` = REISSUE：judged 行保持终态（completed），新 pending 行追加（parent link、payload+reason、anti-loop cap taskVerifyReopenMaxPending=3 保留）；测试断言 judged 行不再翻 reopened。
- `TaskVerify` briefing FAIL 语义 → `--status failed`；verify 输出 REOPENED → FAILED — rework reissued。
- bug-flow tally：reopened 计数 = legacy reopened + failed（reissued 桶）；loop.go 提示词不变（tally 输出面不变）。
- `taskIngestMirrorableSourceStatuses` 去掉 reopened（镜像集只收 pending/claimed/in_progress）。
- CLI usage：status 列表 reopened 行删除，failed 行写明 reissue 语义；reopen 命令文档改 REISSUE。
- 迁移测试：tasklist_running_test（reopened 注释 legacy）、task_verify_test（verify 状态机/装饰计数）、task_dispatch_test（completed→in_progress 拒绝 + verified 终态）、tasks_test.go lifecycle（ended_at 不清除 + 拒绝后退）。
