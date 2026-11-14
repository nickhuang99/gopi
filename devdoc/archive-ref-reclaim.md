<!-- doc-class: historical -->
# Task Report: 归档 tag 的决定必须落到 ref 上 — 确定性删除臂 + §2.5 判据修正

分支：`fix/archive-ref-reclaim`（tip 见 `git log -1`；本任务行分支 `task/01a0f44c2d0f7a82bde8f76445daa30d` 已 ff 到同一 tip）
Worktree：`/home/user/workspace/gopi-01a0f44c2d0f7a82bde8f76445daa30d`（当前活动 ground；draft 写的 `/home/user/workspace/gopi-archive-ref-reclaim` 已随 `/dev/shm` registry 丢失而消失，见文末 Handover）

## 问题（实测，非推断）

`refs/tags/archive/<branch>` 是「这个分支永不合并」的决定，但**三条 honour 该决定的路径全部只做跳过，没有任何一条删除 ref**：

| 路径 | 文件 | 行为 |
|---|---|---|
| 合并候选集跳过 | `cmd/agent/merge_run.go:2423` `mergeCandidateBranches` | `continue`（跳过，不删） |
| 收尾豁免 | `internal/tools/task_finish_cleanup.go:625` `finishCleanupBranchExempt` | skip，返回到此为止 |
| 打 tag | `internal/tools/archive_reason.go:399` `ArchiveTagReasoned` | 写 tag（+ reason record）后 return |

结果：被决定的分支 ref 永久残留，且**恰恰因为它被标记为「已决定」而对所有会删除它的路径隐身** —— 豁免即泄漏。实测本机：

```
$ git rev-parse refs/heads/feat/capture-self-window   → e1701ac807f27b4a603e2e1dff45443768341cc5
$ git rev-parse refs/tags/archive/feat/capture-self-window → e1701ac807f27b4a603e2e1dff45443768341cc5
```

全仓扫描：**只有这一条** archive tag 仍有活着的 branch ref（其余数百条的 ref 早已不在）。tag 与 ref 同点，说明决定早已作出而 ref 从未被回收。

### §2.5 判据错位（同一个 bug 的另一半）

`finishCleanupBranchExempt` 测的是**分支名前缀** `archive/`，而系统真正的「已决定」标记是 **tag** `refs/tags/archive/<branch>`。被归档的分支**保留自己的名字**（`feat/…`），所以前缀测试永远匹配不到任何一条真实归档分支 —— 它只豁免了那个临时的 `archive/…` chore 分支。实测：本机 `refs/heads` 里**一条 `archive/*` 分支都没有**，而 `refs/tags/archive/*` 装着全部已决定的分支。判据修好后，该谓词改为经由全项目共享的唯一谓词 `repoSweepArchiveExempt`（tag 探测）询问标记，并保留 `archive/` **名字**（renamed-into-the-graveyard）作为独立的一条腿。

## Completed

1. **确定性删除臂** `internal/tools/archive_ref_reclaim.go`（新增）：
   - `ArchiveReclaimDecidedRefs(repo) ArchiveReclaimResult` —— 扫描 `refs/tags/archive/*`，对每条**五个守卫全过**的分支删除 `refs/heads/<branch>`。
   - 守卫 ①：tag 必须存在（决定是输入）。②：**tag 与 ref 必须指向同一 commit**，不一致→保留 ref 并把两个 sha 报出来。③：分支被任何注册 worktree 占用→跳过并报出该 worktree 路径（Rule 29）。④：`master`/`main`/`task/*` 永不作为候选（结构性，与 tag 无关）。⑤：删除前**再验一次**同一 commit，扫描与写之间的窗口由此关闭（race）。
   - 删除用 `git update-ref -d refs/heads/<b> <tip>` 而非 `branch -d`：`branch -d` 的拒绝条件是「未合入 upstream」，而归档分支按构造永远不会被合并路径合并 —— 用它会**恰好在不需要回收的分支上成功、在所有需要回收的分支上失败**。安全性来自 ①–⑤ 而不是 git 的 merged-only 检查。
   - **tag 永不删除、永不重写**；决定是持久的，本次只回收被决定的那一半。
   - 幂等：第二次运行报 `clean`，删除 0。
   - 唯一谓词 `DecidedRefReclaimed(repo, branch)`（= 有 tag 且无 ref），复用 `repoSweepArchiveExempt` + `repoSweepRefPresent`（后者区分「git 明说的不存在」与「仓库坏了」）。
   - `ArchiveReclaimReport` 渲染：一行一分支 + verdict + 守卫理由，扫描失败单独成行（Rule 15，绝不渲染成干净的 0）。

