<!-- doc-class: normative -->
# devdoc: round 定义归一 —— 撤销 commit c04bcb345 的 EXCHANGE 拆分

**Date**: 2026-10-12 · **Base**: `d62542d6e` · **撤销对象**: `c04bcb345`

## 结论（operator 2026-10-12 裁定）

> round ≡ iteration ≡ 一次 HTTP request/response（一行 JSONL `request`），
> notify 消息和人类输入都算。

所以 **round 是 `request` 行**，一个词一个量。被拆分出去的那个量（operator 自己打的
那行到下一行之间，那个"人类的轮"）**不是 round，是 turn** —— 它改名 `turns`，作为
**邻居量**与 round 并排打印，各用各的名字。

## 缺陷（实测，本机，本 session）

拆分之后，同一个 flag 在两个表面上答两个数：

| 命令 | 改前印 | 数的量 |
|---|---|---|
| `session log <id> --rounds` | `ROUND 1` / 表头 `ROUND` | **operator 行**（human round）|
| `session log <id> --exchanges` | `EXCHANGE 12` | `request` 行 |
| `session replay <id> --rounds` | `rounds=1 exchanges=12` | operator 行 |
| `session stats <id>` | `exchanges 64` | `request` 行 |
| JSONL 实测 | `type=="request"` = 95 行 | —— |
| info bar | `iter12` | REPL turn（第三个量，也叫 iter）|

一份日志、一个词、四个数 —— 拆分没有消灭分歧，只是把它挪了位置。

## 根因

`internal/sessionevent/counters.go` 里 **`CounterUnitRound` 这个名字被指派给了
`request` 行**，而 `CounterUnitHumanRound` 拿走了裸词 ROUND。于是"round"在所有
surface 上指 operator 行，唯独 flag `--rounds` 指的却是 `request` 行 —— **名字与
读者读到的词脱钩**。改名战役的假设是"一个词两个量，拆成两个词就好"，漏掉的是：
**读者读的那个词必须留给正统的那个量**，非正统的那个才是该被改名的。

## 修法（Rule 14：一个值一处定义）

1. **叶子常量** `internal/sessionevent/counters.go`：`CounterUnitRound` = `"rounds"`
   （= `request` 行，正统），`CounterUnitTurn` = `"turns (operator rows)"`（邻居量）。
   `CounterUnitExchange` / `CounterUnitHumanRound` 两枚删掉。注释改成一条**记录**：
   写清撤的是什么、为什么撤、什么情况下会再犯。
2. **所有 surface 引用同一个常量**，`--rounds` / `rounds=` / stats 的 rounds 行 /
   `ITERATIONS` 全部数同一个量。
3. **`--turns`** 是 operator 行的表（`gopilog.FormatSessionTurnTable`），表头 `TURNS`、
   列 `TURN`，右侧一列印该 turn 花掉的 **rounds** 数。
4. **replay 一步 = 一个 round**（`SessionCanonicalRounds`，与 `SessionRounds` 同源），
   读数 `rounds=126  turns=1`。
5. **info bar** 的字从 `iter` 改成 `turn` —— 它数的是 REPL 的 turn（本地逐轮计数器），
   不是 session 的 round；两个量不再共用一个词（`turnLabel` / `statusColTurn`）。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK（0 error）
- Tests: `make test-budget PKGS='./internal/gopilog ./internal/replayplay ./internal/sessionevent ./cmd/agent'`
- Ran: 见下（Rule 31，真跑，非 `-h`）
- Result: 四表面同数同源

```
$ /tmp/gopi-new session log  e89abe60-347d-4c10-8af3-7466ac8c751c --rounds
SESSION ROUNDS — e89abe60-… (126 round(s))
  ROUND = rounds — one JSONL `request` row = one HTTP request/response = one iteration
  ITERATIONS = 126 loop iterations (…)

$ /tmp/gopi-new session replay e89abe60-347d-4c10-8af3-7466ac8c751c --rounds
… steps=126  rounds=126  turns=1

$ /tmp/gopi-new session stats e89abe60-347d-4c10-8af3-7466ac8c751c
│ rounds         127

$ python3 -c '…count type=="request"…'
jsonl request rows = 127

$ /tmp/gopi-new session log e89abe60-… --turns
SESSION TURNS — e89abe60-… (1 turn(s))
  TURN  ROUNDS  PROMPT
     1     126  [EPIC GOAL] …
```

> **2026-10-27 的后续**：上面 `--turns` 那张表的横幅后来改成
> `SESSION HUMAN TURNS — … (N)` —— 裸词 `turns` 现在归 `chats.turn_count`
> （由 `gopi session show <id>` 打印），而这张表数的是**人敲的那部分**。
> 代码块里保留的是当时的原始输出，不改写。

（replay/log 的 126 与 stats/JSONL 的 127 差 1：两次调用之间这个**正在运行的 session**
又写进了一行 `request` —— 这正是"它们数的是同一个量"的证明。）

`--exchanges` 已退休：`gopi session log <id> --exchanges` 现在被拒；`--turns` 接替。
`grep "one row per HUMAN" cmd/agent/cli_log.go` → 无命中。

## Not Done

- **存量 devdoc 仍在讲旧拆分**（`devdoc/round-vs-exchange-naming.md`、
  `devdoc/stats-exchange-naming.md`、`devdoc/session-exchange-unit-master-measurement.md`、
  `devdoc/session-replay-human-round*.md` 等）。这些是**历史记录**，按 Rule 38 是
  narrative 不是 census，本 run 只在其中标注 supersede 指针，不重写它们的结论。
- `man/` 与 `COMMANDS.md` 的生成块已重生成；`COMMANDS.md` 的描述段是手写块，本 run
  同步改了文字。

## 复现（Rule 41 取证）

```bash
# 同一个量，四个表面
S=<session-id>
gopi session log     $S --rounds | head -3
gopi session replay  $S --rounds | head -1
gopi session stats   $S        | grep rounds
python3 -c "import json,sys;print(sum(1 for l in open(sys.argv[1]) if l.strip() and json.loads(l).get('type')=='request'))" <jsonl>
# 邻居量
gopi session log     $S --turns  | head -3
```

四行输出里的数字必须相同（replay 的即 `rounds=`）；`--turns` 那行可以是别的数 ——
它数的是另一个量，并且它自己就这么说。
