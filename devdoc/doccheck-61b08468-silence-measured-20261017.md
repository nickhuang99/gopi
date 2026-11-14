<!-- doc-class: historical -->
# doccheck `--fix` 的「>3 分钟 0 字节」在 **61b08468** 上重测：机制已点名（2026-10-17）

**本文件是一次测量的记录**（Rule 41：实测与推断分开写），不是对 master 的常设主张。
被测量的是**一个固定的历史 revision**：`61b08468322b83c398ec617afeb4120f25692ef4`。

---

## 结论

对 EPIC 报告的那句话（`gopi tool doccheck --fix` 在 base 61b0846 上「>3 分钟、0 字节」），
本次在 61b08468 本身上的实测结论是：

> **「>3 分钟」可复现（在 N≥1 时确实是 201.2s 的真实工作量）；「0 字节」不可复现 ——
> 第一行在 t=0.214–0.477s 就到了，任何 ≥1 gap 的跑法里都在这一秒内开口。**
> **静默段本身被点名、被度量、且随 N 线性增长**：61b08468 上
> **N=0 → 最大行间静默 15.02s；N=1 → 22.83s；N=3 → 74.15s**。

把上一行的机制说成一句话：**61b08468 的 `--fix` 有一条「只算不响」的腿 ——
逐路径生成器（`RepairGap` → `ClassifyWithGenerator`），它在**语料级渲染腿之前**跑，
而它每等一个 python3 子进程都**一个字节都不打**。** 它的时长 ≈ **N × 约 23–25s/路径**，
上不封顶。N≥4 时它单独就能吃掉调用方的 120s no-output 安全网。

## 1. 方法（先证身份，再测量）

树从**对象库**取（Rule 29：不用第二棵 worktree、不碰主检出），二进制带 revision 戳：

```
$ git archive 61b08468 > $EV/61b08468.tar
$ sha256sum $EV/61b08468.tar
4ff65e72bf11120b3152a2d3ae15e15906aff9d3b4ae0df3d4d3579d810229b4  …
$ git rev-parse 61b08468^{tree}
0d6b885384666e6de8c7412e8c64293a6e96e541
$ go version -m $EV/bin/gopi-61b08468-vcs | grep vcs
	build	vcs.revision=61b08468322b83c398ec617afeb4120f25692ef4
	build	vcs.time=2026-10-16T03:23:35Z
	build	vcs.modified=false
```

**「0 字节」必须在文件上量，不能在管道里量**：管道会缓冲，从管道看到的「没有字节」
是关于**缓冲区**的断言，不是关于写者。主测量（6 次）用
`artifacts/measure-arrivals.sh`（子进程 stdout+stderr **重定向进普通文件**，
harness 每 0.05s 采样行数增长，按行落 `elapsed\tline`，并自报
`first_byte` / `max_inter_line_gap`）。工具自己不做缓冲（`os.Stdout` 是无缓冲 `*os.File`）——
这一句本次**在 61b08468 上自己实测过**，不再借 9abec60 的结论：第 7 次测量
（`artifacts/measure-pipe-arrivals.sh`，把同一个 `--fix` 面的 stdout+stderr 接进**管道**，
由读取端打到达戳）得到 `first_byte=0.466s`、`max_inter_line_gap=15.014s`（= tick 周期，
落在 0.845→15.859），与文件臂 **逐行到达时刻一致** ⇒ **管道缓冲不是那条「0 字节」的机制**。
（该臂由本次 run 自己在 `wall=125.945s` 处 `kill` 收尾 → harness 记 `exit=143`；
那是**我的终止**，不是工具的行为，故不计入任何时长结论，只用于「到达时刻是否一致」这一个比较。）
第 8 次测量把同一个条件**跑到底**：N=3 注入、管道、无人为终止 →
`first_byte=0.181s`、`max_inter_line_gap=80.111s`（= 腿 B）、wall `222.997s`、`exit=0`
（见 §3 的第 6 臂与 §9 的 `run-pipe-n3.sh`）。
**一条自曝**：本 run 早期还写过 `arrivals-pipe-fix-asis-61b08468-complete.tsv`（45 行、**无**
`HARNESS` 尾行，是我在 kill 之后另起一次、又在 15.9s 处收尾的残留），它的数字**不作为证据引用**；
被引用的是上面两个带 `HARNESS` 自报行的臂。

