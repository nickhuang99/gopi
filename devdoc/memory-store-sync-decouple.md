<!-- doc-class: normative -->
# memory store 与跨节点 memory sync 解耦（2026-10-27）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

任务：`01a17a67-f272-7cdf-adc1-2485d7a73a33`（epic `memory-store-sync-decouple`）。

## 一句话

`memory store` 的成功判据从此只有一条——**它把行写进了本机 `~/.gopi/memory.db`**。
store 之后不再 spawn 任何 `gopi memory sync` 子进程；跨节点传播是**手动动作**
（`gopi memory sync --peer P [--push]`，或操作员自己起的 `--every` 前台上限循环）。

> **⚠ 补记（2026-11-06）：上面那句"手动动作"所指的命令本身也已被删除。**
> `gopi memory sync` 连同整个跨节点 sync CLI 家族在 `5677cc6e` 被删掉，今天敲它
> 得到的是 exit 1 和
> `gopi memory: unknown subcommand "sync" — want search|store|get|list|update|delete|feedback|backfill-tokens|converge-keywords|brew|provenance|backup|capture-stats`。
> 本页描述的**删除**依然成立且仍然有效（store 之后确实不再 spawn 任何子进程）；
> 失效的是"那你可以手动跑"这一句。今天唯一跨节点的门是**单向拷贝**
> `gopi memory backup`（VACUUM INTO 快照 + 一条 ssh 腿，不导回任何东西），
> 它不循环，所以本页的 `--every` 前台循环没有任何替代物 —— 这是设计决定。

## 删掉了什么（以及为什么必须删干净）

删除清单（`git rm`）：

```
internal/tools/memory_store_trigger.go                     1033 行
internal/tools/memory_store_trigger_test.go
internal/tools/memory_store_trigger_lifetime_test.go
internal/tools/memory_store_trigger_task_context_test.go
internal/tools/memory_store_trigger_cancel_window_test.go
internal/tools/memory_store_trigger_cwd_test.go
internal/tools/memory_sync_child_cwd.go                    550 行（只为那条腿存在）
internal/tools/memory_sync_child_cwd_decision_test.go
internal/tools/memory_sync_child_cwd_ledger_test.go
internal/agent/memory_sync_on_store.go
internal/agent/memory_sync_on_store_real_test.go
```

调用点与配套一起删（同一个提交）：

| 位置 | 原来做什么 | 现在 |
|---|---|---|
| `internal/agent/loop.go`（`memory_store` after-success） | `a.afterMemoryStore()` | 只置 `a.memoryStored = true`，另加一段说明「没有东西在这里同步」 |
| `internal/agent/selfnotecapture.go`、`selfnotecaptureresponse.go` | 同上（自动捕获两条路径） | 同上，注释说明成因 |
| `cmd/agent/main.go` | `tools.SetMemoryStoreSyncSelfPath(runcmd.SelfPath())` | 删（`internal/runcmd` 的 import 也随之删掉） |
| `cmd/agent/cli_chat.go` closeout | `tools.CancelInFlightMemoryStoreSync()` + `chatStoreSyncCancelledNotice` | 删。**没有 in-flight 轮次可取消，就没有「收尾杀同步导致会话结束」这条路。** |
| `internal/agent/session.go` | `memory_sync_on_store` 事件的常量/记录/写入器 | 删（旧日志仍可读，`trajectory.go` 无 default 分支，事件对现有消费者是惰性的） |
| `internal/tools/memory_sync_assets.go` | `MemorySyncAssetStats.ChildCwdReport` | 删（唯一生产者已不存在：手动 sync 在操作员自己的 shell 里跑，cwd 由 shell 决定） |

**这不是「关掉一个开关」，是「那条腿不存在了」。** 判据不是"符号 grep 归零"，而是
**生产点归零**：`grep -rn "memoryStoreSyncRounds\|memoryStoreSyncRoundCancel" --include=*.go`
在全树（含测试）零命中，意味着没有任何代码还能发起那样一轮。

