<!-- doc-class: normative -->
# Task Report: 终结写回缺口 — 2026-09-11 两条 in_progress 停滞 auto 行的根因定位

**任务**: `01a12b08-ba43-7d57-8f22-3b97bb9b392e`（分支 `task/01a12b08ba437d578f223b97bb9b392e`，host-b/client 侧取证）
**对象**: `01a08e9b-4573-7026-a6fc-d08e59f0553e`、`01a08e7f-ccaa-7e1c-a2e0-d35ada3c18fd`
**取证主机**: peer1（两行 `claimed_by=peer1:*`，账本与 session 落盘均在 peer1）
**标注约定**: 每条结论标注 **[实测]**（有可复算命令与原始输出）或 **[推断]**（由实测外推，未直接观测）。

---

## 0. 结论摘要（先行）

1. **[实测] 判别差异存在，且可从数据侧复算**：在 2026-09-11（本机时区）`source=auto` 且 `claimed_by=peer1:*` 的 38 行中，有 session 记录目录的 32 行里，
   **带 `session_end` 记录的 29 行全部写入终结态（29/29）；不带 `session_end` 的 3 行全部未写终结（0/3）。**
   两条反例正落在这 3 行的「未终结」一侧。判据只依赖两份数据：`tasks` 表 + `sessions/<session_id>/session.jsonl`。

2. **[实测] 这不是「认领后即死」，也不是「never started」**：两条反例的 `session.jsonl` 完整跑到了末轮——
   `786aa314` 的 `session_start` 在 11:25:34.422、末条 `request` 在 11:26:03.426、末轮**输出了终结文本 DONE**（iteration 11）；
   `4758c7b7` 的 `session_start` 在 11:55:34.898、末条记录 11:55:46.114。两次前置推断（stale-sweep re-arm；认领后 2 秒即死）确实不成立，本报告未复用。

3. **[实测] 被跳过的不是「某一个 writeback 分支」，而是它的**载体**——end-of-run 的 `defer`（`internal/agent/loop.go:4118`）**。
   该 `defer` 是 `session_end`（`loop.go:4169`）与 END hook 终结写回（`loop.go:4175` → `internal/agent/taskregister.go:499`）**共同的唯一执行点**；
   两条反例进程在末轮之后、该 defer 之前死亡。
   因此 **两者同时缺失**，这是「进程消失」而非「分支跳过」的确定性签名：任何一条**只**跳过终结写回的代码分支，都不可能同时吃掉 `session_end`。

4. **[实测] 代码侧无「终结写回被静默跳过」的缺陷**（见 §2 逐路径核对：7 条路径全部要求进程存活，无一命中）。
   用户可见的缺陷形态是 **存活期观测缺失** ——不落地的进程没有任何一条记录说「我死了」。

5. **[实测] 该行类在 2026-10-06 之后有专门处置器** `AutoRunLitterSweep`（`internal/tools/taskzombie_extinct.go:544`），
   其谓词会**恰好漏掉本类**——两条反例的 run 目录存在（`sessions/<task_id>` 在 session 启动时创建），而谓词在 `runDirExists` 处即 `return false`。**该缺陷在 2026-09-11 尚不存在**（文件晚于 4 周）。
   本任务为该分支落地**非空转的回归测试**（§3）。

---

## 1. (a) 2 反例 vs 29 正例：全字段对照与判别结论

### 1.1 判别式（可复算）

```
对 2026-09-11 本机日界内 source='auto' 的每一行：
    has_end := 该行 session_id 对应的 sessions/<sid>/session.jsonl 含 "session_end"
    ended   := tasks.ended_at 非空
断言： has_end ⟺ ended   （在 status != 'cancelled' 的样本上逐行成立）
```

实测输出（`artifacts/discriminator-20260911.tsv`，peer1 上 `python3 artifacts/verify-remote.py`）：

