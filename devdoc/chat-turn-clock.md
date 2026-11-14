<!-- doc-class: historical -->
# Task Report: chat turn stamp — 让两次用户请求之间的时间流逝对 LLM 可见

Task: `01a11f96-9336-7d9f-9c88-a1403bc88926` · branch `task/01a11f9693367d9f9c88a1403bc88926`

## Completed

1. **`internal/agent/clock.go` — 请求级时间戳（内存字段，nil-safe）**
   - `SessionClock` 增加 `lastRequest time.Time`：**纯内存**，无 timer、无文件、无 goroutine（Rule 35）。
   - `TurnStamp` 结构（`Tick` / `SinceLast` / `First`）+ `(*SessionClock).TurnStamp()`：
     同一函数既读也写 `lastRequest`，所以"上一次请求"只有一个定义。
   - `Render()`：单行 harness 前缀文本（`[system] …`），首轮说 first、之后报 `+Ns` 间隔。
   - `(a *Agent).requestMessages(keepThinking)`：在**请求组装点**把当前戳作为最后一条 user 消息追加，
     并 `SessionLog.LogUser` 落 JSONL。`Clock == nil` → 零操作，逐字节与改前一致。
   - `tickShapeLabel = "[t+Ns]/[t+~Nm]/[t+~Nh]/[t+~Nd]"`：tick 拼写的**唯一定义**，`clockRuleBase` /
     `clockRuleUnlimited` 与 chat 提示词块都引用它（Rule 1/13/14 —— 原来三处各写一遍字面量）。
     分档（2026-10-22）：`durationTier` 把 duration 折成 s/m/h/d 四档，`< 1m` 精确、其上带 `~`；
     **Tick 与 SinceLast 共用这一个函数**（`tickLabelOf` / `Render`），间隔不再出现裸秒数。
     `tickDecisionAfter = 5*time.Minute` 与其拼写 `tickDecisionLabel`（`[t+~5m]`）同源渲染，
     prompt 不再硬写 `t+300s`。

2. **两个请求组装点各一行**
   - `internal/agent/chat_ops.go:607`（ChatOps ReAct 循环每轮）
   - `internal/agent/loop.go:4969`（`Agent.Chat` 纯对话单轮）
   两条 chat 路径都从 `a.Ctx.WireMessages(...)` 改为 `a.requestMessages(...)`。

3. **chat 提示词说明该标记（只进 chat block）**
   - 新文件 `internal/agent/chat_turn_clock.go`：`chatTurnClockRules()`，由
     `chatContextRulesFor` 末尾追加 → 经 `chatOpsSystemPrompt` 与 `assembleChatSystemPrompt`
     同时到达两条 chat 路径。`assembleSystemPrompt()`（task registry）**不含**该块。
   - 本机 chat 会话实测被说服：模型终局 thinking 原文
     `The [CLOCK] note: +64s gap. Nothing significant to add.`

4. **测试** `internal/agent/chat_turn_clock_test.go`（6 个新用例 + clock_test.go 既有回归）
   - `TestChatTurnClock_PlainTurnCarriesTick` —— 纯对话轮（0 tool call）请求带戳（本 epic 核心）。
   - `TestChatTurnClock_TickIncreasesAcrossRequests` —— 两次请求 tick 单调。
   - `TestTurnStamp_FirstAndGap` —— 首轮不给假间隔；第二轮报实测间隔（最小样本，无 HTTP）。
   - `TestChatTurnClock_NilClockIsNoOp` —— nil clock 无操作，且每请求只多 **1** 条消息（O(1)）。
   - `TestChatTurnClock_IncrementIsBounded` —— 增量字符/token 上限断言。
   - `TestChatTurnClock_ToolResultTickUnchanged` —— 既有 tool result tick 未回退。
   - `TestChatTurnClock_PromptBlockIsChatOnly` —— 块在两条 chat 提示词里、task 提示词里没有。

## Not Done

