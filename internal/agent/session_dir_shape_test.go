package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// The two spellings of ONE row, measured on this host (task 01a12dc6): the
// ledger/session spelling keeps the uuid's five groups, the branch/worktree
// spelling is the same 32 hex characters with every dash dropped. The defect
// these tests pin (bug row 01a12f3c): sessionDirForID joined EITHER spelling
// silently, and NewSessionLoggerForRun then MkdirAll'd it — so a caller holding
// the branch spelling BIRTHED a second sessions/<id> container beside the row's
// own, with the whole tree unaware.
const (
	sessionShapeCanonicalID = "01a12dc6-f44d-7347-9b95-2382a4a7d352"
	sessionShapeUndashedID  = "01a12dc6f44d73479b952382a4a7d352"
	// sessionShapeNearMissID is the OTHER spelling of the live defect (bug row
	// 01a12fd9, measured 2026-10-12): the same uuid with ONE dash dropped, len
	// 35. 22 such containers stand under ~/.gopi/sessions on this host, each
	// with a canonical twin.
	sessionShapeNearMissID          = "01a12f96-c2bf-732f-8cd24b63a367d4af"
	sessionShapeNearMissCanonicalID = "01a12f96-c2bf-732f-8cd2-4b63a367d4af"
)

// TestSessionRunDirRefusesANearMissGrouping is the agent-side seam half of the
// widened rule: a run whose ledger id was typed with a dash dropped must be
// REFUSED by sessionDirForID (through NewSessionLoggerForRun, the caller that
// would MkdirAll the answer) with the canonical id named, and must BIRTH
// nothing — the container beside the row's own is exactly the defect.
func TestSessionRunDirRefusesANearMissGrouping(t *testing.T) {
	_, sessionsDir := tempHome(t)

	dir, err := sessionDirForID(newUUIDv4(), sessionShapeNearMissID)
	if err == nil {
		t.Fatalf("sessionDirForID(taskID=%q) = (%q, nil) — a uuid with a dash dropped must be REFUSED, not joined into a second session directory",
			sessionShapeNearMissID, dir)
	}
	if !strings.Contains(err.Error(), sessionShapeNearMissCanonicalID) {
		t.Errorf("sessionDirForID(%q) refusal = %v, want it to name the canonical row id %q",
			sessionShapeNearMissID, err, sessionShapeNearMissCanonicalID)
	}
	if _, statErr := os.Stat(filepath.Join(sessionsDir, sessionShapeNearMissID)); !os.IsNotExist(statErr) {
		t.Errorf("the refused spelling exists at %q (stat err = %v) — refusing must mint NOTHING",
			filepath.Join(sessionsDir, sessionShapeNearMissID), statErr)
	}
}

// TestSessionLoggerRefusesTheBranchSpelling is the red-then-green regression
// guard: the run-dir seam must REFUSE an id that is not this tree's task-id
// spelling, the refusal must name the near-miss, and NOTHING may be minted.
func TestSessionLoggerRefusesTheBranchSpelling(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLoggerForRun("test-model", "", newUUIDv4(), []string{sessionShapeUndashedID})
	if err == nil {
		_ = sl.Close() //stylegate:ignore closes the fixture session log after its flush
		t.Fatalf("NewSessionLoggerForRun(taskID=%q) succeeded at %q — the branch spelling must be REFUSED, not minted into a second session directory",
			sessionShapeUndashedID, sl.Path())
	}
	if !strings.Contains(err.Error(), sessionShapeCanonicalID) {
		t.Errorf("refusal = %v, want it to name the near-miss spelling %q", err, sessionShapeCanonicalID)
	}
	entries, rerr := os.ReadDir(sessionsDir)
	if rerr != nil && !os.IsNotExist(rerr) {
		t.Fatalf("read the sessions root: %v", rerr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the refusal still created %d entries under the sessions root (%s) — a refused id must mint nothing",
			len(entries), strings.Join(names, ", "))
	}
	if _, statErr := os.Stat(filepath.Join(sessionsDir, sessionShapeUndashedID)); !os.IsNotExist(statErr) {
		t.Errorf("the refused spelling exists at %q (stat err = %v) — refusing must mint NOTHING",
			filepath.Join(sessionsDir, sessionShapeUndashedID), statErr)
	}
}

// TestSessionLoggerAcceptsCanonicalAndLegacyTaskIDs is the non-over-blocking
// half: a canonical uuid row id and the legacy/expanded `t-…` row id both keep
// landing in that row's own directory (the layout the tree already holds).
func TestSessionLoggerAcceptsCanonicalAndLegacyTaskIDs(t *testing.T) {
	_, sessionsDir := tempHome(t)

	for _, tc := range []struct {
		name string
		id   string
	}{
		{"canonical uuid", sessionShapeCanonicalID},
		{"legacy t-<epoch>", "t-1788146419"},
		{"legacy/probe t-<label>", "t-s1-layout-test"},
	} {
		sl, err := NewSessionLoggerForRun("test-model", "", newUUIDv4(), []string{tc.id})
		if err != nil {
			t.Errorf("NewSessionLoggerForRun(%q) [%s]: %v", tc.id, tc.name, err)
			continue
		}
		dir := sessionlayout.RecordContainer(sl.Path())
		if want := filepath.Join(sessionsDir, tc.id); dir != want {
			t.Errorf("run dir for %q = %q, want %q", tc.id, dir, want)
		}
		if err := sl.Close(); err != nil {
			t.Errorf("close %q: %v", tc.id, err)
		}
	}
}
