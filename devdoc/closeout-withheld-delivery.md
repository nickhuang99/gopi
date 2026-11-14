<!-- doc-class: historical -->
# 交付被扣下 —— 无声的那一半（close-out 主仓门禁）

**Epic:** `01a12d5b-bb3a-7000-a1bfc4b9766574dc`
**分支:** `task/01a12d5bbb3a7000a1bfc4b9766574dc`
**树:** `/home/user/workspace/gopi-01a12d5bbb3a7000a1bfc4b9766574dc`
**日期:** 2026-10-12 · **主机:** hostb-Laptop（client→peerbox）

## 一句话

close-out 链路里 **唯一会扣下整个交付物** 的那道门禁（MAIN checkout 脏 →
`finishCleanupMainRepoGate`）此前**一个字都没写进行的账本**；修复让它在行自己的
trail 上留下**可区分**的一句话：`the DELIVERY was withheld (the branch did NOT
reach the peer)`，并且这句话在 `gopi task list` 的 60 字裁剪之内可见。

## 病灶（实测）

链路顺序（`internal/tools/task_finish_cleanup.go`）：

```
taskFinishCleanup
 ├─ ①a  FinishCleanupRestoreMainCheckout        （把 MAIN checkout 停到集成分支）
 ├─ ①b  finishCleanupMainRepoGate …  ← 门禁：MAIN 脏 / HEAD 不在集成分支 → 整链停
 ├─ 模式分叉：IsServerNode() ? finishCleanupServerRecycle : taskFinishCleanupChain
 │      └─ runFinishCleanupChainTarget → push（把分支交给 peer）→ worktree 回收 → branch -d
 └─ 移除臂：拒绝时 finishCleanupDeliveryRefusedReason(...)  ← 唯一的 trail 写点
```

`rep.Skipped` 的**每一个其他出口**都会通过共享记录器写行 trail（移除被拒、目录
已不在、外来绑定）;**扣下交付**这个出口什么都没写 —— 而它恰恰是唯一让整条分支留在
本机的出口。

实测（本机，2026-10-12）：

```
$ for b in $(git for-each-ref --format='%(refname:short)' refs/heads/task/); do
    n=$(git ls-remote --heads peerbox "$b" | wc -l); [ "$n" = 0 ] && echo "peer-ref=0 $b"; done
peer-ref=0 task/01a12d22dac07bdea9f895926cdc64ea
peer-ref=0 task/01a12d2ca3a67c5db2a1fa03081027d5
peer-ref=0 task/01a12d2f2b3f7e66862e144a4ef6b9a2
peer-ref=0 task/01a12d30799b775b8c97f691e282d92c
peer-ref=0 task/01a12d32819d7b6284643e956ba09d63
peer-ref=0 task/01a12d3e81c17028bddb24143aa4a338
peer-ref=0 task/01a12d3fbe40760497bd3ab7394cd78a
peer-ref=0 task/01a12d470b6c78e2ab63247c5be1d25d
peer-ref=0 task/01a12d4735be72249e25c96ab86b22d9
peer-ref=0 task/01a12d4cac777cc7b99cbc5078d74c27
```

**8 条**（后续会更多）`task/*` 分支在 peer 上零 ref，而它们的行全都读作
`completed`。实例行 `01a12d3f-be40-7604-97bd-3ab7394cd78a`：其修复 `3771ebc93`
全仓只有一个 ref 指向（本机的 task 分支），peer 上没有。

行里残留的痕迹（修复前）：

```
$ gopi task show 01a12d3f-… | grep -E '^(status|result|verdict):'
status:   completed
result:   gopi run completed — run: elapsed=3m38s — task-report: …
verdict:  verdict=DOABLE items=0 origin=…
```

`finish-cleanup-*` 事件：**0 条**。`gopi task list` 那一行：**没有任何 tail
marker**。也就是说，"这条分支从未离开本机"这件事，在任何**持久的、一眼可见的**面上
都不存在 —— 只在那个已经结束的 run 的 stderr 里。

## 为什么这是"可区分性"问题，而不只是"少了个日志"

链路里**已经有**一个被拒绝状态的渲染：`[closeout-refused]`
（`internal/tools/task_list_closeout_signal.go`，2026-10-11 的 42 行滞留事件）。
但它 key 的是**同一个** event name，而那个 name 由**两个** outcome 写入：

| outcome | 分支在哪 | 呈现 |
|---|---|---|
| 移除被拒（目录还在 / 脏 worktree） | **已过线到 peer** | `[closeout-refused]: removal skipped (delivery already attempted)…` |
| **交付被扣下（MAIN 脏）** | **从未过线** | 修复前：**什么都不写** |

本机实测：49/49 个带 marker 的行携带的是**第一种**原因。因此修复前 operator 无论看
list 还是 show，都无法把"我的分支没交出去"从"我的分支交出去了、只是本地目录没清"
里分出来。这就是 epic 要的可区分性。

## 修复（最小、无声的两个面都覆盖）

改动只在**已经在跑的代码**里（Rule 35：无脚本、无定时器、无新进程、无新配置键）：

