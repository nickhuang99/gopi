<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# 竞争分析：gopi vs dsh（DeepSeek Harness）— 为什么 dsh 更强

> 首次一手源码对比（2026-09-30）。dsh 仓库 `/home/user/work/dsh-claude` = `/home/user/work/dsh-gopi`，两者同为
> `deepseek-harness` 的 checkout，**同一 commit `141eb6fef834`**（`0.1.0-rc.8`），差异只在构建产物（见 §1.2）。
> gopi 侧基线 `/home/user/work/GoPi` @ `203e8a40`。全部结论来自源码/generated 目录一手阅读，非二手描述。
> 每条结论带 `file:line`；判定只用 **{赢, 输, 平, 未判}**，`未判` = dsh 源码里没有对应物。
> 格式与深度对齐 `devdoc/gopi-vs-codex-competitive-analysis.md`。

---

## 0. 摘要（TL;DR）

dsh 在**工具模型、工作流、插件与依赖模型**三个层面是架构级领先，在**系统提示词**上输给 gopi 的密度与强制力，
在**记忆**上与 gopi 平（两条完全不同的路线），并有 4 个类别是 gopi 有、dsh 源码无对应物。

根因不是"gopi 哪里写错了"，而是**两者定位不同**：

| | gopi | dsh |
|---|---|---|
| 定位 | 单机自驱效率引擎（闭环强制记账 + 跨会话台账） | 可组合的 agent **平台**（everything is a plugin） |
| 形态 | 1 个 Go 二进制 + settings.json，零构建 | pnpm monorepo，226 个 workspace 包，Node 22+，必须装 node_modules 才能跑 |
| 工具模型 | 17 个扁平 `ClaudeToolDef` 全量塞 context，`switch` 硬编码分派 | 工具 = 插件（`defineTool` + `ctx.tools.register`），三级 waterfall 管线 + guard + 并发分类 |
| 工作流 | 单 agent ReAct + 强制 TASK_LIST/记账 | turn/step 事件溯源 + `run_code` Code Mode + subagent/team/workflow/ralph 四种委派面 |
| 系统提示词 | 11 个 section，~4.5K 硬预算，**只有 gopi 一个权威** | 28 处 `systemPrompt.section()` 分散在各包 + persona 空默认（deployment 选择） |
| 记忆 | SQLite `distilled_qa` + feedback/update 质量裁判（LLM 主动调用） | **无跨会话知识库**；可检索的是自己的 session 历史（FTS5） |
| 持久化 | append-only 任务台账 + memory.db | append-only `SessionEvent` 日志（一切事实的单一来源，消息历史**派生**） |
| 插件模型 | 无插件概念（工具/命令写死） | Cordis 插件树，profile/bundle/patch 分层，**agent 可运行时定义并挂载自己的插件** |
| 依赖 | 3 个直接依赖（其中 2 个是为了离线固定版本） | 226 包 workspace + 74 个 vendored Cordis 文件 + node_modules |
| 安全模型 | 危险命令黑名单 + `GOPI_ALLOW_DANGEROUS` + 沙箱 profile | 三档 sandbox mode + 审批策略 + **模型可主动申请提权的 escalation 配对** |
| 输出预算 | 简单 cap | token-meter 估算 + flush 时模型无关剪枝 + spill 落盘（head/tail 预览） |

**六维判定汇总**

| 维度 | 判定 | 一句话 |
|---|---|---|
| 1 工具模型 | **输** | dsh 工具是插件、可组合、并发可分类、三级管线可拦截；gopi 是一个 7282 行文件里的 `switch` |
| 2 工作流 | **输** | dsh 有 Code Mode、四种委派面、turn/step 事件溯源；gopi 单 agent + 强制记账 |
| 3 系统提示词 | **赢**（密度/强制力）/ **输**（可组合性/可部署性） | 见 §3.4：这是全报告唯一 gopi 净胜的维度 |
| 4 记忆 | **平**（两条不同路线，各有结构性缺失） | gopi 有跨任务蒸馏 + 质量裁判；dsh 有全文检索 + 日志即记忆 |
| 5 插件与依赖模型 | **输** | dsh 把"可替换"做成了机制，gopi 把"可扩展"做成了改代码 |
| 6 gopi 有而 dsh 无的面 | **赢**（6 类）/ 部分 `未判` | 台账、沙箱 profile、ui_state 格子、零依赖渲染、定价、桌面 act |

**一句话**：dsh 赢在**把"工具规模""多 agent 委派""可替换性"做成了系统机制**，gopi 把这些做成 prompt 提示词 +
硬编码分派；gopi 赢在**强制力与自持性**——它是 1 个二进制，它的 system prompt 只有它一个权威，
它的记忆跨会话、可投票、可纠正。**gopi 最大的三个输面是 §9 的 D1/D2/D3。**

---

## 1. 前置：材料、方法与 census

### 1.1 方法与边界

- 本报告**只读** dsh 源码（`/home/user/work/dsh-*`，禁写），不改 gopi 任何代码（纯研究）。
- gopi 侧事实全部来自 `/home/user/work/GoPi` 的当前工作树（HEAD `203e8a40`）。
- dsh 侧事实来自源码 + 其 generated 目录（`docs/tool-catalog.md`、`docs/tool-execution-pipeline.md`、
  `docs/persistence-catalog.md` 由 `scripts/gen-*.ts` 生成，`--check` 校验 freshness）——**generated 文件本身
  是从运行时注册表产出的，属于源码事实的投影**，不是二手文档；引用它们时同时给出产生它的脚本。

### 1.2 dsh-claude vs dsh-gopi：目录关系结论（任务明确要求）

**结论：同一 git 仓库的两个 checkout，同一 commit，差异仅为构建产物。**

| 证据 | 命令 | 结果 |
|---|---|---|
| 同 HEAD | `git -C dsh-claude rev-parse HEAD` / `git -C dsh-gopi rev-parse HEAD` | 两者均 `141eb6fef83422698aef7a981029e843e8161534` |
| 同 origin | `git -C <dir> remote -v` | 两者均 `origin /home/user/work/deepseek-harness` |
| 内容差异 | `diff -rq --exclude=.git --exclude=node_modules dsh-claude dsh-gopi` | **67 条，且方向单一 —— 全部是 `Only in /home/user/work/dsh-gopi`**（`grep -c '^Only in /home/user/work/dsh-gopi'` = 67，反向 0）：**49 条**是构建输出**目录**（`Only in <dir>: lib`/`: dist`，其中 44 条叫 `lib`、2 条 `dist`）+ **18 条**是这些目录内的**文件**（`*.js` / `*.js.map` / `*.tsbuildinfo`）。即 **dsh-gopi 被构建过**（`pnpm run build` 的产物），dsh-claude 未构建；**源码/文档/清单区 0 条**（`... \| grep -E '/src/\|package\.json\|/docs/' \| wc -l` → `0`） |
| 体积差 | `du -sh dsh-claude dsh-gopi dsh-claude/node_modules dsh-gopi/node_modules` | dsh-gopi 211M vs dsh-claude 1.6G —— 反直觉但已定位到来源：差额几乎全在 `node_modules`（dsh-claude **1.4G** vs dsh-gopi **55M**，两层条目数同为 31），与 tracked 源码无关 |

**对研究的影响：零。** 两个目录的 tracked 源码逐字节相同，`git ls-files` 一致；本报告统一以
`dsh-claude` 为引证路径（未构建、路径最短），所有 `file:line` 在 dsh-gopi 中同样成立。

### 1.3 Census（Rule 38：数字带生成器 + 测量 commit）

**dsh**（生成器 `git ls-files` / `ls`，测量于 commit `141eb6fef83422698aef7a981029e843e8161534`）：

