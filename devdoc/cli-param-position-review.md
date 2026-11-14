<!-- doc-class: historical -->
# Task Report: CLI 参数风格/结构全面评审（positional param testing epic）

日期: 2026-09-08
分支: feat/cli-param-review（worktree: ~/workspace/gopi-cli-param-review）
类型: 评审报告（code-path analysis + 活体验证）—— 非 feature 实现

## 结论摘要

Epic 提出的问题：「为什么 `log --last 3` 这类一个词 cmd + 参数 的风格被支持，而 `task list --last 3`（两层级 cmd）不支持？」

**答案：不是解析架构的差异，而是各 verb 白名单放行了哪些 flag 的差异。** `task` 与 `log` 用的是**同一个**手写解析器（`memoryFlagSet`）与**同一个**严格 per-verb 白名单模式（`taskFlagMember` / `logFlagMember`）。`taskFlagMember("list")` 只放行 `db|status|all`；`logFlagMember` 放行 `last`（外加约 20 个视图 flag）。`--last` 是 log/stats/raw/vision 的 verb 级选项；`task list --last 3` 若要做，还需要一个产品决策（取哪 N 条？list 本就不排序，要不要加 --sort?）—— 这是产品问题，不是解析器缺口。跨 verb flag 拒绝（taskFlagMember 拒收 list 上的 --last）是**有意的设计原则**（「跨子命令 flag 拒绝」，见 devdoc/tasks-ledger-impl-report.md），在整个 CLI 一致执行 —— `log --last` 同样会被 task/show 拒收。

**发现的唯一真实解析器隐患**：verb 在前、flag 在 verb 之前的写法（`--db X task list`）会落入 ssh-passthrough 启发式分支 → 把 `X` 当 host、丢弃、再 dispatch `task list` → **静默地跑在默认库上**（活体验证：X.db 从未被创建）。只有 `--settings`/`--home` 这一对 process-global 选项有真正的任意位置保证。

## 分析过程

详见本会话结论链（write_task_area 记录 #220–#223）。核心文件证据：

- `cmd/agent/main.go` 约 149 行起：23 个 `isXxxSubcommand(args)` 谓词链，各谓词检查 `args[0] == verb 词` 后跑 `runXxx(args[1:])`。
- `internal/config/settings.go:357` `BootstrapEnvFromArgs`：只从**任何** argv 位置剥离 `--settings PATH` / `--home PATH`（空格或 `=` 形式）→ env 提升 + dispatch 剩余参数。
- `internal/config` `FlagOverrides`（agent 路径选项 --model/--timeout/--prompt/…）。
- 各子命令 flag：手写 `memoryFlagSet`（cli_memory.go:192），允许 verb 之后 flag/positional 交错、顺序无关、`--name value` 与 `--name=value` 双形态；未知 flag 硬错误；verb 必须是 `args[0]`。

## Completed

1. **活体验证 `--settings` 任意位置**（独立 GOPI_HOME + 假 settings 文件，bin 1ce827d）：
   - 前导 `gopi --settings S task list` ✅、中间 `gopi task --settings S list` ✅、尾随 `gopi task list --settings S` ✅、flag 之后 `gopi task list --all --settings S` ✅ —— 全部 "No tasks found." rc=0。
   - 根因：`BootstrapEnvFromArgs` 在 dispatch 前扫描**整个** argv，且剥离后 verb 解析器永不见它 → 该对是真正任意深度位置无关。
2. **活体验证不对称性**：
   - `task list --last 3` → "unknown flag --last"（严格白名单）。
   - `task --db X list` → "missing subcommand"（verb=args[0]，flag 打头则空）—— 与代码注释声称「flag 可在 verb 前」矛盾。
   - `task list --db X` → 正常，创建 X.db。
   - `--db X task list` → ssh-passthrough 分支 → 跑**默认**库（X.db 从未创建，静默）—— 唯一真实危害。
3. **结构评审**（三层解析 + 三种不同 option 词汇表，见报告正文）。
4. **验收口供**：本评审**明确不提出「支持 `task list --last 3`」的 feature 请求**—— 代码路径分析证明那是产品决策而非解析缺口；且 epic 授权 agent 基于 code-path analysis 自由拒绝。

## 结构评审 —— 建议（供未来开发，非本任务实现）

当前 CLI 实际只有两种参数风格：
- **(A) 任意位置 process-global 选项**：仅 `--settings`/`--home` 一对（整 argv 预剥离）。
- **(B) verb 后 per-verb flag**：其余一切选项。verb=args[0] 严格；每 verb 白名单；flag/positional 在 verb 后交错。

未来开发/maintain 建议（降低隐患类别）：
1. **统一两级模型 cmd+sub+flags**：verb=首个 token + hoisted globals；所有非 global 选项一律置于 verb 之后。消灭 ssh-passthrough 静默误路由类别。
2. **消灭三层 option 词汇表**：process-global 配置 / agent-path FlagOverrides / per-verb 白名单 三套词汇表互不相同 —— 这是维护风险根。新 flag 一律先问「属于哪一层」。
3. `--settings` 的任意位置保证值得保留并**作为唯一例外**文档化；其它选项不做全局提升，避免更多隐式重排。
4. verb 前 flag 的启发式（ssh-passthrough）在非 ssh 上下文中应显式报错而非静默丢参（今日 `--db X task list` 静默跑默认库）。

## Not Done（诚实声明）

- **未实现任何代码改动**（除报告外零 diff）：epic 本质是评审请求，且含「agent 可自由拒绝」条款。`task list --last 3` 等新语法**未加**—— 需产品决策（哪 N 条/排序语义）+ 新白名单入口。
- 未跑完整 `go test ./...`（涉及网络/LLM 的既有用例会挂，memory #1489 已知）；仅确认改动为零（本分支无代码 diff，build 不受影响）。
- ssh-passthrough 启发式的修复（建议 4）留未来，因其触及 ssh remote-command 行为，需独立设计 + 契约测试。

## Verification

- 活体验证：见上（全部 rc=0 / 明确报错，已在本会话逐条执行并记录）。
- 代码证据：main.go dispatch 链、settings.go:357 BootstrapEnvFromArgs、cli_memory.go:192 memoryFlagSet、taskFlagMember/logFlagMember 白名单、cli_tasks.go parseTasksArgs —— 本会话已逐一 read 确认。
- Build: 无代码改动，分支 = master 1ce827d。
- Tests:  未跑全量（无代码改动；网络/LLM 用例已知挂，非本任务引入）。
- Commit:  见 git log（本报告唯一提交）。

## 关联记忆

- #1489 / #1491 CLI contract-testsuite 方法（本次评审证实其根基：verb=args[0] 严格 dispatch）。
- #1427 `--settings` 在 `task run` 的 launch 命令中的位置（与本评审的任意位置剥离一致）。
- #1230 gopi CLI node-sync 家族 code-review（ssh passthrough strings.Fields 引号拆坏 = 同区启发式的另一已知坑）。
