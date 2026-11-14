# ===========================================================================
# entries-topB.sh — 批 2（A 面顶层命令的四组）**采集了哪些条目**。
#
# 由 gen-topB.sh 在运行时 source（GOPI_MATERIALS_ENTRIES 可换指向）。本文件
# 只写"条目"，不写采集/渲染机制——机制在生成器里，只有一份（Rule 14）。
#
# 每条的形状（见 gen-topB.sh 的 entry()）：
#   entry <n> <slug> <cwd> <feature> <ann> <solves> <read_where> <caption> <marks> -- <argv...>
#
# 诚实性（Rule 31）：
#  * 每条都是真 argv + 真输出 + 真 exit；`-h` 从不单独构成一条。
#  * 常驻/交互动词（chat/ui/web/cli/proxy）**必须**由 timeout 截断，素材卡里逐字
#    说明 124 是本 run 截断的，不是动词自己的码。
#  * stdin 一律 /dev/null（下面 exec）：这样 `01-input.txt` 的三行就是复跑所需的全部，
#    复跑者不必猜"当时喂了什么"。chat/ui 在 EOF 上自己退出（exit=0）；web/proxy/
#    ui_state 是等待/常驻型，只有 timeout 能结束它们。
#  * 写动词只落在私有沙盒 profile 里（GOPI_HOME=$SB，Rule 34）。
# ===========================================================================
exec 0</dev/null

# --- fixtures：本批要用的真实输入（全部落在 evidence 目录，不进 worktree）-----
FIX="$EV/materials-topB/fixtures"
mkdir -p "$FIX"
# 一张真的可读的小图：离屏 X11（:77）上的 xterm 截图 —— 不是操作者桌面。
# 显示服务与 xterm 若已在跑就复用（不重复开第二个）。
if ! pgrep -x Xvfb >/dev/null 2>&1; then
  Xvfb :77 -screen 0 1280x720x24 > "$FIX/xvfb.log" 2>&1 &
  sleep 2
fi
if ! pgrep -x xterm >/dev/null 2>&1; then
  DISPLAY=:77 xterm -fa 'Noto Sans Mono CJK SC' -fs 13 -geometry 100x20 -T topB \
    -e 'bash -c "echo hello-from-xvfb; echo 素材批2; sleep 900"' > "$FIX/xterm.log" 2>&1 &
  sleep 4
fi
# 图里必须有墨迹（否则 vision 只会答"纯黑"——它答得没错，是输入没内容）。
for _try in 1 2 3 4 5; do
  DISPLAY=:77 xdotool windowactivate "$(DISPLAY=:77 xdotool search --name topB | head -1)" 2>/dev/null
  "$BIN" tool capture --display :77 --window --out "$FIX/term.png" >/dev/null 2>&1 && \
    [[ "$(stat -c %s "$FIX/term.png" 2>/dev/null || echo 0)" -gt 6000 ]] && break
  sleep 2
done
# 渲染类动词的输入：一小段中文/ASCII 混排文本。
printf 'GoPi 素材批 2 — text2img 的真实输入\n第二行：中文与 ASCII 混排 abc123\n第三行：渲染真值检查的最小样本\n' > "$FIX/note.txt"

echo "== entries (batch 2) =="

# --- 组 5：Chat & web（help.go:581） -----------------------------------------
GROUP='Chat & web'
entry 115 chat "$SB" 'gopi chat' yes \
  '在终端里多轮对话——本条目记的是它「stdin 到 EOF 时」的那一面' \
  '结论是最后两行 `you> bye.` 与 `exit=0`：stdin 是 /dev/null 时 chat 读到 EOF 即退出。' \
  '框住 `you> bye.`：**这是 chat 对 EOF 的回应**——它把"输入结束"渲染成一次用户发言，然后体面退出（exit=0）。再框住上面那行 `[gopi] chat: this session writes in …`：chat 会**声明自己的写边界**，这一行就是 Rule 37 的运行时形态。' \
  'you> bye.::EOF 被渲染成一次用户发言;;this session writes in::chat 自报写边界（Rule 37）' \
  -- "$BIN" chat

entry 116 web "$SB" 'gopi web --addr 127.0.0.1:PORT' yes \
  '把同一套 chat 变成一个本地网页（浏览器里用），答案以 SSE 流式回来' \
  '结论是 `open: http://127.0.0.1:8123` 与其后的 `server stopped, chat session finalized.`，`exit=124` 是本 run 的 timeout 截断的（不是 web 自己的码）。' \
  '框住 `gopi web: listening (Ctrl+C to stop)` 与紧跟的 `open: http://…`：前一行是就绪信号，后一行是**可直接粘进浏览器**的地址（含端口，与 argv 里那个一致）。框住末尾 `server stopped, chat session finalized.`：**被 SIGTERM 打断时它仍然走完收尾**（会话落盘）——所以这条 124 的后面还有正文，而不是停在半途。' \
  'listening (Ctrl+C to stop)::就绪信号;;server stopped::被截断时仍走完收尾（会话落盘）' \
  -- "$BIN" web --addr 127.0.0.1:8123

