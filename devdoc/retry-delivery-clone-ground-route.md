<!-- doc-class: historical -->
# retry-delivery：clone-ground 行走 clone 通道，而不是 close-out 链的集成分支豁免

**日期**: 2026-10-26 · **任务**: `01a174c3-db10-7766-8ef8-85f7004bf8f2` (L2) · **基线**: `b7a1ff8`

## 症状（实测，不是推断）

一条真实 failed clone-ground 行，修复**之前**：

```
$ cd /home/user/work/gopi
$ ./bin/gopi task retry-delivery --task 01a16deb-73df-7a96-851d-97297f619246
peer repo path "/home/user/work/gopi" resolved from the declared delivery point (declared as "settings dev_repo")
01a16deb-73df-7a96-851d-97297f619246 blocked branch=master peer=peerbox:/home/user/work/gopi — delivery owed: branch master is exempt
```

而同一时刻 ground 的真实状态是：

```
$ git -C /home/user/workspace/01a16deb-73df-7a96-851d-97297f619246 rev-parse HEAD
c76ccb13e0acdf680b78e082805611ab2ceee5d7
$ git ls-remote --heads peerbox:/home/user/work/gopi master
b7a1ff886776665d31684385de57b63f0bdec5ce	refs/heads/master
$ git -C /home/user/workspace/01a16deb-… rev-list --count HEAD --not refs/remotes/origin/master
1
$ git -C /home/user/workspace/01a16deb-… merge-base --is-ancestor c76ccb1 b7a1ff8 → 非零
```

即：**工作真实存在、从未投递**，而补投路径给出的理由是"branch master is exempt"。

## 根因

`deliveryRetryOne` 的第 ⑥ 步把**每一条**行交给 `TaskFinishCleanupSessionID`（close-out 链）。
该链的 `finishCleanupExemptBranchesResolved` 把 `master`/`main` 当**集成分支**跳过 —— 那对
「回收一条已合并的 feature 分支」是**对的**，但 clone ground 行的 `deliverable.branch`
**就是** `master`（clone 通道自己的投递是 `push origin HEAD:master`），于是这个豁免对它是
一个**永久屏障**：行的整个交付物就是那次 push。

而 clone ground 真正的投递实现 `devworkspace.AttemptDelivery`
（clean → pull --rebase → **声明式** build → `push origin HEAD:<branch>`）**从未被这条路径调用过**。

本机 99/103 条 failed 行是这个形状（epic 简报），所以"failed 行能否补投"整体不成立。

## 修法（只动 retry-delivery 侧的路由，不动豁免规则）

`internal/tools/task_delivery_retry.go`，第 ⑥ 步前加一条**按 ground 形状**的路由：

```go
if deliveryProofIsCloneGround(row.Worktree) {
    rep.Chain = nil
    return deliveryRetryCloneDelivery(ctx, db, row, rep, mainDir, target)
}
```

- **判据是事实，不是名字**：`deliveryProofIsCloneGround` 看 `.git` 是不是**目录**
  （clone 自成仓库）还是**文件**（主仓里的注册）。绝不按分支名或路径拼写判断 —— 那正是
  这个 bug 的形状。
- **豁免规则一字未改**：`finishCleanupExemptBranchesResolved` 仍然豁免 master/main，
  close-out 链仍走它。本修复是**绕开**，不是放宽；linked worktree 行仍走原来的链。
- **不新增第二道闸**：clone 通道自己的步骤（clean / pull / 声明式 build / push）就是闸，
  build 命令必须是 `settings` 的 `repos.<path>.build` **声明**，否则
  `AttemptDelivery` 用自己的句子拒绝（Rule 12）。
- **判据仍是 peer**：投递后重新 `ls-remote` 读 peer，并和**本行自己的 tip**比较；
  peer 只是"有这个分支名"不算投递。读不到 peer 时 fail-closed（记欠投递）。

新增 `deliveryRetryCloneDelivery` / `deliveryRetryCloneConfig` /
`deliveryRetryAttemptDelivery`，以及 `DeliveryRetryReport.Clone *devworkspace.Result`
（clone 行不跑 close-out 链，只报 `Chain` 会让一次真实发生过的尝试读成 `nil`）。

