<!-- doc-class: historical -->
# 「零提交 + report 说 BLOCKED」却写出 `completed` —— 两个矛盾事实出自同一趟收尾

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**行**：`01a186b9-064c-76c8-9d02-d4f9177e4361`（删光 `gopi task reopen`），2026-10-29。
**修复**：`internal/taskreport/declared.go` + `internal/tools/task_nothing_to_deliver.go`，
接在 `cmd/agent/cli_tasks.go` 的 `nothingToDeliver` 分支上。

## 现象

同一趟 close-out 写了两个互相矛盾的事实：

```
status:  completed
result:  clone channel: nothing to deliver — clone channel: /home/user/workspace/
         01a186b9-064c-76c8-9d02-d4f9177e4361 carries no commit to deliver:
         `rev-list --count HEAD --not origin/master` answered 0 against origin/master,
         so there is nothing to push, but origin/master carries 3 commit(s) this ground
         does not have — `pull --rebase origin master` recovers them; this refusal is
         about publishing, not about fetching. This is NOT a refused pull and NOT a
         conflict — do not chase the network (after 3 delivery attempt(s), the workspace
         /home/user/workspace/01a186b9-064c-76c8-9d02-d4f9177e4361 is RECLAIMED: it held
         no commit past master) — task-report: /home/user/.gopi/sessions/
         01a186b9-064c-76c8-9d02-d4f9177e4361/task-report.md (20820 bytes, missing
         sections: ## Files) — worker-output-log: /home/user/.gopi/sessions/
         01a186b9-064c-76c8-9d02-d4f9177e4361/output.log
verdict:        delivery proof for 01a186b9-064c-76c8-9d02-d4f9177e4361 (branch task/01a186b9064c76c89d02d4f9177e4361) NOT judged: the tip 05b7913416b1 existed BEFORE this run began, so the run produced NO commit of its own — there is no delivery to prove (05b7913416b1: author 2026-10-29T16:45:30+08:00, committer 2026-10-29T16:45:30+08:00; the run's in_progress_at 2026-10-29T20:16:58+08:00) — the question was never answered, which is not the same as an answer of "it did not land"
```

取回方式（两行都逐字来自它，未截断）：
`gopi task show 01a186b9-064c-76c8-9d02-d4f9177e4361 | grep -A1 '^result:'`

（完整的一行取回方式：`python3 -c "import sqlite3;
c=sqlite3.connect('file:/home/user/.gopi/memory.db?mode=ro',uri=True);
print(c.execute(\"select reason from tasks where task_id='01a186b9-064c-76c8-9d02-d4f9177e4361'\").fetchone()[0])"`
—— 中段的 channel 原话已在上面的代码块里逐字给出，`RECLAIMED` 之后接的就是
本条要引的那半句 `nothing can be delivered or compared`。）

而那次 run 自己的 `task-report.md` 第 3 行逐字写着：

```
**Verdict: BLOCKED** — 两条由 briefing 写死的硬前置在 master 上仍**双双未落地**，
briefing 明令「若任一未完成，本任务停下来，在报告里写明 blocked on <id>，不要自行动手」。
本轮**没有**删除任何代码、**没有**提交、**没有**移动 ref；工作树干净。
```

（原文第 1 行标题也写着 `本轮裁决 BLOCKED（前置仍未落地），交付物 = 实测取证 + 可执行删除计划`；
完整文件：`/home/user/.gopi/sessions/01a186b9-064c-76c8-9d02-d4f9177e4361/task-report.md`。）

任务目标零进展（`task reopen` 的 50 处引用与 `runTaskReopen` 入口全都还在），
ground 已回收，行读起来像做完了。

## 根因（file:line）

`cmd/agent/cli_tasks.go` 的 `nothingToDeliver` 分支：

```go
if nothingToDeliver {
        reason := cerr.Error()
        cloneDeliveryReason = reason
        taskRunConclusion = reason
        cerr = nil
}
```

然后是那唯一一次写回：

```go
summary := "gopi run completed" + stats
if cloneDeliveryReason != "" {
        summary = cloneDeliveryReason
}
wbErr := writebackOutcome(target, tools.TaskStatusCompleted, summary, opts.reportTo, opts.repo)
```

`TaskStatusCompleted` 是**字面量**，与 `cloneDeliveryReason` 无关：分支只把「空 ground」
换成了一句 reason，**没有换状态**。而 `tools.NothingToDeliverOutcome` 这一类判断
（把 `nothing to deliver` 的读数和 run 自己说了什么对齐）**根本不存在**。

三个已存在的臂都读过这块地，全都读不到这个形状：

| 臂 | 位置 | 为什么没抓住 |
|---|---|---|
| completion witness | `internal/tools/task_completion_witness.go` | `conclusion != ""` ⇒ 判 `concluded`，**设计上**把「有 channel 结论」当作健康形态；它明确写出「never turned back into `failed`」 |
| Rule 30 dirty gate | `internal/tools/tasks.go`（`reclaimRunTrackedDirtRefusal`） | 工作树**确实是干净的**（run 一个字都没改），gate 正确地不拦 |
| Rule 31 ran-check | `internal/taskreport/runcheck.go` | 判的是「报告有没有说真跑过 surface」，不是「报告说自己做完了没有」 |

也就是说：**没有任何一处读 report 的 `Verdict:` 行**，而 report 是唯一说真话的地方。

## 为什么不能简单地「empty ground ⇒ failed」

那正是 `01a18244` 修掉的缺陷：一次被要求**不产出改动**的反证/勘察 run（items=0），
它产出 0 提交是**正确的结论**，判成 `failed` 会让「查完了不用改」这类任务无法收尾。
区分两者的事实只有一条，而且报告里早就写着：**作者自己说没做完。**

