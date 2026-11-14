<!-- doc-class: historical -->
# gopi 自身能力清单 — C 切片（会话可观测性 / 模型与协议接入 / 多模态与人机接入面）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

测量时刻：`v0.4.0`，`go1.24.0`，`linux/amd64`，ground 基线 `d0feafcc`，
分支 `task/01a1aa6d17a97b5485248159134b130d`。

原始证据目录（不随仓库走，Rule 30）：
`/home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/help/`
（114 份帮助转储 + 真实调用记录 + 产出的 PNG/GIF 样本）。

## 0. 取证方法，以及一条必须先说清的陷阱

本文每一格都是**实测**：要么是一条逐字帮助行（`<file>:<line>` 指向转储文件的行），
要么是一次真实调用及其原始输出，要么是 `file:line` 源码锚点。**推断与实测分开写**，
没有观察过的格不填。

**陷阱：`-h` 不能证明一个动词存在。** 对未知词执行 `-h`，gopi 打印**顶层帮助并 exit 0**：

```
$ gopi mcp -h > /tmp/h1 2>&1; echo exit=$?; md5sum < /tmp/h1
exit=0
85807fccdd745f917d2759dd66a2828b
$ gopi login -h   # 同一个 md5
$ gopi zzznosuchverb -h   # 同一个 md5
```

所以 `gopi X -h` 输出「GoPi — a tool-using AI coding agent」时，唯一被证明的事情是
**X 没有自己的帮助页**。判据换成分三种，且三种都必须用：

| 判据 | 命令 | 已知动词 | 未知动词 | 退役动词 |
|---|---|---|---|---|
| 有无自己的帮助页 | `gopi X -h`，与顶层帮助比 md5 | 输出不同 | 输出**等于**顶层帮助，exit 0 | exit 1，指名退役 |
| 裸调是否被识别 | `gopi X` | 干活 | `gopi: "replay" is not a gopi command` exit 1 | `` gopi: `stats` is RETIRED — use `gopi session stats` `` exit 1 |
| 源码有无分派 | `grep -rnw 'CliVerbTokens' --include=*.go cmd/ internal/` | 有 | 无 | 只剩退役提示表 |

**假词实验（比 `mcp` 更硬的对照）。** 先造两个绝不可能是命令的词，再拿它们和一个
真命令一起量。四行的 md5 **完全相同**，等于顶层帮助：

```console
$ B=./bin/gopi
$ $B daemon      -h >x_daemon.txt  2>&1; echo exit=$?; wc -l <x_daemon.txt; md5sum x_daemon.txt
exit=0
170
85807fccdd745f917d2759dd66a2828b  x_daemon.txt
$ $B serve       -h  →  exit=0  170 行  同 md5
$ $B term        -h  →  exit=0  170 行  同 md5
$ $B frobnicatezzz -h → exit=0  170 行  同 md5
$ $B qqq         -h  →  exit=0  170 行  同 md5
```

（真命令的 `-h` 页不可能是 170 行：`gopi capture -h` 77 行、`gopi act -h` 77 行、
`gopi tokens -h` 32 行、`gopi proxy -h` 20 行、`gopi text2img-verify -h` 3 行。）

**裸调在这一层是可靠的，但判据不是退出码，而是 stderr 里那一行。** 从 task run 内部
裸调会被防呆守卫先拦住（exit 3），这个退出码是环境限制、不能当证据；然而守卫的信息里
**包含分派链自己的否定结论**，那一行才是判据：

```console
$ $B mcp  2>&1 | head -2
gopi: refusing to start: this process is inside an agent session (GOPI_AGENT_SESSION(inherited), GOPI_EXECUTOR_SESSION_ID),
  and "mcp" is not a gopi verb — it would fall through to the bare-prompt fallback,
$ $B daemon  2>&1 | sed -n 2p
  and "daemon" is not a gopi verb — it would fall through to the bare-prompt fallback,
$ $B serve   2>&1 | sed -n 2p
  and "serve" is not a gopi verb — it would fall through to the bare-prompt fallback,
$ $B term    2>&1 | sed -n 2p
  and "term" is not a gopi verb — it would fall through to the bare-prompt fallback,
```

一行 `and "<word>" is not a gopi verb` 是**分派链走完之后的否定结论**（`cmd/agent/main.go:704`
那一行 19 个 `is*Subcommand` predicate 全为假才落到这里，生成它的是
`internal/config/agentguard.go:504`），所以它能判「无此动词」，且不受 `-h`
短路影响。本文据此采信「自己的帮助页（与顶层页比 md5）」+「这一行」+「源码锚点」三路；
`-h` 的退出码**不作为任何证据**。

**一个源码侧的旁证，同时是一个诚实警告。** `gopi term` 不是动词，因为那条链问的是
`CliVerbCLI = "cli"`（`cmd/agent/cli_verbs.go:62`），而 `isTermSubcommand` 是判
`"cli"` 的函数（`cmd/agent/cli_term.go:107`：`if len(args) == 0 || args[0] != CliVerbCLI`
→ `return false`）。**函数名（`term`）与它判定的词（`cli`）不一致** —— 这正是
Rule 43 的同音陷阱的另一种形态：**读函数名会得到错误的动词表**。所以第 4 节的表
一律以「实跑 + `CliVerb*` 常量的值」为准，不以函数名或文件名推断。

---

## 1. 会话与可观测性

gopi 的可观测性不是一组「日志命令」，而是**四层同时可读**：会话记录、原始 HTTP 报文、
渲染后的屏幕细胞网格、以及三者之间的对质器。

### 1.1 会话族

```
$ gopi -h  (00-top.txt:107-109)
  gopi session <subcommand>         The session family — list/show plus the log reader
                                    (`gopi session -h`).
  gopi --session <id> ["<prompt>"]  Continue an interrupted session from its checkpoint
```

会话族有**八个子动词，且只有部分共用一个帮助页**（实测 `gopi session <sub> -h` 与
`gopi session -h` 比 md5）：

