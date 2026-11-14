package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// The two spellings of ONE row, measured on this host (task 01a12dc6): the
// ledger/session spelling carries the uuid's five groups, the branch/worktree
// spelling is the same 32 hex characters with every dashes dropped
// (taskGroundIDStem's sanitize, internal/tools/task_run_ground.go). The defect
// these tests pin: the session-dir seam joined EITHER spelling silently, so a
// caller holding the branch spelling minted a SECOND container beside the
// row's own.
const (
	sessionShapeCanonicalID = "01a12dc6-f44d-7347-9b95-2382a4a7d352"
	sessionShapeUndashedID  = "01a12dc6f44d73479b952382a4a7d352"
	// sessionShapeNearMissID is the OTHER spelling of the live defect (bug row
	// 01a12fd9, measured 2026-10-12): the same uuid with ONE dash dropped, len
	// 35. 22 such containers stand under ~/.gopi/sessions on this host, each
	// with a canonical twin — the shape the first fix (dashless only) did not
	// cover, and the one a caller reading a rendered listing really types.
	sessionShapeNearMissID          = "01a12f96-c2bf-732f-8cd24b63a367d4af"
	sessionShapeNearMissCanonicalID = "01a12f96-c2bf-732f-8cd2-4b63a367d4af"
)

// TestTaskSessionDirRefusesANearMissGrouping is the seam half of the widened
// rule: the id is a uuid's 16 bytes with the FOURTH dash dropped, so the ledger
// has a row for it under the canonical spelling and both spellings' containers
// can stand side by side. The seam must refuse it BY NAME and mint nothing —
// this is the caller's only chance to learn which id it meant.
func TestTaskSessionDirRefusesANearMissGrouping(t *testing.T) {
	home := covLifeSessionEnv(t)

	sd, err := TaskSessionDir(sessionShapeNearMissID)
	if err == nil {
		t.Fatalf("TaskSessionDir(%q) = (%q, nil) — a uuid with a dash dropped must be REFUSED, not joined into a second session directory",
			sessionShapeNearMissID, sd)
	}
	if !strings.Contains(err.Error(), sessionShapeNearMissCanonicalID) {
		t.Errorf("TaskSessionDir(%q) refusal = %q, want it to name the canonical row id %q",
			sessionShapeNearMissID, err, sessionShapeNearMissCanonicalID)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".gopi", config.SessionsDirName, sessionShapeNearMissID)); !os.IsNotExist(statErr) {
		t.Errorf("the refusal left %q on disk (stat err = %v) — refusing must mint NOTHING",
			filepath.Join(home, ".gopi", config.SessionsDirName, sessionShapeNearMissID), statErr)
	}
}

// TestTaskSessionDirRefusesTheBranchSpelling is the regression guard for the
// silent second container. The seam must REFUSE an id that is not this tree's
// task-id spelling, and the refusal must name the near-miss spelling so the
// caller can repair its own call site instead of hunting the tree.
func TestTaskSessionDirRefusesTheBranchSpelling(t *testing.T) {
	home := covLifeSessionEnv(t)

	sd, err := TaskSessionDir(sessionShapeUndashedID)
	if err == nil {
		t.Fatalf("TaskSessionDir(%q) = (%q, nil) — the branch spelling must be REFUSED, not joined into a second session directory",
			sessionShapeUndashedID, sd)
	}
	if !strings.Contains(err.Error(), sessionShapeCanonicalID) {
		t.Errorf("TaskSessionDir(%q) refusal = %q, want it to name the near-miss spelling %q",
			sessionShapeUndashedID, err, sessionShapeCanonicalID)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".gopi", config.SessionsDirName, sessionShapeUndashedID)); !os.IsNotExist(statErr) {
		t.Errorf("the refusal left %q on disk (stat err = %v) — refusing must mint NOTHING",
			filepath.Join(home, ".gopi", config.SessionsDirName, sessionShapeUndashedID), statErr)
	}
}

// TestTaskSessionDirAcceptsCanonicalAndLegacySpellings is the non-over-blocking
// half: both spellings this tree really mints keep resolving — a canonical uuid
// row id and the legacy/expanded `t-…` row id (t-<epoch>, t-<epoch>-N and the
// probe/legacy `t-<label>` rows the ledger still holds).
func TestTaskSessionDirAcceptsCanonicalAndLegacySpellings(t *testing.T) {
	home := covLifeSessionEnv(t)
	root := filepath.Join(home, ".gopi", config.SessionsDirName)

	for _, tc := range []struct {
		name string
		id   string
	}{
		{"canonical uuid", sessionShapeCanonicalID},
		{"legacy t-<epoch>", "t-1788146419"},
		{"legacy t-<epoch>-<n>", "t-1788146419-2"},
		{"legacy/probe t-<label>", "t-cov-shape"},
		{"padded canonical", "  " + sessionShapeCanonicalID + "\n"},
	} {
		sd, err := TaskSessionDir(tc.id)
		if err != nil {
			t.Errorf("TaskSessionDir(%q) [%s]: %v", tc.id, tc.name, err)
			continue
		}
		if want := filepath.Join(root, strings.TrimSpace(tc.id)); sd != want {
			t.Errorf("TaskSessionDir(%q) = %q, want %q", tc.id, sd, want)
		}
	}
}
