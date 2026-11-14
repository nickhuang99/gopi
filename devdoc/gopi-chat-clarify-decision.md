<!-- doc-class: normative -->
状态：**设计 + 决策文档（本次不落地任何代码）**
范围：`gopi chat` 表面上"agent 需要用户做决定并等待"这一布局要不要有、画在哪、超时怎么办。
本次结论摘要：**未修改系统 prompt，未修改任何工具定义，未新增 claudeToolDefs 条目。**
本次唯一改动 = 本文件 + `devdoc/DOC-INDEX.tsv` 两行索引。

参考实现（只读）：`~/work/hermes-agent`。本文对它的**每一处**关键论断都由我本人 `read`/`sed -n` 读过原文，标注 `file:line`；凡与代码实际不符者，在 §7 逐条列出并修正。

---

## 0. 前置：本 epic 已被一次 run 回答过（复用优先）

在动手前先做了 prior-art 检索，命中：

- commit `8a7edebc`（"docs(devdoc): gopi chat clarify — Hermes clarify anatomy, surface mapping, four-question decisions, landing checklist"），产出 `devdoc/gopi-chat-clarify.md`（392 行）+ `devdoc/gopi-chat-clarify-landing.md`（110 行），并已在 `devdoc/DOC-INDEX.tsv` 建了两条索引。
- 关键事实：`git merge-base --is-ancestor 0c16bd86 8a7edebc` **成立** —— `0c16bd86`（即本 worktree 的 HEAD）正是 `8a7edebc` 的父提交。也就是说，**这次 epic 的上一棒就落在这个分支的下一个 commit 上**，而本 worktree 是从 `0c16bd86` 重建的，因此那些文件不在本树里。

所以本次不是"从零设计"，而是**核验 + 修正 + 补齐**。这是正确的动作：epic 的验收标准里有"每处论断可核验"和"判据表覆盖低/高风险两档"，而上一版文档恰恰**没有判据表**（见 §5）。

**本文与两版前文的关系**：本文**取代**（supersede）`8a7edebc` 的 `gopi-chat-clarify.md`。凡两版一致的结论，本文逐条重述并给出我本人核验过的行号；凡上一版有误或含糊的，在 §7 逐条指出。上一版的 `gopi-chat-clarify-landing.md`（落地清单）内容仍成立，本文 §6 以更紧凑的形式吸收它，不重复。

---

## 1. 三个决策（先给答案，再给理由）

| # | 问题 | **结论（单一、不含糊）** |
|---|---|---|
| D1 | gopi chat 要不要"限时提问"布局？ | **不要做成"限时提问"。要"提问"，但不要"限时倒计时作为主机制"。** 面板要有、选项要有、结构化结算要有；**倒计时只作为第二阶段的可选降级**，且它的默认语义**不是"代用户执行首选项"**。理由见 §1.1 |
| D2 | 如果要，画在哪？ | **画在 answer band（提交行），选择状态复用 editor 的候选菜单行；绝不新开区域、绝不做模态浮层。** 落点与行号见 §4 |
| D3 | 超时后怎么办？ | **三档，且都不许"默认执行首选项"**：① 低风险**可逆**决策 → 超时后模型按 `best judgement` 继续，**但必须在 band 里留下一条明说"我替你怎么决定、理由是什么"的行**；② 高风险**不可逆**决策 → **超时不得自动继续**，改为**挂起 + 显式告知**（band 留问题、状态行标 `awaiting`、turn 结束但不推进）；③ 不可投递（cooked/无面板）→ 不提问，直接把选项写进回答正文。判据表见 §5 |

### 1.1 D1 为什么是"要提问、但不以倒计时为主"

Hermes 的倒计时不是**用户价值**，是**它自己架构的补丁**。这一点必须说清楚，否则会把 Hermes 的一个缺陷当成特性抄过来：

- CLI 侧 `_clarify_callback` 在 agent 线程里**阻塞**（`cli.py:11694-11725` 的 `while True: response_queue.get(timeout=1)`），所以它**必须**有个 deadline 否则线程永远不返回。
- gateway 侧同理，`wait_for_response` 阻塞在 `threading.Event`（`tools/clarify_gateway.py:103-143`），默认 3600s，而且注释明说这个数字是在**防止把 running-agent guard 永久占住**（`clarify_gateway.py:265-284`，引 issue #32762）。
- 连 Hermes 自己的 headless 模式都**禁止**用 clarify，理由写得极狠：`agent/prompt_builder.py:279-284` —— "Do not call `clarify` to ask questions. You are running headless — there is no live user to answer. **The call will time out and the task will sit silently in `running` with no signal to the operator.**"

