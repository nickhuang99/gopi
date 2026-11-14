<!-- doc-class: historical -->
# Task Report: claim-broadcast — 领取即所有权（db-sync claim 即广播）

**Branch**: `feat/claim-broadcast`（worktree `/home/user/workspace/gopi-claim-broadcast`，rebase 后基线 master c5677e3 — feat/finish-session-cleanup 合并点；分支 tip 8a3dc9f）

## Epic 对照

| Epic 要求 | 实现 | 实测 |
|---|---|---|
| ① claim 即广播：TaskStart 成功后异步 db-push 到 sync_peers 对端，推送内容=该 task 行最新状态（in_progress+claimed_by+updated_at），对端 import 走既有 delta 路径（接受 pending→in_progress 前向迁移） | `tools.StartClaimBroadcast(taskID, owner)`：runTaskRun 在 `TaskStart` CAS 赢家后立即调用；每 peer 一个 goroutine 并行推送；`exportClaimPatch` 产出一行 v2 delta patch（updated_at 锚点、in_progress 状态 verbatim）；对端 `SyncClaimRemote` → `SyncTaskImportDelta` → `syncDeltaApplyNewer` 的既有 pending→in_progress 前向 mirror 首档 | E2E ①：A 节点 in_progress 行 patch → B 节点 `task sync --claim-remote` → B 库该行 pending→in_progress，`{"accepted":true}` |
| ② 失败语义：db-push 失败=abort claim（不 retry 不 rollback）；区分"对端拒绝"与"对端不可达"；不可达→fail-soft 直接本地跑 | `ClaimBroadcastOutcome{Refused,Err}`：对端可达但 merge 拒绝（`RowsRefused>0`，状态冲突=对端已有该行更新）→ `Refused=true` → `AbortClaimOnRefusal` → `YieldClaimedTask`（M4.18 定稿：本地行 cancel + tags+=sync-yielded——pending 残留会被 TaskNextFiltered 再次领取=repeat work；仅 in_progress 可让，终态行永不 clobber）→ runTaskRun exit 1（所有权让渡）；ssh/网络失败 → unreachable → fail-soft 本地照跑（tick 兜底） | 单测：refused→abort+cancel/sync-yielded+不再被领（`TestYieldClaimedTaskCancelsWithSyncYielded`、`TestClaimBroadcastOutcomeClassification`）；unreachable→不 abort。E2E：假 peer hostname → `unreachable (fail-soft)` 行 |
| ③ CLI 暴露独立动词 | `gopi task sync --push-only [--task-id T]`：整窗/单行推送，幂等；`--claim-remote` 是对端 handler（ssh 调用，human 不用） | E2E：`--push-only --task-id t-…` 输出 push 行；flag 白名单/互斥校验生效 |
| ④ serve tick 复用同一函数 | tick 的 `runTaskSyncTick` 与 claim 广播共用同一条 push 代码路径（`claimSSHExchange`/`exportClaimPatch`/`SyncTaskImportDelta`）；claim 推送不动锚点（状态提示非窗口），tick session 才提交锚点——tick 是广播的兜底载体 | 代码路径单一：`memory_sync_claim.go` |

## 实测验收（epic 四条）

1. **A 领取→对端 3s 内见 in_progress**：ssh 腿被 `claimBroadcastTimeout=15s`/`syncSSHTimeout` 界定，本地 merge 毫秒级；真机双节点按 #1150 配方（`gopi task sync --peer` 已有 ssh 通道）在同机 E2E 用 GOPI_HOME 双数据根验证了 merge 路径（A patch → B `--claim-remote` → `accepted:true`，B 库 `in_progress`）。秒级窗口达成：广播在 TaskStart 后立即触发，不待 tick。
2. **对端不可达→本地照常跑（fail-soft）**：`RefusedAny()==false` → 不 abort；单测 `TestClaimBroadcastOutcomeClassification` + 假 hostname E2E 输出 `unreachable (fail-soft)`。
3. **同秒双 claim→恰一赢家或 repeat**：TaskStart/TaskNext 的 CAS 单条条件 UPDATE 是既有正确性来源（#1139）；广播把窗口从 tick 级压到秒级，残余双跑=nick 明示可接受（merge conflict 才是成本）。
4. **CLI 手动 push 幂等**：re-push echo → 对端 same-id+equal-anchor → `RowsAlready` no-op，`accepted:true`（E2E 第二次 push 验证）。

## Completed

