<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: 存活读数与「updated_at 可疑」的终结 — tick-tock liveness

Epic task `01a0a387-ce59-73bb-91b8-58e442195616`:
> see the meaning of updated_at time which is not updated since its first run.
> originally the updated_at meaning gopi toolcall or other output update which
> can be used to measure if task is still running. check source to see if this
> still remains, or what is an economical way to get task tick tock timepoint to
> prove it is still running.

**一句话结论**：`updated_at` 从来不是心跳，也不会再被误读——它现在在自己的
渲染行上自我说明；真正的 tick-tock（`last_toolcall_at` / `tasklist_issued_at`）
升级为**带年龄和判决的读数**，`task show` 与 `task list` 两个面共用同一次
计算、同一个宽限期。

## Completed

### 1. 语义审计（复现 + 溯源，master tip `39a08e20`）

- `updated_at` = **M4.13 delta-sync 的版本锚**，**没有任何 toolcall / agent
  输出路径写它**。全部写入点（`grep 'updated_at[[:space:]]*=[[:space:]]*[?|excluded]'`
  全集 35 个文件命中）都是状态迁移、claim/CAS、claim-broadcast、delta-merge、
  tags、deliverable、run-provenance、verify-evidence、model-backfill、zombie
  re-arm bump 这类**事务性**写入。
- 心跳是 M4.5 的两列：`last_toolcall_at`（tool batch 开始，30s 节流）、
  `tasklist_issued_at`（收到 [TASK_LIST] 块，独立节流钟，`bd3163c3` 修的）。
- live 复现（本 epic 附件 `updated_at.txt` 的行）：task `01a0a37d…`
  `updated_at` 冻结在 13:15:59，而 `last_toolcall_at` 一直接着走 —— 用户看到的
  "自首次运行就不再更新"正是这个；进程还活着（`(live: alive)`）。
- 结论：**信号没缺，缺的是可读性**。`task show` 此前只渲染裸时间戳
  （`last_toolcall_at=2026-09-15 13:19:12`），读者得自己跟自己的表相减、还得知道
  30 分钟宽限期，才能回答"它还在跑吗"。

### 2. 实现：tick-tock 读数（`internal/tools`）

| 变更 | 位置 | 作用 |
|---|---|---|
| `taskTickTock(present, ts, now)` | tasks.go | 一个戳 → `HH:MM:SS (age 32.3s — fresh)`；NULL → `(none — unknown)`（**未记录绝不渲染成 stale**，Rule 15） |
| `taskShowTickTockLine` | tasks.go | show 的 `liveness:` 行：两个戳的读数列 + 测量所用时钟 `(clock HH:MM:SS)` |
| `taskUpdatedAtAnchorNote` | tasks.go | `updated_at: … (state/sync anchor — not a heartbeat)` — 在被误读的那一行上直接写清它的含义 |
| `taskHeartbeatReading(t, now)` | tasks.go | **唯一**计算心跳年龄 + stale 判决的地方（时钟倒挂钳零、`taskStallGraceSec()` 宽限；读 `taskRecordedLivenessAt`，**没有记录过心跳事实 → unknown，绝不拿 created_at 兜底**，见 §6） |
| `taskHeartbeatStaleMarker` | tasks.go | list 行尾追加 ` + heartbeat stale 26h0m8s` —— 进程活着但心跳停了，原本两种状态都渲染成 `[running …]` |
| `taskLastToolcallAtColumn` / `taskTasklistIssuedAtColumn` | tasks.go | 两个列名的**唯一**定义；schema / ALTER / 投影 / rebuild copy / writer / 两个 sweep 全部引用（Rule 1b/13） |

判决所用宽限 = `taskStallGraceSec()` → `config.ResolveTaskGracePeriods`
（CLI > env > settings > 默认 30min）—— **zombie sweep 自己的那个阈值**
（Rule 2：一个解析点）。所以显示说 `stale`，就等于"扫除的 stall 腿会判它死"。

### 3. live 验证（真实台账，非构造）

`GOPI_MEMORY_DB` 指向 `memory.db` 的副本，把本 epic 行的 `last_toolcall_at`
改到 26 小时前（进程仍活）：

```
$ gopi task show 01a0a387-…
liveness:       last_toolcall_at=11:45:25 (age 26h0m8s — stale) tasklist_issued_at=13:27:41 (age 17m52s — fresh)  (clock 13:45:33)
process:        pid=3438277 start=2026-09-15T13:26:32+08:00 cmd="gopi worker"  (live: alive)
updated_at:     2026-09-15 13:27:46  (state/sync anchor — not a heartbeat)

$ gopi task list --status in_progress
… [running 19m0s since 13:26:33 + heartbeat stale 26h0m8s] [via worker]
```

