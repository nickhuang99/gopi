<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 更正：行 `01a133ee-2d09` 的「报错」半是设计好的拒绝路径，「挂起」半仍为实测

**更正任务**：`01a133ee-bdf4-7b0a-847b-75ff991c4477`（Rule 41 第 3 要件：实测与推断分开写）
**被更正的原始行**：`01a133ee-2d09-72f8-9657-e0956d7a0d5b`，标题
「bug: gopi task show 对已存在的 row 报错或挂起（list 能列出，show 读不出）」
**本 run 分支**：`task/01a133eebdf47b0a847b75ff991c4477`
**本 run 会话目录（全部原始证据）**：`/home/user/.gopi/sessions/01a133ee-bdf4-7b0a-847b-75ff991c4477/artifacts/`
**测量主机**：peerbox；**binary**：`/usr/local/bin/gopi` → `/home/user/work/GoPi/bin/gopi`，
`revision: 29a6fd1b397b1b6097bd4133e52d009dadeeefe8 built: 2026-10-13T08:38:47Z`（见 `artifacts/env-snapshot.txt`）

原始行的标题断言是**两半**，本更正把它们**分开处置**：一半撤回，一半保留。

---

## 一、撤回的那半：「`gopi task show` 对一个已存在的 row 报错 exit=1」

**结论：撤回。** 那不是「show 读不出已存在的 row」，而是**调用者把 uuid 写成了非规范拼写**
（去掉连字符），工具**自带了诊断并把规范拼写念了出来** —— 这是设计好的拒绝路径。

### 1.1 实测（本 run 亲自复跑，2026-10-13）

被更正的那条命令，其操作数是 **row `01a133ee-2d09-72f8-9657-e0956d7a0d5b` 去掉连字符**
的形状。本 run 用**同一个 row** 的两种拼写并排复跑：

**先用原 brief 里那条命令的原文操作数（它自己的 row，去连字符）原样重跑：**

```
$ timeout 30 gopi task show 01a133a585dd76d5a667927577aa56a3   # 原 brief 第一条命令的原文操作数
rc=1  stdout=0 bytes  stderr=220 bytes
gopi task show error: task: no task with task_id="01a133a585dd76d5a667927577aa56a3" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean "01a133a5-85dd-76d5-a667-927577aa56a3"?

$ timeout 30 gopi task show 01a133a5-85dd-76d5-a667-927577aa56a3   # 同一 row 的规范拼写
rc=0  stdout=12550 bytes
task_id:        01a133a5-85dd-76d5-a667-927577aa56a3
status:         completed
…
```

- 原始输出：`artifacts/A0-original-operand.{out,err}`（0 / 220 字节）、
  `artifacts/A0b-original-canonical.{out,err}`（12550 / 0 字节）

**再用被更正行自己的两种拼写复跑：**

```
$ timeout 30 gopi task show 01a133ee2d0972f89657e0956d7a0d5b      # 去连字符（非规范拼写）
exit=1  stdout=0 bytes  stderr=220 bytes
gopi task show error: task: no task with task_id="01a133ee2d0972f89657e0956d7a0d5b" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean "01a133ee-2d09-72f8-9657-e0956d7a0d5b"?

$ timeout 30 gopi task show 01a133ee-2d09-72f8-9657-e0956d7a0d5b      # 规范拼写（同一个 row）
exit=0  stdout=3550 bytes  stderr=0 bytes
task_id:        01a133ee-2d09-72f8-9657-e0956d7a0d5b
status:         in_progress
summary:        bug: gopi task show 对已存在的 row 报错或挂起（list 能列出，show 读不出）
…
```

- 原始输出：`artifacts/A1-noncanonical.out`（空）与 `artifacts/A1-noncanonical.err`（220 字节，diagnosis）
- 原始输出：`artifacts/A2-canonical.out`（3550 字节）与 `artifacts/A2-canonical.err`（空）

**这就是对「show 读不出」的反证**：同一个 row，规范拼写读得通、exit=0；
唯一的差别是**该 row 存在的两种拼写**（`deliverable.branch: task/01a133ee2d0972f89657e0956d7a0d5b`
用的正是去连字符拼写，见 `gopi task show` 的输出本身）—— 账本只按规范拼写索引。

### 1.2 「这是设计好的路径」的代码与测试证据（不是推断）

**产生这条诊断的代码位置**（`gopi task show` 这条腿）：

- `internal/tools/tasks.go:1532` —— 子句的**唯一**渲染点：
  `internal/tools/tasks.go:1532: return fmt.Sprintf(" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean %q?", canonical)`
- 它在 `taskIDNearMissSuffix(ctx, db, taskID)` 里（`internal/tools/tasks.go:1509` 起），
  函数上方 1493–1508 行的注释**逐字记载**了这条路径为什么存在、以及它只在一个
  「建议真的可达」的条件下才印（另一行 bug `01a130e4` 的修复：建议必须是对本账本可解析的拼写；
  同一段注释在 `internal/tools/tasks.go:1505` 写明「The rule is internal/sessionid's,
  never a second uuid comparison here」—— 拼写规则只有一处定义，tools 只是渲染它）。
