<!-- doc-class: normative -->
# film 的 GIF 要能被手机微信播放：宽度契约与 MP4 的 B3 判断

Task: `01a1054c-64f3-70d9-b8e4-f5c7e2cb4342`
Measured at: `73d31162` (base) + this branch's diff, 2026-10-04.

## 0. 症状

`gopi chat --replay S --film DIR --out FILE.gif` 的产物**在手机微信里播不了**。
实测（`/tmp/cnrun-1.log` 的那次运行）：

| 读数 | 值 | 来源 |
|---|---|---|
| GIF 逻辑屏 | **1186×521** | `od -j6 -N4` → `162 4 9 2` = `0x04a2 × 0x0209` = 1186×521 |
| 帧数 | 151 | `grep -c $'\x21\xf9\x04'` / PIL `n_frames` |
| 字节 | 3,713,383 | `stat -c %s` |
| loop 读回 | **0（forever，块在位）** | PIL `info['loop']==0`；`grep -abo NETSCAPE2.0` → 偏移 208 |

所以**循环块这一半本来就是好的**（`image/gif` 对多帧流会写它），
真正越界的是**宽度 1186 ≫ 500**。

## 1. 决策：收窄**格子数**，而不是把画面缩放出小尺寸

### 1.1 判据来自这个表面自己的可读性定义，不是审美

这个表面的可读性判据是 **px-per-char**（交付宽度 ÷ 终端列数，地板 16；任务 `01a0f0de`，
记忆 #7013）。film 的字号由**终端面**决定（本机 xterm 内置 fixed 6×13），
布局按**格数**排版。两种做法的后果不同：

- **(a) 把 1186px 重采样到 496px**：字形被压到 0.42 倍，**低于这个表面测量过的话地板**；
  而且同一份实测给出反直觉结论：**文本素材下采样后体积反而更大**
  （同 41 帧：2284px = 3,546,990 B vs 2056px = 6,139,157 B）——
  重采样在字形边缘引入中间色，调色板成本 > 省下的像素。
  最致命的一条：GIF 里的像素**不再是真实窗口的像素**，而"帧的像素来自真实表面"
  正是这个 film 存在的理由（`devdoc/chat-film-pixels-are-the-surface.md`）。
- **(b) 把格数从 117 收到 82**：表面**真的**以 82 列重新排版，字号不变，
  像素仍然来自真实窗口，宽度由公式**构造性**地落在上限内。

**选 (b)。** 代价诚实记录：**换行位置会变** —— 82 列的排版不是 117 列的排版。
这是"同一份内容的另一个真实布局"，而不是"同一张图的模糊版"。

### 1.2 上限是**推导**出来的，不是第二个字面量

```
playableGIFMaxWidth  = 500                       // 读者（手机会话视图）的约束
playableGIFWidthParity = 2                       // 宽度取偶
filmFrameWidthPx(cols) = cols*chatRefCellW + chatRefBorderPx   // 复用 help 页的像素表推导
filmPlayableMaxCols()  = (500-4)/6 = 82          // → 帧宽 496 px
```

`chatRefCellW`(6) / `chatRefCellH`(13) / `chatRefBorderPx`(4) 是**既有常量**
（`cmd/agent/chat_replay_help.go`，help 页的像素表就是它们渲染的），
本改动**引用**它们（Rules 13/14），没有写第二份字号假设 ——
`scripts/replay-check.sh` 的 C11c/C11d（`706x342px` / `604x394px`）**仍然全绿**，正是这一点的证据。

## 2. 三层闸门，都在**生成端**

| 位置 | 做什么 | 不满足时 |
|---|---|---|
| `chatFilmPlayableGeometry`（`chat_film.go`，**spawn producer 之前**） | 把**解析出的**画布（recording 的 / ideal 的）收窄到 ≤82 列 | **打印**解析值→拍摄值+两个像素宽+原因（Rule 15，尺寸变化绝不静默） |
| `chatFilmRequirePlayable`（`chat_film_gif.go`，编码之后） | 用**读回值**卡三条：宽 ≤500、宽偶数、`LoopCount`/`LoopReadBack` 均为 0 | **拒绝**，并给出数字与可修的 flag |
| `composeGIF` 的既有 ledger 校验 | GIF 帧数 == ledger 行数 | 既有行为，未改 |

**循环块的检查是"两个值都为 0"，不是"写的时候传了 0"**：
`LoopCount` 是请求值，`LoopReadBack` 是**文件里读回来的**；
单帧流 Go 不写 Netscape 块（读回 -1），那种"能生成但只播一遍"的文件会被拒绝而不是被报告成完成。

