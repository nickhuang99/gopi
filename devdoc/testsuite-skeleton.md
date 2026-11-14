<!-- doc-class: normative -->
# devdoc: testsuite 骨架 —— 三模式 E2E 冒烟套件

> 分支 `feat/testsuite-skeleton`（基于 master `5c1f7b9`，worktree
> `~/workspace/gopi-testsuite`）。任务书：`~/workspace/gopi-chat-web-align/brief-testsuite.md`。
>
> **作者归属（诚实标注）**：`internal/testutil/` 三件套与 `testsuite/` 三份
> 测试的**初稿来自上一个会话**（因配置切换/API 400 中断，未 commit、未跑通）。
> 本会话接手：评估初稿 → 修掉 1 个 mock 线格式 bug（三模式 6 例因此全挂）→
> 补齐缺口（`scripts/e2e_smoke.sh`、`TestRunEngine_RejectsEmptyInput` env
> 修复、本文档）→ 全绿。逐文件归属见 §6。

## 0. 一句话

`scripts/e2e_smoke.sh` 一键：构建 `bin/gopi` → 跑 `testsuite/`（单任务 / chat
/ web 三模式，mock LLM，沙箱 GOPI_HOME）→ 分模式 pass/fail 报告。
**零真实 API、无需 key、无网也能绿。**

```
single task : 3 pass / 0 fail
chat REPL   : 3 pass / 0 fail
web (SSE)   : 2 pass / 0 fail
RESULT: PASS
```

---

## 1. Mock LLM 基建盘点（交付项 1）

gopi 里 mock 的**唯一注入点**是 `deepseek.Client.BaseURL`，共有两条路到达：

| 路径 | 用法 | 已有先例 |
|---|---|---|
| **进程内注入** | 直接构造 `&deepseek.Client{BaseURL: server.URL, APIKey: "test-key"}` 塞进 `Agent.Client` | `internal/agent/chat_ontest_test.go` 的 `newTestAgent(t, server)` |
| **子进程注入** | 起真二进制，把 `ANTHROPIC_BASE_URL`/`DEEPSEEK_BASE_URL` 指向 httptest server | `cmd/agent/cli_web_test.go` 的 `mockStreamingServer` |

SSE 帧写手（Claude Messages 线格式）有两份**包内私有**实现，二者等价：
- `internal/agent/chat_ontest_test.go` — `writeClaudeSSE` + `claudeTextStreamFragmented`（多段 text_delta，验流式）
- `cmd/agent/cli_web_test.go` — `textSSE`（单段）

**关键局限：这两份都住在 `*_test.go` 里，跨包不可 import。** testsuite 需要
在 `testsuite` 包里驱动真二进制，所以把同一套模式提升为共享包 ——
`internal/testutil`（这是本交付新增，不是重新发明：帧格式、usage 字段、
stop_reason 语义逐字段照抄上面两个先例）。

### 解析顺序（写 mock / 排障必读）

- **key**：`ANTHROPIC_AUTH_TOKEN` > `DEEPSEEK_API_KEY` > settings.json
  （`internal/deepseek/claude_client.go ResolveClaudeAPIKey`）。测试一律注入
  `test-key`，绝不依赖真 key。
- **base URL**：`ANTHROPIC_BASE_URL` > `DEEPSEEK_BASE_URL` > settings >
  默认。testutil 两个都设，future-refactor 也不会漏。
- **端点**：client POST 到 `{base}` + `/anthropic/messages`（或 `/v1/messages`，
  由 `messages_path` 配置决定）。mock 只挂 `/anthropic/messages`。
- **数据根**：`GOPI_HOME` → `~/.gopi/{sessions,raw,memory.db}`。子进程测试
  必须把它指到 `t.TempDir()`，否则会话文件落进真用户 profile。
- **零真实 API 的两条保险**：
  1. `testutil.MockEnv` 剥掉父环境里**所有** `ANTHROPIC_*`/`DEEPSEEK_*`/
     `GOPI_*` 再注入 mock 地址 —— 真 key/真 base URL 泄不进来。
  2. 套件里被触发的工具只有 read/write（本地）与 `memory_search`
     （本地 SQLite FTS，`internal/tools/memory.go` —— 无 embedding/HTTP），
     所以唯一的网络端点就是 mock 本身。

