<!-- doc-class: normative -->
# 程序不能检验自己 —— verify-closure 自验证门的删除判据

> 生效日期 2026-10-25。删除提交 `5f1049ae2`（`remove(task): 删掉 verify-closure 自验证门 —
> 程序不能检验自己`，9 文件，-1781/+118）。本文件不是"功能缺失记录"，而是**判据记录**：
> 它说明这段逻辑为什么不允许以任何形式回来，以及什么可以留下。

## 0. 操作员的裁定（原文）

> "verify 是由测试程序来做的，不是 gopi 内部的逻辑，一个程序不能自己检验自己"

裁定要落成可执行的判据，否则后人会把删除读成"功能没做完"，然后以"改进版"的名义加回来。

## 1. 判据：这段代码在替谁回答"这算不算通过"？

问一个问题就能分类，**答案决定它该不该存在**：

| 档 | 提问 | 谁在回答 | 例子 | 结论 |
|---|---|---|---|---|
| **A** | 这段代码判定**它自己所属程序**的产物合格吗？ | 程序对自己 | 一次 run 报告 `completed` 后，程序自己开一条"独立验证"行，再自己判它 "verify (independent)" 通过／失败 | **禁止**。这不是质量问题，是**权威的循环**：一个自证的结论不承载任何读者可以据以行动的信息 |
| **B** | 这段代码要求**人**（操作员）说清他观察到了什么吗？ | 人，向机器 | `gopi task update <id> --status verified --result "evidence: <命令> + 输出尾部"` 拒绝一条空证据的裁决 | **允许**（`TaskVerifyEvidenceArgs`）。它不判定真假，只拒绝裸断言 |
| **C** | 这段代码**跑一个测试程序/一条被测量的命令**，并把结果记下来吗？ | 测试程序 | `make test-budget PKGS=…`、`go test ./internal/standards/`；`gopi task verify` 作为**操作员动词**手工调用 | **允许**。这才是"验证"：外部仪器产出结果，程序只搬运与记录 |

**分界线不是"机器 vs 人"，是"谁是权威"**。A 档里程序既是被告又是法官；C 档里法官是
测试程序（它对本程序没有忠诚，也不因本程序的结论而改变），程序只是记录员。
**任何"自动开一条 verify 行"的挂点，无论包装得多好，都是 A 档。**

一条推论，写在这里免得被绕过：**"我们自己跑自己的测试套件"不属于 A 档**。gopi 的
`go test ./...` 是 C 档 —— 测试程序是外部仪器，gopi 不在其中判定自己的输出合格，
它只是运行它、读退出码、把结果写进报告。**判据问的是"谁在判定"，不是"谁被判定"。**
本行的验收本身就依赖这一点（`go test ./internal/standards/` 通过是允许的证据）。

## 2. 清单

### 2a. 已删（A 档，不得以任何形式回来）

| 对象 | 是什么 |
|---|---|
| `internal/tools/task_verify_closure.go`（727 行） | 门本体：`closureRowEligible` / `closureEnqueueOnce` / `TaskVerifyClosure`，在一次 run 写回 `completed` 后自动开一条 `verify (independent)` 行 |
| `internal/tools/task_verify_closure_test.go`（552 行） | 门的单元测试 |
| `internal/tools/task_verify_closure_defer_test.go`（280 行） | deferred-requeue 腿的测试 |
| `internal/tools/task_verify_closure_repo_test.go`（171 行） | repo 关联合成测试 |
| `internal/tools/tasks.go` 的门调用点 | `taskUpdateStampedCore` 尾部（终态写回之后）那一段"开 verify 行" |
| `cmd/agent/task_lifecycle_tick.go` 的 deferred requeue 腿 | 只为重测门自己写下的 `verify-closure-deferred` 而存在的清扫腿 —— 门没了就没有 deferral 可排空 |
| `SweepDeferredVerifyClosures` / `workerVerifyRequeueLegLabel` / `VerifyClosureRequeuePrefix` / `verify-closure-requeued` | 上述清扫腿的符号与事件名 |

**被这个提交删掉的全部函数（17 个 `-func` 行，逐个实测）：**

- **3 个被搬走（今天仍有定义）**：`taskHasTagToken` → `task_tag_token.go`、
  `TaskVerifyEvidenceArgs` / `TaskVerifyEvidenceError` → `task_verify_evidence.go`；
