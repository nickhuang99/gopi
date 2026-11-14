<!-- doc-class: historical -->
> **HISTORY (added by the restore row).** `internal/tools/merge_config_anchor_gate.go` and its
> test file were deleted whole in commit `5a35f6104` (the `internal/docclass` teardown). Every
> occurrence below is a RECORD of that lane while it existed — **not** a command to run and not
> a check that runs today. That lane's live check **was** the census generator's `--check` leg
> plus the arms restored under the same documented name `TestMergeConfigAnchorGate`, which drove
> it rather than any merge-time lane. **BOTH WERE DELETED (2026-11-05)**: `78bdf05a`
> (operator ruling) removed the documentation-citation family — `config-anchor-census.go`, its
> `.tsv`, `citation-census.go`, `citation-selector-census.go` and the gates that drove them —
> and `b8cad761` removed `internal/tools/merge_config_anchor_gate_test.go` with the last census
> arm. Nothing judges CONFIG.md's anchors today: `go run` on the generator now prints
> `stat … no such file or directory`, and `-run TestMergeConfigAnchorGate` prints
> `[no tests to run]` and exits 0 (an EMPTY GREEN, not a pass). The anchor rules are prose
> authors obey; a reader must not run either command this note used to give.
> **后续裁决变了（2026-11-01，merge pass verify/withdraw 删除）。** 本文 §3 与 §5.2 把
> 「合并后整树 `go build ./...` + `go vet ./...` 的 post-pass 校验，失败则 withdrawal」
> 列为 merge 体系**仍需保留**的职责之一（§5.2 第 ③ 行，引 `WithdrawMergePass`、
> `merge_run.go:2099`）。**那条职责已经不在了**：`internal/tools/merge_pass_verify.go`
> （`CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`）与
> `merge-withdrawn` 告警在 2026-11-01 随退休的顶层 `gopi merge` 的最后一刀一并删除，
> 它最后的产量调用者 `cmd/agent/merge_run.go` 更早已删。所以 §3 那句「唯一仍属 merge
> 体系独有的能力」只剩前半句（把一条不在任何 workspace 里的分支合进 master），
> §5.2 只剩 ①② 两条。**`gopi task merge` 不校验合并后的树，也不撤回任何落地。**
> 本文文字保留原样 —— 它是 2026-10-19 那天裁决的记录，改它就等于篡改记录；变的只是
> 它描述的那个能力，`man/gopi-merge.md` 带的是更正后的合同。

# worktree 退场系列 5/5：merge 体系的职责、消费者与裁决

**基线**: `5a03e30`（本工作树 HEAD，2026-10-19）
**会话/工作树**: `/home/user/workspace/01a150f4-298a-7b96-9346-d861551fdff8`
**测量时刻**: 2026-10-19T21:5xZ ~ 22:0xZ（本机 hosta）+ peerbox `2026-10-19 13:48 UTC`
**裁决**: **保留（部分保留 + 改造）** —— 不删代码。理由见 §5；Q2 的证据是"零流量"，不是"零消费者"。

本条不执行删除。系列 1–4 交付了修复与退役；本条是调查与裁决，**"保留"是设计要求内的合法结果**，因为 merge 体系的入口（`gopi merge --status`、`gopi task audit-merges`、`gopi task merge-execute`）实测仍然可用且仍是操作员的唯一合并面板，删掉它不会让 clone 通道变强，只会让操作员失去一个只读诊断面。

---

## 0. 为什么这条不能"看起来没人用就删"

三条实测事实把"删"挡在门外，每条都是 `bash` 的输出而不是印象：

1. **入口是活的**（§2.1）：`gopi merge --status` 在 peerbox 与本机各返回 `rc=0` 并按自己的契约输出 branch inventory。
2. **有跨组件的读者**（§1）：`mergeBacklogFamilyBranch` 这个"merge-backlog 家族解析器"被 `archived_branch_retirement.go` 与 `archive_row_reconcile.go` 消费 —— 两个文件都不属于 merge 执行链。
3. **零流量是 2026-10-18 之后的事**（§2.3）：clone 通道启用的当天起，peerbox 的 merge-run 行从"每天数十条"变成 **0 条**。这是"消费者换了交付路径"，不是"消费者不存在"。删掉它等于在操作员还没说"我不再需要合并面板"的时候替他把面板拆了。

---

## 1. Q1 — 职责边界表

### 1.1 普查命令

```bash
M=$(ls internal/tools/merge*.go internal/tools/merger*.go internal/tools/task_merge*.go \
        internal/tools/task_finish_premerge*.go internal/tools/task_merged_cleanup*.go \
        internal/tools/rebuild_vet_mergeset*.go internal/tools/safe_merge*.go \
        internal/tools/finish_cleanup_merge*.go internal/tools/mergeset_*.go \
        cmd/agent/merge*.go cmd/agent/merger*.go cmd/agent/cli_tasks_merg*.go 2>/dev/null | sort -u)
echo "files=$(echo "$M"|wc -l)"; echo "$M"|xargs wc -l|tail -1
echo "non-test files=$(echo "$M"|grep -vc _test.go)"; echo "$M"|grep -v _test.go|xargs wc -l|tail -1
```

原始输出：

```
files=152
  56988 total
non-test files=53
  26188 total
```

即 merge 体系 = **152 个文件 / 56,988 行**，其中非测试 **53 文件 / 26,188 行**、测试 **99 文件 / 30,800 行**（测试占比 54%，这也是"删除"的成本：每删一行实现要连带删 1.2 行测试）。

### 1.2 分组（每组一行，规模用同一批 `ls`+`wc -l` 量出）

