<!-- doc-class: normative -->
# gopi chat clarify — 落地清单（D2）

配套文档：`devdoc/gopi-chat-clarify.md`（设计 + 决策）。
本文件只回答一件事：**实施时要动哪些文件、哪些行、以及动不得的是什么。**

---

## 0. 硬性前置声明（实施时逐条自检）

| # | 声明 | 门禁 |
|---|---|---|
| 1 | **不新增 `claudeToolDefs` 条目。** `clarify` 只进 chat wire。 | `TestGOPIRule3_MatchesClaudeToolDefs`（17 条）+ `TestToolClassesCoverTheToolSurface` 必须仍绿；`git diff <base> -- internal/agent/loop.go` 中 `claudeToolDefs` 字面量 diff 为空 |
| 2 | **不改系统 prompt。** 不碰 `promptSections` / 密度门 / `assembleSystemPrompt`。触发时机全部由工具自己的 description 承担（Hermes 同法：`~/work/hermes-agent/tools/clarify_tool.py:155-168`）。 | `git diff` 中 `promptSections`、`assembleSystemPrompt` 零命中 |
| 3 | **只有在 `SetChatSurface` 装了实体时才上线**，与 `chat_surface` 同一条判据。 | `internal/agent/chat_ops.go:416-418` 的 `a.chatSurface.surface != nil` 守卫必须同时覆盖 `clarify` |
| 4 | **零字节关闭路径**：`--no-color` 下面板输出 0 ESC 字节。 | byte-level 验证（记忆 #5707 的方法：把真实非 TTY stdin 灌进发布二进制，断言 0 个 ESC 字节） |
| 5 | 若实施过程中**被迫**改系统 prompt 或工具定义 → **停下，先出 change 报告**。 | constraint 6 |

---

## 1. 工具面（agent 包）

| 落点 | 现状 | 要做什么 |
|---|---|---|
| `internal/agent/chat_surface.go:22-29` | `chat_surface` 的 def 定义在 agent 包、body 由表面拥有者安装的**先例与理由**（Rule 3 回避路径） | 照抄这个模板新建 `clarify` 的 def 文件（建议 `internal/agent/chat_clarify.go`） |
| `internal/agent/chat_surface.go:37-41` | 未安装实体时返回 `chatSurfaceUnavailableNotice`（一句话，绝不静默空成功） | `clarify` 必须有等价 notice，例如 `chatClarifyUnavailableNotice` |
| `internal/agent/chat_surface.go:450` | `SetChatSurface(s ChatSurface)` —— body 安装点 | 同形新增 `SetChatClarifyPanel(...)`；判据是"面板实体存在"，不是"tool def 存在" |
| `internal/agent/chat_ops.go:412-421` | `chatOpsToolDefs()` —— 构建 chat wire 的**唯一**入口；`:416-418` 是 surface 判据 | 在这里把 clarify 的 def 一起 append（已被同一个 `extra` 切片覆盖，无需第二处） |
| `internal/agent/chat_ops.go:432-434` | `chatScopedToolDefs()` 今天返回 `[]deepseek.ClaudeToolDef{chatSurfaceToolDef}` | **一个元素变两个**：`{chatSurfaceToolDef, chatClarifyToolDef}` |
| `internal/agent/chat_ops.go:423-431` | 为什么必须是**函数**而不是 inline append：def↔dispatcher 门需要两条 wire 的词汇表 | 新增 def 后，`TestGate_ToolDispatchCoversEveryDef` 会自动把它算进词汇表 —— 必须有对应的 dispatcher 分支，否则该门红 |

**必须新增的 dispatcher 分支**：`chatScopedToolDefs()` 的每个名字都必须有 dispatch arm（见 `chat_ops.go:423-431` 的注释所述门禁）。落点与现有 chat_surface 的 dispatch 处同一 switch。

## 2. 工具 schema（模型面对契约）

| 要素 | 取法 |
|---|---|
| `question: string` (required)、`choices: array<string>` (`maxItems: 4`, 非 required) | 直接对应 `~/work/hermes-agent/tools/clarify_tool.py:169-205` |
| 省略 `choices` = 开放式 | `clarify_tool.py:148-153` |
| 选项归一化（dict 形状解包 `label → description → text → title`，排除 `name`/`value`） | `clarify_tool.py:24-59` `_flatten_choice` —— 这是**实测**的 LLM 行为，不是假想，必须抄 |
| 溢出裁剪 `choices[:4]`；空列表 → 退化为开放式 | `clarify_tool.py:98-103` |
| description 必须包含：① 反例（选项写进 `question` 会渲染成死文本）② 正向触发清单 ③ 反向边界（低风险自己选、危险确认不用它） ④ **gopi 特有的跨轮契约**："用户的答复将作为你的**下一轮**用户消息到达；本轮不要重复询问" | ①②③ 照译 `clarify_tool.py:155-168`；④ 是 gopi 新增，见设计文档 §2.3 / §7.1 |
| **不提供** `default` / `recommended` 字段 | 设计文档 §3(3) |

