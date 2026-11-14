<!-- doc-class: normative -->
# gopi 语音输入「延迟大 / 漏字多」根因与替代方案（只调研取证）

- **日期**: 2026-10-08
- **任务**: `01a11829c82f7dc28c16469ea98b4a08`（branch `task/01a11829c82f7dc28c16469ea98b4a08`）
- **主机**: peer1（i7 CPU-only，PulseAudio，无 GPU）
- **性质**: 只调研取证。**没有改一行代码、没有装任何 cron/daemon/服务。** 需要改代码的部分已按 Rule 26 另提任务（见 §6）。
- **证据目录**: `/home/user/.gopi/sessions/01a11829-c82f-7dc2-8c16-469ea98b4a08/artifacts/{q2,q3}`（Rule 30/37：非分支证据不落工作树）
- **凭证**: 全程只从 `~/.doubao/apikey.txt` 由程序自己读取，本报告不出现明文 key。
- **未跑过的一律标注「未验证」**（Rule 31）；本文所有「实测」数字都附真实命令与输出。

---

## 0. 结论（先看这三行）

1. **延迟主要来自架构，不是引擎。** `gopi tool voice_input --seconds 6` 的实测墙钟是 **18.6 s**：
   **10.0 s 是录制前的 `voiceCaptureGraceSec` 空等**、6.0 s 是窗口本身、**2.5–3.3 s 才是引擎**。
   即 **86% 是架构，14% 是引擎**，而其中 **10 s 是纯粹的意外税**（这段等待里没有录音，声音正在被丢掉）。
2. **「漏字」的机制是固定窗口切断了连续语音，不是丢样本。** 同一段 12.455 s 语音：整段送引擎 = 一句话、正确；
   按 6 s 窗口切开 = 第 1 段正确、**第 2 段直接为空（云端与本地都是空）**，即 **50% 内容消失**。
   通道实验（前导 5 s / 12 s 静音两次）显示前导静音都被完整保留（3.94 s / 3.92 s），**采集通道本身不截断**。
3. **火山双向流式（本账号）未授权，买得到但没买**：`volc.seedasr.sauc.duration`、`volc.bigasr.sauc.duration`
   都是 **HTTP 403 `requested resource not granted`**；`*.concurrent` 握手成功但回 **45000292
   `quota exceeded for types: concurrency`**。可用的**录音文件识别 AUC 是整段转写（无流式分字）**，
   12.455 s 音频端到端 **2.5–2.6 s**（RTF 0.21）、且**不受 6 s 窗口限制** —— 这是目前唯一能真正消除「漏字」的通道。

---

## Q1 延迟分解：延迟来自架构还是引擎？

### Q1.1 `/voice` 的一个 round 由什么组成（代码事实）

`cmd/agent/chat_voice.go` 的 `loop()` 每一轮调用 `voiceChatRecorderLocal.Round()`，后者是
`voiceListDevices → voiceResolveSelection → voiceRecord → transcribeFile`（`chat_voice.go:423-441`）。
所以一轮的延迟 = **设备选择 + 录制 + 引擎**，而 `voiceRecord` 的实现在
`cmd/agent/cli_voice_client.go:446/479`：

```go
// cmd/agent/cli_voice_client.go:116-120
// voiceCaptureGraceSec is added to --seconds before the recorder is
// killed: the same host that answers a 2 s request in 1.9 s may take
// several seconds to reach its first sample (Bluetooth headset, suspended
// card), and killing at exactly N would truncate a valid recording.
voiceCaptureGraceSec = 10
...
ctx, cancel := context.WithTimeout(context.Background(),
        time.Duration(seconds+voiceCaptureGraceSec)*time.Second)   // :482-483
```

窗口常量与轮间停顿（`cmd/agent/chat_voice.go`）：

```go
const voiceChatWindowSeconds = 6        // :78  —— 每轮录 6 s
const voiceChatWindowPauseSeconds = 1   // :87  —— 只在「本轮无文本」后等待
```

`loop()`（`chat_voice.go:814-855`）的分支：**成功插入文本后不停顿，立刻开下一轮**；
只有 `text == ""`（静音 / 引擎 `/sil`）才走 `m.pause(...)` 等 1 s。所以传说里的
「1 s 轮间停顿」**只在静音轮出现**，有语音时它不在延迟账里。

### Q1.2 实测：`gopi tool voice_input --seconds 6 --engine local`（与 `/voice` 同一条 recorder + 同一引擎缝）

```
$ cd /home/user/workspace/gopi-01a11829c82f7dc28c16469ea98b4a08
$ ./bin/gopi tool voice_input --seconds 6 --engine local 2>&1 | python3 <ts.py 打时间戳>
T+  0.02s [voice] recording 6s from pulse:@DEFAULT_SOURCE@ (...) [first enumerated]
T+ 16.02s [voice] captured 192000 bytes (6.0s of 16000Hz mono PCM16)
T+ 18.49s [voice] transcribed locally in 2.5s (chunk=15s)
T+ 18.49s /sil
```

