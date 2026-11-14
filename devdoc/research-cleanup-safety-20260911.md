<!-- doc-class: historical -->
# 调研报告：cgroup/scope 资源限制 + 清理器安全设计（决策就绪）

**日期**：2026-09-11 ｜ **分支**：`research/cleanup-safety-20260911`（worktree `~/workspace/gopi-cleanup-safety`）
**性质**：**只研究，不实现** —— 本单不改任何产品代码，交付物仅本文件。
**环境**：peerbox，systemd 249 (249.11-0ubuntu3.16)，unified cgroup v2，Ubuntu 22.04 (jammy)
**上游三单**：`devdoc/peerbox-resource-exhaustion-20260911.md`（事故）、`devdoc/cgroup-limiting-not-cleanup-20260911.md`（主路线已实测）、`devdoc/GOLDEN-RULES.md` §4/§5（判据）

## 术语与标注约定（每节都遵守）

| 标注 | 含义 |
|---|---|
| **[成熟实践]** | 有权威来源（man page / 官方文档 / 上游源码），且能给出可核验标识。来源编号见 §8。 |
| **[本机实测]** | 本单在本机跑出来的结论（命令 + 退出码 + 输出原文见正文）。 |
| **[推测]** | 我的判断/推断，**没有**权威来源背书。可以用于决策参考，但不得当成业界惯例。 |
| **[未知]** | 查不到、也没实测 —— 需要额外验证才能下结论。 |

> **纪律声明**：凡是 man page / 官方文档给出的语义，我不改写、不"意译"；凡是我自己的推理，一律打 **[推测]**。
> 本单**不做**破坏性试验：不删目录、不跑长时间压测、不杀进程；需要验证命令行为时用无害最小样例，且只在
> 隔离 worktree 的临时 probe 目录（`/home` 即 /dev/sda4）内操作，验证后已删除（§7 有 `ls` 证据）。

---

## 〇、结论摘要（先看这一页）

1. **推荐机制**：**事前限制(scope) + 创建者自清(命名空间 GC) + 沙箱降权**三层，**不引入**任何"全局扫描杀/删"的
   判官。三层各自失效都退化为"没有保护"，而不会误伤（§5.1）。
2. **被 cgroup 取代的方向**：**"自研 watchdog 全局扫描杀孤儿进程"整条方向被取代**（§6.3）。cgroup 的
   `KillMode=control-group` / `cgroup.kill` 用"整组回收"来回答问题，**不需要判断"谁是孤儿"**。
   未被取代的：**产物（目录/inode）清理** —— cgroup **完全不管文件数量**（§1.6 / §5.4），
   那仍然必须靠"创建者自清 + 可查证据"，且**靠成熟工具而不是自研扫描器**。
3. **nick 两问的一句话答案**：**watchdog 应该是一个"寄生在已有常驻进程 tick 上的 service，而不是 cron"**
   （cron 的 cgroup 会带走它启动的子进程 —— 本机有前科，§6.1）；**会误伤**，但只在"按名字/PPID 判断"时；
   只要把判断换成**命名空间 + 存活占用 + 年龄**三重条件，"误伤"的后果上界是"多留一晚垃圾"（§6.2）。
4. **不自研清单**（§5.2）：清理器、seccomp 策略、沙箱、进程回收器、新的常驻守护 —— 均有成熟件。
5. **额度定法**（§5.5）：**实测 baseline 的 1/8 ~ 1/10**，且必须写成**声明式常量 + 可复算的实测方法**；
   本机 `dd conv=fsync` baseline 75MB/s → `IOWriteBandwidthMax=8M`（≈1/9.4）已被主路线实测验证。
6. **最硬的一条新证据**（§4.3）：**Debian/Ubuntu 的 tmpfiles 配置把 `/tmp` 清理关掉了**，所以
   **13,044 个目录不是"清理器失效"，而是"根本没人负责清"**。这不是推测，是 `D /tmp 1777 root root -`
   里那个 `-`（age 字段）的直接后果。

---

## 一、cgroup v2 资源限制的权威语义、额度定法与盲区

### 1.1 IOWeight vs IOWriteBandwidthMax：一个管"竞争分配"，一个管"绝对上限"

`systemd.resource-control(5)` 原文（本机 man，systemd 249）：

- `IOWeight=weight, StartupIOWeight=weight`
  > "Set the default overall block I/O weight for the executed processes … Takes a single weight value
  > (between 1 and 10000) … This controls the `io.weight` control group attribute, which defaults to 100.
  > **The available I/O bandwidth is split up among all units within one slice relative to their block I/O
  > weight.** A higher weight means more I/O bandwidth, a lower weight means less."
- `IOReadBandwidthMax=device bytes, IOWriteBandwidthMax=device bytes`
  > "Set the per-device overall block I/O bandwidth maximum limit for the executed processes…
  > **This limit is not work-conserving and the executed processes are not allowed to use more even if the
  > device has idle capacity.** Takes a space-separated pair of a file path and a bandwidth value (in bytes
  > per second) … The file path may be a path to a block device node, **or as any other file in which case
  > the backing block device of the file system of the file is used**."

**[成熟实践] 结论（照抄语义，不加料）**
| 想要的效果 | 用哪个 | 为什么 |
|---|---|---|
| 防"独占设备"（恶意/失控压测打满盘） | `IOWriteBandwidthMax` | not work-conserving：设备空闲也不给超 |
| 防"抢占别人"（互相干扰的多个任务按比例分） | `IOWeight` | 只在**同 slice 内竞争**时按权重分配 |
| 组合 | 两个一起用 | 主路线 `limited-run.sh` 实测形态：`IOWeight=50` + `IOWriteBandwidthMax=/dev/sda3 8M` |

**[本机实测]**（主路线已做，本单复核环境）`IOWriteBandwidthMax=/dev/sda3 8M` 把同一
`dd if=/dev/zero of=... bs=1M count=200 conv=fsync` 从 2.71s（≈75MB/s）压到 26.54s（**7.9MB/s**，命中 8M 上限误差 1.3%）。
**[推测]** 1.3% 的偏差来自计量窗口（cgroup 按 IO 完成计费，dd 的 `conv=fsync` 有同步空隙）——
这是解释，不是文档结论；决策不需要它。

**设备粒度（本机实测）**：`/` 与 `/tmp` = `/dev/sda3`（dev 2051）、`/home` 与 `~/workspace` = `/dev/sda4`（dev 2052）、
`/dev/shm` = tmpfs（dev 28，**没有** IO 控制器可限）。
**[成熟实践 + 推测]** man 允许两种写法：块设备节点 `/dev/sda3`，或**文件系统里的任意文件**（内核自己找 backing device）。
**[推测]** 用 `/home/user/workspace/...` 这种路径写法可以少硬编码设备名（Rule 1b 意义上的"少一处魔法字符串"），
但**我没实测**该写法的解析行为 —— 落地前应各跑一次确认。

### 1.2 MemoryHigh vs MemoryMax：推荐的主控是 High，不是 Max

`systemd.resource-control(5)` 原文：

- `MemoryHigh=bytes`
  > "Specify the **throttling** limit on memory usage … Memory usage **may go above the limit if unavoidable, but
  > the processes are heavily slowed down and memory is taken away aggressively** in such cases.
  > **This is the main mechanism to control memory usage of a unit.**"（对应 `memory.high`）
- `MemoryMax=bytes`
  > "Specify the **absolute** limit … If memory usage cannot be contained under the limit, **out-of-memory killer
  > is invoked inside the unit**. **It is recommended to use `MemoryHigh=` as the main control mechanism and use
  > `MemoryMax=` as the last line of defense.**"（对应 `memory.max`）
- `MemorySwapMax=bytes`：绝对 swap 上限（`memory.swap.max`），**仅在 unified hierarchy 下支持**（本机满足）。

**[成熟实践] 这是上游**明写**的推荐**："High 主控、Max 兜底"。主路线只用 `MemoryMax=64M` 是**兜底位**，
对"愿意让测试慢一点也不要假红"的场景，应补一个 `MemoryHigh`（见 §5.5 额度定法）。

**[本机实测]**（主路线）`MemoryMax=64M MemorySwapMax=0` + 申请 300MB → `Killed`，**exit 137**，机器其余无损。
**副作用 [本机实测]**：容器内 OOM 默认是"杀一个进程"，**不是**"整组一起死" —— systemd 有
`ManagedOOMSwap=`/`ManagedOOMMemoryPressure=` 和 `memory.oom.group`（man 有，本机未开）。**[推测]** 对压测脚本，
"OOM 杀掉组内一个 worker 而宿主 shell 存活" 是**更安全**的失效形态（不会连带杀掉父进程）。

**/dev/shm 也是"内存"**（本机实测 `/dev/shm` = tmpfs 7.8G）：主路线把 scratch 从 `/tmp` 搬到 `/dev/shm` 解决了
"写系统盘"，但**tmpfs 的写入同样计入 `memory.max`**（内存控制器按 page cache/shmem 记账）。
**[推测]** 所以 `MemoryMax` 若定得太紧，会与"把测试产物放 /dev/shm"互相打架；额度必须按"进程 + 产物"一起算。

### 1.3 `--scope` vs `--service`：不是口味问题，是生命周期问题

`systemd-run(1)` 原文：

- service（默认）：
  > "If a command is run as transient **service** unit, it will be started and managed by the service manager like
  > any other service … It will run in a **clean and detached execution environment, with the service manager as its
  > parent process**. In this mode, systemd-run will start the service **asynchronously** in the background and return
  > after the command has begun execution (unless `--no-block` or `--wait` are specified)."
- scope（`--scope`）：
  > "If a command is run as transient **scope** unit, it will be **executed by systemd-run itself as parent process
  > and will thus inherit the execution environment of the caller**. However, the processes of the command are managed
  > by the service manager similar to normal services … Execution in this case is **synchronous, and will return only
  > when the command finishes**."

**[成熟实践] 决策矩阵**
| 需求 | scope | service |
|---|---|---|
| 复用调用者的 cwd/env/TTY（当wrapper用，行为最接近"直接在 shell 里跑") | ✅ | 需要 `--same-dir`/`--pipe`/`--pty` 等自己搬 |
| 调用方**同步等待**并拿到退出码 | ✅（天然） | 需要 `--wait` |
| **调用方死了以后进程不许继续跑** | ❌ **恰恰相反**（见下） | ✅（父是 PID1，调用者死了它照样活） |
| 硬时限由 systemd 保证 | `RuntimeMaxSec=` 只对 service 有效（**scope 不支持**，见下） | ✅ `RuntimeMaxSec=` |

**scope 的致命细节 [本机实测 + 主路线实测]**：scope 里的进程**不由外层 `timeout` 管辖**。
主路线记录：`timeout 10` + `systemd-run --scope` 的 dd 循环**跑满 1742 秒**；本单复核
`timeout 1 setsid /bin/sleep 137713` → `RESULT: survived`（timeout 返回后子进程仍在），
而 `timeout 1 /bin/sleep 10` 能在 ~1s 内结束 —— **`timeout(1)` 只对自己创建的直接子进程发信号，
不追进被 `setsid`（新会话）或被 scope 接管的进程组**。
**[成熟实践] 因此时限必须放在两层里**：scope **内部**第一个命令就是 `timeout N`（主路线写法），
或者改用 **service + `RuntimeMaxSec=`**（`systemd.service(5)` 原文："Configures a maximum time for the service
to run … it is terminated and put into a failure state"，但对 `Type=oneshot` 无效）。

**`cgroup.kill` 与 KillMode [成熟实践]**：`systemd.kill(5)` 的 `KillMode=`
> "If set to `control-group`, **all remaining processes in the control group** of this unit will be killed on unit stop …
> …  not recommended to set KillMode= to `process` or even `none`, as this allows processes to escape the service
> manager's lifecycle and resource management"（本机 `user@1000.service` 实测 `KillMode=mixed`，systemd 默认值）。
主路线实测：`systemctl kill -s SIGKILL run-….scope` **一次干掉整组**，不需要按 PID 猜。
**[成熟实践]** 顺带一条对"防孤儿"的**结构性**结论：service 模式下**父进程是 PID1**，所以"调用者死了子进程变孤儿"
这个问题**根本不存在**；scope 模式下父是调用者，**调用者一死，被托管的进程仍归 cgroup，但需要靠 cgroup 回收**。
→ **要"不产生孤儿"，就用 service；要"像本地命令一样同步跑"，就用 scope + 内部 timeout。**

