package agent

// chat_ops_budget_prompt_test.go — the SAME-ORIGIN half of the ops-budget
// criterion (Rules 13/14), driven from both ends.
//
// The criterion is a pair: the guard in internal/tools decides, and the prompt
// tells the model what will be decided. A pair like that rots in exactly one way
// — one side changes and the other keeps rendering an older truth — and the rot
// is invisible, because each side's own tests stay green (the guard tests call
// the guard, the prompt test reads the prompt). So these tests read BOTH:
//
//   - the NUMBERS the prompt renders are read out of the constants the guard
//     applies, so a band change that forgets the prompt fails here;
//   - the ARGUMENT NAME the prompt's retry advice names is the argument the bash
//     tool's own schema advertises, so a rename fails here rather than handing
//     the model a retry that cannot work;
//   - the PROMPT's verdict and the GUARD's verdict agree on a table of real
//     command lines, so the sentence a model reads and the decision it will meet
//     cannot disagree about the same call.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// chatOpsBudgetLongMergeFlag is a real flag of the LIVE merge spelling, taken
// from its own page (`gopi task merge --all`), so the arm below drives an
// argument shape the command documents rather than a bare word.
const chatOpsBudgetLongMergeFlag = "--all"

// chatOpsBudgetPromptBlock returns the rendered chat boundary — the paragraph the
// model is actually handed, read through the composition point the request
// builders call, never a helper this test was told about.
func chatOpsBudgetPromptBlock(t *testing.T) string {
	t.Helper()
	chatBoundaryFixtureRepo(t)
	return chatContextRulesFor(true)
}

// TestChatOpsBudgetPromptRendersTheGuardsOwnNumbers is the derivation check: the
// two bands in the prompt must be the two constants the guard enforces.
func TestChatOpsBudgetPromptRendersTheGuardsOwnNumbers(t *testing.T) {
	block := chatOpsBudgetPromptBlock(t)
	for _, want := range []string{
		tools.ChatOpClassOperatorWork.String(),
		tools.ChatOpClassRepoWrite.String(),
		tools.ChatOpClassLong.String(),
		tools.ChatOpAutoEnv,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the chat boundary does not render %q — the model is not told the band the guard applies:\n%s",
				want, block)
		}
	}
	// The prompt must NOT turn the bash tool's own cap into a criterion: a bare
	// "120s" there would be the tool's DEFAULT CAP read as a promise about the
	// ACTION, which is the confusion the category criterion refuses to make.
	if strings.Contains(block, "120s") || strings.Contains(block, "120 s") {
		t.Errorf("the chat boundary teaches the bash tool's 120 s default cap as if it were a declaration about "+
			"the action:\n%s", block)
	}
}

// TestChatOpsBudgetPromptNamesTheArgTheSchemaAdvertises is the cross-surface
// check: the retry the refusal advises is only executable if the prompt, the
// refusal and the schema spell ONE argument name.
func TestChatOpsBudgetPromptNamesTheArgTheSchemaAdvertises(t *testing.T) {
	block := chatOpsBudgetPromptBlock(t)
	if !strings.Contains(block, tools.ChatBashTimeoutArg) {
		t.Fatalf("the chat boundary never names the time-budget argument %q — the model cannot declare a bound:\n%s",
			tools.ChatBashTimeoutArg, block)
	}
	props := chatOpsBashSchemaProperties(t)
	if _, ok := props[tools.ChatBashTimeoutArg]; !ok {
		names := make([]string, 0, len(props))
		for k := range props {
			names = append(names, k)
		}
		t.Fatalf("the bash tool's schema has no %q property (it carries %v) — the prompt names an argument "+
			"nobody can pass", tools.ChatBashTimeoutArg, names)
	}
}

// chatOpsBashSchemaProperties returns the bash tool's schema properties, read from
// the source of truth in this package (never a re-parse of a rendered page).
func chatOpsBashSchemaProperties(t *testing.T) map[string]interface{} {
	t.Helper()
	for _, def := range claudeToolDefs {
		if def.Name != toolnames.ToolBash {
			continue
		}
		schema, _ := def.InputSchema.(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		if len(props) == 0 {
			t.Fatalf("the bash tool's schema carries no properties — this probe would pass vacuously")
		}
		return props
	}
	t.Fatalf("no %s tool definition — the probe cannot run", toolnames.ToolBash)
	return nil
}

// TestChatOpsBudgetPromptAndGuardAgreeOnRealCommands is the property that matters
// end to end: for a table of command lines, the sentence the model reads (a
// command that names one of the long classes must be ASKED about) and the verdict
// the guard returns must agree. Both are driven through their real entry points —
// the prompt is the rendered block, the verdict is internal/tools' criterion —
// so neither can pass by agreeing with a copy of the other.
func TestChatOpsBudgetPromptAndGuardAgreeOnRealCommands(t *testing.T) {
	block := chatOpsBudgetPromptBlock(t)
	// The prompt's own claim, read as a claim: the long classes are named, so a
	// model that reads it knows which operations it cannot start unauthorised.
	// The words come from the frozen category TABLE (never a literal list here):
	// the table is the one definition, the prompt is one rendering of it.
	long := tools.ChatOpLongOperationVocabulary()
	if len(long) < 4 {
		t.Fatalf("the long class declares %d words — too few to check the prompt's rendering", len(long))
	}
	for _, word := range []string{long[0], long[1], long[2], long[3]} {
		word = tools.ChatOpRenderedClassList([]string{word})
		if !strings.Contains(block, word) {
			t.Errorf("the chat boundary does not name the %q class among the operations that need a budget:\n%s",
				word, block)
		}
	}
	// The guard's own behaviour on the same classes, with nothing declared: the
	// two surfaces describe ONE rule, so a command in a long class must be refused
	// by the guard as well (the verdict the model will actually meet).
	//
	// THE GROUP ENTRY IS THE LIVE SPELLING (task 01a1958f): it used to be
	// `gopi merge`, which is RETIRED and classifies as OPERATOR WORK — the arm
	// would have gone on passing while proving nothing about a command the
	// console can run. Composed from the leaf constants (Rules 1/13/14).
	longMerge := runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Merge + " " + chatOpsBudgetLongMergeFlag
	for _, cmd := range []string{"make test", "go build ./...", longMerge, "npm install"} {
		adm := tools.ChatOpAdmit(cmd, "")
		if adm.Allowed() {
			t.Errorf("the prompt tells the model that %q needs a budget, but the guard admitted it with nothing "+
				"declared — the two surfaces disagree about the same call", cmd)
		}
	}
	// And the counterpart: ordinary console work must NOT be dragged into that
	// class by the prompt's wording being read as "ask about everything".
	for _, cmd := range []string{"ls -la", "git worktree list", "git status"} {
		adm := tools.ChatOpAdmit(cmd, "")
		if !adm.Allowed() {
			t.Errorf("the guard refuses %q as an undeclared long operation — the prompt would have had to tell "+
				"the model to ask about ordinary console reads", cmd)
		}
	}
}
