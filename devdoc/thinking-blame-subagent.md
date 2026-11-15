<!-- doc-class: normative -->
# thinking 默认值修复 + git_blame 工具

日期: 2026-08-27
分支: feat/thinking-blame-subagent (base c526a53)
Commit: d2fd0ed (Task A) + c33cd45 (Task B)

## Task A — thinking 默认值修复 (d2fd0ed)

### 背景
DeepSeek /anthropic 端点在字段省略时默认启用 thinking。gopi 原代码跟随此行为。
长任务实测（deepseek-v4-flash，qwen 测试）发现：thinking 会吃光整个 max_tokens
预算 → 返回空 content。修复目标 = 默认禁用 thinking，保留显式逃生口。

### 设计裁决（PA，读 loop.go 1105-1128 后定）
1. `GOPI_THINKING` / settings "thinking" 显式设置 = 最高优先级
   （enabled 开 / disabled 关；effort=off 冲突时 off 赢，保留 warning）
2. `GOPI_REASONING_EFFORT` 显式设置（env 非空）且非 off 时：若 thinking 未显式设置
   → 自动 thinking enabled（effort 是 thinking 深度旋钮，显式设置隐含思考意图；
   否则 wireEffort 被清空 = 用户设置被静默忽略，坏 UX）
3. 默认（两者均未设置）→ thinking disabled，wireEffort 空

实现要点：
- `internal/config/config.go`: 新增 `ThinkingMode` 类型（"enabled"/"disabled" +
  兼容 bool/on/off/1/0 解析，非法值报错不静默翻转）；`Config.Thinking` 从
  `*bool` 改为 `*ThinkingMode`
- `internal/config/env.go`: `setThinkingMode` settings→env 桥接输出规范字符串
  （"enabled"/"disabled"，非 bool 的 1/off，否则 loop 的 switch 会拒绝）
- `internal/agent/loop.go`: `effortExplicit` 追踪 GOPI_REASONING_EFFORT 的
  **原始 env 存在性**（fallback 前），非法值重置为 false；thinking 解析后
  effort 显式非 off 且未显式设 thinking → enabled
- wireEffort 仅 thinking enabled 时发送；`ReasoningEffort` 内部字段保留解析值
  （默认 minimal）

### 测试
- 新增: TestNewAgent_ThinkingDefaultDisabled / ThinkingEnabledEscapeHatch /
  ThinkingInvalidFallsBackToDisabled
- 修复: TestNewAgent_ReasoningEffortHigh（effort 显式→auto-enable）/
  ReasoningEffortInvalid（断言改 disabled）/ DefaultMinimalWired（wire 无
  reasoning_effort）
- config_test.go 新增 StringForm 解析 + ApplyEnv 传播测试

## Task B — git_blame 工具 (c33cd45)

### 设计来源
doubao 完整设计: ~/work/claude/the operator/gitblame-tool.txt

### 为什么需要专用工具（bash 替代不了）
1. 原始输出杂乱（行号/hash/作者/时间混合——LLM 耗 token 解析噪声）
2. LLM 常传错行号/路径（无参数校验）
3. 缺裁剪（bash 全文件吐出超大文本）
4. 无异常兜底（不存在/未跟踪/越界——原始 stderr）

### 实现（internal/tools/gitblame.go, 676 行含测试）
- `GitBlame(filePath, startLine, endLine)` → 结构化 JSON
  `{file_path, line_items:[{line_no, commit_hash, author, timestamp, commit_message, code_line}]}`
- optional tool: `exec.LookPath("git")` 失败 → 结构化 "unavailable" 错误，不阻塞
- 路径逃逸防护 `resolveInRepo`: repo-root-relative only，绝对路径 / `..` /
  symlink 逃逸（lexical + EvalSymlinks）拒绝
- 行号校验: 1-based、end≥start、clip-to-file；省略范围 → 50 行默认窗口；
  >200 行范围拒绝（无整文件 blame）
- 异常兜底: untracked（ls-files --error-unmatch）/ 缺失 / 越界 / 过宽 → 结构化错误
- 原始 git 输出对 LLM 完全隐藏（内部解析）
- 记忆联动: commit metadata 经 MemoryStore best-effort 存储

### 注册 5 处 (internal/agent/loop.go)
claudeToolDefs（工具定义+描述）→ executeToolDefault dispatch case →
validateToolArgs（file_path 必需）→ prompt header（"20 tools"）→
parallelSafeTools（只读 git 查询）。注意：改动前 claudeToolDefs 实际已有
19 个定义（header 写 18 是旧的），所以加后是 20 不是 19。

### 两个真实 bug 捕获（TDD 过程）
1. **git blame --porcelain 增量 header**: git 在同一 run 中每条后续行前
   重复发射缩短 hash header —— 解析器若在相同 hash 重发射时重置
   author/time/summary 状态会丢失数据。修复: 仅在 hash 真正变化时重置。
2. **TestSystemPromptDensity (4500-char cap)**: 加工具后 prompt 超限，
   压缩 header 措辞 ~106→~95 chars 通过。

## 验证
- `go build ./...` BUILD_OK
- `go test ./...` 全部包 ok（agent/config/tools/deepseek/text2img/checkpoint/gopilog/cmd）
- settings.json 已加 `"thinking": "disabled"`（用户配置生效）

## 遗留/注意事项
- settings.json 是 symlink → .flash_settings.json；**禁止用 patch/edit 工具修改**
  （会把 api_key 替换成 redacted 占位符——第一轮已踩坑）
- 工作区 untracked（agent/memory.db/result.json/text2img*/bugs/*/devdoc/*.md）为
  历史遗留，与本次任务无关，未动
