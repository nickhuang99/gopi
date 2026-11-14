<!-- doc-class: historical -->
# FEATURE-VERIFY-MATRIX — 8/25 至今 13 项新功能实测验证

> 日期：2026-08-27 · 方法：raw log（wire 真相）+ session JSONL（权威 tool_use）+ session_start rawLogPath 精确配对
> 样本：8 个真实 session（21:12–04:33）+ 全库 1358 session 扫描；gopi binary 实重建（go build ./cmd/agent）
> 结论：**12/13 ✅ 工作，1/13 ⚠️ 部分（#3 成对删除未实战）**

## 汇总表

| # | 功能 | A 代码层 | B 调用层 | C 生效层 | 结论 |
|---|------|---------|---------|---------|------|
| 1 | memory_feedback | loop.go:575 + memory.go:2073 | 3 会话调用 | DB 34 票入库 | ✅ |
| 2 | memory_update | loop.go:602 + memory.go:1863 | 1 次真实（8/25） | record#1 keywords 已改 | ✅ 调用稀少 |
| 3 | search 立即反馈+成对删除 | loop.go:1590 | 反馈有/删除无 | 无实战 wire 证据 | ⚠️ 部分 |
| 4 | prompt 记忆规则 | loop.go:118 + sysprompt | 8/8 首轮 search | sysprompt wire 注入 | ✅ |
| 5 | context_trim | context.go:160 | 5f69077f ×1 | 99159B→128B 替换持续 | ✅ |
| 6 | thinking-trim | context.go:254 + claude_client.go:122 | 全会话 | 占位块/空 thinking | ✅（已纳入） |
| 7 | clock tick | loop.go:303 | 全会话 | [t+NNNs] 全部结果 | ✅ |
| 8 | resume | cli_resume.go + checkpoint/ | 活验 demo | kill→resume 98.5% cache | ✅ |
| 9 | vision_analyze | vision.go:116 | aa62488d ×3 | OCR 文本结果 | ✅ |
| 10 | native image input | main.go:97 auto-switch | ceeb7248 | wire image block + vision model | ✅ |
| 11 | gopi log 子命令 | --raw/--vision/--trim/--grep/--last | 实测全通 | 输出正确 | ✅ |
| 12 | task-end store 修复 | loop.go:100 + 8e3ac0c | 5/8 末轮 store | terse 后仍触发 | ✅ |
| 13 | session_start wire model | session.go:81 + 77ec70b | ceeb7248 | model=vision-exp 一致 | ✅ |

---

## 逐项证据

### 1. memory_feedback — ✅ 工作
- **A**: `internal/agent/loop.go:575` 工具定义（id/rating/reason/tool_use_id 参数）；`internal/tools/memory.go:2073` MemoryFeedback（append-only + EMA + 会话去重）；检索端 `memory.go:773` `s += fbScore(h.FbUseful, h.FbUseless)` 参与排序（Beta 后验，memory.go:2024）。
- **B**: 最近 8 真实 session 中 3 次调用：11b3012b `{"id":894,"rating":"useful","reason":"gopi-empty-thinking placeholder + raw log parsing recipe..."}`、741a5fe4 `{"id":217,"rating":"useful",...cli_log.go structure...}`、aa62488d `{"id":892,"rating":"useful","reason":"确认了 vision_analyze 用法..."}`。
- **C**: DB 34 条投票（`SELECT COUNT(*) FROM memory_feedback` = 34）；12 个不同 record 有票；id=34 (894/useful 21:57:39)、33 (3/useless smoke)、30 (892/useful 21:13:04) 时间戳与 session 吻合。record #3 被 23 条 useless "smoke test" 刷票（测试残留，非污染——但值得关注）。

### 2. memory_update — ✅ 工作（调用稀少）
- **A**: `loop.go:602` LLM 工具 + `memory.go:1863` MemoryUpdate（同 CLI 引擎）；`loop.go:2416` dispatch。
- **B**: 全库仅 **1 次真实调用**（e54d887c，8/25 09:44，`{"id":1,"keywords":["correct-timeout","unlimited"]}`）；最近 20 session 0 次——未被调用（区别于未生效）。
- **C**: DB 生效——record#1 search_kw 现含 `correct-timeout, unlimited`（原 `outdated-5min`）。

### 3. memory_search 立即反馈 + 成对删除 — ⚠️ 部分
- **A**: `loop.go:1590-1608`：仅 `rating=useless && tool_use_id≠""` 触发 `RemoveToolPair`；单测 `search_remove_test.go` 覆盖。
- **B**: 反馈环节工作——search 后 LLM 有反馈（aa62488d pos5、11b3012b pos31 均紧跟 search 相关）。但全部真实投票为 **useful（无 tool_use_id）**；useless+tool_use_id 的调用仅存在于 3 个 smoke-test session（2c8e30cd/a67d4e1d/f15da5e3，均无 rawLogPath，纯测试）。
- **C**: **无真实 wire 成对删除证据**。所有 useless 投票都是 smoke test → RemoveToolPair 真实路径从未在实战触发。功能已实现（有测试），属"实现存在但最近 session 未实战"。

