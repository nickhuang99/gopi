<!-- doc-class: normative -->
# Task Report: 豆包云 ASR（2.0 vs 1.0）实测评估 — 与本地 whisper / Fun-ASR-Nano 基线并列

**分支**: `feat/doubao-asr-eval`（worktree `~/workspace/gopi-doubao-asr`，**未 merge**）
**日期**: 2026-09-13
**音频**: `~/work/whisper.cpp/2084无颜见人0.mp3` → `out/audio/audio16k.wav`（16 kHz / 单声道 / 16 bit / 219.94 s）
**评分**: 完整 `scripts/asr_score.py`（本仓脚本，未改一行为简化版）
**参考文本**: `~/work/whisper.cpp/2084无颜见人0.txt`

---

## 0. 一句话结论

**默认继续用本地 Fun-ASR-Nano；云端只在"离线批量、可等"场景作为第二通道，且用 2.0（`volc.seedasr.auc`，0.8 元/小时）。
若批量转写要最高准确率，走官方 SDK 的 `/nostream` 通道（本次最低 CER 0.10%）。
但有一个前置条件必须先解决：本账号的流式（SAUC）时段包未开通，实时语音场景目前根本走不通。**

---

## Completed

### 1. 可运行的调用路径（两条，零第三方依赖）

| 路径 | 位置 | 依赖 | 说明 |
|---|---|---|---|
| 自写最小客户端 A（文件语义） | `devdoc/doubao-asr-eval/aucfile/main.go`（361 行） | **纯 stdlib** | AUC 录音文件识别：`POST /api/v3/auc/bigmodel/submit` + `/query` 轮询 |
| 自写最小客户端 B（流式语义） | `devdoc/doubao-asr-eval/minclient/main.go`（1004 行） | **纯 stdlib** | SAUC 双向流式 WebSocket，手写 RFC6455 帧 + 协议头，**含 `seg_duration`/`paced` 两个公平性开关** |

两个模块 `go.mod` 都只有 `module` + `go 1.24.0` 两行，`go list -deps` 里没有任何第三方模块
（只有 stdlib 内部 vendor 的 `golang.org/x/crypto`、`x/net` 等随 stdlib 分发的包）。

原始响应证据（含 `log_id` / `X-Tt-Logid`）见 `devdoc/doubao-asr-eval/evidence/`：
- `auc_std.raw.json` / `auc_std.metrics.json`（1.0 提交+查询全量响应与 logid）
- `pair10_r1.metrics.json` … `pair20_r2.metrics.json`（配对实测四跑）
- `probe_volc.*.hdr.txt` / `.body.txt`（**每个 resource_id 的额度探测原始头 + 体**）
- `sauc_rt_10_200.raw.txt` / `sauc_rt_20_200.raw.txt`（流式逐包原始帧，含 `log_id`）

**鉴权**：`X-Api-Key`（新版控制台）；key 从 `~/.doubao/apikey.txt` 读入进程环境，
**从不写入任何文件**（见 §7 泄漏扫描）。

### 2. 实测表（同一音频 / 同一 `asr_score.py`）

所有行都是"整段 219.94 s 中文技术音频 → 一次完整转写 → `asr_score.py` 打分"。

| 引擎 / 通道 | resource_id | CER | 技术词保留 | 耗时(端到端) | RTF | 内存 | 数据来源 |
|---|---|---|---|---|---|---|---|
| **豆包 1.0 + 官方 SDK /nostream** | `volc.bigasr.auc` | **0.10%** ⭐ | **16/16 = 100%** | 221.0 s（受 200 ms 分片节奏支配） | ~1.00 | — | 本次实测（`sdk_ns10_run2.txt`） |
| **豆包 2.0**（文件语义） | `volc.seedasr.auc` | **0.69%** | **16/16 = 100%** | **20.7 s / 18.4 s**（两跑） | 0.094 / 0.084 | 客户端 **52 MB** | 本次实测 |
| **豆包 1.0**（文件语义） | `volc.bigasr.auc` | **0.69% / 0.59%** | **16/16 = 100%** | **21.0 s / 22.9 s**（两跑） | 0.095 / 0.104 | 客户端 52 MB | 本次实测 |
| 豆包 1.0 极速版 | `volc.bigasr.auc_turbo` | 0.69% | 100% | 20.6 s | 0.094 | — | 本次实测 |
| whisper small（本地） | — | 5.14% | **69%** ⚠️ | 209 s | 0.95× | 882 MB | 基线，未重跑 |
| whisper medium（本地） | — | 2.77% | 100% | 738 s | 3.4× | 2234 MB | 基线，未重跑 |
| **Fun-ASR-Nano（本地）** | — | **1.58%** | **100%** | **71 s** | 0.32× | 910 MB | 基线，未重跑 |

