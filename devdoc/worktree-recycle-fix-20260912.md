<!-- doc-class: normative -->
# Task Report — 恢复 "任务结束 → worktree 回收 → 分支成为 merge 候选" 链条

任务：`01a09250-07bd-71fe-a241-580e5477a469`（重提 of `01a091f9-4e2b`）
分支：`fix/worktree-recycle-2`　worktree：`~/workspace/gopi-wtrecycle2`
日期：2026-09-12

## 一句话结论

链条现在通：任务交付时**自己**回收 worktree（分支保留给 merger），兜底 sweep 也能回收
**几十行深**的滞留目录——但只有三个根因同时修掉才成立，且**活跃 worktree 一个没碰**。

---

## 1. 根因（三个，全部实测定位，不是猜）

### 根因 A — foreign-owner 门把"已死行"当合法持有者（滞留目录的直接阻塞者）

`finishCleanupWorktreeOwnerRows` 把**任何**指向同一目录的行都当成 owner，只要它存在就
拒绝回收。实测：completed 行 `01a08cfd` 被 **cancelled** 兄弟行 `01a08cd5` 永久阻塞；
`gopi-memflake` 同形（completed ↔ failed 互锁）。原始报错串（修复前实测）：

```
worktree ownership mismatch: worktree /home/user/workspace/gopi-cov-agent is owned by
row 01a08cd5 (cancelled, claimed-by pid 3377703 gone)
```

**判据错**：cancelled/failed/completed 行的生命已经结束，"是 owner" 不等于"还持有"。
修复后新增一行分类器 `finishCleanupOwnerRowLive`：terminal/cancelled 行 + 本机可解析且
**已消失**的 pid（或压根没有 claim 戳）= STALE 绑定，不是 owner；**外部主机**的 claim
永远视为 live（本机无法证明它死了——保守方向只有这一边）。

### 根因 B — sweep 扫描饥饿：新到 10 行窗口永久被占满（"日志只有 2 行"的真正原因）

```sql
-- 修复前（唯一一次扫描）：
... WHERE status IN ('completed','failed') ... ORDER BY updated_at DESC LIMIT 10
```

每次 sweep 都重新看**最新的 10 行**——而最新 10 行正是 worker 刚刚回收掉的那些。
一个停在 40 行之外的目录**永远不会被看到**。这不是"拒绝"，是"从来没看"：整个
`~/.gopi/logs` 里 `[worker] [finish-cleanup]` 一共只有 **6 行**（含 09-11 的 2 行），
对一个 1000+ 行的账本、19 个滞留目录来说等于没有，而且**没有一条是拒绝原因**（skip 原因
也不落日志）。

实测证据：修复前 sweep 一次运行返回 `lines=[]`（目录原封不动）；修复后同一场景
（40 行更新 + 1 行真滞留）一行命中、目录被回收。

**修复**：第二道扫描 `serveFinishCleanupStrandedIDs`——按 deliverable 里真的 `worktree`
字段筛候选，逐个 `os.Stat`，**目录仍在**才算滞留；`ORDER BY updated_at ASC`（最老的先
看，cap 花在等最久的行上）；与第一道扫描按 task_id 去重。同时：

- `cancelled` 进扫描集合（根因 A 里被取消的行此前**没有任何扫描覆盖**，其目录因此
  可以永久钉死别的行）；
- skip 原因按 `FinishCleanupSkipIsReportable` 分类上报，扫描失败也上报（Rule 15：扫不动
  不能看起来像没事干）。

### 根因 C — 创建期回填把行"劫持"到别人的 worktree（本次才挖出来的真 bug）

`TaskDeliverableWorktree` 是**检测型** seam：executor 每次 bash 后猜它现在在哪。它的
候选解析里含 `git -C <路径>` 目标。于是一次**只读的跨任务对比**就改写了本行的交付物：

```
row 01a09250 deliverable: ...worktree=/home/user/workspace/gopi-wtrecycle2  （创建时，正确）
        ↓ 我执行 `git -C ~/workspace/gopi-cov-agent status`（看一眼别人的 worktree）
row 01a09250 deliverable: ...worktree=/home/user/workspace/gopi-cov-agent  ← 被改写
```

