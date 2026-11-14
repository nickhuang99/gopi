<!-- doc-class: historical -->
# Task Report: `--self` 寻址 — 把「我在哪个窗口」从截图概念升级为通用寻址

Task: `01a0f244-7485-7ccc-b1d2-89fe50a696c8`（epic 由 `01a0f211` 的 pid-window 解析链重构而来）

## Completed

### 1) `internal/shot/selfaddress.go` — 解析本进程窗口 + 显式聚焦（复用解析链，不复制）

`internal/shot/pidwindow.go` 的 `windowRectFor(false)` / `matchWindowByPID` **一行未改逻辑**（仅新增
一个注释+一处共享入口），本文件只加"已知窗口之后"的那一半：

- `selfWindowForThisProcess()` — 寻址路径对解析链的**唯一调用点**（截图路径保持自己的调用；
  链是共享的，不是模式词）。
- `setInputFocus(window)` — 发 `SetInputFocus`（opcode 42，revert-to `RevertToParent`），
  然后**读回** `GetInputFocus`。读回是关键：请求本身没有 reply，信任写入的客户端会报告一个
  从未拿到的焦点。
- `pointInside(rect)` — 腿 B：把指针放进窗口矩形内（边距 `x11SelfFocusMargin`；窗口太小则取中心，
  绝不让指针落在窗口外——那正是本特性要消灭的误定位）。
- `FocusSelf()` — 先 `XSetInputFocus`，**再无条件**把指针移进矩形，然后读回焦点与指针；
  `Mode` 由服务器**读回值**判定：`FocusAfter == window` → `explicit XSetInputFocus`，
  否则 `pointer-position (XTest motion; XSetInputFocus was not honoured)`。
  两条腿都做，且**报告里能看出这一跑依赖的是哪一条**（一条静默替代另一条 = 本文件头拒绝的缺陷）。
- `ResolveSelfWindow()` — `--dry-run` 用：只解析，不设焦点、不移指针、不注入任何事件。
- `TypeIntoSelfWindow(text)` — voice 路径的整个操作：解析 → 取焦点 → **逐字**打进去
  （不解转义：转义解码属于 `act type` 的 flag 语法，不属于语音模型产出的文本）。
- `TypableText(text)` — 纯函数预检（不打开 display）：不可按键的字符在**开麦之前**就被拒，
  拒绝时点名是哪个 rune。

### 2) `gopi act type --self` / `key --self`（含 `--dry-run` 与 JSON 证据）

`cmd/agent/cli_act.go`：`--self` 加入 `act` 的 flag 集合，只对 `type` 与 `key` 生效
（其余动作按名字拒绝，理由：`--self` 是关于**按键目标**的陈述）。JSON 证据新增
`window` / `window_route` / `self` / `self_focus` / `self_mode`。help 里给出可跑示例。

### 3) `gopi voice_input --client --self`

`cmd/agent/cli_voice_client.go`：转录完成后走**同一个**注入路径
（`shot.XActuator.TypeIntoSelfWindow`），所以两个 verb 不会漂移。文本**逐字**打进去且
**不提交**（不追加 Return）—— review-before-send 是语音输入做成 CLI verb 而非 tool 的全部理由。
stdout 照样打印转录（注入是**追加**，不是替代）。`--server` / `--stdio` / `--list-devices`
按名字拒绝。

### 4) 唯一一条有序拒绝路径

`pidwindow.go` 的 `noOwnWindowError`（复用，未新增第二条）：打印**试过的每条腿**、
**每条为何失败**、**用户能做什么**（跨 namespace 的 ssh -X、无 pty、客户端不设 `_NET_WM_PID`
三种处方）。**绝不回退 `_NET_ACTIVE_WINDOW`**。

### 5) 测试

`internal/shot/selfaddress_test.go`（6 条）+ `internal/shot/selfaddressing_gate_test.go`（2 条，含红证明）
+ `cmd/agent/cli_self_addressing_test.go`（6 条）+ `cmd/agent/voice_self_typing_test.go`（2 条）。

## Not Done

- **`_NET_WM_PID` 缺失的窗口无法被 `--self` 寻址**：拒绝路径第 (c) 条给的处方是操作者显式
  `act move X Y` + `act click`——那是**有意的**，不是遗漏（猜测窗口 = 本特性要消灭的缺陷）。
- **`voice_input --client --self` 只覆盖 `--client`**：`--server`/`--stdio` 无窗口可寻，
  按名字拒绝（见上）。不是"没做"，是"不成立"。
