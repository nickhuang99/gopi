<!-- doc-class: historical -->
# 任务 01a12b01 brief 的论证更正（任务 01a12b06）

**出具日期**：2026-10-11 · **主机**：hosta（hosta）· **账本**：`/home/user/.gopi/memory.db`
**更正对象**：行 `01a12b01-1c35-71cf-a554-b578dad6c6a5` 的 briefing
**原始证据目录**：`~/.gopi/sessions/01a12b06-1875-706b-8726-af98c19cb602/evidence/`

## 0. 这份文档要解决什么

`01a12b01` 的 brief 里有**两处因果论断**，来源是**代码注释**而不是观测。本任务的职责是
撤回它们、保留可实测的部分、改写首要问题，并且**给每一条依据标注来源**。

判据的来源只有两种，本文件每条都标：

| 标记 | 含义 |
|---|---|
| **实测** | 数据库字段值 / append-only 事件表 / 操作者通知汇 / 正反对照 —— 给出命令与输出文件 |
| **推断** | 来自代码注释、文档措辞或作者自述 —— **不得单独作为判据**（本任务的主旨） |

## 1. 来源标注表

| # | 依据 | 来源 | 命令 / 输出 |
|---|---|---|---|
| S1 | 两条僵尸行 `status='in_progress'` 30 天未变，`in_progress_at=NULL`，`ground` 全空，session 已 prune，run dir 从未创建 | **实测** | `q1_cohort.py` [2] → `01-cohort-raw.txt` |
| S2 | 这 2 行的 `task_events` 只有 `reframe`（09-11）与 `cancelled`（10-11 手工）——**30 天零状态事件** | **实测** | `q1_cohort.py` [5] → `01-cohort-raw.txt` |
| S3 | `updated_at` **确实被改写过**：通知汇 21 条 `stale`/行，末条 `timestamp=2026-10-11T20:42:28` | **实测** | `q6_notify.py` → `06-notify-sink.txt` |
| S4 | 每条 `stale` 事件快照了 bump 前的 `updated_at`，与事件时刻**恒差 1800s**（21 条全为 1800/1801） | **实测** | `q6_notify.py` / `q7_notify_cadence.py` → `06/07-notify-sink.txt` |
| S5 | `updated_at` 的**唯一**写入者是 `taskZombieBumpSQL`（`UPDATE tasks SET updated_at=? WHERE task_id=? AND status=?`） | **实测**（对工作树的代码检索）+ **推断**（"唯一"是检索结论，不是运行观测） | `grep -rn taskZombieBumpSQL internal/tools/*.go` → `10-update-before.txt` 旁的检索记录 |
| S6 | 该列的**语义**："write-churn column / 同步锚 / 通知 re-arm" | **推断**（代码注释与文档措辞） | 不作为判据 |
| S7 | 同批 32 条 `source='auto'`（2026-09-11 全天）中 30 条由自己的 run 写回终结，2 条没有 | **实测** | `q8_contrast.py` → `08-neg2-vs-pos30.txt` |
| S8 | `last_toolcall_at - claimed_at` 分布：+1s 有 4 条**全部正常终结**；僵尸行的 +2s/+3s 毫不特殊 | **实测** | `q8_contrast.py` §D → `08-neg2-vs-pos30.txt` |
| S9 | 30 条正例同样是 `claimed_by: peerbox:*`（跨主机） | **实测** | `q8_contrast.py` §A/§B → `08-neg2-vs-pos30.txt` |
| S10 | 检测器报出 `zombie.confidence="suspected"`，note 恒为 `heartbeat stop, no claim stamp — suspected` | **实测** | `q6_notify.py` → `06-notify-sink.txt` |
| S11 | `DetectStaleTasks` 只检测不处置（30 天 42 次通知、0 次终结） | **实测**（通知汇计数）+ **推断**（"不处置"由事件表零终结事件反证，未见其写路径） | `q6_notify.py` + `q1_cohort.py` [5] |
| S12 | `--status` 是朴素状态筛选，跨主机 claim 不隐藏任何行 | **实测** | `09-visibility.txt`（`gopi task list --status cancelled` 照常渲染两行） |
| S13 | 3 条 `reframe` 后代批次：被认领、`tasklist_issued_at` 晚于 `last_toolcall_at` —— 一只被**重排**的调度器 | **实测**（字段）+ **推断**（"重排"是解释） | `q8_contrast.py` §B |
| S14 | `CGO_ENABLED=0 go build` 下 `web/` 含 ELF，coreutils 损坏 | **实测** | 见 §5 Not Done |

## 2. 撤回的两条论断

### 撤回① —— "stale-sweep 的 re-arm bump `updated_at` 给僵尸续命"

