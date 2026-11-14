<!-- doc-class: normative -->
# Chat 载体分类：chat / web / ui 与 `web search`/`web fetch` 的归属

**裁决日期**：2026-10-24（操作员）
**承载行**：`01a16d4c`（根）← `01a16d50` / `01a16da6` ← `01a16da8`（终裁）
**本文档地位**：该裁决链的**唯一落地点**。四条行不再各自持有结论，结论在这里。

---

## 1. 裁决结论（先读这一节）

**唯一的目标形态，就这两种：**

```
gopi chat              # 默认，CLI 档，纯终端逐行追加。兜底路径。
gopi chat --web        # 浏览器访问（即今天的 gopi web）
```

**砍掉的三样东西：**

| 砍掉的 | 理由 |
|---|---|
| `--textweb` 档（w3m/lynx 渲染） | 见 §4 |
| `GET /cli` + `POST /cli/chat` 路由 | 见 §4 |
| `web search` / `web fetch` 留在 "Chat & web" 分组里 | 它们是**工具**，不是 chat 载体，见 §3 |

**保留但不对门面发布：**

| 动词 | 档位 | 状态 |
|---|---|---|
| `gopi ui` | 画布模型（TUI） | 隐藏动词，`isUISubcommand` 仍派发（操作员裁决 2026-10-06，task `01a10dba`） |
| `gopi cli` | 隐藏独立动词 | 同上，`isTermSubcommand` 仍派发 |

---

## 2. 判据：为什么兜底必须是 CLI 档

这条判据来自 hermes 的 CLI/TUI 区分（操作员提供原文）：

> **CLI**：屏幕是"历史"，只能往下追加。想要面板/弹窗基本没法做。
> **TUI**：屏幕是"画布"，可以局部重绘、叠浮层、分栏。所以才有那些 picker 和面板。

| | CLI（历史模型） | TUI（画布模型） |
|---|---|---|
| 屏幕 | 只能向下追加 | 局部重绘、叠浮层、分栏 |
| 依赖 | 只要 stdout 能动 | ANSI 光标控制 + 终端宽高识别 |
| **兜底资格** | **可真正兜底** | **不能 —— 要求最高** |

⇒ **兜底方案必须是 CLI 档，不能是 TUI 档。**

### 2.1 gopi 现有载体的实际分档（代码取证）

| 载体 | 档位 | 证据 |
|---|---|---|
| `gopi chat` | **CLI 档** | `cmd/agent/cli_chat.go:2464` 输入走 `newChatInputWithPalette(os.Stdin, ...)`；`cmd/agent/chat_stream_split.go:89` `chatLayoutStream()` 是一个普通 `io.Writer`，逐行 `Fprintf`/`Fprintln` 追加（`cli_chat.go:2563,2565,2567`）。**无光标控制、无局部重绘** ⇒ 天然兜底 |
| `gopi ui` | **TUI 档** | `cmd/agent/cli_ui.go:275` "the TUI owns the cursor of the ..."；`:292` "the cursor-up navigation lands in the wrong place" —— 画布模型，要求光标控制与几何识别 |

**结论**：`gopi chat` 本身就是兜底答案，不需要另造一个。操作员 2026-10-24 的确认（行 `01a16d50`）："gopi chat 实为 CLI 档即兜底答案"。

---

## 3. `web search` / `web fetch` 的归属

**它们是工具，不是 chat 载体。** 操作员原话：

> "因为 web search/web fetch 在我看来更像是 tools 下面的工具，和 web 毫无关系，因为 web 特指的是 chat with web。不是吗？"

### 3.1 现在是错的（实测）

`internal/config/help.go:657` 把不相关的东西塞在一组：

```go
{"Chat & web:", [][2]string{
    {"gopi chat [--tools]", "Interactive multi-turn REPL."},
    {"gopi web [--tools]",  "Local browser chat UI (SSE)."},
    {"gopi ui_state ...",   "Read a live chat screen as a cell grid."},
    {"gopi web search \"<query>\"", "The web_search tool from the shell."},
    {"gopi web fetch <url>", "The web_fetch tool: one URL's cleaned text."},
}},
```

### 3.2 为什么这是命名碰撞而不是分类

`cmd/agent/main.go:675` 的派发链里，`isWebSearchSubcommand` / `isWebFetchSubcommand` 与 `isWebSubcommand` 是**三个并列独立的分支**：

```go
... || isWebSearchSubcommand(dispatchArgs) || isWebFetchSubcommand(dispatchArgs) ...
... || isWebSubcommand(dispatchArgs) ...
```

它们只是**共享词根 `web`**，功能上毫无关系。一个是工具面，一个是聊天载体。

