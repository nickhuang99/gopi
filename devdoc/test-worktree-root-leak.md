<!-- doc-class: historical -->
# 测试进程的工作树根泄漏：根因修复 + 存量回收

**日期**：2026-10-01 · **epic**：test-worktree-root-leak · **分支**：`task/01a0f44fb1307a77805ca81e86836d9a`

## 现象（先量了，再动手）

操作者真实目录 `~/workspace/` 里堆着一批 `gopi-<32位十六进制>` 目录。实测（2026-10-01）：

```
$ ls -d /home/user/workspace/gopi-* | wc -l          # 191（含 4 个合法 worktree 与若干固定目录）
$ 每个残留目录的内容（不是"只有一个 .git 指针"）：
  .git       → "gitdir: /dev/shm/suite-agent-run-<N>/TestReframe…/001/reframe-main/.git/worktrees/gopi-<hex>"
  README.md  → 8 字节（fixture 仓库唯一被 checkout 的文件）
```

`.git` 指向的 gitdir **已不存在**——它随 `/dev/shm` 上那次测试运行一起消失。这就是"孤儿"的定义：
目录还在，注册它的仓库已经不在了。**注意：简报里写的"目录里只有 `.git`"不准确**，实测是
`.git` + 8 字节 `README.md`；判据按实测写，不按描述写（这条差异直接决定了谓词的第一版会漏掉全部 183 个候选）。

## 根因（5-why）

1. **为什么 174~183 个残留？** 测试在操作者真实 workspace 根里 `git worktree add`。
2. **为什么测试会写到那里？** `internal/agent` 的 reframe fixture 调 `tools.TaskRunGroundFor`，
   它经 `taskGroundDirFor` → `config.ResolveRepoWorktreeRoot` → `worktreeroots.Resolve("")`；
   fixture 仓库没声明 `worktree_root`，于是落到**默认根** = `$HOME/workspace`。
3. **为什么 `t.TempDir()` 没挡住？** 它隔离的是 **fixture 仓库**（fixture 把仓库路径当参数传进去），
   **不是 ground 的落点**——ground 按生产规则放在"该仓库的 worktree_root 之下"，而那个 root 是操作者的真实目录。
4. **为什么以前没被发现？** `internal/testutil.MockEnv` 只 pin 了**子进程**那条路的 env；
   在**本进程内**解析 root 的生产路径（`ResolveRepoWorktreeRoot`）没人 pin，
   而"记得调 pin"不是机制。
5. **为什么每次跑都新增？** `git worktree add` 把注册写进 gitdir（在 runner 的 scratch 里），
   scratch 一删，指针就悬空；而目录本身在真实 workspace 里，没有任何 sweep 认领它。

实测复现（修复前后各一次，`internal/agent` 的两个 fixture）：

```
$ go test ./internal/agent/ -run 'TestReframeChildRowsSurviveTheParentGroundsDeath|TestRepairRepoMovesARowOntoALiveWorkTree'
  修复前： 185 → 186 → 189 → 192（每次 +3）
  修复后： 191 → 191（不变）
```

## 修法一：根因侧——测试进程不再解析操作者的真实根

**位置**：`internal/worktreeroots/scratch_pin.go`（新增）+ `roots.go:DefaultRoot`（一个分支）。

**为什么在定义包里，而不是每个调用点加 pin**（Rule 14）：`worktreeroots` 是"agent 可以在哪里创建
worktree"这**一个**定义；`DefaultRoot` 是所有消费者（config 的 per-repo 解析、spawn 缝的
`taskGroundDirFor`、daemoncwd、merger 的 build-cache 闸）都经过的**一个漏斗**。装在漏斗上的 pin
不会被下一个调用者忘记——而这正是本缺陷活了这么久的原因。

**为什么钩子由测试树安装、生产代码永不安装**：`worktreeroots` 是生产要链的 leaf，而
`internal/testutil` import `"testing"`，生产 import 它会把测试框架拖进发行二进制
（先例：`internal/scratch/holder.go`、`internal/scratch/tmproot.go`）。方向因此**倒置**：
包里只放一个 provider（发行二进制里是 nil，生产没有这个分支），由 suite 入口
`testutil.PinSuiteForTestMain` 安装。provider 缺失时
`TestUnderTestRootProviderIsInstalled` 直接红——把"有人记得接"变成"没接就是红的"。

**优先级不变**（Rule 2）：provider 只在 **default 分支**被咨询。显式 root
（`GOPI_WORKTREE_ROOT`、仓库声明的 `worktree_root`）照旧胜出，测试自己 pin 的根仍然有效。
`HOME` 在 pin **之前**解析，所以"没有 HOME"这条既有弃权（`ErrNoHome`）保持可达——
有测试就在测这条契约。

## 修法二：存量侧——`gopi scratch-gc stubs`

**位置**：`internal/scratch/worktree_stub.go`（谓词）+ `cmd/agent/cli_scratch_gc_stubs.go`（门面）。

**为什么是 scratch-gc 的一个 arm，不是新东西**（Rule 35）：这个动词已是"主机级一次性"，
resident tick 也调同一批 sweep。新增的只是一个**谓词**，没有守护进程、没有 cron、没有脚本、没有状态文件。

