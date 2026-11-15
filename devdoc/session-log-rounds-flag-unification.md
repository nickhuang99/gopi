<!-- doc-class: normative -->
# devdoc: `session log --rounds` 与 `session replay --rounds` 同名义 —— 同名 flag 统一到同一单位

> **ORTHODOXY RESTORED 2026-10-12 — this document's naming decision is REVOKED.**
> The operator's ruling: **round ≡ iteration ≡ one HTTP request/response (one JSONL
> `request` row)**, counting notify messages and human input alike. That is what the
> word ROUND names on every surface again; the operator-row quantity (what this
> document calls the human round) is the neighbouring quantity **TURNS**
> (`sessionevent.CounterUnitTurn`, `gopi session log <id> --turns`), and the
> `request`-row unit's own word went back from `exchange` to `round`
> (`sessionevent.CounterUnitRound`, `--rounds`). Read
> `devdoc/round-definition-restored.md` before implementing anything from the text
> below — the reasoning in this file is the HISTORY of the split, kept as a record
> of the path walked, not a description of master.


**Date**: 2026-10-12 · **Task**: `01a12f83-16b0-75f0-b2f8-61a0b691a4da`
· **Branch**: `task/01a12f8316b075f0b2f861a0b691a4da` · **Base**: `b12e9c743`

## 缺陷

同一个 flag 拼写 `--rounds`，在两个表面上数**两个不同的量**：

| 表面 | 改前印的字 | 数的量 |
|---|---|---|
| `session log <id> --rounds` | `SESSION EXCHANGES` / 列 `EXCHANGE`（18 行里另有两条免责声明）| **exchange**（一条 JSONL `request` 行 = 一次 HTTP 请求/响应）|
| `session replay <id> --rounds` | `rounds=` | **human round**（operator 行 —— 人打的那条消息到下一条）|

上一轮战役（`devdoc/stats-exchange-naming.md`、`devdoc/round-vs-exchange-naming.md`）
只把**表头的词**改对了，flag 没动，于是 `log` 里留下两段解释性文字：
`NOT the same number as N loop iterations` 与
`NOT what \`gopi session replay <id> --rounds\` numbers: that surface's unit is the human rounds`。

operator 定的原则把这半截判为缺陷：**`replay` 是 `log` 的升级版，同名 flag 必须语义相同。**
一段"我数的不是那个同名 flag 数的东西"的说明，本身就是"名字错了"的证据 ——
正确的修法不是解释，是取对名字。

## 修法

1. **`log --rounds` 改为 human round 表**（`internal/gopilog.FormatSessionHumanRoundTable`）：
   一行一个 human round，列为 `ROUND / EXCHANGES / PROMPT`。它的边界来自
   `SessionHumanRoundBounds` —— **`SessionHumanRounds` 与 replay 的 `RoundCount` 用的是
   同一个生产者**，所以 `log --rounds` 表头里的 `(N round(s))` 与
   `replay --rounds` 打印的 `rounds=N` **必须相等**（Rule 14：一个事实一个来源）。
2. **exchange 表挪到 `--exchanges`**（`logFlagExchanges = gopilog.SessionExchangeFlagName`，
   值来自常量包，Rule 1/13/14）：表本身没变，名字换成它所数的量自己的词。
3. **删掉两段免责声明**：`Iterations` 一行仍在（它是独立命名），但不再写
   "NOT the same number as"；`NOT what \`gopi session replay …\`` 整段删除 ——
   同名 flag 现在真的同义，没有需要解释的落差。
4. **交叉引用同步**：`logUsage` 的 `--rounds`/`--exchanges` 段、`<round>` 定位参数的拒绝文案
   （"is not an exchange"）、`--rounds --all` / `--rounds --exchanges` 的拒绝、
   `COMMANDS.md`、`devdoc/gopi-session-help-surface-inventory.md`（87 → 88 行）。

## 实测（同一条会话上三个表面的 verbatim 输出）

会话 `0945c8bd-cf5a-4dfc-b630-670266599f81`（jsonl 内 169 条 `request` 行）：

```
$ ./bin/gopi session log 0945c8bd-...-670266599f81 --rounds
SESSION ROUNDS — 0945c8bd-cf5a-4dfc-b630-670266599f81 (10 round(s))
  ROUNDS = human rounds (operator rows) — one human round = one OPERATOR row …
  EXCHANGES = exchanges — one JSONL `request` row = one HTTP request/response (the count `--exchanges` tabulates)
  ITERATIONS = 169 loop iterations …
  ROUND  EXCHANGES  PROMPT
      1          2  read /home/user/.gopi/sessions/5af45c3e-…

$ ./bin/gopi session replay 0945c8bd-...-670266599f81 --rounds
…  steps=10  rounds=10  exchanges=169

$ ./bin/gopi session log 0945c8bd-...-670266599f81 --exchanges
SESSION EXCHANGES — … (169 exchange(s))
… 169 行 exchange 表
```

**`rounds` 10 == 10（同名 flag 同义）**，`exchanges` 169 == 169（`--exchanges` 表 == replay 报的
exchange 数）。改前同一条会话：`log --rounds` 数 169 而 `replay --rounds` 报 `rounds=10`。

## 根因（5-why）

1. 两个表面同名不同义 → 因为 `log --rounds` 打的是 exchange 表；
2. 为什么打 exchange 表 → 因为建表时 exchange 是那个会话唯一的"轮"（canonical round 时代），
   表头就用了 `ROUND`；
3. 为什么后来没跟着改 → 因为 human round 被引入 replay 之后，`log` 侧只改了**表头的词**
   （`SESSION EXCHANGES`/列 `EXCHANGE`），**flag 被视为"CLI 语法，不是量的名字"**而保留
   （见 `devdoc/stats-exchange-naming.md`：「缺陷在量的名字，不在 `--rounds` 这个 flag」）；
4. 为什么那句"flag 不是量的名字"在这里不成立 → 因为那条推理的前提是"两个 flag 各自独立"，
   而 operator 的原则是 `replay` **是 `log` 的升级版**：同一个拼写在两个表面上是同一个承诺，
   不是两段互不相干的语法；
5. 根因：**把"不可改的 CLI 语法"当成了不变量，而没有检查这个拼写是否在别处已经承诺了另一个语义。**
   判据应该是"这个拼写在所有表面上是否同义"，不是"改它会不会打断脚本"。

## 陷阱

- **`--rounds` 的位置参数 `<round>` 数的是 exchange**，不是 human round（`gopi session log <id> 3`
  读第 3 次 API 调用），所以它的拒绝文案、`--toolcall` 的交叉引用都指向 `--exchanges`，
  **不能**跟着改名成 `--rounds`。改名只覆盖 flag，不覆盖 positional —— 这也是为什么
  exchange 表必须有一个自己的表名（`--exchanges` 不只是旧行为的容器，它是 `<round>` 的正确指路牌）。
- **`session stats --rounds` 不受影响**：它同一轮战役里已改成 `PER-EXCHANGE` / `EXCHANGE`
  （`devdoc/stats-exchange-naming.md`），本 run 实测确认它仍印 `PER-EXCHANGE`。
- 新表头行 `EXCHANGES = exchanges — … (the count `--exchanges` tabulates)` 里的 flag 拼写来自
  `gopilog.SessionExchangeFlagName`，不是字面量 —— 否则下一次改名会留下一次说谎的表头。

## 验证

见 `task-report.md` 的 `## Verification`（build / branch-scoped test / `Ran:` 三面实测 / commit）。
