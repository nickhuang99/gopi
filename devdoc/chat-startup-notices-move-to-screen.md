<!-- doc-class: historical -->
# 启动提示不再污染输出区 —— chat 的 tips 回到启动界面

**任务**：`01a0fecb-a231-7be0-87bf-70c38d3cb65b`
**日期**：2026-10-03
**操作者原话**：*「那些是 tips for user，请把这些放到 启动界面里，不要放到 输出区域里」*

## 缺陷（有测量）

`gopi chat` 先画启动屏（`chat_banner.go` 的方框 + 侧栏 tips/session 事实），
紧接着往**输出区**（answer band，操作者读作对话记录的那一块）写三行 `[gopi] …`
散文：

```
[gopi] chat: this terminal does not report Shift+Enter (no reply to the keyboard-protocol query) — press Alt+Enter for a newline inside a message; Enter alone sends it.
[gopi] chat_surface tool: this run's own screen is readable → /home/user/.gopi/sessions/<id>/chat-screen.dump (…)
[gopi] chat_surface tool: this run's own surface identity is published to its children → GOPI_CHAT_SURFACE_ID=<pid>:<dir> (…)
```

每条折成 2–3 行，出现在操作者提问之前的对话区里 —— 原始取证见
`docs/demo/replay/appendix/README-v1-text2img.md` 与
`devdoc/handoff-20260928-chat-self-test-and-unverified-render.md`。

### 5-why：它们为什么会在 band 里

1. **症状**：提示出现在 band。
2. **机制**：它们走 `chatInput.WriteNotice`。保留布局（reserved layout）装上后，
   直接 flow 到 stderr 的写会落在编辑器最后画的那一行上，被下一次重绘擦掉 ——
   `lineedit_wire.go` 的 `WriteNotice` 自己的注释记录了这次测量（`/new` 的缺陷）。
3. **规范**：于是「能活过重绘的载体」在当时只有 band 一个，所以建议、状态、管道
   信息**一律**进了 band。`WriteNotice` 修好了「看不见」，却把「内容该放哪」定错了。
4. **根因**：启动屏本来就是一个「专门承载本 run 事实」的载体（session uuid、模型、
   节点、目录、tools 模式、版本、换行键），但提示的三条信息**没有走它**——
   两条信息在同一时刻可用，却被分散到两个载体。
5. **所以**：把提示**挪**到启动屏，而不是删掉、静音或压在 band 里。

## 设计

三条提示的**事实**进侧栏，**细节**进方框内新增的 `Notices` 段：

| 原提示 | 侧栏事实（`surface` 行） | 方框内 `Notices` 段（短句） |
|---|---|---|
| 键盘协商建议 | `newline Shift+Enter` / `newline Alt+Enter` | 无（侧栏已陈述，属 tip） |
| screen readable / off | `screen readable` / `screen off` | `screen dump → <path>` / `screen dump off — restart without --no-dump-screen` |
| identity published | `identity published` / `identity unpublished` | `surface identity not published — …` |

**顺序约束照旧成立**：方框是**一次** `WriteLines` 提交的，侧栏事实是那次组装的
参数 —— 因此没有任何东西能在方框打印**之后**落到侧栏上（即 commit `6544dad5`
修的那个缺陷：「tips 落在 side panel 上而不是 transcript 里」）。

**回落不是可选项**（Rule 15）：cooked/重定向运行、或窄到画不出方框的终端，
没有屏幕可以承接提示 —— 此时每条提示走它历史上的**同一个** `WriteNotice` 调用，
管道输出与改动前逐字相同。这正是 chat chrome 其余部分守的契约（关色即字节相同），
也是提示**文案常量一个字未改**的原因（`chat_panel_notice.go` 里为侧栏副本剥掉
`[gopi] ` 前缀，流上保留整行）。

**不会被截断**：方框内的 `Notices` 段是唯一「含义依赖词句而非数值」的文字，
所以它**整条包进盒内**、按 notices 边界停，装不下时最后一行写
`… more below`，多出来的原样走流（`chatBannerNoticeFit` 同时告诉调用者哪几条没画）
—— 不留半句，也不静默丢弃。

## 落地的文件

