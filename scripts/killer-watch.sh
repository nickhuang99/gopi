#!/usr/bin/env bash
# killer-watch.sh — catch the process that runs `git worktree remove` and
# identify its parent (the spawned gopi) and that parent's cwd. This is how a
# deletion is attributed to the TEST whose child did it, without guessing.
#
# Usage: killer-watch.sh <log-path> &
#
# The default log location is a directory under the SHARED scratch tree (off the
# `/` device, asserted below) — never /tmp, and no longer the shared-memory
# filesystem: creating there is RETIRED (operator ruling, 2026-09-18).
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
LOGDIR="${LOGDIR:-$(gopi_scratch_mkdir killer-watch)}" || exit 2
LOG="${1:-$LOGDIR/killer.log}"
: > "$LOG"
# Bounded by wall clock — Rule 22 accepts an iteration cap OR a deadline.
WATCH_MAX_SECS="${WATCH_MAX_SECS:-3600}"
watch_started=$SECONDS
while :; do
  if [ $((SECONDS - watch_started)) -ge "$WATCH_MAX_SECS" ]; then break; fi
  # ps walks /proc itself; one line per process, cheap enough at ~20 polls/s.
  ps -eo pid=,ppid=,args= 2>/dev/null | grep -E 'git .*worktree (remove|prune)' | grep -v grep | while read -r pid ppid rest; do
    {
      printf '=== %s KILLER pid=%s ppid=%s cmd=%s\n' "$(date +%s.%N)" "$pid" "$ppid" "$rest"
      printf '    parent cmd: '; tr '\0' ' ' < "/proc/$ppid/cmdline" 2>/dev/null; printf '\n'
      printf '    parent cwd: '; readlink "/proc/$ppid/cwd" 2>/dev/null; printf '\n'
      printf '    parent env HOME=%s GOPI_HOME=%s: ' "$(tr '\0' '\n' < /proc/$ppid/environ 2>/dev/null | sed -n 's/^HOME=//p')" "$(tr '\0' '\n' < /proc/$ppid/environ 2>/dev/null | sed -n 's/^GOPI_HOME=//p')"; printf '\n'
      # The test that owns that child: gopi processes started by `go test` carry
      # the test binary in their ancestry; print the whole chain.
      cur="$ppid"
      for _ in 1 2 3 4 5; do
        pcmd=$(tr '\0' ' ' < "/proc/$cur/cmdline" 2>/dev/null)
        [ -z "$pcmd" ] && break
        printf '    chain: pid=%s cmd=%s\n' "$cur" "$pcmd"
        cur=$(ps -o ppid= -p "$cur" 2>/dev/null | tr -d ' ')
        [ -z "$cur" ] || [ "$cur" = 1 ] && break
      done
    } >> "$LOG"
  done
  sleep 0.05
done