---

## 2. 套件结构

```
internal/testutil/           共享基建（可被任何包的测试复用）
  mockllm.go   脚本化 mock server：NewMockLLM(t, script...) 按"第 N 个请求
               → script[N]"应答；超出脚本长度时重复最后一项（确认轮/推进轮
               拿到无害文本答案，循环自然终止）。Fail=true → HTTP 400
               （client 只重试 429/5xx，即非重试型硬错）。记录每个请求体，
               供线格式断言（RequestBody(n)）。
               帧写手：TextSSE / ToolUseSSE / WriteClaudeSSE（导出，别处也能拼）。
               ParseSSE / CountSSEEvents —— 解析 web /api/chat 的 SSE 输出。
  run.go       子进程 runner：GopiBin（GOPI_BIN > <module root>/bin/gopi，
               没有则 SKIP —— 裸 `go test ./...` 在干净 checkout 上保持绿）；
               RunGopi（跑到退出，带超时）；StartGopi/Proc（常驻进程，
               web 模式用）；WebBaseURL（从 stderr 抓 "open: http://..." 横幅，
               配合 --addr=127.0.0.1:0 的临时端口）。
  session.go   会话 JSONL 结构断言库（交付项 3，所有后续 feature 的验收地基）：
               LoadSession / AssertSingleSession（一个文件=一个会话，文件名=
               sessionId）/ AssertEventOrder（子序列匹配，容忍夹在中间的
               request 行）/ AssertCounts / ToolUseResultPairs（tool_use ↔
               tool_result 按 id 配对：不重不漏、result 在 use 之后）/
               ToolUses / UserContents / SessionEndExitCode。

testsuite/                   三模式 E2E（驱动真 bin/gopi 子进程）
  task_test.go   单任务模式（echo task | gopi / 位置参数）
  chat_test.go   chat REPL（非交互 stdin 喂入 + /quit）
  web_test.go    web 模式（真 `gopi web` + 真 HTTP，浏览器同款请求）
```

### 8 个用例与各自的守卫点

| 用例 | 模式 | 守卫 |
|---|---|---|
| `TestTask_HappyPath_Stdin_ToolExecutesAndSessionLogs` | task | **工具真执行**（write 落盘在沙箱 cwd）+ 事件序 `session_start→user→assistant(tool_use)→tool_result→assistant→session_end` + 配对 + 文件名=sessionId |
| `TestTask_HappyPath_Positional` | task | 位置参数路径；第二轮 happy path（冒烟契约：每模式 2 个 happy） |
| `TestTask_ErrorScenario_LLMFailure` | task | mock 400 → 退出码非 0 + stderr 报错 + session_end 仍落盘且 `exit_code=1` |
| `TestChat_HappyPath_TwoTurnsSameSession` | chat | 非交互两轮 + `/quit` 干净退出（stderr `bye.`）+ **两轮同 session**（1 个 session_start / 2 user / 2 assistant / 1 session_end） |
| `TestChat_ToolsToggle_ToolExecutes` | chat | `/tools` 中途切换：线格式断言（第 2 个请求体带 `write` 工具定义）+ 工具真执行 + 仍单会话 |
| `TestChat_ErrorScenario_TurnFailure` | chat | 首轮 400 → `[gopi] error:` + 非零退出 + session_end `exit_code=1` |
| `TestWeb_HappyPath_TwoTurnsSingleSession` | web | 真 runWeb + 真 HTTP：两轮 `/api/chat` SSE（`delta`≥1 + `done.result`==mock 答案）+ 单会话（**无 per-turn session_end**，server 关停才写一次） |
| `TestWeb_ErrorScenario_BadRequests` | web | 空 message 400 / 坏 JSON 400 / 非 POST 405 / 根页 200 |

断言口径（写新用例前先看这三条，省得跟循环的真实行为打架）：
- **事件序用子序列匹配**（`AssertEventOrder`），不要全序列相等：循环会插
  `request` 行，且完成度检查可能追加一轮。
