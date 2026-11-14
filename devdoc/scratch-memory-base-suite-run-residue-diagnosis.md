<!-- doc-class: historical -->
# /dev/shm suite 残留 26 天未回收的诊断（2026-10-12，host hostb-Laptop）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

任务：`01a12eaf-3ab5-76f9-9b6b-161f09282e31`（worktree `task/01a12eaf3ab576f99b6b161f09282e31`）

## 0. 结论（先说结果）

**回收机制存在、接好了线、也确实在跑 —— 但它是一个「按名字圈定家族 + 超龄 + 无活持有者」
的回收器，而把基座占满的那些东西，绝大多数是它按名字**拒绝**的。**

三条实测证据（全部可原样重跑，汇总在
`~/.gopi/sessions/01a12eaf-3ab5-76f9-9b6b-161f09282e31/artifacts/diagnosis-evidence.txt`）：

1. **饱和是持续的**：`df -B1 /dev/shm` → 已用 6.67 GB / 7.80 GB（86%），可用 **1.06 GiB**；
   内存放置下限 `MinMemoryPlacementFreeBytes` = **1.0 GiB**（`internal/scratch/tmproot.go:252`）。
2. **六个回收面在饱和此刻的可收集合都接近空**（不是「没触发」）：
   - `gopi tool scratch-gc base --dry-run` → `removed=0`（唯一会删 suite-run 一族的显式面）；
   - `/dev/shm/suite-*` 里超过 6h 的：**0 件**（这一族的年龄分布永远贴着 6h 天花板）；
   - 只读探针 `scratch.BaseResiduePlan(base, 6h)` → **0 条**，
     **无论 `autoBaseResidueEnabled` 是 true 还是 false**（这一点推翻了我自己先前的假设，
     见 §7.1 与 §4 的实测过程）；
   - 同一时刻 `/dev/shm` 里有 **145 件超过 7 天、162 件超过 24 小时**的顶层条目，
     其中最大的一件 **386 MB**、最老的 26 天 —— 它们全都**不在**任何面肯看的名单里。
3. **两个缺口的直接签名**：
   - 「非生成式名字」`gopi-ctl-NYRf` / `gopi-sib` / `gopi-red` / `gopi-scratch-m` ——
     `baseResidueSuffixGenerated` 要求尾部是 4+ 位数字或 8+ 位 hex，`NYRf` 不是 ⇒ 拒（118/118/80/67 MB）；
   - 「字面等于 scratch HOME 名」`gopi-test-scratch` ——
     `IsBaseResidueName` 有**明确的一行** `if name == RootDirName { return false }`
     （`internal/scratch/base_residue.go:515`），386 MB、26 天，永远不收。

**一句话根因**：这是一个**只认自己写过的名字**的回收器，而基座里占位的东西
一半是**外来名字**（不该碰，Rule 34 同形）、一半是**该被重新归类的老名字**
（`gopi-test-scratch` 死副本、`gopi-ctl-NYRf` 这类非生成式 token）。
**没有任何一面的职责是「把基座拉回下限以上」** —— 每一面都只是「把某个名字家族里
超龄的收掉」。于是 1.06 GiB 这个刚好卡在 1.0 GiB 下限之上、之下都算「没空间」的
窄带里，回收器**每次都被问、每次都正确地答「我没有可收的」**，基座就停在那里。

最小修复：把 reclaim 步从「只问 suite-run 一族」扩到
**「问所有声称‘这是我的、且已陈旧’的面」** —— 但这**并不能**把 2.5 GiB 的
「拒收项」的归属问题解决掉（那是人的裁定，见 §7.5），
它解决的是「饱和时刻那条路径只问一个家族」这个真实的覆盖缺口（§6 有真实动词验证）。

## 1. /dev/shm 现状实测（原始输出）

```
$ df -B1 /dev/shm
Filesystem      1B-blocks       Used  Available Use% Mounted on
tmpfs          7804604416 6665777152 1138827264  86% /dev/shm

$ stat -f /dev/shm
  File: "/dev/shm"
    ID: 4674001a08724c15 Namelen: 255     Type: tmpfs
Block size: 4096       Fundamental block size: 4096
Blocks: Total: 1905421    Free: 278034     Available: 278034
Inodes: Total: 1905421    Free: 1788168

$ stat -c '%d %n' / /home /dev/shm
66307 /
66309 /home
27 /dev/shm
```

- 已用 6.3G / 7.3G = **86%**，可用 **1.06 GiB**；
- `/` 与 `/home` 同一设备（66307/66309，同 ext4 底层），`/dev/shm` 是 tmpfs（27）。

### 内存放置被拒的定量证据（同一主机、同一分钟）