**显式声明的尺寸不参与收窄**：`--cols/--rows` 是操作者在说"这一片就这么大"，
本改动只收窄**驱动自己解析出来的**画布；一个仍然超限的显式画布会被**拒绝**（给出数字），
而不是被悄悄改小。

## 3. 实跑（Rule 31）

```bash
D=/tmp/wxfilmx-<pid>
$BIN chat --replay ddbde97c-965a-41f2-a690-9c6f1bb91106 \
     --film "$D" --film-terminal xterm --out "$D/wechat.gif"
```

驱动自己的播报（逐字）：

```
[gopi film] shooting at the recording's own terminal size 117x26 (no --cols/--rows was given on this command line)
[gopi film] the resolved canvas is 117x26 (706 px wide, over the 500 px a phone conversation view accepts),
            so this film is shot at 82x26 (496 px wide) instead — the surface is RE-LAID OUT at the narrower
            width rather than resampled down, which keeps the glyphs at the size the layout measured them for
[gopi film] terminal window 2097164 ("gopi film wxfilmx-3405680") area 496x342+0+0, expected 496x342px for the 82x26 cells it was asked for
[gopi film] film written to /tmp/wxfilmx-3405680: 12 frame(s) from 12 ledger row(s), 12 frame(s) reached the settled criterion, 0 did not
[gopi film] animation /tmp/wxfilmx-3405680/wechat.gif: 12 frame(s) at 25cs (4.0 frame(s)/s), 111413 byte(s) — 12 ledger row(s), 12 frame(s) photographed
EXIT=0
```

产物读数（字节级 + PIL）：

```
$ stat -c %s wechat.gif              → 111413
$ od -A d -t x1 -N 14 wechat.gif     → 47 49 46 38 39 61 | f0 01 | 56 01 | 82 00 00 (13…)
                                         "GIF89a"        496      342      GCT 256 色
$ od -A d -t u1 -j 6 -N 4            → 240 1 86 1        = 0x01f0 × 0x0156 = 496×342
$ grep -abo NETSCAPE2.0 wechat.gif   → 40:NETSCAPE2.0
$ od -j 34 -N 20                     → 00 00 00 21 ff 0b "NETSCAPE2.0" 03 01 00
                                         (0x21 0xff 0x0b = 应用扩展；03 01 = 子块；00 00 = 永不到头)
$ python3 -c "from PIL import Image; im=Image.open('wechat.gif'); print(im.size, im.n_frames, im.info['loop'])"
                                     → (496, 342) 12 0
```

**496 ≤ 500 ✓，496 偶数 ✓，NETSCAPE2.0 在位 ✓，loop 读回 0（永远循环）✓。**

## 4. 实测到的**边界**（诚实记录，未修）

**像素宽度取决于被拍摄终端自己的字号**，而不是格数。

- `--film-terminal xterm`（内置 fixed 6×13）：82 列 → 496 px ✓
- 默认终端 `xfce4-terminal`（本机面 ≈6.6×14.2）：同样的 82 列 → **836 px** ✗

后一种情况下运行**失败并拒绝**，而不是产出一个播不了的 GIF：

```
[gopi film] compose the animation …/wechat.gif: …/wechat.gif is 836 px wide, over the 500 px limit
            a phone conversation view accepts — this film was shot at a cell count whose frame is too
            wide, so no playable animation can be written from it (shoot it narrower: --cols/--rows)
EXIT=1
```

这正是 `shot.FilmTerminalSpec` 里已经写下的老事实（"a terminal with a different face gives
different pixels for the same cells"）。**本次没有把这条收紧成"自动按面收窄"，理由：**

- 面宽是在窗口 map 之后才测量到的，而**收窄必须发生在 producer 起之前**
  （否则就是"换个格子数重拍"）；
- 按面反推格数需要一个"面宽"的配置来源，而那是**终端自己的属性**，
  在 gopi 里加一个 `px/cell` 旋钮等于把终端的字体复制一份进来（Rule 35 意义上的新依赖）。
- 因此本改动选择：**能构造性地满足就用 reference face（xterm），不能满足就明确拒绝并给出数字**，
  绝不静默产出一个播不了的交付物。留在 `## Not Done`。

同时，**这条限制现在可见了**：驱动在起终端时就把"格数 → 期望帧尺寸"打出来，
所以 836 与 496 的差异不必等到合成阶段才发现。

