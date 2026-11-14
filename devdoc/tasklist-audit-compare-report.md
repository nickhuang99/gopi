<!-- doc-class: historical -->
# 评审对比报告：DS(deepseek-v4-flash) vs GLM(glm-5.3-flash) — "task list 不发比例 >30%"审计

**评审 agent 独立复核日期：** 2026-08-28（本会话）
**评审对象：**
- DS 侧：`feat/tasklist-audit` @ `114d1d8` → `devdoc/tasklist-audit-report.md`（worklog `0a2a237c-5495-49ea-916e-369ebdbc7eb8.jsonl`）
- GLM 侧：`feat/tasklist-audit-v2` @ `5a48e18` → `devdoc/tasklist-audit-v2-report.md`（worklog `f5a23393-c6fb-4ee8-a6ee-06ee7ddc4742.jsonl`）

所有评审维度均以**独立工具调用复核**为证据（非空泛评语）。本评论反复核了两套报告的每一个关键数字、每一条引用的 session id 与 file:line。

---

## 1. 结论正确性 — 两份报告的核心数字能否互相印证？(抽验 ≥5 session + role-aware 独立解析)

### 1a. 两份报告的核心数字

| 指标 | DS 报告 | GLM 报告 | 本评审独立复核 |
|---|---|---|---|
| 扫描范围 | 595 relevant / 523 real-work | 50 recent / 43 real-work | 两套脚本均重跑 |
| real-work no-list 率 | 4-17% 分层（last-50 ≈ 4-6%, all-time ≈ 17%）| 3/43 ≈ **7%**（last-50）| **印证** |
| 首轮发清单比例 | 2% | 约 2%（引用前审计）| 印证（本复核 turn0≈0-2%）|
| 清单在≥1工具轮后才发 | 93% | 中位 first-emission @ msg~5 | 印证 |
| BEGIN-no-END 静默丢弃 | 10 sessions / 2% | 复现于 68827372 | **印证（真实 bug）** |
| 清单仅在末尾一次性发出 | 未识别 | 8/40 (20%)（仅一次清单且在末条）| **独立复核 9/42=21%，语义一致** |

**结论：两份报告的核心"no-list 真实比例"完全互相印证** —— 都在个位数（last-50: 6-7%），all-time ~17-18%，**远低于 30%**。两者的外层量级、方向、根因判断（R2 松弛回归 + #559 静默丢弃）高度一致。分歧不在此。

### 1b. 独立抽验（role-aware 解析——只数 assistant-role text 块，排除 user Epic 与散文引用假阳性）

两份报告的均强调该坑，我同样用 role-aware 解析复核。抽验 session（本评审逐条跑 parser）：

| session | real-work | lists | 判定 |
|---|---|---|---|
| `6613fe97`（DS 引用）| ✓ 多文本多工具 | BEGIN 多次但 **全无 END + 含 "pending"** | **DS 的 instigator 证据真实**：确认 5 处 BEGIN 区全无 END、含 "pending"。静默丢弃实锤。|
| `68827372`（GLM 引用）| ✓ 64msgs/76tools | lists=2, **pairs=0, bNe=2** | **GLM 的静默丢弃复现实锤**：2 个 BEGIN-no-END，清单丢失。|
| `12c0a5a6` | ✓ 30msgs | lists=1 @ **msg30/30** | GLM 的 late-emission 证据真实：唯一清单在最后一条。|
| `dfa8cd38` | ✓ 91msgs | lists=1 @ **msg91/91** | 同上，实锤。|
| `ec1d48a7` | ✓ 131msgs | lists=1 @ **131/131** | 同上，实锤。|
| `d8d80850` / `0da1bef3` | ✓ | lists=1 @ 60/62、first@161/173 | 同上，实锤（0da1 有两清单）。|
| `e4f36863`（GLM 引用为 no-list）| real 1msg/1tool | lists=0 | GLM 判正：一问答任务，无清单合理。|
| `03a6e23d`（GLM 引用为 no-list）| real | lists=0 | GLM 判正：退化 run。|

