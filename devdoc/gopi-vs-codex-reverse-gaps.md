<!-- doc-class: normative -->
# 反向竞品研究：gopi vs codex — "我们有而 codex 没有"的逐条实证

> 补轮报告（2026-09-30）。**前作**：`devdoc/gopi-vs-codex-competitive-analysis.md`（196 行，2026-08-29，证明 codex 强在哪）。
> 本报告是它的**反向面**：把 gopi 的 8 条机制拿到 codex 源码里逐条查证有无对应物，形成实证清单，再与
> `devdoc/competitor-dsh-analysis.md`、`devdoc/competitor-claude-code-behavior.md` 汇总成三方对照。
> 前作不重写、不推翻；本报告只补它没写的那一半。
>
> **材料**：codex 源码只读 `~/work/codex` @ `ad9e809`（`codex-rs`，Rust）；gopi @ `3c0e926b`（本 run 分支）。
> **方法**：每条机制先在 gopi 侧定位锚点，再到 codex 侧做**穷尽式同名/近义检索**（append-only / ledger /
> parent / claim / merge queue / push / worktree / GC / reclaim / sync / peer / seal / evidence），
> 命中即读实现，读不到即记 `未判` 并写明"查了哪些词、查了哪些 crate"。**只读，未改 codex 一个字节。**

---

## 0. 摘要（TL;DR）

| # | gopi 机制 | codex 侧对应物 | 判定 |
|---|---|---|---|
| 1 | append-only 台账（跨会话任务账本） | 有 append-only **log**（rollout / trace / message-history），但**没有任务台账**；`thread_goal` 是同会话目标 | **赢（台账）+ 平（append-only 存储形态）** |
| 2 | 父子依赖闸门（parent 未释放则子不可 claim） | 有 `thread_spawn_edges` 表与 Open/Closed 状态，**但无 claim 谓词**——数据面有、决策面无 | **部分赢（决策面缺）** |
| 3 | 合并队列 + 推送闸门 | **未判**：`git merge` 在非测试代码中零命中；codex 把"提交/PR"写进提示词（attribution），但**没有 merge 机器** | **赢（无对应物）** |
| 4 | worktree 生命周期（每任务一块地、自动回收） | **无**：codex 甚至显式声明**不识别** `git worktree add` 的检出 | **赢（无对应物）** |
| 5 | scratch GC（有界临时树 + 年龄/持有者谓词） | 有**同形但窄**的回收：`cleanup_stale_snapshots`、rollout 压缩 temp 清理、maintenance lock | **赢（广度与统一谓词）** |
| 6 | 跨节点同步（两主机台账/知识库互推） | **无**：存储层只有 `ThreadStore` 抽象与 Local/InMemory 两个实现，无对等端、无同步格式 | **赢（无对应物）** |
| 7 | 零依赖单二进制 | **否**：145 万行 Rust / 97 crate / 1359 锁包 + npm 平台包分发 | **赢（明确）** |
| 8 | 证据固化（报告封口 + gate-claim/Ran 记录） | **无对应物**：无 seed/report/claim 概念 | **赢（无对应物）** |

**一句话**：codex 强在**平台化**（工具注册表、多智能体、Code Mode、记忆自动管线、沙箱三档——这些前作已证，本报告不重复），
gopi 强在**执行治理**（一块地、一条队列、一道闸门、一份封口证据）。前者是"能力面"，后者是"可信面"；
**codex 把 agent 做得更大，gopi 把 agent 做得更可追责**——两者不是同一条轴上的强弱。

---

## 1. 判定口径（写给复核者）

- **赢** = codex 侧**检索无对应物**（穷尽式）或对应物在**决策面缺失**（有数据无谓词）。
- **平** = 两侧都有同形机制，取舍不同。
- **未判** = 本 run 的检索**不能证明"没有"**（可能藏在生成物/运行时/服务端），逐条写明查了什么。
- 我不把"codex 没做"读作"codex 做不到"：codex 是**面向单机单会话的 agent**，台账/队列/多节点不是它的目标；
  这是**定位差异**，不是缺陷。判定只陈述"源码里有/没有"，不含价值褒贬。
- **Census（Rule 38）**：所有规模数字由 `evidence/census.sh`（本 run 会话目录，只读、不含任何写操）生成，
  输出原件 `evidence/census.out`，两侧 rev 在脚本首行打印。本报告引用的数字与之逐字对应。

```
== codex rev: ad9e809            == gopi rev: 3c0e926b
codex.rust_files: 3256           gopi.go_files: 1847
codex.rust_loc:   1450748        gopi.go_loc:   659079
codex.crate_dirs: 97             gopi.go_mod_direct_requires: 28
codex.cargo_lock_packages: 1359  gopi.vendor_dir: absent
codex.rust_toolchain: channel = "1.95.0"
codex.npm_platform_pkgs: 6
```

---

## 2. 八条机制逐条查证

### 机制 1 — append-only 台账

**gopi 侧**：`internal/tools/tasks.go:295` `CREATE TABLE IF NOT EXISTS tasks (`，列含 `:303 parent_task_id`、
`:310 verify_evidence`、`:313 run_cmd`、`claimed_by` / `ended_at` / `reason`（`:646-647` 的 INSERT 列清单）。
跨会话、跨目录、单文件 `memory.db`。

**codex 侧查的**：`grep -rniE "append[_ -]?only|ledger" --include=*.rs`（全树）、`state/src/model/*`、
`thread-store/src/*`。

- **有 append-only 的"日志"，没有 append-only 的"账本"**：
  - `rollout-trace/src/lib.rs:51` "One append-only raw trace event from `trace.jsonl`"、`:77` "Append-only writer"；
    `rollout-trace/src/raw_event.rs:1,28`；`rollout-trace/README.md:109` "`trace.jsonl`: append-only raw events ordered by writer-assigned `seq`"。
  - `message-history/src/lib.rs:1` "Persistence layer for the global, append-only *message history* file"，`:143`；
    `message-history/src/batch.rs:60` "Codex writes append-only"。
  - `ext/memories/templates/memories/read_path.md:30` 把会话 rollout 描述为 "append-only `jsonl`"。
  - **可证明它被当作不可变事实源**：`thread-store/src/local/revert_thread.rs:17-18` —— "Old rollouts stay intact.
    The new file references the retained prefix, and the only mutable cutover is the existing SQLite rollout-path pointer"。
    即：**回退不改写历史，只改指针**——这是 append-only 纪律的确凿证据。
