<!-- doc-class: normative -->
# devdoc — gopi web/chat 对齐 P1（CLI 命令暴露 + web 截图上传，R4 + R5）

- 分支：`feat/web-align-p1`（worktree `~/workspace/gopi-web-align-p1`，基于 master 2b5a562）
- 主 commit：`642e9bc`（R4+R5 实现，9 files, +1980/−49）+ vision_test 宿主隔离修复（35df2a9）+ gofmt/杂项
- 日期：2026-08-29
- 输入：`~/workspace/gopi-chat-web-align/design-chat-web-alignment.md`（R4/R5 + §3 P1 验收清单）、`devdoc/web-align-p0.md`（先例模式）

---

## 1. 方案 vs 设计文档（R4/R5）：一致 + 三处实施收敛

### 1.1 R4 共享命令表 —— 与设计一致，落点 `cmd/agent/commands.go`

设计建议的形态原样落地：`map[string]*Command{Name, Usage, Run(args, ctx) (string, error)}`，
chat 与 web 查**同一张表**（`execSlashCommand` 单一入口），命令直调 `internal/*` 引擎函数
（零子进程）。第一波命令集与设计完全一致：

| 命令 | 引擎（与 CLI 同源） |
|---|---|
| `/task list[--status S] / show <id> / new "<s>" / update <id> / search <q>` | `tools.TaskList/Show/New/Update/Search` |
| `/log last [N] / sessions / summary <id> / read <id> / search <q>` | `gopilog.LoadAllSummaries / ParseFile / FormatSummary(Table) / FormatSession / FormatGrep`（+ `FindSession` 短 id） |
| `/text2img <text> [--out P] [--size N] [--cols N]` | `tools.Text2Img`（文本先落临时源文件——引擎吃文件路径） |
| ~~`/code-search <symbol>`~~ | **2026-11-07 退役**（操作员裁决：chat 是 agent 入口，不是 IDE/copilot）：引擎 `tools.CodeSearch` 与 TOOL `code_search` 都还在，`gopi tool code-search` 也还在——退掉的只是把这个工具的结果打印给人看的斜杠命令。见下 |
| ~~`/memory search <q> / store "<t>" "<q>" "<a>" / get <id> / list`~~ | **2026-11-07 退役**，同一个裁决。`tools.MemorySearch/Store/Get/ListRecent` 与 TOOL `memory` 全部保留；`tools.MemoryListRecent` 仍在（`gopi memory list` 用它）。见下 |
| `/help [cmd]` | 表自身渲染（列出全部命令 + 每命令的 Usage 首行） |

**两者的退役记录（2026-11-07）**：两个命令连同 `commandAliases` 里的 `codesearch` /
`searchcode` / `mem`、各自的 Run 函数与 Usage 常量一起删除；敲 `/code-search` 或
`/memory` 现在走共享表的**未知命令拒绝**（`slashUnknownCommandError`），并且不再出现在
`/help` 的渲染里。`splitQuotedArgs`（被 `cli_tasks.go` 的 ssh argv 走查调用）与
`slashSearchRoot`（被 `cli_web.go` 的 @ 菜单调用）按 Rule 43 保留，注释里对已删命令的
引用已改为各自的活消费者。这条裁决的依据是**两扇门的分工**：斜杠命令是「人让本地引擎
把结果打给人看」，TOOL 是「模型自己取回结果」——gopi 要的是后者。

会话态命令（`/quit /clear /tools /help`）保留在 chat REPL 本地 switch——它们操作
`chatSession`，共享表看不到；web 侧无需对应物（无 tools 翻转键——R3 范围）。

**实施收敛 1（别名表）**：设计没提；加了 `commandAliases`（codesearch/searchcode→code-search、
mem→memory、t2i→text2img、tasks→task、logs→log）。别名只进查找不进 help 表（避免双份文档漂移）。

**实施收敛 2（命令输出不进 session JSONL）**：设计只规定"不进模型上下文/不进 history"。
实施把 session JSONL 日志也跳过（`serveSlashCommand` 不写 user/assistant 事件）：命令输出可从
其事实源（tasks 表、session 文件、PNG 文件）精确复现，写进 JSONL 只会制造"伪对话轮"。
这是 R2 工具帧"pure display"语义的自然延伸。

**实施收敛 3（SSE 协议面）**：设计说"结果作为 SSE event:command 帧 + 渲染为对话节点"。
实施细节：命令帧是**终端帧**——slash 轮不发 `event: done`（浏览器把 command 帧当终点，
不再开 assistant 气泡）；未知命令也是 200 + command 帧（error 字段），不是 HTTP 4xx——
流协议统一，页面解析器零特判。SSE headers（text/event-stream + 首帧 Flush）在 slash 分流
**之前**发出，两条路径同一响应头。

### 1.2 R5 web 截图上传 —— 与设计一致；一处管线选择 + 一处超界声明

设计管线逐条落地：

- **请求体**：`webChatRequest.Images []webChatImage{media_type, data_b64}`（字段名镜像
  Anthropic wire 的 image source，浏览器转发 data URI 时近零转换）；