**要 6 s 的音频，墙钟 18.5 s。** 三个点分开量，逐个得到同样的答案：

| 分项 | 实测值 | 命令 / 证据 |
|---|---|---|
| 录制窗口本身 | **6.0044 s**（N=3: 3.0038 s；N=6: 6.0039 s） | `parecord --device=@DEFAULT_SOURCE@ --raw --rate=16000 --channels=1 --format=s16le` + `kill -TERM` |
| **录制前空等（grace）** | **+10.000 s** | 上面时间戳：`recording` T+0.02 → `captured` T+16.02，差 **16.00 s** 要的是 6 s 窗口 |
| 本地引擎（Fun-ASR-Nano，`--chunk 15`） | **2.5–3.3 s**（独立进程 6 s 音频 = 2.80 s） | `llama-funasr-cli --enc … -m … -a cap6.wav -n 128` |

窗口长度不同，**grace 恒定 10 s**（这就是它「不管有没有声音都要付」的证据）：

```
### gopi --seconds 3 ###
T+  0.02s [voice] recording 3s ...      T+ 13.03s [voice] captured 96000 bytes   → 13.0 s（3+10）
T+ 15.05s [voice] transcribed locally in 2.0s
### gopi --seconds 10 ###
T+  0.02s [voice] recording 10s ...     T+ 20.03s [voice] captured 320000 bytes  → 20.0 s（10+10）
T+ 23.26s [voice] transcribed locally in 3.2s
```

### Q1.3 `/voice` 连续模式的真跑（3 轮，播放一段 74.7 s 连续语音）

```
( for i in 1 2 3 4 5 6; do paplay --device=q2null a16.wav; done ) &   # 连续说话，无间断
for i in 1 2 3; do ./bin/gopi tool voice_input --seconds 6 --engine local; done
做一个语音输入的延迟实验，测试所山影的方向。      ROUND 1 wall=25.32 s
今天我们用 body 做一个语音输入的延迟实验测试。    ROUND 2 wall=35.90 s
向流逝识别是否已经开通授权？                      ROUND 3 wall=18.72 s
TOTAL_WALL=79.99 s   ROUNDS=3   录到的音频=18 s        # → 4.45× 实时
```

### Q1.4 判定

以 `--seconds 6` 的 **18.6 s** 一轮为账本：

| 阶段 | 秒 | 占比 |
|---|---|---|
| **录制前 grace（空等，声音被丢）** | **10.0** | **54%** |
| 录制窗口（有声音的那 6 s） | 6.0 | 32% |
| 引擎（含每轮重新加载 1.5–1.6 s 模型） | 2.5–3.3 | 14% |

**延迟主要来自架构（86%），引擎只占 14%。** 其中 10 s 那一段不是「等引擎」，是 gopi 在**开麦克风之前**的空等 ——
它带来的第二个后果就是 Q2 的漏字（见下）。引擎侧的效率本身并不差：

| 音频长度 | 引擎墙钟 | RTF |
|---|---|---|
| 2.0 s | 1.818 s | 0.91 |
| 4.0 s | 2.587 s | 0.65 |
| 6.0 s | 2.798 s | 0.47 |
| 12.455 s | 4.207 s | 0.34 |
| 0.2 s（**纯加载+初始化**） | **1.46–1.62 s** | — |

（命令：`ffmpeg -i a16.wav -t N w_N.wav` 后 `llama-funasr-cli … -a w_N.wav -n 128`，`/usr/bin/time` 与 `date +%s.%N` 双记。）
**关键**：短音频的 RTF 更差，因为每轮都要付 1.5 s 的模型加载；`/voice` 每轮都是一个**新进程**，所以这笔钱每轮都付。

> **历史备注（诚实）**：`+10 s grace` 不是隐藏 bug —— 它写在代码注释里（`cli_voice_client.go:116-120`），
> 也在 `devdoc/voice-client-e2e-hp.md` §F5 被记为 "intended (documented in code)"、「`--seconds N` 约等于 N + 13 s」。
> 本次的新事实是：**它从未在「交互式口述」的延迟账本上被定价** —— 交互场景下它占了一轮的 54%，并且是漏字的直接原因。

---

## Q2 漏字机制：实验证据

### Q2.1 假说与排除（先排除「采集通道截断」）

**假说 H1**：「PulseAudio 在没人读的时候会丢数据，所以 `voiceRecord` 的前几秒是空的、语音被截断。」

