<!-- doc-class: historical -->
# 测试进程把 SIGINT 送到 `systemd --user`：候选隔离点的实测比较与落地点

**日期** 2026-10-17 · **主机** hosta（桌面会话，GNOME/tty7）· **姊妹行** 01a1470f（incarnation 修复，已合入 `5639d632a`）

## 一句话

姊妹行修的是「把回收的 pid 认成了自己要杀的那个进程」；本行修的是**同一个投递点的第二个问题**——即使 pid 一点都没被回收、incarnation 完全吻合，只要它**此刻就是 `systemd --user`**，投递 SIGINT 就等于拆掉操作员的整个桌面会话。所以落地点是**投递点的身份拒绝**，不是 cgroup、不是 pid namespace、不是运行器开关。

## 候选隔离点：实测比较（不是断言）

原始输出的完整落盘：`~/.gopi/sessions/01a1471227bf7f3f82ee81778c6ac5f8/artifacts/measure-m*.txt`、`measure-d*.txt`。

| 候选 | 实测结论 | 决定性证据 |
|---|---|---|
| **A. cgroup-scope 钉住进程树** | **否** —— cgroup 不约束信号投递 | M2b：`systemd-run --user --scope` 里发的 `kill -TERM <scope 外的 pid>` 成功杀死外部进程（`inner-cgroup=…/probe-scope2-2928973.scope`，外层进程随后消失）。cgroup 是**资源**边界，不是**信号**边界 |
| **B. pid namespace 钉住进程树** | **技术上可行，代价不可接受** | M3/M10：`unshare -Ur -p -f` 内 `kill -INT <宿主 pid>` → `No such process`（pid 编号在命名空间内不可寻址，`--mount-proc` 都不是必需）；M5b：同一命名空间内 `systemd-run --user` → `Failed to connect to bus`；M5c：`id -u` 变成 0；M5a：`/proc` 只剩 4 个 pid |
| **C. 测试运行器门控（test-budget.sh 包一层）** | **否** —— 隔离不住，逃逸即失效 | 该脚本是**启动器**，不是投递点：它以 `go test -json ./cmd/agent` 跑包，任何测试进程里的 `syscall.Kill(storedPid, SIGINT)` 都发生在它下游，脚本没有任何拦截手段；而能拦的手段（B）在整包测试上**不可用**：M7 同 scope 下 pidns 内 `internal/shot` 的 `TestReclaimOrphanedUnitsStopsDeadCreatorKeepsLive` 直接 FAIL（`Failed to connect to bus: No data available`，因为它真的需要 `systemctl --user` 去停 scope），M9 整包 `unshare … make test-budget PKGS=./cmd/agent` 跑 5 分钟后仍在进行（B 会让整包语义改变，不是「最小」）|
| **D. 桌面会话判据在投递点拒绝** | **是 —— 最小且正确** | 判据是**进程自身的两个读数**：`/proc/<pid>/comm == "systemd"` 且 argv 含 `--user`（与 `procname` 既有的 `OrphanReparentTargets` 同一定义）。M0：本机 manager = pid 54349，`cgroup = …/user@1000.service/init.scope`，`cmdline = /lib/systemd/systemd --user`。投递点只多读 1–2 个 `/proc` 文件，无新依赖、无常驻物、无总线、**无逃逸开关** |
| **E. 信号掩码（发送方屏蔽 SIGINT）** | **否** —— 概念性错误（仍实测） | D1b：发送方 `pthread_sigmask(SIG_BLOCK, SIGINT)`（`SigBlk=…0002`）后 `kill(victim, SIGINT)` 仍然把 SIGINT **投递**给了受害者。掩码只约束投递给**自己**的信号，与投递给陌生人的信号无关 |

**为什么否掉 B 是对的，而不是偷懒**：pid namespace 会把测试进程从宿主 pid 编号空间里摘出去，这**正是**它有效的原因，也**正是**它的代价——`systemd-run --user`、`systemctl --user`、任何「按宿主 pid 找进程」的 e2e 都失去语义（M7 的红就是现场证据）。把整包 `cmd/agent` 塞进 pidns 会改变被测行为，属于「把测试面改小让结果变绿」，不是隔离。