| 度量 | 生成器 | 值 |
|---|---|---|
| tracked 文件总数 | `git ls-files \| wc -l` | 7807 |
| tracked `.ts` 文件 | `git ls-files '*.ts' \| wc -l` | 2438（其中 `.d.ts` 39 → 实现文件 2399） |
| tracked `.ts` 行数 | `git ls-files '*.ts' \| xargs cat \| wc -l` | 532353 |
| workspace 包目录（`packages/<group>/<pkg>/`） | `ls -d packages/*/*/ \| wc -l` | **226**；`git ls-files 'packages/*/*/package.json'` 同为 226。briefing 的"55 包"是 `packages/` **顶层条目**数：**50 个组目录 + 5 个顶层文件**（AGENTS.md / CLAUDE.md / README.md / README.zh.md / README.i18n.yaml）|
| vendored Cordis 文件 | `git ls-files 'vendor/**' \| wc -l` | 74（`vendor/*/package.json` = **9 个包**）|
| 模型可见工具名 | `grep -c '^### ' docs/tool-catalog.md` | 63 条 heading / **58 个不同名** / 27 个 `## ` 包段（同名重复：`bash`/`pwsh`/`send_message`/`list_agents`/`interrupt_agent` 各 2 —— 不同 provider 各注册一次）|
| `systemPrompt.section({` 生产注册点 | `grep -rn 'systemPrompt.section({' packages/ --include='*.ts' \| grep -v '/lib/' \| grep -v '/tests\?/' \| wc -l` | **28**（含测试文件则 72；不带 `{` 的调用 33）|

**关于 briefing 的"18088 源文件"**——这个数字的形状是"含 node_modules 的 `.ts` 数量"：

```
find . -path ./node_modules -prune -o -type f -name '*.ts' -print | wc -l   # 3299   (仓库自己的源码)
find node_modules -type f -name '*.ts' | wc -l                              # 14762  (依赖)
                                                             sum            # 18061
```
3299 的内部构成同样可复核（生成器 `git ls-files` + `find`，@`141eb6fef834`）：

```
git ls-files '*.ts' | wc -l                                                 # 2438  tracked
find . -path ./node_modules -prune -o -type f -name '*.ts' -print \
  | grep '/lib/' | while read f; do git ls-files --error-unmatch "${f#./}" \
      >/dev/null 2>&1 || echo x; done | wc -l                               #  861  未被 tracked
                                                             sum            # 3299  ✓
grep -nE '^(lib/|dist)' .gitignore                                          #    4:lib/
```
即 **那 861 个全部落在 `.gitignore:4` 的 `lib/`（`tsc` 输出），3299 与 2438 的差正好闭合**。
**18,061 与 briefing 的 18,088 差 27 —— 本 run 未能用任何生成器闭合这 27**：
`.mts`/`.cts` 在仓库非 node_modules 区各为 **0** 个、在 node_modules 区分别 **3936 / 232** 个（不是 27）；
`.tsx` 仓库 **262** / node_modules **1**（不是 27）。故 **报告不使用 18088**，只用上面可复现的数字。
（本报告早前一稿曾把这 27 归因于 `.mts/.cts` 变体后缀 —— **该归因实测为假，已撤回**。）
**结论：仓库自身源码 3299 个 `.ts`（含 861 个 gitignored 的 `lib/` 构建产物），**
**tracked 源码 2438 个 —— 报告正文一律用后者。**

**gopi**（生成器见命令，测量于 commit `203e8a402ca4d21027910fb53e3c407755219d54`）：

| 度量 | 生成器 | 值 |
|---|---|---|
| 生产 Go 文件 / 行 | `find internal cmd -name '*.go' -not -name '*_test.go' \| wc -l` / `... \| xargs cat \| wc -l` | 562 / 268742 |
| 工具名常量 | `grep -cE '^\s+Tool[A-Za-z]+ *= *"' internal/toolnames/names.go` | 29（含 `ToolChatSurface` 等非顶层工具动词） |
| 顶层 LLM 工具 | `internal/agent/loop.go:1168` 起的 `claudeToolDefs` 逐项 | **17** |
| 直接依赖 | `go.mod` `require` 块逐行 | 28 条条目 = **5 条直接 + 23 条 `// indirect`**；直接依赖是 `bubbletea`、`go-runewidth`、`modernc.org/sqlite`、`google/uuid`、`golang.org/x/sys` |
| 顶层 CLI 动词文件 | `ls cmd/agent/cli_*.go \| wc -l` | 264 |

---

## 2. 维 1：工具模型 —— **判定：输**

### 2.1 工具是「插件」还是「数组元素」

**gopi**（`internal/agent/loop.go:1168`）：17 个 `deepseek.ClaudeToolDef` 平铺在一个 slice 里，
一次性发给模型；分派是 `executeToolDefault` 里的巨大 `switch tc.Name`。加一个工具 = 改这个 7282 行的文件
（`internal/agent/loop.go` 实测 7282 行）。

**dsh**：
- 工具由插件注册进服务：`export class ToolRuntime extends Service`（`packages/core/tools/src/index.ts:787`），
  注册表是 `NamedEntries<ToolDefinition>`（同文件 `:715`）；
- 每个工具包自己声明 `inject`（它依赖哪些服务）与 `apply(ctx)`——例如
  `packages/shell/tool-bash/src/index.ts:31` `export const inject = ['tools', 'shell', 'systemPrompt', 'shellEnv']`、
  `:190` `export function apply(ctx, config)`；
- 工具用 `defineTool<...>({ name, parameters, execute })`（`packages/core/tools/src/schema.ts:545`）声明，
  schema DSL 同时投影 JSON Schema 与生成类型（`jsonschema.ts` / `ts-types.ts` / `py-types.ts`）；
- **加一个工具 = 加一个包 + 在 profile 里插一行**，`packages/bundle/base/cordis.patch.yml` 就是这样一份
  声明式清单（`:429` 起为 `system-prompt` 等的行）。

**为什么 dsh 赢**：gopi 的工具规模上限是"一个 slice 能塞多少 schema 进 system prompt"；dsh 的上限是磁盘。
gopi 的 `parallelSafeTools`（`internal/agent/loop.go:5527`）是一张手写白名单 map，dsh 是**工具自己声明**
（`ToolDefinition.isConcurrencySafe?(args)`，`packages/core/tools/src/index.ts:269`；调度器读它决定
`{kind:'parallel'|'exclusive'}`，同文件 `:1276-1282`）。

### 2.2 执行管线：`switch` vs 三级 waterfall

**dsh**（`docs/tool-execution-pipeline.md:6`，由 `scripts/gen-doc-graphs.ts` 生成，
`docs/tool-execution-pipeline.md:1` 即 GENERATED 头）：

```
tool/call → tools/pre-execute waterfall（hooks/permission/sandbox）
          → registered monotonic guards（deny 或 abstain）
          → ctx.approval（一次性询问；缺失或无法回答 = DENY，fail-closed）
          → tools/execute waterfall（timeout / retry / metrics，环绕 dispatch）
          → 工具 execute() body
          → tools/post-execute waterfall（accept / block / replace / add context）
          → ToolDefinition.finalizeContent
          → tools/result（冻结的权威结果）
```
每一级都能**改写**调用（waterfall）或**否决**它（guard），且全部不碰 agent loop。落在这套管线上的现成插件：
`packages/guard/timeout-policy/src/index.ts:25`（`TOOL_TIMEOUT`，把超时映射成结构化结果）、
`packages/guard/repeat-tool-reminder/src/index.ts:46`（`thresholds` 默认 `[3,5,8]`，连续重复调用提醒）、
`packages/spill/spill-policy/src/index.ts:4`（`tools/post-execute` 结果改写器：超 `maxInlineBytes` 落盘 + head/tail 预览）。

**gopi**：工具的横切关注点（超时、重复、截断）都在 loop 内部按名字特判。不是错，但**没有第三方可插的口子**。

**为什么 dsh 赢**：这是"策略住在插件里、loop 不知道策略存在"与"策略住在 loop 里"的差别。
dsh 的 sandbox/审批/hook 全部实现为 waterfall 监听器；gopi 的 `internal/sandbox` 是独立包但只被 CLI/loop 调用，
不是可替换的拦截层。