`MinMemoryPlacementFreeBytes = 1 << 30`（= 1.0 GiB；
`internal/scratch/tmproot.go:252`）。可用 1.06 GiB 与下限 1.0 GiB 只差 ~60 MiB，
所以**几十 MiB 的波动就决定整批 suite 落在哪**：

```
10-12 14:41:11  /dev/shm (935.4 MiB free, below the 1.0 GiB floor)  → device-root   ← 全批落 ext4
10-12 15:17:xx  /dev/shm (1023.3 MiB free, below the 1.0 GiB floor) → device-root   ← 全批落 ext4
10-12 15:26:07  /dev/shm 1.1 GiB free                              → memory-base    ← 全批落 tmpfs
```

逐字输出（`~/.gopi/../workspace/gopi-test-scratch/queue/test-budget/run-1791787081-618272.json`）：

```
testutil: memory base /dev/shm had no room: reclaimed 0 stale suite root(s) (kept 1 held by live processes)
testutil: suite "standards": scratch root /home/user/workspace/gopi-test-scratch is ext4/3 filesystem
  (block-backed, and the backing of both / and /home on this host) (placement device-root) — a fresh ledger
  built under it pays this device's journal commits, shared with every other writer on it, so a loaded host
  costs this suite more wall clock than its budget assumes. the memory-backed candidate(s) /dev/shm
  (935.4 MiB free, below the 1.0 GiB floor) were declined for lack of room (floor 1.0 GiB). ...
```

`run-1791789379-784665.json` 同一份流里，五条放置**全部**是
`placement memory-base, 1.1 GiB free` —— 这就是 86%/1.0 GiB 边界上「一过线就全好、
一掉线就全坏」的抖振。

## 2. 残留的归属与存活持有者

```
$ ls -1 /dev/shm | wc -l                       # 413
$ find /dev/shm -maxdepth 1 -type d | wc -l    # 290
$ find /dev/shm -maxdepth 1 -mindepth 1 -name 'suite-*' -type d | wc -l   # 209（实测时点；分钟内变动 ±5）
$ du -sch /dev/shm/suite-* | tail -1           # 3.2G total
$ du -sh /dev/shm                              # 6.3G
```

按名字族的分布（`ls /dev/shm` 归一化尾部后计数）：

```
112  suite-tools-run-<rand>
 84  suite-cmd-agent-run-<rand>
  6  suite-standards-run-<rand>
  4  suite-internal-testutil-run-<rand>
  …  其余为 gopi-*/gocache-*/probe-*/与外来名字
```

**持有者判定（实测，不是推断）**：

```
$ for p in /proc/[0-9]*; do c=$(readlink $p/cwd); case "$c" in /dev/shm/*) echo $p $c;; esac; done
                                         # 空 —— 没有任何进程 cwd 在 /dev/shm 下
$ find /dev/shm -maxdepth 1 -name 'suite-*' -type d -exec test -e '{}/.holder' \; -print | wc -l
197                                      # 197/213 个根带着 .holder 注册
$ <统计 .holder 里 pid 已死的根>          # roots=211  holder-names-dead-pid=194
```

`.holder` 逐字（一个 6h 前死掉的根的注册）：

```
v=2
pid=2269156
start=375363453
cmd=/dev/shm/suite-tools-run-3240957619/TestSharedSessionHolderProbe900823507/001/gopi -test.run=TestHelperSharedSessionCwdHolder
```

⇒ 这些根**没有活的持有者**。它们之所以没被收，不是因为 holder 规则拦着，
而是因为**唯一的自动回收动作只问 suite-run 一族、且那一族在 6h 窗口内**（§3）。

### 2.1 年龄分布：这一族永远贴着 6h 天花板

连续两次实测（横跨本次会话），`/dev/shm/suite-*` 的年龄分布**每次都在 6h 处断掉**：

```
（第一次）
 0.00h  10   1.00h  12   2.00h   6   3.00h   7   4.00h   9   5.00h   5
 0.25h   4   1.25h  20   2.25h  12   3.25h   6   4.25h   8   5.25h  13
 0.50h   9   1.75h   2   2.50h   6   3.50h  13   4.50h  11   5.50h   7
 0.75h  23                2.75h   8   3.75h   7   4.75h   2   5.75h  11
（第二次）
 0.00h   3   1.00h  26   2.00h  12   3.00h   7   4.00h  13   5.00h  27
 0.25h   7   1.25h  14   2.25h   8   3.25h   4   4.25h   6   5.25h   3
 0.50h   8   1.50h   3   2.50h  12   3.50h   2   4.50h   8   5.50h   3
 0.75h  14   1.75h   2   2.75h   6   3.75h   8   4.75h   8   5.75h   4
```

