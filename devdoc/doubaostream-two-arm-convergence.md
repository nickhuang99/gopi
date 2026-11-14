<!-- doc-class: normative -->
# doubao-stream 两臂收敛 — devdoc

**任务**: 01a11f139a707d0c8609049a90dce3c4
**分支**: `task/01a11f139a707d0c8609049a90dce3c4`
**worktree**: `/home/user/workspace/gopi-01a11f139a707d0c8609049a90dce3c4`
**证据目录**: `/home/user/.gopi/sessions/01a11f13-9a70-7d0c-8609-049a90dce3c4/artifacts/`

---

## 1. 收敛了什么：两条手写臂各自的「打包算术」副本

同一个 v2 流式会话（`api/v2/asr`, cluster `volcengine_streaming_common`）有两条客户端臂，
它们**各自算了一遍包长**：

| | file 臂 | chat live 臂 |
|---|---|---|
| 入口 | `doubaostream.Transcribe` (`internal/doubaostream/client.go` 发包循环) | `voiceLiveFeeder.absorb` (`cmd/agent/chat_voice_live.go`) |
| 包长 | `chunkSampleCount(opts.ChunkMillis) * bytesPerSample16` | `voiceLivePacketBytes = voiceLivePacketMillis * voiceCaptureSampleRate * voiceCaptureBytesPerSample / 1000` |
| 末包 | 循环内 `buildAudioPacket(samples[sent:end], last=true)` | 手工 `sess.Finish(tail)`（`chat_voice_live.go` 的 `takePending` + tail） |

`internal/doubaostream/audio.go` 的 `bytesPerSample16` 注释原文说自己是
"the ONE place a sample count becomes a byte count" —— 而 `cmd/agent` 那份是做乘法
第二次写出来。这正是 Rule 13/14 要拦的：**同一算术的第二份副本**。

### 收敛后的形状

新增 `internal/doubaostream` 的 **`PacketAssembler`**（`client.go`）—— 一个**只有几何、
没有时钟、没有状态**的值类型：

- `Bytes`：一个包承载的 PCM16 字节数（= `chunkSampleCount(millis) * bytesPerSample16`）
- `Millis`：这些字节代表的 cadence
- `FinalPacket(tail)`：末包构造（end-of-stream 标志位那一次 `buildAudioPacket(..., true)`）
- 三处读同一构造：`Options.packetAssembler()`（file 臂发包循环）、
  `Session.packetAssembler()`（`Session.Finish` 的末包）、
  `doubaostream.DefaultPacketAssembler()`（chat live 臂）。

`cmd/agent` 侧的 `voiceLivePacketMillis` / `voiceLivePacketBytes` **两个常量已删除**，
`voiceLiveFeeder.absorb` 改为读 `voiceLivePackets.Bytes`；测试引用的
`voiceLivePacketBytes` 同步改为 `voiceLivePackets.Bytes`。

## 2. 不能合并的部分 —— 机制理由（可核对）

**pacing（按音频时间轴排队）只对 file 臂有意义，这不是「不方便」，是两条臂的时钟性质不同。**

`internal/doubaostream/session.go` 的文件头已经写着这件事，收敛后它仍然是**唯一**的
pacing 声明，且收敛没有把它搬进共享结构里 —— 因为搬进去就必须有一条臂撒一个它没有的谎：

- **file 臂有外部时钟**：音频已经全部在手里，服务端按「第 n 包在第 n×chunk 时刻到」判超时。
  所以 `client.go` 发包循环用 `time.Until(audioStart.Add(index*chunkDur))` 给每包排队。
  实测代价（`internal/doubaostream/client.go:395-408` 注释里的历史测量）：不排队、
  改为每包等一次应答 → 10.3s 音频变成 29.6s 的流（3 倍计费），首个 partial 3.65s。
- **live 臂的时钟就是麦克风本身**：采样按真实时间到达，收包即发包就是「实时」的定义；
  再叠一层 timer 是对同一段音频的第二个时钟（`chat_voice_live.go` 的 `voiceLiveFeeder`
  注释：「IT OWNS NO TIMER」）。实测佐证见下方时延表：live 臂的首字块 7.32s、
  首字块→首个 final 0.05s —— 若它背后真有一条起作用的 pacing 队列，这两行不会成立。

**因此共享的是几何（`PacketAssembler`），不是调度。** 这一点写进了 `PacketAssembler`
的文档注释（"WHAT IT IS NOT: it holds no clock and no state"），使后来者若要把它扩成
一个「调度器」必须先读到这段理由。

## 3. 末包 + drain 收尾

- **末包**已经收敛：`Session.Finish(tail)` 与 `PacketAssembler.FinalPacket` 是同一构造
  （本次改动把 `session.go` 里手工的 `buildAudioPacket(tail, true)` 换成
  `s.packetAssembler().FinalPacket(tail)`）。
