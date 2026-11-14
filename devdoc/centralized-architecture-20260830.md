<!-- doc-class: historical -->
# gopi 中心化调度架构定稿（2026-08-30 深夜，nick 四砍后终稿）

## 架构（一句话）
peer1 = 唯一大脑（队列/台账/serve/源码仓）；HP/RPi5/未来节点 = 无状态计算位（gopi 二进制 + settings + ssh 免密，零队列零存储零 serve）。

## 演进史（四砍实录——每刀砍在 PA 加的层上）
1. estimate 校准系数 → 砍，存原始时间戳（tasklist_issued_at）
2. HTTP ingest → 砍，ssh CLI task update 就是 RPC
3. node:X tag 路由 → 砍，tags 回归索引本职，负载均衡计数替代
4. nodes.yaml 独立配置 → 改为 settings.json compute_nodes 白名单段
+ 教训固化：设计评审必答题"这个层删掉会怎样？一条 ssh 能替代就不该存在"

## 五件终稿
① settings.json compute_nodes 白名单 {name, max_concurrent, report_to, repo}（三源配置：CLI > env > settings.json）
② 负载均衡 dispatch（serve tick：ssh 查各节点 'task list --status in_progress' 计数 → count<max_concurrent 最空者）
③ ssh 远端执行（gopi task run <id>，remote 零配置）
④ remote ssh 回 peer1 CLI task update（终态回账）
⑤ git push back feat 分支（代码交付）
+ binary scp 分发（未来：节点无需源码仓，交叉编译+版本协商）
+ stale sweep 挂 serve tick 无条件位 + resume 路径（触发点孤岛修复）

## 信息闭环（closed circle）
deliverable = {repo, worktree(PA 预建已知), branch(结束回填), devdoc} —— submit 时 PA 从 Projects 表查 repo；writeback 自动补 branch；verify/merge agent 读区块零记忆干活。

## 自治闭环（五环）
自投 bug（task_submit tags=bug，4 条实证五要素齐）→ serve 领取修复 → task verify 黑盒关闭（P2.5）→ brew 蒸馏 lesson（M3）→ 检索面消费（M1）。
merge 决定权 step by step：B（verify 自主+merge 保留 PA）→ 数据（verified 误判率）→ A（gopi 自 merge+PA 抽查）。

## 已 merge（本轮架构讨论的实现部分）
M4.5 台账治理七件套（master 8e1342a）：new 删除/update 收窄/next 收缩+claimed 瞬时化/serve flock/--skip-tag/墓碑归档/ingest/存活追踪列。

## 在途/待实现
- M4.6+M4.7（feat/chat-remote-dispatch，resume 续命中）：chat 表（hermes 40 列对比）+ remote dispatch（三源+负载均衡+report_to/repo 回程）
- 增补单：stale sweep 位置（t-1788052204）/model+session stamp（t-1788050446，devdoc 注释描述不存在的功能）/resume --task 关联+主 help（t-1788050237）/gopi shot（t-1788043085）
- verify-loop（feat/task-verify-loop）验收待 M4.6 merge 后

## 实证教训
- GLM 端点间歇故障（DNS/SSE）→ 大任务死中途 → resume 续命（byte-exact checkpoint 实测 78 messages 恢复）优于重派；设计成果从 session JSONL thinking 抢救
- 僵尸判定三层证据链（零先验）：heartbeat 停（弱）→ claimed_by pid 死活（强）→ session mtime 停（佐证）
- 注释是实现的影子也要查（M4.5 stamp 注释描述了不存在的功能）

## RPI5 独立审查（2026-08-30，无阻塞结论 + 5 条补强建议）
1. stale 判定→动作决策表缺失（判定后 release/resume/重派选择 + ssh 不可达时保守计数）——随 M4.7 端到端实测补
2. dispatch 窗口竞态=最终一致设计（下个 tick 纠正）或 fencing（generation/租约戳）——二选一写明
3. 回程 RPC 失败路径与 stale sweep 互相引用（update 失败→三层证据链回收）
4. 新节点 onboarding 5 步 checklist（key 分发→settings→ssh 验证→冒烟）+ env 变量名确切化
5. resume checkpoint 与 settings 漂移的前置依赖标注 + "无状态"术语精确化（无队列无存储，接受只读探查）