**结论**：Hermes 的倒计时是被阻塞线程逼出来的，而它自己承认这条路在无人值守时会"静默挂在 running"。gopi chat 的架构**没有这个约束**（见 §2.1 的跨轮设计），所以：

- **要**：结构化面板、可选行、"Other"、三档语义、决定留痕。
- **不要**：把 120s 倒计时当成 gopi 的主交互形态。gopi 的 read loop 本来就在等输入，**不设 deadline 是合法的**（用户可以离开一小时再回来，面板还在 band 里、还能答）。倒计时若要做，只作为 `--clarify-timeout` 这类**显式 opt-in**，且默认值考虑设为**不超时（0 = wait forever）**。

---

## 2. 问题本质

`gopi chat` 今天是"输入一行 → 跑一轮 ReAct → 输出答案"。agent 需要一个**只有用户能回答的决定**时，只有两条路：

1. 在答案正文里写"问题 + 1) … 2) … 3) …"，然后结束这一轮等用户打一行。
2. 直接猜一个默认值往下跑，把猜测埋进正文。

两条都不可解释：路径 1 把"我需要你决定"降级成散文，用户在滚动区读到的是建议而不是一道题，无超时语义、无归属记录；路径 2 是静默决定，用户事后才发现方向被选过。

**clarity 的价值不是"多一个输入框"**，而是让每一次需要用户决定的地方留下一条**结构化的、寿命有边界的、归属可分辨的、留在 band 里可回看的**记录。

---

## 3. Hermes clarify 机制精确解剖（我本人核验）

### 3.1 Schema —— 4 选项 + UI 追加的第 5 项

`~/work/hermes-agent/tools/clarify_tool.py`（我通读 1-256 行）：

| 事实 | 位置 | 我读到的内容 |
|---|---|---|
| 上限 4 | `clarify_tool.py:19-20` | `MAX_CHOICES = 4`（:19 注释即 'A 5th "Other (type your answer)" option is always appended by the UI' ）—— **第 5 项不在 schema 里，是 UI 追加的** |
| 入参 | `clarify_tool.py:125-179`（`CLARIFY_SCHEMA`） | `question`(string, required) + `choices`(array of string, `maxItems: 4`, **非 required**) |
| 两模式 | `clarify_tool.py:127-143`（description） | 有 `choices` = 多选；**完全省略** = 开放式自由文本 |
| 溢出裁剪 | `clarify_tool.py:90-93` | `if len(choices) > MAX_CHOICES: choices = choices[:MAX_CHOICES]`；空列表→`None`（退化开放式，`:93`） |
| 平台无关归一化 | `clarify_tool.py:24-59`（`_flatten_choice`，:23 起） | LLM 实测会吐 `[{"description": "..."}]`；解包顺序 `label → description → text → title`，**刻意排除 `name`/`value`**；一个都没有→`""`被丢弃 |
| 返回值 | `clarify_tool.py:110-114` | JSON `{question, choices_offered, user_response}`，**用户选择以字符串回到模型上下文** |
| 无 callback | `clarify_tool.py:95-100` | `{"error": "Clarify tool is not available in this execution context."}` —— 硬失败，不是静默成功 |
| callback 异常 | `clarify_tool.py:102-108` | `{"error": "Failed to get user input: <exc>"}` |
| 注册 | `clarify_tool.py:181-191` | `registry.register(name="clarify", toolset="clarify", emoji="❓")`，`check_fn` 恒真（`clarify_tool.py:116-118`） |
| **反向边界（关键）** | `clarify_tool.py:140-147` | description 明写：**危险命令的 yes/no 走 terminal 自己的确认通道，不要用 clarify**；"**Prefer making a reasonable default choice yourself when the decision is low-stakes.**" |

最后一行是整个设计的枢轴：Hermes 自己规定**低风险决策不该问**。这直接支撑 §5 的判据表。

### 3.2 CLI 侧 —— 面板形态、按键、倒计时、超时返回值

`~/work/hermes-agent/cli.py`：

