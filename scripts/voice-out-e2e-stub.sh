#!/usr/bin/env bash
# voice-out-e2e-stub.sh — prove `gopi voice_input --client --out FILE` really
# keeps the captured audio, on a host with NO sound card.
#
# Why a stub rig: the only capture-capable host in this fleet is the HP laptop
# (see devdoc/voice-client-e2e-peer.md), so a proof that needs a real microphone
# cannot run in CI or on the development box. The capture CONTRACT is therefore
# faked at the recorder boundary instead: `arecord`/`pactl` are replaced by tiny
# stubs on PATH that emit a deterministic 440 Hz PCM16 signal for the real
# recording and 0.1s for the policy's liveness probe. Everything above the
# recorder — device policy, argv, trimming, the WAV the client writes, the f32
# it pipes — is the REAL code path in the real binary.
#
# What it asserts (each is a claim about the debug file, not a smoke test):
#   A  --out writes a WAV whose header describes exactly the captured samples
#      (rate/channels/bits/data-size), byte-identical to what the HTTP route
#      forwards, with NON-ZERO audio inside.
#   B  the pre-fix failure is gone: `--out` is accepted (never "unknown flag"),
#      and WITHOUT --out no file is created at all (the flag is what asks).
#   C  THE BUG-REPORT SCENARIO: the forward FAILS (no server on the port) and
#      the WAV still exists, valid — that is the whole point of the feature: a
#      failing client run must leave playable evidence behind.
#   D  an existing file is refused and left untouched (a capture must not
#      overwrite a recording someone kept).
#   E  the refusal's ADVICE is derived from the target that failed: a client
#      pointed at a dead non-default port is never told to start a server on
#      :7788, and the recording still survives.
#
#   Usage:  bash scripts/voice-out-e2e-stub.sh [REPO_ROOT] [BINARY]
#
# The workspace is a per-run directory under the SHARED scratch tree — the same
# one the Go suites use — and a measured device id, never a path spelling,
# settles that it is off the `/` partition (Rule 22). Never /tmp, and no longer
# the shared-memory filesystem: creating there is RETIRED (operator ruling,
# 2026-09-18).

set -euo pipefail

ROOT="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
BIN="${2:-$ROOT/bin/gopi-voice-out-stub-proof}"
# shellcheck source=lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing"; exit 2; }
WORK="$(gopi_scratch_mkdir voice-out-proof)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }

# Rule 22 criterion, measured (never assumed from a path spelling). The helper
# already asserted it; the rig re-states it so the proof carries its own check.
gopi_assert_off_root_fs "$WORK" || fail "workspace $WORK is not verifiably off the / partition"

if [ ! -x "$BIN" ]; then
	echo "[proof] building $BIN"
	(cd "$ROOT" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/agent/) || fail "build failed"
fi

mkdir -p "$WORK/bin" "$WORK/state"
export WORK

# --- stubbed recorder ------------------------------------------------------
# arecord: `-l` answers the enumeration listing; a capture answers 0.1s on the
# FIRST invocation per device (the policy's 1s liveness probe) and 1.0s of a
# 440 Hz tone afterwards (the real recording). The per-device counter lives in
# $WORK/state so a case can be replayed from a clean slate.
cat >"$WORK/bin/arecord" <<'STUB'
#!/usr/bin/env bash
if [[ " $* " == *" -l "* ]]; then
	echo 'card 1: STUB [Stub Card], device 0: Stub Mic [Stub Mic]'
	exit 0
fi
dev=""
prev=""
for a in "$@"; do
	if [ "$prev" = "-D" ]; then dev="$a"; fi
	prev="$a"
done
safe="${dev//[^A-Za-z0-9]/_}"
n=$(( $(cat "$WORK/state/$safe" 2>/dev/null || echo 0) + 1 ))
echo "$n" >"$WORK/state/$safe"
samples=1600 # 0.1s at 16kHz = the liveness probe
[ "$n" -ge 2 ] && samples=16000 # 1.0s = the real recording
exec python3 - "$samples" <<'PY'
import math, struct, sys
n = int(sys.argv[1])
buf = b"".join(struct.pack("<h", int(12000 * math.sin(2 * math.pi * 440 * i / 16000))) for i in range(n))
sys.stdout.buffer.write(buf)
PY
STUB

# pactl: `info` prints nothing (→ no "Server Name", so the policy CONSIDERS its
# tier-3 system default and then records from it), `list short sources` is empty
# (Pulse owns no device here), everything else succeeds quietly.
cat >"$WORK/bin/pactl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$WORK/bin/arecord" "$WORK/bin/pactl"
export PATH="$WORK/bin:$PATH"

run_client() { # run_client <workdir-name> <logfile> [args...]
	local dir="$WORK/$1" log="$WORK/$2" rc=0
	shift 2
	mkdir -p "$dir"
	(cd "$dir" && "$BIN" tool voice_input "$@") >"$log.stdout" 2>"$log.stderr" && rc=0 || rc=$?
	echo "$rc"
}

# --- A: --out writes a real WAV, and --out is accepted ---------------------
code=$(run_client a a.log --client --seconds 1 --out "$WORK/a/voice.wav" --stdio) || true
[ "$code" = "0" ] || { cat "$WORK/a.log.stderr" >&2; fail "case A: exit $code"; }
grep -q "unknown flag" "$WORK/a.log.stderr" && fail "case A: --out was refused as an unknown flag (the pre-fix defect)"
grep -q "wrote" "$WORK/a.log.stderr" || fail "case A: the client never reported writing the file"

