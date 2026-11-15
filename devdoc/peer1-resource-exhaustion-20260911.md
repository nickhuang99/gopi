<!-- doc-class: normative -->
# peer1 资源耗尽事故：根因诊断 + 恢复记录

**日期**：2026-09-11 05:47–06:10
**触发**：peer1 全部资源耗尽，hermes 无法响应，the operator 被迫 kill hermes + kill HOMM3
**结果**：/ 分区 1.5G → 19G；load 9.09 → 1.65；根因定位到 4 类孤儿进程 + 3 类磁盘垃圾

---

## 一、根因（按危害排序）

### 1. 无限循环的 dd 压测（磁盘 IO 饱和 —— 这是"系统慢/无法响应"的直接原因）

`/tmp/suite-ab.sh` 是一个**测试套件的 A/B 压测脚本**，它在跑 `go test ./cmd/agent/`
时人为制造 IO 负载：

```bash
for j in 1 2 3 4 5 6; do
  ( while :; do dd if=/dev/zero of=/tmp/load-$j.bin bs=1M count=8 conv=fsync 2>/dev/null || sleep 1; done ) &
done
```

问题：**`while :; do ... done` 是无限循环**，且 `conv=fsync` 强制每次 8MB 都同步落盘。
6 个并行实例 × 每轮 fsync = 磁盘队列彻底饱和。

更致命的是：**脚本被启动了 6 次**（也许是重复派工/重复执行），且**全部成了孤儿**
（PPID=1，04:06:15 启动，已跑 1h45m）。我 kill 掉 6 个宿主 bash 后，dd 立刻消失。

**观测证据**：
- `ps` 显示 6 个 `bash /tmp/suite-ab.sh <worktree> BEFORE`，PPID=1，启动于 04:06:15
- 每个下挂 `dd if=/dev/zero of=/tmp/load-N.bin bs=1M count=8 conv=fsync`（状态 D，不可中断 IO 等待）
- `du -sh /tmp` 直接超时 300s —— IO 已经完全饥饿
- 修复后 `load average: 9.09 → 0.74`

**这正是 the operator 报告"几个 dd zombie 杀不掉"的东西**：D 状态（uninterruptible sleep）
进程对 SIGTERM 无响应，必须 SIGKILL **父进程**才能让子进程停止重生。

### 2. 12.7 天的测试 mock 服务器（8/29 起就活着）

三个进程从 **8/29** 一直跑到现在（1097527s ≈ 12.7 天），全部 `futex_wait_queue_me`：

| PID | 进程 | 启动 | cwd |
|-----|------|------|-----|
| 4034070 | `/tmp/go-build3451724584/b001/exe/dbg_mock` | 8/29 13:09 | `gopi-testsuite` (deleted) |
| 4036850 | `/tmp/go-build3900647893/b001/exe/main` | 8/29 13:21 | `/tmp/gopi-mock-probe` |
| 4037775 | `./mockd` | 8/29 13:23 | — |

这些都是 Go 测试 `go run`/`go test` 编译出的临时二进制（在 `/tmp/go-build*` 里），
测试结束后**没有被清理**，cwd 指向的目录早已被删除（`(deleted)`）却仍持有原目录的
inode，导致空间无法回收。它们是**孤儿**（PPID=1），无任何监控发现。

同类还有一个 5.2 小时的僵尸 grep：
- PID 3538618 `grep -rln sha256_16 /home/user/work /home/user/workspace`，D 状态，cwd 已删除

### 3. 磁盘垃圾（19G /tmp + 9.5G snapd 旧版本）

| 项目 | 大小 | 来源 |
|------|------|------|
| `/tmp/go-build*` | 6.0G | Go 编译缓存 × 200+ 目录，从没清理 |
| `/tmp/wta-L-*` `wta-load-*` `wtaload_*` | 4.1G | gopi 的 write_task_area 压测残留，每个 201-301M |
| `/tmp/pN_dump_*` `*_verify` | 556M | **13,044 个目录**（Go 测试的 scratch root 产物） |
> **Correction (2026-09-11, scratch-residue-audit)**: 13,044 这个**数量和机制**确实是 Go 测试的
> （pin 之前 scratch root 跟随 `os.TempDir()` → `/tmp`，且只在 TestMain 成功路径清理）；
> 但**今天磁盘上残留的 `pN_dump_*` 名字不是 GoPi 的** —— 抽查 `/tmp/p3_atl_dump/00000.json`
> 是 HoMM3/VCMI 地图解析 dump（`{"file":"/tmp/atl.h3m","mapname":"atl",...}` + `paths.txt`），
> 130 个目录全部是 2026-09-06 07:39–09:25，出自独立的地图工具会话
> （`~/workspace/h3m-deliverable` → `homm3tools/h3mlib`），GoPi 源码与 git 历史里
> 没有 `p3_*`/`paths.txt`/`00000.json`。别再追这个 producer —— 见
> `devdoc/scratch-residue-audit.md` §1。 |
| `/tmp/hippo-*` | 361M | hippocampus 实验残留 × 10 |
| `/tmp/*.db` | 350M | 20 个测试数据库副本 |
| `/tmp/bw*.bin` `mtest` 等杂项 | ~500M | 压测残留 |
| `/var/lib/snapd` | 9.5G | 13 个 disabled 旧 snap revision |
| `/var/cache/apt` | — | 未 clean |

合计回收 **17.5G**（/ 从 1.5G 可用 → 19G 可用）。

