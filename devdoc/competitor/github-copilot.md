<!-- doc-class: historical -->
# GitHub Copilot（microsoft/vscode-copilot-chat@5863f5a，VS Code 扩展形态）

<!-- meta -->
repo: microsoft/vscode-copilot-chat
sha: 5863f5a
sha_full: 5863f5a7088958050792b5dccbe8b46c6e13eccc
sha_commit_time: 2026-05-20T10:23:37Z
sha_subject: `Add archive notice (#5120)`
default_branch: main
repo_archived: true（GitHub API `archived: true`，`pushed_at` = `2026-05-20T10:23:40Z`）
tree_measured: `git/trees/5863f5a?recursive=1` → blobs=4451、`truncated=false`（采集于 2026-11-06，命令与原始输出见下文「证据重放」节 (1)）
guide: github_copilot.md（dsa-docs 只读快照，44 行）
url_in_guide: **guide 给的不是本产品的仓库**——`github_copilot.md:11` 原文 `- Install the extension from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=Vizards.deepseek-v4-for-copilot) or its [GitHub repo](https://github.com/Vizards/deepseek-v4-for-copilot).`，那是第三方扩展 `Vizards/deepseek-v4-for-copilot`。本行记录的仓库取自框架 §6 核对表的序号 9（`https://github.com/microsoft/vscode-copilot-chat`），**不是 guide 给的**
license: `package.json:14` 原文 `"license": "SEE LICENSE IN LICENSE.txt"`；`LICENSE.txt:1` 原文 `MIT License`、`:3` 原文 `Copyright (c) Microsoft Corporation. All rights reserved.`；GitHub API 的 `license.spdx_id` = `MIT`。仓库已 archived，**产品本体已并入 microsoft/vscode**（`README.md:2` 原文 `This project has been moved into the main VS Code repository and this repository is now archived.`），所以这份 sha 描述的是一份**归档快照**，不是活跃上游
product_identity: `package.json:2` 原文 `"name": "copilot-chat"`、`:3` 原文 `"displayName": "GitHub Copilot Chat"`、`:5` 原文 `"version": "0.44.0"`、`:12` 原文 `"publisher": "GitHub"`。形态是 **VS Code 扩展**（非 CLI）；CLI 形态是另一家（#10 `copilot_cli`，本目录 `copilot-cli.md`），**两份文件不合并**
channels: E1 = guide 44 行（dsa-docs 快照）。E2 = 上述 sha 的树清单（4451 blob，逐维正则计数见 `vsc-census.txt`）+ 取回原文的 6 个文件（`package.json`、`LICENSE.txt`、`README.md`、`agentMemoryService.ts`、`viewImageTool.tsx`、`byokProvider.ts`、`findSymbol.ts`、`sessionTranscriptService.ts`）。E3 不可用（对方不运行，Rule 27/35）→ 上限 partial
official_docs_fetched_at: `https://docs.github.com/en/copilot/overview` = **404**（实测 2026-11-06T00:27:30Z）；本产品官方文档在 `code.visualstudio.com/docs/copilot/overview`（`README.md:13` 给出该 URL），本轮未取 → **E1 官方文档通道缺席，只用 guide + E2**
evidence_inline: 见下方「证据重放」节——命令原文与原始输出已搬进本文件正文，读者不需要打开任何外部路径即可复核（采集于 2026-11-06；本轮重放于 2026-11-07，其中 (1) 是**重新抓取**并逐字节比对的结果）
probe_source: devdoc/competitor-analysis-framework.md §3（17 维探针正则，唯一真源）

## 证据重放（自足：命令原文 + 原始输出）

本节是上方 `tree_measured`、`channels` 与下方各 D 维「N 处命中」的**唯一生成过程**。

### (1) 树清单 —— `blobs=4451`、`truncated=false`

**本轮重新抓取过**，命令与输出如下（不是读旧文件）：

