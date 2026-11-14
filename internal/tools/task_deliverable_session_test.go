// The deliverable selector → SESSION resolution: `gopi session log|raw|stats --branch
// B` / `--worktree PATH` must land on the session the row is running, which is
// the whole point of the address (`git branch --show-current` / `pwd` are what
// an operator standing in a task worktree holds; the session id is not).
//
// What this file pins is the TOOLS-layer step (row → session id). The selector →
// row step belongs to task_deliverable_selector_test.go and is not re-tested
// here; the wiring of both into the three verbs is pinned at the CLI entry
// (cmd/agent/cli_session_selector_test.go).
//
// Rule 32: the ledger comes from the scaffold (testutil.NewTestDBPath) and rows
// are created through the tools layer's own writer, exactly as
// cli_tasks_show_selector_test.go's seed does.

package tools

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// sessionSelectorSeed files one ledger row whose deliverable blob records the
// branch/worktree and whose session_id column records the running session —
// the shape every row created through the dispatch path carries.
func sessionSelectorSeed(t *testing.T, dbPath, taskID, status, branch, worktree, sessionID string) {
	t.Helper()
	if _, err := TaskNew("session selector fixture "+taskID, taskID, sessionID, ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
	blob := `{"branch":"` + branch + `","worktree":"` + worktree + `"}`
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`, status, blob, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// TestResolveSessionByDeliverableAddressesTheRowsSession is the epic's headline
// at the tools layer: the branch and the worktree reach the SAME session id the
// task_id does, and the brief carries the row that owns it.
func TestResolveSessionByDeliverableAddressesTheRowsSession(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	const (
		taskID    = "t-session-selector"
		branch    = "feat/session-selector"
		worktree  = "/home/user/workspace/gopi-session-selector"
		sessionID = "3e1f3d26-b5e4-455a-b7c0-bcaeeb1f775a"
	)
	sessionSelectorSeed(t, dbPath, taskID, TaskStatusInProgress, branch, worktree, sessionID)

	for name, sel := range map[string]DeliverableSelector{
		"--branch":   {Branch: branch},
		"--worktree": {Worktree: worktree},
	} {
		brief, err := ResolveSessionByDeliverable(sel, dbPath)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if brief.SessionID != sessionID {
			t.Errorf("%s resolved session %q, want %q", name, brief.SessionID, sessionID)
		}
		if brief.TaskID != taskID {
			t.Errorf("%s resolved row %q, want %q", name, brief.TaskID, taskID)
		}
		if !brief.Readable || brief.RowsWithBranch == 0 {
			t.Errorf("%s: the resolution must carry the ledger evidence it decided from, got readable=%v rows=%d",
				name, brief.Readable, brief.RowsWithBranch)
		}
		if len(brief.Owners) != 1 || brief.Owners[0].TaskID != taskID {
			t.Errorf("%s: owners = %+v, want the one owning row", name, brief.Owners)
		}
	}
}

// TestResolveSessionByDeliverableRefusalsNameTheRepair pins the three refusals
// whose repairs differ: no owner (a different verb answers it), no address at
// all, and a row with no session yet. Each must be an error that NAMES the
// condition — never an empty id handed to a verb that would then read a
// directory listing as its answer (Rule 15).
func TestResolveSessionByDeliverableRefusalsNameTheRepair(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	sessionSelectorSeed(t, dbPath, "t-selector-pending", TaskStatusPending,
		"feat/selector-pending", "", "")

	t.Run("no owner", func(t *testing.T) {
		_, err := ResolveSessionByDeliverable(DeliverableSelector{Branch: "feat/selector-nobody"}, dbPath)
		if err == nil {
			t.Fatal("an unknown branch must fail, not resolve")
		}
		if !errors.Is(err, ErrDeliverableNoOwner) {
			t.Errorf("the no-owner refusal must stay recognizable (ErrDeliverableNoOwner): %v", err)
		}
		if !strings.Contains(err.Error(), "feat/selector-nobody") {
			t.Errorf("the refusal must name the selector: %v", err)
		}
	})

	t.Run("no selector", func(t *testing.T) {
		if _, err := ResolveSessionByDeliverable(DeliverableSelector{}, dbPath); err == nil {
			t.Fatal("an empty selector must be refused")
		}
	})

	t.Run("both selectors", func(t *testing.T) {
		_, err := ResolveSessionByDeliverable(DeliverableSelector{Branch: "feat/x", Worktree: "/wt"}, dbPath)
		if err == nil {
			t.Fatal("--branch together with --worktree must be refused")
		}
		if !strings.Contains(err.Error(), "filter pair") {
			t.Errorf("the refusal must say the two spellings name ONE row: %v", err)
		}
	})

	t.Run("row with no session yet", func(t *testing.T) {
		_, err := ResolveSessionByDeliverable(DeliverableSelector{Branch: "feat/selector-pending"}, dbPath)
		if err == nil {
			t.Fatal("a row that records no session must not resolve to an empty id")
		}
		if !strings.Contains(err.Error(), "records no session") {
			t.Errorf("the refusal must name the condition (no session recorded yet): %v", err)
		}
		if !strings.Contains(err.Error(), "t-selector-pending") {
			t.Errorf("the refusal must name the row, so the operator can go look at it: %v", err)
		}
	})
}
