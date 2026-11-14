<!-- doc-class: historical -->

# gopi × Cline 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: devdoc/competitor-analysis-framework.md（§3 十七维、§2 四级口径、§1 证据通道 E1/E2/E3）
our_side_sources: devdoc/competitor/gopi-self-a.md、devdoc/competitor/gopi-self-b.md、devdoc/competitor/gopi-self-c.md（三份阶段 2 盘点，本次未改一字）
opponent_source: devdoc/competitor/cline.md（阶段 2 采集快照，本次未改一字）
opponent_pin: cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145（short `d3ecc48`）—— 本文件每一处 `cline/cline@d3ecc48:path:NNN` 的行号都固定在该 sha 上
opponent_channel_cap: E1+E2 only —— 对方**不产生 E3**（不得安装/运行第三方 agent，Rule 27/35），故对方等级上限 `partial`；本文件的逐维表里对方无一行 `full`
our_measurement_anchor: ground HEAD `4f829a274f92a4e94f63a25e61c2310960dd5847`（`gopi --version` 的 `revision:` 逐字相同，见 `/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-version.txt`）；`v0.4.0` / `go1.24.0` / `linux/amd64`
evidence_dir: `/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/`（不进 git 树）
reuse_note: 标注 `gopi-self-*.md:NNN` / `summary.md:NNN` / `cline.md:NN` 的格，其原始输出属于**另一个 run 的** artifacts（阶段 2 盘点 run 与阶段 4 汇总 run），本 run 未复跑，只引用它记载的读数；标注 `/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/<文件名>` 的格是本 run 亲自执行并落盘的，文件名逐格列在该行证据列里，可直接 `cat` 取回
counts_as_of: 2026-11-06T03:25Z（本文件的拍摄时刻；下表等级是快照读数，随对方 main 与我方 HEAD 变动而失效）

<!-- 对方证据的取回方式（可点开，不是推断）：
     https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path>
     本 run 实测该 sha 的 raw 端点与 trees 端点均可达（`curl -w '%{http_code}'` = 200，
     逐文件读数见 `/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-fetch.txt`；全树 5171 个 path 条目见
     `/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-paths.txt`），故每一处 `cline/cline@d3ecc48:path:NNN`
     都能按上面的模板逐行取回。本 run 未安装、未运行对方任何一件产物。 -->

## 判定口径（按框架 §2，四级严格定义）

| 等级 | 本表采用的判据 |
|---|---|
| `full` | 本 run 或阶段 2 盘点亲自执行、`exit=0`、输出含该维 §3 表「full 证据串」的证据 |
| `partial` | 无 E3，但有 ≥1 条 E1 或 E2 证据（对方 = guide 行号 或 固定 sha 的 code 行号） |
| `none` | 有 E3 拒绝输出，或 E1+E2 在**覆盖过该维的语料**上双零（看过、没有） |
| `unknown` | E1/E2/E3 三通道都没有被执行过（语料无此话题且未运行） |

`none` 与 `unknown` 严格分开：本文件的逐维表**没有一格**用「没查到」冒充 `none`。
判定列只用五个取值：`敌有我无` / `敌强我弱` / `我有敌无` / `平手` / `不可比`。
本文件对阶段 2 的四格做了**升级并逐一写明理由**：`cline.md:20` 记 D01 = `none`，本 run 在固定 sha 上取回 `apps/cli/package.json` 并读到 `"bin"` 字段（框架 §3 的 D01 code 探针原文就是「`package.json` 的 `bin` 字段」），故记 `partial`；`cline.md:25`/`:32`/`:35` 记 D06/D12/D13 = `unknown`，本 run 取回了 `history-command.ts`、`apps/cline-hub/src/server.ts` 正文并读到命中行，故记 `partial`。**升级不是把「没查到」改成「有」**：原等级、新读数与命中行三者都写在证据列里，任何人可按 `opponent_pin` 的 raw 模板 `curl` 复核。