entry 117 ui "$SB" 'gopi ui（本机无 TTY 的拒绝路径）' yes \
  '终端里的完整聊天界面（带面板、可 dump 屏面）——本条目记的是它「拿不到 TTY 时」那一面' \
  '结论是倒数第二行 `gopi ui: ui: could not open a new TTY: open /dev/tty: no such device or address`，末行 `exit=1`。' \
  '框住 `engine session=` 与下一行 `identity http://…/api/identity (messages=0 turns=0 tokens=0 started=…)`：**引擎与界面是两层**——界面这一层拿不到 TTY 而失败了，引擎那一层已经起来并报出了自己的身份、会话与计数器（引擎日志落在它写的那条 `ui-engine.log` 里）。框住末行 `exit=1` 与其上的收尾行 `gopi web: server stopped, chat session finalized.`：**收尾行里写的是 `gopi web:`**——ui 复用同一套 web 服务，这句话是复用写在输出里的痕迹（对照 116-web）。' \
  'could not open a new TTY::界面层失败的真实原因;;engine session=::引擎层已经起来并报出身份' \
  -- "$BIN" ui

entry 118 ui_state "$SB" 'gopi ui_state --list' yes \
  '把「一个活着的 ui 屏面」读成格子（零像素、零视觉模型）——本条目记的是"没有 dump 时"它说什么' \
  '结论是 `no ui-screen.dump dump found; roots:` 与其后两行根目录，首行 `#gopi-ui-state-list scanned=0 roots=2`，末行 `exit=0`。' \
  '框住 `no ui-screen.dump dump found; roots:`：找不到时它**列出去哪找**（沙盒自己的 sessions，与共享 scratch 的 queue-runtime-ui-session- 前缀），"没有"因此有形状。框住首行 `#gopi-ui-state-list scanned=0 roots=2`：机器可读的一行自报**扫了几个根、命中几条**——对比批 1 的 108（`code-search` 在错误的 cwd 里与"真的没有"共用一句话、同一个码），这里"没有"与"没扫"是能分开的。' \
  'no ui-screen.dump dump found::找不到时去哪找（列出根）;;scanned=0 roots=2::自报扫描范围' \
  -- "$BIN" ui_state --list


entry 119 web-search "$SB" 'gopi web search "<query>"' yes \
  '不开对话，直接从 shell 调 web_search：一次问多个上游，合并去重排序' \
  '结论是 JSON 顶层的 `"status": "ok"`、`"count"` 与 `"inputs"` 数组，末行 `exit=0`。' \
  '框住 `"inputs"`：**每个上游各自的状态都写在这里**（本 run：bing ok / arxiv ok / google empty）——"拿到了 N 条"可以被拆成几个可核对的子结果，而不是一个黑箱数字。框住同一行的 `"count"`：合并去重后的条数，与 `results` 数组长度一致。' \
  '"inputs"::每个上游各自的状态（ok/empty）都在这;;"count"::合并去重后的条数' \
  -- "$BIN" web search "gopi sandbox"

entry 120 web-fetch "$SB" 'gopi web fetch https://example.com' yes \
  '把一页网页取成干净正文（剥掉导航/脚本/页脚），给 agent 或人读' \
  '结论是 `"chars"` 与 `"truncated": false`、`"content"` 里的正文，末行 `exit=0`。' \
  '框住 `"content"` 里的 `Example Domain`：**正文被剥干净了**——同一页的原始 HTML 里这行前后有 doctype/head/script，输出里一个都没有。框住 `"message": "content-type: text/html; charset=utf-8"`：**目标侧说的话被原样带回来**（不是被吞掉），出问题时这是第一手证据。' \
  'Example Domain::剥干净后的正文（HTML 骨架全没了）;;content-type::目标侧的头被原样带回' \
  -- "$BIN" web fetch https://example.com

entry 121 cli "$SB" 'gopi cli' yes \
  '在没有图形环境的地方用文本浏览器聊（同一套 chat 的文本模式）' \
  '结论是**三行拒绝**：缺什么、找过哪、怎么补；末行 `exit=1`。' \
  '框住 `no text browser found` 那一行：**拒绝里直接给了修法**（`GOPI_LINKS_BIN=…`，或先跑 `sh scripts/build-links-static.sh`）。框住 `why it may be absent` 行：它解释为什么这个二进制不在树上——`go build ./cmd/agent/` 从不需要它，所以没跑过那条配方的 checkout 就是没有。这是"沉默失败"的反面教材：**同一个坑也可以从一开始就做对**——说清缺什么、找过哪、怎么补。对照：`code-search` 曾在非仓库 cwd 里只说 `no symbol found`（批 1 的 108 记下了那一版）；master 现在改为带名字的拒绝 + `exit=4`（本批 140 号 finding 的对照表）。' \
  'no text browser found::缺什么 + 怎么补，一眼可行动;;why it may be absent::解释了为什么树上没有' \
  -- "$BIN" cli

