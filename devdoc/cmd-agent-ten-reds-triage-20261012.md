<!-- doc-class: historical -->
# cmd/agent 10 条红的逐条分诊（hosta，master tip）

**分诊对象**：`make test-budget PKGS='./internal/docclass ./cmd/agent'` 在未修改的
master 树上报 `cmd/agent` 10 条失败（外加 `internal/docclass` 1 条）—— 共 11 条具名
FAIL、2 个包 fail。

**测量环境**：主机 `hosta`，run worktree
`/home/user/workspace/gopi-01a12e06d42a7a639128d7478d73aca8`（branch
`task/01a12e06d42a7a639128d7478d73aca8`）。本机 `sqlite3` CLI **未安装**
（`command -v sqlite3` → rc=1）。

**测量 commit（两个，Rule 38 要求点名）**

| 树 | commit | 说明 |
|---|---|---|
| 基线树 | `90fb17de1` | 工单点名的 master tip；`git worktree add --detach /dev/shm/gopi-01a12e06-base 90fb17de1` |
| master tip 复核树 | `d7fb65344` | 本机 `master` ref 的 tip = `90fb17de1` + 51 提交（`git rev-list --count 90fb17de1..master` = 51；`--is-ancestor` 成立） |

两条证据链各自独立、都实跑：

```bash
# ① 基线（工单点名的 tip 90fb17de1）
cd /dev/shm/gopi-01a12e06-base
git rev-parse HEAD            # 90fb17de1c508bdb32a1f11ca2f793347b9f3103
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
make test-budget PKGS='./internal/docclass ./cmd/agent'
→ stamp: 2026-10-12T11:15:59+08:00 host=hosta pkg=./internal/docclass ./cmd/agent
         budget=25m elapsed=900s exit=1 outcome=fail
         stream=/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791774959-3966102.json
→ 包结果：docclass fail(0.62s) / cmd/agent fail(892.086s)；具名 FAIL = 11

# ② master tip 复核（d7fb65344）
cd /dev/shm/gopi-01a12e06-master && git rev-parse HEAD   # d7fb65344
go test -count=1 -v -run '<工单的 9 个 cmd/agent 测试名>' ./cmd/agent/
→ 9 条全部 FAIL（见下），EXIT=1
```

原始输出落盘（本 run session 目录）：

- `evidence/master-tip-10-tests.txt` —— ② 的完整 `-v` 输出（9 条 FAIL 及每条的断言原文）
- `evidence/mine-base90fb17de1-run.json` + `evidence/mine-base-failures.txt` —— ① 的
  `-json` 流（11 条具名 FAIL）与提取出的失败集
- `evidence2/` —— 11 条各自的完整输出切片
- `evidence/env-facts.txt` —— 本机事实（`sqlite3` 缺失、HEAD/master/祖先关系）
- `evidence/ledger-all.txt` —— `gopi task list` 全量，作为「账本是否已覆盖」的判据

**计数口径（Rule 38）**：失败集一律取自 `go test -json` 流的
`{"Action":"fail","Test":…}` 记录，用**集合差**比较，不比较数量。

---

## 一、结论表（11 条，无一条含糊）

