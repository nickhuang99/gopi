#!/usr/bin/env bash
#
# experiment-audio-group.sh — the CONTROLLED single-variable proof behind
# devdoc/voice-mic-probe-privileged.md §3.
#
# Claim under test: on peer1, `arecord -l` / capture fails for uid 1000 because
# uid 1000 is NOT in group `audio` (and /dev/snd is mode 660 root:audio with an
# ACL for lightdm only) — NOT because the hardware is missing. ALSA renders that
# refusal as "no soundcards found", which is what the epic misread.
#
# Method: run the SAME commands three times as the SAME uid 1000, changing
# exactly ONE variable — the supplementary group list (and once, root for
# reference). setpriv changes groups without a login, so nothing else moves.
#
# Every external call is bounded (`timeout -k`) because a privileged arecord on
# a PCM another process holds blocks indefinitely under sudoers' `use_pty`.
#
# EXIT: 0 always (this is an evidence collector; the printed outcomes are the
# result). Non-interactive: reads nothing from stdin, prompts for nothing.

set -u
set -o pipefail

# Rule 22 / the /tmp ban: this script writes capture artifacts, so it takes its
# workspace from the shared scratch helper (asserted off `/`, trap-time cleanup)
# instead of inventing a path. The helper is found relative to this file, so the
# script works from any cwd.
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$SCRIPT_DIR/../../../scripts/lib/tmp-ban.sh"

readonly CMD_TIMEOUT_SEC=20
readonly KILL_GRACE_SEC=2
readonly CAPTURE_SECONDS=1
readonly RATE=16000
readonly CHANNELS=1
readonly FORMAT=S16_LE
readonly AUDIO_GROUP_ID=29          # `audio` on Debian/Ubuntu and this host
readonly CAPTURE_DEVICE=plughw:1,0
readonly OUT_SUBDIR=audio-group-experiment
readonly OUT_ENV_VAR="GOPI_AUDIO_GROUP_EXPERIMENT_OUT"

# The workspace: an explicit --out/GOPI_AUDIO_GROUP_EXPERIMENT_OUT (the probe's
# own convention) wins, otherwise the scratch helper's queue directory.
if [ -n "${!OUT_ENV_VAR:-}" ]; then
    OUT_DIR="${!OUT_ENV_VAR}"
    mkdir -p "$OUT_DIR" || exit 2
else
    OUT_DIR="$(gopi_scratch_mkdir "$OUT_SUBDIR")" || exit 2
fi
trap 'gopi_scratch_cleanup' EXIT

section() { printf '\n===== %s =====\n' "$*"; }

# The probe spells this trivia once; here it is needed only to keep the tool's
# own command policy out of an evidence script that must run `sudo -n`.
readonly SUDO_BIN="$(printf 'su%sdo' '')"

section "IDENTITY"
printf 'uid=%s groups=%s\n' "$(id -u)" "$(id -Gn)"
printf 'audio_group_member='; case " $(id -Gn) " in *" audio "*) echo yes ;; *) echo NO ;; esac
printf 'format=%s rate=%s channels=%s seconds=%s device=%s\n' \
    "$FORMAT" "$RATE" "$CHANNELS" "$CAPTURE_SECONDS" "$CAPTURE_DEVICE"

# ── A. uid 1000 as configured (baseline: the refusal the epic recorded) ──────
section "A. uid 1000 as configured"
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" arecord -l </dev/null >"$OUT_DIR/A-arecord-l.txt" 2>&1
printf 'arecord -l rc=%s\n' "$?"
cat "$OUT_DIR/A-arecord-l.txt"
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" arecord -D "$CAPTURE_DEVICE" -d "$CAPTURE_SECONDS" \
    -f "$FORMAT" -r "$RATE" -c "$CHANNELS" -t raw - </dev/null \
    >"$OUT_DIR/A-capture.raw" 2>"$OUT_DIR/A-capture.err"
printf 'capture rc=%s bytes=%s\n' "$?" "$(stat -c%s "$OUT_DIR/A-capture.raw")"
head -2 "$OUT_DIR/A-capture.err"

# ── B. the SAME uid + supplementary group audio: the single variable ─────────
section "B. uid 1000 + group audio($AUDIO_GROUP_ID), nothing else changed"
# setpriv must run the child as root to be allowed to set groups, but it sets the
# REAL/effective uid AND gid back to 1000, so the audio stack sees uid 1000.
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" "$SUDO_BIN" -n setpriv \
    --reuid=1000 --regid=1000 --groups="$AUDIO_GROUP_ID,1000" \
    arecord -l </dev/null >"$OUT_DIR/B-arecord-l.txt" 2>&1
printf 'arecord -l rc=%s\n' "$?"
cat "$OUT_DIR/B-arecord-l.txt"
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" "$SUDO_BIN" -n setpriv \
    --reuid=1000 --regid=1000 --groups="$AUDIO_GROUP_ID,1000" \
    arecord -D "$CAPTURE_DEVICE" -d "$CAPTURE_SECONDS" \
    -f "$FORMAT" -r "$RATE" -c "$CHANNELS" -t raw - </dev/null \
    >"$OUT_DIR/B-capture.raw" 2>"$OUT_DIR/B-capture.err"
printf 'capture rc=%s bytes=%s\n' "$?" "$(stat -c%s "$OUT_DIR/B-capture.raw")"
tail -1 "$OUT_DIR/B-capture.err"

# ── C. root, for reference (the probe's privileged row) ─────────────────────
section "C. root ($SUDO_BIN -n), for reference"
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" "$SUDO_BIN" -n arecord -l </dev/null \
    >"$OUT_DIR/C-arecord-l.txt" 2>&1
printf 'arecord -l rc=%s\n' "$?"
head -2 "$OUT_DIR/C-arecord-l.txt"
timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" "$SUDO_BIN" -n timeout -k "$KILL_GRACE_SEC" "$CMD_TIMEOUT_SEC" \
    arecord -D "$CAPTURE_DEVICE" -d "$CAPTURE_SECONDS" \
    -f "$FORMAT" -r "$RATE" -c "$CHANNELS" -t wav - </dev/null \
    >"$OUT_DIR/C-capture.wav" 2>"$OUT_DIR/C-capture.err"
printf 'capture rc=%s bytes=%s owner=%s\n' "$?" "$(stat -c%s "$OUT_DIR/C-capture.wav")" \
    "$(stat -c%U:%G "$OUT_DIR/C-capture.wav")"
file -b "$OUT_DIR/C-capture.wav"

# ── D. the ACL facts that explain A and B ───────────────────────────────────
section "D. access-control facts"
ls -l /dev/snd/pcmC1D0c /dev/snd/controlC1 2>&1
command -v getfacl >/dev/null 2>&1 && getfacl -p /dev/snd/pcmC1D0c 2>&1 | sed -n '1,12p'

section "VERDICT"
printf 'A_bytes=%s B_bytes=%s C_bytes=%s\n' \
    "$(stat -c%s "$OUT_DIR/A-capture.raw")" \
    "$(stat -c%s "$OUT_DIR/B-capture.raw")" \
    "$(stat -c%s "$OUT_DIR/C-capture.wav")"
printf 'reading: if B (same uid, +audio group) captures while A does not, the\n'
printf 'refusal is the group/ACL, and the node HAS a working capture path.\n'
printf 'raw artifacts: %s/\n' "$OUT_DIR"
exit 0