| 组 | 范围 | 文件 | 非测试行 | 测试行 | 职责 | 非测试调用者（实测） |
|---|---|---|---|---|---|---|
| **A** | `cmd/agent/merge*.go` | 54 | 8,422 | 11,804 | `gopi merge` 动词本体：一次 pass、`--fast`、`--status`、`--every` 循环、合并锁、pass 校验、union、backstop、handover 预算 | `main.go:659` `isMergeSubcommand`（**活**） |
| **B** | `cmd/agent/merger*.go` | 6 | 436 | 577 | 已退役守护进程的**入口拒绝面**（`merger_retired.go` 报告 knob，不消费） | `main.go:659` `isMergerSubcommand`（**活**，仅拒绝） |
| **C** | `internal/tools/merge_*gate*.go` | 27 | 5,502 | 6,011 | 落地前的六道门禁：build / contract / union / anchor / target-doc / config-anchor | 全部收敛到 `task_merge.go` + `repo_sweep.go` 两处（见 1.3） |
| **D** | `merge_pass*.go` + `merge_union*.go` | 5 | 1,419 | 1,057 | 一次 pass 的快照/校验/**撤回**（`CaptureMergePassScope`/`WithdrawMergePass`） | `merge_run.go:1914`、`:2099` |
| **E** | `task_merge*.go` | 22 | 4,610 | 4,189 | merge-backlog 审计（发现未合并分支）→ pending 行 → 执行器（`TaskMergeBacklogExecute`）、幽灵 ref 派发守卫、终态守卫 | **两组**：`cli_tasks.go`（CLI 面）+ `archived_branch_retirement.go` / `archive_row_reconcile.go`（**非 merge 代码**） |
| **F** | `task_finish_premerge*.go` | 3 | 1,319 | 508 | 交付前同步校验（把 master 合进待交付分支） | `task_finish_cleanup.go:1434`（系列 2/5 已退役其动词面，实现仍在） |
| **G** | `task_merged_cleanup*.go` | 4 | 1,097 | 984 | 合并后的分支/工作树清理 | `cli_tasks.go:4467`、`:4731`、`:4540`（`gopi task merged-cleanup`） |
| **H** | `mergeset_*.go` + `rebuild_vet_mergeset*.go` | 12 | 2,248 | 3,172 | 合并后的 rebuild + vet 集合（`RebuildAfterMergeMergeset` 被 `rebuild.go:171` 调用） | `rebuild.go:171`（**非 merge 文件**） |
| **I** | `merge_revert*.go`、`merge_ceiling_rework.go`、`merge_handover_*.go` | 6 | 902 | 715 | 失败合并的回滚快照、上下文天花板触发的 rework 行、hand-over 计数器/升级 | `merge_run.go`（回滚、handover） + `cli_tasks.go:6202`（回滚快照） |
| **J** | `safe_merge_contract*.go` | 2 | 142 | 236 | Rule 9 前置守卫的**词表契约**（Go 侧镜像 `scripts/lib/safe-merge-contract.sh`），parity 测试对齐两边 | `task_merge_executor.go:280` `mergeExecSafeMergeBlock` |
| **K** | `merger_status*.go`、`merger_ancestors.go`、`merger_graceful*.go` | 15 | 1,795 | 1,391 | `gopi merge --status` 的渲染层：每个分支归入一个桶 + 算术自洽 | `merge_run.go:3860` `MergerSweepPreview` |

### 1.3 数据流（门禁为什么只属于 merge）

门禁入口全是**包私有**的（`mergeBuildGate`、`mergeContractGate`、`mergeUnionGate`、`mergeAnchorGate`、`mergeTargetDocGate`、`mergeConfigAnchorGate`），精确普查非测试调用点：

```bash
for s in mergeBuildGate mergeContractGate mergeUnionGate mergeAnchorGate mergeTargetDocGate mergeConfigAnchorGate; do
  grep -rn "$s(" --include=*.go internal/tools/ | grep -v _test.go | grep -v "func $s"
done
```

原始输出（节选，完整见下）：

```
internal/tools/task_merge.go:672:  if gate := mergeBuildGate(repo, master, branch); gate.Refused {
internal/tools/repo_sweep.go:1464: if gate := mergeBuildGate(repo, masterBranchName, branch); gate.Refused {
internal/tools/task_merge.go:639:  if gate := mergeContractGate(repo, master, branch); gate.Refused {
internal/tools/repo_sweep.go:1460: if gate := mergeContractGate(repo, masterBranchName, branch); gate.Refused {
internal/tools/task_merge.go:707:  if gate := mergeUnionGate(repo, master, branch); gate.Refused {
internal/tools/repo_sweep.go:1468: if gate := mergeUnionGate(repo, masterBranchName, branch); gate.Refused {
internal/tools/task_merge.go:745:  if gate := mergeAnchorGate(repo, master, branch); gate.Refused {
internal/tools/merge_target_doc_gate.go:334: verdict := mergeTargetDocGate(repo, master)
internal/tools/merge_target_doc_gate.go:361: verdict := mergeTargetDocGate(repo, masterBranchName)
internal/tools/merge_target_doc_gate.go:257: cfg := mergeConfigAnchorGate(repo, target, dir)
```

**六道门禁的全部调用点只落在两个文件**：`task_merge.go`（`gopi task merge` 的 CLI 面）与 `repo_sweep.go`（扫楼面）。两者都只在"要合并一个分支到 master"时被调用 → 门禁是 merge 的**从属**，不是独立能力。

### 1.4 精确的"零读者"叶子（真零，非测试调用点 n=0）

```bash
census(){ s=$1; h=$(grep -rn "\b$s(" --include=*.go internal/ cmd/|grep -v _test.go|grep -v "func $s"); \
  printf "%-30s n=%s\n" "$s" "$(echo "$h"|grep -c .)"; }
for s in VerifyMergePass MergeCeilingRework MergesetRebuild RebuildVetMergeset \
         MergedCleanup TaskMergedCleanup MergeRefuseAnchor FinishCleanupOwnerRowForWorktree; do census $s; done
```

原始输出：

```
VerifyMergePass                   n=0
MergeCeilingRework                n=0
MergesetRebuild                   n=0
RebuildVetMergeset                n=0
MergedCleanup                     n=0
TaskMergedCleanup                 n=0
MergeRefuseAnchor                 n=0
FinishCleanupOwnerRowForWorktree  n=0
```

这 8 个**名字**零读者（其中 `merged-cleanup` 的真入口叫 `MergedCleanupSweep`，n=2；这批是声明名与实际入口不同名，见 §6 的执行步骤 —— 这正是"不能凭 grep 命中删除"的反面例子）。

---

## 2. Q2 — `gopi merge` 腿是否仍有消费者（实测 peerbox + 本机）

### 2.1 入口仍然原样工作（两机各一次，只读，不取锁）

peerbox：

```bash
ssh peerbox 'cd /home/user/work/gopi && git log --oneline -1 && gopi merge --status; echo rc=$?'
```

原始输出：

```
5a03e30 CONFIG.md: re-point two sweep-worktrees anchors after the symbol deletion
=== gopi merge --status (READ-ONLY) ===
branch-inventory: /home/user/work/gopi
  branches:  1 local head(s) = 1 integration + 0 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 0 pending
archived: /home/user/work/gopi
  none — no branch is tagged out of the merge queue
=== rc=0 ===
```

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge --status`
> line below is the RECORD of what this run typed, kept verbatim; the top-level verb
> was removed afterwards and answers **exit 1** today (`gopi task merge` runs the
> same engine). The verdict recorded here is about branch retirement, unchanged.

本机：

```bash
gopi merge --status; echo rc=$?
gopi task audit-merges; echo rc=$?
```

原始输出：

```
branch-inventory: /home/user/work/gopi
  branches:  1 local head(s) = 1 integration + 0 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 0 pending
archived: /home/user/work/gopi
  none — no branch is tagged out of the merge queue
rc=0
merge-backlog audit (report-only): scanned=0 submitted=0 duped=0 skipped=0 failed=0
rc=0
```

**结论 (a)**：腿在，契约在，输出在。`scanned=0` 是"没有未合并分支"，不是"路径断了"。

### 2.2 没有任何常驻件在驱动它（peerbox）

```bash
ssh peerbox 'ps -eo pid,etime,args | grep -i "gopi merge" | grep -v grep; echo rc=$?;
           ps -eo pid,etime,args | grep gopi | grep -v grep | wc -l;
           crontab -l | grep -i gopi; systemctl --user list-timers --all;
           ls -la ~/.gopi/merge/'
```

原始输出：

```
peerbox
Mon Oct 19 13:48:13 UTC 2026
=== gopi merge processes ===
=== rc=1 ===
--- worker/task serve ---
```

```
--- crontab:
# Evennia Auto-Development Flywheel
# Runs every 6 hours: 00:00, 06:00, 12:00, 18:00 UTC
--- systemd timers:
NEXT LEFT LAST PASSED UNIT ACTIVATES
0 timers listed.
--- merge lock: 0
--- gopi procs: 0
```

**结论 (b)**：**零常驻驱动**。这正是 `cmd/agent/merge_tick_retired.go` 头注释的原文主张 —— *"WHAT DRIVES UNATTENDED MERGING NOW: NOTHING, BY DESIGN… An operator sweeps the pile by running `gopi merge`; that is the whole mechanism."* 实测与注释一致（注释不是证据，这条实测才是）。

### 2.3 真流量：clone 通道启用日之后为 0

这是本条最关键的一组测量。peerbox 的账本（`~/.gopi/memory.db`）：

```bash
ssh peerbox 'DB=~/.gopi/memory.db;
 sqlite3 "$DB" "select strftime(\"%Y-%m\",created_at,\"unixepoch\") m,count(*) from tasks
                 where summary like \"[auto] You are the merge run%\" group by m order by m desc";
 sqlite3 "$DB" "select datetime(created_at,\"unixepoch\")||\" \"||status from tasks
                 where summary like \"[auto] You are the merge run%\" order by rowid desc limit 3";
 sqlite3 "$DB" "select count(*) from tasks where summary like \"[auto] You are the merge run%\"
                 and created_at > strftime(\"%s\",\"2026-10-18 00:00:00\")"'
```

原始输出：

```
2026-10|416
2026-09|174
2026-10-17 22:17:22 completed
2026-10-17 20:01:45 completed
2026-10-17 13:17:52 completed
0
```

**结论 (c)**：merge pass 的**历史**流量是真实的（2026-09 174 次、2026-10 416 次），最后一次是 `2026-10-17 22:17:22`；**2026-10-18 00:00 之后 0 次** —— 而 `devworkspace.go` 头注释记录的 clone 通道启用日是 **2026-10-18**。交付路径换掉的那天，merge pass 的流量归零，两件事在同一天。

同一账本上另外两族（merge 体系的副产品行）也同向：

```
=== merge-backlog 家族行（merge <%branch> into master）===
10 条，最新 1792104751 = Thu Oct 15 22:52:31 UTC 2026（cancelled）
=== resolve merge conflict 家族行 ===
106 条，最新 5296 = 2026-10-17 22:17:04（cancelled）
```

本机账本（`gopi task list` 与 python sqlite 读同一库）：

```bash
python3 -c "..."  # 见下方输出
```

原始输出：

```
[本机] pending 总数: 4
[本机] merge-run 行总数: 0
[本机] merge-run 最近3行: []
[本机] merge-backlog 行: 0
[本机] resolve-merge-conflict 行: 21
[本机] 最新5行: [('2026-10-19 12:39:55', 'failed', '[auto] 交付没有落地…'), …]
```

**结论 (d)**：本机的账本里 **merge-run 行一条也没有**，pending 4 条没有一条属于 merge 家族。本机是 pure clone 通道节点（`dev_repo=peerbox:/home/user/work/gopi`，`workspace_root=/home/user/workspace`，本会话的工作树就是 `workspace/<task-id>`）。

### 2.4 交付仓库里没有东西等着被合并

```
ssh peerbox 'cd /home/user/work/gopi && git branch --no-merged master | wc -l && git log --oneline -1'
→ 0
→ 5a03e30 CONFIG.md: re-point two sweep-worktrees anchors after the symbol deletion
```

```
本机 /home/user/work/gopi: git branch -a | wc -l → 3（* master / remotes/origin/HEAD / remotes/origin/master）
```

**结论 (e)**：两台机器的交付仓库都是**干净的 master + 零未合并分支**。merge 体系要处理的对象（"一堆待合并的分支"）此刻不存在。

### 2.5 Q2 判定

**"腿活着，消费者是操作员本人，实测零流量。"**

- **有消费者**：`gopi merge --status` / `gopi task audit-merges` / `gopi task merge-execute` 三个 CLI 面都可达、都 rc=0；`mergeBacklogFamilyBranch` 有非 merge 的读者（`archived_branch_retirement.go:20`、`archive_row_reconcile.go:22`）；`RebuildAfterMergeMergeset` 被 `rebuild.go:171` 调用。
- **零流量**：2026-10-18 起 0 次 merge pass、0 个常驻驱动、0 个未合并分支。
- 因此 Q2 **不构成"无消费者"**，只构成"当前不需要每轮跑"。→ **不能删，只能冻结 + 瘦身候选。**

---

## 3. Q3 — clone 通道遇到真冲突时现状是否可接受

### 3.1 真冲突实验（两台 clone 改同一行）

这是"真冲突会不会正好需要 merge 体系"的直接检验。用裸仓库模拟 origin，两个 clone 各改同一行：

```bash
R=$(mktemp -d); cd $R; git init -q --bare origin.git
git clone -q origin.git a; git clone -q origin.git b
# a: 改成 A-WINS 并 push
cd $R/a; printf 'line1\nA-WINS\nline3\n' > f.txt; git commit -qam A; git push -q origin HEAD:master
# b: 改成 B-WINS，然后跑 clone 通道的那一步 pull --rebase
cd $R/b; printf 'line1\nB-WINS\nline3\n' > f.txt; git commit -qam B; git pull --rebase origin master
```

原始输出（stderr 一并保留）：

```
--- git pull --rebase origin master ---
Auto-merging f.txt
CONFLICT (content): Merge conflict in f.txt
From /tmp/mergeq3.QrWD/origin
 * branch            master     -> FETCH_HEAD
   39e30f9..a08f131  master     -> origin/master
Rebasing (1/1)error: could not apply 97ae3a6... B
hint: Resolve all conflicts manually, mark them as resolved with
hint: "git add/rm <conflicted_files>", then run "git rebase --continue".
hint: You can instead skip this commit: run "git rebase --skip".
hint: To abort and get back to the state before "git rebase", run "git rebase --abort".
Could not apply 97ae3a6... B
```

真冲突**确实存在**：`CONFLICT (content)`、rc≠0、rebase 停在半途。

### 3.2 第二个失败形态：过期 clone 直接 push

```bash
cd $R/a; echo a1>a1; git add a1; git commit -qm a1
git push origin HEAD:master   # 不 pull，clone 已过期
```

原始输出：

```
push rc=1
 ! [rejected]        HEAD -> master (fetch first)
error: failed to push some refs to '/tmp/cloneff.3ZSW/o.git'
hint: Updates were rejected because the remote contains work that you do not
hint: have locally. … 'git pull' before pushing again.
```

### 3.3 clone 通道怎么处理这两种形态（读代码 + 跑它自己的测试）

`internal/devworkspace/devworkspace.go:521-533`：

```go
func pullRebase(ctx context.Context, dir, branch string) (string, error) {
	out, err := runCommand(ctx, dir, gitBinary, "pull", "--rebase", "origin", branch)
	if err == nil { return out, nil }
	// A CONFLICT leaves the tree mid-rebase, which would poison every later
	// attempt of this row. Abort it and let the caller report by name: the
	// content decision belongs to the agent, not to this loop.
	if aborted, aerr := runCommand(ctx, dir, gitBinary, "rebase", "--abort"); aerr == nil {
		return out + aborted, err
	}
	return out, err
}
```

`AttemptDelivery` 把它变成 `Blocker{Kind: BlockerKindPull}`（`devworkspace.go:378-383`），`finish()` 再把 blocker **交给一个 agent 轮次**（`cmd/agent/devworkspace_channel.go:129-160` 的注释与实现）；push 竞态走 `MaxPushAttempts` 的有界重试（0.5s/2s/8s + jitter），耗尽后写 brake 记录、**保留工作区不删**。

跑它自己的两条测试（真跑，非声明）：

```bash
go test ./internal/devworkspace -run 'Conflict|Pull' -v
```

原始输出：

```
=== RUN   TestLoopPushesAfterAPullRebase
    devworkspace_test.go:144: clone-channel: attempt 1/3: pull --rebase origin master
    devworkspace_test.go:144: clone-channel: attempt 1/3: build via the declared command
    devworkspace_test.go:144: clone-channel: attempt 1/3: push
    devworkspace_test.go:144: clone-channel: attempt 1/3: push accepted
--- PASS: TestLoopPushesAfterAPullRebase (0.07s)
=== RUN   TestAttemptDeliveryReportsAPullConflictAsABlocker
    devworkspace_test.go:144: clone-channel: attempt 1/3: pull --rebase origin master
--- PASS: TestAttemptDeliveryReportsAPullConflictAsABlocker (0.07s)
PASS
ok  	github.com/nick/gopi/internal/devworkspace	0.149s
```

### 3.4 Q3 判定

**可接受。** clone 通道对真冲突的处置是**完整且自足**的：

| 冲突形态 | clone 通道的处置 | 需要 merge 体系吗 |
|---|---|---|
| 内容冲突（pull --rebase 撞车） | `rebase --abort` + `Blocker{pull}` + 一个 agent 轮次决定 | **不需要** |
| push 竞态（别人先落地） | 有界重试 3 次（0.5/2/8s），耗尽写 brake、保留工作区 | **不需要** |
| 交付后 master 要 build | 通道在 **push 之前**就 build 过（`build` 是 push 的前置，declared 且非猜测） | **不需要**（这是 merge 的 build gate 想做的事，但通道在更早的位置做了） |

**唯一仍属 merge 体系独有的能力**是"把**一条已经存在、不在任何 workspace 里的**分支合进 master"，以及"合并后整树 `go build ./...` + `go vet ./...` 的 post-pass 校验"。clone 通道下这两件事都不会自动发生 —— 因为通道的产出**直接就是 origin 上的一条 fast-forward**，没有"待合并分支"这个中间态。

---

## 4. Q4 — Rule 26 是否依赖 merge

**Rule 26 原文**（`/home/user/work/gopi/GOPI.md:579`）：

> **Rule 26: An Incomplete Fix Is Repaired by Dispatching Again — Never by Waiting** … **Practice**: when a fix turns out to be incomplete, **dispatch a new task** … Corollary — an incomplete fix must be **narrow, never wrong**.

**机制本体**（不经过任何 merge 代码）：

```bash
grep -n "func TaskSubmit(" internal/tools/tasks.go
grep -rn "TaskSubmit(TaskSubmitOptions" --include=*.go . | grep -v _test
```

原始输出：

```
6475:func TaskSubmit(opts TaskSubmitOptions) (string, error) {

./internal/tools/merge_config_anchor_gate.go:365
./internal/tools/task_merge.go:1254
./internal/tools/merge_target_doc_gate.go:421
./internal/tools/rebuild_vet_mergeset.go:856
./internal/tools/agency.go:18
./internal/tools/task_adoption.go:557
./internal/tools/verify_gate.go:855
./internal/tools/task_merge_backlog.go:199
```

**方向是清楚的**：`TaskSubmit` 是 Rule 26 的落地机制，它在 `tasks.go`；8 个非测试调用点里 4 个**恰好落在 merge 代码里** —— 这说明 **merge 依赖 TaskSubmit**，不是 Rule 26 依赖 merge。`agency.go`、`task_adoption.go`、`verify_gate.go`、`task_merge_backlog.go` 这 4 个更能说明问题：它们与 merge 执行链无关。

**反向检查**（merge 代码里提到 Rule 26 的地方）：

```bash
grep -rn "Rule 26" --include=*.go internal/tools/merge* internal/tools/task_merge* cmd/agent/merge* | wc -l
```

原始输出：`4`，全部是**注释**：

```
internal/tools/merge_revert.go:67:      // surface — nothing in a revert may touch a tag (Rule 26).
cmd/agent/merge_preland_gate.go:93:     // WHAT IT HONESTLY DOES NOT COVER (Rule 26's corollary …)
internal/tools/merge_pass_compile_only_test.go:23
cmd/agent/cli_tasks_merger_help_note_reader_gate_test.go:46
```

**Q4 判定：Rule 26 不依赖 merge。** merge 代码里没有一条逻辑、门禁或测试把 Rule 26 的实现挂在自己的执行路径上；唯一的耦合是"merge 在需要另派任务时调用 `TaskSubmit`"，而这是**单向依赖**（merge → 派发机制），删 merge 只会减少 `TaskSubmit` 的一个调用点，不会破坏它。

---

## 5. 裁决

### 5.1 判定：**保留（部分保留 + 改造）**，本条不删代码

判据表（每条对应上面的实测）：

| 条件 | 实测 | 结论 |
|---|---|---|
| Q2 要求"无消费者" | `gopi merge --status`/`audit-merges`/`merge-execute` 三面 rc=0；`mergeBacklogFamilyBranch` 有非 merge 读者；`RebuildAfterMergeMergeset` 被 `rebuild.go` 调用 | **有消费者** → 删除条件不成立 |
| Q3 要求"clone 通道不需要" | 通道自带 pull-conflict → abort+blocker、push 竞态 → 有界重试、push 前 build；两条测试 PASS | **通道确实不需要 merge 参与交付** |
| 两个条件必须同时成立才能删 | 前者不成立 | **保留** |

这个判定是合法的、也是被要求的：载荷写明"判定'保留'是合法结果"。**我不删的理由不是保守，是 Q2 的证据说了"消费者还在（就是操作员本人），只是最近一天没用"。**

### 5.2 第四节的"新职责"（保留下来它现在真正承担的职责）

merge 体系在 clone 通道世界里剩下的职责，只有三条，且都是**操作员驱动、按需**：

| 保留的职责 | 面 | 谁用 | 为什么不归 clone 通道 |
|---|---|---|---|
| **① 只读合并面板** | `gopi merge --status`（`mergeStatusFlag`，`merge_run.go:721-729`，取锁前的只读臂） | 操作员 | 通道只知道自己那一个 workspace；它无法回答"全仓还有几条分支没落地" |
| **② 兜底落地面** | `gopi merge` / `gopi merge --fast`（`mergeRun`/`mergeRunLoop`） | 操作员手动 | 通道的产出是 ff 推送；**任何绕开通道的未合并分支**（手工分支、旧节点遗留、冲突后保留的 workspace 里的提交）只能靠它落地 |
| **③ 合并后整树校验** | `--no-verify` 的反面：post-pass `go build ./...` + `go vet ./...`，失败则 withdrawal（`WithdrawMergePass`，`merge_run.go:2099`） | ①② 的执行体 | 通道的 build 是**单仓声明命令**；它没有"合并后整树 + 撤回"这条臂 |

被改造（冻结，不再扩展）的部分：**不再有任何常驻驱动**（`merge_tick_retired.go` 已是事实），merge 只作为操作员的显式命令存在。

### 5.3 执行步骤（本条已执行的部分 = 只有这份文档）

本条**不执行代码删除**。已经做过的动作只有两条，都可复核：

1. 写本文件（`devdoc/worktree-retire-merge-verdict.md`）。
2. 在 `devdoc/DOC-INDEX.tsv` 追加一行分类（`historical`），否则 `internal/standards` 的 docclass 门禁会因"文档无分类行"变红。实际落地的行（`git diff` 实测，`1 0`）：

```
devdoc/worktree-retire-merge-verdict.md	historical	historical (2 standing / 2 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority
```

（第一次 append 之后流里出现过 `docclass_gate_test.go:91: duplicate row for devdoc/worktree-retire-merge-verdict.md` —— 是同一行被重复写入，已删至单行。这一条 `git diff --numstat` 现在是 `1 0`，可复核。）

**未来若要真删（本条不做，作为后续独立条的输入）**，正确的切开顺序是（依据 §1.4 与 §1.2 的实测，不是猜测）：

1. **先删真零读者的 8 个声明名**（`VerifyMergePass`、`MergeCeilingRework`、`MergesetRebuild`、`RebuildVetMergeset`、`MergedCleanup`、`TaskMergedCleanup`、`MergeRefuseAnchor`、`FinishCleanupOwnerRowForWorktree`）—— 注意这条必须先做名字核对：`merged-cleanup` 的真入口是 `MergedCleanupSweep`（n=2，活），删了 `MergedCleanup` 不等于删了动词。
2. **再删 B 组 436 行**（`merger_retired.go` 的拒绝面）：前提是 `main.go:659` 的 `isMergerSubcommand` 一并删 —— 那是一个**入口契约变更**，不是死代码清理。
3. **A/C/D/K 四组只能在"操作员明确放弃合并面板"之后动**，因为 §5.2 的三条职责都落在它们身上。
4. **E/G/H 不能单独删**：E 的家族解析器有非 merge 读者（`archived_branch_retirement.go`、`archive_row_reconcile.go`），H 的 `RebuildAfterMergeMergeset` 被 `rebuild.go:171` 调用 —— 删 E/H 会先把**非 merge 功能**弄坏。

每一步都必须带调用者普查表 + `make test-budget PKGS='./internal/tools ./cmd/agent'` 绿（串行，不并发跑两个 suite）。

### 5.4 风险

| 风险 | 说明 | 缓解 |
|---|---|---|
| **把"零流量"读成"零消费者"** | 2026-10-18 起 0 次 pass 很容易被下一步误当成可以删 | 本文 §2.1 与 §5.2 已经把"入口活着 + 职责仍在"写死；任何删除都必须重新跑 §2.1 的三条命令 |
| **凭名字 grep 删 → 误删活入口** | `MergedCleanup` n=0 而 `MergedCleanupSweep` n=2；`VerifyMergePass` n=0 但撤回能力真的存在（`WithdrawMergePass` n=1） | 删除前逐符号做"名字 → 真入口"配对（§1.4 的教训） |
| **E/H 组有非 merge 读者** | `mergeBacklogFamilyBranch`、`RebuildAfterMergeMergeset` | 删 E/H 之前先改这两个读者，而不是连着读者一起删 |
| **测试体量** | 99 个测试文件 / 30,800 行；实现:测试 = 1:1.18 | 任何删除都比它看起来贵 1.2 倍 |
| **留下半死状态** | 删掉 pass 但留下 `--status`（或反过来）会让操作员拿到不一致的面 | 按 §5.3 的 4 步顺序，每步都保持 build+test 绿 |

---

## 6. 五条系列总结

### 6.1 系列在 git 里的实际形态

```bash
git log --oneline 964b240..HEAD | wc -l        # 33
git diff --shortstat 964b240..HEAD             # 109 files changed, 8576 insertions(+), 1472 deletions(-)
git show --shortstat --format="" fcbd0b7       # 12 files changed, 57 insertions(+), 1219 deletions(-)
git show --shortstat --format="" def04cf       # 2 files changed, 23 deletions(-)
```

原始输出：

```
--- commits since initial snapshot ---
33
--- 2/5 (fcbd0b7) shortstat ---
 12 files changed, 57 insertions(+), 1219 deletions(-)
--- def04cf shortstat ---
 2 files changed, 23 deletions(-)
--- whole series diff 964b240..HEAD ---
 109 files changed, 8576 insertions(+), 1472 deletions(-)
```

**重要限定**：这个仓库的历史从 `964b240`（2026-10-18，"initial: source snapshot at 0592c9ae4, no history"）开始，**系列 1、3、4 的提交不在这个仓库的 git 历史里**（它们落在上一代仓库）。所以 33 个提交里有 2 条能明确归本系列的名字：

```
fcbd0b7 worktree retirement 2/5: retire the finish-cleanup verb face and its dead close-out shell
def04cf worktree sweep: drop four declaration-only symbols with zero readers
```

净删除量 **-1,242 行**（fcbd0b7 的 -1219 与 def04cf 的 -23）是本系列在这棵树上可量化的沉淀。

### 6.2 worktree grep 计数（系列前后对比）

```bash
for rev in 964b240 fcbd0b7^ fcbd0b7 5a03e30; do
  nf=$(git grep -l -i worktree $rev -- '*.go' | wc -l)
  nl=$(git grep -i -c worktree $rev -- '*.go' | awk -F: '{s+=$NF} END{print s}')
  allf=$(git ls-tree -r --name-only $rev | wc -l)
  namef=$(git ls-tree -r --name-only $rev | grep -ci worktree)
  printf "%s go-worktree files=%s lines=%s tree files=%s name-with-worktree=%s\n" $rev $nf $nl $allf $namef
done
```

原始输出：

```
964b240   go-worktree files=738  lines=12133  | tree files=6238  | name-with-worktree=129
732a4f3^  go-worktree files=746  lines=12207  | tree files=6253  | name-with-worktree=129
732a4f3   go-worktree files=752  lines=12288  | tree files=6258  | name-with-worktree=129
5a03e30   go-worktree files=748  lines=12254  | tree files=6268  | name-with-worktree=130
```

| 指标 | 964b240（系列开始） | 5a03e30（系列 5/5 前） | 变化 |
|---|---|---|---|
| `*.go` 中提及 worktree 的**文件** | 738 | 748 | **+10** |
| `*.go` 中提及 worktree 的**行** | 12,133 | 12,254 | **+121** |
| 文件名带 worktree 的文件 | 129 | 130 | **+1** |
| 仓库文件总数 | 6,238 | 6,268 | +30 |

**这三个数字是"上升"的，必须说清楚为什么，而不是粉饰**：

- `732a4f3`（系列 1，判据对齐）**新增**了"三处判断统一到一个 `devworkspace.ResolveGroundState`"的注释与语义 —— 修 bug 会**增加**对 worktree 的提及（它要说明 clone workspace 不是 linked worktree），这不是退化。
- 真正的删除发生在 `fcbd0b7`（-1,219 行 / -12 文件）与 `def04cf`（-23 行）—— 也就是 `fcbd0b7^ → fcbd0b7` 那一格：go-worktree **752 → 748 文件、12,288 → 12,259 行**。
- 从 `fcbd0b7` 到 `5a03e30` 又有 **-5 行**（12,259 → 12,254），来自 `3c6dad3` 的锚点重指与 `def04cf` 的四个零读者符号。

**因此：本系列的量化成果不是"worktree 相关代码减少了"，而是"删掉了能删的 1,242 行，同时把三条互相矛盾的 ground 判断收敛成一条"。** 前者可数，后者不可数但更值钱 —— 732a4f3 修的是"clone workspace 被误判为 gone、`--resume` 跑不通"，那是系列 1 存在的理由。

### 6.3 五条各自的交付

| # | 主题 | 性质 | 本仓库可见的落地 | 结果 |
|---|---|---|---|---|
| 1 | 判据对齐（ground 存活判断） | 修 bug | `732a4f3` ground: ONE judge 背后两处拒绝；`959f228` 锚点重指 | 完成（`--resume` 不再把 clone workspace 判为 gone） |
| 2 | finish-cleanup 全链退役 | 删代码 | `fcbd0b7`（-1,219 行 / -12 文件）+ `c94c746`、`8096e8f` 清注释 | 动词面退役，实现在 `task_finish_cleanup.go` 仍在（供内部链调用） |
| 3 | sweep / release / roots 退役 | 删代码 | 上一代仓库；本树可见 `def04cf`（四个零读者符号，-23 行）+ `5a03e30`（锚点重指） | 部分退役（`worktreeroots` 保留，clone 通道仍用它算 workspace 位置） |
| 4 | 调用者普查与"只删零读者"裁决 | 调查 | `fa64424`（`devdoc/worktree-lifecycle-census.md`）、`1b8ecef`（分类行） | 完成，判定为"保留并写明理由" |
| 5 | **merge 体系裁决（本条）** | 调查 | 本文件 | **保留（部分保留 + 改造）**，理由是消费者仍在（§2.1）而流量为零（§2.3） |

### 6.4 系列的整体结论

**"worktree 退场"退的不是 worktree 这个词，退的是"每个 agent 在一棵本地共享 master 里干活"这件事。** 五条走下来，同一个判断被三次独立验证：

1. 删得掉的（finish-cleanup 动词面、四个零读者符号、sweep 的一部分）都删了，量是 1,242 行。
2. 删不掉的（`worktreeroots` 算 workspace 位置、merge 的只读面板与兜底落地）都**写清了留下的理由**，而不是留下来等它烂。
3. 最值钱的一条不是删除，是系列 1 那条**判断收敛** —— 它把"这个 workspace 还在不在"从三套互相矛盾的判据收敛成 `devworkspace.ResolveGroundState` 一个判定，那才是 `--resume` 能跑通的原因。

merge 体系在这三者之间的位置很清楚：**它不属于"删得掉"的那一类（消费者是操作员本人，实测仍在），也不属于"必须留"的那一类（clone 通道已经自带冲突处置与 build 前置）。它是"冻结 + 按需"的第三类** —— 没有常驻驱动，操作员要落地就显式跑一次。这就是本条写在 §5.2 的"新职责"。

---

## 7. 验证（实测，含一条**先说红**的诚实账）

本条**不动任何代码**，所以验收不是"test-budget 绿"，而是"我这一行文档没有把任何门禁弄红"。为此跑了文档门禁，并在**同一棵树的 HEAD 上另建一个 worktree 做基线对照**（不含本文件、不含我的索引行）：

```bash
# 当前树（含本文件 + 我的索引行）
go test ./internal/standards -run 'TestDocManifestCoversEveryDevdocFile|TestDocClassBannerMatchesManifest'
# 基线对照（git worktree add --detach /home/user/workspace/_basecheck-merge HEAD，然后同一条命令）
```

**当前树**的原始输出：

```
--- FAIL: TestDocManifestCoversEveryDevdocFile (0.18s)
    docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2626 rows cover 2507 files under devdoc/ (classes: map[historical:1898 index-meta:1 normative:715 roadmap:12])
--- FAIL: TestDocClassBannerMatchesManifest (0.11s)
    docclass_gate_test.go:223: devdoc/DOC-INDEX.tsv: 1564 markdown docs of a bannered class carry their banner (4 banner defects)
FAIL
FAIL	github.com/nick/gopi/internal/standards	1.756s
```

**基线对照**的原始输出（工作树里没有我的文件 —— `ls devdoc/worktree-retire-merge-verdict.md` → `No such file or directory`）：

```
--- FAIL: TestDocManifestCoversEveryDevdocFile (0.17s)
    docclass_gate_test.go:104: devdoc/archive-reclaim-reason.md: no row in devdoc/DOC-INDEX.tsv — …
--- FAIL: TestDocClassBannerMatchesManifest (0.14s)
    docclass_gate_test.go:223: devdoc/DOC-INDEX.tsv: 1563 markdown docs of a bannered class carry their banner (4 banner defects)
```

**"no row" 缺陷条数两边都量一次**（这是唯一能被"加一行"影响的量）：

```bash
grep -c "no row in"      # 当前树 25 ／ 基线 25
```

**判定**：`internal/standards` 在这个基线上**本来就是红的**（25 个未分类文档、4 个 banner 缺陷，两边一致），**与本文件无关**。第一遍 `make test-budget` 流里我的文件只出现过一次，而且是 `docclass_gate_test.go:91: duplicate row` —— 那是我 append 前误留的重复行（第 2636 行），已删，现 `git diff --numstat devdoc/DOC-INDEX.tsv` 为 `1 0`。

那条既有红属于别人：`TestRepoRootGopiDocIsUntrackedButReadable` 说 `GOPI.md` 在本树里既未跟踪也不存在（clone 通道工作树不带 GOPI.md）。按 Rule 26 的推论应另派一条，**不由本条顺手修**（那是"宽而错"）。

**工作树状态**：

```bash
git status --porcelain
→ ?? devdoc/worktree-retire-merge-verdict.md
  M devdoc/DOC-INDEX.tsv
```

**收尾（提交后复测，2026-10-16）**：上面的两条未提交状态是写入当时的状态；提交后实测已经干净：

```
$ git status --porcelain
（空）
$ git diff --numstat 5a03e30..HEAD
1	0	devdoc/DOC-INDEX.tsv
624	0	devdoc/worktree-retire-merge-verdict.md
$ git log --oneline -1
0252abe worktree retirement 5/5: the merge subsystem keeps a job — decision doc with the measurements
$ git worktree list
/home/user/workspace/01a150f4-298a-7b96-9346-d861551fdff8  0252abe [master]
```

基线对照用的 `_basecheck-merge` worktree 已 `git worktree remove`，`git worktree list` 里不再出现（只剩测试框架自己的 prunable 临时树）。

### 7.1 补测：`make test-budget PKGS='./internal/tools ./cmd/agent'`（2026-10-19 22:1x，串行单跑）

载荷对 `test-budget` 的要求挂在"若判定删除"分支上（判定=保留，零代码改动），但为了不让"没跑"被读成"不敢跑"，补跑了一次**串行**的整批：

```
$ make test-budget PKGS='./internal/tools ./cmd/agent'
stamp: 2026-10-19T22:15:44+08:00 host=hosta \
  pkg=./internal/tools ./cmd/agent budget=45m elapsed=525s \
  load_start=8.24 14.98 12.11 load_end=3.84 6.41 9.00 cores=32 exit=1 outcome=fail \
  stream=run-1792419344-3057798.json
make: *** [Makefile:75: test-budget] Error 1
```

**它是红的 —— 而这批红是不是我弄的，用基线逐条量。** 从 stream 里取全部失败用例，在 `git worktree add --detach /home/user/workspace/_basecheck-merge2 5a03e30`（**不含本文、不含我的索引行**）上跑同一批 `-run`：

| 失败用例 | HEAD（0252abe+） | 基线 5a03e30 | 归因 |
|---|---|---|---|
| `TestAssertionTotalityAuditGate` (tools) | FAIL | FAIL | 既有 |
| `TestSelfDeliveryPushesRemovesAndForceDeletes` (tools) | FAIL | FAIL | 既有（`ssh: Could not resolve hostname peerfixture`） |
| `TestSelfDeliveryFailureKeepsTheRefAndNamesTheTarget` (tools) | FAIL | FAIL | 既有（同上） |
| `TestSelfDeliveryIsIdempotentForTheWorkerPath` (tools) | FAIL | FAIL | 既有（同上） |
| `TestSessionResumeFlagRealBinaryConsumesTheKnob` (agent) | FAIL | FAIL | 既有 |
| `TestPublishedHelpPagesStayWithinTheirUsageBudget` (agent) | FAIL | FAIL | 既有（`gopi task -h` 204 行 / 预算 195） |
| `TestManSourcesMatchTheirHelpPage` (agent) | FAIL | FAIL | 既有（`man/gopi-merge.md` SYNOPSIS 漂移） |
| `TestNoTestDrivesTheRealBinaryWithARetiredWord` (agent) | FAIL | FAIL | 既有（3 处驱动 `proxy` 退役词） |
| `TestGOPIMDRule6aDefersToTheGroundTheRunnerSupplied` (agent) | FAIL | FAIL | 既有（**本树无 `GOPI.md`**，基线同样无） |
| `TestGOPIMDRule6aClauseDoesNotPrescribeAForceSpelling` (agent) | FAIL | FAIL | 既有（同上） |
| `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` (agent) | FAIL | FAIL | 既有（`main.go:635` vs `:641`） |

原始输出（基线 tools 包尾）：

```
--- FAIL: TestAssertionTotalityAuditGate (0.59s)
--- FAIL: TestSelfDeliveryPushesRemovesAndForceDeletes (0.11s)
--- FAIL: TestSelfDeliveryFailureKeepsTheRefAndNamesTheTarget (0.11s)
--- FAIL: TestSelfDeliveryIsIdempotentForTheWorkerPath (0.11s)
FAIL
FAIL	github.com/nick/gopi/internal/tools	1.921s
```

原始输出（基线 cmd/agent 包尾）：

```
--- FAIL: TestPublishedHelpPagesStayWithinTheirUsageBudget (2.64s)
--- FAIL: TestManSourcesMatchTheirHelpPage (0.13s)
--- FAIL: TestNoTestDrivesTheRealBinaryWithARetiredWord (1.68s)
--- FAIL: TestGOPIMDRule6aDefersToTheGroundTheRunnerSupplied (0.00s)
--- FAIL: TestGOPIMDRule6aClauseDoesNotPrescribeAForceSpelling (0.00s)
--- FAIL: TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas (0.00s)
FAIL
FAIL	github.com/nick/gopi/cmd/agent	4.833s
```

**11 条失败，HEAD 与基线 11 : 11 逐条一致，零条只在本树出现。** 所以我这一行文档没有把任何门禁从绿变红；`test-budget` 在这个基线上本来就是红的（且这些红大半是 clone 通道工作树的已知缺口：无 `GOPI.md`、无 `peerfixture` 主机名解析、man 源漂移）。

诚实记账：这批红不属于本条，但也没人修 —— 按 Rule 26 应各自另派一条（尤其 `TestGOPIMDRule6a*` 与 `TestNoTestDrivesTheRealBinaryWithARetiredWord` 是 clone 交付通道自身的门禁缺口）。

**收尾**：基线 worktree `_basecheck-merge2` 用完即 `git worktree remove`；本条 `git status --porcelain` 仍为空。