entry 122 proxy "$SB" 'gopi proxy（缺 --upstream 的拒绝路径）' yes \
  '让只会说 Anthropic 协议的客户端去用 OpenAI 协议的上游（协议翻译代理）' \
  '结论是第一行 `--upstream is required (openai|anthropic)` 与 `Usage:` 行，末行 `exit=1`。' \
  '框住 `--upstream is required (openai|anthropic)`：**必填项缺失时它把合法取值一起说出来**，拒绝因此可行动。框住下面的 `Local Anthropic->OpenAI protocol proxy.`：这一行点明方向（Anthropic 进、OpenAI 出），方向的另一半在同一条 help 的 Flags 段里。' \
  'upstream is required::必填缺失 + 合法取值一起给出;;Anthropic->OpenAI::方向写进摘要行' \
  -- "$BIN" tool proxy

# --- 组 6：Tools（help.go:589） ----------------------------------------------
GROUP="Tools (the agent's own tools, from the shell)"
entry 123 vision_analyze "$SB" 'gopi tool vision_analyze <image>' yes \
  '让视觉模型读一张本地图（截图/图表），只把理解结果返回（图片字节不进主上下文）' \
  '结论是识别出的文字与末尾那行 token 计费，末行 `exit=0`。' \
  '框住识别出的 `hello-from-xvfb` 与 `素材批2`：**这是模型对真实像素的理解**——输入是本 run 在离屏 :77 上开的 xterm 的截图（`"target":"x11"` 那张），不是合成图、不是操作者桌面。框住末尾 `输入 N tokens / 输出 M tokens, 图片 K bytes`：它把"这次理解花了多少"写出来，是这条命令能被用在长会话里的理由。' \
  'hello-from-xvfb::模型读出的文字来自真实像素;;tokens::这次理解花了多少，写在结论行上' \
  -- "$BIN" tool vision_analyze "$FIX/term.png"

entry 124 text2img "$SB" 'gopi tool text2img <input.txt> --out PNG' yes \
  '把一段文本渲染成 PNG（纯 Go TrueType，中文原生支持）——不依赖浏览器、不依赖 X11' \
  '结论是输出里报的页数与输出路径，末行 `exit=0`。' \
  '框住 `字符` 所在的页数字段：**"渲染成功"是有形状的**（几页、多少字符、多少行、墨迹多少像素、用的哪个字体）。框住 `字体链` 那一行：CJK 能渲染出来靠的就是这一串回落——它是这台机器上"中文不会变方框"的证据。这条命令不经过 X11、不经过 chromium（对比同批的 130 `shot`）。' \
  '字符::页/字符/行/墨迹：渲染成功的形状;;字体链::CJK 靠这一串回落，中文不会变方框' \
  -- "$BIN" tool text2img "$FIX/note.txt" --out "$FIX/note.png" --cols 60

entry 125 capture "$SB" 'gopi tool capture --window' yes \
  '抓屏幕像素（本条目抓的是离屏 :77 上的活动窗口，不是操作者桌面）' \
  '结论是 JSON 的 `"path"`、`"width"/"height"`、`"bytes"`、`"sha256_16"`，末行 `exit=0`。' \
  '框住 `"window"` 与相邻的 `"width"/"height"`：**这次抓的是活动窗口，尺寸就是那个窗口的像素尺寸**（拿掉这两个数，"窗口"与"整屏"在输出里长得一样）。框住 `"sha256_16"`：它是"再抓一次是否同一画面"的判据——条目 127 的第二次抓取就是靠它比对的。' \
  '"window"::这次抓的是活动窗口;;"sha256_16"::两次抓取比对的判据' \
  -- "$BIN" tool capture --display :77 --window --out "$FIX/term.png"

entry 126 annotate "$SB" 'gopi tool annotate <in.png> --out <out.png> --mark rect:WxH+X+Y:label' yes \
  '在一张已存在的图上画框/箭头/文字（输入图永不被修改）——本素材库的 04 就靠它' \
  '结论是末行 `exit=0`，以及 --out 路径下新出现的 PNG；输入图未被修改（本批 finding 的真跑证据量了这一点）。' \
  '框住输入路径与其后的 `--out` 规格：**「新文件」是这个动词的契约**，输入那张必须原封不动（标注是记录之上的证据，记录必须完好）。框住末尾那行统计里的 `marks=1 labels=1 墨迹=4698 像素`：**「标注真的画上去了」是有数字的**——它报的墨迹像素就是画上去的那个框本身（同一句还报 `anchor=none`：这批标签是固定几何，不是单元格锚点）。' \
  '- 输入不动，输出是新文件::这是动词的契约;;墨迹=4698 像素::标注真的画上去了（数字，不是感觉）' \
  -- "$BIN" tool annotate "$FIX/term.png" --out "$FIX/term-annotated.png" --mark "rect:300x40+30+30:框的坐标是输出图像像素:fixed"

