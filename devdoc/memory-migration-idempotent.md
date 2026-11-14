<!-- doc-class: historical -->
# Task Report: memory.go 迁移 ALTER 幂等化 — 修复并发双实例 duplicate column name

## Completed
- 根因定位：`ensureFeedbackSchema`（internal/tools/memory.go）先 `PRAGMA table_info(distilled_qa)` 探列，再无条件 `ALTER TABLE ... ADD COLUMN useful_count/useless_count/fb_last_at/tier/confidence`。两个并发 gopi 实例同时首次打开共享 `~/.gopi/memory.db`（测试用两个并发 `sql.DB` 句柄模拟，`MaxOpenConns(1)` 是 per-pool 不互斥）时，双方都探到列缺失、都执行 ALTER，后到者收到 `SQL logic error: duplicate column name: useful_count` → `MemoryStore` 失败。探列→ALTER 之间是 TOCTOU 窗口。
- 修复：ALTER 后吞且仅吞 `duplicate column name` 错误（`strings.Contains` 判定），与 tasks.go `tasksP2Columns` 的幂等 ALTER 同型。PRAGMA 探列保留（常态路径免一次冗余 ALTER）；竞态败者的报错按"列已存在即迁移目的达成"处理。9 行变更，含 TOCTOU 机理注释。
- commit `d1d34b6` on branch `fix/memory-migration-idempotent`（worktree ~/workspace/fix-memory-migration-idem，与主仓库共享对象库，commit 已在主仓库分支上）。

## Not Done
- `migrateDefaultTriggers` 里对 `ensureFeedbackSchema/ensureTasksSchema` 的调用本就 `_ =` 吞错（best-effort），未改。
- 全量 `go test ./...`（~10min）未跑，仅跑 epic 点名的 -race 测试 + 回归集；包内其余测试与本次 9 行变更无交集（唯一行为变化是错误吞噬）。

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK；`go vet ./internal/tools/` OK
- 复现修复对比：master 原生 `go test -race ./internal/tools/ -run TestMemorySearchNonBlocking -count=3` 3/3 FAIL（/tmp/race-confirm.log：`memory_test.go:542: concurrent store: SQL logic error: duplicate column name: useful_count (1)`）；修复后同命令 3/3 PASS（/tmp/fix-race.log，13.4s）
- 回归：`go test ./internal/tools/ -run 'TestEnsureTasksSchemaMigratesLegacyFourStatusDB|TestMemoryFeedback|TestMemorySearchNonBlocking' -count=2` 28/28 PASS
- Commit: d1d34b6 `fix(memory): idempotent feedback-schema ALTER under concurrent first use`

## 恢复命令
```
git -C ~/work/GoPi show fix/memory-migration-idempotent:internal/tools/memory.go
git -C ~/work/GoPi log --oneline fix/memory-migration-idempotent -1
```