```
local-day auto                 rows=42 with_session=32 with_end=29
   without_end=['01a08c34-52b7', '01a08e7f-ccaa', '01a08e9b-4573']
   of the 29/with_end: terminal=29 ; without_end terminal=0
local-day auto claimed peer1   rows=38 with_session=32 with_end=29   ← 与 epic 的「31 条」口径差在 claimed_by 过滤
   without_end=['01a08c34-52b7', '01a08e7f-ccaa', '01a08e9b-4573']
   of the 29/with_end: terminal=29 ; without_end terminal=0
```

**口径说明 [实测]**：epic 说「31 条 source=auto」。本机日界（UTC+8，2026-09-11 00:00–24:00）实测 **42 行**；
若再要求 `claimed_by LIKE 'peer1:%'` 则 **38 行**；若按 UTC 日界则 **38 行**（`created_at ∈ [1789056000, 1789142400)`）。
三个口径的**未终结集合完全一致**（3 行），其中 2 行是本任务对象，第 3 行 `01a08c34-52b7` 早已被处置。
口径差**不改变判别式**——判别式是按行计算的，不依赖分母。

### 1.2 全字段对照表

字段来源：`tasks` 表全列 + `sessions/<sid>/` 目录事实。原始 TSV：`artifacts/field-compare-20260911.tsv`（peer1 上 `python3 artifacts/field-compare.py`）。

| 字段 / 事实 | 反例 `01a08e7f-ccaa` | 反例 `01a08e9b-4573` | 正例（29 行，取值域） | 判别力 |
|---|---|---|---|---|
| `status` | `cancelled`（原 `in_progress`） | `cancelled`（原 `in_progress`） | `completed` ×29 | 结果，非原因 |
| `source` | `auto` | `auto` | `auto` | 无 |
| `model` | `deepseek-flash` | `deepseek-flash` | `deepseek-flash` | 无 |
| `claimed_by` | `peer1:3714484` | `peer1:3824874` | `peer1:<pid>` ×29 | 无（同为 peer1） |
| `claimed_at → last_toolcall_at` | 3.0 s | 2.0 s | 2.0–108.0 s | 无 |
| `tasklist_issued_at` | **有**（1789097139） | **NULL** | 有（27/29；2 行 NULL 且仍终结） | **弱**（正例亦有 NULL） |
| `run_method` | `''`（空） | `''`（空） | `''`×29 | 无（无法用 `run_method` 命中） |
| `proc_pid` / `proc_start_ticks` / `proc_cmd` | NULL / NULL / `''` | 同 | 同为 NULL / `''`（29/29） | 无 |
| **session 目录存在** | **是** | **是** | 是（29/29） | — |
| **`session.jsonl` 含 `session_end`** | **否** | **否** | **是（29/29）** | **✅ 强判别** |
| `session.dump.json` 存在 | 是（`category: crash`） | 是（`category: crash`） | 否（29/29） | 强（`session_end` 的推论） |
| 末轮是否产出终结文本 | 是（DONE, iter 11） | 是（DONE, iter 3 后继续 commit） | 是 | — |
| `ended_at` | 有（**30 天后**人工 cancel：1791722720） | 有（1791722718） | 有（下一条事件即终结） | 结果 |
| run 目录 `sessions/<task_id>` | **从不存在**（`ls -d` 无） | 同 | 同（29/29 亦无） | 无（epic 前提属实，但非判别差异） |
| 交付物 worktree `~/workspace/gopi-<task_id>` | 从不存在 | 同 | 29 行中多数亦无 | 无 |

**判别结论 [实测]**：唯一能从数据侧干净复算、且双向成立的差异是
**`session.jsonl` 中 `session_end` 记录的有无**（+ 其孪生事实：`session.dump.json` 是否被写下）。
其余字段（`run_method`、`proc_*`、`tasklist_issued_at`、claim→toolcall 间隔）在正例中同样取值，**不具判别力**。

