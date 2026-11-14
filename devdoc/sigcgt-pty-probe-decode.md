<!-- doc-class: historical -->
# SigCgt / strace 读数矛盾的判定：`rt_sigaction(…, NULL, …)` 是查询，不是安装

**结论先行**：`/proc/<pid>/status` 的 `SigCgt` 读数没有误读；`strace` 那一侧的**归因**错了。
一个在 tty 上启动的 `gopi`，`SigCgt` bit1(SIGINT) 在 exec 后 ~1ms 是 0、在 **t+5.008s** 才变 1，
这是**真实事实**；而 strace 里 `exec+3.4ms` 那行 `rt_sigaction(SIGINT, NULL, {sa_handler=SIG_IGN, …})`
**不是一次安装**——它是"现在的处置是什么"的**查询**，花括号里是**旧值**。把旧值当安装读，
就得到"handler 早已装好、25µs 后被改成 SIG_IGN"这个不存在的故事。

## 1. 两种启动条件下 SigCgt 的实测曲线（同一二进制、同一 pty）

二进制：`~/artifacts/bin/gopi` (`v0.1.0`, go1.24.0)，rig：`~/artifacts/rig/ptyarm.py`
（`pty.fork()` 给子进程真控制终端，同进程同一 `CLOCK_REALTIME` 采 `/proc/<pid>/status`）。

| 臂 | 启动处置 | SigCgt bit1=0 | SigCgt bit1=1 | 触发条件 |
|---|---|---|---|---|
| PTY_D | 继承 **SIG_IGN** | t≈1.0ms | **t=5.0079s** | pty + IGN |
| PTY_F | 继承 SIG_IGN（pty 端回答查询） | t≈0.9ms | **t=5.0109s** | pty + IGN |
| PTY_G | **SIG_DFL** | 从未（0.3ms 起一直是 1） | t≈1.6ms | pty + DFL |
| TERMno / TERMx（管道，非 pty） | SIG_IGN | — | **t=4.6ms** | 无 pty |

四行合起来把边界钉死：**~5s 必须同时满足「真 pty」与「继承 SIG_IGN」**。
`TERM` 变量本身无效（TERMno/TERMx 两臂都是 4.6ms），管道启动时根本没有这个延迟。

## 2. 那 5 秒是一次 `pselect6`，源头是 termenv 的 OSC 查询超时

`strace -f -tt -e trace=signal,execve,pselect6,ppoll,poll,nanosleep`，gopi 主线程：

```
11:11:11.080501  rt_sigaction(SIGINT, NULL, {sa_handler=SIG_DFL, ...})      = 0   ← 查询（DFL 臂）
11:11:11.080519  rt_sigaction(SIGINT, {sa_handler=0x479460, sa_mask=~[], SA_SIGINFO|SA_ONSTACK|SA_RESTART}) = 0  ← 安装
11:11:11.085538  pselect6(2, [1], NULL, NULL, {tv_sec=5, tv_nsec=0}, NULL  <unfinished ...>
11:11:16.090905  <... pselect6 resumed>) = 0 (Timeout)                       ← 5.005s 后
```

`{tv_sec=5}` 的来源是**外部依赖的常量**，不是我们的代码：
`github.com/muesli/termenv@v0.16.0/termenv_unix.go:18` `OSCTimeout = 5 * time.Second`，
经 `waitForData` 的 `unix.Select` 落地（同文件 `:124-131`）。pty 上启动时 gopi 向终端问
`OSC 11 ; ?`（背景色）等能力，**终端不回答就一直等到 5s 超时**。

## 3. 为什么这 5 秒只在 IGN 下推迟掩码：Go runtime 尊重继承的 SIG_IGN

`/usr/local/go/src/runtime/signal_unix.go:154-163`：

```go
func sigInstallGoHandler(sig uint32) bool {
	switch sig {
	case _SIGHUP, _SIGINT:
		if atomic.Loaduintptr(&fwdSig[sig]) == _SIG_IGN {
			return false      // ← 继承来的 SIG_IGN：不装，只记下
		}
	}
	...
```

两条臂的 strace 差别正是这段代码的直接后果（**同一份 trace 判据，不是推断**）：