**实验**：构造 `5 s 数字静音 + 12.455 s 语音` 与 `12 s 数字静音 + 12.455 s 语音` 两个夹具，
播放器先起、0.3 s 后按**生产 argv** 录 6 s，测窗口内的前导静音长度：

```
$ ffmpeg -f lavfi -i anullsrc=r=16000:cl=mono -t 5  -c:a pcm_s16le sil5.wav
$ ffmpeg -i "concat:sil5.wav|a16.wav" -c:a pcm_s16le s5.wav
$ setsid paplay --device=q2b s5.wav & sleep 0.3
$ parecord --device=@DEFAULT_SOURCE@ --raw --rate=16000 --channels=1 --format=s16le > raw6.raw & sleep 6; kill -TERM %1
$ ffmpeg -i raw6.wav -af silencedetect=n=-35dB:d=0.3 -f null -
silence_start: 0
silence_end: 3.93869 | silence_duration: 3.93869      # 夹具前导 5 s
# 换成前导 12 s 的夹具：
silence_end: 3.92456 | silence_duration: 3.92456      # 差异 0.014 s
```

**结论：H1 被证伪。** 前导静音被**完整保留**（两次相差 0.014 s，即夹具首样本到达时刻的抖动）；采集通道不截断。
（顺带得到事实：录制开始后音频确实要到 **≈3.9 s** 才进来 —— 这是 2 个 PulseAudio 节点的管线延迟，
**但它不丢东西**，所以它不是漏字的原因。）

### Q2.2 真机制：固定窗口在句子中间切一刀，切口之后的内容被丢弃

**实验**：把同一段 12.455 s 连续语音按 `/voice` 的窗口边界切开 —— `c1 = 0–6 s`、`c2 = 6–12.455 s`，
两段都用**云端**转写（避开本地引擎质量因素）：

```
$ ffmpeg -i a16.wav -t 6 -acodec pcm_s16le c1.wav
$ ffmpeg -ss 6 -i a16.wav -acodec pcm_s16le c2.wav
$ ./aucfile -file c1.wav -tag c1 -resource_id volc.seedasr.auc     # [c1] total=2488ms rtf=0.415 chars=22
$ ./aucfile -file c2.wav -tag c2 -resource_id volc.seedasr.auc     # ERROR: status=20000003 ... "text":""
c1 text = 今天我们用手机做一个语音输入的延迟实验。测试        ← 语义不完整（"测试"后断气）
c2 text = （空）
真值    = 今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。
```

**同一段语音整段送 = 一句话 42 字全对（见 Q3.3）；按 6 s 窗口切开 = 后半段直接为空。** 这就是「漏字多」。
不是识别错了字，是**根本没有送去识别**。

**gopi 自己的运行也复现了这个形状**（Q1.3 那 3 轮）：连续播放 74.7 s 无间断语音，3 轮拿回来的是 3 个**互不相接**的句子
（`做一个语音输入的延迟实验…` / `今天我们用 body 做一个…` / `向流逝识别是否已经开通授权？`）——
它们是把原文倒着读出来的片段，两轮之间被吃掉的部分**没有任何地方记录**，操作者只看到「怎么少了半句」。

### Q2.3 量化影响

| 切分方式 | 结果 | 内容保留 |
|---|---|---|
| 整段 12.455 s（`--chunk 15` 批式） | 1 句，与真值逐字一致 | **100%** |
| 按 6 s 窗口切 | 第 1 段正确+第 2 段为空 | **≈50%** |
| 按 15 s 切（引擎自己的 chunk） | 与整段基本一致（15 s 窗口对连续语流几乎无损） | **≈100%** |

**结论**：漏字的成本由「窗口大小」而非「引擎能力」决定。窗口 6 s 的损失最大，
窗口 ≥15 s 基本无损 —— 这与「引擎批式转写在 15 s 内没有边界问题」一致。

---

## Q3 火山双向流式 ASR：本账号授权状态 + 首字延迟

### Q3.1 原始 probe 输出（授权与否）

复用的现成零依赖客户端：`devdoc/doubao-asr-eval/minclient`（stdlib-only，`go build` 无第三方依赖）。

