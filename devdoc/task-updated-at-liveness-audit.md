<!-- doc-class: historical -->
# Task Report: updated_at == created_at 疑点审计 + updated_at 写入路径溯源

Epic: task 8cbf768f 的 show 输出中 `created_at == updated_at`（2026-09-01 20:26:43 两次相同）
"against logical — updated_at must be much later"；要求溯源 updated_at 的填充机制，
并评估"LLM 的 task-list 是更新 updated_at 的好时间线"这一设想。

## Completed

### 0. Epic 前半（merge 部分）已闭环 — 复核确认
- `git merge-base --is-ancestor peerbox/feat/branch-scan-archive-filter master` → MERGED，
  0 commits ahead。merge commit **652ea2a** + 报告 **07201cd**（devdoc/branch-scan-archive-filter-merge-report.md）。
- 本 Epic 剩余工作即本审计。

### 1. 疑点直接解释：那是"运行中的 auto 行"的中间快照
- `task show 8cbf768f…` 是在**该会话还在跑的时候**看的（该行 20:41:06 才终态）。
  auto-register 行的生命周期：START 钩子（runEngine）`TaskNew` 插入
  `created_at = updated_at = now`（tasks.go:951-956，同一 `now`）→ 中途 **updated_at 不动** →
  END 钩子 `finishSessionTask` → `TaskUpdateWithResult`（completed/failed）才第一次推进 updated_at。
  → "运行中看 show"必然看到两值相同；跑完后 updated_at = created_at + 863s（实测该行）。
- 全库普查（~/.gopi/memory.db）：lag<2s 的 44 行里 42 行是 terminal/pending（pending 从未被 claim，
  状态从未变过，本来就不该动）；仅 2 行 in_progress lag=0 —— 全部是"正在跑"的会话行（含本审计自己）。
  **0 行** `updated_at < created_at`（backfill-rank.sh ③ 的时钟倒挂类 = 0，与 devdoc 记录一致）。
- 结论：无数据损坏；"created==updated"只是把"行还没被写第二次"误读成了"时间戳坏了"。

### 2. updated_at 写入路径溯源（source-truth，tasks.go / memory_sync_*.go）
updated_at 是 **M4.13 delta-sync 的版本锚**（memory_sync_delta.go 头注释："Every write touches it
(M4.5 invariant)"）。全部写入点：

| 写入点 | 位置 | 触发时机 |
|---|---|---|
| INSERT created=updated=now | tasks.go:951 (TaskNew) | 行创建（auto-register / task submit） |
| taskUpdate | tasks.go:1644 | **状态迁移**（claimed→in_progress→terminal，含 END 钩子） |
| claim CAS / TaskClaimExplicit | tasks.go:2216, 2516, 2527 | pending→claimed（领取） |
| TaskStart (Phase-3 双锁齐) | tasks.go:2544→taskUpdate | claimed→in_progress |
| claim-broadcast / delta merge / harvest | memory_sync_claim.go:463-515, memory_sync_delta.go:565/688, memory_sync_task.go:1071/1092/1843 | 跨节点同步采纳对端真值 |
| tags union / verify evidence / model backfill | tasks.go:2348, 2795, 1571 | 附属写入（顺带 bump） |
| zombie re-arm bump | taskzombie.go:44 (taskZombieBumpSQL) | stale 事件后的去重再武装 |
| ingest 前向迁移 | taskingest.go:177 | 对端终态镜像 |

### 3. nick 的记忆修正："updated_at 每次工具调用都更新"——不是
- 工具调用的存活时间线是**独立的列**，M4.5 ⑦（commit 8a2800c，2026-08-30）：
  `last_toolcall_at`（每次 tool batch 开始，executeToolBatch/executeToolBatchParallel 入口，
  loop.go:3375/3420）+ `tasklist_issued_at`（harness 收到 [TASK_LIST] 块，todoState.update，
  loop.go:523）。30s 节流（taskHeartbeatThrottleMin）。
- 即：**nick 设想的"task-list 是个好时间线"已经是既有设计** —— 就是 tasklist_issued_at 列。
  问题只在（a）它不显示在 `task show` 里，（b）见 ④ 的真 bug。
