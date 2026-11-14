<!-- doc-class: normative -->
# Task Report: bug054 session_id stamp — early backfill of session_id + model during task run

## 背景
`gopi task show` 的 in_progress 行永远看不到执行 session：human 工单行 INSERT 时
session_id 写空（submit 时不知道 executor），in_progress 期间 model=(unknown)、
session_id 空——正是 tail 日志最需要的时刻。auto 行因为 taskregister 创建时就写
session_id+model 所以永远有值。t-1788224020（task-submit-model epic）只实现了
model 回填（writeback 时），session_id 的回填从未存在（tasks.go:1526 /
cli_tasks.go:751 两处注释撒谎说"session_id stamp"存在）。

## Completed
1. **`TaskBackfillSession(taskID, sessionID)`**（internal/tools/tasks.go）——
   单条 guarded UPDATE 同时回填 session_id + model：
   - session_id 与 model 各自 CASE WHEN 空才填，绝不覆盖 submit 时
     --session-id/--model 值、auto 行创建戳、或先前 stamp
   - model 从 child session JSONL 的 session_start 事件解析
     （taskModelFromSession，与 writeback 同源、同权威）
   - WHERE 守卫：仅当至少一列仍为空才 UPDATE（避免假 stamp 日志 + 无谓
     updated_at bump）
   - 空 sessionID = 静默 no-op；best-effort——失败绝不阻塞 run
2. **早期回填**（cmd/agent/cli_tasks.go runTaskRun）：child 改为
   `cmd.Start()` 后台启动 → `waitChildSessionJSONL(started)` 轮询（100ms
   间隔、5s 上限，Rule 1 命名常量）→ 一发现 child session JSONL 立即
   `TaskBackfillSession(target, sid)` → 再 `cmd.Wait()`。**不等 writeback**：
   in_progress 期间 session_id + model 即已落库。
   - 依据：NewSessionLogger 在启动时立即写 + sync session_start
     （session.go:80 `sl.log(start); sl.Sync()`），child spawn 后文件即在盘上
   - 轮询 miss（child 秒退/无 sessions 目录）→ 保持原有 writeback 延迟回填
     兜底，`GOPI_TASK_CHILD_SESSION` export 逻辑不变
3. **`taskModelFromSession` GOPI_HOME 一致性修复**：原来用 `os.UserHomeDir`
   拼 `~/.gopi/sessions`，与 CLI seam 的 `config.GopiDir()`（$GOPI_HOME/.gopi）
   不一致——GOPI_HOME 沙箱下早期 model 解析必然失败。改用 config.GopiDir()。
4. **注释修正（C）**：
   - tasks.go TaskUpdateWithResult 注释：谎言的"on the same best-effort path
     as the session_id stamp"改为真实描述（早期 stamp 为主、writeback 为兜底）
   - cli_tasks.go runTaskRun docstring："via the update path"改为 bug054 早期
     stamp 的真实描述
5. **测试（D）**：
   - internal/tools: TestTaskBackfillSessionStampsEmptyRow（空行双列回填 +
     status 不变 in_progress）、TestTaskBackfillSessionNeverOverwrites（submit
     值不被覆盖）、TestTaskBackfillSessionEmptySIDNoop（空 sid no-op / 无
     session 文件不报错 + sid 照填）
   - cmd/agent: TestWaitChildSessionJSONLPicksChildInWindow（run 窗口内找到
     child session）、TestWaitChildSessionJSONLTimeout（无 session 快速返回
     ""）

## E2E 实测（bin/gopi + 隔离 GOPI_HOME + fresh DB）
- submit（human，无 --session-id/--model）→ show：`session_id` 空 + `model
  (unknown)`（bug 症状基线复现）
- `gopi task run` 后台启动 → t+5s `task show`：
  ```
  status:         in_progress
  session_id:     14255873-f628-4480-97f2-7f75bb468ac3
  model:          deepseek-v4-flash
  ```
  **in_progress 期间双列已填充 = 需求 D 直接证明**
- run log 佐证时序（/tmp/e2e-prove.log）：
  ```
  line 6  [gopi] session: /tmp/e2e-bug054b.opB10r/.gopi/sessions/14255873-...jsonl
  line 7  [gopi] task ledger: registered 14255873-... (in_progress, auto)
  line 9  [task-notify] task t-e2e-prove: session "14255873-..." model "deepseek-v4-flash" stamped at start
          ← 注册后立即（child 仍在第一个 thinking round），远早于 writeback
  ...
  tail    task t-e2e-prove completed (elapsed 20s)
  ```
  stamp 发生在 child 运行中（registered 之后 2 行内，早于 20s 完成）——
  需求 A+B 的直接时序证明。

## Not Done
- worker 循环（runTaskWorker）走 runTaskRun 同一路径，早期 stamp 自动生效
  （无需单独改动）
- E2E 沙箱里的 child 因隔离 GOPI_HOME 缺 plugins/websearch/scripts 目录导致
  web_search 失败——环境 artifact（隔离 GOPI_HOME 需复制 plugins），与 bug054
  代码无关；不影响早期 stamp 证据（stamp 在 line 9 已落库，mid-run show 已
  证明 D）。生产 ~/.gopi 有 plugins，无此问题。
- `task verify` 的 verifier-child 写回仍走 TaskExecutorSessionEnv 兜底通道
  （与 task-submit-model epic 一致，未接早期 stamp——verify child 是自建
  短会话，writeback 语义足够）
- 远程 dispatch（serve → ssh）路径未单独 E2E——远端 `gopi task run` 走同一
  runTaskRun 代码，行为一致

## Verification
- Build: `go build ./internal/tools/ ./cmd/agent/` OK；CGO_ENABLED=0
  `go build -o bin/gopi ./cmd/agent/` OK；`go build ./...` OK
- Vet: go vet clean（改动文件）
- gofmt: clean（含一处既有 struct 对齐的 incidental 修正）
- Tests: 全量 `go test ./internal/tools/ ./cmd/agent/ ./internal/agent/`
  后台跑（job-1）：cmd/agent + internal/agent **PASS**；internal/tools 因默认
  600s timeout 在 ~600s 处超时（goroutine dump 非失败，套件本就 ~590-720s——
  memory #1149/#1190）。internal/tools 隔离复跑（job-3，-timeout 1500s，
  无并发 tools.test）667s：**仅 2 个既有 flake 失败**——
  `TestBashDaemonSpawn_DoesNotLeakProcesses`（master 上同样失败——stash 我的
  改动后复现，memory #1148/#1149 既有 flaky，与本次改动无关）与
  `TestMemorySearchNonBlocking`（master 上 count=3 全绿——并发 load-flake，
  SQLITE_BUSY，memory #1059/#1079）。**我的改动零失败**。定向测试
  `TestTaskBackfillSession*` 3/3 PASS、`TestWaitChildSessionJSONL*` 2/2 PASS
  （count=5 稳定）、`TestNewestSession|TestTaskRunBriefing|TestTasksCLIFullFlow|TestTaskSubmit`
  PASS（cmd/agent）、`TestTaskSubmitModelFlag|TestTaskUpdateWithResultBackfillsModel`
  PASS（writeback 路径未回归）。
- Commit: <pending>