| 事实 | 位置 | 我读到的内容 |
|---|---|---|
| 面板边框/标题 | `cli.py:14735-14738` | `╭─ ` + `Hermes needs your input` + `─…╮`（我实读此行） |
| 问题超出被截断 | `cli.py:14729-14732` | 超过 `max_question_rows` 时尾部换成 `… (question truncated)` |
| 选项行渲染 | `cli.py:14755-14757` | 逐个 choice，选中项用 `class:clarify-selected` |
| **"Other" 行（UI 追加）** | `cli.py:14759-14778` | `other_idx = len(choices)`、`other_num = other_idx + 1`；文案 `Other (type your answer)`（`cli.py:14636-14639` 的 preview 分支同源） |
| 面板底部 | `cli.py:14780` | `╰───…╯` |
| 模态 gate | `cli.py:14785-14791` | `ConditionalContainer(..., filter=Condition(lambda: cli_ref._clarify_state is not None))` |
| **↑/↓ 选择** | `cli.py:13729-13743` | `clarify_up` 用 `max(0, selected-1)`；`clarify_down` 用 `min(len(choices), selected+1)` —— **`len(choices)` 即 "Other" 的下标**，注释写明 "last index is the "Other" option" |
| **数字快捷键** | `cli.py:13745-13770` | `1-9` 选 item 0-8，`0` 选第 10 项；`idx == len(choices)` → 进入 freetext |
| **Enter 结算** | `cli.py:13531-13545` | `selected < len(choices)` → `response_queue.put(choices[selected])`；否则（Other）→ `_clarify_freetext = True` |
| freetext 结算 | `cli.py:13520-13529` | 打文本 + Enter → `put(text)`，清 state |
| **倒计时渲染** | `cli.py:14513-14521` | `remaining = max(0, int(deadline - time.monotonic()))`，`({remaining}s)`；**只在 `_clarify_state` 存在时画** |
| **默认 120s** | `cli.py:472-474`（值在 `:473`） | `"clarify": {"timeout": 120,  # Seconds to wait for a clarify answer before auto-proceeding}` |
| 读取 timeout | `cli.py:11676` | `timeout = CLI_CONFIG.get("clarify", {}).get("timeout", 120)` |
| 每秒重绘循环 | `cli.py:11694`起（轮询循环 11697-11713） | `response_queue.get(timeout=1)`（轮询循环 `cli.py:11697-11713`）；`queue.Empty` → 算 remaining，≤0 则 break，否则每 ≥1.0s 调一次 `_paint_now()` |
| 成功路径留痕 | `cli.py:11700-11702` | `_persist_prompt_summary("?", "Clarify", question, str(result))` |
| **超时清场** | `cli.py:11715-11719` | 清 `_clarify_state`/`_clarify_freetext`/`_clarify_deadline`，`_paint_now()`，打印 `(clarify timed out after {timeout}s — agent will decide)` |
| **超时返回值（逐字）** | `cli.py:11721-11724`（`"The user did not provide a response within the time limit. "` 在 `:11722`） | `"The user did not provide a response within the time limit. Use your best judgement to make the choice and proceed."` |
| **中断/取消返回值（逐字）** | `cli.py:12122-12126`（`set` 在 `:12125`）| `"The user cancelled. Use your best judgement to proceed."`（配套缺陷描述见 `cli.py:12103-12112` 的 docstring（`Push a terminal value onto each queue` 在 `:12108`、`#14026` 在 `:12110`），issue #14026：中断后 state 不清 → 输入被 `read_only` 永久 gate，终端冻结到超时） |

**三档语义表（Hermes）**：

| 情形 | 返回模型 | 屏幕 |
|---|---|---|
| 用户答复 | 用户字符串 | 面板消失，答案进 transcript |
| 超时未答 | "did not provide a response within the time limit. Use your best judgement…" | `(clarify timed out after Ns — agent will decide)` |
| 中断/取消 | "The user cancelled. Use your best judgement to proceed." | 无（由中断本身表达） |

**共同点：永远不返回空/None，永远给模型一句可执行指令。** 区别只在措辞 —— "你没答"和"你取消了"是两件事。

**注意：三档里没有一档说"用第 1 项"。** 这是 Hermes 最值得抄的一条。

### 3.3 网关侧 —— Event + 超时 + 文本规约

`~/work/hermes-agent/tools/clarify_gateway.py`：

