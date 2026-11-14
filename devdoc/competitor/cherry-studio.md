<!-- doc-class: historical -->
# Cherry Studio（CherryHQ/cherry-studio@2292fdd）

<!-- meta -->
repo: CherryHQ/cherry-studio
sha: 2292fdd
sha_full: 2292fdd739266df12518e4f706de1aa99f66015f
sha_commit_time: 2026-11-05T15:40:16Z
sha_subject: `perf(agent-session-message): project only token columns from context rows (#23642)`
default_branch: main
guide: cherry_studio.md（dsa-docs 只读快照，56 行）
url_in_guide: https://github.com/CherryHQ/cherry-studio 、 https://cherry-ai.com
license: AGPL-3.0（`https://api.github.com/repos/CherryHQ/cherry-studio` 的 `license.spdx_id` 原值；仓库内 `LICENSE` 首行为 `GNU AFFERO GENERAL PUBLIC LICENSE`，第 2 行 `Version 3, 19 November 2007`）
open_source: 开源（AGPL-3.0）
version_on_pin: `CherryStudio` 2.1.10（`cherry-studio@2292fdd:package.json:3` 原文 `"version": "2.1.10",`）
pinned_sha_verified_at: 2026-11-06T00:07:00Z（`https://api.github.com/repos/CherryHQ/cherry-studio/commits/main` 的 `sha` 字段原值）
channels: E1 = dsa-docs guide 全文 grep -n + 官方 README（同一 sha）；E2 = 该 sha 的 `git/trees?recursive=1`（`truncated=false`，12,638 blob 路径）与 10 个 raw 原文文件。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial
evidence_inline: 见下方「证据重放」节——命令原文与原始输出已搬进本文件正文，读者不需要打开任何外部路径即可复核（采集于 2026-11-06T00:07:00Z；本轮重放于 2026-11-07）
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc/code 探针正则，唯一真源）

## 证据重放（自足：命令原文 + 原始输出）

本节是上方 `channels` 与下方各 D 维引用的**唯一生成过程**。采集日的中间产物已随批次回放删除，所以这里放的是**能重建它们的命令**加**本轮重放的真实输出**，逐字抄录。

### (1) 树清单 —— E2 的「12,638 blob 路径」「`truncated=false`」

```bash
curl -sS 'https://api.github.com/repos/CherryHQ/cherry-studio/git/trees/2292fdd739266df12518e4f706de1aa99f66015f?recursive=1' -o tree.json
python3 -c 'import json; d=json.load(open("tree.json")); print("sha=%s" % d["sha"]); print("blobs=%d entries=%d truncated=%s" % (sum(1 for e in d["tree"] if e["type"]=="blob"), len(d["tree"]), d["truncated"]))'
```

采集日落盘的 `tree.json` 上跑第二行的输出（本轮读取该文件所得，原样）：

```
sha=2292fdd739266df12518e4f706de1aa99f66015f
blobs=12638 entries=14591 truncated=False
```

⇒ `blobs=12638` 就是 `channels` 行的「12,638 blob 路径」，`truncated=False` 就是「`truncated=false`」；`sha` 字段与首行 `sha_full` 逐字相同。

**本轮重放的诚实结果**：再次 `curl` 这棵一万四千条的大树时，GitHub 在 100 秒内没有返回完整响应（`curl` 退出码 124，落盘 3,493,888 字节的截断 JSON），这是网络限流而不是数据问题。所以「12638」本轮**不是**靠重新抓取确认的，而是靠下面 (2) 的逐字节派生证明的。

### (2) 路径清单由树清单派生 —— `paths.txt` 不是人工产物

```bash
python3 -c 'import json; d=json.load(open("tree.json")); open("/tmp/derive.txt","w").write("\n".join(e["path"] for e in d["tree"] if e["type"]=="blob")+"\n")'
wc -l < /tmp/derive.txt
diff -q /tmp/derive.txt paths.txt && echo "diff: IDENTICAL"
```

输出（原样，三行）：

```
12638
diff: IDENTICAL
```

