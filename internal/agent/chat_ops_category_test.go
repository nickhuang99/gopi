package agent

// chat_ops_category_test.go — the SAME-ORIGIN half of the CATEGORY criterion
// (Rules 13/14), driven from both ends, plus the three-path repro the operator's
// ruling defines.
//
// The criterion is a pair: the guard in internal/tools decides, and the chat
// prompt tells the model what will be decided. A pair like that rots in exactly
// one way — one side changes and the other keeps teaching an older truth — and
// the rot is invisible, because each side's own tests stay green. So these tests
// read BOTH through their real entry points:
//
//   - the CLASSES the prompt renders are the class values the guard reports for a
//     real command line (tools.ChatOpAdmit), so a class change that forgets the
//     prompt fails here;
//   - the VOCABULARY the prompt names is the vocabulary the guard matches on
//     (tools.ChatOpLongOperationVocabulary / ChatOpRepoWriteVocabulary), so an
//     operation moved between classes fails here rather than teaching the model a
//     boundary the mechanism does not apply;
//   - the ONE hard no-go is still a no-go on BOTH sides: the prompt routes a
//     repository write to `gopi task submit` and the guard refuses it, at every
//     posture — asserted together, because a prompt that stopped saying it while
//     the guard still refused would be the model's problem to discover.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/tools"
)

// chatOpLongMergeLiveFlag is a real flag of the LIVE merge spelling, taken from
// its own page (`gopi task merge --all`: sweep every local branch except the
// integration branch) — the arm below drives the spelling an operator can
// actually type, not the retired flat one.
const chatOpLongMergeLiveFlag = "--all"

// chatOpCategoryPromptBlock returns the rendered chat boundary + duty paragraphs —
// the text the model is actually handed, read through the composition point the
// request builders call.
func chatOpCategoryPromptBlock(t *testing.T) string {
	t.Helper()
	chatBoundaryFixtureRepo(t)
	return chatContextRulesFor(true)
}

// TestChatOpCategoryPromptRendersTheGuardsOwnClasses is the derivation check: the
// three class labels in the prompt are the three values the guard can report, and
// the running class's own vocabulary is rendered rather than paraphrased.
func TestChatOpCategoryPromptRendersTheGuardsOwnClasses(t *testing.T) {
	block := chatOpCategoryPromptBlock(t)
	for _, want := range []string{
		tools.ChatOpClassOperatorWork.String(),
		tools.ChatOpClassRepoWrite.String(),
		tools.ChatOpClassLong.String(),
		tools.ChatOpAutoEnv,
		tools.ChatOpAutoSettingsKey,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the chat prompt does not render %q — the model is not told the class the guard applies:\n%s",
				want, block)
		}
	}
	// The long class's vocabulary is the guard's LIST, not a selection from it: a
	// prompt naming fewer operations than the guard matches would leave the model
	// starting an operation the dispatch then refuses.
	for _, w := range tools.ChatOpLongOperationVocabulary() {
		// Rendered the way the prompt renders it: a category line ends in the
		// wildcard ("git add *"), and the phrase the model reads is the prefix.
		if w = tools.ChatOpRenderedClassList([]string{w}); !strings.Contains(block, w) {
			t.Errorf("the prompt does not name the long operation %q, which the guard refuses when it is "+
				"unauthorised:\n%s", w, block)
		}
	}
	// And the removed stopwatch must not come back through the prose: the criterion
	// reads no declared duration, so a prompt teaching one would be teaching a
	// retry that no longer works.
	if strings.Contains(block, "s without asking") || strings.Contains(block, "bounded at ") {
		t.Errorf("the chat prompt still teaches a time band the guard no longer applies:\n%s", block)
	}
}