```
$ find /dev/shm -maxdepth 1 -name 'suite-*' -type d -mmin +360 | wc -l
0                                        # 两次实测都是 0：这一族里没有任何一件超过 6h
$ find /dev/shm -maxdepth 1 -mindepth 1 -mtime +24 | wc -l
162                                      # 但整个 /dev/shm 有 162 件超过 24 小时
$ find /dev/shm -maxdepth 1 -mindepth 1 -mtime +7 | wc -l
145                                      # 145 件超过 7 天（本次会话内最老一件 26 天）
```

⇒ **「26 天没回收」的说法要修正**：`suite-*` 这一族其实被回收得很勤，
只是它**永远在 6h 之内**（因为回收器只收 >6h，且这一族的新陈代谢极快），
所以它的可收集合**恒空**。真正 26 天没动的是 **别的名字** —— 见 §2.3。

### 2.2 回收确实在发生：420 份 test-budget 流里的逐条计数

扫 `~/workspace/gopi-test-scratch/queue/test-budget/run-*.json`（420 份）：

```
reclaim 行总数: 73
其中 reclaimed>0: 8
最近 12 条:
 10-12 13:49  reclaimed=0  kept=1
 10-12 13:59  reclaimed=1  kept=0
 10-12 14:41  reclaimed=0  kept=1   ← ×3（standards/tools/cmd-agent 各一次，全部落 ext4）
 10-12 15:26  reclaimed=0/0/0/1/0  kept=4/3/3/1/2
```

⇒ reclaim 步**经常被调用、偶尔成功** —— 它不是没接线，是**覆盖率不足**。

## 3. 代码定位（file:line）

| 事实 | 位置 |
|---|---|
| 内存放置的下限常量 `MinMemoryPlacementFreeBytes = 1 << 30` | `internal/scratch/tmproot.go:252` |
| 同一常量的 suite 侧真别名（Rule 14） | `internal/testutil/scratch_placement.go:68` |
| 饱和判定 + 先回收再放弃 | `internal/testutil/scratch_placement.go:249`（`memoryPlacementCandidate`） |
| **回收步本体（本次修复点）** | `internal/testutil/scratch_placement.go:214`（`reclaimSaturatedCandidate` → `sweepSaturatedMemoryBase`） |
| 显式 suite-run 面（唯一被问的家族） | `internal/scratch/base_suite_runs.go:257` `SweepBaseSuiteRuns` |
| 基座命名空间面（gopi-/gocache-/probe-） | `internal/scratch/base_residue.go:1159` `SweepBase` |
| suite-run 折进 SweepBase 的**唯一**调用点 | `internal/scratch/base_residue.go:1198` |
| 陈旧窗口默认 6h | `internal/scratch/gc.go:79` `DefaultStaleAge = 6 * time.Hour` |
| 窗口的**唯一**读者 | `internal/scratch/gc.go:173` `ResolveStaleAge` |
| legacy `gopi-suite-tmp-*` 面（每个 pin 都跑） | `internal/testutil/legacy_residue.go:139` `GCLegacyScratchResidueOlderThan` |
| 常驻 tick：**只报告不删**该家族 | `cmd/agent/scratch_gc_tick.go:169-186`（`BaseSuiteRunReport`），删除面 `:191` 走 `SweepBase` |
| 常驻 tick 的 stand-down（suite 内自动面关闭） | `internal/scratch/base_residue.go:442` + `internal/testutil/scratchroot.go:492` |
| 那条 `scratch-gc base` 的 CLI 面 | `cmd/agent/cli_scratch_gc.go:1096` `runScratchGCBase` |

### 3.1 三个面各自的名字与触发

| sweep 面 | 命名空间 | 谁触发 | 对 `/dev/shm/suite-*` 的行为 |
|---|---|---|---|
| `SweepBase`（自动） | `gopi-` / `gocache-` / `probe-` | 常驻 worker/merger tick（60 分钟节流） | **拒绝**（`autoBaseResidueEnabled && nameIsDefaultBaseSuiteRun` → false，`base_residue.go:532`） |
| `SweepBase`（显式） | 同上 + suite-run 折进 | `gopi tool scratch-gc base`（CLI one-shot） | 收（age 6h + 无活 holder） |
| `SweepBaseSuiteRuns` | `suite-<name>-run-<generated>` | placement 的 reclaim-before-decline + 上面那个 CLI 面 | 收（age 6h + 无活 holder） |
| legacy 面 | `gopi-suite-tmp-*` | **每个** `PinSuiteTempRoot`（即每次 `go test`） | 不匹配（名字不同） |
| `GCSweepHome`（home 面） | 走 scratch HOME 树 | 常驻 tick + 每个 pin | 不匹配（不在 HOME 里） |

## 4. 根因链（5-why，每一环都带当日实测）

