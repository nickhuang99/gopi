package main

// cli_tasks_multi_parent_test.go — the parser half of the multi-parent epic.
//
// THE MEASURED DEFECT this file pins: `gopi task submit "..." --parent-task-id A
// --parent-task-id B` exited 0 and stored only B. The flag was never declared
// repeatable, so memoryFlagSet's last-wins rule silently dropped the first
// occurrence — a dependency the caller stated and the ledger discarded, with no
// error anywhere (Rule 15's silent-failure shape, in the one place that decides
// what a row waits for).
//
// Two things must hold together, in the two places that can break them:
//
//   - the PARSER keeps every occurrence, in TYPED order (parent 1 first), on
//     both verbs that take the flag (submit and update);
//   - the FLAG TABLE (taskFlagMember) admits the spelling for both — an
//     advertised flag the table refuses is the class
//     TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted exists to close.
//
// Neither is a substitute for the other: a parser that reads a flag the table
// refuses never runs, and a table that admits a flag no parser reads stores
// nothing.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

const (
	mpParentA = "0199aaaa-0000-7000-8000-00000000000a"
	mpParentB = "0199aaaa-0000-7000-8000-00000000000b"
)

// TestSubmitKeepsEveryParentTaskIDFlag is the regression guard for the silent
// drop: both occurrences must survive, in the order typed.
func TestSubmitKeepsEveryParentTaskIDFlag(t *testing.T) {
	opts, err := parseTasksArgs(taskverbs.Submit, []string{
		"a child with two producers",
		"--" + taskParentTaskIDFlagName, mpParentA,
		"--" + taskParentTaskIDFlagName, mpParentB,
	})
	if err != nil {
		t.Fatalf("submit parse: %v", err)
	}
	if len(opts.parentTaskIDs) != 2 {
		t.Fatalf("both --%s occurrences must survive the parse, got %v (this is the silent-drop defect: last-wins)",
			taskParentTaskIDFlagName, opts.parentTaskIDs)
	}
	if opts.parentTaskIDs[0] != mpParentA || opts.parentTaskIDs[1] != mpParentB {
		t.Fatalf("the typed ORDER must be preserved (parent 1 first — it is the column an older reader sees), got %v", opts.parentTaskIDs)
	}
	if opts.parentTaskID != mpParentA {
		t.Fatalf("parentTaskID must be the FIRST parent, got %q", opts.parentTaskID)
	}
}

// TestSubmitSingleParentIsUnchanged: one flag is the one-parent row every
// release before this epic produced — same field, same value, same count.
func TestSubmitSingleParentIsUnchanged(t *testing.T) {
	opts, err := parseTasksArgs(taskverbs.Submit, []string{"a child", "--" + taskParentTaskIDFlagName, mpParentA})
	if err != nil {
		t.Fatalf("submit parse: %v", err)
	}
	if len(opts.parentTaskIDs) != 1 || opts.parentTaskIDs[0] != mpParentA || opts.parentTaskID != mpParentA {
		t.Fatalf("a single parent must parse exactly as before, got list=%v first=%q", opts.parentTaskIDs, opts.parentTaskID)
	}
}

// TestSubmitRefusesAnEmptyParentTaskID: an empty value is a dropped argument,
// never "no parent" (clearing has its own spelling on update, and submit has no
// dependency to clear).
func TestSubmitRefusesAnEmptyParentTaskID(t *testing.T) {
	if _, err := parseTasksArgs(taskverbs.Submit, []string{"a child", "--" + taskParentTaskIDFlagName, ""}); err == nil {
		t.Fatal("an empty --parent-task-id must be refused, not silently treated as parentless")
	}
}

