<!-- doc-class: normative -->
# film 的 `--wechat` 开关，以及奇数帧高（521）的根因修复

Task: `01a105d0-1d03-7c2b-934e-d467e6379ad2`（前置：`01a1054c` / commit `cc18cbe5`）
Measured at: 本分支 @ `2b9aea9a`（master）+ 本次 diff，2026-10-04。

## 0. 前置交付与本轮的缺口

`cc18cbe5`（`devdoc/film-gif-wechat-playable.md`）已经**自动**把带 `--out` 的片子收窄到
`filmPlayableMaxCols()` = 82 列 / 496 px，并在编码后**读回**卡三条：宽 ≤500、宽偶数、
Netscape 循环块（`chatFilmRequirePlayable`，`cmd/agent/chat_film_gif.go`）。

本轮补三个缺口：

1. 操作者要的**具名开关** `--wechat`（前一轮是隐式的：只要有 `--out` 就收窄）。
2. **字节上限**——一条都没查过。
3. **帧高奇偶**——前一轮只查了**宽**的奇偶，**高**完全没查；而奇数高是下游编码器的硬失败。

## 1. `--wechat`：为什么它是一个**产品属性**，不是四通道旋钮

**决定：`--wechat` 是纯产品属性（feature），不配 `GOPI_FILM_WECHAT` 环境变量、不配
settings key、不配四通道。**

理由（Rule 2/35 的「依赖预算」判据，逐条）：

- **四通道的存在意义是"同一个旋钮，不同层级的来源"** —— 例如 `--film-terminal NAME`
  有 flag/env/settings/默认 四档，因为它描述的是**环境属性**（这台机器装了什么终端），
  环境变量与 settings 是正确的载体：一台机器一个值。
- **`--wechat` 描述的是"这份产物交给谁"**，而这是**一次运行**的属性，不是一台机器的属性。
  把它做成环境变量，等于允许"某台机器上所有 gopi 对话都产出微信形状的 GIF"——
  没有任何理由让一个未言明的环境变量替操作者决定产物的读者。
- **代价（Rule 35 四问）**：一个新的 settings key 会引入一个新配置面（文档、
  `CONFIG.md` 一行、校验、`config_doc_gate_test.go` 的检查），而它解决的问题
  ——"每次都要带 `--wechat`"——在脚本里是一行 shell 变量就能解决的**调用方**问题。
  **没有得到一个可指认的失败**，所以没有挣到一个依赖。
- 所以：**一个裸开关，一个旋钮，一个来源**（命令行的操作者）。

**用法约束**：`--wechat` 要求 `--out`。裸的 `--wechat`（只拍片、不出动画）被**具名拒绝**
（`chatFilmWechatNeedsOut`）：手机收到的是那个 GIF，目录本身不是一个可交付物——
接受一个什么都不改变的词正是 Rule 15 拒绝的静默无效。

## 2. 字节上限：常量的来源，以及它的诚实等级

```go
// cmd/agent/chat_film_gif.go
wechatGIFMaxBytes = 5 * 1024 * 1024   // 5 MiB
```

**来源等级必须说清楚（诚实记录）**：这是**面向用户的约定**（微信用户自行添加表情时
"单个表情不超过 5M" 的普遍说法），**不是本仓从一手规范里量到的**。
本次在 peer1 上做了三次公开检索（`web_search`），**微信没有可检索到的公开尺寸上限规范**，
检索只返回了 arxiv 上关于 GIF 的无关论文与微信首页。因此：

- 常量注释里**写明**它是"operator-facing convention"而不是 primary-source measurement；
- 报告 `## Not Done` 里把它列成**未闭合的一栏**：操作者（有手机的那一方）可以把它
  换成他实际观测到的数字，改一个常量即可；
- **它被写成"我们强制的上限"，而不是"某个客户端在某字节数拒绝文件"的断言** ——
  这个区别是刻意的：前者是我们能负责的（超了我们就拒绝），后者我们量不到。

**超限的处理是拒绝，不是截断**（Rule 15）：`chatFilmWechatTooBigRefused` 给出
文件自己的字节数、上限、上限的 MiB 拼写、以及三个可修的方向（缩短录制 / 收窄 / 加大帧间隔）。

## 3. 奇数帧高（521）——根因在**终端字号面**，不在几何算式

### 3.1 症状与根因

