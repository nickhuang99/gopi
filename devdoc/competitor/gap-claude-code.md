<!-- doc-class: historical -->

# gopi × claude-code 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: devdoc/competitor-analysis-framework.md（§3 十七维、§2 四级口径、§1 证据通道 E1/E2/E3）
opponent_source: devdoc/competitor/claude-code.md（阶段 2 采集快照，本 run 未改一字）
opponent_repo: anthropics/claude-code
opponent_sha: 26382c08758260c9f5deb2019d8fe851f89fa9ae（short `26382c0`）—— 本文件每一处 `anthropics/claude-code@26382c0:path:line` 都固定在该 sha 上
opponent_guide: dsa-docs/claude_code.md（本地语料，109 行；原件 `/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/claude_code.md`，本 run 的副本 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/guide-claude_code.md`）
opponent_channel_cap: E1+E2 only —— 对方**不产生 E3**（不安装、不运行第三方 agent，Rule 27/35），故对方等级上限 `partial`；主表 17 行里对方无一格 `full`
our_side_sources: devdoc/competitor/gopi-self-a.md、gopi-self-b.md、gopi-self-c.md（阶段 2 盘点，本 run 未改一字）+ 本 run 亲自执行的 E3
our_measurement_anchor: `gopi --version` → `v0.4.0` / `go1.24.0` / `linux/amd64` / `revision: 4f829a274f92a4e94f63a25e61c2310960dd5847`（原件 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D01-version.txt`）；本 ground 的 branch 基线 `ed45ace7faa3c491a38ef74383f4bcd1ea198d04`
evidence_dir: /home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/（不进 git 树；下每个文件名可直接 `cat` 取回）
opponent_fetch: 每个 E2 文件由 `curl https://raw.githubusercontent.com/anthropics/claude-code/26382c08758260c9f5deb2019d8fe851f89fa9ae/<path>` 取回并存进 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/`（文件名规则：把该 path 的 `/` 换成 `_` 再加前缀 `cc_`，例如 `mods/agents-md/README.md` 存为 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/cc_mods_agents-md_README.md`），全部 HTTP 200（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e2-quotes.txt` 记下逐行原文）
counts_as_of: 2026-11-06T03:24Z（拍摄时刻；之后对方 main 或我方 HEAD 一动，本表即失效）

## 判定口径（按框架 §2 四级；判定列由机械函数算出，不手写）

| 等级 | 判据 |
|---|---|
| `full` | 本 run 亲自执行过一条命令，`exit=0`，且输出含 §3 表 `full 证据串` 的字面串，命令与输出都已存档 |
| `partial` | 无 E3，但有 ≥1 条 E1（`guide:NN`）或 E2（`owner/repo@sha:path:line`）证据 |
| `none` | 有 `exit≠0` 的拒绝输出，或该维 E1+E2 在**覆盖过该维**的语料上双零（看过、没有） |
| `unknown` | 三通道都没有被执行过，或语料只被抽样读过、不足以判"没有" |

主表的「判定」列**不是手写**，它由 `(我方级, 对方级)` 经框架 §7.3.1 ② 的映射表唯一决定，
代码是 `internal/standards/gapdoc_contract_gate.go` 的 `gapDocVerdictFor`。完整映射：

| 我方 \ 对方 | `full` | `partial` | `none` | `unknown` |
|---|---|---|---|---|
| `full` | 平手 | 我强敌弱 | 我有敌无 | 不可比 |
| `partial` | 敌强我弱 | 平手 | 我有敌无 | 不可比 |
| `none` | 敌有我无 | 敌有我无 | 平手 | 不可比 |
| `unknown` | 不可比 | 不可比 | 不可比 | 不可比 |

`none` 与 `unknown` 严格分开：本文件没有一格用「没查到」冒充 `none`。
**`full` 对 `partial` 落 `我强敌弱`，不是 `平手`** —— 这是六桶相对历史四桶最关键的一格，
也是本文件 13 维实际所在的桶。

## 主表（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号） |
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 我方：10 份 `gopi -h` 全量语料（96,375 字节 / 1,504 行，文件 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/help-corpus.txt`）对 `npm (install\|i) -g\|pip3? install\|pipx install\|brew install\|go install\|uv tool install\|npx [a-z@/-]+` 命中 **0**、grep `exit=1`（原样命令与输出见 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D01-D13.txt`）；对方：`claude_code.md:18` ``npm install -g @anthropic-ai/claude-code`` + `:24` ``claude --version`` |
| D02 | full | partial | 我强敌弱 | 我方：`GOPI_HOME=<sandbox> gopi config setup` → `exit=0`，四行 `present` 末行 `nothing to do — the profile is run-ready`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D09-D02-sandbox-setup.txt`）；对方：`claude_code.md:36` 配置可由「文件或环境变量」给出，`:43` 原文 **If the file does not exist, create it.** —— 该 sha 的 E1 描述的首跑动作是手写 `settings.json` 并填 key |
| D03 | full | partial | 我强敌弱 | 我方：`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic gopi chat --once -p "reply with exactly: OK"` → `exit=0`，输出第 15 行 `OK`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D03-out.txt`）；对方：`claude_code.md:50` 配置体里 `"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic"`，`:67` ``export ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`` |
| D04 | full | partial | 我强敌弱 | 我方：`gopi config show` → `exit=0`，键值两列可读，含 `base_url "https://api.deepseek.com/anthropic"`、`api_key <set: 35 chars>`、`model "deepseek-flash"`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D04-config.txt`）；对方：`anthropics/claude-code@26382c0:examples/settings/README-hipaa.md:3`（`Example `managed-settings.json` and `managed-mcp.json` files`）给出配置载体，但该 sha 的 E1/E2 上未取到「打印生效值」的命令 |
| D05 | full | unknown | 不可比 | 我方：`gopi chat --once -p "reply with exactly: OK"` → `exit=0` 且打印答案 `OK`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D03-out.txt:15`）；对方：`claude_code.md` 全文对 `--print\|--json\|--output-format\|non-?interactive\|headless\|print mode` 命中 0，本 run 抽样的 8 个 E2 文件同样 0（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e2-e1-probe-scan.txt`）；全树 1,473 个 blob 未逐文件读，故记 `unknown` 而不是 `none`（框架 A2） |
| D06 | full | partial | 我强敌弱 | 我方：`gopi session list --limit 3` → 3 条 + `[hidden] 157 session(s) hidden`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D06-sessionlist.txt`）；`gopi session log c5df5e99-b898-4318-b790-b017169b53c0` 读回上一轮的提示、工具调用与结果（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D06-D13-D11b.txt` 第 9/12/19/20 行）；对方：`anthropics/claude-code@26382c0:mods/diff/tests/fixtures/resume.ts:7-9` ``export const RESUME: CommandRunInput = { command: 'resume', args: '',``（测试夹具，非实现），`claude_code.md` 探针 0 命中 |
| D07 | partial | partial | 平手 | 我方：扩展点在盘 —— `/home/user/.gopi/plugins/websearch/scripts/` 下 `arxiv_search.py` `bing_search.py` `deepseek_ask.py` `github_search.py`，加载路径由 `gopi --help-env:88` 的 `GOPI_WEBSEARCH_SCRIPTS_DIR` 决定；`\bmcp\b` 在 5 个命令页命中 0，且 `gopi tool websearch` 不是已注册动词（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D07-plugins.txt`）；对方：`anthropics/claude-code@26382c0:.claude-plugin/marketplace.json:3` `"name": "claude-code-plugins",` 与 `:5` `installed from the claude-plugins-official repository` |
| D08 | full | partial | 我强敌弱 | 我方：同一命令在两种策略下两种结果 —— `--policy=forbidden` 得 `Refused by the CHAT WRITE BOUNDARY`，`--policy=allow` 得 `POLICY-PROBE-A` 真被执行，两次调用均 `exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D08-policy.txt`）；对方：`anthropics/claude-code@26382c0:CLAUDE.md:19-22` `--permission-mode auto`、`--disallowedTools`、`runs only if Claude Code's safety review passes it`，另 `mods/sec-default/hooks/policy/has-mcp-allowlist.ts:10` ``export const hasMcpAllowlist`` |
| D09 | full | partial | 我强敌弱 | 我方：`gopi tool sandbox create --label gapcc-probe` 打印 `/home/user/workspace/gopi-sandbox/sandbox-gapcc-probe-fec812d1`，其 `.gopi/` 下 `settings.json` 权限 `-r--r--r--`、`memory.db` `-rw-------`、`sessions/ raw/ logs/` 三个目录（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D09-D02-sandbox-setup.txt`）；`gopi tool sandbox show` 打印 `sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D09-sandbox.txt`）；对方：`anthropics/claude-code@26382c0:.devcontainer/devcontainer.json:2` `"name": "Claude Code Sandbox",` + `:4` `"dockerfile": "Dockerfile",` + `:45-46` 两个 volume |
| D10 | full | partial | 我强敌弱 | 我方：`vision_analyze` 对 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/D11-before.png`（332,212 字节）真跑，输入 1018 tokens / 输出 1299 tokens，返回屏幕布局描述与识别出的文字（同一次调用的产物见 D11 行）；对方：`anthropics/claude-code@26382c0:mods/agents-md/README.md:133` ``IDE's opened file or selection, and for the `Read` tool's notebook, image`` —— Read 工具存在 image 附件分支 |
| D11 | full | partial | 我强敌弱 | 我方：`gopi tool capture` → `gopi tool act move 1900 1070` → `gopi tool capture`，`sha256_16` 由 `b29a050f2f1ab25e` 变为 `86f4fce933a2c236`，三次调用 `exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D11-capture-act.txt` 与 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D06-D13-D11b.txt`；首次 `act move 400 300` 坐标处无可见变化，两图 sha 相同，故换到远角重测）；对方：`claude_code.md:107` `click the Claude Code icon in the sidebar` 是 IDE 操作说明而非 capture→act；全树 1,473 个 blob 的路径对 `click\|mouse\|browser\|playwright\|screenshot` 命中 0（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e2-e1-probe-scan.txt` 的 D11 行由 `tree.json` 复核） |
| D12 | full | partial | 我强敌弱 | 我方：`gopi web --addr :8799` 起服务后 `curl http://127.0.0.1:8799/` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D12-web.txt`，页面原件 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/web-index.html`）；对方：全树有 `examples/gateway/aws/` 与 `examples/gateway/gcp/`（各含 `Dockerfile` 与 `gateway.yaml.example`），其 `README.md` 两次 `curl` 均 40s 超时未取到，本地 HTTP 面未观察到 |
| D13 | full | partial | 我强敌弱 | 我方：`gopi session stats --tools c5df5e99-b898-4318-b790-b017169b53c0` → `tools 1 calls / 1 kinds` 与明细行 `1  100.0%  bash`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D06-D13-D11b.txt`）；对方：`anthropics/claude-code@26382c0:mods/telemetry/hooks/register.ts:129` `hasCursorTraceId: IsAnalyticsOff.isEnvSet(` + `:130` `await beneath.env.get('CURSOR_TRACE_ID'),` |
| D14 | full | partial | 我强敌弱 | 我方：`gopi tool tokens --file devdoc/competitor-analysis-framework.md` → `9833 tokens (31149 bytes, 424 lines, deepseek-v4.1-bpe)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D14-tokens.txt`）；对方：`anthropics/claude-code@26382c0:mods/diff/tests/fixtures/usage-at.ts:2` ``What `$.session.usage()` answers`` 与 `:12` `context: { window: 200_000 },`（同目录 `mods/telemetry/hooks/entries/token/token.ts:5` 的 `TOKEN` 是事件名正则、不是 token 计数，不作本维证据） |
| D15 | full | unknown | 不可比 | 我方：`gopi tool code-search CliVerbTokens` → `CliVerbTokens  cmd/agent/cli_verbs.go:158  const`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D15-codesearch.txt`）；对方：`claude_code.md` 探针 0 命中；全树路径对 `symbol\|lsp\|index\|grep` 的 442 个命中经逐条抽查全是 `index.ts` 一类文件名噪声（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e2-e1-probe-scan.txt`），不构成符号索引证据，故记 `unknown` |
| D16 | full | partial | 我强敌弱 | 我方：`gopi memory search "clone-channel delivery" --limit 1` → 命中 `#18403`，其 `sources=row 01a17e04-c358-7460-a5e6-00e972f7085e` 指回任务行，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-batch-help.txt`）；对方：`anthropics/claude-code@26382c0:mods/agents-md/README.md:3` ``\`AGENTS.md\` read the way Claude Code reads \`CLAUDE.md\`, as a plugin`` 与仓根 `CLAUDE.md:3` `Guidance for Claude Code and other coding agents working in this repository.` —— 静态规则文件，未见可检索的知识库 |
| D17 | full | partial | 我强敌弱 | 我方：`gopi task list --status pending` → `29 task(s) with status=pending`；`gopi task show 01a1acc7-7a03-7e47-b347-9388a14bb18a` → `status: in_progress` + `session:` + `run:` 行，两次 `exit=0`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D17-task.txt`）；对方：`anthropics/claude-code@26382c0:mods/agents-md/README.md:95` ``\\| `agent.spawn` on `fork: true` \\|`` 与 `:155` `A subagent that is not a fork gets a nested `AGENTS.md` at its own first` |

## 桶视图（六个桶互斥，并集恰为 D01–D17）

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | D01 |
| 敌强我弱 | — |
| 我有敌无 | — |
| 我强敌弱 | D02, D03, D04, D06, D08, D09, D10, D11, D12, D13, D14, D16, D17 |
| 平手 | D07 |
| 不可比 | D05, D15 |

六个桶互斥且并集恰为全部 17 维：`敌有我无` 1、`敌强我弱` 0、`我有敌无` 0、`我强敌弱` 13、`平手` 1、`不可比` 2，相加 = 17。空桶写 `—`，不用别的桶的内容填。

## 与阶段 2 快照的等级差异（本 run 重采，逐维记；本段不含主表行，`grep -cE '^\| D[0-9]{2} \|'` 仍是 17）

阶段 2 快照 `devdoc/competitor/claude-code.md` 自身记 8 格 `partial` / 9 格 `unknown`。本 run 按框架 §7.3「发现矛盾就回阶段 2 重采」重取了 E1/E2，等级从 `unknown` 升级为 `partial` 的共 **7 维**，每维的取回证据在 `/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/` 下可 `cat`：

- **D01**：快照写「E1/E2 三通道均未取得」，实测 `guide-claude_code.md:18` 就是 ``npm install -g @anthropic-ai/claude-code``、`:24` 是 ``claude --version``。
- **D03**：快照写「未取得」，实测 `guide-claude_code.md:50` 与 `:67` 给出 `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic` 两条配置通道。
- **D06**：快照写「抽样未取到内容」，实测取到 `mods/diff/tests/fixtures/resume.ts`（`command: 'resume'`），是**测试夹具**而非实现，故记 `partial` 并在备注里点名。
- **D08**：快照写「抽样未取到内容」，实测取到仓根 `CLAUDE.md:19-22`（`--permission-mode auto`、`--disallowedTools`）与 `mods/sec-default/hooks/policy/has-mcp-allowlist.ts:10`。
- **D12**：快照写「抽样未取到内容」，实测全树有 `examples/gateway/aws/` 与 `examples/gateway/gcp/`（各含 `Dockerfile` 与 `gateway.yaml.example`），但两个 `README.md` 本 run 两次 `curl` 均超时未取到，故证据强度只到「路径与文件名」。
- **D14**：快照写「未取得」，实测取到 `mods/diff/tests/fixtures/usage-at.ts:2`（``What `$.session.usage()` answers``）与 `:12`（`context: { window: 200_000 },`）。
- **D16**：快照写「抽样未取到内容」，实测取到 `mods/agents-md/README.md:3` 与仓根 `CLAUDE.md:3`。

**没有改动的 10 维**：D02/D04/D07/D09/D10/D11/D13/D17 维持 `partial`；D05/D15 维持 `unknown`（本 run 的抽样同样 0 命中，按框架 A2 不足以判 `none`）。**降级 0 格**：本 run 没有推翻快照的任何一条 `partial`。

## 偏离说明：为什么是 17 行而不是 68 行

本文件此前的形态是**四张表 × 17 行 = 68 个 D 行**（四桶：`敌有我无 / 敌强我弱 / 我有敌无 / 平手`）。
那个形态有两处硬伤，都是可机检的：

1. **四桶不是全函数**：`我方 full` 对 `对方 partial` —— 正是本文件 13 维的取值 —— 在四桶下
   **没有落点**。作者手上不存在"没有家"的行，于是把整张 17 维表复制进每个桶，68 = 17 × 4。
   互斥失效的直接后果已经写在同一份文件的旧文里：`我方=none` 的行跑到「我有敌无（我方有）」下面去了。
2. **判定列手写就会与等级矛盾**：旧文件把 `full` 对 `partial` 的 14 维写成 `平手`，
   而 `gapDocVerdictFor` 对那一格算出来是 `我强敌弱`；D01 写成 `敌有我无且敌强我弱`，
   那个词根本不在六个判定里。门禁对这两处都会点名「判定列写的是 X，而 (我方=…, 对方=…) 机械判定为 Y」。

所以骨架换了，读数一个没动：**等级列、证据列逐字是旧文件的内容**，只有「判定」列改由函数算出，
形式从四副本收成「一张 17 行主表 + 一节六桶视图」——视图不是副本，一维在全文里只完整出现一次。
契约原文在 `devdoc/competitor-analysis-framework.md` §7.3.1，机检在
`internal/standards/gapdoc_contract_gate.go`。

## 判定列的机械修正（写清，不掩盖）

历史四表的判定列是手写的，与机械函数不一致的维逐对列在下面。**等级一格未改**，只把判定列按 `gapDocVerdictFor(我方,对方)` 改写。

| 等级对（我方 / 对方） | 历史判定 → 机械判定 | 维数 | 维度 |
|---|---|---|---|
| `none` / `partial` | `敌有我无且敌强我弱` → `敌有我无` | 1 | D01 |
| `full` / `partial` | `平手` → `我强敌弱` | 13 | D02, D03, D04, D06, D08, D09, D10, D11, D12, D13, D14, D16, D17 |

## 结论

1. **D01 是唯一的「敌有我无」**：我方 10 份 `-h` 的 1,504 行语料对安装词正则命中 0（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D01-D13.txt`），对方 `claude_code.md:18` 给了 ``npm install -g @anthropic-ai/claude-code``、`:24` 给了验证命令 ``claude --version``。我方拿到二进制只能自行 `go build`（`Makefile:35` 的 `build` 目标）。**`敌强我弱` 是空桶**（对方上限 `partial`，见桶视图小节）；历史手写的「敌有我无且敌强我弱」里后半个词不在六判定中，已按机械函数收成 `敌有我无`。
2. **D02 我方 `full`、对方 `partial`，机械判定为 `我强敌弱`，但证据方向相反**：我方在私有 profile 上 `gopi config setup` 得 `nothing to do — the profile is run-ready`（`/home/user/.gopi/sessions/01a1acc7-7a03-7e47-b347-9388a14bb18a/artifacts/gap/e3-D09-D02-sandbox-setup.txt`）；对方 `claude_code.md:43` 的首跑动作是 **If the file does not exist, create it.** 即手写 `settings.json` 并填 key。判定列由 `(我方, 对方)` 唯一决定，不因这条反向差异改写；差异写在本条，等级一格未动。
3. **D11 的 `partial` 是证据上限，不是能力上限**：对方 `claude_code.md:107` 只有 IDE 里点图标的说明；全树 1,473 个 blob 的路径对 `click|mouse|browser|playwright|screenshot` 命中 0。本表不据此判 `none`，因为 E1 有一处话题命中，`none` 需要拒绝输出或双零（框架 A2）。
4. **`我强敌弱` 里的 13 格绝大多数是「档位不同、方向未判」**：对方上限 `partial` 由「不安装、不运行第三方 agent」机械决定，我方 `full` 来自本 run 亲自跑的 12 条 E3。`full` 对 `partial` 的差首先是**证据通道差**，不是能力差 —— 本文件不给综合分，也不把等级差读成强弱。
5. **两格 `不可比` 都是抽样不足，不是对方不行**：D05 与 D15 在 `claude_code.md` 上探针 0 命中，E2 只抽读了 8 个文件；全树 1,473 个 blob 未逐文件读，按框架 A2 只能记 `unknown`。要变成 `none` 需要一次全树逐文件取文。
6. **对方证据最硬的三处，都是固定 sha 上可点开的实现/配置点**：D08 的 `CLAUDE.md:19-22`（`--permission-mode auto`、`--disallowedTools`、安全复审才放行）、D07 的 `.claude-plugin/marketplace.json:3`（插件市场）、D12 的 `examples/gateway/{aws,gcp}/`（部署网关样例）。这三处是我方若要读对方设计时该先读的地方。
7. **我方证据强在「可对账」而不是「功能更多」**：D03/D05 的 `OK` 在 `e3-D03-out.txt:15`、D04 的 `api_key <set: 35 chars>` 在 `e3-D04-config.txt:14`、D14 的 `9833 tokens (31149 bytes, 424 lines, deepseek-v4.1-bpe)` 在 `e3-D14-tokens.txt`、D11 的 sha 由 `b29a050f2f1ab25e` 变为 `86f4fce933a2c236`、D12 的 `http=200 bytes=69870`。每条都是「命令 + 退出码 + 原始输出」。
8. **本文件的边界（写清，不掩盖）**：对方无 E3，主表 17 行里对方 `full` 结构性不存在；D12 与 D10 的对方格依赖 `examples/gateway/*/README.md` 与 `mods/agents-md/README.md`，前者两次 `curl` 超时未取到；`counts_as_of` 之后任一方变动，本表即失效。