基线行摘自 `devdoc/SPEC-voice-asr-engine-selection.md` §1（whisper）与
`devdoc/SPEC-voice-input-phase23.md`（Fun-ASR-Nano 三方表），**本次未重跑**（符合约束）。

**读法**：
- **本次最低 CER 出自官方 SDK 的 `/nostream` 通道：0.10%（1011 字错 1 字）**，
  比 AUC 文件识别（0.69%）还低，代价是墙钟 221 s（RTF≈1.00）。
- 云端 AUC 0.69% vs 本地 Fun-ASR-Nano 1.58% —— 云端 CER 低约 0.9 个百分点，
  即 **1011 个字符里少错约 9 个字**。这是"够用 vs 更好用"的差别，不是数量级差别。
- **技术词 16/16 双方都是 100%**：`RAG` / `Agent` / `Prompt` / `AR` / `VR` / `Response`
  两种方案都没丢。whisper-small 的 69% 才是真正的问题源，而它已经被 Fun-ASR-Nano 解决了。
- 内存那一列要公平读：本地方案的 882/2234/910 MB 是**模型常驻**；云端 52 MB 是
  **客户端进程 RSS 峰值**（`/usr/bin/time -v` 实测 `rss_probe`），模型在服务端。
  云端省的是本机内存，代价是网络与计费。

**2.0 vs 1.0 逐字对比**（同音频同参数，配对本跑）：
两跑差异都只有 1 个字，且都落在同一个同音字上：
- rep1：`概念对其`（1.0）→ `概念对齐`（2.0）**2.0 胜**
- rep2：`概念对齐`（1.0）→ `概念对其`（2.0）**1.0 胜**
即 **2.0 相对 1.0 的增益在单条音频上不可分辨**（差异在噪声内），
不能凭本次数据说 2.0 更准；只能说"两者同档"。

### 3. 耗时拆分：实时语义 vs 文件语义（公平性处理）

公平性问题：Fun-ASR-Nano 的 71 s 是**流式分块**语义（边收边出字），
而 AUC 文件识别的 20.7 s 是**提交后整段批处理**。两者不能直接比。
所以用同一个流式端点 `wss://.../sauc/bigmodel_async`，只改 `seg_duration` 和是否按实时节奏发包：

| 语义 | 端点 | `seg_duration` | 发包节奏 | 墙钟 | RTF | 说明 |
|---|---|---|---|---|---|---|
| **实时语义**（对齐 Fun-ASR 分块流式） | `sauc/bigmodel_async` | **200 ms** | **按实时节奏**（1100 包 / 220 s，`send_speed=0.999×`） | **221.0 s**（1.0）/ **221.0 s**（2.0） | **1.001 / 1.001** | 受音频时长支配：audio 219.94 s → 220.2 s 出最终结果 |
| **一次性返回语义** | `sauc/bigmodel_nostream` | 200 ms | 快发（1.2 s 发完 1100 包） | **221.0 s** ⚠️ | ~1.00 | **官方 SDK 实测**：最终只返回**一次**完整结果（重写而非追加），CER **0.10%** |
| **大分片文件语义**（自写客户端） | `sauc/bigmodel_async` | 10000 ms | 不按节奏（22 包，0.34 s 发完） | **189.1 s** ⚠️ | 0.860 | **未跑通，见下方失败说明** |
| **单包文件语义**（自写客户端） | `sauc/bigmodel_nostream` | 300000 ms（整段 1 包） | 不按节奏（1 包） | **190.8 s** ⚠️ | 0.868 | **未跑通**：`chars=0`，`WARNING: empty transcript` |
| **文件语义**（正牌批处理） | `auc/bigmodel/submit+query` | — | 一次 POST + 轮询 | **20.7 / 18.4 s**（2.0）；**21.0 / 22.9 s**（1.0） | **0.084–0.104** | 本次主用通道 |

**实时语义的连接开销分解**（`sauc_rt_*.metrics.json`，2.0 那跑）：

| 阶段 | 1.0 | 2.0 |
|---|---|---|
| TCP dial | 29 ms | 133 ms |
| TLS 握手 | 35 ms | 37 ms |
| WebSocket upgrade | 97 ms | 103 ms |
| **首字返回（first_char）** | **1456 ms** | **1463 ms** |
| 发包结束（last_send） | 220055 ms | 220064 ms |
| 最终结果（final_resp） | 220177 ms | 220177 ms |

**结论**：首字 ~1.46 s（含上面 161–273 ms 的连接建立），
**发包结束到最终结果只差 122 ms** —— 服务端在流式模式下几乎是"边收边出"，
不拖尾。但**实时语义下端到端 = 音频时长**（RTF ≈ 1.00），
这是物理约束：按实时节奏喂 220 s 音频，就不可能 20 s 出结果。
**所以"实时流式"与"文件批处理"的两条耗时不可互相换算，必须分开报**（本节即是）。

