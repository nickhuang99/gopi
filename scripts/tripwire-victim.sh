#!/usr/bin/env bash
# tripwire.sh — attribute the victim's disappearance to the TEST whose window it
# vanished in. A goroutine-free design: the victim is watched by a separate
# process, and the watcher's log is correlated with `go test -v`'s own lines.
#
# Usage: tripwire.sh <victim-path> <repo>
set -u
VICTIM="$1"
REPO="$2"
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
# The two logs live under the SHARED scratch tree — off the `/` device by
# measurement, never /tmp, and no longer under the shared-memory filesystem
# whose use as a base is RETIRED (operator ruling, 2026-09-18). They are
# EVIDENCE, so nothing here removes them: the shared age GC collects them.
LOGDIR="${LOGDIR:-$(gopi_scratch_mkdir tripwire)}" || exit 2
WATCHLOG="$LOGDIR/gopi-victim-watch.log"
TESTLOG="$LOGDIR/gopi-victim-test.log"

rm -f "$WATCHLOG" "$TESTLOG"
# The watcher: sample the victim every 20ms, timestamp each transition.
(
  alive=1
  # Bounded by wall clock — Rule 22 accepts an iteration cap OR a deadline.
  WATCH_MAX_SECS="${WATCH_MAX_SECS:-3600}"
  watch_started=$SECONDS
  while :; do
    if [ $((SECONDS - watch_started)) -ge "$WATCH_MAX_SECS" ]; then break; fi
    if [ -e "$VICTIM" ]; then
      if [ "$alive" = 0 ]; then printf '%s BACK\n' "$(date +%s.%N)" >> "$WATCHLOG"; alive=1; fi
    else
      if [ "$alive" = 1 ]; then printf '%s GONE\n' "$(date +%s.%N)" >> "$WATCHLOG"; alive=0; fi
    fi
    sleep 0.02
  done
) &
WATCHPID=$!
trap 'kill $WATCHPID 2>/dev/null' EXIT

cd "$(dirname "$0")/.." || exit 1
go test -count=1 -v -timeout 20m ./cmd/agent/ > "$TESTLOG" 2>&1
echo "TEST_EXIT=$?"
echo "--- transitions:"
cat "$WATCHLOG"
echo "--- last 40 test lines before the first GONE:"
gone=$(grep -m1 ' GONE$' "$WATCHLOG" | awk '{print $1}')
if [ -n "$gone" ]; then
  awk -v t="$gone" 'match($0, /^=== RUN|^--- (PASS|FAIL|SKIP)|^    --- (PASS|FAIL)/) {print}' "$TESTLOG" | tail -40
  echo "--- the RUN line whose window contains the first GONE:"
  awk 'BEGIN{last=""} /^=== RUN/{last=$0} END{}' "$TESTLOG" >/dev/null
  grep -n '^=== RUN' "$TESTLOG" | tail -20
fi