| 事实 | 位置 | 我读到的内容 |
|---|---|---|
| 模块状态 | `clarify_gateway.py:48-64` | `_ClarifyEntry{clarify_id, session_key, question, choices, event: threading.Event, response, awaiting_text}` |
| 阻塞等待 | `clarify_gateway.py:103-143` | 按 **1s 切片**轮询 `entry.event.wait(timeout=min(1.0, remaining))`；注释说明：裸 `Event.wait(600)` 会让线程 10 分钟零活动，**被网关 inactivity watchdog 杀掉**（`:106-109`） |
| 每次切片保活 | `clarify_gateway.py:131-132` | `touch_activity_if_due(..., "waiting for user clarify response")` |
| 超时返回 | `clarify_gateway.py:143` | `return entry.response` —— 超时即 `None` |
| **默认 3600s** | `clarify_gateway.py:265-284` | `get_clarify_timeout()` 读 `agent.clarify_timeout`，默认 **3600**；docstring 明说旧的 600s 会"evicted the entry mid-think… the agent hung on `running: clarify`"（#32762） |
| **文本规约 "2"→选项文本** | `clarify_gateway.py:190-203` | `_coerce_text_response`：`idx = int(text) - 1`，`0 <= idx < len(choices)` → `return entry.choices[idx]`；否则尝试**大小写不敏感**的选项文本精确匹配；都不中 → 原样返回自由文本 |
| 会话规约入口 | `clarify_gateway.py:206-214` | `resolve_text_response_for_session(session_key, response)` → 取该 session 最老的 pending entry → `resolve_gateway_clarify(...)` |
| "Other" 进入文本捕获 | `clarify_gateway.py:217-227` | `mark_awaiting_text(clarify_id)` 把 `entry.awaiting_text = True` |
| **会话边界清场** | `clarify_gateway.py:249-270` | `clear_session`：`entry.response = ""`（**空串 sentinel**，注释说明调用方据 falsy 判"未响应"）+ `event.set()` |
| 投递失败兜底 | `gateway/run.py:18247-18249` | `return "[clarify prompt could not be delivered]"`，先 `clear_session` |

### 3.4 头无模式与并行 —— 两条"绝不"

- **headless 禁用**（`agent/prompt_builder.py:279-284`）："Do not call `clarify`… The call will time out and the task will sit silently in `running` with no signal to the operator." —— 替代方案是 `kanban_block(reason=...)`。
- **永不并行**（`agent/tool_dispatch_helpers.py:41-42`）：`_NEVER_PARALLEL_TOOLS = frozenset({"clarify"})`，注释 "interactive / user-facing"。一批里只要出现它，整批退化为串行执行。
- 补齐上下文：clarify 属于 agent-runtime 自持 post-hook 的工具集（`agent/agent_runtime_helpers.py:102-104`），dispatch 在 `agent/agent_runtime_helpers.py:2293-2301`，callback 从 `agent.clarify_callback` 取（`agent/agent_init.py:295`、`:361-362`、`:529`）。
- 压缩摘要文案：`agent/context_compressor.py:676-677` → `"[clarify] asked user a question"`。

---

## 4. gopi chat 现状盘点：能承载面板的通道，与缺什么

### 4.1 能承载的通道（行号均由我本人核验）

