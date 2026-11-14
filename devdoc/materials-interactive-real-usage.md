<!-- doc-class: historical -->
# 交互面四条「真的在用」：补产 141/142/143/144，与它们撞出来的三条实测缝


## 这一批解决的是什么

素材库 166 条里，**交互面四条**（115-chat / 117-ui / 118-ui_state / 129-voice_input）
拍的全是**拒绝路径**。这不是那四条作者偷懒，是旧生成范式的结构性后果：那一批的采集
只认「脚本可触发的确定输出」——管道喂 stdin、等一个退出码、抓那份文本。凡是需要
**真打字 / 真开界面 / 真喂音频**的能力，在这个范式里只剩一种可拍的状态：拿不到能力时
它说什么。于是四条素材分别记录了：

| 旧条目 | 它记录的那一面 |
|---|---|
| 115-chat | stdin 到 EOF ⇒ 读到 EOF 即退出 |
| 117-ui | 无 TTY ⇒ `could not open a new TTY` / `exit=1` |
| 118-ui_state | 没有 dump ⇒ `scanned=0`，一个格子也读不出 |
| 129-voice_input | 无参 ⇒ 打印整页 usage / `exit=1` |

四条都是真的，四条也都没有说谎。它们缺的是**另一半**：能力本身在工作时是什么样。

## 补的四条

生成器：`scripts/materials/gen-interactive-real.sh`（可复跑；`--capture-only` /
`--emit-only` 两段可分开跑，字节全落 `--scratch`，不落 worktree）。

| 条目 | 那一面现在是什么 | 02 的逐字节来源 |
|---|---|---|
| 141-chat-roundtrip | 真 xterm 里真打字 → 引擎流式答一行 → Ctrl+C 收尾（一个完整往返） | `gopi ui_state --pid <chat>` |
| 142-ui-panel | 提示行 + 输入行 `you>` + 答案行 `gopi>` + 底部状态行，同屏 | `gopi ui_state --pid <ui>` |
| 143-ui_state-grid | 同一块活屏面被读成 CELL 网格（行/列/光标/属性区间/盲区清单） | `gopi ui_state --pid <ui>` |
| 144-voice-transcribe | 一段真音频进去，一行真中文出来 | `gopi voice_input <wav> --engine doubao` |

**一个需要说明的地方：142 与 143 的 `02-output.txt` 逐字节相同**
（`sha256 prefix d3642effe0d4`，可直接 `sha256sum docs/archive/demo/materials/14*/02-output.txt` 复核）。
采集段对**同一个 ui pid** 连着读了两次（`ui-state-panel.txt` 供 142，`grid.txt` 供 143），
而末次全批重跑里这两次读数落成了同一份字节 —— 于是两条条目虽各引各的来源文件名，
内容却是**一次观测**，不是两次独立观测。两条条目讲的仍然不是同一件事（142 讲"面板这一层长什么样"，
143 讲"同一块活屏面能被读成格子"），但 02 这一份证据只被采了一次；
要两次独立观测，重跑两轮 `--capture-only` 即可（生成器本来就支持分段）。

### 采集的形状（三条「真」是怎么落地的）

**真打字。** 不是 `printf … | gopi chat`，而是：一个无 WM 的 `Xvfb` → 一个真 `xterm`
（`-fa "WenQuanYi Zen Hei Mono"`，字体**显式指定**）→ `gopi chat --dump-screen` 在
里面跑 → `xdotool windowfocus` + `xdotool type` + `xdotool key Return` 把话敲进去。
读屏走 `gopi ui_state --pid`，**零像素、零视觉模型**。

**真开界面。** 同一条 X11 路，换成 `gopi ui --dump-screen --dump-mode ansi`。屏幕真值
另有一份 `gopi capture --screen`（自研 X11 协议客户端，**零外部录制器**）。

**真喂音频。** `espeak -v zh`（既装工具）合成一段中文 wav，交给
`gopi voice_input … --engine doubao`。本机没有 FunASR-Nano 权重
（`--models` 默认 `~/work/funasr-models` 不存在），当时的默认引擎 `local` 出不了文字；
`--engine doubao` 是**显式选**的，且这是本批唯一一次把字节送出本机。

> 2026-10-07 之后：引擎的终点默认值已翻转为 `doubao`（论证见
> `devdoc/doubao-asr-integration.md` §3），所以上面这句「显式选 doubao」描述的是**当时**的
> 事实 —— 今天同样的命令就是默认行为，而"把字节送出本机"由运行时打印的隐私声明明说
> （endpoint + resource id + 三条切回 `local` 的通道），不再靠"没人写 --engine"来隐含。

