<!-- doc-class: historical -->
# Task Report: git_blame — LLM 真实调用（测试驱动工具开发）

## 目标
git_blame 是 336 行强契约工具，但全量会话审计显示 0 次调用/21435（根因：routing prompt 漏行 + 描述首句是纯功能）。本次任务：通过 prompt 引导让 LLM 在真实任务中稳定调用 git_blame，并以真实会话日志作为交付证据（the operator 交付标准）。

## 修正方案（本 agent 的方法）
只做 **一处** 针对性改动：`internal/agent/loop.go` 中 git_blame 工具的 `Description` 首句。

- **改前（反模式 — 纯功能）**：`"Query git blame history for a file's line range..."` —— 描述首句是能力罗列，没有路由决策。
- **改后（claude ALWAYS/NEVER 命令锚点）**：`"Call git_blame for ANY git line-history question — who wrote line N, which commit introduced a bug... ALWAYS use git_blame over \`bash git blame\`... NEVER run \`git blame\` via bash when git_blame answers the question."`

**为什么只改描述、不改 routing 块**：master 的 tool-routing-fix 已在 `toolRoutingRules` 加入 git_blame 路由行，且被 `TestSystemPrompt_ToolRoutingAuditFixes` guard 锁死措辞（`git_blame: git file line-history — "谁写的这行..."`）。描述不在密度统计范围（claudeToolDefs 不参与 assembleSystemPrompt 计数），因此把最强的命令式锚点放进描述首句 = **零密度成本** 获得最强的说服机制（对比 claude's grep: "ALWAYS use {Grep}. NEVER invoke grep as Bash"）。density 保持 4492/4500。

## Verified（HARD 交付标准逐条）

### 1. 真实 gopi 会话日志证明 git_blame 被 LLM 调用 ≥2 次独立场景 ✅
两个独立任务场景，真实 `deepseek-v4-flash` 会话均先调用 git_blame 工具（再以 bash git log -L 交叉复核）：

**场景 1** — "找出 internal/agent/loop.go 第 159 行的作者和 commit"
- 会话 id：`73cb40bb-e393-427f-9cc0-0a4c1fd31762`
- tool_use 记录：`id=call_01_n7cILzfvnZKlrVznUaA23854 name=git_blame input={"file_path":"internal/agent/loop.go","start_line":159,"end_line":159}`
- tool 计数：git_blame 1, bash 2, memory_search 1, memory_store 1

**场景 2** — "查 gitblame.go 第 31 行 gitBlameMaxSpan 常量归属 & 热点行"
- 会话 id：`d9ed875b-a2fa-43d7-ae79-c8018460916d`
- tool_use 记录：`id=call_01_B7idbefS5y5Kst16EXw77990 name=git_blame input={"file_path":"internal/tools/gitblame.go","start_line":25,"end_line":40}`
- tool 计数：git_blame 1, bash 3, memory_search 1, memory_feedback 2, memory_store 1

证据均存于 `~/.gopi/sessions/*.jsonl`，PA 可复核。

### 2. go build + go test 全绿（涉及本改动的包） ✅
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`：OK
- `go test ./internal/agent/`（本改动所在包）：**OK** (30.6s)
- `go test ./...`：唯一失败为 `internal/tools - TestMemorySearchNonBlocking` 的 `SQLITE_BUSY`（数据库锁竞争）。**已证明是 pre-existing flake**：stash 掉本改动后在 master 原样复现 FAIL，且单独重跑 3 次为 FAIL/OK/OK（间歇性并发锁）；与本次 prompt-only 改动无关。

### 3. commit + devdoc 报告 ✅
见下方 commit 记录。

## 实测发现（迭代过程）
- 初版把 routing 块的 git_blame 行也改成 ALWAYS/NEVER 措辞，但 `TestSystemPrompt_ToolRoutingAuditFixes` guard 锁死原措辞导致 FAIL，且密度超 4500（4534）。回退 routing 行、保留描述锚点后 density 回到 4492，全部测试通过。→ 结论：描述首句是零成本的正确说服落点。
- 两次会话模型思维都先做 memory_search 找到过往经验（"git blame 只给最近改动，需 git log -L 区分引入 commit"），再调用 git_blame —— 工具与记忆配合良好。

## Commit
`665967d` `feat(prompt): git_blame description command anchor (ALWAYS/NEVER) — LLM adoption proven`
（逐文件 add: loop.go + devdoc 报告, 未用 git add -A）