**症状** `/dev/shm` 停在 86%（可用 ≈ 1.06 GiB），一批 suite 全判 device arm、
退回 ext4，每个 fresh ledger 首开付该设备 journal commit。

1. **为什么落在 ext4？** 放置认为 `/dev/shm`「没有空间」：可用 935.4–1023.3 MiB
   < `MinMemoryPlacementFreeBytes` = 1.0 GiB（实测 `free` 在 935.4 MiB 与 1.1 GiB
   之间抖振，而 1.0 GiB 正好落在带子中间 ⇒ 同一批 suite 一会儿全 tmpfs、一会儿全 ext4）。
2. **为什么它没有空间？** 基座里有 2.5 GiB 的顶层残留：213 个 `suite-*`（3.2 GB 量级，
   实测 3.14 GiB）+ 一批 9–26 天的老目录与文件（162 件 >24h、145 件 >7d）。
3. **为什么这些还没被收？** 因为**六个回收面在饱和此刻的可收集合都接近空**，
   而这**不是接线问题**：实测 `gopi tool scratch-gc base --dry-run` → `removed=0`，
   只读探针 `BaseResiduePlan(base, 6h)` → `0` 条（arm 开与关都一样），
   `suite-*` 里 >6h 的 **0 件**。
4. **为什么「没得收」？** 因为基座里占位的东西**按名字就被拒**，不是「超龄未收」：
   - `gopi-test-scratch`（386 MB）命中 `IsBaseResidueName` 里
     `if name == RootDirName { return false }`（`base_residue.go:515`）；
   - `gopi-ctl-NYRf`(131M) / `gopi-sib`(118M) / `gopi-red`(80M) / `gopi-scratch-m`(67M)
     尾部不是 4+ 位数字或 8+ 位 hex ⇒ `baseResidueSuffixGenerated` 拒；
   - `master-check`(116M) / `slice7-master-proof`(117M) / `probe1.db`(139M) /
     `pct-ledger.db`(232M) 连前缀都不匹配。
   同一时刻被拒的超龄顶层目录**看得见**：`find /dev/shm -maxdepth 1 -mmin +360 -type d`
   一次列出 20+ 条，`--dry-run` 一条都不列 —— **predicate 与文件系统直接对不上**。
5. **为什么没人发现「对不上」？** 因为**没有任何一面的职责是「把基座拉回下限以上」**：
   每一面都是「把某个**名字家族**里超龄的收掉」。加上常驻面早就只报告不删
   （2026-10-01 的刻意修正，理由正确：常驻进程分不清别人的活 suite），
   而唯一的显式面没有任何自动调用者（`scripts/scratch-gc-watchdog.sh` 里
   `grep -c base` = **0**），于是「回收器说没得收」与「基座占满」这两句话
   可以同时长期为真，谁也不会把它俩对上。

**一句话根因**：回收是**按名字圈定家族**的，而基座里占位的东西一半是**外来名字**
（碰不得）、一半是**该被重新归类的老名字**；回收器正确地说「我没有可收的」，
而没有任何东西负责回答「那为什么可用空间还是 1.06 GiB？」。

### 4.1 为什么是 26 天（而不是这次才出现）

- `SweepBaseSuiteRuns` 诞生于 2026-09-28（`2997f3c97`），reclaim-before-decline
  与它同时；
- 2026-10-01（`08bff27bf`）把 suite-run 家族从常驻 tick 的**删除面**撤下、
  改成只报告 —— 动机正确（常驻面分不清「别人的活 suite」），
  但**撤掉的那份删除量没有转移到别处**：剩下的自动删除者只有 placement 的
  reclaim 步，而那一步的覆盖就是上文的单家族；
- 于是 10-01 之后，「6h 以上被清掉」这件事只由一条每次饱和才跑、每次只收 1-2 件
  的路径承担，而基座在 14 小时工作日的写入速度远大于此 —— 26 天里它一直停在高位，
  因为**没有任何一面的职责是「把基座拉回下限以上」**，只有「把某个家族里超龄的收掉」。

## 5. 已存在的回收能力与它们的实际覆盖面（实测）

六个面在**饱和那一刻**（本次实测）的可收集合：

