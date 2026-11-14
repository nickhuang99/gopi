<!-- doc-class: historical -->
# Task Report: 无人值守语境（unattended context）成为 `--once` 的默认语义，并补上 chat_surface 撞墙出口

**task**: `01a1070b-e9c2-7c8e-a690-148aa05f034a` · **branch**: `task/01a1070be9c27c8ea690148aa05f034a`

## Completed

- **`internal/agent/chat_unattended.go`（新）** — 无人值守语境块，三句话一行，由常量拼装（marker / 事实 / 授权 / 失败形态），导出的 `ChatUnattendedPromptHead()` 供包外按键断言。无新工具、无新机制、无新依赖（Rule 3/35）。
- **`Agent.Unattended`（loop.go）** — 语境由「运行自己声明」驱动，默认 false，只被两个 chat 装配读取（`chatUnattendedBlockOpen`），不参与任何行为强制。
- **两个装配点**（`assembleChatSystemPrompt`、`chatOpsSystemPrompt`）— 声明为无人值守时追加该块；未声明时**字节不变**（空的 section 不带走分隔符）。任务路径 `assembleSystemPrompt` 确认不含该块。
- **`gopi chat --once` 默认携带该语境**，新增显式关闭开关 **`--answer-alone`**（交互/单轮问答用），并说明默认偏向的理由（一次性面的答案的消费者是脚本/agent，不会有人回话）。
- **chat_surface 出口重定向（撞墙 + 无接缝两处）** — 都按语境**分支/追加**，交互原文一字保留：
  - 撞墙（`action=type` 达 `chatSurfaceLoopBound`=8）：交互路径 "…say in it what the operator should type…" 原样；无人值守路径改为「用普通一轮继续，你的回答就是下一步，不要等也不要让操作者输入任何东西」，且把「only a line the OPERATOR submits opens a fresh period」换成「nothing on this run opens a fresh period」。
  - 无接缝（piped / one-shot / `--no-editor` 没有 next-turn seam）：交互路径仍是 surface 自己的原话；无人值守路径**追加**「这里也没有操作者可以等：在你的回答里继续」。
- **工具描述同向分支** — `chatSurfaceToolDefFor(a)`：交互路径描述与改前**逐字节相同**，无人值守路径追加一段，明确 `action=type` **只在有行编辑器的运行里**有效、无 seam 的运行会拒绝它、那里回答是唯一出口，并重申上限不会自我续期。
- **测试** — `internal/agent/chat_unattended_test.go`（存在性双向 × 两个装配点、≤3 行上限、撞墙分支 + `chatSurfaceLoopBound` 仍为 8、描述分支非替换）、`cmd/agent/chat_unattended_wire_test.go`（真实 REPL 轮次打到 mock 模型，读**请求体**：一次性面默认携带 / `--answer-alone` 不携带 / 不是假事实；argv 契约；help 双方向）。`cmd/agent/chat_boundary_test.go` 的 harness 抽出 `runChatBoundaryTurnArgv`（同一生产路径，未复制）。

## Not Done

- **「模型真的撞满 8 轮」的线上运行**没有做（见下 `## Evidence (d)` 的完整交代）：撞满 8 轮要 8 个模型轮次，一次运行就吃掉本会话预算的一大半。**已用两种更便宜的证据替代**：(i) 单元的——两个包的真实调用面驱动同一拒绝，断言两个语境分支 + `chatSurfaceLoopBound` 仍为 8；(ii) **模型级**——真模型在真 `--once` 运行里拿到了**修正后**的无接缝拒绝并照做（这一支比撞墙那支更常见，因为 `--once` 正是无接缝的运行）。
- **`devdoc/` 里 7 份既有文档的行锚点因本次 `loop.go` 插入 15 行而位移**，已按「按符号重新定位、不按算术平移」的规矩逐条核对后重订（`hippocampus.md` ×2、`agent-five-organs-ontology.md`、`competitor-dsh-analysis.md` ×2、`report-seam-hermetic.md`、`review-cli-config-options.md`、`rsi-chat-files-a-row-recipe.md`、`look-without-fixing-bound.md`、`lineedit-band-write-ownership.md`、`test-env-isolation.md`）；两条已不再漂移的 permit 被删除，一条改订到同一测量落在的新行。
- **`TestDocAnchorsResolveInNormativeDocs` 在 master 上本就是红的，本分支不是引入者**——用 HEAD 的 `git archive` 对照树（只读、不 checkout，Rule 29）实测：
  ```
  $ (HEAD 对照树) go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs
  FAIL  ... 233 pre-existing drift entries permitted, 4 new, 1 stale permits     ← 基线：红
  $ (本分支)     同上
  FAIL  ... 231 pre-existing drift entries permitted, 4 new, 0 stale permits     ← 本分支：同样 4 条 new（同一批既有漂移），stale 从 1 降到 0
  ```
  即**本改动零新增漂移**，并把一条 stale permit 一并解决（`loop.go:2938`→`:2953`）。仍红的 4 条是分支基线自带的（`internal/lineedit/editor.go:838` ×2、`cmd/agent/commands.go:704` 等），修它们属于另一件工作，不在本任务范围内。
