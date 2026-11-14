<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# devdoc 归档判据（devdoc/archive/ 的准入门槛）

本文定义**一份文档什么时候可以搬进 `devdoc/archive/`** —— 一条可判定的判据、
把它钉住的机器检查、生成器给出的实测数字、以及剩余量与拆分建议。
归档区的读者界面（"这里是什么"）在 `devdoc/archive/README.md`；本文是**判据**。

## 1 判据（一句话）

> **一份文档可归档，当且仅当它已被 `devdoc/DOC-INDEX.tsv` 判为 `historical`，
> 且仓库里没有任何其它 tracked 文件按路径或文件名提到它。**

展开成三条，任一条不满足即**不搬**：

1. **class = `historical`** —— 由 manifest 决定，不由文件名决定
   （文件名会骗人；实测只有 90/1366 份 historical 的名字里带日期）。
   `normative` / `roadmap` / `index-meta` 一律不搬：它们（或它们承担的路由角色）
   是对**当前** master 的陈述，搬走会让它们的锚点腐烂无人看管。
2. **没有任何其它 tracked 文件提到它**（"提到" = 完整路径 `devdoc/x.md` 或文件名 `x.md`，
   在任意 tracked 文本文件里出现；文档自己提到自己不算）。**一处即足以拒绝。**
   这条之所以比"只看权威文件"更严，是被实测逼出来的（§3.1）。
3. **两条保守约束**（宁可不搬）：该路径出现在 **manifest 的 evidence 列**
   （它正撑着某个 normative 行的 spot-check），或**同 stem 家族的兄弟文档**持有权威
   （`-report`/`-audit`/日期后缀剥掉后同名），都不搬。

**被拒的 877 份不是"落选"，是"选址错误"**：它们本来就该被读到，只是必须先改引用、
或先改 class 才能搬 —— 那是另一个任务（§5.3）。

## 2 判据的机器钉子

判据本身由生成器测量（§3），而**"搬进来 = 是 historical"这个不变式**由 gate 钉住：

```bash
go test ./internal/standards/ -run 'ArchiveRegion'
```

`internal/standards/docarchive_gate.go` 读 manifest（class 的唯一来源，绝不读文件名），
报告 `devdoc/archive/` 下任何 class 不是 `historical` 的行，并带**自己的下限**
（区域里被分类的文档低于下限 = 读空/读残，报红而不是当作干净）。
`internal/standards/docarchive_gate_test.go` 是它的红证：一份 class 为 `normative`
的区域行必须被点名拒绝；同一行改成 `historical` 必须变绿（证明红来自 class 而非夹具形状）；
空区域必须报红（证明"没找到"不等于"没跑"）。

它回答的正是将来最可能发生的两种退化：
**误搬**（还是 normative 的文档被当成记录，于是 anchor gate 也不再看着它）与
**搬后改 class**（行的 class 翻转、文件留在区域里，路径与 class 互相矛盾）。

## 3 测量（Rule 38：数字带生成器与测量时的 commit）

```bash
python3 <session-dir>/archive-census.py .                     # 一行一个数字 + 逐臂计数
python3 <session-dir>/archive-census.py . --explain <file.md> # 单份为什么可/不可归档
```

生成器 = `archive-census.py`（本任务写下的可重跑脚本，随报告的文件清单给出会话内路径；
它的引用规则与分类器 `tools/docclass/classify.py` 的 `build_citation_index` 同源，三处刻意加强，
每处都是被一次实测错误逼出来的 —— 见 §3.1）。

在**本次提交的树**（`dab59ff6`）上实测，基线是它的父提交 `156e2e69`：

| 图 | 值 | 含义 |
|---|---|---|
| files | 2013 | `devdoc/` 下的 tracked 文件数 |
| historical | 1367 | manifest 判为 historical 的行数（含区域 README 自己的一行） |
| historical-md | 1122 | 其中 .md 散文文档 |
| refused | 877 | 判据**拒绝**的 historical |
| archivable | 490 | 判据允许归档、尚未搬的 .md |
| archivable-dated | 24 | 其中**文件名自带日期**的子集 |
| archive-dir | 79 | 已在 `devdoc/archive/` 下的文件（77 搬入的 .md + 区域 README 1 + 既有 `.go` 1） |

