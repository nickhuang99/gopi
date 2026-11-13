#!/usr/bin/env bash
# Rate-limit probe: 录音文件识别 documents "默认最大支持20QPS".
#
# Fires N concurrent submits and records EVERY status code with its logid, so a
# throttle (429 / quota-exceeded) is evidence instead of an assumption.
#
# Note: the request body carries a multi-MB base64 audio payload, so it is
# written to a file and passed with --data-binary @file — an inline -d argument
# would exceed ARG_MAX and curl would never run (silent empty result).
set -u
cd "$(dirname "$0")/.."
OUT=out/evidence/ratelimit
mkdir -p "$OUT"
K=$(tr -d ' \n\r' < "$HOME/.doubao/apikey.txt")
N=${1:-60}
: > "$OUT/summary.txt"

python3 - "$N" "$PWD" <<'PY'
import base64, json, sys, uuid, pathlib
n, root = int(sys.argv[1]), pathlib.Path(sys.argv[2])
audio = (root / "out/audio/audio16k.wav").read_bytes()
b64 = base64.b64encode(audio).decode()
for i in range(1, n + 1):
    body = {
        "user": {"uid": "gopi-ratelimit"},
        "audio": {"data": b64, "format": "wav", "codec": "raw",
                  "rate": 16000, "bits": 16, "channel": 1},
        "request": {"model_name": "bigmodel"},
    }
    d = root / "out/evidence/ratelimit"
    (d / f"req_{i}.json").write_text(json.dumps(body))
    (d / f"req_{i}.id").write_text(str(uuid.uuid4()))
PY

for i in $(seq 1 "$N"); do
  (
    RID=$(cat "$OUT/req_$i.id")
    curl -s -m 90 -o "$OUT/rl_$i.body" -D "$OUT/rl_$i.hdr" \
      -X POST https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit \
      -H "X-Api-Key: $K" -H "X-Api-Resource-Id: volc.bigasr.auc" \
      -H "X-Api-Request-Id: $RID" -H "X-Api-Sequence: -1" \
      -H "Content-Type: application/json" \
      --data-binary "@$OUT/req_$i.json"
    http=$(grep -oiE '^HTTP/[0-9.]+ [0-9]+' "$OUT/rl_$i.hdr" 2>/dev/null | awk '{print $2}' | tail -1)
    api=$(grep -i '^x-api-status-code' "$OUT/rl_$i.hdr" 2>/dev/null | tr -d '\r' | awk '{print $2}')
    logid=$(grep -i '^x-tt-logid' "$OUT/rl_$i.hdr" 2>/dev/null | tr -d '\r' | awk '{print $2}')
    msg=$(grep -i '^x-api-message' "$OUT/rl_$i.hdr" 2>/dev/null | tr -d '\r' | cut -d' ' -f2-)
    printf "%s http=%s api=%s logid=%s msg=%s\n" "$i" "${http:-NA}" "${api:-NA}" "${logid:-NA}" "${msg:-NA}" >> "$OUT/summary.txt"
  ) &
done
wait
sort -n "$OUT/summary.txt" | head -70
echo "--- HTTP status tally ---"
awk '{print $2}' "$OUT/summary.txt" | sort | uniq -c
echo "--- API status tally ---"
awk '{print $3}' "$OUT/summary.txt" | sort | uniq -c
