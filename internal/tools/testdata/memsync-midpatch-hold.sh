#!/bin/sh
# memsync import-atomicity probe (epic memsync-import-atomic): hold the
# ledger's write lock for HOLD seconds, signalling the instant it is held.
#
# The test it serves opens the "atomic window": the importer must be inside
# its critical section while a foreign writer owns the write lock. A patch
# applied row-by-row (per-statement auto-commit) writes its prefix into that
# window and then dies on the locked row; a patch applied inside ONE
# transaction either waits for the lock or rolls back whole.
#
# Usage: memsync-midpatch-hold.sh <db-path> <signal-file> <hold-seconds> <row-id>
# Exit 0 = lock held for the full window and released; non-zero = the probe
# could not take the lock (the caller reports that as inconclusive, never as a
# pass).
set -eu

db="$1"
sig="$2"
hold="$3"
row="$4"

exec python3 - "$db" "$sig" "$hold" "$row" <<'PY'
import sqlite3
import sys
import time

db, sig, hold, row = sys.argv[1], sys.argv[2], float(sys.argv[3]), sys.argv[4]

con = sqlite3.connect(db, timeout=60, isolation_level=None)
con.execute("PRAGMA busy_timeout = 60000")
# BEGIN IMMEDIATE takes the write lock AT the BEGIN — the same lock mode the
# importer's own transaction (DSN _txlock=immediate) needs.
con.execute("BEGIN IMMEDIATE")
con.execute("UPDATE tasks SET summary = 'held-by-midpatch-probe' WHERE task_id = ?", (row,))
# The signal is written only once the lock is demonstrably ours.
with open(sig, "w", encoding="utf-8") as fh:
    fh.write("held\n")
time.sleep(hold)
con.execute("ROLLBACK")
con.close()
PY
