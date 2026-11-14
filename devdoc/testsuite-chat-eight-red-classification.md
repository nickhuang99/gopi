<!-- doc-class: historical -->

# Task Report: testsuite 的 8 条 chat 用例为何在 master 基线上红 —— 逐条定性

任务行 `01a1645c-23fa-7845-81e3-02487e14b8a0`。
测量基点：master `c9c053465eade66bb80e59872c85409fb300ba81`（本 run 的 ground 就是 master 之上，无自有提交）。
`bin/gopi` 由本 run 重建，内嵌 revision == HEAD（见下 Verification）。
本文所有结论都来自本 run 自产的实测，未引用任何他人的测量（Rule 41）。

## 一句话结论

8 条里有 **6 条是产品侧真缺陷**（其中 1 条需规格裁决），**3 条测试侧陈旧/脆弱**。
三条产品缺陷共享同一个根因：**chat-policy 的默认规则表是 `prompt *`，而 chat 面没有审批通道**，
于是 `gopi chat --tools` 的每一个工具调用都被挡在"等操作员批准"上，永远不执行。
按 Rule 41 已就该根因与三条独立缺陷各提一条 bug 行（见 §4）。

---

## 1. 测量方法（可复制粘贴）

```bash
cd <ground>
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi --version          # revision 必须 == git rev-parse HEAD
git rev-parse HEAD            # c9c053465eade66bb80e59872c85409fb300ba81

# 一次性批量（8 条中 7 条；Emphasis 含两个子用例）
GOPI_BIN=$PWD/bin/gopi go test ./testsuite/ -count=1 -v -timeout 900s -run \
 'TestChatPTY_EscapeKillsARunningBashCommand|TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal|TestChatEmphasisColourAlignment_OnRealPTY|TestChatGround_NoBranchNoWorktreeAndTheWriteLandsWhereTheSessionStands|TestChatInfo_OnRealPTY_IdentityMatchesDiskAndTheCommandRuns|TestChatLayerFormat_OnRealPTY|TestChatOnce_ToolsTurnReallyCallsATool'

# 逐条单跑（排除并行竞争）
for T in <each test name>; do
  GOPI_BIN=$PWD/bin/gopi go test ./testsuite/ -count=1 -v -timeout 400s -run "^$T$"
done
```

原始输出（本 run 的 session 目录，Rule 37：证据不进 worktree）：

- `~/.gopi/sessions/01a1645c-23fa-7845-81e3-02487e14b8a0/run-batch1-master-c9c0534.txt` —— 批量，7 条
- `…/run-batch2-master-c9c0534.txt` —— Emphasis 重跑 + palette 帮助用例
- `…/run-batch3-solo-master-c9c0534.txt` —— 单跑复核 5 条
- `…/probe-policy-refusal.txt` —— 策略裁决的直接探针输出

**单跑复核结果（决定"确定性 red"还是"并行竞争"）**：Info、LayerFormat、Cut、Once、Escape
**单跑同样红**，失败消息与批量完全一致；只有 Emphasis 的 `delta=4` 绿、`delta=1` 红（两次一致）。
→ 这些不是资源竞争，是确定性失败。

---

## 2. 逐条分类

| # | 用例 | 判定 | 一句话根因 |
|---|---|---|---|
| 1 | `TestChatPTY_EscapeKillsARunningBashCommand` | **产品缺陷** | 工具轮被 chat-policy 挡住，命令从未启动 |
| 2 | `TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal` | **测试脆弱** | 答案带选择器把一条系统通告收进 band |
| 3 | `TestChatOnce_ToolsTurnReallyCallsATool` | **测试陈旧断言**（+ 产品缺陷叠加） | 断言 `⚙ result read`，产品画 `↳ result read` |
| 4 | `TestChatInfo_OnRealPTY_…` | **测试脆弱** | 前置 turn 的等待条件依赖一个产品不保证的 token |
| 5 | `TestChatGround_NoBranchNoWorktreeAnd…` | **产品缺陷** | write 工具被 chat-policy 挡住，文件未写 |
| 6 | `TestChatLayerFormat_OnRealPTY` | **规格冲突（需裁决）** | fenced 行归 `LayerCodeTool` 且无 gutter，用例在 response 组里找 |
| 7 | `TestChatEmphasisColourAlignment/delta=4` | **绿** | —（19.35s / 19.34s 两次） |
| 8 | `TestChatEmphasisColourAlignment/delta=1` | **测试脆弱** | settle 超时被放大成"chat 未退出"（98s） |

