<!-- doc-class: normative -->
# Final Brief — gopi web 模式对齐 P0

分支：`feat/web-align-p0`（worktree `~/workspace/gopi-web-align-p0`，基于 master 5c1f7b9）
Commits：`9fe3d48`（feat，5 files +878/−87）+ `361f162`（devdoc）。未 merge master，未触碰 master。
构建：`CGO_ENABLED=0 go build -buildvcs=false -o bin/gopi ./cmd/agent`（worktree bin/gopi 自建）。

## 改动文件清单
- `internal/agent/loop.go` — Agent 加 `OnToolEvents func(ToolEvent)` 字段+注释（核心循环零改动）
- `internal/agent/chat_ops.go` — ToolEvent 类型 + emit 助手；ChatOps 工具轮执行前后各发一次
- `cmd/agent/cli_web.go` — R1：webServer.history 服务端权威（每轮后捕获 ag.Ctx.Messages），请求 history 字段接受即忽略；R2：`--tools`/`GOPI_WEB_TOOLS` → ChatOps 分发 + OnToolEvents→SSE `event: tool` + 浏览器折叠卡片；bug003：SIGINT/SIGTERM→http.Server.Shutdown(5s grace)→finalizer；JS 只发 `{message}`；help/横幅更新
- `cmd/agent/cli_web_test.go` — 旧 history 回传测试重写为 R1 契约
- `cmd/agent/cli_web_align_test.go` — 新增 8 个回归测试
- `devdoc/web-align-p0.md` — 方案 vs 设计文档差异、live 证据、遗留问题

## 验收命令 + 输出证据（live，GLM glm-5.3-flash，GOPI_HOME=/tmp/gopi-p0-e2e 隔离）
1. `gopi web --tools` 启动横幅：`tools ENABLED — … loopback only, local-shell trust`
2. R2 真实工具：turn1 curl → SSE 含 **10 个 `event: tool` 帧**（5 call+5 result）；`cat /tmp/gopi-p0-verify/hello.txt` → `Hello`（模型经 write 真实落盘）
3. R1 上下文：turn2 **不带 history** → "文件名是 hello.txt…内容为 Hello"（答出第 1 轮内容）；阴性对照（密码短语）→ "不知道"
4. bug003：SIGTERM → server.log 出现 `terminated — shutting down, finalizing chat session…` + `task ledger: completed`；session JSONL 统计 `{session_start:1, user:3, assistant:14, request:7, tool_result:5, session_end:1}`，末行 `session_end total_tokens=53010` —— 单会话单 session_end，tool_use/tool_result 齐全
5. 测试：`go test ./cmd/agent/ ./internal/agent/ -count=1` 全绿；`go test ./...` 除 `internal/deepseek` 一个既有环境相关失败外全 ok（master 同样失败，非本分支引入）；`-race` 并发守护测试绿

## 遗留问题（详见 devdoc §4）
1. SIGKILL 无 session_end（OS 不执行用户代码；已声明 best-effort-impossible）
2. `TestNewVisionClient_ReusesAPIKey` 环境相关既有失败（真实 settings.json 的 GLM vision_base_url 覆盖默认值；master 同样失败）——建议单独修
3. 浏览器刷新丢展示历史 → P1 `GET /api/transcript`（session JSONL 回放）
4. web 审批缺失（P3）：`--tools` 全有/全无，已在 help/横幅/代码三处声明"loopback only, local-shell 级权限"
5. `maxWebHistoryMessages` 死常量留存作旧契约文档标记（P1 清理）
