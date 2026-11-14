<!-- doc-class: normative -->
# 同一个引擎，两种面：`gopi web --tools` 的工具轮为什么被 chat 写边界拦成"审批请求"

**Task**: `01a159ad` — `TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames`
**Worktree**: `/home/user/workspace/01a159ad-c08b-73a0-8e9b-627ce9d84bbb` （基线 `4dc13ca`）
**Evidence**（全部在本 run 的 session 目录，不在任何 git 树里）：`~/.gopi/sessions/01a159ad-c08b-73a0-8e9b-627ce9d84bbb/`
（`red-prefix.txt` 修前红、`green-target.txt` 修后绿、`redproof-carrier.txt` 新测试标红旧行为、
`greenproof-carrier.txt` 复原后绿、`live-turn.sse` 真跑 SSE、`web-live.log` 启动横幅）

## 1. 结论（一句话）

`Agent.ChatOps` 对**所有**驱动它的面都把 `a.ChatMode = true`，而 chat 写边界只认这一个开关；
于是「页面自己在横幅里承诺 *no approval gate*」的 HTTP 面，被按「控制台面（有真人可问）」的
语义判了 `prompt` —— 一个在浏览器面上**没有读者**的问题。修法不是关掉边界，而是**把"面"
作为值传进这一轮**，让 `prompt` 在无审批通道的面上退化为该面**开轮前就已经声明**的答案，
`forbidden` 一律仍然胜出。

## 2. 5-why（每一层都有实测输出）

**现象**：`TestWebChat_R2_…` 红 —— 文件没落盘。
```
--- FAIL: TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames (0.01s)
    cli_web_align_test.go:350: tool round did not really write /dev/shm/.../made-by-web-test.txt:
    open ...: no such file or directory
```

**why 1 — 为什么没落盘？** 真 dispatch 根本没执行 `write`。在测试里把 tool 帧打出来：
```
DEBUG tool frame: map[kind:call name:write summary:/dev/shm/.../made-by-web-test.txt]
DEBUG tool frame: map[kind:result name:write summary:[NEEDS OPERATOR APPROVAL — command NOT run] `/dev/shm/...`]
```
`executeToolDefault` 的 write 分支第一句就是 `if msg := a.chatModeWriteRefusal(...); msg != "" { return msg }`。

**why 2 — 为什么 chat 写边界会接管一个 HTTP 面？** 因为它的唯一开关是 `a.ChatMode`，而这个字段由
`ChatOps`（`internal/agent/chat_ops.go:489`）和 `Chat`（`loop.go:5002`）在**每一轮**无条件置 true：
```go
a.ChatMode = true
defer func() { a.ChatMode = false }()
```
调用方有四个面，同一个引擎：`cmd/agent/cli_chat.go`（控制台）、`cmd/agent/term.go`（bare prompt）、
`cmd/agent/cli_web.go:1429`（`gopi web`）、加上复用同一 `webServer` 的 `gopi ui` / `gopi cli`。

**why 3 — 为什么边界在那条路径上给出 `prompt` 而不是 allow？** 本进程从未解析过任何策略通道：
```
$ grep -rn "chatAnnounceChatPolicy\|SetChatPolicy(" --include=*.go cmd/agent | grep -v _test
cmd/agent/chat_policy_flag.go:105:	tools.SetChatPolicy(rules, resolved.Posture)
$ grep -rn "func runWeb\b" --include=*.go cmd/agent     # 只有一个 runWeb，且不调用上面那条
```
`runWeb` 从不调用 `chatAnnounceChatPolicy`，于是 `chatPolicyFrozen` 保持零值 → 内置表 `prompt *`
+ 默认 posture `on-request`（`internal/chatpolicy/defaults.go`、`chat_policy_gate.go`）。
`ChatWritePathRefusal` 于是走 `chatWritePolicyText`，即 `[NEEDS OPERATOR APPROVAL …]`。

