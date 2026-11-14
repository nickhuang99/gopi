<!-- doc-class: historical -->
# 本地「真流式」ASR 实测评估：sherpa-onnx streaming zipformer（peerbox，2026-10-08）

Task `01a11854-06b2-7bb1-95f0-ed8f7b618a6d` · branch `task/01a1185406b27bb195f0ed8f7b618a6d` · 主机 peerbox
（Intel i7-5500U 2c/4t，无 GPU）· 全程 **零云依赖**、**零 gopi 代码改动**、**零新增持久依赖**。

结论先行：**真流式在本机跑通了，三个数字都拿到了**（首字 0.44–1.37 s、持续出字
每 0.31–0.44 s 一次、CER 与批式同量级）；但 **不建议现在接入 gopi**——理由在
§6，核心是「准确率的最后一公里是引擎选型问题，不是架构问题」。

---

## 0. 交付物与证据索引

| 物 | 路径（本任务 session 目录，Rule 22 off-`/`） | sha256 / 说明 |
|---|---|---|
| 夹具音频（12.455 s，16 kHz mono PCM16） | `artifacts/asr-stream-eval/fixture.wav` | md5 `2268665191ea477ced51ebab7f01b489` |
| 人工核准真值 | `artifacts/asr-stream-eval/ref.txt` | sha256 `e87a4700…f7b25` |
| 测量装置（逐帧时间戳） | `artifacts/asr-stream-eval/measure_stream.py` | sha256 `3b32da61…f84f75` |
| 打分器（单一口径对齐） | `artifacts/asr-stream-eval/score_window.py` | sha256 `8dc23e32…bc033d` |
| 三数字原始日志 | `partial-*.jsonl` + `summary.txt` | 每条 partial 一行 JSON |
| 引擎/模型 | `sherpa-onnx-v1.13.8-linux-x64-static/`、`*-zipformer*/` | 下载包 sha256 见 `sha256-downloads.txt` |

**夹具是现场生成的，且可复现**（实测重生成字节相同）：

```bash
espeak -v zh -s 150 -w esp.wav \
  "今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。"
ffmpeg -v error -y -i esp.wav -ar 16000 -ac 1 -acodec pcm_s16le fixture.wav
# 重跑 md5 与上面一致：2268665191ea477ced51ebab7f01b489
```

同一段音频与前序任务 `01a11829` 的 `a16.wav` **逐字节相同**（md5 相同），
所以本报告的数字可直接与那份报告的参照同表。

**真值 `ref.txt`（41 字，人工核准，与音频逐字一致）**：

```
今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。
```

---

## 1. 三个数字（同一段 12.455 s 夹具，逐帧时间戳）

装置：`sherpa-onnx-online-websocket-server`（WebSocket 是 sherpa 三条流式入口之一）
+ `measure_stream.py` 客户端。**逐帧读 socket、整条 UTF-8 文本帧到达即打
`time.monotonic()`**，避开行缓冲坑（前序任务已实测：未 `stdbuf` 的管道会把
所有出字挤到进程退出时刻）。音频按 **100 ms 一块、每 100 ms 送一块** 的实时节奏送，
时间轴就是麦克风的真实时间轴。

```bash
# 服务端（zh-14M / 端口 6007；small-bilingual / 端口 6008）
M=$PWD/sherpa-onnx-streaming-zipformer-zh-14M-2023-02-23
./sherpa-onnx-v1.13.8-linux-x64-static/bin/sherpa-onnx-online-websocket-server \
  --port=6007 --num-work-threads=2 --tokens=$M/tokens.txt \
  --encoder=$M/encoder-epoch-99-avg-1.onnx --decoder=$M/decoder-epoch-99-avg-1.onnx \
  --joiner=$M/joiner-epoch-99-avg-1.onnx --log-file=ws-zh14m.log
# 客户端
python3 measure_stream.py --wav fixture.wav --url ws://127.0.0.1:6007 | tee partial-zh14m.jsonl
```

### 数字 ①　首字延迟

