package tools

// ledger_readonly_walsidecar_test.go — the regression guard for a legacy ledger
// open DESTROYING the -wal/-shm sidecars it found.
//
// THE OBSERVATION (measured live 2026-10-13, task 01a13490). A COPIED ledger
// (memory.db + memory.db-wal + memory.db-shm) that a pure-read gopi verb touches
// once comes back as memory.db ALONE: both sidecars are gone. Reproduced on the
// operator's own ledger copy with two different pure-read verbs — `task show`
// (read-only opener) and `task list` (write opener) behave IDENTICALLY, which is
// itself the finding: the deleter is the ledger OPEN/CLOSE layer, not the read
// face that happens to have opened it. strace pins the removal to the gopi
// process's own syscalls (evidence: loadbearing-*.strace, session dir):
//
//	fcntl(7, F_SETLK, {l_type=F_WRLCK, l_start=128, l_len=1}) = 0   // fd 7 = the -wal
//	ftruncate(7, 3) = 0
//	unlink(".../memory.db-shm") = 0
//	unlink(".../memory.db-wal") = 0
//
// The same copy read by an INDEPENDENT SQLite (python3's sqlite3 module,
// file:...?mode=ro) keeps both sidecars and returns the same row counts — so this
// is NOT a property of reading a copied WAL set, and NOT something a reader is
// entitled to do to a ledger it never wrote.
//
// WHY THE LEDGER HERE IS DEVICE-BACKED (this is the same trap the task-show guard
// documents, and the first version of THIS test fell into it). On /dev/shm the
// fixture's ledger is built in ensureTasksSchema's autocommit arm and SQLite never
// creates a -wal at all — the first run of this test failed on `no such file or
// directory: memory.db-wal`, i.e. it was asserting about a file the fixture cannot
// produce. A ledger on a block device (the operator's own shape) is the one that
// carries WAL sidecars, so the fixture takes its directory from
// testutil.TestDeviceScratchDir and SKIPS (never passes) where no device exists.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The three files of a SQLite WAL ledger. The suffixes are the driver's own
// (-wal/-shm), named once here (Rule 1b) because the fixture and every assertion
// must agree on exactly the same files.
const (
	walSidecarWALSuffix = "-wal"
	walSidecarSHMSuffix = "-shm"
)

// walSidecarLedgerFile is the ledger name inside the fixture's own directory.
const walSidecarLedgerFile = "memory.db"

// walSidecarSnapshot copies a live ledger SET (db + whichever sidecars are
// present) into a fresh directory — the copy the operator had in front of them —
// and returns the copied ledger's path plus the number of sidecars it brought.
//
// The source handle is deliberately still OPEN when this runs: a writer that has
// closed checkpoints its WAL away, which would leave the copy with no load-bearing
// -wal to lose (measured: on /dev/shm, where the fixture's ledger has no -wal at
// all, the first version of this test failed rather than reported).
func walSidecarSnapshot(t *testing.T, src, dstDir string) (string, int) {
	t.Helper()
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	copied := 0
	for _, suffix := range []string{"", walSidecarWALSuffix, walSidecarSHMSuffix} {
		blob, err := os.ReadFile(src + suffix)
		if err != nil {
			if suffix == "" {
				t.Fatalf("snapshot read %s: %v", src, err)
			}
			continue // no such sidecar in the source set — nothing to copy
		}
		if err := os.WriteFile(filepath.Join(dstDir, walSidecarLedgerFile+suffix), blob, 0o644); err != nil {
			t.Fatalf("snapshot write %s%s: %v", walSidecarLedgerFile, suffix, err)
		}
		copied++
	}
	return filepath.Join(dstDir, walSidecarLedgerFile), copied
}

// walSidecarExists reports whether the ledger's named sidecar is present.
func walSidecarExists(path, suffix string) bool {
	_, err := os.Stat(path + suffix)
	return err == nil
}

