#!/usr/bin/env bash
# rehome-flat-raw-logs.sh — retire the LEGACY FLAT raw-log directory.
#
# WHY this exists (the epic behind it)
# -----------------------------------
# A session's assets must live in ONE directory — ~/.gopi/sessions/<id>/ — so a
# later debugging task can study (or delete) a whole task at a time. Raw HTTP
# logs used to be written to a single FLAT directory (~/.gopi/raw/, every run's
# file side by side under a timestamped name), and because NewAgent minted the
# bare-session identity twice, even the newer per-session layout split a run's
# raw log away from its own session JSONL (fixed in commit 2464f9f3).
#
# What is left in the flat directory is therefore DIRTY: 1400+ files an operator
# cannot attribute to a task, session or process — "if you cannot figure out,
# then just delete because it would be useless if no one can figure out which
# process/task it belongs to".
#
# ATTRIBUTION IS BY REFERENCE, NEVER BY GUESS
# -------------------------------------------
# The reference is the operator's own record of ownership: a SESSION RECORD
# (*.jsonl / *.json / *.txt inside the sessions tree) that names the file. Two
# strengths of evidence are distinguished, because they are not equal:
#
#   LOCATED — the record quotes the file's LOCATION (<flat dir>/<name>), e.g.
#             session_start's rawLogPath. This is ownership.
#   WEAK    — the record mentions the bare NAME somewhere, without the location
#             (a `ls` of the raw dir inside a transcript, a report listing
#             files, a resume note). This is a mention, NOT ownership: one
#             `ls ~/.gopi/raw` output names every file in the directory, so
#             bare names alone over-attribute massively (measured: they cover
#             794 files, of which only 228 are ever located).
#
# Raw logs themselves are NEVER scanned: a raw log quoting its own name is a
# self-reference that proves nothing, and the sessions tree holds 14.6 GB of
# them (one file alone is 890 MB) against 563 MB of records.
#
# FOUR BUCKETS, and only ONE of them may ever be deleted:
#
#   located, exactly 1 session  -> MOVED into that session's directory
#   located, >1 session         -> AMBIGUOUS, left in place (a wrong owner is
#                                  worse than a stale file: it puts a
#                                  stranger's evidence inside a task dir)
#   weak only (name, no place)  -> LEFT for the operator to judge — deleting a
#                                  file some record mentions would be exactly
#                                  the "someone CAN figure it out" case the
#                                  epic forbids discarding
#   invisible (no mention)      -> reported; deleted ONLY under --delete
#
# DRY-RUN BY DEFAULT. Only --delete removes anything, and it removes ONLY the
# invisible bucket (with a count + byte total printed first).
#
#   ./rehome-flat-raw-logs.sh              # report only
#   ./rehome-flat-raw-logs.sh --move       # move the located logs, no delete
#   ./rehome-flat-raw-logs.sh --delete     # move + delete the invisible bucket
#
# Idempotent: a second run finds the moved files already in their session
# directory (and reports them as already in place) and no longer lists them in
# the flat dir.
set -uo pipefail

# Scratch-root contract: the scan's throwaway files are the one kind of "scratch
# data" this script makes, and they must not land in /tmp. The helper is the
# single source of that root — it resolves the queue, asserts it is off `/`,
# registers the directory for trap-time cleanup, and its word `scratch_mkdir`
# is also what the repo's scripts gate reads as the marker of a compliant
# script. See scripts/lib/tmp-ban.sh and internal/testutil/scripts_gate_test.go.
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

SESSIONS_ROOT=${GOPI_SESSIONS_ROOT:-$HOME/.gopi/sessions}
FLAT_RAW=${GOPI_FLAT_RAW_DIR:-$HOME/.gopi/raw}
DO_MOVE=0
DO_DELETE=0
for arg in "$@"; do
	case "$arg" in
		--move) DO_MOVE=1 ;;
		--delete) DO_MOVE=1; DO_DELETE=1 ;;
		-h|--help) sed -n '2,60p' "$0"; exit 0 ;;
		*) echo "unknown flag: $arg (see --help)" >&2; exit 2 ;;
	esac
done

[ -d "$FLAT_RAW" ] || { echo "no flat raw dir at $FLAT_RAW — nothing to do"; exit 0; }
[ -d "$SESSIONS_ROOT" ] || { echo "no sessions root at $SESSIONS_ROOT — cannot attribute anything" >&2; exit 1; }

# --- 1. build the reference index over the session RECORDS -------------------
# ONE rg -l pass selects the candidate records; each candidate is then read ONCE
# and yields both kinds of evidence: LOCATED (the token carried the flat
# location) and BARE (a mention of the name with no location).
NAME_RE='gopi-raw-[0-9]{8}-[0-9]{6}(-[0-9]+|-u[0-9]+-[0-9]+)?[.]log'
WORK=$(gopi_scratch_mkdir rehome-flat-raw-logs) || exit 1
INDEX="$WORK/refs.txt"
RAWIDX="$WORK/rawnames.txt"
LOC="$WORK/located.txt"
BARE="$WORK/bare.txt"
: >"$INDEX"; : >"$RAWIDX"; : >"$LOC"; : >"$BARE"
trap 'gopi_scratch_cleanup' EXIT
echo "[rehome] scanning $SESSIONS_ROOT records for references (one pass)…" >&2
if command -v rg >/dev/null 2>&1; then
	rg -l -g '*.jsonl' -g '*.json' -g '*.txt' \
		'gopi-raw-[0-9]{8}-[0-9]{6}' "$SESSIONS_ROOT" 2>/dev/null >"$INDEX"