- **没有任务台账**：全树无 `tasks` 表、无 `claimed_by`/`claim` 于非记忆管线（`claim_*` 命中全部落在
  `state/src/runtime/memories.rs` 的记忆提炼 job lease，见机制 8）；`state/src/model/thread_goal.rs:14-21`
  的 `ThreadGoalStatus`（`Active/Paused/Blocked/UsageLimited/BudgetLimited/Complete`）是**线程内目标状态**，
  不是可被跨会话认领的工作项；`state/src/model/graph.rs:5-8` 只有派生边的 Open/Closed（见机制 2）。

**判定**：**台账 = 赢**（codex 无工作项账本）；**append-only 存储形态 = 平**（两侧都守；codex 的
`trace.jsonl`/rollout 甚至比 gopi 更纯粹，`seq` 由写入方分配）。

---

### 机制 2 — 父子依赖闸门（parent 未 completed/verified 则子不可 claim）

**gopi 侧**：`internal/tools/tasks.go:5864` `func taskParentGateDenied`、`:5982` `func taskRowClaimable`
——一个谓词同时服务 claim 漏斗与 pending 视图的 `[queue-skip]` 渲染（Rule 33）。

**codex 侧查的**：`parent_thread_id` / `spawn_agent` / `thread_spawn_edges` / `claim` / `gate`。

- **数据面完备**：`thread-store/src/thread_metadata_sync.rs`（`ThreadMetadataSync`，`:34`）、
  `state/src/runtime/threads.rs:123` `upsert_thread_spawn_edge`（`:131` `INSERT INTO thread_spawn_edges`）、
  `:150` `set_thread_spawn_edge_status`、`:214-216` 按 parent 查子线程、`:292-315` 若干按边查询；
  `core/src/agent/control.rs:728` `state.list_live_thread_spawn_edges()` 组装父子映射。
- **但"边"不是"闸门"**：`state/src/model/graph.rs:5-8` 的 `DirectionalThreadSpawnEdgeStatus { Open, Closed }`
  是**派生观测状态**（由 runtime 在 spawn/close 时写入，`core/src/agent/control/legacy.rs:56-70`
  `set_thread_spawn_edge_status(..., Closed)`），谁读它？答案是**控制面**（列出子 agent）与 **trace reducer**，
  不是**调度/认领**。codex 没有"工作项队列"，因此没有"父未释放则本期不可领取"这一步——闸门天然不存在。
- **唯一的真闸门是深度限制**（不同语义）：`core/src/tools/handlers/multi_agents/spawn.rs:66-69`
  `agent_max_depth` 超限返回 "Agent depth limit reached. Solve the task yourself."，
  `core/src/agent/registry.rs:91-92` `exceeds_thread_spawn_depth_limit`；那是**防递归**，不是**依赖顺序**。

**判定**：**部分赢**——父子**关系**两侧都有（codex 的边表甚至更结构化：显式 Open/Closed + 查询索引），
但**"关系 → 认领决策"的谓词 codex 侧缺失**。这一条的正确读法是：gopi 赢在**把关系当调度输入**。

---

### 机制 3 — 合并队列 + 推送闸门

**gopi 侧**：`internal/tools/task_sync_git_pushback.go:333` `gitPushbackSharesHistoryWithMaster`（推送前
`merge-base --all` 为空即拒，Rule 6 步骤 7）、`:591` `gitPushbackPlan`；merger 家族 20+ 文件
（`merger_status*.go` / `merge_contract_gate.go` / `merge_build_gate.go` / `merge_revert.go`）；
`CONFIG.md:459` `"build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"` —— repos 表既是 merger 的
扫描清单，又是合并后的重编译闸门（Rule 12）。

**codex 侧查的**：`grep -rniE "merge_queue|push gate|pre-?push" --include=*.rs`（全树，**零命中**）、
`grep -rn '"merge"' core/src git-utils/src cli/src app-server/src`（非测试代码**零命中**）、
`git-utils/src/branch.rs`、`ext/git-attribution/`。

- codex 与 git 相关的**全部**能力是**只读或补丁级**：`git-utils/src/branch.rs:15` `merge_base_with_head`（`:10-13` 文档行）
  （只算 merge-base 用于展示）、`git-utils/src/apply.rs`（`apply_git_patch` / `stage_paths`）、
  `git-utils/src/baseline.rs`（`ensure_git_baseline_repository` / `reset_git_repository`，给记忆管线做
  工作区差异基线）。
- codex 对"提交/合并"的态度是**提示词层面**的：`ext/git-attribution/src/world_state.rs:10-18` ——
  "When you create or amend a git commit or create or update a pull request for this task, add Codex
  attribution exactly once"，即**要求模型在 commit message 结尾加 trailer**。**没有 merge 机器、
  没有队列、没有闸门**：合并由人（或平台）做。
- 诚实补充：`cloud-tasks/` 有云端任务提交面，但那是**把任务送到远端**的客户端
  （`cloud-tasks/src/new_task.rs`），不是本地合并队列；本 run 未取得其服务端行为，故不计入。

**判定**：**赢（无对应物）**。并附一条**未判**：codex **云端/服务端**是否存在同形合并闸门，本 run 无法从本仓证明。

---

### 机制 4 — worktree 生命周期（每任务一块地 + 自动回收）

**gopi 侧**：`internal/tools/task_deliverable_worktree.go`（交付物落 `deliverable.worktree`/`branch`）、
`worktree_fresh_path.go`（路径必须新鲜）、`worktree_sweep.go:605` `worktreeSweepPlan`
（脏=留、惰性=收，`worktree_sweep_chat.go` 对会话地块另论）、`reclaim_own_ground.go`（自我回收带 arm/disarm）、
`task_finish_cleanup.go` 的持有者扫描（Rule 30/34）。

**codex 侧查的**：`grep -rni "worktree" --include=*.rs`（全树 21 个命中，逐一读）、`git-utils/src/info.rs`、
`state/src/extract.rs`、`docs/`。

- **codex 明确不识别 worktree**：`git-utils/src/info.rs:26-28` ——
  "Note that this does **not** detect *work‑trees* created with `git worktree add` where the checkout
  lives outside the main repository directory. If you need Codex to work from such a checkout simply
  pass the `--allow-no-git-exec` CLI flag"。
