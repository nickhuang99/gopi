<!-- doc-class: historical -->
# 观察项：GLM 忘记 commit（假完成）——DS 对照组未复现

日期 2026-09-07。状态：**观察中，不行动**（nick 决策——防过度工程）。

## 事件
HP/GLM 任务 t-1788767252（fix gopi command param positional bug）标
completed 但修复从未 commit——躺在 HP 工作树未提交（M 状态）。
write_task_area 声称 "post-commit integrity re-verified" 是幻觉（实际
没 commit）。task run writeback 标 completed = 进程退出即成功，不验证
deliverable → 假完成。

## 决定性对照（为什么判定为模型问题而非系统缺口）
同一 bug（positional param）同一 briefing 类型，两个模型独立修：

| 模型 | 任务 | 结果 |
|---|---|---|
| **GLM-5.3-flash**（HP）| t-1788767252 | ❌ 没 commit——修复滞留工作树（M 状态），假完成 |
| **DeepSeek v4-flash**（peerbox）| t-1788788609 | ✅ commit 了——b9ea37a "fix(settings): strip --settings/--home from dispatch argv"，工作树干净 |

DS 侧历史记录一贯良好：9/2 切 DS 后 bug053/054/055 一次交付零返工
（commit+devdoc+测试随码）；8/28 双模型画像=DS 纪律型 vs GLM 效率型。

## 决策（nick 2026-09-07）
- **不派 writeback git 校验任务**（原设计的"task run completed 前验证
  是否有 commit"）——若这是 GLM 特有行为，为它加 harness 校验 =
  过度工程（追风车）。我们无法修复所有模型问题。
- **不归因 system prompt**（GOPI.md Rule 5 commit 纪律 prompt 层已存在
  ——prompt 软约束对 GLM 不可靠是模型服从性问题，非纪律缺失）。

## 监控条件（触发行动的阈值）
**若未来观察到 DS 模型也忘记 commit（同形态假完成），才行动**——
届时派 writeback git 校验任务（claimed 时记录 HEAD → 结束时查
rev-parse + status --porcelain → dirty+0 commit 标 warning）。

## 相关资产
- HP 修复保全：fix/hp-positional-param-submit 分支（54c3ba2，
  isTasksSubcommand 引号撕裂根因修复——**此修复本身有效值得 merge**，
  与 DS 的 b9ea37a 互补：一个修路由层一个修 settings 层）
- DS 修复：b9ea37a（已在 master？待确认）
- CLI contract testsuite（t-1788790264）——**仍值得做**（防 subcommand
  refactor 静默改路由——那是 harness 结构问题，与模型无关，nick 已批准）