- **校验**：`validateWebImages` 在拿锁/起 agent **之前**——media type 白名单
  （png/jpeg/gif/webp，= `visionMediaTypes` 同集）、base64 可解、解码后 ≤32MiB
  （= `LoadImage` 同预算，一条规则两个入口）、≤8 张/条。失败 4xx + 可读错误；
- **agent 侧构造零改动（设计判断正确）**：`Chat`/`ChatOps` 加 variadic
  `images ...*deepseek.ClaudeImageSource`（镜像 `Run` 的签名先例），轮的 user 消息
  `Image: images[0]` + 文本，多图第 2+ 张各占一条 user 消息——wire 侧
  （claude_client.go user case）原样复用：image block 先行、text 跟随、**带图消息永不合并**；
- **模型切换**：设计建议 turn 级覆盖 `ag.Model`，实施确认 `s.mu` 域内串行，轮前换
  `s.visionModel`（启动时 `ResolveVisionModel(cfgForVision())` 解析一次）、
  `defer` 恢复。GLM `vision_model=glm-5.3-flash` 实测直通（见 §3）；
- **图片只挂当前轮**：图片随 turn 进 `s.history`（session JSONL 序列化保留），但
  `/api/transcript` 回放**不重发像素**（`has_image` 标记）——刷新回放轻量化。

**实施收敛（base64 尺寸与 body 上限的关系，写进测试注释）**：33MiB 解码图的 base64
约 44MiB，必超 4MiB body 上限 → JSON 解码层就 4xx（`unexpected EOF`）。所以 32MiB
引擎级上限只在 body 上限提高后才成为实际瓶颈；测试用 `validateWebImages` 直测保证该
规则独立于 body 上限被锁定。

---

## 2. 改动文件清单

| 文件 | 改动 |
|---|---|
| `cmd/agent/commands.go`（新） | R4 核心：Command 表 + 别名 + `execSlashCommand`/`slashHelpText` + task/log/text2img/code-search/memory 六命令实现（每命令 Usage 常量） |
| `cmd/agent/commands_test.go`（新） | 帮助硬门（每命令 -h/--help/help == Usage、/help 列全表、/help <cmd> == Usage）、别名、task 真库回路、memory 真库、text2img 真渲染、code-search 已知符号、log 边界、错误路径 |
| `cmd/agent/cli_chat.go` | R4：REPL slash switch 改为"会话命令本地处理 + 其余 `execSlashCommand`"；chatUsage 补共享命令表文档 |
| `cmd/agent/cli_web.go` | R4：`serveSlashCommand`（event:command 帧）+ SSE headers 提前 + `handleTranscript`；R5：`webChatRequest.Images`/`webChatImage`、`validateWebImages`、image turn 模型切换/恢复、`webServer.visionModel`；webUsage 重写（slash/images/transcript 文档）；浏览器页：📎 选择器/粘贴/拖拽/缩略图/条带、command 帧渲染节点、transcript 回放恢复 |
| `internal/agent/chat_ops.go` | `ChatOps` variadic images：种子 history + 带图轮 user 消息组装（镜像 runEngine） |
| `internal/agent/loop.go` | `Chat` variadic images（同上组装） |
| `internal/agent/chat_image_r5_test.go`（新） | R5 引擎 wire 断言：image block 先于 text、单图单消息、多图不合并、文本轮零图块（阴性）、ChatOps 上下文形状、caller 拥有模型切换 |
| `cmd/agent/cli_web_p1_test.go`（新） | R4 web 集成：slash 轮零模型请求 + 零 history + 单 command 帧、未知命令错误帧、命令输出不泄入下一轮上下文、transcript 回放；R5 web 集成：图片上 wire + 模型切换/恢复 + 校验 4xx 矩阵；chat/web 同表回路 |
| `internal/tools/memory.go` | 新 `MemoryListRecent(n)`（recent-rows 只读渲染） |
| 其余 7 文件 | gofmt 对齐 + .gitignore（测试二进制） |

---

## 3. 测试与验证证据

### 3.1 单元/集成（mock LLM）

R4/R5 新增 15 个测试全绿（`go test ./cmd/agent/ ./internal/agent/ -run 'TestSlash|TestWebChat_R4|TestWebChat_R5|TestChat_ImageTurn|TestChatOps_ImageTurn|TestChatOps_TextTurn|TestMemoryListRecent|TestIsSlashCommand|TestWebUsage' -count=1 → ok`）。
关键锁定：

- `TestSlashEveryCommandAnswersDashH`——8/28 帮助教训的测试化：每命令 -h/--help/help 输出逐字节等于其 Usage；
- `TestWebChat_R4_SlashCommandNeverReachesModel`——mock 模型服务器收到 **0** 个请求 + `ws.history` 空 + 恰 1 个 command 帧；
- `TestWebChat_R4_SlashOutputNotInNextTurnContext`——命令输出不泄入下一真实轮的模型请求；
- `TestChat_ImageTurn_WireShape`——image block 在 text 之前、media_type/base64 逐字上 wire；
- `TestChatOps_ImageTurn_ToolRoundKeepsImageInContext`——多图不合并 + 上下文形状；
- `TestWebChat_R5_ImageTurnAttachesAndSwitchesModel`——图片轮 wire model=vision、轮后恢复默认、transcript 带 `has_image` 不带像素；
- `TestWebChat_R5_ImageValidation4xx`——bad type / broken base64 / 超上限 / 超 8 张 全 400。