---

## 3. 逐条证据

### 3.1 根因 A —— chat-policy 默认 `prompt *` 且无审批通道（#1、#5，并叠加在 #3 上）

产品侧两个事实，各自有本次实测：

```
$ go test ./internal/chatpolicy/ -run TestZZZProbeFence -v      # 探针，用后已删除
    DEFAULT-RULES-TEXT = "```chat-policy\nprompt *\n```\n"
    DEFAULTS [echo hi]      -> prompt (rule {"prompt" ["*"] "built-in chat-policy defaults"})
    DEFAULTS [sleep 120]    -> prompt (rule {"prompt" ["*"] "built-in chat-policy defaults"})
    ALLOW-FENCE [echo hi]   -> allow  (rule {"allow" ["*"] "operator-fence"})
```

```
$ go test ./internal/tools/ -run TestZZZProbeWrite -v            # 探针，见 probe-policy-refusal.txt
    WRITE "chat-ground-written.txt" -> "[NEEDS OPERATOR APPROVAL — command NOT run] … The file was NOT written."
    BASH  "echo hi"                 -> "[NEEDS OPERATOR APPROVAL — command NOT run] … The command did NOT run."
```

来源：`internal/chatpolicy/defaults.go:25-27`（`prompt *`，blame `1de01bf6 2026-10-19`），
消费点 `internal/tools/chat_write_guard.go:177`、`internal/agent/chat_write_guard.go:70`。

**为什么这是缺陷而不是设计**：该包自己的头注释说"内置表与默认姿态给同一个答案"，
而 `prompt` 在**没有审批通道**的界面上等于"永不执行"。tools 模式于是默认不可用。

失败的用例与消息（单跑，确定性）：

```
$ … -run '^TestChatOnce_ToolsTurnReallyCallsATool$'
--- FAIL: … (0.03s)
    chat_once_test.go:208: stderr does not report the tool result

$ … -run '^TestChatPTY_EscapeKillsARunningBashCommand$'
--- FAIL: … (26.36s)
    chat_bash_escape_interrupt_pty_test.go:202: the bash command never published its pid to
      …/002/bash-escape.pid — the tool round did not start a command

$ … -run '^TestChatGround_NoBranchNoWorktreeAndTheWriteLandsWhereTheSessionStands$'
--- FAIL: … (0.05s)
    chat_ground_test.go:167: the turn's write is not in the caller's directory
      …/002/chat-ground-main: open …/chat-ground-written.txt: no such file or directory
```

#3 的 stderr 还带着产品**实际画出来的**形状，这也是 3.2 的证据：

```
you> ⚙ call read …/002/chat-once-probe.txt
↳ result read chat-once-tool-probe-content
```

### 3.2 #3 的第二重红 —— 陈旧断言（独立缺陷）

`testsuite/chat_once_test.go:207` 断言 stderr 含 `⚙ result read`。
产品自 `14e05f8`（2026-10-19 "the tool-activity row is ONE pure renderer"）起改画 `↳ `：

```
$ grep -n 'NoteContinuation' cmd/agent/term_tool_row.go
205:			{Text: lineedit.NoteContinuation, Style: st.ResultGlyph},
$ grep -n 'NoteContinuation =' internal/lineedit/answer.go
120:	NoteContinuation = "↳ "
$ git log --oneline -3 -L 205,210:testsuite/chat_once_test.go
964b240 initial: source snapshot at 0592c9ae4 …      ← 断言来自快照，此后未动
$ git log --oneline -1 -- cmd/agent/term_tool_row.go
14e05f8 chat: the tool-activity row is ONE pure renderer …
$ git merge-base --is-ancestor 964b240 14e05f8 && echo yes
yes
```

→ 产品的新形状是有意的（copy contract）。**这一条即使修好根因 A 仍会红。**

### 3.3 #2 —— 答案带选择器把系统通告算进答案

```
$ … -run '^TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal$'
--- FAIL: … (13.18s)
    chat_cut_pty_test.go:231: the band does not hold the answer whole:
      got 515 characters, want the token's 400 — a marked row must hand its last
      character to the row below, losing nothing; band rows:
        (band row 0)                        ……[gopi] chat: slot 0 registered in /dev/shm/su
        (band row 1..5)  token 的各个片段 + -cut-end-
```

