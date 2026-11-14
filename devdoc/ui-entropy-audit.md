<!-- doc-class: normative -->
# 界面感知的熵审计 — 三条路径的字节/token 成本、穷举分级与一句话路由规则

审计任务，2026-09-24。**未改任何产品代码** —— 本文件是唯一交付物；改动仅限
`devdoc/`（本文件 + `DOC-INDEX.tsv` 索引行）。

Operator 的判断：**"使用 xdisplay 像素级感知不仅不准确，而且很昂贵"**，
**"本身我们的界面是 text-based 照理说不需要使用像素级的手段"**，
**"也许你可以使用不同的 display，但是这个本身我就不是很欣赏"** ——
换 display 不是解法，**不上像素**才是。本文件把这句话变成可复核的数字。

---

## 0. 一句话可引用结论

> **在同一个 `gopi` 界面状态下，文本状态路径读到的字节数是像素路径的
> （2,310 B vs 307,625 B 整屏；窗口范围内 1/69，2,310 B vs 158,684 B），
> 并且断言更强：它能对 6 个具名事实下断言（tool 调用行、其结果行、工具输出文本、
> 状态行计数器 `idle · turns 1 · tool events 2`、无 `streaming…`、无 `⚠ ` 行），
> 而像素+vision 路径在一次实测里连一行密集文本都读不准（80/100 字符，
> 100 字符的行被读成 109 字符），另一次调用直接返回空内容。**

可引用的最短形式：**自家文本界面用文本状态读，便宜 2 个数量级，且能多断言事实。**

---

## 1. 三路径成本对照表（同一界面状态）

**同一状态的定义**：一台 1920×1080 的 X11 桌面（`localhost:15.0`），其上跑着
`gopi ui --tools`（pid 22202，`/dev/pts/1`）/ `gopi chat --tools`（pid 22449），
20 行 viewport、tools on、一个已结束的 tool-call 轮（turns 1 / tool events 2）。
三路径在同一分钟内在该状态下取数。

| # | 路径 | 读到什么 | bytes | tokens | 实测命令（原文见 §1.1） |
|---|---|---|---|---|---|
| A | **像素** `screen_capture` | 整屏 PNG（全部窗口一起） | **307,625** | 未调 vision（本任务像素采样上限=1） | `screen_capture(display="localhost:15.0", out=…/ui-state.png)` |
| A′ | 像素（既有记录，同屏） | 整屏 PNG | 350,716 | — | session `01a0d2f0-…` `call_01_C0AW…`/`call_01_OfIt…`/`call_01_rAm7…` |
| A″ | 像素（既有记录） | **活动窗口** PNG 1364×740 | **158,684** | **683 in / 751 out** | 同上 + `vision_analyze(image_path=…/live-now.png)` |
| A‴ | 像素（既有记录） | 区域 PNG 1364×180（底栏） | 41,994 | — | session `01a0d2f0-…` `call_01_rAm7…` |
| B | **窗口结构** `xprop` | 7 个窗口 id（`_NET_CLIENT_LIST`） | **113** | ~28（chars/4） | §1.1 B |
| B′ | 窗口结构 `xprop` | 7×(`_NET_WM_NAME`+`WM_CLASS`) | **753** | ~188 | §1.1 B |
| C | **应用状态文本**（进程内导出） | 模型自绘屏幕（20 行 viewport + 200 行全高） | **2,310** | **~578** | §1.1 C |
| C′ | 其中「屏幕本体」（20 行 viewport，去头尾注记） | 24 行、14 行非空 | **475** | ~119 | §1.1 C 的 python 切片 |
| C″ | 其中「全高屏幕」（200 行网格） | 200 行、14 行非空 | **651** | ~163 | 同上 |

**换算口径**：bytes 是文件/输出字节数（`wc -c` 口径）；tokens 用仓库既有启发式
`chars/4` —— `internal/agent/context.go:353`（`EstimateTokens`，注释明确
"4 characters per token"），A″ 的 683/751 是 vision 服务自报的真值。

**比值**：A/C = 307,625 / 2,310 = **133×**；若把像素路径的 JSON 自报头（约 500 B）
也算进去 = 308,125/2,310 = **133×**；按 vision 必读的那份窗口图算
A″/C = 158,684/2,310 = **69×**。
（整屏 PNG 分页：2,310 B 的自绘屏幕只需 1 页文本，307,625 B 的桌面 PNG 在任何
vision 路径里都是整张图）；保守引用请用 1/69（窗口范围）或 1/133（整屏）。**

