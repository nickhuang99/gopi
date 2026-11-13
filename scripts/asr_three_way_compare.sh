#!/usr/bin/env bash
# 三方 ASR 对比：whisper small / whisper medium / Fun-ASR-Nano
#
# 设计要点（沿用本项目已有教训）：
#  - 输出落 ~/workspace，不写 boot 分区（2026-09-11 耗尽事故）
#  - 计时用 /usr/bin/time -v 写文件（不能用 tail 截断——曾丢掉选型所需的数字）
#  - 串行执行（并行会互相抢 CPU 污染计时）
#  - 每方都用同一个 asr_score.py 评分（唯一公平的比法）
set -u

HOME_DIR="$HOME"
WS="$HOME_DIR/workspace/whisper-test"
OUT="$HOME_DIR/workspace/asr-compare"
AUDIO="$HOME_DIR/work/whisper.cpp/2084无颜见人0.mp3"
REF="$HOME_DIR/work/whisper.cpp/2084无颜见人0.txt"
SCORE="$HOME_DIR/work/GoPi/scripts/asr_score.py"
mkdir -p "$OUT"

score() {  # $1=name $2=transcript
    [ -s "$2" ] || { printf "  %-14s (no output)\n" "$1"; return; }
    printf "\n  [%s]\n" "$1"
    python3 "$SCORE" "$2" "$REF" 2>&1 \
        | grep -E 'CER|accuracy|kept|MISSING|SPURIOUS' | sed 's/^/    /'
}

timing() {  # $1=name $2=timefile
    [ -s "$2" ] && grep -E 'Elapsed \(wall|Maximum resident' "$2" | sed "s/^\s*/  $1 /"
}

# ── whisper.cpp（AVX2 构建）──────────────────────────────────────────
for M in small medium; do
    MODEL="$HOME_DIR/work/whisper.cpp/models/ggml-$M.bin"
    BIN="$HOME_DIR/work/whisper.cpp/build-avx2/bin/whisper-cli"
    [ -f "$MODEL" ] && [ -x "$BIN" ] || { echo "SKIP whisper-$M (missing)"; continue; }
    echo "=== whisper $M ==="
    /usr/bin/time -v "$BIN" -m "$MODEL" -f "$AUDIO" -l zh \
        -otxt -of "$OUT/whisper-$M" 2> "$OUT/whisper-$M.time" > /dev/null
    timing "whisper-$M" "$OUT/whisper-$M.time"
done

# ── FunASR-Nano（官方 llama.cpp runtime）────────────────────────────
ENC="$HOME_DIR/work/funasr-models/funasr-encoder-f16.gguf"
LLM="$HOME_DIR/work/funasr-models/qwen3-0.6b-q4km.gguf"
FBIN="$HOME_DIR/work/runtime/llama.cpp/build/bin/llama-funasr-cli"
if [ -f "$ENC" ] && [ -f "$LLM" ] && [ -x "$FBIN" ]; then
    echo "=== Fun-ASR-Nano ==="
    # --chunk is required: without it a long clip is fed as one block and the
    # model emits degenerate output (observed: 220s in, repeated '.' out).
    # README's own gotchas list this as the standard invocation.
    /usr/bin/time -v "$FBIN" --enc "$ENC" -m "$LLM" -a "$AUDIO" --chunk 15 \
        > "$OUT/funasr-nano.txt" 2> "$OUT/funasr-nano.time"
    timing "funasr-nano" "$OUT/funasr-nano.time"
else
    echo "SKIP funasr-nano (model or binary missing)"
fi

# ── 统一评分 ─────────────────────────────────────────────────────────
echo
echo "══════════════ SCORES (same audio, same scorer) ══════════════"
for f in whisper-small whisper-medium funasr-nano; do
    score "$f" "$OUT/$f.txt"
done
