<!-- doc-class: normative -->
# 自交付测试的三条红：fixture 侧错，产品侧对

**日期**：2026-10-27 · **ground**：`01a15b94-…-r1`（clone ground）· **判定**：`fixture 错 / 产品对`

## 一、结论（一句话）

`TestSelfDelivery*` 三条在 `make test-budget PKGS='./internal/tools …'` 下报
`failed=[push: ssh: Could not resolve hostname peerfixture: Temporary failure in name resolution]`，
**错在 fixture 的接缝，不在产品的 `sync_peers` 语义**：fixture 把本地裸库声明成 git **remote 名**，
而交付腿送给 git 的从来是 **`<alias>:<repoTail>` 这条 ssh wire form**。两者是两个名字空间，
只有巧合时才指向同一处；本地裸库按构造成不可达。

**因此不修产品。** `sync_peers` 继续只接受 ssh config 别名（`internal/config/config.go`），
`peerRemoteTargetFor` 继续在两条臂上都输出显式 ssh 形态（`internal/tools/peer_remote_target.go`
的 `SSHTarget` 文档承载实测理由）。修的是 fixture：让**行自己的仓库**用 git 自己的
`url.<绝对裸库路径>.insteadOf` 声明"你将要拿到的这条 wire form 指到我的本地裸库"。

## 二、可再 derive 的判据（三条探针，任何时刻可重跑）

脚本：`~/.gopi/sessions/01a15b94-8a33-71c1-8ae4-3762625fa80b-r1/evidence/probe-namespace.sh`，
输出同目录 `probe-namespace.out`。不用任何测试框架，只用 git 本身 —— 这样"产品对不对"与
"测试怎么写"两件事不互相污染：

| 探针 | 命令 | 实测（本机，git 2.43.0） | 含义 |
|---|---|---|---|
| **A** | `git push 'peerfixture:~/work/GoPi' HEAD:refs/heads/a` | `ssh: Could not resolve hostname peerfixture: Temporary failure in name resolution`，**exit=128** | 链真正送给 git 的形态；在本机（以及任何一台没有该 ssh 别名的主机）都不可达 |
| **B** | `git push peerfixture HEAD:refs/heads/b`（该仓库确实配置了这个 remote） | `* [new branch]`，**exit=0** | remote **名**在配置了它的仓库内是可解析的 |
| **C** | 先 `git config 'url.<绝对裸库路径>.insteadOf' 'peerfixture:~/work/GoPi'`，再跑 A | `* [new branch]`，**exit=0** | git 自己的 rewrite 能把这**一条** wire form 改写到本地路径 |

**判据**：A 红 / B 绿 / C 绿 ⇒ fixture 用错了接缝（它假设链会 push remote 名）；
产品侧的目标构图自洽且按设计输出 ssh 形态。若 A 与 B 同时红，才是产品/主机侧的问题。

**RED 回归证据**（同一判据的测试侧复现）：把 fixture 的 rewrite 腿单独关掉
（`gitRun(... insteadOf ...)` 一行换成 `_ = wire`），三条立刻回到原始句子
——`failed=[push: … exit status 128: ssh: Could not resolve hostname peerfixture …]`；
恢复后 `git hash-object` 与 `git rev-parse HEAD:<path>` 相同（`a225438d…`）。
因此这三条测的**正是**那条 rewrite 腿，不是别的东西。

## 三、为什么产品不该让步（按 Rule 43 的引用分布看）

- `sync_peers` 的契约写在 `internal/config/config.go`：它是 **ssh config 别名**，只用来交换
  task-ledger 行。让 `ResolveSyncPeers` 也接受 git remote 名，等于把"本机 ssh 拓扑"和
  "每个仓库自己的本地设置"合成一个字段 —— 而每个交付腿跑在**不同的仓库**里
  （行自己的 worktree / ground 的 repo），remote 名在那里根本不解析（`peer_remote_target.go`
  的 `SSHTarget` 文档记了实测：`fatal: 'peer1' does not appear to be a git repository`）。
- 也就是说：产品若改成"优先 remote 名"，只是把今天这句 ssh 句子换成另一句 git 句子，
  一个仓库之后重新犯同一个错。

## 四、fixture 的修法形状（复用，不要重新设计）

```go
wire := selfDeliveryPeerName + peerRemoteTailSep + selfDeliveryPeerTail()
gitRun(t, repo, "config", selfDeliveryPeerWireKey(peerDir, selfDeliveryPeerTail()), wire)
```

- key = `url.<peerDir>.insteadOf`，value = 链会送给 git 的**那一条** wire form；
- **tail 必须是调用时的函数**（`selfDeliveryPeerTail()`），不能是包级 `var`：包级变量在
  package init 时求值，早于任何 `t.Setenv`，会读到操作员 profile 的路径而不是 fixture 的
  settings，两侧值不同 → rewrite 不命中 → 三条重新变红（本 ground 上实测过）；
- **失败臂不要改 `git remote set-url`**：链从不 push remote 名，那个值没有任何腿读，
  臂会仍然死在 ssh 解析上而不是死在该臂要表达的地址上。改**同一条 rewrite 接缝**：
  `--unset url.<good>.insteadOf` 然后装 `url.<tmp>/no-such-peer.git.insteadOf '<alias>:<tail>'`，
  git 自己的句子就会点名 `no-such-peer.git`（断言 grep 的就是它）。

## 五、明确不做的事

- **不写 `/etc/hosts`、不写 `~/.ssh/config`**（Rule 35：那是新的持久依赖，而且测的是 ssh
  路由，不是交付链）；
- **不引入新产物**：rewrite 是仓库本地配置，随 `t.TempDir()` 一起消失；
- **不动 `peerRemoteTargetFor` / `ResolveSyncPeers`**。

## 六、本 run 的实测

```
Ran:    go test ./internal/tools/ -run 'TestSelfDelivery' -count=1 -v  # selector-ok: this line records WHAT WAS RUN on 2026-10-27; the test file was deleted 2026-10-31 (task 01a19052, alone-deletion probe), so the selector is a historical citation and cannot match again.
Result: 5/5 PASS（含三条目标臂）；ok github.com/nick/gopi/internal/tools 3.674s
Ran:    make test-budget PKGS='./internal/tools'
Result: 见 task-report.md（无任何 peerfixture 解析失败）
Ran:    bash <session>/evidence/probe-namespace.sh
Result: A exit=128 / B exit=0 / C exit=0  ⇒ fixture 侧错
```

`census-evidence-ok: 本页不含项目级统计数字，只有本 run 的实测记录（Rule 38 narrative）。`
