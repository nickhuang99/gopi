<!-- doc-class: normative -->
# /proc 扫描的代价，与"一个测试把包拖成假红"该怎么归位

任务 01a12ec8-3ea4-743d-b7aa-d086d5ad4657（2026-10-12）。相关 bug 行：
01a12ee4-64a9-7059-aa82-f22c3ff59e9d（生产代码侧的加固，本任务未做）。

## 现象

负载宿主（load ~13，nproc=8）上 `cmd/agent` 撞 Go 默认 10m **包级**预算：

```
panic: test timed out after 10m0s
	running tests:
		TestScratchGCTickReportsUndeclaredProfileChildren (0s)
github.com/nick/gopi/internal/scratch.procMapsPaths ... holder.go:354
github.com/nick/gopi/internal/scratch.LiveHolders ... holder.go:170
github.com/nick/gopi/internal/scratch.BaseSuiteRunReport ... base_suite_runs.go:219
github.com/nick/gopi/cmd/agent.scratchGCTick ... scratch_gc_tick.go:180
FAIL	github.com/nick/gopi/cmd/agent	606.210s
```

## 三个必须先分清的事实（否则会修错东西）

1. **`606.210s` 不是这个测试跑了 606 秒。** 它是 `FAIL <pkg>` 的存活时间 = 包的 10m 预算
   + 6.2s 的 goroutine dump。`-timeout` 是包级的。
2. **`running tests:` 打印的是转储瞬间正在跑的测试，`(0s)` 是"它刚开始"。**
   本机用一个 5s sleep 的探针包验证过：`TestSleepsFiveSeconds (1s)` 在 `-timeout 1s` 下。
   所以 `(0s)` 与"卡了 600 秒"是**相反**的读数。
3. **cmd/agent 有 ~370 个 fixture。** 负载下它们的累计成本先耗尽包预算，该测试只是那时刻
   正在跑的那一个。把它删掉/跳掉能救这次假红，但救不了"包预算在负载宿主上本就紧张"这件事
   （见 memory #5161）。

## 代价从哪来（可直接测量的那一段）

`internal/scratch/holder.go` 的 `LiveHolders()`：一次全量 `/proc` 遍历，每个 pid 读
cwd、exe、**整份 `maps`** 与 `/proc/<pid>/fd/*`。它只有一个上限
`ProcMapsReadLimit = 4 << 20`（**单文件**），**进程数、映射条目总数、总墙壁时钟都没有上限**。
本机直接测量（load 12.6，只测调用、不造负载）：

```
LiveHolders #1: 965ms,  paths=57181
LiveHolders #2: 874ms,  paths=57184
LiveHolders #3: 1.506s, paths=57210
```

同一扫描面的宿主规模：`pids=494 maps_bytes=7113234 fds=3445`。

而触发它不需要任何特殊情况：`internal/scratch/base_suite_runs.go:206-221` 在**第一次**命中真实
suite-run 根时就无条件加载（与候选是否 stale 无关），本机 `/dev/shm` 有 **207** 个真实
`suite-*` 根（共 413 个条目）——也就是说，在负载宿主上每次 `BaseSuiteRunReport` 必付一次全量
`/proc`。`markTest` 的 base arm 因此天然是"机器状态相关"的，这是它被归为
`heavytest.CriterionHostCondition` 的字面理由。

## 决策：为什么走 heavy 分类，而不给扫描加时间上限

deliverable 允许两种形状；这里选 (b) heavy 分类，理由是 **(a) 对删除型调用方是数据丢失风险**：

`LiveHolders()` 的返回值被用作"该目录**没有**活进程持有"的**否定判据**。加一个墙壁时钟上限，
被截断的那一刻就会把"**未扫描**"变成"**未持有**"，而 `unheldBatch` 的下一步是 `removeCollectable`
——删掉一个活进程正在用的目录。24 处调用点共享这条判定（sweep/reclaim/attribution/worktree-stub/
profile/attachments…），正确的修法是让**截断本身可被调用方看见**并让删除型调用方退化为"保留"，
那是一个独立的、必须带回归测试的改动。它已作为 bug 行派出（01a12ee4），不在本任务里顺手做。

heavy 分类只控制**跑不跑**，不控制**断言什么**：`make test-heavy PKGS='./cmd/agent'` 仍会完整跑
它，包括"什么都没被删除"的四路径断言。测试要守的性质一字未改。

## 一个 leaf 包门禁覆盖不到的地方（留给后来者）

`internal/scratch` 是 leaf 包，`internal/standards` 的 heavy 门禁只扫**测试 call site**
（`heavytest.Require`），**扫不到生产函数**。所以 `ProfileRootReport`
（`profile_root.go:526`，同样直接调 `LiveHolders()`，被 CLI 与 resident tick 共用）这条路径
**没有**任何分类手段可用 —— 它只能靠修生产代码来解决（bug 01a12ee4）。

## 复现脚本（无负载制造）

```bash
# 该测试单独跑（本机负载 8.6-15.9）：6.85s-14.0s
go test ./cmd/agent -run TestScratchGCTickReportsUndeclaredProfileChildren -v -count=1

# smoke 默认下它应当 skip（heavy 分类生效）
go test ./cmd/agent -run TestScratchGCTickReportsUndeclaredProfileChildren -v -count=1
#   → HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run) … --- SKIP

# 清单与 call site 的双向核对
go test ./internal/standards/ -run Heavy
```

## 清单

- `cmd/agent/cli_scratch_gc_profile_test.go`：测试体首行加 `heavytest.Require`
- `testdata/heavy-tests.tsv`：补 `TestScratchGCTickReportsUndeclaredProfileChildren /
  CriterionHostCondition / cmd/agent/cli_scratch_gc_profile_test.go`