| 模型（fp32） | 音频 | **首字 T+** | 首字内容 | 一次 Partial 的字 |
|---|---|---|---|---|
| zipformer-zh-14M | 夹具 12.455 s | **1.321 s** | 我 | 1 |
| zipformer-small-bilingual-zh-en | 夹具 12.455 s | **1.365 s** | 我 | 1 |
| zipformer-zh-14M | 自然语音 20 s | **0.752 s** | 固化 | 2 |
| zipformer-small-bilingual-zh-en | 自然语音 20 s | **0.438 s** | 我 | 1 |

原始行（`partial-zh14m.jsonl` / `partial-smallbil.jsonl`）：

```json
{"t_ms": 1320.8, "audio_ms": 1320.8, "kind": "partial", "seg": 0, "text": "我", "delta": "我"}
{"t_ms": 1365.1, "audio_ms": 1365.1, "kind": "partial", "seg": 0, "text": "我", "delta": "我"}
```

**纯计算下限（不实时供料）**：`--pace 0` 把整段音频尽快灌进去，首字落在
**T+0.091 s**——即 12.455 s 音频被吃进 100 ms 时才吐第一个字，说明 1.32 s
**不是模型算不动，而是「要等约 1.3 s 音频才攒够一个可判定的 token」**。
这条对架构很重要：首字延迟由**喂料节奏 + 模型的前置上下文需求**决定，
不是由 CPU 算力决定。

```json
{"t_ms": 90.6, "audio_ms": 90.6, "kind": "partial", "seg": 0, "text": "我", "delta": "我"}
```

### 数字 ②　持续出字（次数 / 间隔）

| 模型 | 音频 | Partial 次数 | 覆盖时长 | **间隔中位数** | 最短 / 最长 | 末次出字 |
|---|---|---|---|---|---|---|
| zh-14M | 夹具 12.455 s | 2 | 2.31 s | 2308.3 ms | 2308.3 / 2308.3 | T+3.629 s |
| small-bilingual | 夹具 12.455 s | **14** | 10.57 s | **441.8 ms** | 264.0 / 2597.2 | T+11.932 s |
| zh-14M | 自然语音 20 s | **41** | 19.21 s | **324.8 ms** | 272.7 / 1304.8 | T+19.958 s |
| small-bilingual | 自然语音 20 s | **45** | 19.50 s | **312.5 ms** | 236.4 / 1341.6 | T+19.941 s |

`seg` 字段在新增时说明 **endpoint（端点检测）触发、开新句**：夹具上 zh-14M 切成
4 段（0/1/2/3），small-bilingual 也切成 4 段；自然语音 20 s 全程 1 段
（没有句末停顿）。**这是「持续出字」与「逐句成文」的分界**：一次 partial 只加
1–3 个字，不是整句重发。

原始行样张（自然语音 20 s，zh-14M 前 6 条 + 末 2 条）：

```json
{"t_ms": 751.7, "audio_ms": 751.7, "kind": "partial", "seg": 0, "text": "固化", "delta": "固化"}
{"t_ms": 1024.4, "audio_ms": 1024.4, "kind": "partial", "seg": 0, "text": "固化为模", "delta": "为模"}
{"t_ms": 1356.7, "audio_ms": 1356.7, "kind": "partial", "seg": 0, "text": "固化为模型底", "delta": "型底"}
{"t_ms": 1642.8, "audio_ms": 1642.8, "kind": "partial", "seg": 0, "text": "固化为模型底层逻辑", "delta": "层逻辑"}
{"t_ms": 2645.2, "audio_ms": 2645.2, "kind": "partial", "seg": 0, "text": "固化为模型底层逻辑成为", "delta": "成为"}
{"t_ms": 2932.8, "audio_ms": 2932.8, "kind": "partial", "seg": 0, "text": "固化为模型底层逻辑成为思想", "delta": "思想"}
…
{"t_ms": 19633.3, "audio_ms": 19633.3, "kind": "partial", "seg": 0, "text": "…训练出的A型质量通讯头", "delta": "通讯头"}
{"t_ms": 19958.4, "audio_ms": 19958.4, "kind": "partial", "seg": 0, "text": "…训练出的A型质量通讯头盔", "delta": "盔"}
```

**口径诚实说明**：这些是**末端延迟（尾部延迟）**——普通行情是 0.27–0.44 s，
最坏的几次拉到 1.30–1.34 s（间隔表里的最长值），逐条可见于 `summary.txt`。
本报告**不引用引擎自报的 xN realtime**。

