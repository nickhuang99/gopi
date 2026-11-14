<!-- doc-class: historical -->
# `dropped_frames` 的判据 —— 它是「字节级去重」，不是「抓帧遗漏」

任务：`01a0fa36`（VTE 臂 `arm-sess-4ca8cd29-20261002/replay.json` 报 `dropped_frames: 19`）。
结论先行：**这个数字是去重计数，不是丢录**（第 1 节，源码判据）；但在取证过程中**查出一个
真缺陷**——驱动抓帧有时抓到的是**上一拍**的画面，而那个缺陷正是**经过这个字段隐身**的
（第 3 节，已修 + 最小回归测试）。全部原始证据在本任务会话的证据区：
`/home/user/.gopi/sessions/01a0fa36-7a91-7061-9e9b-fb43d9c7dec0/evidence/`。

---

## 1. 定性：`dropped_frames` = raw − distinct，去重而非丢录

### 1.1 数字从哪来（逐行源码）

| 环节 | 出处 | 做了什么 |
|---|---|---|
| 驱动逐帧抓屏 | `scripts/replay/replay.sh:328-360`（帧循环） | 读到台账最高拍号 N → 抓窗 → 写 `ack` N |
| 字节级去重 | `scripts/replay/replay.sh:401-417` | `cmp -s "$f" "$prev"` 相同则 `continue`，`distinct` 只记第一张 |
| 相减 | `scripts/replay/replay.sh:417` | `dropped=$((frames - distinct))` |
| 落盘 | `tools/replay/assemble.py:312-313` | `"dropped_frames": max(0, a.raw_frames - a.distinct_frames)` |
| 自校验 | `tools/replay/verify.py:748-753` | `raw − distinct == dropped`，不成立即 FAIL |

即：`raw_frames` = 拍到的**节拍数**；`distinct_frames` = 这些照片里**不同的屏幕**数
（保留每段相同帧的第一张）；`dropped_frames` = **"这一拍的屏幕与上一拍逐字节相同"的节拍数**。
交付的帧数就是 `distinct_frames`（`replay.json` 的 `frames` 字段），而**每一拍在台账与逐拍
dump 里都留着自己的一行/一格**：

* `beats.tsv`（`cmd/gopi-replay/main.go:993-998` 每拍一次 `Fprintf` + `Flush`）：140 拍 141 行
  （含表头）——**拍数没有丢**；
* `scratch/dumps/beat-NNNN.dump`（`cmd/gopi-replay/main.go:969-978`，在**等 ack 之前** Settle
  写出）：140 个逐拍网格——**画面也没有丢**。

### 1.2 19 帧的归属（VTE 臂）

原臂的运行目录不在本机（`arm-sess-4ca8cd29-20261002/` 只交付了产物；scratch 当时在
`/home/user/scratch-gopi.*`，现为空目录）。所以用**同一条 script、同一段源会话、同一台终端
（xfce4-terminal/VTE）**重跑复现：`arm-sess-4ca8cd29-20261002/replay.json` 的出处字段
（`session.jsonl 327301 bytes, 284 records, 2026-10-02T06:06:05 … 09:14:32`）唯一确定了源：
`~/.gopi/sessions/4ca8cd29-019c-4dda-a000-6dafd782bf91/session.jsonl` 的**前 284 行**
（327301 字节，末行时间戳逐字相同）→ 复制到 scratch，按 117×26 重放。

```
run1  xfce4-terminal  raw 140  distinct 128  dropped 12   ← 复现臂（本报告用它取证）
run2  xfce4-terminal  raw 140  distinct 127  dropped 13
run3  xfce4-terminal  raw 140  distinct 137  dropped 3    ← A/B 双抓实验臂
```

重放是**内容确定的、抓帧不是**：同一份源、同一条脚本，三臂的 `dropped` 是 12 / 13 / 3。
一个"丢录"计数不该随抓帧时序抖动到这个程度——这就是往下查的起点。

