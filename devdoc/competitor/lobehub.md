<!-- doc-class: historical -->
# LobeHub（lobehub/lobehub@658cded）

<!-- meta -->
repo: lobehub/lobehub
sha: 658cded
sha_full: 658cdedf1e5e35eba161d3c54cd95461c28a4a54
sha_commit_time: 2026-11-03T13:53:07Z
sha_subject: `🚀 release: v2.3.1 (#22188)`
default_branch: canary
guide: lobehub.md（dsa-docs 只读快照，92 行）
url_in_guide: https://github.com/lobehub/lobehub 、 https://lobehub.com 、 https://app.lobehub.com
license: NOASSERTION（GitHub 许可证字段原文）—— 仓库内 `LICENSE` 首行为 `LobeHub Community License`，第 3 行为 `Copyright (c) 2024/06/17 - current LobeHub LLC. All rights reserved.`，属自定义社区许可证，不是 OSI 标准许可证
open_source: 源码可见（source-available），非 OSI 开源；自定义 LobeHub Community License
license_evidence: `https://raw.githubusercontent.com/lobehub/lobehub/658cdedf1e5e35eba161d3c54cd95461c28a4a54/LICENSE` 首 3 行；`https://api.github.com/repos/lobehub/lobehub` 的 `license.spdx_id` 字段
pinned_sha_verified_at: 2026-11-06T00:07:00Z（`https://api.github.com/repos/lobehub/lobehub/commits/main` 的 `sha` 字段原值）
channels: E1 = dsa-docs guide 全文 grep -n；E2 = 该 sha 的 `git/trees?recursive=1`（`truncated=false`，23,224 blob 路径）与 20 个 raw 原文文件。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial
evidence_inline: 见下方「证据重放」节——命令原文与原始输出已搬进本文件正文，读者不需要打开任何外部路径即可复核（采集于 2026-11-06；本轮重放于 2026-11-07）
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc/code 探针正则，唯一真源）

## 证据重放（自足：命令原文 + 原始输出）

本节是上方 `channels` 与下方各 D 维「N 处命中」的**唯一生成过程**。

### (1) 树清单 —— E2 的「23,224 blob 路径」「`truncated=false`」，以及 `default_branch: canary` 与 `commits/main` 的出入

采集日落盘的树清单上跑：

```bash
python3 -c 'import json; d=json.load(open("tree.json")); print("sha=%s" % d["sha"]); print("blobs=%d entries=%d truncated=%s" % (sum(1 for e in d["tree"] if e["type"]=="blob"), len(d["tree"]), d["truncated"]))'
```

输出（原样）：

```
sha=658cdedf1e5e35eba161d3c54cd95461c28a4a54
blobs=23224 entries=27642 truncated=False
```

⇒ `blobs=23224` 就是 `channels` 行的「23,224 blob 路径」；`sha` 与首行 `sha_full` 逐字相同。

**本轮重放的诚实结果**：再次 `curl` 这棵两万七千条的大树时，GitHub 在 90 秒内没有返回完整响应（`curl` 退出码 124），这是网络限流。所以 23,224 本轮不是靠重新抓取确认的，而是靠 (2) 的逐字节派生证明的。

**一处需要下游注意的出入（照实记录，不下结论）**：`pinned_sha_verified_at` 记的是 `commits/main` 的 `sha`，而本行 `default_branch` 是 `canary`，两者是不同的 ref。若下游要核对「钉住的 sha 是否主开发线的最新提交」，应改用 `https://api.github.com/repos/lobehub/lobehub/commits/canary`。

### (2) 路径清单由树清单派生 —— 23,224 条不是人工产物

```bash
python3 -c 'import json; d=json.load(open("tree.json")); open("/tmp/derive.txt","w").write("\n".join(e["path"] for e in d["tree"] if e["type"]=="blob")+"\n")'
wc -l < /tmp/derive.txt
diff -q /tmp/derive.txt paths.txt && echo "diff: IDENTICAL"
```

输出（原样，三行）：

```
23224
diff: IDENTICAL
```

### (3) 逐维探针计数 —— 各 D 维「N 处命中」的出处

```bash
while IFS=$'\t' read -r dim re; do printf '%s\t%s\n' "$dim" "$(grep -icE -e "$re" paths.txt)"; done < probe-regex.tsv
```

输出（原样，17 行）：

```
D01	0
D02	185
D03	50
D04	45
D05	4
D06	11
D07	1751
D08	134
D09	300
D10	500
D11	136
D12	3453
D13	190
D14	46
D15	18
D16	824
D17	3306
```

