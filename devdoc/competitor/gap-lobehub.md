<!-- doc-class: historical -->

# gopi × LobeHub 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: devdoc/competitor-analysis-framework.md（§3 十七维、§2 四级口径、§1 证据通道 E1/E2/E3）
our_side_sources: devdoc/competitor/gopi-self-a.md、devdoc/competitor/gopi-self-b.md、devdoc/competitor/gopi-self-c.md（三份阶段 2 盘点，本次未改一字）
opponent_source: 阶段 1/2 **无 lobehub 采集文件**（`ls devdoc/competitor/` 32 个条目里不存在 lobehub.md），本文件的「补采集」两节就是它的替代采集；E1 = DSA guide 本地只读快照，E2 = 固定 sha 上的 code 探针
opponent_guide: `/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/lobehub.md`（92 行，本 run 只读复用，未改一字）
opponent_repo: lobehub/lobehub
opponent_sha: 658cdedf1e5e35eba161d3c54cd95461c28a4a54
opponent_sha_commit_time: 2026-11-03T13:53:07Z
opponent_sha_subject: `🚀 release: v2.3.1 (#22188)`
opponent_pin_verified_at: 2026-11-05T23:21:07Z（`curl https://api.github.com/repos/lobehub/lobehub/commits/main` 的 `sha` 字段原值，见 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/lobehub-main-commit.json` 与 `lobehub-probe/lobehub-main-sha.txt`）
opponent_pin_read_method: `https://api.github.com/repos/lobehub/lobehub/git/trees/<sha>?recursive=1` 返回 `truncated=false`，23,224 个 blob 路径全部落盘到 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/paths.txt`；每一处 `lobehub@658cded:path:NN` 都按 `https://raw.githubusercontent.com/lobehub/lobehub/658cdedf1e5e35eba161d3c54cd95461c28a4a54/<path>` 取回原文再读行号，原始文件落在 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/raw/`
opponent_channel_cap: E1+E2 only —— 对方**不产生 E3**（不得安装/运行第三方 agent，Rule 27/35），故对方等级上限 `partial`；本文件逐维表里对方无一行 `full`
our_measurement_anchor: 本 run 的 ground HEAD `2ba11b8039b7c50cc2ff04e224c8bdeb39a4fcdf`，二进制 `/usr/local/bin/gopi`（v0.4.0）；本文件里每一处「本 run 实跑」都是这个二进制在这个 ground 上执行、`exit=0`、输出落在 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/`
evidence_dir: `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/`（不进 git 树；`lobehub-probe/` 是对手侧，`our/` 是我方侧）
counts_as_of: 2026-11-05T23:21:07Z（本文件的拍摄时刻；对方等级是固定 sha 上的快照读数，我方等级是本 ground 上二进制实跑的读数，任一动本表即失效）

<!-- 对方证据的取回方式（可点开，不是推断）：
     https://raw.githubusercontent.com/lobehub/lobehub/658cdedf1e5e35eba161d3c54cd95461c28a4a54/<path>
     本 run 用这个模板成功取回 20 个文件（全部落在 /home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/raw/，行号就是从中读出的），
     所以每一处 `lobehub@658cded:<path>:<NN>` 都能按上面的模板逐行取回。 -->

## 补采集（阶段 2 缺口的替代，不做成 devdoc/competitor/lobehub.md）

阶段 1/2 没有给 lobehub 留采集文件，因此本文件先补两条证据通道，再出表：

| 通道 | 内容 | 落盘位置（不进 git 树） |
|---|---|---|
| E1（doc） | DSA guide 的 lobehub 快照，92 行，只读复用 | `/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/lobehub.md` |
| E2（code） | `lobehub/lobehub@658cded` 的路径普查（23,224 blob）+ 20 个原文文件的逐行读数 | `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/paths.txt`、`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/e2-path-census.txt`、`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/e2-lowercase-path-census.txt`、`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/raw/` |

E2 的路径普查脚本与它的原始输出同在 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/e2-path-census.txt`，按维分组的命中计数不可由本文件的叙述代替；完整穷举取回命令是
`curl -sS "https://api.github.com/repos/lobehub/lobehub/git/trees/658cdedf1e5e35eba161d3c54cd95461c28a4a54?recursive=1"`。

