package tools

// memory_search_open_readonly_test.go — the regression guard for memory_search's
// "read-only" entry point, which opened the ledger with openDB(path, true).
//
// THE DEFECT (measured 2026-10-30, task 01a1892d). openDB's `readOnly` argument
// sets PRAGMA query_only, which is a promise about SQL STATEMENTS — not about the
// file. On a device-backed COPY of a real ledger, one open+close through it
// rewrote memory.db (7c08da3c… → 4bbb8467…) and left NO memory.db-wal at all
// (close checkpoints it away). The write was never a query: it was the open
// (LedgerDSN pins `_txlock=immediate`, the first statement is a PRAGMA WRITE) and
// the close. Same family as the chat dry-run defect (01a188f7).
//
// SO THE ASSERTION IS ABOUT BYTES. A counter, a row count or "no UPDATE ran" is
// green on the broken code; only the ledger's own bytes can tell a read from a
// write. Every handle below comes from the production entry point (openSearchDB)
// — the test never opens the ledger itself, or it would measure its own fixture
// instead of the code.
//
// SKIPS, NEVER PASSES, where the host offers no block-backed scratch directory: a
// ledger on a memory filesystem carries no -wal at all, so the subject would not
// exist and a green would mean nothing.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// ledgerFileDigest is a file's identity as bytes: SIZE plus sha256. Size is
// included because sha256 alone cannot tell a truncated file from a rewritten
// one, and the measured defect changed both. A MISSING file digests to
// ("absent", false) — absence is a change too, and the one the defect produced.
func ledgerFileDigest(t *testing.T, path string) (string, bool) {
	t.Helper()
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "absent", false
		}
		t.Fatalf("stat %s: %v", path, statErr)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(blob)
	return fmt.Sprintf("%d bytes %s", info.Size(), hex.EncodeToString(sum[:])), true
}

// ledgerWalSuffixes is the file SET a WAL ledger owns. -shm is deliberately
// excluded from the assertions: it is the shared wal-index a READER is entitled
// to touch, and it carries no ledger data (see openLedgerIndexDBReadOnly's
// measurement). The rule is "db and db-wal unchanged"; -shm moving is not a write
// of anybody's record.
var ledgerWalSuffixes = []string{"", walSidecarWALSuffix}

// memSearchROTopic / memSearchROQuestion are the row the fixture stores: the byte
// test asserts the read actually FOUND something, so the seed must be searchable
// and is named once (Rule 1b/13).
const (
	memSearchROTopic    = "readonly open topic"
	memSearchROQuestion = "does a read-only search open rewrite the ledger?"
)

