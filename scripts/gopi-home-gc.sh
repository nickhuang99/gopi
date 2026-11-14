#!/usr/bin/env bash
# gopi-home-gc.sh — retention / cluttering sweep for the gopi profile
# (~/.gopi, or $GOPI_HOME/.gopi).
#
# WHY THIS EXISTS (epic 2026-09-12 "worker create logs not within designate
# path"): the profile root holds ad-hoc, never-pruned artifacts. Measured on
# peer1 before this script existed: 647 MB of *.db* under ~/.gopi, 19
# memory.db* files (each up to 83 MB) — `memory.db.bak-latprov-*`,
# `memory.db.peer1-backup-*`, `memory.db.after-quality-fix`, a `memory-glm-audit-*.db`
# — plus orphan `-shm`/`-wal` sidecars whose base DB had already been deleted
# and zero-byte stubs left behind by abandoned runs. Nothing in the repo ever
# created or pruned them: every one is a hand-typed `cp` snapshot (grep for
# "memory.db.bak" in Go: no producer), so there was no retention anywhere.
#
# WHAT IT DOES — four classes, each provably safe:
#   ① RELOCATE root clutter out of the profile root: root-level `memory.db.*`
#      / `memory-*.db` snapshots move into <root>/backups/, and stray
#      `worker-*.log` files (this launcher's old default wrote them at the
#      root) move into <root>/logs/. Moving, never deleting.
#   ② PRUNE the backups directory: keep the newest --keep snapshots AND
#      anything younger than --age days; delete the rest (a sidecar follows its
#      base). ② judges the POST-RELOCATION view — the current backups/ contents
#      plus everything ① is about to move in — so ONE run both homes a stray
#      snapshot and holds it to the retention rule (see the ordering note at ②).
#   ②b DEDUPE the survivors: byte-identical generations keep only their newest
#      copy (detected by content hash, and only where two files share a size).
#      Snapshots are 48-83 MB and a batch loop produces dozens per night. On
#      the HP peer 13 such files shared one size (48488448) and held only 10
#      distinct states, so 3 exact copies (138.7 MiB) were pure redundancy the
#      age rule would have carried for the full window.
#   ③ DELETE orphan sidecars (`<X>-wal` / `<X>-shm` whose `<X>` is gone) and
#      zero-byte DB stubs — files that can hold no data by construction.
#   ④ REPORT everything else in the root with unknown provenance (other .db
#      files, ops logs like nightwatch.log, dumps/) and touch NOTHING. The
#      sweep's contract is that it only acts where ownership is provable.
#
# NEVER TOUCHED: the live database <root>/memory.db and its -wal/-shm. Every
# candidate path is compared against the live DB's realpath and refused on
# match — a hygiene sweep that can delete the live DB is worse than the
# clutter it removes.
#
# Usage:  scripts/gopi-home-gc.sh [--apply] [--keep N] [--age DAYS]
#                                 [--home DIR] [--install]
#   (no --apply) dry-run: print the plan, change NOTHING — the default, so a
#                first run is always free to be wrong.
#   --apply      execute the plan.
#   --keep N     keep the newest N backups (default 5; 0 = age rule only).
#   --age DAYS   keep backups younger than DAYS (default 14).
#   --home DIR   sweep DIR as the gopi root instead of the resolved one
#                (testing seam; also explicit for a relocated profile).
#   --install    install a stable copy in ~/.local/bin and schedule --apply
#                weekly (systemd --user timer, or cron where no user bus
#                exists) — "cleaned up regularly" needs a clock, not a habit.
set -euo pipefail

APPLY=0
KEEP=5
AGE_DAYS=14
ROOT=""
INSTALL=0

usage() { sed -n '2,50p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while (($#)); do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --install) INSTALL=1; shift ;;
    --apply) APPLY=1; shift ;;
    --keep) KEEP="${2:?--keep needs a number}"; shift 2 ;;
    --age) AGE_DAYS="${2:?--age needs a number}"; shift 2 ;;
    --home) ROOT="${2:?--home needs a directory}"; shift 2 ;;
    *) echo "gopi-home-gc: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# --install: make the sweep RUN ON A CLOCK (the epic's "clean up regularly").