## 2. 先从**该工具自己**读出这棵树携带的 N（而不是猜）

```
$ python3 - <<'PY'   # 只读 devdoc/ 与 manifest，不调分类器
manifest rows: 2566
devdoc .md files with NO row: 0
stale rows whose file is gone: 1
PY
```

**61b08468 的 N = 0**：2566 行 manifest 覆盖了全部 2189 个 docdev `.md`，零缺失；
唯一那一行是**文件已不在**的 stale row —— 它属于 `RepairableGaps` 的**不可写**半
（删别人的许可不是一个检查该做的决定），**不进**逐路径生成器腿。

这条把一个**推断**直接证伪：上一份报告推测「当时 N≥7 份未分类文档（29s×7≈203s）」。
在 61b08468 上 N=0，所以那次**不可能**是「N 很大吃掉了窗口」。

## 3. 事实、测量

| # | 条件 | first_byte | 行数 | 最大行间静默 | wall | exit | manifest |
|---|---|---|---|---|---|---|---|
| 1 | 只读（对照组） | **0.476s** | 53 | 15.032s (30.77→45.802) | 59.710s | 0 | 不变 |
| 2 | `--fix`，N=0（树原样） | **0.477s** | 107 | 15.022s (45.842→60.864) | **124.821s** | 0 | 不变 |
| 3 | `--fix`，N=1（注入 1 个 gap） | **0.214s** | 109 | **22.829s** (64.570→87.399) | 149.482s | 0 | `8b1a0275…` → `d431a92f…`，2566→2567 行 |
| 4 | `--fix`，N=3（注入 3 个 gap） | **0.215s** | 111 | **74.151s** (65.793→139.944) | **201.248s** | 0 | 写入后已还原 |
| 5 | `--fix`，N=0，**经管道**读 | **0.466s** | 102 | 15.014s (0.845→15.859) | 125.945s（被本次 run kill） | 143（我的 kill） | 不变 |
| 6 | `--fix`，**N=3 注入，经管道读，跑到底（不 kill）** | **0.181s** | 111 | **80.111s** (65.001→145.112) | **222.997s** | 0 | 写入后已还原（`status_lines=0`） |

第 6 臂是**提报者观测条件（管道）下的完整一次跑**（`artifacts/run-pipe-n3.sh`，无人为终止，
`artifacts/arrivals-pipe-fix-n3gap-61b08468.tsv`，harness 原文
`HARNESS	first_byte=0.181	exit=0	wall=222.997` /
`HARNESS	lines=111	max_inter_line_gap=80.111	from=65.001	to=145.112`）。它的 80.111s 静默由
`145.112	doccheck: repaired 3 path(s): devdoc/zz-pipe-gap-1.md, …` 结束，与文件臂 N=3 的
74.151s 同量级（差 8%，两次跑的不是同一刻的机器负载）⇒ **腿 B 的零字节静默在管道下同样复现**，
而**第一字节仍在 0.181s**。该臂同时给出这条机制的**上界形态**：管道下 `--fix` 的
111 行里有 **9 段 > 5s 静默**，其中 8 段是 15s tick，唯一超长的那段就是腿 B。

N=1 的那 22.829s 窗口由这一行结束（`artifacts/arrivals-fix-n1gap-61b08468.tsv:55`）：

```
87.399	doccheck: repaired 1 path(s): devdoc/zz-injected-gap-fixture.md
```

N=3 的 74.151s 同理，由 `139.944	doccheck: repaired 3 path(s): …` 结束。
**每路径常数 = 74.151−22.829)/(3−1) ≈ 25.7s，与 N=1 的 22.8s 同量级 ⇒ 约 23–26s/路径。**

## 4. 机制（读代码 + 计时，不是猜测）

