<!-- doc-class: historical -->
# devdoc: doccheck 的 drift 报告必须能定位 —— 回显窗口锚在分歧点，路径全列

**背景任务**：`01a14317a5d576eb97ca2cd35e3ed5e0`（`cb7e3dc1a` 上实测）。
**现象**：`gopi tool doccheck` 第三腿（stored-cell drift）报 `12 of 2585`，rc=1，
但**从这份输出里定位不到**漂移在哪。

## 一、缺陷有两半，缺一半都还是不可行动

### 第一半：回显从字节 0 截断 ⇒ 差异看不见

报告行是 `stored %q rendered %q`，两侧单元格各自**从字节 0** 截到
`docclassDriftCellClip = 110` 字节。而漂移单元的形状恰恰是
**长公共前缀 + 尾部差异**（re-derive 出来的单元格与它替换的行共享整段散文，
只在 live-derived 尾上移动：`path.go:NNN`、` stale=N`）。

实测（`cb7e3dc1a`，本仓库自己的语料）：被回显的 3 行里
**2 行两侧印成两个一模一样的引号**：

```
… reason: stored "…internal/removalaudit/audit.go:188; a landed ruling: cited…"
          rendered "…internal/removalaudit/audit.go:222; a landed ruling: cited…"
```

差异在第 **106** 字节，而窗口在 110 处结束——**一行之差**。
相关性是精确的：回显不可读的行，正是差异位置晚于 clipp 的行。

**修法**：窗口**锚在首个分歧字节之前** `docclassDriftDivergenceLead = 40` 字节处，
两侧共用**同一个窗口**（`docclassDriftEchoPair`）；窗口非空时有前导标记、被截断时有尾部标记，
且在 rune 边界上回退（避免把多字节字符切开而印出"看起来像差异"的替换字形）。
短到无需截断的单元格仍**逐字原样**回显。

### 第二半：回显预算同时是**唯一**出现过路径的地方

`docCheckStoredDriftExamples = 3` 同时管着"回显几个单元格"和"屏上出现几个路径"——
于是一份 12 行的报告只点名 3 个文件，其余 9 个化成一个算术余数
（`… and 9 more drifted row(s) NOT echoed`）。而同一屏给出的修复命令是
**按单路径**的（`classify.py <repo> --only <path>`）：报告让它的读者
对 3/4 的总体执行不了自己的指示。

**修法**：拆开两个界。`docCheckStoredDriftPathHead` 下**逐行列出全部漂移路径**
（`docCheckStoredDriftPathLine`，带 manifest 行号），**单元格**回显仍受预算约束；
收尾行改称 `... whose CELLS are not echoed — N counted, M echoed above (the paths are all listed above)`。

## 二、判定：12 行是「过期单元」，不是 classifier 行为变化

判据是**决定性**的（两个快照对照，不是推断）：

| 判据 | 做法 | 结果 |
|---|---|---|
| classifier 变了吗 | `git archive 670be9148 tools/docclass` 取当时的脚本，对同一批路径 `--dry-run --only` | 与今天的渲染**逐字节相同** ⇒ 没变 |
| 那次重生成写错了吗 | `git archive 670be9148 \| tar -x`（整棵树快照），在其**自身树**上渲染并与当时的 manifest 对比 | **match=12, stale=0** ⇒ 写得完全正确 |
| 那漂移从哪来 | `670be9148` 之后代码移动的次数 | `audit.go` 1 次、`cli_tasks.go` 5 次、`repo_sweep.go` 1 次 ⇒ **行号移位**；12 个文档本身 0 次提交 |

即：manifest 把 `路径:行号` 与 ` stale=N` **物化**进了单元格（classifier 自带设计——
drift 是 `reported, NOT fatal`，清除是 per-path 决定），而代码在每次提交里移动，
manifest 不随每次提交重生成。

**陷阱（我踩过，记下来）**：先做「当时的存储 vs 今天的渲染」对比，会读成
"10 行在那次重生成时就已过期"——**这是污染读数**：今天的渲染用的是今天的文件内容。
必须**两边都用同一个 rev**（整棵树快照 + 该 rev 的 manifest），结论才成立。

## 三、回归防护

- `internal/docindex/docclass_drift_test.go`：`TestDocclassDriftEcho_ShowsALateDifference`
  （晚差异必须在回显里可见 + 短单元格逐字原样）、`TestDocclassDriftEcho_APrefixDifferenceIsVisible`
  （一个单元格是另一个的**前缀**，缺失的尾就是差异）。红证明已执行：把 echo 退回
  "两侧都从 0 截断"两个臂**都转红**，恢复后绿。
- `cmd/agent/doccheck_stored_drift_verdict_test.go`：`TestDoccheckStoredDriftArm_NamesEveryDriftedPath`
  （总体大于单元格预算时**每条路径**都在屏上；单元格回显仍守预算；收尾算术仍对账）。

## 四、真跑证据（Rule 31）

真语料（2585 行）上 `./bin/gopi tool doccheck`，rc=1，第三腿现为：

```
doccheck: stored-cell drift: 12 of 2585 rendered row(s) …
doccheck: stored-cell drift: every drifted row, by path (…):
doccheck: stored-cell drift:   devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md (manifest line 19)
… （12 条全列）…
doccheck: stored-cell drift: … reason: stored "…audit.go:188…" rendered "…audit.go:222…"
doccheck: stored-cell drift: … reason: stored "…merge_prompt.md:709…" rendered "…merge_prompt.md:742…"
doccheck: stored-cell drift: … evidence: stored "…(bool, error) {" rendered "…(bool, error) { stale=1"
doccheck: stored-cell drift: ... and 9 more drifted row(s) whose CELLS are not echoed — 12 counted, 3 echoed above (the paths are all listed above)
```

**修复前**屏上的 9 个未命名路径，现在全部可见；三处此前不可见的差异（`:188`→`:222`、
`709`→`:742`、`{`→`{ stale=1`）现在都在回显里。

**遗留数据**：12 行**本身**未清（本任务只改报告与判定，不清数据）——
已另派一行，附完整判定与逐路径重生成命令。

## 五、附带实测（另一条 bug 行）

该 render 阶段实测 **345s**（23 条 15s tick），而 `docclass_drift.go` 的注释写
`this phase runs ~190s on this repo's own corpus`：**理由已过期**（取值仍安全）。
前台调用会被 120s 空闲网杀掉，已另提 bug 行。