| # | 失败项 | 分类 | 依据（本 run 实跑） |
|---|---|---|---|
| A1 | `TestNotifyScriptAgreesWithTheRegistryConstants` | **真实缺陷 · 无人拥有** | 基线 + master tip 两树皆红；`notify.sh` 已无 `${SLOT_PREFIX}${i}${SLOT_SUFFIX}` 字面 |
| A2 | `TestEveryHelpConstantFitsTheWrapBudget` | **真实缺陷 · 无人拥有** | 两树皆红；`cli_tasks.go:430` 89 列 > 88 |
| A3 | `TestPublishedHelpPagesStayWithinTheirUsageBudget/task` | **真实缺陷 · 无人拥有**（A2 同源） | 两树皆红；`gopi task -h` 193 行 > 190 |
| B1 | `TestResumeReclaimsTheWorktreeItFinishedIn` | **既有红 · 账本已覆盖，但修复 stranded** | 行 `01a12de0-…8ef`；master **无** `TakesPart`；master tip 仍红 |
| B2 | `TestResumeRescuesUntrackedResidueBeforeRemoval` | 同上 | 同上 |
| B3 | `TestWorkerResumeOwnsTheGround` | 同上 | 同上 |
| B4 | `TestWorkerResumeByWorktreeIsTheSameGround` | 同上 | 同上 |
| B5 | `TestWorkerResumeByTaskIDIsTheDefault` | 同上 | 同上 |
| B6 | `TestRawPromptRunReleasesItsWorktree` | 同上 **＋ 环境性 skip（仅断言③）** | master tip 仍红（非环境）；另有 sqlite3 缺失的可审计 skip |
| D1 | `TestAnchorNumberGateOnTheRealTree`（`internal/docclass`） | **既有红 · 已修复落地** | 行 `01a12ddd-…4b9`；修复 `9fdde2f78` 在 master；master tip 单跑 **绿** |

**关键判定：11 条中没有一条是「因环境而失败」。** 唯一的环境性成分是 B6 的**断言③**
（读账本那一半）被 `t.Skipf` 跳过——见第三节。

---

## 二、A 类：真实缺陷（无人拥有）

### A1 — `notify.sh` 不再出现 `${SLOT_PREFIX}${i}${SLOT_SUFFIX}`

**实跑原文**（两树一致，本 run 各一次）：

```
# /dev/shm/gopi-01a12e06-base（90fb17de1）与 /dev/shm/gopi-01a12e06-master（d7fb65344）
--- FAIL: TestNotifyScriptAgreesWithTheRegistryConstants (0.00s)
    chat_session_slots_script_test.go:87: notify.sh does not build the slot name from
    ${SLOT_PREFIX}${i}${SLOT_SUFFIX} — the name is spelled somewhere else
```

**根因（文件 + 行 + 谁写的）**：

- 断言位置：`cmd/agent/chat_session_slots_script_test.go:87`（引入于 `3a107a06a`
  "feat(chat): register a chat run in the profile's slot registry…"）。
- 被断言的脚本侧：`scripts/notify/notify.sh:176` 现在是

  ```sh
  _da_slot="${PROFILE}/${SLOT_PREFIX}${_da_i}${SLOT_SUFFIX}"
  ```

  而脚本里**再无** `${i}` 这个拼写（`grep -n '\${i}'` 无命中）。
- **谁写的**：`git_blame scripts/notify/notify.sh:170-182` → 全部 13 行同属
  **`9e8446fdd`**（作者 `user`，2026-10-12 00:41:59Z）"feat(notify): pull peer
  task events in notify.sh before local delivery"。该提交把 `deliver_all()` 改成
  带参函数以复用两次（本地 payload / 对端事件），循环变量 `i` **改名**为 `_da_i`
  （diff 里 `-while [ "$i" -lt …` / `+while [ "$_da_i" -lt …` 成对可见），
  而测试钉的是**字面拼写**。

**漂移机理**：测试用 `strings.Contains(text, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}")`
钉「槽名的唯一拼写」（`chat_session_slots_script_test.go:85-88`）。变量改名后
**语义完全没变**（`SLOT_PREFIX`/`SLOT_SUFFIX` 两个常量仍被展开、仍来自同一对声明），
但**字面不再出现** → 断言红。这是**门禁与实现耦合到标识符拼写**造成的假失败，
而不是槽名真的被拼到了别处。要用能区分的判据：把脚本改名后的那一行实测出来，
`_da_i` 参与构造 `_da_slot`，且 `SLOT_*` 三个 `readonly` 与 Go 侧常量逐字相等
（`SLOT_LIMIT=10` / `chat_session_id_` / `.txt`，
`cmd/agent/chat_session_slots.go:61-62`）。

