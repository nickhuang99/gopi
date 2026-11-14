<!-- doc-class: historical -->
# row-per-resolve — merger 单点管控的 resolve 追踪 (2026-09-05, PA decision)

## 决策记录（PA 拍板）

nick 原话（EPIC 背景 2026-09-05）：*"I want to make sure the resolve-conflict
task is trackable in case retry is needed when it fails to resolve conflict or
even merger service crashes or so etc."* + *"all task creation process must
using same code path"*。Bond 裁决（feat/task-create-unified, 0a3449b）"不需 DB
行"——**部分正确但漏了 nick 的可追踪要求**。

PA 决策：**merger 单点所有权**（不 submit pending 队列 / 不外包 merge 业务——
保留 0a3449b 语义）与 **"resolve 登记行用于追踪"** 不冲突。行由 merger 独有
创建/管控/回收（`in_progress` 直达、不进 TaskNextFiltered 领取池、merger 唯一
读写者）**= 追踪载体非队列项**——单点所有权不受损。本文件承接
`devdoc/merger-self-resolve-20260905.md` 与 `devdoc/task-create-unified.md`
(doubao 备忘映射修订见下)。

## 行的语义与判别

| 字段 | merger resolve 追踪行 | 说明 |
|---|---|---|
| summary | `resolve merge conflict: <branch>` | 独立端点；legacy Submit:true dispatch 的 summary 是 `<branch> into master`，不冲突 |
| status | `in_progress`（创建即定） | **生而为 in_progress（永不定 pending）→ worker/serve/next 结构性领取不到** |
| tags | `merge-conflict,auto-spawn` | **auto-spawn 是 merger 专属判别**；legacy 用 `merge-conflict,auto-dispatch`，永不排除 |
| source | `auto` | |
| payload | = `tools.MergeResolveBriefing(...)` | 与 spawned child 的 stdin 同文本（同 code path） |
| reason (创建) | `git merge conflict needs llm resolve` | |
| deliverable | `{"repo":repo,"branch":branch}` | |
| claimed_by | merger 自身 `host:pid` (TaskStampSelfClaim) | 崩溃恢复的判活依据 |

## 生命周期（MergerRepoSweep self-resolve face = 单点 owner）

行创建/回收织入 **`MergerRepoSweep` 的自 resolve 块**（opt.Resolve != nil），
不是 cmd resolver——这个块已同时拥有 Resolve 调用 与 随后的 re-land
(taskMergeOne) 两条 resolve 生命周期所需边；真实 cmd merger 与每个单测走同一条
生产代码路径。git-pure legacy nil-Resolver face 完全无行（unchanged）。

流程（repo_sweep.go 自 resolve 块）：

1. **resolve 前登记行**：`resolveRowCreate(repo,"master",branch)` → TaskNewOID
   (in_progress) → `resolveRowClaim` (TaskStampSelfClaim = merger 自身 pid)。
   create 失败仅 log-and-continue（resolve 落地比缺审计行更重要）。
2. **Resolve(repo,"master",branch)** 阻塞运行（spawn 真正 child 在 cmd
   mergerResolveConflict 内，child 与 merger 同进程组、随 merger 崩一起死）。
   行 id 存进 `res.ResolveRowIDs`（返回给 cmd/测试读回）。
3. **Resolve 返回错误** → 行 `failed`，reason 用 resolver 错误文本
   （已含 "child exited N"/"timed out"）；分支不动，下 tick 幂等重试。
4. **Resolve 返回成功** → **worktree 二检**（req #6）：`repoSweepProtectedBranches`
   重探受保护分支；
   - 分支此时受保护（resolve 中途被 bond 重新 checkout）→ **不 merge**，
     行 `cancelled`（reason "worktree re-created during resolve"——`cancelled`
     不在 error 语义，用 cancelled+reason），受保护计数++。
   - 否则 `taskMergeOne` re-land → `merged` → 分支删 → 行 `completed`
     (reason "resolved + merged")。re-land 非 merged（意外）→ 行 failed
     "resolve landed but re-merge did not land",分支留待下 tick。