`cmd/agent/cli_doccheck.go:506-508`（61b08468）：

```go
code := docCheckReport(root)     // ← 完整的只读报告，先跑一遍
if opts.Fix {
    code = docCheckFix(root)     // ← 再跑一遍 repair 腿，内部又 render 一次
}
```

于是 `--fix` 的**逐腿时序**（`--fix` 的时序与只读**逐字相同**，43 行到达时刻完全相同 ——
`diff` 实测 `IDENTICAL first 43 lines`）：

```
t=0.214s  第一行落盘（N≥1 时是 file-with-no-row 那几行；N=0 时是 0.477s 的 anchor 小节）
t=0.860s  腿 A #1 宣告：the corpus's own classifier is running over 2565 path(s) …
t=15.8/30.8/45.8/60.8s   腿 A #1 的 15s tick（可见 —— 2fd04cfad 的机制）
t=63.4s   腿 A #1 返回、第一份报告打完
t=63.6s   进入 --fix：RepairGap → ClassifyWithGenerator（**零字节**，N×约 23–26s）
t=87.4s   "repaired N path(s)"      ← N=3 时是 t=139.9s
t=87.8s   --fix 内部再问一遍 anchor 小节（43 行）
t=88.2s   腿 A #2 宣告：the corpus's own classifier is running over 2565 path(s) …（**第二次全语料渲染**）
t=103/118/133/148s       腿 A #2 的 15s tick
t=149.5s  verdict
```

两条腿，各自的量级：

- **腿 A（语料级渲染，1 个 python3 进程渲染全语料）**：有宣告 + 每 15s tick
  （`2fd04cfad`，已机器验证**是** 61b08468 的祖先：`git merge-base --is-ancestor 2fd04cfad 61b08468`
  → **exit 0**）。它**不**沉默，但它被**跑了两遍**（见 §6）。
- **腿 B（逐路径生成器）**：`docCheckFix`→`RepairGap`→`ClassifyWithGenerator`
  对每个 missing 路径起**两个** python3（`classify.py --only` → `banner.py --only`），
  **全程零字节**。实测它在腿 A **之后**跑（`repaired` 行落在 87.4s / 139.9s，
  而腿 A 的第二遍宣告在 88.2s / 140.4s），**所以它落在窗口的中段，不是开头**。

## 5. 那句话到底怎么成立、怎么不成立

- **「>3 分钟」成立**：N=3 实测 **201.248s** > 3 分钟，而且**全是真活**
  （它是 1 遍只读报告 + 1 遍完整 repair + 1 遍全语料渲染 + 3×2 个子进程）。
  没有任何合成负载（Rule 27）。
- **「0 字节」不成立**（在 61b08468 的**任何** N 下）：N=0 时第一行在 0.477s；
  N≥1 时第一行在 0.214–0.215s（gap 行）。要让 120s 安全网开火，需要的是
  **N = 3 就够了，而且这是实测不是外推**：管道臂第 6 臂的静默窗是 `65.001→145.112`（80.111s），
  **它整段把 t=120s 包在里面**。所以一个 120s no-output 安全网在 N=3 上就会开火，
  且开火时刻落在腿 B 的正中间 —— 也就是说
  环境确实可以让调用方看到一段 120s 级沉默，但**第一字节并没有晚到那么久**，
  沉默发生在**中段**。
- 因此上一份报告把机制列成「(a) N≥7 吃掉窗口 / (b) 根本没进打印路径」的**推断**，
  在 61b08468 上的正确形态是**第三种**：**N=0 时没有缺口，而 `--fix` 把语料级渲染
  跑了两遍，N≥1 时再叠加一条零字节的逐路径腿**。

## 6. 顺带更正两条**引用**（实测，与结论同等重要）

1. **`2fd04cfad` 是 61b08468 的祖先** —— 上一份报告写「是共同祖先」，
   本次按 EPIC 的验收 3 精确执行：`git merge-base --is-ancestor 2fd04cfad 61b08468` → **exit 0**。
   引用的这条**是对的**。
