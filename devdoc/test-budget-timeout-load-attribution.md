<!-- doc-class: normative -->
# test-budget 25m TIMEOUT 的负载归因（诊断）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**测量对象**：`scripts/test-budget.sh ./internal/tools ./cmd/agent`
— run tag `run-1791781222-4099764`，stamp `2026-10-12T13:00:22+08:00 budget=25m
elapsed=1598s load_start=18.84 14.48 13.80 load_end=10.83 26.95 26.71 cores=8
exit=3 outcome=timeout`。

**结论（一句话）**：**不是挂死，是整套被拖慢**——两个包各自把 1500s 预算**跑满**
（完成测试的 Elapsed 之和 1498.1s / 1495.0s），同 scope 在负载 2.3 的热基线是
1328s，膨胀系数 **2.16–2.30x**；代价均匀摊在每个测试上（67% 的 tools 测试比基线慢，
中位数 1.90x），时钟在 25m 到点时仍在**正常推进**（最后一秒还在 PASS/RUN），
预算杀掉的不是卡住的用例，而是**尚未轮到的 25%/37% 的顶层测试**。

**诊断专用**：本 run 未改任何阈值、未改任何测试、未造任何负载（Rule 27）。

### 事后校正：引用点已随修复搬家（`99e2eacac`）

`99e2eacac`（serve-lock holder handshake 预算的修复）重写了
`cmd/agent/cli_serve_test.go`，把这条失败串的 emit 点从重写前的
`waitForFile(t, ready)`（旧 :105）搬进了新的 `waitReady` 方法，
旧文本**已不在该文件中**（实测 `grep -c 'waitForFile(t, ready)' cmd/agent/cli_serve_test.go` → `0`）。
今天这条失败串的唯一 emit 点：

`cmd/agent/cli_serve_test.go:461`

```
$ sed -n 461p cmd/agent/cli_serve_test.go
func (h *holderHandshake) waitReady(sink holderTestSink, path string) time.Duration {
```

同一修复的回归测试在 `cmd/agent/serve_lock_handshake_test.go`
（`TestServeLockHolderHandshake_ReportsAChildThatDiesBeforeReadiness`：子进程在
ready 之前就死 → 当场报子进程自己的状态与 stderr，不再报"从未写出 marker"的宽余量超时）。
§4 与 §7 里的 `cli_serve_test.go:105` 是 **victim run 的原文**，保留不改写 —— 那是当时
真实打印出来的串，改动它等于伪造记录；本行才是引用点的当前落点。

---

## 1. 预算到期时在飞的是什么

`go test -timeout` 是**每包**的墙钟；两包并发，各自独立计时。
两者都在自己的 1500s 处被打断，中间相隔 69s（`cmd/agent` 比 `internal/tools`
晚 70s 开始，因为同一 `go test` 命令串行起包）。

| 包 | 流首事件 | 流末事件 | 跨度 | 到期时在飞的测试 | 该测试开始时刻 |
|---|---|---|---|---|---|
| `internal/tools` | 13:00:47.510 | 13:25:52.416 | 1504.9s | `TestOutOfRootDisposalFreesAnOffRootRegistration` | 13:25:51.154 |
| `cmd/agent` | 13:01:57.316 | 13:27:00.829 | 1503.5s | `TestRunPathStillRunsBarePromptShapes/literal_prompt` | 13:26:57.802 |

到期证据（流内原文）：

```
13:25:51.154151 output '=== RUN   TestOutOfRootDisposalFreesAnOffRootRegistration\n'
13:25:52.277337 output 'panic: test timed out after 25m0s\n'
13:25:52.277417 output '\t\tTestOutOfRootDisposalFreesAnOffRootRegistration (1s)\n'
```

```
13:26:57.802894 output '=== RUN   TestRunPathStillRunsBarePromptShapes\n'
13:26:57.802944 output '=== RUN   TestRunPathStillRunsBarePromptShapes/literal_prompt\n'
13:27:00.752477 output 'panic: test timed out after 25m0s\n'
13:27:00.752547 output '\t\tTestRunPathStillRunsBarePromptShapes (3s)\n'
```

