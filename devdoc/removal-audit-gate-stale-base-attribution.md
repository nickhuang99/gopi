<!-- doc-class: historical -->
# 归属判定：报告树基点上的 `TestRemovalAuditGateEveryRemovalSiteEmits` 红 = 陈旧基点假警报（已修复）

task `01a12bcc-1fff-7d8d-9409-f5d56277da05` · branch `task/01a12bcc1fff7d8d9409f5d56277da05` · host hosta

**被测 commit（Rule 38）**：本文件的 census 数字（24 站点 / floor 15）与逐 rev 红绿矩阵
**实测于 `81e69eb97`**（本文件提交前的分支 tip，也就是 `9b88c4b5b`→`fe6a971fa` 窗口闭合之后
的第一个集成点）；本文件自身只改 `devdoc/`（+ `DOC-INDEX.tsv` 一行），提交后该门在同一棵树上
复跑仍 `ok`（复跑的完整输出见 session 目录 `artifacts/09-final-tip.txt`）。生产代码零变化，
因此该绿与矩阵互证。

## 结论（三句话）

1. **那条红是真的红过，但不是"未记录"**：它出现在一个**窗口**里 —— 从 `9b88c4b5b`
   （2026-10-11 23:36，把两处站点改用同 kind 的兄弟函数）起，到 `fe6a971fa`
   （2026-10-12 00:10，把门的词表改为从 `internal/removalaudit` 自身实现派生）止。
2. **被引用的两个基点分处窗口两侧，但这是本行简报的并列表述，不是报告原话**：
   `a3a31da42`（报告 B 写到的 **hosta 本机 master**，且它用的是**另一条测试** `TestCommandsDoc*`
   的红）在**窗口之前**（本条 gate 实测绿）；报告里那条 gate 的红是在**它交付时同步到的
   `ec7342d72`** 上测到的（实测红）。逐字裁定见第二节末尾 —— **报告 B 的机理诊断本来就是对的**，
   还做了因果实验；它文末引用的 bug id 在两台账本上都查不到（见第五节）。
3. **修复 `fe6a971fa` 已是本 run worktree HEAD `81e69eb97` 的祖先**，本树该门实测 **PASS**
   （24 站点 / floor 15；23 emitting + 1 escaped）；**当前 peerbox master `fabcd113e` 也是绿的**。
   因此本行**不需要任何代码修复**，交付物是这份带实测的归属判定，让这条红不再被重复派发。

## 一、红线机理：门把 emitter 名字硬编码，站点却换成了同 kind 的兄弟函数

门槛（窗口内版本，见 `git show ec7342d72:internal/standards/removal_audit_gate_test.go`）写的是**两个名字字面量**：

```
68:	removalAuditEmitWorktree = "WorktreeRemove"
69:	removalAuditEmitBranch   = "BranchDelete"
```

而 `9b88c4b5b` 在同一时刻把两处生产站点从 `BranchDelete` 换成了**同 kind 的兄弟**
`BranchDeleteAt`（该 commit 的 D2 项：记录要带上 tip 与 delivery point）：

```
$ git show 9b88c4b5b -- internal/tools/task_finish_cleanup.go | grep -n '^[-+].*removalaudit\.BranchDelete'
111:-	removalaudit.BranchDelete(removalaudit.ArmFinishCleanupBranch, branchDir, branch, rep.TaskID,
117:+	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, branchDir, branch, rep.TaskID,
127:-		removalaudit.BranchDelete(removalaudit.ArmFinishCleanupBranchForce, branchDir, branch, rep.TaskID,
131:+		removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, branchDir, branch, rep.TaskID,
```

两个函数**写的是同一个 `Record.Kind`** —— 兄弟就是一层转发，记录照写不误
（`internal/removalaudit/audit.go`）：

```go
func BranchDelete(arm Arm, repo, branch, taskID, evidence string) {
	BranchDeleteAt(arm, repo, branch, taskID, "", "", evidence)
}

func BranchDeleteAt(arm Arm, repo, branch, taskID, tip, deliveryPoint, evidence string) {
	emit(Record{ Kind: KindBranchDelete, ... })
}
```