**处理建议（两条都成立，选一）**：

1. **测试侧**：把「字面拼写」的钉改为**结构判据**（槽名由三个声明部分拼成，
   循环变量名不作断言），并保留 `SLOT_*` 与 Go 常量逐字相等的比对。
2. **脚本侧**：把循环变量改回 `i`（或让脚本在构造槽名处显式写一次
   `${SLOT_PREFIX}${i}${SLOT_SUFFIX}`），使字面判据重新成立。

两条都不是本行该做的（Rule 28：`scripts/notify/notify.sh` 属该脚本自己的面）。

### A2 + A3 — `ff6f56bdd` 把两条 help 预算一起推红（同一提交，同一处修复）

**实跑原文**：

```
# 两张树一致
--- FAIL: TestEveryHelpConstantFitsTheWrapBudget (0.07s)
    help_width_test.go:73: cli_tasks.go: tasksUsage line 102 is 89 display cols (> 88):
      "  gopi task branch-refs --peer ALIAS [--repo PATH] [--only withheld] [--json] [--db PATH]"

--- FAIL: TestPublishedHelpPagesStayWithinTheirUsageBudget (0.09s)
    --- FAIL: TestPublishedHelpPagesStayWithinTheirUsageBudget/task (0.01s)
    cli_usage_budget_test.go:170: `gopi task -h` is 193 lines — over the 190-line budget by 3.
```

**根因（文件 + 行 + 谁写的）**：

- `cmd/agent/cli_tasks.go:430`（`tasksUsage` 内的 synopsis 行）——
  `git_blame cmd/agent/cli_tasks.go:430` → **`ff6f56bdd`**（作者 `qingzhe huang`，
  2026-10-12 09:29:42 +0800）"feat(task): branch-refs — the read-only peer-ref
  stranded-delivery audit"。同一提交把 `gopi task -h` 从 190 行以内推到 **193 行**。
- 两个上限都是**本仓库自己的常量**：宽度 `config.HelpWrapCol = 88`（CJK 感知的
  `config.DisplayWidth` 度量，ASCII 行无歧义），页长
  `cmd/agent/cli_usage_budget_test.go:91 taskUsageBudget = 190`（该常量的引入是
  `f0e32cf5e` "feat(help): move verb detail into man pages, budget every published -h page"）。
- 即：**新增一个动词的 synopsis + 说明段，同时越过了宽度墙与页长墙**，而门禁
  正是为「新增动词要付预算」而设。所以这**不是**门禁过时，而是新功能没有付预算。

**处理建议（一条行即可覆盖两条）**：把 `gopi task branch-refs` 的 synopsis 在自然
的 flag-group 处折行（`--repo/--only` 一段、`--json/--db` 一段，续行对齐到
`branch-refs` 之下），并把新增的细节段移入 `man gopi-task`（本仓库
`make man` 管道已存在，`cli_usage_budget_test.go:145-146` 的失败提示就是这么写的）；
若确属新 flag 的契约而必须留在 `-h`，则**同时**在 `cli_usage_budget_test.go` 提升
`taskUsageBudget` 并写明理由——但仅是抬预算不解宽度问题，A2 仍红。

---

## 三、B 类：既有红（账本已覆盖），但**修复 stranded** —— 6 条同一根因

`TestRawPromptRunReleasesItsWorktree` 外，其余 5 条是 **resume/worker close-out
的 worktree 释放**；6 条的失败正文里都出现了同一个 **gate 拒绝串**（本 run 实测
计数：`published-anchor edge is BROKEN` 在 90fb17de1 基线为 5 次，master tip 复核
亦为 5 次）：

```
the published-anchor edge is BROKEN — no generated-document edge was discovered at all —
the scan either did not read the tree or every document lost its generator instruction
 delivery-point sync refused — the worktree was NOT released; the branch keeps its
check-out until it merges cleanly
```

**根因（文件 + 行 + 谁写的）**：

