<!-- doc-class: historical -->
# Task Report: Foreseeable Review — branch-backlog audit (merge 坟场治理设计)

## Completed
- 预见性审查：db-sync push-back feat/* 分支堆积问题的修复设计
- 确认现状：peerbox 已有 2 个本地未 merge 分支 (feat/crash-dump,
  fix/memsync-import-atomic) + hp-unique-maxtokens-work + 5 个 hp remote
  分支 —— 分支坟场已存在, epic 的判断属实
- Epic 两个修复方向的裁决: **② serve tick 分支清点 → 自动 submit
  'merge backlog' 任务** 为主机制（无状态清点, 能捕获存量漂移, 不依赖
  push/sync 事件发生）; ① memsync 'branch-pushed' 提示任务降为后续可选
  （只在 push 时触发, 覆盖不了 serve 不常驻期间的存量）
- 双通道 E2E 可行性实测（设计前验证）:
  a. memsync import-tasks 通道: 2 行 patch → queued=2, from-hp tag 强制,
     task list/show 渲染正常 → ① 的投递链路真实可用
  b. task submit 引擎: TaskSubmit 链路、dedup hash 门、repo deliverable
     验证链路均已在 master, ② 的落库路径零新概念
- 设计文档 devdoc/foresee-review-branch-backlog.md 已落盘并提交:
  发现机制 (for-each-ref + rev-list count)、branch+tip 去重规则、
  submit 行 shape、serve tick 集成点 (serveStaleSweep 同款 pass step +
  15min throttle)、验收脚本规格 (造 2 分支→台账 2 条 merge 任务)、
  风险清单 (台账 spam/多节点审计/degrade-don't-die)

## Not Done
- 实现阶段 2（task_merge_backlog.go 引擎 + serve tick 集成 + 单测 +
  e2e 脚本）—— 本会话为预见性审查+设计定稿, 代码实施是下一个任务
- Option ① memsync branch-pushed hint 的实施（deferred, 设计已记录）

## Verification
- Build: go build ./... OK (worktree feat/merge-backlog @ 1146c87)
- Tests: 未动代码 — 无回归面
- E2E 可行性: memsync import 2行→queued=2 实测 ✓ (sandbox GOPI_HOME)
- Commit: 1146c87 docs(foresee-review): branch-backlog audit design
- 分支: feat/merge-backlog (worktree ~/workspace/gopi-merge-backlog,
  worktree 共享 — 提交即交付, 无需 push)
- 恢复命令: git show feat/merge-backlog:devdoc/foresee-review-branch-backlog.md