## 保留了什么，以及为什么

**`memory_sync_on_store` 这个键留下，但语义反转为「已退役」。**

它仍然 **declared**（`config.Config.MemorySyncOnStore`），所以：

- **拼错仍然报错**——`pushh` 不会被读成「操作员主动关掉了」，而是
  `ResolveMemorySyncOnStoreDirection` 返回 error（Rule 15：静默回退=缺陷）；
- **任何合法值（`push` / `pull` / `off` / 缺省）现在都解析成 `off`**，
  注释里写清楚「`push` 是被接受并忽略，不是不认识」；
- **仍然设置了 `push`/`pull` 的操作员会在启动路径被告知**：
  `cmd/agent/memory_sync_on_store_retired.go` 的 `reportRetiredMemorySyncOnStoreKnob`，
  挂在 `cmd/agent/tmpdir_inject.go` 里**已退役的 memory-sync tick 报告旁边**（同一个
  pre-chain 时刻，同一个形状，不发明第二套）。

如果直接把键从结构体里删掉：`json.Unmarshal` 会**静默忽略**未知键，操作员的文件里
永远躺着一个看起来还有效的设置，而没有任何人会告诉他。这条是本仓库已经付过代价的坑
（`merge_tick` / `memory_sync_tick` 两次都踩过）。

## 新加的护栏：`gopi memory sync` 现在是唯一出口，所以要能自证

> **⚠ 补记（2026-11-06）：本节的主语已被删除，护栏随之不复存在。**
> `gopi memory sync` 在 `5677cc6e` 被删，所以"唯一出口"这句今天无处可指，本节记录的
> 单会话锁（`<ledger>.memory-sync.run`）、`memory-sync.log` 行格式和状态码 `3` 都
> 属于一个**不会再运行的执行体**。保留本节是因为它记录的是当时的推理（一个会写对端
> 账本的动词必须能自证），不是因为这些护栏还在生效。

`cmd/agent/memory_sync_guard.go`：

### ① 同一 `--db` 只允许一个会话（flock）

`<ledger>.memory-sync.run` 上的**非阻塞** `flock(LOCK_EX|LOCK_NB)`：

- 第二个会话**立即**返回，退出码 **3**，说的是
  `another sync is already running against <db> (one session per ledger) — nothing was done`；
- **3 ≠ 1**：重试循环必须能区分「对端挂了」和「别人正在同步」；
- 两个不同 `--db` 互不影响（守卫键在 ledger 上，不是全局）；
- 锁就是 ledger 自己文件系统上的普通文件——**没有 pid 文件、没有守护进程、没有调度器**
  （Rule 35），进程退出（含 `SIGKILL`）由内核释放。

拿不到锁但不该因此拒绝运行时（比如 profile 只读），**降级会被喊出来**
（`gopi memory sync: running WITHOUT the single-session lock: <lockErr>`，输出格式见
`cmd/agent/cli_memory_sync.go:328`），不是静默降级。

### ② 失败/拒绝都落盘一行可 grep（Rule 15）

非零退出码只有"看着这个进程的人"能读到；一个对端挂了三天，恰恰是没人看着的那种失败。
所以每次失败或拒绝都往 `<profile>/logs/memory-sync.log` **追加一行**：

```
2026-10-27T06:57:38+08:00 memory-sync-failed peer=nosuchhost.invalid mode=push exit=1 elapsed=0.0s db=<the-runner-profile>/.gopi/memory.db last="tools.MemoryAssetsPeerSessionMode(peer=nosuchhost.invalid, mode=push) returned: memsync assets: ssh nosuchhost.invalid: exit status 255 (stderr: ssh: Could not resolve hostname nosuchhost.invalid: Name or service not known)"
```

