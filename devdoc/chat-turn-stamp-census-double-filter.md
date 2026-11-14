<!-- doc-class: normative -->
# `TestChatCookedFallback_NonTTY` 的「0 stamps」：fixture 双重过滤，不是产品缺陷

**结论（一句话）**：在改动前的基线 worktree（`96a4320f2`）上，该 fixture 报
`harness turn stamps = 0, model requests = 2` **不是产品不再写戳**——戳确实写进了
`session.jsonl`（真二进制实测：两轮跑写 4 条 user 行，其中 2 条 `[system] [t+0s] …`）；
缺陷在 **fixture 的读取路径把已经过滤过的视图又过滤了一遍**：`ea1357381` 之后
`testutil.Session.UserContents()` 本身就已经是「只看 operator」的视图
（`internal/testutil/session.go:442`，用 `ev.IsHarnessUser()` 过滤，而
`IsHarnessUser` 走的正是生产谓词 `sessionevent.IsHarnessInjected`，`internal/testutil/session.go:61`），
于是 `for _, u := range sess.UserContents() { if IsHarnessInjected(u) … }`
**永远不可能**往 `injected` 里放元素——0 是读取路径造出来的，不是记录里的。

判据分界：下面凡是标 **[实测]** 的都带命令与原始输出（证据目录
`~/.gopi/sessions/01a12c77-e3bb-736e-b248-b27045ddf7bf/artifacts/`）；标 **[推断]** 的
是从实测推出、但没有单独实验直接观测的环节。

---

## 1. 戳到底有没有被写进 JSONL —— 真二进制探针 [实测]

用**基线 `96a4320f2` 自己编出的二进制**（`git works-tree`=detached `/home/user/workspace/gopi-base-01a12c77`，
产物 `artifacts/gopi-base`）跑一次真 `gopi chat`，stdin 是管道（非 TTY，正是该测试的形态），
指向本地 mock LLM；HOME/GOPI_HOME 隔离到 `artifacts/probe-home-base/`：

```
printf 'hello there\nwhat is two plus two\n/quit\n' | env -i … HOME=$H GOPI_HOME=$H … $BIN chat
```

`artifacts/31-stamp-probe-base.out`（基线二进制）与 `artifacts/30-stamp-probe.out`（本树二进制）
**输出逐字相同**：

```
FILE: …/probe-home-base/.gopi/sessions/b8deab18-…/session.jsonl
    1 type='session_start' content=''
    2 type='user' content='hello there'
    3 type='user' content='[system] [t+0s] — first request of this session.'
    4 type='assistant' content='first piped answer.'
    5 type='request' content=''
    6 type='user' content='what is two plus two'
    7 type='user' content='[system] [t+0s] — +0s since your previous request.'
    8 type='assistant' content='second piped answer.'
    9 type='request' content=''
   10 type='session_end' content=''
```

→ user 行共 4 条 = 2 条 operator + 2 条 harness 戳；`request` 行 2 条 → **一 request 一戳**，
与 `internal/agent/clock.go` 的 `turnStampLine` 契约一致。**基线二进制写了戳。**

## 2. 产品代码与读谓词在基线/本树之间字节相同 [实测]

```
git diff --stat 96a4320f2 HEAD -- internal/agent/clock.go internal/testutil/session.go internal/sessionevent/harness.go
（无输出）
git merge-base --is-ancestor ea1357381 96a4320f2   → 是（过滤版 UserContents 在基线里就有）
```

唯一被 7ae446e47 改的东西是 `cmd/agent/cli_lineedit_test.go` 本身（`git show --stat 7ae446e47`）。

## 3. 单一变量 A/B（锚点：`7ae446e47` 的单行改法）[实测]

- 把这一行改法打到 **pristine 基线 worktree**（`/home/user/workspace/gopi-base-01a12c77`，
  detached `96a4320f2`）→ `--- PASS: TestChatCookedFallback_NonTTY`（`artifacts/21-base-green-after-fixture-fix.out`）。
- 在本树把该行**还原**成 `sess.UserContents()` → 同一条消息复红
  （`artifacts/61-red-proof-newarm.out` + `61-red-proof-newarm-raw.txt`）：

```
cli_lineedit_test.go:121: harness turn stamps = 0, model requests = 2 — want exactly one stamp per request (stamps: [])
cli_lineedit_test.go:124: the run wrote no turn stamp at all — every request carries one (internal/agent/clock.go)
```

产品代码全程未动 ⇒ 变的只有那一行 ⇒ 缺陷位置确定。

## 4. 基线红原文（判定所依据的第一手现象）[实测]

`artifacts/20-base-red.out`（基线 worktree，`-run TestChatCookedFallback_NonTTY`，exit=1）：

```
--- FAIL: TestChatCookedFallback_NonTTY (0.06s)
    cli_lineedit_test.go:113: harness turn stamps = 0, model requests = 2 — want exactly one stamp per request (stamps: [])
    cli_lineedit_test.go:116: the run wrote no turn stamp at all — every request carries one (internal/agent/clock.go)
```

## 5. 修复后该 fixture 读到的行（同一记录内自洽）[实测]

