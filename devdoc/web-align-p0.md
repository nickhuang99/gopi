<!-- doc-class: normative -->
# devdoc — gopi web 模式对齐 P0（服务端上下文内化 + web 工具路径）

- 分支：`feat/web-align-p0`（worktree `~/workspace/gopi-web-align-p0`，基于 master 5c1f7b9）
- 主 commit：`9fe3d48`（5 files, +878/−87）
- 日期：2026-08-29
- 输入：`~/workspace/gopi-chat-web-align/design-chat-web-alignment.md`（R1/R2）、`verify-report.md`、`bugs-found/003|006|007.md`

---

## 1. 方案 vs 设计文档（R1/R2）：差异与理由

设计文档的 R1/R2 方案经代码验证后基本成立，实施时做了三处调整：

### 1.1 R1 服务端上下文内化 —— 实施与设计一致，落地比设计更简单

设计文档判断：`Chat`/`ChatOps` 每轮用 seed history **替换** `Ctx.Messages`（chat_ops.go:68、loop.go:2243），所以 web 只需进程内持有 seed（镜像 cli_chat.go 的 `chatSession`），每轮后从 `ag.Ctx.Messages` 捕获。

实施验证后确认无误，且发现一个设计文档没有点明的简化：**"持有权威 history" 与 "8dc5c67 已落地的单会话整合" 共享同一个锁域**——`webServer` 已经为单 agent 持 `s.mu`（轮级串行），`s.history` 字段挂进同一结构体、同一锁域即可，无需新的会话映射或生命周期管理。

改动（全部在 cmd/agent/cli_web.go）：
- `webServer.history []deepseek.Message`（mu 保护，与轮同域）；
- `handleChat`：seed 取 `s.history`（不再读 `req.History`）；每轮后（成功或失败都）`s.history = append([]deepseek.Message{}, ag.Ctx.Messages...)`——镜像 cli_chat.go:329；
- 请求体 `history` 字段保留（back-compat：旧客户端解码不炸）但**接受即忽略**；
- 删除旧的 200 条客户端 history 白名单过滤逻辑（`maxWebHistoryMessages` 仅留注释性常量）；
- 浏览器 JS：发送体只剩 `{message}`（源码注释 `R1: the server owns the transcript — no history shipped`），本地不再累积 `history[]`。

### 1.2 R2 web 工具路径 —— 与设计一致；两处实现层收敛

设计：`--tools`/`GOPI_WEB_TOOLS` 开关 + `handleChat` 切到 `ag.ChatOps(msg, seed)` + `Agent.OnToolEvents func(ToolEvent)` 回调（镜像 OnText）+ SSE `event: tool` 帧 + 浏览器折叠卡片。

实施差异：

1. **模型构造零改动（收敛 1）**：设计文档预期可能要动 `newAgent`/`BuildClaudeRequest`；实际发现 `ChatOps` 内部自建请求时**总是**携带 `claudeToolDefs` + 完整路由 system prompt（chat_ops.go 的 req 构造）——工具面由**调用点选择**（Chat vs ChatOps）决定，与 agent 构造无关。`newAgent` 一行未动。默认（无 --tools）路径照旧走 `Agent.Chat`（`BuildClaudeRequest(..., nil, ...)`，wire 上 0 个 tools 字段）。

2. **OnToolEvents 触发点收敛（收敛 2）**：设计建议 "在 executeToolBatch 前后触发"。实施把触发逻辑放在 **chat_ops.go 的工具轮内**（`emitToolEvents` / `emitToolEventResults` 两个小助手），而 `internal/agent/loop.go` 只加了 `OnToolEvents` 字段 + 注释（~20 行）。理由：硬约束"不动 loop.go 核心循环"；且 `executeToolBatch` 同时被 task 循环调用，在那里触发会让 task 运行也发回调（语义面无端扩大）。ToolEvent = `{kind:"call"|"result", name, summary}`；summary 是一行可读摘要（write/bash 的 path/command 等首个关键参数；result 侧取输出首行，160 字符截断）。触发顺序 = call(全批) → execute → result(全批)。

