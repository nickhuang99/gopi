<!-- doc-class: normative -->
# Crash Dump 设计与实现（feat/crash-dump）

> Epic: GO error handle 应 dump error details for investigation——像 linux core dump；
> 且必须 toggle task status to failed 否则 PA 永远收不到 failure 通知。
> 分支: `feat/crash-dump` (基 master c2bda60)，worktree `~/workspace/gopi-crash-dump`。

## Completed

### ① session_end 扩展 error 块（internal/agent/session.go）
- `LogSessionEnd(exitCode, iter, tokens, elapsed, failure *ErrorRecord)` 新增第 5 参。
- failure=nil（正常 exit_code=0）→ 事件**无 error 块**（⑤ at_exit 语义：error 块的存在本身就是失败标记）。
- failure!=nil → `error` 块含 epic 全字段：
  `category`（bug036: account/network/4xx/5xx/vendor/panic/unknown）、`http_status`、`error_body`(4KB bounded)、
  `last_request_digest`（model+messages 数+last role+bytes）、`failed_iteration`、`recoverable_hint`、
  `transport_error`（网络错误时）、`dump_path`、`remedy`（**仅 account 级**，见下）。

### ② crash dump 文件（internal/agent/session.go WriteCrashDump）
- `<session-id>.dump.json` 与 `<session-id>.jsonl` **同目录同命名族**。
- 内容：`type=crash_dump`、完整最后请求体（`request`，1MiB tail-cap）、完整错误响应（`error_response`，64KB）、
  `last_request_digest`、`failed_iteration`、`recoverable_hint`、上下文（elapsed/iterations/total_tokens/recent_tool_calls）、
  panic 路径另有 `panic_stack`（16KB）。
- agent resume 可读（同目录家族）、PA 可查、gopi server 可预检——JSON 一文件全量。
- Best-effort：写失败只打 stderr，绝不掩盖真实错误。

### ③ task 状态联动 + 失败摘要进 summary 尾（internal/agent/taskregister.go）
- `finishSessionTaskRecorded`：exit!=0 → status=**failed**，summary 尾追加
  `— error(category=4xx http=400 iter=N recoverable=true dump=<path> msg="…")`
  （`ErrorRecord.SummaryDigest`，200 字符 msg cap）；account 级再带 `remedy="…"`。
- **failed 行的 resume 指针随分类走（2026-09-11, fix/llm-error-taxonomy）**：
  `autoTaskResumeHintFormat`（"recoverable: `gopi resume …`"）逐字不变地留给
  所有非 account 失败；`Category == "account"` 时改用 `autoTaskTerminalHintFormat`
  （"NOT recoverable by resume: … 会重放回同一个失败，先修因再 resume 取上下文"）
  ——以前每个 failed 行都喊"recoverable"，等于叫运维把任务重放进一个欠费账号。
- notification 管道（task-notify 读 summary）自然把 failure 携带到 PA 订阅端——实测见下。
- runEngine 收尾传**同一个** ErrorRecord 给 LogSessionEnd 和 finish hook（同一错误、同一 digest，不再二次收集）。

### 错误收集单一路径（internal/agent/crashdump.go + deepseek/lasterr.go）
- **收集点在 deepseek client**：`Client.LastErrDetail`（LastHTTPFailure，mutex 保护）。
  四个发送点（非流/流 × retryDo/ladder roundtrip）在非 200 或 transport error 时 `record(status, body, path, stream, err)`。
- `FailureSnapshot.Category()`：纯派生的 bug036 分类；`RecoverableHint()`：network/5xx/vendor/429/1214→true，其余 4xx→false。
- **account 级优先（2026-09-11, fix/llm-error-taxonomy）**：`Category()` 先问
  `IsAccountLevelStatus(status, body)`（与 retry 路径**同一个**谓词，Rule 13/14 不复制厂商码知识），
  命中即 `category="account"`、`recoverable_hint=false`、并带 `remedy`（`FailureSnapshot.RemedyText()`，
  由 `AccountLevelMessage` 生成 —— 与 retry 路径同一措辞）。GLM 1113 藏在 **429** 里，与真限流同状态码，
  所以 account 判定必须排在 429 分支**之前**；真限流 429 行为完全不变。