| sweep 面 | 命名空间 | 谁触发 | 饱和此刻可收 | 证据 |
|---|---|---|---|---|
| `SweepBase` 命名空间 walk（自动臂） | `gopi-`/`gocache-`/`probe-`，**尾部须生成式** | 常驻 worker/merger tick | 0 | `BaseResiduePlan` 探针 = 0（arm 关） |
| `SweepBase` 命名空间 walk（显式臂） | 同上 | `gopi tool scratch-gc base` | 0 | `--dry-run` → `removed=0` |
| `SweepBaseSuiteRuns` | `suite-<name>-run-<generated>` | placement reclaim + 上面的 CLI 面 | 0（>6h 的同类 0 件） | `find -mmin +360` = 0 |
| legacy `gopi-suite-tmp-*` | 只认该前缀 | 每个 `PinSuiteTempRoot` | 0（本机无此类） | `GCStaleScratchRoot()` = 0 |
| `GCSweepHome`（HOME 面） | HOME 顶层含 `-run-`/legacy/queue 标记 | 常驻 tick + 每个 pin | n/a（不在基座） | — |
| `BaseSuiteRunReport`（只报告） | `suite-*` | 常驻 tick | 不删 | `worker.log:683` |

**两条可执行的结论**：

1. **覆盖面本身不是「没接线」**：`--dry-run` 与 `--explain` 都跑通、谓词与文件系统
   一致（§6 的窗口敏感性验证：`STALE_MIN=180` 时谓词命中 105 件，
   与 `find -mmin +180` 的 105 件**逐件一致**）。基座收不动是因为
   **被拒的名字比可收的名字多得多**。
2. **唯一的显式删除面没有任何自动调用者**：
   `scripts/scratch-gc-watchdog.sh` 里 `grep -c base` = **0**（只跑 home 面与 `trees` 面）；
   本机的 systemd user timer 只有 `gopi-home-gc.timer`（每周一次，跑
   `/home/user/.local/bin/gopi-home-gc.sh`）。

## 6. 最小修复 + 验证

### 修复：让饱和时刻那条路径「问全所有声称‘这是我的、且已陈旧’的面」

`internal/testutil/scratch_placement.go`：

```go
func sweepSaturatedMemoryBase(dir string) {
	olderThan := scratch.ResolveStaleAge()
	baseRemoved, baseErr := scratch.SweepBase(dir, olderThan)              // 面 1+2
	legacyRemoved, legacyErr := GCLegacyScratchResidueOlderThan(olderThan) // 面 3
	…
}
```

- 零新依赖、零新脚本、零新 resident（Rule 35）：挂的全是**已存在**的 sweep 面。
- 触发点不变（仍是 placement 的 reclaim-before-decline），只是这一次它问全了。
- 两个错误分别报告（Rule 15b），互不取消；判定权仍归各自的 age + live-holder 规则。
- **诚实的天花板（必须写在代码旁边，也写在这里）**：这条路径跑在 suite 进程内，
  而 `PinSuiteForTestMain` 调过 `scratch.SetAutoBaseResidue(false)`
  （`internal/testutil/scratchroot.go:492`），所以 `SweepBase` 的**命名空间臂**
  在这个调用点会被 stand-down 拦掉 —— 实测：饱和此刻
  `BaseResiduePlan` 在 arm 开与关**两种状态下的可收集合都是 0**，
  所以这条修复在本机当前状态下**不会**多收一个字节。
  它修的是一个**真实的覆盖不对称**（同一份基座，CLI 面能收的家族多于 reclaim 步肯问的家族），
  而不是本机这一次的饱和。本机这一次的饱和由 §7.5 的「拒收项」负责，那需要人裁定。

### 回归测试（红→绿已实测）

`internal/testutil/scratch_placement_test.go`：
`TestScratchPlacementReclaimsEveryOwnerFamilyBeforeDeclining`

两半：① 饱和时 reclaim **被调用**（走 `memoryReclaim` 测量缝，不碰真 tmpfs，Rule 27）；
② 对 fixture 基座跑真 reclaim：**陈旧的 `gopi-verify-<taskid>` 必须被收**、
**6h 内的 `suite-*-run-*` 必须留下**。

RED（把函数体换回只调 `SweepBaseSuiteRuns` 的旧版）：

```
--- FAIL: TestScratchPlacementReclaimsEveryOwnerFamilyBeforeDeclining (1.03s)
    scratch_placement_test.go:333: /dev/shm/suite-internal-testutil-run-2630741823/.../gopi-verify-01a0a387
    survived the reclaim (lstat err = <nil>) — this is the family the old body never asked about: the base was
    saturated, the suite-run arm had nothing stale, and the reclaim collected nothing
FAIL	github.com/nick/gopi/internal/testutil	8.658s
```

GREEN（修复版）：

```
$ go test -count=1 -timeout 600s -run 'TestScratchPlacement' ./internal/testutil/
ok  	github.com/nick/gopi/internal/testutil	15.530s
```

**测试自身的诚实说明**：② 之所以能红，是因为 fixture 用
`t.Setenv(scratch.MemoryBaseEnvName, tmp)` 把基座指到一个**已配置**的目录；
`IsBaseResidueName` 对**调用者配置过的**基座保留自动臂（`base_residue.go:528-531`：
"a base the caller CONFIGURED keeps the arm in both modes"），
所以那条路径在生产里成立（`ResolveBase` 读了 `GOPI_SCRATCH_BASE` 时），
而**不**覆盖「默认基座 + suite 内 stand-down」这一格 —— 上面已经写明。