```
$ cd devdoc/doubao-asr-eval/minclient && CGO_ENABLED=0 go build -o $EV/minclient .
$ ./minclient -file a16.wav -tag sauc20 -resource_id volc.seedasr.sauc.duration -outdir $EV/probe20
ERROR: connect: upgrade refused: HTTP 403 logid=20261008052841014C3BF1854398CFBC2A \
  body={"error":"[resource_id=volc.seedasr.sauc.duration] requested resource not granted"}
exit=1
$ ./minclient -file a16.wav -tag sauc10 -resource_id volc.bigasr.sauc.duration -outdir $EV/probe10
ERROR: connect: upgrade refused: HTTP 403 logid=202610080528411361C3C6108CA5B5DB0A \
  body={"error":"[resource_id=volc.bigasr.sauc.duration] requested resource not granted"}
exit=1
$ ./minclient -file a16.wav -resource_id volc.megatts.default -outdir $EV/conc
ERROR: connect: upgrade refused: HTTP 403 logid=20261008053828A5C8566CEBDBA1A9EB37 \
  body={"error":"[resource_id=volc.megatts.default] requested resource not granted"}
exit=1
$ ./minclient -file a16.wav -resource_id volc.bigasr.sauc.concurrent -outdir $EV/conc   # 12 s 超时截断
[server error] quota exceeded for types: concurrency
exit=124
$ ./minclient -file a16.wav -resource_id volc.seedasr.sauc.concurrent -outdir $EV/conc  # 同上
[server error] quota exceeded for types: concurrency
exit=124
```

| resource_id | 结果 | 含义 |
|---|---|---|
| `volc.seedasr.sauc.duration`（流式 2.0） | **HTTP 403** `requested resource not granted` | **未开通** |
| `volc.bigasr.sauc.duration`（流式 1.0） | **HTTP 403** 同上 | **未开通** |
| `volc.bigasr.sauc.concurrent`（并发版 1.0） | 握手 101 成功，逐包回 `code=45000292 quota exceeded for types: concurrency` | 资源在，**没买并发** |
| `volc.seedasr.sauc.concurrent`（并发版 2.0） | 同上 | 同上 |
| `volc.megatts.default` | **HTTP 403** | 未开通 |
| `volc.seedasr.auc` / `volc.bigasr.auc`（录音文件） | **可用**（`X-Api-Status-Code: 20000000`） | 当前唯一可用的云端通道 |

> 结论：**买不买得到另说，本账号现在没有流式额度。** 409/403 的 body 才知道原因（裸 403 不给），
> 两次探针的 `logid` 已在上面原文给出，可直接找火山客服核账。

### Q3.2 「首字延迟」怎么量（诚实的分层）

- **HTTP 录音文件（AUC）**：**没有流式分字机制** —— 提交 → 轮询 → 取回整段文本。
  所以「首字延迟」必须由 `submit + 轮询间隔 + 服务端处理` 派生，**不是直接观测到的序列**。
- **WebSocket 双向流式**：客户端本来会打 `first_char_ms`（`minclient` 里就有这一项），
  **但本账号 403，实测不到** —— 这是本报告最大的空缺（见 §7 Not Done）。
- **可比派生量**：本地批式我也会给出「文本落盘时刻」，两者同属「整段转写完成时刻」，
  这样至少能比较**端到端可用文本的延迟**，而不是拿苹果比橘子。

### Q3.3 实测：可用通道（AUC 录音文件）端到端

```
$ ./aucfile -file a16.wav -tag auc20 -resource_id volc.seedasr.auc -outdir $EV/probeauc
[auc20] resource=volc.seedasr.auc mode=file/base64 submit=415ms query=2582ms total=2583ms \
        rtf=0.207 polls=1 chars=42 submit_logid=2026100805275575587FA48ABFDBE721F2 \
        query_logid=202610080527587A4B3305B154B4D20EB6
# metrics.json 里 status_codes = ["20000000","20000000"]（提交与查询各一次 X-Api-Status-Code）
text = 今天我们用朵狸做一个语音输入的延迟实验，测试火山引擎的双向流式识别是否已经开通授权。
（真值 = 今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。）
```

| 分项 | 实测 |
|---|---|
| 12.455 s 音频上传+提交 | 325–415 ms |
| 首次查询（客户端固定 `pollIntervalS` 后） | 2.487–2.582 s |
| **端到端可用文本** | **2.5–2.6 s**（RTF **0.21**） |
| 识别质量 | 同一句 42 字，仅 1 处同音（gopi→朵狸）；**无截断、无漏句** |
| 长度限制 | **不受 6 s 窗口约束**（12.455 s 一次成句） |

**与本地引擎对照（同一段音频）**：本地 12.455 s → 4.21 s（RTF 0.34）；本地 6 s 窗口 → 2.80 s。
即**云端 ≈ 本地的 1.1–1.7 倍速**，而且**没有窗口上限** —— 云端真正解决的是「漏字」，不只是快一点。

**计费**（本仓已有证据 `devdoc/doubao-asr-eval/evidence/pricing-1359370-billing.md`，
**未在本机重新核对官方页面**）：
`豆包流式语音识别模型2.0` 30 小时 **28 元 ≈ 0.93 元/小时**；`豆包录音文件识别模型2.0` 30 小时 **23 元 ≈ 0.77 元/小时**。
（即：按 6 s 窗口这种切法，云端是**按上传的音频小时**计费，**切的窗口越多越贵**。）

---

## Q4 讯飞「离线语音芯片」是否适用于自由文本口述 —— 型号级判断

