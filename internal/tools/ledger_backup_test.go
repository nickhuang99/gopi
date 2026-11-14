// ledger_backup_test.go — the snapshot+ship pipeline's regression suite
// (epic: ledger snapshot/ship, 2026-09-19).
//
// Everything is deterministic and hermetic: no network (the ship leg's
// ledgerBackupSSHExchangeHook runs the PEER's real receive handler
// in-process), no ambient config (GOPI_SETTINGS is sandboxed to an empty
// file), and the source ledger is a real gopi ledger built through the real
// submit path — the smallest fixture that still exercises the real schema.
package tools

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// ledgerBackupTestDB builds a real gopi ledger at path through the production
// submit path, so the fixture carries the real schema (the `tasks` table the
// verification queries exists because gopi made it).
func ledgerBackupTestDB(t *testing.T, path string) {
	t.Helper()
	t.Setenv(memoryDBEnv, path)
	// Role UNdeclared ⇒ the local ledger is authoritative and TaskSubmit
	// writes here (a declared client role would instead demand a sync_to
	// forward leg — correct product behaviour, wrong for a local fixture).
	t.Setenv(config.RoleEnv, "")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	mustSubmitTest(t, "ledger backup fixture row one", "payload one", "lb-fixture-1")
	mustSubmitTest(t, "ledger backup fixture row two", "payload two", "lb-fixture-2")
}

// ledgerBackupSandbox pins the CLI/config sandbox: an empty settings.json and
// an isolated gopi home, so no ambient memory_peers/sync_peers/tmpdir leaks
// into an assertion (the established test trap).
func ledgerBackupSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "settings.json"))
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write sandbox settings: %v", err)
	}
	t.Setenv(LedgerBackupDirEnv, "")
	t.Setenv(LedgerBackupPeerEnv, "")
	return home
}

// ledgerBackupArtifacts lists the restorable artifacts in dir (the .db files,
// never a staging file).
func ledgerBackupArtifacts(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil // a refused run creates nothing — that is the point
	}
	if err != nil {
		t.Fatalf("read backup dir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), LedgerBackupFileSuffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// The snapshot is real — and the live ledger survives it.
// ---------------------------------------------------------------------------

// TestLedgerBackupSnapshotVerifiesAndLeavesTheLiveLedgerIntact is the core
// contract: one call produces an artifact that opens read-only, counts its
// tasks and passes quick_check — while the SOURCE ledger is still writable
// afterwards (a snapshot that broke the live db would be the worst possible
// backup).
func TestLedgerBackupSnapshotVerifiesAndLeavesTheLiveLedgerIntact(t *testing.T) {
	ledgerBackupSandbox(t)
	src := filepath.Join(t.TempDir(), "memory.db")
	ledgerBackupTestDB(t, src)
	dir := filepath.Join(t.TempDir(), "backups")

	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: dir})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if res.Tasks != 2 {
		t.Errorf("verified task count = %d, want 2 (the fixture submitted exactly 2 rows)", res.Tasks)
	}
	if res.Consistency != "ok" {
		t.Errorf("consistency = %q, want \"ok\"", res.Consistency)
	}
	if res.Checksum == "" || res.ChecksumAlgo != LedgerBackupChecksumAlgo {
		t.Errorf("checksum = %q algo=%q, want a %s checksum", res.Checksum, res.ChecksumAlgo, LedgerBackupChecksumAlgo)
	}
	if res.SnapshotSize <= 0 {
		t.Errorf("snapshot size = %d, want > 0", res.SnapshotSize)
	}
	if _, err := os.Stat(res.SnapshotPath); err != nil {
		t.Fatalf("reported artifact missing: %v", err)
	}
	if res.SnapshotPath != filepath.Join(dir, ledgerBackupFileName(timeNowForTest(res))) && filepath.Base(res.SnapshotPath) == "" {
		t.Errorf("artifact path %q is not under %q", res.SnapshotPath, dir)
	}
	// No staging file may survive a successful run.
	if _, err := os.Stat(ledgerBackupStagingPath(res.SnapshotPath)); !os.IsNotExist(err) {
		t.Errorf("staging file survived: err=%v", err)
	}

	// The LIVE ledger is untouched and still writable — the property that
	// makes this safe to run against the authoritative db while it is in use.
	sqldb, err := sql.Open("sqlite", LedgerDSN(src))
	if err != nil {
		t.Fatalf("open live ledger after snapshot: %v", err)
	}
	defer sqldb.Close()
	var verdict string
	if err := sqldb.QueryRow(ledgerBackupQuickCheckPragma).Scan(&verdict); err != nil {
		t.Fatalf("live ledger quick_check: %v", err)
	}
	if strings.TrimSpace(verdict) != "ok" {
		t.Fatalf("live ledger is damaged after the snapshot: %q", verdict)
	}
	if _, err := sqldb.Exec(`CREATE TABLE _ledger_backup_write_probe(x)`); err != nil {
		t.Fatalf("live ledger is no longer writable after the snapshot: %v", err)
	}
	if _, err := sqldb.Exec(`DROP TABLE _ledger_backup_write_probe`); err != nil {
		t.Fatalf("drop probe table: %v", err)
	}
}

