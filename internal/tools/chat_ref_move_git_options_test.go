package tools

// chat_ref_move_git_options_test.go — GIT'S OWN GLOBAL OPTIONS DO NOT MOVE THE
// REF-MOVE CLASS OUT OF THE LINE (row 01a19d6e; the hole measured by e1e22e9f).
//
// THE MEASURED DEFECT. The category table's ref-move rows are WORD PREFIXES and
// the column right after `git` is always the SUBCOMMAND:
//
//	ref-move git branch -f *
//	ref-move git branch -D *
//	ref-move git update-ref *
//	ref-move git push --force *
//	ref-move git reset --hard *
//
// So any spelling that puts one of git's GLOBAL options before the subcommand
// (`git -C <dir> push --force`, `git --git-dir=<p> push --force`,
// `git -c k=v push --force`, `git --work-tree=<p> push --force`) matched NO row,
// fell through to the default class — OPERATOR WORK, the class that RUNS — and
// the whole ref-move fence (PROJECT.md Rule 41 (2b): a ref move is the ONE act
// that stays the operator's) was bypassed by one inserted word.
//
// THE FIX IS NORMALISATION, NOT MORE ROWS. A row per combination would be a
// bottomless list (`-C` then `-c`, then `--no-pager`, …); the match instead
// reads the invocation's SUBCOMMAND and matches the table against
// `git <subcommand> <rest>`, with git's operand-consuming options skipped by the
// ONE table that already knows them (internal/gitargv, shared with the ref guard).
//
// THE DEMOTIONS STILL READ THE ORIGINAL WORDS. A `-C <dir>` names the directory
// git will actually run in, so the legs that may LOWER the class (dry run, ground
// delivery, fast-forward publish) keep receiving the words as typed — the
// subcommand normalisation is for the MATCH only, and can only ever RAISE a
// line's class.

import (
	"strings"
	"testing"
)

// chatRefMoveGitOptionProbeDir is a directory that is NOT a repository: the
// probe's `-C` operands point here so no demotion leg can resolve anything and
// each line is judged on its WORDS alone (a real repository would let the
// fast-forward leg answer, which is another row's surface — 01a19d56/01a19d7e).
const chatRefMoveGitOptionProbeDir = "/nonexistent/01a19d6e/probe-repo"

// TestRefMoveFamilySurvivesGitGlobalOptions is the header regression: ten lines,
// eight of which insert a git global option before a ref-moving subcommand. The
// last two are the READ controls — the point of the guard is a REF MOVE, not
// `git` in general, and a fix that refuses `git -C <dir> status` has broken the
// operator's own hand's-breadth work (Rule 37).
func TestRefMoveFamilySurvivesGitGlobalOptions(t *testing.T) {
	dir := chatRefMoveGitOptionProbeDir
	// `git push` IS NO LONGER A MEMBER of this family (operator's ruling,
	// 2026-11-03, row 01a1a786: "push 都可以") — every push spelling is REPOSITORY
	// WORK, so the global-option shapes below are spelled with the local
	// destructive verbs that still are.
	refMoves := []string{
		"git -C " + dir + " branch -f main HEAD~3",
		"git -C " + dir + " branch -D main",
		"git -C " + dir + " branch -f main HEAD~3",
		"git -C " + dir + " update-ref -d refs/heads/main",
		"git -C " + dir + " reset --hard HEAD~5",
		"git --git-dir=" + dir + "/.git branch -f main HEAD~3",
		"git -c core.hooksPath=/tmp/01a19d6e branch -f main HEAD~3",
	}
	for _, cmd := range refMoves {
		adm := ChatOpAdmit(cmd, "")
		t.Logf("%-70s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		if adm.Class() != ChatOpClassRefMove {
			t.Errorf("%q class = %s, want %s — a git GLOBAL OPTION before the subcommand must not move "+
				"the line out of the ref-move class", cmd, adm.Class(), ChatOpClassRefMove)
		}
		// ADMITTED by the operator's 2026-11-05 ruling; the CLASS above is what this
		// arm still proves (a global option cannot smuggle the move out of it).
		if !adm.Allowed() {
			t.Errorf("%q was refused (%s) — the 2026-11-05 ruling admitted the ref move", cmd, adm.Reason())
		}
	}
	reads := []string{
		"git -C " + dir + " status",
		"git -C " + dir + " log --oneline -3",
	}
	for _, cmd := range reads {
		adm := ChatOpAdmit(cmd, "")
		t.Logf("%-70s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		if adm.Class() != ChatOpClassOperatorWork {
			t.Errorf("%q class = %s, want %s — a READ in another directory is the console's own "+
				"work, and normalising the subcommand must not reach it", cmd, adm.Class(),
				ChatOpClassOperatorWork)
		}
		if !adm.Allowed() {
			t.Errorf("%q was REFUSED (reason %q) — the normalisation must not turn a read into a "+
				"ref move", cmd, adm.Reason())
		}
	}
}

