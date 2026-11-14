<!-- doc-class: historical -->
# cmd/agent 三条既存红灯的逐条判定（2026-10-07，HEAD 3255973cc）

**性质**：某一次收口跑的一次核验记录，不是对 master 的长期主张 → `historical`。

**为什么存在**：`devdoc/cli-reorg-red-baseline-20261007.md` 的 §2 表里，第 5/6/7 条
（`TestAnnotateVerbRunsARealArgvEndToEnd`、`TestChatBoundaryTenMegabyteInputStaysBounded`、
`TestChatStatus_LiveBlockKeepsTheStateWord`）被记成「与 CLI 重组无关的既存红灯」，但没有
判定过它们是**实现错**还是**断言过时**。本文件就是那三条的判定：每条给结论、给证据、
给最小回归测试，并记录改了什么。

**一句话结论**：**三条都是「断言过时」，实现没有可指认的缺陷**。三处断言都写在
「它测量的那个东西」还不是现在这个样子的时候 —— 两条是测试与它所测面各自的演化脱节，
一条是断言把自己写成了不可满足的形式。三条都换成了**更强或等价**的断言，没有一条被放松
（Rule 20）。

---

## 1 `TestAnnotateVerbRunsARealArgvEndToEnd` 的 `-h` 子测 —— 断言过时

**红的样子**（真跑）：
```
cli_annotate_test.go:694: -h did not print the verb's usage: "gopi tool annotate — draw marks onto an EXISTING PNG (a screenshot, a render)\n…"
```
页面**印出来了**，而且是对的；断言找的字符串没找到。

**证据链（全部取自 object store，只读）**：

| 时间点 | commit | `annotateUsage` 首行 | 断言 | 一致？ |
|---|---|---|---|---|
| 测试诞生 | `9ac9f83d2` (2026-09-29) | `gopi annotate — …` | `strings.Contains(stdout, "gopi "+CliVerbAnnotate)` | ✅ 当时对 |
| 走廊道词 | `d3492e0a3` (2026-10-06) | `gopi tool annotate — …` | 同上一行（未改） | ❌ 从此红 |
| 收口前一版 | `61779b448` (2026-10-06) | `gopi tool annotate — …` | `"gopi "+CliVerbTool+" "+CliVerbAnnotate` | ✅ 一度修对 |
| 收敛到 master | `844d1f3d2` (2026-10-06, **= HEAD^**) | 未再改 | 被回退成 `"gopi "+CliVerbAnnotate` | ❌ 又红 |

`d3492e0a3`（把扁平拼写退到家族门后）改变了**帮助页标题本身**（`gopi annotate` →
`gopi tool annotate`），而断言里的那一行在同一个 commit 里没跟着动；`61779b448` 修对了，
`844d1f3d2` 在把分支收敛到 master 实现时把这行**回退**了（只保留 argv 侧的改动）。
四个 commit 都是 master 的祖先 —— 这不是「读代码推断」，是 `git show <c>:<file>` 的原文。

**为什么这不是「实现错」**：真跑两个入口都印出完整、正确的页面 ——
`./bin/gopi tool annotate -h` → rc=0；`./bin/gopi tool annotate --help` → 同一页。
CLI 自身的契约测试（`cmd/agent/cli_contracts_test.go` 的 `tool annotate` 行，指纹为
`cliUsageTitle(annotateUsage)`）**一直是绿的** —— 也就是说实现和它自己的契约一致，
红的只有这一条用「命令路径子串」当页面的断言。

**改法（更强，不是更松）**：改成**指纹相等** —— 用 CLI 契约测试自己的那个 helper
（`cliUsageTitle`，帮助页标题即身份），并要求 `stdout` 的标题 == `annotateUsage` 的标题。
原来的 `strings.Contains(stdout, "gopi annotate")` 连拼写都不对（页面从来没有这一行），
而任何**别的**页面只要含这个词就会通过；指纹相等把「印对了页」和「印了别的页」真正分开。

**最小回归测试**：就是这条子测本身（它是 argv 级 e2e），其回归面由契约测试的同指纹钉住：
`cli_contracts_test.go` 要求 `tool annotate -h` 的标题等于 `annotateUsage` 的标题 ——
两者一起绿 = 页面存在、是这一页、且没被别的页顶掉。

---