- 全树唯一的 `git worktree add` 在**测试**里：`core/src/git_info_tests.rs:772`。
- 其余命中是**字符串**：`state/src/extract.rs:397` `PathBuf::from("/child/worktree")`（测试夹具路径）、
  `core/tests/suite/worktree_trust.rs`（信任判定，非生命周期）。
- 结论：codex 工作在**用户给的那个目录**里，没有"为每次执行分配一块地并回收"的概念——
  因此也没有回收谓词、没有持有者扫描、没有脏/惰性分类。

**判定**：**赢（无对应物）**。

---

### 机制 5 — scratch GC（有界临时树，年龄+持有者谓词）

**gopi 侧**：`internal/scratch/gc.go:1-16` 的文件头就是这份机制的说明——"the scratch home
(`<home>/workspace/gopi-test-scratch`) was bounded only by a sweep INLINE IN THE TEST PIN … A host that
stops running suites therefore keeps every root its killed suites abandoned"，于是把 sweep 移进
**生产代码**（worker/merger tick：`scratchGCTick`）。谓词统一在 `internal/scratch/`（`SweepSandboxes`：
名字标记 + 72h 年龄 + 无活体持有者；`Rule 22` 的 off-`/` 判据 `AssertTempRootOffRootFS`）。

**codex 侧查的**：`grep -rniE "fn .*(cleanup|sweep|prune|evict|purge)" --include=*.rs`、`.tmp` 用法全树、
`rollout/src/maintenance.rs`、`rollout/src/compression.rs`。

**codex 有回收，但它是"每处各自为政"，不是"一条统一谓词"**：

| codex 的回收 | 位置 | 谓词 |
|---|---|---|
| shell 快照回收 | `core/src/shell_snapshot.rs:317` `cleanup_stale_snapshots`（`:320-322` 注释：无匹配 rollout 或超出保留窗口即删，活动会话豁免） | 逐文件查 rollout 存在性 + 修改时间 |
| rollout 压缩 temp 回收 | `rollout/src/compression.rs:770` `cleanup_stale_temps` → `:376` 调用；常量在 `:254-261`（`TEMP_FILE_STALE_AFTER` = `RUN_MARKER_STALE_AFTER` = 6h） | 后缀 + 年龄 |
| 冷 rollout 压缩（有界后台 worker） | `rollout/src/compression.rs:256/259/261`（`MIN_ROLLOUT_AGE`=7 天、`WORKER_MAX_RUNTIME`=5h、`MAX_CONCURRENT_COMPRESSION_JOBS`=2；调用点 `:380/391/516`） | 年龄 + 并发上限 + run marker |
| 互斥 | `rollout/src/maintenance.rs:16` `ROLLOUT_MAINTENANCE_LOCK`、`:24` `try_acquire_rollout_maintenance_lock`（进程级非阻塞文件锁） | flock |

- **codex 没有的东西**：统一临时树、**设备/分区判据**（gopi Rule 22 的 off-`/`）、**持有者活体否决**
  （gopi：`/proc/<pid>/cwd` 站在里面就否决删除，避免连根拔掉活体运行的目录）、以及"未知=保留"的默认。
  codex 的 `.tmp` 目录是**分散的缓存位**（`core-plugins/src/startup_sync.rs:32-34` 的 `.tmp/plugins*`、
  `core-plugins/src/remote/share/local_paths.rs:10`），没有"树"的概念，因此也无从 sweep。
- 评注（诚实）：**机制规模上 codex 不输**——它有 worker、有锁、有直方图埋点；输的是**统一性与安全性默认值**。

**判定**：**赢（广度 + 谓词统一 + 活体否决）**，但要说清"codex 有同类回收，只不是同一条谓词"。

---

### 机制 6 — 跨节点同步（两主机台账/知识库互推）

**gopi 侧**：`internal/tools/memory_sync.go:1-17`（节点间知识库同步，git-PR 语义，provenance 落
`node:<host>` 标签 at import）、`:87` `syncPatchVersion` + `:90` `syncCheckPatchVersion`（格式闸门）、
`memory_sync_claim*.go` / `memory_sync_reconcile*.go` / `memory_sync_delta.go`；任务侧
`task_sync_git.go` + `task_sync_git_pushback.go`（Rule 28 的两主机分工就建立在这上面）。

**codex 侧查的**：`grep -rniE "cross[- ]node|peer|remote host|another machine|state sync" --include=*.rs`、
`config/src/*.rs`、`thread-store/src/*`、`codex-rs/docs`、`codex-rs/*.md`。

- **存储层是"可换后端"，不是"多节点同步"**：`thread-store/src/lib.rs:1-5` 的模块文档说得很清楚——
  "Implementations are responsible for resolving that id to local rollout files, RPC requests, or any other
  backing store"；但**实现只有两个**：`thread-store/src/local/mod.rs:415` `impl ThreadStore for LocalThreadStore`、
  `thread-store/src/in_memory.rs:848` `impl ThreadStore for InMemoryThreadStore`。没有第三方实现，没有对等端。
- **网络面确实是"远端执行/远端控制"，但不是状态同步**：`exec-server/src/relay.rs`（WebSocket relay +
  Noise 通道 + resume framing）、`app-server-daemon/src/remote_control_client.rs:22-24`
  （`REMOTE_CONTROL_READY_TIMEOUT` / WebSocket 控制）。这些是**把一条命令送到另一台机器执行**，
  与"两台机器的**台账/知识库**互推去重"是两件事。
- **记忆侧无同步**：`memories/` 的工作区是本地 git baseline（前作 §4.2 已证），没有 export/import 格式、
  没有版本闸门、没有节点标签。
- **未判**：codex 是闭源服务端在背后跑的（Responses API / 云端任务），**服务端**是否有跨设备状态同步，
  本 run 无法从本仓证明；本仓内**零命中**是确定的事实。

**判定**：**赢（本仓零对应物）** + 一条明确 `未判`（服务端）。

---

### 机制 7 — 零依赖单二进制

**gopi 侧**：census 实测 `gopi.go_mod_direct_requires: 28`、`gopi.vendor_dir: absent`、无 CGO
（`CONFIG.md:459` 的构建命令显式 `CGO_ENABLED=0`，产出 `bin/gopi` 单文件）。