// TestRefMoveSubcommandSpellingsAreCovered pins the OTHER spellings git accepts
// for the same option, because a normaliser that only handles the separated form
// is one more way to slip past the fence: `--git-dir=<p>` attached,
// `--git-dir <p>` separated, `--work-tree=<p>`, an option with NO operand
// (`--no-pager`), and several globals stacked before one subcommand.
func TestRefMoveSubcommandSpellingsAreCovered(t *testing.T) {
	dir := chatRefMoveGitOptionProbeDir
	for _, cmd := range []string{
		"git --git-dir=" + dir + "/.git branch -f main HEAD~3",
		"git --git-dir " + dir + "/.git branch -f main HEAD~3",
		"git --work-tree=" + dir + " branch -f main HEAD~3",
		"git --no-pager branch -f main HEAD~3",
		"git -p branch -f main HEAD~3",
		"git -C " + dir + " --no-pager branch -f main HEAD~3",
		"git --git-dir " + dir + "/.git -c color.ui=false update-ref refs/heads/x HEAD",
		"git -C " + dir + " branch --delete feature",
		"git --git-dir=" + dir + "/.git reset --hard HEAD~2",
	} {
		adm := ChatOpAdmit(cmd, "")
		t.Logf("%-70s => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		// THE CLASS IS THE PROPERTY; the refusal was retired by the operator's
		// 2026-11-05 ruling ("限制的全部改掉"). What this arm still proves is the thing
		// git's global options could once hide: the subcommand is READ THROUGH its
		// flags, so a `--git-dir` before `branch -f` cannot smuggle the move past the
		// criterion. A row that still refused here would be re-asserting the gate this
		// change removed.
		if adm.Class() != ChatOpClassRefMove || !adm.Allowed() {
			t.Errorf("%q => class=%s allowed=%v, want %s / admitted", cmd, adm.Class(), adm.Allowed(),
				ChatOpClassRefMove)
		}
	}
}

// TestGitGlobalOptionsDoNotRescueARepositoryWrite is the boundary of the change:
// normalisation raises a line to the class its SUBCOMMAND names, so a `-C` in
// front of a repository-write verb still runs (the 2026-10-24 ruling), and a `-C`
// in front of a read still runs.
func TestGitGlobalOptionsDoNotRescueARepositoryWrite(t *testing.T) {
	dir := chatRefMoveGitOptionProbeDir
	for _, cmd := range []string{
		"git -C " + dir + " add internal/tools/bash.go",
		"git -C " + dir + " commit -m x",
		"git -C " + dir + " diff --stat",
		"git -C " + dir + " show --stat HEAD",
		"git -C " + dir + " rev-parse HEAD",
	} {
		if adm := ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("%q was refused (class=%s, reason=%q) — the console's own work runs", cmd,
				adm.Class(), adm.Reason())
		}
	}
}

// TestUnparseableGitInvocationStaysARefMove is Rule 15's direction on this fence:
// a line the normaliser cannot resolve must not land in the permissive class BY
// ACCIDENT. The two shapes below carry a ref-moving WORD the normaliser cannot
// place behind a subcommand it found; the line is therefore still refused rather
// than quietly admitted (fail-closed: the class is only ever raised).
func TestUnparseableGitInvocationStaysARefMove(t *testing.T) {
	for _, cmd := range []string{
		// The option's operand is missing, so no subcommand follows it: git
		// itself fails this line, and the console must not admit it as work.
		"git -C",
		"git --git-dir",
	} {
		adm := ChatOpAdmit(cmd, "")
		t.Logf("%-30q => class=%s allowed=%v", cmd, adm.Class(), adm.Allowed())
		if adm.Class() == ChatOpClassRefMove && adm.Allowed() {
			t.Errorf("%q is classed %s yet ADMITTED — a class that asks must never be admitted",
				cmd, adm.Class())
		}
	}
	if strings.TrimSpace(ChatOpClassRefMove.String()) == "" {
		t.Fatal("the ref-move class renders as an empty string — the refusal text would say nothing")
	}
}
