<!-- doc-class: historical -->
# 2026-11-03 夜：15 条 failed 行的判定记录

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**这一夜的工作方式是「先测再跑」，而不是「能 resume 就 resume」。**

判据只有一条：**这条行描述的现象，现在还在不在。**

在 → resume；不在 → 记下证据，不跑。因为一条 `gopi task status` 回答 `0 resumable` 只说明
「可以 resume」，不说明「值得 resume」；而一次 resume 要花掉一个 agent 的完整上下文去确认
一件可能早已被修好的事。

## 结论一览

| 行 | 判定 | 实测证据 |
|---|---|---|
| `01a182e0` 三条 cmd/agent help 门红 | 前提失效 | `go test ./cmd/agent/ -run 'Test.*Help.*'` → `ok 14.525s` |
| `01a18235` heavy-tests.tsv 未重生成 | 前提失效 | `testdata/heavy-tests.tsv` 存在；`go test ./internal/testutil/ -run 'Rule27\|Heavy'` → `ok 79.152s` |
| `01a1839c` git 2.34.1 无 `--write-tree` | 前提失效 | git 2.43.0；`TestDeliveryLoop` 四条 PASS |
| `01a1823e` internal/standards 六条既有红 | 前提失效 | `make test-budget PKGS='./internal/standards'` → `exit=0 outcome=pass`，276 arm |
| `01a1827c` `TestZZRedProof_ARecordForAnotherTip` 红 | 前提失效 | `ok 3.521s` |
| `01a182f1` `publishGround` 承诺两条 refspec | 前提失效 | 签名已无 `ground` 参数；`TestPublishGround*` → `ok 0.512s` |
| `gopi notify` 被误判 REF MOVE | 前提失效 | 实测 `gopi notify --session <self>` 成功入队且本回合收到 |
| DOC-INDEX.tsv 一簇（5 条） | 前提失效 | `git ls-files devdoc/DOC-INDEX.tsv` → 0；5 处命中全为注释 |
| Rule 42 `signal.Reset` 四处 | 前提失效 | 18 处命中中 14 处注释、2 处 fixture 字符串、1 处 `t.Cleanup` 复位（无 re-raise）、真实违规 0 |
| `signal-repeat` 写入位置 | 前提失效 | 常量在 `signalrec.go:223`，回归测试锁住（`signalrec_test.go:127/137`） |
| `session replay` 的 `⚙ ` 计数 | 前提失效 | 回归测试在 `cli_session_replay_turnround_test.go:256`（注释引 `41e5dade`） |
| `01a18377` worker 跑已删除二进制 | **仍然成立** | 5 个进程 `/proc/<pid>/exe → (deleted)`；resume 未产生提交 |
| `01a183d5` delivery-proof-unverified 缺账本事件 | 无法验证 | 需读 HP 账本，本机无该账本 |

**十四条前提失效，一条仍然成立，一条无法验证。**

## 一个必须记下的自我闭环

`01a18377` 的主题是「常驻 worker 跑着已被删除的二进制」，而我交给它的修复动作是
`gopi worker --resume` —— 启动的那个进程，自己就是从那个旧 ground 的 HEAD 上跑起来的。
它读自己的 tip（`da7152820455`，2026-10-29 的），发现该 tip 已在 `origin/master`，
于是判定 `ALREADY ON THE INTEGRATION BRANCH — carries no commit to deliver`，行被写成
`completed`，而缺陷一个字都没被碰。

最有价值的证据是它的 `verdict` 字段自己说的话：

```
the tip da7152820455 existed BEFORE this run began ... so the run produced NO commit of its own
— the question was never answered, which is not the same as an answer of "it did not land"
```

**判据已经知道，写回时没用上。** 因此另提 `01a19d78`。

## 两个我在这一夜犯下的观察错误（记下来，因为它们会重犯）

1. **读自己的工作树当成仓库的最新状态。** 我说 `01a19d57`「一个字都没变」，实际是它的提交
   `4da918a7a` 在 origin 上而我的本地落后 3 个。正确的做法是用 `git show <commit>:<path>`
   读那条提交的对象，或者先 fetch。修好后的三处锚点（`cli_verbs.go:150/:263`、
   `cli_verb_guard.go:489`）我事后核实过，全部指向真正的声明行。

2. **`go test -run` 过滤跑出来的 FAIL 可能不是代码缺陷。**
   `TestSignalRecUnreadableTrailLeavesTheKillUnclassified` 单独跑 PASS（`ok 3.260s`），
   在 `-run 'Signal|Replay'` 的组合过滤下 FAIL —— 它需要 suite 的某个前置状态。
   整包的判据只有 `make test-budget PKGS='...'`，`-run` 只能回答被挑中的那一条。

## 这一夜的交付

| commit | 内容 |
|---|---|
| `3f43206a1` | `test(standards)`：把 DUPLICATE-DECLARATION 这一类钉进门 |
| `50dc9fb2e` | `fix(agent)`：density 门按 knob 的最宽预算测量（原本是环境依赖的） |
| `6908f471b` | `fix(config)`：catalogue `GOPI_TOKENS_COUNT_*` 两个 knob |
| `6fe6716ed` | `chore(census)`：在交付通道发布的 tip 上重生成锚点普查表 |
| `a7bd41805` | `fix(clone-channel)`：already-delivered 出口记录它推过的 tip |
| `7935d49f7` | `test(cmd/agent)`：把 landed-record 那一条列进 bare-peers 覆盖棘轮 |
| `4da918a7a` | `docs(commands)`：三处漂移的散文锚点重指到它们的声明 |
| `49919a029` | `feat(chat)`：自己 checkout 上游分支的快进发布算 REPOSITORY WRITE |
| `9bd3b4adf` | `fix(chat)`：`--tags` / `--follow-tags` 仍保持 REF MOVE |
| `fd7960a63` | `fix(chat)`：在 git 实际推送的目录里测量快进条件 |

`49919a029` 的提交信息直接引用了我这条 bug 行的现场证据
（`e32617fe3` 在共享主 checkout 里没有发布路径，worker 把同样的五个文件重新实现成
`6908f471b`）—— 这是「提一条行 + 给出可复现的证据」比「直接把代码改了」更有用的一个实例：
它让执行者带着同一个现场动手。

## 一条留给下一个人的判据

**一条 `failed` 行值不值得 resume，看它的现象能不能用一条命令测出来。**

能测 → 先测，多数会发现自己已经好了（今晚 14/16）。
测不出来 → 那多半是「需要另一个节点的账本」「需要一次真实的推送」「需要一个正在跑的 worker」
这类只能在特定环境复现的，把复现条件写清楚，别让下一个 agent 重新摸索一遍。
