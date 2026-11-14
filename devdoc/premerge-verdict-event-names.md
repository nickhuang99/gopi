<!-- doc-class: historical -->
# Task Report: premerge 车道的台账事件名由判决决定 —— 并补上此前不存在的肯定臂

**Row**: `01a12fa5-ed66-72a0-97d0-b4fdac9cdbe7`（epic `premerge-event-name-from-verdict`）
**Level**: 本 row 的 briefing 具名声明它属**系统层**（Rule 28 → peerbox），但它已被派发到
`hostb-Laptop` 且 worktree 已建。按"已派发的 row"执行，并在 `## Not Done` 如实写明这一层归属。

## Completed

### 1. `internal/tools/task_finish_premerge.go` —— 事件名由判决选，不再由子串猜

- **新增肯定臂常量** `finishCleanupPreMergeVerifiedEvent = "premerge-verified"`。
  改前词表里**只有** `premerge-refused` 与 `premerge-unverified`，而后者**从未被写过一行**
  （见 `## Evidence`：0 行）。所以"跑过并通过"在这个词表里**根本没有名字**。
- **`finishCleanupPreMergeReason(taskID string, res finishCleanupPreMergeResult)`** 取代
  `(taskID, detail string)`：事件名来自 `res.Status`，不再来自 `strings.Contains(detail, …)`。
- **`finishCleanupPreMergeEventName(status string) string`** —— 判决→名字映射的**唯一定义**
  （Rule 13/14），写入方与测试不可能各自漂移：
  `verified → premerge-verified`、`skipped → premerge-unverified`、`refused → premerge-refused`。
  **未知判决落 refusal 臂**：一个没人能归类的名字，绝不能读回成一个通过
  （Rule 15 —— 宁可少说，绝不多报）。
- **`finishCleanupPreMergeRefusedReason(taskID, detail)`**：给" withheld 投递但不是本门禁判决"
  的关闭步骤（ownership / completion / checklist）用，让这些调用点**陈述判决**而不是各自
  重打一个 result 字面量。它们继续用 `premerge-refused`，所以一条 grep 仍能找出所有被扣下的投递。
- **prune 集合纳入肯定臂**：`finishCleanupPreMergePruneTrail` 的 `IN (?, ?)` → `IN (?, ?, ?)`，
  否则肯定臂的行**只增不减**，而新旧两支共用同一个 `finishCleanupPreMergeEventKeep = 3` 上限。

### 2. 调用方传判决（9 处）

| 位置 | 改动 |
|---|---|
| `task_finish_cleanup.go:1949` | `finishCleanupPreMergeReason(rep.TaskID, sync)` —— 自带判决 |
| `task_finish_cleanup.go:1955` | 同上（abstention 臂） |
| `task_finish_cleanup.go:1968/1985/2007` | → `finishCleanupPreMergeRefusedReason(..., why+blocked)` |
| `task_finish_cleanup.go:4340` | `finishCleanupPreMergeReason(rep.TaskID, verify)`（主 premerge 车道，`!OK()` 臂） |
| `task_finish_cleanup.go:4348` | 同上（主 premerge 车道，通过臂） |
| `task_finish_docclass.go:201` | 传 `{Status: res.Status, Detail: msg}` |
| `task_finish_gendoc.go:365` | 传 `{Status: res.Status, Detail: …}` |

### 3. 肯定臂此前**从不落库** —— 这是缺陷被掩盖的另一半

改前主 premerge 车道只在 `Status == Skipped` 时调用 recorder，VERIFIED 直接静默通过。
于是哪怕把选名逻辑修对，`premerge-verified` 也**永远不会出现**：词表里"跑过并通过"这个名字
不但缺席，而且**没有任何路径会写它**。本次让**两个通过臂都落库**（VERIFIED 与 SKIPPED），
因为"没有记录"正是读者不得不去推断通过的原因 —— 而 204 行误标证明这种推断不可信
（Rule 15：事实要记录，不能靠"没有记录"反推）。

### 4. 测试（`internal/tools/task_finish_premerge_test.go`）

