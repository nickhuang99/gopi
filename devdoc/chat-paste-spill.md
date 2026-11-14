<!-- doc-class: normative -->
# 聊天粘贴：一次 splice 不是 N 次，长粘贴落盘（paste spill）

**范围**：`gopi chat` 原始模式行编辑器（`internal/lineedit`）的粘贴路径，以及
`cmd/agent` 的 chat 接线。两个耦合缺陷，一个会话内修完：

- **(A) 卡顿**：缓冲区每次只长一个 rune（`Value.InsertRune` 每个 rune 复制整条
  rune 切片），N 字节粘贴 = O(N²)；且每个 4096 字节读出都会整屏重绘（重新转换
  整个缓冲区 + 逐行重排）。
- **(B) 缺少 Hermes 式落盘**：没有任何「长粘贴写到文件、输入行只留引用」的机制。

## 1. 实测（本机，修复前）

`Value.InsertString`（当时是逐 rune 内部循环）在测试里直接计时：

| 输入 | 耗时 |
|---|---|
| 1 000 字节 | 3.9 ms |
| 4 000 字节 | 61 ms |
| 16 000 字节 | 936 ms |
| 64 000 字节 | **23.0 s** |

这就是"粘贴一下、提示符冻住"的全部：这段时间编辑器卡在拷贝里，读循环一步都走不动。

## 2. 本体（A）：splice 一次，重绘一次

- `Value.InsertRunes([]rune) int` = 唯一的缓冲区写入点，**一次拷贝**完成整段
  插入；`InsertRune` / `InsertString` 都是它的薄封装（对外语义不变：返回值仍是
  被拒 rune 数，`Enter` 仍走同一条路径）。
- `spliceRunes` 的拷贝顺序是**先搬尾、后写头**（`copy(v.runes[pos+len(rs):], …)`
  再 `copy(v.runes[pos:pos+len(rs)], rs)`）：两段内存重叠时顺序反了会把尾巴踩掉。
- 编辑器在**一个读块**内把连续明文 rune 攒成一段（`pendRune` /
  `flushChunkRunes`），块结束时一次性 splice；遇到任何非明文键（移动、kill、
  Enter、带 Alt 的 rune）先 flush，**保证每个 handler 看到的缓冲区状态与逐键时完全
  一致**。
- `Value.NoteKeys` 仍然报告**键数**，不是延迟：粘贴突发窗口（CRLF 只算一个换行、
  粘贴来的换行绝不提交）的判定因此逐键不变 —— 这是刻意的，批处理只影响缓冲区
  拷贝与重绘时机。
- 重绘门保持不变：`Changed()` 只读一次，`lineChanged || menuDirty ||
  statusPending()` 才画。逐键处理时队列里每个键都让 Changed 为真、都画一次；批处理
  时寄存器先消化全部键、最后一次画 —— 所以屏幕像素不变，`Write` 次数从"每键"降到
  "每读"。

实测：256 KiB（64 个满读块）**341 ms**，同样的输入在修复前是分钟级。

## 3. 本体（B）：长粘贴落盘，行上留引用

