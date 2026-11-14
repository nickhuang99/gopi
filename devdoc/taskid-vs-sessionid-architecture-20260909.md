<!-- doc-class: historical -->
# task_id vs session_id：双 key 架构分析（重构清理评估）

日期 2026-09-09。触发：nick 在 session 目录重构契机下重问根本问题——
"为什么需要 task-id 当已有 session-id？能否去掉 t-timestamp 用 uuid？"

## 代码事实（2026-09-09 实测）
1. **task_id = t-<unix 秒>**（tasks.go taskIDPrefix，-N 后缀撞车循环）；
   session_id = UUID v4（SessionLogger 生成）。
2. **9/5 single-row 重构已解耦二者**：task_id 和 session_id 不同实体——
   session UUID 放 session_id 列（taskregister.go:156 注释明示），task_id
   统一 t-*。
3. **task 先于 session**：TaskSubmit 创建 pending 行无 session（工单等领取）。
4. **session 先于 task**：裸 prompt 的 maybeRegisterSessionTask（session 在跑
   才登记 auto 行）。
5. **实测 task/session 1:1**：0 个 task 多 session（resume 产生新 auto 行非
   同 task_id 续跑）；3 个 session 2 行（single-row 前双行残留）。
6. **跨节点撞车是真 bug**：task-id-collision.md（预见②）——同秒跨节点
   mint 同 id → import 静默丢 bug 单。已修（import 撞 id 比对 payload hash
   改号重排）但**治标**（生成端仍可能撞）。

## 核心结论
**task_id 不能去掉（task=工单 ≠ session=运行）**：
- 工单先于运行存在（pending 无 session）——session-id 无法当 pending 的 key
- 工单可排队/跨节点 sync/被 worker 领取——独立于执行的实体
- 合一 = 消灭工单队列概念（更大的架构革命，非本次清理）

**但 t-<unix> 不是好 key**（nick 质疑成立）：
- 唯一性差（跨节点同秒撞）
- 9/1 否决 uuid 的理由（END hook 省事）已被 9/5 解耦清除

## ⚠️ 2026-09-09 nick 最终裁决（推翻上文"D 不可行"）
**"task submit 返回的 task id 可以简单地就是 session id（uuid）——没有困难。
你甚至可以创建 ~/.gopi/sessions/<uuid>-dir 给 task，唯一目的是为 task 分配
运行目录（日志/附件）。没有 t-timestamp 的必要。"**

我的"D 不可行（pending 无 session）"结论**错误**——session 不需要等运行才
生成：**submit 时就分配 uuid（session 容器 id）+ 建 sessions/<uuid>/ 目录**。
task 的 pending 状态、附件、日志全部进这个目录。task_id 就是这个 uuid。

**最终决策（对应下方方案表 = D 的可行性版，SPEC-db-cleanup-uuid-taskid.md）**：
- task submit → 生成 uuid → 建 `~/.gopi/sessions/<uuid>/`（提交即建，
  metadata.json + attachments/）→ 附件进目录 → 返回 uuid 作 task_id
- **task_id = session id（uuid）统一——消灭 t-<unix>**（跨节点永不冲突）
- 工单队列语义保留（pending/claimed/completed 状态机不变）——key 变 uuid
- session 提前分配作 task 容器——**pending 也有容器**（我此前"D 不可行"
  的错误根源：把 session 锁死为"运行痕迹"，没想到可提前分配作容器）
- resume = 同一 task 目录继续（新 run-uuid 文件但同一 task 行）——消灭
  当前"resume=新 auto 行"的别扭

清理方向（重构契机评估）
| 方案 | 做法 | 优点 | 代价 |
|---|---|---|---|
| A. 保持 t-<unix> | 现状 | 零成本 | 跨节点撞车（已治标）|
| B. task_id 改 uuid/uuidv7 | submit/auto 生成 uuid（v7 时间可排序）| 跨节点永不撞 + 语义干净 | 73 文件引用 + sync keying + 存量迁移 |
| C. 节点前缀+时间 | t-hp-<sec> | 跨节点不相交 | 改形态 + CLI 习惯 |
| D. 去 task_id 用 session | 消灭工单队列 | 最简 | **不可行**（pending 无 session）|

## 历史脉络（为什么走到这）
- 9/1 task-id-style-analysis.md：三方案，**推荐 Option 2（统一 t-<unix>）**——
  review 背书（3 reasons）。当时否决 Option 3（uuid）：破坏 #<n> 显示、
  失时间排序、难人工引用、存量 backfill 危险。
- 9/5 single-row：task_id/session_id 解耦（技术前提变化——当时否决
  Option 3 的理由部分消失）。
- 现在（9/9）：nick 重问——重构清理契机。

## 待 nick 拍板
- B（uuid 替代）vs C（节点前缀）vs A（保持）——或先讨论 task 工单语义
  是否真要保留（D 的边界）。
- 若 B/C：存量 1586 行迁移策略（review 警告 rename 使 sync replica 分叉——
  但若单向 backup 语义 + 全量重灌可能可行）。