逐帧证据（`evidence/frames-vs-grid-run1.tsv`，判据见第 3.1 节）：

```
beats: 140   frames byte-identical to their predecessor: 12
  of those, the SURFACE's own grid also stood still  = genuine repeat : 1  [26]
  of those, the SURFACE's grid CHANGED               = lost grab    : 11
  [23, 30, 36, 41, 52, 56, 77, 80, 94, 120, 128]
```

`dropped` 与"grid 逐行有变却与上一帧逐字节相同"的帧集**完全一致**（run1 的 12 = 11+1）。

---

## 2. 为什么 19 / 12 / 13 / 3 都不是"缺陷计数"

### 2.1 VTE 与 xterm 的差别（同一条脚本的两臂）

`docs/archive/demo/replay/replay-film/replay.json`（xterm 臂，208 拍）`dropped_frames: 0`；
`docs/archive/demo/replay/arm-sess-4ca8cd29-20261002/replay.json`（VTE 臂，140 拍）为 19。
两次运行的**录制方式、脚本、几何、px/char 都相同**，唯一变量是终端。两种终端的机制差别在
`tools/termemu/termemu.py` 的候选表与 `devdoc/terminal-emulator-selection.md`：VTE（xfce4 /
gnome）在同一字符格上**行距约为 xterm 的两倍**（19.017 px/char 时 2225×1432 对 2227×706），
并且 VTE 的帧是**在响应 DCS 询问时才绘制**的——即它的画面提交**晚于**驱动收到 ack 的时刻，
而 xterm 走渲染线程，抓走的是**内核已提交**的内容。

### 2.2 交付帧集自证伪（为什么不能"逐帧 compare 交付帧"去证明没丢帧）

`replay.sh` 交付的帧集（`arm-*/frames/` 与喂给 `gopi gif` 的那串）**本身就是去重后的集合**：
拿它去逐帧比对、"发现并不重复"，是在用去重后的集合论证去重没有发生。`verify.py` 的 motion
检查也因此只跑**每 18 帧的抽样**（`replay.sh:111` 的 `FRAMES_SAMPLE_KEEP`）。
GIF 那层不足虑：`internal/gifgen` 的 quantizer 在 ≤256 色时走 **EXACT 模式**（调色板就是画面
自身的颜色，无损；实测该片 15 色 / 交付 PNG 190 色，均 ≤256）。
⇒ **唯一能判"丢帧 vs 真静止"的真值是驱动侧的两项**：每拍的 `cmp`，与 `gopi-replay` 自己的
逐拍 dump 网格。

---

## 3. 查出的真缺陷：抓帧跑在终端提交重绘之前（已修 + 回归测试）

### 3.1 判据与 5-why

**判据**：`cmp -s f(N-1).png f(N).png` 为真 **且** 该拍自己的 dump 网格与上一拍不同
⇒ 第 N 帧拍到的是**上一拍的画面**（抓早了）；两者都相同，才是真的没变。

**5-why**：
1. 为什么 13 个帧被去重掉？→ 它们与上一帧逐字节相同。
2. 为什么连续两拍会逐字节相同？→ 驱动抓的那一刻，终端还没提交这一拍的重绘（拍手协议保证
   屏幕"不会**变**"，不保证"已经**画完**"）。
3. 为什么没有拦住？→ 抓帧只有一次 `gopi capture`，没有"画面已静止"的判据（唯一做这件事的
   是 `docs/archive/demo/replay/verify-frames.py` 的"两次抓屏 sha256 相同"，而那是**另一条**管线）。
4. 为什么没被发现？→ 抓早了产生的帧与"真静止"的帧**逐字节不可区分**，
   `distinct_frames`/`dropped_frames` 把两者合成同一个数字。
5. 为什么这个数字看起来像答案？→ 字段名把"去重掉的重复帧"叫成了 `dropped_frames`。

