<!-- doc-class: historical -->
# 生成文档漂移：这条 bug 行的三个症状在本 ground 上分别落到了哪里

**Class**: normative (a standing claim about this tree, with the check that
enforces it). **Rule 13** (生成块只有一个写入者), **Rule 38** (census 图必须点名
生成器与测得 commit), **Rule 41** (实测与推断分开写).

## 这一行问的是什么，答案是三个不同的时间点

任务 `01a13294-5bb1-78e0-9670-41ddc451c838` 的症状写于 ground 切点
`b960290f0`（2026-10-13 08:52:44）**之前**，由另一个 run（`01a1326d-…`，基线
`0674b07`）取证。本 run 在 `b960290f0` 上逐条重测同一个 ground，结论是
**三个症状已经不在同一棵树上**：两个已在切点之前被修好，一个（机制面）仍然成立。

| 症状 | 取证时的树 | 本 ground（`b960290f0`） | 谁修好的 |
|---|---|---|---|
| `commands-census --check` exit 1 | 红 | **绿** | `e468094b0`（08:12:21） |
| `TestCommandsDocGeneratedBlockMatchesSource` FAIL | 红 | **绿** | 同上 |
| `gopi tool doccheck` exit 1（行号漂移） | 红 | **绿**（159/159 冻结，表与树精确一致） | 同上 |
| merge 后置验证覆盖不到这类漂移 | 成立 | **仍成立** | 无（本行只提行，不改机制） |

**顺序是这里唯一重要的事实**：`e468094b0` 在 `b960290f0` 之前 **40 分钟**落进
master，而本行的 ground 就切在 `b960290f0` 上。所以本 ground 上不存在「COMMANDS.md
生成块 stale」这个可修的对象 —— 修它需要一个 stale 的树，而这里没有。

## 一、COMMANDS.md 的生成块：已由生成器重生成，本 run 只核对

引入提交是 `3217781bc`（`cmd/agent/cli_archive.go` 插入 27 行，把
`isArchiveSubcommand` 从 `:179` 推到 `:188`），它在 06:04:02 由合并 `5be1220b2`
进入 master 时**没有**重生成 `COMMANDS.md`。同一次插入也让 `man/gopi-merge.md`
失效，那一半在 06:59:23 由 `25885bb65` 修好；`COMMANDS.md` 这一半一直留到
08:12:21 的 `e468094b0`：

```
$ git show --stat --oneline e468094b0
e468094b0 fix(gendoc): re-point the two drifted verb-index anchors and permit the refilter-gate anchor row
 COMMANDS.md                                           | 4 ++--
 internal/docclass/testdata/anchor-number-baseline.tsv | 5 +++--
```

它的改动是两行**生成块**（`cli_archive.go:179→188`、`cli_tasks.go:516→519`）加
一条 permit。本 run **没有**重做这个修复，只做了两件事：证明它是生成器写的，
并证明重跑该生成器逐字节不变。

**before/after 对照（同一个 ground，两份树）** —— before 是一棵 only-`e468094b0^`
的 detached worktree（`0da655900`，用 `git worktree add --detach` 建，
Rule 29：不借用别的 run 的 worktree）：

```
# BEFORE (0da655900 = e468094b0^)
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
commands-census: COMMANDS.md disagrees with the source — re-run without --check to regenerate   [exit 1]
$ go test ./internal/standards/ -run TestCommandsDoc -count=1
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource
      got:  line 6: | `gopi archive` | … | `cmd/agent/cli_archive.go:179` | …
      want: line 6: | `gopi archive` | … | `cmd/agent/cli_archive.go:188` | …
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock  (precondition: 已 stale)
FAIL github.com/nick/gopi/internal/standards 19.139s

# AFTER (b960290f0 = 本 ground)
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
commands-census: COMMANDS.md is in step with the source (44 verbs)                                 [exit 0]
$ go test ./internal/standards/ -run TestCommandsDoc -count=1
ok  github.com/nick/gopi/internal/standards  9.800s
```

**确定性**（Rule 13 的另一半：生成器的重复运行必须不动字节）。在本 ground 上
重跑写入式生成器，前后对拍 blob 哈希：

```
$ git hash-object COMMANDS.md                         # 490f4359717a5e6a26ca12ea7d97ca31a95657ea
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md
commands-census: COMMANDS.md already up to date (44 verbs)
$ git hash-object COMMANDS.md                         # 490f4359717a5e6a26ca12ea7d97ca31a95657ea
$ git status --porcelain                              # （空）
```

