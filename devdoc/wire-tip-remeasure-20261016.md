<!-- doc-class: historical -->
# 在 wire tip 01c167412 上重测三条缺陷（A/B/C）— 2026-10-16

Row: `01a1426f-5541-7a1a-bcc8-92808000ef01` · host `hosta`（client — Rule 28：merge/close-out 机器属 peerbox）·
branch `task/01a1426f55417a1abcc892808000ef01` · tip `01c1674121dc089ada8e1d0292a5f2a1cb90a3e5`

## 为什么重测

briefing 自己声明 `base=10217dc0`。按 freshness 条款（dispatch 缝已经为此盖章 —— 见下面 A 的端到端证据）：

```
$ git merge-base --is-ancestor 10217dc02 refs/heads/master ; echo exit=$?
exit=0
$ git rev-list --count 10217dc02..refs/heads/master
363
```

`10217dc0` 距 master **363 个提交**，所以 three 条结论一律**先在本地重跑**，只报仍然复现的。

---

## A — 「派发前提从未被盖章」

**结论：已修复（在 HEAD 上实测，非引用旧行）。** 两个半句分开量。

### A1 前提盖章机制**存在**，且在本 run 自己的 briefing 上端到端生效

本 run 收到的 prompt（session jsonl 第 1 条 user 消息）里带着 dispatch 缝盖的章，逐字：

```
[Briefing freshness — the BASE REVISION this briefing names was RE-MEASURED at dispatch]
This briefing names 1 revision(s) as the base its own measurements were taken at, and each was re-measured JUST NOW, against THIS RUN's own repo, before the briefing was handed to you — every one of them is BEHIND master.
- the base this briefing names, `10217dc0`, is 363 commit(s) BEHIND refs/heads/master (`git merge-base --is-ancestor 10217dc0 refs/heads/master` → exit=0; `git rev-list --count 10217dc0..refs/heads/master` → 363).
  RE-MEASURE BEFORE REPAIRING: the surface this briefing calls red was measured at the older revision — run THAT surface again HERE, quote its output, and repair it only if it still reproduces. If it is already green, report that instead of repairing it.
  This clause is NOT evidence that the work is done.
  briefing line: "$ git merge-base --is-ancestor 10217dc02 bbf8d97"
```

产生它的代码在 tip 上，且挂在唯一的 spawn 缝：

```
$ grep -rn "TaskBriefingPremiseBaseRefresh" --include=*.go cmd/agent/cli_tasks.go
cmd/agent/cli_tasks.go:2850:		baseClause, baseRecord := tools.TaskBriefingPremiseBaseRefresh(ground.Dir, briefing)
$ git log --oneline -1 b0890a420
b0890a420 fix(briefing): re-measure a briefing's declared BASE revision at dispatch, and stamp RE-MEASURE BEFORE REPAIRING
```

同缝上还有两条姊妹条款（`TaskBriefingPremiseRefresh` @ :2822、`TaskBriefingCountRefresh` @ :2872）——
「没有机制会给前提盖章」这句在 HEAD 上**不成立**。

### A2 `STALE PERMIT` 这条 arm 在本 tip 上**确实已经不红**；但 `doccheck` 仍 exit 1，红的**是另一条 arm**

```
$ $GOPI_HOME/bin/gopi tool doccheck ; echo doccheck_exit=$?
doccheck_exit=1
STALE_PERMIT_lines=0
NOT_PERMITTED_lines=0
  doccheck: stored-cell drift: 5 of 2568 rendered row(s) whose stored text is not what the classifier renders today
doccheck: this run exits 1 on stored-cell drift (the manifest's stored text is not what tools/docclass/classify.py renders today) (see the section above), independently of the 0 gap(s)
verdict: drift
```

原始输出全文：本 run evidence `doccheck-tip-full.txt`；命令+输出的同段记录：`remesure-A-tip.txt`。

即 briefing 原句「`gopi tool doccheck` 在 tip 上因 STALE PERMIT 变红」**已被推翻**：`STALE PERMIT` 计数 0，
exit 1 来自 `stored-cell drift`（5 行，且 `0 gap(s)`）。**「已修复」的准确说法是：STALE PERMIT arm 不再红 +
盖章机制存在；而 doccheck 的 exit 1 本身仍在，只是换了 arm。**