**算术闭合**：`515 − 400 = 115`，而 band row 0（那条槽位通告）的长度正是 115：

```
row 0: len=115   ← 通告行
row 1: len=115   row 2: len=115   row 3: len=115   row 4: len=57   row 5: len=9
sum = 526 ; 用例报告 got=515 ; row 0 之外 = 411 ; token = 400
```

**机制**：`chat_cut_pty_test.go:135-157` 用"每行前两格是 `▌ ` 或 `  `"挑答案行。
那条通告带 70 个前导空格（同批实测 `leading_spaces=70`），前两格恰是 `  `，
和"答案换行继续行"的 lead 无法区分 → 被收进 band → 断言 (3) 多算 115 字符。

→ **测试侧脆弱**：lead 已不再是答案带的唯一特征。产品无缺陷。

### 3.4 #4 —— Info 用例的前置 turn 等待条件不可靠

```
$ … -run '^TestChatInfo_OnRealPTY_IdentityMatchesDiskAndTheCommandRuns$'
--- FAIL: … (33.20s)
    chat_info_pty_test.go:169: the screen never carried the /info block within 20s; stream so far:
        you> /  you> /  ……  [表菜单反复出现]  ……  （全程没有 TURN-OK，也没有 session dir）
```

用例先跑一个真实 turn 并 `chat.waitFor(t, chatInfoPTYAnswer /* "TURN-OK" */, ptyPromptTimeout)`
（`chat_info_pty_test.go:165-167`），再打 `/info`。

**本 run 的同机最小复现（真实 pty + 真实二进制 + 本地 mock 端点）**：

| 场景 | `/info` 块 | 说明 |
|---|---|---|
| 冷启动直接 `/info`，gap 300ms/字 | **渲染** | `session dir` 出现 |
| 冷启动直接 `/info`，gap 100ms/字 | **渲染** | 同上 |
| 一次性写入 `/info`（burst） | **渲染** | 无重复菜单 |
| 逐字 gap 300ms，**无前置 turn** | **渲染**，但菜单重复 3 次 | — |
| 先跑一个 turn（端点返回固定文本）再 `/info` | **渲染** | — |

→ 在本 run 的手工复现里 **`/info` 从未失败**，而失败消息显示前置 turn 的 `TURN-OK`
根本没出现在 33s 的流里：**红点在"前置 turn 的完成判定"，不在 `/info`。**

**推断（标明是推断）**：`waitFor("TURN-OK")` 依赖 mock 的文本真的以该 token 出现；
一旦前置 turn 因任何原因没走完（或文本未原样落到流里），用例会带着一个未完成的 turn
直接打 `/info`，此时按键被当成新一行的输入、菜单反复重绘 —— 流里那串重复的
`you> /> /log (Usage…)` 就是这个形状。要把它判成产品缺陷，需要先抓到"turn 卡住"的直接证据；
本 run 没有抓到，因此按测试脆弱归档，并给出了可执行的稳定化方向。

**稳定化方向**：前置 turn 的完成信号不要绑在一个**答案文本**上（那是 mock 的措辞，不是产品的契约），
改绑在产品的结构信号上 —— 状态行的 `turn1`／`ready`，或 `chat-screen.dump` 的 frames/dumps 计数稳定；
并把"turn 未完成就继续"变成立即失败的显式断言，而不是让后续步骤在错误的前提上跑 33s。

### 3.5 #6 —— fenced 行的层归属：规格冲突

```
$ … -run '^TestChatLayerFormat_OnRealPTY$'
--- FAIL: … (13.60s)
    chat_layer_pty_test.go:254: no response-layer row carries the fenced code row
```

同一次运行的屏幕证据（用例自己打印的产品 dump）：

```
LAYER-EVIDENCE layer 0 marker="▸ " rows=[11]
LAYER-EVIDENCE layer 1 marker="· " rows=[]
LAYER-EVIDENCE layer 2 marker="▌ " rows=[13 17]
LAYER-EVIDENCE layer 3 marker="⚙ " rows=[12]
LAYER-EVIDENCE row 013|▌ the answer body line
LAYER-EVIDENCE row 014|```
LAYER-EVIDENCE row 015|code row for the family      ← 无 gutter
LAYER-EVIDENCE row 016|```
LAYER-EVIDENCE row 017|▌ LAYER-EVIDENCE-END
```

