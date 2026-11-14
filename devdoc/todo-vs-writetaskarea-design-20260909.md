<!-- doc-class: historical -->
# todo vs write_task_area：tasklist 工具族设计研究

日期 2026-09-09。触发：nick 问——task list readonly 指令过时了吗？todo 目的？
只调一次？能否与 write_task_area 合并？名字 awkward？能否整合成单工具/只留
todo？

## 现状（代码事实）
### todo（loop.go:1301 + 4434）
- **零参数只读查看工具**：返回 harness 追踪的 tasklist 状态（todoState.format）
- 无 tasklist 时提示 "emit [TASK_LIST_BEGIN]...[TASK_LIST_END] to establish"
- **parallelSafeTools**（loop.go:3997）：与 read/code_search 同批并发——零成本自查
- **状态更新靠 TASK_LIST 块**：LLM 响应发完整 JSON 数组 → parseTaskList →
  todoState.update（loop.go:2958）——todo 工具本身不写状态
- 用途：**进度自查**（"where do I stand"）——A3 竞争教训：gopi 19 calls 零 todo
  烧 thinking 叙述进度。可多次调用（检查点自查——开工前/每步前/报告前）

### write_task_area（loop.go:1243 + tools/writetaskarea.go:84）
- **每项完成一次**的结论提交工具：task_id + status=completed + conclusion
  （必需）+ compressed_analysis（可选——省略即丢弃分析）
- 提交后：结论写入 session_conclusions 表（append-only）+ **trim 工作区**
  （loop.go:5123——切掉已提交区域保留 epic+commit round+live tail）+ **触发
  REVIEW-VERDICT**（post-commit review——loop.go:5155）
- **不更新 todoState**——tasklist 的 completed 靠 LLM 后续发 TASK_LIST 块
- T2 guard（pendingTaskAreaConclusionGuard loop.go:4067）用结论表做"机器信任
  completed 视图"补状态断裂

## 职责本质（为什么不能简单合并）
| 维度 | todo | write_task_area |
|---|---|---|
| 方向 | **读**（lookup）| **写**（commit）|
| 作用对象 | tasklist 状态（进度）| **结论内容**（知识固化）|
| 机制 | 返回追踪状态 | session_conclusions + trim + REVIEW |
| 节奏 | 随时自查（并行安全）| 每项完成一次（9/9 vs 0/3 强制）|

**读写分离设计**：todo 读状态（零成本自查）；write_task_area 写结论（纪律强制
逐项提交——POC 决定性 9/9 vs 0/3 自由文本）。只留 todo 会退回"自由文本 all
done"失败模式（丢结论固化价值）；合并单一工具要小心 todo 的并行自查价值被
写模式拖累。

## nick 问题逐条答案
1. **readonly 指令过时？**——没过时但表述易误导：实际=查看只读（todo），
   修改靠 TASK_LIST 块。prompt 原文（"todo = read-only view — emit the block
   to change status"）其实准确。
2. **todo 目的？**——进度自查，替代 thinking 叙述（零成本）。
3. **只调一次？**——否。可多次（并行安全），检查点自查。
4. **合并 write_task_area？**——不能简单合并（读 vs 写/状态 vs 内容）。
   但存在状态断裂：wta 不更新 todoState——T2 guard 补了。
5. **名字 awkward？**——是。"write_task_area" 长且"area"模糊；"todo" 误导
   （暗示待办管理非进度自查）。
6. **整合/只留 todo？**——只留 todo 丢结论价值（不可）；整合单工具可行但需
   保 view 模式并行安全。

## 方案（待 nick 拍板）
- A. 改名澄清：todo→task_view / write_task_area→task_commit（职责不变）
- B. 整合单工具 task_update：action=view|commit（view 保并行安全）
- C. 保持现状 + 文档澄清两者关系

## 参考代码
- loop.go:586-610（TASK MANAGEMENT RULES——readonly 表述源）
- loop.go:1301 todo schema / 4434 case / 3997 parallelSafeTools
- loop.go:1243 write_task_area schema / 5123 trim / 5155 review
- loop.go:4067 pendingTaskAreaConclusionGuard（T2 硬接管）
- internal/agent/tasklist.go:75 TaskItem（tasklist JSON 解析）
- internal/tools/writetaskarea.go:84 WriteTaskArea（结论写入）
