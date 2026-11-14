<!-- doc-class: normative -->
# SPEC: gopi voice_input — Phase 2/3 实现设计

**前置**: Phase 0/1 完成（`SPEC-voice-asr-engine-selection.md`）
**定案**: **Fun-ASR-Nano**（CER 1.58% / 71s / 技术词 100% —— 三项全胜 whisper）
**日期**: 2026-09-11

---

## 1. 定案依据（实测，非官方数字）

同一段 220 秒中文音频（含 RAG/prompt/agent 等技术词），同一 `asr_score.py`：

| 引擎 | CER | 技术词 | 耗时 | 实时比 | 模型 |
|---|---|---|---|---|---|
| whisper small | 5.14% | 69% ⚠️ | 209 s | 0.95× | 487 MB |
| whisper medium | 2.77% | 100% | 738 s | 3.4× | 1463 MB |
| **Fun-ASR-Nano** | **1.58%** | **100%** | **71 s** | **0.32×** | **910 MB** |

**whisper 的失败模式**（正是技术场景最怕的）：
```
硅基 → 规矩    驯服 → 兴奋    尔虞我诈 → 耳语我炸
RAG → Rug      AR/VR → ARV2   Prompt/Response → Promptor Spons
```
**Fun-ASR 全部正确**，仅残留 `肌理→机里` 1 处。

⚠️ **官方数字有水分**：README 首页称 AISHELL-1 CER 2.0035%，但其自身 benchmarks
显示会议场景宏平均 17.41% / 微平均 11.68%（最佳配置 9.51%）。
**教训：必须用自己的样本测。**

---

## 2. 关键参数：`--chunk 15`（必须）

```bash
llama-funasr-cli --enc encoder.gguf -m qwen3.gguf -a audio.mp3 --chunk 15
                                                              ↑ 必须
```

| | 无 --chunk | 有 --chunk 15 |
|---|---|---|
| 输出 | 一整页重复"零" ❌ | 正确转录 ✅ |
| 耗时 | 566 s | **71 s**（8×） |

**原因**：不分块时 220 秒音频被当作单个上下文块，超出模型训练分布。

---

## 3. Phase 2：server 侧

**目标**：接收音频 → 返回文本。

**技术前提（已全部验证）**：
```
✅ 静态库齐全：~/work/runtime/llama.cpp/build/_deps/llama-build/
   libllama.a + libggml.a + libggml-base.a + libggml-cpu.a
✅ cgo 可链：实测 llama.cpp from C 正常
✅ 完全静态：CGO_ENABLED=1 + -extldflags "-static" → statically linked (10 MB)
✅ AVX2 自动启用（llama.cpp 构建自动检测，不像 whisper 需要手工指定）
```

**集成形态（gopi 的 cgo 绑定）**：

```go
/*
#cgo CFLAGS: -I<repo>/llama.cpp/include -I<repo>/llama.cpp/ggml/include
#cgo LDFLAGS: -L<build>/_deps/llama-build/src \
             -L<build>/_deps/llama-build/ggml/src \
             -lllama -lggml -lggml-base -lggml-cpu -lstdc++ -lm -fopenmp
#include "llama.h"
*/
import "C"
```

**参考实现**：`fun-asr-nano/funasr-cli/funasr-cli.cpp`（282 行，自包含）
```
音频 → kaldi fbank（自带 C++ 实现，80-mel LFR 7/6）
     → SAN-M encoder + adaptor（ggml，funasr-encoder.gguf）
     → [prefix | audio embeds | suffix]
     → Qwen3-0.6B LLM（llama.cpp，embeds 经 llama_decode 注入）
     → 文本
```

**接口设计**（与 whisper-server 同构，但自研）：

```
gopi voice_input --role server --listen :7788
  POST /transcribe   multipart/form-data: audio=<file>
  → {"text": "...", "engine": "funasr", "chunk": 15, "elapsed_ms": N}
```

**为什么自研 server 而不复用官方**：官方 `server/funasr_gguf_server.py` 是
**Python + subprocess 调 CLI** —— 与"无 Python 运行时"哲学冲突。我们要的是
库级集成（cgo 直接调用）。

---

