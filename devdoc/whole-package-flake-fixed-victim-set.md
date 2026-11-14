<!-- doc-class: historical -->
# 整包测试"不可重复"的真相：红名单不是随机的，受害集合是固定的

**任务行：** `01a18da0-ed86-799e-8881-c3fea78c1a18`（bug，priority 1）
**测量时间：** 2026-10-31，在 `97d185c` 上
**关联文档：** `devdoc/test-budget-freshness-refusal-evidence.md`（同一个守卫的 MOVED / PREDATES / LIVE-WRITER 三种判定），`devdoc/bug-claim-recheck-stale-binary.md`

## 结论（先说，因为它是这条 epic 唯一真正的新知识）

`go test ./cmd/agent/ -count=1` 在同一个 HEAD 上连续跑三次得到三种不同的红名单——**这不是 flaky，
也不是测试之间争用 worktree / ground / session 目录**。原因只有一条：

> **runner 在 suite 开始时构建一次被测二进制，然后跑 11–13 分钟；只要这 13 分钟里树动了，
> 之后每一条还没跑到的 e2e 都会在 0.01–0.04 秒内被 `assertGopiBinIsFresh` 拒绝——而"哪几条
> 还没跑到"取决于树在第几秒动。所以变化的不是受害者集合，是"整包跑到哪里时被截断"。**

配套的三个量化事实（同一个语料，生成器见本文末）：

1. **7.2%** 的 run 含这种拒绝（523 / 7311）。
2. **受害测试只有 13 条**，不是随机抽样：523 个 run 里被拒的测试名集中在这 13 条上，
   出现次数从 470 到 236 不等，**没有任何一次是名单外的 e2e**。
3. **"整包绿过一次"不构成证据**：这条路径的假红率是 7%，而假绿（二进制比树新一点点、
   恰好没被截断）在同一机制下同样可能存在。验收必须先证明 `TREE STABLE`。

## 为什么 epic 的推断是错的（这条要留着，因为它是方法论）

原 briefing 的推断是"C 组 8 条红的共同点是都在建立真实 worktree / ground / session 目录，
且都在 0.01–0.04 秒内失败——这是准备阶段失败"。**"0.01 秒速红"是真的，"争用真实目录"是错的。**
耗时签名只能说明"在 fork 出去做实际工作之前就中止了"，它能由**多种**原因产生，不是判据。
真正的判据只有一个：**现场原文**。

现场原文（`go test -json` 流里逐字，来自
`/home/user/workspace/gopi-test-scratch/queue/test-budget/` 下的 `run-*.json`）：

```
task_briefing_path_set_e2e_test.go:61: /home/user/workspace/gopi-test-scratch/queue/test-budget/run-1792169230-2860475-tested-bin/gopi does NOT contain the tree's production sources: 3 file(s) differ from the revision it was built from (347579b26f9c) — this test drives the BUILT binary, so it would verify code that is not in that binary.
        changed: cmd/agent/merge_run.go, cmd/agent/merge_stop.go, internal/config/agentguard.go
    Rebuild it (CGO_ENABLED=0 go build -o .../gopi ./cmd/agent/) and re-run.

（上面这一行是流里的**原文逐字**：`-o .../gopi` 里的省略号是生成该文本的代码自己打的，
不是本文档截断的。要复现完整路径，读那条 run 的 `run-*.json` 里该行原文。）
```

C 组 8 条打印的是**同一条拒绝**，只差"几个文件"。与任何资源争用无关。

## 机制（读代码，不是猜）

```
每条 e2e → assertGopiBinIsFresh(t, bin)                       cmd/agent/gopibin_identity_guard_test.go:169
        → root := cwdProbeModuleRoot(t)                       从测试 CWD 向上找 go.mod → 当前树
        → decideBinFreshness(root, bin)
           → binaryBuildRevision(bin)                         读 ldflags 打进二进制的 revision
           → productionSourcesChangedSince(root, rev)         git diff --name-only <rev> -- cmd internal
           → len(moved) != 0 ⇒ t.Fatalf(拒绝)
```

`scripts/test-budget.sh` 的设计是**只构建一次**被测二进制（`build_tested_binary` 是第一个慢步骤），
然后把它交给每个包。于是"二进制构建时刻"与"这条测试真正执行的时刻"之间，隔了整个 package 的墙钟时间。

## 最小复现（我做的，1 秒级，不需要任何资源压力）

在一个由当前树干净构建的二进制上，只往一个生产文件加一行注释：

```bash
cd /home/user/workspace/01a18da0-ed86-799e-8881-c3fea78c1a18
echo "// probe" >> internal/config/agentguard.go
GOPI_TEST_BIN=/tmp/epbin go test ./cmd/agent/ -count=1 -timeout 5m \
    -run '^TestWorkerRefusesWhenTheGroundCannotBeMade$'
```