| 通道 | 位置 | 形态 | 适配判断 |
|---|---|---|---|
| `region.WriteLines(text)` | `internal/lineedit/region.go:921`（源；经 `editor.go:694` `Editor.WriteLines`） | **多行正文**，trailing break 丢弃，与 `/help` 面板同路径 | ✅ **面板正文走这条**：问题行 + 选项行 |
| `Editor.Note(text, st)` | `internal/lineedit/editor.go:948` | **单行 + 样式**（`WriteToolRow` 的底层，`cmd/agent/lineedit_wire.go:420-426`） | ✅ 面板的**高亮/边框行**（如 `❓ 需要你的决定`） |
| `Editor.WriteBandRow(row) bool` | `internal/lineedit/editor.go:838` | **单行**，宽行由 caller 按 `BandCols` 预折（`editor.go:802-806`、`WrapToCols`） | ✅ 倒计时行/状态行候选；caller 已知切行点 |
| `Editor.MenuState/MenuSelect/MenuClose/MenuAccept` | `internal/lineedit/editor.go:1061`（`MenuState`）、`:1188`（`MenuSelect`）、`:1202`（`MenuClose`）、`:1224`（`MenuAccept`）；配合 `Menu.Rows()`（`internal/lineedit/menu.go:108`）作为已画行数的**唯一真源**，与 `region.SetMenuRows`（`editor.go:2129`）对账 | **候选菜单/选择态**：`Menu` 是不可变快照，Read loop 只读最后发布的一份；Up/Down 由 `MenuSelect(delta)` 走，取消由 `MenuClose()` 走 | ✅ **选择态复用**：Up/Down + 数字键的按键语义已在 `keys.go`（`KeyUp/KeyDown` 在 `internal/lineedit/keys.go:24-25`，`KeyEscape` 在 `:52-55` 附近，lone-ESC 与 escape-sequence 前缀的歧义由 `BareEscape`/`FlushEscape` `:156-166` 解决）|
| `Editor.SetStatus/SetSessionID/SetActivity` | `internal/lineedit/editor.go:674`、`cmd/agent/lineedit_wire.go:237` | status row —— **恰好一行**，终端宽度对齐：行位置由 `region.StatusRow()`（`internal/lineedit/region.go:605`）给出，行内容由 `AnswerRenderer.drawStatus()`（`internal/lineedit/answer.go:2916`）绘制，正文来自 `styledStatusLine()`（`answer.go:3000`）；`drawStatus` 只在 "行号/宽度/文本" 三者之一变化时才重绘（同一行号的重复 publish 不产生写风暴） | ⚠️ **可用但不得侵占**：只能放 `awaiting…` 这类短标记；**绝不能放面板正文**（行数被写死为 1） |
| `chatInput.WriteNotice(text)` | `cmd/agent/lineedit_wire.go:395` | 有 band → `editor.WriteLines`；无 → stderr。**降级路径已存在** | ✅ 三档 notice 都走它；cooked 模式自动退化为"stderr 编号文本"，与 Hermes 的 text fallback（`clarify_gateway.py:40-46`）同形 |

**布局预算**（`internal/lineedit/region.go:74-133`，我实读）：`regionRowsMin = 2`（band 下限）、`menuRowsCap = 5`、`statusRows = 1`、`minEditorRows = 1`、`editorChromeRows = 2`、`EditorRowsMin = 3`、`rowsMinUsable = regionRowsMin + EditorRowsMin + statusRows = 6`。band 是残差（`:434` `free := rows - statusRows`），**没有上限常量**，所以面板靠"挤出 band 行"落地是行得通的；但**低于 6 行的终端直接没有 reserved layout**，面板必须走降级。

### 4.2 缺什么（诚实清单）

| 缺口 | 现状证据 | 本次要做的决定 |
|---|---|---|
| **模态/选择状态** | 无。编辑按键在 `internal/lineedit/editor.go:2257`（`handleKey`）→ `:2262`（`switch k.Code`）分发，**没有"选择器模式"**（`keys.go:65-66` 一带只是控制字节常量，不是分发点） | D2：**不做模态**。选择态复用菜单（见 §4.1）|
| **倒计时** | `SetActivity` 只渲染传入的字符串，无 ticking | D1：**不作为主机制**；若做，走 `notify.go` 的 wake pipe（见 §5 表末）|
| **超时默认规则** | 完全没有 | D3：见 §5 判据表 |
| **选择输入规约**（"2"→选项文本） | 无。gopi 的 slash 只认 `/` 前缀（`cmd/agent/commands.go:945-947`） | 照译 `clarify_gateway.py:190-203`，但**作为 P2 的一部分**，不是 P1 |
| **跨轮契约** | 无 | **这是 gopi 与 Hermes 最大的结构差异**，见 §4.3 |
| **挂起态的渲染** | 无 | D3 的高风险档需要它（band 留问题 + status row 标 `awaiting`）|

### 4.3 结构差异：跨轮，不是阻塞回调

Hermes 的 clarify 回调在 agent 线程里**阻塞**（`cli.py:11694` 的 `response_queue.get`），因为 gateway 有 worker 池、CLI 有独立 prompt_toolkit 事件循环。

gopi chat **不能这么干**：`cmd/agent/cli_chat.go:1576` 的 feeder 是**唯一**读 stdin 的 goroutine，主循环在 `lineCh` 上串行消费并跑 turn（`lineRead` 在 `:1540-1552`）。让 turn 内部阻塞等输入 = 主循环既跑 turn 又读输入。

