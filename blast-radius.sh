#!/bin/bash
# BLAST RADIUS (epic fix/merge-rebuild-loop): which master-advancing events on
# 2026-09-16 left the binary behind, and for how long.
#
# METHOD. The Rule-12 close loop's signature is a pair of log lines:
#     [merger] [version] minted version/<stamp>-<tip>
#     [merger] [rebuild] ok: <repo> built in <dur> (<tip>)
# so a master move followed by NO mint naming it is a move whose rebuild never
# happened. Two independent sources are joined here:
#   * git reflog (timestamped on disk, immune to log interleaving) = every move
#     master actually made; and
#   * version/* tags = every position the close loop actually NAMED.
# A move whose tip is never named by a tag is a position no rebuild was ever
# performed for.
#
# It also prints the same window from the log side, because the log is the
# operator's evidence: merger.log has NOTHING between 04:31Z and 11:44 local
# (06:35-11:44 local = the window the two dead daemons covered), so the moves in
# that window have no close-loop trace AT ALL.
#
# Run against the MAIN repo (it reads history, writes nothing).
set -u
# The repo is DERIVED from the checkout this script sits in: a fixed path
# would make the tool runnable on exactly one machine.
REPO="${1:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null || pwd)}"
cd "$REPO" || exit 1
DAY="${2:-2026-09-16}"

# Temp data goes OFF the `/` partition (GOPI.md Rule 22): the two join files are
# taken from the shared scratch tree (the SAME tree the Go suites and the merger
# use), never from a bare `/tmp` literal. `.`-sourcing the shared contract keeps
# ONE reader of the scratch-home variables (Rule 2).
# shellcheck source=scripts/lib/tmp-ban.sh
. "$(dirname "$0")/scripts/lib/tmp-ban.sh"
WORK="$(gopi_scratch_mkdir blast-radius)" || exit 1
trap gopi_scratch_cleanup EXIT INT TERM
MINTS="$WORK/mints.txt"; MOVES="$WORK/moves.txt"

echo "############ A. every position the close loop NAMED (version/* tags) ############"
git for-each-ref --sort=creatordate \
  --format='%(creatordate:iso-local)  %(refname:short)  ->  %(objectname:short)' refs/tags/version \
  | awk -v d="$DAY" '$1 ~ "^"d'

echo
echo "############ B. named tips vs master's real movement ($DAY) ############"
git tag -l 'version/*' --format='%(creatordate:unix) %(refname:short)' | sort -n > "$MINTS"
printf '%-20s %-9s %s\n' TIME TIP "ACTION + the last position the close loop NAMED before it"
git reflog --date=iso -150 \
  | sed -nE 's/^([0-9a-f]+) HEAD@\{([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}) ([+-][0-9]{4})\}: (merge|reset|commit)(.*)$/\2 \3|\1|\5\6/p' \
  | grep "^$DAY" > "$MOVES"
while IFS='|' read -r ts sha what; do
  t=$(date -d "$ts" +%s 2>/dev/null) || continue
  last=$(awk -v t="$t" '$1 <= t {e=$1; n=$2} END{if (e) print e" "n}' "$MINTS")
  if [ -z "$last" ]; then mark='NO mint before it'; else
    le=${last%% *}; ln=${last#* }
    if [ $((t - le)) -gt 900 ]; then mark="(last mint $ln was $(( (t-le)/60 ))m earlier)"; else mark="named by $ln"; fi
  fi
  printf '%-20s %-9s %-8s %s\n' "$ts" "$sha" "$what" "$mark"
done < "$MOVES"

echo
echo "############ C. the log side: close-loop lines around the gap ############"
L="${GOPI_MERGER_LOG:-$HOME/.gopi/logs/merger.log}"
if [ -f "$L" ]; then
  echo "-- last close-loop pair before the gap --"
  grep -n '\[merger\] \[version\] minted\|\[merger\] \[rebuild\] ok' "$L" | sed -e 's/\(.\{150\}\).*/\1/' | awk -F: '$1<20270' | tail -4
  echo "-- first close-loop pair after the gap --"
  grep -n '\[merger\] \[version\] minted\|\[merger\] \[rebuild\] ok' "$L" | sed -e 's/\(.\{150\}\).*/\1/' | awk -F: '$1>20270' | head -6
  echo "-- total lines of merger.log --"
  wc -l < "$L"
fi
