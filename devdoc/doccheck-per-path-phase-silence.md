<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# doccheck 的**逐路径相位**静默：根因与修法（2026-10-16，base 9abec60）

**症状（本 task 的 EPIC 原文）**：`gopi tool doccheck --fix` —— Rule 36 指定的**唯一**修复入口
—— 被实测跑过 3 分钟、**一个字节都没打印**就被调用方的超时杀掉；而同一棵树上的只读
`gopi tool doccheck` 在同一个窗口里**打出了报告**。于是调用方**无法区分"还在算"和"挂了"**，
而工具的沉默把使用者推向**手改 `devdoc/DOC-INDEX.tsv`**，那是被明令禁止的。

---

## 1. 先证伪一半：游标已存在的相位**不是**这一半

`internal/docindex/docclass_drift.go` 里**已经**有一个宣告 + 15s tick 的机制
（commit `2fd04cfad`，2026-10-14，**是 61b0846 和 9abec60 的共同祖先**）：

```go
fmt.Fprintf(writer, DocclassDriftProgressAnnouncement+"\n", len(paths), …)
ticker := time.NewTicker(docclassDriftProgressInterval)   // 15s，命名常量
```

**实测（本 ground，管道里，即被报告的那个条件）**：

```
 1.2s  doccheck: the corpus's own classifier is running over 2582 path(s) (…) — …
16.3s  doccheck: the corpus's own classifier is still running (15s of progress measured on it so far) — …
31.3s  doccheck: the corpus's own classifier is still running (30s of progress measured on it so far) — …
75.2s  doccheck: stored-cell drift (…)
```

所以**语料级渲染相位是"可见"的**，它在**管道里也照常冲出**（`os.Stdout` 是无缓冲的
`*os.File`；Go 侧没有 `bufio` 包裹，`pipe` 与 `tty` 两条路径的到达时刻一致）。
→ **报告里"整条命令 3 分钟 0 字节"这半，不能用这个相位解释。**

---

## 2. 根因：**逐路径**的生成器相位，是**唯一一条没有任何输出的腿**

拿真实条件实测（3 份"落盘但无行"的 devdoc，即 `--fix` 真正要修的东西），逐行到达时间：

| 时刻 | 行 |
|---|---|
| 0.2s | `file-with-no-row devdoc/zz-probe-{1,2,3}.md`（3 行） |
| 0.6s | 锚点小节 |
| 0.8s | **语料级**渲染的宣告 |
| 15.9 / 30.9 / 45.9 / 60.9s | 语料级渲染的 tick |
| 66.0s | stored-cell 小节 + `1 gap(s)…` + `verdict: drift`（**报告腿走完**） |
| **136.1s** | `repaired 3 path(s): …` |
| 136.3s | `nothing to repair in the GAP class` |

**t=66.0s → t=136.1s：70.2 秒，一个字节都没有。** 这是全 run 的最大行间间隔，
而且**它随"待修的文档数"线性增长，上不封顶**。

### 机制（不是推断，是读代码 + 计时）

`docCheckFix` → `docindex.RepairGap`（`internal/docindex/docindex.go:892`）对**每一个**
missing 路径做：

```go
if err := ClassifyWithGenerator(root, rel); err != nil { … }   // 每路径一次
```

而 `ClassifyWithGenerator`（`internal/docindex/generator.go:114`）**每个路径起两个进程**：

```go
generatorRunner(repo, ClassifyScriptName, generatorOnlyFlag, rel)   // classify.py --only <path>
generatorRunner(repo, BannerScriptName,   generatorOnlyFlag, rel)   // banner.py   --only <path>
```

每个进程都付**解释器自身的固定开销**——实测本仓库语料下
`python3 tools/docclass/classify.py . --dry-run --only <一个路径>` = **29.0s wall**。
`generatorRunnerSplit` 全程**不打印任何东西**。

**结论**：

> 沉默不是"卡死"，是**一条没有宣告、没有 tick、且长度正比于语料里未分类文档数**的相位。
> 语料级那条腿**已经**被 `2fd04cfad` 变成可观察的；**逐路径这条腿没有**。
> 70.2s（3 份）只是起点：`N` 份 → 约 `29N` 秒的全静默（再加上随后那一轮语料级渲染）。
> 一个 120s 的 no-output watchdog 在 `N ≥ 4` 时必然开火，而它开火时**报得完全没错**。