上面那条记录里 `db=<the-runner-profile>/.gopi/memory.db` 是**省略写法**：真实路径里
的 profile 目录属于跑出这条记录的那台机器（本机没有这个文件，`ls /home/user/.gopi/logs/memory-sync.log`
报 `NO_LOG_ON_THIS_HOST`）。行格式来自 `cmd/agent/memory_sync_guard.go:144` 的
`memorySyncLogLine`，取回真实值的命令是在那台机器上 `tail -1 /home/user/.gopi/logs/memory-sync.log`。

带 **peer / mode / 耗时 / 退出码 / ledger / 最后动作**（最后动作是该 verb 自己拼的
`tools.MemoryAssetsPeerSessionMode(...) returned: <原始错误>`，不是转述）。
拒绝并发那一行走 `memory-sync-busy`，与失败可分。

```
grep memory-sync-failed ~/.gopi/logs/memory-sync.log
grep memory-sync-busy   ~/.gopi/logs/memory-sync.log
```

文件 append-only、不轮转、gopi 内部没有读者——消费方是操作员的 `grep`，目录就是
`worker.log` 已经在用的那个。

## Rule 35：谁回收这个日志文件？

**没有任何东西自动回收它，这是有意的。** 它是一个 append-only 的**一行一次失败**记录：
只有失败才写，一个健康的节点上它根本不增长。它不带来常驻进程、不带定时器、不带新脚本，
也不被任何代码读；删掉它的成本是零（`rm`），而它存在的理由是"事后回答为什么这个节点
的知识停在某天"。**没有新增调度者，也就没有需要回收的调度者**——这是与 cron/daemon 的
本质区别。

## 实测证据（本 run session 目录下的文件）

| 门 | 证据文件 | 结果 |
|---|---|---|
| store 之后没有新 sync 子进程 | `03-gate1-spawn.txt` | 20 次采样 × 0.25s 全程无 `SPAWNED` |
| 手动 sync 失败：非零退出 + 落盘 | `04-gate2c-failure.txt` | `EXIT=1` + `logs/memory-sync.log` 一行带 ssh 原文 |
| 白名单外 peer 也被记录 | `04-gate2-failure.txt` | `EXIT=1` + 一行 `memory-sync-failed`（不是静默） |
| 同一 `--db` 第二个会话被拒 | `05-gate3-busy.txt` | `EXIT=3`，**18ms** 返回，落一行 `memory-sync-busy` |
| store 返回文本 | `03-gate1-spawn.txt` | `Memory stored (id=1).` —— 不含 peer/push/sync |

### 第一版验收为什么是错的（记下来，别再踩）

用验收里原样的
`for i in $(seq 1 10); do pgrep -f 'gopi memory sync' >/dev/null && echo SPAWNED; sleep 0.5; done`
在本机跑出了 **10/10 SPAWNED**——但那是**假阳性**，两个来源：

1. `pgrep -f` **自匹配**：watch 循环所在的 `bash -c` 命令行本身就带着那个字符串；
2. 更重要的：**旧二进制遗留的孤儿 `gopi memory sync --peer peerbox --push`**（pid
   2666249、2744491 等）在修复**之前**就跑着，它们与本次 store 无关。

正确做法（`evidence/watch-sync.sh`）：`pgrep -x gopi` 取候选 → 读 `/proc/<pid>/cmdline`
精确匹配 → **排除基线 pid**。用这一版重跑，全程 0 条。

**教训（Rule 24 的姊妹陷阱）**：判「有没有 spawn」不能用 `pgrep -f`，它既会自匹配，
也会把**别人的旧进程**算成你的证据。先取基线，再按 cmdline 精确比对。

## 复验：逃逸腿（child-cwd）在生产点上是否还活着（2026-10-27，task `01a178eb`）

这份文档说「删掉了什么」，但删一个机制的正确验收判据是 **生产点归零**，不是符号
grep 归零（Rule 43：符号会改名，生产点不可伪造）。本 run 在 `f7aa339` 上把这条
逃逸腿作为**对象**复验了一遍，结论是**它已经没有对象**：