## 2 `TestChatBoundaryTenMegabyteInputStaysBounded` —— 断言过时（写成了不可满足的形式）

**红的样子**（真跑）：
```
chat_boundary_test.go:197: a 10485760-byte input produced a 10774-byte request body — the message grew with the input
```

**实测（探针真跑，四个量级）**：

| 输入 | 请求体 | 消息（turn context） | 比例 输入/体 |
|---|---|---|---|
| 65537 B | 10722 B | 1342 字符 | 6.1 |
| 70000 B | 10722 B | 1342 字符 | 6.5 |
| 1 MiB | 10730 B | 1350 字符 | 98 |
| 10 MiB | 10734 B | 1354 字符 | 977 |

**结论**：边界**工作正常** —— 请求体在 160 倍的输入跨度上只长了 12 字节，这 12 字节
全部来自引用自身的数字（`[paste N: …]` 的计数与 `generatedContextLen==len(msg)` 的
`size=NB`）。~10.7 KB 的常量部分是**chat 的系统提示 + 本轮 framing**，与输入无关。

**为什么该断言不可满足**：`len(body) < len(payload)/1000` 比的是两个不是同类项的东西 ——
分母是**原始输入**，分子是**系统提示 + framing**。要让 10 MB 满足它，请求体必须 < 10 KB
—— 而光系统提示就 ~9 KB。**它对任何 10 MB 输入都不成立，与边界是否生效无关**；
它当年绿只是因为那时 body 恰好 ~1 KB（framing 还小）。注释里那句「比 ~1 KB 小一万倍」
是把当时的**测量巧合**当成了**不变式**。

**判定依据（不是口味问题）**：判据是「**这行代码/这个测试在它被写下的那一刻是否是对的**」——
`83918dd9b`（2026-09-27，`feat(chat): port the message boundary onto master's spill
implementation`）引入该断言时，body 确实 ~1 KB，所以当时是**过时**而非**写错**；后来 framing
长到 ~10 KB，断言就与它声称的不变式（「消息不随输入增长」）**脱钩**了 —— 脱钩的方向是
断言变红，而实现一直对。

**改法**：
1. 这条案例改测**它名字里的那件事的尺寸**：turn context ≤ `8 × lineedit.InputLineRuneBound`
   （从**共享**的输入行上界派生，不是采样值；实测 ~1.3 KB，余量 ~400 倍）、整个请求体 ≤ 64 KiB
   （实测 ~10.7 KB，余量 ~6 倍 —— 而驱动的输入最小 65 KB，泄漏会以数量级失败）。
2. 那个真正的性质（「消息不随输入增长」）独立成一条**最小回归测试**：
   `TestChatBoundaryBodyDoesNotGrowWithTheInput` —— 真跑 65537 B 与 1 MiB 两轮，
   断言请求体增长 ≤ 64 字节（数字算术容差）。边界泄漏会加**兆字节**，比容差大五个数量级。

引用与 spill 断言（`[paste` 存在、spill 文件字节数 == 输入字节数）**原样保留**。

---

## 3 `TestChatStatus_LiveBlockKeepsTheStateWord` —— 断言过时（且在该行自身的几何下不可满足）

**红的样子**（真跑）：
```
chat_status_degradation_test.go:541: cols=100 elapsed=4m0s state=responding:
  the row does not carry "responding\u00a0" — the live block lost the state it describes
  row: "deepseek-flash ·  iter9   ·  ⚙ keyword_search i… ·  ctx 4k/64k≡                        4m00s · resp\u00a0"
```

**行里到底有什么**：`4m00s · resp␣`（`␣` = U+00A0，`statusLiveInFlightGap`）。
状态没丢：它被说成了**声明的短拼写**。

**两个事实（都是算出来的，且都有已提交的兄弟测试钉着）**：

1. **块是 13 格的预留**：`statusLiveCol = 13`（`844d1f3d2^` 与 `844d1f3d2` 两版相同），
   而行里已经有一个计数器（`4m00s ·`）。
   `lineedit.StatusSep + statusLiveWordResponding` = 3 + 11 = **14 > 13** ——
   也就是说**只要计数器在，全长词就放不下（1 位数计数器也一样）**，
   断言要求的字符串在这条行的几何里**不可满足**。