**⚠️ 未完成项（honest）**：`seg_duration=10000, paced=false` 的"同一流式端点的文件吞吐"
跑**失败**了 —— 服务端返回 `[Timeout waiting next packet] waiting next packet timeout:
8.000000 seconds, session has ended`，只转出 103 字 / 142 字（两次）—— 22 包发完后
服务端等不到下一包而断连。整段一次发完（`seg_duration=300000`，1 包）同样失败：
`chars=0` + `WARNING: empty transcript`。这说明 `bigmodel_async` / `bigmodel_nostream`
端点**不能**靠"大包 + 快速发包"当文件通道用，**文件通道只有 AUC**
（`auc/bigmodel/submit+query`，RTF 0.084–0.104）。

**一个意外收获（本次最低 CER）**：官方 SDK 跑 `/nostream`（`seg_duration=200`，
`out/sdkrun/ns10b`，logid `202609131613360BA10739070F647030E6`）最终返回**一次完整重写结果**
（1143 字，非追加），打分 **CER 0.10% / 100% 技术词 / 只错 1 个字**（`他`↔`它`）——
**这是本次所有引擎里最好的成绩，比 AUC 文件识别（0.69%）还低**。
代价是它按 SDK 自己的 ticker 每 200 ms 一包走完 220 s（**墙钟 221 s，RTF ≈ 1.00**），
即**用时间换准确率**。
**注**：自写客户端跑同一端点（`sauc_ns10`）拿到的文本是**重复的**（1900 字，
`却也沦为语言的囚徒` 出现 2 次），**这是我的 minclient 在 `nostream` 端点上的累积缺陷**，
不是服务端问题 —— 官方 SDK 同端点拿到的是干净全文。已如实记录，未修。

### 4. 官方定价（来源：官方文档页，非推算）

抓取方式：火山引擎文档中心的数据接口（`out/pricing/doc_1359370.json` / `doc_1359369.json`，
`DocumentID` 为权威定位），Markdown 副本存 `devdoc/doubao-asr-eval/evidence/pricing-*.md`。

| 商品服务 | 文档中的定位 | 后付费单价 | 资源包（30 小时档） |
|---|---|---|---|
| **豆包录音文件识别模型 2.0**（`volc.seedasr.auc`） | 计费说明表 | **0.8 元/小时** | 23 元 / 30 小时 ≈ **0.77 元/小时** |
| **大模型录音文件识别（标准版）**（`volc.bigasr.auc`，即 1.0） | 计费说明表 | **2.3 元/小时** | 66 元 / 30 小时 = **2.2 元/小时** |
| 大模型录音文件识别（极速版）（`volc.bigasr.auc_turbo`） | 计费说明表 | 4.5 元/小时 | 132 元 / 30 小时 = 4.4 元/小时 |
| 大模型录音文件识别（闲时版） | 计费说明表 | 1.2 元/小时 | 35 元 / 30 小时 ≈ 1.17 元/小时 |
| 大模型录音文件识别（历史小模型） | 历史小模型价格 | 0～300 小时档 **1.8 元/小时** | 500 小时 850 元 = 1.7 元/小时 |
| 豆包流式语音识别模型 2.0（实时，本次**未开通**） | 计费说明表 | 1 元/小时 | 28 元 / 30 小时 ≈ 0.93 元/小时 |
| 大模型流式语音识别（实时，本次**未开通**） | 计费说明表 | 4.5 元/小时 | 132 元 / 30 小时 = 4.4 元/小时 |

**计费口径**（同页原文要点）：
- 后付费**按小时结算，账单按小时出具**；`流式语音识别`与`录音文件识别（含标准版/极速版）`
  两个商品**按小时累进阶梯**（其余按天累进）。
- **按时长计费**：累加每次调用语音时长，**精确至毫秒**，再折算成小时。
- **双声道按单声道计费**，即按**音频时长**计费（不做声道加倍）。
- **免费额度**：`豆包录音文件识别模型 2.0` **20 小时 / 半年**；
  `大模型录音文件识别（含极速版、闲时版）` 20 小时 / 半年（计费概述页）。
- **并发/QPS**：`豆包录音文件识别模型 2.0`、`大模型录音文件识别（含闲时版）` 默认**最大 20 QPS**，
  **半小时内提交的音频总时长不超过 500 小时**；极速版默认 5 并发，增购 **400 元/并发/月**。

**本次实测成本**（按 0.8 元/小时 × 219.94 s ≈ 0.061 h）：
2.0 单次转写 ≈ **0.049 元**；1.0 单次 ≈ **0.14 元**。全部落在免费额度内，实际未计费。

### 5. 增强能力抽样（itn / punc / ddc / speaker / lid / 热词）