⇒ 下文用到的三个数字逐一对应：D05「4 处 `headless` 命中」= 第五行；D07「计 1751」= 第七行；D15「计 18」= 第十五行。

### (4) E2 原文 —— raw 正文的取法，以及按名猜测取回 404 的实情

```bash
curl -sS -o raw/apps_cli_src_tools_isolatedWorker.ts 'https://raw.githubusercontent.com/lobehub/lobehub/658cdedf1e5e35eba161d3c54cd95461c28a4a54/apps/cli/src/tools/isolatedWorker.ts'
grep -n 'ISOLATED_TOOL_APIS' raw/apps_cli_src_tools_isolatedWorker.ts
```

输出（该文件 `http 200`，D15 引用的第 3 行原文即由此定位）：

```
3:const ISOLATED_TOOL_APIS = new Set(['globFiles', 'grepContent', 'listFiles', 'searchFiles']);
```

**按名猜测取回 404 的三处照实说**：D12 备注写「`apps/server/src/app.ts`、`apps/server/src/index.ts` 按名猜测取回 404」。落盘文件各 **14 字节**，正文就是 HTTP 404 的响应体本身：

```bash
wc -c raw/src_config_modelConfig.ts raw/apps_server_src_app.ts raw/src_envs_appEnv.ts
cat raw/src_config_modelConfig.ts
```

```
14 raw/src_config_modelConfig.ts
14 raw/apps_server_src_app.ts
14 raw/src_envs_appEnv.ts
404: Not Found
```

⇒ 这不是「取回了空内容」，是**该路径在该 sha 上不存在**。任何长度或命中共振都不能拿这三个文件当证据 —— 这正是 D13「写入点未在该文件内取证」与 D12「无该文件证据」的由来。

### (5) E1 —— guide 的 92 行

`dsa-docs` 是只读快照，不在本仓库内；本文件对它的每一处引用（`dsa-docs/lobehub.md:NN`）都已把**该行的原文整句抄进备注**，复核不需要打开快照。规模声明由 `wc -l` 得到：

```bash
wc -l < lobehub.md
```

输出：