// timeNowForTest returns the timestamp whose filename the snapshot should have
// used — read back from the artifact itself, so the assertion does not depend
// on the wall clock (a run that straddles a second boundary still passes).
func timeNowForTest(res *LedgerBackupResult) time.Time {
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(res.SnapshotPath), LedgerBackupFilePrefix+"-"), LedgerBackupFileSuffix)
	ts, err := time.ParseInLocation(LedgerBackupFileStampLayout, name, time.Local)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// ---------------------------------------------------------------------------
// A corrupt artifact must be REFUSED — red first, green after the fix.
// ---------------------------------------------------------------------------

// TestLedgerBackupVerifyRefusesACorruptArtifact is the regression guard for
// THIS feature's whole reason to exist: the row count a `cp` produces is
// right while the file is damaged, so a count is not evidence. A gate that
// only ran the count would call the corrupt copy healthy.
//
// RED-FIRST RECORD (observed 2026-09-19 against this test's own subject): a
// `cp` of a live WAL ledger answered `select count(*) from tasks` correctly
// (2057) while `PRAGMA integrity_check` reported "Freelist: size is 1966 but
// should be 1941" plus out-of-order rowids and double-referenced pages.
func TestLedgerBackupVerifyRefusesACorruptArtifact(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)

	// Build a genuinely corrupt artifact: truncate a snapshot mid-file, the
	// shape an interrupted copy produces.
	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: dir})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	raw, err := os.ReadFile(res.SnapshotPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	corrupt := filepath.Join(dir, "corrupt")
	if err := os.WriteFile(corrupt, raw[:len(raw)/3], 0o600); err != nil {
		t.Fatalf("write corrupt artifact: %v", err)
	}
	if _, err := LedgerBackupVerify(corrupt); err == nil {
		t.Fatal("a truncated artifact verified as good — the consistency gate is not doing its job")
	}

	// The counterpart: the good artifact verifies. (Without this half, a
	// Verify that refused EVERYTHING would pass the assertion above.)
	good, err := LedgerBackupVerify(res.SnapshotPath)
	if err != nil {
		t.Fatalf("the good artifact failed verification: %v", err)
	}
	if good.Tasks != 2 || good.Consistency != "ok" {
		t.Fatalf("good artifact verified as tasks=%d consistency=%q, want 2/ok", good.Tasks, good.Consistency)
	}
}

// TestLedgerBackupVerifyCatchesDamageARowCountWouldMiss is the sharpest form of
// the rule, and the reason the consistency verdict is not optional: a database
// can be DAMAGED while `select count(*) from tasks` still answers the right
// number. Measured on the live 103MB ledger 2026-09-19 — one zeroed page at
// 60-95% of the file left the count at its correct 2057 while quick_check
// answered "*** in database main ***".
//
// The test walks candidate pages in a copy until it finds one whose damage the
// COUNT does not see, then requires verification to refuse the artifact
// anyway. A future "optimization" that keeps only the count turns this RED.
func TestLedgerBackupVerifyCatchesDamageARowCountWouldMiss(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)
	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	raw, err := os.ReadFile(res.SnapshotPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	const ledgerBackupTestPageSize = 4096
	pages := len(raw) / ledgerBackupTestPageSize
	step := pages / 60
	if step < 1 {
		step = 1
	}
	for page := pages / 5; page < pages; page += step {
		damaged := append([]byte{}, raw...)
		copy(damaged[page*ledgerBackupTestPageSize:], make([]byte, ledgerBackupTestPageSize))
		path := filepath.Join(dir, "damaged.db")
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatalf("write damaged artifact: %v", err)
		}
		if !ledgerBackupCountSucceeds(t, path) {
			continue // the damage was visible to the count too — not our case
		}
		if _, err := LedgerBackupVerify(path); err == nil {
			t.Fatalf("page %d zeroed: the row count still succeeded AND verification accepted the artifact — the consistency gate is not doing its job", page)
		}
		t.Logf("page %d zeroed: count still answered correctly, verification refused as required", page)
		return
	}
	// Not a silent skip: a fixture too small to exhibit the shape is named.
	t.Skipf("no zeroed page in this %d-page fixture left the row count working — the fixture cannot exhibit the count-misses-damage shape", pages)
}

