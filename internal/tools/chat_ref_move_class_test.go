package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
)

// chat_ref_move_class_test.go — A PUBLISHED-REF MOVE IS ITS OWN CLASS, AND IT IS
// THE ONE THE CONSOLE ASKS ABOUT.
//
// THE MEASURED DEFECT (2026-10-24, task 01a16bb6 / 01a16bfd). The category table
// had no `git branch` line at all, so `git branch -f main HEAD~3` matched NOTHING
// and fell through to the default class, OPERATOR WORK — which ChatOpAdmit admits
// at every posture. Measured output before the fix:
//
//	git branch -f main HEAD~3   => OPERATOR WORK   allowed=true
//
// Two ways of getting there, one defect: the ref-move family was never given a
// class, so the criterion had nothing to refuse.
//
// THE RULE IT VIOLATED. PROJECT.md Rule 37: "a branch, a ref move or a push from
// here is still the operator's decision". The 2026-10-24 ruling that lets a chat
// edit, stage and commit does NOT cover moving a published ref.
//
// `git push` LEFT THIS FAMILY on 2026-11-03 (the operator's ruling "push 都可以",
// row 01a1a786): every push spelling is REPOSITORY WORK now, and the family this
// arm pins is the DESTRUCTIVE LOCAL one — what a branch points at.

// TestRefMoveFamilyIsItsOwnClassAndIsRefused pins one row per member of the
// family: the class AND the verdict, because either alone can regress silently
// (a command re-classed but still admitted, or refused for the wrong reason).
//
// THE CLASS IS JUDGED IN A DIRECTORY THAT CANNOT MEASURE ANYTHING
// (chatOpUnmeasurableDir, chat_ops_unmeasurable_fixture_test.go). This arm is
// about the WORDS: `git branch -f main HEAD~3` belongs to the ref-move family
// whatever tree it is typed in. Handing the criterion "" would not say that —
// "" is the PROCESS CWD (ChatOpClassifyIn's contract), so the answer would depend
// on whether `go test` happens to be standing in a checkout that is clean and one
// commit ahead of its upstream, where the fast-forward demotion legitimately
// re-classes that one spelling as REPOSITORY WRITE. Measured 2026-11-02: that is
// exactly how this arm failed in the shared main checkout.
func TestRefMoveFamilyIsItsOwnClassAndIsRefused(t *testing.T) {
	unmeasurable := chatOpUnmeasurableDir(t)
	for _, cmd := range []string{
		"git branch -f main HEAD~3",
		"git branch -D feature",
		"git branch -B feature master",
		"git branch --force main HEAD~3",
		"git branch --delete feature",
		"git update-ref refs/heads/x HEAD",
		"git reset --hard HEAD",
	} {
		adm := ChatOpAdmitIn(cmd, unmeasurable, "")
		if adm.Class() != ChatOpClassRefMove {
			t.Errorf("%q class = %s, want %s — the ref-move family must be spelled `%s` in the category "+
				"table, never left to fall through to OPERATOR WORK", cmd, adm.Class(), ChatOpClassRefMove,
				chatpolicy.CategoryRefMoveWord)
		}
		// THE ADMISSION IS THE OPERATOR'S 2026-11-05 RULING: "限制的全部改掉" — a ref
		// move RUNS. What this arm still pins is the CLASS, because that is what
		// makes the console able to STATE what it moved; a command re-classed into
		// OPERATOR WORK would run too, and would do it silently.
		if !adm.Allowed() {
			t.Errorf("%q was refused (%s) — the operator's 2026-11-05 ruling admitted the ref move; "+
				"the class is what survives, not the refusal", cmd, adm.Reason())
		}
	}
}

// TestRefMoveIsOpenedByTheOperatorsOwnTurn is the positive control: the class is
// ASKED ABOUT, not banned. Without it a fix could "pass" by refusing the ref move
// forever, which would be a different bug (and would make the console unable to
// do the operator's own hand's-breadth work in their turn).
//
// Judged in the unmeasurable directory for the same reason as the test above: the
// subject is the OPERATOR'S YES, and the class alone can be demoted by a
// fast-forward checkout, which would make this arm pass or fail on where `go test`
// ran rather than on what the operator said.
func TestRefMoveIsOpenedByTheOperatorsOwnTurn(t *testing.T) {
	const cmd = "git branch -f main HEAD~3"
	const operatorsYes = "yes, go ahead"
	if !ChatTurnApprovesTheToolQuestion(operatorsYes) {
		t.Fatalf("the fixture sentence %q is not recognised as an operator approval — the positive "+
			"control would test nothing", operatorsYes)
	}
	adm := ChatOpAdmitIn(cmd, chatOpUnmeasurableDir(t), operatorsYes)
	if adm.Class() != ChatOpClassRefMove {
		t.Fatalf("%q class = %s, want %s", cmd, adm.Class(), ChatOpClassRefMove)
	}
	if !adm.Allowed() {
		t.Errorf("%q was still refused after the operator's own yes (%q) — the ref move must be "+
			"ANSWERABLE, not banned", cmd, operatorsYes)
	}
}

// TestTheOperatorRulingSurvivesTheNewClass is the regression guard for the half
// that was RIGHT before this change and must stay right: the 2026-10-24 ruling
// lets a chat edit, stage and commit, and an ordinary console command runs with
// no question at all. A fix that refuses these has broken the ruling it was meant
// to protect (that is exactly what the abandoned `a4cd97d` arm did — measured:
// `ls -la`, `git commit -m x` and `sed -i s/a/b/ f.go` all came back allowed=false).
func TestTheOperatorRulingSurvivesTheNewClass(t *testing.T) {
	for _, cmd := range []string{
		"git add internal/tools/bash.go",
		"git commit -m x",
		"sed -i s/a/b/ f.go",
		"gofmt -w internal/tools/bash.go",
		"git status",
		"ls -la",
		"gopi task list",
		"ssh peer1 true",
	} {
		if adm := ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("%q was REFUSED (class=%s, reason=%q) — the 2026-10-24 ruling makes this the "+
				"console's own work: it runs with no question, and a fix for the ref-move family "+
				"must not take it away", cmd, adm.Class(), adm.Reason())
		}
	}
}

// TestRefMoveIsAdmittedAndStillNamedByItsClass replaces the refusal-text arm. The
// refusal it used to pin is GONE (operator's 2026-11-05 ruling: "限制的全部改掉"), so
// an arm asserting its wording would be testing a sentence the guard can no longer
// produce — and its own `t.Fatal("fixture no longer refuses")` is exactly what fired.
//
// WHAT THE RULING KEEPS is the NAME: the console still has to be able to say WHICH
// ref it moved, and the class is where that word comes from. So the property worth
// pinning is that a ref move is ADMITTED and still classified as one, never demoted
// into OPERATOR WORK — a demotion would run the same command silently, which is the
// outcome the class existed to prevent.
func TestRefMoveIsAdmittedAndStillNamedByItsClass(t *testing.T) {
	adm := ChatOpAdmit("git branch -f main HEAD~3", "")
	if !adm.Allowed() {
		t.Fatalf("a ref move was refused (%s) — the 2026-11-05 ruling admitted it", adm.Reason())
	}
	if adm.Class() != ChatOpClassRefMove {
		t.Fatalf("class = %s, want %s — an admitted ref move must still be NAMED, or the console "+
			"cannot say which ref it moved", adm.Class(), ChatOpClassRefMove)
	}
	if text := adm.Class().String(); !strings.Contains(text, "REF MOVE") {
		t.Errorf("class renders as %q — it must still name the class the model reasons about", text)
	}
}