**判断：不适用。** 理由不是「讯飞不行」，而是**这类芯片的产品定位不是自由听写**，而是唤醒词 + 命令词。

### Q4.1 型号级证据（厂商规格页原文）

讯飞自身不产通用 MCU 形态的语音芯片；其离线语音能力主要跑在**启英泰伦（Chipintelli）CI 系列**
方案上（讯飞提供算法/方案授权，渠道里常被叫作「讯飞离线语音方案」）。厂商页面
`http://www.chipintelli.com/zh-cn/product/index.html?category_id=60` 的原话：

| 型号 | 厂商规格页原文 | 自由听写？ |
|---|---|---|
| **CI1302** | 「离线远场语音识别，**支持 300 条命令词**」；内置 640KB SRAM + 2MB FLASH | ❌ |
| **CI1303 / CI1306** | 「离线远场语音识别，**支持 500 条命令词**」「支持本地声纹识别」「支持离线 NLP」；4MB FLASH | ❌（500 条仍是闭集命令） |
| **CI1312** | 「离线远场语音识别，支持 300 条命令词」；SOP16 | ❌ |
| 系列共性 | 「**命令词数量最高支持 500 条**，部分支持端侧 NLP」「85dB 噪声下 85% 识别率」「主频 240MHz / 640KB SRAM」 | 闭集语法 |
| **CI2305 / CI2306** | 「语音、**WiFi、蓝牙**三合一/单麦、双麦」 | ❌（同上，定位是家电控制） |

**规格层面无法自洽地放进自由文本口述**：口述没有词表，无法把任意语句编译成 300–500 条命令；
而该系列的**整机算力/内存是 240MHz + 640KB SRAM + 2–4MB FLASH**，与它自己页面里明确对齐的
「智慧开关、灯具、风扇」这类场景匹配。

### Q4.2 与「离线语音听写 SDK/引擎」的区别（这是最容易混淆的一点）

| 维度 | 离线语音芯片（CI13XX 等） | 离线语音听写引擎 |
|---|---|---|
| 形态 | 硬件芯片/模组 | 软件（.so/可执行 + 模型文件） |
| 识别内容 | **唤醒词 + 固定命令词（≤500 条）** | 开放词表，任意语句 |
| 模型规模 | 几百 KB ~ 几 MB | 几十 MB ~ 数百 MB |
| 宿主要求 | 芯片自带 BNPU | 宿主 CPU/NPU（本项目的机器即可） |
| 典型场景 | 智能开关/灯/风扇 | 会议记录、离线笔记、车载听写 |

**结论（型号级）**：CI1302 / CI1303 / CI1306 / CI1312 / CI2305 / CI2306 一类离线语音芯片
**不适用**于 gopi 的自由文本口述 —— 缺的不是「识别率」，而是**开放词表**这条能力本身。
若将来真要「离线 + 自由口述」，走的必须是**离线听写引擎**路线（本机已实测的那条：Fun-ASR-Nano
每 6 s 窗口 2.8 s，或 sherpa-onnx 这类流式引擎），而**不是**换一颗语音芯片。

> 证据等级声明：以上是**厂商公开规格页与资料的引证**，本机**没有**相应的开发板/模组，
> **没有做任何硬件实测**（Rule 31：未跑过的一律标注「未验证」）。见 §7。

---

## Q5 各类方案判断表

评分口径：**首字延迟**对 HTTP 批式/本地批式是「整段文本可用时刻」（Q3.2 已说明为何不能直取真首字）；
**持久出字** = 说话过程中是否持续吐字（真正的流式 partial）；**CER** 只引已实测/已引证值，未测的写未验证。