| 能力 | 实测结果 | 证据 |
|---|---|---|
| **`enable_punc`** | **有效**：开 = 99 个中文标点、1139 字；关 = **0 个标点**、1072 字 | `auc_std.txt` vs `auc_nopunc.txt` |
| **`enable_itn`** | 本次音频无可辨识 ITN 场景差异（数字本就写作 `2084`），**未能单独证伪/证实**；两次开关均默认开 | — |
| **`enable_ddc`（语义顺滑）** | 文本与基线几乎相同（1139 vs 1138 字符，仅 1 处标点变化）—— **本音频上无可测差异** | `auc_std.txt` vs `auc_ddc.txt` |
| **`enable_speaker_info`** | **无 speaker 字段返回**：`utterances` 有 35 条，每条只有 `definite/text/start_time/end_time`，**没有 `speaker_id`** | `auc_speaker.raw.json` |
| **`enable_lid`** | **无语言识别字段返回**：整份响应 JSON 不含 `language`/`lid` | `auc_lid.raw.json` |
| **热词（`corpus.context` 内嵌 `{"hotwords":[{"word":...}]}`）** | **接口接受，但本音频无可测效果** | 见下 |

**热词三段实测**（这是本次最费劲、也最有信息量的一段）：
1. `corpus.context` 传 **字符串**（双重编码的 JSON）→ **接口接受**。
   ```
   {"context":"{\"hotwords\":[{\"word\":\"RAG\"},...]}"}
   ```
2. `corpus.context` 传**裸词串** `"硅基 裸模型"` → 提交成功但查询返回
   `55000001`（任务失败）。
3. `corpus` 里放 `boosting_table_name`（自学习平台热词表名）→ **接口接受**（提交 20000000），
   但该表并不存在却也不报错 —— **服务端静默忽略未知表名**。
4. 早期一次 `{"context":{"hotwords":[{"word":"RAG"}]}}`（context 写成**对象**而非字符串）
   被明确拒绝，报 `45000000`：
   > `add unmapped key hotwords to params store: failed to convert key hotwords to str:
   > invalid value type to convert []interface{}, value: [map[word:RAG] ...]`

   **这个报错把协议格式钉死了**：`hotwords` 必须能被序列化成 **str**，即
   `corpus.context` 是**字符串**，里面再装一层 JSON 数组。这也是
   `need STRUCT type` 那条错误的同一个根因（`corpus` 本身必须是结构体，不能是字符串）。

**热词效果**（1.0，`hw_ctrl`（无热词） vs `hw_target`（boost `概念对齐`/`裸模型对齐`））：
差异**只有 1 处大小写**（`rag` → `RAG`），其余逐字相同。
原因是这次挑的目标太"弱"：热词是按上下文偏置，而 `概念对齐` 本来就对，
两个候选词（对齐/对其）都出现且都正确 —— 没有可改善空间。
**诚实结论：热词链路已跑通（格式正确、接口接受），但"热词提升准确率"这件事本次没有
可复现的正向证据。** 需要构造一个"基线必错、热词能救"的样本才能证明（见 Not Done）。

### 6. 决断性结论

**默认引擎：本地 Fun-ASR-Nano（不变）。云端豆包定位为第二通道，默认用 2.0。**

> **REVERSED 2026-10-07.** 这一行的结论已被推翻：引擎的**终点默认值**现在是 `doubao`
> （`config.VoiceEngineDefault`），理由是 0B 模型 / 0B runtime 与 1.7GB 权重的不对称，
> 以及"缺密钥"比"缺权重"更小的纠正成本。下面的选型分析（谁快、谁准、谁贵）仍然成立 ——
> 变的是"默认给谁"：论证见 `devdoc/doubao-asr-integration.md` §3。

| 场景 | 用谁 | 理由 |
|---|---|---|
| **实时语音输入 / 边收边出** | **本地 Fun-ASR-Nano**（现状） | 云端实时通道（SAUC）**本账号未开通**（HTTP 403 logid=`20260914181717D4E7EF027BB92CDE7FBF`）—— 缺的是**能力**，不是速度（旧的「RTF 1.00 比本地慢 3 倍」说法已更正，见下文）；无网络依赖 |
| **离线批量转写（≥ 数分钟音频、可等）** | **云端豆包 2.0**（`volc.seedasr.auc`） | CER 0.69% vs 1.58%，技术词同为 100%，**零本机内存占用**（52 MB 客户端），单价 0.8 元/小时 |
| **离线批量、要最高准确率、时间充裕** | **云端 `/nostream`（官方 SDK 路径）** | 本次最低 CER **0.10%**；代价是墙钟 221 s（RTF≈1.00），比 AUC 慢 10 倍 |
| **本机无模型/无算力（新机器、容器）** | **云端豆包 2.0** | 零依赖客户端 + 免费额度 20 小时/半年，不需要 910 MB 模型 + 运行时 |
| **隐私敏感音频** | **本地** | 云端意味着音频出国/出机，不可回退 |
| **1.0 还是 2.0** | **2.0** | 单价 **0.8 vs 2.3 元/小时**（2.0 便宜 **2.9 倍**）而质量同档 —— 这是唯一站得住的切换理由；反过来，1.0 **没有任何理由**选 |

