<!-- doc-class: historical -->

# gopi × workbuddy 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: `devdoc/competitor-analysis-framework.md`（§3 十七维、§2 四级口径、§1 证据通道 E1/E2/E3、§7.3.1 差距文档契约）
our_side_sources: 本 run 亲自执行的 E3；17 维的命令与原始输出逐维落盘于 `/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/gopi/e3/`（`D01run.log`、`D01-install.log`、`D02setup.log`、`D03run.log`、`D04.log`、`D05.log`、`D06run.log`、`D07run.log`、`D08run.log`、`D08-allow.raw`、`D08-forbidden.raw`、`D09run.log`、`D10run.log`、`D11run.log`、`D12run.log`、`D13run.log`、`D14run.log`、`D15run.log`、`D16run.log`、`D17run.log`、`cap-1.png`、`cap-2.png`）
our_measurement_anchor: ground HEAD `4bc374d0b657903037da25e03dfdccb4616d56d7`（`bin/gopi --version` 的 `revision:` 行逐字相同，见 `e3/D01run.log`）；`v0.4.0` / `go1.24.0` / `linux/amd64`
opponent_source: **阶段 2 的采集文档 `devdoc/competitor/workbuddy.md` 不存在**，本 run 现场按 §7.3.1 的输入口径补做 E1 采集：guide 原文副本 `/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e1-guide.txt`（106 行 / 3520 字节，sha256 `fefe233b0b9e8569b3595189dcabd6a8018aa9fe0b12630b6741bc36b9312cac`，源 `/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/workbuddy.md`）；17 条 doc 探针的逐维命中记录 `/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e1-probes.txt` 与 `e1-probes-fixups.txt`
opponent_e2_missing: **结构性缺失，不是本次没做。** `devdoc/competitor-analysis-framework.md:280` 原文 `| 23 | workbuddy | **（无官方 GitHub 仓库）** | `workbuddy.md` | **缺失**：`Tencent` org 无 CodeBuddy/WorkBuddy 仓，404 |`；`:282` 原文 `**缺失 2 家，显式列出**：`qoder`、`workbuddy`。这两家**没有可 pin 的 sha**`；`:494` 原文 `2. **两家无仓库**（`qoder`、`workbuddy`）结构性缺 E2，等级上限 `partial`，且更常见的结局是 `unknown`。` 本 run 复验取回（`/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e2-probe.txt`）：`curl -s -o /dev/null -w '%{http_code}' https://api.github.com/repos/Tencent/CodeBuddy` → `404`，`.../repos/Tencent/WorkBuddy` → `404`；`search/repositories?q=codebuddy+in:name` 只返回第三方同名仓库（`olasunkanmi-SE/codebuddy`、`lydiahallie/CodeBuddy`、`CharlexH/CodeBuddy`），无厂商仓库可 pin ⇒ **对方无 E2 证据，无 E3 证据（框架 §1 禁止为取证安装/运行第三方 agent，Rule 27/35 同向）**
opponent_channel_cap: E1 only ⇒ 对方等级上限 `partial`，本文件逐维表的对方列没有一行 `full`
counts_as_of: 2026-11-06T08:00+08:00（下表等级是快照读数，随我方 HEAD 与对方语料变动而失效）

<!-- 采集形态（写清，不掩盖）：
     ① 对方的 E1 语料只有一份 106 行的**集成指南**，主题是「怎么把 DeepSeek V4 接进
        WorkBuddy/CodeBuddy」（安装→写 models.json→重启→可选验证 key→troubleshooting）。
        它不是产品参考手册，所以 17 维里只有 5 维落在它的题材内；其余 12 维的探针
        命中 0，按 §4.4 记 `unknown`＋原因「语料无此话题」，**不记 `none`** ——
        「语料没谈这个」与「看过、确实没有」（§2 A2）是两件事。
     ② 对方 E2 结构性缺失（无仓库可 pin），所以「敌有我无」「敌强我弱」两个桶在
        本文件里都结构性为空，不是对方弱，是通道不存在。
     ③ 有一处 doc 探针的**假阴性**已实测纠正并写进对应格：D04 的 §3 正则是
        `settings\.json|config\.(json|toml|yaml|yml)|~/\.([a-z0-9-]+)/`，而 workbuddy 的
        配置载体是 Windows 路径 `C:\Users\<user>\.codebuddy\models.json`，正则不匹配这类
        写法（`grep` 0 命中），但语料该维话题**存在且是全文主体**，故按语义读记 `partial`
        而非 `none`；同一处另有 D01 的**假阳性**（`irm .*` 从 `confirm the file` 里匹配出
        `irm the file`），也已在格里注明并用逐个不含通配的读数据推翻。 -->

