<!-- doc-class: roadmap -->
# worktree-removal-census — 分级清单、依赖图、分期计划

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> 目标：gopi 的 **ground** 只有一种形态 —— 声明式 `dev_repo` 的 **CLONE**（clone 通道）。worktree 作为**任务 ground** 的通道退役。
> 本文是分批删除的地图：哪些必须先留（还能用、或者别的东西踩在它上面）、哪些现在就能删、哪些是被误以为"还在用"其实只剩测试在引用。
> 测量时间：2026-10-27，HEAD `ecce621`（master），本机 clone ground `/home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77`。
> 计数是**这一版树**的实测量，不是估算；每一条都能用文中给出的命令重取（Rule 38）。

## 0. 为什么会有这份清单

`worktree` 在 gopi 里不是一件东西，是**五件同名不同命的东西**：

| # | 它是什么 | 该不该留 |
|---|---|---|
| ① | **任务 ground 的创建通道** —— runner 侧 `taskGroundCreate` 执行 `git worktree add -b task/<id>`，作为**未声明 `dev_repo` 的宿主的兜底 ground** | **留 runner 侧；删模型侧配方**（见 §1 的翻车记录） |
| ② | **任务 ground 的清理通道** —— finish-cleanup / reclaim / sweep / merged-cleanup 里对 ① 造出的目录做 `worktree remove / prune / unlock` | **按实测收敛，不整体删** |
| ③ | **不写进仓库的私有检出** —— 验证器（`taskVerifyGround`）、merge build gate、测试夹具、clone 通道自己的临时检出 | **留**：它们用的 `git worktree add` 与 ground 通道无关 |
| ④ | **引用/派发的记账** —— `deliverable.worktree` / `deliverable.branch` 字段、`gopi task for-branch`、worktree↔row 归属归属判定 | **留字段，去掉"路径必然存在"的假设** |
| ⑤ | **chat 自己的 ground** —— 已随 epic 删除（`devdoc/chat-session-ground-lifecycle-diagnosis.md`） | 已删 |

**这份清单存在的原因**：把 ①②③④⑤ 混成一句"删掉 worktree"，会把 ③ 的验证器和 ④ 的记账一起带走 —— 那是 Rule 43 的翻车形状（删机制时把机制外的通用工具一起删掉）。

## 1. 批一：一次被实测推翻的判断，和它留下的真实交付

**这一节必须先写翻车，因为翻车的形状比交付更有价值。**

### 1.0 被推翻的判断（2026-10-27，同一天内）

**我当时的判断**：census 读出「207 个 clone / 23 个 linked」，加上本机 `git worktree list` 几乎不注册任何 ground，于是结论"linked-worktree 通道已死"，据此把 `TaskRunGroundFor` 的**创建臂**改成拒绝，并删掉 `taskGroundCreate` / `taskGroundCreateFrom`。

**推翻它的实测**（`make test-budget PKGS='./internal/tools ./internal/devworkspace ./cmd/agent ./internal/agent'`，`exit=1`）：

```
$ grep "^  --- FAIL" test-budget.log | grep -v REFUSED | wc -l
15                 # 注意：这只是日志里 "  --- FAIL" 的缩进形式（13 条测试 + 2 条包级记录）
                   # 精确归属要用两次运行的 stream 做差，见下面 §1.0.1
$ grep "^  --- FAIL" test-budget.log | grep -v REFUSED
  --- FAIL: TestBriefingLastClauseReachesTheChildsFirstRequest
  --- FAIL: TestCLIPositionalSubmitRunsThroughDispatcherE2E
  --- FAIL: TestChatReplayDumpCatalogueIsExhaustive
  --- FAIL: TestChatResumeDoesNotInflateTheSessionEndIterations
  --- FAIL: TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio
  --- FAIL: TestGoldenCLILogChat
  --- FAIL: TestGoldenCLILogSession
  --- FAIL: TestManualRunKeepsWorktreeHeldByForeignProcess
```

失败的**原因**就是我自己写进拒绝里的字符串 —— 每个 e2e walk 都建一个**没有 `dev_repo`** 的 repo，然后期待仍然拿到 ground。它们是 `cmd/agent/task_ground_e2e_test.go:60`、`task_ground_announcement_e2e_test.go:64`、`cli_tasks_recycle_e2e_test.go:135` 这些真实走查。

**根因（5-why）**：为什么我以为通道死了 → 因为本机 207/230 是 clone → 为什么本机是这样 → 因为本机 `settings.json` 声明了 `dev_repo`（peer1）→ 所以 linked 形态在本机**很少被走到** → **但"本机很少走到" ≠ "不可达"**：它是**没声明 `dev_repo` 的宿主（新装的默认状态）的唯一 ground 通道**。我把"我这个视图里没有"当成了"世界上没有"（`GOPI.md` §4.9 记的同一条错误，这是它的第三次复发）。

**处理**：`git checkout --` 回退所有 `internal/tools/*` 与 `internal/agent/*` 的改动，创建臂原样保留。**没有一行错误改动进 master。**

### 1.1 批一真正的交付（保留部分）

| 改动 | 文件 | 为什么它是对的 |
|---|---|---|
| **clone 通道开着时不再无条件调用 worktree resolver** | `cmd/agent/cli_tasks.go:2731` | 原来 `TaskRunGroundFor` 被**无条件调用**，答案在下一行被 clone 通道覆盖 —— 于是每个 clone-channel run 都白跑一次 resolver，其创建臂执行的 `git worktree add` 目录下一行就被丢弃（实测：日志写 `CREATED the ground /home/user/workspace/gopi-01a13207597b7bb6aa37a28db49744d4`，那个目录已不存在）。现在 `if cloneRun == nil { ground, groundErr = tools.TaskRunGroundFor(target) }`：两条通道**每宿主只用一条**，没有谁为两条付费 |
| **模型侧创建配方删除** | `PROJECT.md` Rule 6a / Rule 23 / Rule 29 / Commit Path 步① | 这才是"一个任务产出两个 worktree"的源头：runner 已经造好 ground 并把模型放进去，而 Rule 6a 又命令模型 `git worktree add` 造第二个，那个第二个没有任何 sweep 回收。**runner 造（保留），模型不造（删除）** —— 这两半的判据完全不同，batch 1 的第一版把它们当成一件事，是这次翻车的一半 |
| 文档改为陈述**两种形态** | `PROJECT.md` Rule 6a / 6c | 声明 `dev_repo` → CLONE；未声明 → LINKED WORKTREE。6c 的"你会遇到哪种形态"从"必然是 CLONE（linked 已退役）"改回"由 `GroundModelOf` 实测决定"，并保留原有的两种形态各自正确的交付动作 |
| 钉住新事实的测试 | `cmd/agent/task_ground_gopimd_clause_test.go` arm ③ | 原 arm 钉的是旧配方逐字存在；现在钉**新事实**：配方必须不在（两条 gone 串），且**必须正面陈述** `a run never creates a worktree` / `clone` / `linked worktree` / `dev_repo` —— 只断言"删掉了"是半个测试，下一任读者会自己发明回来 |
| 这条翻车本身 | 本文件 §1.0、`PROJECT.md` Rule 6a 的 "Why the retired-channel claim was WRONG" | Rule 41：发现异常即取证即记。推翻我更早结论的那批红测试，是这条记录的唯一来源 |

**§1.0.1 精确归属：41 条，不是 51 条 —— 而且我第一次报的数字是错的。**

我第一次凭 `grep -c '^  --- FAIL'` 数出 15，然后在提交消息里写成"51 条红"。两者都不对。正确的做法是拿**两次运行的 stream 做集合差**，因为决定性的是"哪条红随回退消失"，不是任何一次运行的计数：

```python
# 两次 run 的 raw json（run1 = 带被回退的删除；run2 = 回退 + gating 之后）
r1 = fails("run-1793062893-3323850.json")   # 56 条 unique
r2 = fails("run-1793064505-237032.json")    # 15 条 unique
r1 - r2   # 41 条：只在我删除期间红 → 归因于我的删除
r2 - r1   #  0 条：我的提交没有引入任何新红
r1 & r2   # 15 条：两次都红 → 预存红，与我无关
```

实测输出：

```
run1 unique: 56   run2 unique: 15
RED ONLY IN RUN1 (= caused by the reverted deletion): 41
  by pkg: {'agent': 41}
RED ONLY IN RUN2 (new after my commit): 0
RED IN BOTH (thus not mine): 15
```

**这 15 条预存红（12 条测试 + 3 条包级/审计记录），与 ground 通道无关，是树的既有状态：**

