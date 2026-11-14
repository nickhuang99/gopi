<!-- doc-class: historical -->
# SPEC: gopi voice_input — whisper.cpp 集成

**状态**: Phase 0 验证完成，Phase 1 设计定稿
**日期**: 2026-09-11
**作者**: PA (基于 nick 的架构设计 + whisper.cpp 代码实读)

---

## 1. 目标（nick 的原话拆解）

> gopi will work as client and server with help of config file... it is the same
> binary, just like gopi running in HP and peerbox. I will run it in my hosta
> laptop with config, it points server as peerbox.

**三段式需求**：

1. **同一份 gopi 二进制**，用 config 决定角色（client / server）—— 与既有的
   gopi 多节点部署完全同构，不是新机制
2. **client 侧**（hosta 笔记本）：配置麦克风、采集、把音频推给 server
3. **server 侧**（peerbox）：跑 whisper.cpp，把音频转成文本

**文本出口（关键设计）**：
> the text is in user input and waiting for user to modify and press send button.
> this will solve the accuracy of ASR.

→ 转写文本**填入输入框**，用户可修改后按发送。**不是**自动提交。
这直接化解了 ASR 的准确率问题（见 §5 实测数据）。

**非实时**：
> considering the local ASR is not so accurate... the real time voice input is
> not useful... it is a convenient of voice to text trick, no dialog.

→ 录一段 → 转一次。不做流式滑窗。这既是需求（准确率），也是硬件约束
（持续推理对 peerbox 双核太重）。

---

## 2. Phase 0 验证结果（已完成）

### 2.1 whisper.cpp 现状

```
~/work/whisper.cpp/                     git clone, 已构建
├── build/bin/
│   ├── whisper-cli                     文件转录（命令行）
│   ├── whisper-server                  HTTP ASR 服务（POST /inference, 收 WAV）
│   ├── whisper-mic                     ★ 采集+转录（miniaudio，非 SDL2）
│   ├── whisper-stream                  SDL2 实时流（不用：需外部动态库）
│   └── whisper-bench / -quantize / -vad-speech-segments
├── build-static/                       ★ 静态库已就绪
│   ├── src/libwhisper.a
│   └── ggml/src/{libggml,libggml-base,libggml-cpu}.a
├── include/whisper.h                   741 行，公开 API
├── bindings/go/                        官方 Go 绑定（cgo）
└── models/
    ├── for-tests-*.bin                 ★ CI 测试夹具，非可用模型（见 §2.3）
    ├── ggml-small.bin                  487 MB（已下载，实测）
    └── ggml-medium.bin                 ~1.5 GB（下载中）
```

### 2.2 拆分点（读 `examples/stream/stream.cpp` 精确定位）

```cpp
// ── client 侧：只做采集 ──
audio_async audio(params.length_ms);              // L147
audio.get(params.step_ms, pcmf32_new);            // L260  取 PCM

// ── server 侧：只做推理 ──
whisper_full(ctx, wparams, pcmf32.data(), size);  // L341
```

**接缝 = `std::vector<float>` 的原始 PCM**（`audio_buffer` 在
`whisper-mic.cpp:83` 声明，`data_callback` 累积）。

**这决定了协议可以极简**：两端都能序列化 float 数组，中间加个长度前缀即可。

### 2.3 `for-tests-*.bin` 是假模型（验证过程记录）

nick 看到 `models/` 里有一堆 `.bin` 以为模型齐了。实际它们是 CI 夹具：

| 证据 | 内容 |
|---|---|
| 二进制 | small/base/medium/large 四个文件**大小完全相同**（51865 字节），只末尾一字节不同（0x03 vs 0x02） |
| 行为 | 跑 76 秒 / 385% CPU / 305MB 内存（确实在推理），但对 1.7MB 中文音频**输出 0 字节** |

**教训**：`magic == "ggml"` 格式正确 ≠ 内容是训练好的权重。

### 2.4 HF 直连被墙 → hf-mirror.com

```
huggingface.co            直连超时（curl 130s timeout）
hf-mirror.com             HTTP/2 200 ✅
www.modelscope.cn         HTTP/1.1 302 ✅
```

**大陆环境下载 whisper 模型用 `https://hf-mirror.com/ggerganov/whisper.cpp/resolve/main/ggml-<size>.bin`**。

### 2.5 ★ 决定性验证：cgo + 完全静态 = 可行

