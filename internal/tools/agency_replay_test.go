package tools

import (
	"context"
	"database/sql"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agencyscan"
)

// Historical-replay harness (epic agency-proposals, deliverable 3).
//
// It runs the REAL miner, through the REAL submit seam (TaskSubmit), over a
// byte copy of the operator's live ledger — twice. The assertions are the
// epic's acceptance criteria in executable form:
//
//	first pass  → a non-empty, explicit proposal list (the "what would it have
//	              proposed?" evidence the devdoc's quality verdict is based on)
//	second pass → ZERO proposals, and the first pass's proposals are COUNTED as
//	              deduped (the idempotence proof)
//
// Nothing is written to the operator's own ledger: GOPI_MEMORY_DB is pointed at
// the copy (Rule 17 — t.Setenv, never os.Setenv, so the flip is undone by the
// testing framework even on failure).
//
// The test SKIPS when no live ledger exists (CI / a fresh machine): it is an
// evidence harness, not a unit test — the idempotence LOGIC is pinned by
// TestScan_IdempotentSecondPassHasZeroProposals in internal/agencyscan, which
// needs no live data.

// liveLedgerPath returns the operator's ledger path, or "" when absent.
func liveLedgerPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".gopi", "memory.db")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// copyLedgerSnapshot copies the live ledger (main file only — the snapshot is
// read + written by this test alone, so WAL sidecars from the live writer are
// deliberately not carried over: a stale -wal would replay the operator's
// uncommitted frames into the copy).
func copyLedgerSnapshot(t *testing.T, src, dstDir string) string {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("cannot read live ledger: %v", err)
	}
	dst := filepath.Join(dstDir, "memory.db")
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		t.Fatalf("write ledger snapshot: %v", err)
	}
	return dst
}

// TestAgencyReplay_LiveLedgerIdempotent is the epic's historical replay.
func TestAgencyReplay_LiveLedgerIdempotent(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	src := liveLedgerPath(t)
	if src == "" {
		t.Skip("no live ledger (~/.gopi/memory.db) — replay harness skipped")
	}
	if testing.Short() {
		t.Skip("replay harness skipped under -short (it copies a multi-MB ledger)")
	}
	snap := copyLedgerSnapshot(t, src, t.TempDir())
	t.Setenv(memoryDBEnv, snap)

	first, err := agencyscan.Scan(context.Background(), agencyscan.Options{Submit: true})
	if err != nil {
		t.Fatalf("first replay pass: %v", err)
	}
	if first.Proposed == 0 {
		t.Fatalf("replay proposed nothing over the live ledger (examined=%d candidates=%d) — "+
			"either the ledger no longer carries any threshold-meeting pattern (report it, do not weaken the gate) "+
			"or the dedupe probe is matching too much", first.Scanned, len(first.Candidates))
	}
	t.Logf("REPLAY PASS 1: examined=%d candidates=%d proposed=%d deduped=%d dropped=%d",
		first.Scanned, len(first.Candidates), first.Proposed, first.Deduped, first.Dropped)
	for _, c := range first.Candidates {
		t.Logf("  %-20s n=%-3d failed=%-3d key=%s", c.Kind, c.Count, c.FailedCount, c.RecurrenceKey)
	}

	second, err := agencyscan.Scan(context.Background(), agencyscan.Options{Submit: true})
	if err != nil {
		t.Fatalf("second replay pass: %v", err)
	}
	t.Logf("REPLAY PASS 2: candidates=%d proposed=%d deduped=%d dropped=%d",
		len(second.Candidates), second.Proposed, second.Deduped, second.Dropped)
	if second.Proposed != 0 {
		t.Fatalf("IDEMPOTENCE VIOLATED over the live ledger: the second pass proposed %d row(s)",
			second.Proposed)
	}
	if second.Deduped != first.Proposed {
		t.Fatalf("second pass must dedupe exactly what the first proposed: deduped=%d proposed(first)=%d",
			second.Deduped, first.Proposed)
	}
	if len(second.Candidates) != len(first.Candidates) {
		t.Fatalf("the miner is not a pure function of the ledger: pass1 candidates=%d pass2=%d",
			len(first.Candidates), len(second.Candidates))
	}

	// The proposals must be OBSERVABLE and QUARANTINED: an operator can read
	// them, and no automatic claimer may take them (the auto-dispatch
	// question, answered in the ledger rather than in prose).
	assertProposalsQuarantined(t, snap)
}

