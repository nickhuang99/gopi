<!-- doc-class: historical -->
# Qoder CLI（docs.qoder.com，闭源）

<!-- meta -->
repo: 无公开源码仓库（Qoder 是闭源产品；`https://api.github.com/repos/qoder/Qoder` 与 `https://api.github.com/repos/qoder-ai/qodercli` 均返回 `{"message":"Not Found"}`，实测 2026-11-06T00:11:00Z）
sha: 不适用（闭源，无 E2 通道）
distribution: `npm install -g @qoder-ai/qodercli` 的 registry 元数据 `version=1.1.83`、`bin=['qoder','qodercli']`（`https://registry.npmjs.org/@qoder-ai/qodercli/latest`，实测 2026-11-06T00:17:00Z）
guide: qoder.md（dsa-docs 只读快照，43 行）
url_in_guide: https://docs.qoder.com/
license: registry 元数据的 `license` 字段为空（未声明）
open_source: 闭源
official_docs_fetched_at: 2026-11-06T00:18:59Z ~ 2026-11-06T00:19:30Z（逐条时间戳与 HTTP 码逐行抄在下方「证据重放」节 (1)）
channels: E1 = docs.qoder.com 十二个页面，HTML→纯文本后 grep -n（转换器全文抄在下方「证据重放」节 (2)；**下文行号一律是转换后 .txt 的行号**，不是 HTML 行号）。E2 结构性不可用（无源码），E3 不可用（对方不运行，Rule 27/35），故上限 partial
404 记录（见过的页面不存在，不是抓取失败）: `https://docs.qoder.com/cli/non-interactive`、`https://docs.qoder.com/cli/headless`、`https://docs.qoder.com/cli/configurations`、`https://docs.qoder.com/cli/scheduled-task`、`https://docs.qoder.com/cli/mcp`
evidence_inline: 见下方「证据重放」节——命令原文与原始输出已搬进本文件正文，读者不需要打开任何外部路径即可复核（采集于 2026-11-06T00:18:59Z~00:19:30Z；本轮重放于 2026-11-07）
probe_source: devdoc/competitor-analysis-framework.md §3（17 维探针正则，唯一真源）

## 证据重放（自足：命令原文 + 原始输出）

本节是上方 `official_docs_fetched_at`、`channels` 与下方各 D 维行号的**唯一生成过程**。E1 没有源码仓库，所以这一节就是本文件全部证据的来源。

### (1) 抓取时间戳与 HTTP 码 —— 逐行原样

每取一个页面追加一行 `HTTP码 时间戳 URL`：

```bash
printf '%s %s %s\n' "$code" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$url" >> official-fetch-log.txt
```

采集日的日志（原样，27 行；本文件相关的是 `docs.qoder.com` 那 16 行）：

```
200 2026-11-06T00:07:25Z https://docs.qoder.com/user-guide/chat/custom-models
200 2026-11-06T00:07:25Z https://docs.qoder.com/cli/custom-models
200 2026-11-06T00:07:25Z https://docs.qoder.com/
200 2026-11-06T00:07:33Z https://docs.github.com/en/copilot
200 2026-11-06T00:07:33Z https://docs.github.com/en/copilot/concepts/agents/cloud-agent
200 2026-11-06T00:07:33Z https://docs.github.com/en/copilot/how-tos/copilot-cli/cli-getting-started
200 2026-11-06T00:07:33Z https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference
200 2026-11-06T00:07:33Z https://www.codebuddy.ai/docs
200 2026-11-06T00:07:48Z https://cnb.cool/codebuddy/codebuddy-code
200 2026-11-06T00:07:48Z https://www.codebuddy.ai/docs/ide/getting-started
200 2026-11-06T00:07:48Z https://copilot.tencent.com/
2026-11-06T00:15:23Z https://cnb.cool/codebuddy/codebuddy-code/-/git/raw/main/README.md
2026-11-06T00:15:28Z https://cnb.cool/codebuddy/codebuddy-code/-/git/raw/main/LICENSE.txt
200 2026-11-06T00:18:59Z https://docs.qoder.com/cli/quickstart
200 2026-11-06T00:18:59Z https://docs.qoder.com/cli/memory
200 2026-11-06T00:18:59Z https://docs.qoder.com/cli/subagent
200 2026-11-06T00:18:59Z https://docs.qoder.com/cli/permissions
200 2026-11-06T00:18:59Z https://docs.qoder.com/cli/sessions
404 2026-11-06T00:18:59Z https://docs.qoder.com/cli/mcp
200 2026-11-06T00:19:12Z https://docs.qoder.com/cli/installation
404 2026-11-06T00:19:12Z https://docs.qoder.com/cli/non-interactive
404 2026-11-06T00:19:12Z https://docs.qoder.com/cli/headless
200 2026-11-06T00:19:12Z https://docs.qoder.com/cli/mcp-servers
404 2026-11-06T00:19:12Z https://docs.qoder.com/cli/configurations
200 2026-11-06T00:19:30Z https://docs.qoder.com/cli/commands
200 2026-11-06T00:19:30Z https://docs.qoder.com/cli/tools
404 2026-11-06T00:19:30Z https://docs.qoder.com/cli/scheduled-task
```

