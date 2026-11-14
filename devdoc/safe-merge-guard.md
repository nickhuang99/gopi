<!-- doc-class: historical -->
# Task Report: 竞争收尾防护（safe-merge.sh + GOPI.md 规则）

## Completed
- **scripts/safe-merge.sh**（可执行，`bash scripts/safe-merge.sh <branch>`）—— merge 前守卫，4 项检查：
  - a. **archive tag 检查**：`git tag -l "archive/<branch>"` 存在 = 已存档（败者）→ 拒绝（exit 1）
  - b. **分支名黑名单**：`loser/*`、`archive/*` 前缀 → 拒绝
  - c. **worktree 占用检查**：`git worktree list` 挂着该分支 → 默认拒绝；`SAFE_MERGE_WORKTREE=warn` 降级为仅警告
  - d. **未 merge 提示**：对比 `git branch --merged <master>`（默认 master，`SAFE_MERGE_MASTER` 可改）→ 信息输出不拦截
  - 全部通过 → ✅ + exit 0
- **GOPI.md Rule 9「竞争收尾流程」**（Layer 3 纪律）：民主裁决后立即 3 步
  （① 胜者 merge master ② 败者 tag archive/<name>-<date> + branch -D + worktree remove ③ branch + worktree 复查）+ **merge 前强制跑 safe-merge.sh**
- 测试：合成场景 6 用例全部验证

## Not Done
- 未接入 git hook（如 pre-merge-commit / prepare-commit-msg）—— 当前为手动前置守卫，epic 未要求 hook

## Verification
- Build: N/A（纯 bash 脚本，`bash -n` 语法通过）
- Tests: 合成仓库 6 用例全过：
  - 假败者（tag archive/test-loser + 分支存在）→ **拒绝** exit=1 ✅
  - 普通分支 feat/normal → **通过** exit=0 ✅
  - loser/ 前缀无 tag → **拒绝** exit=1 ✅
  - worktree 占用 → **拒绝** exit=1 ✅
  - worktree 占用 + SAFE_MERGE_WORKTREE=warn → **通过** exit=0 ✅
  - archive/ 前缀 → **拒绝** exit=1 ✅
- Commit: `eea04a8`（feat/safe-merge 分支）+ `9184e19`（merge 进 master）

## Pitfalls（给未来 agent）
1. bash 守卫工具会拒绝 workdir 外的 `rm -rf`、相对路径脚本调用、shell 变量路径 —— 测试脚本要写成文件放 worktree 内、路径用绝对 path、清理用逐条 rm。
2. worktree 的分支不可在同一 worktree 切换 checkout 别的分支（fatal: already checked out）—— merge 到 master 要在主仓库（git op 不动他人脏文件）。
3. `git worktree list` 第 3 列即 `[branch]`，占用检查 `grep -q "\[$BRANCH\]"` 最简洁。