## 判定口径（按框架 §2，四级严格定义）

| 等级 | 本表采用的判据 |
|---|---|
| `full` | 本 run 亲自执行、`exit=0`、输出含该维 §3 表「full 证据串」的证据 |
| `partial` | 无 E3，但有 ≥1 条 E1 或 E2 证据（对方 = guide 行号 或 固定 sha 的 code 行号） |
| `none` | 有 E3 拒绝输出，或 E1+E2 在**覆盖过该维的语料**上双零（看过、没有） |
| `unknown` | E1/E2/E3 三通道都没有被执行过（语料无此话题且未运行） |

`none` 与 `unknown` 严格分开：本文件的逐维表**没有一格**用「没查到」冒充 `none`。

两处必须说清的边界：

1. **对方 `partial` 是规则产物，不是能力判断**。框架 §1 禁止为取证安装或运行第三方 agent（Rule 27/35），所以对方永远拿不到 E3，`full` 对它有结构性不可能。表里 15 行「我强敌弱」的差异是**证据通道差异**，不是「功能更多」。
2. **我方 `full` 按本 run 实跑算**，不引用别的 run 的读数。凡本 run 没跑的维（`D08` 的双向策略实测）沿用三份盘点的 E3 并把出处写进证据列，且明确标注它属于另一个测量锚点。

