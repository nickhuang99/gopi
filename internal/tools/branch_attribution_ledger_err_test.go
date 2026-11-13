package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The `%!w(<nil>)` defect (gopi merger --status, 2026-09-17).
//
// THE REPORT: the operator read
//
//	branch attribution UNAVAILABLE: branch attribution: task ledger unreadable: %!w(<nil>)
//
// `%!w(<nil>)` is what fmt prints when a NIL error is handed to `%w` — the
// message claims a cause it can no longer name.
//
// ROOT CAUSE (code-verified): BranchAttributionReport read the ledger into
// `err`, recorded `ledgerRead := err == nil`, and then REUSED that same `err`
// for the branch listing:
//
//	rows, err := branchAttributionRows(ctx)          // the ledger error lives here
//	branches, err := branchAttributionBranches(repo) // ← OVERWRITES it with nil
//	...
//	if !ledgerRead { return out, fmt.Errorf("... unreadable: %w", err) }  // err == nil now
//
// So on the exactly interesting path — ledger unreadable AND branch listing
// fine — the wrapper received nil, and the real ledger failure was destroyed by
// the very statement that reported it.
//
// THE FIX: the ledger error keeps its OWN variable (ledgerErr) for its whole
// lifetime, and the `!ledgerRead` arm wraps THAT, which is provably non-nil on
// that arm — the nil path is unrepresentable, not merely avoided. The message
// additionally names WHICH ledger was meant and what was found there.
//
// These tests deliberately reference NO symbol introduced by the fix (only the
// pre-existing memoryDBEnv / memoryDBEnvLegacy and the operator-visible literal)
// so they COMPILE against master's code and fail there on BEHAVIOUR — that is
// what makes them a red proof rather than a compile error.

// ledgerUnreadableMarker is the operator-visible spelling of the unreadable
// ledger, pinned as a literal on purpose: this test is the contract check for
// what the operator actually reads, and a literal is the only form that also
// catches a silent rename of the constant behind it.
const ledgerUnreadableMarker = "task ledger unreadable"

// assertDiagnosableLedgerError is the shared assertion: the rendered error must
// (1) never be the nil wrap, (2) name the real cause, (3) name the ledger path,
// (4) keep the driver's own words.
func assertDiagnosableLedgerError(t *testing.T, err error, ledgerPath string) {
	t.Helper()
	if err == nil {
		t.Fatal("an unreadable ledger must surface as an error, not a clean report")
	}
	msg := err.Error()
	if strings.Contains(msg, "%!w(<nil>)") {
		t.Fatalf("the ledger error is wrapped from a NIL value — the cause was overwritten: %q", msg)
	}
	if !strings.Contains(msg, ledgerUnreadableMarker) {
		t.Fatalf("the message does not name the real cause (%q): %q", ledgerUnreadableMarker, msg)
	}
	// The message must say WHICH ledger was unreadable: a bare "unreadable" with
	// no path sends the operator hunting (Rule 15 — diagnosable, not merely true).
	if !strings.Contains(msg, ledgerPath) {
		t.Fatalf("the message does not name the ledger path it read (%s): %q", ledgerPath, msg)
	}
	// ... and it must keep the opener's own words as the wrapped cause, rather
	// than replacing the real failure with a friendlier sentence.
	if !strings.Contains(msg, "unable to open database file") && !strings.Contains(msg, "not a directory") {
		t.Fatalf("the message does not carry the underlying cause of the unreadable ledger: %q", msg)
	}
}

// TestBranchAttributionLedgerErrorIsNeverNilWrapped is the minimal repro: drive
// the REAL reader with an unusable ledger and assert the rendered message names
// a cause and a location.
//
// RED on master — the wrap rendered `%!w(<nil>)` and named no path;
// GREEN with the ledger error in its own variable.
func TestBranchAttributionLedgerErrorIsNeverNilWrapped(t *testing.T) {
	branch := "feat/attrib-ledger-err"
	repo, _ := branchAttribFixture(t, branch)

	// An unusable ledger: the path is a DIRECTORY. (An ABSENT DB would simply be
	// created by the opener and read fine — the unreadable case needs a path
	// that exists and cannot serve as a ledger.)
	bad := t.TempDir()
	t.Setenv(memoryDBEnv, bad)

	decisions, err := BranchAttributionReport(repo)
	assertDiagnosableLedgerError(t, err, bad)

	// The verdicts stay conservative: nothing was concluded from a ledger that
	// nobody could read.
	for _, d := range decisions {
		if d.Branch == branch && d.Verdict != branchAttribUngoverned {
			t.Fatalf("verdict with an unreadable ledger = %s, want %s", d.Verdict, branchAttribUngoverned)
		}
	}
}

