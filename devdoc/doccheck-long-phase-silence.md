<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# doccheck 的长静默相位：根因与修法（2026-10-14）

**症状（operator 原话）**：`gopi tool doccheck --fix` 改写了 `devdoc/DOC-INDEX.tsv`，
然后在 stdout 和 stderr 上**同时**输出 0 字节长达 120s，调用方的 idle-output 安全网
把它杀掉 —— 而文件其实已经被重写了，operator 只能合理地认为"它什么都没干"。

**结论**：静默是**真的、且是设计使然的**，但它是**两个相位的静默就足以被误判为卡死**。
进程一直在干活（CPU 在烧、子进程活着），只是没有任何一个字节告诉观察者这件事。

---

## 1. 取证：哪一段沉默，沉默了多久

工具是本 session 目录下的 `probe-doccheck.sh`（每 0.2s 采样：相对时间、进程 CPU ticks、
子进程数、stdout/stderr 字节数）与一个**逐行到达时间**打点管道。两次都是对**真实语料**
（2524 行 manifest）跑真实的 `./bin/gopi tool doccheck --repo . --fix`。

### 1a. 逐行到达时间（决定性证据）

```
$T0=$(date +%s.%N); ./bin/gopi tool doccheck --repo . --fix 2>&1 \
  | while IFS= read -r line; do printf '%s\t%s\n' "$(elapsed)" "$line"; done > line-times.tsv
```

| 事件 | 时刻 |
|---|---|
| 第一行（GAP 类：0 gaps） | t = 1.000s |
| 最后一行**在沉默之前** | t = 1.661s |
| GAP + 锚点两节打印完 | t ≈ 3.0s |
| **下一行**（stored-cell 小节） | t = 193.739s |
| 退出 | exit 1，wall 185.8–210s |

**全 run 最大行间间隔 = 190.6s，从 t=3.003s 起**（脚本：`awk -F'\t' 'NR>1{...}' line-times.tsv`）。
这就是 watchdog 会击杀的那段。

### 1b. 进程状态（它不是卡死，是在干活）

采样（`/proc/<pid>/stat` 的 utime+stime、`/proc/<pid>/task/<pid>/children`）：

```
t=1.186 ticks=23    t=2.453 ticks=142   t=4.433 ticks=275
t=5.178 ticks=276   ← 之后 ticks 冻结（Go 侧在等结果）
```

子进程（`classify.py`，一次读多帧）：

```
state=R utime=8806 → state=R utime=9087 → gone      # 持续在烧 CPU
```

**决定性的一帧**：在 t≈52s（沉默窗口内）对进程发 `SIGQUIT`，goroutine 转储：

```
goroutine 1 [syscall]:
  syscall.Syscall6(0xf7 …)            # SYS_WAITID
  os.(*Process).pidfdWait …
  os/exec.(*Cmd).Run
  internal/docindex.docclassDriftRender  docclass_drift.go:456
  internal/docindex.DocclassDrift        docclass_drift.go:364
  main.docCheckReportStoredDrift         cli_doccheck.go:565
  main.docCheckFix                       cli_doccheck.go:804
```

即：**goroutine 1 阻塞在 `docclassDriftRender` 的 `os/exec.Cmd.Run` 上，等 classify.py。**
不是死锁、不是写路径卡住 —— 是一个**跑得很久的子进程调用**。

### 1c. 这段相位是**固有的**，不是"语料太大"

同一脚本、同一棵树，只把 `--only` 的范围收窄：

| `--only` 路径数 | wall | user | sys |
|---|---|---|---|
| 1 | 43.24s | 39.56s | 0.27s |
| 100 | 54.00s | 43.80s | 1.90s |
| 1000 | 70.99s | 56.10s | 13.48s |

**固定成本 ~43s 是脚本自身的 import/链接开销**，所以任何"语料变小"的论证都不能让这次调用提前返回；
而它**一次渲染整个语料、只在结束时输出**，所以它**没有可转达的进度**。
（顺带排除一个错误方向：把 2524 次 `--only` 拆成 N 个进程来"报进度"，实测每进程仍 ~43s 固定成本，
只会更慢 —— 见下表。一次调用是**最快的**形态。）

### 1d. 为什么 operator 看到的是"文件已改写"但"什么都没打印"

`--fix` 的顺序是：先 `RepairGap`（写 manifest，**快**）→ 打印 GAP/锚点两节（t≤3s）→
进入 stored-cell 腿 → **静默 ~190s**。所以"已改写"和"没输出"两件事都真的，只是不同步：
写发生在静默之前。

> 标注：本次实测语料的 GAP 类为 0（`nothing to repair in the GAP class`），
> 因此这次没有发生写入；**写入 run 的形状相同**（多打印一行 `repaired N path(s)`），
> 因为静默发生在写入之后的**同两条腿**上。这是**推断**，未单独实测。

---

## 2. 修法：长相位**宣告自己开始**，并且**在等待期间持续报活**

关键发现来自对照实验：**只印一行宣告是不够的**。实测（本 repo 语料）：

| 形态 | 第一行在静默之后何时到达 | 静默长度 |
|---|---|---|
| 修前 | t = 193.74s | **190.6s** |
| 只加一行宣告 | t = 193.7s | **180.0s**（宣告在 t=3.30s，之后照样 180s 无输出） |

