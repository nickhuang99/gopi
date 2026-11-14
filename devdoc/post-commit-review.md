<!-- doc-class: historical -->
# Task Report: post-commit-review (write_task_area 提交时的强制全局 review)

## Background
doubao 差距分析（`why_claude_code_doing_better.txt`）核心诊断：gopi 与 Claude Code
的差距在 Agent 外层运行时——Claude Code 每完成一个子任务就重评估全局目标并随时重
规划/回退；gopi 一次拆解就极速往下打补丁（幻觉累积）。单纯加"请仔细检查"prompt
无效——反思必须绑定动作回退机制。nick 的想法：在 write_task_area 提交点注入强制
全局 review，给 LLM 机会调整 roadmap 或回滚。

## Implemented
- **注入点**：`internal/agent/loop.go` 的 `taskAreaDirty` 块（write_task_area 提交
  当轮 hook，约 3106 行）——在 `injectTaskAreaConclusions()` + `trimCommittedTaskArea()`
  之后调用新函数 `injectPostCommitReview()`。三者在提交当轮、trim 之后依次执行，
  review 消息落在 live tail，永不 trim 掉。
- **新常量**：`postCommitReviewMarker = "[POST-COMMIT REVIEW — forced global
  checkpoint]"`、`reviewFindingResponsePrefix = "REVIEW-FINDING:"`。
- **新函数 `injectPostCommitReview()`**（~4960 行，trimCommittedTaskArea 之后）：
  追加 ONE 条 user 消息（~180 token），含 3 点判断：
  1) 方向是否偏离原始目标；2) 是否撞上未预料约束/死胡同；3) 新结论是否推翻旧结论。
  协议：无问题回复 "ON TRACK"；有问题以 "REVIEW-FINDING:" 开头 + 具体偏移 + 动作
  （调整 roadmap / 同 task_id 新 append-only conclusion 作废重做 / 新任务）。
- **开关**：`Agent.SkipTaskReview` 字段，NewAgent 中 `boolEnvTrue("GOPI_SKIP_TASK_REVIEW")`
  单点读取（Rule 2）。默认 off = 每次提交都 review；=1 极速模式零 review。
- **无新工具、无额外循环轮**：review 是上下文注入的 one-shot user 消息，模型在下一
  个普通轮回答，不改 loop 轮数结构。
- **append-only 不变**：session_conclusions 表只追加。作废重做 = 同 task_id 新提交
  一条注明 "SUPERSEDES"，旧结论永不改写。

## Not Done
- 未把 review 做成独立工具（约束：不新增核心工具，工具面保持窄）。
- 未对 review 消息做 token 计量上报（消息 ~180 token，成本可忽略；如需精确数字可
  后续接入 cost tracker）。
- 未做真实 LLM E2E（需 live API key + 人为制造推翻场景）；单测 + stub e2e 已覆盖
  行为（R1-R4）。

## Verification
- Build: `go build ./...` OK
- 新测试 `internal/agent/taskarea_review_test.go`：4/4 PASS
  - R1 unit：注入恰好 1 条 user 消息 + marker + 协议 + session raw log 真实事件
  - R2 e2e：真实提交轮后，模型下一请求携带 review（且首请求无 review）
  - R3 e2e 负例：SkipTaskReview=true 全程零 review
  - R4 e2e 作废流：REVIEW-FINDING 后同 task_id 新 append-only 结论（SUPERSEDES），
    2 条结论都在，run 正常结束
- 回归：既有 T2 测试（E1/E2/E3/E6 + Guard 系列 + writetaskarea/trim/inject 系列）全绿
  ——提交轮 fires review 但不增加轮数，stub 轮次结构不变。
- 全仓 `go test ./...`：13/13 包全绿（job-1，exit 0；internal/tools 308s、internal/agent 最后复跑 29.8s 确认最终代码）
- Commit: e8f2532 `feat(agent): post-commit forced global review on write_task_area (nick/doubao gap)`（feat/post-commit-review 分支，worktree 共享主仓 refs，已在主仓）