- 拼写规则本身**不在 tools 里**（Rule 14：单一来源），来自
  `internal/sessionid/sessionid.go:190` 的 `CanonicalSpellingOf`，被
  `internal/tools/tasks.go:1510` 调用；session 那条腿的对应拒绝在
  `internal/sessionid/sessionid.go:165` `Refuse`（`Refuse` 里 `sessionid.go:166` 也调
  `CanonicalSpellingOf`），措辞不同、判据同一。

**回归测试**（本 run 在同一 worktree 上跑通，非引用、非推断 —— `-v` 逐条 PASS）：

```
$ CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/ && echo "BUILD OK"
BUILD OK

$ go test ./internal/tools/ -run 'TestTaskShowNamesTheCanonicalSpellingForANearMiss|TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake|TestTaskShowStillNamesTheCanonicalSpellingForANearMiss' -count=1 -v
=== RUN   TestTaskShowStillNamesTheCanonicalSpellingForANearMiss
--- PASS: TestTaskShowStillNamesTheCanonicalSpellingForANearMiss (0.01s)
=== RUN   TestTaskShowNamesTheCanonicalSpellingForANearMiss
--- PASS: TestTaskShowNamesTheCanonicalSpellingForANearMiss (0.02s)
=== RUN   TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake
--- PASS: TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake (0.02s)
PASS
ok  	github.com/nick/gopi/internal/tools	5.658s

$ go test ./internal/sessionid/ -count=1
ok  	github.com/nick/gopi/internal/sessionid	0.003s
```

（逐字存档：`artifacts/verification.txt`）

- `internal/tools/tasks_test.go:728` `TestTaskShowNamesTheCanonicalSpellingForANearMiss`
  —— 近失拼写**必须**被拒，且**必须念出**规范拼写，并断言**它念出的那个 id 真的可解析**：
  `tasks_test.go:741: t.Fatalf("TaskShow(%q) succeeded — a non-canonical spelling must not resolve", nearMiss)`
  以及 `shown, err := TaskShow(canonical); if err != nil { t.Fatalf(… [the id the refusal names] …) }`。
  这就是「拒绝 + 念出拼写」是**规范**而不是缺陷的机器判据。
- `internal/tools/tasks_test.go:781` `TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake`
  —— 另一半：本账本不持有的规范拼写**不得**给建议、也不得印拼写子句
  （`tasks_test.go:792`：`if strings.Contains(err.Error(), "did you mean") || strings.Contains(err.Error(), "non-canonical") { t.Errorf(… the spelling clause must not print …) }`）。
- `internal/tools/task_show_prefix_refusal_test.go:133` `TestTaskShowStillNamesTheCanonicalSpellingForANearMiss`
  —— 该行为在**前缀拒绝**修复之后仍然保持：
  `task_show_prefix_refusal_test.go:147–152` 断言近失拼写必须被拒、错误里必须含 `did you mean`、
  且必须念出规范拼写。
- 上游 devdoc：`devdoc/task-show-spelling-hint-is-a-repair.md`（该子句的原始任务报告）

**为什么这不构成「缺陷」**：工具拒绝的是**一个不是本账本索引键的拼写**，并且在同一行里
给出了可执行的修复（规范拼写）。这条路径有名字、有注释、有两条回归测试、有一份 devdoc ——
它是产品行为，不是故障。

---

## 二、保留的那半：`task show 01a133e7-…` 挂在 timeout 上

**结论：不撤回（原始观测），但本 run 复测未见重现；按 brief 只记录现象、根因未查、
本文件不作任何成因推测。**

### 2.1 原始观测（引用，不是本 run 的实测 —— 来源标清）

被更正行的 brief 记录了这半的实测：

```
$ timeout 40 gopi task show 01a133e7-0a5e-7709-8fc2-ce73b4782198   # Rule 41 残留行
exit=124 (timeout, 0 bytes)
$ timeout 15 gopi task list | grep 01a133e7
#01a133e7-0a5e-7709-8fc2-ce73b4782198  [pending]  bug: 常驻 gopi merge --every 用已删除的陈旧镜像做合并决定…
```

**同一报告 run 自己留下的计时工件**（`/home/user/.gopi/sessions/01a133ee-2d09-72f8-9657-e0956d7a0d5b/artifacts/timings.txt`，
本 run 原样读取，未改动）：

```
01a133ee-2d09-72f8-9657-e0956d7a0d5b rc=0 53ms
01a13462-3126-7263-b10d-f123b9898555 rc=0 632ms
01a133e7-0a5e-7709-8fc2-ce73b4782198 rc=0 57ms
01a130e4-0ea5-7f1e-9139-61f2036acb3e rc=0 40ms
```