后果是把两个任务**互相锁死**：本行成了别人目录的 foreign owner（阻塞它的回收），别人的行
又成为本行的 owner（阻塞我的）。这正是根因 A 报错里"同名目录有 3 行"的来源。

**修复**：回填只**补空**，不**改写**——行已有 worktree 且与新探测不同 → 直接返回。允许
"补 branch"（同目录、branch 缺失是报告过的真实缺口），禁止"换目录"。这不是把 seam 关掉：
它的职责是补信息缺口，迁移路径另有其人（reconcile 与行自身的 finish writeback）。

---

## 2. 改了什么（按文件）

| 文件 | 改动 |
|---|---|
| `internal/tools/task_finish_cleanup.go` | STALE 分类器 `finishCleanupOwnerRowLive`；`DeliverableWorktreeKey` + `DeliverableWorktreePath` 导出（SQL 侧与 Go 侧同一个 parser）；skip 上报分类 |
| `internal/tools/task_merged_cleanup.go` | merged-sweep 同形门同步修（同一常量、同一判据） |
| `internal/tools/task_deliverable_worktree.go` | 根因 C：已有绑定不再被探测改写 |
| `cmd/agent/cli_tasks.go` | 第二道 stranded 扫描 + 原因上报；`cancelled` 进集合；顺带修掉 Rule 18 gate 命中的 else-after-return（并把 `internal/standards/baseline.go` 里的过期许可条目删除——基线条目是棘轮，必须随修复一起删） |
| 测试 | `cli_tasks_stranded_sweep_test.go`（新）、`cli_tasks_worker_recycle_e2e_test.go`、`task_finish_cleanup_test.go`、`task_merged_cleanup_test.go`、`task_deliverable_worktree_test.go` |
| `internal/tools/zz_wtrecycle_diag_test.go` | **默认跳过**的实测接口（env `GOPI_WTRECYCLE_LIVE_DIAG=<row-id>[,<id>]`）。**保留是有意的**：§4 的逐个处置要与**生产链路本身**的判定对齐，而在链路外面重新实现一遍 owner 门 = 必然漂移的复刻。默认 skip，正常 `go test` 零成本；其局限见 §5.8 |

**本分支没有改动的既有红项**（A/B 对照见 §6，非本次回归）：
`TestMergeVerifyGatePassKeepsMerge`、`TestCovCrHasSessionEnd_*`、
`TestStartupGate_SkipsTheWalkWhileTodayStamped`——它们的源文件都不在
`git diff --name-only master...HEAD` 里，且在干净 master 检出上同样复现。

常量纪律：`DeliverableWorktreeKey` 是 deliverable JSON 字段名的**唯一定义**（Rule 14），
SQL 与 Go 不再各写一次字符串；scan cap 全是具名常量（Rule 1）。

## 3. 测试与红证明（每条修复都有红→绿）

| 测试 | 修的是什么 | 红证明实测 |
|---|---|---|
| `TestSweepRecyclesADeepStrandedWorktree` | 根因 B（饥饿） | 修前 FAIL：`lines=[]`、目录仍在；修后 PASS |
| `TestStrandedScanReachesADeepRow` | 根因 B 选行 | 同上场景（40 新行 + 1 深滞留） |
| `TestStrandedRowInvisibleToTheNewestScanAlone` | 饥饿的**机制**本身 | 断言滞留行**不在**新到 10 行窗口里——若哪天它在，说明第二道扫描变成死代码，测试会提醒后来人 |
| `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow` | 根因 A（交付路径） | 注入"已死兄弟行"（cancelled + 本机 pid 已消失）后：修前 FAIL（目录仍在）；修后 PASS（目录回收、**分支保留**） |
| `TestStaleOwnerRowIsNotABlocker` | 根因 A（单测） | 修前报出上面那条原始 mismatch 串 |
| `TestTaskBackfillDeliverableWorktreeNeverRebindsToAnotherWorktree` | 根因 C | 修前 FAIL：blob 被改写成别人的目录；修后 PASS |
| `TestTaskBackfillDeliverableWorktreeFillsABranchGapWhileBound` | 根因 C 的反向约束 | 防止"直接禁用 seam"混过测试：同目录补 branch 仍须成功 |
| `TestMergedCleanupStaleOwnerRowIsReleased` / `…LiveCwdHolderKeeps…` / `…ForeignOwnedWorktreeSkipped` | merged 路径 + 安全性 | 同一批 |