**为什么现有 sweep 全都看不见这批残留**：每个 sweep 在构造上就被命名空间限死——
`SweepHome` 走 scratch HOME（`<workspace>/gopi-test-scratch`，另一棵树），base 臂走内存 base，
sandbox 臂要 `sandbox-` 名标，declared-trees 臂走 build cache/go-tmp/raw-log。而残留落在
**操作者拥有的持久根**里，既不是 scratch，也不是 worktree（注册它们的仓库已经不存在，
`git worktree list` 什么都不会说）。所以它们对所有 sweep 都是隐形的。

**八个条件，全是"退化"的正面证明，未知一律 KEEP**：

| # | 条件 | 为什么必要 |
|---|---|---|
| 1 | 名字 = `gopi-` + 生成式词干（`[0-9a-zA-Z]{8,64}`） | 手起名（`gopi-scratch`、`gopi-1`）出界 |
| 2 | 是真目录（不是符号链接、不是文件） | 链接可能是操作者自己的布局 |
| 3 | 内容是**扁平**小文件集合，且其中有 `.git` 指针**文件** | 实测残留 = 指针 + 8B README；子目录/链接/超 64KiB = 真工作树 |
| 4 | 指针是单行 `gitdir: <绝对路径>` | 读不出来就不猜 |
| 5 | 该目标**已不存在** | 目标还在 = 活的注册 |
| 6 | 目标落在 **suite RUN 目录**（`suite-<name>-run-<tail>`）之下 | 把"测试泄漏"与"操作者自己弄坏的工作树/被 rm 掉的仓库"分开 |
| 7 | 没有任何活进程占用（cwd / fd / maps，批量 `/proc` 一次） | 有进程站在里面就否决 |
| 8 | 根本身不被拒绝（`/`、`$HOME`、gopi profile、git 工作树） | 根是操作者共享的目录，一个手误就是灾难 |

**动作面默认 dry-run**：`gopi scratch-gc stubs` 只列候选 + 每个候选的判据；
真删要 `--yes`。apply 后同一条日志行打印 `removed=`，且**计划数 ≠ 删除数就报错退出**
（dry-run→apply 这条流程唯一的承诺就是"看到的集合 = 删掉的集合"）。

## 实测证据

```
$ ./bin/gopi scratch-gc stubs --root /home/user/workspace --dry-run
gopi scratch-gc stubs: root=/home/user/workspace source=explicit --root candidates=183 would-remove=27 keep=156
would-remove /home/user/workspace/gopi-01a0e85bd6977df790e4a048b251610c reason="a .git pointer to /dev/shm/…, which no longer exists …, beside 1 leftover file(s)/8 bytes"
keep         /home/user/workspace/gopi-01a0f44c2d0f7a82bde8f76445daa30d reason="the gitdir target still EXISTS — the registration is live"
keep         /home/user/workspace/gopi-go-tmp reason=… （名字不在家族内，根本不会被考虑）
```

（完整输出见报告 `Ran:`/`Result:` 段。）

## 反回归

- `internal/worktreeroots/scratch_pin_test.go` —— 红→绿：pin 摘掉后本条直接红并打印
  `a test process resolved the DEFAULT worktree root to the operator's real root /home/user/workspace`。
- `internal/scratch/worktree_stub_test.go` —— 谓词的每个近似形状（活目标、子目录、超尺寸、
  非绝对指针、符号链接、被进程占用、目标不在 suite run 下）逐个断言"KEEP + 为什么"，
  并断言 `removed == planned` 且 idempotent。
- `internal/tools/worktree_stub_prefix_test.go` —— `scratch.WorktreeStubNamePrefix` 与生产方
  `TaskGroundDirPrefix` 的绑定（leaf 无法反向 import，Rule 14 的 tie 责任在能同时 import 两边的一侧）。

## Not Done / 诚实的缺口

- 判据 3 的尺寸上界（`StubLeftoverMaxFiles`/`StubLeftoverMaxBytes`）是**实测形状 + 保守余量**，
  不是从 fixture 定义推出的理论界：一个 fixture 若被改成写一堆小文件，残留会因超界而被 keep
  （keep 是安全侧，且 dry run 会把它连同原因打出来）。
- `hermes-agent` 两个目录：实测 `~/workspace/gopi-01a0cee1…` 指向的
  `/home/user/work/hermes-agent/.git/worktrees/…` **仍然存在（活注册）**，因此按判据必须保留；
  另一个 `redproof_wt` 是 hermes 自己仓库里的 prunable 注册，属于该仓库的 `git worktree prune`，
  不归本 sweep（`--root` 指向 git 工作树会被拒绝，正是为此）。简报里"两个 hermes 目录待处理"
  在实测下只剩"一个是活的、一个不归我们"。
- 存量回收是**一次性人工动作**，不挂 tick：残留不会自己再生（根因已修），
  再生成也只会落在 scratch 里。把 arm 加进 resident tick 会让"操作者拥有的目录被周期删除"
  变成默认行为，那与 Rule 35 的克制相悖。
