<!-- doc-class: historical -->
# finish-cleanup 的 settled 判据把「目录已消失」当成「已交付」

**症状（operator 提的行 01a12dad-3110-7edb-881b-c87ddeedf45a）**：一条**本地分支从未
推送到对端、对端无任何 ref** 的行，在 `gopi task finish-cleanup --dry-run` 里被渲染成
`would-run=0 settled=1`，于是看起来「无事可做」——而这件事（把分支交给对端）从未发生。

## 1. 判据写出点（实测定位）

`internal/tools/task_finish_cleanup.go`

| 位置 | 角色 |
|---|---|
| `FinishCleanupReportSettled`（原 :4715） | 分类器：`Skipped` + reason 里含记录目录 + `finishCleanupMissingDirReason(reason)` ⇒ **直接 `return true`** |
| `finishCleanupGoneWorktreeSkipMarker`（:822） | 那条 reason 的**头部字面文本**：`"removal skipped (delivery already attempted): the recorded worktree directory is not there — nothing to remove ("` |
| `finishCleanupGoneWorktreeSkipReason`（:853） ← `finishCleanupRemovalReason`（:878） | 该 reason 的**唯一写出点**：`git status` 在该目录上 chdir 失败（`chdir <dir>: no such file or directory`）时返回它 |

**为什么它能与「对端无 ref」并存**：这条 marker 的**措辞**在断言交付
（"delivery already attempted"），但它的**唯一依据**是「目录不在」。

- 该分支从未做过任何交付读取——既不 `ls-remote` 对端，也不看自己的 push 步骤；
- `--dry-run` 走 plan 支时，push 预测原本只在 `removeWhy != ""` 的**另一条**尾部路径追加，
  这条 gone-directory 路径直接 `return`（plan 只多出 ④ 的预测），**report 里没有任何
  步骤记录「交付待办」**；
- 于是分类器手上唯一的事实就是「目录不在」，它按字面读成「已交付过」，判定 settled；
- `settled` 又让 `--dry-run` 的 `would-run` 计数不增，`--apply` 也只多计数一次 settled。

**改前实测（真二节点 fixture，真实二进制，见 §5）**：

```
  01a12dad-e2e-strand completed   branch=task/e2e-strand worktree=…/gone-wt — removal skipped
      (delivery already attempted): the recorded worktree directory is not there — nothing to remove
      (git status: chdir …/gone-wt: no such file or directory)
finish-cleanup (dry-run): candidates=1 would-run=0 settled=1 refused=0 — pass --apply to run the chain
```

同一次运行里对端 `git for-each-ref` 只有 `refs/heads/master`——**分支在任何对端 ref 里都不存在**。

**顺带实测到一个反直觉事实**：同一条行在**未受阻**的两节点主机上，`--apply` 其实**会**
交付成功（本次实测 peer 拿到了 tip `b0fef665…`，`settled=1` 只是它的计数标签）。所以
settled 判词的真实危害是：**它把 dry-run 这个决策面读成「没有待办」**，并让
「交付被 `--align`/pre-merge 挡住」的 stranded 行（下面 §4 的 B 支）既拿不到交付、又永远
读作「已完成」，从而不再被任何一次扫描重试。

## 2. 最小修复（判据改为要求交付侧证据，不再从目录缺失推断）

1. **同一个 reason，两种读法**——`finishCleanupRemovalReasonWithDelivery`（唯一 composer，
   plan 与 apply 两支都调用，Rule 14）：branch 仍在本地且本 report 没有交付证据时，reason
   前缀 `finishCleanupDeliveryOwedSkipMarker = "delivery owed: "`，并附一句人可复核的注解
   （`finishCleanupGoneWorktreeDeliveryOwedNote`）。
2. **判据不再短路**：`FinishCleanupReportSettled` 的 gone-directory 支改为
   `finishCleanupDeliveryEvidenced(rep)`（与同文件 :5320 的 `finishCleanupStepSucceeded`
   同源：交付证据只从 report **自己的步骤**读 —— 执行过的 push 步；或「分支不存在」那一步
   记录的 no-op），**预测（"would …"）不算证据**。
   另加一条：reason 里写了「本机没有任何交付通道」（`single-node:` / 别名不可用）时仍读 settled
   —— 那里交付**不存在**，没有待办（保住 2026-09-16 那条「干净主机别读成卡死」的既有判词）。
