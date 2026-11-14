<!-- doc-class: historical -->
# 演示采集的终端模拟器：从写死 xterm 到可配置 + 自动探测

**结论先行**：`docs/archive/demo/replay/` 的采图脚本不再写死 `xterm`。终端是一个参数
（`--emu auto|<名字>`），`auto` 会在**私有 display 上逐个真起一次**、等出一个真尺寸
窗口再决定用哪台，选不到任何 VTE 终端才退到 `xterm`。两臂（旧 `xterm` / 新 VTE）可以
各录一遍、逐项对照，产物互不覆盖。

本文记录的是**为什么**，以及一路上踩到、并且已经防住的坑 —— 每个坑都附实测数字，
因为"看起来对"在这条路上被证明过至少四次。

---

## 1. 为什么要换掉 xterm（这不是风格问题）

xterm 是**16 色**终端：gopi 的界面发的是 256 色（`colour-230` 的分隔线、`colour-220`
的用户行、`colour-252` 的正文…）。在 xterm 里这些被映射到最近的基本色，画出来的
不是"gopi 的界面换了个终端"，而是**一套不同的颜色**。

更关键的是第二条：**xterm 没有任何"用户配置"的来源**。它的颜色、字体全部来自命令行
参数或 X resources，不读 `~/.config`。而 GNOME Terminal / xfce4-terminal 走的是 VTE +
用户 profile（dconf / terminalrc）—— 它们画出来的东西**是用户的终端长什么样**。

用 xterm 录对外 demo，等于宣称"gopi 的界面是这个配色"，而这个配色是采集脚本临时塞进去
的一个常量。换成 VTE 终端后，同一份脚本读的是机器上真实存在的配置：`peerbox` 上是
`xfce4-terminal` 的 `terminalrc`（前景 `#b7b7b7`），`HP` 上是 GNOME Terminal 的 dconf
profile（前景 `#171421`、背景 `#ffffff`）。**两地画出来的东西本来就该不一样**，这才是
"忠实"。

## 2. 负对照：怎么证明它真的读了配置，而不是换了个写死的常量

光说"VTE 会读配置"不算证据 —— 采集脚本完全可以在命令行里塞一套颜色，看起来一样。
所以有一个负对照（`negctl-xfce4.sh`）：

1. 把操作者的 `~/.config/xfce4/terminal/terminalrc` **复制**两份到会话目录；
2. 只在 B 副本里改**一行**：`ColorForeground=#b7b7b7` → `#ff00ff`；
3. `XDG_CONFIG_HOME` 分别指向两份副本，同一 dump、同一帧、同一终端各录一次；
4. 对两张图做逐像素差。

**实测**（peerbox，2026-10-01）：

```
操作者真实配置文件 sha256=07a40f959bca066a（全程未被触碰）
cfg-a vs cfg-b  same_size=True  2239x1457  changed_pixels=158688  changed_pct=4.864
                mean_channel_delta_over_changed=94.891   max_channel_delta=327
```

4.9% 的像素变了、平均通道差 95 —— 配置里那一行真的走到了像素上。这条负对照同时是
"改色重录"的证据：改一个值 → 画面跟着变。

操作者的 `~/.config` 全程只读：改的是副本，`XDG_CONFIG_HOME` 把它隔离开。

## 3. 踩过的坑（每条都有实测）

### 3.1 `gnome-terminal` 在私有 Xvfb 上起得来窗口吗 —— 分机器，而且**两次误判**

`gnome-terminal` 会同时留下**两个顶层窗**：

| 窗口 | 类 | 尺寸 | 状态 |
|---|---|---|---|
| 占位窗 | `Gnome-terminal-server` | 10x10 | `IsUnMapped`，永远 |
| 真 toplevel | `Gnome-terminal` | 1353x786（实测 HP） | 由 server 异步 realize |

第一次误判：探测只认窗口类 `Gnome-terminal-server`，于是只搜到占位窗 → 把一台**其实
可用**的终端判成不可用（HP 上真窗口明明在，探测说 `no`）。修正是把匹配类改成
`Gnome-terminal`。

第二次误判在同一天早些时候、方向相反：`peerbox` 上同类探测报告 `可用=yes`，但抓下来是
一张**纯背景图**。原因是"搜到了"不等于"是真的窗口" —— 必须**按尺寸过滤**（≥200px），
把 10x10 占位窗挡在外面。

**两条教训合起来**：窗口类名不是可执行名，**并且**"搜到窗口"不是"有个能画的窗口"。

### 3.2 抓屏要等到窗口**几何连续两轮不变**

`xfce4-terminal` 的窗口先以**默认字体**的尺寸出现（117 列 → 1186px），`--font` 生效后
才长到 2239px。早一步返回就拍到一个还在变的窗口：实测那一帧 1186x521、只有 25 个墨迹
行 —— 一张不合格的图混在合格帧的批次里，而它自己的数字看着"也有内容"。

