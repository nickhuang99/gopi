<!-- doc-class: normative -->

# `--drift` 的 `STALE:` 现在只表示"artifact 真落后于 ref"

**Subject**: `gopi version --drift` 的总结论判据，以及列表里第三个 ref
（push-back lane 的 scratch 镜像）为什么不参与这个结论。

## 1. 缺陷：总结句否定自己上面每一行

host hosta，2026-10-29，部署 artifact `d50f746d7`，原样输出：

```
refs/heads/master:  follows the ref — the artifact was built from it — d50f746d7…
refs/remotes/origin/master:  1 ahead commits the ref does not have — this artifact is AHEAD of it, not stale behind it (ref tip 8f68e59f6…)
refs/remotes/gitPushcheck/master:  426 ahead commits the ref does not have — this artifact is AHEAD of it, not stale behind it (ref tip b7a1ff886…)
STALE: this machine runs an artifact that does not follow the ref(s) above. The remedy is …
```

三条逐行文本都在说"不是落后"，最后一行却说 STALE。判据是
`DeployDrift.Stale()` 里的 `r.Reason == "" && !r.Follows` —— 把 AHEAD 折叠进了
stale，而 `DeployDriftVerdictAhead` 的 doc 自己写着 AHEAD "is not stale"、补救
方式也不同（它不会靠 rebase 追上该 ref）。三处（doc、逐行文本、总结句）互相矛盾，
其中两处是对的。

## 2. 裁决：A —— AHEAD 不算 stale

二选一里选 A，理由是三条一致的证据，而不是偏好：

1. `DeployDriftVerdictAhead` 的 doc 已经这么说了；
2. 逐行文本已经这么印了（"not stale behind it"）；
3. **操作员真正能采取的行动**：AHEAD 意味着 artifact 含有该 ref 的全部历史再
   加上一些自己的提交 —— 一个重建只会把 artifact 降回它已经包含的 ref。把它报成
   stale 的唯一可行"补救"是错的。

选 B（留着 stale、删掉两处措辞）会让报告给出一个操作员不该执行的建议。

## 3. 判据收窄成一个方法：`DeployDriftRef.CountsAsStale()`

```go
func (r DeployDriftRef) CountsAsStale() bool {
	if r.Scratch || r.Reason != "" || r.Follows {
		return false
	}
	return r.Behind > 0
}
```

三个条件缺一不可：

- `Reason == ""` —— 测量真的做了。测不到的 ref 不是证据（两种沉默必须区分，Rule 15）；
- `!Follows` —— 用的是 `RebuildAfterMerge` 那套自己的比较，不是第二份定义；
- **`Behind > 0`** —— 该 ref 握着 artifact 没有的提交。这一条是本次修正的核心：
  AHEAD 时 `Behind == 0`，于是它不再投票；DIVERGED 时 `Behind > 0`，于是它仍然
  投票（那种情况下 artifact 确实落后该 ref）；普通的 `behind N` 当然也投票。

`DeployDrift.Stale()` 只被这一处定义，打印层不再自己推导
（`cmd/agent/cli_version.go` 的 `deployDriftSummary` 读的就是 `CountsAsStale()`）
—— 两侧读同一个谓词，是"逐行与总结不可能再相反"的机制，而不是一句承诺。

## 4. 第三个 ref：测量、打印、但明说不投票

`DeployDriftRefs` 的第 3 项 `refs/remotes/gitPushcheck/master` 是 push-back lane
的私有命名空间：由那趟 pass 写入（`gitPushbackCheckRef`），也由那趟 pass 的 prune
收回（`gitPushbackPruneMirrors`）。**在没有 push-back lane 的主机上它冻住，然后
每次落地都更落后一截**——实测 3 天、405 提交且单调增长，2026-10-29 再测已是 426。

一个没有任何操作员动作能把 artifact 移向它的 ref，不能成为"artifact 陈旧"的理由。
所以它：

- 仍然**被测量、被打印**（那个数字是关于这条 lane 的真实证据，删掉就丢信息）；
- 它的行**追加一句明说**它不投票（`versionDriftScratchNotVoted`）；
- `Scratch` 由 `deployDriftRefIsScratch()` 按 `gitPushbackCheckRootRef` 前缀判定，
  同命名空间下的 peer 镜像（`…/<ns>/peer/<branch>`）一并覆盖，不需要第二个字面量
  （Rule 14）；
- `DeployDrift.VotingRefs()` 把"谁投了票"这件事也留成可查询的事实。

**没有删本机那条 ref**：那是症状。删了下一趟 push-back 又会留下，而判据仍然错。

## 5. 干净的结论也印出来，且不叫 "NOT STALE"

原来的干净状态是"什么也不印"。这与"命令停止测量了"读起来一样（Rule 15）。现在
干净时印一行 `CURRENT:` —— **故意不用 "NOT STALE:"**：实测发现干净句子若带
`STALE:` 这个 token，`grep STALE:` 在健康机器上也会命中，操作员唯一会跑的那条
grep 就不再是答案。