## 3. 渲染（internal/lineedit）

| 落点 | 现状 | 用途 |
|---|---|---|
| `internal/lineedit/editor.go:888` `Editor.WriteBandRow(row string) bool` | 一次一行，caller 已切好；行宽超 band 由 caller 用 `WrapToCols`+`BandCols` 预折（`:828-836`） | **面板行主路径**（问题行、选项行、Other 行、倒计时行） |
| `internal/lineedit/editor.go:948` `Editor.Note(text string, st Style) bool` | 带样式的 band 行 | 面板的强调样式（"这一行在等你"） |
| `internal/lineedit/region.go:921` `region.WriteLines(text string) bool` | 多行正文，trailing break 丢弃 | 一次性提交整块面板（cooked 之外的整块路径） |
| `internal/lineedit/menu.go:88` `Menu` / `:108` `Rows()` / `:35` `completionCandidateLimit` / `:39` `completionPageRows` / `:47-59` 单候选 note row | 候选列表 = 原子快照；`Rows()` 是**唯一真源**的已画行数；单候选多占一行 note row 的**形状先例** | **选择态复用**：`Rows()` 式真源决定面板预留行；note row 是 "Other" 行的先例；`completionPageRows` 是选项行的分页先例 |
| `internal/lineedit/keys.go:52-55` `KeyEscape`、`:150-166` `BareEscape`/`FlushEscape` | lone-ESC 与 escape-sequence 前缀的歧义已在 decoder 解决 | Escape 取消档 |
| `internal/lineedit/inject.go:511` `InjectKey` / `:530` `InjectKeyBounded` / `:725` `InjectKeys` | 把这个表面自己的按键注回**同一个** Source/decoder | 面板的选择结算可复用；也是超时自动提交的候选路径 |
| `internal/lineedit/notify.go:11-25` wake pipe（1 字节，`wakePipeBytes` `:29`）；`Editor.SetNotification` `editor.go:960`；`Editor.NotifyEnabled` `editor.go:984` | **非 read-loop 写者 → poll(2) 唤醒 read loop 画**；"每个终端写只有一个 owner" | **倒计时 line**：每秒 publish 一行，不新增第二个写者 |
| `internal/lineedit/palette.go`（`MonoPalette` 是零值） | 零值即关闭路径，无分支 | 面板所有格式输出必须过 palette.go |
| `internal/lineedit/editor.go:296`（区域写入时 caller 自己的换行开始时新 band row） | reserved layout 的行规则 | 面板行释放时按同一规则结算 |

## 4. chat REPL 接线（cmd/agent）

| 落点 | 现状 | 要做什么 |
|---|---|---|
| `cmd/agent/cli_chat.go:1576` | feeder goroutine 阻塞在 `input.ReadLine()`，主循环消费 `lineCh`（`lineRead` 结构在 `:1540-1552`） | **超时的唯一自然落点**：read loop idle 到 deadline → 写超时 notice → 自动提交一句自解释输入 |
| `cmd/agent/lineedit_wire.go:163` `chatInput.ReadLine()` | editor 模式走 `editor.Run()`；cooked 模式走 `bufio` | 面板激活时的按键路由（选择态）要在这里或 editor 内部结算 |
| `cmd/agent/lineedit_wire.go:395` `chatInput.WriteNotice(text)` | 有 band → `editor.WriteLines`；否则 stderr。**降级路径已存在** | 超时/取消/不可用三档 notice 都走它 —— cooked 模式自动退化成"stderr 编号文本 + 等下一行"，与 Hermes 的 text fallback 同形（`~/work/hermes-agent/tools/clarify_gateway.py:40-46`） |
| `cmd/agent/lineedit_wire.go:420` `chatInput.WriteToolRow(line) bool` | 已折行的**单行**提交 | 选项行的可行路径之一（caller 已切好行） |
| `cmd/agent/chat_surface_surface.go:47-90` | `[gopi] …` notice 常量家族（`chatSelfDrivenTurnNotice`/`BoundNotice`/`RenewalNotice`/`chatSurfaceArmedNotice`/`chatSurfaceUnarmedNotice`…） | **三档 clarify 文案按同一格式新增常量**（`[gopi] clarify …`），Rule 1：常量集中，不散落字面量 |
| `cmd/agent/chat_surface_surface.go:409-410` | `ag.SetChatSurface(s)` / `return func() { ag.SetChatSurface(nil) }` | 面板实体的安装/卸载**同形**（安装返回一个卸载闭包） |
| `cmd/agent/lineedit_wire.go:100-150` `chatInput` 结构体 | 持有 `editor` / `injector` / `palmer` / `dumper` | 面板句柄放这里（若需要 REPL 侧可见） |
| `cmd/agent/lineedit_wire.go:485` `in.editor.LayoutGeometry()` / `:631` `chatRegionMinRows()` → `lineedit.RowsMinUsable()` | 窄终端拒绝路径 | 面板必须走同一个拒绝判据（放不下就降级到 stderr 文本） |

