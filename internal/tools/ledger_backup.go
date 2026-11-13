// Package tools — ledger_backup.go: the LIVE ledger snapshot + ship path
// (epic: ledger snapshot/ship, 2026-09-19).
//
// WHY THIS FILE EXISTS. The authoritative ledger — ~/.gopi/memory.db, one
// SQLite file holding BOTH the task ledger and the knowledge base — had no
// pipeline at all: every snapshot in ~/.gopi/backups/ was a hand-typed
// `cp`. A `cp` of a LIVE database is provably corrupt, and the corruption
// is invisible to the check people actually run. Measured 2026-09-19 on the
// live 112MB ledger: the copy answered the RIGHT row count
// (`select count(*) from tasks` → 2057) while `PRAGMA integrity_check`
// reported `Freelist: size is 1966 but should be 1941`, out-of-order rowids
// and double-referenced pages. A COUNT IS NOT A CONSISTENCY PROOF. So the
// snapshot mechanism is SQLite's OWN, never a file copy:
//
//	VACUUM INTO <file>
//
// which reads the source through an ordinary read transaction and writes a
// fresh, fully consistent database. The source file is never written to: a
// live writer may keep writing while the snapshot is taken, and the
// artifact is the consistent image at its own read point.
//
// THE PIPELINE (ONE command, `gopi memory backup`):
//
//	① snapshot — VACUUM INTO <dir>/<name>.partial. Never straight into the
//	             final name: a killed or interrupted run must not leave a
//	             truncated .db sitting in backups/ looking like a backup.
//	② verify   — open the artifact READ-ONLY, run `select count(*) from
//	             tasks`, and require `PRAGMA quick_check` to answer "ok".
//	③ checksum — sha256 + byte size, streamed off disk.
//	④ land     — rename .partial → final (same directory, atomic). An
//	             existing final with the SAME sha256 is kept and the staging
//	             file dropped: re-running is idempotent and never rewrites an
//	             identical 100MB artifact.
//	⑤ ship     — optional, off by --no-ship: the artifact STREAMS over ONE
//	             ssh leg to a peer of the DECLARED knowledge topology
//	             (memory_peers — never sync_peers, never discovered). The
//	             peer re-verifies (size, sha256, read-only open, count) and
//	             answers a JSON receipt; the receipt is the success
//	             criterion, never the ssh exit status alone.
//
// Every step fails LOUDLY (Rule 15b): a silently failed backup is worse
// than no backup, because it is believed.
//
// RESTORE is documented, not performed: `gopi memory backup -h` prints the
// command (stop the writers, move the artifact over the ledger, remove the
// stale -wal/-shm). Snapshotting a live ledger is safe; restoring OVER one
// is not, which is why this feature never does it.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// ---------------------------------------------------------------------------
// The verb vocabulary — OWNED HERE, because this package owns the machinery
// (Rule 14: a value two packages must agree on has ONE definition, in a
// package every consumer can import; Rule 1b/13: never mirror, never type a
// bare literal). cmd/agent already imports internal/tools, so the CLI
// family file references these constants instead of re-typing the words —
// the same direction internal/taskverbs establishes for the task family,
// applied to a family whose machinery lives in this package.
// ---------------------------------------------------------------------------

const (
	// CliVerbMemoryGroup is the `gopi memory` GROUP verb word — the family
	// the backup sub-verb hangs off. It is defined HERE, in this package,
	// because `gopi memory backup --serve` is exactly the remote command this
	// file's ship leg runs: the word and its wire contract are one decision.
	// cmd/agent's CliVerbMemory is an ALIAS of this constant (Rule 14 — one
	// definition, referenced by consumers, never mirrored; a second literal
	// here would be the drift Rule 13 bans).
	CliVerbMemoryGroup = "memory"

	// LedgerBackupVerb is the `gopi memory backup` sub-verb word.
	LedgerBackupVerb = "backup"
	// LedgerBackupServeFlag is the peer-side mode flag
	// (`gopi memory backup --serve`): the receive handler an initiator
	// reaches over ssh. It accepts NO value (a valueless flag).
	LedgerBackupServeFlag = "serve"
)

// ---------------------------------------------------------------------------
// Named constants (Rule 1: no magic numbers or bare string keywords).
// ---------------------------------------------------------------------------