三列口径不同、都要看：**files** = `git grep -l | wc -l`（命中文件数），**prod** =
同上但排除 `*_test.go`（真正的生产点），**lines** = `git grep -n | wc -l`（含注释与测试）。

| 判据 | files `5e3ce2e^`→`HEAD` | prod `5e3ce2e^`→`HEAD` | lines `5e3ce2e^`→`HEAD` |
|---|---|---|---|
| `DecideMemorySyncChildCwd`（`memory_sync_child_cwd.go` 的**唯一**出口） | 4 → **0** | 6 → **0** | 10 → **0** |
| `memoryStoreSyncArgv`（唯一把 `gopi memory sync` 拼成 argv 的地方） | 4 → **0** | 4 → **0** | 8 → **0** |
| `memoryStoreSyncRunChild`（唯一 `exec.CommandContext` 的 spawn 缝） | 5 → **0** | 8 → **0** | 12 → **0** |
| `CancelInFlightMemoryStoreSync`（唯一持有子进程句柄的取消面） | 5 → **0** | 7 → **0** | 29 → **0** |
| `ChildCwdReport`（唯一测量子进程 cwd 的字段） | 3 → **1** | 4 → **1** | 6 → **1** |

唯一残留的那一处是 `internal/tools/memory_sync_assets.go:202` 的一行注释
（"The former ChildCwdReport field is GONE with its only producer"）——**注释不是生产点**，
这正是 Rule 43 要求"打开命中行读"而不是数命中数的原因。

删除前的调用链只有一条，且**决策函数只有一个调用者**：

```
5e3ce2e^:internal/tools/memory_store_trigger.go:704
    childCwd := DecideMemorySyncChildCwd(memoryStoreSyncCwdRole, memoryStoreSyncCwdLabel)
5e3ce2e^:internal/tools/memory_store_trigger.go:688
    cmd := exec.CommandContext(ctx, exe, argv...)      // 唯一的 spawn 缝
```

两者都在 `memory_store_trigger.go` 里，而**这个文件整个被删了**——所以"决策函数在
别处还有调用者"这条路不可能成立：决策函数的调用点本身就是被删的生产点。

**真跑取证（Rule 31）**：在本 ground 内从 `master` 构建出二进制 `/tmp/gopi-verify-01a178eb`
（`go build -o /tmp/gopi-verify-01a178eb ./cmd/agent/`），在一个私人 sandbox
（`gopi tool sandbox create --label verify` → `/home/user/workspace/gopi-sandbox/sandbox-verify-55d7d2a1`）里
**跑两次**真实 `gopi memory store`，都用 `strace -f -e trace=execve,execveat,clone3,clone,vfork` 记录：

| 运行 | 命令 | execve | clone3 调用（带 flags 的行） | 其中不带 `CLONE_THREAD` | `clone(`/`vfork` |
|---|---|---|---|---|---|
| ① sandbox 内 | `cd $SB && GOPI_HOME=$SB strace -f -qq -e trace=execve,execveat,clone3,clone,vfork -o /tmp/store-trace-01a178eb.txt /tmp/gopi-verify-01a178eb memory store --topic "01a178eb 复验探针" --question "memory store 会不会派生子进程？" --answer "本行实测：不派生。"` | **1**（只有它自己） | 23 | **0** | 0 / 0 |
| ② **在 ground 目录里**（若派生 child，child 会继承本 ground 的 cwd） | `cd $GROUND && GOPI_HOME=$SB strace -f -qq -e trace=execve,execveat,clone3,clone,vfork -o /tmp/store-trace-in-ground.txt /tmp/gopi-verify-01a178eb memory store --topic "01a178eb ground-cwd 探针" --question "store 在 task ground 里跑会不会留下子进程？" --answer "实测：不派生。"` | **1**（只有它自己） | 33 | **0** | 0 / 0 |

- `execve` **只有一条**，就是被追踪的 `gopi memory store` 自身；**没有第二条 execve**，
  即**没有任何子进程被启动**（一个 `exec.CommandContext` 的子进程必然产生第二条 `execve`）。