### 4. prompt 记忆规则（task-start search / task-end store 强制）— ✅ 工作
- **A**: `loop.go:118` taskStartSearchMessage + `loop.go:100` completionCheckpointMessage；注入点 `loop.go:1244`（maybeInjectTaskStartSearch）+ 8e3ac0c 修复 terse 绕过；system prompt MEMORY 规则 a-d（memory_search FIRST / task-end store / mid-task store NOW / 判每 hit）。
- **B**: 8/8 真实 session `first_tool=memory_search`（首轮即 search，强制注入+LLM 遵守）；5/8 以 `memory_store` 为末轮 tool。
- **C**: wire 首请求 system 数组含完整 MEMORY 规则文本（`[TOOL ROUTING — ...] - MEMORY — 你的记忆=跨任务竞争力 (if-then): a. start each task with memory_search FIRST...`）。

### 5. context_trim — ✅ 工作
- **A**: `internal/agent/context.go:160` RemoveToolPair（remove/replace 两路径）+ loop.go 工具定义。
- **B**: 5f69077f 1 次真实调用：`{"call_id":"call_00_ET_ZkUXAAJ56oX71sNOTTjC7558","replace_with":"grep -rn \"gopi-raw-20260826-212601\" matched the current session's own raw log ... — dead end, ignore."}`。
- **C**: **wire 铁证**：该 tool_result 在 req21 为 **99,159B**（[t+111s] 开头 + 102KB 截断），req22 起恒为 **128B 结论文本**，一直持续到 req55+（会话全程后半程保持替换）。gopi log --context-trim 实测渲染：`102562 → 130 replaced with 130B conclusion`。

### 6. thinking-trim — ✅ 工作（纳入已有验证）
- **A**: `context.go:254` WireMessages(keepThinking) 剥离；`claude_client.go:122` `EmptyThinkingSignature = "gopi-empty-thinking"`；占位规则（thinking enabled + 无 thinking → 1-char 占位）。
- **B/C**: 本批 wire 复测一致——11b3012b 25 请求 300 个 empty-thinking 占位、5f69077f 86 请求 3608、741a5fe4 79 请求 3080；所有请求 `thinking.type=enabled` 且无真实文本块。已有 devdoc/thinking-trim-verify-lastlogs.md：99.8–99.94% 剥离（keep 75,493 vs 152 chars / 296,075 vs 190）。

### 7. clock tick — ✅ 工作
- **A**: `loop.go:303` tickClockRule（system prompt 注入）；`loop.go:814` SessionClock + withTick（1560/1575：`[t+NNNs] [tool_use id: X]\n<payload>` 单份组装——892a60a 修复过双份）。
- **C**: wire 每条 tool result 均带 `[t+NNNs]`：11b3012b **695** 个、741a5fe4 **5317**、5f69077f **4087**、ceeb7248 **152**。tool_result 内容形如 `[t+21s] [tool_use id: call_00_DSxW...]\n#893 gopi native image input verification...`。

### 8. resume — ✅ 工作
- **A**: `cmd/agent/cli_resume.go`（runResume/resolveResumeLogPath）+ `internal/checkpoint/` + `internal/agent/resume.go`；`gopi resume/checkpoint --help` 实测正常。
- **B**: 7 个 session 引用 resume 命令；130c5d74 / 1e6a89e6 为 8/26 07:50 活验 demo session。
- **C**: devdoc/resume-impl-report.md 活验记录：`timeout 7` 中断 → `gopi resume <rawlog> "finish steps 3-5"` → 完成 3-5 不重做 step 1（4 rounds/4.9s），**prefix cache 98.5% hit**（25856/26263 tokens）——字节精确 checkpoint 重放确认。

### 9. vision_analyze — ✅ 工作
- **A**: `internal/tools/vision.go:116` VisionAnalyze（LoadImage + deepseek vision client，独立端点）；loop.go dispatch。
- **B**: aa62488d 3 次真实调用（`/tmp/context_trim_bug.png`）。
- **C**: OCR 结果完整落地：`【vision 分析结果】(模型 deepseek-v4-flash-vision-exp, 输入 207 tokens / 输出 2048 tokens, 图片 23535 bytes)` + 逐字文本提取；第 3 次调用输出 426 tokens 的真实 OCR 文字内容（中文拼音文本 + 模糊度诚实声明）。且触发 memory_feedback 联动（id=892 useful 入库）。

