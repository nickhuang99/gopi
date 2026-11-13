package tools

// shared_integration_ref_guard_run_dir_test.go — the guard's RUN-DIRECTORY
// regression (task 01a1621f, measured on the live binary 2026-10-22).
//
// The defect, in its smallest form: the guard judged every git invocation against
// the SESSION's working directory, so a command that carries its own
// `cd <dir> &&` was judged in a repository it never touches — and the refusal
// NAMED that repository, sending the reader to inspect a tree the command could
// not reach. The measured command was a plainly legal cross-repository fetch:
//
//	cd /home/nick/work/gopi && git fetch <path> <branch>:<branch>
//
// refused with "FROM /home/nick/workspace/<session clone> would rewrite the SHARED
// branch … of /home/nick/workspace/<session clone>, which is checked out by
// ANOTHER worktree", where the branch does not exist in /home/nick/work/gopi at
// all.
//
// The fixture is two repositories, and the SPLIT is what makes the assertion
// sharp: the session's repository HOLDS the destination branch (so the old
// judgement reads it as a shared-ref write), while the target repository does not
// (so the command is harmless there). Every case runs through BashInDir — the
// real bash-tool entry point — so a case can only pass if the guard is wired into
// the tool it governs.
//
// The second half of the same briefing is also measured here: a git invocation
// that exists only as TEXT (inside a here-document body) is not a command, and
// must not be judged — while a real invocation AFTER that body still is.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture spellings for the two-repository shape (Rule 1: a value used twice is
// named once).
const (
	// The TARGET repository: two checkouts, so its own shared namespace has a
	// holder to protect (the no-weakening half).
	refGuardRunDirTargetMainDirName  = "target-main-checkout"
	refGuardRunDirTargetOtherDirName = "target-linked-worktree"
	refGuardRunDirTargetTrunkBranch  = "tmain"
	refGuardRunDirTargetOtherBranch  = "tother"
	// The free names the PERMITTED fetch rows write into the target repository.
	// They are destination refs in a repository where nothing holds them.
	refGuardRunDirFreeBranch  = "refguard-free-dest"
	refGuardRunDirFreeBranch2 = "refguard-free-dest-2"
	// The here-document fixtures: the delimiter, and a body that spells a
	// governed write which must be READ AS TEXT.
	refGuardRunDirHeredocDelim = "REFGUARD_EOF"
	refGuardRunDirHeredocBody  = "git " + refGuardUpdateRefVerb + " " + refsHeadsPrefix + refGuardFixtureTrunkBranch + " deadbeef"
	// refGuardRunDirCommentBody is the same text as a shell COMMENT.
	refGuardRunDirCommentBody = "# " + refGuardRunDirHeredocBody
	refGuardRunDirEchoPrefix  = "echo ok"
)

// refGuardRunDirTarget is the second repository: a main checkout on
// refGuardRunDirTargetTrunkBranch plus ONE linked worktree holding
// refGuardRunDirTargetOtherBranch — the ordinary two-checkout shape, in a
// repository the session's cwd is NOT.
type refGuardRunDirTarget struct {
	main  string
	other string
}

