#!/usr/bin/env bash
# Probe which Doubao ASR resource_ids the account can actually reach.
# The API key is read from ~/.doubao/apikey.txt and never printed.
# Output goes under out/ (never /tmp, never the / partition).
set -u
OUT="out/probes"
mkdir -p "$OUT"
K=$(tr -d ' \n\r' < "$HOME/.doubao/apikey.txt")
for R in volc.bigasr.auc volc.bigasr.auc_turbo volc.bigasr.auc.concurrent \
         volc.megatts.default volc.bigasr.sauc.duration volc.seedasr.sauc.concurrent; do
  printf "%-32s -> " "$R"
  curl -s -m 20 -o "$OUT/body.$$" -D "$OUT/hdr.$$" \
    -X POST https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit \
    -H "X-Api-Key: $K" \
    -H "X-Api-Resource-Id: $R" \
    -H "X-Api-Request-Id: $(cat /proc/sys/kernel/random/uuid)" \
    -H "X-Api-Sequence: -1" \
    -H "Content-Type: application/json" \
    -d '{"user":{"uid":"probe"},"audio":{"url":"https://example.com/x.mp3","format":"mp3"},"request":{"model_name":"bigmodel"}}'
  grep -i "x-api-status-code\|x-api-message\|x-tt-logid" "$OUT/hdr.$$" | tr -d '\r' | tr '\n' ' '
  printf " | body=%s\n" "$(head -c 140 "$OUT/body.$$")"
  mv "$OUT/hdr.$$" "$OUT/$R.hdr.txt"
  mv "$OUT/body.$$" "$OUT/$R.body.txt"
done