**逐臂计数**（拒绝的 877 份分别栽在哪一条，各臂互不重叠、按判据里的检查顺序判定）：

| 臂 | 份数 | 含义 |
|---|---|---|
| named-by-tracked-file | 246 | 某个其它 tracked 文件提到了它（§1 第 2 条） |
| not-.md | 245 | 根本不是 `.md`（本次判据只对散文文档生效） |
| named-by-current-artifact | 130 | 非 devdoc 的权威文件按**文件名**提到它 |
| path-cited-by-normative | 115 | 某份 normative 文档按**路径**引用它 |
| already-nested | 112 | 已在某个子目录下（不在 `devdoc/*.md` 平面） |
| path-cited-by-non-devdoc | 13 | 非 devdoc 文件按路径引用它 |
| in-the-manifest-evidence-column | 6 | 它（或它所在家族）正撑着某个 normative 行的 spot-check |
| path-cited-by-roadmap | 6 | 某份 roadmap 文档按路径引用它 |
| sibling-held-by-an-authority | 4 | 同 stem 家族的兄弟是有权威的 |

（提交者给的旧数字 1710 / 1361 是**改过的**：present 的文件是 2011，historical 行是 1366
（加区域 README 一行 = 1367）。`archive/` 目录在本次之前只有 1 个 `.go`。）

**本表是快照，不是常数**：它测量于 `dab59ff6`。合并 master 后（本分支 tip `ca0cd0b8`）
同类图随别人新增的两份 devdoc 小幅上移 —— files 2015 / historical 1368 / historical-md 1123 /
refused 878，而 **`archivable` 与 `archivable-dated` 不动（490 / 24）**：
新来的文档都被它们自己的引用钉住，判据对"哪些该搬"的结论不变。
任何时刻要当前值，跑生成器（它自己会打印测量时的 commit）。

### 3.1 判据在被实测打脸后收紧的三处（每条都有观察）

第一版判据是"只看**权威**文件的引用"（非 devdoc 文件 + normative/roadmap 文档），
按它选出 86 份并搬了。搬完做**引用复查**时，三条缺陷暴露出来：

1. **"权威"漏掉了 historical 自己的引用。** 一份 historical 文档链接另一份 historical
   文档，也是链接；按第一版判据它会跟着被搬走，留下一条读不到的链接。
   实测 9 份被搬文件仍被别的 tracked 文件提到 —— 其中 2 份来自一份 normative 文档和一份
   roadmap，7 份来自 historical 兄弟。**这 9 份已全部 `git mv` 撤回原位**。
2. **路径扫描漏掉句末标点。** 第一版正则把 `devdoc/ab-test-runcmd-20260904.md.`
   （句号结尾）整段吞掉，于是 `devdoc/night-progress-20260904.md` 里那条真实引用
   没被看见，那份文档被判成"可归档"上了搬迁名单。收紧后方才现形。
3. **清单自己的路径不算引用。** manifest 每一行都含它自己的路径，
   若照字面统计，**每一份** historical 都会被判"被引用"（这正是第一版 `--explain` 打印
   `ARCHIVABLE` 却与汇总数字矛盾的原因）。一行不能撑起自己的主语；
   Go 侧 `internal/docindex/docindex.go` 的 `RepairableGaps` 对 manifest 自己的行做了同样的豁免。

收紧后的判据对**已搬的 77 份逐份复验：0 份被拒**；对**撤回的 9 份逐份复验：9 份全被拒**
（正确留下来的理由）；再用**最宽松规则**（纯子串匹配，不做任何 token 边界）
复核 77 份在整棵树里的出现：**0 命中**。

## 4 第一批：执行了什么，为什么是这个子集

**已在 `devdoc/archive/` 的 77 份 `.md`**（外加区域 README 1 份与既有的 1 份 `.go`）
= 判据允许、**文件名自带日期**、日期在 2026-08-03 … 2026-09-12 段的那一段
（`bugfix-*` / `night-testsuite-*` / `handoff-*` / `merge-*` / `code-review-*` 等运行记录）。

