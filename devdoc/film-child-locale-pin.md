<!-- doc-class: normative -->
# film child 的 locale：钉住 UTF-8，钉不上就拒绝

**状态：** master 已修（`internal/shot/filmlocale.go`）
**背景：** 补 `01a10370` 自陈的缺口 —— 那次只把**字体**修好了（默认终端 xterm →
xfce4-terminal），**locale** 这一层没动。两层缺陷里，前一层把后一层藏住了。

---

## 1. 缺陷：终端按它自己的 codeset 解码 pty 流

一个终端 emulator 不是「把字节画出来」的哑设备：它把 pty 上的字节流**当文本解码**，
而解码用的 codeset 来自**它自己的 locale**。xfce4-terminal（VTE）继承调用者的环境，
所以当 film driver 在 `LANG=C` / `LC_ALL=C` 下运行时（**这台主机的常态**），VTE 的
codeset 里没有 UTF-8，于是它**每个字节 advance 一个 cell**。

后果不是「看起来有点怪」，而是一个**错误的几何**：

- 一行 `中文测试ABC`（4 汉字 + 3 ASCII）在正常 UTF-8 codeset 下 advance
  `4×2 + 3 = 11` 个 cell；
- 在按字节 advance 时是 `4×3 + 3 = 15` 个 cell。

同一行宽了 **3.7 个 cell**，每个汉字都被撑成字节宽度，并把相邻 ASCII 挤走。
拍出来的片子是**产品从未画过的布局**。

**这是同一台主机、同一个 xfce4-terminal、同一个字体的两次测量**，不是两台机器的对比
（对比见 `devdoc/film-cjk-default-terminal.md`：那是 xterm vs xfce4-terminal 的字体差异，
本题是同一终端在两种 locale 下的差异）。

### 为什么驱动不能「就这样」继承调用者环境

驱动（`cmd/agent/chat_film.go` → `shot.StartFilmTerminalSpec`）在私有 Xvfb 上起终端时，
原本把 `os.Environ()` 原样交给 child。这对 DISPLAY 是错的（已修：必须 REPLACE，
因为 `getenv(3)` 返回**第一个**匹配项，追加不构成覆盖 —— `01a10370` 实测过），
对 locale 同样是错的。**driver 的调用者是谁、它的 locale 是什么，与这块屏面该被
怎么解码无关**。

---

## 2. 修法：pin 在 child 上，且必须被证明可用

### 2.1 组合方式：REPLACE，不是 append

`FilmTerminalChildEnv(display, environ)` 返回**一份新的 env slice**：

1. 丢掉继承来的 `DISPLAY=`、`LANG=`、`LANGUAGE=`、以及**整个 `LC_` 家族**；
2. 写入 `DISPLAY=<私有 display>`、`LANG=<pin>`、`LC_ALL=<pin>`。

丢掉整个 `LC_` 家族（而不是只丢 gettext 那几个名字）有两个理由，都是关于这个 child 的：

- 真正决定**解码与宽度**的变量是 `LC_CTYPE`。留着一个继承来的 `LC_CTYPE`，
  就把本文件存在的那个决定交给了「没人看过的一个变量」；
- film child 的 locale 必须是**一个值**，「丢掉所有 locale 拼写、写入 pin」是这个
  意图唯一不会被漏掉的表达。

**调用者的环境一个字节都不动**：pin 只活在 child 的 env slice 里，本进程的
`LANG`/`LC_*` 从不被 set（证据见 §4.4）。

### 2.2 可用性必须被证明（Rule 15：不静默）

一个**没装**的 locale 不会被 libc 拒绝：`setlocale` 返回 NULL，进程继续在 `"C"` 里跑。
实测：`env -i LC_ALL=xx_YY.bogus locale -a` **exit 0**，只在 stderr 打个 warning。

所以「未经验证的 pin」比「没有 pin」**更糟**：它看起来修好了，实际拍的是乱码。
本实现的做法是**问主机自己**：

```
locale -a        # 这台 libc 认识的所有 locale 名
```

用运行 `locale` 二进制来问，而不是去猜 `/usr/share/i18n` 的目录布局 ——
编译进 glibc 的 locale（`C.UTF-8`）根本不留目录，而问题恰恰是
「**这个** libc 认不认这个名字」。

