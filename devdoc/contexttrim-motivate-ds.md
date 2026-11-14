<!-- doc-class: historical -->
# Task Report: context_trim LLM adoption via reactive dead-end nudge

Branch: feat/contexttrim-motivate-ds
Date: 2026-08-28
Channel: DeepSeek (deepseek-v4-flash, .flash_settings.json)

## Problem
context_trim (dead-end 死胡同 trimming) 全量审计 4次/21435 (0.0%)——LLM 几乎不用。让 LLM 在死胡同场景稳定调用，需改 prompt 引导（不碰工具实现逻辑）。

## Method（三层迭代，只有第三层成功）
纯 prompt 逐层增强，每层都用真实任务冒烟验证，记录到"哪一层才让 flash 真正调用"：

1. **工具 Description 加命令锚点**（claudeToolDefs，零密度成本）
   - 首句 `TRIM DEAD-ENDS NOW` + 具体死胡同例子（missing file / wrong args / nonexistent path）＋ `ALWAYS trim before moving on; NEVER leave an untrimmed dead-end`。
   - **无效**：真实任务（12s / 9 轮跑完）LLM 零调用 —— deepseek-v4-flash 无视工具描述。

2. **taskStartSearchMessage 预埋死胡同指令**（loop.go:119，最强说服面）
   - 加一句 "During the task: the moment a tool result is a dead-end ... call context_trim with that result's tool_use id immediately"。
   - **无效**：LLM 读到预埋但死胡同出现在 6+ 轮之后，预埋被遗忘。附带确认了 task-start 预埋对 memory_feedback 有效（同消息里 feedback 被投了票）——但 context_trim 触发时机是任务中途，预埋覆盖不到。

3. **[成功] 死胡同时刻的 reactive prompt nudge**（loop.go 结果循环）
   - 新增 `deadEndTrimMessage`：结果循环里用 `resultsHaveDeadEnd` + `deadEndMarkers` 识别死胡同结果。"exit=" 只认非零（避开 exit=0 假阳性）；其余按错误子串（Error reading/writing/editing, Traceback, ModuleNotFoundError, Exception, no such file, failed, [stderr], Memory/Debug error, ...）。
   - 一旦本批次有死胡同结果且 `deadEndNudged` 未设，`Ctx.AddUserMessage(deadEndTrimMessage)` 一次并 latch（每 run 至多一次，防刷屏）。Trim 仍是 LLM 自愿调用（会话 tool_use 日志证明），nudge 只是此刻的提示。
   - 死胡同消息正文含可复制的 call_id 锚点：`call_id is the '[tool_use id: …]' stamped on that result, replace_with = a one-line conclusion`。

## Routing line 增强 + 密度压缩
- context_trim 路由行：`dead-end/errored/no-value result → trim NOW + leave one-line conclusion; copy header tool_use id verbatim`.
- 为给该行腾空间（系统提示密度 <4500 守卫，TestSystemPromptDensity），压缩了 write/equations/code_search/keyword_search/bash/job 等行 +~130 字符；密度 4490→4479（守卫全绿）。
- 新增守卫测试：`TestSystemPrompt_ToolRoutingAuditFixes` 加 context_trim 断言；`TestContextTrimToolSchema` 加 TRIM DEAD-ENDS NOW / ALWAYS trim 断言；`TestTaskStartCheckpointMessage_DeadEndTrimAnchor`；`TestResultsHaveDeadEnd`；`TestDeadEndTrimMessage_hasCallIdGuidance`。

## Verification（Rule 11：真实会话 + tool_use 记录）
隔离 HOME（tmp-val/home）+ 独立 GOPI_MEMORY_DB + 任务文本不含 context_trim 字样。查 session JSONL 的 assistant tool_use 事件（name=context_trim）。注意 raw log grep "context_trim" 是假阳性（工具定义嵌在每个请求的 system prompt，memory #876）。

### 最终复跑（2026-08-28，全新会话 fresh evidence，deepseek-v4-flash）
复跑前丢失了场景 A 的 session 文件（c552aaac 不在 tmp-val/home/.gopi/sessions/），用 `run_a2.sh` / `run_b2.sh`（HOME=tmp-val/home-a / home-b 隔离 + GOPI_MEMORY_DB=tmp-val/memory-b2.db）全新复跑，两个场景同一二进制（bin/gopi, CGO_ENABLED=0）：

**场景 A** — bulk_fail.py（16KB 诊断噪音 + import 缺失模块 traceback）：
- session `a01370f1-5fa0-4c65-969e-26f0575935c3`（tmp-val/home-a/.gopi/sessions/）
- context_trim tool_use id=`call_928f9033b459413e9ab986a9`
- args = `{call_id: "call_0e66b3849ea34b42b2721479", replace_with: "bulk_fail.py 失败 exit=1: 打印海量 [trace]/heap 诊断日志后尝试 import 不存在的 nonexistent_metrics_engine → ModuleNotFoundError，无 REPORT READY。"}`
- harness 回执（session JSONL 内）：`context_trim: replaced result of call call_0e66b3849ea34b42b2721479` — trim 真实执行
- 附加证据：该会话的 memory_store 答案自发包含"大输出结果要 context_trim"（习惯内化）