**图。** 03/04 全部走 `scripts/materials/render-entry.py`：`gopi text2img` 渲染
（自研 SFNT/TrueType 栅格化，**全程没有 locale 这个概念** ⇒ 不可能出现旧 17 条那种
Latin-1 乱码），`gopi annotate` 叠框，并在写 04 之前做**三项像素自证**：
框内有墨迹 / annotate 真改了像素 / 标签带没压住证据。任一项不成立就 exit 1、不产图。

## 撞出来的三条实测缝

写进 `05-finding.md`（每条素材目录里一份），这里给结论与出处。

### 1. 显式路径的 dump，`--pid` 也找不到（141 的 finding）

`gopi ui_state -h` 的 Discovery 一节写着：发现只扫
`<profile>/sessions/**/{ui,chat}-screen.dump` 与
`<scratch>/queue/queue-runtime-ui-session-*/ui-screen.dump`，并补了一句
"A run given an explicit dump path is found by --pid, or read directly"。

**实测这一句与实践有缝**：本轮第一次跑用了 `--dump-screen=/abs/path`，文件确实写出来了
（6369 字节，`frames=58 dumps=14` 都在涨），但：

```
gopi ui_state --target ui   →  no ui-screen.dump dump found in any scanned location   (rc=1)
gopi ui_state --pid 1269233 →  no ui-screen.dump dump written by pid 1269233 (dumps on hand: none)
```

`--pid` 是在**已扫描到的候选**里按 pid 挑，不是按 pid 去全盘找——所以显式路径的那份
**两条路都够不着**。把 `--dump-screen` 改成裸 flag（resolver 走
`uiResolveDumpPath`，落进本 run 自己的会话目录）之后，同一个动词立刻报
`scanned=1 state=live` 并完整读出格子。

> 对使用者的含义：想让 `ui_state` 看得见，就**不要**给 `--dump-screen` 指路径。

### 2. `--target` 在同树有两个候选时拒绝猜——**退出的面也算候选**（142 的 finding）

第一次采集复用了同一个沙盒，于是第二轮读屏逐字撞上：

```
gopi ui_state: 2 exited gopi surfaces could be the target; refusing to guess — pass --pid (or --list) to choose:
```

拒绝本身是**诚实的**（两个候选都能当目标时它不挑一个装作唯一，这是对的）。缝在于
**已经退出的面仍然占着候选位**：一次跑过两轮、哪怕两轮都结束了，`--target` 也会歧义。
于是"复用同一个 profile"这个再普通不过的做法，会让后面每一轮读屏都变成 `exit=1`。
本批的对策是**每轮一个全新 label 的沙盒 + `--pid` 钉死读数**，两者都写进了生成器与
`01-input.txt` 的配方。

### 3. 格子会空着，而它上面那份 `lines` 段是另一回事（143 的 finding）

某一次 dump 头部一切正常（`terminal=110x34 grid=110x34 … frames=58 dumps=14`），
`lines_end` 那段也有血有肉（`you>` / `gopi>` / 状态行都在），可紧跟的
`grid_begin count=34` 里**每一行都是空白**。

原因它自己的 blind_spots 逐字写着：

```
005|blank_vs_unpainted|a space cell and a never-painted cell are identical in the grid
```

但更容易骗到人的是**上面那份 `lines_*` 段**：那是 dump 的逐帧 DIFF 记录（本帧画了什么），
与下面「终端此刻长什么样」的 `grid_*` 段**是两件事**。只盯 `grid_begin` 的读者会在
dump 明明记录了一屏内容时得出"屏幕是空的"的结论；反过来只读 `lines_*` 的人，
会把"本帧画过"当成"屏幕上就是这些"。

本批的处理：驱动脚本等的是 `gopi> …` **真的出现在 dump 里**（`wait_for_text` 轮询，
不靠固定 sleep 猜），所以交付的
`02-output.txt` 里 `lines_*` 与 `grid_*` **两边都有那两行**。

### 4. xterm 只给字体不够，还要 `-u8` 与一个 UTF-8 locale

第一版的屏幕抓图里出现了 `â` / `Â`——是 em-dash 被按 Latin-1 画出来的样子。查记忆
命中 `#5710`/`#5704` 的实测：xterm 需要**两样都给**，`-u8` 加一个 UTF-8 的 locale
（缺任一，CJK 与 em-dash 都会变成 mojibake），而且这种图会被**误判成渲染器的锅**。
生成器因此改成 `env LC_ALL=C.utf8 LANG=C.utf8 xterm -u8 …`，并把这件事写进了每条
`01-input.txt` 的配方（复跑者不该再踩一次）。

同时把 `00-entry.md` 的措辞收紧了一句：屏幕那一面只能说「字体与 locale 都显式给定」，
**不能说「不可能乱码」**；交付图上「不可能乱码」有硬依据的只有一条链路——
`gopi text2img`（它没有 locale 这个概念）。

