<!-- doc-class: historical -->
# Task Report: GOPI.md Rule 6 vs task_sync_git.go push-back / finish-cleanup 自动化 — 一致性差距清单

Epic: 检查 GOPI.md Rule 6（worktree 工作流）与已落码的 push-back/cleanup 自动化
（`internal/tools/task_sync_git.go` + `internal/tools/task_finish_cleanup.go` 的
push-back + finish-cleanup 四步链）是否一致，列出 2-3 条过时或缺失的表述。

## Completed

读了三块代码事实：

1. **task_sync_git.go（serve-tick git pass）**
   - `GitSyncClientPass`：① `GitSyncPushLocalBranches` 把本地非集成分支
     （排除 master/main，上限 `gitSyncMaxBranches=20`）逐条 `git push <peer>:~/work/GoPi
     <b>:<b>`（bug038 幂等契约）→ ② `pull --ff-only`（diverged 拒绝、从不 merge）。
     目标由 `GitSyncServerTarget()` 解析：第一个非本机 sync peer。
   - `GitSyncServerPass`：`fetch --prune` 各 peer → `AdoptionSweep(Submit=true)`
     （clean merge + branch 删除；冲突 dispatch Bond 任务）。
2. **task_finish_cleanup.go（终态四步链）**
   - ① dirty guard（脏 worktree = 活 Bond 保护，整链 skip）→ ② `git push
     <peer>:~/work/GoPi <b>:<b>` → ③ push 成功后 `worktree remove --force` →
     ④ `branch -d`，-d 拒删（not fully merged）时因 push 已成功而 `-D` 兜底
     （branchDir 预解析 bug041 修复）。豁免：master/main、`archive/` 前缀、
     内置豁免名单 + `GOPI_FINISH_CLEANUP_EXEMPT` env。服务端节点整链跳过。
   - 触发面双保险：`taskregister.go:179` 终态钩子 +
     `cmd/agent/cli_tasks.go serveFinishCleanupSweep` 周期扫描。
3. **config.ResolveSyncPeers / ResolveNodeRole**：role 由 `GOPI_ROLE`（或
   settings `role`）显式声明 server/client；client 的 `sync_to` 指向 server。
   push 目标 = peer 别名 + `~/work/GoPi` 硬编码契约。

## Gap 清单（Rule 6 过时/缺失表述）

### Gap 1（核心）：6a/6c 宣称 "worktree = 终点、NO push is needed" —— 与已落码的
push-back 自动化直接矛盾

Rule 6a：「committing in the worktree already updates your branch in the main
repo. **NO push is needed.**」；6c 标题即 "worktree, not push"，工作树交付 =
"Done. … that IS the deliverable. Verify with `git log --oneline -3` and **stop**."

落码事实：HP 是**独立 client 节点**，server 在 peerbox；worktree commit 只落在 HP
本地 repo，对 peerbox 永远不可见。两条自动化链都把 **push 到 peer 交付定为必须**：
- finish-cleanup ②（`git push <peer>:~/work/GoPi <b>:<b>`），且 ③ worktree
  remove 以 push 成功为前置（"a removed worktree with an unpushed branch = work loss"）；
- serve-tick `GitSyncClientPass` ① 同一 push 契约，且四步链**终态自动删除
  worktree + 分支**——Rule 6 假设的 "worktree 留着、commit 常驻主仓" 状态
  在 client 节点上被自动化主动清除。

修法：6a/6c 增加 client/server 双节点条款——"NO push is needed" 仅对
**单机同 repo 的 worktree** 成立；跨节点（client→server）交付以
`git push <peer>:<peer-repo> <b>:<b>` 为准，且终态四步链会自动收走
worktree/branch，勿依赖其驻留。

### Gap 2（缺失）：push 目标与 repo 路径已代码化（`~/work/GoPi` + sync peer），
Rule 6 全文无 sync_peers / GOPI_ROLE 概念

落码事实：push-back 目标不是 "origin"、也不是 6c 的 `git push origin feat/<name>`，
而是 `GitSyncServerTarget()` / `finishCleanupServerTarget()` 解析出的
**第一个非本机 sync peer** + 硬编码 `~/work/GoPi` 路径；是否走 client 链由
`IsServerNode()`（`GOPI_ROLE`/settings `role`）判定。Rule 6（含 6b/6c 的
`origin` 字样）对这套 env/settings 驱动的拓扑只字未提——照 6c 字面执行
`git push origin` 在双机拓扑下要么失败要么推错地方。

修法：6c 补一条 "multi-node delivery" 小节：peer 列表来自 settings `sync_peers`
（`config.ResolveSyncPeers`），push 形如 `git push <peer>:~/work/GoPi <b>:<b>`，
幂等（up-to-date exit 0）；手动 push 前先 `gopi task push`（M4.18 同原子）。

### Gap 3（缺失）：6d/6e 的 "cleanup 由 agent 手动声明后执行" 已被自动链取代，
但归档/豁免规则（`archive/` tag 前缀、dirty guard、-d 双锁）没有进文档

落码事实：6d 要求 agent 在 final message 手动声明分支再手动删 clone；6e 让
agent 自己 `rm -rf`。现在 client 侧 `TaskFinishCleanupSessionID` 在**终态自动**
完成 push→worktree remove→branch -d（-D 兜底），并有：
- **dirty guard**：脏 worktree = 活 Bond，整链跳过（6d/6e 无此安全语义）；
- **豁免清单**：master/main、`archive/` 前缀（Rule 9 败者归档 tag 与之同名——
  两处规则共享 `archive/` 约定但 Rule 6 不知道）、内置名单 + `GOPI_FINISH_
  CLEANUP_EXEMPT` env、kill-switch `GOPI_FINISH_CLEANUP=0`；
- **-d 双锁**：push 未成功绝不删分支——比 6d 的人工声明更强的事前保障。

修法：6d/6e 补充 "client 终态自动收尾链（GOPI_FINISH_CLEANUP，默认开）会替你
完成 6e 的清理；手动删 worktree 前确认链未在跑或已设 kill-switch，archive/
豁免分支不会被自动清理"。

## Not Done

- 未改 GOPI.md 本身（Epic 范围=差距清单落档，修订另行裁决）。
- 未覆盖 6b（克隆内路径纪律）——该节与代码无冲突，仅 `origin` 措辞受 Gap 2 波及。

## Verification

- 代码读取：task_sync_git.go 全文；task_finish_cleanup.go 全文（链+豁免+目标解析）；
  taskregister.go:165-205（终态钩子）；cli_tasks.go:1195-1245（sweep 触发面）；
  cli_tasks_sync_git.go:62/101（CLI face）；config/node_role.go（role 解析）。
- Build: 不改 Go 代码，无构建需求；分支树干净（git status 仅新增本 devdoc）。