| 子动词 | 自己的页？ | 一行能力（逐字取自该页） |
|---|---|---|
| `session list` | 与父页相同 | 列存档会话 |
| `session show` | 与父页相同 | 逐字段状态（见 1.2 实测） |
| `session stats` | **有** | 用量/成本/字节/tool 调用统计 |
| `session raw` | **有** | `GoPi session raw — GoPi raw HTTP log, byte for byte (verbatim)`（`83-session-raw-h.txt:1`） |
| `session resume` | **有** | 恢复被打断的会话（`--task <id>` 走任务行那一面） |
| `session replay` | **有** | `play a recorded session back as a PICTURE.`（`82-session-replay-h.txt:1`） |
| `session notes` | **有** | `search the round-level [IMPORTANT] note index`（`81-session-notes-h.txt:1`） |
| `session gc` | **有** | `report which session directories are collectable`（`80-session-gc-h.txt:1`） |

`session list` / `session show` 与父页共用一份帮助，这是**实测出来的**（两者 md5 都是
`26b155d9`，与 `10-session-h.txt` 一致），不是推断。

### 1.2 `session show` / `stats` / `raw` — 三档粒度

```console
$ gopi session show 01a1aa6d-17a9-7b54-8524-8159134b130d
session 01a1aa6d-17a9-7b54-8524-8159134b130d
  mode        run
  state       running
  model       deepseek-flash
  started     2026-11-06 00:24:10
  last write  2026-11-06 00:28:43
  workdir     /home/user/workspace/01a1aa6d-17a9-7b54-8524-8159134b130d
  git branch  task/01a1aa6d17a97b5485248159134b130d
```

```console
$ gopi session stats 01a1aa6d-17a9-7b54-8524-8159134b130d --tools
│ model          deepseek-flash
│ turns          1
│ requests       26
│ prompt tokens  2,090,420   (avg 80,400/request)
│   per-request  min 61,501 │ max 97,590
│ cache          96.84% hit (2.02M) / 66K miss
│ completion     16,878
```

`stats` 有一句**如实承认不可测**的输出，值得单独记下来：

```
│ completed      not measured — the chat index holds no row for this session, so no turn count was recorded
```

```console
$ gopi session raw 01a1aa6d-17a9-7b54-8524-8159134b130d
# gopi session raw HTTP log — created 2026-11-05T16:24:10Z pid=2192635 session=01a1aa6d-17a9-7b54-8524-8159134b130d log=01a1aa6d-17a9-7b54-8524-8159134b130d.raw.log
R 240606
{"model":"deepseek-flash","max_tokens":16384,"system":[{"type":"text","text":"[OUTPUT LANGUAGE — set by the operator, overrides everything below] Write every reply you produce to the user in zh-CN. [以下为这条 JSON 行的其余内容；该行共 240606 字节，本文只引到这里，完整原文取回命令：`sed -n '3p' /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/help/106-session-raw.txt`]
```
`session raw` 输出 67,141 行，是**逐字节的线上请求体**，不是摘要。

`stats` 的 `--cost` / `--bill` 是真实货币口径，且明确拒绝聚合：

```
$ sed -n '87,94p' 14-session-stats-h.txt
--cost / --bill (real money, 元):
  --bill need one session (not --all — an aggregate mixes models and dates).
```

### 1.3 `session resume` 的两张脸

`session resume` 是**唯一一个在帮助里主动划清边界的动词**：

```
A TASK ROW is resumed with `gopi worker --resume <task-id>`, not with this verb:
that face takes over the row's existing ground and runs the close-out chain that
RELEASES its worktree; a resume here leaves it registered for good.
Use this one for what has no row (`gopi chat`, a bare `gopi "<prompt>"`), or for a
row's raw log when no ground is involved.
```

能力含义：**同一个词在两套生命周期里指两件事**，而 gopi 选择在帮助页里点名这件事，
而不是让一个 `resume` 兼做两件事。

### 1.4 `ui_state` — 屏幕作为证据通道（不是像素，是细胞）

```
$ gopi -h  (00-top.txt:117)
  gopi ui_state [--pid N] [--json]  Read a live chat screen as a cell grid.
```

```console
$ gopi ui_state --target chat
#gopi-ui-state surface=chat pid=962224 target=self:chat-tools started=2026-11-05T05:01:11Z state=live dump=/home/user/.gopi/sessions/1918704d-3823-40d4-b1bb-e928ac791f73/chat-screen.dump
an interpreted cell grid (not bytes, not pixels): the escape sequences were already interpreted by the terminal emulator, so this describes the CELLS of the screen
#gopi-screen-dump v=1 cols=117 rows=24
cursor row=22 col=5
```

`ui_state --list` 是一张普查：

```console
$ gopi ui_state --list
#gopi-ui-state-list scanned=256 roots=2
```

**实测到一个必须如实写下的行为**：在 task run 内部 `--target chat` 解析到的是
**当时唯一 live 的 chat surface**（`pid=962224`、会话 `1918704d-3823-40d4-b1bb-e928ac791f73`），
**不是本 run 自己**。原因是本 run 是 `mode run`（`session show` 第一行），没有 chat 屏。
这不是缺陷，但它说明「从 task run 看 chat」看到的是别人的屏幕 —— 想读自己的那一屏，
必须靠 `GOPI_CHAT_SURFACE_ID` 这条已经发布出去的 claim（`PROJECT.md` Rule 3 末段）。

### 1.5 `--compare` — 两个视图互相作证

这是 gopi 独有的一层：**把屏幕网格与转录文本对质，并指出哪一侧输掉**。

```console
$ gopi ui_state --target chat --compare "gopi self-inventory"
exit=3
#gopi-ui-state-compare surface=chat state=live dump=/home/user/.gopi/sessions/1918704d-3823-40d4-b1bb-e928ac791f73/chat-screen.dump session=/home/user/.gopi/sessions/1918704d-3823-40d4-b1bb-e928ac791f73/session.jsonl session_file=/home/user/.gopi/sessions/1918704d-3823-40d4-b1bb-e928ac791f73/session.jsonl transcript_lines=6788 outcome=disagreed match=false
  mismatch grid_row=-1 in_grid=false in_transcript=false damage= display_hits=0 geometry=layout cols=117 rows=24 frames=326706 prefix=0 reason="neither the transcript nor the grid has this line verbatim: it is not text this session produced, so no layer can be blamed (check the session, and that the line is exact)"
```

