#!/usr/bin/env bash
# Clean paired timing run: 1.0 vs 2.0, file semantics, SERIAL, 2 repeats each.
#
# Why re-run: the first 2.0 file-semantics measurement (auc_std_20, 49.2 s) was
# taken while the streaming matrix was hammering the same endpoint, so it
# measured contention, not the model. This script is run with NOTHING else in
# flight so the 1.0/2.0 comparison is apples-to-apples.
#
# Also runs the targeted hotword experiment: the 1.0 baseline mis-hears
# 概念对齐 as 概念对其 (齐→其, a homophone) — boosting exactly that phrase is the
# smallest sample that can show a boosting-table effect.
set -u
cd "$(dirname "$0")/.."
BIN="$PWD/out/bin/aucfile"
AUDIO="$PWD/out/audio/audio16k.wav"
EV="$PWD/out/evidence"

run() { # tag resource [extra...]
  local tag="$1" res="$2"; shift 2
  timeout 300 "$BIN" -file "$AUDIO" -resource_id "$res" -outdir "$EV" -tag "$tag" "$@" 2>&1 | tail -1
}

for rep in 1 2; do
  echo "=== rep$rep 1.0 volc.bigasr.auc ==="
  run "pair10_r$rep" volc.bigasr.auc
  echo "=== rep$rep 2.0 volc.seedasr.auc ==="
  run "pair20_r$rep" volc.seedasr.auc
done

echo "=== hotword targeted: boost 概念对齐 / 裸模型对齐 (1.0) ==="
run hw_target volc.bigasr.auc \
  -corpus '{"context":"{\"hotwords\":[{\"word\":\"概念对齐\"},{\"word\":\"裸模型对齐\"}]}"}'

echo "PAIR DONE"