### 10. native image input — ✅ 工作
- **A**: `cmd/agent/main.go:90-109`：image input → 无显式 --model 时 auto-switch 到 vision model（`[gopi] image input detected → using vision model`）。
- **B**: ceeb7248 session（21:27，"verify native image feature"）。
- **C**: **wire 铁证**——req0 的 user 消息含 `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo..."}}` 块；18/18 请求 model=`deepseek-v4-flash-vision-exp`；每请求 history 首条均重发该 image block（vision 模型全程直读像素，非 OCR 中转）。

### 11. gopi log 子命令（--vision/--trim/--raw/--grep/--last）— ✅ 工作
- **A**: 全部 flag 在 `gopi log --help`（--raw 字节级 raw log、--vision、--context-trim/--trim 家族、--grep、--last N、--memory/--search/--web/--job 家族）。
- **B/C 实测**（go build ./cmd/agent 重建 binary）：
  - `log <id> --vision` → 3 条 vision_analyze 调用（tool_use_id/input/result 齐全）
  - `log <id> --context-trim` → 表格渲染 `CONTEXT_TRIM CALL ... bash call_00_ET_ZkUX... replaced 102562 → 130`
  - `log <id> --grep context_trim` → 9 条匹配条目
  - `log --last 3` → 最近 3 session 汇总（含 token/耗时/模型）
  - `log <id> --raw` → 6320 行 wire 原文（→ REQUEST / ← CHUNK / ← RESPONSE）

### 12. task-end store 修复（terse 绕过）— ✅ 工作
- **A**: `loop.go:100` completionCheckpointMessage + commit 8e3ac0c（"task-end memory_store no longer bypassed by terse completed answers"——R3 分支的 ≥40-char 要求绕过修复）。
- **B**: 8 真实 session 中 5 个末轮 tool 为 memory_store（11b3012b/ceeb7248/a037b7fd/4a5fa384 + 741a5fe4/5f69077f 在最终文本后仍触发）。
- **C**: 741a5fe4 序列末尾：`bash → text("All done — branch feat/log-rawlog...") → memory_store → memory_feedback → text(final)`；5f69077f：`bash → text → memory_store → todo → text(final)`——**最终答案前的 terse/非 terse 文本后 store 仍触发**，修复生效。

### 13. session_start wire model — ✅ 工作
- **A**: commit 77ec70b（"session_start records the wire model for image auto-switch"）+ `session.go:81` model 字段（NewAgent 写入；修复前在 auto-switch 前记录错误模型）。
- **B/C**: ceeb7248 的 session_start `"model":"deepseek-v4-flash-vision-exp"` 与 wire 18/18 请求 model 完全一致——image auto-switch 后 session 记录的是 wire 真模型。`gopi log --last` 渲染也正确显示 vision-exp。

---

## 关键区分

- **实现存在但最近 session 未调用**：#2 memory_update（1 次真实、20 session 内 0 次）、#3 成对删除路径（仅 smoke test 触发）。
- **完全生效**：#1/#4/#5/#6/#7/#8/#9/#10/#11/#12/#13。
- **唯一 ⚠️**：#3——反馈环节真实工作（3 次 useful 投票全入库），但成对删除的 useless 路径缺实战（LLM 倾向投 useful，prompt 已引导 useless 方向——commit 0ba3d0b 加强；根因是最近 session 没有真正撞见"contradicted/stale"的记忆）。

## 方法备注

- 配对：session_start.rawLogPath 精确指向 raw log（非时间戳猜测），8/8 命中。
- raw 解析：marker 驱动（`^→ REQUEST$` 整行匹配），正文含 "→ REQUEST" 字符串会污染 grep——必须 anchor。
- JSONL tool_result 内容在 `message.content[].content`（非顶层 content）。
- 样本横跨 21:12–21:57 与 04:33 两批；741a5fe4（18:08）为 --raw 功能开发会话。

## 附：近期 commit 映射

| 功能 | 关键 commit |
|------|------------|
| clock tick | 2a72d23 |
| memory_feedback | 69bdc1d/cf63513/682fadc |
| memory_update | bcbfd63 |
| search-remove | 82cb805/0ba3d0b |
| task-start/end 强制 | 75728bb/9e3fe35/8e3ac0c |
| context_trim | eee6daa/ccdca44/892a60a |
| thinking-trim | d734963/c01bc07 |
| resume | 6690483/7f51643/cffba0c/9517e88 |
| vision | 8a9afac/6fb876f/6d86b78 |
| native image + wire model | 6d86b78/77ec70b |
| gopi log 家族 | 284393a/209f97f/dc2d3a4/d3d0656/41c8619 |