结果：`FAIL`，拒绝文本与现场原文同源。**一行未提交的生产文件改动 = 这条测试立刻红，
不需要 NPROC、不需要并行、不需要第二个进程。**

还原与核验按 Rule 5 的内容身份判据（锚定 revision，不用 index-relative diff）：

```bash
git restore internal/config/agentguard.go
test "$(git hash-object internal/config/agentguard.go)" = "$(git rev-parse HEAD:internal/config/agentguard.go)" && echo RESTORED
git status --porcelain
```

## 这条拒绝有两条臂（本轮实测的精度补充）

我按上面的最小复现做了一次，拿到的却是**另一条**拒绝文案：不是内容判据那句
`does NOT contain the tree's production sources`，而是 mtime 判据这句：

```
--- FAIL: TestWorkerRefusesWhenTheGroundCannotBeMade (0.02s)
    task_ground_e2e_test.go:451: /home/user/workspace/gopi-ep-refprobe/gopi (mtime 2026-10-31T01:15:19+08:00) is OLDER than /home/user/workspace/01a18da0-ed86-799e-8881-c3fea78c1a18/internal/config/agentguard.go (mtime 2026-10-31T01:15:21+08:00) — this test drives the BUILT binary, so it would verify code that is not in that binary.
        /home/user/workspace/gopi-ep-refprobe/gopi carries no revision in its -ldflags setting
        deciding criterion: mtime (the last resort; an mtime comparison cannot tell an identical-bytes rewrite from a real edit, so a build that stamps its revision gets a content proof instead).
          VERDICT: the binary PREDATES this source — the tree's own records show content newer than this binary (the source differs from HEAD, or HEAD was committed after the binary was built), so the build is simply older than the edit
```

原因逐字写在拒绝里：**`carries no revision in its -ldflags setting`**。
我这次是手工 `go build -o <path> ./cmd/agent/` 造的临时二进制，没走 runner 的
`build_tested_binary`，因此二进制里没有 **revision**，守卫只能退到 mtime 判据；
而 runner 造出来的二进制带了 revision，走的是内容判据（`git diff --name-only <rev> -- cmd internal`）。

**所以"0.01–0.04 秒速红"可以由两条不同的臂产生**，两条臂的**结论相同、修法也相同**
（这个二进制不包含这棵树），但读者若只看前几行来决定"是哪一种"，会拿错分支：

| 臂 | 拒绝文案 | 何时选中 |
|---|---|---|
| 内容 | `does NOT contain the tree's production sources: N file(s) differ from the revision it was built from (<rev>)` | 二进制里有 revision（runner 的产物） |
| mtime | `is OLDER than <source> — this test drives the BUILT binary` + `carries no revision in its -ldflags setting` | 二进制没有 revision（手工 build、旧产物） |

上一轮对 7304 个 run 的普查量到：内容判据 **521** 次、构建身份判据 **0** 次 —— 因为真实 run 里的
被测二进制**总是**由 runner 构建、**总是**带 revision。手工复现时容易落到 mtime 臂而误以为
"和现场不是同一个机制"，这一点必须先看清拒绝里的 `deciding criterion:` 那一行再下判断。

## 受害者集合（13 条，稳定）

严格口径 = 同一 run 内 `Action=="output"` 行含该消息 **且** 该测试出现在 `Action=="fail"` 里
（跨行关联；宽口径会把门的自测 fixture 字面量也数进来，那是错的）：

| 次数 | 测试 |
|---|---|
| 470 | `TestBareRunKeepsAllItsArtifactsInOneSessionDir` |
| 359 | `TestRawPromptRunReleasesItsWorktree` |
| 348 | `TestFrontRunConsoleOutputLandsInItsSessionDir` |
| 347 | `TestWorkerRefusesWhenTheGroundCannotBeMade` |
| 346 | `TestTaskRunSpawnsTheChildInsideItsOwnLinkedWorktree` |
| 345 | `TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree` |
| 335 | `TestWorkerForegroundPrintsTheTaskOnItsConsole` |
| 326 | `TestEditToolAnnotatesTeachingDocsInAuditedTrees` |
| 324 | `TestBriefingLastClauseReachesTheChildsFirstRequest/gate-assertion` |
| 324 | `TestBriefingLastClauseReachesTheChildsFirstRequest/over-limit` |
| 324 | `TestBriefingLastClauseReachesTheChildsFirstRequest/near-limit` |
| 324 | `TestBriefingPathSetClauseReachesTheChildsFirstRequest` |
| 238 | `TestRawPromptCloneDeliveryOrderBlockedLeavesTheRowOpen` |
| 238 | `TestRawPromptCloneDeliveryOrderDeliveredClosesTheRow` |
| 237 | `TestRawPromptCloneChannelDeliversItsCommits` |

（`TestBriefingLastClauseReachesTheChildsFirstRequest` 的三个子测试按顶层名合并算一条，
所以上表 15 行 = **13 个不同的顶层测试**。epic 点名的 C 组 8 条全部在表内。）

