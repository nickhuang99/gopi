<!-- doc-class: historical -->
# Task Report: memory_feedback 任务开始检索后立即投票 —— 真实测试诊断

> 日期：2026-08-26 · 分支：feat/fb-audit · 方法：代码审查 + 1249 个历史会话全量扫描 + 2 轮真实 API 冒烟

## 结论（一句话）

**功能实现完全正确、端到端可用；`gopi log --context` 查不到是**查询方式用错了**——`--context` 只过滤 `context_trim` 工具，memory_feedback 属于 `--memory` 家族。真正的问题是采纳率极低（历史 302 个 post-feature 会话中仅 6 个调用了 feedback，且全部是烟测/验证类任务，无一有机真实任务）。**

---

## 1. 逐条回答 Epic 的四个假设

| 假设 | 判定 | 证据 |
|---|---|---|
| 功能未正确实现？ | ❌ 否，实现完整 | 工具定义(loop.go:563)、分派(loop.go:2287)、校验(loop.go:2434)、引擎 `tools.MemoryFeedback`(memory.go:2061)、useless+tool_use_id 上下文瘦身(loop.go:1481)、DB 投票落库全链路存在 |
| gopi log 未正确检索？ | ⚠️ 半是 | `--memory` 正确显示；**`--context` 是错的检索方式**（见 §3） |
| LLM 不遵守 system prompt？ | ⚠️ 部分是 | 现场实测 LLM **会**调用（§4），但遵守率仅 ~9%；且 prompt 的"立即反馈"指令弱于 memory_store 的强制钩子 |
| 其他原因？ | ✅ 是 | 采纳率低 = prompt 引导强度不足 + 描述语义侧重 useless + 空结果无投票对象（§5） |

## 2. 历史数据全量扫描（~/.gopi/sessions，1249 个会话）

```
total sessions: 1249
sessions with memory_search:        113
sessions with memory_feedback:        6
sessions whose FIRST tool is memory_search: 104   (task-start 检索习惯本身健康)

按功能发布时间线（2026-08-25 上线）:
pre-2026-08-25 : 947 sessions, ms=44, fb=0   (功能未上线，0 正常)
post-2026-08-25: 302 sessions, ms=69, fb=6   (采纳率 6/69 = 9%)

6 个 feedback 会话全部是特殊任务，无一是日常真实任务:
- ca95a425 heartbeat 研究任务（3 票 useful，A/B 竞争臂）
- f9781df7 / fa8ffc05 / a67d4e1d / f15da5e3  resume A/B 竞争 / 烟测任务
- 2c8e30cd  "Do EXACTLY this sequence" 显式指令烟测
```

**关键对比**：task-start memory_search 采纳率 = 104 个会话首个工具就是 search（习惯已内化）；而 search→feedback 的转化率仅 9%。记忆工具的"软提示 persuasion"只对 search 生效，对 feedback 失效。

## 3. `gopi log` 检索问题的实锤（用户误用 --context）

```go
// cmd/agent/cli_log.go:204-208
var toolFamilies = map[string][]string{
	"memory":  {"memory_search", "memory_store", "memory_feedback", "memory_update", "memory_delete"},
	"context": {"context_trim"},   // ← --context 只匹配 context_trim 一个工具
}
```

现场实测（用真实冒烟会话 eb6cd09c，该会话**明确包含** memory_feedback 调用）：
```
$ gopi log eb6cd09c --context   → "No matching tool calls found in this session."  ← 用户看到的
$ gopi log eb6cd09c --memory    → TOOL 2/3: memory_feedback  input: {"id":1,"rating":"useful",...}
                                    result: Memory feedback recorded (id=1, rating=useful ...)
```
`--context` 的设计意图是 context_trim 活动视图（call_id/replace_with/大小对比），命名上容易被误解为"上下文相关的一切"；memory 家族的入口是 `--memory`（帮助文本 cli_log.go:49 已写明，但用户按字面猜了 --context）。

