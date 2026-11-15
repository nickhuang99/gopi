<!-- doc-class: normative -->
# brief 的验收命令 `master...HEAD` 与 ground 的真实切出点

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**定案（2026-10-12，行 `01a12d66`，父行 `01a12d24`）**：task brief 里常见的验收条款
「`internal/` 零改动（`git diff --stat master...HEAD -- internal/` 为空）」，在**刚认领的
worker ground** 上**恒非空**——它量的是 ground 从 peer master **继承**来的改动，不是本 run 的
工作。条款的**意图**没问题，错的是它假定的**减数**。

## 一、这条措辞从哪里来：submitter，不是 gopi 的生成器

**实测**。briefing 文本由 `cmd/agent/cli_tasks.go:2095 taskRunBriefing` 拼装，只有三个 header：

```
[Task summary]   ← row.summary
[Briefing payload] ← row.payload
[Attachments]    ← attachment replica 路径索引
```

它**不生成任何验收措辞**。生产源码（`internal/` + `cmd/`，排除 `_test.go`）里把
`master...HEAD` 拼进文本的地方只有三处，且都是**对别人的分支**求值（从 master 视角看该分支，
语义正确，与本缺陷无关）：

```
internal/tools/task_adoption.go:533,536   diff --name-only/--stat master...<br.Name>
internal/tools/task_merge.go:1185         diff %s...%s --stat（合并侧）
```

行 `01a12d24` 的验收命令逐字出自**它自己的 payload**（`gopi task show 01a12d24-...` 第 85 行
`验收：...（git diff --stat master...HEAD -- internal/ 为空）`），该行由 LLM session
自己 `task_submit` 提行（`submitted_by: LLM session 86f80730-... (task_submit)`）。

⇒ **面不在「生成器」，在 spawn 缝**：`runTaskRunOnce` 是 gopi 唯一能在子进程读到 briefing 之前
往里追加子句的地方（已有 ground 公告、authority 子句、evidence 目的地、premise freshness 四条）。
修复落在这里 = 在既有约束内创造，不新增面、不新增 knob（Rule 35）。

## 二、根因（5-why）与实测

1. **现象**：docs-only 的行，在动第一行文件之前，命令就返回非空。
   - 行 `01a12d24`（只改一个 markdown）：`11 files changed, 1654 insertions(+), 99 deletions(-)`
   - 本 epic 自己的 ground（尚未提交）：`19 files changed, 3952 insertions(+), 121 deletions(-)`
2. **机制**：`master...HEAD` 是三点 diff，减数取**本机** `refs/heads/master`。
3. **而 ground 不是从本机 master 切的**：`taskGroundPeerBaseline`（epic worktree-base-align）
   让新 ground 从 **peer 的 master tip** 切出，因为客户端本机 master 的滞后会随时间增长。
   - `git rev-list --count master..HEAD` = **83**（本 run）/ **55**（行 `01a12d24`）
4. **故该 diff 量到的是继承人口**：peer 上**其他任务已合并**的工作。
5. **规格**：把「本 run 的产出」写成了「本机 master 与本分支的差」。

**排除另一种解释（不是「分支真领先本机 master」）**：
`git merge-base --is-ancestor master HEAD` → **exit 0**；`git rev-list --count HEAD..master` = **0**。

## 三、修复：在 spawn 缝追加「切出点基线」子句

新增 `internal/tools/task_ground_cutpoint.go`：

