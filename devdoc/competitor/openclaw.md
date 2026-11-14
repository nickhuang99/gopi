<!-- doc-class: historical -->

# OpenClaw（openclaw/openclaw@9e72de1）

<!-- meta -->
repo: openclaw/openclaw
sha: 9e72de159848a43f7c7b92967fe9fd08f6826dae
guide: docs/openclaw.md（awesome-deepseek-agent，52 行，抓取 http=200）
url_in_guide: https://github.com/openclaw/openclaw
pinned_sha_verified_at: 2026-11-06T00:14Z
license: MIT（`LICENSE:1` "MIT License"；`package.json:29` `"license": "MIT"`）→ 开源
语言/形态: TypeScript monorepo（`package.json:2` `"name": "openclaw"`、`:3` `"version": "2026.11.1"`、`:23` `"description": "Multi-channel AI gateway with extensible messaging integrations"`、`:35` 有 `bin` 块）；`packages/*` + `ui/` + `extensions/`
证据通道: E1 + E2；E3 不产出（框架 §1 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-openclaw.txt`、`p2-openclaw.txt`、`e2-openclaw.txt`、`guides/guide-openclaw.md`、`e1-probes.txt`）
取样提示: 本仓树极大（`e2-openclaw.txt` 的 D01 探针在 `ui/` 单元测试里就有 357 命中），故 E2 采用"排除 `ui/`、`web/`、`desktop/`、`dashboard/`、测试与生成物后再取首个结构化命中"的口径，并在缺失处如实记 `none`，不拿 UI 代码冒充实现。

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | guide `:12` `curl -fsSL https://openclaw.ai/install.sh \| bash`；`:18` Windows `iwr -useb https://openclaw.ai/install.ps1 \| iex` | 两条一行安装（Unix / Windows 脚本），脚本托管在自己域名上；E2 侧 `package.json:35` 有 `bin` 块，入口不止一个 |
| D02 | partial | E1 | guide `:23` `openclaw onboard --install-daemon`；`:26-31` 逐项问答（Setup mode=QuickStart、Model/auth provider=DeepSeek、API key、Default model、其余可 Skip） | **本组最完整的首跑向导**：有专门的 `onboard` verb，还能同时装 daemon；guide 逐条列出五个提问点 |
| D03 | partial | E1+E2 | guide `:28` "Model/auth provider Select **DeepSeek**"；`:32` v2026.4.24 起"proper support for DeepSeek V4 thinking mode"（`reasoning_content` 400 错误的说明） | 多 provider 选择 + **针对 DeepSeek V4 思考模式的专门兼容修复**——这是 8 家里唯一记载了与模型特性耦合的补丁 |
| D04 | partial | E2 | `openclaw@9e72de1:.agents/skills/release-openclaw-plugin-testing/SKILL.md:260` "Read it back through `config get` or JSON." | 有 `config get` 读回通道（本仓把运维流程写成 skills，证据来自运维 skill 的原文）；配置载体是 `packages/gateway-protocol` 定义的类型化配置 |
| D05 | partial | E2 | `packages/acp-core/src/runtime/types.ts:3` `export type AcpRuntimeSessionMode = "persistent" \| "oneshot";`；`packages/gateway-protocol/src/schema/environments.ts:77` `headlessReconnectCommand: Type.Literal("openclaw node restart")` | **`oneshot` 是运行时模式的一等枚举值**（不是开关）；还有 headless 重连命令的协议字段——headless 是协议级概念，比 CLI flag 更彻底 |
| D06 | partial | E2 | `.agents/skills/release-openclaw-ci/SKILL.md:574` "Only `--resume-request <file>` can continue a candidate request after admission"；`packages/gateway-protocol/src/schema/sessions-goal.ts:62` `stringLiteralUnion(["pause", "resume", "complete", "block"])` | 会话有 `resume` 状态机（pause/resume/complete/block 四态）；`--resume-request` 是**一次准入请求的续传**——与会话续跑是两个概念，本维按"会话可续"判 partial |
| D07 | partial | E2 | `openclaw@9e72de1:packages/gateway-protocol/src/schema/plugins.ts:505` `mcpServers: Type.Array(NonEmptyString)`；`schema/agents-models-skills.ts:400` "/** Reads the current SKILL.md for a discovered, agent-scoped local skill. */"；`schema/plugin-skills.ts:11` | 扩展面**全部进了协议**：plugin（含 mcpServers 声明）、agent-scoped skill（SKILL.md）、plugin-skill 两个 schema。这是本组扩展面"契约化"程度最高的一家 |
| D08 | partial | E2 | `packages/gateway-protocol/src/schema/logs-chat.ts:15` `import { SessionPermissionModeSchema, SessionToolOverridesSchema }`；`:416` `expectedPermissionMode: Type.Optional(Type.Union([SessionPermissionModeSchema, Type.Null()]))` | 权限模式是协议枚举，并且**请求可声明期望模式**（`expectedPermissionMode`）——服务端可校验而不是信任；还有 per-tool 覆盖 |
| D09 | partial | E2 | `.agents/skills/update-team-server/controller/README.md:462` "Non-root or unavailable bubblewrap skips"；`extensions/acpx/src/codex-auth-bridge.ts:339` `"use_legacy_landlock"` | bubblewrap + Landlock 两套内核隔离都在（landlock 有 legacy 兼容开关）；"不可用就 skip"说明降级是显式行为 |
| D10 | partial | E2 | `packages/gateway-protocol/src/schema/worker-inference.ts:38` `export const WorkerInferenceImageContentSchema = inferenceSchemas.image;`；`schema/devices.ts:166` `pattern: "^data:image/png;base64,"` | 图像是协议级 content 类型（worker inference 的 image schema）+ data-URI 校验 |
| D11 | partial | E2 | `packages/net-policy/src/redact-sensitive-url.ts:122` `path.endsWith(".cdpUrl")`；`ui/vitest.config.ts:4` `import { playwright } from "@vitest/browser-playwright"`；`.gitignore:23` `ui/playwright-report/` | 有 **CDP URL** 的处理（浏览器调试通道被 net-policy 特判为敏感 URL）→ 浏览器接管存在；但 capture→act 的具体动作链未在取样窗口内取到 → partial 下限，不升级 |
| D12 | partial | E2 | guide `:36` `openclaw dashboard` → Web UI 的 Chat 页；`:42` `openclaw tui`；`:48` `openclaw terminal`；`package.json:23` "Multi-channel AI gateway" | **三种前端形态**（Web UI / TUI / terminal）+ 自身定位就是 gateway；`createServer` 命中都在 `*.test-support.ts`，故不据此升 full |
| D13 | partial | E2 | `packages/gateway-protocol/README.md:159` "chat.*, session file and diff APIs, transcript"；`:162` "`sessionId` is the opaque stored transcript or runtime instance ID"；`schema/talk-event-vocabulary.ts:5-6` `"transcript.delta"`/`"transcript.done"` | transcript 是**协议事件流**（delta/done 两种事件），会话文件与 diff 都是 API——痕迹面按协议定义，不是日志文件 |
| D14 | partial | E2 | `openclaw@9e72de1:packages/gateway-protocol/src/schema/cron.ts:758-760` `input_tokens`/`total_tokens`；`schema/talk-event-vocabulary.ts:36` `"usage.metrics"`；`schema/logs-chat.ts:505` `usage: Type.Optional(Type.Unknown())` | 用量既是事件（`usage.metrics`）也是 cron 任务的记录字段；`Type.Unknown()` 说明 usage 载荷未收窄（推断：形状由上游决定） |
| D15 | partial | E2 | `openclaw@9e72de1:package.json:2483` `"web-tree-sitter": "0.27.1"`、`:2540` `"tree-sitter-bash": "0.25.1"`；`schema/sessions-search.ts:69` "/** Full-text search response; indexing marks a still-running first-use reconcile. */" | **语法树（tree-sitter，含 bash 语法）** + 全文检索（`sessions-search` 有索引与"首次使用调和"语义）——这是"代码理解"路线而非 LSP；两者都算 D15 通道，按"符号/索引级定位"判 partial |
| D16 | partial | E2 | `packages/memory-host-sdk/src/host/config-utils.ts:126` `export const MEMORY_HOST_ROOT_FILENAME = "MEMORY.md";`；`schema/agents-files.ts:12` "MEMORY.md before anything is written" | 有**独立的 memory host SDK**（`packages/memory-host-sdk/`）+ MEMORY.md 作为根文件；记忆是宿主级能力而非 agent 内嵌 |
| D17 | partial | E2 | `packages/gateway-protocol/src/session-result-validators.ts:3` `import { NativeSubagentsSnapshotSchema } from "./schema/native-subagents.js";`；`session-url-contract/src/session-key.ts:7` 正则含 `subagent` 段 | **原生子 agent 有快照 schema**（可被校验/回放），会话 key 里 subagent 是独立地址段——多 agent 是协议一等公民 |

## 许可证与开闭源结论

MIT（`LICENSE:1` + `package.json:29`）。开源；monorepo 含 `packages/`（协议/网关/运行时/记忆宿主）、`ui/`、`extensions/`（含 `acpx`），无闭源子目录。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=17 none=0 unknown=0`，`n=17`。
**OpenClaw 得分 = 17 × 1 = 17**（本组 17 维无一是 `none` 的两家之一）。