另外的检索陷阱：**对 ~/.gopi/raw/*.log 直接 grep "memory_feedback" 会得到海量假阳性**——每个 API 请求都把含工具定义的系统提示原样写入 raw log，字符串匹配的是 prompt 文本而非真实调用（实测单个 raw log 41 处，全是系统提示；103410 次总命中同理）。正确检索面是 session JSONL 的 tool_use 事件或 `gopi log <id> --memory`。

## 4. 真实 API 冒烟（隔离环境，绝不污染生产库）

方法：`GOPI_MEMORY_DB=/tmp/fbtest/fb-memory.db GOPI_SETTINGS=... HOME=/tmp/fbtest/homeN` 跑当前 master 二进制，两次同一任务。

**Run1（空库）**：`memory_search` 返回 0 命中 → LLM 正确地**没有**调用 feedback（无记录可投票）→ 任务结束 memory_store（id=1）。✓ 符合设计：空结果不投票。

**Run2（同一库，已种入 id=1）**：
```
tool: memory_search {"query": "Go program main.go print hello string go run verify"}
      → 返回 #1 (tier=warm conf=0.85 ...)
tool: memory_feedback {"id": 1, "rating": "useful", "reason": "Predicted the missing-go.mod failure
      and provided the GO111MODULE=off fix; verified working."}
      → result: Memory feedback recorded (id=1, rating=useful ...): useful=1.00 useless=0.00 fb_score=+0.042
tool: memory_store (id=2)
```
DB 独立复核：`gopi memory_get 1` 显示 `fb=0.67(1)`，`memory_feedback` 表有 `(1, useful, ...)` 行。**LLM 完全会遵守提示词调用 feedback**——端到端闭环成立（search→judge→vote→落库→排序信号）。

## 5. 为什么真实任务中几乎不调用（根因分析）

1. **软提示 vs 强制钩子的落差**：task-start memory_search 有 harness 层强制（`[system] TASK-START CHECKPOINT` 注入，loop.go），而 feedback 纯靠 prompt 说服（描述即策略），无任何代码兜底。搜索被强制了、投票没被强制 → 转化率断裂。
2. **描述语义侧重"坏消息"**：memory_search 描述里 "If a hit is useless ... report via memory_feedback" 的指令语气强于 "A hit you used → memory_feedback(useful)"；useless 场景还带 tool_use_id 瘦身奖励。LLM 倾向只在"矛盾/无用"时投票，而多数任务开始阶段命中记录尚未被实际使用过，无法即时判断 useful → 干脆不投。
3. **工具路由规则 (toolRoutingRules) 没有 feedback 条目**：system prompt 的记忆 if-then 只有 a(search)/b(store)/c(mid-store)/d(矛盾→feedback)，"search 后立即反馈"没有成为第一梯队规则，只在工具描述的细节里。
4. **空结果常见**：陌生主题检索 0 命中时（§4 Run1 就是）无对象可投，天然不触发。
5. **deepseek-v4-flash 的顺从性**：历史 6 个 feedback 会话全发生在显式指令/竞争任务里，说明模型对强指令响应、对弱提示忽略。

## 6. 建议（未实施，仅诊断交付）

- **文档/UX 修复**：`gopi log --help` 中 `--context` 标注 "(context_trim only)"，避免与 memory 混淆；可在 §3 的检索面说明中提示 memory_feedback 用 `--memory`。
- **prompt 加固（若采纳率高是目标）**：toolRoutingRules 记忆 if-then 加一行 `e. after memory_search, vote useful/unused per hit (取→评)`；或在 memory_search 工具描述首句把 useful 投票提前。注意 4500 密度门（TestSystemPromptDensity 守护，需压缩既有块腾字符）。
- **harness 兜底（对称于 search/store 的强制）**：类似 maybeEnforceCompletionStore 的一次性 checkpoint——若本轮 memory_search 返回命中且本任务从未投票，注入"对每条命中投票"提示。这会改变既有测试（R3 完成出口 API call 计数），实施需同步更新。
- **采纳率遥测**：trajectory.go A6 管道加 feedback 采纳统计（memory#9 记忆有现成模式）。

## 7. 验证

- Build: `go build ./...` OK（master f25db05）
- Tests: `go test ./cmd/agent/` OK（本诊断未改代码，无回归）
- 真实测试：2 轮隔离 API 冒烟，session JSONL + DB 双重复核，证据见 §4
- Commit: 见分支 feat/fb-audit（本报告）

## 8. 诚实边界

- 未做代码改动（Epic 要求"find out by real testing"，诊断即交付物；修复建议留待后续任务）
- 未对 6 个历史 feedback 会话逐一审计投票内容质量（只确认存在与上下文）
- 采纳率分母用"post-feature 且含 memory_search"（69），若用"全部 post-feature 会话"（302）为 2%；两种口径都低
