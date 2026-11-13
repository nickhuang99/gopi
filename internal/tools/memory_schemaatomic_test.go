package tools

import (
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
)

// TestMemorySchemaPublishedAtomically is the regression guard for the first-open
// race (the t-mem-flake): the schema — base table + both FTS5 indexes + the six
// keep-in-sync triggers — must be published in ONE transaction, so no
// connection can ever observe distilled_qa without its index. The probe hook
// runs INSIDE that transaction, so it observes the DB exactly while the schema
// is in flight: the window in which a concurrent store used to commit a row
// the index could never see (ftsTriggersOwned then saw the installer's
// triggers and skipped syncFTSRow — the row stayed unindexed and the search
// kept returning its empty hint).
//
// On the pre-fix code (autocommit DDL, seam added) this test reports
// "distilled_qa visible without its index/triggers" and fails.
func TestMemorySchemaPublishedAtomically(t *testing.T) {
	path := setMemoryDB(t)
	var hookRuns, incomplete atomic.Int64
	var probeNote atomic.Value
	schemaBatchProbeHook = func() {
		hookRuns.Add(1)
		pdb, err := sql.Open("sqlite", LedgerDSN(path))
		if err != nil {
			probeNote.Store("open: " + err.Error())
			return
		}
		defer pdb.Close() //stylegate:ignore probe handle closed immediately after its single read
		ctx := t.Context()
		ok, err := tableExists(ctx, pdb, "distilled_qa")
		if err != nil {
			if !isLockContentionErr(err) {
				probeNote.Store("probe: " + err.Error())
			}
			return
		}
		if !ok {
			return // no schema published yet — the only correct observation
		}
		fts, ferr := tableExists(ctx, pdb, ftsSyncFTSName)
		tri, terr := tableExists(ctx, pdb, memoryTriIndexTable)
		if ferr != nil || terr != nil || !fts || !tri || !ftsTriggersOwned(ctx, pdb) {
			incomplete.Add(1)
		}
	}
	t.Cleanup(func() { schemaBatchProbeHook = nil })

	if _, err := MemoryStore("atomic topic", "is the schema atomic?",
		"the first-open schema must be published in one transaction", "src-atomic", nil); err != nil {
		t.Fatalf("store on a fresh DB must succeed: %v", err)
	}
	if note, _ := probeNote.Load().(string); note != "" {
		t.Fatalf("in-flight schema probe failed: %s", note)
	}
	if hookRuns.Load() == 0 {
		t.Fatal("probe hook never ran — the test seam did not fire")
	}
	if n := incomplete.Load(); n != 0 {
		t.Fatalf("distilled_qa was visible WITHOUT its index/triggers %d time(s) — schema publication is not atomic", n)
	}

	// The stored row must be indexed AND searchable: the user-visible symptom
	// of the bug was a store that reported success while the search could
	// never find the row.
	db := openRawDB(t, path)
	if err := verifyRowIndexed(t.Context(), db, 1); err != nil {
		t.Fatalf("just-stored row is not indexed: %v", err)
	}
	out, err := MemorySearch("is the schema atomic", 5, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "atomic topic") {
		t.Fatalf("just-stored row is not searchable: %s", out)
	}
}

// TestMemoryStorePartialSchemaFailsLoudly: a DB that has the distilled_qa base
// table but NO FTS index (exactly the state a non-atomic first-open could
// publish, and the state a torn init leaves behind) must never report a
// successful store — such a row is invisible to MemorySearch forever
// (Rule 15: no silent failure).
func TestMemoryStorePartialSchemaFailsLoudly(t *testing.T) {
	path := setMemoryDB(t)
	db := openRawDB(t, path)
	seed(t, db,
		`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT NOT NULL, question TEXT NOT NULL, answer TEXT NOT NULL, sources TEXT DEFAULT '', confidence REAL DEFAULT 0.5, tier TEXT DEFAULT 'warm')`,
	)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	_, err := MemoryStore("partial topic", "does a partial schema store?",
		"a store into a base-table-only DB must fail loudly, never silently", "src-partial", nil)
	if err == nil {
		t.Fatal("store into a base-table-only DB reported success — the row would be unsearchable forever")
	}
	if !strings.Contains(err.Error(), ftsSyncFTSName) {
		t.Fatalf("store failure must name the unusable index %s: %v", ftsSyncFTSName, err)
	}
}