2. **修复 commit 的短 sha 引用是错的**：Bug 行里记的 `3bfa56`，本仓按该前缀
   `git log --all | grep '^3bfa5'` **匹配不到（`git rev-parse 3bfa56` 报
   `fatal: ambiguous argument`）**；它实际是 **`3bfa42b56d392aed3fe218c0809e04c2ee81b79f`**
   （`fix(doccheck): announce and tick the PER-PATH generator phase`，2026-10-16 17:49:10），
   而且它**不是** 61b08468 的祖先（`git merge-base --is-ancestor 3bfa42b56 61b08468` → **exit 1**）。
   也就是说：**腿 B 的静默在 61b08468 上是真实存在的**（本次实测 22.8s / 74.2s），
   修它的那个 commit 在 base 之后 —— 两者自洽。

## 7. tip 上仍然**活着**的那条（提行依据，见 Rule 41）

对**同一个 `--fix` 面**在 tip 复测（二进制 `vcs.revision=cc846ee0e6…`，实测）：

| 条件（tip cc846ee） | first_byte | 最大行间静默 | wall | exit |
|---|---|---|---|---|
| 只读 | 0.214s | 15.043s | **67.449s** | 1（15 条 stored-cell drift） |
| `--fix`（N≥1，实测写入了 manifest） | 0.214s | 15.030s | **133.362s** | 1 |

- **静默已受控**：tip 上 `--fix` 的最大行间静默 **15.030s = tick 周期**，
  与 N 无关 —— `3bfa42b56` 的修法在工作（本次实测，不是推断）。
- **但「同一份语料渲染跑两遍」仍然活着**：`cmd/agent/cli_doccheck.go:528-531` 在 tip
  上仍是 `docCheckReport(root)` 然后 `docCheckFix(root)`，实测代价是
  **133.362s vs 67.449s ≈ 2×**，其中第二遍渲染**只是把同一份字节重新算一次**。
  这是本次在 tip 实测到的、仍然存在的缺陷，已按 Rule 41 单独提行（不做越界修复）。
- **同一批 tip 测量里另有一条实测（补记）**：`scratch-tip` 那棵树在上述 tip 各臂跑完后，
  `git status --porcelain` 报 ` M devdoc/DOC-INDEX.tsv`，diff 是**恰好 2 行删除**，
  且两行都是 `devdoc/merge-compile-only-policy.md` 的重复行（`artifacts/scratch-tip-DOC-INDEX-drift-dedup.diff`）
  ⇒ **文档检查路径里确实有一段会把重复行去掉**，而 `classify.py --only <新文件>` 那一段又因为
  重复行**拒绝写入**（见 `## Verification` 证据 8）。
  **归属已新跑实测（不再留在推断）**：`artifacts/scratch-tip-drift-attribution.txt` —— 在
  **未改动**的那棵 tip scratch 树（`dup_rows=3`, `total_rows=2621`）上分离两腿：
  **只读腿 `leg1_exit=1 leg1_wall=62.96s`，跑完 `status_lines=0 dup_rows=3`（一个字节都没写）**；
  **`--fix` 腿 `leg2_exit=1 leg2_wall=128.43s`，跑完 `status_lines=1 dup_rows=1`**，
  `git diff --stat` = `1 file changed, 2 deletions`，**删掉的正是两条 `normative` 重复行，
  留下的仍是那条 `historical` 行** ⇒ 写者 = **`--fix` 腿**（不是只读腿，也不是"某一次调用"）。
  两腿的日志**都**打了同一行 `doccheck: duplicate-row devdoc/merge-compile-only-policy.md —
  devdoc/DOC-INDEX.tsv carries more than one row for this path`（只读腿 `raw-readonly-tip-attrib.log:1`，
  `--fix` 腿 `raw-fix-tip-attrib.log:1`），即**报告腿只报不改，修复腿报且改**。
  （该 scratch 树已 `git checkout --` 还原。**本次 run 没有**在自己 worktree 的 manifest 上跑过
  `--fix`，所以那里仍是 base 原样：`git show HEAD:devdoc/DOC-INDEX.tsv | grep -c
  '^devdoc/merge-compile-only-policy.md'` → **3**，行号 1290/2629/2630，两个 `normative`
  一个 `historical` —— 这条也说明"重复行已被修掉"**不成立**，它还在 base 上。）

