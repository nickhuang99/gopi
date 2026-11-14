<!-- doc-class: historical -->
# Task Report: 把 hippocampus「备忘每轮重复」的排障教训蒸馏进记忆

Epic: 把这次排除故障的教训写进记忆：gopi 会话里 hippocampus 备忘每轮重复，
靠收敛门（相同记忆状态不再投递）修复。然后结束。

本任务**只写记忆**，不改代码、不新增工具（Rule 3）。

## Completed

1. **提炼出可复用的蒸馏教训**（不是流水账），四个要素齐全：
   - **症状**：同一份 hippocampus 简报在一次 run 内连续投递 8 轮，吃掉上下文；
   - **根因（5-why 到底）**：投递规则是"有简报就投"，**从不比较记忆状态**——
     调度器没有"本 run 已投递过的状态键集合"这一概念，于是每轮都认为有新东西；
   - **修复（两道宿主侧门，不靠调参）**：
     1. **收敛门** `converged(seenKeys, brief)`：brief 的全部状态键都已在本 run 的
        `seenKeys` 里 ⇒ 记忆状态未变 ⇒ **只归档、不投递**（Lv-0）。两个边界：
        首报 `sawBrief` latch 必投（空集合不能"已收敛"）；**无 key 不收敛**
        （键集为空时不可判定"相同状态"，宁可多投不可漏投）。
     2. **最小间隔门** `mayInject`：`hippocampusMinInjectIntervalRounds` 内不重复投，
        `round<=0`（首轮）静默跳过。
   - **实现位置**：`internal/agent/hippocampus.go`（`converged` / `noteState` /
     `mayInject`）＋ `internal/agent/hippocampus_agent.go`（`hippoBriefKeys` 把
     memory state 抽成键集）。
   - **验证命令与坑**：
     `go test ./internal/agent/ -run TestHippocampus` — 测试环境若残留
     `GOPI_HIPPOCAMPUS=1`，会把"默认关闭"的断言打成假失败，需先清：
     `GOPI_HIPPOCAMPUS= go test ./...`（清环境变量再跑）。

2. **写入记忆并纠正一条过粗的旧记录**：
   - 检索命中 4 条同主题记录：#1611 / #1612（已含根因/两道门/实现位置/验证命令与坑）
     与 #1613 / #1614（薄重复，仅有结论、无细节）。
   - `memory(action=feedback, id=1611, rating=useful)` → 随后
     `memory(action=update, id=1611)` 重写 answer（补根因、两道门、位置、坑）+
     12 条中英混合关键词面包屑（收敛门/convergence gate、每轮重复/duplicate
     injection、记忆状态/memory state keys、hippocampus 备忘/brief 等 5W1H 维度）。
   - `memory(action=update, id=1613, tier=cold)`：薄重复记录**降级**而非删除
     （知识库保留版本供审计；降级即可停止污染检索第一名）。

## Not Done

- 未改任何生产代码（本 epic 明确只写记忆）。收敛门的实现属于 `feat/hippocampus-core`
  / `feat/hippocampus-host` 的既有工作，本任务只记录其教训。
- #1614 未单独处理（与 #1613 同类薄记录，已由 #1612 覆盖；留待其来源会话或后续
  检索反馈自然降级，避免同一会话重复改同义条目）。
- 未 merge / 未删 worktree（Rule 6e：merge 由 PA 验收后处理）。

## Verification

- 检索验证（deliverable 2 要求的命中证据）：`memory(action=search, query="gopi 会话
  hippocampus 备忘每轮重复投递 收敛门 相同记忆状态不再投递")` → 命中 #1611/#1612
  （含实现位置 + 验证命令 + 坑）与 #1613/#1614；#1611 已重写为完整版。
- 写入门禁：`memory(action=update)` 返回各自 id（1613 成功降级），
  `task(action=commit)` 记录 conclusion（record #480）。
- Build: 未构建 —— 本任务零代码变更（无代码可构建，如实声明）。
- Commit: 本报告单文件提交于分支 `feat/hippocampus-adoption`（worktree
  `~/workspace/gopi-hippo-adopt`），只 add 该文件（Rule 8，不 `git add -A`）。

## 交付声明（Rule 6d）

- **分支**：`feat/hippocampus-adoption`（worktree 共享主仓库 refs，提交即达主仓库该分支）。
- **恢复命令**：`git show feat/hippocampus-adoption:devdoc/hippocampus-repeat-memory.md`
  ；记忆记录：`memory(action=search, query="hippocampus 备忘 每轮重复 收敛门")`。
- worktree 未删除（未 merge，清理属 PA 验收后动作）。