// refGuardBuildRunDirTarget materialises that repository under base.
func refGuardBuildRunDirTarget(t *testing.T, base string) refGuardRunDirTarget {
	t.Helper()
	tr := refGuardRunDirTarget{
		main:  filepath.Join(base, refGuardRunDirTargetMainDirName),
		other: filepath.Join(base, refGuardRunDirTargetOtherDirName),
	}
	if err := os.MkdirAll(tr.main, 0o755); err != nil {
		t.Fatalf("mkdir target main checkout: %v", err)
	}
	refGuardGit(t, tr.main, "init", "-q", "-b", refGuardRunDirTargetTrunkBranch)
	if err := os.WriteFile(filepath.Join(tr.main, refGuardFixtureTrunkFile), []byte(refGuardFixtureSeedBody), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	refGuardGit(t, tr.main, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, tr.main, "commit", "-q", "-m", "seed")
	refGuardGit(t, tr.main, "worktree", "add", "-q", tr.other, "-b", refGuardRunDirTargetOtherBranch)
	return tr
}

// TestSharedIntegrationRefGuard_JudgesTheRunDirectory is the RED→GREEN proof of
// the run-directory resolution: `workdir` beats a leading `cd`, a leading `cd`
// beats the session cwd, and the refusal (when one is due) is about the
// repository the command actually reaches.
func TestSharedIntegrationRefGuard_JudgesTheRunDirectory(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	f := refGuardBuildFixture(t)
	tr := refGuardBuildRunDirTarget(t, f.base)

	// The destination is refGuardFixtureOtherBranch, and the two directions of the
	// fixture are what make the assertion sharp: the SESSION's repository HOLDS
	// that branch (f.other stands on it), so judging the command there reads it as
	// a shared-ref write — while the TARGET repository, where the command actually
	// runs, has no such branch at all, so the write there moves nothing anybody
	// stands on. This is the measured defect's own shape, not an invented one.
	permittedCrossRepoFetch := shellChangeDirBuiltin + " " + tr.main + " && " +
		gitBinaryName + " " + refGuardFetchVerb + " " + f.trunk + " " +
		refGuardFixtureSrcBranch + refGuardRefSep + refGuardFixtureOtherBranch

	t.Run("permit/cd-selects-the-run-directory", func(t *testing.T) {
		// The measured defect: refused while judged at f.trunk (the session cwd).
		if out, err := BashInDir(permittedCrossRepoFetch, f.trunk); err != nil {
			t.Fatalf("%q run FROM %s must be PERMITTED (it runs in %s): %v (output %q)",
				permittedCrossRepoFetch, f.trunk, tr.main, err, out)
		}
		// The fetch really landed in the TARGET repository — the assertion is the
		// ref, not the exit status.
		if got := refGuardGit(t, tr.main, "rev-parse", "--verify", "--quiet",
			refsHeadsPrefix+refGuardFixtureOtherBranch); got == "" {
			t.Fatalf("fetch did not create %s%s in %s", refsHeadsPrefix, refGuardFixtureOtherBranch, tr.main)
		}
		// And the session's own repository is UNTOUCHED by it: the branch f.other
		// stands on still points where it did — the command never reached it.
		if got := refGuardGit(t, f.trunk, "rev-parse", refsHeadsPrefix+refGuardFixtureOtherBranch); got == "" {
			t.Fatalf("the session repository lost %s%s — the run-directory carry is not the only source of truth here",
				refsHeadsPrefix, refGuardFixtureOtherBranch)
		}
	})

	t.Run("permit/workdir-parameter-still-wins", func(t *testing.T) {
		// The workdir spelling of the same legal command: no cd at all.
		cmd := gitBinaryName + " " + refGuardFetchVerb + " " + f.trunk + " " +
			refGuardFixtureSrcBranch + refGuardRefSep + refGuardRunDirFreeBranch2
		if out, err := BashInDir(cmd, tr.main); err != nil {
			t.Fatalf("%q with workdir=%s must be PERMITTED: %v (output %q)", cmd, tr.main, err, out)
		}
	})

	t.Run("refuse/the-target-repositorys-own-holder", func(t *testing.T) {
		// The no-weakening half: the SAME run directory, but the destination is
		// the branch the target's SECOND checkout stands on. The refusal must be
		// about that repository — never about the session's cwd, whose path
		// appears nowhere in the command.
		cmd := shellChangeDirBuiltin + " " + tr.main + " && " +
			gitBinaryName + " " + refGuardFetchVerb + " " + tr.main + " " +
			refGuardFixtureSrcBranch + refGuardRefSep + refGuardRunDirTargetOtherBranch
		_, err := BashInDir(cmd, f.trunk)
		if err == nil {
			t.Fatalf("%q must be REFUSED: %s is held by %s", cmd, refGuardRunDirTargetOtherBranch, tr.other)
		}
		refusal := err.Error()
		for _, want := range []string{tr.other, refGuardRunDirTargetOtherBranch, tr.main} {
			if !strings.Contains(refusal, want) {
				t.Errorf("%q: refusal must name %q, got:\n%s", cmd, want, refusal)
			}
		}
		// The judged repository is the target: the session's own checkouts are
		// never mentioned, because the command cannot reach them.
		for _, unwanted := range []string{f.trunk, f.other} {
			if strings.Contains(refusal, unwanted) {
				t.Errorf("%q: refusal names %q, a directory the command never touches:\n%s", cmd, unwanted, refusal)
			}
		}
	})
}

// TestSharedIntegrationRefGuard_HeredocBodyIsNotACommand pins the second half of
// the same briefing through the same entry point: text inside a here-document (or
// after a comment marker) is DATA, and a governed verb that appears only there is
// not an invocation. The mirror assertion keeps the rule from widening into
// "stop parsing after a heredoc": a REAL invocation after the body is still
// judged.
func TestSharedIntegrationRefGuard_HeredocBodyIsNotACommand(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	f := refGuardBuildFixture(t)

	heredoc := "cat <<'" + refGuardRunDirHeredocDelim + "'\n" +
		refGuardRunDirHeredocBody + "\n" +
		refGuardRunDirHeredocDelim + "\n"
	comment := refGuardRunDirEchoPrefix + " " + refGuardRunDirCommentBody
	// The body, then a real write: the body is text, the write is judged.
	heredocThenWrite := heredoc +
		gitBinaryName + " -C " + f.other + " " + refGuardUpdateRefVerb + " " +
		refsHeadsPrefix + refGuardFixtureTrunkBranch + " " + f.older

	t.Run("permit/heredoc-body-is-text", func(t *testing.T) {
		out, err := BashInDir(heredoc, f.other)
		if err != nil {
			t.Fatalf("a here-document whose BODY spells a governed write must be PERMITTED (nothing executes it): %v (output %q)", err, out)
		}
		if !strings.Contains(out, refGuardRunDirHeredocBody) {
			t.Fatalf("the here-document did not print its body — the fixture proves nothing: %q", out)
		}
	})

	t.Run("permit/comment-is-text", func(t *testing.T) {
		if out, err := BashInDir(comment, f.other); err != nil {
			t.Fatalf("a comment spelling a governed write must be PERMITTED: %v (output %q)", err, out)
		}
	})

	t.Run("refuse/real-invocation-after-a-heredoc", func(t *testing.T) {
		if _, err := BashInDir(heredocThenWrite, f.other); err == nil {
			t.Fatalf("a REAL invocation after a here-document must still be REFUSED:\n%s", heredocThenWrite)
		}
	})
}