# Same shape as scripts/install-scratch-gc-watchdog.sh — a stable copy in
# ~/.local/bin (a worktree's scripts/ comes and goes; a timer must not point
# into one), a systemd --user service+timer when the user bus exists, cron
# otherwise. The timer runs --apply: a scheduled sweep that only planned would
# never reclaim anything.
install_unit() {
  local stable_dir="$HOME/.local/bin"
  local stable="$stable_dir/gopi-home-gc.sh"
  mkdir -p "$stable_dir"
  # Re-installing FROM the installed copy is the normal path on a host that has
  # no scheduled sweep yet (you run the script you just copied). `install` fails
  # hard on "are the same file", and under set -e that aborted the whole
  # --install before the cron entry was written — the clock silently never got
  # installed (measured on the HP peer). Same file = nothing to copy.
  if [[ "$(readlink -f "${BASH_SOURCE[0]}")" == "$(readlink -f "$stable" 2>/dev/null || printf '%s' "$stable")" ]]; then
    echo "already installed: $stable"
  else
    install -m 0755 "${BASH_SOURCE[0]}" "$stable"
    echo "installed: $stable"
  fi
  local marker="# gopi-home-gc (weekly, installed by scripts/gopi-home-gc.sh --install)"
  if [[ -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" ]] && command -v systemctl >/dev/null 2>&1; then
    local unit_dir="$HOME/.config/systemd/user"
    mkdir -p "$unit_dir"
    cat >"$unit_dir/gopi-home-gc.service" <<EOF
[Unit]
Description=GoPi profile hygiene — relocate/prune ~/.gopi clutter (dry-run: ${stable} )

[Service]
Type=oneshot
ExecStart=${stable} --apply
Nice=10
IOSchedulingClass=idle
EOF
    cat >"$unit_dir/gopi-home-gc.timer" <<'EOF'
[Unit]
Description=GoPi profile hygiene — weekly sweep

[Timer]
OnBootSec=30min
OnUnitActiveSec=7d
Persistent=true
AccuracySec=5min
Unit=gopi-home-gc.service

[Install]
WantedBy=timers.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable --now gopi-home-gc.timer
    echo "installed (systemd --user): $unit_dir/gopi-home-gc.{service,timer}"
    systemctl --user list-timers gopi-home-gc.timer --no-pager 2>/dev/null | head -3
  elif command -v crontab >/dev/null 2>&1; then
    local entry="17 4 * * 0 ${stable} --apply >/dev/null 2>&1"
    local kept
    kept="$(crontab -l 2>/dev/null | grep -v -F "$marker" | grep -v -F "$stable" || true)"
    { [[ -n "$kept" ]] && printf '%s\n' "$kept"; printf '%s %s\n' "$entry" "$marker"; } | crontab -
    echo "installed (cron, Sundays 04:17): $entry"
  else
    echo "gopi-home-gc: no systemd --user bus and no crontab — run it from your own scheduler" >&2
    return 1
  fi
}

if ((INSTALL)); then
  install_unit
  exit $?
fi

if [[ -z "$ROOT" ]]; then
  if [[ -n "${GOPI_HOME:-}" ]]; then ROOT="${GOPI_HOME%/}/.gopi"; else ROOT="$HOME/.gopi"; fi
fi

[[ -d "$ROOT" ]] || { echo "gopi-home-gc: no such gopi root: $ROOT" >&2; exit 1; }

LIVE="$ROOT/memory.db"
BACKUPS="$ROOT/backups"
LOGS="$ROOT/logs"

# One run at a time (mirrors scratch-gc-watchdog.sh): two concurrent sweeps
# would race the same plan against a shifting directory. A DRY RUN takes no
# lock: it writes nothing, so it cannot race destructively — and creating the
# lock would itself be a write during a run that promises to change nothing.
if ((APPLY)); then
  if command -v flock >/dev/null 2>&1; then
    exec 9>"$ROOT/.gopi-home-gc.lock"
    if ! flock -n 9; then echo "gopi-home-gc: another sweep holds the lock — exiting"; exit 0; fi
  else
    echo "gopi-home-gc: warning: flock unavailable — running unserialised" >&2
  fi
fi

live_real="$(readlink -f "$LIVE" 2>/dev/null || echo "$LIVE")"
moves=()      # src|dst
deletes=()    # path
reports=()    # path
dedupes=()    # dst|kept_as  (byte-identical generations dropped by ②b)
freed=0

is_live() {
  local p_real
  p_real="$(readlink -f "$1" 2>/dev/null || echo "$1")"
  [[ "$p_real" == "$live_real" || "$1" == "$LIVE" || \
     "$1" == "$LIVE-wal" || "$1" == "$LIVE-shm" ]]
}

# A snapshot ① relocates may be pruned by ② in the SAME run (that is the whole
# point of the ordering above). Two consequences: its bytes must be counted
# through its SOURCE (the destination does not exist yet), and the `mv` hop is
# pointless — the delete below removes it either way. planned_src_of maps a
# planned destination back to the file that holds the data right now; is_deleted
# answers whether a planned delete covers a path. Both are defined BEFORE the ①
# loop: ② and ②b call them while the plan is still being built.
planned_src_of() {
  local p="$1" m
  for m in "${moves[@]:-}"; do
    [[ -n "$m" ]] || continue
    [[ "${m##*|}" == "$p" ]] && { printf '%s' "${m%%|*}"; return; }
  done
  printf '%s' "$p"
}
is_deleted() {
  local p="$1" d
  for d in "${deletes[@]:-}"; do
    [[ -n "$d" ]] || continue
    [[ "$d" == "$p" ]] && return 0
  done
  return 1
}

# A memory-DB SNAPSHOT, wherever it sits (root or backups/): a memory database
# with something appended — `memory.db.bak-latprov-20260911-125436`,
# `memory-glm-audit.db.bak-batch06-20260910-043837`, `memory.db.after-quality-fix`,
# `memory-glm-audit.db.bak-batch01`, and the plain `memory-*.db` audit copies.
# ONE predicate for both ① (which files are snapshots) and ② (which backups are
# prunable generations): when the two disagreed, the HP root kept a 13-file
# 48 MB `memory-glm-audit.db.bak-*` files that the sweep reported as "unknown
# provenance" and never touched.
#
# NOTE it does NOT match the live DB (`memory.db`), that is checked separately by
# is_live, and it does not match the sidecar shape `X.bak-...-wal`.
is_snapshot() {
  local n="$1"
  [[ "$n" == memory.db.* || "$n" == memory-*.db || "$n" == *.db.bak* ]]
}

# ① + ③: classify every entry in the ROOT (top level only — never recurse, the
# sessions/ tree has 28k directories and is not this sweep's business).
while IFS= read -r -d '' f; do
  name="$(basename "$f")"
  is_live "$f" && continue
  # The sweep's own lock (created only by --apply) is not clutter.
  [[ "$name" == .gopi-home-gc.lock ]] && continue
  # worker logs belong in logs/, not next to memory.db.
  if [[ "$name" == worker-*.log ]]; then
    moves+=("$f|$LOGS/$name"); continue
  fi
  # An orphan sidecar (its base is already gone) is garbage wherever it sits:
  # it cannot be a backup generation and no DB can attach to it.
  if [[ "$name" == *-wal || "$name" == *-shm ]]; then
    base="${name%-wal}"; base="${base%-shm}"
    if [[ ! -e "$ROOT/$base" ]]; then deletes+=("$f"); continue; fi
  fi  # Zero-byte stubs can hold nothing: safe to drop (this catches the
  # abandoned distilled.db / gopi.db / tasks.db trio and empty sidecars).
  if [[ ! -s "$f" && "$name" == *.db* ]]; then
    deletes+=("$f"); continue
  fi
  # DB snapshots at the root belong in backups/. `memory.db.*` / `memory-*.db`
  # was too narrow: it MISSED `memory-glm-audit.db.bak-*` — a 13-file 48 MB
  # family (646 MB) found sitting at the HP profile root, invisible to the
  # sweep on its first real run. A snapshot is the same shape wherever it lives,
  # so ① and ② share ONE predicate: `is_snapshot` above.
  if is_snapshot "$name"; then
    moves+=("$f|$BACKUPS/$name"); continue
  fi
  reports+=("$f")
done < <(find "$ROOT" -maxdepth 1 -type f -print0)

# ②: prune the backups directory (and drop sidecars whose base is being pruned,
# plus orphans whose base is already gone).
#
# ORDER MATTERS, and it is not obvious: the ① relocations above are planned,
# not yet executed, so the snapshots they would move are still at the profile
# ROOT when this runs. Pruning only the CURRENT backups/ contents therefore
# makes a first sweep a two-step: it homes the clutter this run and leaves it
# untouched until the NEXT weekly sweep (measured: a sandbox with 4 root
# snapshots, 3 older than the age rule → run 1 relocated 4, pruned 0; run 2
# pruned 3). So prune the POST-RELOCATION view: the current backups/ contents
# plus every snapshot ① scheduled to land there. For a root snapshot the
# relocation preserves its mtime (① below moves with `mv -p`, and a same-
# filesystem mv already keeps it), so it is aged by the age it actually has,
# not by the moment it was swept.
mapfile -t db_backups < <(
  { find "$BACKUPS" -maxdepth 1 -type f -printf '%T@ %p\n' 2>/dev/null \
      | while read -r ts p; do is_snapshot "$(basename "$p")" && printf '%s %s\n' "$ts" "$p"; done
    for m in "${moves[@]:-}"; do
      [[ -n "$m" ]] || continue
      src="${m%%|*}"; dst="${m##*|}"
      [[ "$dst" == "$BACKUPS/"* ]] || continue
      printf '%s %s\n' "$(stat -c %Y "$src" 2>/dev/null || echo 0)" "$dst"
    done
  } | sort -rn | awk '{print $2}'
)
if ((${#db_backups[@]})); then
  idx=0
  for b in "${db_backups[@]:-}"; do
    [[ -n "$b" ]] || continue
    base_name="$(basename "$b")"
    # A sidecar never counts as its own backup generation.
    if [[ "$base_name" == *-wal || "$base_name" == *-shm ]]; then
      parent="${b%-wal}"; parent="${parent%-shm}"
      if [[ ! -e "$parent" && ! -e "$ROOT/$base_name" ]]; then deletes+=("$b"); fi
      continue
    fi
    # A zero-byte file is not a snapshot: it can hold nothing, and letting one
    # count as a generation is actively harmful — it occupies a keep-N slot and
    # EVICTS a real backup (measured on the real profile: a 0-byte
    # memory.db.bak-pre-legacy-purge-20260910-094506 stub held one of the five).
    # Zero-byte stubs never count, wherever they sit (this is ③'s rule for the
    # root, applied to the backups directory).
    #
    # `-e` FIRST, and it is not cosmetic: a candidate planned by ① does not
    # exist at its destination yet, so `! -s` is TRUE for it and this rule would
    # delete every snapshot the same run just homed (measured: relocated=4,
    # pruned=4, backups/ left empty). Existence is the precondition; only a file
    # that IS there and holds nothing is a stub.
    if [[ -e "$b" && ! -s "$b" ]]; then
      deletes+=("$b")
      for sc in "$b-wal" "$b-shm"; do [[ -e "$sc" ]] && deletes+=("$sc"); done
      continue
    fi
    # Age is read from the file that exists NOW: a snapshot ① will relocate
    # still lives at the ROOT (its planned destination does not exist yet).
    age_target="$b"
    [[ -e "$age_target" ]] || age_target="$ROOT/$base_name"
    keep=0
    if ((idx < KEEP)); then keep=1; fi
    if ((AGE_DAYS > 0)) && [[ -z "$(find "$age_target" -mtime +"$AGE_DAYS" -print -quit 2>/dev/null)" ]]; then keep=1; fi
    if ((keep)); then
      idx=$((idx + 1))
      # its own sidecars survive with it; orphan sidecars are handled above
      continue
    fi
    deletes+=("$b")
    for sc in "$b-wal" "$b-shm"; do [[ -e "$sc" ]] && deletes+=("$sc"); done
  done
fi

# ②b: CONTENT dedupe among the generations that survived the keep-N/age rule.
# A snapshot is 48-83 MB and the profiles hold exact copies of the same state
# (measured on the HP peer: 13 files of exactly 48488448 bytes, written minutes
# apart by a batch loop, but only 10 DISTINCT states — measured by hashing every
# member, not assumed from the size alone). A byte-identical copy is not extra
# protection, so keep ONE per content hash and drop the rest; the retention
# clock alone would have carried those 3 redundant copies (138.7 MiB) for 14
# more days.
# The first candidate for a hash wins, and the list is newest-first, so the
# retained name is the most recent generation. The LIVE db is never a candidate.
declare -A seen_hash=()
survivors=()
declare -A size_count=()
for b in "${db_backups[@]:-}"; do
  [[ -n "$b" ]] || continue
  base_name="$(basename "$b")"
  [[ "$base_name" == *-wal || "$base_name" == *-shm ]] && continue
  is_deleted "$b" && continue
  src_now="$(planned_src_of "$b")"
  [[ -e "$src_now" && -s "$src_now" ]] || continue
  survivors+=("$b")
  sz="$(stat -c %s "$src_now" 2>/dev/null || echo 0)"
  size_count[$sz]=$(( ${size_count[$sz]:-0} + 1 ))
done
# Hash only where a hash can change the answer: two candidates of DIFFERENT size
# cannot be byte-identical, so a unique size proves uniqueness for free. That
# keeps the sweep from reading every 48-83 MB generation every week — on these
# profiles only the duplicate family is ever hashed.
for b in "${survivors[@]:-}"; do
  [[ -n "$b" ]] || continue
  src_now="$(planned_src_of "$b")"
  sz="$(stat -c %s "$src_now" 2>/dev/null || echo 0)"
  (( ${size_count[$sz]:-0} > 1 )) || continue
  h="$(sha256sum "$src_now" 2>/dev/null | cut -d' ' -f1)"
  [[ -n "$h" ]] || continue
  if [[ -n "${seen_hash[$h]:-}" ]]; then
    dedupes+=("$b|${seen_hash[$h]}")
    deletes+=("$b")
    for sc in "$b-wal" "$b-shm"; do [[ -e "$sc" ]] && deletes+=("$sc"); done
    continue
  fi
  seen_hash[$h]="$b"
done

size_of() { stat -c %s "$1" 2>/dev/null || echo 0; }

echo "gopi-home-gc: root=$ROOT apply=$APPLY keep=$KEEP age=${AGE_DAYS}d live=$LIVE"
if ((${#moves[@]})); then
  echo "-- relocations (${#moves[@]}):"
  for m in "${moves[@]}"; do echo "   move ${m%%|*} -> ${m##*|}"; done
fi
if ((${#deletes[@]})); then
  echo "-- deletions (${#deletes[@]}):"
  for d in "${deletes[@]}"; do
    printf '   delete %s (%s bytes)\n' "$d" "$(size_of "$(planned_src_of "$d")")"
    freed=$((freed + $(size_of "$(planned_src_of "$d")")))
  done
fi
if ((${#dedupes[@]})); then
  echo "-- duplicate generations (byte-identical, ${#dedupes[@]}):"
  for dp in "${dedupes[@]}"; do
    echo "   dedupe ${dp%%|*} (same content as $(basename "${dp##*|}"))"
  done
fi
if ((${#reports[@]})); then
  echo "-- untouched, provenance unknown (${#reports[@]}):"
  for r in "${reports[@]}"; do printf '   keep %s (%s bytes)\n' "$r" "$(size_of "$r")"; done
fi
echo "-- reclaimed if applied: $((freed / 1024)) KiB"

if ! ((APPLY)); then
  echo "gopi-home-gc: DRY RUN — nothing changed (re-run with --apply)"
  exit 0
fi

mkdir -p "$BACKUPS" "$LOGS"
relocated=0
for m in "${moves[@]:-}"; do
  [[ -n "$m" ]] || continue
  src="${m%%|*}"; dst="${m##*|}"
  is_live "$src" && { echo "REFUSED (live db): $src" >&2; continue; }
  # ② already decided this snapshot does not survive: don't home what we are
  # about to delete (the delete below reaches the source either way).
  is_deleted "$dst" && continue
  # The relocation is a home, not a re-stamp: `mv` preserves mtime by itself
  # (POSIX: it is `cp -p` + unlink across devices), which the age rule in ②
  # depends on. NOTE `mv` has NO `-p` flag (that is `cp`) — passing it is an
  # instant `invalid option` + set -e abort, i.e. a sweep that silently does
  # nothing.
  mv -f -- "$src" "$dst"
  relocated=$((relocated + 1))
done
for d in "${deletes[@]:-}"; do
  [[ -n "$d" ]] || continue
  # A snapshot ① planned to relocate but ② pruned this run is still AT THE
  # ROOT: its bytes were counted through that source, so the delete must reach
  # the same file or the sweep reports a reclaim it did not make.
  target="$d"
  [[ -e "$target" ]] || target="$(planned_src_of "$d")"
  is_live "$target" && { echo "REFUSED (live db): $target" >&2; continue; }
  rm -f -- "$target"
done

echo "gopi-home-gc: applied — relocated=$relocated pruned=${#deletes[@]} reclaimed=$((freed / 1024)) KiB"
if ((${#dedupes[@]})); then echo "gopi-home-gc: of those pruned, ${#dedupes[@]} were byte-identical duplicates"; fi
echo "gopi-home-gc: live db intact: $(size_of "$LIVE") bytes"
