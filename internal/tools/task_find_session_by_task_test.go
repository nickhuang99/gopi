package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionid"
)

// tsrsRowID is the ROW spelling used by the tests below: a canonical uuid,
// exactly the shape the ledger holds. These tests are about the operand's
// SHAPE, never about whether a row exists — this resolver reads the filesystem
// and never the ledger (the claim is about the tree, not about a row).
const tsrsRowID = "01a12f3c-d888-720e-b7b5-f458f55e98aa"

// tsrsBranchSpelling is tsrsRowID as the BRANCH/WORKTREE spelling — every
// non-alphanumeric character dropped (taskGroundIDStem). Measured on the host
// that produced bug row 01a12f3c: 11 containers with this spelling sat beside
// the canonical ones, all 11 with a live canonical sibling and 0 gopi-written
// files.
func tsrsBranchSpelling() string { return strings.ReplaceAll(tsrsRowID, "-", "") }

// tsrsMintRunRecord materialises a run record under root/<id>/ — the layout the
// resolver is about (a task run writes `<task_id>/<uuid>.jsonl`, whose first
// line is the session_start event carrying that taskId). It returns the file
// path.
func tsrsMintRunRecord(t *testing.T, root, id, runUUID string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mint run dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, runUUID+taskSessionJSONLExt)
	line := `{"type":"session_start","sessionId":"` + runUUID + `","taskId":"` + id + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write run record %s: %v", path, err)
	}
	return path
}

// TestTaskFindSessionJSONLByTaskRefusesTheBranchSpelling is the RED→GREEN
// regression for bug row 01a12f3c's remaining READ seam: this resolver joins the
// id into a path under the sessions root without ever asking whether this tree
// spells a task id that way.
//
// BEFORE the fix, on a tree that carries a branch-spelled container (which the
// ungated writers minted, so it is the measured state, not a hypothesis): the
// resolver joined the operand, found a directory, read its records — and the
// identity check `taskSessionTaskID(path) == taskID` made the two spellings
// AGREE (both sides are the mangled string), so a record belonging to ANOTHER
// spelling of the row was returned as this row's run. That is the silent join
// on the read side: it does not fail, it answers — with evidence out of a
// directory gopi never minted for the row.
//
// AFTER the fix the shape is decided by internal/sessionid, the SAME definition
// the two session-directory writers use (Rules 13/14), the refusal is that
// package's own sentence (reused, never re-written here), and the answer is
// EMPTY: this seam reports and never mints.
func TestTaskFindSessionJSONLByTaskRefusesTheBranchSpelling(t *testing.T) {
	root := t.TempDir()
	mangled := tsrsBranchSpelling()
	// The pre-fix litter, minted on purpose: without it the test would only be
	// re-testing a missing directory.
	tsrsMintRunRecord(t, root, mangled, tsrsRowID)

	var got []string
	stderr := captureStderr(t, func() { got = taskFindSessionJSONLByTask(root, mangled) })
	if len(got) != 0 {
		t.Fatalf("taskFindSessionJSONLByTask returned %v for the branch/worktree spelling %q — the shape gate is not on this seam", got, mangled)
	}
	if want := sessionid.Refuse(mangled).Error(); !strings.Contains(stderr, want) {
		t.Errorf("nothing on stderr carries internal/sessionid's own sentence %q (got %q) — a silent empty answer is indistinguishable from 'this row ran nowhere' (Rule 15)", want, stderr)
	}
	if !strings.Contains(stderr, tsrsRowID) {
		t.Errorf("the report %q does not name the row id %q — the near-miss has to be repairable from the message", stderr, tsrsRowID)
	}
}

// TestTaskFindSessionJSONLByTaskAcceptsAdmissibleIDs is the other half: the gate
// must refuse the near-miss ONLY, and it must never turn a read seam into a
// writer. Both admissible shapes still resolve, and the pre-existing contract
// for an unknown id (no directory → an empty answer, quietly) is untouched.
func TestTaskFindSessionJSONLByTaskAcceptsAdmissibleIDs(t *testing.T) {
	for _, id := range []string{tsrsRowID, "t-1759600000"} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			runUUID := "0f5c5d3e-0000-4000-8000-000000000001"
			want := tsrsMintRunRecord(t, root, id, runUUID)

			var got []string
			stderr := captureStderr(t, func() { got = taskFindSessionJSONLByTask(root, id) })
			if len(got) != 1 || got[0] != want {
				t.Fatalf("taskFindSessionJSONLByTask(root, %q) = %v, want [%s]", id, got, want)
			}
			if stderr != "" {
				t.Errorf("an admissible id produced stderr %q — the gate must speak only about inadmissible operands", stderr)
			}
			// A READ seam must never mint: the tree it read is exactly what it
			// found, plus nothing.
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read root: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != id {
				t.Errorf("sessions root holds %v, want exactly [%s] — this resolver must not create directories", entries, id)
			}
		})
	}
}

// TestTaskFindSessionJSONLByTaskUnknownIDStaysQuiet pins the boundary between
// the two defects, so a later change cannot widen the gate into "refuse ids
// with no container": an ADMISSIBLE id whose directory simply does not exist
// keeps its long-standing silent empty answer (the row ran under another
// layout), and nothing is written to stderr.
func TestTaskFindSessionJSONLByTaskUnknownIDStaysQuiet(t *testing.T) {
	root := t.TempDir()
	unknown := "01a12f3c-0000-4000-8000-000000000000" // admissible, absent from this root

	var got []string
	stderr := captureStderr(t, func() { got = taskFindSessionJSONLByTask(root, unknown) })
	if len(got) != 0 {
		t.Fatalf("want no paths for the unknown id %q, got %v", unknown, got)
	}
	if stderr != "" {
		t.Errorf("an admissible-but-unknown id wrote %q to stderr — the shape gate must not become a container-existence check", stderr)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Errorf("root holds %v (err %v), want empty — a read seam must not materialise anything", entries, err)
	}
}
