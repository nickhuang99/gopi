<!-- doc-class: historical -->
# Task Report: EXCHANGE 命名（会话日志表的数量单位）——master 落地实测 + 残留缺口收口

task `01a12bcd7f7477df9f8e96967bf8abad`（rework epic；原始分支 `task/01a12b69367c73e589d83dcf40cf2733`
tip `ae48c01ce`，其 ref 已被 merge 运行删除且**不是** master 的祖先——`git merge-base --is-ancestor
ae48c01ce master` → NO，merge-base = `a3a31da42`）。

## Completed

### 1) 实测结论：intent 已基本落地（HEAD = `765d157b2`，本 worktree 起点）

| 落地物 | 位置（file:line） |
|---|---|
| `CounterUnitExchange = "exchanges"` | `internal/sessionevent/counters.go:92` |
| `CounterUnitHumanRound = "human rounds (operator rows)"` | `internal/sessionevent/counters.go:121` |
| 导出访问器 `SessionExchangeTableWord` / `SessionExchangeColumnWord` | `internal/gopilog/sessionround.go:172` / `:175` |
| 常量 `sessionExchangeTableWord="EXCHANGES"` / `sessionExchangeColumnWord="EXCHANGE"` | `internal/gopilog/sessionround.go:597` / `:600` |
| 表头 `SESSION EXCHANGES — <id> (N exchange(s))` + 单位行 + 列头 | `internal/gopilog/sessionround.go:616` / `:623` / `:649` |
| COST SUMMARY 用 `CounterUnitExchange` | `internal/gopilog/formatter.go:119` / `:936` |
| stats 侧同一单位词 | `internal/gopilog/*stats*`（golden `stats-rounds.txt` 等） |
| golden 已含 | `tests/fixtures/log-stats-raw/golden/log-session.txt:53` `exchanges: 9` |
| CLI 断言 | `cmd/agent/cli_log_round_test.go`、`cli_session_replay_test.go:544+` |

落地路径（从对象库读到的提交，不是推断）：分支 `ae48c01ce` → merge `2a9903bd0`「exchange naming
prerequisite」进 master；stats 侧由 `ab86a064e` / `877400d13` merge 补齐。**即：原 intent 的绝大部分
字节已在 master，本次不重做。**

### 2) 残留缺口（真、两处，均由 master 自己后续的改动造成）

缺口性质是**陈旧的散文断言**，不是缺功能：

- **`internal/gopilog/sessionround.go:630-639`（注释）+ `:640`（发射文本）**（行号 = 修复前 HEAD 状态，
  由 `git show HEAD~1:` 核出）：log 表那句
  「NOT what `gopi session replay <id> --rounds` numbers … **and where it still counts `request`
  rows under `rounds=`**, that is THIS table's number, not the round」。
  该断言写于 playback 尚未以 human round 为步进单位的时点；master 随后落了 human-round playback
  （`internal/replayplay/material.go:698` `RoundCount: gopilog.SessionHumanRounds(sess)`，
  `:265-273` 字段文档 `RoundCount is how many HUMAN ROUNDS … it is 0 for a dump-only material`，
  `:721+` 一分一人轮一步），并新增 `internal/replayplay/counter_units_test.go` 钉住两数分开。
  于是在**同一份 fixture 上旧断言为假**：见下 §Verification 的实测（log 表 9，playback `rounds=1`）。
- **`internal/gopilog/formatter.go:935`**：COST SUMMARY 那句「…**whatever number `gopi session
  replay <id> --rounds` prints from them**」——同一陈旧形态（把 playback 的数写成「从这些行来的」）。

两处**都没有任何测试断言**（grep `whatever number|still counts .request|where it still` 于 `*_test.go`
零命中），所以缺口不会被现有门禁发现。

### 3) 最小改动

- `internal/gopilog/sessionround.go`：注释改述 playback **当前**单位（human rounds），发射文本改为
  「…that surface's unit is the human rounds (operator rows) — the playback steps by the operator's own
  row, a different quantity from this table's exchange count」；测量证据换成本 run 实测的
  `fixture fc426fed：本表 9 exchanges，playback rounds=1 exchanges=9`。
- `internal/gopilog/formatter.go:935`：句尾改为「…and the unit `gopi session replay <id> --rounds`
  steps by — a different quantity from these rows」。
- **不改任何已落地的命名字节/常量/golden 数字**（只改这两句散文 + golden 中随之变化的那一行）。

### 4) 回归守卫（本 run 新增）

`cmd/agent/cli_session_replay_units_test.go` —— 用**真 fixture + 真二进制**同时跑两个动词，把
「一个 log 的 `rounds` 只能有一个含义」钉成**测量**而非散文：

1. log 表数量词是 `EXCHANGES` 且数字 = reader 的 `SessionRounds`；
2. playback `rounds=` = reader 的 `SessionHumanRounds`，`exchanges=` = `SessionRounds`；
3. **陷阱本身**：playback `rounds=` 必须 **≠** exchange 数（fixture 上 1 ≠ 9）；
4. **红路径**：log 表输出里不得再出现 retired 句子 `still counts \`request\` rows under \`rounds=\``。

fixture 自证非空转：`humanRounds != exchanges` 不成立时用例直接 fail（"a log with several exchanges
per operator row"）。

## Not Done

- **没有**重写/回滚任何已落地字节：原分支的 diff 未 `git apply`（Rule 23/禁令），master 上的
  命名实现按实测判为正确，保持原样。