---

## 3. 修法：把宣告 + tick 放进**每个生成器进程都必经的那一个函数**

坏消息是"要改的地方很多"（repair 腿、create 腿、write hook 的 re-render 三处）；
好消息是它们**全部经由 `generatorRunnerSplit`** —— 所以修法落在**一处**（Rule 14：一条策略，一个位置），
以后新加的腿**不可能因为没接线而保持静默**：

```go
subject := generatorRunSubject(full)          // 该路径 / 该语料，与进程吃的同一个 slice
start   := time.Now()
announceGeneratorRun(script, subject)          // 宣告：脚本 + 这一等是哪一个
ticker  := time.NewTicker(generatorProgressInterval)   // 15s，与语料级同一条命名常量
go func() { for { select {
    case <-done: return
    case <-ticker.C:
        fmt.Fprintf(generatorProgressWriter, GeneratorProgressTick+"\n",
            script, subject, int(time.Since(start).Seconds()))
} } }()
cerr := cmd.Run()
close(done); ticks.Wait(); ticker.Stop()       // 等结束即停，并 JOIN 掉在途的 tick
```

四个刻意的约束：

1. **tick 由"在等待的那个 goroutine"写**：只有它知道"等待是否还在进行"。调用方最远只能印
   "它开始了"，印不出"它还在跑" —— 而 watchdog 需要的恰恰是后者。
2. **数字是量出来的**：`time.Since(start)`，**没有百分比、没有 ETA**（这个进程看不到
   classify.py 的内部进度，任何这类措辞都是编造）。有测试钉住。
3. **等结束即停并 JOIN**：`ticks.Wait()` 是必需的 —— tick 可能在 `cmd.Run()` 返回的**微秒级**
   之前被 select 走，不 JOIN 它就会落进调用方紧接着要打的判决行里。
4. **宣告需要 tick 兜底**：只加宣告**不足以**满足验收 —— 静默窗口的长度是 `29s × N`，
   宣告只把它变成 `29s × N` 的"有头无尾"。tick 才让窗口长度**与语料规模无关**（≤15s）。

**没有动的东西**：写路径、`internal/docindex` 的三个谓词、`DOC-INDEX.tsv` 的任何一个字节、
以及"一条路径一次进程"这个**刻意**的选择（把 N 个 `--only` 拆成 N 个进程只会更慢：
每进程 29s 固定成本）。**这不是"把长过程改快"，是"让'在干活'和'卡死'可区分"。**

---

## 4. 前后对照（真实 run，同一棵树、同一条件：3 份未分类文档，走管道）

| | 修前 | 修后 |
|---|---|---|
| 最大行间静默 | **70.2s**（t=66.0s→136.1s） | **15.1s**（= tick 周期，与 N 无关） |
| 逐路径相位有输出吗 | **无**（0 字节） | 每个进程一行宣告 + 每 15s 一行 tick |
| 120s watchdog | `N ≥ 4` 必杀 | 不杀 |
| 判决 | exit 1（11 stored-cell drift + 3 gap） | **不变**（acceptance #3） |
| 墙钟 | 约 152s | 约 152s（**没有变快，只是可见了**） |

修后的逐路径片段（实测）：

```
 77.3s  …is running `classify.py` for ONE path (…) — devdoc/zz-probe-1.md
 92.4s  …is still running `classify.py` for devdoc/zz-probe-1.md (15s of progress measured on this invocation)
100.1s  …is running `banner.py` for ONE path (…) — devdoc/zz-probe-1.md
100.1s  …is running `classify.py` for ONE path (…) — devdoc/zz-probe-2.md
115.1s  …is still running `classify.py` for devdoc/zz-probe-2.md (15s of progress measured on this invocation)
126.3s  …is running `banner.py` for ONE path (…) — devdoc/zz-probe-2.md
151.7s  doccheck: repaired 3 path(s): …
```

---

## 5. 回归测试（红证已跑）

`internal/docindex/generator_progress_test.go`，3 个 test，全部驱动**真实的**
`generatorRunnerSplit`（fixture 在 `tools/docclass/classify.py` 的**真实路径**上放一个
sleep 替身，所以调用的 argv 形状、解释器常量、stdout/stderr 分流都是真的）：

