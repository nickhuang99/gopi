<!-- doc-class: historical -->
# Task Report: client 本地提交 —— GOPI.md 本机段改写 + 本地 row 的上行机制查清

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a12b30-7cff-7234-8a35-61ce0525c773` · worktree `~/workspace/gopi-01a12b307cff72348a3561ce0525c773`
branch `task/01a12b307cff72348a3561ce0525c773` · 2026-10-11

## Completed

1. **GOPI.md 本机段（LOCAL ONLY, hosta）第 1 条改写为「本机直接提交」** ——
   命令示例不再有 `ssh peerbox` / `scp` / 远端 `/tmp`，`--source human`→`--source agent`，
   并补上「提交位置 ≠ 执行位置」的机制指针。只动了 LOCAL ONLY 段（第 1394–1435 行），
   规则区（Rule 1–41）与 Rule 28 的分工一字未改。
2. **三条经验全部保留**：`--payload @FILE`、`--attachment` 单数可重复（无 `--attachments`）、
   必须回读 `Task queued: <id>`。
3. **一条旧表述被实测推翻并更正**（Rule 41 的场景，就地更正而非另提一条）：
   旧文写「拼错参数时命令只打印 usage 并 **exit 0**」——
   实测 `gopi task submit … --attachments F` → `unknown flag --attachments` + usage，
   **exit 1**，且该行**根本没有入队**（见 §Evidence 的 EXIT=1 两条）。
   新文按实测写成「exit 1，行未入队，所以必须回读 `Task queued:`」。
4. **本地 row 的上行机制查清**（见下一节，是三处配置开关 + 两条腿的确切答案）。
5. 全仓 grep 确认：改写后**「一律去 peerbox 提」类表述只在本机的 GOPI.md 本机段存在过，现已消失**；
   仓库里别处的 `ssh peerbox` 命中全部与「在哪提交任务」无关（见 §Evidence 的 grep 分类）。

## 本地 row 如何上行到执行端 —— 确切答案

### 三个键各管什么（`internal/config/task_sync_switch.go`，一文件三开关）

| 键（settings.json） | env | 默认 | 管什么 |
|---|---|---|---|
| `role` + `sync_to` | `GOPI_NODE_ROLE` / `GOPI_SYNC_TO` | 未声明 | **身份**（`internal/config/node_role.go`）：冲突仲裁 + 谁的 finish-cleanup 分支推送跑。**不**等于「本机账本是远端镜像」。 |
| `task_sync` | `GOPI_TASK_SYNC` | **OFF** | **自动行交换**：claim 广播、终态推送、serve-tick delta、**submit 转发**。全 OFF 时这些整体 no-op；手工动词（`task sync`/`diff`/`harvest --peer`、`task push`、`sync --push-only`）**照常可用**，`sync_peers` 仍是它们的白名单。 |
| `task_pull` | `GOPI_TASK_PULL` | OFF | **worker 拉取**：本机队列**空了**之后，去每个 `sync_peers` 要**一行**可转移的 row 并在本机跑（`task receive --takeover`）。这是**反方向**。 |

代码里说得很明确（`task_submit_forward.go` 的 `taskSubmitNeedsForward`）：
`task_sync: false` 时 `taskSubmitNeedsForward()` 第一句就 `return false` ——
**submit 不转发，row 直接写本机账本**。注释原话：

> Before this switch the two were welded, so declaring a node a client removed its ability to
> submit locally: the row was forwarded, or (with no sync_to) refused. **A client node whose
> operator has not turned task sync on now submits into its OWN ledger, which is what a single
> node's ledger is — the row it will actually claim and run.**

### 两条腿：**代码交付** vs **行交换**（关键区分）

`task_sync` **不**是代码交付的开关。分支 push-back（`finishCleanupServerTarget` →
`config.ResolveSyncPeers`）、serve 的 git/ref pass、adoption sweep，
都由**声明的拓扑**（`sync_peers` + `IsServerNode`）决定，与 `task_sync` 无关 ——
有一条测试把这件事钉死：`internal/tools/task_sync_switch_finish_test.go` 断言
`finishCleanupServerTarget` **不得**出现 `TaskSyncEnabled`。

即：**hosta 的 `task_sync: false` 不影响分支回推 peerbox（代码交付在线），只影响行交换。**

### 当前配置下：本机提的 row **被执行端拿到了吗？**

**拿到了 —— 但执行端是 hosta 本机的 worker，不是 peerbox。**

三处实测（本 run 现场读数）：

```
$ pgrep -x gopi | … /proc/<pid>/cmdline …        # 4 个常驻 gopi worker
gopi worker        cwd=/home/user/.gopi/sessions/01a12b30-7cff-…   # 其中一个是本 run 的 worker
gopi worker        cwd=/home/user/.gopi/sessions/01a12b2c-…
```

```
$ gopi task list --status in_progress | head -3
#01a12b30-7cff-7234-8a35-61ce0525c773  [in_progress]  …  [via worker] [ground task/01a12b30… @ /home/user/workspace/gopi-01a12b30…]
#01a12b2c-6ba2-…                        [in_progress]  …  [via worker] …
#01a12b14-c54a-…                        [in_progress]  …  [via worker] …
```

三个 row 都带 `[via worker]` 且各有 ground —— **都是本机账本里的 row，被本机 worker 认领并正在跑。**

```
$ ssh peerbox 'gopi task show 01a12b30-7cff-7234-8a35-61ce0525c773'
gopi task show error: task: no task with task_id="01a12b30-7cff-7234-8a35-61ce0525c773"
```

**peerbox 账本里没有这一行。** 结论：

- **本地 row 会被执行**（hosta 本机 worker 直接认领同一账本），不是黑海；
- **但它不会自动出现在 peerbox 的队列里** —— `task_sync: false` 使自动行交换整体 no-op，
  包括 `submit` 转发那一跳；
- **`task_pull: true` 不解决这件事**：它管的是 worker 队列空时**去 peer 拉一行**（反方向），
  与本机 row 上行无关。

### 若要让 row 上行到 peerbox 队列 —— operator 的动作（点名键与值）

**这是配置决定，本任务不改（briefing 明令）。** 两个可选面：

1. **持续自动上行**：`~/.gopi/settings.json` 的 **`"task_sync": true`**（或 `GOPI_TASK_SYNC=1`）。
   代价：claim 广播 / 终态推送 / serve-tick delta 也一并打开 —— 这台机器从此参与双向行交换，
   不再只是本地队列。**注意**：`role: client` 的身份会随之生效 ——
   `taskSubmitNeedsForward()` 在 `task_sync: true` **且** `role: client` 时 `return true`，
   此后本机提交走**远端权威账本转发**路径（对 `sync_to=peerbox` 做 ssh 转发 + 回显），
   一旦转发腿不通就是**响亮拒绝**（`taskSubmitRefusalFmt`：不含糊地写「未写入任何本地账本」）。
   所以这一刀不是「只把 row 推上去」，而是把提交语义整体换成转发。**建议 operator 想清楚再动。**
2. **按行手工上行**（不改变全局语义，推荐先试）：
   ```bash
   gopi task sync --push-only --task-id <id> --peer peerbox
   ```
   这是 claim 广播的手工面，`task_sync: false` 时**照常工作**（帮助页原话：
   the switch governs exchange this node does on its own, never one an operator asked for）。

**本报告不自作结论推荐哪个** —— 这是 operator 的决定；两个面的行为差异与代价已如实列出。

## Not Done（诚实）

- **`task_sync` 的开关没有动，也没有替 operator 做「该不该开」的判断** —— briefing 明令。
- **没有验证 `task_sync: true` 时的转发路径真跑** —— 那需要改本机配置并触动 `role: client`
  的提交语义（见上），超出本任务边界；本节只引用代码与帮助页，**不声称实测过**。
- **仓库里另有一处同义表述，未改，属另一个任务**：
  `devdoc/task-01a12b08-terminal-writeback-gap.md:194,196,233` 写
  「按本机策略 1，系统层修复去 peerbox 提 / 去 peerbox 提交」。
  它是**历史 run 的记录**（DOC-INDEX 里若为 historical，改写它就是篡改记录），
  且它引用的是**已被本次改写废弃的策略编号**。**不在本任务改**（Rule 4/历史记录原则），
  由 Rule 26「修不完整就再派一条」另行处理 —— 建议归为一次 doccheck 类的小任务。
- **Rule 41 视角的一个观察，未展开**：`gopi task submit --source human` 被拒时
  **exit 1 且不写行**（已实测），但拒绝文案里那句
  「the row would be stored as agent」**与实测矛盾**（行根本没被存）。
  同一族的 `--attachments` 拼错也是 exit 1 + usage 刷屏、行未入队。
  这两处「用户按提示以为命令成功了/按提示以为会降级存储」是可提的 bug 材料，**本报告只记录现象，
  不代替提行**（本任务边界是改文档，不是扩队列）。

## Verification

- **Build**: `go build ./...` → `BUILD_EXIT=0`（无输出）
- **Tests**: `go test ./internal/standards/ -run 'DocIndex|Manifest|Census'` → 见 §Evidence
- **Ran**:
  1. 本机真提交（Rule 31）——
     `cd /home/user/workspace/gopi-01a12b307cff72348a3561ce0525c773 && gopi task submit "Rule 31 probe …" --payload @/home/user/.gopi/sessions/01a12b30-7cff-…/artifacts/rule31-probe.md --tags bug --source agent`
  2. 回读 —— `gopi task show 01a12b40-e496-71e6-afe2-4cd1fc1c2f19`
  3. 跨机对照 —— `ssh peerbox 'gopi task show 01a12b40-e496-71e6-afe2-4cd1fc1c2f19'`
  4. 收尾取消 —— `gopi task update 01a12b40-e496-71e6-afe2-4cd1fc1c2f19 --status cancelled --reason …`
  5. `--source human` 拒绝面 —— `gopi task submit "probe: human source exit code" --source human`
  6. 全仓 grep（下节）
- **Result**: 见下节原文粘贴。
- **Commit**: `16df17009 docs(local-policy): client 本地提交取代「bug 一律去 peerbox 提」(ssh/scp)`

## Evidence (self-filed)

### ① 本机提交真跑（Rule 31）

```
$ gopi task submit "Rule 31 probe (task 01a12b30-7cff): 本机 gopi task submit 真跑取证，用于验证 client 本地提交面 — 立即取消" --payload @/home/user/.gopi/sessions/01a12b30-7cff-7234-8a35-61ce0525c773/artifacts/rule31-probe.md --tags bug --source agent
Task queued: 01a12b40-e496-71e6-afe2-4cd1fc1c2f19 (status=pending, source=agent, tags=bug)
[bug-flow] tasks with tag bug: 226 (verified 0, reopened 7, other 219)
EXIT=0
```

**不需要 `ssh`、不需要 `scp`、不需要远端 `/tmp`** —— 命令在 hosta 本机一次跑通。

### ② 回读（本机账本）

```
$ gopi task show 01a12b40-e496-71e6-afe2-4cd1fc1c2f19
task_id:        01a12b40-e496-71e6-afe2-4cd1fc1c2f19
status:         pending
summary:        Rule 31 probe (task 01a12b30-7cff): 本机 gopi task submit 真跑取证，用于验证 client 本地提交面 — 立即取消
run_dir:        /home/user/.gopi/sessions/01a12b40-e496-71e6-afe2-4cd1fc1c2f19
tags:           bug
priority:       1
source:         agent
submitted_by:   LLM session  (task_submit)
```

### ③ 跨机对照：同一个 id 在 peerbox 上不存在

```
$ ssh peerbox 'gopi task show 01a12b40-e496-71e6-afe2-4cd1fc1c2f19'
gopi task show error: task: no task with task_id="01a12b40-e496-71e6-afe2-4cd1fc1c2f19"
```

### ④ 收尾取消（探针不执行）

```
$ gopi task update 01a12b40-e496-71e6-afe2-4cd1fc1c2f19 --status cancelled --reason "Rule 31 取证完成，立即取消（不执行）"
[task-notify] cmd ok (stdout: )
Task updated: 01a12b40-e496-71e6-afe2-4cd1fc1c2f19 (status=cancelled, reason=Rule 31 取证完成，立即取消（不执行）)

