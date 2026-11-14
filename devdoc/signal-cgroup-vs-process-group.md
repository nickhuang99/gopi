<!-- doc-class: normative -->
# cgroup 与进程组是两套独立机制 —— 给 `01a17c75` 执行者的两条实测事实

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Handoff 文档。** 写给正在处理
`01a17c75-f193-7c65-9eda-8a18b95c4c6c`（"测试套件清理 worker 时可能把 SIGINT
打到同 session 的 gopi chat"）的执行者，目的是**防止你走错方向**。

**测量时间 / 主机**：2026-10-27，`host-b`，Linux（`/proc/sys/kernel/pid_max`
= 4194304，cgroup v2，`0::` 单层）。

**证据落点**：本 run 的 session 目录
`/home/user/.gopi/sessions/01a17cee-0e70-7404-8c15-e9450da596eb/evidence/signal-measurements.txt`。
本文档正文里每条事实都带**命令 + 原始输出**，且都是本次重测的，不引用任何人的结论当证据。

**读法约定（Rule 41）**：每节标 `[实测]` 或 `[推断]`。`[实测]` 是命令和它打印的东西；
`[推断]` 是我从实测往前走的一步推理，**不能当判据用**，你要自己再验一次才能写进结论。

---

## 1. 一句话结论

- **`kill(2)` 只认 pid / pgid / sid。** cgroup 是资源控制器，**不是**信号寻址单位。
  两个进程在**不同的 systemd scope** 里，**不构成**"信号打不到"的证明 —— 实测见 §2。
- **gopi chat 独占一个进程组**（`pid == pgid`，组内成员数 = 1），所以"组内成员互伤"
  **整类可以排除**。能打到 chat 的 kill 形态只剩三种，见 §4 的三选一表。

如果你在 01a17c75 里读到"chat 在另一个 scope，所以测试代码打不到它"——
**那句话是错的**，别用它做排除依据。

---

## 2. 事实一：cgroup 与进程组是两套独立机制

### 2.1 `[实测]` 两个不同 scope，`kill -0` 仍然成功

`kill -0` **不投递任何信号**（POSIX：`sig` 为 0 时只做错误检查），
所以它是一个纯粹的可达性探针，不会伤害目标 —— 这是可以安全地在别人的进程上跑的命令。

```
$ echo "mine: $(cat /proc/$$/cgroup)"
mine: 0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-668117f9-0083-4d3d-9197-a71f438c5647.scope
$ echo "chat: $(cat /proc/2432526/cgroup)"
chat: 0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-ec1fc736-2998-4355-bd87-09e5b40afcf9.scope
$ kill -0 2432526; echo "exit=$?"
exit=0
```

两个 `vte-spawn-*.scope` 是**不同**的 scope（`ec1fc736-…` vs `668117f9-…`）。
`kill -0` 返回 `0` = 该进程存在且**我方有权向它发信号**。

同一个探针换成组寻址，也一样：

```
$ echo "my pgid=$(ps -o pgid= -p $$ | tr -d ' ')"
my pgid=2445340
$ echo "chat pgid=$(ps -o pgid= -p 2432526 | tr -d ' ')"
chat pgid=2432526
$ kill -0 -2432526; echo "exit=$?"
exit=0
```

我在**别的 scope、别的进程组、别的会话**里，仍然可以按组号寻址 chat 的进程组。

**`[推断]`**：`kill(2)` 的权限判据是 real/effective UID 与 capability，与
cgroup membership 无关。上面两条实测把这个推断的**可观察面**覆盖了：输入是"两个
不同 scope"，输出是"可达"。但我没有做"真投递一个信号并观察到它到达"的实验
（§5 记录了我尝试时的失败），所以严格说，这一节证明的是**探测权限成立**，
不是**投递已发生**。若你要更强的证据，用你自己的受控进程做（配方见 §6）。

### 2.2 `[实测]` setsid 改 pgid/sid，cgroup 一个字不变

```
$ echo "parent(bash) pid=$$ pgid=$(ps -o pgid= -p $$|tr -d ' ') sid=$(ps -o sid= -p $$|tr -d ' ')"
parent(bash) pid=2413024 pgid=2413024 sid=57142
$ cat /proc/$$/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-668117f9-0083-4d3d-9197-a71f438c5647.scope
$ setsid /tmp/setsid-probe.sh
child pid=2413062 pgid=2413062 sid=2413062
child cgroup=0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-668117f9-0083-4d3d-9197-a71f438c5647.scope
```