于是门按字面名字找不到 emit，就把 `internal/tools/task_finish_cleanup.go:4310 / :4339`
报成 `UNRECORDED` —— **"记录在写、门说没写"**，正是 `fe6a971fa` 的 commit message 里
写下的那句话。**这不是缺记录，是门读不到记录**（Rule 13/14 的典型：一个必须写两处的名字，
在其中一处漂移）。修复方式是把词表**从 emitter 自身实现派生**（`removalAuditDeriveVocabulary`，
含 `BranchDelete → BranchDeleteAt` 的委派跳转与不动点），从此改名/新增同 kind 兄弟都不再漂移。

## 二、归属：祖先关系与红/绿窗口（全部实测）

| rev | 日期 | 门怎么认 emitter | `task_finish_cleanup.go` 的 branch 站点 | 该 rev 上跑同名测试 |
|---|---|---|---|---|
| `a3a31da42` | 10-11 22:41 | 硬编码 `BranchDelete` | `BranchDelete` | **ok, exit 0（绿）** |
| `9b88c4b5b` | 10-11 23:36 | 硬编码 `BranchDelete` | **`BranchDeleteAt`** | **FAIL, exit 1；2 条 UNRECORDED** |
| `ec7342d72` | 10-11 23:53 | 硬编码 `BranchDelete` | `BranchDeleteAt` | **FAIL, exit 1；2 条 UNRECORDED** |
| `fe6a971fa` | 10-12 00:10 | **派生词表** | `BranchDeleteAt` | **ok, exit 0（绿）** |
| `62cf75e30` | 10-12 00:55 | 硬编码（挂在旧 master 上） | `BranchDeleteAt` | **FAIL, exit 1；2 条 UNRECORDED** |
| `81e69eb97`（本 run HEAD） | 10-12 01:02 | 派生词表 | `BranchDeleteAt` | **ok, exit 0（绿）** |

祖先关系（`git merge-base --is-ancestor`）：

```
fe6a971fa → a3a31da42 : no      fe6a971fa → ec7342d72 : no
fe6a971fa → fe6a971fa : YES     fe6a971fa → 62cf75e30 : no
fe6a971fa → HEAD(81e69eb97) : YES
```

`fe6a971fa^` **就是 `ec7342d72`**（`git rev-parse fe6a971fa^` → 同一 sha），
`git log --oneline ec7342d72..fe6a971fa` 只有它一个 commit —— 窗口的关闭点与 peerbox master
当时的 tip 是同一棵树加一个 commit。

> **对"干净树上红"这句话的裁定（逐字核对过两份报告之后）。**
> `TestRemovalAuditGateEveryRemovalSiteEmits` 在 `ec7342d72` 上确实是红的，而且与跑测试的
> 分支无关（本报告用 `git archive` 逐 rev 取证）—— 所以两份报告"不是我引入的"这个**判断是对的**。
> 但"报告树基点(ec7342d72 / master a3a31da42)"这个**并列表述是本行任务简报自己的措辞，
> 不是报告里的原话**，逐字核对如下（两份报告的原文都在本树里）：
>
> - **`tasks/01a12b8aa6c87f21be882c61115d1ce4-report.md`（报告 B）根本没搞错。**
>   它在文末"提行（Rule 41）"第 1 条里就把机理**逐字说对了**：
>   "`9b88c4b5b` 把门禁唯一认得的 emitter 名字改了 —— `removalaudit.BranchDelete` →
>   `BranchDeleteAt`（`task_finish_cleanup.go:4307/4336`），而 `removal_audit_gate_test.go:69` 的
>   `removalAuditEmitBranch = "BranchDelete"` 在 `:132` 做**精确标识符比较**，于是两个合规的
>   调用点被判成 unrecorded"，并做了**因果实验**（`git checkout 9b88c4b5b^ -- …` → 绿；
>   还原 → 红），文末声明按 Rule 41 提了行 —— **而它引用的那个 id 在两台账本上都查不到**
>   （见第五节，这是本行的第二条实测更正）。
>   它的 `a3a31da42` FAIL 是**另一条测试**（`TestCommandsDoc*`，见其 §RUN A），
>   与这条红无关；这条红它是在**自己同步到 `ec7342d72` 之后的 tip** 上测到的。
> - **`devdoc/generated-doc-deps-absent-base.md`（报告 A）**说"fails on this branch **with my
>   changes reverted and the worktree clean**"，**它的 branch 就是 `task/01a12b8a`，该分支通过
>   交付点同步（Rule 30a）合入过 `ec7342d72`**（`27a98c0a7 merge: peer master ec7342d72
>   (delivery-point sync) into task/01a12b8a`，在同一棵树的 `git log` 里可见）—— 所以它测的
>   "clean tree"是**自己同步后的分支树**，读数正确，只是没标出那个基点 sha。
>
> 因此"陈旧基点假警报"这个定性成立，"报告把两个基点并成一句话"这句**不成立**：机理早被
> 正确诊断过，缺的是**锚定到具体 sha 的红/绿窗口 + "这条红现在还在不在任何 ref 上"**的
> 实测答案 —— 那才是本行的增量（第二节的矩阵 + 第五节）。