> **拼写与落盘的唯一 owner 是 `internal/lineedit/pastestore.go`。**
> `PasteRefFor`（`[paste N: /abs/path]` 的**唯一生产者**）、`PasteRefKeyword`、
> `PasteThreshold`、continuation 规则、以及 3.1 的封套剥离全部在这一个文件里。
> **消费者**：编辑器（本文件描述的路径）与 `cmd/agent/chat_boundary.go`
> 的消息边界（用户输入不得原样进模型上下文 —— 见 `devdoc/chat-message-boundary.md`）。
> 读者侧只解析、不生产：`internal/lineedit/pasteref.go` 里只有
> `PasteRefPaths` / `HasPasteRef` / `ExceedsInputLineBound` / `InputLineRuneBound`，
> 它**不声明** marker word，只引用 owner 导出的 `PasteRefKeyword`。
>
> 这条约束有门禁，不是口头约定：`cmd/agent/chat_boundary_test.go` 的
> `TestChatBoundaryReferenceSpellingHasOneProducer` 解析 `internal/` 与 `cmd/` 下
> **所有生产 .go 文件的字符串字面量**（不是 grep：注释里出现该拼写是文档，不是第二个生产者），
> 在 `pastestore.go` 之外发现该语法、或发现 `PasteRefFor`/`PasteRefKeyword`
> 被声明不止一次，就 RED。已做 RED 证明（临时加第二个生产者 → 门禁报出文件与行号 → 移除后 GREEN）。
>
> 为什么需要它：两个分支各自写出了这套词汇，合并时在 `pastestore.go` 上 add/add 冲突；
> 冲突只是表面代价 —— 兄弟分支的副本**早于** `57f5ae3d`，按「取较新副本」解决冲突
> 会静默复活 3.1 实测的落盘损坏（文件首字节是终端封套）。因此合并被换成「保留 owner
> 整体、只移植它没有的那半」（见 `devdoc/chat-message-boundary.md` 的
> *The reconciliation this port IS*）。

判据是**一个完整读出**，但"完整"必须按**实测**来定，不能按缓冲区长度想当然：

- pty 把 64 KiB 粘贴交给读循环时是 **16 次 4095 字节 + 1 次 16 字节**（2026-09-28
  用 pty 驱动直测，`chunk=4095`）。因此阈值 = `editReadBuf - readChunkShortfall(1)`；
  若用 `>= 4096` 判据，**一次都不会触发**——正是它的目标场景漏掉。
- 粘贴**不是**按整读块交付的：短尾 read 属于同一个粘贴。第一版按"每个满读一个新
  粘贴"处理，结果是消息变成「16 个引用 + 逃逸的尾部正文」。所以：
  **一个粘贴 = 一个文件 = 一条引用**，续读 append 到同一文件，文件大小改写引用里的 N。
- 续读窗口用编辑器**已有的**粘贴窗口（`PasteEnterSuppressWindow`），不引入第二个
  数字；窗口内、且大于等于 `pasteContinuationMinBytes(8)` 的读算续读（小于 8 字节与
  击键无法区分，按击键处理更安全）。任何普通读关闭窗口，所以两次不同时刻的粘贴仍是
  两次。

阈值直接用 `PasteThreshold`，不引入第二个要同步的数字。

### 3.1 终端自己的 bracketed-paste 封套不是内容（真实 pty 才暴露的缺陷）

gopi 的 raw 模式自己打开了 bracketed paste（`raw.go` 的 `escBracketedOn`），所以
**支持它的终端会把每一次粘贴包在 `CSI 200 ~ … CSI 201 ~` 里**。解码器的职责是**丢掉**
这两个标记（`KeyPasteStart/KeyPasteEnd`），而**落盘路径在读出的字节交给解码器之前就写
盘** —— 它绕过了解码器，于是把终端的外壳当正文写进了文件。

实测（2026-09-28，pty 驱动对 `gopi chat`，脚本化夹具当时看不到）：

- 42000 字节的 bracketed 粘贴 → 文件 **42012 字节**，且**文件第一个字节就是起始标记**。
  脚本化测试只喂"纯正文"，所以一直是绿的。
- 形状不是"封套包着一整块"：4095 字节的第一个读全是正文，**起始标记是第二个读的头
  几个字节**（于是被内联在文件头），结束标记又落在更后面的一个读里。也就是说
  **任何大到会落盘的粘贴都会被写坏** —— 恰好是这个功能存在的场景。

规则（`stripPasteMarkers`）：只丢**贴在读两端**的那两个标记（读首的起始、读尾的结束），
不搜索、不替换中间的字节。原因不是猜线协议，而是线协议就是这样（终端把封套写在它正在
粘贴的那块的两端），而**锚定是防止规则吃掉正文的关键**：中间的字节一概不动，所以用户
真的粘贴进来的 ESC、CSI、控制字节、非法 UTF-8 仍然逐字节落盘（一个"看起来像封套"的
粘贴序列会被搜索式规则误删 —— 本测试就抓到了这一点）。跨读断开的标记不识别（两半按正文
处理）：识别它意味着要缓存字节，而"到了就写"正是落盘路径不能破的性质。

