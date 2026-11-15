<!-- doc-class: normative -->

# run-ground anchor：给"没有 anchor 的 clone ground"一条可达的路

**这条腿解决什么。** run-ground anchor 轴对目录的可见性**只**来自一条记录：
`RunGroundAnchors` 读 removal-audit sink 里的 `kind=worktree-ground`。写这条记录的生产者
在 commit `3feaf4a`（任务 `01a17b5b`）才落地，而且 forward-only —— 它之前开出来的 clone
ground 永远没有记录，因此不可回收、不 keep、连计数都没有。本机实测 14 个 / 2150 MB。

**它不做什么。** 不删任何东西。采纳与删除是两个动作、两条理由：一条命令把"没有记录"
直接变成"已删除"，就没有任何一刻能让操作员读到它判了什么。删除仍然由既有的
`runGroundCollect` 腿在 `--apply` 下完成，**在采纳之后**。

## 三条判据（都是关于目录的事实，没有一条关于记录）

| # | 判据 | 实现 |
|---|---|---|
| ① | 是 clone ground：`.git` 是**目录** | `deliveryProofIsCloneGround`（包内唯一形态判别器） |
| ② | 名字是规范任务 id | `sessionid.IsCanonical` —— 它之前开的 `-r1` 形状因此被**具名**拒绝 |
| ③ | 轴上确实没有它的 anchor | `RunGroundAnchors` |
| ④ | 目录 mtime 老过 floor | 无记录可算时量**目录本身**（发明一个记录戳正是本条腿拒绝的重建） |
| ⑤ | 没有进程站在里面 | `runGroundOccupied`（cwd/fd/exe） |
| ⑥⑦⑧ | origin 在 root 内 / 树干净 / 已交付 | `runGroundCollectCloneVerdict` —— **复用** clone 腿自己的三条拒绝 |

⑥⑦⑧ 复用而非重写，是这条腿最重要的性质：采纳通过的东西，删除腿**不会**再拒；
两个实现"差不多"迟早会分叉成一次卡死的回收。

## 阶梯上的具名 rung（这是本任务真正的设计决定）

`RunGroundAnchorLiveness` 的第一级问"记录里的 pid 还活着吗"。backfill 记录里三个身份字段
全是 `runGroundAnchorUnrecorded`（**绝不伪造 pid / start-ticks / session** —— 那是
`run_ground_anchor.go` 明令禁止的"事后从 /proc 缺席重建"），而 `unrecorded` 不是 pid：
阶梯会答"无法测量 ⇒ LIVE"，把这批 ground 永远留着，**正是本任务要结束的状态**。

所以 `runGroundBackfillOrLaunchLiveness` 按记录的**种类**选问哪一个身份问题：

* **acquisition anchor**（有 taker）→ launch ladder，问"那个化身还活着吗"；
* **backfill anchor**（`Arm == ArmTaskGroundAnchorBackfill`）→ 具名 rung
  `runGroundAdoptTakerReason`，说的是它**测到了什么**而不是谁持有。

**其后每一级原样照跑，一条都没有放宽**：LEDGER rung、age floor、occupancy，以及删除腿
自己的三条 clone rung。`Backfilled` 字段由 `Arm` 推导出来（不是第二个写入口），
所以写者和阶梯不可能对同一个事实有两种拼法。

## 年龄要问两个不同的问题

`runGroundAged` 是 AGE rung 的唯一实现：

* acquisition anchor 记的是 taker **取得**它的时刻，记录戳即年龄；
* backfill anchor 是**现在**为**已经很老**的 ground 写的 —— 拿记录戳当年龄会把每一个被
  救回来的 ground 再停一整个 floor，而那正是这条腿要结束的人口损失。

## 怎么跑

```
gopi task sweep-worktrees --backfill-anchors            # 计划：列出可采纳 + 每一条的拒绝理由
gopi task sweep-worktrees --backfill-anchors --apply    # 采纳（写记录）→ 读取腿 → 删除腿
```

它是 `sweep-worktrees` 的一个 flag，不是第二十一个动词：同一条轴，必须受同一个
plan/apply 门。