- **`loop.go:4380`（task ReAct 循环）未接入 requestMessages。** 有意为之：task loop 每个 round 都带
  tool result ⇒ 已经有 tick 信号，且它的 `[EPIC GOAL]` 与任务脚手架不该被夹入 chat 语义的时间戳；
  epic 的范围写的是"ChatOps 与 Chat 的请求组装点"。这不是遗漏，是一条可再派发的边界（若要把
  task 路径也统一，是一个独立的小任务）。
- **docanchor gate（`TestDocAnchorsResolveInNormativeDocs`）在 master 上就是红的**（见 Verification），
  与本分支无关：在 HEAD 的原样 checkout 上同样红，本分支新文件不出现在任何报错里。
- 未跑全树 suite（Rule 27：merge/verify 路径是 smoke、branch-scoped）。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK；`go vet ./internal/agent/` → 干净。
- Tests: `make test-budget PKGS='./internal/agent'` → 除既有的 `TestDocAnchorsResolveInNormativeDocs`
  外全绿（该用例的「既有红」判定见 E6，本 session 已按新口径复量：只加我的 commit，gate 统计行逐字不变）。
  新增/回归：`go test ./internal/agent/ -run 'TestChatTurnClock|TestTurnStamp|TestSessionClock|TestAgent_WithTick|TestAgent_Run_Tool|TestAgent_Run_TickIncrements' -count=1` → `ok 4.687s`。
- Ran（同一二进制、同一分支，两次独立的真实会话；本 session 复跑的是第二次）：
  ```
  # 会话 A（首次）
  GOPI_HOME=<SB-A> bash -c '{ printf "Reply with exactly: TURN-ONE-OK\n"; sleep 65; \
    printf "Reply with exactly: TURN-TWO-OK\n"; } | bin/gopi chat'
  # 会话 B（本 session 复跑，沙箱由 `gopi tool sandbox create` 建）
  SB=$(bin/gopi tool sandbox create --label turnclock-wire --task-id 01a11f9693367d9f9c88a1403bc88926 | tail -1)
  GOPI_HOME="$SB" GOPI_RAW_LOG=on bash -c '{ printf "Reply with exactly: WIRE-ONE-OK\n"; sleep 70; \
    printf "Reply with exactly: WIRE-TWO-OK\n"; } | bin/gopi chat'
  ```
  两轮**纯对话，零 tool 调用**（会话 B 两个请求体 tool_use=0 / tool_result=0；session.jsonl `grep -c tool_use` = 0）。
- Result: 会话 B `→ REQUEST` 计数 = 2；req#1 带 `[system] [t+0s] — first request of this session.`，
  req#2 带 `[system] [t+69s] — +69s since your previous request.`（raw log 字节偏移 99787 / 206596，
  文件 213488 字节）。戳同时落在 session.jsonl。详见下面的 Evidence。

**E9 — 第三轮独立复量（2026-10-09，另一 session，全部本轮实跑）：**

- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK；`git status --porcelain` → 空（Rule 30）；
  `git rev-list --count HEAD..master` → 0（不落后）。
- **一次自伤的假红，记下来免得下次误判**：本轮第一次跑
  `make test-budget PKGS='./internal/agent'` 时，同一个 bash 调用里先 `export GOPI_HOME=/tmp/gh-final-verify-$$`
  （为隔离 profile）再跑测试，结果 15 个失败 —— `TestSessionLogger_RecordsRawLogPath`、`LogUser`、
  `FullSession`、`ConcurrentWrites`、`TestSessionLoggerRecordsTerminalGeometry` 等。**这些与分支无关**：
  同一命令、同一棵树、`unset GOPI_HOME` 后复跑，只剩 1 个既有红。机理是 session logger 的路径解析跟着
  `GOPI_HOME` 走。纪律：`GOPI_HOME` 只用于真实 gopi 运行（sandbox / chat 取证），**绝不与 `make test-budget` 同 shell**。