标记的**拼写只有一个家**：`internal/sgr` 新增 `BracketedPasteMarkerSeq(param)` /
`PasteMarkerStartParam(200)` / `PasteMarkerEndParam(201)`（`CSI <n> ~`），lineedit 只引用
它。这不是洁癖——`internal/standards` 的 sgr 单一来源门禁（Rule 13/14）**当场**抓到了我
第一版在 lineedit 里手写的 `"\x1b[200~"`：任何第二个拼写 ESC 序列的地方都是缺陷。

回归：`internal/lineedit/paste_spill_test.go` 的
`TestPasteBulkSpill_DropsTheTerminalsPasteEnvelope`（起始标记落在**第二个读**的头、
结束标记落在更后面的读尾——正是 pty 的形状）与
`TestPasteBulkSpill_APastedEscapeSurvivesTheEnvelopeRule`（中间字节一个不动）。

- 文件：`<session dir>/paste/paste-<stamp>-<n>.txt`，`O_EXCL` 创建（重名不覆盖
  别人的字节），0600，字节**原样**写入 —— 不经过按键解码器，所以粘贴里的 ESC、
  控制字节、CRLF、非法 UTF-8 全部原样落盘（**唯一的例外是终端自己加的封套**，见 3.1）。
- 引用：`[paste N: /abs/path]`，N 是内容的 rune 数（读者不必 stat 就知道引用了多少
  内容），路径是绝对路径（引用是用户拿到字节的唯一把手）。
- 目录来源**唯一**：chat 从**会话 logger 自己的 JSONL 路径**取目录（
  `chatPasteStore(ag.SessionLog)`），不再二次推导 sessions 根。会话目录不存在
  （logger 失败）→ **没有 store** → 编辑器退回历史行为（粘贴进缓冲区），而不是写到
  没人管的目录。
- 失败（写不了、名字用尽、引用塞不进缓冲区）**一律上报**（Rule 15）：写不成就退回
  打字，粘贴的字节绝不静默丢弃。

## 4. 什么一定不能坏（都测了）

- bracketed paste；CRLF 只算一个换行；粘贴来的换行绝不提交 —— 编辑器与 chat 两层
  的既有测试全绿。
- **cooked（非 TTY）路径逐字节不变**：`SetPasteStore` 在 cooked 上无 editor = no-op
  （`cmd/agent/chat_paste_wiring_test.go`）。
- 短粘贴（一次读小于 4096）走历史路径：一次 read 一行内容，不落盘，CRLF 折叠成一个
  换行，不提交。
- 无 store 时粘贴进缓冲区，边界仍是原来的 `valueMaxRunesDefault`。

## 5. 判定口径：像素不是回调

落盘的断言读**渲染出来的 cell grid**（`Screen`）：编辑行上出现引用标记、粘贴正文不
在屏幕上、粘贴里的 ESC 没有画到屏幕。成本断言读**读循环的墙钟**（带看门狗，超预算
即失败，而不是"跑完再说太慢"）与 `Write` 块数（每个读块最多一次重绘）。

## 6. 回归与 RED 证明

- `internal/lineedit/paste_cost_test.go`：**自包含**、只用修复前就存在的 API，因此在
  修复前的 commit 上可编译可运行 —— 把它复制到 `git worktree --detach HEAD` 的树里
  跑，即为 RED 证明。实测：修复前 15 s 预算内**从未完成**（看门狗报超时），修复后
  256 KiB 341 ms 通过。
- `internal/lineedit/paste_spill_test.go`：落盘字节精确性（CRLF / ESC+CSI / 控制
  字节 / 非法 UTF-8 四种形状）、**终端封套不落盘**（3.1 的两个测试：起始标记在第二个
  读的头上、中间字节一个不动）、命名与引用形状、短粘贴历史路径、失败退回、像素断言。