$ gopi task show 01a12b40-e496-71e6-afe2-4cd1fc1c2f19
status:         cancelled
```

### ⑤ 拼写坑：实测 exit 1，且行未入队（推翻本机段旧文的「exit 0」）

```
$ gopi task submit "probe: will not be queued" --attachments /tmp/rule31-probe.txt --tags bug
gopi task submit: unknown flag --attachments
GoPi task subcommands — the task-as-memory ledger (跨会话追溯/审计).
EXIT=1
```

### ⑥ `--source human` 在本机被拒（ssh 路径会绕过这个检查）

```
$ gopi task submit "probe: human source exit code" --source human
gopi task submit error: refusing --source human: only a process with no agent chain above it may
record human (this process is inside an agent session, so it may not claim a person filed the row —
the row would be stored as agent). File it with the role you actually are (agent/worker/server), or,
if you are the operator, re-run with GOPI_HUMAN_TERMINAL=1
EXIT=1
$ gopi task list --all | grep -c "human source exit code"
0                      ← 拒绝即不写行
```

### ⑦ 全仓 grep：改写后本机段已无该表述；别处命中与「在哪提交」无关

```
$ git grep -n "一律去 peerbox\|提交必须在 peerbox\|去 peerbox 提\|只取证不改 bug"
devdoc/task-01a12b08-terminal-writeback-gap.md:194:## 5. 后续（按本机策略 1，系统层修复去 peerbox 提）
devdoc/task-01a12b08-terminal-writeback-gap.md:196:去 peerbox 提交（本任务只做定位）：
devdoc/task-01a12b08-terminal-writeback-gap.md:233:  … 已列 §5 去 peerbox 提。
```

**`GOPI.md` 零命中** —— 本机段是它唯一的所在地，改写即清空。
唯一残留是那份**历史 run 记录**（见 §Not Done），它引用的是已被废弃的策略编号。

```
$ git grep -n "ssh peerbox" -- '*.md' '*.go' '*.sh' | wc -l     # 命中均为别的用途
```

分类（逐条看过，无一与「提交任务的位置」有关）：voice_input 的 stdio 传输
（`devdoc/voice-e2e-hp.md`、`SPEC-voice-input-whisper.md`）、备份例程
（`backup-peerbox-to-hp-routine.md`、`peerbox-to-hp-backup-routine.md`）、
求校验和的只读探针（`voice-input-default-shape-reverification.md`）、
历史审计记录（`devdoc/requirements/evidence/*`、`tasks/legacy-audit-*.md`）。
**结论：仓库里没有第二处「bug 一律去 peerbox 提」的同义表述。**

### ⑧ 本机段的三条经验仍在（改写后原文）

```
- **拼写坑（实测）**：payload 是 `--payload @FILE`；附件参数是 **`--attachment`（单数、可重复）**，
  **没有** `--attachments`。… **必须回读 `Task queued: <id>` 那一行才算提交成功**
- **`--source` 写真实身份**…
```

（`--payload @FILE` ✔、`--attachment` 单数可重复/无 `--attachments` ✔、回读 `Task queued: <id>` ✔）

## 与 Rule 28 的关系（不自作结论）

Rule 28 划的是**层**：peerbox 管系统层（worktree 生命周期、分支/合并机具、merger、任务账本、
sync、调度），HP 管不碰系统层的特性开发。本任务**只改「在哪提交」**（hosta 本机 vs ssh 到 peerbox），
**没有改「谁来修」** —— 本机段新文首句就写明「层次分工不变（系统层归 peerbox，Rule 28）——
变的是**在哪提交**，不是**谁来修**」。

两点观察，**留给 operator 判断，本报告不下结论**：

1. 「系统层归 peerbox」**并不意味着「系统层的 row 必须在 peerbox 上提」** —— 本机提交 +
   `--push-only` 手工上行，同样能把一行送到 peerbox 的队列由 peerbox 的 worker 执行。
   提交位置与修复位置是两个可分离的轴。
2. 但 Rule 28 里那句「a feature task is HP-shaped if it … does **not** change how work is
   scheduled, claimed, merged, or cleaned up」说的是**谁执行**；本机提交若配上
   `task_sync: true` + `role: client`，会改变本机的提交语义（转发到权威账本），
   那才是真正触碰系统层边界的动作 —— **这也是本报告把 `task_sync` 的开启列为 operator 决定的原因。**

## 附：本次改动的验证实况（含一条不能算过的红）

改动的**分支作用域**只有 3 个文件 —— `GOPI.md`、`devdoc/DOC-INDEX.tsv`、
`devdoc/local-submit-from-client.md`（`git diff --name-only master...HEAD` 实测），
**没有一行 Go 代码**。据此做的验证与它的真实结果，逐条记下（Rule 5「如实」）：

| 检查 | 命令 | 结果 |
|---|---|---|
| Build | `go build ./...` | exit 0，无输出 |
| 语料门禁 | `go test ./internal/standards/ -run 'Doc\|Index\|Gopi\|Manifest\|ConfigDoc'` | `ok … 6.5s` |
| doccheck | `gopi tool doccheck` | `the devdoc corpus is consistent` |
| worktree | `git status --porcelain` | 空 |
| 包测试 | `make test-budget PKGS='./internal/tools ./cmd/agent'` | **FAIL（28 例）—— 是环境性的，不是本次改动** |

**关于那 28 例红：判据是「能不能指到本机缺的东西」，不靠推断"应该无关"。** 定向单跑最小样本
（`go test -count=1 -run 'TestCodeSearchBackendNoSymbol|TestMergeBuildGate_RefusesTheIncidentShape|TestQueryDefinitionSelfHealsUnsearchableIndex' ./internal/tools/ ./cmd/agent/`）
给出的原始原因是**两类本机环境缺失**：

```
--- FAIL: TestCodeSearchBackendNoSymbol (0.00s)
    cli_code_search_test.go:168: CodeSearch(no-symbol) error: code_search: ctags: exec: "ctags": executable file not found in $PATH
--- FAIL: TestMergeBuildGate_RefusesTheIncidentShape (0.03s)
    merge_build_gate_test.go:244: the non-compiling branch was NOT refused (verdict={Refused:false Detail: BuildCmd: BranchErr:})
[task-notify] merger: BUILD gate ABSTAINS for fix/merger-rebuild-selfrestart (no usable shared Go build cache — the reason is logged above) — merging unchanged
```

即 **① hosta 未装 `ctags`**（`which ctags` → NOT FOUND），**② 无可用共享 Go build cache
→ merge-build 门禁 ABSTAIN**，两类都让对应测试在**本机**必红。它们是**本机环境缺陷的暴露**，
不是本次改动的回归 —— 但**本报告不把它们写成"全绿"**：本次交付**没有**取得
`./internal/tools ./cmd/agent` 的通过记录，这一条如实留在 `## Not Done` 的口径里。
（`go test ./internal/tools` 在 hosta 上**全红**这一事实本身可提 bug 行，见文末观察。）