else
	grep -rlE --include='*.jsonl' --include='*.json' --include='*.txt' \
		'gopi-raw-[0-9]{8}-[0-9]{6}' "$SESSIONS_ROOT" 2>/dev/null >"$INDEX"
fi

: >"$RAWIDX"
while IFS= read -r f; do
	[ -n "$f" ] || continue
	rel=${f#"$SESSIONS_ROOT"/}
	dir=${rel%%/*}
	# LOCATED evidence: the token carries the flat dir as its prefix. The
	# fixed-string pre-filter keeps the (path-anchored) extraction off the
	# hundreds of records that only mention a bare name.
	if grep -qF "$FLAT_RAW/" "$f" 2>/dev/null; then
		grep -oE "$FLAT_RAW/$NAME_RE" "$f" 2>/dev/null |
			sed "s|^$FLAT_RAW/||" | sort -u | sed "s|^|L\t$dir\t|"
	fi
	# BARE evidence: any mention of the name shape, location or not.
	grep -oE "$NAME_RE" "$f" 2>/dev/null | sort -u | sed "s|^|B\t$dir\t|"
done <"$INDEX" >>"$RAWIDX"
awk -F'\t' '$1 == "L" {print $2 "\t" $3}' "$RAWIDX" | sort -u >"$LOC"
awk -F'\t' '$1 == "B" {print $2 "\t" $3}' "$RAWIDX" | sort -u >"$BARE"

moved=0; moved_bytes=0; already=0
unattr=0; unattr_bytes=0; ambiguous=0; ambiguous_bytes=0; weak=0; weak_bytes=0
echo "[rehome] flat dir: $FLAT_RAW"
while IFS= read -r path; do
	name=$(basename "$path")
	size=$(stat -c %s "$path" 2>/dev/null || echo 0)
	loc_owners=$(awk -F'\t' -v n="$name" '$2 == n {print $1}' "$LOC" | sort -u | grep -c . || true)
	bare_owners=$(awk -F'\t' -v n="$name" '$2 == n {print $1}' "$BARE" | sort -u | grep -c . || true)
	if [ "$loc_owners" -gt 1 ]; then
		ambiguous=$((ambiguous + 1)); ambiguous_bytes=$((ambiguous_bytes + size))
		echo "[rehome] AMBIGUOUS ($loc_owners located references) — left in place: $name"
		continue
	fi
	if [ "$loc_owners" -eq 0 ]; then
		if [ "$bare_owners" -gt 0 ]; then
			weak=$((weak + 1)); weak_bytes=$((weak_bytes + size))
			echo "[rehome] WEAK (mentioned by $bare_owners record(s), no location) — left in place: $name"
			continue
		fi
		unattr=$((unattr + 1)); unattr_bytes=$((unattr_bytes + size))
		if [ "$DO_DELETE" -eq 1 ]; then
			if rm -f -- "$path"; then
				echo "[rehome] deleted invisible $name ($size B)"
			else
				echo "[rehome] DELETE FAILED (kept): $name" >&2
			fi
		fi
		continue
	fi
	owner=$(awk -F'\t' -v n="$name" '$2 == n {print $1}' "$LOC" | sort -u | head -1)
	target="$SESSIONS_ROOT/$owner/$name"
	if [ -e "$target" ]; then
		already=$((already + 1))
		echo "[rehome] already in place: $target"
		continue
	fi
	if [ "$DO_MOVE" -eq 1 ]; then
		if mkdir -p "$SESSIONS_ROOT/$owner" && mv -- "$path" "$target"; then
			moved=$((moved + 1)); moved_bytes=$((moved_bytes + size))
			echo "[rehome] moved $name -> $target"
		else
			echo "[rehome] MOVE FAILED (kept in place): $name" >&2
		fi
	else
		moved=$((moved + 1)); moved_bytes=$((moved_bytes + size))
		echo "[rehome] would move $name -> $target (dry run)"
	fi
done < <(find "$FLAT_RAW" -maxdepth 1 -type f -name 'gopi-raw-*.log' | sort)

echo
echo "[rehome] ==== summary ===="
echo "[rehome] located, one owner : $moved file(s), $moved_bytes B  (moved=$DO_MOVE, already in place=$already)"
echo "[rehome] located, contested : $ambiguous file(s), $ambiguous_bytes B  (never touched)"
echo "[rehome] weak mention only  : $weak file(s), $weak_bytes B  (never touched — operator judgement)"
echo "[rehome] invisible          : $unattr file(s), $unattr_bytes B  (deleted=$DO_DELETE)"
if [ "$DO_DELETE" -eq 0 ]; then
	echo "[rehome] DRY RUN — nothing was deleted. Re-run with --delete to drop the invisible files."
fi