### 数字 ③　CER（对人工真值逐字比对）

用**仓库自带打分器**（前序任务钉住的同一把尺子）：

```bash
python3 scripts/asr_score.py <hyp.txt> <ref.txt>
```

夹具 12.455 s（ref 41 字，归一化后 41 字符）：

| 引擎 | 形态 | 转出文本 | CER |
|---|---|---|---|
| zipformer-zh-14M | 流式（拼接各段末次 partial） | `我语音` | **92.68%** |
| zipformer-zh-14M | 批式（整段一次） | `语音` | **95.12%** |
| small-bilingual | 流式 | `我们用保因输入延迟实验测试刘开松手` | **70.73%** |
| small-bilingual | 批式 | `今天我们六个批语音输入延迟实验测试刘子石边已经开封守权` | **56.10%** |

自然语音 20 s（ref 取参考文本中锚定短语命中的 83 字窗口，锚仅出现 1 次，已核）：

| 引擎 | 形态 | CER |
|---|---|---|
| zipformer-zh-14M | 流式 | **16.87%** |
| small-bilingual | 流式 | **16.87%** |
| zipformer-zh-14M | 批式（同一段音频） | **24.10%** |
| small-bilingual | 批式（同一段音频） | **18.07%** |

锚定与打分命令：

```bash
# 锚短语（在 1011 字参考里只出现 1 次，故窗口是音频位置决定的，不是假设决定的）
ANCHOR="不可分割的延伸人们称之为"
python3 score_window.py stream-zh14m-nat20.txt ref-nat20.txt "$ANCHOR"
# -> ref=83 hyp_window=83 edit=14 CER=16.87%
```

参考文本 = 仓库既有的测试素材 `~/work/whisper.cpp/2084无颜见人0.txt`（1011 字，
有声书旁白，与 `2084无颜见人0.mp3` 同一份材料）。

**夹具 CER 为什么这么高（必须说清，否则数字会被误读）**：
`espeak -v zh` 是**共振峰合成器**，本机 espeak 是老的 `espeak`（1.48 系）而非
`espeak-ng`，合成音**可懂度本来就有限**——实测简单句
`今天天气很好，我们一起去公园散步。` 只被听出 `我们一起去公园`。所以：

- 夹具上的 92%/70%/56% 是 **「合成音可懂度 × 引擎能力」的合成值**，
  **不是引擎在人类语音上的准确率**；
- 它**跨引擎可比**（同一段音频、同一把尺子），这一点有效；
- 引擎的**真实人类语音准确率**看自然语音那一行：**流式 16.87% / 批式 18–24%**，
  以及整文件批式（220 s，同一份参考，**不同口径**）：zh-14M 12.07%、small-bilingual
  10.98%（`scripts/asr_score.py batch-*.txt ~/work/whisper.cpp/2084无颜见人0.txt`）。
  这一行是**整文件口径**，与 83 字窗口口径不可直接比，故单列。

---

## 2. 与已测三条参照的同表对照

| 方案 | 首字延迟 | 持续出字 | 本段 CER 口径 | 依赖 / 隐私 | 出处 |
|---|---|---|---|---|---|
| **本地批式 Fun-ASR-Nano**（现有 `--engine local`） | —（整段出） | ❌ 无 partial | 6 s 窗口 → 2.80 s；12.455 s → 4.21 s（RTF 0.32–0.34） | 已有，本地 | 任务 `01a11829` |
| **本地流式 llama-funasr-cli `--chunk 2`** | **首句 T+1.72 s**（非逐字） | ❌ 只有「句」级 | 同段 | 已有，本地 | 任务 `01a11829` |
| **云端 AUC 录音文件**（火山，现有 key） | 整段可用 2.5–2.6 s（RTF 0.21） | ❌ 无 partial | 42 字 1 处同音 | 音频出机 | 任务 `01a11829` |
| **sherpa-onnx streaming zipformer（本报告）** | **T+0.438–1.365 s**（真实逐字） | ✅ **14–45 次 / 20 s，中位 0.31–0.44 s** | 自然语音流式 16.87%（同段批式 18.07–24.10%） | 零云、单文件二进制 + 模型 | 本报告 |

