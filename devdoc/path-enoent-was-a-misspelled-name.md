<!-- doc-class: historical -->
# 绝对路径 stat 报 ENOENT 而对象仍在 —— 实测根因：操作数名字少了一个连字符

**结论先行**：这不是文件系统故障，不是内核名字查找异常，也不是间歇性问题。上一 run
（`01a17ff1`）报告的「readdir 列得出、绝对路径 stat 全 ENOENT、dir_fd 却成功、
时好时坏 20 分钟」，真因是**两个探针各自写了不同的名字字面量**：成功的那个是 canonical
36 字符（4 个连字符），报 741/741 失败的那个**掉了第 4 个连字符**（35 字符，3 个连字符）。
错名是**确定性** ENOENT —— 普通的"无此条目"，不是"这个条目现在找不到了"。

这是 memory `#11040` 的**成员 B**（hyphenated 但掉一个连字符）。该条记录当时就写清了
判据，而上一 run 没有走它：它先怀疑了文件系统，然后把 `dmesg` 里大量的 nvme
`Correctable` 错误当成了候选病因（并在记忆里以"未证实的同窗口因素"记录）。本 run
逐字符取证把它翻了过来。

## 1. 决定性的一次对比

从上一 run 自己的 session jsonl 里，把它**真正传下去的字面量**逐字取出来，而不是读
tool_result 里的回显（回显里两种拼写混在一起，会把水搅浑）：

```
call_00_zJHLVsTLwDWeHfMjiIRr5844  (dir_fd/abs 并排探针, 报成功)
    n='01a17ff1-61fb-79f7-ad41-d20f078e97cb'   len=36 dashes=4  == canonical

call_00_FI1L3oaYuDbEGmAH1ULh6694  (15s 轮询, 报 741/741 ENOENT)
    n='01a17ff1-61fb-79f7-ad41d20f078e97cb'   len=35 dashes=3  != canonical
```

两个探针打的**不是同一个名字**。所以"dir_fd 成功而绝对路径失败"不是内核通道分歧，
是两次不同的输入。

本 run 用**同一个错名**同时打四条通道，四条全 ENOENT（`evidence/03-live-probe.txt`）：

```
-- 负对照 01a17ff1-61fb-79f7-ad41d20f078e97cb   len=35 dashes=3
  readdir : NOT LISTED
  statx   : No such file or directory
  py-abs  : FileNotFoundError [Errno 2]
  dir_fd  : FileNotFoundError [Errno 2]   ← 关键：dir_fd 也失败，它没有"绕过"任何东西
```

正对照（本 run 自己的 canonical 目录）四通道全成功，且 inode 一致（`5245221`）。
再抽 200 个 canonical 目录做 `dir_fd` 与绝对路径的 inode 交叉核对：**2895 个目录中采样
200，mismatch/error = 0**。名字查找通道是健康的。

## 2. 一条判据，比 grep 快得多

```
len(name) == 36 且 name.count('-') == 4   → canonical
len − 连字符数 == 32（但连字符不是 4 个） → uuid 的 16 字节被写错了分组 = 成员 A/B
```

本次实测：36/4 = 2895 个；35/3 = 3 个；35/4 = 2 个；32/0 = 30 个。
**光是"len − dashes = 32"这一条**，就能在任何一次 ENOENT 上先判掉拼写问题，
不需要先看 dmesg。

## 3. 危害是真的，但机制要说准

错名不是无害的疏漏 —— 它会被**真正铸造出来**。`write` 工具的父目录处理是
`os.MkdirAll(filepath.Dir(path), 0o755)`（`internal/tools/write.go:191`），
而 `write` / `edit` / `bash` 三条工具路径**都不经过** `sessionid.IsAdmissible`：

```
$ grep -rn 'IsAdmissible' internal/tools/write.go internal/tools/edit.go internal/tools/bash.go
  (无输出)
```

所以一个错拼写的 `$GOPI_TASK_REPORT_PATH` 或证据路径，会被 `mkdir -p` 成一个
**孤儿的第二容器**。历史物证三个，**每一个都有一个已存在的 canonical 双胞胎**：

