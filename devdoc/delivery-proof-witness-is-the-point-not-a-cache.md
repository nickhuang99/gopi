<!-- doc-class: normative -->
# 交付证明的证人是投递点本身，不是某次 fetch 的缓存

Task `01a17363-177c-72bd-aadd-4603be46f018`。受审运行 `01a16f7c-f54e-7143-bbc7-538d4a4f1819`。

## 现象（行 `01a16f7c`，2026-10-25/26 实测）

一个 clone 通道的 worker run 报 `completed`，但：**零条交付自述、ground 未回收、
tracking ref 从未更新**。三条都不是 wrapper 自己做的：

```
$ python3 -  # 只读打开 ~/.gopi/memory.db 的 task_events
8901 | 03:59:26 | in_progress
8922 | 04:36:54 | delivery-proof-owed   ← wrapper 第 1 次 claim，被守卫拒绝
8923 | 04:37:46 | delivery-proof-owed   ← wrapper 第 2 次 claim，被守卫拒绝
8933 | 05:54:56 | completed              ← 78 分钟后的另一个写者
8934 | 05:54:58 | verify-closure-queued
```

拒绝原文（事件 8922 的 detail，节选）：

> the tip `f9243581587d` (the ground's HEAD) is in NEITHER the ancestry NOR the
> patches of `refs/remotes/origin/master` (`/home/user/workspace/01a16f7c-…`),
> `refs/remotes/origin/master` (`/home/user/work/gopi`) …

而**同一时刻**，交付点自己说的完全不同（今天在本机实测两个目录的 ref）：

```
$ git -C /home/user/workspace/01a16f7c-… rev-parse refs/remotes/origin/master
769dc4545c1bc3592557f6e47fe2d1c41f367f93          ← ground 的缓存，停在切点
$ git -C /home/user/workspace/01a16f7c-… rev-parse HEAD
f9243581587d4458c4f9d3daecf90c5d1e1f2fb4
$ git -C /home/user/work/gopi merge-base --is-ancestor f924358 refs/heads/master
(exit 0)                                          ← 它早就在 master 上了
```

`f924358` 是被**别人路径**（`f8e31a2d4`）merge 进 master 的，ground 的缓存永远不会
知道。所以那条 refusal 不是"保守"，它是**错的**：交付物确实在它声明的交付点上。

## 缺陷 A：两个"证人"都是缓存，没有一个是交付点

`deliveryProofDestinations`（`internal/tools/task_delivery_proof.go`）原来解析两个
目的地，两个都是 `refs/remotes/origin/<branch>`：

| 位置 | 什么时候动 |
|---|---|
| **ground 里**那个 | 只在本 ground 自己 push / fetch 时动 |
| **记录 repo 里**那个 | 只在那个目录上一次跑 git 时动 |

两个都不是关于目的地的事实，而是关于**某个目录最后一次 fetch** 的事实。更糟的是
ground 那个被排在第一——它是**最容易冻结**的那个：交付循环的 fetch 只在下一次尝试里
跑，而一次被拒绝的收尾根本到不了下一次。

后果不是"多说了一句没交付"：收尾层拒绝 `completed` 写回，行**留在 OPEN**，
`Reclaim` 在 `finish()` 里、于是也被跳过 —— ground 不回收是这条拒绝的**下游**，
不是独立缺陷。

## 修复

一个**只读**的活体证人，排在最前：

```
git ls-remote <ground 的 origin> refs/heads/<branch>     # 问交付点
```

- 它**不写任何东西**（不 fetch、不改 ref），因此这条臂"从不移动任何东西"的承诺不变；
- 它**不解析对方的 URL**：`git remote get-url origin` 拿到的字符串原样交给 git，
  镜像/相对路径/`file://` 都成立；
- **对象必须已经在 ground 里**（`git cat-file -e <sha>`）才采信，因为这个臂**不 fetch**
  —— fetch 会移动那个"陈旧缓存"本身，正是缺陷本体。对象不在就返回"问不到"，
  落到原来的两个缓存上，所以**离线收尾的行为与改前完全一致**；
- 交付点不可达 / 没有 origin / 那边没有这个分支 = **证据缺失**，永远不是一个判决。

`deliveryProofRef` 因此多两个字段：`Point`（交付点报的 sha）与 `URL`（交付点自己的
名字）。测量在 ground 里跑（对象的唯一来源），但**对着 `Point` 比**；note 里必须能
读出"是目的地说的，还是某个缓存说的"——这正是缺陷的全部。

## 缺陷 B：唯一能解释"行为什么没关"的那一行，不在这个 run 的记录里

`taskRunCloseoutEscalate` 的判词（`task … was NOT closed out — …`）原来只写
`os.Stderr`。对一个 detached worker，那是一条**没有任何 task id 能到达**的共享流。
run 自己的记录因此彻底沉默：

```
$ grep -c "was NOT closed out" \
    ~/.gopi/sessions/01a16f7c-f54e-7143-bbc7-538d4a4f1819/output.log
0
```

而这条 run 的其它每一条 close-out 行都走 `execRunFacts`（run 自己的 log **加上**
spawning stderr，见 `cmd/agent/cli_tasks_exec_output.go`）。所以修复是把这两行换成
同一个 sink：判词，以及 clone 通道交付失败时的 refusal 行（`cli_tasks.go`）。

**注意这是"加一路"，不是"搬一路"**：`execRunFacts` 本身是 MultiWriter，终端照旧看见。

## 验收（本 run 实跑）

见 `## Verification`（报告 `$GOPI_TASK_REPORT_PATH`）。两个复现测试各自实测红→绿，
红是把生产文件改回旧写法、绿是把修复按 blob 哈希还原之后：

| 测试 | 红（旧写法） | 绿（还原后） |
|---|---|---|
| `TestDeliveryProof_CloneGroundTrustsTheDeliveryPointNotItsCache` | `the arm answered "undelivered" while git says a90cb2645796 IS at the delivery point` | `ok` |
| `TestCloseoutEscalationReachesTheRunsOwnRecord` | `the close-out verdict never reached the run's own record (execRunFacts): … Record contents: ""` | `ok` |

镜像臂 `TestDeliveryProof_LiveWitnessDoesNotRescueAnUndeliveredGround` 保证这条臂
**没有变成"永远交付"**：交付点从没见过 ground 那个 commit 时，答案仍然是 `undelivered`。

## 与既有文档的关系

- `devdoc/clone-delivery-order-at-closeout.md` 记的是**顺序**（先交付后 claim）——
  那条是 `0754f0d`，本 run 的 base `60eeb2c` 已经包含它。本文件记的是顺序修好之后
  **仍然会误判**的另一半：证人本身选错了。
- `devdoc/delivery-proof-at-closeout.md` 是这条臂的原始契约。本文件是它的补丁：
  "在目的地证明" ⇒ "**向目的地**证明"。
- 本文不改任何顺序、不写任何 ref、不引入任何常驻物（Rule 35）。