为什么取这个子集（宁可少做、做对）：

- **最不容易误搬**：文件名里的日期本身就是"这是一次运行的记录"的声明，判据两侧
  （class=historical、无其它文件提到它）在它身上是最强的形态，逐份复核成本最低。
- **批次边界不含主观选择**：同一判据可以再取下一段日期，"剩余量"因此是一个可分批推进的数字，
  而不是一次大裁决。
- **逐份先查引用**：每一份都单独跑过判据（`--explain` 输出为空 = 可归档），
  没有一份是"看着像就搬"；搬完又用最宽松规则全树复核了一遍（§3.1 末）。

搬迁方式：`git mv`（**内容零改动**，Rule §3.4）；`DOC-INDEX.tsv` 由生成器写新行
（`classify.py . --only devdoc/archive/<name>.md`，**只加行**）；旧路径的陈旧行按
`devdoc/archive/README.md` "归档配方"一节的外科式做法收掉 —— 生成器不删陈旧行，
`gopi doccheck --fix` 对 `row-with-no-file` 明确回"none of these can be written by this command"。

## 5 剩余量与拆分建议（诚实）

| 量 | 值 |
|---|---|
| 已在区域 | 79 份文件（77 搬入的 `.md` + 区域 README 1 + 既有 `.go` 1） |
| 判据允许、尚未搬 | **490 份 `.md`** |
| 判据**拒绝**、需要先动引用或改 class | **877 份 historical** |
| historical 总数 | 1367 |

**490 份不是一个 session 的工作量，也不该是。** 建议的批次切法（每批 ≤ 60 分钟，
判据与配方不变，只有切片不同）：

1. **批 2（日期续段）**：`archivable-dated` 的其余文档按日期倒序推进
   （新记录先隔离，收益最高）。
   验收：本批同样的五步；`gopi doccheck` exit 0；`classify.py --check` 报 0 gap(s)；
   `go test ./internal/standards/ -run 'Doc|ArchiveRegion'` 全 PASS。
2. **批 3+（名字前缀切片）**：按 `-report` / `-audit` / `-sweep` / `-verification` 等
   后缀分组整组推进，避免"半族在区域里、半族在外"的选址矛盾
   （本机实测整族可搬的家族只有 1 个两成员族 + 473 个单成员族，所以切片≈按名字前缀切）。
   每批 60–80 份是实测舒适区（本批 77 份的搬迁 + 生成器 + 门禁 ≈ 20 分钟）。
3. **收口任务（独立 epic）**：877 份被拒的 historical 要的不是搬迁，是**改引用**
   （把代码/briefing/normative 文档里的路径改指区域）或**改 class**。
   本任务只给出数字与逐臂分解（§3），不代为裁决。
4. **非 `.md` 的 historical（245 份）**：判据目前只对 `.md` 散文生效
   （只有散文才有"被当成现行依据"的风险）。`.tsv/.py/.sh/.go` 记录件在 `devdoc/` 平铺多年，
   是否需要同样的隔离是**另一个裁决**，本文不下结论。

**为什么不做全量**：一次 `git mv` 490 份会得到一个无法逐份复核的 diff，
而判据的价值恰恰在"逐份可查"；误搬一份 normative 文档的代价（它的锚点从此无人看管）
大于少搬一百份记录的成本。

## 6 边界（明确不做的事）

- **不修改被归档文档的内容**（Rule §3.4）—— 搬迁只改路径。
- **不删除文档**，**不动 `devdoc/rules-reference.md`**（它是 Tier B 的引用目标），
  **不动 GOPI.md 的 Tier A/B 结构**。
- **不搬 normative**、**不搬被其它文件提到的 historical**、**不搬 evidence 列正在用的锚**。
- **不在 `devdoc/archive/` 里新建文档**（区域 README 是唯一例外：它的任务就是当界面）。
- **不新造工具**：判据的钉子是 `internal/standards` 里的一节（Rule 3/35）；
  测量是一次性脚本（随报告交付，不进 `tools/`、不进 Makefile）。
