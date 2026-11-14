<!-- doc-class: historical -->
# cherry-studio 对 gopi 的差距对比表（17 维 × D01–D17）

<!-- meta -->
repo: CherryHQ/cherry-studio
sha: 2292fdd
sha_full: 2292fdd739266df12518e4f706de1aa99f66015f
sha_commit_time: 2026-11-05 23:40:16 +0800
guide: cherry_studio.md（本地只读副本 /home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cherry_studio.md，56 行，HTTP 200 抓取于 2026-11-05）
url_in_guide: https://github.com/CherryHQ/cherry-studio（guide 第 7 行）
pinned_sha_verified_at: 2026-11-05T19:07:00Z（git rev-parse HEAD 在 ./artifacts/cherry/repo 读出上面那个全 sha）
stage2_capture: 本家阶段 1/2 无采集文件（`ls devdoc/competitor/` 22 个文件中无 cherry-studio.md），本文件 §补采集 是它的替代采集
gopivs_binary: gopi v0.4.0，revision 4f829a274f92a4e94f63a25e61c2310960dd5847

**证据目录（全部不进 git 树）**：`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/`
下面每格的 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/<子目录>/<文件>` 都相对于这个根。

## 补采集（阶段 2 缺口的替代，不做成 devdoc/competitor/cherry-studio.md）

| 通道 | 命令（可原样粘贴重跑） | 读数 | 原始输出 |
|---|---|---|---|
| E1 | `LC_ALL=C.UTF-8 grep -niE -e '<§3 doc 探针>' /home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cherry_studio.md` | 17 维命中数 D01..D17 = 0,1,1,0,0,0,2,1,0,4,4,2,0,0,0,2,0 | `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e1-guide-hits.txt`、复核 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e1-guide-hits-recheck.txt` |
| E2 | `cd /home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/repo && git grep -nE -e '<§3 code 探针>' -- '<范围>'` | 命中文件数 D01..D17 = 0, 309, 21, 16, 42, 111, 54, **7**, 38, 19, 4, 45, 116, 22, **0**, 258, 102（D08 单独一轮、D15 假阳性复核，见下三条纠正） | `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e2-repo-hits.txt`（16 支探针，**不含 D08**）、`e2-recheck4.txt`（D08）、`recheck5.txt`（D15 假阳性 + D08 枚举原文）、`e2-evidence.txt`、`e2-recheck.txt` / `e2-recheck2.txt` / `e2-recheck3.txt` |
| E3（我方） | 见每行证据列的命令 | **11** 份命令输出，逐条带 `exit=N`（`ls /home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-*.txt | wc -l` → `11`） | `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-*.txt` |