// TestChatOpCategoryThreePathsAgree is the property that matters end to end: for
// each of the three classes, the prompt's claim and the guard's verdict agree,
// driven through both real entry points (never a copy of either).
func TestChatOpCategoryThreePathsAgree(t *testing.T) {
	block := chatOpCategoryPromptBlock(t)
	t.Setenv(tools.ChatOpAutoEnv, "")
	t.Setenv(config.SettingsPathEnv, t.TempDir()+"/"+chatOpTestSettingsNameAbsent)

	// PATH 1 — operator work RUNS: the prompt states it is the console's own, and
	// the guard admits it with nothing declared.
	for _, cmd := range []string{"git status", "ls internal/agent", "gopi task list", "ssh zpeer 'gopi task list'"} {
		if adm := tools.ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("the prompt says operator work is the console's, but the guard refuses %q (%s)",
				cmd, adm.Reason())
		}
	}
	if !strings.Contains(block, "is YOURS to run in this turn with no permission question") {
		t.Error("the prompt does not grant the operator-work class — the guard admits it, so the model would " +
			"never try")
	}

	// PATH 2 — a repository write RUNS, and BOTH sides now say so. The operator's
	// 2026-10-24 ruling reversed the old hard no-go (a console that cannot commit
	// cannot finish anything, and on the night of the ruling the dispatched workers
	// were the thing breaking the machine, so "route it to a worker" routed the work
	// to the unreliable party). This test pinned the PREVIOUS ruling and stayed red
	// on master until it was rewritten here: what it asserts now is the pair that
	// must stay in step — the guard ADMITS the class, and the prompt renders the
	// ADMISSION's own sentence, so the model is told what will actually happen.
	for _, cmd := range []string{"git commit -m x", "go test ./internal/tools/", "sed -i s/a/b/ internal/x.go"} {
		adm := tools.ChatOpAdmit(cmd, "")
		if !adm.Allowed() {
			t.Errorf("a repository write (%q) is refused by the guard (%s) — the operator's 2026-10-24 ruling "+
				"opened it: a console that cannot commit cannot finish anything", cmd, adm.Reason())
		}
		if adm.Class() != tools.ChatOpClassRepoWrite {
			t.Errorf("%q classified as %s, want %s — the class is unchanged; only the permission moved",
				cmd, adm.Class(), tools.ChatOpClassRepoWrite)
		}
	}
	// SAME ORIGIN, one sentence, two readers: the prompt must carry the guard's own
	// admission sentence verbatim (Rule 13/14). A paraphrase here would be the rot
	// this file exists to catch — the prompt teaching a practice the guard does not
	// state, or (worse) the reverse.
	admission := tools.ChatOpRepoWriteRefusalSentence()
	if strings.TrimSpace(admission) == "" {
		t.Fatalf("the repository-write admission sentence is empty — nothing for the prompt to render")
	}
	if !strings.Contains(block, admission) {
		t.Errorf("the chat prompt does not carry the guard's own repository-write sentence:\nwant %q\nin:\n%s",
			admission, block)
	}
	// And neither side may drift back to the retired wording: the reversal is only
	// real while the ADMITTED class is not described as a task or a question.
	for _, retired := range []string{"never a permission", "WRONG for a REPOSITORY CHANGE"} {
		if strings.Contains(block, retired) {
			t.Errorf("the prompt still teaches the retired repository-write rule %q while the guard admits the "+
				"class — the two sides disagree:\n%s", retired, block)
		}
	}
	// The TASK route still exists and is still named — a repository write is the
	// console's to do, and a task is still the right home when the change wants a
	// branch, a review and a merge. Deleting that route would be the other error.
	if route := chatTaskSubmitRoute(); !strings.Contains(block, route) {
		t.Errorf("the prompt no longer carries the task route %q — a repository write is ADMITTED, but a "+
			"change that wants a branch and a review still routes there:\n%s", route, block)
	}

	// PATH 3 — a long operation ASKS: refused unauthorised, admitted behind the
	// operator's own words, and the prompt teaches exactly that sequence.
	//
	// THE GROUP SPELLING IS THE LIVE ONE (task 01a1958f): this arm used to drive
	// `gopi merge`, which is RETIRED and classifies as OPERATOR WORK, so the arm
	// was proving the ASK on a command the console can never run — and the live
	// spelling `gopi task merge` was in no row at all. Composed from the leaf
	// constants, never re-typed (Rules 1/13/14).
	longMerge := runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Merge + " " + chatOpLongMergeLiveFlag
	for _, cmd := range []string{"make test", "go build ./...", longMerge} {
		if adm := tools.ChatOpAdmit(cmd, ""); adm.Allowed() {
			t.Errorf("a long operation (%q) is admitted with nothing declared (%s)", cmd, adm.Reason())
		}
		if adm := tools.ChatOpAdmit(cmd, "跑吧，最多 5 分钟"); !adm.Allowed() {
			t.Errorf("a long operation (%q) is refused AFTER the operator authorised it (%s)", cmd, adm.Reason())
		}
	}
	if !strings.Contains(block, "must be ASKED about first") {
		t.Errorf("the prompt does not state that a long operation is the one class to ask about:\n%s", block)
	}
}

// chatOpTestSettingsNameAbsent is a settings path that cannot exist — the test
// pins the settings channel so the machine's own settings.json never decides a
// test (Rule 32's spirit: a test owns its inputs).
const chatOpTestSettingsNameAbsent = "no-such-settings.json"

// --- the CATEGORY TABLE's own two proofs ---------------------------------------
//
// The criterion's words moved out of internal/tools' Go slices and into a
// ```chat-op-category fence parsed by internal/chatpolicy (category.go). Two
// properties keep that move honest, and neither is provable by the class test
// above:
//
//  1. SAME ORIGIN, THE OTHER WAY ROUND. The class test reads the prompt and asks
//     the guard; this one walks the TABLE and drives the guard with a real command
//     line built from each line's own words, so a table line that no longer
//     classifies fails here instead of teaching the model a word the classifier
//     ignores.
//  2. TWO TABLES, NO BLEED. The category fence must never be able to decide what
//     runs. It is parsed from a DIFFERENT fence into a DIFFERENT type, so the
//     enforcement arm cannot read it — and this test states the property that
//     matters operationally: adding a category line (or emptying the whole table)
//     leaves the ENFORCEMENT verdict on a command unchanged. That is the vacuum
//     the separation exists to prevent (a class label that silently changes what
//     the console may run).