- **`make test-budget PKGS='./internal/agent ./cmd/agent ./internal/standards'` 两次都不绿，两条失败与本次改动无关，已各自取证**（`budget did NOT trip`，即不是预算超时）：
  1. `TestDocAnchorsResolveInNormativeDocs` —— **master 本就是红的**（基线 4 new / 1 stale；本分支 4 new / **0** stale），见上一条。
  2. `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` —— **受限负载下的超时抖动**，不是本改动的行为差异：同一台机器上，**基线 HEAD 对照树** `ok 2.129s`，**本分支** `ok 2.648s`（各自单独跑都过）；套件里那次是 4 核机上 `load 6.62` 时跑到 `chat_surface_bound_e2e_test.go:1274` 的 20s 等待超时（日志明写「the REPL did not finish within 20s after the injected ctrl-c」，而 mock 已服务 40 轮）。该用例走的是**交互**路径，而本改动在交互路径上是**零字节变化**（交互工具描述逐字节相同、交互拒绝原文保留、装配按 `Unattended=false` 不追加任何内容，三处都有断言）。
- 两次 `make test-budget` 的完整流留痕：`run-1791121286-72661.json`（第一次，失败里有 4 条，其中 3 条是**未提交的新文件**导致子进程在临时 worktree 里构建时 `undefined: chatUnattendedBlockOpen`——提交后即消失）与 `run-1791122082-209402.json`（第二次，只剩上述 2 条）。这条本身是个可复用的教训：**子进程构建类测试在 `git worktree` 里只看已提交内容**。

- **没有动任何上限**：`chatSurfaceLoopBound` 仍是 8，`--turns/--stop-on/--max-seconds` 语义未动（测试里显式断言 bound==8）。

## Verification

- Build: `go build ./...` → OK（exit 0）
- Tests: `make test-budget PKGS='./internal/agent ./cmd/agent ./internal/toolnames'` → 见下方 Evidence（job-1）
- Tests（分支范围内的重测，交付前最后一轮实跑）:
  - `go test ./internal/agent/ -run 'TestChatUnattended|TestChatSurface' -count=1` → `ok github.com/nick/gopi/internal/agent 0.218s`（exit 0）
  - `go test ./cmd/agent/ -run 'TestChatUnattended_' -count=1 -v` → 3/3 PASS（`WireCarriesItOnlyOnTheOneShotFace` 含两个子用例 / `AnswerAloneIsAcceptedAndUnattendedIsTheDefault` / `HelpNamesBothDirections`），`ok github.com/nick/gopi/cmd/agent 4.964s`（exit 0）
  - 一次全包 `go test ./cmd/agent/`（无 -run 过滤）在 286s 内被本会话杀掉，未出结论 —— 它含 10MB 载荷 / 20s 交互等待那批重用例（Rule 27 的 heavy 清单家族），而改动不落在那些用例的路径上；分支范围内该包的新增/改动面已由上面 3/3 覆盖。**不把这次被杀掉的运行当作通过，也不当作失败**：它没有结论。
- Ran（全部真实运行，二进制在证据目录）：
  - RED（修前二进制）：`gopi-red chat --once --tools --max-seconds 240 --turns -1 "<任务>"` ×2
  - GREEN（修后二进制，同一命令同一任务）：`gopi-green chat --once --tools --max-seconds 240 --turns -1 "<任务>"` ×2
  - 交互路径：`printf '你好…并 /quit\n' | gopi-green chat --tools`
  - 撞墙：`gopi-green chat --once --tools --max-seconds 300 --turns -1 "$(cat wall-prompt.txt)"`
