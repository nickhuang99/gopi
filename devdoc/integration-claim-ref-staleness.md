<!-- doc-class: historical -->
# 裸 `master` 做可达性判定会把已 merge 的交付物读成「no ref contains it」

Task `01a12d30-799b-775b-8c97-f691e282d92c`（其上游 bug 行 `01a12d1b`）。
复现、判定、最小修复与红→绿证据。读者：下一个要在 client 上判「某 commit 落地没有」的 agent。

## 1. 现象与复现（原样命令，任意 client 可跑）

host `hostb-Laptop`，`/home/user/work/GoPi`（主工作区，未 fetch）2026-10-12 07:0x：

```
$ git rev-parse refs/heads/master refs/remotes/gitPushcheck/master refs/remotes/peerbox/master
1508e3821d6d43ae873a1772582d0e23873c4e53     ← 本机 master
b8287d5c6e89fc3f8d2ab333876145ee9c5e77eb     ← 交付面的镜像 ref（git_pushback 的 per-pass mirror）
1508e3821d6d43ae873a1772582d0e23873c4e53     ← refs/remotes/peerbox/master（与本地 master 同点）
$ git rev-list --count refs/heads/master..refs/remotes/gitPushcheck/master
62
$ git merge-base --is-ancestor f82777a9f refs/heads/master ; echo $?        → 1
$ git merge-base --is-ancestor f82777a9f refs/remotes/gitPushcheck/master ; echo $?  → 0
$ git ls-remote peerbox refs/heads/master
b8287d5c6e89fc3f8d2ab333876145ee9c5e77eb     ← 服务端真值 = 镜像
$ ssh peerbox 'cd ~/work/GoPi && git rev-parse refs/heads/master && git merge-base --is-ancestor f82777a9f refs/heads/master; echo $?'
b8287d5c6e89fc3f8d2ab333876145ee9c5e77eb
0
```

同一 commit、同一个仓，两个 ref 两个答案：**本地 `refs/heads/master` 是那个陈旧的**。
`f82777a9f` 正是把已判定「no ref contains it」的门禁文件 merge 进 master 的那个 merge
commit（`gitPushcheck/master` 06:43:25，见 `git reflog show`），所以行 `01a12d1b` 的前提
是本机 ref 陈旧造成的假阴性。完整原始输出（含 reflog、`for-each-ref --contains`）留在
本行 session 目录 `artifacts/mirror-staleness-repro.txt`。

## 2. 判定：**本机 master 陈旧是设计（两条已明文记录的决定），缺陷在「质问裸 master 的那个读者」**

逐条引用（文件+行号 / 配置键）：

| 事实 | 依据 |
|---|---|
| 本地 `refs/heads/master` 由人/`git pull` 前进，**没有任何自动路径刷新它** | `internal/tools/task_sync_git.go:398`（client pass 的 `pull --ff-only` 是唯一自动前进路径，且它只在 serve tick / `gopi task push` 时跑；`gitSyncPullFFOnlyFlag` 的注释说明两 master 分叉时它被拒） |
| 交付/预 merge 那条链读的**不是**本地 master，而是镜像 ref | `internal/tools/git_peer_fetch.go:145-152`（`gitPeerMirrorRefspec` = `+refs/heads/master:refs/remotes/gitPushcheck/master`，ONE 定义）；`internal/tools/task_sync_git_pushback.go:316-323`（`gitPushbackIsMerged` 的 `--is-ancestor <branch> gitPushbackCheckRef`） |
| `refs/remotes/<peer>/master` 同样陈旧，且**已被明文决定不修** | `devdoc/premerge-merged-state-test-run.md:121-158`「The stale-tracking-ref arm — MEASURED」+ `:156`「Decision (a written decline, with its reason)」：不刷新 tracking ref、不加第二次 fetch（镜像已是新鲜答案；一个问题的第二个通道是 Rule 14 的缺陷） |
| 交付点同步在 local/server 臂上**故意不 fetch** | `devdoc/delivery-point-sync.md:54`（"this repo's own integration branch … no fetch, no network"） |
| 同样现象在本 epic 之前已被实测记录过 | `devdoc/worktree-base-align.md:164-195`（peer master == 镜像，tracking ref 陈旧） |
| 本机确实是 client 且 peer 已声明 | `~/.gopi/settings.json`：`role=client`、`sync_peers=["peerbox"]`、`task_sync=false`、`task_pull=true`（`CONFIG.md:576-578` 描述两键） |
| **缺陷**：`master_claim.go` 把「X 已在 master 上」判在裸 `master` 上 | `internal/tools/master_claim.go` 原 `AncestorCmd()`/`masterClaimAncestorStatus()` 传 `IntegrationBranchName`（= `refs/heads/master`）；`internal/tools/task_briefing_premise.go` 的 dispatch 期复测走同一条 |

所以：**陈旧本身是「已知的、写入 devdoc 的、刻意的」设计**（那些 ref 只服务人读）；
**把它当权威用来判「交付物落地没有」的读者是缺陷** —— 判据选错了 ref，方向是假阴性
（已落地读成没落地 → 重做已落地的工作，`devdoc/commit-path-ancestry-gate.md` 记录的同类）。

## 3. 修复：判据解析出**该问哪个 ref**（不新增任何机制）

新文件 `internal/tools/integration_claim_target.go`，一个函数：

```
integrationClaimTargetRefAndLookahead(repo) → (ref, lead)
  mirror 胜出 ⟺ 镜像 ref 能解析成 commit
             AND rev-list --count refs/heads/master..<mirror> ≥ 1   (lead)
             AND rev-list --count <mirror>..refs/heads/master == 0  (镜像没有落后 = 是严格超前)
  否则 → refs/heads/master（= 修复前的答案，逐字节不变）
```

