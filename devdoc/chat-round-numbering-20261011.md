<!-- doc-class: historical -->
# `session --rounds` 编号错位:定性、根因与修法

**Task**: `01a12a61289f7d789d2f00cd974e8315` · **Branch**:
`task/01a12a61289f7d789d2f00cd974e8315` · **Commit**: `efbf8941b`

## Completed

### 1. 定性:写侧缺陷(不是"合法无计数行")

`gopi session log <id> --rounds` 里的 `(this round carried no content)` 行**不是**
一个合法的、真的没有内容的 exchange。判据有三条,都实测过:

- 该行 `completion_tokens > 0`。会话 d8943792 的 70 条 `request` 行里 20 条被判空,
  **20 条全部**带有非零 completion(如 ord=2 → 1078、ord=7 → 2898、ord=70 → 31144)。
  真的空 usage 行不存在。
- 20 条空行分布 = **每个回合的最后一条 request 行**(20 个 operator 回合);
  19 条是非末回合,第 70 条是末回合(它的答案落在 `session_end` 之后,
  reader 按设计给它 ordinal 0 — 这一条确实无行可归,但同样是写序造成的位移)。
- 内容并没有丢,只是**挪到了下一个序号**:`--rounds` 显示 round 2 空,
  而 round 3 的 TOOLS/正文正是 round 2 那次交换的产物。

### 2. 根因(5-why)

**机制:`request` 行是"闭合标记",不是"开始标记"。** `internal/gopilog/reader.go`
的 `flushIter(requestOrdinal)` 在**遇到** `request` 行时,把**该行之前**累积的内容块
盖上**这一行**的序号(`reader.go:111`、`273`–`287`)。因此"第 N 轮的内容"必须写在
"第 N 个 `request` 行"**之前**。

用 gopilog 自己的 reader 读 d8943792 的日志,机制无可争辩:

```
iter#0  scope=1   tools=1  comp=88      (round 1 的 tool_use,闭合在 row 1)
iter#1  scope=3   tools=2  text=1 comp=1271   交接文件已读完…   ← 第 1 回合的答案,归给了第 3 行
iter#5  scope=8   tools=2  text=1 comp=3214   明白。在你确认前…  ← 第 2 回合的答案,归给了第 8 行
request rows=70  content rounds=51
```

任务路径 (`loop.go`) 顺序是**对的**:先 `LogAssistantText`/`LogAssistantToolUse`
(`loop.go:4524`/`4527`),后 `LogRequest`(`loop.go:4646`)——所以任务会话从不出现空行。

**写入点:`internal/agent/chat_ops.go` 的工具型 chat 回合。** 顺序**反了**:

| 位置(fix 前) | 动作 | 粒度 |
|---|---|---|
| `chat_ops.go:677` | `LogAssistantToolUse` | 每轮 |
| `chat_ops.go:689` | `LogRequest(round, …)` | 每轮 |
| `chat_ops.go:812` | `LogAssistantText(finalContent)` | **每回合,在 for 循环之后** |

于是每个回合的落盘顺序是:
`… tool_use(轮k) → request(k+1 行) → … → request(末行) → assistant text(本回合答案)`。
答案闭合了**下一回合的第一行**,自己那一行空着;下一回合的 prompt 又被算给被闭合的那一行。
这既解释了空行,也解释了"内容落到 round 3"。

**同一个提交 `chat_ops.go:812` 上的注释还留着一句可疑的说明**:"the assistant's TEXT
is NOT [recorded per round] — see the single LogAssistantText below"。说明这条 write
顺序是**一次性把文本挪到循环之后**时引入的 —— 它的目的是修 `pty-okpty-ok` 重复记录
(`devdoc/chat-duplicate-assistant-record.md`),而且**修重复的目的达到了**;它顺带
把文本挪出了它所属的那一轮,才产生本次的编号错位。这不是"没实现",是**顺序边角**。

### 3. 修法(只改写侧一处,读侧不动)

`internal/agent/chat_ops.go`:

1. **在写行之前**判定这一轮是不是回合的答案:
   `acceptAnswer := len(toolCalls) == 0 && strings.TrimSpace(content) != ""`
   (`chat_ops.go:685`);
2. 在该轮 `LogRequest` **之前**写 `LogAssistantText(content)`(`chat_ops.go:704`–`706`);
3. 删掉循环之后那一次 `LogAssistantText`(`chat_ops.go` 原 812 行),原地留注释说明
   为什么不能在这里写。

