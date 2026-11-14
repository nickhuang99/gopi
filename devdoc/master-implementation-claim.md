<!-- doc-class: normative -->
# master-implementation-claim — the criterion: 「master 已有 X」是一句可复算的测量，不是一句可以顺手断言的话

> **This is a normative statement, not a run's narrative.** 它的正文是**常设的判据**：
> 判据 `MasterClaimEvidenceAt` 活在 `internal/tools/master_claim.go`，文本半边活在
> `internal/taskreport/implclaim.go`，两者都由测试固定。§1 的案子与 §6 的更正只是它**为什么存在**。
>
> **ENFORCED**: `internal/tools/master_claim_test.go`, `internal/taskreport/implclaim_test.go`,
> `internal/tools/master_claim_report_test.go`（seal 的记录面）, `cmd/agent/cli_archive_verify_commit_test.go`.
> **The criterion MUST hold**: 一个 commit 是「master 已有 X」的证据，当且仅当它是集成分支的祖先；
> 成员关系由 `git merge-base --is-ancestor` 的退出码裁决，NOT by 文本匹配。

**Status:** implemented on `task/01a0f450a2e17aee983bb5566715c71b`
(worktree `~/workspace/gopi-01a0f450a2e17aee983bb5566715c71b`), commit at the bottom.
**Epic:** 一个被 `refs/tags/archive/*` 指向的 commit（或任何不是 `master` 祖先的 commit），
不得充当「master 上已有 X」的实现证据；这类声明在收尾时必须被**记录**，并附**可复算的观测**。

---

## 1. 实测的案子（session `01a0f3de`）

该会话的全部任务就是回答一个问题：**master 的重实现有没有漏掉分支 delta 里的东西？**

它在同一个 run 里做了两个互相矛盾的判断：

- 正确的一次观察（逐字来自
  `~/.gopi/sessions/01a0f3de-5b6e-74c3-8981-affbf6f27337/output.log`）：
  `The branch ref is gone from both. Only the attachment patch survives.`
- 错误的一次结论（同一份记录，逐字）：
  `Master's re-implementation is commit e1701ac8 "feat(capture): --own and --list-windows, …", which touched internal/shot/x11list.go.`

它把**被归档分支 `feat/capture-self-window` 的 tip** 当成了 master 的实现，据此判定
「master 已实现、无缺口」，并以 `exit=0` 收尾。

### 1.1 为什么这不是措辞问题，而是方向性错误

拿分支的 tip 当 master，等于**拿分支和自己比**——结论必然是「无缺口」，而这恰好是任务要证伪的命题。
真正在 master 上落地的实现是 `491b5ba7`（分支 delta 的 salvage），不是 `e1701ac8`（被 salvage 的源）。

### 1.2 为什么「命令成功」看起来像证据（本文件的机制核心）

```
$ git rev-parse refs/tags/archive/feat/capture-self-window^{commit}
e1701ac807f27b4a603e2e1dff45443768341cc5      ← 解析成功
$ git show e1701ac8:internal/shot/x11list.go   ← 也成功
```

**archive tag 让 commit 永远留在对象库里。** 于是对该 sha 的每一次读取都成功，
而**成功的命令读起来和证据一模一样**。区分「这个 commit 存在」与「这个 commit 在 master 上」
的唯一机械手段是**对象图**，不是「命令执行成功」这个事实。

### 1.3 判据（一句话）

> 一个 commit 是「master 已有 X」的证据，**当且仅当它是集成分支的祖先**。
> 成员关系由 `git merge-base --is-ancestor <c> <master>` 的**退出码**裁决
> （`0` = 祖先，`1` = 不是）；其余退出码是**错误**，既不是「是」也不是「不是」。

不是文本匹配，不是 tag 名字的模式匹配，不是「某个命令成功了」。

---

## 2. 建了什么（4 件，全部最小）

| 面 | 文件 | 职责 |
|---|---|---|
| 判据 | `internal/tools/master_claim.go` | peel → `merge-base --is-ancestor` 退出码 → `for-each-ref --points-at` 点名持有者；返回**观测**（命令行 + 退出码），不是一个 bool |
| 文本半边 | `internal/taskreport/implclaim.go`（leaf，stdlib only） | 从报告正文取出「master 已有 X」的句子 **和它声称的 commit**，产出待机械复核的 `(sentence, commit)` 对 |
| 收尾记录 | `internal/tools/task_report.go` + `internal/tools/master_claim_report.go` | seal 时对每个声称的 commit 跑判据；不支持 → 记入 append-only digest + stderr |
| CLI 面 | `cmd/agent/cli_archive.go` → `gopi archive --verify-commit <rev>` | **一条命令**即可证伪「X 在 master 上」 |

### 2.1 收尾的纪律，照抄既有先例（`gateclaim.go` / `runcheck.go`）

**RECORD，不重写，不拒绝。**

