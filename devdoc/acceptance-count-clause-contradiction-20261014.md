<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 验收条款把一个「定义只出现一次」写成了 `grep -c` = 1 —— 而该常量必须被打印，全量计数不可能是 1

**裁决（2026-10-14，行 `01a139af-2687-70df-b55c-bd8c0e55d471`，父行 `01a13985-1297-7210-9cf8-dfe0b4970a7f`）**：
父行 ACCEPTANCE 的第一条 `grep -c SELF_ROW_STALE_NOTICE tools/docclass/classify.py` = 1
是一个**措辞缺陷，不是代码缺陷**。它想断言的性质是 «这个常量只有一个定义»（Rule 13/14），
写下的却是 «全量出现次数»，而全量计数在一个正确的实现里**必然是 3**（1 定义 + 1 注释引用 +
1 打印点）。被测代码满足意图，要修的是**判据的写法**。

本文件的下半部分是给同族任务**复用**的行为级判据措辞（§5），上半部分是它的实测依据。

---

## 1. 矛盾的实测证据

### 1.1 父行自己的文本：同一段里，同一个命令给了两个互斥的值

父行 `01a13985-1297-7210-9cf8-dfe0b4970a7f`（peerbox 账本，`status: completed`）的 briefing
原文，逐字（副本：`~/.gopi/sessions/01a139af-2687-70df-b55c-bd8c0e55d471/artifacts/parent-row.txt`）：

```
$ ssh peerbox 'gopi task show 01a13985-1297-7210-9cf8-dfe0b4970a7f'
…
MEASURED ON MERGED MASTER b51a763d2 (verify these first; …):
- `grep -c SELF_ROW_STALE_NOTICE tools/docclass/classify.py` = 0          ← §WHAT IS MISSING
…
ACCEPTANCE:
- `grep -c SELF_ROW_STALE_NOTICE tools/docclass/classify.py` = 1 (the skip is present)
…
THE FIX ALREADY EXISTS AS A REF … carry: (a) a named constant `SELF_ROW_STALE_NOTICE`
(Rule 15: the step-aside is PRINTED, never silent), … (c) a regression test asserting the
step-aside exists AND is printed.
```

三句话放在一起即自相矛盾：

- 要求「**必须被打印**」⇒ 常量出现在 ≥2 处（定义 + 打印点）⇒ `grep -c` **≥ 2**；
- 验收要求 `grep -c` **= 1**；
- 而 = 1 的含义正好是 Rule 13 的**缺陷**形状：常量被定义却从未被引用。

### 1.2 上游参照提交 af7e59ad4：同一个量本身是 3，不是 1

本 epic 要求「照 af7e59ad4 手改」；在该提交自己的文件里量（本 ground 的 object store 里有
这个对象）：

```
$ git show af7e59ad4:tools/docclass/classify.py | grep -c SELF_ROW_STALE_NOTICE
3
$ git show af7e59ad4:tools/docclass/classify.py | grep -n SELF_ROW_STALE_NOTICE
336:SELF_ROW_STALE_NOTICE = ("classify.py: NOT comparing %s: its write-path renderer is "
1118:        # check path (SELF_ROW_STALE_NOTICE), never silent, and the row is still
1522:                print(SELF_ROW_STALE_NOTICE % MANIFEST_REL, file=sys.stderr)
$ git show af7e59ad4:tools/docclass/classify.py | grep -c "^SELF_ROW_STALE_NOTICE = "
1
```

三处 = 1 定义（336）+ 1 注释里的引用（1118）+ 1 打印点（1522）。
**上游修复本身就是 3**，所以「= 1」不是对上游的复述。

### 1.3 父行真正落地的提交 fea3ca591：同一个量也还是 3

父行自己在 `task/01a13985129772109cf8dfe0b4970a7f` 上重放（提交 `fea3ca591`，已随
`69369ba25` 合入 peerbox master）后，用父行验收的第一条命令量它自己的产物：

```
$ ssh peerbox 'cd /home/user/work/GoPi && git show fea3ca591:tools/docclass/classify.py | grep -c SELF_ROW_STALE_NOTICE'
3
$ ssh peerbox '… | grep -n SELF_ROW_STALE_NOTICE'
336:SELF_ROW_STALE_NOTICE = ("classify.py: NOT comparing %s: its write-path renderer is "
1115:        # is PRINTED in the check path (SELF_ROW_STALE_NOTICE), never silent, and
1520:                print(SELF_ROW_STALE_NOTICE % MANIFEST_REL, file=sys.stderr)
$ ssh peerbox '… | grep -c "^SELF_ROW_STALE_NOTICE = "'
1
```

即：**被执行的那次修复，测出的全量计数是 3，从未出现 1**；而它满足的性质是
«`^SELF_ROW_STALE_NOTICE = ` 恰好 1 行»（Rule 13/14）。