// assertProposalsQuarantined checks every proposal row the replay wrote:
// tagged improvement+agency-proposal+<kind>, carrying the quiet fence the
// claimers skip, and still pending (nothing claimed them during the test).
func assertProposalsQuarantined(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer db.Close() //stylegate:ignore test fixture handle

	rows, err := db.Query(
		`SELECT summary, tags, status, payload FROM tasks WHERE tags LIKE ?`,
		"%"+agencyscan.TagAgencyProposal+"%")
	if err != nil {
		t.Fatalf("query proposals: %v", err)
	}
	defer rows.Close() //stylegate:ignore test fixture handle
	n := 0
	for rows.Next() {
		var summary, tags, status, payload string
		if err := rows.Scan(&summary, &tags, &status, &payload); err != nil {
			t.Fatalf("scan proposal: %v", err)
		}
		n++
		if !strings.Contains(tags, QuarantineTagSet) {
			t.Errorf("proposal %q lacks the %q quarantine tag (tags=%q) — an auto-claimer could take it",
				summary, QuarantineTagSet, tags)
		}
		if !strings.Contains(tags, agencyscan.TagImprovement) {
			t.Errorf("proposal %q lacks the %q family tag (tags=%q)", summary, agencyscan.TagImprovement, tags)
		}
		if status != taskStatusPending {
			t.Errorf("proposal %q is %q, want pending (a proposal waits for a human)", summary, status)
		}
		if !strings.Contains(payload, agencyscan.RecurrenceKeyFingerprintMarker) {
			t.Errorf("proposal %q carries no dedupe fence in its payload — the next pass would re-propose it", summary)
		}
	}
	if n == 0 {
		t.Error("no proposal row found after a submitting pass")
	}
}

// TestProposalRowsAreNeverClaimable is the auto-dispatch guard, asserted
// through the queue's OWN selection predicate (TaskNextFiltered) rather than by
// reading tags: a proposal row must be invisible to every automatic claimer.
//
// This is the answer to the epic's explicit question — "will this become an
// auto-dispatch machine?" — expressed in the ledger instead of in prose. The
// fence is QuarantineTagSet, the SAME constant the worker's exclusion list is
// built from (Rule 14), so the two can never drift apart.
func TestProposalRowsAreNeverClaimable(t *testing.T) {
	claimTestDB(t)
	// Seed the PROPOSAL first: claim order is priority/created_at/id ASC, so
	// the proposal row is the OLDER one. If the quarantine fence were absent
	// it would be offered FIRST — that is what makes this test red-provable
	// (a control seeded first would mask the leak by simply being older).
	proposal := claimSeedTask(t, "[auto] duplicate knowledge: control probe")
	tagProposalRow(t, proposal)

	// A claimable control row proves the fixture can in fact be claimed —
	// without it this test could pass green on a queue that claims nothing.
	control := claimSeedTask(t, "agency-control-row")

	res, err := TaskNextFiltered("agency-test", "", QuarantineTagSet)
	if err != nil {
		t.Fatalf("claim scan: %v", err)
	}
	if res.Empty {
		t.Fatal("control row was not claimable — the fixture, not the fence, is what passed")
	}
	if res.Task.TaskID != control {
		t.Fatalf("queue offered %s; the quarantined proposal row %s (or another row) leaked into claim order — "+
			"a proposal must never be auto-claimed", res.Task.TaskID, proposal)
	}
}

// tagProposalRow rewrites a seeded row into the exact proposal shape the miner
// submits (tags + fence-bearing payload).
func tagProposalRow(t *testing.T, taskID string) {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test fixture handle
	tags := strings.Join([]string{agencyscan.TagImprovement, agencyscan.TagAgencyProposal, QuarantineTagSet}, ",")
	payload := agencyscan.RecurrenceKeyFingerprintMarker + "deadbeef"
	if _, err := db.Exec(`UPDATE tasks SET tags=?, payload=? WHERE task_id=?`, tags, payload, taskID); err != nil {
		t.Fatalf("tag proposal: %v", err)
	}
}