**这两个在飞用例都不是卡住的**：它们在 panic 前 1s / 3s 才刚开始，
是**队列里轮到的那一个**，而不是"停在那里的那一个"。
panic 栈里 `database/sql.(*DB).connectionOpener [select, 21 minutes]` 一类帧是
`database/sql` 的常驻连接开瓶器（每个打开过的 DB 一个），**不是被卡住的调用**——
Rule 4 的教训（`devdoc/cmd-agent-test-hang.md`）在这里再次成立：不要读 panic 栈下结论。

## 2. 量化：膨胀是均匀的，不是单点

同一 scope 的绿热基线：`run-1791742805-235281`，
`2026-10-12T02:20:05 pkg=./internal/tools ./cmd/agent elapsed=1328s
load_start=2.30 1.80 1.77 exit=1`（08–19 核 · 8 核，即 0.29x per-core；
victim 是 18.84 → 2.36x per-core）。

| 包 | 完成测试数（victim / baseline） | Σ Elapsed victim | Σ Elapsed baseline | 聚合膨胀 |
|---|---|---|---|---|
| `internal/tools` | 1766 / 2375 | **1498.1s** | 463.1s | **3.14x** |
| `cmd/agent` | 1622 / 2568 | **1495.0s** | 1279.0s | **1.90x** |
| 两包合计（共有测试集） | 3274 共有 | 2617.6s | 1138.9s | **2.30x** |

`Σ Elapsed` 与各自的 1500s 预算之差只有 1.9s / 5.0s —— **两个包都把预算
用满在"正在做正事"上**，没有一秒钟是花在等一个不返回的调用上。

按包看 per-test 比值（仅 baseline ≥ 0.20s 的测试）：

| 包 | n | 中位数 | 均值 | p90 | victim 更慢的测试占比 |
|---|---|---|---|---|---|
| `internal/tools` | 257 | 1.90x | 2.58x | 4.65x | **67%** (1130/1684) |
| `cmd/agent` | 220 | 1.46x | 2.56x | 5.15x | 46% (724/1590) |

均匀膨胀是**判据本身**：真实挂起会表现为"一个测试吃满全部墙钟、其余正常"，
这里看到的是"几乎每个测试都慢一截，累加起来正好是预算"。

**最慢的完成测试与它自己的安静值**（同 scope 基线，同一天）：

| 测试 | victim | 基线 | 比值 |
|---|---|---|---|
| `tools/TestSharedSessionHolderProbe` | 173.26s | 24.61s | 7.04x |
| `tools/TestMemorySearchOversizedQueryIsCapped` | 104.53s | 32.91s | 3.18x |
| `agent/TestCaptureVerbFailureArmsExitNonZero` | 70.08s | 2.10s | 33.4x |
| `agent/TestMemorySyncE2EBidirectional` | 61.06s | 27.99s | 2.18x |
| `tools/TestFinishCleanupLocalSyncQuotesTheFailingPackage` | 44.98s | 11.95s | 3.76x |
| `agent/TestCodeSearchBackendFindsDefinition` | 12.20s | 0.12s | **101.7x** |

"第一个变慢的东西"不需要单独定罪：**第一个测试就开始变慢**。
`internal/tools` 的前 4 个测试（13:01:03–13:01:46）instant 就是
6.87x / 6.21x / 3.92x / 4.55x —— 也就是说负载在 run 的**第 16 秒**就已经在收税，
一直收到 25m 到点。

## 3. 并发来源（不是"4 个相同 run"）

victim 的 1598s 窗口内，stamps.log 记录了 **23 次运行**，其中：

- `12:56:47 → 13:23:11` `./internal/tools ./cmd/agent` exit=3 **timeout**（同一 scope，重叠 1425s）
- `12:56:51 → 13:17:53` `./internal/tools` exit=1 fail（重叠 1261s）
- 另外 21 次是 `./internal/p/...`（每次 17–57s，密集穿插在 13:08–13:25）