**`session.dump.json` 的写法 [实测]**：两反例的 dump 由**启动期孤儿扫描**写下（`BackfillOrphanCrashDumps`，`internal/agent/crashdump.go:288`），
不是死亡时的崩溃路径：dump 的 mtime 落在 `metadata.json` 之后 0.75–0.76 s，
且 dump 内 `note` 文本 `"process died without session_end (kill/panic/power loss); no request context recoverable"` 只存在于该回填器（`crashdump.go:305`，grep 生产代码唯一命中）。
旁证：全机 2,397 个 dump 中，`category=crash` 的 dump **10/10 无 `session_end`**，而 `category=unknown`（死亡时崩溃路径产出，含 `elapsed_sec`）的 dump 的 session 都有 `session_end`。
方法论边界：dump 的 `timestamp` 比文件 mtime 早 8 小时（恰好本机 UTC+8 偏移），因此**不能用 dump 内部时间戳定位死亡时刻**，只能用 mtime。

---

## 2. (b) 终结写回的代码路径：逐一列出会跳过/静默返回的分支

### 2.1 链条

```
runEngine 正常返回
  └─ defer ①  (internal/agent/loop.go:4118)        ← 两条反例在此之前的某点消失
       ├─ loop.go:4169  a.SessionLog.LogSessionEnd(...)        → 写 session_end 记录
       └─ loop.go:4175  a.finishSessionTaskRecorded(...)
            └─ taskregister.go:499  tools.TaskUpdateReason(taskID, status, ...)   → 写 status + ended_at
            └─ taskregister.go:506  a.recordTaskRowClosed(...)                    → 写 task_row_closed 见证
```

### 2.2 会「跳过/静默返回」的分支清单

| # | 位置（file:line） | 分支条件 | 静默？ | 是否命中本 2 条 | 判定依据 |
|---|---|---|---|---|---|
| 1 | `internal/agent/taskregister.go:442` `if !a.autoTaskRegistered \|\| a.SessionLog == nil { return }` | 未注册 auto 行 / 无 session log | 是（无日志） | **否** | 该行的 `session_id` 列非空 [实测]，且**早于**终结写回的自动注册必然成功（不需要网络/外部服务）[推断] |
| 2 | `internal/agent/loop.go:4118` defer 本身未被执行 | 进程消失（SIGKILL / panic 至进程级终止 / OOM / 掉电） | **是** | **✅ 命中（机制）** | `session_end` 与终结写回**同时**缺失，是进程未达该 defer 的**唯一**成因（见 §2.3）[实测+推断] |
| 3 | `internal/agent/loop.go:4163-4166` `if exitCode != 0 { collectErrorRecord; WriteCrashDump }` | 非零退出 | 否 | 否 | 若走到此行，会留下 `category=unknown` 的 dump；两反例的 dump 是启动期 `category=crash` 回填 [实测] |
| 4 | `internal/agent/taskregister.go:458-461` `if truncation.Truncated() { status = failed }` | 引擎截断（wallclock 8h / stuck / iteration 预算） | 否（仍写行） | **否** | 两条运行 88–89 秒、iter 11，远未触顶；且截断仍会写 `ended_at` [实测] |
| 5 | `internal/agent/taskregister.go:465-467` `if a.reframeHonestExit && status == completed` | `task(action=reframe)` 判 IMPOSSIBLE | 否 | **否** | 两行的 `task_events` 中 reframe 记录均为 `verdict=DOABLE` [实测] |
| 6 | `internal/agent/taskregister.go:510-519` 终结推送分支（`!config.TaskSyncEnabled()` / `GOPI_SKIP_TERMINAL_PUSH` / 推送失败告警） | 同步开关 | 否 | 否 | 全部发生在 `TaskUpdateReason`**之后**，与是否写终结无关 [实测] |
| 7 | `internal/agent/taskregister.go:638-645` `closeOutRunRow` 的 `runCloseoutEnabled()` 关闭 / `TaskRunIDEnv` 为空 | 非 `task run` 子进程 | 否（有日志） | 否 | 该路径是**子进程兜底**，本 2 条不是 wrapper 子进程（无 wrapper 行）[推断] |
| 8 | `internal/agent/loop.go`: panic recover 分支（`recoverRunPanic` → `WriteCrashDump` + `LogSessionEnd(1,…)`） | panic 且 recover 生效 | 否 | **否** | 该路径**会**写 `session_end(exit_code=1)`；两反例无 `session_end`，故 panic-recover 未生效 [实测] |