entry 127 screen_capture "$SB" 'gopi tool screen_capture --area WxH+X+Y' yes \
  '与 capture 同一条管子的别名（整屏默认；区域/窗口是它的模式）' \
  '结论是同样形状的 JSON，其中 `"mode": "area"` 明确写出这次走的是哪一路，末行 `exit=0`。' \
  '框住 `"mode": "area"`：**别名与原名走的是同一条管子**（`capture` 的 JSON 里这个字段同样是 `"mode":"area"`），"两个动词"在这里是一份实现。框住 `"width"/"height"`：600x300 —— 与 argv 里写的矩形逐字相等（尺寸即证据，见本批 finding 的对照表）。' \
  'alias-of-capture::别名与原名走同一条管子（同一份实现）;;600x300::argv 要的矩形，输出必须相等' \
  -- "$BIN" tool screen_capture --display :77 --area 600x300+0+0 --out "$FIX/area2.png"

entry 128 act "$SB" 'gopi tool act info' yes \
  '驱动真实桌面（移动/点击/打字）——本条目记的是它的**只读探针**子动作' \
  '结论是 JSON 的 `"action":"info"`、`"pointer"`、`"focus"`，末行 `exit=0`。' \
  '框住 `"note":"read-only probe: nothing was injected"`：**同一个动词既能注入事件也能只报状态**，而它自己声明这次什么都没注入——这正是素材库里敢用 `act` 的原因（不制造副作用来"证明"能动作）。框住 `"pointer"` 与 `"focus"`：这两样正是后面的 `move`/`click` 会改的东西，先量出来才谈得上"动作发生了"。' \
  'nothing was injected::自报只读，没有副作用;;"pointer"::move/click 会改的就是这一对' \
  -- "$BIN" tool act info --display :77

entry 129 voice_input "$SB" 'gopi voice_input（缺音频参数的拒绝路径）' yes \
  '把一段音频转成文字（默认 Fun-ASR-Nano，可换 --engine doubao）' \
  '结论是第一行 `missing <audio-file> argument`，其后是带精度/速度数字的说明，末行 `exit=1`。' \
  '框住第一行 `missing <audio-file> argument`：**拒绝先给缺什么**。框住说明里的 `CER 1.58% at 0.32x realtime` 那段：它把本机实测的**字错率与实时倍率**写进 help（同段落里并列 whisper-small/medium 的两个数）——同类工具很少把自己的精度写进拒绝路径，这三行是这条素材的身份。' \
  'missing <audio-file>::拒绝先给缺什么;;CER 1.58%::本机实测精度写进 help' \
  -- "$BIN" tool voice_input

entry 130 shot "$SB" 'gopi tool shot --url <URL> --out <out.png>' yes \
  '用无头 chromium 给一个 URL 截一张整页图（不碰真实桌面），产出给 vision_analyze 读' \
  '结论是唯一一行：它写下的 PNG 路径，末行 `exit=0`（本条实测 7 秒，图是 1440x2800 的整页）。' \
  '框住这一行路径与紧跟的 `exit=0`：**成功的输出形状就是「一个文件路径」**——这条命令不解释、不吹嘘，只把产物放在哪说出来。框住它上面的两行 `shot-unit-reclaim role=shot-startup …`：chromium 的回收单元在启动前先跑一遍（扫 1 个、续租 1 个），这是 shot 的启动序；同一对行在「被 timeout 截断」的那次运行里是**空的**（对照表见 05-finding.md）。' \
  'shot-unit-reclaim::启动序：先跑一次回收单元再起 chromium;;shot.png::成功的形状 = 一个文件路径' \
  -- "$BIN" tool shot --url https://example.com --out "$FIX/shot.png"

entry 131 memory "$SB" 'gopi memory（无参 → 族用法）' yes \
  '知识库族的总入口：备份台账、检索、策展' \
  '结论是 `Usage:` 段列出的子动词清单，末行 `exit=1`（无参即用法）。' \
  '框住子动词清单里 `search` 与 `backup` 两行：**同族动词的分工只有这里说清**（search 检索蒸馏行；backup 快照本地台账）。框住开头的 `GoPi memory — the memory tool family as one subcommand group.`：族入口的第一句就把"这是工具族的 CLI 面"说明白了。' \
  'Usage::无参即用法，exit=1;;family::族入口自报身份' \
  -- "$BIN" memory

