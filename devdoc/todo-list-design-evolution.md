<!-- doc-class: historical -->
# GoPi Todo List — 设计演进与最终收敛

> 来源: ~/work/claude/nick/gopi_big_view_design_talk.txt
> papers.db: id=160
> 关键洞察: 从复杂 CRUD 到极简无状态——设计本身的设计过程

## 演进三阶段

### 阶段 1: Todo.Review — CRUD 满配

最初设想给 GoPi 增加完整的 Todo CRUD 工具集: Todo.Add, Todo.Delete, Todo.Update, Todo.Decide, Todo.Review。Scheduler 维护任务栈，LLM 通过工具调用操作任务状态。`current_active_task` 字段强制 LLM 声明当前执行目标。

问题：状态散落在两处（LLM + Scheduler），不同步是常态。CRUD 工具增加系统复杂度，与 GoPi 的 4 工具极简哲学冲突。

### 阶段 2: 状态合并 + 去工具化

"每次 response 结束时就把当前任务状态表列出来。LLM 有权利修改、添加，但没有权利删除。Scheduler 仅仅做一个兜底。"

关键转变:
- 放弃所有 CRUD 工具→ 任务清单变成 LLM 响应中的隐式数据块
- Scheduler 变成纯读取角色→ 不维护、不修改、不排序
- 状态一源: LLM 每轮返回的全量表

### 阶段 3: 极简 2 状态

"状态只有两种: progressing 和 completed。父任务拆解后设为 completed。多任务同时在 progressing 是允许的。"

最终收敛:
- 2 状态 (progressing / completed)
- 任意数量 progressing 任务 (LLM 自主选序)
- 禁止删除→ 通过状态办结
- 心跳仅兜底→ 连续多轮无变更才提醒

## 设计内在逻辑

nick 的演进不是随机迭代——每一步都在解决问题:
1. CRUD→ 发现问题: 代码和状态分散在两个系统
2. 合并→ 寻找方案: "Stateless Server" 一源
3. 极简→ 达成收敛: 2 状态, 0 新增工具

核心洞察: 无状态设计的本质是把 **维护成本** 从 Scheduler 推到 LLM——让维护的一方成为状态拥有方。多花几百 token 换取零同步复杂度。