const (
	// LedgerBackupDirName is the backup directory: a child of the gopi home
	// dir (~/.gopi), so a snapshot lands next to the ledger it snapshots.
	LedgerBackupDirName = "backups"

	// LedgerBackupFilePrefix is the artifact filename prefix; the full name
	// is <prefix>-<stamp>.db (see LedgerBackupFileSuffix).
	LedgerBackupFilePrefix = "memory.db.backup"

	// LedgerBackupFileStampLayout stamps the filename with the snapshot's
	// local wall-clock time — the name a human scans for "which one is
	// oldest".
	LedgerBackupFileStampLayout = "20060102-150405"

	// LedgerBackupFileSuffix closes a restorable artifact's name.
	LedgerBackupFileSuffix = ".db"

	// ledgerBackupStagingSuffix names an artifact that is NOT yet proved
	// good. Deliberately not a .db name: nothing that globs backups/ for
	// restorable artifacts may pick up a half-written file.
	ledgerBackupStagingSuffix = ".partial"

	// LedgerBackupChecksumAlgo names the checksum reported for an artifact
	// (and carried in the ship envelope). SHA-256, hex-encoded lowercase.
	LedgerBackupChecksumAlgo = "sha256"

	// LedgerBackupEnvelopeVersion is the wire version of the ship envelope
	// (the peer refuses any other value loudly).
	LedgerBackupEnvelopeVersion = 1

	// LedgerBackupPeerDefault is the peer a ship targets when neither
	// --peer nor the env override names one. It is STILL validated against
	// the declared memory_peers whitelist — a default is a default, not a
	// permission (see LedgerBackupPeerResolved).
	LedgerBackupPeerDefault = "hp"

	// ledgerBackupTasksQuery is the ONE real query the verification runs:
	// the task ledger's own row count — the number an operator can compare
	// against `gopi task list`.
	ledgerBackupTasksQuery = "select count(*) from tasks"

	// ledgerBackupQuickCheckPragma is the consistency verdict. quick_check
	// (not integrity_check) because it answers the same "is this database
	// well-formed" question without also verifying every index entry against
	// its table. Measured on the live 103MB artifact 2026-09-19: quick_check
	// 20.5s, integrity_check 11.9s, and BOTH detected the corrupt `cp` copy
	// — the cheaper one is the earlier failure signal, which is what a
	// refused backup needs.
	ledgerBackupQuickCheckPragma = "PRAGMA quick_check"
)

const (
	// LedgerBackupDirEnv overrides the backup DIRECTORY (read in exactly one
	// place — ResolveLedgerBackupDir, Rule 2).
	LedgerBackupDirEnv = "GOPI_LEDGER_BACKUP_DIR"

	// LedgerBackupPeerEnv overrides the ship TARGET (read in exactly one
	// place — LedgerBackupPeerResolved, Rule 2).
	LedgerBackupPeerEnv = "GOPI_LEDGER_BACKUP_PEER"
)

const (
	// ledgerBackupSSHTimeout bounds the WHOLE ship leg: one ssh connection
	// plus the streamed artifact (the live ledger's snapshot is ~100MB — the
	// 15s task/asset exchange bound is not enough for a file transfer, and a
	// stream that is cut mid-flight is a corrupt landing the peer then has to
	// refuse).
	ledgerBackupSSHTimeout = 5 * time.Minute

	// ledgerBackupEnvelopeHeaderMax caps the header line a peer reads before
	// giving up on a garbage/hostile stream (the header is a five-field JSON
	// object; 64KiB is generous).
	ledgerBackupEnvelopeHeaderMax = 64 * 1024

	// ledgerBackupDirPerm / ledgerBackupFilePerm are the modes of a created
	// backup directory and a landed artifact: an operator's own tooling (and
	// a peer's read-only open) must be able to read a snapshot.
	ledgerBackupDirPerm  = 0o755
	ledgerBackupFilePerm = 0o644
)

// LedgerBackupFileStampLayoutDoc is the human-readable form of the artifact
// naming contract, used in help text so the two cannot drift.
const LedgerBackupFileStampLayoutDoc = LedgerBackupFilePrefix + "-YYYYMMDD-HHMMSS" + LedgerBackupFileSuffix

// ---------------------------------------------------------------------------
// Result + option types.
// ---------------------------------------------------------------------------