| 红 | 实测原因 | 引入者（不是我） |
|---|---|---|
| `TestGoldenCLILogChat` / `TestGoldenCLILogSession` / `TestStatsCLIOverFixture` / `TestStatsCLIFallbackOverFixture` | golden 文本漂移：代码现在印 `the chat index holds no row for this session, so no turn count was recorded for it (index the logs with `gopi chat backfill`)`，golden 里还是旧的 `no ledger reader installed in this process`（`internal/tools/chat_ledger_turn_count.go:50`） | `7734b63 feat(session): 一个 turn 数，四个面 —— log/stats/bar 也印 ledger 的 counts` |
| `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` | `devdoc/worker-control-sigint-attribution.md:138` 声称 `signalrec.Install` 在 `main.go:666`，实测在 `cmd/agent/main.go:673` | 同批源码移动 |
| `TestTaskVerbReopenCarriesARepo` / `TestTaskVerbReopenCarriesTheParentsAttachments` | 夹具行已有 live successor `<task-id>-r1`，reopen 按设计拒绝（第二次 successor 会把同一 payload 排两遍） | 夹具未跟上 reopen 的新守卫 |
| `TestWorkerControlSelector*`（2 条） | `/dev/shm/suite-cmd-agent-run-<pid>/suite-ledger/memory.db is not a distilled knowledge base (missing distilled_qa table)` —— 共享夹具库缺表 | 共享 sample 与 selector 的期望不一致 |
| `TestAssertionTotalityAuditGate` / `TestStylegateRatchetSuiteIsGreenOnThisTree` | `internal/tools` 的既有 gate 红（本会话第一轮之前就在） | 既有 |

**为什么要把这一段写进 census**：我第一次得出的"51"是一个**没有生成器、无法复算**的数字（Rule 38 正是禁止这种数）。正确的数字只能由"两次 stream 差集"产生，所以我把那个 python 片段和它的原始输出都留在这里，下一任读者能自己重算。

**§1.0 的反面结论同样要记住**：不要因为这次翻车就把 epic 反过来 —— **模型侧配方确实该删**。判据是 `r2 - r1 = 0 条新红`：删掉模型侧配方、加上调用点 gate 之后，没有任何一条新红出现（见 §1.2）。被推翻的只是"runner 侧创建通道可以删"。

### 1.2 验证（批一交付的实测输出）

```
$ go build ./...                                   # exit=0
$ go test ./cmd/agent/ -run 'TestGOPIMDRule6a' -count=1
ok  	github.com/the operator/gopi/cmd/agent	4.052s
$ go test ./internal/standards/ -run 'TestBranchForceGate' -count=1
ok  	github.com/the operator/gopi/internal/standards	0.962s
```

以及回退后的那次全量（`PKGS='./internal/tools ./internal/devworkspace ./cmd/agent ./internal/agent'`，`elapsed=988s`，`exit=1 outcome=fail`）：**与回退前那次做差集**，`r1 - r2 = 41` 条红随回退消失、`r2 - r1 = 0` 条新红、`r1 ∩ r2 = 15` 条预存红（见 §1.0.1）。`internal/tools` 与 `internal/devworkspace` 两次都是那 2 条既有 gate 红，无新增。

### 1.3 第三次全量：rebase 之后的重跑，`r3 - r2 = 0`

前两次 run 的基线是 `ecce621`。本 run 的 ground 是 clone 通道的一次 `git clone`，它切出来之后上游 master 又走了 **16 个提交**（`ecce621..485254e`），其中**改到了同一个文件** `cmd/agent/cli_tasks.go`（`a28bedb` 加了 wrapper keepalive，+21 行）。所以提交前先把分支 rebase 到 `485254e`，再跑第三次：

```bash
cd /home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77
git rebase origin/master        # Successfully rebased; master -> 23c1d1c
make test-budget PKGS='./cmd/agent ./internal/tools'
# elapsed=710s exit=1 outcome=fail  stream=run-1793065569-841321.json
```

三次 stream 的集合差（同一段 python，只多喂一个文件）：

```
r1 (带被回退的删除)          54 unique failing tests
r2 (回退后, 基线 ecce621)    13
r3 (rebase 到 485254e 后)     9

r1 - r2 = 41   r2 - r1 = 0   r1 & r2 = 13
r3 - r2 =  0   r2 - r3 = 4   r2 & r3 = 9
```

**`r3 - r2 = 0`：rebase 到新 master 之后，本分支没有引入任何一条新红。** 这是批一「不改坏东西」的判据，比任何一次运行的绝对值都硬 —— 因为它是差集，不是计数。

`r2 - r3 = 4` 的四条红**不是被我修掉的，是上游在那 16 个提交里修掉的**，这里记下来免得下一个人以为它们凭空好了：

| 红 | 消失的原因（提交） |
|---|---|
| `TestAssertionTotalityAuditGate` | `59d5014 fix(stylegate): three sites go %w, and a dead status field goes with its silent failure` 及其配套的 totality 分类 |
| `TestStylegateRatchetSuiteIsGreenOnThisTree` | 同上：`worktree_sweep.go` 那个 `%v` 站点改 `%w`，`internal/devworkspace/devworkspace.go:835` 的忽略错误补了理由 |
| `TestTaskVerbReopenCarriesARepo` / `TestTaskVerbReopenCarriesTheParentsAttachments` | `d2cf823 test(cmd/agent): 两个 reopen CLI 测试的 fixture 遵守 live-successor 门` |

剩下的 9 条红是树上的既有状态，与本 epic 无关（明细见 §1.0.1 的表；其中 4 条是 golden 文本漂移、1 条文档行号漂移、2 条共享 sample 缺 `distilled_qa` 表、2 条 chat 回放/计数断言）。

**§1.3 的教训（写给下一次做同一个决定的人）**：clone ground 从 `git clone` 那一刻起就在老化，**每次提交前都要先 `git rebase origin/master` 再跑全量**，否则跑出来的红里混着别人的修复，`r - r` 的差集才是唯一能归因的东西。

## 2. 分级：现在就能删 / 必须留 / 需要先测量

### A 级 — 可由机器判定为死代码，现在就能删

| 目标 | 判定命令 | 实测结果 |
|---|---|---|
| `mergeBuildGateScratchWorktree`（原 `internal/tools/merge_build_gate_scratch_worktree.go:89`） | `grep -rn 'mergeBuildGateScratchWorktree' --include=*.go . \| grep -v '_test.go:'` | **生产调用者为零，但它不是死代码 —— 它是放错文件的测试助手，已改名搬走（批二已交付）**。判定依据是 Rule 43 的三步：① 生产引用数 = 0；② 总引用数 = 6，全部落在 `_test.go`（`merge_build_gate_scratch_worktree_test.go` 3 处 + `rebuild_vet_mergeset_test.go:208` 1 处 + 定义自引用 2 处）；③ 那 6 处里 `mgToolchain` 是**真的在用**它（`mergeset_stylegate_test.go:421`、`rebuild_vet_mergeset_test.go:111/114/150/585`，共 5 个调用点），而这个助手正是修掉「`/dev/shm` 注册泄漏」那个已实测缺陷的释放梯子（clear lock → remove --force → RemoveAll → prune → verify）。**"生产调用者为零"只证明它不是生产代码，不证明它能删。** 处理：`git mv` 到 `internal/tools/merge_build_gate_scratch_worktree_helpers_test.go`（纯改名，零字节改动），于是它按 Go 的构建规则只进测试二进制，生产二进制不再背这 169 行 |
| `taskGroundCreate` 的两处**注释**提及（`internal/tools/task_ground_cutpoint.go:124`、`cmd/agent/cli_tasks_sync_peers_bare_test.go:1286`、`internal/tools/task_ground_cutpoint_test.go:63`） | 同上 | 命中在注释里，不是引用（Rule 43 同音陷阱）。**不需要动**，但如果那几段注释还在讲"创建会执行 worktree add"，措辞要改，否则读者会以为通道还在 |
| `internal/tools/task_ground_baseline.go` + `_test.go`（516 行） | `git grep -n 'taskGroundBaseline' -- '*.go'` \| grep -v '_test.go' | **必须留，不是删除项**。批一的第一版把它 staged 删除了，实测证明它是活的：`internal/tools/task_run_ground.go:381` 用 `taskGroundBaselineNoteSeparator` 把 base 说明拼进创建细节，而 `task_ground_baseline.go` 的 `taskGroundBaselineAlignedFmt` / `StaleNoteFmt` 就是这个说明的两条来源（"基线从 peer 的 tip 切" vs "peer 取不到，从本地切"）。只要 runner 侧的创建臂还在（§1.0 已证它必须在），这份 baseline 判定就还在被调用 |

### B 级 — 必须留：删了会带走与 ground 通道无关的能力

