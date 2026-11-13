package main

import (
	"strings"
	"testing"
)

// The `task update` TREE-EDGE arm, driven through the REAL BINARY (Rule 31: a
// surface in no execution has not been tested). The engine's own tests
// (internal/tools/tasks_reparent_test.go) call TaskReparent directly; this file
// asks the different question the CLI owns — does the argv an operator types
// reach that engine call, and do the parse-time refusals come back as exit 1
// with NOTHING written?
//
// The sandbox is g1NewSandbox: a private GOPI_HOME in t.TempDir() plus a
// --db-addressed ledger under it, so no probe can touch the operator's profile
// or this host's real ledger (Rule 34's criterion applied with the machinery
// this package already documents; the ledger file is created by the binary's
// own first open).
//
// Note on `--reason` and the ledger's own `task show`: the reason column is
// stamped by a STATUS move (taskUpdateStampedFrom). A pure edge move records
// the reason in the trail instead — it consumes no transition, which is the
// whole point — so the assertions below read the trail, not the column.

const (
	trFlagParent = "--parent-task-id"
	trFlagClear  = "--clear-parent"
)

// trSeed submits one row with an explicit id and an optional parent.
func trSeed(t *testing.T, sb g1Sandbox, id, parent string) {
	t.Helper()
	args := []string{"task", "submit", "row " + id, "--task-id", id, "--db", sb.db}
	if parent != "" {
		args = append(args, trFlagParent, parent)
	}
	out := sb.g1RunOK(t, args...)
	if !strings.Contains(out, id) {
		t.Fatalf("seed %s: submit did not report the id:\n%s", id, out)
	}
}

// trParent reads the row's parent_task_id back the way an operator would —
// through `task show`, not through sqlite.
func trParent(t *testing.T, sb g1Sandbox, id string) string {
	t.Helper()
	shown := sb.g1RunOK(t, "task", "show", id, "--db", sb.db)
	for _, ln := range strings.Split(shown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "parent_task_id:") {
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "parent_task_id:"))
			// orDash renders an absent value as an em dash on every
			// `task show` row — an empty column would leave the line blank.
			if v == "-" || v == "—" {
				return ""
			}
			return v
		}
	}
	t.Fatalf("task show %s rendered no parent_task_id line:\n%s", id, shown)
	return ""
}

// trEvents counts the row's trail entries through the ledger — the "nothing was
// written" half of a refusal, which stdout alone cannot prove. Raw SQL on
// purpose: the assertion must read what the binary WROTE, not a view of it.
func trEvents(t *testing.T, dbPath, id string) int {
	t.Helper()
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count trail entries for %s: %v", id, err)
	}
	return n
}

// TestTaskReparentCommandLine is the CLI contract: the four arms of the
// surface, each driven verbatim.
func TestTaskReparentCommandLine(t *testing.T) {
	sb := g1NewSandbox(t)
	trSeed(t, sb, "5cf96d8d-96d9-5efb-8ed3-9cfadc3b61d9", "")
	trSeed(t, sb, "d0790b13-8df4-5e6b-a547-b610e086b195", "")
	trSeed(t, sb, "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4", "5cf96d8d-96d9-5efb-8ed3-9cfadc3b61d9")

	// 1. SET: the edge moves and `task show` reads the new value back.
	out := sb.g1RunOK(t, "task", "update", "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4", trFlagParent, "d0790b13-8df4-5e6b-a547-b610e086b195",
		"--reason", "the producer changed", "--db", sb.db)
	if !strings.Contains(out, "5cf96d8d-96d9-5efb-8ed3-9cfadc3b61d9 -> d0790b13-8df4-5e6b-a547-b610e086b195") {
		t.Errorf("the move must name both ends:\n%s", out)
	}
	if got := trParent(t, sb, "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4"); got != "d0790b13-8df4-5e6b-a547-b610e086b195" {
		t.Errorf("task show must read the new edge, got %q", got)
	}

	// 2. IDEMPOTENT: the same edge again writes nothing.
	before := trEvents(t, sb.db, "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4")
	if o := sb.g1RunOK(t, "task", "update", "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4", trFlagParent, "d0790b13-8df4-5e6b-a547-b610e086b195",
		"--reason", "again", "--db", sb.db); !strings.Contains(o, "unchanged") {
		t.Errorf("an unchanged edge must say so:\n%s", o)
	}
	if after := trEvents(t, sb.db, "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4"); after != before {
		t.Errorf("an unchanged edge must not add a trail entry: %d → %d", before, after)
	}

	// 3. CLEAR: the row becomes parentless again.
	if o := sb.g1RunOK(t, "task", "update", "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4", trFlagClear,
		"--reason", "no longer dependent", "--db", sb.db); !strings.Contains(o, "d0790b13-8df4-5e6b-a547-b610e086b195 -> (none)") {
		t.Errorf("the cleared arm must name both ends:\n%s", o)
	}
	if got := trParent(t, sb, "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4"); got != "" {
		t.Errorf("the edge must be gone, got %q", got)
	}

	// 4. The EDGE did not consume a transition: the row is still pending.
	if shown := sb.g1RunOK(t, "task", "show", "3c2bab25-ae5e-57a5-a6c8-2ad8671cb4b4", "--db", sb.db); !strings.Contains(shown, "status:") ||
		!strings.Contains(shown, "pending") {
		t.Errorf("a reparent must not move the status:\n%s", shown)
	}
}

