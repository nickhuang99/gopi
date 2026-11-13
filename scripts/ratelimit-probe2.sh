#!/usr/bin/env bash
# Rate-limit probe v2 — concurrency sweep with per-request curl exit codes.
#
# The v1 probe fired N=60 curl processes at once and every one of them wrote a
# ZERO-BYTE header file: the fan-out saturated before the first byte came back.
# v2 records the curl exit code + time_total per request so "the client could not
# even complete the request" is distinguishable from "the server answered 429".
#
# Usage: ratelimit-probe2.sh [N] [N ...]      (default: 5 20)
set -u
cd "$(dirname "$0")/.."
OUT=out/evidence/ratelimit2
mkdir -p "$OUT"
K=$(tr -d ' \n\r' < "$HOME/.doubao/apikey.txt")
SUMMARY="$OUT/summary.txt"
: > "$SUMMARY"

python3 - "$PWD" <<'PY'
import base64, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
d = root / "out/evidence/ratelimit2"
b64 = base64.b64encode((root / "out/audio/audio16k.wav").read_bytes()).decode()
body = {
    "user": {"uid": "gopi-ratelimit"},
    "audio": {"data": b64, "format": "wav", "codec": "raw",
              "rate": 16000, "bits": 16, "channel": 1},
    "request": {"model_name": "bigmodel"},
}
(d / "req.json").write_text(json.dumps(body))
PY

wave() {
  local n="$1"
  echo "=== wave concurrency=$n ===" | tee -a "$SUMMARY"
  local i
  for i in $(seq 1 "$n"); do
    (
      hdr="$OUT/w${n}_$i.hdr"
      rid=$(cat /proc/sys/kernel/random/uuid)
      rc=$(curl -sS -m 90 -o "$OUT/w${n}_$i.body" -D "$hdr" -w '%{http_code} %{time_total}' \
        -X POST https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit \
        -H "X-Api-Key: $K" -H "X-Api-Resource-Id: volc.bigasr.auc" \
        -H "X-Api-Request-Id: $rid" -H "X-Api-Sequence: -1" \
        -H "Content-Type: application/json" \
        --data-binary "@$OUT/req.json" 2> "$OUT/w${n}_$i.err")
      code=$?
      http=$(grep -oiE '^HTTP/[0-9.]+ [0-9]+' "$hdr" 2>/dev/null | awk '{print $2}' | tail -1)
      api=$(grep -i '^x-api-status-code' "$hdr" 2>/dev/null | tr -d '\r' | awk '{print $2}')
      logid=$(grep -i '^x-tt-logid' "$hdr" 2>/dev/null | tr -d '\r' | awk '{print $2}')
      msg=$(grep -i '^x-api-message' "$hdr" 2>/dev/null | tr -d '\r' | cut -d' ' -f2-)
      err=$(head -c 120 "$OUT/w${n}_$i.err" | tr '\n' ' ')
      printf "%s curl_exit=%s http=%s api=%s logid=%s msg=%s err=%s\n" \
        "$i" "$code" "${http:-NA}" "${api:-NA}" "${logid:-NA}" "${msg:-NA}" "${rc:-NA}" >> "$OUT/wave$n.txt"
      [ -n "$err" ] && printf "%s CURLERR=%s\n" "$i" "$err" >> "$OUT/wave$n.txt"
      true
    ) &
  done
  wait
  sort -n "$OUT/wave$n.txt" | tee -a "$SUMMARY"
  echo "--- wave $n tally ---" | tee -a "$SUMMARY"
  awk '{print $2}' "$OUT/wave$n.txt" | sort | uniq -c | tee -a "$SUMMARY"
  awk '{print $3, $4}' "$OUT/wave$n.txt" | sort | uniq -c | tee -a "$SUMMARY"
}

for n in "$@"; do wave "$n"; done
echo "RATELIMIT2 DONE"