**why 4 — 为什么 `prompt` 在这个面上是错的答案？** 因为 `prompt` 的语义是"去**对话里**问操作员、
等他说 yes 再重试"（`chat_policy_gate.go` 的 `chatWriteApprovalBoard` 把这句写死）。HTTP 面上
**提问者就是操作员本人**：模型把这句话渲染回同一个浏览器，问题回到问问题的人手里，这一轮就死在这里。
而这个面自己的三处文案（`cli_web.go` 启动横幅、help 页 `--tools` 段、页面 note）都写着
"turns run the full gopi toolset with no approval gate (loopback only, local-shell trust)"。
**面的声明和面的行为互相矛盾** —— 这才是缺陷。

**why 5（机制层）— 为什么会长期无人发现？** 因为"面"在这个引擎里**没有名字**：
`ChatMode bool` 只区分「chat 形态 vs task 形态」，不区分「哪张面」。控制台面上 `prompt`
是正确且必需的（真人在场），HTTP 面上它是死路；一个布尔量无法同时表达两种情况，
所以两边的语义被同一个值压平，谁先写测试谁先撞上。

## 3. 修法（最小改动，产品侧）

一个值 + 一次判定，不新增配置通道（Rule 35）：

| 位置 | 改动 |
|---|---|
| `internal/tools/chat_policy_carrier.go`（新） | 面名常量（`terminal`/`web`/`ui`/`cli`）、**声明表**（哪些面在自己的横幅里承诺 no approval gate）、以及 `CarrierDecision`：只有 `prompt` 且该面无审批通道时退化为该面声明的答案；`ForCarrier`/`ChatWritePathRefusalForCarrier`/`ChatBashWriteRefusalForCarrier` |
| `internal/chatpolicy/posture.go` | `Posture.WithDecision`（值变换：保留 source，换 unmatched 答案） |
| `internal/tools/chat_write_guard.go` | 把两条臂的三值映射抽成 `chatWritePathRefusalMatch` / `chatBashWriteRefusalMatch`，让面臂与冻结臂走**同一段**渲染 |
| `internal/agent/loop.go` | `Agent.ChatCarrier string`（空 = 未知 = 什么都不退化） |
| `internal/agent/chat_ops.go` / `loop.go` | `ChatOpsOnCarrier` / `ChatOnCarrier`：publish/restore 一轮（与 `publishChatTurn` 同形） |
| `internal/agent/chat_write_guard.go` | hook 读 `a.ChatCarrier` 走面臂；新增 `publishChatCarrier` |
| `cmd/agent/cli_web.go` | `webServer.carrier`（空 = `web`）+ `webChatCarrier()`，turn 里用 `ChatOpsOnCarrier(carrier, …)` / `ChatOnCarrier(…)`；面名常量取自 `internal/tools`（Rule 14） |
| `cmd/agent/cli_ui.go` / `cli_term.go` | 各自服务构造时声明 `ui` / `cli` |

**为什么不是另外两条路**（都实测/读过源码后否掉）：

- **只改测试**（让断言接受 approval 文本）：等于把 `gopi web --tools` 的真实契约改成
  "这个面啥也干不了"，而它的横幅/help/页面三处都写着相反的话 —— 是产品缺陷，不是测试过时。
- **让 `runWeb` 也调 `chatAnnounceChatPolicy`**：那只是把默认值从 `prompt *` 换成别的，
  仍然是**一个全局面**：控制台需要的 `prompt` 语义会被一起关掉，且会把一个"哪张面"的事实
  塞进一个"进程级冻结"的通道里（Rule 2 的反面）。

**面的声明不是这里发明的**：`CarrierDecision` 的退化方向就是该面开轮前已经向操作员宣布过的答案；
未声明的面（含空值）拿回冻结的一对，行为逐字节不变。

## 4. 最小复现测试（新增，且证明能标红旧行为）