## 5. (B) MP4：**B3 —— 不出 MP4**（理由）

### 5.1 (B1) 纯 Go 自研 H.264/MP4：不做

缺的是**视频编解码器**，不是容器。容器（mp4 的 box 结构）自己写是几百行，
但手机要的是 **H.264 + `yuv420p`** —— 操作者自己那条能播的 `ffmpeg -c:v libx264 -pix_fmt yuv420p`
就是这点的证明。纯 Go 的 H.264 **编码器**是一个必须逐位正确的巨大工程，
远超本仓任何一个组件。**本仓不应自研视频编解码器。**

### 5.2 (B2) 引入外部依赖 / 运行时探测 ffmpeg：**否决**（Rule 35 四问逐条）

1. **没有它会坏什么（具体失败）** —— 能指出的失败只有"操作者拿不到 MP4"。
   而操作者的**硬要求是"手机微信能播"**，这条由 (A) 独立满足（§3 的 496px + 循环块产物）。
   **没有观察到"只有 MP4 才能解决"的失败**。得不到一个可指认的失败，就没有挣到一个依赖。
2. **为什么不能在已有进程内解决** —— 能：(A) 已经在 gopi 自己的进程里零依赖地解决了播放问题。
   MP4 属于"想要另一种投递格式"，不是"现有能力解决不了的失败"。
3. **谁拥有它的生命周期、怎么回收** —— **没有人**。运行时探测到的 ffmpeg 是一只**看不见的手**：
   它的版本、参数、在不在机器上全由环境决定，gopi 既不能回收它的行为，也不能承诺它的输出
   —— 这正是 Rule 35 里 `hp-worker-autostart` 那个 cron 的形状（用外部东西把问题盖住）。
4. **下一个发现它的人看到什么** —— 探测式实现的典型产物就是"文件生成了但内容是错的/0 字节"，
   **而这正是操作者本次遇到的事**：`/tmp/cn-result.mp4` = **0 字节**
   （`stat -c %s` 读数，ffmpeg 当时没有给出显眼报错）。
   一条会静默产出 0 字节产物的路径，比没有这条路径更糟。

**结论（briefing 要求的逐字说明）**：**操作者要 MP4，但本仓已明确否决外部编码器依赖；
微信播放问题由 (A) 独立解决。**

若操作者仍要 MP4，**零依赖边界之内**的诚实做法是：用 film **已经写出**的无损真值帧
（`<film>/frame-*.png`），在 gopi **之外**用他自己已有的编码器转一次：

```bash
# 文档，不是依赖：这条命令不在 gopi 的任何代码路径里，也不会被执行
ffmpeg -y -framerate 4 -i '<film>/frame-%04d.png' -movflags +faststart \
       -pix_fmt yuv420p -vf 'scale=500:-2' -c:v libx264 -crf 22 out.mp4
```

注意最后一行是**人**执行的：gopi 不探测它、不调用它、也不假设它存在。

## 6. 验证配方（可复用）

```bash
# 1) 真跑（短 session，避开 01a103f4 的 BadDrawable race）
$BIN chat --replay <短session> --film "$D" --film-terminal xterm --out "$D/wechat.gif"; echo $?
# 2) 字节级：逻辑屏尺寸 + 循环块
od -A d -t u1 -j 6 -N 4 "$D/wechat.gif"          # [w_lo w_hi h_lo h_hi]，LE
grep -abo NETSCAPE2.0 "$D/wechat.gif"            # 必须有命中，偏移就是块的位置
# 3) 独立解码器（PIL）复核
python3 -c "from PIL import Image;im=Image.open('$D/wechat.gif');print(im.size,im.n_frames,im.info['loop'])"
# 4) 红线锚
bash scripts/replay-check.sh                     # 期望：0 stale
```

**坑**：`--film-terminal` 的**默认是 `xfce4-terminal`**，它的字号面与 xterm 不同 ——
同一格数会出**不同的像素宽**。要复现 496 就必须钉 `--film-terminal xterm`（或者自己算面）。

## 7. 本改动没有做的（Not Done）

- **按终端面自动反推格数**（§4）：需要在 gopi 里引入"px/cell"配置或"重拍"机制，
  两者都超出本次预算，且后者是另一个流程。当前是**明确拒绝**。
