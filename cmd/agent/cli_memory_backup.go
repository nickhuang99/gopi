// gopi memory backup — the LIVE ledger snapshot + ship command (epic: ledger
// snapshot/ship, 2026-09-19). See internal/tools/ledger_backup.go for the
// mechanism and every constant this file references.
//
//	gopi memory backup [--dir PATH] [--db PATH] [--peer ALIAS]
//	                   [--no-ship] [--name FILE]
//	gopi memory backup --serve          the PEER side (ssh'd by an initiator)
//
// The one command an operator runs takes a CONSISTENT snapshot of the
// authoritative ledger (SQLite's own VACUUM INTO — never a file copy), proves
// it openable and consistent read-only, reports path + size + sha256, and
// lands a copy on the declared peer over the memory_peers asset channel.
// Re-running is safe and idempotent; a failure in any step is loud, and a
// ship failure still reports the local artifact as good (it is).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nick/gopi/internal/tools"
)

// memoryBackupUsage is the help page for `gopi memory backup`. Rule 5 /
// acceptance: RESTORE is documented here as a COMMAND, never performed by
// this verb — a snapshot of a live ledger is safe, restoring OVER one is not.
const memoryBackupUsage = `GoPi memory backup — snapshot the authoritative ledger and ship it to a peer.

USAGE
  gopi memory backup [--dir PATH] [--db PATH] [--peer ALIAS] [--no-ship]
                     [--name FILE]
  gopi memory backup --serve            (peer side: reads an envelope on stdin)

WHAT IT DOES — five steps, one command:
  1. snapshot  VACUUM INTO into <dir>/<name>.partial
               SQLite's OWN snapshot mechanism, never 'cp'. A copy of a LIVE
               database is provably corrupt (measured: the copy returned the
               right row count while PRAGMA integrity_check reported a bad
               freelist, out-of-order rowids and double-referenced pages).
               VACUUM INTO reads the live ledger through one read
               transaction — writers keep running, the artifact is the
               consistent image at its own read point.
  2. verify    the artifact is opened READ-ONLY, one real query runs
               (select count(*) from tasks) and PRAGMA quick_check must
               answer "ok". A row count alone is NOT a proof — the corrupt
               copy counted correctly.
  3. checksum  sha256 + byte size, streamed off disk.
  4. land      the staging file is renamed onto its final name (atomic, same
               directory). An identical artifact already present is kept:
               re-running is idempotent and never rewrites the same bytes.
  5. ship      unless --no-ship: the artifact STREAMS over one ssh leg to a
               peer of the DECLARED knowledge topology (settings.json
               "memory_peers" / GOPI_MEMORY_PEERS) — never sync_peers, never
               a discovered host. The peer re-verifies size + sha256 +
               read-only open + count and answers a receipt; the RECEIPT is
               the success criterion, never the ssh exit status.

OUTPUT — the path, the size, the checksum and the verified row count:
  ledger backup: /home/<user>/.gopi/backups/` + tools.LedgerBackupFileStampLayoutDoc + `
    source:     /home/<user>/.gopi/memory.db (112009216 bytes)
    snapshot:   103530496 bytes  sha256=<hex>
    verified:   read-only open OK, select count(*) from tasks = 2057, PRAGMA quick_check = ok
    shipped:    hp → /home/<user>/.gopi/backups/... (2057 tasks, quick_check ok)

FLAGS
  --dir PATH     backup directory (default: <gopi home>/backups, i.e.
                 ~/.gopi/backups; override: ` + tools.LedgerBackupDirEnv + `).
                 The directory must be OFF the / filesystem — it is measured,
                 not assumed (device ids); a backup on the boot partition
                 shares its fate with the ledger it is meant to survive.
  --db PATH      the LEDGER to snapshot (default: the resolved live ledger,
                 ~/.gopi/memory.db).
  --peer ALIAS   ship target (default: ` + tools.LedgerBackupPeerDefault + `; override: ` + tools.LedgerBackupPeerEnv + `).
                 Still validated against memory_peers — the default is a
                 default, not a permission.
  --no-ship      take the snapshot only (no network; the local artifact is
                 complete and verified).
  --name FILE    artifact name (default: ` + tools.LedgerBackupFileStampLayoutDoc + `).
  --serve        peer side: read one envelope (header + artifact bytes) on
                 stdin, verify it, store it, print a JSON receipt on stdout.
                 Reached over ssh by an initiator — not run by hand.
  -h, --help     this page (including the restore command below).

Any OTHER flag is refused: every flag here writes something or ships over
the network, so a mistyped --no-ship must never be silently ignored.

RESTORE — the command this verb DOES NOT run (documented, by design):
  A snapshot of a live ledger is safe; restoring OVER one is not. To restore
  artifact A over the ledger:
    1. stop every writer — the chat session, the worker/merger, the task server
    2. mv A ~/.gopi/memory.db
    3. rm -f ~/.gopi/memory.db-wal ~/.gopi/memory.db-shm
    4. gopi memory list --limit 1        # opens the restored ledger; a bad
                                         # file fails loudly here
  Step 3 is not optional: a stale -wal left next to a replaced database is
  the classic corrupted-restore.

EXIT STATUS
  0  snapshot taken, verified and (unless --no-ship) received by the peer
  1  a step failed. A ship failure AFTER a good local snapshot exits 1 and
     says the local artifact is good — the backup exists, the peer copy does
     not (a silently failed backup is worse than none, because it is believed).
`

// The flags this sub-verb accepts, named once (Rule 1b). Declaring them is
// also what makes the parser STRICT here: every flag of `memory backup`
// mutates something (it writes an artifact and may ship it over the network),
// so a mistyped `--no-shipp` must be REFUSED rather than silently ignored —
// the ignored spelling would take and ship the full snapshot anyway.
const (
	memoryBackupDirFlag    = "dir"
	memoryBackupDBFlag     = "db"
	memoryBackupPeerFlag   = "peer"
	memoryBackupNameFlag   = "name"
	memoryBackupNoShipFlag = "no-ship"
)

