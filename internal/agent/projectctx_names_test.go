package agent

// projectctx_names_test.go — the regression proof for the TWO-NAME rule file
// (epic: split the root rule file by content kind, 2026-10-19).
//
// THE DEFECT THIS CLOSES: the project's engineering contract lived in the SAME
// file as one machine's operations policy, and that file is gitignored — so a
// bare clone, a worker's worktree and a sandbox profile started with NO project
// rules at all. The fix splits the file: the contract (Rules 1-41) is the
// TRACKED PROJECT.md, the policy stays in the ignored GOPI.md, and the loader
// accepts BOTH names.
//
// WHAT THESE TESTS PIN, one arm each:
//
//	both present      → PROJECT.md wins, GOPI.md is NAMED as ignored on stderr,
//	                    and the ignored body is NOT injected
//	only GOPI.md      → the pre-split behaviour, byte for byte: the local file
//	                    loads, its heading names IT, no conflict line is printed
//	only PROJECT.md   → the tracked file loads under its own heading
//	fence readers     → the CLIENT's file wins for the chat-policy fences (the
//	                    tracked contract carries no fence, so reading it would
//	                    silently empty the posture table)
//
// Nothing is mocked: real directories, real files, real bytes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// namesTrackedBody / namesLocalBody differ so an assertion can name WHICH
	// document was injected by content alone.
	namesTrackedBody = "# Tracked contract fixture\n\n- TRACKED-MARKER-31: the project's engineering rules.\n"
	namesLocalBody   = "# Local policy fixture\n\n- LOCAL-MARKER-77: this machine's operations policy.\n"
)

// namesTree builds a checkout-shaped directory (a `.git` directory bounds the
// upward walk) holding the named files with their bodies, and returns the dir.
func namesTree(t *testing.T, files map[string]string) string {
	t.Helper()
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, gitDirEntryName), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(top, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return top
}

// namesProfile points the process at an EMPTY private profile, so only the
// repository level contributes to the assertions.
func namesProfile(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir, err := config.GopiDir()
	if err != nil {
		t.Fatalf("config.GopiDir(): %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", dir, err)
	}
}

// TestProjectContextConflictResolvesToTheTrackedName is THE headline arm: a
// directory holding both documents injects the TRACKED one, names the ignored
// one on stderr, and contributes the ignored one's bytes NOWHERE — a run that
// silently dropped one of two rule files would lose half its rules with no
// trace (Rule 15).
func TestProjectContextConflictResolvesToTheTrackedName(t *testing.T) {
	namesProfile(t)
	repo := namesTree(t, map[string]string{
		projectContextFileName:      namesTrackedBody,
		localProjectContextFileName: namesLocalBody,
	})

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	wantPath := filepath.Join(repo, projectContextFileName)
	if got := ResolveProjectContextFile(repo); got.Path != wantPath {
		t.Fatalf("resolution = %q, want the tracked file %q (priority order is %v)", got.Path, wantPath, projectContextFileNames)
	}
	if !strings.Contains(ctx, strings.TrimSpace(namesTrackedBody)) {
		t.Fatalf("the tracked document was not injected:\n%s", ctx)
	}
	if strings.Contains(ctx, strings.TrimSpace(namesLocalBody)) {
		t.Fatalf("the IGNORED document was injected as well — the conflict must pick one:\n%s", ctx)
	}
	if !strings.Contains(ctx, repositoryHeading(projectContextFileName)) {
		t.Fatalf("the repository heading does not name %q:\n%s", projectContextFileName, ctx)
	}
	if !strings.Contains(msg, "[gopi] loaded "+projectContextFileName) {
		t.Fatalf("stderr does not report the loaded tracked file:\n%s", msg)
	}
	ignoredPath := filepath.Join(repo, localProjectContextFileName)
	if !strings.Contains(msg, "ignoring "+ignoredPath) {
		t.Fatalf("stderr does not NAME the ignored sibling %q — Rule 15: a conflict is never resolved in silence:\n%s",
			ignoredPath, msg)
	}
	if !strings.Contains(msg, "priority order: "+coreNameList()) {
		t.Fatalf("stderr does not state the priority order that decided it:\n%s", msg)
	}
}