**场景 B** — countloc.py 不存在路径（python3 打开缺失文件报错）：
- session `394bc516-09e1-44ad-970a-c0b5a5b73153`（tmp-val/home-b/.gopi/sessions/）
- **2 次** context_trim：
  1. id=`call_65f2d697c09c4b0599375751`, args=`{call_id: "call_b284ef06d2c443ab96d48b4b", replace_with: "countloc.py failed: /home/user/workspace/gopi-ct-ds/lib/countloc.py does not exist (Errno 2) — Epic says record reason & skip; no retry"}`
  2. id=`call_b93dbb2161374723ba49a99b`, args=`{call_id: "call_9a9d06cbfe244943840553c6", replace_with: "Verified: cwd=.../tmp-val, repo root gopi-ct-ds exists, but countloc.py is nowhere in the project — path stale, no retry needed"}`
- harness 回执确认两个 trim 均真实执行（replaced result of call call_b284ef06… / call_9a9d06cb…）

**合计 3 次 context_trim / 2 个独立场景，call_id 全部逐字复制自死胡同结果的 tool_use id stamp（零幻觉），replace_with 均为一句结论。达标（Rule 11：≥2 独立场景调用）。**

### 历史首轮验证（上一会话，场景 B 原始 session 仍在 tmp-val/home/）
- session `1aa468b7-aa5c-4353-a39f-759ba7a93d02`：context_trim id=`call_00_CezUGmUNHgCL3ltEyiZ82831`, args=`{call_id: "call_01_iyqwn2atvcNTqK0HNI6f1548", replace_with: "countloc.py not at .../lib/countloc.py — doesn't exist; repo root listed."}`（场景 A 的 session 文件已丢失，故复跑覆盖）

**场景 A** — bulk_fail.py（先打 16KB 诊断噪音再 import 缺失模块报 traceback 的脚本）：
- session `c552aaac-7bfa-4b69-ad5f-e674427a18eb`
- context_trim tool_use id=`call_00_6APlZwsvt6gg4BzPkuHL7210`
- args = `{call_id: "call_00_9Z3mjxc5gonCMUfJtJhU1247", replace_with: "bulk_fail.py ran from tmp-val/fixtures/tools/; exit=1: explode() line 16 import nonexistent_metrics_engine → ModuleNotFoundError. No REPORT READY printed. Confirmed deliberate-failure fixture."}`
- harness 回执：`context_trim: replaced result of call call_00_9Z3mjxc5gonCMUfJtJhU1247 with conclusion (pair kept, roles unchanged)`

**场景 B** — countloc.py 不存在路径（python3 打开缺失文件报错）：
- session `1aa468b7-aa5c-4353-a39f-759ba7a93d02`
- context_trim tool_use id=`call_00_CezUGmUNHgCL3ltEyiZ82831`
- args = `{call_id: "call_01_iyqwn2atvcNTqK0HNI6f1548", replace_with: "countloc.py not at /home/user/workspace/gopi-ct-ds/lib/countloc.py — doesn't exist; repo root listed."}`
- harness 回执：`context_trim: replaced result of call call_01_iyqwn2atvcNTqK0HNI6f1548 with conclusion (pair kept, roles unchanged)`

两个独立场景，call_id 均来自死胡同结果的真实 tool_use id（逐字复制，零幻觉），replace_with 为一句结论——上下文交接处 bulky 死胡同被 84/72 字符结论替换。达标（Rule 11：≥2 独立场景调用）。

## 陷阱（迭代中踩坑）
- read 一个不存在路径 → LLM 会 RETRY 反复读，卡成循环，不 trim。
- bash 里 LLM 爱用 `;` 把失败命令和成功命令串进同一个 bash call → 结果 exit=0（死胡同被合并掉），没有可 trim 的死胡同结果。正确场景 = 让单个命令独立失败（如跑会报 traceback 的 python 脚本）。

## 交付
- go build: OK (CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)
- go test ./...: exit 0 全绿 — cmd/agent 5.388s, internal/agent 20.726s, 其余 ok/cached（36s 总时长）
- 定向守卫测试: TestResultsHaveDeadEnd / TestDeadEndTrimMessage_hasCallIdGuidance / TestTaskStartCheckpointMessage_DeadEndTrimAnchor / TestSystemPrompt_ToolRoutingAuditFixes / TestSystemPromptDensity — 全 PASS
- commit: 见 git log（feat/contexttrim-motivate-ds）
- 验证证据目录: tmp-val/（不入库）— sessions 在 tmp-val/home-a|home-b/.gopi/sessions/，任务文本 scenario_a|b.txt，运行日志 scenario_a2|b2.run.log，verify_ct.py 复核脚本