// LedgerBackupResult is one completed snapshot (and, when shipped, the peer's
// receipt).
type LedgerBackupResult struct {
	// SourcePath / SourceSize describe the LIVE ledger snapshotted.
	SourcePath string
	SourceSize int64
	// SnapshotPath / SnapshotSize describe the artifact on THIS node.
	SnapshotPath string
	SnapshotSize int64
	// Checksum is the hex sha256 of the artifact; ChecksumAlgo names it.
	Checksum     string
	ChecksumAlgo string
	// Tasks is the artifact's own `select count(*) from tasks` — the number
	// the verification really read back.
	Tasks int64
	// Consistency is the artifact's quick_check verdict ("ok" when healthy).
	Consistency string
	// Deduped marks a re-run that found a byte-identical artifact already in
	// place and kept it instead of rewriting it.
	Deduped bool
	// DurationSec covers snapshot + verify + checksum + land.
	DurationSec float64
	// Ship is nil when the ship was skipped (--no-ship) or not requested.
	Ship *LedgerBackupShipResult
}

// LedgerBackupShipResult reports the peer leg.
type LedgerBackupShipResult struct {
	Peer string
	// Receipt is the peer's OWN verification of what it stored — the
	// success criterion.
	Receipt *LedgerBackupReceipt
	// DurationSec of the whole ssh leg.
	DurationSec float64
}

// LedgerBackupVerification is the read-only proof that an artifact is a
// usable database.
type LedgerBackupVerification struct {
	Path        string
	Size        int64
	Tasks       int64
	Consistency string
}

// LedgerBackupOptions selects what to snapshot and where it lands.
type LedgerBackupOptions struct {
	// DBPath overrides the source ledger ("" = the resolved live ledger).
	DBPath string
	// Dir overrides the backup directory ("" = ResolveLedgerBackupDir).
	Dir string
	// Filename overrides the timestamped artifact name (tests/operators).
	Filename string
	// Now stamps the filename (zero = time.Now, so tests pin the name and a
	// re-run in the same second is deterministic).
	Now time.Time
}

// ---------------------------------------------------------------------------
// Path + topology resolvers (one definition each, Rule 2).
// ---------------------------------------------------------------------------

// ResolveLedgerBackupDir resolves the directory snapshots land in: the
// LedgerBackupDirEnv override, else <gopi home>/backups. This is the ONE
// reader of LedgerBackupDirEnv (Rule 2).
func ResolveLedgerBackupDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(LedgerBackupDirEnv)); dir != "" {
		return dir, nil
	}
	home, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("ledger backup: resolve backup dir: %w", err)
	}
	return filepath.Join(home, LedgerBackupDirName), nil
}

// LedgerBackupPeerResolved resolves and AUTHORIZES the ship target: the
// LedgerBackupPeerEnv override or LedgerBackupPeerDefault, then the SAME
// declared-topology check the knowledge channel applies (memory_peers —
// never sync_peers, never a discovered host). This is the ONE reader of
// LedgerBackupPeerEnv (Rule 2). An undeclared peer is refused, so the
// default can never quietly ssh somewhere the operator never listed.
func LedgerBackupPeerResolved() (string, error) {
	peer := strings.TrimSpace(os.Getenv(LedgerBackupPeerEnv))
	if peer == "" {
		peer = LedgerBackupPeerDefault
	}
	if err := syncAssetsPeerWhitelisted(peer, nil); err != nil {
		return "", fmt.Errorf("ledger backup: %w", err)
	}
	return peer, nil
}

// LedgerBackupSourcePath returns the LIVE ledger path this feature
// snapshots — the knowledge-base resolver, whose default is the
// authoritative ~/.gopi/memory.db.
func LedgerBackupSourcePath() string { return memoryDBPath() }

// ledgerBackupFileName builds the artifact name for one snapshot. The
// staging name is DERIVED from it (never assembled separately), so the two
// can never drift.
func ledgerBackupFileName(now time.Time) string {
	return LedgerBackupFilePrefix + "-" + now.Format(LedgerBackupFileStampLayout) + LedgerBackupFileSuffix
}

// ledgerBackupStagingPath is where a snapshot is built BEFORE it is proved
// good: <final><stagingSuffix>.
func ledgerBackupStagingPath(final string) string { return final + ledgerBackupStagingSuffix }

// LedgerBackupArtifactNameValid reports whether a filename arriving over the
// wire may be written at all. The peer writes what the initiator names, so
// the name is CHECKED, never trusted: a bare base name carrying our own
// prefix and suffix — no separators, no traversal, no absolute path, no
// hidden dotfile.
func LedgerBackupArtifactNameValid(name string) bool {
	if name == "" || name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return false
	}
	return strings.HasPrefix(name, LedgerBackupFilePrefix) && strings.HasSuffix(name, LedgerBackupFileSuffix)
}