**codex 侧**（census 实测 + 分发链）：
- `codex.rust_loc: 1450748`、`codex.rust_files: 3256`、`codex.crate_dirs: 97`、
  `codex.cargo_lock_packages: 1359`、`codex.rust_toolchain: channel = "1.95.0"`。
- **二进制不是以源码形态分发的**：`codex-cli/bin/codex.js:16-23` 的 `PLATFORM_PACKAGE_BY_TARGET`
  列 6 个平台包（`@openai/codex-linux-x64` / `-darwin-arm64` / `-win32-x64` …），
  `codex.npm_platform_pkgs: 6`；JS 只是**挑平台包并 spawn**（`:80-89` 解析 vendor 根）。
- 所以 codex **运行时**也是单个 native 二进制（这点必须诚实：不是"必须装 Rust"），
  但它要求：**一个 Node ≥16 运行时 + 一个按平台选中的 npm 包**（`codex-cli/package.json:12-14`），
  且**源码态构建成本**是 ~145 万行 Rust + 1359 个锁定包 + 固定 toolchain 1.95.0。
- gopi 的"零依赖"是可复制的：拷一个二进制 + 一个 settings 文件就能跑（Rule 35 的原话）。

**判定**：**赢**（源码态零依赖 vs 百包工程 + 平台包分发），**附诚实边界**：运行时二进制形态两侧都成立。

---

### 机制 8 — 证据固化（报告封口 + gate-claim / Ran 记录）

**gopi 侧**：`internal/tools/task_report.go:33`（seal 永不覆盖模型内容）、`:60` `TaskShowReportClaimLabel`、
`:172` `taskReportGateClaimRecord`（裁决报告里的**闸门声明**）、`:189` `taskReportRanCheckRecord`
（裁决 Rule 31 的 `Ran:` 证据），`:166` `taskReportClaimWarnPrefix`。也就是：**报告被写下的那一刻，
它的"我跑过了"这句话会被逐条记录并可在 `task show` 读到**（Rule 5 的 `## Evidence (self-filed)`）。

**codex 侧查的**：`grep -rniE "seal|gate.?claim|verify_evidence|verification" --include=*.rs state/src core/src`、
`state/src/audit.rs`。

- 全树 `seal` 于非记忆管线**零命中**；无 `report`/`claim`/`evidence` 数据模型。
- 最接近的两个东西，语义都不同：
  - `memories/write/src/phase1.rs` 的 `try_claim_stage1_job`（`state/src/runtime/memories.rs:671`）——
    那是**记忆提炼任务的 lease 认领**（worker 抢活、防重复），不是"完成的证据"。
  - `rollout-trace/` 的**证据级录制**：`RawTraceEvent` / `seq` / `replay_bundle`，把推理、工具派发、
    MCP 调用全部落成可复演的 trace——**这一面 codex 比 gopi 强**（gopi 没有同等级别的可复演 trace 总线）。
  - `state/src/audit.rs:1` "Read-only state database queries for diagnostics" —— 只读审计查询。
- 但**"完成声明 → 被裁决的记录"**这一环两侧都不是：codex 的 trace 记录**发生了什么**，
  gopi 的 seal 记录**报告声称了什么、声称有没有观察支撑**——后者是**对外交付**的治理，前者是**运行时**的治理。

**判定**：**赢（交付面的证据固化无对应物）**，**附一条明确的输**：运行时**可复演 trace** 这一面 codex 更强（见 §5 G-3）。

---

## 3. "我们有而 codex 没有" — 实证清单（每条带查过的 codex 位置）

| # | 能力 | 查过的 codex 位置（检索证据） | 结论 |
|---|---|---|---|
| 1 | 跨会话**任务台账**（工作项 + 认领 + 交付物 + 证据字段） | `state/src/model/`（backfill_state/graph/log/memories/project/queued_item/rollout_migration_state/thread_goal/thread_metadata —— `tasks` 无）、`grep claim_` 全落 `state/src/runtime/memories.rs` | 无 |
| 2 | **父依赖闸门**（关系 → 调度谓词） | `state/src/model/graph.rs:5-8`、`state/src/runtime/threads.rs:123/150/214`、`core/src/agent/control.rs:728`、`.../multi_agents/spawn.rs:66-69`（深度限制，非依赖） | 数据面有、**决策面无** |
| 3 | **合并队列 + 推送闸门** | `grep merge_queue/push gate` 零命中；`grep '"merge"'` 非测试零命中；`git-utils/src/branch.rs:15`（只算 merge-base）；`ext/git-attribution/src/world_state.rs:10-18`（只写提示词） | 无 |
| 4 | **worktree 生命周期** | `git-utils/src/info.rs:26-28` **显式声明不识别** worktree；唯一 `git worktree add` 在 `core/src/git_info_tests.rs:772`（测试） | 无（且明确不支持） |
| 5 | **统一 scratch GC**（树 + 年龄 + 持有者 + off-`/` 判据） | `core/src/shell_snapshot.rs:317`、`rollout/src/compression.rs:770/254-261`、`rollout/src/maintenance.rs:16`（各有各的回收，无统一树/谓词） | 无（有同类窄机制） |
| 6 | **跨节点同步**（台账/知识库互推，带格式闸门与 provenance） | `thread-store/src/lib.rs:1-5` + 两个实现（`local/mod.rs:415`、`in_memory.rs:848`）；`exec-server/src/relay.rs`、`app-server-daemon/.../remote_control_client.rs:22-24`（远端**执行**，非状态同步） | 无（本仓内） |
| 7 | **零依赖单二进制** | census：1450748 LOC / 97 crate / 1359 锁包 / toolchain 1.95.0；`codex-cli/bin/codex.js:16-23` 6 个平台包 | 无（codex 是百包工程 + 平台包分发） |
| 8 | **证据固化**（报告封口 + gate-claim/Ran 裁决） | `state/src/audit.rs:1`（只读诊断查询）、`state/src/runtime/memories.rs:671`（记忆 job lease） | 无 |

---

## 4. 三方对照汇总表

**数据来源**：本报告（codex 侧，源码一手）+ `devdoc/competitor-dsh-analysis.md`（dsh 源码一手，dsh 侧 census `18088` 文件）
+ `devdoc/competitor-claude-code-behavior.md`（claude 无源码，真跑探测 7 条）。
**口径**：本表**不重写**两份同侪报告，只做**映射与合并**；同侪报告未覆盖的格子**明确标 `待汇总`**，
并写清缺的是哪一条。