| 错名（35/3） | canonical（36/4，存在于磁盘） | btime | 孤儿目录里丢了什么 |
|---|---|---|---|
| `01a12cbe-151c-7094-aa00f94cf98e622b` | `01a12cbe-151c-7094-aa00-f94cf98e622b` | 2026-10-12 05:21:38 | 32 个 `artifacts/` 证据文件 |
| `01a12e11-a972-7a2d-991c9e54e24260de` | `01a12e11-a972-7a2d-991c-9e54e24260de` | 2026-10-12 11:35:03 | `task-report.md` + 6 个 `artifacts/` |
| `01a142d1-699e-7027bce1-1a4b7c85f76f` | `01a142d1-699e-7027-bce1-1a4b7c85f76f` | 2026-10-16 18:28:05 | `task-report.md` |

**后果是"交付物写进了孤儿目录、`$GOPI_TASK_REPORT_PATH` 上什么都没有"**，
不是上一 run 记忆里写的"close-out 把 known 读成 absent"。

这一点必须说准，因为 close-out 那边的判据**本来就是对的**：它不 `stat`，它读 git 自己的
chdir 拒绝 —— `finishCleanupMissingDirReason`（`internal/tools/task_finish_cleanup.go:6423`）
匹配 `"chdir "` / `"cannot change to"` 这一对 marker（`:988` / `:989`），
`finishCleanupWorktreeDirAbsent`（`:6432`）复用它，注释明写
"one reader, so the two can never disagree about what gone means"。

**Row 记录的 worktree 路径来自 ledger（canonical 拼法），不来自一次转写**，所以正常路径下
它对错名免疫。真正的暴露点只有一处：如果某个 run 把**错拼写的路径**写进了
`deliverable.worktree`（提交时传什么就记什么），close-out 会拿到一个真的不存在的路径、
git 真的 chdir 失败、判据**正确地**说"不在" —— 它没有误判，是输入的路径本身错了。
这是"上游写坏了"，不是"下游读错了"。两种归因的修复完全不同，所以必须分开写。

## 4. 宿主环境已被排除，且排除方式是"按盘归属"而不是"看有没有错误"

上一 run 把 dmesg 里的 nvme `Correctable` 错误留成了候选病因。按 PCI 地址归属一次就排掉了：

```
AER 报的是 PCI 10000:e2:00.0     = nvme0n1 = "/" 所在盘
workspace 在 PCI 10000:e1:00.0   = nvme1n1（/home/user/workspace 的挂载设备）
```

| 计数（`journalctl -k`，10-27 起） | 值 |
|---|---|
| `EXT4-fs error|warning` | **0** |
| `I/O error | blk_update_request` | **0** |
| `nvme` reset / timeout / abort | **0** |
| `AER: Correctable` | 111879（**全在根盘 nvme0**） |
| 13:19–13:31 窗口内 `AER: Correctable` | 1081 |

所以"大量 PCIe 错误"与本次 ENOENT **无因果关系** —— 它们报在另一块盘上，而承载
`workspace` 的那块盘零文件系统错误、零 IO 错误。这个区分很重要：只看总数会把
"AER 刷屏"读成病因，而按 PCI 地址归到具体盘，它立刻变成一个无关事实（是一个**独立的、
值得单独提行的**硬件噪声问题）。

## 5. 判别矩阵

八条候选机制各自"成立时该看到的签名"与实测对照，见 `evidence/04-discriminator.txt`：

| 机制 | 成立时的签名 | 实测 | 判定 |
|---|---|---|---|
| (a) 名字转写错（缺一个连字符） | 错名确定性 ENOENT、canonical 成功 | 35/3 vs 36/4，canonical 成功 | **★根因** |
| (b) 内核 dcache/名字查找间歇故障 | canonical 名字也会时好时坏 | canonical 四通道全成功、200/200 inode 一致 | 排除 |
| (c) ext4 / 块层 I/O 错误 | 记录 EXT4-fs error / I/O error | 全天 0 条 | 排除 |
| (d) nvme/PCIe 链路故障 | AER 报在承载该路径的盘上 | AER 全在 nvme0（根盘），workspace 在 nvme1 | 排除 |
| (e) 只读 / 满导致创建失败 | EROFS / ENOSPC，不是 ENOENT | 95% 用、写入探针 OK | 排除 |
| (f) overlay / bind mount 遮蔽 | 同路径多层、挂载 id 变化 | workspace 单层，挂载 269 | 排除 |
| (g) 并发 scratch-gc 删目录 | readdir 也列不出、btime 变新 | readdir 列得出、btime 未变 | 排除 |
| (h) LD_PRELOAD 改写路径 | 变量存在 | `LD_PRELOAD` 未设置 | 排除 |

## 6. gopi 侧已有的正确行为，与真正的缺口

**已有（正确）**：所有走 task-session 缝的路径都先过 `sessionid.IsAdmissible` /
`Refuse`，并在拒绝里**印出 canonical 拼法**：