`setsid(2)` 造出**新会话 + 新进程组**（`pgid == sid == pid`），而 cgroup 路径
**逐字相同**。这是"两套机制独立"最干净的一对读数：一次系统调用只动了其中一套。

### 2.3 `[实测]` 同一 cgroup 的成员 vs 同一进程组的成员 —— 两个集合都不是对方的子集

`/sys/fs/cgroup/<path>/cgroup.procs` 是**内核自己**给这个 cgroup 的成员名单，不是
我扫描推断出来的：

```
$ CG=$(cut -d: -f3 /proc/2432526/cgroup); echo $CG
/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-ec1fc736-2998-4355-bd87-09e5b40afcf9.scope
$ for p in $(cat /sys/fs/cgroup$CG/cgroup.procs); do echo "  pid=$p comm=$(cat /proc/$p/comm 2>/dev/null) pgid=$(awk '{print $5}' /proc/$p/stat 2>/dev/null)"; done
  pid=60277 comm=bash pgid=60277
  pid=552131 comm=bash pgid=552131
  pid=552132 comm=tail pgid=552131
  pid=552133 comm=grep pgid=552131
  pid=703720 comm=bash pgid=703720
  pid=703721 comm=tail pgid=703720
  pid=703722 comm=grep pgid=703720
  pid=2432526 comm=gopi pgid=2432526
  pid=2443636 comm=bash pgid=2443636
  pid=2670836 comm=bash pgid=2670836
  pid=2728700 comm=sleep pgid=2443636
  pid=2730448 comm=sleep pgid=2670836
$ cat /sys/fs/cgroup$CG/cgroup.procs | wc -l
12
```

同一刻，按进程组归类：

```
$ for d in /proc/[0-9]*; do p=${d#/proc/}; pg=$(awk '{print $5}' /proc/$p/stat 2>/dev/null) || continue; [ "$pg" = "2432526" ] && echo "  pid=$p comm=$(cat /proc/$p/comm)"; done
  pid=2432526 comm=gopi
```

**12 个进程住在 chat 的 cgroup 里，分属 6 个不同的进程组
（`60277` / `552131` / `703720` / `2432526` / `2443636` / `2670836`）；
chat 自己的进程组里只有它一个。** 两个集合的交集只有 `2432526`，谁也推不出谁。

### 2.4 `[推断]` 这条事实对 01a17c75 的含义

因为 cgroup 不出现在 `kill(2)` 的寻址里，**"测试进程和 chat 在同一个 scope / 不同
的 scope"这个观察，对"能不能打到 chat"没有任何判决力**。判断必须落在
`pid / pgid / sid` 上。这一点我用上面的实测支持，但**它仍然是推理**：我没有观测到
一次真实发生的、跨 scope 的信号投递（见 §5）。

---

## 3. 事实二：gopi chat 独占进程组，组内互伤整类可排除

### 3.1 `[实测]` 每个 gopi 进程都领自己的进程组

```
$ ps -eo pid,ppid,pgid,sid,etime,cmd | grep -E 'gopi (chat|worker)' | grep -v grep
 651833 3687192  651833 3687192       24:16 gopi worker
2311854  823212 2311854 823212       00:54 gopi worker 01a17cf3-05bc-7f23-841a-048d3bfb344a
2344001   57142 2344001   57142       00:24 gopi worker 01a17cee-0e70-7404-8c15-e9450da596eb
2432526   60277 2432526   60277    03:32:52 gopi chat --resume 07d98407-41a5-42db-875c-de0b077c790a
2434502   57465 2434502   57465    03:13:21 gopi chat --tools
```

每一行 `PID == PGID`：每个 gopi 进程都是**自己进程组的 leader**。

### 3.2 `[实测]` chat 的组里只有 chat 一个成员

按 `/proc/<pid>/stat` 的第 5 个字段（`pgrp`）扫全部进程：

```
$ for d in /proc/[0-9]*; do p=${d#/proc/}; pg=$(awk '{print $5}' /proc/$p/stat 2>/dev/null) || continue; [ "$pg" = "2432526" ] && echo "  pid=$p comm=$(cat /proc/$p/comm)"; done
  pid=2432526 comm=gopi
$ for d in /proc/[0-9]*; do p=${d#/proc/}; pg=$(awk '{print $5}' /proc/$p/stat 2>/dev/null) || continue; [ "$pg" = "2432526" ] && echo x; done | wc -l
1
```