2. **「计数器挤掉词的长度」正是既定设计**：`statusLiveField` 的优先级是
   「压缩计数 > 时长；词的**拼写**可以缩短，词本身不能消失」，
   兄弟测试 `TestStatusLiveCompactionCounterOutranksTheWidestStateWord`
   （`844d1f3d2` 新增，绿）把「响应态必须说短拼写、配对必须落在 `statusLiveCol` 内」
   钉得比本测试更紧；它的 fixture 反过来还断言「计数器 + 全宽词**放不下**」。
   两条测试因此不能同时成立：那条绿着的兄弟测试，正是这条红着的断言的**反证**。

**判决**：这条断言按更晚的设计（短拼写是合法拼写）已经过时；它是
`844d1f3d2` 把 `844d1f3d2^` 的版本**原样带进来**的 —— 即**收敛提交本身把它带红**
（`844d1f3d2^` 的原文与 `844d1f3d2` 的该断言逐字相同；`844d1f3d2` 只改了
`statusProbeIter` 与新增了那条兄弟测试）。

**改法（更强，且把洞补上）**：从「必须含全长词」改成
「**行的设计承认的某个拼写必须在，且恰好一次**」，并附加一条**以前没有**的反向断言：

* 候选来自 `statusLiveStateWords`（**生产自己的**拼写表，不是测试的复制）；
* 出现次数必须 == 1（旧版也查这个）；
* **当 `statusElapsedLabel(e) + StatusSep + 全长词` 本身就放得进 `statusLiveCol` 时，
  必须显示全长词** —— 短拼写只能是「放不下时」的退路，绝不允许变成新默认。
* 行宽 ≤ cols 的断言原样保留（旧版也有）。

**这条反向断言的诚实射程（写进代码注释里了，别当它比实际更硬）**：在今天的几何下
它对两个宽态**不会触发**（全长配对本来就是 19 格 > 13），所以它今天的真实作用是
「把短拼写钉成退路而不是默认」的**哨兵**（若 `statusLiveCol` 或某个词的宽度将来动了，
它会响）。今天真正咬人的是**声明拼写检查**：一个设计没有声明的词（被截断的词、
或测试里自造的词）仍然红 —— 旧断言同样能抓「词不见了」，但它抓不住「截短」，
而且它把**被设计允许的短拼写**误判成丢失。

---

## 4 验证（Rule 5 / 31）

- **Build**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- **三条目标测试**：`go test ./cmd/agent/ -count=1 -run 'TestAnnotateVerbRunsARealArgvEndToEnd|TestChatBoundaryTenMegabyteInputStaysBounded|TestChatStatus_LiveBlockKeepsTheStateWord' -v`
  → **全绿**（见任务报告 §Verification 的原文）
- **Ran**（真跑被改动的 surface）：
  - `./bin/gopi tool annotate -h` → rc=0，首行 `gopi tool annotate — draw marks onto an EXISTING PNG …`
  - `./bin/gopi tool annotate base.png --out out.png --json --mark 'rect:60x30+20+20:label:fixed' --mark 'point:6x6+200+100::fixed'`
    → rc=0，`{"tool":"annotate",…,"marks":2,"labels":1,"ink_pixels":1182,…}`，out.png 确实写出
- **探针**：临时文件 `cmd/agent/zz_probe_bound_test.go`（已删除，worktree 干净）测出 §2 的四个量级表。

- **最小样本**：三条红都在**最小样本**上复现/判定：一条用 `git show <c>:<file>` 的
  四个时间点（object store 只读，不 checkout），一条用四个量级（65537 B → 10 MiB）的探针，
  一条用三个命名常量的算术（`statusLiveCol` / `StatusSep` / `statusLiveWordResponding`）。

## 5 Not Done（诚实）

- §3 的 PTY 端到端（真实 100 列终端里肉眼看到 `resp`）**没有做到**：本机时序没抓到 live block
  的 `responding` 帧（**是时序问题，不是产品结论**）。证据链是**源码算术**（`statusLiveCol`、
  `StatusSep`、`statusLiveWordResponding` 三个命名常量的关系）+ **已提交的绿测试**
  （`TestStatusLiveCompactionCounterOutranksTheWidestStateWord` 的 fixture 断言），
  不是「我看过屏幕」。
- `cmd/agent` 包里**其余**的既存红灯（CLI 重组家族、`TestKeywordSearch*`、`TestSandboxDirFlagDecidesTheRoot`
  等）**不属于本任务**，一处未动。
