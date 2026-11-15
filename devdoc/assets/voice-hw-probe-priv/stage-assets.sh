#!/usr/bin/env bash
# stage the committed evidence for the privileged-rework artifacts.
# A file-listing driver would re-run the probe; this only classifies/moves files
# that already exist (no capture, no /dev/snd access).
set -u
set -o pipefail
readonly SRC="${1:?source probe out dir required}"
readonly DST="${2:?destination artifact dir required}"
mkdir -p "$DST"
if [ ! -d "$SRC" ]; then
    printf 'stage-voice-priv-assets: no such source dir: %s\n' "$SRC" >&2
    exit 2
fi
# Committed: the machine-readable table, every .err transcript, the listings,
# and the capture WAVs that actually held data (an empty WAV is a refusal already
# described by its .err).
for f in "$SRC"/*.tsv "$SRC"/*.txt "$SRC"/*.err; do
    [ -e "$f" ] || continue
    cp -f "$f" "$DST/"
done
for f in "$SRC"/capture-*.wav; do
    [ -e "$f" ] || continue
    if [ -s "$f" ]; then cp -f "$f" "$DST/"; fi
done
printf 'staged %s files into %s\n' "$(ls -1 "$DST" | wc -l)" "$DST"