- **既有红与分支无关的第三种量法**：`go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs`
  → 42 条 drift 里，`grep -cE 'chat_turn_clock|chat_context\.go|clock\.go'` = **0** —— 无一条指向本分支新增/改动的文件；
  粗粒度上也能推：`chat_ops.go` 与 `loop.go` 在 master 与分支行数相同（799/799、7920/7920），
  本分支唯一的行数变化是 `chat_context.go` 423→424（纯新增一行），而该文件出现在 0 条 drift 里。

## Evidence (self-filed)

**E1 — wire 层字节证据（raw HTTP log，`→ REQUEST` 的 JSON body，python json 解析）：**

两次独立的真实会话（同一二进制、同一分支，不同沙箱），量的是同一件事：

```
会话 A（沙箱 sandbox-turnclock-28d7da5c，16:38）
$ python3 parse(<SB>/.gopi/raw/gopi-raw-20261009-163850.log) → requests on the wire: 2
  req#1 body=14104 bytes  stamp='[system] [t+0s] — first request of this session.'
          stamp 50 bytes / 48 chars, tokens~=12
  req#2 body=14436 bytes  stamp='[system] [t+64s] — +64s since your previous request.'
          stamp 54 bytes / 52 chars, tokens~=13

会话 B（沙箱 sandbox-turnclock-wire-032d29ae，16:47，本 session 复跑，GOPI_RAW_LOG=on）
$ log=<SB>/.gopi/sessions/c277c1b6-.../c277c1b6-....raw.log  (213488 bytes)
  req#1 body=103301B  tool_use=0  tool_result=0  stamp='[system] [t+0s] — first request of this session.'
  req#2 body=103554B  tool_use=0  tool_result=0  stamp='[system] [t+69s] — +69s since your previous request.'
```

- 每轮请求上下文都带当前 tick，且 **tick 递增**（0 → 64 / 0 → 69）。
- 纯对话轮（**两个请求体里 tool_use=0 且 tool_result=0**，会话 JSONL `grep -c tool_use` = 0）也有时间信号
  —— 这正是主缺陷。
- 刻意 `sleep 70` 后，下一轮上下文报 `+64s` / `+69s`，与真实间隔一致（间隔被如实 visible）。
- 每轮增量 **48 / 52 字符（50 / 54 字节 —— 同一文本的两种单位，多出的 2 字节是 `—` 的 UTF-8 三字节）**，
  约 12–13 token（chars/4），**O(1)**：长度只随两个计数器的位数变化，上限断言
  `turnStampMaxChars=80` / `turnStampMaxTokens=24`。

**E2 — 同一戳在会话 JSONL 里（审计半边，`grep` session.jsonl）：**

```
[system] [t+0s] — first request of this session.
[system] [t+64s] — +64s since your previous request.
```

**E2b — 模型真的读了它（采纳证据，会话 A 的第 2 轮 thinking 原文）：**

```
$ python3 <parse session.jsonl> | grep thinking
… Keep it consistent — output TURN-TWO-OK then the block.
The [CLOCK] note: +64s gap. Nothing significant to add.
Reply in zh-CN for prose parts, but the required literal is TURN-TWO-OK.
```

提示词块（E4 的 `chatTurnClockRulesText`）教的是「先读这个戳再重复上一轮的时间假设」，模型的思考里
出现的正是这块教它读的那样东西（`[CLOCK] note: +64s gap`）——**这就是 Rule 11 意义上的「被说服使用」**：
不是「代码写好了」，是 LLM 在实际会话里用了它。
（该 thinking 落在会话 A 的 `session.jsonl` 里；两轮 chat 的 raw log 与 session.jsonl 都各有 2 次 `CLOCK`
块，说明块在两条 chat 路径上都到达了 wire。）

**E3 — 原始 raw log 内偏移（非解析，直接字节）：**