### 2.3 Code Mode：把工具变成 SDK（本维最大差距）

**dsh**（`packages/core/tools/src/code-mode.ts:20` `RUN_CODE_NAME = 'run_code'`；`:296` `createRunCodeTool(...)` 注册）：
模型不逐个调工具，而是**写一个 TypeScript/Python 程序**，在程序里 `await tools.name(args)`——
子调用走同一套调度（`isConcurrencySafe` 决定并发/屏障），每个子调用记 `tool/code-dispatch-start` /
`tool/code-dispatch` 事件（`packages/core/tools/src/types.ts:56`），**只有外层 curated 结果进模型历史**。
SDK 类型由 `packages/core/tools/src/ts-types.ts` / `py-types.ts` 生成进 system prompt；
三种呈现模式 `native | code | both`（`packages/core/tools/src/index.ts:665` 的 `Config.mode`，
schema 默认 `:791`），子调用并发上限 `maxParallelSubCalls`（`:776` 默认 10，`:673` 声明）。

**gopi**：无对应物 —— **判定：未判**。gopi 每个工具一次 `tool_use`，多步骤只能靠多轮。

**为什么这是量级差**：一次 `run_code` 能跑几十个子调用而只占一次模型往返；gopi 要多轮 × 每轮一次 LLM 调用。

### 2.4 安全/自治：黑名单 vs 三档沙箱 + 冻结配对

**gopi**：bash 黑名单 + `GOPI_ALLOW_DANGEROUS` 逃逸（无审批分层、无沙箱分级）。

**dsh**：
- `packages/sandbox/sandbox/src/index.ts:29`：`SandboxMode = 'read-only' | 'workspace-write' | 'danger-full-access'`；
- `packages/interaction/user-approval/src/index.ts:94`：`ApprovalPolicy = 'ask' | 'never'`（`:194` 默认 `'ask'`；
  `'never'` 时**每个 ask 都解析为 rejected**，fail-closed）；
- **模型侧协议**（`packages/shell/tool-bash/src/index.ts:83` 起的描述串）：描述里写明"命中沙箱拒绝是安全且预期的：
  读那个 marker，别假设；被拒后**同一轮内**用 `sandbox_permissions`（最窄的更高档）+ 一句 `justification`
  重试一次——这是被拒的唯一合法例外；先聊再问 = 错，那个重试弹出的审批就是用户的同意方式；
  不得投机提权"。
- 审批在 guard 之前解析（pipeline 图），且 `validateEscalationArgs`（同文件 `:67` 调用；
  定义在 `packages/sandbox/sandbox/src/escalation.ts:51`）强制
  `sandbox_permissions ⇔ justification` 配对——**权限申请本身是一个被校验的数据结构，不是一句自然语言**。

**为什么 dsh 赢**：gopi 的模型无法表达"`git fetch` 免审但 `curl` 外网需审"；dsh 的模型**自己会写提权申请**，
且申请必须有理由、必须有被拒的实证、必须是最窄档。这是把自治做成了**协议**。

### 2.5 输出预算：cap vs meter/prune/spill

**dsh**：`packages/llm/token-meter/src/estimate.ts:19` `export const ROLE_OVERHEAD = 4`（每消息的 role 框架开销，
`:57` `estimateContent(message.content) + ROLE_OVERHEAD`；`:13` `CHARS_PER_TOKEN = 4`、`:16` `BLOCK_OVERHEAD = 4`）；
`packages/compaction/compaction-tool-result-pruner/src/index.ts:2`
（**模型无关、replay-safe** 的 tool-result 剪枝服务）；`packages/spill/spill-policy/src/index.ts:2`（`tools/post-execute` 改写器，`:72` 声明依赖该 waterfall）落盘 + 预览。
**gopi**：简单截断。**判定：输**（细节，但同一模式的第三例）。

---

## 3. 维 2：工作流 —— **判定：输**

### 3.1 主循环：单 agent ReAct vs 事件溯源的 turn/step

**gopi**：`runEngine` 标准 ReAct + **闭环强制记账**（`maybeInjectTaskStartSearch`、
`maybeEnforceCompletionStore`、`maxConsecutiveEndTurnRounds = 2`（`internal/agent/loop.go:126`））。

**dsh**（`docs/architecture.md:65`）："A **step** 是 1 次模型请求 + 它调用的工具；一个 **turn** 是 0..n 个 step：
在第一个输入被 claim 前打开，在不再欠账时关闭"。日志是**唯一事实来源**，消息历史是**派生**的
（`docs/subsystems/session.md:5`："LLM message history is *derived* from the log, never stored separately"）。
事件词汇由 `scripts/gen-persistence-catalog.ts` 生成（`packages/core/session/src/known-event-types.ts:2` 的
GENERATED 头、`:19` `KNOWN_SESSION_EVENT_TYPES`，读路径**拒绝解释本 build 不认识的必需事件**——
即：新 harness 写的日志不被旧 harness 静默曲解）。

### 3.2 委派面：1 vs 4（本维最大差距）

| dsh 的委派面 | 证据 |
|---|---|
| `subagent` / `subagent_fork` | `packages/subagent/tool-subagent/src/index.ts:2`（模块头："Model-facing delegation through one configured `ctx.subagents` provider"）；深度由 `assertSubagentMaxDepth` 强制（该文件 `:279` 调用，定义在 `packages/subagent/subagent/src/depth.ts:42`；`:98` `maxDepth` 默认 3）|
| 生命周期控制 `send_message` / `interrupt_agent` / `list_agents` | `packages/subagent/tool-subagent-control/src/index.ts` |
| 子→父结构化汇报 `report` | `packages/subagent/tool-subagent-report/src/index.ts:65`（`childCtx.tools.register(defineTool({ name: 'report'`，注册在**子会话作用域**）|
| 团队 `spawn_teammate` / `team_task_*` / `wait_agent` | `packages/experimental/tool-agent-team/src/index.ts:174`（`name: 'spawn_teammate'`，按 Team Lead 作用域注册，`:160` `agent.ctx`）|
| `workflow`：写 JS 编排脚本扇出 subagent，返回脚本终值 | `packages/workflow/tool-workflow/src/index.ts:2`（模块头一句话即该契约）|
| `ralph`：固定工作流，每轮一个**全新**子 agent，只带不可变目标 + 有界交接 | `packages/workflow/tool-ralph/src/index.ts`，`maxRounds` 默认 256（`:37`） |

