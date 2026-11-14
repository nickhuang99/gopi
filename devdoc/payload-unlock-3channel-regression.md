<!-- doc-class: historical -->
# Task Report: payload 解锁 epic [5/5] — 三通道回归取证 + 单机零回归证据

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

- 任务行：`01a0fea1-6821-7135-9690-d7e3183be4e6` · branch `task/01a0fea1682171359690d7e3183be4e6`
- 取证基点 `base` = `99074b60`（epic 四项改动全部落地之前的最后一次 merge）
- 取证终点 `tip` = `8fb37004`（= master tip，本 run delivery-point sync 后）
- 结论：**三条通道未被破坏；单机模式零回归。** 但 epic 有一项**从未落地**（见 `## Not Done` 第 1 条），它不在本 run 之前被证明的范围内。

---

## Files

**新增**
- `devdoc/payload-unlock-3channel-regression.md` — 本报告
- `devdoc/DOC-INDEX.tsv` — +1 行（Rule 36，与本文件同一 commit `76fe3c4a`）

**未触碰（取证任务，零产品改动）**
- `internal/tools/memory_sync_claim.go` / `memory_sync_session.go` / `memory_sync_reconcile.go` — 本 run 未改（且 base..tip 逐字节相同）
- `cmd/agent/**`、`internal/config/**`、`internal/tools/**` 的任何生产代码 — 未改
- 无新增测试、无新增脚本、无新增依赖（Rule 35）

**证据文件**（Rule 37，全部在 session 目录，**不入 worktree**）
`/home/user/.gopi/sessions/01a0fea1-6821-7135-9690-d7e3183be4e6/evidence/`
`structural_proof.txt` · `tip_all_four_items.txt` · `tip_per_test.txt` · `base_99074b60_control.txt` · `tip_heavy_channel_tests.txt` · `base_heavy_channel_tests.txt` · `tally.txt` · `single_node_cli.txt`

**派发**
- `task_submit` → `01a0fef0-29a3-7b78-9fc0-a25c609094d7`（`bug,test-fixture`, priority 1）
- 已存在且仍 `pending` 的承接者：`01a0fee7-768d-7050-ae11-cd3612f52aab`（epic 第 3 项 rework，未重复提交）

---

## Completed

### 1. 靶向 `-run` 集的真实输出（`make test-budget PKGS='./internal/tools ./cmd/agent'`）

一个 `-run` 集覆盖三条通道 + 单机模式共 **43 组**前缀，按通道分组：

| 通道 | 覆盖的测试前缀 | tip 通过数 |
|---|---|---|
| **claim-lock**（`memory_sync_claim.go`） | `TestClaimLock*` `TestClaimSSH*` `TestClassifyClaimLock` `TestExportClaimPatch` `TestRunClaimLock*` `TestStartClaimLock*` `TestSyncClaim*` `TestWaitClaimLock*` `TestRollbackLocalClaim*` `TestLiveRowsOwnedByOthers*` `TestAnchorGuard` `TestSyncAnchorGuard*` `TestClaimRemote*` | **37** |
| **session**（`memory_sync_session.go`） | `TestSyncPeerSession*` `TestSyncPeerStatus*` `TestSyncTick*` `TestSyncSession*` `TestSyncRemote` `TestSyncFull*` `TestSyncTag*` | **28** |
| **reconcile**（`memory_sync_reconcile.go`） | `TestReconCompare*` `TestTaskDiff*` `TestSyncDelta*` `TestBidir*` `TestSyncTaskPushOnly*` `TestSyncRefusals*` `TestPushTerminalTaskSync*` | **64** |
| **单机模式**（`sync_peers=[]`） | `TestRunClaimLockProtocolNoPeers` `TestStartClaimLockBroadcastEmptyPeers` `TestWaitClaimLockVerdictLockRoundAnswersProceedSingleNode` `TestRunWorkerClaimLocalOnlyWithNoPeers` `TestServeGitSyncSweepNoPeerIsPureNoOp`(+`UnderTaskSyncOn`) `TestServeGitSyncSweepWithPeerAdvancesClock` `TestServeGitSyncThrottles` `TestPushTerminalTaskSyncNoPeersIsSuccess` `TestSyncPeerSessionTagsRefusedWhenNoPeersDeclared` `TestSyncTaskPushOnlyRequiresPeers` `TestSyncStatusWithNoPeerIsASilentEmptyAnswer` `TestTaskDiffNoPeerDeclaredIsSingleNode` `TestServeGitSyncInterval` | **15** |