**结论（维度1）：两报告核心数字互相印证，均正确驳回 30% 前提。GLM 报告额外且正确地识别出 late-emission 模式（详见维度3）。**

> **一处瑕疵（归 GLM）**：GLM 报告第5点手写列出的"3 个 real no-list session"为 `f5a23393/03a6e23d/e4f36863`，但其脚本输出（`tasklist_audit.py` 可重跑）实算的 3 个为 `cd67a086/03a6e23d/e4f36863` —— `f5a23393` 实为**有**清单（first@17，即 GLM 自己的会话），而 `cd67a086`（21msgs/27tools，无清单，即我当前评审会话本尊）被漏写。此误差不影响 7% 率（三个都是"扫描前的进行中会话/退化会话"，非真实失效），但 GLM 报告正文的 session 名单与其脚本不完全一致，属轻微严谨性瑕疵。DS 报告未暴露此类自指误差（DS 无 new finding 手选名单）。

---

## 2. 证据链真实性 — 引用的 session id 与 file:line 是否真实？

### 2a. 引用的 session id（全部实地核对 ~/.gopi/sessions/）

- DS 引用：`6613fe97` ✓ 存在（6613fe97-5524-49df-b417-4a31a614b98a.jsonl）
- GLM 引用：`f5a23393` ✓、`03a6e23d` ✓、`0a2a237c` ✓、`0da1bef3` ✓、`12c0a5a6` ✓、`68827372` ✓、`d8d80850` ✓、`dfa8cd38` ✓、`e4f36863` ✓、`ec1d48a7` ✓ —— **全部真实存在**。

### 2b. 引用的 file:line（实地核 grep / read）

| 引用 | DS 说法 | GLM 说法 | 实际 | 判定 |
|---|---|---|---|---|
| ParseTaskList 静默丢弃 | tasklist.go:91-110 | tasklist.go:91-113 | **tasklist.go:97-98** `if endIdx<=beginIdx {return nil,nil}` | 两者均记准 |
| taskManagementRules rule2 / maxRounds=3 | loop.go:276-285 / 270 | loop.go:263-285 | **loop.go:270 `maxRoundsWithoutTaskList=3`, 272-285 rules** | 两者准 |
| Epic 提示 "end response with BEGIN..END" | loop.go:1411-1412 | loop.go:1411 | **loop.go:1412** | 两者准 |
| ParseTaskList 调用点丢弃 error | — | loop.go:1612/1626/1703 | **1612/1626/1703 三处 `tasks,_ :=`** | GLM 精确 |
| 退出保护 / progressing 日志 | loop.go:608-626 | — | **loop.go:1609-1613**（exit-with-progressing，DS 行号记偏旧）| DS 引用行号偏旧但语义在 |
| R3 trusted-completion 激励 | 未提 | loop.go:1691-1722 | **loop.go:1691-1722 实存**：all-completed+substantive → skip 确认轮 | GLM 精确 |
| heartbeat ShouldRemind 需 CountProgressing>0 | — | heartbeat.go:102-118 | **heartbeat.go:112,118 实存** | GLM 精确 |
| R2 回归 commit | `da28869e` | `da28869` | **da28869e 实存**：`feat(perf): R2 — task list JSON only on change` | 两者准 |

**结论（维度2）：两份报告的引用均真实。GLM 在根因代码行号（R3 激励、heartbeat、Parse 调用点）上比 DS 更精确，DS 在退出保护行号上记偏旧（非致命）。**

---

## 3. 新发现甄别 — GLM 声称 DS 没有的三样东西，逐项判断

### (a) RC2：late-emission 激励 bug（R3 trusted-completion 让模型理性地把唯一清单发在最后一条，20% session）