3. **计划面渲染待办**：plan 支在 plan-only 阶段就为「分支仍在本地」追加 pre-merge + push 两条
   **预测**步（带 `would ` 标记），并在 `FinishCleanupPlan` 里把该行计为 `WouldRun=true`
   —— 于是 dry run 报 `would-run=1 settled=0`，并且能说清它要做什么。
   ⚠️ **改前实测更正（2026-10-13，见 §6）**：本次复核实跑的 stranded 行落在**更早的
   TERMINAL-STATE PROBE**（`git rev-parse --verify HEAD` 在消失目录上失败，reason 头部是
   `worktree unreadable`）——它在 composer 之前 `return`，因此**不会渲染以上两条预测、
   也不写 delivery 措辞**；该形状的判据落在 §6 那条 probe 臂上（无 peer ⇒ settled；
   有 peer ⇒ unsettled/refused）。本节第 3 点描述的是 2026-10-12 那次实测所在的**另一条
   分叉**（probe 之上的 gone-directory 臂），两条分叉今天**并存**。

4. **交付被挡的路径也带上同一措辞**：`finishCleanupNoteRemovalNotReached`（align/pre-merge/serve
   挡下交付时写 reason）同样经 composer，于是 §4-B 的 stranded 行也读作「交付待办」。

## 3. 回归测试（red-on-revert）

- 单测（既有分类器表）新增 4 条断言：无证据⇒**不** settled；有执行过的 push 步⇒settled；
  无交付通道⇒settled；reason 带 owed 措辞⇒即使有 push 步也不 settled。
- `TestFinishCleanupStrandedGoneWorktreeIsNotSettled`：真二节点 fixture（client repo + bare peer，
  测试内注入 target），目录已删、分支仍在、**peer 确认没有该 ref** ⇒ plan 不 settled、渲染带
  `would ` 标记的 push 预测、且预测不算交付证据。
- `TestFinishCleanupGoneWorktreeDeliveredPlanReadsSettled`：先真跑一遍链**交付并删掉 ref**，
  再走同一条 plan 路径 ⇒ 仍读 settled、且不带 owed 措辞（防过度收紧）。
- **red-on-revert 证据**：把 §2 的第 2/3 点回退（仅回退判据与 composer 调用）后重跑，
  新测试红，原始输出见
  `~/.gopi/sessions/01a12dad31107edb881bc87ddeedf45a/artifacts/red-on-revert.txt`。

## 4. 仍是既有的（不在本次修复范围，如实）

- **A：`--apply` 在健康的双节点主机上本来就会推**。因此「从未交付」的可达状态是「交付被挡」
  或「行从未被任何一次 close-out 扫到」，而不是「扫描到却跳过 push」。
- **B：交付侧被挡时，本步修复只让它读作 unsettled/would-run**——`--apply` **仍然不会重推**
  （push 在 align/pre-merge 之后，align 未过就 return）。要让这类行真的被自动补交付，需要
  一条独立的重试路径（既有的 ledger-keyed retry 思路，见记忆 #1817）。**本次不新增常驻/定时
  依赖（Rule 35）**：stderr 与 settle 判词已给出「交付待办」的可见信号，补交付是一条可独立
  派发的任务。

## 5. 真二进制端到端（Rule 31）

fixture：`/dev/shm/gopi-e2e-fix`（dev 27，off `/`），`peer/fixturepeer.git`（bare，扮演对端）
+ `client`（dev 分支 `task/e2e-strand`，已 commit）+ 账本里一条 `worktree` 指向**不存在**目录的
terminal 行；`GOPI_SYNC_PEERS=fixturepeer`，`PATH` 前置一个把 `<alias>:~/work/GoPi` 翻译到
本机 bare repo 的 `ssh` 垫片（gopi 是真的走 `git push <alias>:<path>`，只是把 ssh 换成本地）。

```
$ gopi task finish-cleanup --dry-run --task 01a12dad-e2e-strand
  01a12dad-e2e-strand completed   branch=task/e2e-strand worktree=…/gone-wt — delivery owed: removal
      skipped (delivery already attempted): … (git status: chdir …/gone-wt: no such file or directory)
      — the local branch IS still here — the delivery half has not run for this row, and the absent
      directory is not evidence that it did
finish-cleanup (dry-run): candidates=1 would-run=1 settled=0 refused=0

$ gopi task finish-cleanup --apply --task 01a12dad-e2e-strand
finish-cleanup (APPLY): candidates=1 removed=0 settled=1 refused=0
$ git --git-dir=…/peer/fixturepeer.git for-each-ref
86be8705…  refs/heads/master
dc8eaccb…  refs/heads/task/e2e-strand      ← 交付真的发生（=client 侧 tip）

# 再跑一遍（对端已有该 ref）：不重复动作
finish-cleanup (dry-run): candidates=1 would-run=0 settled=1 refused=0
finish-cleanup (APPLY):   candidates=1 removed=0 settled=1 refused=0    # peer tip 不变
```

