<!-- doc-class: historical -->
# 在 test-budget 跑着的时候改那个包 → 真正发生过的一次全轮 REFUSED

**Task:** 01a1a607…（`gopi tool` 帮助面 + `tool tokens --file/--session`）
· **Measured at:** 2026-11-04, run `run-1793788967-3716682`（`make test-budget PKGS='./cmd/agent'`）

这份记录不为新增机制，只为把一次**操作事故**的判据原文留住：它是第二个撞上
同一个门的行（第一个是 `01a1a5f1`），而两次都不是代码问题。

## 事故形状：不是红证明，也不是并发邻居，是我自己一边跑一边改

机器上已有两份 devdoc 讲这个门的两条腿 ——
`test-budget-freshness-refusal-evidence.md`（一条 mutate-and-restore 的红证明把树
在构建之后写新了）和 `test-budget-concurrent-overlap-warning.md`（两个 test-budget
同时跑、scope 相交）。**这次是第三种**：一个 tarball 之外的普通编辑 ——
`make test-budget` 启动之后，我继续在**它正在覆盖的包**里改 `cmd/agent` 的三个文件
（把 `markerScanWindow` 拼进 usage 页、加 coverage 列表）。

## 输出长什么样（逐字）

`job-1` 的尾部，成片出现同一条：

```
--- REFUSED: TestRawPromptCloneChannelDeliversItsCommits  (github.com/nick/gopi/cmd/agent)
    would not verify: /home/user/workspace/gopi-test-scratch/queue/test-budget/run-1793788967-3716682-tested-bin/gopi
--- REFUSED: TestRawPromptCloneDeliveryOrderBlockedLandsTerminalFailed  (github.com/nick/gopi/cmd/agent)
    would not verify: …/run-1793788967-3716682-tested-bin/gopi
--- REFUSED: TestRawPromptCloneDeliveryOrderDeliveredClosesTheRow  (github.com/nick/gopi/cmd/agent)
    would not verify: …/run-1793788967-3716682-tested-bin/gopi
```

脚本自己已经把这件事翻译好了，**引用它的原话比任何转述都准**：

> Read it as: THIS RUN's own refusals, with the drift that explains all of them — the failures
> above are one condition (the tested binary no longer contains the tree), not N independent
> code verdicts. Rebuild the tested binary and re-run.

收尾的 stamp 行：

```
stamp: 2026-11-04T18:42:47+08:00 host=hosta pkg=./cmd/agent budget=45m elapsed=739s
load_start=20.17 18.18 11.65 load_end=6.45 11.86 12.30 cores=32 exit=1 outcome=fail
stream=run-1793788967-3716682.json
```

## 判据在原码里的位置（PA 独立复核过的那三行）

```
$ grep -rn "would not verify" scripts/ cmd/agent/ internal/ --include=*.sh --include=*.go
scripts/lib/stream-failures.sh:684   # where the artifact came from and which test would not verify is.
scripts/lib/stream-failures.sh:719   # tests would not verify.
scripts/test-budget.sh:1667          # contains) and the refusals (which tests would not verify). Either alone is a fact
scripts/test-budget.sh:1707          # echo "  this run's refusals, by test (each names the artifact it would not verify):"
scripts/test-budget.sh:1709          # 'NR <= n { printf "%s--- REFUSED: %s  (%s)\n%s    would not verify: %s\n", … }'
```

`tested-bin/` 这个**一次性构建**的产物由 runner 在整套跑之前建一次、发给每个包
（`tested_bin_dir`），守卫比的是它的 mtime 与生产源文件的 mtime ——
源被改新 ⇒ 二进制"不再代表这棵树" ⇒ 每个驱动**已构建二进制**的测试都被拒。

## 怎么读，怎么不重犯

- **`REFUSED` 不是失败，是「这一轮不算数」。** 它与"15 个测试红了"在观感上一样、
  在语义上相反：那 15 个测试体**一个都没跑**。
- **不要为了让它变绿去改被测代码** —— DRIFT 的成因在跑的人手上，不在树的代码里。
- 唯一正解：**停下来、提交（或至少停止编辑）、重跑一轮**。
- 顺序纪律（与 `test-budget-freshness-refusal-evidence.md` 最后那条同源）：
  **编辑 → `go build ./...` → 跑 suite**；suite 一起来就别再碰它覆盖的包，
  包括"顺手改一行注释"。
