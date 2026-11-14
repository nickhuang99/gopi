<!-- doc-class: historical -->
# M3 — brew 蒸馏管道 (memroadmap)

分支: `feat/memroadmap-m3`（worktree `~/workspace/gopi-m3`，基线 621a60e）

## Completed

- **`gopi brew` CLI 子命令**（`cmd/agent/cli_brew.go`）：
  `gopi brew [--dry-run] [--limit N] [--session PATH] [--dir PATH] [--json]`。
  复用 memoryFlagSet 解析；`isBrewSubcommand` 挂入 main.go 的子命令分发链
  （cli_memory.go 模式：匹配即执行并返回 true）。
- **管道本体**（`internal/tools/memory_brew*.go`，4 个文件）：
  1. **切片**：`LoadSessionText` 把 session JSONL 渲染成 transcript
     （user/assistant/thinking/tool_use，注入与 request-stats 行跳过），
     `sliceSession` 按事件边界切块（12K chars − 2.5K prompt reserve，40 events/块）。
  2. **判定**（三条件闸门）：每块一次 flash 批量调用，
     prompt 要求 全部满足才判 lesson（可重复模式/可复现闭环/收益达标），
     JSON 回复 `{"lessons":[{topic,evidence}]}`，上限 3 条/块。
  3. **蒸馏**：每条 lesson 一次 flash 调用生成 QA——question 中英双问法
     （M1 #225 面包屑教训：检索词必须在 question），answer 3-4 句 actionable，
     keywords 8-14 个 5W1H 中英混排（<6 个直接拒绝该行）。
  4. **写库**：`storeBrewLesson` → `MemoryStore(topic="brew: "+topic,
     source="brew")`，FTS 即时可见。
- **防自指 ③**（`brewGuardBlocked`）：两层。
  tag 层：所有 brew 行 sources=brew，未来 refill 一条 WHERE 跳过；
  不覆盖层：写库前重放 MemoryStore 的两个 dedup 键（exact question +
  findSimilarDuplicate 相似度），命中非 brew 行 → 拒写（报 deduped），
  人工/agent 行永远赢；命中 brew 行 → 走 MemoryStore 合并（幂等重跑便宜）。
- **LLM 选择 ④**（端点自适应）：
  默认 `thinking:disabled`（deepseek-v4-flash 必需——reasoning_content 吃光
  max_tokens，memory #185 教训）；GLM glm-5.3-flash 返回 400
  "该模型始终思考，不支持关闭思考" → 客户端自动翻转 effortMode，
  改用 `reasoning_effort=low`（memory #915/#920 变体）。模型名按 Rule 2
  解析（DEEPSEEK_MODEL > settings.json model > BrewModel 常量），
  本部署实测走 bigmodel Anthropic 端点 glm-5.3-flash。
- **成本预算 ④**：session 上限 64/run（`--limit` 可再收紧）、chunk 12K chars、
  每块 ≤3 lessons、每次调用 90s 超时、4096 max_tokens。
- **触发**（session 结束/定期）：一次性任务成功退出后
  `maybeBrewOnSessionExit()`——默认 24h 一次（`GOPI_BREW_INTERVAL_SEC`
  覆盖，0=禁用），状态文件 `<gopi dir>/brew_state.json`，**先 touch 状态文件
  再跑**（崩溃不重试风暴），后台 goroutine 执行，错误只进 stderr。
- **测试**（`internal/tools/memory_brew_test.go`、`cmd/agent/cli_brew_test.go`）：
  渲染/跳过注入事件、切片双上限、三条件判定（空 verdict + 超 3 条截断）、
  蒸馏校验（keywords<6 拒绝、topic 来自判定阶段）、防自指（tag + 不覆盖人工行 +
  幂等 dedup）、端到端 stub 跑通且 **memory_search 可检索**、坏 reply 韧性
  （单块失败不炸 run）、GLM always-thinking 400 识别。

## Not Done（诚实清单）

- **定期触发器（cron/常驻）未做**：当前触发点只有一次性 session 退出。
  chat 模式退出点（`loop.go` SessionLog.Close 处）未挂——那在 agent 包，
  会引入 cmd→agent 依赖反转，留给 M4；`gopi brew` 手动跑随时可用。
- **真实全量 sweep（842 个 session）未跑**：验收抽了 4 个 session 样本
  （成本预算 ④），全量回填是运维动作不是代码任务。
