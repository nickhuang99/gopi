<!-- doc-class: historical -->
# Task Report: CLI Subcommand Groups (memory/web family verbs)

## Epic
当前 CLI 扁平动词与 `gopi task <sub>` 组式风格不一致：`gopi memory_search`、
`gopi memory_store`、`gopi memory_get/update/delete/feedback`、`gopi web_search`、
`gopi web_fetch` 都是 `gopi task list` 式组命令的异类。统一为组式子命令
（`gopi memory search`、`gopi web fetch`），为 tab-completion 铺路：
`gopi <TAB>` 弹第一级动词（memory / web / task / log …），再 `gopi memory <TAB>`
弹第二级（search / store / …）。

## Completed
- **memory 组**（`cmd/agent/cli_memory.go`）：`gopi memory search|store|get|
  update|delete|feedback|sync`。`isMemorySubcommand` 现在派发 `memory` 组；
  新增 `runMemoryGroup`（无子命令/未知子命令 → usage + exit 1，不再落入
  agent loop）+ `memorySubcommandNames`（第二级候选集，tab 补全用）。
- **web 组**（`cmd/agent/cli_web.go`、`cli_websearch.go`、`cli_webfetch.go`）：
  `gopi web search|fetch` 是工具组；裸 `gopi web` / 服务器 flags
  （`--tools` / `--addr` / `-h`）仍是 web chat 服务器（向后兼容）。
  未知 `gopi web <x>` → usage + exit 1（原来落入 agent loop）。
- **旧扁平动词 = 别名（不删除）**：`memory_search`→`memory search`、
  `web_fetch`→`web fetch` 等全部保留，运行前 stderr 打一行
  `note: gopi X is deprecated — use gopi Y`，行为完全一致 → 脚本零破坏
  （Rule 8/竞争收尾教训：alias, never remove）。别名表 `memoryLegacyAliases`
  集中声明。
- **死 help 修复**：`gopi memory_feedback_list`（仅出现在 `gopi -h`、从未
  真正派发 —— 会落入 agent loop）现在路由到 audit 子命令
  （`--list` 隐含），stderr 打 deprecation note。
- **错误前缀教学化**：`runMemorySubcommand` 的错误前缀经
  `memoryDisplayName` 输出规范组式拼写（`gopi memory search: ...`）。
- **顶层 usage**（`internal/config/help.go`）：Subcommands 段全部改为组式
  （memory <sub>、web search|fetch、web = 服务器）；`--db` 帮助文字同步。
- **usage_test**：断言组式命令都被广告、旧扁平动词**不再**被广告。
- **scripts**：`m1_acceptance.py`、`m2_acceptance.py` 改用
  `gopi memory search` / `gopi memory feedback`。
- **测试**：新增 `cmd/agent/cli_group_dispatch_test.go`
  （别名表、组候选集、display name、web 组 usage、服务器 flag 识别）。
- **CHANGELOG.md**：Unreleased 段新增条目。

## Not Done
- 未触碰 agent 内部 LLM 工具名（`memory_search` tool 等）—— Epic 明言
  CLI 不必与内部 toolcall 同名，内部名不动。
- `gopi log --memory/--web` 过滤器按 LLM 工具名过滤，保持 `memory_search`
  / `web_search` 不变（正确语义，非 CLI 表面）。
- `isMemorySyncSubcommand` 仍在 main 链上（组派发先处理 `memory sync`，
  该函数成为冗余但无害的 fallback，其负向测试保留）。
- `memory sync --serve-assets` 的 ssh 远程命令字符串（fake-ssh E2E 里）
  仍是 `gopi memory sync ...`—— 组式不变，无需改。

## Verification
- Build: `go build ./...` OK（worktree 内）。
- Tests:
  - `go test ./cmd/agent/ -run 'TestMemory|TestWebGroup|TestMemorySyncUsage|
    TestIsWebSubcommandDetection|TestRunWebHelp|TestMemorySyncE2E…'` 全部 PASS
    （含 44s 的 memory sync 二进制级 E2E）。
  - `go test ./internal/config/` OK。
  - 全量 `go test ./cmd/agent/ -count=1` 后台运行中（首次全量跑因另一 agent
    并发跑 `./internal/tools/` 的 SQLite 锁竞争在 dispatch 测试处 10m 超时；
    `TestDispatchToNodeDbPushSeedsPeerRowBeforeRun` 单独跑 19s PASS —— 非本
    改动引入）。
- Smoke（真实二进制，GOPI_HOME 隔离）：
  - `gopi memory` → usage, exit 1（原来 = agent loop）。
  - `gopi memory search "x"` → 正常搜索。
  - `gopi memory_search "x"` → deprecation note + 正常搜索。
  - `gopi memory_feedback_list` → deprecation note + “No feedback recorded”。
  - `gopi web bogus` → usage exit 1（原来 = agent loop）。
  - `gopi web search -h` / `gopi web fetch -h` → 组式 usage。
  - `gopi web_fetch -h` → deprecation note + usage。
  - `gopi web -h` → 服务器 usage（服务器保留）。
- Commit: 见最终提交（feat/cli-subcommand-groups 分支）。