// walSidecarRowIDs reads every task_id through the file-level reader the fixture
// also uses, so "the row set survived" is asserted against the ledger ITSELF and
// not against a rendered digest.
func walSidecarRowIDs(t *testing.T, path string) []string {
	t.Helper()
	db, err := openTasksDBReadOnly(context.Background(), path)
	if err != nil {
		t.Fatalf("read-only open %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore test-owned handle; released when this helper returns
	rows, err := db.Query(`SELECT task_id FROM tasks ORDER BY task_id`)
	if err != nil {
		t.Fatalf("list task_ids in %s: %v", path, err)
	}
	defer rows.Close() //stylegate:ignore read-only cursor closed at the end of this helper
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan task_id: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate task_ids: %v", err)
	}
	return out
}

// walSidecarFixture builds a DEVICE-BACKED ledger carrying one row, keeps its
// writer handle open (so the ledger carries WAL sidecars rather than a checkpointed
// WAL), and returns the ledger path. It skips — never passes — where the host
// offers no block-backed scratch directory.
func walSidecarFixture(t *testing.T) (string, func()) {
	t.Helper()
	dir := testutil.TestDeviceScratchDir(t, "wal-sidecar")
	if memBacked, description := testutil.ScratchRootFilesystem(dir); memBacked {
		t.Skipf("the fixture's ledger must be on a block device: a memory filesystem's ledger "+
			"carries no -wal to lose, so the subject would not exist: %s", description)
	}
	path := filepath.Join(dir, walSidecarLedgerFile)
	t.Setenv(config.MemoryDBEnv, path)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("build the device-backed ledger: %v", err)
	}
	// The ledger must be in WAL — the mode enableWAL puts the operator's own
	// memory.db in, and a PERSISTENT header property, which is exactly why a COPY
	// of that ledger carries sidecars. Without it SQLite keeps the default rollback
	// journal and the fixture would have no -wal for a reader to destroy (measured:
	// this test skipped at `1 files copied` before this line existed).
	if err := enableWAL(context.Background(), db); err != nil {
		db.Close() //stylegate:ignore the fixture's own handle; the test cannot run without its ledger
		t.Fatalf("put the fixture's ledger in WAL: %v", err)
	}
	if _, err := TaskNew("a row whose commit lands in the ledger's WAL", walSidecarMarkerID, "", ""); err != nil {
		db.Close() //stylegate:ignore the fixture's own handle; the test cannot run without its ledger
		t.Fatalf("seed the row: %v", err)
	}
	// The writer handle stays OPEN through the snapshot (the caller releases it):
	// a closed handle checkpoints its WAL away, and the copy would then carry no
	// -wal at all.
	return path, func() { db.Close() } //stylegate:ignore the fixture's writer handle, released by the caller
}

// walSidecarMarkerID is the row the fixture seeds — named (Rule 1b) because the
// seeding and the survival assertion must be about the same row.
const walSidecarMarkerID = "t-walsidecar-marker"

// TestReadOnlyOpenPreservesTheSnapshotSidecars is the guard proper: a ledger COPY
// that carries sidecars must still carry them after a read-only open/close, and
// must answer with the SAME row set.
func TestReadOnlyOpenPreservesTheSnapshotSidecars(t *testing.T) {
	src, release := walSidecarFixture(t)
	defer release()

	snap, copied := walSidecarSnapshot(t, src, filepath.Join(filepath.Dir(src), "snapshot"))
	if copied < 2 { // the db plus at least one sidecar
		t.Skipf("the source ledger carries no WAL sidecar (%d files copied) — there is nothing here for a "+
			"reader to destroy, so this host cannot answer the question (Rule 15: report, never assume)", copied)
	}
	before := walSidecarRowIDs(t, snap)
	if len(before) == 0 {
		t.Fatalf("the snapshot has no rows to preserve — a ledger the fixture failed to seed proves nothing")
	}
	hadWAL := walSidecarExists(snap, walSidecarWALSuffix)
	hadSHM := walSidecarExists(snap, walSidecarSHMSuffix)

	db, err := openTasksDBReadOnly(context.Background(), snap)
	if err != nil {
		t.Fatalf("read-only open of the snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the read-only handle: %v", err)
	}

	if hadWAL && !walSidecarExists(snap, walSidecarWALSuffix) {
		t.Errorf("the read-only open DELETED %s%s — a pure reader removed a file of a ledger it never wrote",
			filepath.Base(snap), walSidecarWALSuffix)
	}
	if hadSHM && !walSidecarExists(snap, walSidecarSHMSuffix) {
		t.Errorf("the read-only open DELETED %s%s", filepath.Base(snap), walSidecarSHMSuffix)
	}

	after := walSidecarRowIDs(t, snap)
	if len(after) != len(before) {
		t.Errorf("the snapshot answered with %d rows before the read-only open and %d after: a reader "+
			"destroyed ledger content (%v → %v)", len(before), len(after), before, after)
	}
}