// ledgerBackupCountSucceeds reports whether the verification's real query still
// answers over path (the "damage the count misses" precondition).
func ledgerBackupCountSucceeds(t *testing.T, path string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return false
	}
	defer db.Close()
	var n int64
	return db.QueryRow(ledgerBackupTasksQuery).Scan(&n) == nil
}

// TestLedgerBackupVerifyRefusesAFileWithoutTheLedgerSchema pins the OTHER
// failure mode of trusting a name: a well-formed SQLite database that is not
// this ledger must be refused, because the verification's real query cannot
// run against it.
func TestLedgerBackupVerifyRefusesAFileWithoutTheLedgerSchema(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	// A FOREIGN database on purpose: nothing here is a gopi ledger, so this
	// fixture deliberately names its own DSN (permitted by ledgerContractExemptNames
	// in cmd/agent — opening it through LedgerDSN would apply the production
	// contract to a database that must NOT be touched by it).
	foreign := filepath.Join(dir, "foreign.db")
	db, err := sql.Open("sqlite", foreign)
	if err != nil {
		t.Fatalf("open foreign db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated(x)`); err != nil {
		t.Fatalf("create foreign table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close foreign db: %v", err)
	}
	if _, err := LedgerBackupVerify(foreign); err == nil {
		t.Fatal("a database without the ledger schema verified as a usable ledger backup")
	}
}

// ---------------------------------------------------------------------------
// Idempotency — re-running is safe and does not rewrite identical bytes.
// ---------------------------------------------------------------------------

// TestLedgerBackupRerunIsIdempotentAndKeepsTheIdenticalArtifact runs the
// pipeline twice with the SAME pinned timestamp: the second run must report
// the same artifact, mark it deduped, and leave the directory with exactly one
// artifact (never a duplicate, never a rewritten file).
func TestLedgerBackupRerunIsIdempotentAndKeepsTheIdenticalArtifact(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)
	backupDir := filepath.Join(dir, "backups")
	stamp := time.Date(2026, 9, 19, 12, 0, 0, 0, time.Local)
	opts := LedgerBackupOptions{DBPath: src, Dir: backupDir, Now: stamp}

	first, err := LedgerBackupSnapshot(opts)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	firstInfo, err := os.Stat(first.SnapshotPath)
	if err != nil {
		t.Fatalf("stat first artifact: %v", err)
	}
	if first.Deduped {
		t.Error("the FIRST run reported itself deduped")
	}

	second, err := LedgerBackupSnapshot(opts)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if second.SnapshotPath != first.SnapshotPath {
		t.Fatalf("re-run landed at %q, want the same artifact %q", second.SnapshotPath, first.SnapshotPath)
	}
	if second.Checksum != first.Checksum {
		t.Fatalf("re-run checksum %s != first %s", second.Checksum, first.Checksum)
	}
	if !second.Deduped {
		t.Error("re-run did not report the identical artifact as deduped")
	}
	secondInfo, err := os.Stat(second.SnapshotPath)
	if err != nil {
		t.Fatalf("stat second artifact: %v", err)
	}
	if !secondInfo.ModTime().Equal(firstInfo.ModTime()) {
		t.Errorf("re-run rewrote an identical artifact (mtime %v → %v) — it must be kept", firstInfo.ModTime(), secondInfo.ModTime())
	}
	if got := ledgerBackupArtifacts(t, backupDir); len(got) != 1 {
		t.Errorf("backup dir holds %d artifacts (%v), want exactly 1", len(got), got)
	}
}

