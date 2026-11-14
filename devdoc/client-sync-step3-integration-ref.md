<!-- doc-class: normative -->
# 客户端 git sync 步骤③：从 refspec-less `git pull` 到「在持有者里 fast-forward」

**Task**: `01a12e64-23c3-7c10-a07c-b85aef54924d` · **Date**: 2026-10-12 · **Host**: host-b（client）
**File**: `internal/tools/task_sync_git.go` — `GitSyncClientPass` 步骤③

> 本文是 Tier B：设计理由与被否决的替代方案。规则区在 `GOPI.md`，本文件不新增规则。
> 交付报告（含 Build/Tests/Ran/Result/Evidence）在 `tasks/01a12e64-report.md`。

## 1. 症状与根因

**症状**（实测，非推断）：本机 `refs/heads/master` 冻结在 `d7fb65344`，落后对端 `peer1` 的
`refs/heads/master` **97 个提交**；而 `refs/remotes/gitPushcheck/master` 一直在动 —— 也就是
"同步看起来活着，集成分支死了"。同一条 fix 在 `origin/master` 上是 ancestor（rc=0），在本地
`refs/heads/master` 上不是（rc=1），于是产生过"master 上某测试恒红"这类**对错了 ref 的论断**。

**根因**：步骤③ 跑 `git pull --ff-only`，**不给 refspec**；`gitSyncRun` 只设 cwd=repo root。

`git pull` 不带 refspec 时合并的是**调用进程当前分支**（对该分支的上游）。于是：

| 调用方的 cwd | merge 腿合并谁 | 结果 |
|---|---|---|
| 主 checkout（在 master 上） | master（有 upstream） | 正常快进 —— "有时灵"的来源 |
| **worker 的 linked worktree**（`task/<id>`，无 upstream） | 无 —— `fatal: There is no tracking information for the current branch` | rc=1；fetch 腿照样更新 remote ref，ref 永不前进 |
| 任何分支**有** upstream 的 worktree | **那个分支** | `git pull` **rc=0 报 `Already up to date.` / success**，集成分支原地不动 —— **静默 success（Rule 15 的正例）** |

第三种形态最坏：报告印了成功，实情是"同步到了另一个 ref"。

## 2. 修法的形状：先问"谁持有这个 ref"，再决定怎么写

集成分支 ref 是**一个仓库的一条 ref**，所以只有三种局面：

```
① 无人持有   → git fetch <peer> refs/heads/<b>:refs/heads/<b>     # 未加 '+'；建 ref / 前进
② 有人持有   → git fetch <peer> +refs/heads/<b>:<mirror ref>      # 目标是非分支保留 ref
              + git -C <持有者> merge --ff-only <mirror ref>        # ref+index+worktree 一起前进
③ 持有者读不出来 → 拒绝并如实报告
```

② 的 `merge` 就是 `git pull` 的 merge 腿做的那件事，只是**瞄准了该瞄的 ref**；它天然满足
"ref 与 index/worktree 同步"这条不变量 —— 而**裸 ref 写入做不到**（`shared_integration_ref_guard.go`
的 2026-10-12 事故：主 checkout 的 ref 被移动、index 留在旧树 → 60 条 staged 路径 → **merge 队列 park**）。

### 三条实测事实（缺一条这个设计就立不住）

探针 fixture（linked worktree + 本地 bare peer + 第二个 clone 推进 peer master）的原始输出：
`<session>/evidence/probe-step3-shapes.txt`。

1. **`+refs/heads/<b>:refs/heads/<b>` 强制 fetch 不是一条可用路线。** 在**另一个 worktree 持有**该分支时，
   git 2.43 直接拒绝：`fatal: refusing to fetch into branch 'refs/heads/master' checked out at '<path>'`（exit 128）；
   持有者 `checkout --detach` 后它**会成功**，但那是**静默改写别人的 checkout**（index 留下差异）。
   这两面都排除它。而 client 主机上主 checkout 通常**就**持有 master —— 所以"直接 fetch 到 refs/heads"
   在真实拓扑里基本必然走不通。