```
$ grep -ob '\[system\] \[t+[0-9]*s\] — [^"\\]*' <SB>/…/c277c1b6-….raw.log
99787:[system] [t+0s] — first request of this session.
206596:[system] [t+69s] — +69s since your previous request.
$ wc -c …/c277c1b6-….raw.log → 213488
```

（首次会话用的是旧版扁平布局 `<gopi dir>/raw/gopi-raw-20261009-163850.log`，偏移 14255 / 40802、文件 63631 字节；
本轮复跑落在会话目录内的 `<session-id>.raw.log`。两者的**戳字节相同**，差别只在文件落点。）

**E4 — 提示词块只进 chat（`TestChatTurnClock_PromptBlockIsChatOnly` 实跑）：**
`chatSystemPromptServed(nil)` 与 `chatOpsSystemPrompt(...)` 均含 `chatTurnClockMarker`；
`assembleSystemPrompt()`（task）不含。**绿**。

**E5 — 既有 tool result tick 未回退：** `TestAgent_Run_ToolResultCarriesTick`、
`TestAgent_Run_TickIncrementsAcrossRounds`、`TestChatTurnClock_ToolResultTickUnchanged` 三条同时绿
（`ok github.com/nick/gopi/internal/agent 4.973s`）。

**E6 — docanchor 是既有红，不是本分支（本 session 复量，口径修正）。
首次报告的「HEAD 原样 checkout 上 46 行」是**错的**：它拿 `git archive master` 的树去跑，却把
`/tmp/docanchor-master.txt` 说成 HEAD；46 是 master 基线里**恰好指向我改过的两个文件**的行数，
不是「master 红的总量」。本 session 用两个只读树重新量，口径写清楚了：

```
树 1 = `git archive master` → /home/user/workspace/gopi-test-scratch/pristine-docanchor-01a11f96
        （master=9a58fb6c7，与本分支的 merge-base 同一 commit）
树 2 = 树 1 + 只打我的那一个 commit（git format-patch -1 d9e9910c1 | git apply）
        → /home/user/workspace/gopi-test-scratch/mineonly-01a11f96
```

| 量 | 树 1（master） | 树 2（master + 我的 commit 单独打上） |
|---|---|---|
| gate 退出码 | 1（红） | 1（红） |
| 统计行 | `3089 across 693 normative docs (… 242 permitted, 42 new, 12 stale permits)` | **同一行，逐字相同** |

**「只加我的 commit，gate 的每一个数字都不动」= 这 42 个 new drift 没有一个是我的。**
方向性也一样：本分支相对 master 新增 1 篇 normative doc（`chat-turn-clock.md` 我分类成 historical，
不进 gate），少 1 篇（`voice-input-default-local.md` 的一行白），净 693→694；
掉的那两行（`salvage-…cli_tasks.go:3149`、`voice-input-default-local.md` 的 `cli_voice_local.go:267`）
是本分支**继承**的已合入 commit 带来的，不是我的 diff。

**我实际改过的两个文件的锚点全部完好**（下面这 10 条是本分支红清单里指向 `internal/agent/loop.go`
/ `chat_ops.go` 的全部锚点，逐条比对同一行号在两个 rev 的文本）：

```
$ python3 <compare> → total mine: 10 | identical at master & branch: 9 | differing: 0 | out-of-range: 1
  internal/agent/chat_ops.go:1854   master nlines=800 OOR / branch nlines=800 OOR   ← 文件只有 800 行，两个 rev 一样
  其余 9 条（loop.go:1903/2094/2475/2857/3072/3449/3523/4013/4501）两个 rev 都是 BLANK，逐字相同
```

依据：`internal/agent/loop.go` 两个 rev 都是 **7920 行**（我一行都没加删），`chat_ops.go` 也是 **799 行**
（我的改动是**替换** `a.Ctx.WireMessages(...)` → `a.requestMessages(...)`，同 1 行）。所以这 10 条锚点在
master 与分支上解析到的是**同一行文本**，漂移不可能由我引入；且它们不在报告首版的清单里是因为
首次只统计了「指向我改过的文件」的行，本次把判据换成了「同一行文本在两个 rev 是否相同」。