**读法**：本报告填掉的是前三条参照都缺的那一格——**「持续出字」**。
前三条里最好的是 llama-funasr `--chunk 2` 的「首句 T+1.72 s」，那是**句级**；
sherpa 给出的是**字级**：自然语音上 0.44 s 出第一个字，之后每 0.31 s 加字。
延迟这一维 **不输任何一条参照**；准确率这一维 **不如现有本地批式**（见 §6）。

---

## 3. 引擎与模型的真实执行证据（Rule 31）

**① 命令行跑通（bin `sherpa-onnx`）** —— 批式，stdout/stderr 分流已核：

```bash
./sherpa-onnx-v1.13.8-linux-x64-static/bin/sherpa-onnx \
  --encoder=…/encoder-epoch-99-avg-1.onnx --decoder=…/decoder-epoch-99-avg-1.onnx \
  --joiner=…/joiner-epoch-99-avg-1.onnx --tokens=…/tokens.txt \
  --num-threads=2 --model-type=zipformer fixture.wav 2>plain.err | cat -A
# stdout: Start to create recognizer / Recognizer created in 1.32706 s   ← 只有这两行
# stderr: fixture.wav / Number of threads: 2 / … RTF = 0.66/12 = 0.053
#         + 每个 segment 一条 JSON：{"text": "语音", "tokens": ["语","音"], "timestamps": [3.52,3.64], …}
```

`timestamps` 是**引擎自己的词级时间戳**，与 §1 的外部时钟是两条独立证据链
（本报告只把外部时钟当作数字来源，引擎时间戳仅作旁证）。

**② WebSocket 真流式跑通（本报告数字的来源）** —— 见 §1 命令，
`sherpa-onnx-online-websocket-server` 真实响应 14–45 条 partial。

**③ 真麦克风链路跑通（`sherpa-onnx-microphone`）** —— 用 PulseAudio
null-sink + remap-source 造确定性麦克风（前序任务钉住的 recipe），
喂 `5 s 静音 + 12.455 s 夹具`，识别器**实时**打印带 `[2K` 重绘的 partial：

```
0:我们
1:语音  1:语音输入  1:语音输入颜  1:语音输入颜值
2:是  2:是我  2:是我杀
3:刘  3:刘志  3:刘志峰  3:刘志峰一  3:刘志峰一天  3:刘志峰一天丰收  3:刘志峰一天丰收起来
```

（`live-mic-partials.txt`，共 15 次重绘。**这条腿没有时间戳**——`-microphone`
把 partial 以 `\r` 原地重绘，没有逐帧时钟，所以**本报告所有延迟数字都不来自这条腿**，
它只证明「麦克风 → 真流式 → 逐字出字」这条链路在本机是通的。）

Pulse 设备**用完即卸、默认源已还原**（Rule 35）：

```
$ pactl unload-module 83 && pactl unload-module 82
$ pactl set-default-source q2bsrc      # 恢复为运行前的值
$ pactl get-default-source  →  q2bsrc  （was q2bsrc）
```

**④ 环境事实（可复核）**：

- 发布包：`sherpa-onnx-v1.13.8-linux-x64-static.tar.bz2`（404 135 811 B，
  sha256 `d265986f…e297cc`）经 `ghfast.top` 镜像取得（**本机直连 GitHub 常被墙**）。
- 二进制 **静态**（`ldd` 只有 libc/pthread/dl/m）：`sherpa-onnx` 36 048 968 B
  （sha256 `3bff5b52…aa621d`）、`sherpa-onnx-online-websocket-server` 36 650 752 B、
  `sherpa-onnx-microphone` 36 191 944 B。
- 发布包**只带 `.so`，不带 `.a`**（`find … -name '*.a'` 为空）：
  shared 包解开后是 `libsherpa-onnx-c-api.so` / `libsherpa-onnx-cxx-api.so` /
  `libonnxruntime.so`（共 83 MB）+ 两个 C 头文件。
- 模型（均 **Apache-2.0**，README 首行 `license: apache-2.0`）：
  - `streaming-zipformer-zh-14M-2023-02-23`：fp32 三件 55 567 891 B（53 MB）；
    **int8 三件 25 305 928 B（24 MB）**；`tokens.txt` 48 697 B。
  - `streaming-zipformer-small-bilingual-zh-en-2023-02-16`：fp32 约 106 MB；
    int8 三件 49 696 018 B（47 MB）；`tokens.txt` 56 317 B。

