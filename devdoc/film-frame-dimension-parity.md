<!-- doc-class: normative -->
# 取偶对齐：分支 01a105d4 在 master 上的**真实剩余**

Task: `01a10606-eefb-70e9-b070-2d8913cce141`（重做已删除分支 `task/01a105d4ef787055bd577b56cd4629ea`，tip `e1fa3b9c`）
Baseline: master `b3d701fd`（`e1fa3b9c` 的 merge-base 是 `2b9aea9a`，之后 master 有 3 个 commit）
Measured at: 本分支 @ `b3d701fd` + 本次 diff，2026-10-04。

> **性质**：normative（对 master 的常驻论断）。

## 1. 一句话结论

`gopi chat --film` 的**交付契约**现在对**两个轴**都查对齐，并**点名**失败的那个轴；
对齐值是**一个**定义（`playableGIFParity`），帧写盘器与交付门禁引用同一个数，不各写一个 2。

## 2. 这一轮**没有**重放旧分支的什么，以及为什么

旧分支 `01a105d4` 的 tip `e1fa3b9c` 改了 6 个文件（`git show --stat e1fa3b9c`）。
它修的那个缺陷——"拍出来的帧高是奇数"——master 已经用**另一条路**落掉了：

| | 旧分支 `e1fa3b9c` | master `47d60393`（已合并） |
|---|---|---|
| 新文件 | 无 | **`cmd/agent/chat_film_frames.go`** |
| 函数 | `filmEvenFramePNG` + `filmFrame` | `chatFilmEvenFrame` + `evenFrame` + `evenDim` |
| 落点 | `chat_film.go` 的 `shootBeat` 写盘前 | 同一个 `shootBeat` 写盘前（`chat_film.go:809`） |

**两者是同一个缺陷的两种实现**：截到偶数、不重采样、已对齐帧原字节直通。
master 的版本是**常量名**（`chatFilmFrameParity`）、有 `evenDim` 的最小样例测试
（`TestChatFilmEvenDim`）与空操作测试（`TestChatFilmEvenFrameIsANoOpOnEvenFrames`）。

因此**旧分支的 `filmEvenFramePNG` / `filmFrame` 没有被复活**（Rule 23：不要在别人的
实现上叠自己的）——复活它会得到两个做同一件事的函数，其中一个是死的。
本轮的交付是旧分支**真正还没被落掉的那部分**。

## 3. 真实剩余（本轮的 diff）

### 3.1 交付门禁的**高度腿**（核心）

master `47d60393` 确实加了高度检查，但它在 **`--wechat`** 分支里：

```go
// master b3d701fd，chatFilmRequirePlayable
	if !wechat {
		return nil          // ← 高度腿在这之后
	}
	if res.Height%chatFilmFrameParity != 0 { ... }
```

**这是一个真实的缺口**，不是风格问题：奇数高是**文件**的属性（yuv420p 按 2×2 下采样
色度的编码器**直接拒绝**），不是"交给谁"的属性。默认产物（不写 `--wechat`、只写
`--out`）**照样会交付一个很多编码器打不开的文件** —— 而 `47d60393` 自己的文件头
就写着"an encoder that subsamples chroma refuses an odd frame dimension outright"。

本轮：高度腿移到 `!wechat` 早退**之前**，与宽度腿并列，**对每个产物都查**。

### 3.2 按轴点名

旧的单句 `chatFilmOddWidthRefused` 只说得出口"宽"，一旦同一句话要承载高度，
`wide` 和它指的轴就都错了。本轮改成**参数化的一句**：

```go
const chatFilmOddDimensionRefused = "%s is %d px %s (ODD) — a reader that carries chroma at half resolution in each axis pads or drops a line of pixels from a frame whose dimension is not %d-aligned, so this animation is refused rather than delivered silently misaligned"
const (
	filmAxisWidth  = "width"
	filmAxisHeight = "height"
)
```

轴**词**是常量（Rule 1b：带含义的字符串只有一个来源），且**不是**第二个对齐数字 ——
对齐仍然是 `playableGIFParity` 一处定义（Rules 13/14）。
master 的 `chatFilmOddHeightRefused` 由此被这一句取代：它只覆盖高度、且不提"对齐到几"，
两个常量并存就是同一份拒绝的两个落点。

### 3.3 对齐值收敛到**一个**定义

master 的形态本已是"引用而非复读"（`chatFilmFrameParity = playableGIFWidthParity`），
但它引用的那个名字是旧分支要改掉的那一个：

```go
// master
playableGIFWidthParity = 2
chatFilmFrameParity    = playableGIFWidthParity   // 名字说着 Width
```

宽度一半已经由 `chatFilmRequirePlayable` 与 `filmPlayableMaxCols` 用；高度腿一旦落地，
`Width` 这个名字就是**误导性路标**。本轮把**被引用方**改名，引用关系保持一字不动：

```go
// 本轮
playableGIFParity    = 2                            // 两个轴共用的唯一对齐定义
chatFilmFrameParity  = playableGIFParity            // 帧写盘器用的名字，同一份定义
```

这不是新增一层命名：master 的镜像关系**保留**（写盘器与门禁引用同一个数），
改的只是那个被引用的名字——它以前只对宽度成立，现在对两个轴都成立。
`TestChatFilmParityIsOneDefinition` 钉住这条：两名字必须相等，且不等于"各写一个 2"。