provider 可换：`subagent-acp` / `subagent-claude-code` / `subagent-codex` / `subagent-dsh-sdk` /
`subagent-fork-in-process`（`packages/subagent/` 11 个目录），能力不匹配**在启动时以 typed error 拒绝**而非静默降级
（`packages/subagent/subagent/src/types.ts:78`："is rejected with a typed error rather than accepted-then-ignored
(the \"fail loud, no silent …\""；`packages/subagent/tool-subagent/src/index.ts:292-299`
把 `maxDepth` 请求投到无 `depthLimit` 能力的 provider 上直接 throw）。

**gopi**：只有 `job`（后台 shell）——**判定：未判**（子 agent 维度源码无对应物）。

### 3.3 计划/目标/日程：状态机 vs 工具

**dsh**：`plan` 是**日志里的模式**（`packages/plan/plan-mode/src/index.ts:68` `EXIT_PLAN_MODE = 'exit_plan_mode'`，
`:326` `name: EXIT_PLAN_MODE` 注册为工具），plan 模式下工具集被收窄到 `['exit_plan_mode','read','write']`
（测试 `packages/plan/plan-mode/tests/integration.spec.ts:85` 断言 header 里的工具名单）；
`goal` 是持久化目标域（`packages/goal/tool-goal/src/index.ts:3` "persisted same-session goal domain"，
工具 `create_goal`/`get_goal`/`update_goal`），
`blockedAfterConsecutiveRounds` 默认 3（同文件 `:33`），并有 `authority.ts`/`wrapup.ts` 限制谁能改目标；
`schedule` 是会话内提醒（`packages/schedule/schedule/src/tools.ts:318` `name: 'schedule_create'`，
支持 `after_seconds`/`at`/`every_seconds`）。
**gopi**：`task`（会话内 working area + 跨会话 ledger）+ `task_submit`。**判定：平**——
gopi 的**跨会话**台账 dsh 没有，dsh 的 plan-mode/goal/schedule 三件套 gopi 没有。

### 3.4 todo：最后一个「平」的例子

**dsh**（`packages/todo/tool-todo/src/index.ts`）：`todo_write` 是**整表替换**（每次发全表，`:46` DESCRIPTION_HEAD
"Send the ENTIRE …"）、session-owned 状态（落成 `todo/write` 事件，UI 从事件渲染）、`allowParallelInProgress`
是**必填部署选择**（`:37` `allowParallelInProgress: boolean`，schema `:42` `.required()` 无默认）：
`true` 时描述邀请多个 `in_progress`，`false` 时描述要求恰好一个且多标即拒（`:129` 读 config）。
**gopi**：`task` 工具的 `view/commit/review/reframe`。**判定：平**（dsh 的"整表替换 + 部署选择"更干净）。

### 3.5 恢复/检查点

**dsh**：`compaction` 是**能力 seam**（`docs/subsystems/compaction.md`）：`compaction/start`（拿到日志记录的锁）
→ `summary` → `user/message` 替换（唯一作用于 surface 的变更）→ `compaction/end`；
**锁自身是日志事实**，所以崩溃留下的是"可检测的孤儿锁"而不是"谎称完成的 end"。
压缩比例默认 `thresholdRatio 0.8` / `retainRatio 0.16`（`packages/compaction/compaction-basic/src/config.ts:20,23`）。
**gopi**：`resume` + raw-log byte-exact。**判定：平**（gopi 的 raw-log 恢复是 dsh 没有的能力，见 §8）。

---

## 4. 维 3：系统提示词 —— **判定：赢（密度/强制力）+ 输（可组合性/可部署性）**

### 4.1 组装机制：28 个注册点 vs 1 个数组

**dsh**：`systemPrompt.section({ name, order, text })` 由**包自己**注册，全仓 28 处
（`grep -rn 'systemPrompt.section({' packages/*/*/src/*.ts | wc -l` = 28），例如
`packages/shell/tool-bash/src/index.ts:236`、`packages/fs/tool-fs/src/read.ts:70`、`:edit.ts:77`、`:write.ts:63`、
`packages/lsp/tool-lsp/src/index.ts:104`（`order: 112`）、`packages/extensions/tool-cordis/src/index.ts:36`（`order: 115`）；
`packages/core/system-prompt/src/index.ts:57` 定义约定 **`-100` = harness 身份、`0` = deployment persona、
`100–199` = 工具指引**（`PERSONA_ORDER = 0`，同文件 `:131`）。
**但 persona 默认是空串**：`packages/bundle/base/cordis.patch.yml:432` `persona: ''`，注释写明
"The deployment persona is a deployment choice"；`packages/bundle/web-app/cordis.patch.yml:18` 与
`packages/bundle/headless/cordis.patch.yml:9` 各自填一句 `You are a coding agent powered by the {{model}} model.
Your working directory is {{cwd}}.`。

**gopi**（`internal/agent/loop.go:1072`）：11 个 section 一个数组，Order **0,5,10,20,30,40,50,55,60,62,65**
（实测 `awk` 提取），全部在**同一个文件**里，
并有一条硬密度预算：`internal/agent/loop.go:1088` "Density target: <4.5K characters"，实测 4495/4500
（`internal/agent/prompt_density_budget_test.go:34` `systemPromptDensityBudget = 4500`、
`internal/agent/storetiming_test.go:35` "measured 4495/4500"）。

### 4.2 判定（分开说）

- **赢（密度与强制力）**：gopi 的 prompt 是**一个有预算的单一权威**——4.5K 硬顶、有测试守、每一条规则都是
  某次竞赛暴露缺陷后打的补丁（`headerRules` `internal/agent/loop.go:7282` 只有一句话；工具路由常量在 `:693`）。
  dsh 的 prompt 是 28 个包各写各的，**总量无预算、persona 默认空**。
- **输（可组合性与可部署性）**：dsh 加一个工具 → 它自己的指引段落自动进 prompt，与工具生命周期同生共死
  （注册是 effect，卸载即回退——`docs/architecture.md:13` "registrations are effects that unwind when their
  plugin unloads"）；gopi 加一个工具 → 必须去 `internal/agent/loop.go` 里改 `toolRoutingRules`。
- **输（情境注入）**：dsh 另有 3 处**生产**代码的 `systemPrompt.context`（`packages/interaction/user-approval/src/index.ts:205`、
  `packages/sandbox/sandbox-policy/src/index.ts:113`、`packages/subagent/subagent/src/child-agent.ts:170`）——
  把**当前策略**（审批模式、沙箱模式、子 agent 委派规则）作为动态上下文注入，即模型每轮都知道自己处在什么权限下
  （child-agent 处注释写明 order 120 排在 sandbox:policy 110 与 approval:policy 115 之后；带测试共 24 处）。
  gopi 没有这一层。

### 4.3 gopi 没输的地方（诚实清单）

dsh **没有** gopi 那种"强制记账"的 prompt 机制——gopi 在工具描述里写死了任务开始必 `memory(action=search)`、
结束必 `memory(action=store)`（`internal/agent/loop.go:1372` 的 `memory` 描述里逐条列明
`action=search` 是 READ-ONLY/parallel-safe、投票规则、空结果怎么办）。dsh 的工具描述写的是**用法**，
gopi 的工具描述写的是**纪律**。这是 gopi 的差异化，也是它 prompt 密度高的原因。

---

## 5. 维 4：记忆 —— **判定：平**（两条不同路线）

### 5.1 gopi：跨任务蒸馏 + 质量裁判

- `internal/tools/memory.go:118` `distilled_qa` 表（topic/question/answer/sources/evidence/confidence/
  distilled_at/retrieval_count/tier/search_kw/jieba_token/lesson_type/when_at/exam_questions/**kw_what/why/when/where/who**）
  ——5W1H 检索面包屑是**表结构**，不是约定；
- `internal/tools/memory_procedural.go:98` 另有 `procedural` 表（可复用工作流）；
- `internal/tools/memory.go:4390` `MemoryFeedback(id, rating, reason, taskID)` —— **LLM 是唯一质量裁判**，
  投票直接改 confidence 并影响检索排序；`memory(action=update)` 是硬纠正。
- 由 prompt 强制 + 代码闸门保证"先搜后存"。

### 5.2 dsh：没有跨会话知识库，但把 session 历史做成了可检索语料

- 全仓源码里**没有** memory/knowledge 类包：`packages/` 下无 `memory*`/`knowledge*`；
  `grep -rln "memory_store\|knowledge base\|long-term memory" packages/*/*/src/` 只命中
  `packages/workflow/tool-ralph/src/index.ts`，且那里说的是**哲学**：
  `packages/workflow/tool-ralph/src/index.ts:159`（prompt 串内）——
  "The shared workspace and its current working tree are the long-term memory and source of truth"；
  `:182` 同义重申。
- `packages/session-query/session-query-sqlite/src/index.ts:2`
  "Concrete session-query service with SQLite FTS5 over the live-preferred corpus"，
  五个只读工具 `session_event_read|search|trace` / `session_search|trace`
  （`docs/tool-catalog.md`），`openAt: 'startup' | 'first-search' | 'never'`（同文件 `:201` 默认 `'startup'`，
  web profile 设为 `:memory:` + `never`，见 `packages/bundle/web-app/cordis.patch.yml:32-33`）。
  **它检索的是自己的历史，不是跨任务蒸馏的结论。**
- 人手反馈：`packages/feedback/command-feedback/src/index.ts:2` —— `/feedback` 只追加一条**log-only** 事件，
  **不启动模型工作**，也不进入任何知识库。
- `ralph` 的记忆形式 = 每轮 16K 字符的**有界结构化交接**（`maxHandoffChars` 默认 16384，
  `packages/workflow/tool-ralph/src/index.ts:38`），
  且故意不把上一轮报告当权威（`:159` 同句后半："Treat the previous report only as a bounded handoff; confirm it against the workspace"）。

### 5.3 为什么判「平」

| | gopi 有 | dsh 有 |
|---|---|---|
| 跨会话蒸馏结论 | ✅ `distilled_qa` | ❌（明确把工作区当记忆） |
| 质量裁判/纠正 | ✅ feedback + update | ❌（`/feedback` 是 log-only） |
| 历史全文检索 | ❌（只有 `gopi log` 的过滤器） | ✅ FTS5 + 5 个工具 + trace 谱系 |
| 日志即真相 | ❌（台账和 memory 是另外的表） | ✅ 单一 append-only 事件流，消息历史派生 |
| 崩溃一致性 | 部分 | ✅ 锁本身是日志事实（`docs/subsystems/compaction.md`） |

**两条路线各有结构性缺失。** gopi 缺"可检索的历史"，dsh 缺"跨任务知识沉淀"。

---

## 6. 维 5：插件与依赖模型 —— **判定：输**

### 6.1 dsh：everything is a plugin，且有分层与回退

- `README.md:7` / `AGENTS.md:3`：`**everything is a plugin**`，基于 vendored Cordis
  （`vendor/README.md:9` `## Manifest` 表列 9 个 vendored 包 + upstream commit，`:29` `## Local modifications`
  维护**本地修改日志 18 条**，例如 `cordis/src/fiber.ts` 的生命周期加固）；
- `docs/architecture.md:13`："**There is no privileged core to patch**: you extend dsh by mounting a plugin
  beside the others, and registrations are effects that unwind when their plugin unloads."
- `docs/architecture.md:19/21`：**profile**（命名的组合，存 Harness home）与 **bundle**（分发单位，
  在 `package.json` 的 `dsh.profile` / `dsh.bundle` 字段自声明）；分层顺序 = bundle 列表 → profile 的
  `cordis.patch.yml` → home 级 patch → `--patch` overlay，patch 按 **row id** 定位并整块替换 config；
  `dsh --profile web --dump-config` 打印真实树。
- **agent 自己改自己的运行时**：`packages/extensions/tool-cordis/src/index.ts:2`
  "Model-facing Cordis runtime/package inspection, define, run, stop, and remove tools"——
  模型可见的工具包括 `cordis_define` / `cordis_run` / `cordis_stop` / `cordis_undefine` / `cordis_inspect_self`
  （`packages/extensions/tool-cordis/src/index.ts:149,241,330,352,97`），
  即**模型可以在运行中定义并挂载一个新插件**（`packages/extensions/cordis-host-runner/`）。
- 外部世界：MCP 工具桥（`packages/mcp/mcp-client/src/tools.ts:8`，命名 `mcp__<serverName>__<rawName>` 并归一化
  到函数名合法字符集）、Claude Code hook 桥（`packages/hooks/hooks-claude-code/src/index.ts:2`，
  支持 SessionStart / prompt / tool pre-post / Stop / subagent start-stop 的**未修改** hook 脚本）。

### 6.2 gopi：无插件概念

gopi 的扩展方式 = 改 562 个生产 Go 文件之一 + 重新编译。工具、命令、prompt 都是写死的。
**这不是懒**——`internal/tools/` 里有 497 个 `.go` 文件在解决别的问题（台账、worktree、merge、scratch GC）。
但作为**平台**，gopi 输得清楚：没有 profile、没有 patch、没有第三方扩展点、没有运行时挂载。
**判定：输**（架构级差距，同 codex 报告 §1.1 的同一结论在 dsh 上重演）。

### 6.3 依赖预算：这里 gopi 赢

`go.mod` 直接依赖 28 条条目（**5 条直接 + 23 条 `// indirect`**），真正的直接依赖是 `bubbletea`、`go-runewidth`、
`modernc.org/sqlite`、`google/uuid`、`golang.org/x/sys`；其余是为**离线构建**固定版本而显式化的传递依赖。
dsh 是 226 包 workspace（`git ls-files 'packages/*/*/package.json'` = 226，含 `packages/examples` 等；
briefing 的"55 包"是 `packages/` 顶层条目 = 50 组目录 + 5 个文件）+ 74 个 vendored 文件 + 14762 个 node_modules `.ts`，且
`package.json:11` 的 `workspaces` 要求 pnpm（`package.json:7` `"packageManager": "pnpm@11.7.0"`）+ Node
`^22.19.0 || >=24.0.0`（`package.json:8-9`）。
**"能否考到一台机器上跑起来"这一条，gopi 完胜**——这是 Rule 35 的物质基础，也是 dsh 不可能复制的能力。

---

## 7. 维 6：gopi 有而 dsh 无的面 —— **判定：赢 6 类 / 部分 `未判`**

| # | 面 | gopi 证据 | dsh 源码状态 | 判定 |
|---|---|---|---|---|
| 1 | **跨会话任务台账**（claim / 交付物 / 验证证据 / 父依赖闸门） | `internal/tools/tasks.go:295` `CREATE TABLE tasks(... parent_task_id, claimed_by, deliverable, verify_evidence, run_cmd, ended_at ...)`；`:6943` `taskParentGateDenied`、`:5982` `taskRowClaimable` | 无对应物；dsh 的 `goal` 是**同会话**目标（`packages/goal/tool-goal/src/index.ts:3` "persisted same-session goal domain"），`todo` 是会话内清单 | **赢** |
| 2 | **沙箱 profile（可执行环境）** | `internal/sandbox/sandbox.go:1` "the ONE way an agent gets an isolated gopi profile"，`GOPI_HOME` 指向沙箱后 `gopi log --last` 可证 | dsh 的 `sandbox` 是**文件访问模式**（`read-only`/`workspace-write`/`danger-full-access`，`packages/sandbox/sandbox/src/index.ts:29`），不是"另一个可运行的 profile" | **赢** |
| 3 | **`ui_state` 格子级屏幕读取 + `act` 桌面驱动 + `screen_capture` 工具** | `cmd/agent/cli_uistate.go:3`（读 `gopi ui` 的 CELL GRID，无语义模型；`:28` 说明"刻意做成 CLI 子命令而非新工具"）；`cmd/agent/cli_act.go:1-12`（纯 Go XTEST 驱动，理由写在文件头）；`internal/toolnames/names.go:108` `ToolScreenCapture` | 无对应物（dsh 只有 web UI 与终端会话 `packages/terminal/tool-terminal/src/index.ts:163` `terminal_open`，没有"读自己屏幕"的概念） | **未判**（源码无对应物） |
| 4 | **零依赖纯 Go 渲染**（text2img / annotate / 自研 SFNT 解析） | `internal/text2img/render.go:1`（"pure-Go TrueType rasterizer — zero external dependencies"）、`cmd/agent/cli_text2img.go` | 无对应物（dsh 的"图"是浏览器渲染的客户端 UI，`packages/client/ui-*` 34 个包，依赖 React 工具链） | **未判** |
| 5 | **分时定价成本核算**（DeepSeek 高峰 / GLM 五折窗口） | `internal/pricing/pricing.go:46` `type Provider`、`:159` 北京时间固定区、`:163` `glmDiscountEnd`、`:175` `dsPriceCutover` | 无对应物：`packages/llm/token-meter` 只做 token 估算与计量，源码里无价格/货币概念（`packages/llm/token-meter/src/estimate.ts:13,16,19` 是 `CHARS_PER_TOKEN`/`BLOCK_OVERHEAD`/`ROLE_OVERHEAD` 三个常量，不是钱） | **未判** |
| 6 | **raw-log byte-exact 恢复** | gopi `resume`（`internal/agent/resume.go:50` `LoadCheckpoint` + `cmd/agent/cli_resume.go`） | dsh 的恢复是**日志重放 + 派生**（`docs/subsystems/session.md:5`），机制不同且更可移植，但不提供 byte-exact 的原始流 | **平** |

---

## 8. gopi 输在哪里（必写，按严重度排序）

**D1（最严重）— 工具模型不可组合。** gopi 的工具是 17 个扁平定义 + 一个 `switch`；dsh 的工具是插件，
自带指引段落、并发分类、生命周期回退，并且可以被 waterfall 拦截。**后果**：gopi 无法在不改核心文件的前提下
接入任何第三方能力（MCP / 插件 / 运行时挂载），而 dsh 的 `cordis_define` 让**模型自己**扩展运行时。
**最小可行动作**：把 `claudeToolDefs`（`internal/agent/loop.go:1168`）+ `executeToolDefault`
（`internal/agent/loop.go:5818`）里 `:5830` 起的 `switch tc.Name` 换成
`map[toolName]handler` + 每工具一个文件（codex 报告已提过同一项，dsh 再次证实这是行业共识形态）。

**D2 — 没有 Code Mode。** 一次 `run_code` = 几十个子调用 + 一次模型往返；gopi 是一轮一次调用。
这是**吞吐量**的量级差，且它与 gopi 的"节约 token"目标是同一方向的。

**D3 — 没有子 agent 委派面。** dsh 有 subagent / fork / 控制 / 汇报 / team / workflow / ralph 七件套，
且深度有闸门、能力不匹配 fail-loud；gopi 只有 `job` 后台 shell。**后果**：大任务只能串行。

**D4 — 系统提示词不可组合。** gopi 的 prompt 只有 gopi 一个作者；dsh 的 28 个注册点让工具与指引同生共死，
并且有 `systemPrompt.context` 做动态策略注入（模型每轮知道自己处在什么权限下）。

**D5 — 输出预算机制粗糙。** 无 token 估算器、无 replay-safe 剪枝、无 spill 落盘预览（gopi 只有 cap）。

**D6 — 无审批分层（部分已被沙箱 profile 缓解）。** gopi 只有黑名单 + 环境逃逸；
dsh 是 `ask|never` 策略 + 三档沙箱 + **模型侧的提权协议**（带理由、以被拒为实证、最窄档）。

**注**：D1/D2/D3 都指向同一个根因——gopi 的扩展面是**编译期**的，dsh 的是**运行时**的。

---

## 9. 抽查证据（5 条 file:line，任务验收项）

| # | 结论 | 引证 | 校验命令（已于本 run 逐条执行） |
|---|---|---|---|
| 1 | 工具是插件注册、注册表是 `NamedEntries<ToolDefinition>` | `packages/core/tools/src/index.ts:715`（`:787` `class ToolRuntime extends Service`） | `sed -n '715p;787p' packages/core/tools/src/index.ts` → `  readonly tools: NamedEntries<ToolDefinition>` / `export class ToolRuntime extends Service {` |
| 2 | 模型可写代码批量调工具（Code Mode） | `packages/core/tools/src/code-mode.ts:20` | `sed -n '20p' packages/core/tools/src/code-mode.ts` → `export const RUN_CODE_NAME = 'run_code'` |
| 3 | 输出/重复/超时是**可插的 guard 插件**，不是 loop 特判 | `packages/guard/repeat-tool-reminder/src/index.ts:46`、`packages/guard/timeout-policy/src/index.ts:25` | `sed -n '46p' .../repeat-tool-reminder/src/index.ts` → `  thresholds: z.array(z.number()).default([3, 5, 8]),`；`sed -n '25p' .../timeout-policy/src/index.ts` → `export const TOOL_TIMEOUT = 'TOOL_TIMEOUT'` |
| 4 | 沙箱三档 + 审批两档是**枚举类型**（不是文档描述） | `packages/sandbox/sandbox/src/index.ts:29`、`packages/interaction/user-approval/src/index.ts:94` | `sed -n '29p' packages/sandbox/sandbox/src/index.ts` → `export type SandboxMode = 'read-only' \| 'workspace-write' \| 'danger-full-access'`；`sed -n '94p' .../user-approval/src/index.ts` → `export type ApprovalPolicy = 'ask' \| 'never'` |
| 5 | 会话日志是 append-only 单一事实来源，消息历史**派生** | `docs/subsystems/session.md:5`（由 `packages/core/session` 产出，事件词汇 generated by `scripts/gen-persistence-catalog.ts`，见 `packages/core/session/src/known-event-types.ts:2,19`） | `grep -n 'append-only log' docs/subsystems/session.md` → `5:The in-memory, event-sourced model ... A \`Session\` is an **append-only log** ...` |

**gopi 侧对应引证（抽查对照）**：`internal/agent/loop.go:1168`（`var claudeToolDefs = []deepseek.ClaudeToolDef{`）、
`:1072`（`var promptSections = []PromptSection{`）、`internal/toolnames/names.go:52`（`ToolRead = "read"`）、
`internal/tools/tasks.go:295`（`CREATE TABLE IF NOT EXISTS tasks (`）、`internal/tools/memory.go:118`（`distilled_qa`）。

---

## 10. 方法学备注与未判清单

- 全部结论为源码/生成物第一手阅读，未使用任何二手描述；引用的 generated 文档（tool-catalog、tool-execution-pipeline、
  session/persistence 子系统页）**同时给出产生它们的脚本**，可复核。
- **6 处 `未判`（源码无对应物）**：Code Mode、子 agent 委派、`ui_state` 格子读取、桌面 `act`、
  零依赖渲染、分时定价。前两者是 dsh 有 gopi 无（已计入 §8 的输面）；后四者是 gopi 有 dsh 无（§7）。
- **两处刻意排除**（不属于本报告口径）：
  - dsh 的 `packages/client/ui-*`（**34 个** UI 包）与 `apps/web` 属于**产品外壳**，不计入 agent 能力对比；
  - dsh 的 Python/ACP/SDK 三个入口（`python/`、`packages/acp`、`packages/sdk`）是**分发面**，只影响集成不改变内核。
- **诚实清单**：gopi 在 §4（prompt 密度/强制力）、§6.3（依赖预算）、§7（6 类独有能力）上净胜；
  dsh 的 persona 默认为空是**设计选择**（deployment choice）而非缺陷，但它的后果——没有任何一处是
  "整个系统的提示词"——是真实的工程差异。
- 本报告**不提议**把 gopi 改成 dsh：dsh 的形态需要 Node + pnpm + node_modules，
  与 Rule 35（零依赖是执着理念）直接冲突。可借鉴的是**形态**（注册表 + 管线 + 委派），不是**依赖**。

---

## 11. 结论与 gopi 借鉴优先级（Top 行动项）

按撬动力排序（与 codex 报告 §7 的结论高度重合，可作为**行业共识的第二次独立验证**）：

1. **【高】工具注册表化**：`map[name]handler` + 每工具一文件 + 工具自带 prompt 段落与并发分类
   （对应 D1；codex 的 `ToolRegistry`、dsh 的 `NamedEntries<ToolDefinition>` 是同一形态）。
2. **【高】Code Mode（自研版）**：不做 TS/Python 沙箱，只做"一次工具调用内多步"的窄版——
   例如让 `multi_edit` 支持一次多文件，或让 bash 动词承载已有多步（gopi 已有 `gopi act` 这类"动词优先"的先例）。
3. **【高】子 agent 委派**：至少一个带深度上限、能力 fail-loud、结构化汇报的子 agent 通道。
4. **【中】prompt 组合化**：把 `promptSections`（`internal/agent/loop.go:1072`）从"一个文件"改成"工具自带段落 +
   中心预算校验"——保留 4.5K 硬顶（这是 gopi 的赢面），但允许段落由工具贡献。
5. **【中】输出预算三段式**：token 估算 → replay-safe 剪枝 → spill 落盘预览。
6. **【中】审批/沙箱协议化**：把"提权需理由 + 需被拒实证 + 最窄档"这套协议抄进来（gopi 已有沙箱 profile 底座）。
7. **【低】策略动态注入**：把当前审批/沙箱/委派模式作为动态上下文注入 prompt（dsh 的 `systemPrompt.context`）。

---

## Verification

- **Build**: 不适用 —— 纯只读研究，未改动任何 gopi 代码，无构建。
  实测：`git diff --stat HEAD -- '*.go'` → 空；`git diff --stat HEAD` → 只有本报告与 `devdoc/DOC-INDEX.tsv`。

- **Tests**: 引证完整性（本报告全部 `file:line` 断言）+ 文档语料一致性（Rule 36）。
  两个脚本都在 session 目录，不在 worktree（Rule 30）：

  ```
  $ python3 ~/.gopi/sessions/01a0eeb1-67b5-733e-84b9-e7710eed2a00/check_refs.py
  refs_resolved_ok=89
  refs_bad=0
  exit=0
  $ gopi doccheck
  doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
  ```

  **`check_refs.py` 自身有两个缺陷，本 run 已实测并修好（Rule 15：不静默）。**
  此前的 "62/62 命中" 是**假绿**，不是证据：

  1. **扩展名白名单漏了 `go`** —— v1 的正则是 `\.(ts|md|yml|json)`，于是**gopi 侧的每一处 `.go:NNN` 引证都被静默跳过**，
     而脚本仍打印一个干净的数字。同一份定稿报告上对比两个正则（可复现）：

     ```
     $ python3 -c "import re;t=open('<report>').read();
       v1=re.compile(r'\`([A-Za-z0-9_./-]+\.(?:ts|md|yml|json)):(\d+)\`');
       v2=re.compile(r'\`([A-Za-z0-9_./-]+\.(?:ts|tsx|md|yml|yaml|json|go|sql|sh)):(\d+)\`');
       print(len({m.group(0) for m in v1.finditer(t)}), len({m.group(0) for m in v2.finditer(t)}))"
     v1 unique=62  (其中含 '.go:' 的：0)
     v2 unique=85  (其中含 '.go:' 的：28)
     ```

     即 v1 看不见 **28 处** `.go` 引证（`.go` 在它的唯一命中集里为 **0**）。修法：扩展名加上 `go|tsx|yaml|sql|sh`，
     在**首轮修正前**的报告上重跑得 `refs_resolved_ok=89, refs_bad=0`；定稿后为 85 处唯一引证、`refs_bad=0`。
     **半份证据没被审计却报绿 —— 这比不审计更坏。**
     **半份证据没被审计却报绿 —— 这比不审计更坏。**
  2. **只验存在性，不验内容** —— 行号落在注释、类头或相邻声明上照样通过。本 run 另跑内容级复核
     `refs_content2.py`（session 目录 `refs-content2.txt`，逐条打印"引证行内容 vs 该句断言"），
     对 89 处逐行人工比对，**修正 21 处**，全部是"行号指向注释/相邻行而非断言所引之物"：

     | 原引证 | 改为 | 原因 |
     |---|---|---|
     | `packages/shell/tool-bash/src/index.ts:34` | `:31` | `:34` 是 `export interface Config {`；`inject` 在 `:31` |
     | `internal/agent/loop.go:5508` | `:5527` | `:5508` 是注释，map 声明在 `:5527` |
     | `packages/core/tools/src/index.ts:654` | `:665` | `:654` 是 `export interface Config {`；`mode?:` 在 `:665`，schema 默认在 `:791` |
     | `packages/core/tools/src/code-mode.ts:298` | `:296` | `createRunCodeTool` 声明在 `:296` |
     | `packages/shell/tool-bash/src/index.ts:84` | `:83` | 提权协议描述串自 `:83` 起 |
     | `packages/compaction/compaction-tool-result-pruner/src/index.ts:1` | `:2` | `:1` 是 `/**`，断言句在 `:2` |
     | `packages/llm/token-meter/src/estimate.ts:3,18` | `:19,:57,:13,:16` | `:3`/`:18` 是注释；常量与用法在 `:13/:16/:19/:57` |
     | `packages/core/session/src/known-event-types.ts:1` | `:2,:19` | `:1` 是 `/**`；GENERATED 头在 `:2`，集合在 `:19` |
     | `internal/agent/prompt_density_budget_test.go:23` | `:34` | `:23` 是注释；常量 `systemPromptDensityBudget = 4500` 在 `:34` |
     | `internal/agent/loop.go:693` | `:693`（措辞已改为"工具路由常量"） | 该行是 `const toolRoutingRules =` 的起始，措辞不精确 |
     | `packages/subagent/tool-subagent/src/index.ts:433` | `packages/subagent/subagent/src/index.ts:433` | `assertSubagentMaxDepth(request.maxDepth)` 在 subagent 包，不在 tool-subagent |
     | `packages/subagent/tool-subagent-report/src/index.ts:54` | `:65` | `:54` 是 `systemPrompt.section`；工具注册在 `:65`（原句另一断言"能穿透全局 toolFilter"**无源码支撑，已删**） |
     | `packages/experimental/tool-agent-team/src/index.ts:164` | `:174` | `:164` 是 section；`name: 'spawn_teammate'` 在 `:174` |
     | `packages/workflow/tool-workflow/src/index.ts:1` | `:2` | `:1` 是 `/**`，契约句在 `:2` |
     | `packages/goal/tool-goal/src/index.ts:1` / `:31` | `:3` / `:33` | 同上；默认值 3 在 `:33` |
     | `packages/workflow/tool-ralph/src/index.ts:29` | `:38` | `:29` 是类型声明；schema 默认 16384 在 `:38` |
     | `vendor/README.md:11` | `:9,:29` | `:11` 是正文；`## Manifest` 在 `:9`、`## Local modifications` 在 `:29` |
     | `packages/feedback/command-feedback/src/index.ts:1` | `:2` | `:1` 是 `/**` |
     | `packages/hooks/hooks-claude-code/src/index.ts:1` | `:2` | 同上 |
     | `packages/extensions/tool-cordis/src/index.ts:1` | `:2`（工具名另给 `:149,241,330,352,97`） | `:1` 是 `/**` |

  **同时撤回 4 处数字/归因错误（实测证伪）：**

  - **`vendor/README.md` 的 "8 个 vendored 包 / 8 条本地修改"** → 实测 **9 个包**
    （`git ls-files 'vendor/*/package.json' | wc -l` = 9）、**18 条本地修改**
    （`awk '/^## Local modifications/,/^## Sync procedure/' vendor/README.md | grep -cE '^[0-9]+\. '` = 18）。
  - **`packages/client/ui-*` "40 个"** → 实测 **34 个**（`ls -d packages/client/ui-*/ | wc -l`）。
  - **§1.3 的 "18,061/18,088 差 27 = node_modules 下的 `.mts`/`.cts` 变体"** → **实测为假**：
    仓库非 node_modules 区 `.mts`/`.cts` 各 **0** 个，node_modules 区分别 **3936 / 232** 个，`.tsx` 为 262/1 —— 无一是 27。
    该归因已删除，报告改用可闭合的 `3299 = 2438 tracked + 861 gitignored lib/`，并声明**这 27 差未能用任何生成器闭合**。
  - **§1.2 的 "67 条差异"** 由不可闭合的 `66` 改为实测 **67**，并补上方向与拆分：
    全部 `Only in dsh-gopi`（反向 0），**49 条构建输出目录 + 18 条目录内文件**；
    `du` 差额来源由猜测的"node_modules 残留"改为一手测量（dsh-claude `node_modules` 1.4G vs dsh-gopi 55M）。

  脚本口径（v2 定稿）：正则 `` `([A-Za-z0-9_./-]+\.(ts|tsx|md|yml|yaml|json|go|sql|sh)):(\d+)([-,](\d+))?` ``，
  base 按 `internal/|cmd/|devdoc/` 前缀路由到 `/home/user/work/GoPi`，其余路由到 `/home/user/work/dsh-claude`；
  文件必须存在且 `1 <= line <= 文件行数`。
  **已知残余局限（诚实声明）**：脚本按 `path:line` 的**完整路径**匹配，故报告里以 `:NNN` 简写承接
  同一文件的次要引证（如 `(…index.ts:787)` 之后的 `:269`）**不在自动审计范围内**，
  由内容级复核 `refs-content2.py` 覆盖 —— 该脚本按行取上下文，能看见简写。

- **Ran**（逐条 verbatim；除注明外均在该目录下执行）：

  ```
  # 目录关系（§1.2）
  $ cd /home/user/work/dsh-claude && git rev-parse HEAD
  141eb6fef83422698aef7a981029e843e8161534
  $ cd /home/user/work/dsh-gopi && git rev-parse HEAD
  141eb6fef83422698aef7a981029e843e8161534
  $ git -C /home/user/work/dsh-claude remote -v
  origin	/home/user/work/deepseek-harness (fetch)
  origin	/home/user/work/deepseek-harness (push)
  $ cd /home/user/work && diff -rq --exclude=.git --exclude=node_modules dsh-claude dsh-gopi > /tmp/d.txt; wc -l < /tmp/d.txt
  67
  $ grep -c '^Only in /home/user/work/dsh-gopi' /tmp/d.txt ; grep -c '^Only in /home/user/work/dsh-claude' /tmp/d.txt
  67
  0
  $ grep -cE ': (lib|dist|client|\.dsh-build)$' /tmp/d.txt ; grep -vcE ': (lib|dist|client|\.dsh-build)$' /tmp/d.txt
  49
  18
  $ diff -rq --exclude=.git --exclude=node_modules dsh-claude dsh-gopi | grep -E '/src/|package\.json|/docs/' | wc -l
  0
  $ du -sh dsh-claude dsh-gopi dsh-claude/node_modules dsh-gopi/node_modules
  1.6G	dsh-claude
  211M	dsh-gopi
  1.4G	dsh-claude/node_modules
  55M	dsh-gopi/node_modules

  # 抽查 5 条（§9，本 run 重跑）
  $ sed -n '715p;787p' packages/core/tools/src/index.ts
    readonly tools: NamedEntries<ToolDefinition>
  export class ToolRuntime extends Service {
  $ sed -n '20p' packages/core/tools/src/code-mode.ts
  export const RUN_CODE_NAME = 'run_code'
  $ sed -n '46p' packages/guard/repeat-tool-reminder/src/index.ts
    thresholds: z.array(z.number()).default([3, 5, 8]),
  $ sed -n '25p' packages/guard/timeout-policy/src/index.ts
  export const TOOL_TIMEOUT = 'TOOL_TIMEOUT'
  $ sed -n '29p' packages/sandbox/sandbox/src/index.ts
  export type SandboxMode = 'read-only' | 'workspace-write' | 'danger-full-access'
  $ sed -n '94p' packages/interaction/user-approval/src/index.ts
  export type ApprovalPolicy = 'ask' | 'never'
  $ sed -n '56p' packages/core/tools/src/types.ts
      'tool/code-dispatch': CodeDispatchEventData
  $ grep -n 'append-only log' docs/subsystems/session.md | head -1
  5:...A `Session` is an **append-only log** of typed `SessionEvent`s...

  # gopi 侧对照引证
  $ cd /home/user/work/GoPi && sed -n '1168p;1072p;5527p' internal/agent/loop.go
  var claudeToolDefs = []deepseek.ClaudeToolDef{
  var promptSections = []PromptSection{
  var parallelSafeTools = map[string]bool{
  $ sed -n '5864p;5982p' internal/tools/tasks.go
  func taskParentGateDenied(parentID string, parentStatus map[string]string) (bool, string) {
  func taskRowClaimable(rowTags, payload, parentID string, parentStatus map[string]string, skipSet map[string]bool) bool {
  $ sed -n '5818p;5830p' internal/agent/loop.go
  func (a *Agent) executeToolDefault(tc deepseek.ToolCall) string {
  switch tc.Name {

  # Census（§1.3，Rule 38：生成器 + 测量 commit）
  $ git ls-files | wc -l                                    # dsh @141eb6fef834 → 7807
  $ git ls-files '*.ts' | wc -l ; git ls-files '*.ts' | xargs cat | wc -l
  2438
  532353
  $ find . -path ./node_modules -prune -o -type f -name '*.ts' -print | wc -l   # → 3299
  $ find . -path ./node_modules -prune -o -type f -name '*.ts' -print | grep '/lib/' | wc -l   # → 861
  $ find node_modules -type f -name '*.ts' | wc -l          # → 14762
  $ ls -d packages/*/*/ | wc -l ; git ls-files 'packages/*/*/package.json' | wc -l
  226
  226
  $ git ls-files 'vendor/*/package.json' | wc -l ; git ls-files 'vendor/**' | wc -l
  9
  74
  $ grep -c '^### ' docs/tool-catalog.md ; grep -oE '^### `[a-z_]+`' docs/tool-catalog.md | sort -u | wc -l
  63
  58
  $ grep -rn 'systemPrompt.section({' packages/ --include='*.ts' | grep -v '/lib/' | grep -v '/tests\?/' | wc -l
  28
  $ cd /home/user/work/GoPi && find internal cmd -name '*.go' -not -name '*_test.go' | wc -l   # @203e8a40 → 562
  $ awk '/var claudeToolDefs = \[\]deepseek.ClaudeToolDef\{/,/^\}/' internal/agent/loop.go | grep -c 'Name:'   # → 17
  ```

- **Result**: `devdoc/competitor-dsh-analysis.md` 覆盖 6 个维度；§8 给出 gopi 的 6 个输面（D1–D6）；
  §1.2 给出两目录关系结论（同一 commit `141eb6fef834`，67 条差异全部是 dsh-gopi 侧的构建产物）；
  **89 处**（首轮）`path:line` 引证经修好的脚本验证存在且行号在界内（定稿为 85 处唯一引证，`refs_bad=0`），其中 21 处行号已按内容级复核修正；
  `gopi doccheck` exit 0。收尾 `git status --porcelain` 为空（Rule 30）。

- **Commit**: 承载本报告字节的 commit 链（分支 `task/01a0eeb167b5733e84b9e7710eed2a00`）：
  `361a5d91`（首次落地）→ `74a173b6` → `3694289d`（差异数 66→67）→ `7d1b0c89`（首轮内容级复核）
  → `3b8604e3`（Commit 行改 hash 链）→ 本轮（checker 缺陷修正 + 21 处行号 + 3 处证伪数字 + 本段重写）。
  **最终字节的权威取法**（避免本行随每次修正过期）：
  `git log -1 --format=%H -- devdoc/competitor-dsh-analysis.md`。
  首次落地的自检：`git show 361a5d91 --stat` → 恰好本报告 + `devdoc/DOC-INDEX.tsv` 一行。

- **Files**: 进入本 run 写树的仅 2 个文件 ——
  `devdoc/competitor-dsh-analysis.md`（新增）、`devdoc/DOC-INDEX.tsv`（+1 行，Rule 36 写钩子登记）。
  中间产物全部落在 session 目录
  `~/.gopi/sessions/01a0eeb1-67b5-733e-84b9-e7710eed2a00/`：
  `check_refs.py`（v2 定稿）、`refs_content2.py` / `refs-content2.txt`（89 处内容级复核）、`audit2.py`。

- **Evidence (self-filed)**（只列本 run 亲自观察到的：
  `check_refs.py` v1 唯一命中 62 处（含 `.go` 者 0）→ v2 唯一 85 处（含 `.go` 28）、`refs_bad=0`；
  `diff -rq | wc -l` = 67 且反方向 = 0；`git ls-files 'vendor/*/package.json' | wc -l` = 9；
  `ls -d packages/client/ui-*/ | wc -l` = 34；`git status --porcelain` 打印为空。）