## 6. 红证：两个变异，各被哪一层杀死

两个变异都打进去、跑、再用**内容同一性**（`sha256sum`，不是 `git checkout`）恢复：

| 变异 | 内容 | 结果 |
|---|---|---|
| M1 | `CountsAsStale()` 退回 `return !r.Follows`（AHEAD 投票、Scratch 被忽略） | 被 `./internal/tools/ -run TestDeployDrift` **杀死**：`TestDeployDrift_AheadScratchMirrorDoesNotMakeTheArtifactStale`、`TestDeployDrift_AheadOfEveryRefIsNotStale` 报红 |
| M2 | 打印层自己重新推导 `r.Reason == "" && !r.Follows`（即发布的那个形状） | 被 `./cmd/agent/ -run TestDeployDriftSummary` **杀死**：三条 leg 全红 |

M2 这一层是上一 row 的红证用血换来的：只测工具层时，打印点的变异会**存活**
（见 `devdoc/deploy-drift-measures-the-running-artifact.md` §4）。所以本轮
`cli_version_drift_test.go` 直接驱动 `deployDriftSummary`。

`deploy_drift.go` 恢复后 sha256 与变异前逐字相同：`e7d79e92…96e87fa`。

## 7. 两态真跑（不是单测）

在 `/dev/shm/drift-proof-…` 造了一个真仓库（6 个提交），把它 clone 两次，各配
`refs/heads/master`、`refs/remotes/origin/master`、`refs/remotes/gitPushcheck/master`
三个真 ref，再用 `-buildvcs=false -ldflags "-X …/version.Revision=<sha>"` 造两份
真二进制（`-buildvcs=false` 是必需的：否则 toolchain 会把 origin 的 SHA 与 dirty
状态烘进 VCS 信息，伪造的 revision 就不成立）。

**态 1 —— artifact == master == origin，scratch 镜像更早**（即被报告的那个形状）：

```
refs/heads/master:  follows the ref — the artifact was built from it — 5369fa7…
refs/remotes/origin/master:  follows the ref — the artifact was built from it — 5369fa7…
refs/remotes/gitPushcheck/master:  5 ahead commits the ref does not have — this artifact is AHEAD of it, not stale behind it (ref tip d697d6d…) — a push-back lane SCRATCH mirror: measured but NOT counted in the verdict below, because no build an operator can run moves this artifact toward it
CURRENT: no measured ref carries a commit this artifact lacks — …
```

`grep -c 'STALE:'` → **0**。

**态 2 —— artifact 落后 refs/heads/master 5 个提交**：

```
refs/heads/master:  5 behind (the ref is at 5369fa7…)
refs/remotes/origin/master:  5 behind (the ref is at 5369fa7…)
STALE: this machine runs an artifact that is BEHIND refs/heads/master, refs/remotes/origin/master — that ref carries commits this artifact does not. The remedy is …
```

`grep -c 'STALE:'` → **1**。反向不退化。

原样输出：`~/.gopi/sessions/01a1837c-f724-72d3-a524-57121905ea92/evidence/state-N0.txt`
与 `state-Nbehind.txt`。

## 8. 这份改动没做什么

- **没有重建部署的二进制**。那是操作员的动作 / Rule 12 的 merge→rebuild 闭环；
  `/home/user/work/gopi/bin/gopi` 不在本 run 的 ground 里。
- **没有删/改本机那条 scratch ref**：那会掩盖判据的错，而且下趟 push-back 会再留下。
- **没有新增机制**：没有 cron、没有 daemon、没有脚本、没有新文件形态。判据收窄在
  一个已有方法里，总结句来源收窄到同一个方法。
- **没有动 `internal/standards` 的既有红**：`TestStandardGates` 的 Gate9 报
  `internal/tools/delivery_content_landed_test.go:55`（`dcGit`）一条新违规，该文件
  在本 run 的 ground 上未被触碰（`git status --porcelain` 空、`git log -1` 指向
  `77c4dcf`），已按 Rule 41 单独提行。

## 9. 复现

```bash
go build ./...
go test ./internal/tools/ -run TestDeployDrift -count=1
go test ./cmd/agent/ -run 'TestVersion|TestDeployDriftRefLine|TestDeployDriftSummary' -count=1
```

跑真实两态：

```bash
# 在一个真仓库里，把 master / origin/master 指到 TIP，把 gitPushcheck 指到更早的提交，
# 然后造两份真二进制（否则 toolchain 的 VCS 信息会覆盖伪造的 revision）
CGO_ENABLED=0 go build -buildvcs=false \
  -ldflags "-X github.com/nick/gopi/internal/version.Revision=<sha>" \
  -o <repo>/bin/gopi ./cmd/agent/
<repo>/bin/gopi version --drift
```