- **14 个真消失（今天 `grep -rn "^func <sym>(" --include=*.go .` = 0 定义，
  `\b<sym>\b` 引用也为 0）**：`isAutoDispatchedRun`、`closureGateFires`、
  `closureRowEligible`、`closureAlreadyQueued`、`inFlightClosureCount`、
  `enqueueVerifyClosure`、`taskVerifyClosure`、`closureEnqueueOnce`、
  `deferredClosureTaskIDs`、`SweepDeferredVerifyClosures`、`closureDeferralAgeSeconds`、
  `deliverableRepo`、`repoDeliverable`、`taskClosureDefaultRepo`。

**一个同音陷阱，写在这里免得下一个人当成残留**：`deliverableRepo` 现在还有 **4 处**
命中，全部在 `internal/tools/task_merge_backlog_terminal_guard_test.go` —— 那是测试辅助
函数 `d2SeedRow` 的一个**参数名**（`deliverableRepo string`），与被删的同名函数毫无关系。
**判残留要读命中行的上下文，不能只数命中数。**

**这是删除，不是关掉的开关**：没有 flag 能重新武装它（`task_lifecycle_tick.go:319-330`
原地写明了理由）。一个后来人能拨回去的休眠开关，是同一种缺陷换了个更长的引信。

原位保留的**理由段落**（读的时候不要误认为死注释）：`internal/tools/tasks.go`
`taskUpdateStampedCore` 尾部、`cmd/agent/task_lifecycle_tick.go` 第四腿之前。
它们解释"这里曾经有什么、为什么不能再有"，并且**明确写下删除的代价**，防止有人
把它当"回归"来修：

> 一次修复现在可以走到 `completed` 而**没有**任何机器生成的后续行要求有人去看它。
> 这就是重点 —— 证据在行自己的报告里（`Ran:` / `Result:`），证明在测试套件里。

### 2b. 保留（B / C 档，删除时必须识别出来）

| 对象 | 档 | 为什么留 |
|---|---|---|
| `internal/tools/task_tag_token.go` — `taskHasTagToken` | 通用工具 | **它从来不属于那个机制**。原本寄生在 `task_verify_closure.go` 里，但它是"整 token 匹配逗号分隔标签表"的通用判据（`delivery-chain` 不该命中 `chain`，`bug` 不该被 `debug` 点亮），被 6 个文件引用（实测 17 处 grep 命中，其中 12 处是调用点）。删机制文件时把它一起带走 = build 红（见 §4） |
| `internal/tools/task_evidence_digest.go` — `TaskVerifyEvidenceArgs` / `TaskVerifyEvidenceError` | B 档 | 操作员动词的**证据门**：`--status verified` 必须带 `evidence: …` 或 `reason: …`，否则拒绝。它只拒绝裸断言，**不判定真假、不开行、不调度验证者**。文件头已写明"这不是被删的 verified-closure 门" |
| `internal/tools/tasks.go` — `TaskVerify` | C 档 | `gopi task verify` 是**操作员动词**（人要求做一次检查），唯一生产入口是 `cmd/agent/cli_tasks.go:5487` `runTaskVerify`（分发点 5487/5491/6556）。**它没有自动触发器，而且不许长出触发器** |
| ~~`internal/tools/tasks.go` — `TaskVerifyToReopen`~~ **DELETED 2026-10-30，连同 `gopi task reopen` 整个动词** | C 档（记录当时的裁决）（**且已无可达生产调用者**） | 它是 `TaskReopenWithOptions{TaskID, Reason}` 的**零逻辑包装**（"REISSUES rework"）。实测：`grep -rn "TaskVerifyToReopen" --include=*.go . \| grep -v _test.go` 只命中它自己的定义处与两行注释 —— **生产调用者为 0**；`gopi task reopen` 走的是 `runTaskReopen` → `TaskReopenWithOptions`，不经过它。它不自动开行、不判定任何东西，所以**不是被删机制的残留**；它是一个可清理的测试专属别名，**本行不动它**（删它属于另一件事，且会改到 8 个测试文件的调用面）。列在这里是为了如实交代：§2b 说"保留"时，这一条的保留理由只是"它不是 A 档"，不是"它在跑" |
| 测试套件（`go test` / `make test-budget`） | C 档 | 外部仪器。见 §1 推论 |

