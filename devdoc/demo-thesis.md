<!-- doc-class: historical -->
# demo 立意与结构（demo thesis）


**这份文档是什么。** 它不是素材库、不是页面、不是竞品报告——那三样分别已有
`docs/archive/demo/materials/`（159 条原始素材）、`docs/archive/demo/final/`（成品页）、
`docs/archive/demo/final/competitive.md`（四家 file:line 对比）。**这份文档是「论点」**：
把已收集的素材拧成一句话，并给出可被 **01a0eea8** 直接消费的骨架
（文件名、路径、节序）。素材是弹药，本文说打哪儿。

**口径（先说清，否则下文会被误读）。**

- 判定 ∈ **{赢, 输, 平, 未判}**，延续 `docs/archive/demo/final/competitive.md` 的口径：
  **赢** = 对手该维度比 gopi 强，**输** = gopi 更强，**平** = 双方都有等价机制，
  **未判** = 本次能读到的材料里查不到 —— **查不到就是未判，不做无据判定**。
- 每个 census 数字带**生成器 + measured_commit**（GOPI.md Rule 38）。
- 每条「缺什么」必须标 **未做** 或 **刻意不做**；刻意不做引 GOPI.md 规则号。
- **gopi 自己的「最不擅长」必写**，且不许可写成"取舍"来遮缺口 —— 取舍与缺口是两件事。

**本文件的测量点**：`7e140378`（本任务 worktree 的基线，= master 当时 tip）。

---

## 0. 一句话论点（demo 的立意）

> **别人把能力做成了平台，gopi 把能力做成了不变量。**
> 我们输在扩展面（工具是编译期写死的、没有 MCP/ACP、没有子 agent），
> 赢在**可审计、可拷走、能自证**——三件事都是"可以被机器复核"的能力，
> 而不是"可以被描述"的能力。

这句话决定了 demo 的**形态**：不是功能罗列（罗列必输，对手工具数比我们多），
而是**让观众看见一条自己复核得了的记录链**。demo 的说服力来源是
「你可以不信我，你去核」——这正是素材库六件套（`01-input.txt` / `02-output.txt`
含 `exit=` / `03-raw.png` / `04-annotated.png`）的判据，也是 `gopi shot` /
`text2img` / `annotate` 三个自研动词存在的原因。

---

## 1. 三条路线对撞

三条路线不是三家公司的做法分类，而是**三种"把 agent 交到用户手里"的赌注**。
下面每条都写「最擅长什么 / 最不擅长什么」，**包括 gopi 自己**。

### 路线 A — 开放平台（dsh / codex）：把 agent 做成运行时

**赌注**：能力上限由**扩展面**决定。谁的第三方能挂得更多，谁最终更强。

| 最擅长 | 证据 |
|---|---|
| 扩展面在**运行时**，不在编译期 | dsh `packages/subagent/tool-subagent-control/src/index.ts:26` `ctx.tools.register(defineTool({…}))`；模型自己可 `cordis_define` 挂新插件 |
| 工具规模只受磁盘限制 —— 初始 context 只背核心小工具 | codex `codex-rs/tools/src/tool_spec.rs:22` `pub enum ToolSpec`（`Function`/`Namespace`/`ToolSearch`），`tool_search` 走 BM25 按需加载 |
| 外部世界接入是一等公民 | dsh `packages/mcp/mcp-client/src/tools.ts:8`：MCP 工具命名 `mcp__<serverName>__<rawName>`，归一化后注册；`packages/acp/` 另有 ACP 入口 |
| 大任务可并行分解 | codex `codex-rs/core/src/tools/handlers/multi_agents/spawn.rs:69`：`"Agent depth limit reached. Solve the task yourself."`（有深度闸门的真委派生命周期） |

| 最不擅长 | 证据 |
|---|---|
| **分发**：拷一台机器跑不起来 | dsh 需 Node `^22.19.0` + pnpm workspace（226 个 `package.json`）+ node_modules；`devdoc/competitor-dsh-analysis.md` §6.3 实测 14762 个 `node_modules` `.ts` |
| **审计面**：状态散在会话里，没有跨会话台账 | dsh 的 `goal` 是"persisted **same-session** goal domain"（`packages/goal/tool-goal/src/index.ts:3`），不是一个可被第三方复核的 ledger |
| **不变量少**：机制靠约定，不靠谓词 | 没有与 gopi `taskRowClaimable`（`internal/tools/tasks.go:5982`）等价、"派发漏斗与视图共用同一个谓词"的单一判定点 |

### 路线 B — 产品化（claude code）：把 agent 做成好用的产品

**赌注**：用户不为能力付费，为**顺手**付费。

| 最擅长 | 证据（本仓真跑探测，`devdoc/competitor-claude-code-behavior.md`） |
|---|---|
| 长时程 / 后台 agent 面 | P1：`claude --bg "<task>"` → 跨进程 pid + `claude agents/attach/logs/stop` 全生命周期；gopi 的 `job` 是**进程内**后台命令（`internal/tools/job.go:110`）。**判定 claude 赢** |
| 权限层：默认拒绝 + 可配置模式 + **结构化拒绝记录** | P4：默认权限模式下写 cwd 外被拒；P6：`--output-format json` 里有 `permission_denials`（带 `tool_use_id` + 原始 `tool_input`）。**判定 claude 赢** |
| 开箱即用：技能/工作流/同会话任务板/子代理 | P7：25 个工具含 `Agent`（子代理）、`Skill`、`TaskCreate/Get/List/Update/OneOutput/Stop`（6 个同会话任务板动词） |
| 门面 | 产品 UI、IDE 集成、`--permission-mode` 分级授权 —— 面向"开发者一次任务"的体验是四家里最顺的 |