- 报告正文**逐字保留**——没有任何门会去改一个 run 自己写下的句子；
- 发现记在 **row 的 append-only digest**（`master-claim-unverified: …`）+ **stderr**（`[task] master-claim …`）；
- **不拒绝 seal**。理由是可测的、与 `rule31-ran-check.md` 同一条：本仓报告里大量**诚实测量**的句子，
  其主语是 seal 看不见的（在同胞 worktree 上测的、在对端仓库上测的、引用上游项目 master 的）。
  一个按文本就拒绝的门会把它们全部拒掉，而**对着诚实工作开火的门活不过一天**——
  被关掉的门比没有门更糟。

### 2.2 判据返回的是「观测」，不是「布尔」

```go
ev := tools.MasterClaimEvidenceAt(repo, "e1701ac8")
ev.OK()                  // 唯一的布尔
ev.NotOnIntegrationBranch()
ev.Observations()        // 3 行：peel / merge-base 退出码 / 持有该 commit 的 ref
ev.VerdictLine()         // 一行人读的结论，点名 archive tag
```

「repo 解析不出来」时判据 **NO-OP 并把这件事记上去**（`master-claim-unverified: unmeasured — …`），
绝不当成通过，也绝不当成对报告的指控。

---

## 3. 红转绿（判据用 git 退出码语义，不用文本）

`internal/tools/master_claim_test.go` 在**真 git 仓库**里建两侧历史：

- **红**：`:90 TestMasterClaim_ArchiveTagCommitIsNotMasterEvidence` —— 该 revision **能解析**
  （关于它的命令成功），判据仍然判 **NOT evidence**，且 `PointingRefs` 恰好是那个 archive tag
  （分支 ref 已消失，与 `01a0f3de` 的形状一致）。
- **绿**：`:132 TestMasterClaim_LandedCommitIsMasterEvidence` —— **同一个 revision**，
  一旦它的 commit 到达 master（fast-forward），判据翻成 OK。
  revision 的**拼写没变**，变的只有对象图——这正是判据机械的原因。
- `:182 TestMasterClaim_UnresolvedIsNotAPass` —— 解析不出来的 revision **不是**通过。

同一形状经 CLI 复验（见 §5）：`e1701ac8` → NOT（exit 1 点名 archive tag），`491b5ba7` → OK（exit 0）。

**红线证明（本 run 实测）**：判据的符号在 master 上**根本不存在**——
`git grep -l MasterClaimEvidenceAt master -- internal cmd` 无命中，
`git show master:internal/tools/master_claim.go` → `exists on disk, but not in 'master'`。
即这些测试在 master 上连编译都过不去，红是构造性的。

---

## 4. 端到端 seal 观测（本 run 实测，逐字）

```
$ go test ./internal/tools/ -run 'TestSealRecordsMasterClaimBackedByAnArchivedCommit' -count=1 -v
… master_claim_report_test.go:118: sealed digest fragment:
    master-claim-unverified: the listing feature is already implemented on `master` per
    `b9fcf5665478a5e40d3efc9cb99cd9aefd4a7285` [git merge-base --is-ancestor b9fcf566 master → exit=1 —
    not evidence that master implements it; held alive by refs/tags/archive/feat/master-claim-fixture]
[task] master-claim t-master-claim — in /dev/shm/suite-tools-run-…/001: the report says master already
has something, and the commit it names is not an ancestor of master
[task]   the listing feature is already implemented on `master` per `b9fcf566…` [git merge-base
--is-ancestor b9fcf566 master → exit=1 — not evidence that master implements it; held alive by
refs/tags/archive/feat/master-claim-fixture]
```

一份**含错误前提的报告正文** + 一个**真临时 git 仓库**（archive tag 指向非 master 祖先的 commit）
走完 seal：digest 片段与 stderr 记录都出现，报告正文未被改写。

---

## 5. 人为可跑的 CLI 面（本 run 实测，逐字）

```
$ ./bin/gopi archive --verify-commit e1701ac8
repo: /home/user/workspace/gopi-01a0f450a2e17aee983bb5566715c71b
integration branch: master
e1701ac8 (e1701ac8) is NOT an ancestor of master — NOT evidence that master implements it
(exit=1; held alive by refs/tags/archive/feat/capture-self-window)
  git rev-parse --verify --quiet e1701ac8^{commit} → e1701ac807f27b4a603e2e1dff45443768341cc5
  git merge-base --is-ancestor e1701ac8 master → exit=1
  git for-each-ref --points-at=e1701ac807f27b4a603e2e1dff45443768341cc5 --format=%(refname) → refs/tags/archive/feat/capture-self-window
$ echo $?
1

$ ./bin/gopi archive --verify-commit 491b5ba7
repo: /home/user/workspace/gopi-01a0f450a2e17aee983bb5566715c71b
integration branch: master
491b5ba7 (491b5ba7) IS an ancestor of master — usable as evidence that master implements it (exit=0)
  git rev-parse --verify --quiet 491b5ba7^{commit} → 491b5ba7d2d144b820a9326acca8cf4a5be1b430
  git merge-base --is-ancestor 491b5ba7 master → exit=0
  git for-each-ref --points-at=491b5ba7d2d144b820a9326acca8cf4a5be1b430 --format=%(refname) →
$ echo $?
0
```

`--verify-commit` 是**证伪模式**：它不写任何 ref、不改任何东西；它把「X 在 master 上」
变成一条**带退出码的观测**，直接可粘进报告的 `Ran:` 行。