## 三、本树实测（Rule 31：真跑被改面，不是只跑单测）

门槛本体，本 run worktree HEAD `81e69eb97`：

```
$ go test -count=1 -v -run 'TestRemovalAuditGateEveryRemovalSiteEmits|TestRemovalAuditGateDataReport' ./internal/standards/
--- PASS: TestRemovalAuditGateEveryRemovalSiteEmits (2.58s)
    removal_audit_gate_test.go:775: released by removal-audit-ok:: internal/tools/task_reclaim.go:2007 worktree-removal (worktree remove) — visible, reviewed release, not a suppression
--- PASS: TestRemovalAuditGateDataReport (2.55s)
    removal_audit_gate_test.go:794: removal sites: total=24 map[emitting:23 escaped:1] (floor 15) kinds=map[branch-delete:14 worktree-removal:10]
    removal_audit_gate_test.go:801: declared arms with no production emit: [] (floor 15 arms referenced)
PASS
ok  	github.com/nick/gopi/internal/standards	6.041s
```

分支范围套件（Rule 27：SMOKE、按 diff 范围，不是整树）：

```
$ make test-budget PKGS='./internal/standards'
--- summary (94s wall, load 3.90 4.76 5.20 -> 5.96 5.22 5.32) ---
packages: 1 finished, 0 with a failure
stamp: 2026-10-12T01:07:47+08:00 host=hostb-Laptop pkg=./internal/standards budget=25m elapsed=94s exit=0 outcome=pass
  stream=run-1791738467-83081.json
```

## 四、复现配方（Rule 38：图必有 generator 与它被测的 commit）

本节所有数字都由下面的命令产出；**census 数字（24 站点 / floor 15）出自
`TestRemovalAuditGateDataReport`，被测 commit 是 `81e69eb97`**；窗口矩阵出自逐 rev 的
`git archive` 副本。临时归档目录必须 off `/`（Rule 22）：

```bash
# 0) 临时归档目录：设备号必须与 / 不同（这里是 66309 vs 66307）
SB=/home/user/.gopi/sessions/01a12bcc-1fff-7d8d-9409-f5d56277da05/artifacts/trees
stat -c 'device=%d %n' / "$SB"

# 1) 本树（当前 tip）—— 门本体 + 站点 census
cd /home/user/workspace/gopi-01a12bcc1fff7d8d9409f5d56277da05
go test -count=1 -v -run 'TestRemovalAuditGateEveryRemovalSiteEmits|TestRemovalAuditGateDataReport' ./internal/standards/

# 2) 归属：修复是不是本树的祖先
for r in a3a31da42 ec7342d72 fe6a971fa HEAD; do printf '%s: ' $r; \
  git merge-base --is-ancestor fe6a971fa $r && echo YES || echo no; done

# 3) 逐 rev 复现（git archive，绝不 checkout 别人的 worktree —— Rule 29）
for r in a3a31da42 9b88c4b5b ec7342d72 fe6a971fa 62cf75e30 81e69eb97; do
  mkdir -p "$SB/$r" && git archive $r | tar -x -C "$SB/$r"
  ( cd "$SB/$r" && go test -count=1 -run TestRemovalAuditGateEveryRemovalSiteEmits \
      ./internal/standards/ 2>&1 | grep -E '^(ok|FAIL)|is a branch-delete site' )
done

# 4) "硬编码名字"这一半：窗口内与窗口后的门各读一次
for r in a3a31da42 ec7342d72 fe6a971fa; do echo "--- $r"; \
  git show $r:internal/standards/removal_audit_gate_test.go | grep -n 'removalAuditEmit.*=' ; done

# 5) 当前 master（fetch 后核验，Rule 40/28 的只读读法）
git ls-remote peerbox refs/heads/master
git fetch --no-tags peerbox master && git merge-base --is-ancestor fe6a971fa FETCH_HEAD && echo "master carries the fix"
```

## 五、这条红现在在哪里还活着？—— 实测：哪里都不活了