3. **安全性声明（硬约束落实）**：`web --tools` = 本机 shell 等级权限，**loopback only**。工具经 `executeToolBatch` 直接执行，无审批（与 `gopi chat --tools` 完全同级；bash 危险命令有 `GOPI_ALLOW_DANGEROUS` 拦截，但那是命令级黑名单不是审批）。web 审批流（DSH 式 approval 帧）是 P3 范围。`--tools` 启动横幅 + help + 代码注释三处明示。

### 1.3 bug003 —— 实施超出设计文档预期

设计文档只在 P3 提过审批/serve，bug003 修复本身是本单要求：
- `runWeb` 把 `http.Serve` 移入 goroutine，主 goroutine `signal.Notify(SIGINT, SIGTERM)`；
- 信号路径：置 `shuttingDown=true`（handleChat 对新轮返 503，防止与 finalizer 死锁）→ `http.Server.Shutdown`（5s 宽限 `webShutdownGrace`，超时 force-Close）→ finalizer（`finalizeChatSession`）写唯一的 session_end + 关台账；
- 正常 halt 路径不变（Serve 返回后 finalizer）；
- **已知局限（写进代码注释）**：SIGKILL 下 OS 直接回收进程、不执行任何 Go 代码，session_end 无解——best-effort-impossible。

---

## 2. 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/agent/loop.go` | Agent 加 `OnToolEvents func(ToolEvent)` 字段 + 注释（核心循环零改动） |
| `internal/agent/chat_ops.go` | `ToolEventKind`/`ToolEvent` 类型；`emitToolEvents`/`emitToolEventResults`/`toolEventSummary`/`firstLineOf`；工具轮执行前后各发一次 |
| `cmd/agent/cli_web.go` | R1（s.history 字段+捕获+忽略客户端 history）、R2（--tools/GOPI_WEB_TOOLS 解析、Chat/ChatOps 分发、OnToolEvents→`event: tool` SSE）、bug003（signal handler + Shutdown + 503 interlock）、浏览器 JS（不回传 history + 工具折叠卡片 CSS/JS）、help/注释更新 |
| `cmd/agent/cli_web_test.go` | `TestHandleChatRoundsHistoryIntoRequest` 重写为 R1 契约（客户端 history 必须被忽略） |
| `cmd/agent/cli_web_align_test.go` | 新增 8 个测试（下表） |

---

## 3. 测试与验证证据

### 3.1 单元/集成测试（mock LLM，httptest 假 Claude SSE）

| 测试 | 锁定 |
|---|---|
| `TestWebChat_R1_ServerOwnedContextTwoTurnsNoClientHistory` | 两轮都不带 history，第 2 轮的模型 wire 请求里含第 1 轮 user+assistant 内容；`ws.history` 长度 4 |
| `TestWebChat_R1_ClientHistoryFieldAcceptedButIgnored` | 旧客户端带 `history`（注入标记文本）→ 请求 200 正常，但注入文本**不出现在**模型请求 |
| `TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames` | --tools 下：SSE `event: tool` call+result 帧；session JSONL 出现 tool_use+tool_result；**真实 tools.Write 落盘**（读回内容断言） |
| `TestWebChat_R2_DefaultTurnHasNoToolFrames` | 默认（无 --tools）0 个 tool 帧（纯 Chat 路径） |
| `TestWebToolsEnvVarEnablesTools` | GOPI_WEB_TOOLS 契约 |
| `TestWebChat_Bug003_ShutdownWritesSessionEnd` | 一轮 + finalize：session_end 恰 1 个、台账 in_progress→completed |
| `TestWebChat_Bug003_ShuttingDownFlagRejectsNewTurns` | shuttingDown 后 /api/chat 返 503 |
| `TestWebToolsSignalShutdownWritesSessionEnd` | 真子进程跑 `gopi web`（runWeb 信号 handler 生效）→ SIGTERM → 进程退出 + 恰 1 个 session_end（无 key/无二进制时 skip） |

结果：
```
go test ./cmd/agent/ ./internal/agent/ -count=1   → ok（全绿）
go test ./... -count=1                            → 除 internal/deepseek 外全 ok（见遗留）
go test ./cmd/agent/ -race -run 'TestWebChat_...' → ok（并发守护测试保持绿）
```

### 3.2 live E2E 验收（GLM glm-5.3-flash，隔离 GOPI_HOME=/tmp/gopi-p0-e2e）