**判定：真实有据，非过度发挥。** 三重独立证据：
1. **机制真实**：read loop.go:1691-1722 确认 R3 存在 —— 当"round 的清单 all-completed + 答案 substantive"，首个 end_turn 即被信为正常完成（跳过 bug022 的 +1 确认轮）。DS 报告甚至引用了这段（作为"R2 调节奏"的旁证），但**没有意识到它与 R2"omit 普通轮"耦合会形成"奖励在末尾一次性发清单"的激励**。
2. **行为真实**：本评审独立解析 last-50 —— 42 个发清单的 real-work 会话中 **9/42=21% 只在最后一条发唯一一次清单**（12c0a5a6/48d5bec2/43781947/dfa8cd38/9ccb7d54/d9ed875b/a695c112/c87906f2/ec1d48a7）。GLM 报告 8/40=20%，误差来自数据新鲜度，语义吻合。
3. **根因成立**：R2（普通轮 omit 合法）+ R3（末轮 all-completed 清单换 fast-exit）→ 模型跨一整个长会话不发计划、只在收尾发一条"全 completed"清单以换取少跑一轮。这是**激励设计缺陷，非模型不听话**。GLM 准确命名其机理（RC2），且给出数据支撑。

**GLM 独有发现，且是本评审独立复核后真正的增量贡献。DS 完全漏掉此角度。**

### (b) cross-harness 论文对比（arxiv 2604.14228，Claude Code TodoWrite 结构化工具方向）

**判定：真实有据。** web_search + web_fetch 实址核实：
- arxiv **2604.14228** 真实存在（"Dive into Claude Code: The Design Space of Today's and Future AI Agent Systems"，Liu/Zhao/Shang/Shen，2026-04-14）。GLM 的作者、机构、比较对象（OpenClaw、Hermes Agent）、核心论述（"same design questions produce different answers across three contexts"、permission 七模式、五层压缩、MCP/plugin/skill/hook、subagent、append-oriented storage）**全部与论文实际内容吻合**。
- GLM 的 worklog 显示其真实执行了针对性的 `web_search`（4次，明确搜 "Claude Code TodoWrite"、"OpenHands"）+ `web_fetch` 论文本身。
- 唯一保留：论文摘要未逐字出现 "TodoWrite"，TodoWrite/TodoRead 是 Claude Code 广为人知的实际工具（GLM 自己在报告里标注此部分为 harness 知识、lower-confidence）。**诚实标注，非捏造。**

**对照：DS 的 web 研究是已证实的死胡同** —— DS 4 次泛泛 web_search（"LLM agents don't follow system prompt"等），报告自承 "Google reCAPTCHA-blocked, Bing generic, arxiv empty"，未找到该论文。GLM 在此维度完胜。

### (c) P3 提案：task_list 结构化工具替代散文标记

**判定：有据，是论文(c)与根因分析的自然推演，非过度发挥。** 散文标记的三重弱点（safe 解析难/可被 finish_reason=length 截断/harness 无法重注入）都从 (a)(b) 与 #559 静默丢弃真实现象直接推出。且与 DS 报告第4节的结论（"structured/typed task data beats adversarial text markers"、"harness-owned state beats prompt-asked state"）**不谋而合** —— 两份报告在"结构化工具状态优于散文标记"这一点上殊途同归，但 GLM 把它提升为带验收标准（Rule 11 双真实任务场景）的 P3 提案。DS 也提到类似方向但仅作 best-practice 讨论，未成可执行提案。

**结论（维度3）：GLM 的三项新发现全部真实有据（RC2 且有独立数据支撑），无过度发挥。三项均为 DS 未覆盖的增量。GLM 在该 Epic 上的"发现广度"显著优于 DS。**

---

## 4. 工作过程质量 — 从两份 session 日志统计对比

