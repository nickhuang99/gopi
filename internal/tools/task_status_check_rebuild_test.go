// task_status_check_rebuild_test.go pins the ONE divergence that made a
// published remedy a dead end, and the gate that makes its return impossible.
//
// MEASURED (task 01a12f76, 2026-10-12, zbook's live ledger): the transition
// table advertised `pending → superseded` while the persisted
// `tasks.status` CHECK enumerated EIGHT statuses — `superseded` absent — so
// `gopi task update <pending> --status superseded --reason x` answered
// `constraint failed: CHECK constraint failed: status IN (… 'verified',
// 'reopened') (275)`. The illegal-transition error's own prescribed remedy
// walked the caller into a second, contradictory refusal.
//
// The mechanism: `tasksNeedsRebuild` — the ONE arming predicate for the table
// rebuild (SQLite cannot ALTER a CHECK) — probed a HAND-PICKED list
// ({pending, verified} + the legacy four) instead of the set the Go vocabulary
// can write. `superseded` (commit 7c32e88f4, 2026-10-07) joined taskStatusSet,
// taskAllowedTransitions, taskStatusRank and the parent-release set, and this
// probe never learned it: a ledger created between P2.5 and that epic keeps an
// eight-value CHECK forever, and every such ledger contradicts the CLI.
//
// The two tests below split the property:
//
//   - TestTaskUpdateSupersededOnPreSupersededCheckLedger is the REPRO: the
//     exact live-ledger shape (the CHECK aged to omit `superseded`), driven
//     through the REAL update entry point (TaskUpdateReason), red before the
//     fix and green after.
//   - TestTasksNeedsRebuildCoversEveryWritableStatus is the GATE: EXHAUSTIVE
//     over taskStatusSet — the omission of ANY SINGLE member must arm the
//     rebuild — plus the membership half (every status the transition table
//     can name must be in taskStatusSet, and hence in the rendered CHECK).
//     A future status therefore cannot repeat this divergence: adding it to
//     taskStatusSet arms the probe by construction, and the test is written
//     so that a probe list narrower than the set fails HERE rather than on an
//     operator's ledger.
package tools

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// statusCHECKFixtureDB pins a fresh per-test ledger (Rule 32: the scaffold
// owns the path and the environment — never a hand-rolled path, never the
// operator's profile) and returns it open for the status vocabulary, plus the
// path so the test can reopen the ledger the way a real run does.
func statusCHECKFixtureDB(t *testing.T) (context.Context, string) {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	return t.Context(), path
}

// statusCHECKRender mirrors taskSchemaStatusCHECK over an ARBITRARY member
// list: the fixture's own renderer, so a fixture CHECK is never a hand-typed
// status literal (Rule 1b/13) and the fixture cannot drift from the constants
// the production code compares against.
func statusCHECKRender(members []string) string {
	quoted := make([]string, len(members))
	for i, s := range members {
		quoted[i] = "'" + s + "'"
	}
	return strings.Join(quoted, ",")
}

