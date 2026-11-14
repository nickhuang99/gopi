<!-- doc-class: normative -->
# SIGHUP 落进 signalrec 与 mode handler 之间的缝

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a15bb9-9f77-741e-b36a-e8883e4e8e14-r1`，2026-10-21。

## 症状

前台 `gopi worker` 收到 SIGHUP（终端 session 拆除/ssh 断开/pty 消失时内核发的那一个）后：

- **不死**；
- **不 drain**；
- 自己的 lifecycle 日志**一个字都没有**；
- **继续对已经不存在的终端 claim 任务**。

同一进程收到 SIGINT 则正常 `worker: interrupt received — draining`。

## 机制（5-why）

1. 为什么不 die？—— `internal/signalrec` 的 `watchedSignals`（signalrec.go:1414-1417）为**每一个** gopi verb 注册了 SIGHUP。`signal.Notify` 注册即**取代默认处置**（该文件 :1304 的注释自己写着这一点），所以 unix 默认的“SIGHUP 杀掉进程”不再发生。
2. 为什么不 re-raise？—— `signalForwarding()`（:1713-1715）= `forwardsDefault(role) && !handlersClaimed && !noForward()`；`forwardsDefault`（:339-341）= `!modesThatOwnTheirSignalHandlers[role]`；那张表（:315-325）把 `"worker"` 和 `"chat"` 都标为 true，注释说这些 mode “install their own SIGTERM/SIGINT handling”。
3. 为什么这个前提不成立？—— worker 的真实集合是 `{SIGTERM, SIGINT, SIGUSR1}`（`workerStopSignals`），chat 是 `{os.Interrupt, SIGTERM}`。**两个集合都没有 SIGHUP。**
4. 所以信号去哪了？—— 记录器记录后既不转发也不 re-raise，`signal.Stop` 也没人做；handler 不认识它 → **静默丢弃**（Rule 15）。
5. 为什么长期没人发现？—— 因为丢弃是**安静的**：provenance 台账里有一条 SIGHUP 记录（`"sourced":false`），看起来“已经处理了”；而 worker 自己的日志空无一字。覆盖缺口只在**终端真的消失**时可见，而那时通常没人在看。

**根因一句话**：`modesThatOwnTheirSignalHandlers` 是一个**声明**，它与每个 mode 真实的 `signal.Notify` 集合之间没有任何机器检查；声明比事实宽的地方，信号就被吞掉。

## 修复

把 SIGHUP 放进两处真实集合（worker / chat），**而不动 signalrec 的表**：

- 修 handler 集合 → 表的断言**变成真的**，一处 disposition 的所有权仍然只有一个写入者（该 mode 自己）。
- 改表（把 `"worker"` 改成 false）会把 SIGHUP 推回 `reRaise` 的默认死亡路径 —— 正是本 epic 要避免的“在飞的回合被杀”。

SIGHUP 是 **graceful**，不是 force：`workerForceSignalRequested` 仍只认 SIGUSR1。hangup 的语义是“你的终端没了，收尾离开”，不是“丢掉这个回合”。

## 特意没做的两件事

1. **没有把前台 worker/chat detach。** 前台面的全部价值就是绑在操作员终端上（Ctrl+C、status 行、`you>` prompt）；强行 setsid 会破坏 Ctrl+C 语义（脱离控制终端后终端不再向前台进程组发 SIGINT）。detach 已有声明式入口：`--daemon`（`cmd/agent/cli_daemon_spawn.go:36` setsid；`cli_daemon_scope.go` 用 `systemd-run --scope --user` / 进程内 cgroupfs escape 处理 setsid 碰不到的 cgroup 那一半）。
2. **没有为 SIGQUIT 加处理**：signalrec.go:1406-1411 明确保留 Go runtime 的 goroutine stack dump，那是诊断工具，不是缺口。

## 同一缺陷很可能还在别的 face 上（未验证，已按 Rule 41 提行）

`modesThatOwnTheirSignalHandlers` 里还有 `serve` / `web` / `resume` / `session` / `task run` / `task merger` / `merger`。本次只核实了 `worker` 与 `chat`。其中任何一个是“表里声明拥有 handler、真实集合却没有 SIGHUP”，就有一模一样的静默丢弃。

可做的机器守卫（未做）：把每个 mode 的集合导出成可反射的表，断言 `modesThatOwnTheirSignalHandlers` 中每个 `true` 的 mode，其真实 Notify 集合 ⊇ 记录器接管的那组致命信号。

## 验证配方（可复用）

```bash
# 1. 真跑前台 worker，取四元组
ps -o pid,ppid,pgid,sid,args -p <pid>

# 2. 真发 SIGHUP，观察是否受控收尾（不是“是否存活”）
kill -HUP <pid>
sleep 10; ps -p <pid>          # 空闲 worker 在 claim 边界消费，最长一个 idle 周期
grep -i "hangup\|draining" <worker log>

# 3. 机器记录侧
cat ~/.gopi/sessions/process-<pid>/signal-provenance.jsonl
```

**关键判据**：不是“进程还活着”（修复前也活着），而是**日志里有没有 hangup→draining 那一行，以及进程是否在 claim 边界退出**。