- 生产侧：`internal/tools/task_finish_gendoc.go:102 gendocGateBrokenEdgeMarker`
  ＋ `internal/tools/task_finish_deliverysync.go:85 finishCleanupLocalSyncBlockedMarker`，
  拒绝路径 `internal/tools/task_finish_cleanup.go:1810-1815`。
- 引入提交 **`4c2174873`**（2026-10-12 09:04:57 +0800）"feat(closeout): recover the
  published-anchor edge into the delivery path" —— **在 `90fb17de1` 内**，故工单点名的
  tip 就已经红。
- 账本行：**`01a12de0-5cfb-7a31-bbbf-b4e8419808ef`**（status **completed**，tags
  `bug,worker:hosta`）正是这条：其 briefing 标题即
  「交付链的 published-anchor 步在 pristine master 上把整族 finish-cleanup/closeout
  fixture 弄红（26 FAIL）」。该 run 的结论是「修复放门禁侧而非给 ~20 个 fixture 补
  manifest」，并把判据 `TakesPart()` / `GeneratedDocAnswer()` 落在
  `internal/gendoc/edge.go`（其交付 commit `f252ee8fe`，分支 tip `fc51a12fd`）。

**为什么 master 上仍然红（本 run 实测，不是推断）**：

```bash
$ git grep -c 'TakesPart' master -- internal/gendoc/edge.go
rc=1                      # master 上根本没有该标识符
$ git merge-base --is-ancestor 39522d9fd master ; echo NOT-in-master     # 修复的合并提交
$ git for-each-ref --contains fc51a12fd --format='%(refname)'
refs/heads/task/01a12e0dd700705397bf98b9cc261824
refs/heads/task/01a12e11a9597313bcfe2c2a78135acf
refs/remotes/gitPushcheck/master
```

即：修复 commit `fc51a12fd` **只被两个未合并的 task 分支和 `gitPushcheck/master`
可达**，不在 `master` 的历史里；`01a12de0` 行却已是 `completed`、其 worktree 已被
回收（`gopi task show` 的 ground 报 "NOT FOUND now — a stale reading"）。
**这是本行要提的核心缺陷：一条 bug 行以 completed 收尾，而它的修复对 master 不可达，
于是 master tip 继续对 6 个 e2e 测试红。**

**master tip 上的失败正文（`evidence/master-tip-10-tests.txt`，逐字）**：

```
cli_resume_cleanup_e2e_test.go:138: the resume path left the worktree behind (…/001/wt still exists)
cli_worker_resume_e2e_test.go:185: the worktree must be removed by the close-out chain (…/001/wt still exists)
cli_worker_resume_e2e_test.go:225: the worktree must be removed (…/001/wt still exists)
cli_worker_resume_e2e_test.go:436: the worktree must be removed by the close-out chain (…/001/wt still exists)
rawprompt_worktree_release_e2e_test.go:159: the worktree directory …/003/wt still exists after the run exited
```

### B6 的环境性成分（Rule 27：列名可审计，不静默）

`TestRawPromptRunReleasesItsWorktree` 的**断言③**（账本 account）用**系统**
`sqlite3` CLI 读回 row：

- 位置：`cmd/agent/rawprompt_worktree_release_e2e_test.go:201`（`rawPromptRows`）、
  `:263`（`rawPromptRunRow`），`t.Skipf` 在 **`:267`**。
- 本机事实：`command -v sqlite3` → **rc=1**（未安装；`evidence/env-facts.txt`）。
- 实测原文：`rawprompt_worktree_release_e2e_test.go:174: sqlite3 unavailable
  (exec: "sqlite3": executable file not found in $PATH) — the ledger assertion needs it`。
- **可审计列名（本 run 基线流里唯一的 skip 命中）**：

  ```
  TestRawPromptRunReleasesItsWorktree
  ```

  （提取命令与产物：`evidence/sqlite3-skip-audit.txt`；判据 = 该 run 的
  `go test -json` 流里 `output` 含 `sqlite3 unavailable` 的 `Test` 字段，去重后
  只有这一条。）