### 1.4 免 sudo 路径：loginctl linger + `systemd-run --user` —— 本机**不通**，但原因可修

**主路线结论**："`sudo systemd-run --scope` 更简，实测开销 36ms，不启用 linger"。
**本单复核（比主路线更细，结论需要修正一点）**：

| 检查 | 结果 |
|---|---|
| `loginctl show-user nick` → `Linger` | `Linger=no` |
| `/run/user/1000/systemd` | **不存在** |
| `/run/user/1000/bus` | **不存在** |
| `systemctl --user is-system-running` | `Failed to connect to bus: No such file or directory` |
| `systemd-run --user --scope /bin/echo` | `Failed to connect to bus: No such file or directory` |
| 显式 `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus systemd-run --user …` | **同样失败**（因为那个 socket 文件根本不存在） |
| **但** `systemctl is-active user@1000.service` | **`active`**（user manager **进程**在跑，只是没有 session bus socket） |

**[本机实测 + 成熟实践]** 所以"SSH 非 systemd 会话下 `--user` 不可用"的**真正原因是缺 session bus socket**，
不是缺 user manager 进程。`systemd-run(1)` 原文对 `--user` 的说明只是"Talk to the service manager of the
calling user"，它没承诺自动找到 socket。**[推测]** 让 `--user` 可用的最小动作是重建 `/run/user/1000/bus`
（`systemctl start user@1000.service` 重新拉起 / 重新登录一次），**而不是**必须 `enable-linger`；
`loginctl enable-linger nick` 解决的是"**无人登录时**也保持 user manager 存活"，它的代价是新增一个常驻 user systemd。
**[推测]** 对本项目：**不推荐**为此启用 linger（违背"不引入更多常驻"），**推荐**维持主路线的
`sudo systemd-run --scope`（本机免密 sudo 可用 + 36ms 开销），把 `--user` 作为"宿主已配置好用户总线"时的备选。

**[成熟实践]** polkit 侧参考：`/usr/share/polkit-1/actions/org.freedesktop.login1.policy` 的
`org.freedesktop.login1.set-self-linger`（本机实测 `allow_any=yes/allow_inactive=yes/allow_active=yes`）
说明"自 linger"是**允许**的动作 —— 即技术可行，但**可行 ≠ 该做**。

### 1.5 额度定法与校准：[成熟实践] 与 [推测] 必须分开

**[成熟实践]（CI runner 的普遍做法，来自 §8 来源 6/7 的官方立场）**
1. 不可信/不受控的作业**跑在一次性（ephemeral）环境**里，而不是"跑在宿主上再限制它"（GitHub 官方明确
   "self-hosted runners **Don't need to have a clean instance** for every job execution" —— 即**它不保证隔离**，
   隔离必须由部署者自己做）。
2. 资源额度**声明式**写在配置里（job/unit 定义），不写在脚本逻辑里。
3. 额度**按实测 baseline 定**，不按"感觉"定。

**[推测] 对我们最有用的"比例定法"（我的建议，非业界惯例）**
- **先测 baseline**：同一动作在**无限制**下测 3 次，取中位数（本机 `dd bs=1M count=200 conv=fsync` → ~75MB/s）。
- **写带宽取 baseline 的 1/8 ~ 1/10**：本机取 `8M` ≈ 1/9.4。理由（推测）：既能压住"独占"，又不至于让
  "本来就要写 1GB 的正经测试"慢到超时；1/10 是**量级级**的护栏，不是精确调速。
- **内存分两档**：`MemoryHigh` = baseline 峰值 × 1.5（允许慢，不允许假红）；`MemoryMax` = baseline 峰值 × 3
  （兜底，只在真失控时 OOM）。**[推测]**，"High 先疼、Max 才死"能把"假红"概率压到最低。
- **时限双保险**：scope 内 `timeout N` **且**（service 模式）`RuntimeMaxSec=N`。**[本机实测]** 单靠外层 timeout 无效（§1.3）。
- **不许有魔法数（Rule 1/2）**：额度必须进常量/配置，并附"这个值怎么来的"（一次实测命令 + 数字）。

**过度限制的代价（为什么必须校准）[推测，但机理清楚]**
- 限得太紧 → 测试**超时假红** → 人会去把限制关掉 → **保护机制被自己人拆除**（这是保护机制最现实的死法）。
- 假红的**杀伤力大于漏防**：漏防的后果是"某天磁盘满了"（可恢复），假红的后果是"每天有人怀疑 CI 且最终移除限制"（不可逆）。
- **[成熟实践] 对照**：systemd 的默认值本身就很宽松（`Tasks=232`, `MemoryMax=infinity`，本机 `user@1000.service` 实测），
  上游**不**给紧默认值 —— 说明"紧默认"不是成熟做法，**显式声明 + 实测校准**才是。

### 1.6 cgroup 的已知盲区（**必须写进决策，否则会误以为"限制了就安全"**）

| 盲区 | 权威依据 | 后果 |
|---|---|---|
| **不限文件数 / inode / 目录数** | **[成熟实践] 上游限制清单的缺失**：`systemd.resource-control(5)` 通篇有 CPU/内存/IO/PID(`TasksMax`)/设备，**没有任何"文件数量/磁盘空间"控制器**；`TasksMax=` 原文只说 "maximum number of **tasks**"，控制 `pids.max`。 | **本项目的 13,044 目录事故，cgroup 一点都防不住**（§4.3）。这是"数量型爆炸"必须靠配额/自清的根本原因。 |
| **设备粒度**：IO 限制按设备 | man：`IOWriteBandwidthMax=device bytes`，路径/块设备二者之一 | `/`(sda3) 与 `/home`(sda4) 要**分别**配；**tmpfs(/dev/shm) 没有 IO 控制器**可限（只有内存记账） |
| **fork bomb**：`TasksMax` 能挡，但**默认很宽** | 本机实测 `DefaultTasksMax=18858`（`/etc/systemd/system.conf` 里是注释掉的 `#DefaultTasksMax=15%`） | 不显式设 `TasksMax` 时，PID 爆炸的护栏几乎不存在。**[成熟实践]** 压测包装器**必须显式写** `TasksMax=64` 之类的值 |
| **内存：High 只是"慢"，不是"不许"** | man 原文 "may go above the limit if unavoidable" | 只看 High 会出现"机器很慢但没人被杀"的阶段；这就是为什么 Max 必须留作兜底 |
| **磁盘满 ≠ 进程受限**：写满 `/` 会伤**所有人**（包括 sshd 的日志、systemd 的 unit 状态） | **[推测]**（机理：/ 上所有人共享空间） | 所以"绝不写系统盘"是硬约束（§3），不是洁癖 |

**[本机实测：inode 余量其实还很多]** `df -i /` → 9,834,400 inodes 总、**513,541 已用（6%）**。
→ 13,044 目录事故的杀伤是**空间 + 遍历时间**（`du -sh /tmp` 超时 300s），**不是 inode 耗尽**。
**[推测]** 若失控规模再大 10 倍，inode 会先见底；两种失效都要防。

---
## 二、命令黑名单为什么是坏设计 + 成熟替代

### 2.1 黑名单的失效机理（逐条，都是结构性的，不是"没写全"）

1. **改写即绕过 [推测，机理确定]**：`dd` / `/bin/dd` / `\dd` / `env dd` / `python -c 'open(...).write(...)'` /
   `git checkout` 写文件 / `go build` 写文件 —— 黑名单拦的是"名字"，危害来自"效果"。
   **[成熟实践] 佐证**：`fio` 自己在手册里就是这么说的 —— `allow_mounted_write` 的原文是
   "fio will abort jobs that are destructive (e.g. that write) to what appears to be a mounted device…
   **This should help catch creating inadvertently destructive tests**"（**catch**，不是 **prevent**）。
   连专业压测工具都只敢自称"帮你发现"，黑名单式的"我禁止你"先天就低一档。
2. **假阴性永远存在**：新工具（`fio`、`stress-ng`、WebAssembly、容器内脚本）每天新增，黑名单追不上。
3. **假阳性伤害自己人**：把 `dd` 禁掉会顺手禁掉 `dd if=/dev/sda of=backup.img` 这种正经用法；
   最后规则被 `--force` 绕过，护栏归零。
4. **它回答错了问题**：真问题是"**它能造成多大破坏**"，黑名单回答的是"**它叫什么名字**"。
   **[推测]** 这就是 nick 那条"不能为了防危险而引入更危险的东西"的直接应用：黑名单的失效模式是"误伤 + 被绕过"。

### 2.2 成熟替代：把"禁止名字"换成"限制能力"

| 需求 | 成熟件 | 权威语义（照抄） | 本机状态 |
|---|---|---|---|
| **限制系统调用** | systemd `SystemCallFilter=` / seccomp | `systemd.exec(5)`：> "Takes a space-separated list of system call names. If this setting is used, all system calls executed by the unit processes **except for the listed ones will result in immediate process termination with the SIGSYS signal (allow-listing)**. If the first character of the list is `~`, the effect is inverted: only the listed system calls will result in immediate process termination (**deny-listing**). … This feature makes use of the Secure Computing Mode 2 interfaces of the kernel ('seccomp')" **[成熟实践]** | 未实测（需 sudo scope）；**语义清楚：白名单优先，黑名单用 `~` 前缀** |
| **文件系统沙箱** | `bwrap` (bubblewrap) | 上游 README：> "bubblewrap works by creating a **new, completely empty, mount namespace** … you can use commandline options to construct the root filesystem"；> "bubblewrap is **not a complete, ready-made sandbox with a specific security policy** … **the level of protection … is entirely determined by the arguments**" **[成熟实践 + 明确的免责声明]** | **已安装**（0.6.1，deb 包 `bubblewrap`）**[本机实测]** 见下 |
| **（同类备选）** | firejail / nsjail | nsjail README：namespaces(UTS/MOUNT/PID/IPC/NET/USER/CGROUPS/TIME) + `--time_limit`（默认 600s）+ `--rlimit_*` + Kafel seccomp 策略 **[成熟实践]** | **均未安装**（`which firejail` 空；本机只有 `bwrap`） |
| **只读 / 不可写系统目录** | systemd `ProtectSystem=` / `ProtectHome=` / `PrivateTmp=` | `systemd.exec(5)` ProtectSystem `full`/`strict`：把 `/usr`、`/boot`、`/efi`（`strict` 再加 `/etc`）**只读挂载** **[成熟实践]**。**但**：> "some sandboxing functionality is generally **not available in user services** … settings requiring file system namespacing support (such as `ProtectSystem=`) are not available" ← **`--user` 模式下这些不生效**，是个关键限制 | — |
| **降权 + 只读根 + 配额** | 容器（podman/docker）cap-drop / read-only rootfs / storage quota | 通用容器实践：`--cap-drop=ALL`、`--read-only` + `--tmpfs`、`--storage-opt size=` **[成熟实践，本单未逐条取权威链接 → 按"通行做法"计，不作为唯一依据]** | 本机无 docker（未实测） |

**[本机实测] bwrap 是可用且**语义清晰**的护栏（probe 已删，命令与输出如下）**
```
$ bwrap --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp /bin/echo "hello from bwrap"
hello from bwrap            rc=0
$ bwrap --ro-bind / / ... --bind "$PWD" "$PWD" /bin/bash -c 'echo probe > /etc/csafety-probe.txt'
/bin/bash: line 1: /etc/csafety-probe.txt: Read-only file system      ← 系统盘写不进去（rc 是 bash 的 0，不是 bwrap 的）
$ bwrap --ro-bind / / ... --bind "$PWD" "$PWD" /bin/bash -c 'echo probe > ./inside-bwrap.txt'
wrote-inside                ← 只在自己被 bind 进来的目录里可写
$ bwrap ... --die-with-parent --unshare-pid --new-session /bin/echo ok
sandboxed-and-die-with-parent-ok      rc=0
```
**`--die-with-parent`**（`bwrap --help`）："Kills with SIGKILL child process (COMMAND) when bwrap or bwrap's parent dies"
→ 这正是**我们缺的那一环**：调用者一死，沙箱里的东西全死，**不产生孤儿**。