**gopi 的形状**（与既有先例同形）：

```
turn N:  model 调 clarify(question, choices)
         → 面板行 commit 进 band
         → clarify 立刻返回一句确认（"面板已出，等你的选择"）
         → turn N 结束
         ← 用户选/打字（编辑器行结算为一行输入）
turn N+1: 该行作为下一轮用户消息
```

这是 `chat_surface` 的 `type` action 已经走通的形状（`cmd/agent/chat_surface_surface.go`，`return func() { ag.SetChatSurface(nil) }` 在 `:409-410` 的安装/卸载契约同形）。

**代价（必须诚实）**：clarify 的答案作为**新的用户消息**进入对话，而不是同一次工具调用的返回值。所以 def 的 description 必须明确写："用户的答复将在你的**下一轮**到达；本轮不要重复询问。" —— 这是最高风险项，见 §6 风险 1。

---

## 5. 判据表：超时/无人应答时，哪种决定可以默认执行

这是本 epic 点名要正面回答的质疑（"首选项默认执行是否等于没问"）。先给**总裁定**，再给表。

### 5.1 总裁定

> **"问了却自己决定"在低风险场景下不成立为"等于没问"，当且仅当三件事同时为真：**
> **(a) 决定可逆**（错了能撤回，成本 ≈ 0）；
> **(b) 决定可结算**（band 里留下"我替你决定了 + 理由"一行，用户回看时能看见、能反驳）；
> **(c) 推迟的代价 > 做错的代价**（挂起会让任务卡死，而挂起的用户可能很久不回来）。
>
> **三缺一，就必须改为保守默认或挂起。高风险**不可逆**决策永远不满足 (a)，因此永远不得默认执行。**

关于"首选项能否代替用户执行"：**不能，而且这与是否有首选项无关。** Hermes 的 schema 里没有 `default`/`recommended` 字段（`clarify_tool.py:167-205` 只有 question/choices），三档返回值里也没有一档提"第 1 项"（§3.2 表）。**理由**：一旦有"可默认执行的首选项"，"超时"和"用户主动选了它"在 transcript 里长得一样，归属可分辨性直接归零（§2 说要保护的那件事）。**Agent 想推荐，就该在 `question` 文本里说出来**（"我倾向 3，因为…"）—— **推荐是说出来的，不是执行出来的。**

### 5.2 判据表

| 决定类别 | 可逆？ | 影响面 | 超时后 gopi 该做什么 | 屏幕必须留下什么 |
|---|---|---|---|---|
| **低风险 + 可逆**<br>例：选哪个暂存目录、用哪个 UI 文案措辞、要不要为一次试验建临时分支 | ✅ | 本地、可撤回 | **模型按 best judgement 继续**，用 verbose 措辞 "The user did not provide a response within the time limit. Use your best judgement…" | band 一行：`[gopi] clarify 超时 — 我按 X 继续（理由：…）。用 /clarify-retract 或直接说"改回"可撤回` |
| **高风险 + 不可逆**<br>例：`git push --force`、删分支/文件、改 git 历史、发外部消息、调用会产生费用的 API、写生产环境 | ❌ | 外溢、不可撤回 | **绝不自动继续**。**挂起**：turn 正常结束，状态标 `awaiting`，问题**留在 band 里**；模型不得在无人应答时推进该动作 | band：完整问题 + 选项 + `❓ 等待你的决定（未超时挂起）`；status row：`awaiting · 1 项待决` |
| **高风险 + 可逆（有已知安全默认）**<br>例：某个操作不确定要不要做，而"不做"是安全且可继续的 | ✅ | 可撤回 | **取保守默认**（=不做），并**显式告知**"我选了不做，因为不能替你决定" | band：`[gopi] 我按保守默认「不做」继续 — 这个决定需要你，随时说"做吧"我来重做` |
| **需要用户独有信息**（不是偏好，是事实）<br>例：API key、机器地址、外部账号名 | — | — | **不是超时问题**：缺信息就无法推进。**挂起 + 说明缺什么**，绝不用编造值填充 | band：`❓ 缺 X 无法继续 — 等你提供` |
| **不可投递**（cooked/无 band/终端 < `rowsMinUsable=6`） | — | — | **不提问**。把选项写成编号正文，结束 turn，等用户下一行 | 正文里的编号选项文本（Hermes 的 text fallback 同形，`clarify_gateway.py:40-46`）|

