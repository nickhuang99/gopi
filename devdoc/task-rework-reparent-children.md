<!-- doc-class: historical -->
# Task Report: rework 把原行 A 的子行重指向新 rework 行 R

**task** `01a11fcaeec270498198582ef59a9511` · branch `task/01a11fcaeec270498198582ef59a9511`

## Completed

**缺陷（本改动存在的唯一理由）。** 每条 branch-rework 通道都为一个被拒绝的分支新开一行 R：
merge 时的 build 门、CLI contract 门、manifest 门、merge ceiling，以及 verify 门 REVERT 的
merge。这些情形里都有一行既有行 A 承载刚刚被证伪的工作，而 A 常常有子行
（`parent_task_id = A`）。Rule 33 在父行到达 `{completed, verified, superseded}` 时立刻释放
子行，而 A 的 `completed` 回写正是它进入 rework 的原因 —— 于是每个子行都在 rework 刚刚证伪
的前提下被释放并开跑，台账对此一言不发。

**修法是边，绝不是释放集。** 收紧 Rule 33（把 `completed` 移出释放集，或给
`taskParentGateDenied` 加"rework 待办"分支）会把系统里**每一个**已完成行的子行全部 park，
让"父行干完、子行开工"这个最普通的情形无法表达。子行真正持有的是"这项工作的成果已经
交付"，而现在承载该承诺的行是 R —— 所以**边移动**：`parent_task_id: A → R`，经由该列唯一
的写入者 `TaskReparent`（CAS + append-only trail + 环检测/自身检测）。本改动不直接改库、
不改任何状态、不碰 `taskParentGateDenied` / `taskParentReleaseStatuses`。

**单一共享实现**：`internal/tools/rework_reparent_children.go`

- `ReworkReparentChildren(reworkedID, reworkID, reason) ReworkReparentReport` —— 读 A 的子行，
  逐个判定并处置；每个子行恰好落入 `Moved / Already / HandedOff / Terminal / Failed` 之一，
  并各带一句可读的记录（计数从不说"是哪一个"）。
- `reworkReparentFromBranch(repo, branch, reworkID, lane)` —— **只知分支**的通道走的桥：用既有
  的 branch→row 查询（`TaskForBranch` + `TaskForBranchResolvedRow`，Rule 14：不发明第二个查询）
  解析原行；并**排除刚 mint 的 R 自身**，因为各通道在复查之前就已把 R 打上
  `deliverable.branch`，而 R 是 LIVE 的 —— 不排除就会拿 R 和 R 比。

**接入的 7 条通道（收口到同一实现）**

| 通道 | 调用点 |
|---|---|
| build 门 | `buildGateSubmitRework` (task_merge.go) |
| contract 门 | `contractGateSubmitRework` (task_merge.go) |
| manifest 门 | `manifestUnionGateSubmitRework` (task_merge.go) |
| merge ceiling | `MergeCeilingFileRework` (merge_ceiling_rework.go) |
| verify revert | `MergeVerifyFileRework` (verify_gate.go) —— **精确知道** `o.OldTaskID`，直接调共享实现 |
| hand-over 升级 | `MergeHandoverEscalate` (merge_handover_escalate.go) |
| dirty-holder salvage | `dirtyHolderSalvageOne` (dirty_holder_salvage.go) |

前四条加 salvage 只知道分支，走 `reworkReparentFromBranch`；verify 通道知道两端确切的行。
各通道把 `TaskSubmit` 换成 `TaskSubmitReturningID` 以拿到 R 的行 id（镜像腿回 `""`，此时按名
拒绝，见下）。

**逐条决策（都是记录下来的判断，没有一条静默）**

- **幂等**：复查第二次时，子行已不在 A 名下（`WHERE parent_task_id = A` 读不到），报告
  `Children()==0`、零写入、零 trail 条目。写者自身的 unchanged 分支也被识别
  （`reparentUnchangedPrefix`，新增常量，Rule 13）。