另有 `windowmove` 的版本：窗口还没 map 完时 `windowmove` 不生效，此时若沿用"我以为它
在 0,0"去抓，抓到的就是那块空区域。所以**位置只信读回来的值**，并且 `X,Y != 0` 就等
下一轮。

### 3.3 `printf` 的第一个参数**就是格式串** —— `%` 必须转义

`printf 'hit 91% · ready'` 在 bash 下报
`printf: `': invalid format character`，并在 `hit 91` 处**截断**。少的不只是两个字符，
是整条状态行的后半截（实测：状态栏那一行的 `· ready` 整段消失）。

我一度以为"实参不参与格式解析"而把它去掉，四问立刻从 `✓✓✓✓` 掉到 `凶`。这条已恢复，
并把理由写在代码注释里。

### 3.4 xterm 的窗口边框会进抓屏矩形

`xterm` 默认画一圈窗口边框（实测 `(0,0,0)`），而终端背景是 `(12,12,12)`。抓屏走
`--area` 取的是**屏幕上的矩形**，那圈边框因此进图；下游按"非背景即墨迹"统计时，它把
**顶行整行**算成墨迹、把分隔线行认成第 0 行 —— 同一张图从 7/7 通过掉到 0/7。

两处修正：启动参数加 `-bw 0`（关掉窗口边框）；`capture()` 之后做一次
`trim_window_frame()` —— **只裁"最外一圈不是背景色"的那几条边**，判据是"边是否纯背景"
而不是"裁 N 像素"（那是魔数），最多 8 轮，裁不动就停下报错。

### 3.5 空图不许静默通过

一张全背景的抓屏在旧代码里会在 `measure()` 里以 `IndexError` 崩掉 —— 读不出"为什么"。
现在空图会触发**重新定位 + 重抓**，仍为空则 FATAL 并打印 `gopi capture` 自己报的
`sha256_16` 与元数据。

### 3.6 `gnome-terminal` 在非 UTF-8 locale 下直接拒绝启动

```
Non UTF-8 locale (ANSI_X3.4-1968) is not supported!
dbus-daemon: Activated service 'org.gnome.Terminal' failed: Process org.gnome.Terminal exited with status 8
```

启动器里必须显式给 `LANG=C.UTF-8 LC_ALL=C.UTF-8`。

## 4. 两台终端的实测差异（peerbox，2026-10-01）

| | xterm（旧） | xfce4-terminal（VTE 内核） |
|---|---|---|
| 配色来源 | 命令行/X resources，**无用户配置** | `~/.config/xfce4/terminal/terminalrc` |
| 256 色 | 否（16 色） | 是 |
| 字号口径 | `-fs`（xterm 自己的尺子），13 → 19.03 px/字符 | `--font='NAME SIZE'`（pango），28 → 19.14 px/字符 |
| 产图尺寸 | 2227×706 | 2239×1457（26 行 × 56px 行高） |
| 抓屏矩形 | 含窗口边框（需 `-bw 0` + 裁边） | 无 WM 时无边框 |

**产图尺寸不同不是缺陷**：字号/字体度量/行高本来就是终端自己的属性。所以两臂**不能**
逐像素比（尺寸不同就无从比），能比的是每臂自己的量：几何、每字符像素、墨迹色团数、
四问通过率。逐像素 diff 留给同臂内的 `raw` vs `clean`（那一对尺寸必然一致）。

## 5. HP 侧

HP（`hostb-Laptop`，Ubuntu 22.04.5）的实测见任务报告。要点：

* `xfce4-terminal` **未安装** —— 所以 HP 侧 `auto` 探测只能落在 `gnome-terminal` 或
  `xterm` 上，"本机真实终端"这个问题的答案按机器不同；
* HP 的 dconf profile：前景 `#171421`、背景 `#ffffff`、`use-theme-colors=true`、
  `font='Monospace 12'`、palette 与 peerbox 的 GNOME palette 一致（同为 Adwaita 默认）。

## 6. 复现

```bash
# 单臂
bash docs/archive/demo/replay/shoot-replay.sh --emu-name xfce4-terminal --out-tag new-vte
bash docs/archive/demo/replay/shoot-replay.sh --emu-name xterm          --out-tag old-xterm

# 两臂 + 指标对照
bash docs/archive/demo/terminal-compare.sh

# 负对照（改配置副本里一行颜色 → 重录 → 逐像素差）
bash <会话目录>/probe/negctl-xfce4.sh :70 <会话目录>/negctl
```

环境变量：`GOPI_REPLAY_EMULATOR`（终端名）、`GOPI_REPLAY_DISPLAY`、
`GOPI_REPLAY_DUMP`、`GOPI_REPLAY_BIN`、`GOPI_REPLAY_SANDBOX`、`GOPI_REPLAY_SESSION_DIR`。