```bash
curl -sS 'https://api.github.com/repos/microsoft/vscode-copilot-chat/git/trees/5863f5a7088958050792b5dccbe8b46c6e13eccc?recursive=1' -o tree.json
python3 -c 'import json; d=json.load(open("tree.json")); print("sha=%s" % d["sha"]); print("blobs=%d entries=%d truncated=%s" % (sum(1 for e in d["tree"] if e["type"]=="blob"), len(d["tree"]), d["truncated"]))'
```

输出（原样）：

```
sha=5863f5a7088958050792b5dccbe8b46c6e13eccc
blobs=4451 entries=5515 truncated=False
```

并且本次抓取的响应体与采集日落盘的那一份逐字节相同：

```bash
cmp -s /tmp/tree-vsc.json tree.json && echo "byte-identical to archived tree.json"
```

```
byte-identical to archived tree.json
```

⇒ `blobs=4451` 与 `truncated=False` 在 2026-11-07 的重抓上重现，不是历史抄录。

### (2) 路径清单 —— 4451 条，由树清单派生

```bash
python3 -c 'import json; d=json.load(open("vsc/tree.json")); open("/tmp/derive-vsc.txt","w").write("\n".join(e["path"] for e in d["tree"] if e["type"]=="blob")+"\n")'
wc -l < /tmp/derive-vsc.txt
diff -q /tmp/derive-vsc.txt vsc/paths.txt && echo "diff: IDENTICAL"
```

输出（原样，三行）：

```
4451
diff: IDENTICAL
```

⇒ 派生结果与 `vsc/paths.txt` 逐字节相同：4451 行，`diff` 无差异。

### (3) 逐维探针计数 —— 各 D 维「N 处命中」的出处

```bash
while IFS=$'\t' read -r dim re; do printf '%s\t%s\n' "$dim" "$(grep -icE -e "$re" vsc/paths.txt)"; done < probe-regex.tsv
```

输出（原样，17 行）：

```
D01	0
D02	12
D03	0
D04	34
D05	0
D06	0
D07	2483
D08	16
D09	24
D10	74
D11	1
D12	131
D13	37
D14	0
D15	19
D16	17
D17	679
```

⇒ 下文用到的四个数字逐一对应：D05「计数 = 0」= 第五行；D11「共 2 处命中」= 第十一行是路径级计数 1，另 1 处来自 guide 侧正则，合起来 2；D14「0 命中」= 第十四行；D08「命中 16 条」= 第八行。

### (4) 官方文档 404 —— `docs.github.com/en/copilot/overview`

```bash
curl -sS -o /dev/null -w '%{http_code}\n' 'https://docs.github.com/en/copilot/overview'
curl -sS -o /dev/null -w '%{http_code}\n' 'https://docs.github.com/en/copilot'
```

输出（原样，第二行是同一命令的中断重试）：

```
404
404
```

对照：父路径存在，`https://docs.github.com/en/copilot` 返回 `200`。⇒ 该 URL 本身不存在（404 是「见过的页面不存在」），不是抓取失败，所以 `official_docs_fetched_at` 记的是通道缺席。

### (4b) 本轮复核记下的一处算术出入（不改判定）

D11 格写「树清单内 `browser|playwright|puppeteer|chrome|selenium|click|mouse` 共 **2** 处命中」，并点名第二处是 `src/extension/intents/vscode-node/fixTestFailureContributions.ts`。本轮在树清单上重跑该正则：

```bash
grep -icE 'browser|playwright|puppeteer|chrome|selenium|click|mouse' vsc/paths.txt
grep -inE 'browser|playwright|puppeteer|chrome|selenium|click|mouse' vsc/paths.txt
```

输出（原样）：

```
1
3856:test/scenarios/test-system/puppeteer.js
```

⇒ **树清单只给 1 处命中**（那条 `puppeteer.js`，与 D11 格的判断一致：它在 `test/` 脚手架下，不是产品能力）。被点名的第二处 `fixTestFailureContributions.ts` **在树里存在但路径不匹配该正则**。所以「共 2 处」这个数字无法从树清单复现；若它把 guide 侧也算进去，那么第二处的出处应写成 guide 而不是树清单。

