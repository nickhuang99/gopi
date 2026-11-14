<!-- doc-class: historical -->
# Task Report: `gopi session replay` 的轮 = 人类的一轮（operator 自己那一行）

## Completed

- **推翻并改掉了错误的口径。** 上一轮把「人类轮」实现成 harness 时钟 tick 边界
  （`sessionevent.IsHarnessRoundBoundary`）。本 run 在真实会话上实测，**tick 是
  「每请求一次」的** —— 5af45c3e 有 128 条 `[system] [t+…] … since your previous
  request.` 对 128 条 `request` 行 —— 于是按 tick 分段得到 `rounds=128`，正是它
  要取代的旧口径。
- **新口径：人类轮 = operator 自己那一行**（非 harness 注入的 user 行）。
  `sessionevent.IsHumanRoundBoundary` = `!IsHarnessInjected`（精确补集，不会漂移，
  Rule 14 单一定义）。
- `internal/gopilog/reader.go`：读了 operator 行就 `pendingRoundBoundary = true`，
  它开启下一轮；harness 注入（tick / 外部进程 / notices / checkpoint）只作为该轮
  的材料（`HarnessPrompts`），永不开启一轮；若上一轮的 exchange 还开着，operator
  那一行就是 seam，先 `flushIter` 收口。无任何 operator 行的日志（resume / 无人
  输入的 worker run）回退到 `request` 行作 seam，并说明原因。
- `internal/replayplay/material.go`、`cmd/agent/cli_session_replay.go`：一步 = 一个
  人类轮，`rounds=` 与 `exchanges=` 各自具名印刷；文案改为「operator row 到下一个
  operator row」。
- 夹具改写：`internal/gopilog/humanround_test.go`（1 operator 行 + N tool call = 1 轮，
  N=1/2/5；harness 注入一律不开轮；无 operator 行时回退）、
  `internal/replayplay/humanround_material_test.go`（operator 片段缩微版）。
- commit `b...`（本 run 的修正提交）。

## Not Done

- 未在 `capture-*` 会话上跑 `replay --rounds` 做第二处真实取证（只用了 python 直读
  jsonl 数出 18 operator 行 / 36 request）。gopilog 夹具已覆盖「无 tick 的日志」。
- 未改 `gopi session log --rounds` 的表头口径（该面仍是 request 行为单位的交换表，
  且已在 replay 输出里具名说明两者不同）。

## Verification

- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: `go test ./internal/gopilog/ ./internal/replayplay/ ./internal/sessionevent/`
  —— 见 `## Evidence`。
- Ran: `./bin/gopi session replay 5af45c3e-24b6-4578-9f32-f4da2c40cde7 --rounds`
- Result: `steps=16 rounds=16 exchanges=128`；16 行 prompt 全部是 operator 的原话
  （第 1 行 36 requests / 51 tool calls 收成一轮，第 16 行正是 operator 报这个 bug
  的原话）。

## Evidence (self-filed)

- 实测（python 直读 `/home/user/.gopi/sessions/5af45c3e-…/session.jsonl`）：
  `lines 612 {'user': 164, 'request': 128, 'assistant': 158, 'tool_result': 135}`；
  其中 `ticks 128 operator_rows 16 other_system 20`。
- 第二处独立取证（`capture-*` 会话）：`req 36 tick 0 human 18 otherSys 36` ——
  tick 数为 0，tick 口径在那里根本看不见轮。
- 修正前后：`rounds=128 exchanges=128`（错，= 旧口径）→ `steps=16 rounds=16
  exchanges=128`（对）。
- 逐轮实测（python，按 operator 行切段）：`[[36,52,51],[3,4,3],…,[5,5,4]]` —
  16 段，request 合计 128。

## 回归修复（本 run 的第二处红 → 绿）

`make test-budget` 首跑 **FAIL** `TestSessionReplayKeyboard_DrivesTheTransportOverPty`
（`cmd/agent/cli_session_replay_test.go`）：夹具 `replayTestTimeline` 只有 **1 条** operator
行，新口径下 = **1** 个人类轮，`n` 前进后停在 `round 1/1  PAUSED  1x  (last round)`，
断言找不到 `2/`。修法：补第 2 条 operator 行（`a second recorded question`）——夹具注释
本就写着 "TWO model rounds"，新口径下两个 model round 要**两条 operator 行**才是两轮，
`replayTestRoundCount = 2` 因此仍成立。`go test ./cmd/agent/ -run 'TestSessionReplay'`
→ `ok 16.923s`。commit `346c5bc5f`。

先前那份按 harness 时钟 tick 定界的 devdoc（`devdoc/session-replay-human-rounds.md`）已在
文件头加 SUPERSEDED 横幅指向本文件，免得下一个人照着错口径实施。commit `9e0ab2f29`。
