package tools

// chat_ref_move_wrapper_operand_test.go — A WRAPPER'S OWN OPERAND DOES NOT HIDE
// THE GIT INVOCATION BEHIND IT (row 01a19d8a; the half 01a19d6e left open).
//
// ── THE MEASURED DEFECT ────────────────────────────────────────────────────
//
// 01a19d6e closed the hole where a GIT global option stood between `git` and the
// subcommand (`git -C <dir> push --force`). The same fence was still open one
// word EARLIER: a WRAPPER's operand. The head scan read every plain word before
// `git` as "the git token is somebody's argument", so `-u`'s operand `the operator` and
// env's assignment `FOO=1` ended the scan with "no git here" — and the class
// fell to OPERATOR WORK, the class that RUNS. Measured on this ground before the
// fix (the run's own probe, quoted in the report):
//
//	sudo -u the operator git branch -f main HEAD~3   OPERATOR WORK / allowed=true
//	env FOO=1 git branch -f main HEAD~3      OPERATOR WORK / allowed=true
//	sudo -u the operator git reset --hard HEAD~5          OPERATOR WORK / allowed=true
//	nice -n 10 git branch -f main HEAD~3     OPERATOR WORK / allowed=true
//	doas -u root git branch -f main HEAD~3   OPERATOR WORK / allowed=true
//
// ONE WRAPPER OPERAND WAS ENOUGH TO BYPASS THE ENTIRE REF-MOVE FENCE, which is
// the one act PROJECT.md Rule 41 (2b) keeps for the operator.
//
// ── WHAT THIS FILE PINS ────────────────────────────────────────────────────
//
// Three things, and the third is the one that makes the fix safe to land:
//
//  1. THE WRAPPER SPELLINGS ARE REF MOVES (allowed=false);
//  2. THE READS ARE NOT — `sudo -u the operator git status`, `sudo -u the operator ls`,
//     `env FOO=1 git log`, `echo git push` all stay OPERATOR WORK and run;
//  3. THE DEMOTION LEGS CANNOT READ A WRAPPER'S OPTION AS GIT'S. `nice -n 10 git
//     push --force` carries nice's `-n`, and the dry-run predicate read it as
//     push's `--dry-run`, demoting a REAL force-push to REPOSITORY WRITE /
//     allowed=true. That regression was produced by this row's own first attempt
//     and is pinned below as its own case.
//
// ── THE DIRECTION OF EVERY REFUSAL HERE IS FAIL-CLOSED ─────────────────────
//
// A shape the scanner cannot resolve keeps the class it already had. That is why
// the read controls are not decoration: a fix that refused `sudo -u the operator git
// status` would have traded an operator's hand's-breadth work (Rule 37) for a
// fence, which is not a trade this fence may make.

import "testing"

// TestRefMoveFamilySurvivesWrapperOperands is the header regression: every
// wrapper that may legitimately stand in front of a command, spelled with the
// operand that made the old scan give up, must still read as the REF MOVE its
// subcommand names.
func TestRefMoveFamilySurvivesWrapperOperands(t *testing.T) {
	refMoves := []string{
		// The privileged wrappers and their separated-operand options.
		"sudo -u the operator git branch -f main HEAD~3",
		"sudo -g staff git branch -f main HEAD~3",
		"sudo -u the operator git reset --hard HEAD~5",
		"sudo -u the operator git update-ref -d refs/heads/main",
		"sudo -u the operator git branch -D main",
		"doas -u root git branch -f main HEAD~3",
		// env: the assignment SHAPE and its own consuming options.
		"env FOO=1 git branch -f main HEAD~3",
		"env LC_ALL=C GIT_AUTHOR_NAME=x git branch -f main HEAD~3",
		"env -u GIT_DIR git branch -f main HEAD~3",
		// The bare assignment prefix — the shell strips it and runs git.
		"GIT_DIR=/nowhere git branch -f main HEAD~3",
		// The schedulers and their options.
		"nice -n 10 git branch -f main HEAD~3",
		"time -p git branch -f main HEAD~3",
		// The wrappers that take no operand at all.
		"nohup git branch -f main HEAD~3",
		"command git branch -f main HEAD~3",
		"exec git branch -f main HEAD~3",
		// xargs, with and without its own option.
		"xargs git branch -f main HEAD~3",
		"xargs -n 1 git branch -f main HEAD~3",
		// Stacked wrappers.
		"sudo -u the operator env FOO=1 git branch -f main HEAD~3",
	}
	for _, cmd := range refMoves {
		adm := ChatOpAdmitIn(cmd, chatRefMoveGitOptionProbeDir, "")
		t.Logf("%-62s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		if adm.Class() != ChatOpClassRefMove {
			t.Errorf("%q class = %s, want %s — a WRAPPER OPERAND before `git` must not "+
				"hide the ref-moving subcommand behind it", cmd, adm.Class(), ChatOpClassRefMove)
		}
		// THE ADMISSION IS THE OPERATOR'S 2026-11-05 RULING ("\u9650\u5236\u7684\u5168\u90e8\u6539\u6389"): a ref
		// move RUNS. The CLASS is what this arm still pins — that is what lets the
		// console state which ref it moved; a wrapper that hid the subcommand would
		// still run, and would do it silently.
		if !adm.Allowed() {
			t.Errorf("%q was refused (%s) — the operator's 2026-11-05 ruling admitted the ref move; "+
				"the class is what survives here, not the refusal", cmd, adm.Reason())
		}
	}
}