改前同一 fixture 用修复前二进制（`/tmp/gopi-prefix`，由同一 worktree 回退 §2 的第 2/3 点构建）：
`would-run=0 settled=1 refused=0`，对端 `for-each-ref` 只有 master。

## 6. 复核（rework，2026-10-13）：上面 §2 只覆盖了一条分叉，probe 桶是另一条

本文档记录的修复是**同一判据的更早一代**：master 随后把它加宽（`c5b4cd360`），并在
**同一个 file:line 区域**改写了 `finishCleanupNoteRemovalNotReached`（master
`internal/tools/task_finish_cleanup.go:1587` 起，调用
`finishCleanupRemovalReasonWithDelivery(rep, removeWhy, finishCleanupGoneWorktreeDeliveryOwed(...))
+ finishCleanupRemovalNotReachedNote`），这就是它与 master 的**唯一冲突块**。冲突的裁决：
**master 的版本胜**，原因不是措辞新，而是它**覆盖两条入口**。

**本次复核实跑的 stranded 形状，落在这条链更早的一步**——`finishCleanupServerRecycle`
（同文件 :2150）的 TERMINAL-STATE PROBE：

```go
if _, err := finishCleanupGit(ctx, wt, "rev-parse", "--verify", "HEAD"); err != nil {
    rep.Skipped = true
    rep.SkipReason = finishCleanupUnreadableWorktreeMarker + ": " + err.Error()
    return rep, nil
}
```

该 `return` 在 `finishCleanupRemovalReason`（→ `finishCleanupRemovalReasonWithDelivery`，
:4601）**之前**，所以这条桶里的行：reason 里既没有 `delivery owed: `，也没有任何 delivery
措辞；plan 支也不会渲染 pre-merge / push 预测。判据落在 `FinishCleanupReportSettled` 里
`finishCleanupNoDeclaredPeer() && finishCleanupUnreadableGoneWorktreeReason(...)`
（`task_finish_cleanup.go:5915` / `:5902`）那**一条臂**上。

**逐字实测（本次 run 的会话目录 `artifacts/`，fixture `/dev/shm/gopi-e2e-settled`，dev 28 ≠ / 的 2051；同一行、同一个 fixture，只翻转 peer 声明）**：

```
$ GOPI_SYNC_PEERS=e2e-peer gopi task finish-cleanup --dry-run --task e2e-strand-row  # peer 已声明
  e2e-strand-row completed   branch=task/e2e-strand worktree=/dev/shm/…/gone-wt — worktree unreadable:
      git rev-parse --verify HEAD: chdir /dev/shm/…/gone-wt: no such file or directory: chdir …: no such file or directory
finish-cleanup (dry-run): candidates=1 would-run=0 settled=0 refused=1 — pass --apply to run the chain

$ GOPI_SYNC_PEERS= GOPI_SETTINGS=/dev/null gopi task finish-cleanup --dry-run --task e2e-strand-row  # 无 peer 声明
scratch:  reading /dev/null: config: failed to parse /dev/null: unexpected end of JSON input (children keep the ambient temp root)
  e2e-strand-row completed   branch=task/e2e-strand worktree=/dev/shm/…/gone-wt — worktree unreadable: …
finish-cleanup (dry-run): candidates=1 would-run=0 settled=1 refused=0 — pass --apply to run the chain
```

**结论（这是本次复核最重要的一条）**：master 在**真实动词**上的两条臂**按规则分岔** ——
`with a peer` ⇒ `settled=0 refused=1`（`FinishCleanupReportSettled` 不认这条 probe refusal
是 settled），`no peer` ⇒ `settled=1 refused=0`（§6 开头那条已裁定判词）。判据**不是**
从「目录消失」推交付：probe 桶的 reason 里连一个字都没有交付措辞，分类器是把
**peer 问题**单独问了一遍（`finishCleanupNoDeclaredPeer()`）才落判的。
**唯一留下来的差异是措辞层**：probe 桶的 reason 里没有 `delivery owed: `（该 token 只由
`finishCleanupRemovalReasonWithDelivery` 写，那条 composer 在 probe 之后才被走到），所以
operator 读这一行的**文案**时看不到「欠交付」二字，只有 settled/refused 的计数在说。
把它接上去需要把 probe refusal 送进 removal-reason 的词汇表（第二条入口），而那条路唯一的
出口是否定 §6 那条已裁定判据 —— 因此本次**不改**，如实记为残留（下文 §8 的证据链）。
本 run 未新增标记、未新增 peer 读取点、未动任何判据。

