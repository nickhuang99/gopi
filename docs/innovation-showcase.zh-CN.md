<!-- doc-class: roadmap -->

# innovation-showcase.zh-CN.md（骨架 · 5 条已填 / 2 条留位）

> 落盘位置：`/home/nick/work/github/gopi/docs/innovation-showcase.zh-CN.md`
> 与英文稿**逐节对应**，标题译法固定（见骨架表的对照）。
> **7 条已全部填完**（第 1/2/3/4/5/6/7 条，无 `<PENDING>` 残留）。
> 第 2 条的原始字节流按「不贴整段、给命令 + 形状 + 一句价值」写，理由见下。

---

# 这个 harness 做的、大多数没做的事

下面每一条命令都在真机上跑过，输出原样贴出。
没跑过的，条目里会写明 —— 这一页是记录，不是宣传册。

## 1. 与服务端账单逐请求对上的 token 计量

gopi 在本地算每一次请求的 token，再与服务端实际报回来的数字比。
`diff` 那一列就是论据：**每次请求都是 `+0`**，所以你看到的费用就是账单上的费用。

```console
$ gopi tool tokens --session <SESSION-ID>
line     msgs   tools   ours     server    diff
2        1      0       52881    52881     +0
```

退出码 0；`requests: 1 exactly equal: 1`。

**边界（实测踩出来的）：** 会话必须是**完整**的 —— 既要有 `session.jsonl`，
也要有带 `input_tokens` 的 `raw.log`。空会话、或没有请求记录的会话，跑出来没有有效内容。
**大小只决定快慢，不决定对错**：一份 190 MB 的 `raw.log` 跑过了两分钟上限被超时杀掉，那是我自己踩的坑。

## 2. 一次运行的两种独立录像

`screen-replay` 重放一个会话**真正渲染出去的字节**；`--verify` 拿两段录像逐帧比对，
报出第一处差异在哪一帧、哪一字节。另一半是 `raw.log`：每一条请求与响应原文，
token 各字段分列。

```console
$ gopi screen-replay <A> | head -c 400 | cat -v
^[?2004h^[?u^[6n^[c^[]11;?^\P+q636F6C6F7273^\P+q524742^[1;21r^[1;1H^[2J
^[22;1H^[2K^[1m^[38;2;255;248;220myou>^[0m^[5C^[21;1H^[2K^[21;1H
```

输出是一个真实会话录下的原始字节（70 MB 那段），**按记录顺序写回 stdout**：
不建画布、不重新换行、不改颜色。所以它可以被 pipe、被 hash、被逐字节断言。诊断信息走 stderr，
不污染 stdout。

```console
$ gopi screen-replay <A> --verify <B>
gopi screen-replay <SESSION-ID|FILE> --verify
  A: ~/.gopi/sessions/<A>/<A>.screen.stream — frames=826951 bytes=68333537
  B: ~/.gopi/sessions/<B>/<B>.screen.stream — frames=1025200 bytes=47377413
differ: frame 4 — payload bytes differ (7 byte(s) each; first differing byte at 4)
  A: frame 4 at byte offset 188 — "\x1b[1;21r"
  B: frame 4 at byte offset 188 — "\x1b[1;19r"
```

**精确定位到第 4 帧第 188 字节**（两段录屏的滚动区行数不同：21 对 19）。
这就把「支持 diff」这句空话换成了一个可复算的坐标。

**边界：** 一个 `frames=0` 的 `.screen.stream` 是空录制，比对它什么也得不到 —— 要用真实会话的录制。

## 3. 两个 agent，一个仓库 —— 是邮箱，不是聊天室

每个会话有自己的收件箱文件，**用 session id 寻址**。没有共享上下文，也没有共享记忆，
所以一个 agent 的错不会悄悄变成另一个的前提。**收据由读取方写**，这也是为什么「已送达」伪造不了。
消息在回合边界落地，**正在进行的 turn 永远不会被打断**。`--peer <alias>` 把同一套机制带到另一台机器。

```console
$ gopi notify --session <SESSION-ID> --message "$(cat /tmp/msg.md)"
session dir: ~/.gopi/sessions/<SESSION-ID> (resolved as its session directory name)
the session takes it at its next round boundary (an IDLE console wakes for it and
opens the next turn by itself; a turn already in flight is never interrupted)
```

这不是一个没人用的功能。就在这个仓库上，两个 agent 在一个晚上各自推翻了对方的一个结论：
一方报「工作树脏了 7485 行」，实测是 0；另一方查出对方当成干净的代码里，
有 33 处把一个内部主机名写死了。**两次都不是客气性的互评，两次都改变了要发布的东西。**