// ---------------------------------------------------------------------------
// ① snapshot ② verify ③ checksum ④ land
// ---------------------------------------------------------------------------

// LedgerBackupSnapshot takes the consistent snapshot, proves it openable,
// checksums it and lands it under the resolved backup directory. It never
// writes to the source ledger, and a FAILED run leaves the source untouched
// and no artifact behind (only a staging file, which it removes).
func LedgerBackupSnapshot(opts LedgerBackupOptions) (*LedgerBackupResult, error) {
	started := time.Now()

	src := strings.TrimSpace(opts.DBPath)
	if src == "" {
		src = LedgerBackupSourcePath()
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: source ledger %q: %w", src, err)
	}
	if srcInfo.IsDir() {
		return nil, fmt.Errorf("ledger backup: source ledger %q is a directory, not a database file", src)
	}

	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		dir, err = ResolveLedgerBackupDir()
		if err != nil {
			return nil, err
		}
	}
	// Rule 22 — the write site is MEASURED, never assumed: a backup
	// directory sharing `/`'s filesystem competes with the boot partition
	// (the 2026-09-11 exhaustion site), and the whole point of this feature
	// is a backup that survives. The assertion answers for a directory that
	// does not exist yet (measured on its nearest existing ancestor).
	if err := scratch.AssertPathOffRootFS(dir); err != nil {
		return nil, fmt.Errorf("ledger backup: backup directory refused: %w", err)
	}
	if err := os.MkdirAll(dir, ledgerBackupDirPerm); err != nil {
		return nil, fmt.Errorf("ledger backup: create backup directory %q: %w", dir, err)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	name := strings.TrimSpace(opts.Filename)
	if name == "" {
		name = ledgerBackupFileName(now)
	}
	if !LedgerBackupArtifactNameValid(name) {
		return nil, fmt.Errorf("ledger backup: artifact name %q must be %s (a bare file name)",
			name, LedgerBackupFileStampLayoutDoc)
	}
	final := filepath.Join(dir, name)
	staging := ledgerBackupStagingPath(final)

	// A stale staging file from an interrupted run is removed first: it
	// describes a snapshot that was never proved good.
	if err := ledgerBackupClearStaging(staging); err != nil {
		return nil, err
	}

	if err := ledgerBackupVacuumInto(src, staging); err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, err
	}
	// From here on every failure path drops the staging file: an unverified
	// artifact must never survive under a restorable name.
	sum, size, err := ledgerBackupChecksum(staging)
	if err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, err
	}
	verification, err := LedgerBackupVerify(staging)
	if err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, fmt.Errorf("ledger backup: verify snapshot: %w", err)
	}

	res := &LedgerBackupResult{
		SourcePath:   src,
		SourceSize:   srcInfo.Size(),
		SnapshotPath: final,
		SnapshotSize: size,
		Checksum:     sum,
		ChecksumAlgo: LedgerBackupChecksumAlgo,
		Tasks:        verification.Tasks,
		Consistency:  verification.Consistency,
	}

	// ④ land. An identical artifact already in place is KEPT (idempotent
	// re-run): compare on the checksum we just computed, never on the
	// filename alone (:15:03 twice is a name collision, not a same-content
	// proof).
	if prev, _, prevErr := ledgerBackupChecksum(final); prevErr == nil && prev == sum {
		ledgerBackupDiscardStaging(staging)
		res.Deduped = true
		res.DurationSec = time.Since(started).Seconds()
		return res, nil
	}
	if err := os.Rename(staging, final); err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, fmt.Errorf("ledger backup: land snapshot %q: %w", final, err)
	}
	res.DurationSec = time.Since(started).Seconds()
	return res, nil
}

