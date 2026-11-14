<!-- doc-class: normative -->
# devdoc — 演示材料的「动图」表达层：一条真跑通的 gif 生产线 + 可核对性怎么补

**一句话结论：能用。** 零安装路线（Xvfb + xterm + ImageMagick + gifsicle）能在本机把一段
**真实 gopi 终端会话**做成 **63834 字节 / 3.20 秒 / 单文件** 的 gif，并且它的文字可以
**逐字符格**证明就是原始 stdout——不需要相信任何 OCR。

产物：`docs/archive/demo/materials/001-notify-cmd-default-off/06-gif.gif`

---

## 1. 选型：两条路线，一条能跑

| 路线 | 存什么 | 可核对性 | 本机 |
|---|---|---|---|
| **A. Xvfb + xterm + `import` + ImageMagick + gifsicle** | 像素 | 需自己造校验器 | ✅ 全部已装 |
| B. asciinema + agg | **文本流**（`.cast`，JSON） | 天然更好（文本是一等公民） | ❌ 都没装 |

本机实测：`asciinema` / `agg` / `gifski` 不在；`ffmpeg` **没有 x11grab 设备**
（`ffmpeg -devices` 里 `gdigrab`/`x11grab` 均缺），所以「ffmpeg 抓屏拼帧」这条路在本机**不可用**——
不是配置问题，是设备没编进去。ffmpeg 仍可用于别的转码，但本配方没用它。

**为什么选 A 而不是先装 B**：装 `agg`（Rust 静态二进制）与 `asciinema`（pip）是**新增持久依赖**，
按 Rule 35 要回答四个问题（没有它会坏什么 / 为什么不能在本进程内解决 / 谁负责它的生命周期与回收 /
下一个发现它的人看到什么）。对一次 POC，这四个问题一个都答不上来——所以**在约束内先做**：
A 路线的工具全在，做出来的东西同样可核对，代价是校验器要自己造（§3）。

**路线选择对可核对性的真实影响**：B 保存的是**原始文本流**，gif 只是它的一次渲染，
所以「gif 里的字 = 真实输出」在 B 里是**结构保证**；A 只留下像素，这个保证**必须补**。
本次的补法见 §3——补上之后，A 的判定强度不低于 B（甚至更强：B 无法证明**渲染**没出错，
A 的逐格像素比对能）。

---

## 2. 生产线（`scripts/materials/materials-gen-gif.sh`）

```
真实 gopi 命令 ──► xterm（Xvfb 上）──► 每 4s 抓一帧 import ──► convert 装帧 ──► gifsicle 优化 ──► 一个 .gif
        │
        └── 同一个字节流 tee 到 06-gif-raw.txt（复核基准 = 终端上看到的那一份）
```

会话内容（逐字，就是 06-gif-raw.txt）：`task list --status pending` → `task update … --status in_progress`
→ `task list --status in_progress` → `task show …`，每条后面跟它真实的 `exit=`。
**gopi 的输出一个字节都没改**；脚本只负责「把它画到屏幕上、拍下来」。

### 2.1 四个实测踩到的坑（都写进了脚本的 refuse 逻辑）

1. **窗口比虚拟屏宽** → `import -window root` 的 `-crop` 会绕出屏幕边缘，抓回**灰底垃圾帧**。
   实测：1714 px 的窗口放进 1400 px 的屏，帧 1–3 的背景是 `(64,64,64)` 而不是黑。
   脚本现在断言 `x+w ≤ root_w && y+h ≤ root_h`，否则 refuse。
2. **窗口行数 < 会话行数** → 顶行滚出屏幕，**那一行不在 gif 里**，
   「整帧 vs 整份原始输出」的比对当场失去意义。实测：36 行窗口 vs 37 行输出。
   校验器现在直接拒收（不是警告）；生成器把行数开到 44。
3. **`xdotool search --name X` 找不到 `xterm -name X`**：`-name` 设的是 WM instance name，
   xdotool 匹配的是**窗口标题**，要用 `xterm -T X`。找不到时它不报错，而是把空串/`--help` 文本
   往下传（`xdotool getwindowgeometry --shell ""`），错误出现在很后面。
4. **`pkill -f "Xvfb :88"` 会自匹配调用它的那条 bash 命令行**，把自己的 shell 杀掉（Rule 24 的 sibling trap），
   于是后面每条命令都「莫名其妙」失败；用 `pkill -x Xvfb`。同理 `kill %1` 杀的是 job 1，
   不一定是刚起的那个进程——本脚本第一版就因此杀了 Xvfb。

### 2.2 演示必须不依赖启动它的 shell（第九次 run 才干净）

第 7 次 run 的画面里出现了：

