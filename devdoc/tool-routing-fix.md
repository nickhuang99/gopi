<!-- doc-class: historical -->
# Task Report: tool-routing prompt 改进（基于工具使用审计的 5 条建议）

Commit 分支：`feat/tool-routing-fix`（worktree `~/workspace/gopi-tool-routing`）
日期：2026-08-27

## Background（工具使用审计根因）
全量 1408 会话 / 21435 次调用审计（devdoc/tool-usage-audit.md，memory #995）发现：
git_blame 0 次；code_search 0.3%（被 bash-grep 淹没）；memory_feedback 46% 在会话尾部
（>0.9 相对位置）；context_trim 4 次 / memory_update 1 次。

## 改动清单（4 条，全在 loop.go 引导文本层，不动工具实现逻辑）

### 1. [高] git_blame 路由行（toolRoutingRules）
- 新增：`- git_blame: git file line-history — "谁写的这行 / 哪次 commit 引入 bug" 用 git_blame(file_path[,start,end])，not bash git blame.`
- 机理：此前唯一 git 路由是 bash 行 "Use bash for ... git"，prompt 主动把 git blame 推给
  bash（审计证据：9 个 DS 会话用 `bash git blame -L ... | awk/sed`）。现在直接指给工具。

### 2. [高] memory_feedback 时序（toolRoutingRules · 块 d）
- 旧：`d. hit useful → memory_feedback(useful); contradicts → ...(useless)...`
- 新：`d. vote memory_feedback(useful/unused) IMMEDIATELY after each memory_search result — never defer to task end; contradicts → memory_feedback(useless)+memory_update (陈旧=失去裁判).`
- 机理：审计 46% 反馈在会话尾部=陈旧信号。现在要求紧跟检索即时投票。
- 注意：保留 "memory_feedback(useless)" 子串——TestSystemPromptDensity 断言它存在。

### 3. [中] code_search 对比锚点（toolRoutingRules · code_search 行内合并）
- 在 code_search 行内合并：`returns every definition in ONE structured call; bash grep = raw lines you re-read+filter (2-call trip)`
- 机理：135/155 会话无视 routing 行用 `bash grep -rn "func X"`——锚点把"一次结构化调用 vs
  grep 两次往返"的成本差距摆到首句。
- 注：保留了原 "Call code_search FIRST with the bare identifier" ——TestSystemPrompt_CounterExampleRoutingRules 断言。

### 4. [中] bash 文件读取禁例（claudeToolDefs · bash 工具描述）
- 追加：`Reading a file (cat/head/sed -n FILE) when read exists = a logged violation.`
- 位置在工具描述（不参与 assembleSystemPrompt 的密度统计），零密度成本。
- 机理：GLM 垃圾场样本（eb10f10b，142 bash 含 115 cat/head + 95 grep）把 bash 当文件读取器。

## 密度预算使用情况
- 目标：assembled system prompt ≤4500 chars（TestSystemPromptDensity 门禁）。
- 改动前：4467 chars（余量仅 33）。
- 4 条新增净增 ~328 chars（item1 +122, item2 d-line +~49, item3 +~85, item4 在描述侧 0）。
- **压缩腾位 ~320 chars**（保留所有 guard-marker 前提下）：
  - EFFICIENCY #3 "NEVER sleep or poll" 删（与 JOB #2 "Never busy-poll" 重复）−55
  - header "incl. ... and" → ":" −7
  - engineering 各规则去掉冗余括号（mirror-swap、IoU-PIL、symptom 链、z.txt 例、
    (stdlib first; external last)、go test 前缀）−~190
  - job #1 "timeout_sec"→"timeout" −4
  - taskManagement #7 "the todo tool"→"todo" −4
  - conciseThinking #2 去尾 −11
  - code_search 行去掉重复的 (file:line,kind,signature) 细节 −27
- 改动后：**4492 chars**（余量 8，安全）。
- 注：每处压缩都对照 guard 测试的子串断言逐一核验（BASH #2 "Never cat a large file whole"
  是 guard-tested，**必须保留**——曾误删导致 TestSystemPrompt_IncludesBashEfficiencyRules 红）。

## 验证
- `go build ./...`：OK
- `go test -count=1 ./...`：9 包全绿（internal/agent 16s，internal/tools 88s）
- 所有既有 prompt guard 测试通过，含两处被本次改动触发的复测后转绿：
  - TestSystemPrompt_IncludesBashEfficiencyRules（未删 BASH #2）
  - TestSystemPrompt_CounterExampleRoutingRules（保留 "Call code_search FIRST..."）

## 未做（诚实说明）
- 运行时"hint 注入"（bash 调用后自动提示用 code_search/read）未实现——任务判定纯措辞即可，
  且注入属行为层，超出 prompt 工程范围。item 4 采用纯工具描述措辞。
- 4 条全部保留，无需按收益排序删减（靠压缩达标）。

## Commit 清单
（见 stdout——按条目分组提交）
