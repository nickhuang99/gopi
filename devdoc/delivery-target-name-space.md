<!-- doc-class: normative -->
> **历史（本页保留记录，不再是指令）。** `internal/tools/merge_config_anchor_gate.go`
> 与 `internal/tools/merge_config_anchor_gate_test.go` 已在提交 `5a35f6104`
> （`internal/docclass` 拆除，slice 3/5）中被整份删除。下面出现的这两个路径、以及
> `TestMergeConfigAnchorGate*` 三个臂，都是**当时的记录**：文件已不存在，
> 照抄那些命令只会得到「文件不存在」或 `[no tests to run]` 的空绿。
>
> **Retired mechanism (documentation-citation teardown, 2026-11-05).** 这一段原先接着写
> 「现在真正在跑的检查是 census 生成器自己的 `--check` 腿」——**那个检查也已经没有了**。
> `78bdf05a` 与 `b8cad761` 删掉了整个 documentation-citation 家族：三个生成器
> `internal/standards/testdata/config-anchor-census.go`、`citation-census.go`、
> `citation-selector-census.go`，它们写的三张 `.tsv` 表，驱动它们的
> `internal/standards/citation_gate.go` / `citation_selector_gate.go`，
> `internal/configanchor` 包（`anchor.go` 随该家族删除；`census_tables.go` 里最后两个
> 路径常量在 `1e3b0976` 里随那两道零调用方的冻结门一起删除），以及交付路径上的
> census-citation 腿
> （`internal/devworkspace/census_gate.go`、`census_redrive.go`，`0ee5745c`）。
> **本段没有任何一条命令还能跑**，`[no tests to run]` 加 exit 0 是空绿不是通过。
> 今天仍在跑的语料门是：
>
> ```bash
> go test ./internal/standards/ -run RetiredPipeline -count=1
> go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader -count=1
> ```

# 交付目标：SSH 别名与 git remote 名是两个名字空间

**日期**：2026-10-16 · **分支**：`task/01a141613b4779a798ad0f89cbb7f24f` · **基线**：`61b0846`

**census-evidence-ok: 本文件不含项目级统计数字（无「N 个文件 / N% 调用」一类整体性figures），只有本 run 的实测记录，按 Rule 38 属 narrative，不引生成器。**

## 一、缺陷是什么（实测，不是推断）

客户端收尾链的交付目标由 `finishCleanupServerTarget()` 组装，原实现是：

```go
peers, err := config.ResolveSyncPeers(nil)   // settings.json 的 sync_peers（SSH 拓扑）
if _, err := ValidateSSHPeerAlias(peers[0]); err != nil { return "" }
return fmt.Sprintf("%s:%s", peers[0], finishCleanupPeerRepo)
```

它把 **SSH 拓扑别名**当成 **git remote 名**用。两者没有契约：

- 别名由 **ssh** 解析（`~/.ssh/config`，是**本机**的配置）；
- remote 名是 **每个仓库自己的本地设置**（`git remote add`）。

只有巧合时它们才一致。hosta 上 `sync_peers` 是 `["peerbox"]`，而 `git remote -v` 只有 `origin`
—— 于是每条交付腿都死在 git 自己的句子：

```
fatal: 'peerbox' does not appear to be a git repository
```

这句话既没说是哪个**声明的 peer**，也没说本仓库**实际有哪些 remote**（这是本 epic 的直接起因）。

## 二、修法：一个解析器，两条臂，两种都不是失败

新增 `internal/tools/peer_remote_target.go`，**唯一入口** `peerRemoteTargetFor(ctx, dir, alias, repoTail)`：

| 臂 | 条件 | 目标 | 理由 |
|---|---|---|---|
| **remote 臂** | 本仓库配置了同名 remote | **remote 名**（如 `origin`） | 由 git 自己解析：`remote.<name>.url` / `pushurl` 生效，交付腿不再自己拼字符串 |
| **fallback 臂** | 本仓库没有同名 remote | 显式 ssh 形态 `<alias>:<path>` | 只是拓扑里**只有 ssh 名字**的合法形状 —— 是**解析结果**，不是错误；并且**具名诊断**（含本仓库真实 remotes 列表 + `sync_peers` 键名） |

解析结果与它的句子**一起**产生（`peerRemoteTarget{Remote, Rendered, Reason}`），
所以没有调用方可以「报告一个它没解析出的形态」（Rule 14 / Rule 15）。

### 2.1 「问哪个仓库」—— 这是一个被测出来的真问题

