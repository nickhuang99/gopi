<!-- doc-class: historical -->

# Langcli（LangcliTeam/langcli@9c79f06）

<!-- meta -->
repo: LangcliTeam/langcli
sha: 9c79f062b8c49b9b9874595665b41b3a0dfd06fb
guide: docs/langcli.md（awesome-deepseek-agent，43 行，抓取 http=200）
url_in_guide: https://github.com/LangcliTeam/langcli
pinned_sha_verified_at: 2026-11-06T00:14Z
license: **无顶层 LICENSE / COPYING 文件**，`package.json` 无 `license` 字段 —— 源码公开可见但**未授予任何许可**（法律上默认保留所有权利）；`package.json:2` `"name": "langcli-com"`、`:3` `"version": "0.1.53"`、`:34` `"bin": {"langcli": "dist/cli.js"}`
证据通道: E1 + E2；E3 不产出（框架 §1 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-langcli.txt`、`p2-langcli.txt`、`e2-langcli.txt`、`guides/guide-langcli.md`、`e1-probes.txt`）

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | guide `:12` `bash -c "$(curl -fsSL https://assets.langcli.com/installation/install-langcli.sh)"`、`:17` Windows `cmd /c "curl -fsSL -o %TEMP%\install-langcli.bat https://assets.langcli.com/installation/install-langcli.bat && %TEMP%\install-langcli.bat"`、`:28` `npm i -g langcli-com`；`package.json:34` `"bin": {"langcli": "dist/cli.js"}` | 三条安装路径（脚本 / Windows bat / npm）+ 已编译 `dist/` 入口；脚本托管在自己的域名而不是 GitHub raw |
| D02 | partial | E1 | guide `:33` "Go to [LangRouter](https://langrouter.ai/), register an account, save your API key"；`:22` "restart your terminal after installation to ensure environment variables take effect" | 密钥来自**自己的网关 LangRouter**，不是 DeepSeek 控制台 —— 本组 8 家里唯一一家；认证靠环境变量，无首跑向导 |
| D03 | partial | E2 | `langcli@9c79f06:packages/@ant/model-provider/src/providers/openai/modelMapping.ts:37` "Strip custom: prefix (user-defined models from settings.json modelProviders)"；`providers/gemini/modelMapping.ts:9` 同款 | 多供应商适配层（`openai`、`gemini` 两套 modelMapping + 自定义 modelProviders）存在；guide 只提 LangRouter，两者并不矛盾（网关在上游） |
| D04 | partial | E2 | 同上 `modelMapping.ts:37`（settings.json 的 `modelProviders` 键）；`src/utils/doctorDiagnostic.ts:135` `npm config get prefix` | 配置载体是 `settings.json`；有 `doctor` 诊断命令 |
| D05 | partial | E2 | `packages/@ant/model-provider/src/hooks/types.ts:43` "/** Check if non-interactive session */"；`packages/@ant/computer-use-mcp/src/types.ts:396` "a future headless mode" | 代码里区分交互/非交互会话；`computer-use` 一侧自述 headless 还是"future"——**能力不齐**，故只到 partial |
| D06 | partial | E2 | `langcli@9c79f06:src/screens/REPL.tsx:2629` "This handles CLI flag resume (--resume-session) and ResumeConversation screen"；`packages/@ant/model-provider/src/errorUtils.ts:224` "deserialized from JSONL (e.g. --resume)" | 有 `--resume-session` + 会话选择界面；会话以 JSONL 持久（同时是 D13 的证据） |
| D07 | partial | E2 | `src/screens/REPL.tsx:1109` `const mcpClients = useMergedClients(initialMcpClients, mcp.clients)`；`src/commands/init.ts:171` "Create each skill at `.claude/skills/<skill-name>/SKILL.md`" | MCP client + SKILL.md 技能；技能目录是 **`.claude/skills/`** —— 与 Claude Code 布局一致，属同族派生（见文末"备注"） |
| D08 | partial | E2 | `packages/@ant/claude-for-chrome-mcp/src/toolCalls.ts:6` `PermissionMode,`；`:19` `if (name === "set_permission_mode")` | 有 `PermissionMode` 且可**运行中切换**（`set_permission_mode` 是一个工具调用）；策略值枚举未在本次取样窗口内取到 |
| D09 | partial | E2 | `src/utils/sandbox/sandbox-adapter.ts:587` "install missing tools (e.g. apt install bubblewrap socat) or run /sandbox for details"；`:598` "Only return warnings on Linux/WSL (bubblewrap doesn't support globs)" | bubblewrap 隔离 + `/sandbox` 诊断；按平台降级并**返回警告**（不静默） |
| D10 | partial | E2 | `packages/@ant/model-provider/src/shared/openaiConvertMessages.ts:84` `\| { type: 'image_url'; image_url: { url: string } }`；`:89` | 图像走 `image_url` part |
| D11 | partial | E2 | `packages/@ant/computer-use-mcp/src/toolCalls.ts:2208` "Region-crop upscaled screenshot. Coord invariant (computer_use_v2.py:1092)"；`openaiConvertTools.ts:17` "skip server tools like computer_use" | **有 computer-use（截图+坐标动作）的独立 MCP 包**，坐标不变量都写进了注释；这是本组 8 家里 computer-use 证据最强的一家 |
| D12 | none | E1+E2 | E1 `D12` 探针 0 命中；E2 唯一命中 `src/services/mcp/xaaIdpLogin.ts:19` `import { createServer, type Server } from 'http'` 与 `:320` `server = createServer((req, res) => {` —— 那是 **OAuth 回调监听**，不是对外服务面 | 无本地 HTTP/Web 服务面 |
| D13 | partial | E2 | `packages/@ant/model-provider/src/errorUtils.ts:224` "Guard: when deserialized from JSONL (e.g. --resume), the error object may"；`computer-use-mcp/src/toolCalls.ts:1664` "Caller decides what to return for the transcript" | 会话 JSONL 化（结构化痕迹在）；按轮次读回的 verb 未取到（`/ n N Esc` 是 transcript 模态的键位，`REPL.tsx:16`） |
| D14 | partial | E2 | `packages/@ant/model-provider/src/providers/gemini/streamAdapter.ts:42` `input_tokens: inputTokens,`；`:44` `cache_creation_input_tokens: 0,` | 多供应商的 usage 归一（含缓存字段，gemini 侧显式置 0）；无 `/cost` |
| D15 | partial | E2 | `package.json:186` `"vscode-languageserver-protocol": "^3.17.5"`；`:187` `"vscode-languageserver-types"` | 依赖里有 LSP 协议栈（客户端能力的前提）；协议在依赖中，未在本次窗口内取到客户端实现点 |
| D16 | partial | E2 | `src/services/extractMemories/extractMemories.ts:463` "Index file updates are mechanical — the agent touches MEMORY.md to add"；`src/screens/REPL.tsx:5085` "stripped frontmatter, MEMORY.md truncation" | 有**独立的记忆抽取服务**（`services/extractMemories/`）+ MEMORY.md 索引；记忆面在 8 家里做得最"产品化"的一档 |
| D17 | partial | E2 | `src/screens/REPL.tsx:1959` "Updated by both streaming deltas and subagent message content."；`:1961` "denominator correctly includes subagent processing time." | 有子 agent，且计时分母把子 agent 处理时间算进去（细节实现点）；派发 verb 未取到 |

## 许可证与开闭源结论

**许可未知，按"未授权"处理。** 实测：`ls -1 | grep -iE '^licen[cs]e|^copying'` 在仓库根返回 `(no top-level license file)`，`package.json` 也无 `license` 字段（`cite-langcli.txt` 的 `## LICENSE` 段）。这与"开源"是两件事：源码可见 ≠ 授予使用权。`SECURITY.md` 存在说明有安全响应流程，但不构成许可。**引用其代码前必须先拿到许可**；本文件的结论仅限功能性描述，不作可用性推荐。

## 备注：与 Claude Code 的布局同族

`src/commands/init.ts:171` 的技能目录写作 `.claude/skills/<name>/SKILL.md`，包作用域是 `packages/@ant/*`（含 `claude-for-chrome-mcp`）。这是**推断**（依据是路径与包名，不是作者声明）：Langcli 派生自 Anthropic Claude Code 的代码布局。推断不影响上表任何等级——每一格都按实测命中判。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=16 none=1 unknown=0`，`n=17`。
**Langcli 得分 = 16 × 1 = 16**。`none` 的唯一一维是 D12（无本地服务面；`createServer` 只在 OAuth 回调里）。
**许可这条不计分但要单列**：得分 16 与 DeepSeek-TUI / Hermes / nanobot 并列，但它的许可状态是"未授权"，因此**不可作为可借用对象**——分数与可用性是两回事。
