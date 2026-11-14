<!-- doc-class: normative -->
# 被正确拒绝之后：游离提交的命名与发布路径

## 这个缺陷的形状

2026-10-24 的一次 `gopi chat` 在主工作区做了一次 commit，然后 push 被 REF MOVE 守卫
正确地拒绝。三道门各自都是对的：

1. **Rule 37** 说 chat 不提交 —— 但 2026-10-24 操作员裁决让 chat 可以 edit / stage /
   commit，所以那枚提交是**合法**产生的。
2. **REF MOVE 守卫** 正确拒绝了 chat 的 `git push`（移动 ref 是操作员的决定，任何
   posture 都不能代替），所以那枚提交**没有去向**。
3. **`make build`** 正确构建 HEAD —— 而 HEAD 正是那枚本地提交，于是装进
   `/usr/local/bin/gopi` 的二进制带着 `revision d50f746d7`，一枚**在任何服务器上都不
   存在**的提交。

这条链上没有一道门是坏的。缺的是**谁都没有定义过的一环**：push 被正确拒绝之后，本地那枚
提交仍然停在一个没有任何 remote-tracking ref 能到达的分支上，而**没有任何人说出来**。
run 结束时它以为交付了；机器上跑的二进制来自没人评审得到的代码；这个事实要么永远不被发现，
要么由人在事后手工比对 revision 才发现。

## 所以修的是这一环，不是那三道门

约束（PA 2026-10-30 裁决，memory #20210）：**不放宽 REF MOVE 守卫，不改 Rule 37 的
「chat 不提交」命题**。两条都保留 —— 它们是正确的。要补的是它们**之间**那一段：

> 当一枚提交在本地落地、而它不在任何发布目标（任何 `refs/remotes/*`）的祖先里时，
> 在它产生的那一刻点名它（sha + 分支 + ahead 数），并给出发布路径。

## 落点：bash 工具的**结果**，不是 stderr

机制在 `internal/tools/bash_unpublished_commit.go`，由 `bashInDirCtx` 的第 14 步调用
（`internal/tools/bash.go`）。为什么是这个缝：

- **每一个 run 的 commit 都经过它。** `git commit -m "<subject>"` 是每个 run 的做法，
  这与 `git_commit_task_trailer.go` 选同一个缝是同一个理由：它能看到命令。
- **结果通道同时覆盖两类读者。** 工具结果既回到**执行这条命令的模型**（同一个回合里就能
  处置），也写进**该 run 的 session JSONL**（事实活过上下文）。stderr 的告示只对盯着终端
  的人可见 —— 恰恰是这次事故里不存在的那个人。
- **它不改任何状态。** 两个只读 git 探针 + 拼一个字符串；不 push、不动 ref、不建台账行。

## 判据，以及为什么是三个分开的条件

```
unpublishedCommitNotice(command, workdir, exitCode):
  ① exitCode == 0                       —— 失败的 commit 什么都没创建
  ② 命令里有一个创建提交的 `git commit`   —— 只读命令不是任何事的证据
  ③ 当前进程是 chat 的子进程              —— 唯一没有发布路径的 surface
  ④ HEAD 不可达（ahead > 0）             —— 全部的重点
```

**判据本身**：`git rev-list --count HEAD --not --remotes`。它回答的是「HEAD 上有多少
提交，任何 `refs/remotes/` 下的 ref 都不包含」—— 这正是「没有发布目标能收到它」的定义。
不用 `origin/<branch>` 拼名字：clone 的 refspec 决定它保留哪些远程 ref，把名字写死会在
单分支 clone 上量到一个冻结的值（`internal/devworkspace/delivery_anchor.go` 记着这个
实测）。

**条件 ③ 是防假阳性的一半，而不是礼貌。** 同一个仓库状态在 worker 的 ground 上也是真的
—— 从它 commit 到 clone 通道投递 push 之间的几分钟里。在那里印「这个 checkout 没有发布
路径」是**假的**：投递环本身就是那条路径（Rule 6c，`clone-channel: attempt N/M: push
accepted`）。印一句假话比不印更贵，所以告示门控在「承诺为真的那个 surface」：**chat 的
子进程**（`config.ChatSurfaceIdentityEnv`，`gopi chat` 在 arm 时写进自己的环境，bash
工具的子 shell 继承它）。chat 没有分支、没有 ground、没有投递环（Rule 37），所以停在
chat 里的提交真的无处可去 —— 而这道门**零成本**：worker 的子进程根本不带这个变量。

变量畸形读作「没有 chat 祖先」，不读作拒绝：这个变量不授权任何东西
（`config.ReadChatSurfaceIdentity` 的契约），证明不了自己是 chat 子进程的 run 就按不是处理
—— 告示是一条 provenance 断言，这份代码从不发明它。