未改动的行仍是 `(age 32.3s — fresh)` + `(live: alive)` ——
**活的进程 + 停的心跳**现在在 list 面上就能区分，不必再进 show。

### 4. 回归守卫（两个 gate 都做了 red-proof）

| 测试 | 钉住什么 | red-proof |
|---|---|---|
| `TestTaskTickTockRendersAgeAndVerdict` | 戳必须带年龄 + 判决；NULL 是缺席不是 stale | 把 show 换回裸时间戳 → **FAIL**（实测，见下） |
| `TestTaskTickTockClockSkewGuard` | 未来戳钳到 age 0，绝不渲染 `age -2m` | — |
| `TestTaskShowLivenessLineUsesTickTock` | show 行确实走 tick-tock 渲染 + 带时钟 | 同上一并 **FAIL** |
| `TestTaskStateSuffixMarksStaleHeartbeat` | 活着但心跳停 → list 必须带 marker，且不破坏原措辞 | 关掉 marker → **FAIL**（实测） |
| `TestTaskHeartbeatReadingFallbacks` | created_at **不是**心跳（无事实 → unknown）/ 真实心跳超时仍 stale / 0 戳 = unknown / 时钟倒挂 | created_at-only 行必须不再报 stale，且 `taskStallHit` 仍按 created_at 判"行太老" |
| `TestHeartbeatColumnLiteralGate` | 列名字面量只许留在定义处（**AST 扫描，不是字节 grep**） | 在 `taskzombie_sweep.go` 内联回 SQL 文本 → **FAIL**（实测，报 `:136/:137`） |
| `TestHeartbeatColumnOccurrenceProbes` | 边界规则：`COALESCE(col,…)`/`tasks.col` 命中，`col + "s"`、`x_col`、散文不命中 | 误判 `x_<col>` 为命中 → **FAIL**（实测，已修） |

gate 用 `go/parser`+`go/ast` 扫**字符串字面量**（解引号后匹配，带标识符边界）：
字节 grep 会（a）把注释里的列名当违规，（b）**看不见真正的缺陷形态**
（`COALESCE(last_toolcall_at, created_at)` 这类无引号的 SQL 内联——第一版 gate
正是这样瞎的，red-proof 抓出来了）。

**gate 范围（明说，不含糊）**：`internal/tools` 的 production `.go` 文件——
常量、schema、迁移、投影、writer、两个 sweep 都在这里，一个漂移的字面量就会
静默读错心跳。`cmd/agent/cli_tasks.go` 的 2 处仍在算子帮助文本里（那是**给算子
grep 台账看的 DB 列名**，够不到别的包的未导出常量）；把它挪进一个导出常量叶子包
是另一件事，所以在这里**明写为已知未覆盖**，不用放宽 gate 的说法去糊它。

## Not Done

- **`cmd/agent/cli_tasks.go` 的 2 处帮助文本**未常量化（原因见上）。
  要做的话是 Rule 14 的 `internal/taskcolumns` 叶子包（schema 列名跨包共享），
  属于独立改动。
- **show 的 `liveness:` 行只在 `in_progress` 行渲染**（终态行不渲染）——
  刻意保留：终结的行不是"活着"，加一行 tick-tock 只会制造噪声。
- **未改 `updated_at` 的任何写入行为**：审计结论是分层正确（版本锚 vs 存活探针），
  把心跳混进 updated_at 会炸 delta 窗口并把"谁后到"判给心跳噪声。
  本 epic 只修可读性，不动语义。
- **未加 `gopi task status` 层面的变化**：那条路本来就有确定判决（pid 存活 + 退出码），
  不缺。
- `tasklist_issued_at` 在部分运行中仍为 NULL（模型没发 [TASK_LIST] 块）——
  现在渲染为 `(none — unknown)`，是**如实呈现缺席**，不是缺陷。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK**；
  `go build ./...` → **OK**；`gofmt -l`（改动文件）→ 干净。
- Tests: `go test ./internal/tools/ ./internal/agent/ ./internal/standards/ ./internal/config/ ./cmd/agent/ -count=1`
  → 见下方 job 输出（全绿）。