// statusCHECKOmit returns taskStatusSet without the named member — the
// fixture shape of a ledger whose schema predates that value being writable.
// It fails loudly when the member is absent: a fixture asked to omit a status
// that is not in the set would silently produce the FULL set and test nothing
// (Rule 15).
func statusCHECKOmit(t *testing.T, omit string) []string {
	t.Helper()
	found := false
	out := make([]string, 0, len(taskStatusSet))
	for _, s := range taskStatusSet {
		if s == omit {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		t.Fatalf("fixture premise: %q must be a member of taskStatusSet (it is not — the test would otherwise pin the un-aged shape and prove nothing)", omit)
	}
	return out
}

// ageTasksStatusCHECK rewrites the live `tasks` table's stored DDL so its
// status CHECK enumerates exactly `members` — the physical shape of a ledger
// whose schema predates a status. It is the highest-fidelity fixture available
// for this defect: no rename, no column projection, no trigger churn (the
// table keeps every column and every companion object byte for byte; only the
// CHECK text moves), which is precisely how the live ledger diverges.
//
// PRAGMA writable_schema is the documented way to author such a fixture, and
// it is used ONLY here — a test fixture of a historical schema, never a
// production path. Every statement of the edit runs on ONE pinned connection
// (`db.Conn`): writable_schema is a PER-CONNECTION setting, and database/sql's
// pool would otherwise hand `PRAGMA writable_schema=ON`, the UPDATE and
// `PRAGMA schema_version` to three different connections — the UPDATE then
// runs where the pragma is still OFF and the edit is a silent no-op (measured
// while authoring this fixture: the aging appeared to succeed and the DDL read
// back unchanged). The connection and the handle are closed before returning
// so the next open parses the edited schema from disk, never a cached one.
func ageTasksStatusCHECK(t *testing.T, db *sql.DB, members []string) {
	t.Helper()
	ctx := t.Context()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin connection: %v", err)
	}
	defer conn.Close()

	var ddl string
	if err := conn.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
		t.Fatalf("read tasks DDL: %v", err)
	}
	aged, err := ageStatusCHECKDDL(ddl, statusCHECKRender(taskStatusSet), statusCHECKRender(members))
	if err != nil {
		t.Fatalf("age DDL: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA writable_schema=ON`); err != nil {
		t.Fatalf("writable_schema on: %v", err)
	}
	res, err := conn.ExecContext(ctx,
		`UPDATE sqlite_master SET sql = ? WHERE type='table' AND name='tasks'`, aged)
	if err != nil {
		t.Fatalf("rewrite tasks DDL: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("rewrite tasks DDL: rows=%d err=%v (want exactly 1)", n, err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA schema_version = 1`); err != nil {
		t.Fatalf("bump schema_version: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA writable_schema=OFF`); err != nil {
		t.Fatalf("writable_schema off: %v", err)
	}
	// Read the aged text back on the SAME connection: an aging step that did
	// not persist is a fixture that would prove nothing (Rule 15).
	var back string
	if err := conn.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&back); err != nil {
		t.Fatalf("read back aged DDL: %v", err)
	}
	if back != aged {
		t.Fatalf("the aged DDL did not persist:\n want %s\n  got %s", aged, back)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("unpin connection: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close after aging: %v", err)
	}
}

// ageStatusCHECKDDL replaces the rendered CHECK member list inside a table
// DDL. It refuses (rather than passing a DDL through untouched) when the exact
// from-list is not present — an aging step that matched nothing would leave the
// test asserting against a schema it never built.
func ageStatusCHECKDDL(ddl, from, to string) (string, error) {
	if !strings.Contains(ddl, from) {
		return "", errStatusCHECKMemberListAbsent
	}
	return strings.Replace(ddl, from, to, 1), nil
}

// errStatusCHECKMemberListAbsent is the refusal above, named so the failure
// names the mechanism instead of a byte offset (Rule 15).
var errStatusCHECKMemberListAbsent = errors.New(
	"tasks DDL carries no rendered status CHECK member list — the fixture's aging step matched nothing")

// TestTaskUpdateSupersededOnPreSupersededCheckLedger is the REPRO of the
// reported defect, driven through the real write entry point rather than the
// predicate.
//
// RED before the fix (measured on this branch's base): the aged ledger's CHECK
// omits `superseded`, the probe asked only about {pending, verified} — both
// PRESENT — so no rebuild armed, and TaskUpdateReason returned
// `task: update: constraint failed: CHECK constraint failed: status IN (…)`.
// GREEN after: the probe walks taskStatusSet, the omission arms the rebuild,
// the table is re-created from the same slice, and the transition lands.
func TestTaskUpdateSupersededOnPreSupersededCheckLedger(t *testing.T) {
	ctx, path := statusCHECKFixtureDB(t)

	// One row through the real enqueue path, left pending (the state the
	// transition table promotes to `superseded`).
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "row whose work moved to a successor",
		Payload: "fixture briefing",
		Source:  "auto",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := taskIDFromSubmitOutput(t, out)

	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ageTasksStatusCHECK(t, db, statusCHECKOmit(t, taskStatusSuperseded))

	// Re-open the ledger exactly as a run does: the probe must arm the
	// rebuild off the aged DDL, and nothing else is allowed to repair it.
	db2, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("reopen (the aged ledger must be repaired on open): %v", err)
	}
	defer db2.Close()

	var ddl string
	if err := db2.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
		t.Fatalf("read repaired DDL: %v", err)
	}
	if !strings.Contains(ddl, "'"+taskStatusSuperseded+"'") {
		t.Fatalf("the repaired CHECK still omits %q: %s", taskStatusSuperseded, ddl)
	}

	// The reported surface: the transition the error message advertises.
	reason := "work carried by a successor row"
	if _, err := TaskUpdateReason(id, taskStatusSuperseded, reason, ""); err != nil {
		t.Fatalf("task update --status %s must succeed on a ledger whose CHECK predates it: %v",
			taskStatusSuperseded, err)
	}
	row, err := taskByID(ctx, db2, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row == nil {
		t.Fatalf("row %s vanished", id)
	}
	if row.Status != taskStatusSuperseded {
		t.Fatalf("status after the update = %q, want %q", row.Status, taskStatusSuperseded)
	}
	if !strings.Contains(row.Reason, reason) {
		t.Fatalf("reason not persisted: %q", row.Reason)
	}
}