以及 4 个常驻 `gopi worker`（etimes 28311–58494s）。**同 scope 的两条长跑互相
征税**：两份相同的两包套件在同一块设备上同时跑，各自 ~1500s 的预算被对半削。
这不只是"主机整体忙"，而是**同一个 scope 被跑了两次**——两条都属于
`./internal/tools ./cmd/agent`。

## 4. 三个"负载敏感"失败，各一条观察

| # | 测试 | 观察到的输出（原文） | 复跑结果 | 分类 |
|---|---|---|---|---|
| 1 | `tools/TestMemorySearchNonBlocking` (0.10s) | `memory_test.go:590: concurrent store: memory_store: database schema has changed: vtable constructor failed: distilled_qa_fts (17)` | `-count=5` @load 2.44 → **ok 24.1s, 5/5 PASS** | **已登记 flake**，且**结构上就是负载敏感**：`flaketest.Declare` 自述"4 个并发 MemoryStore 抢 fresh ledger 的首次 schema 构建，而那是**一串写事务、每次 commit 一次设备同步**"——负载越重，输家越可能撞上它有界的轮询窗口 |
| 2 | `agent/TestTaskVerbSweepWorktreesRunsAsAdvertised` (25.57s) | `cli_tasks_help_surface_basic_test.go:852: gopi task sweep-worktrees --db …/g1-ledger.db: TIMEOUT after 25s — argv fell through to the agent loop, or parked on a child` | 单独跑 @load 2.44 → **PASS 15.09s** | **负载敏感**：上限是它**自己**的 25s 子进程帽，同日安静值 15.09s 只留 1.6x 余量，victim 的 25.57s = 安静值的 3.48x（基线流 7.35s）→ 被单纯的时间膨胀顶穿 |
| 3 | `agent/TestPublishedHelpPagesStayWithinTheirUsageBudget/task` (0.02s) | ``cli_usage_budget_test.go:170: `gopi task -h` is 198 lines — over the 190-line budget by 8.`` | 单独跑 → **FAIL（同一条）** | **不是负载敏感**：纯行数断言，零时间因素，复跑同样红。它跑在预算的最后一秒（13:26:30），被"顺带"搭进同一份失败摘要 |

**第 4 条（表里没被算进"三个"，但更值得注意）**：
`agent/TestAcquireServeLock_RefusesAfterUnlinkUnderneath`（victim 5.34s）

```
cli_serve_test.go:105: helper never signalled readiness at …/TestAcquireServeLock_RefusesAfterUnlinkUnderneath…/holder.ready
```

上限是命名常量 `serveLockHolderWaitTicks(100) × serveLockHolderWaitTick(50ms) = 5s`，
它自己的注释写着"a slow CI must not turn a working test red"。
空载（load 5.52）下 `-count=5` → **4/5 FAIL**，时长 5.05/5.03/5.04/5.03s，
site 恒为 cli_serve_test.go:105（**该行号属于重写前的文件**；校正见文首「事后校正」）。
**它看起来像负载敏感，其实不是**：失败时长恰好等于上限，说明 helper 从未写出
marker，不是"等得慢"。这条已在 follow-up `01a12eae-f54c-7153-9b87-d216d6693bcb` 提出。

## 5. 预算常量的注释 vs 实测（被检查的那句）

`scripts/test-budget.sh:82-85`：

```
# 25m is 2.5x the default: it clears the worst measured load (1123.8 s under
# load 42-63) while still bounding a real hang.
test_budget="25m"
```

同一组数字在 `:698-700` 的 TIMEOUT banner 里被当判据复述。实测：

1. **`1123.8` 在本机账本里不存在**。全树 `grep -rn '1123.8'` 只命中注释自身与
   `devdoc/test-budget-single-source.md:72` 的表格行（该行来源标注是 *upstream doc*，
   不是本机可复跑的测量）。stamps.log 中最近的 1123s 是**另一个 scope**：
   `./internal/removalaudit ./internal/standards ./internal/tools elapsed=1123s load_start=13.58`。