- 一个便宜的自查：`find <被改的包> -name '*.go' -newer <tested-bin/gopi> -not -path './bin/*'`
  打印为空才动手重跑。

## 同一次事故里撞上的三道守卫

### A. `tested-bin` 新鲜度 —— 跑着的时候改**源文件**（本文主体的那次）

见上文「事故形状」。签名是每个驱动已构建二进制的测试都被点名：

```
--- REFUSED: <TestName>  (pkg)
    would not verify: …/run-<id>-tested-bin/gopi
```

### B. `SUITE CALLER-HEAD GUARD` —— 跑着的时候**提交**也会让它红（比 A 更隐蔽）

A（tested-bin 新鲜度）还能靠"我不改代码"的直觉避开；这道连**写文档**都会撞。
它在 suite 开始时记下 `.git/HEAD`，结束时比对。**期间任何 `git commit` 都会让它红**，
不论提交的是 `.go`、`.md` 还是别的 —— 因为**一次 commit 就是一次对 `.git/HEAD` 的写**。

实测（同一行 `01a1a607`，第一轮已提交、树已干净，仍在第二轮红）：

```
SUITE CALLER-HEAD GUARD: HEAD moved 0b36be6f26bc67dcc554638d3a60996424e7e2dd
    → 7d7ad92254778ff21792382b5cb35a240588ae13 — a case in this package moved
    the checkout this suite runs in, which is the defect this guard exists for:
    the tree under test and the tested binary no longer agree
```

那一轮的 runner 摘要，逐字（注意三行一起读才是完整结论）：

```
--- failures (from this run's own events: Action=="fail"; the budget did NOT trip) ---
  failing tests: 0
  package verdict: FAIL: github.com/nick/gopi/cmd/agent
--- package-level failures (build/panic — no test to name) ---
  PACKAGE FAILED: github.com/nick/gopi/cmd/agent (a package-level verdict — a panic or a TestMain exit; no test to name)
```

⇒ **`failing tests: 0` 而 `package verdict: FAIL`** 是这道守卫的签名：
没有任何测试失败，是一个 package 级的守卫在说"树动了"。
⇒ 代价实测：**747 秒的一轮白跑**。

**纪律（比 A 那条更硬）**：一条 test-budget 在跑的时候
1. 不改它覆盖的包（A）；
2. **不 commit**，哪怕提交的是一份 devdoc（B）；
3. 要改要提交，等它结束 —— 或者先提交、再起 suite，中途只读。

### C. `cmd/agent` 的 coverage ratchet —— 与上面两道无关，但总在同一个时刻咬人

`TestSyncBarePeersTestCoversEveryChangedFile` 判的是
`git diff --name-only <cut> -- cmd/agent/`（**路径前缀**，不是后缀），
所以 `devdoc/*.md` 不在它的视野里；但只要本分支改了 `cmd/agent/` 下一个它没列名的
文件，它就红，并直接点名那个文件。它的名单按行作用域（随该 ground 回收失效），
加名字时要带理由。

`cmd/agent/cli_tasks_sync_peers_bare_test.go` 的
`TestSyncBarePeersTestCoversEveryChangedFile` 会在**本条分支改过 `cmd/agent/` 下一个
它没列名的文件**时报错 —— 这是故意的（一个特性不能长出一条没人断言的新腿）。
本次的六个文件（`cli_tokens.go`、`tokens_inputs.go`、`tokens_count.go`、
`tokens_inputs_test.go`、`cli_tool_usage_family_test.go`、`cli_tool.go`）属于**另一个
特性**，所以进了 `syncPeersBareUnrelatedFiles` 并各自带理由；那份列表是**按行作用域**的，
随该 ground 在 close-out 回收时失效，不是仓库级的长期白名单。

**可操作的一句**：改了 `cmd/agent` 下任何文件之后，先单独跑

```bash
go test ./cmd/agent/ -run 'TestSyncBarePeersTestCoversEveryChangedFile' -count=1
```

再开整套 —— 这个门的报错会直接点名文件，比在 test-budget 的 45 分钟末尾才发现便宜得多。