工作示例见 [examples/session-collaboration/](../examples/session-collaboration/)。

## 4. 三层记忆，在你开口前就已预取

蒸馏过的教训、可复用的流程、以及过去每一次运行的每一轮各一条可搜索笔记。
检索是自动的 —— 在你提问之前，相关的几条已经进了上下文。

```console
$ gopi memory search "clone 通道 push 被拒"
now=<TIMESTAMP>
[distilled]
#<ID> clone-channel 报「carries no commit to deliver」时如何判「已落地」还是「从没提交」
      tier=warm conf=1.00 lesson=lesson stored=2026-10-28 age=26d
      sources=row <TASK-ID> tokens=3 fb=0.97(32)
Q: gopi worker 的交付腿说 "X carries no commit to deliver..." —— 我该补提交，还是本来就没什么可交？
```

命中的这一行 Q 正好是当时真出现的那个问题。**`tier` / `conf` / `fb` 三个字段是可投票的**：
`fb=0.97(32)` 的意思是 32 次反馈把它推到了 0.97 的置信度。坏条目往下沉，好条目往上浮。

> 上面这段输出里 `<ID>` `<TASK-ID>` `<TIMESTAMP>` 是占位符：原输出带有本机主机名与真实任务 id，
> 发布版替换成了占位。字段名、字段值、命中内容一字未改。

**边界，这条很重要：** 预取进来的一对记忆，**只有在下一轮被投票才会留下**。
实测：八对零投票的记忆各自只活了一轮；一对被投了 `useful` 的，五轮之后仍在。
自动预取不是免费午餐 —— 它是一条靠投票维持的队列。

## 5. 一条能在压缩后活下来的自记

`self-record.md` 每一轮都写，上下文被压缩之后重新载入。
所以 500 轮之前做的那个决定，在装着它的那个窗口已经消失之后，仍然在。

```console
$ wc -l ~/.gopi/sessions/<SESSION-ID>/self-record.md
87
$ head -c 200 ~/.gopi/sessions/<SESSION-ID>/self-record.md
t1 <第一条自记的正文 — 那一轮做完了什么>
t2 <第二条 — 这一轮推翻了上一轮的哪个结论>
```

**每轮一行**，前面是轮次号。上面这份 87 行、70 KB —— 它是一个真实会话跑了几十轮之后留下的全部决定。

这不是日志：**下一轮的提示词里会把它读回来**，所以它改变行为。压缩删掉的是原始对话，
不是这一行行的结论。

> 上面两行的正文用占位概括：原件里是那次运行的内部工作内容。
> 行数、字节数、格式（`<轮次> <正文>`）是真实的。

## 6. 界面，以及它底下的单元格网格

`gopi web` 起一个本地浏览器聊天界面。`gopi ui_state` 把一个活的屏幕读回来，
形式是**单元格网格**，每个单元格区间带着颜色与格式 —— 不是像素，也不经过视觉模型，
所以「看屏幕」这件事的 token 成本与文本成正比。

```console
$ gopi ui_state --target chat
grid_begin count=25
000| … the answer band, wrapped as it is drawn …
023|you>
024| … status line: turn, tool, context usage, cache hit rate …
lines_end
```

返回的 JSON 里有一句自我描述，它就是这个命令的定义：

```json
{"tool":"ui_state",
 "what_this_is":"an interpreted cell grid (not bytes, not pixels)",
 "state":"live"}
```

**25 行、60 个单元格区间**，每个区间带自己的属性。同一份数据既能被程序读（`--json`），
也能被人看（`--target chat` 画出原样）。

> 输出里的内容是占位概括：原输出是屏幕上真实的对话正文。结构与字段名一字未改。

## 7. 一个静态二进制，没有安装步骤

没有 Node、没有 Python、没有 Docker、没有 CGO。对**发布版**二进制跑 `file`：

```console
$ curl -sL -o gopi https://github.com/nickhuang99/gopi/releases/download/v0.5.3/gopi_v0.5.3_linux_amd64
$ stat -c%s gopi
65409208
$ sha256sum gopi
9fa046ce6045948971a5632a115ca8ede824782daaa4f4400d9fa0f364bf342a
$ file gopi
gopi: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, BuildID[sha1]=ba846563bd697a92c2f81beb245fea549515d2e0, stripped
```

`statically linked` 是那个词。sha256 与 release 附带的 `.sha256` 文件逐字符相同。

**一个会真踩到的坑：** 地址写错的时候 GitHub **不会大声失败** —— `curl` 退出码仍然是 0，
body 是 9 字节的 `Not Found`。先看大小或先比 sha256，再信任它。

> macOS 那份是交叉编译出来的，**从未运行过**。别把「能编」当「能用」。