**原文（01a12b01）**：僵尸行 `updated_at = 2026-10-11 20:42:28`，"恰是同机 worker `3864028`
认领另一条新任务的时刻，即那一轮 sweep"，故"hosta 每轮 worker 触发 stale-sweep …… re-arm
并把 `updated_at` 刷成当前时刻"。

**为什么它是推断**：它的根据是 `internal/tools/tasks.go` 的一段注释
（`updated_at is a write-churn column … stale-sweep re-arm bump all touch it`）。
作者把注释读成了观测 —— 这正是本任务要根治的错误类型。

**实测推翻了它的哪一半**：**"每轮 sweep 都在刷新"** 不成立，而且**时间戳被倒着念了**。

- 通知汇 `~/.gopi/notify/task-events.jsonl` 对每一行有 **21 条 `stale` 事件**，
  每条都**快照了 bump 之前的 `updated_at`**（`updated_at` 字段）。
- 事件时刻 − 快照 `updated_at` = **1800 秒或 1801 秒**，21 条**无一例外**。
- 因此 `updated_at = now − 1800`，而通知在 `now` 发出 —— **bump 先发生，整整一个 stall
  窗口之后才通知**。"20:42:28 刷新了它"是反的：**20:42:28 是通知时刻，bump 发生在 20:12:28**。
- 另外，通知汇**只含 `stale` 与终态事件**（125 行），所以它能证明"被 bump 过 21 次/行"，
  **不能**用它反推 sweep 的触发频率；`1800s` 是**配置的 stall 窗口**，不是观测到的触发间隔。

**仍然成立的部分（改写保留）**：`updated_at` 的确**会被 stale 通知改写**（实测 21 次/行），
所以"判活死不得依赖它"**依然正确** —— 但理由变了：**该列是 write-churn / 同步锚 / re-arm 三用**，
不是"它已在给僵尸续命"。

**更正后的着力点**：`DetectStaleTasks` **检测而不处置** —— 30 天 42 次通知、0 次终结。
这才是"僵尸永远不被回收"的机制。

### 撤回② —— "认领后 2 秒即中断，说明它死了"

**原文的观察**：两行的 `last_toolcall_at - claimed_at` 为 +2s / +3s。

**实测推翻**：同批 32 行同一字段的分布里，**+1s 有 4 条，全部正常终结**：

```
01a08e1a-8a6e +1s completed   01a08e80-4fea +1s completed
01a08e80-b62e +1s completed   01a08e9c-6fbb +1s completed
01a08e8d-16ec +2s completed   01a08e9b-4573 +2s ← 僵尸行
01a08e9e-7731 +2s completed   01a08e78-55d2 +3s completed
01a08e7f-ccaa +3s ← 僵尸行   01a08e81-3c3e +3s completed
```

**"短"既不是死因也不是判据**，不要在这个差值上建立任何判据。

*（附带更正：原 brief 写"同批 31 条",本次实测口径 `source='auto' AND created_at ∈ 2026-09-11 全天`
得 **32** 条；"31"应是"11:20–12:00 窗口内 24 条"与"11:25–11:56 的 id 族 24 条"之外的另一个口径。
两个口径都指向**同 2 行**。）*

## 3. 保留的 4 条已确证事实（每条一条命令可复现）

| # | 事实 | 一条命令 |
|---|---|---|
| **F1** | 同批 32 条 `source='auto'` 行中 **30 条由自己的 run 写回终结，恰 2 条没有**。判据可测：`ended_at - created_at ≤ 3600s` 记作"自己的 run 写的"；这 2 行为 `+2625585s` / `+2623783s`（30 天后的手工 `cancelled`） | `python3 $SD/q8_contrast.py \| head -4` |
| **F2** | 这 2 行 `status` 自 09-11 未变，`in_progress_at=NULL`（原 `ended_at=NULL`），`claimed_by` 指向 peerbox，session 已 prune，run dir 从未创建，`ground` 全空 | `python3 $SD/q1_cohort.py \| sed -n '/\[2\]/,/\[3\]/p'` |
| **F3** | **`task_events` 30 天零状态事件，而 `updated_at` 被改写过 21 次/行** —— 两个观测面结论相反，只看一个会误判 | `python3 $SD/q6_notify.py \| tail -12` |
| **F4** | **同批 30 条兄弟行正常终结** ⇒ 终结路径本身是通的；32 行里 25 条有 `claimed→last_toolcall` 窗口，C 段的"同时刻竞争者"对照因此可做 | `python3 $SD/q8_contrast.py \| sed -n '/=== C\./,$p'` |

`$SD = ~/.gopi/sessions/01a12b06-1875-706b-8726-af98c19cb602/evidence`

## 4. 首要问题（已改写）

> **同一批 32 个 auto 任务里，为什么恰恰这 2 个的终结状态没有被自己的 run 写回？**