### 3.3 目标

`web search` / `web fetch` **移入 tools 分组**，与其它 `gopi tool` 面并列。

> **未裁决**：是否进一步改名为 `gopi tool web_search` / `gopi tool web_fetch`。改名要同步 `cmd/agent/cli_completion.go` 的 `topLevelCommandNames`，属独立决定，不在本次裁决范围内。

---

## 4. 为什么砍掉 `--textweb` 与 `/cli` 路由（终裁，不再讨论）

操作员原话（行 `01a16da8`）：

> "w3m http://ip:port/cli 我不希望我们引入任何依赖，如果为了实现无浏览器的访问而要求 w3m 我觉得意义不大。你要强迫用户部署这个吗？还是说用户直接使用 w3m 访问和 js web 一样的端口？那么看起来这个实现太困难了，干脆不要了。"

### 三条判死理由

**1. w3m 本身就是新依赖。** 为"无浏览器访问"而要求用户部署 w3m/lynx，就是把一个外部二进制变成 gopi 的前置条件 —— **违反 Rule 35 零依赖**。

**2. 同端口双渲染是不可接受的复杂度。** 若让 w3m 访问与 JS 页面相同的 URL/端口，服务端必须按 User-Agent 分叉渲染：一套内容两种形态、两条维护路径、两套测试。**把最容易腐化的分支塞进最不该复杂的地方。**

**3. 它填的是一个不存在的洞。** `gopi chat` 本身就是纯终端逐行追加，w3m 用户手里的终端可以直接跑它。**兜底路径不需要穿越 HTTP。**

### 4.1 `/cli` 路由的既有取证（为什么它本来就站不住）

| 问题 | 事实 |
|---|---|
| 零宽度适配 | `cmd/agent/term.go` 全文只读 POST 的 Content-Type，**无 User-Agent / COLUMNS** |
| 排版 | `<pre>` 硬编码，长行在终端里折得参差 |
| 结构 | 无 HTML 结构（`markdownNodes` 未调用），标题/列表全拍平 |
| 工具行 | `term.go:361-363` 的 `OnToolEvents` 只在 `--tools` 下注册，形态仅一行文字，无卡片层次 |
| **致命** | **它仍需先起 `gopi web` 再装一个 HTTP 客户端 —— 用同一个昂贵前提去替代那个昂贵前提** |

### 4.2 若执行砍除

- 归档 `GET /cli` 与 `POST /cli/chat`
- 在 devdoc 写清"文本浏览器 fallback"这条思路为何在本机约束下不成立
- 对应场景指回 `gopi chat`
- **不得留下未说明的死代码**

---

## 5. 三处容易混淆的东西（混乱的根源）

裁决前必须分清，否则会改错地方：

| # | 是什么 | 位置 | 处置 |
|---|---|---|---|
| 1 | `gopi cli` —— **隐藏的独立动词** | `isTermSubcommand`（`cmd/agent/main.go`） | 保留（仍派发，不发布） |
| 2 | `GET /cli` + `POST /cli/chat` —— **路由** | `cmd/agent/term.go` | **砍掉** |
| 3 | `gopi ui` —— 隐藏的独立动词（TUI） | `isUISubcommand` | 保留（仍派发，不发布） |

**三者名字里都有 "cli" 或 "ui"，但只有 #2 是被砍的那个。**

---

## 6. 待落地的代码改动（本文档只是裁决，不是实施）

| 改动 | 位置 | 验收 |
|---|---|---|
| `web search` / `web fetch` 移出 "Chat & web" 分组 | `internal/config/help.go:657` | `TestTopLevelHelpCoversEveryDispatchedSubcommand` + `TestTopLevelHelpHidesUnadvertisedVerbs` 两个守卫仍绿 |
| 砍 `GET /cli` / `POST /cli/chat` | `cmd/agent/term.go` | 不得留死代码；`gopi -h` 前后对比 |
| `gopi chat --web` 作为 `gopi web` 的别名（可选） | `cmd/agent/main.go` 派发链 | 需同步 `cli_completion.go` 的 `topLevelCommandNames` |

**约束**：Rule 35 零新依赖；不新增常驻进程/脚本。

---

## 7. 这份文档为什么存在

裁决链 `01a16d4c` → `01a16d50` / `01a16da6` → `01a16da8` 是**四行各自记录了裁决的一部分**，而根行 `01a16d4c` 从未收口。按 Rule 33，父行不终态 ⇒ 子行永久 parked —— 整条链卡死，且没人能一眼看到结论全貌。

本文档把结论收到一处：**阅一份就够，不必爬四行账本。**
