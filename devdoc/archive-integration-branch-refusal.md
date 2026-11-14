<!-- doc-class: historical -->
# Task Report: 归档的拒绝必须发生在写入之前 — 集成分支在写任何 ref 之前被拒

分支：`task/01a12f0d0ca278a0bb7528d4b79ac4ef`（commit `244ba510f`）
Worktree：`/home/user/workspace/gopi-01a12f0d0ca278a0bb7528d4b79ac4ef`
任务行：`01a12f0d-0ca2-78a0-bb75-28d4b79ac4ef`（tags=bug）

## 现象（实测，非推断）

在**一次性临时仓**（`mktemp -d`，非主仓）里建 `master` / `main` / `feat/x` 三个分支，
用修复前的二进制跑同一个动词：`gopi task archive <branch> --reason hand-mistake --detail probe`

```
### CASE 1: integration branch master
--- gopi task archive master --reason hand-mistake --detail probe   rc=0
archived master as archive/master
  reason: unowned
  owner:  (no task row tracked this branch)
  read back: git cat-file blob refs/gopi/archive-reason/master
  skipped          archive/master  branch master is the integration branch — never a reclaim candidate
--- git tag --list 'archive/master' :archive/master
--- git rev-parse --verify refs/gopi/archive-reason/master :9b46c1f321ea307e61a63e4b3aa2e34d36c1a5c7
```

**同一次调用同时说「已归档 master」和「master 被跳过」，rc=0，而两个 ref 都真的写下去了。**
`main` 逐字相同（`archive/main` + `refs/gopi/archive-reason/main` 均存在）。

**危害不是自相矛盾的输出，而是它写下去的东西**：`refs/tags/archive/<branch>` 是本项目的
「已决弃」命名空间，每条 merge 路径都把它读作「这里已经判过一个败者」（**GOPI.md Rule 9**）
—— 于是这次被拒的回收把**整个系统要合并进去的那个头**标成了「已判弃」。这正是归档通道存在
的意义所要防止的那一个反转。

## 根因（写入在守卫之前）

`ArchiveBranch`（`internal/tools/archive_reason.go`）的顺序是：**先写** tag + reason record，
**然后**才把决定交给 reclaim 腿去「贯彻」（删掉被决定的分支 ref）。而 reclaim 腿的守卫 ④
（`archiveReclaimProtected`，`internal/tools/archive_ref_reclaim.go:155`）按**名字**拒绝 master/main ——
于是**拒绝来晚了**：伤害在它开口之前就已经落在磁盘上。

判据本来就已经存在（守卫 ④，`case masterBranchName, mainBranchName: return true`），缺的不是判据，
而是**在写之前问它**。

## 修法

`tools.ArchiveDecide` 里新增**守卫 ⓪**——人手归档通道本来就走的那个 gate，位置在**账本读之前、
第一个 ref 之前**：

```go
if archiveReclaimProtected(branch) {
    return ArchiveDecision{Verdict: ArchiveVerdictRefused,
        Refusal: archiveProtectedRefusalSentence(branch)}, nil
}
```

- **判据只有 ONE 定义（Rule 13/14）**：`archiveReclaimProtected` 是 reclaim 腿自己的谓词，这里只是它的
  **第二个调用者**，不是把 `"master"`/`"main"` 在 CLI 里再写一遍。新增测试直接断言两边问的是同一个谓词。
- **判词只有 ONE 定义**：把守卫 ④ 的句子从 `archiveReclaimProtectedStatement` 里拆出来为
  `ArchiveProtectedRefusalText`，于是「`--reclaim` 跳过 master 的那行」与「`task archive master` 拒它的
  那段」是同一句测量。归档面在此基础上再补两件只有归档调用者需要的事：**没写任何东西**（并点名两个 ref），
  以及**没有 override**。
- **不可 override**：`--override-completed` 清的是**所有者** gate（账本说这活干完了），**不该**清一个**名字**
  谓词。测试 `TestArchiveBranchIntegrationRefusalIsNotOverridable` 钉住这一点。
- **不削弱既有守卫**：reclaim 的六道 guard 一行未动。被 gate 放行、却被 reclaim 腿按**事实**（tip 漂移 /
  worktree 占用 / 活跃账本行）拒掉的分支，那一行照旧打印 —— 那是一个真实可报告的事实分歧，不是本行要消掉的矛盾。
- 拒绝发生在**账本读之前**，所以被拒的归档**不留任何痕迹**（与机制侧 owner gate 自己文档里的性质一致）。

## 验证（Rule 5 / Rule 31：真跑被改的命令面）

- Build: `CGO_ENABLED=0 go build ./...` → exit 0
- Tests:
  - `go test ./internal/tools/ -run 'TestArchiveBranchRedProof|TestArchiveBranchIntegrationRefusalIsNotOverridable|TestArchiveIntegrationRefusalReusesTheReclaimGuardsSentence|TestArchiveReclaim|TestArchiveGate|TestRepoSweep' -count=1` → **ok 5.980s**
  - `go test ./cmd/agent/ -run 'TestArchiveCLI|TestTaskArchiveCarriesTheBranchOperandToTheSameEngine' -count=1` → **ok 3.489s**