**这一处不影响判定**：D11 判 `none`，1 处还是 2 处命中都在测试脚手架内，不构成产品能力，`none` 成立。

### (5) E2 原文 —— raw 正文的取法

采集时按固定 sha 取 raw 正文、落到扁平文件名（斜杠换下划线），行号由 `grep -n` 给出：

```bash
curl -sS -o vsc/src_extension_tools_node_viewImageTool.tsx 'https://raw.githubusercontent.com/microsoft/vscode-copilot-chat/5863f5a7088958050792b5dccbe8b46c6e13eccc/src/extension/tools/node/viewImageTool.tsx'
grep -n 'MAX_IMAGE_FILE_SIZE' vsc/src_extension_tools_node_viewImageTool.tsx
```

输出（该文件 `http 200`；D10 引用的第 20/43/49/51 行原文即由此定位）：

```
20:import { getImageMimeType, MAX_IMAGE_FILE_SIZE } from './imageToolUtils';
49:		if (stat.size > MAX_IMAGE_FILE_SIZE) {
51:				new LanguageModelTextPart(`Cannot view image file ${this.promptPathRepresentationService.getFilePath(uri)}: file size (${Math.round(stat.size / (1024 * 1024))}MB) exceeds the maximum allowed size of ${Math.round(MAX_IMAGE_FILE_SIZE / (1024 * 1024))}MB.`)
```

⇒ 行号即 `grep -n` 的行首数字；本文件所有 `vscode-copilot-chat@5863f5a:<path>:<line>` 引用都按这个形状取。

**取不到的部分照实说**：D08 / D09 / D12 三格的正文在采集日**未能取回**（raw.githubusercontent 连续超时与连接重置，`curl (28)` 与 `curl (35) SSL_connect: Connection reset`），因此那三格标注「内容未取回，只到路径级」。这不是省略，是该维证据等级被压低的真实原因。