即**同一次上报 run** 里的另一次测量该 row 是 `rc=0 57ms`（这是该 run 的工件所载事实，
本 run 只转述、不解释二者为何不同）。

### 2.2 本 run 的复测（按 brief 原样重跑一次）

```
$ timeout 40 gopi task show 01a133e7-0a5e-7709-8fc2-ce73b4782198
attempt1 rc=0 elapsed_ms=67   stdout_bytes=12149  stderr_bytes=0
attempt2 rc=0 elapsed_ms=53   stdout_bytes=12149  stderr_bytes=0
attempt3 rc=0 elapsed_ms=54   stdout_bytes=12149  stderr_bytes=0
```

- 原始输出：`artifacts/C2-hang.{out,err}`、`artifacts/C2-attempt{1,2,3}.{out,err}`、
  `artifacts/C2-retest.txt`、`artifacts/summary-c2.txt`
- 该 row 当前**不是** pending：`C1-list.out` 与 `C2-attempt1.out` 都显示
  `status: completed`（上报时它还是 `[pending]`）。**这是读取到的两个状态事实，
  不是对 hang 的解释。**
- `gopi task list` 能列出该行（`artifacts/C1-list.out` 中 1 行命中，行文截断存档于
  `artifacts/C1-row-line.txt`；`artifacts/C1-list.rerun.out` 再次确认 1 行命中）：

```
#01a133e7-0a5e-7709-8fc2-ce73b4782198  [completed]  bug: 常驻 gopi merge --every 用已删除的陈旧镜像做合并决定（edge 不在运行镜像里）  (model deepseek-flash, source agent, crea
```

### 2.3 明确不写的东西

**本文件不给出 `exit=124` 的成因。** 可复述的相关观测（都不是成因，也不互相推证）：

- 该行为来源 run 留下过一组**锁竞争的测量工件**（同一 session 目录下 `lock-repro.txt`
  显示「writer 持有 BEGIN IMMEDIATE 时 `task show` 耗时 18263ms」、`lock-ab.txt` 显示
  三次交错复现 17226/17126/17130ms，均 `rc=0`）。这些是该 run 记录的**耗时**观测。
- `gopi task list` 在这次测量里同时存在（`1,097,182` 字节输出，见 `C1-list.out` 大小）。

「这两件事是不是同一件事」**不在本任务的范围内**（brief 明文：hang 那半只记录、不查根因），
本文件不作判断，也不为凑结论编造因果。

---

## 三、可复核的最小复跑清单

```bash
S=/home/user/.gopi/sessions/01a133ee-bdf4-7b0a-847b-75ff991c4477/artifacts
# (1) 非规范拼写 → 拒绝 + 念出规范拼写；exit=1
gopi task show 01a133ee2d0972f89657e0956d7a0d5b ; echo "rc=$?"
# (2) 同一 row 的规范拼写 → 读通；exit=0
gopi task show 01a133ee-2d09-72f8-9657-e0956d7a0d5b ; echo "rc=$?"
# (3) hang 那半的原始命令（上报时 exit=124）
timeout 40 gopi task show 01a133e7-0a5e-7709-8fc2-ce73b4782198 ; echo "rc=$?"
# 逐字输出见 $S/A1-noncanonical.err, $S/A2-canonical.out, $S/C2-retest.txt
```

## Not Done

- **hang 那半的根因未查**（brief 与预算共同框定：只记录现象）。本文件不含成因推测。
- 未做全树测试（Rule 27：merge/verify 路径是 SMOKE、分支范围）。
- 未修改任何生产代码：本任务交付的是更正文档，不是修复。

## Evidence (self-filed)

- (1)(2)(3) 三条命令的 **exit/字节数/耗时** 是本 run 亲自观察到的
  （`artifacts/summary-c1.txt`、`artifacts/summary-c2.txt`、`artifacts/C2-retest.txt`、
  `artifacts/A0-original-operand.*`、`artifacts/A0b-original-canonical.*`），不是转述。
- 「设计好的路径」的判据是**代码行**（`internal/tools/tasks.go:1532` 的 `fmt.Sprintf` 渲染点）
  加**三条本 run 跑通的测试**（`-v` 逐条 `--- PASS`，见 `artifacts/verification.txt`；
  `go test ./internal/tools/ …` → `ok 5.658s`；`go test ./internal/sessionid/` → `ok 0.003s`；
  `CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/` → `BUILD OK`），不是注释推断。
- doccheck：`gopi tool doccheck` → `the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row`，rc=0；`python3 tools/docclass/classify.py . --check` →
  `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`，rc=0。
- 第二节的原始 `exit=124` 观测**明确标注为引用**（来源：行 `01a133ee-2d09` 的 brief 与
  本 run 会话 `attachments/` 内的 `correction-row.txt`），本 run 未复现、也不声称复现。