### 3.2 live E2E（GLM glm-5.3-flash，隔离 GOPI_HOME=/tmp/gopi-p1-e2e，自建二进制）

```bash
CGO_ENABLED=0 go build -buildvcs=false -o bin/gopi ./cmd/agent
GOPI_HOME=/tmp/gopi-p1-e2e ./bin/gopi web --addr=127.0.0.1:18100
```

**R4 chat 侧**（§3 P1 验收命令）：
```
$ printf '/task new "P1 验收任务"\n/task list\n/help\ntext not a command\n/quit\n' | ./bin/gopi chat
Task created: t-1787984177 (status=in_progress, model=)
1 task(s):
#1  t-1787984177  [in_progress]  P1 验收任务  (model (unknown), created 2026-08-29 14:16:17)
<chatUsage + 共享命令表>
...
```
非命令行 `text not a command` 正常进模型轮（回答流式输出）；命令行零模型轮（stderr 无 request 事件）。

**R4 web 侧**：
```
$ curl -s -X POST localhost:18100/api/chat -d '{"message":"/task new \"P1 web 验收任务\""}'
event: command
data: {"command":"/task new \"P1 web 验收任务\"","output":"Task created: t-1787984213 (status=in_progress, model=)"}

$ curl -s -X POST localhost:18100/api/chat -d '{"message":"/task list"}' | grep -c 'event: command'
1
$ curl -s localhost:18100/api/transcript
{"messages":[]}          ← 命令轮后服务端上下文确认为空（不进 history 实证）
$ curl -s -X POST localhost:18100/api/chat -d '{"message":"/bogus"}'
event: command
data: {"command":"/bogus","error":"unknown command \"bogus\" — available: code-search, log, memory, task, text2img"}
```

**R5 图片上传（text2img 造图 → 上传 → 模型逐字读出）**：
```
$ printf 'GOPI-VISION-P1-42' > /tmp/p1-src.txt && ./bin/gopi text2img /tmp/p1-src.txt /tmp/p1-test.png
✅ /tmp/p1-src.txt → 1 页 (17 字符, ... 字体 wqy-zenhei.ttc)
$ curl -s -X POST localhost:18100/api/chat -H 'Content-Type: application/json' \
  -d "{\"message\":\"图中写了什么字？只回答图中文字\",\"images\":[{\"media_type\":\"image/png\",\"data_b64\":\"$(base64 -w0 /tmp/p1-test.png)\"}]}"
data: {"result":"GOPI-VISION-P1-42"}     ← 逐字命中

raw log 实证（同一 web session 的 raw log）：
  image blocks: 1        （grep -c '"type": *"image"'）
  model: glm-5.3-flash   （图片轮确实切到 vision 模型）
$ curl -s localhost:18100/api/transcript | head -c 300
{"messages":[{"role":"user","content":"图中写了什么字？只回答图中文字","has_image":true},{"role":"assistant","content":"GOPI-VISION-P1-42"}]}
```

超限 4xx 实证：3MiB 解码图（base64 4MiB+）`curl` → `400`（body 上限拦截）。

P0 先例回归：SIGTERM 停机后 session_end 恰 1 个、R1 两轮连续、R2 工具帧——
P0 的 8 个对齐测试原样全绿（见 §3.1 全量运行）。

---

## 4. 遗留问题

1. **`internal/deepseek/TestNewVisionClient_ReusesAPIKey` 宿主相关失败——本单已修复**：P0 遗留声明的问题（宿主 `/home/user/.gopi/settings.json` 的 `vision_base_url: open.bigmodel.cn` 压过默认值）。修复 = 测试内 `GOPI_SETTINGS` 指向空临时文件（stub 宿主配置，env > settings > default 解析链确定性化），`go test ./... -count=1` 全包全绿（含该包）。
2. **图片历史逐轮重发**：图片挂在 turn 的 user 消息里，随 `s.history` 进后续轮 seed——多轮后图片块重复上 wire（token 成本）。DSH 同样把图放 user 内容；设计文档 P3 的附件持久化（落盘 ref + 按需重读）是正确解法，本单未做。
3. **`/log search` 的 grep 语义粗粒度**：FormatGrep 面向"工具调用 grep"，纯文本查询可能空手而归；交互用户可退回 `/log read`。P2 若有真实使用反馈再调。
4. **`/task update` 只暴露 status/summary**：CLI 的 assignee 等扩展列（R6 P2 范围）随 R6 落地后再进表。
5. **web 前端无命令自动补全**：输入 `/` 后静态提示无（用户靠 /help）；DSH 有下拉补全，属前端打磨项。
6. **浏览器粘贴大图**：`readAsDataURL` 对超大截图（>32MiB）会在客户端被 MAX_B64 拦下并提示，但 iOS Safari 的 HEIC 不在 accept 列表——已按 Anthropic media-type 白名单处理（不支持即不接受）。