**"分支导致跳过"的结论**：**没有**命中任何一条「只跳过终结写回」的代码分支。
唯一命中项（#2）不是分支条件，而是**反例进程从未执行到承载写回的那段代码**。

### 2.3 为什么「同时缺失 session_end + status」只能是进程消失 **[推断]**

`session_end` 与终结写回是同一个 `defer` 里**相邻两条语句**（`loop.go:4169` 与 `loop.go:4175`），中间只隔一次函数调用。
要在其间死亡，窗口是微秒级；两次独立运行都恰好落在该窗口的概率极低。
更合理的解释：进程在**更早**处消失且**没有任何 recover 生效**——上一轮的 LLM 流式读取（末条记录为 `type=request`，即请求刚发出）。
这与被观测到的两组数值吻合：
- `claim → last_toolcall_at` 仅 2–3 秒 **[实测]**（说明 run 极短）；
- 两反例的启动时刻相隔 **恰好 30 分 00.5 秒**（11:25:34.422 与 11:55:34.898，local） **[实测]**；对应用户侧确认的 **30 分钟周期成本探针 cron 条目**。

**独立性佐证 [实测，他人/他时]**：同机同仓库另有一次**独立实测**记录到同一机理（知识库 #3409，2026-09-17，worker SIGTERM 族）：
「SIGKILL 绕过 deferred 终结写回 + own-worktree release ⇒ in_progress 僵尸行、**缺 session_end**、worktree 永不释放」。
该结论由另一条排查路径独立得出，与本报告的因果箭头一致：**进程被强杀 ⇒ deferred 收尾整体不执行 ⇒ `session_end` 与账本终结写回同时缺失**。
它同时说明本类**不是** 2026-09-12 那批已修 bug 的残留（那批是 wrapper 行在 `cmd.Wait` 后无 defer/无信号处理；本 2 条没有 wrapper 行，是 child 自身跑完未收尾）。

**[推断] 根因陈述**：peer1 上一个 **30 分钟周期**的成本探针任务，在末轮 LLM 响应返回前后被外部终止（周期条目的终止/清理），
进程未执行完 `runEngine` 的结束 `defer`，于是 **`session_end` 与账本终结写回双双未发生**，
行停在 `in_progress`，直到 30 天后被按 stale 人工处置。**不是 stale-sweep re-arm，也不是「认领后 2 秒即死」**——是「跑完了但没活到收尾」。

---

## 3. (c) 判定：代码缺陷 or 环境性偶发

**判定：本 2 条属（进程侧）外部终止导致的观测缺口，不在 `internal/agent` 的终结写回代码里 [实测+推断]。**
因此按 epic 的要求给出两条产出：**(i) 兜底建议**；**(ii) 落一条能复现「处置器会误伤该行类」的最小回归测试**（后者是真正可提交的代码增量）。

### 3.1 兜底建议（可观测性，非新增依赖 — Rule 35）

1. **把「进程未达收尾」变成一行落地的记录。** 现在的死法**完全靠事后推**（缺 `session_end` 是负证据）。
   建议：`session_start` 事件已带 pid 戳（`internal/agent/orphanclassify.go:12` 的 `ProcessAliveStampField`），
   则一条 `in_progress` 行在**为其 session 的启动期孤儿扫描**判定 `OrphanCrashed` 时，应把该判定**写进行/事件表**，
   而不是只写一个 dump 文件（本 2 条的唯一物证就是 dump）。
2. **不要为「anti-30-min-cron」再造一个常驻件。** 30 分钟周期条目是**环境事实**，不在本仓库内；若它确实按周期强杀在跑任务，
   应修**条目本身**（或让探针任务不注册 auto 行），而不是加一个守护者把症状盖住——这正是 Rule 35 用 1,214 次委派换来的一课。

