<!-- doc-class: normative -->
# gopi chat clarify — Hermes 式结构化问答的解剖、映射与决策

状态：**设计 + 决策文档（本次不落地代码）**
范围：`gopi chat` 表面上的"agent 需要用户做决定"能力。
本次结论摘要：**未修改系统 prompt，未修改任何工具定义，未新增 claudeToolDefs 条目。**
唯一改动 = 本文件 + `devdoc/gopi-chat-clarify-landing.md`（D2 落地清单）。

参考实现（只读）：`~/work/hermes-agent`（Python + TypeScript，mixed）。
本文所有 Hermes 结论都指向具体 `file:line`；所有 gopi 落点同样指向 `file:line`。

---

## 0. 为什么要有这道题（问题的本质）

`gopi chat` 现在是"输入一行 → 跑一轮 ReAct → 输出答案"。当 agent 发现自己需要一个**只有用户能回答的决定**时，它今天只有两条路：

1. 在答案正文里写一段"问题 + 1) … 2) … 3) …"，然后**结束这一轮**，等用户自己打一行。
2. 直接猜一个默认值往下跑，并把猜测埋进答案正文。

两条路都不可解释：路径 1 把"我需要你决定"降级成了普通散文，用户在滚动区里读到的是一段建议而不是一道题，没有任何超时语义；路径 2 是静默决定，用户事后才在输出里发现方向被选过。Hermes 的 `clarify` 工具存在的唯一理由，就是把这件事**变成一次结构化的、可计时的、有明确默认的交互**。

---

## 1. Hermes clarify 机制精确解剖

### 1.1 工具 schema —— 4 个选项 + 永远追加的 Other

`~/work/hermes-agent/tools/clarify_tool.py`

| 事实 | 位置 | 内容 |
|---|---|---|
| 选项上限 4 | `clarify_tool.py:19-21` | `MAX_CHOICES = 4`，注释写明「A 5th "Other (type your answer)" option is always appended by the UI」 |
| 入参 | `clarify_tool.py:169-205`（`CLARIFY_SCHEMA`） | `question` (string, required) + `choices` (array of string, `maxItems: 4`, 非 required) |
| 两种模式 | `clarify_tool.py:148-153` | 给 `choices` = 多选；**完全省略** `choices` = 开放式自由文本 |
| 溢出裁剪 | `clarify_tool.py:98-103` | `if len(choices) > MAX_CHOICES: choices = choices[:MAX_CHOICES]`；空列表 → `choices = None`（退化为开放式） |
| 平台无关的归一化 | `clarify_tool.py:24-59` `_flatten_choice` | LLM 会吐 `[{"description": "..."}]` 形状的 option。归一化只在**这一个平台无关入口**做：dict 解包顺序 `label → description → text → title`，刻意排除 `name`/`value`（那是组件形状字段，不是人类可读标签）；一个都没有 → 返回 `""` 被丢弃 |
| 返回值 | `clarify_tool.py:118-122` | JSON：`{question, choices_offered, user_response}` —— 用户的选择以**字符串**回到模型上下文 |
| 无 callback → 硬失败 | `clarify_tool.py:105-110` | `{"error": "Clarify tool is not available in this execution context."}`，不是静默成功 |
| callback 抛异常 → 包装 | `clarify_tool.py:112-116` | `{"error": "Failed to get user input: <exc>"}` |
| 注册 | `clarify_tool.py:246-256` | `registry.register(name="clarify", toolset="clarify", ...)`，`emoji="❓"`，`check_fn` 恒真（`clarify_tool.py:126-128`） |

**schema 里最重要的不是字段，是那段 description**（`clarify_tool.py:155-168`）。它做了三件 prompt 工程上的事，值得逐字借鉴：

- **反例教学**：`Right: question='Which deployment target?', choices=['staging','prod']. Wrong: question='Which target? 1) staging 2) prod', choices=[]` —— 明确禁止把选项写进 `question`，理由是「options written into the question string render as dead prose the user can't pick」。这是**渲染层的约束被写回 schema**：面板只把 `choices` 渲染成可选行，写进正文字符串的选项就是死文本。
- **正向触发清单**：任务有歧义需要用户选路线 / 任务后想让用户反馈 / 想提议存 skill 或更新 memory / 决策有真实取舍。
- **反向边界**：`Do NOT use this tool for simple yes/no confirmation of dangerous commands (the terminal tool handles that). Prefer making a reasonable default choice yourself when the decision is low-stakes.` —— 低风险决策**不许问**。

最后这条是整份文档四问里第 1 问的答案来源，1.6 节再展开。

### 1.2 平台回调 —— 同步函数，平台各自实现

契约极简：`callback(question, choices) -> str`（`clarify_tool.py:69-72`）。真正的 UI 在平台层：

