package tools

import "testing"

// chat_op_spelling_test.go — the SPELLING TABLE for the chat classifier's
// DESCENT, pinned in both directions.
//
// THE DEFECT THIS PINS (measured before the fix, on the same table below):
//
//	git push --force origin master               REF MOVE
//	PROBE_SUB=$(git push --force origin master)  OPERATOR WORK   ← the class that RUNS
//	OUT=`git push --force origin master`         REF MOVE
//	bash -c 'git push --force origin master'     OPERATOR WORK   ← the class that RUNS
//	eval 'git push --force origin master'        OPERATOR WORK   ← the class that RUNS
//
// The backtick spelling was already judged and the three others were not, so the
// class of ONE command depended on its punctuation. Two independent holes produced
// it, and each is fixed where it belongs (Rules 13/14 — one scanner, no second
// tokenizer):
//
//   - the ONE tokenizer (chatShellTokens) treated `$( … )` as ordinary word text,
//     so `PROBE_SUB=$(git` became a single word that matches no category row. It
//     is now a clause BREAK, exactly as a backtick already was: the body's
//     commands are cut into clauses and judged, and the word the substitution was
//     folded into rejoins the stream afterwards (a `$( … )` is a word PART).
//   - the classifier (chatOpClassifyIn) judged only the clause as TYPED, so a
//     clause handing a script to an interpreter (`bash -c '…'`, `eval '…'`)
//     matched no row. It now expands each clause into the commands the shell will
//     actually run (chatOpJudgedClauses → chatOpScriptClauses), bounded by
//     chatOpMaxNestedDepth.
//
// BOTH DIRECTIONS ARE ASSERTED, and that is the point of the table rather than a
// single failing case: a descent that judged every wrapped command REF MOVE by
// default would pass every rejection row and turn `bash -c 'git status'` into a
// ref move — a boundary that refuses reads. So each wrapper appears twice, once
// around a ref move and once around a read, and each must land in its own class.

// chatOpSpellingCase is one row: a command line and the class the console must
// read for it.
type chatOpSpellingCase struct {
	name string
	cmd  string
	want ChatOpClass
}

const (
	chatOpSpellingRefMoveSource = "git branch -f main HEAD~3"
	chatOpSpellingReadSource    = "git status"
)

// chatOpSpellingWrappers is the ONE spelling table: every wrapper the defect's
// report named, spelled as a template over the command it wraps. Adding a wrapper
// adds BOTH of its assertions (ref move and read), so the two directions cannot
// drift apart.
var chatOpSpellingWrappers = []struct {
	name string
	// tmpl receives the wrapped command; "%s" appears exactly once.
	tmpl string
}{
	{name: "bare", tmpl: "%s"},
	{name: "dollar-paren-substitution", tmpl: "PROBE_SUB=$(%s)"},
	{name: "backtick-substitution", tmpl: "OUT=`%s`"},
	{name: "bash-c", tmpl: "bash -c '%s'"},
	{name: "eval", tmpl: "eval '%s'"},
}

// chatOpSpellingCases builds the table: every wrapper around a ref move, then
// every wrapper around a read. Built from the two lists above so no wrapper can be
// covered in one direction only.
func chatOpSpellingCases() []chatOpSpellingCase {
	out := make([]chatOpSpellingCase, 0, 2*len(chatOpSpellingWrappers))
	for _, w := range chatOpSpellingWrappers {
		out = append(out, chatOpSpellingCase{
			name: w.name + "/ref-move",
			cmd:  chatOpSpellingFill(w.tmpl, chatOpSpellingRefMoveSource),
			want: ChatOpClassRefMove,
		})
	}
	for _, w := range chatOpSpellingWrappers {
		out = append(out, chatOpSpellingCase{
			name: w.name + "/read",
			cmd:  chatOpSpellingFill(w.tmpl, chatOpSpellingReadSource),
			want: ChatOpClassOperatorWork,
		})
	}
	return out
}

// chatOpSpellingFill substitutes the wrapped command into a wrapper template.
func chatOpSpellingFill(tmpl, inner string) string {
	for i := 0; i+1 < len(tmpl); i++ {
		if tmpl[i] == '%' && tmpl[i+1] == 's' {
			return tmpl[:i] + inner + tmpl[i+2:]
		}
	}
	return tmpl
}