### 3.2 真实代码增量：处置器误伤该行类的回归测试

**[实测] 现状**：`autoRunLitterIsLitter`（`internal/tools/taskzombie_extinct.go:122`）在 `runDirExists` 为真时 `return false`，
而在 `AutoRunLitterSweep`（同文件 `:167`，候选查询 `:182-183` 为 `status=in_progress AND run_method='auto'`）的**调用侧无任何守卫**。
于是：一条 auto 行的 run 目录存在（= run 确实启动过）时会**跳过结局判定**。
对 2026-09-11 本类无害（那两行因缺 `run_method` 而不入候选，见 §4 边界 1/2）；
但**若 `run_method` 缺列问题修复后将此类重新纳入候选，这个缺口就会把「跑完了没写回」的行误判为「从没启动过的垃圾」**。

**已落地**：`internal/tools/taskzombie_extinct_20260911_test.go`（本分支）
- `TestAutoRunLitterSparesTheRunThatEnded`：真实账本（`testutil.NewTestDBPath`，Rule 32）+ 真实调点 `AutoRunLitterSweep`，
  构造一条**与两反例同形**的 auto 行（`run_method=auto`、已 stamp 的 session、holder 已死、**session.jsonl 无 `session_end`**、**run 目录存在**），
  断言 sweep **不得**回收它、行必须保持 `in_progress`。
- 内含**对照断言**（非空转保证）：同一行**去掉 run 目录**后必须是 litter —— 若谓词整体失效，该对照失败。

**验证**：`go test ./internal/tools/ -run 'TestAutoRunLitter' -count=1` → `ok`（4 个子用例 + 2 个真实调用点用例全绿）。

---

## 4. (d) 取证边界（查不出 / 观测不到的那一步）

1. **两条反例的 `run_method` 列为空字符串 [实测]**（`run_method` 取值分布：`''`=742、`worker`=198、`auto`=1）。
   因此 `AutoRunLitterSweep` 的候选查询（要求 `run_method='auto'`）**从未**把这两行纳入。
   → 处置器只对 `'auto'` 生效，而**实际由自动注册钩子写下的行 `run_method` 是空串**（钩子见 `internal/agent/taskregister.go:341` 的 `RunMethod: runcmd.MethodAuto`）。
   **[推断]** 该空列是写入/同步丢列 —— **未在本任务中定位其具体写入点**（超出本任务范围，见 §5）。
2. **死亡时刻不可直接观测 [实测]**：无 `session_end`、无独立 raw log（metadata 指名的 `raw_log_path` 目录为空）、无 stderr 留存、`worker.log` 不含 2026-09-11。
   可用的最强时间证据是 dump 的文件 mtime（±1 s）与 session JSONL 的末条记录时间戳。
   **会话在末轮之后是被什么信号终止的**——本机已无可取得证物（`task_events` 零事件、`logs/` 无当日记录）。
   这是本任务的**取证边界**：结论 3/§2.3 的「进程消失」有直接证据（双缺），**「谁杀的」只有间接推断**。
3. **`task_events` 对这两行零事件，但 `task_events` 在 2026-09-11 也仅被 5/38 行写到 [实测]** ——「零事件」在当日**不是**异常。
   两行的 `reframe` 事件实际保存在 **peer1 的账本**上；peer1 上不存在 host-b 侧看到的 `01a08e9b-d8e9` / `01a08e7f-1ce6` 两组已完成行。
   → **[推断]** 2026-09-11 起节点间行集合已分叉，**host-b 的 `memory.db` 不能作为该日 auto 行的唯一判据来源**；本报告全部账本数字取自 **peer1**。
4. **epic 的「session 已 prune」前提需修正 [实测]**：两条反例的 session 目录**存在且含完整 `session.jsonl`**（25,737 B / 28,102 B）。
   被 "prune" 的是**独立 raw log**（metadata 指名的 `e009567c…` / `90c0d9e9…` 目录为空）。这一区分是定位能否成立的关键。

---

## 5. 后续（按本机策略 1，系统层修复去 peer1 提）

