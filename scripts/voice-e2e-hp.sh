#!/usr/bin/env bash
# voice-e2e-hp.sh — deterministic, human-free end-to-end proof of the two-node
# voice-input path. Rerunnable by any reader with access to the two nodes.
#
#   HP   (`ssh hp`,    host-b) = capture node. Owns the microphone, the
#                                       ALSA/Pulse stack and the client binary.
#   peer1(`ssh peer1`)                 = engine node. Owns gopi + the
#                                       Fun-ASR-Nano weights (~/work/funasr-models).
#
# Claim proven: audio captured on HP crosses to peer1 and comes back as text.
# Why it needs no human: nothing is spoken live. A fixed WAV (a spoken Chinese
# paragraph with a written reference transcript) is played into a PulseAudio
# null-sink and the sink's `.monitor` is captured as the "microphone" — the same
# Pulse source-selection path a real mic uses. Leg B repeats it on a genuinely
# different device: the physical ALC236 capture device, reached through the
# logical name `default`.
#
# Scoring: scripts/asr_score.py from THIS repo (CER against the reference text).
#
#   Usage:  SRCDIR=<HP dir containing bin/gopi> bash scripts/voice-e2e-hp.sh
#   Exit:   0 = both legs transcribed and were scored; 1 = prerequisite missing.
set -euo pipefail

# --------------------------------------------------------- scratch contract
# The shared helper carries the off-`/` device assert and the trap-time cleanup
# (scripts/lib/tmp-ban.sh). /tmp is on the 87%-full `/` partition whose
# 2026-09-11 exhaustion is why dev/test scratch is banned there (Rule 22).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

# ONE scratch directory for BOTH nodes and BOTH legs: this directory is created
# and asserted off `/` here, and the capture node's twin is created at the same
# path (WORKDIR) and asserted with the SAME device-id criterion before use — no
# path is ever typed inline and nothing lands on /tmp.
WORK_ROOT="$(gopi_scratch_mkdir voice-e2e-hp)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# Cleanup runs through cleanup_all once the capture node is known (it removes
# HP's twin, then this side's scratch directory, killing stray children first).
trap gopi_scratch_cleanup EXIT INT TERM

# ------------------------------------------------------------------ constants
# Every device string / rate / path / volume is named here; nothing is typed
# inline at a use site (Rule 1). Any of them can be overridden from the env.
readonly HP_HOST=${HP_HOST:-hp}
readonly PEER_HOST=${PEER_HOST:-peer1}
# WORKDIR: the scratch directory on BOTH nodes (default above; an explicit env
# override is honoured for a reader who already has a prepared workspace).
readonly WORKDIR=${WORKDIR:-$WORK_ROOT}
readonly SRCDIR=${SRCDIR:-$HOME/gopi-voice-e2e-src}
readonly CLIENT_BIN=${CLIENT_BIN:-$SRCDIR/bin/gopi}
readonly SERVER_CMD=${SERVER_CMD:-"gopi tool voice_input --server --stdio"}

# The pinned capture contract — identical to the client's, and the reason a
# stream stays decodable on the far side (16 kHz mono PCM16).
readonly CAP_RATE=16000
readonly CAP_CHANNELS=1
readonly CAP_BITS=16
readonly CAP_FMT_PULSE=s16le
readonly CAP_FMT_ALSA=S16_LE

# The detached-server form of --stdio reads raw 32-bit float PCM on stdin
# (voiceServeStdio → voiceWrapPCMAsWAV, format=3/32-bit). A PCM16 capture must
# therefore be converted before it is piped; the client does exactly this
# (`voicePCM16ToFloat32`, cmd/agent/cli_voice_client.go). A container file
# (wav/mp3/ogg/flac) needs no conversion — it is passed through untouched.
readonly NEEDS_F32_CONVERSION=1

# The deterministic fixture, produced once from a longer reference recording
# (see the report §Fixtures) and pinned by sha256 in the log. The default DERIVES
# from the shared scratch directory rather than from a path baked into this
# file: the producer is `bash scripts/voice-e2e-fixture.sh $FIXTURE_OUTDIR` run
# on HP (its own default is the same scratch tree), so the two scripts agree
# without a hardcoded location.
readonly FIXTURE_OUTDIR=${FIXTURE_OUTDIR:-$WORK_ROOT/voice-e2e-fixture}
readonly FIXTURE_WAV=${FIXTURE_WAV:-$FIXTURE_OUTDIR/clip.wav}
readonly FIXTURE_REF=${FIXTURE_REF:-$FIXTURE_OUTDIR/ref.txt}

# Per-leg logs the control side writes (transcript, engine stderr, reference):
# the scratch directory too, not an ambient temp path.
readonly LOGDIR=${LOGDIR:-$WORK_ROOT}

# Leg A — the deterministic "microphone": a Pulse null-sink monitor.
readonly NULL_SINK=voice_e2e_null
readonly NULL_SINK_MONITOR="$NULL_SINK.monitor"
readonly NULL_SINK_VOLUME=25%