- **task/chat 的最终答案 ≥40 字符**（`loop.go minFinalAnswerChars`）：chat
  Ops 模式首轮无工具即退出、task 的 R3 first-end_turn 信任都以此为门槛，
  太短的答案会多跑确认轮、脚本索引就错位了。
- **多一轮是正常的**：task happy path 脚本 2 项、mock 实际收到 3 个请求 ——
  第三轮来自 `maybeEnforceCompletionStore`（task-end 完成度落库强制检查），
  mock 重复最后一项应答后循环退出。这是**设计行为**，不是套件 bug。

---

## 3. 如何为新 feature 添加 E2E（后续验收都走这条路）

1. **想清楚 mock 脚本**：几轮、每轮 text 还是 tool_use、错误场景要不要
   `Fail: true`。tool_use 的 id 自拟（`call_xxx`），会话断言靠它配对。
2. **沙箱**：`home := t.TempDir()`（GOPI_HOME）+ `work := t.TempDir()`
   （进程 cwd）。web 模式只要 home。
3. **驱动**：
   - 单任务：`testutil.RunGopi(t, args, stdin, testutil.MockEnv(t, home, llm.URL()), work)`
   - chat：同上，`args=[]string{"chat"}`，stdin 里排好轮次 + `/quit`
   - web：`testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, ...)` →
     `proc.WebBaseURL()` → `http.Post(base+"/api/chat", ...)` → `proc.Stop()`
4. **断言**：文件副作用（工具真执行，别只信 mock 收到请求）→
   `testutil.LoadSession(t, home)` → `AssertSingleSession` +
   `AssertEventOrder` + `ToolUseResultPairs`。**feature 特有的业务断言**
   （新工具被调用、新事件字段）加在这里。
5. **跑**：`scripts/e2e_smoke.sh -run TestXxx`（会先重建 bin/gopi）。
6. **命名**：`Test<Mode>_<Feature>_<Invariant>`，Mode ∈ {Task, Chat, Web} ——
   smoke 脚本按前缀分模式出报告。

新工具接入套件的最小验收 = 一个 happy-path 用例让 mock 返回该工具的
tool_use，断言副作用落盘 + 会话配对。（Rule 11 的"LLM 真调用"验收是另一层：
需要真 LLM 场景，套件只验"工具被执行时行为正确"。）

---

## 4. 套件抓到的 bug（按约束不改被测代码，只记录）

**`internal/agent/loop.go` — tool_use 的 `input_json_delta` 若不是字符串，
输入被静默丢成空。**
Claude 线格式里 `partial_json` 是**含 JSON 的字符串**：
`"partial_json":"{\"path\":\"plan.txt\"}"`。初稿 mock 直接把参数 map
marshal 进去，发成了裸对象：
`"partial_json":{"content":"1. build the suite\n2. verify green","path":"plan.txt"}`。
循环收到后**不报错、不告警**，把该 tool_use 当作无输入执行，工具返回
`Missing required parameter: path`，然后照常把错误喂回模型继续跑 ——
整个 run 仍以 `exit_code=0` 结束（stderr 可见 `tool_calls=1: write 1`，
但磁盘上没有文件）。
- 证据：会话记录 `tool_use: name=write input=map[]`，`tool_result` =
  `Missing required parameter: path`（见本会话排查记录）。
- 影响：任何上游把 `partial_json` 发成对象，gopi 都会"成功"地什么都不做。
- 建议（后续 task）：`internal/agent/loop.go` 的 input_json_delta 累积处对**非字符串
  payload** 显式报错（或至少 stderr 告警 + tool_result 标注 malformed），
  并补一个单元测试。mock 侧已修（`testutil.ToolUseSSE`）。

其余观察（非 bug，记录在案）：
- `NewAgent` 在无 key 环境直接失败（`ANTHROPIC_AUTH_TOKEN not set`）——这是
  设计内的 fail-fast，但它让一批测试隐式依赖 shell 环境，见 §6 的修复。
- task 模式 happy path 的答案会在 stderr 上重复出现两次（stdout 一次 +
  stderr 回显两次），怀疑是回显路径重复打印；不影响功能，未深挖。