---

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line） |
|---|---|---|---|---|
| D01 | none | partial | 敌有我无 | 我方：`LC_ALL=C.UTF-8 grep -nEi 'npm (install\|i) -g\|pip3? install\|pipx install\|brew install\|go install\|uv tool install\|npx [a-z@/-]+\|curl .*install\.sh' README.md` 无输出 `exit=1`，且根目录无 `package.json`/`pyproject.toml`/`setup.py`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D01-ours.txt`），只有 `go.mod` + `Makefile`；对方：`lobehub@658cded:README.md:255` `bash <(curl -fsSL https://lobe.li/setup.sh)`，`README.md:261` `docker compose up -d`，`lobehub@658cded:apps/cli/package.json:5` `"bin": {"lh","lobe","lobehub"}`（npm 包 `@lobehub/cli` version `0.0.74`） |
| D02 | full | partial | 我强敌弱 | 我方：`gopi config setup` 幂等、空 home 实跑落 `settings.json`/`sessions/`/`memory.db`（`gopi-self-a.md:137`），同一空 home 直接 `gopi chat --once` 也不再需要前置（`gopi-self-a.md:139`）；对方：guide `lobehub.md:24` `Settings → Service Model` + `:32` `Paste your DeepSeek API Key`，自托管侧 `lobehub@658cded:.env.example:51` `OPENAI_API_KEY=sk-xxxxxxxxx` |
| D03 | full | partial | 我强敌弱 | 我方：本 run `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic /usr/local/bin/gopi chat --once -p "reply with exactly: BASEOK"` → 第 1 行 `BASEOK`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D05-D03-D01.txt`）；对方：guide `lobehub.md:75-76` `DEEPSEEK_API_KEY` / `DEEPSEEK_PROXY_URL=https://api.deepseek.com`，`lobehub@658cded:.env.example:54` `OPENAI_PROXY_URL=https://api.openai.com/v1` |
| D04 | full | partial | 我强敌弱 | 我方：本 run `gopi config show` → `settings file /home/user/.gopi/settings.json (1455 bytes)` 起头的键值段，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；对方：`lobehub@658cded:.env.example` 共 563 行环境变量表，CLI 侧 `lobehub@658cded:apps/cli/src/program.ts:127` `registerConfigCommand(program)` |
| D05 | full | partial | 我强敌弱 | 我方：本 run `gopi chat --once -p "reply with exactly: OK"` → 第 1 行 `OK`、`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D05-D03-D01.txt`）；对方：`lobehub@658cded:apps/cli/src/commands/task/index.ts:49` `.option('--json [fields]', 'Output JSON')` 是结构化一次性输出，但本 run 未运行对方二进制故只有 E2 |
| D06 | full | partial | 我强敌弱 | 我方：本 run `gopi session list --limit 3` → 3 条 + `[hidden] 157 session(s)` + `[limit] 2578 more` + `[scanned] 1653 of 4015`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；对方：`lobehub@658cded:apps/server/src/routers/lambda/topic/transcript.ts:26` `readTopicTranscript`（向导语写 `as lh topic view shows it`），CLI 侧 `apps/cli/src/commands/session-group.ts` |
| D07 | partial | partial | 平手 | 我方：`internal/config/plugins.go` 与 `cmd/agent/cli_websearch.go` 在、`plugins/websearch` 在，而 `grep -rliE '\bmcp\b\|model context protocol' --include=*.go internal/ cmd/` 命中 `0` 文件（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）——扩展点是本地插件树、没有 MCP；对方：`lobehub@658cded:apps/desktop/src/main/libs/mcp/client.ts:14` `StdioClientTransport`、`:387` `StreamableHTTPClientTransport`、`:364` `new Client(`，另 `apps/cli/src/program.ts:121` `registerPluginCommand(program)` |
| D08 | full | partial | 我强敌弱 | 我方：`--policy=forbidden` 拒、`--policy=allow` 过，两条 `exit=0` 的 E3 输出存档（`gopi-self-a.md:143`、`e3/D08a.txt` 与 `e3/D08b.txt`，属阶段 2 测量锚点，本 run 未复跑）；对方：`lobehub@658cded:apps/server/src/services/aiAgent/pipeline/approvalResume.ts:9` `HumanApprovalResolution`、`:23` `ApprovalClaimState`，`:18` 注释写明 pending 行的翻转与回滚守卫 |
| D09 | full | partial | 我强敌弱 | 我方：本 run `gopi tool sandbox create --label gap --task-id 01a1acc7-7d55-7b42-a49e-1c6cfe9bf304` → `path=/home/user/workspace/gopi-sandbox/sandbox-gap-2faf02d3`，`ls` 该路径 `.gopi/` 看到 `settings.json`（0444 副本）、`memory.db`（0600 私有账本）、`plugins`（符号链接到真实插件树）、`sessions/`、`raw/`、`logs/`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D09-D07-D08-D17.txt`）；对方：`lobehub@658cded:apps/cli/src/tools/isolatedWorker.ts:3` `ISOLATED_TOOL_APIS` 白名单、`:36` `childProcess.spawn` 起一个隔离子进程，另 `.devcontainer/devcontainer.json` |
| D10 | full | partial | 我强敌弱 | 我方：本 run `vision_analyze` 读 `/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/cap_before.png` → 返回 `输入 1014 tokens / 输出 2047 tokens` 并逐条读出屏幕上的中文与英文文字；对方：`lobehub@658cded:apps/server/src/routers/lambda/image/index.ts`（474 行图像路由）、`apps/server/src/modules/ModelRuntime/inlineImageNormalizer.ts` |
| D11 | full | partial | 我强敌弱 | 我方：本 run 同一屏三次 capture 的 `sha256_16` 由 `7cd463f2da020964` 经 `act move 960 540` + `act click 1` 变为 `7f72ed1e9c89367a`、再经第三次 capture 变 `039bd27eb99fe3c3`，五次调用全 `exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D11-loop.txt`）；对方：`lobehub@658cded:apps/desktop/src/main/modules/heterogeneousAgent/browserMcpTools.ts:40` `browser_navigate`、`:52` `browser_snapshot`（无障碍快照 + `[ref=e12]`）、`:66` `browser_click` —— 驱动的是**应用内浏览器**，不是宿主桌面 |
| D12 | full | partial | 我强敌弱 | 我方：本 run `gopi web --addr=127.0.0.1:18391` 起服务后 `curl` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D12-web.txt`）；对方：`lobehub@658cded:apps/server/src/routers/lambda/search.ts:87` `export const searchRouter = router(` 是 tRPC 服务面，`apps/server` 整树是 Next.js 服务端 |
| D13 | full | partial | 我强敌弱 | 我方：本 run `gopi session stats --last --tools` → `requests 19`、`prompt tokens 1,589,285`、`cache 95.09% hit (1.51M) / 78K miss`、`source session jsonl <id>.jsonl (wire bodies from <id>.raw.log)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D13-stats.txt`）；另 `gopi session log --last` 打印会话头与 `Workdir:` 行（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；对方：`lobehub@658cded:apps/cli/src/commands/trace/index.ts:7` `Inspect and replay recorded agent execution traces`，读取器 `apps/server/src/routers/lambda/topic/transcript.ts:26` |
| D14 | full | partial | 我强敌弱 | 我方：本 run `gopi tool tokens --file devdoc/competitor-analysis-framework.md` → `11373 tokens (35872 bytes, 500 lines, deepseek-v4.1-bpe)`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；`session stats` 另给服务器侧 usage 与本地计数的并排（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D13-stats.txt`）；对方：`lobehub@658cded:apps/server/src/routers/lambda/usage.ts:21` `export const usageRouter = router(`，`:22` `getAgentUsageStats` 带 `startAt`/`endAt`/`granularity` |
| D15 | full | partial | 我强敌弱 | 我方：本 run `gopi tool code-search TestGapDocContract` → 4 条结构化命中，每条带 `file:line` 与 `kind`，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；对方：`lobehub@658cded:apps/cli/src/tools/isolatedWorker.ts:3` 的 `ISOLATED_TOOL_APIS` 里是 `grepContent`/`globFiles`/`listFiles`/`searchFiles` 四个全文工具，且 `grep -iE '\blsp\b\|symbol' paths.txt` 命中 `0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/e2-lowercase-path-census.txt`）——全文检索有，符号级定义没有 |
| D16 | full | partial | 我强敌弱 | 我方：本 run `gopi memory search "gapdoc 契约门禁" --limit 1` → 命中一条并打印 `keywords:` 整段，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-our-probes.txt`）；写入腿的往返见 `gopi-self-b.md:854`（store 得 id、search 命中同一 id）；对方：`lobehub@658cded:apps/cli/src/commands/memory.ts:18` `Manage user memories`，子动词 `list`/`create`/`edit`/`delete`/`persona`/`extract`/`extract-status` 七个，另 `apps/cli/src/commands/kb` |
| D17 | full | partial | 我强敌弱 | 我方：本 run `gopi task list --status pending` → `15 task(s) with status=pending:` 并逐行打印 id/状态/summary/来源/创建时刻，`exit=0`（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D09-D07-D08-D17.txt`）；提交与按 id 读回的往返见 `gopi-self-b.md:856`（`Task queued: 01a1ac99-e0eb-74f2-b275-777641ef6e35` → `gopi task show 01a1ac99-e0eb-74f2-b275-777641ef6e35` 同 id）；对方：`lobehub@658cded:apps/cli/src/commands/task/index.ts:30` `program.command('task').description('Manage agent tasks')`，`:35` `list` 带 `--parent`/`--agent`/`--board`/`--tree`，`:313-319` 心跳超时读 `t.heartbeat.timeout`；派发侧 `apps/server/src/services/queue/QueueService.ts:15` 写明 `Local` 与 `AGENT_RUNTIME_MODE=queue` 两种执行模式 |

判定列的机械来源：`(我方级,对方级)` 经 `internal/standards/gapdoc_contract_gate.go:201` 的 `gapDocVerdictFor` 唯一决定，不是手写判断。

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

1. **D01 是唯一的「敌有我无」，也是本表里我方唯一落在对方下面的一维**：我方 README 的安装词普查 `0` 命中（`exit=1`，`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/our/e3-D01-ours.txt`），根目录只有 `go.mod` + `Makefile`；对方一字排开三条路线 —— `README.md:255` 的一行 `curl` 安装脚本、`README.md:261` 的 `docker compose up -d`、`apps/cli/package.json:5` 的三个 npm `bin` 别名（`lh` / `lobe` / `lobehub`，包 `@lobehub/cli@0.0.74`）。这一格是我方要补的第一件事，代价也最低。
2. **「敌强我弱」是空桶，且它是规则产物而不是测量结果**：对方上限 `partial` 由「不安装、不运行第三方 agent」（Rule 27/35）机械决定，所以 `(我方 full, 对方 full)` 这一对在整张表里不可能出现。把 15 行「我强敌弱」读成「我方能力强一档」是这一维最典型的误读 —— 差的是**证据通道**，不是能力。
3. **D07 是唯一的「平手」，而且两侧机制不同**：我方扩展点是本地插件树（`internal/config/plugins.go` + `plugins/websearch`），Go 源码里 `\bmcp\b` 命中 `0` 文件；对方反过来 —— MCP 客户端三件套齐全（`StdioClientTransport` / `StreamableHTTPClientTransport` / `new Client(`），`apps/cli/src/program.ts:121` 另注册了插件命令。等级相同、路线不同，本文件不给高下。
4. **对方证据最硬的三处，按可点开的实现点排**：D07 的 MCP 客户端 `apps/desktop/src/main/libs/mcp/client.ts:387`（stdio 与 streamable HTTP 两条传输）、D17 的任务心跳超时 `apps/cli/src/commands/task/index.ts:313-319`（`t.heartbeat.timeout` 与 `⚠ TIMEOUT` 渲染）、D11 的浏览器 `[ref=e12]` 快照式元素寻址 `browserMcpTools.ts:52`。第三处与我们的 XTEST 桌面注入是**两条不同的路**：它驱动应用内浏览器，我们驱动宿主桌面，本表按框架 §3 的定义把两者都记在 D11。
5. **我方证据最硬的是「可对账」而不是「功能更多」**：D11 的五次调用把同一屏的 `sha256_16` 从 `7cd463f2da020964` 推到 `7f72ed1e9c89367a` 再到 `039bd27eb99fe3c3`；D13 的 `cache 95.09% hit (1.51M) / 78K miss` 与 `source session jsonl 01a1ae0b-a983-7c96-936c-28e686d996a9.jsonl (wire bodies from 01a1ae0b-a983-7c96-936c-28e686d996a9.raw.log)` 把本地计数与服务器 usage 并排；D14 的 `11373 tokens (35872 bytes, 500 lines, deepseek-v4.1-bpe)` 是可复算的整数。这三格是「命令 + 退出码 + 原始输出」的完整形状。
6. **本文件的边界（写清，不掩盖）**：对方无 E3，因此对方 `full` 结构性不存在；`D08` 我方那格引用的是阶段 2 锚点的双向策略实测（`gopi-self-a.md:143`、`e3/D08a.txt` 与 `e3/D08b.txt`），本 run 未复跑；`D16` 的写入腿往返、`D17` 的提交读回往返同样来自 `gopi-self-b.md:854` 与 `:856` 的另一个测量锚点，本 run 只复跑了读侧（`memory search`、`task list --status pending`）。`counts_as_of: 2026-11-05T23:21:07Z`，之后对方的 main 与本 ground 的二进制任一动，本表即失效。
7. **补采集的成果应当被阶段 2 吸收，而不是留在本文件里**：E2 的路径普查已经把 `lobehub@658cded` 的 23,224 个 blob 全量落盘（`/home/user/.gopi/sessions/01a1acc7-7d55-7b42-a49e-1c6cfe9bf304/artifacts/lobehub-probe/paths.txt`），按维分组的命中计数在 `e2-path-census.txt`；本文件只消费了其中 20 个原文文件。若后续要写 `devdoc/competitor/lobehub.md`，从这两个文件继续即可，无需重新拉取。