| 最不擅长 | 证据 |
|---|---|
| **跨会话可审计**：任务板随会话消失 | P3：任务板不落盘（`sessions.db` 实测 0 字节）；跨会话要人工 `--resume`。gopi 的 tasks 表是跨会话 append-only 层。**判定 gopi 赢** |
| **记忆按 cwd 分域**：换目录即失忆 | P2 真跑：换目录问同一事实 → "I DO NOT KNOW."；gopi 的 sqlite 知识库换进程换目录都能检索 |
| **危险闸门可被一个 flag 整体关掉** | P6：`--dangerously-skip-permissions` 后 `denials=[]` 全放行，只剩模型自身价值判断 |
| **引入需要被警惕的持久依赖** | 常驻 daemon + `/tmp/cc-daemon-*/…/claim.sock`；且后台进程**未脱离登录会话**（`/proc/<pid>/cgroup` = `session-983.scope`，Rule 24 判据实测）。在本仓 Rule 35 的尺度上这是缺点 |

### 路线 C — 机制化（gopi）：把 agent 做成一组可复核的不变量

**赌注**：能力会被抄走，**不变量不会**——因为不变量要求你放弃扩展面。

| 最擅长 | 证据（本任务真跑，证据原件见 §5） |
|---|---|
| **跨会话台账 + 父依赖闸门**：一个谓词同时决定派发与视图 | `internal/tools/tasks.go:5864` `taskParentGateDenied` / `:5982` `taskRowClaimable`；`gopi task list --status pending` 直接渲染 `[queue-skip] parent … not released` + `[next]`。**两条路径不会一个说能领、一个说不能。** |
| **零依赖单二进制**：一个 Go 产物 + 一个 settings 文件 | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`（本次真跑，20.1s）。这是 Rule 35 的物质基础，也是 A/B 两条路线不可能复制的一条 |
| **像素证据链**：渲染/截图/标注/桌面驱动都是自研、零 CGO、产真 PNG | `internal/text2img/render.go`（自研 SFNT/TrueType 栅格化）、`internal/text2img/annotate.go:198` `AnnotateFile`（输入图永不修改）、`internal/toolnames/names.go:108` `ToolScreenCapture` |
| **临时根 off-`/` 的机械断言 + 拒绝时自报通道** | `internal/scratch/tmproot.go:615` `AssertTempRootOffRootFS`：不是静默退回，是**说出哪条通道决定的** |
| **记忆质量裁判闭环** | `internal/tools/memory.go:4282` `MemoryFeedback`：投票/纠正/反馈**直接改检索排名**；codex 的自动管线更自动，但没有这个用户级降级通道 |
| **沙盒是一份可运行的私有 profile** | `internal/sandbox/sandbox.go:213` `Create`：`GOPI_HOME="$SB" gopi log --last` 证明它有自己的会话（dsh 的 sandbox 是文件访问**模式**，不是另一个能跑的 profile） |

| 最不擅长（**必写，不做稻草人**） | 证据 |
|---|---|
| **工具是编译期写死的，扩展面不存在** | 见 §2.3。加一个工具要动三处：`internal/agent/loop.go:1168` `claudeToolDefs` + `:5830` `switch tc.Name` + 路由 prompt。挂不上任何第三方能力 |
| **没有子 agent 委派 —— 大任务只能串行** | §2.4。唯一的并行是 `internal/tools/job.go:110` 的后台 shell **命令**，不是 agent |
| **没有权限层**：写越界不默认拒绝 | §2.6。`internal/tools/bash.go:62,99` 只有危险命令黑名单 + `GOPI_ALLOW_DANGEROUS` 逃逸口；**一个被 prompt 注入的写指令会直接落到文件系统** |
| **没有后台/可 attach 的长时程 agent 面** | §2.7。claude 赢在这条（P1），差距是量级级的 |
| **没有技能/工作流系统，没有 IDE 集成** | §2.1 / §2.5 |
| **能力面窄**：17 个工具，对手 25（claude 实测）/ 95（hermes 母项目口径）| §2.3；`GOPI.md:55` "GoPi has 17 tools:" |

### 1.4 三条路线的一句话结算

| | A 开放平台 | B 产品化 | C 机制化（我们） |
|---|---|---|---|
| 上限 | 最高（第三方扩） | 中高（体验） | **最低（扩展面自缚）** |
| 下限 | 最差（拷不走） | 中（依赖 daemon） | **最好（一个二进制）** |
| 可审计 | 弱 | 中（结构化拒绝） | **强（台账 + 谓词 + 证据链）** |
| 谁该选它 | 要生态的人 | 要顺手的人 | **要"能核"与"能带走"的人** |

**demo 的立意就落在这张表的第三列**：我们不在第一列跟人比，我们在第三列把话说死。

---

## 2. 「我们缺什么」—— 逐条，带证据，标 未做 / 刻意不做

**这一节是 demo 的最后一节（§4 骨架的 S5），不是附录。** 理由：
把缺口写在最后并且写死，是整个 demo 可信度的来源；先赢后输的排序会让前面每一条
都显得像宣传。**证据列里的命令全部由 `§5` 的生成器产出，可复跑。**

### 2.1 技能（skills）系统 —— **未做**

- **缺什么**：把"一件有步骤、有触发条件、可复用的做事方法"作为一等资产存起来、
  被模型按需加载的能力。gopi 一个都没有。
- **证据**：
  ```
  $ grep -rn --include='*.go' -iE 'SKILL\.md|skillLoader|loadSkills|skillsDir' internal cmd | wc -l
  0
  ```
  gopi 里唯一的 `skills` 字符串是一条注释提到的**插件自带文档**
  （`internal/tools/websearch.go:24`："…see the plugin's skills/websearch.md"），
  那是 websearch 插件的说明书，不是技能系统。
- **对手侧（本地源码一手）**：hermes `skills/` 下有 **21 个**技能目录
  （`apple … yuanbao`，本次实跑 `ls -d /home/user/work/hermes-agent/skills/*/ | wc -l` = 21），
  另有技能图统计：`/home/user/work/hermes-agent/agent/learning_graph.py:29` `class SkillNode`。
- **标注**：**未做**。不是取舍，是缺口 —— gopi 的 `memory` 存的是**蒸馏记忆行（QA）**，
  它记的是"我知道什么"，不是"我该按什么步骤做"。两者不可互相替代。

### 2.2 MCP / ACP 等外部能力接入 —— **未做**

- **缺什么**：挂载第三方工具服务器（MCP），或被编辑器/IDE 以协议方式驱动（ACP）。
- **证据**：
  ```
  $ grep -rn --include='*.go' -iE 'mcp[_-]?client|mcp__|jsonrpc.*mcp|McpServer|mcp.Client' internal cmd | wc -l
  0
  ```
- **对手侧**：dsh `packages/mcp/mcp-client/src/tools.ts:8`
  （`mcp__<serverName>__<rawName>`，归一化到合法函数名），并且有 `packages/acp/` 入口。
- **标注**：**未做**。gopi 现有的唯一外部能力机制是**插件目录**
  （`plugins/` 下本机只有 `websearch` 一个），它是**进程内脚本调用**，
  不是协议接入 —— 挂不上别人的工具。

### 2.3 工具数量写死（17 个，加一个要动三处）—— **刻意不做**

- **缺什么**：工具的**动态发现与注册**。对手的工具规模只受磁盘限制，gopi 的受**编译期**限制。
- **证据**：
  ```
  $ grep -n 'var claudeToolDefs' internal/agent/loop.go
  1168:var claudeToolDefs = []deepseek.ClaudeToolDef{
  $ grep -n 'switch tc.Name' internal/agent/loop.go
  5830:	switch tc.Name {
  ```
  17 个名字真实读自该表（`grep -oE 'Name: +toolnames\.[A-Za-z]+'`）：
  `read git_blame write edit bash code_search keyword_search web_search web_fetch
  memory context_trim task_submit job task vision_analyze text2img screen_capture`。
  工具数还是个**字面量**写在规则文件里：`GOPI.md:55` "GoPi has 17 tools: …"。
- **对手侧**：codex `codex-rs/tools/src/tool_spec.rs:22` `pub enum ToolSpec`
  的 `Namespace` / `ToolSearch` 形态；dsh `ctx.tools.register(...)`
  (`packages/subagent/tool-subagent-control/src/index.ts:26`)。
- **标注**：**刻意不做**，依据 **GOPI.md Rule 3（Tools Are Minimal — Don't Add New Ones）**：
  > "Do NOT add new top-level tools — extensions go through `code_search` …,
  > `keyword_search` …, bash …, read/edit/write, memory …, and `task_submit`."
  并在 `internal/agent/loop.go` 上由 `TestGOPIRule3_MatchesClaudeToolDefs` 断言两处一致。
  **代价必须承认**：这条规则换来的是"每一轮模型看到的工具面固定、可预测、token 预算封顶"，
  代价是**挂不上第三方能力、没有按需加载**。demo 里这句要照写。

### 2.4 多 agent 并行 / 子 agent 委派 —— **未做**

- **缺什么**：把一个任务拆成若干可并行、有独立上下文、有深度闸门的子 agent。
- **证据**：
  ```
  $ grep -rn --include='*.go' -iE 'spawn_?agent|sub-?agent|SubAgent' internal cmd | wc -l
  0
  $ grep -n 'func JobStart' internal/tools/job.go
  110:func JobStart(command string) (string, error) {
  ```
  gopi 唯一的并行机制是**后台 shell 命令**（`job` 的 start/output/kill 三个动作），
  它并行的是**命令**，不是**推理**。
- **对手侧**：codex `…/multi_agents/spawn.rs:69` 有深度闸门
  （`"Agent depth limit reached. Solve the task yourself."`）；
  dsh 有 subagent 控制面（`tool-subagent-control`）。
- **标注**：**未做**（`GOPI.md` 里没有一条规则禁止它 —— Rule 3 管的是**顶层工具**，
  一个子 agent 通道不一定以新顶层工具的形式出现）。这是**最硬的一条缺口**，
  对手报告与 dsh 报告各自独立得出同一结论（"对大型任务是量级级吞吐差"）。
- **诚实补充**：`task_submit` 能"派活给另一个会话"，但那是**跨会话的异步投递**，
  不是同会话的子 agent —— 别把它当子代理面来宣称。

### 2.5 IDE 集成 —— **未做**

- **证据**：
  ```
  $ grep -rn --include='*.go' -iE 'language[_-]?server|gopls|vscode|jsonrpc.*lsp|LSPClient' internal cmd | wc -l
  0
  ```
- **对手侧**：codex README 列有 IDE 形态；claude code 是终端 + 编辑器生态；
  dsh 有 `packages/acp/`（编辑器可用协议驱动它）。
- **标注**：**未做**。gopi 的交互面是 CLI/I-O 与自带的 web chat（`gopi shot --self`，
  静帧见 `docs/archive/demo/external/stills/01-gopi-self-webui.png`）。
  **注意别把"自带 web UI"说成 IDE 集成**——它不是。

### 2.6 沙箱分级与审批分层 —— **部分未做 / 部分是刻意**

- **缺什么**：`read-only → workspace-write → danger-full-access` 三档沙箱，
  与 `ask | never` 的审批策略，以及**结构化可机读的拒绝记录**。
- **证据**：
  ```
  $ grep -n 'GOPI_ALLOW_DANGEROUS' internal/tools/bash.go | head -3
  52://   - GOPI_ALLOW_DANGEROUS=1 disables ALL dangerous-command rejection (legit
  62:// escapeDangerous is set once from the GOPI_ALLOW_DANGEROUS env var in init()
  99:	if v := os.Getenv("GOPI_ALLOW_DANGEROUS"); v == "1" {
  ```
  gopi 的闸门 = **危险命令黑名单 + 一个环境变量逃逸口**；`gopi sandbox` 是
  **路径/台账隔离**（`internal/sandbox/sandbox.go:213`），**不是权限隔离**。
- **对手侧**：codex `AskForApproval` + `SandboxPolicy`；dsh
  `packages/sandbox/sandbox/src/index.ts:29`
  `export type SandboxMode = 'read-only' | 'workspace-write' | 'danger-full-access'`。
  真跑侧（claude）P4 实测：默认拒写 cwd 外。
- **标注**：拆成两半，别混为一谈 ——
  - **分级/审批层：未做**（这是缺口；一个被注入的写指令在 gopi 下会落到文件系统，真跑报告 P4 原文如此）。
  - **"不引入策略层"这个选择：部分是刻意** —— gopi 把安全做成
    **无旁路的路径作用域守卫**而不是**可被一个 flag 整体关掉的策略层**（对比：
    claude 加 `--dangerously-skip-permissions` 后 `denials=[]` 全放行，P6 实测）。
    这条差异在 demo 里的正确说法是"我们少一层，也少一个可以被关掉的开关"，
    **不能**说成"我们的安全更强"。

### 2.7 后台 / 可 attach 的长时程 agent 面 —— **未做**

- **证据**：`job` 是**进程内**后台命令（`internal/tools/job.go:110`），
  进程结束即消失，管理面只有 `start/output/kill`；没有独立的后台会话、没有 attach、没有续聊。
- **对手侧（真跑）**：claude P1 实测 —— `claude --bg` 起独立 pid，
  `claude agents --json` 看 `state`，`claude logs <id>` 回放，
  且该进程的 `/proc/<pid>/cgroup` 显示**未脱离登录会话**（`session-983.scope`）。
- **标注**：**未做**。**这是本次对比里最诚实的一条**：我们连"对手的缺点"都跟着一起
  承认了 —— 我们想要的不是 daemon，而是**看护长任务**的能力，它现在不存在。

### 2.8 记忆的自动沉淀与跨任务整合（consolidation）—— **未做**

- **证据**：gopi 的记忆靠模型每任务**主动** `memory action=store`；
  P2 真跑的旁证显示，若 prompt 不明确要求，很可能一条都不产生。
- **对手侧**：codex 两阶段后台管线（rollout 抽取 → 全局 consolidation sub-agent）。
- **标注**：**未做**。但这条要在 demo 里**配上我们的赢面一起写**：
  `internal/tools/memory.go:4282` `MemoryFeedback` 的**用户级降级/纠正通道**
  codex 没有 —— 我们少的是自动化，多的是裁判权。

### 2.9 内建定时 / 常驻调度 —— **刻意不做**

- **证据**：hermes 有 `cron/` 子系统（本次实跑 `ls /home/user/work/hermes-agent/cron/`
  → `scheduler.py` `jobs.py` `lifecycle_guard.py` …）；gopi 内建调度器**为 0**。
- **标注**：**刻意不做**，依据两条 GOPI.md 规则：
  - **Rule 35（Zero Dependency Is a Discipline）**：不靠系统层 cron/脚本掩盖问题；
    实测事故 —— 一条 `*/10 * * * *` 的 cron 跑到 36,398 行 / 1,214 次委派，
    而它在**掩盖**"worker 不断死亡"这个真问题。
  - **Rule 26（An Incomplete Fix Is Repaired by Dispatching Again — Never by Waiting）**：
    不完整就再派一个任务，而不是加一层自动重启。
  - **必须承认代价**：无人值守的周期作业要**操作者自己**起 systemd。
    这句话在 demo 里要原样出现，不能只写"我们刻意不做"。

### 2.10 单次运行的结构化成本/用量回报 —— **未做**

- **证据**：gopi 的等价信息在 `gopi stats` / trajectory 行里，**不在同一次运行的返回里**；
  claude 每次 `--output-format json` 返回 `usage` / `total_cost_usd`（P4c 实测 `$0.19186`）。
- **标注**：**未做**。（内部对照：gopi 有分时定价核算 `internal/pricing/pricing.go`，
  那是**事后**的账，不是**当次**的回执 —— 两件事。）

### 2.11 缺口汇总表（demo 里直接引这一张）

| # | 缺什么 | 标注 | 依据 |
|---|---|---|---|
| 1 | skills 系统 | **未做** | grep=0；hermes 21 个技能目录 + `learning_graph.py:29` |
| 2 | MCP / ACP 接入 | **未做** | grep=0；dsh `mcp-client/src/tools.ts:8`、`packages/acp/` |
| 3 | 工具数量写死 / 无按需加载 | **刻意不做** | **Rule 3**；`loop.go:1168` + `:5830`；`GOPI.md:55` |
| 4 | 多 agent 并行 / 子 agent | **未做** | grep=0；codex `spawn.rs:69`、dsh `tool-subagent-control` |
| 5 | IDE 集成 | **未做** | grep=0 |
| 6 | 沙箱分级 / 审批层 | **未做** | `bash.go:62,99`；codex `AskForApproval`；真跑 P4 |
| 7 | 后台 / 可 attach 的 agent 面 | **未做** | `job.go:110`；真跑 P1 |
| 8 | 记忆自动整合 | **未做** | codex 两阶段管线；我们的赢面是 `memory.go:4282` |
| 9 | 内建定时 / 常驻调度 | **刻意不做** | **Rule 35 + Rule 26**；hermes `cron/` |
| 10 | 当次运行的成本回执 | **未做** | claude `--output-format json` 实测 |

**9 条「未做」+ 1 条「刻意不做」+ 1 条（§2.6）半分。** 刻意不做只有两条，
不许把「未做」包装成「刻意不做」——那是这份文档最容易被后人改坏的地方。

---

## 3. 三条取胜线（demo 的主干，顺序即叙事）

取胜线不是"我们最炫的功能"，而是**"对手在这一列给不出等价物"**的三件事。
每条都必须满足：**可被观众自己复核**（有命令、有 exit、有 PNG）。

### 线 1 — 「你可以不信我，你去核」：跨会话台账 + 父依赖闸门

- **一句话**：一次工作变成一行跨进程存活的记录，而"孩子能不能被派发"由**一个谓词**决定，
  视图与派发漏斗不可能给出两个答案。
- **为什么对手给不出**：claude 的任务板随会话消失且不落盘（真跑 P3）；
  dsh 的 `goal` 是 same-session 域。
- **证据/配图**（素材库，只读引用，**不复制 bytes**）：
  - `docs/archive/demo/materials/001-task-list/04-annotated.png` —— 一眼看全台账
  - `docs/archive/demo/materials/011-parent-gate-parked/04-annotated.png` —— `[queue-skip] parent … not released`
  - `docs/archive/demo/materials/002-task-submit/04-annotated.png` —— 一次工作 = 一行记录
- **源码锚**：`internal/tools/tasks.go:5864`（`taskParentGateDenied`）、`:5982`（`taskRowClaimable`）
- **一句现场话术**：`gopi task list --status pending` 的输出里，
  `[queue-skip]` 与 `[next]` 是**同一个谓词的两个渲染** —— 这是不变量，不是约定。

### 线 2 — 「一个二进制，拷过去就跑」：零依赖 + 私有沙盒 profile

- **一句话**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`，
  一个产物加一个 settings 文件；沙盒不是"文件访问模式"，是**另一个能跑的 profile**。
