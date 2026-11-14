<!-- doc-class: historical -->
# Task Report: tasks 台账 legacy 四状态 CHECK 重建迁移 — FTS 触发器丢失修复的独立验收

## Completed
- 独立验收测试 `internal/tools/ftsync_verify_test.go`（commit 4f3c432，分支 feat/ftsync-rebuild-rescue）：
  - `TestTasksMigrationFTSParityClassic`：经典 legacy 形态（4 状态 CHECK + 3 列 FTS companions，靠 migrateTasksFTS 形状漂移 3→7 列触发 rescue）
  - `TestTasksMigrationFTSParityIntermediate`：中间态形态（4 状态 CHECK + 已是 7 列 FTS companions —— **真洞**：migrateTasksFTS 的形状检查匹配 → 无 rescue，只有重建迁移自身的 rescue 能救迁移行）
- 验收标准逐条覆盖（每次断言调用）：
  1. `count(tasks_fts) == count(tasks) == count(tasks_tri)`（fresh 提交前后各查一次 —— 旧行全部可召回 + 新行不双写）
  2. `tasks_ai/tasks_au/tasks_ad` 三个触发器存在、`tbl_name='tasks'`、DDL 不含 `tasks_old`
  3. fresh INSERT 即时入索引（新触发器生效）；第二次 `ensureTasksSchema` 后 parity 保持（幂等不回归）
- 测试配方遵循记忆 #1117：裸文件先做 `distilled_qa`/`memory_feedback` 标记（openTasksDB 拒绝非知识库 DB）、legacy 手工 CREATE + 三终态种子行（completed/failed/cancelled）。

## 修复现状核实（本 Epic 的生产代码部分）
- 修复已存在于基线：712ccd2（feat/fix-3major，已 merge 进 master → 本分支基线 5c6f5ca 含之）。
- `tasksRebuildCheckConstraint`（tasks.go:382-560）在 rebuild commit 后执行 FTS rescue：DROP/重建 tasks_fts+tasks_tri → `VALUES('rebuild')` 全量回填 → ai/au/ad 触发器最后重建（防 rebuild 误触发）。
- 既有回归 `TestReproLegacyMigrationLosesFTSRows`（repro_review_test.go）PASS。

## 排障记录（测试迭代中的关键发现）
- 初版测试 `wantRows=4` 失败（fts=3）：每次 `ftsVerifyAssertAcceptance` 调用都会 TaskSubmit 一行 fresh，绝对计数天然"落后一步"；parity（索引数==表行数）才是被测不变量。已改为 fresh 提交前断言绝对值 3、提交后仅断言 parity，注释说明语义。
- `ensureTasksSchema` 的 M4.11 priority backfill 对 NULL priority 行做 UPDATE —— `tasks_au` 触发器对 external-content FTS 是 delete+insert 对写，行数 parity 不变（probe 验证 fts=4→4），非缺陷。
- 全量套件 2 个预存失败与本任务无关（基线 stash 对照复现）：
  - `TestEnsureTagsRegeneratesForNewerTS`：环境 ctags 是 Exuberant 5.9（无 TS/TSX 支持）——记忆 #1139 的 environmental 失败归因配方直接命中
  - `TestJobNoNoticeOnKill`：flaky（基线 -count=3 复现 FAIL/PASS 混合；含本测试的定向 -count=2 全绿）

## Not Done
- 不重做生产修复（已存在且被既有 repro 覆盖）—— 本任务交付的是 Epic 验收标准的独立测试面。
- merge 到 master 未做（Rule：PA 验收后处理）。

## Verification
- Build: `go build ./...` OK
- Tests: 定向 `go test ./internal/tools/ -run 'TestTasksMigrationFTSParity|TestReproLegacyMigrationLosesFTSRows|TestEnsureTasksSchema|TestJobNoNoticeOnKill' -count=2` 全 PASS；全量仅 2 个预存环境/flaky 失败（见上，基线对照复现）
- Commit: 4f3c432（feat/ftsync-rebuild-rescue，worktree ~/workspace/gopi-ftsync-rescue 共享主仓对象库）