**⑤ 速度对照（同一段 20 s，含模型加载）**：

| 形态 | zh-14M | small-bilingual |
|---|---|---|
| 流式（WebSocket，`--pace 0`，含 server 起 + 加载） | **3.635 s** | **3.172 s** |
| 批式（CLI 一次成文） | **4.328 s** | **6.317 s** |

220 s 整文件批式：zh-14M 15.817 s（≈14× 实时）、small-bilingual 29.318 s（≈7.5× 实时）。
**注意**：「约 10–14 倍实时」是**解码速度**，不是用户感知的延迟——实时供料下
用户看到的仍是 §1 的 0.44–1.37 s 首字。

---

## 4. Rule 35 四问（本次**不**新增持久依赖，逐条回答）

**（1）没有它会坏什么？** —— 没有它不会坏任何东西：本次**没有新增持久依赖**。
下载解压出来的二进制与模型全部落在本任务 session 目录（`$GOPI_TASK_REPORT_HOME` 下的
`artifacts/asr-stream-eval/`，位于 `/home` 分区、dev=2052 **离 `/`**，Rule 22），
用完即可删；PulseAudio 的两个 module 已 unload、默认源已还原；**没有装包、没有写
cron/daemon/服务、没有改 gopi 代码、没有动 settings.json**。
如果要把 sherpa 接入 gopi，那就是**新的持久依赖**，四问在 §6 逐条回答。

**（2）为什么不能进程内解决？** —— 本次是**评估**任务，进程内的解法（c-api 静态链接
进 gopi 二进制）本身就是被评估的对象之一，结论见 §6：发布包不给 `.a`，
进程内化需要 cgo，会打破 gopi `CGO_ENABLED=0` 单二进制这条性质。

**（3）谁负责它的生命周期、怎么回收？** —— 本次产生的所有物**归属本任务 session 目录**，
删除命令（说清即执行，本报告交付前不删是因为它们是可复核证据）：

```bash
D=/home/user/.gopi/sessions/01a11854-06b2-7bb1-95f0-ed8f7b618a6d/artifacts/asr-stream-eval
rm -rf "$D/sherpa-onnx-v1.13.8-linux-x64-static" "$D/shared" \
       "$D/sherpa-onnx-streaming-zipformer-zh-14M-2023-02-23" \
       "$D/sherpa-onnx-streaming-zipformer-small-bilingual-zh-en-2023-02-16" \
       "$D"/*.tar.bz2 "$D"/full220.wav "$D"/nat20.wav "$D"/padded.wav "$D"/esp*.wav "$D"/regen*.wav
```

**（4）下一个发现它在跑且不知道来历的人会看到什么？** —— 会看到**本报告**：
下载 URL、sha256、解压路径、启动命令、卸载命令、以及「为什么在这里」。
判断依据是 **host 级读法**而非猜测：`ps` 无残留进程（server 已 kill、microphone 已退出）、
`pactl list short sources` 只剩原有的 2 个源、`git status --porcelain` 为空。

---

## 5. 未跑通 / 未验证项（如实标注）

1. **本机无麦克风硬件**：`sherpa-onnx-microphone` 用的是 PulseAudio
   null-sink + remap-source 的**虚拟麦克风**，不是真人对着真麦克风说话。
   真人语音的端到端延迟 = §1 的数字 + 采集链路延迟，**采集那一段未测**。
2. **`sherpa-onnx-microphone` 不做时间戳**（`\r` 原地重绘），所以**没有**
   来自「真麦克风」那条腿的首字/间隔数字。
3. **未测更大的 zh 流式模型**：`streaming-zipformer-bilingual-zh-en-2023-02-20`
   （511 MB）、`streaming-zipformer-ctc-multi-zh-hans-2023-12-13`、
   `streaming-paraformer-bilingual-zh-en`、`zipformer2-ctc`、`small-bilingual` 的
   **int8** 版本（本报告只用 fp32 出数字；int8 只跑过文本抽查：
   夹具上 `语音圈`、模型自带 `0.wav` 上多冒出两个 `呢`——**未纳入数字表**）。