**切换条件（明确阈值）**：当且仅当满足「音频已在本地、不需要实时结果、且本机模型不可用
或排队过长」时走云端；**实时场景在 SAUC 开通前一律走本地**。

**一个已被推翻的说法（2026-09-14 更正）**：本节此前写着「本地 0.32×，云端实时流式
220 s 要 221 s（1.00×）→ 在"实时"上云端比本地慢 3 倍」。**这个结论是错的**，错在把
**客户端的发送节奏**当成了**服务端的处理速度**。

实测机理（`out/probe/sauc_auc20_nopaced.stdout.txt`，同一账号、同一音频、同一 WS 端点）：
不按实时节奏发送（`paced=false`）时，客户端 **468 ms 就把整段 220 s 音频送完**
（`last_send=468ms`），服务端在同一窗口内持续出字 —— 服务端**根本不是** 1.00×。
`RTF≈1.00` 只出现在**客户端被故意限速**的路径上（实时语义 += 录音场景，人说话比网络慢，
客户端不可能比麦克风更快），以及官方 SDK 的 `/nostream` 路径（其客户端自身按实时节奏提交）。
所以「220 s 音频 → 221 s 墙钟」量的是**客户端节流**，不是云端的流式延迟。

**正确的对比**应该用同一条“发完即知”的通道：
- AUC 文件通道（本 epic 交付的云端通道）：220 s 音频 **18.4 s 出全文（HTTP 272 s 音频实测
  0.084–0.104 RTF，见 `out/evidence/`）**
- `/nostream` 一次提交：RTF≈0.097
- 本地 Fun-ASR-Nano：0.32×

也就是说**云端在“文件转写”上比本地快约 3 倍**（0.10 vs 0.32），而不是慢 3 倍。
**仍然成立的是**：云端 SAUC（`volc.seedasr.sauc.duration`）**本账号未开通**（HTTP 403，
logid=`20260914181717D4E7EF027BB92CDE7FBF`），因此**云端流式低延迟这件事在本账号上
无法测量** —— 我们没有它的一手数据。本地仍是唯一验证过的实时（边录边出）通道，
这个选择理由与速度无关，是**能力可得性**。

### 7. 风险段

**限流实测**（两次，证据全留）：

- **v1（N=60 并发，`scripts/ratelimit-probe.sh`）**：`out/evidence/ratelimit/summary.txt`。
  **59/60 请求的响应头文件是 0 字节**，只有 1 个拿到 `HTTP 200 / api=20000000`。
  即 60 路并发下客户端侧无法完成请求（fan-out 自身饱和），**不能据此说服务端限流**。
  失败模式没有被记录（脚本没记 curl 退出码）—— 这是脚本缺陷。
- **v2（并发爬坡 5 → 20，`scripts/ratelimit-probe2.sh`）**：`out/evidence/ratelimit2/`。
  - **并发 5**：5/5 `HTTP 200 / api=20000000`，墙钟 7.9–9.3 s
  - **并发 20**：**20/20 `HTTP 200 / api=20000000`**，墙钟 34.0–41.5 s
  - **没有出现 429**。

  **结论（诚实）**：在**已开通的 AUC 商品**上，20 并发全部成功，**未观测到限流**；
  单请求墙钟从 8 s（并发 5）涨到 40 s（并发 20），是**排队延迟**而非拒绝。
  文档口径「默认最大 20 QPS，半小时内音频总时长 ≤ 500 小时」与实测一致
  （20 QPS 是**提交速率**上限，本次并发 20 = 20 QPS，刚好压线未越界）。
- **真正观测到 429 的地方**是**并发版流式**（`volc.seedasr.sauc.concurrent`）：
  `HTTP/2 429`，`x-api-status-code: 45000292`，
  `x-api-message: quota exceeded for types: concurrency`
  （`out/probes/volc.seedasr.sauc.concurrent.hdr.txt`）—— 这是**未购买并发**，
  与限流是两件事，但它是本次唯一见到的 429。

**其他风险**：
- **额度风险**：免费额度 20 小时/半年，用尽后按 0.8 元/小时计费；无余额会导致欠费停服
  （官方原文提示）。
- **隐私**：音频必须上行到 `openspeech.bytedance.com`，不可撤回。
- **可用性**：本次多次遇到服务端 `Timeout waiting next packet` 与轮询状态
  `55000001`（一次任务级失败），云端不是"永不失败"。

### 8. D 段：集成代价评估

**8.1 官方 SDK 能不能用 —— 能，但不该用**