## Refinement (v2, feat/post-commit-review-verdict-menu): 显式动作菜单 A-E + 认错无惩罚

### Background
基础版协议（"ON TRACK" / 自由文本 "REVIEW-FINDING:"）被 nick + PA 评审指出缺口：
review 发现方向不对时 LLM 的"出口"是什么？回滚当前任务 = 重复刚完成的工作（伪命题）。
doubao 诊断：极简 agent 不主动废弃已完成的子任务，补丁越打越差。核心洞察：回滚对象
不是"劳动成果"（append-only 永不丢，废弃零损失）而是"方向承诺"——给 LLM 显式低成本的
出口 = 允许宣告"此路不通/方向偏了"而不丢脸、不丢工作。negative finding 是数据。

### Implemented (叠加在基础版上)
- **常量替换**：`reviewFindingResponsePrefix` → `reviewVerdictResponsePrefix =
  "REVIEW-VERDICT:"`，新增 `postCommitReviewVerdictMenu`（单行 A-E 动作码）与
  `postCommitReviewNoPunishment`（认错无惩罚声明）。注入消息尾部 = 菜单 + 声明。
- **协议**：模型回复必须选一个动作码并执行（信任模型执行，框架不解析 verdict——
  只保证注入 + 文档告诉模型动作形态）。菜单：
  A:ON-TRACK / B:RE-ALIGN（重读 epic 更新剩余 roadmap）/ C:AMEND-ITEM <id>（追加
  CORRECTION 结论）/ D:REDO-ITEM <id>（同 task_id 新结论标 SUPERSEDES 重做）/
  E:PATH-DEAD <reason>（路径级死胡同：负发现记录为结论 + 规划替代路径 + 从当前点继续）。
- **E ≠ 回滚**：历史保留为试错证据，从当前点继续，绝不从零重来。D 是单 item 局部错；
  E 是路径级全局死胡同（前面 N 个 item 走偏时选 E 而非逐个 D）。
- **无惩罚声明**（关键心理设计，对抗 LLM"顺从不认错"倾向）：B/D/E are honorable
  outcomes — misdirection happens; recording it beats patching forward. Choosing E
  does not invalidate completed work: it stays as evidence, and you continue from
  HERE (not from zero). Never rewrite history — corrections append.
- **触发节流未开**：评估后默认 GOPI_TASK_REVIEW_EVERY=1（每次提交都 review）。
  真实任务 item 数通常 <10，每次 ~180 token 成本可忽略；节流留作开关不默认开。

### Verification (v2)
- Build: `go build ./...` OK
- 单测 R1 升级：注入消息含全部 A-E 动作码 + "honorable outcomes" 无惩罚声明 +
  ON-TRACK 恰好一次；R2/R3/R4 原样全绿（R4 = 模型选 redo exit → 同 task_id 新结论
  SUPERSEDES append-only，2 条结论都在，run 正常结束）。
- 回归：`internal/agent` 全量测试绿 + 全仓 `go test ./...` 13/13 包绿。
- Commit: 8cc1fd1（feat/post-commit-review-verdict-menu 分支，worktree 共享主仓 refs）

## Files
- `internal/agent/loop.go`：+SkipTaskReview 字段、NewAgent 读取、taskAreaDirty 块
  注入调用、injectPostCommitReview + marker/menu/no-punishment 常量
  （v2 替换 responsePrefix 为 verdict menu + no-punishment）
- `internal/agent/taskarea_review_test.go`：新测试（R1-R4）
- `devdoc/post-commit-review.md`：v2 细化记录（本文件）

## 测试代码说明（R4 复用 e2eAgent）
R4 中第 2 次提交（redo）会再 fire 一次 review；exit 轮在下一轮，故用 DB 断言（2 条
结论 append-only）而非 wire 断言。