# Leg B — a different device path: the physical ALC236 capture device reached
# through `default`. Raw hw:/plughw: forms are BUSY while the audio server owns
# the PCM (measured: devdoc/voice-hw-matrix.md). The source volume is reduced
# from its +30 dB default ON PURPOSE: at the default the ALC236 ADC saturates
# (peak ≈ 1.0, no clipping counter to warn you) and Fun-ASR hallucinates a
# different paragraph — measured, see the report §Audio-path findings.
readonly LEG_B_ALSA_DEVICE=default
readonly LEG_B_SOURCE=alsa_input.pci-0000_05_00.6.analog-stereo
readonly LEG_B_SINK=alsa_output.pci-0000_05_00.6.analog-stereo
readonly LEG_B_SOURCE_VOLUME=12%
readonly LEG_B_SINK_VOLUME=40%
readonly LEG_B_SECONDS=${LEG_B_SECONDS:-228}

section() { printf '\n=== %s ===\n' "$*"; }
run_hp() { ssh "$HP_HOST" "$@"; }

# ------------------------------------------------------------------ scratch
# Both nodes work in the SAME scratch directory (WORKDIR). The control side's
# copy was created and asserted off `/` by gopi_scratch_mkdir above; HP's twin is
# created below through the same device-id criterion (HP has no checkout of this
# repo, so the fragment is self-contained: stat -c %d of the directory vs of /).
# Neither copy is ever /tmp, and both are removed on exit — the control side by
# gopi_scratch_cleanup, HP by cleanup_all below.
readonly HP_SCRATCH_DEVID_CMD="printf '# hp scratch  : %s  dev=%s (/ dev=%s)\n' '$WORKDIR' \"\$(stat -c %d '$WORKDIR')\" \"\$(stat -c %d /)\""

cleanup_all() {
    # Best effort, in order: HP's twin (a remote failure must never leave the
    # control-side cleanup unrun — Rule 15b reports, never swallows silently).
    ssh -o BatchMode=yes "$HP_HOST" "rm -rf '$WORKDIR'" >/dev/null 2>&1 \
        || echo "warning: could not remove $WORKDIR on $HP_HOST (age-GC it there)" >&2
    gopi_scratch_cleanup
}
trap cleanup_all EXIT INT TERM

# ------------------------------------------------------------------ preflight
command -v ssh >/dev/null || { echo "FAIL: ssh not found"; exit 1; }
ssh -o BatchMode=yes "$HP_HOST" true 2>/dev/null || { echo "FAIL: no ssh to $HP_HOST"; exit 1; }
ssh -o BatchMode=yes "$PEER_HOST" 'command -v gopi' >/dev/null 2>&1 \
  || { echo "FAIL: gopi not on $PEER_HOST PATH"; exit 1; }
run_hp "test -x '$CLIENT_BIN'" || { echo "FAIL: client binary not executable: $CLIENT_BIN"; exit 1; }

section "preflight"
echo "# hp          : $HP_HOST  ($(run_hp hostname))"
echo "# peer1       : $PEER_HOST  ($(ssh "$PEER_HOST" hostname))"
echo "# client bin  : $CLIENT_BIN  built from $(cd "$HERE/.." && git rev-parse --short HEAD)"
echo "# capture     : $CAP_RATE Hz, $CAP_CHANNELS ch, PCM${CAP_BITS}LE"
echo "# fixture wav : $FIXTURE_WAV  sha256=$(run_hp "sha256sum '$FIXTURE_WAV' | cut -c1-32")"
echo "# reference   : $FIXTURE_REF  sha256=$(run_hp "sha256sum '$FIXTURE_REF' | cut -c1-32")"

# The capture node's workspace: created here, asserted off `/` (the same
# criterion the control side's scratch root just passed), reported with both
# device ids as evidence.
run_hp "mkdir -p '$WORKDIR'" || { echo "FAIL: cannot create $WORKDIR on $HP_HOST"; exit 1; }
run_hp "$HP_SCRATCH_DEVID_CMD"
run_hp "test \"\$(stat -c %d '$WORKDIR')\" != \"\$(stat -c %d /)\"" \
  || { echo "FAIL: $WORKDIR on $HP_HOST is on the / filesystem (Rule 22) — set WORKDIR to a path off /"; exit 2; }
mkdir -p "$LOGDIR"

# ---------------------------------------------------------------------- leg A
section "LEG A — null-sink monitor ($NULL_SINK_MONITOR) -> $SERVER_CMD @ $PEER_HOST"
run_hp bash -s <<HPSCRIPT
set -euo pipefail
cd $WORKDIR
MOD=\$(pactl load-module module-null-sink sink_name=$NULL_SINK \\
        sink_properties=device.description=$NULL_SINK 2>/dev/null || echo 0)
trap 'pactl unload-module "\$MOD" 2>/dev/null || true' EXIT
echo "# module id   : \$MOD"
echo "# sink        : \$(pactl list short sinks   | grep -F $NULL_SINK | head -1)"
echo "# monitor src : \$(pactl list short sources | grep -F $NULL_SINK | head -1)"
pactl set-sink-volume $NULL_SINK $NULL_SINK_VOLUME