- `clone3` 的每一条调用都带 `CLONE_THREAD|CLONE_SIGHAND|CLONE_VM`（`exit_signal=0`）——
  全是 Go runtime 的线程；**不带 `CLONE_THREAD` 的子进程创建 0 条**，`clone(`/`vfork` 各 0 条。
- 运行 ② 特意在**本 ground 的目录里**执行：逃逸腿的特征产物就是"cwd 落在 task ground 里的
  `gopi memory sync` 子进程"，而这个运行结束后 ground 里**没有多出任何进程**（见下）。

**`/proc` 全表扫描（store 前后各一次，扫描器写成文件而不是行内 `bash -c`）**：
`~/.gopi/sessions/01a178eb-7e71-7882-b98c-60093cc2b2a2/proc-scan.sh <ground>` 做两件事 —— (A) 遍历 `/proc/[0-9]*`
的 `cwd` 看谁站在本 ground 内，(B) 逐 token 读 `cmdline`，找同时含 `memory*` 与 `sync*` 的进程
（不用模式匹配，避免把自己算进去）。两次读数**完全一致**：

```
cwd_in_ground_count=4     ← ①本回合的 bash -c ②proc-scan.sh 自身 ③它的 tee ④worker pid 777377
memory+sync_argv_count=0
```

store **之前**和 store **之后**都是这 4 个，**没有第三个新增**；`memory+sync` argv 的进程
**始终 0**。也就是说：即便在 ground 目录里跑真实 store，也没有任何 `gopi memory sync`
形状的进程出现，更没有进程把 cwd 钉在 ground 里。

**guard ③（`TaskGroundInUse`）读数**：从 ground **之外**（`cd /tmp`）调用，避免下面那条陷阱：

```
$ cd /tmp && gopi task status --worktree /home/user/workspace/01a178eb-7e71-7882-b98c-60093cc2b2a2
ground_now:  no live process stands in it
```

`no live process stands in it` —— 与扫描一致：站在 ground 里的 worker 与调用链是**祖先链**，
被正确排除，没有被算成 holder。这条读数在 store 前后都一样。

**这次复验买到的两条教训，值得留给下一个人：**

1. **"跑一次没看到子进程"不是证据，`strace` 才是；而扫描器本身必须不被算进读数。**
   本 run 的 `/proc` 扫描里，(A) 那 4 个 pid 中**有 3 个是这次扫描自己**（本回合的
   `bash -c`、`proc-scan.sh`、它的 `tee`），只有 `pid 777377` 是本 run 的 worker ——
   也就是说**行内 `bash -c` 写法的 argv 会把自己的扫描器算进"站在 ground 里的进程"**。
   (B) 那一支因此不按模式匹配，而是**逐 token 读 `/proc/<pid>/cmdline`**、并显式跳过自身
   `$$`；写成文件而不是行内，argv 里就不带被搜的字符串。同一个形状的坑还有一次实测：
   `for d in /proc/[0-9]*` 循环期间有进程消失，`read` 就报
   `proc-scan.sh: line 24: /proc/1158845/cmdline: No such file or directory` —— 这是
   `/proc` 的固有竞态，扫描器必须容忍它（重定向 `2>/dev/null`），而不是把它当成命中。
