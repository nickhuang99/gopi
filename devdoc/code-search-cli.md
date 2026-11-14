<!-- doc-class: historical -->
# Task Report: gopi code-search CLI 子命令（可测试性 + 脚本化）

## 目标
`code_search` 工具只有 LLM 调用入口，没有 CLI 暴露。仿 cli_memory/cli_log/cli_websearch
模式新增 `gopi code-search <symbol>` 子命令，让用户/脚本直接测试 code_search 能力，
并以结构化文本输出（人可读 + 可 grep），找不到符号时 exit code 非零。

## Completed

### 新增 CLI 子命令 `gopi code-search <symbol>`
- **文件**：`cmd/agent/cli_code_search.go`
- **功能**：对当前目录（或 `--root DIR`）调用 `internal/tools/symbol.go` 的
  `tools.CodeSearch` 后端（ctags 索引 `queryDefinition`/`ensureTags` + lazy 重建 +
  ripgrep 兜底），输出结构化文本：
  ```
  NewAgent  internal/agent/loop.go:1111  func  func NewAgent(model string) (*Agent, error) {
  ```
  每行 = `name <tab> file:line <tab> kind <tab> signature`（greppable）。
- **找不到符号**：输出 `no symbol found: <symbol>` + **exit code 1**（脚本化友好）。
- **ctags 缺失优雅错误**：`gopi code-search: code_search: ctags: exec: "ctags": executable file not found in $PATH` + exit 1（不抛裸 exec 错误）。
- **`--root` 相对路径修复**：resolve 成绝对路径再传给后端，规避 ctags `cmd.Dir=root`
  时相对 tagsPath 指向 `<root>/<root>/.gopi-tags` 的缺陷（agent 只用 "." 无此问题，
  但 CLI 暴露 `--root` 后成为用户可见缺陷）。

### help（用户强调——防再忘）
- **`gopi help` 列出 code-search + 一行描述**：在 main.go 的 top-level help 循环里新增
  `help`/`help-env` 裸词别名（等价 `-h`/`--help-env`），并把 code-search 子命令写进
  `internal/config/help.go` 的 `FormatUsage()` Subcommands 段。
- **`gopi code-search --help`** 输出用法（`<symbol>` 参数 + 可选 `--root` + exit codes）。
- help 注册机制：subcommands 的 dispatcher 在 main.go 的 top-level `-h` scan **之前**
  检查（`isCodeSearchSubcommand`），保证 `gopi code-search -h` 显示子命令用法而非主用法。

### 测试（正向 + 负面 + help 断言）
- **`cmd/agent/cli_code_search_test.go`**：
  - `TestParseCodeSearchArgs`（纯函数解析：符号、--root、缺失、空、多余、unknown flag）
  - `TestCodeSearchDefaultRootInParse`（默认 "."、--root 覆盖）
  - `TestCodeSearchBackendFindsDefinition`（真实 `tools.CodeSearch("CodeSearch")` → 断言
    命中 `internal/tools/search.go` + LineNo>0 + Signature 非空 —— 核心交付）
  - `TestCodeSearchBackendNoSymbol`（构造运行时不存在的符号 → 0 命中）
  - `TestIsCodeSearchSubcommand`（sibling 排斥 + nil）
  - `TestCodeSearchUsageShowsHelp`（code-search 自身 -h 内容断言）
  - `TestTopLevelUsageShowsCodeSearch`（top-level help 含 code-search —— 防再忘）
- **`usage_test.go`**：`TestTopLevelUsageListsSubcommands` 列表加 `"gopi code-search"`。
- **`main.go`**：dispatcher 挂到 `isCodeSearchSubcommand`.

## Not Done
- cli_log 按工具过滤搜索（log --search 已覆盖 code_search 的 session 记录）未额外改动。
- 未改动 agent loop 的 codeSearchScope（CLI scope 与之一致：RootPath 当前/--root、
  IncludeGlob tools.CodeSearchGlobs、ExcludeGlob .git/** vendor/**、MaxResult 50）。

## Verification

### 真实仓库实测（gopi-cs-cli worktree）
```
$ go build -o gopi-cs-bin ./cmd/agent

# 1. code-search NewAgent
$ ./gopi-cs-bin code-search NewAgent
NewAgent  internal/agent/loop.go:1111  func  func NewAgent(model string) (*Agent, error) {
#        ↑ file:line == loop.go:1111（Epic 预期 "loop.go:1111 附近"）

# 2. code-search --help
GoPi code-search subcommand — expose the code_search tool as a CLI command.
Behavior is identical to the tool_use call (... ctags ... ripgrep ...).

# 3. gopi help 含 code-search
$ ./gopi-cs-bin help 2>&1 | grep code-search
  gopi code-search <symbol> [--root DIR]

# 4. 找不到符号 → exit 1
$ ./gopi-cs-bin code-search ZZZNothingHere9
no symbol found: ZZZNothingHere9         (exit 1)

# 5. ctags 缺失 → 优雅错误 + exit 1（PATH 隐去 ctags、保留 rg）
gopi code-search: code_search: ctags: exec: "ctags": executable file not found in $PATH
```

### Build + Tests（全绿）
- **Build**：`go build ./...` → OK
- **Tests**：
  - `go test ./cmd/agent/...` → ok
  - `go test ./...` → 全包 ok（cmd/agent, internal/agent, internal/checkpoint,
    internal/config, internal/deepseek, internal/gopilog, internal/proxy,
    internal/text2img, internal/tools）
- **Commit**：见下方

## 额外：main.go 裸词 help 别名
`gopi help` / `gopi help-env` 现在等价 `-h` / `--help-env`（task 明确要求 "gopi help
输出里列出 code-search"，而此前 `gopi help` 会把 "help" 当任务 prompt 灌给 agent）。
仅在 `help` 是首参时才触发，避免误伤含 "help" 字样的 prompt。