python3 - "$WORK/a/voice.wav" "$WORK/a.log.stdout" <<'PY' || fail "case A: the WAV is not what the capture contract promises"
import struct, sys
wav = open(sys.argv[1], "rb").read()
raw = open(sys.argv[2], "rb").read()
u16 = lambda o: struct.unpack_from("<H", wav, o)[0]
u32 = lambda o: struct.unpack_from("<I", wav, o)[0]
pcm = wav[44:]
assert wav[0:4] == b"RIFF", "no RIFF magic"
assert wav[8:12] == b"WAVE", "no WAVE magic"
assert u16(20) == 1, "format tag must be integer PCM (was %d)" % u16(20)
assert u16(22) == 1, "channels must be 1 (was %d)" % u16(22)
assert u32(24) == 16000, "sample rate must be 16000 (was %d)" % u32(24)
assert u16(34) == 16, "bits must be 16 (was %d)" % u16(34)
assert u32(40) == len(pcm), "data size %d does not match the %d bytes carried" % (u32(40), len(pcm))
# 1.0s of 16kHz mono PCM16 — the recording, NOT the 0.1s probe.
assert len(pcm) == 32000, "captured %d bytes, want 32000 (1.0s of the real recording)" % len(pcm)
vals = struct.unpack_from("<%dh" % (len(pcm) // 2), pcm)
assert max(vals) > 10000 and min(vals) < -10000, "the WAV holds silence — not a usable debug recording"
assert len(set(vals)) > 100, "the samples are degenerate (%d distinct)" % len(set(vals))
# --stdio streams the SAME audio as raw f32: 32000 PCM16 bytes → 64000 f32 bytes.
assert len(raw) == 64000, "stdout held %d bytes of f32, want 64000" % len(raw)
f = struct.unpack_from("<16000f", raw)
assert max(abs(x) for x in f) > 0.3, "the piped f32 is silent"
print("[proof] A ok: %d-byte WAV, %d bytes of f32 piped, non-silent" % (len(wav), len(raw)))
PY

# --- B: without --out nothing is written -----------------------------------
code=$(run_client b b.log --client --seconds 1 --stdio) || true
[ "$code" = "0" ] || { cat "$WORK/b.log.stderr" >&2; fail "case B: exit $code"; }
[ "$(stat -c %s "$WORK/b.log.stdout")" = "64000" ] || fail "case B: stdout is not the same 64000 f32 bytes"
[ -z "$(ls -A "$WORK/b")" ] || fail "case B: a file appeared in the working directory without --out"
grep -q "wrote" "$WORK/b.log.stderr" && fail "case B: the client claimed to write a file it was not asked for"

# --- C: a FAILED forward still leaves the recording (the bug report) --------
code=$(run_client c c.log --client --seconds 1 --out "$WORK/c/voice.wav" --server-url http://127.0.0.1:1) || true
[ "$code" != "0" ] || fail "case C: the forward to a dead port should have failed"
[ -s "$WORK/c/voice.wav" ] || fail "case C: the forward failed and the recording was LOST — the defect this feature fixes"
python3 - "$WORK/c/voice.wav" <<'PY' || fail "case C: the surviving file is not a valid recording"
import struct, sys
wav = open(sys.argv[1], "rb").read()
assert wav[0:4] == b"RIFF" and wav[8:12] == b"WAVE"
assert len(wav) == 44 + 32000, "size %d" % len(wav)
print("[proof] C ok: forward failed, %d-byte playable WAV kept" % len(wav))
PY

# --- D: an existing file is refused and left untouched ---------------------
mkdir -p "$WORK/d"
printf 'yesterday' >"$WORK/d/kept.wav"
code=$(run_client d d.log --client --seconds 1 --out "$WORK/d/kept.wav" --stdio) || true
[ "$code" != "0" ] || fail "case D: overwriting a kept recording must be refused"
grep -q "already exists" "$WORK/d.log.stderr" || fail "case D: the refusal does not say the file exists"
[ "$(cat "$WORK/d/kept.wav")" = "yesterday" ] || fail "case D: the kept recording was modified"

# --- E: the refusal's ADVICE names THIS run's target (the 2026-10-05 report) -
# The reported run was `--client --out FILE --server-url <somewhere else>` and
# the error's hint said ":7788" — the default, printed under a "cannot reach"
# line naming the URL this run actually asked for, which reads as "it is still
# using the previous run's server". The hint must be derived from the target
# that failed.
DEADPORT=7951
code=$(run_client e e.log --client --seconds 1 --out "$WORK/e/voice.wav" \
	--server-url "http://127.0.0.1:$DEADPORT") || true
[ "$code" != "0" ] || fail "case E: the forward to a dead port should have failed"
grep -q "cannot reach transcriber at http://127.0.0.1:$DEADPORT/transcribe" "$WORK/e.log.stderr" ||
	fail "case E: the refusal does not name the target this run used"
grep -q ":$DEADPORT" "$WORK/e.log.stderr" || fail "case E: the advice does not name the port that failed"
grep -q ":7788" "$WORK/e.log.stderr" &&
	fail "case E: the advice names the :7788 DEFAULT under a refusal for :$DEADPORT — the address of a different run"
[ -s "$WORK/e/voice.wav" ] || fail "case E: the failed forward lost the recording"

echo "[proof] voice_input --client --out: A/B/C/D/E all PASS (binary: $BIN)"