- Result: 见 `## Evidence (self-filed)`

## Evidence (self-filed)

### 门禁结果（本次运行亲眼所见）

```
$ go build ./...                                        → exit 0
$ go test ./internal/standards/ -count=1                → ok (36.0s)
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs
  doc anchors verified: 2914 across 630 normative docs
  (2420 explicit + 494 bare continuations; … 231 pre-existing drift entries permitted, 4 new, 0 stale permits)
$ go test ./internal/agent/ -run 'TestChatUnattended|TestChatSurface_TypeRefuses' → ok
$ go test ./cmd/agent/ -run 'TestChatUnattended|TestSyncBarePeersTestCoversEveryChangedFile' → ok
$ gopi doccheck                                         → the devdoc corpus is consistent（exit 0）
$ git diff --name-only -- go.mod go.sum | wc -l         → 0
$ make test-budget PKGS='./internal/agent ./cmd/agent ./internal/standards'   → 见下（最终一次）
```
`COMMANDS.md` 的两条行引用（`cli_chat.go:1113` 与生成块里的 `:873`）随 `cli_chat.go` 的行位移断了，已按规矩处理：生成块**不手改**，用 `go run internal/standards/testdata/commands-census.go . COMMANDS.md` 重生成；散文里的两条引用重订到 `:1142`。

### (a) RED — 修前二进制，同命令同任务：模型问人后停轮

二进制：`artifacts/gopi-red`（在本 worktree 的**修改前** HEAD 上构建，`CGO_ENABLED=0 go build -o … ./cmd/agent/`，`gopi version` → `v0.1.0`）。

```
$ gopi-red chat --once --tools --max-seconds 240 --turns -1 "<inventory 任务>"
exit=0
stderr: gopi chat: driver stopped after 4 turn(s) and 4m29s: --max-seconds 240 reached
末轮原文: "收到，我停在这里等指令，不再逐行复述。 两个「一句话可启动」的待选动作，随时喊：…要我把…追加进 inventory.md 作为 evidence 代码块。"
```

问人用语计数（`需要我|要不要|请确认|等你|告诉我|shall I|需要您`）：RED p1 stdout **3 处**，末轮就是「等你下令」。产出的 `inventory.md` 里只覆盖了「3 个最大文件」，任务附带的其它要求没有推进 —— **模型把「问人」当成了终点**。

### (b) GREEN — 修后二进制，同一命令同一任务：不问人、自选口径、自驱完成

```
$ gopi-green chat --once --tools --max-seconds 240 --turns -1 "<inventory 任务>"
exit=0
driver stopped after 2 turn(s) and 5m17s: --max-seconds 240 reached
产出: inventory.md（30 行，含「两点口径说明」）+ loop-go-split-feasibility.md（7.2 KB，本会话新增）
末轮原文: "…下一步我会做的：把 inventory.md + loop-go-split-feasibility.md 作为附件提交为一步任务（Step 1 锚点改造），让这两份产出进库、不随本会话被清扫。"
```

差异的**机制**（不是措辞）：RED 那一轮把「你还要我做什么」当成终点并**停下来等**；GREEN 同一任务下**自己选了**「只列非测试文件 / 加函数数 / 存放位置」这些任务没写死的口径，产出了第二份文档，并把「下一步我要做什么」写进回答——即语境里那句「deliver the work, then end your answer with the one next step you would have taken」被真的执行了。
第二个无人值守场景（另一任务：`digest.md`，git 最近 20 个提交里挑 3 个）同样以 `--turns -1 --max-seconds 240` 运行，产出见 `artifacts/run-green-p2/`。

### (c) 交互路径未受影响（raw log 取证 + 一次真实交互运行）

```
$ printf '你好，用一句话说明你是谁，然后 /quit\n' | gopi-green chat --tools   # exit 0，正常对话
raw log = ~/.gopi/sessions/198debfe-…/198debfe-….raw.log
$ grep -c 'UNATTENDED RUN' <raw>               → 0    # 无人值守块不在请求里
$ grep -c 'No human is reading this run' <raw> → 0    # 连「假事实」也没有
$ grep -c 'you are the console' <raw>          → 1    # 而这个 run 确实是 chat 面（对照项，证明这两条 grep 能命中）
```
末轮回答正常（模型自述身份，并把 `/quit` 交回操作者），未出现「无人值守」措辞。