同一棵树上的行为级两端（peerbox master `69369ba25`，实测）：

```
$ ssh peerbox 'cd /home/user/work/GoPi && python3 tools/docclass/classify.py . --check | tail -1'
698 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal: re-derive ONE with `--only <path>`)
$ ssh peerbox 'cd /home/user/work/GoPi && python3 tools/docclass/classify.py . --check | grep -cE "^DOCCLASS STALE devdoc/DOC-INDEX.tsv$"'
0
```

对比本 ground 切出点（修复前）：`699` / `1`（§1.5、§4）。**增量 −1 与「目标行不再被报」
这两条在两端都成立**，是这条验收真正想说的东西。

**根因（一句话）**：验收条款用**全量出现次数**的探针，去断言一条**定义唯一性**的性质。
两种量在「常量必须被打印」的前提下**必然不同**（≥2 vs =1），所以这条条款**不可能被满足**
——除非执行者去破坏它同时要求的那个性质。

### 1.4 意图确实存在，而且上游自己写下了正确的探针（本条为推断，已标明）

上游测试文件里有这条断言（`git show fea3ca591:tools/docclass/classify_test.py`）：

```
1654:        self.assertEqual(src.count("\n" + SELF_ROW_STALE_NOTICE + " = "), 1,
```

**推断**（依据是这行测试，不是我看到的作者意图）：条款里那个「1」的来源是**这条断言** ——
«恰好一个定义»。作者把这条性质**口头**带进了验收，却写成了 `grep -c` 全量计数。
这条推断可被推翻：只要有人指出「1」另有出处即可；但即使推翻，§1.1–1.3 的矛盾仍然独立成立。

### 1.5 同一验收段的第二条同样写给未来的绝对值（附带实测）

第二条「the row count drops by 1 (**699 -> 698**)」把一次 re-derivation 的**增量**写成了
**绝对值**。实测（两个不同的树，各自量一次）：

```
$ cd <本 ground，切出点 7377c4273> && python3 tools/docclass/classify.py . --check | tail -1
699 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal: re-derive ONE with `--only <path>`)
$ cd <本 ground> && python3 tools/docclass/classify.py . --check | grep -cE '^DOCCLASS STALE devdoc/DOC-INDEX\.tsv$'
1
```

（父行自己写的 699 是在它那棵 **b51a763d2** 上量的；上面这个 699 是在本 ground 的切出点
`7377c4273` 上量的。两个读数**碰巧相等**，而「碰巧相等」正是绝对值条款最坏的性质：
它读起来像一条普适事实，实际是两个各自会漂的数。**没有量过父行那棵树，此处也不冒充量过。**）
**增量（699→698）是稳的，绝对值不是**；父行自己的报告里其实已经点明「the criterion is the
**delta** (699 → 698), not zero」，所以这一条只需要改写法（§5.2），不需要改意图。

### 1.6 父行报告里的一处计数归属错误（实测，同族观察，随本裁决归档而不另提行）

父行 task-report 的 `## Evidence (self-filed)` 写：

> `grep -c SELF_ROW_STALE_NOTICE tools/docclass/classify.py` → **3**
> （definition ×1 + the print site + **the test's use** — see `## Not Done`）

实测：`classify.py` 里那 3 处**全部在 `classify.py` 内部**（1 定义 + 1 注释 + 1 打印），
测试文件的引用不在这个计数里 —— 它另计：

```
$ ssh peerbox '… git show fea3ca591:tools/docclass/classify_test.py | grep -c SELF_ROW_STALE_NOTICE'
3
```

这不是代码缺陷，是**报告散文的归属错误**（数字对，构成说错）。它属于本行正在裁决的
同一族（验收/证据里的计数措辞），故**记录在此、不另提行**（约束 3：本行不得同族递归）。

---

## 2. 三条被排除的执行路径，与它们为什么都坏

一个执行者面对「`grep -c` = 1（skip is present）」只有三条路，**三条都坏**：

| # | 做法 | 为什么坏 |
|---|---|---|
| a | **删掉打印点**，让常量只出现一次 | 直接摧毁同一段验收的**另一条**要求（Rule 15：step-aside 必须被打印，never silent），也摧毁被测代码要交付的那个行为本身：`--check` 会回到沉默地把清单自己的行报成 STALE |
| b | **把 `print(SELF_ROW_STALE_NOTICE % …)` 改成裸字面量**，让名字只出现一次 | 计数变成了 1，但代价是 Rule 13 的教科书缺陷（「a constant defined but bypassed by bare literals is a defect」）：定义与使用脱钩，改一次措辞要改两处，而第二处没有任何东西能发现 |
| c | **报「验收未通过」并停下** | 让一条**不可能被满足**的条款把一个正确的修复挡在门外；条款是坏的，被它挡住的工作不是 |