三个输出字段就是这套机制的价值所在：`outcome`（agreed/disagreed）、
`reason`（**具体哪一侧丢的，或者根本没有这一行**）、`geometry`（cols/rows/frames/prefix）。
它拒绝在没有证据时归罪于任何一层 —— 这句话本身是设计声明，不只是报错文案。

### 1.6 `context_trim` 的可观测面

`context_trim` 是工具（在内核 wire 上），但它的**活动有独立的读法**：

```
$ sed -n '44,48p' 13-session-log-h.txt
  gopi session log <session-id> --context-trim   context_trim activity table
  gopi session log <session-id> --context        Only context_trim calls
```

```console
$ gopi session log 01a1aa6d-17a9-7b54-8524-8159134b130d --context-trim
═════════════════════════════════════
Session: 01a1aa6d-17a9-7b54-8524-8159134b130d
Model:   deepseek-flash
Started: 2026-11-06 00:24:10
```

`--todo` 是同一个 `session log` 的另一条腿（任务行进度的读法，同 104 号转储）。

### 1.7 token 计量：自己的计数与服务器的计数并排

这才是「可观测性」在 gopi 里的完整意思 —— 它不只统计，它**对账**：

```console
$ gopi tool tokens --file go.mod
go.mod: 715 tokens (1865 bytes, 46 lines, deepseek-v4.1-bpe)

$ gopi tool tokens --session 01a1aa6d-17a9-7b54-8524-8159134b130d
session 01a1aa6d-17a9-7b54-8524-8159134b130d
raw log /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/01a1aa6d-17a9-7b54-8524-8159134b130d.raw.log

line     msgs   tools   ours     server    diff
2        1      17      61501    61501     +0
66       3      17      68206    68206     +0
1539     3      17      62389    62389     +0
1881     5      17      72475    72475     +0
2238     7      17      65115    65115     +0
```

`ours` 列来自 `internal/tokenizer`（自研 BPE，`deepseek-v4.1-bpe`），
`server` 列来自线上返回的 usage，**`diff` 是二者之差**。差分能成立的前提是
两套计数都存在 —— 这是零依赖实现换来的可对账性。

### 1.8 `session replay` — 会话回放成「画面」

```console
$ gopi session replay 1514490a-339c-4484-be0d-496bf000540d --turn 1
turn 1/1  (3 request(s), 2 tool call(s))  calls=2
PROMPT: run this exact shell command: echo POLICY-PROBE-A
HARNESS:  [system] [t+0s] — first request of this session.
THINKING:
  The user asks: run this exact shell command: echo POLICY-PROBE-A
```

`replay` 与 `chat --replay/--film` 是同一能力的两个入口：

```
$ sed -n '389,394p' 40-chat-h.txt
  --replay SESSION      re-run a RECORDED session on this surface
  --film DIR            film that re-run in a private display and terminal
  --film-terminal NAME  the terminal the film runs inside
```

区别在**产物**：`session replay` 输出文本时间线；`chat --film` 把它演在一个私有 X 显示
里并录成影像。前者给人读，后者能当证据（像素级）。

### 1.9 `session notes` — 跨会话的笔记索引

```console
$ gopi session notes "IMPORTANT" --limit 2
2 note(s) matching "IMPORTANT" via fts (capped at 2):
  hosta  round 13  2026-10-10 13:09  23f88060-704e-442e-b32e-343c94f158ba
      /home/user/.gopi/sessions/23f88060-704e-442e-b32e-343c94f158ba/self-record.md
```

这是**全文检索**（`via fts`），索引对象是每轮的 `[IMPORTANT]` 块，不是会话正文。

---

## 2. 模型与协议接入

### 2.1 一条 wire，一个可换的基址

```go
// internal/agent/loop.go:1217-1218
// (input_schema instead of parameters). The Claude interface is the only
// API path.
```

**gopi 对模型说话只说一种协议：Anthropic Messages。** 换厂商不是换协议，是换
`base_url`：

```
$ gopi --help-env  (02-help-env.txt:16-17)
  ANTHROPIC_BASE_URL               base_url                    --base-url
    API base URL (default https://api.deepseek.com)
  DEEPSEEK_BASE_URL                base_url                    --base-url
    Fallback base URL when ANTHROPIC_BASE_URL unset
```

```console
$ gopi config show | grep -E 'base_url|model|reasoning|thinking|vision'
  base_url                              "https://api.deepseek.com/anthropic"
  model                                 "deepseek-flash"
  reasoning_effort                      "high"
  thinking_budget                       16384
  vision_base_url                       "https://api.deepseek.com/anthropic"
  vision_model                          "deepseek-flash"
```

能力边界要写清楚：**「OpenAI 兼容」在 gopi 里不是一条独立 wire**，而是一个
**协议翻译代理**（见 2.5）。源码里 `openai` 的 10 处命中全是注释与
OpenAI-wire 错误码形状的兼容说明（`261-absence-read.txt`），不是第二条客户端。

### 2.2 思考：三档预算 + 一个「只在开启时上线」的深度枚举

```
$ gopi --help-env  (02-help-env.txt:32-35, 40-41)
  GOPI_THINKING                    thinking                    --thinking
    Thinking mode: disabled (default) or enabled
  GOPI_REASONING_EFFORT            reasoning_effort            --reasoning-effort
  GOPI_THINKING_BUDGET             thinking_budget             --thinking-budget
```

```go
// internal/deepseek/claude_client.go:320-327
// ReasoningEffort is DeepSeek's reasoning-depth enum (off | minimal |
// low | medium | high | max), sent verbatim on the wire. The /anthropic
// endpoint ACCEPTS and HONORS it (verified live 2026-08-22: identical
// prompt produced 6x the thinking chars at high vs no effort, and all
// six values return HTTP 200). It is only sent while thinking is
// enabled; "off" is resolved client-side to thinking disabled and never
// reaches the wire (dsh resolveThinking semantics).
ReasoningEffort string `json:"reasoning_effort,omitempty"`
```

