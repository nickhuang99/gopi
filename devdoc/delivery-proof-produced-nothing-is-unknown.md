<!-- doc-class: normative -->
# delivery-proof: 一个"什么都没产出"的 ground 是 `unknown`，不是 `delivered`

> 2026-10-28，任务 `01a17dd1-85c7-7f02-b3cf-41fa269c6112`。
> 判据实现在 `internal/tools/task_delivery_proof.go`（门本身）与
> `internal/tools/task_delivery_proof_content.go`（`tipIsTheCutPoint` 与
> `deliveryProofTipDir` 两个测量函数）。

## 1. 缺陷：内容臂会把"没产出"判成"已落地"

内容臂（`deliveryProofContentLanded`，本仓库已有的那一臂）问的是：
**"投递点是否已经带着这次 run 产出的内容？"** 它按 `base..tip` 找出本次 run
自己的提交，再逐条比对投递点的树。

问题出在 `base..tip` 为空的时候：**一个什么都没提交的 ground，`tip` 就是它的
`origin/master`**（clone ground 在 cut 时 fetch 一次，此后再不 fetch）。
此时：

- ancestry 臂（`merge-base --is-ancestor <tip> <ref>`）的答案是"contained"，
  但理由是**tip 是它自己的祖先**——一个恒真的同义反复；
- 内容臂的 `rangeRef`/`base` 探测同样能在这条自指的链上得到答案。

于是"这次 run 什么也没产出"被记录成 **`delivered`**，而 `delivered` 正是
释放 ground 的那个信号。

**实测（红证，本提交内）**：在同一份 fixture 上把这道门短路掉
（把条件改写成 `if false && tipIsTheCutPoint(ctx, ground, tip) {`，跑完再还原），同一行得到

```
state = "delivered"
note : row dpg-empty-run: master (the ground's HEAD) is delivered —
       7b868a540c06 is an ancestor of 7b868a540c06def2ab87f44e5d766eb7eed5d55a
       in refs/heads/master (/dev/shm/suite-tools-run-1224318974/TestDeliveryProof_AGroundOnItsCutPointProducedNothing3611390545/001/server = 7b868a540c06)
```

`7b868a540c06 is an ancestor of 7b868a540c06` —— 这句话本身就是证据：
它说的是同义反复，不是关于投递的任何事实。

## 2. 为什么这不是假想：chat 会话的 ground 永远停在 cut 点

Rule 37 给 `gopi chat` 一个自己的 ground，**而 chat 不提交**。所以一个 chat
会话的 ground 在整个会话期间就坐在 cut 点上。没有这道门，每一条 chat 行的
交付证明都会回 `delivered`——一个从未产出的东西被判成已落地。

普通 worker run 也可能落到这个形状：run 在产出提交之前失败、或它唯一的产出
在收尾之前被回滚。

## 3. 修复：门放在任何 containment 臂之前

```go
if tipIsTheCutPoint(ctx, ground, tip) {
    out.State = deliveryProofUnknown
    out.Note = deliveryProofUnknownNote(out, fmt.Sprintf(
        "the tip %s is the very commit this run's ground was cut from, so the run produced NO commit of "+
            "its own — there is no delivery to prove", branchDeleteShortHash(tip)))
    return out, nil
}
```

三个判定，只有中间那个是对的：

| 判定 | 为什么错 |
|---|---|
| `delivered` | 空的主语集合不是"全都落地了"——它是假阳性 |
| `undelivered` | 拒绝一个从未存在过的交付——假拒绝，且这一拒绝会让行无法收尾 |
| **`unknown`** | 唯一诚实的答案：说清"这次 run 没产出"，同时仍把
`delivery-proof-unverified` 记到 trail 上（Rule 15：沉默不是答案） |

**为什么 `unknown` 不阻塞**：`unknown` 是"没有证据"，不是"证据说没有"。行照常
收尾，而"没有证明可给"这个事实留在记录上。

## 4. `tipIsTheCutPoint` 只能在 ground 里问

```go
func tipIsTheCutPoint(ctx context.Context, ground, tip string) bool {
	cut := deliveryProofGitRead(ctx, ground,
		"rev-parse", refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName)
	if cut == "" {
		return false
	}
	if cut == tip {
		return true
	}
	_, err := branchDeleteGit(ctx, ground, "merge-base", "--is-ancestor", tip, cut)
	return err == nil
}
```