- **前提**：skip 只发生在**断言③**；断言①②在 skip 之前就已红（`:155` `/tmp` 目录仍
  注册、`:159` 目录仍存在）。所以 B6 **不是**「因环境而失败」，它的失败与 B1–B5 同根因。

---

## 四、D 类：账本已覆盖且**已修复落地**（工单基线上的第 11 条）

`internal/docclass` 的 `TestAnchorNumberGateOnTheRealTree`：

```
# 90fb17de1（基线树）
--- FAIL: TestAnchorNumberGateOnTheRealTree (0.10s)
    anchor_number_gate_test.go:124: 1 permit(s) in internal/docclass/testdata/anchor-number-baseline.tsv
    no longer cover a drifting row — a stale permit is a silent permission and this table
    only ratchets DOWN. Delete the row(s) (regenerate the table) or restore the site:
      devdoc/task-source-provenance.md  number-drifted  cmd/agent/cli_tasks.go:4818
      source := tasksource.DeriveNow(opts.source)

# d7fb65344（master tip 复核树）
$ cd /dev/shm/gopi-01a12e06-master && go test -count=1 -run TestAnchorNumberGateOnTheRealTree ./internal/docclass/
ok  	github.com/nick/gopi/internal/docclass	0.478s        → 绿
```

- 账本行：**`01a12ddd-424c-7913-b134-6d67966f04b9`**（status **completed**，tags
  `bug,docclass,anchor-number,pre-existing-red`），briefing 即本条现象。
