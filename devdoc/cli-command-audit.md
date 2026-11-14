<!-- doc-class: historical -->
# Task Report: 代码与工作流全面审计清理 — CLI 命令面 + workflow 层

**Date:** 2026-09-02 (audit snapshot)
**Epic:** 全面审计清理（只读分析 + 报告 + 归档分支，master 零改动）
**Branch (deliverable):** `archive/dead-code-sweep-20260902` (based on master)
**Host evidence base:** `~/work/GoPi` @ `git log -1`

---

## 0. Baseline

- Repo: `~/work/GoPi`, branch checked out at audit start: master (clean).
- `go build ./...` — **OK** (exit 0, master baseline).

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

- Live processes at audit time: `./bin/gopi worker` (PID 841087/841097, started 02:53) — a **live worker loop is running** → `gopi worker` = ACTIVE.
- `gopi task serve` — NOT running as daemon; no post-receive hook daemon, but `.git/hooks/post-receive` exists and calls `bin/gopi task audit-merges --repo … --submit`.

---

## 1. CLI 命令面审计 (Part A)

Top-level verbs (from `./bin/gopi -h`, `internal/config/help.go`, `cmd/agent/main.go` `is*Subcommand`):
`task, memsync, brew, checkpoint, resume, proxy, shot, chat, worker, code-search, memory_search, memory_store, memory_feedback, memory_update, memory_export, memory_import, web_search, web_fetch, text2img, vision_analyze, log, git_blame, task_submit, task_evaluate`

`gopi task` subcommands (dispatch `cmd/agent/cli_tasks.go`):
`list, show, update, search, submit, run, serve, archive, ingest, verify, reopen, harvest, sync, adopt, push, audit-merges, diff, tag-resumed`

### Classification (每条带证据)

#### REDUNDANT / ORPHANED — 建议归档