解析必须问**这一行要被交付的那个仓库**，不能问进程的 cwd：remote 是**按仓库**的设置，
而 CLI 面（`gopi task finish-cleanup` / `gopi task retry-delivery`）是在**operator 的 checkout**
里被调用的，被交付的分支却在 task worktree 里。

这不是推理，是本轮**实测**：`bin/gopi` 从本仓库（GoPi，remote 只有 `gitPushcheck`）被调用时，
第一次真实执行**落到了 fallback 臂**，为一份**明明有 `origin` remote 的目标仓库**去拨 ssh。

修法：`finishCleanupRowTargetResolution(repoDir)`（同一条解析路径，问行自己的 repo；
repoDir 为空才回落到进程自己的 checkout = 普通 worker 形状）。调用点：
`taskFinishCleanupChain` 的 push 步骤、`task_delivery_retry` 的 ls-remote/fetch/push 腿
（后者用 `finishCleanupMainRepoDir(row.Repo)`）。

### 2.2 失败时说清楚「你在哪条臂上」

- `gitPushbackTargetFailureDetail`（原有，**所有 push 路径共用**）继续负责「名字不匹配 + pushurl」两种可测成因；
- 新增 `peerRemoteTargetFailureDetail` 在其上追加**解析自己的句子**，且**只在 fallback 臂**追加
  —— 因为 remote 臂下目标本来就是本仓库拥有的名字，问题属于该 remote 的 URL/pushurl，上面两条已覆盖；
- 收尾报告新增 `push-target` 步骤：把「走了哪条臂、为什么」记在 push 发生的地方；**caller 注入的 target**
  （夹具缝）被**明说是注入的**，并附上拓扑自己给出的答案，绝不把注入值说成解析值。

## 三、验证（Rule 31：改了的面必须真跑）

### 单元 + 交付腿（`internal/tools/peer_remote_target_test.go`）

```bash
go test ./internal/tools/ -run 'TestPeerRemoteTarget|TestDeliveryTarget' -count=1
```

- `TestPeerRemoteTargetNamesTheArmAndThisReposRemotes` —— 两条臂的**解析结果与句子**（remote 臂：目标是 remote 名，句子点名 remote 名/别名/本仓库 remotes；fallback 臂：目标是 `<alias>:~/work/GoPi`，句子含别名、目标、`sync_peers` 键、本仓库真实 remotes；**无任何 remote 的仓库必须打印具名的「无」**；空别名 ⇒ 空目标，绝不猜）。
- `TestDeliveryTargetUsesTheReposOwnRemoteName` —— remote 名 ≠ 声明的别名时**仍然交付**：目标 = remote 名，且分支**真的**出现在对端（读对端，不读推送的退出码）。
- `TestDeliveryTargetFallsBackToSshFormAndDeliversTheBranch` —— fallback 臂真的交付：git 真的起了 ssh，仓库自带的 relay shim（`testdata/ssh-sentinel-shim.sh`）真的跑 `git-receive-pack`，哨兵里**真有该 ref**；ssh argv 日志证明**拨的正是声明的别名**。
- `TestDeliveryTargetFailureNamesTheArmAliasAndRemotes` —— 失败时 ref 不得说出：臂、别名、`sync_peers` 键、本仓库 remotes 四项全在原因里，且对端 ref 未被碰、run 的 ground 未被消耗。

### 真实执行（`bin/gopi`，不是 `-h`）

脚本 `~/workspace/gopi-test-scratch/prt-real-run.sh`（throwaway world：settings `sync_peers=["peerbox"]`，
客户端仓库 remote 只有 `origin`，git 的 ssh 被换成本仓库自带的哨兵 relay）。日志在
`~/.gopi/sessions/01a14161-3b47-79a7-98ad-0f89cbb7f24f/artifacts/`。

| 场景 | 命令 | 观察到的结果 |
|---|---|---|
| **① 别名≠remote 名仍交付**（epic 标题案例） | `gopi task finish-cleanup --apply --task <row>` | `done=[branch-checks,push-target,push,worktree,branch] removed=1`；哨兵 `task/prt-real = c23c4b61…`；ssh argv = `argv=peerbox git-receive-pack '~/work/GoPi'` |
| **② 别名 = remote 名 ⇒ 走 remote 臂** | `sync_peers=["origin"]`，同上 | `removed=1`；哨兵 ref 在；`upstream (origin) refs: [master]`；**ssh 日志为空**（git 走本地 remote 即已交付，未拨 ssh） |
| **③ 失败具名诊断** | `GIT_SSH_COMMAND=/bin/false gopi task finish-cleanup --apply --task <row2>` | `done=[branch-checks,push-target] failed=[push: …]`，原因同时含：别名 `peerbox`、目标 `peerbox:~/work/GoPi`、键 `sync_peers`、**本仓库 remotes = origin**；`removed=0 refused=0 settled=1`（失败不消耗 ground） |
| **④ 补投路径** | `gopi task retry-delivery --task <row2>` | `…delivered branch=task/prt-refused peer=peerbox:~/work/GoPi — the peer now advertises task/prt-refused at 449bca0a…`（同一行、同一 goal 的 retrofit 腿也真的把 ref 送到了对端） |