`--film` 的帧 **就是 X11 窗口的原始抓图**：`chat_film.go` 的 `shootBeat` 拿
`shot.SettledGrab(...)` 的 `grab.PNG` 直接 `os.WriteFile` 成 `frame-NNNN.png`；
`internal/gifgen` 在**没有字幕**时 `res.Height = res.FrameHeight + 0`
（`gifgen.go:468`），所以 **GIF 的高度 = 被拍窗口的像素高**。

而窗口像素高 = `rows × 终端字号的格高 + 边框`，**格高是小数**：

| 被拍终端 | 字号面 | 82×26 的窗口 | 奇偶 |
|---|---|---|---|
| `--film-terminal xterm` | 内置 fixed 6×13 | **496×342** | 偶 ✓ |
| 默认（`xfce4-terminal`） | ≈6.6×19.88 | **836×521** | **奇 ✗** |

**同样的 82 列 26 行，两个终端给出不同的像素高**，而 521 是**奇数**。
奇数高不是审美问题：所有在 **yuv420p** 下工作的视频编码器按 2×2 下采样色度，
**直接拒绝奇数帧尺寸**——片子根本编不出来（这正是操作者报的"ffmpeg 转 mp4 失败"）。

**这不是几何算式写错了**：`chatRefCellH=13` 算 26 行是 342，**算式是对的**——
错的是"窗口的真实像素高"在另一张字号面下的取整结果。
前一轮只处理了宽度这一半（减格数），高度这一半当时无人过问。

### 3.2 修法：在**写帧那一刻**裁到偶数（无损，不重采样）

新文件 `cmd/agent/chat_film_frames.go`：`chatFilmEvenFrame(raw []byte)` 解出 PNG，
把宽高各向下取到偶数，用 `draw.Draw(..., draw.Src)` **逐像素拷贝保留区**，
再 PNG 编码。**最多去掉右边 1 列、下边 1 行**。

为什么是"裁"而不是"缩放"：保留的每一个像素**逐位不变**——没有插值、没有缩放核、
没有第二个渲染器，所以帧的像素**仍然是真实窗口的像素**（这正是这个 film 存在的理由，
`devdoc/chat-film-pixels-are-the-surface.md`）。缩放到偶数尺寸会**重新引入**宽度那一半
契约刻意避开的东西。

**在参考面上它是空操作**：xterm 的 496×342 已经是偶数，`chatFilmEvenFrame` 返回
**原始的 `raw` 切片本身**（不是"看起来一样"的重编码），所以**默认产物逐字节不变**——
这条由 `TestChatFilmEvenFrameIsANoOpOnEvenFrames` 钉住，它额外断言"这个 fixture 走一遍
`png.Encode` 会得到不同的字节"，从而让那个相等**是空操作的证据**，而不是"两个编码器恰好一致"。

配套：证据行 `bytes` 列现在记**写下去的**帧字节数（`len(frame.Out)`），不是抓图字节数。

### 3.3 实测 before/after（**同一台机器、同一个录制、同一组参数**）

before 用前置任务**未修**的二进制 `/home/user/.gopi/sessions/01a1054c-.../gopi-wx`：

```bash
# 参数完全相同：--cols 40 --rows 20（显式，故不参与收窄），默认终端
$OLD chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 --film DIR --cols 40 --rows 20 --out DIR/wx.gif
$NEW chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 --film DIR --cols 40 --rows 20 --out DIR/wx.gif --wechat
```

| | 窗口（驱动播报） | `frame-0001.png` | **GIF 逻辑屏** | 字节 |
|---|---|---|---|---|
| **pre-fix**（旧二进制） | `area 416x407+0+0` | **416×407** | **416×407**（静默交付） | 211936 |
| **post-fix**（本次） | `area 416x407+0+0` | **416×406** | **416×406** | 211783 |

post-fix 时驱动**逐帧播报**（Rule 15，尺寸变化绝不静默）：

```
[gopi film] frame-0001.png was photographed 416x407 px (odd) and is written 416x406 px —
            the last column/row is removed so both dimensions are even, which an encoder that
            subsamples chroma requires; the surviving pixels are the display's own, un-resampled
```

**奇数高这条路现在走不通了**：帧在落盘那一刻就已经是偶数，窗口怎么取整都不能再产出奇数帧。

### 3.4 闸门也补上了高这一半

交付闸门 `chatFilmRequirePlayable(res, wechat)` 在 `--wechat` 下**追加**两条拒绝：