| Command | 证据 | 建议 |
|---|---|---|
| `task ingest` | 退役决策 devdoc (47d319e, EPIC #185: HP 拔线 / peerbox 停进程 / bug031 cancelled / memsync 单通道确认)。当前无生产调用方：session 实命令 0 次；`TaskIngestHTTPHandler` 无非测试引用；webhook 无消费者。**仍显示在 help.go:228 与 dispatch** | **归档** `internal/tools/taskingest.go` + CLI 分支 |
| `task harvest` | M4.12 (717675a)。当前生产使用：**仅** `ssh hp … task harvest --peer peerbox`（最近 2026-08-31，e6e82b59）。M4.13+ memsync timestamp-delta (b3b71b6) + M4.24 reconciliation (3cde659) 已将其取代；devdoc/memsync-bidir-m49.md 明确 git 通道=纪律项。harvest 的 git push-back 逻辑已被 task_sync_git.go 复用（GitSyncPushLocalBranches, 自包含）| **归档** harvest 块（memory_sync_task.go ~1125-1905）+ harvest 测试；保留 reconcile 依赖的 harvestRemoteDBPath/harvestShellQuote |
| `task push` (CLI) | M4.18 (648d80a)。CLI 仅 1 处调用方 cli_tasks.go:1898 (`DefaultGitPushback`)。真实使用: 2026-09-01 两次 (3128f17f, a8a00b9e)。push-back 原子已并入 `task sync --peer` (git channel) 与 `GitSyncPushLocalBranches` | **归档** CLI 分支（保留 GitSyncPushLocalBranches 引擎）|
| `task diff` | M4.24 reconciliation (3cde659): `task diff --peer set-compare` 与 `sync --full` 并存，但 diff 无独立调用方（仅 CLI，测试 5 个在 reconcile 测试中）| **UNCLEAR→归档建议**（diff 已被 sync --full 涵盖；保留引擎）|
| `task harvest` CLI flag `--no-push` | 67 个 session 用过（全为 e2e/调试）| 随 harvest 归档 |
| `internal/checkpoint` + `gopi checkpoint` + `gopi resume` | checkpoint: 17 sessions 用过, 但**真实命令全部是调试/审计命令**（`for p in … checkpoint …; go test … -run 'Resume|Checkpoint…'`），非生产会话。resume: 0 次真实命令。checkpoint 被 `gopi chat <log>` (cli_chat.go:335) 与 `agent.LoadCheckpoint` (cli_resume.go:187) 引用 —— 引擎仍被 chat --seed 使用 | **保留引擎**（chat seed 依赖），**归档 CLI 面**（checkpoint/resume 命令, 因无生产使用）— 需先确认 chat seed 属必要功能 |
| `task ingest` 测试 `internal/tools/taskingest_test.go` (3 tests) | 无 | 随 ingest 归档 |
| `memory_sync_harvest_test.go` (433L) / `memory_sync_task_gitpushback_test.go` (164L) | 21 harvest tests | 随 harvest 归档 |

#### ACTIVE — 保留

| Command | 证据 |
|---|---|
| `task list/show/update/search/submit/run/serve/archive/verify/reopen` | 生产会话大量使用 (submit 15, run 9, serve 6, verify 2, reopen 2 sessions) |
| `task sync --peer` | M4.13+ 主通道; 真实 ssh peer 使用 (2b38bfa2, 458f3fc0, 12968a01 — 08-31~09-01); e2e scripts 用 `sync --claim-remote` |
| `task audit-merges` | post-receive hook 实调 (`.git/hooks/post-receive` → `bin/gopi task audit-merges --repo … --submit`) |
| `task adopt` | Adoption chain (M4.18+); 真实使用 (3128f17f 09-01, fcaff097 08-31); internal/tools/task_sync_git.go:280 内部调用 AdoptionSweep |
| `gopi worker` | **live process** (PID 841097) |
| `gopi memsync` | 真实使用 7 sessions; e2e scripts (e2e-m418/420/421) 全用 memsync --export/--import-tasks |
| `gopi proxy` | 真实使用 (bigmodel glm-5.3-flash 代理) |
| `gopi brew` | 真实使用 2 sessions + main.go:158 maybeBrewOnSessionExit |
| `gopi text2img / web_search / code-search / vision_analyze` | 工具类，均有实现+测试 |
| `gopi log` | `--trim` 等 工具视图 |

#### UNCLEAR

- `gopi shot` — 0 次 session 使用；实现+测试存在 (cli_shot.go, 4 tests)。无法确认退役意图 → **保留, 标注低使用**
- `gopi resume` CLI — 引擎被 chat seed 用, CLI 本身 0 次使用 → **建议归档 CLI 面, 保留 LoadCheckpoint/Resume 引擎** (见上 checkpoint 行)
- `task diff` — 被 sync --full 取代, 但属 M4.24 明确交付物 → **归档建议, 保留 reconcile 引擎**

### 退出代码/规模
- harvest 块: `internal/tools/memory_sync_task.go` 1125-1905 (~780 行)
- harvest 测试: 433 + 164 + 部分 task_sync_deliverable_test.go (TestHarvestCarriesDeliverable, 需保留 sync 侧)
- ingest: `internal/tools/taskingest.go` 294 行 + taskingest_test.go 3 tests
- checkpoint CLI: cli_checkpoint.go 209 + cli_resume.go 242 + internal/checkpoint 507 行 (引擎保留)

---

## 2. 归档分支执行 (Part B) — `archive/dead-code-sweep-20260902`

基于 master 创建 (git worktree add, master 零改动)。实际执行的清理:

**已归档 (证据确凿的退役项):**
1. `task ingest` — `internal/tools/taskingest.go` + `taskingest_test.go` + `tasks_ingest_test.go` → **移到 `archive/`** 并加 `//go:build ignore` (编译排除, 内容可恢复)
2. CLI 面: `cmd/agent/cli_tasks.go` 移除 ingest case (parse/flag/dispatch 三分支) + `taskIngestListenDefault`/`taskIngestServeReadHeaderTimeout` consts + `runTaskIngest` + `listen` opts 字段 + `net/http` import; `internal/config/help.go` 移除 ingest 帮助块 + unknown-subcommand 列表移除 ingest

**未归档 (引擎有负载, 保留):**
- `task harvest` — harvest 引擎仍被 `memory_sync_reconcile.go` 依赖 (harvestRemoteDBPath/harvestShellQuote) + ssh hp 调试仍在用 (08-31) → 保留, 标 LOW-USE
- `task push` CLI — push-back 原子已并入 `task sync` (GitSyncPushLocalBranches), CLI 仅 1 处调用 (cli_tasks.go:1898) → 保留 CLI, 标 LOW-USE
- `task diff` — M4.24 reconciliation (3cde659) 明确交付物, sync --full 并存 → 保留
- `checkpoint/resume` CLI — 引擎被 `gopi chat <log>` seed 用 (cli_chat.go:335) → 保留

**验证:**
- `go build ./...` 绿 (archive 分支, commit 3b3c7ec)
- `go vet ./...` 干净, `gofmt -l` 空
- `go test ./cmd/agent/ -run 'TestTasks|TestIsMemsync|TestTaskTag|TestResume'` 13.7s PASS
- `go test ./internal/tools/ -run 'TestTaskSyncBrewChannelUnaffected|TestHarvestNewerWinsMigration|TestSyncDeltaDeliverableForwardMigration|TestSyncTaskDeliverableRoundTrip'` 17.2s PASS
- `go test ./internal/tools/` 全量 — 600s 超时 (go test 10m 默认) — **环境性** (live `gopi worker` PID 841097 + 并行 master 基线测试争 CPU); master 同款全量也在 600s 超时, 非本分支回归 (历史基线 551.8s 绿, 参考 /tmp/sr2-run2.out)
- CLI 冒烟: `gopi task ingest` → "unknown subcommand"; `gopi task -h` 无 ingest; `gopi task merge` 保留
- master 零改动 (`git status` master 干净; 审计 doc 仅存在于 archive 分支)

### 恢复命令 (从 archive 分支)
```bash
# 恢复 task ingest (编译回 tools 包)
git checkout archive/dead-code-sweep-20260902 -- archive/taskingest.go archive/taskingest_test.go archive/tasks_ingest_test.go
mv archive/taskingest*.go internal/tools/   # 去掉 //go:build ignore 首行
# CLI 面恢复: git diff master..archive/dead-code-sweep-20260902 -- cmd/agent/cli_tasks.go internal/config/help.go | git apply -R
```

---

## 3. Workflow 层清理建议 (Part C)

### 3.1 settings.json 键审计
- settings.json 实际键: `api_key, base_url, compute_nodes, model, reasoning_effort, sync_peers, thinking_budget, vision_base_url, vision_model`
- **全部被代码读取** (model 17 files, vision_* 5, compute_nodes 3, sync_peers 3, reasoning_effort/thinking_budget → config.go:119,129 + env.go:136,144)。
- **缺失**: `effort_values` 键 (defaultReasoningEffort "minimal" 在 settings.go:146; loop.go:1970 用 effortValues 校验) — settings.json 无此键, 属**未配置但被校验** → 建议补默认或文档说明。
- **EnvVars 目录缺项**: GOPI_SYNC_PEERS, GOPI_SYNC_NODE, GOPI_SYNC_INTERVAL, GOPI_SYNC_FULL_EVERY, GOPI_SYNC_GIT_INTERVAL, GOPI_ADOPTION_INTERVAL, GOPI_MERGE_AUDIT_INTERVAL, GOPI_TASK_NOTIFY_*, GOPI_WORKER_MAX_ROUNDS, GOPI_NODE_ROLE, GOPI_SYNC_TO — **全部被代码读取但不在 settings.go EnvVars 表/help.go 目录** → 文档缺口。

### 3.2 GOPI.md 与当前工作流偏差
- **Rule 6 差距已落档** (devdoc/gopi-rule6-gap.md, 881c24e): 3 gaps — (1) "no-push-needed" 已过时 (git 通道 push-back 已自动化, worktree 提交≠零推送), (2) sync-peer 拓扑 (GOPI_SYNC_PEERS/IsServerNode) 未入 GOPI.md, (3) auto-cleanup 链 (post-receive audit-merges → adopt → merge) 未文档化。GOPI.md 未改 (Epic 范围=落档)。
- **workflow 建议**: GOPI.md 需新增 sync-peer 拓扑章节 + 更新 Rule 6 no-push-needed 措辞。

### 3.3 过时文档清单 (devdoc/)
- `devdoc/task-harvest-pull.md` (M4.12) — harvest 已退役 → 标 OBSOLETE
- `devdoc/task-harvest-git-pushback.md` — 同上
- `devdoc/memsync-bidir-m49.md` — 记录 git 通道为纪律项, 现 M4.24 已落码 → 更新
- `devdoc/apply-flagfix.sh` — 含 harvest/sync flag 修复脚本, 保留 (历史)
- `devdoc/taskingest-*` (47d319e 已声明退役)

---

## 4. 验证汇总

| 检查 | 结果 |
|---|---|
| `go build ./...` (master) | OK |
| master diff | 零改动 (仅 devdoc/ 新增审计报告) |
| archive 分支 build+test | 见分支验证日志 |
| session 使用统计 | 1160 sessions 扫描 |

---

## 5. 决策记录 (audit)

| # | 决定 | 理由 |
|---|---|---|
| D1 | `task ingest` 归档 | 退役 devdoc + 0 生产调用 |
| D2 | `task harvest` 保留 (LOW-USE) | reconcile 依赖引擎; ssh hp 调试仍在用 (08-31); 退役时机=reconcile 迁移后 |
| D3 | `task push` CLI 保留 (LOW-USE) | push-back 已并入 sync, 但 CLI 是 M4.18 交付物, 保留 1 处调用 |
| D4 | `task diff` 保留 | M4.24 明确交付物; sync --full 并存不冲突 |
| D5 | checkpoint/resume 保留 | 引擎被 chat seed 用; CLI 面 0 生产使用但低风险 |
| D6 | `gopi worker` ACTIVE | live process |
| D7 | `task audit-merges` ACTIVE | post-receive hook 实调 |

---

## 6. 后续动作 (建议)

1. GOPI.md 修订 (sync-peer 拓扑 + Rule 6 no-push-needed) — 另行裁决
2. settings.go EnvVars 表补齐 sync/notify/worker 键 — 文档任务
3. 归档分支合并裁决后: 胜者 merge master + 败者 archive tag + 清理