| 端 | 位置 | 形态 |
|---|---|---|
| CLI | `cli.py:11665` `_clarify_callback(self, question, choices)` | 设置 `self._clarify_state`，prompt_toolkit 模态面板，`queue.Queue` 收键，主循环轮询 |
| Gateway（Discord/Telegram/Slack/…） | `gateway/run.py:18195` `_clarify_callback_sync` | 注册 `clarify_id`，`send_clarify` 走适配器，阻塞在 `threading.Event` 上 |
| Desktop / TUI 网关 | `tui_gateway/server.py:3794-3795` | `"clarify_callback": lambda q, c: _block("clarify.request", sid, {...})`；应答走 JSON-RPC `clarify.respond`（`tui_gateway/server.py:9893`） |
| ACP 工具白名单 | `acp_adapter/tools.py:70` | `"clarify"` 在允许列表里 |

Gateway 侧多一层中间件：`tools/clarify_gateway.py`。它是模块级的 `clarify_id → _ClarifyEntry` 表（`clarify_gateway.py:66-82`），因为适配器要能不解引用 `GatewayRunner` 实例就 resolve 一个应答。两条投递路径（`clarify_gateway.py:33-46`）：

1. **按钮 UI** —— 适配器 override `send_clarify` 画 inline 按钮，按钮回调 resolve；最后一个按钮是 "Other (type answer)"，进入纯文本捕获模式。
2. **文本兜底** —— 没有富 UI 的适配器渲染编号列表；用户回 "2" 或自由文本，gateway 的 `_handle_message` 拦截并 resolve（`gateway/run.py:8988-9014`，明确排除以 `/` 开头的消息，避免把 slash 命令当成答案）。

**注意 `awaiting_text`**（`clarify_gateway.py:73`）：`awaiting_text=not bool(choices)` —— 开放式问题（无 choices）时下一条消息**就是**答案，不画按钮。

### 1.3 超时语义 —— 三处独立的超时常量，且默认值不同

这是最容易被抄错的部分。**三端三个值**：

