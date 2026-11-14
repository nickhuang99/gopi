<!-- doc-class: historical -->
# Task Report: Memory 族合并 — memory_search/store/update/feedback → 单工具 `memory`(action=…)

## Summary
按 `briefings/brief-tool-memory-family.md` + `SPEC-tool-family-merge.md`(只做 memory 族，不动其他族)，
将四个独立 memory 工具折叠为 **一个 `memory` 工具**，以 `action=search|store|update|feedback` 参数分支。
独立分支 `feat/memory-tool`(linked worktree ~/workspace/gopi-memory-tool)，与 job 族分支并行，未来统一 merge。

## Completed

### 1. toolnames 常量包 (internal/toolnames/names.go)
- 新增 `ToolMemory = "memory"` + 四个 action 常量：
  `ToolMemoryActionSearch/Store/Update/Feedback = "search"/"store"/"update"/"feedback"`。
- 删除 `ToolMemorySearch/ToolMemoryStore/ToolMemoryUpdate/ToolMemoryFeedback`
  (原名常量废弃 — Rule 13: 常量存在即被引用，退休即删除，不留镜像)。
- 文件头注释按 task 族先例记录合并史。

### 2. 工具定义 + 调度 + 校验 (internal/agent/loop.go)
- `claudeToolDefs`：四个 def 折叠为一个 `Name: toolnames.ToolMemory` 的 def。
  - schema 唯一必填 `action`，enum 四值；search/store/update/feedback 各自的参数
    (query/limit/intent… topic/question/answer… id/rating/reason/tool_use_id… tier/topic/…) 全部保留为可选。
  - 描述 = 描述即策略 (6788 设计共识 + 任务族先例)：单条超长描述依次覆盖
    search(USE FIRST/取不到→store/SKIP off-topic/非 dumb RAG/tier 信号/CONTRADICTS→useless+tool_use_id→update)
    + store(蒸馏/DISTILLED/NEVER raw 流水账/5W1H breadcrumbs/mid-task floor) + feedback + update
    (VERIFIED evidence/hot-warm 保留/不删历史/保守 demote-first)。5W1H 面包屑哲学完整保留。
- `dispatchTool` / action 分支：switch action → 各自的 tools.Memory* 引擎函数；未知 action 标准化报错。
- `validateToolArgs`：action 缺失/未知 → "Invalid memory action…"；
  每 action 条件必填 (search→query, store→topic+question+answer, feedback→id+rating,
  update→id+至少一个可改字段)，报错消息含 action 前缀供模型自纠。
- `parallelSafeCall` **action-aware**：search(只读检索) 保持并行；
  store/update/feedback(写/改信任/可丢上下文) 串行。与 task(action=view) 的 R7 规则对称。
- `resultsHaveDeadEnd` / context-slim whitelist / compact store-round 判定：均改为按
  `toolnames.ToolMemory + action=search/store` 区分 (search 结果 prose 不误判 dead-end；
  store round 保护只在 action=store 时计数) — 修掉 merge 引入的旧判定路径。

### 3. System prompt / LLM-facing prose
- toolRoutingRules 4 行 memory 规则改写为 `memory(action=search/store/feedback/update)` 语法。
- completionCheckpointMessage / taskStartSearchMessage 用新语法；hotResidentMessagePrefix、
  memoryEmptyHint(内 tools/memory.go) 等 LLM 可见注入串同步。
- density 从超限(4569)压回 4494 < 4500(整段 trim 后 audit-fix pin 全保留)。

### 4. CLI / gopilog / docs
- `gopi log --memory` 族 = 合并后 `memory` 名 **+** 合并前的历史 wire 名
  (memory_search/store/update/feedback — 档案性过滤 token，旧会话仍可滤)。
- cli_toolnames_contract_test：memory_* flat verb 移出 tool-mirror 表，归入
  "legacy alias，无活常量" 的 cliNonToolSurfaceTokens(注释写明退休原因)。
- GOPI.md Rule 3 工具清单 22→19 (删 4 memory_* 增 1 memory)，测试 TestGOPIRule3_MatchesClaudeToolDefs 绿。
- help.go `gopi memory` 用法、gopilog memoryFilterSet 同步。
- CLI `gopi memory <subcommand>` 维持不变 (operation-key 的独立用户面，本就不 mirror wire)。

### 5. 测试 (internal/agent + cmd/agent + internal/gopilog)
- 重写 memory_tools_test.go：schema 单 def + action enum、描述即策略子串 pin、
  四 action validate、dispatch、feedback/update 引擎端到端。
- 迁移全部 mock 调用点 (Name: toolnames.ToolMemory + action…)：
  search_remove/context_trim/compact_p1/taskstartsearch/taskendstore/deadend/taskendstore_bypass。
- 新增 `TestMemoryActionAwareParallelSafety`(search 并行 / store+feedback+update 串行)。
- 新增 `TestMemoryLegacyNames_DispatchLiteralGate`(Rule 13 grep 门禁：loop.go/compact.go
  不得再出现 "memory_search"/"memory_store"/"memory_update"/"memory_feedback" 字面量)。
- golden 文件 log-memory-filter/grep-scoped-memory 更新 (新 filter 头含 `memory`)。

## Not Done / Deferred
- **LLM 真实调用采纳证据 (Rule 11)**：本任务无独立的真实-会话验证 (验收项"LLM 调用新工具")。
  建议 PA 在 merge 后跑 1-2 个真实任务，检查 ~/.gopi/sessions/*.jsonl 出现
  `name:"memory"` + action 参数 (而非旧名)。代码侧已用 dispatch/gate 测试覆盖。
- job 族 / task 族 不属本任务 (独立分支并行)。
- `TestWorkerBusyRoundResetsIdleStreak` 在 master 上同样失败 (已用 /tmp/gopi-master-check 验证) —
  既有时序问题，非本次引入，不修。
- internal/agent 内少数 Logger debug 文案 (log.Printf "memory_search before first round" 等)
  保留历史说法 —— 内部调试日志非 LLM 面。

## Verification
- Build: `go build ./...` OK (worktree)
- Tests:
  - `go test ./internal/agent/` OK (15.9s solo)
  - `go test ./cmd/agent/` OK (excl. pre-existing flake; golden updated)
  - `go test ./internal/gopilog/ ./internal/config/ ./internal/toolnames/` OK
  - `go test ./internal/tools/` OK — job-7 exit=1 + empty stdout = only "ok" line filtered by grep (498s < 600s no-timeout, no FAIL content); targeted memory tests verified solo passing
- Master 对照: /tmp/gopi-master-check @20dae08 — TestWorkerBusyRoundResetsIdleStreak 同样 FAIL (pre-existing)
- Density: 4494 < 4500
- Commits (branch feat/memory-tool, in shared main repo via worktree — 无需 push):
  ab097ca → abc3878 → a92b9e5 → 7da9293 → 7631bfd → 2ef7cc6 → 3f57684 → 011da25
- 无残留: `grep -rn '"memory_search"|...'` 生产代码仅剩 4 类合法点
  (档案日志过滤 / DB 表名 / CLI 维护 key / 历史注释)。

## Recovery
- Branch: `feat/memory-tool`
- `git show feat/memory-tool:devdoc/memory-tool-merge.md`