⇒ 派生结果 12,638 行与采集日落盘的那一份 `diff` 为空。**这就是 (1) 里「12,638」的独立复核**：它不是抄来的数字，是任何持有该 `tree.json` 的人都能重算出来的。

### (3) 逐维探针计数 —— 各 D 维「N 处命中」的出处

探针正则的真源是 `devdoc/competitor-analysis-framework.md` §3。把它按 `Dxx<TAB>regex` 一行一维存成 `probe-regex.tsv` 后重放：

```bash
while IFS=$'\t' read -r dim re; do printf '%s\t%s\n' "$dim" "$(grep -icE -e "$re" paths.txt)"; done < probe-regex.tsv
```

输出（原样，17 行）：

```
D01	0
D02	49
D03	28
D04	21
D05	0
D06	0
D07	1128
D08	67
D09	17
D10	420
D11	164
D12	137
D13	146
D14	18
D15	26
D16	411
D17	821
```

⇒ 下文用到的三个数字逐一对应：D01「0 命中」= 第一行；D05「0 命中」= 第五行；D09「17 个 `sandbox`/`isolat` 命中」= 第九行。与采集日的 census 文件逐行 `diff` 结果为空。

### (4) E2 原文 —— 各 D 维引用的「原文」是怎么取的

每个文件一次 `GET`，按固定 sha 取 raw 正文，再 `grep -n` 定位：

```bash
curl -sS -o raw/README.md 'https://raw.githubusercontent.com/CherryHQ/cherry-studio/2292fdd739266df12518e4f706de1aa99f66015f/README.md'
grep -n 'Keep conversation history and knowledge bases' raw/README.md
```

输出：

```
103:- 💾 Keep conversation history and knowledge bases on your device, with local and WebDAV backup options.
```

⇒ 这就是 D13 格引用的「`README.md:103` 原文」，行号 `103` 由这条 `grep -n` 直接给出。同一形状适用于本文件所有 `cherry-studio@2292fdd:<path>:<line>` 引用：`curl` 该 `<path>` 的 raw 正文，`grep -n` 该原文，行号即 `grep -n` 的行首数字。

### (5) 本轮复核记下的一处出入（不改判定）

**(a) D06 第二个路径本轮取不到正文。** D06 格并列引用 `subagentSession.ts:2` 与 `packages/agent-runtime-pi/src/session.ts`（无行号）。复核时 `raw/` 目录里**只有前者的落盘副本**，后者的抓取记录 `http` 码是空的（既非 200 也非 404）：

```bash
ls raw/ | grep 'agent-runtime-pi'
grep -n 'agent-runtime-pi/src/session.ts' e2-repo.txt
```

```
packages_agent-runtime-pi_src_subagentSession.ts
51:=== packages/agent-runtime-pi/src/session.ts (http ) ===
```

⇒ 「会话管理器符号存在」这句话的依据**只在 `subagentSession.ts:2`**（`import { type SessionEntry, SessionManager } from '@earendil-works/pi-coding-agent'`，本轮 `grep -n` 复现）。D06 格自己已经写了「持久化写入点未单文件取证」，与此一致；这里把**第二个路径没有正文**这件事说明白，免得下游以为它有。

### (6) E1 —— guide 的 56 行

`dsa-docs` 是只读快照，不在本仓库内；本文件对它的每一处引用（右下表 `dsa-docs/cherry_studio.md:NN`）都已把**该行的原文整句抄进备注**，所以复核不需要打开快照。快照本身的规模声明（56 行）由 `wc -l` 得到：

```bash
wc -l < cherry_studio.md
```

输出：