**判据必须是"ground 自己的 cut 点快照 vs tip"，不是"tip 是不是 live 集成分支的祖先"。**
后者的答案永远由"tip 是它自己的祖先"给出——它会把每一条已投递的行、以及每一条
空 run，都判成 delivered。用 `merge-base --is-ancestor <tip> <cut>` 而不是只比相等，
覆盖了"run 的产物被回滚、HEAD 落回 cut 点之前"这一形状。

`cut == ""`（读不到）时返回 **false**，即"这个问题问不了"——门不响，落回下面
的臂。这与 Rule 15 一致：读不到不等于零值成立。

## 5. 这一页记录的是什么，不是什么
本提交**没有**重新实现内容臂——`internal/tools/task_delivery_proof_content.go`
的内容臂已经在 master 上（它由 `01a17dbc` 交付，判据见
`devdoc/delivery-proof-witness-is-the-point-not-a-cache.md` 与
`devdoc/delivery-proof-at-closeout.md`）。本提交在它**前面**插一道门，并只带上
这道门需要的两个测量函数（`tipIsTheCutPoint`、`deliveryProofTipDir`）。

红线测试在 `internal/tools/task_delivery_proof_cutpoint_test.go`，两条：

1. `TestDeliveryProof_AGroundOnItsCutPointProducedNothing` —— 行必须是 `unknown`，
   且 note 里要说"produced NO commit of its own"；
2. `TestDeliveryProof_TheGateGuardsAnArmThatWouldHaveSaidDelivered` —— **红证的
   实体**：直接调 `deliveryProofTipContained`，断言它对这同一份 fixture 答案是
   **contained**。也就是说这道门挡住的是一条真的会判 delivered 的臂，而不是一个
   永远不触发的猜测。

## 6. 本包在本轮是 RED，而这不是本改动造成的（对照实测）

`internal/tools` 这个包在本轮是 RED，而且**在 `87b9e0a` 上就已经是红的**。
两个 tree 各跑一遍**同样那三条命令**，是这条论断唯一的证据形式（我对控制组跑的是
下面这三条 `go test`，不是 `make test-budget` 整包 —— 整包的结果由它们的并集决定，
逐条跑是为了让每一条的归属都能看清）：

| 检查 | 控制组 `/home/user/workspace/dp-ctrl-87b9e0a`（87b9e0a，**没有我的提交**） | 本 ground（87b9e0a + 1 commit） |
|---|---|---|
| `internal/stylegate` `TestStyleGate` | **FAIL** — ignored-error 78 / ceiling 77，unchecked-close 2 / ceiling 0，test-sleep 2 / ceiling 0 | **FAIL** — 逐字相同的三行 |
| `internal/tools` `TestStylegateRatchetSuiteIsGreenOnThisTree` | **FAIL** | **FAIL** |
| `internal/tools` `TestMergeConfigAnchorGate*` | ok | ok |

**我新增的那个测试文件不在任何一条超标的清单里**：

```
go test ./internal/stylegate/ -run '^TestStyleGate$' -count=1 -v | grep -c task_delivery_proof_cutpoint
0
```

三处超标的命中全部落在 `cmd/agent/*_test.go` 与其它内部包的既有文件上（每一条都带
`file:line`），一条也不来自 `internal/tools/task_delivery_proof_cutpoint*.go`。

> **前一版这一节写错了，这里如实标注**：它当时说"7 个 gate 测试先于本提交就红，
> 含 5 × `TestMergeConfigAnchorGate_*`"。本轮在**同一个控制组 worktree** 上复测，
> `TestMergeConfigAnchorGate*` 是 **ok** —— 那句话在写下的时刻就已经过期了。
> 现在这一节的每一行都是本轮跑出来的，控制组 worktree 用完已 `git worktree remove`。

## 7. 验收命令

```bash
go test ./internal/tools/ -run TestDeliveryProof_AGroundOnItsCutPointProducedNothing|TestDeliveryProof_TheGateGuardsAnArmThatWouldHaveSaidDelivered -v
go test ./internal/tools/ -run TestDeliveryProof -v      # 既有的交付证明臂，无回归
gofmt -l internal/tools/task_delivery_proof.go internal/tools/task_delivery_proof_cutpoint.go internal/tools/task_delivery_proof_cutpoint_test.go
CGO_ENABLED=0 go build ./...
```