- **drain 本来就只有一份**，这在收敛前就是事实，本次只是把它写清楚：
  `Result.drain`（file 臂，`client.go`）与 `Session.readLoop`（live 臂，`session.go`）
  **共用** `defaultFinalReadTimeout` 与 `drainQuietFrames`
  —— `sessionDrainReadTimeout = defaultFinalReadTimeout`、`sessionQuietFrames = drainQuietFrames`
  （`session.go` 常量块原文注释："It is defaultFinalReadTimeout, the same budget the file arm's
  drain uses, because it is the same fact about the same service"）。两条臂**共享的是同一个
  数字与同一个「静默即结束」判据**，不是同一段控制流：file 臂是内联循环，live 臂是常驻
  reader goroutine + `Finish` 的有界等待（`sessionFinishWait = sessionDrainReadTimeout*(sessionQuietFrames+1)`）。
  **没有第二次手写副本可删**，本次没有为了「看起来合并了」而把常驻 reader 改成内联循环
  —— 那会破坏 `Finish`/`Close` 的等待与 teardown 契约（task 01a11dfe 修过的那套）。

---

## 4. 实测端到端时延对比（同一段音频、同一引擎）

**音频**: `artifacts/fixture16k.pcm` → `artifacts/fixture.wav`，16000 Hz mono s16le，
**7.227 s**（`ffprobe`: `Duration: 00:00:07.23`），内容为中文 + 英文混合人声。
**引擎**: `--engine doubao-stream`（cluster `volcengine_streaming_common`）。
**原始输出**: 下表每一行的来源文件都在证据目录里，逐字。

| # | 路 | 场景 | 首字节到引擎 | 首个 partial | 末次 partial / 落字 | 端到端总耗时 | 原始输出 |
|---|---|---|---|---|---|---|---|
| A1 | **file 臂** | `gopi tool voice_input fixture.wav --engine doubao-stream` | t=0（握手后立刻发第 1 包） | **1.42 s** | final **8.39 s**（服务端最后一个 partial） | **8.59 s** wall | `armA-file.out` |
| A2 | **file 臂** | 6.00 s 窗口（`-t 6.0` 裁出的 `window6s.wav`） | t=0 | 1.45 s | final 6.92 s | 7.16 s wall | `arm-window6s.out` |
| B1 | **chat live 臂** | 真 pty 驱动 `gopi chat`，`/voice` 进入，模式入场后 **8.0 s** 按 Ctrl+G 收尾 | 模式入场即开录 | **7.32 s**（`t_voice_ready=21.164` → `first_draft=28.481`，`armB-live-4.out`） | 收尾后 0.05 s 落字（`settle → text`（`[voice] 你好你好！`）；见 4.2 行"收尾"） | 模式入场→轮次结束 **9.86 s** | `armB-live*.out` |
| B2 | **chat live 臂** | 同上，**不按收尾键**，让采集自然耗尽 | 模式入场即开录 | **7.29 s**（`t_voice_ready` → `first_draft`） | — | — | `armB-live-fullwindow.out` |

### 4.1 「录满整窗再转写」何时发生、代价多少

- **file 臂不等待整窗**：音频是流式按时间轴发出去的，第 1 个包发出去后 **1.42 s** 就有
  partial（A1）。total 8.59 s ≈ 7.23 s 音频 + 1.36 s（握手 + drain 尾巴）。
- **chat 的 window 臂才是「录满整窗再转写」**：`voiceChatWindowSeconds = 6`
  （`cmd/agent/chat_voice.go:80`），窗口关闭之前**不可能**有任何文字。代价用 A2 实测：
  同一段音频，只要它比窗口长，**首个 partial 最早出现在窗口关闭之后**，即
  ≥6.00 s（本例引擎在整段音频上给首 partial 只要 1.45 s）→ **为了「整段一起转写」多等
  ≥4.55 s 才第一次看到字**。这就是 live 臂存在的理由，也是它的实测收益：
  B1/B2 的 7.32 s ≈ **7.23 s 音频时长 + ~0.09 s**（`voiceFileSource` 按真实速率喂，
  首包即首字块），即**文字在音频结束的那一刻就已经在行上**，而不是等窗口判定。
- **两条路的总耗时在同一个量级**（8.59 s vs 9.86 s），差别不在「快」，在**文字出现的时刻**
  （1.42 s vs 7.32 s）与**是否需要等人按键/等窗口关闭**。

### 4.2 chat live 臂的取证方式与其已知失败面（诚实记录）

`gopi chat` 没有无头入口，所以这一路是用**真 pty** 驱动的
（`artifacts/drive-live-chat.py`：`script -qec 'gopi chat' /dev/null` + pty master 写字节，
逐块记录到达时刻）。驱动方式本身就是本期的一处实测发现，见 §5.1。