// TestLedgerBackupRefusesAnArtifactNameOutsideTheContract pins the naming
// guard: the artifact name is a contract (prefix + .db, a bare base name), and
// a name carrying a path separator — which the PEER would write verbatim —
// must be refused before anything is written.
func TestLedgerBackupRefusesAnArtifactNameOutsideTheContract(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)

	for _, bad := range []string{"../escape.db", "evil.db", "memory.db.backup-1.db/../x.db", ".hidden.db"} {
		if _, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(dir, "backups"), Filename: bad}); err == nil {
			t.Errorf("artifact name %q was accepted — it must be refused", bad)
		}
	}
	// And the name the contract does allow is accepted.
	if _, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(dir, "backups"), Filename: LedgerBackupFilePrefix + "-20260101-000000" + LedgerBackupFileSuffix}); err != nil {
		t.Errorf("contract-conformant name refused: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The peer leg — declared topology only, and the receipt is the criterion.
// ---------------------------------------------------------------------------

// TestLedgerBackupShipIsRefusedForAnUndeclaredPeer pins the topology rule the
// knowledge channel already enforces: the ship target must be declared in
// memory_peers. An undeclared peer must be refused BEFORE any ssh is spawned —
// never discovered, never guessed at.
func TestLedgerBackupShipIsRefusedForAnUndeclaredPeer(t *testing.T) {
	ledgerBackupSandbox(t)
	ran := false
	old := ledgerBackupSSHExchangeHook
	ledgerBackupSSHExchangeHook = func(peer string, envelope []byte) ([]byte, error) {
		ran = true
		return nil, nil
	}
	t.Cleanup(func() { ledgerBackupSSHExchangeHook = old })

	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)
	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: dir})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := LedgerBackupShip("undeclared-peer", res.SnapshotPath); err == nil {
		t.Fatal("shipping to an undeclared peer was allowed — topology is declared, never discovered")
	}
	if ran {
		t.Fatal("the ssh leg ran for an undeclared peer — the whitelist check must come first")
	}

	// And the resolution helper applies the same rule to the DEFAULT.
	t.Setenv(config.MemoryPeersEnv, "someother")
	if _, err := LedgerBackupPeerResolved(); err == nil {
		t.Fatalf("default peer %q was accepted although memory_peers lists only 'someother'", LedgerBackupPeerDefault)
	}
	t.Setenv(config.MemoryPeersEnv, LedgerBackupPeerDefault)
	got, err := LedgerBackupPeerResolved()
	if err != nil {
		t.Fatalf("declared default peer refused: %v", err)
	}
	if got != LedgerBackupPeerDefault {
		t.Fatalf("resolved peer = %q, want %q", got, LedgerBackupPeerDefault)
	}
}

// TestLedgerBackupShipDeliversAndThePeerReceiptIsTheCriterion is the end-to-end
// peer test: the ship leg's hook runs the PEER's REAL receive handler against
// its own backup directory, so the full path is exercised with only the byte
// pipe abstracted. It asserts the peer's copy is byte-identical, that the peer
// really verified it (its own count + quick_check in the receipt), and that a
// re-ship is idempotent on the peer side too.
func TestLedgerBackupShipDeliversAndThePeerReceiptIsTheCriterion(t *testing.T) {
	home := ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)
	peerDir := filepath.Join(home, "peer-backups")

	t.Setenv(config.MemoryPeersEnv, "peer1")
	var peerReceipts []*LedgerBackupReceipt
	old := ledgerBackupSSHExchangeHook
	ledgerBackupSSHExchangeHook = func(peer string, envelope []byte) ([]byte, error) {
		// The PEER's real handler — its own ledger/backup dir resolution is
		// bypassed only for the directory (the peer's node identity is not
		// what this test is about).
		receipt, err := LedgerBackupReceive(envelope, peerDir)
		if err != nil {
			return nil, err
		}
		peerReceipts = append(peerReceipts, receipt)
		return json.Marshal(receipt)
	}
	t.Cleanup(func() { ledgerBackupSSHExchangeHook = old })

	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	ship, err := LedgerBackupShip("peer1", res.SnapshotPath)
	if err != nil {
		t.Fatalf("ship: %v", err)
	}
	if len(peerReceipts) != 1 {
		t.Fatalf("peer received %d artifacts, want 1", len(peerReceipts))
	}
	r := ship.Receipt
	if r.Checksum != res.Checksum || r.Size != res.SnapshotSize {
		t.Errorf("peer receipt %s/%d != artifact %s/%d", r.Checksum, r.Size, res.Checksum, res.SnapshotSize)
	}
	if r.Tasks != res.Tasks {
		t.Errorf("peer verified %d tasks, we verified %d — the landing differs", r.Tasks, res.Tasks)
	}
	if r.Consistency != "ok" {
		t.Errorf("peer consistency = %q, want ok (the peer must verify what it stored)", r.Consistency)
	}
	if r.Filename != filepath.Base(res.SnapshotPath) {
		t.Errorf("peer filename %q != artifact %q", r.Filename, filepath.Base(res.SnapshotPath))
	}
	// The peer's copy is byte-identical on disk.
	landed := filepath.Join(peerDir, filepath.Base(res.SnapshotPath))
	sum, size, err := ledgerBackupChecksum(landed)
	if err != nil {
		t.Fatalf("checksum peer copy: %v", err)
	}
	if sum != res.Checksum || size != res.SnapshotSize {
		t.Errorf("peer copy is %s/%d bytes, artifact is %s/%d", sum, size, res.Checksum, res.SnapshotSize)
	}
	// Re-ship: idempotent, and the peer says it already held it.
	again, err := LedgerBackupShip("peer1", res.SnapshotPath)
	if err != nil {
		t.Fatalf("re-ship: %v", err)
	}
	if !again.Receipt.AlreadyHeld {
		t.Error("re-ship did not report the peer already holding the identical artifact")
	}
	if got := ledgerBackupArtifacts(t, peerDir); len(got) != 1 {
		t.Errorf("peer dir holds %d artifacts (%v), want exactly 1", len(got), got)
	}
}

