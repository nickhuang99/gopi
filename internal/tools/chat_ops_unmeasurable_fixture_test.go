package tools

// chat_ops_unmeasurable_fixture_test.go — the directory a chat-ops arm means when
// it says "there is NO directory this classification can be measured in".
//
// THE CONTRACT THIS FILE EXISTS FOR. ChatOpClassifyIn / ChatOpAdmitIn take the
// invocation's working directory, and "" is NOT a spelling for "no directory": it
// MEANS the process cwd. The product states it three times —
// ChatOpClassifyIn's own doc comment ("the bash tool's own `workdir` argument;
// "" = the process cwd"), chatOpRunDirRoot (which falls back to os.Getwd), and
// refGuardRunDir, whose twin implementation that is (Rule 14).
//
// So an arm that passes "" and reads the result as "nothing to measure" is
// measuring whatever repository `go test` happens to be standing in. ITS ANSWER
// THEN DEPENDS ON THE CHECKOUT, and the verdict flips:
//
//	clean + ahead 1 of its upstream  → the push earns the fast-forward demotion
//	                                   → REPOSITORY WRITE, and a `want REF MOVE`
//	                                   arm FAILS
//	no upstream, or dirty            → nothing is cleared → REF MOVE, and the arm
//	                                   passes
//
// MEASURED (2026-11-02, row 01a19d92). In the shared main checkout
// (/home/nick/work/gopi: master, upstream origin/master, `git status --porcelain`
// empty, `git rev-list --count @{u}..HEAD` = 1) SEVEN arms failed with exactly
// that shape — four in the ref-move family and three in the heredoc family —
// while the same source tree passed in a clone with no upstream. "Clean + ahead
// 1" is not exotic: it is the state EVERY worker is in between `git commit` and
// its publish.
//
// THE INPUT AN ARM ACTUALLY NEEDS is therefore a directory that CANNOT answer any
// of the four questions the demotions ask (clean tree / current branch / own
// upstream / upstream-is-an-ancestor). A directory that does not exist answers
// none of them: every `git -C <dir>…` probe fails, and the criterion keeps the
// class the WORDS alone decide — which is the only thing these arms are about.
//
// WHY NOT `t.Chdir`: pinning the process cwd makes the arm pass by fixing the
// fixture's measurement, not the caller's claim — the same source line would then
// be green in every checkout for a reason that has nothing to do with what it
// asserts, and the "-- = the process cwd" behaviour would stop being exercised by
// anything at all.
//
// WHY NOT `""`: it is the process cwd (above), so it is the one spelling the
// fixture exists to stop an arm from using.

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// chatOpUnmeasurableDirName names the arm's directory inside t.TempDir(), so a
// failure that prints the path says what the path was FOR.
const chatOpUnmeasurableDirName = "chat-ops-unmeasurable"

// chatOpGitRevParseVerb / chatOpGitDirFlag spell the ONE probe used to prove the
// directory answers no repository question. The verb is the same one the
// criterion's own upstream reader asks (chatPushFastForwardUpstream), so "this
// directory cannot answer" is measured with the instrument that would have
// answered it.
const (
	chatOpGitRevParseVerb = "rev-parse"
	chatOpGitDirFlag      = "--git-dir"
)

// chatOpUnmeasurableDir returns a directory that does not exist, inside a parent
// that does, and ASSERTS both halves: a directory that silently existed, or that
// answered git, would make every arm using it measure a checkout again — which is
// the defect this fixture replaces, one indirection further out.
func chatOpUnmeasurableDir(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, chatOpUnmeasurableDirName)
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the unmeasurable directory %s ALREADY EXISTS (stat err = %v) — the arms using it would "+
			"measure a real directory, which is the defect this fixture replaces", dir, err)
	}
	// The missing-parent case is the other way the probe below can fail, and it
	// would prove nothing: `git -C <dir>` may refuse for "cannot change to" rather
	// than for "not a repository". Assert the PARENT is there, so the refusal is
	// attributable to the directory the arms are handed.
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		t.Fatalf("the unmeasurable directory's parent %s is not a readable directory (err = %v) — the probe "+
			"below could then fail for the wrong reason", parent, err)
	}
	if out, err := exec.Command(gitBinaryName, "-C", dir, chatOpGitRevParseVerb,
		chatOpGitDirFlag).CombinedOutput(); err == nil {
		t.Fatalf("the arm's directory %s ANSWERED git (%q) — a classification judged there would be a "+
			"classification judged in a repository, and its answer would change with the checkout", dir, out)
	}
	// A directory that does NOT exist is refused by `git -C` at the chdir step, so
	// git never walks UPWARD out of it — there is nothing above it to find. The
	// probe states the property that matters instead of assuming it: the arms are
	// judged on the directory they are handed, and that directory answers nothing.
	if out, err := exec.Command(gitBinaryName, "-C", dir, chatOpGitRevParseVerb,
		chatOpGitDirFlag).CombinedOutput(); err == nil {
		t.Fatalf("%s became measurable between two probes (%q) — the fixture is not deterministic", dir, out)
	}
	return dir
}