**安全性（明确断言，不是靠"应该没事"）**：活跃判定 = 本机进程 cwd 落在该目录 / 行仍为
pending·claimed·in_progress 且本机 pid 活着 / 有 git 锁。`TestStrandedScanIgnoresLiveRows`
断言 live 行永不被选；外部主机 claim 永不判死。

## 4. 滞留 worktree 逐个处置（实测）

处置判据（全部实测）：行状态终态 + 本机 pid 已消失 + 目录无 live cwd 持有者 + 无未提交改动
→ 可回收；**有任一不满足就保留**，并在表里写明原因。

| worktree | 判定 | 处置 |
|---|---|---|
| `gopi-cov-agent` | 3 行同点名（in_progress 行是我自己=劫持产物）、0 持有者、clean | ✅ 实测回收（分支 `feat/cov-agent` 保留） |
| `gopi-memflake` | completed 行 + failed 兄弟，0 持有者、clean | ✅ 实测回收（分支保留） |
| `gopi-verifyloop` / `gopi-synctagcase` | 终态行、0 持有者、clean | ✅ 实测回收（分支保留） |
| `gopi-wc` | 行已 completed、0 持有者、clean | ✅ 已回收、分支保留。**归属订正**：早前草稿写成"线上 worker 自行回收（独立证据）"——**该说法不成立，已撤回**。全天 `~/.gopi/logs/*.log` 里只有 **6 条** `[worker] [finish-cleanup]` 行，分别是 `diag/tools-flake`、`fix/crashdump-scan-cost`、`fix/crashdump-classify`、`feat/bug-verify-default`（各一次），**没有任何一条点名 gopi-wc**。加上 §5.6 的结构性事实（线上二进制根本不含本次修复），这次回收只能归因于**我这条分支的二进制跑诊断时执行的**——不能当独立证据用 |
| `gopi-hippo-adopt` / `gopi-wtrecycle` | 残留目录，**已不是注册 worktree、无 .git**，内容已进 master `30c1ed3f`（逐字节比对：`leftover-zz_sweep_diag_test.go.txt` 与留存文件相同） | ✅ 删除残留目录 |
| `gopi-ghome-hygiene` / `gopi-provguard` / `gopi-mc3` / `gopi-premerge-probe` / `gopi-salvage-verify` / `/tmp/clicontract-base` | 终态行、0 持有者、clean | ✅ 移除 worktree。**分支已复查**：这几个目录对应的分支（`fix/gopi-home-hygiene`、`fix/wip-worktree-delete-guard` 等）在当前仓库已**不存在**——它们不是"丢失"，而是**早已 merge 进 master 后被正常删除**（`8db1b68b Merge branch 'fix/gopi-home-hygiene'`、`0581206b Merge branch 'fix/wip-worktree-delete-guard'`，两条均 `git merge-base --is-ancestor → IN-MASTER`）。内容在 master，ref 不存在不影响任何东西 |
| `gopi-merger-top` / `gopi-liveowner` / `gopi-salvage-r7` / `gopi-wtrecycle2` | **in_progress 行 + 本机 pid 活着**（含我自己） | ⛔ **未碰** |
| `gopi-salvage-accept` / `gopi-salvage-r9` / 测试 scratch worktree | 有 live 进程 cwd 落在里面（正在跑测试） | ⛔ **未碰** |
| `gopi-combo` / `/tmp/vb` | 有未提交/冲突改动（`UU cmd/agent/main.go` 等） | ⛔ **未碰**（脏目录一律不动） |

没有任何进程被杀过；全程只用 `/proc/*/cwd` 读判定持有者。

## 5. 未做 / 诚实边界

1. **旧行 `01a091f9-4e2b` 没有 rerun**（briefing 要求它保持 failed）；它那条滞留目录按
   残留处理（内容已在 master）。
2. **根因 C 的数据修复是手工的一次性订正**：我把本行（`01a09250`）被劫持的 deliverable
   改回自己的目录。代码层面已堵死再被劫持（不再改写已有绑定），但**历史上被劫持的其它行
   没有全量普查**——只确认了本行这一例（它就是 live 报错现场）。
