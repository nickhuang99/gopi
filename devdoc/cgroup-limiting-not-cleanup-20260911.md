<!-- doc-class: normative -->
# 防患于未然：用 cgroup/scope 限制代替事后清理（peer1 实测验证）

**日期**：2026-09-11
**来源**：peer1 资源耗尽事故后 the operator 的决策——"限制比清理安全，因为限制不会误判谁该被杀"
**地位**：本研究是**主路线**，取代原先"自研 watchdog 扫全局杀孤儿"的方案

---

## 一、结论先行

**用 `systemd-run --scope` + cgroup v2 资源限制，把任何可能失控的负载（压测脚本、
测试套件、agent 生成的命令）关进一个有硬上限的盒子里。**

理由：限制机制**不判断谁该被杀**——它只是让越界者自己撞墙。没有"误伤"这个概念，
因为不存在"谁是被伤害的对象"：越界的是它自己，受害上限是它自己被分配的那点额度。

**`sudo systemd-run --scope` 在本机实测完全可用**（注意：必须走系统总线；
SSH 非 systemd 会话下 `systemd-run --user` 连不上 bus，会报
`Failed to connect to bus`）。

---

## 二、本机环境事实（只读实测）

| 项目 | 实测结果 |
|------|---------|
| cgroup 版本 | **unified v2**（`cgroup2 on /sys/fs/cgroup`，挂载选项含 `memory_recursiveprot`） |
| 可用控制器 | `cpuset cpu io memory hugetlb pids rdma misc`（**io 和 memory 都在**） |
| 当前 shell 所在 | `user.slice/user-1000.slice/session-18700.scope` |
| `systemd-run --user` | ❌ `Failed to connect to bus: No such file or directory`（SSH 非 systemd 会话） |
| `sudo systemd-run --scope` | ✅ **可用**（实测 `-p IOWeight=10 -p MemoryMax=256M` 跑通） |
| `systemd-tmpfiles-clean.timer` | 已 active 1 个月 26 天（每日触发）—— 成熟清理机制本就在跑 |

---

## 三、三条限制的实测证据（真实执行，非纸面）

### 证据 1：IO 写入带宽限制真实生效

同一个 `dd if=/dev/zero of=... bs=1M count=200 conv=fsync`（写 200MB）：

| 条件 | 耗时 | 实际速率 |
|------|------|---------|
| 无限制 | **2.71s** | ~75 MB/s |
| `-p IOWriteBandwidthMax=/dev/sda3 8M` | **26.54s** | **7.9 MB/s** |

→ 精确命中设定的 8M 上限（误差 1.3%）。**这是"事前限制"可行性的直接证据**：
压测脚本再怎么无限循环，它的写带宽被钉死在 8M/s，磁盘队列不会被它打满。

### 证据 2：内存限制真实生效（越界者自己被 OOM，机器无损）

```bash
sudo systemd-run --scope -p MemoryMax=64M -p MemorySwapMax=0 \
  python3 -c "a=bytearray(300*1024*1024)"
```

结果：`Killed`（**exit=137** = SIGKILL/OOM），**系统其余部分毫发无损**。
→ 内存失控被限制在盒子里，而不是让机器进入 swap 抖动/OOM killer 随机杀别人。

### 证据 3：cgroup 提供"整组回收"的正确形状

`cgroup.kill` 文件 + `KillMode=control-group`（systemd 默认）意味着：
**杀的是整个 cgroup 的所有进程，不需要按名字/PPID 去猜**。
这解决了本次事故的核心难题（孤儿 dd 杀不掉：因为它们脱离了自己的会话，
按进程名/PPID 匹配既不可靠又会误伤）。

---

## 四、为什么这比"事后清理"根本性地更安全

| 维度 | 事后清理（自研 watchdog） | 事前限制（cgroup scope） |
|------|------------------------|------------------------|
| **误伤可能性** | **高**——必须判断"谁是孤儿/谁该杀"，PPID=1 在现代 Linux 不等于孤儿（本机就有 Xvfb/sshd/dbus/ollama/gopi merger 等合法 PPID=1 常驻） | **零**——不针对任何具体进程做判断，越界者自己撞墙 |
| **判断依据** | 启发式（名字/PPID/运行时长）→ 必然有 false positive | 内核计数器（实际 IO 字节数）→ 精确 |
| **失效模式** | 判断错 = 杀错东西（可能杀掉正在跑的关键任务） | 限制不生效 = 退化为无限制（=事故前的状态，不更糟） |
| **可审计性** | 需要解释"为什么删了它" | 额度是声明式的，一眼可见 |
| **附带收益** | 无 | 强制团队思考"这个测试需要多少资源" |

**关键不对称**：限制机制失效的后果是"回到没有保护的状态"，而清理机制失效的后果是
"删掉/杀掉不该动的"——**后者严格更糟**。这就是 the operator 说的"不能引入更危险的东西去防危险"。

---

## 五、落地形态（供决策，非本单实现）

### 5.1 压测/负载脚本：强制跑在受限 scope 里

提供一个包装器（如 `scripts/limited-run.sh`），所有压测/负载/长跑测试走它：

```bash
#!/bin/bash
# 用法: limited-run.sh <写带宽> <内存上限> <秒数> -- <命令...>
# 例:   limited-run.sh 8M 512M 120 -- go test ./...
exec sudo -n systemd-run --scope --quiet \
  -p IOWeight=50 \
  -p "IOWriteBandwidthMax=/dev/sda3 $1" \
  -p MemoryMax="$2" -p MemorySwapMax=0 \
  -p TasksMax=64 \
  timeout "$3" "${@:4}"
```

三重保险：**带宽上限 + 内存上限 + 硬时限**（`timeout`）。任何一维越界，
受害上限是它自己的额度。