- agent 侧 `collectErrorRecord`：LLM 错误、stream error（vendor）、网络错误、panic 全走这一个函数。
- panic 路径：`panicCollectorStore(msg, stack)` 单槽交接 → Category=panic、RecoverableHint=false。

### ④ panic recover（internal/agent/loop.go + crashdump.go）
- runEngine 新增**第一个** defer（LIFO 最后执行——happy path 的收尾 defer 先跑，panic 时 recover 兜底）：
  recover → 存 panic 上下文+统计快照（atomic wrapper）→ `recoverRunPanic`：
  写 dump → 写 session_end(error) → Close → finishSessionTask(failed) → **re-panic**。
- re-panic 保证退出码非零 + Go runtime traceback 完整——recover 只留面包屑，不吞崩溃。

### ⑤ 孤儿 session 检测（kill -9 场景）
- `HasSessionEnd(path)`：tail 扫描 JSONL 是否有 session_end。
- `DetectOrphanSessions(exclude...)` / `BackfillOrphanCrashDumps(currentSessionID)`：
  启动时对无 session_end 的孤儿写 `crash_dump`（orphan:true, category:crash, note, session_log 指路, recoverable:true）。
  幂等：已有 dump 跳过。
- **关键坑（root-cause fixed）**：backfill 必须排除**当前进程自己的 session**——它的 JSONL 正在写、当然没有 session_end，
  否则每次启动把自己的活 session 当孤儿 dump 一份（E2E 抓到：`gopi` 启动 2 秒后出现自孤儿 dump）。

## Not Done
- 进程级 panic E2E（kill 一个真实 panic 中的 gopi 进程）：由 in-process 测试
  `TestRecoverRunPanic_WritesDumpAndRepanics` 覆盖同一条 defer→dump→session_end(error)→re-panic 链路；
  未构造出能让 loop 必然 panic 的 mock（畸形 wire 均被优雅处理）。
- 5xx 经 ClaudeChat 包装不重试是**既有**行为（合成错误短路 retryDo 的 status 分支，见 memory #1156），
  本单不修（改重试语义超出本 Epic 范围，会改变生产行为）；LastErrDetail 不受影响——它记的是最后一次失败事实。
- session_end error 块的 `recoverable_hint` 目前是启发式（status+body 关键字），未接 bug036 完整分类矩阵实现。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- 单元测试（-run 过滤，count=1）:
  - agent: TestLogSessionEnd_*（无 error 块/有 error 块）、TestWriteCrashDump_*（命名族/内容）、
    TestDetectOrphanSessions（孤儿+幂等）、TestCollectErrorRecord、TestSummaryDigest、
    TestRecoverRunPanic、TestErrorCategoryMatrix（bug036 矩阵 9 例）——**PASS**
  - deepseek: TestLastErrDetail_*（400 记录/retry 耗尽取最后/流式 503/记录持久化）——**PASS**
- E2E（mock LLM + 隔离 GOPI_HOME）:
  1. **mock 400**：session_end 含 error 块（4xx/400/digest/dump_path）+ `<id>.dump.json` 存在
     （request/digest/failed_iteration/recoverable 全在）+ task 行 **failed** +
     summary 尾 `error(category=4xx http=400 recoverable=true dump=… dump.json msg="…1214…")` ✅
  2. **kill -9**：杀掉 mid-round 的 gopi → 重启后 `[gopi] crash-dump: backfilled 1 orphan session dump(s)`，
     孤儿 session 得到 orphan dump；**重启自己的活 session 未被误 dump**（排除当前 session 后）✅
  3. **panic recover**：单元级证明 dump+session_end(panic)+finish(failed)+re-panic 全链路 PASS ✅

## 关键设计决策
- 错误收集挂 deepseek client（发送点 status/body 在手）而非 agent 侧解析错误字符串——字符串会丢 http_status。
- dump 命名对齐 session 文件族（`<id>.dump.json`），不引入新目录/新命名规则——resume/server/PA 一致可寻。
- session_end 只带 bounded 错误体（4KB）；完整 request（可到 MB 级）只在 dump 文件——JSONL 行不膨胀。
- task summary 尾的错误 digest 是 PA 侧唯一新增依赖面：纯文本、单行、可 grep。
