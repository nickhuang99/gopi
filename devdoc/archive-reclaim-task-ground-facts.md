<!-- doc-class: historical -->
# Task Report: `--reclaim` 对 task/* 的拒绝改为「基于事实」— 追加证据给 01a12dbc

分支：`task/01a12dc056167498a7ce73821aa6cdb2`（tip 见 `git log -1`）
本行：`01a12dc0-5616-7498-a7ce-73821aa6cdb2`

## 问题（实测，非推断）

`gopi task archive --reclaim` 的 guard ④ 用**分支名前缀**判定 task-run ground：

```go
func archiveReclaimProtected(branch string) bool {
	switch branch { case masterBranchName, mainBranchName: return true }
	return strings.HasPrefix(branch, TaskGroundBranchPrefix)   // ← 缺陷
}
```

拒绝句是 *"branch X is the integration branch or a task-run ground — never a reclaim candidate"*。
这句话被**无条件**说出来，因此对一个「账本没有行、没有 worktree 持有、内容早已合并/已被取消」的
`task/<id>` 分支，它是**假的**。结果是这类分支被永久挡在回收之外：豁免即泄漏（本 arm 存在的理由）。

本机实测（旧代码 + 新回归测试，日志见 session 目录 `redproof/red-old-code.log`）：

```
$ go test ./internal/tools/ -run TestArchiveReclaimReclaimsAnUnownedTaskGround -count=1
--- FAIL: TestArchiveReclaimReclaimsAnUnownedTaskGround (0.09s)
    archive_ref_reclaim_taskground_redproof_test.go:77: an unowned, unheld task ground must be reclaimed,
    got {Verdict:skipped Reason:branch task/01a0f44c…a301 is the integration branch or a task-run ground — never a reclaim candidate}
```

## 修复：名字 → 事实

`task/*` 不再按名字拒绝。扫描时对 task-ground **只问两个事实**，任一条成立才拒绝，并在拒绝句里指名是哪一条：

| 事实 | 判据 | 拒绝句指名 |
|---|---|---|
| ③ worktree 持有 | `git worktree list` 注册表（只读，Rule 29） | worktree **路径** |
| ⑥ 账本有非终态行 | 注入的 owner lookup（生产 = `archiveBranchOwners`） | **task id + status** |

`master`/`main` 继续按名字无条件拒绝（唯一保留的名字判据）。四个格的组合见测试表。

关键设计点：

- **默认拒绝的方向不变**（Rule 15）：状态集里**没有**被判为 terminal 的一律保持（`pending/claimed/
  in_progress`、`reopened`、任何本包未分类的状态、以及**空状态**）——「没人推理过的状态不是删除 ref 的
  许可」。这与 worktree sweep 的 `terminal()` 是**不同的问题**，故**故意不复用**它（那个谓词回答
  「这行结束了没」，本谓词回答「还有活进程可能在它上面干活吗」），差异在代码注释里写明了。
- **账本读不到 ≠ 没有主**：lookup 返回 error 时**停止整个扫描**并报错（`res.Err` + exit 1），
  绝不读成「无行 ⇒ 可删」。nil seam 同样按名字拒绝（编程错误，不是状态）。
- **懒查询**：只有「task-ground 名 且 worktree 未持有」才会去问账本 —— 普通的 `feat/…` 残留**零账本访问**。
- **失败面可见**：拒绝行新增 `Kind` 字段（`integration-branch` / `live-task-row` / `held-by-worktree`），
  调用方与测试断言**事实**而不是散文。

### 改动文件

| 文件 | 改动 |
|---|---|
| `internal/tools/archive_ref_reclaim.go` | `archiveReclaimProtected` 去前缀；新增 `archiveReclaimIsTaskGround`、owner seam（`archiveReclaimOwnerLookup` / `archiveReclaimOwnersFromLedger` / `archiveReclaimDecidedRefsWith`）、`archiveReclaimMatchesBranch`、`archiveReclaimRowLiveness`、三条 `*Statement` 拒绝句、`ArchiveReclaimRow.Kind`、常量 `ArchiveReclaimProtected*` |
| `internal/tools/archive_reason.go` | `archiveBranchOwners` 的 SQL 抽成 `archiveReclaimAllOwners`（**一个查询形状**，rm 复用）；其余不变 |
| `internal/tools/archive_ref_reclaim_test.go` | 旧断言 `task/* 永不被回收` **就是缺陷的编码**，改为断言新事实（未持有/无主 ⇒ 回收） |
| `internal/tools/archive_ref_reclaim_taskground_test.go`（新） | 表驱动四格 + 名字判据 + seam + 可读性 + 生产路径端到端 |
| `internal/tools/archive_ref_reclaim_taskground_redproof_test.go`（新） | **红证**：只用修复前就存在的 API，故在旧代码上**可编译且失败** |
| `cmd/agent/cli_archive.go` | `-h`：新增 task-ground 事实判据段；无主分支 ⇒ `--reason` 强制回显 `unowned` |
| `cmd/agent/cli_archive_test.go` | 新 e2e：真二进制 × 真 repo × 真账本，覆盖四种情形 |