- **为什么对手给不出**：dsh 要 Node + pnpm + node_modules（226 个 `package.json`）；
  codex 是 ~130 crates 的 cargo workspace + 独立沙箱助手进程；hermes 是 Python 应用。
- **证据/配图**：
  - `docs/archive/demo/materials/006-sandbox-run/04-annotated.png` —— `gopi sandbox run` 真的在里面跑
  - `docs/archive/demo/materials/005-sandbox-show/04-annotated.png` —— 沙盒在哪、哪条通道定的
  - `docs/archive/demo/materials/env-04-tmpdir-loud-refusal/04-annotated.png` —— 拒绝时**说出通道**，不静默退回
- **源码锚**：`internal/sandbox/sandbox.go:213`、`internal/scratch/tmproot.go:615`
- **一句现场话术**：`GOPI_HOME="$SB" gopi log --last` —— 沙盒有自己的会话，
  这句话本身就是"它是一个 profile"的证明。

### 线 3 — 「我们能看见自己」：零 CGO 的像素证据链

- **一句话**：渲染（`text2img`，自研 SFNT/TrueType）、无头截图（`shot`）、
  真实桌面捕获（`capture`）、标注（`annotate`，输入图永不修改）、桌面驱动（`act`）——
  五个动词全是自研纯 Go，产的是**可复核的 PNG + 像素尺寸**，不是形容词。