**[成熟实践] bwrap 的注意事项（上游自己列的 Limitations，不能省略）**
- `--new-session`：不配合 seccomp 过滤 `TIOCSTI` 时，**必须**加 `--new-session`，否则可被"往调用者终端注入命令"（CVE-2017-5226 类）。
- **bind 进来的东西都是攻击面**："Everything mounted into the sandbox can potentially be used to escalate privileges.
  For example, if you bind a D-Bus socket into the sandbox, it can be used to execute commands via systemd."
  → **不要把 `$DBUS_SESSION_BUS_ADDRESS` / `/run/user/1000/bus` 绑进沙箱**（对我们要跑的测试，没有理由需要它）。
- 上游明说**它不是一个策略**："the level of protection … entirely determined by the arguments"。
  即：**光装 bwrap 不产生任何安全**，必须有一份**固定的参数清单**（并且这份清单要被 review）。

### 2.3 CI 如何隔离不可信脚本（权威立场，避免我把推测写成惯例）

- **[成熟实践] GitHub Actions 官方（self-hosted runner 文档原文）**：
  > "Self-hosted runners … **Don't need to have a clean instance for every job execution.**"
  → 官方**不承诺**隔离。同一文档把 OS/软件更新、成本、隔离责任全部归给部署者。
- **[成熟实践] GitHub Actions 官方安全参考（secure-use）**：不可信 PR 内容 + `pull_request_target`/`workflow_run`
  → "**This can be exploited to take over a repository**"；建议"pin actions to a **full-length commit SHA**"、
  "**Use an intermediate environment variable**"（防脚本注入）、"**Principle of least privilege**" 给 `GITHUB_TOKEN`。
  → **对我们有用的抽象**：把外来的**值**当数据（env）而不是拼进脚本；把能力收到最小。
- **[本机实测：官方文档里没有"用 ulimit/cgroup 限制脚本"这句话]** 我为此专门抓了 secure-use 全文（12027 chars）与
  self-hosted runner 文档（1961 chars），**两篇都没有 "ulimit"/"cgroup" 字样**。
  → **诚实结论**：不能声称"CI 业界用 cgroup 限 runner"。**[成熟实践] 能声称的是两类**：
  (a) 托管 runner 走**一次性环境**（GitHub-hosted），(b) 自托管则**责任在部署者**（官方原话），
  部署者怎么做**没有统一权威答案**。**[推测]** cgroup 限制是自托管部署者可选的一种手段。
- **[成熟实践] 容器时代对"资源限制"的通行做法**（非权威链接，仅作方向）：容器运行时把 cgroup 当默认底座
  （docker `--memory`/`--cpus`/`--pids-limit` 背后就是 cgroup v2 控制器）—— **这不是新发明，是把内核能力声明化**。

### 2.4 coding agent 的 sandbox / 权限提示实践

**[成熟实践]（可核验来源）**
- **bubblewrap**：给**非特权用户**用的沙箱构造器。上游原文："Many container runtime tools like systemd-nspawn, docker,
  etc. … **are not suitable to give to unprivileged users, because it is trivial to turn such access into a fully
  privileged root shell on the host.** … bubblewrap uses `PR_SET_NO_NEW_PRIVS` to turn off setuid binaries"
  → **设计取向：小、可审计、非 setuid、由调用方给策略**。
- **nsjail**：README 明列 `--time_limit SEC **Wall-time limit in seconds (default: 600)**`、`--rlimit_as/--rlimit_cpu/--rlimit_nofile`、
  `--seccomp_policy`、`--cgroup_mem_max/--cgroup_pids_max/--cgroup_cpu_ms_per_sec`。
  → **"默认就给一个 600s 墙钟上限"** 是它的设计选择 **[成熟实践]**；**[推测]** 这一点值得抄到我们的压测包装器：
  **默认有时限，而不是默认无限**。
- **tini / dumb-init**（进程回收器）：见 §4.6 —— 它们的**边界**（"只 reap 自家子进程"）是"不要当全局判官"的教科书先例。
- **[未知 / 未获证据]** 主流 coding agent（Codex/Claude Code 等）**具体的沙箱实现细节**我没拿到可核验的一手文档：
  `openai/codex` 仓库的 `docs/sandbox.md` 只有一句"see this documentation"跳转，目标页是产品文档页（非实现细节）；
  `anthropics/claude-code` 仓库的 README 只有安装与数据政策。**我不把"它们怎么做的"写成结论** ——
  按本单纪律，这属于 [未知]，需要一手来源才能写。

---
## 三、压测脚本的安全设计（为什么成熟工具"默认就有上限"）

### 3.1 fio：默认不是"无限猛写"，而是"写到 size 为止"

**[成熟实践] 逐条来自 fio 官方文档（rev 3.42，§8 来源 8）**

| 参数 | 原文语义 | 安全含义 |
|---|---|---|
| `runtime=time` | "Limit runtime. The test will run until it completes the configured I/O workload **or until it has run for this specified amount of time, whichever occurs first**" | **时限是"或"关系**：跑够时间就停 |
| `time_based` | "If set, fio will run for the duration of the `runtime` specified **even if the file(s) are completely read or written. It will simply loop over the same workload**" | **循环是显式选择**，不是默认 |
| `size` | "How much data are we going to be reading/writing"（job 基本参数） | **默认有量**：不是"写满盘" |
| `offset` / `number_ios` / `loops` | "Data before the given offset will not be touched. This effectively caps the file size at `real_size - offset`" / "the range/size can be set independently of the number of I/Os to perform … it will exit normally" | **可把写入范围钉死在一个文件的一段** |
| `direct=bool` | "use non-buffered I/O. This is usually `O_DIRECT`"，**默认 false**（`buffered` 默认 true） | 默认走 page cache，**不会**把设备队列打爆到"别人写不动" |
| `fsync=int` | "issue an fsync(2) … **for every number of blocks** given … **Defaults to 0, which means fio does not periodically issue and wait for a sync to complete**" | **`conv=fsync`不存在的默认**：不会每个块都同步落盘 |
| `unlink=bool` | "Unlink (delete) the job files when done. **Not the default**, as repeated runs of that job would then waste time recreating the file set" | 默认**不留**也要注意：默认不删 → 残留是**设计后果**，不是 bug |
| `allow_mounted_write=bool` | "If this isn't set, fio will **abort jobs that are destructive (e.g. that write) to what appears to be a mounted device or partition. This should help catch creating inadvertently destructive tests** … Default: false" | **默认拒绝写挂载中的设备** —— 这正是我们那次事故缺的护栏 |
| `--readonly` | "**Turn on safety read-only checks, preventing writes and trims.** … This safety net can be used as an extra precaution" | 上游把"防误写"做成一等开关 |
| `--aux-path=path` | "Use the directory specified by `path` for generated **state files** instead of the current working directory" | **状态文件可指定位置**（不会偷偷写到 cwd） |
| `--warnings-fatal` | "All fio parser warnings are fatal, causing fio to exit with an error" | 配置错误 → **立刻失败**，而不是带着错配置压测 |

**对照我们的事故脚本**（`devdoc/peerbox-resource-exhaustion-20260911.md`）：
```bash
for j in 1 2 3 4 5 6; do ( while :; do dd if=/dev/zero of=/tmp/load-$j.bin bs=1M count=8 conv=fsync 2>/dev/null || sleep 1; done ) & done
```
逐个对照：**无限循环**（fio 需要显式 `time_based` 才循环）、**每 8MB 强制 fsync**（fio 默认 fsync=0）、
**写 `/tmp` = 写系统盘**（fio 默认拒写挂载设备）、**无时限**（fio 有 `runtime` 语义）、**无数量限制**（fio 有 `size`）、
**6 份并行**（fio 要显式 `numjobs`）。**[成熟实践] 结论：不是"dd 坏"，是"手写的 dd 循环缺了 fio 那 9 条默认护栏"。**

**[推测]** 因此最省力的规范化路径是：**压测用 fio（装它），不要用 dd 循环**；如果坚持 dd，就必须手工补齐
"时限 + 总量 + 目标目录 + 不 fsync 每块 + 单实例"这五样。

### 3.2 stress-ng `--hdd`：默认 1GB / 有界、显式声明（上游源码可核验）

**[成熟实践] `stress-hdd.c` 源码（§8 来源 9，Upstream GitHub raw）**
```c
#define MIN_HDD_BYTES   (1 * STRESS_MB)
#define MAX_HDD_BYTES   (MAX_FILE_LIMIT)
#define DEFAULT_HDD_BYTES (1 * STRESS_GB)      /* ← 默认 1GB，不是"写满盘" */
...
{ "hdd N",         "start N workers spinning on write()/unlink()" },
{ NULL, "hdd-bytes N", "write N bytes per hdd worker (default is 1GB)" },
{ NULL, "hdd-ops N",   "stop after N hdd bogo operations" },
{ NULL, "hdd-sleep N", "sleep N seconds after each read/write cycle (default is off)" },
{ NULL, "hdd-write-size N", "set the default write size to N bytes" },
```
并且它的默认写模式是 **write()/unlink()**（写完就删，如 help 字符串所述），
`--hdd-opts` 里 `fsync`/`fdatasync`/`syncfs`/`direct` 都是**需要显式开启**的选项
（源码里 `HDD_OPT_FSYNC` 等只在用户给出 `fsync` 选项时才置位）。
**[成熟实践] 结论与 fio 一致**：成熟压测工具的默认值都是"**有界 + 不强制同步落盘 + 用完即删**"，
把"无限/同步/持久"留给用户显式声明。**[推测]** stress-ng 还有个对我们更有用的点：`hdd-sleep` 与
`hdd-ops N` 提供"节流"与"限量"两个旋钮 —— 这正是我们那个 `while :` 缺的。

### 3.3 `timeout` + `trap` + `setsid` + `kill -- -PGID`：惯例与**它的边界**

**[成熟实践] 惯例**（`timeout(1)` man / `setsid(1)` man）
- `timeout [OPTION] DURATION COMMAND`："start COMMAND, and kill it if still running after DURATION"；
  `-k, --kill-after=DURATION`："also send a KILL signal if COMMAND is still running this long after the initial signal was sent"；
  `--preserve-status`：让退出码反映 COMMAND 的真实状态。
- `setsid`："run a program in a new session" → 新会话 = 新进程组（PGID = 子进程 PID）。
- **杀整组**：`kill -TERM -- -<PGID>`（负号 = 进程组）。**本机实测**：
  ```
  $ setsid /bin/sleep 4 &
  $ ps -o pid,ppid,pgid,sid,stat,cmd -p $child
     PID    PPID    PGID     SID STAT CMD
  3554014 3553999 3554014 3554014 Ss   /bin/sleep 4        ← PGID == SID == PID：独立会话已建立
  $ kill -TERM -- -3554014      → kill_rc=0，进程消失
  ```
- **trap 惯例**：`trap 'cleanup' EXIT INT TERM` —— 保证"自己起的进程/文件，自己在退出路径上收掉"。

**[本机实测 + 关键边界] `timeout` 不跨越会话边界**
```
$ timeout 1 setsid /bin/sleep 137713 & wait ; sleep 0.3
RESULT: survived -> timeout(1) did NOT reach the setsid child     ← 1s 到点，孩子还活着
$ timeout 1 /bin/sleep 10                                        ← 对照：直接子进程，到点即死
```
**结论（[成熟实践] 语义 + [本机实测] 现象）**：`setsid` 让子进程**脱离**了 timeout 能触及的会话/PGID；
**`setsid` 与 `timeout` 同时用 = 时限失效**。这与主路线记录的 `systemd-run --scope` + `timeout` 失效是**同一类**问题。
**[本机实测：残留进程复核]** 我在 probe 期间也观察到一个"多年前的 `sleep 3600`"仍在（PID 3465253）
—— 正是"**没人管的孤儿**"的活标本；本单**不杀**它（约束禁杀进程），仅在 §6.2 引用为"孤儿存在"的证据。