### 4.1 三层之一：技术架构

| 面 | gopi | codex（本报告） | dsh | claude code |
|---|---|---|---|---|
| 工具模型 | 17 扁平 + `switch`（前作锚点） | 多态 `ToolSpec` + `ToolRegistry` + `tool_search` BM25 | 插件注册表 + 三级 waterfall + Code Mode | 25 工具（P7），无自调度/技能/子代理 |
| 执行治理（本报告轴） | **台账 / 父闸门 / 合并队列+推送闸门 / worktree 生命周期 / 统一 GC / 跨节点同步 / 证据封口**（§2） | **仅 append-only 日志 + 派生边**；无台账、无队列、无地块、无同步、无封口（§2 逐条） | 跨会话台账**无**（`goal` = 同会话，dsh §7#1）；合并/地块/GC/同步面 **`待汇总`**（该报告未覆盖） | 台账**无**（单会话 plan）；合并/地块/GC/同步面 **`待汇总`** |
| 隔离模型 | 沙箱 profile（可运行环境）+ 路径守卫 | 沙箱三档 + 审批策略 + exec policy（前作 §5） | 沙箱三档 = **文件访问模式**（非可运行 profile，dsh §7#2） | 默认拒写越界 + permission 模式（**可被一个 flag 关掉**，claude §9.3） |
| 依赖形态 | 28 require / 无 vendor / CGO=0（census） | 97 crate / 1359 包 / npm 平台包 | 18088 文件 / 55 包插件化 | 闭源 npm 分发 |

### 4.2 三层之二：产品价值

| 用户场景 | gopi | codex | dsh | claude code |
|---|---|---|---|---|
| 一次性开发任务 | 中（工具面窄） | **强**（工具平台 + 多 agent + 沙箱分层） | 强（Code Mode 吞吐量级差，dsh D2） | **强**（开箱即用 + 后台 attach，claude §9.2） |
| 多任务/多会话长期工程 | **强**（跨会话台账 + 父闸门 + 逐行审计） | 弱（无台账） | **未判**（`goal` 是同会话，跨会话账本无对应物 → 判 **无**） | 弱（任务板随会话消失，claude §8#3） |
| "能不能信它"（可审计） | **强**（失败落 `failed` 行 + 日志路径 + resume；报告封口裁决） | 中（trace 可复演强，但**完成声明**无裁决） | **`待汇总`**（dsh 报告未做审计面） | 中（`permission_denials`/`total_cost_usd` 结构化强于 gopi，claude §9.2） |
| 单次运行成本可见性 | 弱（在 `stats`/trajectory，不在返回里） | **`待汇总`** | `待汇总` | **强**（`total_cost_usd`，本次 $0.19186） |
| 长时程/后台 | **弱**（`job` 是进程内，claude §8#1 判 gopi 输） | **强**（多 agent + app-server daemon + remote control） | `待汇总` | **强**（跨进程可 attach） |

### 4.3 三层之三：诚实评估 — gopi 明确输的条目

| # | gopi 输在哪 | 对手 | 证据（本 run 或同侪报告） |
|---|---|---|---|
| G-1 | 工具模型不可组合（编译期扩展） | codex / dsh | 前作 §1.1-1.2；dsh D1（跨两份报告一致） |
| G-2 | 无多 agent 委派（只有 `job`） | codex / dsh | 前作 §2.1；dsh D3 / dsh §3.2「1 vs 4」 |
| G-3 | 无可复演 trace 总线 | codex | 本报告 §2 机制 8：`rollout-trace/`（`RawTraceEvent`/`seq`/`replay_bundle`，`rollout-trace/src/lib.rs:51,77`）gopi 无同级机制 |
| G-4 | 无 Code Mode（一轮一次调用） | dsh | dsh D2（吞吐量量级差） |
| G-5 | 记忆**需模型自觉**调用，非自动沉淀 | codex / claude | 前作 §4.2（codex 两阶段自动管线）；claude §9.3#4（gopi 轨迹实测 `search=0 store=0`） |
| G-6 | 无权限层（写越界无默认拒绝） | codex / claude / dsh | claude §9.3#2；前作 §5；dsh D6 |
| G-7 | 长时程/后台面缺失 | claude / codex | claude §9.3#1；codex 有 app-server daemon + remote control |
| G-8 | 输出预算粗糙（只有 cap） | codex / dsh | 前作 §1.4；dsh D5 |
| G-9 | 系统提示词不可组合 | codex / dsh | 前作 §3.3；dsh D4 |
| G-10 | 单次运行的成本/用量不在返回里 | claude | claude §9.3#6 |

**反向（对手输在哪 —— 前两份报告已判，本报告复核一致）**：claude 的台账不落盘、记忆按 cwd 分域、
危险命令闸门可被一个 flag 关掉、引入常驻 daemon+自调度（Rule 35 视角的持久依赖）、后台进程未脱离登录会话
（Rule 24 判据实测）；dsh 的跨会话台账无对应物。**本报告新增**：codex 无任务台账、父闸门决策面、
合并队列、worktree 生命周期、统一 GC、跨节点同步、证据封口（§2/§3）。

### 4.4 汇总表的空缺（如实写明）

| 空缺 | 缺的是哪一条 | 为什么现在补不上 |
|---|---|---|
| dsh 侧的**合并队列 / worktree 生命周期 / scratch GC / 跨节点同步 / 证据封口** | dsh 报告（`competitor-dsh-analysis.md`）的 6 个维度里没有这 5 条（它的维 1-5 是工具/工作流/提示词/记忆/插件依赖），§7「gopi 有而 dsh 无」只列了 6 类（台账/沙箱 profile/屏幕读取/纯 Go 渲染/分时定价/resume） | 那 5 条**从未被查**——不是判了"无"，是**没查**。补齐需一轮 dsh 侧定向检索（读 `/home/user/work/dsh` 或等价物），本 session 预算不含 |
| claude 侧的**合并队列 / worktree 生命周期 / scratch GC / 跨节点同步 / 证据封口** | claude 报告 7 条探测里没有这 5 条 | claude **无源码**，只能真跑；这 5 条需要新的探测设计（且其中"合并队列/跨节点同步"在闭源 CLI 的**行为面**上很可能测不出） |
| codex 侧的**服务端**合并/同步行为 | 机制 3 与机制 6 的 `未判` 项 | codex 本仓是 CLI 客户端；服务端不在源码内 |

