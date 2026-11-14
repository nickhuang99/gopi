<!-- doc-class: normative -->
# bash 的操作者取消：从 chat 的 Esc 到子进程组

Task 01a116d7-854b-7021-89a3-e00d9f6352d9 · `internal/tools/bash.go`, `internal/agent/loop.go`,
`internal/agent/chat_ops.go`

## 缺陷（实测的形态，不是猜测）

`bashInDir` 第 4 步用 `context.WithTimeout(context.Background(), absTimeout)` 自造根 ctx。
`gopi chat` 的 Esc 走的是完全另一条路：`cli_chat.claimTurnInterrupt` →
`agent.InterruptRunBy` → `runScopedRoot()` 发布的 `runCtx` 被 cancel。两条路从不相交，所以
**回合解开了，命令还在跑**：操作者盯着 `sleep 300` 按 Esc，屏幕上打出
`[gopi] turn interrupted by escape`，子进程却继续活到自己的预算用尽（RED 取证里它活到 120s
idle 网才死，tool_result 写的是 `[command killed: no output for 2m0s]`）。

同一处的第二个缺陷在 kill 分支里：watcher 只在 `ctx.Err() == context.DeadlineExceeded` 时才杀，
**把 `Canceled` 一并排除**——即使把 ctx 接通，取消也会被这条判断挡掉。

## 修法（四件必须一起做的事）

1. **ctx 系到调用者**：`bashInDirCtx(ctx, …)` 从调用者 ctx 派生命令 ctx；`absTimeout == 0`
   的 "无限时" 分支用 `WithCancel(callerCtx)`，**不是** `WithCancel(Background())` —— 退回
   Background 就又把取消丢了。旧签名（`bashInDir` / `bashWithTimeoutInDir`）变成薄包装，
   传 `context.Background()`，语义逐字不变。
   `timeout_sec → (abs, idle)` 抽成 `bashBudget()`，ctx/非 ctx 两条入口共用一份（Rule 14）。

2. **统一的安全 kill helper**：`killChildTree(pid)`。**先量后杀** —— `syscall.Getpgid(pid)`
   且 `pgid == pid` 才 `kill(-pgid)`；否则单杀 `pid` 并逐个信号 `bashDescendantPIDs()` 枚举出的后代。
   配方照抄 `cmd/agent/cli_tasks_worker_stop.go:signalRunChild`，理由在那里已被实测记过：
   registry 记的 group 是**假设**，真实组常是调用者的组，`kill(-childPID)` 会 ESRCH 被当无害吞掉
   （报告成功、什么都没发生）。这里赌注更高：bash 的调用者就是 `gopi chat` 进程本身，
   一次没测量的组信号会把操作者的终端带走。三处 kill（ctx watcher / idle monitor）收敛到它。

3. **归因是一个值，不是一组标志**：`killReasonSlot`（先写者胜）+ 三个命名常量
   `KillReasonOperatorCancel` / `KillReasonAbsoluteTimeout` / `KillReasonIdleTimeout`。
   ctx 路径的归因来自 `cmdCtx.Err()`：`DeadlineExceeded` = 我们自己的预算，其余 = 调用者
   （即操作者）。取消分支排在 switch 最前，任何超时都不许把它改写。原来的
   `idleKilled atomic.Int32` 被删除——它只能表达"是不是 idle"，表达不了"是谁"。

4. **文案**：`BashCancelledMarker = "[command cancelled by the operator]"`（导出，供
   `internal/agent` 的读者引用，避免第二处拼写），错误串 `command cancelled by the operator`。

`internal/agent` 侧把 run ctx 一路带下去：`executeToolBatch(ctx, …)` /
`executeToolBatchParallel(ctx, …)` / `executeToolCtx(ctx, tc)` / `executeToolDefaultCtx(ctx, tc)`；
`executeTool` / `executeToolDefault` 保留原签名，内部走 `a.runScope()` = `runScopedRoot()`
发布的 `runCtx`（没有 run 时 Background）。并行批次原本也在 `context.Background()` 上叠加预算，
同样改为叠加在调用者 ctx 上。ToolBash 分支改调 `BashInDirCtx` / `BashWithTimeoutInDirCtx`。

## 两个坑（都是实测出来的，不是理论）

- **`kill(pid, 0)` 对 zombie 成功**。所以"子进程到底死了没"不能在测试里用它判定：被 SIGKILL 的
  子进程在父进程 reap 之前是 Z，`kill(pid,0)` 照样返回 nil。端到端用例因此读 `/proc` 并跳过
  `state == Z` 的行；单测里走 `cmd.Wait()` 的路径则可以直接问进程表（bash 是我们的直接子进程，
  `bashInDirCtx` 自己 reap）。
- **`defer cancel()` 在正常返回时也关 ctx**。watcher 的 `<-ctx.Done()` 不区分"超时"与"我们收工了"，
  原代码因此必须写 `if ctx.Err() == DeadlineExceeded`（这正是排除取消的那条判断）。改法不是放宽它，
  而是把 **`Wait()` 与发信号排序**：`killMu` 保护 `commandExited`，watcher 拿锁后先看它，
  已经 reap 就什么都不做。这条窗口被收窄到"watcher 读过 exited=false 之后、发信号之前子进程恰好被回收"
  ——彻底消除需要 pidfd，属于另一个改动，本报告如实记录它没有被消除。

## 验证

- `go test ./internal/tools/ -run 'TestBashInDirCtx|TestKillChildTree' -count=1`（5 例：
  取消及时返回 + 文案 + 本进程存活 / 孙进程随树而死 / **负向**非组长不组杀且同组兄弟活着 /
  nil ctx 语义不变 / 取消不报 exit 码）
- `go test ./testsuite/ -run 'TestChatPTY_EscapeKillsARunningBashCommand' -count=1 -timeout 300s`
  （真 PTY + 真 `gopi chat --tools` + mock LLM 触发 `echo $$ > pid; sleep 600`，发 lone Escape，
  读 `/proc` 判组死，读 transcript 判文案）
- RED 取证：把 `callerCtx` 改回 `context.Background()` 后两处都 FAIL（单测报 "the operator's key
  is inert against a running command"；PTY 用例报命令活到 120s idle 网），还原后 PASS。