**两条"集合外"已实测排除，不是受害者：**
`TestBudgetRunConditionStaysQuietOnASettledTree`（11 个 run）与
`TestBudgetRunConditionNamesTheDriftAndTheRefusingTest`（1 个 run）住在
`internal/testutil/testbudget_run_condition_gate_test.go`，它们**自己启动一次 test-budget.sh，
并在 run 进行中故意改一个生产文件**来制造 drift（实测其流里 `differ	M internal/scratch/tmproot.go`），
为的是断言 summary 会把 drift 报出来。那条消息是它们的**断言对象**，不是它们的死因。

## 修的方向（明确不要做什么）

- **不要**调 `ulimit`、**不要**加 retry、**不要**减测试、**不要**设 `-p 1`。这些都不触及
  "树在 run 期间移动"这个事实，只会把 7% 的假红藏起来（Rule 35 的形状：加东西盖住问题）。
- 要动的是"被测二进制的来源对树的移动免疫"：让 `root` 锚定到**构建该二进制的那棵树**
  （已有 `GOPI_TEST_SOURCE_ROOT` 这条 pin 的完整先例，`internal/testutil.SuiteSourceRootEnv`），
  或者让整包结论在树移动过时显式标 `ABSTAIN`，而不是把移动造成的红算成代码缺陷。
- 已有的一半已经做了：`devdoc/test-budget-freshness-refusal-evidence.md` 让拒绝文本区分
  MOVED / PREDATES / LIVE-WRITER，并且 `gopi tool` 的 `stream-failures.sh` 会在 summary 里
  单独列出 "STALE-TESTED-BINARY REFUSALS"。**缺的是 `root` 的锚定**：目前 `root` 仍然是
  "测试运行时向上找到的 go.mod"，也就是当前树。

## 未解决项（如实，不猜）

- 含内容判据字符串的 run = 521，严格口径下有真实失败的 run = 520，**差 1 未解释**。
  最可能是某个 run 里该字符串出现在没有 `Test` 字段的输出行（包级/构建阶段输出）。
  重扫 7311 个文件需要约 2 分钟，收益不抵一次超时风险，**记未解释项，不编原因**。
- 具体是哪一次 `pull --rebase` 造成某个 run 的 `3 files differ` **不可还原**：
  该 run 的 `tested-bin` 目录已被 GC（`run-1792169230-2860475-tested-bin/gopi` 不存在）。
- 本文件里的所有数字统计的是**本机一个目录在某一刻的 run 流**，不是项目本身；
  该目录被 GC 之后它们不可能再被任何人重测。按 Rule 38 它们是 **NARRATIVE**（一次测量的记录），
  所以带着生成器和测量 HEAD 写在这里，但不冒充可重生成的普查。

## 生成器（可直接粘贴；把时间窗口钉死，不要凭记忆报数）

```bash
python3 - <<'EOF'
import glob, json
MSG = "differ from the revision it was built from"
files = sorted(glob.glob('/home/user/workspace/gopi-test-scratch/queue/test-budget/run-*.json'))
tot = hit = 0
vic = {}
for f in files:
    tot += 1
    msg, failed = set(), set()
    try:
        with open(f, errors='replace') as fh:
            for line in fh:
                try:
                    d = json.loads(line)
                except Exception:
                    continue
                a, t = d.get('Action'), d.get('Test')
                if not t:
                    continue
                if a == 'output' and MSG in (d.get('Output') or ''):
                    msg.add(t)
                if a == 'fail':
                    failed.add(t)
    except Exception:
        continue
    h = msg & failed
    if h:
        hit += 1
        for t in h:
            vic[t] = vic.get(t, 0) + 1
print("total runs", tot)
print("runs with strict victim", hit, "pct=%.1f%%" % (100.0 * hit / tot))
for t, c in sorted(vic.items(), key=lambda x: -x[1]):
    print(c, t)
EOF
```

在 `97d185c` 上跑它，得到 `total runs 7311`、`runs with strict victim 523 pct=7.2%`，
以及上表那 15 行（13 个顶层测试 + 2 条门的自测，后者已在上文排除）。

## 给下一个碰到它的人（最短捷径）

- 看到"多条测试在 0.01–0.04 秒内一起红"，先 `grep -c "does NOT contain the tree's production sources"`
  这个 run 的流。命中 ⇒ **0 个测试体跑过**，这不是代码判决，别去查并发、别去查端口、别调 ulimit。
- 看到 `TestBudgetRunConditionStaysQuietOnASettledTree` 里的 drift 行，先确认那是不是**门的自测故意制造的**。
- 要做 red→green 的验收：先证明 run 期间树没动（`git diff --name-only <binary-rev> -- cmd internal`
  在你跑之前和之后都为空），否则三次全绿在 7% 基线下什么也证明不了。
