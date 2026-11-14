<!-- doc-class: historical -->
# Task Report: nick 三问修复包 — 三层机制核查 + merge 后分支自动清理

分支 `feat/merge-branch-cleanup`，commit d8d4223（worktree ~/workspace/gopi-merge-cleanup，共享对象库）。

## Epic 核查结论（重要——与诊断有出入）

Epic 诊断"三层机制都缺位"，实测核查后修正为 **1 层缺位 + 1 层已修未察觉 + 数据残留**：

### a) serve tick 无条件 staleSweep — **已实现且在跑**（epic 的 t-1788052204 已被实现合并）

- master 4e22a64 已含 `serveStaleSweep()`：runTaskServe pass 第一步无条件执行
  （cli_tasks.go:848，M4.8 ①，ec035d3 合并 feat/stale-sweep-trigger @ 941b40c）。
- **实跑证据**：/tmp/serve20-t1788134698.log（本 epic 的 serve 进程）
  `[serve] [stale] reported 4 stale in_progress row(s) via GOPI_TASK_NOTIFY`
  —— 14:01:27 tick 真实扫出 4 行僵尸 + re-arm bump（updated_at 06:01:23-28 一致）。
- 节流：serveStaleThrottleMin=10min（Rule 1 常量）；DetectStaleTasks 内置
  zombie 证据链（pid 死活 signal-0 探活 + session mtime）+ GOPI_TASK_NOTIFY 事件投递。
- **闭环缺口不在代码**：notify 订阅端（GOPI_TASK_NOTIFY_EVENTS 默认不含 "stale"，
  需 `GOPI_TASK_NOTIFY_EVENTS=...,stale` 或 =all）+ verify agent 按 zombie 证据
  自动 reopen/cancel 的执行器（devdoc/stale-sweep-trigger.md 已列 Not Done）。

### b) merge 后自动分支清理 — **本次交付**（原本缺位，手工习惯未机制化）

新增 `scripts/post-merge-cleanup.sh`（与 safe-merge.sh 配对成前置守卫+后置清理）：

- 语义：对传入分支若已 `git branch --merged` 入主分支 → 打 archive tag
  （`archive/<branch>-<date>`，仅有独立提交时）→ `git branch -d`；未合分支
  一律跳过（绝不删未合工作——本工具不做裁决，裁决在 PA/safe-merge）。
- `--auto`：无参扫描全部已合分支（排除 master/当前 checkout），一次清完坟场。
- 防呆：master 本体/当前分支/不存在分支/worktree 占用分支 全部跳过并给原因；
  `--dry-run` 预演；`--force` 升级 -D（squash-merge 等非常规合并的显式通道）。
- Rule 1：BAD_PREFIXES/DEFAULT_MASTER/WORKTREE_POLICY 等全部命名常量；
  与 safe-merge.sh 共用 SAFE_MERGE_MASTER 环境变量约定。

### c) feat/m47-e2e-hp — **分支不存在，工作已入 master**

- 本地/远端（remotes/hp, remotes/peerbox）均无该分支；
- 其唯一实质提交 e4cd331（fix(m47): restore dispatch.go）已是 master 祖先
  （git merge-base --is-ancestor = YES）；a4ba5cc（feat m46-m47）同在 master。
- **无遗留交付物，无需处置**。epic 记录的"1 commit 待查"应为分支删除前的旧状态。

### d) #77/#76 僵尸行 — **已 cancelled**（t-1788015288 / t-1788014923 均 cancelled）

- 其余僵尸行现状：9 行 in_progress 中 6 行为陈旧僵尸（updated_at 06:01:23-28，
  即 14:01 sweep 的 re-arm bump——说明 sweep 报过 4 行事件）。
- 已派处置单：t-1788156794（按证据链逐行 cancel，pid 死/无 stamp + session mtime 超 30min）。

### 附带发现：hp-unique-maxtokens-work（5 commits ahead，2026-08-06 残留）

- 内容：max_tokens 无预算方案（feat/max-tokens-env + remove-max-token-limits 的
  残留 checkout，分叉点 8aaab43）。
- **败因定论**：其 live probe（无 max_tokens 成功）与 master 2026-08-22 A4 实证
  （devdoc/maxtokens-converged.md：端点硬强制 max_tokens，8192 cap 精确截断）
  矛盾；master 演化 a660cf31→af6e0cb(384K)→306c21c(16384 收敛) 证据更新。
- 已派归档单 t-1788157112（archive tag + branch -D + devdoc 记录）。

## 交付物验证（post-merge-cleanup.sh 沙箱实测，5 场景）

1. --auto：已合分支删除 ✓（feat/merged-branch 消失）、未合分支跳过 ✓
2. fast-forward 合并（ahead=0）跳过 tag 直接删 ✓
3. squash-merge（--merged 判 false）跳过 ✓；--force 显式强删通道 ✓
4. 不存在分支/master 本体跳过 ✓；幂等重跑（第二次 --auto 无事可做）✓
5. 主仓 dry-run 预演：唯一已合分支 fix/shot-vet-unreachable 因 worktree 占用
   被正确跳过（守卫生效——worktree remove 后才能清理）✓

## Not Done / 派单

- t-1788156772（verify）: m47 归属核查 → 自查已定论，该单转 no-op。
- t-1788156949（verify）: no-op 记录单（m47 已在 master）。
- t-1788156794（bug）: 6 行僵尸 cancel 处置（证据链）。
- t-1788157112（verify）: hp-unique-maxtokens-work 败者归档。
- notify 订阅端文档（GOPI_TASK_NOTIFY_EVENTS 加 stale）与 verify agent
  自动 reopen/cancel 执行器：stale-sweep-trigger devdoc 已列为后续迭代。

## Verification

- post-merge-cleanup.sh: bash -n 语法 OK + 5 场景沙箱实测全过（上述）。
- 主仓现状预演：--dry-run --auto 输出符合预期（占用分支跳过）。
- 分支: feat/merge-branch-cleanup @ d8d4223（主仓可见，worktree 共享对象库）。
- 本报告: devdoc/nick-three-questions-fixpack.md @ 同分支。

## 收尾清单（对照 Rule 9 三步）

1. 胜者 merge：feat/merge-branch-cleanup 待 PA 验收（bash scripts/safe-merge.sh
   feat/merge-branch-cleanup → merge → bash scripts/post-merge-cleanup.sh --auto）。
2. 败者归档：hp-unique-maxtokens-work 已派单（t-1788157112）；
   其余 4 个 worktree 占用分支（crash-dump/dedup-hard/foresee-memsync/merge-backlog）
   为进行中工作，不在坟场清单。
3. 复查：merge 后跑 post-merge-cleanup.sh --auto 即台账归零——本次交付的
   正是该步的自动化。