挑选用例顺序：`GOPI_FILM_LOCALE`（须 UTF-8 **且**已安装） > `C.UTF-8` > `C.utf8` >
`en_US.UTF-8`。默认 `C.UTF-8` 是因为它是 **glibc 内置**的，裸机上也在（Rule 35）。

**没有 rank 3**：「保持调用者的 locale」不是 fallback，**它就是缺陷本身**；
「装了但没验证就直接 pin」是 silent failure。两种情况都是**拒绝**（返回 error），
带命令名、原因和旋钮名。

### 2.3 顺手修掉的一个真 bug（自己踩到的）

第一版用「字符串相等」判断 locale 是否可用 —— 于是**拒绝了本文件自己的默认值**：
`locale -a` 在这台机器上打 `C.utf8`，而仓库里所有调用点写 `C.UTF-8`。
`.UTF-8` 与 `.utf8` 是**同一个 locale 的两种拼写**，glibc 两种都收。
修法是 `localeNameKey()`：大小写不敏感 + 把 codeset 的 dash 去掉，**两侧**都归一化
（可用性比较与候选匹配）。报告里引用的是**主机自己的拼写**（operator 能直接粘进 shell 的那个）。

### 2.4 一次 pin 的可审计性

film 运行会打印一行通知（`FilmChildLocaleNotice`）：

```
[gopi film] the filmed terminal is pinned to locale C.utf8 (its child environment
replaces every inherited LANG/LC_*; GOPI_FILM_LOCALE names a different one)
```

pin 会改变**每一帧中文**的像素；读不到 pin 的人无法分辨「正确的片子」和「乱码的片子」。

---

## 3. 判据：在同一张图里自校准一个 advance 单位

不靠 OCR、不靠 vision 分数（Rule 10.2），判据只用像素：

1. 窗口 = ink 的矩形（每行/列至少 8 个 ink 像素才算窗口，否则边框/指针把矩形拉到整屏）；
2. 文字带 = ink 行的连续段；带内的**列 run**（gap ≤ 1px）是字形 token；
3. 尾部 `ABC` 的**三个 token** 宽度 + 两处间隙 = **一个 ASCII advance 单位**
   —— 从同一张图、同一字体、同一字号自校准；
4. 整行的 advance span 换算成单位数，与两个预测比：**11** = 4 个双宽汉字 + 3 个 ASCII；
   **15** = 每个 UTF-8 字节一个拉丁字形。

脚本（本 run 的 evidence）：`probe/one3.sh`（私有 Xvfb + 裸 xfce4-terminal + 同一个字符串）、
`probe/filmlineprobe.py`（同一判据，限定到指定的行带）。

---

## 4. 测量

### 4.1 受控探针：同一终端，只换 locale（因果变量）

裸 xfce4-terminal，`--geometry=40x6`，`printf '中文测试ABC\n'`，同一次捕获流程：

| locale | unit | span | advance | 判决 |
|---|---|---|---|---|
| `C` | 9.67 px | 149 px | **15.41** | BYTES DRAWN AS LATIN |
| `C.UTF-8` | 9.67 px | 108 px | **11.17** | CJK RENDERED |

两个预测都对上了，所以这是一个**测量**而不是印象。

### 4.2 film 路径：driver 起出的 child（本题真正要修的那条路）

同一份 replay、同一台机器、**调用者是 `LANG=C`**：

- **RED**（修前二进制 = master `6d5c0abe`，`strings bin/gopi | grep -c GOPI_FILM_LOCALE` = 0）：
  frame-0002 的输入行带 = **23 个 ~9px 单宽 token，双宽 token 数 = 0**，
  整行 23.69 units ——「每个字节一个字形」的宽度，与同一屏上正常 ASCII 的 advance 同宽。
- **GREEN**（修后二进制）：同一行带 = **4 个双宽 token（14, 16, 15, 16 px）+ 3 个 ASCII**，
  即 VTE 把 4 个汉字**各画成一格双宽字形**；启动日志出现 pin 通知 `C.utf8`。

vision 逐字读同一行（放大 4× 的裁剪）：

- GREEN → `▶ 中文测试 ABC`（模型原话：字形清晰，无乱码或方块）
- RED → `?00?0?0??0??0?0?????0??0??0???0?0?0??0???0?????00?0???0??00?0?0???0?0ABC`