- `cmd/agent/chat_paste_wiring_test.go`：store 目录 = 会话目录下 `paste/`、无 logger
  即 nil、chat 层一次满读端到端落盘、cooked 无影响。

### 6.1 真实 pty 是唯一看见 3.1 的仪器

脚本化夹具喂的是"纯正文"，所以 3.1 在它们面前一直是绿的；**只有把真字节交给真终端**
才看得见——这也正是 Rule 31 的理由：编译过 + 单测绿不等于这个面被跑过。复现与验证都用
同一个 pty 驱动（隔离 profile，`HOME` 用 sandbox，见 Rule 34）：

```
python3 <sandbox>/demo.py    # pty.fork → 写 "\x1b[200~" + body + "\x1b[201~" → 回车出题
# 修复前：SPILL: …-1.txt 42012 bytes; BYTE-EXACT: False（文件首字节是 0x1b）
# 修复后：SPILL: …-1.txt 42000 bytes; BYTE-EXACT: True
```

## 7. 零依赖（Rule 35）

没有新工具、没有新配置键、没有后台清扫：落盘文件放在**会话自己的目录**里，随会话
目录的生命周期存在与消失；一次写入就是全部状态，没有索引、没有数据库、没有常驻进程。

## 8. 引用的读者只有一个（2026-10-11 补记）

**成品：引用字母表只有一个读者，测试也必须走它。**

引用形状在加 `source=` / `size=` 时**变了**：新字段渲染在**方括号里面**
（`PasteRefFor`，`internal/lineedit/pastestore.go`）：

```
[paste 4095: /abs/path]                          # 旧形
[paste 4095: /abs/path source=human size=65562B] # 现形
```

于是**任何自建的小解析器都会静默失效**：切到第一个 `]` 就把 ` source=human
size=65562B` 一起吃进"路径"，`os.ReadFile` 报 ENOENT —— 而**那个文件一直在原地**，
写者目录、引用目录、磁盘上的文件三者完全一致，错的只是读者。实测（本任务
`repro-fixbranch.txt`）：

```
chat_paste_spill_pty_test.go:106: … unreadable file
(…/paste/paste-20261011-175331-1.txt source=human size=65562B):
open …/paste/paste-20261011-175331-1.txt source=human size=65562B: no such file or directory
```

**这不是"某次写入没落盘"，是"格式演进 + 私有副本"这一类缺陷。** 同一个诊断此前
已经修过一次（`internal/lineedit/paste_spill_test.go` 的 `pastePathFromRef`，注释里
写着 2026-10-06 八条落盘测试以 `size=65536B: no such file or directory` 变红），
**testsuite 这一份没被一起改**——两份副本，一处修了，另一处继续红。

**做法**：读者只认 `lineedit.PasteRefInfoFor`（生产 reader），测试不再自建解析器。
`PasteRefInfoFor` 内部的 `pasteRefFieldCut` 在 ` <key>=` 处断路径，所以**带空格的
路径也能安全读回**——这正是"切第一个空格"看起来合理、实则错的原因。

**回归守卫**：`testsuite/chat_paste_spill_pty_test.go` 的
`TestPasteRefReader_ResolvesTheProductionSpelling`。它**故意不起 PTY**：缺陷在读者，
一个必须靠终端才复现的守卫，在没有终端的套件里等于没有。它走完整条链——store 落盘 →
`PasteRefFor` 渲染 → **测试自己的 helper 读回** → `os.ReadFile` 打开 helper 给的路径
——路径里**带空格**。把旧私有解析器塞回 helper，它立刻红，且报的是原缺陷同款签名
（`… .txt source=human size=42B` 被当成路径）；恢复即绿。

**教训（可复用）**：一种线格式 = 一个 reader。测试要断言引用，就调生产 reader；
自建副本在第一次格式演进时会**假装断言还成立**，并且把失败伪装成"文件不见了"。
