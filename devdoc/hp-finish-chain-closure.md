<!-- doc-class: historical -->
# HP client 收尾四步链 + server merge 链 — 收尾报告 (epic t-1788173679)

## Completed
1. **client 四步链 merge 落地**：feat/finish-session-cleanup @ 8f7e824（bug041 主件）→ safe-merge 守卫 → merge c5677e3。
   四步链 = ①terminal+clean 守卫（dirty=活 Bond 保护）→ ②git push（幂等 up-to-date）→ ③worktree remove --force → ④branch -d（push 成功后 -D 兜底）；
   master/main/hp-unique-maxtokens-work/archive/* 豁免；全链 fail-soft（FinishCleanupStep 审计）。
   接线两处：session-end 钩子（taskregister.go finishSessionTask）+ serve tick sweep（cli_tasks.go，10min 节流 + LIMIT 10）。
2. **实测①**（GOPI_E2E_FINISH_LIVE 真链路，t-1788157112 → feat/memsync-bidir-m49）：push=Everything up-to-date OK、worktree remove OK；
   server merge 链闭环：master 已含 d76fb5a（0 ahead），bug041-repro/bug041b-repro 均证明内容无损。
3. **实测②③④**：单测矩阵全 PASS — dirty→跳过零步（TestFinishCleanupChainDirtyWorktree）、
   push 失败→branch+worktree 保留（TestFinishCleanupChainPushFailKeepsBranch）、
   master/hp-unique-maxtokens-work/archive→exempt（TestFinishCleanupBranchExempt）、幂等 push（TestFinishCleanupIdempotentPush）。
4. **post-merge-cleanup**：8 个已 merge 且 clean 的 worktree/分支对清理（crash-dump/effort-values/import-dedup-hard/merge-backlog/
   merge-branch-cleanup/review-suite/task-list-hash-taskid/shot-vet-unreachable）；
   4 个未 merge 分支（branch-adoption-chain/claim-broadcast/node-role-banner/memsync-import-atomic）保留。

## Bug 发现 + 修复（同 epic 闭环）
- **根因**（5-why）：repo=worktree 交付形态（executor 把 deliverable.repo 回填为 worktree 路径）→ 步骤③删掉该目录后
  步骤④ `git -C <已删目录> branch -d` exit 128、输出 "(no output)" → 分支残留。
  单测 fixture 的 repo=clientDir 掩盖了它（fixture 与 live 形态不一致 = 最小样本教训反例）。
- **修复** 83f73f1：branchDir 在 ③ 之前用 `git rev-parse --path-format=absolute --git-common-dir` 解析（worktree 还活着时），
  branch 步骤永远跑在主 checkout；回归测试 TestFinishCleanupChainRepoIsWorktree（repo==worktree 形态全四步 OK）。

## Not Done
- HP 侧 feat/sync-conflict-completed worktree 残留：HP 节点升级到 bd838bf 二进制后由其 serve tick sweep 自愈（本机无 HP 的 worktree 可清）。
- feat/memsync-bidirectional 38 dirty 的活 Bond：实测时已 clean（0 dirty），其 worktree+分支由四步链按设计移除；无代码改动。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK（master bd838bf）
- Tests: `go test ./internal/tools/ -run TestFinishCleanup`: 6/6 PASS（5 既有 + 1 新回归）
- Commits: 8f7e824 (bug041 主件) → c5677e3 (merge) → 83f73f1 (branch-dir fix) → bd838bf (merge)
- Ledger: t-1788157112 closed; branch/worktree inventory 14→4（仅未 merge 分支保留）

### 全量套件 solo 基线（t-1788177868，2026-08-31）
- 背景：merge 后两次全量 20m 超时 panic 均发生在 HP 并发任务同仓跑测试期间，判定为并发资源竞争放大慢测试而非代码级 FAIL（两个墙点单跑均 PASS 5.3s/6.6s）。
- 执行窗口：solo（pgrep go 为空）20:39:23 开跑；全量 `go test ./internal/... ./cmd/agent/ -count=1 -timeout 25m`
- 结果: 10/11 包 ok（含 cmd/agent 110.4s、tools 848.2s 跑完无 panic 无超时）；1 项 FAIL：
  `--- FAIL: TestBidir2DivergentTerminalsConverge (10.19s)` — "HP tags carry neither cancel-was-intent nor conflict"。
- FAIL 判读（隔离复跑 + 秒级竞态探针实证，非代码回归）：
  该测试未钉 sync peer 白名单（姊妹测试 TestBidirEpic2StateConflictExplicitReport 有 `t.Setenv(config.SyncPeersEnv, ...)`，
  本测试没有），断言依赖 a/b 两侧 updated_at 落在**同一秒**触发 equal-anchor 冲突盖 'conflict' 标签；
  时钟跨秒时走 `syncDeltaApplyNewer` 前向迁移（remote<local → already，无标签）→ L271 断言失败。
  隔离复跑 10 连：7 pass / 3 fail，与秒界碰撞概率吻合；`GOPI_SYNC_PEERS=hp,peerbox`（server-ruling 路径）可把失败率放大到 ~3/8，`GOPI_SYNC_PEERS=` 空时全 pass——环境白名单会翻转分支，实锤竞态而非回归。
  同为 688b7aa（feat/memsync-bidirectional, M4.9 addendum）引入的**测试健壮性缺陷**，merge 语义本身（848s 全量里其余 sync/bidir 测试均 PASS）无回归。
  建议（独立小单）：给该测试补 `equalizeAnchors` + peer 白名单钉死，或断言放宽为"conflict/sync-resolved/migrated 三类显式报告任一即可"。
- 结论: **基线健康**——无代码级 FAIL，无 panic，两次事故中的墙点（TestTaskSyncCompletedBeatsCancelled / TestFinishCleanupChainPushFailKeepsBranch）本轮全量均正常通过；唯一红项是上述已知秒级竞态测试缺陷。