```
[task-notify] cmd "cat >> /home/user/.gopi/notify/task-events.jsonl": exit code 2
  (stderr: sh: 1: cannot create …: Directory nonexistent)
```

**这是真实的 gopi 输出，但是错误的演示**：它来自调用方环境（以及沙箱 settings 是操作者
真实 settings 的**副本**，带着 `notify.*`）。两个修法都用了：

- 脚本先把自己 **re-exec 到一个把所有 `GOPI_*` 摘掉**的环境里（`GOPI_GIF_ENV_SCRUBBED` 防重入）；
- `GOPI_SETTINGS` 指向一份**去掉了 `notify.*`** 的设置副本（本库 001 条目的既有先例
  `settings-off.json`；`GOPI_SETTINGS` 是「一个值」，不是往 profile 里写，profile 的
  `settings.json` 一个字节没动）。

另外：**沙盒 profile 每次 run 唯一**（复用会累积台账行，画面就不确定了——实测复用时
开了 3 条 `in_progress` 行，最长行 224 字符直接溢出窗口）。回收交给现有的
`scratch-gc sandboxes`（72h 年龄，Rule 34），脚本里**不写清理钩子**。

---

## 3. 可核对性：**不信 OCR，回到像素**（`tools/gif-verify.py`）

这是本次交付的核心。拆帧后对最后一帧（内容全部落地的那一帧）做两项**互相独立**的检查：

### (A) 逐字符格双射（字体无关）

把帧切成 `cols × lines` 的字符格，每格取二值墨迹补丁，按补丁内容聚类，然后要求
**屏幕上每个字符位置画的，就是原始文本里那个字符**：

- 不同字符 → 必须是**不同**的形状（「两个字长成一个样」是 OCR 最容易被骗、也最像造假的失败模式）；
- 同一字符 → 只许有**一个**形状；
- 文本有字的格 → 必须有墨；文本是空格的格 → 不许有墨。

帧是**窗口截图**，文本原点未知，所以先在 ±1 格内搜索偏移、取违反最少的那个作为标定，
并且**同时报出 naive 原点与四个相邻格的违反数**——最优必须是尖峰，平台说明标定无意义、
结论不能建立在它上面。（这一步是被数据逼出来的：第一版用 naive 原点，报 76 处违反，
看着像 gif 有问题；真实原因是 1 像素的取格原点偏差。）

实测（本 gif 的最后一帧）：

```
标定原点 dx=1 dy=1 px (+0.11 列 / +0.05 行)
该原点违反 = 0 / 1365 个有墨格     ← 双射成立
naive 原点违反 = 64                ← 标定不做，结论就是假的
相邻格偏移违反 = 385 ~ 1555        ← 尖峰，不是平台
```

### (B) 逐像素

用同一字体把**同一份文本逐行渲染进等高的条带**再拼成期望图，在标定偏移下求墨迹 IoU。

- 为什么**逐行渲染**而不是一次 `label:@file`：整幅多行 label 会**累积行距漂移**——
  实测 magick 的自然行距是 **16.5 px**，而 xterm 的格高是 **19 px**，30 行漂 15 px。
  那是**渲染器的假象**，不是 capture 的缺陷，不许污染结论。
- 实测（本仓已提交的那张 gif）：**IoU = 0.9507**（期望墨 52444 px / 实拍 51221 px；
  只期望有 1921 px、只实拍有 698 px、两者都有 50523 px = **差异墨迹的 95.1%** 两边一致）。

### (C) vision 读帧 = 第三方旁证，**不是**裁决

`vision_analyze` 读同一帧：33 行内容里 **31 行与原始 stdout 逐字相同**（uuid、时间戳、`exit=0` 全对）。
余下差异**全部是读图模型自己的**，`tools/vision-diff.py` 回到 gif 的像素上逐格判给了 baseline：

| reader 读成了 | baseline 实际是 | 像素裁决 |
|---|---|---|
| `,` ×2 | `;` | 该格补丁 == `;` 在别处的补丁 ✓ |
| `0` | `9` | ✓ |
| `-` ×2 | `—` | ✓ |
| `_` | `.` | ✓ |
| 空字段上凭空一个 `-` | （无字符） | 那些格实测**是空的** ✓ |

**12 处字符争议，全部判给 baseline；没有一处是 gif 的。**

裁决脚本自己也有两个坑（都实测踩到）：**行号必须用保留空行的原始行号**，否则帧行号与文本行号错位
（第一版把 31 行压成 29 行，报出一堆「空行差异」）；**列号必须用 baseline 的原始列号**，
不能先折叠空白再比——对齐漂移会伪装成 93 处「字符差异」，而那是排版不是字符。

---

## 4. 体积与时长