`session_accounting_test.go:TestSessionFixtureCountsOperatorTurnsNotHarnessRows` 在同一
fixture 上把三个视图的语义钉死：`UserContents()=1`（operator）、`HarnessUserContents()=1`、
`AllUserContents()=2`，并且 `Counts()[user]=2` 仍是 **raw** 计数。

## 6. 新增的自检断言（本次交付）与它的红/绿证明 [实测]

原来的两条断言**在 census 丢行时也会以同样文字失败**（0 stamps 是"运行没写"和"读数方式错"的
共同输出），所以本次给 fixture 补一条**身份自检**：raw 计数必须等于 operator+stamps。
`cmd/agent/cli_lineedit_test.go`：

```go
if raw := sess.Counts()["user"]; raw != len(operator)+len(injected) {
        t.Errorf("the census read %d user row(s) (operator %d + stamps %d) but the record holds %d — a view that sees fewer rows than the record is a second filter, not a measurement; use AllUserContents (internal/testutil/session.go)", …)
}
```

- **红证**（census 还原成 `UserContents()`）：`artifacts/61-red-proof-newarm-raw.txt`

```
cli_lineedit_test.go:136: the census read 2 user row(s) (operator 2 + stamps 0) but the record holds 4 — a view that sees fewer rows than the record is a second filter, not a measurement; use AllUserContents
```

- **绿证**（census 用 `AllUserContents()`）：`artifacts/62-restored-green.out` → `exit=0 outcome=pass`。

## 7. pin 它的产品测试/常量（Rule 14：不许镜像）[实测]

- `internal/agent/chat_turn_clock_test.go`（`TestChatTurnClock_*`）与
  `internal/agent/sessionbudget_everyround_test.go`：在 wire 上钉"一 request 一戳"。
  `scripts/test-budget.sh -run 'TestChatTurnClock|EveryRound|SessionBudget' ./internal/agent/ ./cmd/agent/`
  → `exit=0 outcome=pass`（`artifacts/42-pins-rerun.out`）。
- 唯一 mint：`internal/agent/clock.go` 的 `turnStampLine`；前缀常量
  `sessionevent.HarnessMessagePrefix`，谓词 `sessionevent.IsHarnessInjected`
  （`internal/sessionevent/harness.go:32`）。
- 视图语义由 `internal/testutil/session_accounting_test.go` 钉死（§5），fixture 侧**不许**
  自己实现过滤规则（Rule 14）。

## 8. 附带解决的一个旧疑点 [实测]

早前报告里"同一个 fixture 在 `:50/:53/:60` 行号上红、断言文字却不同"的未解异常，
实测就是 **A4 之前的旧形状 fixture**（`AssertCounts{"user":2}` + 未过滤的 `UserContents()`），
本机 `master a3a31da42` 与 `refs/remotes/peer1/master ef8b69fd9` 携带的是同一份文本。
逐字原文（从 `44-peer-master-testsuite.out` / `45-peer-master-cmdagent.out` 的 raw stream
提出，`artifacts/46-peer-master-raw-assertions.txt`）：

```
chat_test.go:53 / cli_lineedit_test.go:53: session: user events = 4, want 2
      (types: [session_start user user assistant request user user assistant request session_end])
chat_test.go:61 / cli_lineedit_test.go:60: user messages = [hello there [system] [t+0s] — first request
      of this session. what is two plus two [system] [t+0s] — +0s since your previous request.],
      want the two piped turns
```

→ 那是**同族问题的旧红**（未过滤视图 + `"user":2` 对着 4 条 raw user 行），不是本 fixture
的第二次失败，也不是 flake。注意 `types: [session_start user user assistant request …]` 这一行
本身就是"戳写进去了"的第二份独立证据。

## 9. 未处置的观察（已按 Rule 41 提行，不在本分支修）[实测]

`refs/remotes/peer1/master`（peer upstream, `ef8b69fd9`）**两个 chat fixture 都红**：
它带 writer（`internal/agent/clock.go` 有 `turnStampLine`）但**不带 reader 过滤**
（`internal/testutil/session.go` 无 `IsHarnessUser`/`HarnessUserContents`/`AllUserContents`），
所以 `AssertCounts{"user":2}` 与 `UserContents() len==2` 数到 4 行：

- `artifacts/44-peer-master-testsuite.out` → `TestChat_HappyPath_TwoTurnsSameSession` exit=1
- `artifacts/45-peer-master-cmdagent.out` → `TestChatCookedFallback_NonTTY` exit=1

两份输出都由 `git archive refs/remotes/peer1/master | tar -x -C <session-dir>/artifacts/master-tree-ef8b69fd9`
materialise 出来的树里跑出（**不是 worktree**，Rule 29），故与本 worktree 无所有权冲突。
两份旧形状 fixture 文本在 `refs/remotes/peer1/master` 与本机 `master a3a31da42` 上**逐字节相同**
（`md5sum` 均为 `520c4dc480fee850dd78cf6669dffcf2`，实测）。
**[推断]** 这是"修复只在本地 gitPushcheck lineage、尚未上行到 peer master"的
未同步状态（`ea1357381` 与 `7ae446e47` 都不在 `peer1/master` 里），不是新的产品缺陷；
是否需要上行由 PA/operator 决定 —— 该行以实测证据提交，判定留给裁决方。