## 3. 实测症状（为什么它必须被删）

账本实测（`gopi task list` 两个查询 + `memory.db` 以 `mode=ro&query_only(true)` 只读查询，
原始输出落在 `~/.gopi/sessions/01a1701f-…/evidence/`）：

**生命周期内共 21 条 `verify (independent…)` 行：20 条 `cancelled`，1 条 `completed`**
（`t-1788524560`，2026-09-04，早期原型手工跑通的那一次；`task_id` 还是旧的 `t-` 序号形式）。
`pending` / `failed` / `verified` 各 0 条。

关键在于**它们被撤销了三次，来自三个人/三个时刻** —— 没人把它们当有效工作消费，
每一次都是有人发现队列被自检垃圾占满后手工清掉：

| 撤销时刻 | 条数 | created 范围 | 说明 |
|---|---|---|---|
| 2026-10-24 08:11:37 – 08:12:17 | **12** | 10-23 22:59 – 10-24 06:54 | 第一次清场，一天内积累的 |
| 2026-10-25 04:17:09 | 1 | 10-24 20:04 | 单条 |
| 2026-10-25 06:43:22 – 06:43:28 | **7** | 10-25 05:52 – 06:04 | 删除提交 `5f1049ae2`（06:45:04）前 2 分钟 |
| 合计 cancelled | **20** | | |

**队列占比（实测，撤销时刻 T = 2026-10-25 06:43:21，即那 7 条被撤销的前 1 秒）：**

- 仍处于打开状态的 verify 行：**7 条**（created ≤ T 且未在 T 之前收尾的那一批）；
- 非 verify 的 pending 行（created ≤ T 且今天仍是 pending）：**11 条**；
- → T 时刻可见队列 **18 行，其中 7 行 = 38%（7/18）是自检垃圾**。

  这个分母是**下界近似**：T 到今天的这段时间里有非 verify 行被关闭，它们当时也在队列里，
  已无法从当前状态复原（要精确复原得读 `task_events` 的每一次状态迁移）。方向是明确的 ——
  真实噪声占比只会更高，不会更低。

- **删除之后（`created_at > 1792881904` = `06:45:04`）新建 verify 行：0 条。**
  门的移除是有效的，没有第二条路径还在造它们。
- 真实 CLI（`gopi task list --status pending`，13 行）：
  `grep -c "verify (independent"` = **0**。

**两处必须诚实的修正**（原 briefing 与初稿里的说法都是错的）：

1. **"verify 行把真 bug `01a16fef` 挤到后面"——优先级反转不成立。** 21 条 verify 行
   `priority` 全是 **5**，而 `01a16fef` 是 **priority 1**。`[next]` 停在 `01a16d21`
   是 priority-1 组内的先来先得（created 10-24 16:58 早于 10-25 06:02），与 verify 行无关。
   所以上面的 38% 说的是**队列体积**和**worker 回合被占用**（每条 verify 行都要求一个
   执行会话去"验证"一件没发生的事），**不是**优先级挤压。
2. 初稿写"20 条全 cancelled"，实测是 **21 条 / 20 cancelled + 1 completed**。
   那 1 条 completed 是 2026-09-04 的原型，早于自动化挂点，不影响判据，但数字要准。

**判据不依赖这个说法**：A 档被禁是因为权威循环（§1），不是因为队列副作用 ——
副作用只是它为什么在 2026-10-25 被发现、并被**撤销了三次**。

## 4. 踩过的坑：通用工具和机制住同一个文件

删除时按文件删，`taskHasTagToken` 跟着 `task_verify_closure.go` 一起没了，
**build 立刻红**：同一次编辑里，5 个文件的 15 处引用同时断掉。

**实测（父提交 `5f1049ae2^` 上 `git grep -c taskHasTagToken`）：**

| 文件 | 命中 | 命运 |
|---|---|---|
| `internal/tools/task_verify_closure.go` | 6 | 随机制消失（其中 1 处是定义） |
| `internal/tools/task_verify_closure_test.go` | 4 | 随机制的测试消失 |
| `internal/tools/tasks.go` | 9 | **必须活下来** |
| `internal/tools/task_rework_successor.go` | 2 | **必须活下来** |
| `internal/tools/task_rework_parent_gate_test.go` | 2 | **必须活下来** |
| `internal/tools/rework_reparent_children.go` | 1 | **必须活下来** |
| `internal/tools/task_transfer_eligibility.go` | 1 | **必须活下来** |
| 合计 | **25** | 其中 **15 处 / 5 个文件是通用引用** |