- **分类 lesson_type 细分**（pitfall/success/pattern）未做：实际落库行
  lesson_type 为空串（收尾验收实测），judge prompt 未要求分类字段，M1 列
  已在 schema 里，补一句 prompt + 一个 JSON 字段即可——留作后续迭代。
  （勘误：初稿称"统一写 pattern"，与落库实测不符。）
- **brew 行的 tier/confidence 生命周期**（M2 机器步进）未接：brew 行按
  MemoryStore 默认 warm/0.85 入库，走 M2 通路即可，无需特殊处理。

## Verification

- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK；`go vet ./...` OK
- Tests: `go test ./internal/tools/ -run 'TestLoadSession|TestSlice|TestJudge|TestDistill|TestStoreBrew|TestRunBrew|TestIsAlwaysThinking' -count=1` → 9/9 PASS；
  `go test ./cmd/agent/ -run 'TestIsBrew|TestBrewUsage' -count=1` → PASS
- **真实 session 验收 ⑤**（沙箱 GOPI_HOME + 真实 API glm-5.3-flash，effort=low）：
  - session #1（最大）：1 chunk → gate 判 **无 lesson**（rejected=1）——闸门真的在过滤。
  - session #2-4（次大的三个）：dry-run 全量 JSON → 见下方"验收结果"。
- Commit: 见 `git log feat/memroadmap-m3`（feat(m3): brew 蒸馏管道 …）

## 验收结果（收尾验收 2026-08-30）

- **环境**: 分支 @ cfe590a（effortMode max_tokens 8192 修复后重建二进制），
  GLM glm-5.3-flash（bigmodel Anthropic 端点，effort=low），
  沙箱 `GOPI_HOME=/tmp/gopi-m3-accept-9xFO`（settings.json 自宿主复制，隔离宿主 DB）。
- **命令**: `GOPI_HOME=$SB ./bin/gopi brew --session ~/.gopi/sessions/de883e03-…8608.jsonl --json`
  ——单会话（1.03MB JSONL）、**非 dry-run**、无 --limit。
- **运行结果**: sessions=1，chunks_judged=6，accepted=4，rejected=3，
  stored=4，deduped=0，errors=2，elapsed=**808.3s**（预算 3-8 分钟区间内）。
  2 个 judge 调用 `context deadline exceeded`（brewClientTimeout 90s）——
  单块失败不炸 run，坏块韧性按设计生效；cfe590a 后**无 empty-content**。
- **落库**（沙箱 `.gopi/memory.db`）: 4 行 sources=brew / tier=warm /
  conf=0.85（MemoryStore 默认，走 M2 通路），FTS 即时可见：
  ①read 误报二进制 file+null 字节排查 ②read 误报 binary 验证与兜底读法
  ③grep 无匹配中断 && 链 ④构建失败先查报错文件是否属目标包。
- **检索验证**（`gopi memory_search --db <sandbox>/memory.db`，3 个主题查询）:
  1. "read 工具 误报 binary file 二进制" → #1+#2 双命中 top2；
  2. "grep 无匹配 && 链 中断 set -e" → #3 top1；
  3. "go build 失败 报错文件 目标包 不编译" → #4 top1。
  3/3 查询、4/4 行全部可检索；M2 惰性置信同步生效（eff_conf 0.85→0.86/0.88）。
- **结论**: 收尾计划 ①-④ 通过——真实 API 非 dry-run brew 产出行可检索、
  本节即回填 ③、已按 -f 提交 ④；分支不 merge（PA 处理）。

## 已知坑（给未来 agent）

1. **`.gitignore` 的 `agent` 裸模式吞掉 `cmd/agent/` 下新文件**——
   `git add` 必须 `-f`，否则 commit 静默缺文件。
2. **GLM glm-5.3-flash 不支持 thinking:disabled**（400 code 1210
   "始终思考"）——批量蒸馏代码必须端点自适应：DeepSeek 用 disabled，
   GLM 用 effort=low。硬编码任一种都会在另一半部署上炸。
3. **subcommand 注册模式**：`isXxxSubcommand(args)` 匹配即执行并 return true
   （cli_memory.go 模式）；只返回 bool 不执行 = CLI 静默无输出
   （本次踩过：`gopi brew` 无声退出）。
4. **JSONL 渲染别用多行块**：tool_use input 一行化后必须直接拼，
   中途 WriteString 换行会漏掉尾括号（"(" + 多行 + ")" bug）。
