<!-- doc-class: historical -->
# SPEC: gopi voice_input — ASR 引擎选型与集成（FunASR vs whisper）

**状态**: Phase 0/1 完成（whisper 全测 + FunASR 工程打通，模型下载中）
**日期**: 2026-09-11
**触发**: nick 提供 `~/work/claude/nick/asr_deepseek.txt`，指出 whisper 中文不是最优解

---

## 1. 为什么换方向（nick 的文档 + 我的实测）

**文档核心论点**：
> 中文场景直接使用通用多语种模型（如 Whisper）效果往往不理想。Whisper-large-v3
> 会议场景 CER 18.87%，而专注中文的模型可压到 5% 以内。

**我的实测（whisper.cpp，同一段 220 秒中文音频含技术词汇）**：

| | whisper small | whisper medium |
|---|---|---|
| CER | 5.14% | 2.77% |
| 技术词保留 | **69%** ⚠️ | 100% |
| 错误样例 | `RAG→Rug`、`AR/VR→ARV2`、`Prompt/Response→Promptor Spons` | 无 |
| AVX2 耗时 | 209 s（0.95× 实时） | 738 s（3.4× 实时） |
| 内存 | 882 MB | 2234 MB |

**结论**：whisper 的中英文混杂+技术词场景确实不佳（small 丢 31% 技术词）；
medium 准确但慢 3.4×。**有换引擎的价值。**

---

## 2. 关键发现：whisper 慢 3 倍是构建配置造成的

**这不是 whisper 的问题，是我的构建问题**：

```
CPU 支持:  avx2 ✓  fma ✓  sse4_2 ✓
原构建:    GGML_AVX2=OFF  GGML_FMA=OFF  GGML_AVX=OFF   ← 全关！
```

用 `cmake -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_AVX=ON -DGGML_NATIVE=ON` 重建后：

| | 无 AVX2 | 有 AVX2 | 提升 |
|---|---|---|---|
| small | 622 s (2.8×) | **209 s (0.95×)** | **3.0×** |
| medium | 1085 s (4.9×) | 738 s (3.4×) | 1.5× |

**输出字节完全一致**（AVX2 是纯加速，无数值差异）。

⚠️ **教训**：差点用被错误配置拖慢 3 倍的机器做出"whisper 不可行"的错误结论。

---

## 3. FunASR 方案（官方 = modelscope/FunASR/runtime/llama.cpp）

**官方定位**（README 原话）：
> This is to FunASR what **whisper.cpp is to Whisper**: it lets the models run
> where there is **no GPU and no Python**（laptops, phones, edge boxes）

**构建**：`cmake -B build -DCMAKE_BUILD_TYPE=Release` → "fetches pinned
llama.cpp; **static, self-contained**"

### 3.1 三种模型（同一套 GGML 后端）

| 模型 | 架构 | 特点 |
|---|---|---|
| **Fun-ASR-Nano** | SAN-M encoder + adaptor + Qwen3-0.6B LLM | 精度最高，AISHELL-1 **CER 2.0035%** |
| **SenseVoiceSmall** | SAN-M encoder + CTC | q8 仅 254MB |
| **Paraformer** | 非自回归 | CPU ~22× 实时（最快） |

### 3.2 模型体积（官方 GGUF）

```
Fun-ASR-Nano:  funasr-encoder-f16.gguf  448 MB
             + qwen3-0.6b-q4km.gguf     462 MB
             = 910 MB

对比 whisper-medium 1463 MB → 小 38%，而 CER 2.00% vs 2.77%
```

### 3.3 已完成的工程验证

```
✅ 构建成功（6 个二进制）
   llama-funasr-cli        5.9 MB   ← Fun-ASR-Nano
   llama-funasr-paraformer 2.5 MB
   llama-funasr-sensevoice 2.5 MB
   llama-funasr-vad / encoder / embd

✅ 直接吃 MP3（funasr-common/funasr_audio.h 用 miniaudio 解码）
   注释：load any audio file (WAV/MP3/FLAC/OGG...) as 16 kHz mono f32

✅ 集成接口清晰 —— fun-asr-nano/funasr-cli/funasr-cli.cpp 仅 282 行
   链路：wav/mp3 → kaldi fbank（自带实现）→ SAN-M encoder+adaptor（ggml）
        → [prefix | audio embeds | suffix] → Qwen3 LLM（llama.cpp）→ 文本
   依赖：ggml.h / llama.h / funasr_audio.h / funasr_vad.h / funasr_srt.h

✅ 构建用的本地源码技巧
   -DFETCHCONTENT_SOURCE_DIR_LLAMA=/path/to/llama.cpp
   （绕开 cmake 的慢 fetch）
```

