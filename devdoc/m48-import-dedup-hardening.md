<!-- doc-class: historical -->
# Task Report: M4.8 import 去重加固 — canonical hash + 相似度门 + dedup-scan 审计

Epic：预见性审查发现 PA 双投场景（同 brief 分别 submit 到两库）task_id 不同，
hash 去重是唯一防线；但 briefing 内嵌派发时间戳/路径（泄漏①）或 summary 改
一字（泄漏②）都会让 hash 不同 → 同一工作两节点各跑一遍。

## Completed

- **三级去重 ladder**（`syncTaskDedupDecide`，internal/tools/memory_sync_task.go）：
  `exact`（原 sha256(summary+NUL+payload)）→ `canonical`（规范化 hash）→
  `similar`（trigram Jaccard）。import 的 live 探测、patch 内孪生判定、
  dedup-scan 审计共用同一个决策核——判定与计数永不漂移。
- **泄漏①修复**：`syncTaskCanonText` 正则清洗绝对路径、5 种时间戳形态
  （unix 秒/毫秒、ISO T/空格、HH:MM:SS）、空白 run，ASCII 小写化后 hash。
  短文本（<40 rune）跳过规范化（清洗后无信号）。
- **泄漏②修复**：`syncTaskCanonNearPair` 对 summary 与 payload **双轴分别**
  做 trigram Jaccard（复用 MemoryStore brew 的 `trigramSet/jaccard`），
  任一轴命中即判孪生。阈值 0.35（brew 的 0.5 对 CJK 过严：一词之差的
  CJK 标题 Jaccard 0.625、正文只 0.23；无关短 brief 实测仍分得开）。
  摘要轴是关键词：一词之差在 10 字标题上稠密（0.625），混进全文会被稀释（0.23）。
- **patch 内孪生**：同 patch 携带孪生行（同 brief 双投后一起 export）在
  task_id 分支前判掉，防第二份入队。
- **dedup-scan 审计**（epic ③）：`TaskDedupScan` + CLI
  `gopi memsync --dedup-scan [--live]`——全 ledger（--live 限三活状态）
  两两走 ladder，报告孪生对（task_id A/B + 命中门）。**report-only**：
  不删行不迁移，哪个孪生是真工作由人/PA 裁决。发现孪生 exit=2 供 CI/serve 用。
- **统计行增强**：duped 时 Errors 里写明命中门与孪生 task_id
  （`duped by similar gate (live twin t-a)`），审计可解释。
- **回归测试** `memory_sync_task_dedup_test.go` 9 个：双泄漏 E2E、
  无关工作负例、幂等重导入、scan 孪生/干净、规范化单元、短文本旁路。
- 触发条件（epic ②语义）：similar 门**总是**在 import 时对 live 行运行
  （非仅 hash 失配后）——它就是 hash 的替代防线；dedup-scan 是事后审计层。

## Not Done

- `task run` / `task_evaluate`（LLM 投递路径）未加同款门：LLM 走
  TaskSubmit 直投时 exact 门存在（DB 内 summary+payload 相等才算），
  canonical/similar 门未接入——跨节点泄漏经 import 通道已堵，单节点
  LLM 直投孪生仍靠人。可作为后续任务（接入点在 TaskSubmit INSERT 前）。
- M4.13 delta 通道（SyncTaskImportDelta）未接 similar 门（其按 task_id
  对齐，hash 泄漏面小）；harvest 通道的 same-stamp ladder 不变。
- 竞争收尾（Rule 9）不适用：无裁决，单人实现。

## Verification

- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: `go test ./internal/tools/ -run 'TestCanonical|TestCanonHash|TestThreeGate|TestLadder|TestImportDedupHardened|TestImportReRun|TestDedupScan' -count=1` → ok（9/9 PASS）
- 回归：`go test ./internal/tools/ -run 'TestTaskSync|TestSyncTask|TestHarvest' -count=1` → ok（既有 memsync/harvest 无破坏）
- E2E（双 GOPI_HOME 模拟两节点，3 行孪生 patch）：
  - 新库首投 `queued=1 duped=2`（泄漏①canonical 门 + 泄漏②similar 门各拦 1）
  - 同 patch 重投 `queued=0 duped=3`（幂等）
  - 干净 ledger `dedup-scan: 1 rows scanned, 0 twin pair(s)`（无误报）
  - hp 行数保持 1 条 pending（token 不再双烧）
- Commit: de7483a on feat/import-dedup-hard（worktree gopi-dedup-hard，共享主仓 refs）

## Branch

feat/import-dedup-hard @ de7483a — 恢复命令：
`git -C ~/work/GoPi show feat/import-dedup-hard --stat`（worktree 共享，已入主仓）