**E2 的三处纠正，写出来免得被当成"看过没有"，也免得把一个假阳性当成命中**：
① D08 的权限模式探针在 `src/shared` 只数到 1 个文件，但真正的枚举在 `CherryHQ/cherry-studio@2292fdd:src/shared/ai/agentRuntimeCapabilities.ts:48-54`（五档：`default` / `plan` / `acceptEdits` / `auto` / `bypassPermissions`）——所以 D08 记 `partial` 而不是 `none`。
② **`e2-repo-hits.txt` 里 D15 那行 `count=1` 是假阳性，必须看原文才知道。** 那个 1 来自 `\bLSP\b` 命中的 `CherryHQ/cherry-studio@2292fdd:src/main/utils/htmlToMarkdown.ts:4` `const MAX_COLSPAN = 1000` —— `COLSPAN` 里含子串 `LSP`。逐分支带词边界复测（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/recheck5.txt`）：`language server` files=0、`\bLSP\b` files=0、`codebase index` files=0、`symbolIndex` files=0；E1 亦为 0（`e1-guide-hits.txt` D15 `hits=0`）⇒ 双零，D15 记 `none`。**如果不读原文、只读 `count=1`，这一维会被误记成 `partial`，四张表的判定就全错了。**
③ **D08 不在 `e2-repo-hits.txt` 的 17 支探针里**（那一轮只跑了 16 支），它是 `e2-recheck4.txt` 单独一轮的读数：`git grep -lE 'permissionMode' -- src` 命中 **7** 个文件，枚举本体 `CherryHQ/cherry-studio@2292fdd:src/shared/ai/agentRuntimeCapabilities.ts:48-54` 的五档原文（`const ALL_PERMISSION_MODES = [` / `'default'` / `'plan'` / `'acceptEdits'` / `'auto'` / `'bypassPermissions'` / `] as const satisfies readonly AgentPermissionMode[]`）也在这份 `recheck5.txt` 里逐行贴着。所以上面 D08 那格 `7` 是有据的，不是补出来的。

**E1 里有两处命中不是该维的能力声明，逐条说明（否则会被当成证据）**：D10 的 4 处命中全是 `<img src="./assets/*.png">` 截图标签；D11 的 4 处命中全是 `click` 这个动词（点设置图标/点 Add）。这两维对方改由 E2 支撑。

## 形态（先读这一段）

形态是**一张 17 行主表 + 一节六桶视图**，不是四张各 17 行。历史形态把整表抄进四个桶、合计 68 行，根因是四桶词表**不是全函数**：「我方 `full` / 对方 `partial`」在四桶里没有落点，作者只能复制整表，于是互斥失效。本文件把四个视角的证据合并进主表同一格（原文逐字保留），判定列由 `gapDocVerdictFor(我方,对方)` 机械算出，分桶只在 `## 桶视图` 一节声明一次。
## 主表（D01..D17，一行一维；证据格合并四个视角，原文逐字保留）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号）|
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 本维归属。我方：`ls package.json pyproject.toml setup.py install.sh` 四个 `No such file or directory`（exit=2）、`grep -c '^install:' Makefile` → `0`（exit=1）、6 份 `-h` 对八类安装词的普查 → `0`（exit=1），三读数在 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D01-D09.txt`；对方：`CherryHQ/cherry-studio@2292fdd:electron-builder.yml:168` linux 段 + `CherryHQ/cherry-studio@2292fdd:package.json:23` `"build:linux": "dotenv pnpm run build && electron-builder --linux --x64 --arm64"`，另有 guide `cherry_studio.md:12` 下载安装包（E1） |
| D02 | full | partial | 我强敌弱 | 本维归属。我方：`gopi config show` exit=0，首行 `gopi configuration in force` 并打出全键值（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D01-D04-D06-D13.txt`）；对方：E1 `cherry_studio.md:25` 粘 API Key + E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/provider.ts:56` `apiKey: z.url().optional()`。**同档不同形**：对方是 GUI 向导，我方是命令+配置文件 |
| D03 | full | partial | 我强敌弱 | 本维归属。我方：`gopi config show` 打印 `base_url "https://api.deepseek.com/anthropic"`，且本 run 以该 base_url 起 `gopi chat --once` exit=0 打出 `CHERRY-OK`（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D17-D05.txt`）；对方：E1 `cherry_studio.md:25` `API Host` 可改 + E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/preference/preferenceTypes.ts:310` `apiHost?: string` |
| D04 | full | partial | 我强敌弱 | 本维归属。我方：`gopi config show` exit=0（键与值两列，含 `settings.json` 路径与字节数）；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/preference/preferenceSchemas.ts:651` + `CherryHQ/cherry-studio@2292fdd:src/shared/data/preference/preferenceTypes.ts:11` 单一配置载体，E1 的 `settings.json|config.(json|toml|yaml)` 探针 0 命中（`e1-guide-hits.txt` D04 `hits=0`） |
| D05 | full | partial | 我强敌弱 | 本维归属。我方：`gopi chat --once -p 'reply with exactly: CHERRY-OK'` exit=0 打印 `CHERRY-OK`（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D17-D05.txt`）；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/AgentSessionRuntimeService.ts:173` `headless?: boolean` + `CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/AgentSessionRuntimeService.ts:209` `currentTurn: 'none' | 'interactive' | 'headless'`，E1 探针 0 命中 ⇒ 有 headless 回合形态、没有 CLI 一次性入口（`git grep -cE 'process\.argv' -- src/main` 只 5 个文件，全是 URL 协议处理与测试 fixture） |
| D06 | full | partial | 我强敌弱 | 本维归属。我方：`gopi session list --limit 3` exit=0 打出 3 条 + `[hidden] 157` + `[limit] 2416 more` + `[scanned] 1664 of 3918`；`gopi session log --last` 打会话头/Model/Workdir；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/api/schemas/messages.ts:148` 与 `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/message.ts:540` 的 `topicId: z.string()` 持久载体（非测试文件），E1 探针 0 命中 ⇒ 有持久会话、无 `session list/resume` 命令行 |
| D07 | partial | partial | 平手 | 本维归属。我方：6 份 `-h` 对 `mcp` `grep -ci` = `0`（exit=1），而 `ls /home/user/.gopi/plugins/websearch/scripts/` 有 8 个脚本（`arxiv_search.py`、`bing_search.py`、`deepseek_ask.py`、`github_search.py`、`google_search.py`、`merge_results.py`、`web_fetch.py`、`wiki_search.py`）⇒ 有扩展实现点、无扩展点声明，按框架 §8 上限 `partial`；对方：E1 `cherry_studio.md:5` 与 `cherry_studio.md:55`（Settings → MCP）+ E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/mcpServer.ts:40` `McpServerSchema` 与 `src/main/ai/mcp/servers/`（`ls | grep -v __tests__ | grep -c ".ts$"` = 20 个 server 源文件，另有 `filesystem/` 子目录）。**这是最接近"敌强我弱"的一维，但等级口径显示不出来**：对方是标准的 MCP 客户端+内置 server，我方是自建插件树 |
| D08 | full | partial | 我强敌弱 | 本维归属。我方：`gopi chat --once --tools --policy=forbidden -p 'use bash to run exactly: echo POLICY-PROBE-A'` → 工具结果 `Refused by the CHAT WRITE BOUNDARY (\`echo POLICY-PROBE-A\`): the startup posture is \`forbidden\``；同一条命令 `--policy=allow` → `exit=0` 并输出 `POLICY-PROBE-A`。两次输出 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D08-forbidden-tools.txt` 与 `e3-D08-allow-tools.txt`；对方：E1 `cherry_studio.md:35` 「pick a permission mode」+ E2 `CherryHQ/cherry-studio@2292fdd:src/shared/ai/agentRuntimeCapabilities.ts:48-54` 五档枚举 `default / plan / acceptEdits / auto / bypassPermissions` |
| D09 | full | none | 我有敌无 | 本维归属。我方：`gopi tool sandbox create --label probe --task-id 01a1acc7` → `/home/user/workspace/gopi-sandbox/sandbox-probe-858a210e`（exit=0），其 `.gopi/` 内 `settings.json` 0444（1455 字节）、`memory.db` 0600（0 字节）、`sessions/ raw/ logs/` 0700——私有 profile 真的落盘；对方：`CherryHQ/cherry-studio@2292fdd:docs/dev/references/utility-process/README.md:11` 自述「a crash and native-library isolation boundary, **not a security sandbox**: the child is a full Node context with no capability or OS-level restriction, so V1 must not run untrusted code」，`CherryHQ/cherry-studio@2292fdd:src/main/ai/tools/codeMode/runtime.ts:51` 同句。全部读数在 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D01-D09.txt` |
| D10 | full | partial | 我强敌弱 | 本维归属。我方：`vision_analyze` 对 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/cap1.png` 真跑，输入 1022 tokens / 输出 953 tokens，答出「终端窗口覆盖在浏览器上，最显眼的是 Connection failed 弹窗」；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/main/ai/AiService.ts:958` `{ type: 'image' as const, image: part.data, mediaType: part.mimeType }`（`src/shared/ai` + `src/main/ai` 上该族探针命中 78 个文件）。**E1 的 4 处命中不算证据**：全是 `.png` 截图标签（见 §补采集末段） |
| D11 | full | none | 我有敌无 | 本维归属。我方：`gopi tool capture --screen` 两次 `sha256_16` 由 `5c3e7c9726791c85` 经 `gopi tool act move 400 300` 变为 `1d83331e632aacf9`，三次调用均 exit=0；对方：`CherryHQ/cherry-studio@2292fdd` 的 `playwright` 只出现在 `CherryHQ/cherry-studio@2292fdd:package.json:113` 的 `"test:e2e"` 与 `CherryHQ/cherry-studio@2292fdd:package.json:281` 的 `@playwright/test` devDependency（`grep -lE 'playwright' -- src` 命中 1 个文件且是 `CherryHQ/cherry-studio@2292fdd:src/renderer/components/chat/messages/tools/ToolHeader.tsx:343` 里匹配工具名的正则字面量），唯一的输入注入 `webContents.sendInputEvent` 在 `CherryHQ/cherry-studio@2292fdd:src/main/core/window/quirks.ts:64`，上下文是窗口焦点恢复（`CherryHQ/cherry-studio@2292fdd:src/main/core/window/quirks.ts:52` 注释 `focus-down (begin guard) → native hide/close → sendInputEvent → 50ms restore`），**没有屏幕捕获实现点** ⇒ E1+E2 双零口径记 `none`，原始命中 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e2-recheck.txt`，我方读数 `/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D08-D11.txt` |
| D12 | full | partial | 我强敌弱 | 本维归属。我方：`gopi web --addr 127.0.0.1:7791` 起服务后 `curl` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D12-web.txt`；同一份文件里还留了一次错旗标读数 `gopi web --port 7791` → `unknown subcommand "--port"`，说明正确旗标是 `--addr`）；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/main/ai/mcp/oauth/callback.ts:26` `http.createServer` + `CherryHQ/cherry-studio@2292fdd:src/main/ai/mcp/oauth/callback.ts:104` `server.listen(port, '127.0.0.1', () => {`、`CherryHQ/cherry-studio@2292fdd:src/main/ai/observability/adapters/claudeCode/ClaudeCodeTraceBridgeService.ts:135` `server.listen(0, '127.0.0.1', () => {`（非测试文件 10 个）⇒ 有本地 HTTP 端点，没有面向用户的服务面 |
| D13 | full | partial | 我强敌弱 | 本维归属。我方：`gopi session log --last` exit=0，打出 `Session / Model / Started / Workdir` 头与 `TASK:` 段；`gopi session stats --cost` 的逐项真值见 `devdoc/competitor/gopi-self-a.md` 的 D07 行；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/AgentSessionRuntimeService.ts:1361` runtime transcripts + `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/aiUsageRecord.ts:28` `AiUsageCostBreakdownSchema` 与 `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/aiUsageRecord.ts:19` `AiUsageRecordKindSchema` 的 usage 记录 schema |
| D14 | full | partial | 我强敌弱 | 本维归属。我方：`gopi tool tokens --file devdoc/competitor-analysis-framework.md` → `9833 tokens (31149 bytes, 424 lines, deepseek-v4.1-bpe)`，exit=0（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D05-D07-D09-D14-D15-D16.txt`）；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/shared/data/types/aiUsageRecord.ts:178` `export function getAiUsageRecordTotalTokens(record: AiUsageRecordTokenFields): number | null` |
| D15 | full | none | 我有敌无 | 本维归属。我方：`gopi tool code-search ResolveSandboxRoot` → `ResolveSandboxRoot  internal/config/sandbox.go:228  func`（exit=0）；对方：E1 探针 0 命中（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e1-guide-hits.txt` 的 D15 `hits=0`）+ E2 `git grep -cE -e 'language server|\bLSP\b|codebase index|symbolIndex' -- src` 命中文件数 `0`（`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/cherry/e2-evidence.txt` 末行 `D15_count=0`）⇒ 双零 |
| D16 | full | partial | 我强敌弱 | 本维归属。我方：`gopi memory search clone-channel --limit 2` → 命中 `#18403`，其 `sources=` 段写出源任务行 `01a17e04-c358-7460-a5e6-00e972f7085e`、`ground /home/user/workspace/01a17e04-c358-7460-a5e6-00e972f7085e`；对方：E2 `CherryHQ/cherry-studio@2292fdd:src/shared/ai/claudecode/toolRegistry.ts:39` `mcpServer?: 'cherry-tools' | 'agent-memory' | 'skills'` + `CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/agentSessionRuntimeState.ts:29` 的 knowledgeBase 实现 ⇒ 有知识库与 agent-memory 面，与任务账本不同源 |
| D17 | full | partial | 我强敌弱 | 本维归属。我方：`gopi task submit '<summary>' --tags fixture --payload '<briefing>'` → `Task queued: 01a1ad18-2490-7c83-ae84-5c6c732855e1 (status=pending, source=agent, tags=fixture)`；`gopi task show 01a1ad18-2490-7c83-ae84-5c6c732855e1` 打出 `status: pending` / `summary:` / `briefing:` / `deliverable.repo: /home/user/work/gopi` / `created_at: 2026-11-06 03:03:23`，两次 exit=0；取证完即 `gopi task update 01a1ad18-2490-7c83-ae84-5c6c732855e1 --status cancelled --reason 'D17 evidence-only fixture row: read back by id, now cancelled so no worker claims it'`（输出原样 `Task updated: 01a1ad18-2490-7c83-ae84-5c6c732855e1 (status=cancelled, reason=D17 evidence-only fixture row: read back by id, now cancelled so no worker claims it)`），**不留在队列里等 worker 认领**。三次读数：`/home/user/.gopi/sessions/01a1acc7-79b6-734f-a8cd-aa2743ca1279/artifacts/gap/e3-D17-submit.txt`、`e3-D17-show-cancel.txt`。对方：E2 `CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/ThreadMessageProjection.ts:25` `subagents: readonly Subagent[]`、`CherryHQ/cherry-studio@2292fdd:src/main/ai/agentSession/ThreadMessageProjection.ts:174` `event.subagents?.forEach((relation) => this.assertThread(relation.parent))`、`CherryHQ/cherry-studio@2292fdd:src/shared/ai/claudecode/toolRegistry.ts:188` `'Runs a multi-step workflow that orchestrates subagents'` ⇒ 有子 agent 编排，没有可认领、可按 id 回溯的跨会话任务行 |

## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | D01 |
| 敌强我弱 | — |
| 我有敌无 | D09, D11, D15 |
| 我强敌弱 | D02, D03, D04, D05, D06, D08, D10, D12, D13, D14, D16, D17 |
| 平手 | D07 |
| 不可比 | — |

六个桶互斥且并集恰为全部 17 维：`敌有我无` 1、`敌强我弱` 0、`我有敌无` 3、`我强敌弱` 12、`平手` 1、`不可比` 0，相加 = 17。空桶写 `—`，不用别的桶的内容填。

## 判定列的机械修正（写清，不掩盖）

历史四表的判定列是手写的，与机械函数不一致的维逐对列在下面。**等级一格未改**，只把判定列按 `gapDocVerdictFor(我方,对方)` 改写。

| 等级对（我方 / 对方） | 历史判定 → 机械判定 | 维数 | 维度 |
|---|---|---|---|
| `full` / `partial` | `平手` → `我强敌弱` | 12 | D02, D03, D04, D05, D06, D08, D10, D12, D13, D14, D16, D17 |

## 结论

1. **我方没有安装分发（D01 `none`）是唯一的"敌有我无"**：`ls package.json pyproject.toml setup.py install.sh` 四者皆无、`grep -c '^install:' Makefile` = 0、6 份 `-h` 八类安装词 0 命中；对方在同一维有 `CherryHQ/cherry-studio@2292fdd:electron-builder.yml:168` 的 linux target 与 guide `cherry_studio.md:12` 的安装包下载。
2. **"敌强我弱"在本口径下为空集**（主表 无成员），原因是通道口径而非能力对比：对方上限 `partial`（不得安装运行其程序），我方 15 维 `full`。任何把这张空表读成"我们处处更强"的说法都不成立。
3. **我方独占三维（`我有敌无`：D09 执行隔离、D11 屏幕驱动、D15 代码智能；D11 的证据见第 5 条）**。D09 的读数是沙箱真的建出私有 profile（`settings.json` 0444 / `memory.db` 0600 / `sessions raw logs` 0700），而对方在 `CherryHQ/cherry-studio@2292fdd:docs/dev/references/utility-process/README.md:11` 自述「not a security sandbox: the child is a full Node context」。D15 是对方 E1+E2 双零（`D15_count=0`）对我方 `code-search` 返回 `internal/config/sandbox.go:228`。
4. **对方在一维上明显更深：D07 外部能力扩展**。两边都记 `partial`（等级相同、形态不同）：对方是标准 MCP 客户端（`CherryHQ/cherry-studio@2292fdd:src/shared/data/types/mcpServer.ts:40`）+ 20 个内置 server 源文件；我方 MCP 声明 0 命中、只有 `~/.gopi/plugins/websearch/scripts/` 的 8 个脚本。**这一维的 `partial` 是"我方缺声明"造成的，不是"我方缺实现"**。
5. **D11 屏幕驱动也是 `我有敌无`**，且是本表可信度最高的一格：我方两次 `capture` 的 `sha256_16` 在 `act move` 前后真的变了（`5c3e7c9726791c85` → `1d83331e632aacf9`）；对方的 playwright 只挂在 `CherryHQ/cherry-studio@2292fdd:package.json:113` 的 `test:e2e`，`sendInputEvent` 只用于 `CherryHQ/cherry-studio@2292fdd:src/main/core/window/quirks.ts:64` 的焦点恢复。
6. **D02–D08、D10、D12–D14、D16–D17 这 13 维里，12 维记 `我强敌弱`、D07 一维记 `平手`**（D07 两侧都是 `partial`；历史四表把这 13 维一律记作 `平手`，本表按 `gapDocVerdictFor` 改写，逐维见上「判定列的机械修正」）。`我强敌弱` 的含义只是"我方等级更高"，**不表示能力等同、也不读成"我们处处更强"**——这 12 维全部是 `full` 对 `partial`，差的首先是**证据通道**（对方无 E3）。这 13 维里，对方证据**只有 E2**（代码读得到、没有 guide 佐证）的是 D05 D06 D12 D13 D14 D16 D17 七维，另有 E1 佐证的六维是 D02 D03 D04 D07 D08 D10；我方 13 维全部来自 E3（本 run 真跑出 `exit=0` 与真实输出）。等级口径把"能跑"和"代码这么写"压成 `full` 与 `partial` 两档，这是这张表最需要读者自己补回的一层。
7. **无一维落 `不可比`**：17 维没有出现"任一侧 `unknown`"的情况——对方为 `none` 的三维（D09 D11 D15）与对方为 `partial` 的十四维都有可指认的探针读数，我方为 `none` 的一维（D01）也不缺证据，所以本表 `不可比` 0 行。
