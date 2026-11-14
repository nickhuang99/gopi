<!-- doc-class: historical -->
# `/voice` 的「54 秒零文本」——两个缺陷，一次事故（task 01a11b39）

**结论先说**：operator 看到的「连续说话 54 秒、屏幕零文本、既没有转写提示也没有静音提示」，
不是引擎没听见，也不是文本被滚动遮住。是**两个缺陷叠在一起**：

1. **引擎的「这段音频没有说话」判决被当成了「这一轮失败」，而失败会让模式退出。**
   一个安静窗口 = 麦克风被关掉，此后 operator 说的每一句都属于一个已经关闭的模式。
2. **被切在语音中间、又没出字的窗口，在屏幕上和「房间很安静」长得一模一样。**
   两张提示都在，但没有一句话把「切」和「丢了字」连起来。

---

## 一、现场证据（operator 的 dump，2026-10-08，`gopi ui_state --target chat` 读出）

```
[gopi] VOICE MODE ON — recording 6s from pulse:@DEFAULT_SOURCE@ ... every 6s; each transcript is APPENDED at the caret
                                   ←←← 这里本该是转写文本：整段区间完全空白
[voice] window boundary cut inside speech after 9 extension(s) of 6s — ...
[gopi] voice mode OFF (Esc) — the recorder is stopping; the text already on the line is kept.
```

三个可读的事实，和 briefing 里的疑问 B、C 一一对应：

| 观察 | 含义 |
|---|---|
| `after 9 extension(s) of 6s` | 一轮窗口最长可被延长到 (1+8)×6s = **54 s**（`voiceWindowMaxExtensions = 8`）。`9 extension(s)` **只能**出现在「预算用尽、不得不切在语音中间」的那一条路径上（`cli_voice_recorder_staged.go`：`span.Extended++` 后 `Extended <= 8` 才 continue，第 9 次落到 `cut(pcm, true)`）。⇒ operator 那次 54 秒的说话**只产生了 1 个窗口**，不是 9 个。 |
| ON 与 OFF 之间零文本、零 `no text in the last … window` | 沉默提示一次都没打印过。若引擎回的是空串，这一行**必然**出现 ⇒ 引擎回的不是空串，而是别的路径。 |
| 输入行 `you>` 始终为空 | 没有任何东西被插入。 |

## 二、根因 A：`20000003` 是「完成」，不是「失败」

同一个引擎、同一台机器，`gopi tool voice_input` 正常出字；`/voice` 零字。差别在**送什么**，
但更关键的是**怎么解释引擎的答复**。

doubao AUC 通道对一个「收到了、但里面没有说话」的窗口，回的是**一个已完成的任务 + 空转写**：

```
HTTP 200  X-Api-Status-Code: 20000003
X-Api-Message: Normal silence audio Handle response: no valid speech in audio
body: {"audio_info":{"duration":6000},"result":{"text":""}}
```

`duration=6000` 是关键：音频**完整到达**了，判决是关于音频内容的，不是关于上传的。

而修复前 `transcribeFileDoubao` 把**任何**非 nil error 当成一轮失败，
`voiceChatMode.loop` 的 `if err != nil { m.fail(err); return }` 于是**离开模式**。
于是：**一个安静窗口 = 麦克风关闭**。

### 真实现场复现（不是单测）——A/B，同一 fixture、同一台机器

fixture：18 秒中文语音（espeak 合成 → `q2bsrc` 这个 PulseAudio 虚拟源，无需真人），随后音源静音。

**A：修复前的二进制**（`~/work/GoPi/bin/gopi`）：

```
[gopi] voice mode FAILED — doubao engine: doubaoasr: query request refused: HTTP 200
  status="20000003" message="[Normal silence audio] Handle response: no valid speech in audio"
  logid=20261008193840B4E45D12C25D5C367B08
  body={"audio_info":{"duration":51999},"result":{"additions":{"duration":"51999"},"text":""}}.
  The mode is OFF; the text on the line is kept.
[gopi] voice mode OFF (a failed round) — the recorder is stopping; ...
```