同一事实的 `--check` 面（Rule 38：图必须点名生成器）：
`internal/standards/testdata/commands-census.go`，测得 commit `b960290f0`。

## 二、doccheck 的行号漂移：permit 的 provenance 是真的，处置也是对的

`e468094b0` 同时给
`devdoc/session-record-view-refilter-gate.md` 的行号漂移加了一条 permit：

```
devdoc/session-record-view-refilter-gate.md  number-drifted  cmd/agent/cli_notify_deliver_idle_test.go:200
  if strings.Contains(c, notifyIdleText) {   first-frozen at 0b3c1de29; …
```

**它是 `first-frozen`，不是 `pre-existing`，而这个区别是对的。** 本 run 逐树量了
那句话的落点（`git show <rev>:<path> | grep -n`），四个树一个句子四种落点：

| rev | 该句子的行 |
|---|---|
| freeze base `0b3c1de29` | `:200` |
| `04f066306`（写这份 devdoc 的提交） | `:200` |
| **`81b97ab10`（07:05:59，freeze 之后）** | `:195` |
| `b960290f0`（本 ground） | `:195` |

即：冻结树里它是**连贯的**，移动它的是冻结之后的 `81b97ab10`（
`test(notify): make a child's exit a replayed event, not a one-shot channel value`）。
所以这条 permit 不能写成 `pre-existing at 0b3c1de29 freeze` —— 生成器写
`first-frozen at 0b3c1de29` 正是 `AnchorNumberBaselineReasonFor` 对「基线未确认」
的处置。**本 run 没有改这张表**：`anchor-number-census … --check` 报
「table matches the tree — the freeze is exact」，改它就是伪造 provenance。

三张表、三支生成器、同一结论（Rule 38 的复算面）：

```
$ go run internal/docclass/testdata/anchor-number-census.go  . …/anchor-number-baseline.tsv  --check
anchor-number-census: scanned 242 anchor row(s), 159 incoherent; table carries 159 row(s)
anchor-number-census: table matches the tree — the freeze is exact                     [exit 0]
$ go run internal/docclass/testdata/prose-anchor-census.go   . …/prose-anchor-baseline.tsv   --check      [exit 0]
$ go run internal/docclass/testdata/prose-path-line-census.go . …/prose-path-line-baseline.tsv --check    [exit 0]
$ go run internal/standards/testdata/repo-hygiene-census.go  . …/repo-hygiene-baseline.tsv  --check
census --check: … matches the tracked tree (430 rows)                                  [exit 0]
$ go run internal/standards/testdata/mancensus.go . bin/gopi --check                    [exit 0]
```

## 三、机制面：唯一的半份 —— 后置验证的 run set 不含 `internal/docclass`

前两节都是「已经好了」。这一节是本行真正的存量，**本 run 只判断、不改机制**
（epic 的 constrains 第 5 条）。

`internal/tools/mergeset_gendoc.go` 的 UNION 腿已经把「landing 之后生成物是否
同步」接进了后置验证 —— 这是真的，而且接得对（它读 landing 的并集 diff，把该跑
的 gate 包加进 run set）。它的覆盖边界由两个集合的**交集**决定：

* 后置验证固定的 gate 包：`mergesetGatePackageDirs = {internal/standards,
  internal/stylegate, internal/testutil}`（`internal/tools/rebuild_vet_mergeset.go`）；
* 生成物边发现的 gate 包：`gendoc.Deps` 的 `GatePackages`，按
  `gendoc.BaseDirs = {internal, cmd}` 下的测试文件是否点该文档来推导
  （`internal/gendoc/edge.go`）。

`internal/docclass` 不在第一个集合里，也不是 `COMMANDS.md` 任何一条边的 gate 包
（那条边指向 `internal/standards`）。**所以 anchor-NUMBER 族（
`internal/docclass` 的 `TestAnchorNumberGateOnTheRealTree` 与它的三支 census）
在一棵没有 touch 到 `internal/docclass` 的 landing 上不跑** —— 而这一族恰好就是
本行症状二的那张表。这不是「没人写门禁」，是「写了门禁但不在落地那一刻跑」，
与本行标题描述的缺口是同一个形状，只是换了一个文档族。