**[推测 / 建议]** 正确组合（我已按此写进 §5.5）：
1. **别把两种"接管理"混用**：要么让 systemd 管（scope 内不设 setsid，时限放 scope 内部或 service 的 `RuntimeMaxSec=`），
   要么让 shell 管（`setsid` + 自己 `kill -- -PGID` + `trap EXIT`），**不要混**。
2. 需要"调用者一死全死"时，用 **bwrap `--die-with-parent`**（§2.2）或 **systemd service**（父是 PID1），
   不要用"定时扫描之后杀"。

### 3.4 `ulimit -f` 对 `dd` 的**实测**（本单交付要求项；命令 + 退出码 + 报错原文）

**[本机实测]** 全部在隔离 probe 目录（`/dev/sda4`）里跑，probe 已删除（§7）。
宿主默认：`ulimit -f` = `unlimited` / `ulimit -Hf` = `unlimited`。

| # | 命令 | 退出码 | 报错原文 | 落盘结果 |
|---|---|---|---|---|
| A | `bash -c 'ulimit -f 1024; dd if=/dev/zero of=probe.bin bs=1M count=8 conv=fsync'` | **153** | `File size limit exceeded(core dumped)` | **1,048,576 字节**（= 1024 × 512B = **512KiB**）—— **只写完一对 1MiB 记录的第一半** |
| B | 同命令**无** `ulimit`（对照） | **0** | `8+0 records in / 8+0 records out / 8388608 bytes … copied, 0.272147 s, 30.8 MB/s` | 8,388,608 字节 |
| C | `ulimit -f 0; dd … bs=1M count=1` | **153** | `File size limit exceeded(core dumped)` | **0 字节** |
| D | `ulimit -f 1024; dd … count=8`（无 `conv=fsync`） | **153** | 同上 | 1,048,576 字节 |
| E | `ulimit -f 1024; dd … count=8 oflag=direct` | **153** | 同上 | 1,048,576 字节 |
| G | `ulimit -f 1024; for i in 1..10; do dd … bs=1M count=1; done` | **0（全部成功）** | — | **10,485,760 字节（10 × 1MiB）** |

**结论（分点，标注区分）**
1. **[本机实测] 单位是 512 字节块**：`ulimit -f 1024` 在 `bash` 里允许写到 **1,048,576 字节**（=1MiB）。
   **注意**：这**不是** POSIX 的 512 字节说法；这是 **bash/GNU 环境的实际行为**，与 `devdoc/cgroup-limiting-not-cleanup-20260911.md`
   里"1024 blocks"的换算一致。→ **写文档/常量时必须以实测为准，不要照抄"512B"的教条**（若照抄会把额度算错一倍）。
2. **[本机实测] 超限的报错形态是 SIGXFSZ（信号 25）**：退出码 **153 = 128 + 25**；
   shell 原文 `File size limit exceeded(core dumped)`。`coredumpctl` 佐证 5 条 `SIGXFSZ` 记录（/usr/bin/dd）。
   **[成熟实践] 为什么不是 ENOSPC/EFBIG**：SIGXFSZ 是内核在超过 RLIMIT_FSIZE 时**默认发送**的信号，
   进程若忽略它才收到 `EFBIG`。→ **[推测]** 所以 `ulimit -f` 是"**硬杀**"，不是"友好报错"：
   用它必须预期"进程被打死 + 可能 core"，脚本要能识别 153。
3. **[本机实测] 它只限"单个文件"，不限"总量"**：#G 里 10 次各 1MiB 全部成功（单个文件都没超 1MiB）。
   → **这正是 13,044 目录类事故的漏洞形状**：`ulimit -f` 对"很多小文件"**完全无效**。
4. **[本机实测] `ulimit -f 0` 不是"无限制"，是"文件大小上限 0"**：任何写入立即 SIGXFSZ（#C 得 0 字节）。
   → **[推测]** 这是很容易踩的坑（很多人以为 0=off，`ulimit -c 0` 那种才是 off）。
5. **[本机实测] 副作用（重要）**：`coredumpctl` 显示**每次都产生了 core**（19.9K，落在 `/var/lib/systemd/coredump`，
   它挂在 **`/`** 上：`/var/lib/systemd/coredump` → `/dev/sda3`）。
   → **`ulimit -f` 保护了数据盘，却给系统盘留了 core 记录**。若要全无痕：`ulimit -c 0`（本机默认已是 0，
   `ulimit -a` 实测 `core file size (blocks, -c) 0`），但 **RLIMIT_FSIZE 触发时 systemd-coredump 仍会接管核心转储管道**
   （`/proc/sys/kernel/core_pattern` 实测 `|/lib/systemd/systemd-coredump …`）。
   → **[推测] 把它当"防写系统盘"的**主**手段是不合适的**；它的正确定位是"**给产物文件加一个单文件上限**"，
   "不写系统盘"要靠**目录位置**（§4.4）来保证。
6. **[本机实测] 不改变"无限循环"**：`ulimit -f` 与时限/RuntimeMaxSec 是不同的维度 —— 它管"写多大"，
   不管"跑多久"。→ §5.5 的"三重保险"缺一不可（**带宽 + 内存 + 时限**，再加文件大小只是第四层）。

### 3.5 "绝不写系统盘"：可机械检查的设计（[推测] + [成熟实践] 依据）

**[本机实测] 事实**：`/` = `/dev/sda3`（dev 2051），`/tmp` = **同一个 2051** ← 这是"写 /tmp = 写系统盘"的**设备级证据**；
`/home` = `/dev/sda4`（dev 2052）；`/dev/shm` = tmpfs（dev 28）。
**[成熟实践] 可抄的先例**（本项目已有）：`feat/ban-tmp-scratch` 的**设备号硬断言** ——
scratch root 的 `st_dev` 一旦等于 `/` 的 `st_dev` 就**退出 97（致命）**，而不是警告
（`devdoc/tmp-ban-scratch-root.md`；memory #1755）。**[推测]** 这是"用机械检查代替自觉"的正例：
断言不依赖谁记得"别写 /tmp"。
**[推测] 建议补充的两条**（本单不实现）：(a) 压测包装器解析目标路径的 `st_dev`，等于 `/` 的设备号则拒绝启动；
(b) 把 `/dev/shm` 的 `tmpfs` 写入计入内存额度（§1.2）。

---
## 四、清理器的成熟设计（本单的**核心发现**在 §4.3）

### 4.1 `systemd-tmpfiles` 的语义（照抄 man，不用自己的话替换）

`tmpfiles.d(5)`（本机 systemd 249）与本机 `man systemd-tmpfiles`（8）：

| 项 | 原文 | 决策含义 |
|---|---|---|
| `d` | "Create a directory … **Contents of this directory are subject to time-based cleanup if the age argument is specified.**" | **要清内容，必须给 age** |
| `D` | "Similar to `d`, but in addition **the contents of the directory will be removed when `--remove` is used**." | 两种清法：`--clean`(按 age) 或 `--remove`(每次全清) |
| `e` | "…remove their contents based on age … **If the age argument is 0, contents will be unconditionally deleted every time systemd-tmpfiles is run.**" | `0` 是"每次全清"的危险旋钮 |
| `q` | "Create a subvolume or directory the same as `v` … On **non-btrfs** file systems, this line type is **identical to `d`**." | 本机 ext4 上 `q` ≡ `d` |
| **Age** | "If a file or directory is **older than the current time minus the age field, it is deleted** … **If omitted or set to `-`, no automatic clean-up is done.**" | **`-` = 放弃清理**（← 本机 /tmp 就是这个） |
| Age 的判据 | "The age of a file system entry is determined from its **last modification timestamp (mtime), its last access timestamp (atime), and (except for directories) its last status change timestamp (ctime)**. By default, **any of these three (or two) values will prevent cleanup if it is more recent** than the current time minus the age field." | 被"读过/碰过"的条目**永不过期** |
| `--clean` | "all files and directories **with an age parameter configured** will be cleaned up." | 没 age → 这个命令对它无事可做 |
| `--remove` | "the contents of directories marked with `D` or `R` … are removed **unless an exclusive or shared BSD lock is taken on them**." | 可用 flock 豁免 |
| `--dry-run` | "Process the configuration and print what operations would be performed, but do not actually change anything … **added in version 262**." → **本机实测：`systemd-tmpfiles: unrecognized option '--dry-run'`（249）** | **本机没有干跑能力**，所以任何"改配置"的动作都不可先用 dry-run 验证（→ §5.4） |
| `!` 修饰符 | "this line is only safe to execute **during boot** … systemd-tmpfiles will take lines with an exclamation mark only into consideration **if the `--boot` option is given**" | **`!` + age 的规则在每日 `--clean` 里被完全跳过**（本机 5 个 57 天目录的成因之一，§4.3） |
| flock（爆炸半径机制） | "while the aging algorithm is run an **exclusive BSD file lock** is taken on each directory/file the algorithm decides to remove. If the aging algorithm finds a lock (shared or exclusive) is already taken …, **it (and everything below it) is skipped**. Applications may use this to **temporarily exclude certain directory subtrees**" | **应用可主动豁免**自己的目录（`age=0` + 自己持锁 = "活着就别删、死了立刻删"） |

**关于"SAFETY 章节" —— 诚实说明（我会明确纠正题面）**
任务要求"引用 man tmpfiles.d 的 SAFETY 章节原文"。**本机 `man tmpfiles.d`(systemd 249) 没有名为 `SAFETY` 的章节**
（本机 man 的章节标题实测只有：`NAME / SYNOPSIS / DESCRIPTION / CONFIGURATION DIRECTORIES AND PRECEDENCE /
CONFIGURATION FILE FORMAT / SPECIFIERS / EXAMPLES / SEE ALSO / NOTES`；`man systemd-tmpfiles` 同理，无 `SAFETY`）。
我也抓取了**上游 main 分支**的 `man/tmpfiles.d.xml` 与 `man/systemd-tmpfiles.xml`（§8 来源 3/4），
**同样没有 `SAFETY` 标题**。
**[推测] 题面里的"SAFETY 章节"很可能是**别的** systemd 手册页**（如 `systemd.exec(5)` 的沙箱注意事项，
或 `systemd.resource-control(5)` 关于"限制也会影响 systemd 自己"的说明）。**我不伪造该章节**：
本报告引用的是**实际存在的**安全相关原文段落 —— 即上表的 **Age/`-`、flock 跳过、`!` 仅 boot、
非 root 的 atime 陷阱（§4.4）**，并给出可核验标识（man 名 + 章节 + 上游 XML URL）。这就是"成熟实践"能提供的全部依据。

### 4.1.1 排除机制：`x`/`X` 与 `--exclude-prefix` / `-E`

- `x`（原文）："**Ignore a path during cleaning.** Use this type to exclude paths from clean-up as controlled with the
  **Age** parameter … accept shell-style globs … Note that lines of this type **do not influence the effect of `r` or `R` lines**."
- `X`："Unlike `x`, this parameter will **not exclude the content** if path is a directory, but only directory itself."
- CLI：`--exclude-prefix=path`（"Ignore rules with paths that start with the specified prefix"，可多次）；
  `-E` = 快捷方式，等于 `--exclude-prefix=/dev --exclude-prefix=/proc --exclude-prefix=/run --exclude-prefix=/sys`。
- **[成熟实践] 真实世界的用法（本机文件，逐字引用）** `/usr/lib/tmpfiles.d/gvfsd-fuse-tmpfiles.conf`：
  ```
  # tmpfiles.d defaults are set to clean /run/user every now and then
  # which includes our gvfs-fuse mount being mounted in /run/user/<id>/gvfs
  # This file adds an exclusion rule so that user data don't get automatically
  # cleaned up (i.e. destroyed).
  x /run/user/*/gvfs
  ```
  → **这就是"发包方发现自己会被上游规则误删，于是加一条 exclude"的教科书案例**：上游的清理**确实有误伤**，
  而成熟做法是"**声明式豁免**"，不是"把清理器关掉"。

### 4.2 Debian/Ubuntu 的 `/usr/lib/tmpfiles.d/tmp.conf`（实际内容，逐字）