2. **guard ③（`TaskGroundInUse`）没有被这次删除碰到，而且仍然正常工作。** 提交
   `5e3ce2e` 的 `--stat` 里没有 `internal/tools/task_ground_in_use.go`，
   `git log 5e3ce2e^..5e3ce2e -- internal/tools/task_ground_in_use.go` **输出为空**。
   判据面用自造 marker 进程在 **A/B/C** 三段实测（`guard3-probe.sh`，从 `/tmp` 调用）：

   ```
   ## A. 基线（ground 里没有 marker）           ground_now:  no live process stands in it
   ## B. marker: ( cd $GROUND; exec sleep 300 )  ground_now:  IN USE by live process pid 1403752 (sleep) argv="sleep 300" ppid=1403732
   ## C. kill -TERM 1403752 后（自己持有的 pid）  ground_now:  no live process stands in it
   ```

   三段都对：free 报 free、有 cwd=ground 的活进程报 IN USE 并给出 pid/argv/cwd、定向
   kill 后回到 free。**kill 用自己持有的 pid，不送组信号**（Rule 42）。

   **同一组探针暴露了它的一处读数陷阱，本 run 复现了两次**：`ground_now` 判的是
   "**谁**站在 ground 里"，而调用链本身就是站在 ground 里的进程 —— 所以把 `gopi` 的输出
   **pipe 给 grep** 时，grep 继承调用者 cwd，自己成了 holder：

   ```
   $ cd $GROUND && gopi task status --worktree $GROUND | grep ground_now
   ground_now:  IN USE by live process pid 1409561 (grep) argv="grep ^ground_now:" ppid=1409558 — cwd /home/user/workspace/01a178eb-7e71-7882-b98c-60093cc2b2a2

   $ cd /tmp && gopi task status --worktree $GROUND > /tmp/f; grep ground_now /tmp/f
   ground_now:  no live process stands in it
   ```

   两行是**同一时刻、同一个 ground**，差别只在调用链站在哪里。第一次复现是在本 run 的
   一次在 ground 里跑 `bash guard3-probe.sh $GROUND /tmp/gopi-verify-01a178eb | tee /tmp/guard3-out.txt` 里：`tee` 自己（cwd 就是
   ground，因为那一整条命令在 ground 里跑）把 A/B/C 三段全部污染成 IN USE，看起来像
   "marker 杀不掉"。**所以这条判据必须在 ground 之外调用、并且把输出写到文件再读**；
   在 ground 里 pipe 给任何东西得到的 IN USE 是**读数错误，不是 ground 被占用**
   （已作为 bug 行提交，见本节最后）。

**本 run 的证据**（`/home/user/.gopi/sessions/01a178eb-7e71-7882-b98c-60093cc2b2a2/`）：

- `evidence-production-points.txt` — 上面那张生产点表的原始命令与输出（含 `5e3ce2e` 的
  完整 `--stat`：**11 条 `D`**，其中两个机制文件在删除前分别是 **550 行**
  `internal/tools/memory_sync_child_cwd.go` 与 **1033 行**
  `internal/tools/memory_store_trigger.go`，其余 9 条是它们的测试与 `internal/agent/`
  的 on-store 腿）；
- `evidence-store-realcrun.txt` — 两次真实 `gopi memory store` 的 strace 读数与
  `/proc` 全表扫描（store 前后一致）；
- `evidence-guard3-taskgroundinuse.txt` — A/B/C 三段 guard ③ 读数 + 读数陷阱对照；
- `proc-scan.sh` / `guard3-probe.sh` — 上述两次取证用的扫描器与探针（写成文件，
  argv 里不带被搜的字符串）。

**结论**：briefing 问的"三条退化路径是否逃逸"，在 `master` 上**没有对象** —— 逃逸腿和
它的 spawn 缝**不存在**（不是被门禁挡着），因此"memory sync 子进程钉住 task ground"
这条因果在当前树上**不可达**；guard ③ `TaskGroundInUse` 未被该提交触碰且仍正常工作。

## 影响面

- `gopi memory store` 的返回文本与之前一致（`Memory stored (id=N).`），且现在
  **必然**不含 peer/push/sync——新测试 `TestMemoryStoreReturnSaysNothingAboutSync` 锁死这一点。
- `gopi memory sync --peer P [--push|--pull] [--dry-run] [--every]`：**行为不变**，
  只是多了并发锁与失败落盘。
- 会话收尾不再有「取消 in-flight 同步」这一步——它连同被取消的对象一起消失。
- `CONFIG.md` 的 `memory_sync_on_store` 行改写为「已退役」，并新增手动 verb 的两条契约
  （一 ledger 一会话、失败必落盘）。