2. **`load 42-63` 也没有对应产物**。252 行 stamps.log 中 `load_start>=40` 的
   20 次运行**全部**是 `./internal/p/...`（每次 17–57s），
   **没有任何一次 elapsed > 900s**。该数字产生于 4 核时代；本机 8 核，
   同一绝对 load 值的 per-core 压力减半。
3. **它要覆盖的那个 scope 的真实最坏值被低估**。
   - 唯一真正**跑完**的 2 包运行：`./internal/agent ./cmd/agent` @load_start=3.09,
     elapsed=**1333s** exit=0 pass → 25m/1333s = **1.13x**，不是注释暗示的 1.33x，
     更不是头部写的 2.5x。
   - `./internal/tools ./cmd/agent` 全部 6 次运行：`02:20 load_start=2.30 elapsed=1328s exit=1`
     （唯一非 timeout，但**也没有跑完**），其余 5 次 `1603 / 1589 / 1593 / 1584 / 1598s`
     全部 `exit=3 outcome=timeout` —— **load_start 只有 10.47–20.66，远低于注释声称的 42–63**。
     即：**在注释声称的负载之下，这个 scope 就已经撞满预算**。
4. **注释的前提已失效**：它说 "2.5x the default"，隐含 Go 默认 10m 在本机会
   "必然 600s 收尾"。而本机 `./internal/tools` 单独跑有 4 次绿色：400s / 384s /
   728s / 690s —— 10m 默认已容纳单包，"25m = 2.5x 默认" 的推导失去了它引用的依据。

已作为 follow-up 提出：`01a12eae-d966-7e10-bc93-4a6aadad35bd`。

## 6. 真正被这次超时拿走的东西

超时的危害不是"某测试红了"，而是**覆盖率**：预算到点时两个包都没跑完。

| 包 | victim 实际跑到 | 同 scope 基线的顶层测试数 | 覆盖率 | **从未跑到** |
|---|---|---|---|---|
| `internal/tools` | 1847 | 2469 | **74.8%** | 622 |
| `cmd/agent` | 1640 | 2606 | **62.9%** | 966 |

也就是说：这一行 exit=3 的 run **从未检查过两个包合计约 1588 个顶层测试**。
把它读成"红了几条"会严重低估代价——它是"37% 没验"。

## 7. 测量链（每一步都可复跑）

证据全部落在本 run 的 session 目录
`/home/user/.gopi/sessions/01a12e97-28e2-7537-b454-9b131963a1da/artifacts/`：

| 文件 | 内容 | 产生方式 |
|---|---|---|
| `inflight-at-expiry.txt` | 两包各自的流窗口、在飞用例、到期时刻、Σ Elapsed | 解析 victim 流的 `-json` 事件 |
| `timeline.txt` / `first-stall.txt` | 逐测试时间线、首个变慢点、逐测试比值 | 同上 |
| `baseline-compare.txt` / `tools-package-inflation.txt` | victim vs 绿基线的逐测试与聚合膨胀 | victim 流 + `run-1791742805-235281.json` |
| `overlap-and-history.txt` / `overlap-window.txt` | victim 窗口内并发的 23 次运行、同 scope 6 次运行史 | `stamps.log`（252 行） |
| `load-band.txt` | load_start 分档 × 最大 elapsed | `stamps.log` |
| `failures.txt` | 5 个 fail 事件及其原始 output | victim 流的 `Action=fail` |
| `failure-classification.txt` | 4 个根因的逐条分类 | 上表 + 隔离复跑 |
| `isolated-reruns.txt` | 本次会话 4 次真实复跑的命令与输出 | 直接执行 |
| `budget-constant-check.txt` | 注释所引数字的溯源 | grep + `stamps.log` |

复跑命令（本节真跑过，非引用）：