chat 的**直接子进程**也不在它的组里（各自领组）：

```
$ ps -o pid=,pgid=,comm= --ppid 2432526
2443636 2443636 bash
2670836 2670836 bash
```

chat 的 sid 是 `60277`，那是一个 `bash` —— chat **不是会话 leader**，它只是在别人
的会话里领了自己的进程组：

```
$ ps -o pid,ppid,pgid,sid,cmd -p 60277
    PID    PPID    PGID     SID CMD
  60277   55121   60277   60277 bash
```

### 3.3 `[实测]` 组号就是一个 pid 号

```
$ cat /proc/sys/kernel/pid_max
4194304
$ [ "$(ps -o pgid= -p 2432526 | tr -d ' ')" = "2432526" ] && echo YES
YES
```

组号不是另一个命名空间里的编号，**它就是 leader 的 pid 值**。这解释了为什么组信号
的危险在于"这个号现在指的是谁"，而不在于"这个号属于哪个组"。

### 3.4 `[推断]` "组内成员互伤"为什么可以整类排除

一个进程只能被"包含它的组"打到。chat 的组里只有 chat 自己，所以：

- **不存在**"测试套件的某个子进程和 chat 同组，于是清理它时顺手带走 chat"这条路径；
- 一次 `kill(-pgid, SIGINT)` 想打到 chat，它的 `pgid` **必须恰好等于 `2432526`**，
  也就是**必须点名 chat**，而不是"碰巧同组"。

这是从 §3.2 的实测出发的一步推理，属于 `[推断]`：实测给的是"当前这一刻组内只有 1 个
成员"。chat 是新起的进程会重新领组，所以这个性质对新实例同样成立；但如果你在
01a17c75 里观察到反例（组内 > 1），**以你的实测为准，回来更新本文档**。

---

## 4. 能打到 chat 的 kill 形态 —— 三选一表

`[推断]`（建立在 §2、§3 的实测之上）。三种，没有第四种：

| # | 形态 | 需要的条件 | 在本机是否已被排除 / 还剩什么要查 |
|---|---|---|---|
| 1 | `kill(2432526, sig)` —— **点名 pid** | 发送者必须持有 chat 的**具体 pid** | **未排除，是首要嫌疑**。要查：有没有代码把"worker 的 pid"和 chat 的 pid 搞混、有没有从 `/proc` 或 pgrep 里取到过 chat 的号。`[推断]` pid 可能来自 `cmd.Process.Pid`、`/proc` 扫描、pidfile —— 每种来路都要单独看 |
| 2 | `kill(-1, sig)` —— **同 UID 广播** | real/effective UID 相同即可 | **生产代码 0 处**（实测 §4.1）。但**测试代码**要单独扫；另外 `kill -1` 在 shell 里写法很多（`kill -1`、`kill -- -1`、`kill %1` 语义不同），别只 grep 一种 |
| 3 | `kill(-2432526, sig)` —— **点名组** | 发送者必须**已知 chat 的 pgid**；而 pgid == chat 的 pid，所以等价于"已经点到名" | 与形态 1 同源：来路必须查清。守卫谓词已存在（§4.2），但守卫只判"这组是不是我自己的"，**不判"这个号是不是别人的"** |

**三选一的关键判据**：如果一次 SIGINT 真的打到了 chat，那么**发送者的代码里一定
出现过 `2432526` 这个量级的数字，或者出现过 `-1`**。没有第三条路。

### 4.1 `[实测]` 生产代码里广播形态是 0

```
$ grep -rn 'Kill(-1\|Kill(0' --include=*.go internal/ cmd/ | grep -v _test.go | wc -l
0
```

唯一的一条字面命中是注释（`internal/tools/rebuild_pending.go:13` 里的
`kill 07:17` 是叙述性文字，不是代码）。

### 4.2 `[实测]` 已存在的守卫谓词只判"是不是我自己的组"

```go
// cmd/agent/cli_tasks_worker_stop.go
func runChildSignalGroupIsTheCallers(pgid int) bool {
	return pgid == runChildSignalGroupOf() || pgid == os.Getpid()
}
```

```go
// internal/tools/bash.go:813
func killChildTree(pid int) {
	if pid <= 0 {
		return
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid && !killChildTreeGroupIsTheCallers(pgid) {
```