4. **未测 VAD 组合**（`sherpa-onnx-vad-*`、`--enable-endpoint` 的 4 条 rule
   参数扫描）；本报告只观察到默认 rule2（1.2 s 尾静音）在夹具上切了 3 次刀。
5. **未测进程内集成**（cgo + c-api 静态/动态链接进 gopi）：发布包不带 `.a`，
   只带 `.so` + 头文件；**从源码构建 sherpa-onnx（cmake + onnxruntime）未尝试**，
   所以「能否得到真正 `CGO_ENABLED=0` 的实现」= **未验证**（§6 的依据是发布包内容，
   不是构建实验）。
6. **CER 口径的局限已写在 §1**：夹具是合成音，其绝对 CER 不是人类语音准确率；
   自然语音的 83 字窗口是锚定窗口，且 20 s 的流式与批式同表可比、与整文件口径不可比。
7. **未做 12.455 s 之外的长句流式**（自然语音 20 s 是流式最长的一段）；
   220 s 只有批式数字。长时间流式的内存/状态漂移**未测**。

---

## 6. 「是否接入 gopi」的明确建议

### 建议：**暂不接入（NO）**，但把它列为「若换更大的 zh 流式模型，优先复用本装置复测」

**支持接入的一面（都是实测）**：

- 真流式三个数字齐备：首字 0.44–1.37 s、持续出字中位 0.31–0.44 s、解码 10–14× 实时；
- **零云依赖**、无 token 成本、音频不出机；
- **单文件二进制**（36 MB，静态，`ldd` 只有 libc）+ 一个模型目录即可跑；
- 许可干净：引擎与两个模型均 **Apache-2.0**；
- 交付形态好：既可以是 CLI 子进程，也可以是 WebSocket 服务，
  gopi 现有 `transcribeFile` 那条缝**不需要改引擎语义**就能旁挂。

**反对现在接入的一面（也都是实测）**：

1. **准确率是拦路虎，不是延迟**。现有本地批式 Fun-ASR-Nano 在同一材料上是
   **整文件 12% 级**；而 14M 流式小模型在自然语音 20 s 上是 **16.87%**，
   批式同段 18.07–24.10%。**接入 = 用准确率换延迟**，而这个交换在
   「语音输入要能写出用户说的话」这个场景里通常不划算。
   （夹具上 70%/92% 更是说明 14M 小模型对**非母语训练域的口音/合成音**很脆。）
2. **首字 1.3 s 里有一大半是「前端上下文需求」，换架构省不掉**。
   实测纯计算下限 0.091 s ⇒ 实时供料下 1.32 s 首字的来源是「模型要吃进约
   1.3 s 音频才敢落第一个字」。这与前序任务测到的 llama-funasr `--chunk 2`
   首句 T+1.72 s 是**同一个量级**——本地流式的首字并没有数量级优势。
3. **Rule 35 的账要先算清**：接入 = 新增持久依赖（36 MB 二进制 + 24–47 MB 模型
   + 一套模型下载/校验/回收流程），而**这次评估的收益证明不了它该存在**。
   四问里第（1）问「没有它会坏什么」目前答不上来：现在的痛点是**架构 86%**
   （10 s 空等 + 6 s 硬切窗口），那个痛点的解法在任务 `01a11853`，
   **加一个引擎解决不了**。
4. **进程内化会打破 `CGO_ENABLED=0`**：发布包只有 `.so`（无 `.a`），
   进程内集成走 cgo + `libsherpa-onnx-c-api.so` + `libonnxruntime.so`（83 MB），
   等于把 gopi 的「一个静态二进制」变成「二进制 + 3 个 .so」；
   不搞 cgo 就只能**起子进程**——那也是一条持久依赖，只是换了个形状。

### 如果将来要重新评估（条件式建议）

**触发条件**（任何一个成立就复测）：

- gopi 侧先把「10 s 空等 + 6 s 硬切窗口」修掉（任务 `01a11853`），
  届时**流式的相对优势才可能显现**；
- 决定在本地做**长句/连续口述**（超过 6 s 的口述目前必丢，前序任务实测 ≈50%）；
- 出现一个**中文准确率接近现有批式**的流式模型。