- updated_at **有意**不随心跳/工具调用推进：它是 sync 锚（delta 窗口 `updated_at > anchor`、
  terminal-tier 后到覆盖 tiebreak、反回流 equal-anchor no-op 全靠它）。每次工具调用都 bump 会把
  delta 窗口炸成全量重推、并把"谁后到"判给心跳噪声 —— 现有分层（版本锚 vs 存活探针两列）是对的。

### 4. 真 BUG（本次审计的实质产出）：tasklist_issued_at 触碰被节流误杀
- taskheartbeat.go:78-80 注释宣称 tasklist 触碰"Not throttled — tasklist blocks are already
  sparse"，但实现 `touchTasklistIssuedAt()` → `touchHeartbeat()` —— 与工具调用触碰**共用同一个
  节流时钟**（heartbeatState.lastWrite，taskheartbeat.go:95-99）。M4.5 提交注释里的"not throttled"
  意图在实现层丢失。
- 后果：只要 task-list 到达时距上一次任意触碰（通常是工具 batch，每轮都刷）<30s，
  tasklist_issued_at 写入被静默丢弃。
- 实证（role-aware JSONL 扫描，~/.gopi/sessions/8cbf768f….jsonl）：
  assistant 文本块含 [TASK_LIST_BEGIN] 的发射 **3 次**（首列 20:26:59，末列 20:40:57），
  assistant tool_use 159 次 —— 而该行 tasklist_issued_at = **NULL**（三次全落在节流窗内被丢）。
  本审计会话（1ea47e3c）同样：已发射多次列表，列仍 NULL。live repro 成立。
- 影响面：stale 检测的 fallback 链 `COALESCE(last_toolcall_at, created_at)` 不看该列，主流程无损；
  但"模型发任务清单"这一 liveness 事实在台账上丢失（观测/未来按 tasklist 频率的 stale 阈值都用不上）。

### 5. 修复建议（P1，未实施 — 见 Not Done）
1. **拆分节流时钟**：tasklist 触碰恢复"不节流"契约 —— 给它独立 clock（或直接 bypass throttle），
   `touchHeartbeat(throttle bool)` 形参化；两列互不挤占。回归测试：先 touch toolcall 再立刻 touch
   tasklist（<30s），断言两列都被写入。
2. `task show` 增列存活事实：运行中行渲染 `[running since claimed_at]`（list 视图已有
   taskRunningSuffix，show 视图缺失）+ `last_toolcall_at/tasklist_issued_at`（有值时），
   让"中间快照"自解释，根治这次的误读。
3. **否决**"每次 task-list 发射也 bump updated_at"：会污染 sync 锚语义（delta 全量重推 +
   后到覆盖被发射噪声主导）；正确分层就是 4 里的独立列 + show 可见。

## Not Done
- ~~代码修复未动工~~ → **已修复**（fix/tasklist-issued-at-throttle，worktree
  ~/workspace/gopi-tasklist-throttle，commit ea28a6b）：tasklist touch 现持有独立
  节流时钟（taskHeartbeatState.lastTasklistWrite，tasklistThrottleMin=30s），
  与 last_toolcall_at 的时钟分离；回归测试 TestTouchTasklistNotThrottledAfterToolcall
  在修复前代码上验证为 FAIL（toolcall touch 后 <30s 的 tasklist 写被丢）。
  详见 devdoc/tasklist-issued-at-throttle-fix.md。
- 审计过程中临时给 t-1788266409 盖过一次 tasklist/last_toolcall 戳用于对照，已 revert
  （`changes()=1` 确认还原），该行两列现为 NULL 原值。

## Verification
- 溯源：tasks.go/taskheartbeat.go/memory_sync_*.go 逐处读过（grep "updated_at = ?" 全集 20 处，无遗漏
  —— 无任何 toolcall 路径写 updated_at）。
- 数据：sqlite 普查 lag 分布（44 同值 / 2 活行 / 0 倒挂）；JSONL role-aware 扫描
  （assistant 文本块计数，非 naive grep —— 避开 user Epic 注入的标记误报）。
- live：本会话自身即 repro（tasklist 列 NULL + 多次发射 + 每轮 toolcall 触碰刷节流钟）。
- Commit: 见 devdoc 提交（docs-only，单文件 add）。