`git ls-remote peerbox refs/heads/master`（2026-10-12T01:09:43+08:00）→
`fabcd113e4f1bc922f1c76c33a845b46a7737d49`；fetch 后核对
`git merge-base --is-ancestor fe6a971fa fabcd113e` → **YES**。

**点名：peerbox master `fabcd113e`（2026-10-12 01:09）已含修复，该红在 master 上不存在，
不需要再派发修复行。**

> 复查（2026-10-12T01:18，同一次 fetch 之后）：master 已前进到
> `ef8b69fd9`（`git log --oneline -1 peerbox/master`），`git merge-base --is-ancestor
> fe6a971fa peerbox/master` 仍为 **YES** —— 修复是被"包含"而不是"恰好等于"，所以 master
> 继续前进也不会把这个判定推翻；重新核验的门槛是**祖先关系**，不是上面那个 40 位哈希
> 是否还是 master tip（引用的哈希会过期，关系不会）。

**已经提过的那条 bug 行在本机与 peerbox 的账本上都查不到 —— 引用别人的 row id 前先核验它存在。**
报告 B 文末按 Rule 41 列的第一条是
`01a12bad-9876-7267-87df-69b8f1fc593c`（"集成 tip 上 `TestRemovalAuditGateEveryRemovalSiteEmits` 红：
`9b88c4b5b` 把门禁唯一认得的 emitter 名字改了"），**它描述的正是这条窗口**；而修它的 commit
`fe6a971fa` 在提行之后 12 分钟就落了地（10-12 00:10）。实测两个账本：

```
$ gopi task show 01a12bad-9876-7267-87df-69b8f1fc593c          # hosta
gopi task show error: task: no task with task_id="01a12bad-9876-7267-87df-69b8f1fc593c"
$ gopi task list --all | grep -E '01a12bad|01a12bac'           # hosta: 只命中本行 bcc
#01a12bcc-1fff-7d8d-9409-f5d56277da05  [in_progress] …
$ ssh peerbox 'gopi task show 01a12bad-9876-...'                 # peerbox（只读）
gopi task show error: task: no task with task_id="01a12bad-9876-7267-87df-69b8f1fc593c"
```

⇒ 那个 id 在**两台账本上都不存在**（报告 B 自己那台的行也没有）——它是一条**只在报告散文里
存在的引用**。所以本报告不对它的队列状态下结论；能下的结论只有一条：
**该 bug 行描述的现象在含 `fe6a971fa` 的任何树上都不复现（本矩阵第 4 行即判据的实测），
不需要为它派发执行行。**

仍在窗口内的 ref 只有一个：本机另一条进行中的分支
`task/01a12bb87fe8799fb56eb4b66bf0fb9c`（`62cf75e30`，基点 `a3a31da42`，实测红）——
那是**它自己的基点问题**，交付点同步（Rule 30a）合入新 master 后自然消失；
`devdoc/generated-doc-deps-absent-base.md` 里那句"pre-existing，不是我的"由此得到**归属上的补全**：
它是真的 pre-existing，来源不是那条分支的改动，而是集成 master 上的一次 commit 改名。

**未来的处置指路**：若这条门**再次**报 `UNRECORDED`，先读
`removalAuditDeriveVocabulary` 的派生结果（`TestRemovalAuditGateDerivesTheEmitterVocabulary`
会把它逐字打印出来：`worktree-removal [WorktreeRemove] | branch-delete [BranchDelete BranchDeleteAt]`），
再比对站点的实际调用名 —— **"门说没记录"与"真的没记录"必须先分开，判据是
`internal/removalaudit` 里有没有那个记录被构造出来**，而不是门认不认识那个名字。

## 六、本报告的两处自我更正（写下来，供后来者复核）

本报告在写它的过程中被实测推翻了两次，两次都改了正文，**记录在此**（Rule 41 的精神：证据推翻推论时，
改的是推论）：

1. **"基线"不是简报给的 `a3a31da42`，而是 `ec7342d72`。** 简报的验收条第 4 条要求
   "在 `a3a31da42` 的 archive 树上跑出同形红灯"。实测该 rev **绿**（`ok … 4.320s` / `ok … 3.947s`，
   0 条 UNRECORDED，两次独立运行）；同形红灯在 `9b88c4b5b` 与 `ec7342d72` 上各 2 条。
   归属结论不变（红非本分支引入），但**用来证明"基线红"的那棵树必须换成 `ec7342d72`**。
