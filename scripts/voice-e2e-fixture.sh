#!/usr/bin/env bash
# voice-e2e-fixture.sh — rebuild the deterministic fixture the E2E proof uses.
#
# The fixture is one spoken Chinese recording plus a written reference
# transcript. It is what makes the test human-free: the recording is played into
# a sink/mic instead of a person speaking, and the text that SHOULD come back is
# already on disk, so the transcript is scored (CER) rather than eyeballed.
#
# Default: the WHOLE source recording, whose reference is the whole sidecar text
# — clip and reference are then 1:1 and the CER is a real number.
# CUT_AT=<seconds> takes a prefix instead; the reference is then only the first
# line, so the CER compares a fragment against the whole paragraph and inflates.
# (Measured: a 26.5 s prefix against the full 1011-char reference scores 89.5% —
# a fixture artefact, not an ASR result.)
#
#   Usage:  bash scripts/voice-e2e-fixture.sh [OUTDIR]
#
# OUTDIR defaults to a fresh directory under the shared scratch home
# (<home>/workspace/gopi-test-scratch/queue) — never /tmp, which is on the
# 87%-full `/` partition whose 2026-09-11 exhaustion is why dev/test scratch is
# banned there (Rule 22; the contract lives in scripts/lib/tmp-ban.sh). A
# defaulted directory is removed on exit; a caller-supplied one is left alone
# but must still be off `/` (asserted below).
#
# Cross-node use: the capture rig (scripts/voice-e2e-peer.sh) runs this fixture on
# HP and needs the files to keep existing, so it passes the SAME scratch home it
# will pass here (GOPI_TEST_SCRATCH_HOME) — the fixture path printed at the end
# is what that rig feeds to FIXTURE_WAV/FIXTURE_REF.
set -euo pipefail

readonly SOURCE_HOST=${SOURCE_HOST:-peer1}
# The fixture is a LOCAL sample the caller points at: a fixed absolute
# path only exists on one machine, so the default is a shape, not a home.
readonly SOURCE_DIR=${SOURCE_DIR:-${SOURCE_MP3:+$(dirname "$SOURCE_MP3")}}
readonly SOURCE_MP3=${SOURCE_MP3:-${SOURCE_DIR:-$PWD}/sample.mp3}
readonly SOURCE_TXT=${SOURCE_TXT:-${SOURCE_DIR:-$PWD}/sample.txt}
readonly CUT_AT=${CUT_AT:-}

readonly CAP_RATE=16000
readonly CAP_CHANNELS=1

# The shared scratch contract (off-`/` device assert + trap-time cleanup),
# sourced through this script's own directory so it works from any cwd.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

# OUTDIR: a caller-supplied path (the cross-node rig passes one) is honoured as
# given; otherwise ask the helper for a fresh scratch directory, which it
# asserts off `/` and registers for cleanup.
OUTDIR_DEFAULTED=1
if [ -n "${1:-}" ]; then
  OUTDIR_DEFAULTED=0
fi
if [ "$OUTDIR_DEFAULTED" = 1 ]; then
  OUTDIR="$(gopi_scratch_mkdir voice-e2e-fixture)" || exit 1
  readonly OUTDIR
  # Only the script's OWN default directory is removed on exit; on Ctrl-C or
  # SIGTERM too (the trap body stops background children first).
  trap gopi_scratch_cleanup EXIT INT TERM
else
  readonly OUTDIR="$1"
  mkdir -p "$OUTDIR" || exit 1
  gopi_assert_off_root_fs "$OUTDIR" || {
    # Do not leave behind the directory this run just created on the banned
    # filesystem (it is empty by construction at this point).
    rmdir "$OUTDIR" 2>/dev/null || true
    echo "FAIL: OUTDIR $OUTDIR is on the / filesystem — pass a path off / (Rule 22)" >&2
    exit 2
  }
fi

# The pinned source bytes: a different recording silently changes what the CER
# means, so the hashes are asserted, not just printed.
readonly SOURCE_MP3_SHA=${SOURCE_MP3_SHA:-a001a1023be449a0f6f5f1561dd335795e8672e7d2a7e7399242a0c9c8ce6e70}
readonly SOURCE_TXT_SHA=${SOURCE_TXT_SHA:-aea95730f5cee9045181480c3c7dbac998be1057ca90e5b8f0cdcc5806b49fb8}

get() { ssh "$SOURCE_HOST" "$1"; }

mp3_sha=$(get "sha256sum '$SOURCE_MP3' | cut -d' ' -f1")
txt_sha=$(get "sha256sum '$SOURCE_TXT' | cut -d' ' -f1")
[ "$mp3_sha" = "$SOURCE_MP3_SHA" ] || { echo "FAIL: $SOURCE_MP3 sha256=$mp3_sha != pinned $SOURCE_MP3_SHA"; exit 1; }
[ "$txt_sha" = "$SOURCE_TXT_SHA" ] || { echo "FAIL: $SOURCE_TXT sha256=$txt_sha != pinned $SOURCE_TXT_SHA"; exit 1; }

mkdir -p "$OUTDIR"
if [ -n "$CUT_AT" ]; then
  get "ffmpeg -hide_banner -loglevel error -y -t $CUT_AT -i '$SOURCE_MP3' -ar $CAP_RATE -ac $CAP_CHANNELS -c:a pcm_s16le -f wav -" > "$OUTDIR/clip.wav"
  get "head -1 '$SOURCE_TXT'" > "$OUTDIR/ref.txt"
  echo "# cut        : first ${CUT_AT}s  (reference = first line only; CER is fragment-vs-whole)"
else
  get "ffmpeg -hide_banner -loglevel error -y -i '$SOURCE_MP3' -ar $CAP_RATE -ac $CAP_CHANNELS -c:a pcm_s16le -f wav -" > "$OUTDIR/clip.wav"
  get "cat '$SOURCE_TXT'" > "$OUTDIR/ref.txt"
  echo "# cut        : none — whole recording (clip and reference are 1:1)"
fi

echo "# source mp3 : $SOURCE_HOST:$SOURCE_MP3  sha256=$mp3_sha"
echo "# source txt : $SOURCE_HOST:$SOURCE_TXT  sha256=$txt_sha"
echo "# clip.wav   : $OUTDIR/clip.wav  sha256=$(sha256sum "$OUTDIR/clip.wav" | cut -c1-32)  $(stat -c%s "$OUTDIR/clip.wav") bytes"
echo "# ref.txt    : $OUTDIR/ref.txt   sha256=$(sha256sum "$OUTDIR/ref.txt" | cut -c1-32)  $(stat -c%s "$OUTDIR/ref.txt") bytes"
echo "# OK — fixture rebuilt"
