<!-- doc-class: normative -->
# Review Report: source 值域 llm/human/auto 无导出常量（finding #8 复核）

Date: 2026-09-09 (复核自 t-1788904463 审查 #8 / ticket t-1788906043-1, 该 ticket 已入队 running)
Epic: review-hardcoding-20260909
Discipline: 只报不改 — 本报告是五要素审查单的**复核修正版**（原引用行号过期 + 计数低估），不做代码改动。

## 五要素

### 1. 现象（经当前 master 复核）

**值域定义**（唯一词表，未导出）:
- `internal/tools/tasks.go:407` — `validTaskSource` switch `case "llm","human","auto"`

**读取/语义 switch（额外 2 处，原单未列）**:
- `internal/tools/tasks.go:1624-1628` — `taskSubmittedBy` 渲染 switch（三值 → 提交者行）
- `internal/tools/memory_sync_delta.go:639` — `syncRowIsMirrorNoise` 谓词 `src != "human" && src != "llm"`（auto/空 = 登记噪音）

**非 test 写入点（原单列 6 处，实查 14 处——行号漂移 + 漏 8 处）**:

| 值 | 文件:行（当前 master） | 场景 |
|----|----------------------|------|
| llm | `internal/agent/loop.go:4636` | task_submit 工具落库 ✓（原单 4636 准确） |
| human | `cmd/agent/cli_tasks.go:2487` | `gopi task submit` CLI 默认（原单 2498 已漂移 11 行） |
| auto | `internal/agent/taskregister.go:171` | 会话任务 auto 登记 ✓（原单 171 准确） |
| auto | `cmd/agent/cli_tasks.go:1577` | TaskSubmit 分支推送提示（原单 1588 漂移） |
| auto | `cmd/agent/cli_tasks.go:2053` | merge-backlog 提交（原单 2064 漂移） |
| auto | `internal/tools/task_merge_backlog.go:189` | TaskSubmit（原单 194 漂移） |
| auto | `internal/tools/task_adoption.go:328` | 未列（`source = opts.Source; ""→"auto"`） |
| auto | `internal/tools/task_merge.go:292` | 冲突行 TaskSubmit 默认（未列） |
| auto | `internal/tools/repo_resolve_row.go:147` | resolveRowCreate TaskNewOID（未列） |
| auto | `internal/tools/repo_resolve_row.go:549` | resolve rework TaskSubmit（未列） |
| auto | `internal/tools/tasks.go:1098` | insertTaskRow 空值默认（未列） |
| auto | `internal/tools/tasks.go:2783` | TaskSubmit 空值默认（未列） |
| auto | `internal/tools/tasks.go:4028` | rework/reopen 新行（未列） |
| auto | `internal/tools/memory_sync_task.go:894` | 远端 import 行空值默认（未列） |

**校验消费点**: `validTaskSource` 两处 gate — `tasks.go:2790`（TaskSubmit 内）、`memory_sync_task.go:896`（import 行）。

### 2. 根因（复核确认 + 补正）

source 是 3 值小集合，作者用注释（tasks.go:399）+ 单点 switch 就地管理。跨包共享（internal/agent、cmd/agent、internal/tools 三域 14 写入点）却无单一来源。TaskStatus 有完整先例（tasks.go:50-91：私有小写常量 + taskStatusSet + 导出别名 TaskStatus*），source 词汇完全无对应——`TaskSourceLLM/Human/Auto` 不存在（已查证零匹配）。

**补正 1**: briefing 称"loop.go（agent 域）与写入点（internal/tools、cmd/agent）隔 2 包"，属实。但依赖方向非障碍：internal/agent 与 cmd/agent 均已 import internal/tools（taskregister.go:8、loop.go:22、cli_tasks.go:16 均已证），可直接 `tools.TaskSourceLLM` 引用，无需镜像豁免登记。

**补正 2**: briefing 称 "archive/taskingest 的 mirrorable 源过滤（'pending','claimed'）也依赖此值域"——**不成立**。`archive/taskingest.go:78` `taskIngestMirrorableSourceStatuses = 'pending','claimed','in_progress'` 过滤的是 **status**（非终态可镜像），不是 source 列；taskingest.go:29 明言 "source column is NOT touched"。此依赖点应从报告中剔除。

**补正 3**: 原单 6 写入点低估——实为 14 点 + 2 语义读取 switch + 1 谓词。修正后问题面是原单的 ~3 倍。

### 3. 风险等级判定：minor（维持，理由经复核修正）

维持 minor 依据：
- 值域 3 值稳定（schema 无 source CHECK 约束——已查证零 SQL 约束，Go 侧 validTaskSource 是**唯一**执行闸）；
- 加新值时 TaskSubmit 内 gate（tasks.go:2790）会**运行期硬失败**，漂移不会静默——非 test 写入全走 TaskSubmit/TaskNewOID，校验天然兜底；
- 风险是**无编译期守卫 + 值域定义处与 14 消费点隔 2 包**：改名/加值漏同步会在运行期才暴露（memory_sync import 路径若绕过会静默写坏行）。

升级考量（仍判 minor）：镜像值 semantic 谓词 memory_sync_delta.go:639 若与词表漂移，会把 llm 行当噪音吞掉——但该谓词与词表同包同文件族，改动同步成本低。风格+扩展性缺陷，不强推。

### 4. 建议（采纳修复路径）

仿 TaskStatus 先例（tasks.go:50-91 同位置）:
1. tasks.go:399 注释区附近增：
   `const ( TaskSourceLLM = "llm"; TaskSourceHuman = "human"; TaskSourceAuto = "auto" )`
   （导出别名 or 唯一小写+别名，跟随 TaskStatus 现有模式）
2. `validTaskSource` switch 改引三常量；
3. 14 处写入点改引（表见上）；taskSubmittedBy switch 与 memory_sync_delta.go:639 谓词同步改引；
4. 注释中 `"" = "auto"` 语义（task_adoption.go:60、task_merge.go:64、repo_resolve_row.go:33）改引或加常量引用。
5. 归档 test/夹具字面量保留（memory_sync_delta_test 等 40+ 处 test 行不迁移）。

验收线：tasks.go 定义三常量 + validTaskSource 引常量；非 test 源码 `"llm"/"human"/"auto"` 裸写（表内 14 点 + 2 switch + 1 谓词）清零；`go build ./...` + `go test ./internal/tools/ ./internal/agent/ ./cmd/agent/` 绿。grep-gate 回归测试可仿 archive-ns（断言字面量仅出现在 tasks.go 一处）。

### 5. 边界 & 归属

- investigated-and-cleared：archive/taskingest mirrorable 过滤（status 非 source，剔除）；
- 依赖方向已证非障碍（agent/cmd 均已 import tools）；
- ticket t-1788906043-1 已在队列 running——本报告供该 worker 与 PA 复核采纳；若修复单已按原 6 点开工，需按本报告 14 点扩围。