**B1/B2 的落字一行来自 pty 捕获里的 `[voice] 你好你好！`（t=13.6s / 13.5s），
不是来自一次干净收尾。** 诚实说明：本机实际跑到的 4 次 live 运行里，
**收尾路径 3 次以 `server: code 101` 结束**（`armB-live-3.out`: `0ms of audio sent`），
即服务端没有任何音频到达；随后 `Session.Finish` 走
`internal/doubaostream/session.go:583` 的 `res.Text == "" && res.LastCode == 0` 分支，
报 "the session ended without a transcript and without a code saying why"。
**这是一处真实缺陷，本次没有修，也没有充分归因**：实测事实只有
（a）服务端 `AudioMS` 未上报、`0ms of audio sent`;
（b）采集侧确实在产音频 —— 否则 7.3 s 后不会有正确的 `你好你好！`；
（c）`chat_voice_live.go` 里唯一在 handle 建立**之前**就要用 sink 的调用是
`OnPartial: ... sink.Draft(p.Text)`，而 `sink` 只由 `sink.Settle(report.Text)` 的返回值
赋给 report —— 这条是**代码阅读得出的可疑点，不是实测归因**。
落这条的理由是 Rule 15：把它写成「本 arm 已端到端验证通过」会掩盖一个真实的失败面。
**已单独提交任务**（见报告 `## Not Done`）。

---

## 5. mp3：取证侧 ffmpeg 转码，代码内不解码

**原始输出**: `artifacts/mp3-refusal.out`（导出的命令 + 输出）

```bash
# 1) mp3 是由取证侧 ffmpeg 从 WAV 生成的（仅用于取证，不进仓库 — Rule 39）
ffmpeg -y -i artifacts/fixture.wav -codec:a libmp3lame -b:a 64k artifacts/fixture.mp3

# 2) 把 mp3 直接交给流式引擎 —— 代码拒绝它
$ gopi tool voice_input artifacts/fixture.mp3 --engine doubao-stream
gopi tool voice_input: doubao-stream engine: the streaming channel takes a WAV or bare PCM:
  these bytes are neither (no RIFF/WAVE header)

# 3) 文档给出的补救路径（cli_voice.go 的 --audio-source 段落原文）:
#    "resample with ffmpeg -i IN -ar 16000 -ac 1 -f s16le OUT.pcm"
ffmpeg -y -i artifacts/fixture.mp3 -ar 16000 -ac 1 -f s16le artifacts/fixture-from-mp3.pcm
```

**依据（引用到具体句子）** —— `internal/doubaostream/audio.go` 文件头原文：

> "Only the codecs a WAV container actually carries are decoded: integer PCM 8/16/24/32
> and IEEE float 32/64. Anything else (ADPCM, µ-law, an mp3 or an ogg) is **refused BY NAME**,
> because resampling or transcoding it would be a second silent guess about what the operator meant."

以及 `decodeSamples` 的拒绝句（同文件）：

> "the WAV's format tag %d with %d bits per sample is not one the streaming channel decodes
> (integer PCM 8/16/24/32 or IEEE float 32/64); **the bytes are refused rather than transcoded**"

这句不只写在注释里，**同一条路径真的拒绝**：`artifacts/fixture-adpcm.wav`
（ffmpeg `-codec:a adpcm_ms` 生成，是一个合法 WAV，因此正好走到 `decodeSamples`）实测输出：

```
gopi tool voice_input: doubao-stream engine: the WAV's format tag 2 with 4 bits per sample
  is not one the streaming channel decodes (integer PCM 8/16/24/32 or IEEE float 32/64);
  the bytes are refused rather than transcoded
```

**为什么不在代码里解码**：Rule 35 零依赖 —— 一个 mp3 解码器是**新增依赖**（或一段自研
比特流解析），而运算符已经有 ffmpeg。代码里加它，等于为「这个字节流是什么」再做一次
静默猜测，而正确做法是**拒绝并指名**，把定义权交回运算符。

---

## 6. 实测发现：pty 驱动的两个坑（写给下一个要驱动 `gopi chat` 的人）

1. **prompt 是写字节的许可**：`gopi chat` 启动约 5–7 s（加载 environment.md、画 banner），
   在那之前写进去的字节**没有任何东西消费** —— 实测 t=2.0 s 写的 `/voice\n` 从未出现在行上。
   必须等到 `you>` + 帮助行出现再写。
2. **粘包突发（paste burst）会吞掉 Enter**：`internal/lineedit/value.go`
   `PasteBurstMinChars = 3` / `PasteBurstCharInterval = 8 ms` / `PasteEnterSuppressWindow = 120 ms`。
   一次写 7 字节的 `/voice\n` 被判为粘贴，CR 变成**内容**（实测行上是 `you> /voice` 而未进入模式）。
   逐字节写（间隔 60 ms）后 `/voice` 才被当命令，但**CR 必须再单独拉开 ≥ 120 ms**
   —— 否则它仍是突发的尾字节，被插入为换行。最终配方：每字节 60 ms，CR 前 250 ms。

---

## 7. 验证

见 `tasks/01a11f139a707d0c8609049a90dce3c4-report.md`（Rule 5 的报告，含
Build / Tests / Ran / Result / Commit / Evidence）。