### 4. 孤儿 GUI 进程（内存 + display 号占用）

8 个 `Xvfb :NN` 从 HOMM3 / computer-use 实验中遗留下来（:95 :96 :97 :98 :221 :77 等），
每个 20-50MB，且长期占据 display 号。已清理，仅保留 `:99`（computer-use 官方测试屏）。

---

## 二、执行动作

```bash
# 1. 杀死 6 个 suite-ab.sh 孤儿宿主（子 dd 随之消失）
kill -9 3926383 3926384 3926385 3926387 3926389 3926390

# 2. 杀死 12.7 天测试 mock + 僵尸 grep
kill -9 4034070 4036850 4037775 3538618 3538617

# 3. 清理 /tmp 垃圾（17.5G）
rm -rf /tmp/wta-L-* /tmp/wta-load-* /tmp/wtaload_* /tmp/wta-gopi /tmp/load-*.bin
rm -rf /tmp/p[0-9]*_dump_*
rm -rf /tmp/go-build* /tmp/gocache-hippo /tmp/mtest /tmp/bw*.bin
rm -rf /tmp/gopi-job-*.{out,err} /tmp/gopi-contract-bin-* /tmp/gopi-agent-suite-ledger-*
rm -rf /tmp/hippo-* /tmp/*.db /tmp/posfix-scratch /tmp/v_tr.txt

# 4. 清理 snapd 旧版本 + apt
sudo snap remove --revision <rev> <name>   # 13 个 disabled revision
sudo apt clean

# 5. 清理孤儿 Xvfb（保留 :99）
kill -9 1864112 1864122 1868818 2035074 2084444 2084445 3479460 3945772

# 6. 恢复 merger（setsid 常驻，PPID=1）
/tmp/merger-up.sh   # → setsid ./bin/gopi task merger --every 5m
```

---

## 三、恢复后状态

| 指标 | 事故时 | 恢复后 |
|------|--------|--------|
| `/` 可用 | 1.5G (99%) | **19G (87%)** |
| `/tmp` 占用 | 19G | **2.2G** |
| load average | 9.09 | **1.65** |
| 内存可用 | — | **13Gi** |
| D 状态进程 | 10 个 | **0** |
| gopi 进程 | worker 1 + 僵尸若干 | worker 1 + merger 1（均 PPID=1） |

**上一 session 的工作全部找回**（git 分支是权威载体）：
- master tip = `ea56f4a`（merger 恢复后已自动 merge 了 HasSessionEnd 修复）
- 覆盖率 campaign：**416/499 = 83.4%**（夜间从 72.5% 推到 85.5% 峰值）
- 4 个进行中分支：`feat/cov-agent`（5 ahead）、`feat/tab-order-actuate-poc`（3 ahead）
  待收编；`feat/cov-shot-x11`、`feat/webcam-capture-evaluation` **已 MERGED**

---

## 四、教训 / 防止复发

1. **压测脚本绝不能无限循环 + fsync**。`while :; do dd ... conv=fsync; done` 是
   DoS 自己的写法。压测必须有**时长上限**（`timeout 120 dd...`）和**自毁**（trap EXIT
   清子进程）。当前 `/tmp/suite-ab.sh` 已被删除。
2. **测试 scratch 必须落在可回收位置，且测试结束必须自清**。13,044 个
   `pN_dump_*` 目录 = 测试框架（GOPI_TEST_TMPDIR）没有清理钩子。已有的
   `testutil.PinSuiteTempRoot()` 是正确方向，但需要覆盖所有入口 + 加 age-based GC。
   **已闭环 (2026-09-11)**：`8ef6f48` 之后每次 pin 都内联跑两个 GC —— 家目录的 age GC
   加上 memory base 里 legacy `gopi-*-suite-tmp-*` 残渣的命名空间受限清扫
   （580 → 75，0 个超龄）；pin 未生效时 `PinSuiteForTestMain` 直接在跑测试前
   refuse（exit 97）而不是静默落到 `/tmp`。证据见 `devdoc/scratch-residue-audit.md` §2–§3。
3. **长跑 mock server 必须挂到测试生命周期上**。8/29 的三个 mock 是 `go test` 编译的
   临时二进制，测试结束没被 reap。Go 测试应当用 `t.Cleanup` 或显式 PID 文件回收。
4. **周期性资源巡检**：本事故的两个 12.7 天进程、5.2 小时僵尸 grep、6 个孤儿压测，
   **没有任何监控发现它们**。需要一个轻量 watchdog（检查 PPID=1 的 gopi/go-build
   孤儿 + D 状态超时进程 + /tmp scratch age），零 token 的 bash 脚本即可。
5. **`/` 分区只有 147G 而 /home 有 766G** —— 一切临时产物必须走 `/home` 或
   `~/workspace`，绝不落到 `/tmp`（在 `/` 上）。这条已在 memory 中，但测试框架没遵守。

---

## 五、待办

- [ ] 派任务：给 `testutil.PinSuiteTempRoot()` 加 age-based GC + 覆盖所有测试入口
- [ ] 派任务：`internal/testutil` 加 mock-process reaper（测试结束杀自己起的 mock）
- [ ] 派任务：写 `/tmp` scratch watchdog（PPID=1 孤儿 + D-state 超时 + age 清理）
- [ ] 收编 `feat/cov-agent`（5 ahead）与 `feat/tab-order-actuate-poc`（3 ahead）