## 五、结构性守卫跟着委托走（第二个 commit）

`TestClientBranchPushBackTargetResolverIgnoresTaskSync` 原本只读**一个函数体**并要求里面有
`config.ResolveSyncPeers`。peer 查找搬进共享解析器后，它**按正确的理由**红了
（"the assertion above would be vacuous"），而它保护的性质仍然成立 —— 只是深了一层调用。
现在守卫走读者会走的同一条路（链入口 → 链解析 → 行仓库解析 → 别名选择器 → 唯一的拓扑读取者
`peerRemoteDeclaredPeers`），在这条路上断言三件事：task-sync 开关在**任何**一环都被拒绝；
委托是**真的**（入口必须抵达共享解析器，否则「凭空正确的目标」无法混过）；这条路的终点仍是拓扑。
它读的每个函数体都必须存在（`toolsTestFuncBody` 在函数缺失时**大声失败**），所以改名不能靠沉默通过。

## 六、本轮的第三个观察：CONFIG-ANCHOR 红与本次改动无关

分支作用域套件（`./internal/tools ./cmd/agent`）除上述测试外还有 3 个红：
`TestMergeConfigAnchorGate_{RealTreeIsClean,ARePointedCitationIsAStaleRecord,ReportsTheOffendingPathAndLine}`，
原因是 `internal/shot/filmlocale.go:268` 的 CONFIG.md 引用**行号漂移**（ROT，记录文本现在在 273 行）。
它**不是**本改动引入：本 diff 不碰 `CONFIG.md`、不碰 `internal/shot/filmlocale.go`、不碰
`internal/tools/merge_config_anchor_gate_test.go`（后者最后一笔提交是 `cc31a8f8d`）。
**这里有一处必须先纠正的读法（本轮实测推翻，Rule 41：用错依据的断言会污染后面每一个引用它的推理）**：
第一次读到时，我把它记成「上游已经看见这个条件，所以不是我的红」。**实测把它推翻了** ——
同一次运行里 merger 侧确实打印了 `[task-notify] merger: CONFIG-ANCHOR gate filed bug row: …`
（来源 `internal/tools/merge_config_anchor_gate.go:379`，该分支只在 `TaskSubmit` 返回 nil 时才走到），
但**操作者账本里 0 命中**：

    $ gopi task list 2>/dev/null | grep -c 'merge TARGET carries'    # 0
    $ gopi task list 2>/dev/null | head -2                            # 1426 task(s):  ← 默认视图含全部状态

所以正确的读法是：**这条 ROT 在 base 上真实存在、可复现、且没有任何一行账本记得它**；
「已提行」只是一句日志。已经按 Rule 41 就这一条单独提了一行（`01a142da-dada-…`，payload 见
本 run 的 `artifacts/bug-config-anchor-rot-unrecorded.md`）。这条记录仍然只是为了
让「套件红」不被误读成本 diff 的判决 —— 但它**不能**被读成「有人已经在管了」。

**归属判据（实测，非推断）：在 base 的独立 detached worktree 上逐字复现。**
`--stat` 为空只证明**文件字节相同**，不证明**这份红是 base 状态**；把不变量换成「在 base 树上跑同一个测试拿到同一批失败」
才是判据（Rule 41：推断不得充当判据）：

    $ git worktree add --detach <tmp> 61b084683          # base 自身的树，不含本分支任何提交
    $ git -C <tmp> rev-parse HEAD                        # 61b084683
    $ git -C <tmp> status --porcelain                    # (empty)
    $ cd <tmp> && go test ./internal/tools/ -run 'TestMergeConfigAnchorGate' -count=1
    --- FAIL: TestMergeConfigAnchorGate_RealTreeIsClean (0.04s)
    --- FAIL: TestMergeConfigAnchorGate_ReportsTheOffendingPathAndLine (0.71s)
    --- FAIL: TestMergeConfigAnchorGate_ARePointedCitationIsAStaleRecord (0.86s)
    FAIL    github.com/nick/gopi/internal/tools        rc=1