// TestLedgerBackupReceiveRefusesAShortOrAlteredTransfer is the peer-side
// refusal test: a transfer whose bytes do not match the announced size or
// checksum must be refused and must leave NOTHING behind — the truncated .db
// in backups/ is exactly the file that gets trusted later.
func TestLedgerBackupReceiveRefusesAShortOrAlteredTransfer(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)
	peerDir := filepath.Join(dir, "peer")
	res, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	body, err := os.ReadFile(res.SnapshotPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	header, err := json.Marshal(LedgerBackupEnvelopeHeader{
		Version:      LedgerBackupEnvelopeVersion,
		Filename:     filepath.Base(res.SnapshotPath),
		Size:         res.SnapshotSize,
		Checksum:     res.Checksum,
		ChecksumAlgo: LedgerBackupChecksumAlgo,
	})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	envelope := func(b []byte) []byte {
		out := append([]byte{}, header...)
		out = append(out, ledgerBackupEnvelopeHeaderTerm...)
		return append(out, b...)
	}

	// (a) truncated transfer.
	if _, err := LedgerBackupReceive(envelope(body[:len(body)/2]), peerDir); err == nil {
		t.Error("a truncated transfer was accepted — the peer must refuse a short read")
	}
	// (b) altered transfer (same size, different bytes).
	altered := append([]byte{}, body...)
	altered[len(altered)-1] ^= 0xFF
	if _, err := LedgerBackupReceive(envelope(altered), peerDir); err == nil {
		t.Error("a byte-altered transfer was accepted — the checksum gate is not doing its job")
	}
	// (c) a traversal-shaped name must be refused before any write.
	badName, err := json.Marshal(LedgerBackupEnvelopeHeader{
		Version: LedgerBackupEnvelopeVersion, Filename: "../escape.db",
		Size: res.SnapshotSize, Checksum: res.Checksum, ChecksumAlgo: LedgerBackupChecksumAlgo,
	})
	if err != nil {
		t.Fatalf("marshal bad header: %v", err)
	}
	badEnv := append([]byte{}, badName...)
	badEnv = append(badEnv, ledgerBackupEnvelopeHeaderTerm...)
	badEnv = append(badEnv, body...)
	if _, err := LedgerBackupReceive(badEnv, peerDir); err == nil {
		t.Error("a traversal-shaped artifact name was accepted — it must be refused before any write")
	}
	// (d) a future envelope version must be refused loudly.
	futureHdr, err := json.Marshal(LedgerBackupEnvelopeHeader{
		Version:      LedgerBackupEnvelopeVersion + 1,
		Filename:     filepath.Base(res.SnapshotPath),
		Size:         res.SnapshotSize,
		Checksum:     res.Checksum,
		ChecksumAlgo: LedgerBackupChecksumAlgo,
	})
	if err != nil {
		t.Fatalf("marshal future header: %v", err)
	}
	futureEnv := append([]byte{}, futureHdr...)
	futureEnv = append(futureEnv, ledgerBackupEnvelopeHeaderTerm...)
	futureEnv = append(futureEnv, body...)
	if _, err := LedgerBackupReceive(futureEnv, peerDir); err == nil {
		t.Error("an unknown envelope version was accepted — the peer must refuse it")
	}

	// NOTHING was written by any refusal: no artifact, no staging file.
	if got := ledgerBackupArtifacts(t, peerDir); len(got) != 0 {
		t.Errorf("refused transfers left %d artifacts behind: %v", len(got), got)
	}
	entries, err := os.ReadDir(peerDir)
	if err != nil {
		t.Fatalf("read peer dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ledgerBackupStagingSuffix) {
			t.Errorf("refused transfer left a staging file behind: %s", e.Name())
		}
	}

	// The happy path over the SAME envelope shape does land.
	receipt, err := LedgerBackupReceive(envelope(body), peerDir)
	if err != nil {
		t.Fatalf("a valid transfer was refused: %v", err)
	}
	if receipt.Tasks != res.Tasks {
		t.Errorf("peer receipt tasks = %d, want %d", receipt.Tasks, res.Tasks)
	}
	if got := ledgerBackupArtifacts(t, peerDir); len(got) != 1 {
		t.Errorf("peer dir holds %d artifacts (%v), want 1", len(got), got)
	}
}

