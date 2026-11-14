<!-- doc-class: historical -->
# Task Report: bug052-reissue — dispatch 前缺 db-sync（serve 派远端任务静默失败）

Branch: `feat/bug052-dispatch-dbsync`

## 根因（证据链复核）

`cmd/agent/dispatch.go` 的 `dispatchToNode` 只有一步：`ssh <node> "nohup gopi task run <id> ... >/dev/null 2>&1 &"`。
行只存在于 peer1（中心）库 —— 从未 push 到对端 HP 库。HP 端 `gopi task run <id>` 找不到行，
报 `no task with task_id` 后退出；`nohup >/dev/null` 吞掉全部错误 → serve 端看起来"已派发"，
实际任务从未执行（t-1788260077 事故，原单 t-1788260677 被 sync-resolved 虚假关账，bug053）。

**附带发现的第二个洞**：`runDispatchOnce` 的失败回滚用 `tools.TaskUpdate(taskID, pending)`——
但状态机 `taskAllowedTransitions` 里 **claimed → pending 是非法迁移**，TaskUpdate 静默拒绝（返回 error 被 `_ =` 丢弃）。
即失败路径的回滚本来就是 no-op，任务卡在 claimed。修复统一改用 `tools.RollbackLocalClaim`
（M4.21 的合法 claimed→pending 原语，保留 claimed_by 审计 + 打 `sync-claim-revert` tag）。

## 修复内容（对应 epic A/B/C）

### A. dispatchToNode 三腿派发（db-push → confirm → run）

1. **Leg ① db-push**（`dispatchPushToNode`）：`dispatchExportRow` 把目标行导出为单行 v2 delta patch
   （summary + **payload** + tags + source 全带上 —— payload 是 runnable 的必要条件，bug028），
   stdin 走 ssh 打到对端 `gopi task sync --serve-remote`（**delta import 路径 = 唯一能在对端创建行的动词**；
   claim-remote 只能改对端已有的行 —— 正是 bug052 的洞）。
   随后 `gopi task show <id>` 确认（exit 0 + `task_id:` 行）—— 确认失败 = 行未落地 = 中止派发。
2. **Leg ② nohup run handoff**（原逻辑不变）。
3. **Leg ③** 中心行 claimed → in_progress（合法迁移，remote 后续 report-back 从 in_progress 可收）。

### B. 失败路径（不再静默）

- **db-push 失败 / run-start 失败**：`dispatchLogFailure` 把错误追加到磁盘
  `~/.gopi/raw/dispatch-errors.log`（GOPI_RAW_LOG_DIR aware，非 /dev/null）＋ stderr 双写；
  `RollbackLocalClaim` 把中心行回 pending（任务留在队列，下个 tick 重试）。
- **runDispatchOnce 两个 release-claim 路径**也改为 `RollbackLocalClaim`（修掉非法 no-op）。

### C. 测试（3 个新用例，mock-ssh 双库沙箱）

沙箱：中心库（GOPI_MEMORY_DB）+ 对端库（第二 TempDir）；`dispatchSSHStdinFunc` seam 跑对端
**真实** `SyncRemoteServe` import（完整导入路径，仅省字节管道，bidir2 沙箱模式），
`dispatchSSHFunc` 从对端库答 `task show` / 记录 run handoff。

1. `TestDispatchToNodeDbPushSeedsPeerRowBeforeRun`（C-①）：派发后 → 对端库有行（含 payload）→
   run handoff 带 id + report-to + repo 触发 → 中心行 in_progress。
2. `TestDispatchToNodeDbPushFailureRevertsToPending`（C-② db-push 失败）：行回 pending +
   `sync-claim-revert` 审计 tag + `dispatch-errors.log` 落盘含 `db-push`。
3. `TestDispatchToNodeRunStartFailureRevertsToPending`（B run-start 失败）：行回 pending +
   落盘含 `run-start`（防幻影 in_progress）。

## 验证

- Build: `go build ./...` OK；`go vet ./cmd/agent/` OK。
- Tests: `go test ./cmd/agent/ -run 'TestDispatchToNode|TestDispatchSSHArgs|TestDispatchNodeLoad|TestDispatchPickNode|TestDispatchRemoteCommand|TestParseNodesFlag|TestResolveComputeNodes'` — 8/8 PASS。
  全量 `go test ./cmd/agent/ ./internal/tools/` 后台运行中（历史全量 ~264s，见 memory #1133）。
- 未做：真实 ssh 对 hp 的 live-fire 派发（沙箱 seam 已覆盖全导入路径；live 冒烟需在 peer1 上配
  GOPI_COMPUTE_NODES + GOPI_SYNC_PEERS 实跑 serve —— 本次不触碰生产拓扑）。

## Not Done / 说明

- 对端 HP 的 `gopi task run` 会跑自己的 claim-lock broadcast（其 sync_peers）。若 HP 的 peers 含中心，
  中心行已 in_progress（claimed_by=中心）→ HP 的 claim-remote acquire 会 REFUSE → HP yield。
  生产拓扑里 HP 是叶子节点（无 sync_peers），不受影响；中心≠HP peer 的拓扑下由 claim-lock 协议兜底
  （本轮未改该协议，超出 epic 范围）。
- `dispatchRemoteCommand` 仍用 `nohup ... >/dev/null 2>&1 &`（handoff 语义不变）；"不再 >/dev/null"
  指的是**中心侧**失败落盘 —— remote 侧 run 的错误由 report-back 路径（--report-to）回传，非本次范围。