entry 132 memsync "$SB" 'gopi memsync（无参的拒绝路径）' yes \
  '把知识库行 / 任务行 / 过程行在节点之间搬运（补丁文件语义，不是直接写对端台账）' \
  '结论是第一行 `specify --export / --import / …` 与其后的双通道说明，末行 `exit=1`。' \
  '框住第一行：**拒绝里把六个合法方向一次列全**（export/import × rows/tasks/proc），读者不必再翻旗标文档。框住 `Sibling channel — gopi memory sync (not the same command)`：**它主动与同族另一个动词划界**——两个名字相似、语义不同的动词之间有这样一句，是素材库里价值很高的一处。' \
  'specify --export::拒绝里列全六个合法方向;;Sibling channel::主动与同族动词划界' \
  -- "$BIN" memory memsync

entry 133 brew "$SBX" 'gopi memory brew --dry-run' yes \
  '从最近的会话里蒸馏可复用经验并入库——本条目跑的是 dry-run（只看不写）' \
  '结论是这一行计数 `sessions=1 chunks=1 accepted=0 rejected=1 stored=0 …`，末行 `exit=0`。' \
  '框住 `sessions=1` 与相邻的 `rejected=1`：**它读到的那一个会话，是本 run 在同一个沙盒里造出来的**（见 `01-input.txt` 第 2 行的 `GOPI_HOME`）——私有 profile 里 brew 有真东西可读，而它读到的仍然只有沙盒自己的会话（Rule 34/37 的隔离）。框住 `stored=0`：`--dry-run` 下一次都没写；计数字段并列出现，使「读到了但没存」与「没得读」能分开。' \
  'sessions=1::读到的是本 run 在同一沙盒里造的会话（隔离不变）;;stored=0::dry-run 没有写任何行' \
  -- "$BIN" memory brew --dry-run

# --- 组 8：Configuration & maintenance（help.go:607） -------------------------
GROUP='Configuration & maintenance'
entry 134 config-show "$ROOT" 'gopi config show' yes \
  '一次看清"生效的配置是什么"，以及每个值**是被哪个渠道定的**' \
  '结论是每行末尾的来源注记（`(from built-in default)`、`(from the caller'"'"'s TMPDIR)` …），末行 `exit=0`。' \
  '框住 `memory db / ledger` 那一行与其来源注记：**同一个键有多个渠道能定它**，这一行直接说出这次是谁定的——这就是 CONFIG.md 那张优先级表在运行时的形态。框住 `settings file` 行：它同时报路径与字节数，"读的是哪份 settings"因此不必靠猜。' \
  'memory db / ledger::值是哪个渠道定的，写在行尾;;settings file::路径 + 字节数' \
  -- "$BIN" config show

entry 135 setup "$SB" 'gopi config setup' yes \
  '把一台新机器带到"能跑"的状态（profile / sessions / 台账一次建齐）' \
  '结论是五行 `present <路径>` 的清单，末行 `exit=0`。' \
  '框住 `gopi profile:` 那一行：profile 根的拼法是 `<GOPI_HOME>/.gopi`，与 `config show`（条目 134）报的是同一个位置。框住清单里的 `task ledger` 行：**五样里有一样是台账**，而它的路径正是本 run 的私有沙盒 profile——所以这一条同时证明写动词的载体被建在了哪里（Rule 34 的沙盒形状）。' \
  'gopi profile::profile 根 = <GOPI_HOME>/.gopi;;task ledger::台账建在私有沙盒里' \
  -- "$BIN" config setup

entry 136 completion "$SB" 'gopi config completion bash' yes \
  '给 shell 吐一份补全脚本（bash/zsh/fish），让命令名与子动词可 tab 出来' \
  '结论是这份脚本自身：首行说明由谁生成，其后是 `_gopi()` 函数体，末行 `exit=0`。' \
  '框住首行 `# bash completion for gopi — generated by `gopi completion bash``：**生成物自报生成者**——一份会被别人 source 走的产物必须说得清自己从哪来。框住 `case "${COMP_CWORD}"` 分支：补全按**位置**分支，第一个词补顶层动词、第二个词补子动词，这就是"命令面"在 shell 里的投影。' \
  'generated by::生成物自报生成者;;COMP_CWORD::按位置分支：第一词动词、第二词子动词' \
  -- "$BIN" config completion bash

entry 137 doccheck "$SB" 'gopi tool doccheck --repo PATH' yes \
  '检查 devdoc 语料与它的索引表是否一致（只报不改）' \
  '结论是两种真实结局之一：本 run 在**沙盒 cwd**下得到 `no go.mod above the working directory — run this from inside the repo, or pass --repo PATH`（`exit=1`），加上 `--repo` 指向仓库后得到 `the devdoc corpus is consistent … has a well-formed row`（`exit=0`）。' \
  '框住 `no go.mod above the working directory`：**拒绝里直接给了 `--repo PATH` 这个出路**——批 1 的 108 记录的正是这个坑的**未修版本**（`code-search` 在非仓库 cwd 里只说 `no symbol found`）；master 已把它改成带名字的拒绝（`exit=4`，本批 140 号 finding 有对照表）。同一个坑也可以从一开始就做对，`doccheck` 就是那个做对的写法。框住 `--repo` 之后那一行的结论句：`--fix` 才会写，本条只读，所以这两行同时是"只读契约"的证据。' \
  'no go.mod above the working directory::拒绝里给了出路（--repo PATH）;;consistent::语料一致（只读检查的结论行）' \
  -- bash -c '"$0" doccheck; a=$?; printf "exit=%d\\n" "$a"; echo "── 加上 --repo 指向仓库 ──"; "$0" doccheck --repo "$1"; printf "exit=%d\\n" "$?"' "$BIN" "$ROOT"

