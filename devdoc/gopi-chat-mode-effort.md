<!-- doc-class: normative -->
# 调查报告：为 GoPi 增加 Chat 模式（CLI / TUI）的工作量评估

> Epic: 调查当前 GoPi 项目，评估为 gopi 增加 chat 模式（通过 CLI 或 TUI）的工作量。
> 参照物: `~/work/hermes-agent`（Python、prompt_toolkit TUI，16k 行 cli.py）与
> `/home/user/work/deepseek-harness`（Web 界面 dsh）。

## 结论速览（TL;DR）

**GoPi 已经从底层为 chat 模式准备好了大部分地基，缺的是一个"顶层交互外壳"。**

- **CLI chat（REPL，无 TUI）工作量：小** — 约 **2–4 天**（一个 Go agent）。
  - 多轮上下文续接（Resume + raw-log checkpoint）已存在，可直接复用。
  - SSE 流式已在 deepseek 客户端内实现，需补一层到 stdout 的暴露。
- **TUI chat（类似 hermes 的 prompt_toolkit / 交互式终端）工作量：中** — 约 **5–8 天**。
  - 需要一个终端渲染层（换行、光标、多行输入、快捷键）——GoPi 当前**没有任何终端 UI 依赖**，需要引入或自研。
  - 流式增量渲染到 TTY + Ctrl+C 中断 + 多行粘贴，需要自己处理。
- **完全复刻 hermes 级 TUI（附件/图片粘贴、工作树管理、技能面板、模型切换 UI）**：约 **3–4 周**。
  - 那属于"完整产品"，超出本次"chat 模式"的核心范畴。

关键判断：**GoPi 的 agent 循环（`runEngine`）当前是"单次运行、最终返回一个字符串"**，
且流式输出被循环内部消费、不暴露给调用方。这是 chat 模式唯一需要动核心代码的地方；
其余（会话存储、续接、配置、工具集、日志）全部现成。

---

## 1. 当前架构：为什么 GoPi 是"单发式"而非"会话式"

### 1.1 入口：`cmd/agent/main.go`（272 行）

`main()` 的流程是：

1. 扫描子命令（memory/web_search/vision/text2img/code-search/log/resume/checkpoint/proxy/tasks）→ 提前 return。
2. 解析配置（flag > env > settings.json，注入进程环境）。
3. `readInput(overrides)` 解析输入：
   - 位置参数 = 字面 prompt / 文本文件 / 图片文件；
   - 无位置参数时读 stdin —— **但仅限管道输入**；`isTerminal(os.Stdin)` 为真（即交互 TTY）时打印 usage 并退出（main.go:196-199）。
4. `ag.Run(input.prompt, images...)` —— **一次阻塞调用**，返回最终 `result` 字符串。
5. `fmt.Print(result)` 打印结果并退出。

**这是最核心的单发特性**：GoPi 把"一次 prompt → 一个 result"当成全部。
交互式 TTY 下不给提示符就直接退出的设计，明确假定用户用管道/参数喂任务。

### 1.2 循环：`internal/agent/loop.go` —— `runEngine`（1456 行）

```go
func (a *Agent) Run(userInput string, ...) (result string, err error) {
    return a.runEngine(userInput, false, nil, images)
}
func (a *Agent) Resume(userInput string, history []deepseek.Message) (result string, err error) {
    return a.runEngine(userInput, true, history, nil)
}
```

`runEngine`（单 goroutine ReAct 循环）是 chat 模式的天然复用点：

- **首轮**（resume=false）：把用户输入包成 `[EPIC GOAL]` 框架 + task list 提示 + 注入 task-start memory_search。
- **续轮**（resume=true）：把续接指令作为普通 user message 追加（不再重新框架化/重注入）。
- 每次迭代调 `chatOnce` → `chatOnceAttempt`（SSE 流式），收集工具调用 → `executeToolBatch`，直到模型 end_turn。

**关键缺口**：`runEngine` 把所有模型增量（thinking/text/tool_use）消费在内部，
通过一个本地缓冲区累积成最终 `chatResult`，然后只返回最终结果字符串。
**没有任何"流式回调" / "事件发射器"接口暴露给调用方**（Agent 结构里只有 `Logger *log.Logger`）。