**[本机实测]** `dpkg -S /usr/lib/tmpfiles.d/tmp.conf` → `systemd: /usr/lib/tmpfiles.d/tmp.conf`（**由发行版包提供**）。
文件末尾（卸载注释后的**全部有效行**）：
```
# Clear tmp directories separately, to make them easier to override
D /tmp 1777 root root -
#q /var/tmp 1777 root root 30d
```
对照 **[成熟实践] 上游 systemd main 的同一文件**（§8 来源 5，raw.githubusercontent 抓取）：
```
q /tmp      1777 root root 10d
q /var/tmp  1777 root root 30d
```
**差异**：**上游给 `/tmp` 配了 10 天 age；Debian/Ubuntu 把它改成了 `D` 且 age = `-`（不清理）**，
并把 `/var/tmp` 的 30d 规则整行注释掉。
**[未知]** 我**没有**找到 Debian 官方文档解释这个改动的理由（Debian changelog 未命中；
`salsa.debian.org` 的 changelog 抓取被反爬拦下 → 不重试）。**我不推测成"惯例"**。
**[成熟实践] 能引的是上游文档里记录的"age 式清理的反面案例"**：`tmpfiles.d(5)` 的 EXAMPLES 有一条
> "abrt needs a directory created at boot … and **its content should be preserved from the automatic cleanup applied
> to the contents of /var/tmp**"

—— 即"age 清理会误删长期驻留的目录，上游自己的解法是**加豁免**"。
**[推测]** Debian 选择"根本不按 age 清 /tmp，只在开机时 `--remove` 清空"，是在规避这类误删；代价就是 §4.3 的后果。

### 4.3 ★ 13,044 个目录**为什么**没被清掉 —— exact reason（命令级证据）

**一句话**：**`/tmp` 的清理规则在 Debian/Ubuntu 上被"关掉"了 —— age 字段是 `-`（不清理），
而唯一会清空它的路径是"开机时的 `--remove`"，本机连续开机 57 天从未触发。所以 13,044 个目录
不是"清理器失效"，而是"没有任何生效的规则负责清它们"。**

**证据链（全部为本机今日实测命令与输出）**

| # | 命令 | 输出 | 说明 |
|---|---|---|---|
| 1 | `tail -4 /usr/lib/tmpfiles.d/tmp.conf` | `D /tmp 1777 root root -` | **生效规则**；age = `-` |
| 2 | `systemd-tmpfiles --cat-config \| sed -n '/tmpfiles.d\/tmp.conf/,$p' \| head -12` | 同上（完整配置展开后仍是这一行） | 没有别的文件覆盖它（`/etc/tmpfiles.d/` 只有 `screen-cleanup.conf`） |
| 3 | `ls /etc/tmpfiles.d/ /run/tmpfiles.d/` | `screen-cleanup.conf` / `static-nodes.conf` | **没有本地覆盖** |
| 4 | `systemctl cat systemd-tmpfiles-clean.service \| grep ExecStart` | `ExecStart=systemd-tmpfiles --clean` | 每日只跑 `--clean` → **只处理"配了 age"的规则** |
| 5 | `systemctl cat systemd-tmpfiles-setup.service \| grep ExecStart` | `ExecStart=systemd-tmpfiles --create --remove --boot --exclude-prefix=/dev` | **唯一会清空 `/tmp` 的命令**（`--remove` 作用于 `D` 行） |
| 6 | `systemctl show systemd-tmpfiles-setup.service -p ActiveEnterTimestamp` | `ActiveEnterTimestamp=Wed 2026-07-15 21:34:42 CST` | 它**只在开机时**跑（oneshot） |
| 7 | `uptime` / `who -b` | `up 57 days, 13:01` / `system boot Jul 15 21:33` | **57 天没重启 → 第 5 行的 `--remove` 一次都没再跑** |
| 8 | `journalctl -u systemd-tmpfiles-clean.service \| grep -c 'Finished Cleanup'` | **7**（Sep 04→Sep 10 每晚成功） | **清理器是好的、在跑的**；它只是**没有规则可执行** |
| 9 | `find /tmp -maxdepth 1 -mindepth 1 -mtime +10` | **9 条**，全部 `2026-07-15/16`（开机那天） | 现存最老的条目 = 开机时间 ⇒ **开机后没有发生过任何 age 清理** |
| 10 | 这 9 条的内容 | `.Test-unix` `.XIM-unix` `.font-unix`（`x11.conf` 的 **`D! … 10d`** 管）；4 个 `systemd-private-*`（**被 `x /tmp/systemd-private-%b-*` 显式豁免**，另有 `R!` 规则但同样只在 boot 生效）；`.X0-lock`（`x11.conf:19 r! /tmp/.X[0-9]*-lock`）；`hsperfdata_root`（**无任何规则**） | 逐条都能解释"为什么该剩" |
| 10b | 逐行核实（`grep -rn` 定位到**提供规则的文件**） | `/usr/lib/tmpfiles.d/x11.conf:16 D! /tmp/.Test-unix 1777 root root 10d`；`x11.conf:19 r! /tmp/.X[0-9]*-lock`；`tmp.conf: D /tmp 1777 root root -` | 每个"该剩"的结论都能指到**具体文件的具体行**，不是印象 |
| 11 | `df -i /` | 9,834,400 inodes，**用 6%** | 事故是**空间 + 遍历**问题，不是 inode 耗尽 |
| 12 | 今日计数：`find /tmp -maxdepth 1 -mindepth 1 \| wc -l` = **3,059**；`-type d` = **677** | 与事故当天（13,044 个 **Go 测试 scratch 目录** `/tmp/pN_dump_*`、`*_verify`，556M —— 见 `devdoc/peerbox-resource-exhaustion-20260911.md` 第 60 行）**不可比** | 事故残骸已被回收；**但 `/tmp` 的规则仍是 `-`（无 age）** → **只要再产生一批 garbage，它照样会活到下次重启**。所以"现在没有 13,044 ≠ 问题已修" |

**三个必须分开说的子结论**

1. **[本机实测] "每日清洁器对它无事可做"**：`--clean` 的语义是"处理**配了 age** 的规则"（man 原文），
   而 `/tmp` 的 age 是 `-`（man：「`-` = no automatic clean-up」）。**13,044 个目录再多、再老，也不匹配任何规则**。
2. **[本机实测] 那个 `D! … 10d` 类规则在现实中"永不生效"**：`D! /tmp/.X11-unix … 10d` 这类行的 `!`
   要求 `--boot`，而**带 `--boot` 的那个服务不带 `--clean`**，带 `--clean` 的那个服务不带 `--boot`。
   → 5 个 X 相关目录活了 57 天。**这是"规则写了但两个执行模式互斥"的结构性盲点**，
   比"忘了配 age"更隐蔽，是本次调研最有复用价值的发现之一。
3. **[推测] "只要……就会被清掉"的错误期待**：如果哪天有人把 Debian 的 `-` 改成 `10d`，
   **也不等于**我们的 13,044 目录会被清 —— 因为 (a) age 判据含 **atime**，"被读过"的目录会**续命**；
   (b) 只有 `--clean` 会跑（每天一次），`/tmp` 里"每天都被读"的目录永远年轻；
   (c) 本机 249 **没有 `--dry-run`**，改配置前无法预演。

**与"自研清理器"的对比结论**：上游给的是**声明式规则 + 豁免机制 + 先 dry-run（≥v262）**；
我们缺的不是"更强的清理器"，而是 **(a) 一条真正生效的规则**，和 **(b) 产物落在"有归属"的位置**。
→ §5.4 的"不做"边界正由此而来。

### 4.4 age-based GC 的失败模式（含上游**自己承认**的一条）

| 失败模式 | 依据 | 后果 |
|---|---|---|
| **atime 续命**：被读过的条目永不过期 | [成熟实践] man：age 由 mtime/**atime**/ctime 决定，"**any of these … will prevent cleanup if it is more recent**" | 长期被扫描/被 `du` 的目录**永远不会被回收** |
| **非 root 反而更清不掉** | [成熟实践] **[上游承认]** `man systemd-tmpfiles`："**When running as non-root, directories which are checked for files to clean up will have their access time bumped, which might prevent their cleanup.**"（CAP_FOWNER 才能在检查时不更新 atime） | 用户态清理器**自带**"越查越清不掉"的反向效应 |
| **flock 被别人持住 → 整棵跳过** | man：lock 存在则 "it (and everything below it) is **skipped**" | 这是**特性**（豁免机制），但也是"垃圾清不掉"的一个正解 |
| **只清"规则覆盖到的路径"** | man：age 只作用于列出 `d/D/e/v/q/Q/C/r/R/x/X` 且配了 age 的行 | **不在规则里的目录 = 无人负责**（本机 `/tmp/hsperfdata_root` 就是这样活了 57 天） |
| **`!` 行在 `--clean` 下被跳过** | man：`!` 只在 `--boot` 时考虑 | 见 §4.3 子结论 2 |
| **`age=0` 是"每次全清"** | man：`e` 的 "age … is 0, contents will be unconditionally deleted" | 误配一根手指，等于"每轮删光"→ **不要在共享目录上用 0** |
| **GC 自身扰动会反馈** | man age-by 段："**the aging logic itself will alter the ctime whenever it deletes a file inside it** … ctime for directories is ignored by default" | 上游已经处理了这个反馈；**自研 GC 很容易忘**（我们的判据里若含 ctime 会造成抖动） |
| **"删除"不可逆，误删无法回滚** | [推测]（结构性事实） | 见 §5.3 的爆炸半径机制：**只允许删"自己命名空间内 + 无存活占用"** 的东西 |

### 4.5 "不要自研清理器"的先例（含**本项目自己**的正例与它的合法形态）

**[成熟实践] 先例谱系（都是"声明式 + 收窄范围 + 先预演"）**
| 领域 | 成熟件 | 关键设计 |
|---|---|---|
| 临时文件 | **systemd-tmpfiles** | 规则声明式；`x`/`X` 豁免；flock 豁免；`--dry-run`（v262+）；只做规则里写的路径 |
| 日志 | **logrotate** | 轮转规则声明式；`copytruncate`/`maxage`；**先 `--debug` 预演** |
| 容器产物 | **docker/podman system prune**（+ 存储配额） | 只清"自己管的对象"，且需要 `--volumes` 之类**显式升级**才动数据 |
| 包缓存 | **apt clean / dnf clean / snapd 保留 N 版** | 按"包管理器自己的命名空间"清理 |
| Go 构建缓存 | **`go clean -cache` / `GOTMPDIR`** | 由**工具自己**管自己的目录（我们至今仍在 `/tmp/go-build*` 上留垃圾，见 `devdoc/scratch-residue-audit.md` §6） |
| **本项目** | `internal/scratch` 的 GC（`GCLegacyScratchResidue` 等） | 见下 |

**[成熟实践] 本项目的正例形态（可复用配方）**：它**确实自研了**，但把自己关在极窄的笼子里 ——
① 只走**自己的** memory base（`GOPI_SCRATCH_MEMORY_BASE`，Rule 2 单读者）；
② 只匹配**自己的命名空间**（`gopi-` 前缀 **且** `suite-tmp-` 中缀 **且** 有后缀）→ 另一程序的
`someone-else-suite-tmp-*` 与裸 `suite-tmp-*` **不匹配**；③ 只删**目录**（`Lstat`，不跟随符号链接）；
④ 只删**超过 6h** 的；⑤ **绝不删有活 holder 的**（`.holder` + 批量 `/proc` 检查）；
⑥ 目标是 `/` 文件系统时**拒绝整个 base**；⑦ 删除失败**逐条上报**（Rule 15b）。
→ **[推测] 这就是"自研清理器"唯一可接受的形态：命名空间收窄 + 存活者永不删 + 证据可复算**。
反过来说，"全局扫描 + 按名字/PPID 猜谁该删"**没有任何"成熟实践"支持**（我在本轮来源里找不到一个）。

### 4.6 tini / dumb-init 为什么**只** reap 自家子进程（设计理由，逐字）

**[成熟实践] `krallin/tini` README（§8 来源 10）**
- "All Tini does is **spawn a single child** … and wait for it to exit **all the while reaping zombies and performing
  signal forwarding**."
- "**By default, Tini needs to run as PID 1 so that it can reap zombies (by running as PID 1, zombies get
  re-parented to Tini).** If for some reason you cannot run Tini as PID 1, you should register Tini as a
  **process subreaper** instead (only in Linux >= 3.4) … `tini -s` / `TINI_SUBREAPER`."
