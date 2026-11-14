package tools

// mergeset_stylegate_helpers_test.go — the fixture helpers the WHOLE-REPO-RATCHET
// leg's tests need, restored ALONGSIDE mergeset_stylegate_test.go.
//
// WHY THIS FILE EXISTS AT ALL (Rule 43: judge a symbol by who references it, not
// by which commit's file it happened to live in). Commit 5a9af79 deleted 95
// merge-suite test files by compiling the tree and letting `go vet` name the
// survivors. That method has one blind spot, and this leg is exactly it: go vet
// can only see a reference from a file it still HAS. A file deleted in the same
// sweep takes its own helpers' only callers with it, so nothing in the remaining
// tree referenced mgToolchain, mgDeclareBuild, mgLedger or mgNotifySink — vet
// said nothing about them, and they were deleted with rebuild_vet_mergeset_test.go
// even though the ratchet leg's own file needed them. Restoring that file alone
// therefore does not compile.
//
// The eight symbols below are copied VERBATIM from their pre-deletion sites, not
// rewritten: mgToolchain / mgDeclareBuild / mgLedger / mgNotifySink from
// 5a9af79^:internal/tools/rebuild_vet_mergeset_test.go, and the md* ledger
// READBACK pair from 5a9af79^:internal/tools/mergeset_landing_probe_test.go
// (which the deleted stylegate test drives through mgLedger's database). Each is
// general-purpose — none of them names the merge machinery — so they are not
// retired with it; they move here, in the same commit that restores their caller.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// mgDeclareBuild writes a settings document that declares repo's build contract,
// and points the process at it. A test-local profile: nothing here reads the
// operator's own settings.
func mgDeclareBuild(t *testing.T, repo, build string) {
	t.Helper()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	body, err := json.Marshal(map[string]any{
		"repos": map[string]any{repo: map[string]any{"build": build, "build_timeout_sec": 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
}

// mgToolchain runs the toolchain over a DETACHED worktree of a revision, and
// returns the combined output with its exit code.
//
// It measures the tree OF THAT REVISION (never the fixture's own checkout — the
// measurement must not depend on what the working directory holds, which is the
// property the step itself asserts).
func mgToolchain(t *testing.T, repo, rev string, args ...string) (string, int) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wt")
	// CREATE AND RELEASE AS ONE ACT (merge_build_gate_scratch_worktree.go).
	// `t.TempDir()` removes the DIRECTORY at the end of the test but never the
	// git registration, which lives in `<repo>/.git/worktrees/<id>/` — so the
	// hand-rolled `worktree add` this helper used to run left a prunable entry in
	// the repository for good, and an interrupted add left the LOCKED variant no
	// prune can collect. The release below is the measured ladder (clear a stale
	// lock → remove --force → RemoveAll → prune), run on every exit path.
	release, err := mergeBuildGateScratchWorktree(repo, dir, rev, func() error {
		_, cerr := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
			mergeBuildGateWorktreeDetachFlag, dir, rev)
		return cerr
	})
	if err != nil {
		t.Fatalf("worktree add %s: %v", rev, err)
	}
	defer release()
	spec, reason := mergesetGateResolveSpec(repo)
	if reason != "" {
		t.Fatalf("the fixture cannot resolve the gate's measurement contract: %s", reason)
	}
	out := mergesetGateExecute(dir, append([]string{vetGateToolchainBinary}, args...), spec)
	if out.Abstain != "" {
		t.Fatalf("toolchain %v ABSTAINED: %s", args, out.Abstain)
	}
	code := 0
	if out.Err != nil {
		code = 1
	}
	return vetGateOutput(out.Stdout, out.Stderr), code
}

// mgNotifySink points the notify pipeline at a file the test reads back (the
// same subscription an operator already has).
func mgNotifySink(t *testing.T) string {
	t.Helper()
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", BuildFailedEvent)
	return eventsFile
}

// mgLedger points the ledger at a fresh DB so a bug-row arm writes nothing to
// the operator's own ledger, and returns the path for assertions. The path comes
// from the scaffold (Rule 32), never from a hand-rolled filepath.Join(t.TempDir()).
func mgLedger(t *testing.T) string {
	t.Helper()
	return testutil.NewTestDBPath(t)
}

// mdLandingBugRow is one bug-tagged ledger row read back through the schema's
// own columns.
type mdLandingBugRow struct {
	taskID  string
	summary string
	tags    string
	payload string
}

// mdLandingBugRows reads every bug-tagged row the ledger holds.
func mdLandingBugRows(t *testing.T, dbPath string) []mdLandingBugRow {
	t.Helper()
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open the fixture ledger %s: %v", dbPath, err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT task_id, summary, tags, payload FROM tasks WHERE tags LIKE ` +
		"'%" + bugTag + "%' ORDER BY id")
	if err != nil {
		t.Fatalf("query the fixture ledger's bug rows: %v", err)
	}
	defer rows.Close()
	var out []mdLandingBugRow
	for rows.Next() {
		var row mdLandingBugRow
		if err := rows.Scan(&row.taskID, &row.summary, &row.tags, &row.payload); err != nil {
			t.Fatalf("scan a fixture bug row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the fixture ledger's bug rows: %v", err)
	}
	return out
}

// mdLandingFindRow returns the row whose summary is the defect's identity.
func mdLandingFindRow(rows []mdLandingBugRow, summary string) *mdLandingBugRow {
	for i := range rows {
		if rows[i].summary == summary {
			return &rows[i]
		}
	}
	return nil
}

// mdLedgerSummary is the identity the LEDGER actually stores for a summary: the
// same two write-time steps insertTaskRow applies, in the same order —
// sanitizeTaskSummary (the 200-rune cap) and then strings.TrimSpace.
//
// It is needed because the leg's own mergesetGateBugSummary cuts the headline at
// exactly taskSummaryMaxRunes and does NOT trim: when that cut lands on a space
// between two words, the stored summary is one character SHORTER than the string
// the leg computed. The fixture's repo path length varies from run to run (it
// lives under the scratch suite dir), which moves where the cut lands — measured:
// an untrimmed comparison of the two strings failed 2 of 6, then 3 of 6, runs,
// while comparing against this identity passed 8 of 8.
//
// The cast is a NARROWING, never a widening: the leg's own dedup probe trims the
// same way (reworkSummaryBlocked, rework_handoff.go:121), so the row is not
// double-filed — this is only the reader agreeing with the store about what a
// summary IS.
func mdLedgerSummary(summary string) string {
	return strings.TrimSpace(sanitizeTaskSummary(summary))
}