`internal/agent/chat_carrier_boundary_test.go`：

- `TestChatCarrierWebRunsTheWriteTheConsoleAsksAbout` —— 同一调用、同一冻结表：控制台面得到审批请求且
  文件**不在**；web 面得到执行、文件内容被读回。
- `TestChatCarrierForbiddenStillWinsOnTheWebFace` —— `forbidden write *` 在两张面上都拒绝（防"退化变放宽"）。
- `TestChatCarrierBashArmUsesTheSameAnswer` —— bash 臂同一答案，用目录存在性取证。
- `TestChatCarrierUnsetKeepsTheConsoleAnswer` —— 空面名 = 冻结行为（安全方向）。
- `TestChatOpsOnCarrierPublishesAndRestoresTheCarrier` —— 一轮 publish/restore。

**红证明（Rule 10.4，最小样本）**：把 `CarrierDecision` 的退化那一句去掉，重跑
```
--- FAIL: TestChatCarrierWebRunsTheWriteTheConsoleAsksAbout (0.00s)
    chat_carrier_boundary_test.go:86: the WEB turn still got a boundary answer instead of running:
FAIL	github.com/nick/gopi/internal/agent
```
复原后：`ok github.com/nick/gopi/internal/agent`，且
`test "$(git hash-object internal/tools/chat_policy_carrier.go)" = "$(git hash-object /tmp/carrier.bak)"` → `restore-identical`。

## 5. Rule 31 真跑（不是单测）

真实二进制 + 真模型 + 真 HTTP：

```
$ CGO_ENABLED=0 go build -o ~/.gopi/sessions/01a159ad-.../gopi-carrier ./cmd/agent/
$ ~/.gopi/sessions/01a159ad-.../gopi-carrier web --addr 127.0.0.1:8147 --tools
gopi web: tools ENABLED — turns run the full gopi toolset with no approval gate (loopback only, local-shell trust)

$ curl -s -N --max-time 120 -X POST http://127.0.0.1:8147/api/chat -H 'Content-Type: application/json' \
    -d '{"message":"Use the write tool to create the file <abspath> with the exact content: hello-from-web-carrier…"}'
event: tool  … data: {"kind":"call","name":"write","summary":"…/live-web-write.txt"}
event: tool  … data: {"kind":"result","name":"write","summary":"Successfully wrote 22 bytes to …/live-web-write.txt"}
event: tool  … data: {"kind":"call","name":"read","summary":"…/live-web-write.txt"}
event: tool  … data: {"kind":"result","name":"read","summary":"hello-from-web-carrier"}
event: done

$ cat …/live-web-write.txt
hello-from-web-carrier
```

**同一台机器、同一份 settings、修前** 这条写会被答成 `[NEEDS OPERATOR APPROVAL — command NOT run]`
（见 §2 why 1 的帧）。

## 6. 已知边界（如实写）

- **只处理 `prompt`**：`forbidden` 在所有面上都赢；`allow` 不变。审查者如果认为"HTTP 面也应该
  尊重操作员写的 `prompt`"，那是另一个决定（需要审批通道，而当前面没有），不是这次修的缺陷。
- **`ui` / `cli` 的声明取自它们自己的提示文本**（`uiToolsNoticeFormat`、`termToolsNotice`），
  与 web 同义；若某面将来改成"有审批通道"，把它的名字从声明表删掉即可回到冻结行为。
- **`runWeb` 仍不解析 chat 策略**（`--policy` 只有 `gopi chat` 有）。那是既有的、独立的事实：
  在 web 面上任何 `prompt` 都只能来自内置表或 `GOPI.md` 的 fence。本次不扩大范围。
- 未改 `ChatOps` / `Chat` 的签名（新增 `*OnCarrier` 变体），因此所有既有调用点与测试行为不变。

## 7. 验证

见 `## Verification` 段（目标测试红/绿、新增测试红证明、包级 `make test-budget`）。