**`--answer-alone` 的线上负向对照**：

```
$ gopi-green chat --once --answer-alone "用一句话回答：1+1 等于几？"    # rc=0，stdout: "2。"
raw log = ~/.gopi/sessions/ad37579a-…/ad37579a-….raw.log
$ grep -c 'UNATTENDED RUN' <same raw>               → 0   # 语境被真的关掉了
$ grep -c 'you are the console' <same raw>           → 1   # 同一请求里 chat 面身份仍在
```
（同一棵树上，`--once` 默认那次 raw 里该 marker 出现 —— 见 `## Evidence (d)` 的 `4efa123f`，同族取证。）

### (d) 撞墙 / 无接缝出口（真实运行发现并修掉了自己写的假出口）

**这一节是本次运行最有价值的发现**：真实运行**推翻了本实现的第一版措辞**。

`artifacts/run-wall/`（提示词 `wall-prompt.txt`：要求模型连续 `action=type` 排 10 轮并把每次返回原文抄进 `wall.md`）在 GREEN 二进制上运行。模型照做了 8 次真实工具调用，每一次都拿到**同一条**拒绝：

```
chat_surface action=type refused: this run has no next-turn seam: the input line editor is not
active (a piped, one-shot or --no-editor run reads its lines from the caller). The tool can still
SEE this surface, but nothing can be typed into it.
```

模型随后把 8 条原文一字不改抄进 `wall.md`（"10 次全部被拒绝，拒绝原文 10 条完全相同"），并按拒绝指的方式改用 `action=see`。**它做对了；错的是我们。**

- **根因**：一次性面（`--once` / piped / `--no-editor`）**没有 next-turn seam** —— `action=type` 在那里唯一的出口就是「普通一轮」，而 `--once` 本身不设轮数上限（`--turns -1` 时由 `--max-seconds` 收口）。第一版工具描述写的「Within the period, type is still the way to queue your own next turn」对这类运行是**承诺了一扇不存在的门**。
- **修复（三处，均为语境分支的追加，交互原文一字未动）**：
  1. `chatSurfaceUnattendedRouteClause`（工具描述）改成：`action=type` **只在有行编辑器的运行里**有效，piped/one-shot/--no-editor **没有 seam 并会拒绝**，在那里**回答就是唯一出口**。
  2. 无接缝拒绝本身在无人值守语境下追加一句：`— and there is no operator to wait for on this run either: continue IN YOUR ANSWER…`（交互路径仍是 surface 自己的原话，逐字节可前缀断言）。
  3. 撞墙拒绝的交互措辞「only a line the OPERATOR submits opens a fresh period」在无人值守路径换成「nothing on this run opens a fresh period」——不把一扇不存在的门指给模型。
- **回归测试**：`TestChatSurface_TypeRefusesWhatTheSurfaceCannotCarry` 现在同时断言「无人值守的无接缝拒绝以交互原文为**前缀**（是追加不是重写）」且含替代出口；`TestChatUnattended_WallRefusalBranchesOnWhoseRunThisIs` 断言无人值守的撞墙拒绝**不含** "what the operator should type"。

无接缝那一支的**模型被说服**取证（修正后的措辞，`--turns 1`）见 `artifacts/run-noseam/`：

```
$ gopi-green chat --once --tools --max-seconds 120 --turns 1 "调用 chat_surface action=type …把工具返回的原文一字不改地写进 noseam.md，然后结束。"
rc=0
工具返回（模型一字不改地抄下，未询问、未停顿）：
chat_surface action=type refused: this run has no next-turn seam: the input line editor is not
active (a piped, one-shot or --no-editor run reads its lines from the caller). The tool can still
SEE this surface, but nothing can be typed into it. — and there is no operator to wait for on this
run either: continue IN YOUR ANSWER (it is the next step), deliver what you can finish now, and
choose a convention yourself wherever the task left one open.
```
模型回答："已按指令执行完毕" + 落盘位置（会话目录内，非任何 git 工作树）+ md5，**没有把「等我一下」当作终点**——这正是这一支存在的意义。