// TestTaskReparentCommandLineRefusals pins the four refusals an operator can
// hit at the CLI, each with exit != 0 and NOTHING written (the trail count is
// the witness — a refusal that still appended an audit row would be a silent
// half-write).
func TestTaskReparentCommandLineRefusals(t *testing.T) {
	sb := g1NewSandbox(t)
	trSeed(t, sb, "968db9f3-ffd1-5396-827d-758cca4951e4", "")
	trSeed(t, sb, "67f16e9e-1110-58aa-9838-b70aaaae7209", "968db9f3-ffd1-5396-827d-758cca4951e4")

	// A ghost parent, refused with the submit face's own sentence.
	out, code := sb.g1Run(t, "task", "update", "67f16e9e-1110-58aa-9838-b70aaaae7209", trFlagParent, "t-nosuchrow",
		"--reason", "typo", "--db", sb.db)
	if code == 0 {
		t.Fatalf("a ghost parent must exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, `parent_task_id "t-nosuchrow" does not exist`) {
		t.Errorf("the refusal must reuse submit's sentence:\n%s", out)
	}

	// A cycle: b is a child of a, so making a a child of b closes the ring.
	before := trEvents(t, sb.db, "968db9f3-ffd1-5396-827d-758cca4951e4")
	out, code = sb.g1Run(t, "task", "update", "968db9f3-ffd1-5396-827d-758cca4951e4", trFlagParent, "67f16e9e-1110-58aa-9838-b70aaaae7209",
		"--reason", "closes a ring", "--db", sb.db)
	if code == 0 {
		t.Fatalf("a cycle must exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, "cycle") || !strings.Contains(out, "67f16e9e-1110-58aa-9838-b70aaaae7209 -> 968db9f3-ffd1-5396-827d-758cca4951e4") {
		t.Errorf("the cycle refusal must name the path:\n%s", out)
	}
	if after := trEvents(t, sb.db, "968db9f3-ffd1-5396-827d-758cca4951e4"); after != before {
		t.Errorf("a refused cycle must write nothing: %d → %d", before, after)
	}

	// Self-reference.
	out, code = sb.g1Run(t, "task", "update", "968db9f3-ffd1-5396-827d-758cca4951e4", trFlagParent, "968db9f3-ffd1-5396-827d-758cca4951e4",
		"--reason", "self", "--db", sb.db)
	if code == 0 || !strings.Contains(out, "its own parent") {
		t.Errorf("a self-reference must exit non-zero and say why:\n%s", out)
	}

	// The mutually-exclusive pair, refused at PARSE time (no ledger opened).
	out, code = sb.g1Run(t, "task", "update", "968db9f3-ffd1-5396-827d-758cca4951e4", trFlagParent, "67f16e9e-1110-58aa-9838-b70aaaae7209",
		trFlagClear, "--reason", "both", "--db", sb.db)
	if code == 0 {
		t.Fatalf("the mutually-exclusive pair must exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("the pair must be refused by name:\n%s", out)
	}

	// A bare --parent-task-id with no value: refused, and pointed at the
	// clearing spelling rather than guessing which half was meant.
	out, code = sb.g1Run(t, "task", "update", "968db9f3-ffd1-5396-827d-758cca4951e4", trFlagParent, "",
		"--reason", "empty", "--db", sb.db)
	if code == 0 || !strings.Contains(out, trFlagClear) {
		t.Errorf("a valueless --parent-task-id must be refused and name --clear-parent:\n%s", out)
	}

	// A reparent with no --reason at all.
	out, code = sb.g1Run(t, "task", "update", "968db9f3-ffd1-5396-827d-758cca4951e4", trFlagClear, "--db", sb.db)
	if code == 0 || !strings.Contains(out, "must carry --reason") {
		t.Errorf("a reason-less reparent must exit non-zero:\n%s", out)
	}
}
