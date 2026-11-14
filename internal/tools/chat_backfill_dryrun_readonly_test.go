package tools

// chat_backfill_dryrun_readonly_test.go — the regression guard for a DRY RUN
// that writes.
//
// THE DEFECT (measured 2026-10-30, task 01a188f7). `gopi chat backfill
// --dry-run` guards every UPDATE on its flag, so "repaired=0" reads green — but
// the WAL moved on every run. A COPY of the operator's own ledger, before/after
// ONE plain dry run:
//
//	before  memory.db 38056aa0ea3dfab4…  memory.db-wal 714570717d77e362…  (8 969 272 B)
//	after   memory.db 0a1135c8b7fcf2d7…  memory.db-wal                          (GONE)
//
// The write was never an SQL statement: it was the OPEN (a writable connection
// plus ensureChatSchema's DDL, which commits a write transaction per statement
// and checkpoints the WAL away at close).
//
// SO THE ASSERTION IS ABOUT BYTES, NOT ABOUT A COUNTER. "Nothing was repaired"
// is green on the broken code (nothing was ever repaired — the UPDATEs were
// already guarded); only the ledger's own bytes can tell a read from a write.
//
// SKIPS, NEVER PASSES, where the host offers no block-backed scratch directory:
// a tmpfs ledger never carries a -wal at all (measured in
// ledger_readonly_walsidecar_test.go), so the subject would not exist.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatDryRunLegacySession is the [auto]-shaped legacy row the fixture seeds: it
// makes the run have WORK to report (a dry run over zero candidates would pass
// this test without ever exercising the scan).
const chatDryRunLegacySession = "dryrun-ro-session"

