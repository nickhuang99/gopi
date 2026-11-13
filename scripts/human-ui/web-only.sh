#!/usr/bin/env bash
# web-only.sh — re-run JUST the `gopi web` leg (seed a real turn, then shoot it).
#
# Why a separate entry point: the web capture is the one leg that needs no
# display and no xterm, so when only it fails the other three surfaces do not
# have to be re-captured (each chat leg costs a real model round trip).
set -uo pipefail
# The repository is DERIVED from this script location, overridable: a
# hardcoded worktree uuid exists on exactly one host.
ROOT="${GOPI_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"
SCRATCH="${1:?usage: web-only.sh <scratch-dir> [sandbox-dir]}"
SB="${2:-}"
BIN="$ROOT/bin/gopi"
export GOPI_SHOT_MEM_MAX_MB="${GOPI_SHOT_MEM_MAX_MB:-4096}"
[ -n "$SB" ] && export GOPI_HOME="$SB"
PORT=18097
"$BIN" web --addr "127.0.0.1:$PORT" >"$SCRATCH/web-server.log" 2>&1 &
WEB_PID=$!
sleep 5
kill -0 "$WEB_PID" 2>/dev/null || { echo "web server died"; tail -5 "$SCRATCH/web-server.log"; exit 1; }
python3 "$ROOT/tools/human-ui/seed_web.py" --addr "127.0.0.1:$PORT" \
  --prompt "In two short lines: what does gopi's ledger keep?" \
  --json-out "$SCRATCH/web-turn.json" || { echo "seed failed"; kill $WEB_PID; exit 1; }
"$BIN" tool shot --self --addr "127.0.0.1:$PORT" --viewport 1440x1400 \
  --wait-selector "#log" --wait 3 --out "$SCRATCH/web-still.png" \
  >"$SCRATCH/shot.json" 2>"$SCRATCH/shot.err" || { echo "shot failed: $(tail -2 "$SCRATCH/shot.err")"; kill $WEB_PID; exit 1; }
echo "shot: $(cat "$SCRATCH/shot.json")"
python3 -c "
import struct
d=open('$SCRATCH/web-still.png','rb').read()
print('web-still.png', struct.unpack('>II', d[16:24]), len(d), 'bytes')"
kill $WEB_PID 2>/dev/null