> **等哪两条**：①`devdoc/competitor-dsh-analysis.md` 的**定向补轮**——按机制 3/4/5/6/8 逐条去
> dsh 源码查（这两条研究①= `01a0eeb1-67b5-733e-84b9-e7710eed2a00`、②= `01a0eeb1-8251-740e-87d8-00730e798e36`，
> 两报告均**已到**，本报告的汇总已基于它们，缺的只是这 5 条**从未被查**）；
> ②`devdoc/competitor-claude-code-behavior.md` 的**定向补轮**——同一 5 条的 claude 侧真跑探测
> （claude 无源码，需新探测设计；"合并队列/跨节点同步"在行为面很可能测不出，届时应如实记 `未判`）。
> 本 run 已把两条补轮作为独立工作项提交（见 §6 末"已派单"），两者到位后 §4.1/§4.2 中 9 个
> `待汇总` 格子即可填满。

---

## 5. 未判清单（本 run 测不出，写明原因）

| 判定对象 | 为什么未判 |
|---|---|
| codex **服务端**是否有合并闸门 / 跨设备状态同步 | codex 本仓是 CLI 客户端 + 服务协议；服务端行为不在源码内，本 run 只读本仓 |
| codex 的**云端任务**（`cloud-tasks/`）是否含 PR 合并流程 | 只读到 `cloud-tasks/src/new_task.rs` 的客户端提交面；服务端流程不可见 |
| 机制 5 两侧的**实测行为**（GC 真的回收了吗） | 本 run 是**只读源码查证**，未运行 codex，也未建压力场景（Rule 27 禁止造负载） |
| codex 的 `ThreadStore` trait 是否有**仓外/私有实现**（真正的远端后端） | 本仓两个实现（Local/InMemory）；私有实现不在可见范围 |
| 机制 2 的 codex 侧是否有**未被检索词覆盖**的 claim 谓词 | 检索词为 `parent_thread_id` / `spawn_edge` / `claim` / `gate` / `queue`；若实现用了完全不同的命名，可能漏网——判定基于"穷尽式检索 + 读实现"，不宣称数学完备 |

---

## 6. 方法学备注

- 全部结论为**源码第一手**：每条先 `grep -rniE` 定位，再 `read`/`sed -n` 读实现，再写判定；
  未使用任何二手描述。
- **只读纪律**：本 run 对 `/home/user/work/codex` 只有 `git log`/`git rev-parse`/`grep`/`sed -n`/`find`，
  **无写操作**；证据原件（census 脚本与输出）落本 run 会话目录 `~/.gopi/sessions/01a0eeb1-9e15-7ae0-9945-96443de03323/evidence/`
  （Rule 37，不进 git）。
- **不重写前作**：`gopi-vs-codex-competitive-analysis.md` 一字未改；本报告是它的补轮。
- **census 口径**：§0 与 §2/§3 引用的所有规模数字来自 `evidence/census.sh` @ codex `ad9e809` / gopi `3c0e926b`（Rule 38）。
- 前作的三条"codex 没有"结论（memory_feedback 质量裁判、resume raw-log byte-exact、thinking budget 强制）
  本 run **未复核**（不在 8 条机制内），沿用前作判定。
- **已派单（补 §4.4 的空缺，Rule 26：不完整的修复靠再派单，不靠等）**：
  - ①dsh 定向补轮 → `gopi task submit` 得 id **`01a0eef1-87a7-74b8-8c63-c31947430749`**
    （5 条未查机制逐条查 dsh 源码，交付 `devdoc/gopi-vs-dsh-reverse-gaps.md`）；
  - ②claude-code 定向补轮 → id **`01a0eef1-88f5-7de0-9df3-c9853530a665`**
    （同 5 条的无源码真跑探测，交付 `devdoc/competitor-claude-code-reverse-gaps.md`）。
  两者验收通过并 merge 后，§4.1/§4.2 的 9 个 `待汇总` 格子即可填满——**本报告对这一缺口是明说的，不是留白**。

---

## Verification

- Build: N/A（本任务为只读研究 + 文档交付，未改任何代码；`go build` 未运行——无 Go 源变更）
- Tests: N/A（同上；未新增测试）
- Ran: 见下方 verbatim 段（本 run 实际执行的全部只读命令与产出）
- Result: 报告落 `devdoc/gopi-vs-codex-reverse-gaps.md`；8/8 条机制有 codex 侧 file:line 或明确 `未判`
  （§2 逐条、§5 汇总 5 条未判）；§4 三层齐备；§4.3 列 gopi 输的 10 条；§4.4 写明待汇总的两条
- Commit: `732c2d96`（本次修正在其后的 follow-up commit 上；报告的落盘 commit 即 `732c2d96`）

### Ran（verbatim）