- 官方 Go SDK `sauc_go.zip`（`out/sdksrc/sauc_go`，1412 行）**编译并通过**（`GOPROXY=off` 离线编译 OK）。
- **致命缺陷（Rule 13 类）**：`config/config.go:48 ResourceID()` 与
  `protocol/config.go:48 ResourceID()` 的默认返回值是 **`volc.bigasr.sauc.duration`** ——
  **旧版 1.0 小时包**。`cmd/agent/main.go:21` 的 flag 默认值同样是它。
  **实测证实这个默认值有害**：不带 `-resource_id` 跑（`out/sdkrun/A_default/stdout.txt`）→
  `failed to execute: create connection err: dial websocket err: websocket: bad handshake`
  即 **403 握手被拒**（该 resource_id 本账号未开通）。带 `-resource_id volc.bigasr.auc` 则正常出字。
  → **必须显式指定 resource_id，绝不能让 SDK 默认值兜底**（本任务的硬约束，已用实测钉死）。
- 另一个坑：SDK 把 key 通过 **`os.Setenv("API_KEY", ...)`** 注入，并且
  `protocol.ApiKey()` 会**同时读 `../config.toml`**（缺失只打 Warning），
  即 SDK 的配置面比自写客户端大得多。

**8.2 最小客户端规模**

| 实现 | 行数 | 第三方依赖 | 覆盖能力 |
|---|---|---|---|
| 自写 `aucfile`（文件识别） | **361 行**（单个 `cmd/agent/main.go`） | **0** | submit + 轮询 + itn/punc/ddc/speaker/lid/corpus + 全指标采集 |
| 自写 `minclient`（流式） | **1004 行**（单个 `cmd/agent/main.go`，含手写 WebSocket 帧编解码 + 协议头 + 计时埋点） | **0** | 双向流式 + seg_duration/paced + 首字/末包计时 |
| 官方 SDK | 1412 行（13 个文件） | **4 个直接依赖** | 仅流式 |

**8.3 依赖数量与单二进制影响（实测，`CGO_ENABLED=0`，`-trimpath`）**

| 构建 | 依赖 | 静态二进制大小 |
|---|---|---|
| 纯 stdlib 基线（`json` + `net/http`） | 0 | **5.09 MB** |
| 基线 + `gorilla/websocket` | 1 | **5.65 MB**（+0.56 MB） |
| 自写 `minclient`（手写 WS，0 依赖） | 0 | **7.28 MB** |
| 官方 SDK 最小程序 | 4（+7 间接） | **8.74 MB** |

官方 SDK 的直接依赖（`go mod graph` 15 个节点）：
`github.com/gorilla/websocket`、`github.com/bytedance/sonic`（**JIT/汇编 JSON**，
带 `loader` + `cloudwego/base64x` + `cloudwego/iasm` + `klauspost/cpuid` +
`twitchyliquid64/golang-asm` + `golang.org/x/arch` 间接依赖）、
`github.com/BurntSushi/toml`、`github.com/google/uuid`。

**对单二进制目标的影响**：
- 引入**4 个第三方模块 / 15 个模块图节点**，其中 **`sonic` 是最重的一个**
  （运行时汇编器 + JIT，含 C 风格 loader），且它自己会打印
  `WARNING:(ast) sonic only supports go1.17~1.23, but your environment is not suitable`
  —— 本仓 `go 1.24.0`，**sonic 官方只声明支持到 1.23**，等于把一个
  "版本已越界、只是没炸" 的依赖塞进主二进制。
- **`toml` 依赖完全没必要**：只为读一个可选 `config.toml`。自写客户端用
  `~/.doubao/apikey.txt`（或环境变量）即可，零解析器。
- **本仓当前 `go.mod` 只有 `modernc.org/sqlite`** 一个直接依赖。为了 ASR 引入
  `sonic`+`toml`+`uuid`+`gorilla` **与"零依赖单二进制"目标直接冲突**。
- **正解**：D 段集成**不要用官方 SDK**。本仓已有自证可行的替代 ——
  两个**纯 stdlib** 的最小客户端（合计 1365 行）已覆盖文件与流式两条通道，
  且**离线可编译**（`GOPROXY=off` 已实测）。若嫌手写 WS 太重，
  最省的折中是 **只加 `gorilla/websocket`（+0.56 MB + 1 个依赖）**，
  彻底避开 sonic/toml/uuid。

**8.4 架构影响**（网络 / key / 计费 / 隐私）