```
56
```

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | none | E1+E2 | — | 看过、没有：guide 56 行与官方 README 的安装段探针零命中（`e1-guide.txt` D01 记 NOHIT；README 第 39 行原文 `**[Download Desktop](https://cherryai.com/download)** · **[Getting Started](https://cherryai.com/docs/en/getting-started/quick-start/)**` 是下载页而非具名安装命令）；E2 全树 12,638 路径 D01 探针 0 命中（无 `package.json` 的 `bin` 发布字段：`package.json:2` 原文 `"name": "CherryStudio",`，第 8 行 `"main": "./out/main/main.js",`） |
| D02 | partial | E1+E2 | `dsa-docs/cherry_studio.md:25` + `cherry-studio@2292fdd:.env.example:2` | E1 原文「`2. Paste your [DeepSeek API Key](https://platform.deepseek.com/api_keys) into the **API Key** field. Leave **API Host** as the default `https://api.deepseek.com`.`」；E2 原文「`API_KEY="sk-xxx"`」 |
| D03 | partial | E1+E2 | `dsa-docs/cherry_studio.md:25` + `cherry-studio@2292fdd:src/main/ai/runtime/aiSdk/retry/resolveApiKeyFallbacks.ts:5` | E1 原文（同上一格）API Host 可改；E2 原文「`export function resolveApiKeyFallbacks(`。自定义供应商/端点能力在文档与代码两侧一致 |
| D04 | partial | E2 | `cherry-studio@2292fdd:package.json:18` + `cherry-studio@2292fdd:.env.example:1` | E1 零命中；E2 原文「`"build": "pnpm run typecheck && dotenv -v CHERRY_EDITION=global -- electron-vite build && pnpm run build:utility-process",`、`.env.example:1` 原文 `NODE_OPTIONS=--max-old-space-size=8000`。配置经 `.env` 与 `dotenv -v` 注入构建/运行（这是**构建期**载体；该产品运行时配置存在 sqlite 应用数据目录里，本轮未取到该路径的源码证据）。无「打印生效值」的命令证据，E3 不可用 |
| D05 | unknown | — | — | 语料无此话题：guide 56 行 `--print\|--json\|headless\|non-interactive` 零命中；E2 全树路径 D05 探针 0 命中。桌面 GUI 产品的非交互执行面在两条静态通道上都没有出现 |
| D06 | partial | E2 | `cherry-studio@2292fdd:packages/agent-runtime-pi/src/subagentSession.ts:2` + `cherry-studio@2292fdd:packages/agent-runtime-pi/src/session.ts` | E1 零命中；E2 原文「`import { type SessionEntry, SessionManager } from '@earendil-works/pi-coding-agent'`」。会话管理器符号存在（持久化写入点未单文件取证），E2 单独出现上限 partial |
| D07 | partial | E1+E2 | `dsa-docs/cherry_studio.md:55` + `cherry-studio@2292fdd:src/main/ai/mcp/createMcpBridgeServer.ts:1` | E1 原文「`- **MCP Servers.** Add servers under **Settings → MCP**; any DeepSeek-driven conversation can then call their tools.`」；E2 原文「`import { type Progress, type ReadResourceResult, Server, type Tool } from '@modelcontextprotocol/server'`」 |
| D08 | partial | E1+E2 | `dsa-docs/cherry_studio.md:35` + `cherry-studio@2292fdd:src/main/ai/toolApproval/builtinToolPolicy.ts:1` | E1 原文「`Open the **Agents** tab from the top navigation, click **+ Agents**, give your agent a name, set **Model** to **`deepseek-v4-pro`** (or **`deepseek-v4-flash`**), pick a permission mode, and click **Add**.`」；E2 原文「`import type { AgentPermissionMode } from '@shared/data/api/schemas/agents'`；另有 `packages/agent-behavior/src/core/approval/approval.ts:7-8` 原文「`/** What the gate does with one call (D34): run it (perhaps with rewritten arguments), ask a person, or block it. */`」 |
| D09 | unknown | — | — | 语料无此话题：guide 零命中；E2 全树 17 个 `sandbox\|isolat` 命中经查看均为「session isolation」类笔记与 UI 容器组件，无 sandbox 创建/回收符号（`e2-path-census.txt` D09 首 3 条为 `.agents/notes/implemented/bug-fix/2026-10-22-notes-session-isolation.md`） |
| D10 | partial | E1+E2 | `dsa-docs/cherry_studio.md:38` + `cherry-studio@2292fdd:packages/file-preview/src/plugins/image/imageFilePreviewPlugin.ts:3` | E1 原文「`<img src="./assets/cherry_studio_add_agent.png" width="720" border="1" />`」；E2 原文「`export const imageFilePreviewPlugin = {`。图像处理插件与 `node-screenshots` 依赖（`package.json:168` 原文 `"node-screenshots": "^0.2.8",`）在固定 sha 上 |
| D11 | partial | E1+E2 | `cherry-studio@2292fdd:README.md:74` + `cherry-studio@2292fdd:package.json:113` | E1（官方 README，同一 sha）原文「`- 🛠️ Work with files, run commands, and use browser and computer automation tools.`」；E2 原文「`"test:e2e": "pnpm playwright test",`。屏幕驱动闭环的两条腿（浏览器自动化 + 计算机操作）宣称与工具链均在 |
| D12 | none | E1+E2 | — | 看过、没有：guide 56 行 `serve\|--port\|localhost:\|HTTP (server\|API)` 零命中；E2 全树 12,638 路径无 HTTP 服务启动点（`server.ts` 命中的 `src/main/ai/mcp/servers/filesystem/server.ts` 是 stdio 侧 MCP server，原文首行 `import { type Progress, type ReadResourceResult, Server, type Tool } from '@modelcontextprotocol/server'`，不是对外 HTTP 服务面） |
| D13 | partial | E1+E2 | `cherry-studio@2292fdd:README.md:103` + `cherry-studio@2292fdd:packages/agent-behavior/src/core/goal/goalHistory.ts:7` | E1 原文「`- 💾 Keep conversation history and knowledge bases on your device, with local and WebDAV backup options.`」；E2 原文「`  GoalCleared,`（`goalHistory.ts` 的导入块，目标历史的结构化记录类型）。读取命令无证据 |
| D14 | partial | E2 | `cherry-studio@2292fdd:src/main/ai/utils/billingCost.ts:5` + `cherry-studio@2292fdd:src/main/ai/hooks/billingHook.ts:1` | E1 零命中；E2 原文「`const finiteNonnegativeCost = z.number().nonnegative().refine(Number.isFinite)`（第 7-9 行 `ProviderCostWithCurrencySchema` / `cost: finiteNonnegativeCost,`）；`billingHook.ts` 原文「`import type { LanguageModelV3StreamPart, LanguageModelV3Usage } from '@ai-sdk/provider'`」 |
| D15 | partial | E2 | `cherry-studio@2292fdd:src/main/ai/mcp/servers/filesystem/tools/grep.ts:24` | E1 零命中；E2 原文「`export const grepToolDefinition = {`（第 14 行 `export const GrepToolSchema = z.object({`，第 11 行 `import { logger, MAX_GREP_MATCHES, MAX_LINE_LENGTH, runRipgrep, validatePath } from '../types'`）。全文检索腿有实现；符号定义（LSP）腿无命中 |
| D16 | partial | E1+E2 | `dsa-docs/cherry_studio.md:53` + `cherry-studio@2292fdd:src/main/ai/agents/agentMemoryFileLock.ts:19` | E1 原文「`- **Knowledge Bases.** Drop PDFs, Markdown, or Office files into the **Knowledge Base** tab to RAG over them with DeepSeek V4.`」；E2 原文「`export async function withAgentMemoryFileLock<T>(filePath: string, task: () => Promise<T>): Promise<T> {`（同文件第 4-6 行注释声明「runtime memory tool 与 IPC 编辑器两个写者共用这把锁」） |
| D17 | partial | E2 | `cherry-studio@2292fdd:packages/agent-runtime-pi/src/subagentSession.ts:4` + `cherry-studio@2292fdd:package.json:91` | E1 零命中；E2 原文「`import type { Subagent } from './subagent'`（第 5 行 `import { SUBAGENT_ORIGIN_STATE } from './subagentRun'`）；`package.json:91` 原文含 `@cherrystudio/agent-runtime` / `agent-behavior` / `agent-runtime-pi` 多包构建。多 agent 派发的代码结构在，任务队列行级证据无 |