// ledgerBackupVacuumInto runs SQLite's OWN snapshot mechanism.
//
// LOAD-BEARING DETAIL (measured, not assumed): the source connection must
// NOT be opened mode=ro / query_only. `VACUUM INTO` is refused there with
// "attempt to write a readonly database (8)" — SQLite opens the OUTPUT file
// through the same connection, so a read-only connection cannot create it,
// even though the source itself is only ever read. The source is therefore
// opened write-capable and NEVER written: `VACUUM INTO` reads it under a
// normal read transaction, and the ledger's own integrity is untouched
// (asserted by TestLedgerBackupSnapshotLeavesTheLiveLedgerIntact).
func ledgerBackupVacuumInto(src, dst string) error {
	// LedgerDSN (NOT the read-only variant): a mode=ro/query_only connection
	// cannot create the OUTPUT file, so `VACUUM INTO` is refused with "attempt
	// to write a readonly database (8)" — see the LOAD-BEARING DETAIL above.
	// LedgerDSN carries production's own busy timeout, so the read still queues
	// behind a live writer instead of failing with SQLITE_BUSY.
	db, err := sql.Open("sqlite", LedgerDSN(src))
	if err != nil {
		return fmt.Errorf("ledger backup: open source ledger %q: %w", src, err)
	}
	defer db.Close() //stylegate:ignore the close cannot invalidate an artifact already written; the exec error below is the reported one
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("ledger backup: VACUUM INTO %q from %q (the source ledger is left untouched): %w", dst, src, err)
	}
	return nil
}

// ledgerBackupClearStaging removes a stale staging file (a leftover of an
// interrupted run that was never proved good).
func ledgerBackupClearStaging(staging string) error {
	if err := os.Remove(staging); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ledger backup: clear stale staging file %q: %w", staging, err)
	}
	return nil
}

// ledgerBackupDiscardStaging removes a staging file, LOGGING (never hiding)
// a failure: leaving it behind would be litter, but the caller's real error
// is the one that must survive (Rule 15b).
func ledgerBackupDiscardStaging(staging string) {
	if err := os.Remove(staging); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ledger backup: could not remove staging file %s: %v\n", staging, err)
	}
}

// LedgerBackupVerify proves an artifact is a USABLE database, read-only: it
// opens it with mode=ro, runs one real query (the task-ledger row count) and
// requires quick_check to answer "ok". A count alone is not evidence — the
// corrupt `cp` copy counted correctly.
func LedgerBackupVerify(path string) (*LedgerBackupVerification, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: artifact %q: %w", path, err)
	}
	// The ARTIFACT is never written, so the read-only variant is the right
	// contract here (mode=ro + query_only); its busy timeout is the same
	// dbBusyTimeoutMSec the production opener uses — a local artifact never
	// needs a different bound, so there is no second value to keep in sync.
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("ledger backup: open artifact read-only: %w", err)
	}
	defer db.Close() //stylegate:ignore verification-only handle: the artifact is already on disk and its verdict is returned as data
	db.SetMaxOpenConns(1)

	var tasks int64
	if err := db.QueryRow(ledgerBackupTasksQuery).Scan(&tasks); err != nil {
		return nil, fmt.Errorf("ledger backup: artifact %q is not usable (%s): %w", path, ledgerBackupTasksQuery, err)
	}
	verdict, err := ledgerBackupConsistencyVerdict(db)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: artifact %q: %w", path, err)
	}
	return &LedgerBackupVerification{Path: path, Size: info.Size(), Tasks: tasks, Consistency: verdict}, nil
}

// ledgerBackupConsistencyVerdict runs quick_check and returns its verdict,
// refusing anything that is not a clean "ok". A corrupt database answers
// with one or more "*** ..." lines, but ANY non-"ok" answer is a refusal —
// the gate never assumes a shape it has not seen.
func ledgerBackupConsistencyVerdict(db *sql.DB) (string, error) {
	var out string
	if err := db.QueryRow(ledgerBackupQuickCheckPragma).Scan(&out); err != nil {
		return "", fmt.Errorf("%s: %w", ledgerBackupQuickCheckPragma, err)
	}
	verdict := strings.TrimSpace(out)
	if verdict != "ok" {
		return verdict, fmt.Errorf("snapshot is not consistent (%s answered %q) — refusing it",
			ledgerBackupQuickCheckPragma, ledgerBackupFirstLine(verdict))
	}
	return verdict, nil
}

// firstLine trims a multi-line SQLite verdict to its first line so an error
// message stays readable (the full text stays in the artifact for whoever
// wants it).
func ledgerBackupFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ledgerBackupChecksum STREAMS the artifact off disk and returns its
// lowercase hex sha256 plus byte size — streamed, so a 1GB ledger costs no
// memory.
func ledgerBackupChecksum(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("ledger backup: checksum: open %q: %w", path, err)
	}
	defer f.Close() //stylegate:ignore read-only handle closed at function end
	return LedgerBackupChecksumStream(f)
}

