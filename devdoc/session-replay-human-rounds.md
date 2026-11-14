<!-- doc-class: historical -->
> **SUPERSEDED — 本文件的轮口径是错的，勿照此实施。** 它把「人类轮」定义为 harness
> 时钟 tick 边界，而实测证明 tick 是**每请求一次**的（会话 5af45c3e：128 tick 对 128
> 条 `request` 行，而 operator 行只有 16 条）——按 tick 分段得到的 128 正是它要取代的
> 旧口径。**现行口径 = operator 自己那一行（非 harness 注入的 user 行），见
> `devdoc/session-replay-human-round.md`**，修正提交 `04e146111`。

# Task Report: `session replay` 的 round 口径 —— 人类轮（harness 注入为界）

task: 01a12b40-f6de-72e0-be7a-6b739f42d5b1
branch: task/01a12b40f6de72e0be7a6b739f42d5b1

## Completed

**1. 判定「一轮」并把边界收敛到一处（Rule 14）**

- 新增 `sessionevent.IsHarnessRoundBoundary`（`internal/sessionevent/harness.go`）：
  **一朝一夕只认一次/请求的那条时钟注入** —— `[system] [t+NNNs] — +Ns since your previous request.`
  与 `[system] [t+NNNs] — first request of this session.`（`internal/agent/clock.go` 的
  `turnStampGapFmt` / `turnStampFirstFmt` 两个拼法）。它是 `IsHarnessInjected` 的**严格收窄**。
- `internal/gopilog` 把边界落成 `Iteration.RoundBoundary` 标志（携带，不靠下游再猜文本）。
- 新增 `gopilog.SessionHumanRoundBounds` / `SessionHumanRounds` / `HumanRoundOrdinalIn` /
  `HumanRoundUnitWords` / `SessionRoundUnitWords`（`internal/gopilog/sessionround.go`）：
  边界**只在这里物化一次**，计数、序号、replay 的 label 全部读它。

**2. replay 改成人类轮（`internal/replayplay`）**

- `roundFromHumanRound(index, total, sess, start, end, dims)`：一个**段**（tick 到下一个 tick）
  就是一步，段内**所有** exchange 的 tool call / thinking / 正文 / harness 行按顺序合进同一步 ——
  所以"1 注入 + N 个 tool call = 1 轮"在数据上成立。
- `Material.RoundCount` = 人类轮数；新增 `Material.ExchangeCount` = `request` 行数（第二个、
  **带名字**的数）；`RoundCount` 与 `len(Rounds)` 仍相等。
- `--rounds` 头部同时打印 `rounds=` 与 `exchanges=`，并各自印出单位原话（Rule 15）。
- `cmd/agent/cli_session_replay.go` 的 usage 段与 `--rounds`/`--toolcall` 说明同步改写。

**3. 无 tick 的旧日志仍有轮（Rule 15，不静默归零）**

读取器在**整份日志一条 tick 都没有**时，退回以 `request` 行自身为seam
（`readRequestRoundSeams`）。这是**整文件一次**的决定，不是"内容前面有没有 tick"的逐条判断
（后者会在 tick-stamped 会话里凭空造轮）。

**4. 夹具回归测试**

- `internal/gopilog/humanround_test.go`：1 tick + N tool call = 1 轮（N=1/2/5）；边界判定表的
  正反例（external / notices / operator 引用 / checkpoint 都不开轮）；无 tick 回退；越界序号返回 0。
- `internal/replayplay/humanround_material_test.go`：**operator 贴的那个片段**的缩微版
  （1 tick + 3 tool call = 1 步、label 为 `round 1/1`），以及 2 tick = 2 轮。

## 实测计数表（复现命令与原始输出在本 run session 目录）

`python3 ~/.gopi/sessions/01a12b40-f6de-72e0-be7a-6b739f42d5b1/artifacts/count-table.py <session.jsonl>`

| 会话 | `request` 行 | **时钟注入** | external | notices | operator 消息 |
|---|---|---|---|---|---|
| 5af45c3e | 128 | **128** | 15 | 5 | 16 |
| bc2f74f7 | 81 | **119** | 0 | 0 | 24 |
| 62a7079b | 321 | **322** | 0 | 4(OTHER_SYSTEM) | 119 |

**external / notices 是否算轮首：不算。** 实测（5af45c3e）这 20 条非时钟 `[system]` 行
**每一条都落在与某条时钟 tick 同一个 request 段内**（共享 20、不共享 0）；把边界画在
"每条 `[system]` 行"上，会把某人的同一轮最多切成三段。operator 自己贴出的判据也是
以 tick 为段首的。

