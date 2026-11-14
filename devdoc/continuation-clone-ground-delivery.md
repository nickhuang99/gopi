<!-- doc-class: normative -->
# 续跑面的 clone ground：为什么 `gopi worker --resume` 的行会 `completed` 而提交没发布

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**症状（2026-10-31 实测，两条样本）**：`01a190bb-0960-77ce-b76b-5538eb572ef8`（05:57）
与 `01a190b4-3da1-7175-95ea-4007aac3913e`（06:02）以 `gopi worker --resume` 跑完，
行写 `completed`，日志里却**既没有 `push accepted` 也没有 `delivery did not land`**，
远端 `git ls-remote` 从没见过那些提交。同日同机的对照组（排队派发的 `gopi worker`，
无 `--resume`）推送正常 —— 这就是把缺陷定位在**续跑收尾腿**而不是环境的依据。

## 三层链，源头是一处 `if !resume`

`internal/agent/loop.go` 的起手钩子把「注册行 + 采纳 clone ground」全写在
`if !resume` 之内。**resume 造不出行**，于是整段被跳过，以下三层依次塌陷：

| # | 位置 | 前一刻 | 后果 |
|---|---|---|---|
| 1 | `internal/agent/cloneground.go` 的采纳 | `cloneGroundDir` 保持空 | `CloneRunGround()` 返回 `("", "")` —— 那是 **worktree 通道**的答案 |
| 2 | `internal/agent/clonedelivery.go` 的 `finishCloneDelivery` | 拿到空 ground | 在**兼容臂**处返回：不读配置、不调 `devworkspace.AttemptDelivery` → 没有 attempt，没有结论行 |
| 3 | 收尾钩子 | 交付腿"没报错" | 写 `completed`；`recordCompletionWitness` 记 `witnessless` 且**按契约不改状态** |

第 3 层是这条 bug 的**放大器**：`witnessless` 本来是为了让"无证据的完成"留下痕迹，
但它按设计不拒绝写回，所以一条没有任何发布尝试的行，读起来和一条发布成功的行一样。

## 修法：一处身体，两个调用者

`adoptCloneGroundDir(taskID, announced, branch, adoptedFmt)` 是唯一的采纳身体；
`adoptAnnouncedCloneGround`（runner 宣布的目录）与新增的
`adoptContinuationCloneGround`（**进程自己站的目录**）各调用它一次，只有文案不同。
Rules 13/14：不复制逻辑，不写第二套判据。

**判据没动**：`.git` 是**目录** = clone，是**文件** = linked worktree —— 续跑面
**永不**采纳后者（那是另一个通道的 ground，采纳等于从别人的地面发布）。

**续跑面采纳它已经站在里面的那个目录，绝不另造一个。** 这一点是本次修复的核心语义：
一个续跑是"回到第一次运行工作过的地方"，不是"打开一个新的工作区"。造第二个目录
正是让提交变得不可见的那种形状 —— 两个目录，其中一个是空的，而 briefing 写着另一个。

## 验收判据（机器可读，别用"看起来对"）

1. **工作真的到了**：源 repo 的 `git log --oneline -1` 换成了本次提交 —— 这是 bug 毁掉的
   那个事实。`cmd/agent/worker_delivery_e2e_test.go` 的
   `TestWorkerResumePublishesItsCommits` 断言它；
2. **运行说了它是怎么走的**：stderr 出现 `clone channel: delivery delivered`
   （或 `BLOCKED` / `BROKEN`）。缺了全部三者的运行，正是缺陷的形状（Rule 15）；
3. **空 ground 是结论**：`nothing to deliver`（bug 01a182a8 的形状 —— 把空 ground
   折成"非交付"会把行写回 `in_progress`）。

**红证怎么取回**：把 `loop.go` 续跑臂里那行
`a.adoptContinuationCloneGround(a.autoTaskRowID)` 删掉、重编二进制、跑 ①，
它会报 `the source repo's master did not move — the continuation's commits stayed in
the clone exactly as rows 01a190bb / 01a190b4 reported`。

## 同一行里顺手纠正的两件事

- **`witnessless` 的句子现在带恢复地址**（`internal/tools/task_completion_witness.go`）：
  行记录的 ground 路径 + 它 `HEAD` 站在的提交（短 sha）；ground 已不在磁盘就**明说不存在**。
  修前它只说"什么都没找到"，于是运维要开 session 日志手工找**一个这条腿本来就握着的**事实。
- **帮助文本原来描述错了通道**：`--resume` 写的是 "finish THAT row's existing worktree"，
  而声明了 `dev_repo` 的主机上那个 ground 是 clone，收尾要**发布**。`cmd/agent/main.go` 与
  `man/gopi-worker.md` 一并改正（`TestManSourcesMatchTheirHelpPage` 会比对两者）。

## 别把这条规则读过头

- 它**不是**"resume 必须造一个 ground"。恰恰相反：续跑采纳既有的，没有才不采纳。
- 它**不是**"witnessless 应该拒绝写回"。那条契约（记录、不改状态）没有变，本次只让它
  留下可恢复的地址。
- 它**不给** `--resume` 任何推送权：发布仍然只属于 clone 通道的
  `devworkspace.AttemptDelivery`，本次改动只保证那条腿**被走到**。