| 臂 | init 阶段对 SIGINT 的 syscall | 真正的安装时刻 |
|---|---|---|
| DFL (PTY_I, pid 746289) | 11.080501 查询(DFL) → 11.080519 **安装** | exec+0.24ms |
| IGN (PTY_E, pid 745763) | 38.681675 **只有查询**(返回 IGN)，无安装 | **43.696658（tid 745765）**，即 pselect6 超时后 5.2ms |

IGN 臂里 gopi **从来没有**在 init 装 SIGINT handler——`SigCgt` bit1 为 0 与 strace 完全一致。
它到 t≈5.008s 才装，是因为应用自己走 `signal.Notify`（`cmd/agent/cli_chat.go:2176` 等）
才会 `enableSignal` 把它装上；而那条代码路径排在终端能力探测**之后**。

**`pselect6` 本身与处置无关**：DFL 臂上同样有 11.085538→11.090905 的 5s 超时。
它只是"没人应答的终端查询"的代价；只有在 IGN 臂里，它才**顺带**推迟了掩码，
因为掩码在那条路径之后。

## 4. 判据（避免下一个人再用错证据）

读这两类数值时，规则只有三条：

1. **`rt_sigaction(SIG, NULL, {…}, 8)` 是查询，永远不是安装。** 花括号是**旧处置**。
   只有第二参数是**函数指针**（如 `{sa_handler=0x479460, sa_mask=~[], sa_flags=…SA_SIGINFO}`）
   的那一行才是安装。本次误读就是把这个旧值当成了"安装=ign/立即被改 IGN"。
2. **`SigCgt`/`SigIgn` 是位掩码，bit N-1 = 信号 N。** SIGINT(2) → bit1，SIGQUIT(3) → bit2。
   读法：`(int(mask,16) >> (signo-1)) & 1`。`SigCgt` 的位 = "已安装处理器"，
   `SigIgn` 的位 = "处置是 SIG_IGN"，两者对同一信号**可以同时为 0**（DFL 且未装）。
3. **两个读数只有落在同一 pid 的同一时刻才可比。** 一条 `-f` trace 里的 tid 分属多个进程/线程：
   本 run 里 IGN 臂的安装行 tid=745765 与主线程 745763 不同，若按"文件名里的第一个 pid"归类就会算错时间；
   同理 `execve` 行之后的 tid 才是 gopi 的 pid（strace 自身保持原 pid 并 fork 出被测进程）。

**推论**：`SigCgt` 迟到 ≠ "掩码写晚了"。先看**是否有人在 init 装了**（IGN 继承下没人装）,
再看**装它的那次调用在时间线上排在哪里**（这里排在 5s 探测之后）。

## 5. 可复跑

```bash
P=~/.gopi/sessions/01a144d6-c956-702f-a691-0025e7e7ef5b      # 本 run 的证据目录
# IGN 臂（pty + 继承 SIG_IGN）：SigCgt 在 t≈5.008s 翻转
python3 $P/artifacts/rig/ptyarm.py --bin $P/artifacts/bin/gopi \
  --outdir /tmp/ptyd --victim-home /tmp/ptyd/home --hold-ms 8000 --disposition IGN
# DFL 臂：t≈0.002s 已翻转
python3 $P/artifacts/rig/ptyarm.py --bin $P/artifacts/bin/gopi \
  --outdir /tmp/ptyd2 --victim-home /tmp/ptyd2/home --hold-ms 8000 --disposition DFL
```

`masks.tsv` 每行 `pid, arm, t_rel_s, t_abs, SigBlk, SigIgn, SigCgt`；
`meta.json` 记录被采样的 pid 与（`--answer` 时）responder 实际写回的字节。

## 6. 反向（未做）

`--answer` 臂本应让 pty 回答 `OSC 11`/`DSR 6`/`DA1` 从而让 `pselect6` 立刻返回，
但本次 responder 线程没有读到子进程输出（`responder_sent` 为空），所以**没有**得到
"回答了查询 → 5s 消失"的正面证据。判断"5s = 未应答的终端查询"依据的是**超时值来源**
（termenv `OSCTimeout`）+ **DFL/IGN 两臂该调用都存在**，不是靠这一臂。该臂列为未完成。