## 5. 验证（Rule 5）

- `go build ./...`：OK；`go vet ./internal/testutil/ ./testsuite/`：OK
- `gofmt -l`：本交付触到的 7 个 Go 文件全部格式化（master 上既有的
  `internal/agent/loop.go`/`trajectory.go` 等未格式化文件**不在本次范围**，未动）
- **无 key 环境全绿**：`env -i PATH=… HOME=/tmp/emptyhome-final go test ./...`
  → 11 个包全部 ok（含 `internal/testutil`，无 key、无 settings.json）。
  `TestRunEngine_RejectsEmptyInput` 在该环境下单独跑也 PASS。
- `scripts/e2e_smoke.sh`：PASS（§0 报告），且在 `env -i`（无 key）下复跑仍 PASS
- **跨版本冒烟**：用 master 当前 HEAD（`2b5a562`，含 web-align P0 的
  graceful shutdown）构建的二进制跑本套件 → 8/8 PASS。分支基点是
  `5c1f7b9`，master 已前进但不碰本交付的任何文件；套件对新旧两版代码都绿
- mock 线格式 bug 的回归守卫已验证有效：把 `ToolUseSSE` 临时改回裸对象，
  `TestToolUseSSE_PartialJSONIsAString` 立刻红（`cannot unmarshal object into
  Go value of type string`），还原后绿
- Commit（5 个逻辑单元，逐文件 add）：
  - `4f89924` feat(testutil): shared mock-LLM + session-assertion infra
  - `66d1e89` test(testsuite): three-mode E2E smoke — single task / chat / web
  - `01a4774` fix(agent): TestRunEngine_RejectsEmptyInput no longer needs a key env
  - `5a9e391` feat(scripts): e2e_smoke.sh — one-click three-mode smoke entry
  - 本条 docs commit（本文档）

## 6. 未做 / 缺口（诚实）

- **web 模式的 server 关停断言**没放进套件：`session_end` 在 `gopi web`
  关停时才落盘。杀进程后断言它属于 cmd/agent 的进程内测试
  （`TestWebChat_SinglePersistentSession`），套件里只断言 `session_end=0`。
  若要把"关停后落盘"也守住，需要在 Proc.Stop 后读文件 —— 已留作后续。
- **单会话并发**（两个浏览器 tab 排队）没有用例：web_test 只驱动单客户端
  顺序两轮。cmd/agent 已有进程内覆盖，套件层面留作后续。
- **vision / text2img / job_start / web_search** 等工具不在套件里：它们要么
  需要真实多媒体依赖、要么会碰网。套件先守住骨架（read/write + 会话结构），
  其余工具按 §3 逐个补。
- 上一会话的 `testsuite-r2.out`（空文件残留）已删除，未提交。
- `internal/testutil/run.go` 死代码 `describeProcOutput`（上一会话遗留）已删除。

## 7. 逐文件归属

| 文件 | 来源 |
|---|---|
| `internal/testutil/mockllm.go` | **上一会话初稿**；本会话修 `ToolUseSSE` 的 `partial_json` 线格式 bug + 注释 |
| `internal/testutil/run.go` | **上一会话初稿**；本会话删死代码 `describeProcOutput`（连带 `fmt` import） |
| `internal/testutil/session.go` | **上一会话产出**，本会话未改动（评估：结构、断言口径与循环真实行为相符，直接可用） |
| `internal/testutil/mockllm_test.go` | **本会话新增**：钉死 `partial_json` 必须是字符串（§4 bug 的回归守卫）+ mock 的脚本索引/重复语义 + 400 非重试语义 |
| `testsuite/task_test.go` / `chat_test.go` / `web_test.go` | **上一会话产出**，本会话未改（它们挂是因为 mock bug，不是测试写错） |
| `scripts/e2e_smoke.sh` | **本会话新增** |
| `internal/agent/session_gopihome_test.go` | **本会话修改**：`tempHome(t)` → `newTestAgentEnv(t)`，消除 key env 依赖 |
| `devdoc/testsuite-skeleton.md` | **本会话新增**（本文档） |