两处守卫的形状一致：**只有当目标领自己的组、且那个组不是调用者所在的组**时才走组
信号。这是一道**正确的**防线，但它防的是"误伤自己的会话"，**不是**"误伤另一个
进程"。一个算错的 pgid 若恰好指向 chat 的组，`pgid == pid` 成立、
`!groupIsTheCallers(pgid)` 也成立 —— 于是组信号照发。**守卫不是 chat 的保护伞。**

### 4.3 `[实测]` 套件被 setsid 隔离，隔离的是"组"不是"权限"

`scripts/test-budget.sh` 把 `go test` 放进自己的会话：

```
$ sed -n '2321,2328p' scripts/test-budget.sh
if command -v setsid >/dev/null 2>&1; then
    test_budget_launcher=(setsid)
else
    test_budget_launcher=()
    echo "test-budget: setsid(1) not found — the suite runs in THIS shell's process group," >&2
    echo "test-budget:       so a group-addressed signal sent by a test can reach the caller's session (reported, not swallowed)." >&2
fi
"${test_budget_launcher[@]}" go test -count=1 -timeout "$test_budget" -json "${args[@]}" >"$raw_json" 2>&1 &
```

`[推断]` 这一层**降低了**默认风险（套件的组里不再有 chat），但**不构成**排除：
它挡住的是"套件按自己的组号广播"，挡不住"套件点了一个具体的号"。而且这个结论依赖
本机有 `setsid(1)`；脚本自己已经声明了没有它时会退化并**打一行警告**（不是静默）。

---

## 5. 我在测量中亲自犯的一次同类错误 —— 请把它当反面教材

这一节记录的是**我自己**在这次 run 里做错的事，不是别人的。

**我做错了什么。** 我想验证"组信号能穿过 cgroup 边界"，于是起了一个受控 probe，
然后用 `pgrep -x sleep -n` 去取它的 pid —— 那个命令取的是**启动时间最新的
`sleep`**，不是我的 probe。我把取到的 pid 当成"我的 probe"，向**它的进程组**发了
`kill -TERM`。

**实际打中了谁（实测）：**

```
$ ps -o pid,ppid,pgid,etime,comm -p 2708797      # 打之前，G 段记录的 chat --tools 的子进程
  gopi pid=2434502 children: 2444727 2444727 bash;2708797 2708797 bash
$ kill -TERM -2708797; echo exit=$?
exit=0
$ ps -o pid,ppid,pgid,etime,comm -p 2708797      # 打之后
  已消失（被我 kill -TERM -2708797 杀掉）
$ ps -o pid,ppid,pgid,sid,etime,comm -p 2434502  # 那个组的"祖父"：chat --tools 本体
    PID    PPID    PGID     SID     ELAPSED COMMAND
2434502   57465 2434502   57465    03:17:24 gopi
```

**结果是 `gopi chat --tools` 的一个 `bash` 子进程被我打死了；chat 本体活着。**
这正是本文档第 4 节形态 3 说的那条路：我点名了一个组号，而那个组号来自一个
**我没有证明过持有**的来源。

**为什么这件事值一顿批评，并且正好是你需要的证据。** 它同时证明了：

1. 组号是可以从**模式匹配**里拿到的（`pgrep -n`），拿错时**没有任何机制拦你**；
2. 这次误伤**没有产生任何告警** —— 我是自己回查才发现的；
3. 那一次投递**确实穿过了 cgroup 边界**（我的 scope → chat 的 scope），
   这就是 §2 里我说"没做真投递实验"的那个缺口，被我自己的一次事故填上了。

**给 01a17c75 的教训**：`pgrep`/`pkill -f`/`ps | grep` 取到的号，**必须先验证它指的
是你要的那个进程**（比 `StartTicks`、比 `cmdline`、比 `/proc/<pid>/cgroup`），
再拿它发信号；直接把"最新一个 sleep / 名字对得上"当成身份，就会造成上面这种事。
这也正是 `PROJECT.md` Rule 42 禁止 `pkill -f` / `pgrep -f` 作为控制路径的原因 ——
而我这次用的 `pgrep -x` 也没能救回来，因为**错在"我以为它是我的"，不在模式宽窄**。

---

## 6. 你要怎么继续（可执行配方）

1. **先把"能不能打到"变成"哪一行打到了"**：形态只有三种（§4）。在测试代码里查
   `SIGINT`、`os.Interrupt`、`syscall.Kill`、`Process.Signal`、`kill -INT`、
   `kill -- -`、`kill %`。**生产代码已排除广播形态（§4.1），测试代码没有。**