2. **`git merge --ff-only <sha>` 不是可用目标。** 本地没有该 sha 时：`merge: <sha> - not something we can merge`。
   所以必须先 fetch 到一条 **ref** 再 merge。选的是仓库**既有的** peer 镜像 ref
   `gitPushbackCheckRef`（`refs/remotes/gitPushcheck/master`）—— 它由既有的 `gitPeerMirrorRefspec()`
   渲染（Rule 14），且**不是 refs/heads**，任何 worktree 都不可能持有 ⇒ 镜像这一步永远不会失同步任何人。
3. **无人持有时，未加 `+` 的 refspec fetch 被 git 接受**（exit 0，ref 被创建）。
   这是"主 checkout 停在别的分支 / 仓库还没有这个集成分支"的路线。

## 3. 被否决的替代方案

| 方案 | 否决理由（实测） |
|---|---|
| `git fetch <peer> +refs/heads/<b>:refs/heads/<b>`（强制写分支 ref） | 另有人持有时 git 拒绝（128）；持有者 detached 时**静默**改别人的 checkout；且用当前 cwd 时 git 会在**调用方**移动 HEAD 而不报错（见 §2 事实 1） |
| `git fetch <peer> refs/heads/<b>:refs/heads/<b>`（**不加** `+`） | 同样在"别人持有"时被拒（128）—— 所以它**只能**用在①；在②用它等于步骤③ 永远失败 |
| `git update-ref refs/heads/<b> <sha>` | 就是 `shared_integration_ref_guard.go` 记录的 2026-10-12 事故本体：ref 前进、index/worktree 不动 ⇒ staged diff ⇒ merge 队列 park。**明确不做**，也是本条修复坚持走 merge 的原因 |
| `git -C <repo> pull --ff-only <peer> <b>` | 在有持有者时 git 天然拒绝 ⇒ 需要 fallback ⇒ 那就是带着一个 refspec 的老代码，且**依然可能 rc=0 而 ref 未动**（旧第一腿） |
| 顺带做"持有者不可写时 detach 后再写" | **已写后删除**：写完之后发现那一臂**不可达** —— `--abbrev-ref HEAD` 返回分支名与 `HEAD` detached 不可能同时成立。删掉它之后，"持有者是谁"成了**唯一**要问的问题（更强的形状，也是一次自我纠正） |

## 4. 失败面的处理（Rule 15）

修好的形状**没有"成功但什么都没做"的出口**：

- `step.OK=false` + 原因（`PullFF = "skipped (<原因>)"`）；
- 原因分三类：worktree survey 读不出来（未判定的共享 ref 写入一律不放行）、fetch 失败、merge 不被 git 接受
  （**含本地 ref 与对端分叉** —— `--ff-only` 拒绝，客户端**从不** merge，与旧契约一致）；
- 成功句子只从**实测成功的 git 操作**渲染：`fast-forwarded to <sha>` 里的 sha 是**回读 ref** 得到的，
  不回显 git 的人类散文（旧实现被迫同时接受中英两种拼写，正是因为它在回显本地化文案）。

## 5. 观测与验证

- **收敛的观测量**：`refs/heads/master == <peer tip>`，且持有者 `git status --porcelain` 仍为空。
- 真机运行（`gopi task push`，cwd = 我的 task worktree，**不是**持有 master 的那个）：
  `d7fb65344 → 88efcaa97`，落后提交数 97 → 0，持有者 HEAD 同步前进且干净，我的 ground 未被切换。
- 回归测试三条（`internal/tools/task_sync_git_test.go`）：cwd 不是持有者时仍奉命前进、无人持有时创建、
  **"步骤报 OK ⇒ ref 必须已到对端 tip"**（Rule 15 那一臂，先红后绿实测 `pull="Already up to date."` 而 ref 未动）。

## 6. 边界（不在本行内）

- 分叉时的**强制**路径不做：与旧契约一致，分叉留给运营/人类。
- 本机 peer alias 未配成 git remote（`git ls-remote --heads peer1` ⇒ `fatal:`）；实测走的是
  host-b 自己的 `sync_peers=["peer1"]` + `ssh peer1` 这条路，与 `gopi task push` 的目标同形。
  这是本机环境的既有缺口，不是本行的面。
