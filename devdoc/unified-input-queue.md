<!-- doc-class: normative -->
# 统一输入通路：一个队列，四个来源（human / queue / notify / llm）

**范围**：会话输入如何进入模型的那一条通路 —— `internal/lineedit` 的粘贴引用
格式、`gopi notify --session … --message …` 的投递门控、`internal/inputsource`
的来源词表，以及 `cmd/agent` 的边界标注。

**状态**：本文件是这一改动落到 master 的说明（standing claim），代码锚点见文中
引用；实测数字见 §4（本机 peer1，2026-10-06）。

---

## 1. 为什么只有一个队列

四个来源把文本交给同一个会话：

| 来源 | 进入方式 | 值 |
|---|---|---|
| human | 提示符输入的一行、终端粘贴被编辑器落盘的行 | `human` |
| queue | `/queue <prompt>` 停放、由 REPL 在自己的队列上 `Take()` 取走成为一轮的那行 | `queue` |
| notify | 另一个进程投递：`gopi notify --session <id> --message <text>` | `notify` |
| llm | 模型自己通过 `chat_surface` 工具的 `type` 动作打出的一行 | `llm` |

**`/queue` 为什么不是 `human`**（task `01a10f9b` 修正）：作者相同，**产生者不同** ——
这一轮的行是从队列里出来的，不是从键盘出来的，取件时提示符前没有人。`human` 把两者
压平之后，任何按来源统计的读数都分不出「有人在场的一行」与「停放的一行」，而引用
格式里那个字段本来的用途就是命名**产生者**。判据不是文本：`/queue` 排队的提示词
**就是**那一行字节，下游无法从内容上分辨（`lr.queueTurn` 由取件处记录，
`chatSealSource` 读取，见 §5）。

**三条路，一个队列**：`theJobs.notices`（`internal/tools/job.go`），唯一取件点
`TakeJobNotices`（同文件）。`gopi notify` 的投递落在
`<session-dir>/messages/inbox.jsonl`（`SessionInboxDeliver`，
`internal/tools/session_message.go`），由**轮边界**用 `SessionInboxLoad` 并入同一个
`notices`，因此没有任何第二条队列、第二处注入。

**为什么来源必须是一个值，而不是一个 bool**：读到一个落盘的 payload 引用的人，
必须能分辨「操作者发的」与「别的 agent 发的」—— 两者的信任度不同，「我该回复谁」
的答案也不同。bool 会把这件事压平；四处各写一个裸字符串则会漂移（Rule 1b 的
字符串关键字条款正是为此）。所以词表只有一个定义：`internal/inputsource`
（`human` / `queue` / `notify` / `llm`，`Known()` 是它的守卫），四处消费者都引用它，谁都不
拼写字符串。

---

## 2. 引用格式：一次改动，渲染与解析同改

引用（reference）是「payload 已经落盘，输入行上只留一个把手」的那一行文本。
它由 `PasteRefFor` 渲染（`internal/lineedit/pastestore.go`），由
`PasteRefPaths` / `HasPasteRef` 读回（`internal/lineedit/pasteref.go`）。

```
老： [paste 4095: /abs/path/paste-….txt]
新： [paste 4095: /abs/path/paste-….txt source=notify size=70007B]
```

**兼容只有一个方向**（操作者裁定，2026-10-06）：**新代码读老数据** ——
缺字段就**不显示**，这就是「兼容」的含义。反过来没有意义：老解析器已经不再
运行了。所以 `PasteRefFor` 是**同一个函数**产出两种形状
（`Source != ""` 才追加 `source=`，`Bytes > 0` 才追加 `size=`），读的一侧
（`pasteRefParse`）容忍缺失；没有任何一条分支需要两边保持同步。

**路径的边界不能靠第一个空格切**：绝对路径本身可以含空格。因此新增
`pasteRefFieldCut`，把「路径在哪里结束」收敛成一个判断 —— 第一个
` <key>=` 出现的位置（`key` ∈ {`source`, `size`}）。

**改动顺带修掉的一处「格式的第二份拷贝」**：`internal/lineedit/paste_spill_test.go`
里的 `pastePathFromRef` 原来自己解析一遍引用。引用一扩展，它就把
`path + source=… + size=…` 整段当成路径，8 个 spill 用例随即假红
（报错 `size=65536B: no such file or directory`）。现在它走生产解析器
（`PasteRefInfoFor`）：一个格式，一个读者。