| 保留项 | 位置 | 为什么留 |
|---|---|---|
| 验证器私有检出 | `internal/tools/tasks.go:9641 taskVerifyGround` | 交付的 worktree 消失时，验证器需要一份 `--detach` 的私有检出；它**创建并释放自己**，不注册成任何 row 的 ground。删了 verify 就没有可判的树 |
| merge build gate 的 scratch 检出 | `internal/tools/merge_build_gate.go` + `merge_build_gate_scratch_worktree.go` 的**shape**（不是死的那个函数） | 合并后要在隔离树里重建，这与 ground 通道无关；`devdoc/shm-worktree-risk-assessment.md` 记的 `/dev/shm` 注册泄漏来自这里 |
| `git worktree list/prune` 的**读** | `task_finish_cleanup.go`、`task_reclaim.go`、`worktree_sweep.go`、`task_deliverable_worktree.go`、`task_sync_git.go` | 这些读的是**历史残留**：本机仍有 15 条注册 + 8 条悬空目录（批一取证）。读的腿在残留清零之前不能删，否则残留再也不被发现 |
| `deliverable.worktree` / `deliverable.branch` 字段 | `internal/tools/task_deliverable_worktree.go` | clone 通道也往这里写自己的目录与分支；`gopi task for-branch` 靠它把 ref 反查回 row。**字段留，语义从"必然是一个注册的 linked worktree"改成"这个 row 的工作目录"** |
| `internal/worktreeroots` / `worktree_fresh_path.go` | 同名文件 | 前者是"临时工作区不能落在 `/` 分区"（Rule 22）的实现；后者是"git 会静默复用已存在的空目录"的守卫 —— 它现在服务于**残留清理**与**测试夹具**，不是 ground 创建 |

### C 级 — 需要先测量才能定生死（本清单的诚实部分）

**C1 — 清理通道（②）不能整体删，因为残留还在。** 实测（批一）：

```
本机 ~/workspace：23 个 linked worktree 目录（15 条注册 / 8 条悬空），207 个 clone ground
#   这一行是**写入本节那一刻**的数。10:53 重测是 22（14 / 8），11:20 复测仍是 22 —— 少的那条
#   是 base-4fb9f7d-gate，它在 10:41 之后、10:46 之前连同 owner 一起消失了（见 §7.1 的自我更正）
peer1  /home/user/work/gopi：git worktree list → 1 条（只有主检出）
```

也就是说 **peer1 上 ground 通道的残留为零，本机不为零**。删清理通道的正确前置条件是"两个宿主都归零"，现在只满足一个。

**C2 — `worktree` 名字下的生产代码是混合体，不是可删单元。** 两个不同的问题，两个不同的数（别混）：

```bash
# A) 文件名里带 worktree 的（non-test）：94 个文件
git ls-files | grep -i worktree | grep -v '_test.go' | wc -l          # 94
git ls-files | grep -i worktree | grep -v '_test.go' | xargs wc -l | tail -1   # 24010 行
git ls-files | grep -i worktree | grep -v '_test.go' | grep '\.go$' | xargs wc -l | tail -1  # 8120 行（.go）

# B) 内容里 MENTION worktree 的生产 Go，按命中数
grep -rc "worktree" --include=*.go internal/ cmd/ | grep -v _test.go | grep -v ':0$' | sort -t: -k2 -rn | head -8
```

B 的实测前八（**命中数**，不是行数）：

```
  635  internal/tools/worktree_sweep.go
  326  internal/tools/task_finish_cleanup.go        ← 该文件本身 6196 行
  229  internal/tools/task_deliverable_worktree.go
  170  internal/tools/task_reclaim.go
  160  internal/tools/path_policy.go
  148  cmd/agent/cli_tasks.go
  115  internal/tools/task_merged_cleanup.go
  100  internal/tools/task_resume_ground.go
```

**`task_finish_cleanup.go` 的 6196 行里只有 326 处提到 worktree，其余是 clean → build → push → 状态写回 → 报告封存。** 按文件名删 = 把交付链路一起删掉。**这一级只能按符号删，不能按文件删** —— 这正是 Rule 43 的三步配方要解决的形状。

**C3 — `devworkspace.GroundModelLinkedWorktree` 是"三种形态"判定的第三档，不能删枚举值。** 实测引用：

```
internal/devworkspace/ground.go:75,118,248   ← 定义 + 判定
internal/devworkspace/ground_test.go:90-91   ← 夹具
internal/tools/task_ground_shape_clause_test.go:75
internal/tools/task_run_ground.go:791        ← briefing 形态句子的第三臂
```

它的作用是让 briefing **从测量说话**（clone / linked-worktree / 不可测 三档各有各的交付后果）。删掉第三档，一个真的被测量成 linked worktree 的目录会被说成 clone 或"不可测" —— 那是 2026-10-27 那个"briefing 自相矛盾"缺陷的复发形状。**留，但它的文案不该再暗示"这是新任务的正常形态"。**

## 3. 依赖图

```
                    ┌───────────────────────────────┐
                    │ settings dev_repo              │
                    │ peer1:/home/user/work/gopi     │
                    └───────────────┬───────────────┘
                                    │ 声明式
                    ┌───────────────▼───────────────┐
                    │ clone 通道 internal/devworkspace.Open │
                    │  → ~/workspace/<cluster>/<id>  │
                    └───────────────┬───────────────┘
                                    │ ground (唯一形态)
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
┌───────▼────────┐        ┌─────────▼──────────┐      ┌─────────▼──────────┐
│ ② 清理通道      │        │ ④ 记账              │      │ ③ 私有检出          │
│ finish_cleanup │        │ deliverable.*      │      │ taskVerifyGround   │
│ reclaim/sweep  │        │ for-branch 反查    │      │ merge build gate   │
│ merged_cleanup │        │ 归属判定            │      │ 测试夹具           │
└───────┬────────┘        └─────────┬──────────┘      └─────────┬──────────┘
        │ 只读历史残留               │ 字段通用               │ 自己创建自己释放
        │                           │                       │
   [残留清零后              [字段留，语义改]          [不动，与通道无关]
    才可删]
                    ┌───────────────────────────────┐
                    │ ① 创建通道 —— 已退役 (批一)     │
                    │   taskGroundCreate/From: 删    │
                    │   TaskRunGroundFor 创建臂: 拒绝 │
                    │   无条件调用点: 已 gate        │
                    └───────────────────────────────┘
```

## 4. 分期计划

| 批 | 内容 | 前置条件 | 验收 |
|---|---|---|---|
| **批一（已交付，范围已按实测收窄）** | ① **只删模型侧创建配方**（PROJECT.md Rule 6a/23/29/Commit Path 步①）；② 调用点 gate（clone 通道开着时不再调 worktree resolver）；③ §1.0 的翻车记录 | 无 | `go build ./...` 绿；`go test ./cmd/agent/ -run TestGOPIMDRule6a` 绿；`go test ./internal/standards/ -run TestBranchForceGate` 绿；**回退后那 41 条 cmd/agent 红消失（两次 stream 差集，见 §1.0.1）** |
| **批二（已交付）** | ① `git mv internal/tools/merge_build_gate_scratch_worktree.go → merge_build_gate_scratch_worktree_helpers_test.go`：生产二进制不再背这 169 行测试助手（**不是删除** —— 它被 5 个测试调用点使用，见 §2 A 级第一行）；② §1.3 的两次 `go test -count=1` 实测确认：`TestStylegateRatchetSuiteIsGreenOnThisTree` 在修复后 **7.135s 通过且 `.git/worktrees` 前后都是 0**（修掉的泄漏是真的修掉了，不是被跳过）；③ C3 注释措辞修正留待批三（与 deliverable 语义一起改，避免同一条注释改两遍） | 批一合入 | `go build ./...` exit=0；`go vet ./internal/tools/` clean；`go test ./internal/tools/ -run 'TestScratchWorktree\|TestRebuildVet\|TestMergesetGate\|TestStylegateRatchetSuiteIsGreenOnThisTree\|TestAssertionTotalityAuditGate' -count=1` → `ok  github.com/the operator/gopi/internal/tools 30.079s`；`.git/worktrees` 计数前后皆为 0 |
| **批三（已交付）** | `deliverable.worktree` 语义改写：`--worktree P` 拼写从"必须是一条注册的 linked worktree"改成"这个 row 的工作目录（两种形态都接受）"；briefing 里两处"必须是 linked worktree"的措辞改成形态中立。**改的是断言，不是措辞** —— 见下方"批三为什么不是文案活" | 批二合入 | `gopi worker --resume --worktree <CLONE ground>` 不再被误判成 MAIN checkout；`gopi worker --resume --worktree <被声明的共享仓库>` 仍被拒绝；`go test ./internal/tools/ -run TestResumeGround` 全绿（26 条） |
| **批四（取证完成，写操作待批）** | 本机残留清零。**从前是"23 个目录 → 0"，取证之后这个说法要拆开**（§7）：8 条悬空（owner 仓库已不在，1.15 GB）可删；4 条注册挂在**已废弃的 `/home/user/work/GoPi`** 上（生产仓库 `/home/user/work/gopi` 自己的 `.git/worktrees` = 0）；4 条是可 prune 的测试残留（`git worktree prune` 就是正确的动词）；4 条 GoPi 上的 worktree 各带一份**别人 run 留下的红证变异**（"throwaway falsification"/"DIAG"）—— 删 = 丢弃一次性变异，留 = 占 3.1 GB，这条要操作员裁决。207 个 clone ground **不在本批范围**（§1：它们是 ground 的唯一形态），但**它们里面确实有可回收的**——§7.8 第二轮取证量出 **9 条**满足严格判据（1591 MB），其中 **5 条 `sweep-worktrees` 根本看不见**（零 anchor，`--dry-run` 里连一行都没有）。**这个数字是判据的第四代**（v0→v3，四代同一条命令）：前三代分别判出 10 / 10 / 11 条，三次都是朝"多删"方向错、每一次都被本机上一个真实目录抓出来（§7.8.5 有四个反例的表）—— 所以它不是"终于算对了"，而是"已被三个反例压过、暂时没有第四个"，所以批四执行时**不能只跑那个 verb**，必须按 §7.8.4 的逐条判据自己量一遍 | 操作员确认（**写操作，需请示**）；清单必须是**带测量时刻的快照**，不能沿用上一轮名单（§4 末尾：`base-4fb9f7d-gate` 在 10:41→10:46 之间自己消失了） | 逐条 stat 判据（**不是** `collectable=N` 汇总行 —— §7.7 实测 98.68% 是幽灵）：`git -C /home/user/work/gopi worktree list` 只剩主检出；`git -C /home/user/work/GoPi worktree prune --dry-run` 无输出 |
| **批五** | 残留清扫通道瘦身：只删"清扫 worktree 注册"的部分，`task_finish_cleanup` 的 clean/build/push/写回一概不动 | 批四完成（两个宿主都归零） | 清扫相关测试红 → 预期（被删能力无对象）；其余全绿 |