tip 运行原文（PASS/FAIL 行取自 `go test -json` 逐 test 事件，非日志转写）：

```
--- FAIL (must be empty) ---            <- 无任何 FAIL
--- SKIPPED ---
  TestBidir2ImportSymmetry
  TestBidir2LiveConflictNeverClobbered
  TestBidir2ServeTickAutoSync
  TestRunClaimLockProtocolYieldRollsBack
  TestSyncDeltaReasonTravels
stamp: ... exit=0 outcome=pass stream=run-1790983169-2923955.json  (81s wall)
```

### 2. 单机模式零回归 —— 结构性证明（首选形式）

`git diff base..tip` 对三通道生产文件**为空**，且 sha256 逐字节相同：

```
base=99074b60  tip=8fb37004621962be2774325713886d103cc1b8fa

--- (1) 三通道生产文件 base..tip ---
  [empty => byte-identical]

--- (1b) sha256 ---
  internal/tools/memory_sync_claim.go       base=cbf57d9a708bf24e tip=cbf57d9a708bf24e IDENTICAL
  internal/tools/memory_sync_session.go     base=d83b938ae5497e77 tip=d83b938ae5497e77 IDENTICAL
  internal/tools/memory_sync_reconcile.go   base=dbd6bfc1756d7cf9 tip=dbd6bfc1756d7cf9 IDENTICAL

--- (2) 整个 memory_sync_* 生产集 base..tip ---
internal/tools/memory_sync_delta.go      <- item4 唯一改动的生产文件
internal/tools/memory_sync_task.go       <- 仅 +1 行
--- (2b) memory_sync_delta.go:  1 file changed, 24 insertions(+), 15 deletions(-)

--- (3) internal/config/ (HasSyncPeers 所在包) ---
  [empty => config 包逐字节未变]

--- (4) HasSyncPeers 门控点调用行 base..tip ---
  [empty => 一行都未改]

--- (5) HasSyncPeers 函数体 base 与 tip 完全相同：
func HasSyncPeers() bool {
	peers, err := ResolveSyncPeers(nil)
	return err == nil && len(peers) > 0
}
```

**为什么三通道的测试对照不必要（结构性论证）**：测试是对**代码**的断言；三通道生产文件在 base..tip 之间逐字节相同（sha256 相等），而通道的测试文件同样未被触碰（`git diff base..master -- internal/tools/memory_sync_claim_test.go …reconcile_cov_test.go == 空`）。同一份生产代码 + 同一份测试代码 ⇒ 测试对照在结构上恒等，不可能给出新信息。

**单机模式的论证更强，是两条独立事实的合取**：

1. `HasSyncPeers()` **函数体逐字节相同**且 `internal/config/` 整包未变 ⇒ 拓扑判定本身语义不变；
2. 全部 **7 处门控调用点**（`cli_tasks.go`、`cli_tasks_sync_git.go`、`cli_tasks_sync_pass.go`、`cli_tasks_adopt.go`、`task_finish_cleanup.go`×2）的**调用行 base..tip 零改动** ⇒ 单机分支的进入条件不变。

即：**判定不变 ∧ 门控不变 ⇒ 单机路径行为不变**。epic 唯一改动的生产文件是 `memory_sync_delta.go`，而该文件位于 `if syncDeltaServerResolve` / 同状态分支内部，单机（`sync_peers=[]`）下 `gopi worker` 的 claim gate 在 `!config.HasSyncPeers()` 处即打印 `single-node: no sync peers declared — local claim only` 并跳过整个 `RunClaimLockProtocol`，**根本不会走到 delta merge**。这条"不经过"是 epi 改动 2/5 自己也声明的边界。

### 3. 单机模式零回归 —— 实测对照（两条，一正一反）

**(a) base vs tip 同一 `-run` 集对照**（acceptance 要求的"首选"形式）：