| 方案 | 首字/可用文本延迟（实测） | 持续出字 | CER / 中文质量 | 依赖（Rule 35） | 隐私 | 成本 | 落地工作量 | 判断 |
|---|---|---|---|---|---|---|---|---|
| **现状态：local + 6 s 窗口（`/voice`）** | **18.6 s/轮**（10 s grace + 6 s 窗 + 2.6 s 引擎） | ❌ 整段才出 | 窗口内可用；**跨窗口丢 ≈50%** | 0 新增（已有 910MB+6MB 运行时） | 全程不出机 | 0 | 已是现状 | **当前缺陷源** |
| **本地 + 去掉 10 s grace** | ≈8.6 s/轮 | ❌ | 同上 | 0 | 全程不出机 | 0 | 极小（1 个常量语义） | ✅ **最该先做** |
| **本地 + `--chunk 15`（引擎流式，实测 1.72 s 出第一句）** | **1.72 s 首句**（`--chunk 2`）/ 2.17 s（`--chunk 4`） | ⚠️ 只有 chunk 落的「句」，不是逐字 | 流式较批式略差（`火山影的` 等） | 0 | 全程不出机 | 0 | 中（要按块喂 PCM，不是 `-a 文件`） | ✅ 推荐 |
| **云端 AUC 录音文件（现在可用）** | **2.5–2.6 s**（12.455 s 音频，RTF 0.21） | ❌ 整段一次性 | 42 字 1 处同音；**无窗口上限** | 1 个 key（已有） | 音频出机 | 0.77 元/小时 | 小（引擎已有） | ✅ **立刻可消除漏字** |
| **云端 SAUC 双向流式 2.0** | **未验证**（403 未开通；`minclient` 已含 `first_char_ms` 采集） | ✅ 真 partial | 未验证 | key + 购买 | 音频出机 | 0.93 元/小时 | 小（客户端已写好并跑通协议） | ⏳ 买并发/额度后 1 小时内可实测 |
| **云端 SAUC 并发版** | 未验证（回 45000292「没买并发」） | ✅ | 未验证 | 购买并发 | 音频出机 | 同上 | 小 | ⏳ 同上 |
| **本地流式引擎（sherpa-onnx streaming zipformer）** | **未验证**（本机未装） | ✅（真流式） | 未验证 | 新增 1 个二进制 + 模型（≈百 MB 级） | 全程不出机 | 0 | 中（新引擎接入） | ⏳ 最值得评估的开放项 |
| **whisper.cpp** | 已评估，不重跑 | ❌ 无真流式 | small CER 5.14%/技术词 69% | 488MB 模型 | 不出机 | 0 | — | 只引用（`devdoc/voice-input-whispercpp-evaluation.md`） |
| **讯飞离线语音芯片（CI13XX 等）** | 不适用 | — | **开放词表不支持** | 硬件 | 不出机 | 硬件 | — | ❌ **不适用**（Q4） |
| **讯飞在线听写（走公网）** | 未验证（无凭证） | ⏳ | 未验证 | key + 供应商 | 音频出机 | 未验证 | 中 | ⏳ 与火山同型，未见优势 |

**明确推荐（按顺序）**：

1. **先消掉 10 s grace 与 6 s 窗口这两个 gopi 侧的东西** —— 真正的瓶颈在架构（86%），
   不在选谁当引擎；换引擎最多省 2.6 s，改窗口+grace 省 10 s 以上。**零成本、零新依赖。**
2. **云端大模型批式转写已经可用且更长**：`--engine doubao` 现在就能在 **2.5–2.6 s** 内把一整段
   12.455 s 转成正确句子（RTF 0.21），**不受 6 s 窗口约束** —— 想今天就不漏字，用这条路最快。
3. **要真流式（边说边出字）**：先买火山的流式额度（客户端已就绪，买完 `minclient` 一次跑就能出
   `first_char_ms`）；如不愿上云，则评估 **sherpa-onnx** 这类本地流式引擎（本报告未实测）。

---

## Q6 可执行下一步清单（按性价比排序，每条带验收标准）

> 排序口径 = 「单位工作量换来的延迟/漏字收益」。前三条**不需要任何新依赖**（Rule 35 四问逐条回答见每条末尾）。

| # | 动作 | 预期收益 | 验收标准（可执行、可复现） | Rule 35 四问 |
|---|---|---|---|---|
| **1** | **修掉录制前的 10 s 空等**：让 grace 只在「首样本还没到」时才花，而不是每个窗口无条件付 10 s（交互路径更该如此） | 18.6 s → ≈8.6 s（**-54%**） | 真跑 `gopi tool voice_input --seconds 6` 从 `recording` 到 `captured` 的间隔 ≤ 6.5 s；`/voice` 三轮墙钟 `TOTAL_WALL` 从 79.99 s 降到 ≤30 s；且「设备确实慢到 10 s 才出首样本」的场景仍有保护（回归测试） | ① 不修则每秒白等 10 s 且丢音频（本报告 Q1.2 实测）；② 能，同一函数里改；③ 无新增持久物；④ 不适用（无新产物） |
| **2** | **窗口改为「能量/VAD 驱动的提前收口」**：说完了就立刻送引擎，不再等满 6 s | 6 s 窗口 → 说话结束即转（典型句 3 s 时省 3 s） | 连续语音夹具下，一轮从说话结束到出文本 ≤ 4 s；静音夹具仍不误触发（`/sil` 不再进编辑行，沿用既有测试） | ① 不修则每句都白等剩余窗口；② 能，`voiceChatMode.loop` 已有一轮一判的边界；③ 无；④ 无 |
| **3** | **交互路径改用「尾部窗口」而不是硬切**：保留最近 N 秒（含跨轮重叠），避免 Q2 的切口丢字 | 跨窗口丢字从 ≈50% → ≈0% | 用本报告夹具复现：12.455 s 语音在窗口模式下必须产出**完整一句**（与整段转写逐字一致）；`c2` 那种「窗口后半段为空」不再出现 | ① 不修则连续口述每 6 s 丢一次（Q2.2 实测）；② 能；③ 无；④ 无 |
| **4** | **把云端批式（AUC）作为长句的默认兜底**：短窗口不适合长句时自动整段送云端 | 长句不漏字，延迟 2.5–2.6 s | 12.455 s 与 30 s 夹具均整段正确；云端失败时**必须回落到本地并打印原因**，不得静默 | ① 不修则长句必丢；② 不能（公网调用只能进程外/远端）；③ 已有 key 与代码，无新持久物；④ 失败要显式打印（Rule 15） |
| **5** | **购买/申请火山流式额度并实测真首字延迟** | 拿到真 partial（边说边出字） | `minclient` 跑通 101 且 `codes` 无 45000292；`first_char_ms` 落盘并写进 `metrics.json`；至少 3 次不同句长的实测 | ① 不买就只能批式；② 不能（服务端额度）；③ 无新持久物；④ 无 |
| **6** | **评估本地流式引擎（sherpa-onnx）**：不问「准不准」，先量「首字 + 持续出字 + 本机 RTF」 | 决定是否可能「零云依赖的真流式」 | 同一段 12.455 s：首字时刻、每 200 ms 出字次数、CER；与火山/本地批式同表对照；**未跑通不得上报** | ① 不评估则永远是未知项；② 必要时可进程内/本地进程，但要先证收益；③ 新二进制要写清归属与清理（本次只评估不安装）；④ 报告须写明 |