// memSearchROFixture builds a DEVICE-BACKED memory ledger with the real schema
// (ensureKnowledgeBase's writable arm — the fixture is allowed to write, it is
// not the subject), stores one row, and copies the whole file SET (db + sidecars)
// while the writer handle is still open: a closed handle checkpoints its WAL away
// and the copy would carry none. Returns the COPY's path — the artifact the test
// measures — with config's ledger pin pointed at it.
//
// It skips (never passes) where no block device is available.
func memSearchROFixture(t *testing.T) string {
	t.Helper()
	src := testutil.TestDeviceScratchDir(t, "msearch-ro-src")
	if memBacked, description := testutil.ScratchRootFilesystem(src); memBacked {
		t.Skipf("this fixture's ledger must be on a block device — a memory filesystem's ledger carries "+
			"no -wal to rewrite, so the subject does not exist here: %s", description)
	}
	live := filepath.Join(src, walSidecarLedgerFile)
	t.Setenv(config.MemoryDBEnv, live)
	t.Setenv(config.MemoryDBEnvLegacy, "")

	ctx := context.Background()
	db, err := ensureKnowledgeBase(ctx, live, false)
	if err != nil {
		t.Fatalf("build the device-backed ledger: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO distilled_qa (topic, question, answer) VALUES (?,?,?)`,
		memSearchROTopic, memSearchROQuestion,
		"no — a reader opens mode=ro, so the file's bytes are its own"); err != nil {
		db.Close() //stylegate:ignore the fixture's own writer handle; without the row there is nothing to read
		t.Fatalf("seed the fixture's ledger: %v", err)
	}

	dst := testutil.TestDeviceScratchDir(t, "msearch-ro-dst")
	for _, suffix := range ledgerWalSidecarSet {
		blob, readErr := os.ReadFile(live + suffix)
		if readErr != nil {
			continue // a missing sidecar is not a failure — the set is copied as found
		}
		if writeErr := os.WriteFile(filepath.Join(dst, walSidecarLedgerFile+suffix), blob, 0o644); writeErr != nil {
			db.Close() //stylegate:ignore the fixture's own writer handle, released by the explicit Close below
			t.Fatalf("copy %s%s: %v", walSidecarLedgerFile, suffix, writeErr)
		}
	}
	db.Close() //stylegate:ignore the fixture's writer, closed so its WAL is fully written before the copy is read

	copied := filepath.Join(dst, walSidecarLedgerFile)
	if _, present := ledgerFileDigest(t, copied+walSidecarWALSuffix); !present {
		t.Skipf("the copied ledger carries no %s — a read has no WAL to rewrite, so this host cannot "+
			"answer the question (Rule 15: report, never assume)", walSidecarWALSuffix)
	}
	t.Setenv(config.MemoryDBEnv, copied)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	return copied
}

// ledgerWalSidecarSet is the file set memSearchROFixture copies: the ledger plus
// every sidecar suffix the driver may leave beside it.
var ledgerWalSidecarSet = []string{"", walSidecarWALSuffix, walSidecarSHMSuffix}

// TestMemorySearchOpenLeavesTheLedgerBytesUntouched is the regression guard: two
// read-only opens through the PRODUCTION entry point (openSearchDB, the one
// memory_search / memory get / the provenance faces use) must leave memory.db AND
// memory.db-wal byte-identical, and must still return the row.
//
// RED PROOF (run it, do not trust the green): put the old shape back —
//
//	sed -i 's/^func openKnowledgeBaseDB(path string, readOnly bool) (\*sql.DB, error) {$/func openKnowledgeBaseDB(path string, readOnly bool) (*sql.DB, error) { return openDB(path, readOnly) }\nfunc openKnowledgeBaseDBUnused(path string, readOnly bool) (*sql.DB, error) {/' internal/tools/memory.go
//
// or, equivalently, point the readOnly arm at openDB(path, true) — and this test
// fails on the FIRST open with `memory.db-wal: "…" → "absent"` (checkpointed away)
// while the row count is still correct, i.e. every non-byte judgement stays green
// on the broken code. Restore and verify with
// `git diff HEAD -- internal/tools/memory.go` before committing.
func TestMemorySearchOpenLeavesTheLedgerBytesUntouched(t *testing.T) {
	ledger := memSearchROFixture(t)
	ctx := context.Background()

	before := make(map[string]string, len(ledgerWalSuffixes))
	for _, suffix := range ledgerWalSuffixes {
		digest, present := ledgerFileDigest(t, ledger+suffix)
		if !present {
			t.Fatalf("the fixture's copy is missing %s%s — nothing to measure", walSidecarLedgerFile, suffix)
		}
		before[suffix] = digest
	}

	for run := 1; run <= 2; run++ {
		db, err := openSearchDB(ctx, ledger)
		if err != nil {
			t.Fatalf("read-only open %d: %v", run, err)
		}
		var rows int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM distilled_qa`).Scan(&rows); err != nil {
			db.Close() //stylegate:ignore test-owned handle; the assertion below is about the FILE
			t.Fatalf("read query %d: %v", run, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close the read-only handle %d: %v", run, err)
		}
		if rows == 0 {
			t.Fatalf("open %d read zero rows — the handle was not reading the seeded ledger, so this test "+
				"would pass without exercising a read at all", run)
		}
		for _, suffix := range ledgerWalSuffixes {
			after, present := ledgerFileDigest(t, ledger+suffix)
			if !present {
				t.Fatalf("open %d DELETED %s%s — a read-only entry point removed a file of a ledger it "+
					"promised not to write (before: %s)", run, walSidecarLedgerFile, suffix, before[suffix])
			}
			if after != before[suffix] {
				t.Errorf("open %d REWROTE %s%s: %s → %s — the write is the OPEN, not a query",
					run, walSidecarLedgerFile, suffix, before[suffix], after)
			}
		}
	}

	// The entry point must still FIND the stored row: a "read-only" open that
	// silently returned an empty schema would satisfy the byte assertions
	// trivially.
	hits, err := MemorySearchHits(memSearchROQuestion, memoryDefaultLimit, "")
	if err != nil {
		t.Fatalf("search after the read-only opens: %v", err)
	}
	found := false
	for _, h := range hits {
		if strings.Contains(h.Topic, memSearchROTopic) {
			found = true
		}
	}
	if !found {
		t.Errorf("the read-only entry point could not find the seeded row %q — the byte assertions above "+
			"would then be satisfied by a database nothing can read\nhits: %+v", memSearchROTopic, hits)
	}
}

// TestOpenMemoryLedgerReadOnlyLeavesTheLedgerBytesUntouched is the second face of
// the same defect: OpenMemoryLedger is the opener behind the maintenance verbs'
// `--dry-run` (`gopi memory converge-keywords --dry-run`,
// `gopi memory backfill-tokens --dry-run`), and it used openDB(path, readOnly)
// too. Measured before the fix, on the operator's ledger copy: one
// `converge-keywords --dry-run` REWROTE memory.db (ce6c19bc… → 727ab977…) and
// DELETED memory.db-wal — a verb whose own help text says "write nothing".
//
// The read it performs is the dry-run's own scan (KeywordsTailConverge with
// dry=true), so this arm exercises the production call and not a stand-in query.
func TestOpenMemoryLedgerReadOnlyLeavesTheLedgerBytesUntouched(t *testing.T) {
	ledger := memSearchROFixture(t)
	ctx := context.Background()

	before := make(map[string]string, len(ledgerWalSuffixes))
	for _, suffix := range ledgerWalSuffixes {
		digest, present := ledgerFileDigest(t, ledger+suffix)
		if !present {
			t.Fatalf("the fixture's copy is missing %s%s — nothing to measure", walSidecarLedgerFile, suffix)
		}
		before[suffix] = digest
	}

	for run := 1; run <= 2; run++ {
		db, err := OpenMemoryLedger(ledger, true)
		if err != nil {
			t.Fatalf("read-only maintenance open %d: %v", run, err)
		}
		rep, err := KeywordsTailConverge(ctx, db, true)
		if err != nil {
			db.Close() //stylegate:ignore test-owned handle; the assertion below is about the FILE
			t.Fatalf("dry-run scan %d: %v", run, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close the maintenance handle %d: %v", run, err)
		}
		if rep.Scanned == 0 {
			t.Fatalf("dry run %d scanned zero rows — it did not read the seeded ledger, so this test would "+
				"pass without exercising a scan: %+v", run, rep)
		}
		for _, suffix := range ledgerWalSuffixes {
			after, present := ledgerFileDigest(t, ledger+suffix)
			if !present {
				t.Fatalf("dry run %d DELETED %s%s — the verb promised to write nothing (before: %s)",
					run, walSidecarLedgerFile, suffix, before[suffix])
			}
			if after != before[suffix] {
				t.Errorf("dry run %d REWROTE %s%s: %s → %s — a dry run must not touch the ledger",
					run, walSidecarLedgerFile, suffix, before[suffix], after)
			}
		}
	}
}

// TestKnowledgeBaseReadOnlyOpenRefusesToWrite pins the SHAPE the fix depends on,
// independently of who calls it: the read-only arm must hand back a handle that
// REFUSES a write, and must not CREATE a ledger that is not there.
//
// It is the half a byte comparison cannot state. Bytes prove the run did not
// write; this proves it COULD NOT have — so a later caller that adds an unguarded
// statement to a read face fails here instead of silently reintroducing the
// defect (Rule 15: the refusal is reported, never downgraded to a writable handle).
func TestKnowledgeBaseReadOnlyOpenRefusesToWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("refuses a write", func(t *testing.T) {
		ledger := memSearchROFixture(t)
		db, err := openKnowledgeBaseDB(ledger, true)
		if err != nil {
			t.Fatalf("read-only open: %v", err)
		}
		defer db.Close() //stylegate:ignore test-owned handle; released when this subtest returns
		if _, err := db.ExecContext(ctx,
			`UPDATE distilled_qa SET topic = 'written through a read-only handle'`); err == nil {
			t.Fatalf("the read-only handle ACCEPTED a write — the promise is a DSN property here, not a pragma")
		}
	})

	t.Run("creates nothing", func(t *testing.T) {
		missing := filepath.Join(testutil.TestDeviceScratchDir(t, "msearch-ro-absent"), "absent.db")
		if _, err := openKnowledgeBaseDB(missing, true); err == nil {
			t.Fatalf("a read-only open of a nonexistent ledger SUCCEEDED — it must report, never create " +
				"(a silent fallback to the writable shape is the defect this fix removes)")
		}
		if _, statErr := os.Stat(missing); statErr == nil {
			t.Fatalf("the read-only open CREATED %s on the way to failing", missing)
		}
	})

	t.Run("the writable arm still opens", func(t *testing.T) {
		// The control: openKnowledgeBaseDB(path, false) must remain the ordinary
		// writable opener, or the "read-only" arm proves nothing by contrast and
		// every store path silently became read-only.
		path := testutil.NewTestDBPath(t)
		db, err := openKnowledgeBaseDB(path, false)
		if err != nil {
			t.Fatalf("writable open: %v", err)
		}
		defer db.Close() //stylegate:ignore test-owned handle; released when this subtest returns
		if _, err := db.ExecContext(ctx, `CREATE TABLE msearch_ro_control (n INTEGER)`); err != nil {
			t.Fatalf("the writable arm refused a write: %v", err)
		}
	})
}