| run | commit | pass | fail | skip | wall |
|---|---|---|---|---|---|
| **base** | `99074b60` | **187** | **0** | 5 | 17s |
| **tip** | `8fb37004` | **197** | **0** | 5 | 81s |

同一组测试、同一台机、两棵树都 `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` 后跑。差异 = `+10` 个测试全部来自 epic 新增（`TestTaskSetPayload*`、`TestPayloadRevision*`、`TestLiveRowsOwnedByOthers_SuperstringHostPeer`、`TestRollbackLocalClaimPaddedID*`、`TestSyncTaskPushOnlyPaddedID*`、`TestSyncDeltaTwoNodeHandoff`、`TestSyncDeltaDeliverableForwardMigration` 等）；**没有一条既有测试从 pass 变 fail**。这就是"零回归"的实测形态。

**(b) 真实 CLI 在活单机 profile 上**（Rule 31，`sync_peers: []` 本机实测）：

```
$ grep '"sync_peers"' ~/.gopi/settings.json
  "sync_peers": [],
$ ./bin/gopi task diff                  # 不带 --peer
diff: no sync peers declared (sync_peers is empty) — single-node: no second ledger to disagree with
```

### 4. heavy 集补齐（5 条 skip 的 4 条属于三通道）

smoke 路径下规则性跳过（Rule 27 `heavytest.Require` + `disk-fixture` 标准，**可审计**，每条 skip 都带自己的理由），本 run 用**记录的 opt-in** 补跑：

| run | commit | pass | fail |
|---|---|---|---|
| tip heavy | `8fb37004` | 4 | **1**（`TestRunClaimLockProtocolYieldRollsBack`） |
| base heavy | `99074b60` | 4 | **1**（同一条） |

### 5. 唯一 FAIL 的根因定位（既有缺陷，非 epic 回归）

`TestRunClaimLockProtocolYieldRollsBack` 在 **base 与 tip 同样失败** ⇒ 与 epic 无关。根因（5-why；`memory_sync_claim_test.go:411-426`）：

```
memory_sync_claim_test.go:426: exhausted rounds must yield, got proceed
   (task sync OFF (task_sync/GOPI_TASK_SYNC) — local claim only, no remote leg)
```

它 `t.Setenv(configSyncPeersEnv, "gopi-m421-no-such-peer")` 自造了一个 peer，**却没有打开 task-sync 总开关**。`RunClaimLockProtocol` 在 `internal/tools/memory_sync_claim.go:232` 先判 `if !config.TaskSyncEnabled() { return … taskSyncOffClaimReason }`，于是在 peer 解析之前就返回 Off/Proceed。该门控在 **base 的文件里同样存在**（`:232` 与 `:1075`，文件逐字节相同）⇒ 这是 pre-existing 的测试夹具缺口（少了一个 `t.Setenv`），不是本 epic 引入。

---

## Not Done

诚实列出，按严重度排序：

1. **epic 第 3/5 项从未落地 —— 本 run 的终点快照里它不在。**【最重要】行 `01a0fea1-667e-7232-990d-1cdc5cd8f3b6` 状态 `completed`，但它的三个 commit（`958c1b15` / `0ff45614` / `42c5f10b`）**不在 master 上**，分支 `task/01a0fea1667e7232990d1cdc5cd8f3b6` 已被删除，rework 行 `01a0fee7-768d-7050-ae11-cd3612f52aab` 仍 `pending`。实测三处锁具**仍在原样**：
   - `internal/tools/memory_sync_task.go:2213` `harvestSameContent` 仍含 `r.Payload != local.payload`；
   - `internal/tools/memory_sync_task.go:1135` / `:1238` import 侧的 payload work-hash 重号门仍在；
   - `sameWorkIdentity()` 在 tip 上确实存在且被 `:2064`、`memory_sync_claim.go:605/672` 使用，但它的调用点**不属于** baseline 的 payload 门（该函数由更早的 `b23d31d2` 引入），所以第 3 项声称的"拆掉 payload 项"**未发生**。
   - ⇒ 本报告的"三通道未被破坏 / 单机零回归"是对**当前快照**成立的陈述，**不构成"第 3 项已落地且无害"的证明**。epic 第 5 项（本项）**不得**被读作"epic 四项改动全部验证通过"。
   - **已按 Rule 26 派发**：epic 第 3 项的重做 / 落地需要一个独立任务（本 run 的 scope 是取证，不是实现）—— 该工作已由仍处 `pending` 的 rework 行 `01a0fee7-768d-7050-ae11-cd3612f52aab` 承接，其 briefing 已完整描述冲突实质与交付要求（在 master 上重做「身份判定 = task_id」，与已落地的 `RowsDiverged` 语义对齐），**无需重复提交**；本 run 只把它在此点名，避免它被当成"已完成"。