**复测怎么做（复用本装置，成本约 10 分钟）**：

```bash
# 换模型只改 --encoder/--decoder/--joiner/--tokens 四行
./bin/sherpa-onnx-online-websocket-server --port=6007 … &     # 新模型
python3 measure_stream.py --wav <同一夹具> --url ws://127.0.0.1:6007 | tee new.jsonl
python3 scripts/asr_score.py <拼接文本> <ref.txt>             # 同一把尺子
```

**先决条件**：需要一个**能代表真人语音的夹具**（建议直接用一次真麦克风录制，
而不是 espeak 合成音——本次的最大经验教训就是合成音会同时污染 CER 与首字判断）。

### 若要接入（另提任务时的形态建议，本次**不做**）

- 作为**引擎选项**旁挂在既有 `transcribeFile` 缝上，默认仍 `local`，绝不改默认；
- 用**子进程**形态而非 cgo（保住 `CGO_ENABLED=0` 单二进制）；
- 模型与二进制放 `<config.GopiDir>/voice/sherpa/`，附下载/校验/缺失时的**响亮失败**；
- 接入前必须补：真人夹具 CER + 与现有 Fun-ASR 的 A/B（同一批音频、同一把尺子）。

---

## 7. Verification（本报告数字的可复现命令）

```bash
# 环境
cd /home/user/.gopi/sessions/01a11854-06b2-7bb1-95f0-ed8f7b618a6d/artifacts/asr-stream-eval
B=https://ghfast.top/https://github.com/k2-fsa/sherpa-onnx/releases/download
curl -sL -o sherpa-bin.tar.bz2 $B/v1.13.8/sherpa-onnx-v1.13.8-linux-x64-static.tar.bz2
curl -sL -o zh14m.tar.bz2     $B/asr-models/sherpa-onnx-streaming-zipformer-zh-14M-2023-02-23.tar.bz2
curl -sL -o small-bil.tar.bz2 $B/asr-models/sherpa-onnx-streaming-zipformer-small-bilingual-zh-en-2023-02-16.tar.bz2
tar xjf *.tar.bz2

# 夹具
espeak -v zh -s 150 -w esp.wav "今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。"
ffmpeg -v error -y -i esp.wav -ar 16000 -ac 1 -acodec pcm_s16le fixture.wav
printf '%s\n' '今天我们用gopi做一个语音输入的延迟实验，测试火山引擎双向流式识别是否已经开通授权。' > ref.txt

# 三数字（流式）
./sherpa-onnx-v1.13.8-linux-x64-static/bin/sherpa-onnx-online-websocket-server --port=6007 … &
python3 measure_stream.py --wav fixture.wav --url ws://127.0.0.1:6007 | tee partial-zh14m.jsonl
python3 -c "…"  # 见 summary.txt 里的聚合脚本：拼各段末次 partial
python3 /home/user/workspace/gopi-01a1185406b27bb195f0ed8f7b618a6d/scripts/asr_score.py stream-zh14m-fix.txt ref.txt

# 自然语音 20 s
ffmpeg -v error -y -ss 30 -t 20 -i /home/user/work/whisper.cpp/2084无颜见人0.mp3 -ar 16000 -ac 1 -acodec pcm_s16le nat20.wav
python3 measure_stream.py --wav nat20.wav --url ws://127.0.0.1:6007 | tee partial-zh14m-nat20.jsonl
python3 score_window.py stream-zh14m-nat20.txt ref-nat20.txt "不可分割的延伸人们称之为"

# 真麦克风腿（Pulse 虚拟麦克风，用完还原）
pactl load-module module-null-sink sink_name=asrnull
pactl load-module module-remap-source master=asrnull.monitor source_name=asrnullmic
pactl set-default-source asrnullmic
./sherpa-onnx-…/bin/sherpa-onnx-microphone --tokens=… --encoder=… --decoder=… --joiner=… &
paplay --device=asrnull padded.wav      # 5 s 静音 + 12.455 s 夹具
kill -TERM %1
pactl unload-module <remap> && pactl unload-module <null> && pactl set-default-source q2bsrc
```

`summary.txt` 是 §1 三张表的生成器（逐条打印首字、间隔、次数、拼接文本）。