### 真实动词（Rule 31，真实状态、真实参数）

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ ./bin/gopi tool scratch-gc base --dry-run
scratch-gc role=cli-base base=/dev/shm removed=2 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/suite-tools-run-2643567442 age=6h0m8s
would-remove /dev/shm/suite-tools-run-2643567442 age=6h0m6s

$ ./bin/gopi tool scratch-gc base            # 真跑（唯一会删 suite-run 一族的显式面）
scratch: base suite root /dev/shm/suite-cmd-agent-run-3106421222 kept — a live process holds it (/proc cwd/fd)
scratch: base suite root /dev/shm/suite-scratch-run-225355297 kept — a live process holds it (/proc cwd/fd)
scratch-gc role=base-suite-runs removed=1 kept-live=2 stale=6h0m0s
scratch-gc role=cli-base base=/dev/shm removed=1 stale=6h0m0s disabled=false
```

⇒ 该动词在真状态上跑通：**超龄的收、活着的按名字与理由报告保留**（`/proc cwd/fd`）。

### 窗口敏感性（一行命令复现「这一族的可收集合为什么是空的」）

```
$ gopi tool scratch-gc base --dry-run
scratch-gc role=cli-base base=/dev/shm removed=0  stale=6h0m0s   # ← 真实 6h 窗口：可收 0
$ GOPI_TEST_SCRATCH_STALE_MIN=180 gopi tool scratch-gc base --dry-run
scratch-gc role=cli-base base=/dev/shm removed=206 stale=3h0m0s  # ← 同一份基座，面是活的
$ find /dev/shm -maxdepth 1 -name 'suite-*' -type d -mmin +360 | wc -l     → 0
$ find /dev/shm -maxdepth 1 -name 'suite-*' -type d -mmin +180 | wc -l     → 96…105（逐分钟变动）
```

⇒ 谓词本身没有 bug（`--dry-run` 在 180m 下命中数与 `find` 逐件一致，§6 的探针另有
`flagged-but-fresh: 0` 的交叉验证）；**6h 窗口下这一族的可收集合本来就是空的**。

## 7. 发现但**未修**的问题（每条都有当日实测；按 Rule 41 提行）

### 2.3 真正 26 天没动的，是「被按名字拒收」的那一批 —— 2.42 GiB

```
$ <comm -23 全部顶层条目, --explain 的 in-scope 名单>   # 仅列 >=50M
  386M  2026-09-26  /dev/shm/gopi-test-scratch     ← 命中 IsBaseResidueName 的字面拒绝
  232M  2026-10-12  /dev/shm/pct-ledger.db
  139M  2026-09-27  /dev/shm/probe1.db
  131M  2026-10-06  /dev/shm/gopi-ctl-NYRf         ← 尾部 NYRf 非生成式
  118M  2026-09-27  /dev/shm/prefix-src-928921
  118M  2026-09-27  /dev/shm/gopi-sib              ← 裸家族名（无 token）
  118M  2026-09-27  /dev/shm/gatecheck-01a0e074
  117M  2026-09-27  /dev/shm/slice7-master-proof
  116M  2026-09-27  /dev/shm/master-check
  116M  2026-09-27  /dev/shm/docanchor-master-check
   80M  2026-09-27  /dev/shm/gopi-red
   67M  2026-09-17  /dev/shm/gopi-scratch-m
   63M  2026-10-12  /dev/shm/gopi-pct-final
   63M  2026-10-12  /dev/shm/gopi-pct
   62M  2026-10-07  /dev/shm/gopi-base
   55M  2026-09-17  /dev/shm/gopi-scratch-accm
  ---- 合计（含未列出的 <50M 项）2422 MiB