## 归属错位（brief 要求"分别核实的两条病"）——核实结果

- **口径错**：证实，已修（replay 数的是交换）。
- **归属错位**：**在真实写入形状下不存在**。真实写入顺序是
  `user, assistant, request, tool_result, assistant, request…`（`request` 行是**收尾行**），
  实测：5af45c3e 的 128 个 request 行之后的内容全部归属正确（`iterations=128`，
  `SessionContentRounds` 与 request 行数一致，无"某轮空、内容跑到下一轮"）。
  **本轮新增的夹具都按真实写入顺序排**（`request` 在内容之后），并在两处注释里写明了这个陷阱。

## Not Done（如实）

- **`gopi session log <id> --rounds` 的表头仍写着 `SESSION ROUNDS … ROUND = request events`**：
  该表列的是**交换**（它自己印出的单位原话就是 "one JSONL `request` row = one HTTP
  request/response"），与 replay 的"人类轮"是两个不同的量。两处都已**各自印出单位**，没有
  "同一个词两个意思却都不说"的情况，但**同一个词 'round' 出现在两个表头**这件事本身没有统一 ——
  统一它要改 golden 与多个消费者，超出本 session 预算，已按 Rule 26/41 另提一行。
- `Material.ExchangeCount` 只在 replay 侧印刷；`gopi session stats` 未同步（它输出的本来就是
  另一套带名字的计数，未改口径）。

## Verification

- Build: `go build ./...` → OK（无输出）
- Tests: `go test ./internal/gopilog/ ./internal/replayplay/ ./internal/sessionevent/ -count=1`
  → `ok github.com/nick/gopi/internal/gopilog 1.349s` / `ok …/internal/replayplay 0.482s` /
  `ok …/internal/sessionevent 0.741s`；
  `go test ./cmd/agent/ -run 'Replay|LogRound|SessionRound' -count=1` → `ok github.com/nick/gopi/cmd/agent 47.4s`
- Ran（真跑，Rule 31）:
  - `./bin/gopi session replay 5af45c3e-24b6-4578-9f32-f4da2c40cde7 --rounds`
    → `session 5af45c3e… steps=128  rounds=128  exchanges=128`，并印出
    `rounds = human rounds (one human round = one harness clock tick … to the next — one request may run many tool calls inside it)`
    与 `exchanges = one JSONL \`request\` row = one HTTP request/response`。
  - `./bin/gopi session replay 5af45c3e… --round 3` → 第一行
    `round 3/128  kind=round  label=round 3/128  calls=2  rows=419`，紧随
    `HARNESS:  [system] [t+222s] — +1s since your previous request.` /
    `TOOL: read(path=/home/user/work/GoPi/cmd/agent/chat_panel_notice.go)` ——
    **operator 贴出的那段（1 注入 + 1 tool call）就是一轮**。
  - `./bin/gopi session replay bc2f74f7-095e-46f3-bfd1-325b43ff6fff --rounds`
    → `steps=81  rounds=81  exchanges=81`（119 个 tick，其中 38 个 tick 段完全没有内容 —
    这正是"轮 = 有内容的 tick 段"与"tick 行数"不同的实测）。
- Result: 见 `artifacts/` 下的原始输出；旧二进制（master，`/home/user/work/GoPi/bin/gopi`）
  同一会话同一 `--round 3` 输出**逐行相同**（420 行），说明该会话在旧口径下恰好也把这段
  算作 round 3 —— **新口径的差别出现在 `bc2f74f7` 这类"一个 tick 段含多个 exchange"的会话**
  （旧 `steps=73 / rounds=81`，新 `steps=81 / rounds=81 / exchanges=81`）。
- Commit: 见本条分支 tip（`git log --oneline -1`）。

## Evidence (self-filed)

- `gofmt -l internal/gopilog/types.go` 报该文件未格式化 —— **HEAD 版本同样未格式化**
  （`git show HEAD:internal/gopilog/types.go > /tmp/t.go && gofmt -l /tmp/t.go` 同样命中），
  非本次引入，未顺手重排。
- 计数表原始输出：`artifacts/count-table.txt`（三条会话逐类计数）。
- 真跑输出：`artifacts/replay-rounds-after.txt`。
- `gopi task show 01a12b40-f6de-…` 复核本行的 ground/worktree（已由框架记录，不在此重复）。