所以 acceptance 要求的"输出**贯穿到它自己退出**"必须由**等待本身**提供信号，而不是由调用方在进入前印一句。

**修法落在 `internal/docindex` 的渲染器里**，因为**等待发生在那儿**：

```go
// docclass_drift_render：进入 classify.py 之前宣告，等待期间按 tick 报活
fmt.Fprintf(writer, DocclassDriftProgressAnnouncement+"\n", len(paths), …)
ticker := time.NewTicker(docclassDriftProgressInterval)     // 15s，命名常量
go func() { for { select {
    case <-done:   return
    case <-ticker.C:
        fmt.Fprintf(writer, DocclassDriftProgress+"\n", int(time.Since(phaseStart).Seconds()))
} } }()
runErr := cmd.Run()
close(done); ticker.Stop()                                  // 等结束即停，绝不与判决输出交错
```

**为什么必须在这里**：只有**执行等待的那个 goroutine 知道等待是否还在进行**。调用方（cmd 层）
最远只能印"它开始了"，印不出"它还在跑" —— 而 watchdog 需要的恰恰是后者。
（cmd 层原本的那句宣告在最终版本里被**移除**了：两处喇叭 = 一次相位印两遍，且它永远说不出"还在跑"。）

四个刻意的约束：

1. **数字是量出来的**。路径数就是渲染器**将交给 classify.py 的那个 slice** 的长度，
   所以宣告不可能与渲染对路径数的看法不一致。
2. **只承诺测得到的**。tick 带的是**实测已耗时**，没有百分比、没有 ETA —— 这个进程看不到
   classify.py 的进度，任何这类措辞都是编造的（Rule 15）。有测试钉住。
3. **等结束即停**。`close(done)` + `ticker.Stop()` 在 `cmd.Run()` 返回后立刻执行，
   并有测试断言返回之后**不再有** tick 写进调用方自己的判决输出。
4. **写入者默认就是进程的 stdout**（`docclassDriftProgressWriter`，`io.Writer` 变量）——
   薄路径零接线。变量只是为了**测试能在同步位置读**：要证明的是"tick 在**等待仍然进行时**到达"，
   只有从那个 goroutine 的路径上读才可能证明，重实现一个 ticker 什么也证明不了。

**这个 verb 仍然是 `internal/docindex` 之上的薄执行面**（Rule 36）：报活只是**回声**，
腿的判决完全不依赖它 —— 渲染器自己会重新枚举并报它自己的 NOT MEASURED。

**没有动的东西**：写路径、`internal/docindex` 的三个谓词、DOC-INDEX.tsv 的任何一个字节
（`man_changed NO`）。这不是"把长过程改快"，是"让'在干活'和'卡死'可区分"。

---

## 3. 回归测试

`internal/docindex/docclass_drift_progress_test.go`（7 个 test，全部驱动**真实的渲染器**
`docclassDriftRender`：fixture 在 `tools/docclass/classify.py` 的**真实路径**上放一个替身
分类器，所以调用的 argv 形状、解释器常量、stdout/stderr 分流都是真的）：

| test | 钉住的属性 |
|---|---|
| `…TicksWhileTheWaitIsStillInProgress` | **核心**：宣告 + **至少一个 tick** 在渲染**返回之前**就已写出 —— 只看最终输出会放过"等完再宣告/等完再报活"这个原缺陷形态 |
| `…ProgressLineCarriesNoInventedNumber` | 对**渲染后的行**断言（不是模板）：无 `%`/`ETA`/`remaining`，且带**实测秒数** |
| `…ProgressIntervalFitsTheCallersWatchdog` | 预算：120s 窗口里必须装得下 ≥4 个 tick（一个 tick 被调度吞掉也不能让干活的相位看起来像死的） |
| `…AnnouncementNamesTheNoOutputSilenceItCloses` | 宣告必须**说明它开启的是一段无输出期**，否则读者无从区分沉默与卡死 |
| `…KeepsTheClassifierStreamsApart` | 报活没破坏这条腿赖以成立的性质：summary 走 stderr、行行走 stdout |
| `…StopsTickingWhenTheWaitEnds` | 等结束后**不再有** tick（泄漏的 ticker 会把进度写进调用方的判决输出） |
| `…NoClassifierStillReportsTheTruth` | 没有分类器时仍然**如实报错并指名路径**，绝不变成静默成功 |

其中前两条的写法有讲究：**断言放在写入者里、计时压缩到 20ms**（`docclassDriftProgressInterval`
是变量，**只**为了压缩时钟而存在；生产永远走命名常量 15s）。等待 15s 去证明一个关于**机制**的
属性会让每次跑测试都花掉 15s，而它并不是关于"15"这个数字的。

---

## 4. 前后对照（真实 run）

| | 修前 | 修后 |
|---|---|---|
| 最大行间静默 | **190.6s**（t=3.003s 起） | 见 task report 的 after 表 |
| 120s watchdog | 必杀 | 不会 |
| 判决 | exit 1（670/2524 stored-cell drift） | 不变（acceptance #3） |

> 注：本文件所在的 corpus 行由 `gopi tool doccheck --fix` 自己写入（Rule 36：
> 绝不手改 manifest，也绝不在 Go 里重实现 classify.py）。
