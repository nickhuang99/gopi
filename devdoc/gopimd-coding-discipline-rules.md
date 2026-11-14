<!-- doc-class: historical -->
# Task Report: GOPI.md 编码纪律强化（Rule 1 扩展 + Rule 13/14/15）

Date: 2026-09-09
Epic: GOPI.md coding-discipline strengthening (docs-only)

## Completed

- **Rule 1 扩展**：标题 "No Magic Numbers" → "No Magic Numbers or Bare
  String Keywords"，拆 1a (magic numbers, 原文保留) / 1b (bare string
  keywords 新增)。1b 覆盖 9 findings 的六类字符串关键字——工具名
  (toolnames)、任务状态 (TaskStatus*)、tags、source 值、CLI 动词、
  目录/文件名——每类标注其常量源 + BAD/GOOD Go 示例 + Why。
- **Rule 13（新）Constants Exist to Be Referenced — Never Bypass Them**：
  常量定义了必须引用，裸字符串绕过 = 缺陷（taskSessionsRootName 定义
  1×/引用 2×/裸写 10×；syncTaskUnknownNode 定义 1×/裸写 3×）。grep-gate
  穷举测试是唯一防回潮手段——附 TestUnknownNode_MirrorLiteralGate 先例
  与测试模式代码。
- **Rule 14（新）Shared Values Live in a Small Exported-Constant Package**：
  跨包共享值用 toolnames 式小导出常量包（archive 标签/sessions 目录/
  source 值三类跨包问题占 major 大头）。规则：小而叶、导出、不镜像不
  裸写、CLI 用户面动词也需常量源。BAD/GOOD 示例。
- **Rule 15（新）No Silent Failure**：15a omitempty 陷阱（零值字段被
  omitempty 抹掉 → DeepSeek deserializer 400——memory #951 实证 +
  future_field_regression_test.go 先例）；15b 错误静默吞掉（nohup
  >/dev/null 吞 handoff 错误先例）。BAD/GOOD + decision rule。

## Not Done（检查后 cleared——不新增条目）

以下候选纪律审查后判定**已覆盖/不属于编码纪律**，未占规则号：

| 候选纪律 | 判定 | 理由 |
|----------|------|------|
| 只报不改（review 只报不改，修复走队列） | cleared | 已是任务契约：review 任务由 task_submit bug 队列承接修复（review-hardcoding doc §discipline + task_submit tags=['bug'] 机制），无需编码规则 |
| worker-death reconciliation（死 worker 收编） | cleared | 运维/PA 巡查流程非编码纪律——devdoc/worker-death-reconciliation 已记录处置模式，属调度运维域 |
| Rule 3 工具数（23 tools） | cleared | 有 TestGOPIRule3_MatchesClaudeToolDefs 契约测试守门，非遗漏 |
| 状态机小写常量 | cleared | 已单源化（tools 导出常量先例，健康） |
| env 覆盖默认值 | cleared | 已知豁免（review-hardcoding cleared 表） |
| proxy "auto"/"any" tool_choice | cleared | 独立 API 协议命名空间，非任务域值 |

## 检查记录（其它纪律核查结果）

- **规则编号稳定**：Rule 1-12 编号与内容未动（仅 Rule 1 标题/正文扩充），
  新增规则追加为 13/14/15——验收③ ✅
- **纯文档改动**：仅 GOPI.md 1 文件 + 本报告；Go 代码零改动——验收⑤ ✅

## Verification

- Build: 不适用（纯文档改动，无 Go 代码变更）
- Diff: `git diff --stat` → GOPI.md +184/-1（Rule 1 扩展 +3 条新规则）
- 规则数：12 → 15（Rule 1 扩展 + Rule 13/14/15 追加）
- Commit: 见下
- 报告: devdoc/gopimd-coding-discipline-rules.md（本文件）