## 5. 超时 / 中断

| 落点 | 现状 | 要做什么 |
|---|---|---|
| `internal/agent/loop.go:3239` `Agent.Interrupt()` | ctx cancel + latched bool | 取消档的一环（写取消 notice、清面板、放行输入） |
| `internal/agent/loop.go:3513` `Agent.InterruptRunBy(reason)` | 带原因的硬中断 | 取消档的**首选**入口（reason 可用于文案分档） |
| `cmd/agent/lineedit_wire.go` `SetInterrupt`（`~:200`，per-turn interrupt seam，报"哪个输入携带了这个键"） | 已存在 | 面板激活时 Ctrl+C 的分派 |
| 超时常量 | 无 | **单一入口**：`GOPI_CHAT_CLARIFY_TIMEOUT_SEC`（Rule 2）。**不要**抄 Hermes 的 120s/3600s 双值 —— gopi 只有 chat 一个表面，一个常量 |
| 超时自动提交的文案 | 无 | 照译 `~/work/hermes-agent/cli.py:11725-11727`："The user did not provide a response within the time limit. Use your best judgement to make the choice and proceed." |
| 取消档文案 | 无 | 照译 `~/work/hermes-agent/cli.py:12122-12127`："The user cancelled. Use your best judgement to proceed." |
| 中断安全（Hermes #14026 的等价缺陷） | — | **必须补最小回归测试**：中断后输入层立即可用，**不等**超时；面板预留行结算后 band 行数与实际一致（Hermes 的教训：`~/work/hermes-agent/cli.py:12103-12126` —— 中断必须给每个等着的队列推一个**终值**再清状态，否则输入被永久 gate） |

## 6. 测试落点

| 测试 | 断言 |
|---|---|
| `TestClarify_ToolDefOnlyOnTheChatWire`（对标 `TestChatSurface_ToolDefOnlyOnTheChatWire`） | 两条 wire 的 def 列表之差**恰好**是 `chat_surface` + `clarify` |
| `TestGOPIRule3_MatchesClaudeToolDefs` | **不改**，必须仍绿（证明没偷偷加第 18 个工具） |
| `TestToolClassesCoverTheToolSurface` | **不改**，必须仍绿 |
| `TestGate_ToolDispatchCoversEveryDef` | clarify 的 dispatch arm 存在（否则此门红 —— 见 `chat_ops.go:423-431`） |
| 面板行数对账测试（逐终端宽度） | `Menu.Rows()` 式真源 == 实际占用的 band 行；含窄终端 `RowsMinUsable()` 拒绝路径 |
| 零字节关闭测试 | `--no-color` 下把真实非 TTY stdin 灌进发布二进制，端到端断言 0 个 ESC 字节 |
| 中断回归测试 | 中断后立即可输入；预留行已释放 |
| 选项归一化单测 | dict 形状选项 → 正确解包；`name`/`value` 被排除；>4 项被裁；空列表退化开放式 |

## 7. 建议的阶段边界（每阶段 = 一个 session）

- **P1** 面板行（无倒计时、无选择态）：新 def + `chatScopedToolDefs()` 扩容 + notice 常量 + 面板提交 + 降级路径 + 测试落点 1/2/3/4
- **P2** 选择态：复用 `Menu`/`Rows()` + 字母快捷键 + Escape 取消 + 行数对账测试
- **P3** 倒计时：`notify.go` wake pipe publish + 超时 notice + 自动提交 + 零字节关闭测试
- **P4** 中断安全：取消档 + 中断回归测试

四阶段都**不碰**系统 prompt 与 `claudeToolDefs`；P1 之外的每一阶段都可以独立停下来。

## 8. 明确不动的文件

- `internal/agent/loop.go` 的 `claudeToolDefs` 字面量、`promptSections`、`assembleSystemPrompt`
- `internal/deepseek/claude_client.go:481`（`claudeToolDefs` 是 package-global，被复制前不得变长）
- bash / 破坏性命令的现有确认语义（Hermes 同样把这条划给 terminal 工具：`~/work/hermes-agent/tools/clarify_tool.py:166-168`）
- 任何 gateway / web / desktop 端
