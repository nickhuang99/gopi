#!/usr/bin/env bash
# Doubao ASR evaluation matrix.
#
# Runs every reachable resource_id over the SAME 220 s Chinese tech audio and
# writes transcript + metrics + raw API response under out/.
# Audio files and results stay under ~/workspace (never /tmp, never the / partition).
#
# Usage: bash scripts/run-matrix.sh
set -u
cd "$(dirname "$0")/.."
ROOT="$PWD"
BIN="$ROOT/out/bin"
EV="$ROOT/out/evidence"
AUDIO="$ROOT/out/audio/audio16k.wav"

mkdir -p "$EV" "$ROOT/out/results"
run_auc() {  # $1=tag $2=resource_id $3...=extra flags
  local tag="$1" res="$2"; shift 2
  echo "=== $tag ($res) ==="
  timeout 300 "$BIN/aucfile" -file "$AUDIO" -format wav -resource_id "$res" \
      -outdir "$EV" -tag "$tag" "$@" 2>&1 | tail -3
}

# ── 录音文件识别 standard vs 极速版 ─────────────────────────────────
run_auc auc_std volc.bigasr.auc
run_auc auc_turbo volc.bigasr.auc_turbo

# ── 增强能力抽样（同一音频，逐项开关）────────────────────────────
run_auc auc_nopunc volc.bigasr.auc -punc=false
run_auc auc_ddc    volc.bigasr.auc -ddc=true
run_auc auc_speaker volc.bigasr.auc -speaker=true
run_auc auc_lid    volc.bigasr.auc -lid=true

# ── 热词（直传 hotwords，走 corpus.context）──────────────────────
HOTWORDS='{"hotwords":[{"word":"RAG"},{"word":"Agent"},{"word":"Prompt"},{"word":"AR VR"},{"word":"硅基"}]}'
echo "=== auc_hotwords (corpus.context hotwords) ==="
timeout 300 "$BIN/aucfile" -file "$AUDIO" -format wav -resource_id volc.bigasr.auc \
    -outdir "$EV" -tag auc_hotwords -corpus "$HOTWORDS" 2>&1 | tail -3

# ── 流式 SAUC：账号未开通 duration 额度，实测记录失败证据 ─────────
echo "=== sauc_20_stream (expect resource-not-granted) ==="
timeout 60 "$BIN/minclient" -file "$AUDIO" -url wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async \
    -resource_id volc.seedasr.sauc.duration -paced=true -outdir "$EV" -tag sauc20_async 2>&1 | tail -2
echo "=== sauc_10_stream (expect resource-not-granted) ==="
timeout 60 "$BIN/minclient" -file "$AUDIO" -url wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async \
    -resource_id volc.bigasr.sauc.duration -paced=true -outdir "$EV" -tag sauc10_async 2>&1 | tail -2
echo "=== sauc_20_nostream file semantics (expect resource-not-granted) ==="
timeout 60 "$BIN/minclient" -file "$AUDIO" -url wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream \
    -resource_id volc.seedasr.sauc.duration -paced=false -outdir "$EV" -tag sauc20_nostream 2>&1 | tail -2

echo "MATRIX DONE"