排除法的结论就是裁决本身：**要修的是条款**。条款的作者意图（定义唯一 + 确实被打印 + 目标行
不再被报 STALE）是**对的**，坏的是它选的探针。

---

## 3. 这条缺陷的形状（给同族任务认领）

**症状**：验收/证据里出现 `grep -c <符号>` = 一个**小常数**。
**判据**：当同一个 epic 还要求该符号「被打印 / 被引用 / 被调用」时，这个等式与它**互斥**。
全量计数 = 1 只在「定义即唯一引用」时成立，那正是 Rule 13 的缺陷。
**可复算的证伪**：在**上游参照提交自己的文件**里量同一个量（本行即 §1.2）。参照提交是被
要求重放的对象，它的取值就是条款唯一应当对齐的取值。

**一句记住的**：一条验收条款如果**不可能被满足**，它不会保护质量，只会生产三种伪证之一
（破坏行为 / 破坏常量引用 / 宣布失败）。

---

## 4. 边界声明（本 ground 做了什么、没做什么）

- **代码修复不在本 ground，本行不验证、不重做它。** 实测：本 ground（切出点 `7377c4273`）
  的树里根本没有这个常量，而且清单自己的行仍被报 STALE：
  ```
  $ cd /home/user/workspace/gopi-01a139af268770dfb55cbd8c0e55d471
  $ grep -c SELF_ROW_STALE_NOTICE tools/docclass/classify.py; echo "exit=$?"
  0
  exit=1
  $ python3 tools/docclass/classify.py . --check | grep -cE '^DOCCLASS STALE devdoc/DOC-INDEX\.tsv$'
  1
  ```
  修复本身在 **peerbox**：提交 `fea3ca591`（父行的重放），已随合并 `69369ba25` 进 peerbox master；
  在 peerbox master 上 `grep -c` = 3、`^SELF_ROW_STALE_NOTICE = ` = 1。
  HP 侧 master（`c3a3f4eae`）没有这个修复（同一 grep = 0），本行**没有**把它搬过来（Rule 28：
  系统层归 peerbox；且它不是本行的交付）。
- **没有改任何代码。** 本行的 diff 只有本文件与它自己的清单行（Rule 36 同 commit 分类）。
- **没有新提同族行**（约束 3）：§1.6 的计数归属错误、§1.5 的绝对值条款都**归档在本文件里**，
  作为本裁决的一部分，而不是派生出新行。
- **本文档不断言「`grep -c` 一律坏」。** 它是一个**正确的**探针，当它量的是「出现次数」
  这个它真正测得到的量时（例如 §5.2 的 STALE 行数）。缺陷只在「用它量定义唯一性」。

---

## 5. 正确的行为级判据措辞（同族任务直接复用）

以下三段是**可粘贴**的验收措辞。它们是行为判据（测得到、不会漂），不是数字口诀。

### 5.1 该常量：定义唯一 + 确实被打印（替代 `grep -c … = 1`）

```
- 该常量在 tools/docclass/classify.py 中**恰有一个定义**，且**确实被打印**：
  `grep -c "^<CONST> = " tools/docclass/classify.py` = 1        # 定义唯一
  `grep -n "print(<CONST>" tools/docclass/classify.py` 至少 1 行  # 确实被打印
  （不要用 `grep -c <CONST>` 全量计数当判据：在正确的实现里它 ≥ 2，且随注释增减而变。）
```

### 5.2 被修复的行为：目标行不再被报，且计数是**增量**

```
- `python3 tools/docclass/classify.py . --check` 不再输出
  `^DOCCLASS STALE <目标行>$`（`grep -c` = 0，**不改写绝对值**），
  且 step-aside 的通知文本出现在同一次运行的输出里（附原文）；
  summary 的 STALE 计数相比**修复前同一条命令**减少 1。
  两个读数写在同一次报告里（before/after 各一条命令原文），绝对值不写进判据。
```

### 5.3 探针与性质的对应关系（为什么这样写）

| 想断言的性质 | 正确的探针 | 会骗人的写法 |
|---|---|---|
| 常量只有一个定义（Rule 13/14） | `grep -c "^CONST = "` | `grep -c CONST`（全量，≥2） |
| 常量确实被使用/打印 | `grep -n "print(CONST"` 有命中 | 只用全量计数反推 |
| 某行走廊消失 | 目标 pattern `grep -c` = 0 | 「总数降到 N」（N 随语料漂） |
| 一处修复的规模 | before/after 同一条命令的**差** | 写死 `699 -> 698` 这类绝对值 |

一句话：**探针必须只测它真正测得到的那个量**；用一个全量计数去断言一条唯一性性质，
得到的不是证据，是一个不可能被满足的门。
