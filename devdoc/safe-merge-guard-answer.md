<!-- doc-class: historical -->
# Task Report: safe-merge.sh 拦截的三类分支 + 分支清理流程历史记忆

## Completed
- 通读 `scripts/safe-merge.sh`（blob `0d7e4c2`，主分支 `/home/user/workspace/gopi-master-check` @ ca27da4）并逐条对应行号。
- 实测三条拦截路径 + 一条对照路径（临时 tag / `loser/` 分支 / worktree 占用 / 干净分支），全部复现，探针已清理。
- 检索 hippocampus 记忆中关于分支清理/收尾流程的历史经验（safe-merge 守卫、post-merge-cleanup、finish-cleanup 四步链）。

## Answer 1 — safe-merge.sh 拦截哪三类分支

用法 `bash scripts/safe-merge.sh <branch>`：**merge 前置守卫（Layer 2 兜底）**——全部通过才 `exit 0 + ✅`，任一拦截项触发即 `exit 1 + ❌`。

| # | 拦截类别 | 判定语句（行号） | 拒绝条件 | 降级开关 |
|---|---|---|---|---|
| **a** | **已存档的败者分支**（有 archive tag） | `L41: git tag -l "archive/$BRANCH" \| grep -q .` | tag 存在 = 已归档（败者）→ 拒绝 | 无（硬拦截） |
| **b** | **黑名单前缀分支名** | `L49-58: case "$BRANCH" in "$prefix"*)`，`L23: BAD_PREFIXES=("loser/" "archive/")` | 命中 `loser/` 或 `archive/` 前缀 → 拒绝 | 无（硬拦截） |
| **c** | **仍被 worktree 占用的分支** | `L62: git worktree list \| grep -q "\[$BRANCH\]"` | worktree 仍挂着它 = 未清理 → 默认拒绝 | `SAFE_MERGE_WORKTREE=warn` 降级为警告 |

第 **d** 项（`L75: git branch --merged "$MASTER" --list "$BRANCH"`）**只做信息输出，不拦截**——仅提示该分支是否已合入主分支（`SAFE_MERGE_MASTER`，默认 `master`，可设 `main`）。

常量集中（Rule 1）：`BAD_PREFIXES`（L23）、`DEFAULT_MASTER="master"`（L24）、`WORKTREE_DEFAULT="deny"`（L25）。

**实测证据**（在临时 worktree `~/workspace/gopi-branch-cleanup-mem` 内跑，探针 tag/branch 已删）：
```
❌ 拒绝: 分支 'probe-3079472' 已有存档 tag 'archive/probe-3079472'      # a  → exit 1
❌ 拒绝: 分支名 'loser/probe-3079472' 命中黑名单前缀 'loser/'           # b  → exit 1
❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>      # c  → exit 1
✅ safe-merge: 全部通过 —— 可安全 merge 'ctrl-3079560'                  # 对照 → exit 0
```

## Answer 2 — 过去会话中关于分支清理流程的记忆

- **规则层（GOPI.md Rule 9，记忆 #911 —— 该记忆记录了脚本的诞生与 6 场景验证）**：竞争/交付收尾固定 **3 步** —— ① 胜者分支 merge 到 master（PA 验收通过后）；② 败者分支归档清理：`git tag archive/<name>-<date>` + `git branch -D <name>` + `git worktree remove <path>`；③ 复查 `git branch` + `git worktree list`，确认只剩 胜者/master/进行中分支。**任何 merge 前必须先跑 `bash scripts/safe-merge.sh <branch>`，✅ exit 0 才允许 merge**；被拦截 = 该分支是败者或未清理，立即回到第 2 步归档，**绝不强行 merge**。
- **脚本闭环（记忆 #1163）**：`scripts/post-merge-cleanup.sh` 与 safe-merge.sh 配对 = **前置守卫 + 后置清理**。Rule 9 收尾三步的落地形态：`safe-merge.sh` 守卫 → merge → `post-merge-cleanup.sh --auto`。清理脚本语义：`git branch --merged` 判定 → 有独立提交（ahead>0）才打 `archive/<branch>-<date>` tag → `git branch -d`；`--auto` 全扫已合分支，`--dry-run` 预演，`--force` 是 squash-merge 通道的 `-D`；防呆跳过 master/main 本体、当前 checkout、worktree 占用、不存在的分支；**未 merge 的分支一律跳过 —— 只清坟场，不做裁决**。
- **客户端交付收尾（记忆 #1171）**：`internal/tools/task_finish_cleanup.go` 四步链 —— ① 守卫（terminal + 有 `deliverable.repo` + worktree porcelain clean + 非 server 节点）② `git push` ③ `worktree remove --force`（仅 ② 成功后）④ `branch -d`（`-D` 仅在 push 成功后兜底）。**顺序即安全论证，不可重排**；豁免 master/main/`archive/*` 等；server 侧无新代码，仍走既有脚本闭环。守卫坑：**dirty worktree = 活 Bond 正在写 → 整链跳过，保留 branch+worktree 不丢工作**。
- **并发 / 陈旧前提的坑（记忆 #1227）**：动手删之前必须重新勘察 `git branch` + `git worktree list` + `git for-each-ref refs/remotes` —— epic 里列的分支清单可能已过时。**绝非本地分支的远程跟踪引用（如 `remotes/hp/*`）不是我们的，不该删**（fetch 会重现）。删除顺序：先 `git worktree remove`，再 `git branch -d`（0-ahead 祖先用 `-d` 即可；`-D` 仅用于 push 已成功但未完全合并时）。**safe-merge.sh 只管 merge，不管删除。**
- **merge 竞态的坑（记忆 #1431）**：分支仍被 worktree 占用时 safe-merge 会拦——先 `git worktree remove`，重跑守卫到 exit 0 再 merge；merge 后要按**调用方**（而非管道）grep 验证最后一笔 commit 真的落地到 master（曾发生 merge 抢跑，master 上的修复因缺 caller 而失效）。

## Not Done
- 未修改任何脚本/代码（纯只读调查任务）。
- 未 merge 本分支（Rule 6/9：不自行 merge，由 PA 验收后处理）。

## Verification
- Build: N/A（未改代码）
- Tests: 守卫四路径实测 —— a/b/c 均 ❌ + `exit=1`，干净分支 ✅ + `exit=0`；探针 tag/branch/worktree 已全部清理（`leftover: 0`）
- Commit: `feat/branch-cleanup-mem-answer`（worktree `~/workspace/gopi-branch-cleanup-mem`）