// LedgerBackupChecksumStream hashes a stream with the SAME algorithm the
// reported checksum uses (the ship leg's peer-side re-check calls this).
func LedgerBackupChecksumStream(r io.Reader) (string, int64, error) {
	h := sha256.New()
	size, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("ledger backup: checksum stream: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// ---------------------------------------------------------------------------
// ⑤ ship — the memory_peers asset channel
// ---------------------------------------------------------------------------

// LedgerBackupEnvelopeHeader is the wire header that precedes the raw
// artifact bytes on the ship leg. Size and checksum travel WITH the bytes so
// the peer can prove what it received, rather than trusting the name.
type LedgerBackupEnvelopeHeader struct {
	Version      int    `json:"version"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
}

// LedgerBackupReceipt is the peer's answer: what IT stored, and its own
// read-only verification of that file.
type LedgerBackupReceipt struct {
	Peer string `json:"peer"`
	Path string `json:"path"`
	// Filename echoes the name the initiator announced — the initiator
	// refuses a receipt about some other file.
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
	Tasks    int64  `json:"tasks"`
	// Consistency is the peer's quick_check verdict on its own copy.
	Consistency string `json:"consistency"`
	// AlreadyHeld marks a landing over a byte-identical artifact the peer
	// already had (idempotent re-ship: the peer keeps its copy).
	AlreadyHeld bool `json:"already_held"`
}

// ledgerBackupSSHExchangeHook replaces the ssh transport when non-nil (the
// established syncSSHExchangeHook pattern): tests run the PEER's real
// receive handler in-process, so the full ship path is exercised with the
// byte pipe abstracted. Production never sets it (nil → the ssh exec), and
// that matters for size: the hook path materializes the whole envelope,
// the production path streams the artifact and never buffers it.
var ledgerBackupSSHExchangeHook func(peer string, envelope []byte) ([]byte, error)

// LedgerBackupShip sends one verified artifact to peer over ONE ssh leg and
// returns the peer's receipt. The peer's own answer is the success criterion
// (its read-only open, count, checksum) — never the ssh exit status alone
// (Rule 15b).
func LedgerBackupShip(peer, artifactPath string) (*LedgerBackupShipResult, error) {
	started := time.Now()
	if err := syncAssetsPeerWhitelisted(peer, nil); err != nil {
		return nil, fmt.Errorf("ledger backup: %w", err)
	}
	sum, size, err := ledgerBackupChecksum(artifactPath)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(artifactPath)
	header, err := json.Marshal(LedgerBackupEnvelopeHeader{
		Version:      LedgerBackupEnvelopeVersion,
		Filename:     name,
		Size:         size,
		Checksum:     sum,
		ChecksumAlgo: LedgerBackupChecksumAlgo,
	})
	if err != nil {
		return nil, fmt.Errorf("ledger backup: ship: marshal header: %w", err)
	}

	reply, err := ledgerBackupShipExchange(peer, header, artifactPath)
	if err != nil {
		return nil, err
	}
	var receipt LedgerBackupReceipt
	if err := json.Unmarshal(bytes.TrimSpace(reply), &receipt); err != nil {
		return nil, fmt.Errorf("ledger backup: ship: peer %s receipt: %w (raw: %.200q)", peer, err, string(reply))
	}
	if receipt.Filename != name {
		return nil, fmt.Errorf("ledger backup: ship: peer %s stored %q, expected %q — refusing the receipt",
			peer, receipt.Filename, name)
	}
	if receipt.Checksum != sum || receipt.Size != size {
		return nil, fmt.Errorf("ledger backup: ship: peer %s holds %s (size %d) but we sent %s (size %d) — the landing differs from the artifact",
			peer, receipt.Checksum, receipt.Size, sum, size)
	}
	return &LedgerBackupShipResult{Peer: peer, Receipt: &receipt, DurationSec: time.Since(started).Seconds()}, nil
}

// ledgerBackupShipExchange runs the leg: the test seam when installed, the
// ssh stream otherwise.
func ledgerBackupShipExchange(peer string, header []byte, artifactPath string) ([]byte, error) {
	if ledgerBackupSSHExchangeHook != nil {
		envelope, err := ledgerBackupEnvelopeBytes(header, artifactPath)
		if err != nil {
			return nil, err
		}
		return ledgerBackupSSHExchangeHook(peer, envelope)
	}
	return ledgerBackupSSHCommand(peer, header, artifactPath)
}

// ledgerBackupEnvelopeBytes materializes header + terminator + artifact
// (the seam path only).
func ledgerBackupEnvelopeBytes(header []byte, artifactPath string) ([]byte, error) {
	body, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: ship: read artifact %q: %w", artifactPath, err)
	}
	envelope := make([]byte, 0, len(header)+len(ledgerBackupEnvelopeHeaderTerm)+len(body))
	envelope = append(envelope, header...)
	envelope = append(envelope, ledgerBackupEnvelopeHeaderTerm...)
	envelope = append(envelope, body...)
	return envelope, nil
}

// ledgerBackupSSHCommand runs the ssh leg and returns the peer's stdout (the
// JSON receipt). The artifact STREAMS through a pipe, so a 100MB snapshot
// never sits in this process's memory.
func ledgerBackupSSHCommand(peer string, header []byte, artifactPath string) ([]byte, error) {
	// Peer-alias guard BEFORE anything is sent: the option-shaped alias
	// (-oProxyCommand=…) must be refused on every path.
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ledgerBackupSSHTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+syncSSHConnectTimeout,
		peer,
		// ONE ssh argv element (the remote command string) — the quoted form
		// is load-bearing (syncRemoteGopi comment, memory_sync_task.go).
		ledgerBackupServeArgs())
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(ledgerBackupWriteEnvelope(pw, header, artifactPath))
	}()
	cmd.Stdin = pr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// Unblock the writer if the peer closed early (its refusal is the real
	// error we report below).
	_ = pr.CloseWithError(runErr) //stylegate:ignore closes the read end so the writer goroutine cannot block; the transfer's outcome is reported from runErr
	if runErr != nil {
		errText := strings.TrimSpace(stderr.String())
		if syncAssetsStalePeerHint(errText) || ledgerBackupMissingVerbHint(errText) {
			return nil, fmt.Errorf("ssh %s: %w (stderr: %s)\n%s", peer, runErr, errText, syncAssetsUpgradeHint(peer))
		}
		return nil, fmt.Errorf("ledger backup: ssh %s: %w (stderr: %s)", peer, runErr, errText)
	}
	return stdout.Bytes(), nil
}

// ledgerBackupMissingVerbHint reports whether the ssh stderr marks the peer's
// binary as predating THIS sub-verb: the peer's `gopi memory` group answers
// `unknown subcommand "backup"`. That is the same class of version skew the
// asset channel detects ("unknown flag --serve-assets"), so it gets the same
// actionable upgrade hint — an operator must not have to decode a bare ssh
// failure to learn the peer needs a rebuild. Measured live 2026-09-19: the
// first real ship to the configured peer failed exactly this way (the peer ran
// master's gopi) and the hint now names the fix.
func ledgerBackupMissingVerbHint(stderr string) bool {
	return strings.Contains(stderr, `unknown subcommand "`+LedgerBackupVerb+`"`)
}

// ledgerBackupWriteEnvelope writes the header line and then streams the
// artifact into w.
func ledgerBackupWriteEnvelope(w io.Writer, header []byte, artifactPath string) error {
	if _, err := w.Write(append(header, ledgerBackupEnvelopeHeaderTerm...)); err != nil {
		return fmt.Errorf("ledger backup: ship: write envelope header: %w", err)
	}
	f, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("ledger backup: ship: open artifact %q: %w", artifactPath, err)
	}
	defer f.Close() //stylegate:ignore read-only handle closed at function end
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("ledger backup: ship: stream artifact %q: %w", artifactPath, err)
	}
	return nil
}

// ledgerBackupEnvelopeHeaderTerm ends the JSON header line and starts the
// raw artifact bytes.
var ledgerBackupEnvelopeHeaderTerm = []byte{'\n'}

// ledgerBackupServeArgs is the peer-side argv the initiator runs remotely
// (the receive handler). It is ONE ssh argv element — a quoted remote
// command string. The peer resolves its OWN backup dir and its OWN ledger,
// exactly as the asset serve leg does: a local --dir/--db override never
// leaks into the remote's argv.
func ledgerBackupServeArgs() string {
	return syncRemoteGopi + " " + CliVerbMemoryGroup + " " + LedgerBackupVerb + " --" + LedgerBackupServeFlag
}

// ---------------------------------------------------------------------------
// The PEER side — receive one artifact, prove it, answer a receipt.
// ---------------------------------------------------------------------------

// LedgerBackupReceive is the peer half of a ship (`gopi memory backup
// --serve`): parse the header, write the artifact into OUR backup dir under
// the initiator's name, then PROVE it (size, sha256 over the bytes actually
// received, read-only open, count) before answering. A short or altered
// transfer is refused and no artifact is left behind — a truncated .db in
// backups/ is exactly the file that gets trusted later.
//
// An empty dir resolves like the initiator's does (ResolveLedgerBackupDir).
func LedgerBackupReceive(stdin []byte, dir string) (*LedgerBackupReceipt, error) {
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = ResolveLedgerBackupDir()
		if err != nil {
			return nil, err
		}
	}
	reader := bufio.NewReader(bytes.NewReader(stdin))
	headerLine, err := reader.ReadString(ledgerBackupEnvelopeHeaderTerm[0])
	if err != nil {
		return nil, fmt.Errorf("ledger backup: serve: read envelope header: %w", err)
	}
	if len(headerLine) > ledgerBackupEnvelopeHeaderMax {
		return nil, fmt.Errorf("ledger backup: serve: envelope header exceeds %d bytes — refusing the stream", ledgerBackupEnvelopeHeaderMax)
	}
	var header LedgerBackupEnvelopeHeader
	if err := json.Unmarshal([]byte(strings.TrimSpace(headerLine)), &header); err != nil {
		return nil, fmt.Errorf("ledger backup: serve: parse envelope header: %w", err)
	}
	if header.Version != LedgerBackupEnvelopeVersion {
		return nil, fmt.Errorf("ledger backup: serve: envelope version %d, this gopi speaks %d — upgrade the sender or this node",
			header.Version, LedgerBackupEnvelopeVersion)
	}
	if header.ChecksumAlgo != LedgerBackupChecksumAlgo {
		return nil, fmt.Errorf("ledger backup: serve: unknown checksum algorithm %q (this gopi speaks %s)",
			header.ChecksumAlgo, LedgerBackupChecksumAlgo)
	}
	if !LedgerBackupArtifactNameValid(header.Filename) {
		return nil, fmt.Errorf("ledger backup: serve: refused artifact name %q — want %s",
			header.Filename, LedgerBackupFileStampLayoutDoc)
	}
	if err := scratch.AssertPathOffRootFS(dir); err != nil {
		return nil, fmt.Errorf("ledger backup: serve: backup directory refused: %w", err)
	}
	if err := os.MkdirAll(dir, ledgerBackupDirPerm); err != nil {
		return nil, fmt.Errorf("ledger backup: serve: create backup directory %q: %w", dir, err)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("ledger backup: serve: read artifact body: %w", err)
	}
	sum, size, err := LedgerBackupChecksumStream(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if size != header.Size {
		return nil, fmt.Errorf("ledger backup: serve: received %d bytes, header announced %d — refusing a short transfer (nothing written)",
			size, header.Size)
	}
	if sum != header.Checksum {
		return nil, fmt.Errorf("ledger backup: serve: received %s checksum %s, header announced %s — refusing a corrupt transfer (nothing written)",
			LedgerBackupChecksumAlgo, sum, header.Checksum)
	}

	final := filepath.Join(dir, header.Filename)
	staging := ledgerBackupStagingPath(final)
	if err := ledgerBackupClearStaging(staging); err != nil {
		return nil, err
	}
	if err := os.WriteFile(staging, body, ledgerBackupFilePerm); err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, fmt.Errorf("ledger backup: serve: write artifact %q: %w", staging, err)
	}
	verification, err := LedgerBackupVerify(staging)
	if err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, fmt.Errorf("ledger backup: serve: verify received artifact: %w", err)
	}

	alreadyHeld := false
	if prev, _, prevErr := ledgerBackupChecksum(final); prevErr == nil && prev == sum {
		// This exact artifact is already held — keep ours, drop the copy.
		ledgerBackupDiscardStaging(staging)
		alreadyHeld = true
	} else if err := os.Rename(staging, final); err != nil {
		ledgerBackupDiscardStaging(staging)
		return nil, fmt.Errorf("ledger backup: serve: land artifact %q: %w", final, err)
	}

	return &LedgerBackupReceipt{
		Peer:        syncAssetsNodeName(),
		Path:        final,
		Filename:    header.Filename,
		Size:        verification.Size,
		Checksum:    sum,
		Tasks:       verification.Tasks,
		Consistency: verification.Consistency,
		AlreadyHeld: alreadyHeld,
	}, nil
}