消费者的适配：`editor.go`（`pasteSource` 取自 store 的 `Source()`，续写同一粘贴
时老/新引用用同一个来源）、`answer.go`（未改，读路径）、`cmd/agent/chat_boundary.go`
（`chatSealMessage` 增加 `source` 参数）、`cmd/agent/lineedit_wire.go`
（编辑器 store 标注 `inputsource.Human`）。

---

## 3. notify 的大消息门控：队列里只放引用

**阈值就是输入行自己的容量**，不是第二个新数字：

```go
if !lineedit.ExceedsInputLineBound(message) { … }
```

`ExceedsInputLineBound` 与 `InputLineRuneBound` 都在 `internal/lineedit/pasteref.go`，
`InputLineRuneBound = valueMaxRunesDefault`。**为什么用这个值**：inbox 记录会变成
一个**轮次开场语**（`chatSealMessage`，`cmd/agent/chat_boundary.go`），走的是和输入
行完全相同的边界 —— 一个不同的上限就是「一条消息能有多大？」的第二个答案。

**大消息 ⇒ 复用既有 PasteStore 落盘**，`<session>/paste/paste-<stamp>-<n>.txt`
（`PasteStore`，`internal/lineedit/pastestore.go`），inbox 记录只放引用：

```
queued: message <id> for session <sid>
inbox:  <sid>/messages/inbox.jsonl
payload: <sid>/paste/paste-20261006-015037-1.txt
note:   the inbox record is the reference (source=notify), NOT the payload bytes
```

**落盘失败 = 拒绝投递，不是降级**（Rule 15）。被门控拦下却写不出文件时，
绝不退回「原样追加」—— 那正是这道门要防的缺陷本身。拒绝文案
`notifySpillRefusalFormat` 说明字节数、上限、目标目录和失败原因，并明说
「the message was NOT delivered」。

**门控防的是什么（实测，不是假设）**：100 KB 正文原样追加会让
`inbox.jsonl` 每次投递重上 100 KB，而这个文件是**该会话每一个轮边界都读**的。

---

## 4. 实测（本机 peer1，2026-10-06，真实二进制）

`gopi tool sandbox create --label notify-e2e` 得到一份私有 profile，手工造一个
只含 `session.jsonl` 的会话目录 `e2e-notify-fixture`，然后：

**短消息（走历史路径，逐字节不变）**

```
$ GOPI_HOME=$SB ./bin/gopi notify --session e2e-notify-fixture --message "E2E-RULE31-7c19: a real delivery from a separate process"
queued: message f8d4239d7b34b945 for session e2e-notify-fixture
inbox:  $SB/.gopi/sessions/e2e-notify-fixture/messages/inbox.jsonl
session dir: $SB/.gopi/sessions/e2e-notify-fixture (resolved as its session directory name)
```

inbox 记录：`{"kind":"message","id":"f8d4239d…","message":"E2E-RULE31-7c19: …","source":"notify",…}` ——
`message` **就是**正文，磁盘上没有任何新文件。

**大消息（70 007 字节 > 65 536 rune 上限）**

```
$ GOPI_HOME=$SB ./bin/gopi notify --session e2e-notify-fixture --message "$(cat /tmp/big-msg.txt)"
queued: message e36264486c3e6b99 for session e2e-notify-fixture
inbox:  $SB/.gopi/sessions/e2e-notify-fixture/messages/inbox.jsonl
payload: $SB/.gopi/sessions/e2e-notify-fixture/paste/paste-20261006-015037-1.txt
note:   the inbox record is the reference (source=notify), NOT the payload bytes
```

| 测得 | 值 |
|---|---|
| 落盘文件 | `paste/paste-20261006-015037-1.txt`，**70 007 字节**（`-rw-------`） |
| inbox 里的引用 | `[paste 70007: …/paste-20261006-015037-1.txt source=notify size=70007B]` |
| inbox 记录 | **不含** payload 字节；只说「落盘了、不在这里引用」，并提示用 `read` 工具读 |
| 该行 inbox 记录大小 | 约 430 字节（对比：正文 70 007 字节） |

**老格式解析（兼容方向）**：`[paste 12: /s/paste-1.txt source=gopi notify --session size=12B]`
仍能解析出 path，但 `source` 的值**不**属于四个来源之一（`inputsource.Known` 返回
false）—— 老值是可读的**数据**，不是词表的成员。

---

## 5. 边界：谁标注来源，怎么标注