- **为什么对手给不出**：这套东西的存在理由就是**证据自证**；
  对手的图来自浏览器/第三方工具链（dsh 的 UI 是 34 个 React 包）。
- **证据/配图**：
  - `docs/archive/demo/materials/140-pixel-evidence/04-annotated.png` —— 五个动词各产一张真图
  - `docs/archive/demo/materials/124-text2img/04-annotated.png` —— 中文渲染零 CGO
  - `docs/archive/demo/materials/126-annotate/04-annotated.png` —— 标注是独立动词
  - `docs/archive/demo/materials/130-shot/04-annotated.png` —— 不碰真实桌面也能出整页图
  - 静帧：`docs/archive/demo/external/stills/01-gopi-self-webui.png` … `06-annotate-live-window.png`
- **源码锚**：`internal/text2img/annotate.go:198`、`internal/toolnames/names.go:108`
- **一句现场话术**：本 demo 的**每一张图**都产自这条链 —— 页面自己就是这条线的样品。

**顺序理由**：线 1 立"可审计"（我们最硬的不变量）→ 线 2 立"可带走"（对手做不到的形态）
→ 线 3 立"能自证"（前面所有断言的证据来源）。倒过来会变成功能罗列。

---

## 4. demo 骨架（**01a0eea8 可直接消费**）