### 1.1 每个数字的可复现命令与输出

**A（本次唯一一次像素采样）**

```
screen_capture(display="localhost:15.0", out="/home/user/workspace/_audit-p7/ui-state.png")
→ {"path":"…/ui-state.png","display":"localhost:15.0","mode":"screen","width":1920,
   "height":1080,"x":0,"y":0,"depth":24,"bands":9,"bytes":307625,
   "sha256_16":"36a034eba1369c9a","hint":"Pass this path to vision_analyze to read the screen;…"}
```

**A″（既有记录，不重复调用 vision）**

```
$ S=$(ls -t /home/user/.gopi/sessions/01a0d2f0*/*.jsonl | head -1); python3 - "$S" <<'EOF'
  …（按 tool_use_id 取 tool_result 原文）…
EOF
### screen_capture call_01_OfIt67qU0ymC3QXP6Sfs9630: result chars=507
{"path":"/home/user/workspace/live-now.png","display":":11@fallback","mode":"window",
 "width":1364,"height":740,"x":277,"y":78,"depth":24,"bands":6,"bytes":158684,
 "sha256_16":"653d63d5a3ac3236","window":"0x2c0000a","window_route":"_NET_ACTIVE_WINDOW",…}
### vision_analyze call_00_gh06JS9Q2PiSIiTnHBxO4919: result chars=2562
【vision 分析结果】(模型 deepseek-flash, 输入 683 tokens / 输出 751 tokens, 图片 158684 bytes)
```

来源：session `01a0d2f0-4e6e-7638-84ad-3eb89e329bb1`，2026-09-24（本任务同一台机器）。
同 session 另两条：整屏 `bytes:350716`、区域 `bytes:41994`。

**B / B′（窗口结构路径）**

```
$ xprop -display localhost:15.0 -root _NET_CLIENT_LIST > /tmp/p7_win_ids.txt 2>&1; wc -c /tmp/p7_win_ids.txt
113 /tmp/p7_win_ids.txt
$ head -c 120 /tmp/p7_win_ids.txt
_NET_CLIENT_LIST(WINDOW): window id # 0x1c00003, 0x800017, 0x2c0000a, 0x3400008, 0x36000f8, 0x3000008, 0x34a253e
$ for w in $(xprop -display localhost:15.0 -root _NET_CLIENT_LIST | grep -o '0x[0-9a-f]*'); do \
    xprop -display localhost:15.0 -id $w _NET_WM_NAME WM_CLASS; done > /tmp/p7_win_names.txt
$ wc -c /tmp/p7_win_names.txt
753 /tmp/p7_win_names.txt
$ head -4 /tmp/p7_win_names.txt
_NET_WM_NAME(UTF8_STRING) = "@!0,0;BDHF"
WM_CLASS(STRING) = "gjs", "Gjs"
_NET_WM_NAME(UTF8_STRING) = "DeepSeek 开放平台 — Mozilla Firefox"
WM_CLASS(STRING) = "Navigator", "firefox_firefox"
```

**C（文本状态路径，进程内导出）**

```
$ GOPI_UI_SCREENMODEL_DUMP=/home/user/workspace/_audit-p7/screenmodel-dump.md \
    go test ./internal/uiclient/ -run TestRealToolCallTurnScreenModelIsAgentReadable -v -count=1
=== RUN   TestRealToolCallTurnScreenModelIsAgentReadable
    ASSERT tool call row            "callbashhead-3go.mod"         pass=true
    ASSERT tool result row          "resultbashexit=0"             pass=true
    ASSERT tool output on screen    "github.com/the operator/gopi"         pass=true
    ASSERT status line at rest      "idle·turns1·toolevents2"      pass=true
    ASSERT not still streaming      "streaming…"                   pass=true
    ASSERT no fault recorded        "  ⚠ "                         pass=true
    screen-model dump -> /home/user/workspace/_audit-p7/screenmodel-dump.md
--- PASS: TestRealToolCallTurnScreenModelIsAgentReadable (0.00s)
$ wc -c /home/user/workspace/_audit-p7/screenmodel-dump.md
2310 /home/user/workspace/_audit-p7/screenmodel-dump.md
$ grep -c $'\x1b' /home/user/workspace/_audit-p7/screenmodel-dump.md   # ESC 字节数
0
$ python3 - <<'EOF'   # 切出屏幕本体，量净载荷
… 见输出 …
TERMINAL VIEW (20 rows): bytes=475 lines=24 nonblank=14
FULL HEIGHT (200 rows): bytes=651 lines=200 nonblank=14
```