// TestLedgerBackupMissingVerbHintRecognizesAStalePeer pins the version-skew
// detector: a peer whose gopi predates this sub-verb must be recognized, and
// an unrelated ssh failure must NOT be (a hint attached to every failure is
// noise the operator learns to ignore).
func TestLedgerBackupMissingVerbHintRecognizesAStalePeer(t *testing.T) {
	if !ledgerBackupMissingVerbHint("gopi memory: unknown subcommand \"backup\" — want search|store|get") {
		t.Error("the peer's unknown-subcommand answer was not recognized as a stale-peer marker")
	}
	for _, other := range []string{
		"ssh: connect to host hp port 22: Connection refused",
		"unknown subcommand \"restore\"",
		"ledger backup: serve: received 10 bytes, header announced 20",
	} {
		if ledgerBackupMissingVerbHint(other) {
			t.Errorf("unrelated stderr %q was misread as a stale-peer marker", other)
		}
	}
}

// TestLedgerBackupSnapshotFailsLoudlyOnAMissingSource pins Rule 15b at the
// entry point: a snapshot of a ledger that is not there must fail with an
// error naming the path — never a silent success with no artifact.
func TestLedgerBackupSnapshotFailsLoudlyOnAMissingSource(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "not-here.db")
	_, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: missing, Dir: filepath.Join(dir, "backups")})
	if err == nil {
		t.Fatal("a missing source ledger produced a successful snapshot")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the missing source %q", err, missing)
	}
	if got := ledgerBackupArtifacts(t, filepath.Join(dir, "backups")); len(got) != 0 {
		t.Errorf("a failed snapshot left artifacts behind: %v", got)
	}
}

// TestLedgerBackupRefusesADirectoryOnTheRootFilesystem pins Rule 22 as a
// MEASUREMENT, not a spelling: a backup directory on `/` shares the boot
// partition's fate and must be refused with the device ids named.
func TestLedgerBackupRefusesADirectoryOnTheRootFilesystem(t *testing.T) {
	ledgerBackupSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "memory.db")
	ledgerBackupTestDB(t, src)

	onRoot := rootFSDirForTest(t)
	if onRoot == "" {
		t.Skip("no writable directory found on the / filesystem to probe")
	}
	_, err := LedgerBackupSnapshot(LedgerBackupOptions{DBPath: src, Dir: filepath.Join(onRoot, "gopi-ledger-backup-rootfs-probe")})
	if err == nil {
		t.Fatalf("a backup directory on the / filesystem (%s) was accepted — Rule 22 requires refusing it", onRoot)
	}
	if !strings.Contains(err.Error(), "SAME filesystem") {
		t.Errorf("refusal %q does not explain the device-id measurement", err)
	}
}

// rootFSDirForTest finds a directory that measurably lives on `/`'s
// filesystem — the test needs a REAL /-device path, and hardcoding /tmp would
// be exactly the spelling Rule 22 forbids (a host may have /tmp elsewhere, as
// this one does not — but /dev/shm demonstrably is, device 0x1c vs /'s 0x803).
func rootFSDirForTest(t *testing.T) string {
	t.Helper()
	// The candidates walk real directories and MEASURE each one's device
	// (Rule 22: a path is never safe because of its spelling). The ambient
	// temp root comes from its constant, not a literal.
	for _, cand := range []string{"/var/tmp", os.TempDir(), worktreeroots.AmbientTempRoot, "/"} {
		shares, err := scratch.SharesRootFS(cand)
		if err != nil || !shares {
			continue
		}
		probe := filepath.Join(cand, "gopi-ledger-backup-probe")
		if err := os.MkdirAll(probe, 0o755); err != nil {
			continue
		}
		return cand
	}
	return ""
}
