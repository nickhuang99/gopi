<!-- doc-class: historical -->
# Merger 单点所有权架构决策 — 冲突分支由 merger 自己解（2026-09-05 nick 定稿）

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** 本文描述的常驻 merger
> 已退役: `f9ff1a89` 关闭全部入口, `55c4d6f3` 删除实现（loop/tick/take-over/控制面/
> status 面）。今天任何拼写的 `gopi merger` 都打印 `mergerRetiredCause` 并 exit 1；
> 一次性合并是裸 `gopi merge` (`cmd/agent/merge_run.go`)，merger 的 WORK LOCK
> (`cmd/agent/merger_work_lock.go`) 保留。记录见 `devdoc/merger-retired.md`，分类表
> 与同一日期更正在 `devdoc/merger-self-driving.md`。本文的架构推理仍有效——它正是
> 保留下来的一次性 face 所继承的设计。


## 决策

merger 是处理**所有 merge 业务**的唯一进程。检测到冲突分支 → merger 自带 LLM 会话直接 resolve → merge → branch -d。**绝不 submit merge-conflict task**。

## Rationale（nick 原话 + 论证展开）

> "if the branch cannot be merged, then current design has no way to know when is ready to merge it again, retry? if submitted to a new task, then it is also responsible for merging and resolving and delete the same branch, there is potential race condition for dealing with this branch. so, merger is only process that handles all merging business."

两个致命缺陷（submit 模式）：
1. **无重试时机**：分支冲突 → submit task → 如果 task 失败/未解决，分支永远卡在冲突态——没有机制知道"何时可以再 merge"。谁负责重试？无人。
2. **多重所有权 = race**：submit 出去的 task 也要 merge+resolve+delete 同一分支——分支是共享对象——多个 session 同时解同一分支/一个在解一个在删/重复 merge = 经典 race（正是 task 系统防双跑要消灭的，submit 模式把它引回来了）。

结论：**merger 单点所有权**——发现者即所有者，resolve→merge→branch -d 到底，无交接无竞争。

## 对旧设计的推翻

9/3 定案的 merger = "纯 token-free merge 决策件"：
- "the semantic judgment belongs to the executor agent's session, **never this process**"（语义判断永远外包）
- merger 从不 spawn child（kill 零连坐）

本决策推翻这两条：merger 本身是带 LLM 能力的执行者（冲突时自己起解冲突会话）。merge 的"token-free 自动收编 clean 分支"保留。

## 现状与工单

- 2026-09-05 已 kill merger 守护进程 1330936（2026-09-03 21:13 启动，nick 不知情存活 2 天——它一直在自动 merge clean 分支 + 自动派发 merge-conflict task）
- 改造工单 t-1788565001（proc_c85071d942d5）：冲突分支由 merger 自带 LLM 会话直接 resolve——不 submit。验收：E1 真冲突分支被 merger 自解+零新队列行 / E2 session 审计 / E3 clean 照常 / E4 kill 中断幂等 / E5 回归绿
- 相关：t-1788554650（旧 submit 模式的 merge-conflict task——已 completed，11m53s Bond 解冲突 feat/fix-log-last-degenerate）

## 关联原则（同源）

- db-sync 双跑防护（锁模型/ALOHA——同一行的多执行者=race）
- "single-writer" 原则：共享对象（分支/行）必须单点所有权
- nick 一贯刀法：submit 绕圈=交接缝=bug 面（"split one task into three = trouble and bug"）