### 5.3 为什么低风险档仍然要做

质疑是"问了却自己决定，等于没问"。**答案：不等**。因为：

1. **告诉用户"我在等"本身是价值**，即使最终没等到。用户回看 band 时看得见"这里本来要问你，等不到我走了 X 路"，而不是在一段自信的散文里猜不出发问过。
2. **低风险档的 (a)(b)(c) 三条同时成立**，所以"替用户决定"没有偷走用户的权力 —— 用户随时能反驳，且反驳成本极低（(a) 可逆）。
3. **高风险档这三条必有一条不成立**（不可逆 → (a) 破），所以那里必须挂起。**判据不是"这个决定重要不重要"（主观），而是"可逆性"（客观可查）** —— 这正是让结论不含糊的东西。

---

## 6. 落地路线（每项 ≤ 1 session；本次只登记，不实施）

**前置声明：以下任何阶段都不改系统 prompt（`promptSections`/`assembleSystemPrompt`/密度门），不加 `claudeToolDefs` 条目。** 触发时机完全由工具自身 description 表达（Hermes 就是这样：`clarify_tool.py:140-147`），所以实施它**不触发**"改 prompt/工具定义前先有文档+报告"的约束 —— **本文件就是那份文档**。

| 阶段 | 目标 | 验收标准 |
|---|---|---|
| **P1 面板行（无倒计时、无选择态）** | 新 chat-scoped tool `clarify`：def + `chatScopedToolDefs()` 扩容 + `[gopi] clarify …` notice 常量 + 面板提交 + cooked 降级 | 面板出现在 band；cooked 退化为编号文本；`chatScopedToolDefs()` 返回 2 个 def；**`TestGOPIRule3_MatchesClaudeToolDefs` 仍绿**（证明没偷加第 18 个核心工具）；`TestGate_ToolDispatchCoversEveryDef` 绿（需要两条 wire 词汇表，`internal/agent/chat_ops.go:422-435`）；`claudeToolDefs` diff 为空 |
| **P2 选择态（复用菜单）** | 选项渲染成 editor 候选菜单（`Menu`/`Rows()`），Up/Down + 数字键 + Escape 取消；答案由**编辑器行**结算 | `Menu.Rows()` 式真源与实际占用的 band 行**在每个终端宽度下一致**（含窄终端 `rowsMinUsable` 拒绝路径）；Escape → 取消档文案；"2"→选项文本规约单测（照译 `clarify_gateway.py:190-203`）|
| **P3 倒计时（可选 opt-in，默认关）** | `--clarify-timeout SEC`（`0` = 不超时，**默认 0**）；走 `internal/lineedit/notify.go` 的 wake pipe（1 字节，`wakePipeBytes`）每秒 publish 一次 | `--no-color` 下**零 ESC 字节**（端到端灌真实非 TTY stdin 到发布二进制断言）；**不新增第二个终端写者**（每个终端写只有一个 owner）；用户开始输入即取消倒计时（见风险 3）|
| **P4 中断安全 + 挂起态** | Ctrl+C/`Interrupt()` 走取消档；高风险档的 `awaiting` 挂起渲染（band 留问题 + status row 标记） | **最小回归测试**：中断后立即可输入，**不等**超时（Hermes 的等价缺陷：`cli.py:12096-12113` #14026 —— 中断必须推一个**终值**再清 state，否则输入被永久 gate）；挂起后 band 行数结算一致 |

每阶段可独立停下；P1 之外的每一阶段都不阻塞 P1 的可用性。

---

## 7. 与上一版文档（`8a7edebc`）的差异与修正

我逐条读了上一版，核验其行号。**结论：上一版核心论断全部正确**，以下是我核出的差异 —— 记录在这里，而不是让两版文档在库里说不同的话（Rule 14：一个真源）。

