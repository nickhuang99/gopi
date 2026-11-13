#!/usr/bin/env bash
# Streaming (SAUC) matrix — v2, SERIAL with per-run evidence.
#
# v1 ran the runs back-to-back too tightly: the second realtime run (200 ms packets,
# wall-clock paced) never returned and was killed at the 400 s guard, so no metrics
# file was written for it. v2 runs ONE run at a time, logs each run's exit status,
# and keeps the per-run timeout short enough that a hang is evidence, not a stall.
#
# Semantics split (the fairness axis the epic demands):
#   realtime : seg_duration=200ms, packets paced in wall-clock time -> end-to-end
#              latency of a live stream (comparable to Fun-ASR chunked streaming)
#   file     : large seg_duration, no pacing -> pure throughput of the same
#              bidirectional endpoint (comparable to a batch/file transcription)
set -u
cd "$(dirname "$0")/.."
BIN="$PWD/out/bin"
EV="$PWD/out/evidence"
AUDIO="$PWD/out/audio/audio16k.wav"
SAUC=wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async
SAUCN=wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream

run() {  # tag resource url seg paced budget_ms [extra...]
  local tag="$1" res="$2" url="$3" seg="$4" paced="$5" budget="$6"; shift 6
  echo "=== $tag ==="
  local t0 t1 rc
  t0=$(date +%s)
  timeout "$budget" "$BIN/minclient" -file "$AUDIO" -url "$url" -resource_id "$res" \
      -seg_duration "$seg" -paced="$paced" -outdir "$EV" -tag "$tag" "$@" 2>&1 | tail -2
  rc=${PIPESTATUS[0]}
  t1=$(date +%s)
  echo "[$tag] exit=$rc wall=$((t1 - t0))s"
  echo
}

# ── realtime semantics: 200 ms packets, wall-clock paced (~220 s per run) ──
run sauc_rt_10_200 volc.bigasr.auc  "$SAUC"  200   true  300
run sauc_rt_20_200 volc.seedasr.auc "$SAUC"  200   true  300

# ── file semantics on the SAME streaming endpoint: big packets, no pacing ──
run sauc_file_10_10s volc.bigasr.auc  "$SAUC"  10000 false 300
run sauc_file_20_10s volc.seedasr.auc "$SAUC"  10000 false 300

# ── one-shot /nostream endpoint (true file semantics, single result) ──────
run sauc_ns_10 volc.bigasr.auc  "$SAUCN" 60000 false 300
run sauc_ns_20 volc.seedasr.auc "$SAUCN" 60000 false 300

echo "STREAM MATRIX DONE"