- "**By default, Tini only kills its immediate child process**" … `-g` / `TINI_KILL_PROCESS_GROUP` 才杀整组。
- "Tini can set its **parent death signal** … `-p SIGTERM`".
- 为什么它值得做（README 给的理由）: "It protects you from software that **accidentally** creates zombie processes,
  which can (over time!) **starve your entire system for PIDs** (and make it unusable)."

**设计理由（[成熟实践] 从上述语义可推出，[推测] 是我把它接到我们的场景上）**
1. **回收能力来自"关系"**：只有 PID 1 或注册过的 subreaper **才会被内核**把孤儿重新挂到自己名下。
   → 它处理的是"**我负责的那棵树**"，**不是**"系统里所有看起来像孤儿的进程"。
2. **因此它不可能误伤别人**：它没有"判断谁是垃圾"这一步，也就没有 false positive。
3. **它的总开关是 `-g`（组）与 `-p`（父死信号）**：把"范围"做成显式参数，而不是启发式。
4. **对我们的直接含义 [推测]**：要"不产生孤儿"，办法是把进程放进**一个有边界的集体**
   （cgroup scope / PID namespace / 进程组），并让**创建者**负责收 —— 而不是事后全局扫描。
   这正是 §1.3 的 `KillMode=control-group` 与 `bwrap --die-with-parent` 在做的事。

---
## 五、结论：推荐机制 / 不自研清单 / 误伤分析 / 不做边界 / 额度定法

### 5.1 推荐机制（三层，按"靠前靠安全"排序）

| 层 | 机制 | 权威依据 | 本机可用的证据 |
|---|---|---|---|
| **L1 事前限制** | `sudo systemd-run --scope`（或 service）+ `IOWriteBandwidthMax`（设备级绝对上限）+ `IOWeight`（竞争权重）+ `MemoryHigh`（主控）+ `MemoryMax`+`MemorySwapMax=0`（兜底）+ **`TasksMax`（fork bomb）** + **时限（scope 内 `timeout`；service 用 `RuntimeMaxSec=`）** | §1.1–1.3 [成熟实践] | 主路线实测 75→7.9MB/s、OOM exit 137、`systemctl kill` 整组回收；本单复核设备号/`TasksMax`/`RuntimeMaxSec` 语义 |
| **L2 沙箱降权** | `bwrap --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp --bind <workdir> <workdir> --unshare-pid --new-session --die-with-parent`（**没有 `--die-with-parent` 就不要用 bwrap 跑测试**；**不 bind dbus socket**） | §2.2 [成熟实践] + 上游 Limitations | **本机实测**：系统盘 EROFS、工作目录可写、`--die-with-parent` 生效 |
| **L3 产物自清** | **创建者自清**：产物写在**有归属的目录**（`$HOME/workspace/<...>` 或 `/dev/shm`，**设备号断言 ≠ `/`**），由 `t.Cleanup`/`trap EXIT`/受限 GC 收；**不引入全局扫描删除器** | §1.6（cgroup 不管文件数）+ §4.5（先例）+ `devdoc/tmp-ban-scratch-root.md`（已有设备号断言先例） | **本机实测** `/`=`/tmp`=sda3(2051)、`/home`=sda4(2052)、`/dev/shm`=tmpfs(28) |

**为什么这个顺序**：[成熟实践] 佐证 + [推测] 我的理由 —— L1/L2/L3 的失效模式都是"**退化为没有保护**"
（带宽限制不生效 = 回到事故发生前；沙箱没配 = 就是普通进程；GC 不跑 = 垃圾多留一晚），
**没有"删错/杀错"这种不可逆失效**。这正是 `GOLDEN-RULES.md` §4 那条不对称判据的落地形态。

### 5.2 不自研清单（"不要自己写"的部分，逐条给替代）

| 不做什么 | 用什么 | 依据 |
|---|---|---|
| **不写全局进程扫描器杀"孤儿"** | cgroup：`KillMode=control-group`（默认）、`systemctl kill <unit>`、service 模式（父=PID1）、`bwrap --die-with-parent` | §1.3 + §4.6 [成熟实践]（tini 只 reap 自家树） |
| **不写自己的 seccomp 策略引擎** | `SystemCallFilter=`（白名单；`~` 反向）/ `bwrap`（可传 seccomp）/ `nsjail --seccomp_policy`（Kafel） | §2.2 [成熟实践] |
| **不写自己的文件系统沙箱** | `bwrap`（已装）或 `nsjail`/`firejail`；systemd `ProtectSystem=`/`ProtectHome=`/`PrivateTmp=`（**注意 `--user` 下不可用**） | §2.2 [成熟实践] |
| **不写"命令黑名单"** | 见 §2.1：黑名单是结构性地无效；用"能力限制"代替"名字禁止" | §2.1 [推测 + 上游措辞佐证] |
| **不写通用清理器 / 不按名字扫全局 `rm -rf`** | systemd-tmpfiles / logrotate / 包管理器自带 clean；**真要自研，只允许"极窄命名空间 + 存活者豁免 + 设备号断言"**（§4.5 正例） | §4.5 [成熟实践] |
| **不新增常驻守护进程**（含为跑限制而 `enable-linger`、为 GC 起 watchdog） | 寄生于**已有** tick（本项目已如此：`gopi task worker` 长驻面 + merger pass tick；`devdoc/scratch-gc-resident-tick.md`） | 项目内先例 + §1.4 [推测]（linger 会新增常驻 user manager） |
| **不把时限只交给外层 `timeout`** | scope 内 `timeout` **或** service `RuntimeMaxSec=`；**不要 `setsid` 与 `timeout` 混用** | §1.3 / §3.3 [本机实测] |
| **不把"防写系统盘"寄望于 `ulimit -f`** | 目录位置 + 设备号断言；`ulimit -f` 只当"单文件上限" | §3.4/3.5 [本机实测] |
| **不靠"教育/自觉"**（"大家记得别写 /tmp"） | 机械检查（设备号断言、静态 gate）；本项目已有先例（`scripts/**` 的 `while :` gate、`/tmp` 路径 gate） | `GOLDEN-RULES.md` §3 + `devdoc/tmp-ban-scratch-root.md` |

### 5.3 每个推荐的误伤（false positive）分析与爆炸半径机制

**通用框架**：误伤 = "机制在**不该动**的对象上生效"。判据三问：① 它会动到谁？② 后果多严重？③ 什么机制限制它的爆炸半径？

| # | 推荐 | 它会怎么误伤（场景） | 后果严重度 | 爆炸半径限制机制 |
|---|---|---|---|---|
| 1 | **`IOWriteBandwidthMax` 限带宽** | 本来就要大量写数据的**正经测试**被限到超时 → **假红** | **中**（浪费时间/信任，不损坏数据） | ① 额度按**实测 baseline** 的 1/8~1/10（§5.5）；② 分设备配（`/`(sda3) 严、`/home`(sda4) 宽）；③ 只在**包装器**里生效，普通命令不受影响；④ **[推测]** 保留 `opt-out` 但要求写明理由 |
| 2 | **`MemoryHigh` / `MemoryMax`** | High 太低 → 测试被"重压变慢"到超时（**假红**）；Max 太低 → **OOM 杀掉正在跑的正确测试**（exit 137） | High：中；Max：**中高**（丢掉一次运行结果，不损坏数据） | ① High = 峰值×1.5、Max = 峰值×3，**两档分离**（[成熟实践] 上游推荐 High 主控/Max 兜底）；② `MemorySwapMax=0` 避免 swap 抖动把**整机**拖死；③ OOM 只杀 unit 内进程（**不连带宿主 shell**）；④ **[推测]** 观察 `memory.events` 再收紧，而不是一次到位 |
| 3 | **`TasksMax`（fork bomb 护栏）** | 值太小 → 合法的**并行测试**（`go test -p N`、编译）撞 `pids.max` 失败 | 中（失败可见、可重跑） | ① 显式值而非默认（本机 `DefaultTasksMax=18858` 太宽）；② 触发时 **pids.events** 有计数可查；③ **[推测]** 先设"远大于实测峰值"，再收紧 |
| 4 | **时限（scope 内 `timeout` / `RuntimeMaxSec=`）** | 测试**确实需要更久**被砍 → 结果不完整，可能被误读成通过/失败 | 中 | ① 时限**显式声明**且与文档同步；② `timeout --preserve-status` 保留真实退出码；③ superset 信号 `-k` 先 TERM 后 KILL（`-k` 给清理机会） |
| 5 | **`bwrap` 只读根** | 测试**真的**需要写的路径没 bind 进来 → 测试失败（**不是**静默错误，是显式 EROFS） | **低**（失败即知，且不会损坏） | ① 白名单式 bind（只有工作目录可写）；② **不加 `--unshare-all`**（保留网络，避免"测试连不上网"这类低级误伤）；③ **不 bind dbus**（避免把沙箱变成提权通道——这是**防误伤自己**的机制） |
| 6 | **创建者自清 + 受限 GC** | **最常见**：GC 把**还在被用**的目录删掉（进程还在写，文件没了） | **高**（数据丢失、且不可逆） | ① **绝不删有活 holder 的**（本项目 `.holder` + `/proc` 检查；`devdoc/scratch-residue-audit.md` 的 `TestGCLegacyScratchResidueNeverDeletesAHeldRoot`）；② 只删**命名空间内**的（前缀+中缀+后缀三重）；③ 只删**目录**（不跟随 symlink，不删文件）；④ 只删**超过 6h**；⑤ base 在 `/` 上**拒绝整个 base**；⑥ 删除失败逐条上报；⑦ **[成熟实践] 上游同款**：`tmpfiles.d(5)` 的 flock 豁免（应用持锁 = 跳过硬删） |
| 7 | **设备号断言（禁 `/tmp` 作产物根）** | 在小内存/只读容器里**没有 `/dev/shm`** 可用 → 断言把**所有**测试挡死 | 中（"全红"，但**安全侧**失败，且退出码/消息明确） | ① 保留**显式 opt-out**（本项目 `GOTESTSUITE_NO_PIN=1`）并写明适用条件；② 退出码 **97** 是**致命且可 grep** 的，不会被误读成"部分通过"；③ **[推测]** opt-out 只在 CI 镜像里设，开发机保持默认拒绝 |
| 8 | **watchdog（若最终要做，见 §6.1）** | 见 §6.2 专节：按名字/PPID 判断 → 误杀合法常驻（Xvfb/sshd/dbus/ollama/gopi merger） | **高**（杀错正在跑的任务） | 见 §6.2 的四条硬护栏 |

**共同的一条设计律（[推测]，但与本项目已落地的规则一致）**：
> **判据里凡是"猜"的成分（名字像、PPID=1、跑了很久），就不允许带"删/杀"的动作；
> 凡是带"删/杀"的动作，判据必须全是"事实"（命名空间是我建的、没有活进程持有、设备号不是 `/`、年龄够老）。**

### 5.4 明确"不做"边界（**不做什么**清单）

1. **不做全局扫描杀进程**（任何形式：按名字、按 PPID、按运行时长、按"像测试"）。**理由**：本机合法
   `PPID=1` 常驻就有 Xvfb/sshd/dbus/ollama/`gopi task merger`（`GOLDEN-RULES.md` §4 已实测）；
   没有找到任何权威来源支持这种做法（§4.5 反面）。
2. **不做命令黑名单**（§2.1）。
3. **不做"全树 `rm -rf` 式清理器"**，尤其**不做递归进"别人命名空间的目录"**（本项目已明确：
   `~/workspace/gopi-test-scratch/tools-ext4probe/` 内层故意不递归清理 → 交给 operator 手工确认，
   `devdoc/scratch-residue-audit.md` §6）。
4. **不启用 `loginctl enable-linger`**（为了跑限制而新增常驻 user manager，违背"不新增常驻"）；
   `--user` 路径**本机不可用**（§1.4）。
5. **不把 `/tmp` 当工作区**（设备号 = `/`）；**不把 `/` 或 `/home` 的挂载点当作压测目标**；
   **不写系统盘**（[成熟实践] fio 默认 `allow_mounted_write=false` 的同一条道理）。