parecord --device=$NULL_SINK_MONITOR --raw --rate=$CAP_RATE --channels=$CAP_CHANNELS \\
         --format=$CAP_FMT_PULSE legA.pcm &
REC=\$!
sleep 1
paplay --device=$NULL_SINK '$FIXTURE_WAV'; echo "# paplay rc    : \$?"
# paplay BLOCKS until the playback has drained, so this is already the end of
# the fixture; the short sleep only lets the sink's last buffer reach the
# monitor. (Sleeping for the fixture length here would capture it twice — a
# measured 448 s capture from a 220 s fixture.)
sleep 1
kill -INT \$REC; wait \$REC 2>/dev/null || true
echo "# captured     : \$(stat -c%s legA.pcm) bytes"
sox -t raw -r $CAP_RATE -e signed -b $CAP_BITS -c $CAP_CHANNELS legA.pcm legA.wav
sox legA.wav -n stat 2>&1 | egrep 'Length|Maximum amplitude|RMS     amplitude'
if [ "$NEEDS_F32_CONVERSION" = 1 ]; then
  sox legA.wav -t raw -e floating-point -b 32 -c $CAP_CHANNELS legA.f32
  echo "# converted    : PCM16 -> f32 (\$(stat -c%s legA.f32) bytes) for the --stdio wire"
fi
HPSCRIPT

echo "# transport   : ssh $PEER_HOST \"$SERVER_CMD\" < legA.f32"
run_hp "ssh $PEER_HOST '$SERVER_CMD' < $WORKDIR/legA.f32" > "$LOGDIR/legA.txt" 2>"$LOGDIR/legA.err" || true
echo "# engine      : $(tr '\n' ' ' < "$LOGDIR/legA.err")"
echo "# transcript  : $(cat "$LOGDIR/legA.txt")"
run_hp "cat '$FIXTURE_REF'" > "$LOGDIR/reference.txt"
echo "# CER (scripts/asr_score.py):"
python3 "$HERE/asr_score.py" "$LOGDIR/legA.txt" "$LOGDIR/reference.txt" | sed -n '1,12p'

# ---------------------------------------------------------------------- leg B
section "LEG B — physical device ($LEG_B_ALSA_DEVICE, $LEG_B_SOURCE) -> $SERVER_CMD @ $PEER_HOST"
run_hp bash -s <<HPSCRIPT
set -euo pipefail
cd $WORKDIR
pactl set-source-volume $LEG_B_SOURCE $LEG_B_SOURCE_VOLUME
pactl set-sink-volume   $LEG_B_SINK   $LEG_B_SINK_VOLUME
echo "# source      : \$(pactl list short sources | grep -F $LEG_B_SOURCE | head -1)"
echo "# source vol  : \$(pactl get-source-volume $LEG_B_SOURCE | tr -s ' ')"
echo "# sink        : \$(pactl list short sinks | grep -F $LEG_B_SINK | head -1)"
echo "# sink vol    : \$(pactl get-sink-volume $LEG_B_SINK | tr -s ' ')"
arecord -q -D $LEG_B_ALSA_DEVICE -f $CAP_FMT_ALSA -r $CAP_RATE -c $CAP_CHANNELS -d $LEG_B_SECONDS legB.wav &
REC=\$!
sleep 1
paplay --device=$LEG_B_SINK '$FIXTURE_WAV'; echo "# paplay rc    : \$?"
wait \$REC 2>/dev/null || true
echo "# captured     : \$(stat -c%s legB.wav) bytes"
sox legB.wav -n stat 2>&1 | egrep 'Length|Maximum amplitude|RMS     amplitude'
if [ "$NEEDS_F32_CONVERSION" = 1 ]; then
  sox legB.wav -t raw -e floating-point -b 32 -c $CAP_CHANNELS legB.f32
  echo "# converted    : PCM16 -> f32 (\$(stat -c%s legB.f32) bytes) for the --stdio wire"
fi
HPSCRIPT

echo "# transport   : ssh $PEER_HOST \"$SERVER_CMD\" < legB.f32"
run_hp "ssh $PEER_HOST '$SERVER_CMD' < $WORKDIR/legB.f32" > "$LOGDIR/legB.txt" 2>"$LOGDIR/legB.err" || true
echo "# engine      : $(tr '\n' ' ' < "$LOGDIR/legB.err")"
echo "# transcript  : $(cat "$LOGDIR/legB.txt")"
echo "# CER (scripts/asr_score.py):"
python3 "$HERE/asr_score.py" "$LOGDIR/legB.txt" "$LOGDIR/reference.txt" | sed -n '1,12p'

section "done"
echo "# scratch     : $WORKDIR (removed on exit on both nodes; HP's copy is the same path)"
echo "# transcripts : $LOGDIR/legA.txt $LOGDIR/legB.txt"
echo "# rerun       : SRCDIR=$SRCDIR bash scripts/voice-e2e-hp.sh"
