#!/usr/bin/env bash
# End-to-end acceptance for the --push-only --task-id fix (task 01a0b...):
# a TWO-NODE harness on ONE host (GOPI_HOME isolation, the repo's established
# two-node sim), where the RECEIVING side is the REAL `gopi ... --claim-remote`
# handler running the real binary — so the peer's 1..8 row cap is the actual
# production check, not a stand-in.
#
#   sender (node A, GOPI_HOME=$ROOT/a) --ssh--> receiver (node B, $ROOT/b)
#
# The ssh leg is shimmed at the PATH level (a `ssh` script that runs the
# remote command locally with NODE B's environment): the claim channel's argv
# shape (`ssh <peer> "<gopi> task sync --claim-remote"`) is exactly what the
# shim receives and executes, so the protocol path exercised is the real one.
set -uo pipefail

ROOT=${1:?usage: $0 <root> <gopi binary>}
BIN=${2:?usage: $0 <root> <gopi binary>}
# ROOT is where the two nodes, the ssh shim and the captured output live — it is
# a SCRATCH directory, never the repo. Refuse a ROOT that would litter a git
# work tree (Rule 30: a run leaves its worktree clean) so the harness cannot be
# pointed at $PWD by accident.
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

# The shim: drop the host argument, then run the remote command with NODE B's
# environment (GOPI_HOME + the ledger). This is where the peer's own
# `--claim-remote` handler runs — cap included.
cat > "$ROOT/shim/ssh" <<SHIM
#!/bin/sh
# Drop ssh's own options (-o VALUE …), then the host alias; what remains is
# the remote command — run it locally under NODE B's environment. This is the
# peer's real handler (the cap included).
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

# The fake ssh_config host is unnecessary (the shim ignores the host); the
# alias only has to pass ValidateSSHPeerAlias.

run_a() {  # run the sender binary with node A's environment
  GOPI_HOME="$A" GOPI_SYNC_NODE=node-a GOPI_MEMORY_DB="$A/memory.db" \
    GOPI_NODE_ROLE="" GOPI_SYNC_PEERS="$PEER" PATH="$ROOT/shim:$PATH" "$BIN" "$@"
}

# ---- Fixture: a MULTI-ROW ledger on the SENDER ----------------------------
# One requested row + 12 siblings = 13 rows, far above the peer's 1..8 cap.
for i in 0 1 2 3 4 5 6 7 8 9 10 11; do
  run_a task submit "proof filler $i" --payload "window crowd" --task-id "t-proof-filler-$i" >/dev/null || exit 1
done
run_a task submit "proof requested row" --payload "the one row the manual push must carry" \
  --task-id t-proof-wanted >/dev/null || exit 1
ROWS_A=$(run_a task list 2>/dev/null | grep -c '^#t-proof-')
say "SENDER LEDGER: $ROWS_A rows (1 requested + 12 siblings — far above the peer's 1..8 cap)"

# ---- The peer must first KNOW the row (a claim verb has no insert path) ----
# Deliver only the requested row to B by the full channel (the ordinary path
# that carries rows), then put the sender's row into the state a claim
# broadcast actually ships: in_progress (what a runner holds mid-task). The
# row exists on BOTH sides by then (verified below), so the peer has a real
# row to apply the claim to.
run_a task sync --push-only --task-id t-proof-wanted >"$ROOT/pre.out" 2>&1
say "PRE-DELIVERY (--task-id): $(cat "$ROOT/pre.out")"
run_a task update t-proof-wanted --status in_progress >"$ROOT/pre2.out" 2>&1
A_STATUS=$(run_a task show t-proof-wanted 2>/dev/null | awk '/^status:/{print $2}')
HOLD=0
case "$A_STATUS" in in_progress|claimed) HOLD=1 ;; esac
say "SENDER ROW STATE: $A_STATUS (claim verb: $([ $HOLD = 1 ] && echo yes || echo NO))"

# ---- ACCEPTANCE 1: exact one row on the wire, accepted by the real peer ----
say ""
say "=== ACCEPTANCE 1: --push-only --task-id t-proof-wanted (13-row ledger) ==="
run_a task sync --push-only --task-id t-proof-wanted >"$ROOT/a1.out" 2>&1
R1=$?
say "exit=$R1"
say "output: $(cat "$ROOT/a1.out")"
A1_OK=0
grep -q "accepted (1 row(s))" "$ROOT/a1.out" && A1_OK=1
say "ASSERT one-row-on-wire + peer accepted: $([ $A1_OK = 1 ] && echo PASS || echo FAIL)"

# ---- ACCEPTANCE 2: the un-fixed shape is refused BY THE PEER (the cap) ------
say ""
say "=== ACCEPTANCE 2: the whole window (what the bug shipped) vs the 1..8 cap ==="
run_a task sync --push-only >"$ROOT/a2.out" 2>&1
R2=$?
say "exit=$R2"
say "output: $(cat "$ROOT/a2.out")"
A2_OK=0
grep -q "want 1..8 row(s), got " "$ROOT/a2.out" && A2_OK=1
say "ASSERT the peer's cap is UNCHANGED and still refuses a window: $([ $A2_OK = 1 ] && echo PASS || echo FAIL)"

# ---- ACCEPTANCE 3: sender-side not-found (never a peer row-count complaint)
say ""
say "=== ACCEPTANCE 3: unknown --task-id refuses on the SENDER ==="
run_a task sync --push-only --task-id t-proof-absent >"$ROOT/a3.out" 2>&1
R3=$?
say "exit=$R3"
say "output: $(cat "$ROOT/a3.out")"
A3_OK=0
grep -q "not found in the ledger" "$ROOT/a3.out" && ! grep -q "want 1..8" "$ROOT/a3.out" && A3_OK=1
say "ASSERT sender-side not-found, no peer cap message: $([ $A3_OK = 1 ] && echo PASS || echo FAIL)"

# ---- The mirrored row on the peer (visible evidence) ----------------------
say ""
say "=== PEER SIDE: the requested row, as the receiver's own ledger shows it ==="
GOPI_HOME="$B" GOPI_MEMORY_DB="$B/memory.db" GOPI_SYNC_NODE=node-b \
  GOPI_NODE_ROLE="" "$BIN" task show t-proof-wanted 2>&1 | head -12

say ""
TOTAL=$((A1_OK + A2_OK + A3_OK))
say "RESULT: $TOTAL/3 PASS  (1=exact one row accepted, 2=cap unchanged, 3=sender-side refusal)"
[ "$TOTAL" = 3 ]
