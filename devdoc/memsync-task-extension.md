<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** `cmd/agent/cli_memsync.go` and the `gopi memsync` spelling were
> DELETED with the cross-node sync family (the word still routes, answering with a removal sentence
> that points at `gopi task dedup-scan`). The twin-row audit it describes is that ledger verb now;
> the `--export/--import` halves have no successor because they existed to feed a peer protocol.

# M4 memsync 扩展：task 行同步（devdoc/memsync-task-extension.md）

分支 `feat/memsync-tasks`（基 master 2ed1ed1）。Epic：brew 行通道之外，让 task 行走同一 export→JSON patch→import 机制在 peerbox/HP 间流动。

## 更新 2026-10-03 — epic 01a0fea1 ③：身份 = task_id（payload 退出"同一份工作"判定）

原设计把"同一份工作"判在 (summary, payload) 上。实测缺陷：同一 task_id 下只改写 briefing 文本（payload）时，两端把这一行读成**不同的任务** —— harvest 把它 relabel（t-x → t-x-nhp）、终态 sync-back 拒收（"refusing to fabricate a completion"）、内容阶梯**静默 skip 掉一行**。收窄后的判定与降级后的阶梯（代码在 `internal/tools/memory_sync_task.go`，回归测试 `internal/tools/memory_sync_identity_test.go`）：

- **`sameWorkIdentity(remoteSummary, localSummary)`** 是唯一的"同一份工作"比较器（trim 后比较 summary）；**payload 不在其中**。用于：同 id 导入判定、`-n<node>` relabel 的幂等判定、终态 sync-back 门、harvest 同内容判定、claim 通道的行校验。
- **summary 仍是定义**：两行同 id 但 summary 不同 = 一次真正的 id 撞车 —— 本地 live 行不被吸收，远端行 `-n<node>` relabel 入队（**不再**被当作"同工作"静默 tags union）；终态行撞不同 summary 依旧 refused。
- **内容阶梯降级为警告**（Rule 15：降级必须可见）：命中**不再 skip**，改为计数 + 每行一条 note，行**照常按自己的 task_id 导入**。CLI 侧 `--import-tasks` 现在打印**全部** note（原先只在 `refused>0` 时打印 refused），refused 仍 exit 2。
- **三个计数器的最终语义**（`SyncTaskImportStats` 的 doc 注释是单一来源，此处是契约摘要）：

  | 计数器 | 事件 | 打印处 | 是否改变行的命运 |
  |---|---|---|---|
  | `RowsContentWarn` | 文件通道：内容阶梯命中一条 **LIVE 孪生**（exact / canonical / similar） | 无独立段（见下） | **否** —— 行照常导入 |
  | `RowsDuped` | 同一事件的**保留别名**：文件通道同时 ++，使 CLI 既有 `duped=N` 计数行与所有旧调用方继续看到它 | `imported tasks: … duped=N …` | **否**（不再是 skip） |
  | `RowsDiverged` | delta 通道：同 task_id、同 anchor、**同 status**、summary/payload 不同 —— 对端的同一行复述 | `sync … pull{… diverged=N …}` | **否** —— 两侧内容永不覆写，tags 只并集 |

  规则一句话：**内容信号永远只是警告**（计数 + 报告行 + warn log，行照自己 task_id 走）；只有"同 id 不同 summary"（一次真 id 撞车）才 relabel / refuse。`RowsDiverged` 与 `RowsContentWarn` 是同一问题的**相反观察**（对端复述 vs 本地孪生命中），故按通道保留两个计数器，而不是并成一个。
- **`syncTaskWorkHash` 的去留**：仍保留，但**不再决定导入行的命运** —— 它只服务 `gopi task --dedup-scan` 审计报告（`syncTaskDedupDecide` 的 gate 名 `syncTaskGateExact/Canonical/Similar` 同样是审计词汇）。
- 测试改动：`internal/tools/memory_sync_task_test.go`、`memory_sync_task_dedup_test.go` 中语义变化的断言逐条更新（duped→already、skipped→imported+warned），原因写在断言旁；新增 `internal/tools/memory_sync_identity_test.go`（7 个用例，旧代码上红、新代码上绿）。

## Completed