3. **僵尸残留目录没有批量清理**：`~/workspace` 下还有 19 个非 worktree 残留目录
   （`gopi-go-tmp` 等测试产物）。它们不占 git 注册表、不阻塞 merge，属于另一个题（垃圾
   回收），本轮只清了和本 epic 直接相关的两个。
4. `sweep` 是**每 10 分钟一次、每次 cap 10 行**的节流通道：深滞留目录现在**会被**看到，
   但如果一次积压超过 cap，回收会跨多次 pass 完成（不是一轮清空）。这是有意的——cap 是
   防止一次 pass 拖爆 worker 的护栏。
5. `internal/tools` 包在本机负载 ~29 的情况下出现过一次**与本改动无关的** e2e 失败
   （`TestTaskRunChildSIGKILLClosesWrapperRowE2E`，master 与本分支各跑 3 次均通过，
   属负载敏感 flake，非本次回归）。
6. **线上二进制不含本次修复——这是结构性的，不是疏忽**：运行中的 worker 是
   `/home/user/work/GoPi/bin/gopi`（构建于 05:42:12，master `06a400bf`），
   `strings bin/gopi | grep` 对 `serveFinishCleanupStrandedIDs` / `finishCleanupOwnerRowLive`
   三个新符号**全部 ABSENT**。也就是说：本次修复**尚未在真实 worker 里跑过**，本轮
   观察到的所有成功回收都出自**我这条分支的二进制**（诊断运行）。链路要到 merge →
   Rule 12 的重建之后才算真正上线。**推论**：§4 表格里"已回收"的每一项都应当读作
   "在本分支二进制下被正确回收"，不能当作线上行为的证据。
7. **`zz_wtrecycle_diag_test.go` 是一条真实账本接口**：它的默认行为是 `t.Skip`（
   `go test` 全量运行时零成本、不碰数据），但一旦显式设 `GOPI_WTRECYCLE_LIVE_DIAG`，
   它跑的是**生产链路 + 生产账本**（`GOPI_MEMORY_DB` 未设时的默认库），并可能触发真实
   回收动作。这是我把判定建立在链路本身上的代价，写在明处：**请勿在非诊断场景设置该
   变量**。永久覆盖（不依赖它的）在 §3 的 WTRECYCLE 用例里。

8. **活跃 worktree 的"未碰"结论有时效性**：§4 的活跃判定是 06:0x 的一次快照。
   `~/workspace/gopi-wc` 就是活教材——它先是死任务的 worktree，随后被**重提任务的
   新实例**占用（行 `01a09254-148f` in_progress），期间还收到一条
   `deliverable.worktree backfilled to ".../gopi-wc"` 的通知。同样的目录在两小时内换了主人。
   所以"某目录无持有者"只对**采集那一刻**成立；清理动作前必须重新读一次 `/proc/*/cwd`。

## 6. 验证

```
gofmt -l internal/tools cmd/agent internal/standards   → 干净（无输出）
CGO_ENABLED=0 go build ./...                           → BUILD_OK
go vet ./internal/tools/ ./cmd/agent/                  → 无输出
go test -count=1 -timeout 30m ./internal/tools/ ./cmd/agent/ ./internal/standards/   （job-8 / job-16）
  → internal/tools   ok   749.6s（含全部新增/红证明用例）
  → cmd/agent        见下（唯一红 = 已 A/B 证明的既有缺陷）
  → internal/standards  本分支触及的两道闸门 ok（见下）；整包在 master 上就是红的
```

**`internal/standards` 的范围精确化（订正一处过度概括）**：早前那条"internal/standards
ok 8.9s（六道机器闸门）"是**整包**的说法，只在当时的工作区（尚未 rebase 到 `bbe60f7f`
的旧 base）成立。rebase 后整包 **FAIL**，但失败与本次改动无关——逐条实测：

| 闸门 | 状态 | 归因 |
|---|---|---|
| **Rule 18**（`else` after terminating `if`，本次唯一改动的闸门） | **绿** hits=5 baselined=5 NEW=0 | 本分支修掉 `taskRunBriefing` 的 else-after-return，并同步删掉 `baseline.go` 的过期许可条目 |
| Rule 16 / 17 / 19 / 20 / 21 | 与 `master` **逐字相同**的 FAIL | `cmd/agent/main.go:538`（`0bea9c0d`，**2026-08-31** 就有的 `%v`，保留 `%v` 是有意的：格式化 `payloadFilePrefix` 字符串）与 `internal/tools/memory_sync_claim.go:912`（`%v` + 行号从 `890` 位移到 `912`）两个 NEW 命中，加上 `cmd/agent/main.go:515` / `memory_sync_claim.go:890` 两条 STALE 条目 |

