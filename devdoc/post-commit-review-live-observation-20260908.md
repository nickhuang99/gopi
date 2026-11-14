<!-- doc-class: historical -->
# post-commit review 真实任务实证（t-1788812762 / session b92e13b5）

日期 2026-09-08。来源：nick 观察运行中任务，问"tasklist completed 后仍二次 review
epic——是 system prompt 改进导致的吗？"——确认并分析。

## 现象（nick 观察）
任务 t-1788812762（CLI 参数风格 review，positional-vs-optional）的 --chat log 末尾：
5 项 task 全部 completed 后，agent 仍做了一次全局 review（REVIEW-VERDICT: A），
对 EPIC 整体复盘而非直接收尾。另：agent 基于代码路径分析拒绝了 briefing 里
"task list --last 3" 的示例建议（不写代码）——briefing 授权允许。

## 机制源头（确认 = 最近 system prompt 改进）
1. **post-commit review**（8cc1fd1，2026-09-06 merge——nick 9/6 想法 + doubao gap
   分析）：loop.go:3354——每个 write_task_area 提交后（!SkipTaskReview）注入
   injectPostCommitReview()：REVIEW-VERDICT A-E 菜单 + 认错无惩罚声明。
   - A:ON-TRACK（零成本继续）/ B:RE-ALIGN（重读 epic）/ C:AMEND-ITEM /
     D:REDO-ITEM（SUPERSEDES append-only）/ E:PATH-DEAD（负发现记录+换路径）
2. **诚实报告纪律 + briefing 授权拒绝**（Report A 部分）：agent 可基于代码路径
   分析拒绝 feature 式请求，Not Done 如实声明。

## 实证数据（b92e13b5 session）
- REVIEW-VERDICT 在该 session 出现 **6 次**——每项 write_task_area 提交后各一次
  （非仅 tasklist 完成后）——观感"完成后 review"是因为第 5 项提交恰在收尾。
- 全库已有 14 个 session 触发过（昨晚 contract/精读/positional Bond 都在触发）——
  b92e13b5 是第一个被用户观察确认价值的实例。
- 实例内容（第 5 项提交后）：thinking 对照 EPIC 原始目标（CLI 参数全面评审）→
  检查 5 项是否一致 → item5 报告是综合非推翻 → 裁决 A + 执行确认交付。

## 价值确认（为什么这是好事）
1. **防"局部完成但整体漂移"**：5 项 task 全绿 ≠ epic 达成——review 强制 agent
   收尾前从 EPIC 视角复盘全局。这正是 doubao why_claude_code_doing_better 分析
   的"Claude Code 每次子任务后重规划"缺口（差距 1）的落点。
2. **基于证据的拒绝**：task list --last 3 被拒=代码路径分析（log --last 与
   task list --last 共用 memoryFlagSet + per-verb 白名单；差异仅 logFlagMember 收
   last vs taskFlagMember("list") 收 db|status|all；跨动词 flag 拒绝是刻意设计）
   ——非敷衍，recorded as open question。
3. **review 成本极低**：A-verdict 零额外工具调用（本轮直接继续）——只在真漂移时
   才付 B/D/E 成本。105 轮/590s 的任务 review 开销可忽略。

## 对应 system prompt 变化链
- 9/6 87a7b84：post-commit review 基础版（注入[POST-COMMIT REVIEW]）
- 9/6 8cc1fd1：细化版 A-E 菜单 + 无惩罚声明（本实例用的版本）
- Report A（9/7）：诚实报告/任务协议三态等周边纪律

## 结论
nick 观察正确——这是 post-commit review 机制（8cc1fd1）在真实任务中的第一次
被观察确认。机制按设计工作：强制提交后全局复盘 + 低成本 A-verdict + 真漂移才
付费。无需修改。