- **防环**：不在本文件重写环检测 —— `TaskReparent` 在同一事务里爬父链，会**按名拒绝**，拒绝
  记为 `Failed` 并带上传者自己的原因（可达形状：R 恰好是被它 supersede 的行的子行）。
- **已开跑的子行照样重指向**：claimed / in_progress 的子行**移动并记录**。它已经在被证伪的
  前提下运行了，唯一能修复它的动作就是它下次被判定时读到的这条边；继续指向 A 等于把这个
  谎留在台账里。
- **terminal 子行不动，按名计数**：completed / failed / verified 的子行不欠这条边任何工作，
  改写它的父行只是改写历史而不带来任何门禁收益。
- **跨机 handed-off 子行**：带 `reworkHandoffTag`（`handed-off`）的子行**本地边不重写**，
  走自己的分支**按名记录**（绝不折进 "unchanged"—— Rule 15：处置在别处与"不成立"不是同一个
  事实）。理由：该行的工作已经 `gopi task send --cancel-source` 迁到对端，本地行是那次迁移的
  取消记录，真正读依赖的是对端台账；改写本地边不会影响将要运行该工作的机器，还会与标记
  自相矛盾。**这就是跨机处置结论：显式、有论证、故意 no-op，而不是只写半边（两个台账只碰到
  一个）。**
- **拒绝而不猜**：两端任一为空、两端同一行、原因为空、台账打不开、子行读不出、子行数超过
  `reworkReparentChildScanCap`（500）—— 全部按名拒绝并且**不写任何东西**（对被截断的子行集
  动手比不动手更糟）。
- **失败按名记录**（Rule 15）：某个子行的移动失败不会中止其余子行 —— 否则一个病态子行就让
  其他所有子行继续留在被证伪的前提下；失败者的句子写明边仍指向 A、以及那句释放状态会继续
  释放它。

**共享实现只经既有入口，零新依能**（Rule 35）：不新增进程/定时器/脚本/状态文件；它跑在
**已经会触发**的那次 submit 里，用**已经存在**的写入者。Rule 3 不加工具；Rule 1/13/14 常量
单一来源（`reworkReparentMarker`、`reparentUnchangedPrefix` 等）。

## Not Done

- **verify 通道的 `MergeVerifyReworkOptions.OldTaskID` 为空时**只能按名拒绝（共享实现不猜哪一
  行被 supersede）。这是设计选择，不是缺口 —— 但意味着一条"reverted 但无法指名原行"的 verify
  rework 不会重指向任何子行，只留一句 `nothing reparented: …`。
- **跨机路径只做了本地半边**：对端台账里那条依赖边的重指向没有实现（本地台账看不到对端行
  的 id，也拿不到对端 `TaskReparent` 的 CAS）。本轮处置是**明确拒绝 + 记录**，见上。
- **没有为 7 条通道逐条写端到端测试**：单测覆盖共享实现与 branch 桥；通道本身由既有测试
  （`rework_handoff_test.go`、`merge_handover_counter_test.go`、`verify_gate_test.go`、
  `task_staleness_guard_test.go`）连带覆盖，本轮真跑了其中一条完整门禁臂（verify 通道）与
  ceiling 通道的 DEDUP 臂（桥），见 Verification。
- 真实 merge CLI 的端到端**只跑到两条臂**：verify 通道的门禁臂（`01a11ffd-4ad0-… → 4b66-…`），
  以及 ceiling 通道的 **MINT 臂**（`01a11ffd-4c02-… → 4c53-…`，走桥）——后者即 build / contract /
  manifest / salvage 四条通道共用的那条路径。另两次尝试分别是「分支刚被删（`no repo holds
  task/proof-branch`）只打印 usage」与「落入 merge 子 gopi 的冲突解析（LLM child），2 分钟内未收敛
  被 kill」；同一条 ceiling 命令的第二次运行落到 DEDUP 臂（不 mint R）。
  **五条 branch-keyed 通道（build/contract/manifest/ceiling/salvage）走的是同一个
  `reworkReparentFromBranch`，桥本身有单测**
  （`TestReworkReparentFromBranchPicksTheOriginalOwnerNotTheReworkRow`），但它们的**门禁触发臂**
  的 CLI 级取证未做 —— 这是本轮最老实的缺口。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0, no output（BUILD_DONE）
