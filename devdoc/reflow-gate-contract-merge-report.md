<!-- doc-class: historical -->
# Task Report: Epic merge fix/reflow-gate-contract → master

Epic 参数勘误：Epic 给的 tip `07201cd` 实为分支的 fork 点（merge-base，
它也是 master 上的 docs 提交）。分支真实 tip = `57f72ab`（本地与 peerbox
remote 一致）；"ahead: 20" 实为 11/2（master 11 ahead / branch 2 ahead）。
Epic 元数据过期，不影响裁决。

## Completed
- **定位**：`git fetch peerbox`；分支检出在 worktree `/home/user/workspace/gopi-reflow-gate`（tip 57f72ab = peerbox/fix/reflow-gate-contract）。本分支即 #1217 收尾时排队的 bug 任务 t-1788266431 的产物。
- **diff 审阅**（真实内容 = `git diff 07201cd..57f72ab`，9 files +324/−89）：
  - `c4745d9` fix(sync)：① **双前缀 disarm 修复** — eef8342 把 gate tag 设成完整 `from-<node>` 而 LIKE pattern 又加 `from-`，实际匹配 `from-from-<node>` 永不命中 → 自门禁失效；新 `syncGateTag()` 只取裸节点名，前缀只加一次（TestReflowGateTagNormalization 回归守卫）。② **Peer 接线（M4.24）** — `syncPeerSession` 与 `SyncRemoteServe` 现在传 `Peer=` 对端，门禁从 from-<self> 语义升级为 from-<peer> 语义（把对端的行转发回去才是 reflow）；v1 CLI 零参通道保留 legacy self-gate 契约。③ **Terminal homecoming 豁免（M4.20-E）** — 终态行（completed/failed）即使带 from-<gate> 也可出口（verdict 必须回家），live 行维持严格排除。④ push 路径改为 per-peer export（共享 export 无法表达 per-peer 排除）。
  - 测试契约更新：reflow 测试改钉 from-<peer> 语义 + legacy fallback + per-destination 中继合法性；bidir2 e2e echo 计数 2→1（被门禁掉的正是 reflow）。
  - `57f72ab` docs：devdoc/reflow-gate-contract.md（+79）。
- **零冲突证明（动 worktree 前）**：`git merge-tree 07201cd master branch` → 0 markers；`git merge --no-commit --no-ff` → "Automatic merge went well"；master 自 fork 以来改的文件与分支改的文件 **零交集**（comm -12 空）。
- **safe-merge.sh**：首次 exit=1 [c] worktree 占用（分支检出在 gopi-reflow-gate —— by-design，#1217/#1214 先例）；worktree 经 `git status` 确认干净后 remove；复跑 **exit=0 全过**（[a] 无 archive tag、[b] 名字合规、[c] 无占用、[d] 未 merge）。
- **merge**：`git merge --no-edit fix/reflow-gate-contract` → ort 干净，**merge commit `b47d994`**（--no-ff，无 force）。`merge-base --is-ancestor` = TRUE，`rev-list --count master...branch` = 12/0（branch 侧 0 behind）。
- **验证**：`go build ./...` OK；targeted `go test -run 'TestReflow|TestBidir|TestSyncSelfTag|TestPushTerminal'` OK（14.3s）；`go vet ./internal/tools/` OK。

## Not Done
- 未删除分支 fix/reflow-gate-contract：胜者分支无 archive tag（Rule 9 胜者路径），分支留在 master 之上可达；远端 peerbox 侧清理归 host-side operator（#1207 惯例）。
- 未跑全仓库 `go test ./...`（按 #1207 配方，targeted 覆盖本分支触碰面即可）。

## Verification
- Build: `go build ./...` → OK
- Tests: targeted reflow/bidir/syncselftag/pushterminal → `ok github.com/nick/gopi/internal/tools 14.289s`；`go vet` OK
- Full-suite note: 并发全量跑 `./internal/tools/` 出现 TestMemorySearchNonBlocking（及一次 TestJobNoNoticeOnKill）间歇失败 —— 单跑均 PASS（0.4s），与 fork 点 07201cd 基线 detached worktree 对照同样单跑 PASS —— 已知并发/isolation flake（memory #1198/#1079，devdoc/preexisting-test-reds-fix.md），**非 merge 引入**。reflow 契约测试在 merge 后全绿（本分支的目的就是修它们）。
- Guard: `scripts/safe-merge.sh fix/reflow-gate-contract` exit=0（worktree 解除占用后）
- Merge commit: **b47d994**（master），无冲突、无 force
- 主仓提交确认：worktree 共享 refs，commit 已在主仓 master（`git log master` 可见 b47d994）