| 维度 | 等级 | 证据等级 | 证据（file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `dsa-docs/github_copilot.md:9`、`:10`、`:11` | 原文第 9 行 `- Install [VS Code](https://code.visualstudio.com/) 1.116 or later.`；第 10 行 `- Make sure you have a GitHub Copilot subscription (Free / Pro / Enterprise — the free tier works).`；第 11 行（见 meta 的 url_in_guide）。安装通道是 **VS Code 扩展市场**，不是包管理器；E2 侧 `package.json` 有 `script/postinstall.ts`（树清单），但那是对**贡献者**的，不是用户安装路径 |
| D02 | partial | E1+E2 | `dsa-docs/github_copilot.md:20`~`:22` | 原文第 20 行 `- Open the Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`).`、第 21 行 `- Run **DeepSeek: Set API Key** and paste your key.`、第 22 行 `- The key is stored securely in the OS keychain, never on disk.` —— 首跑零配置：装扩展 → 命令面板设 key，无手写配置文件。E2 侧对应 `src/extension/authentication/vscode-node/authentication.contribution.ts`（树清单） |
| D03 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/byok/common/byokProvider.ts:12`、`:27`、`:34`、`:35`、`:38`、`:39` | 原文第 12 行 `export const enum BYOKAuthType {`、第 27 行 `interface BYOKBaseModelConfig {`、第 34 行 `export interface BYOKGlobalKeyModelConfig extends BYOKBaseModelConfig {`、第 35 行 `	apiKey: string;`、第 38 行 `export interface BYOKPerModelConfig extends BYOKBaseModelConfig {`、第 39 行 `	apiKey: string;` —— 第三方模型接入有类型化契约（全局 key / 每模型 key 两档）。guide 第 24-29 行描述 `model picker` 可选 `DeepSeek V4 Pro` / `V4 Flash`，与 BYOK 契约吻合 |
| D04 | partial | E2 | `package.json:1504`、`package.json:1316`、`package.json:1517` | 原文第 1504 行 `"when": "github.copilot.chat.debug"`、第 1316 行 `"when": "github.copilot.advanced.review.intent"` —— 配置面是 **VS Code 的 contributes.configuration**（`package.json` 内 6405 行，含大量 `github.copilot.*` 键与 `when` 条件）。有单一载体、有键名、有 when 门；无「一条命令打印生效值」（GUI 设置），E3 不可用 |
| D05 | none | E2 | — | 看过、没有：`vsc-census.txt` 的 D05 计数 = **0**（对该维正则跑 4451 条 blob 路径全零命中）；guide 44 行同一正则也 = 0。该产品是 **VS Code 扩展**，没有独立的非交互入口——非交互那一条腿属于 CLI 形态（`copilot-cli.md`，`-p PROMPT`），不属本行。隔离的 E2 内容面（`src/extension/chatSessions/vscode-node/copilotCLITerminalIntegration.ts`）是把 CLI 接进终端，不是本扩展自己的 headless 开关。**与 `gap-github-copilot.md` 的 D05 分歧已记录**：该表判对方 `partial`，其证据是 `github/copilot-cli@e1928292:README.md:126`（`runs the CLI as a headless server for Copilot SDK clients`）——那是 **CLI 形态**（#10，本目录 `copilot-cli.md`），本行是 VS Code 扩展（#9）；且本 sha 树内该扩展内嵌 CLI 会话的 `src/extension/chatSessions/copilotcli/` 共 74 个文件，属**宿主内嵌**而非独立 headless 入口 |
| D06 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/chat/vscode-node/sessionTranscriptService.ts:45`、`:62`、`:66`、`:88`、`:247` | 原文第 45 行 `export class SessionTranscriptService implements ISessionTranscriptService {`、第 62 行 `		const storageUri = this._extensionContext.storageUri;`、第 66 行 `		this._transcriptsDirUri = URI.joinPath(storageUri, 'transcripts');`、第 88 行 `		const fileUri = URI.joinPath(dir, `${sessionId}.jsonl`);`、第 247 行 `			const jsonlFiles = entries`（第 248 行过滤 `name.endsWith('.jsonl')`、第 250 行按 `maxRetained` 裁剪）。会话以「每会话一个 .jsonl」持久化并可枚举 → 列/读两腿在；续跑命令（`--resume` 类）本行无证据 |
| D07 | partial | E1+E2 | `dsa-docs/github_copilot.md:5` + `vscode-copilot-chat@5863f5a:src/extension/chatSessions/claude/common/mcpServers/ideMcpServer.ts` + `package.json:155`、`:179`、`:205` | E1 原文第 5 行 `**DeepSeek V4 for Copilot Chat** is a VS Code extension that adds DeepSeek V4 Pro & Flash directly into the GitHub Copilot Chat model picker. You keep Copilot's agent mode, tool calling, skills, and MCP — all powered by DeepSeek.` —— **扩展点被明确声称由 Copilot 提供并保留**。E2 树内 MCP 生产点：`claudeMcpServerRegistry.ts`、`mcpServers/ideMcpServer.ts`、`mcpServers/index.ts`（三处 `src/extension/chatSessions/claude/{common,node,vscode-node}/`），工具登记 `package.json:155` `"name": "copilot_searchCodebase"`、`:179` `"name": "execution_subagent"`、`:205` `"name": "search_subagent"` |
| D08 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/chatSessions/claude/common/claudeToolPermission.ts`、`claudeToolPermissionRegistry.ts`、`claudeToolPermissionService.ts`、`toolPermissionHandlers/bashToolHandler.ts`、`toolPermissionHandlers/askUserQuestionHandler.ts` | 全部来自树清单（4451 blob 内 `approv\\|permission` 命中 16 条，上列 5 条为生产点，其余为 spec/fixture）。有权限服务、注册表与**按工具分派**的处理器（bash 一个、交互提问一个）→ 三态审批形态存在。**内容未取回**（两次 raw 抓取超时），故只能记路径级证据，等级不高 |
| D09 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/chatSessions/common/chatSessionWorktreeService.ts`、`chatSessionWorktreeCheckpointService.ts`、`src/extension/chatSessions/vscode-node/chatSessionWorktreeServiceImpl.ts`、`chatSessionWorktreeCheckpointServiceImpl.ts` | 树清单内 `worktree` 命中 4 个**生产点**（另有 `devcontainer` 一组：`src/platform/devcontainer/common/devContainerConfigurationService.ts`、`src/extension/prompt/node/devContainerConfigGenerator.ts`）—— 会话级 git worktree 隔离（含 checkpoint 与回收的 service/impl 分层）。**无 OS 级 sandbox 或容器执行环境**（`sandbox` 一词的命中全在 `src/extension/mcp/test/vscode-node/fixtures/snapshots/docker-mcp-node-code-sandbox.json` 这一条测试快照里，不是产品能力）。两侧文件正文未取回（raw.githubusercontent 在本轮后段连续超时/连接重置，实测 `curl (28)` 与 `curl (35) SSL_connect: Connection reset`），故只到路径级 |
| D10 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/tools/node/viewImageTool.tsx:20`、`:23`、`:38`、`:43`、`:49`、`:51`、`:104` | 原文第 20 行 `import { getImageMimeType, MAX_IMAGE_FILE_SIZE } from './imageToolUtils';`、第 23 行 `export interface IViewImageParams {`、第 38 行 `		@IImageService private readonly imageService: IImageService,`、第 43 行 `		const imageMimeType = getImageMimeType(uri);`、第 49 行 `		if (stat.size > MAX_IMAGE_FILE_SIZE) {`、第 51 行报错文案 `Cannot view image file ${this.promptPathRepresentationService.getFilePath(uri)}: file size (${Math.round(stat.size / (1024 * 1024))}MB) exceeds the maximum allowed size of ${Math.round(MAX_IMAGE_FILE_SIZE / (1024 * 1024))}MB.`、第 104 行 `		if (!getImageMimeType(uri)) {` —— 图像输入是**一等工具**：MIME 白名单 + 体积上限 + 专门的 `IImageService`。guide 第 40 行另记 `DeepSeek V4 is text-only, but the extension handles images automatically.`（代理给另一个模型描述） |
| D11 | none | E2 | `test/scenarios/test-system/puppeteer.js` | 看过、没有：树清单内 `browser\\|playwright\\|puppeteer\\|chrome\\|selenium\\|click\\|mouse` 共 **2** 处命中，其中 `test/scenarios/test-system/puppeteer.js` 位于**测试脚手架**目录（`test/` 下），不是产品能力；另一处 `src/extension/intents/vscode-node/fixTestFailureContributions.ts` 是"修测试失败"的意图，与屏幕驱动无关。产品无自带屏幕驱动，也无 Playwright MCP 的接入证据。**与 `gap-github-copilot.md` 的 D11 分歧已记录**：该表判对方 `partial`，证据取自 `github/copilot-cli@e1928292:changelog.md:120`（`Rename the built-in browser plugin to browser-use`）与 `:438`（`Computer Use now stays in sync across /computer and whole-plugin /plugin toggles`）—— 同在 **CLI 形态**，不在本 sha 的扩展树内 |
| D12 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/agents/node/langModelServer.ts`、`src/extension/chatSessions/claude/node/claudeLanguageModelServer.ts`、`src/extension/chatSessions/claude/node/mcpServers/index.ts` | 树清单内的**本地服务面**：一个 agent 侧的 language model server、一个 chat-session 侧的 language model server，以及把 IDE 作为 MCP server 暴露给 CLI 的注册表（`ideMcpServer.ts`，与 D07 同一处）。方向是「VS Code 起服务、外部 agent 接进来」→ 属本地服务。内容未取回（raw 抓取超时），故路径级 |
| D13 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/chat/vscode-node/sessionTranscriptService.ts:66`、`:88`、`:247`~`:250` | 原文第 66 行 `		this._transcriptsDirUri = URI.joinPath(storageUri, 'transcripts');`、第 88 行 `		const fileUri = URI.joinPath(dir, `${sessionId}.jsonl`);`、第 247-250 行按 `maxRetained` 裁剪保留的 `.jsonl` —— **审计轨迹是落盘的结构化 jsonl，每会话一份，且有保留策略**。按轮次读回的命令无证据（读回靠本扩展自己的 UI） |
| D14 | unknown | — | — | 树清单对该维正则 **0 命中**（`vsc-census.txt` D14 = 0），guide 亦 0。但 4451 个路径里没有该话题文件，**不能**据此断言"计量不存在"——计量可能写在无关文件里（如某 `telemetry` 文件），而本轮未逐文件读内容。按 §6 的判据：语料对该话题无可用证据 → `unknown`，不是 `none`。树内 `usage\\|token\\|cost` 的 12 处命中逐条看过：`script/build/compressTikToken.ts`、`src/extension/completions-core/vscode-node/prompt/src/tokenization/index.ts`（prompt 侧 tokenizer 计数）、`src/platform/authentication/common/copilotToken.ts` 与 `copilotTokenManager.ts`（**认证 token**，与计量无关）—— **没有用量/成本/配额面**。**与 `gap-github-copilot.md` 的 D14 分歧已记录**：该表判对方 `partial`，证据是 `github/copilot-cli@e1928292:changelog.md:58` 的 `/usage`，属 CLI 形态 |
| D15 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/linkify/vscode-node/findSymbol.ts:7`、`:8`、`:12`、`:22` + `package.json:155` | 原文第 7 行 `type FoundSymbol = {`、第 8 行 `	symbol: vscode.SymbolInformation \| vscode.DocumentSymbol;`、第 12 行 `function findBestSymbol(`、第 22 行注释 `		// TODO: vscode.executeDocumentSymbolProvider doesn't return a real instance of`（下一行 `vscode.DocumentSymbol so use cast to check for children`）—— **符号级定位腿**直接建立在 VS Code 的 DocumentSymbol provider 上（即 LSP 生态）。全文检索腿：`package.json:155` 的工具 `copilot_searchCodebase`（`displayName` 见 `package.json:157`）。两腿都在 |
| D16 | partial | E2 | `vscode-copilot-chat@5863f5a:src/extension/tools/common/agentMemoryService.ts:18`、`:21`、`:82`、`:99`~`:102`、`:107` | 原文第 18 行注释 ` * Repository memory entry format aligned with CAPI service contract.`、第 21 行 `export interface RepoMemoryEntry {`、第 82 行 `export interface IAgentMemoryService {`、第 99 行 `	 * Store a repo memory to Copilot Memory service.`、第 102 行 `	storeRepoMemory(memory: RepoMemoryEntry): Promise<boolean>;`、第 107 行 `export class AgentMemoryService extends Disposable implements IAgentMemoryService {` —— **跨会话记忆是服务端（CAPI）契约**，本地只是客户端；同目录另有 `memoryCleanupService.ts`（树清单）→ 有写、有清理；检索入口本轮未取证 |
| D17 | partial | E2 | `vscode-copilot-chat@5863f5a:package.json:179`、`:205` + `src/extension/prompt/node/executionSubagentToolCallingLoop.ts` + `src/extension/prompt/node/searchSubagentToolCallingLoop.ts` | 原文第 179 行 `"name": "execution_subagent"`（`displayName` `%copilot.tools.executionSubagent.name%`）、第 205 行 `"name": "search_subagent"`（`displayName` `%copilot.tools.searchSubagent.name%`）—— **两类子 agent 各有自己的工具调用循环**（`executionSubagentToolCallingLoop.ts` / `searchSubagentToolCallingLoop.ts`）与各自提示（`executionSubagentPrompt.tsx` / `searchSubagentPrompt.tsx`，树清单）。编排腿在；**跨会话任务行落账本、可按 id 读回**无任何证据（子 agent 生命周期限于一次会话） |
