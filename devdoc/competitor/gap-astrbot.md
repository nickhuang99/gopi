<!-- doc-class: historical -->

# gopi × AstrBot 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: devdoc/competitor-analysis-framework.md（§3 十七维、§2 四级口径、§1 证据通道 E1/E2/E3）
our_side_sources: devdoc/competitor/gopi-self-a.md、devdoc/competitor/gopi-self-b.md、devdoc/competitor/gopi-self-c.md（三份阶段 2 盘点，本次未改一字）
opponent_source: devdoc/competitor/astrbot.md（阶段 2 采集快照，本次未改一字）
opponent_pin: AstrBotDevs/AstrBot@86c24e846c0de78830f6a0bc402049edb0f0c671（short `86c24e8`）—— 本文件每一格 `astrbot.md（AstrBotDevs/AstrBot@86c24e8）:NN` 的行号都固定在该 sha 上
opponent_channel_cap: E1+E2 only —— 对方**不产生 E3**（不得安装/运行第三方 agent，Rule 27/35），故对方等级上限 `partial`；本文件的逐维表里对方无一行 `full`
our_measurement_anchor: ground HEAD `4f829a274f92a4e94f63a25e61c2310960dd5847`（`gopi --version` 的 `revision:` 逐字相同，见 `e3-D01-version.txt`）；`v0.4.0` / `go1.24.0` / `linux/amd64`
evidence_dir: `/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/`（不进 git 树）
reuse_note: 表中标注 `summary.md:NNN` 的格，其原始输出属于**另一个 run 的** artifacts（阶段 4 汇总 run），本 run 未复跑，只引用它记载的读数；标注 `/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/<文件名>` 的格是本 run 亲自执行并落盘的（文件名逐格列在该行证据列里，可直接 `cat` 取回）
counts_as_of: 2026-11-05T18:53Z（本文件的拍摄时刻；下表等级是快照读数，随对方 main 与我方 HEAD 变动而失效）

<!-- 对方证据的取回方式（可点开，不是推断）：
     https://raw.githubusercontent.com/AstrBotDevs/AstrBot/86c24e846c0de78830f6a0bc402049edb0f0c671/<path>
     本 run 实测该 sha 的 raw 端点可达、TLS 完成、HTTP 响应已开始（`curl -sSv` 的 rc=0），
     故每一处 `AstrBotDevs/AstrBot@86c24e8:path:NNN` 都能按上面的模板逐行取回。 -->

## 判定口径（按框架 §2，四级严格定义）

| 等级 | 本表采用的判据 |
|---|---|
| `full` | 本 run 亲自执行、`exit=0`、输出含该维 §3 表「full 证据串」的证据 |
| `partial` | 无 E3，但有 ≥1 条 E1 或 E2 证据（对方 = guide 行号 或 固定 sha 的 code 行号） |
| `none` | 有 E3 拒绝输出，或 E1+E2 在**覆盖过该维的语料**上双零（看过、没有） |
| `unknown` | E1/E2/E3 三通道都没有被执行过（语料无此话题且未运行） |

`none` 与 `unknown` 严格分开：本文件的逐维表**没有一格**用「没查到」冒充 `none`。