两层可观测的细节：**枚举值由 settings 里的 `effort_values` 表校验**（帮助行原文
"validated against the settings.json effort_values list"），而 `off` 在**客户端**
解析成「不发该字段」—— 所以它是「关掉」而不是「发一个 off」。

### 2.3 输出上限与上下文档位

```
$ gopi --help-env  (02-help-env.txt:46-47)
  GOPI_MAX_TOKENS                  max_tokens                  --max-tokens
    Per-response output cap (default 16384)
```

压缩（compaction）有自己的触发比与目标比，且**只从一个地方读环境变量**：

```go
// internal/agent/compact.go:404
cfg.TriggerRatio = envFloat("GOPI_COMPACT_TRIGGER", DefaultCompactTriggerRatio, cfg.TriggerRatio, func(f float64) bool { return f > 0 && f < 1 })
```

`--compact-trigger` / `--compact-target` 存在（`gopi --compact-trigger 0.5 -h` exit 0，
`161-proxy-compact-evidence.txt`），说明压缩不是自动黑箱，是可以摆参数的。

### 2.4 回复语言是一等旋钮

```
$ gopi --help-env  (02-help-env.txt:319-320)
  GOPI_OUTPUT_LANGUAGE             output_language             --output-language
    Language of gopi's REPLIES — a BCP-47-ish language tag (zh, zh-CN, en-GB; NOT a
```

**实测它拒绝语言名、只收标签**（`142-flag-validation.txt`，逐字）：

```console
$ gopi --output-language chinese -h
exit=2
gopi: refusing to run: --output-language "chinese" is not a language tag (want e.g. zh, zh-CN, en-GB)
  `--output-language` was given on THIS command line, so the value must be usable — a typo here is a mistake to fix, not a declaration to skip
  legal shape: a BCP-47-ish language tag — e.g. zh, zh-CN, en-GB (NOT a language name (`chinese`) or a locale (`zh_CN`))
  a per-invocation declaration is fatal; a settings.json `output_language` value is only reported and the run continues (a per-machine preference must not break every command run on that machine)

$ gopi --output-language zh-CN -h   →  exit=0
$ gopi --output-language zz -h      →  exit=0    (两个字母是合法标签形状)
```

同一条拒绝里同时给出**三个层次的判据**：值是否合法、为什么本次致命、以及为什么
settings 里的同类值不致命 —— 这是一条自带分级策略的错误，不是一句「参数错误」。

**反例，必须一起记**：`--reasoning-effort absurd -h` 与 `--thinking maybe -h` 都是
**exit=0**（`142-flag-validation.txt`）。也就是说 `-h` 会**短路**掉一部分参数的合法性
检查，所以「用 `-h` 探测参数校验」这条路对**一部分**旋钮无效，只有真的跑才验得到。
这是第 0 节那条陷阱的第二个形态：**帮助页的门不等于校验的门。**

这条能力的纵深在**本 run 自己的系统提示第一段**：操作员设的语言被渲染成
`[OUTPUT LANGUAGE — set by the operator, overrides everything below]` —— 也就是说
语言旋钮不是「建议模型用中文」，而是**注入到 prompt 顶部的强制段**。

### 2.5 协议代理：把一种 wire 翻译成另一种

```
$ gopi -h  (00-top.txt:139)
  gopi tool proxy                    Anthropic/OpenAI protocol proxy.
```

```go
// internal/config/help.go:756
{"gopi tool proxy", "Anthropic/OpenAI protocol proxy."},
```

```go
// internal/proxy/server.go:44
upstreamPath: base + "/chat/completions",
```

**真跑，两种结局都取到**：

```console
$ gopi tool proxy --upstream bogus
exit=1
gopi tool proxy: invalid --upstream "bogus" (expected openai or anthropic)

$ gopi tool proxy --upstream openai --openai-base-url https://example.invalid/v4 --openai-model x --openai-key k
exit=124        # timeout 杀掉了一个正在监听的进程
gopi tool proxy: listening on 127.0.0.1:8090 (upstream=openai base=https://example.invalid/v4 model=x)
```

能力定位：gopi 自己的 agent wire 固定 Anthropic；当上游只提供 OpenAI Chat Completions 时，
`gopi tool proxy` 在本机起一个转换层。**这是「接入第三方模型」的唯一官方路径**，
不是内置多 provider 表 —— `internal/config` 里没有任何 provider registry
（`\bprovider` 8 处命中，全在无关上下文，`260-absence-probes.txt`）。

### 2.6 视觉模型是**第二个端点槽**，不是第二条协议

```go
// internal/deepseek/claude_client.go:712-716
// ResolveVisionBaseURL returns the base URL for the vision endpoint.
// Resolution: GOPI_VISION_BASE_URL env > settings vision_base_url >
// DefaultVisionBaseURL (the DeepSeek Anthropic-compatible endpoint). The
// client appends "/v1/messages" to it exactly like claudeMessagesURL does, so
// the same with/without "/anthropic" suffix tolerance applies.
func ResolveVisionBaseURL(cfg *Config) string {
```

```go
// internal/deepseek/claude_client.go:745
func NewVisionClient() (*Client, error) {
```

也就是说 `--vision-model` / `vision_base_url` 允许**主模型与视觉模型分属不同端点**，
而两者仍是同一种协议。帮助行：

```
$ gopi --help-env  (02-help-env.txt:26-27)
  GOPI_VISION_MODEL                vision_model                --vision-model
    Vision model for image understanding (default deepseek-v4-flash-vision-exp)
```

### 2.7 没有的东西（本节口径）

- **MCP 不存在**。`grep -rniE '\bmcp\b' --include=*.go internal/ cmd/` = **0**（`160-mcp-evidence.txt`）。
  注意不能用 `gopi mcp -h` 证明（那条路只证明它没有帮助页，见第 0 节）。
