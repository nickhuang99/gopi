<!-- doc-class: historical -->
# `gopi chat`: 一行输入是命令还是文本 —— 输入分类器（epic 01a104cb）

## 问题（修前的实测）

四个载体（chat REPL、`chat --once`、`gopi web`、`gopi term`）都用同一个判据：

```go
strings.HasPrefix(strings.TrimSpace(line), "/")
```

于是**任何以 `/` 开头的东西都是命令**。粘一条路径就被当成命令拒绝，而且没有第二条通道：

```
$ printf '/tmp/CN-result.gif\n/quit\n' | gopi chat --once
[gopi] unknown command "/tmp/cn-result.gif" — available: /clear, /code-search, …
[gopi] a scripted line was not honoured: unknown command "/tmp/cn-result.gif" …
EXIT=1
```

（修前红证，二进制 `~/.gopi/sessions/01a104cb-…/artifacts/gopi-base-redproof`，见报告 `## Evidence`。）

**红证的两种取法（都实测过）**：
- **行为红证**：base 二进制直接跑真实输入，看它拒绝并 `requests:0`（下节四条载体实跑都贴了 base 对照）；
- **测试红证**：`GOPI_BIN=<base 二进制> go test ./testsuite/ -run TestChatOnceText -count=1` —— 四条能力用例 FAIL、两条不回归用例 PASS（`PASS`/`FAIL` 列表见报告）。

## 判据（本 run 定的，连同理由）

`classifyChatLine(line, accepts)`（`cmd/agent/commands.go`）返回四种之一：

| 形状 | 判定 | 理由 |
|---|---|---|
| 首个 token 是 `//` + 单个词（`//quit`） | **Escape → 文本**，去掉一个 `/` 后发给模型 | 命令形状的行没有别的办法进模型；这是唯一的「按字面发出」通道，也是分类器**唯一**会改写字符串的地方 |
| 首个 token 是 `/` + **不含第二个斜杠**的一段词，且该词被本载体接受 | **命令** | 真命令不受影响 |
| 同上，但该词不被本载体接受，且词形在本命令表的字母表里（字母/数字/`-`/`_`） | **Unknown → 拒绝** | 拼错的命令必须继续报错；否则打字错误会静默变成提问 |
| 其余一切（首个 token 含第二个斜杠的路径、孤立 `/`、`//`、`/ ` 后跟空格、`/^gopi/` 正则、普通句子） | **文本**，逐字不改写 | 人类粘贴最多的是路径，路径**凭自己的形状**就是文本；用户不需要先学会转义 |

**为什么形状判据用的是「第二个斜杠」而不是「查命令表」**：粘贴路径的人不应该为了发现转义通道而先被拒绝一次；而单个斜杠正是人类表示「这是命令」的打字方式。歧义由 token **是什么**来消解，而不是由模型更想要什么来消解。

**已知的诚实边界**：`/etc` 是单段路径、也是命令表里没有的词 → 被判为 **Unknown（拒绝）**。它仍然走不出去，除非写 `///etc`（转义）——这条边界写在代码与本文档里，不是意外。`chat --once` 的唯一 prompt 契约因此需要一个 `'\n'/etc` 前缀（首行是换行时，`--once` 保持「一个 prompt」的旧行为，`/etc` 成为普通文本）。

## 四个载体

| 载体 | 位置 | 变化 |
|---|---|---|
| chat REPL | `cli_chat.go`（`runChatWithInput`） | 删掉 `HasPrefix(msg,"/")`，改问 `classifyChatLine(msg, chatReplWordKnown)`；Unknown 就地拒绝（同一个 `chatUnknownCommandError`，仍走 `chatOnceReportFailure` 上报类） |
| `chat --once` | `chat_once_seq.go` | 首行分类走同一分类器（`chatOnceSeqIsScriptedLine`）；命令**或转义**都让这次调用进入脚本行模式，命令词常量 `chatOnceSeqSlash` 删除、改用共享的 `chatSlashTrigger` |
| `gopi web` | `cli_web.go`（`handleChat`） | 同上，词表为 `sharedSlashWordKnown` |
| `gopi term` | `term.go`（`handleTermChat`） | 同上 |

**词表是按载体传进去的，不是写死的**：`/quit` 结束 chat 会话，而 web/term 是**服务端**会话、没有可结束的东西 —— 所以同一行在一个载体是命令、在另一个载体是「本载体不接受的命令」。这是 `accepts func(word) bool` 参数存在的唯一理由，也是唯一允许的差异。

## 拒绝文案（唯一一处，四载体共享）

`slashUnknownCommandError`（`commands.go`）现在多一句，且转义写法由同一个常量 `chatSlashEscape` 渲染：

```
unknown command "/nonexistent" — available: /clear, …, /tools; try /help; to send it to the model as text, write //nonexistent
```

## 不变量