### 5. 针必须锚在**答案行**上，而且流式答案要等它「落定」

两处都是「等错了东西」：

- **等到了回声。** 第一版拿 `MATERIALS-CHAT-ROUNDTRIP-OK` 当针，可 Markdown 一行
  `you> Reply with exactly one line: MATERIALS-CHAT-ROUNDTRIP-OK` **就含这个子串**——
  于是驱动脚本在用户那行刚回显时立刻认为「答案到了」，读到的屏面上答案区还是空的。
  改成锚在答案行自己的前缀上（chat 是 `▌ <mark>`，ui 是 `gopi> <mark>`）。
  这是一类通用错误：**针要落在被测方产出的那一行，不能落在请求里也出现的那个字符串上**。
- **等错了时长。** 一轮 ui 回合实测 **96.3s**（`gopi log --last` 的实测值：
  `Iterations 1 / Total 19,629 tokens / Elapsed 96.3s`），而固定 90s 的超时会在答案
  流到一半时收工，读到的是被截断的一行（本轮真读到过 `003|gopi> MAT`）。
  判据改成 `wait_for_answer`：先等那一行出现，没等到就等 dump **不再变**
  （mtime 静了 `QUIET_SECS` 秒），两头都锚在观测到的状态上。

## 一条不做纠正的读数（144）

合成的原句是 `你好，我叫尼克，这是一段测试音频。`，doubao 转出来的是
`你好，我叫可可，这是一段测试音频。`——**「尼克」被转成了「可可」**。

这不是脚本的 bug：`gopi voice_input -h` 自己写着 "at CER 1.58% roughly one character
in sixty is wrong, and a wrong technical term (RAG→Rug) costs more than a wrong
character"。`02-output.txt` 是那次运行的真实输出，**逐字节不改**；两处文本的差异
本身是这条素材最有价值的读数——它诚实地展示了这个能力的准确率边界。

## 为什么另外三条不走云端

141/142/143 全程不出本机（X11 抓屏与文本渲染都在本地完成）。144 的每一次运行都会
把一段音频上传到 `openspeech.bytedance.com`。把这件事写进 `05-finding.md` 而不是只写在
argv 里，是因为素材库要人能**独立复核**——复核者有权在跑命令之前就知道音频会走公网。

## 验收证据

- 六件套：四条各 6 个文件，`02-output.txt` 各含 1 行 `^exit=[0-9]+$`。
- vision 复核（`vision_analyze`，模型 deepseek-flash）：
  - 142 读成面板：`you> Reply with exactly one line: MATERIALS-UI-PANEL-OK` /
    `gopi> MATERIALS-UI-PANEL-OK`，红框落在答案行，判"无乱码"；
  - 141（框区裁剪复核）：框住 `MATERIALS-CHAT-ROUNDTRIP-OK`，判"没有乱码"，
    并认出这是终端会话/读屏输出（行号 + `you>` + 状态行）；
  - 143（框区裁剪复核）：框住 `grid_begin count=34`，判"没有出现传统意义上的乱码"；
  - 144：框住 `你好，我叫可可，这是一段测试音频。`，判"中文清晰可读，没有乱码"，
    并逐行读出 `gopi voice_input`、`voice-in.wav`、`exit=0`。
  > 141/143 的整图高 7434/6024 px，vision 在**整图**上会跑偏（把标注标签读成
  > `you> What is the color of the sky?`）；改用框区裁剪复核即得到上表结果。
  > 裁剪只用于复核，不参与交付物。
- 像素自证（`render-entry.py --json-out`，最后一次全批重跑的实测值）：
  `141 ink=1363 changed=2695 covered=0 size=1906x7974` /
  `142 ink=1071 changed=2449 covered=0 size=998x5934` /
  `143 ink=683 changed=2972 covered=0 size=998x6024` /
  `144 ink=191 changed=1750 covered=0 size=1069x564`。
- `gopi doccheck`：corpus 一致（本文件已在同一提交内分类）。
- `git status --porcelain`：只有本次新增的 4 个目录 + 1 个生成器脚本，**其余 159 条未被触碰**。

## 没做的

- 没有把 141/142/143 也做成「真屏幕像素图」：那需要外部录制器或 xterm 自己的字体链路，
  而 Epic 约束要求零外部录制器、且中文必须无 Latin-1 乱码。本批的取舍是**文本证据图**
  （`gopi text2img`）+ **真屏幕抓图另存**（`ui-screen.png`，落在 scratch，未入条目）。
- 没有改 `gopi` 的任何产品代码：三条缝都是**发现**，修不修是另一件事，本轮只负责把
  它们拍到能复核。
- 没有跑全量测试套件（Rule 27：merge/verify 路径只跑 smoke、按分支范围）。