→ **chat 模式要做的第一件事，就是给 `runEngine`/`chatOnce` 加一个可选的流式回调**
（`func(delta string)` 或事件通道），把 already-there 的 SSE 内容透传到 stdout/TUI。
这一条是"小工作量"判断的根因：流式数据本来就在客户端产生了，只是没有往上透传。

### 1.3 客户端：`internal/deepseek/claude_client.go` + `client.go`

流式能力**完全具备**：

- `scanSSEEvents`：SSE 解析器（事件/delta 前缀，缓冲合并微块）。
- `ClaudeStreamEvent`、`ClaudeStreamDelta`：`text_delta | thinking_delta | input_json_delta | signature_delta`。
- HTTP 传输已为低延迟流式调优（连接池、TCP_NODELAY、宽松响应头超时 120s）。
- `RawLog` 回调已经能把每个 SSE chunk 记为 `← CHUNK` —— 这是现成的流式透传雏形。

**也就是说，模型侧流式完全可用的，缺的只是"把流式结果暴露给 UI"这一层胶水。**

### 1.4 会话 / 续接基础设施：`internal/agent/resume.go` + `session.go` + `trajectory.go`

这里其实已经有 chat 模式的多轮基础：

- **`LoadCheckpoint(rawLogPath)`**：从 raw log 的每条 `→ REQUEST`（字节精确的完整上下文检查点：system+GOPI.md+messages+tools+thinking）重建完整会话历史（`[]deepseek.Message`）。这正是多轮 chat 的"从上次继续"。
- **`Resume(userInput, history)`**：seeds 历史 + 追加续接指令，`runEngine` 继续同一上下文 —— 这就是 chat 每一轮的机制。
- **`SessionLogger`**（session.go）：JSONL 会话日志，`gopi log --chat` 已提供人类可读对话视图（`FormatChat`）。
- **trajectory / tasklist / heartbeat / clock**：会话状态富集，chat UI 展示进度可直接用。

> 换句话说：**GoPi 已经能"续接会话"，只是把它暴露成 `gopi resume <log>` 这一个 CLI 子命令，而不是连续的交互式提示符。**

---

## 2. 三档方案的细分工作量

### 方案 A：CLI chat（REPL，无 TUI）— 小 ≈ 2–4 天

新增 `gopi chat`（或 `gopi` 无参数但检测 TTY 时进交互）子命令。

**实现清单：**
1. **流式回调**（核心，~1 天）：给 `runEngine`/`chatOnceAttempt` 加可选 `onDelta func(string)`；
   模型 text delta 实时打到 stdout。这是唯一动核心循环的点，其余全部新增。
2. **REPL 外壳**（~0.5–1 天）：`bufio.Scanner`/`bufio.Reader` 读 stdin，每次回车发一轮；
   每轮建新 Agent 或复用同一 Agent（用 Resume 语义 `AddUserMessage`）。
3. **多轮上下文**（~0.5 天，复用现成）：首轮回 `runEngine(epic)`，后续回复用 `Resume` 语义；
   从 raw log checkpoint 自动续接，或 session 内存续接。
