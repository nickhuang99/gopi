<!-- doc-class: historical -->
# Memorization Effort 参数设计讨论（2026-09-10 凌晨——想法保存，未实施）

> 状态: 讨论中（nick: "看起来还不成熟，需要再设计"——先保存想法）
> 参与: nick + PA。触发: nick 的"记忆努力参数 1-10"想法。

## nick 的核心想法
一个参数（1-10）控制 gopi 花多少努力做记忆——模仿人类学习行为：

1. **effort 语义**（记忆投入）：
   - 最低（0）= 完全无记忆——模式重复无数次也不 memory_store（no brainer）
   - 最高（10）= 记忆一切——精读模式：逐行读源码 → 记录每个
     feature/function/class（ctags 式）→ 读完从 context 移除（合书）
     → 靠笔记继续（学生读完一章的行为）

2. **重复次数 = 模式强度信号**（effort 的机制载体）：
   - 人类捕捉模式 = "见过 → 第二次认出来"
   - effort 高 = 见 1 次就存（什么都记——精读）
   - effort 低 = 见 N 次才存（弱记忆需更多重复）
   - effort 0 = 无限次也不存（永不 memory_store）

3. **上下文管理 = 学生行为**：
   - 读完整文件 → 总结 + memory_store 记录 → 从 context 移除文件内容
     （context 窗口装不下所有文件——像学生合书靠笔记）

## PA 的评论（切中要害的问题——nick 确认这些也是他思考未果的）
1. **effort 粒度**：连续滑尺 vs 离散档位？→ 建议离散枚举（像 reasoning_effort）：
   0=永不记忆 / 2-3=只 lessons / 5-6=重复≥2 存 / 8-9=精读 / 10=ctags 全记忆
2. **"重复"判定单位**（最大技术问题）：
   - 跨 session "第二次见到"需先 memory_search——每次候选都 search 有成本
   - 语义级 vs 字面级重复？
   - 建议：memory_store **写入时**用已有 dedup（exact+trigram）检测——
     命中 = 已是第 2+ 次 → 提升 confidence 而非重复存（避免读时频繁 search）
3. **effort 是全局还是 per-task**？→ 倾向 per-task 声明（task briefing 带字段）
   - 精读（高 effort）适合代码 review / 新项目探索（建完整心智模型）
   - 日常 bug 修复（低 effort）不需要
4. **"合书"时机与代价**：合书前提 = 笔记质量够（回不去查）——
   已有 conclusion carry-forward 但质量依赖 write_task_area 纪律（9/9 vs 0/3）

## 与已有理论的连接（非新发明——已有件的参数化整合）
| 设计件 | 已有基础设施 |
|---|---|
| effort 参数 | reasoning_effort（同哲学）|
| 重复 N 次触发 | memory.db exact-dedup + trigram 查重（M4 sameWorkIdentity）|
| 精读逐行记录 | SELF-REVIEW 模式（code_search 强制完整读）|
| 读完移除 context | context_trim + memory-aware trim 事件 |
| "第二次认出来" | memory_search 先查 → 命中 = 第二次 |

## 理论根基（EC-RAG 自洽）
- 重复 = 强化 = 逆熵（EC-RAG 第二公理）
- 频率自动筛模式 = 归纳涌现（博弈稳态）——**不是 LLM 判断价值，
  是客观重复计数**——解决 memory_store 老大难"什么时候值得存"
  （LLM 主观判断不可靠——brew 质量不稳根因之一）

## 关键风险
- effort 高 + 精读的 token 成本（逐行读全文件+记录+移除——比正常贵得多）
- 应为显式选择（用户/task 声明）非默认

## 下一步（未定——待再设计）
- effort 档位定义细化
- 重复判定的语义（单位/跨 session/成本）
- 与现有管道（memory-aware/write_task_area/brew）的接口
- 保存位置: 此 devdoc + PA memory（讨论待续）