// TestChatOpCategoryTableDrivesTheClassifier is proof (1): every line of the frozen
// category table, turned into a command line from its OWN pattern words, classifies
// as the class that line declares — and the prompt renders those same words.
func TestChatOpCategoryTableDrivesTheClassifier(t *testing.T) {
	block := chatOpCategoryPromptBlock(t)
	tools.ResetChatOpCategoriesToDefaults()
	t.Cleanup(tools.ResetChatOpCategoriesToDefaults)
	tab := tools.ChatOpCategoryFrozen()
	if tab == nil || tab.Len() == 0 {
		t.Fatalf("no category table is frozen — the criterion would judge every line by the default class")
	}
	for _, class := range []struct {
		word string
		val  tools.ChatOpClass
	}{
		{tools.ChatOpClassWord(tools.ChatOpClassRepoWrite), tools.ChatOpClassRepoWrite},
		{tools.ChatOpClassWord(tools.ChatOpClassLong), tools.ChatOpClassLong},
	} {
		words := tab.WordsOf(class.word)
		if len(words) == 0 {
			t.Errorf("the category table declares no %q line — the class the prompt teaches matches nothing", class.word)
			continue
		}
		for _, pattern := range words {
			cmd := chatOpCategoryCommand(t, pattern)
			if got := tools.ChatOpClassify(cmd); got != class.val {
				t.Errorf("category line %q (as %q) classifies as %s, want %s — the table and the classifier "+
					"disagree about the table's OWN words", class.word, cmd, got, class.val)
			}
			if rendered := tools.ChatOpRenderedClassList([]string{pattern}); !strings.Contains(block, rendered) {
				t.Errorf("the chat prompt does not render the category word %q (rendered %q), which the "+
					"classifier matches:\n%s", pattern, rendered, block)
			}
		}
	}
}

// chatOpCategoryCommand turns one category line's pattern into a command line that
// the line must classify: the pattern's words joined, a trailing wildcard filled
// with one argument, and a directory word completed with one child file (so
// `> internal/` becomes `> internal/x.go`, the spelling an operator writes).
func chatOpCategoryCommand(t *testing.T, pattern string) string {
	t.Helper()
	fields := strings.Fields(pattern)
	if len(fields) == 0 {
		t.Fatalf("empty category pattern")
	}
	out := make([]string, 0, len(fields)+1)
	for i, f := range fields {
		if f == "*" && i == len(fields)-1 {
			out = append(out, "ARG")
			continue
		}
		if strings.HasSuffix(f, "/") {
			f += "x.go"
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// TestChatOpCategoryTableCannotDecideWhatRuns is proof (2): the category fence is
// not an enforcement table. Two measurements:
//
//	(a) the ENFORCEMENT verdict on a command is the same whether the category table
//	    is the shipped one or an EMPTY one — a category change moves the class, never
//	    the permission;
//	(b) the built-in category text carries NO decision word at all, so a category
//	    line can never be read as `allow`/`prompt`/`forbidden`.
func TestChatOpCategoryTableCannotDecideWhatRuns(t *testing.T) {
	t.Helper()
	tools.ResetChatPolicyToDefaults()
	t.Cleanup(tools.ResetChatOpCategoriesToDefaults)

	command := "git push origin master"
	tools.ResetChatOpCategoriesToDefaults()
	before := tools.ChatBashWriteRefusal(command)
	classBefore := tools.ChatOpClassify(command)

	// An EMPTY category table: on-request posture, no category lines at all.
	tools.SetChatOpCategories(&chatpolicy.CategoryTable{Source: "test-empty"})
	after := tools.ChatBashWriteRefusal(command)
	classAfter := tools.ChatOpClassify(command)

	if before != after {
		t.Errorf("the ENFORCEMENT verdict changed when the CATEGORY table changed (%q -> %q) — the two tables "+
			"are bleeding into each other", before, after)
	}
	if classBefore == classAfter {
		t.Errorf("emptying the category table left %q classified as %s — the classifier is not reading the "+
			"frozen table", command, classBefore)
	}
	if classAfter != tools.ChatOpClassOperatorWork {
		t.Errorf("with no category lines %q classified as %s — the fallback class must stay the running one",
			command, classAfter)
	}

	for _, word := range []string{"allow ", "prompt ", "forbidden "} {
		for _, line := range strings.Split(chatpolicy.DefaultCategoryText(), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), word) {
				t.Errorf("the built-in CATEGORY table carries the decision line %q — a category fence must "+
					"never be readable as an enforcement rule", strings.TrimSpace(line))
			}
		}
	}
}