- Ran（真跑，一次性临时仓，非主仓；脚本 `~/.gopi/sessions/01a12f0d-…/artifacts/probe-archive-master.sh`）：

```
--- gopi task archive master --reason hand-mistake --detail probe   rc=1
archive refused: archive/master
  branch master is the integration branch — never a reclaim candidate
  NOTHING WAS WRITTEN: no refs/tags/archive/master and no refs/gopi/archive-reason/master — the refusal runs
  BEFORE the first write, so the integration head is never recorded as a decided loser (GOPI.md Rule 9).
  There is no override for this one: archiving master/main is not a decision about a branch, it is a decision
  about the integration head, and every merge path reads an archive tag as exactly that.
--- git tag --list 'archive/master' :
--- git rev-parse --verify refs/gopi/archive-reason/master :<absent>

--- gopi task archive main …   rc=1            同上（archive/main 亦为空）
--- gopi task archive feat/x --reason hand-mistake --detail probe   rc=0   ← CONTROL
archived feat/x as archive/feat/x
### all archive tags in repo:
archive/feat/x
### all reason refs in repo:
refs/gopi/archive-reason/feat/x
```

- Result：`master`/`main` rc=1、两个 ref 都不存在；普通无主分支 `feat/x` 照旧 rc=0、tag 写入并读回
  → 满足 acceptance 的 (a)(b)(c)(d)，且「一刀切拒绝」被 CONTROL 挡住。
- Commit: `244ba510f`

## Evidence (self-filed)

- RED 取证（**同样的测试文件、PRE-FIX 代码**，`git archive HEAD | tar -x` 展开到 `/dev/shm/red-01a12f0d`，
  Rule 29 不借别人的 worktree）：
  ```
  --- FAIL: TestArchiveBranchRedProofRefusesTheIntegrationBranchBeforeAnyWrite (0.04s)
      master: verdict="proceed" recorded=true tag="archive/master" — the integration branch must be REFUSED with nothing written
  --- FAIL: TestArchiveBranchIntegrationRefusalIsNotOverridable (0.04s)
      acknowledgement cleared the integration-branch refusal (verdict="proceed" recorded=true)
  --- FAIL: TestArchiveCLIRefusesTheIntegrationBranchBeforeAnyWrite (2.30s)
      archiving the integration branch master exited 0:
        archived master as archive/master
        …
        skipped          archive/master  branch master is the integration branch — never a reclaim candidate
  ```
  修后同一组测试全绿 → red→green 是被测量到的，不是被断言的。（红证明文件只用**修复前就存在**的符号，
  所以它能在两棵树上编译；命名新导出符号的那条断言另放 `archive_integration_refusal_shared_test.go`，
  否则红树上量到的是编译错误而不是缺陷。）
- doc-anchor 未被我扰动（实测，不是推断）：pre-fix 树与本树各跑一遍 `go test ./internal/agent/ -run TestDocAnchor`，
  两边的 **50 条新漂移 / 8 条 stale 逐字相同**（差异仅为输出里的绝对路径），且**没有一条锚点落在我改的三个文件上**
  → 门禁在两边同样红，红不是本行造成的。
- `gopi tool doccheck` → 见文末（本文件入册后）。

## Not Done（如实）

- **未动 reclaim 的六道 guard**（约束要求），也未给守卫 ④ 增加除「集成分支」外的任何名字 —— 一个更宽的
  名字表 = 在 CLI 里另写一份判据。
- **未跑全量 `make test`**：merge/verify 路径按 Rule 27 走分支内定向 smoke。
- **`main` 之外的本地化集成分支名**（例如某些仓库叫 `trunk`）仍不在守卫 ④ 的集合里 —— 这是**既有**谓词的
  范围，本行只保证「谓词说什么，写入就照做」，不改它的成员。
- 本 run 期间有一次**误编辑**：`archive_reason.go` 里另写了一条重复的 `ArchiveBranch` 定义（编辑落在文件中间），
  发现即以固定文本恢复为原函数，恢复后 `git diff` 只有预期的三处改动、全套定向测试绿。

## Files

- `internal/tools/archive_reason.go` — 守卫 ⓪ + `archiveProtectedRefusalSentence`
- `internal/tools/archive_ref_reclaim.go` — `archiveReclaimProtectedStatement` 改为调用导出的
  `ArchiveProtectedRefusalText`（守卫逻辑本身一行未动）
- `cmd/agent/cli_archive.go` — 注释记下「这条腿够不到的矛盾」及其去向
- `internal/tools/archive_integration_refusal_redproof_test.go`（red proof，两棵树均可编译）
- `internal/tools/archive_integration_refusal_shared_test.go`（ONE 判据 / ONE 判词）
- `cmd/agent/cli_archive_test.go`（真动词 CLI 断言：rc≠0、两个 ref 不存在、CONTROL 照旧成功）