**这是整个方案的架构前提**，因为：

```
gopi 铁律（Makefile L2）:  "Static linking is required: CGO_ENABLED=0 produces
                            a zero-dependency binary."
现状:                      bin/gopi = statically linked, "not a dynamic executable"

whisper 官方 Go 绑定:       cgo（#cgo LDFLAGS: -lwhisper -lggml -lggml-base
                                 -lggml-cpu -lstdc++ -lm -fopenmp）
                          → 需要 CGO_ENABLED=1 → 看似冲突
```

**实测结论：可以共存**（实验代码在 `~/workspace/cgo-static-test/`）：

```bash
CGO_ENABLED=1 go build \
  -ldflags '-linkmode external -extldflags "-static"' \
  -o gopi ./cmd/agent/
```

| 测试 | 结果 |
|---|---|
| t1（动态链接，仅验证能链上） | 4.0 MB，`whisper version from C: 1.8.3` ✅ |
| **t2（完全静态）** | **6.6 MB，`statically linked`，`not a dynamic executable`** ✅ |

**体积代价仅 +2.6 MB**（在 16MB 的 gopi 上完全可接受）。

⚠️ 已知无害警告：`libggml.a` 引用了 `dlopen`（backend-dl 模块），静态链接时
ld 会警告"requires at runtime the shared libraries from the glibc version used
for linking"。`GGML_BACKEND_DL=OFF`（已确认），该代码路径不会被调用。

---

## 3. 架构设计

### 3.1 角色与配置

```jsonc
// ~/.gopi/settings.json  —— 与既有的 settings symlink 机制一致
{
  "voice": {
    "role": "client",            // client | server
    "server_addr": "peerbox:7788", // client 用：推流目标
    "listen_addr": ":7788",      // server 用：监听地址
    "model": "~/work/whisper.cpp/models/ggml-small.bin",  // server 用
    "language": "zh",
    "capture_device": 0,         // client 用：mic 设备 id（可选）
    "max_seconds": 300           // 单段录制上限
  }
}
```

**同一份二进制**，`role` 决定行为 —— 与 gopi 多节点部署（HP/peerbox）同构。

### 3.2 拆分

```
┌─ CLIENT (hosta) ─────────────┐        ┌─ SERVER (peerbox) ─────────────┐
│ gopi voice_input             │        │ gopi voice_input             │
│  · miniaudio 采集            │  PCM   │  · 收流                      │
│  · 累积 vector<float>        │ ─────→ │  · whisper_full              │
│  · Enter 开始/停止           │        │  · 文本填入输入框（待确认）  │
└──────────────────────────────┘        └──────────────────────────────┘
```

**client 侧不链 whisper** —— 它只需要 miniaudio（header-only，编进去）。
**server 侧不链 miniaudio** —— 它只需要 whisper。

→ **可以用 build tag 分离**（`//go:build voice_client` / `voice_server`），
保证 client 侧二进制不含 whisper（体积小、适合笔记本/树莓派）。

### 3.3 传输层：ssh（决策 + 理由）

nick 问"is ssh better? since we already has it setup"，**PA 决策：是**。

| 方案 | 优点 | 缺点 |
|---|---|---|
| **ssh 管道** ✅ | 已配好免密、零新端口、零认证设计、与既有拓扑一致 | 略多开销（音频码率低，可忽略） |
| 裸 TCP | 略低延迟 | 需自己解决寻址/认证/防火墙 |
| HTTP | 生态好 | 需起服务、需端口、与"同一二进制"稍远 |

**关键：非实时需求使 ssh 的延迟抖动完全无关**（16kHz 单声道 float32 = 64KB/s，
一段 30 秒录音 = 1.9MB，ssh 传输 <1 秒）。

实现形态：`gopi voice_input` 在 client 模式把 PCM 写到 stdout，
用既有 ssh 通道送出；或 client 直接 exec `ssh peerbox gopi voice_input --sink`。

### 3.4 文本出口（与 gopi 会话的接线）

**nick 的设计**：文本填入输入框，用户确认后发送。

```
server 收到转写文本
  → 写入一个「待确认输入」位置
  → UI/CLI 显示在输入框（用户可编辑）
  → 用户按 Enter → 走正常的 prompt 提交路径
```

**这是与现有 UI 的接缝**，Phase 3 才做（需要 CLI/UI 配合）。