| 文件 | 改动 |
|---|---|
| `cmd/agent/chat_panel_notice.go` | **新增**。提示的持有/回落机制、`chatBannerSurface` 事实词汇、`chatStartupNotices` 采集点 |
| `cmd/agent/chat_banner.go` | 面板新增 `surface` 事实行（`chatBannerSessionFactCount` 6→7）；新增盒内 `Notices` 段（`chatBannerScreen` / `chatBannerNoticeFit`）；`chatComposeStartupScreen` 拆出 `chatBannerSpecFor` + `chatComposeStartupScreenWithNotices` |
| `cmd/agent/chat_keyboard_protocol.go` | `chatAnnounceKeyboardProtocol` 不再自己打印建议，改为**返回**判定；调用方把句子作为提示持有 |
| `cmd/agent/chat_surface_surface.go` | `installChatSurface` 不再打印，改为**返回**发布的 identity 值（`(func(), string)`） |
| `cmd/agent/cli_chat.go` | REPL 装配：先采集提示 → 组屏（含 Notices 段）→ 一次提交 → 未画的走流 |

## 测试

- `cmd/agent/chat_panel_notice_test.go`（**新增**，包内 5 例）：
  段在盒内且每行等宽；预算穷举下**绝不半画**、盒底线始终在；回落**只打印没画的那条**
  且顺序不变；三条事实在面板/段落/流**至少一处可读**（防止「静默删除」蒙混过关）；
  `chatStartupNotices` 与历史路径逐条对应。
- `testsuite/chat_banner_band_clean_pty_test.go`（**新增**，真 pty + 真二进制）：
  启动屏**之后**的字节里不得出现 `[gopi] chat:` / `[gopi] chat_surface`；同时断言
  启动屏上确实陈述了本终端的换行键（事实是**挪**了而不是丢了）。

### 红→绿（同一用例，同一宿主）

用未改动的 master 二进制跑**新用例**（`git stash` 后单独构建到 `bin/gopi-base`）：

```
GOPI_BIN=$PWD/bin/gopi-base go test ./testsuite/ -run TestChatPTY_StartupScreenLeavesTheAnswerBandClean
--- FAIL
    the ANSWER BAND carries startup chrome ("[gopi] chat_surface") —
      [gopi] chat_surface tool: this run's own surface identity is published …
      [gopi] chat_surface tool: this run's own screen is readable …
      [gopi] chat: this terminal does not report Shift+Enter …
    （三行都在方框底线 ╯ 之后）
```

用本分支二进制跑同一条：

```
GOPI_BIN=$PWD/bin/gopi go test ./testsuite/ -run TestChatPTY_StartupScreenLeavesTheAnswerBandClean
--- PASS (7.45s)
    ╯ 之后只剩 <ESC>[28;1H<ESC>[2K（清行），无任何 [gopi] 行
    面板末行：surface  newline Alt+Enter
```

### 重做（2026-10-03，rework onto current master）

本分支的原始提交基于 `d8c4d74d`，被拒（与 master 冲突）。重做在**当前 master**
（`7f515cc7`）上重新表达，**不做 `git apply`**：

1. **冲突只在一处**：`git diff --stat d8c4d74d..master` 显示 master 只改了
   `cmd/agent/cli_chat.go`（+124/−13：`16fe45d0` 的 beat 驱动重放、`bc315851` 的
   layout stream、`dc11ffbf` 的 mask 过）与 `devdoc/DOC-INDEX.tsv`（+4 行）。
   其余 8 个交付文件在 master 与 `d8c4d74d` **逐字节相同**
   （`git diff d8c4d74d..HEAD -- <file>` 为空），因此它们的变更集原样保留。
2. **`cli_chat.go` 按当前 master 重新表达**（不是贴回旧 hunk）：
   - `installChatSurface` 的返回值接入**采集块**，采集发生在组屏**之前**
     （方框是一次 `WriteLines` 提交，事实是那次组装的参数）；
   - master 的 **beat 驱动**（`sess.beats.maskText`）**保留**：遮罩应用在**整块**
     组屏结果上、且在 `WriteLines` 之前；
   - 顺序成为 `installChatSurface` → 采集（keyboard → identity → screen）→
     组屏（含 Notices 段）→ 一次提交 → 未画的走流回落 —— 正好满足
     「identity 的值必须先有」这一依赖。
3. **`DOC-INDEX.tsv` 取并集**：master 的新行全部保留，本分支的行插在
   `chat-small-height-wait-rework.md` 之后（字母序位置），`comm` 双向验证
   无丢失、无重复。

## 验收中发现的自身缺陷（已修，本轮）

pty 真跑对比 base/new 时，`git diff` 抓到**两处我自己引入的回归**——都发生在
「无屏回落」这条路径上，而这条路径的契约正是**与改动前逐字相同**：