- **零新机制**：全是本地对象库读（`rev-parse` / `rev-list --count`），无网络、无 fetch、无定时器、
  无 hook、无新 ref、无状态文件（Rule 35）。没跑过 peer 那一段的仓库走回退臂，行为与修复前一致。
- **激活条件是被测出来的「严格超前」**，不是「镜像存在」：镜像落后、相等或与本地**分叉**
  （peer master 被 rewind）时一律回退（分叉时两者谁也不包含谁，回退到出厂行为）。
- 与 `devdoc/premerge-merged-state-test-run.md:156` 的书面 decline 一致：**不去刷新任何 ref**，
  只让读者指名正确的 ref。**被否决的方案**：以 `refs/remotes/<peer>/master` 做激活条件 ——
  本机实测该 ref 就是 `1508e3821`（与本地 master 同点），在缺陷现场**不会触发**；因此计数在
  镜像与本地 master 之间直接做，不需要 peer 名单、不需要配置。
- 判定证据带上 ref 名：`MasterClaimEvidence.ClaimRef` + `MirrorLookahead`，`VerdictLine()`
  与 `AncestorCmd()` 输出「问的是哪个 ref、为什么」；`gopi task archive --verify-commit` 多打一行
  `measured against: …`。

改动面：`internal/tools/integration_claim_target.go`(新)、`master_claim.go`、`master_claim_report.go`、
`task_briefing_premise.go`、`cmd/agent/cli_archive.go`。

## 4. 红 → 绿（真跑，两个二进制，同一现场）

现场 `probe-01a12d30/real3`：`refs/heads/master=1508e3821`（陈旧）、
`refs/remotes/gitPushcheck/master=b8287d5c6`（peer 真值，落后 62）—— 与缺陷现场同形。
`gopi-before` 从 `git archive 1508e3821` 的树上构建（该版 `master_claim.go` 里判据仍是
裸 `IntegrationBranchName`）；`gopi-after` 是同一棵树 + 本 run 的 5 个文件。

```
$ probe-01a12d30/gopi-before task archive --verify-commit f82777a9f --repo probe-01a12d30/real3
f82777a9 (f82777a9f) is NOT an ancestor of master — NOT evidence that master implements it (exit=1; no ref names it)
  git merge-base --is-ancestor f82777a9 master → exit=1
exit=1                     ← 红：这正是「no ref contains it」假前提的来源

$ probe-01a12d30/gopi-after task archive --verify-commit f82777a9f --repo probe-01a12d30/real3
integration branch: master
measured against: refs/remotes/gitPushcheck/master
f82777a9 (f82777a9f) IS an ancestor of refs/remotes/gitPushcheck/master — usable as evidence that … implements it
   (exit=0; the delivery lane's mirror was 62 commit(s) ahead of refs/heads/master with nothing in return, so it decided)
  git merge-base --is-ancestor f82777a9 refs/remotes/gitPushcheck/master → exit=0
exit=0                     ← 绿
```

单测（真 git、真仓库，`internal/tools/integration_claim_target_test.go`）：

- `TestIntegrationClaimTargetRefReadsTheMirrorWhenThePeerIsAhead` —— 绿腿：镜像严格超前 ⇒
  判在镜像上，同一 commit 由「拒绝」翻成「已落地」，且渲染出的命令指名镜像。
- `TestIntegrationClaimTargetRefFallsBackWithoutAMirror` —— 对照臂：无镜像 ⇒ 判在本地 master，
  与修复前一致（防止「一律问镜像」的另一种错）。
- `TestIntegrationClaimTargetRefIgnoresADivergedMirror` —— 分叉（lead>0 且 behind>0）⇒ 回退。
- `TestIntegrationClaimTargetRefOnAnUnreadableRepoAbstainsToTheLocalRef` —— 非仓库/空路径 ⇒ 回退（Rule 15）。

## 5. 诚实边界（Rule 26 推论）

- 修复只覆盖**经由 `MasterClaimEvidence` 的读者**（seal 期报告裁决、dispatch 期简报复测、
  `gopi task archive --verify-commit`）。**直接手敲 `git merge-base --is-ancestor <c> master`
  或 `git branch --contains` 的人仍会拿到陈旧答案** —— 没有任何机制让那个 ref 变新鲜，这一点没变。
- 本机 `refs/remotes/peerbox/master` 也陈旧（`1508e3821`，与本地 master 同点），**本次不动它**
  （`devdoc/premerge-merged-state-test-run.md:156` 的 decline 仍然有效）。
- 修复不改变任何门禁的通过/拒绝（没有新 gate，只有判据 ref 的解析）。

## 6. 使用纪律（给下一个判可达性的人）

1. 判「某 commit 是否已落地」**不要用裸 `master`**；用 `gopi task archive --verify-commit <rev>`
   ——它打印 `measured against: <ref>`，把「问的是哪个 ref」写进证据（Rule 5/31）。
2. 若必须手敲 git：先 `git rev-parse refs/heads/master refs/remotes/gitPushcheck/master` +
   `git rev-list --count refs/heads/master..refs/remotes/gitPushcheck/master`；**差值 > 0 时以镜像/
   `git ls-remote <peer> refs/heads/master` 为准**。
3. 「no ref contains it」这句话**必须注明在哪个 ref 集合上测得**（`for-each-ref --contains`
   的输出天然带 ref 名，别把它压成一句布尔）。
