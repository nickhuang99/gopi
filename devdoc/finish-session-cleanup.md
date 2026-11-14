<!-- doc-class: historical -->
# Task Report: client finish-cleanup chain (t-1788173679)

Epic: HP 交付完毕但 worktree+分支挂着没人收尾（feat/sync-conflict-completed
被 peerbox merge 后 HP 侧仍残留）。实现"client 收尾四步链"+ server 侧对应链。

## Completed

### 1. 四步链核心 `internal/tools/task_finish_cleanup.go`（新文件）
- `TaskFinishCleanupSessionID(taskID)` — 单行入口：
  ① **守卫**：行必须 terminal（completed/failed）+ deliverable 有 repo +
     **worktree clean**（`git status --porcelain` 非空 = 活 Bond 在写 →
     skip，永不拆台）+ 客户端节点（`tools.IsServerNode()` gate — server
     是集成分支，不给自己 push）。
  ② **push** `git push <peer0>:~/work/GoPi <branch>:<branch>` — 幂等
     （up-to-date exit-0，bug038 push-back 契约复用）。
  ③ **worktree remove** `git worktree remove <wt> --force` — 仅在 ② 成功后。
  ④ **branch -d** — -d 拒删未合并分支（双保险第一道）；若 -d 因"not fully
     merged"拒删但 ② 已成功（tip 已 server-held），才用 -D 兜底
     （**顺序即安全论证**：-D 只在 push 成功之后可达）。
  - **豁免**：master/main、`hp-unique-maxtokens-work`、`archive/*` 前缀
    分支永不清理；`GOPI_FINISH_CLEANUP_EXEMPT` 可追加（Rule 2 单点读）。
  - **fail-soft**：每步独立报告（`FinishCleanupStep{step,target,ok,detail}`），
    任何失败只记录不炸；失败链保留 branch+worktree（不丢工作）。
  - 分支/worktree 解析：deliverable backfill（M4.9 的
    `{"repo","branch","worktree"}`）优先，fallback `rev-parse --abbrev-ref
    HEAD` + `git worktree list --porcelain` 前缀匹配。
- `syncIsServerNode` → 导出为 `IsServerNode`（cleanup 需要同一个判定，
  内部 conflict-ruling 语义不变）。

### 2. session-end 钩子 `internal/agent/taskregister.go`
`finishSessionTask` 关账后立即跑四步链（`autoTaskRegistered` 且行已
terminal 才可能触发）；结果单行日志
`finish-cleanup: <id> branch=<b> done=[push,worktree,branch]` 或
`skipped: <原因>`。开跑条件天然收窄：无 deliverable 的行直接 skip。

### 3. serve tick sweep `cmd/agent/cli_tasks.go`
`serveFinishCleanupSweep()` — stale sweep 同款 pass step + 10min throttle
（`serveFinishCleanupThrottleMin`）：扫 terminal + deliverable 含 repo 的行
（`serveFinishCleanupQuery`，LIMIT 10 = `serveFinishCleanupScanMax`），
逐行跑链。兜底场景：session-end 进程被杀、钩子没跑到。skip 行不打日志
（routine），只有 done/failed 输出 `[serve] [finish-cleanup] ...`。

### 4. Server 侧对应链（说明性，代码在既有脚本）
server（peerbox pass）merge 分支→删本地分支：HP 的分支 refs 在 client push
时已到（无需 fetch），`scripts/safe-merge.sh <branch>` 通过 → merge →
`scripts/post-merge-cleanup.sh --auto`（feat/merge-branch-cleanup 交付，
--merged 判定 + archive tag + branch -d）完成 server 侧收尾。t-1788086858
b 项并入本单：不再新建 server 代码，复用脚本闭环。

## 实测（epic 四条验收，`go test -run TestFinishCleanup` 全过）
- ① clean worktree + terminal → 链走通：push OK→worktree 物理消失→
  branch 删净→server 裸库 `rev-parse --verify <branch>` 通过。
- ② dirty worktree（活 Bond 写 wip.txt）→ skip，零 step，worktree 完好。
- ③ push 失败（server 端构造 non-fast-forward）→ 链停在 step②，
  worktree+branch 完好（③④锁生效）。
- ④ master/main/hp-unique-maxtokens-work/archive/* 豁免（单测 + 端到端
  master 指向 → exempt skip）。
- 附加：幂等重跑（② up-to-date no-op / 已收尾行 clean-skip）。

## Not Done
- merge 由 PA/reviewer 决定（Rule 6d/9，不自行 merge）。
- server 侧"merge→删分支"没有新代码——按设计复用 safe-merge.sh +
  post-merge-cleanup.sh（如需全自动 server pass，属后续任务）。
- `deliverable.branch/worktree` 的 executor backfill 本身是 M4.9 交付，
  本单只消费不改动（未 backfill 的行走 HEAD/porcelain fallback）。

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK
- Tests: `go test -run TestFinishCleanup ./internal/tools/` 5/5 PASS;
  `go test ./internal/agent/ ./cmd/agent/` PASS（回归零破坏；
  全量 internal 套件后台跑完见下方附录）
- Commit: <见 git log feat/finish-session-cleanup>

## 设计要点（给未来 agent）
- 步骤顺序 = 安全论证：② 失败→什么都不删；③ 失败→branch 留；④ 的 -D
  只在 ② 成功后可达。四步链不可重排。
- dirty gate 保护的是"活 Bond"（feat/memsync-bidirectional 38 dirty 的
  教训——cleanup 永不与在写进程竞争）。
- fixture 用真 git（bare server + client + linked worktree），mock 会
  证不了 git 语义（-d 拒删、porcelain、worktree list）。