A/B 证据（不是推断）：`git worktree add --detach /tmp/gatecheck bbe60f7f` →
`go test -run 'TestStandardGates/TestGate1ErrorWrapRedProof' ./internal/standards/`
在**干净 master** 上逐字复现同样的四行（2 NEW + 2 STALE）——**与我的 HEAD 输出一字不差**。
且 `git diff --stat master...HEAD -- cmd/agent/main.go internal/tools/memory_sync_claim.go`
为**空**（两个文件完全相同），所以本分支不可能是成因。

收尾时又做了一次**逐行机器比对**（不是肉眼看）：把两侧闸门输出里的
`NEW violation` / `STALE baseline` 行各取一份、排序后 `diff`——
```bash
cd ~/workspace/gopi-wtrecycle2 && go test -count=1 -run 'TestStandardGates/TestGate1ErrorWrapRedProof' ./internal/standards/ 2>&1 \
  | grep -E 'NEW violation|STALE baseline' | sed 's/^ *//;s/ matches no violation.*//' | sort > /tmp/gate_mine.txt
cd ~/work/GoPi            && (同上)                                                                    > /tmp/gate_master.txt
diff /tmp/gate_mine.txt /tmp/gate_master.txt   →  空输出（IDENTICAL）
```
`diff` 退出码 0、无输出：四行（2 NEW + 2 STALE）**逐字相同**。这一条是与"我看了一眼觉得一样"
无关的机器判据。

> 为什么这不构成本次任务的交付缺口：这两处 `%v` 不是本次的修复范围（Rule 16 的
> 基线机制本身允许存量站点存在，只要求**棘轮向下**、且 STALE 条目必须随修复删除）。
> 它是 master 上另一条独立的、已由 `bbe60f7f` 命名的红。**留给收尾/merge 阶段处理**，
> 本次不掩盖、不"顺手绿过去"。已另立一条 bug 任务交给队列：
> `01a092b3-592e-7854-a10f-1611b05f8807`（tags=bug，payload 含复现命令 + 逐字输出 +
> 两处命中的归因线索 + 验收标准）。
**最新一次全绿运行（job-18，117s，提交 `092e1f8b` 之后）**：

```
gofmt -l internal cmd                               → GOFMT_CLEAN
CGO_ENABLED=0 go build ./...                        → BUILD_OK
go vet ./internal/tools/ ./cmd/agent/ ./internal/standards/ → VET_OK
go test -count=1 -timeout 30m -run 'Worktree|Stranded|MergedCleanup|FinishCleanup|Recycle|Deliverable' \
    ./internal/tools/ ./cmd/agent/
  → internal/tools  ok  67.440s
  → cmd/agent       ok  21.563s
TestStandardGateDataReport  → Rule 18 hits=5 baselined=5 NEW=0（绿）；Rule 16 NEW=2（= master，见上）
```

**红项的 A/B 归因（不是"大概是 flake"，是实测对照）**：cmd/agent 与 internal/agent
各有一条红，**都在本分支完全没碰的文件里**——

| 红项 | 本分支是否碰过该文件 | 干净 master `d9b3f6ad` 上复现？ |
|---|---|---|
| `TestMergeVerifyGatePassKeepsMerge`（cmd/agent） | 否（`merger_verify_e2e_test.go` 不在 diff） | **是**（39s，同一条 FAIL） |
| `TestCovCrHasSessionEnd_TrueOnlyForEndTail` / `…_GarbageTailAfterEnd`（internal/agent） | 否（`cov_agent_cr_test.go`、`cov_agent_cr.go` 不在 diff） | **是**（0.00s，同一条断言） |
| `TestStartupGate_SkipsTheWalkWhileTodayStamped` | 否（`crashdump_sweep_gate_test.go` 不在文件清单内） | 是（同批 A/B） |

