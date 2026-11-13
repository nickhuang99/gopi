package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitenv_test.go pins the three halves of the hermetic-git fix at their
// smallest sample (Rule 10.4): the strip, the process pin, and the refusal. The
// incident's shape — a fixture whose git child answers about a DIFFERENT
// repository than the one it built — is reproduced here as the red arm.

// TestWithoutGitRoutingEnvRemovesTheRoutingNamesAndNothingElse checks the strip
// is a WHOLE-NAME match: a variable that merely starts with a routing name
// survives, because over-removing is its own silent defect.
func TestWithoutGitRoutingEnvRemovesTheRoutingNamesAndNothingElse(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		GitDirEnvName + "=/somewhere/else/.git",
		GitWorkTreeEnvName + "=/somewhere/else",
		gitIndexFileEnvName + "=/somewhere/else/index",
		gitObjectDirectoryEnvName + "=/somewhere/else/objects",
		gitCommonDirEnvName + "=/somewhere/else/.git",
		GitDirEnvName + "ECTIVE=kept",
		"GIT_AUTHOR_NAME=fixture",
	}
	got := WithoutGitRoutingEnv(env)
	want := []string{"PATH=/usr/bin", GitDirEnvName + "ECTIVE=kept", "GIT_AUTHOR_NAME=fixture"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("strip mismatch\n got: %v\nwant: %v", got, want)
	}
}

// TestGitEnvStripsRoutingAndKeepsTheCallersPins is the helper's contract: the
// caller's own entries survive (they are appended AFTER the strip), and the
// inherited routing does not.
func TestGitEnvStripsRoutingAndKeepsTheCallersPins(t *testing.T) {
	t.Setenv(GitDirEnvName, "/somewhere/else/.git")
	got := GitEnv("GIT_AUTHOR_NAME=fixture")
	if !containsEntry(got, "GIT_AUTHOR_NAME=fixture") {
		t.Fatalf("the caller's pins were dropped: %v", got)
	}
	if containsEntryPrefix(got, GitDirEnvName+"=") {
		t.Fatalf("%s survived GitEnv: %v", GitDirEnvName, got)
	}
}

// TestPinSuiteGitEnvDropsTheInheritedRoutingToStderr is the process-level half,
// and it asserts the REPORT as well as the effect: an operator who exported a
// routing variable learns that the suite dropped it (Rule 15 — never silent).
func TestPinSuiteGitEnvDropsTheInheritedRoutingToStderr(t *testing.T) {
	t.Setenv(GitDirEnvName, "/somewhere/else/.git")
	before := len(os.Environ())
	PinSuiteGitEnv("gitenv-unit")
	if _, ok := os.LookupEnv(GitDirEnvName); ok {
		t.Fatalf("%s is still set after PinSuiteGitEnv — a fixture's git child would inherit it",
			GitDirEnvName)
	}
	if after := len(os.Environ()); after != before-1 {
		t.Fatalf("environment length %d → %d, want exactly one entry removed", before, after)
	}
}

// TestCheckGitDirInsideRootRefusesAForeignRepository is the RED arm of the third
// half, and it is the incident's own shape reproduced: a fixture that asks git
// about its temp root while GIT_DIR names another repository is told that the
// repository is OUTSIDE its root, instead of silently writing there.
//
// The foreign repository is built by this test itself (a second temp dir), so
// the arm needs no real checkout and writes nothing outside the scratch root
// (Rule 22/29).
func TestCheckGitDirInsideRootRefusesAForeignRepository(t *testing.T) {
	foreign := t.TempDir()
	gitInit(t, foreign)
	root := t.TempDir()
	gitInit(t, root)

	// Sanity: with the routing stripped, git answers with the fixture's OWN
	// repository — the assertion's green arm.
	dir, inside, err := checkGitDirInsideRoot(root)
	if err != nil {
		t.Fatalf("checkGitDirInsideRoot(%s): %v", root, err)
	}
	if !inside {
		t.Fatalf("the fixture's own repository was reported outside its root: dir=%s root=%s", dir, root)
	}

	// RED: the SAME call, but the probe's environment now carries the foreign
	// repository — the incident exactly.
	t.Setenv(GitDirEnvName, filepath.Join(foreign, ".git"))
	dir, inside, err = checkGitDirInsideRoot(t.TempDir())
	if err != nil {
		// A probe that cannot answer is an honest failure, never a pass.
		if !strings.Contains(err.Error(), "rev-parse") {
			t.Fatalf("unexpected probe error: %v", err)
		}
		return
	}
	if inside {
		t.Fatalf("a foreign %s was accepted as inside the fixture's root: dir=%s", GitDirEnvName, dir)
	}
}

// TestRestoreGitRoutingEnvRearmsWhatTheSuiteDropped pins the ONE caller's
// contract: the pin keeps what it removed, the restore puts the SAME value back,
// and its returned function takes it away again — a restore that leaked would
// leave the rest of the suite on the pre-pin environment, which is the state the
// pin exists to prevent.
func TestRestoreGitRoutingEnvRearmsWhatTheSuiteDropped(t *testing.T) {
	const poisoned = "/somewhere/else/.git"
	t.Setenv(GitDirEnvName, poisoned)
	PinSuiteGitEnv("gitenv-unit-restore")
	if _, ok := os.LookupEnv(GitDirEnvName); ok {
		t.Fatalf("%s survived PinSuiteGitEnv — nothing to restore", GitDirEnvName)
	}
	restore := RestoreGitRoutingEnv()
	if got, _ := os.LookupEnv(GitDirEnvName); got != poisoned {
		t.Fatalf("RestoreGitRoutingEnv put back %q, want the value the suite held (%q)",
			got, poisoned)
	}
	restore()
	if _, ok := os.LookupEnv(GitDirEnvName); ok {
		t.Fatalf("%s is still set after the restore's own undo — the suite would run on the pre-pin "+
			"environment", GitDirEnvName)
	}
	// A name the suite never dropped cannot be restored: the caller cannot
	// INJECT a routing value through this seam, only re-arm one that was there.
	if got, _ := os.LookupEnv(GitWorkTreeEnvName); got != "" {
		t.Fatalf("%s was not dropped by the pin but came back as %q — the restore must not invent "+
			"routing values", GitWorkTreeEnvName, got)
	}
}

// gitInit initialises a repository in dir, hermetically (the same helper the
// fixtures under test use), so this test cannot itself inherit a routing name.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	cmd.Env = GitEnv("GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in %s: %v\n%s", dir, err, out)
	}
}

// containsEntry reports whether env carries exactly this name=value entry.
func containsEntry(env []string, entry string) bool {
	for _, kv := range env {
		if kv == entry {
			return true
		}
	}
	return false
}

// containsEntryPrefix reports whether env carries an entry starting with prefix.
func containsEntryPrefix(env []string, prefix string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}