导出的是 `internal/uiclient` 模型自己的 `View()`（`internal/uiclient/model.go:744`）——
即应用状态路径的本体。数据来源是一条真实工具调用轮：
`internal/uiclient/testdata/tool-call-turn.sse.b64`（从本机真实 `gopi web` 服务抓的
`POST /api/chat` 响应体）。屏幕模型内部是 VT 单元网格
（`internal/lineedit/screen.go:427` `Contents()`），属性 API 在
`internal/lineedit/screenattr.go:103`（`Cell`）/`:157`（`StyledLine`）/`:175`（`StyledContents`）。

### 1.2 每条路径能断言什么、不能断言什么

| 路径 | 能断言 | 不能断言 |
|---|---|---|
| **A 像素 + vision** | 有/无某个大色块；"大致像不像那个界面"；resize 0 的整屏哈希变化（`sha256_16` 变 = 屏变了） | **列/行位置**（"距左边框几列"）；**状态行计数器**（`turns 1 · tool events 2` 这种数字）；密集行的逐字文本（实测 80/100 字符）；**哪个窗口是谁的**（整屏把他人窗口一起抓进来）；**机器不可判定** —— 输出是自然语言，不是可 `grep` 的字段 |
| **B 窗口结构 `xprop`** | 有哪些顶窗、它们的 id/标题/类名；哪个窗口是 active（`_NET_ACTIVE_WINDOW`） | **窗口内部**任何东西：光标在哪个 widget、TUI 的第几行是什么。同一进程内多个界面（operator 的 `gopi ui --tools` 与 agent 自己的 `gopi chat --tools`）只能看到**一个 gnome-terminal 标题**，分不出是谁（`devdoc/tab-order-actuate-poc.md:151,165`：xprop "NO"——从不回答"哪个 widget 有键盘"） |
| **C 应用状态文本** | 6 个具名事实（§0）；行/列位置（网格有 `Row/Col` 语义）；计数器；会话身份（各自导出各自的）；**零 ESC 字节**（可 `grep`/`cmp`/`awk` 的纯文本） | 见 §4「文本路径做不到的事」 |

---

## 2. 仓库内像素路径穷举与分级（带 file:line）

判据：**目标是不是我们自己的 text-based 界面**（`gopi ui` / `gopi chat` /
bubbletea 屏）？是 → 文本状态路径优先（应替换）；不是（外部 GUI、他人的产物、
或问题本身就是像素问题）→ 合理保留 / 必须用像素。

### 应替换（目标是自家 text UI，却把像素写成默认观察手段）

| file:line | 现状 | 替换成什么 |
|---|---|---|
| `internal/agent/loop.go:1578` | `screen_capture` 工具描述首句路由：**"When to use: 看屏幕 / 截图当前桌面 / 截图当前窗口 / 电脑操作 (computer use) 的第一步"**，并给出 `capture → vision_analyze → act → capture` 循环；**对自家文本界面没有任何反向指引** | 描述里加一句路由句："目标是**我们自己的**文本界面（`gopi ui`/`gopi chat`）时，**不要**用本工具：读 `internal/uiclient` 的状态导出 / `lineedit.Screen` 网格。本工具用于**外部**任意 GUI 与像素真值比对。" |
| `cmd/agent/cli_act.go:6,8` 与 `:76,79` | usage 把 `gopi screen_capture --out before.png` / `after.png` 教成**唯一**的 see/verify 循环（`# compare sha256_16`），未提示文本界面有更便宜的等价物 | 同一段加一行："文本界面 vs 文本界面：读状态导出（字节差 2 个数量级），像素只用于**外部** GUI 或**渲染质量**。" |
| `cmd/agent/cli_screencapture.go:21,29,31` | `gopi screen_capture -h` 的通篇示例只有 before/after PNG | 同上，一行路由句即可 |
| `scripts/ui/capture_emphasis.sh:1-30` | 把**自家** gopi UI 的 emphasis 渲染成 PNG（xterm on Xvfb）再交给 xterm 画属性 | **保留**它的字节与属性 dump 两半（`palette_capture_test.go` 的 `GOPI_PALETTE_CAPTURE_DIR` 产物），把 PNG 降级为"人看的附图"，并在脚本头写明"机器可判定的那一半是 attribute dump，PNG 不是证据" —— 脚本头已经这么说了（`# The PNG is never the only evidence`），所以本行只需在**引用它的文档**里补齐同样的句子 |