所以本条加的是一个**读者**，不是一个新判据：

```
channel 量到空 ground + report 开 `Verdict:` 行声明没做完
        ⇒ 不写 completed；写 failed，理由里逐字引用两句话
channel 量到空 ground + report 没声明
        ⇒ 维持原样：completed，理由是 channel 的原话
```

## 改了哪两个文件

**`internal/taskreport/declared.go`（新，叶包）** —— `DeclaredIncomplete(body) (sentence, bool)`。
行锚定（行首，剥掉一层 list marker 与 Markdown 强调），词汇表
`BLOCKED / NOT DONE / NOT FINISHED / INCOMPLETE / UNFINISHED / ABORTED`，
整词边界（`BLOCKEDX` 不匹配），**保守方向**：没有 `Verdict:` 行就是「没声明」——
把「没写报告」读成「没做完」，会把本仓库每一份历史报告都判成失败。

**`internal/tools/task_nothing_to_deliver.go`（新）** —— `NothingToDeliverOutcome(taskID, channelReason) (status, summary)`，
唯一的判定点，返回状态与理由；声明为未完成时先写一条 trail 记录
（`delivery-nothing-to-deliver-declared`，用 `reclaimRunResidueRefusalLimit` 那个既有上限），
再返回 `failed` + `作者的原话 | channel 的原话`。

**`cmd/agent/cli_tasks.go`（改 6 行）** —— `nothingToDeliver` 分支里
`closeStatus, summary = tools.NothingToDeliverOutcome(target, cloneDeliveryReason)`，
`writebackOutcome` 收这个状态而不是字面量 `TaskStatusCompleted`。

## 红先绿后（原始输出）

红证：把 `if !declared { return TaskStatusCompleted, channelReason }` 临时改成
`if true { _ = declared; return TaskStatusCompleted, channelReason }`（恢复修复前行为）：

```
$ go test ./internal/tools/ -run 'TestNothingToDeliverClosesFailedWhenTheReportDeclaresIt'
--- FAIL: TestNothingToDeliverClosesFailedWhenTheReportDeclaresIt (0.00s)
    task_nothing_to_deliver_test.go:67: status = "completed" — a run whose own report declares
    it did not finish must not close "completed": the ledger would read as finished work while
    the report says the work was never done
FAIL
```

恢复（`sha256sum -c` 对照修复后的文件，不是 index 对照）：

```
$ sha256sum -c /tmp/red-proof-before.txt
internal/tools/task_nothing_to_deliver.go: OK
$ go test ./internal/tools/ -run 'TestNothingToDeliver'
ok  github.com/nick/gopi/internal/tools  0.450s
```

## 端到端（真跑，不是单测）

`cmd/agent/devworkspace_nothing_to_deliver_declared_e2e_test.go`：真 `gopi worker --once`，
agent 只写报告、零提交，ground 与 origin/master 齐平，channel 走真实的 empty-ground 出口。

```
$ go test ./cmd/agent/ -run 'TestCloneChannelNothingToDeliver' -v
--- PASS: TestCloneChannelNothingToDeliverButReportDeclaresFailed (12.64s)
--- PASS: TestCloneChannelNothingToDeliverClosesTheRowAsCompleted (2.35s)   ← 邻臂（01a18244 的反证形状）仍在
```

行的原话（`task show` 的 `reason:` / `result:`）：

```
failed: the run's own report declares it did not finish, so this close-out does not claim
`completed` — the report says: BLOCKED — upstream is not on master, so this run did nothing. |
clone channel: nothing to deliver — clone channel: /dev/shm/suite-cmd-agent-run-3624375205/
TestCloneChannelNothingToDeliverButReportDeclaresFailed3296563156/006/
01a186b9-0000-7000-8000-00000000e2e3 carries no commit to deliver:
`rev-list --count HEAD --not origin/master` answered 0 against origin/master, so there is nothing
to pull and nothing to push. This is NOT a refused pull and NOT a conflict — do not chase the
network (after 3 delivery attempt(s), the workspace /dev/shm/
suite-cmd-agent-run-3624375205/TestCloneChannelNothingToDeliverButReportDeclaresFailed3296563156/006/
01a186b9-0000-7000-8000-00000000e2e3 is RECLAIMED: it held no commit past master)
```

两个事实都在里面：作者自己的 `BLOCKED` 原话，和 channel 自己的空 ground 测量。

## 全量语料上的误判面（1175 份真实报告）

```
walked=1175 declared-incomplete=1
DECLARED 01a186b9-064c-76c8-9d02-d4f9177e4361 :: BLOCKED — 两条由 briefing 写死的硬前置在 master 上仍双双未落地，
```

**1 / 1175**：这个读者只认出那一份写了自己没做完的报告，其余 1174 份没有 `Verdict:`
行或写的是别的结论 —— 保守方向在真实语料上成立，不是靠 fixture 挑出来的。

## 同一批数据里量到的分布（为什么这条不是孤例）

只读打开 `~/.gopi/memory.db`，取所有带 `delivery-proof-unverified` 且
detail 含 `produced NO commit of its own` 的行（28 条），对每一行读它的
status 与它自己的 `task-report.md`：

```
25  completed  无 report
 1  failed     无 verdict token
 1  completed  无 verdict token
 1  completed  Verdict: BLOCKED          ← 就是本条
```

25 条无 report 的是本规则的**保守方向**要保护的样本：没有报告就没有声明，
不能判成失败。真正该拦的只有那 1 条，也正是修复前唯一漏掉的形状。