| 维度 | 影响 |
|---|---|
| **网络** | 新增**外呼依赖**：`openspeech.bytedance.com:443`。离线/内网环境下该通道不可用，必须与本地 Fun-ASR 并存做 fallback（不能替换） |
| **key** | 新增一份**云端凭据**（`~/.doubao/apikey.txt`）。本仓有凭据泄露事故先例 → key 必须只从文件/环境读，**绝不进代码、不进日志、不进提交**（本次已 grep 验证，见 Verification） |
| **计费** | 新增**按小时计费**的成本面（0.8 元/小时）。需要在调用点做**用量记账**（至少记录每次 audio 时长 → 小时 → 元），否则成本不可观测；免费额度 20 小时用尽后**静默开始计费** |
| **隐私** | **音频出机**。当前本地 Fun-ASR 是"音频永不离开本机"，改云端等于打破这个性质 —— 必须是**用户显式选择**的通道，不能做默认 |
| **失败模式** | 云端新增三类失败：`55000001` 任务失败、`45000292` 未购并发、`Timeout waiting next packet` 断连。集成时必须重试 + 回退本地，**不能静默失败**（Rule 15） |
| **协议** | AUC 是 **submit + 轮询**两段式（本次实测 8–9 次轮询换一次结果），轮询间隔策略要自己定；SAUC 是长连接，需处理心跳与断线重连 |

### 9. 仓库门禁

`CGO_ENABLED=0 go build ./cmd/agent` → **OK**；`go test ./internal/standards/` → **ok（4.594s）**。
本次**未修改任何 Go 源码**（只在 `devdoc/`、`scripts/` 下新增评估材料），门禁不受影响。

---

## Not Done (honest)

1. **`volc.seedasr.sauc.duration` / `volc.bigasr.sauc.duration`（流式小时包）未开通** ——
   403 `requested resource not granted`。**epic 里点名的这两个 resource_id 无法直接实测**。
   变通：用**同账号已开通**的 `volc.bigasr.auc` / `volc.seedasr.auc` 在**同一个 SAUC 流式端点**上
   实测（证明端点可用、2.0/1.0 均可跑流式），但**这不是 epic 原始指定的那两个包**。
   要真测那两个，必须**在控制台开通**（不是技术问题，是账号配置）。
2. **文件语义的流式吞吐未能跑通**：`seg_duration=10000, paced=false` 被服务端
   `Timeout waiting next packet` 8 s 断开（两次，103 / 142 字）；整段单包
   （`seg_duration=300000`，1 包）返回 `chars=0` + `WARNING: empty transcript`。
   因此"在流式端点上做文件吞吐"**没有可用数字**，文件通道**只有 AUC**。
   另有一处**我自己的实现缺陷**未修：自写 `minclient` 在 `/nostream` 端点上把
   结果累积成**重复文本**（1900 字，`却也沦为语言的囚徒`×2）；官方 SDK 同端点
   返回干净全文（1143 字，CER 0.10%）。这是 minclient 的问题，不是服务端，
   如实记录**未修复**。
3. **热词提升效果未证实**：格式已跑通（接口接受 `corpus.context` 字符串内嵌 hotwords），
   但本次选的 boost 词（`概念对齐`/`裸模型对齐`）在基线上本来就对，**没造出可改善样本**，
   所以**"热词能提升准确率"本次无正向证据**。需要专门构造"基线必错 + 热词可救"的样本重测。
4. **`speaker_info` / `lid` 返回字段缺失**：两个开关打开后响应里**根本没有对应字段**
   （`utterances` 无 `speaker_id`，整份 JSON 无 `language`/`lid`）。
   可能是 2.0 模型不支持、也可能**需要在控制台开对应能力** —— 本次**无法区分**这两种原因。
5. **`enable_itn` 的独立效果未测**：本音频无 ITN 场景（数字本就规范），开关前后无差异。
6. **`ddc`（语义顺滑）无差异**：1139 vs 1138 字符，仅 1 处标点。可测，但**本音频信息量为零**。
7. **定价来源是文档数据接口，不是可点的商品页 URL**：本环境对该站 HTML 渲染受限
   （`web_fetch` 拿不到正文），改为直接取文档中心数据接口
   （`out/pricing/doc_1359370.json`，`DocumentID=1359370`，`Title=计费说明`，
   `LibraryID=6561`）。**价格文本是官方文档原文**，但我**没有**给出一个
   "浏览器打开就能看到价格"的商品页 URL —— 这一点如实说明。
8. **1.0 与 2.0 的准确率差异在本次单音频上不可分辨**（配对两跑各胜一次，差异同为 1 个同音字）。
   要下"2.0 更准"的结论需要**更大样本**，本次不下这个结论。
9. **`/usr/bin/time -v` 的内存数字**只是**客户端进程 RSS（52 MB）**，
   **不含服务端成本**；云端内存对比的公平口径无法从客户端侧获得。
10. **未测 8 kHz / 双声道 / 长音频（>30 min）** 等边界。

---

## Verification