### 4.1 它是什么（一句话）

> **一个单文件、离线、可双击打开的 HTML：先给三条取胜线各配一张真实运行截图，
> 再把"我们缺什么"用十条带源码行号的条目写死在最后一节。**

### 4.2 形态与硬约束

| 项 | 决定 |
|---|---|
| 形态 | **HTML 主体 + 少量动效**。单文件、零依赖、离线（`file://` 双击即开） |
| 动效预算 | **只允许**：section 入场淡入、配图 hover 放大、锚点平滑滚动。**不允许**：自动轮播、视频、任何需要网络的资源、任何 JS 框架 |
| 图的引用方式 | **相对路径直接引用素材库**（`../materials/<条目>/04-annotated.png`），**绝不复制 bytes** —— 复制会产生第二份 sha256，"这是原始记录"的价值就没了 |
| 净化 | 页面文本不得含绝对路径 / session id / `/home/<user>`（`build.sh` 已有这条自检，会大声报错） |
| census 数字 | 必须带 **生成器 + measured_commit**（Rule 38）。见 §5 |

### 4.3 节序（六节，**「我们缺什么」在最后一节**）

| 节 | 标题 | 内容 | 素材/数据来源 |
|---|---|---|---|
| S0 | 一分钟看懂 gopi | 一句话论点（§0）+ 一个二进制 | 本文 §0 |
| S1 | 三条路线对撞 | §1 的三张表（各写擅长/不擅长，**含 gopi 自己的**） | 本文 §1 + `docs/archive/demo/final/competitive.md` |
| S2 | 取胜线 1：可审计 | 台账 + 父依赖闸门 | 材质 `001-task-list` / `011-parent-gate-parked` / `002-task-submit` |
| S3 | 取胜线 2：可带走 | 零依赖 + 沙盒 profile | 素材 `006-sandbox-run` / `005-sandbox-show` / `env-04-tmpdir-loud-refusal` |
| S4 | 取胜线 3：能自证 | 像素证据链五个动词 | 素材 `140-pixel-evidence` / `124-text2img` / `126-annotate` / `130-shot` |
| **S5** | **我们缺什么（最后一节，不客气）** | §2 的十条 + 缺口汇总表 | 本文 §2 |

