#!/usr/bin/env bash
# 模型选型对比：small vs medium，同一段中文音频。
#
# 设计要点（沿用本项目已有的教训）：
#  - 输出落 ~/workspace（boot 分区曾因临时产物耗尽，2026-09-11 事故）
#  - 每次都记录 wall time + 峰值内存 —— zbook 是 i7-5500U 双核，
#    "准确率" 和 "跑得动吗" 是同等重要的选型依据
#  - 用 /usr/bin/time -v 拿 Maximum resident set size（RSS）
set -u

WHISPER="$HOME/work/whisper.cpp"
OUT="$HOME/workspace/whisper-test"
AUDIO="$WHISPER/2084无颜见人0.mp3"
REF="$WHISPER/2084无颜见人0.txt"
mkdir -p "$OUT"

for M in small medium; do
    MODEL="$WHISPER/models/ggml-$M.bin"
    [ -f "$MODEL" ] || { echo "SKIP $M (model missing)"; continue; }
    echo "=== $M ($(du -h "$MODEL" | cut -f1)) ==="
    /usr/bin/time -v "$WHISPER/build/bin/whisper-cli" \
        -m "$MODEL" -f "$AUDIO" -l zh \
        -otxt -of "$OUT/$M-real" 2> "$OUT/$M.time" > /dev/null
    grep -E 'Elapsed \(wall|Maximum resident' "$OUT/$M.time" | sed 's/^\s*/  /'
    echo "  output: $(wc -c < "$OUT/$M-real.txt" 2>/dev/null || echo 0) bytes"
    if [ -f "$OUT/$M-real.txt" ]; then
        echo "  --- score ---"
        python3 "$HOME/work/GoPi/scripts/asr_score.py" \
            "$OUT/$M-real.txt" "$REF" 2>&1 | grep -E 'CER|accuracy|kept|MISSING|SPURIOUS' | sed 's/^/  /'
    fi
    echo
done