### A3 本 run 实测到、且与 A 相关的两处需要更正的小事实

- briefing 说 `refs/heads/master = refs/remotes/gitPushcheck/master = 01c1674`。实测**不相等**：

```
$ git rev-parse HEAD refs/heads/master refs/remotes/gitPushcheck/master
01c1674121dc089ada8e1d0292a5f2a1cb90a3e5
01c1674121dc089ada8e1d0292a5f2a1cb90a3e5
cdc4970903d76063e0b2f825f4932d731a94491e
```

  peer mirror 落后于本地 master。本 run 的结论一律以 `refs/heads/master` 为准（已注明）。
- 盖章条款里的 `briefing line: "$ git merge-base --is-ancestor 10217dc02 bbf8d97"` —— 扫描器把 briefing 里的
  **一条命令**当成了「base 声明」来读（它含 `10217dc02` 这个 token）。这是盖章臂的**误命中**（false positive）：
  该行不是 base 声明。方向是安全的一侧（多盖一次章、只要求重测），但值得记下。

---

## B — landing bbf8d97 合出的「新的红」，以及当时的 run set 只问一个包

**结论：run set 半句已修复；「新的红」在 tip 上换成了**另一批**红，且其中三条无行承载（已按 Rule 41 提行）。**

### B1 run set 半句：已修复（HEAD 实测）

当时的缺陷是 leg 只问 `./internal/docclass`，看不见自己组合出的 `internal/standards` 半态。
机制现在在 tip 上：

```
$ grep -n "ManifestGatePackageRel" internal/tools/mergeset_docclass.go internal/docclass/docclass.go
internal/tools/mergeset_docclass.go:349:	manifestRef := "." + mergesetGatePackageSep + docclass.ManifestGatePackageRel
internal/docclass/docclass.go:63:	ManifestGatePackageRel = "internal/standards"

$ git log --oneline -1 10217dc02 ; git merge-base --is-ancestor 10217dc02 HEAD ; echo exit=$?
10217dc02 fix(mergeset): the devdoc-anchor leg asks for the manifest's own gate package too
exit=0
```

即 briefing 里那条修复（`10217dc02`）**就在 tip 的历史上**，run set 现在带上 manifest 自己的 gate 包。

### B2 leg 那个包：在 tip 上**绿**

```
$ go test -count=1 ./internal/docclass
ok  	github.com/nick/gopi/internal/docclass	32.496s
```

并且它的冻结表自洽：

```
$ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: reported figures (live scan, never stored in the header): ambiguous=41 numbered=0
anchor-number-census: header figures re-derived and equal (frozen=0 pre-existing=0 first-frozen=0)
anchor-number-census: table matches the tree — the freeze is exact
verdict: clean
census_exit=0
```

（对照 bbf8d97 当时的输出 —— 本 run evidence 引用了原件的 `leg-bbf8d97.txt`：
`--- FAIL: TestAnchorNumberGateOnTheRealTree … 1 permit(s) … no longer cover a drifting row`、
`TestAnchorNumberCensusShippedFaceIsExactOnTheRealTree … exit 1`。**那两条今天不红。**）

### B3 union 那一臂：`internal/standards` 在 tip 上**活红**，7 条 FAIL

```
$ go test -count=1 ./internal/standards 2>&1 | grep -E '^--- FAIL|^ *--- FAIL|^(ok|FAIL)'
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (0.76s)
--- FAIL: TestDocClassBannerMatchesManifest (0.11s)
--- FAIL: TestStandardGates (14.31s)
    --- FAIL: TestStandardGates/TestGate6EnvReaderRedProof (1.82s)
    --- FAIL: TestStandardGates/TestGate9GitEnvRedProof (0.48s)
--- FAIL: TestStandardGateDataReport (12.21s)
--- FAIL: TestGate6EnvReaderFindsTheRealTree (0.31s)
--- FAIL: TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly (0.56s)
FAIL
FAIL	github.com/nick/gopi/internal/standards	74.670s
FAIL
```

逐条查账本承载情况（本地 + `ssh peerbox` 两台合计），**三条无承载，已按 Rule 41 提 bug 行**：

