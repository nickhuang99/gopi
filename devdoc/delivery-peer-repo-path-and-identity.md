<!-- doc-class: normative -->
# 投递腿的 peer 路径：从声明的投递点解析，并在镜像 ref 落库前判「是不是同一个仓库」

Task `01a1734c-4ba1-7538-a6e8-980bd5eb2044`。两个缺陷，一条根因：**一个共享值被三条腿
各写了一份，而每一份都写错了；错的那一份被当成事实，于是下游所有测量都在量错的东西。**

## 缺陷（实测）

三条投递腿各自持有一个 peer 仓库路径常量，三处都写 `~/work/GoPi`：

| 腿 | 常量 | 文件 |
|---|---|---|
| serve 通道 / `gopi task push` | `gitSyncPeerRepo` | `internal/tools/task_sync_git.go` |
| harvest push-back | `harvestPeerRepo` | `internal/tools/memory_sync_task.go` |
| 收尾 push | `finishCleanupPeerRepo` | `internal/tools/task_finish_cleanup.go` |

而本机声明的是另一个拼写：

```
$ gopi config show | grep -E 'dev_repo'
  dev_repo                              "peer1:/home/user/work/gopi"
```

在 peer 上这两个路径**是两个互不相关的仓库**（`git rev-list --max-parents=0` 给出不相交的
根：12025 vs 359），于是：

1. 三条腿的 push 目标全部 non-fast-forward 被拒；
2. 镜像 ref `refs/remotes/gitPushcheck/master` 装进了**另一个仓库**的 tip，滞后告警因此报
   `DIVERGED` 并给出处方 `git merge refs/remotes/gitPushcheck/master` —— 这条处方执行了
   会把 12025 个无关提交嫁接进 master（git 默认还会以
   `fatal: refusing to merge unrelated histories` 拒绝，把人引向
   `--allow-unrelated-histories`，即嫁接本身）。Project Rule 38：处方必须可执行，且不得
   使情况变糟。

## 修复一：一处 resolver（Rule 14 / Rule 2 / Rule 15）

- `internal/config/delivery_repo_path.go` —— **env 的唯一读取点**仍留在 config 包
  （`DeliveryRepoDeclared()` 委托给既有的 `DevRepoConfigured()`，即 env-vs-settings 优先级的
  唯一读取者；Rule 2），另加 `DeliveryReposTable()` 读 `repos` 表**它自己的键**。
- `internal/tools/peer_repo_path.go` —— 顺序解析，每一臂都说清自己是谁：

  | 臂 | 来源 | 何时 |
  |---|---|---|
  | ① | 声明投递点的 path 半边（`GOPI_DEV_REPO` > settings `dev_repo`） | 有声明且解析得出路径 |
  | ② | settings `repos` 表 | 表**无歧义**：命中「正在投递的那个 checkout」，或全表只有一项 |
  | ③ | `peerRepoPathLastResort`（`~/work/GoPi`） | 前两臂都拒绝；**每次落兜底都在 stderr 宣告一次**，说明哪一臂、什么值、为什么全部拒绝 |

  ②有多项且认不出主体时**具名拒绝**，不猜（一次抛硬币决定投递目的地，是最坏的一种沉默）。
  URL 形态（`ssh://`、`user@host:path`）**不是** `<alias>:<path>` 投递点，①具名拒绝它。

三个 per-leg 常量**已删除**，所有调用点改为 `peerRepoPath()` / `peerRepoPathForRepo(dir)`，
包括读侧（`taskBranchRefsPeerTarget`、`peerRemoteRepoTail`）—— 读侧若自造路径，就是在问
另一个仓库的问题。

## 修复二：镜像 ref 落库前判同一性（Rule 38 / Rule 15）

`internal/tools/peer_repo_identity.go` —— 判据是 git 自己的
`merge-base --all <mirror> <localRef>`：**exit 0 + 有输出 = 同源；exit 1 + 无输出 = 实测的
无关**（`mergeVerifyGitExitNo`）；其它一切（128 对象不可解析、超时）是**未测量**，
既不算同源也不算异源，绝不据此下任何结论。