## 崩溃恢复扫描（merger restart 场景，req #5）

`tools.MergerResolveRecoverScan()` — merger 启动 + 每 tick（mergerPassOnce 顶部）
跑。扫描全部 in_progress 行，命中 `merge-conflict AND auto-spawn`：
- claimed pid（host:pid 解析）**死**（signal-0 ESRCH）→ 行 `failed`
  (reason "merger restart: ... gone — retry next sweep")。分支被崩溃原地留下
  → 恢复后下个 sweep 幂等重试。
- pid **活** → 跳过（慢 resolve 永不误杀）。
- 仅 merge-conflict 无 auto-spawn（legacy/adoption 形状）或解析不信任
  （foreign host / 空 / malformed）→ 跳过（未知 ≠ 死）。
- ledger 文件不存在 → **静默 no-op**（不创建空 ledger）。

## Worker gate（req #2, E2）

行 `in_progress` 已保证 TaskNextFiltered（只领 `pending`）领取不到 —— 硬保证。
TaskNextFiltered 另加防御性 gate：tags 同时含 merge-conflict+auto-spawn 的行
即便被某未来路径误建为 pending 也**永不派发给 worker**（单点所有权 gate）。
legacy `merge-conflict,auto-dispatch` 行不受影响（无 auto-spawn）。

## 单测/语义升级

- 原 3 处"零行"测试语义升级（非回退）：
  - `repo_sweep_test.go::TestMergerRepoSweep_SelfResolveConflict`：
    从"0 行"→ 行存在且 completed（claimed、tags 含 auto-spawn、deliverable.repo）。
  - `..._ResolverFailureLeavesBranchForNextTick`：首次 failed（resolve 错误
    reason）+ 分支留现场；restart 新行 completed。
  - `cmd/agent::TestMergerSweepRepoSelfResolvesConflict_SpawnPath`：真 spawn 路径
    行 minted 且 completed。
- 新增 `internal/tools/repo_resolve_row_test.go`：E4（recover scan 关死 pid 行 /
  跳过活 pid + 非 auto-spawn）/ E2（next 领不到 auto-spawn 行）/ E5（resolve 中
  re-checkout → 不 merge + cancelled）。
- E6（统一 INSERT 不回归）：insertTaskRow 全绿，本改造不改 insertTaskRow。

## 文件域改动

- `internal/tools/repo_resolve_row.go`（新）：生命周期 helpers + RecoverScan +
  MergeResolveBriefing (从 cmd 移来，一层共享)。
- `internal/tools/repo_sweep.go`：自 resolve 块织入 create/claim/finish +
  worktree 二检 + `RepoSweepResult.ResolveRowIDs`。
- `internal/tools/tasks.go`：TaskNextFiltered 排除 auto-spawn（防御 gate）。
- `cmd/agent/merge_resolve_child.go`：briefing 委托 tools（删私有副本）。
- `cmd/agent/cli_tasks.go`：mergerPassOnce 顶部接 recover scan；
  mergerSweepRepo 返回 RepoSweepResult（供 cmd e2e 读回行）。

## doubao 备忘映射修订

task-create-unified (0a3449b) 备忘映射在行数层的"不需 DB 行"判定，对
**纯 git merge 业务**仍成立（Submit:false 永不生成 dispatch 行）；但扩展为
nick 的可追踪需求后，merger 自 resolve **登记一个追踪行**（非队列项、merger
单一 owner）。两者不矛盾：行是追踪载体；merge 决策与行解耦（如 merger-config
纪要：*"Whether a task row tracks a branch is irrelevant to the merge
decision"*）。请未来 agent 勿再回到"merger 完全无行"的旧理解——现在 merger
自 resolve **有行**，只是这行它独有、在_progress、不进领取池。