- **`--self` 未扩展到 `gopi capture` 之外的第三个 verb**（`act click --self` 等）：
  Rule 3 的边界，且 `move/click` 写的是给定坐标，`--self` 对其无意义（已按名字拒绝）。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test ./internal/shot/ ./cmd/agent/ -run 'Self|SelfAddress|Act|Voice|Gate'`
  → `ok github.com/nick/gopi/internal/shot 3.806s` / `ok github.com/nick/gopi/cmd/agent 73.841s`
  - `go test ./internal/shot/ -run 'Self' -v` → 9 条 PASS，含
    `TestSelfAddressingNeverAsksForTheActiveWindow`、`TestSelfAddressingGateCatchesAFallback`（红证明）、
    `TestFocusSelfSetsTheFocusAndPointsInsideTheWindow`、`TestResolveSelfWindowInjectsNothing`、
    `TestTypeIntoSelfWindowAddressesThenTypes`
- Ran（活体验证，脚本存档在本次 session 的 `evidence/`）:
  - `verify-self-addressing.sh`：自建 `Xvfb :241`（1280x800x24）+ **两个自建 xterm**：目标
    （`GOPI-SELF-TARGET`，其 pty 前景子进程是 `cat > target.txt`）与负对照（control）。
    在目标窗口内跑 `gopi act type --text 'GOPIPOS-...-OK\n' --self`
  - `verify-voice-self.sh`：自建 `Xvfb :248` + xterm，ASR 用**真 wire 契约**（`POST /transcribe`、
    multipart、`{"text":...}`）的 stub 返回哨兵，其余（设备解析、arecord、WAV 构建、HTTP 转发、
    换行裁剪、可打性预检、`/proc/tty` → pty master → `_NET_WM_PID`、XSetInputFocus、XTEST 注入）
    全是产品自身路径
  - 拒绝路径：`setsid ... act key --key return --self < /dev/null`（无控制 tty）
  - `gopi act move 10 10 --self`、`gopi voice_input --server --self`、
    `gopi voice_input --client --stdio --self`（按名拒绝）
  - `gopi capture --self --out ...png` + `vision_analyze` 读回
- Result:
  - `evidence/rig-241/target.txt` = `GOPIPOS-2346416-OK`，`evidence/rig-241/control.txt` =
    `GOPINEG-2346416-X1` —— **两条哨兵互相没串门**：无 `--self` 的那一发按默认路由落进了**另一个**
    xterm（`DEFAULT-IS-MIS-TARGETED: confirmed`），`--self` 的那一发只进了自己的窗口。
  - `evidence/act-self.json`：
    `{"self":true,"window":"0x40000c","window_route":"_NET_WM_PID pid=2346430 (xterm) window=0x40000c client-area","self_focus":"0x1 -> 0x40000c","self_mode":"explicit XSetInputFocus","pointer":"29,29"}`
    —— 服务器读回焦点 `0x1`（PointerRoot）→ `0x40000c`，**这就是"知道窗口 ≠ 打进窗口"的实测证明**。
  - `evidence/voice-self.err`：`typed 24 character(s) into window 0x20000c via _NET_WM_PID pid=2353832 (xterm) ... (0x1 → 0x20000c, mode explicit XSetInputFocus) — not submitted`；
    `evidence/voicerig-248/target.txt` = `VOICE-SELF-2353534-LANDS`；
    `evidence/voice-send.json` 是配对的那一步 `act key --key return --self`。
  - `evidence/self-window-241.png`（capture 裁剪目标窗口，784x274）+ `vision_analyze` 读回
    `GOPIPOS-2346416-OK`。
  - 无 tty 拒绝：`rc=1`，stderr 打印 `stdin is not a pseudo-terminal (no /dev/pts/N)`、
    ancestry 走链、枚举到的 10 个窗口及其 pid、以及三条处方（a/b/c）。
  - `gopi doccheck` → `the devdoc corpus is consistent`
- Commit: `b222d49d feat(shot): --self addressing — resolve THIS process's window and focus it explicitly`
  （分支 `task/01a0f24474857cccb1d289fe50a696c8`；其后 `7982d6d3` 是 delivery-point
  sync 把 master 并进来的 merge commit，不是交付内容本身）

## Evidence (self-filed)

- `git status --porcelain`：本 worktree 在提交后为空（见下一条命令的输出）。
- 负对照实测：`evidence/rig-241/control.txt` 含 `GOPINEG-2346416-X1` 且**不含** `GOPIPOS-*`
  （`NO-LEAK: control.txt never saw the --self mark`），同一 display、同一时刻、同一个二进制。
- 焦点读回：`self_focus` = `0x1 -> 0x40000c`（前）= PointerRoot，证明**裸 Xvfb 无 WM 时
  指针位置才是路由依据**，`--self` 的两条腿都必要。
- gate 红证明：`TestSelfAddressingGateCatchesAFallback` 在 `windowRectFor` 里植入一次
  `_NET_ACTIVE_WINDOW` 的 `InternAtom` 后由绿转红（gate 是**行为级**的——读假服务器上的请求，
  不是 grep 源码，所以拼写变体绕不过去）。