6. **不做长时间压测 / 不做破坏性验证**（本单约束；落地后也应"默认有时限、默认有量"）。
7. **不改 `/usr/lib/tmpfiles.d/tmp.conf`**（本机 **249 没有 `--dry-run`** → 无法预演；改系统清理策略的
   误伤面是"整台机器"而不是"一个测试目录"，**爆炸半径不可接受**）。若真要动，属于**独立的、需要 reboot 验证的**任务。
8. **不把 `ulimit -f` 当"防写系统盘"手段**（§3.4 第 5 点：它反而会被 systemd-coredump 落到 `/` 上）。
9. **不把推测当惯例**（本报告的 [推测] 段落在落地前必须补实测或权威来源）。

### 5.5 额度定法（可直接抄的定值流程）

**定值流程（[推测] 的方法论 + [本机实测] 的输入）**
1. **测 baseline（无限制，跑 3 次取中位）**：本机 `dd bs=1M count=200 conv=fsync` → **~75 MB/s**。
2. **写带宽** = baseline / 8 ~ /10 → 本机取 **`8M`**（1/9.4）。**理由**：压得住"独占"，不至于假红。
3. **内存**：测峰值 RSS `P`，取 **`MemoryHigh=1.5P`**、**`MemoryMax=3P`**、**`MemorySwapMax=0`**。
   （主路线用的 `MemoryMax=64M` 是**兜底位**的正确形态；对"正经测试"必须按 P 算，否则假红。）
4. **进程数**：`TasksMax` = 实测峰值任务数 × 3（先宽后紧；本机默认 18858 无意义）。
5. **时限**：`timeout N`（scope 内）或 `RuntimeMaxSec=N`；**N = 实测典型耗时 × 3**。
6. **单文件上限（可选第四层）**：`ulimit -f` = 单产物上限；**注意单位是 512B 块**（本机实测 `1024` → 1MiB），
   且**它不限制总文件数**（§3.4）。
7. **落盘位置**：`$HOME/...` 或 `/dev/shm`，**设备号断言 ≠ `/`**；`/dev/shm` 的用量同时算进内存额度（§1.2）。
8. **写成常量 + 附实测证据**（Rule 1/2）：每个数字旁边写"用哪条命令测出来的、哪天的数"。

**本机推荐值（作为**起点**，不是教条；标 [推测] 的都是我的取值）**
| 维度 | 值 | 依据 |
|---|---|---|
| 写带宽（`/`=sda3） | `8M` | [本机实测] baseline 75MB/s 的 ~1/9（主路线刚验证过） |
| 写带宽（`/home`=sda4） | `32M`【推测】 | 同样的 1/8~1/10 逻辑在 sda4 上应重测（其 baseline 未测） |
| `IOWeight` | `50` | [本机实测] 主路线形态；只在竞争时生效（[成熟实践] 语义） |
| 内存 | `MemoryHigh=1.5P` / `MemoryMax=3P` / `MemorySwapMax=0`【推测，P 需测】 | [成熟实践] 上游"High 主控 Max 兜底" |
| `TasksMax` | `64`（压测包装器）/ 实测×3（测试套件） | [本机实测] `DefaultTasksMax=18858` 形同虚设 |
| 时限 | 典型耗时×3，**默认必须有**（抄 nsjail `--time_limit` 默认 600s 的思路） | [成熟实践] nsjail README |
| 产物根 | `$HOME/workspace/...` 或 `/dev/shm`（断言 ≠ `/`） | [本机实测] 设备号 |

---
## 六、nick 两问专节 + "哪个方向被 cgroup 限制取代了"

### 6.1 watchdog 该是 **cron** 还是 **service**？→ **service（且"寄生 tick"优于"新起 service"）**

**[成熟实践] 三种候选的语义差异（都来自本机 man / 项目先例）**

| 形态 | 权威语义 | 对本项目的优缺点 |
|---|---|---|
| **cron** | 独立守护，按表达式起进程；**生成的进程落在 cron 自己的 cgroup 里** | ❌ **本项目有前科**：`devdoc/peerbox-resource-exhaustion-20260911.md` + memory #445 记录，cron 的 cgroup 在任务结束时会对组内进程发 SIGTERM，曾把 15 个任务里的 1 个打断；历史上为躲开它，把守护改成了 `systemd-run --user --scope`。**[推测]** cron 适合"一次性、可被杀"的作业，**不适合**"要保证跑完的清理/巡检" |
| **systemd service（+ timer）** | [成熟实践] oneshot + timer 是 systemd 的**标准周期任务形态**（本机实证：`systemd-tmpfiles-clean.timer` 每晚跑、`systemd-tmpfiles-clean.service` oneshot，`journalctl` 有 7 次成功记录） | ✅ 生命周期/日志/退出码/失败重启都有归口；❌ **新增一个常驻 unit**（与"不新增常驻"冲突，且 GC 只有几十 ms 的工作量也起一个 unit 是浪费） |
| **寄生在已有常驻进程的 tick**（本项目现状） | `devdoc/scratch-gc-resident-tick.md`：把 sweep 挂在 `gopi task worker` 长驻面与 `gopi task merger` pass tick 上，**"Zero new resident processes"**；`GOPI_TEST_SCRATCH_GC_MIN`（默认 60）是**唯一**读它的地方（Rule 2） | ✅ **0 新增常驻**；✅ 复用已有的 5min/10min tick（比 6h 的年龄阈值细一个数量级）；✅ 失败**fail-open**（只记日志，绝不让宿主任务失败）；✅ 有 `scratch_gc=false` / `GOPI_TEST_SCRATCH_GC_MIN=0` 单旋钮关闭 |

**推荐（[成熟实践] 项目先例 + [推测] 选型理由）**：
> **不要 cron；优先级是"寄生 tick" > "oneshot service + timer" > "常驻 service"。**
> 若某天需要"宿主完全没人跑测试也要清理"，那才补一个 **oneshot service + timer**（把已有 tick 当实现，
> timer 只当触发器）—— 即使那样也**不要**常驻循环进程，因为"常驻=多一个可能自己出问题的东西"
> （`GOLDEN-RULES.md` §4 的原话："它自己会不会是杀手"）。

**⚠️ 一条必须写进落地设计的硬约束（[本机实测] / [成熟实践] 语义）**：**watchdog/GC 若用 `systemd-run --scope` 包起来，
外部 `timeout` 管不到它**（主路线实测 1742s，本单复核 `timeout` 不跨 `setsid`）→ **时限必须写在 scope 内部**，
或改用 service + `RuntimeMaxSec=`。**watchdog 自己卡死而没人发现，是它最现实的失效模式。**

### 6.2 会不会误伤？→ **会（若按名字/PPID 猜），且误伤是不可逆的；把判据换成"事实"后上界是"多留一晚垃圾"**

**[本机实测 + 既有实测] 误伤确实存在，不是理论**
- `GOLDEN-RULES.md` §4：本机合法 `PPID=1` 常驻 = Xvfb / sshd / dbus / ollama / **gopi task merger**。
  按"PPID=1 + 名字像测试"去杀 → **立刻误伤**。
- **[本机实测]** 我自己在 probe 期间就撞见一个**多年前的 `sleep 3600`**（PID 3465253）仍在跑 ——
  "孤儿"**真的**存在，但"看起来像孤儿"（PPID=1、跑了很久）**不能**证明它该被杀（本单按约束**没有**动它）。
- **[推测] 更隐蔽的误伤**："杀了父进程"可能连坐一个**正在做正确事情**的测试；"删了目录"可能删掉
  **正被写**的产物（删掉后进程继续往无名 inode 写，磁盘不回收 —— `devdoc/scratch-residue-audit.md` §1 记录过
  "cwd 已删除却仍持有 inode"的实例）。**这类损坏不可逆。**

**四条硬护栏（若最终仍要 watchdog，缺一不可；[成熟实践] 精神 + 本项目已有实现）**
1. **命名空间**：只处理"**我建的**"东西（本项目：`.holder` 文件 + 目录名前缀/中缀/后缀三重匹配；
   见 `devdoc/scratch-residue-audit.md` 的 17 行 accept/reject 表）。
2. **存活占用**：有活进程持有 → **绝不删/杀**（`.holder` pid+starttime + 批量 `/proc` 检查；
   注意 `ptrace_scope` 导致**同用户兄弟进程的 `/proc/<pid>/cwd` 不可读** → 只能判"自己/后裔"，
   所以 **`.holder` 是必需的，不是锦上添花**，memory #1755 pitfall (a)）。
3. **年龄/阈值**：只处理**超过阈值**的（6h）；**"测不到"一律保留**（never delete on doubt）。
4. **设备/位置**：base 在 `/` 上的**整个拒绝**（设备号断言）。

**误伤后果严重度分级（[推测]，供决策）**
| 误伤类型 | 严重度 | 可逆性 |
|---|---|---|
| 杀错进程（正在跑的任务） | **高** | ❌ 不可逆（结果丢失；可能连带删掉中间产物） |
| 删错正在写的目录 | **高** | ❌ 不可逆（数据丢失 + 磁盘不释放） |
| 限制过紧 → 假红 | 中 | ✅ 可逆（重跑；但会侵蚀信任 → 长期杀伤） |
| GC 漏删（保守过头） | **低** | ✅ 可逆（磁盘多占一晚，下一轮再收） |
| watchdog 自己卡死 | 中 | ✅ 可逆（无动作 = 回到无保护） |

→ **结论**：**做"保守的漏删"，不做"激进的误删"**；两者不对称，方向必须偏保守。

### 6.3 ★ 哪个方向被 cgroup 限制**取代**了？—— 明确回答

> **被取代的：整条"自研 watchdog 全局扫描 → 判断谁是孤儿 → 杀掉"的方向。**

**取代依据（三条，前两条是既有实测，第三条是本单新增的语义论证）**
1. **[本机实测]** `systemctl kill -s SIGKILL <scope>` **一次干掉整组**（项目记录：剩余 0）；
   而"按 PID 猜"的做法在同一场景下**反复失败**（主路线实测：`kill -9 <一串 PID>` 之后进程还在，
   因为 scope 内的进程有独立生命周期）。→ **不是"更好"，是"问题消失了"**。
2. **[成熟实践]** `systemd.kill(5)`：`KillMode=control-group`（systemd 默认）杀的是
   "**all remaining processes in the control group**"；`cgroup.kill` 是同一个形状。
   **它不需要回答"谁是孤儿"** —— 组是我建的，组里所有东西都是我的责任。
   **[成熟实践] 同源先例**：tini 的"只 reap 自家树"（§4.6）—— **回收的合法性来自"边界/关系"，不来自"识别垃圾"**。
3. **[本单新增]** 对照表（事故当时的判据 vs cgroup 的判据）：

| 维度 | 全局扫描 watchdog | cgroup 限制 |
|---|---|---|
| 判断依据 | 启发式（名字像、PPID=1、跑了很久） | **内核计数器**（实际 IO 字节数）与**成员身份**（在不在我的 cgroup 里） |
| 需要判断"谁该被杀"吗 | **需要**（必然有 false positive） | **不需要**（越界者自己撞墙；回收按组） |
| 失效模式 | 判断错 = **杀错东西** | 限制不生效 = **退化为无保护**（=事故发生前的状态） |
| 可审计性 | 要解释"为什么杀了它" | 额度**声明式**，一眼可见 |
| 事后残留（产物/目录） | 它管不了（删目录风险同 1/5.3#6） | **cgroup 也管不了**（§1.6）→ 归 L3 创建者自清 |

**没有被取代的（必须明说，避免"cgroup 万能"的错觉）**
- **"产物（目录/inode/空间）无人回收"** 这个问题**cgroup 完全不解决**（§1.6 第 1 行：没有文件数量控制器）。
  → 它归 **L3 创建者自清**，而且**用成熟件（tmpfiles 的规则/豁免）而不是自研扫描器**。
- **"压测脚本自己无限循环/每块 fsync/写系统盘"** 也不被 cgroup **解决**，只被 cgroup **限制住**；
  根治是**换成 fio/stress-ng 的成熟默认**（§3.1/§3.2）。
- **"谁清理"这个组织问题**：§4.3 的 exact reason 说明 —— 我们以为"系统在清 /tmp"，
  实际上**发行版把这条规则关掉了**。**cgroup 不会替我们回答"这块磁盘谁负责"**。

