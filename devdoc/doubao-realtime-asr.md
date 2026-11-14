<!-- doc-class: normative -->
# 豆包实时语音识别 — 端点、计费对齐、四个必调参数

**日期**：2026-10-13 · **来源 session**：`953a917d-9de5-4f7f-bd78-d0248ed4346d`
**权威来源**：`/home/user/Documents/byteDance/audio/豆包语音_实时语音识别_1790590224.pdf`
（14 页，火山引擎官方《豆包语音 实时语音识别》；纯文本副本 `/tmp/doubao-realtime.txt`）
**价格来源**：`/home/user/work/docs/doubao_audio_price.txt` +
`devdoc/doubao-asr-eval/evidence/pricing-1369-overview.md` / `pricing-1370-billing.md`

**一句话**：实时语音识别的正式接口是 `bigmodel_async`；**计费必须按资源 ID 对齐**（同名产品有两代）；
`enable_nonstream` 是让「边出字」与「最终准确」兼得的关键参数。

---

## 1. 端点：`bigmodel_async` 就是实时识别接口

```
POST wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async
```

文档全篇（14 页）**没有出现 `bigmodel_nostream`** —— 那是**文件语义变体**，不是本产品的接口。
官方定义：「低延迟的流式语音识别服务，在用户说话的过程中**持续返回增量识别结果**，
中间结果可能随后续音频的输入而更新。」

> **坑（实测踩过）**：仓库注释把 `bigmodel_async` 称作「**双向端点 / 双向变体**」——
> 那是**从协议形状**（FullClientRequest + 音频流 + 服务端持续回包）**推出来的**，
> **不是产品定义**。照抄它会给执行者一个错误的心智模型。
> **产品文档才是权威**（本页 §1 的依据是 PDF，不是注释）。

---

## 2. 三个名字、三条价 — 按资源 ID 对齐，**不能按名字**

账单文件里「流式语音识别」这个名字**出现多次**（分属 1.0 与历史小模型两条线），
**按名字对必然出错**。

| 名字 | 资源 ID | 端点 | 单价 | 免费额度 |
|---|---|---|---|---|
| **豆包流式语音识别模型 2.0** | `volc.seedasr.sauc.duration`（小时版）<br>`volc.seedasr.sauc.concurrent`（并发版） | `api/v3/sauc/bigmodel_*` | **1 元/小时**（资源包 30h=28 元 ≈0.93；300000h 档 0.8） | 20 小时/半年 |
| 大模型流式语音识别 **1.0**（历史版本） | `volc.bigasr.sauc.duration` / `.concurrent` | `api/v2/asr`（cluster `volcengine_streaming_common`） | **4.5 元/小时** | 20 小时/半年 |
| 豆包录音文件识别模型 2.0 | `volc.seedasr.auc` | `api/v3/auc/bigmodel/submit` + `/query` | **0.8 元/小时**（批式，无 partial） | 20 小时/半年 |
| 豆包实时语音大模型（端到端对话） | — | — | **按 token**（输入音频 80 元/百万、输出音频 300 元/百万） | 100 万 token |

**「豆包实时语音大模型」与前三条不是一回事** —— 那是端到端语音对话，**不做转写**。

**实测对照**：11.232 秒音频走 2.0 ≈ 0.0031 元；同段走 1.0 ≈ 0.014 元 —— **便宜 4.5 倍**。

**对账判据**：拿 `logid` + `audio_info.duration`（ms）算，**不要相信文档措辞**。

---

## 3. 请求头（p.1）

- **`X-Api-Key`** string **require** —— 控制台 > API Key 管理获取；
  **同时也支持旧版控制台鉴权方式**（`X-Api-App-Key` + `X-Api-Access-Key` 仍合法，不必改）
- **`X-Api-Resource-Id`** —— 见 §2，2.0 用 `volc.seedasr.sauc.duration`
- `X-Api-Request-Id` —— 推荐随机 UUID

---

## 4. 四个必调参数

| 参数 | 默认 | 要点 |
|---|---|---|
| **`enable_nonstream`** | false | **推荐开启**。实时返回结果的同时，**一句话结束后对该小段音频重新识别**，用**更稳定准确的结果替换**之前实时显示的文字（检测连续静音达 `end_window_size` 判定一句说完）。文档原文「可显著提高识别准确率」。**它让「低延迟」与「最终准确」兼得 —— 不要二选一。** |
| **`end_window_size`** | **800** ms | 范围 [300,5000]，推荐 [800,1000]。连续静音达该值判定一句结束。 |
| **`force_to_speech_time`** | 0 | **推荐 1000**（ms）。音频起始处强制按有声处理，**规避起始静音/弱音导致的过早判停**。 |
| **`result_type`** | `full` | 可选 `single` = 增量返回（**不返回之前分句结果**）。必须明确决策并写进注释。 |

