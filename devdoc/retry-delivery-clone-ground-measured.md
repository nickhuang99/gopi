<!-- doc-class: historical -->
# retry-delivery 的终态分支到底做了什么 —— 实测（2026-10-26）

> 本页记录一次**实测**，不是规范。它回答的是一条 bug 行（`01a17343-4d51-75c7-82ad-5080044a63fc`）
> 的原始判据是否成立，以及在核查过程中发现的第二层缺陷。

## 一句话结论

`gopi task retry-delivery` 对 **clone-ground 行**（`deliverable.branch = master`）
**两处独立失效**：先被一个**自反的**复活守卫判成 `already-merged`（零动作），
修掉之后又被 close-out 链的**集成分支豁免**挡住（`branch master is exempt`）——
因为 clone ground 的投递通道（`devworkspace.AttemptDelivery`）**从来不是那条链**，
而 `retry-delivery` 只调用后者。本机 99/103 条 failed 行是这个形状。

## 第一层：复活守卫问的是一个自反问题（本 run 已修）

`internal/tools/task_delivery_retry.go` 的 ③④ 步原本是：

```go
peerTip := deliveryRetryPeerTip(ctx, mainDir, target, row.Branch)  // ls-remote --heads <peer> <branch>
if peerTip != "" {
    if deliveryRetryPeerMasterContains(ctx, mainDir, target, peerTip) {   // peer master 是否含 peerTip
        rep.Status = deliveryRetryStatusAlreadyMerged
```

clone 通道推的是 `HEAD:master`，所以 clone ground 的行记录 `deliverable.branch = master`；
于是 `ls-remote --heads <peer> master` 拿到的就是 **peer 自己的集成分支 tip**，
"peer 的 master 是否包含它" **恒为真**。

**实测（修复前，本机真实 failed 行 `01a16deb-73df-7a96-851d-97297f619246`）：**

```
01a16deb-73df-7a96-851d-97297f619246 already-merged branch=master peer=peerbox:~/work/GoPi
  — the peer's master already contains 0592c9ae4537276c23f7ff30d8bc7310a3d645fb — left alone
```

而同一时刻该行 ground 的真实 HEAD 与两个 master **都没有亲缘**：

```
ground HEAD = c76ccb13e0acdf680b78e082805611ab2ceee5d7
  ancestor of delivery-point origin/master (f110202d)?  NO
  ancestor of peerbox:~/work/GoPi master (0592c9ae)?       NO
```

### 修法

在问任何资格问题之前，先读**本行自己的 tip**（`deliveryRetryRowTip`）：

- **clone ground**（`.git` 是目录）→ 该目录的 `HEAD`。**先问 ground，它的答案优先**：
  那是这一 run 真正干活的那棵树。
- **linked worktree / 无 worktree** → 主 checkout 的 `refs/heads/<branch>`。

然后每一处判定都问这个 tip，不问 peer 自己广告的那个：

| 判定 | 旧问法 | 新问法 |
|---|---|---|
| `already-delivered` | peer 有这个分支 | peer 在**本行 tip** 上有这个分支 |
| `already-merged` | peer master 含 **peerTip** | peer master 含 **rowTip** |

保留旧问法的唯一合法场合：**本行本地没有 tip**（分支已被回收），此时 peer 的 ref
是唯一的记录，原来那个问题是它该问的问题。

回归测试：`TestDeliveryRetryOnACloneGroundRowAsksAboutTheRowsOwnTip`
（`internal/tools/task_delivery_retry_test.go`）。红证实测：把守卫改回问 `peerTip`，
测试变红并打印 `status="already-merged" ... detail="REDPROOF"`；恢复后绿。

## 第二层：换掉判据仍然送不出去（**未修，已提 bug `01a174c3`**）

修完第一层，同一条真实行给出的是：

```
01a16deb-73df-7a96-851d-97297f619246 blocked branch=master peer=peerbox:~/work/GoPi
  — delivery owed: branch master is exempt
```

来源（`internal/tools/task_finish_cleanup.go`）：

```go
exempt := map[string]bool{"master": true, "main": true}   // :793
rep.SkipReason = "branch " + branch + " is exempt"        // :925
```

这个豁免**本身是对的**——close-out 链不该把集成分支当"待合并的分支"推上去。
错的是**路由**：clone ground 的投递实现不是那条链，而是

```
internal/devworkspace/devworkspace.go:349   func AttemptDelivery(...)
    clean → pull --rebase origin <branch> → 声明式 build → push origin HEAD:<branch>
```

`grep -rn 'AttemptDelivery(' --include='*.go' .` 全仓只有两处调用：它自己，和
`cmd/agent/devworkspace_channel.go:227`（worker 回合结束时的那条腿）。`retry-delivery`
一次都没调用它，所以对 clone-ground 行它**永远**只能在"被豁免"和"already-merged"之间选。

### 影响面（本机账本实测）

失败行按 `deliverable.branch` 分布：

```
 99 master
  1 task/01a144001e4b7086b97d257497a832ed
  1 task/01a143b13407791abe72d7d1e391ac43
  1 fix/memory-search-nonblocking-flake
  1 feat/safe-merge-guard-answer
```

**99 / 103 落在豁免的那个名字上。**

## 关于原 bug 行的判据

原行写的"retry-delivery 只复述拒绝、不执行任何投递"，其**举证对象选错了**：
它在一次仍然活着的 worker 回合里观察到 `origin/master` 未移动，据此推断投递腿不存在。
实测复核：

- clone 通道的投递腿确实存在（`clone-channel: attempt 1/3: push` → `push accepted`，
  见 `~/.gopi/sessions/01a16e1d-…/output.log:7625-7628`）；
- 它在**回合结束之后、写回之前**执行（`cmd/agent/cli_tasks.go:3290` 的
  `cloneRun.finish(os.Stdout)` 在 `writebackOutcome` 之前），回合内看不到移动是预期；
- 但对**已落 failed 的行**，"补投能不能成功"是另一个问题，答案是：对 clone-ground
  行**不能**，原因是上面两层。

所以原判据作废（提交者已自行撤回），而它指向的那个真问题由本页与 bug `01a174c3` 接住。

## 证据文件

```
/home/user/.gopi/sessions/01a17343-4d51-75c7-82ad-5080044a63fc/
  evidence-retry-delivery-run1.txt      修复前：already-merged，零动作
  evidence-retry-delivery-FIXED.txt     修复后：blocked —— 第二层暴露
  evidence-three-layers.txt             L1 / L2 / L3 三层
  evidence-exempt-master-hole.txt       99/103 的分支分布
  evidence-retry-verdict.txt            该行 ground HEAD 与两个 master 无亲缘
  evidence-branch-refs.txt              withheld=1 / ORPHANED=208 / unresolved=406
```

## 复现配方

```bash
cd <gopi checkout> && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/

# 修复前：already-merged（零动作）
# 修复后：blocked — delivery owed: branch master is exempt
./bin/gopi task retry-delivery --task 01a16deb-73df-7a96-851d-97297f619246

# 两层各自的判据
cd /home/user/work/gopi && git fetch -q origin master
git merge-base --is-ancestor c76ccb13e0acdf680b78e082805611ab2ceee5d7 origin/master; echo $?   # 1 = NO
git ls-remote --heads 'peerbox:~/work/GoPi' master
```