⇒ `official_docs_fetched_at` 的区间 `00:18:59Z ~ 00:19:30Z` 就是日志末五行的时间跨度；上方「404 记录」列的五个 URL 逐字对应日志里的五行 `404`。**这份日志里也有别的产品的抓取行**（github.com、codebuddy.ai、cnb.cool），它们属于同一次批量采集的其他文件，不是本产品的证据。

### (2) HTML→纯文本转换器 —— 全文原样

`channels` 行说的「转换器」就是下面这 12 行，**行号口径由它决定**：它丢掉所有空行，所以「转换后 .txt 的第 N 行」与 HTML 里的行号没有任何对应关系。

```python
import sys, re, html
src, out = sys.argv[1], sys.argv[2]
raw = open(src, encoding="utf-8", errors="replace").read()
raw = re.sub(r"(?is)<(script|style|svg|noscript)[^>]*>.*?</\1>", " ", raw)
raw = re.sub(r"(?i)</(p|div|li|tr|h[1-6]|pre|br)\s*/?>", "\n", raw)
raw = re.sub(r"(?i)<br\s*/?>", "\n", raw)
raw = re.sub(r"<[^>]+>", " ", raw)
raw = html.unescape(raw)
lines = [re.sub(r"[ \t]+", " ", l).strip() for l in raw.split("\n")]
lines = [l for l in lines if l]
open(out, "w", encoding="utf-8").write("\n".join(lines) + "\n")
print(f"{src} -> {out} lines={len(lines)}")
```

用法与它自己打印的回执（本轮重放，跑的是采集日落盘的那份 HTML）：

```bash
python3 html2txt.py qoder/raw/q-installation.html /tmp/q-installation.txt
diff -q /tmp/q-installation.txt qoder/raw/q-installation.txt && echo "IDENTICAL"
```

输出（原样，两行）：

```
qoder/raw/q-installation.html -> /tmp/q-installation.txt lines=265
IDENTICAL
```

⇒ 重跑转换器得到 **265 行**，与采集日落盘的 `q-installation.txt` 逐字节相同。所以 D01 格引用的 `q-installation.txt:98`（macOS/Linux 的安装命令）这类行号，是任何持有该 HTML 的人用这 12 行代码都能精确重算出来的。

⇒ 本文件所有 `q-<page>.txt:<line>` 都是**这个转换器的输出行号**。下游若用别的转换器（或不转换）复核，行号会对不上，那是口径不同而不是证据不同 —— 复核时请用同一段代码。

### (3) 十二个页面 —— 「十二个」是怎么数出来的

```bash
ls raw/*.txt | wc -l
for f in raw/*.txt; do printf '%-30s %s\n' "$(basename $f)" "$(wc -l <$f)"; done
```

输出（原样）：

```
12
q-commands.txt                 375
q-installation.txt             265
q-mcp-servers.txt              251
q-memory.txt                   444
q-permissions.txt              644
q-quickstart.txt              230
q-sessions.txt                 266
q-subagent.txt                631
q-tools.txt                    268
qoder-cli-custom-models.txt    204
qoder-custom-models.txt        176
qoder-home.txt                 153
```

### (4) 语料规模与逐维命中数 —— 各 D 维「N 处命中」的出处

```bash
while IFS=$'\t' read -r dim re; do printf '%s\t%s\n' "$dim" "$(cat raw/*.txt | grep -icE -e "$re")"; done < probe-regex.tsv
```

输出（原样，17 行）：

```
D01	25
D02	59
D03	55
D04	11
D05	26
D06	12
D07	379
D08	168
D09	12
D10	0
D11	10
D12	92
D13	10
D14	18
D15	17
D16	196
D17	207
```

与采集日的 census 文件逐行 `diff` 结果为空。⇒ 下文 D01 的 `q-installation.txt:98` 一类行号，就是这份语料上的 `grep -n` 行首数字。

### (5) 本轮复核记下的一处出入（不改判定）

首部 `guide: qoder.md（dsa-docs 只读快照，43 行）` 写的是 43 行，而快照实际是 104 行：