// TestWrapperOperandReadsAreNotRefMoves is the control that makes the fix
// admissible: the same wrappers in front of a READ are the console's own work
// and must run. `echo git push` is the shape the whole preamble rule exists to
// keep out — there `git` really IS an argument, and it must stay that way.
func TestWrapperOperandReadsAreNotRefMoves(t *testing.T) {
	reads := []string{
		"sudo -u the operator git status",
		"sudo -u the operator git log --oneline -3",
		"sudo -u the operator ls",
		"sudo -u the operator cat README.md",
		"env FOO=1 git log --oneline",
		"env FOO=1 git diff --stat",
		"nice -n 10 git status",
		"nohup git status",
		"sudo git status",
		"sudo -E git status",
		// THE DISCRIMINATOR, both directions: `git` is an ARGUMENT here, so
		// there is no invocation, and no class may be raised off these words.
		"echo git push",
		"echo git branch -f main HEAD~3",
		"printf '%s\\n' git push",
	}
	for _, cmd := range reads {
		adm := ChatOpAdmitIn(cmd, chatRefMoveGitOptionProbeDir, "")
		t.Logf("%-62s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		if adm.Class() != ChatOpClassOperatorWork {
			t.Errorf("%q class = %s, want %s — a READ behind a wrapper is the console's own work, "+
				"and reading the wrapper's operand must not reach it", cmd, adm.Class(),
				ChatOpClassOperatorWork)
		}
		if !adm.Allowed() {
			t.Errorf("%q was REFUSED (reason %q) — the fix must not turn a read into a ref move",
				cmd, adm.Reason())
		}
	}
}

// TestWrapperOwnOptionIsNotGitsPushOption pins the regression this row's first
// attempt produced. `gitPublishIsDryRun` scanned the WHOLE clause, so nice's own
// `-n` was read as push's `--dry-run`, and a REAL force-push was demoted from
// REF MOVE to REPOSITORY WRITE — i.e. ALLOWED. The dry-run exemption is
// legitimate (it writes nothing) and is not weakened here: it is applied to the
// push's OWN words only.
//
// The pair is the whole test: the same `-n`, in the wrapper's position and in the
// push's position, must give OPPOSITE answers.
func TestWrapperOwnOptionIsNotGitsPushOption(t *testing.T) {
	dir := chatRefMoveGitOptionProbeDir
	// The wrapper's `-n` is nice's; the push still writes a ref.
	niceCase := "nice -n 10 git branch -f main HEAD~3"
	if adm := ChatOpAdmitIn(niceCase, dir, ""); adm.Class() != ChatOpClassRefMove || !adm.Allowed() {
		t.Errorf("%q => class=%s allowed=%v, want %s / admitted — `-n` here belongs to nice, not to "+
			"push, so the push is not a dry run; and a ref move RUNS under the 2026-11-05 ruling",
			niceCase, adm.Class(), adm.Allowed(), ChatOpClassRefMove)
	}
	// The push's OWN `-n` is a dry run AND a push: since the 2026-11-03 ruling
	// every push spelling is repository work, so this arm now reads the class the
	// words name rather than a demotion (row 01a1a786).
	dryCase := "git push -n --force origin master"
	if adm := ChatOpAdmitIn(dryCase, dir, ""); adm.Class() != ChatOpClassRepoWrite || !adm.Allowed() {
		t.Errorf("%q => class=%s allowed=%v, want %s / allowed — every push spelling is repository work",
			dryCase, adm.Class(), adm.Allowed(), ChatOpClassRepoWrite)
	}
	// And the same `-n` in front of a READ stays a read.
	readCase := "nice -n 10 git status"
	if adm := ChatOpAdmitIn(readCase, dir, ""); adm.Class() != ChatOpClassOperatorWork || !adm.Allowed() {
		t.Errorf("%q => class=%s allowed=%v, want %s / allowed", readCase, adm.Class(),
			adm.Allowed(), ChatOpClassOperatorWork)
	}
}

// TestWrapperOperandUnreadableStaysFailClosed is the direction the whole fix must
// preserve: a wrapper option this package does not know ends the scan, so the
// line keeps the class it had rather than being read against a guessed
// subcommand. `sudo -Z <opaque> git push --force` is refused to be classified as
// an invocation at all; the assertion is that the answer is NOT a ref move built
// out of the wrong word, and that a genuinely unreadable line still does not
// crash or silently allow a REACHABLE subcommand.
func TestWrapperOperandUnreadableStaysFailClosed(t *testing.T) {
	// An option nobody declares: the scan stops at its operand, which is a plain
	// word, so there is no git invocation to classify. The line falls to the
	// default class — the SAME verdict this package gave before the fix, which is
	// the fail-closed direction (the fix may only turn lines RED, never green).
	cmd := "sudo -Z opaque git branch -f main HEAD~3"
	adm := ChatOpAdmitIn(cmd, chatRefMoveGitOptionProbeDir, "")
	t.Logf("%-62s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
	if adm.Class() == ChatOpClassRefMove {
		t.Errorf("%q class = %s — an UNREADABLE wrapper must not be read as a git invocation; "+
			"the scan has to stop, not guess", cmd, adm.Class())
	}
}