- `TestPreMergeAbstentionIsRecordedUnderItsOwnName` —— 钉住 **SKIPPED 臂**：端到端跑真实
  关闭链，断言台账事件名 `== premerge-unverified`、`!= premerge-refused`，并且该句在
  `premerge-refused` / `premerge-verified` 名下各出现 **0 次**（用 COUNT 在台账自己的语言里
  说反例），外加"弃权仍然投递"这一物理事实。
- `TestPreMergeVerifiedRunIsRecordedUnderItsOwnName` —— 钉住 **VERIFIED 臂**：
  `== premerge-verified`，且不在另两个名字下出现。
- `TestPreMergeVerdictNamesAreDistinctAndTotal` —— 映射本身：三判决三名互不相同，
  未归类判决永不落成 `premerge-verified`。
- 读取辅助 `preMergeLaneEvent` / `countLaneEvents` 按**本门禁自己的三个名字 + 该臂自己的句子**
  取行：同一名字族在**同一 row 上还被别的关闭车道写**（对齐车道、生成文档车道共用这个通道），
  不限定就会把它们的判决读成本门禁的。

### 5. 读取方核查（实测，不是推断）

epic 提到"读取方 `internal/tools/tasks.go:450` 把该名字当判决读回" —— 本 run 实测该处是
**注释**，不是读取逻辑；真正的读取在两个选择器里，结论与 epic 的措辞略有出入，如实记录：

| 位置 | 实测 | 对新旧名字的影响 |
|---|---|---|
| `tasks.go:450` | 注释，正文说 premerge 事件"不是 run digest"，是 `taskEventLatestDetail` 举例**跳过**的对象 | 无（注释，且本次未改） |
| `taskEventLatestDetail`（`tasks.go`） | 只收 `validTaskStatus(event)` 的名字 ⇒ `premerge-refused` / `premerge-verified` / `premerge-unverified` **全被跳过** | 三者都不是 status 名，行为不变 |
| `taskEventLatestVerdict`（`tasks.go`） | 只收 `taskShowVerdictEventName(name)`；`verdictTrailEventNames` 里**只有** `finish-cleanup-removal-refused` 与 `reframe` | `premerge-*` 不在此列 ⇒ `task show` 不渲染它；**本次改动不改变任何读取方行为**，只改变写入的名字 |

即：名字的正确性此前**没有被任何读取方兜住** —— 没有人因为名字错而报错，这正是 281 行
误标能活这么久的第三个原因（写入端默认 + 读取端跳过 + 没有测试钉住）。

## Not Done（如实）

- **历史 281 行未回填。** 台账是 append-only 的审计轨迹，把历史行的名字改掉就是篡改记录。
  本次只让**新**记录说真话；历史误标由本报告的读数留档。
- **未改 `cmd/agent`。** 无需要：本改动只在 `internal/tools` 内。
- **层次归属（Rule 28）。** briefing 声明本 row 属系统层（peerbox），实际派发到 `hostb-Laptop`
  （client，Rule 28 的 feature 侧）。我按已派发的 row 执行并在此具名写出：这份改动落在
  `internal/tools` 的关闭链，按 Rule 28 本应派发到 peerbox。可在 merge 前决定是否迁移。
- **Rule 31 的诚实边界。** 被改的 `finishCleanupPreMergeReason` **不是 CLI 动词**，没有独立的
  命令行入口；本 run 用的端到端调用是**真实的关闭链函数** `runFinishCleanupChainTarget`，
  跑在**真实的两节点 git 世界**（bare server + client + linked worktree）上、并把事件从
  **真实账本**读回。这比单测映射强，但**不等于**在真账本上跑一次 `gopi task finish-cleanup`——
  后者会往活账本写事件、污染运行历史，故未做。

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → `exit 0`；`CGO_ENABLED=0 go vet ./internal/tools/` → `exit 0`
- **Tests**: `CGO_ENABLED=0 go test ./internal/tools/ -run 'PreMerge' -count=1` → `ok github.com/nick/gopi/internal/tools 97.183s`（branch-scoped，覆盖本 diff 触及的包）
- **Ran**（verbatim）:
  ```
  CGO_ENABLED=0 go test ./internal/tools/ -run 'TestPreMergeAbstentionIsRecordedUnderItsOwnName|TestPreMergeVerifiedRunIsRecordedUnderItsOwnName|TestPreMergeVerdictNamesAreDistinctAndTotal' -count=1
  ```