**once-per-turn 契约不变**:只有"被接受为答案的那一轮"写文本;工具轮的文本是工作笔记,
本来就只随它的 `tool_use` 行走。所以多轮回合仍然只留 **1 条** assistant 答案记录,
`pty-okpty-ok` 不会回来 —— 这一点由新测试的第二个用例钉住
(`TestChatRoundOrder_AnswerClosesItsOwnRow` 断言两个回合恰好 2 条答案记录)。

纯 chat 无工具路径 (`loop.go:5080`/`5091`) 本来就是这个顺序,无需改动。
读侧 (`internal/gopilog`) **一行未改**:canonical round = `request` 行 的定义与表头
承诺不变,修的是写入顺序。

### 4. 回归测试

`internal/gopilog/chat_roundorder_test.go`(两个用例)+ 兄弟包 seam
`internal/agent/chat_testseam.go`。

- **为什么放 `internal/gopilog`**:被测的编号是本包的 (`SessionCanonicalRounds`),而
  要驱动真实 chat 写入必须 import `internal/agent` —— 但 `gopilog` 已经 import `agent`
  (`formatter.go:14`),测试放 agent 包会 import cycle。seam 只导出三个 SSE 写助手 +
  一个构造器,产品代码不调用,行为零新增(与 agent 包自己测试用的是同一套 helper)。
- **测试是真的**:起一个假模型 server,跑真实的工具型 chat 回合(第 1 次调用返回
  真 `read` 的 tool_use,之后返回答案),让**真实写入侧**落盘,再用**本包自己的 reader**
  读回来断言。不是构造 JSONL 字符串。
- **用例 1** `TestChatRoundOrder_NoEmptyRound`:多个回合,断言无 `HasContent()==false`
  的轮,且 `len(Iterations)==RequestEvents`。
- **用例 2** `TestChatRoundOrder_AnswerClosesItsOwnRow`:断言字节顺序(答案行在
  下一个 `request` 行之前)+ 每回合恰好 1 条答案记录。

**红证**(把测试拷进 master 的 detached worktree `/dev/shm/gopi-master-check` 跑):

```
--- FAIL: TestChatRoundOrder_NoEmptyRound (0.01s)
    turn 1: round(s) [2] of 2 carry NO content while the turn DID produce an answer
    (stamped round 0) — the answer was written OUTSIDE the round that produced it …
--- FAIL: TestChatRoundOrder_AnswerClosesItsOwnRow (0.02s)
    no `assistant` row between the first two `request` rows
    ([session_start user assistant request tool_result request assistant user assistant request tool_result request assistant])
```

第二条 FAIL 的 kinds 列表更直接:3 轮 2 回合的日志里,第 1 个 `request` 与第 2 个
`request` 之间**只有 tool_result,没有 assistant 答案行** —— 答案被写在第 2 个
request 行之后,于是它闭合的是第 2 行。

### 5. Rule 31 真跑取证

同一个 regime(同二进制开关 `GOPI_THINKING=disabled`、同提示词、`--turns 2`、
隔离 `GOPI_HOME`),两个二进制各跑一次:

**修复前(`/dev/shm/gopi-master-bin`,master 构建)** — session `837247f3`:

```
SESSION ROUNDS — 837247f3-6559-4581-aa89-32c314525e3d (6 round(s))
     1        -       0ms     16,116     3,968        62          -  bash
     2        -        -          -         -         -          -  (this round carried no content — the reader recorded no bloc
     3        -    1291ms     48,638    36,096       151          -
     4        -     841ms     64,979    52,224       188          -
     5        -     769ms     81,391    68,480       234          -
     6        -     905ms     97,858    84,736       280          -
```

**修复后(`/dev/shm/gopi-fixed`,本分支构建)** — session `c85e99cb`:

```
SESSION ROUNDS — c85e99cb-8809-4591-b4ba-bee1d54f3d55 (6 round(s))
  CONTENT = content-carrying exchanges — 6 of these carried content (a row shows '-' in every column when its exchange recorded none)
     1        -       0ms     16,118     3,968        43          -  bash
     2        -       0ms     32,331    19,968        97          -
     3        -       0ms     48,613    36,096       150          -
     4        -       0ms     64,957    52,224       198          -
     5        -       0ms     81,386    68,480       252          -
     6        -       0ms     97,878    84,736       300          -
```

6 轮对 6 轮,空行 1→0,`CONTENT` 从 5 变 6(= `RequestEvents`),轮号 1..6 连续。
**注意 TOOLS 列**:修复前第 1 轮显示 `bash`,第 3 轮开始显示无工具;修复后第 1 轮
仍是 `bash`(同一个 tool_use 行),差异只在空行与 CONTENT 计数 —— 即改动只动了
文本归属,没有动工具归属。

