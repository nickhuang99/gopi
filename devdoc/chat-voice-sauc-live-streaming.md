<!-- doc-class: normative -->
# `gopi chat` 的 `/voice`：sauc 引擎接上 v3 双向流式端点（`bigmodel_async`）

**任务** `01a13303-ab2d-7210-8053-e2ee2af0ce18` · 分支 `task/01a13303ab2d72108053e2ee2af0ce18`

## 一句话

`audio_engine = "sauc"` 时，`gopi chat` 的 `/voice` **不再**用 6 秒窗口循环，而是走 v3 的
**双向流式端点**（`sauc/bigmodel_async`）：音频边说边发，同一句被服务端反复改写，文字在编辑行里
**边听边长**。v2（`streaming`）通道字节不变，两个引擎共用同一套 utterance 控制流。

## 实测（本次真跑，非推断）

命令与原始输出：`<session>/artifacts/live_sauc_probe.out`（真实端点、真实凭据、真实音频）。

| 事实 | 值 |
|---|---|
| 端点 | `wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async` |
| 音频 | `/tmp/rec1.wav`，4832 ms（16 kHz mono PCM16） |
| **首个 partial** | **2499 ms**（音频里首段话约在 2000 ms 处才开始） |
| partial 数 | 8，末条 `definite=true` |
| 改写节奏 | `能听到`(2.5s) → `能听到吗`(2.9s) → … → `能听到吗？不知道能不能听到声音呢？`(5.0s)，约每 300 ms 一次 |
| 终文本 | `能听到吗？不知道能不能听到声音呢？` |
| vendor logid | `20261013103608517010298CFA3FC453E0`（第二次 `…1036220D537C14D9C88D6548A8`） |
| `FinishTimedOut` | false（服务端自己以 last 包结束） |

**这条实测否决了一个计划里的假设**：v3 的响应体**没有** `payload_msg` 包裹层。包内 `parseResponse`
读的顶层 `result.text` **就是**逐字文本（vendor SDK 快照 `sdk-snapshot/protocol/response.go` 暗示有包裹，
实测没有）。所以**没有**新增 `payload_msg` 信封 —— 那会是一个没人发的字段。

## 设计：引擎缝在 utterance 之上，不在协议之内

```
/voice  ──► runVoiceLiveSession（feeder / clock / 结束等待 / 手上余音 flush / finish 键）
                     │  voiceLiveSession 接口
        ┌────────────┴─────────────┐
   v2 适配器                     v3 适配器          ← cmd/agent/voice_live_engines.go
 *doubaostream.Session        *doubaosauc.Session
      (协议不同，传输相同)          (协议不同，传输相同)
```

- **控制流只写一遍**。`voice_stream_runtime.go` 里那些义务（说过的字一个不许丢、第一个失败必须是
  被报出来的那个）与"哪个信封在运音频"无关；按引擎复制它 = 复制义务，而那正是本文件头注释
  记录过的、已经在三个入口之间漂移过一次的东西。
- **引擎自己的词汇留在引擎的适配器里**：各自的 options 类型、Result、Partial，以及各自的
  "已经结束了"哨兵（`doubaostream.ErrSessionFinished` / `doubaosauc.ErrSessionFinished`）。
  运行器不认识任何一方（Rule 3）。
- **两个引擎的结果在缝上被翻译成 `voiceLiveResult`**（文本/logid/reqid/音频毫秒/partial 数/
  首 partial 时刻/服务端时长/耗时），所以 v2 的表面输出**逐字节不变**，v3 的图在同一行上可比。

## `internal/doubaosauc/session.go`：新增的 LIVE 半边

`Transcribe` 的契约是"一个文件进、一个串出"，它做得到只因为文件把音频一次交完（它按音频时间轴
自己造时钟）。交互式听写没有文件：**调用方的采集就是时钟**，所以这个包不该替它定速。于是加了
`Session`（`Open` / `Say` / `Finish` / `PartialCount` / `FinishTimedOut` / `Close`）。

三条读侧决定，每条都是可测的：