---

## 4. 分而治之：任务切分

按 nick 要求"divide and conquer"，且**Phase 0/1 完全绕开麦克风**
（硬件是我不熟悉的领域，先用文件输入验证整条链路）：

| Phase | 内容 | 输入源 | 依赖 |
|---|---|---|---|
| **0 ✅** | whisper 现状摸清、模型下载、cgo 静态链接验证、small 实测 | mp3 文件 | 无 |
| **1** | 模型选型（small vs medium，CER/耗时/内存对比） | mp3 文件 | 0 |
| **2** | server 侧：`voice_input` server 模式（收 PCM → whisper → 文本） | mp3 文件（模拟 PCM） | 1 |
| **3** | client 侧：miniaudio 采集 + 推流（**首次碰硬件**） | 麦克风 | 2 |
| **4** | 与 gopi 会话接线（文本入输入框，走 prompt.submit） | — | 3 |
| **5** | CLI/UI 按钮 | — | 4 |

**每阶段可独立验证**：

```
Phase 2 验证: echo pcm | gopi voice_input --role server  → 正确文本
Phase 3 验证: hosta 说话 → peerbox 出文本
Phase 4 验证: 文本出现在输入框且可编辑
```

---

## 5. 实测数据（Phase 0/1）

### 5.1 small 模型（487MB，peerbox i7-5500U 双核）

```
样本:   2084无颜见人0.mp3（1.7 MB，中文有声书，含 AI/agent 技术词汇）
参考:   人工转写 3192 字节
输出:   2965 字节

CER (字符错误率)   5.14%
准确率             94.86%
技术词汇保留率     11/16 = 69%

MISSING   'ar' 'vr' 'prompt' 'response'
SPURIOUS  'arv2' 'rug' 'promptor' 'spons'
          RAG → Rug / AR、VR → ARV2 / Prompt、Response → Promptor Spons
```

**错误模式 = 同音字替换**（中文 ASR 特征失败）：

```
驯服 → 兴奋      硅基 → 规矩      尔虞我诈 → 耳语我炸
木讷 → 木嫩      悦耳 → 月儿      嵌入 → 欠入
续章 → 叙章      美颜 → 美言      脏话 → 脏化
```

**这是 nick 说的"技术词汇准确率更低"的量化证据。**

### 5.2 为什么"用户确认"设计是必需的

```
CER 5.14% → 每 100 字错 5 个
若自动发送: 技术对话会被同音字错误污染（RAG→Rug 会误导 LLM）
若用户过目: 94.86% 的底子 + 人工纠正 = 可用
```

**nick 的设计直接化解了模型的能力上限。**

### 5.3 medium 对比（待补）

medium 下载后跑同一段音频，补：CER / 技术词保留率 / 耗时 / 内存。

**决策标准**：
- 若 medium CER 显著更低（如 <3%）且技术词保留 >85% → 用 medium
- 若提升有限而耗时翻倍 → 用 small（peerbox 双核是硬约束）

---

## 6. 风险与未知

| 风险 | 状态 | 缓解 |
|---|---|---|
| cgo 破坏静态链接 | ✅ 已验证可解（§2.5） | 静态 ldflags |
| 模型假（for-tests） | ✅ 已识别并下载真模型 | — |
| HF 直连被墙 | ✅ 已解决（hf-mirror） | — |
| **麦克风设备选择** | ⚠️ **未验证** | nick 提醒"没有硬件经验，别低估"；Phase 3 才碰，先列出可行性 |
| 二进制体积 | ✅ 仅 +2.6MB | — |
| 双核推理太慢 | ⚠️ 待测（medium） | small 已实测可用；必要时降采样/量化 |
| 与 UI 的接缝 | ⚠️ 未设计 | Phase 4 |

---

## 7. 参考

- whisper.cpp: `~/work/whisper.cpp`（git clone，含 build-static）
- 官方 Go 绑定: `bindings/go/{whisper.go,params.go,pkg/whisper}`
- API 头: `include/whisper.h`（741 行）
- 采集示例: `examples/mic/whisper-mic.cpp`（miniaudio，非 SDL2）
- 推理示例: `examples/cli/cli.cpp` / `examples/stream/stream.cpp`
- cgo 静态验证: `~/workspace/cgo-static-test/`
- 评估脚本: `~/work/GoPi/scripts/asr_score.py`
