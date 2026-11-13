#!/usr/bin/env bash
# attribute.sh — plant a live victim, watch it, run the suite with -json, and
# report the TEST whose window brackets the moment the victim disappeared.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
# shellcheck source=lib/baseline-verify.sh
. "$HERE/lib/baseline-verify.sh" || { echo "baseline-verify: scripts/lib/baseline-verify.sh missing" >&2; exit 2; }
# Derived defaults: the repo this script lives in, and its sibling
# victim worktree. Neither may name one operator home path.
HERE=$(cd "$(dirname "$0")" && pwd)
VICTIM=${VICTIM:-$HERE/../gopi-victim-acceptance}
REPO=${REPO:-$(cd "$HERE/.." && pwd)}
# The two logs are EVIDENCE, so they stay under the SHARED scratch tree (off `/`
# by measurement, never /tmp) and are NOT trap-deleted — the age GC collects
# them. No longer under the shared-memory base, whose use as a base is RETIRED
# (operator ruling, 2026-09-18).
LOGDIR="${LOGDIR:-$(gopi_scratch_mkdir attribute-victim)}" || exit 2
WATCH="$LOGDIR/victim-watch.log"
JSON="$LOGDIR/victim-json.log"
rm -f "$WATCH"
(
  alive=1
  # Bounded by wall clock — Rule 22 accepts an iteration cap OR a deadline.
  WATCH_MAX_SECS="${WATCH_MAX_SECS:-3600}"
  watch_started=$SECONDS
  while :; do
    if [ $((SECONDS - watch_started)) -ge "$WATCH_MAX_SECS" ]; then break; fi
    if [ -e "$VICTIM" ]; then
      [ "$alive" = 0 ] && { printf '%s BACK\n' "$(date +%s.%N)" >> "$WATCH"; alive=1; }
    else
      [ "$alive" = 1 ] && { printf '%s GONE\n' "$(date +%s.%N)" >> "$WATCH"; alive=0; }
    fi
    sleep 0.02
  done
) &
WATCHPID=$!
trap 'kill $WATCHPID 2>/dev/null' EXIT

# The victim IS a base: the attribution below reads its registration as evidence
# and the watch treats its presence as ground truth, so it must be asserted
# clean and AT the revision the attribution claims — an unverified base tree is
# what produced the 2026-09-19 phantom "master drift" verdict. Creation and the
# assertion are the same call, so this script cannot create one and forget it.
cd "$REPO" || exit 2
gopi_baseline_worktree_add "$VICTIM" "$(git rev-parse master)" >/dev/null || {
  echo "attribute-victim: no verified victim worktree at $(git rev-parse master) — refusing to attribute from an unasserted base" >&2
  exit 2
}
python3 - <<'PY'
import os, time
wt="$VICTIM"
gd=open(os.path.join(wt,'.git')).read().strip().split('gitdir:',1)[1].strip()
when=time.time()-600
for n in ['gitdir','HEAD','']:
    p=os.path.join(gd,n) if n else gd
    if os.path.exists(p): os.utime(p,(when,when))
PY
cd ~/workspace/gopi-repo-sandbox
go test -count=1 -timeout 20m -json ./cmd/agent/ > "$JSON" 2>&1
echo "JSON_EXIT=$?" >> "$JSON"
echo "--- watch transitions:"
cat "$WATCH"
python3 - "$WATCH" "$JSON" <<'PY'
import json, sys, datetime
watch, jlog = sys.argv[1], sys.argv[2]
gone = None
for line in open(watch):
    f = line.split()
    if len(f) >= 2 and f[1] == 'GONE':
        gone = float(f[0]); break
if gone is None:
    print("the victim never disappeared"); sys.exit(0)
spans = {}
order = []
for line in open(jlog):
    line = line.strip()
    if not line.startswith('{'): continue
    try: ev = json.loads(line)
    except Exception: continue
    t = ev.get('Time')
    if not t: continue
    ts = datetime.datetime.strptime(t[:26]+'Z', '%Y-%m-%dT%H:%M:%S.%fZ').replace(tzinfo=datetime.timezone.utc).timestamp()
    act, test = ev.get('Action'), ev.get('Test')
    if not test: continue
    if act == 'run':
        spans.setdefault(test, [ts, ts]); order.append(test)
    elif test in spans:
        spans[test][1] = ts
print("victim GONE at epoch %.3f" % gone)
hits = [(n, s, e) for n, (s, e) in spans.items() if s <= gone <= e]
for n, s, e in hits:
    print("  IN WINDOW: %s  [%.3f .. %.3f]" % (n, s, e))
if not hits:
    prev = [(n, s, e) for n, (s, e) in spans.items() if e < gone]
    prev.sort(key=lambda x: x[2])
    print("  no test window brackets it; the LAST test to finish before it:")
    for n, s, e in prev[-3:]:
        print("    %s  [%.3f .. %.3f]  (+%.3fs before)" % (n, s, e, gone - e))
PY