## 4. Phase 3：client 侧

**目标**：采集麦克风 → 推流 → server。

**采集实现**：`miniaudio.h`（4.1 MB header-only）
- whisper-mic 与 FunASR 的 `funasr-common/funasr_audio.h` 都用它
- **不需要 SDL2**（whisper-stream 用 SDL2，那是反面教材）
- 输出：`std::vector<float>` 16 kHz mono f32

**为什么 miniaudio 是关键**：它是 header-only，**可以直接编进单二进制**，
不需要任何外部动态库。

**传输**：ssh（已配免密/零新端口/与既有拓扑一致）
- 非实时需求使延迟抖动无关（16 kHz mono f32 = 64 KB/s）
- 30 秒录音 = 1.9 MB，ssh 传输 < 1 秒

**nick 指出的硬件难点**（我没有经验的地方）：
```
mic 设备选择：miniaudio 枚举 / ALSA vs PulseAudio / 权限 / 多设备选错
```
→ **Phase 0-2 完全绕开麦克风**，用 mp3 文件验证整条链路；
Phase 3 才碰硬件，先列可行性再动手。

---

## 5. Phase 4/5：与 gopi 接线

**nick 的设计（关键）**：
```
转写文本 → 填入用户输入框 → 用户修改 → 按发送
（不自动提交）
```

**为什么这是必需的**（实测支撑）：
```
Fun-ASR CER 1.58% → 每 100 字错 1.6 个
若自动发送：偶尔的错误进入对话
若用户过目：98.42% 的底子 + 人工纠正 = 可靠
```
即使用 Fun-ASR（比 whisper 好 3 倍），用户确认仍然值得保留 —— 因为技术
对话里一个错的技术词（如 RAG→Rug）比一个错字代价大得多。

---

## 6. 配置

```jsonc
// ~/.gopi/settings.json —— 与既有 settings symlink 机制一致
{
  "voice": {
    "role": "client",                    // client | server
    "engine": "funasr",                  // funasr | whisper（保留切换能力）
    "server_addr": "peerbox:7788",
    "funasr": {
      "encoder": "~/work/funasr-models/funasr-encoder-f16.gguf",
      "llm": "~/work/funasr-models/qwen3-0.6b-q8_0.gguf",
      "chunk_sec": 15,                   // 必须
      "language": "zh"
    },
    "capture_device": 0,                 // client 侧
    "max_seconds": 300
  }
}
```

**同一份二进制靠 role 决定行为** —— 与 gopi 在 HP/peerbox 上跑同一份二进制同构。

---

## 7. 分而治之的执行序

| Phase | 内容 | 输入 | 状态 |
|---|---|---|---|
| 0 | whisper 摸清 / cgo 静态验证 | — | ✅ |
| 1 | 引擎选型（三方对比） | mp3 | ✅ **Fun-ASR 定案** |
| 1.5 | q4km vs q8_0 模型对比 | mp3 | 🔄 |
| 2 | server 侧（收 PCM → FunASR → 文本） | mp3 模拟 | ⏳ |
| 3 | client 侧（miniaudio 采集） | 麦克风 | ⏳ |
| 4 | 会话接线（文本入输入框） | — | ⏳ |
| 5 | CLI/UI 按钮 | — | ⏳ |

**每阶段独立可验证**：
```
Phase 2: cat audio.mp3 | gopi voice_input --role server  → 正确文本
Phase 3: hosta 说话 → peerbox 出文本
Phase 4: 文本出现在输入框且可编辑
```

---

## 8. 参考

- 选型 SPEC: `devdoc/SPEC-voice-asr-engine-selection.md`
- 三方对比脚本: `scripts/asr_three_way_compare.sh`
- 评分工具: `scripts/asr_score.py`
- FunASR 官方 runtime: `~/work/runtime/llama.cpp`
- CLI 参考实现: `~/work/runtime/llama.cpp/fun-asr-nano/funasr-cli/funasr-cli.cpp`
- 模型: `~/work/funasr-models/`
- llama.cpp 源码: `~/work/llama.cpp`（pinned 803b7fc）
- nick 的文档: `~/work/claude/nick/asr_deepseek.txt`