**修复的形状是正确的，值得写下来**：函数被**原样搬到** `task_tag_token.go`（连同它的
注释、注明出处），而且**搬运在同一个提交里完成** —— `5f1049ae2` 的 stat 里
`internal/tools/task_tag_token.go` 是 `+27` 新增，与 `task_verify_closure.go` 的 −727 同一次
落地。所以 **build 红只发生在编写过程中，没有落进 master**。给每个调用点重写一份是
错的解法（Rules 13/14：一处定义，处处引用）。

**教训 —— 而且这条教训当时并没有被写进 `PROJECT.md`。** 本行原来的 briefing 说
"这个坑已被写进 PROJECT.md Rule 41 的上下文里（删文件前确认里面哪些是通用工具）"。
**实测不成立**：`grep -n "通用工具\|taskHasTagToken\|删文件前" PROJECT.md` 无命中，
Rule 41 全文（`sed -n '/^## Rule 41/,/^## Rule 42/p'`）讲的是"发现异常即取证即提 bug"，
一个字都没提删文件与通用工具的分辨。**这条教训此前只活在这次删除的作者心里，没有落盘。**
本文件就是它第一次落盘的地方 —— 这也正是本行存在的理由：如果连"为什么删"都没写下来，
"删文件前先分辨通用工具"更不会有人记得。

可执行版本（一个文件里"机制"与"通用工具"的边界不由文件名决定）：

```bash
# 这个文件里定义的每个符号，还有谁在用？
git grep -n "<symbol>" HEAD -- '*.go' | grep -v "<the-file>"
```

- 只有本文件 / 本机制的测试在用的符号 → 随机制一起删；
- **跨文件、跨层被引用的符号 → 它是通用工具，先搬走再删**，并且搬运要在**同一个
  提交**里完成，否则 `go build ./...` 会红（这次就是红了才发现）。

（另按 Rule 41 提了一行：`01a1707f-8ae2-7df6-9584-10701f2f638c` —— PROJECT.md Rule 41 缺这条教训。）

## 5. 残留与证伪（本行实测）

| 检查 | 结果 |
|---|---|
| `ls internal/tools/task_verify_closure*.go` | `No such file or directory` |
| `grep -rn "closureRowEligible\|closureEnqueueOnce\|SweepDeferredVerifyClosures\|TaskVerifyClosure\|verifyClosure\|taskVerifyClosure\|workerVerifyRequeueLegLabel" --include=*.go .` | **0 命中**（活代码路径为零） |
| 上面 §2a 列的 17 个被删函数，逐个 `grep -rn "^func <sym>(" --include=*.go .` + `\b<sym>\b` | **3 个搬走（各 1 定义：`taskHasTagToken` / `TaskVerifyEvidenceArgs` / `TaskVerifyEvidenceError`）**；**14 个 0 定义 0 引用**；`deliverableRepo` 的 4 处命中是测试参数名（同音，见 §2a） |
| `VerifyClosureRequeuePrefix` / `verify-closure-requeue` | **0 命中**（含非 `.go` 文件） |
| `grep -rn "verify-closure\|VerifyClosure" --include=*.go .` | 只剩**注释**：`tasks.go:4719` 与 `tasks.go:4866`（历史说明 + 指回本文件）、`task_lifecycle_tick.go:319,325`（删除理由 + 事件名 `verify-closure-deferred` 作为"已不存在"的引用）。**无一行活代码**。（`task_tag_token.go` 不含这个连字符串；它提到的是文件名 `task_verify_closure.go`。） |
| `git merge-base --is-ancestor 5f1049ae2 HEAD` | exit 0（在祖先链上） |
| `taskHasTagToken` 引用 | 17 处 grep 命中 / 6 文件 |
| `TaskVerifyEvidenceArgs` | 5 处，含 `cmd/agent/cli_tasks.go:6724` 的活调用 |
| `go build ./...` | exit 0，静默 |
| **还有谁能造出 verify 行吗？** `grep -rn "verify (independent" --include=*.go .` | **0 命中 —— 生产点为零**。不是"门被关掉"，是**没有任何代码再能写下这个 summary** |
| `gopi task list --status pending \| grep -c "verify (independent"` | `0` |
| `go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader` | PASS（本文件第 1 行是合法 doc-class 头） |