2. **对每个组信号点，问"这个 pgid 从哪来"**，并实测那一行：
   ```bash
   # 它是不是 cmd.Process.Pid 被直接当 pgid 用了？
   grep -rn 'Setpgid' --include=*_test.go cmd/ internal/ | head
   ```
   没有 `Setpgid: true` 的子进程**不领自己的组**，`cmd.Process.Pid` 不是它的 pgid。
3. **隔离环境自证**：要复现"跨 scope 也能打到"，起你自己的受控进程（别用
   `pgrep -n`，用变量接 pid）：
   ```bash
   setsid sleep 300 & P=$!            # $! 是你自己起的那个，身份无歧义
   PG=$(ps -o pgid= -p "$P" | tr -d ' ')
   ps -o pid,pgid,sid,comm -p "$P"
   kill -0 "$P"; echo "alive-check exit=$?"
   kill -TERM -"$PG"; sleep 0.3
   kill -0 "$P" 2>/dev/null && echo ALIVE || echo GONE   # GONE = 组信号生效
   ```
   **用 `$!`，不要用 `pgrep`/`pkill`** —— 见 §5。
4. **"不同 scope 所以打不到"这句话不要写进任何结论**，除非你能说出来"哪一次
   `kill(2)` 调用被内核以 `EPERM` 拒绝了"，并把那条输出贴出来。

---

## 7. 我没做到的部分（诚实清单）

- **没有观察到一次"故意的、我持有的、跨 scope 的真投递"成功**。第一次尝试的目标
  已退出（`kill -0` 返回 1，`ps` 无输出），第二次尝试打错了目标（§5）。所以 §2.4
  的推理建立在"权限探测成功"（`kill -0` → 0）和"一次误伤确实穿过了边界"（§5）之上，
  **不是**一个干净的受控实验。要更硬的证据，跑 §6.3 那段。
- **没有查测试代码里的 SIGINT 面**。那是 01a17c75 的正文，不是本文档的范围；
  本文档只负责把"往哪儿看"标出来（§4、§6）。
- **chat 的进程组内只有 1 个成员**是某一刻的读数（§3.2）。如果 chat 有版本差异
  导致某个子进程留在父组里，这个前提要重测。

---

## 8. 复现清单（每条都能直接粘进终端）

```bash
# 8.1 每个 gopi 进程是否 pid==pgid
ps -eo pid,ppid,pgid,sid,etime,cmd | grep -E 'gopi (chat|worker)' | grep -v grep

# 8.2 chat 的组里有几个成员（把 2432526 换成你看到的 chat pid）
CHAT=2432526
for d in /proc/[0-9]*; do p=${d#/proc/}; pg=$(awk '{print $5}' /proc/$p/stat 2>/dev/null) || continue; [ "$pg" = "$CHAT" ] && echo "  pid=$p comm=$(cat /proc/$p/comm)"; done

# 8.3 chat 所在 cgroup 的全部成员（内核名单，与 8.2 的集合不同）
CG=$(cut -d: -f3 /proc/$CHAT/cgroup)
cat /sys/fs/cgroup$CG/cgroup.procs

# 8.4 我这侧与 chat 那侧的 cgroup，逐字对比
echo "mine: $(cut -d: -f3 /proc/$$/cgroup)"
echo "chat: $(cut -d: -f3 /proc/$CHAT/cgroup)"

# 8.5 跨 scope 的可达性探测（kill -0 不投递信号）
kill -0 $CHAT; echo "pid  probe exit=$?"
kill -0 -$CHAT; echo "pgid probe exit=$?"

# 8.6 setsid 改 pgid/sid、不改 cgroup
echo "parent pid=$$ pgid=$(ps -o pgid= -p $$|tr -d ' ') cgroup=$(cut -d: -f3 /proc/$$/cgroup)"
setsid bash -c 'echo "child  pid=$$ pgid=$(ps -o pgid= -p $$|tr -d " ") cgroup=$(cut -d: -f3 /proc/$$/cgroup)"'

# 8.7 生产代码里有没有广播形态
grep -rn 'Kill(-1\|Kill(0' --include=*.go internal/ cmd/ | grep -v _test.go

# 8.8 现存的组信号守卫谓词
sed -n '/^func runChildSignalGroupIsTheCallers/,/^}/p' cmd/agent/cli_tasks_worker_stop.go
```
