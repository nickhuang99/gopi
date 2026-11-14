<!-- doc-class: historical -->
# Task Report: Hippocampus Spec (子任务 1/4) — 两份 devdoc + 接口草图

分支：`feat/hippocampus-spec`（worktree `~/workspace/gopi-hippocampus-spec`，基于 master `3ce88c2`）

## Completed

1. **`devdoc/agent-five-organs-ontology.md`**（246 行）— 五器官本体论，memory=海马体为核心器官
   - §0 溯源：明确 `create_hippocampus-agent.txt`(523 行) 是唯一权威原文；**简报里引用的 "devdoc/agent-five-organs-ontology references" 在仓库中不存在**（幽灵引用），故本体论为**新撰写**，"五器官"这一命名/切分是本文的推论并逐处标注 `[推论]` 与原文证据。
   - §2.2 核心裁决：**海马体只输出 `Confidence`/`MemoryWeight` 原始指标，不下"要不要提醒"的等级判决**——等级是调度器（前额叶）的职责；用"参谋/CEO"反例说明越权的代价。
   - §4 解剖表：五个器官逐一给出**已验证的 file:line 锚点**。含一处关键更正：`internal/agent/heartbeat.go` 自 `f81bf48` 起**已断线**（`NewHeartbeatState:42`/`ShouldRemind:102`/`ReminderCount:138` 零生产调用者，`HeartbeatInterval` 在 `loop.go:1686` 定义、`:2322` 只写不读）；真正在跑的节律是 `internal/agent/taskheartbeat.go:34/38/44` + `internal/tools/taskheartbeat.go:140 DetectStaleTasks` + `:39 NotifySyntheticEvent`。
   - §5 失败模式：含**静态大 prompt 没有的"漏召回"风险**（原文 `/home/user/work/claude/nick/builtin_memory_search_tool_call.txt:171`–`:176` "代价 3"：静态 prompt 至少"信息都在，只是信号弱"；海马体判错则信息直接消失），逃逸出口=反向请求。
   - §6 Lv-0..Lv-3 表（含 1 次/15min 配额锁）。

2. **`devdoc/hippocampus.md`**（388 行）— gopi 落地映射 SPEC
   - §1 复用表（file:line 逐一锚定）：记忆面 `internal/tools/memory.go:1015 MemorySearch`、`:275 MemoryHit`、`:3040 renderMemorySearchResult`、`:2080 MemoryStore`、`:3100 MemoryHotResident`；分级子循环形状先例 `internal/tools/memory_brew_llm.go:109 brewClient` / `:126 Chat` / `:215 callBrewJSON`、`internal/deepseek/claude_client.go:670 NewClaudeClient`；宿主循环 `internal/agent/loop.go:2406 runEngine`、`:2685` 轮循环、`:2729 TakeJobNotices` 轮边界先例、`:3432 maybeInjectTaskStartSearch`（被动通路，本次要被替换的对象）。
   - §2 新代码 N1–N9（新 `internal/hippocampus` 包、`MemorySearchHits` 结构化出口、调度器侧等级裁决、预算、新会话事件类型、旋钮、默认关闭 golden 回归）。
   - §3 简报进入上下文的唯一合法方式（`promptSections` `loop.go:760` 是静态不可配置的 → 只能走 user/system message）。
   - §4 四级通路落点 + §4.1 SSE 单向约束（Lv-2/3 需多步循环，`loop.go:2685`/`:2729` 已是该状态机）+ **明确禁止复用 `Agent.Interrupt`（`loop.go:2341` 会取消 run ctx = 丢弃整轮，正是原文拒绝的"粗暴硬切断"）**。
   - §5 九个新旋钮（默认关闭/保守）+ 六个复用旋钮，单一读取点。
   - §6 **接口草图 AGREED INTERFACE**：`hippocampus.RecallRequest/Brief/Client/Engine/Budget/Level` + `agent.HippocampusHost{StartOfRun,AtRoundBoundary,EndOfRun}` + `Injection`，附**接线清单表**把每个接线点指派给子任务 2/3/4。
   - §7 机器可判定验收（含默认关闭 golden：开/关两次会话 JSONL 去时间戳后逐字节相等）。
   - §8 诚实未决清单（sync-first；`memory_weight` 算法原文未定义；跨会话投递优先级；`internal/tools` 不得 import `internal/hippocampus`）。

## Not Done

- **未改任何生产代码**（本子任务是 spec-only，符合简报范围）。
- `memory_weight` 的具体算法**没有定论**：nick 原文只提"记忆固有权重"，未给公式 → 已列入 §8 未决问题，留给实现子任务在其 spec 内决定（当前只定义类型与语义）。
- 未执行 Rule 11 的真实会话采纳验证——本子任务不含工具/prompt 改动（那是子任务 4 的验收），故不适用。
- 未运行 `go build`/`go test`：本变更**仅新增两个 .md 文件**，无 Go 代码改动，对构建无影响。（若审查要求，可用 `go build ./...` 复核，预期无变化。）

## Verification

- **锚点机器校验（确定性）**：脚本解析两份文档中全部 `path:line` 与 `Symbol:line` 形式锚点，逐条在 HEAD 树上解析出真实源码行：
  - `path:line` 形式：78 条，**全部解析成功**（修正前 1 条 `builtin_memory_search_tool_call.txt:171` 缺目录前缀 → 已补全路径，见 commit `0ddd502`）。
  - `Symbol:line` 形式：59 条，48 条命中（其余 11 条为正则把 `loop.go:2685` 在 `.go` 处切开的伪命中，非真实失败）。
  - 样例核对：`internal/agent/loop.go:3432` → `func (a *Agent) maybeInjectTaskStartSearch() bool {`；`internal/tools/memory_brew_llm.go:109` → `type brewClient interface {`；`internal/agent/taskheartbeat.go:34` → `taskHeartbeatThrottleMin = 30 * time.Second`；`internal/tools/taskheartbeat.go:140` → `func DetectStaleTasks(...)`。
- **Build**：未运行（无 Go 改动，见 Not Done 说明）。
- **Tests**：未运行（同上；Golden 测试是子任务 3 的交付物，本文档 §7 已给出验收口径）。
- **Commit**：
  - `61aae3a` docs(hippocampus): author the missing spec — five-organs ontology + gopi mapping
  - `0ddd502` docs(ontology): self-contain the nick-source anchors (full path for builtin_memory_search_tool_call.txt)
- **Git 卫生（Rule 8）**：逐文件 `git add`，未使用 `git add -A`；`git status --short` 干净。

## 交付位置声明（Rule 6d）

- **Branch**：`feat/hippocampus-spec`（worktree `~/workspace/gopi-hippocampus-spec`）
- **Recovery**：`git show feat/hippocampus-spec:devdoc/hippocampus.md` / `...:devdoc/agent-five-organs-ontology.md` / `...:devdoc/hippocampus-spec-report.md`
- **Commit 已在主仓**：worktree 与主仓共享 object store 与 refs，无需 push；`git log --oneline feat/hippocampus-spec` 可复核（`61aae3a`、`0ddd502`）。
- 未 merge 自己的分支（Rule 6e）；worktree 保留供子任务 2/3/4 续接。
