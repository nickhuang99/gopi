#!/usr/bin/env bash
# Evidence for the claim-remote REVERT leg's idempotent no-op (task 01a0c2b1,
# 2026-09-21): a two-node harness on ONE host where the RECEIVING side is the
# REAL `gopi task sync --claim-remote` handler running the real binary — so the
# accepted/refused document under test is the production one, not a stand-in.
#
#   sender (node A, GOPI_HOME=$ROOT/a) --ssh shim--> receiver (node B, $ROOT/b)
#
# What it asserts (the epic's acceptance, on the real CLI surface):
#   ACCEPTANCE 1  `gopi task sync --push-only --task-id T` on an ALREADY PENDING
#                 row: accepted, one row on the wire, the string "terminal"
#                 ABSENT from the sender's line, and B's row still pending.
#   ACCEPTANCE 2  the same push against a row that is genuinely TERMINAL on the
#                 peer (divergent mirror): REFUSED, with today's wording
#                 ("no longer reclaimable (terminal — left as-is)"), row left
#                 as-is on both sides.
#   ACCEPTANCE 3  the peer's 1..8 row cap is untouched (a whole-window push is
#                 still refused by the peer — this fix changes no protocol
#                 property).
#
# ROOT is a SCRATCH directory (never the repo): the script refuses a ROOT that
# is inside a git work tree, so a run cannot litter its own worktree (Rule 30).
set -uo pipefail

ROOT=${1:?usage: $0 <scratch-root> <gopi binary>}
BIN=${2:?usage: $0 <scratch-root> <gopi binary>}
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'refusing ROOT=%s: it is inside a git work tree — pass a scratch directory\n' "$ROOT" >&2
  exit 2
fi
A=${A_HOME:-$ROOT/a}
B=${B_HOME:-$ROOT/b}
PEER=${PEER_ALIAS:-gopi-proof-peer}
rm -rf "$ROOT/a" "$ROOT/b" "$ROOT/shim"
mkdir -p "$A" "$B" "$ROOT/shim"

say() { printf '%s\n' "$*"; }

