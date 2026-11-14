<!-- doc-class: historical -->
# Reframe 三档机制 — 真实任务 E2E 验证报告（2026-09-10）

## 背景
nick 设计（2026-09-10）：把 reframe（epic 理解）与 evaluate（可行性判断）
合并为**一次结构化 toolcall** `task(action=reframe)`，三档 verdict（零 loophole）：
- DOABLE → 做
- TOO_BIG → "too big to do by myself"：必须给 roadmap（研究可以是第一项），
  harness 自动 submit 子任务，当前任务光荣退出
- IMPOSSIBLE → 原则上不可达（严格判据：provably unattainable in principle）
- 删除 NEEDS_RESEARCH（research 是 reframe 本職；伪出口）

SPEC: briefings/SPEC-reframe-evaluate-merge.md
POC 数据: devdoc/poc-reframe-verdict/（4 案例 × 2 轮 naked-prompt）

## 实现（Bond 01a088ef → merge master 3ce88c2）
- 18 files, +1947/-595
- `internal/taskverdict` 叶子包（Doable/TooBig/Impossible + All/Valid）
- `task(action=reframe)` schema + **harness 硬校验**（validateReframe：
  TOO_BIG 无 roadmap 拒 / IMPOSSIBLE 无 reason 拒 / 空 verdict 拒 / NEEDS_RESEARCH 拒
  ——零副作用）
- dispatch：DOABLE→继续；TOO_BIG→roadmap 自动 submit 为 child tasks
  （parent-linked, source=auto, tag=reframe-roadmap, payload=goal+acceptance）
  + 当前 run 光荣退出；IMPOSSIBLE→ledger failed + reason + reframeHonestExit latch
- free-text [REF RAMED EPIC] 流程退役（改为 toolcall）
- telemetry: task_events 行 + `gopi task verdicts` CLI + task show children 块
- **task_evaluate 退役**（16 工具）；system prompt 4478 chars（< 4500 密度门）

## 真实任务 E2E（nick 要求 "test with some real development task"）

| 任务 | verdict | 结果 |
|---|---|---|
| E2E-1 merge-backlog dedup（legacy-audit 发现的真实 bug）| DOABLE | completed → **merge 1accf36** |
| 01a0891d worktree ownership（真实 bug 单）| DOABLE | completed → **merge b1a3ee6** |
| E2E-2 hippocampus（真实大项目）| **TOO_BIG + 4-item roadmap** | **光荣退出**（completed）|
| └ [1/4] Author the missing spec | — | **completed** |
| └ [2/4] Hippocampus core loop | — | **completed → merge d62b3ec** |
| └ [3/4] Invocation path + interrupt channels | — | in_progress |
| └ [4/4] Real-session adoption proof | — | in_progress |
| 01a0890c finish-cleanup bug | DOABLE | 执行中→完成 |

### 关键实证点
1. **task(action=reframe) 被 LLM 自然接受**——10 个真实任务触发
   （telemetry: 56 records across 10 tasks）
2. **DOABLE 路径完整闭环**：reframe → 执行 → 修复 → 测试 → merger 自动 merge
3. **TOO_BIG 路径完整闭环**：reframe → roadmap → **自动入队 4 children**
   （payload 自足 1088 chars）→ 主任务光荣退出 → **children 自动被 worker
   领取执行** → 完成的 child 被 merger 自动 merge
4. **零 loophole 实证**：hippocampus 的 [1/4] 是"补写缺失的 spec"——
   LLM 自己发现设计文档不完整 → 把"研究"作为 **roadmap 第一项**
   （而非用"需要研究"当逃逸出口）——正是设计意图
5. **E2E-1 的 reframe 质量**（真实数据）：epic_summary 准确引用 audit 数字
   （751 删除/51 重复/8×20 模式）；deliverables 具体（file:line 根因+幂等修复+
   回归测试）；acceptance 可机器检查
6. **无 NEEDS_RESEARCH**（schema 层不存在）

### 未触发档
IMPOSSIBLE 在真实任务中未触发（合理——真实工程任务里"原则上不可达"罕见；
POC 中已验证该档能正确产生高质量理由）。

## 完整自动化链（零人工干预）
```
真实任务 → task(action=reframe) 调查
  ├─ DOABLE   → 执行 → commit → review(A-E) → merger merge
  └─ TOO_BIG  → roadmap → harness 自动入队 children
                 → worker 自动领取 → 各自执行 → merger merge
```

## 结论
三档设计在真实任务中完全按预期工作。核心价值达成：
- **evaluate 不再在调查前瞎猜**（reframe 完成时判断 = 唯一可信时机）
- **"太大"不再零成本**（必须给 roadmap）
- **研究不再是逃逸出口**（是 roadmap 项）
- **诚实出口保留**（IMPOSSIBLE 可诚实承认）
- **bureaucracy 循环消除**（每条出口携带新增信息：roadmap/理由）

## 附录
- 测试: `go test ./internal/hippocampus -count=1` 21/21 PASS；
  reframe_gate_test.go（硬校验零副作用）；reframe_test.go 714 行改动
- 相关: legacy-t purge（devdoc/legacy-t-purge-20260910.md——558 sync-yielded
  + 51 merge-backlog 重复 = bureaucracy 噪音的真实案例，与本研究主题呼应）