**批三为什么不是文案活**（2026-10-27 实测，同一个目录、两种拼写、相反答案）：

```
$ ls -ld /home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77/.git
drwxrwxr-x 8 the operator the operator 4096 Oct 27 10:28 .git                    # → CLONE 形态

$ gopi worker --resume --worktree /home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77
gopi worker: no ground to resume: --worktree /home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77 is the
MAIN checkout of /home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77 — a repo-aware row works in a LINKED
worktree, and the main checkout is not a deliverable ground
exit=1

$ gopi worker --resume 01a17aba-da29-72f5-b7e9-b3c5fb449d77
resume ground: dir=/home/user/workspace/01a17aba-da29-72f5-b7e9-b3c5fb449d77 branch=master repo=/home/user/work/gopi
row=01a17aba-da29-72f5-b7e9-b3c5fb449d77 (task) — (the rest of the line names the model and channel; see $S/resume-worktree-clone-refusal.txt)                          # 同一个目录，默认拼写解析成功
```

机制不是措辞：`task_resume_ground.go` 里那条共用的函数体用 `worktreeSweepMainCheckoutOf`
判断"P 是不是主检出"，而 CLONE 形态下 `git rev-parse --git-common-dir` 返回的是
`<clone>/.git` 本身 —— 于是主检出 = 它自己，`SameDir(main, dir)` 恒真，**每一个 clone
ground 都被当成"自己的主检出"拒绝**。而那正是这个 epic 保留下来的唯一形态（§1 的
2047 clone / 23 linked 普查）。所以这一批改的是**判据**：先量形态
（`devworkspace.GroundModelOf`，与 briefing、advisory、默认拼写同一个函数），
CLONE 走自己的臂。

**新增的 CLONE 臂保留了真正要紧的那条保护**：共享检出永远不交给一个会用"删掉它的
ground"来收行的 resume。判据换成两条可测量的事实 —— ① 这个目录**服务着** linked
worktree（`git worktree list` 除主条目外还有注册；一次运行的工作区一条都不服务，
本 run 自己的 ground 实测 0 条）；② settings 的 `repos` 表**声明**了它（声明就是
操作员自己说"这是共享的"）。两条任一命中即拒绝，且拒绝句仍用操作员已经认识的那句
"MAIN checkout"（Rule 1b：同一句拼写由常量共享，测试和实现不会漂）。

**真实的两个方向都跑过**（Rule 31，不是单测自证）：

```
$ ./bin/gopi worker --resume --worktree /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c
gopi worker: resume: no ledger row names --worktree /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c
(1038 row(s) carry a branch) — there is nothing to resume: the ground may be a foreign
checkout, or the row was deleted
exit=1        ← 修复前这里停在 "MAIN checkout of itself"；现在越过了形态检查，停在账本查找

$ ./bin/gopi worker --resume --worktree /home/user/work/gopi
gopi worker: resume: --worktree /home/user/work/gopi is a repository's MAIN checkout — it is
a key of the settings "repos" table, which declares it as this host's shared repository. (the refusal continues: it names the offending repo key and the route a repo-aware row must take — full text in $S/batch3-rule31-run.txt)
exit=1        ← 新增保护对真正的共享检出生效
```

证据落盘：`~/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/resume-worktree-clone-refusal.txt`（修复前的
反向证据）、`~/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/batch3-rule31-run.txt`（修复后的两个方向）。

**批一被划掉的一项，和划掉它的实测**：原计划"删 ① 创建通道 + 删 `task_ground_baseline*`"**不成立**，两项都是活的：创建通道是未声明 `dev_repo` 宿主的兜底 ground（删了 41 条 e2e 红，两次 stream 差集实测），baseline 判定被 `task_run_ground.go:381` 的创建细节拼接调用。见 §1.0。

**批四为什么是写操作、要请示**：删 `~/workspace` 下的目录是不可逆的。Rule 42 的判据在这里成立：**没有持有句柄的东西不能删**。

**这句原来举的例子已经失效，而它失效的方式本身就是一条教训。** 原文写"15 条注册里有 `gopi-ctltip-01a17a87`（2026-10-27 08:52，detached）这种可能仍被引用的"，写它的时候那是实测；10:46 复核时 `ls -d /home/user/workspace/gopi-ctltip-01a17a87` 已经是 `No such file or directory` —— 一个**别的 run 的 ground** 在两次观察之间自己消失了。同一个快照里还有一条更极端的：`base-4fb9f7d-gate`（103 MB，owner `01a17aa4-` 开头（**当时只记下 8 位前缀，后 28 位没有落盘**；该目录和它的 owner 一起消失了，所以完整 id 今天已无从回溯——这本身就是“快照要记完整”的理由））在 10:41 的普查里是 `LINKED-OK`，10:46 复查时目录和它的 owner 一起不见了。**所以批四的清单必须是快照，每一条都带测量的时刻**（§7 就是这么写的）；把上一轮的名单当成本轮的待办，会把"别的运行刚刚清掉的东西"记成自己的功劳，也会把"刚刚出现的东西"漏掉。

## 5. 明确不在本清单范围内

- **Rule 6a 本身**（worktree 作为 agent 开发流程的标准）—— 那是**人/agent 手工开发**的流程，与 **gopi 自己给 row 造 ground** 是两件事。批一删的是后者。改写 Rule 6a 的措辞是另一条任务（本会话的第 4 项），判据是"不要让读规则的人以为 gopi 会给他造 worktree"，而不是"取消 agent 用 worktree 干活的权利"。
- **`scripts/lib/worktree-fresh.sh` / `scripts/repro-live-holder-worktree-veto.sh`** —— 操作员手工工具的辅助，不在生产路径。
- **测试夹具里的 `git worktree add`**（42 个 `*_test.go`）—— 夹具造 worktree 与被测行为无关。**例外是 `cmd/agent/task_ground_e2e_test.go` 这类真实走查**：它们不自己造 worktree，而是**依赖 runner 造**（`task_ground_announcement_e2e_test.go:64` 那句 "the ground the runner supplied"），删掉 runner 侧创建臂就是删掉它们的被测对象 —— 2026-10-27 的 41 条红正是这么来的（§1.0.1 的差集）。

## 6. 这份清单怎么复核

```bash
# ① RUNTIME 侧创建通道必须还在（它是未声明 dev_repo 的宿主的兜底 ground）
git grep -n 'func taskGroundCreate' -- '*.go'                    # 期望：internal/tools/task_run_ground.go 有定义
git grep -n 'TaskRunGroundFor' -- '*.go' | grep -v _test.go      # 期望：cmd/agent/cli_tasks.go 一处（已 gate）+ 定义

# ② 模型侧配方必须不在（这才是批一删掉的东西）
grep -c 'git worktree add ~/workspace/gopi-<task>' PROJECT.md    # 期望：0
grep -c 'Always use a linked worktree' PROJECT.md                # 期望：0

# ③ 放错文件的测试助手（批二已改名搬进 _test.go）
grep -rn 'mergeBuildGateScratchWorktree' --include=*.go . | grep -v '_test.go:'   # 期望：无输出
grep -c 'func mergeBuildGateScratchWorktree' internal/tools/merge_build_gate_scratch_worktree_helpers_test.go  # 期望：1

# ④ 形态判定的三档是否都在
git grep -n 'GroundModelLinkedWorktree' -- '*.go'                # 期望：ground.go 定义 + 测试 + 生产一臂

# ⑤ 批三：--worktree 拼写必须按形态分臂（CLONE 走自己的臂，不再落进 main-checkout 判断）
grep -n 'GroundModelOf(dir) == devworkspace.GroundModelClone' internal/tools/task_resume_ground.go   # 期望：1 处
grep -n 'func taskResumeGroundSharedCheckoutReason' internal/tools/task_resume_ground.go             # 期望：1 处定义
# 真跑（不是单测）：同一个 clone ground，默认拼写与 --worktree 拼写必须都解析成功
./bin/gopi worker --resume --worktree /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c
#   期望：停在 "no ledger row names --worktree /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c"
#         —— 即已经越过形态检查（批三修复前它停在 "is the MAIN checkout of「它自己」"）
./bin/gopi worker --resume --worktree /home/user/work/gopi
#   期望：被拒绝，且理由是 settings 的 "repos" 表声明了它

# ⑥ 宿主残留现状（两条命令问的不是同一件事，必须都跑）
ssh peer1 git -C /home/user/work/gopi worktree list                          # peer1 的 production
git -C /home/user/work/gopi worktree list                                   # 本机 production（小写目录名）
ls -d /home/user/work/gopi/.git/worktrees/*/ 2>/dev/null | wc -l            # 期望：0（生产仓库自己是干净的）
git -C /home/user/work/GoPi worktree prune --dry-run -v                     # 老检出（大写 P）：期望无输出

# ⑦ 测试侧：批三的两条回归臂
go test ./internal/tools/ -count=1 -run 'TestResumeGroundByWorktree' -v
#   期望：TestResumeGroundByWorktreeTakesACloneWorkspace、TestResumeGroundByWorktreeRefusesADeclaredRepo 各 PASS
```