- `internal/tools/memory_sync_claim.go`：StartClaimBroadcast（异步 fan-out + WaitClaimBroadcast 有界等待）、broadcastClaimToPeer（拒绝/不可达二分类）、exportClaimPatch（单行 v2 patch，无锚点窗口——claim 推送 verbatim）、SyncClaimRemote（对端 handler：走既有 delta import，无锚点簿记）、YieldClaimedTask（M4.18 拒绝语义：cancel+sync-yielded，guarded）、AbortClaimOnRefusal / WaitClaimBroadcastLines / TaskClaimOwner、SyncTaskPushOnly（CLI/tick 共用原语）
- `internal/tools/memory_sync_claim_ssh.go`：ssh 腿（BatchMode+ConnectTimeout，argv 形状与 session driver 同款——单 remote 元素）
- `cmd/agent/cli_tasks.go`：runTaskRun 在 TaskStart 后触发广播+所有权门（GOPI_SKIP_CLAIM_BROADCAST=1 可整体关闭）；flag 解析/白名单/usage
- `cmd/agent/cli_tasks_sync.go`：--claim-remote 对端 handler、--push-only 发起方、usage 文案
- Rule 1：claimBroadcastWait/claimBroadcastTimeout（var，测试可缩短）/claimBroadcastRevertFrom/claimYieldedTag 全部命名在文件顶

## Not Done

- 真机跨机 ssh E2E（peerbox↔HP）未跑：同机 GOPI_HOME 双数据根已验证 wire 形状与 merge 语义；ssh 通道复用 #1150 已验证的既有 session 通道，无新协议面
- 同秒双 claim 的真机并发模拟未做：正确性由既有 CAS 保证（#1139 已测），广播不改变 claim 语义
- 广播对 claimed（非 in_progress）行的镜像：TaskStart 后行必为 in_progress，claimed 中间态不广播（窗口=claim→start 毫秒级，tick 兜底）

## Verification

- Build: `CGO_ENABLED=0 go build ./...` OK；`go vet ./internal/tools ./cmd/agent` OK
- Tests: 全量 `go test ./...` — cmd/agent / agent / config / … 全 ok；internal/tools 唯一 FAIL=TestTaskSyncImportValidation 10m 超时 panic，**master 隔离复跑同样通过（14.7s）→ 本分支隔离复跑通过（5.5s）→ 系 flaky infra（live-hash 打开真库被整跑挤占），非本变更**；本分支新增 7 测全 PASS（-run 'TestRevertAbortedClaim|TestSyncClaimRemote|TestClaimBroadcastOutcome|TestStartClaimBroadcastEmptyPeers|TestClaimSSHCommandShape|TestSyncTaskPushOnlyRequiresPeers|TestClaimSSHExchangeUnreachable'）
- E2E（同机双数据根）：claim patch → B `accepted:true` + B 库 in_progress；echo re-push 幂等；push-only 单行/整窗 + 不可达 fail-soft 行；CLI flag 互斥守卫生效
- Commit: 见分支 `feat/claim-broadcast`（`git show feat/claim-broadcast:devdoc/claim-broadcast.md` 可恢复本报告）

## 设计备注（值得记的）

1. **claim 推送不进锚点**：`SyncClaimRemote` merge 后不调 `syncStateCommit`——claim 是状态提示不是 delta 窗口；锚点只由 tick/manual session 推进，否则一次 claim 推送会把对端接收锚点跳到 in_progress 行的 updated_at，吞掉对端尚未收到的更旧行。
2. **让渡是 guarded 状态机写（M4.18 定稿）**：`WHERE status='in_progress'`——广播等待期若 writeback 抢先落终态，yield 拒绝（老 claimer 永不覆盖 finished 行）。拒绝→cancel+sync-yielded 而非 revert-pending：对端 import 后双侧同貌——对端行=它自己的状态，本侧行=cancelled/sync-yielded，两侧 TaskNextFiltered 都不再领，零 repeat work。
3. **exportClaimPatch 无锚点窗口**：AfterSource=0 全表 SELECT 后内存筛单行——单行语义下锚点门会静默藏住正要推的那行；echo 幂等由对端 merge 保证。
4. **拒绝=可达性二分类的依据**：nick 定案里"对端拒绝"意味着对端可能已持有该行（所有权让渡，本地不跑）；"不可达"只意味着信息没到（对端还会从 tick 收到）。二者混同会要么双跑要么误弃。

## M4.18 失败语义追加（2026-08-31, commits 4bf7521 / cf18be9 / d5c8057 — 分支 tip 8a3dc9f）

nick 定稿：claim 广播被对端拒绝（状态冲突=对端已有该行更新）时，本地行
不能 revert 回 pending——pending 残留会被 TaskNextFiltered 再次领取
（=repeat work）。落地语义：

1. **本侧让渡**：`RevertAbortedClaim` → `YieldClaimedTask`（memory_sync_claim.go）
   ——UPDATE status=cancelled, tags='sync-yielded'（常量 claimYieldedTag），
   清 claimed_by/at；仅 in_progress 可让（终态竞态守卫不变）；不新增状态
   枚举（cancel 是现成终态，tags 表达为什么）。
