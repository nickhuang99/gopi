package tools

// memory_sync_remote_projection_test.go — the peer-side SELECT's OUTPUT NAMES.
//
// THE DEFECT THIS PINS (measured 2026-10-07, cross-host probe with a real
// `sqlite3` CLI): the harvest/row projections wrote `COALESCE(reason,'')` with
// no alias, and `sqlite3 -json` names such an output column with its WHOLE
// SOURCE TEXT — the JSON key came out as `COALESCE(reason,'')`, which matches
// no struct tag, so `reason`, `deliverable`, `run_method`, `run_cmd` and
// `parent_task_id` all arrived EMPTY over the wire. Every reader then saw a row
// with no why, no provenance and no dependency edge — the documented
// "chain links landed unparented" symptom, with the transport as its cause.
//
// TWO INSTRUMENTS, because they catch different things:
//
//	[shape]  a hermetic gate over the SQL text: every output expression carries
//	         an explicit alias, and the two projections share one definition
//	         (a second spelling is how the sibling drifted in the first place).
//	[wire]   the REAL `sqlite3` CLI, on a real fixture database, read back
//	         through the real decoder — the only instrument that can see the
//	         naming rule, and it is skipped (loudly) where the binary is absent.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestRemoteProjectionCarriesAliases is the hermetic half: it reads the SQL the
// transport sends and refuses an output EXPRESSION that is not named.
//
// It is deliberately written against the projection CONSTANT (not against a
// copy of it), so a new coalesced column added without an alias fails here.
func TestRemoteProjectionCarriesAliases(t *testing.T) {
	// sqlite names a COLUMN output by the column; every other output must be
	// named by the query, or the JSON key is the SQL text.
	for _, item := range splitSQLList(syncRemoteCoalescedProjection) {
		item = strings.Join(strings.Fields(item), " ")
		if item == "" {
			continue
		}
		if !strings.Contains(item, syncColumnAliasKeyword) {
			t.Fatalf("projection item %q carries no AS alias — `sqlite3 -json` would name the JSON key with this whole text, and the field would arrive empty", item)
		}
	}
	// The row leg and the anchor window must project the SAME tail: a second
	// spelling is what let them drift (Rule 1b/13).
	for name, query := range map[string]string{
		"syncRowRemoteSelect": syncRowRemoteSelect,
		"harvestRemoteSelect": harvestRemoteSelect,
	} {
		if !strings.Contains(query, syncRemoteCoalescedProjection) {
			t.Fatalf("%s carries its own projection instead of the shared one — the two legs can drift apart again", name)
		}
	}
}

// TestRemoteProjectionDecodesThroughTheRealCLI is the wire half: a throwaway
// ledger, the REAL `sqlite3 -json` the transport invokes, decoded into the very
// struct the transport decodes into.
//
// It is the regression guard for the alias defect, and it fails on the exact
// shape that shipped: without the alias the JSON key is `COALESCE(reason,”)`,
// Unmarshal leaves Reason empty, and the assertion below names the field.
func TestRemoteProjectionDecodesThroughTheRealCLI(t *testing.T) {
	bin, err := exec.LookPath(syncRemoteSQLiteBinary)
	if err != nil {
		// Rule 10.7 / honesty: this instrument needs the peer's binary, and a
		// silent pass would read as "the wire is verified" when it is not.
		t.Skipf("SKIPPED, not passed: %s is not on this host, so the wire's column NAMING rule could not be measured here", syncRemoteSQLiteBinary)
	}
	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	defer db.Close()
	const wantReason = "handed over to peer zbook"
	const wantParent = "t-parent-01"
	if _, err := db.Exec(`INSERT INTO tasks
		(task_id, summary, payload, tags, source, model, status, created_at, updated_at,
		 ended_at, deliverable, run_method, run_cmd, reason, parent_task_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"t-wire-01", "wire probe", "payload-probe", "tag-a,tag-b", "human", "",
		taskStatusPending, 100, 200, 0,
		`{"repo":"/tmp/x"}`, "run-method-probe", "run-cmd-probe", wantReason, wantParent,
	); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	query := strings.Replace(syncRowRemoteSelect, "%s",
		harvestShellQuote("t-wire-01"), 1)
	out, err := exec.Command(bin, "-json", path, query).Output()
	if err != nil {
		t.Fatalf("sqlite3 -json: %v", err)
	}
	// The transport decodes into the SELECT's own row struct and then maps it
	// onto the wire row (syncRowFromHarvest) — the decode is exercised in that
	// exact shape here, because a hand-written struct would only prove the
	// test's own tags.
	var remote []harvestRemoteRow
	if err := json.Unmarshal(out, &remote); err != nil {
		t.Fatalf("decode the peer's answer: %v (%s)", err, out)
	}
	if len(remote) != 1 {
		t.Fatalf("the query answered %d rows: %s", len(remote), out)
	}
	got := syncRowFromHarvest(remote[0])
	if got.Reason != wantReason {
		t.Fatalf("reason arrived as %q, want %q — the projection's column lost its name before it reached the wire (the raw JSON was %s)",
			got.Reason, wantReason, out)
	}
	if got.ParentTaskID != wantParent {
		t.Fatalf("parent_task_id arrived as %q, want %q — a row that travelled WITHOUT its edge is free work on the receiving ledger (Rule 33)", got.ParentTaskID, wantParent)
	}
	if got.Deliverable != `{"repo":"/tmp/x"}` {
		t.Fatalf("deliverable arrived as %q", got.Deliverable)
	}
	if got.RunMethod != "run-method-probe" || got.RunCmd != "run-cmd-probe" {
		t.Fatalf("the run provenance arrived as (%q,%q)", got.RunMethod, got.RunCmd)
	}

	// The ANCHOR WINDOW shares the projection, so it is measured by the same
	// instrument rather than trusted to be identical.
	// The SAME projection through the anchor window's query: one instrument,
	// both legs. (The SQL is passed as one argument here; on the wire the
	// login shell is what parses it, which is why the transport quotes it.)
	out, err = exec.Command(bin, "-json", path,
		strings.Replace(harvestRemoteSelect, "%.6f", "0", 1)).Output()
	if err != nil {
		t.Fatalf("%s -json (anchor window): %v", syncRemoteSQLiteBinary, err)
	}
	var harvest []harvestRemoteRow
	if err := json.Unmarshal(out, &harvest); err != nil {
		t.Fatalf("decode the anchor window: %v (%s)", err, out)
	}
	if len(harvest) != 1 {
		t.Fatalf("the anchor window answered %d rows: %s", len(harvest), out)
	}
	if h := syncRowFromHarvest(harvest[0]); h.Reason != wantReason || h.ParentTaskID != wantParent {
		t.Fatalf("the anchor window lost a name the row leg keeps: reason=%q parent=%q", h.Reason, h.ParentTaskID)
	}
}

// splitSQLList splits a projection into its top-level comma-separated items
// (parens nest, so a split on every comma would cut COALESCE(...) in half).
func splitSQLList(s string) []string {
	var items []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(items, strings.TrimSpace(s[start:]))
}
