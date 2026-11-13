#!/usr/bin/env bash
# verify-rehome.sh — a SELF-CONTAINED verification rig for
# scripts/rehome-signal-provenance.sh. It is NOT part of the shipped script: it
# builds a fixture gopi home in a temp dir, runs the migration dry-run and then
# for real, and prints the before/after facts a reviewer checks. Run it with
# `bash scripts/verify-rehome.sh` from the repo root.
#
# The rig's value is that it exercises the THREE classification priorities and
# the unattributable path in one pass:
#   A. a record with task_run whose session dir EXISTS      → filed
#   B. a record with session whose session dir EXISTS       → filed
#   C. a record with task_run whose session dir is GONE     → unattributable
#   D. a record with NO id fields at all                    → unattributable
#
# Its fixture home is scratch: it lives under the shared scratch home
# (<home>/workspace/gopi-test-scratch/queue), asserted off `/` by device id and
# removed by the trap — never /tmp, which is on the 87%-full `/` partition the
# 2026-09-11 exhaustion filled (Rule 22; see scripts/lib/tmp-ban.sh).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

FIX="$(gopi_scratch_mkdir verify-rehome)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
HOME_DIR="$FIX/home"
trap gopi_scratch_cleanup EXIT INT TERM

mkdir -p "$HOME_DIR/.gopi/logs" \
         "$HOME_DIR/.gopi/sessions/task-A" \
         "$HOME_DIR/.gopi/sessions/sess-B" \
         "$HOME_DIR/.gopi/sessions/gone-C"
: > "$HOME_DIR/.gopi/sessions/task-A/metadata.json"
: > "$HOME_DIR/.gopi/sessions/sess-B/session.jsonl"

cat > "$HOME_DIR/.gopi/logs/signal-provenance.log" <<'EOF'
{"ts":"2026-09-13T08:05:07+08:00","kind":"signal","signal":"SIGTERM","role":"worker","task_run":"task-A"}
{"ts":"2026-09-13T08:06:07+08:00","kind":"intent","kind_detail":"x","session":"sess-B"}
{"ts":"2026-09-13T08:07:07+08:00","kind":"signal","signal":"SIGTERM","role":"worker","task_run":"gone-C"}
{"ts":"2026-09-13T08:08:07+08:00","kind":"signal","signal":"SIGTERM","role":"unknown"}
EOF

echo "=== fixture: 4 records (2 attributable, 1 gone dir, 1 no id) ==="
wc -l < "$HOME_DIR/.gopi/logs/signal-provenance.log"

echo "=== dry-run (must change nothing) ==="
bash scripts/rehome-signal-provenance.sh --home "$HOME_DIR/.gopi" --dry-run | tail -6
echo "source still present after dry-run: $(test -f "$HOME_DIR/.gopi/logs/signal-provenance.log" && echo yes || echo NO)"

echo "=== apply ==="
bash scripts/rehome-signal-provenance.sh --home "$HOME_DIR/.gopi" --apply 2>&1 | tail -12

echo "=== destination A (task-keyed) ==="
cat "$HOME_DIR/.gopi/sessions/task-A/signal-provenance.jsonl"
echo "=== destination B (session-keyed) ==="
cat "$HOME_DIR/.gopi/sessions/sess-B/signal-provenance.jsonl"
echo "=== gone-C dir must NOT have been created ==="
test -e "$HOME_DIR/.gopi/sessions/gone-C/signal-provenance.jsonl" && echo "FAIL: invented a dir" || echo "ok: no invented dir"
echo "=== source kept (unattributable lines present, no --discard-unattributable) ==="
test -f "$HOME_DIR/.gopi/logs/signal-provenance.log" && echo "ok: source kept for the operator" || echo "FAIL: source removed with lines unattributed"
echo "=== the public sink file is NOT written as a destination anywhere ==="
find "$HOME_DIR/.gopi/sessions" -name 'signal-provenance.jsonl' -printf '%p\n'