**⑥ 的本机实测（2026-10-27，供批四参考，不是结论）**：`git worktree list` 在有 clone ground 的
仓库里只列主检出 —— CLONE 形态的 ground 根本不出现在这张表里，这正是"按 worktree list 清点
残留"会漏掉 207 个目录的原因。清点残留要数目录、判 `.git` 的 kind，不是读 worktree list。

## 7. 批四的前置：本机残留的逐条取证（2026-10-27 10:53，**快照，不是待办**）

批四是写操作，所以在动任何目录之前先把「到底有什么、谁拥有它、它是不是脏的」量清楚。
下面的数字都带测量时刻，因为**它们会在两次观察之间变**（见 §4 末尾那条）：
`base-4fb9f7d-gate` 在 10:41 还是 `LINKED-OK 103 MB`，10:46 目录和它的 owner 一起消失了。

### 7.1 形态普查（判别器 = `.git` 是目录还是文件，不是名字）

```
$ for d in */; do d=${d%/}; if [ -d "$d/.git" ]; then echo CLONE
    elif [ -f "$d/.git" ]; then gd=$(sed -n "s/^gitdir: //p" "$d/.git");
        case "$gd" in /*) t=$gd;; *) t="$d/$gd";; esac;
        if [ -d "$t" ]; then echo LINKED-OK; else echo LINKED-DANGLING; fi
    else echo OTHER; fi; done
    207 CLONE
      8 LINKED-DANGLING
     15 LINKED-OK          <-- 这一行和本文件自己的 §7.2 表对不上，见下
    112 OTHER
```

**这四行里有一行是错的，而它错的方式值得留着**（2026-10-27 11:20 复核发现）：
`112+207+8+15 = 342`，可 `112+207+8+14 = 341`。§7.2 那张逐条表（与本节同一次测量、同一个
文件）只有 **22 行：14 条 owner 在 / 8 条 owner 不在**。11:20 用
`bash $S/shape-census.sh | awk '{print $1}' | sort | uniq -c` 重跑，得到

```
    207 CLONE
      8 LINKED-DANGLING
     14 LINKED-OK
    112 OTHER
```

—— 与逐条表一致，与那行 `15` 不一致。原因是**时间**：`15` 是 10:41 那一刻的数，
那时 `base-4fb9f7d-gate`（103 MB）还在；10:46 复查时它和它的 owner 一起消失了，
逐条表（10:53）里已经没有它，但那行汇总没有跟着改。**一次普查里的汇总行和明细表可以是
两个时刻的数，而汇总行看起来更像"结论"** —— 这正是本节反复在说的事，只不过这次它发生
在我自己的文档里。以 **22（14 在 / 8 悬空）** 为准。

207 个 CLONE 就是 §1 说的「ground 的唯一形态」在本机的存量 —— 它们**不出现在
`git worktree list` 里**（clone 不是注册），所以按 worktree list 清点会漏掉全部 207 个。

### 7.2 22 条 linked 的逐条判定（10:41 那一刻是 23 条，多出的 `base-4fb9f7d-gate` 在 10:46 消失）

| 目录 | owner repo | owner 在? | 分支 | 脏 | MB |
|---|---|---|---|---|---|
| gopi-01a12bfd-verify2 | GoPi | 在 | detached | **1** | 142 |
| gopi-01a12e32f80c76eaaf47a71521ee8192 | reportfacts（/dev/shm） | **不在** | — | 0 | 146 |
| gopi-01a12e35b1707ea4852883d4b3080182 | reportfacts（/dev/shm） | **不在** | — | 0 | 146 |
| gopi-01a13019-teeth | GoPi | 在 | detached | **1** | 148 |
| gopi-01a13019-teeth2 | GoPi | 在 | detached | **1** | 148 |
| gopi-01a16a7e05f872e0af8800ab815a4b7c | rf-repo | 在 | task/01a16a7e05f872e0af8800ab815a4b7c | 0 | 160 |
| gopi-01a16a7fca79791ab924555906c42b3a | rf-repo | 在 | task/01a16a7fca79791ab924555906c42b3a | 0 | 160 |
| gopi-01a16a7fca947c2a9649548c79d00095 | rf-repo | 在 | task/01a16a7fca79791ab924555906c42b3a | 0 | 160 |
| gopi-01a16a92e34b7eb5ae78e82c46fb02de | probe-repo | 在 | task/01a16a92e34b7eb5ae78e82c46fb02de | 0 | 160 |
| gopi-01a16a9c69a77d7e96da85f8f3b845b3 | probe-repo | 在 | task/01a16a9c69a77d7e96da85f8f3b845b3 | 0 | 160 |
| gopi-basecheck-f0288c0 | 01a15caf-cd17-7548-8914-9265e5510721（clone，已不在） | **不在** | — | 0 | 102 |
| gopi-baseline-probe-check | 01a1609f-2b19-7688-91ab-f642a66a9995（clone，已不在） | **不在** | — | 0 | 103 |
| gopi-bfd-repro | GoPi | 在 | detached | **1** | 142 |
| gopi-ellipsis-offset | 01a16053-6ba4-700d-a5f2-8aed12d8968c-r1（clone，已不在） | **不在** | — | 0 | 188 |
| gopi-pre-01a169da | 01a169da-65a5-7843-b1fb-96acb7cb6560（clone，已不在） | **不在** | — | 0 | 101 |
| gopi-rp4-wtbase | gopi-rp4 | 在 | detached | 0 | 159 |
| gopi-rp4-wtclean | gopi-rp4 | 在 | detached | 0 | 159 |
| gopi-rp4-wtgreen | gopi-rp4 | 在 | detached | 0 | 159 |
| gopi-rp4-wtred | gopi-rp4 | 在 | detached | 0 | 159 |
| gopi-verify-adb7dc0 | 01a1698c-7c9d-7d4a-a7e0-1a94666e3df7（clone，已不在） | **不在** | — | 0 | 101 |
| mc-01a16208 | 01a16208-2a83-7ebb-bbc1-7ea15c70948e（clone，已不在） | **不在** | — | 0 | 100 |
| zz-base-arm-parent | 01a16ada-8fb6-7cc1-98c1-f284f771e765 | 在 | detached | 0 | 101 |

合计 3.1 GB。**8 条悬空（owner 已不在）合计 1.15 GB**，删它们不会影响任何活仓库。

### 7.3 四条「脏」的，内容是测试用的红证臂（**证据，不是交付物**）

```
gopi-01a13019-teeth   cmd/agent/merge_every_child_test.go   +  // ARM A (throwaway falsification): tolerate EVERYTHING.
gopi-01a13019-teeth2  cmd/agent/merge_every_test.go         +  // ARM B (throwaway falsification): the delegation is gone; a second,
                                                              // weaker hand-rolled walk is back.
                                                              // (the replacement body is an os.Stat + t.Fatalf, 5 lines)
gopi-bfd-repro        testsuite/chat_paste_spill_pty_test.go + 8 行探针：7 条 t.Logf("DIAG <字段>") + 1 条 filepath.WalkDir（逐字内容不是省略的：
                                                              `grep -n 't.Logf("DIAG' ~/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/batch4-dirty-diffs.txt`)
gopi-01a12bfd-verify2 testsuite/chat_paste_spill_pty_test.go + 146 行（staged）
```

按 Rule 30 的正确处置：**untracked 是垃圾，tracked 的改动是未提交的交付物** ——
但这四条都是**别人 run 的临时变异**（"throwaway falsification"、"DIAG"），不是任何人的交付。
它们被留在那里，是因为那些 run 结束时没人复查。**批四不替它们做决定**：
删目录等于丢弃这些变异（它们本来就是一次性的）；保留则占 3.1 GB。这一条要操作员裁决。

### 7.4 四条注册挂在一个**已废弃的检出**上