"为什么 30 天没被回收"是它的**后果**。该调查已另立 **`01a12b08`**（本行的子任务）。

## 5. Not Done（诚实申报）

- **重写后的 brief 能否真正把执行者带回正轨，本次没有验证**，也无法验证：它要等一次
  **新运行**（`resume`/重派）从头读这个 brief。本任务交付的是**机制与实测证据**，
  不是"新运行已修正"。
- **"为什么这 2 行没写回"没有在本任务里回答** —— 那正是 `01a12b08` 的题目。
- 一条本可做的对照（对 2 行与 30 行的 duration/格式做**因果**归因）**未做**：证据指向的是
  "这 2 行的 agent 分支死于第 3–8 秒"（`reframe×3` 后 `last_toolcall_at` 停止），
  但一个"活着"的行只需要 ≥1 条 `reframe` 就足以走到 `completed`（30 条里有 8 条 `reframe×1`），
  因此这条线索**不足以解释**，不得写成判据。
- 本机 `CGO_ENABLED=0 go build` 不可用（`web/` 里有 ELF，coreutils 被配套 toolchain 破坏）；
  本任务**没有改任何 Go 代码**，交付物是数据 + 文档，未受影响。

## 6. Verification（按 Rule 5）

- **Build**：不适用（未改 Go 代码）。仓库侧仅新增一个 `devdoc/` 文档。
- **Tests**：未跑；本分支未触及 `internal/` 或 `cmd/`。见 `task-report.md`。
- **Ran**（逐条真实执行，原始输出在 evidence/）：
  ```
  python3 $EV/q1_cohort.py       > $EV/01-cohort-raw.txt
  python3 $EV/q2_batch.py        > $EV/02-batch-29v2.txt
  python3 $EV/q3_paobs.py        > $EV/03-pa-verbatim-observations.txt
  python3 $EV/q4_cohort_auto.py  > $EV/04-cohort-definitions.txt
  python3 $EV/q5_all32.py        > $EV/05-auto-day.txt
  python3 $EV/q6_notify.py       > $EV/06-notify-sink.txt
  python3 $EV/q8_contrast.py     > $EV/08-neg2-vs-pos30.txt
  gopi task list --status cancelled > $EV/09-visibility.txt
  gopi task update 01a12b01-1c35-71cf-a554-b578dad6c6a5 \
      --payload @$EV/01a12b01-payload-CORRECTED.md --reason "更正 01a12b06:…"
      → "Task payload updated: 01a12b01-… (payload=7661 bytes, was 4961)"
  ```
- **Result**：见 `$EV/12-reach-mechanism-verified.txt` —— brief 已换、`payload_revised`
  事件已落、旧 payload 可逐字取回（sha256 `7585c974…`，4961 字节，与 `tasks.payload` 的原值一致）。
- **Commit**：见 `task-report.md`。

## 7. 更正如何抵达执行者（机制）

### 7.0 更正到不了的地方（诚实申报）

`tasks.summary` 是**不可变**列（账本 append-only 契约：summary immutable、无 delete），所以
`gopi task list` 那一行**仍然显示旧措辞**：

```
#01a12b01-…  [pending]  stale-sweep 给僵尸 in_progress 行续命而非终结:updated_at 是 write-churn 列被 re-arm 不断 bump…
```

`gopi task update` 的可用臂只有 `--status / --payload / --reason / --result / --repo /
--parent-task-id / --clear-parent`，**没有改 summary 的臂**。**正确读法**：**扫列表时不要信 summary，要点进去看 payload** —— 更正后的 brief 在 payload 里（顶部引用块）。这是报给账本设计的一个已知缺口：**"brief 可更正、summary 不可更正"，而 summary 恰恰是操作者扫列表时唯一看到的那行字。**


`01a12b01` 的 payload **原地替换**，不是另写一份文档：

```
gopi task update 01a12b01-1c35-71cf-a554-b578dad6c6a5 \
    --payload @.../01a12b01-payload-CORRECTED.md --reason "更正 01a12b06:…"
→ Task payload updated: 01a12b01-… (payload=7661 bytes, was 4961)
```

- **执行者的读面就是 `tasks.payload`**，所以下一次运行读到的就是更正后的 brief；
- 更正在 brief **顶部**以醒目引用块给出，并**保留**原 A/B/C/D 要求（只改首要问题与 A 段理由）；
- **旧 payload 逐字留存**：`task_events` 新增 `payload_revised` 事件（旧/新 sha256 + 字节数），
  审计面完整；
- **可验证**：`gopi task show 01a12b01-1c35-71cf-a554-b578dad6c6a5 | grep 撤回①`
  命中更正块 —— 见 `12-reach-mechanism-verified.txt` §E2。