// TestProjectContextOnlyLocalNameIsUnchanged is the BACKWARD-COMPATIBILITY arm:
// a checkout carrying only GOPI.md — every machine today, and every sandbox —
// behaves exactly as it did before the split.
func TestProjectContextOnlyLocalNameIsUnchanged(t *testing.T) {
	namesProfile(t)
	repo := namesTree(t, map[string]string{localProjectContextFileName: namesLocalBody})

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if !strings.Contains(ctx, strings.TrimSpace(namesLocalBody)) {
		t.Fatalf("a checkout carrying only %s lost its rules:\n%s", localProjectContextFileName, ctx)
	}
	if !strings.Contains(ctx, repositoryHeading(localProjectContextFileName)) {
		t.Fatalf("the heading does not name the file that was loaded (%s):\n%s", localProjectContextFileName, ctx)
	}
	if !strings.Contains(msg, "[gopi] loaded "+localProjectContextFileName) {
		t.Fatalf("stderr does not carry the pre-split loaded line:\n%s", msg)
	}
	if strings.Contains(msg, "ignoring ") {
		t.Fatalf("a single-file directory reported a conflict:\n%s", msg)
	}
}

// TestProjectContextOnlyTrackedNameLoads: a bare clone carries PROJECT.md and
// nothing else — that is the whole point of the split, and it must inject.
func TestProjectContextOnlyTrackedNameLoads(t *testing.T) {
	namesProfile(t)
	repo := namesTree(t, map[string]string{projectContextFileName: namesTrackedBody})

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if !strings.Contains(ctx, strings.TrimSpace(namesTrackedBody)) {
		t.Fatalf("a bare clone carrying only %s injected no rules:\n%s", projectContextFileName, ctx)
	}
	if !strings.Contains(msg, "[gopi] loaded "+projectContextFileName) {
		t.Fatalf("stderr does not report the tracked file:\n%s", msg)
	}
}

// TestProjectContextPriorityIsDistanceFirst: the priority order decides WITHIN a
// directory, never across the walk — a local file one level UP may not outrank
// the tracked file found HERE, which is the behaviour the single-name walk had.
func TestProjectContextPriorityIsDistanceFirst(t *testing.T) {
	namesProfile(t)
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, gitDirEntryName), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(top, localProjectContextFileName), []byte(namesLocalBody), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nested := filepath.Join(top, "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, projectContextFileName), []byte(namesTrackedBody), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := ResolveProjectContextFile(nested)
	if want := filepath.Join(nested, projectContextFileName); got.Path != want {
		t.Fatalf("resolution = %q, want %q — the NEAREST directory answers first; the name order only breaks a tie inside one directory",
			got.Path, want)
	}
	if len(got.Ignored) != 0 {
		t.Fatalf("ignored = %v, want none — the files are in different directories, so nothing was passed over", got.Ignored)
	}
}

// TestChatPolicyTableReadsTheClientFileWhenBothExist pins the SECOND resolver's
// order: the fences are the client's operations policy, so a directory holding
// both documents must have its posture table read from GOPI.md. Reading the
// tracked contract instead would silently return an EMPTY table (it carries no
// fence) and every command would fall back to the shipped defaults.
func TestChatPolicyTableReadsTheClientFileWhenBothExist(t *testing.T) {
	repo := namesTree(t, map[string]string{
		projectContextFileName:      "# Tracked contract\n\nno fence here\n",
		localProjectContextFileName: "# Local policy\n\n```chat-policy\nallow git status\n```\n",
	})

	pol, from, err := LoadChatPolicyTable(repo)
	if err != nil {
		t.Fatalf("LoadChatPolicyTable: %v", err)
	}
	wantPath := filepath.Join(repo, localProjectContextFileName)
	if from != wantPath {
		t.Fatalf("loaded the fence from %q, want the CLIENT's file %q", from, wantPath)
	}
	if pol == nil || pol.Len() != 1 {
		t.Fatalf("rules = %v, want the client file's single rule", pol)
	}

	// And the same directory's PROJECT-INJECTION walk still prefers the other
	// name — the two orders are one walk with two orders, not one file.
	if got := ResolveProjectContextFile(repo); got.Path != filepath.Join(repo, projectContextFileName) {
		t.Fatalf("the injection walk resolved %q, want the tracked file", got.Path)
	}
}