4. **多行输入 / 中断**（~0.5–1 天）：`\` 续行 or 临时 EOF、Ctrl+C 中断当前轮（当前没有中断机制，需加 ctx cancel + 信号处理）。
5. **命令前缀**（~0.5 天）：`/quit /resume /model /settings /help /clear` 等斜杠命令。
6. **测试**（~0.5 天）：`-run TestChat` 输入→多轮→中断的回归。

**工作量集中在 1（流式透传）和 4（中断语义）**。命令/REPL/续接都是现成件拼装。

### 方案 B：TUI chat（交互式终端 UI）— 中 ≈ 5–8 天

整体继承方案 A 的 1/2/3/5/6，额外加：

1. **终端渲染层**（~1–2 天）：**GoPi 当前零 TUI 依赖**（go.mod 只有 modernc.org/sqlite）。
   选择：
   - 引第三方（`charmbracelet/bubbletea` / `gdamore/tcell` / `rivo/tview`）→ 需联网下载依赖（本机离线，可能受限）；
   - **自研**（对应 Rule 10/ENGINEERING：零依赖优先）→ 一套 ANSI/光标/分页渲染，工作量 +1–2 天。
2. **流式增量渲染**（~1 天）：delta 实时刷新当前行 + 光标管理 + 上下历史滚动。
3. **多行输入区 + 快捷键**（~1 天）：Enter 发送 / Shift+Enter 换行 / Ctrl+C 中断 / 上键历史。
4. **输出分页/滚动回看**（~0.5 天）：超过终端高度时可滚动查看工具结果。
5. **状态栏**（~0.5 天）：model / token / 迭代计数 / 工具运行中指示（有 trajectory/heartbeat 可喂）。
6. **ansiterm / isatty 检测**（~0.5 天，已有 isTerminal 的一部分）。

**工作量集中在：新增终端渲染依赖（或自研）+ 流式增量渲染。**

### 方案 C：hermes 级完整 TUI 产品 — 大 ≈ 3–4 周

在 B 之上再加：工作树管理 UI、技能/插件面板、模型切换器（含 API key/OAuth 流程）、
图片/附件粘贴自动附加（vision 模式已有 image 内容块，可接）、外部 gateway/平台路由、
Kanban 目标循环、light/dark 主题自动检测（OSC11）等。
这超出"chat 模式"核心，属于完整 IDE 级产品。

---

## 3. 风险与前置条件

1. **离线依赖**：本机 go.mod 只依赖 `modernc.org/sqlite`（离线 pin 版本）。
   引 TUI 库需先确认 module cache 有无；**零依赖自研 TUI 更贴合本机约束**（ENGINEERING 规则 7）。
2. **流式透传必须暴露，否则 chat 退化成"一次等 40 秒无反馈"** —— 体验差，必须做方案 A 的第 1 步。
3. **中断语义不可缺**：当前 `runEngine` 无用户中断；chat 必须把 ctx cancel + SIGINT 接进 `chatOnceAttempt`。
4. **任务框架化 vs 自由对话**：GoPi 的 `runEngine` 默认把输入包装成 `[EPIC GOAL]` + task list。
   Chat 模式的"普通问答"可能需要一个**跳过 epic 框架**的开关（`runEngine` 已有 resume 分支可参考，需加一个"非任务框架化"模式）。

---

## 4. 建议落地顺序（若批准实施）

1. **P0（半周）**：`runEngine` 加 `onDelta` 流式回调 + `gopi chat` REPL（方案 A 的 1/2/3）。
   验证：`echo "hi" | gopi chat` 实时出字。
2. **P1（几日）**：多行输入、Ctrl+C 中断、斜杠命令、会话续接。
3. **P2（可选，一周左右）**：零依赖 ANSI TUI 渲染层（方案 B）。

**任何 TUI 工程遵循 Rule 10 习惯：像素级验证（IoU）于 ANSI 布局 / `go test -run TestX` 回归于流式透传。**

---

## 5. 佐证代码位置（供实施者速查）

| 关注点 | 文件:行 | 说明 |
|---|---|---|
| 单发入口 / TTY 拒绝 | `cmd/agent/main.go:100,196` | stdin only 管道，TTY→usage |
| 核心循环 | `internal/agent/loop.go:1527-1561` (`Run`/`Resume`/`runEngine`) | 复用续接点 |
| SSE 流式 | `internal/deepseek/client.go` (`scanSSEEvents`) + `claude_client.go` (`ClaudeStreamEvent`) | 流式已具备 |
| 流式消费 | `internal/agent/loop.go:2206-2318` (`chatOnce`/`chatOnceAttempt`) | **缺透传回调** |
| 会话续接 | `internal/agent/resume.go:81` (`LoadCheckpoint`) | 现成 |
| 会话日志 | `internal/agent/session.go` (`SessionLogger`, `gopi log --chat`) | 现成 |
| 终端检测 | `cmd/agent/main.go` `isTerminal` | 已有基础 |
| Agent 结构 | `internal/agent/loop.go:1012` (`Agent` struct) | 无流式回调字段 |

---

*调查完成于 feat/chat-mode-investigation 分支；本报告为纯调研，无代码改动。*
