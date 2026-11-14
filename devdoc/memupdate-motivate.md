<!-- doc-class: historical -->
# Task Report: memory_update LLM adoption (Rule 11 — contradiction-scenario rescue)

> Epic: 让 memory_update 被 LLM 真实调用（Rule 11 救活尝试——矛盾场景）
> 分支 feat/memupdate-motivate, base 697f049 (master)

## 结论（摘要）
**救活了。** 通过"矛盾场景"验证，LLM 在 2 个独立场景中真实调用了 memory_update，
共 **4 次正确调用**（均带正确 id + 修正内容，非畸形 keywords 数组）。DB 实际变更已复核。
对比历史 1 次/21435（0.0%）且畸形输入——本次证明 LLM 可被说服在"记忆 vs 操作结果矛盾"时主动修正。

## 改动（只改 prompt 引导，未碰 memory_update 实现逻辑）
全力放在三类 prompt 说服面（按 #1008 效力排序，全部零密度成本）：

1. **taskStartSearchMessage**（loop.go:123，harness 强制注入、每个任务必读）——
   在"search 后立即投票"契约上追加矛盾分支：
   `A hit that your later results CONTRADICT — memory_update it (id=#N + corrected field): feedback says WHAT is wrong, update does the FIX.`
   这是**主动契约**：任务开始就让 LLM 知道"搜到的记忆可能在后续被操作结果推翻 → 用 memory_update 修正"。

2. **memory_update 描述首句**（loop.go:639+）——把路由决策放进第一句：
   `USE at the CONTRADICTION moment: a search hit contradicted by your VERIFIED evidence (operation output)... fix it NOW, don't let the wrong record sit (误导=给未来的自己挖坑)`，
   并加**正确字段形状锚点**（修复历史畸形输入）：
   `id (the #N) + the CORRECTED field(s): rewrite the wrong field's value (topic/question/answer/tier/keywords/confidence), not a patch-note stuffed into keywords.`

3. **memory_feedback 描述的 useless 投票分叉**（loop.go:613）——把"投票即分岔点"加大：
   在 `feedback says WHAT is wrong, update does the FIX` 后追加
   `(右转: call memory_update with id + the corrected field RIGHT NOW, not later)`。

> 密度预算：三个改动全在 claudeToolDefs 描述 + taskStartSearchMessage（都不进 assembleSystemPrompt），
> 密度不变 4490/4500。toolRoutingRules 的记忆 block d 已含 `+memory_update`，未改（无余量）。

## 验证（Rule 11 —— 真实会话证明）
方法：HOME 隔离 + scratch DB + 预种过时记录 → 自然语言任务（绝不含工具名）→
python 解析 **session JSONL 的 tool_use 事件**（raw log grep 是假阳性——系统提示含工具名）→
`gopi memory_get` 复核 DB 实际变更。

### 场景 A：上下文压缩阈值矛盾（session `fb661ce6`）
- 预种：memory #1 "compact trigger = 0.9"（实际 `DefaultCompactTriggerRatio = 0.75`，context.go:33）
- 预种：memory #2 "compact target = 0.5"（实际 `DefaultCompactTargetRatio = 0.35`，compact.go:38）
- 任务："查 DefaultCompactTriggerRatio 确切数值，以代码为准"
- LLM：search → read context.go → 发现 0.9 vs 0.75 矛盾 → feedback(useless) → **memory_update id=1 answer→0.75** + **memory_update id=2 answer→0.35**
- DB 复核：id=1 变 `0.75`(tier=hot conf=0.95), id=2 变 `0.35`。✅ 2 次正确调用

### 场景 B：密度预算 + 二进制体积矛盾（session `2b675bd7`）
- 预种：memory #1 "density budget = 4000"（实际 TestSystemPromptDensity 断言 <4500）
- 预种：memory #2 "binary ≈ 40MB"（实际 `ls -lh bin/gopi` = 17MB/17,513,948B）
- 任务："核实两项约束，以实际依据/代码/文件系统为准"
- LLM：search → read prompt_test.go + ls bin/gopi → 发现两个矛盾 → feedback(useless)×2 → **memory_update id=1 answer→4500** + **memory_update id=2 answer→17MB**
- DB 复核：两条皆已修正。✅ 2 次正确调用

### 反证（排除假阳性）
失败试运行（session `8f7980b1`，CWD 错入 tmp-val 找不到文件）**没**调用 memory_update——
证明 JSONL 捕获的是真实 tool_use，非系统提示文案噪音。

## 成功方法 / 失败原因分析（决策依据）
**成功的关键**（与 memory_feedback 修复同构）：
1. **矛盾契约前置到 harness 强制的 taskStartSearchMessage** —— 任务开始就种下"搜到的记忆可能错"的心智，且该注入 LLM 必读（#1008 最强杠杆）。
2. **矛盾是 reactive 的，但契约可 proactive 播种** —— 真正触发在 mid-task（读代码发现 0.9≠0.75），但"遇到矛盾要修正"的指令在 task-start 就位，LLM 全程带着这个命令。
3. **feedback(useless) 是精确分岔点** —— 票=发现问题，紧接着的"右转 call memory_update"把修正动作焊在同一轮上下文里。
4. **正确字段形状锚点**直接挡掉历史 畸形输入。

**若失败的分歧点**（本次未发生，但值得记录）：若 LLM 只 feedback(useless) 而不再 memory_update
（常见短路），则需更强——把"修正动作必须在下一调用完成"写成硬命令而非提示；
或 checkpoint 层在 feedback(useless) 落库后注入一条 nudge 消息。

## 验证
- Build: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ✅
- Tests: 见下方全量 go test ./...
- Commit: <hash> feat(prompt): memory_update contradiction-scenario adoption
- 隔离: 验证产物在 tmp-val/（不入库，留 PA 复核）

## 证据
- session JSONL: tmp-val/home/.gopi/sessions/fb661ce6*.jsonl, tmp-val/home-s2b/.gopi/sessions/2b675bd7*.jsonl
- scratch DB: tmp-val/scratch.db, tmp-val/scratch2.db（已含修正后记录）