```
$ go test ./internal/tools/ ./cmd/agent/ -count=1 -timeout 25m \
    -run 'TestMemorySearchNonBlocking|TestAcquireServeLock_RefusesAfterUnlinkUnderneath|TestTaskVerbSweepWorktreesRunsAsAdvertised'
  -> ok   internal/tools 11.557s ; FAIL cmd/agent 35.023s            (load 2.44)

$ go test ./cmd/agent/ -count=1 -run 'TestTaskVerbSweepWorktreesRunsAsAdvertised' -v
  -> --- PASS: TestTaskVerbSweepWorktreesRunsAsAdvertised (15.09s)

$ go test ./internal/tools/ -count=5 -run 'TestMemorySearchNonBlocking'
  -> ok internal/tools 24.121s                                       (5/5 PASS)

$ go test ./cmd/agent/ -count=5 -run 'TestAcquireServeLock_RefusesAfterUnlinkUnderneath'
  -> 4/5 FAIL @5.03–5.05s, cli_serve_test.go:105                     (load 5.52)
```

## 8. 环境放大量：suite 无法使用内存后端

本次会话运行的输出里，`internal/testutil` 自己报出了这件事：

```
testutil: suite "cmd-agent": scratch root /home/user/workspace/gopi-test-scratch is
ext4/3 filesystem (block-backed, and the backing of both / and /home on this host)
(placement device-root) — a fresh ledger built under it pays this device's journal
commits, shared with every other writer on it, so a loaded host costs this suite
more wall clock than its budget assumes. the memory-backed candidate(s) /dev/shm
(711.4 MiB free, below the 1.0 GiB floor) were declined for lack of room
(floor 1.0 GiB).
```

即：`/dev/shm` 已 **90% 满（6.6G/7.3G，376 个条目，其中 172 个是 testutil 的
`suite-*` 残留，最老的躺了 26 天）**，于是每一次 fresh ledger 首开都落到
ext4 的 journal commit 上，并与主机上每个其他写者共享该设备。
这条与"负载敏感"的测试机制**同源**（见 §4 第 1 条：schema 构建就是一串 commit）。
已作为 follow-up 提出：`01a12eaf-3ab5-76f9-9b6b-161f09282e31`。

## 9. NOT MEASURED（诚实边界）

- **没有做预算内的重跑**。原计划里"回收 `/dev/shm` 后重跑同一 scope"这一步
  在本 run 的预算内没有执行（那会再占 10–15 分钟且需要先做回收，而回收不是本
  run 的权限）。因此本 run **不主张**内存后端能救回 5 次 timeout 中的任何一次。
- **没有测 8 核下单一 suite 串行运行的真实上界**。§2 的基线
  （1328s）本身是**与另一条同 scope 长跑重叠**的 run，因此它**高估**了安静值，
  2.16–2.30x 的膨胀系数是**下界**。`./internal/tools` 单独绿的 400/384/728/690s
  与 `./cmd/agent` 单独绿的一次都没有（252 行账本里 `pkg=./cmd/agent outcome=pass` 为 0 次），
  所以"两包依次跑到底需要多少"**在本机账本里查不到**。
- **没有定位 §4 第 4 条那个 helper 为什么不写 marker**。只测到"空载 4/5 红、
  时长恰好等于上限、site 恒定"；机制（子进程 argv / 环境 / flock 竞争路径）
  是**推断**，已在 follow-up 里标明为待执行者自证。
- **没有主张 25m 应该改成多少**。那是 owner 的阈值决定（Rule 27 的反面：
  也不许为了让车开过去而放宽）。本 run 只提供测量。
- **没有逐条归因那 622 + 966 个未跑到的测试**，只给出覆盖率。

## 10. Follow-up

| 行 id | 主题 |
|---|---|
| `01a12eae-d966-7e10-bc93-4a6aadad35bd` | 25m 预算注释与实测脱节（1123.8s / load 42-63 溯源为空；同 scope 在 load 10–21 就连撞 4 次） |
| `01a12eae-f54c-7153-9b87-d216d6693bcb` | `TestAcquireServeLock_RefusesAfterUnlinkUnderneath` 空载 4/5 红——确定性缺陷被宽余量掩盖 |
| `01a12eaf-3ab5-76f9-9b6b-161f09282e31` | `/dev/shm` 90% 满、172 个未回收 `suite-*` → 内存放置被拒、全部 suite 退回 ext4 |