`d8943792` 本身是**历史文件**,分支上的修复不会改写它(实测:用修复后的二进制读它,
`--rounds` 仍打印 `51 of these carried content`,空行仍在)。老会话要重新对齐需要重录,
不在本次范围 —— 见 `## Not Done`。

## Not Done

- **末回合的 `session_end` 尾巴:一并修好了(本报告初稿曾误判为"未动",已改正)。**
  修复前,回合的最后一次交换跑完后进程被 `--turns N` 停掉,答案文本落在 `session_end`
  之后 → reader 按设计给它 ordinal 0。同一对会话用本包 reader 读回:

  ```
  修复前 (837247f3): iter#5 scope=0 tools=0 text=1 comp=0    ← 答案在 session_end 之后,无行可归
                    EMPTY ROUND 2 of 6 ; EMPTY TOTAL=1
  修复后 (c85e99cb): iter#0..5 scope=1,2,3,4,5,6 全连续 ; EMPTY TOTAL=0
  ```

  根因是**同一个**:只要答案写在它自己那一轮的 `request` 行之前,它自然就落在
  `session_end` 之前 —— 不需要给会话结束钩子再加一次边界。所以这不是第三个缺陷,
  而是同一处 write 顺序的第二个后果。
- **不做历史会话回填**:`d8943792` 已落盘的错位不会被修复改写(实测:用修复后的二进制
  读它,`--rounds` 仍打印 `51 of these carried content`,空行仍在)。理由:读侧没有
  可靠信号能把"写序造成的位移"与"真的空交换"分开 —— 按 ordinal 去猜会改动一份
  合法落盘的记录,而读侧的 canonical 定义(一行 `request` = 一轮)是承诺,不能改成
  "猜测式补正"。老会话重新对齐需要重录。
- `TestDocAnchorsResolveInNormativeDocs` 在本次运行中 FAIL —— **与本改动无关**,
  master 上同样 FAIL(同一 detached worktree 实测:`242 pre-existing drift entries
  permitted, 52 new`,与本分支逐字相同)。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- Tests: `go test -count=1 ./internal/gopilog/` → `ok`(1.9s);
  `go test -count=1 -run 'Chat|Session|Log' ./internal/agent/` → `ok`(15.0s);
  `go vet ./internal/agent/ ./internal/gopilog/` → clean;
  `gofmt -l` 对本次三个文件 → 无输出
- Ran: `GOPI_HOME=<tmp> GOPI_THINKING=disabled GOPI_RAW_LOG=OFF <bin> chat --once --tools --turns 2 -p "Use the bash tool to count the files in the current directory, then answer in one short sentence."`
  再用 `<bin> session log <session-id> --rounds` 读回(两个二进制的原文见 §5)
- Result: 修复前 6 轮 5 CONTENT + 1 空行(round 2);修复后 6 轮 6 CONTENT + 0 空行。
  再用本包 reader 读回两份日志的逐轮归属:修复前 `iter#5 scope=0 comp=0`
  (答案掉在 `session_end` 之后)、`EMPTY TOTAL=1`;修复后 `scope=1..6` 连续、
  `EMPTY TOTAL=0`
- Commit: `efbf8941b`(fix)、`f32e90f8d`(本报告)

## Evidence (self-filed)

- 红证:`cd /dev/shm/gopi-master-check && cp <branch>/internal/gopilog/chat_roundorder_test.go internal/gopilog/ && cp <branch>/internal/agent/chat_testseam.go internal/agent/ && go test -count=1 -run TestChatRoundOrder ./internal/gopilog/`
  → `--- FAIL: TestChatRoundOrder_NoEmptyRound` + `--- FAIL: TestChatRoundOrder_AnswerClosesItsOwnRow`,rc=1(原文见 §4)
- 绿证:同命令在本分支 worktree → `ok github.com/nick/gopi/internal/gopilog 1.008s`
- 真跑对照:上表 §5 的两段 `--rounds` 输出为两条命令的原始 stdout
- 机制取证:用本包 reader 读 `~/.gopi/sessions/d8943792-.../session.jsonl`
  → `request rows=70 content rounds=51`,iter#1 `scope=3`、iter#5 `scope=8`(编号跳跃即空行)
- 与 master 无关性:`cd /dev/shm/gopi-master-check && go test -count=1 -run TestDocAnchorsResolveInNormativeDocs ./internal/agent/`
  → rc=1,`242 pre-existing drift entries permitted, 52 new`(与本分支同数)