- **OpenAI Chat Completions 不是 agent wire**，只在 `internal/proxy/server.go:44` 出现，即代理。
- **没有 provider 注册表**：换厂商 = 换 `base_url`，或走代理。

---

## 3. 多模态与人机接入面

### 3.1 屏幕捕获：自己实现的 X11 客户端

```console
$ gopi tool capture --screen
{"path":"/home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/93-capture-real.png","display":":0","mode":"screen","width":1920,"height":1080,"x":0,"y":0,"depth":24,"bands":9,"bytes":332245,"sha256_16":"020e0fa634284e4c","hint":"Pass this path to vision_analyze to read the screen; compare sha256_16 across captures to prove the screen changed (or did not)."}
```

四个能力要点，全部来自这一条 JSON：

1. **纯 Go**：`depth=24`、`bands=9` 是它自己按 X11 GetImage 协议分带读回来的；
2. **三档取景**（全屏 / 活动窗口 / 自己所在的窗口 / 任意矩形）由帮助页给出；
3. **`sha256_16` 就是「屏幕变没变」的判据**，不需要看图；
4. **它自己告诉你下一步**：`hint` 字段直接指向 `vision_analyze`。

窗口普查也是真的：

```console
$ gopi tool capture --list-windows
"total":137,"truncated":false
```

### 3.2 `act` — 反向的、只读的、可 dry-run 的注入

```console
$ gopi tool act info
{"action":"info","display":":0","dry_run":false,"pointer":"1366,534","focus":"0x280000b","root":"1920x1080","note":"read-only probe: nothing was injected"}
```

`act` 的能力形状是**capture → act → capture 闭环**里的中间那一环，而 `info` 这一条
证明它连「探一下」都区分「注入」与「只读」。

### 3.3 `annotate` / `gif` — 把画面变成可带走的证据

```console
$ gopi tool annotate <in.png> --out <out.png> --mark "rect:120x60+10+10:probe:fixed"
✅ /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/help/86-tool-shot-self-output.png → /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/91-annotate-real.png  1440x2800  marks=1 labels=1 墨迹=3408 像素 anchor=none
```

```console
$ gopi tool gif --frames a.png,a2.png --out /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/94-gif-real.gif --caption "a.png=第一步" --caption "a2.png=第二步"
✅ 2 帧 → /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/94-gif-real.gif  113x170 (每帧 113x120 + 文字带 50 行) delay=25cs loop=forever(0) 调色板槽位=229 实际=229 色 非灰阶=0 饱和=0 色 墨迹=908 像素 字节=4749
```

`gif` 的两条拒绝都**指着原因**，这是能力的一部分（拒绝质量 = 可用性）：

```console
$ gopi tool gif --frames <directory> --out x.gif
gopi gif: read frame /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/frames: read /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/frames: is a directory

$ gopi tool gif --frames a.png,b.png --out x.gif     # b.png 与 a.png 不同宽
gopi gif: frame /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/frames/b.png is 111x120 but frame /home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/frames/a.png is 113x120 — every frame of one GIF shares a canvas, so this is refused rather than cropped
```

### 3.4 `shot` — 对着 URL 与对自己的 UI 截图

```console
$ gopi tool shot --self
/home/user/workspace/01a1aa6d-17a9-7b54-8524-8159134b130d/shot-1793896093.png
```

它用 Chromium（`internal/shot/chrome.go:6` 的查找顺序：
`~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome` → PATH），
**但用的是浏览器二进制，不是 Playwright/WebDriver 自动化**：
`grep playwright|puppeteer|selenium|webdriver` 的 19 处命中全部是**查找 Chromium 路径**
（`261-absence-read.txt`：`chrome.go:89-101` 的缓存目录与版本比较）。

### 3.5 `text2img` — 自研 TrueType 栅格化（零 CGO、零依赖）

```console
$ gopi tool text2img t2i-sample.txt out.png
✅ t2i-sample.txt → 1 页 (31 字符, 3 行, 字号 20px, 行高 32, 折行 50, 墨迹 2583 像素, 字体链 /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc → /usr/share/fonts/truetype/wqy/wqy-microhei.ttc → /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf → /usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf → /usr/share/fonts/truetype/noto/NotoSans-Regular.ttf → /usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf → /usr/share/fonts/truetype/freefont/FreeSans.ttf → /usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf)
```

能力要点：**它把整条字体链打出来**，所以「为什么这个字形长这样」是可回答的；
`墨迹 2583 像素` 是像素级真值（Rule 10 的 IoU 验证口径），不是 OCR 分数。

`text2img-verify` 是这条链的**回归检查器**，用固定样本 `知知知`：

```console
$ GOPI_T2I_VERIFY_DIR=<dir> gopi tool text2img-verify 64
font=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc size=64 pages=1 chars=4 out=[/home/user/.gopi/sessions/01a1aa6d-17a9-7b54-8524-8159134b130d/artifacts/t2i-verify/z_64px.png]
```

### 3.6 `vision_analyze` — 真跑，读出真字

```console
$ gopi tool vision_analyze 90-text2img-real.png --prompt "读出图片里的文字，只输出文字"
【vision 分析结果】(模型 deepseek-flash, 输入 221 tokens / 输出 107 tokens, 图片 5857 bytes)
gopi self-inventory C
知知知 渲染验证
```

它把**渲染出来的 PNG**读回了原文本 —— 这是 capture → vision 这条链的端到端证明，
而不是「工具有个描述」。同时它自报 `输入/输出 tokens`，计量口径与 2.7 一致。

### 3.7 `voice_input` — 只有 STT，没有 TTS，且默认上传