**审计（master 自己的词汇在哪个臂上已就位）**：`finishCleanupRemovalReasonWithDelivery`
（:1236）—— 一个 composer 里集成了 `finishCleanupDeliveryEvidenced`（:1343，只读
rep.Steps 的 push 步、`finishCleanupPushStepExecuted`（:1268）与
`finishCleanupPushStepRefused`（:1290），并把 plan 预测折价）；probe 桶（:2150）**是唯一
没被接进 composer 的 skip 形状**。要把待办信号接到它上面，需要一个把 probe refusal 变成
「removal reason」的入口，那是**第二个入口 / 第二条判据路径**（Rules 13/14 明确反对）——
且它本身与 §7-4 那条 `Settled=False WouldRun=True` 的已裁定判词**直接冲突**。

## 7. 逐函数评估（branch 的两个测试）

两个 branch-only 测试函数（`TestFinishCleanupStrandedGoneWorktreeIsNotSettled`、
`TestFinishCleanupGoneWorktreeDeliveredPlanReadsSettled`）**在 master 上逐字节存在**：

```
$ awk '/^\/\/ TestFinishCleanupStrandedGoneWorktreeIsNotSettled is THE regression/,
       /^\/\/ TestFinishCleanupGoneWorktreeIsReportedAsSettled is the live-shape/' \
       internal/tools/task_finish_cleanup_test.go | head -n -1 > master.txt
$ # branch 侧同段取自 01a12dad 的 diff（本次会话 attachments）
$ diff master.txt branch.txt    # 无输出，107 行对 107 行，逐字节相同
```

覆盖它们的 master 侧证据（引用原文）：

1. `internal/tools/task_finish_cleanup_test.go:2203-2266`（含注释头 2185 起） —— 两个函数体本身（含
   `planChainGone` :1854、`finishCleanupTestPeerAlias` 常量、注释「a gone directory
   whose branch never reached the peer must NOT read settled」）。
2. `internal/tools/task_finish_cleanup_test.go:2110-2146` —— 分类器表里那 4 条断言，
   包含 `finishCleanupDeliveryOwedSkipMarker`（= `"delivery owed: "`，
   `task_finish_cleanup.go:1179`）。
3. `internal/tools/task_finish_cleanup.go:1587`（master 改写后的
   `finishCleanupNoteRemovalNotReached`）与 :1236 的 composer、:1343 的证据阅读器。
4. `internal/tools/task_finish_cleanup_single_node_settled_test.go:36` ——
   `TestFinishCleanupGoneWorktreeProbeRefusalSingleNodeIsSettled` 的 ARM A（无 peer ⇒
   settled）与 ARM B（**有 peer ⇒ 必须不 settled**）把 §6 那条分叉的两面都钉住了。

**没有新的缺口**：这条链的 settled 语义（gone 目录 + 有交付证据 ⇒ settled；gone 目录 +
无证据 + 分支仍在 ⇒ 不 settled；无交付通道 ⇒ 交付不存在，settled）在 master 上由**一个**
分类器 + **一个** composer 完整表达，两个 branch-only 函数是它的测试而不是新语义。

## 8. 本次 run 留下的唯一生产改动：一句已经腐朽的注释

`internal/tools/task_finish_cleanup.go:4592-4600`（plan 支的 reason 组合处）残留了一段
**被改写后接坏的注释** —— 留着旧句的首行「The removal」再接新句，读起来是断句：

```
4592: // THE DELIVERY IS ASKED ABOUT, NOT INFERRED (Rule 41). The removal
4593: // The reason's
4594: // own head is a statement about the DIRECTORY, and the settled
```

它不改变任何行为（纯注释），但它是这次加宽留下的唯一残迹，且会让下一个读到这里的人把两代
推理缝错。本次 run 只修这三行（**不动任何判据、不动任何常量**），并在 §8 记录其证据链：
本 run 的初始 `9d8626d` 与改动后 `git diff` 只有这一段。

## 9. 本次文档处置

本文档在 master 上是 `historical` 类（`devdoc/DOC-INDEX.tsv:809`，
理由原文：「historical (2 standing / 0 record): the doc's own title/name declares a past
run's record, and no current artifact cites it as authority」），但它的 §2 与 §3 **读起来像
在陈述 master 的现状**，而 §2-3 描述的措辞（`rep.SkipReason = owed(removeWhy)）`) 已被
master 的 composer 取代、§3 的两个测试名 master 上确实存在（逐字节相同）。因此本次：
**保留该文档的实测记录，把它读起来像"当前设计"的两处改为指向 master 的权威实现，并补上
§6-§8 的复核结论**（本节即 supersede 的注记，处于
`devdoc/closeout-withheld-delivery.md` 的 lineage 上：后者记的是交付被**扣下**的另一半，
本文记的是 settled/refusal 判据本身）。