**顺带发现（不在本 epic 范围，但值得记账）：本分支的 diff 不是我的 diff。** `git cherry -v master HEAD`
显示 12 个 commit 全部是 `+`（未在 master 中找到等价物），其中 11 个是**继承来的**（memsync / voice /
log-counter / help 等），它们经由两次 `delivery-point sync` merge（`888b3cc68`、`1620e667d`）进了分支。
这批 commit 是否已在别处落地、要不要单独搬回 master，是另一件工作 —— 本 epic 的答案里
只需要它是**既有红**、不是我的增量。`git merge-base --is-ancestor` 已确认 `master` 是 `HEAD` 的祖先
（Rule 的 step 7 成立：本分支从 master 下降）。

**E8 — 生产边界（`--resume` 的诚实声明）：** `gopi chat` 一个进程 = 一个 run-length 会话
（`runChatWithInput` 复用同一 agent + 同一 clock 跑每一 turn），`chat --once` = 一 turn。
`--resume` 会用 raw log 播种对话但**新建 clock**，所以被 resume 的会话首轮报 First —— 这是故意的：
那段间隔本进程从未测量，报一个数字就是编造（Rule 15）。已写入 `clock.go` 的字段注释。

**E7 — 格式与字符串单一来源：** `grep -rn 't+NNNs' internal/ cmd/ --include=*.go` 在生产代码里
现在只有 `clock.go` 的 `tickShapeLabel` 一个定义，`clockRuleBase` / `clockRuleUnlimited` /
chat 块都引用它。

**E10 — 第四轮独立复量：一段**本轮实测**的纯对话两轮字节证据（Rule 31 的最小自足样本）。

不借用前几轮的日志，本轮新建沙箱 `sandbox-turnclock-r3-5eade7c9`
（`./bin/gopi tool sandbox create --label turnclock-r3`）并当场跑：

```
$ GOPI_HOME="$SB" GOPI_RAW_LOG=on bash -c '{ printf "Reply with exactly: R3-ONE-OK\n"; \
      sleep 70; printf "Reply with exactly: R3-TWO-OK\n"; } | ./bin/gopi chat'
chat exit=0
```

产物（会话 `8be35e2c-a76a-44cf-a3e7-77360fd99ec9`，raw log **212145 B**，本轮解析）:

```
→ REQUEST 标记 2 个（真跑两次请求），tool_use 0 / tool_result 2 段为空 ⇒ 两轮都是纯对话轮
req#1 body_bytes=99773  msgs=1  tools=0  tool_msgs=0
      last-msg role=user stamp='[system] [t+0s] — first request of this session.'  chars=48 ~tok=12
req#2 body_bytes=100049 msgs=3  tools=0  tool_msgs=0
      last-msg role=user stamp='[system] [t+69s] — +69s since your previous request.'  chars=52 ~tok=13
每戳在请求体内 **occurrences=1**（不重复注入）
```

字节级（不解析）:
`grep -ob` → `99779:[system] [t+0s] …` / `205903:[system] [t+69s] — +69s since your previous request.`

会话 JSONL（审计半边，`grep -c '"tool_use"' session.jsonl` = **0**）同样两条，tick 0 → 69 递增。

**核对**:
- 每轮请求上下文带当前 tick、tick 递增（0 → 69）—— 本轮自己量到的；
- `sleep 70` 后下一轮报 `+69s`，间隔如实可见（报的就是真实间隔，非回填）；
- **纯对话轮也有信号**（`tools=0`、零 tool 消息）—— 主缺陷的正面证据；
- 每轮增量 = 该戳 **48 / 52 字符 ≈ 12–13 token，O(1)**，与上限常量
  `turnStampMaxChars=80` / `turnStampMaxTokens=24` 一致（长度只随计数位数增长）；