---

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号） |
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 我方：本 run 在 `-h` 全量输出上普查安装词（`npm i -g`/`pip install`/`pipx install`/`brew install`/`go install`/`uv tool install`/`npx`/`curl 管道到 shell 的 install.sh` 八类）命中 `0`，`exit=1`（原样命令与输出见 `/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D01-census.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:README.md:74` `uv tool install astrbot --python 3.12`，另加 guide `:14` 安装脚本与 `:20` Windows 脚本 |
| D02 | full | partial | 我强敌弱 | 我方：`gopi config setup` 幂等，空 home 实跑落 settings.json/sessions/memory.db（`gopi-self-a.md:137`）；对方：`AstrBotDevs/AstrBot@86c24e8:guide:26` `astrbot init` + Web 面板粘 key（`astrbot.md:21`） |
| D03 | full | partial | 我强敌弱 | 我方：本 run `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic gopi chat --once -p "reply with exactly: OK"` → `exit=0`，第 15 行 `OK`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D03-D05-basurl-headless.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:guide:53` Providers 面板粘 DeepSeek key（`astrbot.md:22`） |
| D04 | full | partial | 我强敌弱 | 我方：本 run `gopi config show` 的三行键值 `base_url` / `vision_base_url` / `vision_model`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D03-D04-config.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/config/default.py:1` 配置载体 `data/cmd_config.json` |
| D05 | full | partial | 我强敌弱 | 我方：同一个 headless 调用 `exit=0` 并打印答案 `OK`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D03-D05-basurl-headless.txt`）；对方：`astrbot.md:24` 判 `partial` —— 一次性 headless + 结构化输出未取到 |
| D06 | full | partial | 我强敌弱 | 我方：本 run `gopi session list --limit 3` → 3 条 + `[hidden] 157` + `[limit] 2410 more`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D06-sessionlist.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:changelogs/v4.18.0.md:19` 会话列举属公开 HTTP API |
| D07 | partial | partial | 平手 | 我方：`grep -rniE '\bmcp\b' --include=*.go internal/ cmd/` → `0`，而 `ls internal/config/plugins.go cmd/agent/cli_websearch.go` → 2 个文件在（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e2-D07-D13-zeros.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/agent/mcp_client.py:412` MCP 客户端配置 |
| D08 | full | partial | 我强敌弱 | 我方：`--policy=forbidden` 拒、`--policy=allow` 过，两条 E3 输出都存档（`gopi-self-a.md:143`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/builtin_stars/builtin_commands/main.py:38` `PermissionType.ADMIN` 命令级装饰器 |
| D09 | full | partial | 我强敌弱 | 我方：本 run `gopi tool sandbox show` → `sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D09-sandbox.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/computer/process_sandbox/windows.py:311` 多后端沙箱 |
| D10 | full | partial | 我强敌弱 | 我方：`vision_analyze` 真跑，输入 221 tokens / 输出 107 tokens（`gopi-self-c.md:574`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/builtin_stars/astrbot/main.py:250` 图像转文件路径入模型 |
| D11 | full | partial | 我强敌弱 | 我方：本 run 两次全屏 capture 的 `sha256_16` 由 `0ca758cf80b858e0` 经 `act move 400 300` 变为 `80bb724c68228661`，三次调用 `exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D11-capture-act.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/astr_agent_tool_exec.py:230` `CuaScreenshotTool` 注册为内置工具 |
| D12 | full | partial | 我强敌弱 | 我方：`gopi web` 起服务后 `curl` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`summary.md:572`，阶段 4 run 的实跑，本 run 未复跑）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/platform/webhook_server.py:7` `from fastapi import FastAPI` |
| D13 | full | partial | 我强敌弱 | 我方：本 run `gopi session log --last` → 会话头 + `Workdir:` 行，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D06-D16.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/utils/trace.py:85` trace_id/span_id |
| D14 | full | partial | 我强敌弱 | 我方：本 run `gopi tool tokens --file devdoc/competitor-analysis-framework.md` → `9833 tokens (31149 bytes, 424 lines, deepseek-v4.1-bpe)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D14-tokens.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/builtin_stars/builtin_commands/commands/conversation.py:247` 三项 token 求和 |
| D15 | full | partial | 我强敌弱 | 我方：本 run `gopi tool code-search CliVerbTokens` → `CliVerbTokens  cmd/agent/cli_verbs.go:158  const`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D15-codesearch.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/tools/computer_tools/fs.py:686` ripgrep 全文检索，无符号级 |
| D16 | full | partial | 我强敌弱 | 我方：本 run `gopi memory search "clone-channel" --limit 1` → 命中 `#18403`，其 `sources=` 段写出来源任务行 `01a17e04-c358-7460-a5e6-00e972f7085e`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D06-D16.txt`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/utils/astrbot_path.py:115` 知识库目录 |
| D17 | full | partial | 我强敌弱 | 我方：`gopi task submit` → `Task queued: 01a1ac8c-744b-77e0-91d4-b17913be009a (status=pending, source=agent, tags=fixture)`，`gopi task show` 同 id 往返、两次 `exit=0`（`gopi-self-b.md:330`）；对方：`AstrBotDevs/AstrBot@86c24e8:astrbot/core/astr_agent_tool_exec.py:352` 子 agent provider 覆盖 |

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