| 端 | 位置 | 值 | 理由（源码注释原文） |
|---|---|---|---|
| CLI | `cli.py:472-474` | **120s** | `"timeout": 120,  # Seconds to wait for a clarify answer before auto-proceeding` |
| Gateway | `clarify_gateway.py:265-282` `get_clarify_timeout()` | **3600s** | 读 `agent.clarify_timeout`。注释：默认 3600「long enough that a user who steps away (meeting, AFK, slow to read) still finds a live entry when they tap the button, short enough that a genuinely abandoned prompt eventually unblocks the agent thread instead of pinning the running-agent guard forever. **The old 600s default evicted the entry mid-think, so a late tap landed on a dead entry and the agent hung on `running: clarify` (#32762)**」 |
| 投递失败 | `gateway/run.py:18245-18244` | **15s**（`fut.result(timeout=15)`） | 只是 `send_clarify` 的调度超时，失败 → 清 session 并返回 `"[clarify prompt could not be delivered]"` |

**为什么 CLI 120s、Gateway 3600s？** 两个不同的人机节奏：CLI 用户就坐在那个终端前，120s 不答基本等于走开；Gateway 用户在手机上，3600s 才是"他还可能回来点一下"的量级。

**超时后的返回值是三段不同的文案，语义完全不同**：

1. CLI 超时（`cli.py:11720-11727`）：
   - 屏幕：`(clarify timed out after {timeout}s — agent will decide)`，dim 色
   - 返回模型：`"The user did not provide a response within the time limit. Use your best judgement to make the choice and proceed."`
   - **这是"没答 ≠ 取消"**：横幅说"没在时限内响应"，指令是"用你的最佳判断选一个继续"。**不是**"用户拒绝了"。
2. Gateway 超时（`gateway/run.py:18251-18254`）：`f"[user did not respond within {int(timeout/60)}m]"` —— 连分钟数都报出来。
3. 投递失败（`gateway/run.py:18247`）：`"[clarify prompt could not be delivered]"` —— 与"用户没答"**分开**报，因为病因不同（管道坏了 vs 人不在）。

**`wait_for_response` 的 1 秒切片**（`clarify_gateway.py:103-135`）是一个真正的工程细节，不能省：

```python
deadline = time.monotonic() + max(timeout, 0.0)
while True:
    remaining = deadline - time.monotonic()
    if remaining <= 0: break
    if entry.event.wait(timeout=min(1.0, remaining)): break
    if touch_activity_if_due is not None:
        touch_activity_if_due(activity_state, "waiting for user clarify response")
```

注释写得很直白：「without this, `Event.wait(timeout=600)` blocks the thread for 10 minutes with zero activity touches and the gateway's inactivity watchdog kills the agent while the user is still typing」。即**等待用户本身就是"活动"**，必须周期性上报心跳，否则看门狗把 agent 杀掉。返回值：resolve 后是用户应答字符串；超时是 `None`。

CLI 侧的等价物在 `cli.py:11700-11713`：`while True: try: result = response_queue.get(timeout=1)` —— 同样是 1 秒粒度，但目的是**刷新倒计时**（`if now - _last_countdown_refresh >= 1.0: self._paint_now()`，`cli.py:11711-11713`）。

### 1.4 UI 三端渲染 —— 一份数据，三种形状

**数据形状三端同一份**：`{question, choices[]}`（+ `selected` 索引 / `clarify_id`）。

**(a) CLI —— prompt_toolkit 模态面板**

- 状态：`self._clarify_state = {"question", "choices", "selected": 0, "response_queue"}`（`cli.py:11680-11685`）、`self._clarify_freetext`（`cli.py:11688`）、`self._clarify_deadline`（`cli.py:11686`）
- **它 gate 住整个输入区**：`cli.py:5006-5011`、`cli.py:6159`、`cli.py:7913`（`reserved_below` 与 approval/clarify 面板对齐）、`cli.py:12502-12516`（clarify 激活时回车走 clarify 的 response queue，而不是提交普通输入）
- 倒计时：`_clarify_deadline` 每次重绘都按 `remaining` 重算；每秒强制一次 `_paint_now()`（`cli.py:11707-11713`）
- 状态行字形：freetext 模式 `✎`（`cli.py:13069-13070`），选择模式 `?`（`cli.py:13071-13072`）
- 关闭时要**立刻**重绘一次（`cli.py:11715-11719`），否则面板残影留在屏幕上
- **决策留痕**：`self._persist_prompt_summary("?", "Clarify", question, str(result))`（`cli.py:11702`）—— 把已解决的模态问答压成一行写进 scrollback，配置开关 `persist_prompts: True`（`cli.py:466-469`）。理由注释：「so the decision survives the repaint」。

**(b) Desktop —— inline 面板，不是模态**

`~/work/hermes-agent/apps/desktop/src/components/assistant-ui/clarify-tool.tsx`（386 行）

- 结构（`clarify-tool.tsx:56-60`）：`CLARIFY_SHELL_CLASS = 'my-1.5 rounded-md border border-primary/20 bg-(--ui-chat-surface-background) …'` —— 一两行 margin 的 inline 面板，「matches the surrounding tool rows: a single hairline border in the shared stroke token, a soft surface fill, and a faint primary accent that signals "this one needs you" without the loud animated ring」
- 选项行与 "Other" 行**共用布局，只有颜色不同**（`clarify-tool.tsx:47-52`）
- **键盘等价于鼠标**（`clarify-tool.tsx:40-43`）：`letterFor(i) = 'A' + i`，每个选项（含 Other）带一个 A/B/C… 徽章，**徽章同时是快捷键提示**，按字母直接选中
- 自由文本用 CSS `field-sizing`（`clarify-tool.tsx:53-58`）：一行起步、随输入增长、聚焦时不重排面板
- **选择态只画在字母徽章上**（`clarify-tool.tsx:80-82` 附近注释）：`Selection lives on the letter badge alone — a solid primary fill — not the whole row, which stays a quiet hover target`

**(c) TUI —— overlay**

- `ui-tui/src/components/appOverlays.tsx:72-79` —— `if (overlay.clarify) { … <Clarify … req={overlay.clarify} /> }`，一个 overlay 分支
- 三端共用的状态/动效词表：`agent/display.py:433`（tool name → 目标字段映射，`"clarify": "question"`）、`agent/display.py:584`（动词，`"clarify": "Asking"`）

### 1.5 中断安全默认 —— 三档不同，且必须"推一个终值"

`cli.py:12103-12135` 是本次调研里**最有价值的一段**。它的 docstring 描述了缺陷 #14026：

> approval/clarify/sudo/secret prompts each block a worker thread on a `response_queue.get()`. When the agent is interrupted the worker thread is torn down, but the overlay's state dict stays set — leaving the CLI input gated (`read_only` condition + keypress filter) with no thread servicing the prompt. The result is a frozen terminal until the prompt's own timeout expires.

**修复 = 给每个被阻塞的队列推一个终值**，然后把 state 置 nil 并恢复用户被模态盖住前的那份草稿：

- approval → `"deny"`
- **clarify → `"The user cancelled. Use your best judgement to proceed."`**（`cli.py:12122-12127`）
- sudo / secret → `None` / 空

每一步都单独 try/except 包住 —— 「so a dead queue can't prevent clearing the others」。

**这是 clarify 的第三档语义**，与 1.3 的两档并列：

| 情形 | 返回模型 | 横幅/正文 |
|---|---|---|
| 用户答复 | 用户字符串 | 直接当作答案 |
| 超时未答（`cli.py:11720-11727`） | "did not provide a response within the time limit. Use your best judgement…" | `(clarify timed out after Ns — agent will decide)` |
| 中断/取消（`cli.py:12122-12127`） | "The user cancelled. Use your best judgement to proceed." | 无（由中断本身表达） |

共同点：**永远不返回空、永远不返回 None、永远给模型一句可执行的指令**。区别只在措辞 —— "你没答"和"你取消了"是两件不同的事，模型应当能分辨。

### 1.6 四问的答案 —— Hermes 源码里是怎么说的

**(1) 何时强制用户决策？** —— 从不"强制"，只**限定**。

`clarify_tool.py:166-168` 的 description 是明确的反向边界：危险命令的 yes/no 走 terminal 自己的确认通道，**不用 clarify**；低风险决策**应当自己选**。正向清单（`clarify_tool.py:161-165`）要求：任务有歧义需要用户选路线 / 决策有**真实取舍** / 任务后反馈 / 提议写 skill 或 memory。

翻成人话：**只有当"选错要重做"的代价大于"问一次"的代价时**才问。这是一条经济学判据，不是一条"能力到哪就要问到哪"的判据。

**(2) 失败/超时取什么默认？** —— 不是"选第一个"，而是**把决定权明确交还模型，并给出可解释的理由文本**。

超时文案给的是"用你的最佳判断选一个继续"，**没有**暗示第 1 项是默认。中断文案同理。理由：把"选 A"硬编码成超时默认是把平台的判断冒充用户的判断；"用最佳判断"则诚实地承认这一票是模型投的，而且留在了 transcript 里（`persist_prompt_summary`，`cli.py:11702`）。

**(3) 首选项是否可默认执行？** —— Hermes **没有**这个机制，而且它刻意不做。

schema 里没有任何 `default`/`recommended` 字段，`choices` 只是字符串数组。桌面端的 `letterFor` 只做快捷键。唯一的"排序语义"来自模型的书写顺序，而平台不赋予它任何特权。**这是设计选择**：一个带 `default: true` 的选项会让"用户没答"和"用户选了它"在 transcript 里长得一样，可解释性直接归零。

**(4) 为什么还要问？** —— 因为**平台不能替用户决定，但能让决定变得可结算**。

三处证据：`clarify_tool.py:9-12`（"The actual user-interaction logic lives in the platform layer… this module defines the schema, validation, and a thin dispatcher"）；三端各写各的 UI（1.4）；三档超时文案各不相同（1.5）。核心不是"多一个输入框"，而是**每一次需要用户决定的地方都留下一条可审计的、带理由的记录**，而不是一段散文或一次静默猜测。

---

## 2. 映射到 gopi chat 表面

### 2.1 落差总表

| Hermes | gopi | 结论 |
|---|---|---|
| `callback(question, choices) -> str` 同步阻塞调用，在 agent 线程里 | gopi 的 chat 是**单 goroutine read loop**：`cmd/agent/cli_chat.go:1576` 的 feeder goroutine 阻塞在 `input.ReadLine()` 上，主循环消费 `lineCh` | **不能照抄阻塞式回调**。必须把 clarify 做成"一轮的产物 + 下一轮的输入"，见 2.3 |
| prompt_toolkit 模态面板，**接管整个输入区**（`cli.py:5006-5011`、`cli.py:12502-12516`） | `internal/lineedit` 的 band + editor 布局：`Editor.WriteBandRow`（`editor.go:888`）、`Editor.Note`（`editor.go:948`）、`region.WriteLines`（`region.go:921`） | lineedit **没有**模态面板概念，也不该有（模态会破坏"每个写者只有一个 owner + 零字节关闭路径"）。映射到 **band 行 + editor 的候选菜单**，见 2.4 |
| 倒计时靠 `_paint_now()` 每秒重绘 | `lineedit/notify.go` 的 wake pipe + `Editor.PublishStatus`：非 read-loop 的写者通过 1 字节管道唤醒 poll(2) | **形状完全对得上**，倒计时走同一条 publish/wake 通路，不新增并发写者 |
| `Threading.Event` + 1s 切片 + `touch_activity_if_due` | gopi 没有 gateway 侧 inactivity watchdog；chat 是前台进程 | 简化：`time.Timer` 或 read loop 的 tick 即可，**不需要** activity touch |
| 三档超时文案 | 对应 `cmd/agent/chat_surface_surface.go:47-90` 的 `[gopi] …` notice 常量家族 | 超时/取消/投递失败各一条 notice，走 `input.WriteNotice`（`cmd/agent/lineedit_wire.go:395`） |

### 2.2 工具面：为什么必须是 chat-scoped tool

Rule 3 钉死：`claudeToolDefs` 是普通工具面的 SSOT（17 个条目，`TestGOPIRule3_MatchesClaudeToolDefs` 与 `TestToolClassesCoverTheToolSurface` 两个门守着）。往那里加一条就是**新增核心工具**，而且会被广告给每一个没有 chat 表面的 task run。

`internal/agent/chat_surface.go` 已经把这条路的形状走通了一遍，`clarify` 是它的第二个实例：

- def 定义在 agent 包（模型面对契约），**body 由表面拥有者安装** —— `SetChatSurface`（`internal/agent/chat_surface.go:450`），调用点 `cmd/agent/chat_surface_surface.go:409`，`return func() { ag.SetChatSurface(nil) }`（`:410`）
- 挂到 chat wire 的唯一入口：`chatOpsToolDefs()`（`internal/agent/chat_ops.go:412-421`）
- chat-only 集合：`chatScopedToolDefs()`（`internal/agent/chat_ops.go:432-434`），今天是 `return []deepseek.ClaudeToolDef{chatSurfaceToolDef}`
- **只有在装了实体时才上线**：`if a.chatSurface.surface != nil { defs = append(defs, extra...) }`（`chat_ops.go:416-418`）。注释写明理由：「advertising a capability nothing can answer trades a sentence the model can act on for a call it can only lose」。未安装时每个 action 返回 `chatSurfaceUnavailableNotice` —— **一句话，绝不是静默空成功**（`chat_surface.go:38-41`）
- 门：`TestChatSurface_ToolDefOnlyOnTheChatWire` 断言两个列表只差这一个名字；`TestGate_ToolDispatchCoversEveryDef` 需要 **两条** wire 的词汇表，这正是 `chatScopedToolDefs()` 作为函数而不是 inline append 的原因（`chat_ops.go:423-431`）

`clarify` 落进这套模板意味着：**`chatScopedToolDefs()` 从 1 个元素变成 2 个**，`claudeToolDefs` 一个字节不动。

### 2.3 交互模型：从"阻塞回调"改成"面板是一轮的产物，答案是下一轮的输入"

这是与 Hermes 最大的结构差异，必须讲清楚。

Hermes 的 clarify 回调在 agent 线程里**阻塞**，等用户答完再返回 —— 因为 gateway 有 worker 线程池、CLI 有独立的 prompt_toolkit 事件循环，两条路都能容忍一个阻塞线程。

gopi chat 不能这么干：`cmd/agent/cli_chat.go:1576` 的 feeder 是**唯一**读 stdin 的 goroutine，主循环在 `lineCh` 上串行消费并跑 turn。让 turn 内部阻塞等一行输入，等于让主循环既跑 turn 又读输入 —— 那正是 `chat_surface` 的 `type` action 已经解决的问题（`internal/agent/chat_surface.go:64`：`type` 把一行文本**排队为下一轮输入**，不阻塞当前轮）。

所以 gopi 的形状是：

```
turn N:  model 调 clarify(question, choices)
         → 面板行 commit 进 band（问题 + A/B/C/D 行 + Other 行）
         → clarify 工具立刻返回 [gopi] 语义的确认句（"面板已出，等你的选择"）
         → turn N 结束
         ← 用户按 A / 方向键+Enter / 直接打自由文本
turn N+1: read loop 把这次按键结算成一行输入，作为下一轮的用户消息
         → 超时落到 turn N+1 之前：read loop idle 到 deadline → 写 notice → 自动提交一行
```

这有几个直接好处：

1. **不需要在 turn 内阻塞任何 goroutine**，write-boundary 纪律（`chat_write_boundary.go`）原样成立。
2. **超时的落点天然在 read loop**，而不是 turn 里 —— read loop 本来就在等输入，加一个 deadline 是它的自然扩展。
3. 与 `chat_surface` 的 `type`/`key` 复用同一条注入路径（`internal/lineedit/inject.go:511` `InjectKey` / `:530` `InjectKeyBounded` / `:725` `InjectKeys`）。
4. 面板行是 **band 里的持久行**，用户滚动回看时它还在 —— 这正是 `persist_prompts`（`cli.py:466-469`）想达到的效果，gopi 免费得到。

代价（必须诚实列出）：**clarify 的答案会作为一条新的用户消息进入对话**，而不是像 Hermes 那样作为同一次工具调用的返回值。所以工具返回值里必须明确写"这一票由你在**下一轮**读到的用户消息给出"，否则模型会以为同轮内拿得到答案。

### 2.4 渲染：面板 = band 行；选择 = editor 的候选菜单

**问题行 + 选项行 → answer band**。落点：

- `Editor.WriteBandRow(row string) bool`（`internal/lineedit/editor.go:888`）—— 一次一行，caller 自己知道换行在哪，行宽超 band 由 caller 用 `lineedit.WrapToCols` 按 `Editor.BandCols` 预折（`editor.go:828-836`）。这正是面板需要的语义：**面板的每一行都是我们自己切好的**。
- `Editor.Note(text string, st Style) bool`（`internal/lineedit/editor.go:948`）—— 带样式的行，是 notice 家族的路径。
- `region.WriteLines(text string) bool`（`internal/lineedit/region.go:921`）—— 多行正文（`/help` 面板、slash 命令打的表格），trailing break 丢弃。
- `chatInput.WriteNotice`（`cmd/agent/lineedit_wire.go:395-405`）：有 band → `editor.WriteLines`；没有 → stderr。**降级路径已经存在**，cooked 模式下 clarify 退化成"在 stderr 打一段编号文本 + 等用户下一行"，与 Hermes 的 text fallback（`clarify_gateway.py:40-46`）同形。

**选择态 → 复用 editor 的候选菜单**，而不是新做一套：

`internal/lineedit/menu.go`（`Menu` 在 `:88`，`Rows()` 在 `:108`，上限 `completionCandidateLimit` `:35`，每页 `completionPageRows` `:39`）已经具备面板需要的全部性质：

- 候选列表是**原子快照**（`menu.go:16-24` 注释：key loop 只读最后发布的候选列表）
- `Rows()` 是**已经画出去的**菜单占的行数（`menu.go:31-35`），cmd/agent 直接读它来预留行 —— 面板的倒计时行/Other 行预留同理
- 单候选时多占一行 note row（`menu.go:47-59`）—— "Other" 行的形状先例
- Up/Down 选择、Escape 取消的按键语义已经在 `keys.go` 里（`KeyEscape` `keys.go:52-55`，lone-ESC 歧义由 `BareEscape`/`FlushEscape` `keys.go:150-166` 解决）

**字母快捷键（A/B/C/D）**：借鉴 `clarify-tool.tsx:40-43` —— 徽章同时是提示和快捷键。gopi 的 decoder 已经能把裸 rune 当作 key 处理，画 "A."/"B." 前缀并在 editor 里按 rune 匹配即可，不需要新的按键类型。

**倒计时行**：走 `lineedit/notify.go` 的 wake pipe。非 read-loop 的写者 `PublishStatus` → 写 1 字节进 wake pipe → read loop 的 poll(2) 醒来画。倒计时每秒 publish 一次，**不新增第二个写者**（`notify.go:11-25` 的"每个终端写只有一个 owner = read loop"纪律不破）。零字节关闭路径：`MonoPalette` 是零值（记忆 #5707，`internal/lineedit/palette.go`），格式化输出全走 palette.go —— 面板行在 `--no-color` 下必须是**纯文本零 ESC 字节**。

### 2.5 超时与中断

| 情形 | gopi 语义 | 落点 |
|---|---|---|
| 用户按时答 | 那一行就是答案 | read loop 已有 |
| 超时未答 | band 里补一行 `[gopi] clarify timed out after Ns — deciding with the model's best judgement`；自动提交一行语义化输入（见下） | read loop 的 idle deadline |
| 用户取消（Ctrl+C / Esc） | band 补 `[gopi] clarify cancelled — the model will decide` | `Editor` 的 Escape 分支 + `Agent.Interrupt()`（`internal/agent/loop.go:3239`）/`InterruptRunBy`（`:3513`） |
| 面板根本没画出来（无 band 且 stderr 不可写） | 工具直接返回 `[gopi] clarify could not be presented …` | 与 `gateway/run.py:18247` 同形 |

**超时自动提交的那一行必须自解释**。直接抄 Hermes 的措辞是有理由的（`cli.py:11725-11727`）：

```
The user did not provide a response within the time limit.
Use your best judgement to make the choice and proceed.
```

理由：这条消息会永久留在 transcript 里。写 "timeout" 模型不知道该怎么办；写"用你的最佳判断"模型知道下一步，而且**这是诚实的** —— 我们明确告诉它这一票是它投的，不是用户投的。

**取消是另一档，不是超时的一档**（`cli.py:12122-12127`）：`The user cancelled. Use your best judgement to proceed.`

### 2.6 中断安全：gopi 侧要做的第一件事

`internal/agent/loop.go:3239 Agent.Interrupt()` 只有 ctx cancel + latched bool；`InterruptRunBy(reason)`（`:3513`）带原因。Hermes 的教训（`cli.py:12103-12126`）是：**中断时，任何等在那里的事件循环/队列必须收到一个终值，然后状态必须清干净，否则输入层被永久 gate 住**。

对照 gopi：面板状态如果放在 editor 里（`clarifyPanel` 之类），Ctrl+C 必须做三件事：① 写取消 notice 到 band；② 清面板状态（连同预留行一起结算）；③ 让 read loop 回到普通提示符。**这三步缺任何一步的等价物就是 #14026 那个"冻结终端"**。

gopi 侧还有一条 Hermes 没有的硬约束：预留行的**释放必须结算 display cell**。候选菜单已经有 `Menu.Rows()` 作为"已经画了几行"的唯一真源（`menu.go:31-35`），面板必须同样在**画之前**就把行数写在一个地方，否则 `region` 的重绘会把行算错（记忆 #5707 的 "SGR 字节被当成列宽" 家族缺陷就是这一类）。

---

## 3. 四问的最终取舍（gopi 版）

**(1) 何时强制用户决策？**

采用 Hermes 的经济学判据，并把它**写进工具 description**（而不是写进系统 prompt —— 见 §4）：

> 当且仅当"猜错的代价 > 问一次的代价"时才问。低风险决策自己选并说明。危险命令的确认走已有通道，**不走 clarify**（gopi 的等价物是 bash 工具的既有确认语义，不在这里改）。

具体化到 gopi chat 的三种典型场景：① 用户请求有**多条互斥的实现路线**（改 A 还是改 B）；② 需要**破坏性动作**的授权（删/覆盖/force push）；③ 用户自己要的决策（"这两种风格你选哪种"）。**不问**：能读文件知道的、有唯一合理默认的、纯风格偏好且可事后改的。

**(2) 失败/超时取什么默认？**

**不取"第一个选项"，取"把决定权交还模型 + 给出可解释的理由文本"。** 三条理由：

- 平台硬编码"选 A"= 把平台的偏好冒充用户的偏好，而且 transcript 里看起来像用户选了 A（可解释性归零）；
- Hermes 三档文案（超时/取消/投递失败）都只描述**发生了什么**，不选边（`cli.py:11725-11727`、`cli.py:12122-12127`、`gateway/run.py:18247`、`gateway/run.py:18253`）；
- 模型下一轮能读到这句话，它会说出"因为你没答，我按 X 做了，理由是 Y" —— 这才是用户能反驳的东西。

**(3) 首选项是否可默认执行？**

**不。gopi 的 clarify schema 不允许 `default`/`recommended` 字段。** Hermes 也没有（`clarify_tool.py:169-205` 整个 schema 只有 question/choices 两个字段）。理由：(2) 已经说明——一旦有"默认执行的首选项"，超时和"用户主动选了它"在对话记录里不可区分。如果模型想推荐，它应该在 `question` 文本里**说明推荐理由**（"我倾向 3，因为…"），而不是让平台在超时路径上偷偷替它投一票。**推荐是说出来的，不是执行出来的。**

**(4) 为什么还要问？**

因为 gopi chat 今天的两条路（散文里写问题 / 直接猜）**都没有留下可结算的记录**。clarify 的价值不是"多一个输入框"，而是：① 决定被**结构化**地呈现（选项是可选行，不是正文里的死文本）；② 决定的**寿命有边界**（倒计时 + 明确的超时语义）；③ 决定的**归属可分辨**（用户选的 / 超时由模型判断的 / 被取消的，三档文案不同）；④ 决定**留在 band 里**（滚动回看还在，不随重绘消失）。

---

## 4. 落地路径（最小可实施）

**前置声明：本路径实施时，仍然不改系统 prompt（`promptSections` / 密度门），不改 `claudeToolDefs`。**

理由：Rule 3 与 constraint 6 都要求"改prompt或工具定义前先有文档+报告"。本文件就是那份文档，但它**只覆盖 chat-scoped 工具这一条路** —— 而 chat-scoped 工具按定义不进 `claudeToolDefs`，所以实施它不触发 constraint 6。系统 prompt 侧**一个字都不动**：工具的触发时机完全由工具自己的 description 表达（Hermes 就是这么做的，`clarify_tool.py:155-168`）。

阶段划分（每个阶段 = 一个 session）：

**P1 — 面板行（无工具，无倒计时）**
定义 chat-scoped tool `clarify`，只做"画面板 + 返回确认句"。选择通过**下一轮用户输入**结算（用户打 "2" 或直接打自由文本，模型自己读）。
验收：面板行出现在 band 里；cooked 模式降级 stderr 编号列表；`chatScopedToolDefs()` 返回 2 个 def；`TestChatSurface_ToolDefOnlyOnTheChatWire` 的等价测试断言两条 wire 只差 `chat_surface` + `clarify` 两个名字；`claudeToolDefs` 的 diff 为空。

**P2 — 选择态（复用菜单）**
把选项渲染成 editor 的候选菜单（`Menu`/`Rows()`/`completionPageRows`），支持 Up/Down + 字母快捷键 + Escape 取消。答案由 editor 结算成一行输入。
验收：Escape 取消 = 取消档文案；`Menu.Rows()` 与面板实际占行在**每个终端宽度**下一致（含窄终端 `RowsMinUsable()` 拒绝路径）。

**P3 — 倒计时**
`lineedit/notify.go` 的 wake pipe 每秒 publish 一次倒计时行。超时 → band 补超时 notice + 自动提交一句自解释输入。
验收：零字节关闭路径（`--no-color` 下面板输出 0 ESC 字节，与记忆 #5707 的 byte-level 验证法一致）；倒计时不新增第二个终端写者。

**P4 — 中断安全**
Ctrl+C / `Interrupt()` 走取消档：写 notice、清面板状态、释放预留行、read loop 恢复普通提示符。补一条最小回归测试（Hermes #14026 的等价缺陷：输入层被 gate 住）。
验收：中断后立即能再打字，**不等**超时；预留行结算后 band 行数与实际一致。

---

## 5. 与 normal output area 的取舍

**面板占 band，不占"答案区的下半部分"，也不新开一个区域。** 取舍与理由：

| 选项 | 否决理由 |
|---|---|
| 新开一个 terminal region | gopi 的布局是 band + editor 两段（`region.go` 的 reserved layout）。第三个区域要重做 `rowsMinUsable` 的预算与 `region` 的重绘账本，而面板只是**几行**，不值这个复杂度 |
| 覆盖答案区（模态） | Hermes 的 CLI 是这么做的（`cli.py:5006-5011` gate 住输入区）。但 gopi 的零字节关闭路径与"每个写只有一个 owner"纪律建立在**没有模态覆盖**之上。模态会让 `region` 的重绘账本多一个状态 |
| 只写 stderr（不占 band） | 用户滚动回看时面板消失，等于回到"散文里写问题"。这正是要修的缺陷 |
| **占 band（采纳）** | 面板是 transcript 的一部分，滚动回看还在；复用 `WriteBandRow`/`WriteLines` 的既有结算规则；预留行由 `Menu.Rows()` 式的唯一真源决定；关闭 = 提交几行普通 band 行，**没有新状态要拆** |

代价：面板会**推走**答案区的行（band 向上滚）。可接受 —— 一次 clarify 只有 5~7 行（问题 1 + 选项 ≤4 + Other 1 + 倒计时 1），且 Hermes 的桌面端同样选择"inline，不遮挡"（`clarify-tool.tsx:56-60`）。

---

## 6. Hermes 可借鉴性总评

**可直接借鉴（有直接等价物）**：
- schema 形状与反向边界文案（`clarify_tool.py:169-205`）—— gopi 的 def 可以近乎照译
- 平台无关入口做归一化（`_flatten_choice`，`clarify_tool.py:24-59`）—— LLM 会吐 dict 形状选项，这是**实测**的，不是假想
- 三档结果文案（`cli.py:11725-11727` / `cli.py:12122-12127` / `gateway/run.py:18247`）—— 逐字可用
- 中断必须推终值 + 清状态（`cli.py:12103-12126`）—— gopi 侧的一等约束
- 非 read-loop 写者走 wake pipe（对应 `lineedit/notify.go`）
- 决策留痕（`persist_prompt_summary`，`cli.py:11702`）

**必须重写（形状不匹配）**：
- 阻塞式同步回调 —— gopi 是单 read loop，改成"产物 + 下一轮输入"（§2.3）
- prompt_toolkit 模态面板 —— 映射到 band + 菜单，不做模态（§2.4、§5）
- gateway 的 `threading.Event` + activity touch —— gopi 无 gateway watchdog，简化为 read loop 的 deadline

**不借鉴（明确）**：
- 三端三个不同的超时默认值（120s / 3600s）—— gopi 只有 chat 一个表面，**一个常量**，`GOPI_CHAT_CLARIFY_TIMEOUT_SEC`（Rule 2 单一入口）。Hermes 的两个值是因为"人在终端前"和"人在手机上"是两个节奏，gopi 没有这个分裂
- `tui_gateway` 的 JSON-RPC `clarify.request`/`clarify.respond` 桥 —— gopi 的 chat 没有独立 web/desktop 端在同一次会话里，不需要跨进程 resolve

---

## 7. 风险

1. **模型对"跨轮"契约的误解**（最高风险）。工具同轮返回的不是答案而是"面板已出"，如果 description 没写清楚，模型会在同一轮里等一个不存在的答案。缓解：def 的 description 明确写"用户的答复将作为你的**下一轮**用户消息到达；本轮不要重复询问"。
2. **预留行结算漂移**。面板占几行必须与 `Menu.Rows()` 同源，否则窄终端/缩放时 region 重绘错行。缓解：P2 的验收就是逐宽度对账。
3. **超时把用户正在打的字吃掉**。用户很可能已经在编辑行里打自由文本了，此时超时自动提交会与他的输入打架。缓解：**用户开始输入即取消倒计时**（这是 Hermes 没有暴露但 gopi 必须处理的场景 —— Hermes CLI 的模态面板 gate 住输入区所以不存在这个 race）。
4. **倒计时干扰读答案**。缓解：只画一行、只在 band 末端、`--no-color` 下纯文本。
5. **工具滥用**。模型可能把 clarify 当"确认闸"用（每个动作问一次）。缓解：description 的反向边界 + 只允许 chat wire（task path 根本没有这个工具）。

## 8. 不做什么（本次明确排除）

- **不改系统 prompt**（`promptSections` / 密度门）—— 触发时机由工具 description 承担
- **不加 `claudeToolDefs` 条目** —— Rule 3；只走 `chatScopedToolDefs()`
- **不做多问题批量 clarify** —— Hermes 也只能一次一问（`clarify_tool.py:169-205` 单 `question`）
- **不做 `default`/`recommended` 字段** —— §3(3)
- **不改 bash 的破坏性命令确认语义** —— `clarify_tool.py:166-168` 明确把这条划出去；gopi 同理，本期不碰
- **不做 gateway/web/desktop 端** —— 只有 chat 表面
- **本次不写代码** —— 交付物是文档 + 落地清单