- `res.Height % chatFilmFrameParity != 0` → `chatFilmOddHeightRefused`（说明为什么奇数是致命的：色度下采样是结构性拒绝）；
- `res.Bytes > wechatGIFMaxBytes` → `chatFilmWechatTooBigRefused`（给数字）。

**为什么闸门也要查高度**（不只是修好帧源）：一个**先前已经拍好**的 film 目录
（或任何从别处来的帧）仍然会走这个合成缝，闸门是那条路径上的唯一拦截点。

## 4. 默认产物不变的证明（两层）

1. **缝上**：`TestChatFilmDefaultProductIsTheEncodersOwnOutput` 断言
   `chatFilmComposeGIF(out, frames, false)` 的产物与 `gifgen.Encode(...)` 用**同样的选项**
   直接产出的字节**完全相同**——film 臂是编码器**之上的闸门**，不是第二个编码器。
   这条测试会在任何"顺手改写帧"的改动下变红（缩放、二次 pass、调色板改动）。
2. **端到端**：`--wechat` 与不带开关的两次真实运行，尺寸/帧数/结构一致，字节在同一台机器上
   相差 126 B（111576 vs 111450）——差异来自**录制/终端自身的重绘抖动**，不是形状差异。
   同参数同二进制的字节精确对比见 §3.3（211936 pre-fix 那次的 416×407 是**同一次**几何）。

## 5. 实跑（Rule 31）

```bash
$BIN chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 \
     --film "$D" --film-terminal xterm --out "$D/wx.gif" --wechat
```

驱动的逐字播报（节选）：

```
[gopi film] wechat: .../wx.gif is 496x342 px, even in both dimensions, 111450 byte(s) of 5242880 allowed,
             Netscape loop block read back forever(0) — 12 frame(s)
EXIT=0
```

产物读数（**字节级 + 独立解码器**，都不是"我传了什么"）：

```
$ stat -c %s wx.gif                  → 111450
$ od -A d -t u1 -j 6 -N 4 wx.gif     → 240 1 86 1   = 0x01f0 × 0x0156 = 496×342
$ grep -abo NETSCAPE2.0 wx.gif       → 40:NETSCAPE2.0
$ python3 -c "from PIL import Image;im=Image.open('wx.gif');print(im.size,im.n_frames,im.info['loop'])"
                                     → (496, 342) 12 0
```

**496 ≤ 500 ✓ / 496 偶 ✓ / 342 偶 ✓ / NETSCAPE2.0 在位（偏移 40）✓ / loop 读回 0 ✓ / 111450 B < 5 MiB ✓**

## 6. 验证配方（可复用）

```bash
BIN=./bin/gopi; D=$(mktemp -d)   # 或 session 目录（Rule 39：证据不进树）
$BIN chat --replay <短session> --film "$D" --film-terminal xterm --out "$D/wx.gif" --wechat; echo $?
stat -c %s "$D/wx.gif"                                   # 字节
od -A d -t u1 -j 6 -N 4 "$D/wx.gif"                      # [w_lo w_hi h_lo h_hi] LE = 逻辑屏
grep -abo NETSCAPE2.0 "$D/wx.gif"                        # 循环块在文件里的偏移
python3 -c "from PIL import Image;im=Image.open('$D/wx.gif');print(im.size,im.n_frames,im.info['loop'])"
bash scripts/replay-check.sh                             # 期望 0 stale
```

**坑**：`--film-terminal` 的默认是 `xfce4-terminal`，它的字号面比 xterm 宽且**行高是小数**
→ 同样的格数给出不同的像素尺寸，且可能出**奇数高**。要复现"≤500"必须钉 `--film-terminal xterm`
（或者干脆用 `--wechat`：奇数高这条路已经被 §3 的帧源修复封死）。

## 7. 本次没有做（Not Done）

- **手机端"真的能播"未由我验证**：我没有手机。这是操作者的一栏（技术证据我出）。
- **字节上限的数字权威性**：5 MiB 是面向用户的约定，不是一手规范（§2）；本次三次公开检索
  都没找到微信的公开尺寸/大小规范。
- **`--film-terminal` 默认终端的字号面与参考面不一致**（804/836px 走拒绝路径而不是自动收窄）：
  与前置任务同一条边界，未变。
- **`gopi gif` 那条 verb 不加微信契约**：它的调用者显式给了帧文件，尺寸是调用者自己的事。
- **默认终端下 82 列的宽度仍超限**（xfce4 ≈836px）：需要"按面反推格数"，即把终端字体复制进
  gopi（Rule 35 意义上的新依赖），超出本次预算。