三条**同名、同失败句子、同 ROT 行**（`internal/shot/filmlocale.go:268`，记录文本现在 273 行）在 base 上复现
⇒ 这份红是 base 状态，与本 diff 无因果关系。原始输出留在本 run 的
`artifacts/config-anchor-reds-reproduced-on-base.txt`。**它不在本分支修复**（同一条 ROT 的修复属于 `CONFIG.md` 引用
重新生成，是另一件事，已提行）。


- **`gitPushbackTargetFailureDetail` 的门仍是「实测 probe」而非 git 措辞**：本轮实测确认同一不可解析的 peer 在不同 verb 下句子不同（`git fetch <alias>:<path>` 走 ssh 说 “Could not read from remote repository”，push/本地路径说 “does not appear to be a git repository”）。这条保持原样（probe 为准），未做进一步收敛。
- **hosta 的 `--alias peerbox` 路径下 `~/work/GoPi` 并不存在于对端**：本轮的真实执行把 ssh 换成了本仓库自带的哨兵 relay，证明的是**交付腿与目标解析这一层**通了；它不证明 peerbox 上真有一个可接收的 `~/work/GoPi`（那是拓扑事实，不是本改动的面）。
- **未跑全树测试**（Rule 27：merge/verify 路径只跑分支作用域 smoke）。

## 七、修正（2026-10-16，regression 行 `01a14305-4031-78c0-8371-db6bdc34f178`）

本文件 §二 的表格把 **remote 臂的目标**写成「remote 名（如 `origin`）」，并据此让
`finishCleanupServerTarget()` 返回裸 remote 名。**这一条被本仓自身实测推翻**（Rule 41：用错依据的
断言会污染后面每一个引用它的推理，所以在这里就地改正，而不是留给下一个读者去撞）：

**实测**——在一个**没有**配置该名字的仓库里（每一支真实推送腿所处的状态）：

```
$ git fetch peerbox     # 该仓库无 peerbox remote
fatal: 'peerbox' …
$ git push  peerbox
fatal: 'peerbox' does not appear to be a git repository
```

**机理**：remote 名不是传输方式，它是**每个仓库自己的本地区名**，只在声明它的仓库里可解析。
而交付腿是把 target 交给 **row 自己的仓库**里的 git（`finishCleanupMainRepoDir` / ground 自己的
仓库），不是交给解析发生的那个 checkout。于是「remote 臂返回裸名」把本 epic 要消灭的那句
`fatal: 'peerbox' does not appear to be a git repository` **在下一个仓库里重新造了出来** ——
这同时解释了为什么本仓（`git remote -v` → `peerbox`）上两个既有测试必然翻红。

**修正后的契约**：`peerRemoteTarget.SSHTarget` —— **两条臂交给 git 的都是**
`<alias>:<path>` 这个**唯一** wire form；remote 臂**不改变 wire form**，它的贡献是
**别名选择**（声明的 peer 是本仓 git 真的知道的名字）与**具名诊断**（`Reason` 同时点名
找到的 remote 与交出去的形态）。`Rendered` 仍是「本仓把这个 peer 叫什么」的仓库级答案，
供需要**展示解析**的读者使用；**push / fetch / ls-remote 这三条腿读 `SSHTarget`**：
`finishCleanupServerTarget()`、`task_delivery_retry.go` 的投递通道、收尾链
`push-target` 步骤的「是否被 caller 注入」判据（该判据用 `Rendered` 会把一条**正常的生产推送
误报成注入**，所以同样改读 `SSHTarget`）。

**随之作废的小节**：§2.1 里「fallback 臂是 hosta 形状的**失败/次优**路径」这一读法作废 ——
两臂现在**等价地**是解析结果，区别只在**能被诊断到什么程度**；§三 表格里「② 别名 = remote 名 ⇒
走 remote 臂、ssh 日志为空」不再成立：该场景现在同样经 ssh 形态投递（wire form 恒定），
它的断言改成「解析能找到该 remote（`Remote` 非空）+ 诊断点名它 + 分支真的落地」。

## 八、在 7ecfa12 上重述（2026-10-17，rework 行 `01a144d1-1440-70bd-a956-3bdd5daf8c08`）

§七 的修正在原分支 `task/01a14305…`（commit `22d4d55f2`）上落地后，落地分支随后被删除。本节是
**同一契约在新基线 `7ecfa12`（peer 的 `refs/remotes/gitPushcheck/master`，即本 run 的 cut point）
上的重述**，并且是**手工冲突解决**的结果，不是机械 cherry-pick —— 因为基线在这期间多了一条
同主题的兄弟改动：§`peerRemoteRepoIsEstablishable`（"这个目录**根本不是** git 仓库"与"这个仓库
**没有**同名 remote"是两种条件，前者必须**具名拒绝**而不是构造 `<alias>:<path>` 去拨一个没人测过的
地址）。

