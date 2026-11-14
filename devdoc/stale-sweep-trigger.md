<!-- doc-class: historical -->
# Task Report: stale 检测触发点孤岛修复 + 僵尸判定三层证据链 (M4.8 ①②③)

分支 `feat/stale-sweep-trigger`，commit `941b40c`（worktree ~/workspace/gopi-stale-sweep，共享对象库，提交即已落在主仓的分支上）。

## Completed

**① serve pass 第一步无条件 sweep**（`cmd/agent/cli_tasks.go` runTaskServe）
- sweep 挪到 runDispatchOnce / runTaskRun **之前**：本地与 --nodes-only 两条路径都先扫。
- 孤岛根因修复：旧挂点在 runTaskRun 内、claim 之后，`res.Empty → return` 使空队列/空闲 tick 永不扫描——resume 起的任务、无任务可领时段的僵尸此前 10h+ 漏报。
- 新增 `serveStaleSweep()`：**首次 pass 必扫** + 10min 节流（`serveStaleThrottleMin`，Rule 1 命名常量）；零新守护进程（sweep 搭 serve pass 便车）。

**② resume 路径同 sweep**（`cmd/agent/cli_resume.go` runResume）
- 挂在 `resolveResumeLogPath` 之后、`LoadCheckpoint` 之前——续命实例第一动作即报告僵尸，失败路径（checkpoint 损坏）也不影响 sweep 已生效。零新进程。

**③ 僵尸判定三层证据链**（`internal/tools/taskzombie.go`，零先验）
- a) heartbeat 停：last_toolcall_at（fallback created_at）超阈值——即 stale 查询本身。
- b) claimed pid 死活：`claimed_by` 的 `hostname:pid` 用 signal-0 探活（本机 host 才判；**活 pid 一票否决 stale 事件**——心跳滞后是误报；死 pid = confirmed，零误报强证据）。spawn-kill-wait 真实进程单测。
- c) session JSONL mtime：`<GopiDir>/sessions/<session_id>.jsonl` mtime 年龄佐证（只报告不 gate；无文件=-1 未知）。tools→gopilog 是禁用 import 边（chat.go:620），路径用 config.GopiDir 自解析。
- 证据写入 stale payload 新字段 `zombie`（taskZombieEvidence JSON：confidence/pid_dead/pid_alive/session_idle_sec/session_idle_stale/note）——verify agent 按 confirmed→`task reopen`（工作重新入队）或 cancel+重派（按 payload 是否值得续判断）；suspected→人工看一眼。

**附带修复：真去重（原代码只有注释宣称）**
- 查询加 `updated_at < staleSince` 守卫 + notify 后 `updated_at` bump（`taskZombieBumpSQL`，in_progress 守卫不污染终态行）——无条件 serve 扫描现在每僵尸每阈值窗口只报一次，不随 tick 重发。

**留待后续（按 Epic ④）**：proportional threshold（按 estimate 比例）等 tasklist_issued_at 数据攒够再做，固定 30min 不变（`GOPI_TASK_STALE_SEC` 仍可覆盖）。

## Not Done
- verify agent 的自动 reopen/cancel 执行器（Epic ③ 的"动作"半边）：payload 证据已就位，执行策略属于 task-verify-loop 的后续迭代（避免本任务越界改状态机）。
- 比例阈值（④）：如上，等数据。

## Verification
- Build: `go build ./cmd/... ./internal/...` OK；`go build ./...` 对 devdoc/archive/taskingest-cas-test-archived.go（master 上 e1449e9 归档的测试草稿）报 undefined——master 既有，非本改动。
- 新增测试 11 个，全过：`go test ./internal/tools/ -run 'TestPidAlive|TestZombieVerdict|TestSessionIdleSeconds|TestDetectStaleTasks_LivePidSuppressed|TestDetectStaleTasks_RearmDedup|TestStalePayloadZombieEvidence'` ok；`go test ./cmd/agent/`（含 TestServeTick_StaleSweepFiresWithoutTaskRun / TestRunTaskServe_OnceSweepsZombieFirst / TestServeStaleThrottle / TestResumeSweepsStaleRows / TestResumeHookDoesNotDisturbResolution）ok。
- 实测对照 Epic 验收：a) kill -9 复现行（in_progress+死 pid+超时心跳）在 serve tick 内落 stale 事件、零 task run（TestServeTick…）；b) pid 存活检查逻辑单测（真实 spawn/kill/wait，TestPidAlive）；c) resumed 路径 sweep 生效（TestResumeSweepsStaleRows）。
- 全量回归（job 日志整轮）：`go test ./cmd/... ./internal/...` 全绿零 FAIL。注：TestEnsureTagsRegeneratesForNewerTS / TestMemorySearchNonBlocking 孤立复跑时在**干净 master 上同样失败**（git stash 对照验证）——master 既有 flaky，非本次引入。
- 测试 hermetic 化：GOPI_TASK_NOTIFY_WEBHOOK 显式置空——开发 shell 导出的真实 webhook 否则会向 192.168.7.6:8923 发包。
- Commit: `941b40c`（实现+测试）

## 记忆要点（已存 hippocampus）
- stale 事件 payload 现带 zombie 证据字段；claim stamp 是 hostname:pid（taskDefaultClaimHolder）。
- 沙箱跑 task CLI 的铁律（memory #1146）本次直接套用：所有测试 GOPI_MEMORY_DB/GOPI_HOME 隔离。