# The ssh shim: drop ssh's own options (-o VALUE …) and the host alias, then
# run the remaining remote command under NODE B's environment. This is where
# the peer's real `--claim-remote` handler executes (cap included).
cat > "$ROOT/shim/ssh" <<SHIM
#!/bin/sh
while [ \$# -gt 0 ]; do
  case "\$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
shift                     # the host/alias argv element
GOPI_HOME="$B" GOPI_SYNC_NODE=node-b GOPI_MEMORY_DB="$B/memory.db" \\
  GOPI_NODE_ROLE="" exec /bin/sh -c "\$*"
SHIM
chmod 755 "$ROOT/shim/ssh"
# The remote command is the bare name `gopi` (syncRemoteGopi is load-bearing,
# memory_sync_task.go): the shim resolves it through PATH, so a `gopi` entry
# first on PATH is how the PEER runs THIS build — never whatever gopi the
# ambient PATH happens to hold (a stale installed binary would answer with the
# pre-fix wording and make the harness measure the wrong tree).
ln -sf "$BIN" "$ROOT/shim/gopi"

run_a() {  # the sender binary under node A's environment (the push surface)
  GOPI_HOME="$A" GOPI_SYNC_NODE=node-a GOPI_MEMORY_DB="$A/memory.db" \
    GOPI_NODE_ROLE="" GOPI_SYNC_PEERS="$PEER" PATH="$ROOT/shim:$PATH" "$BIN" "$@"
}
show_b() {  # the peer's OWN ledger view of a row (GOPI_HOME is the .gopi dir)
  GOPI_HOME="$B" GOPI_SYNC_NODE=node-b GOPI_MEMORY_DB="$B/memory.db" \
    GOPI_NODE_ROLE="" "$BIN" task show "$1" 2>&1 | awk '/^status:/{print $2; exit}'
}
b_status() { run_a task show "$1" >/dev/null 2>&1; show_b "$1"; }

PENDING=t-proof-pending
TERMINAL=t-proof-terminal

# ---- Fixture: the two probe rows -----------------------------------------
# Deliberately SMALL (2 rows): on master the --task-id row filter is a
# separate, unfixed defect (branch fix/push-only-task-id-row-filter), so this
# sender still ships its WHOLE window and the peer's 1..8 cap would otherwise
# mask the leg under test. ACCEPTANCE 3 grows the ledger on purpose to show the
# cap is untouched.
run_a task submit "pending re-push probe" --payload "the idempotent no-op row" \
  --task-id "$PENDING" >/dev/null || exit 1
run_a task submit "terminal refusal probe" --payload "the genuinely terminal row" \
  --task-id "$TERMINAL" >/dev/null || exit 1
run_a task sync --push-only --task-id "$PENDING"  >"$ROOT/pre1.out" 2>&1
run_a task sync --push-only --task-id "$TERMINAL" >"$ROOT/pre2.out" 2>&1
say "PRE-DELIVERY: $PENDING -> $(cat "$ROOT/pre1.out")"
say "PRE-DELIVERY: $TERMINAL -> $(cat "$ROOT/pre2.out")"
say "SENDER ROW STATE: $PENDING=$(run_a task show "$PENDING" 2>/dev/null | awk '/^status:/{print $2}')  PEER ROW STATE: $(b_status "$PENDING")"

# ---- ACCEPTANCE 1: the re-push of an ALREADY PENDING row ------------------
say ""
say "=== ACCEPTANCE 1: --push-only --task-id $PENDING (row is pending on BOTH sides) ==="
run_a task sync --push-only --task-id "$PENDING" >"$ROOT/a1.out" 2>&1
R1=$?
say "exit=$R1"
say "output: $(cat "$ROOT/a1.out")"
A1_OK=0
if [ "$R1" = 0 ] \
   && grep -q "accepted (" "$ROOT/a1.out" \
   && ! grep -qi "terminal" "$ROOT/a1.out" \
   && [ "$(b_status "$PENDING")" = pending ]; then
  A1_OK=1
fi
say "ASSERT accepted + no \"terminal\" + peer row still pending: $([ $A1_OK = 1 ] && echo PASS || echo FAIL)"

# ---- ACCEPTANCE 2: a genuinely TERMINAL peer row keeps the refusal --------
# The divergence: the peer's mirror reached a terminal state (the peer's own
# local verdict), the sender's row is still pending. The push verb is pending,
# so the peer's revert leg decides — and a terminal row refuses (终态不回退).
GOPI_HOME="$B" GOPI_SYNC_NODE=node-b GOPI_MEMORY_DB="$B/memory.db" GOPI_NODE_ROLE="" \
  "$BIN" task update "$TERMINAL" --status cancelled >"$ROOT/term.out" 2>&1
say ""
say "PEER TERMINAL SETUP: $(cat "$ROOT/term.out")  (peer row now $(b_status "$TERMINAL"))"
say ""
say "=== ACCEPTANCE 2: --push-only --task-id $TERMINAL (peer row is TERMINAL) ==="
run_a task sync --push-only --task-id "$TERMINAL" >"$ROOT/a2.out" 2>&1
R2=$?
say "exit=$R2"
say "output: $(cat "$ROOT/a2.out")"
A2_OK=0
if [ "$R2" = 0 ] \
   && grep -q "REFUSED" "$ROOT/a2.out" \
   && grep -q "no longer reclaimable (terminal — left as-is)" "$ROOT/a2.out" \
   && [ "$(b_status "$TERMINAL")" = cancelled ] \
   && ! grep -q "want 1..8" "$ROOT/a2.out"; then
  A2_OK=1
fi
say "ASSERT refused with today's wording + peer row left as-is: $([ $A2_OK = 1 ] && echo PASS || echo FAIL)"

# ---- ACCEPTANCE 3: the peer's row cap is untouched -----------------------
say ""
# ---- Fixture for ACCEPTANCE 3: grow ABOVE the peer's 1..8 row cap ----------
for i in 0 1 2 3 4 5 6 7 8 9 10 11; do
  run_a task submit "cap filler $i" --payload "window crowd" --task-id "t-proof-filler-$i" >/dev/null || exit 1
done
say "SENDER LEDGER: $(run_a task list 2>/dev/null | grep -c '^#t-proof-') rows (2 probes + 12 fillers — above the peer's cap)"
say "=== ACCEPTANCE 3: the whole window (no --task-id) still hits the peer's 1..8 cap ==="
run_a task sync --push-only >"$ROOT/a3.out" 2>&1
R3=$?
say "exit=$R3"
say "output: $(cat "$ROOT/a3.out")"
A3_OK=0
grep -q "want 1..8 row(s), got " "$ROOT/a3.out" && A3_OK=1
say "ASSERT the peer's cap is UNCHANGED: $([ $A3_OK = 1 ] && echo PASS || echo FAIL)"

say ""
TOTAL=$((A1_OK + A2_OK + A3_OK))
say "RESULT: $TOTAL/3 PASS  (1=pending re-push accepted without \"terminal\", 2=terminal row keeps the refusal, 3=cap unchanged)"
[ "$TOTAL" = 3 ]