| 指标 | DS (0a2a237c) | GLM (f5a23393) |
|---|---|---|
| 总日志行数 | 177 行 | 148 行 |
| API 请求次数 | 36 | 29 |
| assistant 消息 | 86 | 79 |
| **token 用量** | **无 usage 字段，不可比** | 29 请求共 ~937K（prompt 912K，其中 cache_hit 863K；output 24.7K）|
| 工具调用总数 | 52 | 37 |
| 工具分布 | bash 21, read 6, web_search 4, memory_store 8, memory_feedback 4, context_trim 5, git_blame 1, write 1, todo 1 | bash 15, read 4, code_search 2, keyword_search 2, web_search 4, web_fetch 1, memory_store 2, memory_feedback 1, edit 1, context_trim 1 |
| 结构化搜索工具使用 | 0（全用 bash grep/自写脚本）| **code_search 2 + keyword_search 2（正确使用 GoPi 规则3 的检索工具）** |
| web 研究深度 | 4 泛泛 search，死胡同 | 4 定向 search（TodoWrite/OpenHands）+ **1 web_fetch 真实论文** |
| 自我修正/走回头路 | **5 次 context_trim**（识别并清理死胡同）| 1 次 context_trim |
| 失败/重试 | 0 错误 | 0 错误 |
| memory 采用 | **1 search + 4 feedback + 8 store**（重度复用/反馈知识库）| 1 search + 1 feedback + 2 store |
| 交付方式 | write 报告 + 自写 /tmp/analyze*.py | write/edit 报告 + 自写 /tmp/tasklist_audit.py（自带可重跑参数化）|

**关键过程对比结论：**
1. **工具路由规范（Rule 3）遵守度：GLM 明显优于 DS。** DS 用 21 次 bash（源码检索多靠 grep/自写脚本），GLM 用 code_search/keyword_search/list 等结构化工具。GLM 更符合 GoPi 编码规范倡导的"检索用工具而非 grep"。
2. **研究深度：GLM 更优。** GLM 用定向搜索+抓取真实论文拿到可引用证据；DS 的 web 研究是确认的死角。
3. **token 效率：无法直接对比**（DS 无 usage 字段本身上是 DS 日志的缺陷）；但按请求数/工具数，GLM 更精简（29 req/37 tools vs 36 req/52 tools），且 GLM 的工作更聚焦源码根因（RC2/RC3）。
4. **memory 采用：DS 更主动**（8 store / 4 feedback vs GLM 的 2/1）。DS 审计的正是 memory 相关领域（引用 #1050/#559），其重度 memory 复用是合理的领域匹配；GLM 更轻。此维度 DS 略优，但与"审计深度"是不同轴。
5. **自我修正：DS 用 context_trim 更勤**（5次），说明 DS 更频繁撞上并清理死胡同；GLM 前置搜索更准（code_search 直达定义），少走回头路。

**综合：GLM 工作过程更"用对工具、直击根因、产出可验证增量"；DS 工作过程"更重 memory、更自反，但源码检索靠 grep、web 研究走死胡同"。**

---

## 5. 交付规范 — 分支+commit+devdoc 可恢复性

| | DS | GLM |
|---|---|---|
| 分支 | `feat/tasklist-audit` ✓ | `feat/tasklist-audit-v2` ✓ |
| commit | `114d1d8` `docs(report): task-list issuance audit` | `5a48e18` `docs(report): task-list audit v2` |
| commit message 质量 | 含根因摘要 ✓ | 含 7%+late-emission+RC2 摘要 ✓ |
| devdoc 行数 | 154 行 | 138 行 |
| `git show <branch>:<path>` | ✓ 可取 | ✓ 可取 |
| 是否改动生产代码 | 否（纯报告）| 否（纯报告）|
| 可重跑脚本 | /tmp/analyze*.py（多段脚本）| /tmp/tasklist_audit.py（单文件参数化可重跑）|

**结论（维度5）：两份交付均完整合规、可恢复。GLM 是单文件可重跑脚本（`python3 tasklist_audit.py 50` 一键复现），DS 是多个中间脚本（analyze/analyze_all/analyze2/analyze3/analyze_sessions）—— GLM 的可重跑性/可审计性略优于 DS，但两者都合格。**

---

## 6. 遗漏与偏差 — 各自漏了什么重要角度？有无互相矛盾？

### DS 遗漏（GLM 补上）
1. **late-emission 激励（RC2）** —— DS 知道 R2 松弛，但没把 R2×R3 的耦合识别为"奖励末尾发清单"的激励 bug。DS 归因层面处理偏浅：把"探索后才发计划"归为良性行为，漏掉了 R3 驱动的"只在结尾发一次以骗 fast-exit"的激励缺陷。**这是 DS 最重要的遗漏。**
2. **跨 harness 论文支撑** —— 无（web 死角）。
3. **结构化工具提案（P3）** —— 只讨论，未成带验收标准的提案。