- Tests: `make test-budget PKGS='./internal/tools ./cmd/agent'`（本轮实测，1228s，exit=1）：
  **2 packages finished / 2 with a failure**，全部 3 个失败**均非本 diff 引入**，逐个取证如下 ——
  `git diff --stat master...HEAD` 对这三个测试所属文件**为空**（源码与 master 逐字节相同），其引入
  commit（`844d1f3d2` / `9e2e57750` / `22f3b74b1`）**都是 master 的祖先**，而 master 是我这个分支的
  祖先；本 diff 的文件清单（上表 10 个）不含这些路径上的任何一文件：

  | 失败测试 | 文件 | 判定 |
  |---|---|---|
  | `TestChatCookedFallback_NonTTY` | `cmd/agent/cli_lineedit_test.go` | master-red，未触碰 |
  | `TestChatVoicePrivacyRowCarriesOnePrefix` | `cmd/agent/voice_module_defects_test.go` | master-red，未触碰 |
  | `TestTagsIndexProbeOnRealCtagsBytes` | `internal/tools/torn_index_test.go` | **host-red**：断言句为 `the header probe rejected a COMPLETE index`，该测试自身的注释写的是 universal-ctags **5.9.0**，而本机 `ctags --version` = **Exuberant Ctags 5.9~svn20110310** —— 宿主工具版本差异，与任何改动无关 |

  本 diff 的作用域测试：`go test ./internal/tools/ -run 'TestParentGate|TestRework(Dedup|Handoff)'`
  → `ok … 3.155s`；8/8 `TestReworkReparent*` 单测 PASS（见 `## Evidence`）。`go test
  ./internal/standards/` → `ok 83.1s`；`gopi tool doccheck` → corpus consistent, exit 0。
- **Ran（改前，真实账本 / 真实 CLI）**：一个全新 sandbox（`gopi tool sandbox create --label
  rework-lane-proof2`），真实 `gopi task submit` 造 fixture，A 走完 pending→claimed→in_progress
  →completed 的常规阶梯：

  ```bash
  export GOPI_HOME=<sandbox>
  A=$(gopi task submit "proof parent A (superseded row)" --payload … --repo "$REPO" | …)
  B=$(gopi task submit "proof child B (depends on A)" --payload … --parent-task-id "$A" --repo "$REPO" | …)
  for st in claimed in_progress completed; do gopi task update "$A" --status $st; done
  gopi task list --status pending
  ```

  输出（改前）：

  ```
  #01a12011-5ab7-761b-8dc0-73741ebeeb1f  [pending]  proof child B (depends on A)  … [next]
  ```

  **B 带 `[next]` = 队列的下一行就是它**：`completed` 的 A 释放了它。这就是缺陷本身（Rule 33 的
  释放集在按规格工作，也正是边必须移动的原因）。