**明确不做（本次范围外）**：改代码、装服务、买额度、换硬件 —— 全部按 Rule 26 另提任务（见 §7 与提交记录）。

---

## Not Done（诚实）

（编号对应正文的 §7；本节是唯一声明「什么没做」的地方。）


1. **没有跑真正的交互式 `/voice` 会话（真 pty 里打字进模式）**。Q1 的 `/voice` 数字是
   **同一条生产代码路径**（`voiceChatRecorderLocal.Round` = `voiceListDevices + voiceResolveSelection +
   voiceRecord + transcribeFile`，`chat_voice.go:423-441`）加**同一台机器上的真跑**得到的，
   **不是**从 `gopi chat` 里按 Esc/`/voice` 实测出来的。1 s 轮间停顿的「只在静音轮」结论来自读 `loop()` 的分支。
2. **真「首字延迟」（第一个字出现的时刻）没有测到**。火山流式 403 → 拿不到流式序列；
   本地 `--chunk 2` 拿到的是「第一句（1.72 s）」，不是逐字 first-token；AUC/批式根本没有 partial。
   表中凡标「首字/可用文本延迟」的都是**整段文本可用时刻**。
3. **讯飞系芯片没有任何硬件实测**（本机无开发板/模组）。Q4 是**厂商规格页引证**（页面原话已附），
   不是实测；「不适用」的判据是**开放词表**这条能力边界，不是识别率。
4. **未核对火山官方计费页**，价格引用的是本仓既有证据文件
   `devdoc/doubao-asr-eval/evidence/pricing-1359370-billing.md`（未在本次重新核对）。
5. **sherpa-onnx / 其他本地流式引擎未安装、未跑**，「首字延迟/CER」全部未验证。
6. **夹具是 `espeak` 合成的**（`espeak -v zh -s 150`），不是真人麦克风；本报告所有「窗口/通道」结论
   与语音内容无关（用的是时间戳与长度），但**合成语音的 CER 与真人不可直接等同**。
7. **没有验证 3.9 s 的音频管道延迟在别的机器/别的声卡上是否同样**（本机 2 个 PulseAudio 节点的观测值）。
8. **whisper.cpp 结论未重跑**（按 epic 约束只引用 `devdoc/voice-input-whispercpp-evaluation.md`）。
9. 本地引擎 RTF 本次实测为 **0.34–0.91（取决于音频长度）**；CLI 自报的 `[done] … 0.32x realtime` 与本报告
   口径不同（其分母/起点未复现），故**不引用该自报值**。

## Q8 关键证据索引（命令都可在本机复跑）