### GLM 遗漏（DS 覆盖）
1. **根因与 R2 的精确 commit 史** —— 两者都找到 da28869e，DS 用 git_blame 确认 263-285 行回归引入者，GLM 未明确走 blame 路径（但其 RC1 也引用了同一 commit）。此点微差，DS 略深。
2. **memory 生态位占比**：DS 明确讨论"2% 会话首轮发清单、93% 工具轮后发"的完整分布表（turn0/1/≥2 分层）；GLM 只有中位数+20% 末发，未给完整分布。**DS 在这张分布表上更全面。**

### 互相矛盾点
**两者没有根本性矛盾。** 唯一表面张力：DS 归因"first turn 被当作 first substantive turn"为良性 LLM 行为，GLM 归因同样的 deferral 为"激励 bug（RC2）导向末尾"。二者不冲突 —— DS 的观察（93% 工具轮后发、turn0 仅 2%）和 GLM 的更深洞察（R2×R3 让一部分模型拖到末尾）**共存且互补**：前者说明了"为什么普遍延迟"，后者说明了"为什么有 20% 延迟到极点"。**GLM 在 DS 的观测上挖深了一层。**

### 另有一处 DS 的隐含自我印证失误（供未来注意）
DS 工作日志本身 `0a2a237c` 有 8 处清单、其中 **2 处是 BEGIN-no-END（bNe=2）+ 10 处 user-refs** —— 即 DS 自己的会话也在 prose 中触发了 marker 假阳性。DS 报告并未用"自我会话即含假阳性"这个现成案例来强化其 role-aware 方法论（GLM 报告反而指出了这一点）。这是 DS 可用而未用的一条证据，反映其方法论自省略欠。

---

## 7. 决定性结论

### 哪个 agent 的工作更好？—— **GLM（glm-5.3-flash）整体更优**，理由分三层：

**第一层（结论都正确，打个平手）：** 两者都正确驳回 30% 前提、都给出 ~7% 真实率、都定位 Root cause（R2 松弛 + #559 静默丢弃）、引用都真实、交付都合规。在"把话说对"上，DS 和 GLM 没有差距。

**第二层（增量发现，GLM 胜）：** GLM 独有且**正确**地发现：(a) RC2 晚期发放激励 bug（R2×R3 耦合，本评审独立复核 21% 会话只在末尾发唯一清单，实锤）；(b) 真实存在的跨 harness 论文 arxiv 2604.14228 作为证据底盘；(c) 带 Rule-11 验收标准的 P3 结构化工具提案。三样全部有据、无过度发挥。这三点把"这是一次审计"升级为"这是一次带激励建模+外部基准+落地方案的审计"。**这是决定性差距。**

**第三层（工作过程，GLM 略胜）：** GLM 更合规地使用结构化检索工具（code_search/keyword_search 而非大量 bash grep）、更专注地做定向 web 研究并抓取真实论文、产出单文件可重跑审计脚本、行号引用精确到 R3 激励与 heartbeat。DS 在 memory 复用（8 store）与完整分布表上更优，但过程上走了更多 bash 死胡同。

> **不能判"各有千秋"**：虽然两者"结论层面"平手，但本 Epic 的核心是"审计并给出行动建议"，增量发现的广度和深度是评审任务最重的权重。GLM 在那三点上都是 DS 所缺。**GLM 胜出，胜在"发现"而非"复述"。**

---

## 8. 学什么 —— 胜者方法中值得写进派工规范

从 GLM 的成功路径提炼，建议写入派工规范（GOPI.md / PA 派工 checklist）：