---

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号） |
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 我方：本 run 把可达的 **55** 份帮助页（顶层 + 一级动词 + 二级子动词，406197 字节 / 6540 行）拼成一份语料，在其上普查八类安装词（`npm (install\|i) -g`/`pip3? install`/`pipx install`/`brew install`/`go install`/`uv tool install`/`npx [a-z@/-]+`/`irm .*`）→ `grep -cE` 计数 **0**、`exit=1`；仓库根无 `package.json`/`pyproject.toml`/`setup.py`/`README.md`，唯一获取路径是源码 `Makefile:35` 的 `build:` 目标（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D01-install-census.txt`；与 `gopi-self-a.md:137` 判 `none` 同一结论）；对方：`cline/cline@d3ecc48:apps/cli/package.json:2` `"name": "@cline/cli"`、`:38` `"bin"`、`:4` `"version": "3.0.74"`、`:7` `"publishConfig"`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-D01-cline-pkg.txt`），且 guide `dsa-docs/cline.md:7-21` 给出扩展安装步骤 |
| D02 | full | partial | 我强敌弱 | 我方：`gopi config setup` 幂等，空 home 实跑落 settings.json/sessions/memory.db（`gopi-self-a.md:138`，含 `cmd/agent/cli_setup.go:87 runSetupSubcommand`）；对方：`dsa-docs/cline.md:23` `#### 3. Choose API Key Mode` + `:25` `Bring my own API Key`（E1 原文，`cline.md:21` 同读） |
| D03 | full | partial | 我强敌弱 | 我方：本 run `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic gopi chat --once -p "reply with exactly: OK"` → `exit=0`，第 20 行 `OK`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D03-D05-basurl-headless.txt`）；对方：`dsa-docs/cline.md:53-54` `Select **API Provider** as **OpenAI Compatible**` + `Set **Base URL** to https://api.deepseek.com`（`cline.md:22` 同读） |
| D04 | full | partial | 我强敌弱 | 我方：本 run `gopi config show` → `exit=0`，单页打印 `settings file /home/user/.gopi/settings.json (1455 bytes)` 与 **35** 行键值（`base_url`/`model`/`dev_repo`/`repos` 等），`grep -cE '^  [a-z_.]+ '` = 35（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D04-config.txt`）；对方：`cline/cline@d3ecc48:apps/cli/src/tests/configs/default/data/settings/cline_mcp_settings.json`（全树 path 命中，`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-paths.txt`），配置载体存在但本 run 未取回其正文，E2-path 级证据 |
| D05 | full | partial | 我强敌弱 | 我方：同一个 headless 调用 `exit=0` 并打印答案（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D03-D05-basurl-headless.txt` 第 20 行 `OK`）；对方：`cline/cline@d3ecc48:docs/cline-overview.mdx:20` `Run Cline in your terminal with interactive chat or fully headless automation for CI/CD and scripting.`（`cline.md:24` 同读） |
| D06 | full | partial | 我强敌弱 | 我方：本 run `gopi session list --limit 3` → 3 条 + `[hidden] 157` + `[limit] 2438 more` + `[scanned] 1664 of 3932`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D06-sessionlist.txt`）；对方：`cline/cline@d3ecc48:apps/cli/src/commands/history-command.ts:40` `.command("history")`、`:42` `List session history or manage saved sessions`、`:66` `--session-id <id>`、`:83` 同一 flag 用于 update，另有 `apps/cli/src/utils/resume.ts:4` `export async function loadInteractiveResumeMessages`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-probe-readings.txt`）。**本格由阶段 2 的 `unknown` 升级为 `partial`**：`cline.md:25` 当时记「抽样未取到其内容」，本 run 取回了正文并读到命中行 |
| D07 | partial | partial | 平手 | 我方：扩展点是本地 plugin 树，`ls internal/config/plugins.go cmd/agent/cli_websearch.go` 两文件在、`ls /home/user/.gopi/plugins` → `websearch`，而 `grep -rniE '\bmcp\b' --include=*.go internal/ cmd/` → **0**（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-D07-extend.txt`）；对方：MCP 是对方的扩展点，`cline/cline@d3ecc48:apps/cli/src/commands/mcp.ts`、`apps/cli/src/tui/components/dialogs/mcp-manager-dialog.tsx`、`apps/cli/src/tui/hooks/use-mcp-manager.tsx`，全树 `mcp\|hooks` path 命中 **397**（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-paths.txt`），guide `dsa-docs/cline.md:5` 声明 extension 形态。等级同为 `partial`、机制不同，按框架 A4 不写成打平 |
| D08 | full | partial | 我强敌弱 | 我方：`--policy=forbidden` 拒、`--policy=allow` 过，同一条 bash 两条 E3 输出都存档（`gopi-self-a.md:143`，含 `internal/tools/chat_write_guard.go:202`）；对方：`cline/cline@d3ecc48:apps/cli/src/acp/auto-approve.ts:3` `AUTO_APPROVE_CONFIG_ID = "auto_approve"`、`:11` `Auto-approve tools`、`:13` `Automatically approve all tool calls without asking for permission`、`:26` `parseAutoApproveValue`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-probe-readings.txt`） |
| D09 | full | partial | 我强敌弱 | 我方：本 run `gopi tool sandbox create --label gap-probe --task-id 01a1acc7-7a56-7170-8bcf-4f52aa251136` → `exit=0`，打印 `sandbox /home/user/workspace/gopi-sandbox/sandbox-gap-probe-61466125 created=true`，其 `.gopi/` 下 `memory.db`(0600 空库)/`sessions/`/`raw/`/`logs/`/`settings.json`(0444) 全在，另有 `.sandbox-owner.json` 记 task id + pid（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D09-sandbox.txt`）；对方：`cline/cline@d3ecc48:sdk/packages/core/src/extensions/plugin/plugin-sandbox.ts:18-20` 导入 `SubprocessSandbox`/`WorkerSandbox`、`:45` `Where sandboxed plugins run`，`apps/cli/src/utils/worktree.ts:71` `export async function createTaskWorktree`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-D09-D12.txt`、`e2-probe-readings.txt`） |
| D10 | full | partial | 我强敌弱 | 我方：`vision_analyze` 真跑，`【vision 分析结果】(模型 deepseek-flash, 输入 221 tokens / 输出 107 tokens, 图片 5857 bytes)`，并读出图内文字（`gopi-self-c.md:574`）；对方：`dsa-docs/cline.md:15` `<img src="./assets/cline_step_1.png" width="250" border="1" />`（`cline.md:29`）。**记一条边界**：该 E1 命中是**文档插图**（安装步骤截图），不是图像输入能力的实现点；本格 `partial` 由探针机械命中得出，不代表已确认对方具备图像输入 |
| D11 | full | partial | 我强敌弱 | 我方：本 run 两次全屏 capture 的 `sha256_16` 由 `3506ffc4869f65e8` 经 `gopi tool act move 400 300`（`pointer=400,300`）变为 `138b730f99923275`，三次调用 `exit=0`；另有 `gopi tool act info` 的只读探针 `{"dry_run":false,"pointer":"400,300","root":"1920x1080","note":"read-only probe: nothing was injected"}`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D11-capture-act.txt`、`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D11-act-info.txt`）；对方：`dsa-docs/cline.md:10` `- Click the **Extensions** icon in the activity bar (or press Ctrl+Shift+X).`（`cline.md:30`）。同类边界：该命中是**安装说明里的点击**，不是屏幕捕获/注入的实现点 |
| D12 | full | partial | 我强敌弱 | 我方：本 run `gopi web --addr 127.0.0.1:18097` 起服务后 `curl -w 'http=%{http_code} bytes=%{size_download}'` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D12-web.txt`）；对方：`cline/cline@d3ecc48:apps/cline-hub/src/server.ts:12` `port,`、`:19` `createJsonResponse`、`:78` `function isPublicBrowserRoute(_req: Request, url: URL)`，`apps/cline-hub/src/server/http.ts` 在树中，`grep -cE 'serve\|--port\|localhost:\|HTTP (server\|API)\|API server'` = **27**（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-probe-readings.txt`）。**本格由 `unknown` 升级为 `partial`**，理由同上：正文已取回 |
| D13 | full | partial | 我强敌弱 | 我方：本 run `gopi session log --last` → `exit=0`，打印 `Session: 640ca6c6-2068-4962-aa77-d3f1e1ec9db2`、`Model: deepseek-flash`、`Workdir: /home/user/workspace/01a1acc7-7a03-7e47-b347-9388a14bb18a` 与 `TOOLS:` 段（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D13-sessionlog.txt`）；对方：`cline/cline@d3ecc48:apps/cli/src/commands/history-command.ts:28` `program.opts().json \|\| historyCmd.opts().json ? "json" : "text"`、`:42` 会话历史，`grep -cE 'jsonl\|transcript\|--verbose\|--debug\|log (file\|path)\|trace\|history'` = **22**（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-probe-readings.txt`）。**本格由 `unknown` 升级为 `partial`** |
| D14 | full | partial | 我强敌弱 | 我方：本 run `gopi tool tokens --file devdoc/competitor/cline.md` → `2544 tokens (7242 bytes, 37 lines, deepseek-v4.1-bpe)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D14-tokens.txt`）；对方：`cline/cline@d3ecc48:apps/cli/src/utils/usage-cost-display.ts:3` `export function shouldShowCliUsageCost(providerId: string): boolean`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-grep.txt`），另有 `apps/cli/src/utils/usage-cost-display.ts` 与 `token-usage-ring.tsx` 在树中 |
| D15 | full | partial | 我强敌弱 | 我方：本 run `gopi tool code-search CliVerbTokens` → `CliVerbTokens  cmd/agent/cli_verbs.go:158  const`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D15-codesearch.txt`）；对方：`cline/cline@d3ecc48:sdk/examples/plugins/typescript-lsp/index.ts`（LSP 以**插件示例**形态在树中，`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e2-cline-paths.txt`），`cline.md:34` 记 `.greptile/rules.md:4` 为 E2 文本命中 |
| D16 | full | partial | 我强敌弱 | 我方：本 run `gopi memory search "clone-channel" --limit 1` → 命中 `#18403`（标题以 `clone-channel` 起头）、`tier=warm conf=1.00 sources=row 01a17e04-c358-7460-a5e6-00e972f7085e`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D16-memory.txt`；`gopi-self-b.md:856` 的写→读往返另证）；对方：`cline/cline@d3ecc48:.clinerules/general.md:1` `This file is the secret sauce for working effectively in this codebase. It captures tribal knowledge`（`cline.md:35`）—— 命中落在**规则文件**（跨会话注入的文本），不是可检索的记忆库 |
| D17 | full | partial | 我强敌弱 | 我方：本 run `gopi task show 01a1acc7-7a56-7170-8bcf-4f52aa251136` → `exit=0`，打印 `status: in_progress`、`parent_task_id: 01a1aa6d-3309-7734-a55f-725d3ee87138`、`tags: competitor,phase3`、`runs: 1 run(s)`（`/home/user/.gopi/sessions/01a1acc7-7a56-7170-8bcf-4f52aa251136/artifacts/gap/e3-D17-taskshow.txt`；提交→读回往返见 `gopi-self-b.md:857`）；对方：`cline/cline@d3ecc48:sdk/AGENTS.md:23` `- @cline/agents: stateless agent loop, tool orchestration, hook/extension runtime, event streaming`（`cline.md:36`）—— 命中落在**文档**，子 agent 的行级调度未取到 |

判定列的机械来源：`(我方级,对方级)` 经框架 §7.3 的映射唯一决定，不是手写判断；`grep -cE '^\| D[0-9]{2} \|'` 必须为 17，且每个维度在全文中只出现一次。

## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | D01 |
| 敌强我弱 | — |
| 我有敌无 | — |
| 我强敌弱 | D02, D03, D04, D05, D06, D08, D09, D10, D11, D12, D13, D14, D15, D16, D17 |
| 平手 | D07 |
| 不可比 | — |

六个桶互斥且并集恰为全部 17 维：`敌有我无` 1、`敌强我弱` 0、`我有敌无` 0、`我强敌弱` 15、`平手` 1、`不可比` 0。空桶写 `—`，不用别的桶的内容填。

## 结论

1. **D01 是本对比唯一的「敌有我无」**：我方八类安装词在 **55** 份帮助页（406197 字节 / 6540 行）上计数 0、仓库根无任何打包清单与 README，获取路径只有源码 `Makefile:35`；对方是 npm 上已发布的 `@cline/cli`（`apps/cli/package.json:2/:4/:38` 的 `name`/`version`/`bin`）加扩展安装步骤。这是我方唯一一处**用户拿不到**的能力面。**`敌强我弱` 是空桶**（对方上限 `partial`，见桶视图小节）：历史手写的「敌有我无且敌强我弱」里后半个词不在六判定中，D01 的 `(none, partial)` 经 `gapDocVerdictFor` 只算出 `敌有我无`。
2. **D07 是「平手」，不是「不可比」**（两侧都是 `partial`，`gapDocVerdictFor` 判 `平手`；`不可比` 桶计数 0）：我方扩展点是本地 plugin 树（`/home/user/.gopi/plugins/websearch`，Go 源码里 `\bmcp\b` = 0），对方扩展点是 MCP（`apps/cli/src/commands/mcp.ts`、`mcp-manager-dialog.tsx`，全树 `mcp|hooks` path 397）。档位相同、机制不同，本文件不给高下。
3. **桶 `我有敌无` 是空集，而且这个空集建立在本 run 自己的一次升级上**：阶段 2 把对方 D01 记作 `none`，本 run 取回 `apps/cli/package.json` 正文读到 `:38` 的 `"bin"` 字段（框架 D01 的 code 探针原文），按 §2 升为 `partial`；若保留原读数，桶 `我有敌无` 就会有一行（我方 `full` 对对方 `none`）。**把空集读成「我们处处更强」是这一维最典型的误读**，而这一次连空集本身都不是对方的固有属性，是「路径级普查」与「内容级取回」两种取证深度的差。
4. **桶 `我强敌弱` 的 15 维含义是「我方 E3 对对方 E1/E2」，不是「能力相同」**：对方上限 `partial` 由「不得安装/运行第三方 agent」机械决定（框架 §1、Rule 27/35），我方 15 维 `full` 全部有本 run 或阶段 2 盘点的 `exit=0` 输出。`full` 对 `partial` 是**证据通道差**，不是功能差。
5. **本 run 把三个 `unknown` 升级为 `partial`（D06、D12、D13），升级过程本身就是证据**：在固定 sha 上取回正文 → 命中框架 §3 的 code 探针行 → 记 `partial` 并写明原等级。这不是把「没查到」改写成「有」，两个读数都在证据列里，任何人可 `curl` 复核。
6. **对方证据里有两处是探针假阳性，必须点明（D10、D11）**：`dsa-docs/cline.md:15` 的 `<img src="./assets/cline_step_1.png" width="250" border="1" />` 是安装步骤的**文档插图**，`:10` 的「Click the Extensions icon」是**装扩展的点击**，两者都不构成图像输入或屏幕驱动的实现点。按框架 §2 机械命中记 `partial`，但本条把能力结论留空 —— 记等级不记能力。
7. **我方证据最硬的三格是本 run 亲自跑出的完整形状**：D09 的 `sandbox create` 打印出真实路径与私有 profile 六件（含 `memory.db` 0600、`settings.json` 0444、`.sandbox-owner.json` 记 pid）、D11 两次全屏 `sha256_16` 由 `3506ffc4869f65e8` 变为 `138b730f99923275`、D12 的 `http=200 bytes=69870` 与 `<title>gopi web chat</title>`；三格都是「命令 + `exit=N` + 原始输出」齐全。
8. **对方证据最强的三格**（若要对齐先读这三处）：D07 的 MCP 管理器（`apps/cli/src/commands/mcp.ts` + `mcp-manager-dialog.tsx`）、D09 的双通道插件沙箱（`plugin-sandbox.ts:18-20` 的 `SubprocessSandbox`/`WorkerSandbox`）、D01 的 npm 发布面（`apps/cli/package.json` 的 `bin` + `publishConfig`）。
9. **本文件的边界（写清，不掩盖）**：对方无 E3，逐维表里对方 `full` 结构性不存在；D04、D15 对方那格是 path 级或有文档命中而非实现行，格内已标注；D10/D11 对方那格是探针假阳性（见第 6 条）；`counts_as_of: 2026-11-06T03:25Z`，之后对方 main 与我方 HEAD 任一动，本表即失效。
