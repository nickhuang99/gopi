<!-- doc-class: historical -->
# Task Report: backlog-drain — 消化 hp/* merge-backlog 台账 + 存量 unmerged 分支

Task: t-1788275343 sub-task (Epic: post-receive hook v5 实装后，用主台账逐个消化
pending merge-backlog 任务，验收=merge-backlog pending 归零 + git branch 收敛 +
build/test 全绿 + master log 显示 merge 批次)。

## 结论（TL;DR）

**10 个目标分支已全部并入 master（0 ahead），21 行 hp/* merge-backlog 台账已全部
归零（pending=0），git branch 已收敛到 验收状态，master 全绿。** 本任务实际执行时
发现：merge 工作已由 post-receive hook v5 + merge-executor 的自动落地完成，
剩余工作 = 验证 + 清理证据链 + 台账收尾 + 报告。

## 背景：为什么 merge 已完成

- post-receive hook v5（nick 定稿：push 后直接 `git merge` 分支入 master，
  git=裁决者）已实装于 scripts/git-hooks/post-receive + scripts/install-hooks.sh。
- 上游会话（t-1788275389，glm，in_progress）与本任务并行，已删除 10 个已并入分支。
- 本任务验证：**merge-backlog pending 归零 + 分支收敛 + master 全绿 + 证据链**。

## Completed

1. **10 个存量 unmerged 分支全部确认已并入 master（`git rev-list --count master..<b>` = 0）**：
   - docs/revert-verify, docs/sim-merge-probe, feat/branch-scan-archive-filter,
     feat/e2e-live, feat/hook-test-probe, feat/pushback-chain-report,
     feat/test-a-doc-review, feat/test-b-rule6, fix/reflow-gate-contract,
     fix/tasklist-issued-at-throttle
   - master log 确认 merge commit：
     f1826fd (revert-verify), 3f0e268 (sim-merge-probe), 303edd3/652ea2a
     (branch-scan-archive-filter), f346c59 (e2e-live), 90bef03/fd350f5
     (hook-test-probe), c648da8 (pushback-chain-report), 48ffabf/793e483
     (test-a-doc-review), 90e0b83/3c0d392 (test-b-rule6), 8c9c47d/b47d994
     (reflow-gate-contract), 5ff7481/bb3d4e1 (tasklist-issued-at-throttle)

2. **hp/* remote tracking refs 全部 0 ahead**（hp/docs/*, hp/feat/*, hp/fix/*,
   hp/master — 均为陈旧 tracking ref，内容已并入 master）。

3. **21 行 hp/* merge-backlog 台账归零**：
   - 全部 auto 生成的 merge-backlog 行均已 terminal（cancelled/completed/verified）；
   - `gopi task list --status pending` 中 merge-backlog 行 = **0**（验收达成）；
   - 唯一非 terminal 的是 t-1788275389（21 行 drainer 本身，sibling in_progress，非本任务范围）。

4. **3 行僵尸 failed merge-backlog 行按证据链 verified 关闭**（`failed → verified`
   是台账允许的唯一合法转移，verified=已核查关闭）：
   - t-1788218295 hp/fix/memsync-import-atomic — 分支已不存在（内容已入 master）
   - t-1788218223 hp/feat/task-id-collision — 分支已不存在
   - t-1788218212 hp/feat/serve-tick-git-sync — 分支已不存在

5. **master 验证**：clean worktree (gopi-backlog-drain) `go build ./... &&
   go vet ./... && go test ./... -count=1` — 结果见 Verification 节（detached job）。

6. **分支收敛**：`git branch` = master + hp-unique-maxtokens-work + 进行中分支
   (feat/merge-executor-live, feat/backlog-drain, feat/bug05x, feat/task-merge-cli,
   feat/git-merge-harness-report, archive/dead-code-sweep-20260902)。
   10 个目标分支已全部 `branch -d`（由 sibling 会话删除，本任务复核=已删除）。

## Not Done / 说明

- **未 merge 任何分支**：10 分支已由 hook v5/merge-executor 提前落地（0 ahead）。
  强行重新 merge 无意义（空操作）。本任务以验证+证据链收尾。
- **feat/task-merge-cli 未删除**：pending 行 t-1788275530 声称其"0 ahead 已并入"，
  但实测 **2 ahead**（10dd2e6, 98bc053 未入 master）——该行 premise 错误，删除会丢
  工作。保留分支与 worktree（gopi-taskmerge-cli），由后续会话决定 merge 或 archive。
- **2 行 pending（t-1788275505 verify / t-1788275530 cleanup）** 属 sibling 会话
  (t-1788275389) 的自助行，非 merge-backlog 行；本任务不代其 cancel。
  merge-backlog pending=0 已达成，不受影响。
- **hp-unique-maxtokens-work 豁免**（Epic 明示：历史竞争残留，archive 处理不在本任务）。
- **未动 scripts/install-hooks.sh**（untracked，任务约束）。

## Verification

- Build: `go build ./...` — OK（clean worktree gopi-backlog-drain, master 45e452d）
- Vet: `go vet ./...` — OK
- Tests: `go test ./... -count=1 -timeout 30m` — **12/12 包全 PASS**：
  - cmd/agent 139.16s, internal/agent 122.92s, internal/checkpoint 0.01s,
    internal/config 0.08s, internal/deepseek 0.02s, internal/gopilog 0.44s,
    internal/proxy 0.03s, internal/shot 1.01s, internal/testutil 0.01s,
    internal/text2img 0.26s, **internal/tools 667.99s**, testsuite 0.62s
  - 注：首轮 `-count=1`（默认 10m timeout）在 internal/tools 包 600s 超时 panic——
    本机环境问题（memory #1198：internal/tools 单包需 ~13m，sqlite t.TempDir I/O，
    master baseline 同样复现），非 merge 回归。按配方以 -timeout 30m 重跑全绿。
- 冲突标记: `grep -rn "^<<<<<<<"` 全树 = 0（clean）
- Commit: 见 Git 节

## Git

- 分支：feat/backlog-drain（本任务交付分支，worktree ~/workspace/gopi-backlog-drain）
- master tip：45e452d（Merge branch 'feat/bug055-stuck-toolful'）
- 恢复命令：`git show feat/backlog-drain:devdoc/backlog-drain-report.md`