- **Result**: 三个新测试全绿（`ok github.com/nick/gopi/internal/tools 1.907s`，`EXIT=0`）。
  红证见下（同一命令在回退逻辑下 RED）。
- **Commit**: 见本 commit 自身（`git log -1`）。

## Evidence (self-filed)

**(a) 改前台账读数** —— 本 run 自己跑的只读查询（`internal/tools` 的 Go 驱动，
`file:…/memory.db?mode=ro&_pragma=query_only(true)`，本机未装 sqlite3 CLI）：

```
=== premerge-family event names in the live ledger ===
  premerge-refused         281
  total premerge-family rows: 281
=== of the 'premerge-refused' rows: abstention vs named refusal cause ===
  premerge-refused total                              : 281
  ... detail says mergeability NOT verified (ABSTAIN) : 204
  ... detail names a real refusal cause (CONFLICT)    : 76
```

```
--- the OLD code's two substring arms: did EITHER ever fire? ---
  rows named premerge-unverified (the arm's only output)     : 0
  rows whose detail carries the FetchFail marker             : 0
  rows whose detail carries the NoMerge marker               : 0
--- the abstention reasons actually recorded under the refusal's name ---
  no merge ground  ('no worktree checked out')               : 204
  fetch failed     ('peer master unfetchable')               : 0
  no peer master   ('object is missing locally')             : 0
  kill switch / disabled                                     : 0
  no build contract (Rule 12)                                : 0
  genuine refusal: detail carries git's CONFLICT wording     : 76
```

读数结论（这比 briefing 里的数字更锋利）：**204/281 = 72.6% 是弃权**，
`premerge-unverified` **0 行**，旧代码的两个子串臂**各 0 次命中** ——
即 `event = finishCleanupPreMergeUnverifiedEvent` 那一段是**死代码**，
281 行**全部**落在默认的 `premerge-refused`。词表里三个事实（ran-passed / abstained /
never-ran）挤在一个名字下，其中"跑过并通过"在这个词表里**根本没有名字**。

**(b) 新测试的三臂断言** —— 见上 `Ran:` 命令，`EXIT=0`。

**(c) red-proof（实测，非声明）** —— 把选名逻辑**原样退回**改前形态
（`default REFUSED` + 对 `FetchFail`/`NoMerge` 两个 marker 做 `strings.Contains`），
保留全部符号、只换选名那一行：

```
RED-PROOF: selection reverted to pre-fix (default REFUSED + substring)
build ok with pre-fix logic
--- FAIL: TestPreMergeAbstentionIsRecordedUnderItsOwnName (0.35s)
    a SKIPPED verdict must be recorded as "premerge-unverified", got "premerge-refused"
      (detail "no worktree checked out on the delivered branch … mergeability NOT verified before push")
    an ABSTENTION must never be filed as a refusal — the branch was pushed, not withheld
    that abstention is filed under "premerge-refused" 1 time(s) — the name is a verdict it did NOT earn
--- FAIL: TestPreMergeVerifiedRunIsRecordedUnderItsOwnName (0.80s)
    a VERIFIED verdict must be recorded as "premerge-verified", got "premerge-refused"
    a verified run is filed under "premerge-refused" 1 time(s) — the ledger would read as a refusal
FAIL
RED_RUN_EXIT=1
```

红证**独立地复现了台账缺陷本身**：弃权被记成 `premerge-refused`。
恢复（`sha256sum` 与改动前一致）后：

```
13cbf1378965545eb4806e70c2b9b652a8327f060ae65a63bd67d24a0e8b1ca6  internal/tools/task_finish_premerge.go
BUILD_OK
ok  	github.com/nick/gopi/internal/tools	97.183s
EXIT=0
```

**(d) 分支范围回归** —— `go test ./internal/tools/ -run 'LocalSync|DocClass|Gendoc|GeneratedDoc|Deliverysync|TestFinishCleanup'`（同名字族的相邻车道，`finishCleanupPreMergeReason` 的另外 7 个调用点就在它们里），见本报告末的读数。