2. **base 对照跑了两次，第一次无效，已如实记录。** 第一版用 `git archive` 解包（无 `.git`），导致 7 条以 `task_submit` 播种的测试在 `repo "." is not a git work tree` 处报错失败（`run-1790983304-2938449.json`，exit=1）。判决：**该方法无效，已废弃**；有效版本把 base 树 `git init` + 提交后再跑（`run-1790983393-2942112.json`，exit=0）。两次都在证据文件里，未删除。
3. **`TestRunClaimLockProtocolYieldRollsBack` 未被修好 —— 已派发窄修复任务 `01a0fef0-29a3-7b78-9fc0-a25c609094d7`**（tags `bug,test-fixture`，priority 1）。本 run 只做到"证明它是 pre-existing 且根因是夹具少一行 `t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)`"——兄弟测试 `TestStartClaimLockBroadcastEmptyPeers` 就在同文件下方 48 行写着这个理由。它仍是 claim-lock 通道里**唯一一条不通过的测试**，因此"claim-lock 全绿"这句话不成立（`4 pass / 1 fail`，base 与 tip 相同）。
4. **没有跑真实跨节点（peerbox↔HP）会话。** 本机 `sync_peers: []` 是单机 profile，没有第二个 peer 可指；多节点腿只由同进程/同机双 profile 的测试覆盖，`make test-budget` 的 `-run` 集里也没有真 ssh 对话。按 Rule 31 这是"我跑不了"的情形，明写在此。
5. **未跑全树套件。** 按 Rule 27 与效率规则，只跑了分支作用域（`./internal/tools ./cmd/agent`）的 smoke 靶向集 + 5 条 heavy。其余包（含 `internal/standards` 的 gates、`cmd/agent` 的 chat/replay 族）无覆盖证据。
6. **未新增任何产品代码或测试。** 本项是取证任务，交付物是证据与文档；唯一新增文件是本报告 + DOC-INDEX 行。
7. **`internal/tools` 里不属于这三条通道的测试族未跑**（merge/worktree/ledger 等）；第 4 项改动的 `RowsDiverged` 报告行只由单元测试覆盖，没有在活的 2 节点 delta 会话上观察过。
8. **`make test-heavy` 没有 `PKGS` 透传。** Makefile 的 `test-heavy` 把 `./...` 写死（与 `test-budget` 不同），因此 `make test-heavy PKGS=...` 会跑全树；本 run 直接调用 `scripts/test-budget.sh` 收窄。这是 Makefile 的一处不一致，未修。

---

## Verification

- **Build**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK（tip 与 base 两棵树各一次）
- **Tests**：
  - `make test-budget PKGS='-run "<43 前缀>" ./internal/tools/ ./cmd/agent/'`
    - tip `8fb37004`：`2 finished, 0 with a failure` — **pass=197 fail=0 skip=5**，`exit=0 outcome=pass`，81s（stream `run-1790983169-2923955.json`）
    - base `99074b60`：`2 finished, 0 with a failure` — **pass=187 fail=0 skip=5**，`exit=0 outcome=pass`，17s（stream `run-1790983393-2942112.json`）
  - `GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 scripts/test-budget.sh -run "<5 heavy>" ./internal/tools/`
    - tip：**pass=4 fail=1**，`exit=1`（stream `run-1790983380-2941580.json`）
    - base：**pass=4 fail=1**（同一测试同一条消息），`exit=1`（stream `run-1790983437-2943438.json`）