1. **顺序被换**：历史顺序是 keyboard 建议 → identity → armed/unarmed（master 的三处
   打印点就是这个次序）；我第一版写成 keyboard → armed/unarmed → identity。
   cooked 运行实测 `diff` 可见两行互换（同一 stdout，等价于换顺序打印）。
2. **tools-off 运行丢了 identity 那行**：master 的 identity 打印点在
   `installChatSurface`（`chat_surface_surface.go`，**不过 tools 闸门**），
   armed/unarmed 才在 `if sess.toolsEnabled` 里。我把 identity 收进了
   `if !toolsOn { return }` 之后，于是 `gopi chat`（无 `--tools`）少了一整行 ——
   正是本文件要防的**静默删除**（Rule 15）。

**修法**：`chatStartupNotices` 按历史次序采集——(1) keyboard 建议，(2) identity
（不论 tools 开关，缺失时才是「发布失败」那条），(3) tools-on 才有的 screen 事实与
armed/unarmed 行。**回归测试**：`TestChatStartupNoticesHoldEveryLineTheReplWouldPrint`
新增两条断言——tools-off 且 identity 非空时必须恰好持有 identity 那一行；三条提示的
**顺序**必须等于历史次序（这是「挪了不是丢了」里「没丢」之外的「没乱」）。

**取证（同一沙箱、同一宿主、逐字节）**：

```
# cooked（无 tty），base vs new，把 [pid/会话号] 归一化后 diff
GOPI_HOME=$SB ./bin/gopi-base chat [--tools] < /dev/null   →  /tmp/eq-*.txt
GOPI_HOME=$SB ./bin/gopi      chat [--tools] < /dev/null
diff → IDENTICAL（tools OFF 与 tools ON 两条都逐字相同）
# 44 列（窄到画不出方框，走回落），去掉换行折行后 diff
diff → IDENTICAL，三条标记顺序两边同为 Alt+Enter / identity / screen
```

## Not Done / 诚实记录

- **分支作用域测试跑（`make test-budget PKGS="./cmd/agent ./testsuite ./internal/standards ./internal/lineedit"`）
  没有全绿**，且已做**归因测量**（同一 `-run` 匹配集跑在只差本分支的 base 树上）：
  - `internal/standards`：`TestStandardGates` / `TestStandardGateDataReport`
    （含 `TestGate2TestEnvRedProof` / `TestGate3ElseAfterReturnRedProof` /
    `TestGate4PanicRedProof`）**base 上同样红**（base：`--- FAIL: TestStandardGates
    (13.66s)`、`--- FAIL: TestStandardGateDataReport (13.83s)`；本分支同判）——
    **不是本分支引入**。
  - `testsuite` 的 PTY 家族（`TestChatEmphasisColourAlignment_OnRealPTY` /
    `TestChatLayerFormat_OnRealPTY` / `TestChatLinebreakCadencePty` /
    `TestChatRenderRedZone_OnRealPTY` / `TestChatPTYCut_…` /
    `TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel` /
    `TestChatDowngradeNoticeReachesStderrOnAPTY` /
    `TestTask_BarePrompt_SIGTERM_…`）：**base 树上 8/8 全部同样红**
    （`--- FAIL: …` 逐条一致）。跑时宿主 load 10.5→12.5（4 核），属既有的
    「整包并发下 PTY 用例不稳」家族（见报告的证据段）。
  - `cmd/agent`：唯一红的是
    `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`（base 该跑里
    `ok github.com/nick/gopi/cmd/agent 7.042s`），本分支**单独跑 PASS（1.978s）**
    —— 即已知的负载相关偶发（该用例的失败时长与自身的等待常量一致）。
  - **本分支新增的用例 `TestChatPTY_StartupScreenLeavesTheAnswerBandClean`
    在同一次 `-count=1` 里 PASS**；本分支触碰的 `cmd/agent` 用例
    （`ChatPanelNotice|ChatBanner|ChatKeyboard|ChatStartupNotices`）也全绿。
- **`testsuite/chat_banner_pty_test.go` 在本机失败，且与本改动无关**。已在
  **未改动的 base 二进制**上复现：屏幕 dump 采样器对该布局不取帧，
  报 `never carried the startup screen`。因此本任务的 pty 验收改为读
  **pty 字节流**（同样字节级、同样真实），并把这一既有故障如实记在此处，
  未在本分支顺手修它。
- 侧栏 `surface` 行在窄终端上会按既有「最后一条事实先让位」的规则让位
  （`chatBannerFacts` 的既有优先级），此时方框内 `Notices` 段仍陈述同一事实。