// runMemoryBackup dispatches `gopi memory backup`: the peer-side receive
// handler with --serve, the snapshot+ship pipeline otherwise.
func runMemoryBackup(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(memoryBackupUsage)
			os.Exit(0)
		}
	}
	fs := newMemoryFlagSet()
	// Valueless flags: declared boolean so they never claim the next token,
	// and expected so strict mode accepts them.
	for _, b := range []string{tools.LedgerBackupServeFlag, memoryBackupNoShipFlag} {
		fs.boolean(b)
		fs.expect(b)
	}
	for _, v := range []string{memoryBackupDirFlag, memoryBackupDBFlag, memoryBackupPeerFlag, memoryBackupNameFlag} {
		fs.expect(v)
	}
	positional, err := fs.parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup: %v\n\n%s", err, memoryBackupUsage)
		os.Exit(1)
	}
	if len(positional) > 0 {
		fmt.Fprintf(os.Stderr, "gopi memory backup: unexpected argument %q\n\n%s", positional[0], memoryBackupUsage)
		os.Exit(1)
	}
	if fs.has(tools.LedgerBackupServeFlag) {
		runMemoryBackupServe(fs.get(memoryBackupDirFlag))
		return
	}
	runMemoryBackupSnapshot(fs)
}

// runMemoryBackupSnapshot runs the local pipeline, then (unless --no-ship) the
// peer leg. The two are reported separately so a ship failure can never
// misreport the LOCAL backup, which is good and stays.
func runMemoryBackupSnapshot(fs *memoryFlagSet) {
	res, err := tools.LedgerBackupSnapshot(tools.LedgerBackupOptions{
		DBPath:   fs.get(memoryBackupDBFlag),
		Dir:      fs.get(memoryBackupDirFlag),
		Filename: fs.get(memoryBackupNameFlag),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup: %v\n", err)
		os.Exit(1)
	}
	printLedgerBackupResult(res)

	if fs.has(memoryBackupNoShipFlag) {
		return
	}
	peer := fs.get(memoryBackupPeerFlag)
	if peer == "" {
		peer, err = tools.LedgerBackupPeerResolved()
		if err != nil {
			// The local snapshot is DONE and verified — say so before
			// failing, so nobody reads the failure as "no backup".
			fmt.Fprintf(os.Stderr, "gopi memory backup: ship skipped: %v\n"+
				"gopi memory backup: the local snapshot above is complete and verified; only the peer copy is missing\n", err)
			os.Exit(1)
		}
	}
	ship, err := tools.LedgerBackupShip(peer, res.SnapshotPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup: snapshot landed at %s (size %d, %s %s, %d tasks) but the ship FAILED: %v\n",
			res.SnapshotPath, res.SnapshotSize, res.ChecksumAlgo, res.Checksum, res.Tasks, err)
		os.Exit(1)
	}
	res.Ship = ship
	printLedgerBackupShip(res, ship)
}

// printLedgerBackupResult renders the local half: path, size, checksum and
// the verification that actually ran.
func printLedgerBackupResult(res *tools.LedgerBackupResult) {
	fmt.Printf("ledger backup: %s\n", res.SnapshotPath)
	if res.Deduped {
		fmt.Printf("  note:       an identical artifact was already here (%s matches) — kept, not rewritten\n", res.ChecksumAlgo)
	}
	fmt.Printf("  source:     %s (%d bytes)\n", res.SourcePath, res.SourceSize)
	fmt.Printf("  snapshot:   %d bytes  %s=%s\n", res.SnapshotSize, res.ChecksumAlgo, res.Checksum)
	fmt.Printf("  verified:   read-only open OK, select count(*) from tasks = %d, PRAGMA quick_check = %s\n",
		res.Tasks, res.Consistency)
	fmt.Printf("  took:       %.1fs\n", res.DurationSec)
}

// printLedgerBackupShip renders the peer half from the peer's own receipt.
func printLedgerBackupShip(res *tools.LedgerBackupResult, ship *tools.LedgerBackupShipResult) {
	r := ship.Receipt
	held := ""
	if r.AlreadyHeld {
		held = " [already held — identical artifact kept]"
	}
	fmt.Printf("  shipped:    %s → %s (%d bytes, %s=%s, %d tasks, quick_check %s, %.1fs)%s\n",
		ship.Peer, r.Path, r.Size, res.ChecksumAlgo, r.Checksum, r.Tasks, r.Consistency, ship.DurationSec, held)
}

// runMemoryBackupServe is the peer side: stdin = one envelope (JSON header
// line + artifact bytes), stdout = the JSON receipt. Nothing else is written
// to stdout — stdin/stdout IS the protocol (human-facing detail goes to
// stderr, where the initiator surfaces it in its ssh error text).
func runMemoryBackupServe(dir string) {
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup --serve: read stdin: %v\n", err)
		os.Exit(1)
	}
	receipt, err := tools.LedgerBackupReceive(body, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup --serve: %v\n", err)
		os.Exit(1)
	}
	out, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory backup --serve: marshal receipt: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// isMemoryBackupSubcommand reports whether argv begins with the
// `memory backup` family verb. Like isMemorySyncSubcommand it is matched
// BEFORE the group predicate so `gopi memory backup -h` reaches the
// sub-verb's own handler (the group's -h is the family page).
func isMemoryBackupSubcommand(args []string) bool {
	return len(args) >= 2 && args[0] == CliVerbMemory && args[1] == tools.LedgerBackupVerb
}
