#!/usr/bin/env bash
# M4.13 E2E shim sim — two fake nodes (A=initiator, B=peer) on one host.
# `bin/ssh` intercepts ssh legs (both aliases) and runs the PEER's gopi with
# the peer's GOPI_HOME: `ssh <alias> "gopi task sync --serve-remote"` — the
# remote command arrives as ONE quoted element plus ssh options (the real
# login-shell form; see isTasksSubcommand's ssh-argv handling in cli_tasks.go).
# Usage: bash devdoc/memsync-e2e-sim.sh /path/to/gopi
set -e
BIN="$(readlink -f "$1")"
# Scratch root: the shared workspace scratch home (NEVER /tmp — the / filesystem
# is the 2026-09-11 exhaustion site). See scripts/lib/tmp-ban.sh.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/tmp-ban.sh
. "$HERE/../scripts/lib/tmp-ban.sh"
ROOT="$(gopi_scratch_mkdir memsync-e2e-sim)"
trap gopi_scratch_cleanup EXIT INT TERM
mkdir -p "$ROOT/bin" "$ROOT/a" "$ROOT/b"
cat > "$ROOT/bin/ssh" <<EOF
#!/usr/bin/env bash
# ssh shim: options (-o pairs, lone flags) then the alias decide the node.
alias=""
args=()
while [ \$# -gt 0 ]; do
  case "\$1" in
    -o) shift 2; continue ;;
    -*) shift; continue ;;
    *) if [ -z "\$alias" ]; then alias="\$1"; shift; else args+=("\$1"); shift; fi ;;
  esac
done
case "\$alias" in
  bnode) SIDE_HOME="$ROOT/b" ;;
  anode) SIDE_HOME="$ROOT/a" ;;
  *) echo "ssh shim: unknown alias \$alias" >&2; exit 255 ;;
esac
# no_enforce_memory disables the harness memory checkpoints; raw log off
# keeps the run silent (the ONLY stdout must be the delta envelope).
export GOPI_NO_ENFORCE_MEMORY=1 GOPI_RAW_LOG=off GOPI_AUTO_TASK_REGISTER=0
export GOPI_HOME="\$SIDE_HOME" GOPI_MEMORY_DB="\$SIDE_HOME/memory.db"
exec "$BIN" "\${args[@]}"
EOF
chmod +x "$ROOT/bin/ssh"
export PATH="$ROOT/bin:$PATH"
a() { GOPI_AUTO_TASK_REGISTER=0 GOPI_HOME="$ROOT/a" GOPI_MEMORY_DB="$ROOT/a/memory.db" "$BIN" "$@"; }
b() { GOPI_AUTO_TASK_REGISTER=0 GOPI_HOME="$ROOT/b" GOPI_MEMORY_DB="$ROOT/b/memory.db" "$BIN" "$@"; }
# same-host sim: both nodes are ONE hostname, so patch.Node collides — the
# shim exports GOPI_SYNC_NODE to name each side distinctly (provenance +
# anti-reflow depend on distinct node names).
export GOPI_SYNC_NODE=sim-anode
b2() { GOPI_SYNC_NODE=sim-bnode GOPI_AUTO_TASK_REGISTER=0 GOPI_HOME="$ROOT/b" GOPI_MEMORY_DB="$ROOT/b/memory.db" "$BIN" "$@"; }

echo "=== ① A creates 3 rows → one session → B sees 3"
a task submit "alpha" --source human >/dev/null
a task submit "bravo" --source human >/dev/null
a task submit "charlie" --source human >/dev/null
GOPI_SYNC_PEERS=bnode a task sync --peer bnode

echo "=== ② B claims one → completes + self-submits → session → A migrates + imports"
B_FIRST="$(b2 task list 2>/dev/null | grep pending | head -1 | awk '{print $2}')"
[ -n "$B_FIRST" ] && b2 task update "$B_FIRST" --status claimed >/dev/null
[ -n "$B_FIRST" ] && b2 task update "$B_FIRST" --status in_progress >/dev/null
[ -n "$B_FIRST" ] && b2 task update "$B_FIRST" --status completed >/dev/null
b2 task submit "delta-from-b" --source human >/dev/null
GOPI_SYNC_PEERS=anode b2 task sync --peer anode

echo "=== ③ A's view after round 2 (one migrated to completed + new from-b row):"
a task list | grep completed | head -1
a task list | grep -c "delta-from-b" || true

echo "=== ④ idempotent re-run (anchors → 0 rows):"
GOPI_SYNC_PEERS=bnode a task sync --peer bnode
rm -rf "$ROOT"