产品侧两处自洽的契约：
- `internal/lineedit/layers.go:97-103`：`LineCode`/`LineDiffAdd`/`LineDiffDel` → `LayerCodeTool`；
- `internal/lineedit/layers_test.go:228-240`：fenced 行**以 NO GUTTER 开在 column 0**
  （"measured 2026-10-02"，复制契约：`rowText(codeRow)` 必须等于命令原文）。

用例侧 `chat_layer_pty_test.go:243-254` 却在 `LayerResponse` 组里找这一行。

→ **两侧都自认为对**。这需要一次判据裁决（改用例，还是改复制契约），
不适合由"让测试变绿"的动作静默决定。已按 Rule 41 提行。

### 3.6 #8 —— Emphasis 的 delta=1：settle 超时被放大成"未退出"

```
--- PASS: TestChatEmphasisColourAlignment_OnRealPTY/delta=4 (19.34s)
--- FAIL: TestChatEmphasisColourAlignment_OnRealPTY/delta=1 (98.34s)
    :266: ALIGN the artifact did not settle within 20s; falling back to the pty's
          re-rendered prompt as the end-of-turn signal — the verdicts are then read
          from whatever the artifact holds
    :266: chat did not exit within 1m0s
```

两次运行（batch1 / batch2）耗时 98.34s / 98.29s，**完全一致** → 确定性，不是抖动。

**时间账（与实测吻合）**：`alArtifactGrace = 20s` + `ptyQuitTimeout = 60s` +
`writeGapped("/quit")` 6 字符 × `ptyTypeGap 300ms` ≈ 1.8s ≈ **82–98s**。
真正的失败点是 **settle 等待**（`alWaitTailPainted` 要求 `dumps` 计数器连续两次相同），
"chat did not exit" 只是它的下游后果 —— 报错误导。
`delta=1` 意味着 ~301 字节的答案分 301 帧、每帧 15ms，刷新极密，20s 的固定 grace 不够。

→ **测试侧时序脆弱**，颜色对齐断言本身没有被判红过。

---

## 4. 已提交的 bug 行（Rule 41）

| 行 id | 内容 |
|---|---|
| `01a16885-6754-7a8b-b2b3-5cbfe15305b0` | chat-policy 默认 `prompt *` + 无审批通道 → chat --tools 每个工具调用都被挡 |
| `01a16885-676d-7809-abda-e7c093431c54` | testsuite 断言 `⚙ result read`，产品画 `↳ result read` |
| `01a16885-8a83-7e1c-8708-937d12073b93` | chat_cut 的 band 选择器把 70 空格前导的槽位通告算进答案（515−400=115） |
| `01a16885-8a99-74b7-84cc-76d5d748ad57` | emphasis-align 的 delta=1：settle 超时被放大成"chat 未退出" |
| `01a1688b-7ac9-7d76-a558-b227dbf63e3b` | fenced 行层归属：`LayerCodeTool`/无 gutter vs 用例在 response 组找（规格冲突） |

（前四条 `parent_task_id=independent`，各自可立即认领；第五条因未显式传而链在
`01a16885-8a99…` 之后 —— 这是一次提交纪律上的疏漏，记录在此，不追改。）

## 5. 一并发现、未提行的观察（标为观察，非结论）

`TestChatUsageDocumentsThePaletteKnob`（不在本行 8 条之内，本 run 顺带跑到）稳定红：

```
chat_palette_e2e_test.go:429: `gopi chat -h` does not mention "hermes" / "hermes-light" / "COLORTERM" / "truecolor"
chat_palette_e2e_test.go:433: `gopi chat -h` does not name the answer body's colour (#FFF8DC)
```

实测 `./bin/gopi chat -h` 的输出里 `GOPI_PALETTE` **确实出现**（"--no-color" 一节），
但 `hermes` / `hermes-light` / `COLORTERM` / `truecolor` / `#FFF8DC` 一个都没有
（`grep -icE 'hermes|colorterm|truecolor|FFF8DC'` = **0**）。这些常量在产品里存在
（`internal/lineedit/palette.go:722,724`、`colour.go:127,133`），只是没写进帮助文本。
→ 与 #3 同族：**帮助文本落后于常量**。这属于"文档与产品脱钩"，不是本行 8 条之一，
本 run 只记录，不替它决定归属。

---