### 其余相关参数

- `enable_accelerate_text` + `accelerate_score`（首字返回加速；代价是首字准确率可能下降）
- `vad_segment_duration`（默认 3000）—— **与 `end_window_size` 同时配置时本参数不生效**
- `enable_itn` / `enable_punc`（默认 **true**）
- `enable_ddc`（语义顺滑，删停顿词/语气词；默认 false）
- `show_utterances`（开启后返回分句信息）
- **2.0 独有**：`enable_poi_fc`（需 `enable_nonstream=true`）、图片 URL 上下文（需 nonstream，≤1 张 ≤500KB）

---

## 5. 响应侧（p.9-10）

```
code              int   0=识别成功
event             int   会话事件类型
is_last_package   bool  true = 识别结果已全部返回
payload_sequence  int   响应数据包序号
payload_size      int   payload 字节大小
payload_msg       object{ audio_info{ duration(ms) }, result[ additions{ log_id }, text,
                          utterances[{ definite(true=该分句不再变化), fixed_prefix_result(已确定的前缀结果),
                                       source, speaker_id, text, start_time, end_time }] ] }
```

**`definite` 与 `fixed_prefix_result`** 是「边出字」时判断「这段可以不改了」的判据 ——
实现要画增量文字时，这两个字段比盲猜稳定。

---

## 6. 落地到 gopi 的现状（2026-10-13）

- `sauc` 引擎（`internal/doubaosauc`）已实现，端点默认 `bigmodel_nostream`（**文件语义**，无 partial）
- **`bigmodel_async` 的常量已存在**（`internal/doubaosauc/protocol.go` 的 `EndpointBidirectional`），
  `Options.Paced` 开关也已存在，但**尚无常驻流式会话类型**
- `chat /voice` 的 live 通路**只匹配 `streaming`(v2)**（`voiceLiveEngineSelected`），所以 `sauc`
  落到 6 秒窗口循环（`voiceChatWindowSeconds=6` + `voiceChatWindowPauseSeconds=1`）
  —— 这就是「一句一句 + 每句停顿」体验的来源
- **任务 `01a13303-ab2d-7210-8053-e2ee2af0ce18`** 正在做：把 `sauc` 接上 `bigmodel_async`
  走实时流式（1 元/小时），取代窗口循环

### 配置侧（operator 一键切换）

```
~/.gopi/settings.json                            活动配置（streaming = 1.0 = 4.5 元/小时）
~/.gopi/settings.streaming-4.5union.bak.json     4.5 元线备份（sha256 781c1878…）
~/.gopi/settings.sauc-1union.candidate.json      候选（audio_engine=sauc，两块 param 均保留）

# 切到 sauc（1 元/小时）
cp ~/.gopi/settings.sauc-1union.candidate.json ~/.gopi/settings.json
# 回滚到 streaming（4.5 元/小时）
cp ~/.gopi/settings.streaming-4.5union.bak.json ~/.gopi/settings.json
```

**注意**：`sauc` 默认端点是 `bigmodel_nostream`（无 partial）—— 在 `01a13303` 落地之前直接切过去，
`chat /voice` 会退回窗口模式。**这是「切」与「不切」之间真正的取舍。**

---

## 7. 复现素材与命令

```bash
A=~/.gopi/sessions/01a121fc-eed0-744e-9975-99ad67069342/artifacts/voicectl-11s.wav

./bin/gopi tool voice_input --engine sauc "$A"
# 实测：28 chars, 11232 ms audio, 57 packets, first text 1.525s,
#       logid 202610131004436D1D546B2E48E3B75C2B
```

## Verification

- **Ran（真跑，非 `-h`）**：`./bin/gopi tool voice_input --engine sauc <voicectl-11s.wav>` →
  28 chars / 11232 ms / 57 packets / first text 1.525s / logid `202610131004436D1D546B2E48E3B75C2B`。
  该实测证明 `volc.seedasr.sauc.duration` **在本账号已开通**（此前 403 未开通）。
- **文档要点**：PDF 14 页读毕（`pdftotext -layout` → 658 行文本），§1/§3/§4/§5 均逐条取自原文。
- **计费表**：取自 `docs/doubao_audio_price.txt` 与
  `devdoc/doubao-asr-eval/evidence/pricing-*.md`，**按资源 ID 对齐而非按名字**。