### 4.4 可直接消费的文件与路径

| 用途 | 路径 | 状态 |
|---|---|---|
| **页面生成器（唯一入口，改内容改它）** | `docs/archive/demo/final/build.sh` | master 上已有；`bash docs/archive/demo/final/build.sh` 写三个文本产物，`--shot` 另出渲染证明 |
| 页面（成品） | `docs/archive/demo/final/index.html` | 已有，29 KB，单文件离线 |
| 四家 file:line 对比（S1 的原料） | `docs/archive/demo/final/competitive.md` | 已有，含 `未判` 口径与「怎么测的」命令块 |
| 出处表（每个功能点 → 素材条目 → PNG） | `docs/archive/demo/final/materials.tsv` | 已有，列为 `feature / entry / relpath_from_index / bytes / sha16` |
| 覆盖率（带生成器 + commit） | `docs/archive/demo/final/coverage.tsv` + `coverage-summary.tsv` | 已有；生成器 `scripts/demo-coverage.sh` |
| 像素级渲染证明 | `docs/archive/demo/final/render-proof.png` | 已有；`gopi shot --url file://…` 渲染的那一页 |
| **素材库（只读，绝不改写）** | `docs/archive/demo/materials/`（**159/193 complete**） | 生成器 `scripts/materials-coverage.sh`，`measured_commit` 印在输出头 |
| 对外静帧 + 出处 | `docs/archive/demo/external/stills/*.png` + `CAPTIONS.md` | 已有；每个静帧的 verbatim 命令 + 字节数 + sha256 + 尺寸 |
| 选材表 | `docs/archive/demo/showcase/materials-cut/selection.tsv` | 已有；生成器 `scripts/materials/pick.sh`，含六条谓词判定 |
| 半成品对照（方向校验用） | `docs/archive/demo/preview/index.html` + `README.md` | 已有 |

### 4.5 01a0eea8 要做的三件事（与已有产物不重复）

1. **重排叙事**：把 `docs/archive/demo/final/index.html` 现有的 6 节，按 §4.3 的节序改成
   「三条取胜线 → 我们缺什么（末节）」，并把 §1 的三张路线表补进去
   （现有页面 S1 是"四家竞品对比"，缺"三条路线"这层抽象）。
2. **补末节**：把 §2 的十条 + 汇总表做成 S5，**照抄标注列**（未做 / 刻意不做 + 规则号）。
   这一节**不许软化**：刻意不做只两条，其余九条写"未做"。
3. **动效**：只加 §4.2 允许的三类，且**必须先跑 `bash docs/archive/demo/final/build.sh` 通过自检**
   （缺图即中止、绝对路径即中止），再跑 `--shot` 出 `render-proof.png` 作为像素级证明。

### 4.6 本骨架的边界（读了才不会误读）