- Red-proof（三处，全部实测 FAIL→修→PASS）：
  1. show 换回裸时间戳 + 关掉 list marker →
     `TestTaskShowLivenessLineUsesTickTock` / `TestTaskStateSuffixMarksStaleHeartbeat` **FAIL**；
  2. 在 `taskzombie_sweep.go` 内联 `COALESCE(last_toolcall_at, created_at)` →
     `TestHeartbeatColumnLiteralGate` **FAIL**（报 :136/:137）；
  3. gate 的边界规则把 `x_<col>` 判为命中 → `TestHeartbeatColumnOccurrenceProbes` **FAIL**（已改对）。
- Live: 见 §3 的真实 `task show` / `task list` 输出（进程 alive + 心跳 stale 同时成立）。
- Commit: 见本分支 `fix/task-liveness-ticktock` 末次提交。

## 给未来的自己（一个经济的心跳读数）

不要读 `updated_at` 判活。判活三步，代价 = 一次 `task show`：

1. `updated_at` → 只看**状态/同步**，`(state/sync anchor — not a heartbeat)` 已写在它旁边。
2. `liveness:` 行 → 心跳年龄 + `fresh|stale`（与 zombie sweep 同一宽限）。
3. `process:` 行 → pid/start/cmd 三元组 + `(live: alive)`，这是**确定判决**。

"活着但心跳停" = 2 说 stale、3 说 alive —— 这个组合现在是渲染出来的，不是推出来的。

## 6. 更正（2026-10-22）：created_at 从来就不是心跳 —— 一个假阳性判决

§2 的表格里写着 `taskHeartbeatReading` 有「created_at 回退」。**那是缺陷，不是特性。**

**实测现象**（生产，非构造）：

```
$ gopi task list --status in_progress
#01a1622f-…  [in_progress]  …  [running 3.08s since 17:54:07 + heartbeat stale 3h56m57s]
$ gopi task status 01a1622f-…
→ alive=yes, resumable=no (exit=1)
```

一条**刚被认领 3 秒、进程活着**的行，被报成心跳停滞 3 小时 56 分。3h56m57s 正是它的
`created_at`（13:57:13 → 17:54:10）。照这个读数去 kill/reopen，就会杀掉一条正常运行的任务。

**根因不是缺代码，是现成的代码没接线。** 相隔二十行的 `taskLivenessKnown` 注释明写
"created_at alone does not count"，但它**从未被任何判据调用**；`taskHeartbeatReading`
只检查了 `hb <= 0`，于是 `taskLastLivenessAt` 的 created_at 兜底直接变成了心跳。

**修法（一个问题，两个提问）**：

| 函数 | 问的问题 | created_at |
|---|---|---|
| `taskLastLivenessAt` → `taskStallHit` | 「这行有多老」（zombie sweep） | **保留**为兜底 |
| `taskRecordedLivenessAt` → `taskHeartbeatReading`（show + list） | 「这心跳停了多久」 | **永不**代入 |

`taskLastLivenessAt` 现在 = `taskRecordedLivenessAt` 非零则用之，否则 `CreatedAt`；
`taskLivenessKnown` = `taskRecordedLivenessAt(t) > 0` —— 三者共用同一套算术，
所以 show、list、sweep 的三种问法不可能各自漂移（Rule 13）。
没有心跳事实的行，`known=false`，marker 不渲染（Rule 15：缺席就是缺席）；
它仍然显示自己的年龄，因为 `taskStateSuffix` 的 elapsed 锚在 **claim**，不在 created_at。

**前后对比（同一份台账副本、同一行数据、两个二进制）**：

```
# 修前（pre-fix 二进制）
#hb-proof-fresh      [running 12.5s since 21:03:11 + heartbeat stale 4h0m9s]   ← 假阳性
#hb-proof-realstale  [running 4h10m9s since 16:53:14 + heartbeat stale 4h0m9s]

# 修后（本分支二进制）
#hb-proof-fresh      [running 9.83s since 21:03:11]                            ← 干净
#hb-proof-realstale  [running 4h10m6s since 16:53:14 + heartbeat stale 4h0m6s] ← 真 stale 仍在
```

两个方向都钉住了：`TestTaskHeartbeatReadingFallbacks` 断言 created_at-only 行不再
报 stale **且** `taskStallHit` 仍判它"行太老"；`TestFreshClaimNeverRendersStaleHeartbeat`
在 list 渲染面（`taskStateSuffix`）上跑同一对断言，含"记录过的心跳 4h 老 → marker 回来"。
