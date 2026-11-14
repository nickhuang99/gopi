<!-- doc-class: historical -->

# Deep Code（lessweb/deepcode-cli@f1a5525）

<!-- meta -->
repo: lessweb/deepcode-cli
sha: f1a552583bff353c9b24103542d02cd6bdcfa1d8
guide: docs/deepcode.md（awesome-deepseek-agent，83 行，抓取 http=200）
url_in_guide: https://github.com/lessweb/deepcode-cli
pinned_sha_verified_at: 2026-11-06T00:14Z（`git ls-remote --symref` → `refs/heads/main f1a552583bff353c9b24103542d02cd6bdcfa1d8`）
license: MIT（`LICENSE:1` "MIT License"；`package.json:6` `"license": "MIT"`）→ 开源
证据通道: E1（guide 行号）+ E2（repo@sha:path:line）。E3 不产出：按框架 §1「对方的 E3 边界」与 Rule 27/35，不为取证安装运行第三方 agent，故本文件上限 `partial`。
证据原件: `~/.gopi/sessions/01a1aa6c-ac74-7a6a-9562-983f0aede1d5/artifacts/stage2/`（`e1-probes.txt`、`e1-matrix.tsv`、`cite-deepcode.txt`、`p2-deepcode.txt`、`e2-deepcode.txt`、`guides/guide-deepcode.md`）
判级口径: 逐级取自 `devdoc/competitor-analysis-framework.md` §2；`partial` = 该维 E1 或 E2 探针有非空命中；`none` = E1 与 E2 探针双零（下表给出 `grep -c` 读数）；本文件无 `unknown`（两条通道都已执行，无三通道静默的维）。

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `dsa-docs/docs/deepcode.md:16` = `npm install -g @vegamo/deepcode-cli` | 单一具名安装命令，需 Node 18+；E2 侧 `grep -n '"bin"' package.json` 在 monorepo 根无命中，入口在 `packages/cli`，故只到 E1 |
| D02 | partial | E1+E2 | guide `:27` `Create ~/.deepcode/settings.json with your DeepSeek API key`；`lessweb/deepcode-cli@f1a5525:packages/vscode-ide-companion/src/extension.ts:522` `path.join(os.homedir(), ".deepcode", "settings.json")` | 首跑要**手写** settings.json，没有 `deepseek auth` 这类向导；E1+E2 一致指向同一份手写文件 |
| D03 | partial | E1+E2 | guide `:33` `"BASE_URL": "https://api.deepseek.com"`；`lessweb/deepcode-cli@f1a5525:packages/vscode-ide-companion/src/extension.ts:31` `const DEFAULT_BASE_URL = "https://api.deepseek.com";` | 供应商可换靠 settings 的 `BASE_URL` 键，代码里默认值可被覆盖 |
| D04 | partial | E1+E2 | guide `:27` 与 `:42-48` 配置项表；`lessweb/deepcode-cli@f1a5525:packages/vscode-ide-companion/src/extension.ts:531` 读取该文件的错误分支 | 配置有单一载体（`~/.deepcode/settings.json`）；**没有**打印生效值的命令（`grep -rIn 'config (show|list|get)'` 0 命中） |
| D05 | partial | E2 | `lessweb/deepcode-cli@f1a5525:packages/cli/src/cli-args.ts:117` `describe: "Run one prompt non-interactively (requires --prompt)"` | headless 入口存在（`--prompt` 非交互一次执行）；guide 未记载该 flag，E1 侧 `D05 hits=0` |
| D06 | partial | E1+E2 | guide `:73` `/resume` 选择历史会话；`lessweb/deepcode-cli@f1a5525:packages/vscode-ide-companion/src/provider.ts:24` `"listSessions"` | 会话可列（`listSessions`）可续（`/resume`）；持久化落 `~/.deepcode/history.jsonl`（`packages/core/src/common/message-history.ts:58`） |
| D07 | partial | E1+E2 | guide `:77-82` Agent Skills 两个发现目录；`lessweb/deepcode-cli@f1a5525:packages/core/src/session.ts:1205` `path.join(root, skillName, "SKILL.md")`；MCP：`packages/core/src/session.ts:400` `mcpServers?: Record<string, McpServerConfig>` | 扩展点两类都齐：skills（目录扫描 SKILL.md）+ MCP client（`initMcpServers`，`extension.ts:501`） |
| D08 | none | E1+E2 | E1 `grep -cE '(approv\|permission\|allowlist\|--yolo\|dangerously\|ask before)' guide-deepcode.md` = `0`；E2 结构化命中 0（`cite-deepcode.txt` 的 `-- D08-perm` 段 `(no hit)`，仅 UI 层透传 `permission` 字段） | 命令审批门在两条通道上都无可指认的实现点；UI 的 `permission` 字段是渲染用回传，不是策略枚举 |
| D09 | none | E1+E2 | E1 `D09` 探针 0 命中；E2 `(Sandbox\|seatbelt\|landlock\|bubblewrap)` → `(no hit)` | 无一次性私有执行环境；bash 工具直接在宿主目录跑（`packages/core/templates/tools/bash.md:7` 自述"terminal operations"） |
| D10 | partial | E1+E2 | guide `:69` `Ctrl+V` 粘贴剪贴板图片；`lessweb/deepcode-cli@f1a5525:packages/core/src/common/openai-message-converter.ts:141` `part.type !== "image_url" || supportsMultimodal(model, multimodal)` | 图像走 `image_url` part；非多模态模型另有 `UnderstandImage` 识图工具（`packages/vscode-ide-companion/README_cn.md:68`） |
| D11 | none | E1+E2 | E1 `D11` 探针 0 命中；E2 唯一命中是**文档内示例** `packages/core/templates/skills/bundled/deepcode-self-refer/references/mcp.md:79`（"playwright" mcp 配置样例），非本仓实现 | catch→act 闭环不存在；playwright 只作为可挂载的第三方 MCP 出现在示例里 |
| D12 | none | E1+E2 | E1 `D12` 探针 0 命中；E2 `(http\.Listen\|ListenAndServe\|createServer\|Fastify)` → `(no hit)` | 无本地服务面；CLI+VSCode 扩展是两种接入形态，都不是对外 HTTP 服务 |
| D13 | partial | E2 | `lessweb/deepcode-cli@f1a5525:packages/core/src/common/message-history.ts:58` `path.join(os.homedir(), ".deepcode", "history.jsonl")`；`packages/core/src/common/session-id.ts:40` 以 `<sessionId>.jsonl` 判存在 | 结构化 jsonl 痕迹**写**在；E2 未见按轮次读回的命令（读回接口只在 VSCode provider 里） |
| D14 | partial | E2 | `lessweb/deepcode-cli@f1a5525:packages/core/src/session.ts:248` `const totalTokens = usage.total_tokens;`；`packages/core/src/common/compaction.ts:50` 自述"An estimate, not a tokenizer" | 用的是服务端回报的 `usage`，不是自建 tokenizer；压缩预算用估算值（代码注释明写） |
| D15 | partial | E2 | `lessweb/deepcode-cli@f1a5525:packages/core/src/prompt.ts:289` `ripgrep: checkToolInstalled("rg")`；`packages/core/templates/tools/bash.md:11` 指示优先用 `rg` | 全文检索有一条实现点（ripgrep 探测）；**无符号级定位**（`grep -rIn 'LSP\|language.server'` 在源码 0 命中） |
| D16 | none | E1+E2 | E1 `D16` 探针 0 命中；E2 `(MEMORY\.md\|MemoryStore\|knowledge_base)` → `(no hit)`；仅 `AGENTS.md` 项目指令文件（`packages/core/src/session.ts:3220-3224`） | `AGENTS.md` 是**项目指令**不是跨会话知识库，不计入本维 |
| D17 | none | E1+E2 | E1 `D17` 探针 0 命中；E2 `(subagent\|spawn_agent\|delegate_task)` → `(no hit)`，仅有 `queueMicrotask`/`queued chat response` 这类测试字样 | 无跨会话任务队列、无多 agent 派发 |

## 许可证与开闭源结论

MIT（`LICENSE:1` + `package.json:6`）。完全开源，可再分发与商用；`packages/vscode-ide-companion` 与 `packages/core`、`packages/cli` 三个子包同属一个 monorepo，无闭源子目录。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数（`grep -c '^| D[0-9][0-9] | <等级> '`）：`full=0 partial=11 none=6 unknown=0`，`n=17`。
**Deep Code 得分 = 11 × 1 = 11**。`none` 的 6 维：D08、D09、D11、D12、D16、D17。