```bash
wc -l < qoder.md
sed -n '43p' qoder.md | cat -A
```

输出（原样，第一行是行数；第二行是第 43 行的 `cat -A`，`$` 是行尾）：

````
104
```$
````

⇒ 第 43 行**本身就是一个代码围栏**（三个反引号，`cat -A` 显示为 ``` `` 加 `$`），说明首部那个「43」很可能是写到一半被截断的那一行，而不是文件规模。**这只是一条记录：本文所有 D 维证据都取自 `docs.qoder.com` 的十二个页面，没有一条引用 guide 的行号**，所以这个数字不影响任何判定。

### (6) E1 之外的通道为什么缺席 —— 不是省略，是实测

- **E2 结构性不可用**：闭源，无公开源码仓库。两个候选都返回 `{"message":"Not Found"}`（见首部 `repo:` 行，实测 2026-11-06T00:11:00Z）。
- **E3 不可用（Rule 27/35）**：对方产品不在本机安装、不运行，因此拿不到「一条命令的实测输出」。这两条合起来把本文件的上限压到 `partial`。

| 维度 | 等级 | 证据等级 | 证据（file:line，行号为转换后文本行号） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `q-installation.txt:98`、`q-installation.txt:110` | 原文第 98 行 `curl -fsSL https://qoder.com/install \\| bash`；第 101 行 `irm https://qoder.com/install.ps1 \\| iex`；第 104 行 `curl -fsSL https://qoder.com/install.cmd -o install.cmd && install.cmd`；第 110 行 `npm install -g @qoder-ai/qodercli`。OS 分支明确（macOS/Linux、Windows PowerShell、Windows CMD），且有验证命令第 115 行 `qoder --version`。无 E3 → partial |
| D02 | partial | E1 | `q-quickstart.txt:86`、`q-quickstart.txt:87` | 原文第 86 行 `Qoder CLI installed : Running qoder --version should print the version number.`；第 87 行 `Signed in : You will be automatically guided through the sign-in process the first time you run qoder .` —— 首跑由产品引导登录，不需要先手写配置文件 |
| D03 | partial | E1 | `qoder-cli-custom-models.txt:84`、`:94` | 原文第 84 行 `Qoder CLI supports BYOK (Bring Your Own Key). You can use an API key obtained from a model provider to call models included in the corresponding subscription plan. Add and manage custom models from the Custom tab in /model .`；第 94 行 `Configure BYOK through the Custom wizard in /model . Do not configure BYOK manually in settings.json` |
| D04 | partial | E1 | `q-installation.txt:148`、`q-mcp-servers.txt:110` | 原文第 148 行 `Auto-Upgrade is enabled by default. To disable it, set general.enableAutoUpdate to false in ~/.qoder/settings.json :`；第 110 行 `~/.qoder/settings.json`。单一文档化载体存在（`~/.qoder/settings.json` + `context.fileName` 等键名），但没有"一条命令打印生效值"的文档证据 → partial |
| D05 | partial | E1 | `q-permissions.txt:144`、`q-commands.txt:98` | 原文第 144 行 `Headless ( -p / --prompt ) Auto-deny No interaction available; ask becomes deny .`；第 147 行 `Headless mode ( -p / --prompt ) can be combined with permission modes:`；`q-commands.txt:98` 为章节标题 `Using Commands in Headless Mode`。有 `-p/--prompt` 这一具名非交互开关 |
| D06 | partial | E1 | `q-sessions.txt:110`、`:115`、`:118` | 原文第 110 行 `Use -c ( --continue ) to directly continue the most recent session for the current project:`；第 115 行 `Use -r ( --resume ) to resume a previous session by its ID:`；第 118 行 `Running qoder --resume without an ID opens the Session Browser`。列/续/命名（第 123 行 `--fork-session`）三类齐备 |
| D07 | partial | E1 | `q-mcp-servers.txt:83`、`:119`、`:127` | 原文第 83 行 `Qoder CLI can connect to Model Context Protocol (MCP) servers to use external tools and data sources. After a server is added, its tools become available to the agent in interactive and non-interactive sessions.`；第 119 行 `qoder mcp list`；第 127 行 `qoder mcp add context7 -- npx -y @upstash/context7-mcp@latest`。另有 `q-commands.txt:119` 记载 Skill/Plugin/Workflow 三类命令分类 |
| D08 | partial | E1 | `q-permissions.txt:84`~`:91`、`:97` | 权限模式表原文：第 87 行 `default Normal interactive use Safe reads and internal actions run automatically; sensitive actions require confirmation.`；第 90 行 `bypass_permissions (YOLO) Trusted local experiments only Skips approval prompts.`；第 91 行 `dont_ask Headless flows that must not prompt Never prompts. Any action that would normally ask is denied instead.`；第 97 行 `Use --permission-mode to set the default behavior for the current session:`。allow/ask/deny 三态具名 |
| D09 | partial | E1 | `q-subagent.txt:108`、`:270`、`:272` | 原文第 108 行 `Workspace isolation Use worktree isolation when a Subagent should run in a separate git worktree.`；第 270 行 `To run a Subagent in a separate worktree, add this to its frontmatter:`；第 272 行 `isolation : worktree`。隔离形态是 git worktree，没有 sandbox/容器；回收机制无文档证据 |
| D10 | none | E1 | — | 看过、没有：十二份文档合并后 `grep -ciE 'screenshot\\|\.png\\|image\\|vision\\|multimodal\\|attach'` = **0**（`e1-census.txt` D10 = 0）。语料覆盖了该维（`q-tools.txt` 是工具参考页，第 96-107 行逐条列 Read/Edit/Create 类工具，无图像入口）。E2 结构性不可用 |
| D11 | partial | E1 | `q-mcp-servers.txt:88` | 原文第 88 行 `qoder mcp add playwright -- npx -y @playwright/mcp@latest` —— 屏幕驱动以接入 Playwright MCP 的方式实现，产品自身不内置；capture→act 闭环无文档证据 |
| D12 | none | E1 | — | 看过、没有：十二份文档里 `serve\\|--port\\|localhost:\\|API server` 的命中经逐条查看**全部是 MCP server 语境**（`q-mcp-servers.txt:83` 原文 `Qoder CLI can connect to Model Context Protocol (MCP) servers to use external tools and data sources.`、`q-mcp-servers.txt:94` 原文 `stdio The MCP server runs as a local command.`、`q-mcp-servers.txt:98` 原文 `If you do not specify a type, local command servers should use the default stdio behavior.`；`q-tools.txt:147` 亦有同一句 MCP 定义），没有一处是"本产品对外起 HTTP 服务/监听端口"。逐条 grep 的原始输出见 `e1-official-2.txt` D12 段 |
| D13 | partial | E1 | `q-sessions.txt:88`、`q-commands.txt:122` | 原文第 88 行 `Sessions are isolated and stored per project. Each session has a unique session ID, and session logs are saved in project-organized directories under the User Configuration Directory (default ~/.qoder/projects/<project>/<session-id>.jsonl ).` —— 结构化 jsonl 落盘；`q-commands.txt:122` 原文 `/export [filename] TUI Export the current session to a file`。按轮次读回的命令无独立证据 |
| D14 | partial | E1 | `q-commands.txt:142` | 原文 `/usage TUI View usage statistics, including token consumption and other information`；另 `q-quickstart.txt:123` 原文 `/usage View Credits usage`。token 与 credits 两个口径 |
| D15 | partial | E1 | `q-tools.txt:91`、`q-tools.txt:162`、`q-permissions.txt:278` | 全文检索腿：第 91 行 `Code Search code, text, and configuration files.`、第 162 行 `qoder --tools Read Grep Glob --allowed-tools 'Read,Grep,Glob'`。符号级定位腿：十二份文档 `grep -iE 'LSP\\|language server\\|symbol\\|codebase index\\|indexing'` = **0 命中**（`e1-census.txt` D15 = 17 全部来自 `settings.json` 类误命中，逐条查看无 LSP）。故 partial |
| D16 | partial | E1 | `q-memory.txt:85`、`:86`、`q-commands.txt:129` | 原文第 85 行 `Static memory: Persistent instructions maintained by you or your team, including AGENTS.md and rules.`；第 86 行 `Auto-Memory: Markdown memory saved locally by Qoder CLI when enabled.`；`q-commands.txt:129` 原文 `/memory TUI Open the Memory Overview; when Auto-Memory is enabled, open the auto-memory folder, or use /memory manage to manage topic files`。写入与检索各有入口，未见"检索命中"命令 |
| D17 | partial | E1 | `q-subagent.txt:101`、`:108`、`q-commands.txt:115` | 原文第 101 行 `Orchestrate complex work Qoder CLI can dispatch independent Subagents in parallel. When background Subagent sessions are enabled, independent work can also run in the background and notify the main session later.`；`q-commands.txt:115` 原文 `/agents TUI View and manage the Subagent list, supporting the creation and editing of Subagent configurations`。并行派发与配置面在；**任务行落账本、可按 id 读回**这一类能力无任何文档证据 |