---

## 判定口径（按框架 §2，四级严格定义）

| 等级 | 本表采用的判据 |
|---|---|
| `full` | 本 run 亲自执行、`exit=0`、输出含该维 §3 表「full 证据串」指定的字符串（我方只认 E3） |
| `partial` | 无 E3，但有 ≥1 条 E1 或 E2 证据（对方 = guide 的 `file:line`；我方 = §8 指定命令的帮助面） |
| `none` | 有 E3 拒绝输出，或 E1+E2 在**覆盖了该维的语料**上探针命中 0（看过、没有） |
| `unknown` | 三条通道都没有被执行过（语料里根本没有这个话题，且没有跑命令） |

判定列不是本文件作者的判断：它由 `internal/standards/gapdoc_contract_gate.go:201` 的
`gapDocVerdictFor(我方,对方)` 唯一决定（六桶全函数、互斥）。对方上限 `partial` 是结构性的：
`full` 的定义本身要求一次本 run 亲自执行的 E3，而框架 §1 禁止为取证安装/运行第三方 agent。

---

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号） |
|---|---|---|---|---|
| D01 | partial | none | 我有敌无 | 我方：`make install PREFIX=/home/user/workspace/_tmp/gap-wb-d01` → `install -m 0755 bin/gopi /home/user/workspace/_tmp/gap-wb-d01/gopi`、`installed /home/user/workspace/_tmp/gap-wb-d01/gopi`，`make_exit=0`；随后 `/home/user/workspace/_tmp/gap-wb-d01/gopi --version` 打印 `v0.4.0` / `go: go1.24.0` / `platform: linux/amd64` / `revision: 4bc374d0b657903037da25e03dfdccb4616d56d7`，`exit=0`（`e3/D01-install.log`）。但这是**源码构建**不是包管理器分发：六面 help 转储上 `npm (install\|i) -g`/`pip3? install`/`pipx install`/`brew install`/`go install`/`uv tool install`/`npx ` 命中 `0`；`ls package.json pyproject.toml install.sh` → 三者皆 `No such file or directory`（`exit=2`）；`Makefile:129 install: build` 无 OS 分支 ⇒ 不到 `full`（`e3/D01run.log`）。对方：`e1-guide.txt:9` 原文 `- Install and sign in to WorkBuddy/CodeBuddy.` —— 该步只说「安装并登录」，**没有一条具名安装命令**：探针首个匹配 `e1-guide.txt:105` 是**假阳性**（`irm .*` 从 `confirm the file is placed under` 里匹配出 `irm the file`），逐个去掉通配后 `npm (install\|i) -g`/`pip3? install`/`pipx install`/`brew install`/`go install`/`uv tool install`/`npx [a-z@/-]+` **各自命中 0**（`/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e1-probes-fixups.txt`），E2 又无仓库可查 ⇒ 该维在覆盖了它的语料上命中 0，记 `none` |
| D02 | full | partial | 我强敌弱 | 我方：在一个**不存在的空目录**上 `GOPI_HOME=/home/user/workspace/_tmp/gap-wb-d02c GOPI_SETTINGS=/home/user/workspace/_tmp/gap-wb-d02c/.gopi/settings.json ./bin/gopi config setup` → 逐行 `created`/`initialized`，收尾 `4 path(s) provisioned — the profile is run-ready`，`exit=0`；事后 `ls -A .../gap-wb-d02c/.gopi` 得 `memory.db`/`sessions`/`settings.json`（`e3/D02setup.log`）—— 无手写配置文件即可进入可用状态。对方：`e1-guide.txt:27` 原文 `Set your DeepSeek API Key as an environment variable first:`、`:18` 原文 `C:\Users\<your-username>\.codebuddy\models.json` —— 首跑**必须**先导环境变量并手写一份 `models.json`，E1 单独出现，`partial` |
| D03 | full | partial | 我强敌弱 | 我方：`GOPI_BASE_URL=https://api.deepseek.com/anthropic ./bin/gopi --output-language en 'Reply with exactly: GAP-D03-OK'` → `exit=0` 且 stdout 打印 `GAP-D03-OK`（`e3/D03run.log`）；默认端点另见 `config show` 的 `base_url "https://api.deepseek.com/anthropic"`、`vision_base_url` 同值（`e3/D03.log`）⇒ 改环境变量指向非默认供应商后一次真实请求成功。对方：`e1-guide.txt:5` 原文 `WorkBuddy/CodeBuddy is an AI agent and coding assistant. It supports custom models through local model configuration files, and DeepSeek V4 can be connected through the OpenAI-compatible Chat Completions API.`、`:42` 原文 `"url": "https://api.deepseek.com/v1/chat/completions",` —— 有自定义端点的文档载体，无「改端点后一次真实请求」的实现级证据（E2 缺失），`partial` |
| D04 | full | partial | 我强敌弱 | 我方：`./bin/gopi config show` → `exit=0`，键值两列，实测行 `home                   /home/user`、`settings file          /home/user/.gopi/settings.json  (1455 bytes)`、`memory db / ledger     /home/user/.gopi/memory.db  (from built-in default)`、`run repo               /home/user/workspace/01a1acc7-8020-7045-80ea-0a94d3429844 (from current working directory)`（`e3/D02.log`），另有 `./bin/gopi --help-env` 打印 455 行「每个键＝环境变量＝settings.json 键」三列对照（`e3/D04.log`）。对方：§3 该维 doc 探针在本语料上命中 0（`e3` 同款命令，`e1-probes.txt` D04 段 `[hits=0 exit=1]`），**但这是探针假阴性**：workbuddy 的配置载体是 Windows 路径写法，正则需要 `~/\.([a-z0-9-]+)/` 才匹配，实测 `e1-guide.txt:18` 原文 `C:\Users\<your-username>\.codebuddy\models.json`、`:24` 原文 `<your-project>\.codebuddy\models.json`、`:72` 原文 `Save `models.json` as UTF-8 without BOM.`、`:106` 原文 `paste the actual API Key in the UI or in your local `models.json`. —— 单一文档化载体明确存在且是全文主体，故记 `partial`（语义读，非正则命中，已在 meta 声明）；「一条命令打印生效值」的实现证据仍缺（E2 缺失） |
| D05 | full | unknown | 不可比 | 我方：`echo 'Reply with exactly: GAP-D05-OK' \| ./bin/gopi --output-language en 'Reply with exactly: GAP-D05-OK'` 一次调用即返回，`exit=0`，末行 `GAP-D05-OK`（`e3/D05.log`）—— headless 一次性执行，不需要先开 REPL。对方：doc 探针 `--print\|--json\|--output-format\|non-?-interactive\|headless\|print mode` 在 guide 全文命中 `0`（`e1-probes-fixups.txt` D05 段 `[exit=1]`），E2 无仓库可查 ⇒ 该维**没有任何通道被执行过**，记 `unknown`，原因：**语料无此话题**（该 guide 只讲 GUI 内的模型接入，不谈脚本/CI 调用） |
| D06 | full | unknown | 不可比 | 我方：`./bin/gopi session list --limit 3` → `exit=0`，打印 `3 session(s):` 与每行 `1955c0fe-94f5-42c9-8a8a-e0b67aa706c9  [run]  running  ...  turns none (no chats row)`、`[hidden] 157 session(s)`、`[limit] 2662 more session(s) matched`；`./bin/gopi session log --last` 打印会话头 `Session: 1955c0fe-…` / `Model: deepseek-flash` / `Started: 2026-11-06 07:51:00` / `Workdir: …` 之后跟逐轮 `TASK:`（`e3/D06run.log`）；`./bin/gopi chat --resume 61fafe83-9e15-416d-aff5-839808d117d7 --once '…'` → `exit=0`，日志首行 `chat row ensured: 61fafe83-… (mode=chat)` —— 列得出、读得回、续得上。对方：doc 探针 `--resume\|--continue\|/resume\|session[s]? (list\|show)\|--session` 命中 `0`（`e1-probes-fixups.txt` D06 段 `[exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D07 | partial | unknown | 不可比 | 我方：`./bin/gopi tool web search 'gopi agent framework'` → `exit=0` 且返回真实 JSON（`{"status": "ok", "method": "merged", "query": "gopi agent framework", … "count": 5, "inputs": [{"method": "bing", …}, {"method": "google", "status": "ok", "count": 1}, {"method": "arxiv", "status": "ok", "count": 5}]}`），承载它的是插件树 `/home/user/.gopi/plugins/websearch/scripts/` 的 8 个脚本（`arxiv_search.py`/`bing_search.py`/`deepseek_ask.py`/`github_search.py`/`google_search.py`/`merge_results.py`/`web_fetch.py`/`wiki_search.py`，`e3/D07run.log`）；但扩展点**在 help 面无声明**：六面 help 上 `grep -ci mcp` = `0`（`e3/D07.log`）。框架 §8 明文规定该维我方 `E2 有实现点 + E1 无扩展点声明 → 上限 partial，不得记 full`，故记 `partial`。对方：doc 探针 `\bMCP\b\|mcp server\|model context protocol\|plugin\|hook\|extension\|skill` 命中 `0`（`e1-probes.txt` D07 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D08 | full | unknown | 不可比 | 我方：同一条命令行在两种冻结 posture 下得到两种结果 —— `./bin/gopi chat --policy=allow --once --tools "run: bash -c 'echo GAP-D08-ALLOWED' and report its exact output"` → `exit=0`，`Refused by the CHAT WRITE BOUNDARY` 计数 `0`，`call bash` 计数 `1`，结果行 `- **stdout**：`GAP-D08-ALLOWED``；`./bin/gopi chat --policy=forbidden --once --tools`（同一条 prompt）→ `exit=0`，`Refused by the CHAT WRITE BOUNDARY` 计数 `2`，原文 `↳ result bash Refused by the CHAT WRITE BOUNDARY (`bash -c 'echo GAP-D08-ALLOWED'`): the startup posture is `forbi…``，无任何执行结果（`e3/D08run.log`、`e3/D08-allow.raw`、`e3/D08-forbidden.raw`）—— 该维 §3「同一命令在两种策略下得到两种结果，两次输出都存档」成立。对方：doc 探针 `approv\|permission\|allowlist\|allow_list\|--yolo\|dangerously\|ask before` 命中 `0`（`e1-probes.txt` D08 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D09 | full | unknown | 不可比 | 我方：`./bin/gopi tool sandbox create --label gapd09b --task-id 01a1acc7` → `exit=0`，打印 `sandbox /home/user/workspace/gopi-sandbox/sandbox-gapd09b-fbaf2a9d created=true reused=false … owner=task=01a1acc7 pid=3389741`，末行即路径；该路径下 `ls -la …/.gopi/` 得私有 profile `settings.json`（`-r--r--r--`）、`memory.db`（`-rw-------`，0 字节私有账本）、`sessions/`、`raw/`、`logs/`、`plugins -> /home/user/.gopi/plugins`（`e3/D09run.log`）。另有 `./bin/gopi tool sandbox show` → `sandbox root=/home/user/workspace/gopi-sandbox … stale=72h0m0s`（`e3/D09.log`）—— 创建点与回收阈值都在。对方：doc 探针 `sandbox\|isolat\|container\|docker\|worktree` 命中 `0`（`e1-probes.txt` D09 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D10 | full | partial | 我强敌弱 | 我方：`./bin/gopi tool vision_analyze /home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/gopi/e3/cap-1.png` → `exit=0`，首行 `【vision 分析结果】(模型 deepseek-flash, 输入 1010 tokens / 输出 2048 tokens, 图片 336816 bytes)`，正文逐项描述图内容（活动栏 `Activities`、`wechat` 窗口、`Nov 6 07:50`、Firefox 的 DeepSeek 开放平台页、右上角 `Connection failed` 提示）（`e3/D10run.log`）—— 图片作为输入、输出描述了图内容。对方：`e1-guide.txt:47` 原文 `      "supportsImages": false,`、`:62` 同文 —— 语料确有「图片支持」这个话题（两份 model 条目各一行开关），故 E1 成立记 `partial`；但两行都写 `false`，即该语料只声明了这两条本地模型条目**不启用**图片，未声明产品侧的图片入口，故不加权到更高档 |
| D11 | full | unknown | 不可比 | 我方：capture → act → capture 闭环，第二张与第一张 sha256 不同 —— `./bin/gopi tool capture --out …/cap-1.png` → `{"display":":0","mode":"screen","width":1920,"height":1080,…,"bytes":336816,"sha256_16":"0d070aa1068446b4"}`；`./bin/gopi tool act move 400 300` → `{"action":"move","display":":0","dry_run":false,"pointer":"400,300","focus":"0x2400011","root":"1920x1080","detail":"moved pointer to 400,300"}`，`exit=0`；再 capture → `"sha256_16":"7584883b92c957b1"`，`sha256sum cap-1.png cap-2.png` 得 `0d070aa1…f2d` / `7584883b…35d`（`e3/D11run.log`）。对方：doc 探针 `browser\|playwright\|puppeteer\|chrome\|selenium\|click\|mouse` 命中 `0`（`e1-probes.txt` D11 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D12 | full | unknown | 不可比 | 我方：`./bin/gopi web --addr 127.0.0.1:18099 --tools` 起服务后 `curl -s -o /dev/null -w 'http=%{http_code} bytes=%{size_download}' http://127.0.0.1:18099/` → `http=200 bytes=69870`，`exit=0`；服务进程由本脚本自己启动并以自身 pid 结束（`kill 3398103`）（`e3/D12run.log`）—— 本地请求得 200 且响应体有页面。对方：doc 探针 `serve\|--port\|localhost:\|HTTP (server\|API)\|API server` 命中 `0`（`e1-probes.txt` D12 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D13 | full | unknown | 不可比 | 我方：`./bin/gopi session stats 01a1acc7-8020-7045-80ea-0a94d3429844` → `exit=0`，结构化读出 `source  session jsonl 01a1acc7-….jsonl (wire bodies from 01a1acc7-….raw.log)`、`turns 1`、`requests 54`、`prompt tokens 6,121,926 (avg 113,369/request)`、`cache 98.03% hit (6.00M) / 121K miss`、`completion 64,702`；`./bin/gopi session log --last` 读回逐轮 `TASK:` 段（`e3/D13run.log`、`e3/D13.log`）—— jsonl 落盘 + 按轮次读回两条腿都在。对方：doc 探针 `jsonl\|transcript\|--verbose\|--debug\|log (file\|path)\|trace\|history` 命中 `0`（`e1-probes.txt` D13 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D14 | full | unknown | 不可比 | 我方：`./bin/gopi tool tokens --file devdoc/competitor-analysis-framework.md` → `devdoc/competitor-analysis-framework.md: 11373 tokens (35872 bytes, 500 lines, deepseek-v4.1-bpe)`，`exit=0` —— 对同一输入打印确定的整数并自报编码器名（`e3/D14run.log`）。对方：doc 探针 `token (count\|usage\|limit)\|--usage\|/cost\|cost\|credits\|billing` 命中 `0`（`e1-probes.txt` D14 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D15 | full | unknown | 不可比 | 我方：`./bin/gopi tool code-search gapDocVerdictFor` → `exit=0`，返回 `gapDocVerdictFor  internal/standards/gapdoc_contract_gate.go:201  func  func gapDocVerdictFor(our, opp string) (string, error) {` —— 符号定义带 `file:line`（`e3/D15run.log`）；全文一侧另有 `tool keyword_search` 入口。对方：doc 探针 `LSP\|language server\|symbol\|codebase index\|indexing\|grep` 命中 `0`（`e1-probes.txt` D15 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D16 | full | unknown | 不可比 | 我方：`./bin/gopi memory store --topic gap-probe-D16-workbuddy --question '…' --answer '…'` → `Memory stored (id=28598).`，`exit=0`；紧接着 `./bin/gopi memory search gap-probe-D16-workbuddy --limit 3` → 首条命中 `#28598 gap-probe-D16-workbuddy tier=warm conf=0.85 lesson=lesson when=2026-11-05 23:50:18 tokens=1` —— 同 id 往返（`e3/D16run.log`）。对方：doc 探针 `memory\|memories\|knowledge\|learn\|CLAUDE\.md\|AGENTS\.md\|rules file` 命中 `0`（`e1-probes.txt` D16 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |
| D17 | full | unknown | 不可比 | 我方：`./bin/gopi task submit 'gap-workbuddy D17 probe: submit then read back by id' --repo … --tags gap-probe --payload '…'` → `Task queued: 01a1ae20-f565-7771-b25a-df91e1ed4213 (status=pending, source=agent, tags=gap-probe)`；随后 `./bin/gopi task show 01a1ae20-f565-7771-b25a-df91e1ed4213` 按同一 id 读回 `status: pending`、`summary: gap-workbuddy D17 probe: submit then read back by id`、`run_dir: /home/user/.gopi/sessions/01a1ae20-f565-7771-b25a-df91e1ed4213`、`parent_task_id: —`，两次命令均 `exit=0`（`e3/D17run.log`）。对方：doc 探针 `sub-?agent\|multi-?agent\|Task tool\|delegat\|/-?agent\|worker\|queue` 命中 `0`（`e1-probes.txt` D17 段 `[hits=0 exit=1]`），E2 缺失 ⇒ `unknown`，原因：**语料无此话题** |

判定列的机械来源：`(我方等级, 对方等级)` 经 `internal/standards/gapdoc_contract_gate.go:201` 的 `gapDocVerdictFor` 唯一决定，不是手写判断；`grep -cE '^\| D[0-9]{2} \|'` 必须为 17，且每维在全文中只出现一次。

## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | — |
| 敌强我弱 | — |
| 我有敌无 | D01 |
| 我强敌弱 | D02, D03, D04, D10 |
| 平手 | — |
| 不可比 | D05, D06, D07, D08, D09, D11, D12, D13, D14, D15, D16, D17 |

六桶互斥且并集恰为 17：`敌有我无` 0、`敌强我弱` 0、`我有敌无` 1、`我强敌弱` 4、`平手` 0、`不可比` 12。空桶写 `—` 占位，不用别的桶的内容填。

## 结论

1. **本对比的 12 个 `不可比` 是语料厚度的读数，不是能力胜负。** 对方语料只有一份 106 行的集成指南（sha256 `fefe233b…2cac`），17 条 doc 探针里 12 条命中 `0`（`/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e1-probes.txt` 逐维 `[hits=0 exit=1]`），E2 又无仓库可 pin（`api.github.com/repos/Tencent/CodeBuddy` → `404`）。这 12 维对方 `unknown`，按 §2 A4 不进任何胜负桶。
2. **`敌有我无` 与 `敌强我弱` 两桶为空是结构性结果。** 对方通道上限 `partial`（框架 §1 禁装禁跑），`敌有我无` 需要对方 `full` 而我方 `none`、`敌强我弱` 需要对方高于我方，两者在四档口径下不可达 ⇒ 一律写 `—`。没有为了让桶好看而给对方读一个 `full`。
3. **唯一落在 `我有敌无` 的 D01 是「具名安装命令」这一条，不是「有没有安装这个动作」。** 我方 `make install` 实跑 `make_exit=0` 并能打印 `v0.4.0`，但八类包管理器词在六面 help 上命中 `0`、`package.json`/`pyproject.toml`/`install.sh` 三者皆不存在（`e3/D01run.log`）；对方 `e1-guide.txt:9` 只写 `Install and sign in`、探针逐个命中 `0`（`e3/…/e1-probes-fixups.txt`）。两侧都不到 `full`，差在「有没有一条别人能照抄的命令」。
4. **4 个 `我强敌弱` 全部由本 run 亲自执行的 E3 支撑**，锚点 `revision: 4bc374d0b657903037da25e03dfdccb4616d56d7` 与本 clone 的 `git rev-parse HEAD` 逐字相同（`e3/D01run.log`），所以我方等级对得上这份代码，不是引用别人的快照：D02 `config setup` → `4 path(s) provisioned — the profile is run-ready`；D03 `GOPI_BASE_URL=…` 一次真实请求 `GAP-D03-OK` `exit=0`；D04 `config show` 打印生效值 + `--help-env` 455 行；D10 `vision_analyze` 读回桌面并自报 `输入 1010 tokens / 输出 2048 tokens`。
5. **有两处探针读数被实测推翻，写进格里而不是压掉。** D01 的 `irm .*` 是假阳性（从 `confirm the file` 里匹配出 `irm the file`，逐个去通配后全 `0`）；D04 是假阴性（§3 正则匹配 `~/\.<dir>/` 这类 POSIX 写法，而对方载体是 `C:\Users\<user>\.codebuddy\models.json`，`grep` 0 命中但话题就是全文主体，故按语义读记 `partial` 而非 `none`）。两处都在 `/home/user/.gopi/sessions/01a1acc7-8020-7045-80ea-0a94d3429844/artifacts/stage2/workbuddy/e1-probes-fixups.txt` 有原始输出。
6. **本文件的边界（写清，不掩盖）**：① 对方无 E2、无 E3，故对方列结构性无 `full`，「谁更强」在本框架下只能读到档位；② 对方证据取自单一指南文本，该文本一动，行号即失效；③ 12 个 `unknown` 只说明「这份语料没谈这个」，不说明「对方没有」—— 把 `unknown` 读成 `none` 正是 §2 A2 明令禁止的那一步。
