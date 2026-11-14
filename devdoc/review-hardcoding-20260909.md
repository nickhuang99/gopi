<!-- doc-class: historical -->
# Review Report: Hardcoding-vs-Constant 全库审查 (只报不改)

Date: 2026-09-09
Epic: t-1788904463 (report-only code review)
Scope domain: cmd/agent + internal/agent + internal/tools + task_session.go/tasks.go constant precedents
Discipline: 只报不改 — findings filed as bug tickets (tags=bug,hardcoding,severity:X), queue-owned fixes

## 1. 审查方法（adoption audit）

- **code_search 调用数: 5**（taskSessionsRootName / syncTaskUnknownNode / archiveTagRefPrefix / validTaskSource / mergeBacklogTagName 定义查证）
- keyword_search 调用数: 5（args[0] 动词 / source 值 / isXxxSubcommand / 常量用法 / 配置键）
- read 调用数: 2（validTaskSource 上下文、task_session 常量）
- **零 bash grep 猜证** — 全部行号由 keyword_search/grep -rn 精确输出支撑（grep 仅用于行号枚举非定义猜证）

## 2. 发现分类表

| # | ticket | severity | 类别 | 现象 | 文件:行 |
|---|--------|----------|------|------|---------|
| 1 | t-1788905959 | major | 双份定义+跨包 | merge-backlog 标签 3 处定义跨 2 包（含同包双份） | tasks.go:2617 + cli_tasks.go + task_merge.go |
| 2 | t-1788905960 | major | 重复字面量+行为串 | merge-conflict 标签同包双常量 + 行为字符串裸散落 | task_merge.go + 多文件 |
| 3 | t-1788905960-1 | major | 双份定义(跨包镜像) | archive 标签命名空间跨包镜像，注释自认重复 | task_merge_backlog.go:46 + cli_tasks.go:1560 + task_finish_cleanup.go:64 |
| 4 | t-1788905960-2 | major | 裸字符串关键字 | SQL 状态字面量绕过 tools 导出 TaskStatus 常量 | cli_tasks.go:1870/2232/1102 |
| 5 | t-1788905960-3 | major | 裸字符串关键字 | cmd/agent CLI 镜像层未 import toolnames——工具名扁平别名硬编码 | cli_websearch.go:67 等 7+ 处 |
| 6 | t-1788906043 | major | 裸字符串关键字 | 24+ isXxxSubcommand 分派谓词动词字面量裸写，无 CLI 动词常量源 | cli_web.go:103 / cli_tasks.go:385 等 |
| 7 | t-1788906082 | major | 文件名/目录名遗漏 | "sessions" 目录字面量 10 处绕过 taskSessionsRootName（常量仅自用 2 处） | tasks.go:1485/1521/1666、taskzombie.go:120、session.go:65/608 等 |
| 8 | t-1788906043-1 | minor | 状态机值 | source 值 llm/human/auto 无导出常量，validTaskSource switch + 6 处跨包写入点裸写 | tasks.go:402 + loop.go:4797 等 |
| 9 | t-1788906083 | minor | 重复字面量(同包漏引) | "unknown-node" 同包常量 syncTaskUnknownNode 被 3 兄弟文件裸写绕过 | memory_sync.go:300、memory_sync_procedural.go:166、memory_sync_assets.go:304 |

**计数**: 9 tickets = 7 major + 2 minor（bug-flow tally 150-151）

## 3. investigated-and-cleared 列表（负发现）

| 项 | 位置 | cleared 理由 |
|----|------|-------------|
| `attempt < 10` magic number | tasks.go:2940 (TaskNextFiltered) | 记忆 #1237 已报（P2 finding#7），live 未修 → 不重复报，归属原 ticket |
| 状态机小写常量 + 导出别名 | tasks.go:51-82 TaskStatusPending 等 8 态 | 已单源化，健康（先例，非缺陷） |
| todo 域三态 (todo/progressing/completed) | internal/agent/tasklist.go:19 | 独立于 DB 任务状态域，域内自有常量，非缺陷 |
| GOPI_* env 覆盖默认值（如 worker idle 900 等） | cmd/agent/* | 已知例外：env 覆盖的默认值不算（epic 明列豁免） |
| toolnames 包 23 工具名常量 | internal/toolnames/names.go | 已由 t-1788904350 落地 master（757f6ec），本审查只补 cmd/agent 消费面遗漏（→5960-3） |
| 契约测试 dispatcherOfToken 镜像 | cli_contracts_test.go:96 | 守护谓词集合与 main.go 链一致——健康机制，但不防动词值散落（→6043 补足） |
| internal/proxy request.go:295 "auto"/"any" | proxy 域 | LLM API 协议值（tool_choice），独立命名空间非任务域 source 值，豁免 |
| scripts/*.sh 与 *_test.go 的状态/source 字面量 | — | DB 夹具/运维脚本，豁免（ticket 内注明） |

## 4. 统计

- 发现: 9（major 7 / minor 2）
- cleared: 8 项
- 归属: 100% — 每个实证硬编码点均有 ticket 或 cleared 理由
- 补漏: 早期口头交接的 5 张 ticket 未覆盖 CLI 动词(source 值)两域 → 审查中补 4 张，最终 9 张全覆盖 epic 四类反模式

## 5. 关键根因洞察（供修复方参考）

1. **跨包共享值 = 最高风险面**: archive 前缀、sessions 目录、source 值三类跨包问题占 major 大头。Go unexported 常量无法跨包 → 作者倾向镜像/裸写。toolnames 包先例证明"小导出常量包"模式有效，应推广。
2. **常量定义了没引用**: taskSessionsRootName(定义 1 处/引用 2 处/裸写 10 处)、syncTaskUnknownNode(定义 1/裸写 3) 是同包内漏引——不是跨包难引用，是回填纪律缺失。穷举测试（断言裸字面量=0）是唯一防回潮手段。
3. **CLI 用户面动词是独立命名空间**: 与工具名 wire 值（toolnames）分离，目前零常量源。契约测试守护"集合一致"但不守护"值常量化"——两层问题需分开解决。

## 6. 验收对照

- ✅ 所有实证硬编码点有归属 ticket 或 cleared 理由
- ✅ 报告含 code_search 调用统计（5 次）
- ✅ 本 devdoc commit: `devdoc/review-hardcoding-20260909.md`
- ✅ 只报不改：Go 源码零改动（git status 干净，仅本报告新增）