两个面各自接线：

- **投递腿**（`SyncPeerFetch`）：拿到 tip 之后、返回之前判同一性；异源则返回**具名错误**
  （决定性句子 + resolver 的 provenance + "修声明，不要修历史"），绝不把一个未识别的
  tip 交给下游用自己去量。未测量时放行（不在未测量的前提上拒绝投递 —— 那是同一类缺陷的
  另一个方向）。
- **滞后分类**（`gitSyncIntegrationLag`）：同一性判定**排在任何计数裁决之前**，异源则走
  第四条文本 `gitSyncLagDifferentRepoFormat` —— **不出现任何 git 命令**（缺陷是写错的路径，
  药方是一次声明，命名 git 命令正是当初那条危险处方的来源），也不出现 `DIVERGED` 字样。

同源 `DIVERGED` 的老处方**原样保留**（`task_sync_git_lag_test.go` 仍在断言它）——
把处方到处删掉是另一个 bug。

**两个方向都在真跑里被观测到**（2026-10-26，同一份二进制）：

| 场景 | `merge-base --all` | pass 输出 |
|---|---|---|
| peer 是**同一个**仓库、两边各有新提交 | rc=0 + `9a7f1b0` | `DIVERGED` + `git merge refs/remotes/gitPushcheck/master` 处方（照常） |
| peer 是**另一个**仓库（无共同祖先） | rc=1 + 空 | `is a DIFFERENT REPOSITORY … NO merge is a remedy`，无 `DIVERGED`、无 merge 处方 |

第一行不是构造的夹具：真跑时 peer 恰好前进到 `ee26e1d`，于是 DIVERGED 自然出现，
`merge-base --all` 给出非空基 —— 它证明这次的改动**没有**把同源的告警一起静音
（"到处删掉处方"也能让异源那三条断言全绿，所以这个对照是必要的）。
两行原文：`evidence/task-push-live-1698004.txt` 与
`evidence/e2e-different-repo-rerun-a5ac50c.log`。

## 验证

- 红证（先红）：把同一组断言写成 `zz_redproof_peer_repo_test.go` 放进**旧树**
  （`git archive HEAD` 的副本），两条都 FAIL，原文见
  `~/.gopi/sessions/01a1734c-4ba1-7538-a6e8-980bd5eb2044/evidence/redproof-old-tree.txt`：
  - `RED: the delivery target is "peer1:~/work/GoPi", want the DECLARED delivery point "peer1:/home/user/work/gopi"`
  - `RED: an unrelated peer's classification printed the merge prescription "git merge refs/remotes/gitPushcheck/master"`
- 绿：同一断言在新树全过（`TestPeerRepoPath*`、`TestPeerIdentity*`、`TestDeliveryRepo*`）。
- 真跑 `gopi task push` 的 reflog 取证见任务报告。
- 落定后又两个**只改自己代码、不改行为**的提交（`a5ac50c` guard clause / `1698004` 局部变量
  改名），都是标准门先在真树上点了我的新文件；`1698004` 绕开的是 standards 内容同一性 gate
  的一条**误报**（Go 变量名 `cmp` + `==` 被读成 shell 摘要比对），误报本身已按 Rule 41 提行
  `01a1750d-8953-7eff-93ff-0bc6ad3c94bf`，探针与机理见
  `evidence/bug-cmp-gate-false-positive.md`。

## 回归守则（为什么这些测试不是文本夹具）

同一性是一个**提交图的性质**，所以 `peer_repo_identity_test.go` 在真实 git 仓库里跑
真实 `merge-base`，并且**先测前提**（fixture 只有在 git 自己说"无共同祖先"时才成立 ——
区分 exit 1 与 exit 128），再用两条对照：异源必须报异源且不给处方；同源必须照常分类。
少了对照，"把处方到处删掉"也会绿。