```console
$ gopi tool voice_input /tmp/does-not-exist.wav
exit=1
[voice] PRIVACY — engine sauc UPLOADS the audio to wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async (X-Api-Resource-Id volc.seedasr.sauc.duration); the recording leaves this machine
[voice] sauc endpoint from settings "audio_engine_param.sauc.endpoint" (a configured endpoint wins verbatim, and it overrides BOTH arms)
[voice] sauc credential: falling back to the legacy doubao file /home/user/.doubao/v2_voice.env
[voice] sauc channel → wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async (resource volc.seedasr.sauc.duration), credential /home/user/.doubao/v2_voice.env
gopi tool voice_input: sauc engine: sauc v3 asr: request: read the audio "/tmp/does-not-exist.wav": stat /tmp/does-not-exist.wav: no such file or directory (reqid c05b7ba3-de09-4194-4566-4fe3da581a19)
```

三件事同时被证明：

1. **方向是单向的**：`speech-to-text`（`29-tool-voice_input-h.txt:1`），
   `grep -rniE '\btts\b|text-to-speech|espeak|festival'` 的 6 处命中全是
   `EnableSpeakerInfo` 与测试函数名 `TestChatGeometryNoticeSpeaksWhenTheTerminalIsSmaller`，**没有 TTS**。
2. **隐私前置**：`PRIVACY` 段在错误之前打印，且点名**音频离开本机**。
3. **凭证有回退链并如实说**：settings → 旧 doubao 文件，选择过程逐行打印。

### 3.8 `chat` / `ui` / `cli` / `web` — 四条人机入口

| 入口 | 逐字能力行 | 来源 |
|---|---|---|
| `gopi ui` | `the terminal chat surface: a scrolling transcript, live tool lines and an input box, against the same engine as gopi web` | `90-ui-h.txt:1` |
| `gopi cli` | `chat in a TEXT-mode browser (zero-dependency terminal carrier)` | `91-cli-h.txt:1` |
| `gopi web` | `serve a local browser chat UI (pure-Go, zero deps)` | `41-web-h.txt:1`；`00-top.txt:116` 的 `gopi web [--tools]  Local browser chat UI (SSE).` |
| `gopi chat` | `Interactive multi-turn REPL.` | `00-top.txt:115` 的 `gopi chat [--tools]  Interactive multi-turn REPL.`；完整旗标见 `40-chat-h.txt` |

`web` 有明确的**默认安全与显式放开**：

```
$ sed -n '13p;32p' 41-web-h.txt
  --addr :PORT   listen address (default 127.0.0.1:8090). GOPI_WEB_ADDR env overrides.
approval gate). For other-device access, `gopi web --addr=0.0.0.0:8090` (or the
```

**注意一条实测的坑（本文记录它，以免误用）**：`gopi ui` / `gopi cli` 裸调是**交互 TUI，
会一直阻塞**；探测必须 `-h` 或 `timeout 5`。本 run 为此损失了一次 2 分钟超时。

### 3.9 IM：明确不存在，唯一外联是任务通知 webhook

四条独立取证，全部为 0 或唯一一条设置键：

```
$ grep -rniE '\bmcp\b' --include=*.go internal/ cmd/                → 0 行（exit=1）
$ grep -rniE "api\.telegram\.org|slack\.com/api|discord(app)?\.com/api|open\.feishu\.cn|oapi\.dingtalk\.com|qyapi\.weixin\.qq\.com|api\.line\.me|onebot" --include=*.go .   → exit=1, 0 行
$ grep -rn "IM bot\|im_bot\|imbot" --include=*.go internal/ cmd/      → exit=1, 0 行
$ grep -c -i 'telegram|slack|discord|feishu|dingtalk' <(gopi --help-env)  → 0
```

**唯一一条能出机器的东西**：

```
$ gopi --help-env  (02-help-env.txt:155-156)
  GOPI_TASK_NOTIFY_WEBHOOK         notify.webhook              —
    Task-notify webhook URL (HTTP POST on task transitions; settings key notify.webhook)
```
```go
// internal/config/config.go:745
NotifyWebhookEnv = "GOPI_TASK_NOTIFY_WEBHOOK"
```

所以准确的结论是：**gopi 没有 IM bot 接入；它有一个单向、由任务状态转移驱动的 HTTP
webhook 推送**。把它说成「支持 IM」是错的，把它说成「没有外部可达性」也是错的。

---

## 4. 只在帮助页出现 / 近似死代码的格子

这一节的口径是 Rule 43：**`grep` 命中数不是判据，命中行才是**。本 run 自己踩过一次，
记在下面第 4.3 条，因为它正是一个会误导读者的假阳性。

### 4.1 `-h` 能给出页、但页不属于它的词

| 词 | `gopi X -h` | `and "X" is not a gopi verb`（裸调第 2 行） | 实际 |
|---|---|---|---|
| `mcp` | 顶层帮助 exit 0（md5 `85807fccdd745f917d2759dd66a2828b`） | **出现** | **无此命令**（源码 `\bmcp\b` 0 命中） |
| `login` | 顶层帮助 exit 0（同 md5） | — | 源码无 `CliVerbLogin`；`grep -rnw` 0 命中 |
| `daemon` | 顶层帮助 exit 0（同 md5） | **出现** | 无此命令（`daemon_retired.go` 只有 merger 的退役面，无 daemon 词） |
| `serve` | 顶层帮助 exit 0（同 md5） | **出现** | 无此命令（`taskverbs.Serve` 只作 `gopi task serve` 的退役别名） |
| `term` | 顶层帮助 exit 0（同 md5） | **出现** | 无此命令 —— 审的是 `cli`（`cli_term.go:107` 判 `CliVerbCLI`） |
| `replay` / `checkpoint` / `stats` / `raw` / `resume` / `memsync` / `completion` / `setup` / `archive` / `log` / `brew` | 顶层帮助 或 无 | 不适用（**先被退役表接住**） | **退役**，裸调 exit 1 并指名替代命令 |

退役这一条不是死代码 —— 它有活的拒绝路径和指路的替代命令。**11 个词逐一裸调，
全部 exit 1**，且分两种措辞（这个区别有意义：前者是「这词从来/现在不是命令」，
后者是「它曾经是，搬走了」）：