// TestBranchAttributionLedgerCauseNamesWhereItLooked pins the DIAGNOSIS half of
// the defect: `%!w(<nil>)` named no ledger at all, so the operator could not
// tell which ledger was meant, let alone why it failed. Each case drives the
// REAL reader down a different default-location failure and asserts the
// rendered cause names both the location and what was found there.
func TestBranchAttributionLedgerCauseNamesWhereItLooked(t *testing.T) {
	branch := "feat/attrib-ledger-cause"

	t.Run("present but unopenable file", func(t *testing.T) {
		repo, _ := branchAttribFixture(t, branch)
		dir := t.TempDir()
		path := filepath.Join(dir, "ledger.db")
		if err := os.WriteFile(path, []byte("not a database at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(memoryDBEnv, path)

		_, err := BranchAttributionReport(repo)
		if err == nil {
			t.Fatal("a garbage ledger file must surface as an error")
		}
		msg := err.Error()
		if strings.Contains(msg, "%!w(<nil>)") {
			t.Fatalf("nil-wrap marker on a present-but-unopenable ledger: %q", msg)
		}
		if !strings.Contains(msg, path) {
			t.Fatalf("the cause must name the offending ledger path %s: %q", path, msg)
		}
		if !strings.Contains(msg, "could not be opened") {
			t.Fatalf("a present-but-unopenable ledger must be stated as such: %q", msg)
		}
	})

	t.Run("default channel location is named", func(t *testing.T) {
		repo, _ := branchAttribFixture(t, branch)
		// No explicit ledger channel: the DEFAULT location is the one that
		// failed, and it must be named — with the channel that resolved it —
		// because that is the only way an operator can tell it was the default
		// rather than an override.
		t.Setenv(memoryDBEnv, "")
		t.Setenv(memoryDBEnvLegacy, "")
		// A FILE where the gopi home should be: the default ledger path
		// (<home>/.gopi/memory.db) cannot even be reached, and the opener fails.
		blocker := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOPI_HOME", blocker)

		_, err := BranchAttributionReport(repo)
		if err == nil {
			t.Fatal("an unreachable default ledger must surface as an error")
		}
		msg := err.Error()
		if strings.Contains(msg, "%!w(<nil>)") {
			t.Fatalf("nil-wrap marker on the default ledger path: %q", msg)
		}
		if !strings.Contains(msg, "default location") {
			t.Fatalf("the cause must say the DEFAULT location was the one that failed: %q", msg)
		}
		if !strings.Contains(msg, blocker) {
			t.Fatalf("the cause must name the resolved default ledger path (under %s): %q", blocker, msg)
		}
	})
}

// TestBranchAttributionUnreadableLedgerReachesTheStatusRender is the
// OPERATOR-VISIBLE half: the status surface (the exact producer of the reported
// line) must render the diagnosable cause and must never render `%!w(<nil>)`.
//
// It goes through BranchAttributionLines — the ONE renderer both front doors
// compose — so a future re-plumbing of the report cannot silently reintroduce
// the nil wrap on the path the operator actually reads.
func TestBranchAttributionUnreadableLedgerReachesTheStatusRender(t *testing.T) {
	branch := "feat/attrib-status-render"
	repo, _ := branchAttribFixture(t, branch)

	bad := t.TempDir()
	t.Setenv(memoryDBEnv, bad)

	_, errText := BranchAttributionLines(repo)
	if errText == "" {
		t.Fatal("an unreadable ledger must reach the operator's line, not be dropped")
	}
	if strings.Contains(errText, "%!w(<nil>)") {
		t.Fatalf("the operator line carries the nil-wrap marker: %q", errText)
	}
	if !strings.Contains(errText, ledgerUnreadableMarker) || !strings.Contains(errText, bad) {
		t.Fatalf("the operator line must name the cause and the ledger path: %q", errText)
	}
}

// TestBranchAttributionHealthyLedgerIsUnaffected guards the other direction: a
// readable ledger must still answer, with NO error text — the fix must not make
// every healthy status report an unavailable branch axis.
func TestBranchAttributionHealthyLedgerIsUnaffected(t *testing.T) {
	branch := "feat/attrib-healthy"
	repo, _ := branchAttribFixture(t, branch) // fixture pins GOPI_MEMORY_DB to a fresh DB

	if _, errText := BranchAttributionLines(repo); errText != "" {
		t.Fatalf("a readable ledger must not report the branch axis unavailable: %q", errText)
	}
}