**而且这一族连「边」都没有**，这不是推断，是 edge 自己的扫描范围（
`internal/gendoc/edge.go`）：

```
var CandidateDirs = []string{"", "man"}          // 只有仓库根与 man/ 下的 *.md 才算候选文档
```

`devdoc/` **不在候选目录里**，所以 `devdoc/*.md`（以及它们引用的 `path.go:NNN` 锚点）
根本不进 gendoc 的发现过程 —— 没有边，就没有 `GatePackages`，也就没有任何一条
「landing 动了 X 就必须跑 Y」的映射。这是把两个事实叠起来看的结论：

```
$ go test ./internal/gendoc/ -run TestGeneratedDocDepsCoverTheReferenceDocuments -count=1 -v
    edge_test.go:72: COMMANDS.md → generator internal/standards/testdata/commands-census.go,
      54 anchor source(s), gate package(s) [internal/gendoc internal/standards internal/tools]
--- PASS: TestGeneratedDocDepsCoverTheReferenceDocuments (0.20s)
$ grep -rln "COMMANDS.md" internal/docclass/          # 空 → docclass 不是任何一条边的 gate 包
$ # 后置验证固定的 gate 包（internal/tools/rebuild_vet_mergeset.go）：
$ #   mergesetGateStandardsDir = "internal/standards"
$ #   mergesetGateStylegateDir = "internal/stylegate"
$ #   mergesetGateTestutilDir  = "internal/testutil"
```

即：**devdoc 的行号漂移与生成物漂移共用同一个形状的缺口，但只有 man 与
`COMMANDS.md` 那两半被 UNION 腿补上了，devdoc 这一半今天仍然只能靠
`gopi tool doccheck` 或 `go test ./internal/docclass/` 被人手动跑到。** 本 run 的
症状二（`doccheck` 报 anchor NUMBER drift）走的正是这条路 —— 它当时能被看见，是因为
那个 run **手动跑了动词**，不是因为在落地那一刻有人拦。账本里已有两条同族行
（`01a132a6-…`「doccheck 报 anchor NUMBER drift，standards 套件不覆盖」、
`01a132a4-…`「doccheck 在纯净 master 上 exit 1」）—— 本 run 的判断是：它们报的现象
**已被本 run 量到了根**（见第二节），但**它们指出的覆盖缺口今天没有任何东西在跑**。

最后一条同族的实测事实，与本节分开写：**那些锚点所在的文档，其正文真值也没有门禁。**
`devdoc/session-record-view-refilter-gate.md` 的 class 是 `normative`（"standing claim
about **master**"），而它 §3 写着 `9867ed461`、`6c06db641`、`4cec86abb` 三个提交
「not on master at all（`git merge-base --is-ancestor <c> master` → exit 1 for every
one of them）」，本 run 复测三个都是 master 的祖先（`--is-ancestor` exit 0 ×3）。
`doccheck` 与 `TestDocAnchorsResolveInNormativeDocs` 都只解析锚点、不读正文，所以这条
假站位断言今天全绿 —— 已单独提行，证据见下。

这与本 run 的实测并不矛盾：本 ground 上 `gopi tool doccheck` 是绿的，所以这不是
一个「现在红」的缺陷，而是一个**覆盖缺口**。两件随之而来的事实也一并记下：

1. **本 ground 的兄弟行**：master 上正在动的同族工作是
   `01a131ef-a558…`（anchor-number 单谓词重构按 master 的 ONE predicate 重新生成）
   与 `01a13280-…`（docclass anchor-ambiguity 改动无法自动并入 master）。本 run
   不去碰这张表，正是为了不与它们撞车。
2. **一条 normative 文档的 master 断言已被证伪**（实测，非推断）：
   `devdoc/session-record-view-refilter-gate.md` 的 §3 写着「the fixture-side
   repair is **not on master at all**（`git merge-base --is-ancestor <c> master`
   → exit 1 for every one of them）」，点名 `9867ed461`、`6c06db641`、`4cec86abb`。
   本 run 复测：**三个都是 master 的祖先**。文档的 class 是 `normative`
   （"a standing claim about master"），所以这是一条关于 master 的、今天为假的
   站位断言 —— 而任何门禁都不看文档的**内容**，只解析**锚点**。见本 run 提交的
   bug 行。

