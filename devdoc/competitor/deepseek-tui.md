<!-- doc-class: historical -->

# DeepSeek-TUI（codewhale-hq/Codewhale@885ef69）

<!-- meta -->
repo: codewhale-hq/Codewhale
sha: 885ef69f5d789c088825ed0d797221c21732c25e
guide: docs/deepseek-tui.md（awesome-deepseek-agent，86 行，抓取 http=200）
url_in_guide: https://github.com/Hmbown/DeepSeek-TUI（guide `:7`；该 URL 现重定向到 codewhale-hq/Codewhale，`git ls-remote --symref` 两边解析到同一 sha）
pinned_sha_verified_at: 2026-11-06T00:14Z（`ssh digital git ls-remote --symref https://github.com/Hmbown/DeepSeek-TUI HEAD` → `885ef69f5d789c088825ed0d797221c21732c25e`）
license: MIT（`LICENSE:1`；`Cargo.toml:65` `license = "MIT"`）→ 开源
证据通道: E1（guide 行号）+ E2（repo@sha:path:line）。E3 不产出（框架 §1 对方 E3 边界 + Rule 27/35），上限 `partial`。
证据原件: `artifacts/stage2/`（`cite-deepseek-tui.txt`、`p2-deepseek-tui.txt`、`e2-deepseek-tui.txt`、`guides/guide-deepseek-tui.md`、`e1-probes.txt`）
判级口径: `devdoc/competitor-analysis-framework.md` §2；本文件无 `unknown`。

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `dsa-docs/docs/deepseek-tui.md:15` `npm install -g deepseek-tui`；`:18` `cargo install deepseek-tui-cli` | 两条具名安装路径（Node 预编译包 / Cargo 源码），OS 分支靠 release 二进制而非包管理器分支 |
| D02 | partial | E1 | guide `:32` "On first run, `deepseek auth` walks you through saving it to `~/.deepseek/config.toml`"；可改走 `DEEPSEEK_API_KEY` 环境变量 | 有首跑向导（`deepseek auth`），是 8 家里 D02 最完整的一档；但无 E2 实现点，只到 E1 |
| D03 | partial | E1 | guide `:56-60` 环境变量表：`DEEPSEEK_BASE_URL`、`DEEPSEEK_PROVIDER`（例 `nvidia-nim`） | 供应商可换既有 base_url 也有 provider 开关；E2 侧仅见 `runtime_mcp_servers` 无关命中 |
| D04 | partial | E2 | `codewhale-hq/Codewhale@885ef69:npm/codewhale/README.md:64` "reads `~/.codewhale/config.toml` for auth and default model settings" | 配置单一载体（TOML）；打印生效值的命令存在（`codewhale config list`，见 `crates/engine/src/tools/file.rs:372` 的提示串） |
| D05 | partial | E1 | guide `:204` `codewhale exec` — "One task without the interface"（原文的命令形态用 `ssh digital 'curl -sSL https://raw.githubusercontent.com/Hmbown/DeepSeek-TUI/885ef69f5d789c088825ed0d797221c21732c25e/README.md' | sed -n '204p'` 取回）；`:162` `--output-format stream-json` | headless 一次性执行有明确 verb（`exec`）与 `--continue` 续跑；E2 侧未见对应 flag 断言 |
| D06 | partial | E1+E2 | guide `:163` `exec --continue`；`codewhale-hq/Codewhale@885ef69:crates/engine/src/session_manager/test_cases_02.rs:685` `fn resume_session_persists_repair_once()` | 可列（Ctrl-R 会话选择器，`web/lib/i18n/dictionaries/zh/docs-troubleshooting.ts:84`）可续（`--continue`/`--resume`） |
| D07 | partial | E1+E2 | guide `:76-82` MCP/Skills/Hooks 三节；`codewhale-hq/Codewhale@885ef69:crates/engine/src/core/engine.rs:2578` `runtime_mcp_servers`；`crates/engine/src/core/engine/child_host.rs:74` `skills_dir` | 8 家里最全的扩展面：MCP **client 与 server 两侧**（`deepseek mcp serve`）+ SKILL.md + pre/post hooks + `[hooks]` 配置 |
| D08 | partial | E1+E2 | guide `:41` 子命令族；`codewhale-hq/Codewhale@885ef69:integrations/wecom-bridge/src/index.mjs:232` `auto_approve: config.autoApprove` | 审批策略可配（`autoApprove`），`exec --auto` 默认 Full Access —— 与 gopi 的 allow/ask/deny 三态不同，是"是否自动放行"二态 |
| D09 | partial | E1+E2 | guide `:5` "ships sandboxed tool execution on macOS (Seatbelt), Linux (Landlock)"；`:229` "Seatbelt on macOS, and bubblewrap on Linux"；`codewhale-hq/Codewhale@885ef69:crates/engine/src/core/engine.rs:552` | 一次性私有执行环境有内核级实现（Seatbelt/Landlock/bubblewrap）；E2 命中是注释而非创建/回收函数，故仍 `partial` |
| D10 | partial | E2 | `codewhale-hq/Codewhale@885ef69:crates/engine/src/runtime_handoff.rs:1962` `url: "data:image/png;base64,AAAA".to_string()` | 图像以 `image_url` data-URI part 进入消息；guide 未单列该维 |
| D11 | none | E1+E2 | E1 `D11` 探针 0 命中；E2 唯一命中 `telemetry-ingest/package-lock.json:2472` 是测试依赖 `@vitest/browser-playwright`，非产品实现 | 无 capture→act 闭环；浏览器能力不由本仓提供 |
| D12 | partial | E1+E2 | guide `:84-86` `deepseek serve --http` 暴露 `/v1/*` runtime API；`:34` 子命令 `deepseek mcp list`；`codewhale-hq/Codewhale@885ef69:crates/engine/src/mcp/sse.rs:353` `tokio::net::TcpListener::bind` | 有本地 HTTP 服务面（runtime API，供 IDE/Web 嵌入）；E2 命中在 MCP SSE 内部监听，非顶层 serve 实现点 |
| D13 | partial | E2 | `codewhale-hq/Codewhale@885ef69:telemetry-ingest/README.md:23` `$CODEWHALE_HOME/telemetry/dryrun.jsonl` | 结构化 jsonl 痕迹在（telemetry + session 记录）；按轮次读回的命令未见（`RUST_LOG` 只控制日志级别） |
| D14 | partial | E2 | `codewhale-hq/Codewhale@885ef69:telemetry-ingest/src/schema.ts:199` `"input_tokens"`；`telemetry-ingest/README.md:158` `input_tokens`/`output_tokens`/`cache_read_tokens` | 有 token 计量（服务端 usage 字段落库并聚合）；无 `/cost` 类命令 |
| D15 | partial | E2 | `codewhale-hq/Codewhale@885ef69:Cargo.toml:45` `"crates/lsp"`；`telemetry-ingest/src/schema.ts:212` 工具名表含 `"lsp"`, `"memory_get"`, `"memory_search"` | 有独立 LSP crate（符号级定位通道）；全文检索未见专门实现点，故不判更细 |
| D16 | partial | E2 | `codewhale-hq/Codewhale@885ef69:crates/engine/src/core/engine.rs:2308` `codewhale_runtime::native_memory::NativeMemoryStore::from_global_path(`；`crates/engine/src/tools/native_memory.rs:6` | 有原生跨会话记忆库（`NativeMemoryStore` + `memory_search`/`memory_get` 工具）；持久路径为全局而非项目级 |
| D17 | partial | E2 | guide `:79` "the model can spawn child agents via `agent_spawn` and use the full lifecycle family (`agent_wait`, `agent_result`, `agent_cancel`"（原句的完整枚举用 `ssh digital 'curl -sSL https://raw.githubusercontent.com/Hmbown/DeepSeek-TUI/885ef69f5d789c088825ed0d797221c21732c25e/docs/README.md' | sed -n '79p'` 取回）；`codewhale-hq/Codewhale@885ef69:telemetry-ingest/src/schema.ts:227` `"subagent_spawn"` | 有子 agent 派发生命周期族；"跨会话任务队列"本身未见（队列是进程内 fleet/worker 语义） |

## 许可证与开闭源结论

MIT（`LICENSE:1` + `Cargo.toml:65`）。开源；13-crate Rust workspace，无闭源子目录。仓库由 `Hmbown/DeepSeek-TUI` 迁移到 `codewhale-hq/Codewhale`，guide 里的 URL 仍可用（重定向后同一 sha）。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=16 none=1 unknown=0`，`n=17`。
**DeepSeek-TUI 得分 = 16 × 1 = 16**。`none` 的唯一一维是 D11（无 capture→act 闭环）。