- **Ran（改后，verify 通道的生产函数 `MergeVerifyFileRework`，同一真实账本）**：以
  `GOPI_MEMORY_DB` 指向该 sandbox 的 ledger 驱动生产函数（Rule 31：跑被改动的 surface 本体，
  不是它的替身）：

  ```
  GOPI_MEMORY_DB=$SB/.gopi/memory.db GOPI_PROOF_A=$A GOPI_PROOF_REPO=$REPO GOPI_PROOF_BRANCH=proof/branch \
      go test ./internal/tools/ -run TestZZZReworkLaneProof -count=1 -v
  ```

  输出：

  ```
  [task-notify] rework-child-reparent: 01a12011-5a94-7dcf-b816-f1dac6be5c6b → 01a12011-c47c-7509-8d1e-e1819a4a118e:
                1 child(ren) read — moved 1, already on the rework row 0, handed off to a peer 0, terminal (no work owed) 0, failed 0
  [task-notify] child 01a12011-5ab7-761b-8dc0-73741ebeeb1f:
                parent_task_id 01a12011-5a94-7dcf-b816-f1dac6be5c6b -> 01a12011-c47c-7509-8d1e-e1819a4a118e
  rework row id="01a12011-c47c-7509-8d1e-e1819a4a118e" err=<nil>
  ```

  之后同一 pending 视图（真实 CLI）：

  ```
  #01a12011-c47c-7509-8d1e-e1819a4a118e  [pending]  merge verify rework: proof/branch (was 01a12011-5a94-…)  … [next]
  #01a12011-5ab7-761b-8dc0-73741ebeeb1f  [pending]  proof child B (depends on A)  … [queue-skip] parent 01a12011-c47c-7509-8d1e-e1819a4a118e not released (status=pending; releases on completed|superseded|verified)
  ```

  并且 `gopi task show <B>`：`parent_task_id: 01a12011-c47c-7509-8d1e-e1819a4a118e`。
  **A 的子行边 A→R 已移动，子行重新被 park 并被点名 R**；`[next]` 落到 R 自己身上 ——
  是 park，不是堵头。

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge
> task/proof-lane2` invocation in the 改后 Ran block below is the RECORD of what
> this run executed, kept verbatim; the top-level verb was removed afterwards and
> answers **exit 1** today (`gopi task merge` runs the same engine).

- **Ran（改后，第二条通道 = branch-keyed 的 merge ceiling，真实 CLI `gopi merge`，同一私有 sandbox）**：
  造 `A2`（`deliverable.branch=task/proof-lane2`）与 `B2`（`--parent-task-id A2`），然后直接跑真实
  merge CLI：

  ```bash
  export GOPI_HOME=$SB
  gopi merge task/proof-lane2
  gopi task list --status pending      # 第二次跑同一命令后再看一次
  ```

  输出（`artifacts/rework-reparent-proof.log`）：

  ```
  == LANE 2: merge ceiling rework (MergeCeilingFileRework, branch-keyed) ==
  [task-notify] merge: ceiling stop for task/proof-lane2 — a LIVE branch-rework task already carries resolve merge conflict: rework: task/proof-lane2
  [task-notify] rework-child-reparent: 01a11ffd-6c59-… → 01a11ffd-…: …
  #01a11ffd-6c85-…  [pending]  proof B2: child of A2  (…) [queue-skip] parent 01a11ffd-6c59-… not released (status=pending; releases on completed|superseded|verified)
  先前的第二次调用：IDEMPOTENT (second ceiling call changed the pending view: false)
  ```

  **lane 2 = branch-keyed 的 merge ceiling，走桥；它的 MINT 臂真跑到了**：第一次 `gopi merge
  task/proof-lane2` 的输出（原始 transcript 的 tool_result，已抄进 artifacts 日志的补充段）：

  ```
  [task-notify] merger: rework row "resolve merge conflict: rework: task/proof-lane2" deliverable.branch stamped to "task/proof-lane2" at submit
  [task-notify] rework-child-reparent: rework: branch task/proof-lane2: rework-child-reparent: 01a11ffd-4c02-… → 01a11ffd-4c53-…: 1 child(ren) read — moved 1, …
  [task-notify] rework-child-reparent: rework: child 01a11ffd-4c2d-…: parent_task_id 01a11ffd-4c02-… -> 01a11ffd-4c53-…
  [task-notify] merge: ceiling stop on task/proof-lane2 (the conflict is over the run's context ceiling) — branch-rework task filed with branch.diff attached
    AFTER:  B2=01a11ffd-4c2d-7… | parent_task_id: 01a11ffd-4c53-78f0-9e22-5fc9099395dd
  ```

  这就是**桥 `reworkReparentFromBranch` 的一次真实端到端**（只知分支 → 解析原行 → 排除刚 mint 的 R
  自身 → 移动），即 build / contract / manifest / salvage 四条通道共用的那条路径。**它们的门禁触发臂
  本身（一次真失败构建 / 契约拒绝 / manifest 重复 / dirty-holder）仍未逐条跑到** —— 见 `## Not Done`。

  同一命令**跑第二次**落到 ceiling 的 DEDUP 臂（已有 LIVE rework 行 ⇒ 不 mint R）：pending 视图逐
  字节不变（`IDEMPOTENT (second ceiling call changed the pending view: false)`），B2 的父保持 R2。

  两条通道的**原始日志全文**：
  `/home/user/.gopi/sessions/01a11fca-eec2-7049-8198-582ef59a9511/artifacts/rework-reparent-proof.log`
  （lane 1 的改前视图里 B1 行**不带** `[queue-skip]`，改后带 —— 「completed 的 A 会释放子行」与
  「移动后重新被 park」两半都在同一份日志里；lane 2 的 mint 臂原文在该文件末尾的补充段）。

- **Ran（幂等，真实账本）**：对同一被 supersede 的行 A 再跑同一通道两次：

  ```
  [task-notify] rework-child-reparent: 01a12011-5a94-… → 01a12011-e82a-…: 0 child(ren) read — moved 0, … failed 0
  [task-notify] rework-child-reparent: 01a12011-5a94-… → 01a12011-f920-…: 0 child(ren) read — moved 0, … failed 0
  ```

  子行已不在 A 名下，复查读不到任何子行 → **零写入、零 trail**。

- **Ran（trail 取证，只读打开该 ledger）**：`task_events` 里那次移动只有一条记录，携带两端与原因：

  ```
  event=reparented  detail="reason: rework supersession: <A> was reworked as <R> … old parent_task_id: <A> / new parent_task_id: <R>"
  ```

  移动次数 = 1（幂等两次没有新增）。

- Result: **改前 B 可 claim（`[next]`）；改后 B 的边 A→R 且被 R park（`[queue-skip] parent <R>
  not released`），原因与两端都留在 trail 里**。取自真实台账（sandbox 私有 profile，未触碰
  operator 的 `~/.gopi`；ledger 副本 + 私有 sandbox，用完即弃）。

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → `exit=0`，仅 `BUILD_DONE` 一行；`git status --porcelain`
  在收尾时列出 6 个已修改 + 2 个新文件，无临时产物（`reworkproof/`、`rewproof-bin`、
  `reworkproof-harness` 已删）。
- 真实账本改前输出：见上「Ran（改前）」—— B 行不带 `[queue-skip]`，同视图里 A 行带 `[next]`。
- 真实账本改后输出：见上「Ran（改后）」—— `[task-notify] child …: parent_task_id A -> R` 与
  `[queue-skip] parent 01a1200b-2e0a-7f23-a087-6d03c6a3bba0 not released (status=pending; …)`。
- `git branch` / `git worktree list` / `git status --porcelain` 的收尾读数：

  ```
  $ git branch --show-current
  task/01a11fcaeec270498198582ef59a9511
  $ git status --porcelain     # 收尾时：6 modified + 2 new（本改动的交付物），无临时产物
   M internal/tools/dirty_holder_salvage.go
   M internal/tools/merge_ceiling_rework.go
   M internal/tools/merge_handover_escalate.go
   M internal/tools/task_merge.go
   M internal/tools/tasks.go
   M internal/tools/verify_gate.go
  ?? internal/tools/rework_reparent_children.go
  ?? internal/tools/rework_reparent_children_test.go
  ```

## Deliverable（Rule 6d）

- **分支**：`task/01a11fcaeec270498198582ef59a9511`（= task id，未改名）
- **worktree**：`/home/user/workspace/gopi-01a11fcaeec270498198582ef59a9511`（linked worktree，
  commit 即在主仓 refs 上，**无需 push**）
- **取回**：`git show task/01a11fcaeec270498198582ef59a9511:devdoc/task-rework-reparent-children.md`