```
$ git -C /home/user/work/GoPi log -1 --format='%h %ci'      # 0592c9ae4 2026-10-18
$ git -C /home/user/work/gopi  log -1 --format='%h %ci'     # 6e3191153 2026-10-27
$ git -C /home/user/work/GoPi config --get remote.origin.url # (空)
$ git -C /home/user/work/gopi  config --get remote.origin.url # peer1:/home/user/work/gopi
$ ls -d /home/user/work/gopi/.git/worktrees/*/ | wc -l        # 0 —— 生产仓库自己是干净的
```

`/home/user/work/GoPi` 是**大写 P 的那个老检出**：origin 为空、HEAD 停在 10-18，
已经被 `/home/user/work/gopi`（小写，origin 指向 peer1）取代。§7.2 里四条
`owner=GoPi` 的 worktree 全都挂在它上面。所以「本机有 4 条 worktree 注册」这句话，
说准确些是「**一个废弃的检出上挂着 4 条**」——生产仓库一条都没有。

### 7.5 另有 4 条**可 prune 的测试残留**注册

```
$ git -C /home/user/work/GoPi worktree prune --dry-run -v
Removing worktrees/wt:  gitdir file points to non-existent location
Removing worktrees/wt2: gitdir file points to non-existent location
Removing worktrees/wt3: gitdir file points to non-existent location
Removing worktrees/wt1: gitdir file points to non-existent location
```

这是 2026-10-18 那批 `TestStylegateRatchetSuiteIsGreenOnThisTree` 在 `/dev/shm` 下造的
worktree，注册目录（`wt`/`wt1`/`wt2`/`wt3`，mtime 10-18 05:36–05:48）留在了 GoPi 里，
目录本身随 suite scratch 一起没了。它们**不是**本轮修的那个泄漏的复发：那轮之后
`.git/worktrees` 前后都是 0（§1.3），这 4 条是更早的同一形状。
`git worktree prune` 是回收它们的**正确且唯一**的动词，不需要我们写任何删除逻辑。

### 7.6 证据文件（本机，非交付物）

```
/home/user/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/batch4-forensics-23-linked.txt   # §7.1–7.2 原始输出
/home/user/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/batch4-dirty-diffs.txt          # §7.3 四条完整 diff（413 行）
/home/user/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/batch4-residue-forensics.txt    # §7.4–7.5 + sweep 自身判定
/home/user/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/bug-sweep-collectable-counts-phantoms.txt  # 已提交的 bug 行的复现
```

### 7.7 这条证据链顺手测出的第二个缺陷（已提单，不在本 epic）

`gopi task sweep-worktrees --dry-run`（本该是批四的**执行工具**，所以先跑了一次）报
`run-ground anchors: collectable=756 kept=232`。逐条 stat 756 条路径：**EXISTS=10，GONE=746**。

```
10 / 756 = 1.32%   实际可释放
746 / 756 = 98.68% 目录早已不在
```

机制：报告腿（`run_ground_anchor.go:563`）无条件打 `collectable`，而**删除腿**
（`:637` 起的 "THE GONE RUNG"）第一步就把这类 anchor 全部 skip —— 作者知道这些目录会不在，
并且在删除腿里正确处理了，只是报告腿没有这一档。加上 anchor sink 是 append-only、
没有任何回收臂（`grep -rn 'DELETE FROM run_ground\|anchorDelete\|runGroundAnchorDelete'` 零命中），
幽灵只增不减。

**这对批四的直接后果**：这个 verb 的 `collectable=` 计数**不能用作批四的进度指标**，
更不能用它来判断"已经清干净了没有"。批四的判据必须是本节这样的逐条 stat，
不是任何一条 `collectable=N` 的汇总行。已提单：`01a17b48-9407-7d3b-85d6-438fafc687e5`。

### 7.8 批四的第二轮取证：clone ground 侧，以及 anchor 人口**两头都不准**（2026-10-27 11:14，快照）

§7.1–7.7 量的是 **linked worktree** 那一侧（22 个目录）。真正占地方的是另一侧：
**207 个 clone ground**。下面每个数字都带测量时刻，理由同 §7 开头 —— 同一份名单在两次
观察之间会变（本轮 11:14 量到 linked **22** 条，10:53 是 **23** 条；少的那条自己消失了）。

#### 7.8.1 形态与人口（判别器只有一个：`.git` 是文件还是目录）

```
$ cd /home/user/workspace && bash /home/user/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/shape-census.sh
#   完整脚本就是上面 §6② 那一段；本次运行的原始输出在 batch4-snapshot-A-C.txt
clone grounds=207        linked worktrees=22  (reachable=14 / 2117 MB，DANGLING=8 / 987 MB)

# clone ground 按 remote.origin.url 分
origin=gopi/GoPi: 151    other project: 47    no origin: 9
```

那 47 个 other-project 的（`stable-diffusion-webui` 282189 MB、`llvm-project` 261402 MB、`latent-diffusion` 106614 MB、`fastText` 60994 MB（剩下 43 个更小，清单用下面那条命令取））
是操作员自己的克隆，**不是 ground**；`du` 把整个 `/home/user/workspace` 算成 896 GB，量的是它们。
判别 ground 只能用「`.git` 是目录 **且** 名字是 task id / 有 gopi origin」。

#### 7.8.2 anchor 人口：唯一的人口来源，而它两头都不准

`RunGroundCollectableGrounds`（`run_ground_anchor.go:493`）的人口来源是
`RunGroundAnchors(path)` —— **sink 里的 anchor 记录，不是文件系统**。它唯一的 production
调用点是 `cmd/agent/cli_tasks.go:4826`（`sweep-worktrees`）。由此有两个相反方向的误差：

| 方向 | 实测 | 机制 |
|---|---|---|
| **多算（幽灵）** | sink 988 条 anchor 指向的目录，**只有 128 还在盘上，860 条早已不在** | 报告腿（`:563`）无条件打 `collectable`；删除腿（`:637` "THE GONE RUNG"）第一步就 skip。已提单 `01a17b48-9407-7d3b-85d6-438fafc687e5` |
| **少算（活的目录不在人口里）** | 151 个 gopi-origin clone ground 里 **23 个根本没有 anchor**，其中 13 个是 uuid 命名、合计 2114 MB | `TaskRunGroundForClone` 在 `repo == ""` 时 early return（`task_run_ground.go:1113-1121`），而 `TaskGroundAnchorRecord` 在它**之后**（同文件 :1133）。**没有 repo 的 row，它的 ground 永远没有 anchor**。已提单 `01a17b5b-b96c-7bbd-887b-8d844cd9b609` |

少算那一头的新标本（本机今天就躺着）：

```
$ ./bin/gopi task show 002c43f2-d6dc-4c4b-82d9-f2b40163d53c
status:         completed
source:         auto
submitted_by:   harness auto-registration (row IS the run: session 002c43f2-d6dc-4c4b-82d9-f2b40163d53c)
                # 没有 deliverable.repo 这一行
$ test -d /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c/.git && echo CLONE
CLONE (整棵仓库，233 MB)
$ grep -c 002c43f2 /home/user/.gopi/logs/removal-audit.log
0                # 零 anchor、零删除记录 —— 两条腿都看不见它
```

已单独提单（证据：`bug-repo-less-row-never-anchors.txt`）。**它不是批四的一部分**：
修它是写代码，批四是清目录。

#### 7.8.3 报告腿与删除腿，在同一批目录上给出不同答案

把 `sweep-worktrees --dry-run` 报的 collectable 逐条 stat，**10 个存在**。对这 10 个逐条问
删除腿的 dirty rung（`runGroundCollectCloneVerdict` ②，任一脏即拒）：

```
6 个：tracked changes 1..46 条   → 删除腿会 REFUSE（未提交的工作 = 人的裁决）
4 个：tracked=0 untracked=0      → 删除腿会 proceed
```

所以 `collectable=` 这个数字**第三重不准**：它连"删除腿会不会真的删"都没算。已单独提单（它不是 `01a17b48` 的重复：那一条问存在性，这一条问可删性）：`01a17b60-ddcf-725c-a2c6-6a90acae3e5d`。三件事
一起说明同一个结论 —— **批四的判据只能是逐条测量，不能是任何一条汇总行**。

#### 7.8.4 严格判据下的真清单（可复算）

判据（每条都独立实测，顺序即成本从低到高）：
① `.git` 是目录（clone ground）② origin 含 gopi/GoPi ③ 目录年龄 ≥ 24h
④ `git status --porcelain` 为空 ⑤ **这个 clone 拥有的任何本地 ref**（分支、tag、HEAD）
    都不持有交付点 `refs/remotes/origin/master` 缺的提交（三个反例见 7.8.5 —— 这条判据
    的第一版与第二版各错一次，两次都是"可删"方向的假阳性）⑥ 没有活进程的 cwd 在里面
⑦ 排除本行/在跑的三条（本行 `01a17aba-da29-72f5-b7e9-b3c5fb449d77`、`01a17ae7-a6b6-7ccd-93e8-6811fb1d73e1`、`01a17ae7-a6f6-7c81-86e5-043d7a82cc0b`）