- 提示词块被采纳（会话 A 第 2 轮 thinking 原文 `The [CLOCK] note: +64s gap.`，见 E2b）。


**E13 — 第五轮独立复量（2026-10-09 17:04–17:07，本轮实跑；新建沙箱，不复用任何旧日志）。**

```
$ SB=$(./bin/gopi tool sandbox create --label turnclock-r5 | tail -1)
  → /home/user/workspace/gopi-sandbox/sandbox-turnclock-r5-fdcce697 (created=true)
$ GOPI_HOME="$SB" GOPI_RAW_LOG=on bash -c '{ printf "Reply with exactly: R5-ONE-OK\n"; \
      sleep 70; printf "Reply with exactly: R5-TWO-OK\n"; } | ./bin/gopi chat'
chat exit=0
```

产物（会话 `0b3e3672-6a67-48b4-bb69-c542c7896b47`，raw log **233989 B**）：

```
→ REQUEST 标记 2 个；session.jsonl '"tool_use"'=0 / '"tool_result"'=0  ⇒ 两轮都是纯对话轮
req#1 body_bytes=103156  msgs=1  tools=0
req#2 body_bytes=103510  msgs=3  tools=0
$ grep -ob '\[system\] \[t+[0-9]*s\] — [^"]*' <raw.log>
  99779:[system] [t+0s] — first request of this session.
  227869:[system] [t+69s] — +69s since your previous request.
$ grep -o '<the stamp verbatim>' | awk length  → 50 / 54 字符（= E1–E10 的同一数字，跨轮独立复现）
$ grep -c 'since your previous request' → 2   （1 次是戳，1 次是 chat 提示词块对标记的解释）
```

**一个读法纠错（本轮自己踩的）**：用“请求体末条消息”的字符数当戳的增量会得到 77/81 ——
那是戳**加上本轮操作者提示词**的拼接长度（`Reply with exactly: R5-ONE-OK[system] [t+0s] …`），
不是戳本身。戳的增量必须**单独** grep 出来量（50 / 54 字符 ≈ 12–13 token，O(1)，
在 `turnStampMaxChars=80` / `turnStampMaxTokens=24` 之内）。E1–E12 用的都是后者，口径一致。

chat 提示词块确认落在请求体字节里（本轮 raw log）：

```
…every request carries the current tick] Every request you receive ends with the current session tick
 [t+NNNs] and, after the first, how long passed since your previous request (+Ns). Time passes between
 operator messages whether or not you were asked anything, so read that stam…
```

本轮 build/test（同一轮实跑）：`go build -o … ./cmd/agent/` → `BUILD_OK`；
`go test ./internal/agent/ -run 'TestChatTurnClock|TestTurnStamp|TestSessionClock|TestAgent_WithTick|TestAgent_Run_Tool|TestAgent_Run_TickIncrements|TestClockRule' -count=1`
→ `ok github.com/nick/gopi/internal/agent 4.090s`；`gopi tool doccheck` → 语料一致（exit 0）；
`git status --porcelain` → 空；`git rev-list --count HEAD..master` → 0。

## Files

- `internal/agent/clock.go`（TurnStamp / requestMessages / tickShapeLabel / turnStampLine）
- `internal/agent/chat_turn_clock.go`（新增：chat 提示词块）
- `internal/agent/chat_context.go`（chat 块渲染追加一行）
- `internal/agent/chat_ops.go`（ChatOps 请求组装点）
- `internal/agent/loop.go`（Agent.Chat 请求组装点 + tick 拼写引用常量）
- `internal/agent/chat_turn_clock_test.go`（新增）

## Deliverable

- 分支 `task/01a11f9693367d9f9c88a1403bc88926`（本 worktree 提交即交付，共享 refs，无需 push）
- 取回：`git show task/01a11f9693367d9f9c88a1403bc88926:devdoc/chat-turn-clock.md`