`chatSealSource`（`cmd/agent/chat_boundary.go`）把来源决定在**读的那条臂**上，
**从不看文本**：模型自己打的行和操作者打的行作为字符串无法区分（预算续期同样
按臂判定，理由相同）。

- `selfDriven` ⇒ `inputsource.LLM`
- `queueTurn`（行由 `chatQueue.Take()` 从 REPL 自己的队列取走，`cmd/agent/chat_queue.go`，
  在取件处记录）⇒ `inputsource.Queue`
- 通知轮里带**一个**外部消息 ⇒ 该消息自己的 `source`（从投递记录读，不重新推断）
- 其余（提示符、终端粘贴）⇒ `inputsource.Human`
- **一个都没有，或两个来源并存** ⇒ `""`，引用**不渲染** source 字段

顺序即优先级，且前两条**在构造上**互斥而非靠这里的排序：排队的一行只在自驱动臂没有
待办时才被 `default:` 臂取走（`cli_chat.go`），所以两者不会同时为真。写下顺序是为了
让将来增加第五条臂成为一个**决定**，而不是一个意外。

最后一条是 Rule 15a：缺席的可选字段保持缺席。一个背景作业的完成通知不属于四个
来源中的任何一个；一条同时带着两个来源投递的轮次也不是其中一个**拼写**——
词表里没有复合值，与其发明一个，不如不渲染。

---

## 6. 验证

```
go test -run 'TestNotify|TestPasteRef|TestPasteStore|TestJobNotice|TestRoundBoundary|TestInputLineBound' \
    ./internal/lineedit/ ./internal/tools/ ./internal/agent/ ./cmd/agent/
```

新增/改动的用例：

| 文件 | 断言什么 |
|---|---|
| `internal/lineedit/pasteref_ext_test.go` | 新形状逐字段、老格式可读、缺字段不渲染、路径内含 `key=`、多引用来源、未知来源被拒、分隔符不变 |
| `internal/inputsource/inputsource_test.go` | 四个值互不相同且非空、老拼写不再被认作来源、每个值都能穿过引用格式往返 |
| `cmd/agent/chat_seal_source_test.go` | `chatSealSource` 五个答案（含两个空答案）、**四臂两两不同值**（集合断言：两臂返回同一拼写即红）、边界渲染的来源/大小/路径与真实落盘文件一致 |
| `cmd/agent/cli_notify_deliver_spill_test.go` | 短消息逐字节走历史路径且不落盘；大消息落盘 + 引用带 source/size/runes 且**不引用正文**；写不出文件时拒绝 |
| `internal/lineedit/paste_spill_test.go`（改） | 该文件原有的 8 个 spill 用例改走生产解析器后重新变绿 |

**同一处缺陷的第二、第三个副本（本次收尾时实测发现并修复）**：`cmd/agent` 里还有两处
把引用语法重述了一遍 —— `chat_boundary_test.go` 的 `turnContextTexts` 分支切
`": " .. "]"`、`chat_paste_wiring_test.go` 的 `pastePathInLine` 同理。引用一扩展，两处
都把整段 `path source=… size=…` 当成路径，`os.ReadFile` 于是把**存在**的文件报成
`no such file or directory`：

```
read the spilled payload …/paste-20261006-021526-1.txt source=human size=65560B:
    no such file or directory
```

两处都改走 `lineedit.PasteRefInfoFor`（生产解析器）。教训：**测试自己重述格式 = 格式的
第二份定义**，而它正是会过期的那一份；一个 spelling 只能有一个 reader。

真实端到端（Rule 31）见 §4：跑的是 `bin/gopi` 二进制 + 真实 profile + 真实会话目录。

---

## 7. 没有做的事（诚实）

- **没有新增工具、没有新增配置键、没有新增常驻进程**（Rule 3 / 35）：落盘复用
  既有 `PasteStore`，目录就是会话自己的 `paste/`，随会话生命周期生灭。
- **`/queue` 排队的提示词不单独落盘**：它作为一行文本进队列，直到真正成为消息时才
  经过 `chatSealMessage` 的边界（与提示符输入完全同一条路），届时按需落盘并标注
  `queue`。这一点是复用的结果，不是遗漏。
- **inbox 文件本身没有自动裁剪**：`sessionInboxRefuseBytes` 上限仍然存在，超限时报错
  并说明只能人工清理 —— 这是既有行为，本次不改（不新建机制）。
