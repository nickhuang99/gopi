package tools

// task_transfer_arrival_wire_test.go — the WIRE half of the pull leg's edge.
//
// THE DEFECT THIS PINS (measured 2026-10-07, the pull leg's FIRST live run; the
// same root cause the window leg's gate pins, a second transport): `sqlite3
// -json` names a result column after SQLite's own RESULT name. A BARE column
// keeps its name; an EXPRESSION is named by its WHOLE SOURCE TEXT. Reproduced
// on this host against a table that really holds the column:
//
//	sqlite3 -json     "SELECT a FROM t"                            -> [{"a":"x"}]
//	sqlite3 -json     "SELECT COALESCE(parent_task_id,'') FROM t"  -> [{"COALESCE(parent_task_id,'')":"P1"}]   ← the defect
//	sqlite3 -json     "SELECT COALESCE(parent_task_id,'') AS parent_task_id FROM t" -> [{"parent_task_id":"P1"}]
//
// So the unaliased shape came back under the key `COALESCE(parent_task_id,'')`,
// encoding/json — whose match is case-insensitive but does NOT skip punctuation
// — bound none of it, and the row landed with an EMPTY edge while the pull
// reported `stored`. The dependency edge (Rule 33), the one thing this leg
// exists to carry, was lost in transport with no error and no counter (Rule 15).
//
// WHY THIS ONE RUNS THE REAL BINARY. Every other test of this leg injects the
// Fetch closure, so the ssh+sqlite3 surface never executes — which is exactly
// how the defect shipped green. memory_sync_harvest_wire_test.go's positional
// gate reads the WINDOW leg's shared projection and memory_sync_remote_projection_test.go
// decodes the row SELECT's own list into harvestRemoteRow; both are STATIC
// contracts over the query TEXT, and neither can see a key that arrives under a
// name the text does not predict. This is the dynamic complement: it runs the
// REAL `sqlite3 -json` on the REAL query and asks whether the leg's edge
// actually BINDS.
//
// IT SKIPS LOUDLY WHEN THE BINARY IS ABSENT (Rule 10.7): a silent pass would
// read as "the wire is verified" while nothing had been measured.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestRowRemoteSelectBindsThroughTheRealCLI is the dynamic half of the pull
// leg's wire contract.
func TestRowRemoteSelectBindsThroughTheRealCLI(t *testing.T) {
	bin, err := exec.LookPath(syncRemoteSQLiteBinary)
	if err != nil {
		// Honesty over green: the instrument needs the binary the transport
		// invokes, and this host may not have it.
		t.Skipf("SKIPPED, not passed: %s is not on this host, so the pull leg's column NAMING could not be measured here", syncRemoteSQLiteBinary)
	}
	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	defer db.Close()
	const wantID = "t-arrival-wire"
	const wantParent = "t-arrival-wire-parent"
	const wantReason = "why-probe"
	if _, err := db.Exec(`INSERT INTO tasks
		(task_id, summary, payload, tags, source, model, status, created_at, updated_at,
		 ended_at, deliverable, run_method, run_cmd, reason, parent_task_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wantID, "edge probe", "payload-probe", "tag-a", "human", "",
		taskStatusPending, 100, 200, 0,
		"deliverable-probe", "run-method-probe", "run-cmd-probe", wantReason, wantParent,
	); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	// The peer's answer, verbatim from the SELECT the transport sends.
	query := strings.Replace(syncRowRemoteSelect, "%s", harvestShellQuote(wantID), 1)
	answer := arrivalWireRun(t, bin, path, query)
	if len(answer) != 1 {
		t.Fatalf("the query answered %d rows: %+v", len(answer), answer)
	}
	// ① THE IDENTITY COLUMNS BIND on this leg's own shape — the weaker, honest
	//    contract, and the one that separates "the answer does not bind" from
	//    "the peer's query is not ours": the latter is not this leg's defect.
	if answer[0].TaskID != wantID {
		t.Fatalf("the row leg's SELECT answered a row whose id did not bind (%q, want %q) — the query is not the one this leg decodes", answer[0].TaskID, wantID)
	}
	// ② THE EDGE BINDS — the property this leg exists for, and the one the
	//    static gates cannot see. It fails on the exact shape that shipped: the
	//    key becomes `COALESCE(parent_task_id,'')`, the wire struct's tag does
	//    not match it, and the field stays empty while the pull reports
	//    `stored`.
	if answer[0].ParentTaskID != wantParent {
		t.Fatalf("the dependency edge did not bind through the real `%s -json` (ParentTaskID=%q, want %q) — the remote column's RESULT name must equal the wire struct's tag (task_transfer_eligibility.go / memory_sync_row.go own that name):\n%s",
			syncRemoteSQLiteBinary, answer[0].ParentTaskID, wantParent, query)
	}
	// ③ THE OTHER COALESCED COLUMNS bind too: the same naming rule governs every
	//    expression on this SELECT, so the fix has to hold for all of them and
	//    not just the edge (the mutation this pins: aliasing one column and
	//    leaving the rest is a half-fix that still loses data).
	for _, probe := range []struct {
		name string
		got  string
		want string
	}{
		{"reason", answer[0].Reason, wantReason},
		{"run_method", answer[0].RunMethod, "run-method-probe"},
		{"deliverable", answer[0].Deliverable, "deliverable-probe"},
	} {
		if probe.got != probe.want {
			t.Fatalf("the coalesced column %s did not bind through the real `%s -json` (%q, want %q):\n%s",
				probe.name, syncRemoteSQLiteBinary, probe.got, probe.want, query)
		}
	}
	// ④ THE PEER'S OWN SHAPE IS NOT EXERCISED HERE ON PURPOSE, and the reason is
	//    measured rather than assumed: `COALESCE(parent_task_id,'')` — aliased or
	//    not — already REFUSES to prepare against a peer whose tasks table has no
	//    such column (`no such column: parent_task_id`), which is true of the
	//    unaliased shape this leg shipped with just as much as of the aliased
	//    one. So this gate cannot hold either answer for "an older peer": the
	//    aliasing change neither caused that exposure nor can it close it.
	//    BOUNDED, and worth stating where a reader will look: the column is added
	//    by the ledger's own migration (`internal/tools/tasks.go`: ADD COLUMN
	//    parent_task_id), so the exposure is a peer that never ran a build
	//    carrying it — and it fails LOUDLY at prepare, never as a silently
	//    edgeless row. Asserting a tolerance here would invent a contract nothing
	//    in this transport has ever had.
}

// arrivalWireRun executes one query through the REAL peer-side binary and
// decodes the answer the way the transport decodes it, returning the wire rows.
func arrivalWireRun(t *testing.T, bin, dbPath, query string) []harvestRemoteRow {
	t.Helper()
	out, err := exec.Command(bin, "-json", dbPath, query).Output()
	if err != nil {
		t.Fatalf("%s -json: %v", syncRemoteSQLiteBinary, err)
	}
	var remote []harvestRemoteRow
	if err := json.Unmarshal(out, &remote); err != nil {
		t.Fatalf("decode the peer's answer: %v (%s)", err, out)
	}
	return remote
}