2. **简报的"报告树基点(ec7342d72 / master a3a31da42)"是简报自己的并列表述，不是报告原话。**
   逐字核对两份报告后（§二末尾），报告 B 的机理诊断**本来就是对的**、还做了因果实验。
   本报告不再指认任何报告"把两个基点并成一句话"。

**这两处更正的方向是一致的：本行真正缺的不是"报告错了"的裁定，而是把红钉到具体 sha 的窗口矩阵。**



- **没有改任何生产代码、门、或基线**：本行是归属判定 + 文档。`internal/` 一行未动
  （diff 只有本文件 + `DOC-INDEX.tsv` 一行）。
- **没有 reopen / 改 status 任何既有行**：`01a12bcc…` 是本行的 id；我没有权限也不需要
  去改别人的行。
- **没有改 `devdoc/generated-doc-deps-absent-base.md` 的措辞**：合并它属于别人的分支
  （`62cf75e30` 仍在窗口内），本行只在自己的交点上补写归属，避免跨分支编辑。
- **窗口内的那条分支 `task/01a12bb87fe8799fb56eb4b66bf0fb9c` 我没有动**（Rule 29），
  只做了只读读数（`git archive` + `git log`）。
- 本报告**不主张**窗口之前的更早历史里这条门一直绿：我只测了上表六个 rev，
  更早的树不在本行预算内。

## Verification

- Build: `go build ./...`（本 run worktree）→ 见 task-report.md `## Verification`；本行不改
  生产代码，构建面与 HEAD 同源。
- Tests（门本体）: `go test -count=1 -v -run 'TestRemovalAuditGateEveryRemovalSiteEmits|TestRemovalAuditGateDataReport' ./internal/standards/`
  → 2/2 PASS，`ok … 6.041s`，exit 0。
- Tests（分支范围套件）: `make test-budget PKGS='./internal/standards'`
  → `packages: 1 finished, 0 with a failure`，elapsed 94s，`exit=0 outcome=pass`。
- Ran（逐 rev 复现，逐字）:
  `for r in a3a31da42 9b88c4b5b ec7342d72 fe6a971fa 62cf75e30 81e69eb97; do (cd $SB/$r && go test -count=1 -run TestRemovalAuditGateEveryRemovalSiteEmits ./internal/standards/); done`
- Result: `a3a31da42 ok(0 UNRECORDED) / 9b88c4b5b FAIL(2) / ec7342d72 FAIL(2) / fe6a971fa ok(0) /
  62cf75e30 FAIL(2) / 81e69eb97 ok(0)` —— 红窗口两端闭合，与本树绿互证。
- Commit: 本分支 `task/01a12bcc1fff7d8d9409f5d56277da05` 的 tip（本文件 + `DOC-INDEX.tsv` 一行同 commit）。
- `gopi tool doccheck`（本 worktree）→ `the devdoc corpus is consistent`，exit 0。

## Evidence (self-filed)

原始输出全部落在本 run 的 session 目录（Rule 37）：
`~/.gopi/sessions/01a12bcc-1fff-7d8d-9409-f5d56277da05/artifacts/`

- `00-session-facts.txt` —— `GOPI_TASK_REPORT_PATH`、取证时刻、`stat -c '%d'` 的 `/`=66307
  与 artifacts=66309（Rule 22：归档树 off `/`）。
- `01-ancestry.txt` —— 六个 rev 的 `git log -1`、祖先关系矩阵、`fe6a971fa^` == `ec7342d72`。
- `02-archive-reproduction.txt` —— 逐 rev 复现的汇总表 + `BranchDeleteAt` 的引入 commit
  + 兄弟函数同 kind 的源码。
- `03-site-vs-emitter.txt` —— 每个 rev 的门词表两行与生产站点两行（机理的原文对照）。
- `04-this-tree-green.txt` —— 本树 `-v` 的完整绿输出（含 24 站点 census 两行）。
- `05-peerbox-master.txt` —— `date -Is` 与 `git ls-remote peerbox refs/heads/master` 的原始读数。
- `06-budget-standards.txt` —— `make test-budget PKGS='./internal/standards'` 的原始流。
- `cell-<rev>.txt` —— 六个 rev 各自的**完整**测试输出（含 exit 行）。
- `trees/<rev>/` —— `git archive` 出来的六棵树（设备号 66309，off `/`）。

`git status --porcelain`（本 worktree，提交后）→ 空（Rule 30）。