## Red → Green（同一份测试文件，两个树）

```
# 旧代码（git checkout -- 4 个源文件）+ 新测试
$ go test ./internal/tools/ -run TestArchiveReclaimReclaimsAnUnownedTaskGround -count=1
FAIL  … "never a reclaim candidate"        exit=1
# 应用 fix.patch + 新测试
$ go test ./internal/tools/ -run 'TestArchiveReclaim|TestArchiveBranchOwners' -count=1
ok  github.com/nick/gopi/internal/tools  2.787s
```

红证文件保存在本 run 的 session 目录：`redproof/fix.patch`、`redproof/red-old-code.log`、
`redproof/archive_ref_reclaim_taskground_redproof_test.go`（Rule 37）。

## Verification

- Build: `go build ./...` → OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test ./internal/tools/ -run 'TestArchiveReclaim|TestArchiveBranchOwners' -count=1`
  → **ok github.com/nick/gopi/internal/tools 2.787s**；`go test ./cmd/agent/ -run TestArchiveCLIReclaimAnswersATaskGroundFromTheFacts`
  → **PASS (5.27s)**。
- `make test-budget PKGS='./internal/tools ./cmd/agent'` → **exit 3 = 预算到期（wall-clock），不是断言失败**：
  `stamp: 2026-10-12T12:08:23+08:00 … budget=25m elapsed=1603s load_start=10.47 … load_end=12.35 14.71 18.19 cores=8 exit=3 outcome=timeout`。
  同一 run 另列出 8 条**断言**失败（`TestAnchor`、`TestPublishedAnchorResolves`、`TestNotifyScriptAgreesWithTheRegistryConstants`、
  `TestPublishedHelpPagesStayWithinTheirUsageBudget`、`TestEveryHelpConstantFitsTheWrapBudget` …）。
  **它们不是本 diff 造成的**，实测依据：本分支 `git diff --name-only 26d16be82..HEAD` **不含**
  `cmd/agent/cli_tasks.go`（报错的那个 `-h` 页面）；本 diff 新增的 archive 段**不出现在** `gopi task -h`
  里（`./bin/gopi task -h | grep -c "A task/<id> branch is NOT refused"` → 0，它只出现在
  `gopi task archive -h`）；`gopi task -h` 现在是 193 行 / 预算 190，而基线 `26d16be82` 已经如此。
  该 host 当时 load 12–18（8 核），本就跑不动这两个包的整包；这批失败与同 host 上兄弟行
  `01a12e2a-ff38`（"9 red tests on master"）主题重合。**本 run 未修它们（Rule 28：系统层归 peerbox／该行已被认领）。**
- Ran: 见下方三条真跑记录（Rule 31）
- Commit: 见本分支 tip

## Evidence (self-filed)

1. **红证（旧代码 + 新测试）**：`go test ./internal/tools/ -run TestArchiveReclaimReclaimsAnUnownedTaskGround -count=1`
   → `--- FAIL … Verdict:skipped Reason:… never a reclaim candidate`，exit 1（全文 `redproof/red-old-code.log`）。
2. **绿（修复后）**：`go test ./internal/tools/ -run 'TestArchiveReclaim|TestArchiveBranchOwners' -count=1` → `ok github.com/nick/gopi/internal/tools 2.787s`。
3. **真二进制 e2e**：`go test ./cmd/agent/ -run TestArchiveCLIReclaimAnswersATaskGroundFromTheFacts -count=1 -v`
   → `--- PASS: … (5.27s)`；该测试**真跑** `bin/gopi task archive --reclaim` 四次（真 repo + 真 git 仓库 + 隔离账本）：
   无主无持有 ⇒ `deleted=1` 且 `refs/heads` 消失、tag 留在原 tip；`in_progress` 行 ⇒ `skipped=1` 且拒绝句含
   task id 与 `in_progress`、分支未被移动；worktree 持有 ⇒ `skipped=1` 且拒绝句含 worktree 路径；`master` ⇒
   `is the integration branch` + `skipped=1` 且 ref 未动。
4. **`-h`**：`./bin/gopi task archive -h` → 新段落打印（"A task/<id> branch is NOT refused for its name…"
   与 "its reason is FORCED to --reason unowned"）。
5. **手工真跑（fixture repo，Rule 31 的 `Ran:`，全部在本 run 内以真二进制完成）**：
   - 无主分支 + 显式 `--reason superseded`（证明 `-h` 的 unowned 说明是真的）：
     ```
     $ gopi task archive feat/unowned-demo --reason superseded
     archived feat/unowned-demo as archive/feat/unowned-demo
       reason: unowned
       owner:  (no task row tracked this branch)
       deleted          archive/feat/unowned-demo          exit=0
     $ git cat-file blob refs/gopi/archive-reason/feat/unowned-demo  →  "reason: unowned"
     $ git for-each-ref → refs/heads/master, refs/tags/archive/feat/unowned-demo(+reason ref)
     ```
   - task-ground **被 worktree 持有** + 被归档的 master（真 `git worktree add`）：
     ```
     $ gopi task archive --reclaim
     archive ref reclaim: /dev/shm/gopi-archdemo2-…/repo
       skipped  archive/master  branch master is the integration branch — never a reclaim candidate
       skipped  archive/task/01a0f44c…  branch task/01a0f44c… is checked out at /dev/shm/gopi-archdemo2-…/held —
                                          a live checkout is never touched (GOPI.md Rule 29)
       deleted=0 kept=0 skipped=2                          exit=0
     $ git rev-parse refs/heads/task/01a0f44c…  → 8b20ca51…（未动）
     ```
   - 无主无持有的 task-ground ⇒ `deleted=1` 且 `refs/heads` 消失、tag 留在原 tip：由第 3 条的
     `TestArchiveCLIReclaimAnswersATaskGroundFromTheFacts`（真二进制 4 次调用）覆盖。

## Not Done (honest)

- **真机上 `task/*` 残留的实际回收**：本 run 的 worktree/repo（`/home/user/workspace/gopi-01a12dc0…`）
  全部分支都在使用中，本 run 没有权限也没有理由去删别人的 ref（Rule 29），所以「本机真实残留被清掉」
  是**合并后由 operator 跑 `gopi task archive --reclaim` 才发生的动作**，本 run 交付的是它**现在会**这么做。
- **本机 master 上的可复现实例在本 run 期间消失了，这一点必须如实说清**：`/home/user/work/GoPi` 现在
  只有 4 条 refs/heads（master + 3 条 live ground，其中 2 条是本 run 及兄弟行的 ground），**没有一条
  带 archive tag** —— 所以「本机残留被回收」这句**本 run 无法作为证据**（我没有跑过针对它的
  `--reclaim`，也不会去跑）。我用 fixture repo 复现了同样的形状（无主 + 无持有 + 有 tag ⇒ 回收），
  那才是本 run 的证据。
- **另一个真实的残留形态（本 run 顺带查到的、不属于本修复范围）**：`gopi task for-branch
  task/01a12e26a5ed7d388eb4b164137bc5d5` 报 `status=completed (ended)`、worktree 路径指向一个
  **已不存在**的目录，而该分支**既不在 refs/heads 也不在 worktree 列表里**（证据：本 run session
  目录 `artifacts/bug-dead-task-branch-evidence.txt`）。这是「账本指向一个已消失的 ground」的另一类
  残留，需要的是「分支不存在时如何收尾」的决策，不是本 arm 的判据 —— 归 01a12dbc 那条关闭链的
  修复，本 run 只留证。
- 未在真机账本上验证「同一分支有多行（cancelled + in_progress）」这一格——表里用注入 seam 覆盖了，
  但生产路径的端到端只覆盖了单行 `in_progress`。
- `--reclaim` 的 CLI 层未新增 `Kind` 的机器可读输出（CLI 仍按 `ArchiveReclaimRowLine` 渲染），
  如果需要 `--json` 再加。
- **整包 `make test-budget` 未绿**（exit 3 = 25m wall-clock 预算到期，host load 12–18；同 run 另有 8 条断言失败，
  经核验不属于本 diff：涉及文件不在本分支改动集内，且本 diff 新增的 help 段不在报错的那个页面上）。
  本 diff 的 scoped 测试全绿；整包绿这件事**不声称为本 run 的成果**。
- **另记（Rule 41）**：本 run 用真 CLI 时发现仓库缺少 `~/.gopi/logs/` 时 `removalaudit` 的
  分支删除记录写不进去，只在 stderr 说一句 —— 已作为独立现象提行
  `01a12e53-a1c9-7836-87af-2b36b0dbaac5`（连同一处 `%!(EXTRA &errors.errorString=&{XML syntax
  error on line 1: unexpected EOF})` 的格式化缺陷，evidence 在本 run 的 `artifacts/`）。本 run 未修。