**一个诚实的边界**：本文件不能、也没有试图证明"以后没人会加回来"。能保证这件事的
只有上面那个判据（§1）被下一个人读到 —— 这是它被写成 normative 类文档、而不是
写进某次任务的报告的原因。

### 5a. 删除提交漏掉的一处残留（本行实测，已修）

`5f1049ae2` 删掉了 `internal/tools/task_verify_closure_test.go`，但**没有清理
`testdata/heavy-tests.tsv` 里指向它的两条行**，于是 master 上
`go test ./internal/standards/ -run TestHeavyTestInventoryMatchesItsCallSites` **是红的**：

```
heavy_inventory_gate_test.go:138: stale row in testdata/heavy-tests.tsv:
  internal/tools/task_verify_closure_test.go TestVerifyClosureGateIgnoresNonBugAndHumanRuns
  (CriterionDiskFixture) — the test was renamed, moved or deleted; delete the row so
  the inventory keeps ratcheting with the code
heavy_inventory_gate_test.go:138: stale row in testdata/heavy-tests.tsv:
  internal/tools/task_verify_closure_test.go TestVerifyClosureGateStarvationGuard
  (CriterionDiskFixture) — ...
```

**这是"删除机制"最容易被漏掉的那一类残留**：不是代码引用，而是**清单里的一行数据**。
编译器不会因为它报错，`grep` 一个符号名也找不到它 —— 只有那条门会。教训的实际含义是：
**清单（inventory / baseline / TSV）也是删除范围的一部分**，它按 `文件路径 + 测试名`
记账，而这两个东西正是删除会改的。

门自己给出的指令就是"删掉该行，让清单随代码只降不升"。实测这两条测试确已不存在
（`grep -rn "TestVerifyClosureGateStarvationGuard\|TestVerifyClosureGateIgnoresNonBugAndHumanRuns"
--include=*.go .` → 0 命中），且清单里指向已删文件的**只有**这两行（其余行逐一 `[ -f ]` 验过）。
删两行 → 门红转绿。**这只是清单与代码对齐，不恢复任何自验证逻辑。**

## 相关

- 本行（`01a1701f`）是**验证+记录行**：逐层复跑取证后确认删除范围（代码 / 通用工具 /
  数据清单）在 master 上已全部对齐。取证的原始输出归档在
  `~/.gopi/sessions/01a1701f-1bbf-786d-b1fd-aaf646926163/evidence.txt`。
- **一个与删除无关的 master 红，写在这里免得下一个跑验收的人误判**（**2026-10-25 的观测，
  该 bug 行修完后此条自然过期**）：干净 master 上
  `go test ./internal/stylegate/ -run 'TestStyleGate$'` 是红的（`cmd/agent/cli_web_shot_test.go:71,80`
  的 `unchecked-close` ×2、`cmd/agent/web_voice_test.go:164` 的 `test-sleep` ×1），
  连带把 `internal/tools` 的合并基线门 `TestStylegateRatchetSuiteIsGreenOnThisTree` 带红。
  两处违规的来源提交 `4ce71e2`(09:00) / `53619d9`(09:22) 都**晚于**本删除提交 `5f1049ae2`(06:45)，
  且删除提交从未碰过那两个文件 —— **不是删除造成的**，已提行 `01a1738c-1007-7708-8cff-6a27bfa153dc`。
- `PROJECT.md` Rule 41（发现异常即取证即提 bug）—— **注意**：这条规则里**没有**
  "删文件前分辨通用工具"的教训（§4 实测），已按 Rule 41 提行
  `01a1707f-8ae2-7df6-9584-10701f2f638c` 要求补进去；补写落地前，本文件 §4 是它唯一的文字记录。
- `PROJECT.md` Rule 31（没跑过的命令不算交付）—— 本行的验收全部是实测命令 + 原始输出。
- `PROJECT.md` Rule 36 —— 本文件的 doc-class 头。