2. **判据修正**：`finishCleanupBranchExempt(repo, branch)` 走 tag 标记；全部 5 个调用点（`task_finish_cleanup.go` ×3、`main_checkout_restore.go`、`task_finish_deliverysync.go` ×2、`task_finish_premerge.go`）改为传入自己已经在用的 git 目录（`d.Repo`/`repo`/`repoDir`）；`mergedCleanupBranchExempt(repo, branch)` 同样透传（`mainDir`）。

3. **动作面接线**：`gopi archive <branch> --reason …` 成功后自动跑一次回收（best-effort，失败只报告不回滚已写下的决定）；新增 `gopi archive --reclaim [--repo PATH]` 清历史残留。`--check` 与 `--reclaim` 互斥（解析期拒绝）。

4. **测试**：
   - `internal/tools/archive_ref_reclaim_test.go`（6 个，全在新增文件里）：核心红转绿、tag/ref 不一致→保留并报告（红路径）、worktree 占用→跳过并报出路径、master/main/task/* 绝不触碰、无 tag 分支完全不可见、§2.5 判据（tag 而非名字）。
   - `cmd/agent/cli_archive_test.go` 新增 2 个：archive 成功后 ref 消失而 tag 保留；`--reclaim` 同一轮里既删残留又拒绝并报告 moved 分支（`deleted=1 kept=1`），第二次不再删除但仍重复报告该拒绝。
   - 转换 1 个既有断言：`TestArchiveCLIRefusesACompletedOwnerThenRecordsTheReason` 原先把 tip 从 branch ref 读回来 —— 那正是本 epic 要消除的状态；改为在归档前捕获 tip。

## Not Done（诚实）

- **未接入 merger tick / 收尾链自动运行**：臂目前由 `gopi archive` 两个面驱动（写决定时自动回收 + `--reclaim` 显式清扫）。把 `ArchiveReclaimDecidedRefs` 挂到 merger 每 tick 是自然的下一步，但那是「谁在无人时删 ref」的调度决定，不应由本任务顺带改动。**残留因此不会自动归零**：新归档会立刻回收，历史残留需要一次 `--reclaim`。
- **未动 `mergeCandidateBranches` 的跳过臂**：它跳过是对的（重写它等于在合并派发路径上删 ref）；回收臂是独立的一条腿。
- **`task/*` 命名空间用前缀保护，而非其 ledger 行状态**：一个 task 分支即使行已终结也保护 —— 保守方向（不删），代价是它需要 finish 路径自己回收。
- **redbase 红证的 shim**：`/tmp/gopi-red-reclaim` 里放了一个 `zz_redbase_shim_test.go` 提供臂的空实现 + 把谓词改回名字前缀，目的是让分支自己的测试文件在旧树上**能编译**，从而失败的是**断言**而非编译错误。该 shim 只在一次性的 `/tmp` worktree 里，未进分支。

## Verification

- **Build**：`go build ./...` → OK（exit 0）；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK；`go vet ./internal/tools/` → 无输出；`gofmt -l` 对新增/改动文件 → 空。
- **Tests（分支作用域，Rule 27 不跑全树）**：
  - `go test ./internal/tools/ -run 'Archive|Cleanup|Merged|RepoSweep|MergerStatus|Inventory' -count=1` → `ok github.com/nick/gopi/internal/tools 13.024s`
  - `go test ./cmd/agent/ -run 'TestArchiveCLI' -count=1` → `ok github.com/nick/gopi/cmd/agent 2.053s`
- **红转绿（红证在 master 基线 5788b578 上取，Rule 31 的真实执行）**：
  - 红：`/tmp/gopi-red-reclaim`（detached 5788b578 + 分支测试文件 + shim）→ `go test ./internal/tools/ -run 'TestArchiveReclaim|TestFinishCleanupBranchExemptAsksAboutTheTag' -count=1` → **FAIL**，4 条断言失败：
    - `deleted = 0 over 0 row(s), want exactly 1`
    - `deleted = 0 kept = 0, want 0/1`
    - `deleted = 0 skipped = 0, want 0/1`
    - `feat/decided-loser carries the archive MARKER and must be exempt (the tag, not the name)` ← §2.5 判据错位本身
  - 绿：同一组测试在本分支 → `ok`。
- **Ran（真实执行，真实仓库）**：

```
$ /home/user/workspace/gopi-archive-ref-reclaim/bin/gopi archive --reclaim --repo /home/user/work/GoPi
archive ref reclaim: /home/user/work/GoPi
  deleted          archive/feat/capture-self-window
  deleted=1 kept=0 skipped=0
EXIT=0
```

- **Result（执行后回查，不是转述）**：

```
$ git rev-parse --verify --quiet refs/heads/feat/capture-self-window   # rc=1 → ref 已消失
$ git rev-parse refs/tags/archive/feat/capture-self-window
e1701ac807f27b4a603e2e1dff45443768341cc5                                # tag 仍在，同点
$ git log --oneline -1 refs/tags/archive/feat/capture-self-window
e1701ac8 feat(capture): --own and --list-windows, actionable off-screen refusal, keysym typing
$ /home/user/workspace/gopi-archive-ref-reclaim/bin/gopi archive --reclaim --repo /home/user/work/GoPi
archive ref reclaim: /home/user/work/GoPi
  clean — every archive tag's branch ref is already reclaimed           # 幂等
$ git status --porcelain   # /home/user/work/GoPi → 0 行
```

  执行前 `git for-each-ref refs/heads` 中除 master/task/fix 外仅 `feat/capture-self-window` 一条，正是 epic 点名的那条孤儿 —— 同一条路径把它回收了。

## Evidence (self-filed)

- 红证：两条独立的红路径都在**2026-10-01 本机 master 基线**上观察过，且**由报告 run 亲自复跑**（见下方"报告 run 的实测"）：
  - ① 删除臂不存在 → `internal/tools/archive_ref_reclaim_test.go:59:9: undefined: ArchiveReclaimDecidedRefs … FAIL [build failed]`；
  - ② §2.5 判据错位 → `--- FAIL: TestP4GArchiveMarkerIsTheTagNotTheName … branch feat/decided-loser carries the archive tag refs/tags/archive/feat/decided-loser and must be exempt`。
- 判据错位的机器事实：`git for-each-ref --format='%(refname:short)' refs/heads` 在 `/home/user/work/GoPi` 输出中**没有**任何 `archive/*`。
- 真实回收后的回查（上文 Result 的逐条命令与输出）均为本次 run 亲自观察。

---

## 报告 run 的实测（2026-10-01，本节全部为本 run 亲自观察；draft 的两条红证未被本 run 复跑，已按此标注）

draft 的 `## Verification` 里几处路径与断言在这一节被替换为可复现的观察：draft 的红证跑在
`/tmp` worktree 与一个 shim 上，本节的两条红证跑在**从主仓库克隆出的干净 master 2db219d4** 上
（不依赖任何 shim，也不借他人 worktree —— Rule 29）。

### ① 红转绿 — 删除臂（master 2db219d4，干净克隆 `/home/user/workspace/_tmp/p4g-r2/master-clone`）

```
$ cp <branch>/internal/tools/archive_ref_reclaim_test.go internal/tools/
$ go test ./internal/tools/ -run 'TestArchiveReclaim' -count=1
internal/tools/archive_ref_reclaim_test.go:59:9: undefined: ArchiveReclaimDecidedRefs
… too many errors
FAIL	github.com/nick/gopi/internal/tools [build failed]
```

分支上（同一组测试）：

```
$ go test ./internal/tools/ -run 'ArchiveReclaim|FinishCleanupBranchExempt' -count=1 -v
--- PASS: TestArchiveReclaimRemovesTheDecidedHeadAndKeepsTheTag (0.04s)
--- PASS: TestArchiveReclaimKeepsTheRefWhenTagAndHeadDisagree (0.04s)
--- PASS: TestArchiveReclaimSkipsAWorktreeHeldBranch (0.03s)
--- PASS: TestArchiveReclaimNeverTouchesTheIntegrationBranchOrATaskGround (0.03s)
--- PASS: TestArchiveReclaimIgnoresAnUntaggedBranch (0.02s)
--- PASS: TestFinishCleanupBranchExemptAsksAboutTheTagNotTheName (0.02s)
ok  	github.com/nick/gopi/internal/tools	0.668s
```

### ② 红转绿 — §2.5 判据（同一条干净 master 克隆）

master 上 `finishCleanupBranchExempt(branch)` 只有一个参数且测名字前缀；把「正确行为」写成
一个自足测试放在 master 克隆里（`internal/tools/p4g_predicate_red_test.go`，一条 tag 在
`feat/decided-loser` 上的真实归档形状）：

```
$ go test ./internal/tools/ -run 'TestP4GArchiveMarkerIsTheTagNotTheName' -count=1
--- FAIL: TestP4GArchiveMarkerIsTheTagNotTheName (0.03s)
    p4g_predicate_red_test.go:17: branch feat/decided-loser carries the archive tag refs/tags/archive/feat/decided-loser and must be exempt — the criterion asks about the NAME, not the decision
FAIL	github.com/nick/gopi/internal/tools	1.835s
```

分支上同一断言 → PASS（见 ① 的最后一个用例；签名带 `repo`）。

### ③ Ran — 真实执行（真实仓库 `/home/user/work/GoPi`，真实 CLI verb，本 run 的二进制）

```
$ go build -o /home/user/workspace/_tmp/p4g-r2/gopi-branch ./cmd/agent/          # exit 0
$ git -C /home/user/work/GoPi update-ref refs/heads/feat/capture-self-window e1701ac807f27b4a603e2e1dff45443768341cc5
-- BEFORE --
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/heads/feat/capture-self-window
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/tags/archive/feat/capture-self-window

$ gopi-branch archive --reclaim --repo /home/user/work/GoPi
archive ref reclaim: /home/user/work/GoPi
  deleted          archive/feat/capture-self-window
  deleted=1 kept=0 skipped=0
cli-exit=0

-- AFTER --
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/tags/archive/feat/capture-self-window
$ git rev-parse --verify --quiet refs/heads/feat/capture-self-window   → NO（已消失）
$ gopi-branch archive --reclaim --repo /home/user/work/GoPi
archive ref reclaim: /home/user/work/GoPi
  clean — every archive tag's branch ref is already reclaimed            # 幂等
```

先 `update-ref` 复原孤儿再回收，是**故意**的：上一次 run 已经把它删掉过，不复原就观察不到状态迁移。

### ④ Ran — 四条守卫的真实 CLI 输出（干净克隆里造齐四种形状）

```
$ gopi-branch archive --reclaim --repo <clone>
  kept-divergent   archive/feat/p4g-div2  archive tag and branch ref name DIFFERENT commits (archive/feat/p4g-div2 c5c4fb17… vs refs/heads/feat/p4g-div2 ec1f9904…) — the branch moved after the decision or the tag was written elsewhere; keeping the ref (Rule 15: report, never delete work)
  skipped          archive/feat/p4g-held  branch feat/p4g-held is checked out at /home/user/workspace/_tmp/p4g-r2/held — a live checkout is never touched (GOPI.md Rule 29)
  skipped          archive/master  branch master is the integration branch or a task-run ground — never a reclaim candidate
  skipped          archive/task/01a0f44c2d0f7a82bde8f76445daa30d  branch task/… is the integration branch or a task-run ground — never a reclaim candidate
  deleted=0 kept=1 skipped=3
```

事后 `git show-ref` 证明：divergent 的 ref 仍在 `ec1f9904`；worktree 占用的 ref 仍在；
master 与 task/* 都未被触碰；四条 tag 全部健在。

### ⑤ Ran — 自动腿（archive 成功后同一次调用内回收）

```
$ gopi-branch archive feat/p4g-auto3 --reason unowned --detail "…" --repo <clone>
archived feat/p4g-auto3 as archive/feat/p4g-auto3
  reason: unowned
  owner:  (no task row tracked this branch)
  read back: git cat-file blob refs/gopi/archive-reason/feat/p4g-auto3
  deleted          archive/feat/p4g-auto3
cli-exit=0
$ git show-ref | grep p4g-auto3
c5c4fb17… refs/tags/archive/feat/p4g-auto3        # ref 没了，tag 与 reason record 都在
```

（本 run 顺手修掉了第一次运行时的渲染缺陷：删除行原先打印 `deleted: ` 空串，因为删除的
`Reason` 按设计为空。现在由唯一的 `ArchiveReclaimRowLine` 渲染且以 tag 命名该行 —— 自动腿与
`--reclaim` 两个面共用同一个渲染器，Rule 13。）

### ⑥ master 二进制的红证（CLI 面）

```
$ <master 2db219d4 的二进制> archive --reclaim --repo <clone>
gopi archive: unknown flag --reclaim
<usage: --check 一节之后即结束，没有 --reclaim>
exit=1
```

### ⑦ 门（分支作用域，Rule 27：不跑全树）

```
go build ./...                                                                  → exit 0
go test ./internal/tools/ -run 'Archive' -count=1                               → ok 3.493s
go test ./cmd/agent/ -run 'Archive' -count=1                                    → ok 3.085s
go test ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile' -count=1 → ok（含 DOC-INDEX 行）
go test ./internal/standards/ -count=1                                          → ok
go test ./internal/smokegate/ -count=1                                          → ok
gopi doccheck                                                                   → corpus is consistent
git status --porcelain（本 run worktree）                                        → 空
```

### 本 run 的补充交付（报告 run 追加）

- `ArchiveReclaimRowLine` —— 行渲染的唯一来源（Rule 13），修掉自动腿里 `deleted: ` 空行的缺陷。
- `runArchiveReclaimAfterArchive(repo, branch)` —— 只打印**本次决定**那一行，但扫描失败必须
  仍然报错（Rule 15：跑不动的回收腿不能看起来像「什么都没找到」）。

---

## Handover — the real execution and the closes taken after the earlier draft (2026-10-01 06:30 CST)

The sections above were written by the drafting run; this section records what the
REPORTING run verified for itself, because three of the draft's own claims were
not yet true when it stopped, and one of them was false.

### What the draft got wrong (recorded, not edited away)

1. **The worktree it names no longer existed.** `/home/user/workspace/gopi-archive-ref-reclaim`
   was gone (deleted, with its registration as collateral of the `/dev/shm`
   worktree-registry loss tracked by task `01a0f44f`), so `bin/gopi` and the
   `Ran:` block above cite a path that could not be executed when read. This run
   recreated the ground as
   `/home/user/workspace/gopi-01a0f44c-archive-reclaim` (same branch
   `fix/archive-ref-reclaim`, `git worktree add … fix/archive-ref-reclaim`) and
   ran everything from there.
2. **The orphan had already been reclaimed — by the drafting run, during this epic.**
   When the reporting run first looked, `refs/heads/feat/capture-self-window` was
   ALREADY ABSENT while `refs/tags/archive/feat/capture-self-window` was still at
   `e1701ac8`. The acceptance criterion "the orphan is reclaimed by this same
   path" was therefore already satisfied **before** the acceptance run, by this
   arm — which is why the acceptance had to RECREATE the ref
   (`git update-ref refs/heads/feat/capture-self-window e1701ac8…`) to observe the
   transition at all. A criterion satisfied before it is measured measures nothing.

### Ran — the changed surface, executed for real

`CGO_ENABLED=0 go build -o <session>/gopi-reclaim ./cmd/agent/` then, against the REAL
repo `/home/user/work/GoPi` with the incident shape restored:

```
$ git -C /home/user/work/GoPi update-ref refs/heads/feat/capture-self-window e1701ac807f27b4a603e2e1dff45443768341cc5
$ git -C /home/user/work/GoPi show-ref | grep capture-self-window
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/heads/feat/capture-self-window
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/tags/archive/feat/capture-self-window

$ <session>/gopi-reclaim archive --reclaim --repo /home/user/work/GoPi
archive ref reclaim: /home/user/work/GoPi
  deleted          archive/feat/capture-self-window
  deleted=1 kept=0 skipped=0
exit=0

$ git -C /home/user/work/GoPi show-ref | grep capture-self-window
e1701ac807f27b4a603e2e1dff45443768341cc5 refs/tags/archive/feat/capture-self-window   # tag kept, ref gone
```

### Red → green, observed on this run (base = master `2db219d4`)

Two separate red proofs, because they are two separate defects:

```
# ① the delete arm — on master, the arm's own test file does not compile because the arm does not exist
$ cd /home/user/workspace/gopi-redproof-01a0f44c   # detached at master 2db219d4 + the branch's test file
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestArchiveReclaim' -count=1
internal/tools/archive_ref_reclaim_test.go:59:9: undefined: ArchiveReclaimDecidedRefs
… FAIL github.com/nick/gopi/internal/tools [build failed]

# ② the §2.5 criterion — the exemption as it stands does not match a REAL archived branch
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestRedProof_ExemptAsksAboutTheTagNotTheName' -count=1
--- FAIL: TestRedProof_ExemptAsksAboutTheTagNotTheName (0.02s)
    zz_redproof_25_test.go:14: feat/decided-loser carries the archive MARKER and must be exempt (the tag, not the name)
FAIL github.com/nick/gopi/internal/tools 1.174s

# green, same predicate with the repo argument, on fix/archive-ref-reclaim
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestRedProof_ExemptAsksAboutTheTagNotTheName' -count=1
ok  	github.com/nick/gopi/internal/tools	1.416s
```

### Gates (branch-scoped, Rule 5 / Rule 27 — no whole-tree suite)

```
$ go build ./...                                                   → exit 0
$ go test ./internal/tools/ -run 'TestArchiveReclaim|TestFinishCleanupBranchExempt' -count=1   → ok 1.987s
$ go test ./cmd/agent/ -run 'Archive' -count=1                     → ok 3.984s
$ go test ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile' -count=1 → ok 2.637s
$ go test ./internal/smokegate/ -count=1                           → ok 0.010s
$ go test ./internal/tools/ -run 'TestArchive|TestMergedCleanup|TestFinishCleanup|TestRepo' -count=1 → ok 59.914s
$ gopi doccheck                                                    → corpus is consistent
$ git status --porcelain                                           → (empty)
```

### Delivery shape (Rule 6 / Rule 30) — and the one thing that nearly lost this work

The branch `fix/archive-ref-reclaim` was **running with no registered worktree**
(`git worktree list` showed no entry for it), so finish-cleanup would refuse to
release a ground that did not exist and the deliverable had no path to hand-over.
Fixed by re-registering the ground (see 1. above) and fast-forwarding the task
branch onto the delivered tip:

```
$ git -C <task ground> merge --ff-only 369c9d01
task/01a0f44c2d0f7a82bde8f76445daa30d → 369c9d01   (deliverable reachable from the row's own branch)
```

The path named in the body of this document (`…/gopi-archive-ref-reclaim`) is kept
as written, because it records what that run did; the live ground is
`/home/user/workspace/gopi-01a0f44c-archive-reclaim`.