// TestChatOpSpellingTableDescendsIntoWrappedCommands drives the EXPORTED surface
// the chat face calls (ChatOpClassify) over the whole spelling table. A passing
// assertion is therefore about the answer the console gets, not about a helper's
// shape.
func TestChatOpSpellingTableDescendsIntoWrappedCommands(t *testing.T) {
	cases := chatOpSpellingCases()
	if len(cases) != 2*len(chatOpSpellingWrappers) {
		t.Fatalf("the table lost a direction: %d case(s) for %d wrapper(s)", len(cases), len(chatOpSpellingWrappers))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatOpClassify(tc.cmd); got != tc.want {
				t.Fatalf("ChatOpClassify(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestChatOpSpellingRefusalReachesTheDescendedCommand asserts the consequence the
// class exists for: a refused spelling is refused by the ADMISSION the tool
// dispatch asks (ChatOpAdmit), with the ref-move reason, and the read wrappers
// run unanswered. A classifier that returned the right class but whose admission
// did not read it would leave the hole open.
// TestChatOpSpellingRefusalReachesTheDescendedCommand had judged the ADMISSION a
// wrapped ref move lands on, and it asserted a REFUSAL with the ref-move reason.
// The operator's 2026-11-05 ruling ("限制的全部改掉") admitted the ref move, so what
// survives of this test is the half that is still TRUE and still load-bearing: the
// descent. A wrapped spelling must reach the same CLASS as its bare form — that is
// the classification property the wrappers exist to preserve — and the admission
// must then be the same on both sides.
func TestChatOpSpellingDescendsIntoTheSameClassAsTheBareSpelling(t *testing.T) {
	ResetChatPolicyToDefaults()
	for _, cmd := range []string{
		"PROBE_SUB=$(git branch -f main HEAD~3)",
		"bash -c 'git branch -f main HEAD~3'",
		"eval 'git branch -f main HEAD~3'",
	} {
		if got := ChatOpClassify(cmd); got != ChatOpClassRefMove {
			t.Errorf("ChatOpClassify(%q) = %v, want REF MOVE — the wrapper hid the command", cmd, got)
			continue
		}
		if adm := ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("ChatOpAdmit(%q) refused a ref move the operator's 2026-11-05 ruling admitted (%s)", cmd, adm.reason)
		}
	}
	for _, cmd := range []string{
		"PROBE_SUB=$(git status)",
		"bash -c 'git status'",
		"eval 'git status'",
	} {
		if adm := ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("ChatOpAdmit(%q) refused a wrapped READ (%s)", cmd, adm.reason)
		}
	}
}

// TestChatOpSpellingKeepsTheReadWrappingARead pins the OTHER half of the descent's
// contract with the surfaces that already existed: a `$( … )` inside a message is
// a read of a file, not a command, and must stay OPERATOR WORK. It is what stops a
// future "descend into every substitution" from refusing the console's own
// `gopi notify --message "$(cat reply.md)"`.
func TestChatOpSpellingKeepsTheReadWrappingARead(t *testing.T) {
	for _, cmd := range []string{
		`gopi notify --session s1 --message "$(cat /tmp/reply.md)"`,
		`cd "$(dirname "$0")" && git status`,
		`echo "$(git rev-parse --short HEAD)"`,
	} {
		if got := ChatOpClassify(cmd); got != ChatOpClassOperatorWork {
			t.Errorf("ChatOpClassify(%q) = %v, want OPERATOR WORK", cmd, got)
		}
	}
}

// TestChatOpSpellingDescendsIntoANestedScript pins the recursion's own direction:
// a script that hands another script to an interpreter is still judged by what the
// innermost one RUNS, and the bound (chatOpMaxNestedDepth) does not demote what it
// already read on the way down.
func TestChatOpSpellingDescendsIntoANestedScript(t *testing.T) {
	for _, cmd := range []string{
		`bash -c "eval 'git branch -f main HEAD~3'"`,
		`sh -ec 'bash -c "git branch -f main HEAD~3"'`,
		`xargs -0 git branch -f main HEAD~3`,
	} {
		if got := ChatOpClassify(cmd); got != ChatOpClassRefMove {
			t.Errorf("ChatOpClassify(%q) = %v, want REF MOVE", cmd, got)
		}
	}
	// A script FILE is not readable by the classifier, so it adds nothing — and the
	// clause it rides on is still classified by the words that ARE there.
	if got := ChatOpClassify("bash scripts/build.sh"); got == ChatOpClassRefMove {
		t.Errorf("ChatOpClassify of a script FILE read REF MOVE; the file's body was guessed at")
	}
}