```console
$ for w in replay checkpoint stats raw resume memsync completion setup archive log brew; do \
    printf '%-12s ' "$w"; gopi $w 2>&1 | head -1 | cut -c1-110; done
replay       gopi: "replay" is not a gopi command — this binary would have started a RUN with it as the prompt (and regis
checkpoint   gopi: `checkpoint` is RETIRED — use `gopi session checkpoint` (`gopi session checkpoint -h` for its help). T
stats        gopi: `stats` is RETIRED — use `gopi session stats` (`gopi session stats -h` for its help). To send the word
raw          gopi: `raw` is RETIRED — use `gopi session raw` (`gopi session raw -h` for its help). To send the word as a 
resume       gopi: `resume` is RETIRED — use `gopi session resume` (`gopi session resume -h` for its help). To send the w
memsync      gopi: `memsync` is RETIRED — use `gopi memory memsync` (`gopi memory memsync -h` for its help). To send the 
completion   gopi: `completion` is RETIRED — use `gopi config completion` (`gopi config completion -h` for its help). To 
setup        gopi: `setup` is RETIRED — use `gopi config setup` (`gopi config setup -h` for its help). To send the word a
archive      gopi: `archive` is RETIRED — use `gopi task archive` (`gopi task archive -h` for its help). To send the word
log          gopi: `log` is RETIRED — use `gopi session log` (`gopi session log -h` for its help). To send the word as a 
brew         gopi: `brew` is RETIRED — use `gopi memory brew` (`gopi memory brew -h` for its help). To send the word as a
```

**判据价值**：退役转移是**一对多对一**——11 个旧词现在全部收进 4 个族
（`session` 5 个：checkpoint/stats/raw/resume/log；`memory` 2 个：memsync/brew；
`config` 2 个：completion/setup；`task` 1 个：archive），而 `replay` 不属于退役族，
它是一条独立的「你本来会花掉一次真实 run」的拒绝。**这是一条对能力盘点真正有用的事实：
顶层动词表已经收敛成「一个族一个名词」的形状，扁平动词被系统性收走了。**

### 4.2 子动词共用父页：三处

判据是「与父页比 md5」，逐个子动词量一遍（父页行数一起量，否则
「行数相同」和「字节相同」分不开）：

```console
$ B=./bin/gopi; md5(){ "$@" 2>&1 | md5sum | cut -d' ' -f1; }
$ md5 $B tool sandbox -h
8cc637aa1b85eae942646db8098ac30a
$ for s in create run show; do echo "sandbox $s $(md5 $B tool sandbox $s -h) lines=$($B tool sandbox $s -h 2>&1|wc -l)"; done
sandbox create 8cc637aa1b85eae942646db8098ac30a lines=138
sandbox run    8cc637aa1b85eae942646db8098ac30a lines=138
sandbox show   8cc637aa1b85eae942646db8098ac30a lines=138
$ md5 $B session -h
aec51fbeb6cf7a60b130a95f931882a8
$ for s in list show log stats resume checkpoint raw notes replay; do echo "session $s $(md5 $B session $s -h) lines=$($B session $s -h 2>&1|wc -l)"; done
session list        aec51fbeb6cf7a60b130a95f931882a8 lines=96
session show        aec51fbeb6cf7a60b130a95f931882a8 lines=96
session log         2ed669e8b3a213e92c6c5578ede3f201 lines=130
session stats       a88425789a38ba34014f707d5e8c8330 lines=94
session resume      c5ad5143d1fe40756340b32f42234a9f lines=106
session checkpoint  18660849c7c012a8cbdda77ac9c9e2df lines=75
session raw         011aa21a474787bb06143b1e03298469 lines=54
session notes       52cecddb2c63ee210727700eeb7d02a4 lines=26
session replay      2255d3da108c87b8a85dc2b305c982fd lines=154
```

**所以「`-h` 有输出」永远是假的证据**，真正的判据只有两种：与父页 md5 不同，
或者源码里那个 word 有独立 predicate。共用父页的正好 5 个子动词
（`sandbox create|run|show`、`session list|show`），其余 7 个子动词各有独立页。

这不是缺陷，但**它意味着「`-h` 有输出」不能证明子动词被单独实现**：
`sandbox show` 的真实行为要靠调用看（`gopi tool sandbox show` → `sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s`）。

### 4.3 一个常量名的同音陷阱（本 run 亲自踩到，写下来防止复述）

第一轮死代码探针跑出「`CliVerbText` 已定义、无生产调用者」。**这是假的**：

```
$ grep -oE '^\s+CliVerb[A-Za-z]+' cmd/agent/cli_verbs.go     # 正则停在数字前
	// CliVerbText2imgVerify is the `gopi tool text2img-verify` verb — render the `知`
	    CliVerbText2imgVerify = "text2img-verify"
$ grep -rnw 'CliVerbText' --include=*.go cmd/ internal/ | wc -l
0
```

`CliVerbText` **这个符号根本不存在**；`grep -oE "[A-Za-z]+"` 在 `Text2imgVerify` 的
数字处截断了，于是「未使用」的判决落在了一个前缀上。改用整词匹配后，
`CliVerb*` 常量**没有一个**是「只有定义、没有生产调用者」。
**教训（与 Rule 43 同一条）**：判残留必须读命中行或用 `-w`，不能数命中数。

### 4.4 真实存在的两处「只在某一路由出现」

下面两条**不是**死代码，但容易被误读成死代码，所以列出它们的真实归属：

| 名字 | 常量 | 在 agent wire（`claudeToolDefs`，17 项）？ | 真实归属 |
|---|---|---|---|
| `chat_surface` | `internal/toolnames/names.go:173` | **不在** | chat wire 专用（`internal/agent/chat_ops.go:486` 起长注释解释为什么它不能进 `claudeToolDefs`） |
| `voice_input` | `internal/toolnames/names.go:147` | **不在** | CLI 子命令（`cmd/agent/cli_tool.go:154`、`cli_voice.go:25`、`cli_tool_spelling_table.go:112-114`） |