```bash
# --- 基线 ---
cd /home/user/work/codex && git log --oneline -1 && git rev-parse HEAD
#   → ad9e809 Preserve managed deny-read rules across permission updates (#40004)
#     ad9e8097fd3d0d2f1c1166575d2c6cd8cb9e1833

# --- 机制 1：append-only / 台账 ---
cd /home/user/work/codex/codex-rs && grep -rniE "append[- _]only" --include=*.rs --include=*.md .
#   → rollout-trace/src/raw_event.rs:1,28 ; rollout-trace/src/lib.rs:51,77
#     message-history/src/lib.rs:1 ; message-history/src/batch.rs:60
#     ext/memories/templates/memories/read_path.md:30 ; rollout-trace/README.md:109
grep -rniE "append[_ -]?only|ledger" --include=*.rs -l .
#   → external-agent-migration/src/sessions/ledger.rs（迁移账本，非工作项台账）
cd /home/user/work/codex/codex-rs && sed -n '1,60p' state/src/model/thread_goal.rs
#   → ThreadGoalStatus { Active, Paused, Blocked, UsageLimited, BudgetLimited, Complete }（同会话目标）
sed -n '1,60p' thread-store/src/local/revert_thread.rs
#   → :11-14 "Old rollouts stay intact. … the only mutable cutover is the existing SQLite rollout-path pointer"

# --- 机制 2：父子依赖闸门 ---
grep -rniE "parent_thread_id|parent_thread|child_thread|spawn_agent" --include=*.rs .
#   → app-server-protocol/src/protocol/v2/thread_data.rs:207 ; tui/src/app_event.rs:245 ...
sed -n '1,50p' state/src/model/graph.rs
#   → :5-8 enum DirectionalThreadSpawnEdgeStatus { Open, Closed }
grep -rn "DirectionalThreadSpawnEdgeStatus\|spawn_edge\|thread_spawn_edge" --include=*.rs .
#   → state/src/runtime/threads.rs:123 upsert_thread_spawn_edge ; :150 set_thread_spawn_edge_status
#     core/src/agent/control.rs:728 list_live_thread_spawn_edges
grep -rn "depth" core/src/tools/handlers/multi_agents/spawn.rs
#   → :66 let max_depth = turn.config.agent_max_depth; :69 "Agent depth limit reached. Solve the task yourself."
code_search/read: core/src/agent/registry.rs:91-92 exceeds_thread_spawn_depth_limit = 防递归（非依赖顺序）

# --- 机制 3：合并队列 + 推送闸门 ---
grep -rniE "merge_queue|merge queue|push gate|pre-?push" --include=*.rs -l .
#   → （零命中）
grep -rn '"merge"' --include=*.rs core/src git-utils/src cli/src app-server/src
#   → （非测试代码零命中；core/src/tools/handlers/mod.rs:292 是注释里的 "unchecked merge"）
sed -n '1,60p' git-utils/src/branch.rs
#   → :12 pub fn merge_base_with_head（只计算，用于展示）
sed -n '1,30p' ext/git-attribution/src/world_state.rs
#   → :10-18 "When you create or amend a git commit or create or update a pull request for this task,
#           add Codex attribution exactly once"（提示词层，非机器）
grep -rniE "\bgit commit|auto_commit|git merge" --include=*.rs .
#   → ext/git-attribution/src/lib.rs:24 ; state/src/model/thread_metadata.rs:181（记录 commit shas，不执行合并）

# --- 机制 4：worktree 生命周期 ---
grep -rniE "worktree" --include=*.rs -il .
#   → 21 文件，逐一读
sed -n '18,40p' git-utils/src/info.rs
#   → :26-28 "does **not** detect *work‑trees* created with `git worktree add` … pass `--allow-no-git-exec`"
grep -rniE "worktree add|worktree_add|create_worktree|WorktreeCreate" --include=*.rs .
#   → core/src/git_info_tests.rs:772（测试内）
grep -rniE "clone the repo|git clone|scratchpad|workspace_dir" --include=*.rs core/src
#   → （零命中）

# --- 机制 5：scratch GC ---
grep -rniE "fn .*(cleanup|sweep|prune|evict|purge)" --include=*.rs .
#   → core/src/shell_snapshot.rs:317 cleanup_stale_snapshots
#     rollout/src/compression.rs:770 cleanup_stale_temps ; :376 调用点
#     memories/write/src/storage.rs:80 prune_rollout_summaries ; memories/write/src/extensions/prune.rs:9
sed -n '310,360p' core/src/shell_snapshot.rs
#   → :320-322 "Removes shell snapshots that either lack a matching session rollout file or whose
#           rollouts have not been updated within the retention window. The active session id is exempt"
sed -n '1,80p' rollout/src/maintenance.rs
#   → :16 const ROLLOUT_MAINTENANCE_LOCK ; :24 try_acquire_rollout_maintenance_lock
sed -n '250,262p' rollout/src/compression.rs
#   → :254-261 TEMP_SUFFIX / COMPRESSION_LEVEL=3 / MIN_ROLLOUT_AGE=7d / RUN_MARKER_STALE_AFTER=6h /
#           TEMP_FILE_STALE_AFTER / WORKER_MAX_RUNTIME=5h / MAX_CONCURRENT_COMPRESSION_JOBS=2
grep -rn "\.tmp" --include=*.rs . | head -30
#   → core-plugins/src/startup_sync.rs:32-34（.tmp/plugins* 分散缓存位）

# --- 机制 6：跨节点同步 ---
grep -rniE "cross[- ]node|peer|remote host|another machine" --include=*.rs -l .
#   → exec-server/src/{relay.rs,noise_channel.rs,connection.rs} ; app-server-daemon/src/remote_control_client.rs
grep -rniE "RpcThreadStore|RemoteThreadStore" --include=*.rs .
#   → （零命中）
grep -rn "impl ThreadStore for" --include=*.rs .
#   → thread-store/src/local/mod.rs:415 ; thread-store/src/in_memory.rs:848（只有两个实现）
sed -n '1,5p' thread-store/src/lib.rs
#   → "Implementations are responsible for resolving that id to local rollout files, RPC requests,
#      or any other backing store."（抽象在，实现不在）
sed -n '22,25p' app-server-daemon/src/remote_control_client.rs
#   → REMOTE_CONTROL_READY_TIMEOUT（远端控制通道，非状态同步）

# --- 机制 7：零依赖单二进制 ---
sed -n '1,40p' codex-cli/package.json ; sed -n '1,90p' codex-cli/bin/codex.js
#   → package.json:12-14 engines.node >=16 ; codex.js:16-23 PLATFORM_PACKAGE_BY_TARGET（6 个平台包）
#     codex.js:80-89 vendorRoot 解析
cat codex-rs/rust-toolchain.toml
#   → channel = "1.95.0"

# --- 机制 8：证据固化 ---
grep -rniE "verify_evidence|verification|claim_|claimed_by" --include=*.rs state/src core/src
#   → 全部落在 state/src/runtime/memories.rs（记忆 job lease：:671 try_claim_stage1_job, :1076 try_claim_global_phase2_job）
sed -n '1,32p' state/src/audit.rs
#   → "Read-only state database queries for diagnostics"（只读审计，无证据裁决）

# --- gopi 侧锚点（并置核对） ---
cd /home/user/workspace/gopi-01a0eeb19e157ae0994596443de03323
grep -n "CREATE TABLE IF NOT EXISTS tasks\|parent_task_id\|verify_evidence\|run_cmd" internal/tools/tasks.go
#   → :295 CREATE TABLE ; :303 parent_task_id ; :310 verify_evidence ; :313 run_cmd
grep -n "func taskParentGateDenied\|func taskRowClaimable" internal/tools/tasks.go
#   → :5864 ; :5982
grep -n "func gitPushbackSharesHistoryWithMaster\|func gitPushbackPlan" internal/tools/task_sync_git_pushback.go
#   → :333 ; :591
grep -n "^func worktreeSweepPlan" internal/tools/worktree_sweep.go ; grep -n "^func SelfGroundReleaseArm" internal/tools/reclaim_own_ground.go
#   → worktree_sweep.go:605 ; reclaim_own_ground.go:78
grep -n "TaskShowReportClaimLabel\|func taskReportGateClaimRecord\|func taskReportRanCheckRecord" internal/tools/task_report.go
#   → :60 ; :172 ; :189
head -16 internal/scratch/gc.go
#   → 包级说明：scratchHome 有界化 + 移入生产代码（worker/merger tick）
grep -n "syncPatchVersion\|func syncCheckPatchVersion" internal/tools/memory_sync.go
#   → :87 syncPatchVersion ; :90 syncCheckPatchVersion
grep -n '"build"' CONFIG.md
#   → :459 "build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"

# --- census（Rule 38：生成器 + rev）---
S=/home/user/.gopi/sessions/01a0eeb1-9e15-7ae0-9945-96443de03323
bash $S/evidence/census.sh | tee $S/evidence/census.out
#   → （输出逐字见 §1；codex @ ad9e809，gopi @ 3c0e926b）

# --- 姊妹产物口径核对（三方汇总的输入）---
grep -n '^#\{1,3\} ' devdoc/competitor-dsh-analysis.md       # 维 1-6 + §7 gopi 有而 dsh 无（6 类）+ §8 输处
grep -n '^#\{1,3\} ' devdoc/competitor-claude-code-behavior.md  # P1-P7 + §8 总表 + §9 三层面 + §10 未判
sed -n '412,470p' devdoc/competitor-dsh-analysis.md          # dsh §7 表格 + §8 D1-D6
sed -n '340,410p' devdoc/competitor-claude-code-behavior.md  # claude 总表 + 三层面 + 未判清单
```