### 5.2 测试套件也走限制（不只是压测）

本次事故里，`go test ./cmd/agent` 自己就是负载源。让测试入口（`scripts/run_tests.sh`、
TestMain）默认跑在 scope 里，等于给"测试"这件事本身一个资源天花板。

### 5.3 与"创建者负责清理"的关系

两者**不冲突、互补**：
- **cgroup 限制**防"失控"（运行时）
- **创建者自清**（`t.Cleanup` / pgid）防"残留"（运行后）
- 全局扫描杀孤儿：**仍然不做**（等研究单 `01a08d67` 给出成熟实践，且很可能结论是"不做"）

---

## 六、必须诚实标注的边界与风险

1. **`sudo` 依赖**：`systemd-run --scope` 走系统总线需要 sudo（无密码 sudo 本机可用）。
   若不想依赖 sudo，可创建 user manager（`loginctl enable-linger the operator`）后用
   `systemd-run --user`。**此路径本单未实测**，需验证。
2. **限制粒度是设备级**：`IOWriteBandwidthMax=/dev/sda3 8M` 限的是对 `/dev/sda3`
   的写。若压测写的是别的分区（`/dev/sda4` = `/home`），要另配。
   → 实际上这是个好处：**能给 `/home` 宽松、给 `/` 极严格**。
3. **`IOWeight` 与 `IOWriteBandwidthMax` 的区别**：前者是相对权重（只在与别人
   竞争时生效），后者是绝对上限。**防"独占"用后者，防"抢占"用前者**。
4. **过度限制会误事**：把测试关进 1MB/s 会让它慢到超时假红。额度必须**实测校准**
   （本次实测基线 75MB/s，限制取 8M = 约 1/10，明显能跑完但不会打满盘）。
   → **额度值本身需要像 magic number 一样被论证**（参考 Rule 1）。
5. **cgroup 不能限制"数量爆炸"**：13,044 个目录的问题是**数量**而非带宽。
   `TasksMax` 限进程数，但**文件数没有直接限额**（需靠配额或自清）。
   → 这就是为什么"创建者自清"仍然必须做。

---

## 七、已验证的补充结论（2026-09-11 追加实测）

### 🔴 自伤事故：PA 自己复现了同一个错误（最有说服力的论据）

在写这份文档的过程中，PA 用 `systemd-run --scope` 做**实验 B**（验证限制生效）。
实验超时后 PA 去做别的事，**忘了清理实验进程**。结果：

- 3 个 `while :; do dd ... conv=fsync; done` 孤儿在 `/home/user/workspace/` 里
  **跑了 1742 秒（29 分钟）**，把 load 拉到 6.8
- `timeout 10` **没有生效**——因为 `systemd-run --scope` 把进程放进了**独立的
  cgroup scope，`timeout` 管不到它**
- 发现它靠的是 PA 巡查 load 时的偶然一眼

**这次的清理方式恰好证明了对道路线**：

```bash
# 错误示范（按 PID 猜，且 timeout 管不到 scope 内进程）
kill -9 3992856 3992857 ...          ← 杀了还在（scope 内进程有独立生命周期）

# 正确做法（cgroup 整组回收）
sudo systemctl kill -s SIGKILL run-rfb5ad58bff214d60968ebda1a5eb9a9f.scope
# → 一次干掉整组，不用猜任何 PID，剩余 0
```

**两条教训**：
1. **`timeout` 与 `systemd-run --scope` 不兼容**——scope 内的进程不由外层 timeout
   管辖。要限时，**时限必须设在 scope 内部**（scope 里第一个命令就是 `timeout N`），
   或者用 systemd 自己的 `RuntimeMaxSec=`。
2. **做实验的人也会忘记清理**（PA 也不例外）。这正是为什么"创建者自清 + 下次启动
   清自己命名空间"必须做成机制，而不能依赖自觉。

### loginctl linger 路径：**不必要，`sudo systemd-run --scope` 更简**


实测本机 `loginctl show-user the operator` → **`Linger=no`**，且 `/run/user/1000/systemd`
不存在（无 user manager）。启用 linger 需要 `loginctl enable-linger the operator`，
副作用是**在无登录时也常驻一个 user systemd 实例**——为了跑限制而引入一个新的
常驻进程，违背"不引入更多东西"的原则。

**开销实测**（scope 包装 vs 直接跑 `/bin/echo`）：

| 方式 | 耗时 |
|------|------|
| 裸跑 `/bin/echo tiny` | 0.037s |
| `sudo systemd-run --scope -p MemoryMax=128M /bin/echo tiny` | **0.073s** |

→ 额外开销 **36ms**，对任何实际测试可忽略。**结论：用 `sudo systemd-run --scope`，
不启用 linger。**（无密码 sudo 本机已可用，实测通过。）

### 已验证 vs 待验证 清单更新

✅ 已验证：cgroup v2 unified / io+memory 控制器可用 / `IOWriteBandwidthMax` 生效
（75→7.9 MB/s）/ `MemoryMax` OOM 生效（exit 137）/ `sudo systemd-run --scope` 可用
且开销 36ms / `systemd-tmpfiles-clean.timer` 在跑 / linger=no 且无 user manager

⏳ 待验证（交研究单）：额度校准的**推荐比例**（我们实测 75MB/s 取 8M≈1/10 是否合理）、
`/home`(sda4) 与 `/`(sda3) 分别的设备名与额度、`MemoryHigh` vs `MemoryMax` 的选择、
cgroup 对**文件数量**爆炸无直接限额的确认与替代（磁盘配额）、`ulimit -f` 对 `dd`
的实际效果、与 `run_tests.sh`/CI 的集成点与 opt-out 方式。

