<!-- doc-class: normative -->
# Task Report: bug056 — writeback 覆盖 summary 复发（审计线索丢失）

## Summary

bug051 的"虚假关账"症状再次出现（3128f17f 行，task-merge-CLI EPIC 的 auto
镜像 summary 被覆写为 `gopi run completed — run: elapsed=…`）。根因：harness
写回通道 `TaskUpdateWithResult → taskUpdate` 仍把结果摘要写进 `tasks.summary`
列——summary 不可变规则（the operator 2026-08-30 终审）的唯一漏网之鱼。

本修复实现 bug051 的真实意图：**执行结果不再写 summary 列**，改走独立
append-only `task_events` 审计表（Epic A 选型：倾向 task_events，tag 前缀
方案污染 FTS 索引的 tags 列，被否决）。

## Completed

1. **新增 `task_events` 表**（append-only 审计）：
   - `tasksSchemaEventsTable()`: `(id, task_id, event, detail, created_at)` —
     记录所有状态变迁 + 执行统计摘要。`idx_task_events_task_id` 索引。
   - `ensureTasksSchema` 幂等迁移（CREATE IF NOT EXISTS，零迁移脚本）。
   - 无 FTS5 伴生表（Epic C：审计数据不索引，避免事件写入时全文索引抖动）。
2. **`taskUpdate` 删除 summary SET 子句**——summary 从引擎层面不可变。
   状态变迁统一追加 `task_events` 行（detail = 写回摘要，无摘要则空）。
3. **`TaskUpdateWithResult` / `TaskRunWriteback`**：结果摘要进 events，
   summary 永保 submit 时 payload 摘要。
4. **`TaskShow` 渲染 `result:` 行**：最新事件的 detail 作为 result 行显示，
   操作者仍能看到 run stats（`gopi run completed — run: elapsed=…`）。
5. **auto-register 结束 hook（taskregister.go）**：`[auto]` summary 不再被
   追加 stats——stats 作为 event detail 记录；删除已死的 `autoTaskSummary` 字段。
6. **TaskIngest 远端镜像**：stats 不再追加本地 summary，改写 events 行
   （`(from <node>)` 去重守卫移到 events 查重）。
7. **tasknotify**：`tokens` 解析回退到最新 event detail（summary 无 stats 时）；
   新增 `result` 字段——携带最新 event detail（失败 digest / run stats），
   通知消费者即使 summary 不可变仍能看到 WHY（PA 读通知不丢失败原因）。
8. **TaskVerify 证据链**：evidence 从最新 event detail 读取（child 写回
   摘要现在在 events 里）。

## Not Done

- `chat.go` 的 `ChatBackfill` 迁移标记（`summary = summary || " — chat: backfilled"`）
  —— 单次迁移的固定后缀，非写回摘要，且不在 bug056 的 writeback 覆盖面内；
  未改动（其测试断言该行为）。
- 历史行回填：其余被覆写 summary 行不做恢复（append-only ledger 永不重写，
  与 model 列迁移同策略）；唯一例外 = 证据锚 t-1788254585（Epic ④d 点名，
  原 summary 有引用可恢复——epic 证据链原文'auto task using session id as
  task id'——已于 2026-09-02 用备份 /tmp/memory.db.bug056-pre-repair 恢复）。
  存量 97 行 `gopi run completed|failed — run: elapsed=…` 的原始 summary 无
  备份可恢复（备份均晚于覆写），记录在案。
- 竞争收尾（Rule 9）需 PA 验收后执行。

## Verification

- Build: `go build ./...` OK；`go vet` 三包 OK。
- Tests:
  - `internal/tools` 全量套件运行中（job-1，>120s 用 job_start）
  - `internal/agent` 全量：`ok ... 93.6s`
  - `cmd/agent` 全量：`ok ... 111.6s`
  - 定向：TestTaskUpdateWithResultKeepsSummaryImmutable（新增回归，Epic D）、
    TestTaskSearchFTSAndTriggers、TestTaskIngest*、TestTaskNotify*（含 Result
    字段断言）、TestTaskVerify*、TestAutoTask*/TestChatTask* 全绿。
  - 恢复被误删的 TestTasksRejectForeignDB（foreign-DB 守卫，与新 schema 兼容，
    独立复跑 PASS）。
- E2E（Rule 11，真任务跑通）：
  - `gopi task submit "bug056 E2E — original EPIC summary text" --payload …`
  - `gopi task run t-e2e-056` → completed
  - `gopi task show t-e2e-056`：
    - `summary: bug056 E2E — original EPIC summary text`（原值，未被覆写）
    - `result:  gopi run completed — run: elapsed=15s`（stats 独立载体可见）
  - `task_events` 表内容：`t-e2e-056|in_progress|` → `…|completed|gopi run
    completed — run: elapsed=15s`（append-only，含子会话 auto 行）
- 回归测试断言（Epic D）：summary 与 submit 一致 + 统计在独立载体可见 +
  FTS 只命中原 summary（不索引 events）。

## Commit

- 见分支 `feat/bug056-writeback-summary`（worktree 共享，直接落分支）。