| 项 | 上一版 | 本次核验结果 |
|---|---|---|
| `Editor.SetNotification` 行号 | 上一版写 `editor.go:955`；其自己的后续提交 `de61c0d5` 已改为 `:960` | ✅ 本树实测 `internal/lineedit/editor.go:960` —— **上一版已自我修正**，本文沿用 `:960` |
| 面板是否新开区域 | 上一版 §5 已选"占 band"，否决新区域/模态 | ✅ **我同意**，见 §4.1、§5。理由独立复核：band 无上限常量（`region.go:434` 残差），`rowsMinUsable=6` 是降级门 |
| **超时判据表** | ❌ **上一版没有** —— §1.6(2)(3) 只在 Hermes 语境内回答问题，未给"哪种决定可默认执行"的可执行判据表 | ✅ **本文 §5 补齐**（本 epic 的验收标准 5 明确要求覆盖低风险可逆 / 高风险不可逆两档）|
| **倒计时是否为主机制** | 上一版 P3 把倒计时列为阶段之一，但**未明确它是否默认开启** | ✅ **本文 D1 明确**：不作为主机制、默认**不超时**（0=wait forever）。依据：Hermes 的倒计时是被阻塞线程逼出来的补丁，且其 headless 模式自认会"silently sit in running"（`agent/prompt_builder.py:279-284`）|
| **"首选项默认执行"裁定** | 上一版 §3(3) 说"不" + "推荐是说出来的，不是执行出来的" | ✅ **一致**，本文 §5.1 重述并补上**三条件判据 (a)(b)(c)**，使其可判定而不只是原则 |
| Hermes 行号 `cli.py:12122-12127`（取消文案） | 上一版引用 | ✅ 实测 `cli.py:12122-12126` 为 `put("The user cancelled. Use your best judgement to proceed.")`，区间基本准确 |
| 三档语义共同点 | 上一版："永远不返回空/None" | ⚠️ **需精确化**：CLI 三档确实都不为空（`cli.py:11721`、`:12123`）；但**网关侧超时会返回 `None`**（`clarify_gateway.py:143` 返回 `entry.response`，超时未 set 即为 `None`），由 `gateway/run.py:18247-18249` 转成 `"[user did not respond within {int(timeout/60)}m]"`。**gopi 应采用 CLI 的措辞质量**（可执行指令），而非网关的短哨兵 |

---

## 8. 风险

1. **模型对跨轮契约的误解（最高）**：工具同轮返回的不是答案。若 description 没写清，模型会在同轮内等一个不存在的答案。缓解：def 的 description 明写"答复作为**下一轮**用户消息到达；本轮不要重复询问"。
2. **预留行结算漂移**：面板占几行必须与 `Menu.Rows()` 同源，否则窄终端/缩放时 region 重绘错行。缓解：P2 验收就是逐宽度对账。
3. **超时把用户正在打的字吃掉**：用户很可能已在编辑行打字，超时自动提交会与他的输入打架。**Hermes 不存在这个 race**（它的模态 gate 住输入区），**gopi 必须有**。缓解：**用户开始输入即取消倒计时**。
4. **倒计时干扰阅读**：只画一行、只在 band 末端、`--no-color` 下纯文本。
5. **工具滥用**（把 clarify 当"每个动作确认一次"的闸）：缓解 = description 的反向边界（照译 `clarify_tool.py:146-147`）+ 只挂在 chat wire（task path 根本没有这个工具，同 `chat_surface` 的 `chatSurfaceUnavailableNotice`，`internal/agent/chat_surface.go:38-41`）。

## 9. 明确不做

- **不改系统 prompt**（`promptSections`/`assembleSystemPrompt`/密度门）
- **不加 `claudeToolDefs` 条目**（Rule 3）；只走 `chatScopedToolDefs()`（`internal/agent/chat_ops.go:433-435`）
- **不做"默认执行首选项"** —— 无 `default`/`recommended` 字段，超时永不暗示第 1 项（§5.1）
- **不做模态浮层** —— 与 gopi"每个终端写只有一个 owner + 零字节关闭路径"纪律相悖
- **不做多问题批量 clarify**（Hermes 也只能一次一问）
- **不改 bash 破坏性命令的现有确认语义**（`clarify_tool.py:145-147` 同样把这条划出去）
- **不做 gateway/web/desktop 端**（只有 chat 表面）
- **本次不写代码** —— 交付物 = 文档

## 10. 声明

**本次未修改 `internal/agent` 的 system prompt 组装，未新增或修改任何 tool schema。** 本次提交只含两个文档文件（本文件 + `DOC-INDEX.tsv` 索引行），`git diff --stat` 可验。