```

**这批东西的归属是「人的裁定」，不是 sweep 该猜的**（`/dev/shm` 是 1777 共享目录，
Rule 34 同形：放宽名字判据 = 去删别人的目录）。但**至少两个是明确的死副本**：

- `/dev/shm/gopi-test-scratch`（386M，2026-09-26）：`readlink -f` 实测
  `/home/user/workspace/gopi-test-scratch` **不是**软链（真 HOME 在设备上），
  所以它是一条**旧布局的死副本**。`IsBaseResidueName` 对它的拒绝是**对的**
  （软链布局下它就是活树），但在本机它已经是垃圾。
- `/dev/shm/probe1.db`（139M，2026-09-27）/ `pct-ledger.db`（232M）：
  名字不带任何 gopi 前缀，无从判定。

### 7.0 三次被我自己的实测推翻的假设（记录在此以免后人重走 —— Rule 41）

我先后**推断**过两件事，都被实测推翻，都在报告里改口：

**（a）「pin 时刻的 HOME GC 漏掉了 `suite-*`」** —— 我读到 `internal/scratch/gc.go:200`
的注释就推断命名空间没覆盖。实测推翻：`IsScratchEntry` 用的是
`strings.Contains(name, SuiteRunMarker)`（`gc.go:217`），HOME 树顶层的 `suite-*`
**在**它的名单里；三个幸存者全部 <6h（1M 大小），属于「正确地不收」。
同一份流也证明它在动：`testutil: scratch GC removed 1 stale entry older than 6h0m0s`。

**（b）「四个常驻 worker 的 stdout 指向已经死掉的 pty」** —— 我看 pid 是早上启的
就推断 pty 已死。实测推翻：`gnome-terminal-server`（pid 2809144，9 天前启动）
持有 `/dev/ptmx`，四个 worker 各跑在一个终端标签页里，pts **是活的**。
修正后的事实见 §7.3。

**（c）「`SweepBase` 的命名空间臂能收一批」** —— 我先写进 §0/§4/§6 当成修复的收益。
只读探针推翻：`scratch.BaseResiduePlan(base, 6h)` 在 **arm 开与关两种状态下都是 0 条**，
因为基座里那一族的名字**全部不符合生成式判据**。两小时后 `--dry-run` 显示
当时那 4 件（`gopi-ab-delegated-2741` 等）是**恰好越过 6h** 才出现的，
现在已全部 GONE（`ls -d /dev/shm/gopi-ab-delegated-2741` → GONE），
`--dry-run` 回到 `removed=0`。§0/§4/§6 已按此改写。

```
$ scratch.BaseResiduePlan(ResolveBase(), 6h)  探针（只读，不删）
PROBE standdown-active face1=0 err=<nil>
PROBE armed face1=0 err=<nil>
PROBE face3 legacy-removable=0 err=<nil>
PROBE window=6h0m0s base=/dev/shm
```

### 7.2 常驻 tick 对 suite-run 家族的 report 会漏报「整族都在窗口内」的饱和态

`scratch_gc_tick.go:169` 的 report 只在 `autoNow+kept+fresh > 0` 时打印，
且它只统计 suite-run 一族。基座里 2.4 GiB 的 `gopi-*` 残留**从来不出现在任何一行
worker 日志里**（实测：`worker.log` 全文 5 条 scratch-gc 行，无一条提到它们）。

### 7.3 常驻 tick 的报告没有自己的**持久**目的地（它只写 stdout）

实测四个常驻 worker 的 stdout：

```
pid 1004143 gopi worker  →  /dev/pts/4   started 05:52:47
pid 1010190 gopi worker  →  /dev/pts/5   started 05:54:43
pid 3656808 gopi worker  →  /dev/pts/3   started (10-11) 21:31:40
pid 3896410 gopi worker  →  /dev/pts/0   started (10-11) 23:02:56
```

这些 pts **是活的**（一个 9 天前启动的 `gnome-terminal-server` pid 2809144
持有 `/dev/ptmx`，四个 worker 各自跑在一个 gnome-terminal 标签页里）——
**所以「指向死 pty」这个我一开始的猜测同样是错的**（Rule 41：推断不得当判据，
实测推翻即改口）。

修正后的实测事实是：`scratch_gc_tick.go` 的 report 走 `fmt.Printf`（stdout，行 140/182/201/237），
只有**错误**路径才走 `os.Stderr`。而 stdout 是**启动它的那个终端**：

- 有人盯着那个标签页时它可见，没人盯时**它不落到任何文件**；
- `~/.gopi/logs/worker.log` 里那 5 条 scratch-gc 行（`worker.log:586,681,683,684,685`）
  是**启动时的重定向**留下的 —— 也就是说，这份日志有没有内容，取决于谁启动、
  怎么启动，**不取决于 gopi 自己**；
- 结果：本任务要回答的「为什么 26 天没人发现基座饱和」，
  答案里有一条是**这个报告没有持久目的地**（`logs/worker.log` 末条 15:14:01 之后，
  15:36 还在跑的 worker 没有再写进任何日志文件）。

### 7.4 `scripts/scratch-gc-watchdog.sh` 从不调用 `base` 面

```
$ grep -c base scripts/scratch-gc-watchdog.sh
0
```

它是 host 级 watchdog，只跑 home 面与 `trees` 面。`base` 面（唯一会收 suite-run 的
显式面）**没有任何自动调用者**。不修：本机没有对应的 systemd timer
（只有 `gopi-home-gc.timer`，每周一次跑 `/home/user/.local/bin/gopi-home-gc.sh`），
而按 Rule 35，为它新装一个 timer 是「围着 gopi 加东西」，不是修 gopi。

## 8. 未做的事（诚实清单）

- **没有真收 `/dev/shm` 的存量**。本任务只读诊断 + 最小修复；2.42 GiB 的
  「被按名字拒收」大件（含 386 MB 的死副本）需要人判（§2.3），
  权威清理动词是 `gopi tool scratch-gc base`（读法面 `--dry-run`/`--explain`）。
- **没有把常驻 tick 的删除权限还给 suite-run 家族**。2026-10-01 的撤下是对的
  （常驻面分不清别人的活 suite），本任务保持它；改的是**饱和时刻那一条显式路径**的覆盖面。
- **没有改 `MinMemoryPlacementFreeBytes`（1.0 GiB）**。它是被测量选出来的
  （`tmproot.go:231` 注释：单次 run 的根 57/88/297 MiB 实测），
  调它等于把「空间不足」换成「跑一半 ENOSPC」，本任务不动。
- **没有放宽任何名字判据**（`IsBaseResidueName` / `baseResidueSuffixGenerated`）。
  放宽 = 去删 1777 共享目录里别人的东西，这是裁定不是修复。
- **本次的修复在本机当前状态下不会多收一个字节**（§6 已写明原因：
  命名空间臂在 suite 内被 stand-down 拦掉，且那一族的名字此刻全不合判据）。
  它修的是覆盖不对称；**本机这次的饱和不靠它解决**。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test -count=1 -run 'TestScratchPlacement' ./internal/testutil/` → `ok 15.530s`；
  新增回归测试红证明见 §6（旧函数体 → `--- FAIL … survived the reclaim`）