- `/help` 的词表本身未改（`slashHelpText` 只多出「Type any other line to chat with the model.」这一句原有末行）。
- `chat --once` 的「首行是命令」契约未改：首行是命令**或转义**才进入脚本行模式，其余输入仍是「一个 prompt、多行折叠成一条消息」（`TestChatOnceText_OnePromptContractIsUnchanged` 钉住）。
- 命令字仍是具名常量（`chatSlashTrigger` / `chatSlashEscape` / `helpCommandName` …），`chatOnceSeqSlash` 这个重复定义被删除。
- 不新增顶层工具、不新增依赖或脚本（Rule 3/35）。

## 验证配方（可复用）

```bash
# 红证：base 二进制 + 本 run 的测试文件 → 必须有 FAIL
git checkout -- <被改的文件>; go build -o /tmp/gopi-red ./cmd/agent/
GOPI_BIN=/tmp/gopi-red go test ./testsuite/ -run TestChatOnceText -count=1

# 绿：本分支
CGO_ENABLED=0 go build ./... && go build -o bin/gopi ./cmd/agent/
GOPI_BIN=$PWD/bin/gopi go test ./testsuite/ -run TestChatOnceText -count=1 -v
```

`testutil.RunGopi` + `NewMockLLM`：`RequestCount()==0` 断言「被就地分发、没发给模型」，`RequestCount()>0` + 读**请求体的 user 消息**（不是整包 substring —— system prompt 里就写着 `/tools`，整包搜索会假阳性）断言「真的到了模型」。

## 四个载体的真实载体实跑（Rule 31，不是单元测试）

`chat --once` 是唯一 stdout 载体（`--once` + stdin 脚本，stdout 即消息/成功）；web 与 term 都在同一个 `gopi web` 进程里，只差端点：`POST /api/chat`（JSON `{message}`）与 `POST /cli/chat`（form `message=`，Content-Type 必须 `application/x-www-form-urlencoded`）。**`gopi term` 这个动词不存在** —— 终端载体是 `gopi cli`，而它默认去开 `scripts/links` 文本浏览器；要打这个端点就直接起 `gopi web`（同一个 mux，`runWeb` 同时挂 `/api/chat`、`/cli/chat`）。

判「真的到了模型」用的是**独立 mock provider**：`cmd/agent` 外的 stdlib-only 小服务，`POST` 任意路径即回一段 Anthropic SSE，并暴露 `GET /__stats` → `{"requests":N,"user_text":[…请求体里 role==user 的文本…]}`。它不进仓库（Rule 35），二进制与日志留在 session 目录。

| 载体 | 输入 | 修前（base） | 修后 |
|---|---|---|---|
| `chat --once` | `/tmp/CN-result.gif` | `unknown command …`，exit=1，requests=0 | stdout `MOCK-ANSWERED`，exit=0，requests=1，wire=`/tmp/CN-result.gif` |
| chat REPL | `/tmp/CN-result.gif` | 同左 | requests=1，wire=`/tmp/CN-result.gif` |
| `gopi web` `/api/chat` | `/tmp/CN-result.gif` | `event: command` + error，requests=0 | `event: delta` → done，requests=1，wire=`/tmp/CN-result.gif` |
| `gopi cli` `/cli/chat` | `/tmp/CN-result.gif` | `[turn failed] unknown command …`，requests=0 | 页面 `<pre>` 回 `MOCK-ANSWERED`，requests=1，wire=`/tmp/CN-result.gif` |
| 四载体 | `//gopi-literal` | `unknown command "/gopi-literal"`，requests=0 | requests=1，wire=`/gopi-literal`（**去掉一个斜杠**，逐字其余不变） |
| 四载体 | `/nonexistent` | 拒绝（无转义提示） | 仍拒绝，`…try /help; to send it to the model as text, write //nonexistent` |
| 四载体 | `/quit` `/task list` `/tools` `/help` | 就地分发，requests=0 | **不变**：就地分发，requests=0 |

**`/tools` 在 web/term 是「本载体不接受的命令」**（服务端会话没有可以开关的工具开关），这不是回归：base 二进制对 `/tools` 在 term 上给出的也是 `unknown command "/tools"`（贴在同一份证据里），修前修后同一句话，只是多了一句转义提示。四载体「一致的判定」指的是**分类器给同一行的 verdict 一致**（命令/文本/转义/Unknown），词表按载体不同是设计的一部分。

## 已知边界（诚实）

- `/etc`：单段路径且不在任何词表里 → **Unknown（拒绝）**；要按字面发出写 `///etc`（`//` + `/etc`）或 `//etc`。这是判据的代价，写在代码注释与本文档里。
- `//` 单独一行、`//tmp/x`（转义后仍含斜杠）→ **文本，逐字不改写**：转义通道只吃「`//` + 不含斜杠的一个词」，所以它误伤面为零，代价是这类行拿不到「去掉一个斜杠」的行为。
- 词表按载体不同（`/quit` 只有 REPL 有），所以「同一行在不同载体 verdict 不同」是**设计**：`accepts` 参数的存在理由。