## 本 ground 上仍然红的一条门禁 —— 与本 run 无关，本 run 只量它

`go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1` 在本
ground 上 **FAIL**（`3238 across 773 normative docs; 316 pre-existing drift entries
permitted, 8 new, 2 stale permits`）。它是**切点自带的红**，不是本 run 带来的，而
**证明方式是控制的，不是声明式的**（Rule 41：实测与推断分开）：

```
# 在本 ground（b960290f0 + 本 run 的 2 个 devdoc 文件）上
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
docanchor_test.go:359: doc anchors verified: 3238 across 773 normative docs (… 316 pre-existing
  drift entries permitted, 8 new, 2 stale permits)
--- FAIL: TestDocAnchorsResolveInNormativeDocs (3.126s)

# 在只含切点的 detached 对照树（…/gopi-control-b960290f0-01a13294，git rev-parse HEAD=b960290f0）
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
docanchor_test.go:359: doc anchors verified: 3234 across 772 normative docs (… 316 pre-existing
  drift entries permitted, 8 new, 2 stale permits)
--- FAIL: TestDocAnchorsResolveInNormativeDocs (4.93s)
```

两棵树的 **new 数 8、stale permits 数 2 完全相同**，`3234→3238` 是**772→773 篇文档**
带来的（本 run 新增的那一篇），而 `gendoc-drift-merge-visibility` 这个名字在对照树的
输出里出现 **0 次** —— 本 run 的文档没有产生任何一条 finding。所以这一条红是切点的
存量，登记在这里而不是塞进 `## Not Done` 的"没做"里：它不在本 epic 的交付范围内
（epic 的验收面是 `TestCommandsDoc` / `commands-census --check` / `doccheck` 三者，
本 run 三条都实测绿了），但它确实在本 ground 上红着，下一个读这份报告的人有权知道
它红、以及它为什么不是本 run 的责任。

## 不要从这里学错的东西

* **不要把「一行 stale」当成「这一类不在门禁里」。** 这一族有门禁
  （`internal/standards` 的 `TestCommandsDoc*`）、有生成器、有 census `--check`，
  而且都在 merge 后置验证里跑。落空的是**跑的时刻**：一个分支把 stale 的生成物
  一路带进来，落地那一刻没跑（本行的症状），落地之后第一次有人手动跑才红。
* **不要用「重跑生成器看看有没有 diff」当成漂移的证据。** 生成器的写入式运行与
  `--check` 是同一条比较，但只有 `--check` 在**不写**的前提下给退出码；本 run 的
  确定性证明用的是「写入式重跑 + blob 哈希对拍」。

## Verification

* Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK（mancensus 的
  `--check` 需要这个二进制）。
* Tests: `make test-budget PKGS='./internal/standards'` → `exit=0 outcome=pass`，
  206 test(s)，0 个包失败（含 `TestStandardGates` 15.98s）；
  `go test ./internal/standards/ -run TestCommandsDoc -count=1` → ok 9.8s；
  before 对照树 `0da655900` 同一命令 → FAIL（见上）。
* Ran: 见上四段逐字命令与输出；`gopi tool doccheck` → exit 0。
* Result: 本 ground 上 `commands-census --check` / `TestCommandsDoc` /
  `doccheck` 三者皆绿；COMMANDS.md 重跑生成器 blob 哈希不变；三张 docclass
  baseline 与两张 standards baseline 的 `--check` 全部 exit 0。
* Commit: 见本 run 的报告（`task-report.md`）的 Commit 行。

## Evidence (self-filed)

* `go run internal/standards/testdata/commands-census.go . COMMANDS.md --check`
  → `commands-census: COMMANDS.md is in step with the source (44 verbs)`，exit 0。
* 生成器确定性：`git hash-object COMMANDS.md` = `490f4359717a5e6a26ca12ea7d97ca31a95657ea`
  在写入式重跑前后**相同**；`git status --porcelain` 空。
* before 对照：`0da655900` 上同一 `--check` → `disagrees with the source`，
  exit 1；`go test … -run TestCommandsDoc` → 2 个 FAIL。
* `gopi tool doccheck` → `159 of 159 are frozen …` + `the devdoc corpus is
  consistent`，exit 0。
* `anchor-number-census … --check` → `table matches the tree — the freeze is exact`，
  exit 0；`repo-hygiene-census … --check` → 430 rows，exit 0。
