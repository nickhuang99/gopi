<!-- doc-class: historical -->
# Task Report: bug030 — CLI 探测调用（gopi version / -h / 裸 gopi）不再产生 session 空壳 + 永久 in_progress 台账行

## Completed

- **version 子命令短路**（`cmd/agent/cli_version.go`）：`gopi version` / `--version` / `-v`
  在 `main()` 分发最前端短路（与 memory/web_search 等子命令同层、先于 config 解析），
  打印构建版本（-ldflags stamp > 内嵌 VCS revision > "dev"）后直接 return。
  修复前：未知参数走 prompt 路径 → 完整 agent session（session_start 写 JSONL 空壳，
  进程即退无 session_end）+ taskregister start hook 登记 auto 行（summary=CLI 参数），
  end hook 永远不会跑 → 行永远 in_progress（污染 HP in_progress 视图 + stale 检测误报）。
- **`-h` / 裸 gopi 无需修复**（核实结论）：`-h/--help` 在 main() 顶部 `os.Exit(0)`
  （不建 agent）；裸 gopi（无参数+TTY stdin）打印 usage 后 `os.Exit(1)`
  ——两者都在 newAgent() 之前。Epic 猜测的"它们也建 session"不成立，
  真正的污染源只有 version 落空进 prompt 路径这一条。
- **验收测试**（`cmd/agent/cli_version_test.go`）：
  - `TestVersionSubcommand`：version/--version/-v 输出版本、不落 prompt 路径；
  - **`TestVersionSubcommandNoLedgerRow`（bug030 回归守卫）**：沙箱 GOPI_HOME 跑完
    version 后，sessions/ 不得出现任何 JSONL，memory.db（若存在）tasks 表 0 行
    ——同时钉死 bug 的两个半边（session 空壳 + 台账行）。
- **HP 台账清理**：Epic 自己的探测行 `73dd5571…`（[auto] 发现（nick：HP 台账 2 条
  summary=version…，model glm-5.3-flash）已 `gopi task update <id> --status cancelled`。
  修后真机验收：in_progress 计数 1→**0**，sessions/ 数量不变。
- **搭车修复（同 worktree 已暂存内容，独立 commit）**：`internal/tools/symbol.go`
  exuberant-ctags 5.9 能力探测（--languages 过滤 / --map-* / --extras=+q vs --extra=+q），
  `search_test.go` JS/TS 用例在 exuberant-era 二进制上 skip 而非 fail。
- **附送 bug 工单**：`internal/agent` 2 个 HEAD 预存失败
  （TestAgent_Run_ContextTrimReplaceSlimming / TestAgent_Run_ToolResultPayloadOnce，
  经临时 worktree 对照 HEAD 2ed1ed1 确认预存）→ 已入队 t-1788076890（tags=bug）。

## Not Done

- `gopi tasks` 顶层复数子命令不存在（只有 `gopi task`）——README 无引用，不属本 Epic，未改。
- 预存失败 2 例的修复（见上，已移交独立工单）。
- 未在本 worktree 做 chat 全量回归（`gopi chat` 正常建 session 的行为由
  TestChatRegister 覆盖，未受本改动影响；version 分发先于一切 session 路径，
  结构上不可能影响 chat）。

## Verification

- Build: `CGO_ENABLED=0 go build ./cmd/agent/` → OK
- Tests: `go test ./cmd/agent/ ./internal/agent/` → cmd/agent 全 PASS（含新增回归
  9 例 PASS）；internal/agent 2 FAIL 为 HEAD 预存（对照验证，已建 bug 工单）
- Real machine: `gopi version` → `2ed1ed119752-dirty`；台账 in_progress 1→0；
  `~/.gopi/sessions/*.jsonl` 33 个不变
- Commits:
  - `0a00728` feat(cli): gopi version subcommand — zero-session CLI probe short-circuit (bug030)
  - `b1061bb` fix(code-search): exuberant-ctags capability probing — languages/map/extras

## Design notes

- 判断点选 main() 分发层（version 与其他子命令同层），不选 taskregister gate：
  version 的正确语义就是"不进 prompt 路径"，修 session/ledger 层只会治标。
- taskregister.go 无需改动：start hook 由 SessionLog 存在性 gate；session 不存在，
  行就不会登记——修好上游，下游自然干净。
- 测试断言"memory.db 不存在也算过"：version 路径连知识库初始化都不该触发，
  这是最强通过形态。