1. **`finishCleanupMainRepoGate` 的扣下分支**（`task_finish_cleanup.go`）：
   - `rep.SkipReason = reason + closeoutWithheldStateSeparator + finishCleanupDeliveryWithheldMarker
     + finishCleanupRemovalNotReachedNote`
     —— 状态句与"worktree 也没被回收"这半句都进来，**行 trail、list 渲染、收尾输出
     共用同一句话**（一句话只有一个来源）；
   - 调用链路**唯一**的记录器 `finishCleanupRemovalRefusedReason(rep.TaskID, rep.SkipReason)`
     把整句写进行 trail —— 与移除臂同一个写者，因此两端不可能漂移；
   - **plan 模式不写**（`if !rep.PlanOnly`）：`--dry-run` 是对账本的**读**，写下去会让
     `--dry-run` 修改它正在描述的行，下一个读者也就分不清"交付被扣下"和"有人跑过一次
     plan"。报告仍然携带那句话（operator 看得见），trail 在**真实** run 被扣下时才得到它。
     这条由 `TestCloseoutWithheldDeliveryPlanModeWritesNothing` 钉住。
2. **event name 不变，写者多了一个**。考虑过把
   `finish-cleanup-removal-refused` 改名成 `…-delivery-refused`（它现在的语义是
   「交付车道被拒」，而不是「移除被拒」），但**改名是有代价的**：本机账本该 event
   **2063 行**全部由移除臂写入（实测 `artifacts/21-trail-before.txt`），改名会让这
   2063 行按名字不可达。所以常量名与取值都保持原样，本次的改动是**第二个写者**
   （主仓门禁），加上一个**可区分的状态句**。这条记录在此，是为了下一个读者不必再
   权衡一次。
3. **列表渲染**（`task_list_closeout_signal.go`）：
   `taskListCloseoutSignalSuffix` 把状态句提到 60 字裁剪**之前** —— 一个扫描
   `gopi task list` 的人必须在**第一眼**就知道"这条分支没出去"，而不是滚动到
   clip 之后（或者不得不逐行 `task show`）。**尾随冒号是必须的**：`clip` 在 rune
   边界上截断，冒号保证前缀在任何裁剪下都完整。

**没有动的东西**：Rule 30 的 dirty 拒绝、Rule 6 step-7 的 ancestry 检查、
`finishCleanupMainRepoDirtBlocksDelivery` 的两档分类（untracked-only 仍然
RECORDED 而不扣下）、门禁的三个调用点、plan/apply 同判。本次改动只让**已有的拒绝**
多留下一句持久的话 —— 病是"无声"，不是"拒绝"。

## 一个实现陷阱（写给下一个改这里的人）

第一版 marker 以**冒号加空格**结尾（`…reach the peer): `）。它在**未经 trim** 的
detail 上是子串（index 312），但在任何 `strings.TrimSpace` 过的副本上**不再是子串**
—— 而两个面都会 trim。表现是：单测里 `strings.Contains` 直接命中真值，跑起来却是
老渲染。判据：**marker 不以空白结尾**，分隔符独立成一个常量
（`closeoutWithheldStateSeparator`）。这条已由
`TestCloseoutWithheldDeliveryIsRecordedOnTheRowTrail` 的 trail 断言钉住（trail
读回来的是**库里那一列**，不是内存里的字符串）。

## 与既有设计的关系

- `devdoc/silent-degradation-four-cases.md` 的 case ① 修的是**移除臂**的可见性；
  本次修的是它的**镜像**：被扣下的交付。两者现在共用一个 event name 与一个渲染面,
  且**可区分**。
- `devdoc/closeout-main-repo-check.md` 记录了门禁本身的来历（为什么要读 MAIN）。
  本次不改它的判据，只在它拒绝时补上持久记录。

## 已知的残余（诚实）

- **历史行不会被追溯标记**。修复只影响**修复之后**发生的 close-out。本机那 8 条
  已滞留分支，需要一次 close-out 重跑（`gopi task finish-cleanup --apply --task <id>`，
  门禁在动作之前就返回，所以重跑不会 push/删任何东西，只会补上那一句）才会带上
  标记。这一步不在本分支做（它写 operator 的真实账本，属于 operator 的决定）。
- **历史行不会被追溯标记**（可见性的代价，不是本修复的漏洞）：本次让**未来**的扣下留痕，
  历史 8 行的空 trail 不能被追溯补写 —— 任何渲染都不能凭空造出那句话。已作为 bug 行
  `01a12d75-cacf-7caa-840d-927822603997` 单独提交（含 8 条可逐条复核的行号与只读读法）。
- **本 run 自己的交付也在这条射程里**（实测）：本 worktree clean、commit 在
  `task/01a12d5bbb3a7000a1bfc4b9766574dc` 上，但 peer 零 ref，而 close-out 启动所在的
  MAIN checkout 有 60 条 tracked 修改 —— 若结束时 MAIN 仍脏，本次交付同样会被扣下。
  修复让那种情况至少在行上留下可 grep 的一句话；**扣下本身仍会发生**（有意的守卫）。