`duration=51999` —— **51,999 毫秒的一个窗口**，正好是 54 s 上限附近的同一个形状。
屏幕上只出现了前面 18 秒的三行文本，然后模式就死了。这就是 operator 的事故。

**B：修复后**（同一 fixture、同一脚本）：

```
[voice] 现在开始连续语音输入测试。
[voice] 请仔细听，今天是10月8日，我们在这台机器上做一次完整的手术验证。
[voice] 第一句语音识别的目标是把连续的说话变成连
[voice] this window was cut inside speech after 9 extension(s) of 6s and produced NO text —
        a word split across two windows can come back as silence; a short pause between
        phrases gives the cut a gap to land in. Still listening.
...
[gopi] voice mode OFF (Esc) — the recorder is stopping; ...
```

模式**一直活着**（状态栏 `rec · ready`），安静窗口只打印一行提示，下一轮继续。

### 修复

- `internal/doubaoasr`：新增 `StatusNoSpeech = "20000003"`（**唯一定义**，Rule 1/13）+ `APIError.NoSpeech()`。
- `cmd/agent/cli_voice_engine.go`：`errors.As` 到 `*APIError` 且 `NoSpeech()` ⇒ 返回 `("", nil)`——
  **它本来就是「空转写」，现在如实报告为空转写**。其他判决（403、配额、网络）照旧失败，
  且错误里仍然带着厂商自己的 code（Rule 15）。

## 三、根因 B：一个「没出字的切分窗口」在屏幕上和「安静房间」无法区分

修复前：

- `chatVoiceWindowSplitNote`（`cut inside speech …`）**只在 `text != ""` 的分支里**打印；
- `chatVoiceSilentNote`（`no text in the last … window`）不提出口被切的事。

于是**恰好那一次事故**（切在语音中间 + 引擎没出字）打印的两行，读起来是自相矛盾的：
一句说「切在语音中间，词可能被拆开」，一句说「这个窗口没有说话内容」。
operator 因此被告知「我们什么都没听到」——而音频明明完整送到了引擎。

**这正是 briefing 的疑问 B**：为什么只有 `:902` 的提示、没有 `:894` 的提示。
答案不是「文本被清掉了」，也不是「插入失败」：那一轮**根本没有 text**，
所以 `:894` 那一行不在它的代码路径上，而 `:902` 那一行是**不受 `text != ""` 约束**的
（它在循环末尾、被 `if span.Split` 守护）。现在的修复让 `Split` 这个事实对**两条分支**都可见：

```
[voice] this window was cut inside speech after %d extension(s) of %ds and produced NO text —
        a word split across two windows can come back as silence; a short pause between
        phrases gives the cut a gap to land in. Still listening.
```

- 新常量 `chatVoiceSplitSilentNote`（Rule 1/13）；
- 新方法 `voiceChatMode.lastWindowSplit()` —— **边界只读一次**，两条分支共用，
  不含 recorder 的 recorder 返回 `(false, 0)`（诚实读法：这个 recorder 不报告边界）；
- 窗口**自己到点收口**（`Split == false`）时**仍然**打印原来那句普通沉默提示 ——
  否则「切在语音中间」这句话会因为到处出现而失去意义。

## 四、疑问 C：是「引擎没出字」还是「出了字看不见」

**是前者，而且更糟：出过字，但模式在 early round 之后就已经退出了。**

A/B 里 A 的屏幕 dump（`runA-prefix/01-on.grid.txt`）显示：前 18 秒语音期间**确实出了三行文本**
并追加到了输入行，随后第一个安静窗口就把模式杀掉。25 行屏幕确实会滚动，但**不是**本事故主因：
即使在完全滚回可见范围的 10 秒内截屏，`you>` 输入行也是空的。

顺带一个**独立的**、非致命的观察（**未在本任务中修改**）：
窗口被切在语音中间时，切点落在最后一个静音「run」的开头，而该 run 的长度**只要求 ≥ 200 ms**
（`voiceCaptureTrailingSilenceMS`），不要求是一个真正的停顿。因此连续说话时，
**词可能被真的切成两半**：A/B 的实测文本里 `把连续的说话变成连` 之后就是下一窗；
`voice_input` 送整段时是 `变成连续的文本`。这是**部分丢词**（不是整段丢失），
operator 现在能在屏幕上看到 `cut inside speech … a word may be split` 这句话，
但切点判据本身没有改。`run<6s-window → 必然 9 extensions` 的根因（bound 时钟与
「静音 run ≥200 ms 才切」冲突）见下。