| FAIL | 承载 | 行 |
|---|---|---|
| `TestDocClassBannerMatchesManifest`（`devdoc/removal-audit-shell-census.md` 半态） | **无** | `01a1432c-eab6-7218-998b-0c4bb59cac86` |
| `TestCommandsDocGeneratedBlockMatchesSource`（`cli_tasks.go:525` vs `:531`） | **无（本次漂移）** | `01a1432c-ead9-70f9-8a6d-ecb7a286abfc` |
| `TestGate6EnvReaderFindsTheRealTree`（Rule 21 `NEW=1`） | **无** | `01a1432c-eaf8-790f-a5b6-42a120ecd3d9` |
| `TestGate9GitEnv…` ×2（stale baseline `git_child_locale_test.go:243`） | 有 | `#01a14172` / `#01a141ff`（pending，本次不另提） |

**与 bbf8d97 的关系要说清（实测，不推断）**：bbf8d97 组合出的那条 fresh red 是
`devdoc/ledger-readonly-faces.md` 的 banner 半态（原输出见 `leg-bbf8d97.txt`）。**那条路径今天已经修好**：
文件第一行无 banner、DOC-INDEX 分类 `normative`，两者一致 —— 承载它的是 peerbox 的 `#01a136a7`（completed）
与 `#01a136e7`（completed）。今天红的是**另一条路径** `devdoc/removal-audit-shell-census.md`，
由 landing `8ddf9cf28` 引入，账本 0 行。

---

## C — `GOPI_TASK_REPORT_PATH` 指向去横线 session 目录

**结论：机制面已修复（拒绝拼写）；「谁产生的」已测出 —— 是那个 run 的 model 自己造的，不是 harness；
「谁回收的」实测：至今无人回收，且按设计不会被自动收。briefing 对该现象的描述需要更正。**

### C1 前提更正：`GOPI_TASK_REPORT_PATH` **从未**指向去横线路径

briefing 说「`GOPI_TASK_REPORT_PATH` 曾在运行中途指向一个去横线的 session 目录」。该 run 的 raw log 实测：

```
$ grep -c 'GOPI_TASK_REPORT_PATH=/home/user/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4' <raw log>
76
$ grep -c 'GOPI_TASK_REPORT_PATH=/home/user/.gopi/sessions/01a136ee881' <raw log>
0
```

**76 次读数全部是带横线的；去横线拼写作为 env 值一次也没出现。** 去横线只出现在两处：
(a) 那个 run 自己写下的 report 正文（它自己把现象叙述成了 env 值），(b) 它自己的探针命令。
—— 按 Rule 41 第 3 条：这是**实测**；「它是那个 run 的误归因」是**推断**（证据方向很强，但仍是推断）。

### C2 谁产生的：那个 run 的 model 自己 `mkdir -p`

从它自己的 raw log 复制出的命令：

```
DL=/home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4; HY=/home/user/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4; echo "inode HY: $(stat -c %i $HY)"; mkdir -p $DL; echo "mkdir exit=$?"; echo "inode DL: $(stat -c %i $DL 2>&1)"; ... touch $DL/.alias-probe; ls $HY/.alias-probe 2>&1
```

输出：

```
exit=2
inode HY: 4983109
mkdir exit=0
inode DL: 3580829
=== do they share content? ===
--- HY ---
8c73f2f1-2d71-40bc-900a-ffaca50dd2f3.jsonl
...
ls: cannot access '/home/user/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4/.alias-probe': No such file or directory
```

10:01 它又用一条 `cp` 把 report 与 artifacts 拷了进去：

```
D=…/01a136ee-8817-…; DL=…/01a136ee88177…; mkdir -p $DL && cp -v $D/task-report.md $DL/task-report.md 2>&1; cp -v $D/artifacts/*.txt $DL/artifacts/ 2>&1 | tail -3; ...
```

**产生者 = 那个 run 的 model 的一次 alias 探针 + 一次拷贝**，不是 harness 的拼写。

### C3 它今天还在，且**没有任何回收**（实测 13:26，创建于 10:00–10:01）

