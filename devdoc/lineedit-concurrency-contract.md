<!-- doc-class: normative -->
# lineedit / uiclient 并发契约：谁写、谁读、为什么安全

**本文回答一个问句**：`go test ./cmd/agent -race` 在干净 master 上报的
`internal/lineedit.(*Screen)` 与 `internal/uiclient.(*ScreenDumper)` 竞争，修好之后
**哪条边被哪把锁关掉**，以及那些**故意不加锁**的边凭什么安全。它不是一份 API 文档，
是把"谁能碰哪个字段"写成可复核的句子 —— 下一个人加一条新路径时，用它判断自己是不是
又开了一条没有锁的边。

## 1. 修之前的五条真边（实测，2026-10-27，task 01a1756e）

基线读数（干净 master `934dd69`）：

```
go test ./cmd/agent -race -count=1 > race-before-cmd-agent.txt 2>&1
grep -c "WARNING: DATA RACE" race-before-cmd-agent.txt
771
```

771 个报告块按**两个栈顶符号**聚类（同一聚类的"地址/goroutine 号"不同，语义相同）：

| # | 写方 | 读方 | 根因 |
|---|---|---|---|
| 1 | `ScreenDumper.tailFlush` → `flushUnarmed` | `ScreenDumper.Write` / `flush` → `flushUnarmed` | `tailFlush` **先解锁再调 `flushUnarmed`**：`flushUnarmed` 读写的是 sink 自己的字段（`dump`/`last`/`lastSig`/`written`）并读 `d.screen`，于是这个定时器 goroutine 成了网格的**第二个写者** |
| 2 | `Screen.setScrollRegion`（经 `controlSequence`） | `Screen.ScrollRegion`（经 `Dump`） | 同上：`Dump` 在 sink 锁内读网格，而 `Feed` 在别的 goroutine 里写网格 —— 一旦 #1 让定时器也在锁外读网格，网格本身就变成了无锁共享 |
| 3 | `Screen.controlSequence` / `eraseDisplay` / `put` | `Screen.Dump` / `Line` / `attrRuns` | 同 #1（网格级帧） |
| 4 | `AnswerRenderer.drainNotes`（读循环） | `AnswerRenderer.Noting`（**外来 goroutine** 经 `bandwait.go:waitNoteWrite`） | `notes` 这个 slice header 无锁：外来生产者 `append`、外来等待者读长度、读循环 `take + 清空` |
| 5 | `Editor.SetDraft`（`chatInput.VoiceDraft`，语音生产者 goroutine） | `Editor.repaint`（读循环） | `repaint` 里**裸读 `e.drafts.text`**，而该字段由 `e.drafts.mu` 保护；同族的还有 `chat_voice.go` 的 `state.inLoop` 裸读 vs `markLoopGoroutine` 的锁内写 |

外加一条测试脚手架边（**历史记录：`cli_memory_sync_every_test.go` 已在 2026-11-05 随
`gopi memory sync --every` 的循环一起删除，这里描述的是当年那条脚手架边的形状；
同一课在现存的 `cli_notify_deliver_test.go` 上仍然成立**）：`cli_memory_sync_every_test.go` 用裸 `bytes.Buffer` 接子进程输出，
而 `os/exec` 在**自己的 goroutine** 上拷贝子进程输出（`writerDescriptor`）—— 测试主
goroutine 轮询 `buf.String()`，两者无 happens-before。修法不是加锁新机制，是用本包已有的
`syncBuffer`（`cli_notify_deliver_test.go`）。

## 2. 修之后的契约（这就是"为什么安全"）

### 2.1 `ScreenDumper`：sink 的状态只在 `d.mu` 里被改

**唯一规则**：`flushUnarmed` 必须在 `d.mu` **持有期间**被调用。`flush`（持有后调用）
本来就是这么做的；`tailFlush` 之前不是，现在改成同一个形状：

```go
d.mu.Lock()
d.tail = nil
d.dirty = false
closed := d.closed
err := d.flushUnarmed(false)   // ← 在同一把锁里
d.mu.Unlock()
```

`recordError` 自己取锁，所以它在**解锁之后**才跑（在锁内调用会自锁）。

由此得到的层级是单向的，不可能死锁：

```
d.mu  保护：d.screen(网格)、d.dump、d.last、d.lastSig、d.written、d.frames、d.err、d.closed、d.tail、d.dirty
  └─ 网格 d.screen 只能在 d.mu 内被 Feed/Resize/Dump —— 它没有自己的锁，sink 的锁就是它的锁
```

**读者**：`Write`（渲染 goroutine）、`flush`/`flushUnarmed`（同一 goroutine 与定时器）、
`SetGeometry`/`Geometry`/`Snapshot`/`Frames`/`Writes`（任意持有 sink 的调用方）——
全部走 `d.mu`。**写者**：`Write`（喂字节）、`SetGeometry`（`Resize`）。
两方相遇的点只有网格，而网格只在锁内被碰。

### 2.2 `AnswerRenderer.notes`：header 用 `notesMu`，行本身不可变

`notes` 有**两个**goroutine 碰它，这就是它必须加锁的原因：