`claudeToolDefs` 的 17 项（`internal/agent/loop.go:1264` 至 `internal/agent/loop.go:1791`，逐项 `toolnames.ToolX`）：
`read`、`git_blame`、`write`、`edit`、`bash`、`code_search`、`keyword_search`、
`web_search`、`web_fetch`、`memory`、`context_trim`、`task_submit`、`job`、`task`、
`vision_analyze`、`text2img`、`screen_capture`。
**`voice_input` 与 `chat_surface` 都不在其中** —— 前者是 shell 侧能力（不需要模型决定
何时录音），后者只在有 chat 屏可读时才有意义。

---

## 5. gopi 明确**没有**的东西（每条都是实测零或具名拒绝）

| 能力 | 判据（实测） | 结论 |
|---|---|---|
| **MCP**（Model Context Protocol） | `grep -rniE '\bmcp\b' --include=*.go internal/ cmd/` → **0**；`gopi mcp -h` 打印顶层帮助（不算证据，见第 0 节） | 不存在 |
| **IM bot 接入**（Telegram/Slack/Discord/飞书/钉钉/企微/WhatsApp/LINE/OneBot） | 逐词整词匹配 `--include=*.go`：`telegram` **0**、`feishu` **0**、`dingtalk` **0**、`discord` **0**；五个 IM API 主机名（`api.telegram`/`hooks.slack`/`open.feishu`/`oapi.dingtalk`/`discord.com/api`）**各 0 行** | 不存在；只有单向 webhook |
| （同上的一条反证）`slack` **111 行**，但**没有一行是 IM** | 全部是英文词义「余量」（`internal/testutil/runfixture_test.go:58` 的 `the bound did not hold (budget + WaitDelay + slack)`；`internal/standards/retired_pipeline_reference_gate_test.go:41` 的 `It is slack deliberately (≈20% below`），命中集中在 `cmd/agent/cli_usage_budget_test.go`(31)、`internal/standards/*`、`internal/lineedit/*` | **假阳性，逐行读过** |
| **TTS / 语音合成** | `\btts\b\|text-to-speech\|espeak\|festival` → 6 处命中**全部**是 `EnableSpeakerInfo` 与测试函数名 | 不存在；只有 STT |
| **浏览器自动化 API**（Playwright/Puppeteer/WebDriver/Selenium） | 19 处命中全部是**查找 Chromium 二进制**（`internal/shot/chrome.go:89-101`），无控制协议 | 不存在；`shot` 是一次性渲染截图 |
| **多 provider 注册表** | `\bprovider` 在 `internal/config/*.go` 8 处命中，无表结构；换厂商靠 `base_url` 或 `gopi tool proxy` | 不存在 |
| **OpenAI wire 作为 agent 协议** | `chat/completions` 仅 `internal/proxy/server.go:37-44` | 只有代理会说 |
| **Wayland 捕获** | `capture` 的 display 输出 `":0"`，实现是手写 X11 GetImage（`capture --screen` 的 `bands=9`/`depth=24`） | X11 only（Wayland 下按设计大声失败） |

---

## 6. 这张地图意味着什么（一句话一格）

| 面向 | gopi 的做法 | 代价 / 边界 |
|---|---|---|
| 协议 | 只讲 Anthropic Messages（`loop.go:1217-1218`） | 接 OpenAI-only 上游必须过 `tool proxy` |
| 换模型 | 换 `base_url`，视觉另有 `vision_base_url` | 没有 provider 表，也没有自动选模 |
| 可观测 | 会话 / 原始报文 / 细胞网格 / 对质器**四层并存** | `--target chat` 在 task run 里看到的是别人的屏 |
| 计量 | 自己的 BPE 与服务器 usage **并排差分**（`diff=+0`） | 差分只在 raw log 存在时有第二列 |
| 多模态入 | 纯 Go X11 捕获 + 自研 TrueType 渲染 + vision 读 | X11 only；无 TTS |
| 人机入口 | `ui` / `cli` / `web` / `chat` 四条，互不替代 | 裸调 `ui`/`cli` 会阻塞 |
| 对外触达 | 任务 webhook 一个单向出口 | 没有 IM bot |
| 依赖 | 一条 wire、一个基址、零依赖渲染与捕获 | 换来的是「可对账」而不只是「能用」 |

---

## 7. 证据索引（全部在 session 产物目录，不入库）

- 帮助转储：`artifacts/help/*.txt`（114 份，编号即采集顺序）
- 本轮补采的复验（2026-11-06T00:42 在本 ground 内重跑，全部落盘）：
  `70-reverify-absence.txt`（`\bmcp\b` 0、`chat/completions` 3、`text-to-speech` 0）、
  `71-slack-shape.txt`（`slack` 111 行的逐行形状 + 五个 IM API 主机名各 0）、
  `60-run-session-list.txt`（真跑，50 session）、`61-run-session-stats.txt`（无参数 exit=1 的选输入拒绝）、
  `62-run-tool-tokens.txt`（exit=2 的用法拒绝）、`63-run-ui_state.txt`（真跑，20770 行细胞网格）、
  `64-run-session-show.txt`（无 id exit=1）、`65-run-session-stats-cost.txt`（真跑，本会话成本）
- 逐主题取证文件（含命令逐字与原始输出）：
  `140-verb-dispatch-census.txt`、`151-tool-dispatch-census.txt`、
  `160-mcp-evidence.txt`、`113-im-conclusion-evidence.txt`、`260-absence-probes.txt`、
  `261-absence-read.txt`、`190-help-vs-dispatch.txt`、`191-login-and-retired.txt`、
  `230-subverb-help.txt`、`240-rule43-homonym.txt`、`201-wire-thinking-evidence.txt`、
  `250-provider-wire.txt`、`170b`–`170e`（capture/act/gif 真跑）、
  `180-tokens-replay-notes.txt`、`210-vision-voice-real.txt`、`241-text2img-verify-real.txt`
- 真实产物样本：`90-text2img-real.png`、`91-annotate-real.png`（1440x2800）、
  `93-capture-real.png`（1920x1080, sha256_16 `020e0fa634284e4c`）、
  `94-gif-real.gif`（2 帧, 4749 字节）、`86-tool-shot-self-output.png`
