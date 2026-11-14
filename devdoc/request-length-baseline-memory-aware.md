<!-- doc-class: historical -->
# Request-Length Baseline — memory-aware 核心验收指标（7168c24d 实证）

日期：2026-09-05
对象：session 7168c24d（gopi log --last N bug 修复任务，nick 手动 run）——463 轮完整生命周期
用途：memory-aware（write_task_area + trim + carry-forward）的 request-length 基线数据 + 验收红线。

## 一、每轮 request-length 曲线（真实 prompt_tokens，计费口径）

来源：session JSONL 的 request 事件（顶层字段 iteration/prompt_tokens/cache_hit/cache_miss/completion_tokens）——非估算。

关键节点：
```
轮  0:   13,170 tokens（EPIC briefing 起点）
轮 32:   51,769   ｜线性爬升 ~1K/轮
轮 39:   14,163  ← TRIM -43,200（item-1 write_task_area 提交）
轮 96:   60,336   ｜重新爬升
轮 168:  87,426
轮 171:  13,601  ← TRIM -74,360（item-2 提交）
轮 296:  86,807   ｜长尾修复爬升
轮 448: 105,789  ← 峰值 106,139
轮 450:  13,988  ← TRIM -92,151（item-3 提交 + 收尾）
轮 462:  18,684（任务完成）
```

总量：463 轮 × 均值 70,976 = 总 prompt 32,861,953 tokens
- cache_hit 32,524,672 = 98.97%
- cache_miss    337,281 = 1.03%（5 次 trim 破坏前缀的全部代价）
- completion      215,178

## 二、核心洞察

1. **锯齿天花板机制**：峰值 106K << 200K 有效窗口。无 trim 对照推算：前 39 轮斜率 ~1K/轮 → 463 轮峰值 ~300K+（撞进 RoPE 衰减/指令稀释区）。trim（write_task_area 提交驱动归零）是"有效窗口守护者"。
2. **代价 ~0**：5 次 trim 总 miss 337K tokens（0.3M，约 1%）——trim 后第 1 轮 miss（轮 55 cache_hit=0），下一轮前缀重建、98.97% 恢复。0.3M miss 换全程安全区。
3. **推演深度靠归零重启**：463 轮长链（多 item 长任务）的每一段都保持在模型最佳工作区间（<150K）。
4. **trim 保留内容**（见逐轮分析）：briefing 锚点 + 提交轮（assistant tool_use + 回执）+ live tail + [WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)] 注入块（结论 1→2→3 条渐进累积，append-only 不覆盖）。
5. **trim 是段删除不是渐进摘要**：item 内 tool_result 全量保留几十轮（18K reader.go 全文等），item 边界整段清算。压缩率 96-98%（msg 数）——item 内渐进摘要 = 未来增量优化方向。

## 三、验收红线（memory-aware A/B 用）

- max request-length 必须 < 150K（设计红线——超过进有效窗口衰减区）
- trim 时机由 write_task_area 提交驱动（不是 token 阈值——提交边界 = 认知边界）
- cache hit 率应保持 >97%（trim 破坏前缀是可接受代价）
- 对照基线 = 本曲线（无 memory-aware 的 A/B 组应呈现无锯齿单调爬升 → 峰值超窗）

## 四、分析方法（可复用）

1. session JSONL request 事件顶层字段（非 message 内）提 prompt_tokens 序列
2. 相邻大落差（>10K）= trim 断崖点
3. raw log（→ REQUEST 块）对比 trim 前后消息集（msg_id 匹配 diff）——看真实保留/删除
4. 保留内容检查：briefing 在 [0]？结论注入块在？live tail 在？

## 五、相关

- devdoc/night-delivery-20260905.md（T1 总交付）
- devdoc/verify-write-task-area-20260904.md（E1-E5 验证）
- t-1788555938（T2：框架硬接管 + resume carry-forward——进行中）