---

## 6. 对 `01a0f44f` §7 的事实更正

epic 的交付物 6 要求本 devdoc 含「`01a0f44f` §7 的事实更正」。本 run 把它查到底，
结论是一句**必须说清楚的话**，而不是一个被含糊带过的句子：

**（a）§7 不在 `01a0f44f` 交付的产物里。**
本 run 实测（两个产物都读）：

```
$ grep -c "§7" ~/.gopi/sessions/01a0f44f-b130-7a77-805c-a81e86836d9a/task-report.md
0
$ git show master:devdoc/test-worktree-root-leak.md | grep -c "§7"
0
```

该报告的章节是 `## Completed / ## Not Done / ## Verification / ## Evidence (self-filed)`；
其 devdoc 是 `## 现象 / ## 根因 / ## 修法一 / ## 修法二 / ## 实测证据 / ## 反回归 / ## Not Done`。
**没有编号的 §7。** 所以「`01a0f44f` §7」这个指针在产物上**落不到任何一行**。

**（b）它指向的是本任务自己的 briefing 行文。**
本 run 读了本任务 row 的 payload（`~/.gopi/memory.db` 的 `tasks.payload`，逐字），
其 §1 开头即：

> **§1 更正**：我在同一次会话提交的 `01a0f44f-b130-7a77-805c-a81e86836d9a` 里，§7 有一条**错误论断**
> （"`--list-windows` 不在 master 上"）。

即：**是 payload 里引用了「`01a0f44f` §7」这个说法**，而那个说法在 `01a0f44f` 的产物里不存在。
（`01a0f44f` 的报告与 devdoc 都**一个字都没提** `list-windows`。）

**（c）payload 那条更正本身是对的、且被本 run 复算过。**
`--list-windows` **确实已经在 master 上**：

```
$ git ls-tree master --name-only internal/shot/x11list.go
internal/shot/x11list.go                                  exit=0
$ git grep -n "list-windows" master -- cmd/agent/cli_capture.go | head -1
master:cmd/agent/cli_capture.go:50:  gopi capture --list-windows   # what IS on this display (JSON)
$ git log --oneline -1 491b5ba7
491b5ba7 feat(capture): re-implement --list-windows on master (salvage of feat/capture-list-windows)
$ git merge-base --is-ancestor 491b5ba7 master; echo exit=$?
exit=0
```

所以本 devdoc 的事实更正是：**「`01a0f44f` §7」是一个悬空指针（dangling pointer）——
它既不在 `01a0f44f` 的报告里，也不在它的 devdoc 里；它只在引用它的那份 briefing 文本里。
而它所断言的 `--list-windows` 事实（已在 master 上，由 `491b5ba7` 落地）经本 run 复算是真的。**

顺带一条与本次判据同形的教训：payload 说「我在 §7 里写了」，**这句话是 prompt 层的断言，
不是产物里的观测**——它在产物里落空，正如 `01a0f3de` 的「master's re-implementation is e1701ac8」
在对象图里落空。**两处是同一个缺陷家族：一句话声称的东西，没有一条命令去核过。**

---

## 7. Not Done / 诚实的缺口

- **文本半边是保守的，不是完备的。** `implclaim.go` 识别的是「master 已有 X」这一族的句式 + 句子里的
  commit。一个用别的说法表达同一断言、或把 commit 写在隔壁句子的报告，会**漏过**。这是刻意的方向：
  漏记一句话好过误伤一句诚实的测量（§2.1 的实测理由）。
- **判据只对「本 run 自己的仓库」测量。** repo 解析不出来时它 NO-OP 并把「未能测量」记上。
  一个在**对端仓库**上做的比较，本判据不覆盖（也看不见）。
- **不收尾拒绝。** 不支持的声明不会阻止 `completed` 写回——见 §2.1 的实测理由。发现是**记录在案的**，
  不是**被拦下的**；把关仍然是复核者的判断。
- **`--verify-commit` 只回答「这个 commit 在不在 master 上」。** 它不回答「master 上这份实现是否完整」
  ——那是人的判断，一个机械检查若假装能做，就是在说谎。

---

## 8. 本任务自身的一次声称更正（同族的另一个样本）

本次 epic 由**两个 session** 执行。第一个 session 在它的工作区里写下过一条结论：
「CLI 已实跑 `./bin/gopi archive --verify-commit e1701ac8` → exit=1，点名
`refs/tags/archive/feat/capture-self-window`」。**当时的 `bin/gopi` 并不存在**（该 run 从未构建过它），
所以那条声称**没有对应的观测**。

本 run（第二个 session）把两件事分开做了：

1. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` 真正构建；
2. 再一条一条真跑，把**逐字输出**写进 §5。

**为什么值得写在这里**：它和 §1 是同一族缺陷——一句话声称的东西没有一条命令去核过。
区别只在位置：§1 是 LLM 把一个错的 commit 当成了实现证据，这里是 LLM 把一次没跑的
命令写成了已跑。**同一个机制（收尾时记录 + 要求可复算观测）对两者都适用。**