## Not Done（诚实）

- **没有修任何一条**。本行的交付是定性 + 证据 + 派发，不是修复（briefing 明确"不要求本行自己修完"）。
- #4（Info）**没有抓到"前置 turn 卡住"的直接证据**：手工最小复现在 5 种场景下 `/info` 全部渲染成功，
  所以只能归到"等待条件绑在答案文本上"这一层。要升级为产品缺陷，需要一条能稳定复现
  `TURN-OK` 不出现的路径 —— 本 run 未找到，按 Rule 41 的纪律不臆断。
- #6（LayerFormat）**故意没给结论**：两侧都有书面契约与实测依据，判据层面的选择留给拿到该行的人。
- 没有跑全量 testsuite（Rule 27 的分工：merge/verify 路径是 branch-scoped smoke，
  不是整棵树）；本 run 只跑这 8 条 + 2 条顺带相关的用例。
- `TestChatEmphasisColourAlignment/delta=4` 判定为绿，但没有做"它是否测到了它声称的东西"的独立复核。

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → 退出码 0
- **Revision 对齐**: `./bin/gopi --version` 输出 `revision: c9c053465eade66bb80e59872c85409fb300ba81`，
  与 `git rev-parse HEAD` 相同（`revision: c9c0534…ba81` == `c9c053465eade66bb80e59872c85409fb300ba81`）
- **Tests（本 run 实际跑的）**:
  - 批量 7 条：`go test ./testsuite/ -count=1 -v -timeout 900s -run '<7 names>'`
    → `FAIL github.com/nick/gopi/testsuite 205.366s`，7 条全红
  - 单跑 5 条：`-run '^<name>$'` 逐条 → 全部 `EXIT=1`，失败消息与批量一致
  - Emphasis 重跑：`delta=4 PASS (19.34s)` / `delta=1 FAIL (98.29s)`
  - 策略探针：`go test ./internal/chatpolicy/ -run TestZZZProbeFence -v` → 见 §3.1
  - 守卫探针：`go test ./internal/tools/ -run TestZZZProbeWrite -v` → 存档 `probe-policy-refusal.txt`
- **Ran（最小复现，真实 pty + 真实二进制 + 本地 mock 端点）**:
  - `/tmp/pty_info3.py http://127.0.0.1:18931` → `/info` 块渲染成功（先跑一轮）
  - `/tmp/pty_burst.py … burst` / `… gapped` → 两种输入节奏下 `/info` 均渲染（cold）
  - `/tmp/pty_echo.py … cold` → `echo contains '/info': True`，菜单在输入 `/` 后立即出现
  - `/tmp/mockllm.py 18931 "answer with the token"` → `curl … -w %{http_code}` = `200`
- **Result**: 8 条定性完成；5 条 bug 行已入队（id 见 §4）；本 run 未修改任何产品源码。
- **Commit**: 本报告随本 run 的 ground 分支提交（见 `git log --oneline -1`）。

## Evidence（self-filed）

- 基线 commit：`git rev-parse HEAD` → `c9c053465eade66bb80e59872c85409fb300ba81`
- 二进制 revision：`./bin/gopi --version` → `revision: c9c053465eade66bb80e59872c85409fb300ba81`（与上面同一串）
- 批量红：`run-batch1-master-c9c0534.txt` 内 `grep -E '^--- (PASS|FAIL)'` → 7 行全 `FAIL`
- 确定性：`run-batch3-solo-master-c9c0534.txt` 内 5 条单跑 `EXIT=1`，消息与批量一致
- 策略默认表：`TestZZZProbeFence` 打印 `DEFAULT-RULES-TEXT = "```chat-policy\nprompt *\n```\n"`
- 写守卫：`TestZZZProbeWrite` 对 `write`/`bash` 均返回 `[NEEDS OPERATOR APPROVAL — command NOT run]`
- Cut 算术：band row 长度 115/115/115/115/57/9，sum=526，row0 之外=411，用例报告 got=515 → 515−400=115=row0
- Info 不可复现：手工 5 种场景 `/info` 块均渲染；失败流中 `TURN-OK` 计数 = 0
- LayerFormat 屏幕真值：用例自打印 `row 015|code row for the family`（无 gutter）与 `layer 2 rows=[13 17]`
- 未提交任何产品改动：本 run 结束前 `git status --porcelain` 只列出本报告（见提交前的检查）