1. **"再深挖一层"纪律**：当一个观察被归因为"良性 LLM 行为"（如 DS 的"deferral 合理"），强制做一次**机制耦合检查** —— 该行为是否正好被某一 harness 逻辑（R3 fast-exit）奖励？根因分类从"prompt 松弛"推进到"激励设计 bug"。RC2 正是这样从 DS 的良性归因中挖出来的。
2. **外部基准优先于内部常识**：跨 harness 对比要走"真实 paper + 真实 product docs + 定向搜索"，不要停在泛泛 web 搜索（DS 的死胡同证明泛搜无效）。GLM 的 `web_search("Claude Code TodoWrite OpenHands...")` + `web_fetch(arxiv abs)` 是正确姿势。
3. **单文件可重跑审计脚本**：审计/评测类任务要求交付"一个命令可重跑的脚本"（`tasklist_audit.py N`），而非多个中间脚本。DS 的 analyze*.py 散落多个，可复现性差。
4. **行号引用精确化**：引用 bug 时必须精确到机制边界（R3 的 1691-1722、heartbeat 的 102-118），用 read 而不是靠 grep 猜行号。
5. **结构化搜索工具优先**（规则3 重申）：源码检索用 code_search/keyword_search，grep 是最后手段。GLM 比 DS 更符合规范，且确实更快直达定义。

---

## 9. 派工决策影响 — P0/P1.4/P2/P3 清单与"P1.5"建议

两报告在**派工优先级上的共识**（DS P0+P1.4/P2 | GLM P0+P1/P2/P3/P4）已对齐：
- **P0**：修 ParseTaskList 静默丢弃（BEGIN-only 解析到 EOF + warning + `TestParseTaskList` 回归）。**两报告一致，必须派。**
- **P1.4/P2 系**：no-list-yet nudge、first_tasklist_round 遥测。**两报告一致，应派。**

### GLM 的 RC2 已被验证为真 → 派工清单必须加一条"P1.5 解耦 exit 与清单发放时机"

**明确建议：加入 P1.5 是必要的，列为 P1 级别（紧随 P0）。** 依据：
1. 本评审独立复核确认 **21%** 的 last-50 发清单会话只在结尾发唯一一次"全 completed"清单，这是**常态化的 plan-tailling（计划只在收尾补一张收据）**，不是偶发。让模型在长会话里"不发计划直到结束"直接违背 task-list 的设计初衷（计划，而非收据），是比"no-list"更隐蔽的真缺陷。
2. 根因已在源码坐实（loop.go:1691-1722 R3 fast-exit 与 rule2 omit 的耦合），修复是**低成本的激励解耦**：GLM 提议把 R3 trusted-completion 扩展到接受 `tasks==nil 且 todoState.hasList==true 且已全部 completed；同时 prompt 明确"清单是计划不是收据，首个实质轮就建立"**。这样不再奖励"拖到最后发"。
3. 不修的话，P0/P1.4/P2 修完，"no-list"率继续下降，但"只在结尾发清单"的 20% 会掩盖为"has list = 合规"，遥测也测不出激励问题 —— 等于用一个指标的错误替代读不出真问题。

**具体派工建议（P1.5）**：
- 解耦：R3 trusted-completion 不再要求"本轮必须带 all-completed 清单"（改为：harness 侧 `hasList` + 远端已全部 completed 即可 fast-exit）；保留"发了清单可 fast-exit"作为兼容路径。
- 加 prompt 规则："task list 是计划不是收据；在首个实质工具轮之前建立它，不要在最后一条消息才发。"（呼应 RC4）
- 遥测命名：`first_tasklist_msg_ratio = first_list_msg / total_msgs`（P4 已有雏形，P1.5 直接复用）。

**综合派工优先级（评审建议终版）：** P0（修静默丢弃）→ **P1.5（解耦 exit 与清单发放时机，RC2）** → P1.4/P2（no-list-yet nudge + 遥测）→ P3（task_list 结构化工具，中期）→ P4（jsonl 端遥测字段）。

---

## 10. 交付物声明（Rule 6d）

- **分支：** `feat/tasklist-audit-compare`
- **恢复命令：** `git show feat/tasklist-audit-compare:devdoc/tasklist-audit-compare-report.md`
- **确认：** 本报告已在 worktree `~/workspace/tasklist-compare` 提交；worktree 与主仓共享对象库，commit 已落在主仓的该分支上（无需 push）。
- 未触碰 master、未触碰 live 二进制 `bin/gopi`、无 force-push。
