package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
	sqlite "modernc.org/sqlite" // the driver's own error type: the chain must still carry it
)

// The tests below pin gopi's own READING of a driver-level SQLITE_CANTOPEN on a
// ledger open (chat_prefetch_ledger_open.go). The defect they exist for: the
// driver answers a read-only open of a path that does not exist with
//
//	unable to open database file: out of memory (14)
//
// — a sentence that reads as an allocation failure and names neither the ledger
// nor the trigger, while `(14)` is SQLITE_CANTOPEN and the cause is the PATH.

// cantOpenProbeLedgerPath names a ledger whose PARENT DIRECTORY does not exist,
// so a mode=ro open cannot succeed (the failure here is a STATE, not load —
// Rule 27). The missing directory is the one witness that the path is the
// subject of the sentence.
func cantOpenProbeLedgerPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-dir", config.MemoryDBFileName)
}

// TestPrefetchLedgerOpenCauseNamesTheLedgerAndThePathShapedReason is the pin: a
// CANTOPEN from the driver becomes a gopi sentence carrying the LEDGER PATH,
// the CODE and gopi's statement of the cause — and the driver's own text is
// still in the chain (Rule 15: the translation ADDS a diagnosis, it never
// hides what the driver said).
func TestPrefetchLedgerOpenCauseNamesTheLedgerAndThePathShapedReason(t *testing.T) {
	unopenable := cantOpenProbeLedgerPath(t)

	_, err := prefetchOpenHandle(context.Background(), unopenable)
	if err == nil {
		t.Fatalf("prefetchOpenHandle succeeded for a ledger whose parent directory does not exist (%s) "+
			"— the state this test needs was not produced", unopenable)
	}

	got := err.Error()
	if !strings.Contains(got, unopenable) {
		t.Errorf("the warm-opener failure does not name the ledger path %s — the driver's own sentence "+
			"names neither the file nor the probe; got %q", unopenable, got)
	}
	if !strings.Contains(got, prefetchLedgerCantOpenCause) {
		t.Errorf("the warm-opener failure does not state gopi's cause (%q) — a reader is left with the "+
			"driver's wording, which reads as an allocation failure; got %q", prefetchLedgerCantOpenCause, got)
	}
	if !strings.Contains(got, fmt.Sprintf("%d", prefetchLedgerCantOpenCode)) {
		t.Errorf("the warm-opener failure does not carry the SQLite code %d — the number is what lets a "+
			"reader look the result code up; got %q", prefetchLedgerCantOpenCode, got)
	}

	// The driver's error is still reachable through the chain: this is the
	// difference between translating and swallowing.
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		t.Fatalf("the driver's error is no longer in the chain of %q — the translation must wrap, not "+
			"replace (Rule 15)", got)
	}
	if coded.Code() != prefetchLedgerCantOpenCode {
		t.Errorf("the chain carries code %d, want %d", coded.Code(), prefetchLedgerCantOpenCode)
	}
	var driverErr *sqlite.Error
	if !errors.As(err, &driverErr) {
		t.Errorf("the driver's *sqlite.Error is not in the chain of %q — a caller that wants the driver's "+
			"own text must still find it", got)
	}
}

// TestPrefetchLedgerOpenCauseOnlyTouchesCantOpen is the refusal-to-overreach
// arm. The translation must be a classifier, not a blinder: a failure that is
// NOT SQLITE_CANTOPEN passes through BYTE-IDENTICAL, and a nil error stays nil
// (a caller's `err != nil` test must keep meaning what it meant).
func TestPrefetchLedgerOpenCauseOnlyTouchesCantOpen(t *testing.T) {
	unopenable := cantOpenProbeLedgerPath(t)

	if err := prefetchLedgerOpenCause(nil, unopenable); err != nil {
		t.Errorf("prefetchLedgerOpenCause(nil) = %v, want nil — a nil error must stay nil", err)
	}

	// A non-CANTOPEN error that DOES carry a driver code, produced for real: a
	// query against a table that does not exist on a seeded ledger is
	// SQLITE_ERROR (not CANTOPEN), so it is the honest witness that the
	// code-14 arm is narrow.
	seeded := seedPrefetchLedger(t)
	probe, err := sql.Open(prefetchDriverName, tools.LedgerDSN(seeded))
	if err != nil {
		t.Fatalf("probe open of %s: %v", seeded, err)
	}
	defer func() {
		if cerr := probe.Close(); cerr != nil {
			t.Logf("close the probe handle: %v", cerr)
		}
	}()
	var n int
	queryErr := probe.QueryRow("SELECT count(*) FROM no_such_table_at_all").Scan(&n)
	if queryErr == nil {
		t.Fatal("the probe expected a driver error for a table that does not exist")
	}
	var queryCoded interface{ Code() int }
	if !errors.As(queryErr, &queryCoded) {
		t.Fatalf("the probe's failure carries no driver code (%v) — it cannot stand as a non-CANTOPEN "+
			"coded witness", queryErr)
	}
	if queryCoded.Code() == prefetchLedgerCantOpenCode {
		t.Fatalf("the probe's failure carries code %d — the witness must be a DIFFERENT code",
			prefetchLedgerCantOpenCode)
	}
	if got := prefetchLedgerOpenCause(queryErr, seeded); got != queryErr {
		t.Errorf("a driver error of another code was rewritten: %v → %v — only SQLITE_CANTOPEN (%d) is "+
			"gopi's to translate", queryErr, got, prefetchLedgerCantOpenCode)
	}
}

// TestPrefetchWarmFallbackStderrCarriesGopisOwnCantOpenSentence drives the REAL
// executor (Rule 31): the stderr line the fallback already owes (Rule 15's
// disclosure, pinned by TestPrefetchWarmHandleFailureIsADisclosedFallbackNotASilentOne)
// now carries gopi's own sentence as well as the path — so an operator reading
// the line is told the open failed because the PATH is not there, instead of
// being sent after memory pressure.
func TestPrefetchWarmFallbackStderrCarriesGopisOwnCantOpenSentence(t *testing.T) {
	dir := t.TempDir()
	unopenable := filepath.Join(dir, "no-such-dir", config.MemoryDBFileName)
	t.Setenv("GOPI_MEMORY_DB", unopenable)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	stderrPath := filepath.Join(dir, "stderr.txt")
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create the stderr capture: %v", err)
	}
	defer func() {
		if cerr := stderrFile.Close(); cerr != nil {
			t.Logf("close the stderr capture: %v", cerr)
		}
	}()

	p := chatPrefetchFor(dir, func() string { return "read-only ledger DSN" }, stderrFile)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()
	if h := p.handle(); h != nil {
		t.Fatalf("a handle was returned for the unopenable ledger %s: %+v", unopenable, h)
	}
	if err := stderrFile.Sync(); err != nil {
		t.Fatalf("flush the stderr capture: %v", err)
	}
	reported, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read the stderr capture: %v", err)
	}
	line := string(reported)
	if !strings.Contains(line, prefetchLedgerCantOpenCause) {
		t.Errorf("the disclosed fallback line does not carry gopi's cause (%q) — the operator gets the "+
			"driver's `out of memory (14)` wording alone; got %q", prefetchLedgerCantOpenCause, line)
	}
	if !strings.Contains(line, unopenable) {
		t.Errorf("the disclosed fallback line does not name the ledger path %s; got %q", unopenable, line)
	}
	if !strings.Contains(line, prefetchWarmFallbackReason) {
		t.Errorf("the fallback DECISION is no longer named (%q) — the translation must not displace the "+
			"disclosure it rides on; got %q", prefetchWarmFallbackReason, line)
	}
}