- `GroundCutPoint(repo, branch)` —— 从**分支自己的 reflog** 读切出点：
  `git reflog show --format=%H <branch>` 的**最旧一条**。
  - 为什么是 reflog：它是 git **原生**的来源记录，由地面的创建命令
    （`taskGroundCreateFrom` 的 `git worktree add -b`）写下；`internal/tools/task_merged_cleanup.go`
    已用同一读法判断「这个分支提交过没有」（Rules 8/13：复用，不造第二个探针）。
  - **零新依赖**（Rule 35）：没有新 ref、新文件、新 env、新 settings key、新脚本——
    切出点本来就在创建那一刻被 git 记下了。
  - 取**条目本身**而不是它的 message：reflog 记的是每个位置指向的 commit，
    所以「创建条目的 commit」就是切出点，`Created from HEAD` 这类 subject 也不影响读数。
  - 读不到（无 reflog / git 报错）→ `Measured()==false`，**明确写出未测量**，
    绝不把 `master` 当兜底（Rule 15 + 本 epic 的成因就是「拿 master 当基线」）。
- `TaskGroundCutPointClause(repo, branch) (clause, record)` —— 渲染子句与缝的记录行；
  子句给出**三条可执行判据**（本 run 已提交 / 本 run 未提交 / 单提交），并**点名被反驳的拼法**。
- `cmd/agent/cli_tasks.go`：在 premise 子句之后追加（缝上第 5 条子句），记录行常量在
  `cmd/agent/task_child_env.go`（`taskGroundCutPointLineFmt`）。
- **submitter 的 prose 一字未改**：子句只补「该按什么重算」，不重写别人的验收条件。

## 四、给下一个读者的最短判据（在刚认领的 ground 上）

| 问题 | 命令 |
|---|---|
| 本 run 改了哪些文件（含未提交） | `git diff --name-only` / `git diff --stat -- <path>` |
| 本 run 提交后对某目录是否零改动 | `git diff --stat <切出点>..HEAD -- <path>` |
| 切出点是什么 | `git reflog show --format=%H <branch> \| tail -1`（最旧一条） |
| 本 run 的单个提交改了什么 | `git show --stat <sha> -- <path>` |
| 分支是否分叉 | `git merge-base --is-ancestor master HEAD`（0 = 未分叉） |

**不要用 `master...HEAD` 作为「本 run 零改动」的判据**：客户端 ground 上它恒非空。

## 五、与既有结论的关系（含被推翻的一条）

- `tasks/01a12d24-report.md` 提出「改用 peer ref / `origin/master`」。**实测被推翻**：
  本 run 的 `origin/master`（`6fa57c7ec`）**不是** HEAD 的祖先
  （`merge-base --is-ancestor origin/master HEAD` → exit **1**），而
  `gitPushcheck/master` 指向的才是切出点——且该 ref 是 pushback 通道的**一次性镜像**，
  在一次 run 内就被重写（实测三次读数：`02476ccde` → `02476ccde` → `6fa57c7ec`）。
  ⇒ **peer ref 也不是稳定基线**；reflog 的创建条目才随分支走。
- 不加新 ref、不在 claim 期 stamp base 字段：前者是依赖（Rule 35），后者属系统层（Rule 28）。

## 六、验证与证据

- 单测（工具侧，含**同一条命令的两个读数对照**）：
  `go test ./internal/tools/ -run TestGroundCutPoint -count=1` →
  fixture 上 `master...HEAD` = `2 files changed, 4 insertions(+)`，`<cut>..HEAD` = 空。
- 端到端（真实 `gopi worker` 缝）：`go test ./cmd/agent/ -run 'TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree|TestGroundAnnouncement'`
  → PASS；round log 逐字：
  `briefing cut point → branch task/...: cut at 083bc16 (evidence: git -C ... reflog show --format=%H ... ⇒ exit=0 )`
- red 证明：本子句上线前，标签在 HEAD 中不存在——
  `git grep -c "GroundCutPointLabel" HEAD -- internal cmd` → **exit 1**（0 命中）。
- 本 run 的实测对照：`git diff --stat master...HEAD -- internal/` = 19 files/3952 insertions，
  而 `git diff --stat <切出点>..HEAD -- internal/` = **空**（证据落 session 目录
  `artifacts/criterion-vs-cutpoint.txt`、`baseline-candidates.txt`、`peer-ref-stability.txt`）。