- Ran: `./bin/gopi tool scratch-gc base --dry-run` 与 `./bin/gopi tool scratch-gc base`
  （真实动词，真实 `/dev/shm`，输出见 §6）
- Result: dry-run 列出 2 件超龄；真跑 `removed=1 kept-live=2`，
  被保留的两件**按名字与理由**打印（`a live process holds it (/proc cwd/fd)`）
- Commit: `391d77f8c`
- 诊断证据的命令与原始输出见 §1–§4、§7（每条主张都带当场输出）

### 包级测试的两条 **既存** 失败（不是本次改动引入 —— 实测对照）

`make test-budget PKGS='./internal/testutil ./internal/scratch'` →
`exit=1 outcome=fail`（`run-1791790601-896856.json`），两条失败：

```
--- FAIL: TestScratchPlacementIsMemoryBackedOnThisHost
    scratch_placement_test.go:59: placement "device-root": this suite's root is NOT memory-backed
    although /dev/shm is — a fresh ledger built under the root pays the device's journal commits
--- FAIL: TestScratchPlacementHonoursACallerRootAndReportsADevice
    scratch_placement_test.go:99: a caller TMPDIR on tmpfs (memory filesystem):
    got {memory=false label="device-root" dir=""}, want the memory-root arm
```

**对照实测**（把两个文件换成 `HEAD~1` 的版本，同一个 worktree、同一分钟）：

```
=== TRUE pre-fix (HEAD~1) ===
    scratch_placement_test.go:58: placement "device-root": … NOT memory-backed …
--- FAIL: TestScratchPlacementIsMemoryBackedOnThisHost (0.00s)
    scratch_placement_test.go:98: … got {memory=false label="device-root" dir=""} …
--- FAIL: TestScratchPlacementHonoursACallerRootAndReportsADevice (9.35s)
```

⇒ 修复前后**逐字相同**地失败 ⇒ **这两条失败与本次改动无关，是既存的**，
而且它们**就是本任务的现象本身**：这两条测试断言「本机 `/dev/shm` 有空间，
所以 suite 应该落在内存上」，而 `/dev/shm` 此刻可用 1.06 GiB（下限 1.0 GiB，
抖动时跌破）⇒ 放置正确地判 device-root ⇒ 断言失败。

**这是一条值得单独提行的既有事实**（Rule 41）：**本机在基座饱和期间，
`internal/testutil` 包里永远有两条测试是红的**，它们报的是「placement 没有优先选内存」，
而真正的原因是「内存基座被按名字拒收的残留占满」。一个把症状当缺陷报的测试，
会把注意力从「谁占了基座」引到「placement 逻辑是不是坏了」—— 本次会话里我自己
就先往那个方向走了一段（§7.0 的 (c)）。

本次提交**不动**这两条测试（它们在此刻是**正确地失败**；真正的修法是让人裁定
§2.3 那批名字，或者把断言改成「若基座饱和则是既存配置问题」——两者都不是本任务的范围）。