### 合理保留（外部目标 / 已经是文本优先 / 历史记录）

| file:line | 为什么保留 |
|---|---|
| `cmd/agent/cli_act.go:1-10`（`gopi act` XTEST 本体）、`internal/tools/screencapture.go` | 目标是**外部**任意 GUI（operator 的 firefox/gedit/calculator）：文本模型对它一无所知，像素/A11y 是唯一通道。分级只针对**路由文本**，不是工具本身 |
| `devdoc/tab-order-actuate-poc.md:151,165` | 记录"xprop 从不回答哪个 widget 有键盘"——**支撑**本审计的窗口结构路径边界，且已在用 a11y 结构化状态 |
| `devdoc/cli-as-eyes-and-hands-feasibility.md:26`（"For *fidelity*: do not use vision on a screen when text is available"）、`:27`（采纳率：bash 32,758/273 sessions vs `vision_analyze` 13/4、`screen_capture` 7/5）、以及该报告的第一次真调用记录（587 input tokens、一次空内容、一屏 2 页 PNG） | 已经**是本审计的证据来源**，其结论与本文一致；保留并把本文引用为数字出处 |
| `devdoc/agent-ui-observation-research.md:12-46`（historical） | 外部世界先例研究（Playwright MCP "bypassing the need for screenshots"、Chrome DevTools MCP "Prefer taking a snapshot over taking a screenshot"）；属于"别人怎么做"的记录，不改 |
| `briefings/brief-land-bubbletea-ui-hp.md:8` | 明确写成验收："**the UI's state is assertable from a readable model with no screenshot**" —— 仓库已有的正解先例，应被本文的路由句引用 |
| `internal/lineedit/palette_capture_test.go:70-80`（`GOPI_PALETTE_CAPTURE_DIR` 落 `capture-colour.ansi` / `-mono.txt` / `-chrome.ansi`） | 已经是**机器可读**的属性路径（505 B / 407 B / 377 B ANSI 字节），正是文本路径该有的形状 |
| `briefings/brief-gc-scope-gotmp-and-rawlogs.md:89` | 保护 HOMM3 项目**外部**产物的截图资产，与自家 UI 无关 |
| `devdoc/x11-xauthority-candidates.md`、`devdoc/screen-capture-*.md`、`devdoc/COMPUTER-USE-CLOSURE-VERIFIED.md`（historical） | 主题是**抓图机制本身**（Xauthority、CLI 暴露、actuation 闭环）的历史记录，不是"如何观察自家文本 UI"的建议；冻结保存 |
| `briefings/brief-research-ui-dev-prior-art.md:35,54,59,62` | 该 brief 的任务正是"问业界是否 prefer structured state over pixels"——**问题本身**就是像素 vs 结构化状态，保留 |

### 必须用像素（文本路径在原理上做不到）

| file:line | 为什么必须 |
|---|---|
| `GOPI.md:325`（Rule 10.2 / 工程习惯 3） | 渲染**质量**（字体/glyph/字距）只能像素级 IoU 量化；vision 无法量化质量损失 |
| `devdoc/gui-chat-roadmap.md:477` | 同一规则在 UI 路线图里的复用："pixel-check with `text2img`+IoU; never assert 'the callback fired'" |
| `internal/shot/shot.go:108-122`（`SurfaceChat` → `captureTextSurface`；CLI `cmd/agent/cli_shot.go --surface chat`） | 把终端界面**画**成 PNG：唯一天然处理字形/字体/颜色的通道。注意它的**输入**已经是文本（pty 字节流 + 单元网格），像素只在**输出端**出现 |
| `devdoc/screen-capture-and-actuation-tools.md:77` | "pixel-level truth: pure-Go capture vs ImageMagick import, same screen" —— 审计**抓图工具本身**是不是真值，这是像素问题 |
| `devdoc/ch9329-hid-actuator-evaluation.md:260,514` | 输入设备/外部 GUI 验证（HID 致动器），"`vision_analyze` only on ambiguity"（已是最省的用法） |

---

## 3. 一句话路由规则（可判定）+ 三条边界