| 产物 | 路径（session 目录） | 内容 |
|---|---|---|
| 夹具 | `artifacts/q3/a16.wav` | 12.455 s / 16 kHz mono / 由 `espeak -v zh` + `ffmpeg -ar 16000 -ac 1` 生成 |
| Q3 流式 probe | `artifacts/q3/probe20/run.txt`, `probe10/run.txt`, `conc/*.log` | 403 原文 + logid + 45000292 原文 |
| Q3 可用通道 | `artifacts/q3/probeauc/auc20.{txt,metrics.json}` | 文本 + submit/query ms + `status_codes` + logid |
| Q3 切分实验 | `artifacts/q3/probechunk/c1.txt`（有字）/ `c2.txt`（空）+ `c2` 的 query 原始 body | 漏字证据 |
| Q1 时间线 | `artifacts/q2/ts.py` + 各次 stdout 时间戳 | grace 10.00 s 的直接观测 |
| Q1 引擎曲线 | `artifacts/q3/w_{2,4,6,12.455}.wav` 与 `artifacts/q2/cap6.wav` | RTF 与加载成本 |
| Q2 通道实验 | `artifacts/q2/raw6.wav`（前导 5 s）、`raw6b.wav`（前导 12 s） | silencedetect 3.94 / 3.92 s |
| Q2 t=0 对齐 | 同上 + Q1.3 的三轮实跑输出 | 窗口从录制时刻算起 |

---

## Completed（Rule 5 交付说明）

- `devdoc/voice-input-latency-and-alternatives.md`（本文件）：Q1 延迟分解（真实时间戳 + 各分项独立实测）、
  Q2 漏字机制（通道假说被证伪 + 切分实验 + 量化）、Q3 原始 probe 输出（403/45000292 原文与 logid）
  与可用通道端到端实测、Q4 讯飞型号级判断（含厂商规格页原文）、Q5 方案判断表 + 明确推荐、
  Q6 六条可执行下一步（各带验收标准与 Rule 35 四问）、Q7 诚实 Not Done、Q8 证据索引。
- 全程零代码改动、零新增持久依赖：`git status --porcelain` 为空，改动仅本文件（见提交）。

## Verification

- **Build**: `CGO_ENABLED=0 go build ./cmd/agent/` → `-rwxrwxr-x … bin/gopi`（exit 0）—— 用于真跑，
  工作树内 `bin/` 已被 `.gitignore:2` 忽略。
- **Tests**: 未跑（本任务未改任何代码，无需回归；跑全量套件属 Rule 27 禁止的重路径）。
- **Ran**:（逐条即上文引用的命令，全部真跑过）
  - `./bin/gopi tool voice_input --seconds {3,6,10} --engine local`（时间戳流水）
  - `for i in 1..3: ./bin/gopi tool voice_input --seconds 6 --engine local` → `TOTAL_WALL=79.99 s`
  - `./minclient -file a16.wav -resource_id {volc.seedasr.sauc.duration, volc.bigasr.sauc.duration, …}`
  - `./aucfile -file {a16,c1,c2}.wav -resource_id volc.seedasr.auc`
  - `llama-funasr-cli --enc … -m … -a w_N.wav [-n 128] [--chunk N]`
  - `parecord --device=@DEFAULT_SOURCE@ --raw --rate=16000 --channels=1 --format=s16le` (+`kill -TERM`)
  - `ffmpeg … -af silencedetect=n=-35dB:d=0.3`
- **Result**: 见 Q1–Q5 各表；核心三个数字：**要 6 s 音频花 18.5 s（其中 10.0 s 空等）**、
  **6 s 切分使 12.455 s 语音丢 ≈50%**、**可用云端通道 2.5–2.6 s 无窗口上限**。
- **Commit**: 见 §6 提交（branch `task/01a11829c82f7dc28c16469ea98b4a08`）。

## Evidence (self-filed)

- grace 10.00 s：`--seconds 6` 的 `recording` T+0.02 → `captured` T+16.02（同一 pty，`ts.py` 打戳）——观测值。
- grace 不随窗口变：`--seconds 3` → 13.03 s，`--seconds 10` → 20.03 s ——观测值。
- 录制窗口成本：`parecord` N=3 → 3.0038 s（58252 B）、N=6 → 6.0039 s（174812 B）——观测值。
- 引擎：2 s/4 s/6 s/12.455 s = 1.818/2.587/2.798/4.207 s；0.2 s 纯加载 = 1.46–1.62 s ——观测值。
- 本地流式出字：`--chunk 2` 首句 T+1.72 s、`--chunk 4` T+2.17 s、批式三句全在 T+4.14 s ——观测值。
- 云端可用通道：12.455 s → total 2583 ms、RTF 0.207、`status_codes=["20000000","20000000"]` ——观测值。
- 流式未授权：两次 403 + logid `…414C3BF1854398CFBC2A` / `…1361C3C6108CA5B5DB0A`；
  并发版 `quota exceeded for types: concurrency`；`megatts.default` 403 ——观测值。
- 漏字：`c1` 22 字 / `c2` 0 字（后者 query body 原文 `"text":""`）——观测值。
- 通道不截断：前导静音 3.93869 s（夹具 5 s） vs 3.92456 s（夹具 12 s）——观测值。
- 未跑项的标注：§7 全部列为 Not Done，无一处对外声明为实测。