// TestTasksNeedsRebuildCoversEveryWritableStatus is the GATE — the property
// that makes this divergence impossible to reintroduce.
//
// (a) MEMBERSHIP: every status the transition table can NAME (a key or a
// target of taskAllowedTransitions) must be a taskStatusSet member, and the
// rendered CHECK must carry every member. A status reachable by a transition
// but absent from the set is exactly the bug: advertised by the CLI,
// rejected by storage.
//
// (b) PER-MEMBER OMISSION: for EVERY single member of taskStatusSet, aged out
// of the CHECK alone, the probe must arm the rebuild. This is the arm that
// went red: the old hand-picked probe covered {pending, verified} and was
// blind to the omission of any other member.
func TestTasksNeedsRebuildCoversEveryWritableStatus(t *testing.T) {
	ctx := t.Context()

	// (a) membership — the transition table ⊆ taskStatusSet ⊆ rendered CHECK.
	members := make(map[string]bool, len(taskStatusSet))
	for _, s := range taskStatusSet {
		members[s] = true
	}
	for from, targets := range taskAllowedTransitions {
		if !members[from] {
			t.Errorf("taskAllowedTransitions has source %q which is NOT in taskStatusSet — the transition table advertises a status storage cannot hold", from)
		}
		for _, to := range targets {
			if !members[to] {
				t.Errorf("taskAllowedTransitions[%q] names target %q which is NOT in taskStatusSet — the CLI advertises a transition the CHECK constraint refuses", from, to)
			}
		}
	}
	rendered := taskSchemaStatusCHECK()
	for _, s := range taskStatusSet {
		if !strings.Contains(rendered, "'"+s+"'") {
			t.Errorf("taskSchemaStatusCHECK omits %q (rendered: %s)", s, rendered)
		}
	}
	if len(taskStatusSet) == 0 {
		t.Fatalf("taskStatusSet is empty — the gate would pass vacuously")
	}

	// (b) per-member omission, exhaustive over taskStatusSet.
	for _, omit := range taskStatusSet {
		t.Run("omitting_"+omit, func(t *testing.T) {
			// Each subtest owns its ledger: the aging then the repair MUTATE
			// the schema, so sharing one file would let the first subtest's
			// repaired CHECK satisfy the next subtest's probe and mask a real
			// blindness (Rule 32: the scaffold owns the path per test).
			_, path := statusCHECKFixtureDB(t)

			db, err := openTasksDB(ctx, path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			ageTasksStatusCHECK(t, db, statusCHECKOmit(t, omit))

			// Re-open the aged ledger through the REAL open path: the probe
			// under test is the one openTasksDB reaches (ensureTasksSchema),
			// not a hand-called predicate.
			reopened, err := openTasksDB(ctx, path)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer reopened.Close()

			var ddl string
			if err := reopened.QueryRowContext(ctx,
				`SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&ddl); err != nil {
				t.Fatalf("read DDL: %v", err)
			}
			if !strings.Contains(ddl, "'"+omit+"'") {
				t.Fatalf("the CHECK omits %q and the real open path left it that way — a writable status the stored schema refuses is exactly the divergence this gate exists for (stored: %s)", omit, ddl)
			}
		})
	}
}
