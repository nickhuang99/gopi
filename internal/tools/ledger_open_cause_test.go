// ledger_open_cause_test.go — the SQLITE_CANTOPEN translation, pinned at the
// seam that owns it.
//
// WHY THIS TEST LIVES HERE rather than only in cmd/agent's session-list arms: a
// real code-14 error needs a path the DRIVER cannot open while the DIRECTORY is
// already there (the missing-directory arm is a different, also-correct
// message). The tools layer can produce that state directly, and it is where the
// translation is implemented, so this red-proof is about one thing: does a
// CANTOPEN arrive as gopi's sentence instead of the driver's
// "unable to open database file: out of memory (14)".
package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sqlitecode"
)

// cantOpenIndexPath produces a path the DRIVER cannot open while its parent
// directory exists: the ledger name is itself an existing directory. The
// openability probe passes (fileExists is true), the parent-directory guard has
// nothing to make, and openDB's first statement is what fails — which is the
// exact shape the real defect produced.
func cantOpenIndexPath(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	path := filepath.Join(parent, "ledger-that-is-a-directory")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir the colliding name: %v", err)
	}
	return path
}

// TestLedgerIndexOpenCauseNamesThePathAndDispatchesTheFalseCause is the
// translation pin: a code-14 failure becomes a gopi sentence carrying the PATH
// and the result CODE, states the path/permission cause, and explicitly denies
// the allocation reading the driver's own wording invites.
func TestLedgerIndexOpenCauseNamesThePathAndDispatchesTheFalseCause(t *testing.T) {
	path := cantOpenIndexPath(t)

	db, err := openLedgerIndexDB(context.Background(), path, ensureChatSchema)
	if err == nil {
		db.Close() //stylegate:ignore test handle closed after the open unexpectedly succeeded
		t.Fatalf("opening a ledger whose name is an existing DIRECTORY succeeded (%s) — the state this test "+
			"needs was not produced", path)
	}

	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("the failure does not name the ledger path %s — the driver's own sentence names neither the "+
			"file nor the probe; got %q", path, msg)
	}
	if !strings.Contains(msg, sqlitecode.CantOpenCause) {
		t.Errorf("the failure does not state gopi's cause (%q) — a reader is left with the driver's wording, "+
			"which reads as an allocation failure; got %q", sqlitecode.CantOpenCause, msg)
	}
	// The recognition must have fired: this is a real CANTOPEN, not a guess.
	if !sqlitecode.Is(err) {
		t.Errorf("sqlitecode.Is did not recognise the driver's CANTOPEN in %q — the classifier is what decides "+
			"whether the translation applies at all", msg)
	}
}

// TestLedgerIndexOpenCauseOnlyTouchesCantOpen is the refuse-to-overreach half:
// the translation is a classifier, not a blinder. Every failure gets its PATH
// named (a reader needs the file either way), a non-CANTOPEN error keeps its own
// cause and its wrap chain, and a nil error stays nil so a caller's `err != nil`
// test keeps meaning what it meant.
func TestLedgerIndexOpenCauseOnlyTouchesCantOpen(t *testing.T) {
	if got := ledgerIndexOpenCause(nil, "/some/path"); got != nil {
		t.Errorf("ledgerIndexOpenCause(nil) = %v, want nil — a nil error must stay nil", got)
	}

	plain := errors.New("the disk caught fire")
	got := ledgerIndexOpenCause(plain, "/some/ledger.db")
	if got == nil || !errors.Is(got, plain) {
		t.Fatalf("a non-CANTOPEN error lost its chain: %v", got)
	}
	if strings.Contains(got.Error(), sqlitecode.CantOpenCause) {
		t.Errorf("a non-CANTOPEN failure was given the CANTOPEN cause — the classifier overreached: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "/some/ledger.db") {
		t.Errorf("a failure still does not name the ledger path: %q", got.Error())
	}
}

// TestFreshProfileGetsAnIndexInsteadOfADegradedRead is the ROOT-CAUSE pin, at
// the seam where the defect was: opening the chat index when the profile
// directory does not exist yet must CREATE the profile and hand back a usable
// index, not fail with SQLITE_CANTOPEN.
//
// The sibling face (`gopi task list`) already passed this, because it reaches
// initKnowledgeBase which mkdirs. The chat opener called openDB directly, whose
// first statement creates the FILE and therefore needs the DIRECTORY — so on a
// fresh GOPI_HOME the listing reported a degraded index while the profile it was
// reading had simply never been made.
func TestFreshProfileGetsAnIndexInsteadOfADegradedRead(t *testing.T) {
	root := t.TempDir()
	// Only `root` exists; the profile directory does NOT.
	path := filepath.Join(root, "not-created-yet", "memory.db")

	db, err := openLedgerIndexDB(context.Background(), path, ensureChatSchema)
	if err != nil {
		t.Fatalf("opening the chat index on a profile directory that does not exist failed: %v\n"+
			"This is the defect: the opener must create the profile, as the tasks face already does.", err)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("closing the ledger at %s failed: %v", path, closeErr)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("no ledger at %s after a successful open: %v", path, statErr)
	}
}
