<!-- doc-class: normative -->
# Reframe E2E 测试发现：directive 注入 ✅ / LLM 触发不可靠 ⚠️

日期 2026-09-08。测试任务 t-1788847341（cancelled——合成素材失败）。

## 测试设计
13K chars 假 briefing（80 模块"审查"）+ 附件 → 验证 reframe 触发 + context
reduction。

## 发现 1（机制 ✅）：directive 完整注入
首条 user 消息正确合成：EPIC GOAL + summary + 13K briefing + [attachment1:
<副本路径>] + [EPIC REFRAME] 指令（含 "must ALSO open with [REF RAMED EPIC]"
措辞）——注入链路工作。

## 发现 2（素材 ❌）：合成任务被 Bond 识破
Bond 8 轮内 grep 出 "REF RAMED EPIC/maybeApplyReframe" 代码 + 附件假痕迹 →
逆向研究测试构造而非执行任务。**教训：reframe 验证必须用真实大任务素材**
（长代码库审查/真实迁移），合成测试会被 LLM 识破。

## 发现 3（机制 ⚠️ 真 gap）：LLM 触发 reframe 不可靠
directive 绑定"建立 tasklist 的响应"——但 LLM 深探索（大任务逐模块调查）时
可能**迟迟不建 tasklist 也不 reframe**（8 轮无两者）。期间 raw 13K briefing
持续逐轮注入（prompt 20K→35K 增长）——context reduction 未达成直到 LLM
最终建 tasklist。

## 硬化方向（待 nick 决策）
1. **首响应强制**：reframe 不绑定 tasklist——大 briefing（>reframeMinRawLength）
   的**首个 assistant 响应**就必须含 [REF RAMED EPIC]（探索前 reframe——LLM
   先理解目标再动手），tasklist 可随后。
2. **harness nudge**：大 briefing + N 轮（如 3）无 reframe marker → 注入提醒
   （仿 pendingTaskAreaConclusionGuard 模式——memory-aware 已验证此模式）。
3. 或接受现状（LLM 最终会 tasklist 时 reframe——只是延迟）+ 文档化边界。

## 状态
- P1/P2 已 merge master（13ce217/84bf472——机制代码就位）
- 本发现记录待 nick 决定硬化方向后派工
