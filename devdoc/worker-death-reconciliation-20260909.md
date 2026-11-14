<!-- doc-class: historical -->
# Worker 死亡事故记录（2445485）——Bond 工作完成但关账断链

日期 2026-09-09。影响：硬编码单 5960-1（archive 命名空间单源化）。

## 事故链
1. worker 2445485 先跑完 DB 清理 Phase 1（t-1788905574，59m45s——completed）
2. 领 5959（merge-backlog 标签）→ completed（adoption merge e2c5f40）
3. 领 5960-1（archive 标签跨包镜像——大单，3 文件）→ **Bond 完成工作**
   （分支 feat/archive-namespace-single-source d6acfff + docs 9c17476）
   → **worker 2445485 死亡（进程消失）**——FinishSessionTask 没跑成
   → 5960-1 卡 in_progress（claimed_by peerbox:2445485 死 PID）91min 无写入
4. PA 巡查发现（91min 停滞——非活跃）→ 诊断死 PID → 人工收编

## 处置
- 分支内容完整（worktree 干净 + build OK + 2 commits）——工作没丢
- PA merge（10 files 170+ 行）+ 关账 completed（reconciliation reason）
- 起新 worker（proc_6e4b6bbf9548）继续消化队列

## 教训
1. **worker 死亡后 Bond 的工作通常已完成**（分支/worktree 是证据）——
   先查分支内容再决定（不要 cancel 重跑——浪费已完成工作）
2. **in_progress 卡 90min+ 无写入 = 死 worker 信号**（stale 检测应覆盖——
   但 sweep 只在 serve/task run 入口触发——无 serve 时靠 PA 巡查）
3. **worker 连续跑多张大单会累积死亡风险**——长任务后 worker 可能
   资源耗尽/被系统杀——多 worker 并行时注意
4. **PA 收编模式**：分支 merge + 关账（completed + reason 说明）——
   与 merger adoption 互补（adoption 覆盖正常完成，PA 覆盖死 worker 残留）

## 关联
- worker 2445485 之前跑：DB 清理（completed）+ 5959（completed）+
  5960-1（完成未关账——本次）
- 硬编码修复进度：5959 ✅ merge / 5960 ✅ merge / 5960-1 ✅ merge（PA）/
  5960-2 跑中（新 worker 2510395）