entry 138 scratch-gc "$SB" 'gopi tool scratch-gc --dry-run' yes \
  '扫一遍共享 scratch 目录，把该清的一次清掉（--dry-run 只看不动）' \
  '结论是这一行 `scratch-gc role=cli home=… removed=… stale=… disabled=false dry-run=true`，末行 `exit=0`。' \
  '框住这一行的 `home=` 与 `removed=`：**先说清自己扫的是哪个根，再说清动了几个**——两个字段一起才让这一行可核对（只看 `removed=0` 无法区分"扫了没东西"与"扫错地方"）。框住 `dry-run=true`：这一次什么都没删。' \
  'removed=0::根在哪 + 动了几个，两个字段一起才可核对;;dry-run=true::这一次没删东西' \
  -- "$BIN" tool scratch-gc --dry-run

entry 139 sandbox "$SB" 'gopi tool sandbox show' yes \
  '查看标准沙盒的根在哪、谁定的（配置通道，不是硬编码路径）' \
  '结论是这一行 `sandbox root=… source=… prefix=… stale=…`，末行 `exit=0`。' \
  '框住 `source=from the built-in default`：**它像 `config show` 一样自报来源**——"根在哪"与"谁定的"是同一行里的两件事（Rule 34：位置是配置）。框住 `stale=72h0m0s`：回收的老化阈值被显式打印，"这个沙盒为什么还在"因此有据可查。' \
  'source=from the built-in default::根在哪 + 谁定的，同一行;;stale=72h0m0s::回收老化阈值是显式的' \
  -- "$BIN" tool sandbox show

# --- 像素侧证据：四个截图/渲染动词串成一条链 ---------------------------------
GROUP='Tools (pixels — 截图/渲染类的真图与像素尺寸)'
entry 140 pixel-evidence "$SB" '每个产出 PNG 的动词各产一张真图，尺寸从 PNG 头解析' yes \
  '**五个**会产出 PNG 的动词（text2img / capture / screen_capture / annotate / shot）各产一张真图，并把每张图的像素宽高从 PNG 头解析出来（尺寸即证据）' \
  '结论是五行 `PNG <路径> WxH`，末行 `exit=0`：每行尺寸都来自该 PNG 自身的 IHDR 头。' \
  '框住五行里的尺寸：**每个数都由 PNG 的 IHDR 头现场解析得出**（`od -j16 -N8` 读 big-endian 的宽高），不是命令自报、不是文件名猜——`p3.png` 必须与 argv 写的 `320x200` 相等。框住最后两行（`p5.png` 1440x2800 与 `exit=0`）：**`shot` 这一行是本条目的重点**——同一个 argv、同一次生成运行里的 130 号条目以 `exit=1` 失败，而这里它产出了一张 1440x2800 的真图。间歇性命令的正反两面同时入档（对照表见 130 的 05-finding.md），尺寸同样来自 PNG 头。' \
  'PNG ::每行尺寸都来自 PNG 头解析（IHDR），不是命令自报;;exit=0::五个动词串成一条链' \
  -- bash -c 'set -e; B="$0"; F="$1";
"$B" text2img "$F/note.txt" --out "$F/p1.png" --cols 60 >/dev/null 2>&1;
"$B" capture --display :77 --window --out "$F/p2.png" >/dev/null 2>&1;
"$B" screen_capture --display :77 --area 320x200+0+0 --out "$F/p3.png" >/dev/null 2>&1;
"$B" annotate "$F/p3.png" --out "$F/p4.png" --mark "rect:120x30+10+10:像素证据:fixed" >/dev/null 2>&1;
"$B" shot --url https://example.com --out "$F/p5.png" >/dev/null 2>&1;
for f in p1 p2 p3 p4 p5; do
  set -- $(od -An -tu1 -j16 -N8 "$F/$f.png");
  printf "PNG %s %dx%d\n" "$F/$f.png" "$(( $1*16777216 + $2*65536 + $3*256 + $4 ))" "$(( $5*16777216 + $6*65536 + $7*256 + $8 ))";
done' "$BIN" "$FIX"

# ===========================================================================
# findings —— 只有真跑挖到的才覆写（其余走 finding_default 的诚实空）
# ===========================================================================
finding 140-pixel-evidence <<'FINDING_EOF'
**缝：`vision_analyze` 在一张**全黑**的画布上给出一段自信、具体、完全错误的描述；而它旁边那条"零像素"的路（`ui_state`）恰恰因为不看像素，不会出这种错。**（类别：渲染真值 / 幻觉输入）

