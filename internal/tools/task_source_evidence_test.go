package tools

// The defect this file guards (epic "task source fake human", 2026-09-23):
// `gopi task submit` (cmd/agent's runTaskSubmit) defaulted the source to the
// literal "human", so EVERY row an agent filed through the CLI claimed a
// person's authorship — 457 agent-session rows on the operator's own ledger
// carried source=human alongside a session id and a model. The operator's
// ruling: only a true human may claim human; anything else records the role it
// actually is (agent / worker / merger / server).
//
// These tests drive the FOUNDATION the fix rests on: the shared row-create
// funnel may not record a human claim it has no evidence for, and never writes
// a retired spelling.

import (
	"context"
	"testing"

	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/testutil"
)

// TestInsertTaskRowRefusesAnUnsupportedHumanClaim pins the funnel's evidence
// requirement. A probe process (this test binary) carries no agent signature,
// so it MAY claim a person in the ordinary case — the point of the assertion
// is the other half: the funnel passes every human claim through the same
// derivation the CLI uses, so a claim this process cannot support is replaced
// rather than stored. The test simulates the unsupported case by an agent
// signature installed in the environment, which is exactly how the real one
// arrives (GOPI_EXECUTOR_SESSION_ID is inherited by every process an agent
// spawns).
func TestInsertTaskRowRefusesAnUnsupportedHumanClaim(t *testing.T) {
	// The agent session's own export, plus the pid-anchored marker naming
	// SOMEONE ELSE's pid (an agent sits above this process — the shape of a
	// `gopi task submit` typed into an agent's shell).
	t.Setenv("GOPI_EXECUTOR_SESSION_ID", "01a0cdfc-92cb-745c-a1c6-f8ac947d02d6")
	t.Setenv("GOPI_AGENT_SESSION", "999999")
	t.Setenv("GOPI_HUMAN_TERMINAL", "")

	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	id, err := insertTaskRow(context.Background(), db, taskCreateFields{
		TaskID:  "t-fake-human-redproof",
		Summary: "an agent's row must not claim a person",
		Status:  taskStatusPending,
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("insertTaskRow: %v", err)
	}
	row, err := taskByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("taskByID: %v", err)
	}
	if row.Source == tasksource.Human {
		t.Fatalf("source = %q — an agent session's row was allowed to claim a person; "+
			"want %q (the derived value for a process under an agent session)",
			row.Source, tasksource.Agent)
	}
	if row.Source != tasksource.Agent {
		t.Errorf("source = %q, want %q (the derived agent role)", row.Source, tasksource.Agent)
	}
}

// TestInsertTaskRowCanonicalizesARetiredSpelling: the funnel stores the CURRENT
// vocabulary, so a caller passing the historical "llm"/"auto" does not leave a
// third spelling in the ledger (the retired aliases stay readable on the rows
// that already carry them).
func TestInsertTaskRowCanonicalizesARetiredSpelling(t *testing.T) {
	// A clean environment: a probe process is entitled to a human claim, so
	// this test exercises the canonicalization branch and not the evidence one.
	t.Setenv("GOPI_HUMAN_TERMINAL", "1")

	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	cases := map[string]string{
		"llm":  tasksource.Agent,
		"auto": tasksource.Merger, // its majority historical writer
	}
	for spelling, want := range cases {
		id, err := insertTaskRow(context.Background(), db, taskCreateFields{
			TaskID:  "t-alias-" + spelling,
			Summary: "retired spelling " + spelling,
			Status:  taskStatusPending,
			Source:  spelling,
		})
		if err != nil {
			t.Fatalf("insertTaskRow(%q): %v", spelling, err)
		}
		row, err := taskByID(context.Background(), db, id)
		if err != nil {
			t.Fatalf("taskByID(%q): %v", id, err)
		}
		if row.Source != want {
			t.Errorf("source for %q = %q, want %q (the canonical value)", spelling, row.Source, want)
		}
	}
}

// TestValidTaskSourceAcceptsTheVocabulary: validation reads the ONE list. The
// retired aliases stay accepted (a legacy row must remain readable and a peer
// ledger may still send one), and an invented value is refused.
func TestValidTaskSourceAcceptsTheVocabulary(t *testing.T) {
	accepted := []string{
		tasksource.Human, tasksource.Agent, tasksource.Worker,
		tasksource.Merger, tasksource.Server, "llm", "auto",
	}
	for _, v := range accepted {
		if !validTaskSource(v) {
			t.Errorf("validTaskSource(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "bogus", "HUMAN", "human "} {
		if validTaskSource(v) {
			t.Errorf("validTaskSource(%q) = true, want false", v)
		}
	}
}