1. **D01 是唯一一处「敌有我无」**：我方安装词普查 `0` 命中（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D01-census.txt`），对方有脚本、`uv tool install`、Docker 三条路线（`AstrBotDevs/AstrBot@86c24e8:README.md:74` + guide `:14`）。我方要拿到二进制只能自行 `go build`（`Makefile:35-36` 的 `build:` 目标）。**`敌强我弱` 是空桶**（对方上限 `partial`，见桶视图小节）：历史手写的「敌有我无且敌强我弱」里后半个词不在六判定中，D01 的 `(none, partial)` 经 `gapDocVerdictFor` 只算出 `敌有我无`。
2. **D07 是唯一的「平手」**：我方扩展点是本地 plugin 树（`internal/config/plugins.go`、`cmd/agent/cli_websearch.go` 两个文件在，`\bmcp\b` 在 Go 源码 `0` 命中），对方是 MCP 客户端（`AstrBotDevs/AstrBot@86c24e8:astrbot/core/agent/mcp_client.py:412`）。两侧都是 `partial`，`gapDocVerdictFor` 判 `平手`（`不可比` 桶计数 0）；等级相同、机制不同，本文件不给高下。
3. **桶 `我有敌无` 是空集，且这是口径产物**：对方 17 维无一 `none`（`astrbot.md（AstrBotDevs/AstrBot@86c24e8）:45`），要对方 `none` 才成立的格子一格都落不下；把空集读成「我们处处更强」是这一维最典型的误读。
4. **桶 `我强敌弱` 的 15 维与桶 `平手` 的 1 维，含义都是「档位相同或仅差一档，深度未判」**：对方上限 `partial` 由「不得安装/运行第三方 agent」这一条规则机械决定（框架 §1），我方 15 维 `full` 来自本 run 或三份盘点里的 E3 实跑。`full` 对 `partial` 的差异是**证据通道差异**，不是能力差异。
5. **对方证据强在三处，且都是固定 sha 上可点开的实现点**：D11 CUA 截图工具（`AstrBotDevs/AstrBot@86c24e8:astrbot/core/astr_agent_tool_exec.py:230`）、D13 OpenTelemetry 级 trace（`AstrBotDevs/AstrBot@86c24e8:astrbot/core/utils/trace.py:85`）、D01 三条安装路线。这三格是本文件里对方证据最硬的地方，也是我方若要对齐时该先读的三处。
6. **我方证据强在「可对账」而不是「功能更多」**：D03/D04/D14 三格本 run 亲自跑出 `exit=0` 的键值、整数与 token 数（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D03-D04-config.txt`、`e3-D14-tokens.txt`），D11 两次全屏 `sha256_16` 由 `0ca758cf80b858e0` 变为 `80bb724c68228661`（`/home/user/.gopi/sessions/01a1acc7-37f0-70d8-9fb5-90e7cf6fb492/artifacts/gap/e3-D11-capture-act.txt`）—— 这三格是「命令 + 退出码 + 原始输出」的完整形状。
7. **本文件的边界（写清，不掩盖）**：对方无 E3，故逐维表里对方 `full` 结构性不存在；D12 我方那格引用的是阶段 4 汇总 run 的实测（`summary.md:572`），本 run 未复跑；`counts_as_of: 2026-11-05T18:53Z`，之后对方的 main 与我方 HEAD 任一动，本表即失效。