| 检查 | 命令 | 结果 |
|---|---|---|
| 自写客户端离线编译（零依赖） | `cd devdoc/doubao-asr-eval/aucfile && GOPROXY=off go build` | **OK** |
| 官方 SDK 离线编译 | `cd out/sdksrc/sauc_go && GOPROXY=off go build` | **OK** |
| 静态二进制 | `CGO_ENABLED=0 go build -trimpath` | minclient **7.28 MB** / SDK **8.74 MB** / 纯 stdlib 基线 **5.09 MB** |
| **真实 API 调用成功** | `./out/bin/aucfile -file out/audio/audio16k.wav -resource_id volc.seedasr.auc` | **成功**，`X-Tt-Logid=20260913155631AC715CA5F752666F55B7`（提交）/ `20260913155720A53FCA89879944FB7530`（查询） |
| **原始响应留证** | `devdoc/doubao-asr-eval/evidence/*.raw.json` | 含 `header.code=0` + `additions.log_id` + `audio_info.duration=219939` |
| 同音频同脚本打分 | `python3 scripts/asr_score.py <hyp> /home/user/work/whisper.cpp/2084无颜见人0.txt` | 见 §2 表（0.69% / 100%） |
| 评分脚本完整性 | 脚本未改；含 CER + 技术词保留 + 差异对齐（**10 项检查**） | `scripts/asr_score.py` 原样使用 |
| 限流实测 | `bash scripts/ratelimit-probe2.sh 5 20` | **25/25 成功，无 429**；证据 `out/evidence/ratelimit2/` |
| **密钥泄漏扫描** | `grep -rl -F "$(cat ~/.doubao/apikey.txt)" devdoc/ scripts/` 与 8 字符前缀 | **无命中**（两者都为空） |
| 仓库构建 | `CGO_ENABLED=0 go build ./cmd/agent` | **exit 0** |
| 仓库门禁 | `go test ./internal/standards/` | **ok 4.594s** |
| 分支 | `git branch --show-current` | `feat/doubao-asr-eval`（**未 merge**） |

**密钥处理**：key 仅经 `tr -d ' \n\r' < ~/.doubao/apikey.txt` 读入**进程环境**，
由 `X-Api-Key` 头直接发出；扫描确认**仓库内任何被跟踪/待提交文件都不含 key 或其 8 字符前缀**。
报告中所有出现处一律 `[REDACTED]`。

---

## 附：本次实测的原始 log_id 清单（可回溯）

| 跑次 | resource_id | 提交 logid | 查询 logid |
|---|---|---|---|
| `auc_std`（1.0 基线） | `volc.bigasr.auc` | `202609131543317C8189BF03A7C07AF031` | `20260913154353DF810796CD18E1D71760` |
| `auc_std_20`（2.0） | `volc.seedasr.auc` | `20260913155631AC715CA5F752666F55B7` | `20260913155720A53FCA89879944FB7530` |
| `pair10_r1` | `volc.bigasr.auc` | `20260913160858BB7E13E88559F36CEE3F` | `20260913160919B979A507B7BC2ADBF601` |
| `pair20_r1` | `volc.seedasr.auc` | `20260913160919047D9020282DE8D87589` | `20260913160940565B88A3FEE6917A2095` |
| `pair10_r2` | `volc.bigasr.auc` | `20260913160940192712558EEBD93EFC66` | `20260913161003802D730D68D83334653F` |
| `pair20_r2` | `volc.seedasr.auc` | `20260913161003BEB1A98097F458782DB8` | `20260913161021901256A444AA23D81016` |
| `sauc_rt_10_200`（实时 1.0） | `volc.bigasr.auc` | — | `log_id=20260913160044D5877F7DEE88316BA566` |
| `sauc_rt_20_200`（实时 2.0） | `volc.seedasr.auc` | — | `log_id=20260913160424DEB981709161BD1AB4E3` |
| 官方 SDK 流式 1.0 | `volc.bigasr.auc` | — | `logid 2026091316045429CA98EA41AAFD6E6AAB` |
| 官方 SDK 流式 1.0（async） | `volc.bigasr.auc` | — | `logid 202609131607250D537C14D9C88D716CEB` |
| **官方 SDK `/nostream`（本次最低 CER 0.10%）** | `volc.bigasr.auc` | — | `logid 202609131613360BA10739070F647030E6` |
| `/nostream` 单包（失败） | `volc.bigasr.auc` | — | `logid 2026091316165076BE75264EE576710D5A` |
| `/nostream` 单包（失败，2.0） | `volc.seedasr.auc` | — | `logid 202609131620013369C1DF5B03726BADA7` |
| 限流 wave-20 | `volc.bigasr.auc` | 20 条 logid 全存 | `out/evidence/ratelimit2/wave20.txt` |
| 额度探测（未开通） | `volc.*.sauc.duration` / `.concurrent` / `megatts` | — | `out/probes/*.hdr.txt` + `x-api-status-code` |