| 指标 | 实测 |
|---|---|
| 文件 | `docs/archive/demo/materials/001-notify-cmd-default-off/06-gif.gif` |
| `file` | `GIF image data, version 89a, 1678 x 840` |
| 字节 | **63834** |
| 帧 | 5（`disposal asis`，`transparent` 增量帧，`--optimize=3`） |
| 每帧延迟 | 400/800/400/400/1200 ms |
| 总时长 | **3200 ms = 3.20 s**，`loop forever` |
| 调色板 | 全局 64 色 |

约 12.8 KB/帧。终端内容大片同色，gif 的 LZW 压得很狠；`--colors 16` 还能更小，代价是抗锯齿变粗。
**时长不是靠 gopi 多打印东西堆出来的**——要更长就多抓几帧或拉长 `INTERVAL`。

---

## 5. 零依赖声明（Rule 35 四问）

本配方**没有新增任何持久依赖**：没有 cron、没有 daemon、没有状态文件、没有新数据库、
没有 `~/.bashrc` 之类的主机级改动。用到的全部是**本机已装**的命令
（Xvfb / xterm / xdotool / import / convert / gifsicle），生命周期都是「随本次命令起、随本次命令灭」。
唯一**新建的持久物**是本 run 的沙盒 profile——它是 Rule 34 规定的标准形态，
owner 写在 `.sandbox-owner.json`，由**现有的** `scratch-gc sandboxes` 按年龄回收，
下一个发现它的人能读到它是什么、是谁的、什么时候会被收走。

`scripts/materials/materials-gen-gif.sh` 与 `tools/*.py` 都是**一次性调用的命令**，
不是自调度的常驻物：它们不注册任何东西、不写在任何人启动时跑的位置。

四问（若将来有人想把它变成常驻）：**没有它会坏什么**——现在没有观察到任何「需要它常驻」的失败；
**为什么不能在本进程内解决**——能，本来就是；**谁负责回收**——无人，所以不做；
**下一个看到它的人读到什么**——因此不存在这样的东西。

---

## 6. 复核怎么跑（逐字）

```bash
cd <repo> && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
bash scripts/materials/materials-gen-gif.sh docs/archive/demo/materials/001-notify-cmd-default-off
python3 tools/gif-verify.py \
    docs/archive/demo/materials/001-notify-cmd-default-off/06-gif.gif \
    docs/archive/demo/materials/001-notify-cmd-default-off/06-gif-raw.txt 186 9 19
python3 tools/vision-diff.py \
    docs/archive/demo/materials/001-notify-cmd-default-off/06-gif-raw.txt <vision 读出的文本> \
    <拆出的最后一帧.png> 186 9 19 1 1
```

判定口径：`gif-verify.py` 退出码非 0 = 不通过；生成脚本在写出前**自己先跑一次**
`gif-verify.py`，不通过就**拒绝交付**（实测触发过一次：窗口 36 行 < 输出 37 行）。

---

## 7. `## Not Done`（诚实清单）

- **只做了 Route A**。Route B（asciinema + agg）**没有跑**，本机没有这两个工具，
  也没装（装了就是新增持久依赖，见 §5）。所以「B 比 A 好」这句是**推理**，不是本 run 的实测。
- **没有横向对比静态 PNG 的体积**（03-raw.png 222931 B / 04-annotated.png 223678 B 是路径压
  scrot 图，和 gif 的 1678×840 不是同一台账）。gif 反而**更小**（63828 B），但这不构成
  「gif 取代静态图」的论据——静态图承担的是**标注解释**，gif 承担的是「在跑」，
  两者不互替（本库 README 的 `annotation_required` 判据就是这么定的）。
- **帧率是 4 帧/秒（抓拍），不是真录屏**。终端内容更新快于抓拍节奏时会丢中间态：
  本会话每条命令之间停 2–5 s，所以没有丢；**换一个输出更密的会话就会丢**。
  要真逐帧，得走 Route B，或者自己写 xterm 的 VT 输出流录制（没做）。
- **没有做 `06-gif.gif` 的自动回归**：`tools/gif-verify.py` 是手动/生成器内调用的校验器，
  没有接进任何 CI/测试套件。它读的是**像素**，对二进制产物做断言需要固定沙盒与字体，
  本 run 没做这一步。
- **`06-gif.gif` 的图像内容没有做「标注」**。静态层有 `04-annotated.png` 承担解释，
  动图层的解释在 `06-gif.README.md` 的散文里——按本库 README 的判据这算**不合格形态**
  （库里拒绝散文解说）。本 run 没画帧内的标注框：3 秒的动图里加框/箭头会变成**动画特效**，
  而这是任务明令禁止的。这个张力留给后续决定（要么接受 README 承担解释，
  要么给 gif 一个「静止的标注尾帧」）。
- **没有 push 回 peer1**（任务约束，且 HP 隔离机制下 push 本就应当失败）。