// chatDryRunFixture builds a DEVICE-BACKED ledger carrying one legacy [auto]
// row, copies the whole file SET (db + sidecars) while the writer is still open
// — a closed handle checkpoints its WAL away and the copy would carry none —
// and returns the COPY's path. It skips where no block device exists.
func chatDryRunFixture(t *testing.T) string {
	t.Helper()
	return chatDryRunFixtureSeeded(t, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO `+chatBackfillTasksTableName+
			` (task_id, session_id, summary, created_at, updated_at) VALUES (?,?,?,?,?)`,
			"t-dryrun-legacy", chatDryRunLegacySession, chatLegacyAutoSummaryPrefix+"a legacy harness row",
			1788000000, 1788000000)
		return err
	})
}

// chatDryRunFixtureSeeded is chatDryRunFixture with the caller's own seed
// landing in the SOURCE ledger before the copy is taken — the repair's fixture
// needs a chats row, the backfill's needs a legacy tasks row, and neither may be
// created after the copy (a writable open on the copy would checkpoint the WAL
// that is the thing being measured).
func chatDryRunFixtureSeeded(t *testing.T, seed func(ctx context.Context, db *sql.DB) error) string {
	t.Helper()
	src := testutil.TestDeviceScratchDir(t, "chat-dryrun-src")
	if memBacked, description := testutil.ScratchRootFilesystem(src); memBacked {
		t.Skipf("this fixture's ledger must be on a block device — a memory filesystem's ledger carries "+
			"no -wal to rewrite, so the subject does not exist here: %s", description)
	}
	live := filepath.Join(src, walSidecarLedgerFile)
	t.Setenv(config.MemoryDBEnv, live)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	t.Setenv(chatDBEnv, "")

	ctx := context.Background()
	db, err := openTasksDB(ctx, live)
	if err != nil {
		t.Fatalf("build the device-backed ledger: %v", err)
	}
	if err := enableWAL(ctx, db); err != nil {
		db.Close() //stylegate:ignore the fixture's own writer handle; without WAL the fixture proves nothing
		t.Fatalf("put the fixture's ledger in WAL: %v", err)
	}
	if err := ensureChatSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore the fixture's own writer handle; without the chat face there is no scan
		t.Fatalf("land the chat face: %v", err)
	}
	if err := seed(ctx, db); err != nil {
		db.Close() //stylegate:ignore the fixture's own writer handle; without the seed there is nothing to scan
		t.Fatalf("seed the fixture's ledger: %v", err)
	}

	dst := testutil.TestDeviceScratchDir(t, "chat-dryrun-dst")
	for _, suffix := range []string{"", walSidecarWALSuffix, walSidecarSHMSuffix} {
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
		t.Skipf("the copied ledger carries no %s — a dry run has no WAL to rewrite, so this host cannot "+
			"answer the question (Rule 15: report, never assume)", walSidecarWALSuffix)
	}
	return copied
}

// TestChatBackfillDryRunLeavesTheLedgerBytesUntouched is the regression guard:
// two dry runs over a real WAL ledger must leave memory.db AND memory.db-wal
// byte-identical, and must still REPORT the work they found.
//
// RED PROOF (do this, do not trust the green): point the dry-run arm back at the
// writable opener —
//
//	sed -i 's/return openChatDBReadOnly(ctx)/return openChatDB(ctx)/' \
//	    internal/tools/chat.go    # inside chatIndexOpenDB's readOnly branch
//
// — and this test fails on the FIRST dry run with
// `memory.db-wal: "71457071…" → "absent"` (the WAL is checkpointed away) while
// `stats` still reports `migrated=1 … dryrun=true`, i.e. the counter is green
// on the broken code. Restore the line (verify with
// `git diff HEAD -- internal/tools/chat.go`) before committing.
func TestChatBackfillDryRunLeavesTheLedgerBytesUntouched(t *testing.T) {
	ledger := chatDryRunFixture(t)
	t.Setenv(config.MemoryDBEnv, ledger)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	t.Setenv(chatDBEnv, ledger)
	t.Setenv("GOPI_HOME", t.TempDir()) // the session logs this run re-derives from

	dbBefore, hadDB := ledgerFileDigest(t, ledger)
	walBefore, hadWAL := ledgerFileDigest(t, ledger+walSidecarWALSuffix)
	if !hadDB || !hadWAL {
		t.Fatalf("the fixture's copy is missing its ledger set (db=%v wal=%v) — nothing to measure", hadDB, hadWAL)
	}

	for run := 1; run <= 2; run++ {
		stats, report, err := ChatBackfill(ChatBackfillOptions{DryRun: true})
		if err != nil {
			t.Fatalf("dry run %d: %v", run, err)
		}
		if !strings.Contains(stats, "dryrun=true") {
			t.Fatalf("dry run %d did not run in dry mode: stats=%q report=%q", run, stats, report)
		}
		if !strings.Contains(stats, "total=1") {
			t.Fatalf("dry run %d scanned no candidates, so this test would pass without exercising the "+
				"scan at all: stats=%q report=%q", run, stats, report)
		}
		dbAfter, hasDB := ledgerFileDigest(t, ledger)
		walAfter, hasWAL := ledgerFileDigest(t, ledger+walSidecarWALSuffix)
		if !hasDB {
			t.Fatalf("dry run %d DELETED %s%s — a run that promised to report removed the ledger", run,
				walSidecarLedgerFile, "")
		}
		if !hasWAL {
			t.Fatalf("dry run %d DELETED %s%s — a run that promised to report wrote the ledger's WAL "+
				"away (before %s)", run, walSidecarLedgerFile, walSidecarWALSuffix, walBefore)
		}
		if dbAfter != dbBefore {
			t.Errorf("dry run %d rewrote the ledger itself: %s %s → %s", run, walSidecarLedgerFile, dbBefore, dbAfter)
		}
		if walAfter != walBefore {
			t.Errorf("dry run %d rewrote the WAL: %s%s %s → %s", run, walSidecarLedgerFile,
				walSidecarWALSuffix, walBefore, walAfter)
		}
	}
}

// TestChatIndexOpenDBReadOnlyRefusesToWrite pins the SHAPE the fix depends on,
// independently of who calls it: the read-only arm must hand back a handle that
// REFUSES a write and must not CREATE a ledger that is not there.
//
// It is the half a byte comparison cannot state: bytes prove the run did not
// write, this proves it COULD NOT have — so a later caller that adds an
// unguarded statement to a dry-run path fails here instead of silently
// reintroducing the defect.
func TestChatIndexOpenDBReadOnlyRefusesToWrite(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	ctx := context.Background()

	t.Run("refuses a write", func(t *testing.T) {
		ledger := chatDryRunFixture(t)
		t.Setenv(config.MemoryDBEnv, ledger)
		t.Setenv(config.MemoryDBEnvLegacy, "")
		t.Setenv(chatDBEnv, ledger)
		db, err := chatIndexOpenDB(ctx, true)
		if err != nil {
			t.Fatalf("read-only open: %v", err)
		}
		defer db.Close() //stylegate:ignore test-owned handle; released when this subtest returns
		if _, err := db.ExecContext(ctx, `UPDATE chats SET first_msg = 'written by a dry run'`); err == nil {
			t.Fatalf("the read-only handle ACCEPTED a write — the dry-run promise is a pragma here, not a property")
		}
	})

	t.Run("creates nothing", func(t *testing.T) {
		missing := filepath.Join(testutil.TestDeviceScratchDir(t, "chat-dryrun-absent"), "absent.db")
		t.Setenv(config.MemoryDBEnv, missing)
		t.Setenv(config.MemoryDBEnvLegacy, "")
		t.Setenv(chatDBEnv, missing)
		db, err := chatIndexOpenDB(ctx, true)
		if err == nil {
			db.Close() //stylegate:ignore test-owned handle; the assertion below is about the FILE
			t.Fatalf("a read-only open of a nonexistent ledger SUCCEEDED — it must report, never create " +
				"(a silent fallback to the writable shape is the defect this fix removes)")
		}
		if _, statErr := os.Stat(missing); statErr == nil {
			t.Fatalf("the read-only open CREATED %s on the way to failing", missing)
		}
	})
}

// chatDryRunRepairSession is the chats row the repair's fixture carries: a stored
// head the harness wrote, plus a session log whose operator line is different —
// so the scan has a row it WOULD rewrite. Without that, a dry run that wrote
// nothing would prove nothing (the redirect's subject would be absent).
const chatDryRunRepairSession = "dryrun-ro-repair-session"

// TestChatFirstMsgRepairDryRunLeavesTheLedgerBytesUntouched is the second arm of
// the same defect, and the one the first arm could not cover: `gopi chat backfill
// --dry-run --repair-first-msg` promised not to write, and repaired=0 was green
// while every run still rewrote the ledger — the WRITE WAS THE OPEN, not an
// UPDATE.
//
// RED PROOF (do this, do not trust the green): point the repair back at the
// writable opener —
//
//	sed -i 's/chatIndexOpenDB(ctx, dryRun)/openChatDB(ctx)/' \
//	    internal/tools/chat_first_msg_repair.go
//
// — and this test fails on the FIRST run with `memory.db-wal: "…" → "absent"`
// (the WAL is checkpointed away) while the scan still reports the row as one it
// would have repaired, i.e. the report is green on the broken code. Restore the
// line and confirm with `git diff HEAD -- internal/tools/chat_first_msg_repair.go`
// before committing.
func TestChatFirstMsgRepairDryRunLeavesTheLedgerBytesUntouched(t *testing.T) {
	// The repair reads its names from the session tree, so the home comes first:
	// a log written after the fixture's copy would be invisible to the scan and
	// the run would report "no log" for every row (green, and meaningless).
	t.Setenv("GOPI_HOME", t.TempDir())
	writeChatSessionJSONLBare(t, chatDryRunRepairSession, []struct{ text, source string }{
		{chatDryRunRepairOperatorLine, inputsource.Human},
	})

	ledger := chatDryRunFixtureSeeded(t, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO chats (session_id, mode, first_msg, started_at) VALUES (?,?,?,?)`,
			chatDryRunRepairSession, ChatModeCLI, chatNoticeBatchFirstMsg, 1788000000)
		return err
	})
	t.Setenv(config.MemoryDBEnv, ledger)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	t.Setenv(chatDBEnv, ledger)

	dbBefore, hadDB := ledgerFileDigest(t, ledger)
	walBefore, hadWAL := ledgerFileDigest(t, ledger+walSidecarWALSuffix)
	if !hadDB || !hadWAL {
		t.Fatalf("the fixture's copy is missing its ledger set (db=%v wal=%v) — nothing to measure", hadDB, hadWAL)
	}

	for run := 1; run <= 2; run++ {
		repairs, err := ChatRepairFirstMsgs(true)
		if err != nil {
			t.Fatalf("dry run %d: %v", run, err)
		}
		var scanned, wouldRepair int
		for _, r := range repairs {
			if r.SessionID != chatDryRunRepairSession {
				continue
			}
			scanned++
			if r.After != "" {
				wouldRepair++
			}
		}
		if scanned != 1 {
			t.Fatalf("dry run %d did not scan the seeded row, so this test would pass without exercising "+
				"the scan at all: %+v", run, repairs)
		}
		if wouldRepair != 1 {
			t.Fatalf("dry run %d reports nothing to repair, so the run had no write to decline: %+v", run, repairs)
		}
		dbAfter, hasDB := ledgerFileDigest(t, ledger)
		walAfter, hasWAL := ledgerFileDigest(t, ledger+walSidecarWALSuffix)
		if !hasDB {
			t.Fatalf("dry run %d DELETED the ledger — a run that promised to report removed it", run)
		}
		if !hasWAL {
			t.Fatalf("dry run %d DELETED the WAL — a run that promised to report wrote it away (before %s)",
				run, walBefore)
		}
		if dbAfter != dbBefore {
			t.Errorf("dry run %d rewrote the ledger itself: memory.db %s → %s", run, dbBefore, dbAfter)
		}
		if walAfter != walBefore {
			t.Errorf("dry run %d rewrote the WAL: memory.db-wal %s → %s", run, walBefore, walAfter)
		}
	}
}

// chatDryRunRepairOperatorLine is the operator's own first line in the repair
// fixture's session log — the value the scan re-derives and a dry run must NOT
// land.
const chatDryRunRepairOperatorLine = "what does the sync-peers roster actually gate?"