- `internal/agent/session.go:112`
- `internal/tools/task_session.go:157`
- `internal/tools/tasks.go:3151`（加上 `taskIDNearMissSuffix` 的 "did you mean" 子句）
- `internal/signalrec/signalrec.go:826`

`internal/sessionid/sessionid.go:221` 的 `CanonicalSpellingOf` 是这条规则的唯一实现
（把 uuid 的 16 字节重新分组；不做"多一位十六进制"的猜测，理由写在注释里 —— 那个修复
不可推导，猜出来的 "did you mean" 比没有更糟）。

**缺口**：`write` / `edit` / `bash` 三条工具路径完全不碰 `sessionid`，而
`write` 的 `MkdirAll` 会把任何拼写落成目录。这就是孤儿容器能被铸造的通道 ——
上面那三个历史物证就是这么来的。

## 7. 加固取舍：什么该做，什么不该做

**不该做的三件**（都会把成本转移给未来）：

1. **"stat 失败就重试"** —— 错名是确定性的，重试一千次一样 ENOENT，只是把
   一次明确的失败变成十秒延迟。
2. **内核对名字查找加缓存/绕过层** —— 病因不在内核，没有可修的东西。为它写的任何
   `dir_fd` 兜底读法，是在为一个不存在的故障维持一条代码路径（Rule 35：为不存在的东西
   养调度器）。
3. **在 `write` 里对任意路径做 uuid 猜测修正** —— `write` 是通用工具，一个
   "把 `foo-bar-baz` 重排成 uuid" 的启发式会在正常路径名上误伤。`CanonicalSpellingOf`
   守住"长度对、去掉连字符正好 32 个十六进制"这条，误伤面很小，但把它塞进通用
   `write` 仍然是把 session 一侧的规则泄漏到每一层。

**该做的事，按成本从低到高**：

1. **（最省，零代码）报告路径先核对再写**：在把交付物写到
   `$GOPI_TASK_REPORT_PATH` / 证据路径之前，先与 `os.listdir(parent)` 里的名字逐字符
   比对，或用 `len − dashes == 32` 判一下。上面那三个孤儿目录，每个都是**写之前少一次
   核对**造成的。这一条不需要改任何生产代码，写进规则/记忆就够。
2. **（一行级）`write` 的父目录如果落在 sessions 根下，先过 `sessionid.IsAdmissible`**：
   判据是"父路径的 `filepath.Dir` 等于 `config.GopiDir()/sessions`"，命中才校验，
   其余路径行为不变。这是把**已存在的**门挪到真正铸造目录的那一处，不是新机制。
3. **（需要在 peerbox 上做，Rule 28）上面第 2 条的落地属于 `internal/tools` 系统层**，
   不是 feature 层；应作为一条独立任务提给正确的主机，而不是在本 run 顺手改。

## 8. 我**没有**做的（如实）

- **没有修任何生产代码。** 本 run 的交付是诊断与判别，不是加固。上面第 7 节的第 2 条
  是一个**建议**，不是已落地的改动。
- **没有复现异常。** 异常发生在 2026-10-28 13:19–13:31，现在已不可原地复现；
  本 run 的"活体探测"证明的是**通道当前健康**，与"当时发生了什么"是两个命题。
  把两者混为一谈的写法（"现在能 stat 了所以它自己好了"）会是错的。
- **没有 `smart-log`。** `nvme-cli` 未安装（`command -v nvme` 无输出）。那是诊断需求，
  不是修复手段，所以按 Rule 25 没有去申请权限 —— 这里如实记录拿不到。
- **没有清理那三个孤儿目录。** 它们是证据（且其中一个装着 32 个 `artifacts/` 文件）；
  是否合并/删除应由操作员决定，不是诊断该顺手做的。

## 9. 一条独立的、值得单独提行的观察

`AER: Correctable` 在 10-27 起累计 **111879** 条，全部落在 `nvme0n1`（`10000:e2:00.0`，
根盘）。这是 PCIe 物理层（Receiver ID）的可纠正错误，量级已经足以在 `dmesg` 里刷掉
其他日志 —— 事实上本 run 想回看 13:19–13:31 窗口时，`dmesg` 的环形缓冲里
6604 行几乎全是它。这与本次 ENOENT 无关，但它**掩盖其他内核证据**的能力是真的，
而且它可能预示根盘链路在退化。这是硬件层面的问题，不属于本 epic 的修复范围。
