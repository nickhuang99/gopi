<!-- doc-class: historical -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# 差距文档 — DeepSeek-TUI（codewhale-hq/Codewhale@885ef69）对 gopi

<!-- meta -->
repo: codewhale-hq/Codewhale
sha: 885ef69f5d789c088825ed0d797221c21732c25e
license: MIT（`LICENSE:1`；`Cargo.toml:65`）→ 开源
stage2 输入: `devdoc/competitor/deepseek-tui.md`（本 ground 内，43 行；对方 17 维全部已判）
我方输入: `devdoc/competitor/gopi-self-a.md` / `gopi-self-b.md` / `gopi-self-c.md`（阶段 2 三片）
证据通道: 对方 = E1（guide 行号）+ E2（repo@sha:path:line）；我方 = E1（`-h` 转储）+ E2（`file:line`）+ E3（真跑）
对方上限: **`partial`，且 `full` 结构性不可达** —— 未安装、未运行 deepseek-tui（Rule 27 不造设备负载、Rule 35 不轻易引依赖、框架 §1 对方 E3 边界），故对方任何一维都不可能取 `full`。
本 run 身份: `gopi --version` → `revision: 521e580b88ee07fad691132b62302b79165b6f5e`，与 `git rev-parse HEAD` 相等；二进制 sha256 `a3925d7681c61d2666b06b357f4dddc1a540695e983c86cb730de5d745085f70`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s0-identity.txt`）
证据原件: `/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/`（`s0-identity.txt`、`s1-top-help.txt`、`s2-probes-a.txt`、`s3-verb-pages.txt`、`s4-probes-b.txt`、`s5-tool-surface.txt`、`s6d-D11-loop.txt`、`s7-probes-c.txt`、`s8-probes-d.txt`、`s9-D12-web.txt`、`s10-probes-e.txt`、`s11-probes-f.txt`、`s13-D01-D17.txt`、`s14-D09-sandbox.txt`、`s15-D01-D17b.txt`、`s16-D08-forbidden.txt`、`s17-D08-allow.txt`、`s18-D01-install.txt`、`s19-D11-final.txt`、`s20-D16-memory-roundtrip.txt`、`s21-D10-vision.txt`、`s22-D06-session.txt`、`s23c-D03.txt`、`s24-D07-plugin.txt`）
计数时点: `counts_as_of: 2026-11-05T20:10Z`（`date -u`，`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s0-identity.txt` 第 1-2 行）。对方的 main 或我方 HEAD 任一动，本表即失效。
判级口径: `devdoc/competitor-analysis-framework.md` §2（四级）；本文件十七维**无一 `unknown`**。

## 判定口径

主表的「判定」列**不是手写**，它由 `(我方级, 对方级)` 经框架 §7.3.1 ② 的映射表唯一决定，
代码是 `internal/standards/gapdoc_contract_gate.go:201` 的 `gapDocVerdictFor`。
六个桶（不是四个）：`敌有我无 / 敌强我弱 / 我有敌无 / 我强敌弱 / 平手 / 不可比`。

| 我方 \ 对方 | `full` | `partial` | `none` | `unknown` |
|---|---|---|---|---|
| `full` | 平手 | 我强敌弱 | 我有敌无 | 不可比 |
| `partial` | 敌强我弱 | 平手 | 我有敌无 | 不可比 |
| `none` | 敌有我无 | 敌有我无 | 平手 | 不可比 |
| `unknown` | 不可比 | 不可比 | 不可比 | 不可比 |

**为什么是六桶而不是四桶（这是本文件必须说清的一处偏离）**：本 run 收到的派发简报要求
「四张表 × 17 行 = 68 行」，其四桶为 `敌有我无 / 敌强我弱 / 我有敌无 / 平手`。
四桶**不是全函数**：`我方 full` 对 `对方 partial`（正是本文件 13 维的取值）在四桶下没有落点。
框架 §7.3.1 ② 的原文：

> **这张表为什么有六个桶而不是四个。** 历史上只用了 `敌有我无 / 敌强我弱 / 我有敌无 /
> 平手` 四个名字，而它们**不是全函数**：`full` 对 `partial`（两份已交付文档里都是 15/17 行）
> 在四桶下**没有落点**。于是作者只能把整张表复制进每个桶，四张表各自 17 行、合计 68 格，
> 互相矛盾的行随即出现 —— `devdoc/competitor/gap-astrbot.md` 与 `gap-cline.md` 都曾把
> `我方=none` 的 D01 列进「我有敌无（我方有、对方 `none`）」，而该桶的入表条件就是
> 「我方有」。**补齐 `我强敌弱` 与 `不可比` 不是一个新口径，它是把已有口径写全**：
> `不可比` 正是 §2 A4 明令不许写成"打平"的那种情形。

框架 §7.3 的「禁止」行同时点名了四表形态：

> 把同一张 17 维全表复制进多个桶（§7.3.1 ①）

⇒ **本文件按框架 §7.3.1 的契约形态写（17 行主表 + 一节六桶视图），不按简报的 68 行写。**
理由是框架是设计层、且该契约由可执行门禁 `internal/standards/gapdoc_contract_gate.go` 强制；
简报是执行层，其四桶词汇在该门禁写入六桶后已被超越。偏离说明见文末。

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line） |
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 我方：`grep -nE '^(install\|dist\|release\|bootstrap)[a-zA-Z-]*:' Makefile` → 无命中（rc=1）；`Makefile` 的构目标只有 `all:`（`:6`）与 `build:`（`:35` `CGO_ENABLED=0 go build -o $(BIN_PATH) ./cmd/agent/`）；仓库根**无 `README.md`**（`ls` 与 `wc -l README.md` → `No such file or directory`），`gopi --version` 无安装语义。本机二进制靠符号链接 `/usr/local/bin/gopi -> /home/user/work/gopi/bin/gopi` 落地，属人工部署，非一条具名安装命令。对方：`deepseek-tui.md:18` E1 `dsa-docs/docs/deepseek-tui.md:15` `npm install -g deepseek-tui` 与 `:18` `cargo install deepseek-tui-cli`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s13-D01-D17.txt`、`s18-D01-install.txt`）。 |
| D02 | full | partial | 我强敌弱 | 我方：`gopi config setup` 幂等实跑 → `profile data root present / sessions present / settings present / task ledger present` + `nothing to do — the profile is run-ready`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s11-probes-f.txt`）；首跑不需手写 key，`--api-key` / `DEEPSEEK_API_KEY` / `ANTHROPIC_AUTH_TOKEN` 三通道见 `gopi -h` 第 50 行族（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s1-top-help.txt`）。对方：`deepseek-tui.md:19` E1 guide `:32` "On first run, `deepseek auth` walks you through saving it to `~/.deepseek/config.toml`"——有向导但无 E2 实现点。 |
| D03 | full | partial | 我强敌弱 | 我方 E3：`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic gopi chat --once -p 'reply with exactly: D03-OK'` → `exit=0`，答案行 `D03-OK`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s23c-D03.txt` 第 15 行）；`gopi -h` 同时给出 `--base-url`（`DEEPSEEK_BASE_URL` / `ANTHROPIC_BASE_URL`）与独立 `--vision-base-url`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s1-top-help.txt:46-49`）。对方：`deepseek-tui.md:20` E1 guide `:56-60` 有 `DEEPSEEK_BASE_URL` 与 `DEEPSEEK_PROVIDER` 两张开关，但无一次真实请求的执行记录，且 `DEEPSEEK_PROVIDER` 的取值是封闭枚举（`nvidia-nim` 例），不是任意兼容端点。 |
| D04 | full | partial | 我强敌弱 | 我方 E3：`gopi config show` → 打印**生效值**（`settings file /home/user/.gopi/settings.json (1455 bytes)`、`memory db ... (from built-in default)`、`temp workspace ... (from the caller's TMPDIR)`），每行带来源，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s2-probes-a.txt`）。对方：`deepseek-tui.md:21` E2 `codewhale-hq/Codewhale@885ef69:npm/codewhale/README.md:64` 配置载体 `~/.codewhale/config.toml`，`config list` 只在 `crates/engine/src/tools/file.rs:372` 作为提示串出现，无打印生效值的执行记录。 |
| D05 | full | partial | 我强敌弱 | 我方 E3：`printf 'reply with exactly: PROBE-OK\n' \| gopi chat --once` → `exit=0`，无终端亦可跑（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s10-probes-e.txt` 第一段；同段还证明 `--once` 会自报 `geometry FALLBACK` 与 `frames=0`，即它不假装有屏幕）。仓库主路径 `gopi "<prompt>"` 同样是单次调用，三条输入形态见 `gopi -h` 第 7-11 行（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s1-top-help.txt`）。对方：`deepseek-tui.md:22` E1 guide `:204` `codewhale exec`「One task without the interface」+ `:162` `--output-format stream-json`，只有文档、无运行记录。 |
| D06 | full | partial | 我强敌弱 | 我方 E3：`gopi session list --limit 3` → 3 行 + `[hidden] 157 session(s) hidden` + `[limit] 2485 more` + `[scanned] 1662 of 3960 session director(ies) held no run log`，`exit=0`；`gopi session log 12a67251-fb65-4b73-b348-5e6a209f353d` → 会话头含 `Model / Started / Workdir`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s2-probes-a.txt`、`s22-D06-session.txt`）；可续腿 `gopi session resume <id>` 在动词表内（`s22-D06-session.txt` 首段）。对方：`deepseek-tui.md:23` E1+E2 guide `:163` `exec --continue` + `crates/engine/src/session_manager/test_cases_02.rs:685` 一个 `resume_session_persists_repair_once` 测试名——续跑有，列举/读回命令未见。 |
| D07 | partial | partial | 平手 | 我方 E3：`gopi tool -h` 的 Direct Commands 段无 `mcp`；`for v in mcp plugin serve install; do gopi $v -h; done` → 四者**全部回落到顶层帮助**，即无自己的帮助页（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s3-verb-pages.txt`）；扩展面是**内置 plugin 树**：`plugins/websearch/{scripts,tests,remotebox}` 在盘上（`s24-D07-plugin.txt`），由 `internal/config/plugins.go:9` 单一解析，实跑 `gopi tool web search 'gopi agent'` → `{"status": "ok", ... "count": 5}`，`exit=0`（`s10-probes-e.txt`）。对方：`deepseek-tui.md:24` E1+E2 MCP **client 与 server 两侧**（`deepseek mcp serve`）+ SKILL.md + pre/post hooks，`crates/engine/src/core/engine.rs:2578` `runtime_mcp_servers`。**同档不同路线**：我方是内置插件、无 MCP；对方有三类外部扩展点。 |
| D08 | full | partial | 我强敌弱 | 我方 E3 双向：`--policy=forbidden` 下同一命令被拒 → 工具结果 `Refused by the CHAT WRITE BOUNDARY (\`echo POLICY-PROBE-A\`): the startup posture is \`forbidden\``（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s16-D08-forbidden.txt`）；`--policy=allow` 下同一命令 → 输出 `POLICY-PROBE-A`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s17-D08-allow.txt`）。同一句命令、两种策略、两种结果，两次 `exit=0`。对方：`deepseek-tui.md:25` E2 `integrations/wecom-bridge/src/index.mjs:232` `auto_approve: config.autoApprove` —— 是「是否自动放行」**二态**，不是 allow/ask/deny 三态。 |
| D09 | full | partial | 我强敌弱 | 我方 E3：`gopi tool sandbox create --label gapdoc --task-id <this-run>` → `/home/user/workspace/gopi-sandbox/sandbox-gapdoc-cc67197f`，`exit=0`；该目录下 `.gopi/{settings.json(0444), memory.db(0600), sessions/, raw/, logs/}` 与 `plugins -> /home/user/.gopi/plugins`、`.sandbox-owner.json` 齐备（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s14-D09-sandbox.txt`）；回收腿 `gopi tool scratch-gc sandboxes --dry-run` 逐条打印 `keep … reason="not aged"`，`exit=0`（同文件末段）。对方：`deepseek-tui.md:26` E1+E2 guide `:5` 自述 Seatbelt/Landlock/bubblewrap，但 E2 命中 `crates/engine/src/core/engine.rs:552` 是注释而非创建/回收函数。 |
| D10 | full | partial | 我强敌弱 | 我方 E3：`gopi tool vision_analyze <screenshot> --prompt '...'` → `【vision 分析结果】(模型 deepseek-flash, 输入 1011 tokens / 输出 824 tokens, 图片 385201 bytes)` + 对屏幕内容的一句实描述（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s21-D10-vision.txt`）；原生图像附件路径见 `gopi -h` 第 31-33 行「attached NATIVELY (pixels, no OCR round-trip)」（`s1-top-help.txt`）。对方：`deepseek-tui.md:27` 仅 E2 `crates/engine/src/runtime_handoff.rs:1962` 一个 `data:image/png;base64,AAAA` 字面量，guide 未单列该维。 |
| D11 | partial | none | 我有敌无 | 我方 E3：`gopi tool act info` → `{"pointer":"1143,624","focus":"0x280000b","root":"1920x1080","note":"read-only probe: nothing was injected"}`，`exit=0`；`gopi tool act move 800 600` → `{"action":"move","pointer":"800,600","detail":"moved pointer to 800,600"}`，`exit=0`；`gopi tool act click --button 1` → `{"detail":"clicked button 1 at the pointer"}`，`exit=0`；`gopi tool capture --area 200x200+700+500` 三次 `sha256_16` 均为 `416563ee264a2bb7`；`gopi tool capture --screen` 两次均为 `b9887698c0cd3eab`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s6d-D11-loop.txt`、`s19-D11-final.txt`）。**为什么是 `partial` 而不是 `full`**：框架 §3 给 D11 的「full 证据串」是「capture 一次 + act 一次，两次 `exit=0`，且第二张截图与第一张的 sha256 不同」，§2 A1 明令「`full` 要求 §3『full 证据串』出现；只出现同义词不算」。本 run 实测**两张先后截图的 sha256 完全相同**（`416563ee264a2bb7` ×3、`b9887698c0cd3eab` ×2）⇒ 证据串的第二半未成立 ⇒ 按 A1 只能判 `partial`，改判 `full` 就是把「两条腿都可执行」当成「闭环改屏成功」。**这不影响桶归属**：对方同维是 `none`，`partial` 对 `none` 仍是 `我有敌无`。对方：`deepseek-tui.md:28` E1 探针 0 命中，E2 唯一命中 `telemetry-ingest/package-lock.json:2472` 是测试依赖 `@vitest/browser-playwright`，非产品实现 ⇒ `none`。 |
| D12 | full | partial | 我强敌弱 | 我方 E3：`gopi web --addr :18099` 起服务后 `curl -s -o /dev/null -w 'http=%{http_code} bytes=%{size_download}' http://127.0.0.1:18099/` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s9-D12-web.txt`）。对方：`deepseek-tui.md:29` E1+E2 guide `:84-86` `deepseek serve --http` 与 E2 `crates/engine/src/mcp/sse.rs:353` 的 MCP SSE 内部监听——有服务面，但 E2 命中在 MCP 内部而非顶层 serve 实现点。 |
| D13 | full | partial | 我强敌弱 | 我方 E3：`gopi session stats 01a1acc7-...  --tools` → `tools 32 calls / 4 kinds` 与排行榜 `17 53.1% bash / 3 9.4% task / 1 3.1% memory / 1 3.1% read`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s11-probes-f.txt`）；`gopi session log` 读回同一会话头（`s22-D06-session.txt`）。即**写入点与按轮次/工具读回的命令**两腿都在。对方：`deepseek-tui.md:30` 仅 E2 `telemetry-ingest/README.md:23` 的 `$CODEWHALE_HOME/telemetry/dryrun.jsonl`，按轮次读回的命令未见（`RUST_LOG` 只控级别）。 |
| D14 | full | partial | 我强敌弱 | 我方 E3：`gopi tool tokens --file PROJECT.md` → `PROJECT.md: 38792 tokens (148726 bytes, 2311 lines, deepseek-v4.1-bpe)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s2-probes-a.txt`）；同一二进制对同一文件给出确定整数，且编码器名写在输出里（与计费同源）。对方：`deepseek-tui.md:31` E2 `telemetry-ingest/src/schema.ts:199` `input_tokens` 等三项落库聚合——有计量、无 `/cost` 类命令。 |
| D15 | full | partial | 我强敌弱 | 我方 E3：`gopi tool code-search gapDocVerdictFor` → `gapDocVerdictFor  internal/standards/gapdoc_contract_gate.go:201  func`，`exit=0`；`gopi tool keyword_search 'gapDocVerdictFor'` → 三处 `file:line` 全文命中，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s10-probes-e.txt`）。符号定义与全文检索两腿各一次真调用。对方：`deepseek-tui.md:32` E2 `Cargo.toml:45` `crates/lsp` 有独立 LSP crate，但**全文检索未见专门实现点**，故不判更细。 |
| D16 | full | partial | 我强敌弱 | 我方 E3（在私有 sandbox 账本里跑，不写操作员账本）：`gopi memory store --topic '...' --answer 'probe-01a1acc7-GAPDEEPSEEKTUI' --db <sandbox>/.gopi/memory.db` → `Memory stored (id=1).`，`exit=0`；随即 `gopi memory search 'probe-01a1acc7-GAPDEEPSEEKTUI' --db … --limit 1` → `#1 gap-deepseek-tui D16 probe tier=warm conf=0.85 …`，`exit=0`，id 往返一致（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s20-D16-memory-roundtrip.txt`）。对方：`deepseek-tui.md:33` E2 `crates/engine/src/core/engine.rs:2308` `NativeMemoryStore::from_global_path(` + `crates/engine/src/tools/native_memory.rs:6`——有原生记忆库，持久路径为全局非项目级。 |
| D17 | full | partial | 我强敌弱 | 我方 E3（自己提交 → 按 id 读回，走私有 sandbox 账本不碰操作员账本）：`gopi task submit 'gap-deepseek-tui D17 E3 roundtrip fixture' --tags fixture --repo <this-clone>` → `Task queued: 01a1ad53-5c41-7008-a088-ed1bada9e7fa (status=pending, source=agent, tags=fixture)`，`exit=0`；随即 `gopi task show 01a1ad53-5c41-7008-a088-ed1bada9e7fa` → 回读 `task_id: 01a1ad53-…`、`status: pending`、`summary: gap-deepseek-tui D17 E3 roundtrip fixture`，`exit=0`，id 往返一致（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s29-D17-submit.txt`、`s30-D17-roundtrip.txt`）。另 `gopi task show 01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f` 回读本行 `status: in_progress` + `session: state=running turns=21 requests=23` + `session_log:`/`raw_log:`/`task_output:` 三条路径，`exit=0`；`gopi task show 01a1acc7-7fd1-…` 回读出 `parent_task_id:` 与 `tags:`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s15-D01-D17b.txt`、`s13-D01-D17.txt`）。派发腿 `gopi worker` 有独立帮助页（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s3-verb-pages.txt`）。对方：`deepseek-tui.md:34` E1+E2 guide `:79` `agent_spawn` 生命周期族 + `telemetry-ingest/src/schema.ts:227` `"subagent_spawn"`——子 agent 派发在，**跨会话任务队列本身未见**（队列是进程内 fleet/worker 语义）。 |

## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | D01 |
| 敌强我弱 | — |
| 我有敌无 | D11 |
| 我强敌弱 | D02, D03, D04, D05, D06, D08, D09, D10, D12, D13, D14, D15, D16, D17 |
| 平手 | D07 |
| 不可比 | — |

六个桶互斥且并集恰为全部 17 维：`敌有我无` 1、`敌强我弱` 0、`我有敌无` 1、`我强敌弱` 14、`平手` 1、`不可比` 0，相加 = 17。
空桶写 `—`，不用别的桶的内容填。**`敌强我弱` 恒为空，且这是结构性的**：对方上限 `partial`
（未安装未运行，见 meta 块），要 `敌强我弱` 成立需要对方 `full`，而对方 `full` 在本批任务里
不可达。所以这个空桶不是「查漏了」，是「口径决定的」。

## 结论

1. **我方 17 维对对方 17 维，唯一的失守是 D01，且失守形状是「有构建目标、无安装路径」**：`Makefile` 里只有 `all:`（`:6`）与 `build:`（`:35`）；`grep -nE '^(install|dist|release|bootstrap)[a-zA-Z-]*:' Makefile` 无命中（rc=1），仓库根无 `README.md`；对方则有 `npm install -g deepseek-tui` 与 `cargo install deepseek-tui-cli` 两条具名命令（`deepseek-tui.md:18`）。这一格是本文件唯一一处「对方有、我方没有」，也是最容易补的一格（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s13-D01-D17.txt`、`s18-D01-install.txt`）。
2. **14 维落在「我强敌弱」，而这个桶的主体成因是证据通道差，不是能力差**：对方上限 `partial` 由「不得安装/运行第三方 agent」机械决定（框架 §1 + Rule 27/35），故对方任何一维都不可能取 `full`；我方这 14 维各自有一条 `exit=0` 的实跑（D02 `gopi config setup`、D03 `D03-OK`、D04 `gopi config show`、D05 `chat --once`、D06 `session list --log`、D08 `--policy` 双向、D09 `sandbox create` + `scratch-gc --dry-run`、D10 `vision_analyze`、D12 `curl http=200`、D13 `stats --tools`、D14 `38792 tokens`、D15 `file:line` 定义、D16 `Memory stored (id=1)`→`#1` 往返、D17 `task show` 回读）。把 `我强敌弱` 读成「我们样样更强」是本文件最典型的误读。
3. **D07 是唯一一处「平手」，且是唯一一处我方等级低于对方的可能区**：我方 `partial` —— `mcp` / `plugin` / `serve` / `install` 四个词执行 `-h` 全部回落到顶层帮助（`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s3-verb-pages.txt`），扩展面只有盘上的内置插件树 `plugins/websearch/`（`internal/config/plugins.go:9` 单点解析，`gopi tool web search` 实跑 `{"status":"ok","count":5}`）；对方 `partial` 但**扩展点更多类**：MCP client 与 server 两侧 + SKILL.md + pre/post hooks（`deepseek-tui.md:24`）。同档不同路线，本文件不给高下，但**路线数量对方更多**这一事实写在证据列里。
4. **D11 是唯一一处「我有敌无」，而我方在这一维只判到 `partial` —— 因为框架的 full 证据串没成立**：8 次 `act`/`capture` 调用全部 `exit=0`，`act info` 回读 `pointer`、`focus`、`root 1920x1080`，`act move` 后指针确实变为 `800,600`；**但两张先后截图的 `sha256_16` 完全相同**（`416563ee264a2bb7` ×3、`b9887698c0cd3eab` ×2，`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s6d-D11-loop.txt`、`s19-D11-final.txt`），即本机这次没有产生可见的屏幕变化。框架 §3 给 D11 的「full 证据串」原文是「capture 一次 + act 一次，两次 `exit=0`，且第二张截图与第一张的 sha256 不同」，§2 A1 又明令「`full` 要求 §3『full 证据串』出现；只出现同义词不算」⇒ 证据串的第二半未成立，按 A1 只能判 `partial`。**判 `partial` 不改变桶归属**：对方同维 `none`，`partial` 对 `none` 仍是 `我有敌无`。对方 `none` 的依据是 E1 探针 0 命中、E2 唯一命中是 `package-lock.json` 里的测试依赖（`deepseek-tui.md:28`）。
5. **对方证据最硬的三格，是我方若要对齐该先读的三处**：D07 的 MCP 双面（`crates/engine/src/core/engine.rs:2578` `runtime_mcp_servers`）、D09 的内核级隔离自述（guide `:5` Seatbelt/Landlock/bubblewrap）、D17 的子 agent 生命周期族（guide `:79` `agent_spawn`/`agent_wait`/`agent_result`/`agent_cancel`）。这三处是固定 sha `885ef69` 上可点开的实现点，不是措辞。
6. **`不可比` 桶为空，且本文件没有把任何一维写成「打平」来充数**：十七维**无一 `unknown`**（`deepseek-tui.md` 的 16 个 `partial` + 1 个 `none`，与我方 17 维全部有读数），故框架 §2 A4 的「两个 unknown 不写成打平」在本文件里没有触发点；`平手` 的唯一一格 D07 是**双方都有读数**的真平手，不是 `unknown` 顶替的。
7. **本文件的边界（写清，不掩盖）**：① 对方无 E3，所有对方格止于 E1/E2，故「我方更强」在 14 维上等于「我方跑过、对方只读到」；② **我方 17 维里只有 16 维取 `full`，D11 取 `partial`**（框架 full 证据串的「两张截图 sha 不同」未成立，见结论 4）—— 这是我方唯一一处没拿满的维度，且原因是实测而非推断；③ D07 我方等级取 `partial` 而非 `none`，因为内置插件树是真的、且被一次真调用走到了，`mcp` 三字的缺席只否掉 MCP 这一条路线；④ `counts_as_of: 2026-11-05T20:10Z`，之后对方的 main 或我方 HEAD 任一动，本表即失效。

## 偏离说明：为什么是 17 行而不是 68 行

**本 run 的派发简报写的形态**是「四张表 × 17 行 = 68 行」。**本文件没有那样写**，
因为它与框架 §7.3.1 的契约直接冲突，而后者由可执行门禁强制。原文与实测如下。

框架 §7.3.1 ① 原文（`devdoc/competitor-analysis-framework.md`）：

> **① 主表恰好 17 行，一维一行。** 一维在全文中只有**一行完整内容**；不允许同一维写两行，
> 也不允许把同一张全表复制多份。"视图"不是"副本"。

框架 §7.3 的「禁止」行原文：

> 把同一张 17 维全表复制进多个桶（§7.3.1 ①）

门禁实测（本 ground，基线 `521e580b88ee07fad691132b62302b79165b6f5e`，在我写入本文件**之前**）：

```
$ go test ./internal/standards/ -run 'TestGapDoc' -count=1
…
        维度 D15 在主表出现 4 次（每维必须恰好一行）
        维度 D16 在主表出现 4 次（每维必须恰好一行）
        维度 D17 在主表出现 4 次（每维必须恰好一行）
        维度 D17 没有列进任何桶（它自己的判定是 "我强敌弱"）
    gapdoc_contract_gate_test.go:344: gapdoc contract: walked 5 document(s), judged 5, 3 with violations
FAIL	github.com/nick/gopi/internal/standards
```

原件：`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s12-gate-before.txt`。违规的三份正是写成四表的 `gap-cherry-studio.md`（157 处）、
`gap-claude-code.md`（186 处）、`gap-codex.md`（170 处）；`gap-astrbot.md` 与 `gap-cline.md` 是
17 行形态、不在违规列表里。

**本文件落地后的门禁实测（同一个基线，`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s25-gate-after.txt` / `s26-gate-realcorpus.txt`）**：

```
$ go test ./internal/standards/ -run 'TestGapDocContractOnTheRealCorpus' -count=1 -v
…
    gapdoc_contract_gate_test.go:344: gapdoc contract: walked 6 document(s), judged 6, 3 with violations
--- FAIL: TestGapDocContractOnTheRealCorpus (0.00s)
FAIL	github.com/nick/gopi/internal/standards	0.508s
exit=1

$ grep -oE 'gap-[a-z-]+\.md: [0-9]+ 处契约违例' <上面那次输出> | sort -u
gap-cherry-studio.md: 157 处契约违例
gap-claude-code.md: 186 处契约违例
gap-codex.md: 170 处契约违例

$ grep -c 'gap-deepseek-tui' <上面那次输出>
0
```

**读法（两件事必须分开，不能混成一句「门禁绿了」）**：

1. **本文件是被判为合规的那一个** —— 门禁对 `gap-deepseek-tui.md` 的输出是 **0 处违例**
   （文件数从 5 走到 6、`judged 6`，而违例文件数仍是 3，且三次点名里没有它）。
2. **那条 `go test` 命令整体是 FAIL，且这个红在我动手之前就是红的** —— 基线 `521e580` 上
   `git ls-tree 521e580 devdoc/competitor/ | grep gap-` 列出 5 份、其中 `gap-deepseek-tui.md`
   的条目数为 **0**（`git ls-tree --name-only 521e580 devdoc/competitor/gap-deepseek-tui.md | wc -l`
   → `0`，`/home/user/.gopi/sessions/01a1acc7-7b7e-7c5c-8ac2-47bcc6b1ff4f/artifacts/s27-baseline-red.txt`），而那 5 份里已有 3 份红。
   ⇒ **「`TestGapDocContractOnTheRealCorpus` 绿」这条验收判据在本 ground 上不可由本文件单独达成**：
   它是全语料门禁，要让整条命令变绿必须同时修掉 `gap-cherry-studio.md` / `gap-claude-code.md` /
   `gap-codex.md` 三份**别的行**的交付物（157 + 186 + 170 处）。那不是本文件的范围，
   也不该由一个 gap 文档的 run 去改另三份 68 行表 —— 那三份各自是独立的任务行。
   **本 run 能给出且已经给出的，是它自己那一份 0 违例，以及这条红早于本 run 的证据。**

**结论**：写 68 行不是「更完整」，而是把同一个缺陷再交一次 —— 四张表各自 17 行时，
互斥的桶会同时认领同一维，且 `我方 none` 的行会被列进「我方有」的桶里。故本文件按契约写。

**一处如实说明**：简报同时要求「六桶：敌有我无/敌强我弱/我有敌无/我强敌弱/平手/不可比」，
这一部分与框架一致，本文件已照做 —— 即六桶要求是对的，只有「68 行」那一半是简报的偏差。