// TestUpdateKeepsEveryParentTaskIDFlag is the same guard on the update face,
// where the list REPLACES the row's whole parent set.
func TestUpdateKeepsEveryParentTaskIDFlag(t *testing.T) {
	opts, err := parseTasksArgs(taskverbs.Update, []string{
		mpParentA,
		"--" + taskParentTaskIDFlagName, mpParentB,
		"--" + taskParentTaskIDFlagName, mpParentA,
		"--reason", "the producer changed",
	})
	if err != nil {
		t.Fatalf("update parse: %v", err)
	}
	if len(opts.parentTaskIDs) != 2 {
		t.Fatalf("both --%s occurrences must survive the parse, got %v", taskParentTaskIDFlagName, opts.parentTaskIDs)
	}
	if opts.parentTaskIDs[0] != mpParentB || opts.parentTaskIDs[1] != mpParentA {
		t.Fatalf("typed order must be preserved, got %v", opts.parentTaskIDs)
	}
}

// TestUpdateClearParentAndSetAreStillExclusive pins the pair's refusal after the
// multi-parent change: --clear-parent now clears a LIST, which makes "clear it
// AND set it" an even more contradictory instruction than before.
func TestUpdateClearParentAndSetAreStillExclusive(t *testing.T) {
	_, err := parseTasksArgs(taskverbs.Update, []string{
		mpParentA,
		"--" + taskParentTaskIDFlagName, mpParentB,
		"--" + taskClearParentFlagName,
		"--reason", "why",
	})
	if err == nil {
		t.Fatal("--parent-task-id and --clear-parent must stay mutually exclusive")
	}
	if !strings.Contains(err.Error(), taskClearParentFlagName) {
		t.Fatalf("the refusal must name the clearing flag, got: %v", err)
	}
}

// TestFlagTableAdmitsTheParentFlagOnBothVerbs: the admission table half. The
// help page documents --parent-task-id under both verbs, so the table must
// admit it under both or the documented invocation exits with "unknown flag".
func TestFlagTableAdmitsTheParentFlagOnBothVerbs(t *testing.T) {
	for _, verb := range []string{taskverbs.Submit, taskverbs.Update} {
		if !taskFlagMember(verb, taskParentTaskIDFlagName) {
			t.Errorf("%s --%s must be a member — the help page documents it and the parse arm reads it",
				verb, taskParentTaskIDFlagName)
		}
	}
	if !taskFlagMember(taskverbs.Update, taskClearParentFlagName) {
		t.Errorf("update --%s must be a member", taskClearParentFlagName)
	}
}

// TestSubmitWithoutParentsDoesNotSliceAPanic is the regression guard for the
// crash the FIRST real invocation found (`gopi task submit "..." --repo ... --db
// ...`, no parent flag): the dispatch arm read `opts.parentTaskIDs[1:]`, and on a
// zero-length slice that is `slice bounds out of range [1:0]` — a panic on the
// ORDINARY case (no parents at all), not an edge case. The guard is
// multiParentsRest; this pins its contract at both ends.
func TestSubmitWithoutParentsDoesNotSliceAPanic(t *testing.T) {
	if got := multiParentsRest(nil); got != nil {
		t.Fatalf("no parents must yield no tail, got %v", got)
	}
	if got := multiParentsRest([]string{mpParentA}); got != nil {
		t.Fatalf("one parent has no tail (it IS the old column), got %v", got)
	}
	if got := multiParentsRest([]string{mpParentA, mpParentB}); len(got) != 1 || got[0] != mpParentB {
		t.Fatalf("two parents put the second in the tail, got %v", got)
	}
	// And the case the panic was measured on: a submit parsed with no parent
	// flag at all must produce an empty list the dispatch arm can slice.
	opts, err := parseTasksArgs(taskverbs.Submit, []string{"an ordinary row"})
	if err != nil {
		t.Fatalf("submit parse: %v", err)
	}
	if len(opts.parentTaskIDs) != 0 {
		t.Fatalf("a parentless submit must carry no parents, got %v", opts.parentTaskIDs)
	}
	if got := multiParentsRest(opts.parentTaskIDs); got != nil {
		t.Fatalf("a parentless submit must yield no tail, got %v", got)
	}
}