- **`gopi gif`（静态帧序列那条 verb）不加同样的宽度契约**：它的调用者显式给了帧文件，
  尺寸是调用者自己的事；本次的门是 **film 的产物**。
  —— **2026-10-11 更新（§9）**：现在它会**警告**（不拒绝）：产物宽度 > `playableGIFMaxWidth`
  时把一句警告挂进 `Warnings`（文本走 stderr，`--json` 进 `warnings`），文件照常写出。
  它仍然是"不加契约"（不拒绝），但那句提示不再缺席。
- **手机端"真的能播"未由我验证**：我没有手机，这一栏留给操作者。
- **`01a103f4`（BadDrawable race）未修**：本次用短 session（12 拍）规避。

## 8. 真机读数（2026-10-11，操作者的手机）—— 500 这个值的第一份**真机**证据

§0 的 1186×521 是"被拒绝"的读数，它只说上限**在 1186 以下**，没有说线在哪里。
下面这张表是第一次**在真机上**同时拿到"能播"和"不能播"两个样本（操作者经微信**相册路径**发送）：

| 尺寸(px) | 帧数 | 字节 | 真机结果 |
|---|---|---|---|
| 494×502 | 20 | 495,163 | ✅ **能播** |
| 634×520 | 12 | 322,242 | ❌ 不能播 |
| 714×520 | — | — | ❌ 不能播 |
| 954×520 | — | — | ❌ 不能播 |
| 1134×502 | — | — | ❌ 不能播 |

**对照是干净的，而且指向宽度**：不能播的 634 样本在**字节数上更小**（322,242 < 495,163）、
在**帧数上更少**（12 < 20），唯一比能播样本多出来的维度就是**宽度**（多 140 px）。
所以变量是**宽度**，不是体积、不是帧数 —— 收窄宽度就播。
这同时排除了循环块 / 文件头 / 发送路径三种猜测（§0 已逐条排除过）。

**因此 `playableGIFMaxWidth` 保持 500，不上调**：读数只把线**夹在 (494, 634]** 这个区间里，
本仓没有任何人测过区间内部（**Not Done**，见下）。500 是"被读数支持"的那个数：
494 在门内且能播，634 在门外且不能播。

**不开一次新实验就能用上这份读数的诊断法**：一个播不了的动画，**先看宽度** ——
`od -j6 -N4 file.gif` 读逻辑屏宽度，超过 500 px 就是答案。
（这份读数花掉了真机、一个晚上和数次往返；下次不必重跑。）

### §8 的 Not Done（诚实记录）

- **500~634 px 之间的断点未测**：没有任何真机样本落在 (494, 634) 内，
  所以本仓**不推断** 501 / 550 / 600 哪个能播。要收紧就必须再做一次真机实验，
  而当前闸门用 500 是**保守**方向（宁可错杀、不放过），无需先测断点。
- **能播/不能播的网络与微信版本环境未记录**：样本来自操作者的一次发送，
  微信版本、网络、发送路径（相册）之外的分支未逐个取证。

## 9. `gopi tool gif` 这条事后合成路径：只警告，不拒绝

§7 曾把"`gopi tool gif` 不加宽度契约"记为 Not Done。本次补上的是**警告**（不是拒绝）：
`cmd/agent/cli_gif.go` 在编码之后，若产物宽度 > `playableGIFMaxWidth`（**引用同一个常量**，
Rule 13/14，不写第二个 500），就把一句警告挂进 `gifgen.Result.Warnings`——
文本模式走 stderr，`--json` 进 `warnings` 数组。**文件照常写出**：这条 verb 是通用合成器，
画布由调用者的帧决定，为别的读者（报告 / 编辑器 / 浏览器）合成的调用者不该被它挡住；
而 film 那条路径**拥有自己的帧**，所以它仍然是**拒绝**（§2 的中层闸门）。
差别是"能不能重拍"，不是"哪条更严格"。

取证（Rule 31，实跑）：

```
$ ./bin/gopi tool gif --frames f1134_0.png,f1134_1.png --out wide-1134.gif        # 1134 px 帧
✅ 2 帧 → wide-1134.gif  1134x160 … 字节=2045  ⚠️ 1 条警告
gopi gif: ⚠️ wide-1134.gif is 1134 px wide (160 px tall) — OVER the 500 px ceiling …    (stderr)
exit=0，文件在盘上（2045 B）

$ ./bin/gopi tool gif --frames f494_0.png,f494_1.png --out ok-494.gif            # 494 px 帧
✅ 2 帧 → ok-494.gif  494x160 … 字节=1417      （无警告，exit=0）

边界：501 px → 警告；500 px → 不警告（警告是 `> 500`，不是 `>= 500`）。
```

