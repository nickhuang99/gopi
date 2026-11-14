<!-- doc-class: historical -->
# Task Report: M4.19 sync-lifecycle — 双层 sync 生命周期原子化（实现层）

## Completed

### ① 起点保守（TaskStart 成功路径）— 核查结论：已完全覆盖，无代码改动
- `cmd/agent/cli_tasks.go` runTaskRun：TaskStart 赢得行后立即 `tools.StartClaimBroadcast(target, owner)` 异步广播 → `WaitClaimBroadcastLines` 有界等待 → 对端拒绝 `os.Exit(1)` 不执行 briefing（所有权让渡优先于双跑）。
- 恢复路径同样覆盖：`TaskResumeClaimed`（claimed 行的 crash recovery）先行，随后同一 TaskStart + broadcast —— 两条进入路径都过起点推送。
- 行为锁定测试已存在：`TestYieldClaimedTaskCancelsWithSyncYielded`（cancel+sync-yielded、TaskNextFiltered 不重领、终态竞态守卫）、`TestClaimBroadcastOutcomeClassification`（refusal→abort / unreachable→fail-soft）。

### ② 终点 must-succeed（核心新增）
- `internal/tools/memory_sync_claim.go`：
  - `PushTerminalTaskSync(taskID)`：复用 claim 通道 push-only 面（`exportClaimPatch` 单行 verbatim + `claimSSHExchange`），**每个 peer 必须接受**；重试 3 次指数 backoff 1s/2s/4s（`terminalPushRetries` var + `terminalPushMaxAttempts/Retry1..3` 常量，Rule 1）。无 sync_peers = 空成功（起点也未推送，语义对称）。
  - `notifyTerminalPushFailed` + 导出包装 `NotifyTerminalPushFailed`：重试穷尽后走 GOPI_TASK_NOTIFY 管道，合成事件 `event=terminal-push-failed`（notifyStaleTask 先例：loadTaskNotifyConfig → enabled → events 过滤 → payload → deliverTaskNotify）。
- `internal/agent/taskregister.go`：`finishSessionTaskRecorded` 在 `TaskUpdateWithResult` 落完本地终态行后调 `PushTerminalTaskSync(sessionID)`；失败→报警+日志，成功/跳过→日志。`GOPI_SKIP_TERMINAL_PUSH=1` 独立 kill-switch（sims/自管理 sync），默认推送 ON。`GOPI_SKIP_CLAIM_BROADCAST` 不动。
- `internal/tools/tasknotify.go`：`taskNotifyEventTerminalPushFailed`/`taskNotifyEventLegacyConflict` 事件名常量；`GOPI_TASK_NOTIFY_EVENTS=all` 现在覆盖合成事件（订阅"全部"者不应漏报报警）；`memsyncWarnf` always-on warn 面。

### ③ 冲突重标（语义级）
- `internal/tools/memory_sync_delta.go`：两条 conflict 路径都接报警 —
  - client conflict-tag 路径（`syncDeltaMergeRow` RowsConflict++ 处，含 content-conflict 分支）：`notifyLegacyConflict(taskID, detail)`。
  - server ruling 路径（`syncDeltaServerResolve`）：warn + notify 后照常 ruling（M4.16 裁决保留为 legacy 兜底）。
  - `notifyLegacyConflict`：`[memsync][warn]` stderr 日志（notify 未配置也可见）+ `event=legacy-conflict` notify。

### 测试（tools 包，17.3s）
- `TestPushTerminalTaskSyncNoPeersIsSuccess`（空拓扑=空成功）
- `TestPushTerminalTaskSyncUnreachableExhaustsRetries`（不可达 peer 穷尽 3 次重试报错，错误含 retry 计数）
- `TestNotifyTerminalPushFailedFiresCmd`（cmd 目标收到 event=terminal-push-failed payload，含 task/错误详情）
- `TestNotifyTerminalPushFailedUnsubscribedIsSilent`（默认 events 过滤下合成事件不投递）
- `TestSyncDeltaLegacyConflictFiresNotify`（client 路径：conflict=1 + event=legacy-conflict + re-echo 不重发）
- `TestSyncDeltaServerRulingFiresLegacyConflictNotify`（server 路径：RowsResolved=1 + event=legacy-conflict）

### 提交（feat/sync-lifecycle-mustsucceed，worktree ~/workspace/gopi-sync-lifecycle，git add 单文件 ×6）
- 021754e feat(sync): PushTerminalTaskSync — the must-succeed terminal push (M4.19 ②)
- 0f65cf7 feat(agent): FinishSessionTask terminal push hook (M4.19 ②)
- f849359 feat(notify): synthetic alarm events terminal-push-failed + legacy-conflict (M4.19 ②③)
- 9372594 feat(sync): legacy-conflict re-label — warn + notify on both conflict paths (M4.19 ③)
- 31a1331 test(sync): terminal-push unit matrix — retry exhaustion + notify contracts

## Not Done（→ 已派 t-1788218896 收尾，fresh-context 执行）
- 全量 `go test ./internal/tools/ ./internal/agent/`（tools 包 3-15min，须 job_start detached -timeout 600s；本会话只跑了目标测试）。
- E2E 4 场景脚本化（scripts/e2e-m419-terminal.sh：场景 2 正常完成对端对齐 / 场景 3 不可达重试+报警 / 场景 4 legacy 冲突注入；场景 1 对照 e2e-m418-yield.sh）。双 GOPI_HOME 同机配方 + GOPI_SYNC_NODE + sqlite3 -cmd ".timeout 5000" 已写进 payload。
- merge 到 master 由 PA 负责（本会话不 merge）。

## Verification
- Build: `go build ./...` OK；`go vet ./internal/tools ./internal/agent` clean
- Tests: 目标矩阵 `go test ./internal/tools/ -run 'TestPushTerminal|TestNotifyTerminal|TestSyncDeltaLegacyConflict|TestSyncDeltaServerRulingFires|TestTaskNotify' -count=1` → ok 17.309s（全量见 t-1788218896）
- Commit: 021754e..31a1331（feat/sync-lifecycle-mustsucceed，5 commits，worktree 已共享到主仓 refs）

## 设计要点（devdoc/devdoc 备忘）
- 终点推送不 new ssh 面：完全复用 claim 通道（exportClaimPatch 是 verbatim 单行 + 幂等 merge + refusal 显式化），对端零新解析。
- 不可达≠拒绝：终点推送把 unreachable 和 REFUSED 都计为"未送达"（重试），与起点广播的 fail-soft 二分不同 —— nick 定稿"at end we are a bit aggressive"。
- 数组不能 const：`terminalPushRetries` 是 var（注释声明 Rule-1 note，同 claimBroadcastTimeout 先例）；生产默认只在声明处。
- 测试坑（本次踩过）：(1) `GOPI_TASK_NOTIFY_EVENTS=all` 原本只展开 status 枚举，合成事件需显式订阅 —— 已改为 all 覆盖报警事件；(2) 不可达 peer 测试不能用 self 做 peer（同进程同 DB，push 会假成功）——用不存在别名；(3) env 变量测试后 `t.Cleanup(os.Unsetenv)`。