### Evidence (self-filed)

| 声明 | 本 run observed 的东西 |
|---|---|
| codex 侧零写入 | 全程只运行 `git log`/`git rev-parse`/`grep`/`sed -n`/`find`/`cat`/`wc` —— 无写、无 checkout、无构建；未对 `/home/user/work/codex` 执行任何变更命令 |
| census 数字可复现 | `evidence/census.sh` 输出 `evidence/census.out`（本 run 会话目录），首行打印两侧 rev；§0 数字与输出逐字一致 |
| 8/8 条机制有判定 | §2 逐条给出 codex 侧 `file:line` 或 `未判`（机制 3/6 各含 1 条未判，§5 汇总共 5 条未判） |
| "无对应物"来自穷尽检索 | §2 每条列出实际执行的检索词与结果（含零命中的那条命令原文） |
| gopi 侧锚点存在 | §2 各条目与 "Ran" 段末的 `grep -n` 输出一致 |
| 前作未被改写 | `gopi-vs-codex-competitive-analysis.md` 不在本 commit 的文件列表中（§6、commit 内容） |

---

## 附：本报告与三份既有产品的分工

| 文件 | 回答的问题 | 本报告的关系 |
|---|---|---|
| `gopi-vs-codex-competitive-analysis.md` | codex **强在哪**（工具/工作流/提示词/记忆） | 补轮：本报告答**反向面**；不改前作 |
| `competitor-dsh-analysis.md` | gopi vs dsh（6 维度 + 输处 D1-D6） | 汇总：本报告 §4 引用其结论；**不**替它补它没查的 5 条 |
| `competitor-claude-code-behavior.md` | claude 无源码真跑 7 条探测 | 同上 |
| **本报告** | **gopi 的 8 条机制在 codex 侧有无对应物 + 三方对照** | — |

---

## 7. 复核后的行号修正（本 run 自查，2026-09-30）

交付前对 codex 侧引用做了**逐条 re-read**（不是复用 grep 输出），命中 4 处行号偏差，已修正：

| 位置 | 原引用 | 实测 | 处置 |
|---|---|---|---|
| `thread-store/src/local/revert_thread.rs` | `:11-14` | 引文在 **`:17-18`**（`:11-14` 是 `use` 行） | 已改为 `:17-18` |
| `git-utils/src/branch.rs`（§2 机制 3） | `:12` | 函数在 **`:15`**（`:10-13` 是文档注释） | 已改为 `:15`，并补 `:10-13` |
| `git-utils/src/branch.rs`（§3 清单行） | `:12` | 同上 | 已改为 `:15` |
| `rollout/src/compression.rs` 常量组 | `:18-21,256-260` | `:18-21` 是 `COMPRESSED_SUFFIX`/`TEMP_SUFFIX` 等**不相关**常量；`MIN_ROLLOUT_AGE`=`256`、`TEMP_FILE_STALE_AFTER`=`258`、`WORKER_MAX_RUNTIME`=`259`、`MAX_CONCURRENT_COMPRESSION_JOBS`=`261` | 已改为 `:256/259/261` + 调用点 `:380/391/516` |

复核中**确认无误**的引用（连同 `sed -n` 原文一并抽查）：

- `state/src/model/graph.rs:5-8` → `pub enum DirectionalThreadSpawnEdgeStatus {`（`:8`）；
- `core/src/tools/handlers/multi_agents/spawn.rs:66-69` → `agent_max_depth` + "Agent depth limit reached. Solve the task yourself."；
- `git-utils/src/info.rs:26-28` → "`git worktree add` where the checkout lives outside the main repository directory"；
- `state/src/runtime/threads.rs:123/150/214` → `upsert_thread_spawn_edge` / `set_thread_spawn_edge_status` / `FROM thread_spawn_edges`；
- `core/src/agent/control.rs:728` → `list_live_thread_spawn_edges()`；
- `ext/git-attribution/src/world_state.rs:10-18` → 纯提示词文本（Co-authored-by trailer 规则）；
- `state/src/audit.rs:1` → "Read-only state database queries for diagnostics."；
- `state/src/runtime/memories.rs:671` → `try_claim_stage1_job`（记忆 job lease，非工作项认领）；
- gopi 侧 `internal/tools/tasks.go:295/5864/5982`、`task_sync_git_pushback.go:333/591`、`CONFIG.md:459` 全部逐字命中。

**这一节是 Rule 5「gate 结果必须自带观察」的自查落地**：一条 file:line 若没被 re-read，就是转述而非证据。