### 3.4 环境障碍与解法（可复用）

| 障碍 | 解法 |
|---|---|
| GitHub 直连 TLS 错误/超时（clone 卡 20+ 分钟） | **ghfast.top / gh-proxy.com 镜像**：`https://ghfast.top/https://github.com/...` |
| search-bot 无法编译（1vCPU/957MB 且无 cmake） | 回退 peerbox 本地构建 |
| HF 直连被墙 | **hf-mirror.com** |
| peerbox←search-bot 带宽 14.2 kB/s | 见 §5 未解 |

---

## 4. 集成设计（与 whisper 方案同构）

**已验证的架构前提**（Phase 0 用 whisper 验证，FunASR 复用同一套）：

```bash
CGO_ENABLED=1 go build -ldflags '-linkmode external -extldflags "-static"'
→ statically linked, "not a dynamic executable"
→ 6.6 MB（whisper）；FunASR 会是类似量级
```

**拆分点**（两者一致）：

```
client 侧：miniaudio 采集 → vector<float> PCM
   （whisper 用 examples/mic/whisper-mic.cpp；FunASR 用 miniaudio 同款）
server 侧：
   whisper: whisper_full(ctx, wparams, pcmf32.data(), size)
   FunASR:  整个 funasr-cli.cpp 的链路（fbank → encoder → llama_decode）
```

**角色与配置**（与 gopi 多节点部署同构）：

```jsonc
// ~/.gopi/settings.json
{
  "voice": {
    "role": "client",              // client | server
    "engine": "funasr",            // funasr | whisper
    "server_addr": "peerbox:7788",
    "model_dir": "~/work/funasr-models",
    "language": "zh",
    "capture_device": 0
  }
}
```

**文本出口**（nick 的设计，关键）：
```
转写文本 → 填入用户输入框 → 用户修改 → 按发送
（不自动提交；5% CER 靠人工纠正兜住）
```

---

## 5. 当前阻塞：模型下载带宽

**四条路径实测**：

| 路径 | 速度 | 910MB 需要 |
|---|---|---|
| peerbox ← search-bot（ssh/scp/rsync） | 14.2 kB/s | 18 小时 |
| peerbox ← hf-mirror | 40-130 kB/s | 2-6 小时 ← 当前使用 |
| peerbox ← modelscope | 899 B/s | 不可用 |
| search-bot 本机磁盘 | 340 MB/s | （磁盘无问题） |

**诊断**：瓶颈是 peerbox 的国际出口带宽（当前时段劣化），非配置问题。
search-bot 上的模型已下齐（910MB 在 `/tmp/funasr-models/`）。

**进度**：encoder-f16 149/448 MB（进行中）

---

## 6. 待办

| 项 | 状态 |
|---|---|
| whisper small/medium 全测（CER/技术词/耗时/内存） | ✅ |
| AVX2 构建优化（3× 提速） | ✅ |
| FunASR runtime 构建 | ✅ |
| FunASR 模型下载 | 🔄 149/448 MB |
| **用同一段 2084 音频测 Fun-ASR-Nano** | ⏳ 等模型 |
| 三方对比表（whisper small / medium / Fun-ASR-Nano） | ⏳ |
| 集成 SPEC（cgo 静态链接 + client/server 拆分） | ✅ 本文档 §4 |
| Phase 2：server 侧实现（收 PCM → ASR → 文本） | ⏳ |
| Phase 3：client 侧（miniaudio 采集） | ⏳ |

---

## 7. 参考

- whisper.cpp: `~/work/whisper.cpp`（build/ 原构建，build-avx2/ 优化构建）
- FunASR 官方 runtime: `~/work/runtime/llama.cpp`（build/bin/ 六个二进制）
- llama.cpp 源码: `~/work/llama.cpp`（pinned 803b7fc，供 FETCHCONTENT_SOURCE_DIR_LLAMA）
- 模型: `~/work/funasr-models/`（下载中）
- 测试素材: `~/work/whisper.cpp/2084无颜见人0.mp3` + `.txt`（220 秒中文，含技术词汇）
- 评分工具: `~/work/GoPi/scripts/asr_score.py`（CER + 技术词保留率 + 差异对齐）
- 对比脚本: `~/work/GoPi/scripts/asr_model_compare.sh`
- nick 的文档: `~/work/claude/nick/asr_deepseek.txt`
- 上游: `https://github.com/modelscope/FunASR/tree/main/runtime/llama.cpp`
