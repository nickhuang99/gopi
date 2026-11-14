<!-- doc-class: historical -->
# Task Report: memfeedback-motivate — 让 memory_feedback 被 LLM 真实调用（prompt 时序引导）

分支：`feat/memfeedback-motivate` ｜ 工作树：`~/workspace/gopi-memfeedback`

## 背景
交付标准 = **真实 gopi 会话日志证明 LLM 在 memory_search 结果到达当下调用 memory_feedback**
（nick 标准 Rule 11：工具开发必须证明 LLM 实际调用，只写代码+单测=半成品，不能 commit）。

全量会话审计基线：memory_feedback 28 次/21435（0.1%）+ 46% 反馈在会话相对位置 >0.9（deferred 到任务尾），采纳率仅 9%。

Memory 检索（#903/#876/#852）给出的根因：task-start memory_search 是 harness 强制 checkpoint，
但 **taskStartSearchMessage 只命令 "call memory_search" 一个字，从不命令投票**；feedback 只靠工具描述里的软提示，deepseek-v4-flash 对弱提示忽略 → 立即反馈从未进强 checkpoint。

## 修改内容（2 处，全在 prompt 引导，不碰实现逻辑）
1. **`internal/agent/loop.go` taskStartSearchMessage**（harness 最强杠杆，任务开始注入）：
   在 "…取不到→酿造。" 后追加 —— *"Then, while the search result is still in front of you,
   vote memory_feedback IMMEDIATELY on the hits (used→useful, contradicted→useless+tool_use_id) —
   never defer the vote to task end."*
2. **`internal/agent/loop.go` memory_feedback 描述首句强化**：
   前置命令式锚点 —— *"Vote on a memory_search hit RIGHT NOW — 投票=质量裁判, judge each result
   the moment it arrives (搜索结果的当下就投), never defer to task end."*（描述即策略，首句路由决策）

（toolRoutingRules 行 d 项已有的 "IMMEDIATELY after each memory_search result — never defer to task end"
与 search 描述已有的 "immediately assess each hit" 保持不动——前次 merge 已落地；本次补的是
taskStartSearchMessage 这个 harness 强制入口，缺失的最强一环。）

## 密度预算
保持 ≤4500 chars（Rule 1/内存 record #728 教训：master 基线已 4489/4500，改 prompt 必测）。
`TestSystemPromptDensity` PASS，`internal/agent` 全包 fresh 测试 39.5s PASS。

## 验证（关键 —— LLM 真实调用证据）
设置：`HOME=/tmp/mf-home`（隔离 sessions/raw）、`GOPI_MEMORY_DB=/tmp/mf-home/memory.db`（scratch 库，
不污染 ~/.gopi/memory.db）、`--settings /tmp/mf-home/.gopi/settings.json`（DeepSeek flash）。
scratch 库预种 3 条 gopi 相关记录（compact 配置 / text2img 渲染验证 / 立即投票）。

### 场景 1 —— "先查记忆里 gopi 的 compact 配置经验，然后…--db 隔离"
- 会话：`fbfe4c56-bb53-4597-9a24-d63f86399648.jsonl`
- 时序（assistant 轮次）：**round1 memory_search → round2 文本 "memory 命中。投票。" → round3 memory_feedback**
- feedback 调用：`{"id":1, "rating":"useful", "reason":"直接命中任务核心：--db 隔离与 settings.json 优先级经验"}`
- **gap = 1 轮**（search 结果到达后的下一轮投票，非任务尾 store 轮/round6）
- round2 文本字面出现"投票"——prompt 时序约束被 LLM 采纳的直接证据

### 场景 2 —— "先查记忆里怎么验证 text2img 渲染质量（像素级 vs OCR），然后照做"
- 会话：`af3fb6dc-78d6-46b7-a2f8-ecd0293d0e21.jsonl`
- 时序：**round1 memory_search → round2 memory_feedback**
- feedback 调用：`{"id":2, "rating":"useful", "reason":"直接命中验证配方，包含最小采样例 z..."}`
- **gap = 0 轮**（search 之后的紧接着一轮，最理想）

两场景均在 search 结果的 1-2 轮内调用（criterion: 搜索后的 1-2 轮内调用 ✓），且均非任务尾。

## 交付标准核对
- [x] 真实 gopi 会话日志证明 memory_feedback 在 memory_search 后 1-2 轮内被调用（≥2 场景）
- [x] go build OK（CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/）
- [x] go test ./...：9 包全 `ok`（internal/tools 307s bash daemon 慢测试，TEST-EXIT:0）
- [x] commit 已做 + 本 devdoc

## 未做 / 说明
- 未碰 memory_feedback 实现逻辑（tools.MemoryFeedback / harness 逻辑一行未动）——只改 prompt 引导。
- 未加 harness 强制投票 checkpoint（会改 R3 完成出口测试的 API call 计数，超出本任务"只改 prompt"范围）。
- toolRoutingRules / search 描述中已有的时序约束是前次 merge 的成果，本次是在其上的补齐。