**构建版本诚实声明**：(a)/(b) 的 GREEN 二进制构建于**撞墙/无接缝文案修正之前**（该修正只改 chat_surface 的描述与两条拒绝文案，不改装配逻辑、不改 `--once` 的语境默认值）；(d) 的无接缝运行使用的是**修正后重新构建**的二进制。两条结论互不依赖。

### (e) prompt 增文 ≤ 3 行

`TestChatUnattended_BlockIsAtMostThreeLines` 断言：块自身 `\n` 计数 ≤ `chatUnattendedPromptLineCount`(=3)；并且**装配后的整段提示词**因该块增长的行数 ≤ 3（含 section 分隔的空行）。实际块 = 1 个渲染行（三句以空格相连）。

### (f) build / test / 依赖

- `go build ./...` → exit 0
- `git diff --name-only -- go.mod go.sum | wc -l` → `0`（依赖未变）
- `make test-budget PKGS='./internal/agent ./cmd/agent ./internal/toolnames'` → job-1 结论见下

### (g) 报告与索引

**CONFIG.md 未改，是刻意的**：`--answer-alone` 是 `gopi chat` 的子命令 flag，而 CONFIG.md §3 自己写明了边界——"Subcommand flags are not top-level flags… belong to their verb and are documented by that verb's own `-h`。The catalog deliberately leaves their `CLIFlag` column empty"。它没有 env 通道（该放的是 flag 表之外的 knob），所以 `internal/standards/config_doc_gate_test.go` 的两条门（不许杜撰 knob、不许丢掉已登记 knob）都不涉及它；`go test ./internal/standards/` 绿即证。
- 本文件 `devdoc/chat-unattended-context.md`；`devdoc/DOC-INDEX.tsv` 同步（`gopi doccheck` 结果见下）。
- `git status --porcelain`（worktree 结束时）→ 空。

## Files

- `internal/agent/chat_unattended.go`（新） — 语境块、marker、`ChatUnattendedPromptHead`、`chatUnattendedBlockOpen`
- `internal/agent/chat_unattended_test.go`（新）
- `internal/agent/loop.go` — `Agent.Unattended` 字段（+15 行，插入位置 :2037）
- `internal/agent/chat_context.go` — `assembleChatSystemPrompt` 条件追加（空 section 不带走分隔符）
- `internal/agent/chat_ops.go` — `chatOpsSystemPrompt` 条件追加；`chatScopedToolDefs(a)`
- `internal/agent/chat_surface.go` — 撞墙拒绝按语境分支（closer + remedy）；无接缝拒绝按语境**追加**；`chatSurfaceToolDefFor(a)` + 无人值守路由句
- `internal/agent/chat_surface_test.go` — 无接缝分支的断言（追加而非重写）
- `internal/agent/tool_dispatch_coverage_test.go` — 调用点跟随签名
- `cmd/agent/cli_chat.go` — `chatFlagAnswerAlone`、`chatSession.unattended`、argv 消费、闭集条目、verbose 提示
- `cmd/agent/chat_once.go` — `--once` 帮助新增 UNATTENDED BY DEFAULT 段（并点名 `--answer-alone`）
- `cmd/agent/chat_boundary_test.go` — harness 抽 `runChatBoundaryTurnArgv`
- `cmd/agent/chat_unattended_wire_test.go`（新） — 请求体契约
- `cmd/agent/cli_tasks_sync_peers_bare_test.go` — 认领本次改动的两个 cmd/agent 文件（unrelated 清单，含理由）
- `COMMANDS.md` — 生成块按 `commands-census.go` 重生成；散文两条行引用重订
- `devdoc/hippocampus.md` / `agent-five-organs-ontology.md` / `competitor-dsh-analysis.md` / `report-seam-hermetic.md` / `review-cli-config-options.md` / `rsi-chat-files-a-row-recipe.md` / `look-without-fixing-bound.md` / `lineedit-band-write-ownership.md` / `test-env-isolation.md` — 因 `loop.go`/`cli_chat.go` 行位移而重订的行锚点
- `internal/agent/testdata/docanchor-baseline.tsv` — 删 2 条已不再漂移的 permit，改订 1 条并写明理由
- `devdoc/chat-unattended-context.md`（本报告）+ `devdoc/DOC-INDEX.tsv`