- **生产者**：turn goroutine（`Note`/`NoteRuns`）**或外来 goroutine**（`WriteLines` 的
  flow seam、命令自己的输出块）→ `noteNow` / `noteNowRunsCopied` → `append`；
- **等待者**：外来调用方的 `bandwait.go:waitNoteWrite` → `Noting()`（只读长度）；
- **消费者**：读循环 `Editor.drainNotes` → `AnswerRenderer.drainNotes`。

锁只保护 **slice header**（长度、append、整体取走）。行一旦入队就**不可变**
（`noteRow` 不再被改），所以 `drainNotes` 在锁外逐行 `writeNote` 是安全的 —— 那些行
已经属于它自己（`takeNotes` 的返回值），队列里再进来的行属于下一次 drain。

`takeNotes` 的语义是**快照**而不是"清空"：`drainNotes` 提交期间进来的行留在队列里等下一
次 drain（生产者已经 `PublishStatus` 唤醒过读循环），不会被结尾的清空吞掉。

### 2.3 `Editor.drafts`：锁内写，锁内读 —— `repaint` 不许裸读

`drafts.text` 由 `e.drafts.mu` 保护（`SetDraft` 从任意 goroutine 调用）。`repaint`
是读循环，但"读循环"不等于"只有读循环碰这个字段"，所以它的两处引用改为走
`e.Draft()`（同一把锁）：

```go
rows := e.val.RowsForDraft(e.render.prompt, e.Draft(), e.width())
...
e.render.SetDraft(e.Draft())
```

**注意这里不引入"一次读、两处用"的快照变量**：这两处读之间的间隔里生产者可以再 publish
一次，那本来就是允许的（下一帧会用新值），而引入快照变量会需要一个新的同步决定 —— 两处
各自加锁读取，代价是一次 mutex，语义是"每次用到的都是当时的真值"。

### 2.4 `voiceChatMode.state.inLoop`：身份字段必须在 `state.mu` 内读

`inLoop` 写于 `markLoopGoroutine`（锁内），读于 `shutdown`。`shutdown` 原本在**释放锁
之后**裸读它，于是与写方无 happens-before。修法是把它读进一个局部变量，在同一把锁里读：

```go
loopIsCaller := false
{
    m.state.mu.Lock()
    loopIsCaller = m.state.inLoop == lineedit.CurrentGoroutineID()
    m.state.mu.Unlock()
}
if loopDone != nil && !loopIsCaller {
```

这个字段的语义（"当前这次 entry 的采集循环是不是调用者"）没有变，只是读它回到锁里。

## 3. 故意**不**加锁的边（读的人需要知道，否则会"顺手修"）

- `AnswerRenderer.committed` / `writes` / `statusWrites` / `statusDirty`：`atomic`。
  两个 goroutine 都碰（turn goroutine + 读循环），原子量正是它们的契约。
- `Editor.loopGoid`：`atomic.Int64`，`bandwait.go` 的等待循环按"owner 变了就放手"退出。
- `AnswerRenderer.editor`：构造后只读，注释里写明"read-only after construction"。
- `Screen` 的**所有**字段：不加锁是**契约的一部分** —— 网格属于 `ScreenDumper`，由
  sink 的 `d.mu` 串行化。任何**绕过 sink 直接持有 `*Screen` 并跨 goroutine 用**的新代码
  都是在拆这个契约（`internal/shot/textcapture.go` 那样单 goroutine 独占的用法是允许的）。
- 渲染字节流的**顺序**（不是数据）：`chatPaintWriter` 负责让两个画者的字节在 sink 与
  终端之间保持同一顺序 —— 那是顺序问题，不是内存模型问题，`-race` 看不见它。

## 4. 取证配方（复用，不是"跑一遍看看"）

```bash
# 先落盘再读退出码 —— 不走管道（管道的 $? 是 grep 的）
go test ./cmd/agent -race -count=1 > race-before.txt 2>&1; echo "exit=$?"
grep -c "WARNING: DATA RACE" race-before.txt

# 771 个块要按「两个栈顶符号」聚类再判断，别逐条读：
# 同一地址、不同 goroutine 号的重复块是同一个缺陷的重复采样。
```

窄过滤（一条缺陷一条命令，几秒出结果，用来做红→绿）：

```bash
go test ./cmd/agent -run 'TestChatDump_GridFollowsTheLayoutGeometryOnARealPty' -count=1 -race
# 修复前: exit=1, 5 × WARNING: DATA RACE
# 修复后: exit=0, 0 ×
```

整包 `./cmd/agent -race` 约 10 分钟，必须后台跑（`job(action=start)`），前台会被
无输出网杀掉。

## 5. 结论

这四条边的共同形状是**「一次解锁之后再碰受该锁保护的东西」**：`tailFlush` 解锁读网格、
`shutdown` 解锁读身份、`drainNotes` 从没锁过队列、`repaint` 绕过 accessor 读字段。
修法一律是**把访问挪进锁里**，没有一处是"加个 sleep/重试/放宽断言"，也没有一处改了
`-race` 的跑法或加了 build tag。