```bash
$ HOME=/tmp/gopi-p0-e2e GOPI_HOME=/tmp/gopi-p0-e2e ./bin/gopi web --tools --addr=127.0.0.1:18321
gopi web: tools ENABLED — turns run the full gopi toolset with no approval gate (loopback only, local-shell trust)
```

**R2 + R1 验收（真实写文件 + 上下文连续）**：

第 1 轮（要求真实写文件并 cat 回读）：
```
turn1.sse: 6664 bytes, grep -c 'event: tool' = 10（5 个 call + 5 个 result 帧）
event: done → "任务已完成并确认：文件 /tmp/gopi-p0-verify/hello.txt 写入 Hello 成功，cat 回读一致"
$ cat /tmp/gopi-p0-verify/hello.txt
Hello                      ← 模型经 write 工具真实落盘
```

第 2 轮（**curl 完全不带 history 字段**，R1 验收）：
```
→ "文件名是 hello.txt，位于 /tmp/gopi-p0-verify/ 目录下，内容为 Hello（共 6 字节…）。
   上一轮已用 bash cat 回读确认过内容一致。"
```
—— 服务端上下文内化生效：裸 API 调用者两轮连续（bugs-found/006 的"失忆"复现路径失效）。

阴性对照第 3 轮：
```
Q: 我的密码短语是什么？
→ "不知道。你没有告诉过我任何密码短语…刚才的任务只涉及创建 /tmp/gopi-p0-verify/hello.txt 并回读内容 Hello。"
```
—— 上下文恰好 = 本会话真实历史（没有把无关内容幻觉进来）。

**bug003 验收（SIGTERM 优雅停机）**：
```
$ kill -TERM <server pid>
server.log:
  gopi web: terminated — shutting down, finalizing chat session…
  [gopi] task ledger: completed 334e7729-… (auto)
  gopi web: server stopped, chat session finalized.
session JSONL 事件统计（单文件 334e7729-511b-48ba-86ae-e3b52af17bb4.jsonl）：
  {session_start:1, user:3, assistant:14, request:7, tool_result:5, session_end:1}
末行：{"type":"session_end","exit_code":0,"total_iterations":3,"total_tokens":53010,...}
```
—— 3 轮全部落在**同一个** session JSONL（tool_use/tool_result 齐全），SIGTERM 后恰 1 个 session_end + 台账 completed（bugs-found/003 的泄漏路径失效）。

---

## 4. 遗留问题

1. **SIGKILL 无 session_end（已声明局限）**：OS 直接回收进程，任何用户态 handler 都不运行。日志停在最后一个 Sync 点（tool_result 级即时 Sync / 轮级 Sync），session 可读但缺 session_end 行。如需完全闭环，唯一途径是外部 wrapper（systemd/supervisor 发 SIGTERM 而非 KILL），或引入周期性 checkpoint 行——P2+ 评估。
2. **`internal/deepseek/TestNewVisionClient_ReusesAPIKey` 在本机失败——master 同样失败（环境相关，非本分支引入）**：真实 `~/.gopi/settings.json` 带 `vision_base_url: open.bigmodel.cn`，测试期待 DeepSeek 默认值。属 vision 测试对宿主配置的依赖，与 web-align P0 无关，建议单独修复（测试内 stub settings 路径或 t.Setenv 隔离）。
3. **浏览器刷新丢展示历史（设计文档风险 2，P1 范围）**：R1 后 transcript 在服务端，页面刷新后显示层为空；`GET /api/transcript`（session JSONL 回放）是 P1 计划项。
4. **`maxWebHistoryMessages` 成为死常量**：留作旧契约的文档性标记；P1 清理时可删（连带 legacy `webChatMessage` 类型评估）。
5. **web 审批缺失（P3）**：`--tools` 是全有/全无开关；DSH 式逐工具审批（危险工具需浏览器确认）未实现，已在 help/横幅/代码三处声明风险。
6. **finalizer 与正常 halt 的重复锁域**：`finalizeChatSession` 在信号路径与 Serve 返回路径都调用一次（两路互斥，select 语义保证），无重复写 session_end 的路径；`SessionLogger.Close` 幂等兜底。