---
## 七、验证与自查（本单是研究单，验证 = 证据可复算 + 无副作用）

**A. 无副作用证据（本单**没有**做破坏性操作）**
- 所有 probe 都在隔离 worktree 的临时目录（`gopi-cleanup-safety/.probe-20260911/`，位于 `/dev/sda4`）内进行。
- **已删除并核验**：
  ```
  $ ls -d ./.probe-20260911     → No such file or directory        （worktree 目录）
  $ ls -d /home/user/workspace/csafety-probe  → No such file or directory  （man dump 暂存目录）
  $ git status --porcelain      → （空，除本报告外无改动）
  ```
- **没有**删除任何目录/文件（除自己创建的 probe 文件）、**没有**跑长压测（probe 都是秒级）、
  **没有**杀任何进程（包括那个多年前的 `sleep 3600` —— 仅在 §6.2 引用为证据）。
- 可复算的只读检查：`man`/`--help`/`--cat-config`/`--clean`（**注意**：本机 249 **无 `--dry-run`**，
  我**没有**执行 `--clean`，只读的是 `--cat-config` 与 journal 历史）。

**B. 证据可复算清单（读报告的人可以自己再跑一遍）**

| 报告位置 | 复算命令 |
|---|---|
| §1.1 IO 语义 | `man systemd.resource-control`（搜 `IOWeight=` / `IOWriteBandwidthMax=`） |
| §1.2 内存语义 | 同上（`MemoryHigh=` / `MemoryMax=` / `MemorySwapMax=`） |
| §1.3 scope/service、timeout 不跨界 | `man systemd-run`；`man systemd.service`（`RuntimeMaxSec=`）；`man systemd.kill`（`KillMode=`）；本单 probe：`timeout 1 setsid /bin/sleep 137713 &` → 存活 |
| §1.4 免 sudo 路径 | `loginctl show-user nick \| grep Linger`；`ls /run/user/1000/bus`；`systemctl is-active user@1000.service`；`systemctl --user is-system-running` |
| §1.6 盲区 | `systemctl show DefaultTasksMax`；`df -i /`；`man systemd.resource-control`（通篇搜 `inode`/`quota` → 无） |
| §2.2 bwrap | `bwrap --help \| grep -E 'die-with-parent\|ro-bind\|new-session'`；README：`devdoc` 引的 raw URL（§8 来源 11） |
| §3.1 fio 默认 | fio 官方文档（§8 来源 8）：搜 `runtime=` / `time_based` / `unlink=` / `allow_mounted_write` |
| §3.2 stress-ng | 上游 `stress-hdd.c`（§8 来源 9）：`DEFAULT_HDD_BYTES` / `help[]` |
| §3.4 ulimit 实测 | `bash -c 'ulimit -f 1024; dd if=/dev/zero of=$HOME/probe.bin bs=1M count=8 conv=fsync'; echo $?` → 153 + 1048576B（**跑完删掉 probe.bin**） |
| §4.2 Debian tmp.conf | `dpkg -S /usr/lib/tmpfiles.d/tmp.conf; tail -4 /usr/lib/tmpfiles.d/tmp.conf`；上游对照见 §8 来源 5 |
| §4.3 exact reason | 见 §4.3 的 11 行命令表（全部只读） |
| §4.6 tini 边界 | tini README（§8 来源 10）：搜 "reaping zombies" / "only kills its immediate child" |

**C. 自查（本报告是否满足交付要求）**
- ✅ 四节 + 结论齐备；每处结论都带 **[成熟实践]/[本机实测]/[推测]/[未知]** 标注。
- ✅ 每条推荐都有**误伤场景 + 严重度 + 爆炸半径机制**（§5.3 八行表）。
- ✅ 有"不做"清单（§5.4，九条）与额度定法（§5.5，含本机推荐值表）。
- ✅ nick 两问有专节（§6.1 cron vs service；§6.2 会不会误伤），并明确回答了"哪条方向被取代"（§6.3）。
- ✅ `ulimit -f` vs `dd` 有**命令 + 退出码 + 报错原文**（§3.4 六行表）。
- ✅ 引用了本机 `man tmpfiles.d` 的实际内容与 `/usr/lib/tmpfiles.d/tmp.conf` 实际内容（§4.1/§4.2）。
- ✅ 13,044 目录的 exact reason 有命令级证据（§4.3 十一行）。
- ✅ ≥6 个可核验权威来源（§8 列 12 条）。
- ⚠️ **一处如实更正题面**：`man tmpfiles.d`（本机 249）与上游 main 的 `man/tmpfiles.d.xml` **均无 `SAFETY` 章节**
  —— 我没有伪造该章节，而是引用**实际存在的**安全条目（Age/`-`、flock 跳过、`!` 仅 boot、非 root atime 陷阱），
  并给出可核验来源。这是"引用权威"与"不编造"之间我选择的后者（§4.1 末尾有完整说明）。

---
## 八、来源清单（≥6 个可核验来源；分成"一手权威"与"本地证据"）

**一手权威（上游/官方文档与源码）**
1. `systemd.resource-control(5)` —— 本机 man（systemd 249）：`IOWeight=` / `IOWriteBandwidthMax=` /
   `MemoryHigh=` / `MemoryMax=` / `MemorySwapMax=` / `TasksMax=` 原文（§1.1–1.2、1.6）。
2. `systemd-run(1)`、`systemd.service(5)`、`systemd.kill(5)`、`systemd.exec(5)` —— 本机 man（§1.3、1.4、2.2）。
3. `tmpfiles.d(5)` —— 本机 man（systemd 249）：类型码表、**Age 语义（含 `-` = no clean-up）**、
   **flock 跳过语义**、`x`/`X` 豁免、`!` 仅 boot、非 root atime 陷阱（§4.1、4.4）。
   上游 XML 对照：https://raw.githubusercontent.com/systemd/systemd/main/man/tmpfiles.d.xml （已抓取，无 SAFETY 章节）
4. `systemd-tmpfiles(8)` —— 本机 man + 上游 https://raw.githubusercontent.com/systemd/systemd/main/man/systemd-tmpfiles.xml
   （`--clean` / `--remove` / `--dry-run`（v262+）/ `--exclude-prefix` / `-E` / 退出码 65/73/1）（§4.1）。
5. 上游 systemd `tmpfiles.d/tmp.conf` —— https://raw.githubusercontent.com/systemd/systemd/main/tmpfiles.d/tmp.conf
   （`q /tmp 1777 root root 10d`；`q /var/tmp 1777 root root 30d`）（§4.2）。
6. GitHub Actions 官方文档 —— *Secure use reference*
   https://docs.github.com/en/actions/reference/security/secure-use
   （pin 到 commit SHA / 中间环境变量防注入 / 最小权限；**无 ulimit/cgroup 字样**）（§2.3）。
7. GitHub Actions 官方文档 —— *About self-hosted runners*
   https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/about-self-hosted-runners
   （"**Don't need to have a clean instance for every job execution**"；隔离责任在部署者）（§2.3）。
8. **fio 官方文档 rev 3.42** —— https://fio.readthedocs.io/en/latest/fio_doc.html
   （`runtime=` / `time_based` / `direct=` / `fsync=` / `unlink=` / `allow_mounted_write=` / `--readonly` /
   `--aux-path` / `--warnings-fatal` 原文）（§3.1）。
9. **stress-ng 上游源码** `stress-hdd.c` —— https://raw.githubusercontent.com/ColinIanKing/stress-ng/master/stress-hdd.c
   （`DEFAULT_HDD_BYTES (1 * STRESS_GB)`、`help[]` 文案、`HDD_OPT_FSYNC` 需显式开启）（§3.2）。
10. **tini** README —— https://raw.githubusercontent.com/krallin/tini/master/README.md
    （"**By default, Tini only kills its immediate child process**"；PID1/subreaper 才 reap；`-g`/`-p`）（§4.6）。
11. **bubblewrap** README —— https://raw.githubusercontent.com/containers/bubblewrap/main/README.md
    （"not a complete, ready-made sandbox with a specific security policy"；`--new-session`+TIOCSTI；bind 的提权面）
    本机 `bwrap --help`（0.6.1）（§2.2）。
12. **nsjail** README —— https://raw.githubusercontent.com/google/nsjail/master/README.md
    （`--time_limit SEC **Wall-time limit in seconds (default: 600)**`、namespaces、`--rlimit_*`、Kafel seccomp）（§2.2、2.4）。

**本地证据（本机实测 / 项目内文档）**
13. `/usr/lib/tmpfiles.d/tmp.conf`（dpkg 归属 `systemd`，版本 249.11-0ubuntu3.16）与 `/etc/tmpfiles.d/` 内容（§4.2）。
14. `journalctl -u systemd-tmpfiles-clean.service`（7 次成功）与 `systemctl show`（开机时间 / `ExecStart`）（§4.3）。
15. `devdoc/peerbox-resource-exhaustion-20260911.md`（事故复盘）、`devdoc/cgroup-limiting-not-cleanup-20260911.md`
    （主路线实测：75→7.9MB/s、exit 137、scope 1742s）、`devdoc/GOLDEN-RULES.md` §3/§4/§5、
    `devdoc/scratch-gc-resident-tick.md`、`devdoc/scratch-residue-audit.md`、`devdoc/tmp-ban-scratch-root.md`。

**明确排除的来源（诚实记录）**：`bing` 对多个 systemd/隔离类查询返回 zhihu/baidu 垃圾（不可用）；
`freedesktop.org` 的 man HTML 对本出口返回 HTTP 418；`google` 被 reCAPTCHA 拦；
`salsa.debian.org` 的 Debian changelog 抓取被反爬拦下（§4.2 的 Debian 改动理由因此记为 **[未知]**，未推测）；
`openai/codex` 与 `anthropics/claude-code` 仓库内**没有**可核验的沙箱实现细节 → §2.4 相关内容记为 **[未知]**，不写成结论。

---
## 九、落地候选（本单**不实现**，仅登记，供后续单取舍）

> 每条都标了"是否需要新的实测"；**没有一条**引入新的常驻进程或自研清理器。

| # | 候选动作 | 依赖 | 是否需要新实测 |
|---|---|---|---|
| C1 | 压测包装器（`scripts/`）改用 fio/stress-ng + 补齐 9 条默认（时限/总量/目标目录/不每块 fsync/单实例） | 需先装 fio/stress-ng（**装新依赖 = 决策项**） | 是（装完测 baseline） |
| C2 | 包装器加 **设备号断言**：产物根 `st_dev == / 的 st_dev` → 拒绝启动（项目已有同款先例） | 无 | 否（已有先例可抄） |
| C3 | 包装器加 `TasksMax` 显式值（默认 18858 形同虚设） | sudo scope | 否（语义已核） |
| C4 | 时限从"外层 `timeout`"改为"scope 内 `timeout`"或 service `RuntimeMaxSec=`；**禁用 setsid+timeout 混用** | 无 | 否（本单已实测） |
| C5 | 若要把测试放进 `bwrap`：固定参数清单（含 `--die-with-parent --new-session`、**不 bind dbus**）+ review | 无（bwrap 已装） | 是（跑一轮真实测试验证不误伤） |
| C6 | 内存额度改"High 主控 + Max 兜底"两档（按实测 P 计算） | 无 | 是（测 P） |
| C7 | 把"不自研清理器/不自研 watchdog"写成项目规则（`GOPI.md` 或 devdoc 规则），并附 §4.5 正例边界 | 无 | 否 |
| C8 | `--user` 路径若要可用：先修 `/run/user/1000/bus`（**不**用 enable-linger） | 需登录/session 侧改动 | 是 |
| C9 | 若要"宿主无人跑测试也清理"：**oneshot service + timer**（实现仍复用已有 sweep tick），**不新增常驻循环** | systemd unit | 是（timer 行为验证） |

**优先级建议（[推测]）**：C2 > C4 > C3 > C6 > C7 > C5 > C1 > C9 > C8 ——
"机械断言 + 时限正确性"最便宜、收益最直接；"装 fio"涉及新依赖，放在后面单独决策。

---

**报告完**（本单交付物 = 本文件；无产品代码改动）。