**build 声明的解析顺序**（与运行时腿同源）：ground 自己的 `origin` → `settings dev_repo`
的本地路径 →行记录的 `deliverable.repo`，取第一个在 `repos` 表里有 build 的。

## 第二个缺陷（真跑打出来的，不是推理出来的）

第一版修复把一条**空推送**报成了 `delivered`。逐字读数：

```
$ ./bin/gopi task retry-delivery --task 01a16deb-73df-7a96-851d-97297f619246
01a16deb-… delivered branch=master … the peer now advertises it at b7a1ff8…
$ git ls-remote --heads peerbox:/home/user/work/gopi master     # 与运行前完全一样
b7a1ff88…	refs/heads/master
```

机理：clone 通道的**第一步**就是 `pull --rebase origin <branch>`，它把 ground 快进到了 peer 的
tip（该行补丁上游已由另一个 commit 重建 —— `git cherry` 显示 `-`）。随后的
`push origin HEAD:master` 于是是**空推送**，git 仍然 exit 0；而我当时用于比对的 `rowTip`
是在通道跑完之后读的，**等于拿 peer 的值和它自己比**。

修法 —— 判定改成**两个事实**，缺一不可：

1. **tip 必须在投递前读**（`rowTipBefore`），否则 `pull --rebase` 会把它换成 peer 的值；
2. **peer 的 ref 必须真的推进**（`tip != peerTipBefore`）。没推进时：
   - peer 的 master **已包含** `rowTipBefore`（`git cherry` 全为 `-` / `merge-base --is-ancestor` 成立）
     → `already-merged`，理由写明"重推会复活服务端采纳链故意删掉的 ref"；
   - 否则 → `blocked`，理由是"通道说推了，但 peer 的 ref 一动没动，本行的工作不在 peer 上"。

红灯测试 `TestDeliveryRetryCloneGroundDoesNotCallAnEmptyPushADelivery` 钉住它：让 peer 直接
seed 成 ground 自己的 tip（于是 pull 无事可做、push 是空操作、git exit 0），断言**不得**报
`delivered`，且必须是 `already-merged` / `already-delivered` 之一。

## 测试

`internal/tools/task_delivery_retry_clone_test.go`（新增）：

| 测试 | 钉住什么 |
|---|---|
| `TestDeliveryRetryCloneGroundRowDeliversThroughTheChannel` | ① route：peer 裸仓真的收到了**本行自己的 tip**（问裸仓，不问报告）；② reason：**不得**再出现 `is exempt`；③ `Chain == nil`、`Clone.Pushed`、ground 被保留 |
| `TestDeliveryRetryCloneGroundRefusalIsTheChannelsNotTheExemptions` | 没有 build 声明时，拒绝句是**通道自己的**（`clone channel: ` + `no build command is declared`），欠投递落在 trail，peer 没被推 |
| `TestDeliveryRetryCloneGroundRouteIsTheChannel` | 判据本身：`.git` 是目录 = clone；是文件 / 不存在 / 空路径 = 不是 |

红灯证明是**真变异**，不是故事（逐字输出见任务报告 `## Evidence`）：把生产路由改成
`if false && deliveryProofIsCloneGround(...)`（即修复前的形状）后：

```
--- FAIL: TestDeliveryRetryCloneGroundRowDeliversThroughTheChannel (0.09s)
    a clone-ground row was answered with the close-out chain's exemption ("delivery owed: branch master is exempt") …
--- FAIL: TestDeliveryRetryCloneGroundRefusalIsTheChannelsNotTheExemptions (0.09s)
    the refusal is the close-out chain's exemption ("delivery owed: branch master is exempt"), not the channel's refusal
```

恢复按**内容同一性**验证（`sha256sum` 与变异前一致 `9cb700ce…`，变异串 0 处残留），再跑绿。

## 为什么不做成开关

Rule 35 的口径：能直接判的不加依赖。ground 的形状是一个**可测量的事实**，不需要配置键、
不需要定时器、不需要脚本 —— 一条 `if` 加一个判据函数就够，且它每一次运行都重新测。

## 未覆盖（如实）

- `DeliveryRetryReport.Clone` 只在 clone-ground 分支填充；linked worktree 行仍只有 `Chain`。
- 没有新增"多少次失败后放弃"的策略：通道自己的 brake（`MaxPushAttempts` 3 次、
  `MaxDeliveryRounds`）已经是那个上限。