## 4. 代码落点

| 位置 | 内容 |
|---|---|
| `cmd/agent/chat_film_gif.go` | 常量 `playableGIFParity`（唯一对齐定义）+ `chatFilmFrameParity`（引用它）；`chatFilmOddDimensionRefused` + `filmAxisWidth` / `filmAxisHeight`；`chatFilmRequirePlayable` 的两条轴腿（高度腿**不在** `--wechat` 分支里） |
| `cmd/agent/chat_film_frames.go` | **未改**（master 的 `chatFilmEvenFrame` / `evenDim` 保持原样，`filmEvenFramePNG` 不复活） |
| `cmd/agent/chat_film_gif_test.go` | 高度腿的红证（按轴点名、值与对齐都出现、对称的宽度情形、两轴对齐则**接受**）+ 单一定义钉 |
| `cmd/agent/chat_film_wechat_test.go` | 原有断言跟着改：高度腿现在是**两个产物都拒**，不再是"只有 `--wechat` 拒" |

## 5. 验证（实测，非推导）

**红证 → 绿证**（规则：每个修复带最小复现测试）。把高度腿从
`chatFilmRequirePlayable` 里**删掉**后：

```
--- FAIL: TestChatFilmRequirePlayableRefusesOddHeight (0.00s)
    chat_film_gif_test.go:531: a 500x521 animation was accepted, want a refusal on the odd height
```

恢复后同一测试 PASS。红是因"该拒的没收"（不是编译错，不是别的测试的连带失败）。

**回归**：`go test ./cmd/agent/ -run 'Film|RequirePlayable|Wechat|ParityIsOne'` → `ok`。

**Rule 31 真跑**（真参数、真回放、真终端，`peerbox`，`DISPLAY=localhost:15.0`；两次，
一次**偶数几何**、一次**奇数几何** —— 后者才是会让对齐腿有资格触发的那条路）：

```
./bin/gopi chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 \
    --film <session>/artifacts/verify-final --film-terminal xterm --cols 40 --rows 20 \
    --out <session>/artifacts/verify-final/film.gif                  → EXIT=0

./bin/gopi chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 \
    --film <session>/artifacts/verify-odd --film-terminal xterm --cols 41 --rows 21 \
    --out <session>/artifacts/verify-odd/film.gif                    → EXIT=0
```

（`<session>` = `/home/user/.gopi/sessions/01a10606-eefb-70e9-b070-2d8913cce141`；
每个目录里都有它自己那次调用的 `run.out`，读数与命令行放在一起。）

驱动逐字播报（奇数那次，12 帧同形）与**文件字节的读数**：

```
[gopi film] frame-0001.png was photographed 250x277 px (odd) and is written 250x276 px — ...
[gopi film] animation .../verify-odd/film.gif: 12 frame(s) at 25cs (4.0 frame(s)/s), 55400 byte(s)
```

| 读数 | 奇数跑 | 偶数跑 | 来源（不采信"我传了什么"） |
|---|---|---|---|
| 抓到的窗口 | **250×277**（两轴都奇） | 244×264 | 驱动播报，即 `chatFilmEvenFrame` 之前的 `SrcW`/`SrcH` |
| `frame-*.png` 像素尺寸 | **250×276**，去重后**只有一个值** | 244×264 | `struct.unpack('>II', IHDR[16:24])` 逐文件读 |
| GIF 逻辑屏 | **250×276** | 244×264 | 文件字节 `[6:10]` 小端读 |
| 两轴对齐 | 宽 % 2 = 0，高 % 2 = 0 | 同 | 同上 |
| 字节 | 55400 | 51499 | `os.path.getsize` |

**"帧与 GIF 两轴一致"是量出来的，不是推的**：磁盘上 12 个帧的 IHDR 去重后只有一个
尺寸，GIF 的逻辑屏就是这个尺寸 —— 帧写盘器与编码器不可能给出两个不同的画布。

**门禁本身在真跑里不会开火，这是设计而不是缺口**：帧写盘器先按 `chatFilmFrameParity`
裁齐，所以 `chatFilmRequirePlayable` 是**已交付文件**的后备（预存的影片目录、被改动的
裁剪路径、几何不同的编码器）。它的拒收由单元测试的红证钉住（§5 上一段），不由真跑钉。

**`--wechat` 的字节上限**这一条没有在本轮被触发（55400 ≪ 5242880），所以本轮的
真跑**不构成**对它那一条链路的验证（它由 `TestChatFilmWechatIsStrictlyStrongerThanTheDefault`
与上一轮的报告覆盖）。

## 6. 没有做的事（诚实）

- **旧分支的 `filmEvenFramePNG` / `filmFrame` 没有复活**，见 §2 —— 那是它被
  `47d60393` 取代的那一半，不是剩余。
- **宽度那半的既有缺陷未动**：本机默认终端面比驱动内置参考面宽，82 列预期 496 px
  实到 836 px，`chatFilmTooWideRefused` 会在 GIF 写完后拒收。这是**既有行为**，
  独立于本任务（前一轮 `devdoc/film-gif-wechat-switch-and-even-frames.md` 已列为未闭合栏）。
- **`wechatGIFMaxBytes` 的来源等级**仍是"面向用户的约定"而非一手规范测量
  （见 `devdoc/film-gif-wechat-switch-and-even-frames.md` §2），本轮未改。
