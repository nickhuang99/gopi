<!-- doc-class: historical -->
# Task Report: peerbox→HP 知识备份 — export gate 扩展 + 实际执行

## Completed
- **Export gate 扩展**（feat/export-gate-wide, commit 5ac19ff）：
  SyncExport 默认从 brew-only 扩为**所有非 node:\* 本地行**（brew +
  hermes 导入 + 空 source 早期蒸馏）；node:\* 永不回流（anti-reflow 不变）；
  显式 `--source TAG` 仍可收窄回单 tag（如 memsync --export --source brew）。
  Import 侧无需改（identity=question，human-wins/garbage gate 不变）。
- **测试**：TestSyncExportLocalNonNode（宽 gate + --source brew 收窄 +
  node:\* 排除）；E2E TestMemorySyncE2EBidirectional 增加空 source 行证明
  宽类在 CLI 级传输。tools+agent sync 测试全绿。
- **实际执行**（peerbox decisive-side）：
  `gopi memory sync --peer hp` → pushed=1478。HP live distilled_qa
  1213 → 1617（+404 = node:peerbox）。peerbox 1477 question 中 1476 到 HP
  （余 1 = garbage gate 拒绝的测试行 "ok question?"，按设计）。
- **不覆盖验证**：HP pre-live 快照 vs live diff — 0 lost / 0 content-changed
  （HP 独有 130 行、双侧共有 1072 行全部原样保留）。node:\* 无回流
  （HP node:peerbox=404 仅来自本次导入）。
- **HP 可检索验证**：HP `gopi memory search "FTS5 trigram..."` 与
  "hermes skills import" 均检索到 peerbox 导入的知识行。
- **整库单向快照**：memory-peerbox-full-20260909-051058.db 落 HP
  ~/.gopi/backups/ — 表数 distilled_qa=1488 / tasks=1586 / chats=82 /
  procedural=2 / session_conclusions=310 与源机一致。
- **文档**：devdoc/backup-peerbox-to-hp-routine.md（标准流程含命令清单/
  验收/回滚）；devdoc/memory-sync-assets-2026-09-02.md §6 标记 SUPERSEDED。

## Not Done
- procedural 行走 assets ssh 会话（devdoc 既有 follow-up，仍用文件通道 +
  本次整库快照覆盖）——不在本 epic 范围。
- serve-tick 自动化（memory sync 是手动 verb，设计如此）。
- 真冲突（同 question 双侧内容分歧）裁决：unique-only 通道按设计不动，
  SyncML decisive-side=peerbox 覆盖是留给另一任务的裁决原则（本 op 无覆盖）。

## Verification
- Build: `go build ./...` OK；`go vet ./internal/tools/ ./cmd/agent/` OK
- Tests: tools `-run 'Sync|Memory'` + agent `-run TestMemorySync` 全 PASS
  （tools 全量 sync 区 ~99s，agent E2E ~6s）
- 执行验收：HP 1213→1617（+404 node:peerbox）；peerbox 1476/1477 覆盖；
  0 lost / 0 changed；node:\* 无回流；HP 检索命中；快照表数双侧一致
- Commit: 5ac19ff feat(memsync): widen export gate (worktree feat/export-gate-wide,
  5ac19ff docs 待追加)
