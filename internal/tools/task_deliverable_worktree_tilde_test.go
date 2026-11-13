package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted pins the
// candidate seam's tilde contract (Rule 15, no silent failure):
//
// a bare "~user" is NOT expandable by expandHome, and the pre-fix code
// statted the RAW token — relative to the process cwd. That is the exact way
// a candidate could point at an unrelated directory and have its branch
// stamped into the row's deliverable (a wrong-path attribution), and when the
// wrong path did not exist the token was dropped in total silence — the
// observed producer-release failure mode (the row's deliverable stays
// repo-only, so the close-out release has nothing to free and nothing says
// why). The malformed token must be dropped, and no directory under the cwd
// may leak into the candidate list because of it.
func TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted(t *testing.T) {
	// A decoy directory that a naive `filepath.Abs("~decoyuser/wt")` would
	// resolve to: <cwd>/~decoyuser/wt. If the raw token is ever statted,
	// this existing directory becomes a candidate.
	decoy := filepath.Join("~decoyuser", "wt")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatalf("seed decoy: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll("~decoyuser") }) //stylegate:ignore best-effort removal of the decoy directory this test created
	decoyAbs, err := filepath.Abs(decoy)
	if err != nil {
		t.Fatalf("abs decoy: %v", err)
	}

	cands := taskBashWorkdirCandidates("", "cd ~decoyuser/wt && git status")
	for _, c := range cands {
		if c == decoyAbs {
			t.Fatalf("malformed tilde %q was statted as the cwd-relative %q and became a candidate — "+
				"a wrong-path stat can stamp an unrelated directory's branch into the row", decoy, decoyAbs)
		}
	}
}

// TestTaskBashWorkdirCandidates_ExpandsARealTilde is the guard's other half:
// the drop must be about MALFORMED tokens only — a plain "~/..." in a
// creation command (the Rule 6a shape `git worktree add ~/workspace/gopi-x`)
// still resolves to a real directory and stays a candidate. Without this the
// "drop" could be implemented by dropping every tilde.
func TestTaskBashWorkdirCandidates_ExpandsARealTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(home, "workspace", "gopi-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("seed worktree dir: %v", err)
	}

	cands := taskBashWorkdirCandidates("", "git worktree add ~/workspace/gopi-x -b feat/x")
	for _, c := range cands {
		if c == wt {
			return
		}
	}
	t.Fatalf("a real `~/...` worktree token was not expanded into %q; candidates=%v", wt, cands)
}