- **Ran**（Rule 31，逐字）：
  1. `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
  2. `make test-budget PKGS='-run "^TestClaimLock|TestClaimSSH|…|TestPayloadRevision" ./internal/tools/ ./cmd/agent/'`（tip 与 base 各一次）
  3. `GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 scripts/test-budget.sh -run '^TestBidir2ImportSymmetry|TestBidir2LiveConflictNeverClobbered|TestBidir2ServeTickAutoSync|TestRunClaimLockProtocolYieldRollsBack|TestSyncDeltaReasonTravels' ./internal/tools/`
  4. `git merge --ff-only master`（delivery-point sync；本 run 起点 `3ee92634` → 终点 `8fb37004`）
  5. `git diff --stat 99074b60..HEAD -- internal/tools/memory_sync_claim.go internal/tools/memory_sync_session.go internal/tools/memory_sync_reconcile.go` → 空
  6. `./bin/gopi task diff`（活 profile，`sync_peers: []`）→ `diff: no sync peers declared (sync_peers is empty) — single-node: no second ledger to disagree with`
  7. `./bin/gopi doccheck` → `the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`
  8. `git status --porcelain`（worktree）→ 提交后为空
- **Result**：三通道生产文件 base..tip 逐字节相同（3/3 sha256 相等）；同一 `-run` 集 base 187 pass/0 fail、tip 197 pass/0 fail（无一条既有测试转红）；5 条 heavy 的 4 条通过、第 5 条在 base 与 tip **同样**失败（pre-existing 夹具缺口，根因已定位到 `memory_sync_claim.go:232` 的 `TaskSyncEnabled()` 门在 peer 解析之前返回）；活单机 profile 上 `gopi task diff` 打出单机判定原文。**结论：三通道未被破坏，单机零回归；但 epic 第 3 项从未落地。**
- **Commit**：见本文件所在 commit（`devdoc/payload-unlock-3channel-regression.md` + `devdoc/DOC-INDEX.tsv` 同一 commit，Rule 36）

## Evidence (self-filed)

- `git diff --stat 99074b60..HEAD -- <三通道生产文件>` → **无输出**（空即逐字节相同）。
- sha256 16 前缀：claim `cbf57d9a708bf24e`==`cbf57d9a708bf24e`；session `d83b938ae5497e77`==`d83b938ae5497e77`；reconcile `dbd6bfc1756d7cf9`==`dbd6bfc1756d7cf9`（三条都是 `IDENTICAL`）。
- `git diff --stat 99074b60..HEAD -- internal/config/` → **无输出**；`git diff -U0 … | grep HasSyncPeers` **无输出**（7 处门控调用行零改动）；`HasSyncPeers()` 函数体 base 与 tip 打印结果逐字符相同。
- tip stream `run-1790983169-2923955.json`：逐 test 事件统计 `{'pass': 197, 'skip': 5}`，`fail` 计数为 0；5 条 skip 的 `output` 事件原文均以 `HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): <name>\tdisk-fixture:` 开头。
- base stream `run-1790983393-2942112.json`：`exit=0 outcome=pass`，`{'pass': 187, 'skip': 5}`。
- heavy 两次运行 `exit=1`，`--- failures` 块两次都只列 `--- FAIL: TestRunClaimLockProtocolYieldRollsBack (0.02s)`；tip 侧 output 原文含 `memory_sync_claim_test.go:426: exhausted rounds must yield, got proceed (task sync OFF (task_sync/GOPI_TASK_SYNC) — local claim only, no remote leg)`。
- 无效的第一次 base 对照 `run-1790983304-2938449.json`：7 条 `--- FAIL`，output 原文含 `task_submit: repo "." is not a git work tree`。
- 活 profile：`grep '"sync_peers"' ~/.gopi/settings.json` → `"sync_peers": [],`；`./bin/gopi task diff` 原文见上。
- 证据文件（Rule 37，全部在 session 目录，不入 worktree）：`/home/user/.gopi/sessions/01a0fea1-6821-7135-9690-d7e3183be4e6/evidence/` —
  `structural_proof.txt`、`tip_all_four_items.txt`、`tip_per_test.txt`、`base_99074b60_control.txt`、`base_heavy_channel_tests.txt`、`tip_heavy_channel_tests.txt`、`tally.txt`、`single_node_cli.txt`
- 无 worktree 内 scratch：`git status --porcelain` 提交后为空（Rule 30）。