1. **流阶段不设读超时**（`sessionFlowReadTimeout = 0`）。读超时可以在**帧中间**到期，帧状态不可续读
   ——再读就把那一帧剩下的字节当头解析。说话期间超时也发现不了任何事，所以读侧就停在帧边界等。
   这是**构造上不可能**，不是"检测到了"。（`doubaostream` 的同一处记录过这个实测缺陷。）
2. **排空阶段才设超时**，并且只有在读者还在读时才设 —— 从发出结束标志那一刻起，"服务端不说了"
   正是要发现的事。连续 `drainQuietReads` 次静默 = 结束。数字与文件臂共用 `drainReadBudget`
   （是同一个服务端事实，不是巧合）。
3. **`Finish` 等在 `readerDone` 上，而不是猜它已经到了**。等待有界；到期就
   `FinishTimedOut()=true`，把"少了一截"**报出来**，绝不静默截断。

## 端点：一个 knob，两个默认值，各自服务自己那种活

`voiceSaucEffectiveEndpoint`（文件臂，`bigmodel_nostream`）与 `voiceSaucLiveEndpoint`
（LIVE 臂，`EndpointBidirectional` = `bigmodel_async`）**默认值不同是故意的**：
文件臂的契约是一进一出，最后给答案就够；LIVE 臂存在的理由就是"说话时就有字"，
在文件语义端点上它会直到说完才出声 —— 那正是被替换掉的窗口循环的行为，且不带窗口循环的任何理由。

`audio_engine_param.sauc.endpoint` **两边都逐字胜出**。两个默认值不是矛盾，是两种活。

## 回归证据（新增用例）

- `internal/doubaosauc/session_test.go` —— 一个说本包自己信封的假服务：
  - partial 在**下一个包发出之前**到达（"边发边出字"这件事被断言，不是被假设）；
  - 尾部以**取负的序列号**发出且字节不差（结束义务）；
  - `Say` 在 `Finish` / `Close` 之后各自返回**具名**哨兵（竞态 ≠ 传输坏——异步臂要能分辨）；
  - **`Close` 不发送结束标志**（中止路径不得宣告"说完了"）；
  - **流阶段不设超时**：服务端中途停摆超过排空预算，partial 依然到达（帧内超时的回归守卫）；
  - 结束标志后服务端一言不发 → 以静默结束，且耗时有界。
- `cmd/agent/voice_live_engines_test.go`：
  - 引擎选择真值表**覆盖每一个引擎**（表不完整即失败 —— 新引擎必须表态）；
  - 两个流式引擎各拿到**自己类型的 options**，错类型被拒绝（否则会拿零凭据去开连接，
    再把 401 报成操作者的错）；
  - 两个臂的端点默认值各自成立，且配置值**两边都赢**；
  - 一次 utterance 用 double 走完：字节不丢、partial 带时刻上抛、手上余音经 `Finish` 尾部出、
    会话被关、引擎结果进报告；
  - 具名竞态拒绝**不**被报成失败，真实传输失败**仍然**被报；
  - 没引擎的运行被**具名**拒绝（不是 nil 调用）。

## 踩过的坑

- `responseDoc.Result` 从匿名 struct 改成具名 `responseResult`：匿名 struct 无法在调用点命名，
  两个读者（包内测试、任何想陈述响应形状的调用方）就得把字段抄一遍。
- 假服务读音频帧的偏移量必须用协议常量算（`headerSize4Bytes + sequenceFieldBytes + lengthFieldBytes`），
  我第一版用 `2*headerFieldBytes` 猜，把 4 字节序列号读成了头 —— 三个用例同时报 0 字节。
- 假服务的 partial 要挂在**Say 里**发（会话的读侧只在包与包之间出字），不能挂 `time.Sleep`：
  否则断的是自己的调度器。

## 未做 / 已知

- `make test-budget PKGS='./internal/doubaosauc ./cmd/agent'` 的完整结果见任务报告；
  `go test ./internal/standards/` 在本机 master 上**已经红**（`CONFIG.md:174` 的锚
  `cmd/agent/main.go:805` 落在空行），与本差异无关，已按 Rule 41 取证并提行
  `01a1331f-1b81-71d1-84d5-b6b3efc90fd0`。
- `gopi chat --film` 的中文在 xterm 的 core bitmap 字体下是方块（本机已知策略），
  端到端驱动改用包内探针（同一组被改函数）真跑，证据见上表。