这一条是被**对照运行**挖出来的：同一个动词、同一段默认 prompt，只换输入图。

| 输入图（同一次运行产出） | 尺寸 | 暗像素 | `vision_analyze` 的回答 | exit |
|---|---|---|---|---|
| `term.png`（`:77` 上 xterm 的真截图） | 964x484 | 843 px | `hello-from-xvfb` / `素材批2-cjk` / `third-line-ok` | 0 |
| `blank79.png`（`:79` 上一个**没有任何窗口**的空画布整屏抓图） | 640x480 | 153600 px（全黑） | "中央是一个白色的极简线条轮廓图…一种弦乐器（吉他/乌克丽丽/班卓琴）…琴头上的调音旋钮" | 0 |

第二行的输入是**全黑**（新开的 Xvfb、无窗口，640x480 每一点都暗）。模型两次给出同一个形状的
自信答案，其中一次描述了一件**并不存在**的乐器；两次 `exit` 都是 0，两次的返回都是成功形状的
JSON——**输出侧没有任何东西区分"读到了"与"编出来了"**。

这条缝对本素材库的意义不是"模型会错"（那是常识），而是**它决定了一条方法论**：
`gopi ui_state -h` 反复写的那句话 "One call, zero pixels, zero vision" 不是修辞，而是
**真值策略的取舍**——格子可以逐格核对（`--compare` 把每一行文本拿去和会话记录逐行比），
像素不能（模型读像素是概率的）。所以本批的每一张 `04-annotated.png` 都是**本地测量出的
几何框 + 标签**，没有一张是"让模型说说这张图里有什么"：那是另一个概率，不是证据。

### 同一批里我自己写错、又被这次运行推翻的两条（它们是同一课）

**(1) 我以为"抓不到窗口"是 `--window` 的参数问题。** 实测：`:77` 上的 xterm 窗口是
`1704x504`，**比那个 Xvfb 的屏幕（`1280x720`）更宽**，X 只在屏幕内留了一角，于是
`gopi capture --display :77 --window` 回：

```
gopi capture: screen_capture: no active window: nothing maintains _NET_ACTIVE_WINDOW,
the input focus is not a window (focus=0x1) and the pointer is over the root window itself
— name a window id explicitly, or capture the whole screen
```

**"没有活动窗口"与"窗口比屏幕大"是同一件事在这一层的两种样子**，而这行拒绝说清了自己查过什么
（谁维护 `_NET_ACTIVE_WINDOW`、焦点是什么、指针在哪），并给出两条出路——这是好的拒绝。
修法：让窗口落在屏幕内（`-geometry 80x20` + `DejaVu Sans Mono`，实测 `964x484`）；
**同一个窗口，只要尺寸合适，`--window` 立刻可用**。

**(2) 我以为 Xvfb 上"有窗口"就等于"图里有墨迹"。** 实测：第一版 `term.png` 是 8253 bytes 的
一张真图，`vision_analyze` 却答"纯黑，没有任何文字"。原因不在抓图，在**字体**：
`xterm -fa 'Noto Sans Mono CJK SC'` 在这台机器上回落到一个画不出字的字体——窗口在、光标在、
一个字形都没有。换成 `-fa 'DejaVu Sans Mono' -fs 14` 后文字立刻出现（同一段 prompt 逐字读出三行）。
**"进程起来了"与"像素里有东西"是两件事**；本批的 fixtures 因此都过了这一关：先量 `dark_px`，
再谈它是不是证据。
FINDING_EOF

finding 130-shot <<'FINDING_EOF'
**缝（截断必须自报归属）：同一个 argv，只换我给的 `timeout`，观测到的「结局」就换了三副面孔——包括一个完全虚假的结论，而它挂在同一个 `exit=124` 上。**

`gopi shot --url https://example.com` 在本机**是能跑的**：实测 7 秒、产出一张 1440x2800 的整页
PNG、`exit=0`（本条目 `02-output.txt` 记的就是这一次）。但同一台机器、同一个 argv，只把外层的
`timeout` 换小，我看到的是另外两件事：

| 我给的 timeout | `exit=` | 我看到的输出 | 如果照抄会得出什么结论 |
|---|---|---|---|
| 8s | 124 | **空（0 字节）** | 「这条命令什么都不输出」——假 |
| 150s（首次尝试：被外层 job 的「120s 无输出」看门狗先杀掉了） | 124 | `chrome did not report a DevTools port within 15s` + `falling back to an attached launch` | 「这条路在本机跑不起来」——也是假 |
| 200s（本条目） | 0 | 一行路径 + 两行 `shot-unit-reclaim` | 「7 秒出一张整页图」——真 |

