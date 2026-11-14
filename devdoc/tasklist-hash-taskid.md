<!-- doc-class: historical -->
# Task Report: task list # 列改为 task_id 数字部分（跨节点对账修复）

## Completed
- `internal/tools/tasks.go` TaskList 渲染：`#%d`（本地 rowid）→ `#<task_id 数字部分>`（方案 b）。
  - 新增 `taskHashDisplay(taskID)`：`t-1788146419 → 1788146419`；冲突形态 `t-<unix>-N`、自定义 id（如 pa-q-*）、空串 → 回退完整 task_id（行首永远有 # 标识）。
  - 新增常量 `taskIDCollisionSep`（Rule 1：无魔法字面量）。
- 回归测试：`TestTaskListHashColumnIsTaskIDDigits`（渲染层：#1788146419 出现、rowid #1 不出现）+ `TestTaskHashDisplay`（纯函数 4 case 表驱动）。
- E2E：fresh DB `gopi task submit` → `task list` 实际输出 `#1788172091  t-1788172091  [pending] ...`。

## Not Done
- 方案 a（按 task_id 排序+显示序号）/ 方案 c（去 # 列）未采用——Epic 推荐 b。
- 历史行号 #N 在旧 devdoc 中的引用（task-autoregister-impl.md 等）为历史快照，未回改。

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK；gofmt -l 干净。
- Tests: `go test ./internal/tools/ -run 'TestTaskList|TestTaskHashDisplay|TestTaskNew|TestTaskShow'` PASS。
- Commit: `1d5ef8a` feat/task-list-hash-taskid（worktree ~/workspace/gopi-task-hash-id，commit 已落主 repo 共享 refs）。

## 兼容性检查（# 解析依赖处）
- 全仓 grep `#%d` / rowid 渲染：仅 memory_procedural.go / memory.go 的 memory 表 #（不同表，不受影响）；task list 的 # 无其他 Go 依赖点（task show/run 按 task_id 解析，不解析 #）。
- scripts/*.sh 无解析 task list # 列的脚本（backfill-task-model.sh 按 model 列查询，不碰 #）。
- devdoc 中历史 #N 引用均为展示快照，非程序依赖。