> **为什么会必然用满 9 次 extension**：`voiceWindowPlan.cut` 要求切点所在的静音 run
> **起点 ≥ minBytes(1.5 s)**。这个 run 是**短促**的那个（约 200–400 ms），
> 于是只要它落在 1.5 s 之内，`runStart >= p.minBytes` 就不成立，候选被丢弃，
> 窗口只好一直延长到预算耗尽、切在语音中间。改造它需要重新设计 `minBytes` 的语义，
> **超出本任务 60 分钟预算**，已按 Rule 26 作为独立缺陷提交（见 `## Not Done`）。

## 五、验证

```
Build: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → BUILD_OK
Tests: make test-budget PKGS='./cmd/agent ./internal/doubaoasr'   → 见下方
Ran:   ./run-rig.sh short18.wav runA-prefix  (~/work/GoPi/bin/gopi)   ← 修复前
       ./run-rig.sh short18.wav runC-final   (bin/gopi)               ← 修复后
Result: A 用同一 fixture 复现 operator 事故（20000003 → The mode is OFF）；
        B/C 同一 fixture 下模式保持 ON，安静窗口打印切分+沉默合并提示。
```

### 红→绿（Rule 10.5：每个修复带最小回归测试）

| 测试 | 修复前 | 修复后 |
|---|---|---|
| `TestVoiceDoubaoNoSpeechIsAnEmptyTranscriptNotAFailure` | **FAIL**（真报错文本含 `20000003` / `logid=2026100819203030ED41BEECA127FF8C6E`，与 operator dump 同形） | PASS |
| `TestVoiceDoubaoSpeechStillTranscribes` | PASS | PASS |
| `TestVoiceDoubaoRefusalStillFails`（控制腿：真失败必须继续失败） | PASS | PASS |
| `TestDoubaoNoSpeechPredicateIsTheOneCode` | PASS | PASS |
| `TestChatVoice_SplitSilentWindowSaysBothFacts` | **FAIL**（旧沉默分支不打印出口被切） | PASS |
| `TestChatVoice_QuietWindowKeepsThePlainSilenceRow`（控制腿：没切就不许说切了） | PASS | PASS |
| `TestChatVoice_SilenceInsertsNothingAndSaysSo` / `_SilenceMarkerInsertsNothingAndSaysSo` | PASS | PASS |

复现方法（可重跑）：`/home/user/workspace/gopi-test-scratch/voice-chat-rig/run-rig.sh`
+ `rigplay.sh`（合成喇叭：等 `q2bsrc` 真的 `RUNNING` 再 `aplay`，否则音频落在没有 reader 的时间窗里）。

## 六、Not Done（诚实清单，可直接派单）

1. **切点判据未改**：`voiceWindowPlan.cut` 的 `runStart >= minBytes` 会让短促静音 run 被丢弃，
   使窗口在连续说话时**必然**用满 8 次 extension 再切在语音中间，造成**部分丢词**。
   本任务只让这件事**可见**（原来连看不见都算不上：屏幕上什么都没有）。
   **独立任务已提交**（见任务账本），根因与线索见本文第四节引用块。
2. **`--channels=1` 未动**（briefing 明令）。
3. **屏幕 25 行的滚动**：本事故不是它造成的（A 的证据），因此未改；若将来出现「早期轮次可见性」
   类反馈，那是独立问题。
4. **真人语音未测**：本机无人，验收用 espeak 合成音频 + PulseAudio 虚拟源（`q2bsrc`）。
   采样率/音量与真人不同，`voice_input` 整段路径在同一 fixture 上识别完全正确，
   但**真人麦克风的噪声底**会改变切点行为，这一点没有被本次验证覆盖。
5. **web/桌面等其他 /voice 宿主**未验证（本任务只覆盖 `gopi chat`）。