> **路由规则：观察目标是"我们自己、且是文本形态"的界面（`gopi ui` / `gopi chat`
> / 任何 bubbletea 屏）→ 只走文本状态路径（进程内状态导出 / `lineedit.Screen`
> 单元网格；必要时只读重放 pty 字节流进同一模型）；只有目标是**外部** GUI、
> 或问题本身是**像素问题**（字形/字体/颜色/渲染回归的 IoU、抓图工具自身的真值），
> 才允许 pixel / vision。**

判定只需两步：①**这个界面是我们写的吗？** ②**我问的是"内容"还是"像素"？**
两个都答"是"/"内容" → 文本路径。任一为"外部"/"像素" → 像素路径。

**三条边界**：

1. **颜色当前不可断言 —— 已过期（本文实测修正）**。`internal/lineedit/screen.go`
   的 `Line()`/`Contents()`（`:413`/`:427`）确实只给纯文本、丢属性；但
   `internal/lineedit/screenattr.go:103,157,175` 已提供 per-cell SGR
   （`Cell`/`StyledLine`/`StyledContents`，landed 4f56ccd2）。所以正确表述是：
   **"纯文本面（`Contents()`）无属性；属性面存在，用 `StyledContents()`/`Cell()`。
   唯一真正只能靠像素的是**渲染保真度**（字体/glyph/字距/抗锯齿），因为那发生在
   终端/渲染器一侧，不在这份网格里。** 引用旧结论（"颜色不可断言"）前请先看
   `screenattr.go`——`devdoc/cli-as-eyes-and-hands-feasibility.md:26` 已把它标为过期。
2. **渲染质量必须像素**：IoU/字形对照（GOPI.md:325）是唯一能回答"这行字看起来
   对不对"的手段；vision 不能量化质量损失（Rule 10.2）。
3. **越界抓他人窗口有隐私成本**：整屏抓图必然把 operator 的别的窗口一起抓进来
   （本次实测样本里就有 firefox 的 `platform.deepseek.com/usage` 与 gedit 里打开的
   文件；session `01a0d2f0-…` 的 vision 转写里出现了 apikey 文件的内容）。
   "换一个 display"不解决这个（换的是**抓谁**，不是**要不要抓**），operator 明确
   不欣赏该解法。**进程内自观察天然没有这个成本** —— 它只读自己的状态。
4. （附加，成本侧）**同机可能有多个 gopi 界面**（operator 的 `gopi ui --tools` pid 22202
   与 agent 自己的 `gopi chat --tools` pid 22449）。像素路径分不清来源；文本路径各自
   导出各自的。

---

## 4. 诚实反面 —— 文本路径做不到的事

1. **看不到渲染器真正画出来的东西**：字体替换、字形缺失、终端自身的行高/字距、
   抗锯齿、颜色映射到 RGB 之后的实际观感。文本面告诉你"单元格里是 `知`"，不告诉你
   "这个终端把 `知` 画成了豆腐块"。
2. **看不到终端的自适应行为**：tmux/zellij 的状态栏、滚动条、terminal 的 reflow、
   resize 时终端自己的重排（我们能断言**我们**算出来的网格，不能断言 xterm 画的）。
3. **看不到不在我们模型里的东西**：外部进程往同一个 pty 写的字节、operator 手动敲的
   键（除非它进了我们喂的字节流）、别的窗口覆盖在上面。
4. **它只覆盖"我们拥有那个界面"的情况**：agent 无法观察 operator 正在看的、我们没有
   附着点的终端（无 tmux server 可 attach），这一点像素路径反而"能"（代价是隐私与噪声）。
5. **属性面还不是渲染面**：`StyledContents()` 给的是 SGR 风格值，不是像素；"这行是
   灰色、那行也是灰色"能断言，"这两个灰看起来一样吗"不能。
6. **文本路径今天还不是模型可调用的成品**：本文量的是一份**测试路径导出的文本**
   （`internal/uiclient` 的 dump 钩子 + `lineedit.Screen`），不是模型随时能叫的一个
   观测工具；可调用化是另一个任务（parent 行的兄弟任务），本文只证明"值不值得做"。

---

## 5. Not Done（honest）

1. **没有产出任何产品能力**：本文只证明"值不值得做"，没有加一个模型可调用的观测
   入口（那是兄弟任务的事），也没有改 `screen_capture`/`vision_analyze` 的描述文本
   （§2 的"应替换"三行只是**待办清单**，不是已完成的改动）。
2. **文本路径的采纳率无法测**：Adoption 只对**已存在且被路由**的工具可测
   （Rule 11）。文本路径今天只有测试钩子（`GOPI_UI_SCREENMODEL_DUMP`、
   `GOPI_PALETTE_CAPTURE_DIR`），没有工具槽，因此本文不报它的采纳率。