## 它说什么

```
[unpublished commit] <sha> on branch <branch> is reachable from NO remote-tracking ref
(`rev-list --count HEAD --not --remotes` answered <N> in <dir>), so this checkout has no
publication path for it: a chat's `git push` is a REF MOVE and is refused by design, and
nothing here moves a ref on its behalf. NOTHING WAS PUSHED — the commit stays local until
one of these runs:
  - file the work: `gopi task submit "<what this commit does>"` — its worker commits in its
    own ground and the clone channel's delivery loop publishes it;
  - hand the sha to the operator and let them decide where it goes: <sha>
```

三件事是一起给出的，缺任何一件都不构成修复：**sha**（读的人要能复制才能行动）、
**测量命令与它的答案**（读者能自己重跑，而不是相信一个结论）、**两条出路**（一个没有出口的
死胡同是一条报告，不是修复）。

## 实测（2026-10-30，本 ground）

真实仓库状态 + 真实入口（不是 `-h`，不是单元测试）：在
`/home/user/workspace/gopi-test-scratch/rule31/clone`（一个真的 clone 自本地 bare
origin、`refs/remotes/origin/master` 存在的仓库）里跑

```
/home/user/workspace/01a183d9-cd89-77f7-99dc-7c93ddc934b8/bin/gopi chat --once \
  -p "…create rule31.txt… git add rule31.txt && git commit -m 'rule31: unpublished commit probe'…" \
  --tools --policy=allow
```

该 run 的 session（`faed98b0-ccbc-4d4f-bb10-63847272e35d`）里，bash 工具的
tool_result 尾部就是上面那段告示，sha = `9e12a110fb36de29046cdadbd5cfa02e36b4aca9`，
`answered 1`，目录就是那个 clone。模型在同一个回合里读到它，并在回答里写明「这个 commit
只在本地，`rev-list --count HEAD --not --remotes` 为 1，origin 上没有它」。

## 它故意不做的

- **不 push、不动 ref、不建行。** REF MOVE 守卫与 Rule 37 原样保留（约束 1）。
- **不新增依赖。** 没有脚本、没有定时器、没有常驻进程、没有新配置键（约束 2）：一个环境
  变量（已有的）、两个只读 git 探针、一个字符串。
- **不加工具、不改工具面。** 只多了一段输出（约束 3）。
- **不把「没有发布路径」说成「push 失败」。** 句子里的 `NOTHING WAS PUSHED` 是硬编码常量
  `unpublishedNothingPushedClaim`，因为一句被读成「推送失败了，去查网络」的告示会把人送去
  追鬼 —— `internal/devworkspace/delivery_anchor.go` 记着同一形状的代价。
- **不改 Rule 37 的正文。** 规则区文本与实现之间的矛盾需要操作员给措辞（上一轮
  `01a16b81` 已如此判定），本轮不擅自动 `PROJECT.md`。

## 红证明

`internal/tools/bash_unpublished_commit_test.go` 四个 arm：

| arm | 断言 |
|---|---|
| `…NamesTheCommit` | 真实 clone 里一次真实提交后，结果里有 sha、分支、`answered 1 in <dir>`、`gopi task submit`、`NOTHING WAS PUSHED`；断言之前先证明 fixture 真的处于被测状态（`rev-list --count` == 1） |
| `…SilentForAWorkerGround` | 同样的状态、同样的命令、没有 chat identity → 不印「no publication path」 |
| `…SilentWhenReachable` | 提交在**同一条命令里**就被 `refs/remotes/origin/published` 收到 → 不印 |
| `…SkipsNonCommittingCommands` | 只读命令在同样的 ahead 状态里不印；同一状态下的提交命令照印 |

**红证明实测**：把 `bash.go` 第 14 步对 `unpublishedCommitNotice` 的调用去掉后，
`…NamesTheCommit` 与 `…SkipsNonCommittingCommands` 变红（「the result never named the
commit …」），两个 silence arm 保持绿 —— 这正是它们作为 silence arm 而不是正例副本的意义。
恢复后四个全绿。

## 一个在实现中量到的坑

第 2f 步会把命令**改写**（注入 `--trailer`），而 `gitCommitInsertionPoints` 对被注入过的
命令按设计返回「没有插入点」。所以在第 14 步用**改写后**的文本问「这条命令创建提交了吗」，
问的是调用者从没写过的那根字符串，答案是错的 —— 实测：告示在它唯一存在的那个命令形状上
**从不触发**。修法是第 1 步把调用者原文留一份（`callerCommand`），第 14 步问原文。