- **没有**为 `9cead479` 那条历史测量做复核（它属于原分支的证据，本 run 无法在其上复现；新写入的
  注释改用本 run 可复核的 `fc426fed` 实测数值，避免留下无法复核的引文）。
- 原分支 `ae48c01ce` 的 ref 已消失，未做恢复（对象仍在；本 worktree 未借阅任何外来 worktree，Rule 29）。
- 未跑全树套件（Rule 27：merge/verify 路径只跑 branch-scoped smoke）。

## Verification

- Build: `go build ./...` → 无输出，exit 0；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Vet: `go vet ./internal/gopilog/ ./internal/replayplay/ ./internal/sessionevent/` → exit 0
- Tests: `go test ./internal/gopilog/ ./internal/sessionevent/ ./internal/replayplay/ -count=1`
  → 3/3 `ok`（1.284s / 0.642s / 0.368s）；
  `go test ./cmd/agent/ -count=1 -run 'SessionReplay|SessionLog|Round|CounterUnit|LogGolden'`
  → `ok github.com/nick/gopi/cmd/agent 35.803s`（branch-scoped，Rule 27：不跑全树）。
  两个命令都跑在最终提交树的**源码**上（报告自身的措辞改动不参与编译，后续 amend 只动本文件）。
- Ran（逐字，真实参数，真 fixture）:

```bash
SB=$(./bin/gopi tool sandbox create --label exch-audit --task-id 01a12bcd7f7477df9f8e96967bf8abad | tail -1)
mkdir -p "$SB/.gopi/sessions"
cp tests/fixtures/log-stats-raw/sessions/fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4.jsonl "$SB/.gopi/sessions/"
GOPI_HOME="$SB" ./bin/gopi session log    fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4 --rounds | head -12
GOPI_HOME="$SB" ./bin/gopi session replay fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4 --rounds | head -6
```

- Result（改后实测输出摘录）:

```
SESSION EXCHANGES — fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4 (9 exchange(s))
  EXCHANGE = exchanges — one JSONL `request` row = one HTTP request/response
  NOT what `gopi session replay <id> --rounds` numbers: that surface's unit is the human rounds
  (operator rows) — the playback steps by the operator's own row, a different quantity from this
  table's exchange count
...
EXCHANGE      RAW  ELAPSED     PROMPT CACHE-HIT COMPLETION  RAW LINES  TOOLS
--- 以及 ---
session fc426fed-...  source=session.jsonl  geometry=80x24 (fallback)  steps=1  rounds=1  exchanges=9
  rounds = human rounds (one human round = one OPERATOR row …)
  exchanges = one JSONL `request` row = one HTTP request/response
   1  round   round 1/1        calls=8 rows=262  prompt="[EPIC GOAL] Solve these 5 items IN ORDER…"
```

即：**同一份 log，log 表 9（exchanges），playback rounds=1 / exchanges=9** —— 旧断言为假，新文本为真。

- Golden 由**生成器**重导，非手改：`GOPI_GOLDEN_UPDATE=1 go test ./internal/gopilog/ -run TestGolden`
  → 仅 `tests/fixtures/log-stats-raw/golden/log-chat.txt:139` 一行变化（diff 见 §Evidence）；
  随后 `go test ./internal/gopilog/ -count=1 -run TestGolden` → ok（门禁绿）。

## Evidence (self-filed)

- A/B 红绿（**红是实测的，不是推断的**）：把两个源文件回退到 `HEAD` 版本、保留新用例 →
  ```
  --- FAIL: TestSessionReplayAndLogNumberDifferentQuantitiesUnderRounds (2.45s)
      cli_session_replay_units_test.go:105: `session log --rounds` still carries the retired sentence
      "still counts `request` rows under `rounds=`", which this fixture's own numbers refute
      (playback rounds=1, table exchanges=9):
  FAIL
  ```
  恢复修复版 → `--- PASS: … (0.82s)`，`ok github.com/nick/gopi/cmd/agent 1.259s`。
- 陈旧断言无门禁覆盖：`grep -rn 'whatever number\|still counts `request`\|where it still' --include=*_test.go .`
  → 0 命中（除无关同形词），故缺口不会被现有测试发现。
- 属主关系实测：`git merge-base master ae48c01ce` = `a3a31da42`；
  `git merge-base --is-ancestor ae48c01ce master` → NO（tip 未 merge 进 master，且 ref 已删）。
- 落地位置：`grep` 命中的 file:line 见 §Completed 表格（每条都回读确认，非 grep 推断）。
- `git status --porcelain`（worktree 内，收尾时为空）：见 §Deliverable。

## Deliverable

- **分支**：`task/01a12bcd7f7477df9f8e96967bf8abad`（本 run 的 worktree，Rule 6a：runner 已建，未另建）。
- **tip**：该分支的 HEAD（`git -C /home/user/work/GoPi log --oneline -1 task/01a12bcd7f7477df9f8e96967bf8abad`
  读取；此处刻意不写死 hash —— 一份在 amend 中写死自身 hash 的报告，下一行就会被自己的提交推翻）。
- **取回命令**：`git -C /home/user/work/GoPi show task/01a12bcd7f7477df9f8e96967bf8abad:devdoc/session-exchange-unit-master-measurement.md`
- **worktree 干净**：`git status --porcelain` → 无输出（提交后实测）。