3. **像素路径的成本只覆盖"抓 + 读"**：未计入 agent 侧把 PNG 放进上下文的二次成本
   （一屏 307 KB 走 base64 约 +410 KB/次），也未做图文混排的长会话实验。
4. **窗口结构路径只测了 X11 顶窗层**：Wayland（HP 那台是 rootless Xwayland）上
   `xprop` 的表现未测；该路径在本审计里的定位是"举一个比像素便宜但仍弱于文本的
   中间档"，不是推荐解法。
5. **金额/时间未量化**：只给字节与 token，不给"省了多少钱/多少秒"——那需要真实
   调用计费，本文不给估算数字（估算是另一种拍脑袋）。

## 6. Verification（Rule 5 / Rule 31：跑过的面）

- **Build**: `go build ./...` → 见 commit 前的实际输出（本审计未改产品代码）。
- **Tests**: `go test ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile|TestDocManifestRowsAreWellFormed'`
  → `ok github.com/the operator/gopi/internal/standards`；日志 `2293 rows cover 2293 files`、
  `523 normative rows (each carrying a spot-check)`。
  `go test ./internal/agent/ -run TestDocAnchor` → 本文件贡献 **0** 条 anchor 失败
  （grep 文件名无输出）；该 gate 本身仍红，原因是与本文无关的既有 baseline drift。
  `go test ./internal/standards/ -run TestDocClassBannerMatchesManifest` → 仍红，
  点名的是两份**未被我改动**的文档（`devdoc/handoff-20260924-preflight.md`、
  `devdoc/worker-resume-ground.md` 缺 banner）；HEAD 自身的提交信息即写明
  "the gate was red on master before my handoff landed"。
- **Ran**: `GOPI_UI_SCREENMODEL_DUMP=… go test ./internal/uiclient/ -run TestRealToolCallTurnScreenModelIsAgentReadable -v -count=1`
  （§1.1 C 的原文）与 `GOPI_PALETTE_CAPTURE_DIR=… go test ./internal/lineedit/ -run TestPaletteCaptureGolden`
  与 `screen_capture(display=localhost:15.0, out=…/ui-state.png)`（像素样本 1 次）。
- **Result**: 文本导出 2310 B、0 个 ESC 字节、6 条 ASSERT 全 pass；像素样本
  `bytes=307625 sha256_16=36a034eba1369c9a`；窗口结构 `113 B`/`753 B`。
- **Commit**: 见本文件所在分支的提交（`devdoc/ui-entropy-audit.md` +
  `devdoc/DOC-INDEX.tsv` 一行）。

## 7. Evidence (self-filed)

- 像素样本（1 次，本任务）：`screen_capture display=localhost:15.0 out=…/ui-state.png`
  → `bytes=307625`，`sha256_16=36a034eba1369c9a`，`1920x1080x24`。未再抓图、未调 vision。
- 文本导出：`GOPI_UI_SCREENMODEL_DUMP=… go test ./internal/uiclient/ -run TestRealToolCallTurnScreenModelIsAgentReadable -v`
  → `--- PASS` + 6 条 `ASSERT … pass=true`；`wc -c` = `2310`；`grep -c $'\x1b'` = `0`。
- 窗口结构：`wc -c /tmp/p7_win_ids.txt` = `113`；`wc -c /tmp/p7_win_names.txt` = `753`（7 窗口）。
- 既有记录（标注来源 = session `01a0d2f0-4e6e-7638-84ad-3eb89e329bb1`，2026-09-24）：
  `bytes:350716`（整屏）、`bytes:158684` + `输入 683 tokens / 输出 751 tokens`（窗口）、

  587/192/271 input tokens、空内容、一屏 2 页；`:27` 的采纳率。
- 属性路径：`GOPI_PALETTE_CAPTURE_DIR=… go test ./internal/lineedit/ -run TestPaletteCaptureGolden`
  → `ok github.com/the operator/gopi/internal/lineedit 0.304s`；`capture-colour.ansi` 505 B、
  `capture-mono.txt` 407 B、`capture-chrome.ansi` 377 B。
- 只读保证：本次会话未改任何产品代码；`git status --porcelain` 结束时为空
  （改动仅 `devdoc/ui-entropy-audit.md` + `devdoc/DOC-INDEX.tsv`，见 commit）。