```
$ stat -c '%F %N' /home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4
directory '/home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4'
$ ls -la /home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4
total 152
drwxrwxr-x    3 nick nick   4096 Oct 16 10:01 .
-rw-rw-r--    1 nick nick      0 Oct 16 10:00 .alias-probe
drwxrwxr-x    2 nick nick   4096 Oct 16 10:01 artifacts
-rw-r--r--    1 nick nick   8292 Oct 16 10:01 task-report.md
$ ls -li <both>/task-report.md
4984752 -rw-r--r-- 1 nick nick 10530 …  /…/01a136ee-8817-7a61-8f3d-942bdbfa31d4/task-report.md
3580832 -rw-r--r-- 1 nick nick  8292 …  /…/01a136ee88177a618f3d942bdbfa31d4/task-report.md
```

不同 inode、不同大小（后者是 REC441 那版的逐字节副本，md5 `793703846362dea5f6d4e880862e5ba2`）。

**「无人回收」是设计，不是漏收** —— `internal/sessionlayout/families.go` 的 `FamilyRootStrayDir` 原文：

> "What it does NOT bound: any name the shape table does not carry — a NEW root-level directory stays
> unclassified and is REPORTED, never swept."

去横线的 32-hex 名既不是 `t-`/`process-`（`IsRootTaskContainerName`），也不在 stray shape 表里 → 永久留存。
因此修法**不是**放宽 sweep（Rule 35：不要为了消除某物而新增机械），而是让产生端不制造它，或由 operator
确认后手工删除。**本 run 不删**（不是本 run 造的目录，删别人的 session 目录不是本 run 的决定）——
已按 Rule 41 提行 `01a1432c-eb15-7bd8-bc28-eef284345123` 交 operator 判断去留。

### C4 机制面（拒绝拼写）在 tip 上已修复 —— 但本 run **未做**写入端复现

代码面（引用，非实测）：`internal/tools/task_session.go:157` 与 `internal/signalrec/signalrec.go:733` 都调用
`sessionid.IsAdmissible`；`internal/sessionid/sessionid.go:138` 的 `isNonCanonicalUUIDSpelling` 正是
「32 hex 但 dash 分组不是 8/13/18/23」这一形状，拒绝语点名修复值（`Refuse`，:165）。

**未测量**：本 run 没有用去横线 task id 实际驱动一次写入端（`gopi task show <dashless>` /
`TaskReportPath(<dashless>)`）来观察拒绝。理由：要产生一个有意义的写入端读数需要造一个 ledger 行或伪造
session 目录，成本高于本 run 的预算且会留下残留。**这一半明确记为未测量。**

---

## 汇总

| 条 | 结论 | 依据 |
|---|---|---|
| A「没有机制给前提盖章」 | **已修复** | 本 run 自己 prompt 上的章 + `cli_tasks.go:2850` + `b0890a420` |
| A 半句「doccheck 因 STALE PERMIT 红」 | **已修复**（STALE PERMIT 计数 0）；exit 1 仍在，改由 `stored-cell drift`（5 行） | `doccheck-tip-full.txt` |
| B「run set 只问 ./internal/docclass」 | **已修复** | `ManifestGatePackageRel = "internal/standards"` + `10217dc02` 是 HEAD 祖先 |
| B「bbf8d97 组合出的红无行承载」 | **原路径已修**（`ledger-readonly-faces.md`，`#01a136a7`/`#01a136e7`）；**同一族在别处仍活** | 见 B3 表；三条已提行 |
| C「report path 指向去横线目录」 | **前提不成立**（76/76 读数带横线）；**产生者** = 那个 run 的 model `mkdir -p`；**回收者** = 无，且按设计不自动收 | C1–C3 |
| C 写入端拒绝臂 | **未测量** | C4 |

## 本次未做（诚实）

- 未删去横线目录：不是本 run 造的，删除他人的 session 目录超出本 run 权限（已提行）。
- 未修任何 `internal/standards` 红：Rule 28 —— 系统层归 peerbox，本 run（hosta，client）只测量与提行。
- 未测量 C4 的写入端拒绝臂（理由见 C4）。
- 未核对 `refs/remotes/gitPushcheck/master`（`cdc497090`）与本地 master 的差集内容：本 run 一律以
  `refs/heads/master` 为准，peer mirror 的落后量本身未逐条展开。