## 落地点（实现）

`internal/procname/sessionmgr.go`（新）：
- `IsLoginSessionManagerPID(pid)` — `comm == UserManagerComm` **且** argv 含 `UserManagerArg`；读不到就是 false（只做**正向识别**，读不到绝不声称）。
- `RefuseLoginSessionManagerTarget(pid) error` — 命中就返回包了 `ErrSessionManagerTarget` 的错误（**拒绝要报错，不许静默跳过**，Rule 15）。

`internal/testutil/procident.go` — `KillIfIdentical` 在 incarnation 比对**通过之后、`syscall.Kill` 之前**加一道：

```go
if !now.SameIncarnation(id) { return … }            // 已有：拒绝被回收的 pid
if err := procname.RefuseLoginSessionManagerTarget(id.Pid); err != nil { return err }  // 新增：拒绝「此刻是会话管理器」
if err := syscall.Kill(id.Pid, sig); err != nil { … }
```

选这个缝的理由（不是任意一个）：`KillIfIdentical` 是**两个已实测事故点共同的下游**——`cmd/agent` 的 `groundKill`（`groundKillSignal` 直接就是它）与 `internal/testutil` 的孤儿清收（`terminateSwept`）。加上它，任何**经过 incarnation 校验**的投递都同时获得会话管理器拒绝；而未来新写的投递点只要复用这个函数就自动继承。

**明确不覆盖（如实）**：`internal/shot/displayalloc.go` 的 `displayKillIncarnation`（自己的 incarnation 实现，只投递给本 run 起的 X server）、`cmd/agent/child_stop.go:signalChildOnly`、`cmd/agent/cli_worker_control.go:workerSignalPID` 仍是各自独立的投递点。它们不经过 `KillIfIdentical`，因此本行的守卫不覆盖——把每一个都改一遍不是「最小」，而是把同一道判据复制成四份（Rule 13/14 的反面）。

## 最小 repro 测试（红→绿，且自身不终止会话）

`internal/procname/sessionmgr_test.go`：

1. `TestLoginSessionManagerIsIdentifiedByCommAndTheUserFlag` —— 夹具 `/proc` 树上的四条臂：正向（comm+`--user`）、**系统** manager（同 comm 无 flag，必须否定）、argv 里"含有"`--user` 字样但 comm 是 bash（必须否定）、无 `/proc` 条目（必须否定）。
2. `TestRefuseDeliveryToLoginSessionManager_RealManagerAndRealChild` —— 在**真机**上：
   - (1) 对**活的** `systemd --user` 调用守卫 → 必须返回 `ErrSessionManagerTarget`；
   - (2) **不投递地**证明守卫承重：同一 pid 的 starttime 仍与记录吻合 ⇒ 旧的 incarnation 守卫**会**放行，新的管理器守卫**拒绝**；
   - (3) 负向对照 + 真投递：自己 spawn 的 `sleep` 不被拒绝，且同一投递形状**真的**把它 SIGINT 掉（否则 (1) 可能是「守卫什么都不做」）；
   - (4) 测试进程自身不被拒绝；
   - (5) 收尾**再读一次** manager 的 starttime，变了就 FAIL —— 这个测试自己不许成为结束会话的那一个。

**测试的安全性契约**：没有任何一条臂向 manager 投递信号；承重性由 (2) 的「同一活体目标上两个判据给出相反裁决」表达。

## 验收

见本 task 的 report（`$GOPI_TASK_REPORT_PATH`，即 `/home/user/.gopi/sessions/01a14712-27bf-7f3f-82ee-81778c6ac5f8/task-report.md`）的 `## Verification`：两轮真实 `make test-budget PKGS='./cmd/agent'` 的原始 stamp，以及 `journalctl -o short-iso --since <start> | grep 'Received SIGINT'` **两轮都为空**（`grep-exit=1`，同窗口 journal 分别有 818 / 614 行，证明空不是「没读到」）。原始输出同时留在 `/home/user/.gopi/sessions/01a1471227bf7f3f82ee81778c6ac5f8/artifacts/acceptance-desktop-run{,2}.txt`。