```
$ bash ~/.gopi/sessions/01a17aba-da29-72f5-b7e9-b3c5fb449d77/strict-collectable.sh
  # the script lives with this run's evidence (it is a measurement instrument, not a
  # deliverable: Rule 35 — a script in the tree would be a new persistent dependency
  # that nothing reclaims). Its seven criteria are quoted above in ①②③④⑤⑥⑦, and the
  # TWO lines that matter are criteria ③ and ⑤:
  #   mtime=$(stat -c %Y "$dir"); age=$((NOW-mtime)); [ "$age" -ge "$AGE" ] || continue
  #   unpushed=$(git -C "$dir" rev-list --count --branches --tags HEAD \
  #                --not refs/remotes/origin/master 2>/dev/null) || continue
  #   [ "$unpushed" = "0" ] || continue
  # re-derive them from those lines alone if the file is gone. The age gate is the one
  # a hand-written quick list forgets, and §7.8.6 is what that costs.
STRICT collectable clone grounds: 9  (1591 MB)

  139M 01a16991-2e81-7889-a7bf-b598dbadecf7  NO-anchor   peer1:/home/user/work/gopi
  139M 01a16a6a-a297-7115-bf3c-df5eeb38d941  NO-anchor   peer1:/home/user/work/gopi
  139M 01a16a6a-ca22-7759-a53b-f8ea02eb91db  NO-anchor   peer1:/home/user/work/gopi
  139M 01a16a6a-f3e8-70e2-9c20-b715c93143fb  NO-anchor   peer1:/home/user/work/gopi
  139M 01a16d0f-2874-7b36-b65f-84af4a0f50fc  NO-anchor   peer1:/home/user/work/gopi
  236M 01a16deb-73df-7a96-851d-97297f619246  anchored    peer1:/home/user/work/gopi
  224M 01a16f9d-88f3-7b53-99a3-130dbb12aa9a  anchored    peer1:/home/user/work/gopi
  231M 01a16fb3-7639-767e-8c84-1cd1eda1aed5  anchored    peer1:/home/user/work/gopi
  205M 01a173cb-d40d-76cf-8148-59adb464a3aa  anchored    peer1:/home/user/work/gopi
```

9 条里只有 **4 条**与 `sweep-worktrees` 的 collectable 名单重合；其余 5 条**完全没有
anchor**，sweep 的 `--dry-run` 里连一行都没有（§7.8.2 那一头）。

**三个数字，三个不同的判据，别混用**：11 是第二版判据（问 HEAD，不问本地其它 ref），
10 是第一版（问 `--not --remotes`），9 是现在这一版（问"这个 clone 拥有的任何 ref"）。
每一次收窄都是因为一个**真实的目录**被误判成可删（§7.8.5）。**收窄的方向是安全的方向**：
从"能删的更多"变成"能删的更少"，而每一条被移出名单的都带一份实测理由。

**这一节不是待办，是一份带时刻的清单**：执行批四时必须重新量（理由见 §7 开头）。
**而且必须用这一节的七条判据重新量，不能用任何别的快照** —— 同机同目录在 11:32 还有
一份**四条判据**的清单（少了年龄闸），它多列了 `002c43f2`，差集的成因见 §7.8.6。

#### 7.8.5 判据的四代：每一个反例都是本机上一个真实目录

这一节是整份文档里最该被读的部分。它记的不是"我想清楚了"，而是**判据错了三次、
三次都是朝"多删"的方向错、每一次都是被一个真实存在的目录抓出来的**。四代判据、
四个数字、同一条命令：

| 代 | 判据问的是 | 判出 | 被谁推翻 |
|---|---|---|---|
| v0 | `--all --not --remotes`（任何 remote 有没有） | 10 | `01a15b11` |
| v1 | 交付点 vs `rev-parse --abbrev-ref HEAD` | 10 | `01a16933`（答案对、理由错） |
| v2 | 交付点 vs `rev-parse HEAD` | 11 | `01a16933`（答案错） |
| v3（当前） | 交付点 vs **这个 clone 拥有的任何 ref** | **9** | 暂时没有第四个 |

**v0 —— 问"任何 remote 有没有"，判 10 条可删。**

反例 `01a15b11-dd6a-7e34-972e-573f1a424fc7`。sweep 说 `keep`：

```
kept: the ground's branch master holds 1 commit that no delivery point has
      (refs/remotes/origin/master, refs/remotes/origin/master) — this directory
      is that work's only home
```

两个答案都对，问的不是同一件事：那个 commit `71ea4f7` **确实**在
`refs/remotes/origin/feat/reopen-carries-evidence` 上，**不在** `origin/master` 上。
`--not --remotes` 问"任何 remote 有没有"，删除腿问"**交付点**有没有"。一个只住在 feat
分支上的提交会被判成已交付，从而删掉它的唯一 home。**判据必须问交付点。**

**v1 —— 交付点 vs `rev-parse --abbrev-ref HEAD`，判 10 条可删。**

这一代对 `01a16933` 的答案是"量不出，保留"：detached 的 clone 上
`rev-parse --abbrev-ref HEAD` 返回字符串 `HEAD`（不是分支名），判据于是 `continue`。
**结论碰巧是对的，理由完全是错的** —— 它跳过的是"detached ⇒ 不可判定"，而 detached
本身完全可判定（HEAD 就是个 commit id）。

**v2 —— 交付点 vs `rev-parse HEAD`，判 11 条可删。**

反例 `01a16933-3731-7ddc-bd9a-6596c39cd1ef`（232 MB）。它是 **detached**，而 HEAD 恰好
就在交付点上：

```
$ git -C /home/user/workspace/01a16933-3731-7ddc-bd9a-6596c39cd1ef rev-parse --short HEAD
d09822a                                     # == origin/master 的 tip
$ git -C /home/user/workspace/01a16933-3731-7ddc-bd9a-6596c39cd1ef rev-list --count \
      refs/remotes/origin/master..refs/heads/master
7                                           # 本地 master 握着 7 个交付点没有的提交
$ git -C /home/user/workspace/01a16933-3731-7ddc-bd9a-6596c39cd1ef merge-base --is-ancestor \
      refs/heads/master refs/remotes/origin/master && echo YES || echo NO
NO
```

问 HEAD 得到 `ahead=0`，问本地 `master` 得到 `7`。sweep 的 `keep` 理由正是那 7 条
（"the ground's branch master holds 7 commits that no delivery point has"）。
**detached 的 HEAD 与那棵仓库持有的分支是两份不同的东西**，问其中一个就删掉另一个的
唯一 home。

**同一个缺陷在 v1 与 v2 里以两种相反的样子出现**：v1 因为"量不出"而跳过（答案对、
理由错），v2 因为"量对了 HEAD"而误删（答案错）。**真正缺的从来不是"更好的 HEAD 判断"，
是"该问的对象根本不是 HEAD"。**

**v3（当前）—— 问"这个 clone 拥有的任何 ref"：**

```
unpushed=$(git -C "$dir" rev-list --count --branches --tags HEAD --not refs/remotes/origin/master)
[ "$unpushed" = "0" ] || continue
```

判 9 条 / 1591 MB。**它不问"哪个 ref 是当前检出的"，因为这个问题没有正确答案** ——
一个 clone 可以把工作留在任意一个本地 ref 上，而删除删掉的是整个目录，所有 ref 一起消失。

三次脚本运行的原始输出（同一个文件 `strict-collectable.sh` 的 v1/v2/v3 —— v0 写在脚本之前，
是 §7.8.4 原文里的那条 `--not --remotes`）：

```
11:25:47  v1  → 10 条
11:26:06  v2  → 11 条   ← 01a16933 此刻被判可删
11:26:50  v3  →  9 条
```

**`gopi-pristine` 是第四个反例，但它不是判据问题，是不可判定。**
161 MB、无 anchor、`status` 干净、名字不是 task id。它的 `remote.origin.url` 指向
`/home/user/workspace/gopi-01a14be02cb07570b29ea198695b20fe` —— **那个目录已经不在了**，
所以"它的提交有没有交付"在这台机器上**无法证明**。v1/v2 都让它通过了（`origin/master`
存在、差值 0），v3 把它挡在名单外（`--branches --tags HEAD --not` 数出 90 条）。
按"未知即保留"它被排除，并且**不该被任何人静默跳过** —— 它可能是操作员自己的参考检出。
这一条要人来判，不是脚本。

**每次收窄的方向都是安全的那个方向**（可删的更少）。一份清目录的判据，正确性的定义
不是"漏得少"，是"**误删得少**"：漏掉一个目录只是继续占空间（可回收，下次再量），
误删一个目录是不可逆的。

#### 7.8.6 两份清单差一条：不是判据的第五个反例，是**两套判据**

11:32 与 11:33 之间，同机同目录产生了**两份清单**，相差一条 `002c43f2`。我当时的
第一反应是把它记成判据 v3 的第五个反例（"多删"方向）——**这个结论是错的**，实测把
它推翻了，而且推翻的方式比结论本身有用。

差的那一条是 `002c43f2-d6dc-4c4b-82d9-f2b40163d53c`（233 MB）。两份清单的判据写在
各自文件的头两行里：

