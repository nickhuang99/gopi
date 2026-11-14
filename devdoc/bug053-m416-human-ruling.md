<!-- doc-class: historical -->
# Task Report: bug053 — 三张 bug 单虚假关账（M4.16 sync-conflict 裁决的系统性副作用）

## Summary

M4.16 的 syncDeltaServerResolve（server 冲突裁决 = 无脑 completed + sync-resolved tag）把三张真实人类工单（bug050/051/052）虚假关账：三单 tags 均带 `sync-resolved` 但实现从未存在于 master 或任何分支。本单修复裁决语义 + 重新提交四张真实工单。

## Completed

### A. 四张真实工单重新提交（bug053 修复要求 A）
- **t-1788279645 [bug054]** = bug050 重提：stuck 同形 tool_use 循环盲区（toolStreak）— loop.go stuckState 扩展
- **t-1788279646 [bug055]** = bug051 重提：writeback 覆盖 summary（content 不可变被违反）— TaskUpdateWithResult 收窄
- **t-1788279646-1 [bug056]** = bug052 重提：dispatch 前缺 db-sync（db-sync-first 未落地）
- **t-1788279646-2 [bug052-reissue]** = 与 bug056 同实现域，可合并处理

### B. M4.16 裁决语义修补（核心代码）
文件：`internal/tools/memory_sync_delta.go` + `internal/tools/tasknotify.go`

1. **`syncRowIsMirrorNoise(existing)`** — 判定本地行是否为登记噪音（可被无脑拉平）：
   - source ∈ {human, llm} 且 payload 非空 = **真工单**（永不无脑拉平）
   - source=auto 或 payload 空 = 登记噪音/镜像行（允许原裁决）
2. **裁决守卫**（`syncDeltaMergeRow` 内原 line 501 调用点）：server 冲突裁决前先检查——
   - 真工单 → **不 resolve**：仅 union conflict tag（`syncDeltaTagConflictNoRuling`，status/ended_at/anchor 全部不动）+ `RowsConflict` + **`ledger-conflict-human-task` 通知**（新事件，人工/PA 裁决）
   - 登记噪音 → 原 `syncDeltaServerResolve` 无脑 completed 照旧
   - 已知冲突重回声（conflict tag 已在 + tags 相等）→ RowsAlready 纯 no-op，不重复告警
3. **新通知事件 `taskNotifyEventLedgerConflictHumanTask`**（`tasknotify.go`）— 随 "all" 事件集启用，走既有 notify 管道（webhook/cmd）

### C. 回归测试（3 个）
`internal/tools/memory_sync_delta_test.go`：
- **`TestSyncDeltaServerRulingBlocksHumanTicket`** — 核心回归：peerbox pending human 工单（source=human + payload）+ HP 镜像同 task_id 同 status 同 anchor 不同 tags 碰撞（bug050/051/052 的真实裁决签名）→ 断言**不被拉平 completed**（status 仍 pending、ended_at 未设、仅 conflict tag）+ ledger-conflict-human-task 告警 + 重回声不重复告警
- **`TestSyncDeltaServerRulingStillFlattensAutoMirror`** — 反向守卫：source=auto 镜像行仍被无脑 completed（原裁决合法用途不破坏）+ legacy-conflict 告警不变
- **`TestSyncDeltaPendingHumanTicketVsCancelledMirror`** — bug053 要求 C 逐字场景：peerbox pending human 工单 + HP 镜像 cancelled → 断言永不落 completed（rank ladder 落到 cancelled 终态可 reopen，但绝不 fake-close 成 completed）

## Not Done
- 三张旧单（t-1788259388/9882/0677）的 completed 状态未回滚为 pending/reopened（实现层已通过重提覆盖；历史行保留 completed 作为裁决副作用证据 — PA 可另行处理）
- bug054/055/056 的实际实现由各自新单执行，本单只重提

## Verification
- Build: `go build ./...` — OK
- Tests: `go test ./internal/tools/ -run 'TestSyncDelta|TestTaskNotify|TestSyncTask'` — ok
  - 新增 2 个回归测试 PASS
  - 既有 delta/tasknotify/synctask 套件无回归
- 全量 `go test ./internal/tools/` + `go test ./cmd/agent/` — 后台运行中（job-1）

## 关键设计决策
- **裁决只该发生在"双侧都写过状态"场景**：M4.16 语义缺陷是"工单被 sync 冲突裁决关账 ≠ 工作被完成"。真工单（human/llm + payload）的 completed 必须由真实工作产生（writeback/worker），裁决只吸收登记噪音。
- 被拦截的真工单行**保持原位**（pending 不动），只打 conflict tag — 下一轮 sync 重回声走 known-conflict no-op，不会反复告警。