**A/B 证明**（`evidence/ab-two-grabs-run3.md`）：重放器**仍阻塞在同一拍的 ack 上**时再抓一张
（间隔 0.35s），139 拍里 **5 拍**两张不同，其中 3 拍首抓帧 == 上一拍（被去重）；
另 2 拍首抓帧 == 上一拍**已交付**的帧（说明它自己也是上一拍的内容）。

### 3.2 修（surgical）

* 新增 `tools/replay/grab.py`（settled grab）：连续抓两次，**一致才交付**（被 ack 阻塞的屏
  是定点）；`--tries` 有界，未静止则交付最后一张并记 `settled=no`（不静默）；每帧一行证据
  `grabs.tsv`（label / grabs / stale_grabs / settled）。
* `scripts/replay/replay.sh`：`capture_to()` 改调 grab.py（`GRAB_TRIES=4`，`GRAB_SETTLE_MS=120`
  为命名常量，Rule 1）；日志与注释说明"去重"语义；`log` 改为
  `distinct frames: N (M beats repeated the previous screen byte for byte)`。
* `tools/replay/assemble.py:290-313`：`distinct_frames` / `dropped_frames` 加字段级注释，写清
  语义与那唯一的歧义（抓早 vs 真静止），并指向 dump 作为复核真值；模块 docstring 同步。
* `docs/archive/demo/replay/replay-film/README.md` §"重放是怎么对得上的"第 1 条：加"抓的是屏幕静止，
  不是抓一次"与两个计数的定义。

### 3.3 修后实测（run4，同一条脚本、同一份源、同一台终端）

```
raw 140  distinct 139  dropped 1     ← grabs.tsv: stale_grabs 合计 0，未静止 0，没有一帧需要第 3 次抓
grid 复核: 唯一相同的一帧 grid 也是真的没变（genuine repeat: 1，lost grab: 0）
```

同一份源在**未修**的臂上是 dropped 12/13，**修后**是 1，且那 1 个是"起拍空屏"这类
"多拍共用同一画面"。两次的 `verify.py` 四问（可追溯/可读性/运动/遮蔽）全绿。

---

## 4. 最小回归测试

```
python3 tools/replay/test_grab.py       # 新增；15 例全过（注入式 capture，不碰 X/终端）
python3 tools/replay/test_verify.py     # 既有；全过
python3 tools/termemu/test_termemu.py   # 既有；24 例 OK
```

`test_grab.py` 钉住三件事，且**每件都断在修复前后的差别上**：
(a) 抓早的那次不入交付——"lagging grab: delivers THIS beat's screen, not the previous one"；
(b) 永不静止的屏被交付并**上报**（`settled=False`），有界不静默；
(c) 管线口径：两个真的重绘过的节拍，其帧互不相同（旧的单抓会得到两张相同的帧 = 被去重）。

---

## 5. 诚实的边界（Not Done）

* **没能在原 VTE 臂的 scratch 上取证**：`4ca8cd29` 那次的 scratch 不在本机，本报告用的是
  **同源同脚本同终端**的重跑（run1/2/3），复现出 12/13/3 三组数字（原臂 19）——数字对得上
  机制，但不是同一批文件。
* **没改 `verify.py` 去拦"抓早"**：它的 traceability 检查读的正是逐拍 dump，网格与驱动是**两份
  独立记录**；真要拦截，需要一个"驱动帧 vs dump 网格"的比对，而 dump 是**文本网格**、帧是像素，
  两者之间没有可靠的等价判据（这正是它当初被放到像素级之外的缘故）。留给后续。
* **`dropped_frames` 字段名未改**：改名会动 `replay.json` 的消费面（`verify.py:748` 与
  `docs/archive/demo/replay/replay-film/README.md`），而语义澄清已按"代码或 README 之一"的要求落在
  `assemble.py` 的字段注释里。若要改名，建议 `undistinct_beats` / `repeated_screen_beats`，
  与本文件的注释一起改。
* 修后残余：run4 仍有 1 个去重帧（真静止）。抓帧抖动无法降到 0——能降的是"抓早"这一类，
  证据在 `grabs.tsv`。