- **internal/tools/memory_sync_task.go**（新文件，~430 行）：
  - `SyncTaskExport(opts)` — SELECT tasks 行（`--status` 状态门 / `--tags` tag 过滤 / `--since` created_at 门 / `--limit` 截断；ORDER BY created_at ASC, id ASC = 调度器 claim 序）。产出 `SyncTaskPatch{version,node,exported_at,rows}`。
  - **执行态不导出**：SyncTaskRow 不含 claimed_by/claimed_at/heartbeat；live 行（pending/claimed/in_progress）导出时 status 置空——接收端自己 pending 起步；终态行的 status 就是反向 sync-back 的载荷，原样携带。
  - `SyncTaskImport(path)` — 五条机制：
    1. **内容阶梯（epic 01a0fea1 ③ 后降级为警告）**：sha256(summary+NUL+payload)，再 canonical hash，再 trigram 相似度；阶梯门（id 分支之前）：同 patch 内撞 或 与本地 **live**（pending/claimed/in_progress）行内容相同 → **计数 + 一行 note，行照常导入**（身份是 task_id）。原语义（命中即 skip + duped）见上方 2026-10-03 小节。
    2. **task_id 保留原号**：TaskSubmit 传 `--task-id` 原样入队，t-* 号跨节点可追溯。冲突不重写：同 id 且 **summary 相同（payload 不参与判定）** → tags union / already；不同 summary + 本地 pending → 原行不动 + `-n<node>` relabel 入队（见上方 2026-10-03 小节）；终态 patch 行无本地对应行 → **refused**（绝不把已完成工作重新 pending——防重复执行）。
    3. **状态机前向迁移**：fresh import 只写 pending；本地行后续状态由本地执行推进；反向 sync-back（终态 patch 行 + 本地 pending 行）走 `syncTaskMigrate`——**pending-only 的 CAS UPDATE**（`WHERE task_id=? AND status='pending'`），目标限 completed/failed/verified（cancelled 不远端迁移：本地判断留本地）。故意不走 `taskAllowedTransitions`：单机状态机建模的是本地 claim→run→writeback 生命周期，远端合法走完的行是"分布式真相补齐"，本迁移的门（pending-only + 可前向目标）比状态机更严。
    4. **provenance 强制**：import 强制给每行 tags 追加 `from-<patch.Node>`（node 缺失 → `from-unknown-node`），与 brew 行 `node:<host>` 同构；文件里的 tags 只描述工作，通道标记在导入时决定。
    5. **永不覆写**（nick 硬约束）：已有行的 summary/payload 永不 UPDATE；同 task_id、同 summary 且本地 pending → 仅 tags union（`taskTagsUnion`，幂等：tag 集合相等 → already 不动）。
  - `WriteSyncTaskPatch` — 与 brew patch 相同的 pretty-JSON 落盘。
- **cmd/agent/cli_memsync.go**：`--export-tasks --out [--status][--tags][--since][--limit][--db]`、`--import-tasks <patch> [--db]`；usage 文档化五机制；原 brew `--export/--import` 逻辑拆成 `runMemsyncBrewExport/Import`（行为零改动）；退出码沿用 0/1/2（partial）。
- **internal/tools/memory_sync_task_test.go**（11 个测试，tools 包 `TestTaskSync*` 全 PASS）：
  - export：pending live 行无 status、--limit/--tags、终态行携带 status、oldest-first、非法 --status 拒绝；
  - import：fresh 入队（from-peerbox 强制 + 原 tags 保留 + id 保留）、**幂等**（re-import all duped=3：2 live hash + 1 patch 内 dup）、live hash 防双投（不同 task_id 同 work → duped）、**不覆写**（HACKED summary/payload 不落地，tags 三方 union）、**前向迁移**（pending→completed + 重复 import 幂等 already）、迁移门（cancelled already 不迁移；in_progress→completed refused）、id 冲突不同 work（tags union only；ghost 终态 refused）、校验（空 summary/坏 source/缺文件）、from-unknown-node。
  - **零回归**：brew 通道 export→import 同库往返照旧（1 行 deduped，tasks 表不动）。

## Not Done

- `--tags` 过滤与 priority 列：tasks 表无 priority 列（P3 路线图），导出序用 created_at ASC（调度器自身顺序）顶替；`syncTaskPriorityDefault` 常量注释记录了这一点。
- 多 patch 合并 / 压缩传输：patch 即单文件 JSON，未做分片。
- 真实跨机（ssh peerbox↔HP 实机）：E2E 用同机双 GOPI_HOME 模拟两节点（数据面与真机一致——同一二进制同一 CLI 路径）；ssh 派发属 M4.7 已有通道。

## Verification

- Build：`go build ./...` OK（CGO_ENABLED=0 二进制亦过）。
- 单测：`go test ./internal/tools/ -run TestTaskSync -count=1` → ok（11/11）；`go test ./cmd/agent/ -run 'Memsync|TaskList'` → ok。
- 全量：`go test ./...`（后台 job，tools 包 ~220s 慢为 master 既有）。
- **E2E 实测**（/tmp/memsync-e2e/run-e2e.sh，双 GOPI_HOME 模拟 peerbox/HP）：
  1. peerbox submit 3 pending → `--export-tasks --status pending --limit 2` → HP `--import-tasks`：`queued=2`，HP task list 两行 pending、原号 t-* 保留、tags 带 from-peerbox；**再 import 同 patch：`duped=2`（幂等，hash 优先）**。
  2. HP `task update` → in_progress → completed → `--export-tasks --status completed` → peerbox `--import-tasks`：`migrated=1`，peerbox 该行 `[completed]`（前向迁移），其余行不动。
  3. brew `--export/--import` 往返照常（零回归）。
- Commit：见下方 git log（feat/memsync-tasks @ worktree ~/workspace/gopi-memsync-tasks）。

## 设计决策（给未来 agent）

- **hash 门统一放 id 分支之前**：epic 实测①要求"hash 重复 import → all duped"；若 id 分支先行，同 patch 重导入会报 already 而非 duped。终态行跳过 hash 门——sync-back 的身份是 task_id 而非工作内容（本地行 summary 与远端一致但语义是"迁移这一行"）。
- **`syncTaskMigrate` 不走 `taskAllowedTransitions`**：单机状态机里 pending→completed 非法（必须经过 claim/in_progress）。但 sync-back 行在远端已合法走完该生命周期；硬套状态机会让分布式补齐永远被拒。取而代之：pending-only CAS + 可前向目标白名单，比状态机更保守（live 行永不被远端终态覆写——claimed/in_progress 的本地行对 sync-back 报错而非迁移）。
- **终态 ghost 行 refused**：import 只写 pending（机制③），而"凭空造终态行"既是向后写也纵容重复执行（queue 一个已完成的任务会重跑）。
- CLI task submit 的 summary 是**位置参数**（`gopi task submit "summary" --payload ...`），不是 `--summary`（E2E 踩过一次）。