- 落地提交：**`9fdde2f78`** "fix(docclass): regenerate the anchor-number freeze — one
  permit's verdict changed class"，**在 master 上**（`git log --oneline master --
  internal/docclass/testdata/anchor-number-baseline.tsv` 首行）。
- 漂移的机理（本 run 实测的数字）：permit 记的是 `cmd/agent/cli_tasks.go:4818`，而该
  文本在 90fb17de1 上位于 **:5625**、在 master 上位于 **:5629**；表的
  `settled_at` 文本随之从 "recorded text is at :5556, not :4818"（90fb17de1）改为
  ":5629, not :4818"（master）。所以工单基线上的红是**该表在 90fb17de1 那一刻的
  瞬时状态**，`9fdde2f78` 之后不再成立。

---

## 五、本行做了什么、没做什么（诚实边界）

- **做了什么**：两树实跑并集差、11 条逐条分诊、A 类三条与 B 类根因定位到文件+行+提交、
  账本行指认（`01a12de0` / `01a12ddd`）、A 类与 B 类各提 bug 行（含复现与证据路径）。
- **没做什么（不在本行范围）**：没有修改任何被分诊的代码（`scripts/notify/notify.sh`、
  `cmd/agent/cli_tasks.go`、`cmd/agent/*_test.go`、`internal/tools/`、`internal/gendoc/`
  均不在本行 diff 里）；B 类的修复已存在（`fc51a12fd`），本行只报「对 master 不可达」，
  不动门禁也不动 fixture。
- **没做（环境）**：未安装 `sqlite3` 来「让 B6 变绿」——那会掩盖 A/B 两类的真实信号，
  且属于改本机环境而非改代码。

## 六、收尾实测：三条 bug 行提交后被**串行链钉住**（本 run 实测，已修复）

**现象（本 run 实跑，非推断）**：三条 bug 行提交后回读队列，**三条全部不可认领**：

```
#01a12e30-fc36-…  [pending] … [queue-skip] parent 01a12e30-f209-… not released (status=pending)
#01a12e30-f209-…  [pending] … [queue-skip] parent 01a12e30-e32e-… not released (status=pending)
#01a12e30-e32e-…  [pending] … [queue-skip] parent 01a12e06-d42a-7a63-9128-d7478d73aca8
                                                             not released (status=in_progress)
```

**机理（读源码复核，`internal/agent/loop.go:6855-6868`，注释自称 operator ruling 2026-10-05）**：
提交缝把**连续提交的行自动串成串行链**（item i 的 parent = item i−1），设计意图是
「提交顺序 = 依赖顺序」；并且本会话的**第一条**提交（`lastSubmitRowID` 为空）把 parent
回退锚到 **`a.autoTaskRowID` = 提交者自己那条 live 行**。三条缺陷彼此**无依赖**
（三个不同文件：`scripts/notify/notify.sh` / `gopi task branch-refs` 的 help 页 /
`internal/tools` 的 close-out 释放），却被一个**环状**的串行链全部 park：
e32e 等本行、f209 等 e32e、fc36 等 f209 —— **一个 dispatcher 也拿不到**，
而 Rule 33 明说没有 `--force`、没有 staleness 释放、父行 `cancelled/failed` 就永久 park。
**串行链本身不是缺陷**（有意设计，且 `reframe.go` 会自报 chainNote）；缺陷是
(a) **链首自指**提交者自己的 live 行，(b) **「互相独立」没有拼写**，
(c) **失败静默**——三条回执只有 `Task queued: …` 一行。

**修复（实测有效）**：`gopi task update <id> --clear-parent --reason …` 逐条清除，
回读三行已无 `[queue-skip]`：

```
--- gopi task update 01a12e30-e32e-… --clear-parent --reason '…'
Task parent updated: 01a12e30-e32e-7d61-b96e-cbee5766a504 (parent_task_id 01a12e06-… -> (none))
--- … f209 …  (parent_task_id 01a12e30-e32e-… -> (none))
--- … fc36 …  (parent_task_id 01a12e30-f209-… -> (none))
### AFTER  gopi task list --status pending | grep 01a12e30
#01a12e30-fc36-…  [pending]  bug: 6 条 worktree-释放 e2e …              ← 无 [queue-skip]
#01a12e30-f209-…  [pending]  bug: gopi task branch-refs …              ← 无 [queue-skip]
#01a12e30-e32e-…  [pending]  bug: TestNotifyScriptAgreesWithTheRegistryConstants …  ← 无 [queue-skip]
```

原始输出：`evidence/bug-rows-front-state.txt`（修复前）+ `evidence/bug-rows-parent-edge-repair.txt`
（逐条 `--clear-parent` 回执与修复后队列）。**提行不等于入队**——回读 `[queue-skip]`
才是判据；本行据此另提一条 bug 行（提交缝缺"独立"表达，见报告 `## Completed` 末条）。

**遗留的诚实边界**：`--clear-parent` 是**事后**修补；提交时没有"这三条互相独立"的拼写，
后人重复同样的三步仍会踩到同一坑。本行不改提交缝代码（系统层归 peerbox，Rule 28），只提行。

**另一处实测更正（Rule 41 自纠）**：本文早期收尾有一行写 `grep -n '\${i}' … rc=0`，
读起来像"脚本里还有 `${i}`"。实测更正为 **`grep -c -F '${i}'` → `0`（rc=1）**，
即 master 的 `scripts/notify/notify.sh` 里**确实不再有** `${i}` 这个拼写（A1 的判据成立），
`_da_i` 出现于 :174-:177。原始输出见 `evidence/closeout-reverify.txt`。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`（两棵只读树内各一次）— OK
- Tests（本 run 实跑，非引用）:
  - 基线 90fb17de1：`make test-budget PKGS='./internal/docclass ./cmd/agent'` →
    docclass fail / cmd/agent fail(892s)，11 条具名 FAIL
  - master tip d7fb65344：`go test -count=1 -v -run '<9 名>' ./cmd/agent/` → 9/9 FAIL，EXIT=1；
    `go test -count=1 -run TestAnchorNumberGateOnTheRealTree ./internal/docclass/` → ok
- Ran: 上面两条命令原样（stream 与输出落 `evidence/`、`evidence2/`）
- Result: 见第一、二、三、四节的原文引用