- **不改素材库**：`docs/archive/demo/materials/` 只读，一个新字节都不写（Rule 29 同源理由）。
- **不改系统层**：本任务只写 `devdoc/demo-thesis.md`，不碰 merge / worktree 机制（Rule 28）。
- **不做四家产品的对照实验**：竞品判定基于 `docs/archive/demo/final/competitive.md`（本地源码一手）
  与 `devdoc/competitor-claude-code-behavior.md`（真跑探测）；**文档未描述处一律 `未判`**。
- **census 口径分歧如实标注**：见 §5.3 —— 同一面在不同文档里分母不同，本文**不取平均、不取最大**，
  列出各自的生成器与 commit。

---

## 5. 真跑证据（本任务产出，全部可复跑）

证据原件落在本 run 的会话目录（**不进 git**，Rule 37）：
`/home/user/.gopi/sessions/01a0eec4-ef18-7263-8051-6d47711e6991/evidence/`

| 文件 | 内容 |
|---|---|
| `E-tools-and-gaps.txt` | E1–E12：工具表行号、17 个工具名、四项 grep=0、bash 闸门、plugins 树、hermes 技能数 |
| `E-census.txt` / `E-census2.txt` | E13–E20：顶层动词 / 各面子命令 / flag / env 的计数 |
| `E-spotcheck.txt` | E21：13 条 `file:line` 逐条 `sed -n` 回源码打印 |
| `E-competitors.txt` | E22–E25：dsh / codex / hermes 对手侧的 `grep -n` 落点 |
| `E-materials-coverage.txt` | 素材库覆盖率 census 的逐字输出头 |

### 5.1 生成器（verbatim，可整段复跑）

```bash
# 在 worktree 根目录执行；先建 binary（Rule 12 声明的 build 命令）
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # → BUILD_OK, 20.1s

# —— 工具面 (§2.3) ——
grep -n 'var claudeToolDefs' internal/agent/loop.go       # → 1168
grep -n 'switch tc.Name'     internal/agent/loop.go       # → 5830
grep -oE 'Name: +toolnames\.[A-Za-z]+' internal/agent/loop.go \
  | sed 's/Name: *toolnames\.//' | wc -l                  # → 17
grep -n 'GoPi has 17 tools' GOPI.md                       # → 55

# —— 四项「未做」的机械证据 (§2.1/2.2/2.4/2.5) ——
grep -rn --include='*.go' -iE 'SKILL\.md|skillLoader|loadSkills|skillsDir' internal cmd | wc -l   # → 0
grep -rn --include='*.go' -iE 'mcp[_-]?client|mcp__|jsonrpc.*mcp|McpServer|mcp.Client' internal cmd | wc -l  # → 0
grep -rn --include='*.go' -iE 'spawn_?agent|sub-?agent|SubAgent' internal cmd | wc -l            # → 0
grep -rn --include='*.go' -iE 'language[_-]?server|gopls|vscode|jsonrpc.*lsp|LSPClient' internal cmd | wc -l  # → 0

# —— 沙箱/审批 (§2.6) ——
grep -n 'GOPI_ALLOW_DANGEROUS' internal/tools/bash.go     # → 52, 62, 99, 255

# —— 对手侧本地源码 (§2.1/2.2/2.4/2.9) ——
ls -d /home/user/work/hermes-agent/skills/*/ | wc -l      # → 21
ls /home/user/work/hermes-agent/cron/                     # → scheduler.py … lifecycle_guard.py
grep -n 'mcp__' /home/user/work/deepseek-harness/packages/mcp/mcp-client/src/tools.ts | head -1  # → 8
grep -n 'Agent depth limit reached' \
  /home/user/work/codex/codex-rs/core/src/tools/handlers/multi_agents/spawn.rs                    # → 69

# —— census (Rule 38) ——
./bin/gopi -h | sed -n '/^Commands/,/^Examples/p' | grep -cE '^  gopi '     # → 37 (行数)
./bin/gopi -h | grep -oE '^  gopi [a-zA-Z0-9_-]+' | sort -u | wc -l         # → 35 (去重动词)
./bin/gopi -h | grep -cE '^  --[a-z]'                                       # → 23 (全局 flag)
./bin/gopi --help-env | grep -cE '^\s+GOPI_'                                # → 88

# —— 素材库覆盖率 ——
bash scripts/materials-coverage.sh                        # → produced 159 / 193, measured commit 7e140378
```

### 5.2 file:line 抽查（13 条，**每一条都由 `sed -n` 从真实文件打印**，逐字）

```
internal/agent/loop.go:1168       | var claudeToolDefs = []deepseek.ClaudeToolDef{
internal/agent/loop.go:5830       | 	switch tc.Name {
GOPI.md:55                        | GoPi has 17 tools: read, write, edit, bash, code_search, keyword_search, web_search, web_fetch, git_
internal/tools/bash.go:62         | // escapeDangerous is set once from the GOPI_ALLOW_DANGEROUS env var in init()
internal/tools/bash.go:99         | 	if v := os.Getenv("GOPI_ALLOW_DANGEROUS"); v == "1" {
internal/tools/job.go:110         | func JobStart(command string) (string, error) {
internal/tools/tasks.go:5864      | func taskParentGateDenied(parentID string, parentStatus map[string]string) (bool, string) {
internal/tools/tasks.go:5982      | func taskRowClaimable(rowTags, payload, parentID string, parentStatus map[string]string, skipSet map…
internal/tools/memory.go:4282     | func MemoryFeedback(id int, rating, reason, taskID string) (string, error) {
internal/scratch/tmproot.go:615   | func AssertTempRootOffRootFS() error {
internal/text2img/annotate.go:198 | func AnnotateFile(inPath, outPath string, marks []Mark, o AnnotateOptions) (*AnnotateResult, error) 
internal/sandbox/sandbox.go:213   | func Create(root, rootSource string, opts Options) (*Sandbox, error) {
internal/toolnames/names.go:108   | 	ToolScreenCapture = "screen_capture"
```