对照方式：`git worktree add /tmp/ab-master <干净master sha> --detach` → 同一组 `-run` 在
**干净 master 检出**上跑 → 逐条复现同样的断言文本。`git diff --name-only master...HEAD`
对这三个测试文件是**空的**，所以本分支不可能是成因。**独立旁证**：master 上一个
salvage 收尾 commit（`bbe60f7f`，与本分支无关）同样写道 "round 10 — final acceptance
gate on master is RED and attributable to three post-salvage merges"，即 master 自己的
验收门当时就是红的——与本 A/B 结论一致。
（另注：`internal/tools` 单包在默认 10m 超时下会撞上**已有的** `TestTaskStampRun`
慢路径，表现为 600s FAIL——`-timeout 30m` 下同一包 ok 749.6s。这一条与本次改动无关，
也是 memory #1332/#1794 记录过的既有现象。）

**收尾自审（提交前各一次）**：`go vet` 无输出；两次
`git diff master...HEAD | grep '^+'` 扫描确认新增代码里**没有**状态字面量
（`"pending"`/`"completed"`/…）也没有 `os.Getenv("GOPI_…")` 裸字面量；测试里的
ledger 路径一律走 `memoryDBEnv`（Rule 1b/13/14）。扫描过程中**发现并修掉了三处自身缺陷**
（各自一个 commit，见 `git log`）：

1. `FinishCleanupSkipIsReportable` 有**两份**——`internal/tools` 的真身，和 `cmd/agent`
   里一份**没人调用**的副本（唯一调用点早已改用 `tools.` 那份）。同一谓词两个定义正是
   Rule 14 禁止的漂移源，删除副本。
2. 常量 `finishCleanupOwnerTerminalStatuses` 声明后**从未被引用**（终态集合的 policy 归
   `taskCleanupEligibleStatus`）——Rule 13 的旁路，删除。
3. 三个新/改测试文件把 env **名字**写成字符串字面量（`cmd/agent` 里还**重声明**了一份
   `memoryDBEnvKey`，与 `main_test.go` 的 `memoryDBEnv` 重复）——改为引用包内唯一常量。

## 7. 交付物声明（Rule 6d）

- 分支：`fix/worktree-recycle-2`（已 rebase 到当时 master，0 behind），commit 见 `git log --oneline -1 fix/worktree-recycle-2`
- 恢复命令：`git show fix/worktree-recycle-2:devdoc/worktree-recycle-fix-20260912.md`
- commit 已在**主仓库**可见（worktree 共享 refs，无需 push）：`git log --oneline -3 fix/worktree-recycle-2`
- **给验收方的一条提示**：`bash scripts/safe-merge.sh fix/worktree-recycle-2` 目前会以
  `[c] 分支仍被 worktree 占用` 拒绝——这是我仍在 `~/workspace/gopi-wtrecycle2` 里作业的
  正常状态，不是守卫发现的缺陷。验收通过后 `git worktree remove ~/workspace/gopi-wtrecycle2`
  即可解锁 merge（另两条 [a] 无存档 tag、[b] 无黑名单前缀均为 ✅）。

### 7.1 收尾追加

- 分支 `fix/worktree-recycle-2` 最终 **19 commits**，`git rev-list --count $(git merge-base HEAD master)..HEAD` = 19，0 behind master，`git status --porcelain` 为空。
  （该数字随每个 devdoc-only commit 自增一位——要权威数字就重跑上面那条命令。）
- Rule 16 归因增加了上文那段**排序后逐行 diff** 的机器判据（`diff` 退出 0、无输出）。
- 收尾复验 job-21（`gofmt -l internal cmd` / `CGO_ENABLED=0 go build ./...` / `go vet` / 30m 超时全量 `internal/tools`+`cmd/agent`）在 `3a1390a3` 上跑到 TEST 段时被**主动 kill**（gofmt/BUILD/VET 三段已 OK）——它是**冗余复跑**，不是"还没跑完"：
  同一 scope 的 job-18 在 `092e1f8b` 上已全绿（tools ok 67.4s / agent ok 21.6s），而
  `git diff --name-only 092e1f8b..HEAD` 只有 `devdoc/worktree-recycle-fix-20260912.md`
  ——**零代码改动**。`internal/tools` 全量在 30m 超时下要跑 ~750s，在代码逐字节相同的前提下
  再等一轮没有新信息（Rule: 最小样本 / 不做无信息量的重复劳动）。这条 diff 判据是**可复现**的，
  验收方一条命令即可复核。
