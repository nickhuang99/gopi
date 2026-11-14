<!-- doc-class: historical -->

# Reasonix（esengine/DeepSeek-Reasonix@8a4c879）

<!-- meta -->
repo: esengine/DeepSeek-Reasonix
sha: 8a4c8791843139a73ef4f6bf709f49b1127ad9a3
default_branch: `studio`（`git ls-remote --symref` 实测，非 `main`）
guide: docs/reasonix.md（awesome-deepseek-agent，27 行，抓取 http=200）
url_in_guide: https://github.com/esengine/DeepSeek-Reasonix
pinned_sha_verified_at: 2026-11-06T00:14Z
license: MIT（`LICENSE:1` "MIT License"）→ 开源
语言/形态: Go module `reasonix`（`go.mod:1`，go 1.26.0），TUI（bubbletea v2）
证据通道: E1 + E2；E3 不产出（框架 §1 对方 E3 边界 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-reasonix.txt`、`p2-reasonix.txt`、`e2-reasonix.txt`、`guides/guide-reasonix.md`、`e1-probes.txt`）

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `docs/reasonix.md:16` `npx reasonix code`；`:23` "No global install required" | 零安装（npx 直跑），无包管理器分支；guide 明说"不需要全局安装" |
| D02 | partial | E1 | `docs/reasonix.md:14` "The first run of Reasonix prompts for it via a built-in wizard and persists it to `~/.reasonix/config.json` — no environment variable needed" | 有首跑向导**且明确不要环境变量**——8 家里 D02 叙述最明确的一份，但只到 E1 |
| D03 | partial | E1+E2 | guide `:5` "talks to `api.deepseek.com` without a translation shim"；`esengine/DeepSeek-Reasonix@8a4c879:internal/contract/config/deepseek_presets.go`（preset 表） | 供应商预设存在；但设计上明确"直连 DeepSeek、不要翻译层"，与 OpenAI 兼容别的供应商不是同一路线 |
| D04 | partial | E2 | `esengine/DeepSeek-Reasonix@8a4c879:internal/contract/config/bringforward.go:3` "bringConfigForward applies every in-memory upgrade a loaded config gets"；承载文件 `~/.reasonix/config.json`（guide `:14`）、`config.toml`（`internal/safety/sandbox/sandbox.go:152` 提示串） | 配置有版本前滚机制（配置迁移是一等公民）；打印生效值未见专门 verb |
| D05 | partial | E2 | `esengine/DeepSeek-Reasonix@8a4c879:internal/contract/config/paths.go:431` "Used by `reasonix --continue` / `--resume` to find the recent ones" | `--continue`/`--resume` 是脚本化入口；独立的一次性 `--print` 模式未见（`D05-headless` 首命中是 shell 参数守卫代码，非本能力） |
| D06 | partial | E2 | `internal/contract/config/paths.go:431`；`internal/contract/provider/provider.go:249` "// reasonix --resume." | 可续（`--continue`/`--resume` 选最近会话）；可列的 verb 未见 |
| D07 | partial | E2 | `internal/contract/config/migrate.go:29` `MCPServers map[string]legacyMCPServer \`json:"mcpServers"\``；`internal/contract/config/config.go:777` "each a directory of SKILL.md / `<name>.md` playbooks — scanned between"；`internal/ext/skill/builtins.go:99` | MCP（含旧 `--mcp` 格式的迁移）+ SKILL.md 技能目录，两类扩展点齐 |
| D08 | partial | E2 | `internal/contract/config/edit.go:470` "SetPermissionMode sets the writer-fallback mode. Accepts "ask", "allow", or"；`internal/contract/event/notice_codes.go:13` `NoticeCodePlanAutoApproved = "plan_auto_approved"` | 三态策略枚举（`ask`、`allow` 与注释未列全的第三值——第三值用 `ssh digital 'curl -sSL https://raw.githubusercontent.com/esengine/DeepSeek-Reasonix/8a4c8791843139a73ef4f6bf709f49b1127ad9a3/internal/contract/config/edit.go' | sed -n '470,480p'` 取回）+ 计划自动放行事件——与 gopi 的 allow/ask/deny 形状最接近的一家 |
| D09 | partial | E2 | `internal/safety/sandbox/seatbelt_other.go:97` "When spec.Mode is "enforce" and bubblewrap (bwrap) is available on PATH"；`internal/safety/sandbox/sandbox.go:152` "or set [sandbox] bash = "off" in config.toml" | 内核级隔离（bubblewrap/seatbelt）且**可关**，有三态 `spec.Mode` |
| D10 | partial | E2 | `internal/contract/config/deepseek_presets.go:39` "Images ride input_image parts here, measured 2026-09-13 against the endpoint"；`internal/contract/config/provider_entry.go:98` "openai image_url detail hint: low|high" | 图像走 `input_image`/`image_url`，且注释带实测日期（作者真跑过） |
| D11 | none | E1+E2 | E1 `D11` 探针 0 命中；E2 唯一命中 `internal/runtime/usecap/mcp_concurrency.go:26-27` `"playwright"`, `"puppeteer"` —— 该文件是**MCP 服务器名的并发上限表**，不是浏览器实现 | 无 capture→act 闭环；playwright 只作为可挂载 MCP 的名字出现在并发表里 |
| D12 | partial | E2 | `internal/frontend/serve/listen.go:19` `return http.ListenAndServe(addr, s.Handler())` | 有本地 serve 面（`internal/frontend/serve`）；guide 未记载 |
| D13 | partial | E2 | `internal/safety/evidence/evidence.go:431` "WithSessionMessages attaches a lazy transcript accessor so verifyStepEvidence"；`:436` "ordinary tool calls never pay for a full transcript copy" | 有**证据/审计**一等公民（`internal/safety/evidence`），且为性能做了惰性 transcript；按轮次读回的 CLI verb 未见 |
| D14 | partial | E2 | `internal/safety/typesafe/client.go:45` `InputTokens int \`json:"input_tokens"\``；`internal/contract/provider/usage.go:12` "standard prompt_tokens_details.cached_tokens — the openai provider normalises" | 缓存命中单独计量（guide `:5` 自称 "cache-first loop"），与服务端 usage 对齐；无 `/cost` |
| D15 | none | E1+E2 | E1 `D15` 探针 0 命中；E2 `internal/safety/sandbox/prepare.go:25` "builds argv for a raw argument vector (e.g. ripgrep)"、`seatbelt_other.go:118` "like ripgrep that don't need a shell wrapper" | 只有 ripgrep 通道，**无符号级定位**（`grep -rIn 'LspClient\|language.server'` 在源码 0 命中） |
| D16 | partial | E1+E2 | guide `:5` "automatic tool-call repair"；`esengine/DeepSeek-Reasonix@8a4c879:REASONIX.md:220` "instead saves a fallible background fact (frontmatter file + `MEMORY.md`"（该行后半句用 `ssh digital 'curl -sSL https://raw.githubusercontent.com/esengine/DeepSeek-Reasonix/8a4c8791843139a73ef4f6bf709f49b1127ad9a3/REASONIX.md' | sed -n '220p'` 取回） | 有跨会话事实库（frontmatter 文件 + MEMORY.md）；注意 `internal/runtime/taskmonitor/memory.go:15` 的 `InMemoryStore` 是**进程内**监控，与本维无关（同音陷阱，已排除） |
| D17 | partial | E2 | `internal/safety/evidence/effect_watch.go:54` `case r.ToolName == "spawn_agents":`；`internal/safety/evidence/outcome.go:149` `case r.Success && (r.ToolName == "spawn_agents")` | 有 `spawn_agents` 子 agent 工具，且**它的副作用被证据层单独判定**（parse 级而非文档级）；跨会话队列未见 |

## 许可证与开闭源结论

MIT（`LICENSE:1`）。开源，Go 单模块；默认分支名是 `studio`（不是 `main`），`benchmarks/` 下有 13 个基准（含 `memorybench`、`swebench`）是"作者实测能力"的旁证，但基准存在不等于能力实现，未据此升级任何维度。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=15 none=2 unknown=0`，`n=17`。
**Reasonix 得分 = 15 × 1 = 15**。`none` 的 2 维：D11（无浏览器接管）、D15（只有 ripgrep，无符号级定位）。