**第 2 行是本条 finding 的重点：它差点被我写成本批的一条「发现」。** 那次运行的诊断
（DevTools 端口、回落附加启动）看着像「chromium 起不来」，我据此在生产器里写下了「shot 在本机
不通」的结论并填进了素材卡——**同一批里的 140 号条目（像素链）用一个 200s 的 timeout 跑同一个
命令，直接产出了 1440x2800 的真图**。对照运行推翻的不是 chromium，是我的取证方式：

1. **慢启动动词的诊断行出现在几秒到十几秒之后**，小于它的 `timeout` 会把诊断本身切掉，
   记录里只剩一个没有内容的码。**「空输出 + 124」要读成「我的取证被截早了」，不是「它没话说」。**
2. **同一个 `exit=124` 里混着两件事**：它自己的失败，与我截断它的动作。本库接受的唯一写法是把
   归属逐字写进条目正文（本条目卡片就是），而不是让 `124` 冒充「它的退出码」。
3. **可复核的形态**：同一个 argv 在不同 timeout 下的多次观测要并排放进同一张表，否则后一次会
   静默覆盖前一次——本批的 130 与 140 都在库里，两次读数可以互相校对。
FINDING_EOF

finding 137-doccheck <<'FINDING_EOF'
**缝（同一个坑的两种活法）："在错误的 cwd 里跑"这件事，一个动词只报错，另一个把出路写进拒绝。
两段输出都在本条目里，逐字对照。**

同一次运行、同一个 cwd（私有沙盒 profile，非仓库），只换动词与参数：

| argv | 输出（逐字首行） | exit |
|---|---|---|
| `gopi doccheck` | `gopi doccheck: no go.mod above the working directory — run this from inside the repo, or pass --repo PATH` | 1 |
| `gopi doccheck --repo <仓库>` | `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row` | 0 |
| `gopi code-search zzNoSuchSymbol`（批 1 的 108，同 cwd） | `no symbol found: zzNoSuchSymbol` | 1 |
| `gopi code-search sandboxShow`（本批实测，同 cwd） | `gopi code-search: refusing: no code repository at the search root` + 三行明细 + `this is NOT a "no results" answer: the search did not run` | **4** |

第 1 行与第 3 行是**同一个坑**：都是"我所在的目录不是仓库"，一个把出路（`--repo PATH`）写进拒绝，
另一个（旧版）只说"没找到"。第 4 行是同一个 `code-search` 在 master 上**已经修好**的样子：
**不同的原因落在不同的句子和不同的退出码上**（4 ≠ 1），并且逐字否认"这是零结果"。
把修前（批 1 的 108）与修后（本批）放进同一张表，才看得出这条拒绝被人改过——
**单独一批素材看不出这件事**。
FINDING_EOF

finding 118-ui_state <<'FINDING_EOF'
**缝：「面上真开过一个 ui 会话」与「ui_state 有东西可读」**不是同一件事，而 `--list` 的 roots
把这句话说得很清楚——同一批里用两个状态量了同一个 profile。**

| 我做的事（同一 profile、同一 argv） | `ui_state --list` 的读数 | 会话目录里有什么 |
|---|---|---|
| 只在沙盒里跑过 `gopi chat` / `gopi web`（本条目 118） | `scanned=0 roots=2` | 有 chat/web 的会话 |
| 再让 `gopi ui` 真的开着（另一次运行，同形状的沙盒） | 仍然是 `scanned=0 roots=2` | ui 自己报了 `engine session=<id>`，会话目录里**有 raw 日志与会话记录** |

第二行是判决性的：**ui 真的开过会话、会话目录里真有东西，`ui_state` 一样返回 0**——
因为该动词只认 **armed 的 dump**（它自己那行 help 写着：`a surface writes a dump only when it
is armed: gopi chat --dump-screen … or gopi ui --dump-screen`）。所以这一条不是缺陷报告，
而是一次**读数口径的澄清**：`scanned=0` 的正确读法是「没有 armed 的 dump」，
**不是**「这台机器上没有 gopi 会话」。这两件事在本库的其他条目里恰好都真实存在
（112 读了 raw 日志、110 读了 session.jsonl、111 读了会话统计），把它们混起来会得出
「ui_state 坏了」的错误结论。

**同一次运行里还有一处值得记的形状**：`roots` 里的第二个根是
`/home/user/workspace/gopi-test-scratch/queue/queue-runtime-ui-session-`（共享 scratch 的
队列运行目录），而第一个根是本 profile 自己的 `sessions/`。**它把"我搜了哪些地方"逐字列出来**，
所以上面那条误读不会发生——对比批 1 的 108（`code-search` 在非仓库 cwd 里与"真的没有"
共用一句话、同一个码），这里"没有"与"没扫到"是能分开的。**这也正是 140 号 finding
的正面例子**：凡是"没有找到"的回答，都该带上它搜过的范围。
FINDING_EOF

auto_finding_all