这两个读数合起来是完整的：**同一个字符串，修前被字节解码成乱码且宽度错 1.79 倍，
修后是汉字且宽度正确**。U+FFFD 替换符（`beats.tsv` 的 preview 里可见 `中文���试ABC`）
属于**另一个阶段**（ledger 读取时的解码），它自己的 renderer 是 VT 屏障而不是 VTE
（VT 模型按 East-Asian-Width 推进列），所以它不影响本条测量；本判据之所以选
`beats.tsv` 里**可解码的那条**（`中文测试ABC`）来跑，就是为了把它排除掉。

### 4.3 UTF-8 调用者不回归

调用者 `LANG=C.UTF-8 LC_ALL=C.UTF-8` 跑同一条 film → 正常完成，pin 通知仍是 `C.utf8`
（pin 是幂等的：UTF-8 调用者也被钉到同一个 UTF-8 locale），中文照常。

### 4.4 调用者环境未被改动

RED / GREEN / UTF-8 三次运行都取 `env | grep -E '^(LANG|LC_)'` 前后各一份并 diff →
**IDENTICAL**（`evidence/caller-env-before.txt` vs `caller-env-after.txt` 等）。

### 4.5 拒绝路径（不静默）

| 输入 | 结果 |
|---|---|
| `GOPI_FILM_LOCALE=C` | film **不启动**，exit=1，消息给出原因 + 建议 + 旋钮名 |
| `GOPI_FILM_LOCALE=xx_YY.UTF-8` | film **不启动**，消息给出 `locale -a` 命令 + 本机清单 + 旋钮名 |

两条都在「terminal attempt 1..3 failed」里重试三次后失败退出，**没有任何一次回退到
调用者 locale**。

### 4.6 host 桌面配置未被碰

`~/.config/xfce4/terminal/terminalrc` 在整轮测量前后 sha256 一致
（`07a40f95…c17f9`），mtime 未变（`2026-10-04 05:13:08`）。

> 本轮测量**结束后的实测状态**（不是推断）：`/home/user/.config/xfce4/terminal/` 下只有
> `accels.scm` 与 `terminalrc` 两项，**没有任何临时文件**；本改动新增/修改的代码里
> **没有一行文件系统写入**（`git diff` 里没有 `os.Write*` / `Mkdir` / `Rename`），
> 且整个提交没有任何一处**写入** `/home/user/.config`（只有散文里提到这个路径）。
> 所以「桌面配置未被碰」是可复核的：`sha256sum -c` 为 `OK`。

---

## 5. 面（改了哪些地方）

| 文件 | 改动 |
|---|---|
| `internal/shot/filmlocale.go` | 新增：pin 的组合、可用性证明、拒绝、通知 |
| `internal/shot/terminalterm.go` | 起 terminal 时改用 `FilmTerminalChildEnv`；把拒绝向上传播 |
| `cmd/agent/chat_film.go` | 打印 pin 通知（可审计） |
| `internal/shot/filmlocale_test.go` | 新增：12 个回归测试 |
| `CONFIG.md` | `GOPI_FILM_LOCALE` 目录行（旋钮必须能被 operator 查到） |

**没动的**（约束）：`go.mod`/`go.sum`、四通道 registry、默认终端、
`~/.config/xfce4/terminal/terminalrc`、任何 host 级配置。

---

## 6. 回归测试钉住的东西

`go test ./internal/shot/`（12 个新测试，全绿）：

- pin **压过**继承来的 `LANG=C`/`LC_ALL=C`/`LC_CTYPE=C`：三个都不许存活，
  且 `LANG` 与 `LC_ALL` 两个都必须变成 UTF-8（驱动真正的组合函数，不是替身）；
- **调用者环境不变**：进程 env 前后逐条相等（含条数）；
- 继承的 `DISPLAY` 被替换（不是追加）；
- 拒绝三态：主机无 UTF-8 locale / pin 不是 UTF-8 / pin 未安装 —— 每条都要在消息里
  指名命令、原因和旋钮；
- 候选顺序、`.UTF-8` 与 `.utf8` 等价、listing 每进程只读一次；
- 一个测试**打真机**（不替身），所以「seam 是诚实的」是被检查的，而不是被假设的。