```
92
```

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E2 | `lobehub@658cded:apps/cli/package.json:5` 原文 `"bin": {`（第 6-8 行 `"lh": "dist/index.js",` / `"lobe": "dist/index.js",` / `"lobehub": "dist/index.js"`） | E1 侧零命中：guide 92 行里 `npm install`/`brew install` 等探针零命中（`e1-guide.txt` D01 记 NOHIT；`irm` 的两处命中经 `cat -A` 复核为 `confirm` 内部的同名子串，非安装命令）。E2 单独出现，上限 partial |
| D02 | partial | E1+E2 | `dsa-docs/lobehub.md:32` 原文 + `lobehub@658cded:.env.example:51` | E1 原文「`3. Paste your DeepSeek API Key into **API Key**.`」；E2 原文「`OPENAI_API_KEY=sk-xxxxxxxxx`」（第 51 行；该文件共 563 行，逐家供应商各一节）。两条静态证据一致，上限仍为 partial |
| D03 | partial | E1+E2 | `dsa-docs/lobehub.md:33` + `lobehub@658cded:apps/server/src/modules/ModelRuntime/apiKeyManager.ts:11` | E1 原文「`4. Leave **API Proxy URL** empty unless you use a custom proxy. The default endpoint is `https://api.deepseek.com`.`」；E2 原文「`export class ApiKeyManager {`」 |
| D04 | partial | E1+E2 | `dsa-docs/lobehub.md:24` + `lobehub@658cded:.env.example:1` | E1 原文「`Open LobeHub and go to **Settings → Service Model**.`」；E2 原文「`# Specify your API Key selection method, currently supporting `random` and `turn`.`」。单一文档化配置载体存在；「一条命令打印生效值」无 E2 命中、E3 不可用，故不升 full |
| D05 | unknown | — | — | 语料无此话题：guide 92 行 `--print\|--json\|headless\|non-interactive` 零命中；E2 全树路径 4 处 `headless` 命中经打开确认为编辑器组件（`apps/server/src/services/agentDocuments/headlessEditor.ts:12` 原文 `import type { HeadlessEditorOptions, HeadlessLiteXMLOperation } from '@lobehub/editor/headless';`），不是非交互执行模式 |
| D06 | partial | E2 | `lobehub@658cded:apps/server/src/router-hono/agent/handlers/resumeAbandonedParent.ts:11` | E1 零命中；E2 原文「`* Resume the parent of an abandoned sub-agent. Whoever abandoned the child`」。会话可续的代码点存在，未见「列/查」命令，E2 单独出现上限 partial |
| D07 | partial | E2 | `lobehub@658cded:packages/const/src/url.ts:42` + `lobehub@658cded:apps/cli/src/program.ts:63` | E1 零命中；E2 原文「`export const WIKI_PLUGIN_GUIDE = urlJoin(USAGE_DOCUMENTS, '/plugins/development');`」；全树另有 `.claude/skills`、`.codex/skills`、`.cursor/skills` 三个扩展点目录（`e2-path-census.txt` D07 计 1751） |
| D08 | partial | E2 | `lobehub@658cded:src/features/ChatInput/ControlBar/ApprovalMode.tsx:14` + `lobehub@658cded:apps/server/src/services/aiAgent/pipeline/approvalResume.ts:8` | E1 零命中；E2 原文「`import { type ApprovalMode } from '@/store/user/slices/settings/selectors';`」。审批模式的选择器与审批恢复管线各有一个符号 |
| D09 | partial | E2 | `lobehub@658cded:packages/builtin-tool-cloud-sandbox/src/ExecutionRuntime/index.ts:2` + `lobehub@658cded:docker-compose/deploy/docker-compose.yml` | E1 零命中；E2 原文「`import { ComputerRuntime } from '@lobechat/tool-runtime';`」。沙箱执行运行时与自托管编排文件均在固定 sha 上存在 |
| D10 | partial | E2 | `lobehub@658cded:apps/server/src/modules/ModelRuntime/inlineImageNormalizer.ts:4` | E1 零命中；E2 原文「`'image/jpeg': 'jpeg',`」（同文件第 5 行 `'image/png': 'png',`）。图像输入的 MIME 归一化分支存在 |
| D11 | partial | E1+E2 | `dsa-docs/lobehub.md:40` + `lobehub@658cded:apps/desktop/src/main/controllers/BrowserControlCtr.ts:1` | E1 原文「`<sub>DeepSeek provider settings in the LobeHub web app opened with Chrome.</sub>`」；E2 原文「`import type { BrowserKeyCombo } from '@lobechat/device-control/browser-input';`」。浏览器键盘输入注入点在；「看屏幕」的 capture 腿无该维直接证据 |
| D12 | partial | E1+E2 | `dsa-docs/lobehub.md:72` + `lobehub@658cded:apps/server/src/services/agentSignal/enqueue.ts:25` | E1 原文「`If you self-host LobeHub and want the server to provide DeepSeek credentials globally, add the following environment variables and restart the service:`」；E2 原文「`* Enqueues one source event for async AgentSignal execution through Upstash Workflow.`」。自托管服务面存在；HTTP 监听点的入口文件按名猜测取回 404（`apps/server/src/app.ts`、`apps/server/src/index.ts`），无该文件证据 |
| D13 | partial | E2 | `lobehub@658cded:apps/cli/src/commands/topic/readTranscript.ts:32` | E1 零命中；E2 原文「`export const readTopicTranscript = async (`」。结构化轮次记录有读取器；写入点未在该文件内取证 |
| D14 | partial | E2 | `lobehub@658cded:apps/server/src/services/usage/cost.ts:30` | E1 零命中；E2 原文「`// Pricing rates live in the model's currency (per million tokens). Normalize to`」。成本/用量计算点存在 |
| D15 | partial | E2 | `lobehub@658cded:apps/cli/src/tools/isolatedWorker.ts:3` | E1 零命中；全树无 `LSP`/`language server` 命中（`e2-path-census.txt` D15 计 18，逐个查看均为 grep 渲染组件）；E2 原文「`const ISOLATED_TOOL_APIS = new Set(['globFiles', 'grepContent', 'listFiles', 'searchFiles']);`」——全文检索腿存在，符号定义（LSP）腿无证据 |
| D16 | partial | E2 | `lobehub@658cded:apps/cli/src/commands/memory.ts:18` | E1 零命中；E2 原文「`const memory = program.command('memory').description('Manage user memories');`」。记忆读写有命令入口；存储表结构未取证 |
| D17 | partial | E2 | `lobehub@658cded:apps/cli/src/daemon/maintenanceWorker.ts:43` + `lobehub@658cded:apps/server/src/services/agentSignal/enqueue.ts:41` | E1 零命中；E2 原文「`export async function runMaintenanceWorker(config: MaintenanceConfig, ready: () => void) {`」与「`export const enqueueAgentSignalSourceEvent = async <TSourceType extends AgentSignalSourceType>(`」。队列 + 常驻 worker 两个符号各一 |