```
$ head -2 $S/batch4-fresh-snapshot-2026-10-27T11:32:31+08:00.txt
# 批四待删清单 — 快照 2026-10-27T11:32:31+08:00（每次执行前必须重新量，不得沿用）
# 判据: 干净(dirty=0) + 无 live cwd + ahead=0 + 形态=CLONE(.git 是目录)
```

**四条**。而 `strict-collectable.sh` 是**七条**（§7.8.4 的 ①②③④⑤⑥⑦）。差的正是
**判据 ③（目录年龄 ≥ 24h）**，以及 ⑦（在跑的三条按名排除）。所以两份清单不是
"同一把尺子量出两个数"，是**两把尺子**：快照那份是手工收窄时留下的旧尺，它没有年龄闸。

`002c43f2` 在这把旧尺下合格、在新尺下不合格，原因就是年龄：

```
$ m=$(stat -c %Y /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c); now=$(date +%s)
$ echo "mtime=$(date -d @$m -Iseconds) age_h=$(( (now-m)/3600 ))"
mtime=2026-10-27T08:21:21+08:00 age_h=3          # 判据 ③ 要求 >= 24h
```

**而它的"干净、ahead=0、无 live cwd"是真的**——这就是为什么它骗得过旧尺：

```
$ cd /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c
$ git status --porcelain                      # 空
$ git rev-list --count --branches --tags HEAD --not refs/remotes/origin/master
0
$ git status --porcelain --ignored | head -3   # 脏在 ignored，porcelain 看不见
!! .gopi-tags
!! bin/
!! cmd/agent/.gopi-tags
$ du -sm bin .gopi-tags cmd/agent/.gopi-tags
66      bin
20      .gopi-tags
6       cmd/agent/.gopi-tags
```

233 MB 里有 **92 MB 是今晨 08:12–08:21 在它里面编译出的产物**（`bin/gopi` 66 MB、
cscope 风格的 tags 文件 26 MB），`.git/index` 的时间正是 08:08:03、`git reflog` 只有
一条 `clone: from peer1:/home/user/work/gopi`。**它不是一团陈旧残留，是今天早上某个
进程刚做完一整轮 build 的地方** —— 判据 ③ 的年龄闸正是为这种目录设的，而它工作得
完全正确：**旧尺把它放进待删清单，是因为旧尺没有那道闸，不是因为 v3 数错了。**

它的身份也一并测清了：`gopi task show 002c43f2-d6dc-4c4b-82d9-f2b40163d53c` 有账本行（`completed`，
2026-08-30 的 auto-registration 行），但 `session_log` / `run_dir` **都找不到**，
`ground (resolved now)` 三项全 `none`；sink 里 `grep -c` 是 **0**（零 anchor、零删除
记录）；`bin/gopi --version` 是 `v0.3.0`（一份**能跑的**旧二进制）。所以它是一个
**账本行在、run 目录没了、今晨又被重新 clone + build 过**的目录 —— 谁在 08:07 建的它，
`~/.gopi/sessions` 里查不到：15 个在 07:30–08:30 被碰过的 session 中，逐个 grep
目录名**零命中**，`removal-audit.log` 在 08:07:55+08（= 00:07:55Z）那个窗口里也
**零条**记录。**这是推断，不是实测**：它由一次没有 anchor 的 clone 通道 run 建出，
而那条腿只在 2026-10-18 之后的提交里写 anchor（§7.8.2），所以"查不到"与"没人建过"
在本机不可区分。

**结论，也是这一节要留给批四执行者的一句话**：差集**本身不是判据的反例**。看到两份
清单不一致时，第一个动作是**把两份判据的条目并排抄下来比**，不是假设其中一份数错了。
这一次并排比的结果是 v3 仍然只有三个反例（§7.8.5 的表），第四个还空着。

#### 7.8.7 证据文件（本机，非交付物）

```
$S/batch4-snapshot-A-C.txt               # §7.8.1 的人口与形态
$S/batch4-clone-ground-forensics.txt     # §7.8.2 anchor 覆盖（第一版清单）
$S/strict-collectable.sh                 # §7.8.4/§7.8.5 的判据本体（可执行；四代判据就是这份文件的四个版本）
$S/strict-collectable-run.txt            # v3（当前）的原始输出：9 条 / 1591 MB，带测量时刻
$S/bug-sweep-report-vs-remove-leg.txt    # §7.8.3 逐条 dirty 判定
$S/batch4-sweep-dryrun.txt               # sweep --dry-run 全文（232 KB）
$S/bug-repo-less-row-never-anchors.txt   # §7.8.2 少算那一头的机制与标本
$S/batch4-instrument-diff.txt            # §7.8.6 两份清单的差集与两份判据的并排比对（含 002c43f2 的年龄实测）
$S/batch4-fresh-snapshot-2026-10-27T11:32:31+08:00.txt
                                         # §7.8.6 的反面教材：一份四条判据（缺年龄闸）的手写快照
$S/batch4-execute-verify-113839.txt      # 批四执行前的最后一次复核（7 列逐条，见 §7.8.8）
```

#### 7.8.8 批四执行前的最后复核：清单在 7 分钟内没有变化，且每一列都是量出来的

写操作的前一步不是"再跑一遍判据脚本"，而是**把判据的每一列对着清单逐条重测一遍**，
因为脚本输出只给出"它进了清单"，不给出"它凭什么进"。2026-10-27 11:38:39 实测：

```
DIR                                               MB   AGE_H UNPUSHED  REF-SET  ANCHOR    LEDGER
01a16991-2e81-7889-a7bf-b598dbadecf7             139      83        0      A     NO-anchor show_exit=1
01a16a6a-a297-7115-bf3c-df5eeb38d941             139      79        0      A     NO-anchor show_exit=1
01a16a6a-ca22-7759-a53b-f8ea02eb91db             139      79        0      A     NO-anchor show_exit=1
01a16a6a-f3e8-70e2-9c20-b715c93143fb             139      79        0      A     NO-anchor show_exit=1
01a16d0f-2874-7b36-b65f-84af4a0f50fc             139      67        0      B     NO-anchor show_exit=1
01a16deb-73df-7a96-851d-97297f619246             236      26        0      B     anchored  show_exit=0
01a16f9d-88f3-7b53-99a3-130dbb12aa9a             224      54        0      B     anchored  show_exit=0
01a16fb3-7639-767e-8c84-1cd1eda1aed5             231      53        0      B     anchored  show_exit=0
01a173cb-d40d-76cf-8148-59adb464a3aa             205      35        0      B     anchored  show_exit=0
TOTAL: 1591 MB
no live cwd inside any of them: (empty)
```

`REF-SET` 只有两种取值，因为 9 条目录的 ref 恰好只落成两个集合。两条命令把它测全
（`REF-SET` 是下面两次输出的编号，不是概括）：

```
$ git -C /home/user/workspace/01a16991-2e81-7889-a7bf-b598dbadecf7 \
      for-each-ref --format='%(refname)'
refs/heads/master
refs/remotes/origin/HEAD
refs/remotes/origin/fix/ellipsis-offset-anchoring
refs/remotes/origin/master
refs/remotes/origin/task/01a16520-ab3d-7c37-989e-78441cc6f986

$ git -C /home/user/workspace/01a16d0f-2874-7b36-b65f-84af4a0f50fc \
      for-each-ref --format='%(refname)'
refs/heads/master
refs/remotes/origin/HEAD
refs/remotes/origin/master
```

（`REF-SET A` 的四条目录各自输出上面前 5 行，`REF-SET B` 的五条各自输出后 3 行——
已逐条跑过，见 `$S/batch4-execute-verify-113839.txt` 的 `REFS` 列。**每一条都只有
`refs/heads/master` 一个本地分支**，其余全是 `refs/remotes/*`；而 `UNPUSHED=0` 是按
`--branches --tags HEAD --not refs/remotes/origin/master` 量的，已经把那个本地
`master` 算进去了。）

两个必须一起读的列：

- **`show_exit=1` 不是"账本查询失败"**，是 `gopi task show` 回答"没有这一行"——5 条无
  anchor 的 ground 里没有一条在账本上。它们既不占账本行，也不占本机缺的提交（`UNPUSHED=0`
  是按 `--branches --tags HEAD --not refs/remotes/origin/master` 量的，把**所有本地 ref**
  都算进去了）。所以"没有账本行"在这里不是风险，是它们的身份：**没有主人的目录**。
- **`ANCHOR` 那一列不是判据**，只是 `sweep-worktrees` 能不能看见它的解释（§7.8.2：
  5 条零出现 = 清扫通道对它们完全沉默）。批四的清单**不靠** anchor 决定。

复核还给出一个时间维度的读数：11:32 的快照与 11:38 的复核在**同一批目录**上给出同样的
9 条 / 1591 MB，而 11:27 的 v3 输出与此逐字节相同——**7 分钟内清单稳定**，这份快照可以
带着 `2026-10-27T11:38:39+08:00` 的时刻交给执行者。§7.8.6 讲的正是相反的情形
（`base-4fb9f7d-gate` 在 10:41→10:46 之间自己消失），所以时刻不是客套：**它是这份清单
唯一的有效期声明**。