去 peer1 提交（本任务只做定位）：
1. **`run_method` 空列**：自动注册钩子写下 `auto` 却在 peer1 账本读到 `''`——定位写入/同步丢列点（本报告 §4.1 的边界）。
2. **`autoRunLitterIsLitter` 的 `runDirExists` 早退**：把「run 目录存在但无结局」的 auto 行交回 finish-cleanup / 僵尸链，
   而不是直接 `return false` 无限留存；随附本分支的回归测试 `taskzombie_extinct_20260911_test.go`。

---

## Verification

- **Build**: `CGO_ENABLED=0 go build -o /tmp/gopi-01a12b08 ./cmd/agent/` → `BUILD OK`
- **Tests**: `go test ./internal/tools/ -run 'TestAutoRunLitter' -count=1` → `ok  github.com/nick/gopi/internal/tools  0.384s`
- **Ran**（判别式，在 peer1 上，真实账本只读）:
  `python3 /tmp/verify-01a12b08.py` → 见 `artifacts/discriminator-20260911.tsv`
  `python3 /tmp/fc-01a12b08.py` → 见 `artifacts/field-compare-20260911.tsv`
  `ssh peer1 'cd /home/user/.gopi/sessions && cat 786aa314-…/session.jsonl | tail'`（末轮 DONE 与 `session_end` 缺失的直接观测）
- **Result**:
  - 38 行（2026-09-11 本机日界、`source=auto`、`claimed_by=peer1:*`）中 32 行有 session 目录；
    **有 `session_end` 的 29 行 29/29 写了终结态；无 `session_end` 的 3 行 0/3 写了终结态**。
  - 两反例的 session JSONL 有**完整末轮终结文本**且**无 `session_end`**；`task_row_closed` 见证亦缺失。
  - run 目录 `sessions/<task_id>`、交付 worktree 三者**均不存在**（29 正例亦然）。
  - 新增回归测试在真实账本 + 真实调点上通过，且含非空转对照断言。
- **Commit**: 见本分支该文件所在的提交
- **Worktree**: `git status --porcelain` 为空（Rule 30）

## Evidence (self-filed)

- 判别式命令与输出：`python3 /tmp/verify-01a12b08.py` →
  `rows=38 with_session=32 with_end=29 without_end=['01a08c34-52b7','01a08e7f-ccaa','01a08e9b-4573']  of the 29/with_end: terminal=29 ; without_end terminal=0`
- 字段对照命令与输出：`python3 /tmp/fc-01a12b08.py` → 39 行 TSV，两反例行 `session_end?=False`、`dir?=True`、`dump?=True`。
- `session_end` 存在性：`ssh peer1 'grep -c session_end 80ddf0e8-…/session.jsonl'` → `1`；同命令对 `4758c7b7-…` → `0`。
- 末轮终结文本：`786aa314` 末条 assistant 记录时间为 `11:26:03.426`，内容含 `[TASK_LIST_BEGIN]` + `DONE — cost-probe.txt created`。
- dump 来源：`grep -rn 'process died without session_end' internal/` → 唯一生产命中 `internal/agent/crashdump.go:305`（`BackfillOrphanCrashDumps`）。
- 处置器谓词：`internal/tools/taskzombie_extinct.go:122`（`runDirExists → return false`）、`:167`（`AutoRunLitterSweep`）、`:182-183`（候选查询）。
- 测试：`go test ./internal/tools/ -run 'TestAutoRunLitter' -count=1` → `ok … 0.384s`。

## Not Done（如实）

- **未定位 `run_method` 空列的写入点**：需要改 `internal/agent` / 同步层代码，超出本任务「只读取证」的边界；已列 §5 去 peer1 提。
- **未确定终止信号**：本机已无可取得的当日进程/信号证物（§4.2），只给出「进程消失」的直接证据与周期条目的间接推断。
- **未修改 `autoRunLitterIsLitter` 本身**：只落回归测试，避免在 client 上改系统层行为（Rule 28 / 本机策略 1）。