| test | 钉住的属性 |
|---|---|
| `…RunAnnouncesAndTicksWhileTheWaitIsInProgress` | **核心**：宣告恰好 1 次，且**至少 2 个 tick 在等待期间写出**（见下） |
| `…NamesItsPathAndInventsNoNumber` | 对**渲染后的行**断言：必须点名**路径**与脚本；不得出现 `ETA`/`remaining`/`% of`；必须带实测秒数 |
| `…StopsTickingWhenTheWaitEnds` | 等待结束后**不再有** tick（泄漏的 ticker 会把进度写进调用方的判决输出） |

**为什么"至少 2 个 tick"而不是"≥1"**：repair 腿对**每个**路径重复这整段等待，
**一个 tick 只能把窗口缩到"一条路径的长度"（29s）**，不能让它与 N 无关 —— 要证明的
性质是"窗口长度与 N 无关"，所以断言必须排除"只报一次"的形态。

**断言为什么对"渲染后的行"而不是模板**：`DocclassDriftProgress` 那类常量携带 `%s` 动词，
拿它去 `strings.Contains(output, …)` **永远匹配不到任何东西**（同样的坑本仓已在
`docCheckDriftAmbiguousCountMark` 上踩过）。所以 phase 另有两个**经替换后仍存活**的短语常量
（`GeneratorRunAnnouncementMark` / `GeneratorRunTickMark`），断言对它们做计数。

**红证（行为级，不是编译级）**：只把 announce/tick 两处调用**删掉**（保留全部常量与
`subject`/`start`，让测试照常编译 —— 编译不过的红证是弱证据），同一命令：

```
$ make test-budget PKGS='-run "TestGeneratorRunAnnounces|TestGeneratorProgress" ./internal/docindex'
  --- FAIL: TestGeneratorRunAnnouncesAndTicksWhileTheWaitIsInProgress
          generator_progress_test.go:122: the per-path phase printed NOTHING in 322ms — a caller cannot
          tell this from a hang (the defect this phase's liveness exists to remove)
  --- FAIL: TestGeneratorProgressNamesItsPathAndInventsNoNumber
stamp: … exit=1 outcome=fail
```

**恢复**用内容身份核验，不用 index diff：

```
hash_fixed=abe24aa3d3a7ec2c0ac7dbff80ed845bf03ab05c
hash_head=2f39d3ee008820aff84c39267712a661eb90ab1b    ← 变异时（= HEAD:…）
hash_after_restore=abe24aa3d3a7ec2c0ac7dbff80ed845bf03ab05c   ← 与 hash_fixed 相等
```

---

## 6. 本次**没有**修的东西（诚实）

- **11 行 stored-cell drift 本身**（`PRINCIPLE-deterministic-lifecycle-ownership.md`、
  `compute-nodes-unwired-20260930.md`、`gopi-task-ledger-architecture.md` + 8 未回显）
  —— 那是**数据修复**，EPIC 明确划到本任务之外。本 branch 因此 `doccheck` 仍 exit 1，
  **原因与本改动无关**（base 9abec60 上同样存在）。
- **"一条路径一个进程"这个形态**：不优化。实测把它拆成 N 个进程只会更慢（每进程 29s 固定成本）。
- **`repair` 腿的墙钟**：没有变快，只变可见。要变快是另一件事（批量化 classify.py 的
  `--only` 入参），本任务不碰。

---

## 7. 可复用的取证配方

```bash
# 逐行到达时间：报告"哪一段静默、静默多久"的唯一判据。
# 关键：写进**文件**再由轮询者读，不要用管道 —— 管道会阻塞在工具自身的缓冲上，
# 报出来的是"最后一次性到达"，而不是"每一行何时到达"。
stdbuf -oL -eL ./bin/gopi tool doccheck --fix > tty.log 2>&1 &
# 轮询 tty.log 的行数增长，给每一行打上相对时刻；
# 最大行间间隔 = 调用方 watchdog 会击杀的那段。

# 区分"卡死"与"在等"：采样进程 state 与 CPU ticks，以及 /proc/<pid>/task/<pid>/children。
# Rule 24 sibling trap: 用 pidof -x gopi（精确名），不要 pgrep -f "<pattern>"（自匹配）。
# 决定性的一帧：在静默窗口内对进程发 SIGQUIT，看 goroutine 转储停在哪个 syscall 上。
```