**对手侧落点（本机源码树，同样逐条 `sed -n` 打印）**：

```
/home/user/work/deepseek-harness/packages/sandbox/sandbox/src/index.ts:29        | export type SandboxMode = 'read-only' | 'workspace-write' | 'danger-full-access'
/home/user/work/deepseek-harness/packages/goal/tool-goal/src/index.ts:3          |  * persisted same-session goal domain.
/home/user/work/deepseek-harness/packages/subagent/tool-subagent-control/src/index.ts:26 |   ctx.tools.register(defineTool({
/home/user/work/deepseek-harness/packages/mcp/mcp-client/src/tools.ts:8          |  * is `mcp__<serverName>__<rawName>`, normalized to the DeepSeek function-name
/home/user/work/codex/codex-rs/tools/src/tool_spec.rs:22                         | pub enum ToolSpec {
/home/user/work/codex/codex-rs/core/src/tools/handlers/multi_agents/spawn.rs:69  |             "Agent depth limit reached. Solve the task yourself.".to_string(),
/home/user/work/hermes-agent/agent/learning_graph.py:29                          | class SkillNode:
/home/user/work/hermes-agent/skills/*/                                           | 21 个目录（apple … yuanbao）
```

### 5.3 census 口径分歧（如实列出，**不取平均、不取最大**）

同一个"面"在不同文档里分母不同 —— 这是信息，不是错误，**必须带生成器一起读**：

| 面 | 本文实测（`7e140378`） | `docs/archive/demo/materials/README.md` §6（`50f70a6c`） | 生成器差异 |
|---|---:|---:|---|
| 顶层命令 | **37**（行）/ 35（去重动词） | 37 / 35 | 同一口径，两个 commit |
| `task` 子命令 | 33（`^\s{2,4}[a-z]` 口径）/ **17**（`^  gopi task <verb>` 去重口径） | 17 | **口径不同**：后者是 README §6 的权威口径 |
| `memory` 子命令 | 17 / 10（去重） | 16 | 行数口径 16 vs 17：help 排版在两次测量间变了 |
| `sandbox` 子命令 | 5 / **4**（去重） | 4 | 去重口径一致 |
| `checkpoint` 子命令 | 3 | 3 | 一致 |
| 全局 flag | **23** | 23 | 一致（`^\s+--` 口径在历史上一度给出 0，是**错口径**，已弃用） |
| 环境变量 | **88** | 89 | `^\s+GOPI_` 口径；本次 88，README 89，差 1 —— 两次测量间仓变了 |
| **素材库覆盖率** | **159 / 193 complete** | 同 | 生成器 `scripts/materials-coverage.sh`，`193` 是**题面基线常量**（不在仓内），**不是实测值** |

**读法**：`193` 是**引用**，不是本仓事实；左右不等不是错误，是仓在动。
任何人要换分母，必须**同时**给出新分母的实测命令与新 commit —— 这正是 Rule 38 要求的。

### 5.4 本次运行实际观察到的结果

- `go build`：`BUILD_OK`，exit 0，20.1 s（job-1 的完整输出）。
- 四项「未做」的 grep：`0 / 0 / 0 / 0`（各自 `wc -l` 的输出，见 `E-tools-and-gaps.txt`）。
- 工具表读到 **17** 个名字，与 `GOPI.md:55` 的字面量一致。
- 素材库 census：`produced 159 / 193`，`candidate dirs 159 (complete=159 incomplete=0)`，
  `unreadable 0`。
- 13 条 `file:line` 抽查：**13/13 命中**（`E-spotcheck.txt`）。
- 竞品侧本机源码：hermes `skills/*/` = **21**；其 `cron/` 含 `scheduler.py`；
  dsh `mcp-client/src/tools.ts:8` 与 codex `spawn.rs:69` 均命中。

---

## 6. Not Done（诚实部分）

1. **本文没有自己跑四家产品的对照实验** —— 竞品事实**引自已完成的探测**
   （`docs/archive/demo/final/competitive.md` 的本地源码一手对比、
   `devdoc/competitor-claude-code-behavior.md` 的 7 条真跑探测）。
   本文只做了**对手侧源码落点的重新命中**（§5.1 末段），没有新增运行时观测。
2. **claude 一面仍是「未判」**：本机无可读源码（245 MB 打包 ELF），
   per-dimension 判定只能走真跑；本文沿用该边界，未把它读成「赢」或「平」。
3. **§2.6 的「部分刻意」是本文唯一一处双标注** —— 它确实同时是缺口与取舍。
   若后续有人要被它读成"我们都是刻意的"，**那句话是错的**，§2.6 已写明拆法。
4. **demo 骨架只给到节序与文件路径，没有写 HTML** —— 那是 01a0eea8 的工作；
   本文的交付边界是"论点 + 结构"，写页面即越界。
5. **census 数字随仓变化**：§5.3 的所有数字都在 `7e140378` 测得，
   换 commit 必须重跑生成器并更新本节（Rule 38）。