## 8. 残留的推断（标明为推断，不得当判据）

- **管道那一半：已实测排除（不再是推断）**。两次管道臂都做了：N=0 那次在 125.945s 被我
  kill（`first_byte=0.466s`），N=3 那次**跑到底**（`first_byte=0.181s`, wall 222.997s,
  最大静默 **80.111s** = 腿 B）。管道下第一字节仍在**亚秒级**，而腿 B 的零字节静默
  **完整复现** ⇒ 「看不到字节」如果是缓冲造成的，就该在 0 秒前也看不到——事实相反。
- **推断（唯一残留的那一条，且已可量化）**：那次调用看到的是**腿 B 中段的连续沉默**，
  而调用方把这段沉默记成了「零字节」。**实测支撑**：N=3 在管道下腿 B 实测 **80.111s**
  零字节，且该腿随 N 线性增长（文件臂 `22.829s`(N=1) → `74.151s`(N=3)，
  斜率 ≈ 25.7s/路径）。**实测的交叉点**：N=3 的静默窗 `65.001→145.112` 已经把 t=120s 包住，
  故 120s 级安全网在 N=3 上即会开火（N 再大只会更早被包住）。
  **仍是推断的部分**：本次**没有**跑 N≥6（预算不允许：N=3 已经 223s，N=6 约 400s），
  也**无法重放那次调用本身**（调用方的 harness、当时的 N、机器负载都不可考）。
  换句话说：机制侧**全部是实测**（腿 B 存在、零字节、随 N 线性、在管道下同样成立），
  只有「那次调用的 N 到底是多少」是推断。
- **推断**：腿 B 在 61b08468 上的每路径成本可能还含一个**未被点名**的固定量
  （N=3 减去 N=1 的斜率 25.7s/路径 与 N=1 的 22.8s 不完全相等），本次没有把
  `classify.py --only` / `banner.py --only` 分开计时，故不宣称拆分比例。

## 9. 证据清单（全部在本次 run 的 session 目录，未写进树）

`/home/user/.gopi/sessions/01a1441e-0153-728f-8757-b3cc3dfe5941/`
- `artifacts/measure-arrivals.sh` —— 逐行到达时间 harness（文件臂，非管道）
- `artifacts/run-pipe-n3.sh` —— **第 8 次测量**（管道 + N=3，无人为终止，跑到底）
- `artifacts/arrivals-pipe-fix-n3gap-61b08468.tsv` / `artifacts/pipe-n3-run.log` —— 该臂的
  逐行到达戳与自报 `HARNESS first_byte=0.181 exit=0 wall=222.997` / `max_inter_line_gap=80.111`
- `artifacts/run-arms.sh` / `run-arm4-n3.sh` / `run-tip-arm.sh` —— 三次调用的原文
- `artifacts/arrivals-readonly-61b08468.tsv`、`arrivals-fix-asis-61b08468.tsv`、
  `arrivals-fix-n1gap-61b08468.tsv`、`arrivals-fix-n3gap-61b08468.tsv`
- `artifacts/arrivals-readonly-tip.tsv`、`arrivals-fix-tip.tsv`
- `artifacts/raw-*.log` —— 各臂的原始合并输出
- `scratch-61b0846/`、`scratch-tip/` —— 两棵 `git archive` 树（**在 worktree 之外**）
- `bin/gopi-61b08468-vcs`、`bin/gopi-tip` —— 两个带 vcs 戳的二进制

二进制身份的取得方式（记下来，因为踩过一次）：`git archive` 出来的树没有 `.git`，
`go build` 便**静默不打** `vcs.revision`；本次的做法是在该树内 `git init` +
`objects/info/alternates` 指向主库对象目录 + `update-ref` 固定到目标 commit，
再构建 —— 于是 `go version -m` 读出 `vcs.revision=61b08468…`、`vcs.modified=false`。