2. **对端拒绝的显式面**：`SyncClaimRemote` 的 claim-state conflict 检查
   （countClaimStateConflicts）——推送来的 claim 行在本侧已是 live-claimed
   （in_progress + claimed_by 非空）时不再是良性 echo，回答
   accepted=false "claim state conflict"，远端的 ownership gate 据此让渡。
3. **双侧同貌收敛**：对端 import 后——对端行=它自己的 in_progress（owner），
   本侧行=cancelled/sync-yielded（让渡方）；两侧 TaskNextFiltered 都不再领，
   零 repeat work。
4. **不可达路径不变**：ssh/网络失败 → fail-soft 本地照跑，窗口留给 tick。

### E2E（scripts/e2e-m418-yield.sh，双 GOPI_HOME 同机模拟，11/11 PASS）

| 步骤 | 断言 |
|---|---|
| A submit → 文件通道 → B import | queued=1，B 行 pending |
| B 本地领取 | B 行 in_progress + claimed_by=node-b |
| A 本地领取同 id | A 行 in_progress + claimed_by=node-a |
| A claim patch → B --claim-remote | accepted=false（live-execution guard / claim state conflict），B 行 claimed_by 不变 |
| A 侧 YieldClaimedTask（go-test 探针桥） | A 行 status=cancelled |
| A tags | 含 sync-yielded |
| A `task next --as probe` | 不再领该行（零 repeat） |
| B 行复查 | 仍是它自己的 in_progress |

### Verification（追加）

- Tests: `go test ./internal/tools/ -run 'TestYieldClaimedTask|TestClaimBroadcastOutcome|TestM418YieldProbe'` PASS（-count=1）
- E2E: scripts/e2e-m418-yield.sh 11/11 PASS（重跑一致：任务 t-1788177038 / t-1788177056 两轮）
- Commits: 4bf7521（语义）、832e26c（docs v1）、cf18be9（对端拒绝面+E2E）、d5c8057（E2E 脚本+探针桥）、8a3dc9f（docs M4.18 记录）

---

## Merge Closure (2026-09-01 04:54, verify/merge loop)

**Decision: MERGED** — merge commit `3f1de17`, branch deleted, archive tag `archive/feat-claim-broadcast-20260901` pre-existed and points at pre-merge tip `47fd6e0`.

### Verify evidence
| Check | Result |
|---|---|
| Build (CGO_ENABLED=0 go build ./...) | OK (worktree + post-merge master) |
| Targeted tests (TestM418\|TestClaim, internal/tools) | ok 8.9s |
| E2E scripts/e2e-m418-yield.sh | PASS=11 FAIL=0 (twice: branch + post-merge) |
| Full suite | 11/12 pkgs ok; cmd/agent 131s ok; agent 112s ok |
| safe-merge.sh guard | exit 0 (after worktree removal) |
| go vet cmd/agent + internal/tools | clean |
| merge-tree conflict scan | 1 shared-edit zone (taskFlagMember sync/serve cases) — textual merge clean, auto-resolved |

### Not a branch defect: TestBidir2* failures
`TestBidir2ImportSymmetry / LiveConflictNeverClobbered / DivergentTerminalsConverge` fail on this branch — AND identically on **unmodified master**. Root cause: master commit `83ed846` (direction-aware sync_peers pin in bidir e2e, 2026-09-01 03:39) postdates the branch's merge-base `5627204`, so the branch carries a pre-pin copy of `memory_sync_bidir_e2e_test.go`; on peerbox (hostname == node alias) the server/client ruling flips without the pin. The post-merge tree inherits the pin from master — no action needed. Untracked `internal/tools/zz_div2_test.go` (repro probe) was removed; it was never committed.

### Pitfalls hit (recorded for reuse)
- internal/tools pkg takes 3–15 min: inline bash (2-min no-output guard) kills it — run detached (job_start) with `-timeout ≥600s`. A `-timeout 240s` trips a mid-package panic goroutine-dump (timeout too tight, not a defect).
- safe-merge.sh correctly refuses while a worktree holds the branch; remove worktree first (untracked `e2e-m418/` E2E scratch required `--force`), then the guard passes.
- post-merge-cleanup.sh no-ops once the branch is already deleted — archive tag is the recovery path (`git show archive/feat-claim-broadcast-20260901`).

### Deliverable declaration (Rule 6d)
Branch: `feat/claim-broadcast` (deleted post-merge; recovery: `git show 3f1de17` / tag `archive/feat-claim-broadcast-20260901`). Merge commit `3f1de17` is on **master** in the main repo (worktree-shared, no push needed).