**两处冲突的裁决（逐条，可复核）**：

1. **`peerRemoteTargetFor` 的先后次序** —— 兄弟侧要求先 probe 仓库可直接取（否则 `Reason` 走
   no-repo 臂、答案为空），本改动要求 `SSHTarget` 在**两条臂分开之前**合成。二者不冲突，正确形状
   是**次序**：先 probe → 仓库不可建立则返回空（兄弟侧的合同）→ 建立之后立刻合成
   `SSHTarget`（本改动的合同）。合成放在 probe **之后**，所以"不可回答的仓库"仍然答**没有目标**，
   而不是拨一个没人测量过的路径。
2. **`finishCleanupServerTarget()` 的文档与返回** —— 保留兄弟侧"一个主语一个答案"（`ForRepo` 是
   唯一带仓库的面），同时保留本改动的**解析不是本函数自己拼的**段落，返回 `SSHTarget`；理由段落里
   的 empty 情形补上第三种（仓库无法建立）。

**一处不改注释就漏掉的腿**：`taskFinishCleanupChain` 把目标传给 `runFinishCleanupChainTarget` 时
读的是 `finishCleanupServerTargetForRepo(d.Repo).Rendered`，而 `finishCleanupServerTarget()`、
投递重试通道、`push-target` 步骤的注入判据都已改读 `SSHTarget`。冲突解决期容易只合并**被 git 标红
的两块注释**而漏掉这第三处：`Rendered` 在 remote 臂上是**裸 remote 名**，于是收尾链会把裸名交给
git —— 正是本 epic 要消灭的那句话。改后该行读 `SSHTarget`。

**断言升级（这是本 run 的交付物 2）**：`TestDeliveryWireFormHoldsOnBothArms` 把契约钉在**腿**上，
而不是钉在解析结构体上 —— 它跑**真实的收尾链**（`taskFinishCleanupChain`，即 `--apply` 用的同一个
dispatcher），读 `push-target` 步骤自己的 `Target`（那是这条腿解析出来并交出去的字符串），两条臂
（remote 臂 = 本仓配置了同名 remote；fallback 臂 = hosta 形状）**都**断言它是
`<alias>:<path>`，并且断言分支**真的经 ssh 形态落地**在哨兵仓库、ssh argv 里带着声明的别名。

- **为什么要有这条腿级断言**：字段对 ≠ 腿带上了它。本 epic 的缺陷正活在这个缝里 —— 解析的 remote
  臂答裸名，而三条腿各自要**记得**读另一个字段才不出错。把「步骤自己的 Target」钉住，这个缝就无法
  再被表达出来。
- **RED 证明（实测）**：把 `taskFinishCleanupChain` 那一行改回 `.Rendered`，该测试**翻红**：
  `the push leg must carry the wire form "gopi-leak-shim:/home/user/work/GoPi" on this arm, got "gopi-leak-shim"`；
  恢复后 `git hash-object internal/tools/task_finish_cleanup.go` = `bf9ee6532a00…`（`sshtarget-restore-identity.md`：
  恢复以**内容同一性**判定，不以索引差异判定），同一测试转绿。
- **夹具命名坑（下一个读者会踩）**：subtest 名不得带括号/逗号。`t.TempDir()` 用测试名派生目录名，
  而 git 把 `GIT_SSH_COMMAND` 交给 `sh -c` 执行 —— 名字里的括号会让 shim 报
  `Syntax error: word unexpected`，看起来像产品失败，实际是夹具命名。

**第二条腿也钉住了**：`TestDeliveryRetryChannelCarriesTheWireFormOnBothArms` 把同一契约钉在
**投递重试通道**（`deliveryRetryOne` → `rep.Target` → 收尾链的 push）上，两条臂各跑一次，断言
`rep.Target` 是 `<alias>:<path>`、分支**真的**被对端 advertisement、ssh argv 带声明别名。之所以要
单独一条：①/② 两个既有用例只驱动 fallback 形状（夹具配的是 `origin`），**remote 臂**才是 `Rendered`
（裸 remote 名）与 `SSHTarget` 分岔的地方。RED 证明同法：把该行改回 `.Rendered`，该用例翻红
`the retry channel must carry the wire form "…:/home/user/work/GoPi" … got "…"`；恢复后
`git hash-object internal/tools/task_delivery_retry.go` = `8b14333fb7d8…`（与变异前同一对象 id），
同一用例转绿。
